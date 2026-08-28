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
// WebGPU draws the shape. rect-area-specular degrades on both GPU backends,
// because neither uploads the fitted LTC tables. light-probe-sh degrades only
// on canvas2d: both GPU backends evaluate the spherical harmonics, and
// Canvas2D shades the probe as flat ambient. No light feature may exclude a
// backend.
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
		if tc.name == "light-probe" {
			// SH evaluates on both GPU backends; the probe stays degraded on
			// canvas2d only.
			for backend, feats := range got {
				if (backend == capability.BackendWebGPU || backend == capability.BackendWebGL) &&
					slices.Contains(feats, capability.FeatureLightProbeSH) {
					t.Errorf("%s: light-probe-sh must not degrade on %s", tc.name, backend)
				}
			}
		}
		// A light feature must never exclude a backend.
		if len(ir.BackendCaps.Capable) == 0 {
			t.Errorf("%s: light features excluded every backend", tc.name)
		}
	}
}
