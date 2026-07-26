package geom

import "math"

// This file builds the two flat discs: a filled circle and an annulus.
//
// Both lie in the XZ plane at y=0 with a +Y normal, which is the ground-plane
// convention the plane primitive and the polygon triangulator already use. The
// winding is clockwise in (x, z), because a clockwise loop in (x, z) gives a +Y
// face normal.
//
// Both meshes are indexed. The rim points are shared, and an indexed disc costs
// about a third of the wire bytes of an expanded one.

// Circle builds a filled disc, or a pie slice when thetaLength is less than a
// full turn.
//
// radius is the disc radius. segments is the number of rim steps. thetaStart is
// the angle of the first rim point, measured from +X toward +Z. thetaLength is
// the angle the disc sweeps. A thetaLength at or below zero means a full turn.
func Circle(radius float64, segments int, thetaStart, thetaLength float64, want Attribute) *Mesh {
	radius = PositiveOr(radius, 1)
	segments = ClampInt(segments, 32, 3, 512)
	thetaLength = sweepOr(thetaLength)

	b := newBuilder(want, segments+2)
	up := vec3{0, 1, 0}
	center := vertex{position: vec3{}, normal: up, uv: vec2{0.5, 0.5}, color: discColor(0)}
	b.emit(center)
	for i := 0; i <= segments; i++ {
		theta := thetaStart + thetaLength*float64(i)/float64(segments)
		cos, sin := math.Cos(theta), math.Sin(theta)
		b.emit(vertex{
			position: vec3{X: radius * cos, Y: 0, Z: radius * sin},
			normal:   up,
			uv:       radialUV(cos, sin),
			color:    discColor(1),
		})
	}
	b.mesh.Indices = make([]int, 0, segments*3)
	for i := 1; i <= segments; i++ {
		// Rim point i+1 comes before rim point i, so the loop runs clockwise in
		// (x, z) and the face normal points to +Y.
		b.index(0, i+1, i)
	}
	return b.build()
}

// CircleVertexCount returns how many vertices Circle emits.
func CircleVertexCount(segments int) int {
	return ClampInt(segments, 32, 3, 512) + 2
}

// Ring builds an annulus, or a ring slice when thetaLength is less than a full
// turn.
//
// innerRadius and outerRadius bound the band. thetaSegments is the number of
// steps around the ring; phiSegments is the number of bands across it.
// thetaStart and thetaLength name the sweep, as they do for Circle.
//
// An inner radius at or above the outer radius returns nil, because such a ring
// has no area and a silent empty mesh would look like a dropped draw.
func Ring(innerRadius, outerRadius float64, thetaSegments, phiSegments int, thetaStart, thetaLength float64, want Attribute) *Mesh {
	outerRadius = PositiveOr(outerRadius, 1)
	if innerRadius < 0 || math.IsNaN(innerRadius) || math.IsInf(innerRadius, 0) {
		innerRadius = 0
	}
	if innerRadius >= outerRadius {
		return nil
	}
	thetaSegments = ClampInt(thetaSegments, 32, 3, 512)
	phiSegments = ClampInt(phiSegments, 1, 1, 128)
	thetaLength = sweepOr(thetaLength)

	stride := thetaSegments + 1
	b := newBuilder(want, (phiSegments+1)*stride)
	up := vec3{0, 1, 0}
	for band := 0; band <= phiSegments; band++ {
		t := float64(band) / float64(phiSegments)
		radius := innerRadius + (outerRadius-innerRadius)*t
		for step := 0; step <= thetaSegments; step++ {
			theta := thetaStart + thetaLength*float64(step)/float64(thetaSegments)
			cos, sin := math.Cos(theta), math.Sin(theta)
			b.emit(vertex{
				position: vec3{X: radius * cos, Y: 0, Z: radius * sin},
				normal:   up,
				uv:       radialUV(cos*radius/outerRadius, sin*radius/outerRadius),
				color:    discColor(t),
			})
		}
	}
	b.mesh.Indices = make([]int, 0, phiSegments*thetaSegments*6)
	for band := 0; band < phiSegments; band++ {
		for step := 0; step < thetaSegments; step++ {
			inner := band*stride + step
			outer := (band+1)*stride + step
			// Both triangles run clockwise in (x, z), so both face +Y.
			b.index(inner, inner+1, outer)
			b.index(inner+1, outer+1, outer)
		}
	}
	return b.build()
}

// RingVertexCount returns how many vertices Ring emits.
func RingVertexCount(thetaSegments, phiSegments int) int {
	return (ClampInt(phiSegments, 1, 1, 128) + 1) * (ClampInt(thetaSegments, 32, 3, 512) + 1)
}

// sweepOr resolves an authored sweep angle. A value at or below zero, or a
// non-finite value, means a full turn.
func sweepOr(thetaLength float64) float64 {
	if thetaLength <= 0 || math.IsNaN(thetaLength) || math.IsInf(thetaLength, 0) {
		return 2 * math.Pi
	}
	if thetaLength > 2*math.Pi {
		return 2 * math.Pi
	}
	return thetaLength
}

// discColor tints a disc from its center to its rim so the unlit fallback path
// shows the shape.
func discColor(t float64) vec3 {
	return vec3{X: 0.62 + 0.24*t, Y: 0.70 - 0.10*t, Z: 0.92 - 0.12*t}
}
