package strictcheck

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func formPageFixture(formTag string) string {
	return "package main\n\nfunc Page() Node {\n\treturn " + formTag + "\n}\n"
}

// --- Accepts -----------------------------------------------------------

func TestFormActionContractAcceptsRegisteredAction(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(
		`<form method="post" action={actionPath("createUser")}><button type="submit">Go</button></form>`))
	mustWrite(t, filepath.Join(dir, "page.server.go"), `package main

import "m31labs.dev/gosx/route"

func init() {
	route.RegisterFileModuleHere(route.FileModuleOptions{
		Actions: route.FileActions{
			"createUser": nil,
		},
	})
}
`)
	if err := CheckFile(context.Background(), filepath.Join(dir, "page.gsx")); err != nil {
		t.Fatalf("expected a registered action to be accepted, got %v", err)
	}
}

func TestFormActionContractAcceptsNoActionsAtAllOnAStaticForm(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(`<form method="get" action="/search"></form>`))
	if err := CheckFile(context.Background(), filepath.Join(dir, "page.gsx")); err != nil {
		t.Fatalf("expected a plain static action to be out of scope, got %v", err)
	}
}

// TestFormActionContractAbstainsOnDynamicActionsConstruction proves the
// "only report with confidence" rule (gosx#249): a page.server.go that
// builds its Actions map through a helper call, not a literal, must not be
// flagged even though this scan cannot see "createUser" registered
// anywhere.
func TestFormActionContractAbstainsOnDynamicActionsConstruction(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(
		`<form method="post" action={actionPath("createUser")}></form>`))
	mustWrite(t, filepath.Join(dir, "page.server.go"), `package main

import "m31labs.dev/gosx/route"

func init() {
	route.RegisterFileModuleHere(route.FileModuleOptions{
		Actions: buildActions(),
	})
}

func buildActions() route.FileActions {
	return route.FileActions{"createUser": nil}
}
`)
	if err := CheckFile(context.Background(), filepath.Join(dir, "page.gsx")); err != nil {
		t.Fatalf("expected a dynamically built Actions map to abstain, got %v", err)
	}
}

// --- Rejects -------------------------------------------------------------

// TestFormActionContractRejectsUnregisteredAction reconstructs the
// gosx#249 premise-table defect: a form posts to an action name that is
// never registered anywhere (a guaranteed 404 the moment a real request
// reaches it), the examples/dashboard defect that shipped in this
// repository's own history.
func TestFormActionContractRejectsUnregisteredAction(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(
		`<form method="post" action={actionPath("signup-claim")}><button type="submit">Claim</button></form>`))
	// No page.server.go at all: nothing registers any action for this page.
	err := CheckFile(context.Background(), filepath.Join(dir, "page.gsx"))
	if err == nil {
		t.Fatal("expected an unregistered form action to be rejected")
	}
	if !strings.Contains(err.Error(), `actionPath("signup-claim")`) || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected the diagnostic to name the unregistered action, got: %v", err)
	}
}

// TestFormActionContractAbstainsOnUnrenderedServerGoTemplate reconstructs
// the gosx#249 scaffold defect: cmd/gosx/templates/docs ships
// "page.server.gotmpl" beside each "page.gsx" that needs one, never a
// real "page.server.go" -- `gosx init` renders the template into a real
// file only when scaffolding a new project. Before this test existed,
// this scan read "no page.server.go here" as "confidently registers
// nothing" and reported the framework's own scaffold as broken.
func TestFormActionContractAbstainsOnUnrenderedServerGoTemplate(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(
		`<form method="post" action={actionPath("signIn")}></form>`))
	mustWrite(t, filepath.Join(dir, "page.server.gotmpl"), `package main

import "m31labs.dev/gosx/route"

func init() {
	route.RegisterFileModuleHere(route.FileModuleOptions{
		Actions: route.FileActions{
			"signIn": nil,
		},
	})
}
`)
	if err := CheckFile(context.Background(), filepath.Join(dir, "page.gsx")); err != nil {
		t.Fatalf("expected a page.server.gotmpl sibling to abstain, got %v", err)
	}
}

func TestFormActionContractRejectsUnregisteredFormactionOnButton(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(
		`<button formaction={actionPath("signOut")}>Sign out</button>`))
	mustWrite(t, filepath.Join(dir, "page.server.go"), `package main

import "m31labs.dev/gosx/route"

func init() {
	route.RegisterFileModuleHere(route.FileModuleOptions{
		Actions: route.FileActions{
			"signIn": nil,
		},
	})
}
`)
	err := CheckFile(context.Background(), filepath.Join(dir, "page.gsx"))
	if err == nil {
		t.Fatal("expected an unregistered formaction to be rejected")
	}
	if !strings.Contains(err.Error(), `actionPath("signOut")`) {
		t.Fatalf("expected the diagnostic to name signOut, got: %v", err)
	}
}
