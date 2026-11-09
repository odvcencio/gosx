//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/game/kitui"
	"m31labs.dev/gosx/game/replay/replayui"
)

// The replay viewer in a real browser: e2e/testdata/replaybrowser records a
// match of the toy game in wasm, then the test scrubs it with real mouse and
// keyboard input and checks that the state hash at every tick equals the hash
// live play had at that tick. Run it on the Apple GPU host with:
//
//	mac-gpu-run <worktree> go test -tags e2e -timeout 5m -run TestReplayViewerBrowser ./e2e/

type replayState struct {
	Tick, Start, End uint64
	Hash, Live       string
	Match            bool
	Paused           bool
	Speed            float64
	Camera           string
	Resim            int
	Markers          int
	Bytes            int
	Pose             [3]float64
}

func startReplayServer(t *testing.T) string {
	t.Helper()
	root := e2eRepoRoot(t)
	dir := t.TempDir()
	wasm := filepath.Join(dir, "main.wasm")
	cmd := exec.Command("go", "build", "-o", wasm, "./e2e/testdata/replaybrowser")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build replaybrowser: %v\n%s", err, out)
	}
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	var wasmExec string
	for _, rel := range []string{"lib/wasm/wasm_exec.js", "misc/wasm/wasm_exec.js"} {
		p := filepath.Join(strings.TrimSpace(string(out)), rel)
		if _, err := os.Stat(p); err == nil {
			wasmExec = p
			break
		}
	}
	if wasmExec == "" {
		t.Fatal("wasm_exec.js not found")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/main.wasm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		http.ServeFile(w, r, wasm)
	})
	mux.HandleFunc("/wasm_exec.js", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, wasmExec) })
	mux.HandleFunc("/app", func(w http.ResponseWriter, r *http.Request) {
		head := gosx.RenderHTML(kitui.Styles()) + gosx.RenderHTML(replayui.Styles()) + gosx.RenderHTML(kitui.Script())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8">%s</head><body style="margin:0;background:#101218"><div id="bar"></div>%s</body></html>`, head, loader)
	})
	app := httptest.NewServer(mux)
	t.Cleanup(app.Close)
	if e2eWindowsRelay() {
		return e2eExposeToWindows(t, app.URL)
	}
	return app.URL
}

func (p *browserPage) replayFrame(t *testing.T, ms float64) replayState {
	t.Helper()
	var raw string
	p.eval(t, fmt.Sprintf(`replay.frame(%v)`, ms), &raw)
	var st replayState
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return st
}

func TestReplayViewerBrowser(t *testing.T) {
	url := startReplayServer(t)
	p := newBrowserPage(t, e2eChromePath(t), nil, 1000, 600, "", 4*time.Minute)
	p.navigate(t, url+"/app")
	p.waitFor(t, `window.replay !== undefined || window.wasmError !== undefined`, 60*time.Second, "replay wasm start")
	var failed string
	p.eval(t, `window.wasmError || ''`, &failed)
	if failed != "" {
		t.Fatalf("wasm: %s\n%s", failed, p.Console())
	}

	st := p.replayFrame(t, 0)
	if st.Tick != 0 || !st.Paused || !st.Match || st.End != 900 || st.Markers != 2 {
		t.Fatalf("initial state: %+v", st)
	}

	t.Run("mouse click on the scrubber seeks and matches live play", func(t *testing.T) {
		var box struct{ X, Y, W, H float64 }
		p.eval(t, `(()=>{const r=document.getElementById('kit-replay-scrub').getBoundingClientRect();return {X:r.x,Y:r.y,W:r.width,H:r.height}})()`, &box)
		if box.W == 0 {
			t.Fatal("scrubber has no size")
		}
		for _, frac := range []float64{0.5, 0.1, 0.9, 0.3} {
			x, y := box.X+box.W*frac, box.Y+box.H/2
			p.run(t, input.DispatchMouseEvent(input.MousePressed, x, y).WithButton(input.Left).WithClickCount(1),
				input.DispatchMouseEvent(input.MouseReleased, x, y).WithButton(input.Left).WithClickCount(1))
			st = p.replayFrame(t, 16)
			want := frac * 900
			if float64(st.Tick) < want-60 || float64(st.Tick) > want+60 {
				t.Fatalf("click at %.0f%% seeked to tick %d, want about %.0f", frac*100, st.Tick, want)
			}
			if !st.Match {
				t.Fatalf("tick %d: replay hash %s, live %s", st.Tick, st.Hash, st.Live)
			}
		}
	})

	t.Run("keyboard on the scrubber, buttons and hotkeys", func(t *testing.T) {
		p.eval(t, `document.activeElement && document.activeElement.blur()`, nil)
		p.run(t, chromedp.Click(`[data-kit-action="replay.restart"]`, chromedp.ByQuery))
		if st = p.replayFrame(t, 16); st.Tick != 0 {
			t.Fatalf("restart: tick %d", st.Tick)
		}
		p.run(t, chromedp.Click(`[data-kit-action="replay.step_forward"]`, chromedp.ByQuery),
			chromedp.Click(`[data-kit-action="replay.step_forward"]`, chromedp.ByQuery))
		if st = p.replayFrame(t, 0); st.Tick != 2 || !st.Match {
			t.Fatalf("step forward x2: %+v", st)
		}
		// Arrow keys on the focused range input step it (native range behaviour).
		p.run(t, chromedp.Focus(`#kit-replay-scrub`, chromedp.ByQuery), chromedp.KeyEvent(kb.ArrowRight), chromedp.KeyEvent(kb.ArrowRight))
		if st = p.replayFrame(t, 0); st.Tick < 3 || !st.Match {
			t.Fatalf("range arrow keys: %+v", st)
		}
		// Speed and play through the real buttons: 4x for 60 frames of 16.667 ms.
		p.run(t, chromedp.Click(`[data-kit-action="replay.speed"][data-kit-value="4"]`, chromedp.ByQuery))
		p.run(t, chromedp.Click(`[data-kit-action="replay.toggle"]`, chromedp.ByQuery))
		start := p.replayFrame(t, 0).Tick
		for i := 0; i < 30; i++ {
			st = p.replayFrame(t, 1000.0/60)
		}
		if st.Speed != 4 || st.Paused {
			t.Fatalf("speed/play: %+v", st)
		}
		if got := st.Tick - start; got < 115 || got > 125 { // 0.5 s * 4 * 60 Hz = 120
			t.Fatalf("4x for 0.5 s advanced %d ticks, want about 120", got)
		}
		if !st.Match {
			t.Fatalf("hash mismatch during playback at %d", st.Tick)
		}
		// Hotkeys need focus off the form controls: Space pauses.
		p.eval(t, `document.activeElement.blur()`, nil)
		p.run(t, input.DispatchKeyEvent(input.KeyRawDown).WithCode("Space").WithKey(" ").WithWindowsVirtualKeyCode(32),
			input.DispatchKeyEvent(input.KeyUp).WithCode("Space").WithKey(" ").WithWindowsVirtualKeyCode(32))
		if st = p.replayFrame(t, 16); !st.Paused {
			t.Fatalf("Space should pause: %+v", st)
		}
	})

	t.Run("markers and camera picker", func(t *testing.T) {
		p.run(t, chromedp.Click(`[data-kit-action="replay.marker"][data-kit-value="600"]`, chromedp.ByQuery))
		if st = p.replayFrame(t, 0); st.Tick != 600 || !st.Match {
			t.Fatalf("marker jump: %+v", st)
		}
		for _, cam := range []string{"shot:high", "orbit", "fly", "chase"} {
			p.run(t, chromedp.Click(`[data-kit-action="replay.camera"][data-kit-value="`+cam+`"]`, chromedp.ByQuery))
			for i := 0; i < 90; i++ {
				st = p.replayFrame(t, 1000.0/60)
			}
			if st.Camera != cam {
				t.Fatalf("camera %q, want %q", st.Camera, cam)
			}
		}
		var checked string
		p.eval(t, `document.querySelector('[data-kit-action="replay.camera"][aria-checked="true"]').dataset.kitValue`, &checked)
		if checked != "chase" {
			t.Fatalf("camera picker shows %q selected", checked)
		}
	})

	t.Run("seek latency in the browser", func(t *testing.T) {
		var perSeek float64
		p.eval(t, `replay.seekBench(300)`, &perSeek)
		t.Logf("mean seek latency over 300 random seeks, 900-tick recording, keyframe every 60 ticks: %.3f ms", perSeek)
		if perSeek > 16 {
			t.Fatalf("a seek takes %.2f ms, more than a 60 Hz frame", perSeek)
		}
		if st = p.replayFrame(t, 0); !st.Match {
			t.Fatalf("hash mismatch after benchmark seeks at tick %d", st.Tick)
		}
	})
}
