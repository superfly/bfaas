package main

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/superfly/coordBfaas/coord"
	"github.com/superfly/coordBfaas/machines"
	"github.com/superfly/coordBfaas/machines/pool"
)

func main() {
	log.Printf("starting coord")

	// Get settings from env.
	workerApp := os.Getenv("WORKER_APP")
	workerImage := os.Getenv("WORKER_IMAGE")
	flyAuth := os.Getenv("FLY_TOKEN")
	reqTimeStr := os.Getenv("MAXREQTIME")
	region := os.Getenv("FLY_REGION")
	machId := os.Getenv("FLY_MACHINE_ID")
	poolSizeStr := os.Getenv("POOLSIZE")
	flyReplay := os.Getenv("FLY_REPLAY") != ""

	log.Printf("checking args")
	switch workerApp {
	case "mock":
		if reqTimeStr == "" {
			log.Fatalf("need: MAXREQTIME")
		}
	default:
		if workerApp == "" || workerImage == "" || flyAuth == "" || reqTimeStr == "" || machId == "" || poolSizeStr == "" {
			log.Fatalf("need: WORKER_APP, WORKER_IMAGE, FLY_TOKEN, MAXREQTIME, FLY_MACHINE_ID, POOLSIZE")
		}
	}

	if region == "" {
		region = "qmx"
	}

	maxReqTime, err := time.ParseDuration(reqTimeStr)
	if err != nil {
		log.Fatalf("MAXREQTIME: %v", err)
	}

	poolSize, err := strconv.Atoi(poolSizeStr)
	if err != nil {
		log.Fatalf("POOLSIZE: %v", err)
	}

	log.Printf("starting pool")

	// Make worker pool.
	var p pool.Pool
	if workerApp == "mock" {
		log.Printf("using mock pool")
		p = pool.NewMock("go", "run", "cmd/basher/main.go")
	} else {
		log.Printf("using fly pool")
		api := machines.NewInternal(flyAuth)
		var err error
		p, err = pool.New(api, machId, workerApp, workerImage,
			pool.Port(8001), pool.Size(poolSize), pool.Region(region),
			pool.WorkerTime(2*maxReqTime), pool.LeaseTime(5*time.Minute))
		if err != nil {
			log.Fatalf("pool.New: %v", err)
		}
	}
	defer p.Close()

	log.Printf("building coord")
	srv, err := coord.New(p, 8000, maxReqTime, flyReplay)
	if err != nil {
		log.Fatalf("coord.New: %v", err)
	}

	log.Printf("running server")
	if err := coord.RunWithSignals(srv.Server, time.Second); err != nil {
		log.Fatalf("RunWithSignals: %v", err)
	}
}
