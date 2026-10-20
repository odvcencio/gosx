package scene

import (
	"reflect"
	"slices"
	"unicode/utf8"
)

// Only built-in scalars have the direct JSON semantics this rejection models.
// Named types may implement MarshalJSON; structs, maps and other streams still
// use the reference comparison. Discover fields once so new plain ObjectIR
// fields automatically participate without maintaining a second schema.
var objectScalarFields = func() []int {
	t := reflect.TypeFor[ObjectIR]()
	var fields []int
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() || field.Tag.Get("json") == "-" {
			continue
		}
		typ := field.Type
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if typ.PkgPath() != "" {
			continue
		}
		switch typ.Kind() {
		case reflect.Bool, reflect.String, reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint64, reflect.Float64:
			fields = append(fields, i)
		}
	}
	return fields
}()

// objectRecordJSONDifferent proves a wire difference after the structural
// equality walk declines. A small interaction/material edit must not encode
// both entire mesh records just to discover that edit. Signed scalar zeros
// retain the reference fallback because omitempty can erase their difference.
func objectRecordJSONDifferent(a, b *ObjectIR) bool {
	left, right := reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem()
	for _, i := range objectScalarFields {
		x, y := left.Field(i), right.Field(i)
		if x.Kind() == reflect.Pointer {
			if x.IsNil() != y.IsNil() {
				return true
			}
			if x.IsNil() {
				continue
			}
			x, y = x.Elem(), y.Elem()
		}
		switch x.Kind() {
		case reflect.Bool:
			if x.Bool() != y.Bool() {
				return true
			}
		case reflect.String:
			// JSON replaces malformed UTF-8. Different invalid byte strings
			// can therefore encode identically and need the reference fallback.
			if x.String() != y.String() && utf8.ValidString(x.String()) && utf8.ValidString(y.String()) {
				return true
			}
		case reflect.Int, reflect.Int64:
			if x.Int() != y.Int() {
				return true
			}
		case reflect.Uint, reflect.Uint64:
			if x.Uint() != y.Uint() {
				return true
			}
		case reflect.Float64:
			if x.Float() != y.Float() {
				return true
			}
		}
	}
	return meshVerticesJSONDifferent(a.Vertices, b.Vertices)
}

func meshVerticesJSONDifferent(a, b *MeshVertices) bool {
	if a == nil || b == nil {
		return (a == nil) != (b == nil)
	}
	if a.Count != b.Count || a.Immutable != b.Immutable || a.Dynamic != b.Dynamic ||
		(a.Revision == nil) != (b.Revision == nil) ||
		(a.Revision != nil && *a.Revision != *b.Revision) || !slices.Equal(a.Indices, b.Indices) {
		return true
	}
	for _, pair := range [4][2][]float64{{a.Positions, b.Positions}, {a.Normals, b.Normals}, {a.UVs, b.UVs}, {a.Tangents, b.Tangents}} {
		if len(pair[0]) != len(pair[1]) {
			return true
		}
		for i, v := range pair[0] {
			// Numeric array elements always ship, including signed zeros.
			if !provenFloatEqual(v, pair[1][i]) {
				return true
			}
		}
	}
	return false
}
