package docs

import (
	"fmt"
	"strings"

	"m31labs.dev/gosx/action"
	docs "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

func init() {
	docs.RegisterDocsPage("Forms", "Server-side form handling with validation, CSRF protection, and flash messages.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Forms",
				"description": "Server-side form handling with validation, CSRF protection, and flash messages.",
				"tags":        []string{"forms", "actions", "validation", "csrf"},
				"toc": []map[string]string{
					{"href": "#html-forms", "label": "HTML Forms"},
					{"href": "#server-actions", "label": "Server Actions"},
					{"href": "#validation", "label": "Validation"},
					{"href": "#csrf-protection", "label": "CSRF Protection"},
					{"href": "#flash-messages", "label": "Flash Messages"},
					{"href": "#redirects", "label": "Redirects"},
				},
			}, nil
		},
	})
}

// RegisterManagedActions installs the docs form endpoint before the shared
// route router is built. File modules render only; the aggregate installer in
// main owns the registration boundary.
func RegisterManagedActions(router *route.Router) error {
	if router == nil {
		return fmt.Errorf("managed action router is nil")
	}
	return router.RegisterManagedPOST("subscribe", action.Config{}, func(ctx *action.Context) (action.Result, error) {
		email := strings.TrimSpace(ctx.Form.Value("email"))
		if email == "" || !strings.Contains(email, "@") {
			return action.Result{}, action.Validation("Enter a valid email address.", map[string]string{"email": "A valid email address is required."})
		}
		if err := session.AddFlash(ctx.Request, "notice", "Thanks — your subscription was saved."); err != nil {
			return action.Result{}, err
		}
		return action.Result{OK: true, Redirect: "/docs/forms"}, nil
	})
}
