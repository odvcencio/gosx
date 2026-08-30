//go:build !tinygo

package gosx

import (
	"os"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
)

// TestShippedGSXCorpusHasNoUntypedLegacyPropsDeclarations keeps executable
// templates and examples free of untyped legacy props declarations. It does
// not reject zero-prop legacy route entries. The test inspects lowered
// declarations instead of source text because the docs intentionally render
// legacy `props any` snippets as migration guidance.
func TestShippedGSXCorpusHasNoUntypedLegacyPropsDeclarations(t *testing.T) {
	files := gsxCorpusFiles(t)
	if len(files) == 0 {
		t.Fatal("no shipped .gsx files found")
	}

	var residue []string
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		prog, err := Compile(source)
		if err != nil {
			t.Fatalf("compile %s: %v", path, err)
		}
		for _, component := range prog.Components {
			if component.Syntax == ir.ComponentSyntaxLegacy && strings.TrimSpace(component.PropsType) == "any" {
				residue = append(residue, path+":"+component.Name)
			}
		}
	}

	if len(residue) > 0 {
		sort.Strings(residue)
		t.Fatalf("shipped .gsx contains untyped legacy props declarations:\n%s", strings.Join(residue, "\n"))
	}
}
