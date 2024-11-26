package pool

import (
	"fmt"
	"sync"
	"time"

	"github.com/superfly/coordBfaas/machines"
)

// Mach is a machine in the pool.
type Mach struct {
	pool        *Pool
	id          string
	leaseExpire time.Time
}

// Pool is a pool of machines.
type Pool struct {
	owner     string
	maxSize   int
	leaseTime time.Duration
	reqTime   time.Duration
	nowFunc   func() time.Time // for mocking

	machines *machines.Api
	appName  string

	mu   sync.Mutex
	free []*Mach
	used map[string]*Mach
}

func NewPool(owner string, maxSize int, leaseTime time.Duration, reqTime time.Duration, machines *machines.Api, appName string) *Pool {
	// XXX TODO: bootstrap free pool with p.getLeases(ctx), picking up any machines owned by us.
	// or at least destroying them.
	return &Pool{
		owner:     owner,
		maxSize:   maxSize,
		leaseTime: leaseTime,
		nowFunc:   time.Now,
		machines:  machines,
		appName:   appName,
		used:      make(map[string]*Mach),
	}
}

func (p *Pool) allocFree() *Mach {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.free) == 0 {
		return nil
	}

	m := p.free[0]
	p.free = p.free[1:]
	p.used[m.id] = m
	return m
}

func (p *Pool) Alloc() (*Mach, error) {
	if m := p.allocFree(); m != nil {
		return m, nil
	}

	// TODO: if we're over capacity, block on a semaphore.
	// otherwise create a new machine, get a lease on it,
	// add it to the used list, and return it.
	return nil, fmt.Errorf("TODO")
}
