package author

import (
	"fmt"
	"sort"

	"m31labs.dev/gosx/assetpipe/gltfedit"
)

// Rig is a skinned mesh read back from a glTF document: what the audit
// needs and what a renderer would draw.
type Rig struct {
	Names   []string // joint names, skin order
	Parent  []int    // parent joint index, -1 for a skeleton root
	Rest    []TRSKey // each joint's local rest transform
	Inverse []Mat4   // inverse bind matrices
	P, N    []Vec3   // bind positions and stored normals
	Skin    []Influence
	Tri     [][3]int
	Clips   []RigClip
}

// TRSKey is a local transform.
type TRSKey struct {
	T Vec3
	R Quat
	S Vec3
}

// Matrix returns the transform.
func (k TRSKey) Matrix() Mat4 { return TRS(k.T, k.R, k.S) }

// RigClip is one animation's channels on joints.
type RigClip struct {
	Name     string
	Channels []RigChannel
}

// RigChannel is one joint's keyed translation, rotation or scale.
type RigChannel struct {
	Joint  int
	Path   string
	Step   bool
	Times  []float64
	Values [][]float64
}

// Times returns the sorted, distinct key times of the clip.
func (c RigClip) Times() []float64 {
	seen := map[float64]bool{}
	var out []float64
	for _, ch := range c.Channels {
		for _, t := range ch.Times {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	sort.Float64s(out)
	return out
}

// Local returns each joint's local transform at time t.
func (r *Rig) Local(c RigClip, t float64) []TRSKey {
	out := append([]TRSKey(nil), r.Rest...)
	for _, ch := range c.Channels {
		v := ch.sample(t)
		switch ch.Path {
		case "translation":
			out[ch.Joint].T = Vec3{v[0], v[1], v[2]}
		case "rotation":
			out[ch.Joint].R = Quat{v[0], v[1], v[2], v[3]}.Normalize()
		case "scale":
			out[ch.Joint].S = Vec3{v[0], v[1], v[2]}
		}
	}
	return out
}

func (ch RigChannel) sample(t float64) []float64 {
	n := len(ch.Times)
	if t <= ch.Times[0] {
		return ch.Values[0]
	}
	if t >= ch.Times[n-1] {
		return ch.Values[n-1]
	}
	k := sort.SearchFloat64s(ch.Times, t)
	if ch.Times[k] == t || ch.Step {
		if ch.Times[k] == t {
			return ch.Values[k]
		}
		return ch.Values[k-1]
	}
	a, b := ch.Values[k-1], ch.Values[k]
	f := (t - ch.Times[k-1]) / (ch.Times[k] - ch.Times[k-1])
	if ch.Path == "rotation" {
		q := Slerp(Quat{a[0], a[1], a[2], a[3]}, Quat{b[0], b[1], b[2], b[3]}, f)
		return q[:]
	}
	out := make([]float64, len(a))
	for i := range a {
		out[i] = a[i] + (b[i]-a[i])*f
	}
	return out
}

// World returns the joints' transforms in skeleton space (ancestors above
// the skeleton roots are left out) for local transforms.
func (r *Rig) World(local []TRSKey) []Mat4 {
	out := make([]Mat4, len(local))
	done := make([]bool, len(local))
	var visit func(i int)
	visit = func(i int) {
		if done[i] {
			return
		}
		m := local[i].Matrix()
		if p := r.Parent[i]; p >= 0 {
			visit(p)
			m = out[p].Mul(m)
		}
		out[i], done[i] = m, true
	}
	for i := range local {
		visit(i)
	}
	return out
}

// Posed returns the skinned positions for local joint transforms.
func (r *Rig) Posed(local []TRSKey) []Vec3 {
	world := r.World(local)
	skin := make([]Mat4, len(world))
	for i := range world {
		skin[i] = world[i].Mul(r.Inverse[i])
	}
	out := make([]Vec3, len(r.P))
	for i, p := range r.P {
		out[i] = SkinPoint(skin, r.Skin[i], p)
	}
	return out
}

// Joint returns the named joint's index, or -1.
func (r *Rig) Joint(name string) int {
	for i, n := range r.Names {
		if n == name {
			return i
		}
	}
	return -1
}

// ReadRig reads the first skinned primitive of a document, its skin and
// every animation channel that drives one of the skin's joints.
func ReadRig(d *gltfedit.Document) (*Rig, error) {
	meshNode := -1
	for i, n := range d.Nodes {
		if n.Mesh != nil && n.Skin != nil {
			meshNode = i
			break
		}
	}
	if meshNode < 0 {
		return nil, fmt.Errorf("author: document has no skinned mesh node")
	}
	mesh := d.Meshes[*d.Nodes[meshNode].Mesh]
	skin := d.Skins[*d.Nodes[meshNode].Skin]
	if len(mesh.Primitives) == 0 {
		return nil, fmt.Errorf("author: skinned mesh has no primitive")
	}
	prim := mesh.Primitives[0]
	r := &Rig{}
	vec3s := func(name string) ([]Vec3, error) {
		a, ok := prim.Attributes[name]
		if !ok {
			return nil, fmt.Errorf("author: primitive has no %s", name)
		}
		v, w, err := d.ReadAccessor(a)
		if err != nil {
			return nil, err
		}
		if w != 3 {
			return nil, fmt.Errorf("author: %s has %d components", name, w)
		}
		out := make([]Vec3, len(v)/3)
		for i := range out {
			out[i] = Vec3{v[3*i], v[3*i+1], v[3*i+2]}
		}
		return out, nil
	}
	var err error
	if r.P, err = vec3s("POSITION"); err != nil {
		return nil, err
	}
	if r.N, err = vec3s("NORMAL"); err != nil {
		return nil, err
	}
	ja, ok1 := prim.Attributes["JOINTS_0"]
	wa, ok2 := prim.Attributes["WEIGHTS_0"]
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("author: primitive has no JOINTS_0 or WEIGHTS_0")
	}
	jv, _, err := d.ReadAccessor(ja)
	if err != nil {
		return nil, err
	}
	wv, _, err := d.ReadAccessor(wa)
	if err != nil {
		return nil, err
	}
	r.Skin = make([]Influence, len(r.P))
	for i := range r.Skin {
		for k := 0; k < 4; k++ {
			r.Skin[i].Joint[k] = int(jv[4*i+k])
			r.Skin[i].Weight[k] = wv[4*i+k]
		}
	}
	if prim.Indices == nil {
		return nil, fmt.Errorf("author: primitive is not indexed")
	}
	idx, err := d.ReadIndices(*prim.Indices)
	if err != nil {
		return nil, err
	}
	for i := 0; i+2 < len(idx); i += 3 {
		r.Tri = append(r.Tri, [3]int{int(idx[i]), int(idx[i+1]), int(idx[i+2])})
	}

	nodeJoint := map[int]int{}
	for j, n := range skin.Joints {
		nodeJoint[n] = j
	}
	parentOf := map[int]int{}
	for i, n := range d.Nodes {
		for _, c := range n.Children {
			parentOf[c] = i
		}
	}
	var ibm []float64
	if skin.InverseBindMatrices != nil {
		if ibm, _, err = d.ReadAccessor(*skin.InverseBindMatrices); err != nil {
			return nil, err
		}
	}
	for j, n := range skin.Joints {
		node := d.Nodes[n]
		r.Names = append(r.Names, node.Name)
		p := -1
		if pn, ok := parentOf[n]; ok {
			if pj, ok := nodeJoint[pn]; ok {
				p = pj
			}
		}
		r.Parent = append(r.Parent, p)
		k := TRSKey{R: Identity, S: Vec3{1, 1, 1}}
		if len(node.Translation) == 3 {
			k.T = Vec3{node.Translation[0], node.Translation[1], node.Translation[2]}
		}
		if len(node.Rotation) == 4 {
			k.R = Quat{node.Rotation[0], node.Rotation[1], node.Rotation[2], node.Rotation[3]}
		}
		if len(node.Scale) == 3 {
			k.S = Vec3{node.Scale[0], node.Scale[1], node.Scale[2]}
		}
		r.Rest = append(r.Rest, k)
		var m Mat4
		if ibm != nil {
			// Column-major in the file.
			for c := 0; c < 4; c++ {
				for row := 0; row < 4; row++ {
					m[row][c] = ibm[16*j+4*c+row]
				}
			}
		} else {
			m = TRS(Vec3{}, Identity, Vec3{1, 1, 1})
		}
		r.Inverse = append(r.Inverse, m)
	}
	for _, a := range d.Animations {
		c := RigClip{Name: a.Name}
		for _, ch := range a.Channels {
			if ch.Target.Node == nil {
				continue
			}
			j, ok := nodeJoint[*ch.Target.Node]
			if !ok || ch.Target.Path == "weights" {
				continue
			}
			s := a.Samplers[ch.Sampler]
			times, _, err := d.ReadAccessor(s.Input)
			if err != nil {
				return nil, err
			}
			vals, w, err := d.ReadAccessor(s.Output)
			if err != nil {
				return nil, err
			}
			if s.Interpolation == "CUBICSPLINE" {
				return nil, fmt.Errorf("%w: cubic spline channel in %s", gltfedit.ErrUnsupported, a.Name)
			}
			rc := RigChannel{Joint: j, Path: ch.Target.Path, Step: s.Interpolation == "STEP", Times: times}
			for i := range times {
				rc.Values = append(rc.Values, vals[i*w:(i+1)*w])
			}
			c.Channels = append(c.Channels, rc)
		}
		if len(c.Channels) > 0 {
			r.Clips = append(r.Clips, c)
		}
	}
	return r, nil
}
