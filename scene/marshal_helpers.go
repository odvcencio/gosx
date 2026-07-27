package scene

import (
	"encoding/json"
	"strings"
)

// linePointWire is a Vector3 that always emits x/y/z as zero-valued
// fields — unlike Vector3's default omitempty form. The legacy
// map-based marshaling of ObjectIR.Points always included all three
// coordinates (via an explicit map[string]any{"x": p.X, "y": p.Y,
// "z": p.Z}), so preserving that wire shape matters for the JS
// consumer that reads these arrays.
type linePointWire struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// LinePoints is the polyline vertex list of an ObjectIR. It is a named
// []Vector3, so existing code that assigns, ranges, indexes or appends
// a []Vector3 keeps compiling unchanged.
//
// It carries the MarshalJSON that ObjectIR used to carry. ObjectIR is
// ~1 KB, so a MarshalJSON on ObjectIR forced encoding/json to box a
// full copy per record, encode it into a private buffer, copy the
// result out, and re-scan every byte through appendCompact. Moving the
// method down to the field that needs it costs the same work only for
// the rare object that has line points, and lets the ~1 KB object
// record encode straight through reflection.
//
// A profile of a 1000-object scene put appendCompact at 43% of the
// marshal CPU and the ObjectIR wrapper at 57% of its allocated bytes.
type LinePoints []Vector3

// MarshalJSON emits every coordinate of every vertex, including zeros.
// Vector3 tags x, y and z omitempty, so a point like Vec3(0, 2, 0)
// would otherwise reach the browser as {"y":2} and move the line.
func (p LinePoints) MarshalJSON() ([]byte, error) {
	return json.Marshal(toLinePointsWire(p))
}

// toLinePointsWire converts a []Vector3 to []linePointWire so it
// marshals with all three coordinates present.
func toLinePointsWire(pts []Vector3) []linePointWire {
	if len(pts) == 0 {
		return nil
	}
	out := make([]linePointWire, len(pts))
	for i, p := range pts {
		out[i] = linePointWire{X: p.X, Y: p.Y, Z: p.Z}
	}
	return out
}

// jsonString returns the JSON string-literal form of s — i.e. the value
// strconv.Quote would give but with HTML-safe escaping matching
// encoding/json's default.
//
// We can't use strconv.Quote directly because it produces Go-style escapes
// (\xHH) that aren't valid JSON. encoding/json's escaping rules are:
//
//   - " → \"
//   - \ → \\
//   - control bytes < 0x20 → \u00HH
//   - runes 0x20–0x7E pass through
//   - 0x7F and above pass through as UTF-8 (json.Marshal is UTF-8 safe)
//
// For the fast path — when s has no character that needs escaping — we
// return `"s"` with zero heap allocations beyond the final string cast.
func jsonString(s string) string {
	if !needsJSONEscape(s) {
		var b strings.Builder
		b.Grow(len(s) + 2)
		b.WriteByte('"')
		b.WriteString(s)
		b.WriteByte('"')
		return b.String()
	}

	var b strings.Builder
	b.Grow(len(s) + 4)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case c < 0x20:
			b.WriteString(`\u00`)
			const hex = "0123456789abcdef"
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// needsJSONEscape reports whether any byte in s requires escaping in a
// JSON string literal. The common case (class names, tag names, hex
// colors, kind strings) returns false immediately on the first character.
func needsJSONEscape(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' || c < 0x20 {
			return true
		}
	}
	return false
}
