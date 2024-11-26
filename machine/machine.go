package machine

import (
	"context"
	"fmt"
	"net/http"

	"github.com/superfly/coordBfaas/japi"
)

type opts struct {
	client *http.Client
	url    string
	auth   string
}

var defOpts = opts{
	client: &http.Client{},
	url:    "http://_api.internal:4280",
}

type Opt func(*opts)

func OptClient(client *http.Client) Opt {
	return func(o *opts) { o.client = client }
}

func OptUrl(url string) Opt {
	return func(o *opts) { o.url = url }
}

type MachineApi struct {
	appName  string
	image    string
	exec     []string
	machines MachinesApi
}

func New(auth, appName, image string, exec []string, opts ...Opt) Api {
	o := defOpts
	for _, opt := range opts {
		opt(&o)
	}

	jsonApi := japi.New(o.url, japi.Client(o.client), japi.Header("Authorization", auth))
	return &MachineApi{
		appName:  appName,
		image:    image,
		exec:     exec,
		machines: MachinesApi{jsonApi},
	}
}

// XXX TODO
// Currently this creates and starts a new machine for each request.
// Ideally we would have some pool of created machines and be able to
// enumerate them, and start/stop them as needed instead of always
// creating new machines.
// We also need some mechanism to clean up any machines that could not
// be destroyed because an API call failed or a coordinator was
// forcefully shutdown.
// Ideally we would be able to enumerate all machines, figure out which
// ones were "too old", and stop or destroy them.

func (p *MachineApi) Start(ctx context.Context) (Machine, error) {
	// TODO: make more of these configurable options.
	m, err := p.machines.Create(ctx, p.appName, &CreateMachineReq{
		Config: MachineConfig{
			Init: Init{
				Exec: p.exec,
			},
			Image:       p.image,
			AutoDestroy: false,
			Restart: Restart{
				Policy: "never",
			},
			Guest: Guest{
				CpuKind:  "shared",
				Cpus:     1,
				MemoryMb: 256,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("machines.Create: %w", err)
	}

	if _, err := p.machines.Start(ctx, p.appName, m.Id); err != nil {
		// Use bg context to try our best to destroy the machine.
		p.machines.Destroy(context.Background(), p.appName, m.Id, true)
		return nil, fmt.Errorf("machines..Start: %w", err)
	}

	return &FlyMachine{p, m.Id, m.PrivateIp}, nil
}

type FlyMachine struct {
	parent *MachineApi
	id     string
	addr   string
}

func (p *FlyMachine) Info() MachineInfo {
	return MachineInfo{
		Id:   p.id,
		Addr: p.addr,
	}
}

func (p *FlyMachine) Stop(ctx context.Context) error {
	ok, err := p.parent.machines.Destroy(ctx, p.parent.appName, p.id, true)
	if err != nil {
		return fmt.Errorf("machines.Destroy: %w", err)
	}
	if !ok {
		return fmt.Errorf("machines.Destroy failed")
	}
	return nil
}
