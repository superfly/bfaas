package basher

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

type Server struct {
	*http.Server
	used atomic.Bool
}

func New(port int, machId string) (*Server, error) {
	server := &Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /run", server.withOnce(server.handleRun))

	server.Server = &http.Server{
		// No timeouts set.
		// Only listen on IPv4. Workers cannot reach each other on IPv4,
		// but flycast requests from coordinator can reach IPv4.
		Addr:    fmt.Sprintf("0.0.0.0:%d", port),
		Handler: mux,
	}
	return server, nil
}
