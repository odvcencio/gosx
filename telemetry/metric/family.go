package metric

import (
	"math"
	"slices"
	"strings"
	"unicode/utf8"
	"unsafe"
)

// Label declares a fixed whitelist, or a Declare-only domain when Values is
// nil. MaxValues defaults to 64 and cannot exceed 1,024.
type Label struct {
	Name      string
	Values    []string
	MaxValues int
}
type CounterOptions struct {
	Name, Help string
	Labels     []Label
}
type GaugeOptions = CounterOptions
type HistogramOptions struct {
	Name, Help string
	Labels     []Label
	Bounds     []float64
}

type InstrumentKind uint8

const (
	KindCounter InstrumentKind = iota
	KindGauge
	KindHistogram
)

type family struct {
	registry       *registryState
	name, help     string
	kind           InstrumentKind
	labels         []labelDomain
	bounds         []float64
	cells          map[string]*cell
	ordered        []*cell
	snapshotSeries []SeriesSnapshot
	counter        CounterVec
	gauge          GaugeVec
	histogram      HistogramVec
}

type labelDomain struct {
	name     string
	max      int
	explicit bool
	values   map[string]string // immutable canonical copies of admitted strings
}

type descriptor struct {
	name, help string
	kind       InstrumentKind
	labels     []Label
	bounds     []float64
}

func (r *Registry) NewCounter(opts CounterOptions) (*CounterVec, error) {
	f, err := r.register(descriptor{opts.Name, opts.Help, KindCounter, opts.Labels, nil})
	if f == nil || err != nil {
		return nil, err
	}
	return &f.counter, nil
}

func (r *Registry) NewGauge(opts GaugeOptions) (*GaugeVec, error) {
	f, err := r.register(descriptor{opts.Name, opts.Help, KindGauge, opts.Labels, nil})
	if f == nil || err != nil {
		return nil, err
	}
	return &f.gauge, nil
}

func (r *Registry) NewHistogram(opts HistogramOptions) (*HistogramVec, error) {
	f, err := r.register(descriptor{opts.Name, opts.Help, KindHistogram, opts.Labels, opts.Bounds})
	if f == nil || err != nil {
		return nil, err
	}
	return &f.histogram, nil
}

func (r *Registry) register(d descriptor) (*family, error) {
	if err := validateDescriptor(d, r != nil && r.privileged); err != nil {
		return nil, err
	}
	s := r.get()
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealed {
		return nil, ErrAfterBuild
	}
	if f := s.families[d.name]; f != nil {
		if !f.matches(d) {
			return nil, ErrConflict
		}
		return f, nil
	}
	// Generated histogram names must not collide with another descriptor.
	for _, f := range s.ordered {
		if namesOverlap(d.name, d.kind, f.name, f.kind) {
			return nil, ErrConflict
		}
	}
	bytes := descriptorBytes(d)
	samples := 0
	if len(d.labels) == 0 {
		samples = sampleCost(d.kind, len(d.bounds))
		bytes += cellBytes(d.kind, nil, d.bounds) + cellCapacityBytes(cellCapacity(0, 1, s.opts.MaxSeries))
	}
	if len(s.families) == maxFamilies || samples > s.opts.MaxSeries-s.samples || bytes > s.opts.MaxBytes-s.bytes {
		return nil, ErrCapacity
	}
	// All retained descriptor, cell, lookup, and future snapshot space is
	// reserved before copying caller-owned strings/slices or publishing state.
	f := &family{registry: s, name: strings.Clone(d.name), help: strings.Clone(d.help), kind: d.kind,
		labels: make([]labelDomain, len(d.labels)), bounds: slices.Clone(d.bounds), cells: make(map[string]*cell)}
	for i, label := range d.labels {
		domain := &f.labels[i]
		*domain = labelDomain{name: strings.Clone(label.Name), max: labelMax(label), explicit: label.Values != nil, values: make(map[string]string)}
		for _, value := range label.Values {
			value = strings.Clone(value)
			domain.values[value] = value
		}
	}
	f.counter.family, f.gauge.family, f.histogram.family = f, f, f
	if len(d.labels) == 0 {
		f.growCells(1)
		f.addCell("", nil)
	}
	s.families[f.name] = f
	s.snapshotFamilies = append(s.snapshotFamilies, FamilySnapshot{})
	s.ordered = append(s.ordered, f)
	slices.SortFunc(s.ordered, func(a, b *family) int { return strings.Compare(a.name, b.name) })
	s.samples += samples
	s.bytes += bytes
	return f, nil
}

func validateDescriptor(d descriptor, privileged bool) error {
	if !identifier(d.name, 128, true) {
		return invalid("metric.name", "identifier")
	}
	if strings.HasPrefix(d.name, "gosx_") && !privileged {
		return invalid("metric.name", "reserved")
	}
	if len(d.help) > 256 || !utf8.ValidString(d.help) {
		return invalid("metric.help", "text")
	}
	if len(d.labels) > 6 {
		return invalid("metric.labels", "limit")
	}
	for i, label := range d.labels {
		if !identifier(label.Name, 64, false) {
			return invalid("metric.labels", "identifier")
		}
		if strings.HasPrefix(label.Name, "__") {
			return invalid("metric.labels", "reserved")
		}
		if d.kind == KindHistogram && label.Name == "le" {
			return invalid("metric.labels", "reserved_bucket_label")
		}
		for _, earlier := range d.labels[:i] {
			if earlier.Name == label.Name {
				return invalid("metric.labels", "duplicate")
			}
		}
		if label.MaxValues < 0 || labelMax(label) > 1024 || len(label.Values) > labelMax(label) {
			return invalid("metric.labels", "value_limit")
		}
		for j, value := range label.Values {
			if !validValue(value) {
				return invalid("metric.labels", "value_text")
			}
			for _, earlier := range label.Values[:j] {
				if earlier == value {
					return invalid("metric.labels", "duplicate_value")
				}
			}
		}
	}
	if len(d.bounds) > 32 {
		return invalid("metric.bounds", "limit")
	}
	for i, bound := range d.bounds {
		if math.IsNaN(bound) || math.IsInf(bound, 0) || i > 0 && bound <= d.bounds[i-1] {
			return invalid("metric.bounds", "finite_increasing")
		}
	}
	return nil
}

func identifier(value string, max int, colon bool) bool {
	if value == "" || len(value) > max {
		return false
	}
	for i := range len(value) {
		c := value[i]
		if c == '_' || colon && c == ':' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

func labelMax(label Label) int {
	if label.MaxValues == 0 {
		return 64
	}
	return label.MaxValues
}
func validValue(value string) bool { return len(value) <= 128 && utf8.ValidString(value) }
func sampleCost(kind InstrumentKind, bounds int) int {
	if kind == KindHistogram {
		return bounds + 3
	}
	return 1
}

// Charges cover retained Go objects, map growth, slice capacity and allocator
// rounding. Domain strings are owned once; tuple values reference those copies.
// The independent retained-heap gate checks these estimates against real data.
func descriptorBytes(d descriptor) int64 {
	n := int64(1024 + 2*(len(d.name)+len(d.help)) + 32*len(d.bounds))
	for _, label := range d.labels {
		n += int64(256 + 2*len(label.Name))
		for _, value := range label.Values {
			n += domainBytes(value)
		}
	}
	return n
}

func domainBytes(value string) int64 { return int64(128 + len(value)) }
func cellBytes(kind InstrumentKind, values []string, bounds []float64) int64 {
	// Cell and string-header arrays are rounded to allocation sizes. The
	// map charge covers the retained lookup and bounded index staging;
	// domains own value strings and staging borrows caller tuples until return.
	n := allocationBytes(int64(unsafe.Sizeof(cell{}))) + 64 + allocationBytes(int64(16*len(values)))
	keyBytes := 2 * len(values)
	for _, value := range values {
		keyBytes += len(value)
	}
	n += allocationBytes(int64(keyBytes))
	n += allocationBytes(int64(len(values)) * int64(unsafe.Sizeof(LabelValue{})))
	if kind == KindHistogram {
		n += allocationBytes(int64(unsafe.Sizeof(Histogram{}))) + allocationBytes(int64(8*(len(bounds)+1)))
		n += allocationBytes(int64(unsafe.Sizeof(histogramScratch{}))) + allocationBytes(int64(8*len(bounds))) + allocationBytes(int64(8*(len(bounds)+1)))
	}
	return n
}

// Round conservatively across Go's allocation classes and large-object pages.
// Long tuple keys must not be charged as if every class were 16 bytes wide.
func allocationBytes(n int64) int64 {
	switch {
	case n <= 256:
		return (n + 15) / 16 * 16
	case n <= 512:
		return (n + 31) / 32 * 32
	case n <= 1024:
		return (n + 127) / 128 * 128
	case n <= 32768:
		size := int64(1024)
		for size < n {
			size *= 2
		}
		return size
	default:
		return (n + 8191) / 8192 * 8192
	}
}

// Reserve actual slice capacity once for a whole batch. Counter and gauge
// cells do not retain unused histogram state.
func cellCapacity(current, needed, maximum int) int {
	if needed <= current {
		return current
	}
	return min(maximum, max(needed, current+current/4+8))
}

func cellCapacityBytes(capacity int) int64 {
	return allocationBytes(int64(capacity)*int64(unsafe.Sizeof((*cell)(nil)))) + allocationBytes(int64(capacity)*int64(unsafe.Sizeof(SeriesSnapshot{})))
}

func (f *family) growCells(added int) {
	capacity := cellCapacity(cap(f.ordered), len(f.ordered)+added, f.registry.opts.MaxSeries)
	if capacity == cap(f.ordered) {
		return
	}
	ordered := make([]*cell, len(f.ordered), capacity)
	copy(ordered, f.ordered)
	f.ordered = ordered
	series := make([]SeriesSnapshot, len(f.snapshotSeries), capacity)
	copy(series, f.snapshotSeries)
	f.snapshotSeries = series
}

func (f *family) matches(d descriptor) bool {
	if f.kind != d.kind || f.help != d.help || len(f.labels) != len(d.labels) || !slices.Equal(f.bounds, d.bounds) {
		return false
	}
	for i, label := range d.labels {
		domain := f.labels[i]
		if domain.name != label.Name || domain.max != labelMax(label) || domain.explicit != (label.Values != nil) {
			return false
		}
		if domain.explicit {
			if len(domain.values) != len(label.Values) {
				return false
			}
			for _, value := range label.Values {
				if _, ok := domain.values[value]; !ok {
					return false
				}
			}
		}
	}
	return true
}

func namesOverlap(a string, ak InstrumentKind, b string, bk InstrumentKind) bool {
	if a == b {
		return true
	}
	for _, suffix := range []string{"_bucket", "_sum", "_count"} {
		if ak == KindHistogram && a+suffix == b || bk == KindHistogram && b+suffix == a {
			return true
		}
	}
	return false
}
