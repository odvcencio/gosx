//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		app.baseURL = fmt.Sprintf("http://127.0.0.1:%d", freeE2EPort(t))
		app = startDocsApp(t, app.baseURL)
	}
	roomID := fmt.Sprintf("%016x", uint64(time.Now().UnixNano()))
	roomURL := app.baseURL + "/demos/tabletop?room=" + roomID
	flags := map[string]any{"force-prefers-reduced-motion": "reduce", "mute-audio": true}
	traceRoomUI := `window.__tabletopHubEvents = []; window.__tabletopInputs = []; window.__tabletopSends = []; document.addEventListener('gosx:hub:event', (event) => { if (event.detail?.event === 'room:ui') window.__tabletopHubEvents.push({at: Date.now(), data: event.detail.data}); }); window.addEventListener('pointerdown', (event) => window.__tabletopInputs.push({at: Date.now(), type: event.pointerType, x: event.clientX, y: event.clientY}), true); const __tabletopSend = WebSocket.prototype.send; WebSocket.prototype.send = function(data) { try { const message = JSON.parse(data); window.__tabletopSends.push({at: Date.now(), event: message.event}); } catch (_) {} return __tabletopSend.call(this, data); };`
	first := newBrowserPage(t, chrome, flags, 390, 844, traceRoomUI, 90*time.Second)
	second := newBrowserPage(t, chrome, flags, 390, 844, traceRoomUI, 90*time.Second)
	for name, page := range map[string]*browserPage{"first": first, "second": second} {
		if status := page.navigate(t, roomURL); status < 200 || status > 299 {
			t.Fatalf("%s browser got HTTP %d for shared room\n\nLogs:\n%s", name, status, app.logs.String())
		}
		var hubRoomID string
		page.eval(t, `new URL(Array.from(window.__gosx.hubs.values())[0].entry.path, location.origin).searchParams.get('room')`, &hubRoomID)
		if hubRoomID != roomID {
			t.Fatalf("%s browser joined room %q from its hub manifest, want requested room %q", name, hubRoomID, roomID)
		}
		page.waitFor(t, `document.querySelector('#tabletop-scene')?.getAttribute('data-gosx-scene3d-command-ready') === 'true'`, 30*time.Second, "Scene3D command receiver")
		page.waitFor(t, `document.querySelector('.tabletop__scene-facts p:first-child strong')?.textContent.trim() === '4'`, 10*time.Second, "four seed objects")
		var horizontalOverflow bool
		page.eval(t, `document.documentElement.scrollWidth > document.documentElement.clientWidth`, &horizontalOverflow)
		if horizontalOverflow {
			t.Fatalf("%s browser has horizontal overflow at 390 px", name)
		}
	}
	first.waitFor(t, `document.querySelector('.tabletop__presence strong')?.textContent.includes('2 here')`, 10*time.Second, "two visitors in one room")
	second.waitFor(t, `document.querySelector('.tabletop__presence strong')?.textContent.includes('2 here')`, 10*time.Second, "shared visitor count")

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
		if updateAt == 0 || lag > 500*time.Millisecond {
			var trace string
			_ = page.tryEval(`JSON.stringify({inputs: window.__tabletopInputs, sends: window.__tabletopSends})`, &trace)
			t.Fatalf("%s visitor received no room:ui placement update within 500 ms (lag %s, events %s, input/send trace %s)", name, lag, rawEvents, trace)
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

func saveTabletopEvidenceFrame(t *testing.T, page *browserPage, name string) {
	t.Helper()
	dir := os.Getenv("GOSX_TABLETOP_EVIDENCE_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create tabletop evidence directory: %v", err)
	}
	data := page.screenshotElement(t, ".tabletop__stage")
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
