package transpile_test

import (
	"context"
	"encoding/json"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/gsxcorpus"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/transpile"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type FeatureCardProps struct{ Href, Title string }
type schemaMeta struct{ Title string }
type schemaOverview struct {
	Features []FeatureCardProps
	Meta     schemaMeta
}

func TestSchemaOwnedGoExecution(t *testing.T) {
	source := []byte(`package main
component Page(props: OverviewProps) {
 return <main><h1>{props.Meta.Title}</h1><Each of={props.Features} as="feature"><Card href={feature.Href} title={feature.Title}/></Each></main>
}
component Card(props: FeatureCardProps) {
 return <Link href={props.Href}>{props.Title}</Link>
}
`)
	defs := []ir.StructSchema{
		{Name: "OverviewProps", Fields: []ir.SchemaField{{Name: "Features", Type: "[]FeatureCardProps"}, {Name: "Meta", Type: "MetaProps"}}},
		{Name: "FeatureCardProps", Fields: []ir.SchemaField{{Name: "Href", Type: "string"}, {Name: "Title", Type: "string"}}},
		{Name: "MetaProps", Fields: []ir.SchemaField{{Name: "Title", Type: "string"}}},
	}
	for i := range defs {
		defs[i].Origin = ir.SchemaOrigin{File: "model.go", Line: i + 2}
	}
	schemas, err := ir.NewSchemaSet("main", defs)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := transpile.Transpile(source, transpile.Options{Schemas: schemas})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"OverviewProps", "FeatureCardProps", "MetaProps"} {
		if strings.Contains(generated, "type "+name) {
			t.Fatal("duplicate declaration", generated)
		}
	}
	dir := t.TempDir()
	if err := (gsxcorpus.Case{}).WriteStrictCheckModule(dir); err != nil {
		t.Fatal(err)
	}
	model := `package main
type OverviewProps struct{Features []FeatureCardProps;Meta MetaProps}
type FeatureCardProps struct{Href,Title string}
type MetaProps struct{Title string}
`
	runner := `package main
import("encoding/json";"os";"m31labs.dev/gosx")
func main(){json.NewEncoder(os.Stdout).Encode([]string{
gosx.RenderHTMLWithContext(Page(OverviewProps{Features:[]FeatureCardProps{{Href:"/docs",Title:"<card>"},{Href:"/other?a=1&b=2",Title:"Second & card"}},Meta:MetaProps{Title:"Overview"}}),gosx.RenderContext{RequestTarget:"/docs"}),
gosx.RenderHTMLWithContext(Page(OverviewProps{Meta:MetaProps{Title:"Empty"}}),gosx.RenderContext{RequestTarget:"/docs"}),
})}
`
	for name, content := range map[string]string{"model.go": model, "page.go": generated, "main.go": runner} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "run", ".")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	output, err := run()
	if err != nil {
		t.Fatal(err, output)
	}
	program, err := gosx.CompileWithOptions(source, gosx.CompileOptions{Schemas: schemas})
	if err != nil {
		t.Fatal(err)
	}
	var outputs []string
	if err := json.Unmarshal([]byte(output), &outputs); err != nil || len(outputs) != 2 {
		t.Fatalf("outputs: %v %s", err, output)
	}
	for i, props := range []schemaOverview{{Features: []FeatureCardProps{{Href: "/docs", Title: "<card>"}, {Href: "/other?a=1&b=2", Title: "Second & card"}}, Meta: schemaMeta{Title: "Overview"}}, {Meta: schemaMeta{Title: "Empty"}}} {
		expected, err := route.RenderProgramComponent(program, "Page", route.ProgramRenderEnv{Props: props, Values: map[string]any{"request": map[string]any{"target": "/docs"}}})
		if err != nil || outputs[i] != expected {
			t.Fatalf("case %d: %v generated %s\nfile %s", i, err, outputs[i], expected)
		}
	}
	for _, fragment := range []string{"<h1>Overview</h1>", "&lt;card&gt;", "Second &amp; card", "href=\"/other?a=1&amp;b=2\"", "aria-current=\"page\""} {
		if !strings.Contains(outputs[0], fragment) {
			t.Fatalf("missing %q: %s", fragment, outputs[0])
		}
	}
	if strings.Count(outputs[0], "<a ") != 2 || outputs[1] != "<main><h1>Empty</h1></main>" {
		t.Fatalf("row/empty output: %q", outputs)
	}
	// Metadata supplies visibility, not certification of the actual declaration.
	if err := os.WriteFile(filepath.Join(dir, "model.go"), []byte(strings.Replace(model, "Href,Title string", "Href int;Title string", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	output, err = run()
	if err == nil || !strings.Contains(output, "props.Href") {
		t.Fatalf("mismatched authored model accepted: %v %s", err, output)
	}
	t.Log("authored-model mismatch rejected:", output)
}
