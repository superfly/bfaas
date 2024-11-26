package machine

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

type PoolMach struct {
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

	machines MachinesApi
	appName  string

	mu   sync.Mutex
	free []*PoolMach
	used map[string]*PoolMach
}

func NewPool(owner string, maxSize int, leaseTime time.Duration, reqTime time.Duration, machines MachinesApi, appName string) *Pool {
	// XXX TODO: bootstrap free pool with p.getLeases(ctx), picking up any machines owned by us.
	// or at least destroying them.
	return &Pool{
		owner:     owner,
		maxSize:   maxSize,
		leaseTime: leaseTime,
		nowFunc:   time.Now,
		machines:  machines,
		appName:   appName,
		used:      make(map[string]*PoolMach),
	}
}

func (p *Pool) allocFree() *PoolMach {
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

func (p *Pool) Alloc() (*PoolMach, error) {
	if m := p.allocFree(); m != nil {
		return m, nil
	}

	// TODO: if we're over capacity, block on a semaphore.
	// otherwise create a new machine, get a lease on it,
	// add it to the used list, and return it.
	return nil, fmt.Errorf("TODO")
}

// getExpired removes items from the free list that
// don't have enough lease time left to perform useful work,
// and returns a list of those machines.
func (p *Pool) getExpired() []*PoolMach {
	p.mu.Lock()
	defer p.mu.Unlock()

	var free []*PoolMach
	var expired []*PoolMach
	for _, m := range p.free {
		needTime := p.nowFunc().Add(p.reqTime)
		if m.leaseExpire.Before(needTime) {
			expired = append(expired, m)
		} else {
			free = append(free, m)
		}
	}
	p.free = free
	return expired
}

// cleanOwned cleans up machines we hold leases on.
func (p *Pool) cleanOwned(ctx context.Context) {
	for _, m := range p.getExpired() {
		ok, err := p.machines.Destroy(ctx, p.appName, m.id, true)
		if err != nil || !ok {
			log.Printf("pool.Clean machines.Destroy failed: ok=%v, err=%v", ok, err)
		}
	}
}

// getLeases returns all machines and leases for our appName.
func (p *Pool) getLeases(ctx context.Context) (map[string]*LeaseResp, error) {
	machs, err := p.machines.List(ctx, p.appName)
	if err != nil {
		return nil, fmt.Errorf("machines.List: %w", err)
	}

	resp := make(map[string]*LeaseResp)
	for _, m := range machs {
		lease, err := p.machines.GetLease(ctx, p.appName, m.Id)
		if err != nil {
			log.Printf("getLeases machines.GetLease failed: %v", err)
			continue
		}

		resp[m.Id] = lease
	}
	return resp, nil
}

// cleanUnowned cleans up machines we do not hold leases on.
func (p *Pool) cleanUnowned(ctx context.Context) {
	leases, err := p.getLeases(ctx)
	if err != nil {
		log.Printf("poolClean: %v", err)
		return
	}

	for machId, lease := range leases {
		// TODO: figure out how to tell if lease is expired.
		// lease.Status? lease.ExpiresAt?
		log.Printf("mach=%v lease=%+v", machId, lease)
	}
}

// Clean cleans up machines that are no longer needed.
func (p *Pool) Clean(ctx context.Context) {
	p.cleanOwned(ctx)
	p.cleanUnowned(ctx)
}
