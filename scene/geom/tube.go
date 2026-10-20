package geom

import "math"

// This file sweeps a circular cross section along an arbitrary 3D path. The
// result is the tube body Three.js builds with TubeGeometry: one vertex ring
// per path point (plus a duplicated seam ring for a closed path), indexed so
// adjacent rings share vertices.
//
// The mesh is indexed like the torus knot, because the picker walks shared
// vertices and the renderer expands the indices at upload time. Frames come
// from parallel transport rather than a fixed world-up cross product, so the
// cross section does not spin or flip where the path turns vertical.

const (
	tubeDefaultRadialSegments = 8
	tubeMinRadialSegments     = 3
	tubeMaxRadialSegments     = 128
)

// TubeVertexCount reports how many shared vertices Tube emits for a path of
// pathPoints points, without building. radialSegments resolves through the
// same default and clamp Tube applies. An open path needs at least two points
// and a closed path at least three; anything less returns zero.
func TubeVertexCount(pathPoints, radialSegments int, closed bool) int {
	radial := ClampInt(radialSegments, tubeDefaultRadialSegments, tubeMinRadialSegments, tubeMaxRadialSegments)
	if closed {
		if pathPoints < 3 {
			return 0
		}
		return (pathPoints + 1) * (radial + 1)
	}
	if pathPoints < 2 {
		return 0
	}
	return pathPoints * (radial + 1)
}

// Tube sweeps a circle of radius along a 3D centerline and returns the tube
// surface, or nil for input it cannot honor. The flat path holds X, Y, Z per
// point. A closed path treats the last point as connected back to the first
// and must not repeat the first point as its own last point.
//
// The generator rejects paths whose length is not divisible by three, that
// carry a non-finite number, that have too few points, or that hold two equal
// consecutive points (the first and last point of a closed path count as
// consecutive). It never emits NaN or Inf into a buffer.
//
// radius at or below zero falls back to 1. radialSegments falls back to 8 and
// stays inside [3, 128]. Each ring carries radialSegments+1 vertices, so the
// U=0 and U=1 seam vertices stay distinct. want selects which streams fill;
// PositionsOnly leaves Normals and UVs nil.
func Tube(path []float64, radius float64, radialSegments int, closed bool, want Attribute) *Mesh {
	rings := validTubePath(path, closed)
	if rings == 0 {
		return nil
	}
	points := len(path) / 3
	radius = PositiveOr(radius, 1)
	radial := ClampInt(radialSegments, tubeDefaultRadialSegments, tubeMinRadialSegments, tubeMaxRadialSegments)

	centers := make([]vec3, points)
	for i := range centers {
		centers[i] = vec3{X: path[i*3], Y: path[i*3+1], Z: path[i*3+2]}
	}

	// Cumulative centerline distance drives UV V. A closed path adds the
	// closing edge back to the start, so the duplicated seam ring reaches V=1.
	steps := points - 1
	if closed {
		steps = points
	}
	total := 0.0
	distances := make([]float64, steps+1)
	for i := 1; i <= steps; i++ {
		a := centers[i-1]
		b := centers[i%points]
		total += math.Sqrt((b.X-a.X)*(b.X-a.X) + (b.Y-a.Y)*(b.Y-a.Y) + (b.Z-a.Z)*(b.Z-a.Z))
		distances[i] = total
	}
	if total <= 0 {
		return nil
	}

	// One frame per emitted ring: an open path keeps one ring per point, a
	// closed path appends a duplicate of the first ring.
	frameCount := points
	if closed {
		frameCount = points + 1
	}
	normals, binormals := transportFrames(centers, closed, frameCount)

	stride := radial + 1
	b := newBuilder(want, frameCount*stride)
	for i := 0; i < frameCount; i++ {
		center := centers[i%points]
		v := distances[minInt(i, steps)] / total
		for j := 0; j < stride; j++ {
			theta := 2 * math.Pi * float64(j) / float64(radial)
			cos, sin := math.Cos(theta), math.Sin(theta)
			out := addVec(scaleVec(normals[i], cos), scaleVec(binormals[i], sin))
			b.emit(vertex{
				position: addVec(center, scaleVec(out, radius)),
				normal:   out,
				uv:       vec2{U: float64(j) / float64(radial), V: v},
			})
		}
	}
	b.mesh.Indices = make([]int, 0, steps*radial*6)
	for i := 0; i < steps; i++ {
		for j := 0; j < radial; j++ {
			near := i*stride + j
			far := (i+1)*stride + j
			b.index(near, far+1, far)
			b.index(near, near+1, far+1)
		}
	}
	return b.build()
}

// validTubePath validates the flat path and returns the number of path points,
// or zero when the path cannot build a tube. Consecutive duplicates — including
// the wrap-around pair of a closed path — reject the whole path, because they
// would collapse a ring pair and emit degenerate triangles.
func validTubePath(path []float64, closed bool) int {
	if len(path)%3 != 0 {
		return 0
	}
	for _, value := range path {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0
		}
	}
	points := len(path) / 3
	minimum := 2
	if closed {
		minimum = 3
	}
	if points < minimum {
		return 0
	}
	eq := func(i, j int) bool {
		return path[i*3] == path[j*3] && path[i*3+1] == path[j*3+1] && path[i*3+2] == path[j*3+2]
	}
	for i := 0; i+1 < points; i++ {
		if eq(i, i+1) {
			return 0
		}
	}
	if closed && eq(points-1, 0) {
		return 0
	}
	return points
}

// transportFrames parallel-transports one normal/binormal pair along the path.
// Tangents are one-sided at the open endpoints and centered (wrapped for a
// closed path) elsewhere. For a closed path the residual twist between the
// last transported frame and the first spreads evenly over the sweep, so the
// duplicated final ring lands exactly on the first frame.
func transportFrames(centers []vec3, closed bool, frameCount int) ([]vec3, []vec3) {
	points := len(centers)
	tangents := make([]vec3, frameCount)
	pointAt := func(i int) vec3 {
		m := i % points
		if m < 0 {
			m += points
		}
		return centers[m]
	}
	for i := 0; i < frameCount; i++ {
		var tangent vec3
		switch {
		case !closed && i == 0:
			tangent = subVec(pointAt(1), pointAt(0))
		case !closed && i == points-1:
			tangent = subVec(pointAt(points-1), pointAt(points-2))
		default:
			tangent = subVec(pointAt(i+1), pointAt(i-1))
		}
		tangent = normalize(tangent)
		if isFiniteVec(tangent) {
			tangents[i] = tangent
		} else {
			tangents[i] = vec3{Y: 1}
		}
	}

	normals := make([]vec3, frameCount)
	binormals := make([]vec3, frameCount)
	normals[0] = normalize(leastParallelNormal(tangents[0]))
	binormals[0] = crossVec(tangents[0], normals[0])
	for i := 1; i < frameCount; i++ {
		tangent := tangents[i]
		previous := normals[i-1]
		along := dotVec(previous, tangent)
		projected := subVec(previous, scaleVec(tangent, along))
		if lengthSquared(projected) < 1e-18 || !isFiniteVec(projected) {
			// The previous normal runs along this tangent (a sharp reversal).
			// Any finite perpendicular keeps the buffers honest.
			projected = leastParallelNormal(tangent)
		}
		normal := normalize(projected)
		normals[i] = normal
		binormals[i] = normalize(crossVec(tangent, normal))
	}

	if closed {
		last, first := normals[frameCount-1], normals[0]
		turn := math.Atan2(dotVec(crossVec(last, first), tangents[frameCount-1]), dotVec(last, first))
		for i := 1; i < frameCount; i++ {
			angle := turn * float64(i) / float64(frameCount-1)
			cos, sin := math.Cos(angle), math.Sin(angle)
			normal, binormal := normals[i], binormals[i]
			normals[i] = addVec(scaleVec(normal, cos), scaleVec(binormal, sin))
			binormals[i] = subVec(scaleVec(binormal, cos), scaleVec(normal, sin))
		}
		// The last ring duplicates the first center and tangent. Pin its frame
		// to the first after distributing the residual twist so the geometric
		// seam is bit-for-bit closed, not merely equal within float tolerance.
		normals[frameCount-1] = normals[0]
		binormals[frameCount-1] = binormals[0]
	}
	return normals, binormals
}

func lengthSquared(v vec3) float64 { return v.X*v.X + v.Y*v.Y + v.Z*v.Z }

func isFiniteVec(v vec3) bool {
	return !(math.IsNaN(v.X) || math.IsNaN(v.Y) || math.IsNaN(v.Z) ||
		math.IsInf(v.X, 0) || math.IsInf(v.Y, 0) || math.IsInf(v.Z, 0))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
