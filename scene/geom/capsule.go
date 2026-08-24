package geom

import "math"

// Capsule resolves a Y-axis capsule whose straight cylindrical body runs
// Length along Y between two hemispherical caps of the given Radius. The
// shape is centered at the origin, so its total Y extent is Length + 2*Radius.
//
// capSegments is the number of curve segments per hemisphere; it falls back
// to 4 and stays inside [1, 64]. radialSegments is the number of steps around
// the axis; it falls back to 8 and stays inside [3, 128]. A non-positive or
// non-finite radius or body length falls back to 1, so the two equator rings
// stay distinct and no quad collapses onto itself.
//
// The mesh is indexed. Every ring carries radialSegments+1 vertices so the
// longitude seam keeps U=0 and U=1 as separate vertices at identical
// positions, and the two equator rings sit Length apart instead of collapsing
// into one degenerate ring. UV U wraps once around the axis; UV V runs
// monotonically 0..1 from the bottom pole to the top pole. Normals point
// outward and triangles wind counter-clockwise as seen from outside.
func Capsule(radius, length float64, capSegments, radialSegments int, want Attribute) *Mesh {
	radius = PositiveOr(radius, 1)
	length = PositiveOr(length, 1)
	capSegments, radialSegments = resolveCapsuleSegments(capSegments, radialSegments)

	half := length / 2
	rings := 2 * capSegments // interior cap rings plus both equator rings

	b := newBuilder(want, CapsuleVertexCount(capSegments, radialSegments))

	// One revolution of direction vectors. The seam column repeats column 0
	// bit-for-bit so U=0 and U=1 share exact positions and no crack opens.
	cosines := make([]float64, radialSegments+1)
	sines := make([]float64, radialSegments+1)
	for j := 0; j < radialSegments; j++ {
		phi := 2 * math.Pi * float64(j) / float64(radialSegments)
		cosines[j], sines[j] = math.Cos(phi), math.Sin(phi)
	}
	cosines[radialSegments], sines[radialSegments] = cosines[0], sines[0]

	capArc := math.Pi * radius / 2
	totalArc := 2*capArc + length

	// A pole is a single vertex: the fan around it stays non-degenerate and
	// the UV V endpoint stays exactly 0 or 1.
	emitPole := func(y, ny, v float64) int {
		return b.emit(vertex{
			position: vec3{X: 0, Y: y, Z: 0},
			normal:   vec3{X: 0, Y: ny, Z: 0},
			uv:       vec2{U: 0.5, V: v},
		})
	}

	// emitCapRing emits one latitude ring of the hemisphere centered at
	// centerY. sign is -1 below the equator and +1 above it. angle runs from
	// 0 at the pole toward pi/2 at the equator but stops short of it, so the
	// outermost cap ring never coincides with the body ring it joins.
	emitCapRing := func(centerY, sign, angle, v float64) int {
		start := -1
		sinA, cosA := math.Sin(angle), math.Cos(angle)
		for j := 0; j <= radialSegments; j++ {
			index := b.emit(vertex{
				position: vec3{
					X: radius * sinA * cosines[j],
					Y: centerY + sign*radius*cosA,
					Z: radius * sinA * sines[j],
				},
				normal: normalize(vec3{
					X: sinA * cosines[j],
					Y: sign * cosA,
					Z: sinA * sines[j],
				}),
				uv: vec2{U: float64(j) / float64(radialSegments), V: v},
			})
			if j == 0 {
				start = index
			}
		}
		return start
	}

	// emitEquatorRing emits one body ring: full radius, horizontal normals.
	emitEquatorRing := func(y, v float64) int {
		start := -1
		for j := 0; j <= radialSegments; j++ {
			index := b.emit(vertex{
				position: vec3{X: radius * cosines[j], Y: y, Z: radius * sines[j]},
				normal:   normalize(vec3{X: cosines[j], Y: 0, Z: sines[j]}),
				uv:       vec2{U: float64(j) / float64(radialSegments), V: v},
			})
			if j == 0 {
				start = index
			}
		}
		return start
	}

	// Rows run bottom pole, bottom cap, bottom equator, top equator, top cap,
	// top pole, so every band joins two strictly separated rings.
	emitPole(-half-radius, -1, 0)
	step := (math.Pi / 2) / float64(capSegments)
	rowStarts := make([]int, 0, rings)
	for i := 1; i < capSegments; i++ {
		angle := step * float64(i)
		rowStarts = append(rowStarts, emitCapRing(-half, -1, angle, radius*angle/totalArc))
	}
	rowStarts = append(rowStarts, emitEquatorRing(-half, capArc/totalArc))
	rowStarts = append(rowStarts, emitEquatorRing(half, (capArc+length)/totalArc))
	// Top cap rings are emitted from the equator upward, matching the bottom
	// hemisphere, so every band joins two strictly rising rows and one shared
	// winding rule covers the whole mesh. UV V keeps climbing with each row.
	for i := capSegments - 1; i >= 1; i-- {
		angle := step * float64(i)
		v := (capArc + length + radius*(math.Pi/2-angle)) / totalArc
		rowStarts = append(rowStarts, emitCapRing(half, 1, angle, v))
	}
	topPole := emitPole(half+radius, 1, 1)

	// Bands between adjacent full rings: two outward triangles per column.
	for k := 0; k+1 < len(rowStarts); k++ {
		lower := rowStarts[k]
		upper := rowStarts[k+1]
		for j := 0; j < radialSegments; j++ {
			b.index(lower+j, upper+j, lower+j+1)
			b.index(lower+j+1, upper+j, upper+j+1)
		}
	}
	// The bottom pole was emitted first, so it is vertex 0.
	bottomRing := rowStarts[0]
	for j := 0; j < radialSegments; j++ {
		b.index(0, bottomRing+j, bottomRing+j+1)
	}
	topRing := rowStarts[len(rowStarts)-1]
	for j := 0; j < radialSegments; j++ {
		b.index(topPole, topRing+j+1, topRing+j)
	}
	return b.build()
}

// CapsuleVertexCount reports how many shared vertices Capsule emits for the
// authored segment counts, applying the same defaults and clamps.
func CapsuleVertexCount(capSegments, radialSegments int) int {
	capSegments, radialSegments = resolveCapsuleSegments(capSegments, radialSegments)
	return 2 + 2*capSegments*(radialSegments+1)
}

// resolveCapsuleSegments applies the capsule segment defaults and limits:
// cap curves fall back to 4 inside [1, 64], radial steps fall back to 8
// inside [3, 128].
func resolveCapsuleSegments(capSegments, radialSegments int) (int, int) {
	return ClampInt(capSegments, 4, 1, 64), ClampInt(radialSegments, 8, 3, 128)
}
