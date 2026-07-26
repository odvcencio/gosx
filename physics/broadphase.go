package physics

import (
	"math"
	"slices"
	"sort"
)

type ColliderPair struct {
	A *Collider
	B *Collider
}

// spatialEntry caches one collider's world bounds for the duration of a step.
// AABB() rotates vectors, so computing it once per step instead of once per
// candidate pair removes most of the broadphase cost.
type spatialEntry struct {
	collider *Collider
	aabb     AABB
	stamp    uint64
}

// SpatialHash is a uniform-grid broadphase. One rebuild per step fills two cell
// maps: cells holds every collider, staticCells holds only the immovable,
// non-trigger colliders that the swept pass needs.
type SpatialHash struct {
	cellSize float64
	skin     float64

	cells       map[cellKey][]int32
	staticCells map[cellKey][]int32
	usedKeys    []cellKey
	staticKeys  []cellKey

	entries        []spatialEntry
	infinite       []int32
	staticInfinite []int32

	pairKeys []uint64
	pairs    []ColliderPair

	// indexOrdered records whether entries are already sorted by collider
	// index. When they are, sorting the packed pair keys also sorts the pairs,
	// so the final comparison sort is skipped.
	indexOrdered bool

	queryGen uint64
	// built counts rebuilds. Callers use it to detect a stale grid.
	built uint64
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
		cellSize:    cellSize,
		cells:       make(map[cellKey][]int32),
		staticCells: make(map[cellKey][]int32),
		skin:        0.001,
	}
}

// Rebuild refills the grid from the given colliders. Call it once per step
// before any query.
func (s *SpatialHash) Rebuild(colliders []*Collider) {
	if s == nil {
		return
	}
	s.reset(len(colliders))

	lastIndex := math.MinInt
	s.indexOrdered = true
	for _, collider := range colliders {
		if collider == nil {
			continue
		}
		aabb := collider.AABB().Expand(s.skin)
		index := int32(len(s.entries))
		s.entries = append(s.entries, spatialEntry{collider: collider, aabb: aabb})
		if collider.index < lastIndex {
			s.indexOrdered = false
		}
		lastIndex = collider.index

		// The swept pass only ever looks at immovable, solid colliders, so the
		// filter happens here, at insert time, and not inside the sweep loop.
		isStatic := immovableCollider(collider) && !collider.IsTrigger

		minCell, maxCell, ok := s.cellRange(aabb)
		if !aabb.IsFinite() || !ok {
			s.infinite = append(s.infinite, index)
			if isStatic {
				s.staticInfinite = append(s.staticInfinite, index)
			}
			continue
		}
		for x := minCell.x; x <= maxCell.x; x++ {
			for y := minCell.y; y <= maxCell.y; y++ {
				for z := minCell.z; z <= maxCell.z; z++ {
					key := cellKey{x: x, y: y, z: z}
					s.insert(s.cells, &s.usedKeys, key, index)
					if isStatic {
						s.insert(s.staticCells, &s.staticKeys, key, index)
					}
				}
			}
		}
	}
	s.built++
}

// Built reports how many times the grid was rebuilt. A caller compares it
// against its own step counter to notice a stale grid.
func (s *SpatialHash) Built() uint64 {
	if s == nil {
		return 0
	}
	return s.built
}

// reset empties the grid but keeps the per-cell slices, so a steady-state
// simulation stops allocating after the first few steps.
func (s *SpatialHash) reset(colliderCount int) {
	// A world whose bodies travel far leaves empty cells behind. Rebuild the
	// maps when the bookkeeping outgrows the live data.
	limit := 8*colliderCount + 4096
	if len(s.cells) > limit {
		s.cells = make(map[cellKey][]int32, len(s.usedKeys))
	} else {
		for _, key := range s.usedKeys {
			s.cells[key] = s.cells[key][:0]
		}
	}
	if len(s.staticCells) > limit {
		s.staticCells = make(map[cellKey][]int32, len(s.staticKeys))
	} else {
		for _, key := range s.staticKeys {
			s.staticCells[key] = s.staticCells[key][:0]
		}
	}
	s.usedKeys = s.usedKeys[:0]
	s.staticKeys = s.staticKeys[:0]
	s.entries = s.entries[:0]
	s.infinite = s.infinite[:0]
	s.staticInfinite = s.staticInfinite[:0]
}

func (s *SpatialHash) insert(cells map[cellKey][]int32, keys *[]cellKey, key cellKey, index int32) {
	bucket, exists := cells[key]
	if !exists || len(bucket) == 0 {
		*keys = append(*keys, key)
	}
	cells[key] = append(bucket, index)
}

// CandidatePairs rebuilds the grid and returns the overlapping pairs, ordered
// by collider index. The returned slice is owned by the hash and is reused by
// the next call.
func (s *SpatialHash) CandidatePairs(colliders []*Collider) []ColliderPair {
	if s == nil {
		return nil
	}
	s.Rebuild(colliders)
	return s.pairsFromGrid()
}

func (s *SpatialHash) pairsFromGrid() []ColliderPair {
	s.pairKeys = s.pairKeys[:0]
	for _, key := range s.usedKeys {
		bucket := s.cells[key]
		for i := 0; i < len(bucket); i++ {
			for j := i + 1; j < len(bucket); j++ {
				s.appendPairKey(bucket[i], bucket[j])
			}
		}
	}
	for _, index := range s.infinite {
		for other := range s.entries {
			s.appendPairKey(index, int32(other))
		}
	}

	// A packed key is (lowEntry << 32 | highEntry), so a numeric sort followed
	// by a single dedupe pass replaces the old map plus comparison sort.
	slices.Sort(s.pairKeys)
	s.pairs = s.pairs[:0]
	for i, key := range s.pairKeys {
		if i > 0 && key == s.pairKeys[i-1] {
			continue
		}
		a := s.entries[key>>32].collider
		b := s.entries[key&0xffffffff].collider
		if b.index < a.index {
			a, b = b, a
		}
		s.pairs = append(s.pairs, ColliderPair{A: a, B: b})
	}
	if !s.indexOrdered {
		sort.Slice(s.pairs, func(i, j int) bool {
			ai, bi := orderedColliderIndexes(s.pairs[i].A, s.pairs[i].B)
			aj, bj := orderedColliderIndexes(s.pairs[j].A, s.pairs[j].B)
			if ai == aj {
				return bi < bj
			}
			return ai < aj
		})
	}
	return s.pairs
}

func (s *SpatialHash) appendPairKey(i, j int32) {
	if i == j {
		return
	}
	if j < i {
		i, j = j, i
	}
	if !validEntryPair(s.entries[i], s.entries[j]) {
		return
	}
	s.pairKeys = append(s.pairKeys, uint64(uint32(i))<<32|uint64(uint32(j)))
}

// QueryAABB appends every collider whose cached bounds overlap box. Rebuild
// must have run for the current step.
func (s *SpatialHash) QueryAABB(box AABB, out []*Collider) []*Collider {
	return s.query(s.cells, s.infinite, box, out)
}

// QueryStaticAABB appends every immovable, non-trigger collider whose cached
// bounds overlap box.
func (s *SpatialHash) QueryStaticAABB(box AABB, out []*Collider) []*Collider {
	return s.query(s.staticCells, s.staticInfinite, box, out)
}

func (s *SpatialHash) query(cells map[cellKey][]int32, unbounded []int32, box AABB, out []*Collider) []*Collider {
	if s == nil || len(s.entries) == 0 {
		return out
	}
	s.queryGen++
	generation := s.queryGen

	minCell, maxCell, ok := s.cellRange(box)
	if !box.IsFinite() || !ok {
		// The query box spans too many cells to walk. Fall back to a linear
		// scan, which is still one pass instead of one pass per cell.
		for i := range s.entries {
			out = s.take(int32(i), generation, box, out, false)
		}
		return out
	}
	for x := minCell.x; x <= maxCell.x; x++ {
		for y := minCell.y; y <= maxCell.y; y++ {
			for z := minCell.z; z <= maxCell.z; z++ {
				for _, index := range cells[cellKey{x: x, y: y, z: z}] {
					out = s.take(index, generation, box, out, true)
				}
			}
		}
	}
	for _, index := range unbounded {
		out = s.take(index, generation, box, out, false)
	}
	return out
}

// take appends one entry to out unless this query already reported it. The
// stamp lives on the entry, so deduplication needs no map.
func (s *SpatialHash) take(index int32, generation uint64, box AABB, out []*Collider, checkBounds bool) []*Collider {
	entry := &s.entries[index]
	if entry.stamp == generation {
		return out
	}
	entry.stamp = generation
	if checkBounds && !entry.aabb.Overlaps(box) {
		return out
	}
	return append(out, entry.collider)
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

func validEntryPair(a, b spatialEntry) bool {
	if a.collider == nil || b.collider == nil || a.collider == b.collider {
		return false
	}
	if a.collider.Body != nil && a.collider.Body == b.collider.Body {
		return false
	}
	if immovableCollider(a.collider) && immovableCollider(b.collider) {
		return false
	}
	return !a.aabb.IsFinite() || !b.aabb.IsFinite() || a.aabb.Overlaps(b.aabb)
}

func immovableCollider(c *Collider) bool {
	return c == nil || c.Body == nil || !c.Body.IsDynamic()
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
