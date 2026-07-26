package crdt

import (
	"errors"
	"math/bits"
	"slices"
)

// errVisibleRange reports a visible range that falls outside the sequence.
var errVisibleRange = errors.New("visible range out of bounds")

// seq is the index a list or text object keeps over its elements.
//
// Elements live in document order, split across blocks of bounded size. Each
// block counts its visible elements, and a Fenwick tree holds the prefix sums
// over those counts. A lookup by element identity uses a hash map to reach the
// block, then scans inside that one block.
//
// The layout gives these costs for a sequence of n elements held in b blocks:
//
//	identity lookup      one map read plus one block scan
//	visible index of id  one map read, one block scan, log b tree reads
//	element at index     one tree descent over log b, one block scan
//	insert after id      one block memory move, log b tree writes
//	visible length       one field read
//
// No operation copies the whole sequence, and no operation formats an element
// identity into a string. The previous code did both on every edit, which made a
// single keystroke cost time proportional to the document length.
//
// The first block index was a flat prefix array rebuilt by a forward walk. An
// edit at the middle staled every counter after it, so the next query added
// about b/2 counters. At one million elements that was near 3900 additions for
// one keystroke. The Fenwick tree replaces the walk with log b work: 13 tree
// writes for the edit and 13 tree reads for the query. Only a block split still
// costs a full rebuild, and a split happens once per seqBlockTarget inserts into
// the same block.
const (
	// seqBlockTarget is the size a block is split back down to.
	seqBlockTarget = 128
	// seqBlockMax is the size that triggers a split.
	seqBlockMax = 256
)

// seqTree is a Fenwick tree over the visible counts of the blocks.
//
// nodes is one-based. nodes[i] holds the sum of the counts in the range that
// ends at block i-1 and spans i&-i blocks. That layout answers a prefix sum and
// applies a point update in log b steps, and it finds the block that holds a
// visible index by one descent over the same tree.
//
// A structural change to the block list, which means a split or a reload, sets
// dirty. The next query rebuilds the whole tree in b steps. Count changes stay
// incremental.
type seqTree struct {
	nodes []int
	n     int
	dirty bool
}

// build recomputes every node from the block counts. Cost is b.
func (t *seqTree) build(blocks []*seqBlock) {
	t.n = len(blocks)
	if cap(t.nodes) < t.n+1 {
		// Grow with headroom. A split adds one block and rebuilds, so an
		// exact-fit buffer would allocate again at every split.
		t.nodes = make([]int, t.n+1, 2*(t.n+1))
	}
	t.nodes = t.nodes[:t.n+1]
	t.nodes[0] = 0
	for i := range blocks {
		t.nodes[i+1] = blocks[i].visible
	}
	for i := 1; i <= t.n; i++ {
		parent := i + (i & -i)
		if parent <= t.n {
			t.nodes[parent] += t.nodes[i]
		}
	}
	t.dirty = false
}

// ensure rebuilds the tree when a structural change made it stale.
func (t *seqTree) ensure(blocks []*seqBlock) {
	if !t.dirty && t.n == len(blocks) {
		return
	}
	t.build(blocks)
}

// add applies a count change for one block. A stale tree ignores the change,
// because the next rebuild reads the counts again.
func (t *seqTree) add(bi, delta int) {
	if delta == 0 {
		return
	}
	if t.dirty {
		return
	}
	if bi < 0 || bi >= t.n {
		t.dirty = true
		return
	}
	for i := bi + 1; i <= t.n; i += i & -i {
		t.nodes[i] += delta
	}
}

// prefixSum returns the number of visible elements in the blocks before bi.
func (t *seqTree) prefixSum(bi int) int {
	if bi > t.n {
		bi = t.n
	}
	sum := 0
	for i := bi; i > 0; i -= i & -i {
		sum += t.nodes[i]
	}
	return sum
}

// search returns the block that holds the given visible index, and the count of
// visible elements that precede the index inside that block. The caller must
// pass an index below the visible length.
func (t *seqTree) search(index int) (int, int) {
	if t.n == 0 {
		return 0, index
	}
	pos, remaining := 0, index
	for bit := 1 << (bits.Len(uint(t.n)) - 1); bit > 0; bit >>= 1 {
		next := pos + bit
		if next <= t.n && t.nodes[next] <= remaining {
			remaining -= t.nodes[next]
			pos = next
		}
	}
	return pos, remaining
}

type seqBlock struct {
	elems   []listElem
	visible int
	pos     int
}

type seq struct {
	blocks []*seqBlock
	byID   map[OpID]*seqBlock
	total  int
	vis    int
	// tree holds the prefix sums over the block visible counts.
	tree seqTree
}

func newSeq() *seq {
	first := &seqBlock{pos: 0}
	s := &seq{
		blocks: []*seqBlock{first},
		byID:   make(map[OpID]*seqBlock),
	}
	s.tree.build(s.blocks)
	return s
}

// reset loads elems as the document order of the sequence.
func (s *seq) reset(elems []listElem) {
	s.blocks = nil
	s.byID = make(map[OpID]*seqBlock, len(elems))
	s.total = 0
	s.vis = 0
	s.tree.dirty = true

	for start := 0; start < len(elems); start += seqBlockTarget {
		end := min(start+seqBlockTarget, len(elems))
		block := &seqBlock{
			elems: append(make([]listElem, 0, seqBlockMax), elems[start:end]...),
			pos:   len(s.blocks),
		}
		for i := range block.elems {
			if !block.elems[i].Deleted {
				block.visible++
			}
			s.byID[block.elems[i].ID] = block
		}
		s.total += len(block.elems)
		s.vis += block.visible
		s.blocks = append(s.blocks, block)
	}
	if len(s.blocks) == 0 {
		s.blocks = []*seqBlock{{pos: 0}}
	}
}

// length returns the number of elements, visible and tombstoned.
func (s *seq) length() int { return s.total }

// visibleLength returns the number of elements that are not tombstoned.
func (s *seq) visibleLength() int { return s.vis }

// locate returns the block and the offset inside it that hold id.
func (s *seq) locate(id OpID) (*seqBlock, int, bool) {
	block, ok := s.byID[id]
	if !ok {
		return nil, 0, false
	}
	for i := range block.elems {
		if block.elems[i].ID == id {
			return block, i, true
		}
	}
	return nil, 0, false
}

// contains reports whether id belongs to the sequence.
func (s *seq) contains(id OpID) bool {
	_, _, ok := s.locate(id)
	return ok
}

// elemByID returns a copy of the element with the given identity.
func (s *seq) elemByID(id OpID) (listElem, bool) {
	block, off, ok := s.locate(id)
	if !ok {
		return listElem{}, false
	}
	return block.elems[off], true
}

// visibleIndexOf returns the visible index of id. It returns the visible length
// when the element is absent or tombstoned, which matches the older lookup that
// searched a materialized visible list.
func (s *seq) visibleIndexOf(id OpID) int {
	block, off, ok := s.locate(id)
	if !ok || block.elems[off].Deleted {
		return s.vis
	}
	s.tree.ensure(s.blocks)
	index := s.tree.prefixSum(block.pos)
	for i := 0; i < off; i++ {
		if !block.elems[i].Deleted {
			index++
		}
	}
	return index
}

// visibleAt returns a copy of the element at a visible index.
func (s *seq) visibleAt(index int) (listElem, bool) {
	if index < 0 || index >= s.vis {
		return listElem{}, false
	}
	block, off, ok := s.blockAtVisible(index)
	if !ok {
		return listElem{}, false
	}
	return block.elems[off], true
}

// visibleIDAt returns the identity of the element at a visible index.
func (s *seq) visibleIDAt(index int) (OpID, bool) {
	elem, ok := s.visibleAt(index)
	if !ok {
		return OpID{}, false
	}
	return elem.ID, true
}

// blockAtVisible resolves a visible index to a block and an offset.
func (s *seq) blockAtVisible(index int) (*seqBlock, int, bool) {
	s.tree.ensure(s.blocks)
	bi, remaining := s.tree.search(index)
	if bi >= len(s.blocks) {
		return nil, 0, false
	}
	block := s.blocks[bi]
	for i := range block.elems {
		if block.elems[i].Deleted {
			continue
		}
		if remaining == 0 {
			return block, i, true
		}
		remaining--
	}
	return nil, 0, false
}

// collectVisibleRange appends the identities of count visible elements starting
// at a visible index.
func (s *seq) collectVisibleRange(index, count int, out *[]OpID) error {
	if count <= 0 {
		return nil
	}
	if index < 0 || index+count > s.vis {
		return errVisibleRange
	}
	block, off, ok := s.blockAtVisible(index)
	if !ok {
		return errVisibleRange
	}
	bi := block.pos
	for count > 0 {
		block = s.blocks[bi]
		for ; off < len(block.elems) && count > 0; off++ {
			if block.elems[off].Deleted {
				continue
			}
			*out = append(*out, block.elems[off].ID)
			count--
		}
		if count == 0 {
			return nil
		}
		bi++
		off = 0
		if bi >= len(s.blocks) {
			return errVisibleRange
		}
	}
	return nil
}

// insertAfter places elem directly after the element named by after, then steps
// over the concurrent inserts that must precede it.
//
// The rule is the replicated growable array rule: an element goes after its
// reference, and before the first following element whose identity is smaller
// than its own. A child is always created after its parent, so a child identity
// is always larger than the parent identity. Stepping over larger identities
// therefore steps over whole concurrent subtrees and stops at the right place.
func (s *seq) insertAfter(after OpID, hasAfter bool, elem listElem) {
	bi, off := 0, 0
	if hasAfter {
		if block, o, ok := s.locate(after); ok {
			bi, off = block.pos, o+1
		} else {
			// The reference is unknown. Append so no element is lost.
			bi = len(s.blocks) - 1
			off = len(s.blocks[bi].elems)
		}
	}

	for {
		block := s.blocks[bi]
		if off >= len(block.elems) {
			if bi+1 >= len(s.blocks) {
				break
			}
			bi++
			off = 0
			continue
		}
		if block.elems[off].ID.Greater(elem.ID) {
			off++
			continue
		}
		break
	}
	s.insertAt(bi, off, elem)
}

// append adds elem at the end of the document order.
func (s *seq) append(elem listElem) {
	bi := len(s.blocks) - 1
	s.insertAt(bi, len(s.blocks[bi].elems), elem)
}

func (s *seq) insertAt(bi, off int, elem listElem) {
	block := s.blocks[bi]
	if off >= len(block.elems) {
		block.elems = append(block.elems, elem)
	} else {
		block.elems = slices.Insert(block.elems, off, elem)
	}
	if !elem.Deleted {
		block.visible++
		s.vis++
		s.tree.add(bi, 1)
	}
	s.total++
	s.byID[elem.ID] = block
	if len(block.elems) >= seqBlockMax {
		s.split(bi)
	}
}

// split halves an oversized block so that no block scan grows without bound.
func (s *seq) split(bi int) {
	block := s.blocks[bi]
	cut := len(block.elems) / 2
	moved := block.elems[cut:]

	next := &seqBlock{elems: append(make([]listElem, 0, len(moved)+seqBlockTarget), moved...)}
	for i := range next.elems {
		if !next.elems[i].Deleted {
			next.visible++
		}
		s.byID[next.elems[i].ID] = next
	}
	block.elems = block.elems[:cut]
	block.visible -= next.visible

	s.blocks = slices.Insert(s.blocks, bi+1, next)
	for i := bi + 1; i < len(s.blocks); i++ {
		s.blocks[i].pos = i
	}
	// The block list changed shape, so the tree must be built again.
	s.tree.dirty = true
}

// setVisibility flips the tombstone on id when visID wins the comparison against
// the identity that last decided visibility. It returns the stored element.
func (s *seq) setVisibility(id OpID, deleted bool, visID OpID) (listElem, bool) {
	block, off, ok := s.locate(id)
	if !ok {
		return listElem{}, false
	}
	elem := &block.elems[off]
	current := elem.VisibilityID
	if current.Actor == "" {
		current = elem.ID
	}
	if !visID.Greater(current) {
		return *elem, true
	}
	if elem.Deleted != deleted {
		if deleted {
			block.visible--
			s.vis--
			s.tree.add(block.pos, -1)
		} else {
			block.visible++
			s.vis++
			s.tree.add(block.pos, 1)
		}
	}
	elem.Deleted = deleted
	elem.VisibilityID = visID
	return *elem, true
}

// forEach walks the elements in document order. It stops when fn returns false.
func (s *seq) forEach(fn func(elem *listElem) bool) {
	for _, block := range s.blocks {
		for i := range block.elems {
			if !fn(&block.elems[i]) {
				return
			}
		}
	}
}

// elems materializes the document order. Callers that serialize or clone the
// object use it; the edit paths do not.
func (s *seq) elems() []listElem {
	out := make([]listElem, 0, s.total)
	for _, block := range s.blocks {
		out = append(out, block.elems...)
	}
	return out
}

// visibleText concatenates the string values of the visible elements.
func (s *seq) visibleText() string {
	var buf []byte
	for _, block := range s.blocks {
		if block.visible == 0 {
			continue
		}
		for i := range block.elems {
			if block.elems[i].Deleted {
				continue
			}
			buf = append(buf, block.elems[i].Value.Str...)
		}
	}
	return string(buf)
}

// clone copies the sequence, including the element values.
func (s *seq) clone() *seq {
	out := newSeq()
	elems := s.elems()
	for i := range elems {
		elems[i].Value = elems[i].Value.Clone()
	}
	out.reset(elems)
	return out
}
