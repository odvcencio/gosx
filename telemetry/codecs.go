package telemetry

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
	"unsafe"

	"m31labs.dev/gosx/internal/telemetryfields"
	"m31labs.dev/gosx/telemetry/schema"
)

type FieldType = schema.FieldType

const (
	FieldInt      = schema.FieldInt
	FieldFloat    = schema.FieldFloat
	FieldBool     = schema.FieldBool
	FieldDuration = schema.FieldDuration
	FieldEnum     = schema.FieldEnum
	FieldInts     = schema.FieldInts
	FieldFloats   = schema.FieldFloats
	FieldEnums    = schema.FieldEnums
	FieldObject   = schema.FieldObject
)

type FieldDefinition struct {
	Name   string
	Type   FieldType
	Values []string
	Fields []FieldDefinition
}

// DomainCodec accepts one application type and only its declared fields. The
// encoder runs synchronously: it must not retain FieldSet or capture game or
// request state. Declared enums are trusted, reviewed nonpersonal values.
type DomainCodec[T any] struct {
	Name    string
	Version uint16
	Fields  []FieldDefinition
	Encode  func(*FieldSet, T) error
}

type ActivityCodecs[A, P, E any] struct {
	Activity    DomainCodec[A]
	Participant DomainCodec[P]
	Event       DomainCodec[E]
}

type NoFields struct{}

type fieldNode struct {
	name     string
	typ      FieldType
	values   []string
	children []int
}
type fieldSchema struct {
	nodes []fieldNode
	roots []int
}
type compiledDomainCodec[T any] struct {
	name    string
	version uint16
	schema  fieldSchema
	encode  func(*FieldSet, T) error
	empty   bool
	bytes   int64
}

// Prepare is allocation-free. Registration must reserve the returned charge
// for all three codecs before freezing any descriptors or publishing a kind.
func prepareDomainCodec[T any](c DomainCodec[T]) (DomainCodec[T], int64, int, bool, error) {
	var zero T
	_, noFields := any(zero).(NoFields)
	empty := noFields && c.Name == "" && c.Version == 0 && len(c.Fields) == 0 && c.Encode == nil
	if empty {
		c.Name, c.Version = "none", 1
	}
	if !kindName(c.Name) || c.Version == 0 || (!empty && c.Encode == nil) {
		return c, 0, 0, false, invalid("codec", "descriptor")
	}
	if !empty && c.Fields == nil {
		return c, 0, 0, false, invalid("codec", "schema_required")
	}
	count := 0
	stringBytes := func(s string) int64 { return (int64(len(s)) + 15) &^ 15 }
	charge := int64(unsafe.Sizeof(compiledDomainCodec[T]{})) + stringBytes(c.Name)
	var visit func([]FieldDefinition, int) error
	visit = func(defs []FieldDefinition, depth int) error {
		if depth > 2 || len(defs) > 64-count {
			return invalid("codec", "field_cap")
		}
		count += len(defs)
		charge += int64(len(defs)) * (int64(unsafe.Sizeof(fieldNode{})) + 8)
		for i, d := range defs {
			if !telemetryfields.ValidKey(d.Name) || d.Type > FieldObject {
				return invalid("codec", "field")
			}
			for _, prev := range defs[:i] {
				if prev.Name == d.Name {
					return invalid("codec", "duplicate_field")
				}
			}
			charge += stringBytes(d.Name)
			if d.Type == FieldEnum || d.Type == FieldEnums {
				if len(d.Values) == 0 || len(d.Values) > 128 {
					return invalid("codec", "enum_cap")
				}
				charge += int64(len(d.Values)) * int64(unsafe.Sizeof(""))
				for j, v := range d.Values {
					if len(v) > 64 || !utf8.ValidString(v) {
						return invalid("codec", "enum_value")
					}
					for _, prev := range d.Values[:j] {
						if prev == v {
							return invalid("codec", "duplicate_enum")
						}
					}
					charge += stringBytes(v)
				}
			} else if len(d.Values) != 0 {
				return invalid("codec", "enum_type")
			}
			if d.Type == FieldObject {
				if depth == 2 {
					return invalid("codec", "depth")
				}
				if err := visit(d.Fields, depth+1); err != nil {
					return err
				}
			} else if len(d.Fields) != 0 {
				return invalid("codec", "object_type")
			}
		}
		return nil
	}
	err := visit(c.Fields, 0)
	return c, charge, count, empty, err
}

func freezeDomainCodec[T any](c DomainCodec[T], charge int64, count int, empty bool) *compiledDomainCodec[T] {
	out := &compiledDomainCodec[T]{name: strings.Clone(c.Name), version: c.Version, encode: c.Encode, empty: empty, bytes: charge}
	out.schema.nodes = make([]fieldNode, 0, count)
	var freeze func([]FieldDefinition) []int
	freeze = func(defs []FieldDefinition) []int {
		ids := make([]int, len(defs))
		var order [64]int
		for i := range defs {
			order[i] = i
		}
		sort.Slice(order[:len(defs)], func(i, j int) bool { return defs[order[i]].Name < defs[order[j]].Name })
		for i, n := range order[:len(defs)] {
			d := defs[n]
			id := len(out.schema.nodes)
			ids[i] = id
			v := fieldNode{name: strings.Clone(d.Name), typ: d.Type, values: make([]string, len(d.Values))}
			for j, s := range d.Values {
				v.values[j] = strings.Clone(s)
			}
			sort.Strings(v.values)
			out.schema.nodes = append(out.schema.nodes, v)
			children := freeze(d.Fields)
			out.schema.nodes[id].children = children
		}
		return ids
	}
	out.schema.roots = freeze(c.Fields)
	return out
}

func (c *compiledDomainCodec[T]) encodeFields(pool *fieldPool, value T) (out schema.Fields, err error) {
	if c.empty {
		return nil, nil
	}
	lease, release := pool.acquire()
	if lease == nil {
		return nil, &domainEncodingError{code: "codec_busy", cause: ErrCapacity}
	}
	defer release()
	defer func() {
		if recover() != nil {
			out = nil
			err = invalid("codec", "panic")
		}
	}()
	f := &FieldSet{lease: lease, generation: lease.generation.Load(), schema: &c.schema, scope: c.schema.roots}
	if err = c.encode(f, value); err != nil {
		return nil, &domainEncodingError{code: "encode_error", cause: err}
	}
	if lease.err != nil {
		return nil, lease.err
	}
	if _, err = renderStaged(lease.scratch[:0], lease, &c.schema, c.schema.roots); err != nil {
		return nil, err
	}
	return copyStaged(lease, &c.schema, c.schema.roots), nil
}

type domainEncodingError struct {
	code  string
	cause error
}

func (e *domainEncodingError) Error() string {
	return "telemetry: domain codec failed (" + e.code + ")"
}
func (e *domainEncodingError) Unwrap() error { return e.cause }

func copyStaged(l *fieldLease, s *fieldSchema, scope []int) schema.Fields {
	var out schema.Fields
	for _, id := range scope {
		cell := l.cells[id]
		if !cell.present {
			continue
		}
		n := s.nodes[id]
		f := schema.Field{Name: n.name, Type: n.typ}
		data := l.values[int(cell.offset) : int(cell.offset)+int(cell.length)]
		switch n.typ {
		case FieldInt:
			_ = json.Unmarshal(data, &f.Int)
		case FieldFloat:
			_ = json.Unmarshal(data, &f.Float)
		case FieldBool:
			_ = json.Unmarshal(data, &f.Bool)
		case FieldDuration:
			f.Duration = cell.duration
		case FieldEnum:
			_ = json.Unmarshal(data, &f.Enum)
		case FieldInts:
			_ = json.Unmarshal(data, &f.Ints)
		case FieldFloats:
			_ = json.Unmarshal(data, &f.Floats)
		case FieldEnums:
			_ = json.Unmarshal(data, &f.Enums)
		case FieldObject:
			f.Object = copyStaged(l, s, n.children)
		}
		out = append(out, f)
	}
	return out
}
