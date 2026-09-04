package uirecipe

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
)

func TestEmbeddedCatalogIsCanonicalAndDeterministic(t *testing.T) {
	first, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"button", "card", "input", "tokens"}
	var names []string
	for _, item := range first.List() {
		names = append(names, item.Name)
		if item.Version != "1.0.0" {
			t.Fatalf("%s version = %q", item.Name, item.Version)
		}
	}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("recipe names = %v, want %v", names, wantNames)
	}
	if !reflect.DeepEqual(first.List(), second.List()) {
		t.Fatal("loading the embedded catalog changed list order or values")
	}
	if first.license != "MIT" || first.source == "" || first.provenance == "" {
		t.Fatalf("missing provenance: %#v", first)
	}
}

func TestCatalogRejectsTraversalAndMalformedSource(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		files    fstest.MapFS
		want     string
	}{
		{
			name: "target traversal",
			manifest: testCatalogManifest(`{
        "name":"bad","version":"1.0.0","description":"bad","dependencies":[],
        "files":[{"source":"valid.css","target":"../escape.css"}]
      }`),
			files: fstest.MapFS{"valid.css": &fstest.MapFile{Data: []byte(".ok {}\n")}},
			want:  "invalid target path",
		},
		{
			name: "windows traversal",
			manifest: testCatalogManifest(`{
        "name":"bad","version":"1.0.0","description":"bad","dependencies":[],
        "files":[{"source":"valid.css","target":"..\\escape.css"}]
      }`),
			files: fstest.MapFS{"valid.css": &fstest.MapFile{Data: []byte(".ok {}\n")}},
			want:  "invalid target path",
		},
		{
			name: "malformed gsx",
			manifest: testCatalogManifest(`{
        "name":"bad","version":"1.0.0","description":"bad","dependencies":[],
        "files":[{"source":"bad.gsx","target":"app/ui/bad.gsx"}]
      }`),
			files: fstest.MapFS{"bad.gsx": &fstest.MapFile{Data: []byte("package ui\ncomponent Bad( {\n")}},
			want:  "compile",
		},
		{
			name: "malformed css",
			manifest: testCatalogManifest(`{
        "name":"bad","version":"1.0.0","description":"bad","dependencies":[],
        "files":[{"source":"bad.css","target":"public/ui/bad.css"}]
      }`),
			files: fstest.MapFS{"bad.css": &fstest.MapFile{Data: []byte(".bad {\n")}},
			want:  "unterminated",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files := fstest.MapFS{"manifest.json": &fstest.MapFile{Data: []byte(test.manifest)}}
			for name, file := range test.files {
				files[name] = file
			}
			_, err := loadCatalog(files)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestAddIsDeterministicAndIdempotent(t *testing.T) {
	catalog := mustCatalog(t)
	firstRoot := testAppRoot(t)
	secondRoot := testAppRoot(t)

	first, err := catalog.Add(firstRoot, "button", AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := catalog.Add(secondRoot, "button", AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("add results differ:\n%#v\n%#v", first, second)
	}
	for _, action := range first.Files {
		if action.Action != "create" {
			t.Fatalf("first add action = %#v", action)
		}
		left := mustRead(t, filepath.Join(firstRoot, filepath.FromSlash(action.Path)))
		right := mustRead(t, filepath.Join(secondRoot, filepath.FromSlash(action.Path)))
		if !bytes.Equal(left, right) {
			t.Fatalf("installed %s differs across roots", action.Path)
		}
	}
	leftManifest := mustRead(t, filepath.Join(firstRoot, filepath.FromSlash(installedManifestPath)))
	rightManifest := mustRead(t, filepath.Join(secondRoot, filepath.FromSlash(installedManifestPath)))
	if !bytes.Equal(leftManifest, rightManifest) {
		t.Fatal("installed manifests are not deterministic")
	}
	wantManifest := mustRead(t, "testdata/button-manifest.golden.json")
	if !bytes.Equal(leftManifest, wantManifest) {
		t.Fatalf("installed manifest differs from golden\ngot:\n%s\nwant:\n%s", leftManifest, wantManifest)
	}

	before := snapshotFiles(t, firstRoot, append(first.Files, FileAction{Path: installedManifestPath})...)
	again, err := catalog.Add(firstRoot, "button", AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range again.Files {
		if action.Action != "unchanged" {
			t.Fatalf("idempotent add action = %#v", action)
		}
	}
	after := snapshotFiles(t, firstRoot, append(again.Files, FileAction{Path: installedManifestPath})...)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("idempotent add changed installed bytes")
	}
	diff, err := catalog.Diff(firstRoot, "button")
	if err != nil {
		t.Fatal(err)
	}
	if !diff.Clean {
		t.Fatalf("fresh install differs: %#v", diff)
	}
}

func TestAddConflictMakesNoPartialWrites(t *testing.T) {
	catalog := mustCatalog(t)
	root := testAppRoot(t)
	conflict := filepath.Join(root, "app", "ui", "button.gsx")
	if err := os.MkdirAll(filepath.Dir(conflict), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflict, []byte("local ownership\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := catalog.Add(root, "button", AddOptions{})
	if err == nil || !strings.Contains(err.Error(), "ui diff button") {
		t.Fatalf("error = %v", err)
	}
	if got := string(mustRead(t, conflict)); got != "local ownership\n" {
		t.Fatalf("conflicting source changed to %q", got)
	}
	for _, relative := range []string{"public/ui/tokens.css", "public/ui/button.css", installedManifestPath} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); !os.IsNotExist(err) {
			t.Fatalf("conflict left partial file %s: %v", relative, err)
		}
	}
}

func TestUpdateAdvancesOnlyPreviouslyUnmodifiedFiles(t *testing.T) {
	catalog := mustCatalog(t)
	root := testAppRoot(t)
	if _, err := catalog.Add(root, "button", AddOptions{}); err != nil {
		t.Fatal(err)
	}
	future := cloneCatalog(catalog)
	button := future.recipes["button"]
	for i := range button.files {
		if button.files[i].Target == "public/ui/button.css" {
			button.files[i].Content = append(button.files[i].Content, []byte("/* catalog 1.1 */\n")...)
			button.files[i].SHA256 = contentHash(button.files[i].Content)
		}
	}
	additive := recipeFile{
		Source:  "button/additive.css",
		Target:  "public/ui/button-addon.css",
		Content: []byte(".gsx-button-addon {}\n"),
	}
	additive.SHA256 = contentHash(additive.Content)
	button.files = append(button.files, additive)
	button.Version = "1.1.0"
	button.Summary.Version = "1.1.0"
	future.recipes["button"] = button

	result, err := future.Add(root, "button", AddOptions{Update: true})
	if err != nil {
		t.Fatal(err)
	}
	wantsUpdate := false
	wantsCreate := false
	for _, action := range result.Files {
		if action.Path == "public/ui/button.css" && action.Action == "update" {
			wantsUpdate = true
		}
		if action.Path == "public/ui/button-addon.css" && action.Action == "create" {
			wantsCreate = true
		}
	}
	if !wantsUpdate || !wantsCreate {
		t.Fatalf("result does not contain guarded update: %#v", result)
	}
	if !bytes.Contains(mustRead(t, filepath.Join(root, "public", "ui", "button.css")), []byte("catalog 1.1")) {
		t.Fatal("catalog update was not installed")
	}
	if got := string(mustRead(t, filepath.Join(root, "public", "ui", "button-addon.css"))); got != ".gsx-button-addon {}\n" {
		t.Fatalf("additive catalog file = %q", got)
	}
	manifest := string(mustRead(t, filepath.Join(root, filepath.FromSlash(installedManifestPath))))
	if !strings.Contains(manifest, "public/ui/button-addon.css") {
		t.Fatal("additive catalog file was not recorded in the installed manifest")
	}
}

func TestUpdateRejectsLocalModificationBeforeAdditiveWrite(t *testing.T) {
	catalog := mustCatalog(t)
	root := testAppRoot(t)
	if _, err := catalog.Add(root, "button", AddOptions{}); err != nil {
		t.Fatal(err)
	}
	buttonPath := filepath.Join(root, "app", "ui", "button.gsx")
	local := append(mustRead(t, buttonPath), []byte("// local application change\n")...)
	if err := os.WriteFile(buttonPath, local, 0o644); err != nil {
		t.Fatal(err)
	}

	future := cloneCatalog(catalog)
	button := future.recipes["button"]
	for i := range button.files {
		if button.files[i].Target == "app/ui/button.gsx" {
			button.files[i].Content = append(button.files[i].Content, []byte("// catalog 1.1\n")...)
			button.files[i].SHA256 = contentHash(button.files[i].Content)
		}
	}
	additive := recipeFile{
		Source:  "button/additive.css",
		Target:  "public/ui/additive.css",
		Content: []byte(".gsx-additive {}\n"),
	}
	additive.SHA256 = contentHash(additive.Content)
	button.files = append(button.files, additive)
	future.recipes["button"] = button

	_, err := future.Add(root, "button", AddOptions{Update: true})
	if err == nil || !strings.Contains(err.Error(), "modified after installation") || !strings.Contains(err.Error(), "ui diff button") {
		t.Fatalf("error = %v", err)
	}
	if !bytes.Equal(mustRead(t, buttonPath), local) {
		t.Fatal("local modification was overwritten")
	}
	if _, err := os.Stat(filepath.Join(root, "public", "ui", "additive.css")); !os.IsNotExist(err) {
		t.Fatalf("guarded update made an additive partial write: %v", err)
	}
}

func TestAddAndDiffRejectSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink setup is not reliably available without Windows developer mode")
	}
	catalog := mustCatalog(t)
	root := testAppRoot(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "public")); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Add(root, "tokens", AddOptions{}); err == nil || !strings.Contains(err.Error(), "is a symlink") {
		t.Fatalf("Add error = %v", err)
	}
	if _, err := catalog.Diff(root, "tokens"); err == nil || !strings.Contains(err.Error(), "is a symlink") {
		t.Fatalf("Diff error = %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("symlink escape wrote outside root: %v", entries)
	}
}

func TestDiffReportsModifiedAndMissingFilesDeterministically(t *testing.T) {
	catalog := mustCatalog(t)
	root := testAppRoot(t)
	if _, err := catalog.Add(root, "button", AddOptions{}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "app", "ui", "button.gsx")
	if err := os.WriteFile(path, []byte("package ui\n// local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "public", "ui", "button.css")); err != nil {
		t.Fatal(err)
	}
	first, err := catalog.Diff(root, "button")
	if err != nil {
		t.Fatal(err)
	}
	second, err := catalog.Diff(root, "button")
	if err != nil {
		t.Fatal(err)
	}
	if first.Clean || !reflect.DeepEqual(first, second) {
		t.Fatalf("diff is clean or nondeterministic:\n%#v\n%#v", first, second)
	}
	statuses := map[string]string{}
	for _, entry := range first.Entries {
		statuses[entry.Path] = entry.Status
		if entry.Status != "unchanged" && (!strings.Contains(entry.Patch, "--- ") || !strings.Contains(entry.Patch, "+++ catalog/")) {
			t.Fatalf("missing unified patch for %#v", entry)
		}
	}
	if statuses["app/ui/button.gsx"] != "modified" || statuses["public/ui/button.css"] != "missing" || statuses["public/ui/tokens.css"] != "unchanged" {
		t.Fatalf("statuses = %#v", statuses)
	}
}

func TestRecipeComponentsCompileAndRenderSemanticStates(t *testing.T) {
	catalog := mustCatalog(t)

	buttonProgram, err := gosx.Compile(recipeContent(t, catalog, "button", ".gsx"))
	if err != nil {
		t.Fatal(err)
	}
	type ButtonProps struct {
		Type     string
		Variant  string
		Size     string
		Disabled bool
	}
	buttonHTML, err := route.RenderProgramComponent(buttonProgram, "Button", route.ProgramRenderEnv{Props: ButtonProps{
		Type: "submit", Variant: "primary", Size: "md", Disabled: true,
	}}, gosx.Text("Save"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<button`, `type="submit"`, `disabled`, `gsx-button--primary`, `Save`, `</button>`} {
		if !strings.Contains(buttonHTML, want) {
			t.Fatalf("button HTML %q missing %q", buttonHTML, want)
		}
	}

	cardProgram, err := gosx.Compile(recipeContent(t, catalog, "card", ".gsx"))
	if err != nil {
		t.Fatal(err)
	}
	type CardProps struct{ Variant, Eyebrow, Title, Description string }
	cardHTML, err := route.RenderProgramComponent(cardProgram, "Card", route.ProgramRenderEnv{Props: CardProps{
		Variant: "raised", Eyebrow: "Preview", Title: "Balance", Description: "Current total",
	}}, gosx.El("strong", gosx.Text("$42")))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<article`, `<header`, `<h3`, `<strong>$42</strong>`, `gsx-card--raised`} {
		if !strings.Contains(cardHTML, want) {
			t.Fatalf("card HTML %q missing %q", cardHTML, want)
		}
	}

	inputProgram, err := gosx.Compile(recipeContent(t, catalog, "input", ".gsx"))
	if err != nil {
		t.Fatal(err)
	}
	type InputProps struct {
		ID, Name, Type, Label, Placeholder, Value, Help, Error string
		Required, Disabled, Invalid                            bool
	}
	inputHTML, err := route.RenderProgramComponent(inputProgram, "Input", route.ProgramRenderEnv{Props: InputProps{
		ID: "email", Name: "email", Type: "email", Label: "Email", Placeholder: "you@example.com",
		Help: "Used for receipts.", Error: "Enter a valid address.", Required: true, Invalid: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<label`, `for="email"`, `type="email"`, `aria-invalid="true"`, `aria-describedby="email-help email-error"`, `required`, `role="alert"`} {
		if !strings.Contains(inputHTML, want) {
			t.Fatalf("input HTML %q missing %q", inputHTML, want)
		}
	}
}

func TestComponentStylesUseTokensAndCompleteInteractionStates(t *testing.T) {
	catalog := mustCatalog(t)
	for _, name := range []string{"button", "card", "input"} {
		css := string(recipeContent(t, catalog, name, ".css"))
		for _, forbidden := range []string{"#", "rgb(", "rgba(", "hsl(", "hsla(", "!important"} {
			if strings.Contains(css, forbidden) {
				t.Fatalf("%s component CSS contains raw %q value", name, forbidden)
			}
		}
	}
	button := string(recipeContent(t, catalog, "button", ".css"))
	for _, want := range []string{":hover", ":focus-visible", ":active", ":disabled", "prefers-reduced-motion"} {
		if !strings.Contains(button, want) {
			t.Fatalf("button CSS missing %q", want)
		}
	}
	input := string(recipeContent(t, catalog, "input", ".css"))
	for _, want := range []string{":hover", ":focus-visible", ":active", ":disabled", "[data-invalid]", "prefers-reduced-motion"} {
		if !strings.Contains(input, want) {
			t.Fatalf("input CSS missing %q", want)
		}
	}
}

func TestUnusedRecipesShipZeroBytesInApplicationDependencyGraph(t *testing.T) {
	command := exec.Command("go", "list", "-deps", "m31labs.dev/gosx")
	command.Dir = filepath.Join("..", "..")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list application dependency graph: %v", err)
	}
	for _, packagePath := range strings.Fields(string(output)) {
		if packagePath == "m31labs.dev/gosx/internal/uirecipe" || strings.Contains(packagePath, "/cmd/gosx") {
			t.Fatalf("CLI-only recipe catalog leaked into application dependency graph as %q", packagePath)
		}
	}
}

func TestRecipeDocumentationPinsOwnershipAndRuntimeBoundary(t *testing.T) {
	document := string(mustRead(t, filepath.Join("..", "..", "docs", "ui-recipes.md")))
	for _, want := range []string{
		"## Visual System", "gosx ui list", "gosx ui add button", "gosx ui diff button",
		"--update", "no force mode", "contributes exactly zero source", "server-binary bytes",
		"app/ui", "public/ui", ".gosx/ui/manifest.json", "WCAG AAA",
	} {
		if !strings.Contains(document, want) {
			t.Fatalf("documentation missing %q", want)
		}
	}
}

func testCatalogManifest(recipeJSON string) string {
	return `{
  "schemaVersion":1,
  "catalogVersion":"test",
  "source":"test",
  "license":"MIT",
  "provenance":"test",
  "recipes":[` + recipeJSON + `]
}`
}

func mustCatalog(t *testing.T) *Catalog {
	t.Helper()
	catalog, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func testAppRoot(t *testing.T) string {
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

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func snapshotFiles(t *testing.T, root string, files ...FileAction) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, file := range files {
		out[file.Path] = contentHash(mustRead(t, filepath.Join(root, filepath.FromSlash(file.Path))))
	}
	return out
}

func cloneCatalog(source *Catalog) *Catalog {
	clone := *source
	clone.recipes = make(map[string]recipe, len(source.recipes))
	for name, item := range source.recipes {
		item.Dependencies = append([]string(nil), item.Dependencies...)
		item.Files = append([]string(nil), item.Files...)
		item.files = append([]recipeFile(nil), item.files...)
		for i := range item.files {
			item.files[i].Content = append([]byte(nil), item.files[i].Content...)
		}
		clone.recipes[name] = item
	}
	return &clone
}

func recipeContent(t *testing.T, catalog *Catalog, name, extension string) []byte {
	t.Helper()
	item := catalog.recipes[name]
	for _, file := range item.files {
		if filepath.Ext(file.Source) == extension {
			return file.Content
		}
	}
	t.Fatalf("recipe %s has no %s source", name, extension)
	return nil
}

func TestInstalledManifestRejectsTraversal(t *testing.T) {
	root := testAppRoot(t)
	path := filepath.Join(root, filepath.FromSlash(installedManifestPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := installedManifest{
		SchemaVersion: 1,
		Recipes: []installedRecipe{{Name: "tokens", Version: "1", Files: []installedFile{{
			Path: "../outside", SHA256: strings.Repeat("0", 64),
		}}}},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readInstalledManifest(root); err == nil || !strings.Contains(err.Error(), "invalid installed path") {
		t.Fatalf("error = %v", err)
	}
}

var _ fs.FS = fstest.MapFS{}
