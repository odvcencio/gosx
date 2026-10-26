package vessel

import "m31labs.dev/gosx/det"

// Ship-local axes match the pose: X is starboard, Y is up from the waterline
// origin, and Z points to the stern (the bow is toward -Z). A deck is a flat
// walking surface at height Y made of walkable polygons, minus holes.

// Vec2 is a point on a deck, in ship-local metres.
type Vec2 struct{ X, Z float64 }

// Vec3 is a ship-local or world point in metres.
type Vec3 struct{ X, Y, Z float64 }

// Polygon is a closed outline. The last point connects to the first. It may be
// convex or concave, in either winding.
type Polygon []Vec2

// Rect is an axis-aligned rectangle in ship-local metres, centred on (X, Z).
type Rect struct{ X, Z, SizeX, SizeZ float64 }

// Polygon returns the rectangle as a four-point polygon.
func (r Rect) Polygon() Polygon {
	hx, hz := float64(0.5*r.SizeX), float64(0.5*r.SizeZ)
	return Polygon{{r.X - hx, r.Z - hz}, {r.X + hx, r.Z - hz}, {r.X + hx, r.Z + hz}, {r.X - hx, r.Z + hz}}
}

// Contains reports whether the point is inside the polygon (even-odd rule).
func (p Polygon) Contains(x, z float64) bool {
	in := false
	for i, j := 0, len(p)-1; i < len(p); j, i = i, i+1 {
		a, b := p[i], p[j]
		if (a.Z > z) != (b.Z > z) {
			cross := float64(float64(b.X-a.X)*(z-a.Z))/(b.Z-a.Z) + a.X
			if x < cross {
				in = !in
			}
		}
	}
	return in
}

// Bounds returns the polygon's bounding box as min and max corners.
func (p Polygon) Bounds() (lo, hi Vec2) {
	if len(p) == 0 {
		return
	}
	lo, hi = p[0], p[0]
	for _, v := range p[1:] {
		lo.X, lo.Z = det.Min(lo.X, v.X), det.Min(lo.Z, v.Z)
		hi.X, hi.Z = det.Max(hi.X, v.X), det.Max(hi.Z, v.Z)
	}
	return
}

// Deck is one walking level. A point is on the deck when it lies in at least
// one Walk polygon and in no Hole.
type Deck struct {
	Index int // position in Layout.Decks, lowest deck first
	Name  string
	Y     float64 // surface height above the waterline origin, metres
	Walk  []Polygon
	Holes []Polygon
}

// Contains reports whether (x, z) is walkable on the deck.
func (d *Deck) Contains(x, z float64) bool {
	for _, h := range d.Holes {
		if h.Contains(x, z) {
			return false
		}
	}
	for _, w := range d.Walk {
		if w.Contains(x, z) {
			return true
		}
	}
	return false
}

// Ramp is a sloped walkable rectangle, used for stair flights. Its height rises
// linearly along Z from FromY at the low-Z edge to ToY at the high-Z edge (swap
// the two to descend toward +Z).
type Ramp struct {
	Rect
	FromY, ToY float64
}

// HeightAt returns the ramp height at (x, z) and whether the point is on it.
func (r Ramp) HeightAt(x, z float64) (float64, bool) {
	if det.Abs(x-r.X) > float64(0.5*r.SizeX) || det.Abs(z-r.Z) > float64(0.5*r.SizeZ) {
		return 0, false
	}
	t := float64(z-(r.Z-float64(0.5*r.SizeZ))) / r.SizeZ
	return r.FromY + float64(t*(r.ToY-r.FromY)), true
}

// StepHeight is how far above a walker's feet a surface may be and still count
// as the walker's deck. It matches the Scene3D walker's default step.
const StepHeight = 0.5

// PortalKind names how a portal moves people.
type PortalKind int

const (
	// Stairs move people on foot at a rate set by their width. Anyone may use
	// them at once; they queue only when the flow rate is exceeded.
	Stairs PortalKind = iota
	// Elevator moves a car of people at a time between its stops.
	Elevator
)

func (k PortalKind) String() string {
	if k == Elevator {
		return "elevator"
	}
	return "stairs"
}

// Portal is a vertical connection between decks at one (X, Z). People board on
// one stop and leave on another. Stops lists the deck indices it serves.
//
// Cost model, in seconds, for travelling n levels:
//
//	stairs:   PerLevel * n
//	elevator: Dwell + PerLevel * n
//
// Capacity is persons per second for stairs, and persons per car for elevators.
type Portal struct {
	ID        string
	Kind      PortalKind
	X, Z      float64
	Stops     []int   // deck indices, ascending
	Capacity  float64 // stairs: persons/s; elevator: persons per car
	PerLevel  float64 // seconds per level
	Dwell     float64 // elevator: seconds to load and unload one car
	MaxLevels int     // stairs: most levels one trip may span; 0 means any
	Reach     float64 // metres from (X, Z) within which a walker may board; 0 means 1.5
}

// Serves reports whether the portal has a stop on the deck.
func (p *Portal) Serves(deck int) bool {
	for _, s := range p.Stops {
		if s == deck {
			return true
		}
	}
	return false
}

// TripTime returns the seconds to travel between two stops with no queue.
func (p *Portal) TripTime(from, to int) float64 {
	n := float64(absInt(to - from))
	t := float64(p.PerLevel * n)
	if p.Kind == Elevator {
		t += p.Dwell
	}
	return t
}

// CanTravel reports whether the portal links the two decks.
func (p *Portal) CanTravel(from, to int) bool {
	if from == to || !p.Serves(from) || !p.Serves(to) {
		return false
	}
	return p.Kind == Elevator || p.MaxLevels == 0 || absInt(to-from) <= p.MaxLevels
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Layout is a ship's decks and the portals between them.
type Layout struct {
	Decks   []Deck
	Portals []Portal
	Ramps   [][]Ramp // Ramps[i] are the stair flights that start on deck i
}

// DeckAt returns the deck a ship-local point is standing on: the highest deck
// whose surface is at or below y+StepHeight and that has (x, z) walkable. It
// returns -1 when no deck qualifies.
func (l *Layout) DeckAt(x, y, z float64) int {
	for i := len(l.Decks) - 1; i >= 0; i-- {
		d := &l.Decks[i]
		if d.Y <= y+StepHeight && d.Contains(x, z) {
			return i
		}
	}
	return -1
}

// FloorAt returns the ship-local walking height at (x, z) for a walker whose
// feet are at y, and the deck index, or ok=false when nothing is walkable. A
// stair flight counts when it is at or below y+StepHeight and above the deck.
func (l *Layout) FloorAt(x, y, z float64) (height float64, deck int, ok bool) {
	deck = l.DeckAt(x, y, z)
	if deck >= 0 {
		height, ok = l.Decks[deck].Y, true
	} else {
		height, deck = -1e30, -1
	}
	for i := range l.Ramps {
		for _, r := range l.Ramps[i] {
			if h, on := r.HeightAt(x, z); on && h <= y+StepHeight && h > height {
				height, deck, ok = h, i, true
			}
		}
	}
	return
}

// ToWorld converts a ship-local point to world coordinates. It applies roll,
// then pitch, then heading, the same order as the Scene3D vessel.
func (p Pose) ToWorld(l Vec3) Vec3 {
	sr, cr := det.Sin(p.Roll), det.Cos(p.Roll)
	sp, cp := det.Sin(p.Pitch), det.Cos(p.Pitch)
	sy, cy := det.Sin(p.Heading), det.Cos(p.Heading)
	xx := float64(cr*l.X) - float64(sr*l.Y)
	yy := float64(sr*l.X) + float64(cr*l.Y)
	zz := float64(sp*yy) + float64(cp*l.Z)
	y2 := float64(cp*yy) - float64(sp*l.Z)
	return Vec3{
		X: p.X + float64(cy*xx) + float64(sy*zz),
		Y: p.Y + y2,
		Z: p.Z - float64(sy*xx) + float64(cy*zz),
	}
}

// ToLocal converts a world point to ship-local coordinates. It inverts
// [Pose.ToWorld].
func (p Pose) ToLocal(w Vec3) Vec3 {
	sr, cr := det.Sin(p.Roll), det.Cos(p.Roll)
	sp, cp := det.Sin(p.Pitch), det.Cos(p.Pitch)
	sy, cy := det.Sin(p.Heading), det.Cos(p.Heading)
	dx, dy, dz := w.X-p.X, w.Y-p.Y, w.Z-p.Z
	xx := float64(cy*dx) - float64(sy*dz)
	zz := float64(sy*dx) + float64(cy*dz)
	yy := float64(cp*dy) + float64(sp*zz)
	z := float64(cp*zz) - float64(sp*dy)
	return Vec3{X: float64(cr*xx) + float64(sr*yy), Y: float64(cr*yy) - float64(sr*xx), Z: z}
}

// DeckAtWorld returns the deck under a world point while the ship has the given
// pose, or -1. Rolling and pitching do not change the answer for a person who
// stands on a deck, because the point is first moved into ship-local space.
func (l *Layout) DeckAtWorld(p Pose, w Vec3) int {
	v := p.ToLocal(w)
	return l.DeckAt(v.X, v.Y, v.Z)
}

// PortalsAt returns the indices of portals that have a stop on the deck.
func (l *Layout) PortalsAt(deck int) []int {
	var out []int
	for i := range l.Portals {
		if l.Portals[i].Serves(deck) {
			out = append(out, i)
		}
	}
	return out
}

// NearestPortal returns the portal on the deck closest to (x, z), the walking
// distance to it in metres, and ok=false if the deck has none. Ties go to the
// lower index.
func (l *Layout) NearestPortal(deck int, x, z float64) (index int, distance float64, ok bool) {
	best := -1.0
	for _, i := range l.PortalsAt(deck) {
		p := &l.Portals[i]
		d := det.Hypot(p.X-x, p.Z-z)
		if best < 0 || d < best {
			best, index, ok = d, i, true
		}
	}
	return index, det.Max(best, 0), ok
}
