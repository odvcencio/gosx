package metric

import (
	"bytes"
	"context"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fullScrapeRegistry(tb testing.TB) *Registry {
	tb.Helper()
	r := &Registry{}
	routes := make([]string, 300)
	for i := range routes {
		routes[i] = "/games/{code}/" + strconv.Itoa(i) + "/\"quoted\"\\\n"
	}
	c, err := r.NewCounter(CounterOptions{Name: "requests_total", Help: "Accepted requests.", Labels: []Label{{Name: "route", Values: routes[:200], MaxValues: 512}}})
	if err != nil {
		tb.Fatal(err)
	}
	h, err := r.NewHistogram(HistogramOptions{Name: "request_duration_seconds", Help: "Complete request work.",
		Labels: []Label{{Name: "route", Values: routes, MaxValues: 512}, {Name: "method", Values: []string{"GET"}}, {Name: "status", Values: []string{"2xx"}}},
		Bounds: []float64{.001, .002, .005, .01, .02, .05, .1, .2, .5, 1, 2, 5, 10}})
	if err != nil {
		tb.Fatal(err)
	}
	for i, route := range routes {
		meter, err := h.Bind(route, "GET", "2xx")
		if err != nil {
			tb.Fatal(err)
		}
		meter.Observe(.25)
		meter.Observe(1)
		if i < 200 {
			meter, err := c.Bind(route)
			if err != nil {
				tb.Fatal(err)
			}
			meter.Add(2)
		}
	}
	r.Seal()
	if r.Usage().Samples != 5000 {
		tb.Fatal("fixture must expose exactly 5,000 samples")
	}
	return r
}

func TestFullScrapeBudget(t *testing.T) {
	r := fullScrapeRegistry(t)
	var buffer bytes.Buffer
	buffer.Grow(maxTextBytes)
	if err := r.WritePrometheus(&buffer); err != nil {
		t.Fatal(err)
	}
	if buffer.Len() > maxTextBytes || parseText(t, buffer.String()) != 5000 {
		t.Fatal("full sample/byte budget")
	}
	bytes := buffer.Len()
	allocs := testing.AllocsPerRun(50, func() {
		buffer.Reset()
		if err := r.WritePrometheus(&buffer); err != nil {
			panic(err)
		}
	})
	if allocs > 2 {
		t.Fatalf("warm scrape allocations=%g limit=2", allocs)
	}
	times := make([]time.Duration, 100)
	for i := range times {
		buffer.Reset()
		start := time.Now()
		if err := r.WritePrometheus(&buffer); err != nil {
			t.Fatal(err)
		}
		times[i] = time.Since(start)
	}
	slices.Sort(times)
	t.Logf("5,000 samples: bytes=%d allocations=%g p50=%s p99=%s reserved=%d", bytes, allocs, times[49], times[98], r.Usage().Bytes)
	if allocs := testing.AllocsPerRun(50, func() { r.WithSnapshot(context.Background(), func(Snapshot) error { return nil }) }); allocs > 2 {
		t.Fatalf("snapshot allocations=%g", allocs)
	}
}

func TestExpositionSizeFailsBeforeWrite(t *testing.T) {
	r := &Registry{}
	values := make([]string, 600)
	for i := range values {
		values[i] = strings.Repeat("\\", 120) + strconv.Itoa(i)
	}
	labels := make([]Label, 6)
	for i := range labels {
		labels[i] = Label{Name: "label_" + strconv.Itoa(i), Values: values, MaxValues: 1024}
	}
	v, err := r.NewHistogram(HistogramOptions{Name: "large", Labels: labels, Bounds: []float64{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if _, err := v.Bind(value, value, value, value, value, value); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	if err := r.WritePrometheus(&output); err != ErrCapacity || output.Len() != 0 {
		t.Fatalf("size overflow: err=%v bytes=%d", err, output.Len())
	}
}

func BenchmarkPrometheus5000(b *testing.B) {
	r := fullScrapeRegistry(b)
	var buffer bytes.Buffer
	buffer.Grow(maxTextBytes)
	r.WritePrometheus(&buffer)
	b.SetBytes(int64(buffer.Len()))
	b.ReportAllocs()
	for b.Loop() {
		buffer.Reset()
		if err := r.WritePrometheus(&buffer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSnapshot5000(b *testing.B) {
	r := fullScrapeRegistry(b)
	b.ReportAllocs()
	for b.Loop() {
		if err := r.WithSnapshot(context.Background(), func(Snapshot) error { return nil }); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrometheusDiscard5000(b *testing.B) {
	r := fullScrapeRegistry(b)
	b.ReportAllocs()
	for b.Loop() {
		if err := r.WritePrometheus(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
