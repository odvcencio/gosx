//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/server"
)

// TestHubLifecycleSignalAndResyncInBrowser proves the connection-state
// attribute follows a real socket through a server-side disconnect and
// reconnect, and that resync requests reach the hub and obey the rate limit.
//
// A page script cannot change visibilityState in Chrome. The online and focus
// triggers take the same resyncHubs path as visible and pageshow, so this test
// covers the trigger plumbing.
func TestHubLifecycleSignalAndResyncInBrowser(t *testing.T) {
	chrome := findChrome(t)
	var resyncs atomic.Int32
	var clientID atomic.Value
	room := hub.New("lifecycle-e2e")
	room.RequireOrigin = true
	room.Latch("state")
	room.Broadcast("state", map[string]any{"n": 1})
	room.On("join", func(ctx *hub.Context) { clientID.Store(ctx.Client.ID) })
	room.On("resync", func(ctx *hub.Context) {
		resyncs.Add(1)
		ctx.Hub.ReplayLatched(ctx.Client.ID)
	})

	pageRuntime := server.NewPageRuntime()
	id := pageRuntime.BindHub("live", "/ws", []hydrate.HubBinding{{Event: "state", Signal: "$live.state"}})
	if err := pageRuntime.SetHubLifecycle(id, hydrate.HubLifecycle{
		ConnectionSignal: "$live.connection",
		Resync:           &hydrate.HubResyncConfig{MinIntervalMS: 1200},
	}); err != nil {
		t.Fatal(err)
	}
	page := "<!doctype html><html><head><meta charset=\"utf-8\"></head><body><span id=\"s\" data-gosx-hub-status=\"live\"></span>" +
		gosx.RenderHTML(pageRuntime.Head()) + "</body></html>"
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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Navigate(web.URL)); err != nil {
		t.Fatal(err)
	}
	connection := func(state string) string {
		return `document.getElementById("s").getAttribute("data-gosx-hub-connection") === "` + state + `"`
	}
	waitForBrowserCondition(t, ctx, connection("open"))
	waitForBrowserCondition(t, ctx, `document.readyState === "complete"`)

	first, _ := clientID.Load().(string)
	if first == "" || !room.Disconnect(first, "e2e") {
		t.Fatalf("could not disconnect client %q", first)
	}
	waitForBrowserCondition(t, ctx, connection("reconnecting"))
	// The reconnect backoff starts at 500 ms; allow it to finish.
	deadline := time.Now().Add(15 * time.Second)
	for {
		var open bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(connection("open"), &open))
		if open {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hub did not reconnect within 15 s")
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.dispatchEvent(new Event('online')); window.dispatchEvent(new Event('online'));`, nil)); err != nil {
		t.Fatal(err)
	}
	waitUntil := time.Now().Add(5 * time.Second)
	for resyncs.Load() < 1 && time.Now().Before(waitUntil) {
		time.Sleep(25 * time.Millisecond)
	}
	if got := resyncs.Load(); got != 1 {
		t.Fatalf("resyncs after two online events = %d, want 1", got)
	}
	time.Sleep(1500 * time.Millisecond)
	if got := resyncs.Load(); got != 1 {
		t.Fatalf("resyncs after the rate-limit window = %d, want still 1", got)
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.dispatchEvent(new Event('focus'))`, nil)); err != nil {
		t.Fatal(err)
	}
	waitUntil = time.Now().Add(5 * time.Second)
	for resyncs.Load() < 2 && time.Now().Before(waitUntil) {
		time.Sleep(25 * time.Millisecond)
	}
	if got := resyncs.Load(); got != 2 {
		t.Fatalf("resyncs after focus = %d, want 2", got)
	}
}
