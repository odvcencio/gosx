package earcut

import (
	"math"
	"sort"
)

// node is a vertex in a circular doubly linked list representing a polygon
// ring. prev/next form the ring; prevZ/nextZ form a second, z-order-sorted
// linked list used by isEarHashed once the input is large enough to bother
// hashing. i is the vertex's coordinate offset into the flat input array
// (always a multiple of dims), not a vertex index — callers convert with
// i/dims when a vertex index is needed (matching upstream's `i / dim | 0`).
type node struct {
	i    int
	x, y float64

	prev, next   *node
	z            int32
	prevZ, nextZ *node

	// steiner marks a single-point hole ring (a "steiner point"); such nodes
	// must survive filterPoints even though they look locally degenerate.
	steiner bool
}

func newNode(i int, x, y float64) *node {
	return &node{i: i, x: x, y: y}
}

// Triangulate triangulates the polygon described by a flat vertex coordinate
// array, optional hole ring start indices (indices into vertices, i.e. in
// vertices not coordinates — hole[k]*dims is the coordinate offset where hole
// ring k begins), and dims, the number of coordinates per vertex (2 or 3;
// only the first two participate in triangulation). It returns a flat list of
// triangle indices into vertices (triplets), as vertex indices, not
// coordinate offsets.
//
// Mirrors the upstream JS earcut(data, holeIndices, dim) signature exactly.
func Triangulate(vertices []float64, holes []int, dims int) []int {
	if dims <= 0 {
		dims = 2
	}

	hasHoles := len(holes) > 0
	outerLen := len(vertices)
	if hasHoles {
		outerLen = holes[0] * dims
	}
	outerNode := linkedList(vertices, 0, outerLen, dims, true)
	triangles := []int{}

	if outerNode == nil || outerNode.next == outerNode.prev {
		return triangles
	}

	var minX, minY, maxX, maxY, invSize float64

	if hasHoles {
		outerNode = eliminateHoles(vertices, holes, outerNode, dims)
	}

	// if the shape is not too simple, we'll use z-order curve hash later; calculate polygon bbox
	if len(vertices) > 80*dims {
		minX, maxX = vertices[0], vertices[0]
		minY, maxY = vertices[1], vertices[1]

		for i := dims; i < outerLen; i += dims {
			x, y := vertices[i], vertices[i+1]
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}

		// minX, minY and invSize are later used to transform coords into integers for z-order calculation
		invSize = math.Max(maxX-minX, maxY-minY)
		if invSize != 0 {
			invSize = 32767 / invSize
		}
	}

	earcutLinked(outerNode, &triangles, dims, minX, minY, invSize, 0)

	return triangles
}

// linkedList creates a circular doubly linked list from polygon points in
// the specified winding order.
func linkedList(data []float64, start, end, dims int, clockwise bool) *node {
	var last *node

	if clockwise == (signedArea(data, start, end, dims) > 0) {
		for i := start; i < end; i += dims {
			last = insertNode(i, data[i], data[i+1], last)
		}
	} else {
		for i := end - dims; i >= start; i -= dims {
			last = insertNode(i, data[i], data[i+1], last)
		}
	}

	if last != nil && equals(last, last.next) {
		removeNode(last)
		last = last.next
	}

	return last
}

// filterPoints eliminates collinear or duplicate points from the ring
// containing start, walking the window [start, end). end defaults to start
// (i.e. pass nil) to sweep the whole ring.
func filterPoints(start, end *node) *node {
	if start == nil {
		return start
	}
	if end == nil {
		end = start
	}

	p := start
	for {
		again := false

		if !p.steiner && (equals(p, p.next) || area(p.prev, p, p.next) == 0) {
			removeNode(p)
			p = p.prev
			end = p
			if p == p.next {
				break
			}
			again = true
		} else {
			p = p.next
		}

		if !again && p == end {
			break
		}
	}

	return end
}

// earcutLinked is the main ear slicing loop which triangulates a polygon
// (given as a linked list). pass tracks which fallback stage is active: 0 is
// the normal ear-clipping pass, 1 retries after filtering collinear points,
// 2 cures local self-intersections, and beyond that the polygon is split.
func earcutLinked(ear *node, triangles *[]int, dims int, minX, minY, invSize float64, pass int) {
	if ear == nil {
		return
	}

	// interlink polygon nodes in z-order
	if pass == 0 && invSize != 0 {
		indexCurve(ear, minX, minY, invSize)
	}

	stop := ear
	var prev, next *node

	// iterate through ears, slicing them one by one
	for ear.prev != ear.next {
		prev = ear.prev
		next = ear.next

		var earFound bool
		if invSize != 0 {
			earFound = isEarHashed(ear, minX, minY, invSize)
		} else {
			earFound = isEar(ear)
		}

		if earFound {
			// cut off the triangle
			*triangles = append(*triangles, prev.i/dims, ear.i/dims, next.i/dims)

			removeNode(ear)

			// skipping the next vertex leads to less sliver triangles
			ear = next.next
			stop = next.next

			continue
		}

		ear = next

		// if we looped through the whole remaining polygon and can't find any more ears
		if ear == stop {
			switch pass {
			case 0:
				// try filtering points and slicing again
				earcutLinked(filterPoints(ear, nil), triangles, dims, minX, minY, invSize, 1)
			case 1:
				// if this didn't work, try curing all small self-intersections locally
				ear = cureLocalIntersections(filterPoints(ear, nil), triangles, dims)
				earcutLinked(ear, triangles, dims, minX, minY, invSize, 2)
			case 2:
				// as a last resort, try splitting the remaining polygon into two
				splitEarcut(ear, triangles, dims, minX, minY, invSize)
			}

			break
		}
	}
}

// isEar checks whether a polygon node forms a valid ear with adjacent nodes.
func isEar(ear *node) bool {
	a, b, c := ear.prev, ear, ear.next

	if area(a, b, c) >= 0 {
		return false // reflex, can't be an ear
	}

	ax, ay := a.x, a.y
	bx, by := b.x, b.y
	cx, cy := c.x, c.y

	// triangle bbox
	x0 := math.Min(ax, math.Min(bx, cx))
	y0 := math.Min(ay, math.Min(by, cy))
	x1 := math.Max(ax, math.Max(bx, cx))
	y1 := math.Max(ay, math.Max(by, cy))

	// now make sure we don't have other points inside the potential ear
	p := c.next
	for p != a {
		if p.x >= x0 && p.x <= x1 && p.y >= y0 && p.y <= y1 &&
			pointInTriangle(ax, ay, bx, by, cx, cy, p.x, p.y) &&
			area(p.prev, p, p.next) >= 0 {
			return false
		}
		p = p.next
	}

	return true
}

func isEarHashed(ear *node, minX, minY, invSize float64) bool {
	a, b, c := ear.prev, ear, ear.next

	if area(a, b, c) >= 0 {
		return false // reflex, can't be an ear
	}

	ax, ay := a.x, a.y
	bx, by := b.x, b.y
	cx, cy := c.x, c.y

	// triangle bbox
	x0 := math.Min(ax, math.Min(bx, cx))
	y0 := math.Min(ay, math.Min(by, cy))
	x1 := math.Max(ax, math.Max(bx, cx))
	y1 := math.Max(ay, math.Max(by, cy))

	// z-order range for the current triangle bbox
	minZ := zOrder(x0, y0, minX, minY, invSize)
	maxZ := zOrder(x1, y1, minX, minY, invSize)

	p := ear.prevZ
	n := ear.nextZ

	// look for points inside the triangle in both directions
	for p != nil && p.z >= minZ && n != nil && n.z <= maxZ {
		if p.x >= x0 && p.x <= x1 && p.y >= y0 && p.y <= y1 && p != a && p != c &&
			pointInTriangle(ax, ay, bx, by, cx, cy, p.x, p.y) && area(p.prev, p, p.next) >= 0 {
			return false
		}
		p = p.prevZ

		if n.x >= x0 && n.x <= x1 && n.y >= y0 && n.y <= y1 && n != a && n != c &&
			pointInTriangle(ax, ay, bx, by, cx, cy, n.x, n.y) && area(n.prev, n, n.next) >= 0 {
			return false
		}
		n = n.nextZ
	}

	// look for remaining points in decreasing z-order
	for p != nil && p.z >= minZ {
		if p.x >= x0 && p.x <= x1 && p.y >= y0 && p.y <= y1 && p != a && p != c &&
			pointInTriangle(ax, ay, bx, by, cx, cy, p.x, p.y) && area(p.prev, p, p.next) >= 0 {
			return false
		}
		p = p.prevZ
	}

	// look for remaining points in increasing z-order
	for n != nil && n.z <= maxZ {
		if n.x >= x0 && n.x <= x1 && n.y >= y0 && n.y <= y1 && n != a && n != c &&
			pointInTriangle(ax, ay, bx, by, cx, cy, n.x, n.y) && area(n.prev, n, n.next) >= 0 {
			return false
		}
		n = n.nextZ
	}

	return true
}

// cureLocalIntersections goes through all polygon nodes and cures small
// local self-intersections.
func cureLocalIntersections(start *node, triangles *[]int, dims int) *node {
	p := start
	for {
		a := p.prev
		b := p.next.next

		if !equals(a, b) && intersects(a, p, p.next, b) && locallyInside(a, b) && locallyInside(b, a) {
			*triangles = append(*triangles, a.i/dims, p.i/dims, b.i/dims)

			// remove two nodes involved
			removeNode(p)
			removeNode(p.next)

			p = b
			start = b
		}
		p = p.next
		if p == start {
			break
		}
	}

	return filterPoints(p, nil)
}

// splitEarcut tries splitting the polygon into two and triangulating each
// independently.
func splitEarcut(start *node, triangles *[]int, dims int, minX, minY, invSize float64) {
	// look for a valid diagonal that divides the polygon into two
	a := start
	for {
		b := a.next.next
		for b != a.prev {
			if a.i != b.i && isValidDiagonal(a, b) {
				// split the polygon in two by the diagonal
				c := splitPolygon(a, b)

				// filter colinear points around the cuts
				a = filterPoints(a, a.next)
				c = filterPoints(c, c.next)

				// run earcut on each half
				earcutLinked(a, triangles, dims, minX, minY, invSize, 0)
				earcutLinked(c, triangles, dims, minX, minY, invSize, 0)
				return
			}
			b = b.next
		}
		a = a.next
		if a == start {
			break
		}
	}
}

// eliminateHoles links every hole into the outer loop, producing a single
// ring polygon without holes.
func eliminateHoles(data []float64, holeIndices []int, outerNode *node, dims int) *node {
	var queue []*node

	hlen := len(holeIndices)
	for i := 0; i < hlen; i++ {
		start := holeIndices[i] * dims
		end := len(data)
		if i < hlen-1 {
			end = holeIndices[i+1] * dims
		}
		list := linkedList(data, start, end, dims, false)
		if list == list.next {
			list.steiner = true
		}
		queue = append(queue, getLeftmost(list))
	}

	sort.SliceStable(queue, func(i, j int) bool { return queue[i].x < queue[j].x })

	// process holes from left to right
	for i := range queue {
		outerNode = eliminateHole(queue[i], outerNode)
	}

	return outerNode
}

// eliminateHole finds a bridge between vertices that connects hole with an
// outer ring and links it.
func eliminateHole(hole, outerNode *node) *node {
	bridge := findHoleBridge(hole, outerNode)
	if bridge == nil {
		return outerNode
	}

	bridgeReverse := splitPolygon(bridge, hole)

	// filter collinear points around the cuts
	filterPoints(bridgeReverse, bridgeReverse.next)
	return filterPoints(bridge, bridge.next)
}

// findHoleBridge implements David Eberly's algorithm for finding a bridge
// between a hole and an outer polygon.
func findHoleBridge(hole, outerNode *node) *node {
	p := outerNode
	hx, hy := hole.x, hole.y
	qx := math.Inf(-1)
	var m *node

	// find a segment intersected by a ray from the hole's leftmost point to the left;
	// segment's endpoint with lesser x will be potential connection point
	for {
		if hy <= p.y && hy >= p.next.y && p.next.y != p.y {
			x := p.x + (hy-p.y)*(p.next.x-p.x)/(p.next.y-p.y)
			if x <= hx && x > qx {
				qx = x
				if p.x < p.next.x {
					m = p
				} else {
					m = p.next
				}
				if x == hx {
					return m // hole touches outer segment; pick leftmost endpoint
				}
			}
		}
		p = p.next
		if p == outerNode {
			break
		}
	}

	if m == nil {
		return nil
	}

	// look for points inside the triangle of hole point, segment intersection and endpoint;
	// if there are no points found, we have a valid connection;
	// otherwise choose the point of the minimum angle with the ray as connection point

	stop := m
	mx, my := m.x, m.y
	tanMin := math.Inf(1)
	var tan float64

	// the triangle's x-span endpoints, chosen by which side of the ray hy falls on
	triAx, triCx := qx, hx
	if hy < my {
		triAx, triCx = hx, qx
	}

	p = m

	for {
		if hx >= p.x && p.x >= mx && hx != p.x &&
			pointInTriangle(triAx, hy, mx, my, triCx, hy, p.x, p.y) {

			tan = math.Abs(hy-p.y) / (hx - p.x) // tangential

			if locallyInside(p, hole) &&
				(tan < tanMin || (tan == tanMin && (p.x > m.x || (p.x == m.x && sectorContainsSector(m, p))))) {
				m = p
				tanMin = tan
			}
		}

		p = p.next
		if p == stop {
			break
		}
	}

	return m
}

// sectorContainsSector reports whether the sector at vertex m contains the
// sector at vertex p in the same coordinates.
func sectorContainsSector(m, p *node) bool {
	return area(m.prev, m, p.prev) < 0 && area(p.next, m, m.next) < 0
}

// indexCurve interlinks polygon nodes in z-order.
func indexCurve(start *node, minX, minY, invSize float64) {
	p := start
	for {
		if p.z == 0 {
			p.z = zOrder(p.x, p.y, minX, minY, invSize)
		}
		p.prevZ = p.prev
		p.nextZ = p.next
		p = p.next
		if p == start {
			break
		}
	}

	p.prevZ.nextZ = nil
	p.prevZ = nil

	sortLinked(p)
}

// sortLinked is Simon Tatham's linked list merge sort algorithm, sorting by
// z along the prevZ/nextZ links.
// http://www.chiark.greenend.org.uk/~sgtatham/algorithms/listsort.html
func sortLinked(list *node) *node {
	inSize := 1

	for {
		p := list
		list = nil
		var tail *node
		numMerges := 0

		for p != nil {
			numMerges++
			q := p
			pSize := 0
			for i := 0; i < inSize; i++ {
				pSize++
				q = q.nextZ
				if q == nil {
					break
				}
			}
			qSize := inSize

			for pSize > 0 || (qSize > 0 && q != nil) {
				var e *node
				if pSize != 0 && (qSize == 0 || q == nil || p.z <= q.z) {
					e = p
					p = p.nextZ
					pSize--
				} else {
					e = q
					q = q.nextZ
					qSize--
				}

				if tail != nil {
					tail.nextZ = e
				} else {
					list = e
				}

				e.prevZ = tail
				tail = e
			}

			p = q
		}

		tail.nextZ = nil
		inSize *= 2

		if numMerges <= 1 {
			break
		}
	}

	return list
}

// zOrder computes the z-order (Morton code) of a point given coords and the
// inverse of the longer side of the data bbox.
func zOrder(x, y, minX, minY, invSize float64) int32 {
	// coords are transformed into non-negative 15-bit integer range
	xi := int32((x - minX) * invSize)
	yi := int32((y - minY) * invSize)

	xi = (xi | (xi << 8)) & 0x00FF00FF
	xi = (xi | (xi << 4)) & 0x0F0F0F0F
	xi = (xi | (xi << 2)) & 0x33333333
	xi = (xi | (xi << 1)) & 0x55555555

	yi = (yi | (yi << 8)) & 0x00FF00FF
	yi = (yi | (yi << 4)) & 0x0F0F0F0F
	yi = (yi | (yi << 2)) & 0x33333333
	yi = (yi | (yi << 1)) & 0x55555555

	return xi | (yi << 1)
}

// getLeftmost finds the leftmost node of a polygon ring.
func getLeftmost(start *node) *node {
	p := start
	leftmost := start
	for {
		if p.x < leftmost.x || (p.x == leftmost.x && p.y < leftmost.y) {
			leftmost = p
		}
		p = p.next
		if p == start {
			break
		}
	}
	return leftmost
}

// pointInTriangle checks if a point lies within a convex triangle.
func pointInTriangle(ax, ay, bx, by, cx, cy, px, py float64) bool {
	return (cx-px)*(ay-py) >= (ax-px)*(cy-py) &&
		(ax-px)*(by-py) >= (bx-px)*(ay-py) &&
		(bx-px)*(cy-py) >= (cx-px)*(by-py)
}

// isValidDiagonal checks if a diagonal between two polygon nodes is valid
// (lies in the polygon interior).
func isValidDiagonal(a, b *node) bool {
	return a.next.i != b.i && a.prev.i != b.i && !intersectsPolygon(a, b) && // doesn't intersect other edges
		((locallyInside(a, b) && locallyInside(b, a) && middleInside(a, b) && // locally visible
			(area(a.prev, a, b.prev) != 0 || area(a, b.prev, b) != 0)) || // does not create opposite-facing sectors
			(equals(a, b) && area(a.prev, a, a.next) > 0 && area(b.prev, b, b.next) > 0)) // special zero-length case
}

// area computes the signed area of a triangle.
func area(p, q, r *node) float64 {
	return (q.y-p.y)*(r.x-q.x) - (q.x-p.x)*(r.y-q.y)
}

// equals checks if two points are equal.
func equals(p1, p2 *node) bool {
	return p1.x == p2.x && p1.y == p2.y
}

// intersects checks if two segments intersect.
func intersects(p1, q1, p2, q2 *node) bool {
	o1 := sign(area(p1, q1, p2))
	o2 := sign(area(p1, q1, q2))
	o3 := sign(area(p2, q2, p1))
	o4 := sign(area(p2, q2, q1))

	if o1 != o2 && o3 != o4 {
		return true // general case
	}

	if o1 == 0 && onSegment(p1, p2, q1) {
		return true // p1, q1 and p2 are collinear and p2 lies on p1q1
	}
	if o2 == 0 && onSegment(p1, q2, q1) {
		return true // p1, q1 and q2 are collinear and q2 lies on p1q1
	}
	if o3 == 0 && onSegment(p2, p1, q2) {
		return true // p2, q2 and p1 are collinear and p1 lies on p2q2
	}
	if o4 == 0 && onSegment(p2, q1, q2) {
		return true // p2, q2 and q1 are collinear and q1 lies on p2q2
	}

	return false
}

// onSegment checks, for collinear points p, q, r, whether point q lies on
// segment pr.
func onSegment(p, q, r *node) bool {
	return q.x <= math.Max(p.x, r.x) && q.x >= math.Min(p.x, r.x) &&
		q.y <= math.Max(p.y, r.y) && q.y >= math.Min(p.y, r.y)
}

func sign(num float64) int {
	switch {
	case num > 0:
		return 1
	case num < 0:
		return -1
	default:
		return 0
	}
}

// intersectsPolygon checks if a polygon diagonal intersects any polygon
// segments.
func intersectsPolygon(a, b *node) bool {
	p := a
	for {
		if p.i != a.i && p.next.i != a.i && p.i != b.i && p.next.i != b.i &&
			intersects(p, p.next, a, b) {
			return true
		}
		p = p.next
		if p == a {
			break
		}
	}
	return false
}

// locallyInside checks if a polygon diagonal is locally inside the polygon.
func locallyInside(a, b *node) bool {
	if area(a.prev, a, a.next) < 0 {
		return area(a, b, a.next) >= 0 && area(a, a.prev, b) >= 0
	}
	return area(a, b, a.prev) < 0 || area(a, a.next, b) < 0
}

// middleInside checks if the middle point of a polygon diagonal is inside
// the polygon.
func middleInside(a, b *node) bool {
	p := a
	inside := false
	px := (a.x + b.x) / 2
	py := (a.y + b.y) / 2
	for {
		if ((p.y > py) != (p.next.y > py)) && p.next.y != p.y &&
			(px < (p.next.x-p.x)*(py-p.y)/(p.next.y-p.y)+p.x) {
			inside = !inside
		}
		p = p.next
		if p == a {
			break
		}
	}
	return inside
}

// splitPolygon links two polygon vertices with a bridge. If the vertices
// belong to the same ring, it splits the polygon into two; if one belongs to
// the outer ring and another to a hole, it merges them into a single ring.
func splitPolygon(a, b *node) *node {
	a2 := newNode(a.i, a.x, a.y)
	b2 := newNode(b.i, b.x, b.y)
	an := a.next
	bp := b.prev

	a.next = b
	b.prev = a

	a2.next = an
	an.prev = a2

	b2.next = a2
	a2.prev = b2

	bp.next = b2
	b2.prev = bp

	return b2
}

// insertNode creates a node and optionally links it with the previous one
// (in a circular doubly linked list).
func insertNode(i int, x, y float64, last *node) *node {
	p := newNode(i, x, y)

	if last == nil {
		p.prev = p
		p.next = p
	} else {
		p.next = last.next
		p.prev = last
		last.next.prev = p
		last.next = p
	}
	return p
}

func removeNode(p *node) {
	p.next.prev = p.prev
	p.prev.next = p.next

	if p.prevZ != nil {
		p.prevZ.nextZ = p.nextZ
	}
	if p.nextZ != nil {
		p.nextZ.prevZ = p.prevZ
	}
}

// Deviation returns the relative difference between the polygon's area and
// the total area of its triangulation — a value near 0 means a correct
// triangulation. Useful for verifying Triangulate's output in tests. Mirrors
// earcut.deviation from the JS API.
func Deviation(vertices []float64, holes []int, dims int, triangles []int) float64 {
	if dims <= 0 {
		dims = 2
	}

	hasHoles := len(holes) > 0
	outerLen := len(vertices)
	if hasHoles {
		outerLen = holes[0] * dims
	}

	polygonArea := math.Abs(signedArea(vertices, 0, outerLen, dims))
	if hasHoles {
		hlen := len(holes)
		for i := 0; i < hlen; i++ {
			start := holes[i] * dims
			end := len(vertices)
			if i < hlen-1 {
				end = holes[i+1] * dims
			}
			polygonArea -= math.Abs(signedArea(vertices, start, end, dims))
		}
	}

	trianglesArea := 0.0
	for i := 0; i < len(triangles); i += 3 {
		a := triangles[i] * dims
		b := triangles[i+1] * dims
		c := triangles[i+2] * dims
		trianglesArea += math.Abs(
			(vertices[a]-vertices[c])*(vertices[b+1]-vertices[a+1]) -
				(vertices[a]-vertices[b])*(vertices[c+1]-vertices[a+1]))
	}

	if polygonArea == 0 && trianglesArea == 0 {
		return 0
	}
	return math.Abs((trianglesArea - polygonArea) / polygonArea)
}

// signedArea computes the signed area of the ring data[start:end] (a flat
// coordinate slice, dims coordinates per vertex).
func signedArea(data []float64, start, end, dims int) float64 {
	sum := 0.0
	j := end - dims
	for i := start; i < end; i += dims {
		sum += (data[j] - data[i]) * (data[i+1] + data[j+1])
		j = i
	}
	return sum
}

// Flatten turns a polygon in nested-ring form (as in GeoJSON: rings[0] is the
// outer contour, rings[1:] are holes; each ring is a slice of points; each
// point is a slice of coordinates) into the flat vertices/holes/dims form
// Triangulate accepts. Mirrors earcut.flatten from the JS API.
func Flatten(rings [][][]float64) (vertices []float64, holes []int, dims int) {
	if len(rings) == 0 || len(rings[0]) == 0 {
		return nil, nil, 0
	}
	dims = len(rings[0][0])

	holeIndex := 0
	for i, ring := range rings {
		for _, p := range ring {
			for d := 0; d < dims && d < len(p); d++ {
				vertices = append(vertices, p[d])
			}
		}
		if i > 0 {
			holeIndex += len(rings[i-1])
			holes = append(holes, holeIndex)
		}
	}
	return vertices, holes, dims
}
