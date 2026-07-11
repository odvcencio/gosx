package docs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDemoCatalogContracts(t *testing.T) {
	if len(Demos()) != 9 {
		t.Fatalf("Demos() length = %d, want 9", len(Demos()))
	}
	seen := make(map[string]bool, len(Demos()))
	validStatus := map[string]bool{"featured": true, "live": true, "lab": true, "prototype": true}
	for _, demo := range Demos() {
		if demo.Slug == "" || seen[demo.Slug] {
			t.Fatalf("empty or duplicate demo slug %q", demo.Slug)
		}
		seen[demo.Slug] = true
		if demo.Title == "" || demo.Promise == "" || demo.Lesson == "" {
			t.Errorf("demo %q lacks a title, promise, or lesson", demo.Slug)
		}
		if !validStatus[demo.Status] {
			t.Errorf("demo %q has invalid status %q", demo.Slug, demo.Status)
		}
		if len(demo.Facets) == 0 || len(demo.Packages) == 0 || demo.SourcePath == "" {
			t.Errorf("demo %q lacks proof metadata", demo.Slug)
		}
		if demo.RenderMode == "" || demo.Limitations == "" {
			t.Errorf("demo %q lacks honest runtime metadata", demo.Slug)
		}
		if _, err := os.Stat(repoPath(t, demo.SourcePath)); err != nil {
			t.Errorf("demo %q source path %q: %v", demo.Slug, demo.SourcePath, err)
		}
	}
	if cms, ok := FindDemo("cms"); !ok || cms.Status != "prototype" {
		t.Error("CMS must remain explicitly labeled prototype until editing is real")
	}
	checkers, ok := FindDemo("checkers")
	if !ok || checkers.Status != "live" {
		t.Fatal("checkers must be truthfully listed as a live two-seat match")
	}
	for _, required := range []string{"two-player", "no product network multiplayer", "persistence", "active CPU", "compiled Arbiter policy fallback", "Elio"} {
		if !strings.Contains(checkers.Limitations, required) {
			t.Errorf("checkers limitations missing %q: %s", required, checkers.Limitations)
		}
	}
	for _, required := range []string{"Selena", "Arbiter policy", "Elio adapter"} {
		if !contains(checkers.Facets, required) {
			t.Errorf("checkers facets missing %q", required)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestDemoDetailsClientKeyboardContract(t *testing.T) {
	client, err := os.ReadFile(repoPath(t, "examples/gosx-docs/public/demos-dock.js"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(client)
	for _, required := range []string{
		`event.key === "Escape"`,
		`event.key !== "Tab"`,
		`event.shiftKey`,
		`previousFocus.focus()`,
		`aria-expanded`,
		`data-demo-details-source`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("demos-dock.js missing keyboard/details behavior %q", required)
		}
	}
}

func repoPath(t *testing.T, relative string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	return filepath.Join(root, filepath.FromSlash(relative))
}
