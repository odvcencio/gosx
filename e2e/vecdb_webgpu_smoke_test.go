//go:build e2e && webgpusmoke

// Port of the retired e2e/vecdb_webgpu_smoke.mjs (playwright script) to a
// chromedp test. Builds the vecdb WebGPU smoke example for js/wasm, serves it
// with wasm_exec.js, and drives it in Chrome with --enable-unsafe-webgpu.
//
// Like the retired script, this is a MANUAL smoke, not part of the standard
// e2e gate — hence the extra webgpusmoke tag. Run it on demand with:
//
//	go test -tags 'e2e webgpusmoke' -run TestVecDBWebGPUSmoke ./e2e
//
// WebGPU is genuinely required by the page under test; on hosts where
// navigator.gpu is unavailable (most headless CI runners) the test SKIPS
// instead of failing. When WebGPU is present the smoke result must report
// passed. NOTE: as of this port, examples/vecdb-webgpu-smoke does not compile
// for js/wasm (vecdb prepared-query API drift) — that pre-existing breakage
// fails this test at the build step until the example is updated.
package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVecDBWebGPUSmoke(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	exampleDir := filepath.Join(root, "examples", "vecdb-webgpu-smoke")

	// Build the wasm binary.
	build := exec.Command("go", "build", "-o", filepath.Join(exampleDir, "main.wasm"), "./examples/vecdb-webgpu-smoke")
	build.Dir = root
	build.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build vecdb-webgpu-smoke wasm: %v\n%s", err, out)
	}

	// Copy wasm_exec.js from GOROOT.
	gorootCmd := exec.Command("go", "env", "GOROOT")
	gorootOut, err := gorootCmd.Output()
	if err != nil {
		t.Fatalf("go env GOROOT: %v", err)
	}
	goroot := strings.TrimSpace(string(gorootOut))
	copied := false
	for _, candidate := range []string{
		filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"),
		filepath.Join(goroot, "misc", "wasm", "wasm_exec.js"),
	} {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(exampleDir, "wasm_exec.js"), data, 0o644); err != nil {
			t.Fatalf("write wasm_exec.js: %v", err)
		}
		copied = true
		break
	}
	if !copied {
		t.Fatal("could not locate wasm_exec.js under GOROOT")
	}

	// Serve the example directory.
	server := &http.Server{Handler: http.FileServer(http.Dir(exampleDir))}
	listener, err := netListen(t)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	url := "http://" + listener.Addr().String() + "/"

	page := newBrowserPage(t, chrome, map[string]any{"enable-unsafe-webgpu": true}, 1280, 900, "", 90*time.Second)
	page.navigate(t, url)

	var hasGPU bool
	page.eval(t, `typeof navigator !== "undefined" && "gpu" in navigator`, &hasGPU)
	if !hasGPU {
		t.Skip("navigator.gpu unavailable in this environment; WebGPU smoke requires a WebGPU-capable Chrome")
	}

	page.waitFor(t, `!!(window.__vecdbWebGPUSmokeResult || window.__vecdbWebGPUSmokeError)`,
		30*time.Second, "vecdb smoke result")

	var result struct {
		Result *struct {
			Passed bool `json:"passed"`
		} `json:"result"`
		Error  any    `json:"error"`
		Status string `json:"status"`
	}
	page.eval(t, `({
    result: window.__vecdbWebGPUSmokeResult ?? null,
    error: window.__vecdbWebGPUSmokeError ?? null,
    status: document.getElementById("status")?.textContent ?? null,
  })`, &result)

	blob, _ := json.Marshal(result)
	t.Logf("vecdb webgpu smoke: %s\nConsole:\n%s", blob, page.Console())
	if result.Error != nil {
		errText, _ := json.Marshal(result.Error)
		// A present navigator.gpu can still lack a usable adapter (software
		// renderers). Treat adapter-acquisition failures as environment skips,
		// anything else as real failures.
		if strings.Contains(strings.ToLower(string(errText)), "adapter") {
			t.Skipf("WebGPU adapter unavailable: %s", errText)
		}
		t.Fatalf("vecdb webgpu smoke error: %s", errText)
	}
	if result.Result == nil || !result.Result.Passed {
		t.Fatalf("vecdb webgpu smoke did not pass: %s", blob)
	}
}
