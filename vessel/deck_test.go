package vessel

import (
	"math"
	"reflect"
	"testing"
)

func TestPolygonContains(t *testing.T) {
	sq := Rect{X: 10, Z: -4, SizeX: 6, SizeZ: 2}.Polygon()
	for _, c := range []struct {
		x, z float64
		in   bool
	}{{10, -4, true}, {12.9, -4.9, true}, {13.1, -4, false}, {10, -2.9, false}, {7.1, -3.1, true}, {0, 0, false}} {
		if got := sq.Contains(c.x, c.z); got != c.in {
			t.Errorf("(%v,%v) inside = %v, want %v", c.x, c.z, got, c.in)
		}
	}
	// A concave L shape.
	l := Polygon{{0, 0}, {4, 0}, {4, 1}, {1, 1}, {1, 4}, {0, 4}}
	if !l.Contains(3, 0.5) || !l.Contains(0.5, 3) || l.Contains(3, 3) {
		t.Error("L-shaped polygon misclassified a point")
	}
}

func TestDeckPlanOutlineIsHullShaped(t *testing.T) {
	plan := DeckPlan{Name: "Promenade", Y: 10, Length: 200, Beam: 30, Voids: []Rect{{X: 0, Z: 0, SizeX: 8, SizeZ: 20}}}
	d := plan.Build()
	if !d.Contains(0, 50) || !d.Contains(14, 0) || d.Contains(16, 0) {
		t.Error("amidships should span the beam")
	}
	if d.Contains(0, 101) || d.Contains(0, -101) {
		t.Error("deck must end at the stem and transom")
	}
	// Narrower toward the bow than amidships, and the stern keeps most of the beam.
	if d.Contains(10, -95) || !d.Contains(10, 90) {
		t.Error("bow should taper and the stern stay broad")
	}
	if d.Contains(0, 0) || d.Contains(3, 9) || !d.Contains(5, 0) {
		t.Error("the void must be a hole")
	}
	inset := DeckPlan{Length: 200, Beam: 30, Inset: 1}.Build()
	if inset.Contains(14.6, 0) || !inset.Contains(13.9, 0) {
		t.Error("inset should pull the outline in from the rail")
	}
}

func TestStandardLayoutForEachClass(t *testing.T) {
	for _, c := range Presets() {
		l := StandardLayout(c)
		if len(l.Decks) != c.Decks {
			t.Fatalf("%s: %d decks, want %d", c.Name, len(l.Decks), c.Decks)
		}
		for i, d := range l.Decks {
			if d.Index != i || (i > 0 && d.Y <= l.Decks[i-1].Y) {
				t.Errorf("%s deck %d: index %d, Y %v not ascending", c.Name, i, d.Index, d.Y)
			}
			if !d.Contains(float64(0.3*c.Beam), float64(0.1*c.Length)) && i > 1 {
				// the point is inside the hull outline on every upper deck
				t.Errorf("%s deck %d should cover (0.3B, 0.1L)", c.Name, i)
			}
		}
		// Every portal stop is a walkable spot on its deck, and a stair flight
		// ends on the deck above, at the end of its hole.
		for _, p := range l.Portals {
			for _, s := range p.Stops {
				if !l.Decks[s].Contains(p.X, p.Z) {
					t.Errorf("%s: portal %s stop on deck %d at (%.1f,%.1f) is not walkable", c.Name, p.ID, s, p.X, p.Z)
				}
			}
		}
		for i := 0; i < c.Decks-1; i++ {
			if len(l.Ramps[i]) != 2 {
				t.Fatalf("%s: deck %d has %d flights", c.Name, i, len(l.Ramps[i]))
			}
			for _, r := range l.Ramps[i] {
				top, bottom := r.ToY, r.FromY
				if r.FromY > r.ToY {
					top, bottom = r.FromY, r.ToY
				}
				if bottom != l.Decks[i].Y || top != l.Decks[i+1].Y {
					t.Errorf("%s flight %d spans %.2f..%.2f, decks %.2f..%.2f", c.Name, i, bottom, top, l.Decks[i].Y, l.Decks[i+1].Y)
				}
				if l.Decks[i+1].Contains(r.X, r.Z) {
					t.Errorf("%s: deck %d has no hole over the flight from below", c.Name, i+1)
				}
				if !l.Decks[i].Contains(r.X, r.Z) {
					t.Errorf("%s: flight %d does not start on its deck", c.Name, i)
				}
				// The foot of the flight is on the lower deck and its head ends beside the hole on the upper deck.
				head := r.Z + float64(0.5*r.SizeZ) + 0.2
				if r.FromY > r.ToY {
					head = r.Z - float64(0.5*r.SizeZ) - 0.2
				}
				if !l.Decks[i+1].Contains(r.X, head) {
					t.Errorf("%s flight %d: nothing to step onto past its head", c.Name, i)
				}
			}
		}
	}
}

func TestDeckAtAndFloorAt(t *testing.T) {
	l := StandardLayout(Liner())
	// Standing on every deck at a spot clear of voids returns that deck.
	for i, d := range l.Decks {
		if got := l.DeckAt(-10, d.Y, 60); got != i {
			t.Errorf("DeckAt on deck %d returned %d", i, got)
		}
		if got := l.DeckAt(-10, d.Y+0.4, 60); got != i {
			t.Errorf("a walker 0.4 m above deck %d returned %d", i, got)
		}
	}
	if got := l.DeckAt(-10, -50, 60); got != -1 {
		t.Errorf("below the lowest deck returned %d", got)
	}
	// Above the highest deck still belongs to it.
	top := len(l.Decks) - 1
	if got := l.DeckAt(-10, l.Decks[top].Y+1.7, 60); got != top {
		t.Errorf("eyes above the top deck returned %d", got)
	}
	// Over the atrium a walker at the upper deck's level has no floor there; the next deck down holds.
	atriumDeck := len(l.Decks) / 3
	if _, _, ok := l.FloorAt(0, l.Decks[atriumDeck].Y, 0); ok && l.Decks[atriumDeck].Contains(0, 0) {
		t.Error("atrium should be a hole")
	}
	if got := l.DeckAt(0, l.Decks[atriumDeck].Y, 0); got != atriumDeck-1 {
		t.Errorf("falling through the atrium should land on deck %d, got %d", atriumDeck-1, got)
	}
	// Mid-flight the floor is the ramp, not the deck below.
	r := l.Ramps[0][0]
	h, deck, ok := l.FloorAt(r.X, l.Decks[0].Y+1.6, r.Z)
	if !ok || deck != 0 || math.Abs(h-(l.Decks[0].Y+deckSpacing/2)) > 1e-9 {
		t.Errorf("mid-flight floor %.3f on deck %d (ok=%v)", h, deck, ok)
	}
	h, _, _ = l.FloorAt(r.X, l.Decks[0].Y, r.Z)
	if h != l.Decks[0].Y {
		t.Errorf("a walker at the foot of the flight stands on the deck, got %.3f", h)
	}
}

func TestShipLocalVersusWorldWhileRolling(t *testing.T) {
	l := StandardLayout(Megaship())
	pose := Pose{X: 1234, Y: 0.8, Z: -987, Heading: 1.1, Pitch: 0.04, Roll: 0.21} // 12 degrees of roll
	misread := 0
	for i := range l.Decks {
		for _, p := range []Vec2{{-18, 80}, {16, -60}, {-5, 140}, {10, 0}} {
			if !l.Decks[i].Contains(p.X, p.Z) {
				continue
			}
			local := Vec3{p.X, l.Decks[i].Y, p.Z}
			w := pose.ToWorld(local)
			back := pose.ToLocal(w)
			if math.Abs(back.X-local.X)+math.Abs(back.Y-local.Y)+math.Abs(back.Z-local.Z) > 1e-9 {
				t.Fatalf("round trip moved %v to %v", local, back)
			}
			if got := l.DeckAtWorld(pose, w); got != i {
				t.Errorf("deck %d point read as deck %d while rolling", i, got)
			}
			// Feet slightly above the deck still belong to it.
			feet := pose.ToWorld(Vec3{p.X, l.Decks[i].Y + 0.2, p.Z})
			if got := l.DeckAtWorld(pose, feet); got != i {
				t.Errorf("deck %d walker read as deck %d while rolling", i, got)
			}
			// A reader that ignores the tilt and compares world height with deck height is wrong on high decks.
			if naive := l.DeckAt(w.X-pose.X, w.Y-pose.Y, w.Z-pose.Z); naive != i {
				misread++
			}
		}
	}
	if misread == 0 {
		t.Error("the tilt should be large enough that a tilt-blind reader misreads some decks")
	}
	// The deck is a plane in world space: heights across it differ by roll.
	a, b := pose.ToWorld(Vec3{-15, 30, 0}), pose.ToWorld(Vec3{15, 30, 0})
	if d := math.Abs(a.Y - b.Y); d < 5 {
		t.Errorf("30 m across a rolled deck should change height by metres, got %.2f", d)
	}
}

func TestRoutes(t *testing.T) {
	l := StandardLayout(Liner())
	same, ok := l.Route(3, -10, 60, 3, -10, 90, RouteOptions{})
	if !ok || len(same.Hops) != 0 || math.Abs(same.Time-30/1.2) > 1e-9 {
		t.Errorf("same-deck route %+v ok=%v", same, ok)
	}
	// Two decks apart: the quickest way near a stair tower is the stairs.
	r, ok := l.Route(2, 0, -86, 4, 0, -80, RouteOptions{})
	if !ok || len(r.Hops) != 1 || l.Portals[r.Hops[0].Portal].Kind != Stairs {
		t.Fatalf("route near stairs: %+v ok=%v", r, ok)
	}
	if r.Hops[0].From != 2 || r.Hops[0].To != 4 || r.Hops[0].Ride != 2*stairPerLevel {
		t.Errorf("hop %+v", r.Hops[0])
	}
	// Across the whole ship, the lift wins: 13 levels by lift is 14+3*13 s.
	far, ok := l.Route(0, 0, -86, 13, 0, 86, RouteOptions{})
	if !ok {
		t.Fatal("no route to the top deck")
	}
	var rode float64
	for _, h := range far.Hops {
		rode += h.Ride
		if h.To <= h.From {
			t.Errorf("route descends: %+v", h)
		}
	}
	if len(far.Hops) < 1 || l.Portals[far.Hops[0].Portal].Kind != Elevator {
		t.Errorf("a 13-level trip should use a lift: %+v", far)
	}
	// Close every lift: the route falls back on chained stairs.
	noLift, ok := l.Route(0, 0, -86, 13, 0, 86, RouteOptions{Avoid: func(i int) bool { return l.Portals[i].Kind == Elevator }})
	if !ok || len(noLift.Hops) < 4 || noLift.Time <= far.Time {
		t.Errorf("stairs-only route: %d hops, %.0f s vs %.0f s", len(noLift.Hops), noLift.Time, far.Time)
	}
	for _, h := range noLift.Hops {
		if l.Portals[h.Portal].Kind != Stairs || h.To-h.From > 4 {
			t.Errorf("bad stairs hop %+v", h)
		}
	}
	// A long queue at every lift pushes a short trip to the stairs.
	flow := l.NewFlow()
	for i := range l.Portals {
		if l.Portals[i].Kind == Elevator {
			flow.FreeAt[i] = 600
		}
	}
	queued, _ := l.Route(5, -14, 0, 7, -14, 20, RouteOptions{Flow: flow})
	free, _ := l.Route(5, -14, 0, 7, -14, 20, RouteOptions{})
	if l.Portals[free.Hops[0].Portal].Kind != Elevator || l.Portals[queued.Hops[0].Portal].Kind != Stairs {
		t.Errorf("free route should use the lift beside it and the queued one the stairs: %v / %v", free.Hops, queued.Hops)
	}
	if _, ok := l.Route(0, 0, 0, 99, 0, 0, RouteOptions{}); ok {
		t.Error("a deck that does not exist has no route")
	}
	if _, ok := l.Route(0, 0, -86, 13, 0, 86, RouteOptions{Avoid: func(int) bool { return true }}); ok {
		t.Error("with every portal closed there is no route between decks")
	}
	again, _ := l.Route(0, 0, -86, 13, 0, 86, RouteOptions{})
	if !reflect.DeepEqual(far, again) {
		t.Error("routing must be repeatable")
	}
	t.Logf("deck 1 to deck 14 across the ship: %d hops, %.0f s by lift; stairs only: %d hops, %.0f s", len(far.Hops), far.Time, len(noLift.Hops), noLift.Time)
}

func TestPortalCapacityAndQueues(t *testing.T) {
	l := StandardLayout(Liner())
	flow := l.NewFlow()
	stairs, lift := 0, 2
	if l.Portals[stairs].Kind != Stairs || l.Portals[lift].Kind != Elevator {
		t.Fatal("unexpected portal order")
	}
	// 14 people at 1.4 persons/s occupy the stairs for 10 s; the last one arrives 2 levels later.
	start, arrive, ok := flow.Admit(&l, stairs, 1, 3, 14, 100)
	if !ok || start != 100 || math.Abs(arrive-(100+10+24)) > 1e-9 {
		t.Errorf("stairs party: start %v arrive %v", start, arrive)
	}
	start2, _, _ := flow.Admit(&l, stairs, 1, 3, 7, 103)
	if math.Abs(start2-110) > 1e-9 || math.Abs(flow.Wait(stairs, 103)-12) > 1e-9 {
		t.Errorf("a second party should queue behind the first: starts at %v", start2)
	}
	// 45 people need three 20-person cars, each round trip 2*(14... ) apart.
	trip := l.Portals[lift].TripTime(0, 5)
	s, a, ok := flow.Admit(&l, lift, 0, 5, 45, 0)
	if !ok || s != 0 || math.Abs(a-(2*2*trip+trip)) > 1e-9 {
		t.Errorf("lift party: start %v arrive %v, want arrive %v", s, a, 5*trip)
	}
	if _, _, ok := flow.Admit(&l, stairs, 1, 9, 1, 0); ok {
		t.Error("stairs are limited to four levels per trip")
	}
	if _, _, ok := flow.Admit(&l, stairs, 1, 1, 1, 0); ok {
		t.Error("a trip needs two different decks")
	}
	if got, _, ok := l.NearestPortal(4, -13, 0); !ok || l.Portals[got].ID != "lifts-mid" {
		t.Errorf("nearest portal to the mid lifts is %v", got)
	}
}

func TestLayoutIsRepeatable(t *testing.T) {
	a, b := StandardLayout(Megaship()), StandardLayout(Megaship())
	if !reflect.DeepEqual(a, b) {
		t.Error("the same class must build the same layout")
	}
	if len(a.Decks) != 20 {
		t.Errorf("megaship has %d decks", len(a.Decks))
	}
}
