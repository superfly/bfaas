package machines

import (
	"github.com/superfly/coordBfaas/japi"
)

type ApiOpt = japi.ApiOpt
type ReqOpt = japi.ReqOpt

// MachinesApi provides a subset of the fly machines API.
type Api struct {
	json *japi.Api
}

// New returns a machines API for a specified url.
func New(token, url string, opts ...ApiOpt) *Api {
	j := japi.New(url, opts...)
	japi.Header("Authorization", token)(j)
	return &Api{j}
}

// NewPublic returns a machines API using the public url.
func NewPublic(token string, opts ...ApiOpt) *Api {
	return New(token, "https://api.machines.dev", opts...)
}

// NewInternal returns a machines API using the internal url.
func NewInternal(token string, opts ...ApiOpt) *Api {
	return New(token, "http://_api.internal:4280", opts...)
}

func LeaseNonce(nonce string) ReqOpt {
	return japi.ReqHeader("fly-machine-lease-nonce", nonce)
}
