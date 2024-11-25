package main

import (
	"log"
	"os"
	"time"

	"github.com/superfly/coordBfaas/basher"
)

func main() {
	srv, err := basher.New(3333, "m3333", os.Getenv("PUBLIC"))
	if err != nil {
		log.Fatalf("basher.New: %v", err)
	}

	if err := basher.RunWithSignals(srv.Server, time.Second); err != nil {
		log.Fatalf("RunWithSignals: %v", err)
	}
}
