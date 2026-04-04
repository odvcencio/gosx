package docs

import (
	"strings"

	"github.com/odvcencio/gosx/action"
	docs "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
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
		Actions: route.FileActions{
			"subscribe": func(ctx *action.Context) error {
				email := ctx.FormData["email"]
				if email == "" || !strings.Contains(email, "@") {
					ctx.ValidationFailure("Please enter a valid email.", map[string]string{
						"email": "A valid email address is required.",
					})
					return nil
				}
				return ctx.Success("Subscribed!", nil)
			},
		},
	})
}
