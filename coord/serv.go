package coord

import (
	"fmt"
	"net/http"
	"time"

	"github.com/superfly/coordBfaas/machines/pool"
	"github.com/superfly/coordBfaas/stats"
)

const (
	statsRequest = "request"
	statsProxy   = "proxy"
)

type Server struct {
	*http.Server
	maxReqTime time.Duration
	flyReplay  bool
	pool       pool.Pool

	stats map[string]*stats.Collector
}

func New(pool pool.Pool, port int, maxReqTime time.Duration, enableFlyReplay bool) (*Server, error) {
	server := &Server{
		pool:       pool,
		maxReqTime: maxReqTime,
		flyReplay:  enableFlyReplay,
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
