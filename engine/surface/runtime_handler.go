//go:build !js

package surface

import (
	_ "embed"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// bootstrapJS is the hand-authored JS bootstrap served at /gosx/surface/runtime.js.
// It walks the DOM for data-gosx-engine-component placeholders, loads each
// component's WASM, instantiates it, and bridges DOM events into the canonical
// __gosx_surface_event payload shape (see runtime/runtime.go for the kind table).
//
//go:embed runtime/bootstrap.js
var bootstrapJS []byte

// wasmExecOnce guards the lazy resolution of wasm_exec.js.
var (
	wasmExecOnce  sync.Once
	wasmExecBytes []byte
	wasmExecErr   string
)

// resolveWasmExecJS returns the bytes of wasm_exec.js resolved from the host's
// Go SDK (preferred) or TinyGo install (fallback). Returns (nil, reason) when
// neither source is available. Result is cached for the process lifetime.
func resolveWasmExecJS() ([]byte, string) {
	wasmExecOnce.Do(func() {
		wasmExecBytes, wasmExecErr = loadWasmExecFromGoroot(runtime.GOROOT())
		if len(wasmExecBytes) > 0 {
			return
		}
		// Fallback: ask tinygo for its TINYGOROOT.
		wasmExecBytes, wasmExecErr = loadWasmExecFromTinyGo()
	})
	return wasmExecBytes, wasmExecErr
}

// loadWasmExecFromGoroot tries to read wasm_exec.js from a Go SDK rooted at
// goroot. Returns (bytes, "") on success, (nil, reason) on failure.
func loadWasmExecFromGoroot(goroot string) ([]byte, string) {
	if goroot == "" {
		return nil, "GOROOT empty"
	}
	// Go 1.21+ moved wasm_exec.js to lib/wasm/wasm_exec.js; the older
	// misc/wasm/wasm_exec.js path remained as a symlink until Go 1.24
	// and was removed in Go 1.26. Probe both, newer first.
	candidates := []string{
		filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"),
		filepath.Join(goroot, "misc", "wasm", "wasm_exec.js"),
	}
	for _, p := range candidates {
		if data, err := os.ReadFile(p); err == nil {
			return data, ""
		}
	}
	return nil, "wasm_exec.js not found under GOROOT " + goroot
}

// loadWasmExecFromTinyGo shells out to `tinygo env TINYGOROOT` and reads
// targets/wasm_exec.js from the reported root.
func loadWasmExecFromTinyGo() ([]byte, string) {
	tinygoPath, err := exec.LookPath("tinygo")
	if err != nil {
		return nil, "tinygo not on PATH"
	}
	out, err := exec.Command(tinygoPath, "env", "TINYGOROOT").Output()
	if err != nil {
		return nil, "tinygo env TINYGOROOT failed: " + err.Error()
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return nil, "tinygo env TINYGOROOT empty"
	}
	candidates := []string{
		filepath.Join(root, "targets", "wasm_exec.js"),
		filepath.Join(root, "targets", "wasm", "wasm_exec.js"),
	}
	for _, p := range candidates {
		if data, err := os.ReadFile(p); err == nil {
			return data, ""
		}
	}
	return nil, "wasm_exec.js not found under TINYGOROOT " + root
}

// RuntimeHandler returns an http.Handler that serves the engine-surface JS
// bootstrap and the toolchain wasm_exec.js shim. Mount alongside Handler():
//
//	mux.Handle("/gosx/engines/", surface.Handler())
//	mux.Handle("/gosx/surface/", surface.RuntimeHandler())
//
// Routes served:
//   - GET /gosx/surface/runtime.js   → bootstrap (200, application/javascript)
//   - GET /gosx/surface/wasm_exec.js → Go/TinyGo toolchain shim (200 if resolvable, 503 otherwise)
//   - any other path                 → 404
func RuntimeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gosx/surface/runtime.js":
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Header().Set("Cache-Control", "public, max-age=300")
			_, _ = w.Write(bootstrapJS)
		case "/gosx/surface/wasm_exec.js":
			data, reason := resolveWasmExecJS()
			if len(data) == 0 {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("gosx/surface: cannot resolve wasm_exec.js: " + reason + "\n" +
					"hint: ensure Go is installed (GOROOT) or TinyGo is on PATH"))
				return
			}
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Header().Set("Cache-Control", "public, max-age=300")
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	})
}
