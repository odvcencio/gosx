package route

import (
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/island/program"
)

func TestIslandMapTypesFollowRenderPackageNotSharedSourceCache(t *testing.T) {
	source := []byte("package app\ntype Props struct { M map[string]bool }\n//gosx:island\nfunc Sample(props Props) Node {\nreturn <div>{props.M[\"missing\"]}</div>\n}\n")
	var cached any
	for _, tc := range []struct {
		name, sibling string
		want          program.ExprType
	}{{"builtin", "", program.TypeBool}, {"shadow", "type bool string", program.TypeString}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "page.gsx")
			if err := os.WriteFile(file, source, 0600); err != nil {
				t.Fatal(err)
			}
			if tc.sibling != "" {
				if err := os.WriteFile(filepath.Join(dir, "types.go"), []byte("package app\n"+tc.sibling+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p, err := loadCachedGSXProgram(file)
			if err != nil {
				t.Fatal(err)
			}
			if cached == nil {
				cached = p
			} else if cached != p {
				t.Fatal("fixture did not exercise shared content cache")
			}
			island, err := newFileProgramRenderer(p, fileRenderOptions{SourceFile: file}).islandProgram("Sample")
			if err != nil {
				t.Fatal(err)
			}
			for _, expr := range island.Exprs {
				if expr.Value == program.IndexTypedMap {
					if expr.Type != tc.want {
						t.Fatalf("map type %v, want %v", expr.Type, tc.want)
					}
					return
				}
			}
			t.Fatal("typed map index missing")
		})
	}
}
