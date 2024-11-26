package machine

import (
	"context"
	"fmt"
	"time"
)

// MachinesApi provides a subset of the fly machines API.
type MachinesApi struct {
	json *JsonApi
}

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

func (p *MachinesApi) Create(ctx context.Context, appName string, req *CreateMachineReq) (*MachineResp, error) {
	var resp MachineResp
	r := p.json.Req("POST", ReqPath("/v1/apps/%s/machines", appName), ReqBody(req), ReqRespBody(&resp))
	if err := r.Do(ctx); err != nil {
		return nil, err
	}

	return &resp, nil
}

type StartMachineResp struct {
	PreviousState string `json:"previous_state"`
	Migrated      bool   `json:"migrated"`
	NewHost       string `json:"new_host"`
}

func (p *MachinesApi) Start(ctx context.Context, appName, machId string) (*StartMachineResp, error) {
	var resp StartMachineResp
	r := p.json.Req("POST", ReqPath("/v1/apps/%s/machines/%s/start", appName, machId), ReqRespBody(&resp))
	if err := r.Do(ctx); err != nil {
		return nil, err
	}
	return &resp, nil
}

type OkResp struct {
	Ok bool `json:"ok"`
}

func (p *MachinesApi) WaitFor(ctx context.Context, appName, machId, instanceId string, timeout time.Duration, state string) (bool, error) {
	var resp OkResp
	r := p.json.Req("GET", ReqPath("/v1/apps/%s/machines/%s/wait", appName, machId),
		ReqRespBody(&resp),
		ReqQuery("instance_id", instanceId),
		ReqQuery("timeout", fmt.Sprintf("%d", int(timeout.Seconds()))),
		ReqQuery("state", state))
	if err := r.Do(ctx); err != nil {
		return false, err
	}
	return resp.Ok, nil
}

func (p *MachinesApi) Destroy(ctx context.Context, appName, machId string, force bool) (bool, error) {
	var resp OkResp
	r := p.json.Req("DELETE", ReqPath("/v1/apps/%s/machines/%s", appName, machId),
		ReqRespBody(&resp),
		ReqQuery("force", fmt.Sprintf("%v", force)))
	if err := r.Do(ctx); err != nil {
		return false, err
	}
	return resp.Ok, nil
}

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

// XXX TODO: need a way to pass lease nonce in headers in other requests... ugh. api getting complex.

func (p *MachinesApi) Lease(ctx context.Context, appName, machId string, req *LeaseReq) (*LeaseResp, error) {
	var resp LeaseResp
	r := p.json.Req("POST", ReqPath("/v1/apps/%s/machines/%s/lease", appName, machId),
		ReqBody(req),
		ReqRespBody(&resp))
	if err := r.Do(ctx); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (p *MachinesApi) GetLease(ctx context.Context, appName, machId string) (*LeaseResp, error) {
	var resp LeaseResp
	r := p.json.Req("GET", ReqPath("/v1/apps/%s/machines/%s/lease", appName, machId), ReqRespBody(&resp))
	if err := r.Do(ctx); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (p *MachinesApi) List(ctx context.Context, appName string) ([]MachineResp, error) {
	// TODO: do we want to support include_deleted, region, metadata.key query params?
	var resp []MachineResp
	r := p.json.Req("GET", ReqPath("/v1/apps/%s/machines", appName), ReqRespBody(&resp))
	if err := r.Do(ctx); err != nil {
		return nil, err
	}
	return resp, nil
}
