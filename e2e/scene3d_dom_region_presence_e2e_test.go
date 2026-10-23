//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
)

// TestDOMRegionPresenceClassInBrowser proves that a custom post pass with a
// DOM region and a presenceClass puts the class on the document element while
// the card is visible in the canvas, and removes it when the card is hidden.
// Headless Chrome with SwiftShader is a software WebGL device, so the scene
// sets requireWebGL to keep the WebGL backend instead of the 2D fallback.
func TestDOMRegionPresenceClassInBrowser(t *testing.T) {
	chrome := findChrome(t)
	props, err := json.Marshal(map[string]any{
		"objects":      []map[string]any{{"kind": "box", "x": 0, "y": 0, "z": 0, "size": 0.6, "color": "#d98263"}},
		"background":   "#e8e2d8",
		"requireWebGL": true,
		"postEffects": []map[string]any{{
			"kind":       "customPost",
			"name":       "Glass",
			"vertexGLSL": "attribute vec2 a_position;\nvarying vec2 v_uv;\nvoid main() {\n  v_uv = a_position * 0.5 + 0.5;\n  gl_Position = vec4(a_position, 0.0, 1.0);\n}",
			"fragmentGLSL": "precision mediump float;\nvarying vec2 v_uv;\nuniform sampler2D _sceneColor;\n" +
				"uniform float regionCount;\nuniform float regionAspect;\nuniform vec4 region0Rect;\nuniform vec4 region0Meta;\n" +
				"void main() {\n  vec4 c = texture2D(_sceneColor, v_uv);\n  gl_FragColor = c * (1.0 + 0.0 * (regionCount + regionAspect + region0Rect.x + region0Meta.x));\n}",
			"uniforms":     map[string]any{},
			"shaderLayout": map[string]any{"uniformBlock": map[string]any{"fields": []any{}}},
			"domRegions":   map[string]any{"selector": ".glass-card", "max": 1, "presenceClass": "glass-live"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	pageRuntime := server.NewPageRuntime()
	mount := pageRuntime.Engine(engine.Config{
		Name:         scene.DefaultEngineName,
		Kind:         engine.KindSurface,
		MountID:      "scene",
		Props:        json.RawMessage(props),
		Capabilities: []engine.Capability{engine.CapCanvas, engine.CapWebGL},
		MountAttrs: map[string]any{
			"data-gosx-scene3d": true,
			"style":             "width: 480px; height: 300px;",
		},
	}, gosx.Text(""))
	body := `<main><div style="position:relative;width:480px;height:300px">` + gosx.RenderHTML(mount) +
		`<div class="glass-card" id="card" style="position:absolute;left:40px;top:40px;width:120px;height:60px;border-radius:12px">text</div></div></main>`
	page := "<!doctype html><html><head><meta charset=\"utf-8\"></head><body style=\"margin:0\">" + body + gosx.RenderHTML(pageRuntime.Head()) + "</body></html>"
	app := server.New()
	app.SetRuntimeRoot(e2eRepoRoot(t))
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
	waitForBrowserCondition(t, ctx, `document.getElementById("scene").getAttribute("data-gosx-scene3d-ready") === "true"`)
	waitForBrowserCondition(t, ctx, `document.getElementById("scene").getAttribute("data-gosx-scene3d-backend") === "webgl"`)
	var fallback string
	_ = chromedp.Run(ctx, chromedp.Evaluate(`String(document.getElementById("scene").getAttribute("data-gosx-scene3d-renderer-fallback") || "")`, &fallback))
	if fallback != "" {
		t.Fatalf("renderer fell back (%q); fix the test shader before trusting the presence class", fallback)
	}
	waitForBrowserCondition(t, ctx, `document.documentElement.classList.contains("glass-live")`)
	var presence string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById("scene").getAttribute("data-gosx-scene3d-dom-region-presence")`, &presence)); err != nil {
		t.Fatal(err)
	}
	if presence != "Glass:1" {
		t.Fatalf("dom-region-presence = %q, want Glass:1", presence)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById("card").style.display = "none"; window.dispatchEvent(new Event("resize"))`, nil)); err != nil {
		t.Fatal(err)
	}
	waitForBrowserCondition(t, ctx, `!document.documentElement.classList.contains("glass-live")`)
}
