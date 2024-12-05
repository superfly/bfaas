package machines

import (
	"context"

	"github.com/superfly/coordBfaas/japi"
)

type CreateMachineReq struct {
	Config     MachineConfig `json:"config"`
	Region     string        `json:"region"`
	Name       string        `json:"name"`
	SkipLaunch bool          `json:"skip_launch"`
	LeaseTTL   int           `json:"lease_ttl"`
}

type MachineConfig struct {
	Init        Init              `json:"init"`
	Metadata    map[string]string `json:"metadata"`
	Services    []Service         `json:"services"`
	Image       string            `json:"image"`
	AutoDestroy bool              `json:"auto_destroy"`
	Restart     Restart           `json:"restart"`
	Guest       Guest             `json:"guest"`
}

type Init struct {
	Exec []string `json:"exec"`
}

type Service struct {
	Protocol     string `json:"protocol"`
	InternalPort int    `json:"internal_port"`
	//Autostop           string `json:"autostop"` // off/stop/suspend
	Autostop           bool   `json:"autostop"` // TODO: support string or off/stop/suspend
	Autostart          bool   `json:"autostart"`
	MinMachinesRunning int    `json:"min_machines_running"`
	Ports              []Port `json:"ports,omitempty"`
	// todo: checks, concurrency, force_instance_key, force_instance_description
}

type Port struct {
	Port int `json:"port,omitempty" toml:"port,omitempty"`
	// todo: start_port, end_port,
	Handlers   []string `json:"handlers,omitempty" toml:"handlers,omitempty"`
	ForceHTTPS bool     `json:"force_https,omitempty" toml:"force_https,omitempty"`
	// todo: tls_options, http_options, proxy_proto_options
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
	CreatedAt  string `json:"created_at"`
	// UpdatedAt
	Config MachineConfig `json:"config"`
	// Events

	Nonce string `json:"nonce"`
}

func (p *Api) Create(ctx context.Context, appName string, req *CreateMachineReq, opts ...ReqOpt) (*MachineResp, error) {
	var resp MachineResp
	r := p.json.Req("POST", japi.ReqPath("/v1/apps/%s/machines", appName), japi.ReqBody(req), japi.ReqRespBody(&resp))
	r.ApplyOpts(opts...)
	if err := r.Do(ctx); err != nil {
		return nil, err
	}

	return &resp, nil
}
