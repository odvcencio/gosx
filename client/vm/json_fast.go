package vm

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"m31labs.dev/gosx/island/program"
)

// fastDecodeMaxDepth bounds the recursion of decodeJSONFast. Deeper documents
// are left to the reference decoder.
const fastDecodeMaxDepth = 128

// decodeJSONFast decodes one JSON document straight into a Value, with no
// intermediate tree of interfaces. It accepts only text it can decode exactly
// as the reference decoder behind ValueFromJSON would; for anything else, such
// as malformed text, an escaped surrogate, bytes that are not UTF-8, or very
// deep nesting, ok is false and the caller uses the reference decoder, which
// also supplies the error message.
func decodeJSONFast(text string) (value Value, ok bool) {
	r := jsonReader{text: text}
	r.skipSpace()
	value, ok = r.value(0)
	if !ok {
		return Value{}, false
	}
	r.skipSpace()
	return value, r.pos == len(text)
}

type jsonReader struct {
	text string
	pos  int
	// items and pairs hold the elements and members of the containers being
	// read. A container copies its own range into a slice or map of exactly the
	// right size, so nothing grows while it is read.
	items []Value
	pairs []jsonPair
}

type jsonPair struct {
	name  string
	value Value
}

func (r *jsonReader) skipSpace() {
	r.pos = skipJSONSpace(r.text, r.pos)
}

func (r *jsonReader) value(depth int) (Value, bool) {
	if r.pos >= len(r.text) || depth > fastDecodeMaxDepth {
		return Value{}, false
	}
	switch c := r.text[r.pos]; {
	case c == '{':
		return r.object(depth)
	case c == '[':
		return r.array(depth)
	case c == '"':
		s, ok := r.str()
		return StringVal(s), ok
	case c == 't':
		return r.literal("true", BoolVal(true))
	case c == 'f':
		return r.literal("false", BoolVal(false))
	case c == 'n':
		return r.literal("null", ZeroValue(program.TypeAny))
	case c == '-' || (c >= '0' && c <= '9'):
		end := scanJSONNumber(r.text, r.pos)
		if end < 0 {
			return Value{}, false
		}
		number, ok := numberValueFromToken(r.text[r.pos:end])
		r.pos = end
		return number, ok
	}
	return Value{}, false
}

func (r *jsonReader) literal(word string, v Value) (Value, bool) {
	end := r.pos + len(word)
	if end > len(r.text) || r.text[r.pos:end] != word || (end < len(r.text) && !isJSONDelimiter(r.text[end])) {
		return Value{}, false
	}
	r.pos = end
	return v, true
}

func (r *jsonReader) array(depth int) (Value, bool) {
	r.pos++
	r.skipSpace()
	if r.pos < len(r.text) && r.text[r.pos] == ']' {
		r.pos++
		return ArrayVal([]Value{}), true
	}
	base := len(r.items)
	for {
		item, ok := r.value(depth + 1)
		if !ok {
			return Value{}, false
		}
		r.items = append(r.items, item)
		r.skipSpace()
		if r.pos >= len(r.text) {
			return Value{}, false
		}
		switch r.text[r.pos] {
		case ',':
			r.pos++
			r.skipSpace()
		case ']':
			r.pos++
			items := make([]Value, len(r.items)-base)
			copy(items, r.items[base:])
			r.items = r.items[:base]
			return ArrayVal(items), true
		default:
			return Value{}, false
		}
	}
}

func (r *jsonReader) object(depth int) (Value, bool) {
	r.pos++
	r.skipSpace()
	if r.pos < len(r.text) && r.text[r.pos] == '}' {
		r.pos++
		return ObjectVal(map[string]Value{}), true
	}
	base := len(r.pairs)
	for {
		if r.pos >= len(r.text) || r.text[r.pos] != '"' {
			return Value{}, false
		}
		name, ok := r.str()
		if !ok {
			return Value{}, false
		}
		r.skipSpace()
		if r.pos >= len(r.text) || r.text[r.pos] != ':' {
			return Value{}, false
		}
		r.pos++
		r.skipSpace()
		field, ok := r.value(depth + 1)
		if !ok {
			return Value{}, false
		}
		r.pairs = append(r.pairs, jsonPair{name: name, value: field})
		r.skipSpace()
		if r.pos >= len(r.text) {
			return Value{}, false
		}
		switch r.text[r.pos] {
		case ',':
			r.pos++
			r.skipSpace()
		case '}':
			r.pos++
			members := r.pairs[base:]
			fields := make(map[string]Value, len(members))
			for _, member := range members {
				fields[member.name] = member.value
			}
			r.pairs = r.pairs[:base]
			return ObjectVal(fields), true
		default:
			return Value{}, false
		}
	}
}

// str reads the string that opens at r.pos. The result never shares memory with
// the text, so a small value does not keep a large document alive.
func (r *jsonReader) str() (string, bool) {
	start := r.pos + 1
	i := start
	ascii := true
	for i < len(r.text) {
		c := r.text[i]
		if c == '"' {
			s := r.text[start:i]
			if !ascii && !utf8.ValidString(s) {
				return "", false
			}
			r.pos = i + 1
			return strings.Clone(s), true
		}
		if c == '\\' {
			return r.escapedStr(start, i)
		}
		if c < 0x20 {
			return "", false
		}
		if c >= utf8.RuneSelf {
			ascii = false
		}
		i++
	}
	return "", false
}

// escapedStr finishes a string whose first escape is at text[esc].
func (r *jsonReader) escapedStr(start, esc int) (string, bool) {
	var b strings.Builder
	b.WriteString(r.text[start:esc])
	i := esc
	for i < len(r.text) {
		c := r.text[i]
		switch {
		case c == '"':
			s := b.String()
			if !utf8.ValidString(s) {
				return "", false
			}
			r.pos = i + 1
			return s, true
		case c == '\\':
			if i+1 >= len(r.text) {
				return "", false
			}
			i++
			switch r.text[i] {
			case '"', '\\', '/':
				b.WriteByte(r.text[i])
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'u':
				if i+4 >= len(r.text) {
					return "", false
				}
				code, ok := hex4(r.text[i+1 : i+5])
				if !ok || (code >= 0xD800 && code < 0xE000) {
					return "", false
				}
				b.WriteRune(rune(code))
				i += 4
			default:
				return "", false
			}
			i++
		case c < 0x20:
			return "", false
		default:
			b.WriteByte(c)
			i++
		}
	}
	return "", false
}

func hex4(s string) (rune, bool) {
	var n rune
	for i := 0; i < 4; i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			n = n<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			n = n<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			n = n<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return n, true
}

// numberValueFromToken gives the Value that ValueFromJSON builds for a JSON
// number token: an exact int64 when the token is one, else a float, which
// becomes an integer when it has no fraction and fits. ok is false when the
// token is out of range for float64; the Value is then the decoder's own
// fallback, which ValueFromJSON never reaches because it rejects such tokens.
func numberValueFromToken(token string) (value Value, ok bool) {
	if n, err := strconv.ParseInt(token, 10, 64); err == nil {
		return Int64Val(n), true
	}
	f, err := strconv.ParseFloat(token, 64)
	return parseAnyValue(f), err == nil
}

func skipJSONSpace(text string, i int) int {
	for i < len(text) {
		switch text[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

func isJSONDelimiter(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', ',', ']', '}':
		return true
	}
	return false
}

// scanJSONNumber returns the index just after the number token at text[i], or
// -1 when the token does not follow the JSON number grammar.
func scanJSONNumber(text string, i int) int {
	j := i
	if j < len(text) && text[j] == '-' {
		j++
	}
	switch {
	case j >= len(text):
		return -1
	case text[j] == '0':
		j++
	case text[j] >= '1' && text[j] <= '9':
		for j < len(text) && text[j] >= '0' && text[j] <= '9' {
			j++
		}
	default:
		return -1
	}
	if j < len(text) && text[j] == '.' {
		j++
		start := j
		for j < len(text) && text[j] >= '0' && text[j] <= '9' {
			j++
		}
		if j == start {
			return -1
		}
	}
	if j < len(text) && (text[j] == 'e' || text[j] == 'E') {
		j++
		if j < len(text) && (text[j] == '+' || text[j] == '-') {
			j++
		}
		start := j
		for j < len(text) && text[j] >= '0' && text[j] <= '9' {
			j++
		}
		if j == start {
			return -1
		}
	}
	if j < len(text) && !isJSONDelimiter(text[j]) {
		return -1
	}
	return j
}
