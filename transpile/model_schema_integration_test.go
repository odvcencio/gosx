package transpile_test

import (
	"context"
	"m31labs.dev/gosx/internal/gsxcorpus"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/transpile"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type OwnedRow struct{ Title string }
type ownedMeta struct{ Title string }
type ownedPage struct {
	Rows []OwnedRow
	Meta ownedMeta
}

func TestOwnedPackageGoExecution(t *testing.T) {
	dir := t.TempDir()
	if err := (gsxcorpus.Case{}).WriteStrictCheckModule(dir); err != nil {
		t.Fatal(err)
	}
	source := "package main\ncomponent Page(props: Props) {\n return <main><h1>{props.Meta.Title}</h1><Each of={props.Rows} as=\"row\"><p>{row.Title}</p></Each></main>\n}\n"
	for name, content := range map[string]string{
		"page.gsx": source,
		"model.go": "package main\ntype Props struct {Rows []OwnedRow; Meta Meta}\ntype OwnedRow struct {Title string}\ntype Meta struct {Title string}\n",
		"main.go":  "package main\nimport (\"fmt\";\"m31labs.dev/gosx\")\nfunc main(){fmt.Print(gosx.RenderHTML(Page(Props{Rows:[]OwnedRow{{Title:\"<one>\"},{Title:\"two\"}},Meta:Meta{Title:\"Heading\"}})))}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	files, err := transpile.LoadPackageWithOptions(ctx, filepath.Join(dir, "page.gsx"), transpile.PackageOptions{GoModels: true, Env: os.Environ()})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := transpile.TranspilePackage(files)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "page_generated.go"), []byte(generated[filepath.Join(dir, "page.gsx")]), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, output)
	}
	want, err := route.RenderProgramComponent(files[0].Program, "Page", route.ProgramRenderEnv{Props: ownedPage{Rows: []OwnedRow{{"<one>"}, {"two"}}, Meta: ownedMeta{"Heading"}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != want || want != "<main><h1>Heading</h1><p>&lt;one&gt;</p><p>two</p></main>" {
		t.Fatalf("generated %s file %s", output, want)
	}
}
