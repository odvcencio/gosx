package evalparity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx/transpile"
)

// transpileResult is one case's outcome from the transpile backend:
// exactly one of HTML or Err is set.
type transpileResult struct {
	HTML string
	Err  string
}

// gosxModuleRoot locates the checked-out gosx module this test binary was
// built from, by walking up from this source file's own path — stable
// regardless of `go test`'s working directory.
func gosxModuleRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// thisFile: <root>/internal/evalparity/transpile_run.go
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// transpilePreamble is the exact, fixed prefix every case's generated
// source produces through transpile.Transpile (see source.go's gsxSource):
// one package clause and one single-line gosx import — transpile.go
// collapses the blank lines gsxSource writes between declarations, so
// there is no blank line here either. The batch build strips it per case
// and supplies one shared copy in the assembled program.
const transpilePreamble = "package app\nimport gosx \"m31labs.dev/gosx\"\n"

func stripTranspilePreamble(id, src string) (string, error) {
	if !strings.HasPrefix(src, transpilePreamble) {
		return "", fmt.Errorf("case %q: transpile output does not start with the expected preamble %q; got:\n%s", id, transpilePreamble, src)
	}
	return strings.TrimPrefix(src, transpilePreamble), nil
}

// transpileModule is a cached temp Go module (`replace` to gosxModuleRoot(),
// go.sum copied verbatim — the same shape readme_example_test.go uses) that
// every case's generated program builds inside. One module serves both the
// batched program (source.go's gsxSource never imports anything beyond
// "m31labs.dev/gosx", "encoding/json", and "fmt", so the dependency set —
// and therefore go.mod/go.sum — never changes per case) and each isolated,
// expected-to-fail case build.
type transpileModule struct {
	dir string
}

func setupTranspileModule(t *testing.T) *transpileModule {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "gosx-evalparity-harness")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("clear temp module dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create temp module dir: %v", err)
	}

	root := gosxModuleRoot()
	goMod := fmt.Sprintf("module example.test/gosx-evalparity-harness\n\ngo 1.26.4\n\nrequire m31labs.dev/gosx v0.0.0\nreplace m31labs.dev/gosx => %s\n", filepath.ToSlash(root))
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	goSum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatalf("read root go.sum: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), goSum, 0o644); err != nil {
		t.Fatalf("write go.sum: %v", err)
	}
	// A placeholder main.go so `go mod tidy` has a package to resolve
	// imports for. buildAndRun overwrites it before every real build.
	placeholder := "package main\n\nimport (\n\t\"encoding/json\"\n\t\"fmt\"\n\n\tgosx \"m31labs.dev/gosx\"\n)\n\nfunc main() {\n\t_ = gosx.Node{}\n\t_, _ = json.Marshal(0)\n\tfmt.Print(\"{}\")\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(placeholder), 0o644); err != nil {
		t.Fatalf("write placeholder main.go: %v", err)
	}

	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = dir
	tidyCmd.Env = append(os.Environ(), "GOWORK=off")
	var tidyOut bytes.Buffer
	tidyCmd.Stdout = &tidyOut
	tidyCmd.Stderr = &tidyOut
	if err := tidyCmd.Run(); err != nil {
		t.Fatalf("go mod tidy generated transpile module: %v\n%s", err, tidyOut.String())
	}
	return &transpileModule{dir: dir}
}

// buildAndRun overwrites the module's main.go with program, builds it, and
// (on a successful build) runs it and returns stdout. buildErr is the
// combined build output, non-empty exactly when the build failed; runErr
// is set when the build succeeded but the binary exited non-zero.
func (m *transpileModule) buildAndRun(program string) (stdout, buildErr, runErr string) {
	if err := os.WriteFile(filepath.Join(m.dir, "main.go"), []byte(program), 0o644); err != nil {
		return "", fmt.Sprintf("write main.go: %v", err), ""
	}
	binPath := filepath.Join(m.dir, "evalparity_bin")
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildCmd.Dir = m.dir
	buildCmd.Env = append(os.Environ(), "GOWORK=off")
	var buildOut bytes.Buffer
	buildCmd.Stdout = &buildOut
	buildCmd.Stderr = &buildOut
	if err := buildCmd.Run(); err != nil {
		return "", buildOut.String(), ""
	}

	runCmd := exec.Command(binPath)
	runCmd.Dir = m.dir
	var runOut, runErrBuf bytes.Buffer
	runCmd.Stdout = &runOut
	runCmd.Stderr = &runErrBuf
	if err := runCmd.Run(); err != nil {
		return "", "", runErrBuf.String()
	}
	return runOut.String(), "", ""
}

// wrapProgram assembles decls (one or more component/Props declarations,
// preamble already stripped) and calls (one `results["id"] = ...` line per
// declared component) into a complete, runnable main package that JSON-
// encodes every case's rendered HTML to stdout.
func wrapProgram(decls, calls string) string {
	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("import (\n\t\"encoding/json\"\n\t\"fmt\"\n\n\tgosx \"m31labs.dev/gosx\"\n)\n\n")
	b.WriteString(decls)
	b.WriteString("\nfunc main() {\n")
	b.WriteString("\tresults := map[string]string{}\n")
	b.WriteString(calls)
	b.WriteString("\tout, err := json.Marshal(results)\n")
	b.WriteString("\tif err != nil {\n\t\tfmt.Println(\"MARSHAL_ERROR:\", err)\n\t\treturn\n\t}\n")
	b.WriteString("\tfmt.Print(string(out))\n")
	b.WriteString("}\n")
	return b.String()
}

// caseCallLine is the `results["id"] = gosx.RenderHTML(CaseX(...))` line
// for c, sharing componentName/propsTypeName with the per-case
// gosx.Compile path (compile.go) so a symbol name can never drift between
// the transpile and route/VM backends.
//
// The call is wrapped in an immediately-invoked, recover-guarded closure:
// real Go panics on an out-of-range slice/array index at runtime (unlike
// route/VM, which both fail soft — see the "index out of range" category
// in cases_table.go), and an unrecovered panic in one case's render call
// would crash the whole generated program, taking every other batched
// case down with it. A recovered panic renders as literal text
// "PANIC: <message>", which reliably fails stripDivWrapper's "<div>...
// </div>" shape check, turning it into that one case's ordinary error
// result — never a lost batch.
func caseCallLine(c Case) string {
	call := componentName(c) + "()"
	if c.PropsFields != "" {
		call = fmt.Sprintf("%s(%s{%s})", componentName(c), propsTypeName(c), c.PropsLiteral)
	}
	return fmt.Sprintf(`	func() {
		defer func() {
			if r := recover(); r != nil {
				results[%q] = fmt.Sprintf("PANIC: %%v", r)
			}
		}()
		results[%q] = gosx.RenderHTML(%s)
	}()
`, c.ID, c.ID, call)
}

// runTranspileBatch transpiles every case and evaluates each through real
// Go: transpile.Transpile emits Go source, and a temp module (see
// transpileModule) builds and runs it with the real Go compiler.
//
// A case the table marks Unsupported[Transpile] is built in isolation, so
// a real Go compile error (e.g. `int && int`: Go's && requires bool
// operands, a restriction the GSX expression grammar itself does not
// share — see runIsolatedTranspileCase) cannot take down the shared
// build. Every other case is transpiled, then assembled into one shared
// program and built once — the fast, common path. A build failure there
// means a case that is NOT marked Unsupported[Transpile] does not actually
// compile as real Go: the harness fails loudly with the compiler's own
// message and the generated source, rather than silently skipping it, so
// the fix is always either "correct the case" or "record the new
// Unsupported[Transpile] entry" (see doc.go's triage contract) — never a
// silent gap in the support matrix.
//
// t.Skip fires only when the `go` toolchain itself is unavailable.
func runTranspileBatch(t *testing.T, cases []Case) map[string]transpileResult {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH; skipping transpile backend")
	}

	ordered := make([]Case, len(cases))
	copy(ordered, cases)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })

	module := setupTranspileModule(t)
	results := make(map[string]transpileResult, len(cases))

	var decls, calls strings.Builder
	batched := 0
	for _, c := range ordered {
		if _, expectUnsupported := c.Unsupported[Transpile]; expectUnsupported {
			results[c.ID] = runIsolatedTranspileCase(t, module, c)
			continue
		}

		out, err := transpile.Transpile(gsxSource(c), transpile.Options{SourceFile: c.ID + ".gsx"})
		if err != nil {
			// Not marked Unsupported[Transpile], but transpile itself
			// rejected it — checkBackend reports this as the case's
			// transpile failure (an "unexpected error", since the table
			// did not predict it), same as a downstream compile failure
			// would.
			results[c.ID] = transpileResult{Err: err.Error()}
			continue
		}
		body, err := stripTranspilePreamble(c.ID, out)
		if err != nil {
			t.Fatalf("runTranspileBatch: %v", err)
		}
		decls.WriteString(body)
		decls.WriteString("\n")
		calls.WriteString(caseCallLine(c))
		batched++
	}

	if batched > 0 {
		program := wrapProgram(decls.String(), calls.String())
		stdout, buildErr, runErr := module.buildAndRun(program)
		if buildErr != "" {
			t.Fatalf("go build rejected a case the table does not mark Unsupported[Transpile]; either the case is wrong or it needs an Unsupported[Transpile] entry:\n%s\n--- generated main.go ---\n%s", buildErr, program)
		}
		if runErr != "" {
			t.Fatalf("generated transpile program exited with an error:\n%s\n--- generated main.go ---\n%s", runErr, program)
		}
		var rendered map[string]string
		if err := json.Unmarshal([]byte(stdout), &rendered); err != nil {
			t.Fatalf("decode generated program output: %v\nstdout:\n%s", err, stdout)
		}
		for id, html := range rendered {
			text, err := stripDivWrapper(html)
			if err != nil {
				results[id] = transpileResult{Err: err.Error()}
				continue
			}
			results[id] = transpileResult{HTML: text}
		}
	}

	return results
}

// runIsolatedTranspileCase builds and runs c alone, so a case the table
// expects real Go to reject (Unsupported[Transpile]) — whether
// transpile.Transpile itself fails (GSX-only syntax with no Go
// equivalent, like ternary) or the Go compiler fails on syntactically
// valid-looking output it emits (like a non-bool && operand) — cannot
// fail the shared build for every other case.
func runIsolatedTranspileCase(t *testing.T, module *transpileModule, c Case) transpileResult {
	t.Helper()
	out, err := transpile.Transpile(gsxSource(c), transpile.Options{SourceFile: c.ID + ".gsx"})
	if err != nil {
		return transpileResult{Err: err.Error()}
	}
	body, err := stripTranspilePreamble(c.ID, out)
	if err != nil {
		t.Fatalf("runIsolatedTranspileCase: %v", err)
	}
	program := wrapProgram(body+"\n", caseCallLine(c))
	stdout, buildErr, runErr := module.buildAndRun(program)
	if buildErr != "" {
		return transpileResult{Err: strings.TrimSpace(buildErr)}
	}
	if runErr != "" {
		return transpileResult{Err: strings.TrimSpace(runErr)}
	}
	var rendered map[string]string
	if err := json.Unmarshal([]byte(stdout), &rendered); err != nil {
		t.Fatalf("decode isolated program output for case %q: %v\nstdout:\n%s", c.ID, err, stdout)
	}
	html, ok := rendered[c.ID]
	if !ok {
		t.Fatalf("isolated program for case %q produced no result:\n%s", c.ID, stdout)
	}
	text, err := stripDivWrapper(html)
	if err != nil {
		return transpileResult{Err: err.Error()}
	}
	return transpileResult{HTML: text}
}
