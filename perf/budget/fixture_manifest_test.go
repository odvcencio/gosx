package budget

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestMeasureFixtureCatalogAndArtifactHaveSeparateBindings(t *testing.T) {
	opts, manifest, _, _ := testRouteMeasurement(t)
	if manifest.FixturesSHA256 == manifest.CatalogSHA256 {
		t.Fatal("producer digest copied the catalog hash")
	}
	if _, err := measureApp(context.Background(), opts, testBodyNormalizer); err != nil {
		t.Fatal("correct independent bindings failed", err)
	}
	catalog := opts.Public.FixtureSHA256
	for _, cause := range []string{"catalog", "artifact", "missing-artifact"} {
		changed := opts
		switch cause {
		case "catalog":
			changed.Public.FixtureSHA256 = manifest.FixturesSHA256
		case "artifact":
			wrong := catalog
			changed.Public.ArtifactSHA256 = &wrong
		case "missing-artifact":
			changed.Public.ArtifactSHA256 = nil
		}
		if _, err := measureApp(context.Background(), changed, testBodyNormalizer); err == nil {
			t.Fatal("independent provenance binding was deleted", cause)
		}
	}
}
func TestMeasureFixtureProducerDigestDetectsContractChanges(t *testing.T) {
	_, manifest, _, _ := testRouteMeasurement(t)
	original := manifest.FixturesSHA256
	body, _ := json.Marshal(manifest)
	if _, err := DecodeFixtureManifest(bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*FixtureManifest){
		func(m *FixtureManifest) { m.SourceSHA = strings.Repeat("b", 40) },
		func(m *FixtureManifest) { m.CatalogSHA256 = strings.Repeat("3", 64) },
		func(m *FixtureManifest) { m.Routes[0].StartupCounts.Engines++ },
		func(m *FixtureManifest) { m.Assets[0].SHA256 = strings.Repeat("3", 64) },
		func(m *FixtureManifest) { m.Assets[1].Phase = "startup" },
	} {
		var changed FixtureManifest
		json.Unmarshal(body, &changed)
		edit(&changed)
		digest, err := FixtureManifestSHA256(changed)
		if err != nil || digest == original {
			t.Fatal("contract change did not change producer hash", err)
		}
		data, _ := json.Marshal(changed)
		if _, err := DecodeFixtureManifest(bytes.NewReader(data)); err == nil {
			t.Fatal("stale producer digest was admitted")
		}
		changed.FixturesSHA256 = digest
		data, _ = json.Marshal(changed)
		if _, err := DecodeFixtureManifest(bytes.NewReader(data)); err != nil {
			t.Fatal("rehashing changed contract failed", err)
		}
	}
	manifest.FixturesSHA256 = strings.Repeat("4", 64)
	if actual, err := FixtureManifestSHA256(*manifest); err != nil || actual != original {
		t.Fatal("self digest entered its own hash", err)
	}
}
