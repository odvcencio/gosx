package scene

import "strings"

// linePointWire is a Vector3 that always emits x/y/z as zero-valued
// fields — unlike Vector3's default omitempty form. The legacy
// map-based marshaling of ObjectIR.Points always included all three
// coordinates (via an explicit map[string]any{"x": p.X, "y": p.Y,
// "z": p.Z}), so preserving that wire shape matters for the JS
// consumer that reads these arrays.
//
// Used by ObjectIR.MarshalJSON, which shadows the embedded alias's
// Points field with a []linePointWire.
type linePointWire struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
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

