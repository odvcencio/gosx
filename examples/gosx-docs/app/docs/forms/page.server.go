package docs

import (
	"strings"

	"github.com/odvcencio/gosx/action"
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/session"
)

func init() {
	docsapp.RegisterDocsPage(
		"Forms",
		"Relative page actions, CSRF protection, validation state, and session-backed flashes now fit the file-routed app model.",
		route.FileModuleOptions{
			Actions: route.FileActions{
				"subscribe": func(ctx *action.Context) error {
					if strings.TrimSpace(ctx.FormData["email"]) == "" {
						return action.Validation("Add an email address to continue.", map[string]string{
							"email": "Email is required.",
						}, ctx.FormData)
					}
					session.AddFlash(ctx.Request, "notice", "The session flash survived a redirect-backed form post.")
					return ctx.Success("Validation state and success messages now survive a normal browser redirect.", nil)
				},
			},
		},
	)
}
