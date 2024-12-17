package stats

import (
	"log"
	"math"
	"testing"

	"github.com/alecthomas/assert/v2"
)

func assertApprox(t *testing.T, x float64, y float64) {
	t.Helper()
	dt2 := (x - y) * (x - y)
	eps2 := 1e-9
	assert.True(t, dt2 < eps2)
}

func TestStats(t *testing.T) {
	vectors := [][]float64{
		[]float64{},
		[]float64{2},
		[]float64{1, 2, 3},
		[]float64{1, 2, 3, 4, 5},
		[]float64{5, 5, 5},
	}

	avg := func(xs []float64) float64 {
		sum := 0.0
		for _, x := range xs {
			sum += x
		}
		return sum / float64(len(xs))
	}
	vari := func(xs []float64) float64 {
		mean := avg(xs)
		v := 0.0
		for _, x := range xs {
			v += (x - mean) * (x - mean)
		}
		return v / float64(len(xs))
	}
	stddev := func(xs []float64) float64 {
		return math.Sqrt(vari(xs))
	}
	min := func(xs []float64) float64 {
		m := math.Inf(1)
		for _, x := range xs {
			if x < m {
				m = x
			}
		}
		return m
	}
	max := func(xs []float64) float64 {
		m := math.Inf(-1)
		for _, x := range xs {
			if x > m {
				m = x
			}
		}
		return m
	}

	for _, vector := range vectors {
		c := New()
		for _, x := range vector {
			c.Add(x)
		}
		st := c.Stats()

		log.Printf("vector %v", vector)
		log.Printf("%+v", st)
		log.Printf("avg=%f var=%f", avg(vector), vari(vector))
		assert.Equal(t, len(vector), st.Count)
		assert.Equal(t, min(vector), st.Min)
		assert.Equal(t, max(vector), st.Max)
		assert.Equal(t, math.IsNaN(avg(vector)), math.IsNaN(st.Avg))
		if !math.IsNaN(st.Avg) {
			assertApprox(t, avg(vector), st.Avg)
		}

		assert.Equal(t, math.IsNaN(vari(vector)), math.IsNaN(st.Var))
		if !math.IsNaN(st.Var) {
			assertApprox(t, vari(vector), st.Var)
			assertApprox(t, stddev(vector), st.StdDev)
		}
	}
}
