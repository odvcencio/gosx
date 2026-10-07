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

func TestScene3DTimelineURLIsAdvertisedWithoutLoading(t *testing.T) {
	r := NewRenderer("main")
	r.SetBootstrapFeatureScene3DTimelinePath("/activity/runtime/timeline.js?v=abc")
	r.RenderEngine(scene.Props{Timelines: scene.Bool(true)}.EngineConfig(), gosx.Text(""))
	markup := gosx.RenderHTML(r.BootstrapScript())
	if !strings.Contains(markup, `data-gosx-scene3d-timeline-url="/activity/runtime/timeline.js?v=abc"`) {
		t.Fatal("timeline must preserve an Activity-safe override URL")
	}
	if strings.Contains(markup, `src="/activity/runtime/timeline.js?v=abc"`) || strings.Contains(markup, `href="/activity/runtime/timeline.js?v=abc"`) {
		t.Fatal("timeline must not be fetched before playback")
	}
}

func TestScene3DTimelineURLRequiresOptIn(t *testing.T) {
	for _, props := range []string{`{}`, `{"timelines":false}`, `{"timelines":"true"}`, `null`, `{invalid`} {
		r := NewRenderer("main")
		r.SetBootstrapFeatureScene3DTimelinePath("/runtime/timeline.js")
		r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(props)}, gosx.Text(""))
		if markup := gosx.RenderHTML(r.BootstrapScript()); strings.Contains(markup, "data-gosx-scene3d-timeline-url") {
			t.Fatalf("timeline URL advertised without opt-in for %s", props)
		}
	}
}

func TestScene3DTimelineURLWithMultipleEngines(t *testing.T) {
	r := NewRenderer("main")
	r.SetBootstrapFeatureScene3DTimelinePath("/runtime/timeline.js")
	r.RenderEngine(engine.Config{Name: "Other", Props: json.RawMessage(`{"timelines":true}`)}, gosx.Text(""))
	r.RenderEngine(scene.Props{}.EngineConfig(), gosx.Text(""))
	if strings.Contains(gosx.RenderHTML(r.BootstrapScript()), "data-gosx-scene3d-timeline-url") {
		t.Fatal("another engine must not opt Scene3D into timelines")
	}
	r.RenderEngine(scene.Props{Timelines: scene.Bool(true)}.EngineConfig(), gosx.Text(""))
	if !strings.Contains(gosx.RenderHTML(r.BootstrapScript()), "data-gosx-scene3d-timeline-url") {
		t.Fatal("one opted-in scene must advertise the timeline URL")
	}
}

func TestScene3DTimelineManifestURL(t *testing.T) {
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{BootstrapFeatureScene3DTimeline: buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-timeline.abc.js", Hash: "abc"}}}
	r := NewRenderer("main")
	if err := r.ApplyBuildManifest(manifest, "/activity"); err != nil {
		t.Fatal(err)
	}
	if r.bootstrapFeatureScene3dTimelinePath != "/activity/runtime/bootstrap-feature-scene3d-timeline.abc.js" {
		t.Fatalf("timeline URL must use the production chunk version: %s", r.bootstrapFeatureScene3dTimelinePath)
	}
}
