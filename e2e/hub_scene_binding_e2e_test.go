//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/internal/chrometest"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
)

// TestHubSceneBindingsRoundTripInBrowser exercises the generated GoSX hub
// bootstrap in two isolated Chrome instances. A scene input from one page is
// validated by the Go hub handler, broadcast back as a command batch, and
// applied by the other page's Scene3D mount bridge.
func TestHubSceneBindingsRoundTripInBrowser(t *testing.T) {
	chrome := findChrome(t)
	var accepted atomic.Int32
	room := hub.New("scene-binding-e2e")
	room.RequireOrigin = true
	room.On("scene:edit", func(ctx *hub.Context) {
		var input struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(ctx.Data, &input); err != nil || input.Kind != "edit" {
			return
		}
		revision := accepted.Add(1)
		room.Broadcast("scene:update", map[string]any{
			"revision": revision,
			"commands": []scene.Command{{
				Kind:     scene.CommandCreateObject,
				ObjectID: "hub-object",
				Data: scene.CommandPayload{Kind: "box", Geometry: "box", Props: map[string]any{
					"x": 1.25, "y": 0, "z": 0, "size": 0.4, "color": "#d98263",
				}},
			}},
		})
	})

	pageRuntime := server.NewPageRuntime()
	pageRuntime.BindHub("tabletop", "/ws", []hydrate.HubBinding{
		{Event: "scene:update", SceneMountID: "scene", SceneCommands: true},
		{Event: "scene:edit", Direction: "out", SceneMountID: "scene", SceneInput: "pick", SceneInputKind: "edit"},
	})
	sceneMount := pageRuntime.Engine(engine.Config{
		Name:         scene.DefaultEngineName,
		Kind:         engine.KindSurface,
		MountID:      "scene",
		Props:        json.RawMessage(`{"objects":[],"background":"#e8e2d8"}`),
		Capabilities: []engine.Capability{engine.CapCanvas, engine.CapWebGL},
		MountAttrs: map[string]any{
			"data-gosx-scene3d": true,
			"style":             "width: 480px; height: 300px;",
		},
	}, gosx.Text(""))
	body := `<main>` + gosx.RenderHTML(sceneMount) + `</main>`
	page := "<!doctype html><html><head><meta charset=\"utf-8\"></head><body>" + body + gosx.RenderHTML(pageRuntime.Head()) + "</body></html>"
	app := server.New()
	app.SetRuntimeRoot(e2eRepoRoot(t))
	app.Mount("/ws", room)
	app.Mount("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	web := httptest.NewServer(app.Build())
	defer web.Close()

	firstCtx, closeFirst := hubBindingBrowser(t, chrome)
	defer closeFirst()
	secondCtx, closeSecond := hubBindingBrowser(t, chrome)
	defer closeSecond()
	// Starting the second Chrome can consume its startup budget. Start page
	// deadlines only after both processes are bound, so it cannot expire the
	// first browser's assertions before navigation even begins.
	firstCtx, cancelFirst := context.WithTimeout(firstCtx, 25*time.Second)
	defer cancelFirst()
	secondCtx, cancelSecond := context.WithTimeout(secondCtx, 25*time.Second)
	defer cancelSecond()

	for _, ctx := range []context.Context{firstCtx, secondCtx} {
		if err := chromedp.Run(ctx, chromedp.Navigate(web.URL)); err != nil {
			t.Fatalf("navigate binding page: %v", err)
		}
		waitForBrowserCondition(t, ctx, `!!window.__gosx && window.__gosx.hubs &&
		  window.__gosx.hubs.get("gosx-hub-0") && window.__gosx.hubs.get("gosx-hub-0").socket.readyState === 1`)
		waitForBrowserCondition(t, ctx, `(() => {
      const scene = document.getElementById("scene");
      return scene && scene.getAttribute("data-gosx-scene3d-command-ready") === "true" && scene.__gosxScene3DState;
    })()`)
	}

	if err := chromedp.Run(firstCtx, chromedp.Evaluate(`document.getElementById("scene").dispatchEvent(
	  new CustomEvent("gosx:scene3d:input", {bubbles: true, detail: {kind: "pick", input: {objectID: "vase-1"}}})
	)`, nil)); err != nil {
		t.Fatalf("dispatch scene pick from first browser: %v", err)
	}
	waitForBrowserCondition(t, firstCtx, `document.getElementById("scene").__gosxScene3DState.objects.has("hub-object")`)
	waitForBrowserCondition(t, secondCtx, `document.getElementById("scene").__gosxScene3DState.objects.has("hub-object")`)
	var objectPosition float64
	if err := chromedp.Run(secondCtx, chromedp.Evaluate(`document.getElementById("scene").__gosxScene3DState.objects.get("hub-object").x`, &objectPosition)); err != nil {
		t.Fatalf("read replicated Scene3D object: %v", err)
	}
	if objectPosition != 1.25 {
		t.Fatalf("replicated Scene3D object x = %v, want 1.25", objectPosition)
	}
	if got := accepted.Load(); got != 1 {
		t.Fatalf("Go hub accepted %d edits, want exactly one", got)
	}
}

func hubBindingBrowser(t *testing.T, chrome string) (context.Context, func()) {
	t.Helper()
	browser, err := chrometest.Start(t.Context(), chrome,
		"--no-sandbox", "--use-angle=swiftshader", "--enable-webgl", "--mute-audio")
	if err != nil {
		t.Fatalf("start browser for hub binding: %v", err)
	}
	return browser.Context, browser.Close
}

func waitForBrowserCondition(t *testing.T, ctx context.Context, expression string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var ready bool
		if err := chromedp.Run(ctx, chromedp.Evaluate("Boolean("+expression+")", &ready)); err != nil {
			t.Fatalf("evaluate browser readiness: %v", err)
		}
		if ready {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	var state string
	_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({
	  title: document.title,
	  gosx: Object.keys(window.__gosx || {}),
	  hubs: window.__gosx && window.__gosx.hubs ? Array.from(window.__gosx.hubs.keys()) : [],
	  scripts: Array.from(document.scripts).map(function(script) { return [script.src, script.getAttribute("data-gosx-script")]; })
	})`, &state))
	t.Fatalf("browser condition did not become true: %s; page=%s", fmt.Sprint(expression), state)
}
