package physics

import (
	"math"
	"slices"
)

type ColliderPair struct {
	A *Collider
	B *Collider
}

// spatialEntry caches one collider's world bounds for the duration of a step.
// AABB() rotates vectors, so computing it once per step instead of once per
// candidate pair removes most of the broadphase cost.
//
// touchedBuild and infinite exist only for an entry in staticEntries: they
// let Rebuild tell a slot's collider is new, moved, or gone without touching
// the cell maps when nothing changed. A dynamic entry, rebuilt fresh every
// step, never reads them.
type spatialEntry struct {
	collider *Collider
	aabb     AABB
	stamp    uint64

	touchedBuild uint64
	infinite     bool
}

// SpatialHash is a uniform-grid broadphase.
//
// cells/entries hold the movable (dynamic) census and are cleared and rebuilt
// on every Rebuild call, because a dynamic collider's pose can change every
// step.
//
// staticCells/staticEntries hold the immovable, non-trigger census and
// persist across Rebuild calls instead: a truly static collider's AABB never
// changes, so a slot only costs a cell re-insertion the step its collider is
// new, moves (for example a kinematic body), or leaves the world. A scene
// with thousands of static colliders and a few hundred moving ones then pays
// a broadphase rebuild proportional to the few hundred, not the thousands.
type SpatialHash struct {
	cellSize float64
	skin     float64

	cells    map[cellKey][]int32
	usedKeys []cellKey

	entries  []spatialEntry
	infinite []int32

	staticCells    map[cellKey][]int32
	staticKeys     []cellKey
	staticEntries  []spatialEntry
	staticInfinite []int32
	// staticSlot maps a collider's identity (the *Collider pointer itself,
	// not collider.index) to its slot in staticEntries. The slot is stable
	// for the collider's lifetime, which is what lets staticCells cache cell
	// membership across steps; a positional index into a slice rebuilt every
	// step could not do that, because removing an unrelated collider shifts
	// everything after it.
	//
	// This must not key on collider.index: that field is assigned only by
	// World.registerCollider, so a *Collider built directly with NewCollider
	// and used through this package's public API (NewSpatialHash,
	// CandidatePairs, QueryStaticAABB) without a World keeps index == 0 for
	// every collider. Keying on index would then collapse every standalone
	// static collider onto the same slot, silently dropping all but the
	// last one from the static grid.
	staticSlot map[*Collider]int32
	// staticFree lists slots a removed static collider left behind, so a
	// later static collider reuses the slot instead of growing the array.
	staticFree []int32

	pairs []ColliderPair
	// pairKeys is the raw (pre-dedupe) candidate list, packed as two side IDs
	// (see dynamicSideID/staticSideID) per uint64. Packing lets the hot sort
	// use slices.Sort's primitive fast path instead of a comparator callback
	// over *Collider fields; materializePairs resolves the surviving keys
	// back to colliders and does the (much smaller) collider-index sort the
	// caller-facing order requires.
	pairKeys []uint64

	queryGen uint64
	// built counts rebuilds. Callers use it to detect a stale grid, and
	// Rebuild uses it to stamp which static slots the current pass visited.
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
		staticSlot:  make(map[*Collider]int32),
		skin:        0.001,
	}
}

// Rebuild refills the dynamic grid from the given colliders and syncs the
// persistent static grid against them. Call it once per step before any
// query.
func (s *SpatialHash) Rebuild(colliders []*Collider) {
	if s == nil {
		return
	}
	s.reset(len(colliders))
	s.built++
	build := s.built

	for _, collider := range colliders {
		if collider == nil {
			continue
		}
		aabb := collider.AABB().Expand(s.skin)

		if immovableCollider(collider) && !collider.IsTrigger {
			s.syncStatic(collider, aabb, build)
			continue
		}

		index := int32(len(s.entries))
		s.entries = append(s.entries, spatialEntry{collider: collider, aabb: aabb})

		minCell, maxCell, ok := s.cellRange(aabb)
		if !aabb.IsFinite() || !ok {
			s.infinite = append(s.infinite, index)
			continue
		}
		for x := minCell.x; x <= maxCell.x; x++ {
			for y := minCell.y; y <= maxCell.y; y++ {
				for z := minCell.z; z <= maxCell.z; z++ {
					key := cellKey{x: x, y: y, z: z}
					s.insert(s.cells, &s.usedKeys, key, index)
				}
			}
		}
	}

	s.pruneStatic(build)
}

// Built reports how many times the grid was rebuilt. A caller compares it
// against its own step counter to notice a stale grid.
func (s *SpatialHash) Built() uint64 {
	if s == nil {
		return 0
	}
	return s.built
}

// reset empties the dynamic side of the grid but keeps the per-cell slices,
// so a steady-state simulation stops allocating after the first few steps.
// The static side is not touched here: syncStatic/pruneStatic keep it correct
// incrementally instead of a clear-and-rebuild every step.
func (s *SpatialHash) reset(colliderCount int) {
	// A world whose bodies travel far leaves empty cells behind. Rebuild the
	// map when the bookkeeping outgrows the live data.
	limit := 8*colliderCount + 4096
	if len(s.cells) > limit {
		s.cells = make(map[cellKey][]int32, len(s.usedKeys))
	} else {
		for _, key := range s.usedKeys {
			s.cells[key] = s.cells[key][:0]
		}
	}
	s.usedKeys = s.usedKeys[:0]
	s.entries = s.entries[:0]
	s.infinite = s.infinite[:0]
}

func (s *SpatialHash) insert(cells map[cellKey][]int32, keys *[]cellKey, key cellKey, index int32) {
	bucket, exists := cells[key]
	if !exists || len(bucket) == 0 {
		*keys = append(*keys, key)
	}
	cells[key] = append(bucket, index)
}

// syncStatic keeps one immovable, non-trigger collider's slot in
// staticEntries and its cell membership current. A collider whose AABB has
// not changed since its last visit costs two Vec3 comparisons and nothing
// else; only a new, moved, or reclassified collider pays for a cell
// re-insertion.
func (s *SpatialHash) syncStatic(collider *Collider, aabb AABB, build uint64) {
	slot, known := s.staticSlot[collider]
	if !known {
		slot = s.allocStaticSlot()
		s.staticSlot[collider] = slot
		s.staticEntries[slot] = spatialEntry{collider: collider, aabb: aabb, touchedBuild: build}
		s.insertStatic(slot, aabb)
		return
	}

	entry := &s.staticEntries[slot]
	entry.touchedBuild = build
	if entry.collider == collider && entry.aabb == aabb {
		// Unchanged: skip the cell map entirely. This is the case a static
		// scene hits on every step after the first.
		return
	}
	s.removeStatic(slot, entry.aabb, entry.infinite)
	entry.collider = collider
	entry.aabb = aabb
	s.insertStatic(slot, aabb)
}

func (s *SpatialHash) allocStaticSlot() int32 {
	if n := len(s.staticFree); n > 0 {
		slot := s.staticFree[n-1]
		s.staticFree = s.staticFree[:n-1]
		return slot
	}
	slot := int32(len(s.staticEntries))
	s.staticEntries = append(s.staticEntries, spatialEntry{})
	return slot
}

func (s *SpatialHash) insertStatic(slot int32, aabb AABB) {
	minCell, maxCell, ok := s.cellRange(aabb)
	if !aabb.IsFinite() || !ok {
		s.staticEntries[slot].infinite = true
		s.staticInfinite = append(s.staticInfinite, slot)
		return
	}
	s.staticEntries[slot].infinite = false
	for x := minCell.x; x <= maxCell.x; x++ {
		for y := minCell.y; y <= maxCell.y; y++ {
			for z := minCell.z; z <= maxCell.z; z++ {
				key := cellKey{x: x, y: y, z: z}
				s.insert(s.staticCells, &s.staticKeys, key, slot)
			}
		}
	}
}

func (s *SpatialHash) removeStatic(slot int32, aabb AABB, infinite bool) {
	if infinite {
		s.staticInfinite = removeInt32Value(s.staticInfinite, slot)
		return
	}
	minCell, maxCell, ok := s.cellRange(aabb)
	if !aabb.IsFinite() || !ok {
		return
	}
	for x := minCell.x; x <= maxCell.x; x++ {
		for y := minCell.y; y <= maxCell.y; y++ {
			for z := minCell.z; z <= maxCell.z; z++ {
				key := cellKey{x: x, y: y, z: z}
				if bucket, ok := s.staticCells[key]; ok {
					s.staticCells[key] = removeInt32Value(bucket, slot)
				}
			}
		}
	}
}

func removeInt32Value(list []int32, v int32) []int32 {
	for i, x := range list {
		if x == v {
			list[i] = list[len(list)-1]
			return list[:len(list)-1]
		}
	}
	return list
}

// pruneStatic drops a static collider's slot when this Rebuild did not visit
// it, which means the world removed the collider since the previous step.
// Ranging staticSlot costs time proportional to the live static count but,
// unlike the old clear-and-rebuild, allocates nothing and touches the cell
// maps only for the (normally zero) slots that actually left.
func (s *SpatialHash) pruneStatic(build uint64) {
	if len(s.staticSlot) == 0 {
		return
	}
	for collider, slot := range s.staticSlot {
		entry := &s.staticEntries[slot]
		if entry.touchedBuild == build {
			continue
		}
		s.removeStatic(slot, entry.aabb, entry.infinite)
		*entry = spatialEntry{}
		delete(s.staticSlot, collider)
		s.staticFree = append(s.staticFree, slot)
	}
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

// staticSideTag marks a packed side ID (see appendCandidate) as a slot in
// staticEntries rather than an index into entries. D and S (the dynamic and
// static census sizes) each stay far under 2^31 in practice, so the tag bit
// and the index never collide.
const staticSideTag = uint32(1) << 31

func dynamicSideID(index int32) uint32 { return uint32(index) }
func staticSideID(slot int32) uint32   { return uint32(slot) | staticSideTag }

// resolveSide turns a packed side ID back into the collider it named.
func (s *SpatialHash) resolveSide(id uint32) *Collider {
	if id&staticSideTag != 0 {
		return s.staticEntries[id&^staticSideTag].collider
	}
	return s.entries[id].collider
}

// pairsFromGrid walks the dynamic grid for dynamic-dynamic candidates, crosses
// each occupied dynamic cell against the static grid's same cell for
// dynamic-static candidates, and folds in the unbounded (infinite-AABB)
// colliders on both sides. A static-static candidate is never generated,
// because two immovable colliders can never collide.
//
// Candidates are collected as packed uint64 side-ID keys rather than
// *Collider pairs so the dominant cost, sorting away the duplicate candidates
// a collider spanning several cells produces, uses slices.Sort's primitive
// fast path. Only the much smaller deduped result needs the collider-index
// order the caller sees, so the *Collider-comparator sort runs on that
// instead of on the raw candidate list.
func (s *SpatialHash) pairsFromGrid() []ColliderPair {
	s.pairKeys = s.pairKeys[:0]

	for _, key := range s.usedKeys {
		bucket := s.cells[key]
		statics := s.staticCells[key]
		for i := 0; i < len(bucket); i++ {
			ei := &s.entries[bucket[i]]
			aID := dynamicSideID(bucket[i])
			for j := i + 1; j < len(bucket); j++ {
				ej := &s.entries[bucket[j]]
				s.appendCandidate(aID, ei.collider, ei.aabb, dynamicSideID(bucket[j]), ej.collider, ej.aabb)
			}
			for _, slot := range statics {
				es := &s.staticEntries[slot]
				s.appendCandidate(aID, ei.collider, ei.aabb, staticSideID(slot), es.collider, es.aabb)
			}
		}
	}

	for _, index := range s.infinite {
		ei := &s.entries[index]
		aID := dynamicSideID(index)
		for other := range s.entries {
			eo := &s.entries[other]
			s.appendCandidate(aID, ei.collider, ei.aabb, dynamicSideID(int32(other)), eo.collider, eo.aabb)
		}
		for si := range s.staticEntries {
			es := &s.staticEntries[si]
			if es.collider == nil {
				continue
			}
			s.appendCandidate(aID, ei.collider, ei.aabb, staticSideID(int32(si)), es.collider, es.aabb)
		}
	}
	for _, slot := range s.staticInfinite {
		es := &s.staticEntries[slot]
		aID := staticSideID(slot)
		for other := range s.entries {
			eo := &s.entries[other]
			s.appendCandidate(aID, es.collider, es.aabb, dynamicSideID(int32(other)), eo.collider, eo.aabb)
		}
	}

	slices.Sort(s.pairKeys)
	s.pairs = s.pairs[:0]
	for i, key := range s.pairKeys {
		if i > 0 && key == s.pairKeys[i-1] {
			continue
		}
		a := s.resolveSide(uint32(key >> 32))
		b := s.resolveSide(uint32(key))
		if b.index < a.index {
			a, b = b, a
		}
		s.pairs = append(s.pairs, ColliderPair{A: a, B: b})
	}
	slices.SortFunc(s.pairs, comparePairsByIndex)
	return s.pairs
}

func comparePairsByIndex(x, y ColliderPair) int {
	if x.A.index != y.A.index {
		return x.A.index - y.A.index
	}
	return x.B.index - y.B.index
}

// appendCandidate applies the same admission rules the old single-map
// broadphase used (validEntryPair): reject a nil or self pair, a pair sharing
// a body, a pair where both sides are immovable, and a bounded pair whose
// boxes do not overlap. A surviving pair's packed side IDs, ordered low to
// high, are appended to pairKeys for the sort-and-dedupe pass.
func (s *SpatialHash) appendCandidate(sideA uint32, a *Collider, aabbA AABB, sideB uint32, b *Collider, aabbB AABB) {
	if a == nil || b == nil || a == b {
		return
	}
	if a.Body != nil && a.Body == b.Body {
		return
	}
	if immovableCollider(a) && immovableCollider(b) {
		return
	}
	if aabbA.IsFinite() && aabbB.IsFinite() && !aabbA.Overlaps(aabbB) {
		return
	}
	if sideB < sideA {
		sideA, sideB = sideB, sideA
	}
	s.pairKeys = append(s.pairKeys, uint64(sideA)<<32|uint64(sideB))
}

// QueryAABB appends every collider whose cached bounds overlap box. Rebuild
// must have run for the current step.
func (s *SpatialHash) QueryAABB(box AABB, out []*Collider) []*Collider {
	out = s.query(s.entries, s.cells, s.infinite, box, out)
	return s.query(s.staticEntries, s.staticCells, s.staticInfinite, box, out)
}

// QueryStaticAABB appends every immovable, non-trigger collider whose cached
// bounds overlap box.
func (s *SpatialHash) QueryStaticAABB(box AABB, out []*Collider) []*Collider {
	return s.query(s.staticEntries, s.staticCells, s.staticInfinite, box, out)
}

func (s *SpatialHash) query(entries []spatialEntry, cells map[cellKey][]int32, unbounded []int32, box AABB, out []*Collider) []*Collider {
	if s == nil || len(entries) == 0 {
		return out
	}
	s.queryGen++
	generation := s.queryGen

	minCell, maxCell, ok := s.cellRange(box)
	if !box.IsFinite() || !ok {
		// The query box spans too many cells to walk. Fall back to a linear
		// scan, which is still one pass instead of one pass per cell.
		for i := range entries {
			out = s.take(entries, int32(i), generation, box, out, false)
		}
		return out
	}
	for x := minCell.x; x <= maxCell.x; x++ {
		for y := minCell.y; y <= maxCell.y; y++ {
			for z := minCell.z; z <= maxCell.z; z++ {
				for _, index := range cells[cellKey{x: x, y: y, z: z}] {
					out = s.take(entries, index, generation, box, out, true)
				}
			}
		}
	}
	for _, index := range unbounded {
		out = s.take(entries, index, generation, box, out, false)
	}
	return out
}

// take appends one entry to out unless this query already reported it. The
// stamp lives on the entry, so deduplication needs no map. A nil collider
// marks a static slot a removal tombstoned since the cell map last pointed at
// it; skip it rather than report a stale pointer.
func (s *SpatialHash) take(entries []spatialEntry, index int32, generation uint64, box AABB, out []*Collider, checkBounds bool) []*Collider {
	entry := &entries[index]
	if entry.collider == nil {
		return out
	}
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

func immovableCollider(c *Collider) bool {
	return c == nil || c.Body == nil || !c.Body.IsDynamic()
}

// orderedColliderIndexes returns the two collider indexes in ascending order.
// Tests use it to check that a pair list is sorted.
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
