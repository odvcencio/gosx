package wire

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
	"golang.org/x/net/html"
)

func overNodeLimitScript() string {
	// 2450 declarations with 102 named nodes each, then 50 two-node
	// expressions and the program root: exactly 250001 named nodes.
	// Small blocks avoid a pathological flat statement-list parse.
	return strings.Repeat("function f(){"+strings.Repeat("x;", 49)+"}\n", 2450) + strings.Repeat("x;", 50)
}

func assertAnalysisLimit(t *testing.T, set ReferenceSet, err error, bound string, limit int) {
	t.Helper()
	if err != nil || set.Complete {
		t.Fatalf("analysis limit must retain incomplete coverage without a hard error: %+v %v", set, err)
	}
	for _, drop := range set.Drops {
		if drop.Reason == "analysis-limit" && drop.Bound == bound && drop.Limit == int64(limit) {
			return
		}
	}
	t.Fatalf("missing typed analysis-limit for %s=%d: %+v", bound, limit, set.Drops)
}

func TestReferencesAnalysisLimitsAreIncomplete(t *testing.T) {
	cases := []struct{ name, kind, body string }{
		{"input-bytes", KindScript, strings.Repeat(" ", (16<<20)+1)},
		{"ast-nodes", KindScript, overNodeLimitScript()},
		{"ast-depth", KindScript, strings.Repeat("(", 257) + "x" + strings.Repeat(")", 257) + ";"},
		{"html-depth", KindDocument, strings.Repeat("<div>", 257) + "x" + strings.Repeat("</div>", 257)},
		{"document-count", KindDocument, strings.Repeat(`<iframe srcdoc=""></iframe>`, 4096)},
	}
	body := `<script>fetch("/hidden.json")</script>`
	for i := 0; i < 33; i++ {
		body = `<iframe srcdoc="` + html.EscapeString(body) + `"></iframe>`
	}
	cases = append(cases, struct{ name, kind, body string }{"srcdoc-depth", KindDocument, body})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set, err := ScanReferences([]byte(tc.body), tc.kind)
			limit := map[string]int{"input-bytes": 16 << 20, "ast-nodes": 250000, "ast-depth": 256, "html-depth": 256, "document-count": 4096, "srcdoc-depth": 32}[tc.name]
			assertAnalysisLimit(t, set, err, tc.name, limit)
		})
	}
}

func TestReferencesFormattedByteLimit(t *testing.T) {
	// This is the formatter output guard, not a second input-body allowance.
	// The oversized generated script must never reach the parser.
	body := []byte(strings.Repeat("x", (32<<20)+1))
	out := referenceScanner{ReferenceSet: ReferenceSet{Complete: true}}
	err := validateFormattedReferences(api.TransformResult{Code: body})
	set, err := referenceResult(&out, err)
	assertAnalysisLimit(t, set, err, "formatted-bytes", 32<<20)
}

func TestReferencesConstructedSrcdocLimits(t *testing.T) {
	for _, tc := range []struct {
		bound               string
		depth, count, bytes int
		body                string
		limit               int
	}{
		{"srcdoc-depth", 32, 0, 0, "", 32},
		{"srcdoc-count", 0, 4096, 0, "", 4096},
		{"srcdoc-bytes", 0, 0, 16 << 20, "x", 16 << 20},
	} {
		t.Run(tc.bound, func(t *testing.T) {
			out := referenceScanner{ReferenceSet: ReferenceSet{Complete: true}, srcdocDepth: tc.depth, srcdocCount: tc.count, srcdocBytes: tc.bytes}
			err := scanSrcdocReferences(&html.Node{Type: html.ElementNode, Data: "iframe"}, tc.body, &out)
			set, err := referenceResult(&out, err)
			assertAnalysisLimit(t, set, err, tc.bound, tc.limit)
		})
	}
}

func TestReferencesParserLimitReceipts(t *testing.T) {
	// Each receipt is the parser's own stopping contract. Limits are retained,
	// and malformed or invariant-failure receipts are not relabelled as bounds.
	for _, tc := range []struct {
		reason ts.ParseStopReason
		bound  string
	}{
		{ts.ParseStopIterationLimit, "parser-iterations"},
		{ts.ParseStopStackDepthLimit, "parser-stack-depth"},
		{ts.ParseStopNodeLimit, "parser-nodes"},
		{ts.ParseStopMemoryBudget, "parser-memory-bytes"},
		{ts.ParseStopTimeout, "parser-time-micros"},
	} {
		t.Run(tc.bound, func(t *testing.T) {
			out := referenceScanner{ReferenceSet: ReferenceSet{Complete: true}}
			runtime := ts.ParseRuntime{IterationLimit: 7, StackDepthLimit: 7, NodeLimit: 7, MemoryBudgetBytes: 7}
			set, err := referenceResult(&out, referenceParseLimit(tc.reason, runtime, 7))
			assertAnalysisLimit(t, set, err, tc.bound, 7)
		})
	}
	for _, reason := range []ts.ParseStopReason{ts.ParseStopAccepted, ts.ParseStopNoStacksAlive, ts.ParseStopInvariantViolation} {
		if referenceParseLimit(reason, ts.ParseRuntime{}, 0) != nil {
			t.Fatalf("non-limit receipt %s was treated as an analysis limit", reason)
		}
	}
}

func TestReferencesParserTimeoutIsIncomplete(t *testing.T) {
	parser := ts.NewParser(grammars.JavascriptLanguage())
	parser.SetTimeoutMicros(1)
	// Generated valid input exceeds the configured parser time budget, without
	// changing the production parser's default timeout or allocation ceilings.
	tree, err := parseReferenceSyntax(parser, []byte(overNodeLimitScript()))
	if tree != nil {
		tree.Release()
		t.Fatal("parser timed out without discarding its partial tree")
	}
	out := referenceScanner{ReferenceSet: ReferenceSet{Complete: true}}
	set, err := referenceResult(&out, err)
	assertAnalysisLimit(t, set, err, "parser-time-micros", 1)
}
