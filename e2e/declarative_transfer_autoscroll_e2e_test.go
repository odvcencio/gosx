//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// TestDeclarativeTransferAutoScrollTouchWithSmoothScroll exercises both
// scroll destinations that a transfer can encounter in a real touch browser:
// a nested scroll pane and the document viewport. The fixture deliberately
// applies scroll-behavior:smooth to both roots. The runtime must still make
// each edge tick deterministic, keep the held pointer coordinates, re-hit the
// newly exposed target, and submit only after a real touchend.
func TestDeclarativeTransferAutoScrollTouchWithSmoothScroll(t *testing.T) {
	root := e2eRepoRoot(t)
	runtime, err := os.ReadFile(filepath.Join(root, "client", "runtime", "host", "navigation-runtime.min.js"))
	if err != nil {
		t.Fatalf("read generated navigation runtime: %v", err)
	}

	var posts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, transferAutoScrollFixtureHTML(r.URL.Query().Get("mode")))
		case "/navigation-runtime.min.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(runtime)
		case "/transfer":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			posts.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true,"message":"Assigned"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	chrome := e2eChromePath(t)
	page := newBrowserPage(t, chrome, nil, 390, 844, "", 60*time.Second)
	if err := chromedp.Run(page.ctx,
		emulation.SetDeviceMetricsOverride(390, 844, 1, true),
		emulation.SetTouchEmulationEnabled(true),
	); err != nil {
		t.Fatalf("configure 390x844 touch emulation: %v", err)
	}

	for _, tc := range []struct {
		mode           string
		name           string
		nested         bool
		wantRootScroll bool
	}{
		{mode: "nested", name: "nearest nested scroller", nested: true},
		{mode: "viewport", name: "smooth document viewport", wantRootScroll: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts.Store(0)
			if status := page.navigate(t, server.URL+"/?mode="+tc.mode); status < 200 || status > 299 {
				t.Fatalf("fixture returned %d", status)
			}
			page.waitFor(t,
				`document.querySelector("#handle")?.getAttribute("data-gosx-transfer-handle-ready") === "true"`,
				3*time.Second,
				"transfer handle preparation",
			)

			var viewport struct {
				Width  float64
				Height float64
			}
			page.eval(t, `({width: window.innerWidth, height: window.innerHeight})`, &viewport)
			if viewport.Width < 300 || viewport.Height < 300 {
				t.Fatalf("unexpected emulated viewport: %+v", viewport)
			}

			startX, startY := 60.0, 40.0
			moveX, moveY := viewport.Width-20, viewport.Height-20
			if tc.nested {
				var frame struct {
					Left   float64
					Right  float64
					Top    float64
					Bottom float64
				}
				page.eval(t, `(() => {
					const rect = document.querySelector("#frame").getBoundingClientRect();
					return {left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom};
				})()`, &frame)
				moveX, moveY = frame.Right-20, frame.Bottom-10
			}

			if err := chromedp.Run(page.ctx,
				input.DispatchTouchEvent(input.TouchStart, []*input.TouchPoint{{X: startX, Y: startY, ID: 1, Force: 1}}),
				input.DispatchTouchEvent(input.TouchMove, []*input.TouchPoint{{X: moveX, Y: moveY, ID: 1, Force: 1}}),
			); err != nil {
				t.Fatalf("dispatch touch start/move: %v", err)
			}

			if !page.pollFor(
				`document.querySelector("#target")?.classList.contains("gosx-transfer-target--over") === true`,
				5*time.Second) {
				var diagnostic string
				page.eval(t, `JSON.stringify((() => {
					const frame = document.querySelector("#frame");
					const target = document.querySelector("#target");
					const handle = document.querySelector("#handle");
					const rect = target ? target.getBoundingClientRect() : null;
					return {
						frameScrollLeft: frame ? frame.scrollLeft : 0,
						frameScrollTop: frame ? frame.scrollTop : 0,
						frameScrollWidth: frame ? frame.scrollWidth : 0,
						frameClientWidth: frame ? frame.clientWidth : 0,
						frameOverflowX: frame ? getComputedStyle(frame).overflowX : "",
						viewportScrollY: window.scrollY,
						targetClass: target ? target.className : "",
						handleGrabbed: handle ? handle.getAttribute("aria-grabbed") : "",
						rootClass: document.querySelector("[data-gosx-transfer]")?.className || "",
						targetRect: rect ? {left: rect.left, top: rect.top, right: rect.right, bottom: rect.bottom} : null,
					};
				})())`, &diagnostic)
				t.Fatalf("edge-scroll target timeout: %s", diagnostic)
			}

			var state struct {
				FrameScrollLeft  float64
				FrameScrollTop   float64
				ViewportScrollY  float64
				TargetOver       bool
				FrameBehavior    string
				DocumentBehavior string
			}
			page.eval(t, `(() => {
				const frame = document.querySelector("#frame");
				const target = document.querySelector("#target");
				return {
					frameScrollLeft: frame ? frame.scrollLeft : 0,
					frameScrollTop: frame ? frame.scrollTop : 0,
					viewportScrollY: window.scrollY,
					targetOver: !!target && target.classList.contains("gosx-transfer-target--over"),
					frameBehavior: frame ? getComputedStyle(frame).scrollBehavior : "",
					documentBehavior: getComputedStyle(document.documentElement).scrollBehavior,
				};
			})()`, &state)
			if !state.TargetOver {
				t.Fatal("target lost before touchend")
			}
			if state.DocumentBehavior != "smooth" {
				t.Fatalf("document scroll behavior = %q, want smooth", state.DocumentBehavior)
			}
			if tc.nested {
				if state.FrameScrollLeft <= 0 || state.FrameScrollTop <= 0 {
					t.Fatalf("nearest scroller did not advance on both axes: %+v", state)
				}
				if state.FrameBehavior != "smooth" {
					t.Fatalf("nearest scroller behavior = %q, want smooth", state.FrameBehavior)
				}
			} else if !tc.wantRootScroll || state.ViewportScrollY <= 0 {
				t.Fatalf("viewport edge scroll did not advance the document: %+v", state)
			}

			if err := chromedp.Run(page.ctx, input.DispatchTouchEvent(input.TouchEnd, nil)); err != nil {
				t.Fatalf("dispatch touch end: %v", err)
			}
			page.waitFor(t,
				`document.querySelector("[data-gosx-transfer]")?.getAttribute("data-gosx-form-state") === "success"`,
				3*time.Second,
				"authoritative transfer response",
			)
			if got := posts.Load(); got != 1 {
				t.Fatalf("touch transfer POST count = %d, want 1", got)
			}
		})
	}
}

func transferAutoScrollFixtureHTML(mode string) string {
	if mode == "nested" {
		return `<!doctype html>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
  html, body { margin: 0; padding: 0; scroll-behavior: smooth; }
  body { font-family: sans-serif; }
  #frame {
    width: 300px;
    height: 180px;
    overflow: auto;
    scroll-behavior: smooth;
    border: 1px solid #456;
  }
  #pool { position: relative; width: 860px; height: 840px; }
  [data-gosx-transfer-source] { position: absolute; left: 20px; top: 20px; }
  [data-gosx-transfer-target] {
    position: absolute;
    left: 600px;
    top: 740px;
    width: 260px;
    height: 96px;
  }
</style>
<section id="transfer" data-gosx-transfer="true" data-gosx-transfer-action="POST /transfer">
  <div id="frame">
    <div id="pool">
      <div data-gosx-transfer-source="player-7">
        <button id="handle" data-gosx-transfer-handle type="button">Player 7</button>
      </div>
      <button id="target" data-gosx-transfer-target="QB"
        data-gosx-transfer-eligible-for="player-7" type="button">Quarterback</button>
    </div>
  </div>
</section>
<script src="/navigation-runtime.min.js"></script>`
	}

	return `<!doctype html>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
  html, body { margin: 0; padding: 0; scroll-behavior: smooth; overflow: visible; }
  body { font-family: sans-serif; }
  #pool { position: relative; width: 100%; height: calc(100vh + 756px); }
  [data-gosx-transfer-source] { position: absolute; left: 20px; top: 20px; }
  [data-gosx-transfer-target] {
    position: absolute;
    left: calc(100% - 190px);
    top: calc(100vh + 656px);
    width: 180px;
    height: 100px;
  }
</style>
<section id="transfer" data-gosx-transfer="true" data-gosx-transfer-action="POST /transfer">
  <div id="pool">
    <div data-gosx-transfer-source="player-7">
      <button id="handle" data-gosx-transfer-handle type="button">Player 7</button>
    </div>
    <button id="target" data-gosx-transfer-target="QB"
      data-gosx-transfer-eligible-for="player-7" type="button">Quarterback</button>
  </div>
</section>
<script src="/navigation-runtime.min.js"></script>`
}
