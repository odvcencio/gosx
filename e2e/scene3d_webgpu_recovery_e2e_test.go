//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
)

// TestSceneWebGPURecoveryReloadsIntoWebGLInBrowser proves the reload contract
// of scene.Props.WebGPURecovery in a real browser: the request reloads the
// page once, the next boot is WebGL-forced and publishes that state, the
// one-shot flag is consumed, and no second reload follows.
//
// Headless Chrome with SwiftShader has no WebGPU adapter, so the strike
// counter cannot run here. runtime-45-scene-webgpu-recovery.test.js covers it
// with fake diagnostics, including the reload guard when storage cannot be
// written. This test calls the bridge directly and counts document loads on
// the server.
func TestSceneWebGPURecoveryReloadsIntoWebGLInBrowser(t *testing.T) {
	chrome := findChrome(t)
	pageRuntime := server.NewPageRuntime()
	mount := pageRuntime.Engine(engine.Config{
		Name:    scene.DefaultEngineName,
		Kind:    engine.KindSurface,
		MountID: "scene",
		Props: json.RawMessage(`{"objects":[{"kind":"box","x":0,"y":0,"z":0,"size":0.5,"color":"#d98263"}],` +
			`"preferWebGPU":true,"webgpuRecovery":{"strikes":1,"reload":true}}`),
		Capabilities: []engine.Capability{engine.CapCanvas, engine.CapWebGL},
		MountAttrs: map[string]any{
			"data-gosx-scene3d": true,
			"style":             "width: 320px; height: 200px;",
		},
	}, gosx.Text(""))
	page := "<!doctype html><html><head><meta charset=\"utf-8\"></head><body><main>" + gosx.RenderHTML(mount) + "</main>" +
		gosx.RenderHTML(pageRuntime.Head()) + "</body></html>"
	app := server.New()
	app.SetRuntimeRoot(e2eRepoRoot(t))
	var pageLoads atomic.Int32
	app.Mount("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			pageLoads.Add(1)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	web := httptest.NewServer(app.Build())
	defer web.Close()

	ctx, closeBrowser := hubBindingBrowser(t, chrome)
	defer closeBrowser()
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Navigate(web.URL)); err != nil {
		t.Fatal(err)
	}
	waitForBrowserCondition(t, ctx, `document.getElementById("scene").getAttribute("data-gosx-scene3d-ready") === "true"`)
	if got := pageLoads.Load(); got != 1 {
		t.Fatalf("page loads at first boot = %d, want 1", got)
	}
	var state string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById("scene").getAttribute("data-gosx-scene3d-webgpu-recovery")`, &state)); err != nil {
		t.Fatal(err)
	}
	if state != "inactive" {
		t.Fatalf("first boot recovery state = %q, want inactive (no WebGPU adapter here)", state)
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(`(window.__gosx && window.__gosx.scene3d && window.__gosx.scene3d.requestWebGLRecovery
	  ? window.__gosx.scene3d.requestWebGLRecovery
	  : window.__gosx_scene3d_request_webgl_recovery)({reload: true})`, nil)); err != nil {
		t.Fatal(err)
	}
	waitForBrowserCondition(t, ctx, `performance.getEntriesByType("navigation")[0].type === "reload"`)
	waitForBrowserCondition(t, ctx, `document.getElementById("scene").getAttribute("data-gosx-scene3d-ready") === "true"`)
	var backend string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById("scene").getAttribute("data-gosx-scene3d-backend")`, &backend),
		chromedp.Evaluate(`document.getElementById("scene").getAttribute("data-gosx-scene3d-webgpu-recovery")`, &state),
	); err != nil {
		t.Fatal(err)
	}
	if backend != "webgl" || state != "webgl-forced" {
		t.Fatalf("after reload backend=%q recovery=%q, want webgl and webgl-forced", backend, state)
	}
	var flag bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`sessionStorage.getItem("gosx:scene3d:force-webgl-next") === null`, &flag)); err != nil {
		t.Fatal(err)
	}
	if !flag {
		t.Fatal("the one-shot force-webgl flag must be consumed on the reloaded boot")
	}
	// The server counts document loads, so a reload loop would show here:
	// one load at first boot, one after the reload, and no more.
	if got := pageLoads.Load(); got != 2 {
		t.Fatalf("page loads after the reload = %d, want 2", got)
	}
	time.Sleep(3 * time.Second)
	if got := pageLoads.Load(); got != 2 {
		t.Fatalf("page loads after waiting = %d, want still 2 (no reload loop)", got)
	}
}
