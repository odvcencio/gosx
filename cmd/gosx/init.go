package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/odvcencio/gosx"
)

func cmdInit() {
	dir := "."
	module := ""
	template := ""

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--module":
			if i+1 >= len(args) {
				fatal("init requires a value after --module")
			}
			i++
			module = strings.TrimSpace(args[i])
		case "--template":
			if i+1 >= len(args) {
				fatal("init requires a value after --template")
			}
			i++
			template = strings.TrimSpace(args[i])
		default:
			dir = args[i]
		}
	}

	if err := RunInit(dir, module, template); err != nil {
		fatal("init: %v", err)
	}
}

func RunInit(dir string, module string, template string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", dir, err)
	}
	if err := os.MkdirAll(absDir, 0755); err != nil {
		return fmt.Errorf("create %s: %w", absDir, err)
	}

	if module == "" {
		module = defaultModuleName(absDir)
	}

	template, err = normalizeInitTemplate(template)
	if err != nil {
		return err
	}

	files, err := scaffoldFilesForTemplate(module, template)
	if err != nil {
		return err
	}

	for _, file := range files {
		if err := writeScaffoldFile(absDir, file.Path, file.Contents); err != nil {
			return err
		}
	}
	if err := syncModulesPackage(absDir); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "gosx init: created %s template in %s\n", template, absDir)
	return nil
}

func defaultModuleName(dir string) string {
	base := filepath.Base(dir)
	base = strings.ToLower(strings.TrimSpace(base))
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "gosx-app"
	}

	re := regexp.MustCompile(`[^a-z0-9._/-]+`)
	base = re.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		return "gosx-app"
	}
	return base
}

func writeScaffoldFile(root string, rel string, contents string) error {
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func goModTemplate(module string) string {
	return fmt.Sprintf(`module %s

go 1.25.1

require github.com/odvcencio/gosx v%s
`, module, gosx.Version)
}

func mainTemplate(module string) string {
	return strings.ReplaceAll(`package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	_ "__MODULE__/modules"
	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/env"
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

	appName := getenv("APP_NAME", "My GoSX App")
	port := getenv("PORT", "8080")
	sessions := session.MustNew(getenv("SESSION_SECRET", "gosx-app-session-secret"), session.Options{})

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetMetadata(server.Metadata{
			Links: []server.LinkTag{
				{Rel: "stylesheet", Href: "/styles.css"},
			},
		})
		return server.HTMLDocument(ctx.Title(appName), ctx.Head(), body)
	})
	if err := router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{}); err != nil {
		log.Fatal(err)
	}

	app := server.New()
	app.EnableNavigation()
	app.Use(sessions.Middleware)
	app.Use(sessions.Protect)
	app.SetPublicDir(filepath.Join(root, "public"))
	app.API("GET /api/health", func(ctx *server.Context) (any, error) {
		ctx.CachePublic(30 * time.Second)
		ctx.CacheTag("health")
		return map[string]any{
			"ok":      true,
			"app":     appName,
			"version": gosx.Version,
			"time":    time.Now().Format(time.RFC3339),
		}, nil
	})
	app.Mount("/", router.Build())

	log.Printf("%s listening on http://localhost:%s", appName, port)
	log.Fatal(app.ListenAndServe(":" + port))
}

func getenv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
`, "__MODULE__", module)
}

func appLayoutTemplate() string {
	return `package app

func Layout() Node {
	return <div class="site-shell">
		<header class="site-header">
			<a href="/" data-gosx-link class="site-brand">GoSX Starter</a>
			<nav class="site-nav">
				<a href="/" data-gosx-link class="site-link">Home</a>
				<a href="/stack" data-gosx-link class="site-link">Transition Demo</a>
				<a href="/api/health" class="site-link">API</a>
			</nav>
		</header>
		<Slot />
	</div>
}
`
}

func modulesTemplate(module string) string {
	return strings.ReplaceAll(`package modules

import (
	_ "__MODULE__/app"
	_ "__MODULE__/app/stack"
)
`, "__MODULE__", module)
}

func appHomeServerTemplate() string {
	return `package app

import (
	"os"
	"strings"

	"github.com/odvcencio/gosx/action"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
	"github.com/odvcencio/gosx/session"
)

func init() {
	route.MustRegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			appName := os.Getenv("APP_NAME")
			if appName == "" {
				appName = "My GoSX App"
			}
			return map[string]string{
				"appName": appName,
				"source":  page.Source,
			}, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			values, _ := data.(map[string]string)
			appName := values["appName"]
			if appName == "" {
				appName = "My GoSX App"
			}
			return server.Metadata{
				Title:       appName,
				Description: "A GoSX app scaffolded with file-routed .gsx pages, session-backed form actions, root-level public assets, env loading, and a colocated JSON API.",
			}, nil
		},
		Actions: route.FileActions{
			"subscribe": func(ctx *action.Context) error {
				if strings.TrimSpace(ctx.FormData["email"]) == "" {
					return action.Validation("Add an email address to continue.", map[string]string{
						"email": "Email is required.",
					}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", "The starter app is using redirect-safe form state and session-backed flashes.")
				return ctx.Success("Form submission completed without leaving the server-first model.", nil)
			},
		},
	})
}
`
}

func appHomeTemplate() string {
	return `package app

func Page() Node {
	return <main class="shell">
		<span class="eyebrow">GoSX</span>
		<h1>{data.appName}</h1>
		<p>Server-rendered HTML, file-routed .gsx pages, session-backed form actions, root-level public assets, metadata, env loading, and JSON APIs are ready out of the box.</p>

		<div class="actions">
			<a href="/stack" data-gosx-link class="button primary">Open page transition</a>
			<a href="/api/health" class="button">Open API route</a>
			<a href="https://github.com/odvcencio/gosx" class="button">GoSX repo</a>
		</div>

		<section class="card">
			<h2>Starter form</h2>
			<p>
				This page posts to a relative action, validates on the server, and restores values after a normal browser redirect.
			</p>
			<form class="docs-form" method="post" action={actionPath("subscribe")}>
				<input type="hidden" name="csrf_token" value={csrf.token}></input>
				<label class="field">
					<span>Name</span>
					<input name="name" value={actions.subscribe.values.name}></input>
				</label>
				<label class="field">
					<span>Email</span>
					<input name="email" value={actions.subscribe.values.email}></input>
				</label>
				<p class="form-error">{actions.subscribe.fieldErrors.email}</p>
				<p class="form-status">{action.message}</p>
				<p class="flash-note">{flash.notice}</p>
				<div class="actions">
					<button class="button primary" type="submit">Submit the starter action</button>
				</div>
			</form>
		</section>

		<section class="card">
			<h2>Next steps</h2>
			<ul>
				<li>
					Edit the files under
					<span class="inline-code">app/</span>
					to add routes and content.
				</li>
				<li>
					Add a sibling
					<span class="inline-code">page.server.go</span>
					file beside any route when you need
					<span class="inline-code">Load</span>,
					<span class="inline-code">Metadata</span>,
					or
					<span class="inline-code">Actions</span>.
				</li>
				<li>
					Keep a blank import of
					<span class="inline-code">your/module/modules</span>
					in
					<span class="inline-code">main.go</span>
					so those file modules register at startup.
				</li>
				<li>
					Drop assets into
					<span class="inline-code">public/</span>
					to serve them from the site root.
				</li>
				<li>
					Use
					<span class="inline-code">server.Metadata</span>
					and
					<span class="inline-code">ctx.AddHead(...)</span>
					in your layout for SEO and document tags.
				</li>
				<li>
					Use
					<span class="inline-code">app.API(...)</span>
					for colocated JSON endpoints.
				</li>
			</ul>
		</section>
	</main>
}
`
}

func appStackServerTemplate() string {
	return `package app

import (
	"os"

	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
)

func init() {
	route.MustRegisterFileModuleHere(route.FileModuleOptions{
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			appName := os.Getenv("APP_NAME")
			if appName == "" {
				appName = "My GoSX App"
			}
			return server.Metadata{
				Title:       appName + " Stack",
				Description: "A second page rendered through the GoSX navigation runtime and declared through a file-route server module.",
			}, nil
		},
	})
}
`
}

func appStackTemplate() string {
	return `package app

func Page() Node {
	return <main class="shell">
		<span class="eyebrow">Client Navigation</span>
		<h1>Page transitions without a full reload</h1>
		<p>This page was fetched as HTML, swapped into the live document, and re-used the same GoSX runtime lifecycle.</p>

		<div class="actions">
			<a href="/" data-gosx-link class="button primary">Back home</a>
		</div>
	</main>
}
`
}

func appNotFoundTemplate() string {
	return `package app

func Page() Node {
	return <main class="shell">
		<span class="eyebrow">404</span>
		<h1>Page not found</h1>
		<p>
			Check your routes or drop the missing asset into
			<span class="inline-code">public/</span>.
		</p>
		<div class="actions">
			<a href="/" data-gosx-link class="button primary">Back home</a>
		</div>
	</main>
}
`
}

func appErrorTemplate() string {
	return `package app

func Page() Node {
	return <main class="shell">
		<span class="eyebrow">500</span>
		<h1>Something broke</h1>
		<p>The app hit an unexpected error while rendering the current page.</p>
		<div class="actions">
			<a href="/" data-gosx-link class="button primary">Back home</a>
		</div>
	</main>
}
`
}

func envTemplate() string {
	return `APP_NAME=My GoSX App
PORT=8080
SESSION_SECRET=change-me-in-production
GOSX_ENV=development
`
}

func gitignoreTemplate() string {
	return `/build
/dist
.DS_Store
`
}

func stylesTemplate() string {
	return `:root {
  --bg: #f5efe5;
  --ink: #122620;
  --muted: #51635d;
  --card: rgba(255, 255, 255, 0.84);
  --line: rgba(18, 38, 32, 0.12);
  --accent: #dc5f3f;
  --accent-ink: #fff7f0;
  --shadow: 0 28px 64px rgba(18, 38, 32, 0.14);
}

* {
  box-sizing: border-box;
}

body {
  margin: 0;
  min-height: 100vh;
  font-family: "Georgia", "Iowan Old Style", serif;
  color: var(--ink);
  background:
    radial-gradient(circle at top left, rgba(220, 95, 63, 0.22), transparent 32rem),
    radial-gradient(circle at bottom right, rgba(18, 38, 32, 0.1), transparent 28rem),
    linear-gradient(180deg, #f9f4ed 0%, var(--bg) 100%);
}

.site-shell {
  min-height: 100vh;
}

.site-header {
  width: min(56rem, calc(100% - 2rem));
  margin: 0 auto;
  padding: 1.25rem 0 0;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
}

.site-brand {
  color: var(--ink);
  text-decoration: none;
  font-size: 0.95rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.site-nav {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 0.75rem;
}

.site-link {
  color: var(--muted);
  text-decoration: none;
  font-size: 0.92rem;
}

.site-link:hover,
.site-brand:hover {
  color: var(--accent);
}

main.shell {
  width: min(52rem, calc(100% - 2rem));
  margin: 4rem auto;
  padding: 2.5rem;
  border: 1px solid var(--line);
  border-radius: 2rem;
  background: var(--card);
  box-shadow: var(--shadow);
  backdrop-filter: blur(16px);
}

.eyebrow {
  display: inline-block;
  margin-bottom: 1rem;
  padding: 0.35rem 0.75rem;
  border-radius: 999px;
  background: rgba(220, 95, 63, 0.12);
  color: var(--accent);
  font-size: 0.8rem;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}

h1 {
  margin: 0 0 1rem;
  font-size: clamp(2.8rem, 8vw, 4.8rem);
  line-height: 0.95;
}

h2 {
  margin-top: 0;
}

p,
li {
  color: var(--muted);
  font-size: 1.05rem;
  line-height: 1.7;
}

ul {
  margin: 0;
  padding-left: 1.2rem;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.85rem;
  margin: 2rem 0;
}

.docs-form {
  display: grid;
  gap: 0.85rem;
}

.field {
  display: grid;
  gap: 0.45rem;
}

.field span {
  color: var(--muted);
  font-size: 0.82rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.field input {
  width: 100%;
  padding: 0.82rem 0.95rem;
  border: 1px solid var(--line);
  border-radius: 1rem;
  background: rgba(255, 255, 255, 0.88);
  color: var(--ink);
  font: inherit;
}

.field input:focus-visible {
  outline: 2px solid rgba(220, 95, 63, 0.18);
  outline-offset: 2px;
  border-color: rgba(220, 95, 63, 0.4);
}

.form-error,
.form-status,
.flash-note {
  margin: 0;
  min-height: 1.4rem;
}

.form-error {
  color: #8e2e1f;
}

.button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0.85rem 1.15rem;
  border: 1px solid var(--line);
  border-radius: 999px;
  color: var(--ink);
  text-decoration: none;
  font: inherit;
  cursor: pointer;
  transition: transform 160ms ease, box-shadow 160ms ease, border-color 160ms ease;
}

.button:hover {
  transform: translateY(-1px);
  box-shadow: 0 14px 30px rgba(18, 38, 32, 0.08);
  border-color: rgba(18, 38, 32, 0.22);
}

.button.primary {
  background: var(--accent);
  color: var(--accent-ink);
  border-color: transparent;
}

.card {
  padding: 1.5rem;
  border-radius: 1.25rem;
  border: 1px solid var(--line);
  background: rgba(255, 255, 255, 0.65);
}

.inline-code {
  padding: 0.12rem 0.35rem;
  border-radius: 0.45rem;
  background: rgba(18, 38, 32, 0.07);
  font-family: "IBM Plex Mono", "SFMono-Regular", monospace;
  font-size: 0.92em;
}

@media (max-width: 640px) {
  .site-header {
    padding-top: 1rem;
    flex-direction: column;
    align-items: flex-start;
  }

  .site-nav {
    justify-content: flex-start;
  }

  main.shell {
    margin: 1.5rem auto;
    padding: 1.5rem;
  }
}
`
}
