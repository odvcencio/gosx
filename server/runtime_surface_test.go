package server

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestPageRuntimeSurfaceActivatesSharedRuntime(t *testing.T) {
	runtime := NewPageRuntime()
	node := runtime.Surface(gosx.CanvasBoard(gosx.CanvasBoardProps{ID: "board"}))

	if !runtime.Active() {
		t.Fatal("Surface did not activate page runtime")
	}
	if got := gosx.RenderHTML(node); !strings.Contains(got, `data-gosx-surface-kind="canvas2d"`) {
		t.Fatalf("surface node changed unexpectedly: %s", got)
	}

	summary := runtime.Summary()
	if !summary.Bootstrap || !summary.Runtime || summary.BootstrapMode != "full" {
		t.Fatalf("surface summary = %+v", summary)
	}
	if summary.RuntimePath == "" || summary.WASMExecPath == "" || summary.BootstrapFeatureEnginesPath == "" {
		t.Fatalf("surface runtime paths missing: %+v", summary)
	}
	if summary.Engines != 0 || summary.SelfDescribingSurfaces != 1 {
		t.Fatalf("surface counts = engines %d self-describing %d", summary.Engines, summary.SelfDescribingSurfaces)
	}
}

func TestPageRuntimeMultipleSurfacesEmitOneRuntimeSet(t *testing.T) {
	runtime := NewPageRuntime()
	runtime.Surface(gosx.CanvasBoard(gosx.CanvasBoardProps{ID: "one"}))
	runtime.Surface(gosx.CanvasBoard(gosx.CanvasBoardProps{ID: "two"}))

	head := gosx.RenderHTML(runtime.Head())
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
	if got := runtime.Summary().SelfDescribingSurfaces; got != 2 {
		t.Fatalf("self-describing surface count = %d, want 2", got)
	}
}

func TestPageRuntimeSurfaceMissingKindDoesNotActivate(t *testing.T) {
	runtime := NewPageRuntime()
	node := gosx.El("canvas", gosx.Attrs(gosx.Attr("id", "blank-surface")))
	got := runtime.Surface(node)

	if runtime.Active() {
		t.Fatal("missing-kind surface activated page runtime")
	}
	if gosx.RenderHTML(got) != gosx.RenderHTML(node) {
		t.Fatalf("missing-kind surface changed node: %s", gosx.RenderHTML(got))
	}
	if summary := runtime.Summary(); summary.Bootstrap || summary.SelfDescribingSurfaces != 0 || summary.BootstrapFeatureEnginesPath != "" {
		t.Fatalf("missing-kind summary = %+v", summary)
	}
}
