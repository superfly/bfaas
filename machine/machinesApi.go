package machine

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const apiUrl = "http://_api.internal:4280" // XXX

var client = &http.Client{}

// MachinesApi provides a subset of the fly machines API.
type MachinesApi struct {
	JsonApi
}

var defaultMachinesApi = MachinesApi{
	JsonApi: JsonApi{
		client: client,
		apiUrl: apiUrl,
	},
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

type CreateMachineResp struct {
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

func (p *MachinesApi) Create(ctx context.Context, appName string, req *CreateMachineReq) (*CreateMachineResp, error) {
	var resp CreateMachineResp
	if err := p.Post(ctx, fmt.Sprintf("/v1/apps/%s/machines", appName), req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

type OkResp struct {
	Ok bool `json:"ok"`
}

func (p *MachinesApi) WaitFor(ctx context.Context, appName, machId, instanceId string, timeout time.Duration, state string) (bool, error) {
	qs := make(url.Values)
	qs.Set("instance_id", instanceId)
	qs.Set("timeout", fmt.Sprintf("%d", int(timeout.Seconds())))
	qs.Set("state", state)

	var resp OkResp
	if err := p.Get(ctx, fmt.Sprintf("/v1/apps/%s/machines/%s/wait", appName, machId), qs, &resp); err != nil {
		return false, err
	}
	return resp.Ok, nil
}

func (p *MachinesApi) Destroy(ctx context.Context, appName, machId string, force bool) (bool, error) {
	qs := make(url.Values)
	qs.Set("force", fmt.Sprintf("%v", force))

	var resp OkResp
	if err := p.Delete(ctx, fmt.Sprintf("/v1/apps/%s/machines/%s", appName, machId), qs, &resp); err != nil {
		return false, err
	}
	return resp.Ok, nil
}
