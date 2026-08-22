//go:build e2e

// Real-browser regression for the Lodestar Meridian (/demos/orrery)
// declarative-animation substrate, reproducing the QA finding that motivated
// the repair: before the fix, state.animations shipped but no graph
// AnimationClip transform target ever moved, and nothing could pause what did
// move.
//
// The test drives the shared runtime path end to end in headless Chrome:
//
//  1. The SSR payload carries the lowered clip (stable targetID refs).
//  2. In ordinary motion mode the published scene clock advances — the same
//     clock every declarative-animation consumer samples — and the canvas2d
//     fallback pixels change (headless has no GPU; the wasm runtime computes
//     the animated bundle identically for every backend).
//  3. A KEYBOARD Enter on the generic toggle flips mount+button state to
//     paused, aria-pressed=true, and the clock freezes (two samples equal).
//  4. A KEYBOARD Space resumes from the frozen pose: the clock continues from
//     its frozen value instead of resetting (no discontinuity, no runaway).
//  5. Under emulated prefers-reduced-motion the runtime reports
//     reduced-motion and disables the toggle truthfully.
package e2e

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func orreryBaseURL() string {
	if url := os.Getenv("GOSX_ORRERY_E2E_BASE_URL"); url != "" {
		return url
	}
	return "http://127.0.0.1:3075"
}

func TestOrreryDeclarativeAnimationPauseResume(t *testing.T) {
	chrome := e2eChromePath(t)
	app := startDocsApp(t, orreryBaseURL())
	// Tall viewport: the editorial panel and its toggle sit beside/below the
	// canvas, and focusing the control must not scroll the canvas out of the
	// viewport (an offscreen canvas legitimately parks the render loop).
	page := newBrowserPage(t, chrome, nil, 1280, 2600, "", 180*time.Second)

	// (1) SSR payload: lowered choreography with stable target refs ships.
	resp, err := http.Get(app.baseURL + "/demos/orrery")
	if err != nil {
		t.Fatalf("fetch /demos/orrery: %v\n\nLogs:\n%s", err, app.logs.String())
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read /demos/orrery: %v", err)
	}
	html := string(body)
	if !strings.Contains(html, "meridian-procession") {
		t.Fatalf("expected SSR HTML to contain the meridian-procession clip\n\nLogs:\n%s", app.logs.String())
	}
	if !strings.Contains(html, `"targetID": "orrery-planet-cinder"`) {
		t.Fatalf("expected SSR HTML to carry lowering-resolved stable targetID refs\n\nLogs:\n%s", app.logs.String())
	}

	// (2) Ordinary motion: the scene clock advances and pixels change.
	if status := page.navigate(t, app.baseURL+"/demos/orrery"); status < 200 || status > 299 {
		t.Fatalf("/demos/orrery returned %d\n\nLogs:\n%s", status, app.logs.String())
	}
	page.waitFor(t, `!!document.querySelector("[data-gosx-scene3d-mounted]")`, 30*time.Second, "[data-gosx-scene3d-mounted]")

	readState := func() (mode, clock, loop, loopReason, wants string) {
		var out struct {
			Mode       string `json:"mode"`
			Clock      string `json:"clock"`
			Loop       string `json:"loop"`
			LoopReason string `json:"loopReason"`
			Wants      string `json:"wants"`
		}
		page.eval(t, `(() => {
      const el = document.querySelector("[data-gosx-scene3d-mounted]");
      if (!el) return null;
      return { mode: el.getAttribute("data-gosx-scene3d-animation-state"), clock: el.getAttribute("data-gosx-scene3d-animation-clock"), loop: el.getAttribute("data-gosx-scene3d-render-loop"), loopReason: el.getAttribute("data-gosx-scene3d-render-loop-reason"), wants: el.getAttribute("data-gosx-scene3d-render-loop-wants-animation") };
    })()`, &out)
		t.Logf("[orrery] mode=%q clock=%q loop=%q reason=%q wants=%q", out.Mode, out.Clock, out.Loop, out.LoopReason, out.Wants)
		return out.Mode, out.Clock, out.Loop, out.LoopReason, out.Wants
	}

	page.waitFor(t, `(function(){var el=document.querySelector("[data-gosx-scene3d-mounted]");return el && el.getAttribute("data-gosx-scene3d-animation-state")==="playing";})()`,
		30*time.Second, "animation-state=playing")

	modeA, clockA, _, _, _ := readState()
	time.Sleep(1200 * time.Millisecond)
	modeB, clockB, _, _, _ := readState()
	if modeA != "playing" || modeB != "playing" {
		t.Fatalf("ordinary motion mode expected playing, got %q -> %q\n\nConsole:\n%s", modeA, modeB, page.Console())
	}
	if clockA == "" || clockA == clockB {
		t.Fatalf("scene clock did not advance over 1.2s (%q -> %q): declarative choreography is inert again\n\nConsole:\n%s",
			clockA, clockB, page.Console())
	}

	// Pixel evidence on the software backend (headless has no GPU): the
	// animated bundle differs frame to frame. On hardware backends the
	// drawing buffer is not readable without preserveDrawingBuffer, so the
	// clock assertions above carry the proof instead.
	var backend string
	page.eval(t, `document.querySelector("[data-gosx-scene3d-mounted]").getAttribute("data-gosx-scene3d-backend")`, &backend)
	if backend == "canvas2d" || backend == "canvas" {
		snap := func() string {
			var data string
			page.eval(t, `document.querySelector("[data-gosx-scene3d-canvas]").toDataURL()`, &data)
			return data
		}
		pixA := snap()
		time.Sleep(900 * time.Millisecond)
		pixB := snap()
		if pixA == pixB {
			t.Fatalf("canvas2d pixels identical across 0.9s of ordinary motion\n\nConsole:\n%s", page.Console())
		}
	} else {
		t.Logf("[orrery] backend %q: skipping pixel diff (unreadable drawing buffer); clock assertions carry motion proof", backend)
	}

	// (3) KEYBOARD pause: Enter on the focused toggle freezes the clock.
	toggleSelector := `button[data-gosx-scene3d-animation-toggle]`
	page.waitFor(t, `!!document.querySelector("`+toggleSelector+`")`, 10*time.Second, "pause toggle")
	if err := chromedp.Run(page.ctx, chromedp.SendKeys(toggleSelector, kb.Enter, chromedp.ByQuery)); err != nil {
		t.Fatalf("send Enter to pause toggle: %v", err)
	}
	page.waitFor(t, `(function(){var m=document.querySelector("[data-gosx-scene3d-mounted]");var b=document.querySelector("`+toggleSelector+`");return m&&b&&m.getAttribute("data-gosx-scene3d-animation-state")==="paused"&&b.getAttribute("aria-pressed")==="true";})()`,
		10*time.Second, "paused state after Enter")

	// The paused loop must STOP rather than keep burning requestAnimationFrame
	// work at a frozen clock: after the toggle's settle render the mount
	// reports the stopped paused loop truthfully.
	page.waitFor(t, `(function(){var m=document.querySelector("[data-gosx-scene3d-mounted]");return m&&m.getAttribute("data-gosx-scene3d-render-loop")==="stopped"&&m.getAttribute("data-gosx-scene3d-render-loop-reason")==="paused"&&m.getAttribute("data-gosx-scene3d-render-loop-wants-animation")==="false";})()`,
		10*time.Second, "stopped paused render loop after Enter")

	modePaused, frozenClock, loopPaused, loopReasonPaused, wantsPaused := readState()
	time.Sleep(800 * time.Millisecond)
	modePaused2, clockWhilePaused, loopPaused2, _, _ := readState()
	if modePaused != "paused" || modePaused2 != "paused" {
		t.Fatalf("expected paused mode to persist, got %q -> %q", modePaused, modePaused2)
	}
	if loopPaused != "stopped" || loopPaused2 != "stopped" {
		t.Fatalf("a user-paused declarative scene must stop the render loop, got %q -> %q", loopPaused, loopPaused2)
	}
	if loopReasonPaused != "paused" {
		t.Fatalf("stopped paused render-loop reason must be \"paused\", got %q", loopReasonPaused)
	}
	if wantsPaused != "false" {
		t.Fatalf("wants-animation must read false while the paused loop is stopped, got %q", wantsPaused)
	}
	if clockWhilePaused != frozenClock {
		t.Fatalf("clock advanced while paused (%q -> %q): pause must freeze the scene clock exactly",
			frozenClock, clockWhilePaused)
	}

	// (4) KEYBOARD resume: Space continues from the frozen pose.
	if err := chromedp.Run(page.ctx, chromedp.SendKeys(toggleSelector, " ", chromedp.ByQuery)); err != nil {
		t.Fatalf("send Space to resume toggle: %v", err)
	}
	page.waitFor(t, `(function(){var m=document.querySelector("[data-gosx-scene3d-mounted]");var b=document.querySelector("`+toggleSelector+`");return m&&b&&m.getAttribute("data-gosx-scene3d-animation-state")==="playing"&&b.getAttribute("aria-pressed")==="false";})()`,
		10*time.Second, "playing state after Space")
	page.waitFor(t, `(function(){var m=document.querySelector("[data-gosx-scene3d-mounted]");return m&&m.getAttribute("data-gosx-scene3d-render-loop")==="active";})()`,
		10*time.Second, "render loop active again after Space")

	time.Sleep(600 * time.Millisecond)
	_, resumedClock, loopResumed, loopReasonResumed, wantsResumed := readState()
	t.Logf("[orrery] post-resume loop=%q reason=%q wants=%q", loopResumed, loopReasonResumed, wantsResumed)
	if loopResumed != "active" {
		t.Fatalf("resume must return the render loop to active, got %q", loopResumed)
	}
	if resumedClock == clockWhilePaused {
		t.Fatalf("clock did not resume after unpausing (stuck at %q)", resumedClock)
	}
	parseClock := func(v string) float64 {
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f
	}
	jump := parseClock(resumedClock) - parseClock(clockWhilePaused)
	const wall = 0.6 // seconds waited after resume; generous margin below
	if jump > wall+1.5 {
		t.Fatalf("resume jumped the scene clock by %.2fs after %.1fs of wall time: runaway delta", jump, wall)
	}
}

// TestOrreryReducedMotionDisablesToggleTruthfully covers the reduced-motion
// contract: the runtime reports reduced-motion on the mount and disables the
// pause control instead of pretending motion can be toggled.
func TestOrreryReducedMotionDisablesToggleTruthfully(t *testing.T) {
	chrome := e2eChromePath(t)
	app := startDocsApp(t, orreryBaseURL())
	initScript := `
    (function () {
      var original = window.matchMedia;
      window.matchMedia = function (query) {
        if (String(query).indexOf("prefers-reduced-motion") !== -1) {
          return {
            matches: true, media: String(query), onchange: null,
            addEventListener: function () {}, removeEventListener: function () {},
            addListener: function () {}, removeListener: function () {},
            dispatchEvent: function () { return false; },
          };
        }
        return original.call(window, query);
      };
    })();`
	page := newBrowserPage(t, chrome, nil, 1280, 800, initScript, 120*time.Second)
	if status := page.navigate(t, app.baseURL+"/demos/orrery"); status < 200 || status > 299 {
		t.Fatalf("/demos/orrery returned %d\n\nLogs:\n%s", status, app.logs.String())
	}
	page.waitFor(t, `!!document.querySelector("[data-gosx-scene3d-mounted]")`, 30*time.Second, "[data-gosx-scene3d-mounted]")
	page.waitFor(t, `(function(){var m=document.querySelector("[data-gosx-scene3d-mounted]");return m&&m.getAttribute("data-gosx-scene3d-animation-state")==="reduced-motion";})()`,
		30*time.Second, "reduced-motion state")
	var disabled string
	page.eval(t, `(() => { var b=document.querySelector("button[data-gosx-scene3d-animation-toggle]"); return b ? (b.disabled ? "true" : "false") : "missing"; })()`, &disabled)
	if disabled != "true" {
		t.Fatalf("pause toggle should be disabled under reduced motion, got %q\n\nConsole:\n%s", disabled, page.Console())
	}
}
