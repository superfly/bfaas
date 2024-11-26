package main

import (
	"log"
	"os"
	"time"

	"github.com/superfly/coordBfaas/coord"
	"github.com/superfly/coordBfaas/machine"
)

func main() {
	machApi := machine.NewMock("go", "run", "cmd/basher/main.go")

	s := os.Getenv("MAXREQTIME")
	privKey := os.Getenv("PRIVATE")
	machId := os.Getenv("FLY_MACHINE_ID")
	if machId == "" || privKey == "" || s == "" {
		log.Fatalf("need MAXREQTIME, PRIVATE, and FLY_MACHINE_ID")
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
