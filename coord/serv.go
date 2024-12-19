package coord

import (
	"fmt"
	"net/http"
	"time"

	"github.com/superfly/coordBfaas/auth"
	"github.com/superfly/coordBfaas/machines/pool"
	"github.com/superfly/coordBfaas/stats"
)

const (
	statsRequest = "request"
	statsProxy   = "proxy"
)

type Server struct {
	*http.Server
	signer     auth.Signer
	maxReqTime time.Duration
	pool       pool.Pool

	stats map[string]*stats.Collector
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
		stats: map[string]*stats.Collector{
			statsRequest: stats.New(),
			statsProxy:   stats.New(),
		},
	}

	server.Server = &http.Server{
		Addr:        fmt.Sprintf(":%d", port),
		ReadTimeout: 10 * time.Second,
		// Not setting write timeout, but we're managing request times.
		//WriteTimeout:   maxReqTime,
		MaxHeaderBytes: 4096,
		Handler:        http.HandlerFunc(server.proxyToWorker),
	}
	return server, nil
}
