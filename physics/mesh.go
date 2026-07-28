package physics

import "math"

// Triangle mesh collider storage plus a median-split bounding volume hierarchy
// over the triangles. The mesh is stored in collider-local space and built
// once at construction, so a step only transforms the candidate triangles that
// a query actually reaches.

const (
	// meshLeafSize is the largest triangle count kept in one leaf. Small
	// leaves deepen the tree; large leaves widen the per-leaf scan.
	meshLeafSize = 4
	// meshMaxDepth bounds the recursion so degenerate meshes cannot blow the
	// stack.
	meshMaxDepth = 32
)

type meshTriangle struct {
	a Vec3
	b Vec3
	c Vec3
	// normal is the unit face normal, following the winding of a, b and c.
	normal Vec3
	// edgeFiltered marks the edges whose neighbour makes a flat or a concave
	// join. Bit 0 is edge a-b, bit 1 is edge b-c and bit 2 is edge c-a. A
	// contact on such an edge must use the face normal, never the edge normal.
	edgeFiltered uint8
}

// filteredEdgeEndpoints returns the two corners of one edge and reports whether
// the internal edge filter covers it.
func (t meshTriangle) filteredEdgeEndpoints(edge int) (Vec3, Vec3, bool) {
	if t.edgeFiltered&(1<<uint(edge)) == 0 {
		return Vec3{}, Vec3{}, false
	}
	switch edge {
	case 0:
		return t.a, t.b, true
	case 1:
		return t.b, t.c, true
	default:
		return t.c, t.a, true
	}
}

func (t meshTriangle) bounds() AABB {
	return AABB{
		Min: t.a.Min(t.b).Min(t.c),
		Max: t.a.Max(t.b).Max(t.c),
	}
}

func (t meshTriangle) center() Vec3 {
	return t.a.Add(t.b).Add(t.c).Div(3)
}

// meshNode is one node of the flattened BVH. Leaves carry a triangle range;
// interior nodes carry the index of their right child, with the left child
// always sitting at the next slot.
type meshNode struct {
	bounds AABB
	start  int
	count  int
	right  int
}

func (n meshNode) leaf() bool {
	return n.count > 0
}

type triangleMesh struct {
	tris   []meshTriangle
	nodes  []meshNode
	bounds AABB
}

// newTriangleMesh copies the vertex and index buffers and builds the BVH. It
// returns nil when the buffers cannot describe at least one triangle, which the
// collider turns into a validation error.
func newTriangleMesh(vertices []Vec3, indices []int) *triangleMesh {
	if len(vertices) < 3 {
		return nil
	}
	if len(indices) == 0 {
		// No index buffer means the vertices are already triangle corners.
		indices = make([]int, len(vertices)-len(vertices)%3)
		for i := range indices {
			indices[i] = i
		}
	}
	if len(indices) < 3 {
		return nil
	}

	tris := make([]meshTriangle, 0, len(indices)/3)
	corners := make([][3]int, 0, len(indices)/3)
	for i := 0; i+2 < len(indices); i += 3 {
		ia, ib, ic := indices[i], indices[i+1], indices[i+2]
		if ia < 0 || ib < 0 || ic < 0 ||
			ia >= len(vertices) || ib >= len(vertices) || ic >= len(vertices) {
			continue
		}
		tri := meshTriangle{a: vertices[ia], b: vertices[ib], c: vertices[ic]}
		// Drop zero-area triangles. They carry no surface and would produce
		// degenerate support directions.
		cross := tri.b.Sub(tri.a).Cross(tri.c.Sub(tri.a))
		if cross.Len2() <= 1e-24 {
			continue
		}
		tri.normal = cross.Normalize()
		tris = append(tris, tri)
		corners = append(corners, [3]int{ia, ib, ic})
	}
	if len(tris) == 0 {
		return nil
	}
	markInternalEdges(tris, corners)

	mesh := &triangleMesh{tris: tris}
	mesh.nodes = make([]meshNode, 0, 2*len(tris)/meshLeafSize+2)
	mesh.build(0, len(tris), 0)
	mesh.bounds = mesh.nodes[0].bounds
	return mesh
}

// meshEdgeKey names one undirected edge by its two vertex indices, lower first.
type meshEdgeKey struct {
	lo int
	hi int
}

// markInternalEdges records, for every triangle edge, whether the neighbour
// across that edge makes a flat or a concave join.
//
// A body that slides across such an edge must keep pushing along the face
// normal. Without the mark, the narrowphase can hand the solver the edge normal
// instead, which points sideways and throws the body off the surface. That is
// the hitch a player feels when crossing a triangle boundary on a mesh floor.
//
// A convex ridge keeps its edge contacts, because there the edge normal is the
// real surface direction.
func markInternalEdges(tris []meshTriangle, corners [][3]int) {
	if len(tris) != len(corners) {
		return
	}
	type edgeOwner struct {
		tri  int
		edge int
	}
	owners := make(map[meshEdgeKey]edgeOwner, len(tris)*3)
	for t := range tris {
		for e := 0; e < 3; e++ {
			a := corners[t][e]
			b := corners[t][(e+1)%3]
			key := meshEdgeKey{lo: a, hi: b}
			if key.lo > key.hi {
				key.lo, key.hi = key.hi, key.lo
			}
			other, seen := owners[key]
			if !seen {
				owners[key] = edgeOwner{tri: t, edge: e}
				continue
			}
			// The edge is shared. Decide convexity from the dihedral: the join
			// is convex when the neighbour's third corner lies behind this
			// triangle's face plane.
			if !edgeIsConvex(tris[t], tris[other.tri]) {
				tris[t].edgeFiltered |= 1 << uint(e)
				tris[other.tri].edgeFiltered |= 1 << uint(other.edge)
			}
			delete(owners, key)
		}
	}
}

// edgeIsConvex reports whether two triangles that share an edge form a ridge
// that bulges away from the surface.
func edgeIsConvex(tri, neighbour meshTriangle) bool {
	const flatTolerance = 1e-7
	// The join bulges outward when the neighbour folds away from this face, so
	// its centre lies behind this triangle's plane. A concave valley puts the
	// neighbour in front of the plane, and a flat join puts it on the plane.
	offset := tri.normal.Dot(neighbour.center().Sub(tri.a))
	return offset < -flatTolerance
}

// build recursively splits the triangle range [start,end) at the median of the
// widest axis and appends the node in depth-first order. It returns the index
// of the node it created.
func (m *triangleMesh) build(start, end, depth int) int {
	bounds := m.rangeBounds(start, end)
	index := len(m.nodes)
	m.nodes = append(m.nodes, meshNode{bounds: bounds})

	count := end - start
	if count <= meshLeafSize || depth >= meshMaxDepth {
		m.nodes[index].start = start
		m.nodes[index].count = count
		return index
	}

	axis := widestAxis(bounds.Size())
	mid := start + count/2
	nthElementByCenter(m.tris[start:end], mid-start, axis)

	m.build(start, mid, depth+1)
	right := m.build(mid, end, depth+1)
	m.nodes[index].right = right
	return index
}

func (m *triangleMesh) rangeBounds(start, end int) AABB {
	bounds := m.tris[start].bounds()
	for i := start + 1; i < end; i++ {
		bounds = bounds.Union(m.tris[i].bounds())
	}
	return bounds
}

// queryAABB appends the indexes of every triangle whose bounds overlap box.
// The builder emits the left subtree immediately after its parent, so the left
// child of node i is always node i+1.
func (m *triangleMesh) queryAABB(box AABB, out []int) []int {
	if m == nil || len(m.nodes) == 0 {
		return out
	}
	var stack [meshMaxDepth * 2]int
	top := 0
	stack[top] = 0
	top++
	for top > 0 {
		top--
		index := stack[top]
		node := m.nodes[index]
		if !node.bounds.Overlaps(box) {
			continue
		}
		if node.leaf() {
			for i := node.start; i < node.start+node.count; i++ {
				if m.tris[i].bounds().Overlaps(box) {
					out = append(out, i)
				}
			}
			continue
		}
		if top+2 > len(stack) {
			continue
		}
		stack[top] = node.right
		top++
		stack[top] = index + 1
		top++
	}
	return out
}

// queryRay calls visit for every triangle index in a leaf whose bounds the ray
// enters within maxDistance. visit may lower the search distance by returning a
// smaller limit, which prunes the rest of the walk.
func (m *triangleMesh) queryRay(origin, direction Vec3, maxDistance float64, visit func(index int, limit float64) float64) {
	if m == nil || len(m.nodes) == 0 || visit == nil {
		return
	}
	limit := maxDistance
	var stack [meshMaxDepth * 2]int
	top := 0
	stack[top] = 0
	top++
	for top > 0 {
		top--
		index := stack[top]
		node := m.nodes[index]
		if !rayHitsAABB(node.bounds, origin, direction, limit) {
			continue
		}
		if node.leaf() {
			for i := node.start; i < node.start+node.count; i++ {
				limit = visit(i, limit)
			}
			continue
		}
		if top+2 > len(stack) {
			continue
		}
		stack[top] = node.right
		top++
		stack[top] = index + 1
		top++
	}
}

// rayHitsAABB runs the slab test for a ray against a box.
func rayHitsAABB(box AABB, origin, direction Vec3, maxDistance float64) bool {
	tMin := 0.0
	tMax := maxDistance
	mins := [3]float64{box.Min.X, box.Min.Y, box.Min.Z}
	maxs := [3]float64{box.Max.X, box.Max.Y, box.Max.Z}
	origins := [3]float64{origin.X, origin.Y, origin.Z}
	dirs := [3]float64{direction.X, direction.Y, direction.Z}
	for i := 0; i < 3; i++ {
		if math.Abs(dirs[i]) <= epsilon {
			if origins[i] < mins[i] || origins[i] > maxs[i] {
				return false
			}
			continue
		}
		inv := 1 / dirs[i]
		t1 := (mins[i] - origins[i]) * inv
		t2 := (maxs[i] - origins[i]) * inv
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		if t1 > tMin {
			tMin = t1
		}
		if t2 < tMax {
			tMax = t2
		}
		if tMin > tMax {
			return false
		}
	}
	return true
}

func widestAxis(size Vec3) int {
	axis := 0
	extent := size.X
	if size.Y > extent {
		axis = 1
		extent = size.Y
	}
	if size.Z > extent {
		axis = 2
	}
	return axis
}

func axisComponent(v Vec3, axis int) float64 {
	switch axis {
	case 0:
		return v.X
	case 1:
		return v.Y
	default:
		return v.Z
	}
}

// nthElementByCenter partially orders triangles so that the element at index n
// is the one that a full sort would place there, comparing triangle centers on
// the given axis. This is quickselect, so the split costs O(count) instead of
// O(count log count).
func nthElementByCenter(tris []meshTriangle, n, axis int) {
	low := 0
	high := len(tris) - 1
	for low < high {
		pivot := axisComponent(tris[(low+high)/2].center(), axis)
		i := low
		j := high
		for i <= j {
			for axisComponent(tris[i].center(), axis) < pivot {
				i++
			}
			for axisComponent(tris[j].center(), axis) > pivot {
				j--
			}
			if i <= j {
				tris[i], tris[j] = tris[j], tris[i]
				i++
				j--
			}
		}
		if n <= j {
			high = j
		} else if n >= i {
			low = i
		} else {
			return
		}
	}
}

// localAABBFromWorld converts a world AABB into a conservative AABB in the
// collider's local frame. Rotating a box never shrinks it, so the result may be
// larger than needed but never misses a triangle.
func (c *Collider) localAABBFromWorld(box AABB) AABB {
	center := c.WorldCenter()
	basis := mat3FromQuat(c.WorldRotation())
	localCenter := basis.mulT(box.Center().Sub(center))
	half := box.Size().Mul(0.5)
	// Project the world half extents onto the local axes.
	localHalf := Vec3{
		X: math.Abs(basis.x.X)*half.X + math.Abs(basis.x.Y)*half.Y + math.Abs(basis.x.Z)*half.Z,
		Y: math.Abs(basis.y.X)*half.X + math.Abs(basis.y.Y)*half.Y + math.Abs(basis.y.Z)*half.Z,
		Z: math.Abs(basis.z.X)*half.X + math.Abs(basis.z.Y)*half.Y + math.Abs(basis.z.Z)*half.Z,
	}
	return AABBFromCenterHalfExtents(localCenter, localHalf)
}
