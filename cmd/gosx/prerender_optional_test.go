package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectPrerenderConfig(t *testing.T) {
	for _, tc := range []struct {
		config string
		want   bool
	}{
		{`{}`, true},
		{`{"build":{"prerender":{}}}`, true},
		{`{"build":{"prerender":{"enabled":true}}}`, true},
		{`{"build":{"prerender":{"enabled":false}}}`, false},
	} {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "gosx.config.json"), tc.config)
		cfg, err := loadProjectConfig(dir)
		if err != nil || cfg.Build.Prerender.enabled() != tc.want {
			t.Fatalf("config %s: enabled=%v, err=%v", tc.config, cfg.Build.Prerender.enabled(), err)
		}
	}
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "gosx.config.json"), `{"build":{"prerender":{"enabled":"false"}}}`)
	if _, err := loadProjectConfig(dir); err == nil {
		t.Fatal("non-boolean enabled must be rejected")
	}
}

func TestPrerenderNoStaticRoutesSkipsServer(t *testing.T) {
	for _, page := range []string{"", "dynamic", "parameter"} {
		t.Run(page, func(t *testing.T) {
			dir := t.TempDir()
			mustWriteFile(t, filepath.Join(dir, "app", "route.config.json"), `{"prerender":false}`)
			if page == "dynamic" {
				mustWriteFile(t, filepath.Join(dir, "app", "page.gsx"), "package app\ncomponent Page() { return <p>dynamic</p> }\n")
			}
			if page == "parameter" {
				mustWriteFile(t, filepath.Join(dir, "app", "[id]", "page.gsx"), "package app\ncomponent Page() { return <p>parameter</p> }\n")
				mustWriteFile(t, filepath.Join(dir, "app", "[id]", "route.config.json"), `{"prerender":true}`)
			}
			mustWriteFile(t, filepath.Join(dir, "public", "style.css"), "body { color: black; }\n")
			output := filepath.Join(dir, "static")
			mustWriteFile(t, filepath.Join(output, "index.html"), "stale snapshot")
			staged := false
			manifest, err := prerenderStaticBundle(staticExportOptions{
				AppRoot: dir, OutputDir: output,
				BinaryPath: filepath.Join(dir, "missing-server"),
				StageAssets: func(outputDir string, manifest exportManifest) error {
					staged = true
					if outputDir != output || len(manifest.Pages) != 0 {
						t.Fatal("unexpected empty-export staging")
					}
					return nil
				},
			})
			if err != nil || manifest.Pages == nil || len(manifest.Pages) != 0 || !staged {
				t.Fatalf("empty export=%+v, staged=%v, err=%v", manifest, staged, err)
			}
			if _, err := os.Stat(filepath.Join(output, "index.html")); !os.IsNotExist(err) {
				t.Fatal("stale snapshot survived")
			}
			if _, err := os.Stat(filepath.Join(output, "style.css")); err != nil {
				t.Fatal("public assets were not staged")
			}
		})
	}
}

func TestRunBuildProdPrerenderDisabledKeepsServerAndAssets(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("production compiler subprocess is covered by the CLI partition")
	}
	dir := filepath.Join(t.TempDir(), "authenticated-app")
	if err := RunInit(dir, "example.com/authenticated-app", ""); err != nil {
		t.Fatal(err)
	}
	addLocalGoSXReplace(t, dir)
	mustWriteFile(t, filepath.Join(dir, "app", "route.config.json"), `{"prerender":true}`)
	mustWriteFile(t, filepath.Join(dir, "gosx.config.json"), `{"build":{"prerender":{"enabled":false}}}`)
	marker := filepath.Join(dir, "started")
	t.Setenv("TEST_BUILD_STARTED", marker)
	mustWriteFile(t, filepath.Join(dir, "main.go"), `package main
import ("log"; "os")
func main() {
    _ = os.WriteFile(os.Getenv("TEST_BUILD_STARTED"), []byte("started"), 0600)
    log.Fatal("runtime authentication configuration required")
}
`)
	tidyModule(t, dir)
	if err := RunBuild(dir, false); err != nil {
		t.Fatal(err)
	}
	if report, err := checkDeploymentBundle(filepath.Join(dir, "dist")); err != nil || !report.OK {
		t.Fatalf("production bundle failed deployment checks: %+v, %v", report, err)
	}
	for _, rel := range []string{"dist/build.json", "dist/server/app", "dist/run.sh", "dist/assets/runtime"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("missing production artifact %s: %v", rel, err)
		}
	}
	for _, rel := range []string{"started", "dist/export.json", "dist/static", "dist/edge"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
			t.Fatalf("unexpected startup or prerender artifact %s: %v", rel, err)
		}
	}
}
