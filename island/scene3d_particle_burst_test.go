package island

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/engine"
)

func TestScene3DParticleBurstURLIsAdvertisedWithoutLoading(t *testing.T) {
	r := NewRenderer("main")
	r.SetBootstrapFeatureScene3DParticleBurstPath("/activity/runtime/burst.js?v=abc")
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(`{"particleBursts":true}`)}, gosx.Text(""))
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

func TestScene3DParticleBurstManifestURL(t *testing.T) {
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{BootstrapFeatureScene3DParticleBurst: buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-particle-burst.abc.js", Hash: "abc"}}}
	r := NewRenderer("main")
	if err := r.ApplyBuildManifest(manifest, "/activity"); err != nil {
		t.Fatal(err)
	}
	if r.bootstrapFeatureScene3dParticleBurstPath != "/activity/runtime/bootstrap-feature-scene3d-particle-burst.abc.js" {
		t.Fatalf("burst URL must use the production chunk version: %s", r.bootstrapFeatureScene3dParticleBurstPath)
	}
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(`{"particleBursts":true}`)}, gosx.Text(""))
	if markup := gosx.RenderHTML(r.BootstrapScript()); !strings.Contains(markup, `data-gosx-scene3d-particle-burst-url="/activity/runtime/bootstrap-feature-scene3d-particle-burst.abc.js"`) {
		t.Fatal("opted-in burst must advertise its hashed manifest URL")
	}
}
