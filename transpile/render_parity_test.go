package transpile

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/format"
	"m31labs.dev/gosx/route"
)

// TestStrictAndGeneratedLegacyRenderParity exercises the actual generated Go
// program, rather than comparing transpiler strings. The temporary program
// runs the legacy projection through gosx.RenderHTML, while the same source is
// rendered through the strict file IR before and after two formatter passes.
// This is the contract that catches whitespace changes hidden by equivalent
// source snapshots.
func TestStrictAndGeneratedLegacyRenderParity(t *testing.T) {
	const source = `package main

import "m31labs.dev/gosx"

func Page() gosx.Node {
	return <p>
		Hello
		{" "}
		world.
	</p>
}
`

	strictSource := []byte(source)
	strictHTML := renderStrict(t, strictSource)
	if want := `<p>Hello world.</p>`; strictHTML != want {
		t.Fatalf("strict render did not apply intentional expression spacing and line normalization: got %q, want %q", strictHTML, want)
	}
	formatted, err := format.Source(strictSource)
	if err != nil {
		t.Fatalf("format.Source: %v", err)
	}
	formattedAgain, err := format.Source(formatted)
	if err != nil {
		t.Fatalf("format.Source(second pass): %v", err)
	}
	if string(formattedAgain) != string(formatted) {
		t.Fatalf("formatter did not converge\nfirst:\n%s\nsecond:\n%s", formatted, formattedAgain)
	}
	if got := renderStrict(t, formatted); got != strictHTML {
		t.Fatalf("strict render changed after formatting: before=%q after=%q", strictHTML, got)
	}
	if got := renderStrict(t, formattedAgain); got != strictHTML {
		t.Fatalf("strict render changed after second formatting pass: before=%q after=%q", strictHTML, got)
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
			legacy, err := Transpile(tc.source, Options{SourceFile: "parity.gsx"})
			if err != nil {
				t.Fatalf("Transpile: %v", err)
			}
			legacyHTML := runGeneratedLegacy(t, legacy)
			if legacyHTML != strictHTML {
				t.Fatalf("generated legacy render differs from strict route render: legacy=%q strict=%q\nlegacy source:\n%s", legacyHTML, strictHTML, legacy)
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
	dir, err := os.MkdirTemp("", ".gosx-render-parity-")
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
