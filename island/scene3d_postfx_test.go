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

// postFXAdvertised reports which per-backend post-effect chunks the page
// advertises, and fails if it fetches either at startup.
func postFXAdvertised(t *testing.T, config engine.Config) (webgpu, webgl bool) {
	t.Helper()
	r := NewRenderer("main")
	r.SetBootstrapFeatureScene3DPostFXWebGPUPath("/activity/runtime/postfx-webgpu.js?v=abc")
	r.SetBootstrapFeatureScene3DPostFXWebGLPath("/activity/runtime/postfx-webgl.js?v=abc")
	r.RenderEngine(config, gosx.Text(""))
	markup := gosx.RenderHTML(r.BootstrapScript())
	for _, chunk := range []string{"postfx-webgpu.js", "postfx-webgl.js"} {
		if strings.Contains(markup, `src="/activity/runtime/`+chunk) || strings.Contains(markup, `href="/activity/runtime/`+chunk) {
			t.Fatalf("%s must be demand-loaded with its renderer, not fetched at startup", chunk)
		}
	}
	webgpu = strings.Contains(markup, `data-gosx-scene3d-postfx-webgpu-url="/activity/runtime/postfx-webgpu.js?v=abc"`)
	webgl = strings.Contains(markup, `data-gosx-scene3d-postfx-webgl-url="/activity/runtime/postfx-webgl.js?v=abc"`)
	return webgpu, webgl
}

func TestScene3DPostFXURLIsAdvertisedForTheEffectsThatNeedIt(t *testing.T) {
	for name, props := range map[string]scene.Props{
		"taa":          {PostFX: scene.PostFX{Effects: []scene.PostEffect{scene.TAA{}}}},
		"dof":          {PostFX: scene.PostFX{Effects: []scene.PostEffect{scene.DOF{FocusDistance: 4}}}},
		"autoExposure": {Environment: scene.Environment{AutoExposure: &scene.AutoExposure{Key: 0.18}}},
	} {
		if webgpu, webgl := postFXAdvertised(t, props.EngineConfig()); !webgpu || !webgl {
			t.Errorf("%s scene must advertise both post-effect chunks: webgpu=%v webgl=%v", name, webgpu, webgl)
		}
	}
	// WebGPU motion blur reads the TAA velocity pass; WebGL2 motion blur is camera-only.
	blur := scene.Props{PostFX: scene.PostFX{Effects: []scene.PostEffect{scene.MotionBlur{}}}}
	if webgpu, webgl := postFXAdvertised(t, blur.EngineConfig()); !webgpu || webgl {
		t.Errorf("a motion-blur scene must advertise only the WebGPU post-effect chunk: webgpu=%v webgl=%v", webgpu, webgl)
	}
}

func TestScene3DPostFXURLRequiresTheEffects(t *testing.T) {
	plain := scene.Props{PostFX: scene.PostFX{Effects: []scene.PostEffect{scene.Bloom{}, scene.Decals{Pools: []string{"hits"}}}}}
	if webgpu, webgl := postFXAdvertised(t, plain.EngineConfig()); webgpu || webgl {
		t.Error("bloom and decals do not need the post-effect chunks")
	}
	for _, props := range []string{
		`{}`, `null`, `{invalid`,
		`{"scene":{"postEffects":[{"kind":"bloom"}]}}`,
		`{"postEffects":"taa"}`,
		`{"postFX":{"effects":[{"kind":"taa"}]}}`,
		`{"environment":{"autoExposure":null}}`,
	} {
		if webgpu, webgl := postFXAdvertised(t, engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(props)}); webgpu || webgl {
			t.Errorf("post-effect chunks advertised for %s", props)
		}
	}
}

func TestScene3DPostFXManifestURLs(t *testing.T) {
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
		BootstrapFeatureScene3DPostFXWebGPU: buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-postfx-webgpu.abc.js", Hash: "abc"},
		BootstrapFeatureScene3DPostFXWebGL:  buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-postfx-webgl.def.js", Hash: "def"},
	}}
	r := NewRenderer("main")
	if err := r.ApplyBuildManifest(manifest, "/activity"); err != nil {
		t.Fatal(err)
	}
	if r.bootstrapFeatureScene3dPostFXWebGPUPath != "/activity/runtime/bootstrap-feature-scene3d-postfx-webgpu.abc.js" {
		t.Fatalf("WebGPU post-effect URL must use the production chunk version: %s", r.bootstrapFeatureScene3dPostFXWebGPUPath)
	}
	if r.bootstrapFeatureScene3dPostFXWebGLPath != "/activity/runtime/bootstrap-feature-scene3d-postfx-webgl.def.js" {
		t.Fatalf("WebGL post-effect URL must use the production chunk version: %s", r.bootstrapFeatureScene3dPostFXWebGLPath)
	}
}
