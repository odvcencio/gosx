package capability

import (
	"os"
	"strings"
	"testing"
)

// TestEveryMatrixRowHasCorroboration keeps a new Matrix row from shipping
// without a test that reads renderer source before asserting the cell. See
// corroborationIndex's doc comment for the drift this guards against.
func TestEveryMatrixRowHasCorroboration(t *testing.T) {
	for feature := range Matrix {
		entry, ok := corroborationIndex[feature]
		if !ok {
			t.Errorf("Matrix row %q has no corroborationIndex entry; add one naming the test file "+
				"that reads renderer source for it before this cell can be trusted", feature)
			continue
		}
		data, err := os.ReadFile(entry.File)
		if err != nil {
			t.Errorf("corroborationIndex[%q] names %q, which does not exist: %v", feature, entry.File, err)
			continue
		}
		if !strings.Contains(string(data), entry.Identifier) {
			t.Errorf("corroborationIndex[%q] names %q, but that file does not mention %q; "+
				"a corroboration test must call evidenceFor(t, %s, ...)",
				feature, entry.File, entry.Identifier, entry.Identifier)
		}
	}
}

// TestCorroborationIndexNamesNoMissingRow keeps the index from accumulating
// dead entries for a feature that has since left Matrix.
func TestCorroborationIndexNamesNoMissingRow(t *testing.T) {
	for feature := range corroborationIndex {
		if _, ok := Matrix[feature]; !ok {
			t.Errorf("corroborationIndex names %q, which has no Matrix row; remove the dead entry", feature)
		}
	}
}
