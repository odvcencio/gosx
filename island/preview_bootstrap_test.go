package island

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
)

// preview-bootstrap tests cover the island.EnablePreviewBootstrap() flag.
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

func TestPreviewSelectiveBootstrapInitializesRuntime(t *testing.T) {
	EnablePreviewBootstrap()
	t.Cleanup(ResetPreviewBootstrap)
	r := NewRenderer("main")
	r.SetRuntime("/runtime.wasm", "", 123)
	r.SetBootstrapRuntimePath("/bootstrap-runtime.js")
	r.SetBootstrapFeaturePaths("/bootstrap-feature-islands.js", "", "")

	if got := r.Summary(); got.BootstrapPath != "/bootstrap-runtime.js" || got.BootstrapFeatureIslandsPath != "/bootstrap-feature-islands.js" || !got.Manifest {
		t.Fatalf("preview runtime plan = %+v", got)
	}
	manifestJSON, err := r.ManifestJSON()
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Preview bool `json:"preview"`
		Runtime struct {
			Path string `json:"path"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		t.Fatal(err)
	}
	if !manifest.Preview || manifest.Runtime.Path != "/runtime.wasm" {
		t.Fatalf("preview manifest = %s", manifestJSON)
	}
	head := gosx.RenderHTML(r.PreloadHints()) + gosx.RenderHTML(r.PageHead())
	if !strings.Contains(head, `href="/bootstrap-feature-islands.js" as="script"`) {
		t.Fatalf("preview must preload its islands feature: %s", head)
	}
	if strings.Contains(head, `data-gosx-script="patch"`) || strings.Contains(head, "bootstrap-feature-engines.js") {
		t.Fatalf("preview emitted unrelated features: %s", head)
	}
	assertPreviewRelayBeforeBootstrap(t, head)
}

func TestPreviewSelectiveBootstrapCompatibilityFallback(t *testing.T) {
	EnablePreviewBootstrap()
	t.Cleanup(ResetPreviewBootstrap)
	for _, missing := range []string{"runtime", "islands", "both", "manifest"} {
		t.Run(missing, func(t *testing.T) {
			r := NewRenderer("main")
			if missing == "manifest" {
				if err := r.ApplyBuildManifest(&buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
					Bootstrap: buildmanifest.HashedAsset{File: "bootstrap.js"},
				}}, "/gosx/assets"); err != nil {
					t.Fatal(err)
				}
			}
			r.SetRuntime("/runtime.wasm", "", 123)
			if missing == "runtime" || missing == "both" {
				r.bootstrapRuntimePath = ""
			}
			if missing == "islands" || missing == "both" {
				r.bootstrapFeatureIslandsPath = ""
			}
			if r.clientRuntimePlan().Selective || r.Summary().BootstrapPath != r.bootstrapPath || r.Summary().BootstrapFeatureIslandsPath != "" {
				t.Fatalf("incomplete selective assets must use compatibility bootstrap: %+v", r.Summary())
			}
			head := gosx.RenderHTML(r.PreloadHints()) + gosx.RenderHTML(r.PageHead())
			if !strings.Contains(head, `"preview":true`) || !strings.Contains(head, `"path":"/runtime.wasm"`) {
				t.Fatalf("fallback must still initialize the preview bridge: %s", head)
			}
			assertPreviewRelayBeforeBootstrap(t, head)
		})
	}
}

func assertPreviewRelayBeforeBootstrap(t *testing.T, head string) {
	t.Helper()
	relay := strings.Index(head, `data-gosx-script="relay"`)
	loader := strings.Index(head, `data-gosx-script="wasm-exec"`)
	bootstrap := strings.Index(head, `data-gosx-script="bootstrap"`)
	if relay < 0 || loader < relay || bootstrap < loader {
		t.Fatalf("preview scripts must run relay, WASM loader, then bootstrap: %s", head)
	}
}
