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

func TestFileTrailingSlashAliasMixedDynamicCatchAll(t *testing.T) {
	router := fileSlashTestRouter(t, []string{"[id]", "admin/[...path]"})
	checked, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	for name, handler := range map[string]http.Handler{"BuildChecked": checked, "Build": router.Build()} {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct{ path, want string }{
				{"/admin/", "admin/[...path]/page.gsx id= path= q="},
				{"/admin/chapter/", "admin/[...path]/page.gsx id= path=chapter/ q="},
				{"/item/?q=kept", "[id]/page.gsx id=item path= q=kept"},
			} {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
				if w.Code != http.StatusOK || w.Body.String() != tc.want {
					t.Fatalf("%s: status=%d body=%q want=%q", tc.path, w.Code, w.Body.String(), tc.want)
				}
			}
		})
	}
}

func TestFileTrailingSlashAliasRouteTrees(t *testing.T) {
	for _, tc := range []struct {
		name  string
		dirs  []string
		paths map[string]string
	}{
		{"nested_dynamic", []string{"users/[id]", "users/admin/[...path]"}, map[string]string{
			"/users/admin/": "users/admin/[...path]/page.gsx",
			"/users/42/":    "users/[id]/page.gsx id=42",
		}},
		{"literal_and_catch_all", []string{"admin", "admin/[...path]"}, map[string]string{
			"/admin":  "admin/page.gsx",
			"/admin/": "admin/[...path]/page.gsx",
		}},
		{"root_catch_all", []string{"notes", "[id]", "[...path]"}, map[string]string{
			"/notes":  "notes/page.gsx",
			"/notes/": "[...path]/page.gsx id= path=notes/",
			"/42/":    "[...path]/page.gsx id= path=42/",
		}},
		{"leaf_aliases", []string{"notes", "users/[id]"}, map[string]string{
			"/notes/":              "notes/page.gsx",
			"/users/a%2Fb/?q=kept": "users/[id]/page.gsx id=a/b path= q=kept",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, err := fileSlashTestRouter(t, tc.dirs).BuildChecked()
			if err != nil {
				t.Fatal(err)
			}
			for path, want := range tc.paths {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
				if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), want) {
					t.Errorf("%s: status=%d body=%q want prefix=%q", path, w.Code, w.Body.String(), want)
				}
			}
		})
	}
}

func TestFileTrailingSlashAliasPreservesCanonicalResponses(t *testing.T) {
	for _, tc := range []struct {
		name, pattern string
		status        int
	}{
		{"handler_not_found", "GET /admin/{$}", http.StatusNotFound},
		{"method_not_allowed", "POST /admin/{$}", http.StatusMethodNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := fileSlashTestRouter(t, []string{"admin"})
			router.Handle(tc.pattern, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}))
			handler, err := router.BuildChecked()
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/", nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%q", w.Code, tc.status, w.Body.String())
			}
		})
	}
}

func fileSlashTestRouter(t *testing.T, dirs []string) *Router {
	t.Helper()
	root := t.TempDir()
	for _, dir := range dirs {
		target := filepath.Join(root, dir)
		if err := os.MkdirAll(target, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, "page.gsx"), []byte("package app\nfunc Page() Node {\n return <p>page</p>\n}\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	router := NewRouter()
	if err := router.AddDir(root, FileRoutesOptions{Modules: NewFileModuleRegistry(), Render: func(ctx *RouteContext, page FilePage) (gosx.Node, error) {
		return gosx.Text(page.Source + " id=" + ctx.Param("id") + " path=" + ctx.Param("path") + " q=" + ctx.Query("q")), nil
	}}); err != nil {
		t.Fatal(err)
	}
	return router
}
