package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrerenderBasePathWithEitherProxyMode(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("build subprocess covered by test-cli")
	}
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/prefixed-export\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\n")
	addLocalGoSXReplace(t, dir)
	for _, rel := range []string{"app/page.gsx", "app/news/page.gsx"} {
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
 if err := app.SetBasePath("/.proxy/game", server.BasePathOptions{ProxyStripsPrefix: os.Getenv("TEST_EXPORT_STRIPS") == "1"}); err != nil { log.Fatal(err) }
 app.SetPublicDir("public")
 app.Mount("/_gosx/css/page.css", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/css"); w.Write([]byte("p{color:blue}")) }))
 page := func(ctx *server.Context) gosx.Node {
  ctx.AddHead(gosx.El("link", gosx.Attrs(gosx.Attr("rel", "stylesheet"), gosx.Attr("href", "/_gosx/css/page.css"), gosx.Attr("data-gosx-file-css", "page"))))
  return gosx.Fragment(gosx.El("a", gosx.Attrs(gosx.Attr("href", "/news")), gosx.Text("News")), gosx.El("div", gosx.Attrs(gosx.Attr("data-gosx-region-url", "/fragment"))))
 }
 app.Page("/", page)
 app.Page("/news", page)
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
	for _, strips := range []bool{false, true} {
		t.Run(fmt.Sprint(strips), func(t *testing.T) {
			mode := "0"
			if strips {
				mode = "1"
			}
			t.Setenv("TEST_EXPORT_STRIPS", mode)
			output := filepath.Join(t.TempDir(), "static")
			staged := false
			manifest, err := prerenderStaticBundle(staticExportOptions{AppRoot: dir, OutputDir: output, BinaryPath: binary, StageAssets: func(target string, manifest exportManifest) error {
				if target != filepath.Join(output, ".proxy", "game") {
					t.Fatalf("runtime assets staged outside prefix: %s", target)
				}
				staged = true
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			if manifest.BasePath != "/.proxy/game" || !staged || len(manifest.Routes) != 2 {
				t.Fatalf("manifest: %+v", manifest)
			}
			for _, entry := range manifest.Routes {
				if !strings.HasPrefix(entry.Path, "/.proxy/game/") || !strings.HasPrefix(entry.File, ".proxy/game/") {
					t.Fatal(entry)
				}
				body := readFile(t, filepath.Join(output, filepath.FromSlash(entry.File)))
				css := "_gosx/css/page.css"
				if strings.HasSuffix(entry.Path, "/news") {
					css = "../" + css
				}
				for _, want := range []string{`content="/.proxy/game"`, `href="` + css + `"`, `data-gosx-region-url="/.proxy/game/fragment"`} {
					if !strings.Contains(body, want) {
						t.Fatalf("missing %s in %s", want, body)
					}
				}
				if strings.Contains(body, "/.proxy/game/.proxy/game") {
					t.Fatal("double prefix", body)
				}
			}
			if css := readFile(t, filepath.Join(output, ".proxy", "game", "_gosx", "css", "page.css")); css != "p{color:blue}" {
				t.Fatal(css)
			}
			readFile(t, filepath.Join(output, ".proxy", "game", "style.css"))
		})
	}
}
