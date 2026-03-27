package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
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

func TestRunFmtHelpUsage(t *testing.T) {
	var out bytes.Buffer
	fmtUsage(&out)
	usage := out.String()
	for _, snippet := range []string{
		"gosx fmt - Format GoSX source files",
		"gosx fmt <file.gsx|dir>",
		"gosx fmt app/layout.gsx",
	} {
		if !strings.Contains(usage, snippet) {
			t.Fatalf("expected %q in fmt usage %q", snippet, usage)
		}
	}
}
