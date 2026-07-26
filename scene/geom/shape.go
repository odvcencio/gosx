package geom

import (
	"math"
	"slices"

	"m31labs.dev/gosx/scene/earcut"
)

// This file builds the two generators that start from an authored 2D outline:
// a flat cap and a solid extrusion. Both triangulate the outline with package
// scene/earcut, the same triangulator PolygonGeometry already uses.
//
// An outline lies in the XZ plane. Point pairs run (x0, z0, x1, z1, ...). A
// hole ring is cut out of the outline. A ring with one point acts as a Steiner
// point, which is what earcut does.

// Contour is one closed outline plus its holes, in the XZ plane.
type Contour struct {
	// Outline holds the outer ring as flat 2D pairs.
	Outline []float64
	// Holes holds the rings cut out of the outline, each as flat 2D pairs.
	Holes [][]float64
}

// ExtrudeOptions names the solid an extrusion builds from a contour.
type ExtrudeOptions struct {
	// Depth is the height of the solid along +Y. A value at or below zero
	// selects 1.
	Depth float64
	// Bevel turns on a rounded lip at the top face and the bottom face.
	Bevel bool
	// BevelThickness is how far the lip reaches along Y. A value at or below
	// zero selects a tenth of the depth.
	BevelThickness float64
	// BevelSize is how far the lip pulls in from the outline. A value at or
	// below zero selects BevelThickness.
	BevelSize float64
	// BevelSegments is the number of steps in the lip. A value at or below zero
	// selects 3. The lip is capped at 16 steps.
	BevelSegments int
}

func (o ExtrudeOptions) resolved() ExtrudeOptions {
	o.Depth = PositiveOr(o.Depth, 1)
	if !o.Bevel {
		o.BevelThickness = 0
		o.BevelSize = 0
		o.BevelSegments = 0
		return o
	}
	o.BevelThickness = PositiveOr(o.BevelThickness, o.Depth*0.1)
	o.BevelSize = PositiveOr(o.BevelSize, o.BevelThickness)
	o.BevelSegments = ClampInt(o.BevelSegments, 3, 1, 16)
	if o.BevelThickness*2 >= o.Depth {
		// The two lips would meet or cross, which folds the wall inside out.
		// Hold them apart so the solid keeps a straight middle.
		o.BevelThickness = o.Depth * 0.25
	}
	return o
}

// Shape builds a flat cap from one contour, lying in the XZ plane at the given
// elevation with a +Y normal. It returns nil for a degenerate contour.
func Shape(contour Contour, y float64, want Attribute) *Mesh {
	points, holeStarts := flattenContour(contour)
	if len(points) < 6 {
		return nil
	}
	indices := orientUp(points, earcut.Triangulate(points, holeStarts, 2))
	if len(indices) == 0 {
		return nil
	}
	minX, minZ, maxX, maxZ := contourExtent(points)
	spanX := math.Max(maxX-minX, 1e-12)
	spanZ := math.Max(maxZ-minZ, 1e-12)

	b := newBuilder(want, len(points)/2)
	up := vec3{0, 1, 0}
	for i := 0; i+2 <= len(points); i += 2 {
		x, z := points[i], points[i+1]
		b.emit(vertex{
			position: vec3{X: x, Y: y, Z: z},
			normal:   up,
			uv:       vec2{U: (x - minX) / spanX, V: (z - minZ) / spanZ},
			color:    discColor(0.5),
		})
	}
	b.mesh.Indices = indices
	return b.build()
}

// Extrude builds a solid by sweeping one contour along +Y. The bottom cap sits
// at y=0 and the top cap at y=Depth. Both caps and every side wall carry
// outward normals. It returns nil for a degenerate contour.
//
// With Bevel on, the caps pull in by BevelSize and the lip climbs
// BevelThickness, so the solid gets a rounded edge instead of a sharp one. The
// inset uses the angle bisector at each outline point, which is the same
// construction three.js ExtrudeGeometry uses. A deep bevel on a thin contour can
// fold the inset ring onto itself; keep BevelSize below the contour's own
// thickness.
func Extrude(contour Contour, opts ExtrudeOptions, want Attribute) *Mesh {
	opts = opts.resolved()
	points, holeStarts := flattenContour(contour)
	if len(points) < 6 {
		return nil
	}
	capIndices := orientUp(points, earcut.Triangulate(points, holeStarts, 2))
	if len(capIndices) == 0 {
		return nil
	}
	rings := splitRings(points, holeStarts)
	if len(rings) == 0 {
		return nil
	}

	minX, minZ, maxX, maxZ := contourExtent(points)
	spanX := math.Max(maxX-minX, 1e-12)
	spanZ := math.Max(maxZ-minZ, 1e-12)

	levels := extrudeLevels(opts)
	// Resolve the inset ring of every level once. A ring lookup inside the wall
	// loop would rebuild the same list for every ring of every level.
	inset := make([][]float64, len(levels))
	for i, lv := range levels {
		if lv.inset == 0 {
			inset[i] = points
			continue
		}
		inset[i] = insetContour(points, rings, lv.inset)
	}

	b := newBuilder(want, len(points)/2*(len(levels)+2))

	// Both caps use the same triangulation. The bottom cap sits at the first
	// level and is wound for a -Y normal; the top cap sits at the last level and
	// is wound for +Y.
	emitCap := func(flat []float64, y float64, up bool) {
		base := b.mesh.VertexCount()
		normal := vec3{0, 1, 0}
		if !up {
			normal = vec3{0, -1, 0}
		}
		for i := 0; i+2 <= len(flat); i += 2 {
			x, z := flat[i], flat[i+1]
			b.emit(vertex{
				position: vec3{X: x, Y: y, Z: z},
				normal:   normal,
				uv:       vec2{U: (x - minX) / spanX, V: (z - minZ) / spanZ},
				color:    discColor(0.5),
			})
		}
		for i := 0; i+3 <= len(capIndices); i += 3 {
			if up {
				b.index(base+capIndices[i], base+capIndices[i+1], base+capIndices[i+2])
				continue
			}
			// orientUp wound the cap for +Y. Reverse it for the bottom.
			b.index(base+capIndices[i], base+capIndices[i+2], base+capIndices[i+1])
		}
	}

	last := len(levels) - 1
	emitCap(inset[0], levels[0].height, false)
	emitCap(inset[last], levels[last].height, true)

	// Walls. Every ring sweeps through every level, so one wall is a grid of
	// (levels) rows by (ring points + 1) columns. The extra column repeats the
	// first point, which lets the texture run once around without a seam vertex
	// shared between u=1 and u=0.
	for _, ring := range rings {
		count := len(ring)
		outward := ringOutwardNormals(points, ring)
		distance := make([]float64, count+1)
		for i := 0; i < count; i++ {
			next := (i + 1) % count
			x0, z0 := points[ring[i]*2], points[ring[i]*2+1]
			x1, z1 := points[ring[next]*2], points[ring[next]*2+1]
			distance[i+1] = distance[i] + math.Hypot(x1-x0, z1-z0)
		}
		perimeter := math.Max(distance[count], 1e-12)

		base := b.mesh.VertexCount()
		stride := count + 1
		for levelIndex, lv := range levels {
			source := inset[levelIndex]
			radial, vertical := levelProfileNormal(levels, levelIndex)
			for step := 0; step <= count; step++ {
				corner := ring[step%count]
				x, z := source[corner*2], source[corner*2+1]
				out := outward[step%count]
				b.emit(vertex{
					position: vec3{X: x, Y: lv.height, Z: z},
					normal:   normalize(vec3{X: out.X * radial, Y: vertical, Z: out.Z * radial}),
					uv:       vec2{U: distance[step] / perimeter, V: lv.height / opts.Depth},
					color:    discColor(0.5),
				})
			}
		}
		for levelIndex := 0; levelIndex+1 < len(levels); levelIndex++ {
			for step := 0; step < count; step++ {
				low := base + levelIndex*stride + step
				high := base + (levelIndex+1)*stride + step
				b.index(low, high, low+1)
				b.index(low+1, high, high+1)
			}
		}
	}
	return b.build()
}

// extrudeLevel is one ring of the swept profile. inset is how far the ring pulls
// in from the authored outline; height is its elevation.
type extrudeLevel struct {
	inset  float64
	height float64
}

// extrudeLevels lists the rings the wall sweeps through, from the bottom cap to
// the top cap. Without a bevel there are two. With a bevel the lip at each end
// follows a quarter circle, so a step turns the profile by an equal angle.
func extrudeLevels(opts ExtrudeOptions) []extrudeLevel {
	if !opts.Bevel {
		return []extrudeLevel{{height: 0}, {height: opts.Depth}}
	}
	levels := make([]extrudeLevel, 0, opts.BevelSegments*2+2)
	// The bottom lip starts fully pulled in at y=0 and reaches the authored
	// outline at y=BevelThickness. The pair (1-sin, 1-cos) traces a quarter
	// circle, so the lip is round.
	for step := 0; step <= opts.BevelSegments; step++ {
		angle := float64(step) / float64(opts.BevelSegments) * math.Pi / 2
		levels = append(levels, extrudeLevel{
			inset:  opts.BevelSize * (1 - math.Sin(angle)),
			height: opts.BevelThickness * (1 - math.Cos(angle)),
		})
	}
	// The top lip mirrors the bottom one about the middle of the solid.
	for step := opts.BevelSegments; step >= 0; step-- {
		angle := float64(step) / float64(opts.BevelSegments) * math.Pi / 2
		levels = append(levels, extrudeLevel{
			inset:  opts.BevelSize * (1 - math.Sin(angle)),
			height: opts.Depth - opts.BevelThickness*(1-math.Cos(angle)),
		})
	}
	return levels
}

// levelProfileNormal returns the wall normal at one level, split into the part
// along the outward horizontal direction and the part along Y.
//
// Read the profile in the plane spanned by the outward direction and Y. A step
// from one level to the next moves -d(inset) outward and +d(height) up, so the
// surface tangent is (-d(inset), d(height)). Turning that tangent a quarter turn
// toward the outside gives the normal (d(height), d(inset)). A level in the
// middle of the wall averages the step below it and the step above it, which
// makes a beveled lip shade round instead of faceted.
func levelProfileNormal(levels []extrudeLevel, index int) (radial, vertical float64) {
	add := func(from, to int) {
		radial += levels[to].height - levels[from].height
		vertical += levels[to].inset - levels[from].inset
	}
	if index > 0 {
		add(index-1, index)
	}
	if index+1 < len(levels) {
		add(index, index+1)
	}
	length := math.Hypot(radial, vertical)
	if length <= 0 || math.IsNaN(length) {
		return 1, 0
	}
	return radial / length, vertical / length
}

// orientUp returns the triangulation wound so every triangle faces +Y.
//
// Package scene/earcut always emits counter-clockwise triangles in the 2D input
// plane, whatever direction the author wound the ring. A counter-clockwise loop
// in (x, z) has a -Y face normal, so the raw output faces down. Measure the
// total signed area and reverse every triangle when it points the wrong way.
//
// Do not skip this step. A raycaster tests both sides of a triangle, and the
// browser runtime does not cull back faces today, so a wrongly wound cap looks
// right in every current test and turns invisible the day back-face culling
// arrives.
func orientUp(points []float64, indices []int) []int {
	if len(indices) < 3 {
		return nil
	}
	out := append([]int(nil), indices...)
	total := 0.0
	for i := 0; i+3 <= len(out); i += 3 {
		ax, az := points[out[i]*2], points[out[i]*2+1]
		bx, bz := points[out[i+1]*2], points[out[i+1]*2+1]
		cx, cz := points[out[i+2]*2], points[out[i+2]*2+1]
		total += (bx-ax)*(cz-az) - (bz-az)*(cx-ax)
	}
	if total > 0 {
		// The loop runs counter-clockwise in (x, z), which faces -Y.
		for i := 0; i+3 <= len(out); i += 3 {
			out[i+1], out[i+2] = out[i+2], out[i+1]
		}
	}
	return out
}

// flattenContour joins the outline and the holes into one point list and
// returns the index each hole starts at, as earcut wants them.
func flattenContour(contour Contour) ([]float64, []int) {
	points := append([]float64(nil), contour.Outline...)
	if len(points)%2 != 0 {
		points = points[:len(points)-1]
	}
	holeStarts := make([]int, 0, len(contour.Holes))
	for _, hole := range contour.Holes {
		if len(hole) < 2 {
			continue
		}
		holeStarts = append(holeStarts, len(points)/2)
		points = append(points, hole[:len(hole)/2*2]...)
	}
	return points, holeStarts
}

// splitRings returns the point numbers of each ring, oriented so one wall
// formula serves every ring.
//
// A ring that runs counter-clockwise in (x, z) hands the wall builder an
// outward normal that points away from that ring's own interior. That is what
// an outline wants. A hole wants the opposite: the solid lies outside the hole,
// so the wall must face into the hole. Reversing a hole ring flips its normal
// without any second formula.
//
// A ring with fewer than three points is dropped, because it bounds no wall.
func splitRings(points []float64, holeStarts []int) [][]int {
	total := len(points) / 2
	bounds := append([]int{0}, holeStarts...)
	bounds = append(bounds, total)
	rings := make([][]int, 0, len(bounds)-1)
	for i := 0; i+1 < len(bounds); i++ {
		start, end := bounds[i], bounds[i+1]
		if end-start < 3 {
			continue
		}
		ring := make([]int, 0, end-start)
		for index := start; index < end; index++ {
			ring = append(ring, index)
		}
		counterClockwise := ringSignedArea(points, ring) > 0
		wantCounterClockwise := i == 0
		if counterClockwise != wantCounterClockwise {
			slices.Reverse(ring)
		}
		rings = append(rings, ring)
	}
	return rings
}

func contourExtent(points []float64) (minX, minZ, maxX, maxZ float64) {
	minX, minZ = math.Inf(1), math.Inf(1)
	maxX, maxZ = math.Inf(-1), math.Inf(-1)
	for i := 0; i+2 <= len(points); i += 2 {
		minX = math.Min(minX, points[i])
		maxX = math.Max(maxX, points[i])
		minZ = math.Min(minZ, points[i+1])
		maxZ = math.Max(maxZ, points[i+1])
	}
	if math.IsInf(minX, 1) {
		return 0, 0, 0, 0
	}
	return minX, minZ, maxX, maxZ
}

// ringSignedArea returns twice the signed area of one ring in (x, z). A positive
// value means the ring runs counter-clockwise in (x, z), which is a -Y face.
func ringSignedArea(points []float64, ring []int) float64 {
	total := 0.0
	for i := range ring {
		next := (i + 1) % len(ring)
		x0, z0 := points[ring[i]*2], points[ring[i]*2+1]
		x1, z1 := points[ring[next]*2], points[ring[next]*2+1]
		total += x0*z1 - x1*z0
	}
	return total
}

// ringOutwardNormals returns the outward horizontal normal at each ring point.
//
// The ring arrives already oriented from splitRings, so one formula serves every
// ring: an edge running (dx, dz) carries the normal (dz, -dx). That is the same
// direction the wall winding gives, so the shaded normal and the geometric
// normal always agree.
//
// Each point averages the normals of the two edges that meet there, which keeps
// a corner smooth.
func ringOutwardNormals(points []float64, ring []int) []vec3 {
	count := len(ring)
	out := make([]vec3, count)
	edgeNormal := func(from, to int) vec3 {
		x0, z0 := points[from*2], points[from*2+1]
		x1, z1 := points[to*2], points[to*2+1]
		dx, dz := x1-x0, z1-z0
		return normalize(vec3{X: dz, Y: 0, Z: -dx})
	}
	for i := 0; i < count; i++ {
		prev := (i - 1 + count) % count
		next := (i + 1) % count
		a := edgeNormal(ring[prev], ring[i])
		c := edgeNormal(ring[i], ring[next])
		out[i] = normalize(vec3{X: a.X + c.X, Y: 0, Z: a.Z + c.Z})
	}
	return out
}

// insetContour pulls every ring in by the given distance along its inward
// bisector. The result keeps the point order, so the cap triangulation still
// applies.
func insetContour(points []float64, rings [][]int, inset float64) []float64 {
	out := append([]float64(nil), points...)
	for _, ring := range rings {
		normals := ringOutwardNormals(points, ring)
		for i, index := range ring {
			// The bisector shortens as a corner sharpens. Divide by the cosine of
			// the half angle so the inset stays uniform along the edges.
			scale := 1.0
			prev := (i - 1 + len(ring)) % len(ring)
			next := (i + 1) % len(ring)
			edge0 := edgeDirection(points, ring[prev], ring[i])
			edge1 := edgeDirection(points, ring[i], ring[next])
			cos := edge0.X*edge1.X + edge0.Z*edge1.Z
			if half := math.Sqrt(math.Max((1+cos)/2, 1e-6)); half > 1e-3 {
				scale = 1 / half
			}
			if scale > 4 {
				scale = 4
			}
			out[index*2] -= normals[i].X * inset * scale
			out[index*2+1] -= normals[i].Z * inset * scale
		}
	}
	return out
}

func edgeDirection(points []float64, from, to int) vec3 {
	x0, z0 := points[from*2], points[from*2+1]
	x1, z1 := points[to*2], points[to*2+1]
	return normalize(vec3{X: x1 - x0, Y: 0, Z: z1 - z0})
}
