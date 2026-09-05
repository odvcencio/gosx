package transpile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
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
	sameFileOut, err := Transpile(sameFile, Options{SourceFile: "same.gsx"})
	if err != nil {
		t.Fatalf("transpile same-file: %v", err)
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
	sharedOut, err := Transpile(sharedCaller, Options{
		SourceFile: "page.gsx",
		SharedImports: map[string]SharedImport{
			"./ui": {GoImportPath: "example.test/app/ui", Components: shared},
		},
	})
	if err != nil {
		t.Fatalf("transpile shared: %v", err)
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
	out, err := Transpile(caller, Options{
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

func TestCollectSharedComponentsIncludesProplessChildrenAndNamedSlots(t *testing.T) {
	source := []byte(`package ui

component Chrome() {
	return <section><h1>{slotTitle}</h1><main>{children}</main></section>
}
`)
	prog, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("compile shared source: %v", err)
	}
	shared := CollectSharedComponents([]PackageFile{{Path: "ui/chrome.gsx", Source: source, Program: prog}})
	comp, ok := shared["Chrome"]
	if !ok {
		t.Fatalf("Chrome missing from shared components: %#v", shared)
	}
	if comp.PropsType != "" {
		t.Fatalf("PropsType = %q, want propless", comp.PropsType)
	}
	if !comp.AcceptsChildren {
		t.Fatal("AcceptsChildren = false, want true")
	}
	if got := strings.Join(comp.AcceptsSlots, ","); got != "Title" {
		t.Fatalf("AcceptsSlots = %q, want Title", got)
	}
}

func TestCollectSharedComponentsSourceOnlyMatchesPrecompiledMetadata(t *testing.T) {
	source := []byte(`package ui

type ChromeProps struct {
	HTMLFor string
}

component Chrome(props: ChromeProps) {
	return <section for={props.HTMLFor}>{slotTitle}{children}</section>
}
`)
	prog, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("compile shared source: %v", err)
	}
	precompiled := CollectSharedComponents([]PackageFile{{Path: "ui/chrome.gsx", Source: source, Program: prog}})
	sourceOnly := CollectSharedComponents([]PackageFile{{Path: "ui/chrome.gsx", Source: source}})
	if !reflect.DeepEqual(sourceOnly, precompiled) {
		t.Fatalf("source-only metadata drifted from precompiled:\nsource-only: %#v\nprecompiled: %#v", sourceOnly, precompiled)
	}
}

func TestCollectSharedComponentsSkipsIslandNeighborButKeepsPureView(t *testing.T) {
	pureSource := []byte(`package ui

component Frame() {
	return <section>{children}</section>
}
`)
	islandSource := []byte(`package ui

//gosx:island
component Counter() {
	return <button>count</button>
}
`)
	pureProg, err := gosx.Compile(pureSource)
	if err != nil {
		t.Fatalf("compile pure source: %v", err)
	}
	islandProg, err := gosx.Compile(islandSource)
	if err != nil {
		t.Fatalf("compile island source: %v", err)
	}
	shared := CollectSharedComponents([]PackageFile{
		{Path: "ui/frame.gsx", Source: pureSource, Program: pureProg},
		{Path: "ui/counter.gsx", Source: islandSource, Program: islandProg},
	})
	if _, ok := shared["Frame"]; !ok {
		t.Fatalf("pure Frame missing from shared components: %#v", shared)
	}
	if _, ok := shared["Counter"]; ok {
		t.Fatalf("island Counter entered pure-view shared catalog: %#v", shared)
	}
}

func TestCollectSharedComponentsOmitsDuplicateNamesIndependentOfInputOrder(t *testing.T) {
	first := []byte(`package ui

component Badge() {
	return <span>a</span>
}
`)
	second := []byte(`package ui

component Badge() {
	return <span>b</span>
}
`)
	firstProg, err := gosx.Compile(first)
	if err != nil {
		t.Fatalf("compile first source: %v", err)
	}
	secondProg, err := gosx.Compile(second)
	if err != nil {
		t.Fatalf("compile second source: %v", err)
	}
	for name, files := range map[string][]PackageFile{
		"forward": {
			{Path: "ui/a.gsx", Source: first, Program: firstProg},
			{Path: "ui/b.gsx", Source: second, Program: secondProg},
		},
		"reversed": {
			{Path: "ui/b.gsx", Source: second, Program: secondProg},
			{Path: "ui/a.gsx", Source: first, Program: firstProg},
		},
	} {
		t.Run(name, func(t *testing.T) {
			shared := CollectSharedComponents(files)
			if _, ok := shared["Badge"]; ok {
				t.Fatalf("duplicate Badge selected a winner from %s input order: %#v", name, shared)
			}
		})
	}
}

func TestCollectSharedComponentsOmitsPureIslandDuplicateNamesIndependentOfInputOrder(t *testing.T) {
	pure := []byte(`package ui

component Badge() {
	return <span>pure</span>
}
`)
	island := []byte(`package ui

//gosx:island
component Badge() {
	return <span>island</span>
}
`)
	pureProg, err := gosx.Compile(pure)
	if err != nil {
		t.Fatalf("compile pure source: %v", err)
	}
	islandProg, err := gosx.Compile(island)
	if err != nil {
		t.Fatalf("compile island source: %v", err)
	}
	for name, files := range map[string][]PackageFile{
		"pure first": {
			{Path: "ui/a.gsx", Source: pure, Program: pureProg},
			{Path: "ui/b.gsx", Source: island, Program: islandProg},
		},
		"island first": {
			{Path: "ui/a.gsx", Source: island, Program: islandProg},
			{Path: "ui/b.gsx", Source: pure, Program: pureProg},
		},
	} {
		t.Run(name, func(t *testing.T) {
			shared := CollectSharedComponents(files)
			if _, ok := shared["Badge"]; ok {
				t.Fatalf("mixed pure/island duplicate Badge selected a projection target from %s input order: %#v", name, shared)
			}
		})
	}
}

func TestSharedProplessCallsProjectWithoutPropsArgument(t *testing.T) {
	source := []byte(`package ui

component Badge() {
	return <span>badge</span>
}

component Wrap() {
	return <section>{children}</section>
}
`)
	prog, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("compile shared source: %v", err)
	}
	shared := CollectSharedComponents([]PackageFile{{Path: "ui/badge.gsx", Source: source, Program: prog}})
	caller := []byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func Page() gosx.Node {
	return <div><ui.Badge /><ui.Wrap><b>caller</b></ui.Wrap></div>
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
	for _, want := range []string{
		`ui.Badge()`,
		`ui.Wrap(gosx.El("b", gosx.Text("caller")))`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestSharedTypedNamedSlotsProjectByDeclaredName(t *testing.T) {
	source := []byte(`package ui

type LayoutProps struct {
	ID string
}

component Layout(props: LayoutProps) {
	return <section id={props.ID}>{slotTitle}<nav>{slotNav}</nav>{children}</section>
}
`)
	prog, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("compile shared source: %v", err)
	}
	shared := CollectSharedComponents([]PackageFile{{Path: "ui/layout.gsx", Source: source, Program: prog}})
	caller := []byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func Page() gosx.Node {
	return <ui.Layout ID="main"><p>body</p><h1 slot="Title">Title</h1></ui.Layout>
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
	want := `ui.Layout(ui.LayoutProps{ID: "main"}, gosx.Fragment(), gosx.El("h1", gosx.Text("Title")), gosx.El("p", gosx.Text("body")))`
	if !strings.Contains(out, want) {
		t.Fatalf("shared slot args did not follow declared slot order with zero omitted slot and caller-owned children:\nwant %q\nin:\n%s", want, out)
	}
}

func TestSharedNamedSlotPayloadMayBeASharedComponent(t *testing.T) {
	source := []byte(`package ui

component Bare() {
	return <span>bare</span>
}

component Frame() {
	return <section>{slotTitle}{children}</section>
}
`)
	prog, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("compile shared source: %v", err)
	}
	shared := CollectSharedComponents([]PackageFile{{Path: "ui/frame.gsx", Source: source, Program: prog}})
	caller := []byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func Page() gosx.Node {
	return <ui.Frame><p>body</p><ui.Bare slot="Title" /></ui.Frame>
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
	want := `ui.Frame(ui.Bare(), gosx.El("p", gosx.Text("body")))`
	if !strings.Contains(out, want) {
		t.Fatalf("shared component slot payload did not stay caller-owned:\nwant %q\nin:\n%s", want, out)
	}
}

func TestSharedProjectionRejectsChildrenForLeafComponent(t *testing.T) {
	shared := CollectSharedComponents([]PackageFile{sharedTeamMarkPackageFile(t)})
	caller := []byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func Page() gosx.Node {
	return <ui.TeamMark Tone="home" Abbreviation="NE"><b>lost</b></ui.TeamMark>
}
`)
	_, err := Transpile(caller, Options{
		SourceFile: "page.gsx",
		SharedImports: map[string]SharedImport{
			"./ui": {GoImportPath: "example.test/app/ui", Components: shared},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "renders no children") {
		t.Fatalf("error = %v, want shared children rejection", err)
	}
}

func TestSharedProjectionRejectsEncodedWhitespaceAndEmptyFragmentLeafBody(t *testing.T) {
	shared := CollectSharedComponents([]PackageFile{sharedTeamMarkPackageFile(t)})
	for name, body := range map[string]string{
		"nbsp":          "&nbsp;",
		"encodedSpace":  "&#32;",
		"encodedTab":    "&#9;",
		"emptyFragment": "<></>",
	} {
		t.Run(name, func(t *testing.T) {
			caller := []byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func Page() gosx.Node {
	return <ui.TeamMark Tone="home" Abbreviation="NE">` + body + `</ui.TeamMark>
}
`)
			_, err := Transpile(caller, Options{
				SourceFile: "page.gsx",
				SharedImports: map[string]SharedImport{
					"./ui": {GoImportPath: "example.test/app/ui", Components: shared},
				},
			})
			if err == nil || !strings.Contains(err.Error(), "renders no children") {
				t.Fatalf("Transpile error = %v, want meaningful child rejection", err)
			}
		})
	}
}

func TestSharedProjectionIgnoresWhitespaceAndCommentOnlyLeafBody(t *testing.T) {
	shared := CollectSharedComponents([]PackageFile{sharedTeamMarkPackageFile(t)})
	for name, body := range map[string]string{
		"whitespace":  "\n\t\t\n",
		"commentOnly": "\n\t\t// staging note\n\t\t\n",
	} {
		t.Run(name, func(t *testing.T) {
			caller := []byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func Page() gosx.Node {
	return <ui.TeamMark Tone="home" Abbreviation="NE">` + body + `</ui.TeamMark>
}
`)
			if _, err := Transpile(caller, Options{
				SourceFile: "page.gsx",
				SharedImports: map[string]SharedImport{
					"./ui": {GoImportPath: "example.test/app/ui", Components: shared},
				},
			}); err != nil {
				t.Fatalf("Transpile rejected formatting-only shared leaf body: %v", err)
			}

			routerRoot := t.TempDir()
			mustWriteGoFile(t, filepath.Join(routerRoot, "ui", "team_mark.gsx"), sharedTeamMarkSource)
			mustWriteGoFile(t, filepath.Join(routerRoot, "page.gsx"), string(caller))
			node, err := route.DefaultFileRenderer(nil, route.FilePage{FilePath: filepath.Join(routerRoot, "page.gsx"), Pattern: "/"})
			if err != nil {
				t.Fatalf("DefaultFileRenderer rejected formatting-only shared leaf body after Transpile accepted it: %v", err)
			}
			want := `<span class="team-mark tone-home">NE</span>`
			if got := gosx.RenderHTML(node); got != want {
				t.Fatalf("file-router html = %q, want %q", got, want)
			}
		})
	}
}

func TestSharedProjectionRejectsUnknownNamedSlot(t *testing.T) {
	shared := CollectSharedComponents([]PackageFile{sharedTeamMarkPackageFile(t)})
	caller := []byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func Page() gosx.Node {
	return <ui.TeamMark Tone="home" Abbreviation="NE"><h1 slot="Title">lost</h1></ui.TeamMark>
}
`)
	_, err := Transpile(caller, Options{
		SourceFile: "page.gsx",
		SharedImports: map[string]SharedImport{
			"./ui": {GoImportPath: "example.test/app/ui", Components: shared},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `declares no slot named "Title"`) {
		t.Fatalf("error = %v, want shared unknown-slot rejection", err)
	}
}

func TestSharedProjectionRejectsAttributesForProplessComponent(t *testing.T) {
	source := []byte(`package ui

component Badge() {
	return <span>badge</span>
}
`)
	prog, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("compile shared source: %v", err)
	}
	shared := CollectSharedComponents([]PackageFile{{Path: "ui/badge.gsx", Source: source, Program: prog}})
	caller := []byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func Page() gosx.Node {
	return <ui.Badge Label="lost" />
}
`)
	_, err = Transpile(caller, Options{
		SourceFile: "page.gsx",
		SharedImports: map[string]SharedImport{
			"./ui": {GoImportPath: "example.test/app/ui", Components: shared},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "declares no props") {
		t.Fatalf("error = %v, want propless shared attribute rejection", err)
	}
}

func TestSharedSiblingFilePropsShapeProjectsDeclaredFields(t *testing.T) {
	typeSource := []byte(`package ui

type CardProps struct {
	Label string
}
`)
	cardSource := []byte(`package ui

component Card(props: CardProps) {
	return <article>card</article>
}
`)
	typeProg, err := gosx.Compile(typeSource)
	if err != nil {
		t.Fatalf("compile type source: %v", err)
	}
	cardProg, err := gosx.Compile(cardSource)
	if err != nil {
		t.Fatalf("compile card source: %v", err)
	}
	shared := CollectSharedComponents([]PackageFile{
		{Path: "ui/types.gsx", Source: typeSource, Program: typeProg},
		{Path: "ui/card.gsx", Source: cardSource, Program: cardProg},
	})
	caller := []byte(`package app

import (
	"m31labs.dev/gosx"
	ui "./ui"
)

func Page() gosx.Node {
	return <ui.Card label="Visible" />
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
	if want := `ui.Card(ui.CardProps{Label: "Visible"})`; !strings.Contains(out, want) {
		t.Fatalf("sibling-file props shape was not used for shared projection; want %q in:\n%s", want, out)
	}
}

func TestSameFileTypedSlotPropAtDirectRootRendersLikeFileRouter(t *testing.T) {
	source := []byte(`package app

type BadgeProps struct {
	Slot string
}

component Badge(props: BadgeProps) {
	return <span data-slot={props.Slot}>{props.Slot}</span>
}

component Page() {
	return <Badge slot="direct" />
}
`)
	generated, err := Transpile(source, Options{SourceFile: "app/page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if want := `Badge(BadgeProps{Slot: "direct"})`; !strings.Contains(generated, want) {
		t.Fatalf("direct slot prop was not projected as a typed prop; want %q in:\n%s", want, generated)
	}
	assertGeneratedAppAndFileRouterHTML(t, generated, source, `<span data-slot="direct">direct</span>`)
}

func TestSharedTypedSlotPropAtDirectRootRendersLikeFileRouter(t *testing.T) {
	sharedSource := []byte(`package ui

type BadgeProps struct {
	Slot string
}

component Badge(props: BadgeProps) {
	return <span data-slot={props.Slot}>{props.Slot}</span>
}
`)
	callerSource := []byte(`package app

import ui "./ui"

component Page() {
	return <ui.Badge slot="direct" />
}
`)
	sharedProg, err := gosx.Compile(sharedSource)
	if err != nil {
		t.Fatalf("compile shared source: %v", err)
	}
	sharedComponents := CollectSharedComponents([]PackageFile{{Path: "app/ui/badge.gsx", Source: sharedSource, Program: sharedProg}})
	sharedGo, err := Transpile(sharedSource, Options{SourceFile: "app/ui/badge.gsx"})
	if err != nil {
		t.Fatalf("Transpile shared: %v", err)
	}
	callerGo, err := Transpile(callerSource, Options{
		SourceFile:       "app/page.gsx",
		strictProjection: true,
		SharedImports: map[string]SharedImport{
			"./ui": {GoImportPath: "example.test/render/app/ui", Components: sharedComponents},
		},
	})
	if err != nil {
		t.Fatalf("Transpile caller: %v", err)
	}
	if want := `ui.Badge(ui.BadgeProps{Slot: "direct"})`; !strings.Contains(callerGo, want) {
		t.Fatalf("direct shared slot prop was not projected as a typed prop; want %q in:\n%s", want, callerGo)
	}

	module := newActualGoSXRenderModule(t)
	mustWriteGoFile(t, filepath.Join(module, "app", "ui", "badge.go"), sharedGo)
	mustWriteGoFile(t, filepath.Join(module, "app", "page.go"), callerGo)
	runGeneratedRenderTest(t, module, `<span data-slot="direct">direct</span>`, sharedGo, callerGo)

	routerRoot := t.TempDir()
	mustWriteGoFile(t, filepath.Join(routerRoot, "ui", "badge.gsx"), string(sharedSource))
	mustWriteGoFile(t, filepath.Join(routerRoot, "page.gsx"), string(callerSource))
	node, err := route.DefaultFileRenderer(nil, route.FilePage{FilePath: filepath.Join(routerRoot, "page.gsx"), Pattern: "/"})
	if err != nil {
		t.Fatalf("DefaultFileRenderer: %v", err)
	}
	if got, want := gosx.RenderHTML(node), `<span data-slot="direct">direct</span>`; got != want {
		t.Fatalf("file-router html = %q, want %q", got, want)
	}
}

func TestSharedGeneratedGoRendersLikeFileRouterForSlotsAndChildren(t *testing.T) {
	sharedSource := []byte(`package ui

type FrameProps struct {
	ID string
}

component Badge() {
	return <span class="badge">badge</span>
}

component Shell() {
	return <div class="shell">{children}</div>
}

component Frame(props: FrameProps) {
	return <section id={props.ID}><header>{slotTitle}</header><aside>{slotAside}</aside><footer>{slotFooter}</footer><main>{children}</main></section>
}
`)
	callerSource := []byte(`package app

import ui "./ui"

component Page() {
	return <ui.Frame ID="home"><ui.Shell><p>body</p></ui.Shell><ui.Badge slot="Title" /><strong slot="Aside">aside</strong></ui.Frame>
}
`)
	sharedFile := PackageFile{Path: "app/ui/frame.gsx", Source: sharedSource}
	callerFile := PackageFile{Path: "app/page.gsx", Source: callerSource}
	sharedProg, err := gosx.Compile(sharedSource)
	if err != nil {
		t.Fatalf("compile shared source: %v", err)
	}
	callerProg, err := gosx.Compile(callerSource)
	if err != nil {
		t.Fatalf("compile caller source: %v", err)
	}
	sharedFile.Program = sharedProg
	callerFile.Program = callerProg
	sharedComponents := CollectSharedComponents([]PackageFile{sharedFile})

	sharedGo, err := Transpile(sharedSource, Options{SourceFile: sharedFile.Path})
	if err != nil {
		t.Fatalf("Transpile shared: %v", err)
	}
	callerGo, err := Transpile(callerSource, Options{
		SourceFile:       callerFile.Path,
		strictProjection: true,
		SharedImports: map[string]SharedImport{
			"./ui": {GoImportPath: "example.test/render/app/ui", Components: sharedComponents},
		},
	})
	if err != nil {
		t.Fatalf("Transpile caller: %v", err)
	}
	if strings.Contains(callerGo, `Attr("slot"`) {
		t.Fatalf("slot routing attribute leaked into generated component call:\n%s", callerGo)
	}

	module := newActualGoSXRenderModule(t)
	mustWriteGoFile(t, filepath.Join(module, "app", "ui", "frame.go"), sharedGo)
	mustWriteGoFile(t, filepath.Join(module, "app", "page.go"), callerGo)
	mustWriteGoFile(t, filepath.Join(module, "app", "page_render_test.go"), "package app_test\n\nimport (\n\t\"testing\"\n\n\t\"example.test/render/app\"\n\t\"m31labs.dev/gosx\"\n)\n\nfunc TestGeneratedPageRender(t *testing.T) {\n\tgot := gosx.RenderHTML(app.Page())\n\twant := `<section id=\"home\"><header><span class=\"badge\">badge</span></header><aside><strong>aside</strong></aside><footer></footer><main><div class=\"shell\"><p>body</p></div></main></section>`\n\tif got != want {\n\t\tt.Fatalf(\"generated html = %q, want %q\", got, want)\n\t}\n}\n")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "./app", "-count=1")
	cmd.Dir = module
	cmd.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false -mod=mod", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated render go test failed: %v\n%s\n--- ui/frame.go ---\n%s\n--- app/page.go ---\n%s", err, out, sharedGo, callerGo)
	}

	routerRoot := t.TempDir()
	mustWriteGoFile(t, filepath.Join(routerRoot, "ui", "frame.gsx"), string(sharedSource))
	mustWriteGoFile(t, filepath.Join(routerRoot, "page.gsx"), string(callerSource))
	node, err := route.DefaultFileRenderer(nil, route.FilePage{FilePath: filepath.Join(routerRoot, "page.gsx"), Pattern: "/"})
	if err != nil {
		t.Fatalf("DefaultFileRenderer: %v", err)
	}
	want := `<section id="home"><header><span class="badge">badge</span></header><aside><strong>aside</strong></aside><footer></footer><main><div class="shell"><p>body</p></div></main></section>`
	if got := gosx.RenderHTML(node); got != want {
		t.Fatalf("file-router html = %q, want %q", got, want)
	}
}

func TestSharedExpressionWrappedSlotChildRendersLikeFileRouter(t *testing.T) {
	sharedSource := []byte(`package ui

component Frame() {
	return <section><header>{slotTitle}</header><aside>{slotAside}</aside><main>{children}</main></section>
}
`)
	callerSource := []byte(`package app

import ui "./ui"

component Page() {
	return <ui.Frame>
		{<h1 slot="Title">Title</h1>}
		<p>body</p>
		{<strong slot="Aside">aside</strong>}
	</ui.Frame>
}
`)
	sharedProg, err := gosx.Compile(sharedSource)
	if err != nil {
		t.Fatalf("compile shared source: %v", err)
	}
	sharedComponents := CollectSharedComponents([]PackageFile{{Path: "app/ui/frame.gsx", Source: sharedSource, Program: sharedProg}})
	sharedGo, err := Transpile(sharedSource, Options{SourceFile: "app/ui/frame.gsx"})
	if err != nil {
		t.Fatalf("Transpile shared: %v", err)
	}
	callerGo, err := Transpile(callerSource, Options{
		SourceFile:       "app/page.gsx",
		strictProjection: true,
		SharedImports: map[string]SharedImport{
			"./ui": {GoImportPath: "example.test/render/app/ui", Components: sharedComponents},
		},
	})
	if err != nil {
		t.Fatalf("Transpile caller: %v", err)
	}
	if strings.Contains(callerGo, `Attr("slot"`) {
		t.Fatalf("slot routing attribute leaked into expression-wrapped shared generated Go:\n%s", callerGo)
	}
	want := `<section><header><h1>Title</h1></header><aside><strong>aside</strong></aside><main><p>body</p></main></section>`
	module := newActualGoSXRenderModule(t)
	mustWriteGoFile(t, filepath.Join(module, "app", "ui", "frame.go"), sharedGo)
	mustWriteGoFile(t, filepath.Join(module, "app", "page.go"), callerGo)
	runGeneratedRenderTest(t, module, want, sharedGo, callerGo)

	routerRoot := t.TempDir()
	mustWriteGoFile(t, filepath.Join(routerRoot, "ui", "frame.gsx"), string(sharedSource))
	mustWriteGoFile(t, filepath.Join(routerRoot, "page.gsx"), string(callerSource))
	node, err := route.DefaultFileRenderer(nil, route.FilePage{FilePath: filepath.Join(routerRoot, "page.gsx"), Pattern: "/"})
	if err != nil {
		t.Fatalf("DefaultFileRenderer: %v", err)
	}
	if got := gosx.RenderHTML(node); got != want {
		t.Fatalf("file-router html = %q, want %q", got, want)
	}
}

func TestSameFileIndentedSlotAndBodyRendersLikeFileRouter(t *testing.T) {
	source := []byte(`package app

component Frame() {
	return <section><header>{slotTitle}</header><aside>{slotAside}</aside><main>{children}</main></section>
}

component Page() {
	return <Frame>
		{<h1 slot="Title">Title</h1>}
		<p>body</p>
		{<strong slot="Aside">aside</strong>}
	</Frame>
}
`)
	generated, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if strings.Contains(generated, `Attr("slot"`) {
		t.Fatalf("slot routing attribute leaked into generated Go:\n%s", generated)
	}
	want := `<section><header><h1>Title</h1></header><aside><strong>aside</strong></aside><main><p>body</p></main></section>`
	assertGeneratedAppAndFileRouterHTML(t, generated, source, want)
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

func newActualGoSXRenderModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	mustWriteGoFile(t, filepath.Join(dir, "go.mod"),
		"module example.test/render\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\nreplace m31labs.dev/gosx => "+filepath.ToSlash(root)+"\n")
	if sum, err := os.ReadFile(filepath.Join(root, "go.sum")); err == nil {
		mustWriteGoFile(t, filepath.Join(dir, "go.sum"), string(sum))
	}
	return dir
}

func assertGeneratedAppAndFileRouterHTML(t *testing.T, generated string, source []byte, want string) {
	t.Helper()
	module := newActualGoSXRenderModule(t)
	mustWriteGoFile(t, filepath.Join(module, "app", "page.go"), generated)
	runGeneratedRenderTest(t, module, want, "", generated)

	routerRoot := t.TempDir()
	mustWriteGoFile(t, filepath.Join(routerRoot, "page.gsx"), string(source))
	node, err := route.DefaultFileRenderer(nil, route.FilePage{FilePath: filepath.Join(routerRoot, "page.gsx"), Pattern: "/"})
	if err != nil {
		t.Fatalf("DefaultFileRenderer: %v", err)
	}
	if got := gosx.RenderHTML(node); got != want {
		t.Fatalf("file-router html = %q, want %q", got, want)
	}
}

func runGeneratedRenderTest(t *testing.T, module, want, sharedGo, callerGo string) {
	t.Helper()
	mustWriteGoFile(t, filepath.Join(module, "app", "page_render_test.go"), "package app_test\n\nimport (\n\t\"testing\"\n\n\t\"example.test/render/app\"\n\t\"m31labs.dev/gosx\"\n)\n\nfunc TestGeneratedPageRender(t *testing.T) {\n\tgot := gosx.RenderHTML(app.Page())\n\twant := `"+want+"`\n\tif got != want {\n\t\tt.Fatalf(\"generated html = %q, want %q\", got, want)\n\t}\n}\n")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "./app", "-count=1")
	cmd.Dir = module
	cmd.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false -mod=mod", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated render go test failed: %v\n%s\n--- shared.go ---\n%s\n--- app/page.go ---\n%s", err, out, sharedGo, callerGo)
	}
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
