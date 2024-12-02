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

// TODO: Think about how to cleanup any machines that might be left over
// from previous pools that have since been terminated without proper cleanup.

// TODO: Think about how to allow graceful pool shutdown.

// TODO: Think about how to handle errors where a machine in the pool might be "bad"
// for some reason. Right now we allocate machines up front and never destroy them
// or add new machines. We might be better to keep a list of bad machines, try
// to periodically destroy them until they are destroyed, keep them out of rotation,
// and add new workers to the pool (carefully, to avoid an unbounded number of machines
// being created!)

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
	pool *Pool
	fly  *machines.MachineResp

	broken    BrokenType
	retryTime time.Time
}

// Free stops a machine and returns it to the pool.
// This can block for a few seconds, but is safe to call as `go mach.Free()`.
func (mach *Mach) Free() {
	mach.pool.Free(mach)
}

// Pool is a pool of machines.
type Pool struct {
	api       *machines.Api
	name      string
	size      int
	appName   string
	createReq *machines.CreateMachineReq

	now func() time.Time // TODO: for mocking. do we really need this?

	retryDelay time.Duration
	createCtx  context.Context

	// shutdown is true once shutdown has started.
	// at this point all Machs are owned by the pool
	// shutdown, and messages should no longer be
	// queued on the free and broken channels.
	shutdown bool

	mu     sync.Mutex
	machs  map[string]*Mach // TODO: could just be a list.
	free   chan *Mach
	broken chan *Mach
}

type Opt func(*Pool)

func Context(ctx context.Context) Opt {
	return func(p *Pool) { p.createCtx = ctx }
}

func RetryDelay(delay time.Duration) Opt {
	return func(p *Pool) { p.retryDelay = delay }
}

func Size(size int) Opt {
	return func(p *Pool) { p.size = size }
}

// NewPool creates a new machine pool of up to size machines owned by this pool.
// Name should be a unique name for the pool, such as the pool machine name.
// Pool creation can be slow.
func NewPool(api *machines.Api, name string, appName string, createReq *machines.CreateMachineReq, opts ...Opt) (*Pool, error) {
	p := &Pool{
		name:      name,
		size:      2,
		appName:   appName,
		createReq: createReq,

		api: api,

		retryDelay: 10 * time.Second,
		createCtx:  context.Background(),

		machs: make(map[string]*Mach),
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
func (p *Pool) Close() error {
	// TODO: there are still races to think about here.
	// Other code might have handle on in-use machs.
	// They may try to use the mach after close destroys it.
	// Think about how to avoid cascades of errors during shutdown...

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
		log.Printf("pool: destroy %s %s", p.appName, mach.fly.Id)
		ok, derr := p.api.Destroy(ctx, p.appName, mach.fly.Id, true)
		derr = checkOk(ok, derr)
		if derr != nil {
			err = errors.Join(err, fmt.Errorf("api.Destory: %w", derr))
		}
	}
	return err
}

// addMach creates an unstarted machine and adds it to the pool's free list.
func (p *Pool) addMach(ctx context.Context) error {
	req := *p.createReq
	req.SkipLaunch = false
	req.Name = fmt.Sprintf("worker-%s-%d", p.name, rand.Uint64())
	log.Printf("pool: create %s %s", p.appName, req.Name)
	flym, err := p.api.Create(ctx, p.appName, &req)
	if err != nil {
		return fmt.Errorf("api.Create: %w", err)
	}

	mach := &Mach{
		pool: p,
		fly:  flym,
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.machs[mach.fly.Id] = mach
	p.free <- mach
	return nil
}

// Alloc returns the next free machine, blocking if necessary.
func (p *Pool) Alloc(ctx context.Context) (*Mach, error) {
	log.Printf("pool: alloc wait")
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

	log.Printf("pool: alloc machine %s %s", p.appName, mach.fly.Id)
	if p.shutdown {
		return nil, ErrPoolClosed
	}

	log.Printf("pool: start %s %s", p.appName, mach.fly.Id)
	_, err := p.api.Start(ctx, p.appName, mach.fly.Id)
	if err != nil {
		go p.Free(mach)
		return nil, fmt.Errorf("api.Start %s: %w", mach.fly.Id, err)
	}

	if p.shutdown {
		return nil, ErrPoolClosed
	}

	log.Printf("pool: wait for %s %s started", p.appName, mach.fly.Id)
	ok, err := p.api.WaitFor(ctx, p.appName, mach.fly.Id, mach.fly.InstanceId, 10*time.Second, "started")
	err = checkOk(ok, err)
	if err != nil {
		go p.Free(mach)
		return nil, fmt.Errorf("api.WaitFor started %v: %w", mach.fly.Id, err)
	}

	log.Printf("pool: alloc %s %s", p.appName, mach.fly.Id)
	return mach, nil
}

// Free stops a machine and returns it to the pool.
// Freeing is done in a background context to stop machines as best as possible.
// This can block for a few seconds, but is safe to call as `go p.Free(mach)`.
func (p *Pool) Free(mach *Mach) {
	log.Printf("pool: free %s %s", p.appName, mach.fly.Id)
	if p.shutdown {
		return
	}

	ctx := context.Background()
	log.Printf("pool: stop %s %s", p.appName, mach.fly.Id)
	ok, err := p.api.Stop(ctx, p.appName, mach.fly.Id)
	err = checkOk(ok, err)
	if err != nil {
		log.Printf("api.Stop %s: %v", mach.fly.Id, err)
		p.setBroken(mach, BrokenStopFailed)
		return
	}

	if p.shutdown {
		return
	}

	log.Printf("pool: wait for %s %s stopped", p.appName, mach.fly.Id)
	ok, err = p.api.WaitFor(ctx, p.appName, mach.fly.Id, mach.fly.InstanceId, 10*time.Second, "stopped")
	err = checkOk(ok, err)
	if err != nil {
		log.Printf("api.Waitfor stopped %s: %v", mach.fly.Id, err)
		p.setBroken(mach, BrokenStopNotReached)
		return
	}

	if !p.shutdown {
		// XXX can panic if p.broken closed, race condition.
		p.free <- mach
	}
}

func (p *Pool) setBroken(mach *Mach, brokenType BrokenType) {
	log.Printf("pool: broken machine %v: %v", mach.fly.Id, brokenType)
	mach.broken = brokenType
	mach.retryTime = p.now().Add(p.retryDelay)
	if !p.shutdown {
		// XXX can panic if p.broken closed, race condition.
		p.broken <- mach
	}
}

func (p *Pool) handleBroken() {
	// TODO: contexts and timeouts for this stuff?
	log.Printf("pool: handleBroken started")
	for mach := range p.broken {
		dt := p.now().Sub(mach.retryTime)
		if dt > 0 {
			log.Printf("pool: handleBroken %s %v sleeping %v", p.appName, mach.fly.Id, dt)
			time.Sleep(dt)
		}

		log.Printf("pool: handleBroken %s %v was %v, try again", p.appName, mach.fly.Id, mach.broken)

		// TODO: smarter handling. if it keeps failing maybe give up, destroy the thing
		// and create a new one?
		// but perhaps these kinds of failures are best handled by manual intervention.
		p.Free(mach)

		// stagger the cleanup a little...
		time.Sleep(100 * time.Millisecond)
	}
	log.Printf("pool: handleBroken done")
}
