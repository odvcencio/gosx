package island

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/scene"
)

// The SMAA chunk is content-gated: a page advertises it only for a scene whose
// post chain lists SMAA, never for FXAA, TAA or a scene with no chain, and
// nothing fetches it eagerly.
func TestScene3DSMAAURLIsContentGated(t *testing.T) {
	box := scene.NewGraph(scene.Mesh{ID: "box", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}})
	chain := func(effects ...scene.PostEffect) scene.Props {
		return scene.Props{Graph: box, PostFX: scene.PostFX{Effects: effects}}
	}
	const attr = `data-gosx-scene3d-smaa-url="/gosx/bootstrap-feature-scene3d-smaa.js"`
	for _, tc := range []struct {
		name  string
		props scene.Props
		want  bool
	}{
		{"smaa", chain(scene.Tonemap{Mode: scene.TonemapACES}, scene.SMAA{}), true},
		{"fxaa", chain(scene.Tonemap{Mode: scene.TonemapACES}, scene.FXAA{}), false},
		{"taa", chain(scene.Tonemap{Mode: scene.TonemapACES}, scene.TAA{}), false},
		{"no chain", scene.Props{Graph: box}, false},
	} {
		markup := terrainCrowdScenePage(t, nil, marshalSceneProps(t, tc.props))
		if got := strings.Contains(markup, attr); got != tc.want {
			t.Errorf("%s: SMAA URL advertised = %v, want %v", tc.name, got, tc.want)
		}
		for _, eager := range []string{`src="/gosx/bootstrap-feature-scene3d-smaa.js"`, `href="/gosx/bootstrap-feature-scene3d-smaa.js"`} {
			if strings.Contains(markup, eager) {
				t.Errorf("%s: the SMAA chunk must not load eagerly: %s", tc.name, eager)
			}
		}
	}

	// A production build advertises the hashed file under the asset base path.
	manifest := &buildmanifest.Manifest{}
	manifest.Runtime.BootstrapFeatureScene3D = buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d.base1.js", Hash: "base1"}
	manifest.Runtime.Features = map[string]buildmanifest.HashedAsset{
		"scene3d-smaa": {File: "bootstrap-feature-scene3d-smaa.s1.js", Hash: "s1", Integrity: "sha384-smaa"},
	}
	markup := terrainCrowdScenePage(t, manifest, marshalSceneProps(t, chain(scene.SMAA{})))
	for _, want := range []string{`data-gosx-scene3d-smaa-url="/games/x/gosx/assets/runtime/bootstrap-feature-scene3d-smaa.s1.js"`, `data-gosx-scene3d-smaa-integrity="sha384-smaa"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("production page lacks %s", want)
		}
	}
}
