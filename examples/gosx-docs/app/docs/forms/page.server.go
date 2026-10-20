package docs

import (
	"strings"

	"m31labs.dev/gosx/action"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

type subscribeData struct {
	Form route.FormState
}

func init() {
	docsapp.RegisterDocsPage("Forms", "Server-side form handling with validation, CSRF protection, and flash messages.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"subscribe":   subscribeData{Form: ctx.FormState("subscribe")},
				"sample001":   docsapp.DocSample("forms/code-001.gsx.sample"),
				"sample002":   docsapp.DocSample("forms/code-002.go.sample"),
				"sample003":   docsapp.DocSample("forms/code-003.go.sample"),
				"sample004":   docsapp.DocSample("forms/code-004.gsx.sample"),
				"sample005":   docsapp.DocSample("forms/code-005.gsx.sample"),
				"sample006":   docsapp.DocSample("forms/code-006.go.sample"),
				"sample007":   docsapp.DocSample("forms/code-007.go.sample"),
				"sample008":   docsapp.DocSample("forms/code-008.gsx.sample"),
				"sample009":   docsapp.DocSample("forms/code-009.go.sample"),
				"sample010":   docsapp.DocSample("forms/code-010-form.gsx.sample"),
				"sample011":   docsapp.DocSample("forms/code-010-action.go.sample"),
				"sample012":   docsapp.DocSample("forms/code-011.go.sample"),
				"sample013":   docsapp.DocSample("forms/code-012.gsx.sample"),
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
		Actions: route.FileActions{
			"subscribe": func(ctx *action.Context) error {
				email := ctx.FormData["email"]
				if email == "" || !strings.Contains(email, "@") {
					ctx.ValidationFailure("Please enter a valid email.", map[string]string{
						"email": "A valid email address is required.",
					})
					return nil
				}
				session.AddFlash(ctx.Request, "notice", "Subscription saved.")
				return ctx.Success("Subscribed!", nil)
			},
		},
	})
}
