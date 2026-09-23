package evalparity

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateMatrix = flag.Bool("update", false, "regenerate docs/expression-support-matrix.md instead of checking it")

func matrixDocPath() string {
	return filepath.Join(gosxModuleRoot(), "docs", "expression-support-matrix.md")
}

// TestSupportMatrixUpToDate fails if docs/expression-support-matrix.md
// does not match what GenerateMatrix(cases) produces right now — the
// generated-artifact guard the task calls for: the matrix is derived from
// the same Case table TestExpressionParity checks, so the two can never
// silently drift apart. Run with -update to regenerate the file:
//
//	go test ./internal/evalparity/... -run TestSupportMatrixUpToDate -update
func TestSupportMatrixUpToDate(t *testing.T) {
	if err := validateCases(cases); err != nil {
		t.Fatalf("cases table is inconsistent: %v", err)
	}
	want := GenerateMatrix(cases)
	path := matrixDocPath()

	if *updateMatrix {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("regenerated %s", path)
		return
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run with -update to generate it)", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s is out of date; regenerate it with:\n\tgo test ./internal/evalparity/... -run TestSupportMatrixUpToDate -update", path)
	}
}
