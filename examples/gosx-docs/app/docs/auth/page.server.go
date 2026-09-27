package docs

import (
	"strings"

	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/auth"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

func init() {
	docsapp.RegisterDocsPage(
		"Auth",
		"Magic links, passkeys, and session management built into the framework.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				currentUser, signedIn := auth.Current(ctx.Request)
				return map[string]any{
					"mode":        "light",
					"title":       "Auth",
					"description": "Magic links, passkeys, and session management built into the framework.",
					"tags":        []string{"auth", "sessions", "magic-links", "passkeys"},
					"toc": []map[string]string{
						{"href": "#sessions", "label": "Sessions"},
						{"href": "#session-demo", "label": "Live Session Demo"},
						{"href": "#magic-links", "label": "Magic Links"},
						{"href": "#passkeys", "label": "WebAuthn / Passkeys"},
						{"href": "#oauth", "label": "OAuth"},
						{"href": "#protected-routes", "label": "Protected Routes"},
						{"href": "#csrf", "label": "CSRF"},
					},
					"sessionSample": docsapp.DocSample("auth/sessionSample.go.sample"),
					"magicSample":   docsapp.DocSample("auth/magicSample.go.sample"),
					"passkeySample": docsapp.DocSample("auth/passkeySample.go.sample"),
					"oauthSample":   docsapp.DocSample("auth/oauthSample.go.sample"),
					"guardSample":   docsapp.DocSample("auth/guardSample.go.sample"),
					"csrfSample":    docsapp.DocSample("auth/csrfSample.gosx.sample"),
					"currentUser": map[string]any{
						"signedIn": signedIn,
						"name":     currentUser.Name,
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
