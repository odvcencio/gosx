package scene

import (
	"slices"
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

// TestLightKindFeaturesReachTheWireVerdict pins that per-light-kind shortfalls
// reach the author, not just the renderer console.
//
// capability.LightKindFeatures existed and was unit-tested before this, but
// collectFeatures never called it, so BackendCaps carried no light information
// and the honesty gate stayed blind. This test covers the wire path; the Matrix
// cells themselves are corroborated against renderer source in
// scene/capability/lights_test.go.
//
// Expected shape: rect-area-light degrades on WebGL and canvas2d because only
// WebGPU draws the shape. rect-area-specular and light-probe-sh degrade
// everywhere, because no backend uploads the fitted LTC tables or evaluates
// spherical harmonics. No light feature may exclude a backend.
func TestLightKindFeaturesReachTheWireVerdict(t *testing.T) {
	wired := runtimeFeatureCollectionWired(t, "capability.LightKindFeatures(")
	for _, tc := range []struct {
		name  string
		node  Node
		wants []capability.Feature
	}{
		{"rect-area", RectAreaLight{ID: "ra", Color: "#ffffff", Intensity: 1, Width: 2, Height: 1},
			[]capability.Feature{capability.FeatureRectAreaLight, capability.FeatureRectAreaSpecular}},
		{"light-probe", LightProbe{ID: "lp", Intensity: 1},
			[]capability.Feature{capability.FeatureLightProbeSH}},
		{"point-light-none", PointLight{ID: "p", Color: "#ffffff", Intensity: 1}, nil},
	} {
		ir := Props{Graph: NewGraph(tc.node)}.SceneIR()
		if ir.BackendCaps == nil {
			t.Fatalf("%s: no BackendCaps on the wire", tc.name)
		}
		got := ir.BackendCaps.Degraded
		if !wired {
			for _, feats := range got {
				for _, feature := range feats {
					if feature == capability.FeatureRectAreaLight ||
						feature == capability.FeatureRectAreaSpecular ||
						feature == capability.FeatureLightProbeSH {
						t.Errorf("%s: light feature %s leaked before the runtime collector phase", tc.name, feature)
					}
				}
			}
			continue
		}
		for _, want := range tc.wants {
			found := false
			for _, feats := range got {
				if slices.Contains(feats, want) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: %s not reported as degraded anywhere; Degraded=%v", tc.name, want, got)
			}
		}
		if tc.wants == nil {
			for backend, feats := range got {
				for _, f := range feats {
					if f == capability.FeatureRectAreaLight || f == capability.FeatureRectAreaSpecular || f == capability.FeatureLightProbeSH {
						t.Errorf("%s: unexpected light feature %s on %s", tc.name, f, backend)
					}
				}
			}
		}
		// A light feature must never exclude a backend.
		if len(ir.BackendCaps.Capable) == 0 {
			t.Errorf("%s: light features excluded every backend", tc.name)
		}
	}
}

// TestLightShadowFeaturesReachTheWireVerdict pins that shadow-only capability
// gaps reach the author too, the same way TestLightKindFeaturesReachTheWireVerdict
// does for kind-only gaps. capability.LightShadowFeatures needs its own wiring
// guard because collectFeatures calls it as a second, separate helper beside
// capability.LightKindFeatures — the shadow answer depends on CastShadow and
// ShadowCascades, not on kind alone, so a caller could miss it even while the
// kind call stays wired.
//
// Point-light shadows are not exercised here: scene.PointLight carries no
// CastShadow field until this cluster's PointLight-authoring PR lands. Once it
// does, extend this table with a point case that expects
// capability.FeaturePointLightShadow.
func TestLightShadowFeaturesReachTheWireVerdict(t *testing.T) {
	wired := runtimeFeatureCollectionWired(t, "capability.LightShadowFeatures(")
	for _, tc := range []struct {
		name  string
		node  Node
		wants []capability.Feature
	}{
		{"directional-cascaded",
			DirectionalLight{ID: "d1", Color: "#ffffff", Intensity: 1, CastShadow: true, ShadowCascades: 3},
			[]capability.Feature{capability.FeatureShadowCascades}},
		{"directional-single-cascade",
			DirectionalLight{ID: "d2", Color: "#ffffff", Intensity: 1, CastShadow: true, ShadowCascades: 1},
			nil},
		{"directional-no-shadow",
			DirectionalLight{ID: "d3", Color: "#ffffff", Intensity: 1},
			nil},
	} {
		ir := Props{Graph: NewGraph(tc.node)}.SceneIR()
		if ir.BackendCaps == nil {
			t.Fatalf("%s: no BackendCaps on the wire", tc.name)
		}
		got := ir.BackendCaps.Degraded
		if !wired {
			for _, feats := range got {
				if slices.Contains(feats, capability.FeatureShadowCascades) ||
					slices.Contains(feats, capability.FeaturePointLightShadow) {
					t.Errorf("%s: shadow feature leaked before the runtime collector phase", tc.name)
				}
			}
			continue
		}
		for _, want := range tc.wants {
			found := false
			for _, feats := range got {
				if slices.Contains(feats, want) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: %s not reported as degraded anywhere; Degraded=%v", tc.name, want, got)
			}
		}
		if tc.wants == nil {
			for backend, feats := range got {
				if slices.Contains(feats, capability.FeatureShadowCascades) ||
					slices.Contains(feats, capability.FeaturePointLightShadow) {
					t.Errorf("%s: unexpected shadow feature on %s; Degraded=%v", tc.name, backend, feats)
				}
			}
		}
		// A shadow feature must never exclude a backend.
		if len(ir.BackendCaps.Capable) == 0 {
			t.Errorf("%s: shadow features excluded every backend", tc.name)
		}
	}
}
