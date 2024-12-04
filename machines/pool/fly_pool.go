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

func sleepWithContext(ctx context.Context, dt time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(dt):
		return nil
	}
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
	isShutdown bool
	cancel     context.CancelFunc

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

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	if false {
		// Add one free machine to the pool to detect creation errors early.
		mach, err := p.createMach(ctx, false)
		if err != nil {
			p.cancel()
			return nil, err
		}
		p.addFreeMach(mach)
	}

	// claim orphans for our pool and cleanup, in the background.
	go p.clean(ctx)

	log.Printf("pool: starting with %d workers", p.size) // TODO: p.size is not atomic
	return p, nil
}

// shutdown stops the pool but does not perform cleanup.
// See Close/Destory for cleanup
func (p *FlyPool) shutdown() {
	log.Printf("pool: shutdown")
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isShutdown {
		return
	}

	p.isShutdown = true
	close(p.free)
	close(p.discards)
	p.cancel()
}

// Close stops all machines in the pool.
// It runs with a background context in an attempt to
// cleanup as best it can.
// It accumulates all errors it encounters, but continues
// to try to stop each pool machine.
func (p *FlyPool) Close() error {
	p.shutdown()

	p.mu.Lock()
	defer p.mu.Unlock()

	// stop all machines, but don't destroy them.
	bgctx := context.Background()
	var err error
	for _, mach := range p.machs {
		err = errors.Join(err, mach.stop(bgctx))
	}
	return err
}

// Destroy destroys all machines in the pool.
// It runs with a background context in an attempt to
// cleanup as best it can.
// It accumulates all errors it encounters, but continues
// to try to destroy each pool machine.
func (p *FlyPool) Destroy() error {
	p.shutdown()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Destroy All Machines!
	bgctx := context.Background()
	var err error
	for machid, mach := range p.machs {
		err = errors.Join(err, mach.destroy(bgctx))
		delete(p.machs, machid)
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

	return newMach(p, flym, flym.Nonce, expire, start), nil
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
	if p.isShutdown {
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
	if p.isShutdown {
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
	log.Printf("pool: handleDiscards exiting")
}

func (p *FlyPool) getLease(ctx context.Context, machId string) (*machines.LeaseData, error) {
	lease, err := p.api.GetLease(ctx, p.appName, machId)
	if err != nil {
		return nil, fmt.Errorf("api.GetLease: %w", err)
	}
	if lease.Status != "success" {
		return nil, fmt.Errorf("api.GetLease: !success")
	}
	return &lease.Data, nil
}

// cleanMach performs cleanup operations on a machine, if necessary.
// It destroys machines that do not have an active lease.
// It adopts machines into the pool if they are owned by this pool instance and not yet in the pool,
// or destroys them if they are not suitable or needed.
// It returns the number of machines adopted into this pool.
//
// Note: This is complicated for efficiency, to avoid doing too many fly API calls.
// This is run periodically for every machine in the worker app.
// It would be greatly simplified if listing machines returned back lease information.
func (p *FlyPool) cleanMach(ctx context.Context, m *machines.MachineResp) int {
	p.mu.Lock()
	alreadyInOurPool := p.machs[m.Id] != nil
	p.mu.Unlock()

	ours := m.Config.Metadata[MetaPoolKey] != p.metadata
	createdAt, _ := time.Parse(time.RFC3339, m.CreatedAt)
	age := p.now().Sub(createdAt)
	probablyExpired := age > p.leaseTime

	if alreadyInOurPool {
		return 0
	}

	if !ours {
		if probablyExpired {
			// Try to destroy it. This wont work if it has a valid lease since we don't provide the lease nonce.
			log.Printf("pool: cleanMach %v: age=%v, destroying, not ours", m.Id, age)
			p.api.Destroy(ctx, p.appName, m.Id, true)
		}
		return 0
	}

	lease, err := p.getLease(ctx, m.Id)
	if err != nil {
		log.Printf("pool: cleanMach %v: destroying, error getting lease: %v", m.Id, err)
		p.api.Destroy(ctx, p.appName, m.Id, true)
	}

	leaseExpires := time.Unix(lease.ExpiresAt, 0)
	started := m.State == "started"
	mach := newMach(p, m, lease.Nonce, leaseExpires, started)

	err = func() error {
		if !mach.leaseSufficient(p.workerTime) {
			return fmt.Errorf("lease expiring too soon")
		}

		if err := mach.stop(ctx); err != nil {
			return err
		}

		if !p.addFreeMach(mach) {
			return fmt.Errorf("pool already at capacity")
		}

		return nil
	}()
	if err != nil {
		log.Printf("pool: cleanMach %v: destroying: %v", m.Id, err)
		mach.destroy(ctx)
		return 0
	} else {
		return 1
	}
}

func (p *FlyPool) clean(ctx context.Context) {
	log.Printf("pool: clean starting")
	for {
		log.Printf("pool: cleaning")
		ms, err := p.api.List(ctx, p.appName)
		if err != nil {
			log.Printf("pool: clean: api.List: %v", err)
		} else {
			cnt := 0
			for _, m := range ms {
				cnt += p.cleanMach(ctx, &m)
			}
			log.Printf("pool: clean: adopted %d machines", cnt)
		}

		if err := sleepWithContext(ctx, cleanerDelay); err != nil {
			break
		}
	}
	log.Printf("pool: clean exiting")
}
