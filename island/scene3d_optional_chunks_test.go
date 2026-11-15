package island

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
)

// Shader warm-up and text atlases are content-gated: a page advertises each
// chunk only when a scene declares the prop, and nothing fetches it eagerly.
func TestScene3DWarmupAndTextAtlasURLsAreContentGated(t *testing.T) {
	render := func(props string) string {
		r := NewRenderer("main")
		r.SetBootstrapFeatureScene3DWarmupPath("/runtime/warmup.js?v=abc")
		r.SetBootstrapFeatureScene3DTextAtlasPath("/runtime/text-atlas.js?v=abc")
		r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(props)}, gosx.Text(""))
		return gosx.RenderHTML(r.BootstrapScript())
	}
	both := render(`{"shaderWarmup":{"materials":[{"material":{"kind":"standard"}}]},"textAtlases":[{"id":"numbers","texture":"gosx-text-atlas:numbers#00","labels":["7"]}]}`)
	for _, want := range []string{
		`data-gosx-scene3d-warmup-url="/runtime/warmup.js?v=abc"`,
		`data-gosx-scene3d-text-atlas-url="/runtime/text-atlas.js?v=abc"`,
	} {
		if !strings.Contains(both, want) {
			t.Fatalf("a scene with both props must advertise %s", want)
		}
	}
	for _, eager := range []string{`src="/runtime/warmup.js?v=abc"`, `src="/runtime/text-atlas.js?v=abc"`} {
		if strings.Contains(both, eager) {
			t.Fatalf("the chunk must not load eagerly: %s", eager)
		}
	}
	if markup := render(`{"scene":{"textAtlases":[{"id":"plates"}]}}`); !strings.Contains(markup, "data-gosx-scene3d-text-atlas-url") || strings.Contains(markup, "data-gosx-scene3d-warmup-url") {
		t.Fatal("the IR props shape must gate the text atlas chunk alone")
	}
	for _, props := range []string{`{}`, `null`, `{invalid`, `{"shaderWarmup":null,"textAtlases":null}`, `{"scene":{"objects":[{"id":"box","kind":"box"}]}}`} {
		markup := render(props)
		if strings.Contains(markup, "data-gosx-scene3d-warmup-url") || strings.Contains(markup, "data-gosx-scene3d-text-atlas-url") {
			t.Fatalf("opt-in chunk URL advertised for %s", props)
		}
	}
}
