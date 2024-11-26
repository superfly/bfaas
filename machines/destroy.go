package machines

import (
	"context"
	"fmt"

	"github.com/superfly/coordBfaas/japi"
)

func (p *Api) Destroy(ctx context.Context, appName, machId string, force bool) (bool, error) {
	var resp OkResp
	r := p.json.Req("DELETE", japi.ReqPath("/v1/apps/%s/machines/%s", appName, machId),
		japi.ReqRespBody(&resp),
		japi.ReqQuery("force", fmt.Sprintf("%v", force)))
	if err := r.Do(ctx); err != nil {
		return false, err
	}
	return resp.Ok, nil
}
