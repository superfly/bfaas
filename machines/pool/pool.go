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
	appName   string
	createReq *machines.CreateMachineReq

	now func() time.Time

	retryDelay time.Duration
	createCtx  context.Context
	shutdown   bool

	mu     sync.Mutex
	nextId uint64
	machs  map[string]*Mach
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

// NewPool creates a new machine pool of up to size machines owned by this pool.
// Name should be a unique name for the pool, such as the pool machine name.
// Pool creation can be slow.
func NewPool(api *machines.Api, name string, size int, appName string, createReq *machines.CreateMachineReq, opts ...Opt) (*Pool, error) {
	p := &Pool{
		api:       api,
		name:      name,
		appName:   appName,
		createReq: createReq,

		retryDelay: 10 * time.Second,
		createCtx:  context.Background(),

		nextId: rand.Uint64(), // note: not security sensitive
		machs:  make(map[string]*Mach),
		free:   make(chan *Mach, size),
		broken: make(chan *Mach, size),
	}

	for _, opt := range opts {
		opt(p)
	}

	// TODO: This will be slow...
	// think about creating pool machines in a background thread.
	// This will require thinking more about handling pool errors gracefully.
	// as the errors will happen after the pool is partially created.
	for i := 0; i < size; i++ {
		if err := p.addMach(p.createCtx); err != nil {
			cerr := p.Close()
			return nil, errors.Join(err, cerr)
		}
	}

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
		ok, derr := p.api.Destroy(ctx, p.appName, mach.fly.Id, true)
		derr = checkOk(ok, derr)
		if derr != nil {
			err = errors.Join(err, fmt.Errorf("api.Destory: %w", derr))
		}
	}
	return err
}

// addMach creates a machine and adds it to the pool's free list.
func (p *Pool) addMach(ctx context.Context) error {
	p.mu.Lock()
	id := p.nextId
	p.nextId += 1
	p.mu.Unlock()

	req := *p.createReq
	req.Name = fmt.Sprintf("worker-%s-%d", p.name, id)
	flym, err := p.api.Create(ctx, p.appName, &req)
	if err != nil {
		return fmt.Errorf("api.Create: %w", err)
	}

	mach := &Mach{
		pool: p,
		fly:  flym,
	}

	p.mu.Lock()
	p.machs[mach.fly.Id] = mach
	p.free <- mach
	return nil
}

// Alloc returns the next free machine, blocking if necessary.
func (p *Pool) Alloc(ctx context.Context) (*Mach, error) {
	var mach *Mach
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case mach = <-p.free:
		if mach == nil {
			return nil, ErrPoolClosed
		}
		// continue with m...
	}

	if p.shutdown {
		return nil, ErrPoolClosed
	}

	_, err := p.api.Start(ctx, p.appName, mach.fly.Id)
	if err != nil {
		go p.Free(mach)
		return nil, fmt.Errorf("api.Start %s: %w", mach.fly.Id, err)
	}

	if p.shutdown {
		return nil, ErrPoolClosed
	}

	ok, err := p.api.WaitFor(ctx, p.appName, mach.fly.Id, mach.fly.InstanceId, 10*time.Second, "started")
	err = checkOk(ok, err)
	if err != nil {
		go p.Free(mach)
		return nil, fmt.Errorf("api.WaitFor started %v: %w", mach.fly.Id, err)
	}

	return mach, nil
}

// Free stops a machine and returns it to the pool.
// Freeing is done in a background context to stop machines as best as possible.
// This can block for a few seconds, but is safe to call as `go p.Free(mach)`.
func (p *Pool) Free(mach *Mach) {
	if p.shutdown {
		return
	}

	ctx := context.Background()
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
	log.Printf("broken machine %v: %v", mach.fly.Id, brokenType)
	mach.broken = brokenType
	mach.retryTime = p.now().Add(p.retryDelay)
	if !p.shutdown {
		// XXX can panic if p.broken closed, race condition.
		p.broken <- mach
	}
}

func (p *Pool) handleBroken() {
	// TODO: contexts and timeouts for this stuff?
	for mach := range p.broken {
		dt := p.now().Sub(mach.retryTime)
		if dt > 0 {
			log.Printf("handleBroken %v sleeping %v", mach.fly.Id, dt)
			time.Sleep(dt)
		}

		log.Printf("handleBroken %v was %v, try again", mach.fly.Id, mach.broken)

		// TODO: smarter handling. if it keeps failing maybe give up, destroy the thing
		// and create a new one?
		// but perhaps these kinds of failures are best handled by manual intervention.
		p.Free(mach)

		// stagger the cleanup a little...
		time.Sleep(100 * time.Millisecond)
	}
}
