package motion

// Interner assigns stable, deterministic numeric IDs to string refs in
// first-seen order.  IDs are assigned 0, 1, 2, … in the order Intern is
// called for each unique string.  Map iteration order is NEVER used to
// assign IDs — only the slice (ordered) side is the authority.
type Interner struct {
	ids  map[string]int
	refs []string
}

// NewInterner returns an empty Interner ready for use.
func NewInterner() *Interner {
	return &Interner{ids: make(map[string]int)}
}

// Intern returns the existing id for s, or assigns the next sequential id
// (0, 1, 2, …) on first sight.
func (in *Interner) Intern(s string) int {
	if id, ok := in.ids[s]; ok {
		return id
	}
	id := len(in.refs)
	in.ids[s] = id
	in.refs = append(in.refs, s)
	return id
}

// Lookup returns the ref for an id and ok=true.  Returns ("", false) when
// id is out of range.
func (in *Interner) Lookup(id int) (string, bool) {
	if id < 0 || id >= len(in.refs) {
		return "", false
	}
	return in.refs[id], true
}

// Len returns the number of unique refs interned so far.
func (in *Interner) Len() int { return len(in.refs) }

// Refs returns the refs indexed by id (Refs()[i] == ref with id i).
// The returned slice is the interner's backing slice — do not mutate it.
func (in *Interner) Refs() []string { return in.refs }

// PrepareTracks walks the timeline (including nested Sub timelines) in
// deterministic pre-order and assigns each Track's TargetID and PropID by
// interning Track.Target.Ref into targets and Track.Prop into props.
//
// Pre-order: for every Positioned child, its Track (if any) is processed
// before recursing into its Sub (if any).  Children are visited in slice
// order (index 0 first), which is deterministic.  Map iteration is never
// used — IDs are assigned strictly by first-seen order in this traversal.
func PrepareTracks(tl *Timeline, targets, props *Interner) {
	prepareTimeline(tl, targets, props)
}

func prepareTimeline(tl *Timeline, targets, props *Interner) {
	for i := range tl.Children {
		child := &tl.Children[i]

		// Process the track first (pre-order).
		if child.Track != nil {
			child.Track.TargetID = targets.Intern(child.Track.Target.Ref)
			child.Track.PropID = props.Intern(child.Track.Prop)
		}

		// Then recurse into the sub-timeline.
		if child.Sub != nil {
			prepareTimeline(child.Sub, targets, props)
		}
	}
}
