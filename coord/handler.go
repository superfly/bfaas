package coord

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"syscall"
	"time"
)

var client = &http.Client{}

var retryTimes = 5
var retryDelay = 50 * time.Millisecond

// doWithRetry will retry a request several times if the connection is refused,
// giving the worker machine some time to start up its http server.
func doWithRetry(req *http.Request) (resp *http.Response, err error) {
	for i := 0; i < retryTimes; i += 1 {
		if i > 0 {
			log.Printf("%v: retrying", err)
			time.Sleep(retryDelay)
		}

		resp, err = client.Do(req)
		if err == nil || !errors.Is(err, syscall.ECONNREFUSED) {
			return
		}
	}
	return
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	worker, err := s.pool.Alloc(context.Background())
	if err != nil {
		log.Printf("pool.Alloc: %v", err)
		http.Error(w, "create worker failed", http.StatusInternalServerError)
		return
	}
	defer worker.Free()

	ctx, _ := context.WithTimeout(r.Context(), s.maxReqTime)
	url := fmt.Sprintf("%s/run", worker.Url)
	workReq, err := http.NewRequestWithContext(ctx, "POST", url, r.Body)
	if err != nil {
		log.Printf("NewRequestWithContext: %v", err)
		http.Error(w, "create worker request failed", http.StatusInternalServerError)
		return
	}

	auth := s.signer(time.Now(), worker.Id)
	workReq.Header.Set("Authorization", auth)
	log.Printf("making request for %v", s.maxReqTime)
	workResp, err := doWithRetry(workReq)
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
