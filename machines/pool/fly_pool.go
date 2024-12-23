package pool

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/superfly/coordBfaas/japi"
	"github.com/superfly/coordBfaas/machines"
	"github.com/superfly/coordBfaas/stats"
)

const MetaPoolKey = "pool_id"

const (
	statsAlloc   = "alloc"
	statsCreate  = "create"
	statsStart   = "start"
	statsStop    = "stop"
	statsDestroy = "destroy"
	statsLease   = "lease"
)

var cleanerDelay = 5 * time.Minute
var ErrPoolClosed = fmt.Errorf("The Pool Is Closed")
var defaultGuest = machines.Guest{
	CpuKind:  "shared",
	Cpus:     1,
	MemoryMb: 256,
}

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

func newWorkerName(poolName string) string {
	return fmt.Sprintf("worker-%s-%d", poolName, rand.Uint64())
}

func parseWorkerName(name string) (string, error) {
	ws := strings.Split(name, "-")
	if len(ws) != 3 {
		return "", fmt.Errorf("malformed worker name format")
	}
	if ws[0] != "worker" {
		return "", fmt.Errorf("malformed worker name tag")
	}
	return ws[1], nil
}

// FlyPool is a pool of Fly machines.
type FlyPool struct {
	api        *machines.Api
	name       string
	capacity   int
	leaseTime  time.Duration
	workerTime time.Duration

	appName    string
	machImage  string
	machPort   int
	machGuest  *machines.Guest
	machRegion string

	now func() time.Time // TODO: for mocking. do we really need this?

	metadata string

	// It is assumed that once shutdown is true, all machs
	// have been returned to the pool and no further operations will
	// be performed.
	isShutdown bool
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	freeWg     sync.WaitGroup

	mu       sync.Mutex
	machs    map[string]*Mach
	free     chan *Mach
	discards chan *Mach

	stats map[string]*stats.Collector
}

var _ Pool = (*FlyPool)(nil)

type Opt func(*FlyPool)

func Size(capacity int) Opt {
	if capacity < 1 {
		capacity = 1
	}
	return func(p *FlyPool) { p.capacity = capacity }
}

func WorkerTime(d time.Duration) Opt {
	return func(p *FlyPool) { p.workerTime = d }
}

func LeaseTime(d time.Duration) Opt {
	return func(p *FlyPool) { p.leaseTime = d }
}

func Port(port int) Opt {
	return func(p *FlyPool) { p.machPort = port }
}

func Guest(guest *machines.Guest) Opt {
	return func(p *FlyPool) { p.machGuest = guest }
}

func Region(region string) Opt {
	return func(p *FlyPool) { p.machRegion = region }
}

// New creates a new machine pool of up to capacity machines owned by this pool.
// Name should be a unique name for the pool, such as the pool machine name.
func New(api *machines.Api, poolName, appName, image string, opts ...Opt) (*FlyPool, error) {
	metadata := fmt.Sprintf("%v//%v", poolName, image)
	p := &FlyPool{
		name:       poolName,
		capacity:   2,
		leaseTime:  30 * time.Minute,
		workerTime: time.Minute,

		appName:   appName,
		machImage: image,
		machPort:  8000,
		machGuest: &defaultGuest,

		now: time.Now,

		api:      api,
		metadata: metadata,

		machs: make(map[string]*Mach),

		stats: map[string]*stats.Collector{
			statsAlloc:   stats.New(),
			statsCreate:  stats.New(),
			statsStart:   stats.New(),
			statsStop:    stats.New(),
			statsDestroy: stats.New(),
			statsLease:   stats.New(),
		},
	}

	for _, opt := range opts {
		opt(p)
	}

	// construct after p.capacity might be set by options.
	p.free = make(chan *Mach, p.capacity)
	p.discards = make(chan *Mach, p.capacity)

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	// claim orphans for our pool and cleanup, in the background.
	p.wg.Add(2)
	go p.handleDiscards(ctx)
	go p.clean(ctx)

	return p, nil
}

// shutdown stops the pool but does not perform cleanup.
// See Close/Destory for cleanup
func (p *FlyPool) shutdown() {
	log.Printf("pool: shutdown")

	// make sure bg freeMach threads are finished.
	p.freeWg.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isShutdown {
		return
	}

	p.isShutdown = true
	close(p.free)
	close(p.discards)
	p.cancel()
	p.wg.Wait()
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
	for _, mach := range p.machs {
		err = errors.Join(err, mach.destroy(bgctx))
		delete(p.machs, mach.Name)
	}
	return err
}

// addFreeMach adds the mach to the pool as a free machine if it is needed.
// The machine should be "stopped" and should not yet be in the pool.
func (p *FlyPool) addFreeMach(mach *Mach) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.machs) < p.capacity {
		p.machs[mach.Name] = mach
		p.free <- mach
		return true
	}
	return false
}

// createMach creates a new machine and starts it.
func (p *FlyPool) createMach(ctx context.Context, mach *Mach) error {
	dt := p.stats[statsCreate].Start()
	defer dt.End()

	req := machines.CreateMachineReq{
		Name:       mach.Name,
		LeaseTTL:   int(mach.leaseExpires.Sub(time.Now()).Seconds()),
		SkipLaunch: false,
		Region:     p.machRegion,
		Config: machines.MachineConfig{
			Image: p.machImage,
			Guest: *p.machGuest,
			Restart: machines.Restart{
				Policy: "no",
			},
			Metadata: map[string]string{
				MetaPoolKey: p.metadata,
			},
			Services: []machines.Service{
				machines.Service{
					Protocol:     "tcp",
					InternalPort: p.machPort,
					Autostop:     false,
					Autostart:    false,
					Ports: []machines.Port{
						machines.Port{
							Port:       80,
							Handlers:   []string{"http"},
							ForceHTTPS: false,
						},
					},
				},
			},
		},
	}

	log.Printf("pool: create %s %s", p.appName, req.Name)
	flym, err := p.api.Create(ctx, p.appName, &req)
	if err != nil {
		return fmt.Errorf("api.Create %s: %w", p.appName, err)
	}

	mach.Id = flym.Id
	mach.InstanceId = flym.InstanceId
	mach.leaseNonce = flym.Nonce

	//log.Printf("pool: create %s %s: success %v", p.appName, req.Name, mach.Id)
	if err := mach.waitFor(ctx, "started"); err != nil {
		return fmt.Errorf("api.Create %s: %w", p.appName, err)
	}

	return nil
}

// growPool creates a new machine and adds it to the pool if the
// pool is not yet at capacity. It returns the created machine but
// does not add it to the free list.
func (p *FlyPool) growPool(ctx context.Context) (*Mach, error) {
	var nascent *Mach
	defer p.discardMach(nascent, "growPool failed")

	// Allocate the nascent machine under lock.
	p.mu.Lock()
	if len(p.machs) < p.capacity {
		name := newWorkerName(p.name)
		expire := p.now().Add(p.leaseTime)
		nascent = newMachNascent(p, name, expire)
		p.machs[nascent.Name] = nascent
	}
	p.mu.Unlock()

	if nascent == nil {
		log.Printf("pool: growPool: cant grow")
		return nil, nil
	}

	// Bring the nascent machine up, or discard it.
	if err := p.createMach(ctx, nascent); err != nil {
		log.Printf("pool: growPool: createMach failed: %v", err)
		return nil, err
	}

	mach := nascent
	nascent = nil
	return mach, nil
}

// getFreeImmediately returns the next free machine if there are any immediately available.
func (p *FlyPool) getFreeImmediately() *Mach {
	select {
	case mach := <-p.free:
		return mach
	default:
		return nil
	}
}

// waitForFree returns the next free machine, waiting for one if none is available.
func (p *FlyPool) waitForFree(ctx context.Context) (*Mach, error) {
	if p.isShutdown {
		return nil, ErrPoolClosed
	}

	select {
	case <-ctx.Done():
		log.Printf("pool: alloc: cancelled")
		return nil, ctx.Err()
	case mach := <-p.free:
		if mach == nil {
			log.Printf("pool: alloc: cancelled: pool closed")
			return nil, ErrPoolClosed
		}
		return mach, nil
	}
}

// allocLeased gets the next free machine that has enough lease time left,
// discarding any machines that do not have enough lease time left.
// It grows the pool automatically if there are no free machines immediately
// available and the pool is not yet at capacity.
func (p *FlyPool) allocLeased(ctx context.Context, waitForFree bool) (*Mach, error) {
	var err error
	for {
		mach, err := func() (*Mach, error) {
			mach := p.getFreeImmediately()
			if mach != nil {
				return mach, nil
			}

			mach, err = p.growPool(ctx)
			if err != nil {
				return nil, err
			}
			if mach != nil {
				return mach, nil
			}

			if waitForFree {
				return p.waitForFree(ctx)
			}

			return nil, nil
		}()
		if err != nil {
			return nil, err
		}

		if mach == nil {
			return nil, nil
		}

		if mach.leaseSufficient(p.workerTime) {
			return mach, nil
		}

		// if the lease is still good, extend it.
		if mach.leaseExpires.After(p.now()) {
			expire := p.now().Add(p.leaseTime)
			err = mach.updateLease(ctx, expire)
			if err == nil {
				return mach, nil
			}

			log.Printf("pool: alloc: extend lease failed: %v", err)
		}

		p.discardMach(mach, "not enough lease left")
		// and try again...
	}
}

// Alloc returns the next free machine, blocking if necessary.
func (p *FlyPool) Alloc(ctx context.Context, waitForFree bool) (*Mach, error) {
	dt := p.stats[statsAlloc].Start()
	defer dt.End()

	mach, err := p.allocLeased(ctx, waitForFree)
	if err != nil || mach == nil {
		return nil, err
	}

	if err := mach.start(ctx); err != nil {
		log.Printf("pool: mach.start: %v", err)
		p.discardMach(mach, "start machine failed")
		return nil, err
	}

	log.Printf("pool: alloc %s %s %s", p.appName, mach.Name, mach.Id)
	return mach, nil
}

// freeMach stops a machine and returns it to the pool.
// Freeing is done in a background context to stop machines as best as possible.
// This can block for a few seconds, but is safe to call as `go p.Free(mach)`.
func (p *FlyPool) freeMach(mach *Mach) {
	if p.isShutdown {
		return
	}
	log.Printf("pool: free %s %s %s", p.appName, mach.Name, mach.Id)

	// Don't make caller wait for the machine to stop,
	p.freeWg.Add(1)
	go func() {
		defer p.freeWg.Done()

		ctx := context.Background()
		if err := mach.stop(ctx); err != nil {
			log.Printf("pool free: stopMach %v %v: %v", mach.Name, mach.Id, err)
			p.discardMach(mach, "stop machine failed")
			return
		}

		//log.Printf("pool free: stopMach %v %v: done", mach.Name, mach.Id)
		p.free <- mach
	}()
}

// discardMach asynchronously destroys the machine or fails silently.
func (p *FlyPool) discardMach(mach *Mach, msg string) {
	if mach == nil {
		return
	}

	log.Printf("pool: discard machine %v %v: %v", mach.Name, mach.Id, msg)

	p.mu.Lock()
	delete(p.machs, mach.Name)
	p.mu.Unlock()

	p.discards <- mach
}

// handleDiscards handles discarded machines asynchronously with
// a minimal-effort attempt at destroying the machine.
// Failures here will be caught by this or another pool's cleanOrphans cleaner.
func (p *FlyPool) handleDiscards(ctx context.Context) {
	log.Printf("pool: handleDiscards: started")
	for mach := range p.discards {
		if err := mach.destroy(ctx); err != nil {
			log.Printf("pool: handleDiscards: %v", err)
			// but continue...
		}

		// stagger the cleanup a little if there are several machines to discard...
		if err := sleepWithContext(ctx, 100*time.Millisecond); err != nil {
			break
		}
	}
	log.Printf("pool: handleDiscards: exiting")
	p.wg.Done()
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
	poolMach := p.machs[m.Name]
	p.mu.Unlock()

	alreadyInOurPool := poolMach != nil
	poolNameFromName, _ := parseWorkerName(m.Name)
	ours := m.Config.Metadata[MetaPoolKey] == p.metadata && poolNameFromName == p.name
	createdAt, _ := time.Parse(time.RFC3339, m.CreatedAt)
	age := p.now().Sub(createdAt)
	probablyExpired := age > p.leaseTime
	log.Printf("pool: clean: mach %v %v: age=%v, ours=%v inpool=%v", m.Name, m.Id, age, ours, alreadyInOurPool)

	if alreadyInOurPool {
		// TODO: extend leases here for machines with a lease that is running low?

		if !poolMach.leaseSufficient(0) {
			// Destroy it, but leave it in our pool and free queue.
			// It will get discarded when someone tries to allocate it.
			log.Printf("pool: clean: mach %v %v: age=%v, destroying, ours", m.Name, m.Id, age)
			poolMach.destroy(ctx)
		}
		return 0
	}

	if !ours {
		if probablyExpired {
			// Try to destroy it. This wont work if it has a valid lease since we don't provide the lease nonce.
			log.Printf("pool: clean: mach %v %v: age=%v, destroying, not ours", m.Name, m.Id, age)
			p.api.Destroy(ctx, p.appName, m.Id, true)
		}
		return 0
	}

	lease, err := p.getLease(ctx, m.Id)
	if err != nil {
		log.Printf("pool: clean: mach %v %v: destroying, error getting lease: %v", m.Name, m.Id, err)
		p.api.Destroy(ctx, p.appName, m.Id, true)
		return 0
	}

	leaseExpires := time.Unix(lease.ExpiresAt, 0)
	mach := newMachFromFly(p, m, lease.Nonce, leaseExpires)

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
		log.Printf("pool: clean: mach %v %v: destroying: %v", m.Name, m.Id, err)
		mach.destroy(ctx)
		return 0
	} else {
		return 1
	}
}

func (p *FlyPool) showStats() {
	for k, stat := range p.stats {
		log.Printf("pool: stats: %s: %+v", k, stat.Stats())
	}
}

func (p *FlyPool) clean(ctx context.Context) {
	log.Printf("pool: clean: starting")
	for {
		p.showStats()

		log.Printf("pool: cleaning")
		ms, err := p.api.List(ctx, p.appName, japi.ReqQuery("region", p.machRegion))
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
	log.Printf("pool: clean: exiting")
	p.wg.Done()
}
