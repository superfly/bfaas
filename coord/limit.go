package coord

import (
	"log"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type limEntry struct {
	rlim *rate.Limiter
	exp  time.Time
}

// Limiter is a rate limiter keyed by a string.
type Limiter struct {
	r    rate.Limit
	b    int
	life time.Duration

	mu  sync.Mutex
	lim map[string]*limEntry
}

func newLimiter(r rate.Limit, b int, bucketLife time.Duration) *Limiter {
	lim := &Limiter{
		r:    r,
		b:    b,
		lim:  make(map[string]*limEntry),
		life: bucketLife,
	}

	return lim
}

func (p *Limiter) clean() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// this could be optimized...
	for k, lim := range p.lim {
		if lim.exp.Before(time.Now()) {
			delete(p.lim, k)
		}
	}
}

func (p *Limiter) ensure(k string) *limEntry {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.lim[k] == nil {
		p.lim[k] = &limEntry{
			rlim: rate.NewLimiter(p.r, p.b),
		}
	}
	l := p.lim[k]
	l.exp = time.Now().Add(p.life)
	return l
}

func (p *Limiter) Allow(k string) bool {
	ret := p.ensure(k).rlim.Allow()
	p.clean() // TODO: do slowly in cleaner thread.. XXX
	return ret
}

type Handler func(w http.ResponseWriter, req *http.Request)

func (p *Limiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		k := req.Header.Get("X-Forwarded-For")
		log.Printf("rate limit by %q", k)
		if !p.Allow(k) {
			http.Error(w, http.StatusText(429), http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, req)
	})
}
