package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"m31labs.dev/gosx"
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

func initUsage(w io.Writer) {
	fmt.Fprintf(w, `gosx init - Scaffold a GoSX application or docs site

Usage:
  gosx init [dir] [--module <module>] [--template docs]

Examples:
  gosx init my-app
  gosx init my-docs --template docs

`)
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

	if err := tidyScaffold(absDir); err != nil {
		fmt.Fprintf(os.Stderr, "gosx init: %v\n", err)
		fmt.Fprintf(os.Stderr, "gosx init: run `go mod tidy` in %s before `go run .`\n", absDir)
		return nil
	}

	fmt.Fprintf(os.Stderr, "\nNext:\n  cd %s\n  go run .\n", dir)
	return nil
}

// tidyScaffold resolves the new module's dependencies.
//
// The template writes go.mod and no go.sum, and a module with neither will not
// build — `go run .` stops on the first missing sum entry and names a package
// the author never mentioned. Every scaffolded project hit that, so the step
// belongs here rather than in a paragraph of documentation that the error
// message does not point to.
//
// A failure is not fatal. It usually means no network, and the project is
// complete apart from this; the caller prints the command to run by hand.
func tidyScaffold(dir string) error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	// The generated go.mod carries its own replace directives, so an enclosing
	// go.work would only make the result depend on where the project was
	// created.
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod tidy: %w\n%s", err, strings.TrimSpace(string(output)))
	}
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
	template := fmt.Sprintf(`module %s

go %s

require m31labs.dev/gosx v%s
`, module, gosx.MinGoVersion, gosx.Version)
	if replacePath := localGoSXReplacePath(); replacePath != "" {
		template += fmt.Sprintf("\nreplace m31labs.dev/gosx => %s\n", replacePath)
		for _, line := range localGoSXDependencyReplaceLines(replacePath) {
			template += line
		}
	}
	return template
}

// localGoSXReplacePath reports the GoSX checkout this CLI was built from, so a
// project scaffolded inside the repository builds against the working tree
// instead of the released module.
//
// This must fail closed. runtime.Caller reports a build-time path, and for a
// CLI installed with `go install` that path is the read-only module cache —
// which satisfies every "does this look like GoSX" test on its own: the go.mod
// declares the module, and env/ and session/ are both present. Answering yes
// there writes an absolute path from the packager's machine into a stranger's
// go.mod, leaking a home directory and breaking the project on every other
// machine. Answering no wrongly costs a contributor one dogfooding shortcut.
//
// So the two checks below are the discriminating ones, and both must pass.
func localGoSXReplacePath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if !isGoSXCheckout(repoRoot) {
		return ""
	}
	return filepath.ToSlash(repoRoot)
}

// isGoSXCheckout reports whether dir is a GoSX working tree rather than an
// extracted copy of the module. It is separated from localGoSXReplacePath so a
// test can hand it a directory instead of depending on where the test binary
// was compiled.
func isGoSXCheckout(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil || !strings.Contains(string(data), "module m31labs.dev/gosx") {
		return false
	}
	for _, sub := range []string{"env", "session"} {
		info, err := os.Stat(filepath.Join(dir, sub))
		if err != nil || !info.IsDir() {
			return false
		}
	}
	if inModuleCache(dir) {
		return false
	}
	// A working tree carries version-control metadata. In a linked worktree
	// .git is a file rather than a directory, so accept either. An unpacked
	// module zip has neither.
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return false
	}
	return true
}

// inModuleCache reports whether dir lives under the Go module cache.
//
// Two independent signals, because neither is reliable alone: GOMODCACHE is
// unset in most environments, and the @version path element is a convention.
// Either one firing is enough to refuse.
func inModuleCache(dir string) bool {
	if cache := os.Getenv("GOMODCACHE"); cache != "" {
		if rel, err := filepath.Rel(filepath.Clean(cache), dir); err == nil &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	// The cache stores every module as <path>@<version>. No checkout of GoSX
	// itself is named that way.
	for _, element := range strings.Split(filepath.ToSlash(dir), "/") {
		if strings.Contains(element, "@") {
			return true
		}
	}
	return false
}

func localGoSXDependencyReplaceLines(repoRoot string) []string {
	data, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, 2)
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 4 || fields[0] != "replace" || fields[2] != "=>" {
			continue
		}
		modulePath := fields[1]
		if modulePath == "m31labs.dev/gosx" {
			continue
		}
		target := fields[3]
		if strings.Contains(target, "@") {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(repoRoot, target)
		}
		if info, err := os.Stat(target); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, fmt.Sprintf("replace %s => %s\n", modulePath, filepath.ToSlash(target)))
	}
	return out
}

func mainTemplate(module string) string {
	return strings.ReplaceAll(`package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "__MODULE__/modules"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/env"
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

	appName := getenv("APP_NAME", "My GoSX App")
	port := getenv("PORT", "8080")
	publicBase := getenv("PUBLIC_URL", "http://localhost:"+port)
	// Local development serves plain HTTP. A Secure cookie never reaches the
	// server there. Point PUBLIC_URL at an https origin to restore Secure.
	sessions, err := session.New(getenv("SESSION_SECRET", "gosx-app-session-secret"), session.Options{
		AllowInsecure: strings.HasPrefix(publicBase, "http://"),
	})
	if err != nil {
		log.Fatal(err)
	}

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
	app.EnableISR()
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
	rootHandler, err := router.BuildChecked()
	if err != nil {
		log.Fatal(err)
	}
	app.Mount("/", rootHandler)

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
	"log"
	"os"
	"strings"

	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
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
				Title:       server.Title{Default: appName},
				Description: "A GoSX app scaffolded with file-routed .gsx pages, session-backed form actions, root-level public assets, env loading, and a colocated JSON API.",
			}, nil
		},
		Actions: route.FileActions{
			"subscribe": func(ctx *action.Context) error {
				if strings.TrimSpace(ctx.Form.Value("email")) == "" {
					return action.ValidationWithValues("Add an email address to continue.", map[string]string{
						"email": "Email is required.",
					}, map[string]string{"email": ctx.Form.Value("email")})
				}
				session.AddFlash(ctx.Request, "notice", "The starter app is using redirect-safe form state and session-backed flashes.")
				return ctx.Success("Form submission completed without leaving the server-first model.", nil)
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
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
	"log"
	"os"

	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			appName := os.Getenv("APP_NAME")
			if appName == "" {
				appName = "My GoSX App"
			}
			return server.Metadata{
				Title:       server.Title{Default: appName + " Stack"},
				Description: "A second page rendered through the GoSX navigation runtime and declared through a file-route server module.",
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
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
