package route

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestDynamicFilePagesAcceptExactTrailingSlashWithoutExport(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"notes", "users/[id]", "docs/[...path]"} {
		target := filepath.Join(root, dir)
		if err := os.MkdirAll(target, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, "page.gsx"), []byte("package app\nfunc Page() Node {\n return <p>page</p>\n}\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	modules := NewFileModuleRegistry()
	loads := 0
	if err := modules.Register(FileModuleFor(filepath.Join(root, "notes", "page.gsx"), FileModuleOptions{Load: func(*RouteContext, FilePage) (any, error) { loads++; return nil, nil }})); err != nil {
		t.Fatal(err)
	}
	router := NewRouter()
	if err := router.AddDir(root, FileRoutesOptions{Modules: modules, Render: func(ctx *RouteContext, page FilePage) (gosx.Node, error) {
		if ctx.pattern != page.Pattern {
			return gosx.Node{}, fmt.Errorf("alias changed route identity: %q", ctx.pattern)
		}
		if _, ok := ctx.Params["$"]; ok {
			return gosx.Node{}, fmt.Errorf("end marker leaked into params")
		}
		return gosx.Text(page.RoutePath + " " + ctx.Param("id") + " " + ctx.Param("path") + " " + ctx.Query("q")), nil
	}}); err != nil {
		t.Fatal(err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, want string
		status     int
	}{
		{"/notes", "/notes", 200},
		{"/notes/?q=kept", "/notes   kept", 200},
		{"/users/42/", "/users/{id} 42", 200},
		{"/docs/chapter/", "chapter/", 200},
		{"/notes/unknown/", "", 404},
	} {
		for _, cookie := range []string{"", "session=visitor"} {
			t.Run(tc.path+cookie, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, tc.path, nil)
				req.Header.Set("Cookie", cookie)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.want) || w.Header().Get("Location") != "" {
					t.Fatalf("status=%d body=%q headers=%v", w.Code, w.Body.String(), w.Header())
				}
			})
		}
	}
	if loads != 4 {
		t.Fatalf("loader calls=%d want=4", loads)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/notes/", nil))
	if w.Code != 200 {
		t.Fatalf("HEAD status=%d", w.Code)
	}
}
