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

func TestScene3DParticleBurstURLIsAdvertisedWithoutLoading(t *testing.T) {
	r := NewRenderer("main")
	r.SetBootstrapFeatureScene3DParticleBurstPath("/activity/runtime/burst.js?v=abc")
	r.RenderEngine(scene.Props{ParticleBursts: scene.Bool(true)}.EngineConfig(), gosx.Text(""))
	markup := gosx.RenderHTML(r.BootstrapScript())
	if !strings.Contains(markup, `data-gosx-scene3d-particle-burst-url="/activity/runtime/burst.js?v=abc"`) {
		t.Fatal("burst must preserve an embedded override URL")
	}
	if strings.Contains(markup, `src="/activity/runtime/burst.js?v=abc"`) || strings.Contains(markup, `href="/activity/runtime/burst.js?v=abc"`) {
		t.Fatal("burst must not be fetched before playback")
	}
	if !strings.Contains(markup, `data-gosx-scene3d-compute-url=`) || strings.Contains(markup, `src="/gosx/bootstrap-feature-scene3d-compute.js`) {
		t.Fatal("future bursts need an advertised compute URL without eager loading")
	}
}

func TestScene3DParticleBurstURLRequiresOptIn(t *testing.T) {
	for _, props := range []string{`{}`, `{"particleBursts":false}`, `{"particleBursts":"true"}`, `null`, `{invalid`, `{"timelines":true}`, `{"scene":{"computeParticles":[{"id":"smoke","count":1}]}}`} {
		r := NewRenderer("main")
		r.SetBootstrapFeatureScene3DParticleBurstPath("/runtime/burst.js")
		r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(props)}, gosx.Text(""))
		if strings.Contains(gosx.RenderHTML(r.BootstrapScript()), "data-gosx-scene3d-particle-burst-url") {
			t.Fatalf("burst URL advertised without opt-in for %s", props)
		}
	}
}

func TestScene3DParticleBurstURLWithMultipleEngines(t *testing.T) {
	r := NewRenderer("main")
	r.SetBootstrapFeatureScene3DParticleBurstPath("/runtime/burst.js")
	r.RenderEngine(engine.Config{Name: "Other", Props: json.RawMessage(`{"particleBursts":true}`)}, gosx.Text(""))
	r.RenderEngine(scene.Props{Timelines: scene.Bool(true)}.EngineConfig(), gosx.Text(""))
	if strings.Contains(gosx.RenderHTML(r.BootstrapScript()), "data-gosx-scene3d-particle-burst-url") {
		t.Fatal("another engine or timelines must not opt Scene3D into bursts")
	}
	r.RenderEngine(scene.Props{ParticleBursts: scene.Bool(true)}.EngineConfig(), gosx.Text(""))
	markup := gosx.RenderHTML(r.BootstrapScript())
	if !strings.Contains(markup, "data-gosx-scene3d-particle-burst-url") || !strings.Contains(markup, "data-gosx-scene3d-timeline-url") {
		t.Fatal("each opted-in feature must advertise its own URL")
	}
}

func TestScene3DParticleBurstManifestURL(t *testing.T) {
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{BootstrapFeatureScene3DParticleBurst: buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-particle-burst.abc.js", Hash: "abc"}}}
	r := NewRenderer("main")
	if err := r.ApplyBuildManifest(manifest, "/activity"); err != nil {
		t.Fatal(err)
	}
	if r.bootstrapFeatureScene3dParticleBurstPath != "/activity/runtime/bootstrap-feature-scene3d-particle-burst.abc.js" {
		t.Fatalf("burst URL must use the production chunk version: %s", r.bootstrapFeatureScene3dParticleBurstPath)
	}
}
