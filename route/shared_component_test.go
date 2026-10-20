package route

import (
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// TestDefaultFileRendererExecutesSharedComponentCall is the render-seam
// proof: the shared components design's own headline example (import ui
// "./ui"; <ui.TeamMark Tone={props.Tone}/>) reached from a strict caller,
// asserted on RENDERED OUTPUT BYTES — not merely a successful compile. A
// fixture in this repo once compiled a broken pattern without rendering it
// and thereby certified the exact pattern that took a production app down
// twice; asserting only "renders without error" would repeat that mistake.
func TestDefaultFileRendererExecutesSharedComponentCall(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "ui/team_mark.gsx", `package app

type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"team-mark tone-" + props.Tone}>{props.Abbreviation}</span>
}
`)
	writeRouteFile(t, root, "page.gsx", `package app

import ui "./ui"

component Page() {
	return <div><ui.TeamMark Tone="home" Abbreviation="NE"/></div>
}
`)

	node, err := DefaultFileRenderer(nil, FilePage{FilePath: filepath.Join(root, "page.gsx"), Pattern: "/"})
	if err != nil {
		t.Fatalf("DefaultFileRenderer: %v", err)
	}
	html := gosx.RenderHTML(node)
	want := `<div><span class="team-mark tone-home">NE</span></div>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestDefaultFileRendererExecutesSharedComponentCallWithChildren covers the
// PR #246 contract (writeSharedComponent renders children on the parent
// renderer, then calls writeLocalComponentWithChildren on a renderer bound
// to the shared target's own ir.Program) with rendered output bytes: the
// caller's <p> markup must appear inside the shared Panel's own <section>,
// proving the two-renderer split actually threads the finished node across.
func TestDefaultFileRendererExecutesSharedComponentCallWithChildren(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "ui/panel.gsx", `package app

type PanelProps struct {
	Title string
}

component Panel(props: PanelProps) {
	return <section><h2 class="panel__title">{props.Title}</h2><div class="panel__body">{children}</div></section>
}
`)
	writeRouteFile(t, root, "page.gsx", `package app

import ui "./ui"

component Page() {
	return <ui.Panel Title="Standings"><p>caller markup</p></ui.Panel>
}
`)

	node, err := DefaultFileRenderer(nil, FilePage{FilePath: filepath.Join(root, "page.gsx"), Pattern: "/"})
	if err != nil {
		t.Fatalf("DefaultFileRenderer: %v", err)
	}
	html := gosx.RenderHTML(node)
	want := `<section><h2 class="panel__title">Standings</h2><div class="panel__body"><p>caller markup</p></div></section>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestDefaultFileRendererRejectsSharedComponentChildrenWhenUnsupported
// proves the render seam's own fail-closed guarantee: a shared callee that
// does not place {children} refuses child content instead of silently
// dropping it — ir.Lower cannot prove this for a shared call (no I/O), and
// the Go compiler cannot catch it either (every strict component projects
// a variadic children parameter that accepts any number of arguments), so
// this is the one point that ever proves it.
func TestDefaultFileRendererRejectsSharedComponentChildrenWhenUnsupported(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "ui/badge.gsx", `package app

type BadgeProps struct {
	Label string
}

component Badge(props: BadgeProps) {
	return <span>{props.Label}</span>
}
`)
	writeRouteFile(t, root, "page.gsx", `package app

import ui "./ui"

component Page() {
	return <ui.Badge Label="new"><b>unexpected</b></ui.Badge>
}
`)

	_, err := DefaultFileRenderer(nil, FilePage{FilePath: filepath.Join(root, "page.gsx"), Pattern: "/"})
	if err == nil {
		t.Fatalf("DefaultFileRenderer succeeded for children at a shared callee that renders none")
	}
	if !strings.Contains(err.Error(), "renders no children") {
		t.Fatalf("error = %v, want a message naming the renders-no-children rule", err)
	}
}

// TestDefaultFileRendererRejectsUnknownSharedComponent proves a shared call
// naming a component the target directory does not declare fails clearly
// at render time rather than falling back to the unresolved-tag
// placeholder markup (defaultRenderedComponent).
func TestDefaultFileRendererRejectsUnknownSharedComponent(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "ui/team_mark.gsx", `package app

type TeamMarkProps struct {
	Tone string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"team-mark tone-" + props.Tone}></span>
}
`)
	writeRouteFile(t, root, "page.gsx", `package app

import ui "./ui"

component Page() {
	return <div><ui.Missing/></div>
}
`)

	_, err := DefaultFileRenderer(nil, FilePage{FilePath: filepath.Join(root, "page.gsx"), Pattern: "/"})
	if err == nil {
		t.Fatalf("DefaultFileRenderer succeeded for a shared component the target directory does not declare")
	}
	if !strings.Contains(err.Error(), "declares no strict component named Missing") {
		t.Fatalf("error = %v, want a message naming the missing component", err)
	}
}
