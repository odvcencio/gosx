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

	"github.com/chromedp/chromedp"
)

func TestFluidCanvasAndStatsStayWithinDemoViewport(t *testing.T) {
	chrome := e2eChromePath(t)
	var styles string
	for _, file := range []string{"global.css", "demos/layout.css", "demos/fluid/page.css"} {
		css, err := os.ReadFile(filepath.Join(e2eRepoRoot(t), "examples/gosx-docs/app", file))
		if err != nil {
			t.Fatal(err)
		}
		styles += string(css) + "\n"
	}
	// Exercise the authored CSS without an app build or GPU. The real shell
	// clips overflow, so document.scrollWidth alone misses a clipped HUD.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta name="viewport" content="width=device-width, initial-scale=1"><style>%s</style>
<div class="demos-shell" data-demo-slug="fluid"><div class="demos-body">
  <nav class="demo-dock"><ul><li class="demo-dock__item">Velocity Field</li></ul></nav>
  <main class="demo-viewport"><section class="fluid"><div class="fluid__frame">
    <canvas class="fluid__canvas" width="800" height="500"></canvas>
    <div class="fluid__hud">
      <div class="fluid__hud-title">Field details</div>
      <div class="fluid__stat"><span>Wire size</span><b class="fluid__stat-value">31104 B / 21343 avg</b></div>
      <div class="fluid__stat"><span>Rate</span><b class="fluid__stat-value">60 fps / 19 Hz</b></div>
      <div class="fluid__stat"><span>Particles</span><b class="fluid__stat-value">4000</b></div>
    </div>
  </div></section></main>
</div></div>`, styles)
	}))
	defer server.Close()
	page := newBrowserPage(t, chrome, nil, 1440, 960, "", 45*time.Second)
	page.navigate(t, server.URL)
	for _, width := range []int{1920, 1440, 1280, 1101, 1100, 1024, 768, 390, 320} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			if err := chromedp.Run(page.ctx, chromedp.EmulateViewport(int64(width), 960)); err != nil {
				t.Fatal(err)
			}
			var result struct {
				Clipped []string `json:"clipped"`
				Overlap bool     `json:"overlap"`
				Aspect  float64  `json:"aspect"`
			}
			page.eval(t, `(() => {
  const frame = document.querySelector('.fluid__frame').getBoundingClientRect();
  const canvas = document.querySelector('canvas').getBoundingClientRect();
  const hud = document.querySelector('.fluid__hud').getBoundingClientRect();
  const clipped = [...document.querySelectorAll('.fluid__canvas, .fluid__hud, .fluid__stat-value')].filter(el => {
    const r = el.getBoundingClientRect();
    return r.width <= 0 || r.left < Math.max(0, frame.left) - 1 || r.right > Math.min(innerWidth, frame.right) + 1 || el.scrollWidth > el.clientWidth + 1;
  }).map(el => el.className);
  return { clipped, overlap: canvas.right > hud.left + 1 && canvas.bottom > hud.top + 1, aspect: canvas.width / canvas.height };
})()`, &result)
			if len(result.Clipped) > 0 || result.Overlap || result.Aspect < 1.59 || result.Aspect > 1.61 {
				t.Fatalf("fluid layout at %dpx: %+v", width, result)
			}
		})
	}
}
