package lint_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"m31labs.dev/gosx/det/lint"
)

func TestHazards(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), lint.Analyzer, "hazards")
}

func TestOptOutReportsNothing(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), lint.Analyzer, "optout")
}

func TestSuggestedFix(t *testing.T) {
	analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), lint.Analyzer, "fixme")
}
