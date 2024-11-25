package basher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

type Handler func(w http.ResponseWriter, r *http.Request)

func (s *Server) withAuth(next Handler) Handler {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if err := s.verifier(time.Now(), auth); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

func (s *Server) withOnce(next Handler) Handler {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.used.Swap(true) {
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
	bs, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	cmd := exec.Command("/bin/bash", "-c", string(bs))

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
			bs, _ := json.Marshal(s)

			mu.Lock()
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(bs))
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
		log.Printf("command exit %v", err)

		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	wg.Wait()
	fmt.Fprintf(w, "event: exit\ndata: {\"code\":%d}\n\n", exitCode)
}
