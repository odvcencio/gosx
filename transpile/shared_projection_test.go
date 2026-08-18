package transpile

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// This file covers WP4 of the shared-.gsx-components design (see
// gsx-shared-components-spec.md, section 11): the Go PROJECTION of a
// cross-directory component call, taken from a supplied resolver map. The
// resolver itself (a directory walk and a real `go list` call) belongs to
// gosx check (strictcheck, WP5); these tests build the map by hand or with
// CollectSharedComponents, exactly the shape gosx check will supply.

// sharedTeamMarkSource is the shared directory's own .gsx source: a strict
// component with props, and nothing else (design section 1.4).
const sharedTeamMarkSource = `package ui

type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"team-mark tone-" + props.Tone}>{props.Abbreviation}</span>
}
`

// sharedTeamMarkPackageFile compiles sharedTeamMarkSource into a PackageFile,
// the shape CollectSharedComponents (and gosx check's directory load) both
// consume.
func sharedTeamMarkPackageFile(t *testing.T) PackageFile {
	t.Helper()
	source := []byte(sharedTeamMarkSource)
	prog, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("compile shared source: %v", err)
	}
	return PackageFile{Path: "ui/team_mark.gsx", Source: source, Program: prog}
}

func TestSharedCallProjectsQualifiedCompositeLiteral(t *testing.T) {
	shared := CollectSharedComponents([]PackageFile{sharedTeamMarkPackageFile(t)})
	if _, ok := shared["TeamMark"]; !ok {
		t.Fatalf("CollectSharedComponents did not resolve TeamMark from shared source")
	}

	caller := []byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func StandingRow(tone, abbreviation string) gosx.Node {
	return <div><ui.TeamMark Tone={tone} Abbreviation={abbreviation}></ui.TeamMark></div>
}
`)
	out, err := Transpile(caller, Options{
		SourceFile: "page.gsx",
		SharedImports: map[string]SharedImport{
			"./ui": {GoImportPath: "example.test/app/ui", Components: shared},
		},
	})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}

	if want := `ui.TeamMark(ui.TeamMarkProps{Tone: tone, Abbreviation: abbreviation})`; !strings.Contains(out, want) {
		t.Fatalf("missing shared call %q in:\n%s", want, out)
	}
	if want := `ui "example.test/app/ui"`; !strings.Contains(out, want) {
		t.Fatalf("missing rewritten shared import %q in:\n%s", want, out)
	}
	if strings.Contains(out, `"./ui"`) {
		t.Fatalf("relative import path leaked into projected Go:\n%s", out)
	}
}

// TestSharedCallStructurallyMatchesSameFileShape is the parity guard the
// task's discipline section asks for: a shared call and a same-file call
// with an identical props shape and identical attributes must project to
// the identical call shape, differing only by the "alias." qualifier a
// shared call adds to both the function name and the composite literal
// type. If a future change lets the two shapes drift apart, this test
// catches it independent of any wording in either message.
//
// Both sides use a strict caller body — the spec's own headline example
// (section 1.2) — projected with transpileProjectionOnly, which runs only
// t.emit, skipping Transpile's gosx.Compile semantic gate. WP1 (the ir.Lower
// relaxation that will let a strict body reach a shared call through the
// full gosx.Compile pipeline) is a separate, independently dispatched work
// package; exercising the projection layer directly is what proves WP4's
// own scope without waiting on it, exactly as designed (spec section 1.3:
// "type safety... is already proven today. Execution is not" — the same
// split applies to this test rig: the shape is proven here, the semantic
// gate is WP1's).
func TestSharedCallStructurallyMatchesSameFileShape(t *testing.T) {
	sameFile := []byte(`package app

type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"team-mark tone-" + props.Tone}>{props.Abbreviation}</span>
}

type StandingRowProps struct {
	Tone         string
	Abbreviation string
}

component StandingRow(props: StandingRowProps) {
	return <div><TeamMark Tone={props.Tone} Abbreviation={props.Abbreviation}></TeamMark></div>
}
`)
	sameFileOut, err := transpileProjectionOnly(t, sameFile, Options{SourceFile: "same.gsx"})
	if err != nil {
		t.Fatalf("transpile same-file (projection only): %v", err)
	}

	shared := CollectSharedComponents([]PackageFile{sharedTeamMarkPackageFile(t)})
	sharedCaller := []byte(`package app

import ui "./ui"

type StandingRowProps struct {
	Tone         string
	Abbreviation string
}

component StandingRow(props: StandingRowProps) {
	return <div><ui.TeamMark Tone={props.Tone} Abbreviation={props.Abbreviation}></ui.TeamMark></div>
}
`)
	sharedOut, err := transpileProjectionOnly(t, sharedCaller, Options{
		SourceFile: "page.gsx",
		SharedImports: map[string]SharedImport{
			"./ui": {GoImportPath: "example.test/app/ui", Components: shared},
		},
	})
	if err != nil {
		t.Fatalf("transpile shared (projection only): %v", err)
	}

	// Strip exactly the alias qualifier a shared call adds to the call and
	// to the composite literal type; nothing else about the call shape may
	// differ.
	normalizedShared := strings.ReplaceAll(sharedOut, "ui.TeamMark", "TeamMark")
	sameFileCall := extractCall(t, sameFileOut, "TeamMark(")
	sharedCall := extractCall(t, normalizedShared, "TeamMark(")
	if sameFileCall != sharedCall {
		t.Fatalf("shared call shape drifted from same-file call shape:\nsame-file: %s\nshared (normalized): %s", sameFileCall, sharedCall)
	}
}

// transpileProjectionOnly runs exactly the projection half of Transpile:
// parse, then t.emit(root). It skips Transpile's gosx.Compile semantic
// gate, which is how ir.Lower's pending WP1 relaxation (shared-call
// admission for a strict caller body) would otherwise block this test from
// exercising transpile's own, already-complete half of the feature.
func transpileProjectionOnly(t *testing.T, source []byte, opts Options) (string, error) {
	t.Helper()
	tree, lang, err := gosx.Parse(source)
	if err != nil {
		return "", err
	}
	root := tree.RootNode()
	if root.HasError() {
		return "", gosx.DescribeParseError(root, source, lang)
	}
	tp := &transpiler{
		src:              source,
		lang:             lang,
		sourceFile:       opts.SourceFile,
		imports:          make(map[string]string),
		strictProjection: opts.strictProjection,
		importNames:      opts.importNames,
		sharedImports:    opts.SharedImports,
	}
	result := tp.emit(root)
	if len(tp.errs) > 0 {
		return "", errors.New(strings.Join(tp.errs, "\n"))
	}
	return result, nil
}

// extractCall returns the balanced-parenthesis call expression starting at
// the last occurrence of prefix in src — the call site, not the earlier
// func TeamMark(props TeamMarkProps) declaration that also starts with
// "TeamMark(".
func extractCall(t *testing.T, src, prefix string) string {
	t.Helper()
	idx := strings.LastIndex(src, prefix)
	if idx < 0 {
		t.Fatalf("call prefix %q not found in:\n%s", prefix, src)
	}
	depth := 0
	for i := idx; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[idx : i+1]
			}
		}
	}
	t.Fatalf("unbalanced call starting at %q in:\n%s", prefix, src)
	return ""
}

// TestSharedSpreadCallKeepsExistingRefusal covers the task's explicit
// requirement to keep transpile.go:1195's refusal unchanged: a spread
// attribute at a shared strict callee is proven only at the file renderer
// boundary in v1, never by gosx transpile.
func TestSharedSpreadCallKeepsExistingRefusal(t *testing.T) {
	shared := CollectSharedComponents([]PackageFile{sharedTeamMarkPackageFile(t)})
	caller := []byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func StandingRow(team map[string]any) gosx.Node {
	return <div><ui.TeamMark {...team}></ui.TeamMark></div>
}
`)
	_, err := Transpile(caller, Options{
		SourceFile: "page.gsx",
		SharedImports: map[string]SharedImport{
			"./ui": {GoImportPath: "example.test/app/ui", Components: shared},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "proven by the file renderer boundary, not by gosx transpile") {
		t.Fatalf("error = %v, want the file-renderer-boundary refusal", err)
	}
}

// TestSharedCallWithNoResolverNamesGosxCheck covers the task's explicit
// exit gate: a file carrying a shared import, transpiled with no resolver
// at all, must fail with a message naming gosx check rather than produce
// broken Go.
func TestSharedCallWithNoResolverNamesGosxCheck(t *testing.T) {
	caller := []byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func StandingRow(tone, abbreviation string) gosx.Node {
	return <div><ui.TeamMark Tone={tone} Abbreviation={abbreviation}></ui.TeamMark></div>
}
`)
	out, err := Transpile(caller, Options{SourceFile: "page.gsx"})
	if err == nil {
		t.Fatalf("Transpile succeeded with no resolver; got broken-Go output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "gosx check") {
		t.Fatalf("error = %v, want a message naming gosx check", err)
	}
	if strings.Contains(out, "ui.TeamMark(") {
		t.Fatalf("unresolved shared call leaked a broken call into output:\n%s", out)
	}
}

// TestSharedCallWithUnknownComponentNamesGosxCheck covers the "resolver
// present but this component absent" half of the same discipline: a
// resolved shared import that does not declare the called component must
// not fall back to an untyped call (which would emit non-compiling Go);
// it fails the same way as a wholly unresolved import.
func TestSharedCallWithUnknownComponentNamesGosxCheck(t *testing.T) {
	caller := []byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func StandingRow() gosx.Node {
	return <div><ui.Missing></ui.Missing></div>
}
`)
	out, err := Transpile(caller, Options{
		SourceFile: "page.gsx",
		SharedImports: map[string]SharedImport{
			"./ui": {GoImportPath: "example.test/app/ui", Components: map[string]SharedComponent{}},
		},
	})
	if err == nil {
		t.Fatalf("Transpile succeeded for an undeclared shared component; got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "gosx check") {
		t.Fatalf("error = %v, want a message naming gosx check", err)
	}
}

// TestStrictProjectionRetainsRewrittenSharedImport covers the task's
// explicit exit-gate requirement: strictProjectionImports must retain a
// shared import — with its path rewritten to the resolved Go import path —
// because the alias is a selector root of the projected call.
func TestStrictProjectionRetainsRewrittenSharedImport(t *testing.T) {
	shared := CollectSharedComponents([]PackageFile{sharedTeamMarkPackageFile(t)})
	caller := []byte(`package app

import ui "./ui"

type StandingRowProps struct {
	Tone         string
	Abbreviation string
}

component StandingRow(props: StandingRowProps) {
	return <div><ui.TeamMark Tone={props.Tone} Abbreviation={props.Abbreviation}></ui.TeamMark></div>
}
`)
	// Uses transpileProjectionOnly for the same reason as
	// TestSharedCallStructurallyMatchesSameFileShape above: a strict
	// caller's shared call does not yet clear Transpile's gosx.Compile gate
	// (that relaxation is WP1's, not WP4's), so this test exercises
	// strict-projection emission directly.
	out, err := transpileProjectionOnly(t, caller, Options{
		SourceFile:       "page.gsx",
		strictProjection: true,
		SharedImports: map[string]SharedImport{
			"./ui": {GoImportPath: "example.test/app/ui", Components: shared},
		},
	})
	if err != nil {
		t.Fatalf("transpile (strict projection): %v", err)
	}
	if want := `ui "example.test/app/ui"`; !strings.Contains(out, want) {
		t.Fatalf("strict projection dropped the rewritten shared import; got:\n%s", out)
	}
	if want := `ui.TeamMark(ui.TeamMarkProps{`; !strings.Contains(out, want) {
		t.Fatalf("strict projection missing shared call; got:\n%s", out)
	}
}

// TestSharedCallResolvesLowerCamelAttribute proves CollectSharedComponents
// genuinely resolves field aliases from the shared directory's own source
// (design section 5.2's "resolve, do not constrain" — open question 3),
// not merely the exact-spelling fallback: a lower-camel attribute name
// resolves to its exact exported Go field the same way a same-file call
// already resolves one.
func TestSharedCallResolvesLowerCamelAttribute(t *testing.T) {
	shared := CollectSharedComponents([]PackageFile{sharedTeamMarkPackageFile(t)})
	caller := []byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func StandingRow(tone, abbreviation string) gosx.Node {
	return <div><ui.TeamMark tone={tone} abbreviation={abbreviation}></ui.TeamMark></div>
}
`)
	out, err := Transpile(caller, Options{
		SourceFile: "page.gsx",
		SharedImports: map[string]SharedImport{
			"./ui": {GoImportPath: "example.test/app/ui", Components: shared},
		},
	})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if want := `ui.TeamMark(ui.TeamMarkProps{Tone: tone, Abbreviation: abbreviation})`; !strings.Contains(out, want) {
		t.Fatalf("lower-camel attribute did not resolve to its exact field; got:\n%s", out)
	}
}

// TestSharedCallProjectionCompiles is the golden end-to-end proof: the
// projected Go for a cross-directory named-attribute call, written to a
// real module alongside the shared package's own projected Go, compiles
// with the real Go compiler. This is the exit gate's strongest form —
// stronger than a string match, because it proves the composite literal's
// field names and the props struct's field types actually line up.
func TestSharedCallProjectionCompiles(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}

	sharedFile := sharedTeamMarkPackageFile(t)
	sharedGo, err := Transpile(sharedFile.Source, Options{SourceFile: sharedFile.Path})
	if err != nil {
		t.Fatalf("Transpile shared: %v", err)
	}
	sharedComponents := CollectSharedComponents([]PackageFile{sharedFile})

	callerSource := []byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func StandingRow(tone, abbreviation string) gosx.Node {
	return <div><ui.TeamMark Tone={tone} Abbreviation={abbreviation}></ui.TeamMark></div>
}
`)
	callerGo, err := Transpile(callerSource, Options{
		SourceFile: "page.gsx",
		SharedImports: map[string]SharedImport{
			"./ui": {GoImportPath: "example.test/app/ui", Components: sharedComponents},
		},
	})
	if err != nil {
		t.Fatalf("Transpile caller: %v", err)
	}

	dir := newGoBuildModule(t)
	mustWriteGoFile(t, filepath.Join(dir, "ui", "team_mark.go"), sharedGo)
	mustWriteGoFile(t, filepath.Join(dir, "app", "page.go"), callerGo)

	cmd := exec.CommandContext(context.Background(), "go", "build", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s\n--- ui/team_mark.go ---\n%s\n--- app/page.go ---\n%s", err, out, sharedGo, callerGo)
	}
}

// newGoBuildModule builds a temp Go module with a minimal m31labs.dev/gosx
// stub (the same node-builder surface transpile targets), replaced in so a
// generated file's "m31labs.dev/gosx" import resolves without network
// access or the real module's much larger dependency graph.
func newGoBuildModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "gosxstub")
	mustWriteGoFile(t, filepath.Join(stub, "node.go"), `package gosx

type Node struct{}
type AttrValue struct{}
type AttrList []AttrValue

func El(string, ...any) Node       { return Node{} }
func Text(string) Node             { return Node{} }
func Expr(any) Node                { return Node{} }
func RawHTML(string) Node          { return Node{} }
func Fragment(...Node) Node        { return Node{} }
func Attr(string, any) AttrValue   { return AttrValue{} }
func Attrs(...AttrValue) any       { return nil }
func Props(values ...AttrValue) AttrList { return values }
func Spread(any) AttrValue         { return AttrValue{} }
func If(cond bool, child Node) Node { return Node{} }
func Map[T any](items []T, fn func(T, int) Node) Node { return Node{} }
`)
	mustWriteGoFile(t, filepath.Join(stub, "go.mod"), "module m31labs.dev/gosx\n\ngo 1.26\n")
	mustWriteGoFile(t, filepath.Join(dir, "go.mod"),
		"module example.test/app\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\nreplace m31labs.dev/gosx => "+filepath.ToSlash(stub)+"\n")
	return dir
}

func mustWriteGoFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
