package meshsimplify

import (
	"container/heap"
	"math"
	"sort"
)

// Mesh is an indexed triangle list. Positions holds three float32 values per
// vertex and Indices holds three vertex indices per triangle.
type Mesh struct {
	Positions []float32
	Indices   []uint32
}

// VertexCount returns the number of vertices.
func (m Mesh) VertexCount() int { return len(m.Positions) / 3 }

// TriangleCount returns the number of triangles.
func (m Mesh) TriangleCount() int { return len(m.Indices) / 3 }

// Options controls one simplification run.
type Options struct {
	// TargetRatio is the fraction of input triangles to keep, from 0 to 1.
	TargetRatio float64
	// TargetTriangles overrides TargetRatio when it is above zero.
	TargetTriangles int
	// MaxErrorFraction stops the run once the cheapest remaining collapse
	// costs more than this fraction of the bounding box diagonal. Zero
	// removes the limit.
	MaxErrorFraction float64
	// BoundaryWeight scales the constraint planes that hold open borders in
	// place. Zero selects DefaultBoundaryWeight.
	BoundaryWeight float64
}

// DefaultBoundaryWeight holds open mesh borders close to their original
// shape. The value is large enough that a border collapses only when the
// interior has no cheaper option left.
const DefaultBoundaryWeight = 1000.0

// VertexSource records where an output vertex came from. An output position
// is the blend of source vertices A and B at factor T, where 0 keeps A and 1
// keeps B. A caller carries every other vertex attribute with the same blend.
type VertexSource struct {
	A, B int32
	T    float64
}

// Result reports the simplified mesh and the cost of producing it.
type Result struct {
	Mesh    Mesh
	Sources []VertexSource

	InputVertices   int
	InputTriangles  int
	OutputVertices  int
	OutputTriangles int
	Collapses       int
	// MaxCollapseError is the largest quadric cost accepted, expressed as a
	// distance in model units.
	MaxCollapseError float64
	// LockedVertices counts vertices held in place because another vertex
	// shares their position. Moving one copy of a split vertex and not the
	// other would tear the surface open.
	LockedVertices int
	// BoundingBoxDiagonal is the diagonal of the input bounding box. Error
	// numbers divided by this value compare across models.
	BoundingBoxDiagonal float64
}

type edgeKey struct{ a, b int32 }

type collapseCandidate struct {
	a, b     int32
	stampA   uint32
	stampB   uint32
	cost     float64
	x, y, z  float64
	t        float64
	position int // heap index
}

type candidateHeap []*collapseCandidate

func (h candidateHeap) Len() int           { return len(h) }
func (h candidateHeap) Less(i, j int) bool { return h[i].cost < h[j].cost }
func (h candidateHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i]; h[i].position = i; h[j].position = j }
func (h *candidateHeap) Push(item any)     { *h = append(*h, item.(*collapseCandidate)) }
func (h *candidateHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// Simplify reduces mesh to the requested triangle budget.
func Simplify(mesh Mesh, opts Options) Result {
	vertexCount := mesh.VertexCount()
	triangleCount := mesh.TriangleCount()
	result := Result{
		InputVertices:  vertexCount,
		InputTriangles: triangleCount,
	}
	if vertexCount == 0 || triangleCount == 0 {
		result.Mesh = mesh
		return result
	}

	target := opts.TargetTriangles
	if target <= 0 {
		ratio := opts.TargetRatio
		if ratio <= 0 || ratio >= 1 {
			ratio = 1
		}
		target = int(math.Round(float64(triangleCount) * ratio))
	}
	if target < 1 {
		target = 1
	}

	boundaryWeight := opts.BoundaryWeight
	if boundaryWeight <= 0 {
		boundaryWeight = DefaultBoundaryWeight
	}

	positions := make([]float64, len(mesh.Positions))
	for i, value := range mesh.Positions {
		positions[i] = float64(value)
	}
	diagonal := boundingBoxDiagonal(positions)
	result.BoundingBoxDiagonal = diagonal

	triangles := make([][3]int32, triangleCount)
	for i := 0; i < triangleCount; i++ {
		triangles[i] = [3]int32{
			int32(mesh.Indices[i*3+0]),
			int32(mesh.Indices[i*3+1]),
			int32(mesh.Indices[i*3+2]),
		}
	}

	alive := make([]bool, vertexCount)
	locked := lockSplitVertices(positions)
	for i := range alive {
		alive[i] = true
		if locked[i] {
			result.LockedVertices++
		}
	}
	triangleAlive := make([]bool, triangleCount)
	for i := range triangleAlive {
		triangleAlive[i] = true
	}

	adjacency := make([][]int32, vertexCount)
	for index, tri := range triangles {
		for _, vertex := range tri {
			adjacency[vertex] = append(adjacency[vertex], int32(index))
		}
	}

	quadrics := make([]quadric, vertexCount)
	for index, tri := range triangles {
		a, b, c, area, ok := trianglePlane(positions, tri)
		if !ok {
			triangleAlive[index] = false
			continue
		}
		d := -(a*positions[tri[0]*3] + b*positions[tri[0]*3+1] + c*positions[tri[0]*3+2])
		q := planeQuadric(a, b, c, d, area)
		for _, vertex := range tri {
			quadrics[vertex] = quadrics[vertex].add(q)
		}
	}
	addBoundaryQuadrics(positions, triangles, triangleAlive, quadrics, boundaryWeight)

	stamps := make([]uint32, vertexCount)
	queue := &candidateHeap{}
	heap.Init(queue)
	seen := map[edgeKey]bool{}
	for index, tri := range triangles {
		if !triangleAlive[index] {
			continue
		}
		for i := 0; i < 3; i++ {
			a, b := tri[i], tri[(i+1)%3]
			key := orderedEdge(a, b)
			if seen[key] {
				continue
			}
			seen[key] = true
			if candidate := buildCandidate(positions, quadrics, locked, stamps, key.a, key.b); candidate != nil {
				heap.Push(queue, candidate)
			}
		}
	}

	sources := make([]VertexSource, vertexCount)
	for i := range sources {
		sources[i] = VertexSource{A: int32(i), B: int32(i), T: 0}
	}

	liveTriangles := 0
	for _, ok := range triangleAlive {
		if ok {
			liveTriangles++
		}
	}
	maxError := math.Inf(1)
	if opts.MaxErrorFraction > 0 && diagonal > 0 {
		limit := opts.MaxErrorFraction * diagonal
		maxError = limit * limit
	}

	for liveTriangles > target && queue.Len() > 0 {
		candidate := heap.Pop(queue).(*collapseCandidate)
		if !alive[candidate.a] || !alive[candidate.b] {
			continue
		}
		if stamps[candidate.a] != candidate.stampA || stamps[candidate.b] != candidate.stampB {
			continue
		}
		if candidate.cost > maxError {
			break
		}
		if !collapseIsSafe(positions, triangles, triangleAlive, adjacency, candidate) {
			// A collapse that folds a triangle stays out of the queue until
			// its neighbourhood changes.
			continue
		}
		applyCollapse(positions, triangles, triangleAlive, adjacency, alive, quadrics, sources, candidate, &liveTriangles)
		result.Collapses++
		errorDistance := math.Sqrt(math.Max(0, candidate.cost))
		if errorDistance > result.MaxCollapseError {
			result.MaxCollapseError = errorDistance
		}
		stamps[candidate.b]++
		keep := candidate.b
		for _, neighbour := range neighbourVertices(triangles, triangleAlive, adjacency, keep) {
			stamps[neighbour]++
		}
		for _, neighbour := range neighbourVertices(triangles, triangleAlive, adjacency, keep) {
			if next := buildCandidate(positions, quadrics, locked, stamps, keep, neighbour); next != nil {
				heap.Push(queue, next)
			}
		}
	}

	result.Mesh, result.Sources = compact(positions, triangles, triangleAlive, alive, sources)
	result.OutputVertices = result.Mesh.VertexCount()
	result.OutputTriangles = result.Mesh.TriangleCount()
	return result
}

func orderedEdge(a, b int32) edgeKey {
	if a > b {
		a, b = b, a
	}
	return edgeKey{a, b}
}

// lockSplitVertices marks every vertex whose position another vertex shares.
// Those vertices sit on an attribute seam, and moving one copy without the
// other would tear the mesh.
func lockSplitVertices(positions []float64) []bool {
	count := len(positions) / 3
	locked := make([]bool, count)
	type key [3]float64
	first := make(map[key]int32, count)
	for i := 0; i < count; i++ {
		k := key{positions[i*3], positions[i*3+1], positions[i*3+2]}
		if previous, ok := first[k]; ok {
			locked[i] = true
			locked[previous] = true
			continue
		}
		first[k] = int32(i)
	}
	return locked
}

func trianglePlane(positions []float64, tri [3]int32) (float64, float64, float64, float64, bool) {
	ax, ay, az := positions[tri[0]*3], positions[tri[0]*3+1], positions[tri[0]*3+2]
	bx, by, bz := positions[tri[1]*3], positions[tri[1]*3+1], positions[tri[1]*3+2]
	cx, cy, cz := positions[tri[2]*3], positions[tri[2]*3+1], positions[tri[2]*3+2]
	ux, uy, uz := bx-ax, by-ay, bz-az
	vx, vy, vz := cx-ax, cy-ay, cz-az
	nx := uy*vz - uz*vy
	ny := uz*vx - ux*vz
	nz := ux*vy - uy*vx
	length := math.Sqrt(nx*nx + ny*ny + nz*nz)
	if length < 1e-20 {
		return 0, 0, 0, 0, false
	}
	return nx / length, ny / length, nz / length, length / 2, true
}

// addBoundaryQuadrics constrains open borders. For every edge used by a
// single triangle, the routine adds a plane through the edge and
// perpendicular to that triangle.
func addBoundaryQuadrics(positions []float64, triangles [][3]int32, triangleAlive []bool, quadrics []quadric, weight float64) {
	uses := map[edgeKey]int{}
	for index, tri := range triangles {
		if !triangleAlive[index] {
			continue
		}
		for i := 0; i < 3; i++ {
			uses[orderedEdge(tri[i], tri[(i+1)%3])]++
		}
	}
	for index, tri := range triangles {
		if !triangleAlive[index] {
			continue
		}
		nx, ny, nz, _, ok := trianglePlane(positions, tri)
		if !ok {
			continue
		}
		for i := 0; i < 3; i++ {
			a, b := tri[i], tri[(i+1)%3]
			if uses[orderedEdge(a, b)] != 1 {
				continue
			}
			ex := positions[b*3] - positions[a*3]
			ey := positions[b*3+1] - positions[a*3+1]
			ez := positions[b*3+2] - positions[a*3+2]
			// The constraint plane holds the edge and stands perpendicular to
			// the triangle.
			px := ey*nz - ez*ny
			py := ez*nx - ex*nz
			pz := ex*ny - ey*nx
			length := math.Sqrt(px*px + py*py + pz*pz)
			if length < 1e-20 {
				continue
			}
			px, py, pz = px/length, py/length, pz/length
			d := -(px*positions[a*3] + py*positions[a*3+1] + pz*positions[a*3+2])
			q := planeQuadric(px, py, pz, d, weight)
			quadrics[a] = quadrics[a].add(q)
			quadrics[b] = quadrics[b].add(q)
		}
	}
}

// buildCandidate scores one edge and picks the cheapest legal placement.
func buildCandidate(positions []float64, quadrics []quadric, locked []bool, stamps []uint32, a, b int32) *collapseCandidate {
	if a == b {
		return nil
	}
	// The collapse removes a and keeps b. A locked vertex may serve as the
	// survivor but must not move.
	if locked[a] && locked[b] {
		return nil
	}
	if locked[a] {
		a, b = b, a
	}
	sum := quadrics[a].add(quadrics[b])
	ax, ay, az := positions[a*3], positions[a*3+1], positions[a*3+2]
	bx, by, bz := positions[b*3], positions[b*3+1], positions[b*3+2]

	type placement struct {
		x, y, z float64
		cost    float64
	}
	best := placement{x: bx, y: by, z: bz, cost: sum.evaluate(bx, by, bz)}
	if !locked[b] {
		options := []placement{
			{x: ax, y: ay, z: az},
			{x: (ax + bx) / 2, y: (ay + by) / 2, z: (az + bz) / 2},
		}
		if x, y, z, ok := sum.optimum(); ok {
			options = append(options, placement{x: x, y: y, z: z})
		}
		for _, option := range options {
			option.cost = sum.evaluate(option.x, option.y, option.z)
			if option.cost < best.cost {
				best = option
			}
		}
	}

	// The blend factor projects the placement onto the edge so a caller can
	// carry every other vertex attribute.
	ex, ey, ez := bx-ax, by-ay, bz-az
	lengthSquared := ex*ex + ey*ey + ez*ez
	t := 1.0
	if lengthSquared > 0 {
		t = ((best.x-ax)*ex + (best.y-ay)*ey + (best.z-az)*ez) / lengthSquared
		t = math.Max(0, math.Min(1, t))
	}
	return &collapseCandidate{
		a: a, b: b,
		stampA: stamps[a], stampB: stamps[b],
		cost: math.Max(0, best.cost),
		x:    best.x, y: best.y, z: best.z,
		t: t,
	}
}

// collapseIsSafe rejects a collapse that would fold a neighbouring triangle
// over on itself.
func collapseIsSafe(positions []float64, triangles [][3]int32, triangleAlive []bool, adjacency [][]int32, candidate *collapseCandidate) bool {
	for _, index := range adjacency[candidate.a] {
		if !triangleAlive[index] {
			continue
		}
		tri := triangles[index]
		if containsVertex(tri, candidate.b) {
			continue // The triangle disappears with the collapse.
		}
		beforeX, beforeY, beforeZ, _, ok := trianglePlane(positions, tri)
		if !ok {
			continue
		}
		afterX, afterY, afterZ, ok := movedTriangleNormal(positions, tri, candidate)
		if !ok {
			return false
		}
		if beforeX*afterX+beforeY*afterY+beforeZ*afterZ <= 0 {
			return false
		}
	}
	return true
}

func movedTriangleNormal(positions []float64, tri [3]int32, candidate *collapseCandidate) (float64, float64, float64, bool) {
	var px, py, pz [3]float64
	for i, vertex := range tri {
		if vertex == candidate.a {
			px[i], py[i], pz[i] = candidate.x, candidate.y, candidate.z
			continue
		}
		if vertex == candidate.b {
			px[i], py[i], pz[i] = candidate.x, candidate.y, candidate.z
			continue
		}
		px[i], py[i], pz[i] = positions[vertex*3], positions[vertex*3+1], positions[vertex*3+2]
	}
	ux, uy, uz := px[1]-px[0], py[1]-py[0], pz[1]-pz[0]
	vx, vy, vz := px[2]-px[0], py[2]-py[0], pz[2]-pz[0]
	nx := uy*vz - uz*vy
	ny := uz*vx - ux*vz
	nz := ux*vy - uy*vx
	length := math.Sqrt(nx*nx + ny*ny + nz*nz)
	if length < 1e-20 {
		return 0, 0, 0, false
	}
	return nx / length, ny / length, nz / length, true
}

func containsVertex(tri [3]int32, vertex int32) bool {
	return tri[0] == vertex || tri[1] == vertex || tri[2] == vertex
}

func applyCollapse(
	positions []float64,
	triangles [][3]int32,
	triangleAlive []bool,
	adjacency [][]int32,
	alive []bool,
	quadrics []quadric,
	sources []VertexSource,
	candidate *collapseCandidate,
	liveTriangles *int,
) {
	a, b := candidate.a, candidate.b
	positions[b*3] = candidate.x
	positions[b*3+1] = candidate.y
	positions[b*3+2] = candidate.z
	quadrics[b] = quadrics[a].add(quadrics[b])
	sources[b] = VertexSource{A: sources[a].A, B: sources[b].A, T: candidate.t}
	alive[a] = false

	for _, index := range adjacency[a] {
		if !triangleAlive[index] {
			continue
		}
		tri := &triangles[index]
		for i := range tri {
			if tri[i] == a {
				tri[i] = b
			}
		}
		if tri[0] == tri[1] || tri[1] == tri[2] || tri[0] == tri[2] {
			triangleAlive[index] = false
			*liveTriangles--
			continue
		}
		adjacency[b] = append(adjacency[b], index)
	}
	adjacency[a] = nil
}

func neighbourVertices(triangles [][3]int32, triangleAlive []bool, adjacency [][]int32, vertex int32) []int32 {
	seen := map[int32]bool{}
	var out []int32
	for _, index := range adjacency[vertex] {
		if !triangleAlive[index] {
			continue
		}
		for _, other := range triangles[index] {
			if other == vertex || seen[other] {
				continue
			}
			seen[other] = true
			out = append(out, other)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func compact(positions []float64, triangles [][3]int32, triangleAlive []bool, alive []bool, sources []VertexSource) (Mesh, []VertexSource) {
	remap := make([]int32, len(alive))
	for i := range remap {
		remap[i] = -1
	}
	var out Mesh
	var outSources []VertexSource
	for _, index := range indexOrder(triangles, triangleAlive) {
		tri := triangles[index]
		for _, vertex := range tri {
			if remap[vertex] >= 0 {
				continue
			}
			remap[vertex] = int32(len(outSources))
			out.Positions = append(out.Positions,
				float32(positions[vertex*3]),
				float32(positions[vertex*3+1]),
				float32(positions[vertex*3+2]),
			)
			outSources = append(outSources, sources[vertex])
		}
		out.Indices = append(out.Indices,
			uint32(remap[tri[0]]),
			uint32(remap[tri[1]]),
			uint32(remap[tri[2]]),
		)
	}
	return out, outSources
}

func indexOrder(triangles [][3]int32, triangleAlive []bool) []int {
	out := make([]int, 0, len(triangles))
	for index := range triangles {
		if triangleAlive[index] {
			out = append(out, index)
		}
	}
	return out
}

func boundingBoxDiagonal(positions []float64) float64 {
	if len(positions) < 3 {
		return 0
	}
	minX, minY, minZ := positions[0], positions[1], positions[2]
	maxX, maxY, maxZ := minX, minY, minZ
	for i := 3; i < len(positions); i += 3 {
		minX = math.Min(minX, positions[i])
		minY = math.Min(minY, positions[i+1])
		minZ = math.Min(minZ, positions[i+2])
		maxX = math.Max(maxX, positions[i])
		maxY = math.Max(maxY, positions[i+1])
		maxZ = math.Max(maxZ, positions[i+2])
	}
	dx, dy, dz := maxX-minX, maxY-minY, maxZ-minZ
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}
