// Package typeoracle type-checks a GoSX package's transpiled Go alongside
// its hand-written sibling .go files, using go/types, and maps every
// resulting diagnostic back to .gsx source positions.
//
// # Role
//
// gotreesitter and ir stay the parser, CST, and IR layer GoSX authoring and
// the LSP already depend on; this package replaces none of that. strictcheck
// already proves structural rules with go/ast pattern matches (see
// strictcheck/servergo.go), which is why a strict component's props type
// must be declared in the same .gsx file: the file renderer, and every
// go/ast-based proof, only ever sees one file. A separate part of
// strictcheck (goCheck, strictcheck/check.go) already feeds the same
// projection through a real Go compiler too, so this package rarely proves
// a healthy strict package unhealthy that strictcheck alone would have
// passed — the two mostly agree on pass/fail (cmd/gosx/check_types.go's own
// doc comment covers this in more detail for the CLI's --types flag).
//
// What Session is for is a second, additive opinion consulted for type
// facts neither of those give: a props struct's real field types (including
// one declared in a sibling .go file), whether an expression assigns to a
// field, and structured, individual diagnostics (ir.Diagnostic values, not
// one joined compiler-output string) a caller like an LSP can attribute
// one at a time. The shape matches templ, Vue's Volar, and svelte-check: a
// generated-Go shadow feeds a real type checker, and positions map back
// through the shadow to the authored source.
//
// # Source mapping
//
// transpile already carries the position map this package needs.
// transpile.TranspilePackageWithSharedImports (strict-projection mode)
// prefixes every top-level declaration with a "//line <gsx-file>:<line>"
// directive (transpile.go, emitStrictSourceFile). go/parser interprets that
// directive automatically, so every AST node parsed from the generated Go
// already carries its true .gsx file and line in its token.Pos — Session
// adds no source mapping of its own, it only reads what transpile already
// writes for strictcheck/collision.go's identical purpose.
//
// A directive with no explicit column resets column tracking to 0
// ("unknown") for every position in that segment, including the
// directive's own first line, not only the lines after it (verified
// against this Go toolchain's go/parser). transpile.go's lineDirective now
// supplies a column for a const/type declaration and a typed-legacy stub,
// because go/parser recomputes every subsequent line's column from the
// generated text's own bytes once a directive supplies one at all — and
// both of those declaration kinds emit text that is byte-identical (const/
// type) or keyword-aligned (a typed-legacy stub's "func Name(...)" matches
// its source's own "func Name(...)") to the .gsx source, so that recomputed
// column is the true one throughout the whole declaration, not just its
// first line.
//
// A strict `component Name(...)` declaration is the one exception:
// emitStrictComponent's signature line is synthesized ("func Name(...)"
// replacing source's "component Name(...)", a different length), so a
// column anchored to the source position would misalign every position on
// that synthesized first line. transpile.go's lineDirectiveNoColumn keeps
// the original no-column shape for that one declaration kind, so it is
// still only row-accurate, and Session's diagnostic conversion falls back
// to column 1 there — the same fallback strictcheck/collision.go's
// declSpan already uses for the identical reason. Closing that gap (an
// accurate column even inside a strict component's own body) would need a
// second directive positioned at the return statement, or a per-line
// directive throughout the body; left as a next step, since it touches
// transpile's own JSX emission walk rather than this package.
//
// # Importer choice
//
// go/importer's "source" mode reads sibling package source directly, but it
// resolves import paths through go/build.Context, which predates modules
// and cannot resolve a module-qualified import path outside GOPATH
// (golang/go#28387) — unusable for a real module like this one.
// golang.org/x/tools/go/packages is the module-aware answer every serious
// go/types consumer (gopls, templ, svelte-check-style tools) reaches for,
// but it is not a dependency of gosx today, and stdlib is preferred first.
//
// So Session drives go/importer.ForCompiler(fset, "gc", lookup): the same
// "gc" compiler export-data format `go list -export` already produces,
// which strictcheck's own goCheck already shells out to (strictcheck/
// check.go) to type-check a projection through the real Go compiler. This
// package asks the same command for the same data and reads it directly
// with go/types instead, because building a query API (a props struct's
// field set, an expression's assignability) needs a live *types.Package and
// *types.Info in hand, not a pass/fail compiler exit code. The lookup
// function this package builds from one `go list -export -deps -json` call
// per session is the only module-aware, stdlib-only way to resolve a real
// dependency graph's compiled type information; go/importer's own fallback
// path (lookup == nil) only resolves $GOPATH-style installed packages and
// is never used here.
//
// # Scope of this slice
//
// Session type-checks one package directory's strict-projected .gsx
// declarations plus its sibling .go files. It does not resolve shared
// (./ or ../ prefixed) cross-directory component imports — every directory
// this package loads is checked in isolation, the same way strictcheck's
// own goCheck was checked in isolation before shared-import resolution was
// added on top of it. Wiring shared-import resolution through is the
// natural next step and does not require any change to Session's shape:
// LoadWithOptions already threads an Options value a caller can extend.
package typeoracle
