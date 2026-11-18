package island

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/engine"
)

func TestPreloadHintsWithNonce(t *testing.T) {
	r := NewRenderer("main")
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
		Bootstrap:               buildmanifest.HashedAsset{File: "bootstrap.js", Hash: "boot"},
		BootstrapRuntime:        buildmanifest.HashedAsset{File: "bootstrap-runtime.js", Hash: "runtime"},
		BootstrapFeatureEngines: buildmanifest.HashedAsset{File: "bootstrap-feature-engines.js", Hash: "engines"},
		BootstrapFeatureScene3D: buildmanifest.HashedAsset{File: "bootstrap-feature-scene3d.js", Hash: "scene"},
	}}
	if err := r.ApplyBuildManifest(manifest, "/gosx/assets"); err != nil {
		t.Fatal(err)
	}
	r.bootstrapFeatureScene3dWebGLPath = "/gosx/scene3d-webgl.js"
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(`{"forceWebGL":true,"scene":{"objects":[{"texture":"/albedo.jpg"}]}}`)}, gosx.Text(""))
	plain := gosx.RenderHTML(r.PreloadHints())
	if got := gosx.RenderHTML(r.PreloadHintsWithNonce("")); got != plain {
		t.Fatalf("empty nonce changed hints: %s", got)
	}
	got := gosx.RenderHTML(r.PreloadHintsWithNonce(`x"<&`))
	var scripts, other int
	for _, line := range strings.Split(got, "\n") {
		if line == "" {
			continue
		}
		if strings.Contains(line, `as="script"`) {
			scripts++
			if !strings.Contains(line, `nonce="x&#34;&lt;&amp;"`) {
				t.Errorf("missing escaped nonce: %s", line)
			}
		} else {
			other++
			if strings.Contains(line, `nonce=`) {
				t.Errorf("nonce on non-script hint: %s", line)
			}
		}
	}
	if scripts < 2 || other == 0 || !strings.Contains(got, "scene3d-webgl.js") || !strings.Contains(got, "bootstrap-feature-engines") {
		t.Fatalf("expected feature and scene script hints and a non-script hint: %s", got)
	}
}
