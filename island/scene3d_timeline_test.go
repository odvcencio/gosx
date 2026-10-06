package island

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/engine"
)

func TestScene3DTimelineURLIsAdvertisedWithoutLoading(t *testing.T) {
	r := NewRenderer("main")
	r.SetBootstrapFeatureScene3DTimelinePath("/activity/runtime/timeline.js?v=abc")
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(`{}`)}, gosx.Text(""))
	markup := gosx.RenderHTML(r.BootstrapScript())
	if !strings.Contains(markup, `data-gosx-scene3d-timeline-url="/activity/runtime/timeline.js?v=abc"`) {
		t.Fatal("timeline must preserve an Activity-safe override URL")
	}
	if strings.Contains(markup, `src="/activity/runtime/timeline.js?v=abc"`) || strings.Contains(markup, `href="/activity/runtime/timeline.js?v=abc"`) {
		t.Fatal("timeline must not be fetched before playback")
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
