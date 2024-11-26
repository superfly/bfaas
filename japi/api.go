package japi

import (
	"net/http"
)

// Api provides an API for buildng and making JSON HTTP requests.
type Api struct {
	url    string
	client *http.Client
	header http.Header
}

// NewApi returns a new Api object which encodes settings
// and default values used when constructing requests.
func New(url string, opts ...ApiOpt) *Api {
	n := &Api{
		url:    url,
		client: &http.Client{},
		header: make(http.Header),
	}

	for _, opt := range opts {
		opt(n)
	}
	return n
}

type ApiOpt func(*Api)

// Client sets an HTTP client to use when making requests.
func Client(client *http.Client) ApiOpt {
	return func(p *Api) { p.client = client }
}

// Header adds a header that will be included in all requests.
func Header(k, v string) ApiOpt {
	return func(p *Api) { p.header.Add(k, v) }
}
