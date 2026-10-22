package island

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
)

func TestSelectivePageEmitsFeatureChunkScriptTags(t *testing.T) {
	r := NewRenderer("main")
	r.RenderEngine(engine.Config{Name: "Painter", Kind: engine.KindSurface}, gosx.Text(""))
	r.BindHub("ticks", "/hubs/ticks", nil)
	head := gosx.RenderHTML(r.BootstrapScript())
	bootstrapAt := strings.Index(head, `data-gosx-script="bootstrap"`)
	enginesAt := strings.Index(head, `<script defer data-gosx-script="feature-engines" src="`)
	hubsAt := strings.Index(head, `<script defer data-gosx-script="feature-hubs" src="`)
	if bootstrapAt < 0 || enginesAt < bootstrapAt || hubsAt < bootstrapAt {
		t.Fatalf("feature tags must follow the bootstrap tag: %s", head)
	}
	if strings.Contains(head, `data-gosx-script="feature-islands"`) || strings.Contains(head, `data-gosx-script="feature-controllers"`) {
		t.Fatalf("a page without islands or controllers must not load those chunks: %s", head)
	}
	hints := gosx.RenderHTML(r.PreloadHints())
	if strings.Contains(hints, "bootstrap-feature-engines") || strings.Contains(hints, "bootstrap-feature-hubs") {
		t.Fatalf("a chunk declared as a script tag must not also be preloaded: %s", hints)
	}
}
