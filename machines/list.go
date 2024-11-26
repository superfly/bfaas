package machines

import (
	"context"

	"github.com/superfly/coordBfaas/japi"
)

func (p *Api) List(ctx context.Context, appName string) ([]MachineResp, error) {
	// TODO: do we want to support include_deleted, region, metadata.key query params?
	var resp []MachineResp
	r := p.json.Req("GET", japi.ReqPath("/v1/apps/%s/machines", appName), japi.ReqRespBody(&resp))
	if err := r.Do(ctx); err != nil {
		return nil, err
	}
	return resp, nil
}
