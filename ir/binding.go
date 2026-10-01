// Package ir — import-binding layer.
//
// This file gives the lowerer one place to answer "does this identifier
// refer to that import path?" instead of the name-based shortcut it used
// to take. Before this file existed, ir/lower.go's signalCallKind treated
// any call shaped like `X.New(...)` as a gosx signal constructor whenever
// X's source text was literally "signal" — with no check that "signal"
// actually named the m31labs.dev/gosx/signal import in this file. Three
// bugs followed from that shortcut:
//
//  1. A local variable or parameter named "signal" shadowed the intended
//     package meaning; `signal.New` still lowered as a signal constructor.
//  2. A different import that also used the local name "signal" (an
//     unrelated package, aliased or default-named "signal") was silently
//     treated as the gosx signal package.
//  3. Importing gosx's signal package under an alias worked only because
//     of a second, separately maintained set (signalImports); every new
//     name-based check in the lowerer would have needed its own copy of
//     that logic.
//
// ImportTable fixes (2) and gives (3) one home; LexicalScope fixes (1).
// Both are plain data structures with no gotreesitter dependency, so they
// stay usable from any lowering path (CST-based ir/lower.go today; a
// future go/ast-based path could reuse ImportTable's Add/Lookup shape
// too) and impose no TinyGo build-tag cost.
package ir

import (
	"path"
	"strings"
)

// ImportTable is one source file's import table: every import spec's
// resolved local identifier, keyed by that identifier, plus the set of
// dot-imported paths (which contribute no keyed identifier — a dot
// import's exported names merge directly into file scope and are never
// reached through "Identifier.Member" syntax, so callers check
// HasDotImport for those instead of Lookup).
//
// A later Add for the same local name replaces the earlier binding, the
// same way a real Go compiler would reject the file for a duplicate
// import identifier — ir/lower.go's caller only ever sees the file as
// actually written, so the common case is exactly one Add per name.
type ImportTable struct {
	byName map[string]string // local identifier -> import path
	dots   map[string]bool   // import path -> true, for `import . "path"`
}

// NewImportTable returns an empty import table.
func NewImportTable() *ImportTable {
	return &ImportTable{
		byName: make(map[string]string),
		dots:   make(map[string]bool),
	}
}

// Add records one import spec's binding. importPath is the quoted import
// path with the quotes already stripped. alias is the source text of an
// explicit import name: "" when the import has no explicit name (the
// local identifier is then the path's last segment), "." for a dot
// import, "_" for a blank import, or the explicit alias text otherwise.
func (t *ImportTable) Add(importPath, alias string) {
	if t == nil {
		return
	}
	importPath = strings.TrimSpace(importPath)
	if importPath == "" {
		return
	}
	switch alias {
	case "_":
		// Side-effect only; contributes no identifier to file scope.
		return
	case ".":
		t.dots[importPath] = true
	case "":
		t.byName[path.Base(importPath)] = importPath
	default:
		t.byName[alias] = importPath
	}
}

// Lookup reports the import path bound to localName in this file's
// import table, ignoring lexical scope — callers that also need to
// respect local shadowing should check LexicalScope.Shadows first (see
// PackageOf, which does both).
func (t *ImportTable) Lookup(localName string) (string, bool) {
	if t == nil || localName == "" {
		return "", false
	}
	p, ok := t.byName[localName]
	return p, ok
}

// HasDotImport reports whether importPath was imported with
// `import . "importPath"`, merging its exported names directly into
// this file's scope.
func (t *ImportTable) HasDotImport(importPath string) bool {
	return t != nil && t.dots[strings.TrimSpace(importPath)]
}

// PackageOf reports the import path that localName resolves to, given
// the identifiers shadowing it in scope. It returns ok=false both when
// localName is not an import identifier at all, and when scope shadows
// it with a local declaration (a param, `:=`, or `var`) — the caller
// cannot tell those two cases apart from PackageOf alone; a caller that
// needs to (to support an implicit default package name absent any
// import, the way gosx's own examples write `signal.New` without ever
// importing "m31labs.dev/gosx/signal") should call Lookup and
// LexicalScope.Shadows directly instead.
func (t *ImportTable) PackageOf(localName string, scope *LexicalScope) (string, bool) {
	if scope.Shadows(localName) {
		return "", false
	}
	return t.Lookup(localName)
}

// LexicalScope tracks identifiers bound by a function's parameters and
// its locally declared variables, so a binding lookup can tell a package
// reference (X.New) apart from a local or parameter that happens to
// share the import's identifier.
//
// It is intentionally coarse: one flat set per function scope, not a
// nested per-block scope. ir/lower.go's signal/computed/handler
// detection only ever inspects a function's top-level statements (never
// descends into a nested if/for/switch block — see analyzeBody), so a
// flat set already matches every call site precisely: a name bound
// inside a nested block cannot leak out to shadow a sibling top-level
// statement, and this type is never asked about names visible only
// inside such a block.
type LexicalScope struct {
	bound map[string]bool
}

// NewLexicalScope returns an empty lexical scope.
func NewLexicalScope() *LexicalScope {
	return &LexicalScope{bound: make(map[string]bool)}
}

// Bind records name as a local declaration — a parameter, a `:=` target,
// or a `var` name — that shadows any import of the same identifier from
// this point in the scope onward. The blank identifier "_" is never a
// real binding and Bind ignores it, matching Go's own scoping rules.
func (s *LexicalScope) Bind(name string) {
	if s == nil || name == "" || name == "_" {
		return
	}
	s.bound[name] = true
}

// Shadows reports whether name is bound by a local declaration in this
// scope. A nil scope shadows nothing.
func (s *LexicalScope) Shadows(name string) bool {
	if s == nil || name == "" {
		return false
	}
	return s.bound[name]
}
