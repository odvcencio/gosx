package author

import (
	"fmt"
	"math"
)

// Joint is one bone: its name, its parent's index (-1 for the root) and its
// bind position in model space. Bind rotations are the identity, so a clip's
// rotations are the joints' own.
type Joint struct {
	Name   string
	Parent int
	Bind   Vec3
}

// Skeleton is a joint hierarchy, parents before children.
type Skeleton struct {
	Joints []Joint
}

// Add appends a joint under parent ("" for the root) and returns its index.
func (s *Skeleton) Add(name, parent string, bind Vec3) int {
	p := -1
	if parent != "" {
		p = s.Must(parent)
	}
	s.Joints = append(s.Joints, Joint{name, p, bind})
	return len(s.Joints) - 1
}

// Index returns the named joint's index, or -1.
func (s *Skeleton) Index(name string) int {
	for i, j := range s.Joints {
		if j.Name == name {
			return i
		}
	}
	return -1
}

// Must returns the named joint's index and panics when there is none.
func (s *Skeleton) Must(name string) int {
	i := s.Index(name)
	if i < 0 {
		panic(fmt.Sprintf("author: no joint %q", name))
	}
	return i
}

// Pose is one frame: a local rotation per joint by name (missing joints keep
// the identity) and the root joint's position.
type Pose struct {
	Rot  map[string]Quat
	Root Vec3
}

// World returns every joint's model-space transform in the pose.
func (s *Skeleton) World(p Pose) []Mat4 {
	out := make([]Mat4, len(s.Joints))
	for i, j := range s.Joints {
		r, ok := p.Rot[j.Name]
		if !ok {
			r = Identity
		}
		m := r.Matrix()
		t := p.Root
		if j.Parent >= 0 {
			t = j.Bind.Sub(s.Joints[j.Parent].Bind)
		}
		m[0][3], m[1][3], m[2][3] = t[0], t[1], t[2]
		if j.Parent >= 0 {
			m = out[j.Parent].Mul(m)
		}
		out[i] = m
	}
	return out
}

// Skinning is the per-joint skinning matrices of a pose: world times inverse
// bind.
func (s *Skeleton) Skinning(world []Mat4) []Mat4 {
	out := make([]Mat4, len(world))
	for i, w := range world {
		b := s.Joints[i].Bind
		out[i] = w.Mul(TRS(b.Scale(-1), Identity, Vec3{1, 1, 1}))
	}
	return out
}

// SkinPoint applies linear blend skinning to one point.
func SkinPoint(skin []Mat4, in Influence, p Vec3) Vec3 {
	var o Vec3
	for k := 0; k < 4; k++ {
		if w := in.Weight[k]; w != 0 {
			o = o.Add(skin[in.Joint[k]].Apply(p).Scale(w))
		}
	}
	return o
}

// Posed returns the mesh's skinned positions for a pose.
func (s *Skeleton) Posed(m *Mesh, p Pose) []Vec3 {
	skin := s.Skinning(s.World(p))
	out := make([]Vec3, len(m.P))
	for i, v := range m.P {
		out[i] = SkinPoint(skin, m.Skin[i], v)
	}
	return out
}

// Band blends skin weights across a joint so the surfaces on either side
// bend together instead of one shell pushing through the next. Along Axis
// (pointing from the parent's side to the child's) a vertex's share moves
// from Parent at -Width to Child at +Width with a smoothstep.
type Band struct {
	Parent, Child int
	Centre, Axis  Vec3
	Width         float64
	// Falloff, when set, scales the blend by a weight from 0 to 1 per
	// vertex, for example fading a hip band out toward the midline. Use a
	// smooth function: a step cuts the surface into followers and
	// non-followers and tears the edges between them.
	Falloff func(p Vec3) float64
}

// Apply rebinds the vertices of the given shells. A vertex that carries
// Parent weight hands the band's share of it to Child. A vertex wholly on
// Child takes the parent's share inside the band, and follows Parent wholly
// beyond the band on the parent's side, so for example no leg weight
// reaches above the hip. Bands stack: two legs can share one pelvis.
func (b Band) Apply(m *Mesh, shells []Shell) int {
	axis := b.Axis.Unit()
	changed := 0
	for _, sh := range shells {
		for i := sh.First; i < sh.End; i++ {
			in := m.Skin[i]
			d := m.P[i].Sub(b.Centre).Dot(axis) / b.Width
			f := 1.
			if b.Falloff != nil {
				f = math.Max(0, math.Min(1, b.Falloff(m.P[i])))
			}
			s := Smoothstep(d, -1, 1)
			pk, ck := slot(in, b.Parent), slot(in, b.Child)
			switch {
			case ck >= 0 && in.Weight[ck] == 1:
				if f == 0 {
					continue
				}
				if d <= -1 {
					m.Skin[i] = Rigid(b.Parent)
				} else {
					m.Skin[i] = Influence{Joint: [4]int{b.Child, b.Parent, 0, 0}, Weight: [4]float64{1 - (1-s)*f, (1 - s) * f, 0, 0}}
				}
				changed++
			case pk >= 0 && in.Weight[pk] > 0:
				move := in.Weight[pk] * s * f
				if move <= 0 {
					continue
				}
				if ck < 0 {
					if ck = slot(in, -1); ck < 0 {
						continue // four joints already; leave it
					}
					in.Joint[ck] = b.Child
				}
				in.Weight[pk] -= move
				in.Weight[ck] += move
				m.Skin[i] = in
				changed++
			}
		}
	}
	return changed
}

// slot returns the influence slot holding joint, or for joint -1 the first
// slot with no weight; -1 when there is none.
func slot(in Influence, joint int) int {
	for k := 0; k < 4; k++ {
		if joint < 0 && in.Weight[k] == 0 {
			return k
		}
		if joint >= 0 && in.Joint[k] == joint && in.Weight[k] > 0 {
			return k
		}
	}
	return -1
}

// Lengthen scales the bone from joint to its first child by scale, before
// any weights are blended. The child and every joint below it move by the
// change; vertices bound to those joints move with them. Vertices bound to
// joint itself stretch along the bone: a vertex at fraction t of the bone
// (0 at joint, 1 at the child) moves by the change times (t-anchor)/(1-anchor),
// clamped to 0..1, so the part above anchor keeps its shape. Shells for which
// rigid returns true move as one piece with their joint instead.
func (s *Skeleton) Lengthen(m *Mesh, joint string, scale, anchor float64, rigid func(Shell) bool) {
	j := s.Must(joint)
	child := -1
	for i, c := range s.Joints {
		if c.Parent == j {
			child = i
			break
		}
	}
	if child < 0 {
		return
	}
	bone := s.Joints[child].Bind.Sub(s.Joints[j].Bind)
	delta := bone.Scale(scale - 1)
	below := map[int]bool{}
	for i := range s.Joints {
		for k := i; k >= 0; k = s.Joints[k].Parent {
			if k == child {
				below[i] = true
				break
			}
		}
	}
	isRigid := make([]bool, len(m.P))
	for _, sh := range m.Shells {
		if rigid != nil && rigid(sh) {
			for i := sh.First; i < sh.End; i++ {
				isRigid[i] = true
			}
		}
	}
	for i, p := range m.P {
		owner := m.Skin[i].Joint[0]
		switch {
		case below[owner]:
			m.P[i] = p.Add(delta)
		case owner == j && !isRigid[i]:
			t := p.Sub(s.Joints[j].Bind).Dot(bone) / bone.Dot(bone)
			f := math.Max(0, math.Min(1, (t-anchor)/(1-anchor)))
			m.P[i] = p.Add(delta.Scale(f))
		}
	}
	for i := range s.Joints {
		if below[i] {
			s.Joints[i].Bind = s.Joints[i].Bind.Add(delta)
		}
	}
}

// ShellsOn returns the shells modelled on any of the given joints.
func (m *Mesh) ShellsOn(joints ...int) []Shell {
	var out []Shell
	for _, s := range m.Shells {
		for _, j := range joints {
			if s.Joint == j {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// Clip is one animation: a name, a length in seconds, the number of evenly
// spaced keys, and the pose at normalized time t from 0 to 1.
type Clip struct {
	Name     string
	Duration float64
	Keys     int
	Sample   func(t float64) Pose
}

// NodeClip animates one non-joint node (a wrapper above the skeleton) with
// explicit keys: a translation (3 values a key) or rotation (4) channel.
type NodeClip struct {
	Name   string
	Node   string
	Path   string // "translation" or "rotation"
	Times  []float64
	Values [][]float64
}

// angleBetween returns the angle in degrees between two directions.
func angleBetween(a, b Vec3) float64 {
	c := a.Unit().Dot(b.Unit())
	return math.Acos(math.Max(-1, math.Min(1, c))) * 180 / math.Pi
}
