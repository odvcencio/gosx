package ir

import (
	"strings"
	"testing"
)

// --- helpers -----------------------------------------------------------------

// makeCanvasComponent returns a minimal Program + component index for a surface
// engine component whose root is a <canvas> element.
//
// attrs is the list of Attr values to attach to the canvas node.
func makeCanvasProgram(attrs []Attr) (*Program, int) {
	prog := &Program{
		Package:     "graph",
		PackagePath: "github.com/odvcencio/gosx/examples/graph",
	}
	prog.Nodes = append(prog.Nodes, Node{
		Kind:  NodeElement,
		Tag:   "canvas",
		Attrs: attrs,
	})
	prog.Components = append(prog.Components, Component{
		Name:               "Graph",
		PropsType:          "GraphProps",
		Root:               0,
		IsEngine:           true,
		EngineKind:         "surface",
		EngineCapabilities: []string{"canvas", "pointer"},
		EngineSurface:      true,
		SurfaceHandlers: collectHandlers(attrs),
	})
	return prog, 0
}

// collectHandlers builds a SurfaceHandlerRef slice from on* expression attrs.
func collectHandlers(attrs []Attr) []SurfaceHandlerRef {
	var refs []SurfaceHandlerRef
	for _, a := range attrs {
		if !strings.HasPrefix(a.Name, "on") || len(a.Name) <= 2 {
			continue
		}
		if a.Name[2] < 'A' || a.Name[2] > 'Z' {
			continue
		}
		if a.Kind != AttrExpr {
			continue
		}
		refs = append(refs, SurfaceHandlerRef{
			EventName:    a.Name,
			FunctionName: a.Expr,
		})
	}
	return refs
}

// --- tests -------------------------------------------------------------------

// TestLowerEngineSurfaceHappyPath verifies that LowerEngineSurface produces a
// well-formed SurfaceProgram for a valid canvas component with two handlers and
// a static width attribute.
func TestLowerEngineSurfaceHappyPath(t *testing.T) {
	attrs := []Attr{
		{Kind: AttrStatic, Name: "width", Value: "100%"},
		{Kind: AttrStatic, Name: "height", Value: "600px"},
		{Kind: AttrExpr, Name: "onMount", Expr: "mount", IsEvent: true},
		{Kind: AttrExpr, Name: "onClick", Expr: "onSelect", IsEvent: true},
	}
	prog, idx := makeCanvasProgram(attrs)

	sp, err := LowerEngineSurface(prog, idx)
	if err != nil {
		t.Fatalf("LowerEngineSurface: %v", err)
	}

	if sp.Name != "Graph" {
		t.Errorf("Name: want %q, got %q", "Graph", sp.Name)
	}
	if sp.Package != "github.com/odvcencio/gosx/examples/graph" {
		t.Errorf("Package: want %q, got %q", "github.com/odvcencio/gosx/examples/graph", sp.Package)
	}
	if sp.PropsTypeName != "GraphProps" {
		t.Errorf("PropsTypeName: want %q, got %q", "GraphProps", sp.PropsTypeName)
	}
	if len(sp.Capabilities) != 2 || sp.Capabilities[0] != "canvas" || sp.Capabilities[1] != "pointer" {
		t.Errorf("Capabilities: want [canvas pointer], got %v", sp.Capabilities)
	}

	// MountAttrs should contain width and height but not the on* attrs.
	if sp.MountAttrs["width"] != "100%" {
		t.Errorf("MountAttrs[width]: want %q, got %q", "100%", sp.MountAttrs["width"])
	}
	if sp.MountAttrs["height"] != "600px" {
		t.Errorf("MountAttrs[height]: want %q, got %q", "600px", sp.MountAttrs["height"])
	}
	if _, ok := sp.MountAttrs["onMount"]; ok {
		t.Error("MountAttrs should not contain onMount")
	}

	// Handlers should be translated to canonical names.
	if len(sp.Handlers) != 2 {
		t.Fatalf("Handlers: want 2, got %d: %+v", len(sp.Handlers), sp.Handlers)
	}
	findHandler := func(event string) *SurfaceHandlerBind {
		for i := range sp.Handlers {
			if sp.Handlers[i].EventName == event {
				return &sp.Handlers[i]
			}
		}
		return nil
	}
	if h := findHandler("mount"); h == nil || h.FunctionName != "mount" {
		t.Errorf("expected handler {mount mount}, got %+v", findHandler("mount"))
	}
	if h := findHandler("click"); h == nil || h.FunctionName != "onSelect" {
		t.Errorf("expected handler {click onSelect}, got %+v", findHandler("click"))
	}

	// SourceFingerprint must be non-empty (even without source files the
	// capabilities and handlers contribute to the hash).
	if sp.SourceFingerprint == "" {
		t.Error("SourceFingerprint must not be empty")
	}
}

// TestLowerEngineSurfaceRejectNonCanvas verifies that a component whose root is
// not <canvas> is rejected: EngineSurface must be false after lowering, and
// LowerEngineSurface must return an error.
func TestLowerEngineSurfaceRejectNonCanvas(t *testing.T) {
	// Build a program where the root is <div>, not <canvas>.
	prog := &Program{
		Package:     "graph",
		PackagePath: "github.com/odvcencio/gosx/examples/graph",
	}
	prog.Nodes = append(prog.Nodes, Node{
		Kind: NodeElement,
		Tag:  "div",
	})
	prog.Components = append(prog.Components, Component{
		Name:          "Graph",
		Root:          0,
		IsEngine:      true,
		EngineKind:    "surface",
		// EngineSurface intentionally NOT set — simulates what Lower produces
		// after encountering a non-canvas root.
		EngineSurface: false,
	})

	_, err := LowerEngineSurface(prog, 0)
	if err == nil {
		t.Fatal("expected error for non-canvas root, got nil")
	}
	if !strings.Contains(err.Error(), "not an engine surface") {
		t.Errorf("error message should mention 'not an engine surface', got: %v", err)
	}

	// The validate pass should also emit a diagnostic.
	// Simulate what the lowering pass would do: call lowerEngineSurface
	// directly via the lowerer, using a fake lowerer.
	l := &lowerer{
		src:           nil,
		srcStr:        "",
		lang:          nil,
		prog:          prog,
		signalImports: make(map[string]struct{}),
	}
	comp := &prog.Components[0]
	l.lowerEngineSurface(comp)

	if comp.EngineSurface {
		t.Error("EngineSurface must remain false for non-canvas root")
	}
	if len(l.errs) == 0 {
		t.Error("expected at least one diagnostic for non-canvas root")
	}
	foundMsg := false
	for _, d := range l.errs {
		if strings.Contains(d.Message, "engine surface root must be <canvas>") {
			foundMsg = true
		}
	}
	if !foundMsg {
		t.Errorf("expected diagnostic about <canvas> root, got: %+v", l.errs)
	}
}

// TestLowerEngineSurfaceRejectUnknownHandler verifies that an unknown on*
// handler attribute produces a diagnostic and is excluded from SurfaceHandlers.
func TestLowerEngineSurfaceRejectUnknownHandler(t *testing.T) {
	prog := &Program{
		Package:     "graph",
		PackagePath: "github.com/odvcencio/gosx/examples/graph",
	}
	prog.Nodes = append(prog.Nodes, Node{
		Kind: NodeElement,
		Tag:  "canvas",
		Attrs: []Attr{
			{Kind: AttrExpr, Name: "onMount", Expr: "mount", IsEvent: true},
			{Kind: AttrExpr, Name: "onLaserPointer", Expr: "laser", IsEvent: true}, // unknown
		},
	})
	comp := Component{
		Name:          "Graph",
		Root:          0,
		IsEngine:      true,
		EngineKind:    "surface",
		EngineSurface: false,
	}
	prog.Components = append(prog.Components, comp)

	l := &lowerer{
		prog:          prog,
		signalImports: make(map[string]struct{}),
	}
	c := &prog.Components[0]
	l.lowerEngineSurface(c)

	// Should still be valid (the known handler was accepted).
	if !c.EngineSurface {
		t.Error("EngineSurface should be true when at least the canvas root is valid")
	}

	// The unknown handler should produce a diagnostic.
	foundUnknown := false
	for _, d := range l.errs {
		if strings.Contains(d.Message, "onLaserPointer") {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Errorf("expected diagnostic for unknown handler onLaserPointer, got: %+v", l.errs)
	}

	// The unknown handler must not appear in SurfaceHandlers.
	for _, ref := range c.SurfaceHandlers {
		if ref.EventName == "onLaserPointer" {
			t.Error("onLaserPointer should not appear in SurfaceHandlers")
		}
	}
	// The known handler must appear.
	foundMount := false
	for _, ref := range c.SurfaceHandlers {
		if ref.EventName == "onMount" {
			foundMount = true
		}
	}
	if !foundMount {
		t.Error("onMount should appear in SurfaceHandlers")
	}
}

// TestLowerEngineSurfaceFingerprintStability verifies that the same input
// always produces the same fingerprint, and different inputs produce different
// fingerprints.
func TestLowerEngineSurfaceFingerprintStability(t *testing.T) {
	attrs := []Attr{
		{Kind: AttrStatic, Name: "width", Value: "100%"},
		{Kind: AttrExpr, Name: "onMount", Expr: "mount", IsEvent: true},
	}
	prog, idx := makeCanvasProgram(attrs)
	sp1, err := LowerEngineSurface(prog, idx)
	if err != nil {
		t.Fatalf("first lower: %v", err)
	}
	sp2, err := LowerEngineSurface(prog, idx)
	if err != nil {
		t.Fatalf("second lower: %v", err)
	}
	if sp1.SourceFingerprint != sp2.SourceFingerprint {
		t.Errorf("fingerprint not stable: %q vs %q", sp1.SourceFingerprint, sp2.SourceFingerprint)
	}

	// Different capabilities → different fingerprint.
	prog.Components[idx].EngineCapabilities = []string{"canvas", "pointer", "webgl"}
	sp3, err := LowerEngineSurface(prog, idx)
	if err != nil {
		t.Fatalf("third lower: %v", err)
	}
	if sp3.SourceFingerprint == sp1.SourceFingerprint {
		t.Error("fingerprint should differ when capabilities change")
	}
}

// TestLowerEngineSurfaceOutOfRange verifies that an out-of-range component
// index produces an error.
func TestLowerEngineSurfaceOutOfRange(t *testing.T) {
	prog := &Program{}
	_, err := LowerEngineSurface(prog, 5)
	if err == nil {
		t.Fatal("expected error for out-of-range index")
	}
}

// TestValidateEngineSurfaceNonCanvasRoot verifies that Validate emits a
// diagnostic when an engine surface component has a non-canvas root. This
// tests the validate pass independently of the lowering pass.
func TestValidateEngineSurfaceNonCanvasRoot(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes, Node{Kind: NodeElement, Tag: "div"})
	prog.Components = append(prog.Components, Component{
		Name:          "Bad",
		Root:          0,
		IsEngine:      true,
		EngineKind:    "surface",
		EngineSurface: false, // not set because root is not canvas
	})

	diags := Validate(prog)
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "engine surface root must be <canvas>") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected diagnostic about non-canvas root in Validate, got: %+v", diags)
	}
}
