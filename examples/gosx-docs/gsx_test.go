package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
)

func TestDocsGSXFilesCompile(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "app")

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".gsx" {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		prog, err := gosx.Compile(source)
		if err != nil {
			t.Fatalf("compile %s: %v", path, err)
		}
		// Any file a route loader will bind (layout.gsx / page.gsx) must
		// produce at least one component. A bare fragment compiles but
		// silently 500s at prerender with "no components found".
		base := filepath.Base(path)
		if base == "layout.gsx" || base == "page.gsx" {
			if len(prog.Components) == 0 {
				rel, _ := filepath.Rel(root, path)
				t.Fatalf("%s has no components (bare-fragment form breaks route resolution)", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestDemosLayoutStructure verifies the editorial demos shell has the expected
// structural shape. We use compile + raw-source grep rather than rendered HTML
// because the IR does not expose an HTML renderer in tests.
func TestDemosLayoutStructure(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	layoutPath := filepath.Join(filepath.Dir(thisFile), "app", "demos", "layout.gsx")

	source, err := os.ReadFile(layoutPath)
	if err != nil {
		t.Fatalf("read layout.gsx: %v", err)
	}

	// 1. Must compile without errors.
	prog, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("compile demos/layout.gsx: %v", err)
	}

	// 2. Must produce at least one component (bare-fragment form breaks routes).
	if len(prog.Components) == 0 {
		t.Fatal("demos/layout.gsx has no components")
	}

	src := string(source)

	// 3. Required structural class names.
	structuralClasses := []string{
		"demos-shell",
		"demo-dock",
		"demo-viewport",
		"demo-meta",
	}
	for _, cls := range structuralClasses {
		if !strings.Contains(src, cls) {
			t.Errorf("demos/layout.gsx missing class %q", cls)
		}
	}

	// 4. Dock nav must carry aria-label="Demos".
	if !strings.Contains(src, `aria-label="Demos"`) {
		t.Error(`demos/layout.gsx missing aria-label="Demos" on dock nav`)
	}

	// 5. All seven demo slugs must appear in the dock.
	slugs := []string{"playground", "fluid", "livesim", "cms", "scene3d", "scene3d-bench", "collab"}
	for _, slug := range slugs {
		if !strings.Contains(src, slug) {
			t.Errorf("demos/layout.gsx missing demo slug %q", slug)
		}
	}

	// 6. Meta footer must have the three data-drawer pill buttons.
	drawerAttrs := []string{
		`data-drawer="source"`,
		`data-drawer="packages"`,
		`data-drawer="prerender"`,
	}
	for _, attr := range drawerAttrs {
		if !strings.Contains(src, attr) {
			t.Errorf("demos/layout.gsx missing meta button with %s", attr)
		}
	}

	// 7. Slot must be present (the viewport renders child pages).
	if !strings.Contains(src, "<Slot") {
		t.Error("demos/layout.gsx missing <Slot /> for page content")
	}
}
