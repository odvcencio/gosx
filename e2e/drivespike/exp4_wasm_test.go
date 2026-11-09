//go:build e2e && drivespike

package drivespike

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// buildClockWasm builds testdata/wasmclock with the standard Go toolchain or
// TinyGo and returns the module and its wasm_exec.js.
func buildClockWasm(t *testing.T, toolchain string) (wasm, execJS string, err error) {
	root := repoRoot(t)
	dir := t.TempDir()
	wasm = filepath.Join(dir, "clock.wasm")
	pkg := "./e2e/drivespike/testdata/wasmclock"
	switch toolchain {
	case "go":
		cmd := exec.Command("go", "build", "-o", wasm, pkg)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", "", fmt.Errorf("go build: %v\n%s", err, out)
		}
		out, err := exec.Command("go", "env", "GOROOT").Output()
		if err != nil {
			return "", "", err
		}
		goroot := strings.TrimSpace(string(out))
		for _, rel := range []string{"lib/wasm/wasm_exec.js", "misc/wasm/wasm_exec.js"} {
			if _, err := os.Stat(filepath.Join(goroot, rel)); err == nil {
				return wasm, filepath.Join(goroot, rel), nil
			}
		}
		return "", "", fmt.Errorf("no wasm_exec.js under %s", goroot)
	case "tinygo":
		tinygo, err := exec.LookPath("tinygo")
		if err != nil {
			return "", "", errSkip
		}
		args := []string{"build", "-target", "wasm", "-o", wasm, pkg}
		name := tinygo
		if runtime.GOOS == "linux" {
			// chi-1 shares one TinyGo lock between sessions.
			if flock, err := exec.LookPath("flock"); err == nil {
				args = append([]string{"/home/draco/work/.runtime/locks/tinygo.lock", tinygo}, args...)
				name = flock
			}
		}
		cmd := exec.Command(name, args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOWORK=off")
		if g := os.Getenv("GOSX_TINYGO_GOROOT"); g != "" {
			cmd.Env = append(cmd.Env, "GOROOT="+g)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", "", fmt.Errorf("tinygo build: %v\n%s", err, out)
		}
		out, err := exec.Command(tinygo, "env", "TINYGOROOT").Output()
		if err != nil {
			return "", "", err
		}
		return wasm, filepath.Join(strings.TrimSpace(string(out)), "targets", "wasm_exec.js"), nil
	}
	return "", "", fmt.Errorf("unknown toolchain %q", toolchain)
}

const wasmPage = `<!doctype html><html><head><meta charset="utf-8"><script src="/wasm_exec.js"></script><script>
window.glog = [];
window.goLog = function (n, v) { glog.push({ n: n, val: v, at: window.__drive ? __drive.now() : null, perf: performance.now(), date: Date.now() }); };
var go = new Go();
WebAssembly.instantiateStreaming(fetch("/clock.wasm"), go.importObject).then(function (r) { window.started = true; return go.run(r.instance); })
  .then(function () { window.exited = true; }).catch(function (e) { window.wasmError = String(e && e.stack || e); });
</script></head><body>wasm clock</body></html>`

type goLog struct {
	N    string
	Val  float64 // the value Go reported
	At   float64 // the probe's virtual clock when Go reported it
	Perf float64
	Date float64
}

// Experiment 4: Go code compiled to WebAssembly (standard Go and TinyGo).
func TestExp4Wasm(t *testing.T) {
	b := startBrowser(t)
	for _, toolchain := range []string{"go", "tinygo"} {
		t.Run(toolchain, func(t *testing.T) {
			res := newResults(t, "4/"+toolchain)
			defer res.summary()
			wasm, execJS, err := buildClockWasm(t, toolchain)
			if err == errSkip {
				t.Logf("RESULT exp=4/%s browser=%s check=%q NOTRUN no tinygo on PATH", toolchain, browserLabel(), "build")
				t.Skip("no tinygo")
			}
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/wasm_exec.js", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, execJS) })
			mux.HandleFunc("/clock.wasm", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/wasm")
				http.ServeFile(w, r, wasm)
			})
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte(wasmPage))
			})
			url := serve(t, mux)
			tb := b.newTab(t, 2*time.Minute)
			wall := 1.7e12
			tb.addProbe(map[string]any{"wall": wall})
			tb.navigate(url + "/")
			if !tb.waitFor(`glog.some(e => e.n === "start.unixms") || !!window.wasmError`, 60*time.Second) {
				t.Fatalf("wasm never started\n%s", tb.Console())
			}
			var werr string
			tb.eval(`window.wasmError || ""`, &werr)
			if werr != "" {
				t.Fatalf("wasm: %s", werr)
			}
			read := func() []goLog {
				var l []goLog
				tb.eval(`glog`, &l)
				return l
			}
			find := func(l []goLog, name string) []goLog {
				var out []goLog
				for _, e := range l {
					if e.N == name {
						out = append(out, e)
					}
				}
				return out
			}
			l := read()
			start := find(l, "start.unixms")
			res.check("time.Now() reads virtual wall time", len(start) == 1 && near(start[0].Val, wall, 1), "start=%s (wall=%v)", jsonString(start), wall)
			time.Sleep(700 * time.Millisecond)
			l = read()
			res.check("time.Sleep(200ms) does not return in 700 ms of real time without a step", len(find(l, "sleep.elapsed")) == 0, "log=%s", jsonString(l))
			r := tb.step(199)
			l = read()
			res.check("time.Sleep has not returned at 199 ms", len(find(l, "sleep.elapsed")) == 0, "log=%s step=%s", jsonString(l), jsonString(r))
			r = tb.step(1)
			l = read()
			sl := find(l, "sleep.elapsed")
			res.check("time.Sleep returns once the clock reaches 200 ms", len(sl) == 1, "log=%s step=%s", jsonString(l), jsonString(r))
			if len(sl) == 1 {
				res.check("time.Since after the sleep is 200 ms of virtual time", near(sl[0].Val, 200, 0.01) && near(sl[0].At, 200, 1e-6), "elapsed=%v at V=%v", sl[0].Val, sl[0].At)
			}
			if len(sl) == 0 {
				// The sleep may land a little after 200 ms; find where.
				for i := 0; i < 20 && len(sl) == 0; i++ {
					tb.step(1)
					sl = find(read(), "sleep.elapsed")
				}
				t.Logf("RESULT exp=4/%s browser=%s check=%q INFO sleep returned at V=%s", toolchain, browserLabel(), "late sleep", jsonString(sl))
			}
			var ticks []goLog
			for i := 0; i < 3; i++ {
				tb.step(100)
				ticks = find(read(), "tick.elapsed")
			}
			ok := len(ticks) == 3
			for i, tk := range ticks {
				if !near(tk.Val, float64(300+100*i), 0.01) || !near(tk.At, float64(300+100*i), 1e-6) {
					ok = false
				}
			}
			res.check("time.Ticker(100ms) ticks at 300, 400, 500 ms of virtual time, one per step", ok, "ticks=%s", jsonString(ticks))
			su := find(read(), "sleep.unixms")
			res.check("time.Now() after the sleep reads wall+200", len(su) == 1 && near(su[0].Val, wall+200, 1), "sleep.unixms=%s", jsonString(su))
			var exited bool
			tb.step(16)
			tb.eval(`!!window.exited`, &exited)
			t.Logf("exited=%v console:\n%s", exited, tb.Console())
		})
	}
}
