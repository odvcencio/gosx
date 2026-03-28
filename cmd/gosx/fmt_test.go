package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
)

func TestRunFmtFormatsSingleGSXFile(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "page.gsx", `package main

func Page() Node {
	return <main><section><h1>Hi</h1></section></main>
}
`)
	path := filepath.Join(dir, "page.gsx")

	var stderr bytes.Buffer
	count, err := RunFmt(path, &stderr)
	if err != nil {
		t.Fatalf("RunFmt(%s): %v", path, err)
	}
	if count != 1 {
		t.Fatalf("expected 1 formatted file, got %d", count)
	}

	formatted := readFile(t, path)
	if !strings.Contains(formatted, "<section>") || !strings.Contains(formatted, "<h1>Hi</h1>") {
		t.Fatalf("unexpected formatted output %q", formatted)
	}
	if strings.Contains(formatted, "<main><section>") {
		t.Fatalf("expected formatter to expand nested GSX elements, got %q", formatted)
	}
}

func TestRunFmtFormatsOnlyGSXFilesInDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "app/page.gsx", `package main

func Page() Node {
	return <div><span>Hi</span></div>
}
`)
	writeTempFile(t, dir, "app/page.server.go", `package main

func Loader() string { return "ok" }
`)

	var stderr bytes.Buffer
	count, err := RunFmt(filepath.Join(dir, "app"), &stderr)
	if err != nil {
		t.Fatalf("RunFmt(dir): %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 formatted file, got %d", count)
	}
	if !strings.Contains(stderr.String(), "gosx: formatted 1 files") {
		t.Fatalf("unexpected fmt output %q", stderr.String())
	}

	formatted := readFile(t, filepath.Join(dir, "app", "page.gsx"))
	if strings.Contains(formatted, "<div><span>") {
		t.Fatalf("expected formatted GSX output, got %q", formatted)
	}
	goFile := readFile(t, filepath.Join(dir, "app", "page.server.go"))
	if !strings.Contains(goFile, `func Loader() string { return "ok" }`) {
		t.Fatalf("expected non-GSX file to remain unchanged, got %q", goFile)
	}
}

func TestRunFmtPreservesFragmentIndentationInsideReturnStatements(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "nav.gsx", `package main

func NavLink(props any) Node {
	return <>
		<If when={props.Active}>
			<a href={props.Href}>{props.Label}</a>
		</If>
		<If when={props.Active == false}>
			<a href={props.Href}>{props.Label}</a>
		</If>
	</>
}
`)
	path := filepath.Join(dir, "nav.gsx")

	var stderr bytes.Buffer
	if _, err := RunFmt(path, &stderr); err != nil {
		t.Fatalf("RunFmt(%s): %v", path, err)
	}

	formatted := readFile(t, path)
	if strings.Contains(formatted, "return <>\n\t<If") {
		t.Fatalf("expected fragment children to stay nested under return indentation, got:\n%s", formatted)
	}
	if _, err := gosx.Compile([]byte(formatted)); err != nil {
		t.Fatalf("formatted source should compile, got %v\n%s", err, formatted)
	}
}

func TestRunFmtHelpUsage(t *testing.T) {
	var out bytes.Buffer
	fmtUsage(&out)
	usage := out.String()
	for _, snippet := range []string{
		"gosx fmt - Format GoSX source files",
		"gosx fmt <file.gsx|dir>",
		"gosx fmt --check <file.gsx|dir>",
		"gosx fmt app/layout.gsx",
	} {
		if !strings.Contains(usage, snippet) {
			t.Fatalf("expected %q in fmt usage %q", snippet, usage)
		}
	}
}

func TestRunFmtCheckReportsUnformattedGSXFile(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "page.gsx", `package main

func Page() Node {
	return <main><section><h1>Hi</h1></section></main>
}
`)
	path := filepath.Join(dir, "page.gsx")

	var stderr bytes.Buffer
	count, err := RunFmtCheck(path, &stderr)
	if err == nil {
		t.Fatal("expected check to report formatting drift")
	}
	if count != 0 {
		t.Fatalf("expected 0 verified files, got %d", count)
	}
	if !strings.Contains(err.Error(), "needs formatting") {
		t.Fatalf("expected formatting error, got %v", err)
	}
}

func TestRunFmtCheckVerifiesFormattedDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "app/page.gsx", `package main

func Page() Node {
	return <div><span>Hi</span></div>
}
`)

	var stderr bytes.Buffer
	if _, err := RunFmt(filepath.Join(dir, "app"), &stderr); err != nil {
		t.Fatalf("RunFmt(dir): %v", err)
	}

	stderr.Reset()
	count, err := RunFmtCheck(filepath.Join(dir, "app"), &stderr)
	if err != nil {
		t.Fatalf("RunFmtCheck(dir): %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 verified file, got %d", count)
	}
	if !strings.Contains(stderr.String(), "gosx: verified 1 files") {
		t.Fatalf("unexpected check output %q", stderr.String())
	}
}

func TestRunFmtCheckHandlesWrappedTextWithoutDrift(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "page.gsx", `package main

func Page() Node {
	return <article>
		<p>
			This example is a real GoSX app, not a brochure hung next to one.
					Routes, server actions, auth, client navigation, and Scene3D all live in the same
							codebase.
		</p>
	</article>
}
`)
	path := filepath.Join(dir, "page.gsx")

	var stderr bytes.Buffer
	if _, err := RunFmt(path, &stderr); err != nil {
		t.Fatalf("RunFmt(%s): %v", path, err)
	}

	stderr.Reset()
	if _, err := RunFmtCheck(path, &stderr); err != nil {
		t.Fatalf("RunFmtCheck(%s): %v", path, err)
	}

	formatted := readFile(t, path)
	if strings.Contains(formatted, "\n\t\t\t\t\t\t") {
		t.Fatalf("expected wrapped text indentation drift to be removed, got:\n%s", formatted)
	}
	if !strings.Contains(formatted, "Routes, server actions, auth, client navigation, and Scene3D all live in the same codebase.") {
		t.Fatalf("expected wrapped text to normalize to a single logical line, got:\n%s", formatted)
	}
}

func TestRunFmtCheckLeavesRawStringCodeExamplesStable(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "page.gsx", "package main\n\nfunc Page() Node {\n\treturn <article>\n\t\t{DocsCodeBlock(\"gosx\", `func Demo() Node {\n\t\t    return <Scene3D>\n\t\t        <div class=\"fallback\">Ready</div>\n\t\t    </Scene3D>\n\t\t}`)}\n\t</article>\n}\n")
	path := filepath.Join(dir, "page.gsx")

	var stderr bytes.Buffer
	if _, err := RunFmt(path, &stderr); err != nil {
		t.Fatalf("RunFmt(%s): %v", path, err)
	}

	stderr.Reset()
	if _, err := RunFmtCheck(path, &stderr); err != nil {
		t.Fatalf("RunFmtCheck(%s): %v", path, err)
	}

	formatted := readFile(t, path)
	if strings.Count(formatted, "    return <Scene3D>") != 1 {
		t.Fatalf("expected raw string example indentation to stay stable, got:\n%s", formatted)
	}
}
