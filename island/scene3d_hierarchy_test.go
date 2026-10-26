package island

import (
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/scene"
	"strings"
	"testing"
)

func TestScene3DHierarchyURLPreservesHashedDeliveryWithoutEagerFetch(t *testing.T) {
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{BootstrapFeatureScene3DHierarchy: buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-hierarchy.abc.js", Hash: "abc"}}}
	renderer := NewRenderer("main")
	if err := renderer.ApplyBuildManifest(manifest, "/activity"); err != nil {
		t.Fatal(err)
	}
	props := scene.Props{Graph: scene.NewGraph(scene.Group{ID: "rotor", Spin: scene.Rotate(4, 0, 0)})}
	renderer.RenderEngine(props.EngineConfig(), gosx.Text(""))
	markup := gosx.RenderHTML(renderer.BootstrapScript())
	const asset = "/activity/runtime/bootstrap-feature-scene3d-hierarchy.abc.js"
	if !strings.Contains(markup, `data-gosx-scene3d-hierarchy-url="`+asset+`"`) {
		t.Fatal("hierarchy did not retain its hashed embedded URL")
	}
	if strings.Contains(markup, `src="`+asset+`"`) || strings.Contains(markup, `href="`+asset+`"`) {
		t.Fatal("hierarchy must load through its content gate")
	}
}

func TestScene3DHierarchyStartupGateMatchesRetainedGroups(t *testing.T) {
	for _, spin := range []scene.Euler{{}, scene.Rotate(0, 1, 0)} {
		renderer := NewRenderer("main")
		renderer.RenderEngine((scene.Props{Graph: scene.NewGraph(scene.Group{ID: "rotor", Spin: spin})}).EngineConfig(), gosx.Text(""))
		gates, _, err := renderer.perfSceneStartupGates()
		if err != nil {
			t.Fatal(err)
		}
		if gates["hierarchy"] != (spin != (scene.Euler{})) {
			t.Fatalf("wrong hierarchy reachability for %v: %v", spin, gates)
		}
	}
}
