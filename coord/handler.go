package coord

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

var client = &http.Client{}

type Worker struct {
	machId string
	url    string
	auth   string
}

// newWorker starts a new worker.
func (s *Server) newWorker() (*Worker, error) {
	// For now we fake it, and assume we have basher running on localhost:3333.
	// later we need to start a machine, get its machId and address, etc..
	machId := "m3333"
	auth := s.signer(time.Now(), machId)
	url := fmt.Sprintf("http://localhost:3333/run")

	log.Printf("start worker %v", machId)
	return &Worker{
		machId: machId,
		url:    url,
		auth:   auth,
	}, nil
}

func (w *Worker) Stop() error {
	// TODO: shut down the worker's machine.
	log.Printf("stop worker %v", w.machId)
	return nil
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	worker, err := s.newWorker()
	if err != nil {
		log.Printf("newWorker: %v", err)
		http.Error(w, "create worker failed", http.StatusInternalServerError)
		return
	}
	defer worker.Stop()

	ctx, _ := context.WithTimeout(r.Context(), s.maxReqTime)
	workReq, err := http.NewRequestWithContext(ctx, "POST", worker.url, r.Body)
	if err != nil {
		log.Printf("NewRequestWithContext: %v", err)
		http.Error(w, "create worker request failed", http.StatusInternalServerError)
		return
	}

	workReq.Header.Set("Authorization", worker.auth)
	workResp, err := client.Do(workReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			// Do not output anything, we may have already outputted some.
			log.Printf("client.Do timed out")
			return
		}

		log.Printf("client.Do: %v", err)
		http.Error(w, "make worker request failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(workResp.StatusCode)
	io.Copy(w, workResp.Body)
	workResp.Body.Close()
}
