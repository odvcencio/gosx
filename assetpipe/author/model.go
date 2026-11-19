package author

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"m31labs.dev/gosx/assetpipe/gltfedit"
)

// Material is the model's one PBR material, textured by its atlas.
type Material struct {
	Name        string
	DoubleSided bool
	Roughness   float64
	Metallic    float64
}

// Model is a skinned model ready to write: one mesh, one skeleton, its
// clips, and a chain of plain nodes above the skeleton root.
type Model struct {
	Generator string
	MeshName  string // the name of the node that draws the mesh
	Skeleton  *Skeleton
	Mesh      *Mesh
	Clips     []Clip
	// Wrappers are nodes from the scene root down to the skeleton root's
	// parent, outermost first. NodeClips may animate them.
	Wrappers  []string
	NodeClips []NodeClip
	Material  Material
}

// Document builds the glTF document without an image. Add one with
// EmbedImage or LinkImage before writing.
func (m *Model) Document() (*gltfedit.Document, error) {
	sk, mesh := m.Skeleton, m.Mesh
	if sk == nil || mesh == nil || len(sk.Joints) == 0 || len(mesh.P) == 0 {
		return nil, fmt.Errorf("author: model needs a skeleton and a mesh")
	}
	if len(sk.Joints) > 255 {
		return nil, fmt.Errorf("author: %d joints do not fit JOINTS_0 bytes", len(sk.Joints))
	}
	d := gltfedit.New(m.Generator)
	const vtx = gltfedit.TargetArrayBuffer
	flat := func(v []Vec3) []float64 {
		out := make([]float64, 0, 3*len(v))
		for _, p := range v {
			out = append(out, p[0], p[1], p[2])
		}
		return out
	}
	var uv, weights []float64
	var joints []int32
	for i := range mesh.P {
		uv = append(uv, mesh.UV[i][0], mesh.UV[i][1])
		s := mesh.Skin[i]
		for k := 0; k < 4; k++ {
			joints = append(joints, int32(s.Joint[k]))
			weights = append(weights, s.Weight[k])
		}
	}
	attr := map[string]int{}
	var err error
	add := func(name string, values []float64, kind string) {
		if err != nil {
			return
		}
		attr[name], err = d.AddAccessor(values, kind, gltfedit.ComponentFloat, false, vtx)
	}
	normals := Normals(mesh.P, mesh.Tri)
	add("POSITION", flat(mesh.P), "VEC3")
	add("NORMAL", flat(normals), "VEC3")
	add("TEXCOORD_0", uv, "VEC2")
	if err != nil {
		return nil, err
	}
	if attr["JOINTS_0"], err = d.AddAccessorInts(joints, "VEC4", gltfedit.ComponentUnsignedByte, false, vtx); err != nil {
		return nil, err
	}
	add("WEIGHTS_0", weights, "VEC4")
	if err != nil {
		return nil, err
	}
	var idx []int32
	for _, t := range mesh.Tri {
		idx = append(idx, int32(t[0]), int32(t[1]), int32(t[2]))
	}
	indices, err := d.AddAccessorInts(idx, "SCALAR", gltfedit.IndexComponentType(len(mesh.P)), false, gltfedit.TargetElementArrayBuffer)
	if err != nil {
		return nil, err
	}
	material := 0
	prim := gltfedit.Primitive{Attributes: attr, Indices: &indices, Material: &material}
	gmesh := gltfedit.Mesh{}
	if len(mesh.Targets) > 0 {
		var names []string
		for _, t := range mesh.Targets {
			morphed := make([]Vec3, len(mesh.P))
			for i, p := range mesh.P {
				morphed[i] = p.Add(t.Delta[i])
			}
			mn := Normals(morphed, mesh.Tri)
			dn := make([]Vec3, len(mn))
			for i := range mn {
				dn[i] = mn[i].Sub(normals[i])
			}
			pa, err := d.AddAccessor(flat(t.Delta), "VEC3", gltfedit.ComponentFloat, false, vtx)
			if err != nil {
				return nil, err
			}
			na, err := d.AddAccessor(flat(dn), "VEC3", gltfedit.ComponentFloat, false, vtx)
			if err != nil {
				return nil, err
			}
			prim.Targets = append(prim.Targets, map[string]int{"POSITION": pa, "NORMAL": na})
			gmesh.Weights = append(gmesh.Weights, t.Weight)
			names = append(names, t.Name)
		}
		gmesh.Extras, _ = json.Marshal(map[string]any{"targetNames": names})
	}
	gmesh.Primitives = []gltfedit.Primitive{prim}
	d.Meshes = []gltfedit.Mesh{gmesh}

	// Joints first, in skeleton order, so node index = joint index.
	for i, j := range sk.Joints {
		var children []int
		for k, c := range sk.Joints {
			if c.Parent == i {
				children = append(children, k)
			}
		}
		t := j.Bind
		if j.Parent >= 0 {
			t = j.Bind.Sub(sk.Joints[j.Parent].Bind)
		}
		d.AddNode(gltfedit.Node{Name: j.Name, Translation: []float64{t[0], t[1], t[2]}, Children: children})
	}
	var inv []float64
	var jointList []int
	for i, j := range sk.Joints {
		// Column-major translation by -bind.
		inv = append(inv, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, -j.Bind[0], -j.Bind[1], -j.Bind[2], 1)
		jointList = append(jointList, i)
	}
	zero := 0
	meshNode := d.AddNode(gltfedit.Node{Name: m.MeshName, Mesh: &zero, Skin: &zero})
	ibm, err := d.AddAccessor(inv, "MAT4", gltfedit.ComponentFloat, false, 0)
	if err != nil {
		return nil, err
	}
	root := 0
	d.Skins = []gltfedit.Skin{{InverseBindMatrices: &ibm, Joints: jointList, Skeleton: &root}}
	d.MarkSkinsChanged()
	wrapper := map[string]int{}
	sceneRoot := 0
	for i, name := range m.Wrappers {
		child := 0
		if i+1 < len(m.Wrappers) {
			child = len(d.Nodes) + 1
		}
		wrapper[name] = d.AddNode(gltfedit.Node{Name: name, Children: []int{child}})
		if i == 0 {
			sceneRoot = wrapper[name]
		}
	}
	d.Scenes = []gltfedit.Scene{{Nodes: []int{sceneRoot, meshNode}}}
	d.MarkScenesChanged()

	for _, c := range m.Clips {
		a, err := m.clip(d, c)
		if err != nil {
			return nil, fmt.Errorf("author: clip %s: %w", c.Name, err)
		}
		d.Animations = append(d.Animations, a)
	}
	for _, nc := range m.NodeClips {
		node, ok := wrapper[nc.Node]
		if !ok {
			return nil, fmt.Errorf("author: node clip %s names no wrapper %q", nc.Name, nc.Node)
		}
		in, err := d.AddAccessor(nc.Times, "SCALAR", gltfedit.ComponentFloat, false, 0)
		if err != nil {
			return nil, err
		}
		kind := "VEC3"
		if nc.Path == "rotation" {
			kind = "VEC4"
		}
		var vals []float64
		for _, v := range nc.Values {
			vals = append(vals, v...)
		}
		out, err := d.AddAccessor(vals, kind, gltfedit.ComponentFloat, false, 0)
		if err != nil {
			return nil, err
		}
		d.Animations = append(d.Animations, gltfedit.Animation{Name: nc.Name,
			Samplers: []gltfedit.AnimationSampler{{Input: in, Output: out, Interpolation: "LINEAR"}},
			Channels: []gltfedit.AnimationChannel{{Sampler: 0, Target: gltfedit.AnimationTarget{Node: &node, Path: nc.Path}}}})
	}
	d.MarkAnimationsChanged()
	// glTF requires bounds only on POSITION and on animation inputs; the
	// rest would cost every kit file their JSON.
	keep := map[int]bool{attr["POSITION"]: true}
	for _, t := range prim.Targets {
		keep[t["POSITION"]] = true
	}
	for _, a := range d.Animations {
		for _, s := range a.Samplers {
			keep[s.Input] = true
		}
	}
	for i := range d.Accessors {
		if !keep[i] {
			d.Accessors[i].Min, d.Accessors[i].Max = nil, nil
		}
	}

	mat := m.Material
	if err := d.SetSection("materials", []map[string]any{{"name": mat.Name, "doubleSided": mat.DoubleSided,
		"pbrMetallicRoughness": map[string]any{"baseColorTexture": map[string]int{"index": 0}, "roughnessFactor": mat.Roughness, "metallicFactor": mat.Metallic}}}); err != nil {
		return nil, err
	}
	if err := d.SetSection("textures", []map[string]int{{"source": 0, "sampler": 0}}); err != nil {
		return nil, err
	}
	if err := d.SetSection("samplers", []map[string]int{{"magFilter": 9729, "minFilter": 9987, "wrapS": 33071, "wrapT": 33071}}); err != nil {
		return nil, err
	}
	if err := d.SetSection("scene", 0); err != nil {
		return nil, err
	}
	return d, nil
}

// clip samples one clip into channels: the root joint's translation, then a
// rotation per joint, with the root's rotation last.
func (m *Model) clip(d *gltfedit.Document, c Clip) (gltfedit.Animation, error) {
	ts := Linspace(0, 1, c.Keys)
	times := make([]float64, len(ts))
	poses := make([]Pose, len(ts))
	for i, t := range ts {
		times[i] = t * c.Duration
		poses[i] = c.Sample(t)
	}
	in, err := d.AddAccessor(times, "SCALAR", gltfedit.ComponentFloat, false, 0)
	if err != nil {
		return gltfedit.Animation{}, err
	}
	a := gltfedit.Animation{Name: c.Name}
	channel := func(node int, path string, vals []float64) error {
		kind := "VEC4"
		if path == "translation" {
			kind = "VEC3"
		}
		out, err := d.AddAccessor(vals, kind, gltfedit.ComponentFloat, false, 0)
		if err != nil {
			return err
		}
		a.Samplers = append(a.Samplers, gltfedit.AnimationSampler{Input: in, Output: out, Interpolation: "LINEAR"})
		n := node
		a.Channels = append(a.Channels, gltfedit.AnimationChannel{Sampler: len(a.Samplers) - 1, Target: gltfedit.AnimationTarget{Node: &n, Path: path}})
		return nil
	}
	rot := func(name string) []float64 {
		var v []float64
		for _, p := range poses {
			q, ok := p.Rot[name]
			if !ok {
				q = Identity
			}
			v = append(v, q[:]...)
		}
		return v
	}
	for i, j := range m.Skeleton.Joints {
		if j.Parent < 0 {
			var v []float64
			for _, p := range poses {
				v = append(v, p.Root[:]...)
			}
			if err := channel(i, "translation", v); err != nil {
				return a, err
			}
			continue
		}
		if err := channel(i, "rotation", rot(j.Name)); err != nil {
			return a, err
		}
	}
	for i, j := range m.Skeleton.Joints {
		if j.Parent < 0 {
			if err := channel(i, "rotation", rot(j.Name)); err != nil {
				return a, err
			}
		}
	}
	return a, nil
}

// EmbedImage stores an encoded image in the binary buffer as image 0.
func EmbedImage(d *gltfedit.Document, data []byte, mime string) error {
	view := d.AddBufferView(data, 0)
	return d.SetSection("images", []map[string]any{{"bufferView": view, "mimeType": mime}})
}

// LinkImage points image 0 at a URI; data, when given, becomes a data URI.
func LinkImage(d *gltfedit.Document, uri string, data []byte, mime string) error {
	if data != nil {
		uri = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	return d.SetSection("images", []map[string]any{{"uri": uri, "mimeType": mime}})
}
