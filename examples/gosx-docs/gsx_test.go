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

// TestPlaygroundPageRenders verifies the playground page shell compiles and
// registers correctly.
func TestPlaygroundPageRenders(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	playDir := filepath.Join(filepath.Dir(thisFile), "app", "demos", "playground")

	// 1. page.gsx must compile and export a Page component.
	gsxSource, err := os.ReadFile(filepath.Join(playDir, "page.gsx"))
	if err != nil {
		t.Fatalf("read playground/page.gsx: %v", err)
	}
	prog, err := gosx.Compile(gsxSource)
	if err != nil {
		t.Fatalf("compile playground/page.gsx: %v", err)
	}
	if len(prog.Components) < 1 {
		t.Fatal("playground/page.gsx: expected at least one component")
	}
	if prog.Components[0].Name != "Page" {
		t.Errorf("playground/page.gsx: Components[0].Name = %q; want %q", prog.Components[0].Name, "Page")
	}

	// 2. page.server.go must contain the registration call and key symbols.
	serverSource, err := os.ReadFile(filepath.Join(playDir, "page.server.go"))
	if err != nil {
		t.Fatalf("read playground/page.server.go: %v", err)
	}
	serverSrc := string(serverSource)
	serverChecks := []string{
		"docsapp.RegisterStaticDocsPage",
		`"Playground"`,
		`"compile"`,
		"NewCompileAction",
		"playgroundCompiler",
	}
	for _, needle := range serverChecks {
		if !strings.Contains(serverSrc, needle) {
			t.Errorf("playground/page.server.go missing %q", needle)
		}
	}

	// 3. page.css must be scoped under .play and use the terminal green accent.
	cssSource, err := os.ReadFile(filepath.Join(playDir, "page.css"))
	if err != nil {
		t.Fatalf("read playground/page.css: %v", err)
	}
	cssSrc := string(cssSource)
	cssChecks := []string{".play", "#9fffa5"}
	for _, needle := range cssChecks {
		if !strings.Contains(cssSrc, needle) {
			t.Errorf("playground/page.css missing %q", needle)
		}
	}
}

// TestPlaygroundPageHasEditorPlumbing verifies that page.gsx carries the
// data attributes and script tag needed by editor.js, and that editor.js
// itself contains the key functions.
func TestPlaygroundPageHasEditorPlumbing(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	playDir := filepath.Join(filepath.Dir(thisFile), "app", "demos", "playground")
	publicDir := filepath.Join(filepath.Dir(thisFile), "public")

	// 1. Read page.gsx.
	gsxSource, err := os.ReadFile(filepath.Join(playDir, "page.gsx"))
	if err != nil {
		t.Fatalf("read playground/page.gsx: %v", err)
	}
	src := string(gsxSource)

	// 2. Root section must carry data-compile-url.
	if !strings.Contains(src, `data-compile-url={actionPath("compile")}`) {
		t.Error(`playground/page.gsx missing data-compile-url={actionPath("compile")} on root section`)
	}

	// 3. Root section must carry data-csrf-token.
	if !strings.Contains(src, `data-csrf-token={csrf.token}`) {
		t.Error(`playground/page.gsx missing data-csrf-token={csrf.token} on root section`)
	}

	// 4. Preset options must carry data-source.
	if !strings.Contains(src, `data-source={p.Source}`) {
		t.Error(`playground/page.gsx missing data-source={p.Source} on preset <option> elements`)
	}

	// 5. Script tag for editor.js must be present (either via src attr or inline).
	hasEditorScript := strings.Contains(src, "playground-editor.js") ||
		(strings.Contains(src, "<script") && strings.Contains(src, "waitForRuntime"))
	if !hasEditorScript {
		t.Error(`playground/page.gsx missing editor.js script (expected playground-editor.js src or inline waitForRuntime)`)
	}

	// 6. editor.js must exist and contain the key functions.
	editorPath := filepath.Join(publicDir, "playground-editor.js")
	editorSource, err := os.ReadFile(editorPath)
	if err != nil {
		t.Fatalf("read public/playground-editor.js: %v", err)
	}
	editorSrc := string(editorSource)

	editorChecks := []string{
		"compile",
		"waitForRuntime",
		"base64ToBytes",
		"window.__gosx_hydrate",
	}
	for _, needle := range editorChecks {
		if !strings.Contains(editorSrc, needle) {
			t.Errorf("public/playground-editor.js missing %q", needle)
		}
	}
}

// TestDemosIndexLists7Cards verifies the /demos index page files have the
// expected structure and roster. We use raw-source grep rather than rendering
// because the GSX IR does not expose an HTML renderer in tests.
func TestDemosIndexLists7Cards(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	demosDir := filepath.Join(filepath.Dir(thisFile), "app", "demos")
	pagePath := filepath.Join(demosDir, "page.gsx")
	serverPath := filepath.Join(demosDir, "page.server.go")

	// 1. page.gsx must compile.
	pageSource, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatalf("read app/demos/page.gsx: %v", err)
	}
	prog, err := gosx.Compile(pageSource)
	if err != nil {
		t.Fatalf("compile app/demos/page.gsx: %v", err)
	}
	if len(prog.Components) == 0 {
		t.Fatal("app/demos/page.gsx has no components (bare-fragment form breaks route resolution)")
	}

	// 2. page.server.go must contain all 7 demo slugs.
	serverSource, err := os.ReadFile(serverPath)
	if err != nil {
		t.Fatalf("read app/demos/page.server.go: %v", err)
	}
	serverSrc := string(serverSource)

	slugs := []string{"playground", "fluid", "livesim", "cms", "scene3d", "scene3d-bench", "collab"}
	for _, slug := range slugs {
		if !strings.Contains(serverSrc, slug) {
			t.Errorf("app/demos/page.server.go missing demo slug %q", slug)
		}
	}

	// 3. Must use RegisterStaticDocsPage.
	if !strings.Contains(serverSrc, "RegisterStaticDocsPage") {
		t.Error(`app/demos/page.server.go missing "RegisterStaticDocsPage"`)
	}

	// 4. Must carry the "Demos" title.
	if !strings.Contains(serverSrc, `"Demos"`) {
		t.Error(`app/demos/page.server.go missing "Demos" title string`)
	}
}
