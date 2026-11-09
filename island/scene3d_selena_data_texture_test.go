package island

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
)

// The data-texture chunk is content-gated: a page advertises it only when a
// Selena material reads a data texture or names a host-written one, and
// nothing fetches it eagerly.
func TestScene3DSelenaDataTextureURLIsContentGated(t *testing.T) {
	render := func(props string) string {
		r := NewRenderer("main")
		r.SetBootstrapFeatureScene3DDataTexturePath("/runtime/data-texture.js?v=abc")
		r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(props)}, gosx.Text(""))
		return gosx.RenderHTML(r.BootstrapScript())
	}
	for _, props := range []string{
		`{"scene":{"objects":[{"id":"wall","shaderLayout":{"textures":[{"name":"spectrum","format":"r16float","sampleMode":"load"}]}}]}}`,
		`{"scene":{"objects":[{"id":"wall","customUniforms":{"spectrum":"gosx:data:spectrum"}}]}}`,
	} {
		markup := render(props)
		if !strings.Contains(markup, `data-gosx-scene3d-data-texture-url="/runtime/data-texture.js?v=abc"`) {
			t.Fatalf("a data-texture material must advertise the chunk: %s", props)
		}
		if strings.Contains(markup, `src="/runtime/data-texture.js?v=abc"`) {
			t.Fatal("the data-texture chunk must not load eagerly")
		}
	}
	for _, props := range []string{`{}`, `null`, `{invalid`,
		`{"scene":{"objects":[{"shaderLayout":{"textures":[{"name":"albedo","format":"rgba8unorm"}]}}]}}`,
		`{"scene":{"objects":[{"customUniforms":{"radiance":"gosx:environment:radiance"}}]}}`} {
		if strings.Contains(render(props), "data-gosx-scene3d-data-texture-url") {
			t.Fatalf("data-texture URL advertised for %s", props)
		}
	}
}
