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

	fmt.Printf("public = %s\n", pub)
	fmt.Printf("private = %s\n", priv)
}
