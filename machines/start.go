package machines

import (
	"context"

	"github.com/superfly/coordBfaas/japi"
)

type StartMachineResp struct {
	PreviousState string `json:"previous_state"`
	Migrated      bool   `json:"migrated"`
	NewHost       string `json:"new_host"`
}

func (p *Api) Start(ctx context.Context, appName, machId string) (*StartMachineResp, error) {
	var resp StartMachineResp
	r := p.json.Req("POST", japi.ReqPath("/v1/apps/%s/machines/%s/start", appName, machId), japi.ReqRespBody(&resp))
	if err := r.Do(ctx); err != nil {
		return nil, err
	}
	return &resp, nil
}
