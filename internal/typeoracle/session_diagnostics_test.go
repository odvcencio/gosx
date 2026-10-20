package typeoracle

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestSessionReportsTypeErrorAtGSXPosition covers fixture (b): a real Go
// type error in a .gsx file's own top-level const declaration — a shape
// ir.Lower's own semantic gate never inspects (it validates props-derived
// expressions inside a component body, not an ordinary const initializer;
// see this package's report for the fixtures ir.Lower does already catch)
// — must be reported by go/types at the const's true .gsx file, line, and
// column, not at the temporary projection go/types actually parsed.
func TestSessionReportsTypeErrorAtGSXPosition(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main

type CardProps struct {
	Count int
}

const Limit int = "oops"

component Card(props: CardProps) {
	return <div>hi</div>
}

component Page() {
	return <Card />
}
`)
	session := loadTestSession(t, dir)
	if len(session.Diagnostics) != 1 {
		t.Fatalf("Diagnostics = %#v, want exactly one", session.Diagnostics)
	}
	diag := session.Diagnostics[0]
	if diag.Span.File != path {
		t.Fatalf("Span.File = %q, want %q", diag.Span.File, path)
	}
	if diag.Span.StartLine != 7 {
		t.Fatalf("Span.StartLine = %d, want 7 (the const declaration's own .gsx line)", diag.Span.StartLine)
	}
	// "const Limit int = " is 18 runes; the offending literal starts at
	// column 19. This is the payoff of transpile's lineDirective carrying
	// a column (gosx#... this slice): a passthrough declaration like this
	// one gets a fully accurate column, not the column-1 fallback a
	// diagnostic on a synthesized line still needs.
	if diag.Span.StartCol != 19 {
		t.Fatalf("Span.StartCol = %d, want 19", diag.Span.StartCol)
	}
	if !strings.Contains(diag.Message, `cannot use "oops"`) {
		t.Fatalf("Message = %q, want it to name the offending literal", diag.Message)
	}
}

// TestSessionCleanPackageHasNoDiagnostics is the control for
// TestSessionReportsTypeErrorAtGSXPosition: an otherwise identical package
// with no type error must check clean, so the fixture above is proven to
// fail because of the "oops" mismatch specifically, not some unrelated gap
// in Session's projection or importer wiring.
func TestSessionCleanPackageHasNoDiagnostics(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), `package main

type CardProps struct {
	Count int
}

const Limit int = 10

component Card(props: CardProps) {
	return <div>hi</div>
}

component Page() {
	return <Card />
}
`)
	session := loadTestSession(t, dir)
	if len(session.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %#v, want none", session.Diagnostics)
	}
	if session.Package == nil {
		t.Fatal("Package = nil, want a checked package")
	}
}

// TestSessionNoStrictComponentReturnsEmptySession covers a legacy-only
// directory: transpile.TranspilePackageWithSharedImports projects nothing
// for it (see package.go's own doc comment), so there is nothing for
// go/types to check. Session must report that as a normal, error-free,
// nil-Package result — not fail, and not panic when a caller's query
// method is still called against it.
func TestSessionNoStrictComponentReturnsEmptySession(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), `package main

func Page(data any) Node {
	return <div>{data}</div>
}
`)
	session, err := LoadWithOptions(context.Background(), dir, testOptions())
	if err != nil {
		t.Fatalf("LoadWithOptions: %v", err)
	}
	if session.Package != nil {
		t.Fatalf("Package = %v, want nil for a legacy-only directory", session.Package)
	}
	if len(session.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %#v, want none", session.Diagnostics)
	}
	if _, ok := session.LookupType("Page"); ok {
		t.Fatal("LookupType unexpectedly resolved against a nil Package")
	}
}

// TestLoadFileMatchesLoadWithOptions proves LoadFile (the entry point
// gosx check's per-file command line naturally has a path for, not a
// directory) resolves the identical package LoadWithOptions(dir) does when
// pointed at one of that directory's own files.
func TestLoadFileMatchesLoadWithOptions(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main

type CardProps struct {
	Count int
}

const Limit int = "oops"

component Card(props: CardProps) {
	return <div>hi</div>
}

component Page() {
	return <Card />
}
`)
	session, err := LoadFileWithOptions(context.Background(), path, testOptions())
	if err != nil {
		t.Fatalf("LoadFileWithOptions: %v", err)
	}
	if len(session.Diagnostics) != 1 {
		t.Fatalf("Diagnostics = %#v, want exactly one", session.Diagnostics)
	}
	if session.Diagnostics[0].Span.StartLine != 7 {
		t.Fatalf("Span.StartLine = %d, want 7", session.Diagnostics[0].Span.StartLine)
	}
}
