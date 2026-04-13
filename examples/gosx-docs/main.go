package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/auth"
	"github.com/odvcencio/gosx/env"
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	collab "github.com/odvcencio/gosx/examples/gosx-docs/app/demos/collab"
	livesim "github.com/odvcencio/gosx/examples/gosx-docs/app/demos/livesim"
	_ "github.com/odvcencio/gosx/examples/gosx-docs/modules"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
	"github.com/odvcencio/gosx/session"
)

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(thisFile)
	if err := env.LoadDir(root, ""); err != nil {
		log.Fatal(err)
	}
	port := getenv("PORT", "8080")
	publicBase := strings.TrimRight(getenv("PUBLIC_URL", "http://localhost:"+port), "/")
	sessions := session.MustNew(getenv("SESSION_SECRET", "gosx-docs-session-secret"), session.Options{})
	authn := auth.New(sessions, auth.Options{LoginPath: "/docs/auth"})
	docsapp.BindAuth(authn)
	magicLinks := authn.MagicLinks(auth.MagicLinkOptions{
		Path:        "/auth/magic-link",
		SuccessPath: "/docs/auth",
		FailurePath: "/docs/auth",
		FlashKey:    "magicLink",
		Resolver: auth.MagicLinkResolverFunc(func(_ context.Context, email string) (auth.User, error) {
			return docsDemoUser(email), nil
		}),
	})
	docsapp.BindMagicLinks(magicLinks)
	webauthn := authn.WebAuthn(auth.WebAuthnOptions{
		RPName:      "GoSX",
		Origin:      publicBase,
		SuccessPath: "/docs/auth",
		FailurePath: "/docs/auth",
		FlashKey:    "passkey",
		Resolver: auth.WebAuthnResolverFunc(func(_ context.Context, login string) (auth.User, error) {
			return docsDemoUser(login), nil
		}),
	})
	docsapp.BindWebAuthn(webauthn)

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.AddHead(server.NavigationScript())
		ctx.AddHead(gosx.RawHTML(`<link rel="preload" href="/fonts/SpaceGrotesk-Bold.woff2" as="font" type="font/woff2" crossorigin>`))
		ctx.AddHead(gosx.RawHTML(`<link rel="preload" href="/fonts/Inter-400.woff2" as="font" type="font/woff2" crossorigin>`))
		ctx.AddHead(gosx.RawHTML(`<link rel="preload" href="/fonts/JetBrainsMono-Regular.woff2" as="font" type="font/woff2" crossorigin>`))
		return server.HTMLDocument(ctx.Title("GoSX"), ctx.Head(), body)
	})

	if err := router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{}); err != nil {
		log.Fatal(err)
	}

	app := server.New()
	router.SetRevalidator(app.Revalidator())
	app.EnableISR()
	app.EnableNavigation()
	app.Use(sessions.Middleware)
	app.Use(authn.Middleware)
	app.Use(sessions.Protect)
	app.SetPublicDir(filepath.Join(root, "public"))
	app.Mount("/auth/magic-link/request", magicLinks.RequestHandler())
	app.Mount("/auth/magic-link", magicLinks.CallbackHandler())
	app.Mount("/auth/webauthn/register/options", webauthn.RegisterOptionsHandler())
	app.Mount("/auth/webauthn/register", webauthn.RegisterHandler())
	app.Mount("/auth/webauthn/login/options", webauthn.LoginOptionsHandler())
	app.Mount("/auth/webauthn/login", webauthn.LoginHandler())
	app.Redirect("GET /docs", "/docs/getting-started", http.StatusTemporaryRedirect)
	app.Mount("/demos/collab/ws", collab.Hub)
	app.Mount("/demos/livesim/ws", livesim.Hub)
	app.Mount("/", router.Build())

	log.Printf("gosx-docs at http://localhost:%s", port)
	log.Fatal(app.ListenAndServe(":" + port))
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
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
