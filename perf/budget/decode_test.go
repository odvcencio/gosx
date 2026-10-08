package budget

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeRejectsAmbiguousJSON(t *testing.T) {
	data := fixture(t, "coefficients")
	for name, body := range map[string][]byte{
		"duplicate-root":    bytes.Replace(data, []byte(`"schema":`), []byte(`"schema":"gosx.budget-coefficients/v1","schema":`), 1),
		"escaped-duplicate": bytes.Replace(data, []byte(`"schema":`), []byte(`"\u0073chema":"gosx.budget-coefficients/v1","schema":`), 1),
		"duplicate-nested":  bytes.Replace(data, []byte(`"value": 200000`), []byte(`"value": 0, "value": 200000`), 1),
		"invalid-utf8":      bytes.Replace(data, []byte("illustrative-cold"), []byte{'a', 0xff, 'b'}, 1),
		"trailing-value":    append(append([]byte{}, data...), []byte(" {}")...),
		"truncated":         data[:len(data)-10],
		"nonfinite":         bytes.Replace(data, []byte(`"value": 200000`), []byte(`"value": 1e999`), 1),
		"oversized":         append(bytes.Repeat([]byte(" "), maxInputBytes), data...),
	} {
		t.Run(name, func(t *testing.T) {
			var c Coefficients
			if err := decodeInput(body, "Coefficients", &c); err == nil {
				t.Fatal("ambiguous JSON accepted")
			}
		})
	}
	var c Coefficients
	if err := decodeInput(append(data, bytes.Repeat([]byte(" "), maxInputBytes-len(data))...), "Coefficients", &c); err != nil {
		t.Fatalf("exact byte limit rejected: %v", err)
	}
	if err := decodeInput([]byte(strings.Repeat("[", 66)+"0"+strings.Repeat("]", 66)), "Coefficients", &c); err == nil {
		t.Fatalf("nesting not bounded: %v", err)
	}
}

func TestDecodePublicLoadersUseStrictBytes(t *testing.T) {
	loaders := []struct {
		name string
		load func(string, LoadOptions) error
	}{
		{"profile", func(path string, opts LoadOptions) error { _, err := LoadProfile(path, opts); return err }},
		{"coefficients", func(path string, opts LoadOptions) error { _, err := LoadCoefficients(path, opts); return err }},
		{"toolchain", func(path string, opts LoadOptions) error { _, err := LoadToolchain(path, opts); return err }},
	}
	for _, loader := range loaders {
		t.Run(loader.name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(fixture(t, loader.name), &value); err != nil {
				t.Fatal(err)
			}
			if loader.name == "toolchain" {
				value["fonts"] = []any{}
			}
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			path := filepath.Join(root, "input.json")
			opts := LoadOptions{RootDir: root}
			write := func(body []byte) {
				t.Helper()
				if err := os.WriteFile(path, body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(data)
			if err := loader.load(path, opts); err != nil {
				t.Fatal(err)
			}
			duplicate := append([]byte(`{"schema":"invalid",`), data[1:]...)
			write(duplicate)
			if err := loader.load(path, opts); err == nil {
				t.Fatal("loader bypassed duplicate-key check")
			}
			invalid := append([]byte{'{', '"', 0xff, '"', ':', '0', ','}, data[1:]...)
			write(invalid)
			if err := loader.load(path, opts); err == nil {
				t.Fatal("loader bypassed UTF-8 check")
			}
			write(append(bytes.Repeat([]byte(" "), maxInputBytes-len(data)), data...))
			if err := loader.load(path, opts); err != nil {
				t.Fatalf("exact limit rejected: %v", err)
			}
			write(append(bytes.Repeat([]byte(" "), maxInputBytes-len(data)+1), data...))
			if err := loader.load(path, opts); err == nil {
				t.Fatal("loader accepted oversized input")
			}
		})
	}
}

func TestDecodeClosedNestedInputs(t *testing.T) {
	for _, name := range []string{"profile", "coefficients", "toolchain"} {
		definition := map[string]string{"profile": "Profile", "coefficients": "Coefficients", "toolchain": "Toolchain"}[name]
		var raw json.RawMessage
		if err := decodeInput(fixture(t, name), definition, &raw); err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		value["unexpected"] = "rejected"
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := decodeInput(data, definition, &raw); err == nil {
			t.Fatalf("%s accepted unknown member", name)
		}
	}
}

func TestDecodeSchemaVocabulary(t *testing.T) {
	// Closed keyed objects are used for registered allocation types.
	schema := map[string]any{"type": "object", "minProperties": float64(1), "maxProperties": float64(2), "propertyNames": map[string]any{"type": "string", "pattern": "^[a-z]+$"}, "additionalProperties": map[string]any{"type": "array", "minItems": float64(1), "maxItems": float64(2), "uniqueItems": true, "items": map[string]any{"oneOf": []any{map[string]any{"type": "null"}, map[string]any{"type": "number", "minimum": float64(0)}}}}}
	for _, body := range []string{`{"a":[1.25]}`, `{"a":[null]}`, `{"a":[0,1]}`} {
		d := json.NewDecoder(strings.NewReader(body))
		d.UseNumber()
		var value any
		if err := d.Decode(&value); err != nil {
			t.Fatal(err)
		}
		if err := validateInput(value, schema); err != nil {
			t.Fatalf("valid keyed input rejected %s: %v", body, err)
		}
	}
	for _, body := range []string{`{}`, `{"A":[1]}`, `{"a":[1,1]}`, `{"a":[-1]}`, `{"a":[true]}`, `{"a":[1],"b":[1],"c":[1]}`, `{"a":[]}`} {
		d := json.NewDecoder(strings.NewReader(body))
		d.UseNumber()
		var value any
		if err := d.Decode(&value); err != nil {
			t.Fatal(err)
		}
		if err := validateInput(value, schema); err == nil {
			t.Fatalf("invalid keyed input accepted %s", body)
		}
	}
}
