package performance

import (
	"regexp"
	"testing"
	"time"
)

func TestCommittedPerformanceReceiptsMatchSchemaAndAcceptance(t *testing.T) {
	receipts, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	if err := receipts.Validate(); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(receipts.Commit) {
		t.Fatalf("measurement source commit %q is not a full Git SHA", receipts.Commit)
	}
	if age := time.Since(receipts.MeasuredAt); age < -time.Minute || age > 90*24*time.Hour {
		t.Fatalf("measurement date %s is outside the last 90 days", receipts.MeasuredAt)
	}
	wantScores := map[string]struct {
		performance   int
		accessibility int
		bestPractices int
		seo           int
		maxCLS        float64
	}{
		"/demos/":        {performance: 90, accessibility: 100, bestPractices: 100, seo: 100, maxCLS: 0.01},
		"/capabilities/": {performance: 95, accessibility: 100, maxCLS: 0.01},
		"/performance/":  {performance: 95, accessibility: 100, maxCLS: 0.01},
	}
	for _, page := range receipts.Lighthouse.Pages {
		want, ok := wantScores[page.Path]
		if !ok {
			continue
		}
		if page.Median.Performance < want.performance || page.Median.Accessibility < want.accessibility || page.Median.BestPractices < want.bestPractices || page.Median.SEO < want.seo || page.Median.CLS > want.maxCLS {
			t.Errorf("%s misses its published target: %+v, want at least performance/accessibility/best-practices/SEO %d/%d/%d/%d and CLS <= %.3f", page.Path, page.Median, want.performance, want.accessibility, want.bestPractices, want.seo, want.maxCLS)
		}
	}
}
