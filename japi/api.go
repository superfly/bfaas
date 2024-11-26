package japi

import (
	"net/http"
)

// JsonApi provides an API for buildng and making JSON HTTP requests.
type JsonApi struct {
	url    string
	client *http.Client
	header http.Header
}

// NewJsonApi returns a new JsonApi object which encodes settings
// and default values used when constructing requests.
func New(url string, opts ...JsonApiOpt) *JsonApi {
	n := &JsonApi{
		url:    url,
		client: &http.Client{},
		header: make(http.Header),
	}

	for _, opt := range opts {
		opt(n)
	}
	return n
}

type JsonApiOpt func(*JsonApi)

// Client sets an HTTP client to use when making requests.
func Client(client *http.Client) JsonApiOpt {
	return func(p *JsonApi) { p.client = client }
}

// Header adds a header that will be included in all requests.
func Header(k, v string) JsonApiOpt {
	return func(p *JsonApi) { p.header.Add(k, v) }
}
