//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/server"
)

// TestHubCSSVarsApplyAndFlashInBrowser proves a hub payload reaches CSS custom
// properties with no page JavaScript, that the flash class comes and goes, and
// that prefers-reduced-motion skips the flash.
// The flash lasts 3 s so the check below cannot race the 25 ms polling.
func TestHubCSSVarsApplyAndFlashInBrowser(t *testing.T) {
	chrome := findChrome(t)
	room := hub.New("css-vars-e2e")
	room.RequireOrigin = true
	room.On("join", func(ctx *hub.Context) {
		state := json.RawMessage(`{"coreInner":"#ff8800","activity":0.5}`)
		ctx.Hub.Send(ctx.Client.ID, "tick", map[string]any{"frame": 1, "state": state})
	})
	pageRuntime := server.NewPageRuntime()
	pageRuntime.BindHub("palette", "/ws", []hydrate.HubBinding{{Event: "tick", CSSVars: &hydrate.HubCSSVars{
		Root: "state", FlashClass: "arrival", FlashMS: 3000, LiveClass: "live",
		Vars: []hydrate.HubCSSVar{{Name: "--core", Path: "coreInner"}, {Name: "--activity", Path: "activity", Digits: 2}},
	}}})
	page := "<!doctype html><html><head><meta charset=\"utf-8\"></head><body><main id=\"m\">x</main>" + gosx.RenderHTML(pageRuntime.Head()) + "</body></html>"
	app := server.New()
	app.SetRuntimeRoot(e2eRepoRoot(t))
	app.Mount("/ws", room)
	app.Mount("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	web := httptest.NewServer(app.Build())
	defer web.Close()

	ctx, closeBrowser := hubBindingBrowser(t, chrome)
	defer closeBrowser()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Navigate(web.URL)); err != nil {
		t.Fatal(err)
	}
	waitForBrowserCondition(t, ctx, `getComputedStyle(document.documentElement).getPropertyValue("--core").trim() === "#ff8800"`)
	waitForBrowserCondition(t, ctx, `getComputedStyle(document.documentElement).getPropertyValue("--activity").trim() === "0.50"`)
	waitForBrowserCondition(t, ctx, `document.documentElement.classList.contains("live")`)
	var flashed bool
	_ = chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.classList.contains("arrival")`, &flashed))
	if !flashed {
		t.Fatal("flash class must be present right after the first apply")
	}
	waitForBrowserCondition(t, ctx, `!document.documentElement.classList.contains("arrival")`)

	// Reduced motion: a second browser with the media feature emulated gets the values but no flash.
	ctx2, closeSecond := hubBindingBrowser(t, chrome)
	defer closeSecond()
	ctx2, cancel2 := context.WithTimeout(ctx2, 25*time.Second)
	defer cancel2()
	if err := chromedp.Run(ctx2,
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}),
		chromedp.Navigate(web.URL),
	); err != nil {
		t.Fatal(err)
	}
	waitForBrowserCondition(t, ctx2, `getComputedStyle(document.documentElement).getPropertyValue("--core").trim() === "#ff8800"`)
	var flashedReduced bool
	_ = chromedp.Run(ctx2, chromedp.Evaluate(`document.documentElement.classList.contains("arrival")`, &flashedReduced))
	if flashedReduced {
		t.Fatal("flash class must be skipped under prefers-reduced-motion: reduce")
	}
}
