package strictcheck

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckFileReportsComponentTypeNameCollision covers gosx#230 ask 3: a
// strict component and a sibling .go type share a name, so the projection
// redeclares it. The diagnostic must name both declarations and point at
// the .gsx line the component is written on, not at the temporary
// projection file the Go compiler would have blamed.
func TestCheckFileReportsComponentTypeNameCollision(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, filepath.Join(dir, "page.server.go"), `package main

type MatchupCard struct {
	Tone string
}

func LoadCard() MatchupCard { return MatchupCard{Tone: "red"} }
`)
	mustWrite(t, path, `package main
type MatchupCardProps struct {
	Tone string
}
component MatchupCard(props: MatchupCardProps) {
	return <span>{props.Tone}</span>
}
component Page() {
	return <main>ok</main>
}
`)
	err := CheckFile(context.Background(), path)
	if err == nil {
		t.Fatal("CheckFile unexpectedly accepted a component/type name collision")
	}
	for _, want := range []string{
		"strict component MatchupCard collides with type MatchupCard declared at page.server.go:3:6",
		"a Go package declares each name once",
		"page.gsx:5:1",
		"MatchupCardData",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want to contain %q", err, want)
		}
	}
	// The generic Go redeclaration error must not be what the author sees.
	if strings.Contains(err.Error(), "zz_gosx_strictcheck") {
		t.Fatalf("error names the temporary projection file: %v", err)
	}
}

// TestCheckFileReportsGsxTypeNameCollision covers the other half of the
// same projection: a .gsx type declaration also lands in the package, so a
// sibling .go type of the same name collides identically.
func TestCheckFileReportsGsxTypeNameCollision(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, filepath.Join(dir, "page.server.go"), `package main

type CardProps struct {
	Tone string
}
`)
	mustWrite(t, path, `package main
type CardProps struct {
	Tone string
}
component Card(props: CardProps) {
	return <span>{props.Tone}</span>
}
component Page() {
	return <main>ok</main>
}
`)
	err := CheckFile(context.Background(), path)
	if err == nil {
		t.Fatal("CheckFile unexpectedly accepted a type name collision")
	}
	if want := "type CardProps collides with type CardProps declared at page.server.go:3:6"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want to contain %q", err, want)
	}
}

// TestCheckFileAcceptsDistinctSiblingDeclarations is the false-positive
// guard: a sibling .go file carrying the converter type, its loader func,
// and package-level vars and consts must keep checking clean while the
// .gsx declares its own renderer-visible schema beside it.
func TestCheckFileAcceptsDistinctSiblingDeclarations(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, filepath.Join(dir, "page.server.go"), `package main

type MatchupCardData struct {
	Tone string
}

const defaultTone = "red"

var cardCount = 0

func LoadCard() MatchupCardData { return MatchupCardData{Tone: defaultTone} }

func (d MatchupCardData) Card() string { return d.Tone }
`)
	mustWrite(t, path, `package main
type MatchupCardProps struct {
	Tone string
}
component MatchupCard(props: MatchupCardProps) {
	return <span>{props.Tone}</span>
}
component Page() {
	return <main>ok</main>
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

// TestCheckFileIgnoresTestFileDeclarations guards a sibling file that never
// joins the projection's build: `go list -export .` compiles the non-test
// package only, so a name declared in a _test.go file is not a
// redeclaration of a projected one.
func TestCheckFileIgnoresTestFileDeclarations(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, filepath.Join(dir, "page_test.go"), `package main

type Badge struct{}
`)
	mustWrite(t, path, `package main
type BadgeProps struct {
	Tone string
}
component Badge(props: BadgeProps) {
	return <span>{props.Tone}</span>
}
component Page() {
	return <main>ok</main>
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

// TestSiblingGoDeclsSkipsForeignPackageFile guards the package-name filter
// directly. A directory may hold .gsx files of more than one package (see
// TestCheckTreeChecksEachPackageClauseInOneDirectory), so a .go file whose
// package clause names another package must not lend its declarations to
// the package under check.
func TestSiblingGoDeclsSkipsForeignPackageFile(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "mine.go"), "package main\n\ntype Mine struct{}\n")
	mustWrite(t, filepath.Join(dir, "theirs.go"), "package other\n\ntype Theirs struct{}\n")
	decls := siblingGoDecls(dir, "main")
	if _, ok := decls["Mine"]; !ok {
		t.Fatalf("siblingGoDecls dropped a same-package declaration: %#v", decls)
	}
	if _, ok := decls["Theirs"]; ok {
		t.Fatalf("siblingGoDecls kept a foreign-package declaration: %#v", decls)
	}
}

// TestCheckFileIgnoresExcludedBuildConstraintDeclarations guards a sibling
// .go file the current build never compiles: an unsatisfied build
// constraint keeps its declarations out of the package, so a shared name is
// not a redeclaration and must not be reported as one.
func TestCheckFileIgnoresExcludedBuildConstraintDeclarations(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, filepath.Join(dir, "plan9only.go"), `//go:build plan9 && arm

package main

type Badge struct{}
`)
	mustWrite(t, path, `package main
type BadgeProps struct {
	Tone string
}
component Badge(props: BadgeProps) {
	return <span>{props.Tone}</span>
}
component Page() {
	return <main>ok</main>
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

// TestCheckFileIgnoresSiblingMethodNames guards a method: a func with a
// receiver declares no package-level name, so a method named like a
// component is not a collision.
func TestCheckFileIgnoresSiblingMethodNames(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, filepath.Join(dir, "page.server.go"), `package main

type carrier struct{}

func (carrier) Badge() string { return "badge" }
`)
	mustWrite(t, path, `package main
type BadgeProps struct {
	Tone string
}
component Badge(props: BadgeProps) {
	return <span>{props.Tone}</span>
}
component Page() {
	return <main>ok</main>
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

// TestCheckPackageAcceptsLegacyOnlyPackageWithSharedName guards the gate
// itself: a package with no strict syntax produces no projection, so a
// legacy .gsx component sharing a name with a sibling .go declaration is
// not this check's business. Legacy .gsx components are interpreted by the
// file router; they never land in the Go package.
func TestCheckPackageAcceptsLegacyOnlyPackageWithSharedName(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, filepath.Join(dir, "page.server.go"), `package main

type Badge struct{}
`)
	mustWrite(t, path, `package main
func Badge(props any) Node {
	return <span>badge</span>
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}
