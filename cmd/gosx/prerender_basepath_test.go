package main

import (
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	runtimehost "m31labs.dev/gosx/client/runtime/host"
)

func TestPrerenderBasePathWithEitherProxyMode(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("build subprocess covered by test-cli")
	}
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/prefixed-export\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\n")
	addLocalGoSXReplace(t, dir)
	for _, rel := range []string{"app/page.gsx", "app/news/page.gsx", "app/news/details/page.gsx"} {
		mustWriteFile(t, filepath.Join(dir, rel), "package app\ncomponent Page() { return <p>export</p> }\n")
	}
	mustWriteFile(t, filepath.Join(dir, "public", "style.css"), "body { color: blue; }")
	mustWriteFile(t, filepath.Join(dir, "main.go"), `package main
import (
 "log"
 "net/http"
 "os"
 "m31labs.dev/gosx"
 "m31labs.dev/gosx/server"
)
func main() {
 app := server.New()
 app.EnableNavigation()
 if err := app.SetBasePath(os.Getenv("TEST_EXPORT_PREFIX"), server.BasePathOptions{ProxyStripsPrefix: os.Getenv("TEST_EXPORT_STRIPS") == "1"}); err != nil { log.Fatal(err) }
 app.SetPublicDir("public")
 app.Mount("/_gosx/css/page.css", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/css"); w.Write([]byte("p{color:blue}")) }))
 page := func(ctx *server.Context) gosx.Node {
  ctx.AddHead(gosx.El("link", gosx.Attrs(gosx.Attr("rel", "stylesheet"), gosx.Attr("href", "/_gosx/css/page.css"), gosx.Attr("data-gosx-file-css", "page"))))
  return gosx.Fragment(gosx.El("p", gosx.Attrs(gosx.Attr("data-internal-route", ctx.Request.URL.Path))), gosx.El("a", gosx.Attrs(gosx.Attr("href", "/news?q=1#top")), gosx.Text("News")), gosx.El("a", gosx.Attrs(gosx.Attr("href", "/news/details")), gosx.Text("Details")), gosx.El("div", gosx.Attrs(gosx.Attr("data-gosx-region-url", "/fragment"))))
 }
 app.Page("/", page)
 app.Page("/news", page)
 app.Page("/news/details", page)
 log.Fatal(app.ListenAndServe(":"+os.Getenv("PORT")))
}
`)
	tidyModule(t, dir)
	binary := filepath.Join(dir, "server")
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", binary, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, out)
	}
	for _, prefix := range []string{"/.proxy/game", "/news"} {
		for _, strips := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/strips=%t", prefix, strips), func(t *testing.T) {
				mode := "0"
				if strips {
					mode = "1"
				}
				t.Setenv("TEST_EXPORT_STRIPS", mode)
				t.Setenv("TEST_EXPORT_PREFIX", prefix)
				mountDir := filepath.FromSlash(strings.TrimPrefix(prefix, "/"))
				output := filepath.Join(t.TempDir(), "static")
				staged := false
				manifest, err := prerenderStaticBundle(staticExportOptions{AppRoot: dir, OutputDir: output, BinaryPath: binary, StageAssets: func(target string, manifest exportManifest) error {
					if target != filepath.Join(output, mountDir) {
						t.Fatalf("runtime assets staged outside prefix: %s", target)
					}
					staged = true
					return nil
				}})
				if err != nil {
					t.Fatal(err)
				}
				if manifest.BasePath != prefix || !staged || len(manifest.Routes) != 3 {
					t.Fatalf("manifest: %+v", manifest)
				}
				for _, internal := range []string{"/", "/news", "/news/details"} {
					public := prefix + internal
					var entry exportRoute
					for _, candidate := range manifest.Routes {
						if candidate.Path == public {
							entry = candidate
						}
					}
					wantFile := strings.TrimPrefix(strings.TrimRight(public, "/"), "/") + "/index.html"
					if entry.Path != public || entry.File != wantFile {
						t.Fatalf("internal %s: got %+v, want public %s and file %s", internal, entry, public, wantFile)
					}
					body := readFile(t, filepath.Join(output, filepath.FromSlash(entry.File)))
					depth := 0
					if internal != "/" {
						depth = len(strings.Split(strings.Trim(internal, "/"), "/"))
					}
					css := strings.Repeat("../", depth) + "_gosx/css/page.css"
					for _, want := range []string{`content="` + prefix + `"`, `href="` + css + `"`, `data-gosx-region-url="` + prefix + `/fragment"`, `data-internal-route="` + internal + `"`} {
						if !strings.Contains(body, want) {
							t.Fatalf("missing %s in %s", want, body)
						}
					}
					base, err := url.Parse("https://export.example" + strings.TrimRight(public, "/") + "/")
					if err != nil {
						t.Fatal(err)
					}
					for label, target := range map[string]string{"News": "/news?q=1#top", "Details": "/news/details"} {
						match := regexp.MustCompile(`<a[^>]*href="([^"]*)"[^>]*>` + label + `</a>`).FindStringSubmatch(body)
						if len(match) != 2 {
							t.Fatalf("missing %s link in %s", label, body)
						}
						ref, err := url.Parse(match[1])
						if err != nil {
							t.Fatal(err)
						}
						got := base.ResolveReference(ref)
						want, err := url.Parse("https://export.example" + prefix + target)
						if err != nil {
							t.Fatal(err)
						}
						if strings.TrimSuffix(got.Path, "/") != want.Path || got.Host != want.Host || got.Scheme != want.Scheme || got.RawQuery != want.RawQuery || got.Fragment != want.Fragment {
							t.Fatalf("%s link from %s resolves to %s, want %s", label, public, got, want)
						}
					}
				}
				if css := readFile(t, filepath.Join(output, mountDir, "_gosx", "css", "page.css")); css != "p{color:blue}" {
					t.Fatal(css)
				}
				readFile(t, filepath.Join(output, mountDir, "style.css"))
				navigationPath := filepath.FromSlash(strings.TrimPrefix(runtimehost.NavigationRuntimePath, "/"))
				if got := readFile(t, filepath.Join(output, mountDir, navigationPath)); got != runtimehost.NavigationRuntime {
					t.Fatal("exported navigation runtime differs from the served asset")
				}
			})
		}
	}
}
