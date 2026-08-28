package route

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// signalPageProps is the strict props type the tests below declare on a
// `component Page(props: signalPageProps)` entry — the shape a Load hook
// (route/filemodule.go's FileLoadFunc) must return for gosx#248's binding to
// prove it.
type signalPageProps struct {
	Scene string
	Down  int
}

const strictEntryPropsPageSource = `package docs

type PageProps struct {
	Scene string
	Down  int
}

component Page(props: PageProps) {
	return <main data-down={props.Down}>{props.Scene}</main>
}
`

// TestRenderFilePageBindsLoadReturnToStrictPageProps is the render-time proof
// for gosx#248: a strict Page entry that declares props renders its file
// module's own Load return value, proved through strictSpreadProps, the same
// boundary a nested <Component {...props}/> call re-runs. Byte assertion,
// not a compile-only check — see the task's warning about a fixture that
// once compiled a broken pattern without ever rendering it.
func TestRenderFilePageBindsLoadReturnToStrictPageProps(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", strictEntryPropsPageSource)

	module := FileModule{
		Source: "page.gsx",
		Load: func(ctx *RouteContext, page FilePage) (any, error) {
			return signalPageProps{Scene: "Fourth and Long", Down: 4}, nil
		},
	}
	ctx := &RouteContext{}
	page := FilePage{FilePath: root + "/page.gsx", Pattern: "/"}
	if err := prepareFileRouteContext(ctx, page, module, nil); err != nil {
		t.Fatalf("prepareFileRouteContext: %v", err)
	}
	node, err := renderFilePage(ctx, page, module, nil)
	if err != nil {
		t.Fatalf("renderFilePage: %v", err)
	}

	html := gosx.RenderHTML(node)
	const want = `<main data-down="4">Fourth and Long</main>`
	if html != want {
		t.Fatalf("rendered html = %q, want %q", html, want)
	}
}

// TestRouterAddDirBindsLoadReturnToStrictPageProps repeats the proof above
// through the full HTTP path (Router.AddDir -> Router.Build -> ServeHTTP),
// so the DataLoader/ctx.Data wiring route.go's buildHandler performs before
// calling the route Handler is covered too, not just the direct
// renderFilePage call.
func TestRouterAddDirBindsLoadReturnToStrictPageProps(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", strictEntryPropsPageSource)

	modules := NewFileModuleRegistry()
	if err := modules.Register(FileModuleFor("page.gsx", FileModuleOptions{
		Load: func(ctx *RouteContext, page FilePage) (any, error) {
			return signalPageProps{Scene: "Fourth and Long", Down: 4}, nil
		},
	})); err != nil {
		t.Fatal(err)
	}

	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node { return body })
	if err := router.AddDir(root, FileRoutesOptions{Modules: modules}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	router.Build().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", w.Code, w.Body.String())
	}
	const want = `<main data-down="4">Fourth and Long</main>`
	if w.Body.String() != want {
		t.Fatalf("body = %q, want %q", w.Body.String(), want)
	}
}

// TestRenderFilePageLegacyPageIgnoresEntryPropsMapLoad proves the framework
// side of gosx#248's byte-identity constraint: EntryProps now always carries
// ctx.Data (see renderFilePage), but a legacy (non-strict) component never
// reads it — renderFileProgramHTML's strict-entry branch is gated on
// comp.Syntax — so a legacy page.gsx with a Load hook returning
// map[string]any, the shape every existing app uses, renders exactly as it
// did before this change.
func TestRenderFilePageLegacyPageIgnoresEntryPropsMapLoad(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", `package docs

func Page() Node {
	return <main>{data.Scene}</main>
}
`)
	module := FileModule{
		Source: "page.gsx",
		Load: func(ctx *RouteContext, page FilePage) (any, error) {
			return map[string]any{"Scene": "Fourth and Long"}, nil
		},
	}
	ctx := &RouteContext{}
	page := FilePage{FilePath: root + "/page.gsx", Pattern: "/"}
	if err := prepareFileRouteContext(ctx, page, module, nil); err != nil {
		t.Fatalf("prepareFileRouteContext: %v", err)
	}
	node, err := renderFilePage(ctx, page, module, nil)
	if err != nil {
		t.Fatalf("renderFilePage: %v", err)
	}
	html := gosx.RenderHTML(node)
	const want = `<main>Fourth and Long</main>`
	if html != want {
		t.Fatalf("rendered html = %q, want %q", html, want)
	}
}

// TestRenderFilePageStrictPageRejectsMapLoadReturn is the first of the three
// required negative diagnostics: a strict Page entry whose Load hook
// returns map[string]any — every existing app's shape — fails closed with a
// message naming the component, its declared props type, and what to
// return instead of a map. It does not silently coerce the map into
// PageProps.
func TestRenderFilePageStrictPageRejectsMapLoadReturn(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", strictEntryPropsPageSource)

	module := FileModule{
		Source: "page.gsx",
		Load: func(ctx *RouteContext, page FilePage) (any, error) {
			return map[string]any{"Scene": "Fourth and Long", "Down": 4}, nil
		},
	}
	ctx := &RouteContext{}
	page := FilePage{FilePath: root + "/page.gsx", Pattern: "/"}
	if err := prepareFileRouteContext(ctx, page, module, nil); err != nil {
		t.Fatalf("prepareFileRouteContext: %v", err)
	}
	_, err := renderFilePage(ctx, page, module, nil)
	if err == nil {
		t.Fatal("renderFilePage unexpectedly accepted a map Load return for a strict entry")
	}
	for _, want := range []string{
		root + "/page.gsx",
		"strict render entry Page",
		"props PageProps",
		"Load returned map[string]interface {}",
		"return a PageProps value",
		"instead of a map",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to contain %q", err, want)
		}
	}
}

// TestRenderFilePageStrictPageRejectsWrongStructType is the second required
// negative diagnostic: a Load hook that returns a real, differently-typed
// struct — every field PageProps declares is present by name, but Down is a
// string instead of an int — still fails closed on the leaf-type mismatch,
// naming the file and the component, not just the mismatch. (A struct
// missing a field entirely is TestRenderFilePageStrictPageRejectsMissingField,
// the other required negative case.)
func TestRenderFilePageStrictPageRejectsWrongStructType(t *testing.T) {
	type unrelatedProps struct {
		Scene string
		Down  string // PageProps declares Down as int
	}
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", strictEntryPropsPageSource)

	module := FileModule{
		Source: "page.gsx",
		Load: func(ctx *RouteContext, page FilePage) (any, error) {
			return unrelatedProps{Scene: "Fourth and Long", Down: "4"}, nil
		},
	}
	ctx := &RouteContext{}
	page := FilePage{FilePath: root + "/page.gsx", Pattern: "/"}
	if err := prepareFileRouteContext(ctx, page, module, nil); err != nil {
		t.Fatalf("prepareFileRouteContext: %v", err)
	}
	_, err := renderFilePage(ctx, page, module, nil)
	if err == nil {
		t.Fatal("renderFilePage unexpectedly accepted a Load return of the wrong struct type")
	}
	for _, want := range []string{
		root + "/page.gsx",
		"render strict entry Page (props PageProps)",
		"prop Down (int)",
		"runtime value has type string, want exact int",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to contain %q", err, want)
		}
	}
}

// TestRenderFilePageStrictPageRejectsMissingField is the third required
// negative diagnostic: a Load hook that returns the right props type but
// omits one rendered field fails closed instead of zero-filling it, naming
// the file, the component, and the missing field.
func TestRenderFilePageStrictPageRejectsMissingField(t *testing.T) {
	type partialPageProps struct {
		Scene string
		// Down is missing, but the page renders props.Down.
	}
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", strictEntryPropsPageSource)

	module := FileModule{
		Source: "page.gsx",
		Load: func(ctx *RouteContext, page FilePage) (any, error) {
			return partialPageProps{Scene: "Fourth and Long"}, nil
		},
	}
	ctx := &RouteContext{}
	page := FilePage{FilePath: root + "/page.gsx", Pattern: "/"}
	if err := prepareFileRouteContext(ctx, page, module, nil); err != nil {
		t.Fatalf("prepareFileRouteContext: %v", err)
	}
	_, err := renderFilePage(ctx, page, module, nil)
	if err == nil {
		t.Fatal("renderFilePage unexpectedly accepted a Load return missing a rendered field")
	}
	for _, want := range []string{
		root + "/page.gsx",
		"render strict entry Page (props PageProps)",
		"partialPageProps has no field Down",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to contain %q", err, want)
		}
	}
}
