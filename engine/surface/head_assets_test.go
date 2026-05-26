package surface

import (
	"strings"
	"testing"

	gosx "m31labs.dev/gosx"
)

// TestHeadAssetsEmitsBothScripts asserts the rendered HTML contains both
// wasm_exec.js and runtime.js script tags, in that order, both deferred.
func TestHeadAssetsEmitsBothScripts(t *testing.T) {
	html := gosx.RenderHTML(HeadAssets())

	wasmIdx := strings.Index(html, `src="/gosx/surface/wasm_exec.js"`)
	rtIdx := strings.Index(html, `src="/gosx/surface/runtime.js"`)
	if wasmIdx < 0 {
		t.Errorf("HeadAssets missing wasm_exec.js script tag: %s", html)
	}
	if rtIdx < 0 {
		t.Errorf("HeadAssets missing runtime.js script tag: %s", html)
	}
	if wasmIdx >= 0 && rtIdx >= 0 && wasmIdx > rtIdx {
		t.Errorf("wasm_exec.js must precede runtime.js so Go is defined when bootstrap runs; got order wasm@%d > rt@%d\n%s", wasmIdx, rtIdx, html)
	}

	// Each script must be deferred (don't block parsing).
	deferCount := strings.Count(html, "defer")
	if deferCount < 2 {
		t.Errorf("expected both scripts to be deferred; defer count = %d\n%s", deferCount, html)
	}
}
