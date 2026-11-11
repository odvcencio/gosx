package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPackageMapIsFresh fails when docs/packages.md no longer matches the
// package tree, or when a package has no row in layers.txt. Run
// `go run ./scripts/pkgmap` to refresh it.
func TestPackageMapIsFresh(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list over the whole module")
	}
	root := filepath.Join("..", "..")
	got, err := Render(root)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(root, "docs", "packages.md"))
	if err != nil {
		t.Fatal(err)
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
