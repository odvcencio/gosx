package claimcheck

import (
	"sort"
	"strings"
	"testing"
)

// The fixture corpus under testdata/corpus carries one marker per case, and
// every marker ends with a unique "case-..." tag. The tests below address a
// marker by that tag rather than by line number, so adding a case never
// renumbers the table.
//
// The walk skips any directory named testdata, so the deliberately false
// markers in the corpus never reach TestRepoClaimsHold.
const corpusRoot = "testdata/corpus"

// tagOf returns the case tag at the end of a marker line.
func tagOf(text string) string {
	for _, field := range strings.Fields(text) {
		if strings.HasPrefix(field, "case-") {
			return field
		}
	}
	return ""
}

// wantOK are the corpus markers that must parse and hold.
var wantOK = []string{
	"case-ok-has",
	"case-ok-lacks",
	"case-ok-count",
	"case-ok-subdir",
	"case-ok-js",
}

// wantFalse are the corpus markers that must parse and fail, with a phrase the
// failure message must carry. The phrase is what tells a reader which way the
// claim is wrong.
var wantFalse = map[string]string{
	"case-false-has":            "does not",
	"case-false-lacks":          "does NOT contain",
	"case-false-count":          "exactly 1 time(s), and it contains it 2 time(s)",
	"case-false-missing-target": "cannot be read",
	"case-false-js":             "does not",
}

// wantMalformed are the corpus markers that must not parse, with a phrase the
// reason must carry.
var wantMalformed = map[string]string{
	"case-bad-no-fields":            "does not parse",
	"case-bad-unknown-verb":         `unknown verb "contains"`,
	"case-bad-count-without-number": "needs a number",
	"case-bad-has-with-count":       "takes no count",
	"case-bad-unclosed-backtick":    "does not parse",
	"case-bad-empty-pattern":        "empty",
	"case-bad-uncompilable-regexp":  "does not compile",
	"case-bad-absolute-target":      "absolute",
	"case-bad-dot-relative-target":  "relative to the marker",
	"case-bad-self-target":          "the file that carries the marker",
}

// wantSkipped are markers the walk must never reach. Each sits in a directory
// or a file the scan excludes on purpose.
var wantSkipped = []string{
	"case-skipped-vendor",
	"case-skipped-bundle",
}

// TestCorpusSplitsTrueFalseAndMalformed is the meta test. It proves that the
// checker separates three outcomes that a weaker checker confuses:
//
//   - a claim that holds,
//   - a claim that parses and is false,
//   - a line that means to be a marker and does not parse.
//
// Break the parser and the malformed rows fail. Break Verify and the false rows
// fail. Break the walk and the counts fail. So the meta test notices a change to
// any part of the mechanism, not only to the claims.
func TestCorpusSplitsTrueFalseAndMalformed(t *testing.T) {
	claims, malformed, err := Scan(corpusRoot)
	if err != nil {
		t.Fatalf("scan %s: %v", corpusRoot, err)
	}

	gotMalformed := map[string]string{}
	for _, m := range malformed {
		tag := tagOf(m.Text)
		if tag == "" {
			t.Errorf("%s: malformed marker carries no case tag: %s", m.Where(), m.Text)
			continue
		}
		gotMalformed[tag] = m.Reason
	}
	assertSameTags(t, "malformed", keysOf(wantMalformed), keysOf(gotMalformed))
	for tag, phrase := range wantMalformed {
		reason, ok := gotMalformed[tag]
		if !ok {
			continue // assertSameTags already reported it
		}
		if !strings.Contains(reason, phrase) {
			t.Errorf("%s: reason %q does not mention %q", tag, reason, phrase)
		}
	}

	results := VerifyAll(corpusRoot, claims)
	if len(results) != len(claims) {
		t.Fatalf("VerifyAll returned %d results for %d claims; every claim must be evaluated", len(results), len(claims))
	}

	gotOK := map[string]bool{}
	gotFalse := map[string]string{}
	for _, r := range results {
		tag := tagOf(r.Claim.Text)
		if tag == "" {
			t.Errorf("%s: claim carries no case tag: %s", r.Claim.Where(), r.Claim.Text)
			continue
		}
		if r.OK {
			gotOK[tag] = true
			continue
		}
		gotFalse[tag] = r.Problem
	}

	assertSameTags(t, "holding", wantOK, keysOfBool(gotOK))
	assertSameTags(t, "false", keysOf(wantFalse), keysOf(gotFalse))
	for tag, phrase := range wantFalse {
		problem, ok := gotFalse[tag]
		if !ok {
			continue
		}
		if !strings.Contains(problem, phrase) {
			t.Errorf("%s: problem %q does not mention %q", tag, problem, phrase)
		}
	}

	// A failure must name the marker site and the target, or a reader cannot act
	// on it.
	for _, r := range results {
		if r.OK {
			continue
		}
		for _, want := range []string{r.Claim.Where(), r.Claim.Target} {
			if !strings.Contains(r.Problem, want) {
				t.Errorf("%s: problem does not name %q:\n%s", tagOf(r.Claim.Text), want, r.Problem)
			}
		}
	}

	for _, tag := range wantSkipped {
		if gotOK[tag] || gotFalse[tag] != "" || gotMalformed[tag] != "" {
			t.Errorf("%s reached the checker; the walk must skip its directory or file", tag)
		}
	}
}

// TestScanOfATreeWithNoSourceFindsNothing proves the scan does not invent a
// claim. TestRepoClaimsHold pairs with it: the repository walk fails when the
// claim count drops to zero, so an empty result can never read as a pass.
func TestScanOfATreeWithNoSourceFindsNothing(t *testing.T) {
	claims, malformed, err := Scan("testdata/empty")
	if err != nil {
		t.Fatalf("scan testdata/empty: %v", err)
	}
	if len(claims) != 0 || len(malformed) != 0 {
		t.Fatalf("scan of a tree with no source returned %d claim(s) and %d malformed marker(s), want none",
			len(claims), len(malformed))
	}
}

// TestVerifyNeverSkipsAMissingTarget pins the rule that costs the most when it
// is wrong. Three guards in this repository skipped when their input file went
// away, and one was the only cross-language shader check in the tree.
func TestVerifyNeverSkipsAMissingTarget(t *testing.T) {
	c := Claim{
		Source:  "fake.go",
		Line:    1,
		Verb:    VerbHas,
		Target:  "no/such/file.go",
		Pattern: "anything",
	}
	res := Verify(corpusRoot, c)
	if res.OK {
		t.Fatal("Verify passed a claim whose target does not exist")
	}
	for _, want := range []string{"no/such/file.go", "cannot be read", "Do not skip it"} {
		if !strings.Contains(res.Problem, want) {
			t.Errorf("problem does not mention %q:\n%s", want, res.Problem)
		}
	}
}

// TestParseFileReportsEveryMarkerOnce guards against a scan that reads one line
// twice or drops one. The corpus is the only input whose marker count is known
// by hand.
func TestParseFileReportsEveryMarkerOnce(t *testing.T) {
	claims, malformed, err := Scan(corpusRoot)
	if err != nil {
		t.Fatalf("scan %s: %v", corpusRoot, err)
	}
	seen := map[string]int{}
	for _, c := range claims {
		seen[tagOf(c.Text)]++
	}
	for _, m := range malformed {
		seen[tagOf(m.Text)]++
	}
	for tag, n := range seen {
		if n != 1 {
			t.Errorf("%s was reported %d times, want 1", tag, n)
		}
	}
	want := len(wantOK) + len(wantFalse) + len(wantMalformed)
	if got := len(claims) + len(malformed); got != want {
		t.Errorf("the corpus produced %d marker(s), want %d; add the new case to the tables in this file", got, want)
	}
}

// assertSameTags reports every tag that is in one set and not the other.
func assertSameTags(t *testing.T, kind string, want, got []string) {
	t.Helper()
	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[w] = true
	}
	gotSet := map[string]bool{}
	for _, g := range got {
		gotSet[g] = true
	}
	for _, w := range want {
		if !gotSet[w] {
			t.Errorf("%s: expected case %q, and the checker did not report it", kind, w)
		}
	}
	for _, g := range got {
		if !wantSet[g] {
			t.Errorf("%s: the checker reported case %q, which the table does not expect", kind, g)
		}
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func keysOfBool(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
