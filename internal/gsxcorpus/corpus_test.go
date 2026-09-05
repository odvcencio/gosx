package gsxcorpus

import (
	"strings"
	"testing"
)

func TestStrictCheckGoModQuotesReplacePathWithSpaces(t *testing.T) {
	got, err := strictCheckGoMod([]byte("module m31labs.dev/gosx\n\ngo 1.26\n"), `/tmp/path with spaces/gosx`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `replace m31labs.dev/gosx => "/tmp/path with spaces/gosx"`) {
		t.Fatalf("replace path was not quoted for spaces:\n%s", got)
	}
}

func TestStrictCheckGoModRewritesCRLFModuleDirective(t *testing.T) {
	got, err := strictCheckGoMod([]byte("module m31labs.dev/gosx\r\n\r\ngo 1.26\r\nrequire example.test/dep v0.0.0\r\n"), `/tmp/gosx`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "module ") != 1 {
		t.Fatalf("generated go.mod has duplicate module directives:\n%s", got)
	}
	if !strings.HasPrefix(got, "module example.test/corpus\n\n") {
		t.Fatalf("generated go.mod did not replace module directive:\n%s", got)
	}
	if !strings.Contains(got, "go 1.26") || !strings.Contains(got, "require example.test/dep v0.0.0") {
		t.Fatalf("generated go.mod did not preserve dependency tail:\n%s", got)
	}
}
