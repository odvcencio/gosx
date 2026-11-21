package author

import "math"

// Influence binds a vertex to up to four joints. Weights sum to 1.
type Influence struct {
	Joint  [4]int
	Weight [4]float64
}

// Rigid binds a vertex wholly to one joint.
func Rigid(joint int) Influence {
	return Influence{Joint: [4]int{joint, joint, 0, 0}, Weight: [4]float64{1, 0, 0, 0}}
}

// Shell is the vertex range one builder call made: a ring stack, an
// ellipsoid, a tube or a hand-made surface. Joint bands and the audit work on
// shells.
type Shell struct {
	Name       string
	Joint      int // the joint the shell was modelled on
	First, End int // vertex range [First, End)
	Rings, N   int // ring count and vertices per ring; 0 when irregular
	Tube       bool
}

// MorphTarget is a named per-vertex displacement, written as a glTF morph
// target. Delta has one entry per vertex.
type MorphTarget struct {
	Name   string
	Delta  []Vec3
	Weight float64 // the mesh's default weight
}

// Mesh is one skinned primitive under construction.
type Mesh struct {
	P       []Vec3
	UV      [][2]float64
	Skin    []Influence
	Tri     [][3]int
	Shells  []Shell
	Targets []MorphTarget
	// UVMap places a surface's (u, v) in 0..1 into the texture. The default
	// is the identity.
	UVMap func(cell int, u, v float64) [2]float64
}

// AtlasGrid maps surface UVs into one cell of a cols x rows texture atlas,
// inset so that mipmaps do not bleed between cells: (cell%cols + inset +
// u*span)/cols.
type AtlasGrid struct {
	Cols, Rows  int
	Inset, Span float64
}

// Map places (u, v) of the given cell.
func (g AtlasGrid) Map(cell int, u, v float64) [2]float64 {
	return [2]float64{(float64(cell%g.Cols) + g.Inset + u*g.Span) / float64(g.Cols), (float64(cell/g.Cols) + g.Inset + v*g.Span) / float64(g.Rows)}
}

// Vertex appends one vertex bound rigidly to joint and returns its index.
func (m *Mesh) Vertex(p Vec3, joint, cell int, u, v float64) int {
	idx := len(m.P)
	m.P = append(m.P, p)
	m.Skin = append(m.Skin, Rigid(joint))
	uv := [2]float64{u, v}
	if m.UVMap != nil {
		uv = m.UVMap(cell, u, v)
	}
	m.UV = append(m.UV, uv)
	return idx
}

// Begin opens a shell for hand-made vertices; End closes it.
func (m *Mesh) Begin(name string, joint, rings, n int) int {
	m.Shells = append(m.Shells, Shell{Name: name, Joint: joint, First: len(m.P), Rings: rings, N: n})
	return len(m.P)
}

// End closes the shell Begin opened.
func (m *Mesh) End() { m.Shells[len(m.Shells)-1].End = len(m.P) }

// Ring is one level of a ring stack: the centre, and the radii along X and
// along Z. The ring lies in the plane y = Centre[1].
type Ring struct {
	Centre Vec3
	RX, RZ float64
}

// Quads joins consecutive rings of n vertices each, starting at start, with
// two triangles per quad: (a, d, d+n) and (a, d+n, a+n).
func (m *Mesh) Quads(start, rings, n int) {
	for k := 0; k < rings-1; k++ {
		for j := 0; j < n; j++ {
			a := start + k*n + j
			d := start + k*n + (j+1)%n
			m.Tri = append(m.Tri, [3]int{a, d, d + n}, [3]int{a, d + n, a + n})
		}
	}
}

// RingStack lofts elliptical rings bottom to top, n vertices each, and caps
// both ends with fans. Vertex j of a ring sits at angle 2*pi*j/n, at
// (sin, cos) on (X, Z). It is a lathe when the rings share an axis.
func (m *Mesh) RingStack(name string, joint, cell int, rings []Ring, n int) Shell {
	start := m.Begin(name, joint, len(rings), n)
	den := float64(max(1, len(rings)-1))
	for k, r := range rings {
		for j := 0; j < n; j++ {
			a := 2 * math.Pi * float64(j) / float64(n)
			m.Vertex(Vec3{r.Centre[0] + r.RX*math.Sin(a), r.Centre[1], r.Centre[2] + r.RZ*math.Cos(a)}, joint, cell, float64(j)/float64(n), float64(k)/den)
		}
	}
	m.Quads(start, len(rings), n)
	top := start + (len(rings)-1)*n
	for j := 1; j < n-1; j++ {
		m.Tri = append(m.Tri, [3]int{start, start + j + 1, start + j}, [3]int{top, top + j, top + j + 1})
	}
	m.End()
	return m.Shells[len(m.Shells)-1]
}

// Ellipsoid is a ring stack of r+1 rings with n vertices each, centre c and
// semi-axes s.
func (m *Mesh) Ellipsoid(name string, joint, cell int, c, s Vec3, n, r int) Shell {
	rings := make([]Ring, r+1)
	for k := 0; k <= r; k++ {
		t := -math.Pi/2 + math.Pi*float64(k)/float64(r)
		rings[k] = Ring{Vec3{c[0], c[1] + s[1]*math.Sin(t), c[2]}, math.Max(.0002, s[0]*math.Cos(t)), math.Max(.0002, s[2]*math.Cos(t))}
	}
	return m.RingStack(name, joint, cell, rings, n)
}

// Tube sweeps an open circle of radius r and n vertices along points. The
// frame at each point is built from the tangent and world up, or world X
// when the tangent is near vertical.
func (m *Mesh) Tube(name string, joint, cell int, points []Vec3, r float64, n int) Shell {
	start := m.Begin(name, joint, len(points), n)
	m.Shells[len(m.Shells)-1].Tube = true
	last := len(points) - 1
	for i, p := range points {
		axis := points[min(i+1, last)].Sub(points[max(0, i-1)]).Unit()
		up := Vec3{0, 1, 0}
		if math.Abs(axis[1]) >= .91 {
			up = Vec3{1, 0, 0}
		}
		u := axis.Cross(up).Unit()
		v := axis.Cross(u).Unit()
		for j := 0; j < n; j++ {
			a := 2 * math.Pi * float64(j) / float64(n)
			m.Vertex(p.Add(u.Scale(math.Cos(a)).Add(v.Scale(math.Sin(a))).Scale(r)), joint, cell, float64(j)/float64(n), float64(i)/float64(last))
		}
	}
	for k := 0; k < last; k++ {
		for j := 0; j < n; j++ {
			a := start + k*n + j
			d := start + k*n + (j+1)%n
			m.Tri = append(m.Tri, [3]int{a, d, a + n}, [3]int{d, d + n, a + n})
		}
	}
	m.End()
	return m.Shells[len(m.Shells)-1]
}

// Merge appends another mesh's vertices, triangles, shells and targets.
// Targets match by name; a target missing on one side gets zero deltas.
func (m *Mesh) Merge(o *Mesh) {
	base := len(m.P)
	m.P = append(m.P, o.P...)
	m.UV = append(m.UV, o.UV...)
	m.Skin = append(m.Skin, o.Skin...)
	for _, t := range o.Tri {
		m.Tri = append(m.Tri, [3]int{t[0] + base, t[1] + base, t[2] + base})
	}
	for _, s := range o.Shells {
		s.First += base
		s.End += base
		m.Shells = append(m.Shells, s)
	}
	names := map[string]bool{}
	for i := range m.Targets {
		names[m.Targets[i].Name] = true
		var add []Vec3
		if j := targetIndex(o.Targets, m.Targets[i].Name); j >= 0 {
			add = o.Targets[j].Delta
		} else {
			add = make([]Vec3, len(o.P))
		}
		m.Targets[i].Delta = append(m.Targets[i].Delta, add...)
	}
	for _, t := range o.Targets {
		if names[t.Name] {
			continue
		}
		delta := append(make([]Vec3, base), t.Delta...)
		m.Targets = append(m.Targets, MorphTarget{Name: t.Name, Delta: delta, Weight: t.Weight})
	}
}

func targetIndex(ts []MorphTarget, name string) int {
	for i, t := range ts {
		if t.Name == name {
			return i
		}
	}
	return -1
}

// AddTarget adds a morph target whose delta for each vertex is f(index,
// position). It returns the target's index.
func (m *Mesh) AddTarget(name string, weight float64, f func(i int, p Vec3) Vec3) int {
	d := make([]Vec3, len(m.P))
	for i, p := range m.P {
		d[i] = f(i, p)
	}
	m.Targets = append(m.Targets, MorphTarget{Name: name, Delta: d, Weight: weight})
	return len(m.Targets) - 1
}

// Morphed returns the positions with the given target weights applied.
func (m *Mesh) Morphed(weights []float64) []Vec3 {
	out := append([]Vec3(nil), m.P...)
	for t, w := range weights {
		if w == 0 || t >= len(m.Targets) {
			continue
		}
		for i, d := range m.Targets[t].Delta {
			out[i] = out[i].Add(d.Scale(w))
		}
	}
	return out
}

// Normals returns smooth vertex normals from the face winding: each vertex
// sums the unnormalised normals of its faces, corner by corner.
func Normals(p []Vec3, tri [][3]int) []Vec3 {
	norm := make([]Vec3, len(p))
	face := make([]Vec3, len(tri))
	for i, t := range tri {
		face[i] = p[t[1]].Sub(p[t[0]]).Cross(p[t[2]].Sub(p[t[0]]))
	}
	for c := 0; c < 3; c++ {
		for i, t := range tri {
			norm[t[c]] = norm[t[c]].Add(face[i])
		}
	}
	for i, n := range norm {
		norm[i] = n.Scale(1 / math.Max(1e-7, n.Len()))
	}
	return norm
}
