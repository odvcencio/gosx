package hydrate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManifestRequireFeatureDedupesAndValidates(t *testing.T) {
	m := NewManifest()
	for _, name := range []string{"engine-bridge", "engine-bridge", " painter "} {
		if err := m.RequireFeature(name); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(m.Features, ","); got != "engine-bridge,painter" {
		t.Fatalf("features = %q", got)
	}
	for _, bad := range []string{"", "Engine Bridge", "../x", "a/b"} {
		if err := m.RequireFeature(bad); err == nil {
			t.Fatalf("RequireFeature(%q) accepted", bad)
		}
	}
	data, _ := json.Marshal(m)
	if !strings.Contains(string(data), `"features":["engine-bridge","painter"]`) {
		t.Fatalf("manifest JSON lacks features: %s", data)
	}
	if empty, _ := json.Marshal(NewManifest()); strings.Contains(string(empty), `"features"`) {
		t.Fatalf("empty manifest must omit features: %s", empty)
	}
}

func TestManifestWithBasePathCopiesFeatures(t *testing.T) {
	m := NewManifest()
	if err := m.RequireFeature("painter"); err != nil {
		t.Fatal(err)
	}
	out := m.WithBasePath("/app")
	if err := out.RequireFeature("edits"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(m.Features, ","); got != "painter" {
		t.Fatalf("base manifest features changed: %q", got)
	}
	if got := strings.Join(out.Features, ","); got != "painter,edits" {
		t.Fatalf("copy features = %q", got)
	}
}

func TestRequireFeatureRejectsContractKeyCollisions(t *testing.T) {
	m := NewManifest()
	if err := m.RequireFeature("a1"); err != nil {
		t.Fatal(err)
	}
	err := m.RequireFeature("a-1")
	if err == nil || !strings.Contains(err.Error(), `"a-1"`) || !strings.Contains(err.Error(), `"a1"`) {
		t.Fatalf("a-1 must be rejected naming both features, got %v", err)
	}
	err = m.RequireFeature("text-layout")
	if err == nil || !strings.Contains(err.Error(), `"text-layout"`) || !strings.Contains(err.Error(), `"textlayout"`) {
		t.Fatalf("text-layout must be rejected naming the legacy textlayout chunk, got %v", err)
	}
	if got := strings.Join(m.Features, ","); got != "a1" {
		t.Fatalf("rejected features must not be recorded, got %q", got)
	}
	// Re-requiring a recorded name and requiring a legacy name stay fine.
	for _, ok := range []string{"a1", "engines", "textlayout"} {
		if err := m.RequireFeature(ok); err != nil {
			t.Fatalf("RequireFeature(%q): %v", ok, err)
		}
	}
}

func TestFeatureContractKey(t *testing.T) {
	for name, want := range map[string]string{
		"engine-bridge": "bootstrapFeatureEngineBridgePath",
		"scene3d":       "bootstrapFeatureScene3dPath",
		"a-1":           "bootstrapFeatureA1Path",
		"textlayout":    "bootstrapFeatureTextLayoutPath",
	} {
		if got := FeatureContractKey(name); got != want {
			t.Errorf("FeatureContractKey(%q) = %q, want %q", name, got, want)
		}
	}
}
