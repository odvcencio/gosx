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
	// CMS earned "live" once block adding, live preview, and full-draft publish
	// became real; its limitations must keep documenting what is still missing.
	cms, ok := FindDemo("cms")
	if !ok || cms.Status != "live" {
		t.Error("CMS must be listed live now that block editing and live preview are real")
	}
	for _, required := range []string{"no persistence", "no reordering", "no block removal"} {
		if !strings.Contains(cms.Limitations, required) {
			t.Errorf("cms limitations missing %q: %s", required, cms.Limitations)
		}
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

func TestDemoShellUsesGoSXManagedInteractions(t *testing.T) {
	layout, err := os.ReadFile(repoPath(t, "examples/gosx-docs/app/demos/layout.gsx"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(layout)
	for _, required := range []string{
		`data-gosx-bind-source`,
		`data-gosx-toggle-target`,
		`data-gosx-disclosure-target`,
		`data-gosx-disclosure-close`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("demo layout missing GoSX-managed behavior %q", required)
		}
	}
	if strings.Contains(source, `<script`) {
		t.Error("demo layout must not ship bespoke script elements")
	}
}

func TestBespokeDemoScriptDebtDoesNotGrow(t *testing.T) {
	// These predate the no-escape-hatch invariant. Keep the list exact: a new
	// script fails immediately, and deleting one requires deleting its entry so
	// the debt can only move toward zero.
	legacy := map[string]bool{
		"checkers/page.gsx":      true,
		"cms/page.gsx":           true,
		"collab/page.gsx":        true,
		"fluid/page.gsx":         true,
		"livesim/page.gsx":       true,
		"playground/page.gsx":    true,
		"scene3d/page.gsx":       true,
		"scene3d-bench/page.gsx": true,
	}
	root := repoPath(t, "examples/gosx-docs/app/demos")
	found := map[string]bool{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".gsx" {
			return err
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !strings.Contains(string(source), `<script`) {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		relative = filepath.ToSlash(relative)
		found[relative] = true
		if !legacy[relative] {
			t.Errorf("new bespoke demo script in %s; add the behavior to GoSX instead", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for relative := range legacy {
		if !found[relative] {
			t.Errorf("legacy script debt %s was removed; delete its exception too", relative)
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
