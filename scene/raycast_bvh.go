package scene

import (
	"math"
	"slices"
	"strings"
	"sync"

	"m31labs.dev/gosx/scene/geom"
)

// This file adds a bounding volume hierarchy (BVH) over a flattened scene
// graph. TraceGraph walks the graph for every ray and stays allocation free,
// which suits a single pick. Editors, hitscan loops, and drag gizmos fire many
// rays against one static graph, so they should pay the traversal cost once.
// SceneAccelerator does that: it flattens the graph into primitives, builds a
// BVH over their world-space boxes, and answers each ray in about log(n) box
// tests plus the exact tests for the few primitives the ray can still reach.

// primKind names the exact intersection routine a flattened primitive uses.
type primKind uint8

const (
	primMesh primKind = iota
	primInstance
	primPoint
	primSprite
	primModel
)

// accelPrim is one raycastable primitive in world space. An InstancedMesh
// contributes one accelPrim per instance and a Points cloud one per particle,
// so the BVH prunes instances and particles with the same code path it uses
// for meshes.
type accelPrim struct {
	geometry Geometry
	world    worldTransform
	scale    Vector3
	id       string
	kind     primKind
	pickable bool
	// stroked marks a geometry whose pick radius the exact test needs, which is
	// Lines and nothing else. The flag saves that divide for every other
	// primitive. It sits in the padding after pickable, so it costs no memory.
	stroked bool
	// index carries the instance or particle index, or -1 for single primitives.
	index int
	// radius is the world-space bounding sphere radius.
	radius float64
}

// aabb is an axis-aligned bounding box in world space.
type aabb struct {
	Min Vector3
	Max Vector3
}

// lowerOf and upperOf compare two finite coordinates. They replace math.Min and
// math.Max in the build loops because those two functions carry NaN and signed
// zero rules the compiler cannot fold into one instruction. Every coordinate a
// scene graph produces is finite, so the plain comparison is enough.
func lowerOf(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func upperOf(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func unionAABB(left, right aabb) aabb {
	return aabb{
		Min: Vector3{X: lowerOf(left.Min.X, right.Min.X), Y: lowerOf(left.Min.Y, right.Min.Y), Z: lowerOf(left.Min.Z, right.Min.Z)},
		Max: Vector3{X: upperOf(left.Max.X, right.Max.X), Y: upperOf(left.Max.Y, right.Max.Y), Z: upperOf(left.Max.Z, right.Max.Z)},
	}
}

func sphereAABB(center Vector3, radius float64) aabb {
	return aabb{
		Min: Vector3{X: center.X - radius, Y: center.Y - radius, Z: center.Z - radius},
		Max: Vector3{X: center.X + radius, Y: center.Y + radius, Z: center.Z + radius},
	}
}

// bvhMaxLeafSize is the primitive count at which splitting stops paying. Four
// exact tests cost less than the extra box tests a deeper tree adds.
const bvhMaxLeafSize = 4

// bvhNode is one node of the hierarchy. count above zero marks a leaf that owns
// order[start : start+count]. count of zero marks an interior node whose left
// child is the next node and whose right child is at index start.
type bvhNode struct {
	bounds aabb
	start  int32
	count  int32
}

// bvh is a median-split bounding volume hierarchy over a fixed primitive set.
type bvh struct {
	nodes []bvhNode
	order []int32
}

// buildBVH builds a hierarchy over boxes. The returned tree is empty when boxes
// is empty.
func buildBVH(boxes []aabb) bvh {
	if len(boxes) == 0 {
		return bvh{}
	}
	tree := bvh{
		nodes: make([]bvhNode, 0, len(boxes)),
		order: make([]int32, len(boxes)),
	}
	centers := make([]Vector3, len(boxes))
	for i := range boxes {
		tree.order[i] = int32(i)
		centers[i] = Vector3{
			X: (boxes[i].Min.X + boxes[i].Max.X) / 2,
			Y: (boxes[i].Min.Y + boxes[i].Max.Y) / 2,
			Z: (boxes[i].Min.Z + boxes[i].Max.Z) / 2,
		}
	}
	tree.split(boxes, centers, 0, int32(len(boxes)))
	return tree
}

// split builds the subtree that owns order[first : first+count] and returns its
// node index.
func (b *bvh) split(boxes []aabb, centers []Vector3, first, count int32) int32 {
	self := int32(len(b.nodes))
	b.nodes = append(b.nodes, bvhNode{})
	span := b.order[first : first+count]

	if count <= bvhMaxLeafSize {
		b.nodes[self] = bvhNode{bounds: leafBounds(boxes, span), start: first, count: count}
		return self
	}

	axis, extent := widestCentroidAxis(centers, span)
	if extent <= 0 {
		// Every center coincides, so no split can separate the primitives.
		b.nodes[self] = bvhNode{bounds: leafBounds(boxes, span), start: first, count: count}
		return self
	}

	half := count / 2
	// Partition around the median instead of sorting the whole range. Both give
	// the same split point, but the partition costs O(count) per level, which
	// keeps the whole build at O(n log n).
	partitionAroundRank(span, centers, axis, int(half))

	left := b.split(boxes, centers, first, half)
	right := b.split(boxes, centers, first+half, count-half)
	// Union the two child boxes instead of sweeping the range again. Read the
	// children after both calls return, because append can move the node slice.
	b.nodes[self] = bvhNode{
		bounds: unionAABB(b.nodes[left].bounds, b.nodes[right].bounds),
		start:  right,
		count:  0,
	}
	return self
}

// leafBounds unions the boxes of the primitives one leaf owns.
func leafBounds(boxes []aabb, span []int32) aabb {
	bounds := boxes[span[0]]
	for _, index := range span[1:] {
		bounds = unionAABB(bounds, boxes[index])
	}
	return bounds
}

// partitionAroundRank reorders order so every element before rank has a centroid
// component at or below every element from rank onward. It is a quickselect on
// the chosen axis and breaks ties on the primitive index so the split stays
// deterministic across runs.
func partitionAroundRank(order []int32, centers []Vector3, axis, rank int) {
	low, high := 0, len(order)-1
	for low < high {
		pivotIndex := low + (high-low)/2
		pivot := centers[order[pivotIndex]]
		pivotKey := axisComponent(pivot, axis)
		pivotTie := order[pivotIndex]
		left, right := low, high
		for left <= right {
			for centroidLess(centers, order[left], axis, pivotKey, pivotTie) {
				left++
			}
			for centroidLess(centers, pivotTie, axis, axisComponent(centers[order[right]], axis), order[right]) {
				right--
			}
			if left <= right {
				order[left], order[right] = order[right], order[left]
				left++
				right--
			}
		}
		if rank <= right {
			high = right
		} else if rank >= left {
			low = left
		} else {
			return
		}
	}
}

// centroidLess orders two primitives on one axis, breaking ties on the index.
func centroidLess(centers []Vector3, index int32, axis int, key float64, tie int32) bool {
	value := axisComponent(centers[index], axis)
	if value != key {
		return value < key
	}
	return index < tie
}

func axisComponent(value Vector3, axis int) float64 {
	switch axis {
	case 0:
		return value.X
	case 1:
		return value.Y
	default:
		return value.Z
	}
}

// widestCentroidAxis returns the axis with the largest centroid spread and that
// spread. The widest axis gives the most balanced median split.
func widestCentroidAxis(centers []Vector3, indices []int32) (int, float64) {
	min := centers[indices[0]]
	max := min
	for _, index := range indices[1:] {
		center := centers[index]
		min.X = lowerOf(min.X, center.X)
		min.Y = lowerOf(min.Y, center.Y)
		min.Z = lowerOf(min.Z, center.Z)
		max.X = upperOf(max.X, center.X)
		max.Y = upperOf(max.Y, center.Y)
		max.Z = upperOf(max.Z, center.Z)
	}
	spanX, spanY, spanZ := max.X-min.X, max.Y-min.Y, max.Z-min.Z
	if spanX >= spanY && spanX >= spanZ {
		return 0, spanX
	}
	if spanY >= spanZ {
		return 1, spanY
	}
	return 2, spanZ
}

// bvhRay caches the reciprocal ray direction so the slab test needs no divide.
type bvhRay struct {
	origin  Vector3
	inverse Vector3
}

// newBVHRay prepares a ray for slab tests. Zero direction components become a
// very small value so the reciprocal stays finite and no comparison sees a NaN.
func newBVHRay(ray Ray) bvhRay {
	const tiny = 1e-30
	direction := ray.Direction
	if direction.X == 0 {
		direction.X = tiny
	}
	if direction.Y == 0 {
		direction.Y = tiny
	}
	if direction.Z == 0 {
		direction.Z = tiny
	}
	return bvhRay{
		origin:  ray.Origin,
		inverse: Vector3{X: 1 / direction.X, Y: 1 / direction.Y, Z: 1 / direction.Z},
	}
}

// hitsBox reports whether the ray reaches the box within maxDistance. padding
// grows the box on every side, which lets one prebuilt tree answer queries that
// use a larger point threshold than the build used.
func (r bvhRay) hitsBox(box aabb, padding, maxDistance float64) bool {
	near := 0.0
	far := math.Inf(1)
	if maxDistance > 0 {
		far = maxDistance
	}

	lower := (box.Min.X - padding - r.origin.X) * r.inverse.X
	upper := (box.Max.X + padding - r.origin.X) * r.inverse.X
	if lower > upper {
		lower, upper = upper, lower
	}
	near = upperOf(near, lower)
	far = lowerOf(far, upper)
	if near > far {
		return false
	}

	lower = (box.Min.Y - padding - r.origin.Y) * r.inverse.Y
	upper = (box.Max.Y + padding - r.origin.Y) * r.inverse.Y
	if lower > upper {
		lower, upper = upper, lower
	}
	near = upperOf(near, lower)
	far = lowerOf(far, upper)
	if near > far {
		return false
	}

	lower = (box.Min.Z - padding - r.origin.Z) * r.inverse.Z
	upper = (box.Max.Z + padding - r.origin.Z) * r.inverse.Z
	if lower > upper {
		lower, upper = upper, lower
	}
	near = upperOf(near, lower)
	far = lowerOf(far, upper)
	return near <= far
}

// forEachCandidate calls visit for every primitive index in a leaf box the ray
// reaches. Traversal uses a fixed stack, so it allocates nothing.
func (b *bvh) forEachCandidate(ray Ray, padding, maxDistance float64, visit func(prim int32)) {
	if len(b.nodes) == 0 {
		return
	}
	prepared := newBVHRay(ray)
	// A balanced tree over the primitive counts this engine handles never
	// exceeds this depth, and split() halves the range at every level.
	var stack [64]int32
	depth := 1
	stack[0] = 0
	for depth > 0 {
		depth--
		index := stack[depth]
		node := b.nodes[index]
		if !prepared.hitsBox(node.bounds, padding, maxDistance) {
			continue
		}
		if node.count > 0 {
			for i := node.start; i < node.start+node.count; i++ {
				visit(b.order[i])
			}
			continue
		}
		if depth+2 <= len(stack) {
			stack[depth] = node.start
			stack[depth+1] = index + 1
			depth += 2
			continue
		}
		// The stack is full only for a pathological tree. Fall back to visiting
		// the whole subtree so the query stays correct.
		b.visitSubtree(index, visit)
	}
}

func (b *bvh) visitSubtree(index int32, visit func(prim int32)) {
	node := b.nodes[index]
	if node.count > 0 {
		for i := node.start; i < node.start+node.count; i++ {
			visit(b.order[i])
		}
		return
	}
	b.visitSubtree(index+1, visit)
	b.visitSubtree(node.start, visit)
}

// triangleSoup is an indexed triangle list a ray can test exactly. positions
// holds flat xyz triples. indices holds three vertex indices per triangle; empty
// indices mean the positions already run as a triangle list. tree is optional: an
// empty tree makes intersect walk every triangle, which suits a one-shot query
// over a small mesh, while a built tree answers in about log(n) box tests.
//
// A soup over a BufferGeometry borrows the caller's slices, so building one
// allocates nothing.
type triangleSoup struct {
	positions []float64
	indices   []int
	tree      bvh
}

// soupOfBuffer views a BufferGeometry as a triangle soup without copying.
func soupOfBuffer(geometry BufferGeometry) triangleSoup {
	return triangleSoup{positions: geometry.Positions, indices: geometry.Indices}
}

func (s *triangleSoup) triangleCount() int {
	if len(s.indices) > 0 {
		return len(s.indices) / 3
	}
	return len(s.positions) / 9
}

// triangleAt returns the three corners of one triangle. An index that reaches
// past the position list yields the origin, which makes the triangle degenerate,
// and triangleHit then rejects it. That mirrors expandBufferAttr, which skips
// malformed indices instead of failing.
func (s *triangleSoup) triangleAt(triangle int) (Vector3, Vector3, Vector3) {
	if len(s.indices) > 0 {
		base := triangle * 3
		return s.vertexAt(s.indices[base]), s.vertexAt(s.indices[base+1]), s.vertexAt(s.indices[base+2])
	}
	base := triangle * 3
	return s.vertexAt(base), s.vertexAt(base + 1), s.vertexAt(base + 2)
}

func (s *triangleSoup) vertexAt(vertex int) Vector3 {
	base := vertex * 3
	if vertex < 0 || base+3 > len(s.positions) {
		return Vector3{}
	}
	return Vector3{X: s.positions[base], Y: s.positions[base+1], Z: s.positions[base+2]}
}

// intersect returns the nearest triangle the ray crosses. Both traversal modes
// keep the nearest distance and break a tie on the lower triangle number, so a
// soup with a tree and a soup without one always return the same triangle.
func (s *triangleSoup) intersect(ray Ray) (RayHit, bool) {
	best := math.Inf(1)
	bestTriangle := -1
	var bestNormal Vector3
	if len(s.tree.nodes) == 0 {
		count := s.triangleCount()
		for triangle := 0; triangle < count; triangle++ {
			s.consider(ray, triangle, &best, &bestTriangle, &bestNormal)
		}
	} else {
		s.walkTree(ray, &best, &bestTriangle, &bestNormal)
	}
	if bestTriangle < 0 {
		return RayHit{}, false
	}
	return surfaceHit(ray, best, bestNormal), true
}

// consider keeps one triangle when it beats the current nearest hit.
func (s *triangleSoup) consider(ray Ray, triangle int, best *float64, bestTriangle *int, bestNormal *Vector3) {
	first, second, third := s.triangleAt(triangle)
	distance, normal, ok := triangleHit(ray, first, second, third)
	if !ok {
		return
	}
	if distance < *best || (distance == *best && triangle < *bestTriangle) {
		*best, *bestTriangle, *bestNormal = distance, triangle, normal
	}
}

// walkTree descends the triangle hierarchy with a fixed stack, so it allocates
// nothing. It passes the nearest distance found so far as the slab test cap,
// which drops whole subtrees that sit behind the current hit.
func (s *triangleSoup) walkTree(ray Ray, best *float64, bestTriangle *int, bestNormal *Vector3) {
	prepared := newBVHRay(ray)
	var stack [64]int32
	depth := 1
	stack[0] = 0
	for depth > 0 {
		depth--
		index := stack[depth]
		node := s.tree.nodes[index]
		if !prepared.hitsBox(node.bounds, 0, *best) {
			continue
		}
		if node.count > 0 {
			for i := node.start; i < node.start+node.count; i++ {
				s.consider(ray, int(s.tree.order[i]), best, bestTriangle, bestNormal)
			}
			continue
		}
		if depth+2 <= len(stack) {
			stack[depth] = node.start
			stack[depth+1] = index + 1
			depth += 2
			continue
		}
		// The stack fills only for a pathological tree. Fall back to the whole
		// subtree so the answer stays right.
		s.considerSubtree(ray, index, best, bestTriangle, bestNormal)
	}
}

func (s *triangleSoup) considerSubtree(ray Ray, index int32, best *float64, bestTriangle *int, bestNormal *Vector3) {
	node := s.tree.nodes[index]
	if node.count > 0 {
		for i := node.start; i < node.start+node.count; i++ {
			s.consider(ray, int(s.tree.order[i]), best, bestTriangle, bestNormal)
		}
		return
	}
	s.considerSubtree(ray, index+1, best, bestTriangle, bestNormal)
	s.considerSubtree(ray, node.start, best, bestTriangle, bestNormal)
}

// buildSoupTree builds a hierarchy over the triangles of a soup. Call it when the
// same triangles answer many rays; a one-shot query is cheaper without it.
func (s *triangleSoup) buildSoupTree() {
	count := s.triangleCount()
	if count <= bvhMaxLeafSize {
		return
	}
	boxes := make([]aabb, count)
	for triangle := 0; triangle < count; triangle++ {
		first, second, third := s.triangleAt(triangle)
		boxes[triangle] = aabb{
			Min: Vector3{
				X: lowerOf(first.X, lowerOf(second.X, third.X)),
				Y: lowerOf(first.Y, lowerOf(second.Y, third.Y)),
				Z: lowerOf(first.Z, lowerOf(second.Z, third.Z)),
			},
			Max: Vector3{
				X: upperOf(first.X, upperOf(second.X, third.X)),
				Y: upperOf(first.Y, upperOf(second.Y, third.Y)),
				Z: upperOf(first.Z, upperOf(second.Z, third.Z)),
			},
		}
	}
	s.tree = buildBVH(boxes)
}

// segmentSoup holds the drawn strokes of a polyline and an optional hierarchy
// over them. One Lines mesh carries many segments, so a hierarchy cuts the exact
// tests the same way the scene BVH cuts primitives. The boxes bound the bare
// segments; a query pads them by its own pick radius, so one prebuilt soup answers
// any threshold.
type segmentSoup struct {
	from []Vector3
	to   []Vector3
	tree bvh
}

func (*segmentSoup) sceneGeometry() {}

func (*segmentSoup) legacyGeometry() (string, map[string]any) {
	return "lines", nil
}

// newSegmentSoup resolves the strokes the runtime draws and builds a hierarchy
// over them. It resolves them in the same order intersectLines walks, so the two
// paths number the segments alike and break a tie alike.
func newSegmentSoup(geometry LinesGeometry) *segmentSoup {
	points := geometry.Points
	if len(points) < 2 {
		return nil
	}
	soup := &segmentSoup{}
	for _, pair := range geometry.Segments {
		if !lineSegmentIsDrawn(pair, len(points)) {
			continue
		}
		soup.from = append(soup.from, points[pair[0]])
		soup.to = append(soup.to, points[pair[1]])
	}
	if len(soup.from) == 0 {
		for index := 0; index+1 < len(points); index++ {
			soup.from = append(soup.from, points[index])
			soup.to = append(soup.to, points[index+1])
		}
	}
	if len(soup.from) > bvhMaxLeafSize {
		boxes := make([]aabb, len(soup.from))
		for i := range soup.from {
			from, to := soup.from[i], soup.to[i]
			boxes[i] = aabb{
				Min: Vector3{X: lowerOf(from.X, to.X), Y: lowerOf(from.Y, to.Y), Z: lowerOf(from.Z, to.Z)},
				Max: Vector3{X: upperOf(from.X, to.X), Y: upperOf(from.Y, to.Y), Z: upperOf(from.Z, to.Z)},
			}
		}
		soup.tree = buildBVH(boxes)
	}
	return soup
}

// intersect returns the nearest stroke the ray reaches within threshold.
func (s *segmentSoup) intersect(ray Ray, threshold float64) (RayHit, bool) {
	if threshold <= 0 || len(s.from) == 0 {
		return RayHit{}, false
	}
	search := strokeSearch{ray: ray, threshold: threshold, best: math.Inf(1), index: -1}
	if len(s.tree.nodes) == 0 {
		for index := range s.from {
			search.consider(index, s.from[index], s.to[index])
		}
		return search.hit()
	}
	prepared := newBVHRay(ray)
	var stack [64]int32
	depth := 1
	stack[0] = 0
	for depth > 0 {
		depth--
		index := stack[depth]
		node := s.tree.nodes[index]
		// Pad the box by the pick radius so it holds the whole capsule, and cap the
		// slab test at the nearest stroke found so far.
		if !prepared.hitsBox(node.bounds, threshold, search.best) {
			continue
		}
		if node.count > 0 {
			s.considerLeaf(node, &search)
			continue
		}
		if depth+2 <= len(stack) {
			stack[depth] = node.start
			stack[depth+1] = index + 1
			depth += 2
			continue
		}
		s.considerSubtree(index, &search)
	}
	return search.hit()
}

func (s *segmentSoup) considerLeaf(node bvhNode, search *strokeSearch) {
	for i := node.start; i < node.start+node.count; i++ {
		segment := int(s.tree.order[i])
		search.consider(segment, s.from[segment], s.to[segment])
	}
}

func (s *segmentSoup) considerSubtree(index int32, search *strokeSearch) {
	node := s.tree.nodes[index]
	if node.count > 0 {
		s.considerLeaf(node, search)
		return
	}
	s.considerSubtree(index+1, search)
	s.considerSubtree(node.start, search)
}

// sceneGeometry and legacyGeometry let a prebuilt soup stand in for the
// BufferGeometry it came from. SceneAccelerator swaps the geometry of a triangle
// mesh for its soup, so the exact test reaches the prebuilt hierarchy through the
// same dispatch the graph walk uses, and the primitive record stays the same size.
// The type is unexported, so no scene graph can carry one.
func (*triangleSoup) sceneGeometry() {}

func (*triangleSoup) legacyGeometry() (string, map[string]any) {
	return "gltf-mesh", nil
}

// torusKnotRadius and torusKnotTube resolve the authored knot size. The defaults
// match torusKnotTriangleMesh in the runtime.
func torusKnotRadius(geometry TorusKnotGeometry) float64 {
	return positiveOr(geometry.Radius, 0.17)
}

func torusKnotTube(geometry TorusKnotGeometry) float64 {
	return positiveOr(geometry.Tube, 0.045)
}

// segmentResolution resolves an authored segment count the way the runtime does:
// a value at or below zero selects the fallback, and the result stays inside the
// renderer's limits.
func segmentResolution(value, fallback, low, high int) int {
	if value <= 0 {
		value = fallback
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// torusKnotKey names one tessellation. The four resolved numbers decide every
// vertex, so two knots with the same key own the same triangles.
type torusKnotKey struct {
	radius  float64
	tube    float64
	radial  int
	tubular int
}

// torusKnotCacheLimit caps how many tessellations the process keeps. Scenes
// author a handful of knot sizes, so this holds every live one. A scene that
// sweeps the parameters clears the cache instead of growing without bound.
const torusKnotCacheLimit = 32

var (
	// torusKnotCache reads without a lock, because a ray reaches it for every knot
	// candidate it tests. The lock guards writes only.
	torusKnotCache     sync.Map
	torusKnotCacheLock sync.Mutex
	torusKnotCacheSize int
)

// torusKnotSoup returns the tessellated knot for one geometry, building it on
// first use. The tessellation is the surface the renderer draws, so a triangle
// test against it is exact for the drawn mesh, not for the ideal swept tube.
//
// The key holds only resolved numbers, so a cached soup can never fall out of
// step with its geometry.
func torusKnotSoup(geometry TorusKnotGeometry) *triangleSoup {
	key := torusKnotKey{
		radius:  torusKnotRadius(geometry),
		tube:    torusKnotTube(geometry),
		radial:  segmentResolution(geometry.RadialSegments, 16, 3, 64),
		tubular: segmentResolution(geometry.TubularSegments, 128, 8, 512),
	}
	if cached, ok := torusKnotCache.Load(key); ok {
		return cached.(*triangleSoup)
	}
	// Build outside the lock. Two callers may build the same soup at once; both
	// results hold the same triangles, so keeping either one is correct.
	built := buildTorusKnotSoup(key)
	torusKnotCacheLock.Lock()
	if torusKnotCacheSize >= torusKnotCacheLimit {
		torusKnotCache.Clear()
		torusKnotCacheSize = 0
	}
	kept, loaded := torusKnotCache.LoadOrStore(key, built)
	if !loaded {
		torusKnotCacheSize++
	}
	torusKnotCacheLock.Unlock()
	return kept.(*triangleSoup)
}

// buildTorusKnotSoup tessellates one knot through package scene/geom, the single
// generator the browser wire path, the native renderer and this picker share.
// A triangle test against the result is exact for the drawn mesh, not for the
// ideal swept tube.
//
// The soup asks for positions alone. A ray reads no normal and no texture
// coordinate, so the other streams would be paid for and never read.
func buildTorusKnotSoup(key torusKnotKey) *triangleSoup {
	mesh, ok := tessellate(TorusKnotGeometry{
		Radius:          key.radius,
		Tube:            key.tube,
		RadialSegments:  key.radial,
		TubularSegments: key.tubular,
	}, geom.PositionsOnly)
	if !ok {
		return &triangleSoup{}
	}
	soup := &triangleSoup{positions: mesh.Positions, indices: mesh.Indices}
	soup.buildSoupTree()
	return soup
}

// SceneAccelerator answers many ray queries against one static scene graph.
// Build it once, then call Trace or Raycast per ray. Rebuild it after the graph
// changes; the accelerator holds a snapshot and never observes later edits.
type SceneAccelerator struct {
	prims []accelPrim
	boxes []aabb
	tree  bvh
	// buildThreshold is the pick radius the boxes were sized with. A query with a
	// larger threshold pads the box tests by the excess.
	buildThreshold float64
	nodesVisited   int
	// soups holds one prebuilt hierarchy per distinct triangle mesh or polyline, so
	// ten thousand instances of one mesh build one tree. The key is the address of
	// the first coordinate, which stays fixed for the life of the snapshot.
	soups map[*float64]Geometry
}

// NewSceneAccelerator flattens graph and builds a BVH over its raycastable
// primitives. Options set build-time defaults; only PointThreshold matters at
// build time because it sizes the boxes of Points particles and Sprites.
func NewSceneAccelerator(graph Graph, options ...RaycastOption) *SceneAccelerator {
	opts := RaycastOptions{}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	accel := &SceneAccelerator{buildThreshold: pointHitRadius(opts)}
	for _, node := range graph.Nodes {
		accel.flatten(node, identityTransform())
	}
	accel.boxes = make([]aabb, len(accel.prims))
	for i := range accel.prims {
		accel.boxes[i] = sphereAABB(accel.prims[i].world.Position, accel.prims[i].radius)
	}
	accel.tree = buildBVH(accel.boxes)
	return accel
}

// PrimitiveCount reports how many raycastable primitives the accelerator holds.
// Instances and particles each count once.
func (a *SceneAccelerator) PrimitiveCount() int {
	if a == nil {
		return 0
	}
	return len(a.prims)
}

// Raycast returns the closest hit, matching RaycastGraph.
func (a *SceneAccelerator) Raycast(ray Ray, options ...RaycastOption) (RayHit, bool) {
	trace := a.Trace(ray, options...)
	if trace.Closest == nil {
		return RayHit{}, false
	}
	return *trace.Closest, true
}

// Trace runs the same query as TraceGraph and returns the same hits, using the
// BVH instead of a full graph walk.
func (a *SceneAccelerator) Trace(ray Ray, options ...RaycastOption) RayTrace {
	opts := RaycastOptions{}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	ray.Direction = normalizeVector(ray.Direction)
	trace := RayTrace{Ray: ray, Options: opts, Hits: []RayHit{}}
	if a == nil || ray.Direction == (Vector3{}) {
		return trace
	}
	trace.NodesVisited = a.nodesVisited

	threshold := pointHitRadius(opts)
	padding := math.Max(0, threshold-a.buildThreshold)

	type orderedHit struct {
		hit   RayHit
		order int
	}
	var hits []orderedHit
	candidates := 0

	a.tree.forEachCandidate(ray, padding, opts.MaxDistance, func(index int32) {
		prim := &a.prims[index]
		candidates++
		if opts.PickableOnly && !prim.pickable {
			trace.FilteredPrimitives++
			return
		}
		hit, ok := a.intersect(prim, ray, threshold)
		trace.PrimitivesTested++
		if prim.kind == primInstance {
			trace.InstancesTested++
		}
		if !ok {
			return
		}
		if opts.MaxDistance > 0 && hit.Distance > opts.MaxDistance {
			return
		}
		hits = append(hits, orderedHit{hit: hit, order: int(index)})
	})

	trace.BroadphaseRejected = len(a.prims) - candidates
	slices.SortStableFunc(hits, func(left, right orderedHit) int {
		switch {
		case left.hit.Distance < right.hit.Distance:
			return -1
		case left.hit.Distance > right.hit.Distance:
			return 1
		case left.order < right.order:
			return -1
		case left.order > right.order:
			return 1
		default:
			return 0
		}
	})
	if len(hits) > 0 {
		trace.Hits = make([]RayHit, len(hits))
		for i := range hits {
			trace.Hits[i] = hits[i].hit
		}
		closest := trace.Hits[0]
		trace.Closest = &closest
	}
	return trace
}

// intersect runs the exact test for one flattened primitive.
func (a *SceneAccelerator) intersect(prim *accelPrim, ray Ray, threshold float64) (RayHit, bool) {
	switch prim.kind {
	case primPoint, primSprite:
		radius := threshold
		if prim.kind == primSprite {
			radius *= prim.scale.X
		}
		hit, ok := intersectWorldSphere(ray, prim.world.Position, radius)
		if !ok {
			return RayHit{}, false
		}
		hit.ID = prim.id
		hit.Pickable = prim.pickable
		hit.Method = "point-threshold"
		if prim.kind == primPoint {
			hit.Kind = "points"
			index := prim.index
			hit.InstanceIndex = &index
		} else {
			hit.Kind = "sprite"
		}
		return hit, true
	default:
		localThreshold := 0.0
		if prim.stroked {
			localThreshold = localDistanceBound(prim.world, prim.scale, threshold)
		}
		hit, ok := raycastTransformedGeometry(prim.geometry, prim.world, prim.scale, ray, localThreshold)
		if !ok {
			return RayHit{}, false
		}
		hit.ID = prim.id
		hit.Pickable = prim.pickable
		if prim.kind == primInstance {
			index := prim.index
			hit.InstanceIndex = &index
		}
		if prim.kind == primModel {
			hit.Kind = "model"
			hit.Method = "bounds-aabb"
		}
		return hit, true
	}
}

// flatten mirrors raycastNode so the accelerator and TraceGraph cover exactly
// the same primitives.
func (a *SceneAccelerator) flatten(node Node, parent worldTransform) {
	a.nodesVisited++
	switch current := node.(type) {
	case Mesh:
		a.flattenMesh(current, parent)
	case *Mesh:
		if current != nil {
			a.flattenMesh(*current, parent)
		}
	case InstancedMesh:
		a.flattenInstancedMesh(current, parent)
	case *InstancedMesh:
		if current != nil {
			a.flattenInstancedMesh(*current, parent)
		}
	case Group:
		a.flattenChildren(current.Children, combineTransforms(parent, localScaledTransform(current.Position, current.Rotation, current.Scale)))
	case *Group:
		if current != nil {
			a.flattenChildren(current.Children, combineTransforms(parent, localScaledTransform(current.Position, current.Rotation, current.Scale)))
		}
	case LODGroup:
		a.flattenLODGroup(current, parent)
	case *LODGroup:
		if current != nil {
			a.flattenLODGroup(*current, parent)
		}
	case Points:
		a.flattenPoints(current, parent)
	case *Points:
		if current != nil {
			a.flattenPoints(*current, parent)
		}
	case Sprite:
		a.flattenSprite(current, parent)
	case *Sprite:
		if current != nil {
			a.flattenSprite(*current, parent)
		}
	case Decal:
		a.flattenMesh(decalAsMesh(current), parent)
	case *Decal:
		if current != nil {
			a.flattenMesh(decalAsMesh(*current), parent)
		}
	case Model:
		a.flattenModel(current, parent)
	case *Model:
		if current != nil {
			a.flattenModel(*current, parent)
		}
	}
}

func (a *SceneAccelerator) flattenChildren(nodes []Node, parent worldTransform) {
	for _, child := range nodes {
		a.flatten(child, parent)
	}
}

func (a *SceneAccelerator) flattenLODGroup(group LODGroup, parent worldTransform) {
	world := combineTransforms(parent, localTransform(group.Position, group.Rotation))
	for _, level := range group.Levels {
		if level.Node == nil {
			continue
		}
		a.flatten(level.Node, world)
	}
}

func (a *SceneAccelerator) flattenMesh(mesh Mesh, parent worldTransform) {
	world := combineTransforms(parent, localTransform(mesh.Position, mesh.Rotation))
	scale := meshScaleOrUnit(mesh.Scale)
	radius, strokes := geometryBounds(mesh.Geometry)
	a.append(accelPrim{
		geometry: a.exactGeometry(mesh.Geometry),
		world:    world,
		scale:    scale,
		id:       strings.TrimSpace(mesh.ID),
		kind:     primMesh,
		pickable: mesh.Pickable == nil || *mesh.Pickable,
		stroked:  strokes > 0,
		index:    -1,
		radius:   radius*worldScaleBound(world, scale) + strokes*a.buildThreshold,
	})
	a.flattenChildren(mesh.Children, world)
}

func (a *SceneAccelerator) flattenInstancedMesh(mesh InstancedMesh, parent worldTransform) {
	count := instanceCount(mesh)
	pickable := mesh.Pickable == nil || *mesh.Pickable
	id := strings.TrimSpace(mesh.ID)
	localRadius, strokes := geometryBounds(mesh.Geometry)
	stroke := strokes * a.buildThreshold
	geometry := a.exactGeometry(mesh.Geometry)
	for i := 0; i < count; i++ {
		position := vectorAt(mesh.Positions, i, Vector3{})
		rotation := eulerAt(mesh.Rotations, i, Euler{})
		world := combineTransforms(parent, localTransform(position, rotation))
		scale := sanitizedScale(vectorAt(mesh.Scales, i, sceneUnitScale()))
		a.append(accelPrim{
			geometry: geometry,
			world:    world,
			scale:    scale,
			id:       id,
			kind:     primInstance,
			pickable: pickable,
			stroked:  strokes > 0,
			index:    i,
			radius:   localRadius*worldScaleBound(world, scale) + stroke,
		})
	}
}

// exactGeometry returns the geometry the exact test should read. A triangle mesh
// and a polyline swap to a prebuilt hierarchy, built once per distinct vertex or
// point buffer, so ten thousand instances of one mesh build one tree. Every other
// geometry passes through. A torus knot needs no entry here: its tessellation
// lives in a cache keyed by the authored numbers, which both the walk and the
// accelerator read.
//
// The cache key is the address of the first coordinate. The accelerator holds a
// snapshot of the graph, so that address names the same geometry for its whole
// life. Nothing outside one build reads the cache, so an edit to the graph cannot
// leave a stale tree behind.
func (a *SceneAccelerator) exactGeometry(geometry Geometry) Geometry {
	var buffer BufferGeometry
	switch typed := geometry.(type) {
	case BufferGeometry:
		buffer = typed
	case *BufferGeometry:
		if typed == nil {
			return geometry
		}
		buffer = *typed
	case LinesGeometry:
		if len(typed.Points) < 2 {
			return geometry
		}
		key := &typed.Points[0].X
		if cached, ok := a.soups[key]; ok {
			return cached
		}
		soup := newSegmentSoup(typed)
		if soup == nil {
			return geometry
		}
		a.rememberSoup(key, soup)
		return soup
	default:
		return geometry
	}
	if len(buffer.Positions) < 9 {
		return geometry
	}
	key := &buffer.Positions[0]
	if cached, ok := a.soups[key]; ok {
		return cached
	}
	soup := soupOfBuffer(buffer)
	soup.buildSoupTree()
	a.rememberSoup(key, &soup)
	return &soup
}

func (a *SceneAccelerator) rememberSoup(key *float64, geometry Geometry) {
	if a.soups == nil {
		a.soups = make(map[*float64]Geometry, 1)
	}
	a.soups[key] = geometry
}

func (a *SceneAccelerator) flattenPoints(points Points, parent worldTransform) {
	count := points.Count
	if count <= 0 {
		count = len(points.Positions)
	}
	if count > len(points.Positions) {
		count = len(points.Positions)
	}
	world := combineTransforms(parent, localTransform(points.Position, points.Rotation))
	id := strings.TrimSpace(points.ID)
	for i := 0; i < count; i++ {
		center := affinePoint(worldAffine(world), points.Positions[i])
		a.append(accelPrim{
			world:    worldTransform{Position: center, Rotation: quaternion{W: 1}},
			scale:    sceneUnitScale(),
			id:       id,
			kind:     primPoint,
			pickable: true,
			index:    i,
			radius:   a.buildThreshold,
		})
	}
}

func (a *SceneAccelerator) flattenSprite(sprite Sprite, parent worldTransform) {
	if strings.TrimSpace(sprite.Target) != "" {
		return
	}
	radiusScale := spriteRadiusScale(sprite)
	a.append(accelPrim{
		world:    worldTransform{Position: combineTransforms(parent, localTransform(sprite.Position, Euler{})).Position, Rotation: quaternion{W: 1}},
		scale:    Vector3{X: radiusScale, Y: radiusScale, Z: radiusScale},
		id:       strings.TrimSpace(sprite.ID),
		kind:     primSprite,
		pickable: true,
		index:    -1,
		radius:   a.buildThreshold * radiusScale,
	})
}

func (a *SceneAccelerator) flattenModel(model Model, parent worldTransform) {
	if model.Bounds <= 0 {
		return
	}
	world := combineTransforms(parent, localTransform(model.Position, model.Rotation))
	scale := meshScaleOrUnit(model.Scale)
	geometry := BoxGeometry{Width: model.Bounds, Height: model.Bounds, Depth: model.Bounds}
	radius, _ := geometryBounds(geometry)
	a.append(accelPrim{
		geometry: geometry,
		world:    world,
		scale:    scale,
		id:       strings.TrimSpace(model.ID),
		kind:     primModel,
		pickable: model.Pickable == nil || *model.Pickable,
		index:    -1,
		radius:   radius * worldScaleBound(world, scale),
	})
}

func (a *SceneAccelerator) append(prim accelPrim) {
	a.prims = append(a.prims, prim)
}
