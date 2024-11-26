package machines

import (
	"github.com/superfly/coordBfaas/japi"
)

// MachinesApi provides a subset of the fly machines API.
type Api struct {
	json *japi.JsonApi
}

// New returns a machines API for a specified url.
func New(url string, opts ...japi.JsonApiOpt) *Api {
	j := japi.New(url, opts...)
	return &Api{j}
}

// NewPublic returns a machines API using the public url.
func NewPublic(opts ...japi.JsonApiOpt) *Api {
	return New("https://api.machines.dev", opts...)
}

// NewInternal returns a machines API using the internal url.
func NewInternal(opts ...japi.JsonApiOpt) *Api {
	return New("http://_api.internal:4280", opts...)
}
