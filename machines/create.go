package machines

import (
	"context"

	"github.com/superfly/coordBfaas/japi"
)

type CreateMachineReq struct {
	Config MachineConfig `json:"config"`
}

type MachineConfig struct {
	Init        Init    `json:"init"`
	Image       string  `json:"image"`
	AutoDestroy bool    `json:"auto_destroy"`
	Restart     Restart `json:"restart"`
	Guest       Guest   `json:"guest"`
}

type Init struct {
	Exec []string `json:"exec"`
}

type Restart struct {
	Policy string `json:"policy"`
}

type Guest struct {
	CpuKind  string `json:"cpu_kind"`
	Cpus     int    `json:"cpus"`
	MemoryMb int    `json:"memory_mb"`
}

type MachineResp struct {
	Id     string `json:"id"`
	Name   string `json:"name"`
	State  string `json:"state"`
	Region string `json:"region"`
	// ImageRef
	InstanceId string `json:"instance_id"`
	PrivateIp  string `json:"private_ip"`
	// CreatedAt, UpdatedAt
	Config MachineConfig `json:"config"`
	// Events
}

func (p *Api) Create(ctx context.Context, appName string, config *MachineConfig, opts ...ReqOpt) (*MachineResp, error) {
	req := &CreateMachineReq{*config}
	var resp MachineResp
	r := p.json.Req("POST", japi.ReqPath("/v1/apps/%s/machines", appName), japi.ReqBody(req), japi.ReqRespBody(&resp))
	r.ApplyOpts(opts...)
	if err := r.Do(ctx); err != nil {
		return nil, err
	}

	return &resp, nil
}
