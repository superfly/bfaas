package coord

import (
	"fmt"
	"net/http"
	"time"

	"github.com/superfly/coordBfaas/auth"
	"github.com/superfly/coordBfaas/machines/pool"
)

type Server struct {
	*http.Server
	signer     auth.Signer
	maxReqTime time.Duration
	pool       pool.Pool
}

func New(pool pool.Pool, port int, privKey string, maxReqTime time.Duration) (*Server, error) {
	signer, err := auth.NewSigner(privKey)
	if err != nil {
		return nil, fmt.Errorf("building signer: %w", err)
	}

	server := &Server{
		pool:       pool,
		signer:     signer,
		maxReqTime: maxReqTime,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /run", server.handleRun)

	server.Server = &http.Server{
		Addr:        fmt.Sprintf(":%d", port),
		ReadTimeout: 10 * time.Second,
		// Not setting write timeout, but we're managing request times.
		//WriteTimeout:   maxReqTime,
		MaxHeaderBytes: 4096,
		Handler:        mux,
	}
	return server, nil
}
