package main

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/strictcheck"
)

func TestStaticStarterErrorStatuses(t *testing.T) {
	for _, template := range []string{"app", "docs"} {
		t.Run(template, func(t *testing.T) {
			dir := t.TempDir()
			files, err := scaffoldFilesForTemplate("example.com/status-pages", template)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				if file.Path != "app/not-found.gsx" && file.Path != "app/error.gsx" {
					continue
				}
				if err := os.WriteFile(filepath.Join(dir, filepath.Base(file.Path)), []byte(file.Contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(filepath.Join(dir, "broken"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "broken", "page.gsx"), []byte("package app\nfunc Page() Node {\n return <unclosed\n}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			router := route.NewRouter()
			if err := router.AddDir(dir, route.FileRoutesOptions{}); err != nil {
				t.Fatal(err)
			}
			handler := router.Build()
			for path, status := range map[string]int{"/missing": 404, "/broken": 500} {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				if w.Code != status {
					t.Fatalf("%s status=%d want%d: %s", path, w.Code, status, w.Body.String())
				}
				want := "Page not found"
				if template == "docs" {
					want = "The page you asked for is not in this route tree."
				}
				if status == 500 {
					want = "Something broke"
					if template == "docs" {
						want = "This docs request failed before it could render cleanly."
					}
				}
				if !strings.Contains(w.Body.String(), want) {
					t.Fatalf("%s missing %q: %s", path, want, w.Body.String())
				}
			}
		})
	}
}

func TestStaticStarterPages(t *testing.T) {
	for _, template := range []string{"app", "docs"} {
		t.Run(template, func(t *testing.T) {
			dir := t.TempDir()
			if err := RunInit(dir, "example.com/static-pages", template); err != nil {
				t.Fatal(err)
			}
			mod := readFile(t, filepath.Join(dir, "go.mod"))
			repo := localGoSXReplacePath()
			if repo == "" || !strings.Contains(mod, "replace m31labs.dev/gosx => "+repo+"\n") {
				t.Fatal("missing current core replacement")
			}
			t.Logf("core replacement: %s", repo)
			cases := []struct{ name, path, entry string }{{"app-not-found", "app/not-found.gsx", "NotFound"}, {"app-error", "app/error.gsx", "Error"}}
			if template == "docs" {
				cases = []struct{ name, path, entry string }{{"docs-not-found", "app/not-found.gsx", "NotFound"}, {"docs-error", "app/error.gsx", "Error"}, {"docs-nested-not-found", "app/docs/not-found.gsx", "NotFound"}, {"docs-layout", "app/docs/layout.gsx", "Layout"}, {"docs-getting-started", "app/docs/getting-started/page.gsx", "Page"}, {"docs-routing", "app/docs/routing/page.gsx", "Page"}}
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					path := filepath.Join(dir, tc.path)
					src := readFile(t, path)
					p, err := gosx.Compile([]byte(src))
					if err != nil {
						t.Fatal(err)
					}
					if len(p.Components) != 1 || p.Components[0].Syntax != ir.ComponentSyntaxStrict || p.Components[0].Name != tc.entry {
						t.Fatalf("expected sole strict %s", tc.entry)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := strictcheck.CheckFileWithOptions(ctx, path, strictcheck.Options{Env: append(os.Environ(), "GOPROXY=off", "GOSUMDB=off"), GOWORK: "off", GOFLAGS: "-mod=readonly"}); err != nil {
						t.Fatal(err)
					}
					baseline := filepath.Join("testdata", "static-pages-baseline", tc.name+".gsx")
					if tc.entry == "Layout" {
						old, err := route.FileLayout(baseline)
						if err != nil {
							t.Fatal(err)
						}
						current, err := route.FileLayout(path)
						if err != nil {
							t.Fatal(err)
						}
						body := gosx.El("p", gosx.Text("<child>"))
						a, b := gosx.RenderHTML(old(&route.RouteContext{}, body)), gosx.RenderHTML(current(&route.RouteContext{}, body))
						if normalizeIntertagWhitespace(a) != normalizeIntertagWhitespace(b) {
							t.Fatalf("layout differs: %q != %q", a, b)
						}
					} else {
						var old bytes.Buffer
						if err := runRender(baseline, "", &old); err != nil {
							t.Fatal(err)
						}
						for _, entry := range []string{"", tc.entry} {
							var got bytes.Buffer
							if err := runRender(path, entry, &got); err != nil {
								t.Fatal(err)
							}
							if normalizeIntertagWhitespace(old.String()) != normalizeIntertagWhitespace(got.String()) {
								t.Fatalf("entry %q differs: %q != %q", entry, old.String(), got.String())
							}
						}
					}
				})
			}
		})
	}
}
