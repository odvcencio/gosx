package geom

import "math"

// Lathe revolves a flat radius/Y profile around the Y axis. The profile
// alternates radius, Y and must contain at least two finite points with
// non-negative radii; otherwise Lathe returns nil.
func Lathe(profile []float64, segments int, phiStart, phiLength float64, want Attribute) *Mesh {
	if len(profile) < 4 || len(profile)%2 != 0 {
		return nil
	}
	for _, v := range profile {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
	}
	for i := 0; i < len(profile); i += 2 {
		if profile[i] < 0 {
			return nil
		}
	}

	points := len(profile) / 2
	segments = ClampInt(segments, 12, 3, 512)
	if math.IsNaN(phiStart) || math.IsInf(phiStart, 0) {
		phiStart = 0
	}
	if math.IsNaN(phiLength) || math.IsInf(phiLength, 0) || phiLength <= 0 {
		phiLength = 2 * math.Pi
	} else if phiLength > 2*math.Pi {
		phiLength = 2 * math.Pi
	}

	// Smooth unit normals from the profile tangent in the (radius, Y) plane.
	// A tangent (dr, dy) yields the outward plane normal (dy, -dr).
	var normals []vec3
	if want&AttrNormals != 0 {
		normals = make([]vec3, points)
		for i := 0; i < points; i++ {
			var dr, dy float64
			switch i {
			case 0:
				dr = profile[2] - profile[0]
				dy = profile[3] - profile[1]
			case points - 1:
				dr = profile[2*i] - profile[2*i-2]
				dy = profile[2*i+1] - profile[2*i-1]
			default:
				dr = profile[2*i+2] - profile[2*i-2]
				dy = profile[2*i+3] - profile[2*i-1]
			}
			n := vec3{X: dy, Y: -dr, Z: 0}
			l := math.Hypot(n.X, n.Y)
			if l == 0 || math.IsNaN(l) || math.IsInf(l, 0) {
				// Degenerate local tangent: fall back to a finite outward radial normal.
				n = vec3{X: 1, Y: 0, Z: 0}
				l = 1
			}
			normals[i] = vec3{X: n.X / l, Y: n.Y / l, Z: 0}
		}
	}

	b := newBuilder(want, (segments+1)*points)
	last := points - 1
	for j := 0; j <= segments; j++ {
		t := float64(j) / float64(segments)
		phi := phiStart + phiLength*t
		cos, sin := math.Cos(phi), math.Sin(phi)
		u := t
		for i := 0; i < points; i++ {
			radius, y := profile[2*i], profile[2*i+1]
			v := 0.0
			if last > 0 {
				v = float64(i) / float64(last)
			}
			var normal vec3
			if normals != nil {
				pn := normals[i]
				normal = vec3{X: pn.X * cos, Y: pn.Y, Z: pn.X * sin}
			}
			b.emit(vertex{
				position: vec3{X: radius * cos, Y: y, Z: radius * sin},
				normal:   normal,
				uv:       vec2{U: u, V: v},
			})
		}
	}
	for j := 0; j < segments; j++ {
		row := j * points
		next := row + points
		for i := 0; i < last; i++ {
			a := row + i
			bIdx := next + i
			c := row + i + 1
			d := next + i + 1
			b.index(a, d, bIdx)
			b.index(bIdx, c, d)
		}
	}
	return b.build()
}

// LatheVertexCount reports the vertex count Lathe would emit for a profile
// with the given number of points, applying the same segment resolution.
func LatheVertexCount(profilePoints, segments int) int {
	if profilePoints < 2 {
		return 0
	}
	segments = ClampInt(segments, 12, 3, 512)
	return (segments + 1) * profilePoints
}
