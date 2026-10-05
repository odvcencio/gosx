package gosx_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/strictcheck"
)

func TestStrictProjectionLoaderModelsCheckBuildAndRender(t *testing.T) {
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.test/projection\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\nreplace m31labs.dev/gosx => " + filepath.ToSlash(root) + "\n",
		"page.gsx": `package app
type Author struct { Name string }
type NoteItem struct { Title string; Author Author }
type PageProps struct { Featured NoteItem; Notes []NoteItem }
component Page(props: PageProps) {
	return <main><h1>{props.Featured.Author.Name}</h1><Each of={props.Notes} as="note"><p>{note.Title}: {note.Author.Name}</p></Each></main>
}
`,
		"page.server.go": `package app
import "m31labs.dev/gosx/route"
type AuthorData struct { Name string }
type NoteItemData struct { Title string; Author AuthorData; Unused int }
type PageData struct { Featured NoteItemData; Notes []NoteItemData }
func Load(ctx *route.RouteContext, page route.FilePage) (any, error) {
	note := NoteItemData{Title: "A note", Author: AuthorData{Name: "Reader"}}
	return PageData{Featured: note, Notes: []NoteItemData{note}}, nil
}
`,
		"page_test.go": `package app
import (
	"net/http/httptest"
	"testing"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
)
func TestRender(t *testing.T) {
	modules := route.NewFileModuleRegistry()
	if err := modules.Register(route.FileModuleFor("page.gsx", route.FileModuleOptions{Load: Load})); err != nil { t.Fatal(err) }
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node { return body })
	if err := router.AddDir(".", route.FileRoutesOptions{Modules: modules}); err != nil { t.Fatal(err) }
	w := httptest.NewRecorder()
	router.Build().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || w.Body.String() != "<main><h1>Reader</h1><p>A note: Reader</p></main>" { t.Fatalf("status %d: %s", w.Code, w.Body.String()) }
}
`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := strictcheck.CheckFileWithOptions(context.Background(), filepath.Join(dir, "page.gsx"), strictcheck.Options{GOFLAGS: "-mod=mod"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"build", "./..."}, {"test", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, output)
		}
	}
}
