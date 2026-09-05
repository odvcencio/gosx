package ir_test

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/gsxcorpus"
	"m31labs.dev/gosx/ir"
)

func TestAuthoringCorpusIslandMatrix(t *testing.T) {
	cases, err := gsxcorpus.Load()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, tc := range cases {
		if tc.IRIsland == gsxcorpus.Skip {
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			src, err := tc.MainSource()
			if err != nil {
				t.Fatal(err)
			}
			tree, lang, err := gosx.Parse(src)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			prog, err := ir.Lower(tree.RootNode(), src, lang)
			if err != nil {
				t.Fatalf("Lower: %v", err)
			}
			idx := -1
			for i, comp := range prog.Components {
				if comp.Name == tc.IslandTarget {
					idx = i
					break
				}
			}
			if idx < 0 {
				t.Fatalf("island target %q not found", tc.IslandTarget)
			}
			if tc.IRIsland == gsxcorpus.Reject {
				if diags := ir.Validate(prog); !diagnosticsContain(diags, tc.ErrorContains("irIsland")) {
					t.Fatalf("Validate diagnostics = %#v, want substring %q", diags, tc.ErrorContains("irIsland"))
				}
			}
			_, err = ir.LowerIsland(prog, idx)
			assertCorpusExpectation(t, "irIsland", tc.IRIsland, tc.ErrorContains("irIsland"), err)
			checked++
		})
	}
	if checked == 0 {
		t.Fatal("no island corpus cases ran")
	}
}

func diagnosticsContain(diags []ir.Diagnostic, needle string) bool {
	if needle == "" {
		return len(diags) > 0
	}
	needle = strings.ToLower(needle)
	for _, diag := range diags {
		if strings.Contains(strings.ToLower(diag.Message), needle) {
			return true
		}
	}
	return false
}

func assertCorpusExpectation(t *testing.T, target string, want gsxcorpus.Expectation, contains string, err error) {
	t.Helper()
	switch want {
	case gsxcorpus.Pass:
		if err != nil {
			t.Fatalf("%s error = %v, want pass", target, err)
		}
	case gsxcorpus.Reject:
		if err == nil {
			t.Fatalf("%s succeeded, want reject", target)
		}
		if contains != "" && !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(contains)) {
			t.Fatalf("%s error = %v, want substring %q", target, err, contains)
		}
	case gsxcorpus.Skip:
	default:
		t.Fatalf("unknown expectation %q for %s", want, target)
	}
}
