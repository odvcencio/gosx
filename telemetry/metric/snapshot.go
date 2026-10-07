package metric

import (
	"context"
	"errors"
	"math"
	"slices"
	"time"
)

type LabelValue struct{ Name, Value string }
type HistogramSnapshot struct {
	Bounds []float64
	Counts []uint64
	Count  uint64
	Sum    float64
}
type SeriesSnapshot struct {
	Labels    []LabelValue
	Counter   uint64
	Gauge     float64
	Histogram *HistogramSnapshot
}
type FamilySnapshot struct {
	Name, Help string
	Kind       InstrumentKind
	Series     []SeriesSnapshot
}

// Snapshot is valid only during its callback. Counts are noncumulative and
// include the overflow bucket. Clone makes an independently retained copy.
type Snapshot struct{ Families []FamilySnapshot }

type histogramScratch struct {
	value  HistogramSnapshot
	bounds []float64
	counts []uint64
}

var errSnapshotPanic = errors.New("metric: snapshot callback panic")

// WithSnapshot copies finite descriptors and values, then calls visit outside
// registry/histogram locks. One owner and at most eight 2-second waiters share
// scratch with text exposition. Callbacks must make bounded copies and return;
// adapters perform network I/O after return. Retaining slices is invalid.
func (r *Registry) WithSnapshot(ctx context.Context, visit func(Snapshot) error) error {
	if ctx == nil {
		return invalid("snapshot.context", "required")
	}
	if visit == nil {
		return invalid("snapshot.callback", "required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s := r.get()
	if s == nil {
		return visitSnapshot(ctx, Snapshot{}, visit)
	}
	if err := s.acquireSnapshot(ctx); err != nil {
		return err
	}
	defer func() { <-s.snapshotGate }()
	return visitSnapshot(ctx, s.copySnapshot(), visit)
}

func visitSnapshot(ctx context.Context, snapshot Snapshot, visit func(Snapshot) error) (err error) {
	defer func() {
		if recover() != nil {
			err = errSnapshotPanic
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err = visit(snapshot); err != nil {
		return &snapshotVisitError{cause: err}
	}
	return ctx.Err()
}

type snapshotVisitError struct{ cause error }

func (e *snapshotVisitError) Error() string { return "metric: snapshot callback failed" }
func (e *snapshotVisitError) Unwrap() error { return e.cause }

func (s *registryState) acquireSnapshot(ctx context.Context) error {
	select {
	case s.snapshotGate <- struct{}{}:
		return nil
	default:
	}
	s.mu.Lock()
	if s.snapshotWaiters == 8 {
		s.mu.Unlock()
		return ErrCapacity
	}
	s.snapshotWaiters++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.snapshotWaiters--; s.mu.Unlock() }()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case s.snapshotGate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return context.DeadlineExceeded
	}
}

func (s *registryState) copySnapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, f := range s.ordered {
		for j, c := range f.ordered {
			for k, value := range c.values {
				c.snapshotLabels[k] = LabelValue{f.labels[k].name, value}
			}
			series := SeriesSnapshot{Labels: c.snapshotLabels}
			switch f.kind {
			case KindCounter:
				series.Counter = c.value.Load()
			case KindGauge:
				series.Gauge = math.Float64frombits(c.value.Load())
			case KindHistogram:
				h, buf := &c.histogram, c.snapshotHistogram
				copy(buf.bounds, f.bounds)
				h.mu.Lock()
				copy(buf.counts, h.counts)
				buf.value = HistogramSnapshot{buf.bounds, buf.counts, h.count, h.sum}
				h.mu.Unlock()
				series.Histogram = &buf.value
			}
			f.snapshotSeries[j] = series
		}
		s.snapshotFamilies[i] = FamilySnapshot{f.name, f.help, f.kind, f.snapshotSeries}
	}
	return Snapshot{Families: s.snapshotFamilies}
}

// Clone's allocations belong to the retaining adapter, outside the registry
// arena. That adapter must declare and bound its own extra capacity.
func (s Snapshot) Clone() Snapshot {
	copy := Snapshot{Families: slices.Clone(s.Families)}
	for i := range copy.Families {
		f := &copy.Families[i]
		f.Series = slices.Clone(f.Series)
		for j := range f.Series {
			series := &f.Series[j]
			series.Labels = slices.Clone(series.Labels)
			if h := series.Histogram; h != nil {
				series.Histogram = &HistogramSnapshot{slices.Clone(h.Bounds), slices.Clone(h.Counts), h.Count, h.Sum}
			}
		}
	}
	return copy
}
