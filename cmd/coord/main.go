package main

import (
	"log"
	"os"
	"time"

	"github.com/superfly/coordBfaas/coord"
	"github.com/superfly/coordBfaas/machine"
)

func main() {
	workerApp := os.Getenv("WORKER_APP")
	workerImage := os.Getenv("WORKER_IMAGE")
	flyAuth := os.Getenv("FLY_TOKEN")
	machId := os.Getenv("FLY_MACHINE_ID")
	privKey := os.Getenv("PRIVATE")
	s := os.Getenv("MAXREQTIME")
	if workerApp == "" || workerImage == "" || flyAuth == "" || machId == "" || privKey == "" || s == "" {
		log.Fatalf("need: WORKER_APP, WORKER_IMAGE, FLY_TOKEN, FLY_MACHINE_ID, PRIVATE, MAXREQTIME")
	}

	var machApi machine.Api
	if workerApp == "mock" {
		machApi = machine.NewMock("go", "run", "cmd/basher/main.go")
	} else {
		exec := []string{"/app/basher"}
		machApi = machine.New(flyAuth, workerApp, workerImage, exec)
	}

	maxReqTime, err := time.ParseDuration(s)
	if err != nil {
		log.Fatalf("MAXREQTIME: %v", err)
	}

	srv, err := coord.New(8000, privKey, maxReqTime, machApi)
	if err != nil {
		log.Fatalf("coord.New: %v", err)
	}

	if err := coord.RunWithSignals(srv.Server, time.Second); err != nil {
		log.Fatalf("RunWithSignals: %v", err)
	}
}
