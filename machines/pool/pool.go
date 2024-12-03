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

// TODO: think about logging options.

var ErrPoolClosed = fmt.Errorf("The Pool Is Closed")

type BrokenType string

const (
	BrokenOK             BrokenType = "NotBroken"
	BrokenStopFailed     BrokenType = "StopFailed"
	BrokenStopNotReached BrokenType = "StopNotReached"
)

// Mach is a machine in the pool.
type Mach struct {
	Url string
	Id  string

	pool Pool
	fly  *machines.MachineResp

	broken    BrokenType
	retryTime time.Time
}

// Free stops a machine and returns it to the pool.
// This can block for a few seconds, but is safe to call as `go mach.Free()`.
// It is an error to free a machine after the pool is closed and it may
// result in a panic.
func (mach *Mach) Free() {
	mach.pool.Free(mach)
}

// FlyPool is a pool of Fly machines.
type FlyPool struct {
	api       *machines.Api
	name      string
	size      int
	appName   string
	createReq *machines.CreateMachineReq
	machPort  int

	now func() time.Time // TODO: for mocking. do we really need this?

	retryDelay time.Duration
	createCtx  context.Context

	// shutdown is true once shutdown has started.
	// at this point all Machs are owned by the pool
	// shutdown, and messages should no longer be
	// queued on the free and broken channels.
	shutdown bool

	mu     sync.Mutex
	machs  []*Mach
	free   chan *Mach
	broken chan *Mach
}

var _ Pool = (*FlyPool)(nil)

type Opt func(*FlyPool)

func Context(ctx context.Context) Opt {
	return func(p *FlyPool) { p.createCtx = ctx }
}

func RetryDelay(delay time.Duration) Opt {
	return func(p *FlyPool) { p.retryDelay = delay }
}

func Size(size int) Opt {
	return func(p *FlyPool) { p.size = size }
}

func Port(port int) Opt {
	return func(p *FlyPool) { p.machPort = port }
}

// New creates a new machine pool of up to size machines owned by this pool.
// Name should be a unique name for the pool, such as the pool machine name.
// Pool creation can be slow.
func New(api *machines.Api, name string, appName string, createReq *machines.CreateMachineReq, opts ...Opt) (*FlyPool, error) {
	p := &FlyPool{
		name:      name,
		size:      2,
		appName:   appName,
		createReq: createReq,
		machPort:  8000,

		now: time.Now,

		api: api,

		retryDelay: 10 * time.Second,
		createCtx:  context.Background(),

		machs: make([]*Mach, 0),
	}

	for _, opt := range opts {
		opt(p)
	}

	// construct after p.size might be set by options.
	p.free = make(chan *Mach, p.size)
	p.broken = make(chan *Mach, p.size)

	// TODO: This will be slow...
	// think about creating pool machines in a background thread.
	// This will require thinking more about handling pool errors gracefully.
	// as the errors will happen after the pool is partially created.
	log.Printf("starting %d workers", p.size)
	for i := 0; i < p.size; i++ {
		if err := p.addMach(p.createCtx); err != nil {
			cerr := p.Close()
			return nil, errors.Join(err, cerr)
		}
	}
	log.Printf("%d workers started", p.size)

	go p.handleBroken()
	return p, nil
}

// checkOk transforms (ok, err) into err.
func checkOk(ok bool, err error) error {
	if err == nil && !ok {
		return fmt.Errorf("!ok")
	}
	return err
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
	close(p.broken)

	// Destroy all machines.
	ctx := context.Background()
	var err error
	for _, mach := range p.machs {
		log.Printf("pool: destroy %s %s", p.appName, mach.Id)
		ok, derr := p.api.Destroy(ctx, p.appName, mach.Id, true)
		derr = checkOk(ok, derr)
		if derr != nil {
			err = errors.Join(err, fmt.Errorf("api.Destory: %w", derr))
		}
	}
	return err
}

// addMach creates an unstarted machine and adds it to the pool's free list.
func (p *FlyPool) addMach(ctx context.Context) error {
	req := *p.createReq
	req.SkipLaunch = false
	req.Name = fmt.Sprintf("worker-%s-%d", p.name, rand.Uint64())
	log.Printf("pool: create %s %s", p.appName, req.Name)
	flym, err := p.api.Create(ctx, p.appName, &req)
	if err != nil {
		return fmt.Errorf("api.Create: %w", err)
	}

	mach := &Mach{
		Url:  fmt.Sprintf("http://[%s]:%d", flym.PrivateIp, p.machPort),
		Id:   flym.Id,
		pool: p,
		fly:  flym,
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.machs = append(p.machs, mach)
	p.free <- mach
	return nil
}

// Alloc returns the next free machine, blocking if necessary.
func (p *FlyPool) Alloc(ctx context.Context) (*Mach, error) {
	log.Printf("pool: alloc wait")
	if p.shutdown {
		return nil, ErrPoolClosed
	}

	var mach *Mach
	select {
	case <-ctx.Done():
		log.Printf("pool: alloc cancelled context")
		return nil, ctx.Err()
	case mach = <-p.free:
		if mach == nil {
			log.Printf("pool: alloc cancelled with closed pool")
			return nil, ErrPoolClosed
		}
		// continue with mach...
	}

	log.Printf("pool: alloc machine %s %s", p.appName, mach.Id)
	log.Printf("pool: start %s %s", p.appName, mach.Id)
	_, err := p.api.Start(ctx, p.appName, mach.Id)
	if err != nil {
		go p.Free(mach)
		return nil, fmt.Errorf("api.Start %s: %w", mach.Id, err)
	}

	log.Printf("pool: wait for %s %s started", p.appName, mach.Id)
	ok, err := p.api.WaitFor(ctx, p.appName, mach.Id, mach.fly.InstanceId, 10*time.Second, "started")
	err = checkOk(ok, err)
	if err != nil {
		go p.Free(mach)
		return nil, fmt.Errorf("api.WaitFor started %v: %w", mach.Id, err)
	}

	log.Printf("pool: alloc %s %s", p.appName, mach.Id)
	return mach, nil
}

// Free stops a machine and returns it to the pool.
// Freeing is done in a background context to stop machines as best as possible.
// This can block for a few seconds, but is safe to call as `go p.Free(mach)`.
func (p *FlyPool) Free(mach *Mach) {
	log.Printf("pool: free %s %s", p.appName, mach.Id)
	if p.shutdown {
		return
	}

	ctx := context.Background()
	log.Printf("pool: stop %s %s", p.appName, mach.Id)
	ok, err := p.api.Stop(ctx, p.appName, mach.Id)
	err = checkOk(ok, err)
	if err != nil {
		log.Printf("api.Stop %s: %v", mach.Id, err)
		p.setBroken(mach, BrokenStopFailed)
		return
	}

	log.Printf("pool: wait for %s %s stopped", p.appName, mach.Id)
	ok, err = p.api.WaitFor(ctx, p.appName, mach.Id, mach.fly.InstanceId, 10*time.Second, "stopped")
	err = checkOk(ok, err)
	if err != nil {
		log.Printf("api.Waitfor stopped %s: %v", mach.Id, err)
		p.setBroken(mach, BrokenStopNotReached)
		return
	}

	p.free <- mach
}

func (p *FlyPool) setBroken(mach *Mach, brokenType BrokenType) {
	log.Printf("pool: broken machine %v: %v", mach.Id, brokenType)
	if p.shutdown {
		return
	}

	mach.broken = brokenType
	mach.retryTime = p.now().Add(p.retryDelay)
	p.broken <- mach
}

func (p *FlyPool) handleBroken() {
	log.Printf("pool: handleBroken started")
	for mach := range p.broken {
		dt := p.now().Sub(mach.retryTime)
		if dt > 0 {
			log.Printf("pool: handleBroken %s %v sleeping %v", p.appName, mach.Id, dt)
			time.Sleep(dt)
		}

		log.Printf("pool: handleBroken %s %v was %v, try again", p.appName, mach.Id, mach.broken)

		// Try to free it again, which may return it to the broken list if freeing it fails.
		mach.Free()

		// stagger the cleanup a little if there are several broken machines...
		time.Sleep(100 * time.Millisecond)
	}
	log.Printf("pool: handleBroken done")
}
