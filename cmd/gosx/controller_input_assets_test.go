package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/controller"
	"m31labs.dev/gosx/server"
)

func TestControllerInputAssetsAcrossBuildModes(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("compiler subprocesses are covered by the CLI partition")
	}
	want, err := os.ReadFile(filepath.Join(testRepoRoot(t), "client", "js", "bootstrap-controller-input.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"dev", "export", "production"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/controller-app\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.57.5\n")
			addLocalGoSXReplace(t, dir)
			mustWriteFile(t, filepath.Join(dir, "app", "page.gsx"), "package app\n\ncomponent Page() {\n    return <main>Controls</main>\n}\n")
			mustWriteFile(t, filepath.Join(dir, "app", "route.config.json"), `{"prerender":true}`)
			mustWriteFile(t, filepath.Join(dir, "main.go"), `package main
import (
    "log"
    "m31labs.dev/gosx"
    "m31labs.dev/gosx/controller"
    "m31labs.dev/gosx/server"
)
func main() {
    app := server.New()
    app.SetRuntimeRoot(".")
    app.Page("/", func(ctx *server.Context) gosx.Node {
        ctx.Controller(controller.Config{Name: "prefs", Storage: &controller.Storage{Namespace: "prefs"}})
        return gosx.RawHTML("<main>Controls</main>")
    })
    log.Fatal(app.ListenAndServe(":8080"))
}
`)
			tidyModule(t, dir)
			root := dir
			switch mode {
			case "dev":
				if err := prepareDevAssetsWithPrograms(dir, nil); err != nil {
					t.Fatal(err)
				}
			case "export":
				if err := RunExport(dir); err != nil {
					t.Fatal(err)
				}
				root = filepath.Join(dir, "dist", "static")
			case "production":
				if err := RunBuild(dir, false); err != nil {
					t.Fatal(err)
				}
				root = filepath.Join(dir, "dist")
			}
			app := server.New()
			app.SetRuntimeRoot(root)
			app.Page("/", func(ctx *server.Context) gosx.Node {
				ctx.Controller(controller.Config{Name: "prefs", Storage: &controller.Storage{Namespace: "prefs"}})
				return gosx.RawHTML("<main>Controls</main>")
			})
			var handler http.Handler = app.Build()
			if mode == "export" {
				handler = http.FileServer(http.Dir(root))
			}
			srv := httptest.NewServer(handler)
			defer srv.Close()
			get := func(path string) []byte {
				t.Helper()
				resp, err := srv.Client().Get(srv.URL + "/" + strings.TrimLeft(path, "/"))
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				if err != nil || resp.StatusCode != http.StatusOK {
					t.Fatalf("GET %s: status=%d err=%v", path, resp.StatusCode, err)
				}
				return body
			}
			page := get("/")
			match := regexp.MustCompile(`"bootstrapControllerInputPath":("[^"]+")`).FindSubmatch(page)
			if len(match) != 2 {
				t.Fatal("controller page did not advertise its input chunk")
			}
			var inputPath string
			if err := json.Unmarshal(match[1], &inputPath); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{inputPath, "/gosx/bootstrap-controller-input.js"} {
				if got := get(path); !bytes.Equal(got, want) {
					t.Fatalf("GET %s did not serve the built input chunk", path)
				}
			}
		})
	}
}
