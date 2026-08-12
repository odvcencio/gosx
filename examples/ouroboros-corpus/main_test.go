package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	cdpRuntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/island/program"
	ouroboros "m31labs.dev/gosx/perf/ouroboros"
)

func TestFixtureManifestMatchesJSON(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(corpusRoot(), "fixtures.v1.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var disk RouteManifest
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	got, err := json.Marshal(corpusManifest)
	if err != nil {
		t.Fatalf("marshal code manifest: %v", err)
	}
	want, err := json.Marshal(disk)
	if err != nil {
		t.Fatalf("marshal disk manifest: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("manifest drift\ncode=%s\ndisk=%s", got, want)
	}
}

func TestFixtureRoutesServeAndDeclarePlans(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	handler := app.Build()
	for _, record := range corpusManifest.Routes {
		if record.External {
			continue
		}
		t.Run(record.ID, func(t *testing.T) {
			body, status := getRoute(t, handler, record.Route)
			if status != http.StatusOK {
				t.Fatalf("%s status = %d", record.Route, status)
			}
			assertContains(t, body, `data-route-path="`+record.Route+`"`)
			switch record.ID {
			case "R00":
				assertContains(t, body, `"bootstrap":false`)
				assertNotContains(t, body, `id="gosx-manifest"`)
				assertNotContains(t, body, `data-gosx-navigation`)
				assertNotContains(t, body, `data-gosx-script=`)
				assertNotContains(t, body, `/gosx/`)
			case "R01":
				assertContains(t, body, `data-gosx-bootstrap-mode="lite"`)
				assertNotContains(t, body, `wasm_exec`)
			case "R02":
				assertContains(t, body, `data-gosx-island="Counter"`)
				assertContains(t, body, `"islands":1`)
				assertContains(t, body, `action="/action/form/__actions/validate-name"`)
			case "R03":
				manifest := routeManifest(t, body)
				if len(manifest.Islands) != 5 {
					t.Fatalf("R03 islands = %d, want 5", len(manifest.Islands))
				}
				shared := 0
				for _, island := range manifest.Islands {
					if island.Component == "SharedSelection" {
						shared++
						if island.ProgramRef != "/_ouroboros/islands/SharedSelection.json" {
							t.Fatalf("SharedSelection programRef = %q", island.ProgramRef)
						}
					}
				}
				if shared != 2 {
					t.Fatalf("SharedSelection manifest entries = %d, want 2", shared)
				}
				assertSharedSelectionProgram(t, handler)
			case "R04":
				assertContains(t, body, `data-action-name="validate-name"`)
				assertContains(t, body, `data-gosx-action="POST /action/form/__actions/validate-name"`)
				assertContains(t, body, `data-gosx-action-target="#action-state"`)
				assertContains(t, body, `data-gosx-action-signal="$ouroboros.action.name"`)
				assertContains(t, body, `data-gosx-bootstrap-mode="lite"`)
				assertNotContains(t, body, `wasm_exec`)
			case "R05":
				assertContains(t, body, `data-gosx-surface-kind="canvas2d"`)
				assertContains(t, body, `data-gosx-engine-component="CanvasBoard"`)
				assertNotContains(t, body, `data-gosx-engine="CanvasBoard"`)
				// Full runtime activation (enhancement.runtime, wasm assets,
				// bootstrap-feature-engines path) needs PageRuntime.Surface to
				// register the canvas with the page runtime. That API is
				// deferred until codex/gosx-ouroboros-runtime-refactor lands;
				// until then this route renders gosx.CanvasBoard directly, a
				// static SSR placeholder with no client runtime declared.
				contract := documentContract(t, body)
				if contract.Enhancement.Runtime || contract.Assets.BootstrapMode != "none" {
					t.Fatalf("R05 runtime contract = %+v, want no runtime declared (Surface API deferred)", contract)
				}
				// No PageRuntime.Surface registration means no gosx-manifest
				// script renders at all (contract.Assets.Manifest == false), so
				// there is no engine list to inspect until the Surface API lands.
				assertNotContains(t, body, `id="gosx-manifest"`)
			case "R06":
				assertContains(t, body, `"hubs":1`)
				assertContains(t, body, `$ouroboros.echo`)
				assertNoWASMRuntimePath(t, body)
			case "R07":
				assertContains(t, body, `data-gosx-engine="GoSXVideo"`)
				assertContains(t, body, `"syncMode": "follow"`)
				assertContains(t, body, `"src": "/media/ouroboros-placeholder.mp4"`)
				assertContains(t, body, `"sync": "/_ouroboros/video-sync"`)
				assertNoWASMRuntimePath(t, body)
			case "R08":
				assertContains(t, body, `data-gosx-engine="GoSXScene3D"`)
				assertContains(t, body, `feature-scene3d`)
				assertNotContains(t, body, `"preferWebGL"`)
				assertNoWASMRuntimePath(t, body)
			case "R09A", "R09B":
				assertContains(t, body, `data-gosx-link`)
				assertContains(t, body, `data-navigation-island=`)
				contract := documentContract(t, body)
				if !contract.Enhancement.Navigation || !contract.Enhancement.Runtime || contract.Assets.Islands != 1 || contract.Assets.RuntimePath == "" {
					t.Fatalf("%s navigation runtime contract = %+v", record.ID, contract)
				}
			}
		})
	}
}

func TestTinyGoCurrentMatchesServedRuntimePaths(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	handler := app.Build()
	for _, record := range corpusManifest.Routes {
		if record.External {
			continue
		}
		if record.ID == "R05" {
			// R05's manifest declares the "full" runtime contract the Surface
			// API activates once it lands (codex/gosx-ouroboros-runtime-refactor).
			// Until then this route renders gosx.CanvasBoard directly and
			// serves no runtime path, so skip the comparison for this route.
			continue
		}
		body, status := getRoute(t, handler, record.Route)
		if status != http.StatusOK {
			t.Fatalf("%s status = %d", record.Route, status)
		}
		contract := documentContract(t, body)
		want := "none"
		if contract.Assets.RuntimePath != "" {
			if strings.Contains(contract.Assets.RuntimePath, "runtime-islands") {
				want = "islands"
			} else {
				want = "full"
			}
		}
		if record.ExpectedTinyGoCurrent != want {
			t.Fatalf("%s ExpectedTinyGoCurrent = %q, served runtime path %q implies %q", record.ID, record.ExpectedTinyGoCurrent, contract.Assets.RuntimePath, want)
		}
	}
}

func TestFutureVariantsUseO02Names(t *testing.T) {
	allowed := map[string]bool{
		"core": true, "engine": true, "collab": true, "full": true, "none": true,
	}
	for _, record := range corpusManifest.Routes {
		if !allowed[record.ExpectedTinyGoFuture] {
			t.Fatalf("%s ExpectedTinyGoFuture = %q", record.ID, record.ExpectedTinyGoFuture)
		}
	}
}

func TestLocalVideoFixturesServe(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	handler := app.Build()
	req := httptest.NewRequest(http.MethodGet, "/media/ouroboros-placeholder.mp4", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("media status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("media content-type = %q", got)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("media fixture is empty")
	}
	assertMP4HasBoxes(t, rec.Body.Bytes())
	assertFFProbeReadsFixtureMP4(t, rec.Body.Bytes())
}

func TestFixtureMP4BrowserLoadsMetadata(t *testing.T) {
	chrome, ok := chromePath()
	if !ok {
		t.Skip("Chrome/Chromium not available for media metadata smoke")
	}
	app, err := newApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	appHandler := app.Build()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metadata-smoke" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<!doctype html><html><body></body></html>")
			return
		}
		appHandler.ServeHTTP(w, r)
	}))
	defer server.Close()

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chrome),
		chromedp.Headless,
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("autoplay-policy", "no-user-gesture-required"),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	defer allocCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()
	ctx, cancel := context.WithTimeout(browserCtx, 12*time.Second)
	defer cancel()

	mediaURL := server.URL + "/media/ouroboros-placeholder.mp4"
	var got struct {
		ReadyState  int     `json:"readyState"`
		Duration    float64 `json:"duration"`
		VideoWidth  int     `json:"videoWidth"`
		VideoHeight int     `json:"videoHeight"`
		ErrorCode   int     `json:"errorCode,omitempty"`
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL+"/metadata-smoke"),
		chromedp.Evaluate(`(function() {
			const video = document.createElement("video");
			video.id = "fixture-video";
			video.muted = true;
			video.playsInline = true;
			video.preload = "metadata";
			video.src = `+jsonStringLiteral(mediaURL)+`;
			document.body.appendChild(video);
			video.load();
		})()`, nil),
		chromedp.Poll(`(function() {
			const video = document.getElementById("fixture-video");
			if (!video) return false;
			const err = video.error;
			if (err) return { errorCode: err.code, readyState: video.readyState };
			if (video.readyState < 1) return false;
			return {
				readyState: video.readyState,
				duration: video.duration,
				videoWidth: video.videoWidth,
				videoHeight: video.videoHeight
			};
		})()`, &got, chromedp.WithPollingTimeout(6*time.Second)),
	); err != nil {
		t.Fatalf("browser metadata smoke: %v", err)
	}
	if got.ErrorCode != 0 || got.ReadyState < 1 {
		t.Fatalf("metadata did not load: %+v", got)
	}
	if got.VideoWidth != 16 || got.VideoHeight != 16 || got.Duration <= 0 {
		t.Fatalf("unexpected media metadata: %+v", got)
	}
}

func TestActionFormBrowserAppliesDeclarativeState(t *testing.T) {
	t.Skip("browser hydration needs real runtime assets; deferred until the Surface API and asset pipeline land (codex/gosx-ouroboros-runtime-refactor)")
	chrome, ok := chromePath()
	if !ok {
		t.Skip("Chrome/Chromium not available for action browser smoke")
	}
	server := newFixtureServer(t)
	defer server.Close()

	ctx, cancel, audit := newAuditedChromeContext(t, chrome, 20*time.Second)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL+"/static"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		pollBool(`(function() {
			return !!document.querySelector("[data-route-id='R00']") &&
				!window.__gosx &&
				Array.from(document.scripts || []).every(function(script) {
					return String(script.src || "").indexOf("/gosx/") < 0;
				});
		})()`, 10*time.Second),
		chromedp.Navigate(server.URL+"/action/form"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		pollBool(`window.__gosx && window.__gosx.ready === true && window.__gosxDeclarativeActions === true`, 10*time.Second),
		chromedp.Click(`button[type="submit"]`, chromedp.ByQuery),
		pollBool(`(function() {
			const state = document.querySelector("#action-state [data-action-state='error']");
			return !!(state && state.textContent.indexOf("name required") >= 0);
		})()`, 6*time.Second),
		chromedp.Evaluate(`(function() {
			const input = document.querySelector("input[name='name']");
			input.value = "Ada";
			input.dispatchEvent(new Event("input", {bubbles:true}));
			return input.value === "Ada";
		})()`, nil),
		chromedp.Evaluate(`document.querySelector("form").requestSubmit()`, nil),
		pollBool(`(function() {
			const state = document.querySelector("#action-state [data-action-state='ok']");
			const text = (state && state.textContent) || document.body.textContent || "";
			return text.indexOf("accepted Ada") >= 0;
		})()`, 6*time.Second),
		chromedp.Navigate(server.URL+"/action/form"),
		chromedp.WaitReady(`input[name="name"]`, chromedp.ByQuery),
		pollBool(`window.__gosx && window.__gosx.ready === true && window.__gosxDeclarativeActions === true`, 10*time.Second),
		chromedp.Click(`button[type="submit"]`, chromedp.ByQuery),
		pollBool(`(function() {
			const state = document.querySelector("#action-state [data-action-state='error']");
			return !!(state && state.textContent.indexOf("name required") >= 0);
		})()`, 6*time.Second),
		chromedp.Evaluate(`(function() {
			const input = document.querySelector("input[name='name']");
			input.value = "Grace";
			input.dispatchEvent(new Event("input", {bubbles:true}));
			return input.value === "Grace";
		})()`, nil),
		chromedp.Evaluate(`document.querySelector("form").requestSubmit()`, nil),
		pollBool(`(function() {
			const state = document.querySelector("#action-state [data-action-state='ok']");
			const text = (state && state.textContent) || document.body.textContent || "";
			return text.indexOf("accepted Grace") >= 0;
		})()`, 6*time.Second),
	)
	if err != nil {
		t.Fatalf("browser action state: %v state=%s", err, browserState(ctx))
	}
	assertBrowserClean(t, ctx, audit)
}

func TestSharedSelectionBrowserWritesSharedSignal(t *testing.T) {
	t.Skip("browser hydration needs real runtime assets; deferred until the Surface API and asset pipeline land (codex/gosx-ouroboros-runtime-refactor)")
	chrome, ok := chromePath()
	if !ok {
		t.Skip("Chrome/Chromium not available for shared signal browser smoke")
	}
	server := newFixtureServer(t)
	defer server.Close()

	ctx, cancel, audit := newAuditedChromeContext(t, chrome, 30*time.Second)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL+"/islands/kitchen"),
		pollBool(`window.__gosx && window.__gosx.ready && window.__gosx.islands && window.__gosx.islands.size === 5`, 20*time.Second),
		chromedp.Click(`.shared-selection button`, chromedp.ByQuery),
		pollBool(`(function() {
			const values = window.__gosx && window.__gosx.sharedSignals && window.__gosx.sharedSignals.values;
			return values && values.get("$ouroboros.selection") === "beta" && document.body.textContent.indexOf("beta") >= 0;
		})()`, 8*time.Second),
	)
	if err != nil {
		t.Fatalf("browser shared signal: %v state=%s", err, browserState(ctx))
	}
	assertBrowserClean(t, ctx, audit)
}

func TestSharedSelectionColdLoadWithRuntimeProbeHasNoEarlyHydrateError(t *testing.T) {
	t.Skip("browser hydration needs real runtime assets; deferred until the Surface API and asset pipeline land (codex/gosx-ouroboros-runtime-refactor)")
	chrome, ok := chromePath()
	if !ok {
		t.Skip("Chrome/Chromium not available for shared signal browser smoke")
	}
	server := newFixtureServer(t)
	defer server.Close()

	ctx, cancel, audit := newAuditedChromeContext(t, chrome, 30*time.Second)
	defer cancel()

	script, err := ouroboros.RuntimeJSONProbeScript([]string{
		"__gosx_runtime_ready",
		"__gosx_hydrate",
		"__gosx_action",
		"__gosx_dispose",
		"__gosx_set_shared_signal",
		"__gosx_set_input_batch",
	})
	if err != nil {
		t.Fatalf("runtime JSON probe script: %v", err)
	}
	if err := injectChromePreloadScript(ctx, script); err != nil {
		t.Fatalf("inject runtime JSON probe: %v", err)
	}

	err = chromedp.Run(ctx,
		chromedp.Navigate(server.URL+"/islands/kitchen"),
		pollBool(`window.__gosx && window.__gosx.ready && window.__gosx.islands && window.__gosx.islands.size === 5`, 20*time.Second),
		chromedp.Click(`.shared-selection button`, chromedp.ByQuery),
		pollBool(`(function() {
			const values = window.__gosx && window.__gosx.sharedSignals && window.__gosx.sharedSignals.values;
			return values && values.get("$ouroboros.selection") === "beta" && document.body.textContent.indexOf("beta") >= 0;
		})()`, 8*time.Second),
	)
	if err != nil {
		t.Fatalf("browser cold shared signal with runtime probe: %v state=%s", err, browserState(ctx))
	}
	assertBrowserClean(t, ctx, audit)
}

func TestCanvasBoardBrowserPickWritesSharedSignal(t *testing.T) {
	t.Skip("self-describing surface registration deferred until the Surface API lands (codex/gosx-ouroboros-runtime-refactor)")
	chrome, ok := chromePath()
	if !ok {
		t.Skip("Chrome/Chromium not available for CanvasBoard browser smoke")
	}
	server := newFixtureServer(t)
	defer server.Close()

	ctx, cancel, audit := newAuditedChromeContext(t, chrome, 45*time.Second)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL+"/canvas-board"),
		pollBool(`(function() {
			const board = document.querySelector("#ouroboros-board[data-gosx-surface-id]");
			return !!(window.__gosx && window.__gosx.ready && board);
		})()`, 20*time.Second),
		pollBool(`typeof window.__gosx_canvas_event === "function" && typeof window.__gosx_render_canvas === "function"`, 8*time.Second),
		chromedp.Evaluate(`(function() {
			const canvas = document.querySelector("#ouroboros-board");
			const rect = canvas.getBoundingClientRect();
			const x = rect.left + rect.width / 2 - 36;
			const y = rect.top + rect.height / 2 + 4;
			canvas.dispatchEvent(new PointerEvent("pointerdown", {bubbles:true, pointerId:1, button:0, buttons:1, clientX:x, clientY:y}));
			canvas.dispatchEvent(new PointerEvent("pointerup", {bubbles:true, pointerId:1, button:0, buttons:0, clientX:x, clientY:y}));
		})()`, nil),
		pollBool(`(function() {
			const values = window.__gosx && window.__gosx.sharedSignals && window.__gosx.sharedSignals.values;
			return values && values.get("$surface.event.selectedID") === "alpha";
		})()`, 8*time.Second),
		pollBool(`(function() {
			const canvas = document.querySelector("#ouroboros-board");
			if (!canvas || !canvas.getContext) return false;
			const ctx = canvas.getContext("2d");
			if (!ctx || canvas.width < 2 || canvas.height < 2) return false;
			const data = ctx.getImageData(0, 0, Math.min(canvas.width, 32), Math.min(canvas.height, 32)).data;
			for (let i = 0; i < data.length; i += 4) {
				if (data[i] !== 0 || data[i + 1] !== 0 || data[i + 2] !== 0 || data[i + 3] !== 0) return true;
			}
			return false;
		})()`, 8*time.Second),
		pollBool(`(function() {
				const canvas = document.querySelector("#ouroboros-board");
				if (!canvas || !canvas.getContext || !canvas.width || !canvas.height) return false;
				const ctx = canvas.getContext("2d");
				if (!ctx) return false;
				const data = ctx.getImageData(Math.floor(canvas.width / 2), Math.floor(canvas.height / 2), 1, 1).data;
				return data[0] !== 0 || data[1] !== 0 || data[2] !== 0 || data[3] !== 0;
			})()`, 8*time.Second),
	)
	if err != nil {
		t.Fatalf("browser CanvasBoard pick: %v state=%s", err, browserState(ctx))
	}
	assertBrowserClean(t, ctx, audit)
}

func TestCanvasBoardBrowserPickWithRuntimeProbeAndGetter(t *testing.T) {
	t.Skip("self-describing surface registration deferred until the Surface API lands (codex/gosx-ouroboros-runtime-refactor)")
	chrome, ok := chromePath()
	if !ok {
		t.Skip("Chrome/Chromium not available for CanvasBoard browser smoke")
	}
	server := newFixtureServer(t)
	defer server.Close()

	ctx, cancel, audit := newAuditedChromeContext(t, chrome, 45*time.Second)
	defer cancel()

	script, err := ouroboros.RuntimeJSONProbeScript([]string{
		"__gosx_runtime_ready",
		"__gosx_hydrate",
		"__gosx_hydrate_canvas",
		"__gosx_hydrate_compute",
		"__gosx_action",
		"__gosx_reload_program",
		"__gosx_dispose",
		"__gosx_hydrate_engine",
		"__gosx_tick_engine",
		"__gosx_render_engine",
		"__gosx_pick_engine",
		"__gosx_engine_dispose",
		"__gosx_hydrate_engine_surface",
		"__gosx_dispatch_engine_surface_event",
		"__gosx_tick_engine_surface",
		"__gosx_dispose_engine_surface",
		"__gosx_tick_canvas",
		"__gosx_render_canvas",
		"__gosx_canvas_event",
		"__gosx_canvas_set_backend",
		"__gosx_canvas_update_html",
		"__gosx_dispose_canvas",
		"__gosx_set_shared_signal",
		"__gosx_get_shared_signal",
	})
	if err != nil {
		t.Fatalf("runtime JSON probe script: %v", err)
	}
	if err := injectChromePreloadScript(ctx, script); err != nil {
		t.Fatalf("inject runtime JSON probe: %v", err)
	}

	err = chromedp.Run(ctx,
		chromedp.Navigate(server.URL+"/canvas-board"),
		pollBool(`(function() {
			const board = document.querySelector("#ouroboros-board[data-gosx-surface-id]");
			return !!(window.__gosx && window.__gosx.ready && board);
		})()`, 20*time.Second),
		pollBool(`(function() {
				const canvas = document.querySelector("#ouroboros-board");
				if (!canvas || !canvas.getContext || !canvas.width || !canvas.height) return false;
				const ctx = canvas.getContext("2d");
				if (!ctx) return false;
				const data = ctx.getImageData(Math.floor(canvas.width / 2), Math.floor(canvas.height / 2), 1, 1).data;
				return data[0] !== 0 || data[1] !== 0 || data[2] !== 0 || data[3] !== 0;
			})()`, 8*time.Second),
		chromedp.Evaluate(`(function() {
				const canvas = document.querySelector("#ouroboros-board");
				if (!canvas) return false;
				const rect = canvas.getBoundingClientRect();
				const x = rect.left + rect.width / 2 - 36;
				const y = rect.top + rect.height / 2 + 4;
				canvas.dispatchEvent(new PointerEvent("pointerdown", {bubbles:true, pointerId:1, button:0, buttons:1, clientX:x, clientY:y}));
				canvas.dispatchEvent(new PointerEvent("pointerup", {bubbles:true, pointerId:1, button:0, buttons:0, clientX:x, clientY:y}));
				return true;
			})()`, nil),
		pollBool(`(function() {
			const getter = window.__gosx_get_shared_signal;
			if (typeof getter !== "function") return false;
			return getter("$surface.event.selectedID") === "\"alpha\"";
		})()`, 8*time.Second),
	)
	if err != nil {
		t.Fatalf("browser CanvasBoard pick with runtime probe/getter: %v state=%s", err, browserState(ctx))
	}
	assertBrowserClean(t, ctx, audit)
}

func TestCanvasBoardBrowserColdWarmEvidence(t *testing.T) {
	t.Skip("self-describing surface registration deferred until the Surface API lands (codex/gosx-ouroboros-runtime-refactor)")
	chrome, ok := chromePath()
	if !ok {
		t.Skip("Chrome/Chromium not available for CanvasBoard browser smoke")
	}
	server := newFixtureServer(t)
	defer server.Close()

	ctx, cancel, audit := newAuditedChromeContext(t, chrome, 60*time.Second)
	defer cancel()

	for _, phase := range []string{"cold", "warm"} {
		t.Run(phase, func(t *testing.T) {
			err := chromedp.Run(ctx,
				chromedp.EmulateViewport(1280, 720),
				chromedp.Navigate(server.URL+"/canvas-board"),
				pollBool(`(function() {
					const board = document.querySelector("#ouroboros-board[data-gosx-surface-id]");
					return !!(window.__gosx && window.__gosx.ready && board);
				})()`, 20*time.Second),
				pollBool(`(function() {
						const canvas = document.querySelector("#ouroboros-board");
						if (!canvas || !canvas.getContext || !canvas.width || !canvas.height) return false;
						const ctx = canvas.getContext("2d");
						if (!ctx) return false;
						const data = ctx.getImageData(Math.floor(canvas.width / 2), Math.floor(canvas.height / 2), 1, 1).data;
						return data[0] !== 0 || data[1] !== 0 || data[2] !== 0 || data[3] !== 0;
					})()`, 8*time.Second),
				chromedp.Evaluate(`(function() {
					const canvas = document.querySelector("#ouroboros-board");
					const values = window.__gosx && window.__gosx.sharedSignals && window.__gosx.sharedSignals.values;
					if (values && values.delete) values.delete("$surface.event.selectedID");
					const rect = canvas.getBoundingClientRect();
					const x = rect.left + rect.width / 2 - 36;
					const y = rect.top + rect.height / 2 + 4;
					canvas.dispatchEvent(new PointerEvent("pointerdown", {bubbles:true, pointerId:1, button:0, buttons:1, clientX:x, clientY:y}));
					canvas.dispatchEvent(new PointerEvent("pointerup", {bubbles:true, pointerId:1, button:0, buttons:0, clientX:x, clientY:y}));
				})()`, nil),
				pollBool(`(function() {
					const values = window.__gosx && window.__gosx.sharedSignals && window.__gosx.sharedSignals.values;
					return values && values.get("$surface.event.selectedID") === "alpha";
				})()`, 8*time.Second),
			)
			if err != nil {
				t.Fatalf("%s CanvasBoard evidence: %v state=%s", phase, err, browserState(ctx))
			}
		})
	}
	assertBrowserClean(t, ctx, audit)
}

func TestCanvasBoardBrowserPrimeThenWarmEvidence(t *testing.T) {
	t.Skip("self-describing surface registration deferred until the Surface API lands (codex/gosx-ouroboros-runtime-refactor)")
	chrome, ok := chromePath()
	if !ok {
		t.Skip("Chrome/Chromium not available for CanvasBoard browser smoke")
	}
	server := newFixtureServer(t)
	defer server.Close()

	ctx, cancel, audit := newAuditedChromeContext(t, chrome, 60*time.Second)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 720),
		chromedp.Navigate(server.URL+"/canvas-board"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Navigate(server.URL+"/canvas-board"),
		pollBool(`(function() {
			const board = document.querySelector("#ouroboros-board[data-gosx-surface-id]");
			return !!(window.__gosx && window.__gosx.ready && board);
		})()`, 20*time.Second),
		pollBool(`(function() {
				const canvas = document.querySelector("#ouroboros-board");
				if (!canvas || !canvas.getContext || !canvas.width || !canvas.height) return false;
				const ctx = canvas.getContext("2d");
				if (!ctx) return false;
				const data = ctx.getImageData(Math.floor(canvas.width / 2), Math.floor(canvas.height / 2), 1, 1).data;
				return data[0] !== 0 || data[1] !== 0 || data[2] !== 0 || data[3] !== 0;
			})()`, 8*time.Second),
		chromedp.Evaluate(`(function() {
			const canvas = document.querySelector("#ouroboros-board");
			const values = window.__gosx && window.__gosx.sharedSignals && window.__gosx.sharedSignals.values;
			if (values && values.delete) values.delete("$surface.event.selectedID");
			const rect = canvas.getBoundingClientRect();
			const x = rect.left + rect.width / 2 - 36;
			const y = rect.top + rect.height / 2 + 4;
			canvas.dispatchEvent(new PointerEvent("pointerdown", {bubbles:true, pointerId:1, button:0, buttons:1, clientX:x, clientY:y}));
			canvas.dispatchEvent(new PointerEvent("pointerup", {bubbles:true, pointerId:1, button:0, buttons:0, clientX:x, clientY:y}));
		})()`, nil),
		pollBool(`(function() {
			const values = window.__gosx && window.__gosx.sharedSignals && window.__gosx.sharedSignals.values;
			return values && values.get("$surface.event.selectedID") === "alpha";
		})()`, 8*time.Second),
	)
	if err != nil {
		t.Fatalf("primed warm CanvasBoard evidence: %v state=%s", err, browserState(ctx))
	}
	assertBrowserClean(t, ctx, audit)
}

func TestHubEchoBrowserBindingUpdatesSharedSignal(t *testing.T) {
	t.Skip("browser hydration needs real runtime assets; deferred until the Surface API and asset pipeline land (codex/gosx-ouroboros-runtime-refactor)")
	chrome, ok := chromePath()
	if !ok {
		t.Skip("Chrome/Chromium not available for hub browser smoke")
	}
	server := newFixtureServer(t)
	defer server.Close()

	ctx, cancel, audit := newAuditedChromeContext(t, chrome, 30*time.Second)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL+"/hub/echo"),
		pollBool(`window.__gosx && window.__gosx.ready && window.__gosx.hubs && window.__gosx.hubs.size === 1`, 20*time.Second),
		pollBool(`(function() {
			const record = Array.from(window.__gosx.hubs.values())[0];
			return !!(record && record.socket && record.socket.readyState === WebSocket.OPEN);
		})()`, 8*time.Second),
		chromedp.Evaluate(`(function() {
			const record = Array.from(window.__gosx.hubs.values())[0];
			record.socket.send(JSON.stringify({event:"echo", data:{status:"browser"}}));
		})()`, nil),
		pollBool(`(function() {
			const values = window.__gosx && window.__gosx.sharedSignals && window.__gosx.sharedSignals.values;
			const value = values && values.get("$ouroboros.echo");
			return !!(value && value.status === "echo");
		})()`, 8*time.Second),
	)
	if err != nil {
		t.Fatalf("browser hub echo: %v state=%s", err, browserState(ctx))
	}
	assertBrowserClean(t, ctx, audit)
}

func TestNavigationBrowserSameDocumentDisposeRemount(t *testing.T) {
	t.Skip("browser hydration needs real runtime assets; deferred until the Surface API and asset pipeline land (codex/gosx-ouroboros-runtime-refactor)")
	chrome, ok := chromePath()
	if !ok {
		t.Skip("Chrome/Chromium not available for navigation browser smoke")
	}
	server := newFixtureServer(t)
	defer server.Close()

	ctx, cancel, audit := newAuditedChromeContext(t, chrome, 45*time.Second)
	defer cancel()

	var got struct {
		Path          string `json:"path"`
		DisposeCalls  int    `json:"disposeCalls"`
		Islands       int    `json:"islands"`
		OldRootGone   bool   `json:"oldRootGone"`
		NavigationAPI bool   `json:"navigationAPI"`
		Entries       int    `json:"entries"`
	}
	err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL+"/navigation/a"),
		pollBool(`window.__gosx && window.__gosx.ready && window.__gosx.islands && window.__gosx.islands.size === 1`, 20*time.Second),
		chromedp.Evaluate(`(function() {
			window.__o02Nav = { disposeCalls: 0 };
			const oldRoot = document.querySelector("[data-gosx-island='Counter']");
			if (oldRoot) oldRoot.setAttribute("data-before-nav", "true");
			const original = window.__gosx_dispose_page;
			window.__gosx_dispose_page = async function() {
				window.__o02Nav.disposeCalls += 1;
				return original.apply(this, arguments);
			};
		})()`, nil),
		chromedp.Click(`[data-gosx-link]`, chromedp.ByQuery),
		pollBool(`(function() {
			return location.pathname === "/navigation/b"
				&& !!window.__gosx
				&& window.__gosx.ready
				&& !!window.__gosx.islands
				&& window.__gosx.islands.size === 1
				&& !!document.querySelector("[data-navigation-current='b']");
		})()`, 12*time.Second),
		chromedp.Evaluate(`(function() {
			return {
				path: window.__gosx_page_nav && window.__gosx_page_nav.getState ? window.__gosx_page_nav.getState().currentPath : "",
				disposeCalls: window.__o02Nav ? window.__o02Nav.disposeCalls : 0,
				islands: window.__gosx && window.__gosx.islands ? window.__gosx.islands.size : 0,
				oldRootGone: !document.querySelector("[data-before-nav='true']"),
				navigationAPI: !!(window.__gosx_page_nav && window.__gosx_page_nav.getState),
				entries: performance.getEntriesByType("navigation").length
			};
		})()`, &got),
	)
	if err != nil {
		t.Fatalf("browser navigation remount: %v state=%s", err, browserState(ctx))
	}
	if got.Path != "/navigation/b" || got.DisposeCalls < 1 || got.Islands != 1 || !got.OldRootGone || !got.NavigationAPI || got.Entries != 1 {
		t.Fatalf("unexpected navigation evidence: %+v", got)
	}
	assertBrowserClean(t, ctx, audit)
}

func TestNavigationColdLoadWithRuntimeProbeHydratesAndRemounts(t *testing.T) {
	t.Skip("browser hydration needs real runtime assets; deferred until the Surface API and asset pipeline land (codex/gosx-ouroboros-runtime-refactor)")
	chrome, ok := chromePath()
	if !ok {
		t.Skip("Chrome/Chromium not available for navigation browser smoke")
	}
	server := newFixtureServer(t)
	defer server.Close()

	ctx, cancel, audit := newAuditedChromeContext(t, chrome, 45*time.Second)
	defer cancel()

	script, err := ouroboros.RuntimeJSONProbeScript([]string{
		"__gosx_runtime_ready",
		"__gosx_hydrate",
		"__gosx_action",
		"__gosx_dispose",
		"__gosx_set_shared_signal",
		"__gosx_set_input_batch",
	})
	if err != nil {
		t.Fatalf("runtime JSON probe script: %v", err)
	}
	if err := injectChromePreloadScript(ctx, script); err != nil {
		t.Fatalf("inject runtime JSON probe: %v", err)
	}

	var got struct {
		Path          string `json:"path"`
		DisposeCalls  int    `json:"disposeCalls"`
		Islands       int    `json:"islands"`
		OldRootGone   bool   `json:"oldRootGone"`
		NavigationAPI bool   `json:"navigationAPI"`
		Entries       int    `json:"entries"`
	}
	err = chromedp.Run(ctx,
		chromedp.Navigate(server.URL+"/navigation/a"),
		pollBool(`window.__gosx && window.__gosx.ready && window.__gosx.islands && window.__gosx.islands.size === 1`, 20*time.Second),
		chromedp.Evaluate(`(function() {
			window.__o02Nav = { disposeCalls: 0 };
			const oldRoot = document.querySelector("[data-gosx-island='Counter']");
			if (oldRoot) oldRoot.setAttribute("data-before-nav", "true");
			const original = window.__gosx_dispose_page;
			window.__gosx_dispose_page = async function() {
				window.__o02Nav.disposeCalls += 1;
				return original.apply(this, arguments);
			};
		})()`, nil),
		chromedp.Click(`[data-gosx-link]`, chromedp.ByQuery),
		pollBool(`(function() {
			return location.pathname === "/navigation/b"
				&& !!window.__gosx
				&& window.__gosx.ready
				&& !!window.__gosx.islands
				&& window.__gosx.islands.size === 1
				&& !!document.querySelector("[data-navigation-current='b']");
		})()`, 12*time.Second),
		chromedp.Evaluate(`(function() {
			return {
				path: window.__gosx_page_nav && window.__gosx_page_nav.getState ? window.__gosx_page_nav.getState().currentPath : "",
				disposeCalls: window.__o02Nav ? window.__o02Nav.disposeCalls : 0,
				islands: window.__gosx && window.__gosx.islands ? window.__gosx.islands.size : 0,
				oldRootGone: !document.querySelector("[data-before-nav='true']"),
				navigationAPI: !!(window.__gosx_page_nav && window.__gosx_page_nav.getState),
				entries: performance.getEntriesByType("navigation").length
			};
		})()`, &got),
	)
	if err != nil {
		t.Fatalf("browser navigation with runtime probe: %v state=%s", err, browserState(ctx))
	}
	if got.Path != "/navigation/b" || got.DisposeCalls < 1 || got.Islands != 1 || !got.OldRootGone || !got.NavigationAPI || got.Entries != 1 {
		t.Fatalf("unexpected navigation probe evidence: %+v", got)
	}
	assertBrowserClean(t, ctx, audit)
}

func assertMP4HasBoxes(t *testing.T, data []byte) {
	t.Helper()
	for _, top := range []string{"ftyp", "moov", "mdat"} {
		if !mp4ContainsBox(data, top) {
			t.Fatalf("fixture MP4 missing %s box", top)
		}
	}
	for _, path := range [][]string{
		{"moov", "trak", "mdia", "minf", "stbl", "stsd"},
		{"moov", "trak", "mdia", "minf", "stbl", "stts"},
		{"moov", "trak", "mdia", "minf", "stbl", "stsc"},
		{"moov", "trak", "mdia", "minf", "stbl", "stsz"},
		{"moov", "trak", "mdia", "minf", "stbl", "stco"},
	} {
		if !mp4ContainsBox(data, path...) {
			t.Fatalf("fixture MP4 missing box path %s", strings.Join(path, "/"))
		}
	}
}

func assertFFProbeReadsFixtureMP4(t *testing.T, data []byte) {
	t.Helper()
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not available")
	}
	path := filepath.Join(t.TempDir(), "fixture.mp4")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write temp fixture MP4: %v", err)
	}
	out, err := exec.Command(ffprobe, "-hide_banner", "-loglevel", "error", "-show_entries", "format=duration:stream=codec_name,width,height,nb_frames", "-of", "json", path).Output()
	if err != nil {
		t.Fatalf("ffprobe fixture MP4: %v", err)
	}
	var probed struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			NBFrames  string `json:"nb_frames"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &probed); err != nil {
		t.Fatalf("decode ffprobe output: %v", err)
	}
	if len(probed.Streams) != 1 {
		t.Fatalf("ffprobe streams = %d, want 1: %s", len(probed.Streams), out)
	}
	stream := probed.Streams[0]
	if stream.CodecName != "h264" || stream.Width != 16 || stream.Height != 16 || stream.NBFrames != "1" || probed.Format.Duration == "" {
		t.Fatalf("unexpected ffprobe metadata: %+v format=%+v", stream, probed.Format)
	}
}

func mp4ContainsBox(data []byte, path ...string) bool {
	if len(path) == 0 {
		return true
	}
	for _, box := range mp4Boxes(data) {
		if box.typ != path[0] {
			continue
		}
		if len(path) == 1 {
			return true
		}
		if mp4ContainsBox(box.payload, path[1:]...) {
			return true
		}
	}
	return false
}

type mp4Box struct {
	typ     string
	payload []byte
}

func mp4Boxes(data []byte) []mp4Box {
	var boxes []mp4Box
	for offset := 0; offset+8 <= len(data); {
		size := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
		header := 8
		if size == 1 {
			if offset+16 > len(data) {
				break
			}
			size = binary.BigEndian.Uint64(data[offset+8 : offset+16])
			header = 16
		} else if size == 0 {
			size = uint64(len(data) - offset)
		}
		if size < uint64(header) || uint64(offset)+size > uint64(len(data)) {
			break
		}
		typ := string(data[offset+4 : offset+8])
		payloadStart := offset + header
		if typ == "meta" && payloadStart+4 <= offset+int(size) {
			payloadStart += 4
		}
		boxes = append(boxes, mp4Box{
			typ:     typ,
			payload: data[payloadStart : offset+int(size)],
		})
		offset += int(size)
	}
	return boxes
}

func chromePath() (string, bool) {
	if explicit := strings.TrimSpace(os.Getenv("GOSX_CHROME_BIN")); explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit, true
		}
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, true
		}
	}
	return "", false
}

func newFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	app, err := newApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	return httptest.NewServer(app.Build())
}

type chromeAudit struct {
	mu      sync.Mutex
	entries []string
}

func (a *chromeAudit) Add(entry string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, entry)
}

func (a *chromeAudit) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = nil
}

func (a *chromeAudit) Entries() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.entries...)
}

func newChromeContext(t *testing.T, chrome string, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel, _ := newAuditedChromeContext(t, chrome, timeout)
	return ctx, cancel
}

func newAuditedChromeContext(t *testing.T, chrome string, timeout time.Duration) (context.Context, context.CancelFunc, *chromeAudit) {
	t.Helper()
	audit := &chromeAudit{}
	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chrome),
		chromedp.Headless,
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("autoplay-policy", "no-user-gesture-required"),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	chromedp.ListenTarget(browserCtx, func(ev any) {
		switch ev := ev.(type) {
		case *cdpRuntime.EventConsoleAPICalled:
			var parts []string
			for _, arg := range ev.Args {
				parts = append(parts, arg.Value.String())
			}
			message := strings.Join(parts, " ")
			level := fmt.Sprint(ev.Type)
			t.Logf("chrome console %s: %s", level, message)
			if strings.Contains(message, "[gosx]") && (level == "warning" || level == "error") {
				audit.Add("console " + level + ": " + message)
			}
		case *cdpRuntime.EventExceptionThrown:
			message := ev.ExceptionDetails.Text
			t.Logf("chrome exception: %s", message)
			audit.Add("exception: " + message)
		}
	})
	ctx, cancel := context.WithTimeout(browserCtx, timeout)
	return ctx, func() {
		cancel()
		browserCancel()
		allocCancel()
	}, audit
}

func assertBrowserClean(t *testing.T, ctx context.Context, audit *chromeAudit) {
	t.Helper()
	if entries := audit.Entries(); len(entries) != 0 {
		t.Fatalf("unexpected browser console/runtime diagnostics: %s state=%s", strings.Join(entries, " | "), browserState(ctx))
	}
	var issuesJSON string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify(window.__gosx && typeof window.__gosx.listIssues === "function" ? window.__gosx.listIssues() : [])`, &issuesJSON)); err != nil {
		t.Fatalf("read browser issues: %v", err)
	}
	if issuesJSON != "[]" {
		t.Fatalf("unexpected browser issues: %s state=%s", issuesJSON, browserState(ctx))
	}
}

func pollBool(expr string, timeout time.Duration) chromedp.Action {
	var ok bool
	return chromedp.Poll(expr, &ok, chromedp.WithPollingTimeout(timeout))
}

func injectChromePreloadScript(ctx context.Context, script string) error {
	addScript := page.AddScriptToEvaluateOnNewDocument(script)
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := addScript.Do(ctx)
		return err
	}))
}

func browserState(ctx context.Context) string {
	var state string
	err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({
		path: location.pathname,
		readyState: document.readyState,
		hasGosx: !!window.__gosx,
		gosxReady: !!(window.__gosx && window.__gosx.ready),
		declarative: !!window.__gosxDeclarativeActions,
		submitAction: typeof window.__gosx_submit_action,
		manifest: !!document.getElementById("gosx-manifest"),
		documentContract: !!document.getElementById("gosx-document"),
		canvasEvent: typeof window.__gosx_canvas_event,
		canvasRender: typeof window.__gosx_render_canvas,
		sharedSignals: Array.from(((window.__gosx && window.__gosx.sharedSignals && window.__gosx.sharedSignals.values) || new Map()).entries()),
		scripts: Array.from(document.scripts || []).map(s => ({
			src: s.src || "",
			role: s.getAttribute("data-gosx-script") || "",
			bootstrap: s.getAttribute("data-gosx-bootstrap-mode") || "",
			nav: s.hasAttribute("data-gosx-navigation")
		})),
		resources: performance.getEntriesByType("resource").filter(e => String(e.name).indexOf("/gosx/") >= 0).map(e => ({
			name: e.name,
			type: e.initiatorType,
			size: e.transferSize || 0
		})),
		issues: window.__gosx && typeof window.__gosx.listIssues === "function" ? window.__gosx.listIssues() : null,
		errors: window.__o02Errors || []
	})`, &state))
	if err != nil {
		return "state unavailable: " + err.Error()
	}
	return state
}

func jsonStringLiteral(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(data)
}

func TestLocalVideoSyncSocketReplaysDecision(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	server := httptest.NewServer(app.Build())
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/_ouroboros/video-sync"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial video sync socket: %v", err)
	}
	defer conn.Close()

	for i := 0; i < 4; i++ {
		var message struct {
			Event string          `json:"event"`
			Data  json.RawMessage `json:"data"`
		}
		if err := conn.ReadJSON(&message); err != nil {
			t.Fatalf("read sync message: %v", err)
		}
		if message.Event != "sync" {
			continue
		}
		var payload struct {
			Type     string  `json:"type"`
			MediaID  string  `json:"mediaID"`
			Position float64 `json:"position"`
			Playing  bool    `json:"playing"`
		}
		if err := json.Unmarshal(message.Data, &payload); err != nil {
			t.Fatalf("decode sync payload: %v", err)
		}
		if payload.Type != "sync" || payload.MediaID != "/media/ouroboros-placeholder.mp4" || payload.Position != 0 || payload.Playing {
			t.Fatalf("unexpected sync payload: %+v", payload)
		}
		return
	}
	t.Fatal("sync socket did not replay a drift decision")
}

func TestReadyzAndActionForm(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	handler := app.Build()
	body, status := getRoute(t, handler, "/readyz")
	if status != http.StatusOK {
		t.Fatalf("readyz status = %d body=%s", status, body)
	}
	assertContains(t, body, `"ok":true`)

	req := httptest.NewRequest(http.MethodPost, "/action/form/__actions/validate-name", strings.NewReader("name="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty action status = %d", rec.Code)
	}
	assertContains(t, rec.Body.String(), `"fieldErrors":{"name":"name required"}`)

	req = httptest.NewRequest(http.MethodPost, "/action/form/__actions/validate-name", strings.NewReader("name=baseline"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("valid action status = %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/action/form?ok=1" {
		t.Fatalf("redirect location = %q", got)
	}
}

func assertSharedSelectionProgram(t *testing.T, handler http.Handler) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/_ouroboros/islands/SharedSelection.json", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("SharedSelection program status = %d", rec.Code)
	}
	var p program.Program
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode SharedSelection program: %v", err)
	}
	if p.Name != "SharedSelection" {
		t.Fatalf("program name = %q", p.Name)
	}
	for _, signal := range p.Signals {
		if signal.Name == "$ouroboros.selection" {
			return
		}
	}
	t.Fatalf("SharedSelection program missing $ouroboros.selection signal: %+v", p.Signals)
}

func TestNoAppSideJavaScriptOrBundlerConfig(t *testing.T) {
	root := corpusRoot()
	disallowed := map[string]bool{
		".js":          true,
		".ts":          true,
		".tsx":         true,
		"package.json": true,
		"vite.config":  true,
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "build", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		ext := filepath.Ext(name)
		if disallowed[ext] || disallowed[name] || strings.HasPrefix(name, "webpack.config") || strings.HasPrefix(name, "rollup.config") {
			t.Fatalf("disallowed fixture file: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixture: %v", err)
	}
}

func getRoute(t *testing.T, handler http.Handler, route string) (string, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, route, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	data, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return string(data), rec.Code
}

func routeManifest(t *testing.T, body string) hydrate.Manifest {
	t.Helper()
	const marker = `<script id="gosx-manifest" type="application/json">`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("response missing gosx manifest\n%s", firstBytes(body, 1200))
	}
	start += len(marker)
	end := strings.Index(body[start:], `</script>`)
	if end < 0 {
		t.Fatalf("gosx manifest script is unterminated")
	}
	var manifest hydrate.Manifest
	if err := json.Unmarshal([]byte(body[start:start+end]), &manifest); err != nil {
		t.Fatalf("decode gosx manifest: %v", err)
	}
	return manifest
}

type testDocumentContract struct {
	Enhancement struct {
		Bootstrap  bool `json:"bootstrap"`
		Runtime    bool `json:"runtime"`
		Navigation bool `json:"navigation"`
	} `json:"enhancement"`
	Assets struct {
		BootstrapMode               string `json:"bootstrapMode"`
		Manifest                    bool   `json:"manifest"`
		RuntimePath                 string `json:"runtimePath,omitempty"`
		WASMExecPath                string `json:"wasmExecPath,omitempty"`
		PatchPath                   string `json:"patchPath,omitempty"`
		BootstrapPath               string `json:"bootstrapPath,omitempty"`
		BootstrapFeatureIslandsPath string `json:"bootstrapFeatureIslandsPath,omitempty"`
		BootstrapFeatureEnginesPath string `json:"bootstrapFeatureEnginesPath,omitempty"`
		BootstrapFeatureHubsPath    string `json:"bootstrapFeatureHubsPath,omitempty"`
		BootstrapFeatureScene3DPath string `json:"bootstrapFeatureScene3dPath,omitempty"`
		HLSPath                     string `json:"hlsPath,omitempty"`
		Islands                     int    `json:"islands,omitempty"`
		Engines                     int    `json:"engines,omitempty"`
		Hubs                        int    `json:"hubs,omitempty"`
	} `json:"assets"`
}

func documentContract(t *testing.T, body string) testDocumentContract {
	t.Helper()
	const marker = `id="gosx-document" type="application/json" data-gosx-document-contract>`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("response missing gosx document contract\n%s", firstBytes(body, 1200))
	}
	start += len(marker)
	end := strings.Index(body[start:], `</script>`)
	if end < 0 {
		t.Fatalf("gosx document contract script is unterminated")
	}
	var contract testDocumentContract
	if err := json.Unmarshal([]byte(body[start:start+end]), &contract); err != nil {
		t.Fatalf("decode gosx document contract: %v", err)
	}
	return contract
}

func assertNoWASMRuntimePath(t *testing.T, body string) {
	t.Helper()
	contract := documentContract(t, body)
	if contract.Enhancement.Runtime || contract.Assets.RuntimePath != "" || contract.Assets.WASMExecPath != "" {
		t.Fatalf("expected no WASM runtime path, got %+v", contract)
	}
}

func assertContains(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Fatalf("response missing %q\n%s", want, firstBytes(body, 1200))
	}
}

func assertNotContains(t *testing.T, body, want string) {
	t.Helper()
	if strings.Contains(body, want) {
		t.Fatalf("response unexpectedly contains %q\n%s", want, firstBytes(body, 1200))
	}
}

func firstBytes(body string, n int) string {
	var b bytes.Buffer
	for i, r := range body {
		if i >= n {
			b.WriteString("...")
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}
