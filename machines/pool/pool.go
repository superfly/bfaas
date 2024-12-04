package pool

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/superfly/coordBfaas/machines"
)

const MetaPoolKey = "pool_id"

var cleanerDelay = 5 * time.Minute
var ErrPoolClosed = fmt.Errorf("The Pool Is Closed")

// checkOk transforms (ok, err) into err.
func checkOk(ok bool, err error) error {
	if err == nil && !ok {
		return fmt.Errorf("!ok")
	}
	return err
}

// FlyPool is a pool of Fly machines.
type FlyPool struct {
	api      *machines.Api
	name     string
	capacity int

	appName    string
	createReq  *machines.CreateMachineReq
	machPort   int
	createCtx  context.Context
	workerTime time.Duration
	leaseTime  time.Duration

	now func() time.Time // TODO: for mocking. do we really need this?

	metadata string

	// It is assumed that once shutdown is true, all machs
	// have been returned to the pool and no further operations will
	// be performed.
	shutdown bool

	mu       sync.Mutex
	size     int // note: size may be greater than len(machs) during machine creation.
	machs    map[string]*Mach
	free     chan *Mach
	discards chan *Mach
}

var _ Pool = (*FlyPool)(nil)

type Opt func(*FlyPool)

func Context(ctx context.Context) Opt {
	return func(p *FlyPool) { p.createCtx = ctx }
}

func Size(capacity int) Opt {
	if capacity < 1 {
		capacity = 1
	}
	return func(p *FlyPool) { p.capacity = capacity }
}

func Port(port int) Opt {
	return func(p *FlyPool) { p.machPort = port }
}

func WorkerTime(d time.Duration) Opt {
	return func(p *FlyPool) { p.workerTime = d }
}

func LeaseTime(d time.Duration) Opt {
	return func(p *FlyPool) { p.leaseTime = d }
}

// New creates a new machine pool of up to capacity machines owned by this pool.
// Name should be a unique name for the pool, such as the pool machine name.
func New(api *machines.Api, poolName string, appName string, createReq *machines.CreateMachineReq, opts ...Opt) (*FlyPool, error) {
	metadata := fmt.Sprintf("%v//%v", poolName, createReq.Config.Image)
	p := &FlyPool{
		name:      poolName,
		capacity:  2,
		size:      0,
		leaseTime: 30 * time.Minute,

		appName:    appName,
		createReq:  createReq,
		workerTime: time.Minute,
		machPort:   8000,

		now: time.Now,

		api:       api,
		createCtx: context.Background(),
		metadata:  metadata,

		machs: make(map[string]*Mach),
	}

	for _, opt := range opts {
		opt(p)
	}

	// construct after p.capacity might be set by options.
	p.free = make(chan *Mach, p.capacity)
	p.discards = make(chan *Mach, p.capacity) // TODO: think about sizing. reclaiming existing machines might produce more than capacity

	// Add one free machine to the pool to detect creation errors early.
	bgctx := context.Background()
	mach, err := p.createMach(bgctx, false)
	if err != nil {
		return nil, err
	}
	p.addFreeMach(mach)

	// Claim orphans that belong to us, or destroy them.
	p.claimOrphans(bgctx)

	go p.handleDiscards(bgctx)
	go p.cleanOrphans(bgctx)

	log.Printf("pool: starting with %d workers", p.size)
	return p, nil
}

// Close forcefully destroys all machines in the pool.
// It runs with a background context in an attempt to
// cleanup as best it can.
// It accumulates all errors it encounters, but continues
// to try to destroy each pool machine.
func (p *FlyPool) Close() error {
	log.Printf("pool: close")
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.shutdown {
		return nil
	}

	p.shutdown = true
	close(p.free)
	close(p.discards)

	// stop all machines, but don't destroy them.
	bgctx := context.Background()
	var err error
	for _, mach := range p.machs {
		err = errors.Join(err, mach.stop(bgctx))
	}
	return err
}

// addFreeMach adds the mach to the pool as a free machine.
// It should never be called when the machine is already in the pool.
func (p *FlyPool) addFreeMach(mach *Mach) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.machs[mach.Id] != nil {
		panic(fmt.Errorf("re-adding existing machine %v", mach.Id))
	}

	if p.size < p.capacity {
		p.size += 1
		p.machs[mach.Id] = mach
		p.free <- mach
		return true
	}
	return false
}

// createMach creates a new machine.
func (p *FlyPool) createMach(ctx context.Context, start bool) (*Mach, error) {
	req := *p.createReq
	req.SkipLaunch = !start
	req.Name = fmt.Sprintf("worker-%s-%d", p.name, rand.Uint64())
	expire := p.now().Add(p.leaseTime)
	req.LeaseTTL = int(p.leaseTime.Seconds())
	if req.Config.Metadata == nil {
		req.Config.Metadata = make(map[string]string)
	}
	req.Config.Metadata[MetaPoolKey] = p.metadata // XXX TODO: metadata is untrusted!

	log.Printf("pool: create %s %s", p.appName, req.Name)
	flym, err := p.api.Create(ctx, p.appName, &req)
	if err != nil {
		return nil, fmt.Errorf("api.Create %s: %w", p.appName, err)
	}

	return newMach(p, flym, expire, start), nil
}

// getFreeImmediately returns the next free machine if there are any immediately available.
func (p *FlyPool) getFreeImmediately() *Mach {
	select {
	case mach := <-p.free:
		return mach
	}
	return nil
}

// waitForFree returns the next free machine, waiting for one if none is available.
func (p *FlyPool) waitForFree(ctx context.Context) (*Mach, error) {
	if p.shutdown {
		return nil, ErrPoolClosed
	}

	select {
	case <-ctx.Done():
		log.Printf("pool: alloc cancelled context")
		return nil, ctx.Err()
	case mach := <-p.free:
		if mach == nil {
			log.Printf("pool: alloc cancelled with closed pool")
			return nil, ErrPoolClosed
		}
		return mach, nil
	}
}

// growPool creates a new machine and adds it to the pool if the
// pool is not yet at capacity. It returns the created machine but
// does not add it to the free list.
func (p *FlyPool) growPool(ctx context.Context) (*Mach, error) {
	p.mu.Lock()
	create := p.size < p.capacity
	if create {
		p.size += 1 // indicate intention to grow the pool
	}
	p.mu.Unlock()

	if !create {
		return nil, nil
	}

	mach, err := p.createMach(ctx, true)
	if err != nil {
		p.mu.Lock()
		p.size -= 1 // we failed, roll back the size change.
		p.mu.Unlock()

		log.Printf("pool: growPool createMach failed: %v", err)
		return nil, err
	}

	p.mu.Lock()
	// p.size has already been updated...
	p.machs[mach.Id] = mach
	p.mu.Unlock()
	return mach, nil
}

// allocLeased gets the next free machine that has enough lease time left,
// discarding any machines that do not have enough lease time left.
// It grows the pool automatically if there are no free machines immediately
// available and the pool is not yet at capacity.
func (p *FlyPool) allocLeased(ctx context.Context) (*Mach, error) {
	var err error
	for {
		mach := p.getFreeImmediately()

		if mach == nil {
			mach, err = p.growPool(ctx)
			if err != nil {
				return nil, err
			}
		}

		if mach == nil {
			mach, err = p.waitForFree(ctx)
			if err != nil {
				return nil, err
			}
		}

		if mach.leaseSufficient(p.workerTime) {
			return mach, nil
		}

		p.discardMach(mach, "not enough lease left")
		// and try again...
	}
}

// Alloc returns the next free machine, blocking if necessary.
func (p *FlyPool) Alloc(ctx context.Context) (*Mach, error) {
	log.Printf("pool: alloc")
	mach, err := p.allocLeased(ctx)
	if err != nil {
		return nil, err
	}

	if err := mach.start(ctx); err != nil {
		log.Printf("pool: startMach: %v", err)
		p.discardMach(mach, "start machine failed")
		return nil, err
	}

	log.Printf("pool: alloc %s %s", p.appName, mach.Id)
	return mach, nil
}

// freeMach stops a machine and returns it to the pool.
// Freeing is done in a background context to stop machines as best as possible.
// This can block for a few seconds, but is safe to call as `go p.Free(mach)`.
func (p *FlyPool) freeMach(mach *Mach) {
	log.Printf("pool: free %s %s", p.appName, mach.Id)
	if p.shutdown {
		return
	}

	ctx := context.Background()
	if err := mach.stop(ctx); err != nil {
		log.Printf("free: stopMach: %v", err)
		p.discardMach(mach, "stop machine failed")
		return
	}

	p.free <- mach
}

// discardMach asynchronously destroys the machine or fails silently.
func (p *FlyPool) discardMach(mach *Mach, msg string) {
	log.Printf("pool: discard machine %v: %v", mach.Id, msg)

	p.mu.Lock()
	delete(p.machs, mach.Id)
	p.size -= 1
	p.mu.Unlock()

	p.discards <- mach
}

// handleDiscards handles discarded machines asynchronously with
// a minimal-effort attempt at destroying the machine.
// Failures here will be caught by this or another pool's cleanOrphans cleaner.
func (p *FlyPool) handleDiscards(ctx context.Context) {
	log.Printf("pool: handleDiscards started")
	for mach := range p.discards {
		if err := mach.destroy(ctx); err != nil {
			log.Printf("pool: handleDiscards: %v", err)
			// but continue...
		}

		// stagger the cleanup a little if there are several machines to discard...
		time.Sleep(100 * time.Millisecond)
	}
	log.Printf("pool: handleDiscards done")
}

// cleanOrpans destroys any worker machines that are not owned by any pool.
// A machine is owned by a pool if the pool has a valid lease on it, and
// has marked the pool ownership in the machine's metadata.
func (p *FlyPool) cleanOrphans(ctx context.Context) {
	// TODO: p.shutdown access is not atomic. but probably ok?
	for !p.shutdown {
		log.Printf("pool: cleanOrphans: listing machines")
		ms, err := p.api.List(ctx, p.appName)
		if err != nil {
			log.Printf("pool: cleanOrphans: api.List: %v", err)
		} else {
			for _, m := range ms {
				hasLease := m.Nonce != ""
				if hasLease {
					continue
				}

				// This would fail without a nonce if there was a lease.
				p.api.Destroy(ctx, p.appName, m.Id, true)

				// stagger the cleanup a little if there are several machines to discard...
				time.Sleep(100 * time.Millisecond)
			}
		}

		time.Sleep(cleanerDelay)
	}
}

// claimOrphans enumerates all machines and processes all the machines
// that are owned by this pool (or a previous incarnation of this pool).
// It adopts orphans that are suitable for use, up to the pool capacity,
// and discards any others.
func (p *FlyPool) claimOrphans(ctx context.Context) {
	ms, err := p.api.List(ctx, p.appName)
	if err != nil {
		log.Printf("pool: claimOrphans: api.List: %v", err)
		return
	}

	reconstitute := func(mach *Mach) error {
		// If it was orphaned while running, make sure to stop it.
		if err := mach.stop(ctx); err != nil {
			return err
		}

		lease, err := p.api.GetLease(ctx, p.appName, mach.Id)
		if err != nil {
			return fmt.Errorf("api.GetLease: %w", err)
		}
		if lease.Status != "success" {
			return fmt.Errorf("api.GetLease: !success")
		}

		leaseExpires := time.Unix(lease.Data.ExpiresAt, 0)
		mach.leaseExpires = leaseExpires // ...told you.
		if !mach.leaseSufficient(p.workerTime) {
			return fmt.Errorf("lease expiring too soon")
		}

		if !p.addFreeMach(mach) {
			return fmt.Errorf("pool already at capacity")
		}

		return nil
	}

	cnt := 0
	for _, m := range ms {
		p.mu.Lock()
		inPool := p.machs[m.Id] != nil
		p.mu.Unlock()

		if inPool {
			// we already have it in our pool.
			continue
		}

		hasLease := m.Nonce != ""
		if !hasLease || m.Config.Metadata[MetaPoolKey] != p.metadata {
			// its expired, or we dont want it. Let the cleaner handle it.
			continue
		}

		var leaseExpires time.Time // we'll fill this in later (in reconstitute)...
		started := m.State == "started"
		mach := newMach(p, &m, leaseExpires, started)
		if err := reconstitute(mach); err != nil {
			log.Printf("pool: claimOrphans: destroying %v because reconstitute failed: %v", mach.Id, err)
			mach.destroy(ctx)
		} else {
			cnt += 1
		}
	}
	log.Printf("pool: claimed %d orphans", cnt)
}
