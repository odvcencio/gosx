package metric

import (
	"encoding/binary"
	"slices"
	"strings"
)

type InstrumentVec interface{ instrumentVec() }
type TupleDeclaration struct {
	Instrument InstrumentVec
	Values     []string
}

func (*CounterVec) instrumentVec()   {}
func (*GaugeVec) instrumentVec()     {}
func (*HistogramVec) instrumentVec() {}

func familyOf(v InstrumentVec) *family {
	switch v := v.(type) {
	case *CounterVec:
		if v != nil {
			return v.family
		}
	case *GaugeVec:
		if v != nil {
			return v.family
		}
	case *HistogramVec:
		if v != nil {
			return v.family
		}
	}
	return nil
}

func bind(f *family, values []string) (*cell, error) {
	if f == nil {
		return nil, nil
	}
	s := f.registry
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateTuple(f, values); err != nil {
		return nil, err
	}
	key := tupleKey(values)
	if c := f.cells[key]; c != nil {
		return c, nil
	}
	if s.sealed {
		return nil, ErrAfterBuild
	}
	// Bind can form a tuple from a fixed whitelist but never teaches a
	// Declare-only domain an unknown value.
	for i, value := range values {
		if !f.labels[i].explicit {
			return nil, invalid("metric.labels", "undeclared_tuple")
		}
		if _, ok := f.labels[i].values[value]; !ok {
			return nil, invalid("metric.labels", "undeclared_value")
		}
	}
	if err := declareLocked(s, []TupleDeclaration{{instrumentFor(f), values}}); err != nil {
		return nil, err
	}
	return f.cells[key], nil
}

func instrumentFor(f *family) InstrumentVec {
	switch f.kind {
	case KindCounter:
		return &f.counter
	case KindGauge:
		return &f.gauge
	default:
		return &f.histogram
	}
}

// DeclareBatch atomically reserves every tuple and each domain extension. A
// failure leaves samples, domains, and byte reservations unchanged. A batch
// cannot exceed the registry's scalar capacity, even through duplicates.
func (r *Registry) DeclareBatch(declarations []TupleDeclaration) error {
	s := r.get()
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealed {
		return ErrAfterBuild
	}
	return declareLocked(s, declarations)
}

type pendingFamily struct {
	family  *family
	cells   map[string]int
	domains []map[string]struct{}
}

func declareLocked(s *registryState, declarations []TupleDeclaration) error {
	if len(declarations) > s.opts.MaxSeries {
		return ErrCapacity
	}
	pending := make(map[*family]*pendingFamily)
	samples, bytes := 0, int64(0)
	for index, declaration := range declarations {
		f := familyOf(declaration.Instrument)
		if f == nil || f.registry != s {
			return invalid("metric.declarations", "foreign_instrument")
		}
		if err := validateTuple(f, declaration.Values); err != nil {
			return err
		}
		key := tupleKey(declaration.Values)
		if f.cells[key] != nil {
			continue
		}
		p := pending[f]
		if p == nil {
			p = &pendingFamily{family: f, cells: make(map[string]int), domains: make([]map[string]struct{}, len(f.labels))}
			pending[f] = p
		}
		if _, exists := p.cells[key]; exists {
			continue
		}
		for i, value := range declaration.Values {
			domain := &f.labels[i]
			if _, exists := domain.values[value]; exists {
				continue
			}
			if domain.explicit {
				return invalid("metric.labels", "undeclared_value")
			}
			if _, exists := p.domains[i][value]; exists {
				continue
			}
			if len(domain.values)+len(p.domains[i]) == domain.max {
				return ErrCapacity
			}
			if p.domains[i] == nil {
				p.domains[i] = make(map[string]struct{})
			}
			p.domains[i][value] = struct{}{}
			bytes += domainBytes(value)
		}
		samples += sampleCost(f.kind, len(f.bounds))
		bytes += cellBytes(f.kind, declaration.Values, f.bounds)
		if samples > s.opts.MaxSeries-s.samples || bytes > s.opts.MaxBytes-s.bytes {
			return ErrCapacity
		}
		// Staging is finite and discarded before return. Retained copies are
		// made only after the whole batch's reservation has succeeded.
		p.cells[key] = index
	}
	for _, p := range pending {
		f := p.family
		capacity := cellCapacity(cap(f.ordered), len(f.ordered)+len(p.cells), s.opts.MaxSeries)
		bytes += cellCapacityBytes(capacity) - cellCapacityBytes(cap(f.ordered))
	}
	if bytes > s.opts.MaxBytes-s.bytes {
		return ErrCapacity
	}
	for _, p := range pending {
		f := p.family
		f.growCells(len(p.cells))
		for i, added := range p.domains {
			for value := range added {
				value = strings.Clone(value)
				f.labels[i].values[value] = value
			}
		}
		for key, index := range p.cells {
			f.addCell(key, declarations[index].Values)
		}
		f.sortCells()
	}
	s.samples += samples
	s.bytes += bytes
	return nil
}

func validateTuple(f *family, values []string) error {
	if len(values) != len(f.labels) {
		return invalid("metric.labels", "arity")
	}
	for _, value := range values {
		if !validValue(value) {
			return invalid("metric.labels", "value_text")
		}
	}
	return nil
}

// Length framing admits all valid UTF-8 values, including NUL, without tuple
// collisions. Key construction stays off producer paths, which use Bind once.
func tupleKey(values []string) string {
	if len(values) == 0 {
		return ""
	}
	if len(values) == 1 {
		return values[0]
	}
	var key strings.Builder
	n := len(values) * 2
	for _, value := range values {
		n += len(value)
	}
	key.Grow(n)
	var length [2]byte
	for _, value := range values {
		binary.BigEndian.PutUint16(length[:], uint16(len(value)))
		key.Write(length[:])
		key.WriteString(value)
	}
	return key.String()
}

func (f *family) addCell(key string, values []string) {
	c := &cell{values: make([]string, len(values))}
	for i, value := range values {
		c.values[i] = f.labels[i].values[value]
	}
	if f.kind == KindHistogram {
		c.histogram = &Histogram{cell: c, bounds: f.bounds, counts: make([]uint64, len(f.bounds)+1)}
	}
	c.counter.cell, c.gauge.cell = c, c
	// Multi-label keys come from the private length-framed builder. Reuse
	// that owned key; single-label keys use the domain's copied string.
	if len(values) == 1 {
		key = c.values[0]
	}
	f.cells[key] = c
	f.ordered = append(f.ordered, c)
}

func (f *family) sortCells() {
	slices.SortFunc(f.ordered, func(a, b *cell) int {
		for i, value := range a.values {
			if n := strings.Compare(value, b.values[i]); n != 0 {
				return n
			}
		}
		return 0
	})
}
