package bootpage

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
)

func manifest() *buildmanifest.Manifest {
	m := &buildmanifest.Manifest{GoWASM: map[string]buildmanifest.HashedAsset{
		"game": {File: "game.0123456789abcdef.wasm", Hash: "0123456789abcdef", Size: 100, Integrity: "sha256-WASM"},
	}}
	m.Runtime.StandardGoWASMExec = buildmanifest.HashedAsset{File: "standard-go-wasm_exec.1234.js", Integrity: "sha256-EXEC"}
	return m
}

func TestPreloadLinks(t *testing.T) {
	html := gosx.RenderHTML(PreloadLinks(manifest(), "/gosx/assets", "game", PreloadOptions{Integrity: true}))
	for _, want := range []string{
		`href="/gosx/assets/runtime/standard-go-wasm_exec.1234.js"`, `as="script"`, `integrity="sha256-EXEC"`,
		`href="/gosx/assets/go-wasm/game.0123456789abcdef.wasm"`, `as="fetch"`, `type="application/wasm"`, `integrity="sha256-WASM"`,
		`rel="preload"`, `crossorigin="anonymous"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s in\n%s", want, html)
		}
	}
	plain := gosx.RenderHTML(PreloadLinks(manifest(), "/gosx/assets", "game", PreloadOptions{}))
	if strings.Contains(plain, "integrity") {
		t.Error("integrity must be opt-in")
	}
	if got := gosx.RenderHTML(PreloadLinks(manifest(), "/gosx/assets", "missing", PreloadOptions{})); strings.Contains(got, ".wasm") {
		t.Error("unknown module should emit no wasm link:", got)
	}
	if got := gosx.RenderHTML(PreloadLinks(nil, "/gosx/assets", "game", PreloadOptions{})); got != "" {
		t.Errorf("nil manifest: %q", got)
	}
}

func TestWatchdogScript(t *testing.T) {
	html := gosx.RenderHTML(Watchdog())
	if !strings.HasPrefix(html, "<script") || !strings.Contains(html, "data-boot-root") || !strings.Contains(html, "gosx-boot:") {
		t.Fatal(html[:min(len(html), 200)])
	}
	if strings.Contains(WatchdogSource(), "</script") {
		t.Fatal("script would close its own tag")
	}
}
