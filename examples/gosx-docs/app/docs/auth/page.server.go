package docs

import (
	"strings"

	"github.com/odvcencio/gosx/action"
	"github.com/odvcencio/gosx/auth"
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/session"
)

func init() {
	docsapp.RegisterDocsPage(
		"Auth",
		"Magic links, passkeys, and session management built into the framework.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"mode":        "light",
					"title":       "Auth",
					"description": "Magic links, passkeys, and session management built into the framework.",
					"tags":        []string{"auth", "sessions", "magic-links", "passkeys"},
					"toc": []map[string]string{
						{"href": "#sessions", "label": "Sessions"},
						{"href": "#magic-links", "label": "Magic Links"},
						{"href": "#passkeys", "label": "WebAuthn / Passkeys"},
						{"href": "#protected-routes", "label": "Protected Routes"},
						{"href": "#csrf", "label": "CSRF"},
					},
					"authFlows": map[string]any{
						"magicLinkEnabled":        docsapp.MagicLinks() != nil,
						"magicLinkRequestPath":    "/auth/magic-link/request",
						"webauthnEnabled":         docsapp.WebAuthnManager() != nil,
						"webauthnRegisterOptions": "/auth/webauthn/register/options",
						"webauthnRegisterPath":    "/auth/webauthn/register",
						"webauthnLoginOptions":    "/auth/webauthn/login/options",
						"webauthnLoginPath":       "/auth/webauthn/login",
						"oauthProviders":          docsapp.OAuthProviders(),
					},
				}, nil
			},
			Actions: route.FileActions{
				"signIn": func(ctx *action.Context) error {
					if docsapp.AuthManager() == nil {
						return action.Error(500, "auth manager not configured")
					}
					name := strings.TrimSpace(ctx.FormData["name"])
					if name == "" {
						return action.Validation("Enter a name to sign in.", map[string]string{
							"name": "Name is required.",
						}, ctx.FormData)
					}
					if !docsapp.AuthManager().SignIn(ctx.Request, auth.User{
						ID:    strings.ToLower(strings.ReplaceAll(name, " ", "-")),
						Name:  name,
						Roles: []string{"docs"},
					}) {
						return action.Error(500, "session middleware not available")
					}
					session.AddFlash(ctx.Request, "notice", "Signed in as "+name+".")
					return ctx.Success("The auth middleware will now expose the current user to routed .gsx pages.", nil)
				},
				"signOut": func(ctx *action.Context) error {
					if docsapp.AuthManager() == nil {
						return action.Error(500, "auth manager not configured")
					}
					docsapp.AuthManager().SignOut(ctx.Request)
					session.AddFlash(ctx.Request, "notice", "Signed out.")
					return ctx.Success("The session-backed auth state has been cleared.", nil)
				},
			},
		},
	)
}
