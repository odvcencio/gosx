package dev

import (
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/island/program"
)

func TestHotSwapResolvesSiblingMapType(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "page.gsx")
	source := "package app\ntype Props struct { M map[string]bool }\n//gosx:island\nfunc Sample(props Props) Node {\nreturn <div>{props.M[\"missing\"]}</div>\n}\n"
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "types.go"), []byte("package app\ntype bool string\n"), 0600); err != nil {
		t.Fatal(err)
	}
	programs, err := compileIslandPrograms(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(programs) != 1 {
		t.Fatalf("programs = %d, want 1", len(programs))
	}
	compiled, err := program.DecodeJSON(programs[0].ProgramJSON)
	if err != nil {
		t.Fatal(err)
	}
	for _, expr := range compiled.Exprs {
		if expr.Value == program.IndexTypedMap {
			if expr.Type != program.TypeString {
				t.Fatalf("map type %v, want string", expr.Type)
			}
			return
		}
	}
	t.Fatal("typed map index missing")
}
