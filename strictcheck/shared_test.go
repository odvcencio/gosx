package strictcheck

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// sharedTeamMarkFixture writes the shared components design's own headline
// example (a strict ui.TeamMark declared in app/ui, called from a strict
// app/page.gsx) into a fresh test module, and returns the module root and
// page.gsx's path. Page itself stays zero-props — a strict route render
// entry may not bind root props (validateStrictRenderEntries) — so the
// shared call is exercised with literal attribute values, same as this
// package's other same-file fixtures (see strictFixture).
func sharedTeamMarkFixture(t *testing.T) (root, page string) {
	t.Helper()
	root = newTestModule(t)
	mustWrite(t, filepath.Join(root, "app", "ui", "team_mark.gsx"), `package ui

type TeamMarkProps struct {
	Tone         string
	Abbreviation string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"team-mark tone-" + props.Tone}>{props.Abbreviation}</span>
}
`)
	page = filepath.Join(root, "app", "page.gsx")
	mustWrite(t, page, `package main

import ui "./ui"

component Page() {
	return <div><ui.TeamMark Tone="home" Abbreviation="NE"/></div>
}
`)
	return root, page
}

// TestCheckFileAcceptsSharedComponentCall is the check-seam proof: a strict
// caller's shared call — blocked before this change by ir.Lower's
// same-file gate — now clears gosx check end to end, through the real Go
// compiler, exactly as a same-file call does.
func TestCheckFileAcceptsSharedComponentCall(t *testing.T) {
	_, page := sharedTeamMarkFixture(t)
	if err := CheckFile(context.Background(), page); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

// TestCheckFileRejectsSharedComponentFieldTypo proves the check seam's own
// contribution: a field the shared target does not declare fails through
// the real Go compiler, not through a silently-accepted call.
func TestCheckFileRejectsSharedComponentFieldTypo(t *testing.T) {
	root := newTestModule(t)
	mustWrite(t, filepath.Join(root, "app", "ui", "team_mark.gsx"), `package ui

type TeamMarkProps struct {
	Tone string
}

component TeamMark(props: TeamMarkProps) {
	return <span class={"team-mark tone-" + props.Tone}>{props.Tone}</span>
}
`)
	page := filepath.Join(root, "app", "page.gsx")
	mustWrite(t, page, `package main

import ui "./ui"

component Page() {
	return <div><ui.TeamMark Toneo="home"/></div>
}
`)
	err := CheckFile(context.Background(), page)
	if err == nil {
		t.Fatalf("CheckFile succeeded for an unknown field on a shared component")
	}
}

// TestCheckFileRejectsSharedImportOutsideAppRoot covers the explicit build
// constraint: gosx build stages only app/, content/, and public/, so a
// shared import that resolves outside app/ must fail check with a clear
// diagnostic instead of shipping a page that 404s on first render.
func TestCheckFileRejectsSharedImportOutsideAppRoot(t *testing.T) {
	root := newTestModule(t)
	mustWrite(t, filepath.Join(root, "sharedlib", "widget.gsx"), `package sharedlib

type WidgetProps struct {
	Label string
}

component Widget(props: WidgetProps) {
	return <span>{props.Label}</span>
}
`)
	page := filepath.Join(root, "app", "page.gsx")
	mustWrite(t, page, `package main

import sharedlib "../sharedlib"

component Page() {
	return <div><sharedlib.Widget Label="hi"/></div>
}
`)
	err := CheckFile(context.Background(), page)
	if err == nil {
		t.Fatalf("CheckFile succeeded for a shared import outside app/")
	}
	if !strings.Contains(err.Error(), "outside") || !strings.Contains(err.Error(), "app") {
		t.Fatalf("error = %v, want a diagnostic naming the app/ boundary", err)
	}
}

// TestCheckFileAcceptsSharedComponentCallWithChildren covers the PR #246
// contract: a shared call carrying children must clear check the same way
// a same-file call does.
func TestCheckFileAcceptsSharedComponentCallWithChildren(t *testing.T) {
	root := newTestModule(t)
	mustWrite(t, filepath.Join(root, "app", "ui", "panel.gsx"), `package ui

type PanelProps struct {
	Title string
}

component Panel(props: PanelProps) {
	return <section><h2>{props.Title}</h2><div>{children}</div></section>
}
`)
	page := filepath.Join(root, "app", "page.gsx")
	mustWrite(t, page, `package main

import ui "./ui"

component Page() {
	return <ui.Panel Title="Heading"><p>body</p></ui.Panel>
}
`)
	if err := CheckFile(context.Background(), page); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

func TestCheckFileAcceptsSharedComponentCallWithNamedSlots(t *testing.T) {
	root := newTestModule(t)
	mustWrite(t, filepath.Join(root, "app", "ui", "layout.gsx"), `package ui

type LayoutProps struct {
	ID string
}

component Layout(props: LayoutProps) {
	return <section id={props.ID}>{slotTitle}<nav>{slotNav}</nav>{children}</section>
}
`)
	page := filepath.Join(root, "app", "page.gsx")
	mustWrite(t, page, `package main

import ui "./ui"

component Page() {
	return <ui.Layout ID="home"><p>body</p><h1 slot="Title">Title</h1></ui.Layout>
}
`)
	if err := CheckFile(context.Background(), page); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

func TestCheckFileRejectsSharedComponentChildrenWhenUnsupported(t *testing.T) {
	root := newTestModule(t)
	mustWrite(t, filepath.Join(root, "app", "ui", "team_mark.gsx"), `package ui

type TeamMarkProps struct {
	Tone string
}

component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}
`)
	page := filepath.Join(root, "app", "page.gsx")
	mustWrite(t, page, `package main

import ui "./ui"

component Page() {
	return <ui.TeamMark Tone="home"><b>lost</b></ui.TeamMark>
}
`)
	err := CheckFile(context.Background(), page)
	if err == nil || !strings.Contains(err.Error(), "renders no children") {
		t.Fatalf("error = %v, want shared children rejection", err)
	}
}

func TestCheckFileRejectsUnknownSharedComponentSlot(t *testing.T) {
	root := newTestModule(t)
	mustWrite(t, filepath.Join(root, "app", "ui", "team_mark.gsx"), `package ui

type TeamMarkProps struct {
	Tone string
}

component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}
`)
	page := filepath.Join(root, "app", "page.gsx")
	mustWrite(t, page, `package main

import ui "./ui"

component Page() {
	return <ui.TeamMark Tone="home"><h1 slot="Title">lost</h1></ui.TeamMark>
}
`)
	err := CheckFile(context.Background(), page)
	if err == nil || !strings.Contains(err.Error(), `declares no slot named "Title"`) {
		t.Fatalf("error = %v, want shared unknown-slot rejection", err)
	}
}

func TestCheckFileAcceptsSharedSiblingFilePropsShape(t *testing.T) {
	root := newTestModule(t)
	mustWrite(t, filepath.Join(root, "app", "ui", "types.gsx"), `package ui

type CardProps struct {
	Label string
}
`)
	mustWrite(t, filepath.Join(root, "app", "ui", "card.gsx"), `package ui

component Card(props: CardProps) {
	return <article>card</article>
}
`)
	page := filepath.Join(root, "app", "page.gsx")
	mustWrite(t, page, `package main

import ui "./ui"

component Page() {
	return <ui.Card label="Visible" />
}
`)
	if err := CheckFile(context.Background(), page); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

func TestCheckFileRejectsCalledSharedIslandTarget(t *testing.T) {
	root := newTestModule(t)
	mustWrite(t, filepath.Join(root, "app", "ui", "counter.gsx"), `package ui

//gosx:island
component Counter() {
	return <button>count</button>
}
`)
	page := filepath.Join(root, "app", "page.gsx")
	mustWrite(t, page, `package main

import ui "./ui"

component Page() {
	return <ui.Counter />
}
`)
	err := CheckFile(context.Background(), page)
	if err == nil || !strings.Contains(err.Error(), "cannot be projected") || !strings.Contains(err.Error(), "gosx check") {
		t.Fatalf("error = %v, want called island target excluded from shared pure-view projection", err)
	}
}

func TestCheckFileIgnoresUnusedSharedIslandNeighbor(t *testing.T) {
	root := newTestModule(t)
	mustWrite(t, filepath.Join(root, "app", "ui", "frame.gsx"), `package ui

component Frame() {
	return <section>{children}</section>
}
`)
	mustWrite(t, filepath.Join(root, "app", "ui", "counter.gsx"), `package ui

//gosx:island
component Counter() {
	return <button>count</button>
}
`)
	page := filepath.Join(root, "app", "page.gsx")
	mustWrite(t, page, `package main

import ui "./ui"

component Page() {
	return <ui.Frame><p>body</p></ui.Frame>
}
`)
	if err := CheckFile(context.Background(), page); err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
}

func TestCheckFileRejectsDuplicateSharedComponentNames(t *testing.T) {
	root := newTestModule(t)
	mustWrite(t, filepath.Join(root, "app", "ui", "a.gsx"), `package ui

component Badge() {
	return <span>a</span>
}
`)
	mustWrite(t, filepath.Join(root, "app", "ui", "b.gsx"), `package ui

component Badge() {
	return <span>b</span>
}
`)
	page := filepath.Join(root, "app", "page.gsx")
	mustWrite(t, page, `package main

import ui "./ui"

component Page() {
	return <ui.Badge />
}
`)
	err := CheckFile(context.Background(), page)
	if err == nil {
		t.Fatal("CheckFile succeeded for duplicate shared component names")
	}
	if !strings.Contains(err.Error(), "Badge redeclared") && !strings.Contains(err.Error(), "cannot be projected") {
		t.Fatalf("error = %v, want Go redeclaration or unresolved shared projection diagnostic", err)
	}
}

func TestCheckFileRejectsPureIslandDuplicateSharedComponentNamesBothOrders(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"pure first": {
			"a.gsx": `package ui

component Badge() {
	return <span>pure</span>
}
`,
			"b.gsx": `package ui

//gosx:island
component Badge() {
	return <span>island</span>
}
`,
		},
		"island first": {
			"a.gsx": `package ui

//gosx:island
component Badge() {
	return <span>island</span>
}
`,
			"b.gsx": `package ui

component Badge() {
	return <span>pure</span>
}
`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := newTestModule(t)
			for filename, source := range files {
				mustWrite(t, filepath.Join(root, "app", "ui", filename), source)
			}
			page := filepath.Join(root, "app", "page.gsx")
			mustWrite(t, page, `package main

import ui "./ui"

component Page() {
	return <ui.Badge />
}
`)
			err := CheckFile(context.Background(), page)
			if err == nil {
				t.Fatal("CheckFile succeeded for mixed pure/island duplicate shared component names")
			}
			if !strings.Contains(err.Error(), "Badge redeclared") && !strings.Contains(err.Error(), "cannot be projected") {
				t.Fatalf("error = %v, want Go redeclaration or unresolved shared projection diagnostic", err)
			}
		})
	}
}
