package wire

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx/controller"
	"m31labs.dev/gosx/hydrate"
)

func TestReferencesControllerResourceGates(t *testing.T) {
	gates := []string{``, `,"immediate":true`, `,"immediate":false`, `,"immediate":false,"pollMs":100`, `,"immediate":false,"refreshSignal":"reload"`, `,"pollMs":100,"refreshSignal":"reload"`}
	for i, gate := range gates {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			body := `<script type="application/json" id="gosx-manifest">{"version":"0.1.0","controllers":[{"id":"c","config":{"resources":[{"url":"/active.json","output":"loaded"` + gate + `}]}}]}</script>`
			for _, placement := range []string{"standalone", "with-known-references"} {
				candidate := body
				if placement == "with-known-references" {
					candidate = strings.Replace(body, `"version":"0.1.0",`, `"version":"0.1.0","islands":[{"programRef":"/known.bin"}],"runtime":{"path":"/known.wasm"},`, 1)
				}
				set, err := ScanReferences([]byte(candidate), KindDocument)
				if err != nil || set.Complete {
					t.Fatalf("unmodelled controller resource certified complete: %s %+v err=%v", placement, set, err)
				}
				if placement == "with-known-references" && !reflect.DeepEqual(set.Resources, []Reference{{"/known.bin", KindProgram, false}, {"/known.wasm", KindWASM, false}}) {
					t.Fatalf("controller uncertainty lost known references: %+v", set)
				}
			}
		})
	}
}

// Walk the producer schema independently of the scanner and policy table.
// Include all scalar fields, not just strings, so new loading gates also require
// a classification. Map keys/values and opaque JSON are covered at their owner.
func manifestSchemaFields(typ reflect.Type, path string, fields map[string]reflect.Type) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	opaque := typ == reflect.TypeFor[json.RawMessage]() || typ.Kind() == reflect.Interface
	switch {
	case opaque:
		fields[path] = typ
	case typ.Kind() == reflect.Struct:
		if typ == reflect.TypeFor[controller.Config]() {
			fields[path] = typ
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if f.PkgPath != "" || name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			next := name
			if path != "" {
				next = path + "." + name
			}
			manifestSchemaFields(f.Type, next, fields)
		}
	case typ.Kind() == reflect.Map:
		fields[path] = typ
		elem := typ.Elem()
		for elem.Kind() == reflect.Slice {
			elem = elem.Elem()
		}
		if elem.Kind() == reflect.Struct {
			manifestSchemaFields(typ.Elem(), path+"{}", fields)
		}
	case typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array:
		if typ.Elem().Kind() == reflect.Struct {
			if typ.Elem() == reflect.TypeFor[controller.FetchResource]() {
				fields[path] = typ
			}
			manifestSchemaFields(typ.Elem(), path+"[]", fields)
		} else {
			fields[path] = typ
		}
	default:
		fields[path] = typ
	}
}

func TestManifestFieldClassificationCoversSchema(t *testing.T) {
	fields := map[string]reflect.Type{}
	manifestSchemaFields(reflect.TypeFor[hydrate.Manifest](), "", fields)
	for path := range fields {
		policy, ok := manifestFieldPolicies[path]
		if !ok {
			t.Errorf("unclassified manifest field %s (%s)", path, fields[path])
			continue
		}
		if policy.loader == "" || policy.rationale == "" {
			t.Errorf("missing loader justification for %s", path)
		}
		if policy.class < manifestFetchedNow || policy.class > manifestMetadata {
			t.Errorf("invalid classification for %s", path)
		}
	}
	for path := range manifestFieldPolicies {
		if _, ok := fields[path]; !ok {
			t.Errorf("stale policy %s", path)
		}
	}
	t.Logf("classified manifest fields=%d", len(fields))
}

func TestReferencesGeneratedManifestFieldPolicies(t *testing.T) {
	fields := map[string]reflect.Type{}
	manifestSchemaFields(reflect.TypeFor[hydrate.Manifest](), "", fields)
	paths := make([]string, 0, len(fields))
	for path := range fields {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	cases := 0
	for _, path := range paths {
		policy, ok := manifestFieldPolicies[path]
		if !ok {
			t.Errorf("unclassified field %s", path)
			continue
		}
		for _, mode := range []string{"value", "empty", "fragment"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				value := manifestPolicyTestValue(fields[path], mode)
				// Isolate the field so unrelated metadata cannot witness a drop.
				drops := []referenceDropReason{}
				out := referenceScanner{ReferenceSet: ReferenceSet{Complete: true}, onDrop: func(r referenceDropReason) { drops = append(drops, r) }}
				classifyManifestField(path, value, &out)
				want := dropManifestMetadata
				if policy.modelled && policy.class != manifestMetadata {
					want = dropNestedScan
				}
				if !policy.modelled && mode != "empty" {
					if out.Complete {
						t.Fatalf("unmodelled field dropped: %s value=%v drops=%v", path, value, drops)
					}
				} else {
					if !out.Complete || len(drops) != 1 || drops[0] != want {
						t.Fatalf("wrong field disposition: %s complete=%v drops=%v want=%v", path, out.Complete, drops, want)
					}
				}
				raw := manifestPolicyTestDocument(path, value)
				scanDrops := []referenceDropReason{}
				set, err := scanReferences([]byte(`<script type="application/json" id="gosx-manifest">`+raw+`</script>`), KindDocument, func(reason referenceDropReason) { scanDrops = append(scanDrops, reason) })
				if err != nil {
					t.Fatalf("generated schema fixture invalid: %s: %v JSON=%s", path, err, raw)
				}
				if !policy.modelled && mode != "empty" && set.Complete {
					t.Fatalf("unmodelled field certified complete: %s JSON=%s", path, raw)
				}
				target := "/candidate.json"
				if mode == "fragment" {
					target = "#candidate"
				} else if mode == "empty" {
					target = ""
				}
				assertReferenceDisposition(t, path+"/"+mode, set, err, scanDrops, want, target)
			})
			cases++
		}
	}
	t.Logf("manifest table entries=%d generated cases=%d", len(paths), cases)
}

func manifestPolicyTestValue(typ reflect.Type, mode string) any {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	candidate := "/candidate.json"
	if mode == "fragment" {
		candidate = "#candidate"
	}
	if mode == "empty" {
		switch typ.Kind() {
		case reflect.Map, reflect.Interface, reflect.Pointer:
			return nil
		case reflect.Struct:
			return map[string]any{}
		case reflect.Slice, reflect.Array:
			if typ == reflect.TypeFor[json.RawMessage]() {
				return nil
			}
			if typ.Kind() == reflect.Array {
				return []any{0, 0, 0, 0}
			}
			return []any{}
		case reflect.String:
			return ""
		case reflect.Bool:
			return false
		default:
			return float64(0)
		}
	}
	if typ == reflect.TypeFor[json.RawMessage]() || typ.Kind() == reflect.Interface {
		return map[string]any{"url": candidate}
	}
	switch typ.Kind() {
	case reflect.String:
		return candidate
	case reflect.Bool:
		return true
	case reflect.Struct:
		return map[string]any{"resources": []any{map[string]any{"url": candidate, "output": "loaded"}}}
	case reflect.Map:
		switch typ.Elem().Kind() {
		case reflect.String:
			return map[string]any{"candidate": candidate}
		case reflect.Struct:
			return map[string]any{"candidate": map[string]any{"path": candidate}}
		case reflect.Slice:
			return map[string]any{"candidate": []any{map[string]any{"uri": candidate}}}
		default:
			return map[string]any{"candidate": map[string]any{"url": candidate}}
		}
	case reflect.Slice:
		if typ.Elem().Kind() == reflect.Struct {
			return []any{map[string]any{"url": candidate, "output": "loaded"}}
		}
		return []any{candidate}
	case reflect.Array:
		return []any{1, 2, 3, 4}
	default:
		return float64(1)
	}
}

func manifestPolicyTestDocument(path string, value any) string {
	// A common runtime consumer prevents bridge-only uncertainty from hiding
	// unsupported fields. Baseline typed entries otherwise scan completely.
	root := map[string]any{
		"version": "0.1.0", "runtime": map[string]any{"path": "/known.wasm"},
		"islands":        []any{map[string]any{"id": "i", "programRef": "/known.bin"}},
		"computeIslands": []any{map[string]any{"programRef": "/known.bin"}},
		"engines":        []any{map[string]any{"programRef": "/known.wasm"}},
		"controllers":    []any{map[string]any{"id": "c", "config": map[string]any{}}},
		"bundles":        map[string]any{"candidate": map[string]any{"path": "/known.wasm"}},
	}
	current := root
	parts := strings.Split(path, ".")
	for i, part := range parts {
		key := strings.TrimSuffix(strings.TrimSuffix(part, "[]"), "{}")
		if i == len(parts)-1 {
			current[key] = value
			break
		}
		var child map[string]any
		switch {
		case strings.HasSuffix(part, "{}[]"):
			entries, _ := current[key].(map[string]any)
			if entries == nil {
				entries = map[string]any{}
				current[key] = entries
			}
			values, _ := entries["candidate"].([]any)
			if len(values) == 0 {
				values = []any{map[string]any{}}
				entries["candidate"] = values
			}
			child = values[0].(map[string]any)
		case strings.HasSuffix(part, "[]"):
			entries, _ := current[key].([]any)
			if len(entries) == 0 {
				entries = []any{map[string]any{}}
				current[key] = entries
			}
			child = entries[0].(map[string]any)
		case strings.HasSuffix(part, "{}"):
			entries, _ := current[key].(map[string]any)
			if entries == nil {
				entries = map[string]any{}
				current[key] = entries
			}
			child, _ = entries["candidate"].(map[string]any)
			if child == nil {
				child = map[string]any{}
				entries["candidate"] = child
			}
		default:
			child, _ = current[key].(map[string]any)
			if child == nil {
				child = map[string]any{}
				current[key] = child
			}
		}
		current = child
	}
	body, err := json.Marshal(root)
	if err != nil {
		panic(err)
	}
	return string(body)
}

func TestReferencesManifestPolicyControls(t *testing.T) {
	cases := []struct {
		fields   string
		complete bool
	}{
		{``, true},
		{`,"controllers":[{"id":"c","config":{}}]`, true},
		{`,"controllers":[{"id":"c","config":{"resources":[]}}]`, false},
		{`,"controllers":[{"id":"c","config":{"resources":[{}]}}]`, false},
		{`,"islands":[{"programRef":"/known.bin","props":null}],"runtime":{"path":"/known.wasm"}`, true},
		{`,"islands":[{"programRef":"/known.bin","props":{}}],"runtime":{"path":"/known.wasm"}`, true},
		{`,"islands":[{"programRef":"/known.bin","props":{"url":"/hidden"}}],"runtime":{"path":"/known.wasm"}`, false},
		{`,"controllers":[{"id":"c","config":{"resources":[{"url":"","immediate":false}]}}]`, false},
		{`,"textureVariants":{}`, true},
		{`,"textureVariants":{"/source.png":[]}`, false},
	}
	for _, tc := range cases {
		raw := `{"version":"0.1.0"` + tc.fields + `}`
		set, err := ScanReferences([]byte(`<script type="application/json" id="gosx-manifest">`+raw+`</script>`), KindDocument)
		if err != nil || set.Complete != tc.complete {
			t.Errorf("manifest control %s: %+v err=%v wantComplete=%v", raw, set, err, tc.complete)
		}
	}
	// A schema field added without a table entry fails closed, even if empty.
	typ := reflect.TypeFor[struct {
		FutureURL string `json:"futureURL"`
	}]()
	for _, value := range []any{"/hidden", ""} {
		out := referenceScanner{ReferenceSet: ReferenceSet{Complete: true}}
		walkManifestFields(typ, "", map[string]any{"futureURL": value}, &out)
		if out.Complete {
			t.Fatalf("unclassified schema extension accepted: %v", value)
		}
	}
}
