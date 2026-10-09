package telemetryrecord

import (
	"m31labs.dev/gosx/internal/telemetryerr"
	"m31labs.dev/gosx/internal/telemetryfields"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type jsonWriter struct {
	buf []byte
	err error
}

func (w *jsonWriter) add(b ...byte) {
	if w.err != nil {
		return
	}
	if len(b) > MaxRecordBytes-len(w.buf) {
		w.err = telemetryerr.ErrFieldBudget
		return
	}
	w.buf = append(w.buf, b...)
}
func (w *jsonWriter) text(s string) { w.add([]byte(s)...) }
func (w *jsonWriter) quoted(s string) {
	w.add('"')
	for _, c := range []byte(s) {
		switch c {
		case '"', '\\':
			w.add('\\', c)
		case '\b':
			w.text(`\b`)
		case '\f':
			w.text(`\f`)
		case '\n':
			w.text(`\n`)
		case '\r':
			w.text(`\r`)
		case '\t':
			w.text(`\t`)
		default:
			if c < 0x20 {
				const hex = "0123456789abcdef"
				w.add('\\', 'u', '0', '0', hex[c>>4], hex[c&15])
			} else {
				w.add(c)
			}
		}
	}
	w.add('"')
}
func empty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	default:
		return v.IsZero()
	}
}
func (w *jsonWriter) value(v reflect.Value) {
	if w.err != nil {
		return
	}
	if v.Type() == reflect.TypeFor[time.Time]() {
		t := v.Interface().(time.Time)
		w.quoted(t.UTC().Format(time.RFC3339Nano))
		return
	}
	if v.Type() == reflect.TypeFor[telemetryfields.Fields]() {
		b, err := telemetryfields.AppendFields(nil, v.Interface().(telemetryfields.Fields))
		if err != nil {
			w.err = err
			return
		}
		w.add(b...)
		return
	}
	var scratch [32]byte
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			w.text("null")
		} else {
			w.value(v.Elem())
		}
	case reflect.Struct:
		w.add('{')
		first := true
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")
			if tag[0] == "-" {
				continue
			}
			omit := false
			for _, opt := range tag[1:] {
				if opt == "omitempty" && empty(v.Field(i)) || opt == "omitzero" && v.Field(i).IsZero() {
					omit = true
				}
			}
			if omit {
				continue
			}
			if !first {
				w.add(',')
			}
			first = false
			w.quoted(tag[0])
			w.add(':')
			w.value(v.Field(i))
		}
		w.add('}')
	case reflect.Slice, reflect.Array:
		w.add('[')
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				w.add(',')
			}
			w.value(v.Index(i))
		}
		w.add(']')
	case reflect.String:
		w.quoted(v.String())
	case reflect.Bool:
		w.add(strconv.AppendBool(scratch[:0], v.Bool())...)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		w.add(strconv.AppendInt(scratch[:0], v.Int(), 10)...)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		w.add(strconv.AppendUint(scratch[:0], v.Uint(), 10)...)
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			w.err = telemetryerr.ErrInvalidOptions
			return
		}
		b := strconv.AppendFloat(scratch[:0], f, 'g', -1, 64)
		if i := strings.IndexByte(string(b), 'e'); i >= 0 {
			w.add(b[:i+1]...)
			exp := b[i+1:]
			if exp[0] == '-' {
				w.add('-')
			}
			if exp[0] == '+' || exp[0] == '-' {
				exp = exp[1:]
			}
			for len(exp) > 1 && exp[0] == '0' {
				exp = exp[1:]
			}
			w.add(exp...)
		} else {
			w.add(b...)
		}
	default:
		w.err = telemetryerr.ErrInvalidOptions
	}
}
