package capability

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// evidenceT is the part of *testing.T that cellEvidence uses. The self-test at
// the bottom of this file substitutes a recorder for it, so it can check that
// assertAgrees fails in BOTH flip directions without failing itself.
type evidenceT interface {
	Helper()
	Fatalf(format string, args ...any)
}

// cellEvidence ties one Matrix cell to renderer source, in whichever direction
// the renderer points.
//
// WHY this type exists. Eight tests in this package used to open with a guard
// shaped like:
//
//	if !Matrix[FeatureGPUCull][BackendWebGPU] {
//	    t.Skip("Matrix says WebGPU cannot GPU-cull; nothing to corroborate")
//	}
//
// The guard is conditional on the thing it guards. So flipping a cell to a lie
// also DELETES the test that would catch the lie, and the suite still reports
// green. Five wrong cells were found by hand before this shape changed, so the
// weakness was not hypothetical.
//
// The correct order is: read the renderer, decide what the cell must be, then
// assert the cell matches. A cell flipped either way then fails. cellEvidence
// records the reading, so a failure names the symbol that settled it.
//
// The two symbol kinds carry opposite polarity:
//
//   - needs:     every symbol must be present for the cell to be true. One
//     missing symbol means the implementation is not there.
//   - refutedBy: any symbol being present means the cell must be true. A false
//     cell rests on all of them staying absent.
//
// water_shadow_test.go and linedashed_test.go already read the renderer before
// deciding. This type generalises their shape.
type cellEvidence struct {
	t       evidenceT
	feature Feature
	backend Backend

	// required counts every symbol passed to needs, so an empty reading cannot
	// claim "all present" and silently demand a true cell.
	required int
	// refuting counts every symbol passed to refutedBy. It is counted apart from
	// required because a refuting reading that finds NOTHING is the healthy case
	// for a false cell, and it must not look like "no reading happened".
	refuting int
	// absent lists the required symbols the renderer does not carry.
	absent []string
	// found lists the refuting symbols the renderer does carry.
	found []string
}

// evidenceFor starts a reading of the cell for feature on backend.
func evidenceFor(t evidenceT, feature Feature, backend Backend) *cellEvidence {
	t.Helper()
	if _, ok := Matrix[feature]; !ok {
		t.Fatalf("Matrix has no row for %q, so no cell can be corroborated; a test that reads "+
			"a missing row passes on the zero value and proves nothing", feature)
	}
	return &cellEvidence{t: t, feature: feature, backend: backend}
}

// needs records that source must contain every symbol for the cell to be true.
// path names the file in the failure message.
func (e *cellEvidence) needs(path, source string, symbols ...string) *cellEvidence {
	e.t.Helper()
	for _, symbol := range symbols {
		e.required++
		if !strings.Contains(source, symbol) {
			e.absent = append(e.absent, fmt.Sprintf("%s: %q", path, symbol))
		}
	}
	return e
}

// refutedBy records that the cell must be true if source contains any symbol.
// Use it for a cell that reads false: the absence is the evidence.
func (e *cellEvidence) refutedBy(path, source string, symbols ...string) *cellEvidence {
	e.t.Helper()
	for _, symbol := range symbols {
		e.refuting++
		if strings.Contains(source, symbol) {
			e.found = append(e.found, fmt.Sprintf("%s: %q", path, symbol))
		}
	}
	return e
}

// wantCell reports the value the renderer demands of the cell.
func (e *cellEvidence) wantCell() bool {
	if len(e.found) > 0 {
		return true
	}
	return e.required > 0 && len(e.absent) == 0
}

// assertAgrees fails when the real Matrix cell and the renderer disagree, in
// either direction.
func (e *cellEvidence) assertAgrees(why string) {
	e.t.Helper()
	e.assertCellIs(Matrix[e.feature][e.backend], why)
}

// assertCellIs compares one cell value against the reading. assertAgrees passes
// the real Matrix cell. The self-test passes both values directly, so it never
// mutates the shared Matrix.
//
// why states the reasoning behind the reading, so a reader who hits the failure
// does not have to rebuild it.
func (e *cellEvidence) assertCellIs(got bool, why string) {
	e.t.Helper()
	if e.required == 0 && e.refuting == 0 {
		e.t.Fatalf("no evidence was read for Matrix[%s][%s]; call needs or refutedBy before "+
			"asserting, or the assertion proves nothing", e.feature, e.backend)
		return
	}
	want := e.wantCell()
	if got == want {
		return
	}
	if want {
		e.t.Fatalf("Matrix[%s][%s] is false but the renderer implements the feature.\n"+
			"Evidence that it does: %s\n"+
			"Reasoning: %s\n"+
			"Flip the cell to true, update the renderer capability manifest so drift_test.go "+
			"stays green, and rewrite this test to corroborate the new answer.",
			e.feature, e.backend, strings.Join(sortedCopy(e.found), ", "), why)
		return
	}
	e.t.Fatalf("Matrix[%s][%s] is true but the renderer does not implement the feature.\n"+
		"Missing evidence: %s\n"+
		"Reasoning: %s\n"+
		"Either finish the implementation or flip the cell back to false. A cell that reads "+
		"true without an implementation renders a wrong image and reports no gap to the author.",
		e.feature, e.backend, strings.Join(sortedCopy(e.absent), ", "), why)
}

// missingSymbols reports which symbols source does not contain, each prefixed
// with path so a failure names the file. Use it for a claim that is not a Matrix
// cell — LightKindFeatures, for one — where the same "read first, then assert"
// order still applies.
func missingSymbols(path, source string, symbols ...string) []string {
	var missing []string
	for _, symbol := range symbols {
		if !strings.Contains(source, symbol) {
			missing = append(missing, fmt.Sprintf("%s: %q", path, symbol))
		}
	}
	return missing
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// recordingT records a Fatalf instead of ending the test, so the self-test can
// examine a deliberate failure.
type recordingT struct {
	failed  bool
	message string
}

func (r *recordingT) Helper() {}

func (r *recordingT) Fatalf(format string, args ...any) {
	r.failed = true
	r.message = fmt.Sprintf(format, args...)
}

// TestCellEvidenceFailsBothFlipDirections proves the helper catches a wrong cell
// whichever way it is wrong. Without this, the restructure would rest on the same
// kind of unchecked assumption it exists to remove.
//
// Every case supplies a synthetic source string, so no real renderer or real cell
// takes part.
func TestCellEvidenceFailsBothFlipDirections(t *testing.T) {
	const path = "synthetic.js"

	cases := []struct {
		name       string
		cell       bool
		read       func(e *cellEvidence)
		wantFailed bool
	}{
		{
			name:       "true cell with every required symbol present agrees",
			cell:       true,
			read:       func(e *cellEvidence) { e.needs(path, "alpha beta", "alpha", "beta") },
			wantFailed: false,
		},
		{
			name:       "true cell with a required symbol missing fails",
			cell:       true,
			read:       func(e *cellEvidence) { e.needs(path, "alpha", "alpha", "beta") },
			wantFailed: true,
		},
		{
			name:       "false cell with a required symbol missing agrees",
			cell:       false,
			read:       func(e *cellEvidence) { e.needs(path, "alpha", "alpha", "beta") },
			wantFailed: false,
		},
		{
			name:       "false cell with every required symbol present fails",
			cell:       false,
			read:       func(e *cellEvidence) { e.needs(path, "alpha beta", "alpha", "beta") },
			wantFailed: true,
		},
		{
			name:       "false cell with no refuting symbol agrees",
			cell:       false,
			read:       func(e *cellEvidence) { e.refutedBy(path, "unrelated source", "beginComputePass") },
			wantFailed: false,
		},
		{
			name:       "false cell refuted by a present symbol fails",
			cell:       false,
			read:       func(e *cellEvidence) { e.refutedBy(path, "beginComputePass()", "beginComputePass") },
			wantFailed: true,
		},
		{
			name:       "true cell with a refuting symbol present agrees",
			cell:       true,
			read:       func(e *cellEvidence) { e.refutedBy(path, "beginComputePass()", "beginComputePass") },
			wantFailed: false,
		},
		{
			name:       "a reading with no evidence at all fails either way",
			cell:       true,
			read:       func(e *cellEvidence) {},
			wantFailed: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingT{}
			// FeatureGPUCull only supplies a real Matrix row for evidenceFor to
			// find. The cell value under test is tc.cell, passed explicitly.
			e := evidenceFor(rec, FeatureGPUCull, BackendWebGPU)
			tc.read(e)
			e.assertCellIs(tc.cell, "synthetic reading")
			if rec.failed != tc.wantFailed {
				t.Fatalf("cell=%v: failed=%v want %v (message: %s)",
					tc.cell, rec.failed, tc.wantFailed, rec.message)
			}
		})
	}
}

// TestEvidenceForRejectsAMissingRow keeps evidenceFor from reading a row that
// does not exist. Matrix[unknown][backend] returns false on a nil map, so a
// typo in a feature name would otherwise make a corroboration test assert
// "false equals false" forever.
func TestEvidenceForRejectsAMissingRow(t *testing.T) {
	rec := &recordingT{}
	evidenceFor(rec, Feature("no-such-feature"), BackendWebGPU)
	if !rec.failed {
		t.Fatal("evidenceFor accepted a feature with no Matrix row")
	}
	if !strings.Contains(rec.message, "no row") {
		t.Fatalf("the failure must name the missing row; got: %s", rec.message)
	}
}
