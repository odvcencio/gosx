package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyDirIfPresentSkipsGoTestFiles(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	files := map[string]string{
		"page.gsx":                  "package app\n",
		"page.go":                   "package app\n",
		"page_test.go":              "package app\n",
		"nested/inner.go":           "package nested\n",
		"nested/inner_test.go":      "package nested\n",
		"nested/fixtures_test.gsx":  "package nested\n",
		"testdata/golden.html":      "<html></html>\n",
	}
	for name, body := range files {
		path := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := copyDirIfPresent(src, dst); err != nil {
		t.Fatalf("copyDirIfPresent: %v", err)
	}

	for _, want := range []string{"page.gsx", "page.go", "nested/inner.go", "testdata/golden.html"} {
		if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(want))); err != nil {
			t.Fatalf("expected %s to be copied: %v", want, err)
		}
	}
	for _, skip := range []string{"page_test.go", "nested/inner_test.go"} {
		if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(skip))); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be skipped in bundle output, stat err=%v", skip, err)
		}
	}
}
