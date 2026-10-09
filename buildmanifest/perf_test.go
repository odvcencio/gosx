package buildmanifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func perfFixture() *Manifest {
	hash := strings.Repeat("a", 64)
	return &Manifest{PerfAssetUses: &PerfAssetUses{Version: 1, Assets: []PerfAssetUse{
		{ID: "framework/scene", SHA256: hash, URL: "/gosx/scene.hash.js", Owner: "framework", Kind: "js", Phase: "startup", Condition: "always", Dependencies: []string{"framework/webgpu"}},
		{ID: "framework/webgpu", SHA256: strings.Repeat("b", 64), URL: "/gosx/webgpu.hash.js", Owner: "framework", Kind: "js", Phase: "startup", Condition: "webgpu", Dependencies: []string{}},
		{ID: "app/demo/model", SHA256: strings.Repeat("c", 64), URL: "/models/model.hash.glb", Owner: "app", Kind: "model", Phase: "startup", Condition: "always", Dependencies: []string{}},
	}}}
}

func TestPerfAssetRoundTripAndCompatibility(t *testing.T) {
	original := perfFixture()
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var restored Manifest
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if err := restored.ValidatePerfAssetUses(); err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(&restored)
	if string(data) != string(again) {
		t.Fatal("manifest changed during round trip")
	}
	old := &Manifest{}
	if err := old.ValidatePerfAssetUses(); err != nil {
		t.Fatal(err)
	}
	if old.PerfAssetUses != nil {
		t.Fatal("compatibility validation fabricated reachability")
	}
	data, _ = json.Marshal(old)
	if strings.Contains(string(data), "perfAssetUses") {
		t.Fatal("old manifest gained performance metadata")
	}
	var empty Manifest
	if err := json.Unmarshal([]byte(`{"perfAssetUses":{"version":1,"assets":[]}}`), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.PerfAssetUses == nil || empty.PerfAssetUses.Assets == nil {
		t.Fatal("known empty graph became unknown")
	}
}

func TestPerfAssetRejectsInvalidDeclarations(t *testing.T) {
	for _, test := range []struct {
		name, field string
		change      func(*PerfAssetUse)
	}{
		{"escape-id", "id", func(a *PerfAssetUse) { a.ID = "framework/../private" }},
		{"drive-id", "id", func(a *PerfAssetUse) { a.ID = `C:\private\asset` }},
		{"absolute-id", "id", func(a *PerfAssetUse) { a.ID = "/framework/core" }},
		{"long-id", "id", func(a *PerfAssetUse) { a.ID = strings.Repeat("a", 241) }},
		{"hash", "sha256", func(a *PerfAssetUse) { a.SHA256 = strings.Repeat("A", 64) }},
		{"origin", "url", func(a *PerfAssetUse) { a.URL = "https://example.invalid/asset.js" }},
		{"protocol-relative", "url", func(a *PerfAssetUse) { a.URL = "//example.invalid/asset.js" }},
		{"query", "url", func(a *PerfAssetUse) { a.URL = "/asset.js?q=private" }},
		{"fragment", "url", func(a *PerfAssetUse) { a.URL = "/asset.js#private" }},
		{"escape-url", "url", func(a *PerfAssetUse) { a.URL = "/assets/../private" }},
		{"encoded-url", "url", func(a *PerfAssetUse) { a.URL = "/assets/%2e%2e/private" }},
		{"owner", "owner", func(a *PerfAssetUse) { a.Owner = "unknown" }},
		{"app-namespace", "id", func(a *PerfAssetUse) { a.Owner = "app" }},
		{"kind", "kind", func(a *PerfAssetUse) { a.Kind = "source-map" }},
		{"phase", "phase", func(a *PerfAssetUse) { a.Phase = "lazy" }},
		{"condition", "condition", func(a *PerfAssetUse) { a.Condition = "webgl2" }},
		{"null-dependencies", "dependencies", func(a *PerfAssetUse) { a.Dependencies = nil }},
		{"many-dependencies", "dependencies", func(a *PerfAssetUse) { a.Dependencies = make([]string, 129) }},
		{"escaped-dependency", "dependencies/0", func(a *PerfAssetUse) { a.Dependencies = []string{"../private"} }},
		{"missing-dependency", "dependencies/0", func(a *PerfAssetUse) { a.Dependencies = []string{"framework/missing"} }},
		{"duplicate-dependency", "dependencies/1", func(a *PerfAssetUse) { a.Dependencies = []string{"framework/webgpu", "framework/webgpu"} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := perfFixture()
			test.change(&m.PerfAssetUses.Assets[0])
			err := m.ValidatePerfAssetUses()
			var input *PerfAssetError
			if !errors.As(err, &input) || input.Code != "invalid-input" || input.Pointer != "/perfAssetUses/assets/0/"+test.field {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
	for _, p := range []*PerfAssetUses{{Version: 2, Assets: []PerfAssetUse{}}, {Version: 1}, {Version: 1, Assets: make([]PerfAssetUse, 4097)}} {
		if err := (&Manifest{PerfAssetUses: p}).ValidatePerfAssetUses(); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
}

func TestPerfAssetDependencyGraphs(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"self", func(m *Manifest) { m.PerfAssetUses.Assets[0].Dependencies = []string{"framework/scene"} }},
		{"cycle", func(m *Manifest) { m.PerfAssetUses.Assets[1].Dependencies = []string{"framework/scene"} }},
		{"wrong-backend", func(m *Manifest) { m.PerfAssetUses.Assets[0].Condition = "webgl" }},
		{"identity-hash", func(m *Manifest) {
			a := m.PerfAssetUses.Assets[0]
			a.SHA256 = strings.Repeat("d", 64)
			m.PerfAssetUses.Assets = append(m.PerfAssetUses.Assets, a)
		}},
		{"identity-owner", func(m *Manifest) {
			a := m.PerfAssetUses.Assets[2]
			a.Owner = "framework"
			m.PerfAssetUses.Assets = append(m.PerfAssetUses.Assets, a)
		}},
		{"identity-kind", func(m *Manifest) {
			a := m.PerfAssetUses.Assets[0]
			a.Kind = "wasm"
			m.PerfAssetUses.Assets = append(m.PerfAssetUses.Assets, a)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := perfFixture()
			test.mutate(m)
			if err := m.ValidatePerfAssetUses(); err == nil {
				t.Fatal("invalid graph accepted")
			}
		})
	}
	for _, condition := range []string{"always", "webgpu", "webgl", "device-loss", "pipeline-recovery", "hls-required", "interaction"} {
		m := perfFixture()
		m.PerfAssetUses.Assets[2].Condition = condition
		if err := m.ValidatePerfAssetUses(); err != nil {
			t.Fatal(err)
		}
	}
	m := perfFixture()
	a := m.PerfAssetUses.Assets[1]
	a.Condition = "webgl"
	a.Phase = "after-ready"
	m.PerfAssetUses.Assets = append(m.PerfAssetUses.Assets, a)
	m.PerfAssetUses.Assets[0].Condition = "webgl"
	if err := m.ValidatePerfAssetUses(); err != nil {
		t.Fatal("compatible conditional use rejected", err)
	}
}

func TestPerfAssetGlobalIdentity(t *testing.T) {
	base := perfFixture()
	head := perfFixture()
	head.PerfAssetUses.Assets[0].URL = "/export/gosx/scene.hash.js"
	if err := ValidatePerfAssetConsistency(base, head, &Manifest{}); err != nil {
		t.Fatal("URL rewrite changed logical identity", err)
	}
	head.PerfAssetUses.Assets[0].SHA256 = strings.Repeat("e", 64)
	if err := ValidatePerfAssetConsistency(base, head); err == nil {
		t.Fatal("conflicting global SHA accepted")
	}
	head = perfFixture()
	head.PerfAssetUses.Assets[2].Owner = "framework"
	if err := ValidatePerfAssetConsistency(base, head); err == nil {
		t.Fatal("conflicting global ownership accepted")
	}
	head = perfFixture()
	head.PerfAssetUses.Assets[2].ID = "app/other/model"
	head.PerfAssetUses.Assets[2].SHA256 = strings.Repeat("f", 64)
	if err := ValidatePerfAssetConsistency(base, head); err != nil {
		t.Fatal("separate app namespaces conflict", err)
	}
}

func TestPerfAssetClosedJSON(t *testing.T) {
	data, _ := json.Marshal(perfFixture().PerfAssetUses)
	for _, raw := range []string{
		`{"version":1,"assets":[],"privatePath":"C:/private/asset"}`,
		`{"version":1,"version":1,"assets":[]}`,
		`{"version":1}`, `{"assets":[]}`, `{"version":1,"assets":null}`,
		strings.Replace(string(data), `"id":"framework/scene"`, `"id":"framework/scene","id":"framework/scene"`, 1),
		strings.Replace(string(data), `"id":"framework/scene"`, `"privatePath":"C:/private/asset","id":"framework/scene"`, 1),
		strings.Replace(string(data), `"dependencies":["framework/webgpu"]`, `"dependencies":null`, 1),
		strings.Replace(string(data), `"condition":"always",`, "", 1),
	} {
		var p PerfAssetUses
		err := json.Unmarshal([]byte(raw), &p)
		if err == nil {
			t.Fatal("invalid JSON contract accepted")
		}
		if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "C:") {
			t.Fatal("error copied input value")
		}
	}
}

func TestPerfAssetLoadRejectsInvalidGraph(t *testing.T) {
	m := perfFixture()
	m.PerfAssetUses.Assets[0].Dependencies = []string{"framework/missing"}
	data, _ := json.Marshal(m)
	path := filepath.Join(t.TempDir(), "build.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted invalid graph")
	}
	data, _ = json.Marshal(perfFixture())
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
}

// perfSizeFixture fills the production-formatted metadata to an exact byte
// count without exceeding the per-asset or asset-count limits.
func perfSizeFixture(t *testing.T, size int) *Manifest {
	t.Helper()
	m := &Manifest{PerfAssetUses: &PerfAssetUses{Version: 1, Assets: []PerfAssetUse{}}}
	for i := 0; i < 3000; i++ {
		m.PerfAssetUses.Assets = append(m.PerfAssetUses.Assets, PerfAssetUse{
			ID:     fmt.Sprintf("framework/asset-%04d-%s", i, strings.Repeat("a", 180)),
			SHA256: strings.Repeat("a", 64), URL: "/" + strings.Repeat("u", 200),
			Owner: "framework", Kind: "js", Phase: "startup", Condition: "always", Dependencies: []string{},
		})
	}
	// Bypass custom marshaling so over-limit inputs can still reach the decoder.
	type plain PerfAssetUses
	data, err := json.MarshalIndent(plain(*m.PerfAssetUses), "  ", "  ")
	if err != nil {
		t.Fatal(err)
	}
	remaining := size - len(data)
	if remaining < 0 {
		t.Fatal("fixture starts above the requested size")
	}
	for i := range m.PerfAssetUses.Assets {
		a := &m.PerfAssetUses.Assets[i]
		padding := min(remaining, 240-len(a.URL))
		a.URL += strings.Repeat("u", padding)
		remaining -= padding
	}
	if remaining != 0 {
		t.Fatal("fixture cannot reach the requested size")
	}
	return m
}

func TestPerfAssetSizeBoundaryRoundTrip(t *testing.T) {
	const limit = 2 << 20
	for _, delta := range []int{-1, 0, 1} {
		t.Run(fmt.Sprintf("limit%+d", delta), func(t *testing.T) {
			m := perfSizeFixture(t, limit+delta)
			accepted := delta <= 0
			check := func(t *testing.T, label string, err error) {
				t.Helper()
				if accepted {
					if err != nil {
						t.Fatalf("%s: %v", label, err)
					}
					return
				}
				var input *PerfAssetError
				if !errors.As(err, &input) || input.Code != "invalid-input" || input.Pointer != "/perfAssetUses" {
					t.Errorf("%s: expected fixed size error, got %v", label, err)
				}
			}
			check(t, "native validation", m.ValidatePerfAssetUses())
			// This wrapper retains the real field's nesting and indentation while
			// bypassing serialization checks for the decoder's over-limit case.
			type plain PerfAssetUses
			unchecked := struct {
				PerfAssetUses plain `json:"perfAssetUses"`
			}{plain(*m.PerfAssetUses)}
			for _, format := range []string{"compact", "production-indent"} {
				t.Run(format, func(t *testing.T) {
					encode := func(value any) ([]byte, error) {
						if format == "compact" {
							return json.Marshal(value)
						}
						return json.MarshalIndent(value, "", "  ")
					}
					data, err := encode(unchecked)
					if err != nil {
						t.Fatal(err)
					}
					var fields map[string]json.RawMessage
					if err := json.Unmarshal(data, &fields); err != nil {
						t.Fatal(err)
					}
					if format == "production-indent" && len(fields["perfAssetUses"]) != limit+delta {
						t.Fatal("fixture missed the metadata byte boundary")
					}
					var restored Manifest
					check(t, "decode", json.Unmarshal(data, &restored))
					path := filepath.Join(t.TempDir(), "build.json")
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
					loaded, err := Load(path)
					check(t, "Load", err)
					if accepted && (!reflect.DeepEqual(restored.PerfAssetUses, m.PerfAssetUses) || !reflect.DeepEqual(loaded.PerfAssetUses, m.PerfAssetUses)) {
						t.Fatal("metadata changed during round trip")
					}
					serialized, err := encode(m)
					check(t, "serialization", err)
					if accepted {
						check(t, "serialized round trip", json.Unmarshal(serialized, &restored))
					}
				})
			}
		})
	}
}
