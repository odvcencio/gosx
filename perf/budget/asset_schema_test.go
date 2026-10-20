package budget

import (
	"encoding/json"
	"testing"
)

func TestPerfAssetSchemaClosedAndBounded(t *testing.T) {
	// These are synthetic artifact identities for schema tests, not measurements.
	good := map[string]any{"id": "framework/scene", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "url": "/gosx/scene.hash.js", "owner": "framework", "kind": "js", "phase": "startup", "condition": "webgpu", "dependencies": []any{"framework/base"}}
	decode := func(value map[string]any) error {
		data, _ := json.Marshal(value)
		var out json.RawMessage
		return decodeInput(data, "AssetUse", &out)
	}
	if err := decode(good); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		field string
		value any
	}{
		{"id", "framework/../private"}, {"sha256", "not-a-hash"}, {"url", "https://example.invalid/asset"}, {"url", "//private/asset"}, {"url", "/assets/../private"},
		{"owner", "unknown"}, {"kind", "trace"}, {"phase", "lazy"}, {"condition", "webgl2"}, {"dependencies", []any{"../private"}}, {"dependencies", nil},
	} {
		copy := make(map[string]any, len(good))
		for key, value := range good {
			copy[key] = value
		}
		copy[test.field] = test.value
		requireInputError(t, decode(copy), "fixtures", "/"+test.field+func() string {
			if test.field == "dependencies" && test.value != nil {
				return "/0"
			}
			return ""
		}())
	}
	for field := range good {
		copy := make(map[string]any, len(good))
		for key, value := range good {
			if key != field {
				copy[key] = value
			}
		}
		if err := decode(copy); err == nil {
			t.Fatal("missing asset field accepted", field)
		}
	}
	good["privatePath"] = "/private/asset"
	requireInputError(t, decode(good), "fixtures", "")
}

func TestPerfAssetFixtureManifestSchema(t *testing.T) {
	catalog := fixture(t, "catalog")
	var source struct {
		Routes []any `json:"routes"`
	}
	if err := json.Unmarshal(catalog, &source); err != nil {
		t.Fatal(err)
	}
	value := map[string]any{"schema": "gosx.perf-fixtures/v1", "version": 1, "routes": source.Routes, "assets": []any{}, "sourceSHA": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "fixturesSHA256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "catalogSHA256": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
	data, _ := json.Marshal(value)
	var out json.RawMessage
	if err := decodeInput(data, "FixtureManifest", &out); err != nil {
		t.Fatal(err)
	}
	value["sourceSHA"] = "wrong"
	data, _ = json.Marshal(value)
	requireInputError(t, decodeInput(data, "FixtureManifest", &out), "fixtures", "/sourceSHA")
}
