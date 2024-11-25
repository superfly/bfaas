package basher

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/superfly/coordBfaas/auth"
)

type Server struct {
	*http.Server
	verifier auth.Verifier
	used     atomic.Bool
}

func New(port int, machId string, pubKey string) (*Server, error) {
	verifier, err := auth.NewVerifier(pubKey, machId, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("building verifier: %w", err)
	}

	server := &Server{
		verifier: verifier,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /run", server.withAuth(server.withOnce(server.handleRun)))

	server.Server = &http.Server{
		// No timeouts set.
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
	return server, nil
}
