package pool

import "context"

type Pool interface {
	// Close shuts down the pool and leaves workers running in the "stopped" state.
	// It is an error to try to Alloc or Free after the pool is closed.
	Close() error

	// Destroy shuts down the machine pool down and destroys all resources.
	// It is an error to try to Alloc or Free after the pool is destroyed.
	Destroy() error

	// Alloc allocates a machine from the pool. It blocks waiting for
	// a free machine if there are no free machines available.
	// It is an error to call Alloc after the pool has been closed.
	//
	// mach.Free returns a machine to the pool for use, unblocking a call
	// to alloc that is waiting for a free machine.
	// It is an error to call Free after the pool has been closed, and
	// could result in a panic.
	Alloc(ctx context.Context, waitForFree bool) (*Mach, error)
}
