package pool

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/superfly/coordBfaas/japi"
	"github.com/superfly/coordBfaas/machines"
)

const startRetryTimes = 4

var startRetryWait = 50 * time.Millisecond

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
	Name       string

	// Free stops a machine and returns it to the pool.
	// This can block for a few seconds, but is safe to call as `go mach.Free()`.
	// It is an error to free a machine after the pool is closed and it may
	// result in a panic.
	Free func()

	pool         *FlyPool
	leaseNonce   string
	leaseExpires time.Time
	state        string
}

// newMachNascent makes a pre-created Mach. Caller must fill in Id, leaseNonce, and InstanceId once started.
func newMachNascent(p *FlyPool, name string, leaseExpires time.Time) *Mach {
	m := &Mach{
		Url:        fmt.Sprintf("http://%s.flycast", p.appName),
		Id:         "",
		Name:       name,
		InstanceId: "",

		pool:         p,
		leaseExpires: leaseExpires,
		leaseNonce:   "",
		state:        "nascent",
	}
	m.Free = func() { p.freeMach(m) }
	return m
}

// newMachFromFly makes a Mach from a fly machine listing.
func newMachFromFly(p *FlyPool, flym *machines.MachineResp, leaseNonce string, leaseExpires time.Time) *Mach {
	m := &Mach{
		Url:        fmt.Sprintf("http://%s.flycast", p.appName),
		Id:         flym.Id,
		Name:       flym.Name,
		InstanceId: flym.InstanceId,

		pool:         p,
		leaseExpires: leaseExpires,
		leaseNonce:   leaseNonce,
		state:        flym.State,
	}
	m.Free = func() { p.freeMach(m) }
	return m
}

func (mach *Mach) waitFor(ctx context.Context, state string) error {
	if mach.Id == "" {
		return fmt.Errorf("pool: waitFor %s %s %s: cant wait for nascent machine", mach.pool.appName, mach.Name, state)
	}

	log.Printf("pool: wait for %s %s %s %s", mach.pool.appName, mach.Name, mach.Id, state)
	nonceOpt := machines.LeaseNonce(mach.leaseNonce)
	ok, err := mach.pool.api.WaitFor(ctx, mach.pool.appName, mach.Id, mach.InstanceId, 60*time.Second, state, nonceOpt)
	err = checkOk(ok, err)
	if err != nil {
		log.Printf("pool: wait for %s %s %s %s: %v", mach.pool.appName, mach.Name, mach.Id, state, err)
		return fmt.Errorf("api.WaitFor %s %s %v: %w", mach.Name, mach.Id, state, err)
	}
	log.Printf("pool: wait for %s %s %s %s: done", mach.pool.appName, mach.Name, mach.Id, state)
	mach.state = state
	return nil
}

func (mach *Mach) start(ctx context.Context) error {
	if mach.Id == "" {
		return fmt.Errorf("pool: start %s %s: cant start nascent machine", mach.pool.appName, mach.Name)
	}

	log.Printf("pool: start %s %s %s", mach.pool.appName, mach.Name, mach.Id)
	if mach.state == "started" {
		return nil
	}

	dt := mach.pool.stats[statsStart].Start()
	defer dt.End()

	// Retry on 412 PreconditionFailed, which indicates that the machine is not fully stopped yet.
	var err error
	nonceOpt := machines.LeaseNonce(mach.leaseNonce)
	wait := startRetryWait
	for times := 0; times < startRetryTimes; times += 1 {
		_, err = mach.pool.api.Start(ctx, mach.pool.appName, mach.Id, nonceOpt)
		if !japi.ErrorIsStatus(err, http.StatusPreconditionFailed) {
			break
		}

		log.Printf("pool: start %s %s %s: %s, retrying", mach.pool.appName, mach.Name, mach.Id, err)
		time.Sleep(wait)
		wait = 2 * wait
	}
	if err != nil {
		return fmt.Errorf("api.Start %s %s: %w", mach.Name, mach.Id, err)
	}

	if err := mach.waitFor(ctx, "started"); err != nil {
		return err
	}
	log.Printf("pool: start %s %s %s: done", mach.pool.appName, mach.Name, mach.Id)
	return nil
}

func (mach *Mach) stop(ctx context.Context) error {
	if mach.Id == "" {
		return fmt.Errorf("pool: stop %s %s: cant stop nascent machine", mach.pool.appName, mach.Name)
	}

	if mach.state == "stopped" {
		return nil
	}

	dt := mach.pool.stats[statsStop].Start()
	defer dt.End()

	log.Printf("pool: stop %s %s %s", mach.pool.appName, mach.Name, mach.Id)
	nonceOpt := machines.LeaseNonce(mach.leaseNonce)
	_, err := mach.pool.api.Stop(ctx, mach.pool.appName, mach.Id, nonceOpt)
	if err != nil {
		return fmt.Errorf("api.Stop %s %s: %w", mach.Name, mach.Id, err)
	}

	if err := mach.waitFor(ctx, "stopped"); err != nil {
		return err
	}
	log.Printf("pool: stop %s %s %s: done", mach.pool.appName, mach.Name, mach.Id)
	return nil
}

func (mach *Mach) destroy(ctx context.Context) error {
	if mach.Id == "" {
		return nil
	}

	dt := mach.pool.stats[statsDestroy].Start()
	defer dt.End()

	log.Printf("pool: destroy %s %s %s", mach.pool.appName, mach.Name, mach.Id)
	mach.state = "destroyed"
	nonceOpt := machines.LeaseNonce(mach.leaseNonce)
	ok, err := mach.pool.api.Destroy(ctx, mach.pool.appName, mach.Id, true, nonceOpt)
	err = checkOk(ok, err)
	if err != nil {
		return fmt.Errorf("api.Destroy %s %s: %w", mach.Id, mach.Name, err)
	}
	return nil
}

func (mach *Mach) updateLease(ctx context.Context, exp time.Time) error {
	dt := mach.pool.stats[statsLease].Start()
	defer dt.End()

	log.Printf("pool: updateLease %s %s %s", mach.pool.appName, mach.Name, mach.Id)
	ttl := int(exp.Sub(mach.pool.now()).Seconds())
	nonceOpt := machines.LeaseNonce(mach.leaseNonce)
	lease, err := mach.pool.api.Lease(ctx, mach.pool.appName, mach.Id, &machines.LeaseReq{Ttl: ttl}, nonceOpt)
	if err != nil {
		return fmt.Errorf("api.Lease %s %s: %w", mach.Id, mach.Name, err)
	}
	if lease.Status != "success" {
		return fmt.Errorf("api.Lease %s %s: bad status %s", mach.Id, mach.Name, lease.Status)
	}

	mach.leaseExpires = exp
	return nil
}

// leaseSufficient returns true if the lease has at least dt time left.
func (mach *Mach) leaseSufficient(dt time.Duration) bool {
	needUntil := mach.pool.now().Add(dt)
	return mach.leaseExpires.After(needUntil)
}
