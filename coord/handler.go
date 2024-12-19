package coord

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
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
		if n == 0 {
			log.Printf("coord: copyFlusher tot=%d n=%d err=%v", tot, n, err)
			return tot, err
		}

		log.Printf("coord: proxying %q", string(buf[:n]))
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
func doWithRetry(body []byte, req *http.Request) (resp *http.Response, err error) {
	delay := retryDelay
	for i := 0; i < retryTimes; i += 1 {
		if i > 0 {
			log.Printf("coord: %v: retrying after %v", err, delay)
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

func (s *Server) proxyToWorker(w http.ResponseWriter, r *http.Request) {
	defer func() {
		for k, v := range s.stats {
			log.Printf("coord: proxyToWorker: stats %s: %+v", k, v.Stats())
		}
	}()

	dtReq := s.stats[statsRequest].Start()
	defer dtReq.End()

	w.Header().Set("Coord", os.Getenv("FLY_MACHINE_ID"))

	// We need the body multiple times, read it into memory.
	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		http.Error(w, "read body failed", http.StatusInternalServerError)
		return
	}
	log.Printf("coord: proxyToWorker %v %q", r.Header, string(body))

	replayMeta := r.Header.Get("fly-replay-src")
	retriesRemaining := 1

	// meta has the retries
	if replayMeta != "" {
		log.Printf("coord: replay meta: %v", replayMeta)
		matches := regexp.MustCompile(`state=retries-(-?\d+)$`).FindStringSubmatch(replayMeta)
		if matches != nil {
			r, err := strconv.Atoi(matches[0])
			if err != nil {
				retriesRemaining = r
			}
		}
	}
	waitForMachine := retriesRemaining <= 0
	worker, err := s.pool.Alloc(context.Background(), waitForMachine)
	if err != nil {
		log.Printf("coord: pool.Alloc: %v", err)
		http.Error(w, "create worker failed", http.StatusInternalServerError)
		return
	}

	if worker == nil && retriesRemaining <= 0 {
		log.Printf("coord: no worker available, out of retries")
		http.Error(w, "no worker available", http.StatusServiceUnavailable)
		return
	}
	if worker == nil {
		retriesRemaining = retriesRemaining - 1
		// gotta replay
		log.Printf("coord: no worker available, fly-replay")
		//w.Header().Set("fly-replay", "elsewhere=true")

		//w.Header().Set("fly-replay", fmt.Sprintf("elsewhere=true;state=%d", retriesRemaining))
		w.Header().Set("fly-replay", fmt.Sprintf("elsewhere=true;state=retries-%d", retriesRemaining))
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("no worker available\n"))
		return
	}

	defer worker.Free()

	// Proxy request r to worker.Url with extra headers added.
	ctx, _ := context.WithTimeout(r.Context(), s.maxReqTime)
	method := r.Method
	url := fmt.Sprintf("%s%s", worker.Url, r.URL.Path)
	workReq, err := http.NewRequestWithContext(ctx, method, url, nil) // body filled in by doWithRetry
	if err != nil {
		log.Printf("coord: NewRequestWithContext: %v", err)
		http.Error(w, "create worker request failed", http.StatusInternalServerError)
		return
	}

	for k, v := range r.Header {
		workReq.Header[k] = v
	}
	workReq.Header.Set("fly-force-instance-id", worker.Id)
	workReq.URL.RawQuery = r.URL.RawQuery

	log.Printf("coord: making request for %v to worker %v: %v %v", s.maxReqTime, worker.Id, method, workReq.URL.String())
	dtProxy := s.stats[statsProxy].Start()
	workResp, err := doWithRetry(body, workReq)
	dtProxy.End()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			// Do not output anything, we may have already outputted some.
			log.Printf("coord: client.Do timed out")
			return
		}

		log.Printf("coord: client.Do: %v", err)
		http.Error(w, "make worker request failed", http.StatusInternalServerError)
		return
	}
	defer workResp.Body.Close()

	// proxy response workResp back to w.
	if id := workResp.Header.Get("worker"); id != worker.Id {
		log.Printf("coord: warning: request went to %v not %v", id, worker.Id)
	}

	for k, v := range workResp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(workResp.StatusCode)
	copyFlusher(w, workResp.Body)
	log.Printf("coord: finished proxying response")
	workResp.Body.Close()
}
