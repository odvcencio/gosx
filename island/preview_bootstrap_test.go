package island

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// preview-bootstrap tests cover the new island.EnablePreviewBootstrap() flag
// introduced by ADR 0009 + plan section C of
// plans/2026-05-26-iframe-cross-frame-signal-transport.md.
//
// EnablePreviewBootstrap() is a process-level idempotent flag. When set, any
// Renderer constructed AFTER the call (or already constructed) emits a
// minimal islands-style bootstrap regardless of whether the page registered
// any islands — so the storefront iframe gets a WASM Bridge that can receive
// cross-frame $preview.* signal writes. Tests reset the flag via
// ResetPreviewBootstrap() so they remain isolated.

// C.1: EnablePreviewBootstrap is a no-op without effect on plain renderers
// when not set.
func TestPreviewBootstrapDisabledByDefault(t *testing.T) {
	ResetPreviewBootstrap()
	r := NewRenderer("main")
	html := gosx.RenderHTML(r.PageHead())
	if html != "" {
		t.Fatalf("default Renderer with no islands should emit empty head; got %q", html)
	}
}

// C.2: When enabled, BootstrapScript emits a non-empty bootstrap independent
// of registered islands.
func TestEnablePreviewBootstrapEmitsBootstrapWithNoIslands(t *testing.T) {
	ResetPreviewBootstrap()
	defer ResetPreviewBootstrap()
	EnablePreviewBootstrap()

	r := NewRenderer("main")
	head := gosx.RenderHTML(r.PageHead())

	if head == "" {
		t.Fatal("EnablePreviewBootstrap should cause PageHead to emit script tags")
	}
	if !strings.Contains(head, `data-gosx-bootstrap-mode="preview"`) {
		t.Fatalf("expected preview bootstrap mode, got %q", head)
	}
	if !strings.Contains(head, "/gosx/relay.js") {
		t.Fatalf("expected relay.js script tag, got %q", head)
	}
	if !strings.Contains(head, "data-gosx-script=\"relay\"") {
		t.Fatalf("expected relay script marker, got %q", head)
	}
}

// C.3: Preview bootstrap loads the wasm_exec + tiny runtime so the iframe has
// a Bridge to receive into.
func TestEnablePreviewBootstrapLoadsWASMRuntime(t *testing.T) {
	ResetPreviewBootstrap()
	defer ResetPreviewBootstrap()
	EnablePreviewBootstrap()

	r := NewRenderer("main")
	head := gosx.RenderHTML(r.PageHead())

	if !strings.Contains(head, "wasm_exec.js") {
		t.Fatalf("preview bootstrap should load wasm_exec.js, got %q", head)
	}
}

// C.4: Idempotent — calling EnablePreviewBootstrap twice has the same effect.
func TestEnablePreviewBootstrapIsIdempotent(t *testing.T) {
	ResetPreviewBootstrap()
	defer ResetPreviewBootstrap()
	EnablePreviewBootstrap()
	EnablePreviewBootstrap()

	r := NewRenderer("main")
	head1 := gosx.RenderHTML(r.PageHead())

	EnablePreviewBootstrap()
	r2 := NewRenderer("main")
	head2 := gosx.RenderHTML(r2.PageHead())
	if head1 != head2 {
		t.Fatalf("expected idempotent emission; head1=%q head2=%q", head1, head2)
	}
}

// C.5: Pages that register actual islands AND have preview-bootstrap
// enabled still work — the preview flag does not block normal hydration.
func TestEnablePreviewBootstrapDoesNotBlockIslands(t *testing.T) {
	ResetPreviewBootstrap()
	defer ResetPreviewBootstrap()
	EnablePreviewBootstrap()

	r := NewRenderer("main")
	r.SetBundle("main", "/gosx/runtime.wasm")
	r.RenderIsland("Counter", nil, gosx.Text("0"))

	head := gosx.RenderHTML(r.PageHead())
	if !strings.Contains(head, "gosx-manifest") {
		t.Fatalf("islands present should still emit manifest; got %q", head)
	}
	if !strings.Contains(head, "/gosx/relay.js") {
		t.Fatalf("preview bootstrap should still emit relay.js when islands also present; got %q", head)
	}
}
