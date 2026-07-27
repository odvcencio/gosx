package island

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
)

func probeRenderer() *Renderer {
	return NewRenderer("probe")
}

func TestPosterProbeEmitsMarkup(t *testing.T) {
	props, _ := json.Marshal(map[string]any{
		"poster": "/posters/hero.9f3c.png", "posterWidth": 640, "posterHeight": 360,
		"posterAlt": "A lit sphere over a dark floor",
	})
	r := probeRenderer()
	node := r.RenderEngine(engine.Config{
		Name: "GoSXScene3D", Kind: engine.KindSurface, Props: props,
		MountAttrs: map[string]any{"data-gosx-scene3d": true, "style": "max-width:960px"},
	}, gosx.Text(""))
	html := gosx.RenderHTML(node)
	t.Logf("\n%s", html)
	for _, want := range []string{
		`data-gosx-scene3d-poster="/posters/hero.9f3c.png"`,
		"aspect-ratio:640 / 360",
		`background-image:url(&#34;/posters/hero.9f3c.png&#34;)`,
		"max-width:960px",
		`<img src="/posters/hero.9f3c.png"`,
		`alt="A lit sphere over a dark floor"`,
		`fetchpriority="high"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestPosterProbeEmitsNothingWithoutPoster(t *testing.T) {
	r := probeRenderer()
	node := r.RenderEngine(engine.Config{
		Name: "GoSXScene3D", Kind: engine.KindSurface,
		Props:      json.RawMessage(`{"background":"#000"}`),
		MountAttrs: map[string]any{"data-gosx-scene3d": true},
	}, gosx.Text(""))
	html := gosx.RenderHTML(node)
	t.Logf("\n%s", html)
	if strings.Contains(html, "poster") || strings.Contains(html, "<img") || strings.Contains(html, "style=") {
		t.Errorf("a page with no poster emitted poster markup:\n%s", html)
	}
}

func TestPosterProbeIgnoresNonSceneEngines(t *testing.T) {
	r := probeRenderer()
	node := r.RenderEngine(engine.Config{
		Name: "SomeOtherEngine", Kind: engine.KindSurface,
		Props: json.RawMessage(`{"poster":"/posters/x.png"}`),
	}, gosx.Text(""))
	if html := gosx.RenderHTML(node); strings.Contains(html, "<img") {
		t.Errorf("a non-Scene3D engine got poster markup:\n%s", html)
	}
}
