// Go-only managed-form fixture with real actions and session/CSRF protection.
package main

import (
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

func main() {
	_, file, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(file)
	sessions := session.MustNew("gosx-action-feedback-e2e-fixture-secret", session.Options{AllowInsecure: true})
	actions := action.NewRegistry()
	actions.Register("save", func(ctx *action.Context) error {
		switch strings.TrimSpace(ctx.FormData["title"]) {
		case "":
			ctx.ValidationFailure("Title is required.", map[string]string{"title": "Enter a title."})
		case "conflict":
			ctx.ValidationFailure("Revision changed.", map[string]string{"title": "Reload the current revision."})
			ctx.SetStatus(http.StatusConflict)
		case "destination":
			ctx.RedirectWithMessage("/?destination=error", "Saved; review the destination.")
		case "inline":
			return ctx.Success("Saved inline.", nil)
		default:
			ctx.RedirectWithMessage("/?saved=1", "Saved.")
		}
		return nil
	})

	app := server.New()
	app.SetRuntimeRoot(root)
	app.EnableNavigation()
	app.Use(sessions.Middleware)
	app.Use(sessions.Protect)
	app.Mount("POST /__actions/{name}", actions)
	app.Page("GET /", func(ctx *server.Context) gosx.Node {
		// A form is an explicit session write boundary; anonymous reads alone
		// do not create a cookie or CSRF token.
		session.Current(ctx.Request).Set("form-feedback-fixture", true)
		state, message := "idle", ""
		if ctx.Request.URL.Query().Get("destination") == "error" {
			state, message = "error", "Destination needs attention."
		}
		return gosx.El("main",
			gosx.El("h1", gosx.Text("Managed form feedback")),
			gosx.El("form",
				gosx.ManagedFormAttrs(gosx.ManagedFormOptions{State: state, Layer: "bootstrap", Fallback: "native-form"}),
				gosx.Attrs(gosx.Attr("id", "save-form"), gosx.Attr("method", "post"), gosx.Attr("action", "/__actions/save")),
				gosx.El("input", gosx.Attrs(gosx.Attr("type", "hidden"), gosx.Attr("name", "csrf_token"), gosx.Attr("value", session.Token(ctx.Request)))),
				gosx.El("label", gosx.Attrs(gosx.Attr("for", "title")), gosx.Text("Title")),
				gosx.El("input", gosx.Attrs(gosx.Attr("id", "title"), gosx.Attr("name", "title"), gosx.Attr("aria-describedby", "title-error"))),
				gosx.El("p", gosx.Attrs(gosx.Attr("id", "title-error"), gosx.Attr("class", "form-error"), gosx.Attr("role", "alert"))),
				gosx.El("p", gosx.Attrs(gosx.Attr("class", "form-status"), gosx.Attr("role", "status")), gosx.Text(message)),
				gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit")), gosx.Text("Save")),
			),
		)
	})
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(app.ListenAndServe(":" + port))
}
