package machine

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

// JsonApi provides an API for buildng and making JSON HTTP requests.
type JsonApi struct {
	url    string
	client *http.Client
	header http.Header
}

// NewJsonApi returns a new JsonApi object which encodes settings
// and default values used when constructing requests.
// The returned object can be further modified with chainable methods.
func NewJsonApi(url string) *JsonApi {
	return &JsonApi{
		url:    url,
		client: &http.Client{},
		header: make(http.Header),
	}
}

// WithClient sets an HTTP client to use when making requests.
func (p *JsonApi) WithClient(client *http.Client) *JsonApi {
	p.client = client
	return p
}

// AddHeader adds a header that will be included in all requests.
func (p *JsonApi) AddHeader(k, v string) *JsonApi {
	p.header.Add(k, v)
	return p
}

// AddHeader sets a header that will be included in all requests.
func (p *JsonApi) SetHeader(k, v string) *JsonApi {
	p.header.Set(k, v)
	return p
}

// JsonReq encodes the parameters needed to perform a single HTTP request.
type JsonReq struct {
	url      string
	client   *http.Client
	method   string
	header   http.Header
	qs       url.Values
	reqBody  interface{}
	respBody interface{}
	okCodes  []int
}

// Req builds a new request object using default values configured for the JsonApi.
// The returned value can be updated with chainable modifier methods.
func (p *JsonApi) Req(method, pathFmt string, a ...interface{}) *JsonReq {
	return &JsonReq{
		url:     p.url + fmt.Sprintf(pathFmt, a...),
		client:  p.client,
		method:  method,
		header:  maps.Clone(p.header),
		qs:      make(url.Values),
		okCodes: []int{http.StatusOK},
	}
}

// AddHeader adds a header which will be sent in the request.
func (p *JsonReq) AddHeader(k, v string) *JsonReq {
	p.header.Add(k, v)
	return p
}

// SetHeader sets a header which will be sent in the request.
func (p *JsonReq) SetHeader(k, v string) *JsonReq {
	p.header.Set(k, v)
	return p
}

// AddQuery adds a query key and value which will be encoded in the request URL.
func (p *JsonReq) AddQuery(k, v string) *JsonReq {
	p.qs.Set(k, v)
	return p
}

// ReqBody sets the request body to encode and deliver as JSON.
func (p *JsonReq) ReqBody(x interface{}) *JsonReq {
	p.reqBody = x
	return p
}

// RespBody sets the response body to parse JSON response bodies into.
func (p *JsonReq) RespBody(x interface{}) *JsonReq {
	p.respBody = x
	return p
}

// OkCodes sets the list of http status codes that indicate success.
func (p *JsonReq) OkCodes(codes ...int) *JsonReq {
	p.okCodes = codes
	return p
}

// Do performs the request, returning any errors.
// If the request has a response body and there are no errors,
// the response's body is parsed into it.
func (p *JsonReq) Do(ctx context.Context) error {
	url := p.url
	if len(p.qs) > 0 {
		url = url + "?" + p.qs.Encode()
	}

	var body io.Reader
	if p.reqBody != nil {
		buf := bytes.NewBuffer(nil)
		if err := json.NewEncoder(buf).Encode(p.reqBody); err != nil {
			return fmt.Errorf("%s: encode request: %w", p.url, err)
		}
		body = buf
	}

	req, err := http.NewRequestWithContext(ctx, p.method, url, body)
	if err != nil {
		return fmt.Errorf("%s: NewRequestWithContext: %w", p.url, err)
	}

	req.Header = p.header
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: client.Do: %w", p.url, err)
	}

	ok := slices.Contains(p.okCodes, resp.StatusCode)
	if !ok {
		bs, _ := io.ReadAll(resp.Body)
		var body string
		if bs != nil {
			body = string(bs)
		}
		return fmt.Errorf("%s: client.Do: status %d (%q)", p.url, resp.StatusCode, body)
	}

	if p.respBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(p.respBody); err != nil {
			return fmt.Errorf("%s: parse response: %w", p.url, err)
		}
	}

	return nil
}
