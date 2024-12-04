package pool

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"

	"github.com/superfly/coordBfaas/machines"
)

var machConfig = machines.MachineConfig{
	Init: machines.Init{
		Exec: []string{"/bin/sleep", "inf"},
	},
	Image: "registry-1.docker.io/library/ubuntu:latest",
	Restart: machines.Restart{
		Policy: "no",
	},
	Guest: machines.Guest{
		CpuKind:  "shared",
		Cpus:     1,
		MemoryMb: 256,
	},
}

var createReq = &machines.CreateMachineReq{
	Config: machConfig,
	Region: "qmx",
}

func getTestApi(t *testing.T) (appName string, api *machines.Api) {
	appName = os.Getenv("APPNAME")
	token := os.Getenv("FLY_API_TOKEN_WORKER")
	if appName == "" || token == "" {
		t.Skip("requires env: APPNAME, FLY_API_TOKEN_WORKER")
	}

	internal := os.Getenv("FLY_PUBLIC_IP") != ""
	if internal {
		api = machines.NewInternal(token)
	} else {
		api = machines.NewPublic(token)
	}
	return
}

func TestPool(t *testing.T) {
	poolName := "TestPool"
	appName, api := getTestApi(t)
	log.Printf("create pool")
	pool, err := New(api, poolName, appName, createReq, Size(2), WorkerTime(time.Minute), LeaseTime(5*time.Minute))
	assert.NoError(t, err)

	ctx := context.Background()
	m, err := pool.Alloc(ctx)
	assert.NoError(t, err)
	log.Printf("allocated %+v", m)

	m.Free()

	m, err = pool.Alloc(ctx)
	assert.NoError(t, err)
	log.Printf("allocated %+v", m)

	m.Free()

	log.Printf("close pool")
	err = pool.Close()
	assert.NoError(t, err)

	log.Printf("recreate pool")
	pool, err = New(api, poolName, appName, createReq, Size(2), WorkerTime(time.Minute), LeaseTime(5*time.Minute))
	assert.NoError(t, err)

	m, err = pool.Alloc(ctx)
	assert.NoError(t, err)
	log.Printf("allocated %+v", m)

	m.Free()

	log.Printf("destroy pool")
	err = pool.Destroy()
	assert.NoError(t, err)
}
