package island

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
)

// The Selena lighting chunk is content-gated: a page advertises it only when a
// Selena material reads lights, shadows or fog, and nothing fetches it eagerly.
func TestScene3DSelenaLightingURLIsContentGated(t *testing.T) {
	lit := `{"scene":{"objects":[{"id":"rock","kind":"box","shaderLayout":{"context":["cameraPos","lights","lightCount","shadow0"],"textures":[{"name":"shadowMap0","engine":"shadowMap0"}]}}]}}`
	r := NewRenderer("main")
	r.SetBootstrapFeatureScene3DSelenaLightingPath("/runtime/selena-lighting.js?v=abc")
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(lit)}, gosx.Text(""))
	markup := gosx.RenderHTML(r.BootstrapScript())
	if !strings.Contains(markup, `data-gosx-scene3d-selena-lighting-url="/runtime/selena-lighting.js?v=abc"`) {
		t.Fatal("a lit Selena material must advertise the lighting chunk")
	}
	if strings.Contains(markup, `src="/runtime/selena-lighting.js?v=abc"`) {
		t.Fatal("the lighting chunk must not load eagerly")
	}
	for _, props := range []string{`{}`, `null`, `{invalid`,
		`{"scene":{"objects":[{"shaderLayout":{"context":["cameraPos","sunDir","sunColor","ambient"]}}]}}`,
		`{"scene":{"objects":[{"shaderLayout":{"context":["spheres","resolution"]}}]}}`} {
		r := NewRenderer("main")
		r.SetBootstrapFeatureScene3DSelenaLightingPath("/runtime/selena-lighting.js")
		r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(props)}, gosx.Text(""))
		if strings.Contains(gosx.RenderHTML(r.BootstrapScript()), "data-gosx-scene3d-selena-lighting-url") {
			t.Fatalf("lighting URL advertised for %s", props)
		}
	}
}
