package metric

import (
	"errors"
	"math"
	"runtime"
	"slices"
	"sync"
	"testing"
)

func TestHistogramBoundsAndOverflow(t *testing.T) {
	for _, bounds := range [][]float64{{2, 1}, {1, 1}, {math.NaN()}, {math.Inf(1)}, make([]float64, 33)} {
		if _, err := (&Registry{}).NewHistogram(HistogramOptions{Name: "invalid", Bounds: bounds}); !errors.Is(err, ErrInvalidOptions) {
			t.Fatal(err)
		}
	}
	if _, err := (&Registry{}).NewHistogram(HistogramOptions{Name: "invalid", Labels: []Label{{Name: "le"}}}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	bounds := []float64{1, 3}
	v, err := (&Registry{}).NewHistogram(HistogramOptions{Name: "duration", Bounds: bounds})
	if err != nil {
		t.Fatal(err)
	}
	bounds[0] = 100
	h, _ := v.Bind()
	for _, value := range []float64{-1, 1, 2, 3, 4} {
		if err := h.Observe(value); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(h.counts, []uint64{2, 2, 1}) || h.count != 5 || h.sum != 9 {
		t.Fatalf("snapshot: %v %d %g", h.counts, h.count, h.sum)
	}
	v, _ = (&Registry{}).NewHistogram(HistogramOptions{Name: "finite_sum"})
	h, _ = v.Bind()
	h.Observe(math.MaxFloat64)
	if err := h.Observe(math.MaxFloat64); !errors.Is(err, ErrCapacity) || h.count != 1 {
		t.Fatal("sum overflow changed histogram")
	}
	h.count = ^uint64(0)
	if err := h.Observe(0); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}

func TestHistogramConcurrentConsistency(t *testing.T) {
	v, _ := (&Registry{}).NewHistogram(HistogramOptions{Name: "concurrent", Bounds: []float64{0, 1, 2}})
	h, _ := v.Bind()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 2000; n++ {
				if err := h.Observe(1); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	for {
		h.mu.Lock()
		var count uint64
		for _, n := range h.counts {
			count += n
		}
		consistent := count == h.count && h.sum == float64(h.count)
		h.mu.Unlock()
		if !consistent {
			t.Fatal("torn bucket/count/sum snapshot")
		}
		select {
		case <-done:
			if h.count != 8000 {
				t.Fatal(h.count)
			}
			return
		default:
			runtime.Gosched() // wasm cooperatively schedules its single OS thread
		}
	}
}
