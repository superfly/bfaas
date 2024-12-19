package main

import (
	"log"
	"os"
	"time"

	"github.com/superfly/coordBfaas/basher"
)

func main() {
	machId := os.Getenv("FLY_MACHINE_ID")
	if machId == "" {
		log.Fatalf("need FLY_MACHINE_ID")
	}

	srv, err := basher.New(8001, machId)
	if err != nil {
		log.Fatalf("basher.New: %v", err)
	}

	if err := basher.RunWithSignals(srv.Server, time.Second); err != nil {
		log.Fatalf("RunWithSignals: %v", err)
	}
}
