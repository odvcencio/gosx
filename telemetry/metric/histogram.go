package metric

import (
	"math"
	"sort"
	"sync"
)

type HistogramVec struct{ family *family }

// Histogram updates bucket, count, and sum under one lock. Snapshot readers
// use that same lock and release it before formatting or application callbacks.
type Histogram struct {
	cell   *cell
	mu     sync.Mutex
	bounds []float64
	counts []uint64
	count  uint64
	sum    float64
}

func (v *HistogramVec) Bind(values ...string) (*Histogram, error) {
	var f *family
	if v != nil {
		f = v.family
	}
	c, err := bind(f, values)
	if c == nil || err != nil {
		return nil, err
	}
	return c.histogram, nil
}

func (v *HistogramVec) Declare(values ...string) error {
	if v == nil || v.family == nil {
		return nil
	}
	return (&Registry{state: v.family.registry}).DeclareBatch([]TupleDeclaration{{v, values}})
}

func (h *Histogram) Observe(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return invalid("metric.value", "not_finite")
	}
	if h == nil || h.cell == nil {
		return nil
	}
	i := sort.SearchFloat64s(h.bounds, value)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count == ^uint64(0) || math.IsInf(h.sum+value, 0) {
		return ErrCapacity
	}
	h.counts[i]++
	h.count++
	h.sum += value
	return nil
}
