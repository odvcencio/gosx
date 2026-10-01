package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunCheckTypesNoStrictComponentIsOK covers a legacy-only page: the
// strict projection is empty (transpile.TranspilePackageWithSharedImports
// projects nothing for a package with no strict component), so there is
// nothing for the go/types oracle to check, and runCheckTypes reports that
// as ok rather than an error. This fixture uses a bare temp directory with
// no go.mod at all, the same as TestRunCheckAcceptsModernGSXShapes: since
// nothing here reaches `go list`, none is needed.
func TestRunCheckTypesNoStrictComponentIsOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	writeTempFile(t, dir, "page.gsx", `package main

func Page(item Item) Node {
	return <article>{item.Title}</article>
}
`)

	var out bytes.Buffer
	if err := runCheckTypes(path, &out); err != nil {
		t.Fatalf("runCheckTypes: %v", err)
	}
	if !strings.Contains(out.String(), "types: ok") {
		t.Fatalf("output = %q, want it to report ok", out.String())
	}
}

// TestRunCheckTypesReportsTypeError proves `gosx check --types`'s
// implementation reports a go/types error at the .gsx file's own position,
// via runCheckTypes' own structured diagnostics rather than strictcheck's
// single joined compiler-output error string. strictcheck's own goCheck
// (runCheck) already feeds the same projection through a real Go compiler
// and would reject this same fixture too — internal/typeoracle's added
// value is not new pass/fail coverage over that (see its package doc's
// "Role" section): it is a live *types.Package a caller can query (props
// struct field sets, assignability -- see internal/typeoracle's own query
// tests) and diagnostics as individual ir.Diagnostic values rather than one
// opaque wrapped compiler-output string.
func TestRunCheckTypesReportsTypeError(t *testing.T) {
	dir := newInvalidStrictStarter(t, "check-types-error")
	path := filepath.Join(dir, "app", "page.gsx")
	mustWriteFile(t, path, `package app

type CardProps struct {
	Label string
}

const Limit int = "oops"

component Card(props: CardProps) {
	return <p>{props.Label}</p>
}

component Page() {
	return <Card label="hi" />
}
`)

	var out bytes.Buffer
	err := runCheckTypes(path, &out)
	if err == nil {
		t.Fatal("runCheckTypes = nil error, want it to report the const mismatch")
	}
	if !strings.Contains(err.Error(), "1 diagnostic") {
		t.Fatalf("error = %v, want it to report one diagnostic", err)
	}
	if !strings.Contains(out.String(), "page.gsx:7:") {
		t.Fatalf("output = %q, want a finding positioned at page.gsx:7", out.String())
	}
	if !strings.Contains(out.String(), `cannot use "oops"`) {
		t.Fatalf("output = %q, want it to name the offending literal", out.String())
	}
}
