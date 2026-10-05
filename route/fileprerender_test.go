package route

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
)

func TestFileRoutesPrerenderUsesResolvedHooksBeforeLoading(t *testing.T) {
	for _, tc := range []struct {
		name, config          string
		load, actions, export bool
		want                  int
	}{
		{name: "static", export: true, want: 200},
		{name: "loader", load: true, export: true, want: 204},
		{name: "actions", actions: true, export: true, want: 204},
		{name: "opt-in-loader", load: true, config: `{"prerender":true}`, export: true, want: 200},
		{name: "opt-in-actions", actions: true, config: `{"prerender":true}`, export: true, want: 200},
		{name: "opt-out", config: `{"prerender":false}`, export: true, want: 204},
		{name: "live-loader", load: true, want: 200},
		{name: "live-opt-out", load: true, config: `{"prerender":false}`, want: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exportEnv := ""
			if tc.export {
				exportEnv = "1"
			}
			t.Setenv("GOSX_STATIC_EXPORT", exportEnv)
			root := t.TempDir()
			source := filepath.Join(root, "page.gsx")
			if err := os.WriteFile(source, []byte("package app\nfunc Page() Node { return <p>page</p> }\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if tc.config != "" {
				if err := os.WriteFile(filepath.Join(root, "route.config.json"), []byte(tc.config), 0600); err != nil {
					t.Fatal(err)
				}
			}
			loads := 0
			opts := FileModuleOptions{}
			if tc.load {
				opts.Load = func(*RouteContext, FilePage) (any, error) { loads++; return nil, nil }
			}
			if tc.actions {
				opts.Actions = FileActions{"save": func(*action.Context) error { return nil }}
			}
			modules := NewFileModuleRegistry()
			if err := modules.Register(FileModuleFor(source, opts)); err != nil {
				t.Fatal(err)
			}
			router := NewRouter()
			if err := router.AddDir(root, FileRoutesOptions{Modules: modules, Render: func(*RouteContext, FilePage) (gosx.Node, error) { return gosx.Text("page"), nil }}); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			router.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d", w.Code, tc.want)
			}
			if tc.want == 204 && (loads != 0 || w.Body.Len() != 0 || w.Header().Get("X-GoSX-Prerender") != "skip") {
				t.Fatalf("skipped page loaded or rendered: loads=%d headers=%v", loads, w.Header())
			}
			if tc.load && tc.want == 200 && loads != 1 {
				t.Fatalf("loads=%d want=1", loads)
			}
			if tc.export && tc.load && tc.want == 200 && w.Header().Get("X-GoSX-Prerender") != "load" {
				t.Fatal("missing opted-in loader metadata")
			}
			if !tc.export && w.Header().Get("X-GoSX-Prerender") != "" {
				t.Fatal("export metadata leaked into live responses")
			}
		})
	}
}
