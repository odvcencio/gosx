//go:build !js || !wasm

package rooms

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed client.html
var clientHTML []byte

// AssetDir is where a production build puts the browser client, relative to
// the app root: main.wasm, main.wasm.gz and wasm_exec.js. scripts/deploy-gosx-docs.sh
// writes them there; nothing under it is committed.
const AssetDir = "runtime-assets/rooms"

// AssetHandler serves the rooms browser client under /demos/rooms/client/:
// the embedded page, wasm_exec.js and main.wasm. The wasm loads only when a
// visitor opens that page, so no other route pays for it.
//
// The files come from root/runtime-assets/rooms. When they are missing and the
// site is not running in production (GOSX_ENV), the handler builds them once
// with the local Go toolchain into a temporary directory, so `gosx dev` works
// without a setup step. In production a missing file is a 404.
func AssetHandler(root string) http.Handler {
	dir := filepath.Join(root, filepath.FromSlash(AssetDir))
	var (
		once     sync.Once
		devDir   string
		devError error
	)
	resolve := func() (string, error) {
		if _, err := os.Stat(filepath.Join(dir, "main.wasm")); err == nil {
			return dir, nil
		}
		if os.Getenv("GOSX_ENV") == "production" {
			return "", os.ErrNotExist
		}
		once.Do(func() { devDir, devError = buildDevClient(root) })
		return devDir, devError
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/demos/rooms/client")
		name = strings.TrimPrefix(name, "/")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		switch name {
		case "", "index.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(clientHTML))
		case "wasm_exec.js", "main.wasm":
			base, err := resolve()
			if err != nil {
				if os.IsNotExist(err) {
					http.NotFound(w, r)
					return
				}
				http.Error(w, "rooms client unavailable: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
			serveAsset(w, r, base, name)
		default:
			http.NotFound(w, r)
		}
	})
}

func serveAsset(w http.ResponseWriter, r *http.Request, dir, name string) {
	path := filepath.Join(dir, name)
	contentType := "application/javascript; charset=utf-8"
	if name == "main.wasm" {
		contentType = "application/wasm"
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			if _, err := os.Stat(path + ".gz"); err == nil {
				path += ".gz"
				w.Header().Set("Content-Encoding", "gzip")
			}
		}
		w.Header().Add("Vary", "Accept-Encoding")
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// buildDevClient compiles examples/rooms/client for js/wasm into a temporary
// directory and copies wasm_exec.js from GOROOT, for a local `gosx dev`.
func buildDevClient(root string) (string, error) {
	out := filepath.Join(os.TempDir(), "gosx-docs-rooms-client")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", err
	}
	build := exec.Command("go", "build", "-o", filepath.Join(out, "main.wasm"), "m31labs.dev/gosx/examples/rooms/client")
	build.Dir = root
	build.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
	if msg, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build examples/rooms/client: %v\n%s", err, msg)
	}
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return "", err
	}
	for _, rel := range []string{"lib/wasm/wasm_exec.js", "misc/wasm/wasm_exec.js"} {
		src := filepath.Join(strings.TrimSpace(string(goroot)), rel)
		if data, readErr := os.ReadFile(src); readErr == nil {
			if err := os.WriteFile(filepath.Join(out, "wasm_exec.js"), data, 0o644); err != nil {
				return "", err
			}
			// A compressed copy keeps a local page close to production size.
			wasm, _ := os.ReadFile(filepath.Join(out, "main.wasm"))
			var gz bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&gz, gzip.BestSpeed)
			_, _ = zw.Write(wasm)
			_ = zw.Close()
			_ = os.WriteFile(filepath.Join(out, "main.wasm.gz"), gz.Bytes(), 0o644)
			return out, nil
		}
	}
	return "", fmt.Errorf("wasm_exec.js not found under GOROOT %s", strings.TrimSpace(string(goroot)))
}
