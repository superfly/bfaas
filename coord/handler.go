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

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	worker, err := s.machApi.Start()
	if err != nil {
		log.Printf("machApi.Start: %v", err)
		http.Error(w, "create worker failed", http.StatusInternalServerError)
		return
	}
	defer worker.Stop()

	ctx, _ := context.WithTimeout(r.Context(), s.maxReqTime)
	url := fmt.Sprintf("http://%s/run", worker.Info().Addr)
	workReq, err := http.NewRequestWithContext(ctx, "POST", url, r.Body)
	if err != nil {
		log.Printf("NewRequestWithContext: %v", err)
		http.Error(w, "create worker request failed", http.StatusInternalServerError)
		return
	}

	auth := s.signer(time.Now(), worker.Info().Id)
	workReq.Header.Set("Authorization", auth)
	log.Printf("making request for %v", s.maxReqTime)
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
