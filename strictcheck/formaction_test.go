package strictcheck

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
)

func formPageFixture(formTag string) string {
	return "package main\n\nfunc Page() Node {\n\treturn " + formTag + "\n}\n"
}

func registeredActionFixture(name string) string {
	return `package main

import "m31labs.dev/gosx/route"

func init() {
	route.RegisterFileModuleHere(route.FileModuleOptions{
		Actions: route.FileActions{
			"` + name + `": nil,
		},
	})
}
`
}

// --- Accepts -----------------------------------------------------------

func TestFormActionContractAcceptsRegisteredAction(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(
		`<form method="post" action={actionPath("createUser")}><input type="hidden" name="csrf_token" value={csrf.token}></input><button type="submit">Go</button></form>`))
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

// TestFormActionContractAcceptsStaticCSRFTokenInGridironShape is the positive
// half of the Gridiron regression: static wrappers do not hide a descendant
// token from the check.
func TestFormActionContractAcceptsStaticCSRFTokenInGridironShape(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(
		`<form class="claim-team" method="post" action={actionPath("claimTeam")}><div class="claim-team__fields"><input type="hidden" name="csrf_token" value={csrf.token}></input><button type="submit">Claim team</button></div></form>`))
	mustWrite(t, filepath.Join(dir, "page.server.go"), registeredActionFixture("claimTeam"))
	if err := CheckFile(context.Background(), filepath.Join(dir, "page.gsx")); err != nil {
		t.Fatalf("expected a statically visible csrf_token control to be accepted, got %v", err)
	}
}

// TestFormActionContractRejectsGridironClaimFormWithoutCSRF is the exact
// failure shape that motivated this check: a mutating actionPath form has a
// real button, but no descendant control that session.Manager.Protect can
// read from the submitted body.
func TestFormActionContractRejectsGridironClaimFormWithoutCSRF(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(
		`<form class="claim-team" method="post" action={actionPath("claimTeam")}><div class="claim-team__fields"><button type="submit">Claim team</button></div></form>`))
	mustWrite(t, filepath.Join(dir, "page.server.go"), registeredActionFixture("claimTeam"))
	err := CheckFile(context.Background(), filepath.Join(dir, "page.gsx"))
	if err == nil {
		t.Fatal("expected a mutating Gridiron-shaped form without csrf_token to be rejected")
	}
	if !strings.Contains(err.Error(), `actionPath("claimTeam")`) || !strings.Contains(err.Error(), `csrf_token`) {
		t.Fatalf("expected the diagnostic to name claimTeam and csrf_token, got: %v", err)
	}
}

func TestFormActionContractPreservesGETActionPathForms(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(
		`<form method="get" action={actionPath("search")}><input name="query"></input></form>`))
	mustWrite(t, filepath.Join(dir, "page.server.go"), registeredActionFixture("search"))
	if err := CheckFile(context.Background(), filepath.Join(dir, "page.gsx")); err != nil {
		t.Fatalf("expected GET actionPath form without csrf_token to remain valid, got %v", err)
	}
}

func TestFormActionContractPreservesExternalNativeForms(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(
		`<form method="post" action="https://payments.example.test/charge"><button type="submit">Pay</button></form>`))
	if err := CheckFile(context.Background(), filepath.Join(dir, "page.gsx")); err != nil {
		t.Fatalf("expected an external/native form to remain out of scope, got %v", err)
	}
}

// Browsers use the first duplicate direct form attribute. Keep the CSRF
// classifier aligned with that effective value so a later duplicate cannot
// disguise a mutating file action or turn a native/GET form into a false
// positive.
func TestFormActionContractUsesFirstDuplicateDirectAttributes(t *testing.T) {
	tests := []struct {
		name    string
		form    string
		wantErr bool
	}{
		{
			name:    "later get does not disguise post",
			form:    `<form method="post" method="get" action={actionPath("save")}></form>`,
			wantErr: true,
		},
		{
			name:    "later native action does not disguise file action",
			form:    `<form method="post" action={actionPath("save")} action="/native"></form>`,
			wantErr: true,
		},
		{
			name: "first get remains nonmutating",
			form: `<form method="get" method="post" action={actionPath("save")}></form>`,
		},
		{
			name: "first native action remains outside file action contract",
			form: `<form method="post" action="/native" action={actionPath("save")}></form>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := newTestModule(t)
			mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(tt.form))
			mustWrite(t, filepath.Join(dir, "page.server.go"), registeredActionFixture("save"))
			err := CheckFile(context.Background(), filepath.Join(dir, "page.gsx"))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected the effective mutating file action without csrf_token to be rejected")
				}
				if !strings.Contains(err.Error(), `actionPath("save")`) || !strings.Contains(err.Error(), "csrf_token") {
					t.Fatalf("expected the CSRF diagnostic for save, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected the first effective attribute to keep this form outside the CSRF check, got: %v", err)
			}
		})
	}
}

func TestFormActionContractRejectsEveryUnsafeMethodWithoutCSRF(t *testing.T) {
	for _, method := range []string{"post", "put", "patch", "delete"} {
		t.Run(method, func(t *testing.T) {
			dir := newTestModule(t)
			form := `<form method="` + method + `" action={actionPath("save")}></form>`
			mustWrite(t, filepath.Join(dir, "page.gsx"), formPageFixture(form))
			mustWrite(t, filepath.Join(dir, "page.server.go"), registeredActionFixture("save"))
			err := CheckFile(context.Background(), filepath.Join(dir, "page.gsx"))
			if err == nil || !strings.Contains(err.Error(), "csrf_token") {
				t.Fatalf("expected %s file action without csrf_token to be rejected, got: %v", method, err)
			}
		})
	}
}

// TestFormActionContractAbstainsOnUnknownNestedAndDynamicDescendants proves
// that a component call and an expression hole are boundaries: either could
// render a csrf_token control at runtime, so the check must not guess that it
// is missing.
func TestFormActionContractAbstainsOnUnknownNestedAndDynamicDescendants(t *testing.T) {
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "page.gsx"), `package main

func Page() Node {
	return <section>
		<form method="post" action={actionPath("nestedFields")}><ClaimFields /></form>
		<form method="post" action={actionPath("dynamicFields")}>{children}</form>
	</section>
}

func ClaimFields() Node {
	return <div></div>
}
`)
	mustWrite(t, filepath.Join(dir, "page.server.go"), `package main

import "m31labs.dev/gosx/route"

func init() {
	route.RegisterFileModuleHere(route.FileModuleOptions{
		Actions: route.FileActions{
			"nestedFields":  nil,
			"dynamicFields": nil,
		},
	})
}
`)
	var warnings []ir.Diagnostic
	if err := CheckFileWithOptions(context.Background(), filepath.Join(dir, "page.gsx"), Options{Warnings: &warnings}); err != nil {
		t.Fatalf("expected unknown nested/dynamic descendants to abstain, got %v", err)
	}
	if len(warnings) != 2 {
		t.Fatalf("expected one safe CSRF warning per unknown descendant boundary, got %d: %+v", len(warnings), warnings)
	}
	for _, warning := range warnings {
		if warning.Severity != ir.SeverityWarning || !strings.Contains(warning.Message, "csrf_token") {
			t.Fatalf("expected csrf warning diagnostics, got %+v", warnings)
		}
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
		`<form method="post" action={actionPath("createUser")}><input type="hidden" name="csrf_token" value={csrf.token}></input></form>`))
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
		`<form method="post" action={actionPath("signIn")}><input type="hidden" name="csrf_token" value={csrf.token}></input></form>`))
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
