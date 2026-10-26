package route

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

const islandFileCounter = `package app

//gosx:island
func Counter(props any) Node {
	count := signal.New(props.Initial)
	increment := func() { count.Set(count.Get() + 1) }
	return <div class="counter">
		<span>{count.Get()}</span>
		<button onClick={increment}>+</button>
	</div>
}
`

func serveIslandFixture(t *testing.T, root string) string {
	t.Helper()
	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Document("Demo", body))
	})
	if err := router.AddDir(root, FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	router.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

func requireHydratingIsland(t *testing.T, body string) {
	t.Helper()
	for _, snippet := range []string{
		`data-gosx-island="Counter"`,
		`data-gosx-enhance="island"`,
		`data-gosx-on-click="increment"`,
		`<span>2</span>`,
		`<script id="gosx-manifest" type="application/json">`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected %q in %q", snippet, body)
		}
	}
}

// TestRouterAddDirHydratesIslandFromSiblingFile pins that an island declared
// in another .gsx file of the same directory resolves in page.gsx the way a
// same-package Go declaration would. It used to render as unknown static markup.
func TestRouterAddDirHydratesIslandFromSiblingFile(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "counter.gsx", islandFileCounter)
	writeRouteFile(t, root, "page.gsx", `package app

func Page() Node {
	return <main class="demo-page"><Counter Initial={2} /></main>
}
`)
	requireHydratingIsland(t, serveIslandFixture(t, root))
}

// TestRouterAddDirHydratesIslandFromSharedDirectory pins that an island in a
// directory imported by relative path (import ui "./ui") hydrates. It used to
// render its server markup with no island wrapper, manifest entry or program.
func TestRouterAddDirHydratesIslandFromSharedDirectory(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "ui/counter.gsx", islandFileCounter)
	writeRouteFile(t, root, "page.gsx", `package app

import ui "./ui"

func Page() Node {
	return <main class="demo-page"><ui.Counter Initial={2} /></main>
}
`)
	requireHydratingIsland(t, serveIslandFixture(t, root))
}

const islandFileStrictCounter = `package app

type CounterProps struct {
	Label string
	Initial int
}

//gosx:island
component Counter(props: CounterProps) {
	count := signal.New(props.Initial)
	increment := func() { count.Set(count.Get() + 1) }
	return <div class="counter">
		<b>{props.Label}</b>
		<span>{count.Get()}</span>
		<button onClick={increment}>+</button>
	</div>
}
`

// TestRouterAddDirHydratesStrictIslandFromOtherFiles covers strict islands,
// whose typed props are proven against the declaring file's own struct.
func TestRouterAddDirHydratesStrictIslandFromOtherFiles(t *testing.T) {
	cases := map[string]struct{ file, tag, imports string }{
		"sibling": {"counter.gsx", "Counter", ""},
		"shared":  {"ui/counter.gsx", "ui.Counter", "import ui \"./ui\"\n\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeRouteFile(t, root, tc.file, islandFileStrictCounter)
			writeRouteFile(t, root, "page.gsx", `package app

`+tc.imports+`func Page() Node {
	return <main class="demo-page"><`+tc.tag+` Label="Boats" Initial={2} /></main>
}
`)
			body := serveIslandFixture(t, root)
			requireHydratingIsland(t, body)
			if !strings.Contains(body, "<b>Boats</b>") {
				t.Fatalf("typed prop Label did not reach the island markup in %q", body)
			}
		})
	}
}

// TestRouterAddDirRendersStrictComponentFromSiblingFile pins that a plain
// strict component in a sibling file resolves too, with its children.
func TestRouterAddDirRendersStrictComponentFromSiblingFile(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "panel.gsx", `package app

type PanelProps struct {
	Title string
}

component Panel(props: PanelProps) {
	return <section><h2>{props.Title}</h2>{children}</section>
}
`)
	writeRouteFile(t, root, "page.gsx", `package app

func Page() Node {
	return <main><Panel Title="Crew"><p>inside</p></Panel></main>
}
`)
	body := serveIslandFixture(t, root)
	if want := `<section><h2>Crew</h2><p>inside</p></section>`; !strings.Contains(body, want) {
		t.Fatalf("expected %q in %q", want, body)
	}
}

// TestRouterAddDirSiblingLookupKeepsThePagePackage pins that sibling
// resolution follows the page's own package even when the alphabetically
// first sibling does not compile. A neighbour in another package that declares
// the same name must not win.
func TestRouterAddDirSiblingLookupKeepsThePagePackage(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "a_broken.gsx", "package app\n\nfunc Broken() Node {\n\treturn <div>\n}\n")
	writeRouteFile(t, root, "b_other.gsx", "package other\n\n//gosx:island\nfunc Counter() Node {\n\tcount := signal.New(9)\n\treturn <div class=\"wrong-package\"><span>{count.Get()}</span></div>\n}\n")
	writeRouteFile(t, root, "counter.gsx", islandFileCounter)
	writeRouteFile(t, root, "page.gsx", `package app

func Page() Node {
	return <main class="demo-page"><Counter Initial={2} /></main>
}
`)
	body := serveIslandFixture(t, root)
	requireHydratingIsland(t, body)
	if strings.Contains(body, "wrong-package") {
		t.Fatalf("resolved Counter from another package: %s", body)
	}
}
