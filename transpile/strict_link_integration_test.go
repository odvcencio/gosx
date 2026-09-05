package transpile_test

import (
	"context"
	"encoding/json"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/gsxcorpus"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/strictcheck"
	"m31labs.dev/gosx/transpile"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type LinkRow struct {
	Href  string
	Label string
}
type linkPageProps struct {
	Rows     []LinkRow
	Href     string
	Label    string
	Prefetch bool
}

func TestStrictManagedLinkRealGoParity(t *testing.T) {
	source := []byte(`package main
type LinkRow struct { Href string; Label string }
type PageProps struct { Rows []LinkRow; Href string; Label string; Prefetch bool }
component Page(props: PageProps) {
 return <main><Frame><Link slot="Header" href="/header">Header</Link><Link className="nav" href={props.Href} prefetch={props.Prefetch} aria-current="step">{props.Label}</Link></Frame><Each of={props.Rows} as="row" index="i"><Frame><Link slot="Header" data-index={i} href={row.Href}>{row.Label}</Link></Frame></Each><Link href="/other"/></main>
}

component Frame() {
 return <section>{slotHeader}{children}</section>
}
`)
	dir := t.TempDir()
	if err := (gsxcorpus.Case{}).WriteStrictCheckModule(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "page.gsx")
	if err := os.WriteFile(path, source, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := strictcheck.CheckFileWithOptions(ctx, path, strictcheck.Options{}); err != nil {
		t.Fatal(err)
	}
	generated, err := transpile.Transpile(source, transpile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "page.go"), []byte(generated), 0600); err != nil {
		t.Fatal(err)
	}
	runner := `package main
import("encoding/json";"os";"m31labs.dev/gosx")
func main(){var out []string;for _,target:=range []string{"/docs?tab=one","/docs?tab=two","/docs/child","/else"}{out=append(out,gosx.RenderHTMLWithContext(Page(PageProps{Href:"/docs?tab=one",Label:"<docs>&",Prefetch:true,Rows:[]LinkRow{{Href:"/docs",Label:"row"}}}),gosx.RenderContext{RequestTarget:target}))};json.NewEncoder(os.Stdout).Encode(out)}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(runner), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real generated Go: %v\n%s", err, output)
	}
	var actual []string
	if err := json.Unmarshal(output, &actual); err != nil {
		t.Fatal(err, string(output))
	}
	program, err := gosx.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	props := linkPageProps{Href: "/docs?tab=one", Label: "<docs>&", Prefetch: true, Rows: []LinkRow{{Href: "/docs", Label: "row"}}}
	for i, target := range []string{"/docs?tab=one", "/docs?tab=two", "/docs/child", "/else"} {
		expected, err := route.RenderProgramComponent(program, "Page", route.ProgramRenderEnv{Props: props, Values: map[string]any{"request": map[string]any{"target": target}}})
		if err != nil {
			t.Fatal(err)
		}
		if actual[i] != expected {
			t.Fatalf("%s generated %s\nfile %s", target, actual[i], expected)
		}
	}
	if _, err := route.RenderProgramComponent(program, "Page", route.ProgramRenderEnv{Props: struct {
		Href     int
		Label    string
		Prefetch bool
		Rows     []LinkRow
	}{Href: 42}}); err == nil {
		t.Fatal("wrong runtime href accepted")
	}
}

func TestStrictManagedLinkLocalShadowRender(t *testing.T) {
	source := []byte("package app\ncomponent Page() {\n return <Link/>\n}\ncomponent Link() {\n return <strong>local</strong>\n}\n")
	program, err := gosx.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	html, err := route.RenderProgramComponent(program, "Page", route.ProgramRenderEnv{})
	if err != nil || html != "<strong>local</strong>" {
		t.Fatal(html, err)
	}
}
