package route

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx"
)

// TestRepositoryCorpusRendersDeterministically is the corpus-level proof for
// gosx#188: it compiles every .gsx file under the repository root (72 at the
// time of the fix, most of them under examples/gosx-docs) and renders every
// component each one declares twice, byte-comparing the two renders.
//
// Before the fix, 12 of the rendered components (all docs pages, the same
// class of file gosx#188 quoted) differed between the two renders because
// defaultRenderedComponent iterated its attrs map in Go's randomized order.
// After the fix, none differ. A file this test cannot compile, or a
// component that errors on render (both expected for some of the corpus —
// layout and error-boundary files are not meant to stand alone), is skipped
// rather than failed: this test is about output determinism, not full
// pipeline coverage.
func TestRepositoryCorpusRendersDeterministically(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(wd) // route/ -> repository root

	var files []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.Name() == "node_modules" {
			return filepath.SkipDir
		}
		if !info.IsDir() && filepath.Ext(path) == ".gsx" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no .gsx files found under repository root; skipping corpus determinism check")
	}

	rendered := 0
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		prog, err := gosx.Compile(src)
		if err != nil {
			continue // not every .gsx file in the corpus is a standalone page
		}
		for _, comp := range prog.Components {
			first, err := RenderProgramComponent(prog, comp.Name, ProgramRenderEnv{})
			if err != nil {
				continue // component needs props/env this bare render does not supply
			}
			rendered++
			second, err := RenderProgramComponent(prog, comp.Name, ProgramRenderEnv{})
			if err != nil {
				t.Fatalf("%s :: %s: second render errored after the first succeeded: %v", f, comp.Name, err)
			}
			if sha256.Sum256([]byte(first)) != sha256.Sum256([]byte(second)) {
				t.Errorf("%s :: %s renders nondeterministically:\n render 1: %s\n render 2: %s", f, comp.Name, first, second)
			}
		}
	}
	if rendered == 0 {
		t.Skip("no component in the corpus rendered with a bare env; skipping corpus determinism check")
	}
	t.Logf("rendered %d components from %d .gsx files with matching bytes across two renders", rendered, len(files))
}
