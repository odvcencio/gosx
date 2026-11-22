package actions

// Releases turns buttons that act when they are let go into one command per
// release, from a simulation step's edges (Ticks.Edges): a pass thrown when
// the receiver's button comes up, soft after a tap, hard after a hold. A
// release counts only for a press the step reader saw while the button could
// act; a release the player did not make (the window lost the focus, the
// browser took the touch) counts for nothing.
//
// Whether a release was a tap is the action's own tap timing (ActionDef.Tap,
// Edge.Tapped). The zero Releases is ready to use. It is not safe for
// concurrent use.
type Releases struct {
	armed map[string]bool
}

// Step reads one step's edges for name. available says whether the button
// can act now; while it cannot, a press is forgotten. fired is true for the
// release of a press seen while available, with tapped for a tap.
func (r *Releases) Step(edges []Edge, name string, available bool) (fired, tapped bool) {
	if !available {
		delete(r.armed, name)
		return false, false
	}
	for _, e := range edges {
		if e.Name != name {
			continue
		}
		switch e.Kind {
		case PressEdge:
			if r.armed == nil {
				r.armed = map[string]bool{}
			}
			r.armed[name] = true
		case ReleaseEdge:
			if r.armed[name] && !e.Canceled {
				fired, tapped = true, e.Tapped
			}
			delete(r.armed, name)
		}
	}
	return fired, tapped
}

// Armed reports whether a press of name waits for its release.
func (r *Releases) Armed(name string) bool { return r.armed[name] }

// Clear forgets every press waiting for its release: at a snap, a whistle, a
// dialog or a tuck of the ball.
func (r *Releases) Clear() { clear(r.armed) }
