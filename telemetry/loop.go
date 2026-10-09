package telemetry

import (
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"m31labs.dev/gosx/telemetry/metric"
	"m31labs.dev/gosx/telemetry/schema"
)

type LoopOptions struct {
	Budget   time.Duration
	TickRate int
}

// LoopKind declares a finite kind before Build; instances may start afterward.
type LoopKind struct {
	owner     *Telemetry
	opts      LoopOptions
	metrics   loopInstruments
	instances int64 // protected by loopState.mu
}

type loopInstruments struct {
	instances, budget         *metric.Gauge
	ticks, overruns           *metric.Counter
	duration, lag, stateBytes *metric.Histogram
}
type loopVectors struct {
	instances, budget         *metric.GaugeVec
	ticks, overruns           *metric.CounterVec
	duration, lag, stateBytes *metric.HistogramVec
}
type loopData struct {
	bins              [513]uint64
	samples, overruns uint64
	max               time.Duration
}
type loopSlot struct {
	mu          sync.Mutex
	kind        *LoopKind
	epoch, next uint64
	pending     uint64
	data        loopData
}
type loopState struct {
	mu      sync.Mutex
	kinds   map[string]*LoopKind
	slots   *[256]loopSlot
	vectors loopVectors
	closed  atomic.Bool
	bytes   atomic.Int64
}

// The fixed arena covers all slots, kind metadata, map capacity and handles.
// Closed handles cannot retain a separate bin array or observe a reused slot.
const loopArenaBytes = int64(2 << 20)

var loopBounds = []float64{.0005, .001, .002, .004, .008, .012, .016, .020, .025, .033, .050, .075, .100, .250, 1}

func (t *Telemetry) initializeLoops() error {
	s := &loopState{kinds: make(map[string]*LoopKind, 32)}
	v := &s.vectors
	labels := []metric.Label{{Name: "loop", MaxValues: 33}}
	for _, d := range []struct {
		name string
		dst  **metric.GaugeVec
	}{{"gosx_loop_instances", &v.instances}, {"gosx_loop_budget_seconds", &v.budget}} {
		x, err := t.authority.NewGauge(metric.GaugeOptions{Name: d.name, Labels: labels})
		if err != nil {
			return err
		}
		*d.dst = x
	}
	for _, d := range []struct {
		name string
		dst  **metric.CounterVec
	}{{"gosx_loop_ticks_total", &v.ticks}, {"gosx_loop_tick_overruns_total", &v.overruns}} {
		x, err := t.authority.NewCounter(metric.CounterOptions{Name: d.name, Labels: labels})
		if err != nil {
			return err
		}
		*d.dst = x
	}
	for _, d := range []struct {
		name   string
		bounds []float64
		dst    **metric.HistogramVec
	}{{"gosx_loop_tick_duration_seconds", loopBounds, &v.duration}, {"gosx_loop_lag_seconds", loopBounds, &v.lag}, {"gosx_loop_state_bytes", []float64{1024, 2048, 4096, 8192, 16384, 32768, 49152, 65536, 98304, 131072, 262144}, &v.stateBytes}} {
		x, err := t.authority.NewHistogram(metric.HistogramOptions{Name: d.name, Labels: labels, Bounds: d.bounds})
		if err != nil {
			return err
		}
		*d.dst = x
	}
	t.loops = s
	s.bytes.Store(16 << 10)
	_, err := t.bindLoop("other")
	return err
}

func (t *Telemetry) bindLoop(kind string) (loopInstruments, error) {
	v := &t.loops.vectors
	batch := make([]metric.TupleDeclaration, 0, 7)
	for _, x := range []metric.InstrumentVec{v.instances, v.budget, v.ticks, v.overruns, v.duration, v.lag, v.stateBytes} {
		batch = append(batch, metric.TupleDeclaration{Instrument: x, Values: []string{kind}})
	}
	if err := t.authority.DeclareBatch(batch); err != nil {
		return loopInstruments{}, err
	}
	m := loopInstruments{}
	m.instances, _ = v.instances.Bind(kind)
	m.budget, _ = v.budget.Bind(kind)
	m.ticks, _ = v.ticks.Bind(kind)
	m.overruns, _ = v.overruns.Bind(kind)
	m.duration, _ = v.duration.Bind(kind)
	m.lag, _ = v.lag.Bind(kind)
	m.stateBytes, _ = v.stateBytes.Bind(kind)
	return m, nil
}

func (t *Telemetry) NewLoopKind(kind string, opts LoopOptions) (*LoopKind, error) {
	if !kindName(kind) {
		return nil, invalid("loop_kind", "name")
	}
	if opts.TickRate < 0 || opts.TickRate > 1000 {
		return nil, invalid("loop_rate", "range")
	}
	if opts.Budget < 0 {
		return nil, invalid("loop_budget", "range")
	}
	if opts.Budget == 0 && opts.TickRate > 0 {
		opts.Budget = time.Second / time.Duration(opts.TickRate)
	}
	if opts.Budget == 0 {
		return nil, invalid("loop_budget", "required")
	}
	if t == nil || t.loops == nil {
		return &LoopKind{opts: opts}, nil
	}
	s := t.loops
	s.mu.Lock()
	defer s.mu.Unlock()
	if !t.Enabled() {
		return nil, ErrClosed
	}
	if s.kinds[kind] != nil {
		return nil, ErrConflict
	}
	if len(s.kinds) == 32 {
		t.core.dropped["series"].Add(1)
		return nil, ErrCapacity
	}
	m, err := t.bindLoop(kind)
	if err != nil {
		return nil, err
	}
	k := &LoopKind{owner: t, opts: opts, metrics: m}
	m.budget.Set(opts.Budget.Seconds())
	s.kinds[strings.Clone(kind)] = k
	return k, nil
}

// Loop meters one writer and permits concurrent Health readers. Close releases
// instrumentation only; the application still owns its actual runtime.
type Loop struct {
	kind   *LoopKind
	slot   *loopSlot
	epoch  uint64
	closed atomic.Bool // only used by an aggregate-only capacity fallback
}

// Instance returns an aggregate-only meter with ErrCapacity at the global cap.
// That fallback accepts Observe, but has no health bins or Begin/End token.
func (k *LoopKind) Instance() (*Loop, error) {
	if k == nil || k.owner == nil {
		return &Loop{}, nil
	}
	s := k.owner.loops
	s.mu.Lock()
	defer s.mu.Unlock()
	if !k.owner.Enabled() {
		return nil, ErrClosed
	}
	if s.slots == nil {
		s.bytes.Store(loopArenaBytes)
		s.slots = new([256]loopSlot)
	}
	for i := range s.slots {
		x := &s.slots[i]
		if x.kind != nil || x.epoch == math.MaxUint64 {
			continue
		}
		x.mu.Lock()
		x.epoch++
		x.kind = k
		x.next, x.pending, x.data = 0, 0, loopData{}
		l := &Loop{kind: k, slot: x, epoch: x.epoch}
		x.mu.Unlock()
		k.instances++
		k.metrics.instances.Set(float64(k.instances))
		return l, nil
	}
	k.owner.core.dropped["loop_instances"].Add(1)
	// Kind aggregation can continue without retaining another health histogram.
	return &Loop{kind: k}, ErrCapacity
}

type TickInfo struct {
	StateBytes, Inputs int
	Lag                time.Duration
}

// TickToken is scoped to one instance and its current Begin. Starting a new
// tick invalidates an unfinished token; copied tokens still end only once.
type TickToken struct {
	owner             *Loop
	epoch, generation uint64
	start             time.Duration
	fault             error
}

func validTick(d time.Duration, info TickInfo) error {
	if d < 0 || info.StateBytes < 0 || info.Inputs < 0 || info.Lag < 0 {
		return invalid("tick", "negative")
	}
	return nil
}

func (l *Loop) Begin() TickToken {
	if l == nil || l.slot == nil {
		return TickToken{}
	}
	x := l.slot
	x.mu.Lock()
	if x.kind == nil || x.epoch != l.epoch || x.next == math.MaxUint64 {
		x.mu.Unlock()
		return TickToken{}
	}
	x.next++
	x.pending = x.next
	tok := TickToken{owner: l, epoch: l.epoch, generation: x.next}
	x.mu.Unlock()
	now, err := readClock(l.kind.owner.opts.Clock)
	tok.start, tok.fault = now.Monotonic, err
	return tok
}

func (l *Loop) End(tok TickToken, info TickInfo) error {
	if l == nil || l.kind == nil {
		return validTick(0, info)
	}
	x := l.slot
	if tok.owner != l || x == nil {
		return ErrConflict
	}
	x.mu.Lock()
	if !x.accepts(tok) {
		x.mu.Unlock()
		return ErrConflict
	}
	if tok.fault != nil {
		x.pending = 0
		x.mu.Unlock()
		return tok.fault
	}
	x.mu.Unlock()
	if err := validTick(0, info); err != nil {
		return err
	}
	now, err := readClock(l.kind.owner.opts.Clock)
	if err == nil && now.Monotonic < tok.start {
		err = invalid("clock", "negative_elapsed")
	}
	if err != nil {
		x.mu.Lock()
		if x.accepts(tok) {
			x.pending = 0
		}
		x.mu.Unlock()
		return err
	}
	d := now.Monotonic - tok.start
	x.mu.Lock()
	if !x.accepts(tok) {
		x.mu.Unlock()
		return ErrConflict
	}
	if x.data.samples == math.MaxUint64 {
		x.mu.Unlock()
		return ErrCapacity
	}
	x.pending = 0
	x.data.observe(d, l.kind.opts.Budget)
	x.mu.Unlock()
	l.kind.record(d, info)
	return nil
}

// Caller holds mu. Clock callbacks always run outside the slot lock.
func (x *loopSlot) accepts(tok TickToken) bool {
	return x.kind != nil && x.epoch == tok.epoch && x.pending != 0 && x.pending == tok.generation
}

func (l *Loop) Observe(d time.Duration, info TickInfo) error {
	if err := validTick(d, info); err != nil {
		return err
	}
	if l == nil || l.kind == nil {
		return nil
	}
	if l.slot == nil {
		if l.closed.Load() || l.kind.owner.loops.closed.Load() {
			return ErrClosed
		}
		l.kind.record(d, info)
		return nil
	}
	x := l.slot
	x.mu.Lock()
	if x.kind == nil || x.epoch != l.epoch {
		x.mu.Unlock()
		return ErrClosed
	}
	if x.data.samples == math.MaxUint64 {
		x.mu.Unlock()
		return ErrCapacity
	}
	x.data.observe(d, l.kind.opts.Budget)
	x.mu.Unlock()
	// Kind histogram locks are never held together with the instance lock.
	l.kind.record(d, info)
	return nil
}

func (d *loopData) observe(duration, budget time.Duration) {
	ns := uint64(duration)
	if ns > 0 {
		ns--
	}
	bin := ns / 100000
	if bin > 512 {
		bin = 512
	}
	d.bins[bin]++
	d.samples++
	if duration > d.max {
		d.max = duration
	}
	if duration > budget {
		d.overruns++
	}
}

func (k *LoopKind) record(d time.Duration, info TickInfo) {
	k.metrics.ticks.Add(1)
	if d > k.opts.Budget {
		k.metrics.overruns.Add(1)
	}
	k.metrics.duration.Observe(d.Seconds())
	k.metrics.lag.Observe(info.Lag.Seconds())
	k.metrics.stateBytes.Observe(float64(info.StateBytes))
}

func (l *Loop) Health() schema.TickHealth {
	if l == nil || l.kind == nil {
		return schema.TickHealth{}
	}
	h := schema.TickHealth{BudgetMS: float64(l.kind.opts.Budget) / 1e6}
	if l.slot == nil {
		return h
	}
	x := l.slot
	x.mu.Lock()
	if x.kind == nil || x.epoch != l.epoch {
		x.mu.Unlock()
		return h
	}
	d := x.data
	x.mu.Unlock()
	if d.samples == 0 {
		return h
	}
	h.Available, h.Samples, h.Overruns = true, d.samples, d.overruns
	h.MaxMS, h.OverflowSamples = float64(d.max)/1e6, d.bins[512]
	h.P50MS = d.quantile(d.samples/2 + d.samples%2)
	h.P99MS = d.quantile(d.samples - d.samples/100)
	return h
}

func (d *loopData) quantile(rank uint64) float64 {
	var count uint64
	for i, n := range d.bins {
		count += n
		if count >= rank {
			if i == 512 {
				return float64(d.max) / 1e6
			}
			return float64(i+1) / 10
		}
	}
	return float64(d.max) / 1e6
}

func (l *Loop) Close() {
	if l == nil || l.kind == nil {
		return
	}
	if l.slot == nil {
		l.closed.Store(true)
		return
	}
	s, x := l.kind.owner.loops, l.slot
	s.mu.Lock()
	x.mu.Lock()
	if x.kind != nil && x.epoch == l.epoch {
		x.kind, x.pending, x.data = nil, 0, loopData{}
		l.kind.instances--
		l.kind.metrics.instances.Set(float64(l.kind.instances))
	}
	x.mu.Unlock()
	s.mu.Unlock()
}

func (t *Telemetry) releaseLoops() {
	if t.loops == nil {
		return
	}
	s := t.loops
	s.mu.Lock()
	s.closed.Store(true)
	if s.slots == nil {
		s.mu.Unlock()
		return
	}
	for i := range s.slots {
		x := &s.slots[i]
		x.mu.Lock()
		if x.kind != nil {
			x.kind.instances--
			x.kind.metrics.instances.Set(float64(x.kind.instances))
			x.kind, x.pending, x.data = nil, 0, loopData{}
		}
		x.mu.Unlock()
	}
	s.mu.Unlock()
}
