package machines

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
)

var machConfig = MachineConfig{
	Init: Init{
		Exec: []string{"/bin/sleep", "inf"},
	},
	Image: "registry-1.docker.io/library/ubuntu:latest",
	Restart: Restart{
		Policy: "no",
	},
	Guest: Guest{
		CpuKind:  "shared",
		Cpus:     1,
		MemoryMb: 256,
	},
}

func findMach(machs []MachineResp, machId string) *MachineResp {
	for _, mach := range machs {
		if mach.Id == machId {
			return &mach
		}
	}
	return nil
}

// TestApi tests the API out by creating and managing machines.
// It requires APPNAME, FLY_API_TOKEN environment variables and
// the provided token must be capable of managing APPNAME.
// It creates and destroy machines and should be used with care.
// Run with `-v` if you want to be sure to know if it is skipped or not.
func TestApi(t *testing.T) {
	appName := os.Getenv("APPNAME")
	token := os.Getenv("FLY_API_TOKEN")
	if appName == "" || token == "" {
		t.Skip("requires env: APPNAME, FLY_API_TOKEN")
	}

	ctx := context.Background()
	api := NewPublic(token)
	//image := "registry-1.docker.io/library/ubuntu:latest"

	// Create
	log.Printf("start")
	mach, err := api.Create(ctx, appName, &machConfig)
	assert.NoError(t, err)
	log.Printf("created %v: %+v", mach.Id, mach)

	ok, err := api.WaitFor(ctx, appName, mach.Id, mach.InstanceId, 10*time.Second, "started")
	assert.NoError(t, err)
	log.Printf("waitfor started %s: %v", mach.Id, ok)
	assert.True(t, ok)

	// List
	machs, err := api.List(ctx, appName)
	assert.NoError(t, err)
	m := findMach(machs, mach.Id)
	assert.NotZero(t, m)
	assert.Equal(t, m.Id, mach.Id)
	assert.Equal(t, m.State, "started")

	// Stop and restart
	ok, err = api.Stop(ctx, appName, mach.Id)
	assert.NoError(t, err)
	log.Printf("stop %s: %v", mach.Id, ok)
	assert.True(t, ok)

	ok, err = api.WaitFor(ctx, appName, mach.Id, mach.InstanceId, 10*time.Second, "stopped")
	assert.NoError(t, err)
	log.Printf("waitfor stopped %s: %v", mach.Id, ok)
	assert.True(t, ok)

	startResp, err := api.Start(ctx, appName, mach.Id)
	assert.NoError(t, err)
	log.Printf("start %s: %+v", mach.Id, startResp)
	assert.True(t, ok)

	ok, err = api.WaitFor(ctx, appName, mach.Id, mach.InstanceId, 10*time.Second, "started")
	assert.NoError(t, err)
	log.Printf("waitfor started %s: %v", mach.Id, ok)
	assert.True(t, ok)

	// Lease
	lease, err := api.Lease(ctx, appName, mach.Id, &LeaseReq{Descr: "abc123", Ttl: 500})
	assert.NoError(t, err)
	log.Printf("lease %v: %+v", mach.Id, lease)
	assert.Equal(t, lease.Status, "success")
	assert.Equal(t, lease.Data.Descr, "abc123")
	nonce := lease.Data.Nonce

	// Destroy without lease fails
	_, err = api.Destroy(ctx, appName, mach.Id, true)
	assert.Error(t, err)

	// Destroy
	ok, err = api.Destroy(ctx, appName, mach.Id, true, LeaseNonce(nonce))
	assert.NoError(t, err)
	log.Printf("destroy %s: %v", mach.Id, ok)
	assert.True(t, ok)
}
