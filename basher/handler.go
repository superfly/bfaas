package basher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
)

type Handler func(w http.ResponseWriter, r *http.Request)

func (s *Server) withOnce(next Handler) Handler {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.used.Swap(true) {
			log.Printf("basher: received second request")
			http.Error(w, "conflict", http.StatusConflict)
			return
		}

		defer func() {
			go s.Shutdown(context.Background())
		}()
		next(w, r)
	}
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("raw") != ""
	w.Header().Set("Worker", os.Getenv("FLY_MACHINE_ID"))
	if !raw {
		w.Header().Set("Content-Type", "text/event-stream")
	}

	bs, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	log.Printf("basher: running %q", string(bs))
	cmd := exec.CommandContext(r.Context(), "/bin/bash", "-c", string(bs))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "stdout failed", http.StatusInternalServerError)
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		http.Error(w, "stdout failed", http.StatusInternalServerError)
		return
	}

	flusher, canFlush := w.(http.Flusher)

	copier := func(event string, r io.ReadCloser) {
		defer r.Close()

		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if err != nil || n == 0 {
				break
			}
			s := string(buf[:n])

			log.Printf("basher: delivering %s %q", event, s)
			mu.Lock()
			if raw {
				fmt.Fprintf(w, "%s", s)
			} else {
				bs, _ := json.Marshal(s)
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(bs))
			}
			if canFlush {
				flusher.Flush()
			}
			mu.Unlock()
		}

		wg.Done()
	}

	wg.Add(2)
	go copier("stdout", stdout)
	go copier("stderr", stderr)

	var exitCode int
	if err := cmd.Run(); err != nil {
		log.Printf("basher: command exit %v", err)

		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	wg.Wait()
	log.Printf("basher: done with code %d", exitCode)
	if raw {
		fmt.Fprintf(w, "\nexit: %d\n", exitCode)
	} else {
		fmt.Fprintf(w, "event: exit\ndata: {\"code\":%d}\n\n", exitCode)
	}
}
