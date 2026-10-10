package budget

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/assetmeasure"
)

func testPhaseCost(id, owner, phase string, n, wire int64) PhaseCost {
	return PhaseCost{RequestIdentity: id, Owner: owner, Phase: phase, Sizes: assetmeasure.Sizes{SHA256: strings.Repeat("a", 64), Raw: n * 2, Gzip: n + 1, Brotli: n}, WireBytes: wire, Requests: 1}
}
func TestMeasurePhasesReconcileOwnershipAndSessionInventory(t *testing.T) {
	costs := []PhaseCost{
		testPhaseCost("document", "app", "critical", 10, 20),
		testPhaseCost("runtime", "framework", "startup", 100, 80),
		testPhaseCost("fallback", "framework", "after-ready", 300, 240),
		testPhaseCost("full", "framework", "dormant", 500, 0),
	}
	costs[3].Requests = 0
	got, err := SumPhases(costs)
	want := PhaseTotals{NormalizedBytes: 110, FrameworkBytes: 100, AppBytes: 10, WireBytes: 100, Requests: 2, Phases: PhaseBytes{Critical: 10, Startup: 100, AfterReady: 300, Dormant: 500}}
	if err != nil || !reflect.DeepEqual(got, want) || got.FrameworkBytes+got.AppBytes != got.NormalizedBytes || got.Phases.Critical+got.Phases.Startup != got.NormalizedBytes {
		t.Fatal("phase or ownership reconciliation differs", got, err)
	}
}
func TestMeasurePhasesAliasesAndRedirects(t *testing.T) {
	cost := testPhaseCost("resolved-runtime", "app", "after-ready", 100, 80)
	alias := cost
	alias.Owner = "framework"
	alias.Phase = "startup"
	redirect := testPhaseCost("redirect-hop-a", "app", "startup", 10, 8)
	otherHop := redirect
	otherHop.RequestIdentity = "redirect-hop-b"
	got, err := SumPhases([]PhaseCost{cost, alias, redirect, otherHop})
	want := PhaseTotals{NormalizedBytes: 120, FrameworkBytes: 100, AppBytes: 20, WireBytes: 96, Requests: 3, Phases: PhaseBytes{Startup: 120}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("aliases repeated or redirect requests lost", got, err)
	}
	reversed, err := SumPhases([]PhaseCost{otherHop, redirect, alias, cost})
	if err != nil || !reflect.DeepEqual(got, reversed) {
		t.Fatal("phase accounting depends on inventory order")
	}
}
func TestMeasurePhasesInvalidAndConflictingCosts(t *testing.T) {
	for _, cause := range []string{"phase", "owner", "identity", "hash", "raw", "gzip", "brotli", "wire", "requests", "changed-body", "changed-encoding", "overflow"} {
		t.Run(cause, func(t *testing.T) {
			cost := testPhaseCost("fixture", "app", "startup", 100, 80)
			costs := []PhaseCost{cost}
			switch cause {
			case "phase":
				costs[0].Phase = "lazy"
			case "owner":
				costs[0].Owner = "other"
			case "identity":
				costs[0].RequestIdentity = ""
			case "hash":
				costs[0].Sizes.SHA256 = "invalid"
			case "raw":
				costs[0].Sizes.Raw = -1
			case "gzip":
				costs[0].Sizes.Gzip = -1
			case "brotli":
				costs[0].Sizes.Brotli = -1
			case "wire":
				costs[0].WireBytes = -1
			case "requests":
				costs[0].Requests = -1
			case "changed-body":
				alias := cost
				alias.Sizes.SHA256 = strings.Repeat("b", 64)
				costs = append(costs, alias)
			case "changed-encoding":
				alias := cost
				alias.WireBytes++
				costs = append(costs, alias)
			case "overflow":
				costs[0].Sizes.Brotli = math.MaxInt64
				other := cost
				other.RequestIdentity = "other"
				costs = append(costs, other)
			}
			_, err := SumPhases(costs)
			var typed *InputError
			if !errors.As(err, &typed) || typed.Reference != "measure" || typed.Pointer != "/phaseCosts" {
				t.Fatal("invalid cost accepted or error lacked location", err)
			}
		})
	}
}
