package watch

import (
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
)

func TestWatchStrictComponentsRenderTypedQueueAndTransport(t *testing.T) {
	node, err := route.DefaultFileRenderer(
		&route.RouteContext{Data: watchSnapshot("alpha")},
		route.FilePage{FilePath: filepath.Join("[code]", "page.gsx"), Pattern: "/watch/[code]"},
	)
	if err != nil {
		t.Fatalf("render watch page: %v", err)
	}

	html := gosx.RenderHTML(node)
	wants := []string{
		`<div class="queue-row"> <span class="queue-row__index">01</span> <div class="queue-row__body"> <strong>Alien</strong> <p>1979</p> </div> <span class="queue-row__state">Ready</span> </div>`,
		`<section class="transport-shell" aria-label="Playback controls prototype"> <div class="transport-shell__head"> <div> <span class="eyebrow">Server-first</span> <h3>Transport shell</h3> </div> <p>Reserve client work for media transport only.</p> </div> <div class="transport-shell__actions"> <span class="chip chip--primary">Resume</span> <span class="chip">Subtitle warmup</span> <span class="chip">Hold canonical position</span> </div> </section>`,
	}
	for _, want := range wants {
		if !strings.Contains(html, want) {
			t.Fatalf("watch page does not contain exact strict component output\nwant: %s\ngot:  %s", want, html)
		}
	}
	if strings.Contains(html, `data-gosx-component="TransportShell"`) {
		t.Fatalf("watch page retained unresolved TransportShell placeholder: %s", html)
	}
}
