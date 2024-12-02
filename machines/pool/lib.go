package pool

import "context"

type Pool interface {
	// Close shuts down the machine pool down and destroys all resources.
	// It is an error to try to Alloc or Free after the pool is closed.
	Close() error

	// Alloc allocates a machine from the pool. It blocks waiting for
	// a free machine if there are no free machines available.
	// It is an error to call Alloc after the pool has been closed.
	Alloc(ctx context.Context) (*Mach, error)

	// Free returns a machien to the pool for use, unblocking a call
	// to alloc that is waiting for a free machine.
	// It is an error to call Free after the pool has been closed, and
	// could result in a panic.
	Free(*Mach)
}
