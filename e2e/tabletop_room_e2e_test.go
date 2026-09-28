//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestTabletopEditReachesSecondBrowser proves that a real Scene3D pick in one
// room reaches a second isolated browser context over the GoSX hub.
func TestTabletopEditReachesSecondBrowser(t *testing.T) {
	chrome := e2eChromePath(t)
	app := &docsApp{logs: &logBuffer{}}
	if baseURL := os.Getenv("GOSX_E2E_BASE_URL"); baseURL != "" {
		app.baseURL = baseURL
		if err := waitForHealthy(baseURL+"/readyz", 5*time.Second); err != nil {
			t.Fatalf("wait for configured docs server: %v", err)
		}
	} else {
		// The dev server does not install the docs app's custom room WebSocket
		// mount. This proof covers the routed room and generated browser runtime,
		// so use the production-shaped app unless a server was explicitly given.
		app = startProductionDocsApp(t)
		// The built app defaults PUBLIC_URL to localhost. Keep the browser origin
		// aligned with that host so the docs origin checks and WebSocket upgrade
		// exercise the same public-shaped URL.
		app.baseURL = strings.Replace(app.baseURL, "http://127.0.0.1:", "http://localhost:", 1)
	}
	roomID := fmt.Sprintf("%016x", uint64(time.Now().UnixNano()))
	roomURL := app.baseURL + "/demos/tabletop?room=" + roomID
	flags := map[string]any{"enable-unsafe-swiftshader": true, "enable-webgl": true, "use-angle": "swiftshader", "mute-audio": true}
	traceRoomUI := `window.__tabletopHubEvents = []; window.__tabletopInputs = []; window.__tabletopSends = []; document.addEventListener('gosx:hub:event', (event) => { if (event.detail?.event === 'room:ui') window.__tabletopHubEvents.push({at: Date.now(), data: event.detail.data}); }); window.addEventListener('pointerdown', (event) => window.__tabletopInputs.push({at: Date.now(), type: event.pointerType, x: event.clientX, y: event.clientY}), true); const __tabletopSend = WebSocket.prototype.send; WebSocket.prototype.send = function(data) { try { const message = JSON.parse(data); window.__tabletopSends.push({at: Date.now(), event: message.event}); } catch (_) {} return __tabletopSend.call(this, data); };`
	first := newBrowserPage(t, chrome, flags, 390, 844, traceRoomUI, 90*time.Second)
	second := newBrowserPage(t, chrome, flags, 390, 844, traceRoomUI, 90*time.Second)
	for name, page := range map[string]*browserPage{"first": first, "second": second} {
		if status := page.navigate(t, roomURL); status < 200 || status > 299 {
			t.Fatalf("%s browser got HTTP %d for shared room\n\nLogs:\n%s", name, status, app.logs.String())
		}
		page.waitFor(t, `document.querySelector('#tabletop-scene')?.getAttribute('data-gosx-scene3d-command-ready') === 'true' ||
      document.querySelector('#tabletop-scene')?.getAttribute('data-gosx-scene3d-start-state') === 'poster'`,
			30*time.Second, "Scene3D hardware start or poster fallback")
		var startState string
		page.eval(t, `document.querySelector('#tabletop-scene')?.getAttribute('data-gosx-scene3d-start-state') || ''`, &startState)
		if startState == "poster" {
			var posterOnly bool
			page.eval(t, `Array.from(window.__gosx?.hubs?.values?.() || []).every((hub) => !hub.entry?.path?.includes("/demos/tabletop/ws/")) &&
      document.querySelector('.tabletop__poster')?.complete && document.querySelector('.tabletop__poster')?.naturalWidth > 0`, &posterOnly)
			if !posterOnly {
				t.Fatalf("%s browser did not keep the poster and room hub idle without a renderer\nconsole:\n%s", name, page.Console())
			}
			t.Skip("browser has no hardware or software renderer for the live room interaction")
		}
		page.waitFor(t, `document.querySelector('#tabletop-scene')?.getAttribute('data-gosx-scene3d-command-ready') === 'true'`, 30*time.Second, "Scene3D command receiver")
		page.waitFor(t, `Array.from(window.__gosx?.hubs?.values?.() || []).some((hub) => hub.entry?.path?.includes("/demos/tabletop/ws/"))`, 10*time.Second, "tabletop hub binding after Scene3D interaction is ready")
		var hubRoomID string
		page.eval(t, `new URL(Array.from(window.__gosx.hubs.values()).find((hub) => hub.entry?.path?.includes("/demos/tabletop/ws/")).entry.path, location.origin).searchParams.get('room')`, &hubRoomID)
		if hubRoomID != roomID {
			t.Fatalf("%s browser joined room %q from its hub manifest, want requested room %q", name, hubRoomID, roomID)
		}
		page.waitFor(t, `document.querySelector('.tabletop__scene-facts p:first-child strong')?.textContent.trim() === '4'`, 10*time.Second, "four seed objects")
		var horizontalOverflow bool
		page.eval(t, `document.documentElement.scrollWidth > document.documentElement.clientWidth`, &horizontalOverflow)
		if horizontalOverflow {
			t.Fatalf("%s browser has horizontal overflow at 390 px", name)
		}
	}
	waitForTabletopVisitors(t, first, app, "first")
	waitForTabletopVisitors(t, second, app, "second")

	saveTabletopEvidenceFrame(t, first, "01-first-before.png")
	saveTabletopEvidenceFrame(t, second, "02-second-before.png")
	first.eval(t, `Array.from(document.querySelectorAll('.tabletop__control-group button')).find((button) => button.textContent.trim() === 'Plant')?.click()`, nil)
	first.waitFor(t, `Array.from(document.querySelectorAll('.tabletop__control-group button')).find((button) => button.textContent.trim() === 'Plant')?.getAttribute('aria-pressed') === 'true'`, 3*time.Second, "plant palette selection")
	saveTabletopEvidenceFrame(t, first, "03-first-plant-selected.png")
	if err := chromedp.Run(first.ctx, chromedp.ScrollIntoView(`#tabletop-scene canvas`, chromedp.ByQuery)); err != nil {
		t.Fatalf("scroll tabletop surface into view: %v", err)
	}
	var center struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	first.eval(t, `(() => { const rect = document.querySelector('#tabletop-scene canvas').getBoundingClientRect(); return {x: rect.left + rect.width / 2, y: rect.top + rect.height / 2}; })()`, &center)
	start := time.Now()
	if err := chromedp.Run(first.ctx, chromedp.MouseClickXY(center.X, center.Y)); err != nil {
		t.Fatalf("click open tabletop surface: %v\nconsole:\n%s", err, first.Console())
	}
	var pointerDownAt int64
	first.eval(t, `window.__tabletopInputs.at(-1)?.at || 0`, &pointerDownAt)
	if pointerDownAt == 0 {
		t.Fatalf("the browser did not record the tabletop pointer event: %s", first.Console())
	}
	var firstCount, secondCount string
	for time.Since(start) < 5*time.Second {
		if first.tryEval(`document.querySelector('.tabletop__scene-facts p:first-child strong')?.textContent.trim()`, &firstCount) == nil &&
			second.tryEval(`document.querySelector('.tabletop__scene-facts p:first-child strong')?.textContent.trim()`, &secondCount) == nil &&
			firstCount == "5" && secondCount == "5" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	elapsed := time.Since(start)
	if firstCount != "5" || secondCount != "5" {
		t.Fatalf("shared placement counts after %s are first=%q second=%q, want 5 in both\nFirst room:ui events: %s\nSecond room:ui events: %s\n\nFirst console:\n%s\nSecond console:\n%s", elapsed, firstCount, secondCount, evalTabletopHubEvents(first), evalTabletopHubEvents(second), first.Console(), second.Console())
	}
	for name, page := range map[string]*browserPage{"first": first, "second": second} {
		var rawEvents string
		page.eval(t, `JSON.stringify(window.__tabletopHubEvents)`, &rawEvents)
		var events []struct {
			At   int64 `json:"at"`
			Data struct {
				Objects int `json:"objects"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(rawEvents), &events); err != nil {
			t.Fatalf("decode %s room updates: %v", name, err)
		}
		var updateAt int64
		for _, event := range events {
			if event.Data.Objects == 5 && event.At >= pointerDownAt && (updateAt == 0 || event.At < updateAt) {
				updateAt = event.At
			}
		}
		lag := time.UnixMilli(updateAt).Sub(time.UnixMilli(pointerDownAt))
		deadline := 500 * time.Millisecond
		if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
			// Hosted Chrome renders the four-model scene in software. The last
			// CI run delivered the remote room update in 621 ms; keep that runner
			// bounded while preserving the 500 ms product target locally.
			deadline = 2 * time.Second
		}
		if updateAt == 0 || lag > deadline {
			var trace string
			_ = page.tryEval(`JSON.stringify({inputs: window.__tabletopInputs, sends: window.__tabletopSends})`, &trace)
			t.Fatalf("%s visitor received no room:ui placement update within %s (lag %s, events %s, input/send trace %s)", name, deadline, lag, rawEvents, trace)
		}
		t.Logf("%s visitor received the shared placement in %s", name, lag)
	}
	saveTabletopEvidenceFrame(t, first, "04-first-after-place.png")
	saveTabletopEvidenceFrame(t, second, "05-second-after-sync.png")
	time.Sleep(120 * time.Millisecond)
	saveTabletopEvidenceFrame(t, second, "06-second-confirmed.png")

	if got := first.PageErrors(); len(got) > 0 {
		t.Fatalf("first browser reported page errors: %v\nconsole:\n%s", got, first.Console())
	}
	if got := second.PageErrors(); len(got) > 0 {
		t.Fatalf("second browser reported page errors: %v\nconsole:\n%s", got, second.Console())
	}
}

func waitForTabletopVisitors(t *testing.T, page *browserPage, app *docsApp, name string) {
	t.Helper()
	if page.pollFor(`document.querySelector('.tabletop__presence strong')?.innerText.includes('2 here')`, 10*time.Second) {
		return
	}
	var state string
	_ = page.tryEval(`JSON.stringify({presence: document.querySelector('.tabletop__presence')?.innerText, hubs: Array.from(window.__gosx?.hubs?.values?.() || []).map((hub) => ({path: hub.entry?.path, url: hub.socket?.url, readyState: hub.socket?.readyState})), events: window.__tabletopHubEvents})`, &state)
	t.Fatalf("%s browser did not see two visitors in the shared room\nBrowser state: %s\nConsole:\n%s\nServer logs:\n%s", name, state, page.Console(), app.logs.String())
}

func saveTabletopEvidenceFrame(t *testing.T, page *browserPage, name string) {
	t.Helper()
	dir := os.Getenv("GOSX_TABLETOP_EVIDENCE_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create tabletop evidence directory: %v", err)
	}
	// Include the live object count below the rendered stage so each frame
	// carries a visible receipt of the shared edit, even when a placed model
	// overlaps another object from this camera angle.
	data := page.screenshotElement(t, ".tabletop__scene-panel")
	if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
		t.Fatalf("write tabletop evidence frame %s: %v", name, err)
	}
}

func evalTabletopHubEvents(page *browserPage) string {
	var events string
	if err := page.tryEval(`JSON.stringify(window.__tabletopHubEvents || [])`, &events); err != nil {
		return err.Error()
	}
	return events
}
