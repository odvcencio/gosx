// Package telemetryfields shares portable, typed domain views and bounded JSON.
package telemetryfields

import (
	"math"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"m31labs.dev/gosx/internal/telemetryerr"
)

// FieldType identifies a declared scalar, bounded array, or object. There is
// deliberately no free-form string, time, raw JSON, or interface value.
type FieldType uint8

const (
	FieldInt FieldType = iota
	FieldFloat
	FieldBool
	FieldDuration
	FieldEnum
	FieldInts
	FieldFloats
	FieldEnums
	FieldObject
)

// Fields is a copied domain projection. Changing a returned field or slice
// cannot change the source activity or record. Duration values encode in ms.
type Fields []Field

// Field contains only the value selected by Type. Enum values originate in a
// trusted codec's declared whitelist; they are never learned from client data.
type Field struct {
	Name     string
	Type     FieldType
	Int      int64
	Float    float64
	Bool     bool
	Duration time.Duration
	Enum     string
	Ints     []int64
	Floats   []float64
	Enums    []string
	Object   Fields
}

// MarshalJSON emits compact, lexicographically ordered fields without HTML
// escaping. It also bounds altered views, including cyclic object slices.
func (f Fields) MarshalJSON() ([]byte, error) {
	return AppendFields(make([]byte, 0, 4096), f)
}

// AppendFields is for framework callers with a reserved, reusable byte arena.
func AppendFields(dst []byte, f Fields) ([]byte, error) {
	b := fieldJSON{buf: dst}
	b.fields(f, 0)
	return b.buf, b.err
}

// AppendValue encodes a declared value into bounded framework staging scratch.
func AppendValue(dst []byte, f Field) ([]byte, error) {
	b := fieldJSON{buf: dst}
	b.value(f, 0)
	return b.buf, b.err
}

type fieldJSON struct {
	buf   []byte
	err   error
	count int
}

func (b *fieldJSON) add(v ...byte) {
	if b.err != nil {
		return
	}
	if len(v) > 4096-len(b.buf) {
		b.err = telemetryerr.ErrFieldBudget
		return
	}
	b.buf = append(b.buf, v...)
}

func (b *fieldJSON) fields(fields Fields, depth int) {
	if depth > 2 || len(fields) > 64-b.count {
		b.err = telemetryerr.ErrFieldBudget
		return
	}
	b.count += len(fields)
	var order [64]int
	for i := range fields {
		order[i] = i
	}
	sort.Slice(order[:len(fields)], func(i, j int) bool { return fields[order[i]].Name < fields[order[j]].Name })
	b.add('{')
	for i, n := range order[:len(fields)] {
		f := fields[n]
		if !ValidKey(f.Name) || (i > 0 && f.Name == fields[order[i-1]].Name) {
			b.err = telemetryerr.ErrInvalidOptions
			return
		}
		if i > 0 {
			b.add(',')
		}
		b.string(f.Name)
		b.add(':')
		b.value(f, depth)
	}
	b.add('}')
}

// ValidKey is the bounded identifier check shared with codec registration.
func ValidKey(s string) bool {
	if len(s) == 0 || len(s) > 64 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, c := range []byte(s[1:]) {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func (b *fieldJSON) string(s string) {
	if !utf8.ValidString(s) || len(s) > 64 {
		b.err = telemetryerr.ErrInvalidOptions
		return
	}
	b.add('"')
	for _, c := range []byte(s) {
		switch c {
		case '"', '\\':
			b.add('\\', c)
		case '\b':
			b.add('\\', 'b')
		case '\f':
			b.add('\\', 'f')
		case '\n':
			b.add('\\', 'n')
		case '\r':
			b.add('\\', 'r')
		case '\t':
			b.add('\\', 't')
		default:
			if c < 0x20 {
				const hex = "0123456789abcdef"
				b.add('\\', 'u', '0', '0', hex[c>>4], hex[c&15])
			} else {
				b.add(c)
			}
		}
	}
	b.add('"')
}

func (b *fieldJSON) number(v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		b.err = telemetryerr.ErrInvalidOptions
		return
	}
	var scratch [32]byte
	value := strconv.AppendFloat(scratch[:0], v, 'g', -1, 64)
	// Exponent signs and zero padding are unnecessary in canonical JSON.
	for i, c := range value {
		if c == 'e' {
			prefix, exponent := value[:i+1], value[i+1:]
			b.add(prefix...)
			if exponent[0] == '-' {
				b.add('-')
			}
			if exponent[0] == '+' || exponent[0] == '-' {
				exponent = exponent[1:]
			}
			for len(exponent) > 1 && exponent[0] == '0' {
				exponent = exponent[1:]
			}
			b.add(exponent...)
			return
		}
	}
	b.add(value...)
}

func (b *fieldJSON) value(f Field, depth int) {
	var scratch [24]byte
	switch f.Type {
	case FieldInt:
		b.add(strconv.AppendInt(scratch[:0], f.Int, 10)...)
	case FieldFloat:
		b.number(f.Float)
	case FieldBool:
		b.add(strconv.AppendBool(scratch[:0], f.Bool)...)
	case FieldDuration:
		b.number(float64(f.Duration) / float64(time.Millisecond))
	case FieldEnum:
		b.string(f.Enum)
	case FieldObject:
		if depth >= 2 {
			b.err = telemetryerr.ErrFieldBudget
			return
		}
		b.fields(f.Object, depth+1)
	case FieldInts, FieldFloats, FieldEnums:
		n := len(f.Ints)
		if f.Type == FieldFloats {
			n = len(f.Floats)
		} else if f.Type == FieldEnums {
			n = len(f.Enums)
		}
		if n > 32 {
			b.err = telemetryerr.ErrFieldBudget
			return
		}
		b.add('[')
		for i := 0; i < n; i++ {
			if i > 0 {
				b.add(',')
			}
			switch f.Type {
			case FieldInts:
				b.add(strconv.AppendInt(scratch[:0], f.Ints[i], 10)...)
			case FieldFloats:
				b.number(f.Floats[i])
			case FieldEnums:
				b.string(f.Enums[i])
			}
		}
		b.add(']')
	default:
		b.err = telemetryerr.ErrInvalidOptions
	}
}
