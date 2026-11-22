package actions

import (
	"testing"
	"time"
)

func TestReleasesFireOnTheReleaseOfASeenPress(t *testing.T) {
	var r Releases
	press := Edge{Name: "throw1", Kind: PressEdge}
	tap := Edge{Name: "throw1", Kind: ReleaseEdge, Tapped: true}
	hold := Edge{Name: "throw1", Kind: ReleaseEdge, HeldFor: 300 * time.Millisecond}
	if fired, _ := r.Step([]Edge{press}, "throw1", true); fired || !r.Armed("throw1") {
		t.Fatal("a press fired, or was not kept")
	}
	if fired, tapped := r.Step([]Edge{tap}, "throw1", true); !fired || !tapped {
		t.Fatalf("a tap's release: fired %v tapped %v", fired, tapped)
	}
	// Press and hold, release on a later step: fired, not a tap.
	r.Step([]Edge{press}, "throw1", true)
	r.Step(nil, "throw1", true)
	if fired, tapped := r.Step([]Edge{hold}, "throw1", true); !fired || tapped {
		t.Fatalf("a hold's release: fired %v tapped %v", fired, tapped)
	}
	// A tap inside one step: press and release together.
	if fired, tapped := r.Step([]Edge{press, tap}, "throw1", true); !fired || !tapped {
		t.Fatalf("a tap in one step: fired %v tapped %v", fired, tapped)
	}
	// Other actions' edges do nothing.
	if fired, _ := r.Step([]Edge{{Name: "throw2", Kind: PressEdge}, {Name: "throw2", Kind: ReleaseEdge}}, "throw1", true); fired {
		t.Fatal("another button's release fired")
	}
}

func TestReleasesIgnoreUnseenCanceledAndUnavailable(t *testing.T) {
	var r Releases
	release := Edge{Name: "b", Kind: ReleaseEdge, Tapped: true}
	if fired, _ := r.Step([]Edge{release}, "b", true); fired {
		t.Fatal("a release of a press never seen fired")
	}
	r.Step([]Edge{{Name: "b", Kind: PressEdge}}, "b", true)
	if fired, _ := r.Step([]Edge{{Name: "b", Kind: ReleaseEdge, Canceled: true}}, "b", true); fired {
		t.Fatal("a canceled release fired")
	}
	r.Step([]Edge{{Name: "b", Kind: PressEdge}}, "b", true)
	r.Step(nil, "b", false) // the button could no longer act
	if fired, _ := r.Step([]Edge{release}, "b", true); fired {
		t.Fatal("a press forgotten while unavailable fired")
	}
	r.Step([]Edge{{Name: "b", Kind: PressEdge}}, "b", true)
	r.Clear()
	if fired, _ := r.Step([]Edge{release}, "b", true); fired || r.Armed("b") {
		t.Fatal("Clear kept a press")
	}
}
