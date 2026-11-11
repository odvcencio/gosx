package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestPackageMapIsFresh fails when docs/packages.md no longer matches the
// package tree, or when a package has no row in layers.txt. Run
// `go run ./scripts/pkgmap` to refresh it.
func TestPackageMapIsFresh(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list over the whole module")
	}
	root := filepath.Join("..", "..")
	touchTree(t, root)
	got, err := Render(root)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(root, "docs", "packages.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(got) {
		t.Fatal("generated package map is not valid UTF-8")
	}
	if string(want) != string(got) {
		t.Fatal("docs/packages.md is stale; run `go run ./scripts/pkgmap`")
	}
}

func TestMatchPrefersLongestRule(t *testing.T) {
	rules := []rule{{prefix: "engine", tree: true, layer: "kernels", tier: "stable"}, {prefix: "engine/wasm", tree: true, layer: "browser", tier: "stable"}}
	if r, ok := match(rules, "engine/wasm/x"); !ok || r.layer != "browser" {
		t.Fatalf("got %+v %v", r, ok)
	}
	if r, ok := match(rules, "engine/host"); !ok || r.layer != "kernels" {
		t.Fatalf("got %+v %v", r, ok)
	}
	if _, ok := match(rules, "enginex"); ok {
		t.Fatal("a tree rule must not match a sibling with the same prefix")
	}
}

// touchTree reads every Go source and go.mod file under root. `go list` runs in
// a subprocess, so the test cache cannot see the files it reads; reading them
// here makes the cache entry depend on the tree, and a cached pass cannot hide a
// stale package map.
func touchTree(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", ".canopy":
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") || filepath.Base(path) == "go.mod" {
			_, err = os.ReadFile(path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSynopsisKeepsMultibyteCharactersWhole(t *testing.T) {
	for in, want := range map[string]string{
		"Package x — édits the tree.": "Édits the tree.",
		"Package x éclairs.":          "Éclairs.",
		"Package ir is a thing.":      "A thing.",
	} {
		got := synopsis(in)
		if !utf8.ValidString(got) {
			t.Errorf("synopsis(%q) = %q is not valid UTF-8", in, got)
		}
		if got != want {
			t.Errorf("synopsis(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestLayerRulesHoldExceptForKnownEdges pins the import edges that break the
// layer rules. A new violation fails the test, and so does an exception that no
// longer applies.
func TestLayerRulesHoldExceptForKnownEdges(t *testing.T) {
	root := filepath.Join("..", "..")
	touchTree(t, root)
	edges, err := Violations(root)
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := readExceptions(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[Edge]bool{}
	for _, e := range edges {
		seen[e] = true
		if !allowed[e] {
			t.Errorf("import %s breaks the layer rules in docs/packages.md; fix the import or move the package", e)
		}
	}
	for e := range allowed {
		if !seen[e] {
			t.Errorf("%s in layer_exceptions.txt no longer breaks the rules; delete the line", e)
		}
	}
}
