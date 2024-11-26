package machine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

var NoReqBody interface{} = nil

type JsonApi struct {
	client *http.Client
	apiUrl string
	auth   string
}

type Errorf = func(format string, a ...any) error

func (p *JsonApi) makeUrl(path string) (string, Errorf) {
	url := fmt.Sprintf("%s%s", p.apiUrl, path)
	errorf := func(format string, a ...any) error {
		args := append([]interface{}{url}, a...)
		return fmt.Errorf("%s: "+format, args...)
	}
	return url, errorf
}

// newRequest makes a request to the API url with a method and body and path.
func (p *JsonApi) newRequest(ctx context.Context, method, path string, body io.Reader, qs url.Values) (*http.Request, Errorf, error) {
	url, errorf := p.makeUrl(path)
	full_url := url
	if len(qs) > 0 {
		full_url = url + "?" + qs.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, full_url, body)
	if err != nil {
		return nil, errorf, errorf("NewRequestWithContext: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.auth)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, errorf, nil
}

// do performs a request using the configured client and parses the response body into respData.
func (p *JsonApi) do(req *http.Request, errorf Errorf, respData interface{}) error {
	resp, err := p.client.Do(req)
	if err != nil {
		return errorf("client.Do: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		bs, _ := io.ReadAll(resp.Body)
		var body string
		if bs != nil {
			body = string(bs)
		}
		return errorf("client.Do: status %d (%q)", resp.StatusCode, body)
	}

	if err := json.NewDecoder(req.Body).Decode(respData); err != nil {
		return errorf("parse response: %w", err)
	}
	return nil
}

// get performs a GET request to path and parses the response body into resp.
func (p *JsonApi) Get(ctx context.Context, path string, qs url.Values, resp interface{}) error {
	req, errorf, err := p.newRequest(ctx, "GET", path, http.NoBody, qs)
	if err != nil {
		return err
	}

	return p.do(req, errorf, resp)
}

// del performs a DELETE request to path and parses the response body into resp.
func (p *JsonApi) Delete(ctx context.Context, path string, qs url.Values, resp interface{}) error {
	req, errorf, err := p.newRequest(ctx, "DELETE", path, http.NoBody, qs)
	if err != nil {
		return err
	}

	return p.do(req, errorf, resp)
}

// post performs a POST request to path using the json encoded reqBody and
// parses the response body into resp.
func (p *JsonApi) Post(ctx context.Context, path string, reqBody, resp interface{}) error {
	buf := bytes.NewBuffer(nil)
	if reqBody != nil {
		if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
			_, errorf := p.makeUrl(path)
			return errorf("encode request: %w", err)
		}
	}

	req, errorf, err := p.newRequest(ctx, "POST", path, buf, nil)
	if err != nil {
		return err
	}

	return p.do(req, errorf, resp)
}
