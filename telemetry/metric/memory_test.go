package metric

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// A retained-heap measurement independent of the registry's admission formula.
func TestRegistryRetainedHeapAccounting(t *testing.T) {
	const count = 7168
	tuples := make([][]string, count)
	for i := range tuples {
		tuples[i] = []string{"page", fmt.Sprintf("/route/%03d", i/14), []string{"GET", "HEAD"}[i/7%2], fmt.Sprint(i % 7)}
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	r, err := NewRegistry(RegistryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	v, err := r.NewCounter(CounterOptions{Name: "heap_probe_total", Labels: []Label{
		{Name: "kind", MaxValues: 1024}, {Name: "route", MaxValues: 1024}, {Name: "method", MaxValues: 1024}, {Name: "status_class", MaxValues: 1024},
	}})
	if err != nil {
		t.Fatal(err)
	}
	batch := make([]TupleDeclaration, len(tuples))
	for i := range batch {
		batch[i] = TupleDeclaration{v, tuples[i]}
	}
	if err := r.DeclareBatch(batch); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("samples=%d accounted=%d retained=%d bytes/sample=%d", count, r.Usage().Bytes, retained, retained/count)
	if retained > r.Usage().Bytes {
		t.Fatalf("retained heap %d exceeds reservation %d", retained, r.Usage().Bytes)
	}
	runtime.KeepAlive(r)
	runtime.KeepAlive(tuples)
}

// S7b must be able to reserve its request counters, duration histograms and
// response bytes for every default route, plus fixed overflow and self metrics.
func TestDefaultRequestMetricInventoryFits(t *testing.T) {
	r := &Registry{}
	routes := make([]string, 0, 514)
	for i := 0; i < 512; i++ {
		routes = append(routes, fmt.Sprintf("/route/%03d", i))
	}
	routes = append(routes, "(other)", "(none)")
	labels := []Label{{Name: "kind", Values: []string{"page", "error", "other"}}, {Name: "route", Values: routes, MaxValues: 1024}}
	requests, err := r.NewCounter(CounterOptions{Name: "http_requests_total", Labels: append(append([]Label{}, labels...), Label{Name: "method", Values: []string{"GET", "HEAD"}}, Label{Name: "status_class", Values: []string{"1xx", "2xx", "3xx", "4xx", "5xx", "other", "hijacked"}})})
	if err != nil {
		t.Fatal(err)
	}
	duration, err := r.NewHistogram(HistogramOptions{Name: "http_request_duration_seconds", Labels: labels, Bounds: []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := r.NewCounter(CounterOptions{Name: "http_response_bytes_total", Labels: labels})
	if err != nil {
		t.Fatal(err)
	}
	batch := make([]TupleDeclaration, 0, len(routes)*16)
	for _, route := range routes {
		kinds := []string{"page", "error"}
		if route == "(other)" || route == "(none)" {
			kinds = []string{"other"}
		}
		for _, kind := range kinds {
			for _, method := range []string{"GET", "HEAD"} {
				for _, status := range []string{"1xx", "2xx", "3xx", "4xx", "5xx", "other", "hijacked"} {
					batch = append(batch, TupleDeclaration{requests, []string{kind, route, method, status}})
				}
			}
			batch = append(batch, TupleDeclaration{duration, []string{kind, route}}, TupleDeclaration{response, []string{kind, route}})
		}
	}
	if err := r.DeclareBatch(batch); err != nil {
		t.Fatalf("default request inventory: %v", err)
	}
	if _, err := r.NewGauge(GaugeOptions{Name: "http_requests_in_flight"}); err != nil {
		t.Fatal(err)
	}
	// Leave room for each mandatory aggregate family and its fixed overflow.
	for i := 0; i < 128; i++ {
		if _, err := r.NewCounter(CounterOptions{Name: fmt.Sprintf("aggregate_%03d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	usage := r.Usage()
	if usage.Samples > defaultMaxSeries || usage.Bytes > defaultMaxBytes {
		t.Fatalf("default inventory exceeds unchanged caps: %+v", usage)
	}
	t.Logf("default request and aggregate inventory: %+v", usage)
}

func TestReservedLabelNamesRejected(t *testing.T) {
	for _, name := range []string{"__name__", "__private", "__"} {
		r := &Registry{}
		before := r.Usage()
		if _, err := r.NewCounter(CounterOptions{Name: "events_total", Labels: []Label{{Name: name}}}); err == nil {
			t.Fatalf("reserved label %q accepted", name)
		}
		if r.Usage() != before {
			t.Fatal("invalid descriptor changed reservations")
		}
	}
}

func TestRegistryLongTupleHeapAccounting(t *testing.T) {
	const count = 512
	values := make([][]string, count)
	for i := range values {
		values[i] = make([]string, 6)
		for j := range values[i] {
			values[i][j] = fmt.Sprintf("%04d%02d", i, j) + strings.Repeat("x", 122)
		}
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	r := &Registry{}
	labels := make([]Label, 6)
	for i := range labels {
		labels[i] = Label{Name: fmt.Sprintf("label_%d", i), MaxValues: 1024}
	}
	v, err := r.NewCounter(CounterOptions{Name: "long_labels_total", Labels: labels})
	if err != nil {
		t.Fatal(err)
	}
	batch := make([]TupleDeclaration, count)
	for i := range batch {
		batch[i] = TupleDeclaration{v, values[i]}
	}
	if err := r.DeclareBatch(batch); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("long labels: accounted=%d retained=%d", r.Usage().Bytes, retained)
	if retained > r.Usage().Bytes {
		t.Fatal("long tuple allocations exceed reservation")
	}
	runtime.KeepAlive(r)
	runtime.KeepAlive(values)
}
