//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// softwareGL forces SwiftShader so the hardware-gpu gate fails deterministically.
var softwareGL = map[string]any{"enable-unsafe-swiftshader": true, "use-angle": "swiftshader", "disable-features": "WebGPU"}

func sceneBytesRequested(page *browserPage) bool {
	return page.anyRequest(func(u string) bool {
		// The scene3d-defer chunk holds the gates, so it must load to decide.
		return (strings.Contains(u, "bootstrap-feature-scene3d") && !strings.Contains(u, "bootstrap-feature-scene3d-defer")) || strings.Contains(u, "/gosx/scene3d/")
	})
}

func TestScene3DGateKeepsPosterAndRequestsNoSceneBytes(t *testing.T) {
	chrome := e2eChromePath(t)
	app := startDocsApp(t, "http://127.0.0.1:3073")
	for _, tc := range []struct {
		name, gates, reason string
		reduced             bool
	}{
		{"software-gpu", "hardware-gpu", "hardware-gpu", false},
		{"reduced-motion", "full-motion", "full-motion", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := newBrowserPage(t, chrome, softwareGL, 1280, 800, "", 60*time.Second)
			if tc.reduced {
				if err := chromedp.Run(page.ctx, emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}})); err != nil {
					t.Fatal(err)
				}
			}
			page.navigate(t, app.baseURL+"/test/scene3d-defer?gates="+tc.gates)
			page.waitFor(t, `document.querySelector('#defer-scene-a')?.getAttribute('data-gosx-scene3d-defer-state') === 'gated'`, 20*time.Second, "gated state")
			time.Sleep(2 * time.Second) // give a wrongly scheduled download time to show up
			if sceneBytesRequested(page) {
				t.Fatalf("a failed gate must request no scene bytes:\n%s", strings.Join(page.Requests(), "\n"))
			}
			var state struct {
				Poster   bool   `json:"poster"`
				Canvas   bool   `json:"canvas"`
				Renderer string `json:"renderer"`
				Reason   string `json:"reason"`
			}
			page.eval(t, `(() => ({
    poster: !!document.querySelector('#defer-scene-a [data-fixture-poster]'),
    canvas: !!document.querySelector('#defer-scene-a canvas'),
    renderer: document.documentElement.getAttribute('data-gosx-renderer'),
    reason: document.documentElement.getAttribute('data-gosx-renderer-reason')}))()`, &state)
			if !state.Poster || state.Canvas || state.Renderer != "none" || state.Reason != tc.reason {
				t.Fatalf("state = %+v\n%s", state, page.Console())
			}
		})
	}
}

func TestScene3DDeferMountsAfterIdleAndSurvivesNavigation(t *testing.T) {
	chrome := e2eChromePath(t)
	app := startDocsApp(t, "http://127.0.0.1:3074")
	page := newBrowserPage(t, chrome, softwareGL, 1280, 800, "", 90*time.Second)
	page.navigate(t, app.baseURL+"/test/scene3d-defer")
	page.waitFor(t, `document.querySelector('#defer-scene-a')?.getAttribute('data-gosx-scene3d-defer-state') === 'mounted' && document.querySelector('#defer-scene-a[data-gosx-scene3d-mounted="true"]') !== null`, 45*time.Second, "deferred mount")
	if !sceneBytesRequested(page) {
		t.Fatal("the deferred scene must have fetched its props and chunk")
	}
	var hints int
	page.eval(t, `document.querySelectorAll('link[rel="preload"][href*="bootstrap-feature-scene3d.js"]').length`, &hints)
	if hints < 1 {
		t.Fatal("the runtime must emit the scene chunk preload when the deferred load starts")
	}
	if err := chromedp.Run(page.ctx, chromedp.Click("#defer-next", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	page.waitFor(t, `location.search.includes('scene=b') && document.querySelector('#defer-scene-b')?.getAttribute('data-gosx-scene3d-defer-state') === 'mounted'`, 45*time.Second, "second deferred mount after managed navigation")
	var leftovers int
	page.eval(t, `document.querySelectorAll('#defer-scene-a').length`, &leftovers)
	if leftovers != 0 {
		t.Fatalf("the previous mount must be gone after navigation, found %d", leftovers)
	}
	if errs := page.PageErrors(); len(errs) > 0 {
		t.Fatalf("page errors: %v", errs)
	}
}
