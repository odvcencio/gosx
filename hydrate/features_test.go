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
