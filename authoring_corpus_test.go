package gosx

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/gsxcorpus"
)

func TestAuthoringCorpusParseAndCompileMatrix(t *testing.T) {
	cases, err := gsxcorpus.Load()
	if err != nil {
		t.Fatal(err)
	}
	seenParse, seenCompile := 0, 0
	for _, tc := range cases {
		src, err := tc.MainSource()
		if err != nil {
			t.Fatal(err)
		}
		t.Run(tc.Name+"/parse", func(t *testing.T) {
			tree, _, err := Parse(src)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			hasError := tree.RootNode().HasError()
			if tc.Parse == gsxcorpus.Pass && hasError {
				t.Fatal("Parse produced an error tree")
			}
			if tc.Parse == gsxcorpus.Reject && !hasError {
				t.Fatal("Parse produced a clean tree, want error tree")
			}
			seenParse++
		})
		t.Run(tc.Name+"/compile", func(t *testing.T) {
			_, err := Compile(src)
			assertCorpusExpectation(t, "compile", tc.Compile, tc.ErrorContains("compile"), err)
			if tc.Compile != gsxcorpus.Skip {
				seenCompile++
			}
		})
	}
	if seenParse != len(cases) || seenCompile == 0 {
		t.Fatalf("vacuous corpus coverage: parse=%d compile=%d cases=%d", seenParse, seenCompile, len(cases))
	}
}

func TestAuthoringCorpusManifestMatrix(t *testing.T) {
	cases, err := gsxcorpus.Load()
	if err != nil {
		t.Fatal(err)
	}
	totals := map[gsxcorpus.Expectation]int{}
	targets := map[string]int{}
	for _, tc := range cases {
		for target, status := range tc.TargetStatuses() {
			totals[status]++
			targets[target]++
			if status == gsxcorpus.Skip && tc.NotApplicable[target] == "" {
				t.Fatalf("%s/%s lacks not-applicable reason", tc.Name, target)
			}
		}
	}
	for _, target := range []string{"parse", "compile", "transpile", "format", "lspAnalyze", "lspFormat", "strictCheck", "route", "irIsland"} {
		if targets[target] != len(cases) {
			t.Fatalf("target %s has %d statuses, want %d", target, targets[target], len(cases))
		}
	}
	t.Logf("authoring corpus matrix: cases=%d executed-pass=%d executed-reject=%d not-applicable=%d", len(cases), totals[gsxcorpus.Pass], totals[gsxcorpus.Reject], totals[gsxcorpus.Skip])
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
