package pool

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/superfly/coordBfaas/machines"
)

// Mach is a machine in the pool.
// A machine is owned by a pool if it has an unexpired lease, and metadata
// `pool_id` that is the same as the pool's metadata.
//
// When a pool restarts with the same machine ID and the same worker image,
// its `pool_id` will be the same, and the pool can take ownership of any owned machines
// it finds.
type Mach struct {
	Url        string
	Id         string
	InstanceId string

	// Free stops a machine and returns it to the pool.
	// This can block for a few seconds, but is safe to call as `go mach.Free()`.
	// It is an error to free a machine after the pool is closed and it may
	// result in a panic.
	Free func()

	pool         *FlyPool
	leaseNonce   string
	leaseExpires time.Time
	started      bool
}

func newMach(p *FlyPool, flym *machines.MachineResp, leaseNonce string, leaseExpires time.Time, started bool) *Mach {
	m := &Mach{
		Url:        fmt.Sprintf("http://[%s]:%d", flym.PrivateIp, p.machPort),
		Id:         flym.Id,
		InstanceId: flym.InstanceId,

		pool:         p,
		leaseExpires: leaseExpires,
		leaseNonce:   leaseNonce,
		started:      started,
	}
	m.Free = func() { p.freeMach(m) }
	return m
}

func (mach *Mach) start(ctx context.Context) error {
	log.Printf("pool: start %s %s", mach.pool.appName, mach.Id)
	if mach.started {
		return nil
	}

	nonceOpt := machines.LeaseNonce(mach.leaseNonce)
	_, err := mach.pool.api.Start(ctx, mach.pool.appName, mach.Id, nonceOpt)
	if err != nil {
		return fmt.Errorf("api.Start %s: %w", mach.Id, err)
	}

	log.Printf("pool: wait for %s %s started", mach.pool.appName, mach.Id)
	ok, err := mach.pool.api.WaitFor(ctx, mach.pool.appName, mach.Id, mach.InstanceId, 10*time.Second, "started", nonceOpt)
	err = checkOk(ok, err)
	if err != nil {
		return fmt.Errorf("api.WaitFor started %v: %w", mach.Id, err)
	}
	mach.started = true
	return nil
}

func (mach *Mach) stop(ctx context.Context) error {
	if !mach.started {
		return nil
	}

	log.Printf("pool: stop %s %s", mach.pool.appName, mach.Id)
	mach.started = false
	nonceOpt := machines.LeaseNonce(mach.leaseNonce)
	_, err := mach.pool.api.Stop(ctx, mach.pool.appName, mach.Id, nonceOpt)
	if err != nil {
		return fmt.Errorf("api.Stop %s: %w", mach.Id, err)
	}

	log.Printf("pool: wait for %s %s stopped", mach.pool.appName, mach.Id)
	ok, err := mach.pool.api.WaitFor(ctx, mach.pool.appName, mach.Id, mach.InstanceId, 10*time.Second, "stopped", nonceOpt)
	err = checkOk(ok, err)
	if err != nil {
		return fmt.Errorf("api.WaitFor stopped %v: %w", mach.Id, err)
	}
	return nil
}

func (mach *Mach) destroy(ctx context.Context) error {
	log.Printf("pool: destroy %s %s", mach.pool.appName, mach.Id)
	nonceOpt := machines.LeaseNonce(mach.leaseNonce)
	ok, err := mach.pool.api.Destroy(ctx, mach.pool.appName, mach.Id, true, nonceOpt)
	err = checkOk(ok, err)
	if err != nil {
		return fmt.Errorf("api.Destroy %s: %w", mach.Id, err)
	}
	return nil
}

// leaseSufficient returns true if the lease has at least dt time left.
func (mach *Mach) leaseSufficient(dt time.Duration) bool {
	needUntil := mach.pool.now().Add(dt)
	return mach.leaseExpires.After(needUntil)
}
