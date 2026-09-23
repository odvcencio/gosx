package ir

import "testing"

func TestImportTableDefaultName(t *testing.T) {
	tbl := NewImportTable()
	tbl.Add("m31labs.dev/gosx/signal", "")

	path, ok := tbl.Lookup("signal")
	if !ok || path != "m31labs.dev/gosx/signal" {
		t.Fatalf("Lookup(%q) = (%q, %v), want (%q, true)", "signal", path, ok, "m31labs.dev/gosx/signal")
	}
}

func TestImportTableExplicitAlias(t *testing.T) {
	tbl := NewImportTable()
	tbl.Add("m31labs.dev/gosx/signal", "sig")

	if path, ok := tbl.Lookup("sig"); !ok || path != "m31labs.dev/gosx/signal" {
		t.Fatalf("Lookup(%q) = (%q, %v), want the signal import path", "sig", path, ok)
	}
	// The default name is no longer bound once an explicit alias is given.
	if _, ok := tbl.Lookup("signal"); ok {
		t.Fatalf("Lookup(%q) should be unbound once the import uses alias %q", "signal", "sig")
	}
}

func TestImportTableDifferentPackageSameLocalName(t *testing.T) {
	tbl := NewImportTable()
	tbl.Add("some/other/pkg", "signal")

	path, ok := tbl.Lookup("signal")
	if !ok || path != "some/other/pkg" {
		t.Fatalf("Lookup(%q) = (%q, %v), want the unrelated package's path", "signal", path, ok)
	}
	if path == "m31labs.dev/gosx/signal" {
		t.Fatalf("an unrelated package aliased %q must not resolve to the gosx signal path", "signal")
	}
}

func TestImportTableDotImport(t *testing.T) {
	tbl := NewImportTable()
	tbl.Add("m31labs.dev/gosx/signal", ".")

	if !tbl.HasDotImport("m31labs.dev/gosx/signal") {
		t.Fatal("dot import of signal should register as a dot import")
	}
	if _, ok := tbl.Lookup("signal"); ok {
		t.Fatal("a dot import must not bind the identifier \"signal\"")
	}
}

func TestImportTableBlankImport(t *testing.T) {
	tbl := NewImportTable()
	tbl.Add("m31labs.dev/gosx/signal", "_")

	if _, ok := tbl.Lookup("signal"); ok {
		t.Fatal("a blank import must not bind any identifier")
	}
	if tbl.HasDotImport("m31labs.dev/gosx/signal") {
		t.Fatal("a blank import is not a dot import")
	}
}

func TestLexicalScopeShadowing(t *testing.T) {
	scope := NewLexicalScope()
	if scope.Shadows("signal") {
		t.Fatal("an empty scope should shadow nothing")
	}
	scope.Bind("signal")
	if !scope.Shadows("signal") {
		t.Fatal("a bound name should be shadowed")
	}
}

func TestLexicalScopeIgnoresBlank(t *testing.T) {
	scope := NewLexicalScope()
	scope.Bind("_")
	if scope.Shadows("_") {
		t.Fatal("the blank identifier is never a real binding")
	}
}

func TestLexicalScopeNilSafe(t *testing.T) {
	var scope *LexicalScope
	if scope.Shadows("anything") {
		t.Fatal("a nil scope should shadow nothing")
	}
	// Bind on a nil receiver must not panic.
	scope.Bind("anything")
}
