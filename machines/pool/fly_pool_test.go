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

func getTestApi(t *testing.T) (appName, image string, api *machines.Api) {
	image = os.Getenv("IMAGE")
	appName = os.Getenv("APPNAME")
	token := os.Getenv("FLY_API_TOKEN_WORKER")
	if appName == "" || token == "" || image == "" {
		t.Skip("requires env: APPNAME, FLY_API_TOKEN_WORKER, IMAGE")
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
	appName, image, api := getTestApi(t)
	log.Printf("create pool")
	pool, err := New(api, poolName, appName, image, Size(2), WorkerTime(time.Minute), LeaseTime(5*time.Minute), Region("qmx"), Port(8001))
	assert.NoError(t, err)

	ctx := context.Background()
	m, err := pool.Alloc(ctx, true)
	assert.NoError(t, err)
	log.Printf("allocated %+v", m)

	m.Free()

	m, err = pool.Alloc(ctx, true)
	assert.NoError(t, err)
	log.Printf("allocated %+v", m)

	m.Free()

	log.Printf("close pool")
	err = pool.Close()
	assert.NoError(t, err)

	log.Printf("recreate pool")
	pool, err = New(api, poolName, appName, image, Size(2), WorkerTime(time.Minute), LeaseTime(5*time.Minute), Region("qmx"), Port(8001))
	assert.NoError(t, err)

	m, err = pool.Alloc(ctx, true)
	assert.NoError(t, err)
	log.Printf("allocated %+v", m)

	m.Free()

	log.Printf("destroy pool")
	err = pool.Destroy()
	assert.NoError(t, err)
}
