package evalparity

import (
	"errors"
	"fmt"
	"testing"
)

// outcome is one backend's result for one case: exactly one of text or err
// is set.
type outcome struct {
	text string
	err  error
}

// summary accumulates the counts the harness reports at the end of a run:
// cases, per-backend agreement/divergence/unsupported, and any mismatch
// the table does not yet account for (a real, newly found divergence).
type summary struct {
	cases       int
	agreed      map[Backend]int
	unsupported map[Backend]int
	diverged    map[Backend]int
	unexpected  []string
}

func newSummary() *summary {
	return &summary{
		agreed:      map[Backend]int{},
		unsupported: map[Backend]int{},
		diverged:    map[Backend]int{},
	}
}

// TestExpressionParity is the differential harness: every case in cases
// runs through all three backends that claim to support it, and the
// result is checked against the case's pinned expectation (Want, an
// accepted Diverges value, or an Unsupported error). A backend producing
// something the table does not predict is a newly found, unrecorded
// divergence and fails the test — see doc.go for the triage contract this
// enforces (fix it, or add a Diverges/Unsupported entry that documents
// it).
func TestExpressionParity(t *testing.T) {
	if err := validateCases(cases); err != nil {
		t.Fatalf("cases table is inconsistent: %v", err)
	}

	transpileResults := runTranspileBatch(t, cases)
	sum := newSummary()
	sum.cases = len(cases)

	for _, c := range cases {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			results := map[Backend]outcome{}

			tr, ok := transpileResults[c.ID]
			switch {
			case !ok:
				results[Transpile] = outcome{err: errors.New("no transpile result recorded (harness bug)")}
			case tr.Err != "":
				results[Transpile] = outcome{err: errors.New(tr.Err)}
			default:
				results[Transpile] = outcome{text: tr.HTML}
			}

			prog, idx, cerr := compileCase(c)
			if cerr != nil {
				results[Route] = outcome{err: cerr}
				results[VM] = outcome{err: cerr}
			} else {
				if text, err := runRoute(prog, c); err != nil {
					results[Route] = outcome{err: err}
				} else {
					results[Route] = outcome{text: text}
				}
				if text, err := runVM(prog, idx, c); err != nil {
					results[VM] = outcome{err: err}
				} else {
					results[VM] = outcome{text: text}
				}
			}

			for _, b := range backendOrder {
				res := results[b]
				checkBackend(t, sum, c, b, res)
			}
		})
	}

	t.Logf("evalparity summary: %d cases", sum.cases)
	for _, b := range backendOrder {
		t.Logf("  %-10s agreed=%-3d diverged=%-3d unsupported=%-3d",
			b, sum.agreed[b], sum.diverged[b], sum.unsupported[b])
	}
	if len(sum.unexpected) > 0 {
		t.Logf("  %d newly found (unrecorded) divergence(s):", len(sum.unexpected))
		for _, line := range sum.unexpected {
			t.Logf("    %s", line)
		}
	}
}

// checkBackend reconciles one backend's outcome for one case against the
// case's Unsupported/Diverges/Want expectations, records the result in
// sum, and fails t when reality does not match the table.
func checkBackend(t *testing.T, sum *summary, c Case, b Backend, res outcome) {
	t.Helper()

	if reason, expectUnsupported := c.Unsupported[b]; expectUnsupported {
		if res.err == nil {
			t.Errorf("%s: expected unsupported (%s), but it ran and produced %q", b, reason, res.text)
			sum.unexpected = append(sum.unexpected, fmt.Sprintf("%s/%s: expected unsupported, got %q", c.ID, b, res.text))
			return
		}
		sum.unsupported[b]++
		return
	}

	if res.err != nil {
		t.Errorf("%s: unexpected error (case does not list this backend in Unsupported): %v", b, res.err)
		sum.unexpected = append(sum.unexpected, fmt.Sprintf("%s/%s: unexpected error: %v", c.ID, b, res.err))
		return
	}

	if divergeText, isDivergent := c.Diverges[b]; isDivergent {
		if res.text != divergeText {
			t.Errorf("%s: pinned divergence changed — want %q, got %q (reason: %s)", b, divergeText, res.text, c.DivergesReason[b])
			sum.unexpected = append(sum.unexpected, fmt.Sprintf("%s/%s: divergence drifted: want %q got %q", c.ID, b, divergeText, res.text))
			return
		}
		sum.diverged[b]++
		return
	}

	if res.text != c.Want {
		t.Errorf("%s: got %q, want %q", b, res.text, c.Want)
		sum.unexpected = append(sum.unexpected, fmt.Sprintf("%s/%s: got %q want %q", c.ID, b, res.text, c.Want))
		return
	}
	sum.agreed[b]++
}
