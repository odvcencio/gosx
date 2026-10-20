package budget

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSchemaNullableNumbersAndFormats(t *testing.T) {
	numbers := map[string]any{"type": []any{"number", "null"}, "minimum": float64(0)}
	for _, v := range []any{json.Number("1"), json.Number("1.25"), nil} {
		if err := validateInput(v, numbers); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []any{json.Number("-1"), "1"} {
		if err := validateInput(v, numbers); err == nil {
			t.Fatal("invalid number accepted")
		}
	}
	for _, format := range []string{"date", "date-time", "unknown"} {
		schema := map[string]any{"type": "string", "format": format}
		for _, v := range []string{"2026-10-08", "2026-10-08T00:00:00Z", "2026-02-30", "2026-02-30T00:00:00Z"} {
			want := format == "date" && v == "2026-10-08" || format == "date-time" && v == "2026-10-08T00:00:00Z"
			if err := validateInput(v, schema); (err == nil) != want {
				t.Fatalf("format %s: %v", format, err)
			}
		}
	}
}

func TestSchemaRejectsInvalidShapes(t *testing.T) {
	for _, s := range []any{
		nil, true, map[string]any{"type": "array"},
		map[string]any{"type": "array", "items": nil},
		map[string]any{"type": []any{"array", "null"}},
		map[string]any{"type": "string", "format": "unknown"},
		map[string]any{"pattern": "["}, map[string]any{"$ref": "#/$defs/Missing"},
		map[string]any{"required": []any{1}}, map[string]any{"properties": []any{}},
	} {
		if err := checkSchemaShape(s, inputDefinitions, false); err == nil {
			t.Fatal("invalid schema shape accepted")
		}
	}
	for _, value := range []any{[]any{}, []any{json.Number("1")}} {
		if err := validateInput(value, map[string]any{"type": "array"}); err == nil {
			t.Fatal("missing items accepted")
		}
	}
}

func requireInputError(t *testing.T, err error, ref, pointer string) {
	t.Helper()
	var input *InputError
	if !errors.As(err, &input) || input.Code != "invalid-input" || input.Reference != ref || input.Pointer != pointer {
		t.Fatalf("wrong input location: %v", err)
	}
}

func TestInputErrorsCarryLocationsWithoutValues(t *testing.T) {
	for _, test := range []struct {
		definition, fixture, ref, pointer string
		edit                              func(map[string]any)
	}{
		{"Profile", "profile", "profile", "/width", func(v map[string]any) { v["width"] = "rejected-value" }},
		{"Coefficients", "coefficients", "coefficients", "/sets/0/entries/0/value", func(v map[string]any) { coefficientEntry(v, 0, 0)["value"] = "rejected-value" }},
		{"Profile", "profile", "profile", "", func(v map[string]any) { v["rejected-value"] = 1 }},
	} {
		var value map[string]any
		if err := json.Unmarshal(fixture(t, test.fixture), &value); err != nil {
			t.Fatal(err)
		}
		test.edit(value)
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var out json.RawMessage
		err = decodeInput(body, test.definition, &out)
		requireInputError(t, err, test.ref, test.pointer)
		if strings.Contains(err.Error(), "rejected-value") {
			t.Fatal("error contains an input value")
		}
	}
	var out json.RawMessage
	requireInputError(t, decodeInput([]byte("{\"width\":1,\"width\":2}"), "Profile", &out), "profile", "/width")
	if pointerChild("", "a~/b") != "/a~0~1b" {
		t.Fatal("pointer escaping differs")
	}
}

func TestInputErrorReferenceForThirdFont(t *testing.T) {
	var tc Toolchain
	if err := json.Unmarshal(fixture(t, "toolchain"), &tc); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	data := []byte("font fixture")
	tc.Fonts = nil
	for i := 0; i < 3; i++ {
		name := "font" + strconv.Itoa(i) + ".woff2"
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		tc.Fonts = append(tc.Fonts, Ref{File: name, SHA256: hex.EncodeToString(digest[:])})
	}
	tc.Fonts[2].SHA256 = strings.Repeat("a", 64)
	body, err := json.Marshal(tc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "toolchain.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = LoadToolchain(path, LoadOptions{RootDir: root})
	requireInputError(t, err, "fonts[2]", "/sha256")
}

func TestInputErrorPointerIncludesValidatedMapKeys(t *testing.T) {
	schema := map[string]any{"type": "object", "propertyNames": map[string]any{"type": "string", "pattern": "^[a-z]+$"}, "additionalProperties": map[string]any{"type": "array", "items": map[string]any{"type": "number", "minimum": float64(0)}}}
	err := validateInput(map[string]any{"slot": []any{json.Number("-1")}}, schema)
	requireInputError(t, err, "", "/slot/0")
	err = validateInput(map[string]any{"unaccepted-key": []any{json.Number("-1")}}, schema)
	requireInputError(t, err, "", "")
}
