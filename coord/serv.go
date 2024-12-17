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
	rlim       *Limiter

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
		rlim:       newLimiter(6, 2, time.Minute), // 1 req/10s per ip src, initial burst of 2.
		stats: map[string]*stats.Collector{
			statsRequest: stats.New(),
			statsProxy:   stats.New(),
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /run", server.handleRun)
	mux.HandleFunc("GET /hdr", func(w http.ResponseWriter, r *http.Request) {
		for k, v := range r.Header {
			fmt.Fprintf(w, "hdr: %s=%s\n", k, v)
		}
	})

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
