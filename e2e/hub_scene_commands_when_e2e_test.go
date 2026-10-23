//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
)

// recordCommandTimingScript runs before any page script. It records, for the
// "scene" mount, whether data-gosx-scene3d-revealed was already "true" at the
// moment the first command batch arrived.
const recordCommandTimingScript = `
window.__commandsAt = 0;
window.__revealedAtCommands = null;
document.addEventListener("gosx:scene3d:commands", function(event) {
  if (!event.target || event.target.id !== "scene" || window.__commandsAt) return;
  window.__commandsAt = performance.now();
  window.__revealedAtCommands = event.target.getAttribute("data-gosx-scene3d-revealed");
}, true);
`

// TestHubSceneCommandsWaitForRevealInBrowser proves that a hub batch sent at
// join time waits for the first content frame when the binding asks for the
// "revealed" gate, and that a scene which never reveals still gets the batch
// after the timeout.
func TestHubSceneCommandsWaitForRevealInBrowser(t *testing.T) {
	chrome := findChrome(t)
	room := hub.New("reveal-e2e")
	room.RequireOrigin = true
	room.On("join", func(ctx *hub.Context) {
		ctx.Hub.Send(ctx.Client.ID, "scene:update", map[string]any{
			"revision": 1,
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
	pageRuntime.BindHub("reveal", "/ws", []hydrate.HubBinding{
		{Event: "scene:update", SceneMountID: "scene", SceneCommands: true, SceneCommandsWhen: "revealed"},
		{Event: "scene:update", SceneMountID: "empty", SceneCommands: true, SceneCommandsWhen: "revealed", SceneCommandsWhenTimeoutMS: 1500},
	})
	mount := func(id, props string) gosx.Node {
		return pageRuntime.Engine(engine.Config{
			Name:         scene.DefaultEngineName,
			Kind:         engine.KindSurface,
			MountID:      id,
			Props:        json.RawMessage(props),
			Capabilities: []engine.Capability{engine.CapCanvas, engine.CapWebGL},
			MountAttrs: map[string]any{
				"data-gosx-scene3d": true,
				"style":             "width: 320px; height: 200px;",
			},
		}, gosx.Text(""))
	}
	withContent := mount("scene", `{"objects":[{"kind":"box","x":0,"y":0,"z":0,"size":0.6,"color":"#d98263"}],"background":"#e8e2d8"}`)
	empty := mount("empty", `{"objects":[],"background":"#e8e2d8"}`)
	body := `<main>` + gosx.RenderHTML(withContent) + gosx.RenderHTML(empty) + `</main>`
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

	ctx, closeBrowser := hubBindingBrowser(t, chrome)
	defer closeBrowser()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := cdppage.AddScriptToEvaluateOnNewDocument(recordCommandTimingScript).Do(ctx)
			return err
		}),
		chromedp.Navigate(web.URL),
	); err != nil {
		t.Fatal(err)
	}

	waitForBrowserCondition(t, ctx, `document.getElementById("scene").__gosxScene3DState && document.getElementById("scene").__gosxScene3DState.objects.has("hub-object")`)
	var revealedAtCommands string
	var commandsAt float64
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`String(window.__revealedAtCommands)`, &revealedAtCommands),
		chromedp.Evaluate(`window.__commandsAt`, &commandsAt),
	); err != nil {
		t.Fatal(err)
	}
	if commandsAt <= 0 || revealedAtCommands != "true" {
		t.Fatalf("scene commands applied before reveal: commandsAt=%v revealed attribute at that moment=%q", commandsAt, revealedAtCommands)
	}

	// The empty scene never draws content, so only the 1.5 s timeout releases its batch.
	var revealedEmpty string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById("empty").getAttribute("data-gosx-scene3d-revealed")`, &revealedEmpty)); err != nil {
		t.Fatal(err)
	}
	if revealedEmpty != "false" {
		t.Fatalf("empty scene revealed = %q, want false", revealedEmpty)
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		var has bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(function(){var s=document.getElementById("empty").__gosxScene3DState;return !!(s&&s.objects.has("hub-object"))})()`, &has))
		if has {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("empty scene never received the batch after the reveal timeout")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
