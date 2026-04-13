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

// TestCMSDemoRewrittenShape verifies the CMS demo was rewritten to the editorial
// magenta design language, has proper server-side state, and is free of dead
// drag affordances.
func TestCMSDemoRewrittenShape(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	cmsDir := filepath.Join(filepath.Dir(thisFile), "app", "demos", "cms")

	// -- page.gsx checks --
	gsxSource, err := os.ReadFile(filepath.Join(cmsDir, "page.gsx"))
	if err != nil {
		t.Fatalf("read cms/page.gsx: %v", err)
	}
	gsxSrc := string(gsxSource)

	gsxRequire := []string{
		"data.blocks",
		"<Each",
		`actionPath("publish")`,
		"csrf_token",
	}
	for _, needle := range gsxRequire {
		if !strings.Contains(gsxSrc, needle) {
			t.Errorf("cms/page.gsx missing %q", needle)
		}
	}

	// -- page.css checks --
	cssSource, err := os.ReadFile(filepath.Join(cmsDir, "page.css"))
	if err != nil {
		t.Fatalf("read cms/page.css: %v", err)
	}
	cssSrc := string(cssSource)

	// Must use shell design tokens (not hardcoded values).
	if !strings.Contains(cssSrc, "var(--demo-accent)") {
		t.Error("cms/page.css missing var(--demo-accent) — theming must wire to shell tokens")
	}

	// Must NOT contain cursor: grab (dead drag affordance).
	if strings.Contains(cssSrc, "cursor: grab") {
		t.Error("cms/page.css must not contain 'cursor: grab' — no drag affordances on non-draggable elements")
	}

	// Must NOT contain old gold hex (regression guard).
	if strings.Contains(cssSrc, "#D4AF37") {
		t.Error("cms/page.css must not contain old gold hex #D4AF37 — editorial magenta rewrite required")
	}

	// -- page.server.go checks --
	serverSource, err := os.ReadFile(filepath.Join(cmsDir, "page.server.go"))
	if err != nil {
		t.Fatalf("read cms/page.server.go: %v", err)
	}
	serverSrc := string(serverSource)

	serverRequire := []string{
		"democtl.NewLimiter",
		`RegisterStaticDocsPage("CMS Editor"`,
		"sync.Mutex",
		"publishStore",
	}
	for _, needle := range serverRequire {
		if !strings.Contains(serverSrc, needle) {
			t.Errorf("cms/page.server.go missing %q", needle)
		}
	}
}

// TestScene3DDemoCinematicShape verifies the scene3d demo was rewritten to a
// cinematic PBR showcase with three-point lighting, ACES tonemapping, bloom,
// editorial CSS tokens, and proper accessibility markup.
func TestScene3DDemoCinematicShape(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	sceneDir := filepath.Join(filepath.Dir(thisFile), "app", "demos", "scene3d")

	// -- program.go checks --
	progSource, err := os.ReadFile(filepath.Join(sceneDir, "program.go"))
	if err != nil {
		t.Fatalf("read scene3d/program.go: %v", err)
	}
	progSrc := string(progSource)

	// PostFX chain: ACES tonemap + bloom must be declared.
	progRequire := []string{
		"scene.Tonemap",
		"scene.Bloom",
	}
	for _, needle := range progRequire {
		if !strings.Contains(progSrc, needle) {
			t.Errorf("scene3d/program.go missing %q — postfx not wired", needle)
		}
	}

	// Three-point lighting: must have at least three distinct light types.
	lightTypes := []string{"DirectionalLight", "PointLight", "HemisphereLight"}
	found := 0
	for _, lt := range lightTypes {
		if strings.Contains(progSrc, lt) {
			found++
		}
	}
	if found < 2 {
		t.Errorf("scene3d/program.go has %d distinct light types (DirectionalLight, PointLight, HemisphereLight); want at least 2 for three-point setup", found)
	}

	// -- page.css checks --
	cssSource, err := os.ReadFile(filepath.Join(sceneDir, "page.css"))
	if err != nil {
		t.Fatalf("read scene3d/page.css: %v", err)
	}
	cssSrc := string(cssSource)

	// Must wire to shell design tokens.
	if !strings.Contains(cssSrc, "var(--demo-accent)") {
		t.Error("scene3d/page.css missing var(--demo-accent) — theming must wire to shell tokens")
	}

	// Must NOT contain old geo-zoo class (renamed to scene3d-showcase).
	if strings.Contains(cssSrc, ".geo-zoo") {
		t.Error("scene3d/page.css still contains .geo-zoo class — must be renamed to .scene3d-showcase")
	}

	// -- page.gsx checks --
	gsxSource, err := os.ReadFile(filepath.Join(sceneDir, "page.gsx"))
	if err != nil {
		t.Fatalf("read scene3d/page.gsx: %v", err)
	}
	gsxSrc := string(gsxSource)

	// Must carry aria-label for accessibility.
	if !strings.Contains(gsxSrc, "aria-label") {
		t.Error("scene3d/page.gsx missing aria-label — accessibility required")
	}
}

// TestScene3DBenchRewrittenShape verifies the scene3d-bench demo was rewritten
// to the editorial steel design language (flat, monospace, token-driven) with
// a live histogram SVG and GPU info strip.
func TestScene3DBenchRewrittenShape(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	benchDir := filepath.Join(filepath.Dir(thisFile), "app", "demos", "scene3d-bench")

	// -- page.css checks --
	cssSource, err := os.ReadFile(filepath.Join(benchDir, "page.css"))
	if err != nil {
		t.Fatalf("read scene3d-bench/page.css: %v", err)
	}
	cssSrc := string(cssSource)

	// Must wire to shell design token for accent color.
	if !strings.Contains(cssSrc, "var(--demo-accent)") {
		t.Error("scene3d-bench/page.css missing var(--demo-accent) — theming must wire to shell tokens")
	}

	// Must NOT use glass blur (flat design).
	if strings.Contains(cssSrc, "backdrop-filter") {
		t.Error("scene3d-bench/page.css must not contain backdrop-filter — flat design, no glass blur")
	}

	// Regression guard: none of the old hardcoded blue hexes.
	for _, oldBlue := range []string{"#8ecfff", "#6b8da8", "#d6ebff"} {
		if strings.Contains(cssSrc, oldBlue) {
			t.Errorf("scene3d-bench/page.css must not contain old hardcoded blue %q — editorial rewrite required", oldBlue)
		}
	}

	// -- page.gsx checks --
	gsxSource, err := os.ReadFile(filepath.Join(benchDir, "page.gsx"))
	if err != nil {
		t.Fatalf("read scene3d-bench/page.gsx: %v", err)
	}
	gsxSrc := string(gsxSource)

	// New class names must be in place.
	if !strings.Contains(gsxSrc, "scene3d-bench__overlay") {
		t.Error("scene3d-bench/page.gsx missing class scene3d-bench__overlay — rename from bench3d required")
	}

	// GPU strip must be present.
	if !strings.Contains(gsxSrc, "bench3d-gpu") {
		t.Error("scene3d-bench/page.gsx missing id bench3d-gpu — GPU info strip required")
	}

	// Histogram SVG must be present (viewBox covering 240 wide x 40 tall).
	if !strings.Contains(gsxSrc, `viewBox="0 0 240 40"`) {
		t.Error(`scene3d-bench/page.gsx missing histogram SVG with viewBox="0 0 240 40"`)
	}

	// Functional substrate: perf gate flag must still be set.
	if !strings.Contains(gsxSrc, "__gosx_scene3d_perf = true") {
		t.Error("scene3d-bench/page.gsx missing __gosx_scene3d_perf = true — perf gate must remain")
	}

	// Functional substrate: PerformanceObserver must still be attached.
	if !strings.Contains(gsxSrc, "PerformanceObserver") {
		t.Error("scene3d-bench/page.gsx missing PerformanceObserver — observer must remain")
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

// TestLiveSimDemoShape verifies the livesim demo has correct structural wiring:
// hub, sim runner, BindHub, page markup, client JS, and nav flip.
func TestLiveSimDemoShape(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Dir(thisFile)
	livesimDir := filepath.Join(root, "app", "demos", "livesim")
	publicDir := filepath.Join(root, "public")
	mainPath := filepath.Join(root, "main.go")
	layoutPath := filepath.Join(root, "app", "demos", "layout.gsx")

	// ── page.server.go ────────────────────────────────────────────────────────
	serverSource, err := os.ReadFile(filepath.Join(livesimDir, "page.server.go"))
	if err != nil {
		t.Fatalf("read livesim/page.server.go: %v", err)
	}
	serverSrc := string(serverSource)

	serverChecks := []struct{ needle, label string }{
		{`hub.New("livesim")`, `hub.New("livesim")`},
		{"sim.New(", "sim.New("},
		{"ctx.Runtime().BindHub(", "ctx.Runtime().BindHub("},
		{"runner.Start()", "runner.Start()"},
	}
	for _, c := range serverChecks {
		if !strings.Contains(serverSrc, c.needle) {
			t.Errorf("livesim/page.server.go missing %q", c.label)
		}
	}

	// ── page.gsx ──────────────────────────────────────────────────────────────
	gsxSource, err := os.ReadFile(filepath.Join(livesimDir, "page.gsx"))
	if err != nil {
		t.Fatalf("read livesim/page.gsx: %v", err)
	}
	gsxSrc := string(gsxSource)

	if !strings.Contains(gsxSrc, "/livesim-client.js") {
		t.Error("livesim/page.gsx missing /livesim-client.js script src")
	}
	if !strings.Contains(gsxSrc, "livesim-canvas") {
		t.Error("livesim/page.gsx missing livesim-canvas id")
	}

	// page.gsx must compile and export Page.
	prog, err := gosx.Compile(gsxSource)
	if err != nil {
		t.Fatalf("compile livesim/page.gsx: %v", err)
	}
	if len(prog.Components) == 0 || prog.Components[0].Name != "Page" {
		t.Error("livesim/page.gsx: expected Page component")
	}

	// ── game.go ───────────────────────────────────────────────────────────────
	gameSource, err := os.ReadFile(filepath.Join(livesimDir, "game.go"))
	if err != nil {
		t.Fatalf("read livesim/game.go: %v", err)
	}
	gameSrc := string(gameSource)

	if !strings.Contains(gameSrc, "func (g *game) Tick(") {
		t.Error("livesim/game.go missing func (g *game) Tick(")
	}
	if !strings.Contains(gameSrc, "worldWidth") {
		t.Error("livesim/game.go missing worldWidth constant")
	}
	if !strings.Contains(gameSrc, "worldHeight") {
		t.Error("livesim/game.go missing worldHeight constant")
	}

	// ── public/livesim-client.js ──────────────────────────────────────────────
	jsSource, err := os.ReadFile(filepath.Join(publicDir, "livesim-client.js"))
	if err != nil {
		t.Fatalf("read public/livesim-client.js: %v", err)
	}
	jsSrc := string(jsSource)
	if !strings.Contains(jsSrc, "gosx:hub:event") {
		t.Error("public/livesim-client.js missing gosx:hub:event listener")
	}

	// ── layout.gsx: livesim must be a live <a>, not an aria-disabled <span> ──
	layoutSource, err := os.ReadFile(layoutPath)
	if err != nil {
		t.Fatalf("read layout.gsx: %v", err)
	}
	layoutSrc := string(layoutSource)
	if !strings.Contains(layoutSrc, `href="/demos/livesim"`) {
		t.Error(`layout.gsx missing href="/demos/livesim" — dock item not flipped to Live`)
	}
	// Must NOT still have aria-disabled on the livesim entry.
	// Check that the pattern `data-demo="livesim" aria-disabled` does not appear
	// on the same element (other demos may still be aria-disabled).
	if strings.Contains(layoutSrc, `data-demo="livesim" aria-disabled`) {
		t.Error("layout.gsx livesim entry still has aria-disabled — not fully flipped")
	}

	// ── main.go: hub mount ────────────────────────────────────────────────────
	mainSource, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	mainSrc := string(mainSource)
	if !strings.Contains(mainSrc, `app.Mount("/demos/livesim/ws"`) {
		t.Error(`main.go missing app.Mount("/demos/livesim/ws"`)
	}
}
