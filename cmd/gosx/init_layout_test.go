package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/strictcheck"
)

func TestStarterStrictLayoutChecks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "starter-layout")
	if err := RunInit(dir, "example.com/starter-layout", ""); err != nil {
		t.Fatal(err)
	}
	repo := localGoSXReplacePath()
	mod := readFile(t, filepath.Join(dir, "go.mod"))
	if repo == "" || !strings.Contains(mod, "replace m31labs.dev/gosx => "+repo+"\n") {
		t.Fatalf("generated module is not bound to source checkout %q: %s", repo, mod)
	}
	t.Logf("strict check core replacement: %s", repo)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := strictcheck.CheckFileWithOptions(ctx, filepath.Join(dir, "app", "layout.gsx"), strictcheck.Options{
		Env: append(os.Environ(), "GOPROXY=off", "GOSUMDB=off"), GOWORK: "off", GOFLAGS: "-mod=readonly",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestStarterStrictLayoutParity(t *testing.T) {
	source := appLayoutTemplate()
	program, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Components) != 1 || program.Components[0].Syntax != ir.ComponentSyntaxStrict || !program.Components[0].AcceptsChildren {
		t.Fatal("starter must contain one strict children-accepting Layout")
	}
	legacy := strings.Replace(source, "component Layout()", "func Layout() Node", 1)
	legacy = strings.Replace(legacy, "{children}", "<Slot />", 1)
	var outputs []string
	for _, text := range []string{legacy, source} {
		path := filepath.Join(t.TempDir(), "layout.gsx")
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		layout, err := route.FileLayout(path)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, gosx.RenderHTML(layout(&route.RouteContext{}, gosx.El("p", gosx.Text("<safe>")))))
	}
	// Existing strict formatting keeps inter-element whitespace. Do not
	// normalize text or attributes, which are the layout's visible contract.
	normalize := normalizeIntertagWhitespace
	if normalize(outputs[0]) != normalize(outputs[1]) {
		t.Fatalf("layout changed: legacy=%q strict=%q", outputs[0], outputs[1])
	}
	if !strings.Contains(outputs[1], "&lt;safe&gt;") {
		t.Fatal("layout lost or unescaped content")
	}
}
