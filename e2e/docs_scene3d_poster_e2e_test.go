//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

func TestDocsScenePosterKeepsRendererRecoveryVisible(t *testing.T) {
	chrome := e2eChromePath(t)
	css, err := os.ReadFile(filepath.Join(e2eRepoRoot(t), "examples/gosx-docs/app/demos/layout.css"))
	if err != nil {
		t.Fatal(err)
	}
	// Use the runtime's unavailable-renderer DOM shape with the actual demo CSS.
	// No application build or GPU is needed to exercise the paint-order failure.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><style>%s</style>
<div class="gosx-scene3d-poster-stage" style="position:relative;isolation:isolate;width:min(400px,calc(100vw - 64px));height:280px;margin:32px">
  <img class="gosx-scene3d-poster" alt="" style="background:#123456">
  <div id="mount" data-gosx-scene3d-render-gpu="false" data-gosx-scene3d-mounted="true">
    <div class="gosx-scene3d-unsupported" data-gosx-scene3d-unsupported="true" role="status" style="padding:24px">
      <p>Scene rendering is unavailable. Enable hardware acceleration or update your browser.</p>
    </div>
  </div>
</div>`, css)
	}))
	defer server.Close()
	page := newBrowserPage(t, chrome, nil, 800, 640, "", 45*time.Second)
	page.navigate(t, server.URL)
	for _, width := range []int{800, 375} {
		for _, state := range []struct {
			name    string
			mounted bool
			reduced bool
		}{
			{"unavailable", false, false},
			{"renderer-lost", true, false},
			{"reduced-motion", true, true},
		} {
			t.Run(fmt.Sprintf("%d/%s", width, state.name), func(t *testing.T) {
				motion := "no-preference"
				if state.reduced {
					motion = "reduce"
				}
				if err := chromedp.Run(page.ctx,
					chromedp.EmulateViewport(int64(width), 640),
					emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: motion}}),
				); err != nil {
					t.Fatal(err)
				}
				var result struct {
					Opacity float64 `json:"opacity"`
					Above   bool    `json:"above"`
				}
				page.eval(t, fmt.Sprintf(`(() => {
  document.getElementById('mount').setAttribute('data-gosx-scene3d-mounted', '%t');
  const poster = document.querySelector('.gosx-scene3d-poster');
  const message = document.querySelector('.gosx-scene3d-unsupported');
  // Enable hit testing on the pointer-transparent poster to probe paint order.
  poster.style.pointerEvents = 'auto';
  const rect = message.getBoundingClientRect();
  const top = document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2);
  return { opacity: Number(getComputedStyle(poster).opacity), above: message.contains(top) };
})()`, state.mounted), &result)
				if result.Opacity <= 0 || result.Opacity >= 1 || !result.Above {
					t.Fatalf("recovery message is obscured: poster opacity=%g, message above poster=%t", result.Opacity, result.Above)
				}
			})
		}
	}
}
