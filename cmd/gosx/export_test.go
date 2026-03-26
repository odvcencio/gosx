package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunExportWritesStaticBundleForStarterApp(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "export-app")
	if err := RunInit(dir, "example.com/export-app", ""); err != nil {
		t.Fatal(err)
	}
	addLocalGoSXReplace(t, dir)
	tidyModule(t, dir)

	if err := RunExport(dir); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"dist/static/index.html",
		"dist/static/stack/index.html",
		"dist/static/404.html",
		"dist/static/styles.css",
		"dist/static/gosx/runtime.wasm",
		"dist/static/gosx/wasm_exec.js",
		"dist/static/gosx/bootstrap.js",
		"dist/static/gosx/patch.js",
		"dist/export.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("expected export artifact %s: %v", rel, err)
		}
	}

	indexHTML := readFile(t, filepath.Join(dir, "dist", "static", "index.html"))
	for _, snippet := range []string{
		"<title>My GoSX App</title>",
		"GoSX Starter",
		`href="styles.css"`,
	} {
		if !strings.Contains(indexHTML, snippet) {
			t.Fatalf("expected %q in exported index.html", snippet)
		}
	}
	stackHTML := readFile(t, filepath.Join(dir, "dist", "static", "stack", "index.html"))
	if !strings.Contains(stackHTML, `href="../styles.css"`) {
		t.Fatalf("expected relative stylesheet url in nested export page, got %q", stackHTML)
	}

	notFoundHTML := readFile(t, filepath.Join(dir, "dist", "static", "404.html"))
	if !strings.Contains(notFoundHTML, "Page not found") {
		t.Fatalf("expected exported 404 page, got %q", notFoundHTML)
	}

	var manifest exportManifest
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, "dist", "export.json"))), &manifest); err != nil {
		t.Fatalf("decode export manifest: %v", err)
	}
	if len(manifest.Pages) != 2 || manifest.Pages[0] != "/" || manifest.Pages[1] != "/stack" {
		t.Fatalf("unexpected export pages: %#v", manifest.Pages)
	}
}

func TestRewriteStaticExportHTMLRewritesRootAssetsAndImageOptimizerURLs(t *testing.T) {
	input := `<!DOCTYPE html><html><head><link rel="stylesheet" href="/styles.css"><meta property="og:image" content="/_gosx/image?src=%2Fcover.png&w=640"></head><body><a href="/docs/getting-started">Docs</a><img src="/_gosx/image?src=%2Fpaper-card.png&w=960"><img srcset="/_gosx/image?src=%2Fpaper-card.png&w=320 320w, /_gosx/image?src=%2Fpaper-card.png&w=640 640w"></body></html>`
	output, err := rewriteStaticExportHTML("/docs/routing", input)
	if err != nil {
		t.Fatal(err)
	}
	for _, snippet := range []string{
		`href="../../styles.css"`,
		`content="../../cover.png"`,
		`href="../getting-started"`,
		`src="../../paper-card.png"`,
		`srcset="../../paper-card.png 320w, ../../paper-card.png 640w"`,
	} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("expected %q in rewritten html %q", snippet, output)
		}
	}
}

func TestStaticExportPagesSkipsPrerenderDisabledScopes(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(filepath.Join(appDir, "admin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "page.gsx"), []byte(`package main

func Page() Node { return <main>Home</main> }
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "admin", "page.gsx"), []byte(`package main

func Page() Node { return <main>Admin</main> }
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "admin", "route.config.json"), []byte(`{"prerender": false}`), 0644); err != nil {
		t.Fatal(err)
	}

	pages, err := staticExportPages(appDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0] != "/" {
		t.Fatalf("unexpected export pages: %#v", pages)
	}
}

func addLocalGoSXReplace(t *testing.T, dir string) {
	t.Helper()
	goModPath := filepath.Join(dir, "go.mod")
	goMod := readFile(t, goModPath)
	repoRoot := testRepoRoot(t)
	replaceLine := "\nreplace github.com/odvcencio/gosx => " + repoRoot + "\n"
	if strings.Contains(goMod, replaceLine) {
		return
	}
	if err := os.WriteFile(goModPath, []byte(goMod+replaceLine), 0644); err != nil {
		t.Fatalf("write %s: %v", goModPath, err)
	}
}

func testRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve repo root caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func tidyModule(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go mod tidy: %v", err)
	}
}
