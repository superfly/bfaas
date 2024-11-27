package machines

import (
	"context"
	"net/http"

	"github.com/superfly/coordBfaas/japi"
)

type LeaseReq struct {
	Descr string `json:"description"`
	Ttl   int    `json:"ttl"`
}

type LeaseResp struct {
	Status string    `json:"status"`
	Data   LeaseData `json:"data"`
}

type LeaseData struct {
	Nonce     string `json:"nonce"`
	ExpiresAt int    `json:"expires_at"`
	Owner     string `json:"owner"`
	Descr     string `json:"description"`
	Version   string `json:"version"`
}

func (p *Api) Lease(ctx context.Context, appName, machId string, req *LeaseReq, opts ...ReqOpt) (*LeaseResp, error) {
	var resp LeaseResp
	r := p.json.Req("POST", japi.ReqPath("/v1/apps/%s/machines/%s/lease", appName, machId),
		japi.ReqBody(req),
		japi.ReqRespBody(&resp),
		japi.OkCodes(http.StatusOK, http.StatusCreated))
	r.ApplyOpts(opts...)
	if err := r.Do(ctx); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (p *Api) GetLease(ctx context.Context, appName, machId string, opts ...ReqOpt) (*LeaseResp, error) {
	var resp LeaseResp
	r := p.json.Req("GET",
		japi.ReqPath("/v1/apps/%s/machines/%s/lease", appName, machId),
		japi.ReqRespBody(&resp))
	r.ApplyOpts(opts...)
	if err := r.Do(ctx); err != nil {
		return nil, err
	}
	return &resp, nil
}
