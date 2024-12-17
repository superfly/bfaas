package stats

import (
	"math"
	"sync"
	"time"
)

// Collector incrementally collects count, average, variance, and standard deviation
// via the Add() method using Welford's algorithm.
// Reference: https://en.wikipedia.org/wiki/Algorithms_for_calculating_variance#Welford's_online_algorithm
type Collector struct {
	mu        sync.Mutex
	Count     float64
	Min       float64
	Max       float64
	Avg       float64
	meanDist2 float64
}

// New returns a new statistics collector.
func New() *Collector {
	return &Collector{
		Min: math.Inf(1),
		Max: math.Inf(-1),
	}
}

// Add accumulates `x` into the collected statistics.
func (p *Collector) Add(x float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.Count += 1.0
	if x < p.Min {
		p.Min = x
	}
	if x > p.Max {
		p.Max = x
	}
	delta := x - p.Avg
	p.Avg += delta / p.Count
	delta2 := x - p.Avg
	p.meanDist2 += delta * delta2
}

type Stats struct {
	Count  int     `json:"count"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Avg    float64 `json:"avg"`
	Var    float64 `json:"var"`
	StdDev float64 `json:"stddev"`
}

// Stats processes the collected statistics and returns it.
func (p *Collector) Stats() *Stats {
	p.mu.Lock()
	defer p.mu.Unlock()

	avg := p.Avg
	if p.Count == 0 {
		avg /= p.Count // we want NaN
	}

	v := p.meanDist2 / p.Count
	return &Stats{
		Count:  int(p.Count),
		Min:    p.Min,
		Max:    p.Max,
		Avg:    avg,
		Var:    v,
		StdDev: math.Sqrt(v),
	}
}

type Timer struct {
	c     *Collector
	start time.Time
}

// Start starts a duration measurement.
func (p *Collector) Start() Timer {
	return Timer{p, time.Now()}
}

// End finishes a duration measurement and adds the number of seconds into the collected statistics.
func (p *Timer) End() {
	dt := time.Now().Sub(p.start)
	p.c.Add(dt.Seconds())
}
