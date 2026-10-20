package route

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestFileRouteDevelopmentErrorPosition(t *testing.T) {
	for _, tc := range []struct {
		name, source, position, expression string
	}{
		{"strict prop mismatch", "package app\ntype Props struct { Count int }\ncomponent Page(props: Props) {\n\treturn <h2>{props.Count}</h2>\n}\n", "page.gsx:4:13:", "props.Count"},
		{"similar prop names", "package app\ntype Props struct { Count int; CountTotal int }\ncomponent Page(props: Props) {\n\treturn <h2>{props.CountTotal} {props.Count}</h2>\n}\n", "page.gsx:4:32:", "expression: props.Count</pre>"},
		{"malformed markup", "package app\nfunc Page() Node {\n\treturn <h2>Next steps</h3>\n}\n", "page.gsx:3:23:", "expected &lt;/h2&gt;"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeRouteFile(t, root, "page.gsx", tc.source)
			modules := NewFileModuleRegistry()
			modules.Register(FileModule{Source: "page.gsx", Load: func(*RouteContext, FilePage) (any, error) {
				return struct {
					Count      string
					CountTotal int
				}{Count: "invalid", CountTotal: 42}, nil
			}})
			router := NewRouter()
			if err := router.AddDir(root, FileRoutesOptions{Modules: modules}); err != nil {
				t.Fatal(err)
			}
			for _, dev := range []bool{true, false} {
				t.Run(map[bool]string{true: "dev", false: "production"}[dev], func(t *testing.T) {
					if dev {
						t.Setenv("GOSX_DEV", "1")
						t.Setenv("GOSX_ENV", "development")
					} else {
						t.Setenv("GOSX_DEV", "")
						t.Setenv("GOSX_ENV", "production")
					}
					w := httptest.NewRecorder()
					router.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
					if w.Code != http.StatusInternalServerError {
						t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
					}
					for _, detail := range []string{tc.position, tc.expression, filepath.Join(root, "page.gsx")} {
						if strings.Contains(w.Body.String(), detail) != dev {
							t.Fatalf("dev=%v, body %q has unexpected detail %q", dev, w.Body.String(), detail)
						}
					}
				})
			}
		})
	}
}

func TestStrictAttributeRenderErrorPosition(t *testing.T) {
	prog, err := gosx.Compile([]byte("package app\ntype Props struct { Count int }\ncomponent Badge(props: Props) {\n\treturn <b>{props.Count}</b>\n}\ncomponent Page() {\n\treturn <Badge count={42} />\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a typed caller's runtime value; the static literal was valid.
	for i := range prog.Nodes {
		if prog.Nodes[i].Tag == "Badge" {
			prog.Nodes[i].Attrs[0].Expr = "runtimeCount"
		}
	}
	_, err = RenderProgramComponent(prog, "Page", ProgramRenderEnv{Values: map[string]any{"runtimeCount": "invalid"}})
	var located *RenderError
	if !errors.As(err, &located) || located.Span.StartLine != 7 || located.Span.StartCol != 16 || located.Expression != "runtimeCount" {
		t.Fatalf("error=%v located=%+v", err, located)
	}
	if !strings.Contains(err.Error(), "want exact int") {
		t.Fatalf("missing prop mismatch: %v", err)
	}
}
