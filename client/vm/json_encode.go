package vm

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"unicode/utf8"

	"m31labs.dev/gosx/island/program"
)

// AppendJSON appends the JSON text of v to out. The text is byte for byte what
// json.Marshal(v.ToAny()) writes, without building the tree of interface values
// that call goes through: members of an object in byte order, the characters
// <, > and & escaped, and floats in the shortest form that reads back the same.
// It returns an error where json.Marshal does, for a float that is not finite.
//
// patch_json_test.go and json_encode_test.go pin the equality against
// encoding/json on a randomized corpus.
func AppendJSON(out []byte, v Value) ([]byte, error) {
	return appendJSONValue(out, v, 0)
}

func appendJSONValue(out []byte, v Value, depth int) ([]byte, error) {
	// A Value can contain itself. ToAny stands in a string for what is nested
	// too deep, and so does this.
	if depth > maxValueRecursionDepth {
		return AppendJSONString(out, cycleSentinel), nil
	}
	if v.isList() {
		items := v.list()
		out = append(out, '[')
		for i := range items {
			if i > 0 {
				out = append(out, ',')
			}
			var err error
			if out, err = appendJSONValue(out, items[i], depth+1); err != nil {
				return out, err
			}
		}
		return append(out, ']'), nil
	}
	if v.isMap() {
		return appendJSONObject(out, v.dict(), depth)
	}
	switch v.Type {
	case program.TypeString:
		return AppendJSONString(out, v.text()), nil
	case program.TypeBool:
		if v.truth() {
			return append(out, "true"...), nil
		}
		return append(out, "false"...), nil
	case program.TypeInt:
		return strconv.AppendInt(out, v.integer(), 10), nil
	case program.TypeFloat:
		return appendJSONFloat(out, v.num)
	}
	return append(out, "null"...), nil
}

func appendJSONObject(out []byte, fields map[string]Value, depth int) ([]byte, error) {
	out = append(out, '{')
	if len(fields) == 0 {
		return append(out, '}'), nil
	}
	var scratch [16]string
	names := scratch[:0]
	for name := range fields {
		names = append(names, name)
	}
	slices.Sort(names)
	for i, name := range names {
		if i > 0 {
			out = append(out, ',')
		}
		out = AppendJSONString(out, name)
		out = append(out, ':')
		var err error
		if out, err = appendJSONValue(out, fields[name], depth+1); err != nil {
			return out, err
		}
	}
	return append(out, '}'), nil
}

var errNotFinite = errors.New("json: unsupported value: not a finite number")

// appendJSONFloat writes f as encoding/json does for a float64: the shortest
// digits that read back the same, in exponent form below 1e-6 and from 1e21, and
// with the exponent's leading zero removed (1e-09 becomes 1e-9).
func appendJSONFloat(out []byte, f float64) ([]byte, error) {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return out, errNotFinite
	}
	format := byte('f')
	if abs := math.Abs(f); abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	out = strconv.AppendFloat(out, f, format, -1, 64)
	if format == 'e' {
		n := len(out)
		if n >= 4 && out[n-4] == 'e' && out[n-3] == '-' && out[n-2] == '0' {
			out[n-2] = out[n-1]
			out = out[:n-1]
		}
	}
	return out, nil
}

const lowerHex = "0123456789abcdef"

// lineSeparator and paragraphSeparator are valid inside a JSON string but
// break a JavaScript source literal, so encoding/json escapes both.
const (
	lineSeparator      = '\u2028'
	paragraphSeparator = '\u2029'
)

// jsonSafeByte reports whether a byte below 0x80 can go into a JSON string
// unescaped.
//
// The set matches encoding/json's htmlSafeSet: printable ASCII minus the quote
// and the backslash, minus '<', '>' and '&'. encoding/json escapes those three
// by default so a payload cannot break out of a <script> block, and the output
// must match byte for byte.
func jsonSafeByte(b byte) bool {
	if b < 0x20 {
		return false
	}
	switch b {
	case '"', '\\', '<', '>', '&':
		return false
	}
	return true
}

// AppendJSONString appends s as a quoted JSON string, matching
// encoding/json.Marshal with HTML escaping left on.
func AppendJSONString(out []byte, s string) []byte {
	out = append(out, '"')
	start := 0
	for i := 0; i < len(s); {
		b := s[i]
		if b < utf8.RuneSelf {
			if jsonSafeByte(b) {
				i++
				continue
			}
			out = append(out, s[start:i]...)
			switch b {
			case '\\', '"':
				out = append(out, '\\', b)
			case '\b':
				out = append(out, '\\', 'b')
			case '\f':
				out = append(out, '\\', 'f')
			case '\n':
				out = append(out, '\\', 'n')
			case '\r':
				out = append(out, '\\', 'r')
			case '\t':
				out = append(out, '\\', 't')
			default:
				// Control bytes other than \b, \f, \n, \r and \t, plus
				// <, > and &.
				out = append(out, '\\', 'u', '0', '0', lowerHex[b>>4], lowerHex[b&0xF])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8 becomes an escaped replacement character, as
			// encoding/json does.
			out = append(out, s[start:i]...)
			out = append(out, `\ufffd`...)
			i += size
			start = i
			continue
		}
		if r == lineSeparator || r == paragraphSeparator {
			// Line and paragraph separators are valid JSON but break a
			// JavaScript source literal, so encoding/json escapes them.
			out = append(out, s[start:i]...)
			out = append(out, '\\', 'u', '2', '0', '2', lowerHex[r&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	out = append(out, s[start:]...)
	return append(out, '"')
}
