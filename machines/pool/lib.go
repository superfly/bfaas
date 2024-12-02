package pool

import "context"

type Pool interface {
	Close() error
	Alloc(ctx context.Context) (*Mach, error)
	Free(*Mach)
}
