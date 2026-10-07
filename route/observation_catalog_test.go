package route

import (
	"net/http"
	"reflect"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/server"
)

func TestBuiltRouterObservationPatterns(t *testing.T) {
	r := NewRouter()
	r.Add(Route{Pattern: "/game", Children: []Route{{Pattern: "/round/{id}", Handler: func(*RouteContext) gosx.Node { return gosx.Text("round") }}}})
	r.Handle("GET /raw", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r.handleKind("action", "POST /game/__actions/{__gosx_action}", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h, err := r.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	p := h.(server.ObservationCatalogProvider)
	got, overflow := p.ObservationPatterns(0)
	want := []server.ObservationPattern{
		{Kind: "action", Pattern: "/game/__actions/{__gosx_action}", Methods: []string{"POST"}},
		{Kind: "error", Pattern: "/game/round/{id}", Methods: []string{"*"}},
		{Kind: "mount", Pattern: "/raw", Methods: []string{"GET", "HEAD"}},
		{Kind: "page", Pattern: "/game/round/{id}", Methods: []string{"*"}},
	}
	if overflow || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v overflow=%v", got, overflow)
	}
	got[0].Methods[0] = "changed"
	again, _ := p.ObservationPatterns(0)
	if again[0].Methods[0] != "POST" {
		t.Fatal("catalog is not private")
	}
	limited, overflow := p.ObservationPatterns(2)
	if !overflow || !reflect.DeepEqual(limited, want[:2]) {
		t.Fatalf("bounded: %#v overflow=%v", limited, overflow)
	}
}

func TestFileActionCatalogUsesDeclaredKind(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "round/[id]/page.gsx", `package game
func Page() Node { return <main>Round</main> }
`)
	modules := NewFileModuleRegistry()
	if err := modules.Register(FileModuleFor("round/[id]/page.gsx", FileModuleOptions{
		Actions: FileActions{"finish": func(*action.Context) error { return nil }},
	})); err != nil {
		t.Fatal(err)
	}
	r := NewRouter()
	if err := r.AddDir(root, FileRoutesOptions{Modules: modules}); err != nil {
		t.Fatal(err)
	}
	rows, _ := r.Build().(server.ObservationCatalogProvider).ObservationPatterns(0)
	if len(rows) != 3 || rows[0].Kind != "action" || rows[0].Pattern != "/round/{id}/__actions/{__gosx_action}" || !reflect.DeepEqual(rows[0].Methods, []string{"POST"}) {
		t.Fatalf("action catalog: %#v", rows)
	}
}
