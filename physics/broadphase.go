package physics

import (
	"math"
	"sort"
)

type ColliderPair struct {
	A *Collider
	B *Collider
}

type SpatialHash struct {
	cellSize float64
	cells    map[cellKey][]*Collider
	skin     float64
}

type cellKey struct {
	x int
	y int
	z int
}

func NewSpatialHash(cellSize float64) *SpatialHash {
	if cellSize <= 0 {
		cellSize = 2
	}
	return &SpatialHash{
		cellSize: cellSize,
		cells:    make(map[cellKey][]*Collider),
		skin:     0.001,
	}
}

func (s *SpatialHash) CandidatePairs(colliders []*Collider) []ColliderPair {
	if s == nil {
		s = NewSpatialHash(2)
	}
	for key := range s.cells {
		delete(s.cells, key)
	}

	var infinite []*Collider
	for _, collider := range colliders {
		if collider == nil {
			continue
		}
		aabb := collider.AABB().Expand(s.skin)
		if !aabb.IsFinite() {
			infinite = append(infinite, collider)
			continue
		}
		minCell, maxCell, ok := s.cellRange(aabb)
		if !ok {
			infinite = append(infinite, collider)
			continue
		}
		for x := minCell.x; x <= maxCell.x; x++ {
			for y := minCell.y; y <= maxCell.y; y++ {
				for z := minCell.z; z <= maxCell.z; z++ {
					key := cellKey{x: x, y: y, z: z}
					s.cells[key] = append(s.cells[key], collider)
				}
			}
		}
	}

	pairMap := make(map[uint64]ColliderPair)
	for _, cellColliders := range s.cells {
		for i := 0; i < len(cellColliders); i++ {
			for j := i + 1; j < len(cellColliders); j++ {
				addCandidatePair(pairMap, cellColliders[i], cellColliders[j])
			}
		}
	}
	for _, inf := range infinite {
		for _, collider := range colliders {
			if inf == collider {
				continue
			}
			addCandidatePair(pairMap, inf, collider)
		}
	}

	pairs := make([]ColliderPair, 0, len(pairMap))
	for _, pair := range pairMap {
		pairs = append(pairs, pair)
	}
	sort.Slice(pairs, func(i, j int) bool {
		ai, bi := orderedColliderIndexes(pairs[i].A, pairs[i].B)
		aj, bj := orderedColliderIndexes(pairs[j].A, pairs[j].B)
		if ai == aj {
			return bi < bj
		}
		return ai < aj
	})
	return pairs
}

func (s *SpatialHash) cellRange(aabb AABB) (cellKey, cellKey, bool) {
	minCell := cellKey{
		x: int(math.Floor(aabb.Min.X / s.cellSize)),
		y: int(math.Floor(aabb.Min.Y / s.cellSize)),
		z: int(math.Floor(aabb.Min.Z / s.cellSize)),
	}
	maxCell := cellKey{
		x: int(math.Floor(aabb.Max.X / s.cellSize)),
		y: int(math.Floor(aabb.Max.Y / s.cellSize)),
		z: int(math.Floor(aabb.Max.Z / s.cellSize)),
	}

	const maxCellsPerAxis = 128
	if maxCell.x-minCell.x > maxCellsPerAxis ||
		maxCell.y-minCell.y > maxCellsPerAxis ||
		maxCell.z-minCell.z > maxCellsPerAxis {
		return cellKey{}, cellKey{}, false
	}
	return minCell, maxCell, true
}

func addCandidatePair(pairs map[uint64]ColliderPair, a, b *Collider) {
	if !validCandidatePair(a, b) {
		return
	}
	if b.index < a.index {
		a, b = b, a
	}
	pairs[pairKey(a, b)] = ColliderPair{A: a, B: b}
}

func validCandidatePair(a, b *Collider) bool {
	if a == nil || b == nil || a == b {
		return false
	}
	if a.Body != nil && a.Body == b.Body {
		return false
	}
	if immovableCollider(a) && immovableCollider(b) {
		return false
	}
	aabbA := a.AABB()
	aabbB := b.AABB()
	return !aabbA.IsFinite() || !aabbB.IsFinite() || aabbA.Overlaps(aabbB)
}

func immovableCollider(c *Collider) bool {
	return c == nil || c.Body == nil || !c.Body.IsDynamic()
}

func pairKey(a, b *Collider) uint64 {
	ai, bi := orderedColliderIndexes(a, b)
	return uint64(uint32(ai))<<32 | uint64(uint32(bi))
}

func orderedColliderIndexes(a, b *Collider) (int, int) {
	ai := 0
	bi := 0
	if a != nil {
		ai = a.index
	}
	if b != nil {
		bi = b.index
	}
	if bi < ai {
		return bi, ai
	}
	return ai, bi
}
