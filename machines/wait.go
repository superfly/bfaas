package machines

import (
	"context"
	"fmt"
	"time"

	"github.com/superfly/coordBfaas/japi"
)

type OkResp struct {
	Ok bool `json:"ok"`
}

func (p *Api) WaitFor(ctx context.Context, appName, machId, instanceId string, timeout time.Duration, state string, opts ...ReqOpt) (bool, error) {
	var resp OkResp
	r := p.json.Req("GET", japi.ReqPath("/v1/apps/%s/machines/%s/wait", appName, machId),
		japi.ReqRespBody(&resp),
		japi.ReqQuery("instance_id", instanceId),
		japi.ReqQuery("timeout", fmt.Sprintf("%d", int(timeout.Seconds()))),
		japi.ReqQuery("state", state))
	r.ApplyOpts(opts...)
	if err := r.Do(ctx); err != nil {
		return false, err
	}
	return resp.Ok, nil
}
