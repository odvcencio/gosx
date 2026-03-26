package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildServerBinaryIfPresentBuildsMainPackage(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "go.mod", "module example.com/app\ngo 1.22\n")
	writeTempFile(t, dir, "main.go", `package main

func main() {}
`)

	out := filepath.Join(dir, "dist", "server", "app")
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		t.Fatal(err)
	}

	built, err := buildServerBinaryIfPresent(dir, out)
	if err != nil {
		t.Fatal(err)
	}
	if !built {
		t.Fatal("expected main package to build")
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected binary at %s: %v", out, err)
	}
}

func TestBuildServerBinaryIfPresentSkipsLibraryPackage(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "go.mod", "module example.com/lib\ngo 1.22\n")
	writeTempFile(t, dir, "lib.go", `package lib

func Value() int { return 1 }
`)

	out := filepath.Join(dir, "dist", "server", "app")
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		t.Fatal(err)
	}

	built, err := buildServerBinaryIfPresent(dir, out)
	if err != nil {
		t.Fatal(err)
	}
	if built {
		t.Fatal("expected library package to be skipped")
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("unexpected server binary for library package")
	}
}

func TestCompileDevIslandsWritesJSONPrograms(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "build", "islands")
	writeTempFile(t, dir, "counter.gsx", `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	return <div><span>{count.Get()}</span><button onClick={increment}>+</button></div>
}
`)

	if err := compileDevIslands(dir, out); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(out, "Counter.json"))
	if err != nil {
		t.Fatalf("expected Counter.json: %v", err)
	}
	if !strings.Contains(string(data), `"name": "Counter"`) {
		t.Fatalf("unexpected island JSON: %s", data)
	}
}

func TestStageSidecarCSSCopiesFiles(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "build", "css")
	writeTempFile(t, dir, "counter.css", ".counter { color: red; }\n")

	if err := stageSidecarCSS(dir, out); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(out, "counter.css"))
	if err != nil {
		t.Fatalf("expected copied css: %v", err)
	}
	if strings.TrimSpace(string(data)) != ".counter { color: red; }" {
		t.Fatalf("unexpected css contents: %q", string(data))
	}
}

func TestStageSidecarCSSPreservesRelativePaths(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "build", "css")
	writeTempFile(t, dir, "app/page.css", ".page { color: red; }\n")
	writeTempFile(t, dir, "app/docs/page.css", ".docs-page { color: blue; }\n")

	if err := stageSidecarCSS(dir, out); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{"app/page.css", "app/docs/page.css"} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Fatalf("expected preserved css path %s: %v", rel, err)
		}
	}
}

func writeTempFile(t *testing.T, dir, name, contents string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
