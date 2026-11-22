package main

import (
	"go/format"
	"testing"
)

// A project's modules/modules.go is a tracked file, so what the generator
// writes must already be gofmt output: a repository that checks formatting
// must not have to skip it.
func TestRenderModulesPackageIsFormatted(t *testing.T) {
	for name, imports := range map[string][]string{
		"no imports":  nil,
		"two imports": {"example.com/app", "example.com/app/stack"},
	} {
		got := renderModulesPackage(imports)
		want, err := format.Source([]byte(got))
		if err != nil {
			t.Fatalf("%s: generated source does not parse: %v\n%s", name, err, got)
		}
		if got != string(want) {
			t.Errorf("%s: generated source is not gofmt output:\n got %q\nwant %q", name, got, want)
		}
	}
}
