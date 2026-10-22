package scene

import (
	"math"

	"m31labs.dev/gosx/vessel"
)

// VesselDeck is one walkable level of a ship, in the ship's own frame: X toward
// starboard, Y up from the waterline origin, Z toward the stern. A point is on
// the deck when it lies inside any of Polygons and outside every hole. Ramps are
// stair flights that start on this deck and climb to the next.
//
// Decks replace the single rectangular deck that a sailing vessel uses. They
// work with both sailing and ExternalPose vessels. Build them from a
// vessel.Layout with VesselDecksFromLayout, or write them by hand.
type VesselDeck struct {
	Name     string       `json:"name,omitempty"`
	Y        float64      `json:"y"`
	Polygons [][]float64  `json:"polygons"`        // each: x0, z0, x1, z1, ...
	Holes    [][]float64  `json:"holes,omitempty"` // same layout
	Ramps    []VesselRamp `json:"ramps,omitempty"`
}

// VesselRamp is a sloped walkable rectangle in ship-local metres, centred on
// (X, Z). Its height rises linearly along Z from FromY at the low-Z edge to ToY
// at the high-Z edge.
type VesselRamp struct {
	X     float64 `json:"x"`
	Z     float64 `json:"z"`
	SizeX float64 `json:"sizeX"`
	SizeZ float64 `json:"sizeZ"`
	FromY float64 `json:"fromY"`
	ToY   float64 `json:"toY"`
}

// VesselDecksFromLayout converts a deterministic deck layout to wire form.
// Coordinates round to a millimetre, which keeps the JSON small and does not
// change what a walker can reach.
func VesselDecksFromLayout(l vessel.Layout) []VesselDeck {
	mm := func(v float64) float64 { return math.Round(v*1000) / 1000 }
	flat := func(polys []vessel.Polygon) [][]float64 {
		var out [][]float64
		for _, p := range polys {
			pts := make([]float64, 0, 2*len(p))
			for _, v := range p {
				pts = append(pts, mm(v.X), mm(v.Z))
			}
			out = append(out, pts)
		}
		return out
	}
	decks := make([]VesselDeck, len(l.Decks))
	for i, d := range l.Decks {
		decks[i] = VesselDeck{Name: d.Name, Y: mm(d.Y), Polygons: flat(d.Walk), Holes: flat(d.Holes)}
		if i < len(l.Ramps) {
			for _, r := range l.Ramps[i] {
				decks[i].Ramps = append(decks[i].Ramps, VesselRamp{X: mm(r.X), Z: mm(r.Z), SizeX: mm(r.SizeX), SizeZ: mm(r.SizeZ), FromY: mm(r.FromY), ToY: mm(r.ToY)})
			}
		}
	}
	return decks
}
