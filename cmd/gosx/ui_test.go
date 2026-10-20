package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/uirecipe"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/transpile"
)

func TestRunUIListMatchesGolden(t *testing.T) {
	var output bytes.Buffer
	if err := runUICommand([]string{"list"}, &output); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/ui-list.golden")
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != string(want) {
		t.Fatalf("ui list differs from golden\ngot:\n%s\nwant:\n%s", output.String(), want)
	}
}

func TestRunUIAddThenDiff(t *testing.T) {
	root := uiTestAppRoot(t)
	var output bytes.Buffer
	if err := runUICommand([]string{"add", "--root", root, "card"}, &output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"added card@1.0.0", "app/ui/card.gsx", "public/ui/card.css", "public/ui/tokens.css", ".gosx/ui/manifest.json"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("add output %q missing %q", output.String(), want)
		}
	}
	output.Reset()
	if err := runUICommand([]string{"diff", "--root", root, "card"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "clean") {
		t.Fatalf("diff output = %q", output.String())
	}

	card := filepath.Join(root, "app", "ui", "card.gsx")
	if err := os.WriteFile(card, []byte("package ui\n// owned here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	err := runUICommand([]string{"diff", "--root", root, "card"}, &output)
	if !errors.Is(err, uirecipe.ErrDifferences) {
		t.Fatalf("diff error = %v", err)
	}
	if !strings.Contains(output.String(), "modified  app/ui/card.gsx") || !strings.Contains(output.String(), "+++ catalog/app/ui/card.gsx") {
		t.Fatalf("modified diff output = %q", output.String())
	}
}

func TestUICommandProcessIntegration(t *testing.T) {
	if os.Getenv("GOSX_UI_PROCESS") == "1" {
		args := strings.Split(os.Getenv("GOSX_UI_ARGS"), "\x1f")
		os.Args = append([]string{"gosx"}, args...)
		main()
		os.Exit(0)
	}
	root := uiTestAppRoot(t)
	stdout, stderr, err := runUIProcess(t, "ui", "add", "--root", root, "input")
	if err != nil {
		t.Fatalf("process add: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "added input@1.0.0") {
		t.Fatalf("process stdout = %q", stdout)
	}
	stdout, stderr, err = runUIProcess(t, "ui", "diff", "--root", root, "input")
	if err != nil {
		t.Fatalf("process diff: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "clean") {
		t.Fatalf("process diff stdout = %q", stdout)
	}
}

func TestUICommandRejectsUnknownRecipeAndUpdateOnDiff(t *testing.T) {
	root := uiTestAppRoot(t)
	var output bytes.Buffer
	if err := runUICommand([]string{"add", "--root", root, "unknown"}, &output); err == nil || !strings.Contains(err.Error(), "gosx ui list") {
		t.Fatalf("unknown recipe error = %v", err)
	}
	if err := runUICommand([]string{"diff", "--update", "--root", root, "button"}, &output); err == nil {
		t.Fatal("diff accepted --update")
	}
}

func TestStarterScaffoldDoesNotInstallUnusedUIRecipes(t *testing.T) {
	files, err := scaffoldFilesForTemplate("example.com/app", initTemplateApp)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasPrefix(file.Path, "app/ui/") || strings.HasPrefix(file.Path, "public/ui/") || strings.HasPrefix(file.Path, ".gosx/ui/") {
			t.Fatalf("starter scaffold ships unused UI recipe bytes in %s", file.Path)
		}
	}
}

func TestInstalledRecipeSourcesPassCLIFormatCompileCheckAndSharedRender(t *testing.T) {
	catalog, err := uirecipe.Load()
	if err != nil {
		t.Fatal(err)
	}
	root := uiTestAppRoot(t)
	for _, name := range []string{"button", "card", "input"} {
		if _, err := catalog.Add(root, name, uirecipe.AddOptions{}); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}
	for _, name := range []string{"button", "card", "input"} {
		path := filepath.Join(root, "app", "ui", name+".gsx")
		if _, err := RunFmtCheck(path, io.Discard); err != nil {
			t.Fatalf("gosx fmt --check %s: %v", name, err)
		}
		catalogPath := filepath.Join("..", "..", "internal", "uirecipe", "recipes", "v1", name, name+".gsx")
		if err := runCheck(catalogPath, io.Discard); err != nil {
			t.Fatalf("gosx check %s: %v", name, err)
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := transpile.Transpile(source, transpile.Options{SourceFile: path})
		if err != nil {
			t.Fatalf("gosx compile %s: %v", name, err)
		}
		if !strings.Contains(compiled, "package ui") {
			t.Fatalf("compiled %s source has no ui package:\n%s", name, compiled)
		}
	}

	page := filepath.Join(root, "app", "page.gsx")
	if err := os.WriteFile(page, []byte(`package app

import ui "./ui"

component Page() {
	return <main>
		<ui.Button Type="button" Variant="primary" Size="md" Disabled={false}>Save</ui.Button>
	</main>
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	node, err := route.DefaultFileRenderer(nil, route.FilePage{FilePath: page, Pattern: "/"})
	if err != nil {
		t.Fatalf("render shared installed recipe: %v", err)
	}
	html := gosx.RenderHTML(node)
	if !strings.Contains(html, `class="gsx-button gsx-button--primary gsx-button--md"`) || !strings.Contains(html, "Save") {
		t.Fatalf("shared installed recipe HTML = %q", html)
	}
}

func uiTestAppRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.25.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func runUIProcess(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=TestUICommandProcessIntegration")
	command.Env = append(os.Environ(),
		"GOSX_UI_PROCESS=1",
		"GOSX_UI_ARGS="+strings.Join(args, "\x1f"),
	)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}
