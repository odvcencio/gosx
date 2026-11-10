package island

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
)

// terrainCrowdScenePage renders one Scene3D engine and returns the bootstrap
// markup. A non-nil manifest stands in for a production build.
func terrainCrowdScenePage(t *testing.T, manifest *buildmanifest.Manifest, props json.RawMessage) string {
	t.Helper()
	r := NewRenderer("main")
	if manifest != nil {
		r.ApplyBuildManifest(manifest, "/games/x/gosx/assets")
	}
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: props}, gosx.Text(""))
	return gosx.RenderHTML(gosx.Fragment(r.PreloadHints(), r.BootstrapScript()))
}

func marshalSceneProps(t *testing.T, props scene.Props) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(props)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The terrain and crowd chunks are content-gated: a page advertises each one
// only when a scene declares terrain (or wind, or scatter bands) or a crowd,
// and nothing fetches either eagerly.
func TestScene3DTerrainAndCrowdURLsAreContentGated(t *testing.T) {
	terrain := scene.Props{}
	terrain.Environment.Terrain = &scene.Terrain{Heightmap: scene.GenerateHeightmap(scene.TerrainNoise{Seed: 1, Columns: 9, Rows: 9}), Width: 64, HeightScale: 8}
	crowd := scene.Props{}
	crowd.Environment.Crowds = []scene.Crowd{{ID: "stand", Stands: []scene.CrowdStand{{ID: "north", Shape: scene.CrowdShapeBowl, HalfWidth: 20, HalfDepth: 12, Rows: 4}}}}
	plain := scene.Props{Graph: scene.NewGraph(scene.Mesh{ID: "box", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}})}

	const terrainAttr, crowdAttr = `data-gosx-scene3d-terrain-url="/gosx/bootstrap-feature-scene3d-terrain.js"`, `data-gosx-scene3d-crowd-url="/gosx/bootstrap-feature-scene3d-crowd.js"`
	for _, tc := range []struct {
		name           string
		props          json.RawMessage
		terrain, crowd bool
	}{
		{"terrain", marshalSceneProps(t, terrain), true, false},
		{"crowd", marshalSceneProps(t, crowd), false, true},
		{"plain", marshalSceneProps(t, plain), false, false},
		{"wind", json.RawMessage(`{"scene":{"environment":{"wind":{"speed":3}}}}`), true, false},
		{"scatter band", json.RawMessage(`{"instancedMeshes":[{"id":"grass","lodGroup":"meadow"}]}`), true, false},
		{"empty crowd list", json.RawMessage(`{"environment":{"crowds":[],"terrain":null}}`), false, false},
		{"invalid props", json.RawMessage(`{invalid`), false, false},
	} {
		markup := terrainCrowdScenePage(t, nil, tc.props)
		if got := strings.Contains(markup, terrainAttr); got != tc.terrain {
			t.Errorf("%s: terrain URL advertised = %v, want %v\n%s", tc.name, got, tc.terrain, markup)
		}
		if got := strings.Contains(markup, crowdAttr); got != tc.crowd {
			t.Errorf("%s: crowd URL advertised = %v, want %v\n%s", tc.name, got, tc.crowd, markup)
		}
		for _, eager := range []string{`src="/gosx/bootstrap-feature-scene3d-terrain.js"`, `src="/gosx/bootstrap-feature-scene3d-crowd.js"`, `href="/gosx/bootstrap-feature-scene3d-terrain.js"`, `href="/gosx/bootstrap-feature-scene3d-crowd.js"`} {
			if strings.Contains(markup, eager) {
				t.Errorf("%s: the chunk must not load eagerly: %s", tc.name, eager)
			}
		}
	}
}

// A production build advertises the hashed manifest URL under the asset base
// path, with its integrity digest, so a page under a subpath and a static
// export both fetch the right file.
func TestScene3DTerrainAndCrowdURLsUseTheBuildManifest(t *testing.T) {
	manifest := &buildmanifest.Manifest{}
	manifest.Runtime.BootstrapFeatureScene3D = buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d.base1.js", Hash: "base1"}
	manifest.Runtime.Features = map[string]buildmanifest.HashedAsset{
		"scene3d-terrain": {File: "bootstrap-feature-scene3d-terrain.t1.js", Hash: "t1", Integrity: "sha384-terrain"},
		"scene3d-crowd":   {File: "bootstrap-feature-scene3d-crowd.c1.js", Hash: "c1", Integrity: "sha384-crowd"},
	}
	markup := terrainCrowdScenePage(t, manifest, json.RawMessage(`{"scene":{"environment":{"terrain":{"width":64},"crowds":[{"id":"stand"}]}}}`))
	for _, want := range []string{
		`data-gosx-scene3d-terrain-url="/games/x/gosx/assets/runtime/bootstrap-feature-scene3d-terrain.t1.js"`,
		`data-gosx-scene3d-terrain-integrity="sha384-terrain"`,
		`data-gosx-scene3d-crowd-url="/games/x/gosx/assets/runtime/bootstrap-feature-scene3d-crowd.c1.js"`,
		`data-gosx-scene3d-crowd-integrity="sha384-crowd"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %s in\n%s", want, markup)
		}
	}
}
