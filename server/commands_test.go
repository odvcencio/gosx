package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/command"
)

func TestCommandsOnlyPagePublishesManifestAndWorkbenchContract(t *testing.T) {
	app := New()
	app.Page("GET /commands", func(ctx *Context) gosx.Node {
		if err := ctx.Runtime().Commands(command.Command{ID: "edit.undo", Title: "Undo", Keys: []string{"Mod+Z"}, Action: command.Action{Signal: "$edit", Value: "undo"}}); err != nil {
			t.Error(err)
		}
		return gosx.Text("Commands")
	})
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest("GET", "/commands", nil))
	body := w.Body.String()
	for _, want := range []string{`"commands":[{"id":"edit.undo"`, `"features":["workbench"]`, `"bootstrapFeatureWorkbenchPath":"/gosx/bootstrap-feature-workbench.js"`, `bootstrap-runtime.js`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %s: %s", want, body)
		}
	}
	for _, unwanted := range []string{`src="/gosx/bootstrap.js`, `bootstrap-feature-controllers.js`, `runtime.wasm`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("commands-only page includes %s", unwanted)
		}
	}
}

func TestCommandsRegistrationRejectsInvalidAndDuplicateCommands(t *testing.T) {
	r := NewPageRuntime()
	if err := r.Commands(command.Command{ID: "x", Title: "X"}); err == nil || r.Active() {
		t.Fatal("invalid commands activated the runtime")
	}
	cmd := command.Command{ID: "help", Title: "Help", Reserved: true}
	if err := r.Commands(cmd); err != nil {
		t.Fatal(err)
	}
	if err := r.Commands(cmd); err == nil {
		t.Fatal("duplicate accepted")
	}
	var nilRuntime *PageRuntime
	if err := nilRuntime.Commands(cmd); err != nil {
		t.Fatal(err)
	}
}
