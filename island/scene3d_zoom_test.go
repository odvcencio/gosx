package island

import (
	"encoding/json"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"strings"
	"testing"
)

func TestScene3DZoomURLIsPropGated(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		r := NewRenderer("main")
		raw, err := json.Marshal(map[string]any{"controlZoom": enabled})
		if err != nil {
			t.Fatal(err)
		}
		r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: raw}, gosx.Text(""))
		markup := gosx.RenderHTML(r.BootstrapScript())
		if strings.Contains(markup, "data-gosx-scene3d-zoom-url=") != enabled {
			t.Fatalf("zoom advertisement must match opt-in %v", enabled)
		}
		if strings.Contains(markup, `src="/gosx/bootstrap-feature-scene3d-zoom.js"`) {
			t.Fatal("zoom chunk must load lazily")
		}
	}
}
