package metric

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/telemetryauthority"
)

func TestRegistryDescriptorsAndAuthority(t *testing.T) {
	bad := []CounterOptions{
		{Name: "PRIVATE/name"}, {Name: strings.Repeat("a", 129)}, {Name: "gosx_private"},
		{Name: "valid", Help: string([]byte{255})}, {Name: "valid", Help: strings.Repeat("h", 257)},
		{Name: "valid", Labels: []Label{{Name: "bad:name"}}},
		{Name: "valid", Labels: []Label{{Name: "same"}, {Name: "same"}}},
		{Name: "valid", Labels: []Label{{Name: "key", MaxValues: -1}}},
		{Name: "valid", Labels: []Label{{Name: "key", MaxValues: 1025}}},
		{Name: "valid", Labels: []Label{{Name: "key", Values: []string{"same", "same"}}}},
		{Name: "valid", Labels: []Label{{Name: "key", Values: []string{strings.Repeat("v", 129)}}}},
		{Name: "valid", Labels: []Label{{Name: "key", Values: []string{string([]byte{255})}}}},
		{Name: "valid", Labels: make([]Label, 7)},
	}
	for _, opts := range bad {
		for _, r := range []*Registry{nil, {}} {
			if _, err := r.NewCounter(opts); !errors.Is(err, ErrInvalidOptions) || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("descriptor rejection: %v", err)
			}
		}
	}
	for _, opts := range []RegistryOptions{{MaxSeries: -1}, {MaxBytes: -1}} {
		if _, err := NewRegistry(opts); !errors.Is(err, ErrInvalidOptions) {
			t.Fatal(err)
		}
	}
	r := &Registry{}
	if _, err := WithAuthority(r, telemetryauthority.Key{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	privileged, err := WithAuthority(r, telemetryauthority.New())
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := privileged.NewCounter(CounterOptions{Name: "gosx_reserved"})
	if err != nil {
		t.Fatal(err)
	}
	if again, err := privileged.NewCounter(CounterOptions{Name: "gosx_reserved"}); err != nil || again != reserved {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := r.NewCounter(CounterOptions{Name: "gosx_reserved"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := privileged.NewGauge(GaugeOptions{Name: "gosx_reserved"}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if r.Usage() != privileged.Usage() {
		t.Fatal("authority view does not share state")
	}
}

func TestRegistryReservationsAndSampleAccounting(t *testing.T) {
	r, err := NewRegistry(RegistryOptions{MaxSeries: 6})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.NewCounter(CounterOptions{Name: "one"}); err != nil {
		t.Fatal(err)
	}
	h, err := r.NewHistogram(HistogramOptions{Name: "duration", Labels: []Label{{Name: "kind", Values: []string{"a", "b"}}}, Bounds: []float64{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Bind("a"); err != nil {
		t.Fatal(err)
	}
	before := r.Usage()
	if before.Samples != 6 {
		t.Fatalf("B+3 accounting: %+v", before)
	}
	if _, err := h.Bind("b"); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if _, err := r.NewGauge(GaugeOptions{Name: "extra"}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if r.Usage() != before {
		t.Fatal("failed reservation changed state")
	}
	limited, err := NewRegistry(RegistryOptions{MaxBytes: before.Bytes - 1})
	if err != nil {
		t.Fatal(err)
	}
	limited.NewCounter(CounterOptions{Name: "one"})
	lh, err := limited.NewHistogram(HistogramOptions{Name: "duration", Labels: []Label{{Name: "kind", Values: []string{"a", "b"}}}, Bounds: []float64{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lh.Bind("a"); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	full := &Registry{}
	for i := 0; i < maxFamilies; i++ {
		if _, err := full.NewCounter(CounterOptions{Name: fmt.Sprintf("family_%03d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := full.NewCounter(CounterOptions{Name: "excess"}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}

func TestDeclareBatchAtomicAndSeal(t *testing.T) {
	r, _ := NewRegistry(RegistryOptions{MaxSeries: 3})
	a, _ := r.NewCounter(CounterOptions{Name: "a", Labels: []Label{{Name: "kind", MaxValues: 2}}})
	b, _ := r.NewGauge(GaugeOptions{Name: "b", Labels: []Label{{Name: "kind", MaxValues: 2}}})
	if _, err := a.Bind("first"); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal("Bind learned a value")
	}
	if err := a.Declare("first"); err != nil {
		t.Fatal(err)
	}
	before := r.Usage()
	batch := []TupleDeclaration{{a, []string{"second"}}, {b, []string{"second"}}, {b, []string{"third"}}}
	if err := r.DeclareBatch(batch); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if r.Usage() != before || len(b.family.labels[0].values) != 0 || len(a.family.labels[0].values) != 1 {
		t.Fatal("partial batch admission")
	}
	if err := r.DeclareBatch(batch[:2]); err != nil {
		t.Fatal(err)
	}
	admitted := r.Usage()
	if err := r.DeclareBatch(batch[:2]); err != nil || r.Usage() != admitted {
		t.Fatal("duplicate declaration charged again")
	}
	other := &Registry{}
	foreign, _ := other.NewCounter(CounterOptions{Name: "foreign"})
	if err := r.DeclareBatch([]TupleDeclaration{{foreign, nil}}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	r.Seal()
	if _, err := a.Bind("second"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Bind("unknown"); !errors.Is(err, ErrAfterBuild) {
		t.Fatal(err)
	}
	if err := a.Declare("second"); !errors.Is(err, ErrAfterBuild) {
		t.Fatal(err)
	}
	if _, err := r.NewCounter(CounterOptions{Name: "late"}); !errors.Is(err, ErrAfterBuild) {
		t.Fatal(err)
	}
}

func TestTupleAuthorityCopiesAndCollisions(t *testing.T) {
	r := &Registry{}
	labels := []Label{{Name: "kind", Values: []string{"a", "b"}}, {Name: "status", Values: []string{"x", "y"}}}
	c, err := r.NewCounter(CounterOptions{Name: "fixed", Labels: labels})
	if err != nil {
		t.Fatal(err)
	}
	labels[0].Name = "changed"
	labels[0].Values[0] = "changed"
	if _, err := c.Bind("a", "y"); err != nil {
		t.Fatal(err)
	}
	d, _ := r.NewCounter(CounterOptions{Name: "declared", Labels: []Label{{Name: "kind", MaxValues: 2}, {Name: "status", MaxValues: 2}}})
	if err := d.Declare("a", "x"); err != nil {
		t.Fatal(err)
	}
	if err := d.Declare("b", "y"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Bind("a", "y"); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal("Bind formed an undeclared Cartesian product")
	}
	if err := d.Declare("third", "x"); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if tupleKey([]string{"a\x00", "b"}) == tupleKey([]string{"a", "\x00b"}) {
		t.Fatal("tuple framing collision")
	}
	if _, err := c.Bind("a"); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	r.NewHistogram(HistogramOptions{Name: "latency", Bounds: []float64{1}})
	if _, err := r.NewCounter(CounterOptions{Name: "latency_count"}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := r.NewCounter(CounterOptions{Name: "fixed", Help: "different"}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestInstrumentsZeroOverflowAndAllocations(t *testing.T) {
	var counter Counter
	counter.Add(1)
	var nilCounter *Counter
	nilCounter.Add(1)
	var nilRegistry *Registry
	cv, err := nilRegistry.NewCounter(CounterOptions{Name: "inert"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := cv.Bind()
	if err != nil {
		t.Fatal(err)
	}
	c.Add(1)
	var g Gauge
	if err := g.Set(1); err != nil {
		t.Fatal(err)
	}
	var h Histogram
	if err := h.Observe(1); err != nil {
		t.Fatal(err)
	}
	if err := g.Set(math.NaN()); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if err := h.Observe(math.Inf(1)); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	r := &Registry{}
	cv, _ = r.NewCounter(CounterOptions{Name: "counter"})
	c, _ = cv.Bind()
	c.Add(^uint64(0))
	c.Add(1)
	if c.cell.value.Load() != ^uint64(0) {
		t.Fatal("counter overflow decreased value")
	}
	gv, _ := r.NewGauge(GaugeOptions{Name: "gauge"})
	liveGauge, _ := gv.Bind()
	hv, _ := r.NewHistogram(HistogramOptions{Name: "hist", Bounds: []float64{1, 2}})
	liveHist, _ := hv.Bind()
	if allocs := testing.AllocsPerRun(1000, func() { c.Add(1); liveGauge.Set(3); liveHist.Observe(1) }); allocs != 0 {
		t.Fatalf("producer allocations=%g", allocs)
	}
	if err := liveGauge.Set(math.Inf(-1)); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
}

func BenchmarkInstruments(b *testing.B) {
	r := &Registry{}
	cv, _ := r.NewCounter(CounterOptions{Name: "counter"})
	c, _ := cv.Bind()
	gv, _ := r.NewGauge(GaugeOptions{Name: "gauge"})
	g, _ := gv.Bind()
	hv, _ := r.NewHistogram(HistogramOptions{Name: "hist", Bounds: []float64{1, 2, 3}})
	h, _ := hv.Bind()
	for name, update := range map[string]func(){"counter": func() { c.Add(1) }, "gauge": func() { g.Set(1) }, "histogram": func() { h.Observe(1) }} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				update()
			}
		})
	}
}
