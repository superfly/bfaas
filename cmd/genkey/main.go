package main

import (
	"fmt"

	"github.com/superfly/coordBfaas/auth"
)

func main() {
	pub, priv, err := auth.GenKeypair()
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	fmt.Printf("PUBLIC=%s\n", pub)
	fmt.Printf("PRIVATE=%s\n", priv)
}
