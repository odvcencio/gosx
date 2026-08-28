package island

import (
	"strings"
	"testing"

	gosx "m31labs.dev/gosx"
)

// TestManifestScriptReleaseOptIn pins the data-gosx-release contract. The
// attribute permits the browser runtime to drop the manifest JSON text from
// the DOM after its one memoized parse. It must never appear unrequested: a
// page whose own inline scripts read the element's text after boot would
// silently lose that data, so the default is retention and the app opts in
// per renderer.
func TestManifestScriptReleaseOptIn(t *testing.T) {
	t.Parallel()

	build := func(release bool) string {
		r := NewRenderer("main")
		r.SetBundle("main", "/gosx/runtime.wasm")
		r.RenderIsland("Counter", nil, gosx.Text("0"))
		if release {
			r.ReleaseManifestText()
		}
		return gosx.RenderHTML(r.ManifestScript())
	}

	retained := build(false)
	if strings.Contains(retained, "data-gosx-release") {
		t.Fatalf("release attribute emitted without opt-in: %q", retained)
	}

	released := build(true)
	if !strings.Contains(released, `<script id="gosx-manifest" type="application/json" data-gosx-release>`) {
		t.Fatalf("opt-in did not emit the release attribute: %q", released)
	}
	if !strings.Contains(released, `"component": "Counter"`) {
		t.Fatalf("release attribute must not change the manifest payload: %q", released)
	}
}
