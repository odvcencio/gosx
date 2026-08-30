package watch

import (
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
)

func TestRoomPreviewStrictComponentRendersNormalizedFields(t *testing.T) {
	node, err := route.DefaultFileRenderer(
		&route.RouteContext{Data: landingSnapshot()},
		route.FilePage{FilePath: filepath.Join("page.gsx"), Pattern: "/"},
	)
	if err != nil {
		t.Fatalf("render landing page: %v", err)
	}

	html := gosx.RenderHTML(node)
	want := `<article class="room-preview"> <div class="room-preview__top"> <span class="eyebrow">Playing</span> <span class="mono">Prewarmed</span> </div> <strong>Alien</strong> <p>Room Alpha</p> </article>`
	if !strings.Contains(html, want) {
		t.Fatalf("landing page does not contain exact RoomPreview output\nwant: %s\ngot:  %s", want, html)
	}
}
