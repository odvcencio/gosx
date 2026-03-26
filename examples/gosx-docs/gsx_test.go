package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/odvcencio/gosx"
)

func TestDocsGSXFilesCompile(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "app")

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".gsx" {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := gosx.Compile(source); err != nil {
			t.Fatalf("compile %s: %v", path, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
