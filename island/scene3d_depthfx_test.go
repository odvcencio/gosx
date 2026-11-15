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

func depthFXAdvertised(t *testing.T, config engine.Config) bool {
	t.Helper()
	r := NewRenderer("main")
	r.SetBootstrapFeatureScene3DDepthFXPath("/activity/runtime/depthfx.js?v=abc")
	r.RenderEngine(config, gosx.Text(""))
	markup := gosx.RenderHTML(r.BootstrapScript())
	if strings.Contains(markup, `src="/activity/runtime/depthfx.js`) || strings.Contains(markup, `href="/activity/runtime/depthfx.js`) {
		t.Fatal("the depth effects chunk must be demand-loaded, not fetched at startup")
	}
	return strings.Contains(markup, `data-gosx-scene3d-depthfx-url="/activity/runtime/depthfx.js?v=abc"`)
}

func TestScene3DDepthFXURLIsAdvertisedForDecalsAndMotionBlur(t *testing.T) {
	for name, effect := range map[string]scene.PostEffect{
		"decals":     scene.Decals{Pools: []string{"hits"}},
		"motionBlur": scene.MotionBlur{},
	} {
		props := scene.Props{PostFX: scene.PostFX{Effects: []scene.PostEffect{effect}}}
		if !depthFXAdvertised(t, props.EngineConfig()) {
			t.Errorf("%s scene must advertise the depth effects chunk", name)
		}
	}
}

func TestScene3DDepthFXURLRequiresTheEffects(t *testing.T) {
	for _, props := range []string{
		`{}`, `null`, `{invalid`,
		`{"scene":{"postEffects":[{"kind":"bloom"}]}}`,
		`{"postEffects":"decals"}`,
		`{"postFX":{"effects":[{"kind":"decals"}]}}`,
		`{"scene":{"postEffects":[7,null,{"kind":5}]}}`,
	} {
		if depthFXAdvertised(t, engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(props)}) {
			t.Errorf("depth effects chunk advertised for %s", props)
		}
	}
	for _, props := range []string{
		`{"scene":{"postEffects":[{"kind":"bloom"},{"kind":"decals"}]}}`,
		`{"postEffects":[{"kind":"motionBlur"}]}`,
		`{"postFX":[{"kind":"decals"}]}`,
	} {
		if !depthFXAdvertised(t, engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(props)}) {
			t.Errorf("depth effects chunk missing for %s", props)
		}
	}
}

func TestScene3DDepthFXManifestURL(t *testing.T) {
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{BootstrapFeatureScene3DDepthFX: buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-depthfx.abc.js", Hash: "abc"}}}
	r := NewRenderer("main")
	if err := r.ApplyBuildManifest(manifest, "/activity"); err != nil {
		t.Fatal(err)
	}
	if r.bootstrapFeatureScene3dDepthFXPath != "/activity/runtime/bootstrap-feature-scene3d-depthfx.abc.js" {
		t.Fatalf("depth effects URL must use the production chunk version: %s", r.bootstrapFeatureScene3dDepthFXPath)
	}
}
