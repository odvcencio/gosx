package main

import (
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestExportStagesExternalFileCSS(t *testing.T) {
	for _, mode := range []string{"export", "build"} {
		t.Run(mode, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "css-app")
			if err := RunInit(dir, "example.com/css-app", ""); err != nil {
				t.Fatal(err)
			}
			addLocalGoSXReplace(t, dir)
			// This fixture tests exported CSS, so opt its loader page into snapshots.
			mustWriteFile(t, filepath.Join(dir, "app", "route.config.json"), `{"prerender":true}`)
			mainPath := filepath.Join(dir, "main.go")
			main := readFile(t, mainPath)
			main = strings.Replace(main, "route.FileRoutesOptions{}", `route.FileRoutesOptions{ExternalCSS: "/_gosx/docs-css/"}`, 1)
			mustWriteFile(t, mainPath, main)
			mustWriteFile(t, filepath.Join(dir, "app", "page.css"), `.home { color: seagreen; }`)
			mustWriteFile(t, filepath.Join(dir, "app", "stack", "page.css"), `.stack { color: coral; }`)
			mustWriteFile(t, filepath.Join(dir, "app", "not-found.css"), `.missing { color: crimson; }`)
			tidyModule(t, dir)
			var err error
			if mode == "export" {
				err = RunExport(dir)
			} else {
				err = RunBuild(dir, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, page := range []struct{ path, color string }{
				{"index.html", "seagreen"}, {"stack/index.html", "coral"}, {"404.html", "crimson"},
			} {
				exported := readFile(t, filepath.Join(dir, "dist", "static", page.path))
				links := regexp.MustCompile(`<link[^>]+href="([^"]+)"[^>]+data-gosx-file-css=`).FindAllStringSubmatch(exported, -1)
				found := false
				for _, link := range links {
					ref, err := url.Parse(html.UnescapeString(link[1]))
					if err != nil {
						t.Fatal(err)
					}
					if strings.HasPrefix(ref.Path, "/") {
						t.Fatalf("exported stylesheet is not relative: %s", ref)
					}
					stylesheet := filepath.Join(dir, "dist", "static", filepath.Dir(page.path), filepath.FromSlash(ref.Path))
					data, err := os.ReadFile(stylesheet)
					if err != nil {
						t.Fatalf("%s references missing CSS %s: %v", page.path, ref, err)
					}
					if strings.Contains(string(data), page.color) {
						found = true
						if !strings.Contains(string(data), "data-gosx-s=") {
							t.Fatalf("%s CSS lost its authored scope", page.path)
						}
					}
				}
				if !found {
					t.Fatalf("%s has no staged %s CSS; %d stylesheet links", page.path, page.color, len(links))
				}
			}
		})
	}
}
