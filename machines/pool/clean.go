package pool

import (
	"context"
	"fmt"
	"log"

	"github.com/superfly/coordBfaas/machines"
)

// getExpired removes items from the free list that
// don't have enough lease time left to perform useful work,
// and returns a list of those machines.
func (p *Pool) getExpired() []*Mach {
	p.mu.Lock()
	defer p.mu.Unlock()

	var free []*Mach
	var expired []*Mach
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
func (p *Pool) getLeases(ctx context.Context) (map[string]*machines.LeaseResp, error) {
	machs, err := p.machines.List(ctx, p.appName)
	if err != nil {
		return nil, fmt.Errorf("machines.List: %w", err)
	}

	resp := make(map[string]*machines.LeaseResp)
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
