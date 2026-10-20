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

func TestDocsScenePosterDemoLayouts(t *testing.T) {
	chrome := e2eChromePath(t)
	baseURL := os.Getenv("GOSX_E2E_BASE_URL")
	if baseURL == "" {
		baseURL = startDocsApp(t, fmt.Sprintf("http://127.0.0.1:%d", freeE2EPort(t))).baseURL
	}

	t.Run("showreel-intro", func(t *testing.T) {
		page := newBrowserPage(t, chrome, nil, 1280, 900, "", 90*time.Second)
		// Exercise the server-rendered page before the client mounts a renderer.
		if err := chromedp.Run(page.ctx, emulation.SetScriptExecutionDisabled(true)); err != nil {
			t.Fatal(err)
		}
		page.navigate(t, baseURL+"/demos/showreel")
		for _, width := range []int{1280, 375} {
			for _, fallback := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d/fallback=%t", width, fallback), func(t *testing.T) {
					if err := chromedp.Run(page.ctx, chromedp.EmulateViewport(int64(width), 900)); err != nil {
						t.Fatal(err)
					}
					var visible bool
					page.eval(t, fmt.Sprintf(`(() => {
  const stage = document.querySelector('.orbital-study__scene');
  const mount = stage.querySelector('[data-gosx-scene3d]');
  mount.setAttribute('data-gosx-scene3d-render-gpu', '%t');
  mount.setAttribute('data-gosx-scene3d-mounted', '%t');
  const poster = stage.querySelector('.gosx-scene3d-poster');
  poster.style.pointerEvents = 'auto';
  const intro = document.querySelector('.orbital-study__intro');
  intro.style.pointerEvents = 'auto';
  return Number(getComputedStyle(poster).opacity) === 1 &&
    [...intro.querySelectorAll('h1, p, a')].every(el => {
      const rect = el.getBoundingClientRect();
      const top = document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2);
      return el === top || el.contains(top);
    });
})()`, !fallback, fallback), &visible)
					if !visible {
						t.Fatal("Showreel title, instructions, or navigation are covered by the poster")
					}
				})
			}
		}
	})

	t.Run("checkers-reduced-motion-move", func(t *testing.T) {
		// Force the WebGL path on the software test renderer so this exercises
		// a successful mount rather than the intentionally static CPU fallback.
		const requireWebGL = `(() => {
  const parse = JSON.parse;
  JSON.parse = function(...args) {
    const value = parse.apply(this, args);
    for (const engine of value?.engines || []) {
      if (engine.component === 'GoSXScene3D') engine.props.requireWebGL = true;
    }
    return value;
  };
})()`
		page := newBrowserPage(t, chrome, map[string]any{
			"enable-unsafe-swiftshader": true,
			"use-angle":                 "swiftshader",
			"disable-features":          "WebGPU",
		}, 1280, 900, requireWebGL, 90*time.Second)
		if err := chromedp.Run(page.ctx,
			emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}),
		); err != nil {
			t.Fatal(err)
		}
		page.navigate(t, baseURL+"/demos/checkers")
		if !page.pollFor(`!!document.querySelector('.checkers-showcase__scene [data-gosx-scene3d-mounted="true"][data-gosx-scene3d-render-gpu="true"]') && !!document.querySelector('[data-checkers-revision]')`, 45*time.Second) {
			var state string
			page.eval(t, `(() => {
  const mount = document.querySelector('.checkers-showcase__scene [data-gosx-scene3d]');
  return JSON.stringify({mounted: mount?.dataset.gosxScene3dMounted, gpu: mount?.dataset.gosxScene3dRenderGpu,
    renderer: mount?.dataset.gosxScene3dRenderer, revision: document.querySelector('[data-checkers-root]')?.dataset.checkersRevision});
})()`, &state)
			t.Fatalf("live Checkers scene and match did not become ready: %s\n%s", state, page.Console())
		}
		// Move through the semantic board, then verify the updated scene remains
		// visible and receives pointer input under the reduced-motion policy.
		page.eval(t, `document.querySelector('.checkers-showcase__board-panel').open = true`, nil)
		var sources []int
		page.eval(t, `[...document.querySelectorAll('[data-checkers-hole][data-owner="1"]')].map(el => Number(el.dataset.checkersHole))`, &sources)
		for _, source := range sources {
			var revision string
			page.eval(t, `document.querySelector('[data-checkers-root]').dataset.checkersRevision`, &revision)
			if err := chromedp.Run(page.ctx, chromedp.Click(fmt.Sprintf(`[data-checkers-hole="%d"]`, source), chromedp.ByQuery)); err != nil {
				t.Fatal(err)
			}
			page.waitFor(t, fmt.Sprintf(`document.querySelector('[data-checkers-root]').dataset.checkersRevision !== %q`, revision), 10*time.Second, "source selection")
			var legal bool
			page.eval(t, `!!document.querySelector('[data-checkers-hole][data-legal]')`, &legal)
			if legal {
				break
			}
		}
		var destination int
		page.eval(t, `Number(document.querySelector('[data-checkers-hole][data-legal]').dataset.checkersHole)`, &destination)
		page.eval(t, `window.posterMoveCommands = 0; document.querySelector('.checkers-showcase__scene [data-gosx-scene3d-mounted]').addEventListener('gosx:scene3d:commands', () => window.posterMoveCommands++);`, nil)
		if err := chromedp.Run(page.ctx, chromedp.Click(fmt.Sprintf(`[data-checkers-hole="%d"]`, destination), chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
		page.waitFor(t, fmt.Sprintf(`document.querySelector('[data-checkers-hole="%d"]').dataset.owner === '1' && window.posterMoveCommands > 0`, destination), 10*time.Second, "committed move and scene update")
		page.eval(t, `document.querySelector('.checkers-showcase__board-panel').open = false`, nil)
		var live struct {
			Reduced    bool    `json:"reduced"`
			Visibility string  `json:"visibility"`
			Opacity    float64 `json:"opacity"`
			Transition float64 `json:"transition"`
			HitCanvas  bool    `json:"hitCanvas"`
			Top        string  `json:"top"`
		}
		page.eval(t, `(() => {
  const canvas = document.querySelector('.checkers-showcase__scene canvas');
  const poster = document.querySelector('.checkers-showcase__scene .gosx-scene3d-poster');
  canvas.scrollIntoView({block: 'center'});
  const rect = canvas.getBoundingClientRect();
  const top = document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2);
  return {reduced: matchMedia('(prefers-reduced-motion: reduce)').matches,
    visibility: getComputedStyle(canvas).visibility, opacity: Number(getComputedStyle(poster).opacity),
    transition: parseFloat(getComputedStyle(poster).transitionDuration), hitCanvas: top === canvas,
    top: top?.tagName + '.' + top?.className};
})()`, &live)
		// The page-wide motion rule caps transitions at 0.01ms with !important.
		if !live.Reduced || live.Visibility != "visible" || live.Opacity != 0 || live.Transition > 0.00001 || !live.HitCanvas {
			t.Fatalf("live Checkers canvas is obscured after a reduced-motion move: %+v", live)
		}
	})
}
