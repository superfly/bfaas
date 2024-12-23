package japi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
)

// HttpError is an error indicating a status code that does not indicate success.
// It captures the StatusCode and body of the response.
type HttpError struct {
	url        string
	StatusCode int
	Body       string
}

func (p *HttpError) Error() string {
	return fmt.Sprintf("%s: client.Do: status %d (%q)", p.url, p.StatusCode, p.Body)
}

// ErrorIsStatus returns true if err indicates an HTTP response with statusCode.
func ErrorIsStatus(err error, statusCode int) bool {
	httpErr, ok := err.(*HttpError)
	return ok && httpErr.StatusCode == statusCode
}

// Req encodes the parameters needed to perform a single HTTP request.
type Req struct {
	client  *http.Client
	baseUrl string

	method   string
	path     string
	header   http.Header
	qs       url.Values
	reqBody  interface{}
	respBody interface{}
	okCodes  []int
}

type ReqOpt func(*Req)

// Req builds a new request object using default values configured for the JsonApi.
func (p *Api) Req(method string, opts ...ReqOpt) *Req {
	n := &Req{
		client:  p.client,
		baseUrl: p.url,

		method:  method,
		header:  maps.Clone(p.header),
		qs:      make(url.Values),
		okCodes: []int{http.StatusOK},
	}

	n.ApplyOpts(opts...)
	return n
}

func (p *Req) ApplyOpts(opts ...ReqOpt) {
	for _, opt := range opts {
		opt(p)
	}
}

// ReqPath sets the URL path for the request.
func ReqPath(pathFmt string, a ...interface{}) ReqOpt {
	return func(p *Req) { p.path = fmt.Sprintf(pathFmt, a...) }
}

// ReqHeader adds a header which will be sent in the request.
func ReqHeader(k, v string) ReqOpt {
	return func(p *Req) { p.header.Add(k, v) }
}

// ReqQuery adds a query key and value which will be encoded in the request URL.
func ReqQuery(k, v string) ReqOpt {
	return func(p *Req) { p.qs.Set(k, v) }
}

// ReqBody sets the request body to encode and deliver as JSON.
func ReqBody(x interface{}) ReqOpt {
	return func(p *Req) { p.reqBody = x }
}

// ReqRespBody sets the response body to parse JSON response bodies into.
func ReqRespBody(x interface{}) ReqOpt {
	return func(p *Req) { p.respBody = x }
}

// ReqOkCodes sets the list of http status codes that indicate success.
func OkCodes(codes ...int) ReqOpt {
	return func(p *Req) { p.okCodes = codes }
}

// Do performs the request, returning any errors.
// If the request has a response body and there are no errors,
// the response's body is parsed into it.
func (p *Req) Do(ctx context.Context) error {
	url := p.baseUrl + p.path
	fullUrl := url
	if len(p.qs) > 0 {
		fullUrl = fullUrl + "?" + p.qs.Encode()
	}

	var body io.Reader
	if p.reqBody != nil {
		buf := bytes.NewBuffer(nil)
		if err := json.NewEncoder(buf).Encode(p.reqBody); err != nil {
			return fmt.Errorf("%s: encode request: %w", url, err)
		}
		body = buf
	}

	req, err := http.NewRequestWithContext(ctx, p.method, fullUrl, body)
	if err != nil {
		return fmt.Errorf("%s: NewRequestWithContext: %w", url, err)
	}

	req.Header = p.header
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: client.Do: %w", url, err)
	}

	ok := slices.Contains(p.okCodes, resp.StatusCode)
	if !ok {
		bs, _ := io.ReadAll(resp.Body)
		var body string
		if bs != nil {
			body = string(bs)
		}
		return &HttpError{url, resp.StatusCode, body}
	}

	if p.respBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(p.respBody); err != nil {
			return fmt.Errorf("%s: parse response: %w", url, err)
		}
	}

	return nil
}
