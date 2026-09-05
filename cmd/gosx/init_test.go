package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestRunInitCreatesStarterProject(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "starter")

	if err := RunInit(dir, "example.com/starter", ""); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"go.mod",
		"main.go",
		".env",
		".gitignore",
		"app/layout.gsx",
		"app/page.server.go",
		"app/page.gsx",
		"app/stack/page.server.go",
		"app/stack/page.gsx",
		"app/not-found.gsx",
		"app/error.gsx",
		"modules/modules.go",
		"public/favicon.ico",
		"public/styles.css",
		"perf-budget.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("expected %s: %v", rel, err)
		}
	}

	goMod := readFile(t, filepath.Join(dir, "go.mod"))
	if !strings.Contains(goMod, "module example.com/starter") {
		t.Fatalf("unexpected go.mod: %s", goMod)
	}

	mainGo := readFile(t, filepath.Join(dir, "main.go"))
	for _, snippet := range []string{
		`_ "example.com/starter/modules"`,
		`_, thisFile, _, _ := runtime.Caller(0)`,
		`root := server.ResolveAppRoot(thisFile)`,
		`env.LoadDir(root, "")`,
		`sessions, err := session.New(getenv("SESSION_SECRET", "gosx-app-session-secret"), session.Options{`,
		`AllowInsecure: strings.HasPrefix(publicBase, "http://"),`,
		`router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{})`,
		`app.EnableNavigation()`,
		`app.Use(sessions.Middleware)`,
		`app.Use(sessions.Protect)`,
		`app.SetPublicDir(filepath.Join(root, "public"))`,
		`app.API("GET /api/health"`,
		`rootHandler, err := router.BuildChecked()`,
		`app.Mount("/", rootHandler)`,
		`ctx.SetLanguage("en")`,
		`Icons: &server.Icons{`,
		`URL:  "/favicon.ico",`,
		`Type: "image/x-icon",`,
		`server.HTMLDocument(ctx.Document(appName, body))`,
	} {
		if !strings.Contains(mainGo, snippet) {
			t.Fatalf("expected scaffold to contain %q", snippet)
		}
	}

	modulesGo := readFile(t, filepath.Join(dir, "modules", "modules.go"))
	for _, snippet := range []string{
		`_ "example.com/starter/app"`,
		`_ "example.com/starter/app/stack"`,
	} {
		if !strings.Contains(modulesGo, snippet) {
			t.Fatalf("expected scaffold module imports in modules/modules.go to contain %q", snippet)
		}
	}

	stackGSX := readFile(t, filepath.Join(dir, "app", "stack", "page.gsx"))
	for _, snippet := range []string{
		`type TransitionPanelProps struct`,
		`Title       string`,
		`Description string`,
		`component Page()`,
		`<TransitionPanel`,
		`component TransitionPanel(props: TransitionPanelProps)`,
		`{children}`,
		`<a href="/" data-gosx-link class="button primary">Back home</a>`,
	} {
		if !strings.Contains(stackGSX, snippet) {
			t.Fatalf("expected strict stack route to contain %q, got:\n%s", snippet, stackGSX)
		}
	}
	if strings.Contains(stackGSX, `func Page() Node`) {
		t.Fatalf("expected stack route to use strict component syntax, got:\n%s", stackGSX)
	}

	assertStarterFaviconICO(t, readBytes(t, filepath.Join(dir, "public", "favicon.ico")))

	assertAllGSXCompile(t, dir)

	perfBudget := readFile(t, filepath.Join(dir, "perf-budget.json"))
	if !strings.Contains(perfBudget, `"js_total_kb <= 120"`) {
		t.Fatalf("expected starter perf budget, got:\n%s", perfBudget)
	}
}

func TestRunInitDoesNotOverwriteExistingFavicon(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "starter")
	faviconPath := filepath.Join(dir, "public", "favicon.ico")
	if err := os.MkdirAll(filepath.Dir(faviconPath), 0755); err != nil {
		t.Fatal(err)
	}
	original := []byte("keep existing icon")
	if err := os.WriteFile(faviconPath, original, 0644); err != nil {
		t.Fatal(err)
	}

	err := RunInit(dir, "example.com/starter", "")
	if err == nil || !strings.Contains(err.Error(), "public/favicon.ico") || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("RunInit error = %v, want existing favicon refusal", err)
	}
	if got := readBytes(t, faviconPath); !bytes.Equal(got, original) {
		t.Fatalf("existing favicon overwritten: got %x want %x", got, original)
	}
}

func TestRunInitStarterStrictStackRouteChecksAndRenders(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "starter-strict-stack")
	if err := RunInit(dir, "example.com/starter-strict-stack", ""); err != nil {
		t.Fatal(err)
	}

	stackFile := filepath.Join(dir, "app", "stack", "page.gsx")
	var checkErr bytes.Buffer
	if err := runCheck(stackFile, &checkErr); err != nil {
		t.Fatalf("check strict stack route: %v\n%s", err, checkErr.String())
	}
	if got := checkErr.String(); !strings.Contains(got, "ok: 2 components") ||
		!strings.Contains(got, "Page") ||
		!strings.Contains(got, "TransitionPanel(TransitionPanelProps)") {
		t.Fatalf("unexpected check output for strict stack route:\n%s", got)
	}
	if strings.Contains(checkErr.String(), "warning:") {
		t.Fatalf("strict stack route should not emit check warnings:\n%s", checkErr.String())
	}

	want := `<main class="shell"><span class="eyebrow">Client Navigation</span><h1>Page transitions without a full reload</h1><p>This page was fetched as HTML, swapped into the live document, and re-used the same GoSX runtime lifecycle.</p><div class="actions"><a href="/" data-gosx-link class="button primary">Back home</a></div></main>
`
	for _, component := range []string{"", "Page"} {
		var rendered bytes.Buffer
		if err := runRender(stackFile, component, &rendered); err != nil {
			t.Fatalf("render strict stack route component %q: %v", component, err)
		}
		if normalizeIntertagWhitespace(rendered.String()) != want {
			t.Fatalf("rendered strict stack route component %q changed DOM:\n got %q\nwant %q", component, rendered.String(), want)
		}
	}
}

func TestRunInitStarterProjectBuilds(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("shells out to `go test ./...` in a generated module; race instrumentation adds no value and blows the -race timeout")
	}
	dir := filepath.Join(t.TempDir(), "starter-build")

	if err := RunInit(dir, "example.com/starter-build", ""); err != nil {
		t.Fatal(err)
	}

	addLocalGoSXReplace(t, dir)
	tidyModule(t, dir)
	goTestModule(t, dir)
}

func TestRunInitDerivesModuleNameFromDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My Demo App")

	if err := RunInit(dir, "", ""); err != nil {
		t.Fatal(err)
	}

	goMod := readFile(t, filepath.Join(dir, "go.mod"))
	if !strings.Contains(goMod, "module my-demo-app") {
		t.Fatalf("unexpected derived module: %s", goMod)
	}
}

func TestRunInitAddsLocalGoSXReplaceWhenRepoRootIsAvailable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "starter-local-replace")

	if err := RunInit(dir, "example.com/starter-local-replace", ""); err != nil {
		t.Fatal(err)
	}

	goMod := readFile(t, filepath.Join(dir, "go.mod"))
	replaceLine := "replace m31labs.dev/gosx => " + filepath.ToSlash(testRepoRoot(t))
	if !strings.Contains(goMod, replaceLine) {
		t.Fatalf("expected go.mod to contain %q, got:\n%s", replaceLine, goMod)
	}
	for _, line := range localGoSXDependencyReplaceLines(testRepoRoot(t)) {
		if !strings.Contains(goMod, strings.TrimSpace(line)) {
			t.Fatalf("expected go.mod to contain local dependency replace %q, got:\n%s", strings.TrimSpace(line), goMod)
		}
	}
}

func TestRunInitFailsWhenFileAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := RunInit(dir, "example.com/existing", ""); err == nil {
		t.Fatal("expected init to fail when scaffold file already exists")
	}
}

func TestRunInitCreatesDocsTemplate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "docs")

	if err := RunInit(dir, "example.com/docs", "docs"); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"go.mod",
		".env",
		".gitignore",
		"main.go",
		"app/layout.gsx",
		"app/modules.go",
		"app/page.server.go",
		"app/page.gsx",
		"modules/modules.go",
		"app/not-found.gsx",
		"app/error.gsx",
		"app/docs/layout.gsx",
		"app/docs/not-found.gsx",
		"app/docs/forms/page.gsx",
		"app/docs/forms/page.server.go",
		"app/docs/auth/page.gsx",
		"app/docs/auth/page.server.go",
		"app/docs/getting-started/page.gsx",
		"app/docs/getting-started/page.server.go",
		"app/docs/images/page.gsx",
		"app/docs/images/page.server.go",
		"app/docs/routing/page.gsx",
		"app/docs/routing/page.server.go",
		"app/docs/runtime/page.gsx",
		"app/docs/runtime/page.server.go",
		"public/docs.css",
		"public/runtime/watch-transport.js",
		"perf-budget.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("expected %s: %v", rel, err)
		}
	}

	mainGo := readFile(t, filepath.Join(dir, "main.go"))
	for _, snippet := range []string{
		`docsapp "example.com/docs/app"`,
		`_ "example.com/docs/modules"`,
		`_, thisFile, _, _ := runtime.Caller(0)`,
		`root := server.ResolveAppRoot(thisFile)`,
		`sessions, err := session.New(getenv("SESSION_SECRET", "gosx-docs-session-secret"), session.Options{`,
		`AllowInsecure: strings.HasPrefix(publicBase, "http://"),`,
		`docsapp.BindAuth(authn)`,
		`route.FileLayout(filepath.Join(root, "app", "layout.gsx"))`,
		`ctx.SetLanguage("en")`,
		`return server.HTMLDocument(ctx.Document("GoSX Docs", body))`,
		`router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{})`,
		`app.Use(sessions.Middleware)`,
		`app.Use(authn.Middleware)`,
		`app.Use(sessions.Protect)`,
		`app.SetPublicDir(filepath.Join(root, "public"))`,
		`app.Redirect("GET /docs", "/docs/getting-started", http.StatusTemporaryRedirect)`,
		`app.Rewrite("GET /runtime", "/docs/runtime")`,
		`app.API("GET /api/meta"`,
		`app.HandleAPI(server.APIRoute{`,
		`rootHandler, err := router.BuildChecked()`,
		`app.Mount("/", rootHandler)`,
		`ensureDocsSampleAssets(root)`,
	} {
		if !strings.Contains(mainGo, snippet) {
			t.Fatalf("expected docs scaffold to contain %q", snippet)
		}
	}

	assertAllGSXCompile(t, dir)

	runtimeServerGo := readFile(t, filepath.Join(dir, "app", "docs", "runtime", "page.server.go"))
	if !strings.Contains(runtimeServerGo, `ctx.LifecycleScript(docsapp.PublicAssetURL("runtime/watch-transport.js"))`) {
		t.Fatalf("expected runtime scaffold to register lifecycle script, got:\n%s", runtimeServerGo)
	}

	modulesGo := readFile(t, filepath.Join(dir, "modules", "modules.go"))
	for _, snippet := range []string{
		`_ "example.com/docs/app"`,
		`_ "example.com/docs/app/docs/auth"`,
		`_ "example.com/docs/app/docs/forms"`,
		`_ "example.com/docs/app/docs/getting-started"`,
		`_ "example.com/docs/app/docs/images"`,
		`_ "example.com/docs/app/docs/routing"`,
		`_ "example.com/docs/app/docs/runtime"`,
	} {
		if !strings.Contains(modulesGo, snippet) {
			t.Fatalf("expected docs module imports in modules/modules.go to contain %q", snippet)
		}
	}

	perfBudget := readFile(t, filepath.Join(dir, "perf-budget.json"))
	if !strings.Contains(perfBudget, `"/docs/runtime"`) || !strings.Contains(perfBudget, `"js_total_kb <= 160"`) {
		t.Fatalf("expected docs perf budget, got:\n%s", perfBudget)
	}
}

func TestRunInitDocsTemplateBuilds(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("shells out to `go test ./...` in a generated module; race instrumentation adds no value and blows the -race timeout")
	}
	dir := filepath.Join(t.TempDir(), "docs-build")

	if err := RunInit(dir, "example.com/docs-build", "docs"); err != nil {
		t.Fatal(err)
	}

	addLocalGoSXReplace(t, dir)
	tidyModule(t, dir)
	goTestModule(t, dir)
}

func TestRunInitRejectsUnknownTemplate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "unknown")
	if err := RunInit(dir, "example.com/unknown", "wat"); err == nil {
		t.Fatal("expected unknown template error")
	}
}

func TestSyncModulesPackageGeneratesImportsFromServerFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/auto\n\ngo 1.25.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"app/page.server.go",
		"app/blog/page.server.go",
		"app/blog/[slug]/page.server.go",
	} {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package app\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := syncModulesPackage(dir); err != nil {
		t.Fatal(err)
	}

	modulesGo := readFile(t, filepath.Join(dir, "modules", "modules.go"))
	for _, snippet := range []string{
		`// Code generated by gosx. DO NOT EDIT.`,
		`_ "example.com/auto/app"`,
		`_ "example.com/auto/app/blog"`,
	} {
		if !strings.Contains(modulesGo, snippet) {
			t.Fatalf("expected generated modules file to contain %q", snippet)
		}
	}
	// Bracket directories like [slug] produce invalid Go import paths and are
	// discovered at runtime via filesystem scanning, so they must be excluded.
	if strings.Contains(modulesGo, "[slug]") {
		t.Fatalf("modules file should not contain bracket directory imports, got:\n%s", modulesGo)
	}
}

func TestSyncModulesPackageUsesContainingModuleRoot(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "examples", "docs")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/root\n\ngo 1.25.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	serverFile := filepath.Join(projectDir, "app", "guides", "page.server.go")
	if err := os.MkdirAll(filepath.Dir(serverFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serverFile, []byte("package app\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := syncModulesPackage(projectDir); err != nil {
		t.Fatal(err)
	}

	modulesGo := readFile(t, filepath.Join(projectDir, "modules", "modules.go"))
	if !strings.Contains(modulesGo, `_ "example.com/root/examples/docs/app/guides"`) {
		t.Fatalf("expected nested module import in %q", modulesGo)
	}
}

func TestSyncModulesPackageHandlesRelativeProjectDirUnderContainingModule(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "examples", "docs")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/root\n\ngo 1.25.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	serverFile := filepath.Join(projectDir, "app", "guides", "page.server.go")
	if err := os.MkdirAll(filepath.Dir(serverFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serverFile, []byte("package app\n"), 0644); err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if chdirErr := os.Chdir(wd); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	}()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	if err := syncModulesPackage(filepath.Join("examples", "docs")); err != nil {
		t.Fatal(err)
	}

	modulesGo := readFile(t, filepath.Join(projectDir, "modules", "modules.go"))
	if !strings.Contains(modulesGo, `_ "example.com/root/examples/docs/app/guides"`) {
		t.Fatalf("expected nested module import in %q", modulesGo)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func assertStarterFaviconICO(t *testing.T, data []byte) {
	t.Helper()
	const expectedSHA256 = "a9bef67f2f133707710a890da15b0b9e087eaa1fed190b35f4f3d6e663deb735"
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != expectedSHA256 {
		t.Fatalf("starter favicon SHA-256 = %s, want %s", hex.EncodeToString(got[:]), expectedSHA256)
	}
	if len(data) != 4286 {
		t.Fatalf("starter favicon length = %d, want 4286", len(data))
	}
	if len(data) < 62 {
		t.Fatalf("starter favicon too short: %d", len(data))
	}
	if binary.LittleEndian.Uint16(data[0:2]) != 0 ||
		binary.LittleEndian.Uint16(data[2:4]) != 1 ||
		binary.LittleEndian.Uint16(data[4:6]) != 1 {
		t.Fatalf("starter favicon has invalid ICO header: % x", data[:6])
	}
	if data[6] != 32 || data[7] != 32 || data[8] != 0 || data[9] != 0 {
		t.Fatalf("starter favicon directory entry = % x, want one 32x32 truecolor image", data[6:10])
	}
	if binary.LittleEndian.Uint16(data[10:12]) != 1 || binary.LittleEndian.Uint16(data[12:14]) != 32 {
		t.Fatalf("starter favicon planes/bitcount = % x, want 1/32", data[10:14])
	}
	payloadSize := int(binary.LittleEndian.Uint32(data[14:18]))
	payloadOffset := int(binary.LittleEndian.Uint32(data[18:22]))
	if payloadOffset != 22 || payloadSize != len(data)-payloadOffset {
		t.Fatalf("starter favicon payload offset/size = %d/%d for total %d", payloadOffset, payloadSize, len(data))
	}
	payload := data[payloadOffset:]
	if binary.LittleEndian.Uint32(payload[0:4]) != 40 ||
		binary.LittleEndian.Uint32(payload[4:8]) != 32 ||
		binary.LittleEndian.Uint32(payload[8:12]) != 64 ||
		binary.LittleEndian.Uint16(payload[12:14]) != 1 ||
		binary.LittleEndian.Uint16(payload[14:16]) != 32 {
		t.Fatalf("starter favicon DIB header does not describe a 32x32 BGRA image with mask: % x", payload[:16])
	}

	pixels := payload[40 : 40+32*32*4]
	var black, light, gold int
	for i := 0; i < len(pixels); i += 4 {
		b, g, r, a := pixels[i], pixels[i+1], pixels[i+2], pixels[i+3]
		if a < 200 {
			continue
		}
		switch {
		case r < 16 && g < 16 && b < 16:
			black++
		case r > 220 && g > 220 && b > 220:
			light++
		case r > 180 && g > 140 && g < 210 && b < 100:
			gold++
		}
	}
	if black == 0 || light == 0 || gold == 0 {
		t.Fatalf("starter favicon missing expected mark colors: black=%d light=%d gold=%d", black, light, gold)
	}
}

func normalizeIntertagWhitespace(html string) string {
	return regexp.MustCompile(`>\s+<`).ReplaceAllString(html, "><")
}

func goTestModule(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test ./... in %s: %v\n%s", dir, err, output)
	}
}

func assertAllGSXCompile(t *testing.T, root string) {
	t.Helper()
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
		if _, err := gosx.Compile(source); err != nil {
			t.Fatalf("compile %s: %v", path, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}
