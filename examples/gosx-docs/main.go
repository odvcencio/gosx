package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/env"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	checkers "m31labs.dev/gosx/examples/gosx-docs/app/demos/checkers"
	cms "m31labs.dev/gosx/examples/gosx-docs/app/demos/cms"
	collab "m31labs.dev/gosx/examples/gosx-docs/app/demos/collab"
	fluid "m31labs.dev/gosx/examples/gosx-docs/app/demos/fluid"
	livesim "m31labs.dev/gosx/examples/gosx-docs/app/demos/livesim"
	playground "m31labs.dev/gosx/examples/gosx-docs/app/demos/playground"
	docsauth "m31labs.dev/gosx/examples/gosx-docs/app/docs/auth"
	docsforms "m31labs.dev/gosx/examples/gosx-docs/app/docs/forms"
	_ "m31labs.dev/gosx/examples/gosx-docs/modules"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(thisFile)
	if err := env.LoadDir(root, ""); err != nil {
		log.Fatal(err)
	}
	port := getenv("PORT", "8080")
	publicBase := strings.TrimRight(getenv("PUBLIC_URL", "http://localhost:"+port), "/")
	// Keep the Secure cookie flag, unless PUBLIC_URL serves plain HTTP. A
	// local HTTP run needs the opt-out, because a browser drops a Secure
	// cookie on a plain HTTP origin.
	sessionSecret, err := docsSessionSecret(publicBase, os.Getenv("SESSION_SECRET"))
	if err != nil {
		log.Fatal(err)
	}
	sessions, err := session.New(sessionSecret, session.Options{
		AllowInsecure: strings.HasPrefix(publicBase, "http://"),
	})
	if err != nil {
		log.Fatal(err)
	}
	authn := auth.New(sessions, auth.Options{LoginPath: "/docs/auth"})
	docsapp.BindAuth(authn)
	publicAuthDemos := !strings.HasPrefix(strings.ToLower(publicBase), "https://")
	var magicLinks *auth.MagicLinks
	var webauthn *auth.WebAuthn
	if publicAuthDemos {
		magicLinks = authn.MagicLinks(auth.MagicLinkOptions{
			Path: "/auth/magic-link",
			// Pin the origin that carries the sign-in token. Without BaseURL a
			// forged Host header could send the token to another site.
			BaseURL:     publicBase,
			SuccessPath: "/docs/auth",
			FailurePath: "/docs/auth",
			FlashKey:    "magicLink",
			Resolver: auth.MagicLinkResolverFunc(func(_ context.Context, email string) (auth.User, error) {
				return docsDemoUser(email), nil
			}),
		})
		webauthn = authn.WebAuthn(auth.WebAuthnOptions{
			RPName:      "GoSX",
			Origin:      publicBase,
			SuccessPath: "/docs/auth",
			FailurePath: "/docs/auth",
			FlashKey:    "passkey",
			Resolver: auth.WebAuthnResolverFunc(func(_ context.Context, login string) (auth.User, error) {
				return docsDemoUser(login), nil
			}),
		})
	}
	docsapp.BindMagicLinks(magicLinks)
	docsapp.BindWebAuthn(webauthn)

	router := route.NewRouter()
	if err := registerManagedActions(router); err != nil {
		log.Fatal(err)
	}
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.AddHead(server.NavigationScript())
		ctx.AddHead(gosx.RawHTML(`<link rel="preload" href="/fonts/SpaceGrotesk-Bold.woff2" as="font" type="font/woff2" crossorigin>`))
		ctx.AddHead(gosx.RawHTML(`<link rel="preload" href="/fonts/Inter-400.woff2" as="font" type="font/woff2" crossorigin>`))
		ctx.AddHead(gosx.RawHTML(`<link rel="preload" href="/fonts/JetBrainsMono-Regular.woff2" as="font" type="font/woff2" crossorigin>`))
		return server.HTMLDocumentWithLanguage(ctx.Title("GoSX"), "en", ctx.Head(), body)
	})

	if err := router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{}); err != nil {
		log.Fatal(err)
	}

	app := server.New()
	// Keep page-generated hashed runtime URLs and the runtime file server on the
	// same application root. Without this, `go run ./examples/gosx-docs` from
	// the repository root can read the repository's dist/build.json while
	// serving the docs app's build output, leaving Scene3D and navigation chunks
	// as 404s.
	app.SetRuntimeRoot(root)
	router.SetRevalidator(app.Revalidator())
	app.EnableISR()
	app.EnableNavigation()
	app.Use(docsSecurityHeaders)
	app.Use(canonicalDocsIndex)
	app.Use(limitDocsRequestBodies(1 << 20))
	app.Use(sessions.Middleware)
	app.Use(authn.Middleware)
	// CSRF-protect unsafe requests, but exempt the telemetry beacon: it is
	// append-only, rate-limited, and sanitized (no auth-sensitive mutation), and
	// it POSTs without a CSRF token — so blanket Protect would 403 every client
	// error/telemetry report (and silently drop WebGPU crash reports).
	app.Use(func(next http.Handler) http.Handler {
		protected := sessions.Protect(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == server.ClientEventsRoute || r.URL.Path == "/api/site/probe" {
				next.ServeHTTP(w, r)
				return
			}
			protected.ServeHTTP(w, r)
		})
	})
	app.SetPublicDir(filepath.Join(root, "public"))
	mountSiteDocuments(app, root)
	configureProductionReadiness(app)
	if publicAuthDemos {
		app.Mount("/auth/magic-link/request", magicLinks.RequestHandler())
		app.Mount("/auth/magic-link", magicLinks.CallbackHandler())
		app.Mount("/auth/webauthn/register/options", webauthn.RegisterOptionsHandler())
		app.Mount("/auth/webauthn/register", webauthn.RegisterHandler())
		app.Mount("/auth/webauthn/login/options", webauthn.LoginOptionsHandler())
		app.Mount("/auth/webauthn/login", webauthn.LoginHandler())
	}
	app.Mount("/demos/collab/ws", collab.Hub)
	app.Mount("/demos/checkers/ws", checkers.Hub)
	app.Mount("/demos/fluid/ws", fluid.Hub)
	app.Mount("/demos/livesim/ws", livesim.Hub)
	rootHandler, err := router.BuildChecked()
	if err != nil {
		log.Fatal(err)
	}
	app.Mount("/", rootHandler)

	log.Printf("gosx-docs at http://localhost:%s", port)
	log.Fatal(app.ListenAndServe(":" + port))
}

func registerManagedActions(router *route.Router) error {
	installers := []func(*route.Router) error{
		playground.RegisterManagedActions,
		cms.RegisterManagedActions,
		docsauth.RegisterManagedActions,
		docsforms.RegisterManagedActions,
	}
	for _, install := range installers {
		if err := install(router); err != nil {
			return err
		}
	}
	return nil
}

func limitDocsRequestBodies(maxBytes int64) server.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && maxBytes > 0 && r.Method != http.MethodGet && r.Method != http.MethodHead {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func docsSessionSecret(publicBase, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value != "" {
		return value, nil
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(publicBase)), "https://") {
		return "", fmt.Errorf("SESSION_SECRET is required for the public HTTPS docs deployment")
	}
	return "gosx-docs-session-secret", nil
}

func docsDemoUser(value string) auth.User {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		value = "guest@gosx.dev"
	}
	local := value
	if at := strings.Index(local, "@"); at >= 0 {
		local = local[:at]
	}
	name := strings.ReplaceAll(local, ".", " ")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.TrimSpace(name)
	if name == "" {
		name = "GoSX User"
	}
	return auth.User{
		ID:    value,
		Email: value,
		Name:  strings.Title(name),
		Roles: []string{"docs"},
		Meta: map[string]any{
			"provider": "docs-demo",
		},
	}
}
