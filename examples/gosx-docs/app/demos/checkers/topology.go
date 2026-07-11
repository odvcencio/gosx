// Package checkers implements deterministic Chinese Checkers rules without
// depending on rendering, browser, or policy packages.
package checkers

import "sort"

const (
	HoleCount = 121
	CampCount = 6
	CampSize  = 10
	NoHole    = Hole(0xff)
)

type Hole uint8

// Cube is a hex-grid coordinate. X+Y+Z is always zero.
type Cube struct{ X, Y, Z int8 }

type Topology struct {
	Coords    [HoleCount]Cube
	Neighbors [HoleCount][6]Hole
	Jumps     [HoleCount][6]Hole
	Middles   [HoleCount][6]Hole
	Camps     [CampCount][CampSize]Hole
	campOf    [HoleCount]int8
	index     map[Cube]Hole
}

var Standard = newTopology()

var directions = [6]Cube{{1, -1, 0}, {1, 0, -1}, {0, 1, -1}, {-1, 1, 0}, {-1, 0, 1}, {0, -1, 1}}

func newTopology() *Topology {
	t := &Topology{index: make(map[Cube]Hole, HoleCount)}
	for i := range t.campOf {
		t.campOf[i] = -1
	}
	coords := make([]Cube, 0, HoleCount)
	for x := -4; x <= 4; x++ {
		for y := -4; y <= 4; y++ {
			z := -x - y
			if z >= -4 && z <= 4 {
				coords = append(coords, Cube{int8(x), int8(y), int8(z)})
			}
		}
	}
	base := make([]Cube, 0, CampSize)
	for x := 5; x <= 8; x++ {
		for y := -4; y <= x-9; y++ {
			base = append(base, Cube{int8(x), int8(y), int8(-x - y)})
		}
	}
	for camp := 0; camp < CampCount; camp++ {
		for _, c := range base {
			coords = append(coords, rotate(c, camp))
		}
	}
	sort.Slice(coords, func(i, j int) bool {
		if coords[i].Y != coords[j].Y {
			return coords[i].Y < coords[j].Y
		}
		return coords[i].X < coords[j].X
	})
	for i, c := range coords {
		t.Coords[i], t.index[c] = c, Hole(i)
	}
	for camp := 0; camp < CampCount; camp++ {
		campCoords := make([]Cube, len(base))
		for i, c := range base {
			campCoords[i] = rotate(c, camp)
		}
		sort.Slice(campCoords, func(i, j int) bool {
			if campCoords[i].Y != campCoords[j].Y {
				return campCoords[i].Y < campCoords[j].Y
			}
			return campCoords[i].X < campCoords[j].X
		})
		for i, c := range campCoords {
			h := t.index[c]
			t.Camps[camp][i] = h
			t.campOf[h] = int8(camp)
		}
	}
	for i, c := range t.Coords {
		for d, delta := range directions {
			t.Neighbors[i][d] = t.lookup(add(c, delta))
			t.Middles[i][d] = t.Neighbors[i][d]
			t.Jumps[i][d] = t.lookup(add(c, scale(delta, 2)))
			if t.Middles[i][d] == NoHole {
				t.Jumps[i][d] = NoHole
			}
		}
	}
	return t
}

func (t *Topology) HoleAt(c Cube) (Hole, bool) { h, ok := t.index[c]; return h, ok }
func (t *Topology) Camp(h Hole) int {
	if int(h) >= HoleCount {
		return -1
	}
	return int(t.campOf[h])
}
func OppositeCamp(c int) int {
	if c < 0 || c >= CampCount {
		return -1
	}
	return (c + 3) % CampCount
}

func (t *Topology) lookup(c Cube) Hole {
	if h, ok := t.index[c]; ok {
		return h
	}
	return NoHole
}
func add(a, b Cube) Cube        { return Cube{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func scale(a Cube, n int8) Cube { return Cube{a.X * n, a.Y * n, a.Z * n} }
func rotate(c Cube, n int) Cube {
	for ; n > 0; n-- {
		c = Cube{-c.Z, -c.X, -c.Y}
	}
	return c
}
