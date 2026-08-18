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
