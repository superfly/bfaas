package main

import (
	"log"
	"os"
	"time"

	"github.com/superfly/coordBfaas/basher"
)

func main() {
	pubKey := os.Getenv("PUBLIC")
	machId := os.Getenv("FLY_MACHINE_ID")
	if machId == "" || pubKey == "" {
		log.Fatalf("need PUBLIC and FLY_MACHINE_ID")
	}

	srv, err := basher.New(8001, machId, pubKey)
	if err != nil {
		log.Fatalf("basher.New: %v", err)
	}

	if err := basher.RunWithSignals(srv.Server, time.Second); err != nil {
		log.Fatalf("RunWithSignals: %v", err)
	}
}
