package island

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/engine"
)

func TestScene3DPresentationURLsRemainLazyAndVersioned(t *testing.T) {
	for _, tc := range []struct {
		name, override, want string
	}{
		{"hashed manifest", "", "/activity/runtime/bootstrap-feature-scene3d-presentation.abc.js"},
		{"canonical override", "/gosx/bootstrap-feature-scene3d-presentation.js", "/gosx/bootstrap-feature-scene3d-presentation.js?v=abc"},
		{"embedded override", "/activity/playback.js?v=custom&mode=app", "/activity/playback.js?v=custom&amp;mode=app"},
		{"external override", "https://cdn.example/playback.js?v=custom", "https://cdn.example/playback.js?v=custom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRenderer("main")
			manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
				BootstrapFeatureScene3D:             buildmanifest.HashedAsset{File: "scene.js", Hash: "scene"},
				BootstrapFeatureScene3DPresentation: buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d-presentation.abc.js", Hash: "abc"},
			}}
			if err := r.ApplyBuildManifest(manifest, "/activity"); err != nil {
				t.Fatal(err)
			}
			if tc.override != "" {
				r.SetBootstrapFeatureScene3DPresentationPath(tc.override)
			}
			r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(`{"timelines":true}`)}, gosx.Text(""))
			markup := gosx.RenderHTML(r.BootstrapScript())
			if !strings.Contains(markup, `data-gosx-scene3d-presentation-url="`+tc.want+`"`) {
				t.Fatalf("missing presentation URL %q: %s", tc.want, markup)
			}
			if strings.Contains(markup, `src="`+tc.want+`"`) || strings.Contains(gosx.RenderHTML(r.PreloadHints()), tc.want) {
				t.Fatal("presentation chunk must load only on first playback")
			}
		})
	}
}

func TestScene3DPlaybackCanonicalURLsUseManifestVersions(t *testing.T) {
	r := NewRenderer("main")
	if err := r.ApplyBuildManifest(&buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
		BootstrapFeatureScene3D:              buildmanifest.HashedAsset{File: "scene.js"},
		BootstrapFeatureScene3DTimeline:      buildmanifest.HashedAsset{File: "timeline.abc.js", Hash: "abc"},
		BootstrapFeatureScene3DParticleBurst: buildmanifest.HashedAsset{File: "burst.def.js", Hash: "def"},
	}}, "/activity"); err != nil {
		t.Fatal(err)
	}
	r.SetBootstrapFeatureScene3DTimelinePath("/gosx/bootstrap-feature-scene3d-timeline.js")
	r.SetBootstrapFeatureScene3DParticleBurstPath("/gosx/bootstrap-feature-scene3d-particle-burst.js")
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(`{"timelines":true,"particleBursts":true}`)}, gosx.Text(""))
	markup := gosx.RenderHTML(r.BootstrapScript())
	for _, want := range []string{
		`data-gosx-scene3d-timeline-url="/gosx/bootstrap-feature-scene3d-timeline.js?v=abc"`,
		`data-gosx-scene3d-particle-burst-url="/gosx/bootstrap-feature-scene3d-particle-burst.js?v=def"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing versioned URL %s", want)
		}
	}
}
