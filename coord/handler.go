package coord

import (
	"bytes"
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
var retryDelay = 20 * time.Millisecond

func copyFlusher(w http.ResponseWriter, r io.Reader) (int, error) {
	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 4096)
	tot := 0
	for {
		n, err := r.Read(buf)
		if err != nil || n == 0 {
			return tot, err
		}

		n, err = w.Write(buf[:n])
		tot += n
		if err != nil {
			return tot, err
		}
		if canFlush {
			flusher.Flush()
		}
	}
}

// doWithRetry will retry a request several times if the connection is refused,
// giving the worker machine some time to start up its http server.
// Since requests go through fly proxy, we treat ECONNRESET similarly to ECONNREFUSED.
func doWithRetry(req *http.Request) (resp *http.Response, err error) {
	// We need the body multiple times, read it into memory.
	body, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("error reading body: %v", err)
	}

	delay := retryDelay
	for i := 0; i < retryTimes; i += 1 {
		if i > 0 {
			log.Printf("%v: retrying after %v", err, delay)
			time.Sleep(delay)
			delay = 2 * delay
		}

		req.Body = io.NopCloser(bytes.NewBuffer(body))
		resp, err = client.Do(req)
		if err == nil || !(errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET)) {
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
	workReq.Header.Set("fly-force-instance-id", worker.Id)
	workReq.URL.RawQuery = r.URL.RawQuery

	log.Printf("making request for %v to %v", s.maxReqTime, url)
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
	defer workResp.Body.Close()

	for k, v := range workResp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(workResp.StatusCode)
	copyFlusher(w, workResp.Body)
	workResp.Body.Close()
}
