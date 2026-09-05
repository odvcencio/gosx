package format

import (
	"bytes"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/gsxcorpus"
)

func TestAuthoringCorpusFormatIdempotenceAndParseValidity(t *testing.T) {
	cases, err := gsxcorpus.Load()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, tc := range cases {
		if tc.Format == gsxcorpus.Skip {
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			src, err := tc.MainSource()
			if err != nil {
				t.Fatal(err)
			}
			formatted, err := Source(src)
			assertCorpusExpectation(t, "format", tc.Format, tc.ErrorContains("format"), err)
			if err != nil {
				return
			}
			tree, _, err := gosx.Parse(formatted)
			if err != nil {
				t.Fatalf("Parse formatted: %v", err)
			}
			if tree.RootNode().HasError() {
				t.Fatalf("formatted output has parse errors:\n%s", formatted)
			}
			again, err := Source(formatted)
			if err != nil {
				t.Fatalf("format second pass: %v", err)
			}
			if !bytes.Equal(formatted, again) {
				t.Fatalf("format is not idempotent\nfirst:\n%s\nsecond:\n%s", formatted, again)
			}
			checked++
		})
	}
	if checked == 0 {
		t.Fatal("no format corpus cases ran")
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
