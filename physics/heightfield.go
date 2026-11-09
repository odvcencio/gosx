package physics

import (
	"fmt"
	"math"
)

// Heightfield collider: a regular grid of heights in collider-local space,
// collided and raycast as two triangles per cell without ever storing the
// triangles. Memory is one float per sample, so a 1025 x 1025 terrain costs
// 8 MB instead of the ~200 MB a triangle mesh of the same grid would.
//
// Layout. Columns run along local +X and rows along local +Z. The grid is
// centred on the collider origin: column 0 sits at x = -Width/2, the last
// column at +Width/2, row 0 at z = -Depth/2 and the last row at +Depth/2.
// Heights[row*Columns+column] is the local Y of that sample.
//
// Each cell is split on the diagonal from (column, row) to (column+1, row+1),
// the same split scene.Terrain uses for its height queries and its finest
// render level, so a body rests on the surface the player sees.

// heightfieldMaxTriangles bounds the triangles one narrowphase query gathers.
// A body that spans more cells than this collides against the nearest ones
// only; give large bodies a coarser heightfield.
const heightfieldMaxTriangles = 512

type heightfield struct {
	columns int
	rows    int
	heights []float64
	minX    float64
	minZ    float64
	cellX   float64
	cellZ   float64
	bounds  AABB
	// scratch holds the triangles of one narrowphase query. Steps run on one
	// goroutine, so a per-collider buffer avoids a per-query allocation.
	scratch []meshTriangle
}

func newHeightfield(config ColliderConfig) (*heightfield, error) {
	columns, rows := config.Columns, config.Rows
	if columns < 2 || rows < 2 {
		return nil, fmt.Errorf("physics: heightfield collider needs Columns >= 2 and Rows >= 2, got %d x %d", columns, rows)
	}
	if len(config.Heights) != columns*rows {
		return nil, fmt.Errorf("physics: heightfield collider has %d Heights, want Columns*Rows = %d", len(config.Heights), columns*rows)
	}
	if !(config.Width > 0) || !(config.Depth > 0) || math.IsInf(config.Width, 0) || math.IsInf(config.Depth, 0) {
		return nil, fmt.Errorf("physics: heightfield collider needs finite Width > 0 and Depth > 0, got %v x %v", config.Width, config.Depth)
	}
	h := &heightfield{
		columns: columns,
		rows:    rows,
		heights: make([]float64, len(config.Heights)),
		minX:    -config.Width / 2,
		minZ:    -config.Depth / 2,
		cellX:   config.Width / float64(columns-1),
		cellZ:   config.Depth / float64(rows-1),
	}
	low, high := math.Inf(1), math.Inf(-1)
	for i, value := range config.Heights {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("physics: heightfield collider height %d is not finite", i)
		}
		h.heights[i] = value
		low = math.Min(low, value)
		high = math.Max(high, value)
	}
	h.bounds = AABB{Min: Vec3{X: h.minX, Y: low, Z: h.minZ}, Max: Vec3{X: -h.minX, Y: high, Z: -h.minZ}}
	return h, nil
}

func (h *heightfield) height(column, row int) float64 {
	return h.heights[row*h.columns+column]
}

func (h *heightfield) point(column, row int) Vec3 {
	return Vec3{
		X: h.minX + float64(float64(column)*h.cellX),
		Y: h.height(column, row),
		Z: h.minZ + float64(float64(row)*h.cellZ),
	}
}

// heightAt returns the local surface height at local (x, z), clamped to the
// grid edge.
func (h *heightfield) heightAt(x, z float64) float64 {
	gx := clampFloat((x-h.minX)/h.cellX, 0, float64(h.columns-1))
	gz := clampFloat((z-h.minZ)/h.cellZ, 0, float64(h.rows-1))
	i := min(int(math.Floor(gx)), h.columns-2)
	j := min(int(math.Floor(gz)), h.rows-2)
	fx, fz := gx-float64(i), gz-float64(j)
	h00, h10 := h.height(i, j), h.height(i+1, j)
	h01, h11 := h.height(i, j+1), h.height(i+1, j+1)
	if fx >= fz {
		return h00 + float64(fx*(h10-h00)) + float64(fz*(h11-h10))
	}
	return h00 + float64(fz*(h01-h00)) + float64(fx*(h11-h01))
}

func newMeshTriangle(a, b, c Vec3) meshTriangle {
	tri := meshTriangle{a: a, b: b, c: c}
	if n := b.Sub(a).Cross(c.Sub(a)); n.Len2() > 1e-24 {
		tri.normal = n.Normalize()
	}
	return tri
}

// cellTriangle returns one of the two triangles of a cell, wound so its
// normal points up. part 0 is (i,j), (i+1,j+1), (i+1,j); part 1 is
// (i,j), (i,j+1), (i+1,j+1). It carries no edge filter.
func (h *heightfield) cellTriangle(i, j, part int) meshTriangle {
	if part == 0 {
		return newMeshTriangle(h.point(i, j), h.point(i+1, j+1), h.point(i+1, j))
	}
	return newMeshTriangle(h.point(i, j), h.point(i, j+1), h.point(i+1, j+1))
}

// triangle returns a cell triangle with its internal-edge filter set: an edge
// shared with a neighbour that makes a flat or concave join pushes along the
// face, exactly as markInternalEdges does for a triangle mesh. Grid border
// edges have no neighbour and keep their edge normals.
func (h *heightfield) triangle(i, j, part int) meshTriangle {
	tri := h.cellTriangle(i, j, part)
	type neighbour struct {
		i, j, part int
	}
	// Edge order follows the corner order: edge 0 is a-b, 1 is b-c, 2 is c-a.
	var across [3]neighbour
	if part == 0 {
		across = [3]neighbour{{i, j, 1}, {i + 1, j, 1}, {i, j - 1, 1}}
	} else {
		across = [3]neighbour{{i - 1, j, 0}, {i, j + 1, 0}, {i, j, 0}}
	}
	for edge, n := range across {
		if n.i < 0 || n.j < 0 || n.i >= h.columns-1 || n.j >= h.rows-1 {
			continue
		}
		if !edgeIsConvex(tri, h.cellTriangle(n.i, n.j, n.part)) {
			tri.edgeFiltered |= 1 << uint(edge)
		}
	}
	return tri
}

// cellRange returns the inclusive cell index range a local XZ interval
// overlaps, and false when it misses the grid.
func (h *heightfield) cellRange(minX, maxX, minZ, maxZ float64) (i0, i1, j0, j1 int, ok bool) {
	if maxX < h.bounds.Min.X || minX > h.bounds.Max.X || maxZ < h.bounds.Min.Z || minZ > h.bounds.Max.Z {
		return 0, 0, 0, 0, false
	}
	i0 = max(0, int(math.Floor((minX-h.minX)/h.cellX)))
	i1 = min(h.columns-2, int(math.Floor((maxX-h.minX)/h.cellX)))
	j0 = max(0, int(math.Floor((minZ-h.minZ)/h.cellZ)))
	j1 = min(h.rows-2, int(math.Floor((maxZ-h.minZ)/h.cellZ)))
	return i0, i1, j0, j1, i0 <= i1 && j0 <= j1
}

// gather appends the local triangles whose bounds overlap a local box, in row
// then column order, so a query is deterministic.
func (h *heightfield) gather(box AABB, out []meshTriangle) []meshTriangle {
	i0, i1, j0, j1, ok := h.cellRange(box.Min.X, box.Max.X, box.Min.Z, box.Max.Z)
	if !ok {
		return out
	}
	for j := j0; j <= j1; j++ {
		for i := i0; i <= i1; i++ {
			low := math.Min(math.Min(h.height(i, j), h.height(i+1, j)), math.Min(h.height(i, j+1), h.height(i+1, j+1)))
			high := math.Max(math.Max(h.height(i, j), h.height(i+1, j)), math.Max(h.height(i, j+1), h.height(i+1, j+1)))
			if high < box.Min.Y || low > box.Max.Y {
				continue
			}
			for part := 0; part < 2; part++ {
				if len(out) >= heightfieldMaxTriangles {
					return out
				}
				tri := h.triangle(i, j, part)
				if tri.normal.Len2() == 0 || !tri.bounds().Overlaps(box) {
					continue
				}
				out = append(out, tri)
			}
		}
	}
	return out
}

// collideHeightfieldShape mirrors collideMeshShape: one manifold per touching
// triangle, keeping the deepest meshMaxManifolds.
func collideHeightfieldShape(field, other *Collider, out []ContactManifold) []ContactManifold {
	h := field.heightfield
	if h == nil || other == nil || other.Shape == ShapePlane || other.Shape == ShapeTriangleMesh || other.Shape == ShapeHeightfield {
		return out
	}
	otherShape, ok := newConvexShape(other)
	if !ok {
		return out
	}
	box := other.AABB()
	if !box.IsFinite() {
		return out
	}
	h.scratch = h.gather(field.localAABBFromWorld(box.Expand(contactTolerance)), h.scratch[:0])
	if len(h.scratch) == 0 {
		return out
	}
	center := field.WorldCenter()
	basis := mat3FromQuat(field.WorldRotation())
	sphereRadius, sphereIsBall := 0.0, other.Shape == ShapeSphere
	if sphereIsBall {
		sphereRadius = math.Abs(other.Radius)
	}
	sphereCenter := other.WorldCenter()
	var best [meshMaxManifolds]epaResult
	bestCount := 0
	for _, local := range h.scratch {
		worldA := center.Add(basis.mul(local.a))
		worldB := center.Add(basis.mul(local.b))
		worldC := center.Add(basis.mul(local.c))
		worldTri := meshTriangle{a: worldA, b: worldB, c: worldC, normal: basis.mul(local.normal), edgeFiltered: local.edgeFiltered}
		if sphereIsBall {
			if result, ok := collideSphereTriangle(sphereCenter, sphereRadius, worldA, worldB, worldC); ok {
				result.Normal = filterMeshContactNormal(worldTri, result.PointA, result.Normal)
				bestCount = keepDeepestResult(&best, bestCount, result)
			}
			continue
		}
		tri := triangleShape(worldA, worldB, worldC)
		simplex, overlap := gjkOverlap(tri, otherShape)
		if !overlap {
			continue
		}
		result, ok := epaPenetration(tri, otherShape, simplex)
		if !ok || result.Normal.Len2() <= epsilon {
			continue
		}
		result.Normal = filterMeshContactNormal(worldTri, result.PointA, result.Normal)
		bestCount = keepDeepestResult(&best, bestCount, result)
	}
	for i := 0; i < bestCount; i++ {
		out = append(out, makeContactManifold(field, other, best[i].Normal, []ContactPoint{
			makeContactPoint(field, other, best[i].ContactPoint(), best[i].Depth),
		}))
	}
	return out
}

// raycastHeightfield walks the cells under the ray's XZ path in order
// (Amanatides and Woo) and returns the first triangle hit.
func raycastHeightfield(c *Collider, ray Ray, maxDistance float64) (RaycastHit, bool) {
	h := c.heightfield
	if h == nil {
		return RaycastHit{}, false
	}
	origin, direction, basis := localRay(c, ray)
	enter, exit, ok := rayBoxSpan(h.bounds, origin, direction, maxDistance)
	if !ok {
		return RaycastHit{}, false
	}
	start := origin.Add(direction.Mul(enter))
	gx := clampFloat((start.X-h.minX)/h.cellX, 0, float64(h.columns-1)-1e-9)
	gz := clampFloat((start.Z-h.minZ)/h.cellZ, 0, float64(h.rows-1)-1e-9)
	i, j := int(math.Floor(gx)), int(math.Floor(gz))
	stepI, stepJ := 0, 0
	nextX, nextZ := math.Inf(1), math.Inf(1)
	deltaX, deltaZ := math.Inf(1), math.Inf(1)
	if direction.X > 0 {
		stepI, deltaX = 1, h.cellX/direction.X
		nextX = enter + float64(float64(float64(i+1)-gx)*h.cellX)/direction.X
	} else if direction.X < 0 {
		stepI, deltaX = -1, -h.cellX/direction.X
		nextX = enter + float64(float64(gx-float64(i))*h.cellX)/-direction.X
	}
	if direction.Z > 0 {
		stepJ, deltaZ = 1, h.cellZ/direction.Z
		nextZ = enter + float64(float64(float64(j+1)-gz)*h.cellZ)/direction.Z
	} else if direction.Z < 0 {
		stepJ, deltaZ = -1, -h.cellZ/direction.Z
		nextZ = enter + float64(float64(gz-float64(j))*h.cellZ)/-direction.Z
	}
	limit := math.Min(exit, maxDistance)
	for i >= 0 && j >= 0 && i < h.columns-1 && j < h.rows-1 {
		best := math.Inf(1)
		var bestNormal Vec3
		for part := 0; part < 2; part++ {
			if t, normal, hit := rayTriangle(origin, direction, h.cellTriangle(i, j, part), limit); hit && t < best {
				best, bestNormal = t, normal
			}
		}
		if !math.IsInf(best, 1) {
			if bestNormal.Dot(direction) > 0 {
				bestNormal = bestNormal.Neg()
			}
			return finishLocalHit(c, ray, basis, bestNormal, best)
		}
		if nextX < nextZ {
			if nextX > limit {
				break
			}
			i += stepI
			nextX += deltaX
		} else {
			if nextZ > limit {
				break
			}
			j += stepJ
			nextZ += deltaZ
		}
	}
	return RaycastHit{}, false
}

// rayBoxSpan clips a ray to a box and returns the entry and exit distances.
func rayBoxSpan(box AABB, origin, direction Vec3, maxDistance float64) (float64, float64, bool) {
	enter, exit := 0.0, maxDistance
	for axis := 0; axis < 3; axis++ {
		o, d := axisComponent(origin, axis), axisComponent(direction, axis)
		lo, hi := axisComponent(box.Min, axis), axisComponent(box.Max, axis)
		if math.Abs(d) < 1e-15 {
			if o < lo || o > hi {
				return 0, 0, false
			}
			continue
		}
		t0, t1 := (lo-o)/d, (hi-o)/d
		if t0 > t1 {
			t0, t1 = t1, t0
		}
		enter, exit = math.Max(enter, t0), math.Min(exit, t1)
		if enter > exit {
			return 0, 0, false
		}
	}
	return enter, exit, true
}

// HeightAt returns the world Y of a heightfield collider's surface below the
// world point (x, z), and false for any other shape or a point off the grid.
// The query follows the collider's offset and rotation.
func (c *Collider) HeightAt(x, z float64) (float64, bool) {
	if c == nil || c.Shape != ShapeHeightfield || c.heightfield == nil || c.invalid != nil {
		return 0, false
	}
	hit, ok := c.Raycast(Ray{Origin: Vec3{X: x, Y: c.AABB().Max.Y + 1, Z: z}, Direction: Vec3{Y: -1}}, math.Inf(1))
	if !ok {
		return 0, false
	}
	return hit.Point.Y, true
}
