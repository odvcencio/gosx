package main

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/auth"
	"github.com/odvcencio/gosx/env"
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	runtimedocs "github.com/odvcencio/gosx/examples/gosx-docs/app/docs/runtime"
	_ "github.com/odvcencio/gosx/examples/gosx-docs/modules"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
	"github.com/odvcencio/gosx/session"
)

type navItem struct {
	href  string
	label string
}

var navItems = []navItem{
	{href: "/", label: "Overview"},
	{href: "/docs/getting-started", label: "Getting Started"},
	{href: "/docs/routing", label: "Routing"},
	{href: "/docs/forms", label: "Forms"},
	{href: "/docs/auth", label: "Auth"},
	{href: "/docs/runtime", label: "Runtime"},
	{href: "/docs/images", label: "Images"},
	{href: "/labs/stream", label: "Streaming"},
	{href: "/labs/secret", label: "Secret"},
}

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(thisFile)
	if err := ensureDocsSampleAssets(root); err != nil {
		log.Fatal(err)
	}
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
		RPName:      "GoSX Docs",
		Origin:      publicBase,
		SuccessPath: "/docs/auth",
		FailurePath: "/docs/auth",
		FlashKey:    "passkey",
		Resolver: auth.WebAuthnResolverFunc(func(_ context.Context, login string) (auth.User, error) {
			return docsDemoUser(login), nil
		}),
	})
	docsapp.BindWebAuthn(webauthn)
	var oauthManager *auth.OAuth
	var oauthProviders []map[string]string
	var providerConfigs []auth.OAuthProvider
	hasGoogleOAuth := false
	hasGitHubOAuth := false
	if clientID, clientSecret := getenv("GOOGLE_CLIENT_ID", ""), getenv("GOOGLE_CLIENT_SECRET", ""); clientID != "" && clientSecret != "" {
		providerConfigs = append(providerConfigs, auth.GoogleProvider(clientID, clientSecret, publicBase+"/auth/oauth/google/callback"))
		oauthProviders = append(oauthProviders, map[string]string{
			"Name":  "google",
			"Label": "Google",
			"Href":  "/auth/oauth/google?next=/docs/auth",
		})
		hasGoogleOAuth = true
	}
	if clientID, clientSecret := getenv("GITHUB_CLIENT_ID", ""), getenv("GITHUB_CLIENT_SECRET", ""); clientID != "" && clientSecret != "" {
		providerConfigs = append(providerConfigs, auth.GitHubProvider(clientID, clientSecret, publicBase+"/auth/oauth/github/callback"))
		oauthProviders = append(oauthProviders, map[string]string{
			"Name":  "github",
			"Label": "GitHub",
			"Href":  "/auth/oauth/github?next=/docs/auth",
		})
		hasGitHubOAuth = true
	}
	if len(providerConfigs) > 0 {
		oauthManager = authn.OAuth(auth.OAuthOptions{
			Providers:   providerConfigs,
			SuccessPath: "/docs/auth",
			FailurePath: "/docs/auth",
			FlashKey:    "oauth",
		})
	}
	docsapp.BindOAuth(oauthManager, oauthProviders)

	siteLayout, err := route.FileLayout(filepath.Join(root, "app", "layout.gsx"))
	if err != nil {
		log.Fatal(err)
	}
	wrapSite := func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		return siteLayout(ctx, body)
	}

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.AddHead(server.Stylesheet("docs.css"))
		ctx.AddHead(server.NavigationScript())
		if ctx != nil && ctx.Request != nil && ctx.Request.URL != nil && ctx.Request.URL.Path == "/docs/auth" {
			ctx.AddHead(auth.WebAuthnScript())
		}
		return server.HTMLDocument(ctx.Title("GoSX Docs"), ctx.Head(), body)
	})
	router.Add(route.Route{
		Pattern: "/labs/stream",
		Handler: func(ctx *route.RouteContext) gosx.Node {
			ctx.SetMetadata(server.Metadata{
				Title:       "Streaming | GoSX Docs",
				Description: "Deferred regions flush fallback HTML first, then stream the resolved node into place.",
			})
			region := ctx.DeferWithOptions(server.DeferredOptions{
				Class: "card",
			}, gosx.El("div",
				gosx.El("strong", gosx.Text("Loading region")),
				gosx.El("p", gosx.Text("The server has already flushed this fallback while the deferred resolver finishes.")),
			), func() (gosx.Node, error) {
				time.Sleep(180 * time.Millisecond)
				return gosx.El("div", gosx.Attrs(gosx.Attr("class", "card")),
					gosx.El("strong", gosx.Text("Resolved region")),
					gosx.El("p", gosx.Text("This card streamed after the initial HTML shell and replaced the fallback slot in-place.")),
				), nil
			})

			return wrapSite(ctx, gosx.El("article", gosx.Attrs(gosx.Attr("class", "prose")),
				gosx.El("div", gosx.Attrs(gosx.Attr("class", "page-topper")),
					gosx.El("span", gosx.Attrs(gosx.Attr("class", "eyebrow")), gosx.Text("Streaming")),
					gosx.El("p", gosx.Attrs(gosx.Attr("class", "lede")), gosx.Text("Deferred regions flush fallback HTML first, then stream resolved content into place.")),
				),
				gosx.El("h1", gosx.Text("Streaming in GoSX starts with deferred regions, not a separate rendering stack.")),
				gosx.El("p", gosx.Text("A page can flush its shell immediately, keep the fallback visible, and stream late sections into the live DOM as resolvers finish.")),
				gosx.El("section", gosx.Attrs(gosx.Attr("class", "feature-grid")),
					region,
					gosx.El("div", gosx.Attrs(gosx.Attr("class", "card")),
						gosx.El("strong", gosx.Text("API")),
						gosx.El("p", gosx.Text("Use ctx.Defer(...) or ctx.DeferWithOptions(...) inside server or route handlers.")),
					),
				),
				gosx.El("pre", gosx.Attrs(gosx.Attr("class", "code-block")), gosx.Text(`ctx.Defer(
    <p>Loading...</p>,
    func() (gosx.Node, error) {
        return <section>Resolved</section>, nil
    },
)`)),
				gosx.El("div", gosx.Attrs(gosx.Attr("class", "hero-actions")),
					gosx.El("a", gosx.Attrs(gosx.Attr("href", "/docs/runtime"), gosx.Attr("data-gosx-link", true), gosx.Attr("class", "cta-link")), gosx.Text("Back to runtime")),
					gosx.El("a", gosx.Attrs(gosx.Attr("href", "/"), gosx.Attr("data-gosx-link", true), gosx.Attr("class", "cta-link primary")), gosx.Text("Back to overview")),
				),
			))
		},
	})
	router.Add(route.Route{
		Pattern:    "/labs/secret",
		Middleware: []route.Middleware{authn.Require},
		Handler: func(ctx *route.RouteContext) gosx.Node {
			ctx.SetMetadata(server.Metadata{
				Title:       "Secret Lab | GoSX Docs",
				Description: "A guarded route proving auth middleware works on the same router that serves file-based pages.",
			})
			name := ""
			if user, ok := auth.Current(ctx.Request); ok {
				name = user.Name
			}
			return wrapSite(ctx, gosx.El("article", gosx.Attrs(gosx.Attr("class", "prose")),
				gosx.El("div", gosx.Attrs(gosx.Attr("class", "page-topper")),
					gosx.El("span", gosx.Attrs(gosx.Attr("class", "eyebrow")), gosx.Text("Protected")),
					gosx.El("p", gosx.Attrs(gosx.Attr("class", "lede")), gosx.Text("This route is wrapped in auth middleware before the router resolves the page handler.")),
				),
				gosx.El("h1", gosx.Text("You reached a guarded route.")),
				gosx.El("p", gosx.Text("Current user: "+name)),
				gosx.El("div", gosx.Attrs(gosx.Attr("class", "hero-actions")),
					gosx.El("a", gosx.Attrs(gosx.Attr("href", "/docs/auth"), gosx.Attr("data-gosx-link", true), gosx.Attr("class", "cta-link primary")), gosx.Text("Back to auth")),
					gosx.El("a", gosx.Attrs(gosx.Attr("href", "/"), gosx.Attr("data-gosx-link", true), gosx.Attr("class", "cta-link")), gosx.Text("Back to overview")),
				),
			))
		},
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
	if oauthManager != nil && hasGoogleOAuth {
		app.Mount("/auth/oauth/google", oauthManager.BeginHandler("google"))
		app.Mount("/auth/oauth/google/callback", oauthManager.CallbackHandler("google"))
	}
	if oauthManager != nil && hasGitHubOAuth {
		app.Mount("/auth/oauth/github", oauthManager.BeginHandler("github"))
		app.Mount("/auth/oauth/github/callback", oauthManager.CallbackHandler("github"))
	}
	app.Redirect("GET /docs", "/docs/getting-started", http.StatusTemporaryRedirect)
	app.Redirect("GET /legacy/runtime", "/docs/runtime", http.StatusMovedPermanently)
	app.Rewrite("GET /runtime", "/docs/runtime")
	app.API("GET /api/meta", func(ctx *server.Context) (any, error) {
		ctx.Cache(server.CachePolicy{
			Public:               true,
			MaxAge:               time.Minute,
			StaleWhileRevalidate: 5 * time.Minute,
		})
		ctx.CacheTag("docs-meta")
		pages := make([]map[string]string, 0, len(navItems))
		for _, item := range navItems {
			pages = append(pages, map[string]string{
				"href":  item.href,
				"label": item.label,
			})
		}
		return map[string]any{
			"ok":      true,
			"product": "gosx-docs",
			"version": gosx.Version,
			"pages":   pages,
		}, nil
	})
	app.API("GET /api/runtime/scene-program", func(ctx *server.Context) (any, error) {
		ctx.Cache(server.CachePolicy{
			Public: true,
			MaxAge: 5 * time.Minute,
		})
		return runtimedocs.SceneDemoProgram(), nil
	})
	app.HandleAPI(server.APIRoute{
		Pattern:    "GET /api/me",
		Middleware: []server.Middleware{authn.Require},
		Handler: func(ctx *server.Context) (any, error) {
			user, _ := auth.Current(ctx.Request)
			return map[string]any{
				"ok":   true,
				"user": user,
			}, nil
		},
	})
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

func ensureDocsSampleAssets(root string) error {
	publicDir := filepath.Join(root, "public")
	if err := os.MkdirAll(publicDir, 0755); err != nil {
		return err
	}

	sample := filepath.Join(publicDir, "paper-card.png")
	if _, err := os.Stat(sample); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	const width = 1200
	const height = 780
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			base := uint8(228 + (y*18)/height)
			ink := uint8(36 + (x*94)/width)
			img.Set(x, y, color.RGBA{
				R: base,
				G: uint8(216 + (x*20)/width),
				B: uint8(198 + (y*24)/height),
				A: 255,
			})
			if x > 84 && x < width-84 && y > 84 && y < height-84 && (x+y)%17 < 2 {
				img.Set(x, y, color.RGBA{R: ink, G: uint8(74 + (y*32)/height), B: 62, A: 255})
			}
			if x > 180 && x < width-180 && y > 160 && y < 260 {
				img.Set(x, y, color.RGBA{R: 181, G: 91, B: 52, A: 255})
			}
		}
	}

	file, err := os.Create(sample)
	if err != nil {
		return err
	}
	defer file.Close()
	return png.Encode(file, img)
}
