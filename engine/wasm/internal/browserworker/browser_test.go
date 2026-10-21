//go:build !js || !wasm

// Package browserworker holds the real-browser tests for Thread: worker
// engines. They build a Go WASM engine, serve a page made with the real
// island.Renderer and the real runtime bundles from client/js, and drive it in
// Chromium with Playwright (run.mjs). They run only with ENGINE_WORKER_BROWSER=1
// (make test-engine-worker-browser); ENGINE_WORKER_CHROME selects the browser.
package browserworker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/island"
)

type report map[string]any

type browserState struct {
	Reports []report         `json:"reports"`
	Events  []map[string]any `json:"events"`
	Issues  []map[string]any `json:"issues"`
	UA      string           `json:"ua"`
	Error   string           `json:"error"`
	Log     []string         `json:"log"`
	Extra   map[string]any   `json:"extra"`
}

func (s browserState) find(kind string) report {
	for _, r := range s.Reports {
		if r["kind"] == kind {
			return r
		}
	}
	return nil
}

var (
	buildOnce sync.Once
	buildDir  string
	buildErr  error
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// assets builds the fixture engine once and stages wasm_exec.js.
func assets(t *testing.T) string {
	t.Helper()
	if os.Getenv("ENGINE_WORKER_BROWSER") == "" {
		t.Skip("set ENGINE_WORKER_BROWSER=1 to run the real-browser worker tests")
	}
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "engine-worker-browser")
		if buildErr != nil {
			return
		}
		cmd := exec.Command("go", "build", "-o", filepath.Join(buildDir, "fixture.wasm"), "./engine/wasm/internal/browserworker/fixture")
		cmd.Dir = repoRoot(t)
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off", "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("building the fixture engine: %v\n%s", err, out)
			return
		}
		goroot, err := exec.Command("go", "env", "GOROOT").Output()
		if err != nil {
			buildErr = err
			return
		}
		src, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(goroot)), "lib", "wasm", "wasm_exec.js"))
		if err != nil {
			buildErr = err
			return
		}
		// The same isolation wrapper `gosx build` applies (cmd/gosx wrapStandardGoWASMExec).
		wrapped := "(function(global){\n\"use strict\";\nvar hadGo = Object.prototype.hasOwnProperty.call(global, \"Go\");\nvar previousGo = global.Go;\nvar standardGo;\ntry {\n" +
			string(src) +
			"\nstandardGo = global.Go;\n} finally {\nif (hadGo) global.Go = previousGo; else delete global.Go;\n}\nif (typeof standardGo !== \"function\") throw new Error(\"standard-Go wasm_exec.js did not publish Go\");\nObject.defineProperty(global, \"__gosx_standard_go_wasm_ctor\", {value: standardGo, writable: false, configurable: true});\n})(globalThis);\n"
		buildErr = os.WriteFile(filepath.Join(buildDir, "standard-go-wasm_exec.js"), []byte(wrapped), 0o644)
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return buildDir
}

// harness script: collects what the engines report and what the runtime raises.
const harness = `<script>
window.__reports = []; window.__events = [];
document.addEventListener("gosx:engine:report", function(e) { window.__reports.push(e.detail.detail); });
["thread-fallback", "worker-error", "worker-frame-error"].forEach(function(n) {
  document.addEventListener("gosx:engine:" + n, function(e) { window.__events.push({ name: n, detail: e.detail }); });
});
var q = new URLSearchParams(location.search);
if (q.get("noworker")) window.__gosx_engine_worker_disable = true;
</script>`

type pageOptions struct {
	engines []engine.Config
	body    string
}

func page(t *testing.T, opts pageOptions) string {
	t.Helper()
	r := island.NewRenderer("main")
	r.SetClientAssetPaths("/gosx/wasm_exec.js", "/gosx/patch.js", "/gosx/bootstrap.js")
	r.SetStandardGoWASMExecPath("/gosx/standard-go-wasm_exec.js")
	var body strings.Builder
	body.WriteString(opts.body)
	for _, cfg := range opts.engines {
		body.WriteString(gosx.RenderHTML(r.RenderEngine(cfg, gosx.Node{})))
	}
	return "<!doctype html><html><head><meta charset=\"utf-8\"><title>engine worker</title>" + harness +
		gosx.RenderHTML(r.PageHead()) + "</head><body>" + body.String() + "</body></html>"
}

func serve(t *testing.T, dir string, html func(r *http.Request) string) *httptest.Server {
	t.Helper()
	root := repoRoot(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/gosx/", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		path := filepath.Join(root, "client", "js", name)
		if name == "standard-go-wasm_exec.js" {
			path = filepath.Join(dir, name)
		}
		if _, err := os.Stat(path); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, path)
	})
	mux.HandleFunc("/engines/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		http.ServeFile(w, r, filepath.Join(dir, filepath.Base(r.URL.Path)))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html(r)))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// drive opens url in Chromium and returns the page's state once waitExpr is truthy.
func drive(t *testing.T, url, waitExpr string, timeout time.Duration) browserState {
	t.Helper()
	cmd := exec.Command("node", "run.mjs", url, waitExpr, fmt.Sprint(timeout.Milliseconds()))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if len(out) == 0 {
		t.Fatalf("browser run produced no output: %v %s", err, stderr.String())
	}
	var state browserState
	if jerr := json.Unmarshal(out, &state); jerr != nil {
		t.Fatalf("browser output %q: %v", out, jerr)
	}
	if state.Error != "" {
		t.Fatalf("browser run failed: %s\nlog: %v", state.Error, state.Log)
	}
	return state
}

func workerEngine(props string) engine.Config {
	return engine.Config{
		Name: "WorkerFixture", Kind: engine.KindWorker, Runtime: engine.RuntimeGoWASM, Thread: engine.ThreadWorker,
		WASMPath: "/engines/fixture.wasm", Props: json.RawMessage(props),
	}
}

func TestWorkerEngineBootsInARealWorker(t *testing.T) {
	dir := assets(t)
	ts := serve(t, dir, func(*http.Request) string {
		return page(t, pageOptions{engines: []engine.Config{workerEngine(`{"scenario":"boot"}`)}})
	})
	state := drive(t, ts.URL+"/", `(window.__reports||[]).some(r => r.kind === "mounted")`, 30*time.Second)
	mounted := state.find("mounted")
	if mounted["thread"] != "worker" || mounted["hasDocument"] != false || mounted["hasPort"] != true || mounted["fallback"] != "" {
		t.Fatalf("engine did not run in a worker: %v\nlog: %v", mounted, state.Log)
	}
	if len(state.Events) != 0 {
		t.Fatalf("unexpected fallback events: %v", state.Events)
	}
	t.Logf("worker engine mounted: %v (%s)", mounted, state.UA)
}

func TestWorkerEngineFallsBackToTheMainThreadAndReports(t *testing.T) {
	dir := assets(t)
	ts := serve(t, dir, func(*http.Request) string {
		return page(t, pageOptions{engines: []engine.Config{workerEngine(`{"scenario":"fallback"}`)}})
	})
	state := drive(t, ts.URL+"/?noworker=1", `(window.__reports||[]).some(r => r.kind === "mounted")`, 30*time.Second)
	mounted := state.find("mounted")
	if mounted["thread"] != "" || mounted["hasDocument"] != true || mounted["hasPort"] != false || mounted["fallback"] != "worker-disabled" {
		t.Fatalf("engine did not fall back: %v\nlog: %v", mounted, state.Log)
	}
	if len(state.Events) != 1 || state.Events[0]["name"] != "thread-fallback" {
		t.Fatalf("want one thread-fallback event, got %v", state.Events)
	}
	found := false
	for _, issue := range state.Issues {
		if issue["type"] == "worker" && issue["fallback"] == "main-thread" && strings.Contains(fmt.Sprint(issue["message"]), "disabled") {
			found = true
		}
	}
	if !found {
		t.Fatalf("fallback was not reported as a runtime issue: %v", state.Issues)
	}
}
