package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
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
		"public/styles.css",
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
		`session.MustNew(getenv("SESSION_SECRET", "gosx-app-session-secret"), session.Options{})`,
		`router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{})`,
		`app.EnableNavigation()`,
		`app.Use(sessions.Middleware)`,
		`app.Use(sessions.Protect)`,
		`app.SetPublicDir(filepath.Join(root, "public"))`,
		`app.API("GET /api/health"`,
		`app.Mount("/", router.Build())`,
		`server.HTMLDocument(ctx.Title(appName), ctx.Head(), body)`,
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

	assertAllGSXCompile(t, dir)
}

func TestRunInitStarterProjectBuilds(t *testing.T) {
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
		`session.MustNew(getenv("SESSION_SECRET", "gosx-docs-session-secret"), session.Options{})`,
		`docsapp.BindAuth(authn)`,
		`route.FileLayout(filepath.Join(root, "app", "layout.gsx"))`,
		`return server.HTMLDocument(ctx.Title("GoSX Docs"), ctx.Head(), body)`,
		`router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{})`,
		`app.Use(sessions.Middleware)`,
		`app.Use(authn.Middleware)`,
		`app.Use(sessions.Protect)`,
		`app.SetPublicDir(filepath.Join(root, "public"))`,
		`app.Redirect("GET /docs", "/docs/getting-started", http.StatusTemporaryRedirect)`,
		`app.Rewrite("GET /runtime", "/docs/runtime")`,
		`app.API("GET /api/meta"`,
		`app.HandleAPI(server.APIRoute{`,
		`app.Mount("/", router.Build())`,
		`ensureDocsSampleAssets(root)`,
	} {
		if !strings.Contains(mainGo, snippet) {
			t.Fatalf("expected docs scaffold to contain %q", snippet)
		}
	}

	assertAllGSXCompile(t, dir)

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
}

func TestRunInitDocsTemplateBuilds(t *testing.T) {
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
		`_ "example.com/auto/app/blog/[slug]"`,
	} {
		if !strings.Contains(modulesGo, snippet) {
			t.Fatalf("expected generated modules file to contain %q", snippet)
		}
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
