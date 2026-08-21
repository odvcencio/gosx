package format_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	gosxformat "m31labs.dev/gosx/format"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/transpile"
)

// TestWrappedTextRenderParity proves the formatter's central promise on a
// realistic multiline sentence: source indentation and line breaks do not
// leak into HTML, punctuation remains adjacent, and the strict route and
// actual generated legacy Go render agree for original/once/twice-formatted
// source.
func TestWrappedTextRenderParity(t *testing.T) {
	const source = `package main

import "m31labs.dev/gosx"

func Page() gosx.Node {
	return <article>
		<p>
			This example is a real GoSX app, not a brochure hung next to one.
					Routes, server actions, auth, client navigation, and Scene3D all live in the same
							codebase.
		</p>
	</article>
}
`
	strictSource := []byte(source)
	const wantHTML = `<article><p>This example is a real GoSX app, not a brochure hung next to one. Routes, server actions, auth, client navigation, and Scene3D all live in the same codebase.</p></article>`
	if got := renderStrict(t, strictSource); got != wantHTML {
		t.Fatalf("strict render leaked source layout or changed punctuation: got %q, want %q", got, wantHTML)
	}
	formatted, err := gosxformat.Source(strictSource)
	if err != nil {
		t.Fatalf("format.Source: %v", err)
	}
	formattedAgain, err := gosxformat.Source(formatted)
	if err != nil {
		t.Fatalf("format.Source(second pass): %v", err)
	}
	if string(formattedAgain) != string(formatted) {
		t.Fatalf("formatter did not converge\nfirst:\n%s\nsecond:\n%s", formatted, formattedAgain)
	}
	for _, tc := range []struct {
		name   string
		source []byte
	}{
		{name: "original", source: strictSource},
		{name: "formatted once", source: formatted},
		{name: "formatted twice", source: formattedAgain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderStrict(t, tc.source); got != wantHTML {
				t.Fatalf("strict render changed after formatting: got %q, want %q", got, wantHTML)
			}
			legacy, err := transpile.Transpile(tc.source, transpile.Options{SourceFile: "wrapped.gsx"})
			if err != nil {
				t.Fatalf("Transpile: %v", err)
			}
			if got := runGeneratedLegacy(t, legacy); got != wantHTML {
				t.Fatalf("generated legacy render changed or diverged: got %q, want %q", got, wantHTML)
			}
		})
	}
}

func TestMultilineLiteralNBSPRenderParityAndConvergence(t *testing.T) {
	// A line containing only NBSP is still authored text. It must survive the
	// formatter byte-for-byte and render identically through both the strict
	// IR route and the actual Go emitted by the legacy transpiler.
	const source = "package main\n\nimport \"m31labs.dev/gosx\"\n\nfunc Page() gosx.Node {\n\treturn <p>\n\t\t\u00a0\n\t</p>\n}\n"
	const wantHTML = "<p>\u00a0</p>"
	if got := renderStrict(t, []byte(source)); got != wantHTML {
		t.Fatalf("strict render discarded multiline literal NBSP: got %q, want %q", got, wantHTML)
	}
	formatted, err := gosxformat.Source([]byte(source))
	if err != nil {
		t.Fatalf("format.Source: %v", err)
	}
	if !strings.Contains(string(formatted), "\n\t\t\u00a0\n") {
		t.Fatalf("formatter discarded multiline literal NBSP source bytes:\n%s", formatted)
	}
	formattedAgain, err := gosxformat.Source(formatted)
	if err != nil {
		t.Fatalf("format.Source(second pass): %v", err)
	}
	if string(formattedAgain) != string(formatted) {
		t.Fatalf("formatter did not converge for multiline literal NBSP\nfirst:\n%s\nsecond:\n%s", formatted, formattedAgain)
	}
	for _, tc := range []struct {
		name   string
		source []byte
	}{
		{name: "original", source: []byte(source)},
		{name: "formatted once", source: formatted},
		{name: "formatted twice", source: formattedAgain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderStrict(t, tc.source); got != wantHTML {
				t.Fatalf("strict render changed literal NBSP: got %q, want %q", got, wantHTML)
			}
			legacy, err := transpile.Transpile(tc.source, transpile.Options{SourceFile: "multiline-nbsp.gsx"})
			if err != nil {
				t.Fatalf("Transpile: %v", err)
			}
			if got := runGeneratedLegacy(t, legacy); got != wantHTML {
				t.Fatalf("generated legacy render changed literal NBSP: got %q, want %q", got, wantHTML)
			}
		})
	}
}

func renderStrict(t *testing.T, source []byte) string {
	t.Helper()
	prog, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("gosx.Compile: %v", err)
	}
	html, err := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("route.RenderProgramComponent: %v", err)
	}
	return html
}

func runGeneratedLegacy(t *testing.T, generated string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	moduleRoot := filepath.Dir(filepath.Dir(thisFile))
	dir, err := os.MkdirTemp("", ".gosx-wrapped-parity-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	program := generated + "\nfunc main() { println(gosx.RenderHTML(Page())) }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(program), 0o600); err != nil {
		t.Fatalf("write generated program: %v", err)
	}
	cmd := exec.Command("go", "run", filepath.Join(dir, "main.go"))
	cmd.Dir = moduleRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run generated legacy program: %v\n%s\nprogram:\n%s", err, output, program)
	}
	return strings.TrimSpace(string(output))
}
