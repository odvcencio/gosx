// Package examples holds the guard that the standalone examples teach the
// component syntax the README recommends.
package examples

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
)

// legacyPropsDeclaration matches `func Name(props ...) Node`, which the README
// marks deprecated. `func Page() Node` and `func Layout() Node` take no props
// and stay: a layout needs them because a strict component cannot render
// <Slot />, and a page reads its loader result as `data`.
var legacyPropsDeclaration = regexp.MustCompile(`(?m)^func\s+([A-Z]\w*)\(\s*props\b[^)]*\)\s*Node\b`)

// legacyAllowed lists the declarations that cannot move to the strict form yet.
// Each needs a reason; remove the entry when the reason goes away.
var legacyAllowed = map[string]string{
	"expressions/expressions.gsx:Expressions": "a strict island cannot yet express closure methods on a slice prop (filter, map, find)",
	"synced-video/player.gsx:SyncedPlayer":    "a .gsx //gosx:engine component has no strict form yet (README, legacy component syntax)",
	"turbofan/app/page.gsx:TurbofanControls":  "a legacy Page cannot call a strict component with named attributes",
}

// skippedExamples are not standalone examples: gosx-docs is the docs site and
// ouroboros-corpus is a test corpus. Other work owns both.
var skippedExamples = map[string]bool{"gosx-docs": true, "ouroboros-corpus": true}

func TestExamplesUseStrictComponentSyntax(t *testing.T) {
	checked := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && skippedExamples[d.Name()] && filepath.Dir(path) == "." {
				return filepath.SkipDir
			}
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".gsx" {
			return nil
		}
		checked++
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(path)
		for _, m := range legacyPropsDeclaration.FindAllStringSubmatch(string(source), -1) {
			if _, ok := legacyAllowed[slash+":"+m[1]]; !ok {
				t.Errorf("%s declares legacy props component %s; use component %s(props: Type)", slash, m[1], m[1])
			}
		}
		prog, err := gosx.Compile(source)
		if err != nil {
			t.Errorf("compile %s: %v", slash, err)
			return nil
		}
		for _, diag := range ir.ValidateWarnings(prog) {
			if strings.Contains(diag.Message, "untyped legacy") {
				t.Errorf("%s: %s", slash, diag.Message)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("found no .gsx files")
	}
}
