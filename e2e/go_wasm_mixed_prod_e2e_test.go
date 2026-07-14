//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

type mixedBuildManifest struct {
	Runtime struct {
		WASMExec struct {
			File string `json:"file"`
		} `json:"wasmExec"`
		StandardGoWASMExec struct {
			File string `json:"file"`
		} `json:"standardGoWasmExec"`
		BootstrapRuntime struct {
			File string `json:"file"`
		} `json:"bootstrapRuntime"`
		BootstrapFeatureIslands struct {
			File string `json:"file"`
		} `json:"bootstrapFeatureIslands"`
		BootstrapFeatureEngines struct {
			File string `json:"file"`
		} `json:"bootstrapFeatureEngines"`
	} `json:"runtime"`
	Islands []struct {
		File string `json:"file"`
	} `json:"islands"`
}

func TestProductionBuildRunsMixedTinyGoAndStandardGoWASM(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	fixture := filepath.Join(t.TempDir(), "mixed")
	copyFixtureTree(t, filepath.Join(root, "e2e", "testdata", "go-wasm-mixed"), fixture)

	module := fmt.Sprintf("module example.com/gosx-mixed\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\n\nreplace m31labs.dev/gosx => %s\n", filepath.ToSlash(root))
	if err := os.WriteFile(filepath.Join(fixture, "go.mod"), []byte(module), 0644); err != nil {
		t.Fatal(err)
	}
	engineTmp := filepath.Join(t.TempDir(), "fixture.wasm")
	buildEngine := exec.Command("go", "build", "-o", engineTmp, "./engine/wasm/testdata/fixture")
	buildEngine.Dir = root
	buildEngine.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
	if output, err := buildEngine.CombinedOutput(); err != nil {
		t.Fatalf("build standard-Go engine: %v\n%s", err, output)
	}
	engineData, err := os.ReadFile(engineTmp)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(engineData)
	engineName := "fixture." + hex.EncodeToString(digest[:4]) + ".wasm"
	engineDir := filepath.Join(fixture, "public", "engines")
	if err := os.MkdirAll(engineDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(engineDir, engineName), engineData, 0644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(fixture, "main.go")
	mainData, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	mainData = []byte(strings.ReplaceAll(string(mainData), "__ENGINE_WASM_PATH__", "/engines/"+engineName))
	if err := os.WriteFile(mainPath, mainData, 0644); err != nil {
		t.Fatal(err)
	}

	build := exec.Command("go", "run", "./cmd/gosx", "build", "--prod", fixture)
	build.Dir = root
	build.Env = append(os.Environ(), "GOWORK=off")
	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("gosx build --prod: %v\n%s", err, output)
	}
	t.Logf("gosx build --prod:\n%s", output)

	dist := filepath.Join(fixture, "dist")
	var manifest mixedBuildManifest
	manifestData, err := os.ReadFile(filepath.Join(dist, "build.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	for label, file := range map[string]string{
		"TinyGo wasm_exec":      manifest.Runtime.WASMExec.File,
		"standard-Go wasm_exec": manifest.Runtime.StandardGoWASMExec.File,
		"runtime bootstrap":     manifest.Runtime.BootstrapRuntime.File,
		"islands feature":       manifest.Runtime.BootstrapFeatureIslands.File,
		"engines feature":       manifest.Runtime.BootstrapFeatureEngines.File,
	} {
		if file == "" || !strings.Contains(file, ".") {
			t.Fatalf("%s was not emitted as a hashed asset: %q", label, file)
		}
		if _, err := os.Stat(filepath.Join(dist, "assets", "runtime", file)); err != nil {
			t.Fatalf("missing emitted %s asset %q: %v", label, file, err)
		}
	}
	if len(manifest.Islands) != 1 || manifest.Islands[0].File == "" {
		t.Fatalf("normal island was not emitted: %#v", manifest.Islands)
	}
	port := freeE2EPort(t)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	logs := startBuiltFixture(t, dist, port)
	if err := waitForHealthy(baseURL+"/", 45*time.Second); err != nil {
		t.Fatalf("%v\n%s", err, logs.String())
	}
	pageResp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	pageHTML, readErr := io.ReadAll(pageResp.Body)
	_ = pageResp.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	pageText := string(pageHTML)
	for _, file := range []string{
		manifest.Runtime.WASMExec.File,
		manifest.Runtime.StandardGoWASMExec.File,
		manifest.Runtime.BootstrapRuntime.File,
		manifest.Runtime.BootstrapFeatureIslands.File,
		manifest.Runtime.BootstrapFeatureEngines.File,
		manifest.Islands[0].File,
	} {
		if !strings.Contains(pageText, file) {
			t.Fatalf("production page omitted emitted asset %q", file)
		}
	}
	for _, compat := range []string{"/gosx/wasm_exec.js", "/gosx/standard-go-wasm_exec.js", "/gosx/bootstrap-runtime.js"} {
		if strings.Contains(pageText, compat) {
			t.Fatalf("production page retained compatibility runtime URL %q", compat)
		}
	}
	resp, err := http.Get(baseURL + "/engines/" + engineName)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/wasm") {
		t.Fatalf("engine MIME = %q", got)
	}

	initScript := `(() => {
window.__gosx_mixed_events = [];
document.addEventListener("gosx:engine:mounted", (event) => window.__gosx_mixed_events.push(event.detail));
const original = WebAssembly.instantiateStreaming;
window.__gosx_streaming_attempts = 0;
WebAssembly.instantiateStreaming = function(...args) {
  window.__gosx_streaming_attempts += 1;
  return original.apply(this, args);
};
})()`
	page := newBrowserPage(t, chrome, nil, 1280, 900, initScript, 90*time.Second)
	if status := page.navigate(t, baseURL+"/"); status != http.StatusOK {
		t.Fatalf("fixture status %d\n%s", status, logs.String())
	}
	waitMixedFixtureReady(t, page)
	var state struct {
		Scripts              []string `json:"scripts"`
		Sources              []string `json:"sources"`
		ConstructorsIsolated bool     `json:"constructorsIsolated"`
		StreamingAttempts    int      `json:"streamingAttempts"`
		Emitted              bool     `json:"emitted"`
		EmitErrorSafe        string   `json:"emitErrorSafe"`
	}
	page.eval(t, `(() => {
const scripts = [...document.querySelectorAll("script[data-gosx-script]")];
const mount = document.querySelector("[data-gosx-engine=GoWASMFixture]");
return {
  scripts: scripts.map((script) => script.dataset.gosxScript),
  sources: scripts.map((script) => script.getAttribute("src") || ""),
  constructorsIsolated: typeof window.Go === "function" && typeof window.__gosx_standard_go_wasm_ctor === "function" && window.Go !== window.__gosx_standard_go_wasm_ctor,
  streamingAttempts: window.__gosx_streaming_attempts || 0,
  emitted: (window.__gosx_mixed_events || []).some((event) => event && event.detail && event.detail.payload && event.detail.payload.mounted === true),
  emitErrorSafe: mount && mount.dataset.emitErrorSafe || "",
};
})()`, &state)
	wantOrder := []string{"standard-go-wasm-exec", "wasm-exec", "bootstrap"}
	positions := make([]int, len(wantOrder))
	for i, role := range wantOrder {
		positions[i] = indexOfString(state.Scripts, role)
	}
	if positions[0] < 0 || positions[1] <= positions[0] || positions[2] <= positions[1] {
		t.Fatalf("runtime script order = %#v", state.Scripts)
	}
	for _, source := range state.Sources {
		if strings.HasPrefix(source, "/gosx/") && !strings.Contains(source, "/gosx/assets/runtime/") {
			t.Fatalf("browser consumed compatibility runtime URL %q", source)
		}
	}
	if !state.ConstructorsIsolated || state.StreamingAttempts < 2 || !state.Emitted || state.EmitErrorSafe != "true" {
		t.Fatalf("mixed runtime state = %#v", state)
	}
	page.eval(t, `document.querySelector(".counter button:last-child").click()`, nil)
	if err := chromedp.Run(page.ctx, chromedp.Poll(
		`document.querySelector(".counter") && document.querySelector(".counter").textContent.includes("1")`,
		nil,
		chromedp.WithPollingTimeout(10*time.Second),
	)); err != nil {
		t.Fatalf("wait for TinyGo island increment: %v\nconsole:\n%s\npage errors: %v", err, page.Console(), page.PageErrors())
	}
	var count string
	page.eval(t, `document.querySelector(".counter").textContent`, &count)
	if !strings.Contains(strings.TrimSpace(count), "1") {
		t.Fatalf("TinyGo-backed island did not hydrate: count=%q", count)
	}
	page.eval(t, `window.__gosx_dispose_engine("gosx-engine-0")`, nil)
	var disposed struct {
		Count    string `json:"count"`
		Fallback bool   `json:"fallback"`
	}
	page.eval(t, `(() => { const mount = document.querySelector("[data-gosx-engine=GoWASMFixture]"); return {count: mount.dataset.disposeCount || "", fallback: !!mount.querySelector("[data-server-fallback=true]")}; })()`, &disposed)
	if disposed.Count != "1" || !disposed.Fallback {
		t.Fatalf("Go-WASM disposal state = %#v", disposed)
	}

	fallbackInit := `(() => {
const original = WebAssembly.instantiateStreaming;
window.__gosx_forced_streaming_rejections = 0;
WebAssembly.instantiateStreaming = function(response, imports) {
  if (response && response.url && response.url.includes("/engines/")) {
    window.__gosx_forced_streaming_rejections += 1;
    return Promise.reject(new Error("forced MIME fallback"));
  }
  return original.call(this, response, imports);
};
})()`
	fallbackPage := newBrowserPage(t, chrome, nil, 1280, 900, fallbackInit, 90*time.Second)
	fallbackPage.navigate(t, baseURL+"/")
	waitMixedFixtureReady(t, fallbackPage)
	var rejections int
	fallbackPage.eval(t, `window.__gosx_forced_streaming_rejections`, &rejections)
	if rejections != 1 {
		t.Fatalf("expected one forced engine streaming rejection, got %d", rejections)
	}

	managedPage := newBrowserPage(t, chrome, nil, 1280, 900, "", 90*time.Second)
	if status := managedPage.navigate(t, baseURL+"/blank"); status != http.StatusOK {
		t.Fatalf("managed-navigation fixture status %d\n%s", status, logs.String())
	}
	managedPage.eval(t, `window.__gosx_page_nav.navigate("/managed")`, nil)
	waitMixedFixtureReady(t, managedPage)
	var managedState struct {
		CurrentPath              string `json:"currentPath"`
		StandardGoDOMLoaded      bool   `json:"standardGoDOMLoaded"`
		ConstructorsIsolated     bool   `json:"constructorsIsolated"`
		StandardGoScriptLoadMode string `json:"standardGoScriptLoadMode"`
	}
	managedPage.eval(t, `(() => {
const standardGo = document.querySelector('script[data-gosx-script="standard-go-wasm-exec"][data-gosx-script-loaded="true"]');
return {
  currentPath: window.__gosx_page_nav.getState().currentPath,
  standardGoDOMLoaded: !!standardGo,
  constructorsIsolated: typeof window.Go === "function" && typeof window.__gosx_standard_go_wasm_ctor === "function" && window.Go !== window.__gosx_standard_go_wasm_ctor,
  standardGoScriptLoadMode: standardGo && standardGo.getAttribute("data-gosx-script-load") || "",
};
})()`, &managedState)
	if managedState.CurrentPath != "/managed" || !managedState.StandardGoDOMLoaded || !managedState.ConstructorsIsolated || managedState.StandardGoScriptLoadMode != "dom" {
		t.Fatalf("managed-navigation mixed runtime state = %#v\nconsole:\n%s\npage errors: %v", managedState, managedPage.Console(), managedPage.PageErrors())
	}
}

func waitMixedFixtureReady(t *testing.T, page *browserPage) {
	t.Helper()
	if err := chromedp.Run(page.ctx,
		chromedp.WaitVisible(`[data-gosx-engine="GoWASMFixture"][data-mounted="true"]`, chromedp.ByQuery),
		chromedp.Poll(
			`window.__gosx && window.__gosx.islands && window.__gosx.islands.size === 1`,
			nil,
			chromedp.WithPollingTimeout(30*time.Second),
		),
		chromedp.WaitVisible(`.counter`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("wait for mixed fixture: %v\nconsole:\n%s\npage errors: %v", err, page.Console(), page.PageErrors())
	}
}

func startBuiltFixture(t *testing.T, dist string, port int) *logBuffer {
	t.Helper()
	logs := &logBuffer{}
	cmd := exec.Command(filepath.Join(dist, "run.sh"))
	cmd.Dir = dist
	cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", port))
	cmd.Stdout = logs
	cmd.Stderr = logs
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
	})
	return logs
}

func copyFixtureTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
	if err != nil {
		t.Fatal(err)
	}
}

func indexOfString(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return -1
}
