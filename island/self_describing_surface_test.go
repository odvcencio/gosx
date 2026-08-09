package island

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/island/program"
)

func TestSelfDescribingSurfaceEnablesRuntimeWithoutManifestEngine(t *testing.T) {
	r := NewRenderer("surface-test")
	node := r.Surface(gosx.CanvasBoard(gosx.CanvasBoardProps{ID: "board"}))

	if got := gosx.RenderHTML(node); !strings.Contains(got, `data-gosx-surface-kind="canvas2d"`) {
		t.Fatalf("surface node changed unexpectedly: %s", got)
	}
	if len(r.Manifest().Engines) != 0 {
		t.Fatalf("self-describing surface registered manifest engines: %#v", r.Manifest().Engines)
	}

	summary := r.Summary()
	if !summary.Bootstrap || summary.BootstrapMode != "full" {
		t.Fatalf("surface bootstrap summary = %+v", summary)
	}
	if summary.RuntimePath == "" || summary.WASMExecPath == "" || summary.BootstrapFeatureEnginesPath == "" {
		t.Fatalf("surface runtime paths missing: %+v", summary)
	}
	if summary.Engines != 0 || summary.SelfDescribingSurfaces != 1 {
		t.Fatalf("surface counts = engines %d self-describing %d", summary.Engines, summary.SelfDescribingSurfaces)
	}
	if manifest := r.clientManifest(); manifest == nil || len(manifest.Engines) != 0 {
		t.Fatalf("client manifest engines = %#v", manifest)
	}
	manifest := r.clientManifest()
	if manifest == nil || len(manifest.SelfDescribingSurfaces) != 1 {
		t.Fatalf("client manifest surfaces = %#v", manifest)
	}
	surface := manifest.SelfDescribingSurfaces[0]
	if surface.Kind != "canvas2d" || surface.Feature != "engines" || surface.Runtime != "shared" || surface.Count != 1 {
		t.Fatalf("surface entry = %+v", surface)
	}
	if !containsString(surface.Capabilities, "canvas") || !containsString(surface.Capabilities, "webgpu") {
		t.Fatalf("surface capabilities = %#v", surface.Capabilities)
	}
}

func TestSelfDescribingSurfacesDoNotDuplicateScripts(t *testing.T) {
	r := NewRenderer("surface-test")
	r.Surface(gosx.CanvasBoard(gosx.CanvasBoardProps{ID: "one"}))
	r.Surface(gosx.CanvasBoard(gosx.CanvasBoardProps{ID: "two"}))

	head := gosx.RenderHTML(r.PageHead())
	for _, marker := range []string{
		`data-gosx-script="wasm-exec"`,
		`data-gosx-script="bootstrap"`,
	} {
		if got := strings.Count(head, marker); got != 1 {
			t.Fatalf("%s count = %d, want 1\n%s", marker, got, head)
		}
	}
	if got := strings.Count(head, `data-gosx-script="feature-engines"`); got != 0 {
		t.Fatalf("feature-engines static script count = %d, want 0\n%s", got, head)
	}
	if got := r.Summary().SelfDescribingSurfaces; got != 2 {
		t.Fatalf("self-describing surface count = %d, want 2", got)
	}
	manifest := r.clientManifest()
	if manifest == nil || len(manifest.SelfDescribingSurfaces) != 1 || manifest.SelfDescribingSurfaces[0].Count != 2 {
		t.Fatalf("coalesced surface manifest = %#v", manifest)
	}
	if r.Summary().BootstrapFeatureEnginesPath == "" {
		t.Fatalf("surface summary missing engine feature path: %+v", r.Summary())
	}
}

func TestSelfDescribingSurfaceMissingKindIsNoop(t *testing.T) {
	r := NewRenderer("surface-test")
	node := gosx.El("canvas", gosx.Attrs(gosx.Attr("id", "blank-surface")))

	if got := r.Surface(node); gosx.RenderHTML(got) != gosx.RenderHTML(node) {
		t.Fatalf("blank surface node changed: %s", gosx.RenderHTML(got))
	}
	if summary := r.Summary(); summary.Bootstrap || summary.SelfDescribingSurfaces != 0 || summary.BootstrapFeatureEnginesPath != "" {
		t.Fatalf("missing kind changed summary: %+v", summary)
	}
	if manifest := r.clientManifest(); manifest != nil && len(manifest.SelfDescribingSurfaces) != 0 {
		t.Fatalf("missing kind registered manifest surfaces: %#v", manifest)
	}
}

func TestSelfDescribingSurfaceLeavesOtherPlansUnchanged(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		summary := NewRenderer("empty").Summary()
		if summary.Bootstrap || summary.BootstrapMode != "none" || summary.RuntimePath != "" || summary.BootstrapFeatureEnginesPath != "" {
			t.Fatalf("empty summary changed: %+v", summary)
		}
	})

	t.Run("lite", func(t *testing.T) {
		r := NewRenderer("lite")
		r.EnableBootstrap()
		summary := r.Summary()
		if !summary.Bootstrap || summary.BootstrapMode != "lite" || summary.RuntimePath != "" || summary.BootstrapFeatureEnginesPath != "" {
			t.Fatalf("lite summary changed: %+v", summary)
		}
	})

	t.Run("island", func(t *testing.T) {
		r := NewRenderer("island")
		r.RenderIslandFromProgram(program.CounterProgram(), map[string]int{"initial": 1})
		summary := r.Summary()
		if summary.Islands != 1 || summary.SelfDescribingSurfaces != 0 || summary.BootstrapFeatureEnginesPath != "" {
			t.Fatalf("island summary changed: %+v", summary)
		}
	})

	t.Run("normal-engine", func(t *testing.T) {
		r := NewRenderer("engine")
		r.RenderEngine(engine.Config{Name: "NormalEngine", Kind: engine.KindSurface}, gosx.Text(""))
		summary := r.Summary()
		if summary.Engines != 1 || summary.SelfDescribingSurfaces != 0 || summary.BootstrapFeatureEnginesPath == "" {
			t.Fatalf("engine summary changed: %+v", summary)
		}
		if len(r.Manifest().Engines) != 1 {
			t.Fatalf("normal engine manifest count = %d", len(r.Manifest().Engines))
		}
	})
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
