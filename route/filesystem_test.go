package route

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/action"
	"github.com/odvcencio/gosx/server"
	"github.com/odvcencio/gosx/session"
)

func TestScanDirDiscoversPagesAndSpecialFiles(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", `package docs

func Page() Node {
	return <main>Home</main>
}
`)
	writeRouteFile(t, root, "guides/index.html", `<main>Guides</main>`)
	writeRouteFile(t, root, "blog/[slug]/page.html", `<main>Blog</main>`)
	writeRouteFile(t, root, "not-found.html", `<main>Missing</main>`)
	writeRouteFile(t, root, "error.html", `<main>Error</main>`)

	bundle, err := ScanDir(root)
	if err != nil {
		t.Fatal(err)
	}

	if bundle.NotFound == nil || !strings.HasSuffix(bundle.NotFound.FilePath, "not-found.html") {
		t.Fatalf("expected not-found page, got %+v", bundle.NotFound)
	}
	if bundle.Error == nil || !strings.HasSuffix(bundle.Error.FilePath, "error.html") {
		t.Fatalf("expected error page, got %+v", bundle.Error)
	}

	patterns := []string{}
	for _, page := range bundle.Pages {
		patterns = append(patterns, page.Pattern)
	}
	expected := []string{"/", "/blog/{slug}", "/guides"}
	for _, want := range expected {
		if !contains(patterns, want) {
			t.Fatalf("expected discovered pattern %q, got %v", want, patterns)
		}
	}
}

func TestDefaultFileRendererRendersGSXPage(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.gsx")
	if err := os.WriteFile(path, []byte(`package docs

func Page() Node {
	return <section><h1>Hello</h1><p>World</p></section>
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	node, err := DefaultFileRenderer(nil, FilePage{FilePath: path, Pattern: "/"})
	if err != nil {
		t.Fatal(err)
	}

	html := gosx.RenderHTML(node)
	if !strings.Contains(html, "<h1>Hello</h1>") {
		t.Fatalf("unexpected rendered gsx html %q", html)
	}
}

func TestDefaultFileRendererRendersLiteralExpressionText(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.gsx")
	source := "package docs\n\nfunc Page() Node {\n\treturn <pre>{`router.Add(\"/blog/{slug}\")`}</pre>\n}\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	node, err := DefaultFileRenderer(nil, FilePage{FilePath: path, Pattern: "/"})
	if err != nil {
		t.Fatal(err)
	}

	html := gosx.RenderHTML(node)
	if !strings.Contains(html, `router.Add(&#34;/blog/{slug}&#34;)`) {
		t.Fatalf("unexpected rendered literal expression html %q", html)
	}
}

func TestDefaultFileRendererRendersLocalComponents(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.gsx")
	source := `package docs

func Card(props any) Node {
	return <section class="card">
		<h2>{props.Title}</h2>
		{children}
	</section>
}

func Page() Node {
	return <Card Title="Hello">
		<p>World</p>
	</Card>
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	node, err := DefaultFileRenderer(nil, FilePage{FilePath: path, Pattern: "/"})
	if err != nil {
		t.Fatal(err)
	}

	html := gosx.RenderHTML(node)
	for _, snippet := range []string{
		`<section class="card">`,
		`<h2>Hello</h2>`,
		`<p>World</p>`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in rendered local component html %q", snippet, html)
		}
	}
}

func TestDefaultFileRendererSupportsIfEachAndLinkBuiltins(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.gsx")
	source := `package docs

func Page() Node {
	return <main>
		<If when={data.show}>
			<span>{data.label}</span>
		</If>
		<ul>
			<Each as="item" index="i" of={data.items}>
				<li data-index={i}>{item}</li>
			</Each>
		</ul>
		<Link href="/docs">Docs</Link>
	</main>
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := &RouteContext{
		Data: map[string]any{
			"show":  true,
			"label": "Visible",
			"items": []string{"alpha", "beta"},
		},
	}
	node, err := DefaultFileRenderer(ctx, FilePage{FilePath: path, Pattern: "/"})
	if err != nil {
		t.Fatal(err)
	}

	html := gosx.RenderHTML(node)
	for _, snippet := range []string{
		`<span>Visible</span>`,
		`<li data-index="0">alpha</li>`,
		`<li data-index="1">beta</li>`,
		`href="/docs"`,
		`data-gosx-link`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in rendered builtin html %q", snippet, html)
		}
	}
}

func TestDefaultFileRendererSupportsSpreadAttrsOnElementsAndLinks(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.gsx")
	source := `package docs

func Page() Node {
	return <main>
		<a {...data.anchor}>Open scene</a>
		<Link {...data.link}>Docs</Link>
	</main>
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := &RouteContext{
		Data: map[string]any{
			"anchor": map[string]any{
				"href":      "/demo/3d/geometry-zoo",
				"className": "hero-link",
			},
			"link": map[string]any{
				"href":  "/docs",
				"title": "Docs home",
			},
		},
	}

	node, err := DefaultFileRenderer(ctx, FilePage{FilePath: path, Pattern: "/"})
	if err != nil {
		t.Fatal(err)
	}

	html := gosx.RenderHTML(node)
	for _, snippet := range []string{
		`href="/demo/3d/geometry-zoo"`,
		`class="hero-link"`,
		`href="/docs"`,
		`title="Docs home"`,
		`data-gosx-link`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in rendered spread html %q", snippet, html)
		}
	}
}

func TestDefaultFileRendererSupportsImageBuiltin(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.gsx")
	source := `package docs

func Page() Node {
	return <main>
		<Image
			alt="Sample artwork"
			sizes="100vw"
			class="demo-image"
			{...data.image}
		/>
	</main>
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := &RouteContext{
		Data: map[string]any{
			"image": map[string]any{
				"src":    "/paper-card.png",
				"widths": []int{320, 640, 960},
				"width":  960,
				"height": 624,
			},
		},
	}
	node, err := DefaultFileRenderer(ctx, FilePage{FilePath: path, Pattern: "/"})
	if err != nil {
		t.Fatal(err)
	}

	html := gosx.RenderHTML(node)
	for _, snippet := range []string{
		`class="demo-image"`,
		`src="/_gosx/image?h=624&amp;src=%2Fpaper-card.png&amp;w=960"`,
		`srcset="/_gosx/image?h=624&amp;src=%2Fpaper-card.png&amp;w=320 320w, /_gosx/image?h=624&amp;src=%2Fpaper-card.png&amp;w=640 640w, /_gosx/image?h=624&amp;src=%2Fpaper-card.png&amp;w=960 960w"`,
		`width="960"`,
		`height="624"`,
		`alt="Sample artwork"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in rendered image html %q", snippet, html)
		}
	}
}

func TestDefaultFileRendererSupportsEngineBuiltins(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.gsx")
	source := `package docs

func Page() Node {
	return <main>
		<Surface name="Whiteboard" jsExport="Whiteboard" class="board" props={data.board}>
			<div>Board fallback</div>
		</Surface>
		<Worker name="SearchIndexer" jsExport="SearchIndexer" props={data.job} />
		<Scene3D class="scene-shell" {...data.scene}>
			<div>Scene fallback</div>
		</Scene3D>
	</main>
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := &RouteContext{
		Data: map[string]any{
			"board": map[string]any{
				"room": "alpha",
			},
			"job": map[string]any{
				"index": "posts",
			},
			"scene": map[string]any{
				"width":  640,
				"height": 360,
				"scene": map[string]any{
					"objects": []map[string]any{
						{
							"kind":  "cube",
							"size":  1.6,
							"x":     0,
							"y":     0,
							"z":     0,
							"color": "#8de1ff",
						},
					},
				},
			},
		},
	}
	node, err := DefaultFileRenderer(ctx, FilePage{FilePath: path, Pattern: "/"})
	if err != nil {
		t.Fatal(err)
	}

	html := gosx.RenderHTML(node)
	for _, snippet := range []string{
		`class="board"`,
		`data-gosx-engine="Whiteboard"`,
		`data-gosx-engine-kind="surface"`,
		`class="scene-shell"`,
		`data-gosx-engine="GoSXScene3D"`,
		`data-gosx-scene3d`,
		`Scene fallback`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in rendered engine html %q", snippet, html)
		}
	}

	head := gosx.RenderHTML(ctx.Runtime().Head())
	for _, snippet := range []string{
		`gosx-manifest`,
		`Whiteboard`,
		`SearchIndexer`,
		`GoSXScene3D`,
		`bootstrap.js`,
	} {
		if !strings.Contains(head, snippet) {
			t.Fatalf("expected %q in engine runtime head %q", snippet, head)
		}
	}
}

func TestDefaultFileRendererDoesNotInjectDefaultSceneObjectsForProgramRef(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.gsx")
	source := `package docs

func Page() Node {
	return <Scene3D class="scene-shell" programRef="/api/runtime/scene-program">
		<div>Scene fallback</div>
	</Scene3D>
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := &RouteContext{}
	node, err := DefaultFileRenderer(ctx, FilePage{FilePath: path, Pattern: "/"})
	if err != nil {
		t.Fatal(err)
	}

	html := gosx.RenderHTML(node)
	if !strings.Contains(html, `data-gosx-engine="GoSXScene3D"`) {
		t.Fatalf("expected Scene3D mount shell in %q", html)
	}

	head := gosx.RenderHTML(ctx.Runtime().Head())
	if !strings.Contains(head, `/api/runtime/scene-program`) {
		t.Fatalf("expected programRef in runtime head %q", head)
	}
	if !strings.Contains(head, `/gosx/runtime.wasm`) {
		t.Fatalf("expected runtime wasm path in runtime head %q", head)
	}
	if strings.Contains(head, `"objects"`) || strings.Contains(head, `"kind":"cube"`) {
		t.Fatalf("expected runtime Scene3D manifest without injected default objects, got %q", head)
	}
}

func TestScanDirBuildsNestedLayoutsGroupsAndNearestErrorPages(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "layout.gsx", `package docs

func Layout() Node {
	return <div class="root"><Slot /></div>
}
`)
	writeRouteFile(t, root, "(marketing)/layout.gsx", `package docs

func Layout() Node {
	return <section class="marketing"><Slot /></section>
}
`)
	writeRouteFile(t, root, "(marketing)/about/page.gsx", `package docs

func Page() Node {
	return <main>About</main>
}
`)
	writeRouteFile(t, root, "docs/[slug]/page.gsx", `package docs

func Page() Node {
	return <main>Doc page</main>
}
`)
	writeRouteFile(t, root, "docs/[slug]/error.gsx", `package docs

func Page() Node {
	return <main>Doc error</main>
}
`)
	writeRouteFile(t, root, "docs/[slug]/not-found.gsx", `package docs

func Page() Node {
	return <main>Doc missing</main>
}
`)

	bundle, err := ScanDir(root)
	if err != nil {
		t.Fatal(err)
	}

	about, ok := findFilePage(bundle.Pages, "(marketing)/about/page.gsx")
	if !ok {
		t.Fatalf("expected grouped about page in %+v", bundle.Pages)
	}
	if about.Pattern != "/about" {
		t.Fatalf("expected hidden route group path, got %q", about.Pattern)
	}
	if len(about.Layouts) != 2 {
		t.Fatalf("expected root + group layouts, got %#v", about.Layouts)
	}
	if !strings.HasSuffix(about.Layouts[0], "layout.gsx") || !strings.Contains(about.Layouts[1], "(marketing)/layout.gsx") {
		t.Fatalf("unexpected layout chain %#v", about.Layouts)
	}

	docPage, ok := findFilePage(bundle.Pages, "docs/[slug]/page.gsx")
	if !ok {
		t.Fatalf("expected docs page in %+v", bundle.Pages)
	}
	if docPage.ErrorPage == nil || docPage.ErrorPage.Source != "docs/[slug]/error.gsx" {
		t.Fatalf("expected nearest error page, got %+v", docPage.ErrorPage)
	}
	if len(bundle.NotFoundScopes) != 1 || bundle.NotFoundScopes[0].Pattern != "/docs/{slug}" {
		t.Fatalf("expected scoped docs not-found, got %+v", bundle.NotFoundScopes)
	}
}

func TestRouterAddDirRegistersFileRoutesAndNotFound(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", `package docs

func Page() Node {
	return <main>Home</main>
}
`)
	writeRouteFile(t, root, "about/page.html", `<main>About</main>`)
	writeRouteFile(t, root, "not-found.gsx", `package docs

func Page() Node {
	return <main>Missing</main>
}
`)

	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Title("Docs"), ctx.Head(), body)
	})
	if err := router.AddDir(root, FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}

	handler := router.Build()

	req := httptest.NewRequest(http.MethodGet, "/about", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "About") {
		t.Fatalf("expected about page, got %q", w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/missing", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Missing") {
		t.Fatalf("expected file-based not found page, got %q", w.Body.String())
	}
}

func TestRouterAddDirComposesDefaultAndNestedFileLayoutsWithRouteGroups(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "layout.gsx", `package docs

func Layout() Node {
	return <div class="root"><Slot /></div>
}
`)
	writeRouteFile(t, root, "(marketing)/layout.gsx", `package docs

func Layout() Node {
	return <section class="marketing"><Slot /></section>
}
`)
	writeRouteFile(t, root, "(marketing)/about/page.html", `<main>About</main>`)

	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument("Docs", ctx.Head(), body)
	})
	if err := router.AddDir(root, FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}

	handler := router.Build()

	req := httptest.NewRequest(http.MethodGet, "/about", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	for _, snippet := range []string{
		"<title>Docs</title>",
		`<div class="root">`,
		`<section class="marketing">`,
		`<main>About</main>`,
	} {
		if !strings.Contains(w.Body.String(), snippet) {
			t.Fatalf("expected %q in %q", snippet, w.Body.String())
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/marketing/about", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected grouped path to stay hidden, got %d", w.Code)
	}
}

func TestRouterAddDirAutomaticallyIncludesSidecarCSSForLayoutsAndPages(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "layout.gsx", `package docs

func Layout() Node {
	return <div class="root"><Slot /></div>
}
`)
	writeRouteFile(t, root, "layout.css", `.root { background: linen; }`)
	writeRouteFile(t, root, "docs/layout.gsx", `package docs

func Layout() Node {
	return <section class="docs-shell"><Slot /></section>
}
`)
	writeRouteFile(t, root, "docs/layout.css", `.docs-shell { border: 1px solid tan; }`)
	writeRouteFile(t, root, "docs/page.gsx", `package docs

func Page() Node {
	return <main class="page">Styled docs page</main>
}
`)
	writeRouteFile(t, root, "docs/page.css", `.page { color: sienna; }`)

	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument("Docs", ctx.Head(), body)
	})
	if err := router.AddDir(root, FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	w := httptest.NewRecorder()
	router.Build().ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`data-gosx-file-css="layout.css"`,
		`.root { background: linen; }`,
		`.docs-shell { border: 1px solid tan; }`,
		`.page { color: sienna; }`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
	if !(strings.Index(body, `.root { background: linen; }`) < strings.Index(body, `.docs-shell { border: 1px solid tan; }`) &&
		strings.Index(body, `.docs-shell { border: 1px solid tan; }`) < strings.Index(body, `.page { color: sienna; }`)) {
		t.Fatalf("expected outer layout CSS before nested layout CSS before page CSS in %q", body)
	}
}

func TestRouterAddDirAppliesSidecarMetadataFromLayoutsAndPages(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "layout.gsx", `package docs

func Layout() Node {
	return <div class="root"><Slot /></div>
}
`)
	writeRouteFile(t, root, "layout.meta.json", `{"canonical":"https://gosx.dev/docs"}`)
	writeRouteFile(t, root, "docs/layout.gsx", `package docs

func Layout() Node {
	return <section class="docs-shell"><Slot /></section>
}
`)
	writeRouteFile(t, root, "docs/layout.meta.json", `{"description":"Nested docs description"}`)
	writeRouteFile(t, root, "docs/page.gsx", `package docs

func Page() Node {
	return <main class="page">Metadata docs page</main>
}
`)
	writeRouteFile(t, root, "docs/page.meta.json", `{"title":"Sidecar Metadata Title"}`)

	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Title("Fallback"), ctx.Head(), body)
	})
	if err := router.AddDir(root, FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	w := httptest.NewRecorder()
	router.Build().ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `<title>Sidecar Metadata Title</title>`) {
		t.Fatalf("expected sidecar title in %q", body)
	}
	for _, snippet := range []string{
		`name="description"`,
		`content="Nested docs description"`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
	if !strings.Contains(body, `rel="canonical"`) || !strings.Contains(body, `href="https://gosx.dev/docs"`) {
		t.Fatalf("expected canonical link in %q", body)
	}
}

func TestRouterAddDirSupportsDynamicSegments(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "blog/[slug]/page.html", `<main>Dynamic</main>`)

	router := NewRouter()
	if err := router.AddDir(root, FileRoutesOptions{
		Render: func(ctx *RouteContext, page FilePage) (gosx.Node, error) {
			return gosx.Text(page.Pattern + ":" + ctx.Param("slug")), nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	handler := router.Build()
	req := httptest.NewRequest(http.MethodGet, "/blog/hello-world", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if body := w.Body.String(); body != "/blog/{slug}:hello-world" {
		t.Fatalf("unexpected dynamic route body %q", body)
	}
}

func TestBuildFileRouteSkipsGroupsAndCapturesCatchAll(t *testing.T) {
	routePath, pattern, params := buildFileRoute([]string{"(marketing)", "docs", "[...slug]"})
	if routePath != "/docs/{slug...}" || pattern != "/docs/{slug...}" {
		t.Fatalf("unexpected route values: path=%q pattern=%q", routePath, pattern)
	}
	if len(params) != 1 || params[0] != "slug" {
		t.Fatalf("unexpected params: %#v", params)
	}
}

func TestRouterAddDirUsesScopedNotFoundWithDynamicParams(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "layout.gsx", `package docs

func Layout() Node {
	return <div class="root"><Slot /></div>
}
`)
	writeRouteFile(t, root, "docs/layout.gsx", `package docs

func Layout() Node {
	return <section class="docs-shell"><Slot /></section>
}
`)
	writeRouteFile(t, root, "not-found.gsx", `package docs

func Page() Node {
	return <main class="missing-root">Missing root</main>
}
`)
	writeRouteFile(t, root, "docs/[slug]/not-found.gsx", `package docs

func Page() Node {
	return <main class="missing-doc">Missing {params.slug}</main>
}
`)

	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument("Docs", ctx.Head(), body)
	})
	if err := router.AddDir(root, FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}

	handler := router.Build()

	req := httptest.NewRequest(http.MethodGet, "/docs/routing/missing", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	for _, snippet := range []string{
		`class="missing-doc">Missing routing</main>`,
		`<div class="root">`,
		`<section class="docs-shell">`,
	} {
		if !strings.Contains(w.Body.String(), snippet) {
			t.Fatalf("expected %q in %q", snippet, w.Body.String())
		}
	}
	if strings.Contains(w.Body.String(), "Missing root") {
		t.Fatalf("expected scoped not-found to win, got %q", w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/missing", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `class="missing-root">Missing root</main>`) {
		t.Fatalf("expected root not-found, got %q", w.Body.String())
	}
}

func TestRouterAddDirAppliesFileModuleLoadMetadataAndRender(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "crew/[team]/[member]/page.gsx", `package docs

func Page() Node {
	return <main>Ignored by module render</main>
}
`)

	modules := NewFileModuleRegistry()
	if err := modules.Register(FileModuleFor("crew/[team]/[member]/page.gsx", FileModuleOptions{
		Load: func(ctx *RouteContext, page FilePage) (any, error) {
			return map[string]string{
				"team":   ctx.Param("team"),
				"member": ctx.Param("member"),
				"source": page.Source,
			}, nil
		},
		Metadata: func(ctx *RouteContext, page FilePage, data any) (server.Metadata, error) {
			values := data.(map[string]string)
			return server.Metadata{
				Title:       values["member"] + " | " + values["team"],
				Description: page.Source,
			}, nil
		},
		Render: func(ctx *RouteContext, page FilePage, data any) (gosx.Node, error) {
			values := data.(map[string]string)
			return gosx.El("main", gosx.Text(values["team"]+":"+values["member"]+":"+values["source"])), nil
		},
	})); err != nil {
		t.Fatal(err)
	}

	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Title("Docs"), ctx.Head(), body)
	})
	if err := router.AddDir(root, FileRoutesOptions{Modules: modules}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/crew/platform/draco", nil)
	w := httptest.NewRecorder()
	router.Build().ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		"<title>draco | platform</title>",
		`content="crew/[team]/[member]/page.gsx"`,
		"platform:draco:crew/[team]/[member]/page.gsx",
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
}

func TestRouterAddDirAppliesFileModuleTemplateBindings(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "crew/page.gsx", `package docs

func Page() Node {
	return <main class="crew-page" data-brand={brand}>
		<Hero title={data.title} />
		<MemberCard team={data.team} member={data.member} />
		{Lead(data.member)}
	</main>
}
`)

	modules := NewFileModuleRegistry()
	if err := modules.Register(FileModuleFor("crew/page.gsx", FileModuleOptions{
		Load: func(ctx *RouteContext, page FilePage) (any, error) {
			return map[string]string{
				"title":  "Platform Crew",
				"team":   "platform",
				"member": "draco",
			}, nil
		},
		Bindings: func(ctx *RouteContext, page FilePage, data any) FileTemplateBindings {
			return FileTemplateBindings{
				Values: map[string]any{
					"brand": "GoSX Crew",
				},
				Funcs: map[string]any{
					"Lead": func(name string) gosx.Node {
						return gosx.El("p", gosx.Attrs(gosx.Attr("class", "lead")), gosx.Text("Hello "+strings.ToUpper(name)))
					},
				},
				Components: map[string]any{
					"Hero": func(props struct{ Title string }) gosx.Node {
						return gosx.El("section", gosx.Attrs(gosx.Attr("class", "hero")),
							gosx.El("h1", gosx.Text(props.Title)),
						)
					},
					"MemberCard": func(team string, member string) gosx.Node {
						return gosx.El("article", gosx.Attrs(gosx.Attr("class", "member-card")),
							gosx.Text(team+":"+member),
						)
					},
				},
			}
		},
	})); err != nil {
		t.Fatal(err)
	}

	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument("Crew", ctx.Head(), body)
	})
	if err := router.AddDir(root, FileRoutesOptions{Modules: modules}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/crew", nil)
	w := httptest.NewRecorder()
	router.Build().ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`class="crew-page" data-brand="GoSX Crew"`,
		`<section class="hero"><h1>Platform Crew</h1></section>`,
		`<article class="member-card">platform:draco</article>`,
		`<p class="lead">Hello DRACO</p>`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
}

func TestRouterAddDirAutoResolvesGoComponentsFromFuncsAndValues(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "crew/page.gsx", `package docs

func Page() Node {
	return <main class="crew-page">
		<Hero title={data.title} />
		<cms.MemberCard team={data.team} member={data.member} />
	</main>
}
`)

	modules := NewFileModuleRegistry()
	if err := modules.Register(FileModuleFor("crew/page.gsx", FileModuleOptions{
		Load: func(ctx *RouteContext, page FilePage) (any, error) {
			return map[string]string{
				"title":  "Platform Crew",
				"team":   "platform",
				"member": "draco",
			}, nil
		},
		Bindings: func(ctx *RouteContext, page FilePage, data any) FileTemplateBindings {
			return FileTemplateBindings{
				Values: map[string]any{
					"cms": map[string]any{
						"MemberCard": func(props struct {
							Team   string
							Member string
						}) gosx.Node {
							return gosx.El("article", gosx.Attrs(gosx.Attr("class", "member-card")),
								gosx.Text(props.Team+":"+props.Member),
							)
						},
					},
				},
				Funcs: map[string]any{
					"Hero": func(props struct{ Title string }) gosx.Node {
						return gosx.El("section", gosx.Attrs(gosx.Attr("class", "hero")),
							gosx.El("h1", gosx.Text(props.Title)),
						)
					},
				},
			}
		},
	})); err != nil {
		t.Fatal(err)
	}

	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument("Crew", ctx.Head(), body)
	})
	if err := router.AddDir(root, FileRoutesOptions{Modules: modules}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/crew", nil)
	w := httptest.NewRecorder()
	router.Build().ServeHTTP(w, req)

	body := w.Body.String()
	for _, snippet := range []string{
		`class="crew-page"`,
		`<section class="hero"><h1>Platform Crew</h1></section>`,
		`<article class="member-card">platform:draco</article>`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
}

func TestRouterAddDirUsesNearestDirectoryErrorPage(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "layout.gsx", `package docs

func Layout() Node {
	return <div class="root"><Slot /></div>
}
`)
	writeRouteFile(t, root, "docs/layout.gsx", `package docs

func Layout() Node {
	return <section class="docs-shell"><Slot /></section>
}
`)
	writeRouteFile(t, root, "error.gsx", `package docs

func Page() Node {
	return <main>Root Error</main>
}
`)
	writeRouteFile(t, root, "docs/error.gsx", `package docs

func Page() Node {
	return <main>Docs Error</main>
}
`)
	writeRouteFile(t, root, "docs/broken/page.gsx", `package docs

func Page() Node {
	return <main>Broken</main>
}
`)

	modules := NewFileModuleRegistry()
	if err := modules.Register(FileModuleFor("docs/broken/page.gsx", FileModuleOptions{
		Render: func(ctx *RouteContext, page FilePage, data any) (gosx.Node, error) {
			return gosx.Node{}, fmt.Errorf("boom")
		},
	})); err != nil {
		t.Fatal(err)
	}

	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument("Docs", ctx.Head(), body)
	})
	if err := router.AddDir(root, FileRoutesOptions{Modules: modules}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/docs/broken", nil)
	w := httptest.NewRecorder()
	router.Build().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	for _, snippet := range []string{
		"Docs Error",
		`<div class="root">`,
		`<section class="docs-shell">`,
		`<main>Docs Error</main>`,
	} {
		if !strings.Contains(w.Body.String(), snippet) {
			t.Fatalf("expected %q in %q", snippet, w.Body.String())
		}
	}
	if strings.Contains(w.Body.String(), "Root Error") {
		t.Fatalf("expected nearest error page, got %q", w.Body.String())
	}
}

func TestRouterAddDirFileModulesCanEmitCacheHeadersAndRevalidate(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", `package docs

func Page() Node {
	return <main>Cached</main>
}
`)

	modules := NewFileModuleRegistry()
	if err := modules.Register(FileModuleFor("page.gsx", FileModuleOptions{
		Metadata: func(ctx *RouteContext, page FilePage, data any) (server.Metadata, error) {
			ctx.CachePublic(time.Minute)
			ctx.CacheTag("docs-pages")
			return server.Metadata{}, nil
		},
	})); err != nil {
		t.Fatal(err)
	}

	router := NewRouter()
	if err := router.AddDir(root, FileRoutesOptions{Modules: modules}); err != nil {
		t.Fatal(err)
	}

	handler := router.Build()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected etag in %v", w.Header())
	}

	notModifiedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	notModifiedReq.Header.Set("If-None-Match", etag)
	notModifiedRes := httptest.NewRecorder()
	handler.ServeHTTP(notModifiedRes, notModifiedReq)
	if notModifiedRes.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d: %s", notModifiedRes.Code, notModifiedRes.Body.String())
	}

	router.RevalidateTag("docs-pages")

	updatedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	updatedReq.Header.Set("If-None-Match", etag)
	updatedRes := httptest.NewRecorder()
	handler.ServeHTTP(updatedRes, updatedReq)
	if updatedRes.Code != http.StatusOK {
		t.Fatalf("expected 200 after revalidate, got %d: %s", updatedRes.Code, updatedRes.Body.String())
	}
}

func TestRouterAddDirAppliesInheritedDirectoryRouteConfig(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "route.config.json", `{"headers":{"X-App-Scope":"root"}}`)
	writeRouteFile(t, root, "docs/route.config.json", `{
  "cache": {
    "public": true,
    "maxAge": "45s",
    "staleWhileRevalidate": "5m"
  },
  "cacheTags": ["docs-pages"],
  "headers": {
    "X-Docs-Scope": "docs"
  }
}`)
	writeRouteFile(t, root, "docs/page.gsx", `package docs

func Page() Node {
	return <main>Docs</main>
}
`)

	router := NewRouter()
	if err := router.AddDir(root, FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}

	handler := router.Build()

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-App-Scope"); got != "root" {
		t.Fatalf("expected root header, got %q", got)
	}
	if got := w.Header().Get("X-Docs-Scope"); got != "docs" {
		t.Fatalf("expected docs header, got %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=45, stale-while-revalidate=300" {
		t.Fatalf("unexpected cache-control %q", got)
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected etag in %v", w.Header())
	}

	notModifiedReq := httptest.NewRequest(http.MethodGet, "/docs", nil)
	notModifiedReq.Header.Set("If-None-Match", etag)
	notModifiedRes := httptest.NewRecorder()
	handler.ServeHTTP(notModifiedRes, notModifiedReq)
	if notModifiedRes.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d: %s", notModifiedRes.Code, notModifiedRes.Body.String())
	}

	router.RevalidateTag("docs-pages")

	updatedReq := httptest.NewRequest(http.MethodGet, "/docs", nil)
	updatedReq.Header.Set("If-None-Match", etag)
	updatedRes := httptest.NewRecorder()
	handler.ServeHTTP(updatedRes, updatedReq)
	if updatedRes.Code != http.StatusOK {
		t.Fatalf("expected 200 after revalidate, got %d: %s", updatedRes.Code, updatedRes.Body.String())
	}
}

func TestRouterAddDirAppliesDirectoryModulesToPagesAndActions(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "docs/page.gsx", `package docs

func Page() Node {
	return <main>Docs</main>
}
`)

	modules := NewFileModuleRegistry()
	if err := modules.Register(FileModuleFor("docs/page.gsx", FileModuleOptions{
		Actions: FileActions{
			"save": func(ctx *action.Context) error {
				return ctx.Success("saved", nil)
			},
		},
	})); err != nil {
		t.Fatal(err)
	}

	dirModules := NewDirModuleRegistry()
	if err := dirModules.Register(DirModuleFor(root, DirModuleOptions{
		Middleware: []Middleware{
			func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-App-Middleware", "root")
					next.ServeHTTP(w, r)
				})
			},
		},
	})); err != nil {
		t.Fatal(err)
	}
	if err := dirModules.Register(DirModuleFor("docs", DirModuleOptions{
		Middleware: []Middleware{
			func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-Docs-Middleware", "docs")
					next.ServeHTTP(w, r)
				})
			},
		},
		Configure: func(ctx *RouteContext, page FilePage) error {
			ctx.Header().Set("X-Docs-Configured", page.Source)
			return nil
		},
	})); err != nil {
		t.Fatal(err)
	}

	router := NewRouter()
	if err := router.AddDir(root, FileRoutesOptions{
		Modules:    modules,
		DirModules: dirModules,
	}); err != nil {
		t.Fatal(err)
	}

	handler := router.Build()

	pageReq := httptest.NewRequest(http.MethodGet, "/docs", nil)
	pageRes := httptest.NewRecorder()
	handler.ServeHTTP(pageRes, pageReq)
	if pageRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", pageRes.Code, pageRes.Body.String())
	}
	if got := pageRes.Header().Get("X-App-Middleware"); got != "root" {
		t.Fatalf("expected root middleware header, got %q", got)
	}
	if got := pageRes.Header().Get("X-Docs-Middleware"); got != "docs" {
		t.Fatalf("expected docs middleware header, got %q", got)
	}
	if got := pageRes.Header().Get("X-Docs-Configured"); got != "docs/page.gsx" {
		t.Fatalf("expected docs configure header, got %q", got)
	}

	actionReq := httptest.NewRequest(http.MethodPost, "/docs/__actions/save", nil)
	actionReq.Header.Set("Accept", "application/json")
	actionRes := httptest.NewRecorder()
	handler.ServeHTTP(actionRes, actionReq)
	if actionRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", actionRes.Code, actionRes.Body.String())
	}
	if got := actionRes.Header().Get("X-App-Middleware"); got != "root" {
		t.Fatalf("expected root middleware header on action, got %q", got)
	}
	if got := actionRes.Header().Get("X-Docs-Middleware"); got != "docs" {
		t.Fatalf("expected docs middleware header on action, got %q", got)
	}
	if got := actionRes.Header().Get("X-Docs-Configured"); got != "" {
		t.Fatalf("expected no configure header on action, got %q", got)
	}
}

func TestRouterAddDirRegistersFileModuleActions(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "blog/[slug]/page.gsx", `package docs

func Page() Node {
	return <main>Blog</main>
}
`)

	modules := NewFileModuleRegistry()
	if err := modules.Register(FileModuleFor("blog/[slug]/page.gsx", FileModuleOptions{
		Actions: FileActions{
			"publish": func(ctx *action.Context) error {
				return ctx.Success("published", map[string]string{
					"slug": ctx.Request.PathValue("slug"),
					"name": ctx.FormData["name"],
				})
			},
		},
	})); err != nil {
		t.Fatal(err)
	}

	router := NewRouter()
	if err := router.AddDir(root, FileRoutesOptions{Modules: modules}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/blog/hello-world/__actions/publish", strings.NewReader("name=GoSX"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	router.Build().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode action response: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok response, got %#v", payload)
	}
	dataValue, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected action data object, got %#v", payload["data"])
	}
	if dataValue["slug"] != "hello-world" || dataValue["name"] != "GoSX" {
		t.Fatalf("unexpected action data %#v", dataValue)
	}
}

func TestFileLayoutWrapsRouteContent(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "layout.gsx", `package docs

func Layout() Node {
	return <div class="shell"><Slot /></div>
}
`)

	layout, err := FileLayout(filepath.Join(root, "layout.gsx"))
	if err != nil {
		t.Fatal(err)
	}

	router := NewRouter()
	router.SetLayout(layout)
	router.Add(Route{
		Pattern: "/",
		Handler: func(ctx *RouteContext) gosx.Node {
			return gosx.El("main", gosx.Text("Home"))
		},
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.Build().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `<div class="shell">`) || !strings.Contains(w.Body.String(), `<main>Home</main>`) {
		t.Fatalf("unexpected wrapped body %q", w.Body.String())
	}
}

func TestFileLayoutPrefersLayoutComponentOverHelpers(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "layout.gsx", `package docs

func NavButton(props any) Node {
	return <button>{props.Label}</button>
}

func Layout() Node {
	return <div class="shell"><Slot /></div>
}
`)

	layout, err := FileLayout(filepath.Join(root, "layout.gsx"))
	if err != nil {
		t.Fatal(err)
	}

	router := NewRouter()
	router.SetLayout(layout)
	router.Add(Route{
		Pattern: "/",
		Handler: func(ctx *RouteContext) gosx.Node {
			return gosx.El("main", gosx.Text("Home"))
		},
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.Build().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `<div class="shell">`) || !strings.Contains(w.Body.String(), `<main>Home</main>`) {
		t.Fatalf("unexpected helper-wrapped body %q", w.Body.String())
	}
}

func TestFileLayoutSupportsHTMLPlaceholder(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "layout.html", `<div class="shell">{{slot}}</div>`)

	layout, err := FileLayout(filepath.Join(root, "layout.html"))
	if err != nil {
		t.Fatal(err)
	}

	router := NewRouter()
	router.SetLayout(layout)
	router.Add(Route{
		Pattern: "/",
		Handler: func(ctx *RouteContext) gosx.Node {
			return gosx.El("p", gosx.Text("Docs"))
		},
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.Build().ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), `<div class="shell"><p>Docs</p></div>`) {
		t.Fatalf("unexpected html layout body %q", w.Body.String())
	}
}

func TestRouterFilePagesSupportRequestDataActionsAndCSRF(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "account/[slug]/page.gsx", `package docs

func Page() Node {
	return <main>
		<h1>{data.title}</h1>
		<p class="slug">{params.slug}</p>
		<p class="tab">{query.tab}</p>
		<form method="post" action={actionPath("save")}>
			<input type="hidden" name="csrf_token" value={csrf.token}></input>
			<input name="name" value={actions.save.values.name}></input>
			<input name="email" value={actions.save.values.email}></input>
			<p class="error">{actions.save.fieldErrors.email}</p>
			<p class="status">{action.message}</p>
		</form>
	</main>
}
`)

	modules := NewFileModuleRegistry()
	if err := modules.Register(FileModuleFor("account/[slug]/page.gsx", FileModuleOptions{
		Load: func(ctx *RouteContext, page FilePage) (any, error) {
			return map[string]string{
				"title": "Account " + ctx.Param("slug"),
			}, nil
		},
		Actions: FileActions{
			"save": func(ctx *action.Context) error {
				if strings.TrimSpace(ctx.FormData["email"]) == "" {
					return action.Validation("email is required", map[string]string{
						"email": "required",
					}, ctx.FormData)
				}
				return ctx.Success("saved", nil)
			},
		},
	})); err != nil {
		t.Fatal(err)
	}

	router := NewRouter()
	if err := router.AddDir(root, FileRoutesOptions{Modules: modules}); err != nil {
		t.Fatal(err)
	}

	sessions := session.MustNew("route-render-session-secret", session.Options{})
	handler := sessions.Middleware(sessions.Protect(router.Build()))

	getReq := httptest.NewRequest(http.MethodGet, "/account/draco?tab=security", nil)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRes.Code, getRes.Body.String())
	}

	body := getRes.Body.String()
	for _, snippet := range []string{
		"Account draco",
		`class="slug">draco</p>`,
		`class="tab">security</p>`,
		`action="/account/draco/__actions/save"`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}

	csrf := findInputValue(t, body, "csrf_token")
	if csrf == "" {
		t.Fatalf("expected csrf token in %q", body)
	}
	cookie := getRes.Result().Cookies()[0]

	postReq := httptest.NewRequest(http.MethodPost, "/account/draco/__actions/save", strings.NewReader("name=Ada&email=&csrf_token="+csrf))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(cookie)
	postRes := httptest.NewRecorder()
	handler.ServeHTTP(postRes, postReq)
	if postRes.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", postRes.Code, postRes.Body.String())
	}
	if location := postRes.Header().Get("Location"); location != "/account/draco" {
		t.Fatalf("expected redirect to page path, got %q", location)
	}

	cookie = postRes.Result().Cookies()[0]
	reloadReq := httptest.NewRequest(http.MethodGet, "/account/draco", nil)
	reloadReq.AddCookie(cookie)
	reloadRes := httptest.NewRecorder()
	handler.ServeHTTP(reloadRes, reloadReq)
	if reloadRes.Code != http.StatusOK {
		t.Fatalf("expected 200 after redirect, got %d: %s", reloadRes.Code, reloadRes.Body.String())
	}

	reloaded := reloadRes.Body.String()
	for _, snippet := range []string{
		`value="Ada"`,
		`class="error">required</p>`,
		`class="status">email is required</p>`,
	} {
		if !strings.Contains(reloaded, snippet) {
			t.Fatalf("expected %q in %q", snippet, reloaded)
		}
	}
}

func writeRouteFile(t *testing.T, root, rel, contents string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func findFilePage(pages []FilePage, source string) (FilePage, bool) {
	for _, page := range pages {
		if page.Source == source {
			return page, true
		}
	}
	return FilePage{}, false
}

func findInputValue(t *testing.T, html, name string) string {
	t.Helper()
	pattern := regexp.MustCompile(`name="` + regexp.QuoteMeta(name) + `" value="([^"]*)"`)
	matches := pattern.FindStringSubmatch(html)
	if len(matches) != 2 {
		return ""
	}
	return matches[1]
}
