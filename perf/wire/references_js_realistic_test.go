package wire

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Checked-in output from esbuild's pinned Go API (transform and ESM bundle)
// and Terser 5.44.1 (ecma:2020, compress:true, mangle:true). Sources and Terser
// options are retained in the corpus so the outputs can be reproduced without
// adding Node/minifier dependencies to the Go test suite.
func TestReferencesRealisticJavaScriptStaysComplete(t *testing.T) {
	var fixtures []struct {
		Name, Producer, Source, Code string
		References                   []Reference
		Origin                       *struct{ Path, Declaration string }
		Replacement                  *struct{ Old, New string }
	}
	raw, err := os.ReadFile("testdata/javascript-complete.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	complete := 0
	for _, fixture := range fixtures {
		t.Run(fixture.Producer+"/"+fixture.Name, func(t *testing.T) {
			if fixture.Origin != nil {
				raw, err := os.ReadFile(filepath.Join("../..", fixture.Origin.Path))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(raw), fixture.Origin.Declaration) {
					t.Fatal("bootstrap fixture drifted from production; refresh the corpus")
				}
				want := fixture.Origin.Declaration
				if fixture.Replacement != nil {
					if strings.Count(want, fixture.Replacement.Old) != 1 {
						t.Fatal("ambiguous literal-target specialization")
					}
					want = strings.Replace(want, fixture.Replacement.Old, fixture.Replacement.New, 1)
				}
				if fixture.Source != want {
					t.Fatal("unrecorded bootstrap specialization")
				}
			}
			set, err := ScanReferences([]byte(fixture.Code), KindScript)
			if err != nil || !set.Complete || !reflect.DeepEqual(set.Resources, fixture.References) {
				t.Fatalf("realistic code lost complete/exact coverage: %+v err=%v want=%+v code=%s", set, err, fixture.References, fixture.Code)
			}
			complete++
			// The same minified bytes also exercise the HTML module route.
			set, err = ScanReferences([]byte(`<script type="module">`+fixture.Code+`</script>`), KindDocument)
			if err != nil || !set.Complete || !reflect.DeepEqual(set.Resources, fixture.References) {
				t.Fatalf("HTML lost realistic script coverage: %+v err=%v", set, err)
			}
		})
	}
	t.Logf("realistic corpus: %d/%d scripts complete with exact references", complete, len(fixtures))
	if len(fixtures) != 36 || complete != len(fixtures) {
		t.Fatalf("realistic corpus result: %d/%d", complete, len(fixtures))
	}
}
