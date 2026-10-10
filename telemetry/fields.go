package telemetry

import (
	"math/bits"
	"sort"
	"sync/atomic"
	"time"

	"m31labs.dev/gosx/internal/telemetryfields"
	"m31labs.dev/gosx/telemetry/schema"
)

// FieldSet is a synchronous codec's bounded staging view. Do not retain it or
// use it concurrently. A failed setter rejects the complete encoding even if
// the encoder ignores its returned error. Setters replace earlier values.
type FieldSet struct {
	lease      *fieldLease
	generation uint64
	schema     *fieldSchema
	scope      []int
}

type stagedCell struct {
	offset, length uint16
	present        bool
	duration       time.Duration
}
type fieldLease struct {
	generation      atomic.Uint64
	values, scratch [4096]byte
	cells           [64]stagedCell
	used            int
	err             error
}
type fieldPool struct {
	mask   atomic.Uint32
	leases [8]fieldLease
}

// Registration charges the whole pool before allocating it, including JSON
// scratch, cell indices and synchronization, beyond the 4 KiB encoded limit.
const fieldPoolBytes = int64(128 << 10)

func (p *fieldPool) acquire() (*fieldLease, func()) {
	if p == nil {
		return nil, nil
	}
	for {
		mask := p.mask.Load()
		free := ^mask & 255
		if free == 0 {
			return nil, nil
		}
		id := bits.TrailingZeros32(free)
		bit := uint32(1) << id
		if !p.mask.CompareAndSwap(mask, mask|bit) {
			continue
		}
		l := &p.leases[id]
		l.generation.Add(1)
		return l, func() {
			l.generation.Add(1)
			clear(l.values[:])
			clear(l.scratch[:])
			clear(l.cells[:])
			l.used = 0
			l.err = nil
			p.mask.And(^bit)
		}
	}
}

func (f *FieldSet) active() bool {
	return f != nil && f.lease != nil && f.lease.generation.Load() == f.generation && f.generation&1 == 1
}
func (f *FieldSet) fail(err error) error {
	if f.active() && f.lease.err == nil {
		f.lease.err = err
	}
	return err
}
func (f *FieldSet) lookup(key string, typ FieldType) (int, error) {
	if !f.active() {
		return 0, ErrClosed
	}
	if f.lease.err != nil {
		return 0, f.lease.err
	}
	i := sort.Search(len(f.scope), func(i int) bool { return f.schema.nodes[f.scope[i]].name >= key })
	if i == len(f.scope) || f.schema.nodes[f.scope[i]].name != key {
		return 0, f.fail(invalid("domain_fields", "unknown_field"))
	}
	id := f.scope[i]
	if f.schema.nodes[id].typ != typ {
		return 0, f.fail(invalid("domain_fields", "type"))
	}
	return id, nil
}
func (f *FieldSet) put(v schema.Field) error {
	id, err := f.lookup(v.Name, v.Type)
	if err != nil {
		return err
	}
	n := f.schema.nodes[id]
	if v.Type == FieldEnums && len(v.Enums) > 32 {
		return f.fail(ErrFieldBudget)
	}
	allowed := func(s string) bool {
		i := sort.SearchStrings(n.values, s)
		return i < len(n.values) && n.values[i] == s
	}
	if v.Type == FieldEnum && !allowed(v.Enum) {
		return f.fail(invalid("domain_fields", "enum"))
	}
	if v.Type == FieldEnums {
		for _, s := range v.Enums {
			if !allowed(s) {
				return f.fail(invalid("domain_fields", "enum"))
			}
		}
	}
	l := f.lease
	data, err := telemetryfields.AppendValue(l.scratch[:0], v)
	if err != nil {
		return f.fail(err)
	}
	if len(data) > len(l.values)-l.used+int(l.cells[id].length) {
		return f.fail(ErrFieldBudget)
	}
	l.remove(id)
	cell := stagedCell{offset: uint16(l.used), length: uint16(len(data)), present: true, duration: v.Duration}
	copy(l.values[l.used:], data)
	l.used += len(data)
	l.cells[id] = cell
	return nil
}

func (l *fieldLease) remove(id int) {
	c := l.cells[id]
	end := int(c.offset) + int(c.length)
	if c.present && c.length != 0 {
		copy(l.values[int(c.offset):], l.values[end:l.used])
		l.used -= int(c.length)
		for i := range l.cells {
			if l.cells[i].present && l.cells[i].offset > c.offset {
				l.cells[i].offset -= c.length
			}
		}
	}
	l.cells[id] = stagedCell{}
}

func (f *FieldSet) Int(key string, v int64) error {
	return f.put(schema.Field{Name: key, Type: FieldInt, Int: v})
}
func (f *FieldSet) Float(key string, v float64) error {
	return f.put(schema.Field{Name: key, Type: FieldFloat, Float: v})
}
func (f *FieldSet) Bool(key string, v bool) error {
	return f.put(schema.Field{Name: key, Type: FieldBool, Bool: v})
}
func (f *FieldSet) Duration(key string, v time.Duration) error {
	return f.put(schema.Field{Name: key, Type: FieldDuration, Duration: v})
}
func (f *FieldSet) Enum(key, v string) error {
	return f.put(schema.Field{Name: key, Type: FieldEnum, Enum: v})
}
func (f *FieldSet) Ints(key string, v []int64) error {
	return f.put(schema.Field{Name: key, Type: FieldInts, Ints: v})
}
func (f *FieldSet) Floats(key string, v []float64) error {
	return f.put(schema.Field{Name: key, Type: FieldFloats, Floats: v})
}
func (f *FieldSet) Enums(key string, v []string) error {
	return f.put(schema.Field{Name: key, Type: FieldEnums, Enums: v})
}
func (f *FieldSet) Object(key string, encode func(*FieldSet) error) error {
	id, err := f.lookup(key, FieldObject)
	if err != nil {
		return err
	}
	if encode == nil {
		return f.fail(invalid("domain_fields", "encoder_required"))
	}
	var reset func([]int)
	reset = func(scope []int) {
		for _, n := range scope {
			reset(f.schema.nodes[n].children)
			f.lease.remove(n)
		}
	}
	reset(f.schema.nodes[id].children)
	f.lease.cells[id] = stagedCell{present: true}
	child := &FieldSet{lease: f.lease, generation: f.generation, schema: f.schema, scope: f.schema.nodes[id].children}
	if err = encode(child); err != nil {
		return f.fail(&domainEncodingError{code: "encode_error", cause: err})
	}
	return f.lease.err
}

func renderStaged(dst []byte, l *fieldLease, s *fieldSchema, scope []int) ([]byte, error) {
	add := func(v []byte) bool {
		if len(v) > 4096-len(dst) {
			return false
		}
		dst = append(dst, v...)
		return true
	}
	if !add([]byte{'{'}) {
		return nil, ErrFieldBudget
	}
	first := true
	for _, id := range scope {
		c := l.cells[id]
		if !c.present {
			continue
		}
		if !first && !add([]byte{','}) {
			return nil, ErrFieldBudget
		}
		first = false
		n := s.nodes[id]
		if !add([]byte{'"'}) || !add([]byte(n.name)) || !add([]byte{'"', ':'}) {
			return nil, ErrFieldBudget
		}
		if n.typ == FieldObject {
			var err error
			dst, err = renderStaged(dst, l, s, n.children)
			if err != nil {
				return nil, err
			}
		} else if !add(l.values[int(c.offset) : int(c.offset)+int(c.length)]) {
			return nil, ErrFieldBudget
		}
	}
	if !add([]byte{'}'}) {
		return nil, ErrFieldBudget
	}
	return dst, nil
}
