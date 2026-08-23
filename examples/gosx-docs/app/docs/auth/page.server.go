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
					"sessionSample": "sessions, err := session.New(os.Getenv(\"SESSION_SECRET\"), session.Options{\n\tCookieName:      \"__Host-myapp\",\n\tEncrypt:         true,\n\tPreviousSecrets: strings.Fields(os.Getenv(\"SESSION_PREVIOUS_SECRETS\")),\n})\nif err != nil {\n\tlog.Fatal(err)\n}\n\nauthn := auth.New(sessions, auth.Options{LoginPath: \"/login\"})\napp.Use(sessions.Middleware)\napp.Use(authn.Middleware)\napp.Use(sessions.Protect)",
					"magicSample":   "magic := authn.MagicLinks(auth.MagicLinkOptions{\n\tPath:        \"/auth/magic-link\",\n\tBaseURL:     \"https://app.example\",\n\tSuccessPath: \"/dashboard\",\n\tStore:       authredis.NewMagicLinkStore(redisClient, authredis.Options{}),\n\tSender:      mailer,\n\tResolver: auth.MagicLinkResolverFunc(func(ctx context.Context, email string) (auth.User, error) {\n\t\treturn lookupUser(ctx, email)\n\t}),\n})\napp.Mount(\"/auth/magic-link/request\", magic.RequestHandler())\napp.Mount(\"/auth/magic-link\", magic.CallbackHandler())",
					"passkeySample": "passkeys := authn.WebAuthn(auth.WebAuthnOptions{\n\tRPName: \"My App\",\n\tOrigin: \"https://app.example\",\n\tStore:  authredis.NewWebAuthnStore(redisClient, authredis.Options{}),\n\tResolver: auth.WebAuthnResolverFunc(func(ctx context.Context, login string) (auth.User, error) {\n\t\treturn lookupUser(ctx, login)\n\t}),\n})\napp.Mount(\"/auth/webauthn/register/options\", passkeys.RegisterOptionsHandler())\napp.Mount(\"/auth/webauthn/register\", passkeys.RegisterHandler())\napp.Mount(\"/auth/webauthn/login/options\", passkeys.LoginOptionsHandler())\napp.Mount(\"/auth/webauthn/login\", passkeys.LoginHandler())",
					"oauthSample":   "oauth := authn.OAuth(auth.OAuthOptions{\n\tProviders: []auth.OAuthProvider{\n\t\tauth.GoogleProvider(clientID, clientSecret, \"https://app.example/auth/google/callback\"),\n\t},\n\tSuccessPath: \"/dashboard\",\n})\napp.Mount(\"/auth/google\", oauth.BeginHandler(\"google\"))\napp.Mount(\"/auth/google/callback\", oauth.CallbackHandler(\"google\"))",
					"guardSample":   "protected := authn.Require(adminHandler)\n\nadminOnly := authn.RequireRole(\"admin\")\nprotectedAdmin := adminOnly(adminHandler)",
					"csrfSample":    "<form method=\"post\" action={actionPath(\"submit\")}>\n\t<input type=\"hidden\" name=\"csrf_token\" value={csrf.token} />\n\t<button type=\"submit\">Submit</button>\n</form>",
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
					name := strings.TrimSpace(ctx.Form.Value("name"))
					if name == "" {
						return action.ValidationWithValues("Enter a name to sign in.", map[string]string{
							"name": "Name is required.",
						}, map[string]string{"name": ctx.Form.Value("name")})
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
