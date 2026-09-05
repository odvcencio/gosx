package transpile

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/gsxcorpus"
)

func TestAuthoringCorpusTranspileSyntaxOnlyMatrix(t *testing.T) {
	cases, err := gsxcorpus.Load()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, tc := range cases {
		if tc.Transpile == gsxcorpus.Skip {
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			src, err := tc.MainSource()
			if err != nil {
				t.Fatal(err)
			}
			out, err := Transpile(src, Options{SourceFile: tc.Main})
			assertCorpusExpectation(t, "transpile", tc.Transpile, tc.ErrorContains("transpile"), err)
			if err != nil {
				return
			}
			again, err := Transpile(src, Options{SourceFile: tc.Main})
			if err != nil {
				t.Fatalf("transpile second pass: %v", err)
			}
			if out != again {
				t.Fatalf("transpile output is not deterministic\nfirst:\n%s\nsecond:\n%s", out, again)
			}
			if _, err := parser.ParseFile(token.NewFileSet(), tc.Main+".go", out, parser.AllErrors); err != nil {
				t.Fatalf("generated Go syntax parse failed: %v\n%s", err, out)
			}
			checked++
		})
	}
	if checked == 0 {
		t.Fatal("no transpile corpus cases ran")
	}
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
