// Package query provides type-safe binding between URL query parameters and Go
// structs — the "URL as a first-class, typed state container" pattern.
//
// It is deliberately dependency-free and net/http-free so the same Decode/Encode
// pair works on the server (via route.RouteContext.QueryInto) and inside an
// island compiled to WebAssembly, where the browser URL is the single source of
// truth for shareable, back-button-friendly UI state.
//
// Fields are bound with a `query` struct tag:
//
//	type Filters struct {
//	    Q     string   `query:"q"`
//	    Page  int      `query:"page,default=1"`
//	    Open  bool     `query:"open"`
//	    Tags  []string `query:"tags"`
//	    Skip  string   `query:"-"`   // never bound
//	}
//
// Supported field kinds: string, bool, int/int8/.../int64, uint/.../uint64,
// float32/float64, and []string (repeated params and/or comma-separated).
// Missing parameters fall back to the tag's default; an unparseable value
// returns an error naming the offending field.
package query

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

// Decode populates dst (a non-nil pointer to a struct) from values.
func Decode(values url.Values, dst any) error {
	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("query: Decode requires a non-nil pointer to a struct, got %T", dst)
	}
	sv := rv.Elem()
	st := sv.Type()
	for i := 0; i < st.NumField(); i++ {
		field := st.Field(i)
		if !field.IsExported() {
			continue
		}
		name, def, ok := parseTag(field)
		if !ok {
			continue
		}
		fv := sv.Field(i)
		present := values.Has(name)
		raw := values.Get(name)
		if !present {
			if def == "" {
				continue // leave the zero value
			}
			raw = def
		}
		if err := setField(fv, field, name, raw, present, values[name]); err != nil {
			return err
		}
	}
	return nil
}

// Encode renders src (a struct or pointer to struct) into url.Values using the
// same tags. A field is emitted only when its string form differs from the
// tag default (empty default for untagged-default fields), so Decode(Encode(v))
// round-trips and the resulting URLs stay clean.
func Encode(src any) (url.Values, error) {
	rv := reflect.ValueOf(src)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, fmt.Errorf("query: Encode received a nil pointer")
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("query: Encode requires a struct or pointer to struct, got %T", src)
	}
	out := url.Values{}
	st := rv.Type()
	for i := 0; i < st.NumField(); i++ {
		field := st.Field(i)
		if !field.IsExported() {
			continue
		}
		name, def, ok := parseTag(field)
		if !ok {
			continue
		}
		fv := rv.Field(i)
		if fv.Kind() == reflect.Slice {
			if fv.Type().Elem().Kind() == reflect.String {
				for _, s := range fv.Interface().([]string) {
					out.Add(name, s)
				}
			}
			continue
		}
		s := scalarString(fv)
		if s == def {
			continue // matches default (or empty) — omit for a clean URL
		}
		out.Set(name, s)
	}
	return out, nil
}

// parseTag returns the query name and default for a field, or ok=false when the
// field is unbound (no `query` tag, or `query:"-"`).
func parseTag(field reflect.StructField) (name, def string, ok bool) {
	tag, has := field.Tag.Lookup("query")
	if !has || tag == "-" {
		return "", "", false
	}
	parts := strings.Split(tag, ",")
	name = strings.TrimSpace(parts[0])
	if name == "" || name == "-" {
		return "", "", false
	}
	for _, opt := range parts[1:] {
		opt = strings.TrimSpace(opt)
		if v, found := strings.CutPrefix(opt, "default="); found {
			def = v
		}
	}
	return name, def, true
}

func setField(fv reflect.Value, field reflect.StructField, name, raw string, present bool, all []string) error {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		// A bare `?flag` (present, empty value) reads as true.
		if present && raw == "" {
			fv.SetBool(true)
			return nil
		}
		b, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			if strings.EqualFold(strings.TrimSpace(raw), "on") {
				fv.SetBool(true)
				return nil
			}
			return fmt.Errorf("query: field %q: invalid bool %q", name, raw)
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return fmt.Errorf("query: field %q: invalid integer %q", name, raw)
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return fmt.Errorf("query: field %q: invalid unsigned integer %q", name, raw)
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return fmt.Errorf("query: field %q: invalid number %q", name, raw)
		}
		fv.SetFloat(f)
	case reflect.Slice:
		if field.Type.Elem().Kind() != reflect.String {
			return fmt.Errorf("query: field %q: only []string slices are supported", name)
		}
		var items []string
		source := all
		if !present && raw != "" {
			source = []string{raw} // default value
		}
		for _, v := range source {
			for _, part := range strings.Split(v, ",") {
				if part = strings.TrimSpace(part); part != "" {
					items = append(items, part)
				}
			}
		}
		fv.Set(reflect.ValueOf(items))
	default:
		return fmt.Errorf("query: field %q: unsupported kind %s", name, fv.Kind())
	}
	return nil
}

func scalarString(fv reflect.Value) string {
	switch fv.Kind() {
	case reflect.String:
		return fv.String()
	case reflect.Bool:
		if fv.Bool() {
			return "true"
		}
		return "false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(fv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(fv.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(fv.Float(), 'g', -1, 64)
	default:
		return ""
	}
}
