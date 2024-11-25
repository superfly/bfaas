package basher

import (
	"fmt"
	"net/http"
	"time"

	"github.com/superfly/coordBfaas/auth"
)

type Server struct {
	*http.Server
	verifier auth.Verifier
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if err := s.verifier(time.Now(), auth); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	fmt.Fprintf(w, "yay!\n")
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
	mux.HandleFunc("POST /run", server.handleRun)

	server.Server = &http.Server{
		// No timeouts set.
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
	return server, nil
}
