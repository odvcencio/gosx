package motion

import (
	"testing"
)

// TestInternBasic covers the core Intern/Lookup/Len/Refs behaviour.
func TestInternBasic(t *testing.T) {
	in := NewInterner()

	id0 := in.Intern("alpha")
	id1 := in.Intern("beta")
	id2 := in.Intern("gamma")

	if id0 != 0 {
		t.Errorf("first Intern want 0, got %d", id0)
	}
	if id1 != 1 {
		t.Errorf("second Intern want 1, got %d", id1)
	}
	if id2 != 2 {
		t.Errorf("third Intern want 2, got %d", id2)
	}

	// Re-interning the first ref must return the same id.
	again := in.Intern("alpha")
	if again != 0 {
		t.Errorf("re-intern 'alpha' want 0, got %d", again)
	}

	// Len must reflect unique refs only.
	if in.Len() != 3 {
		t.Errorf("Len want 3, got %d", in.Len())
	}

	// Lookup by id.
	s, ok := in.Lookup(1)
	if !ok || s != "beta" {
		t.Errorf("Lookup(1) want ('beta', true), got (%q, %v)", s, ok)
	}
	_, ok = in.Lookup(99)
	if ok {
		t.Error("Lookup(99) want ok=false")
	}

	// Refs slice order must match insertion order.
	refs := in.Refs()
	if len(refs) != 3 {
		t.Fatalf("Refs len want 3, got %d", len(refs))
	}
	want := []string{"alpha", "beta", "gamma"}
	for i, w := range want {
		if refs[i] != w {
			t.Errorf("Refs()[%d] want %q, got %q", i, w, refs[i])
		}
	}
}

// TestPrepareTracks covers basic two-track assignment and determinism.
func TestPrepareTracks(t *testing.T) {
	buildTL := func() *Timeline {
		return &Timeline{
			Children: []Positioned{
				{
					Track: &Track{
						Target: Target{Kind: TargetSceneNode, Ref: "a"},
						Prop:   "position",
					},
				},
				{
					Track: &Track{
						Target: Target{Kind: TargetSceneNode, Ref: "b"},
						Prop:   "position",
					},
				},
			},
		}
	}

	// First run.
	tl1 := buildTL()
	targets1 := NewInterner()
	props1 := NewInterner()
	PrepareTracks(tl1, targets1, props1)

	t0 := tl1.Children[0].Track
	t1 := tl1.Children[1].Track

	if t0.TargetID != 0 {
		t.Errorf("track0 TargetID want 0, got %d", t0.TargetID)
	}
	if t1.TargetID != 1 {
		t.Errorf("track1 TargetID want 1, got %d", t1.TargetID)
	}
	if t0.PropID != 0 {
		t.Errorf("track0 PropID want 0, got %d", t0.PropID)
	}
	if t1.PropID != 0 {
		t.Errorf("track1 PropID want 0 (same prop), got %d", t1.PropID)
	}

	// Second run with fresh interners must produce identical assignment.
	tl2 := buildTL()
	targets2 := NewInterner()
	props2 := NewInterner()
	PrepareTracks(tl2, targets2, props2)

	t2a := tl2.Children[0].Track
	t2b := tl2.Children[1].Track

	if t2a.TargetID != t0.TargetID {
		t.Errorf("determinism: run2 track0 TargetID %d != run1 %d", t2a.TargetID, t0.TargetID)
	}
	if t2b.TargetID != t1.TargetID {
		t.Errorf("determinism: run2 track1 TargetID %d != run1 %d", t2b.TargetID, t1.TargetID)
	}
	if t2a.PropID != t0.PropID {
		t.Errorf("determinism: run2 track0 PropID %d != run1 %d", t2a.PropID, t0.PropID)
	}
	if t2b.PropID != t1.PropID {
		t.Errorf("determinism: run2 track1 PropID %d != run1 %d", t2b.PropID, t1.PropID)
	}
}

// TestPrepareTracksNested verifies pre-order traversal: parent tracks before
// sub-timeline tracks, and repeated refs reuse the same id.
func TestPrepareTracksNested(t *testing.T) {
	// Timeline structure:
	//   child[0] -> Track{ref:"x", prop:"scale"}
	//   child[1] -> Sub timeline with:
	//                 sub.child[0] -> Track{ref:"y", prop:"rotation"}
	//                 sub.child[1] -> Track{ref:"x", prop:"scale"}  (repeat of "x")
	tl := &Timeline{
		Children: []Positioned{
			{
				Track: &Track{
					Target: Target{Kind: TargetSceneNode, Ref: "x"},
					Prop:   "scale",
				},
			},
			{
				Sub: &Timeline{
					Children: []Positioned{
						{
							Track: &Track{
								Target: Target{Kind: TargetSceneNode, Ref: "y"},
								Prop:   "rotation",
							},
						},
						{
							Track: &Track{
								Target: Target{Kind: TargetSceneNode, Ref: "x"},
								Prop:   "scale",
							},
						},
					},
				},
			},
		},
	}

	targets := NewInterner()
	props := NewInterner()
	PrepareTracks(tl, targets, props)

	// Pre-order: child[0].Track ("x") is first → TargetID 0.
	track0 := tl.Children[0].Track
	if track0.TargetID != 0 {
		t.Errorf("parent track TargetID want 0, got %d", track0.TargetID)
	}
	if track0.PropID != 0 {
		t.Errorf("parent track PropID want 0, got %d", track0.PropID)
	}

	// Sub child[0] ("y") → TargetID 1 (first new ref after "x").
	subTrack0 := tl.Children[1].Sub.Children[0].Track
	if subTrack0.TargetID != 1 {
		t.Errorf("sub track0 TargetID want 1, got %d", subTrack0.TargetID)
	}
	// "rotation" is a new prop → PropID 1.
	if subTrack0.PropID != 1 {
		t.Errorf("sub track0 PropID want 1, got %d", subTrack0.PropID)
	}

	// Sub child[1] ("x") → TargetID 0 (reuse).
	subTrack1 := tl.Children[1].Sub.Children[1].Track
	if subTrack1.TargetID != 0 {
		t.Errorf("sub track1 TargetID (repeat 'x') want 0, got %d", subTrack1.TargetID)
	}
	// "scale" is reused → PropID 0.
	if subTrack1.PropID != 0 {
		t.Errorf("sub track1 PropID (repeat 'scale') want 0, got %d", subTrack1.PropID)
	}

	// Interner lengths must reflect unique refs only.
	if targets.Len() != 2 {
		t.Errorf("targets.Len want 2, got %d", targets.Len())
	}
	if props.Len() != 2 {
		t.Errorf("props.Len want 2, got %d", props.Len())
	}
}
