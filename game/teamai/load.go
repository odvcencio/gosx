package teamai

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// LoadJSON reads a teamai JSON data file (see docs/team-ai.md), checks it and
// compiles it with Compile. Every problem is reported in one *SpecError with
// the path of the offending entry, such as
// formations[0](wedge).slots[2](wing).rol: unknown field "rol".
func LoadJSON(data []byte, reg *Registry) (*Book, error) {
	spec, err := DecodeJSON(data)
	if err != nil {
		return nil, err
	}
	return Compile(spec, reg)
}

// DecodeJSON reads a teamai JSON data file into a Spec without compiling it.
// It rejects syntax errors (with line and column), unknown fields, duplicate
// keys, values of the wrong type, and a format or version this build does not
// read.
func DecodeJSON(data []byte) (*Spec, error) {
	if err := checkSyntax(data); err != nil {
		return nil, err
	}
	var head struct {
		Format  string  `json:"format"`
		Version float64 `json:"version"`
	}
	_ = json.Unmarshal(data, &head)
	var is issues
	switch {
	case head.Version != float64(int(head.Version)):
		is.addf("version", "want a whole number")
	case head.Format != Format:
		is.addf("format", "format %q: want %q", head.Format, Format)
	case head.Version < 1:
		is.addf("version", "version %v: want 1", head.Version)
	case head.Version > Version:
		is.addf("version", "version %v is newer than this build reads (%d)", head.Version, Version)
	}
	if err := is.err(); err != nil {
		return nil, err
	}
	walkJSON(&is, "", data, reflect.TypeOf(Spec{}))
	if err := is.err(); err != nil {
		return nil, err
	}
	var spec Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		// walkJSON checks every value, so this is a bug if it happens.
		return nil, &SpecError{Issues: []Issue{{Msg: err.Error()}}}
	}
	return &spec, nil
}

// checkSyntax reports the first JSON syntax error with its line and column,
// and data after the top-level value.
func checkSyntax(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	var v json.RawMessage
	err := dec.Decode(&v)
	if err == nil {
		if _, terr := dec.Token(); terr != io.EOF {
			off := int(dec.InputOffset())
			return &SpecError{Issues: []Issue{{Path: lineCol(data, off), Msg: "data after the top-level object"}}}
		}
		if len(bytes.TrimSpace(v)) == 0 || v[0] != '{' {
			return &SpecError{Issues: []Issue{{Msg: "a teamai data file is a JSON object"}}}
		}
		return nil
	}
	var se *json.SyntaxError
	if errors.As(err, &se) {
		// Offset counts the offending byte.
		return &SpecError{Issues: []Issue{{Path: lineCol(data, int(se.Offset)-1), Msg: "JSON syntax: " + se.Error()}}}
	}
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return &SpecError{Issues: []Issue{{Msg: "JSON syntax: unexpected end of input"}}}
	}
	return &SpecError{Issues: []Issue{{Msg: "JSON: " + err.Error()}}}
}

// lineCol returns the 1-based line and column of byte offset off.
func lineCol(data []byte, off int) string {
	off = max(0, min(off, len(data)))
	line := 1 + bytes.Count(data[:off], []byte{'\n'})
	col := off - bytes.LastIndexByte(data[:off], '\n')
	return fmt.Sprintf("line %d column %d", line, col)
}

var (
	jsonUnmarshaler = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	textUnmarshaler = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
)

type jsonPair struct {
	key string
	raw json.RawMessage
}

// walkJSON checks raw against type t and reports problems under path.
func walkJSON(is *issues, path string, raw json.RawMessage, t reflect.Type) {
	raw = bytes.TrimSpace(raw)
	if reflect.PointerTo(t).Implements(jsonUnmarshaler) || reflect.PointerTo(t).Implements(textUnmarshaler) {
		v := reflect.New(t)
		if err := json.Unmarshal(raw, v.Interface()); err != nil {
			is.addf(path, "%s", strings.TrimPrefix(err.Error(), "json: "))
		}
		return
	}
	if t.Kind() == reflect.Pointer {
		if string(raw) == "null" {
			return
		}
		walkJSON(is, path, raw, t.Elem())
		return
	}
	switch t.Kind() {
	case reflect.Struct:
		pairs, ok := objectPairs(raw)
		if !ok {
			is.addf(path, "want an object")
			return
		}
		fields := jsonFields(t)
		for i, p := range pairs {
			for j := 0; j < i; j++ {
				if pairs[j].key == p.key {
					is.addf(field(path, p.key), "duplicate key")
					break
				}
			}
			ft, ok := fields[p.key]
			if !ok {
				is.addf(field(path, p.key), "unknown field %q%s", p.key, suggest(p.key, t))
				continue
			}
			walkJSON(is, field(path, p.key), p.raw, ft)
		}
	case reflect.Slice:
		if string(raw) == "null" {
			return
		}
		items, ok := arrayItems(raw)
		if !ok {
			is.addf(path, "want an array")
			return
		}
		parent, table := splitLast(path)
		for i, item := range items {
			walkJSON(is, at(parent, table, i, entryName(item, t.Elem())), item, t.Elem())
		}
	case reflect.String:
		var s string
		if json.Unmarshal(raw, &s) != nil {
			is.addf(path, "want a string")
		}
	case reflect.Float64:
		var f float64
		if json.Unmarshal(raw, &f) != nil {
			is.addf(path, "want a number")
		}
	case reflect.Int:
		var n int
		if json.Unmarshal(raw, &n) != nil {
			is.addf(path, "want a whole number")
		}
	case reflect.Bool:
		var b bool
		if json.Unmarshal(raw, &b) != nil {
			is.addf(path, "want true or false")
		}
	default:
		is.addf(path, "unsupported type %v", t)
	}
}

// splitLast splits "a.b.c" into "a.b" and "c".
func splitLast(path string) (string, string) {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[:i], path[i+1:]
	}
	return "", path
}

// objectPairs returns the members of a JSON object in document order.
func objectPairs(raw json.RawMessage) ([]jsonPair, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, false
	}
	var pairs []jsonPair
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, _ := tok.(string)
		var v json.RawMessage
		if dec.Decode(&v) != nil {
			return nil, false
		}
		pairs = append(pairs, jsonPair{key: key, raw: v})
	}
	return pairs, true
}

func arrayItems(raw json.RawMessage) ([]json.RawMessage, bool) {
	var items []json.RawMessage
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &items) != nil {
		return nil, false
	}
	return items, true
}

// jsonFields maps the JSON keys of struct t, embedded structs included, to
// their types.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			for k, v := range jsonFields(f.Type) { //gosx:allow map-range copying a map of field types; order does not matter
				out[k] = v
			}
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		out[name] = f.Type
	}
	return out
}

// entryName returns the name an array entry is known by in error paths: the
// same key Compile uses.
func entryName(raw json.RawMessage, t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return ""
	}
	pairs, ok := objectPairs(bytes.TrimSpace(raw))
	if !ok {
		return ""
	}
	get := func(key string) string {
		for _, p := range pairs {
			if p.key == key {
				var s string
				if json.Unmarshal(p.raw, &s) == nil {
					return s
				}
			}
		}
		return ""
	}
	switch t {
	case reflect.TypeOf(SegmentSpec{}), reflect.TypeOf(StepSpec{}):
		return get("label")
	case reflect.TypeOf(BranchSpec{}):
		return get("goto")
	case reflect.TypeOf(BandOffsetSpec{}):
		return get("band")
	case reflect.TypeOf(InterruptSpec{}):
		return get("when")
	case reflect.TypeOf(PlanAssignSpec{}):
		return get("slot") + get("role")
	}
	return get("name")
}

// suggest returns a hint naming the known field closest to key.
func suggest(key string, t reflect.Type) string {
	best, bestD := "", 3
	fields := jsonFields(t)
	names := make([]string, 0, len(fields))
	for k := range fields { //gosx:allow map-range collecting keys that are sorted next
		names = append(names, k)
	}
	sortStrings(names)
	for _, n := range names {
		if d := editDistance(key, n); d < bestD {
			best, bestD = n, d
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf(" (did you mean %q?)", best)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// editDistance is the Levenshtein distance between two short strings.
func editDistance(a, b string) int {
	if len(a) > 32 || len(b) > 32 {
		return 99
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
