package lsp

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/gsxcorpus"
)

func TestAuthoringCorpusLSPAnalyzeAndFormatMatrix(t *testing.T) {
	cases, err := gsxcorpus.Load()
	if err != nil {
		t.Fatal(err)
	}
	checkedAnalyze, checkedFormat := 0, 0
	for _, tc := range cases {
		src, err := tc.MainSource()
		if err != nil {
			t.Fatal(err)
		}
		if tc.LSPAnalyze != gsxcorpus.Skip {
			t.Run(tc.Name+"/analyze", func(t *testing.T) {
				diags := Analyze(tc.Main, src)
				err := diagnosticsError(diags)
				assertCorpusExpectation(t, "lspAnalyze", tc.LSPAnalyze, tc.ErrorContains("lspAnalyze"), err)
				checkedAnalyze++
			})
		}
		if tc.LSPFormat != gsxcorpus.Skip {
			t.Run(tc.Name+"/format", func(t *testing.T) {
				_, err := FormatSource(src)
				assertCorpusExpectation(t, "lspFormat", tc.LSPFormat, tc.ErrorContains("lspFormat"), err)
				checkedFormat++
			})
		}
	}
	if checkedAnalyze == 0 || checkedFormat == 0 {
		t.Fatalf("vacuous LSP corpus coverage: analyze=%d format=%d", checkedAnalyze, checkedFormat)
	}
}

type diagnosticListError []Diagnostic

func (e diagnosticListError) Error() string {
	var parts []string
	for _, d := range e {
		parts = append(parts, d.Message)
	}
	return strings.Join(parts, "\n")
}

func diagnosticsError(diags []Diagnostic) error {
	if len(diags) == 0 {
		return nil
	}
	return diagnosticListError(diags)
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
