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
		prog, err := gosx.Compile(source)
		if err != nil {
			t.Fatalf("compile %s: %v", path, err)
		}
		// Any file a route loader will bind (layout.gsx / page.gsx) must
		// produce at least one component. A bare fragment compiles but
		// silently 500s at prerender with "no components found".
		base := filepath.Base(path)
		if base == "layout.gsx" || base == "page.gsx" {
			if len(prog.Components) == 0 {
				rel, _ := filepath.Rel(root, path)
				t.Fatalf("%s has no components (bare-fragment form breaks route resolution)", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
