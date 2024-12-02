package main

import (
	"log"
	"os"
	"time"

	"github.com/superfly/coordBfaas/coord"
	"github.com/superfly/coordBfaas/machines"
	"github.com/superfly/coordBfaas/machines/pool"
)

func main() {
	// Get settings from env.
	workerApp := os.Getenv("WORKER_APP")
	workerImage := os.Getenv("WORKER_IMAGE")
	flyAuth := os.Getenv("FLY_TOKEN")
	machId := os.Getenv("FLY_MACHINE_ID")
	privKey := os.Getenv("PRIVATE")
	reqTimeStr := os.Getenv("MAXREQTIME")
	switch workerApp {
	case "mock":
		if privKey == "" || reqTimeStr == "" {
			log.Fatalf("need: PRIVATE, MAXREQTIME")
		}
	default:
		if workerApp == "" || workerImage == "" || flyAuth == "" || machId == "" || privKey == "" || reqTimeStr == "" {
			log.Fatalf("need: WORKER_APP, WORKER_IMAGE, FLY_TOKEN, FLY_MACHINE_ID, PRIVATE, MAXREQTIME")
		}
	}

	maxReqTime, err := time.ParseDuration(reqTimeStr)
	if err != nil {
		log.Fatalf("MAXREQTIME: %v", err)
	}

	// Make worker pool.
	var p pool.Pool
	if workerApp == "mock" {
		log.Printf("using mock pool")
		p = pool.NewMock("go", "run", "cmd/basher/main.go")
	} else {
		log.Printf("using fly pool")
		machConfig := machines.MachineConfig{
			Init: machines.Init{
				Exec: []string{"/app/basher"},
			},
			Image: workerImage,
			Restart: machines.Restart{
				Policy: "no",
			},
			Guest: machines.Guest{
				CpuKind:  "shared",
				Cpus:     1,
				MemoryMb: 256,
			},
		}
		createReq := &machines.CreateMachineReq{
			Config: machConfig,
			Region: "qmx",
		}

		api := machines.NewInternal(flyAuth)
		var err error
		p, err = pool.New(api, "workers", workerApp, createReq, pool.Size(2))
		if err != nil {
			log.Fatalf("pool.New: %v", err)
		}
	}
	defer p.Close()

	srv, err := coord.New(p, 8000, privKey, maxReqTime)
	if err != nil {
		log.Fatalf("coord.New: %v", err)
	}

	if err := coord.RunWithSignals(srv.Server, time.Second); err != nil {
		log.Fatalf("RunWithSignals: %v", err)
	}
}
