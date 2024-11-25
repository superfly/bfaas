package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/superfly/coordBfaas/auth"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s machid", os.Args[0])
	}
	machId := os.Args[1]

	signer, err := auth.NewSigner(os.Getenv("PRIVATE"))
	if err != nil {
		log.Fatalf("auth.NewSigner: %v", err)
	}

	fmt.Printf("%s\n", signer(time.Now(), machId))
}
