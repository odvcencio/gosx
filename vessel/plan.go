package vessel

import "m31labs.dev/gosx/det"

// DeckPlan describes one deck in a few numbers. Build turns it into a hull-shaped
// walkable outline, so a 20-deck ship takes 20 short descriptions, not 20 hand-
// drawn polygons.
//
// The outline runs from the bow (-Z) to the stern (+Z). The bow narrows along
// its first BowFraction of Length in a parabolic curve to a stem of
// BowTip*Beam. The stern rounds over its last SternFraction of Length to
// 0.7*Beam at the transom. Inset pulls the outline in from the rail.
type DeckPlan struct {
	Name          string
	Y             float64 // walking surface height above the waterline origin
	Length, Beam  float64 // outline before Inset
	BowFraction   float64 // default 0.22
	BowTip        float64 // stem width as a fraction of Beam; default 0.08
	SternFraction float64 // default 0.08
	Inset         float64 // metres kept clear at the rail; default 0
	CenterZ       float64 // shift of the outline along Z
	Voids         []Rect  // openings such as an atrium or pool well
	Stations      int     // points per side; default 16
}

// Outline returns the deck outline polygon.
func (p DeckPlan) Outline() Polygon {
	n := p.Stations
	if n < 3 {
		n = 16
	}
	bowF, tip, sternF := p.BowFraction, p.BowTip, p.SternFraction
	if bowF <= 0 {
		bowF = 0.22
	}
	if tip <= 0 {
		tip = 0.08
	}
	if sternF <= 0 {
		sternF = 0.08
	}
	half := func(t float64) float64 { // t in [0,1] from bow to stern
		w := 1.0
		if t < bowF {
			u := t / bowF
			w = tip + float64((1-tip)*(1-float64((1-u)*(1-u))))
		} else if t > 1-sternF {
			v := (1 - t) / sternF
			w = 1 - float64(0.3*float64((1-v)*(1-v)))
		}
		return det.Max(float64(float64(0.5*p.Beam)*w)-p.Inset, 0)
	}
	z0 := float64(-0.5*p.Length) + p.Inset
	span := p.Length - float64(2*p.Inset)
	out := make(Polygon, 0, 2*(n+1))
	for i := 0; i <= n; i++ { // starboard side, bow to stern
		t := float64(i) / float64(n)
		out = append(out, Vec2{half(t), p.CenterZ + z0 + float64(t*span)})
	}
	for i := n; i >= 0; i-- { // port side, stern to bow
		t := float64(i) / float64(n)
		out = append(out, Vec2{-half(t), p.CenterZ + z0 + float64(t*span)})
	}
	return out
}

// Build returns the deck described by the plan. Index is set by the caller.
func (p DeckPlan) Build() Deck {
	d := Deck{Name: p.Name, Y: p.Y, Walk: []Polygon{p.Outline()}}
	for _, v := range p.Voids {
		d.Holes = append(d.Holes, v.Polygon())
	}
	return d
}

// Layout design constants for [StandardLayout], in metres and seconds.
const (
	firstDeckY    = 1.5 // lowest walking surface above the waterline origin
	deckSpacing   = 3.3 // height between decks
	flightLength  = 6.0 // plan length of one stair flight
	flightWidth   = 2.4
	stairPerLevel = 12.0 // seconds per level on stairs
	liftPerLevel  = 3.0
	liftDwell     = 14.0
	liftCapacity  = 20.0
	stairFlow     = 1.4 // persons per second per stair
)

// StandardLayout builds a plausible layout for a hull class from its length,
// beam and deck count: one deck per level, hull-shaped, narrower at the bottom
// and top; an atrium opening through the middle decks; two stair towers (fore
// and aft) with a switchback flight for each level; and three elevator banks
// serving every deck.
//
// The layout depends only on the class, so every platform builds the same one.
func StandardLayout(c HullClass) Layout {
	n := c.Decks
	if n < 1 {
		n = 1
	}
	l := Layout{Ramps: make([][]Ramp, n)}
	atriumLo, atriumHi := n/3, 2*n/3 // decks [atriumLo, atriumHi) have the atrium
	atrium := Rect{X: 0, Z: 0, SizeX: float64(0.22 * c.Beam), SizeZ: float64(0.1 * c.Length)}
	towers := []float64{float64(-0.3 * c.Length), float64(0.3 * c.Length)}
	for i := 0; i < n; i++ {
		// Lower decks sit in the narrowing hull; the top decks are superstructure.
		lengthF, beamF := 1.0, 1.0
		if i == 0 {
			lengthF, beamF = 0.86, 0.82
		} else if i == 1 {
			lengthF, beamF = 0.94, 0.92
		}
		if top := n - 1 - i; top < 4 {
			lengthF = 0.96 - float64(0.07*float64(3-top))
			beamF = 0.97 - float64(0.05*float64(3-top))
		}
		plan := DeckPlan{
			Name: deckName(i, n), Y: deckY(i),
			Length: float64(c.Length * lengthF), Beam: float64(c.Beam * beamF), Inset: 0.6,
		}
		if i >= atriumLo && i < atriumHi {
			plan.Voids = append(plan.Voids, atrium)
		}
		// Each flight arrives on deck i+1 through a hole in it.
		if i > 0 {
			for _, tz := range towers {
				prev := flight(i-1, tz, deckY(i-1), plan.Y)
				plan.Voids = append(plan.Voids, prev.Rect)
			}
		}
		d := plan.Build()
		d.Index = i
		l.Decks = append(l.Decks, d)
		if i < n-1 {
			for _, tz := range towers {
				l.Ramps[i] = append(l.Ramps[i], flight(i, tz, plan.Y, deckY(i+1)))
			}
		}
	}
	all := make([]int, n)
	for i := range all {
		all[i] = i
	}
	for k, tz := range towers {
		l.Portals = append(l.Portals, Portal{
			ID: "stairs-" + []string{"fore", "aft"}[k], Kind: Stairs, X: 0, Z: tz, Stops: all,
			Capacity: stairFlow, PerLevel: stairPerLevel, MaxLevels: 4, Reach: 2,
		})
	}
	for k, tz := range []float64{float64(-0.2 * c.Length), 0, float64(0.2 * c.Length)} {
		l.Portals = append(l.Portals, Portal{
			ID: "lifts-" + []string{"fore", "mid", "aft"}[k], Kind: Elevator, X: float64(-0.3 * c.Beam), Z: tz, Stops: all,
			Capacity: liftCapacity, PerLevel: liftPerLevel, Dwell: liftDwell, Reach: 2,
		})
	}
	return l
}

// deckY is the walking height of deck i.
func deckY(i int) float64 { return firstDeckY + float64(float64(i)*deckSpacing) }

// stairX is the lateral centre of the flight for level i: flights alternate
// sides so the hole for one is never under the start of the next.
func stairX(level int) float64 {
	if level%2 == 0 {
		return -1.6
	}
	return 1.6
}

// flight returns the stair flight from deck level i (surface fromY) up to the
// next. It climbs toward +Z on even levels and toward -Z on odd levels, so the
// flights switch back.
func flight(level int, centerZ, fromY, toY float64) Ramp {
	r := Ramp{Rect: Rect{X: stairX(level), Z: centerZ, SizeX: flightWidth, SizeZ: flightLength}, FromY: fromY, ToY: toY}
	if level%2 == 1 {
		r.FromY, r.ToY = toY, fromY
	}
	return r
}

func deckName(i, n int) string {
	return "Deck " + itoa(i+1)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
