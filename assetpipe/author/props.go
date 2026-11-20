package author

import (
	"fmt"

	"m31labs.dev/gosx/assetpipe/gltfedit"
)

// Prop is one named mesh with no skin: a piece of gear or a prop. Its
// skin and shells are ignored; its vertices are written where they are.
type Prop struct {
	Name string
	Mesh *Mesh
}

// PropSet is a glTF of unskinned props that share one material: each prop
// is a mesh and a node of its own, named Prop.Name, so a reader can pick one
// by name (Scene3D's scene.Attachment.Mesh does).
type PropSet struct {
	Generator string
	Props     []Prop
	Material  Material
}

// Document builds the glTF document without an image. Add images with
// SetImages before writing, as for a Model.
func (s *PropSet) Document() (*gltfedit.Document, error) {
	if len(s.Props) == 0 {
		return nil, fmt.Errorf("author: a prop set needs a prop")
	}
	d := gltfedit.New(s.Generator)
	const vtx = gltfedit.TargetArrayBuffer
	material := 0
	var roots []int
	for i, p := range s.Props {
		m := p.Mesh
		if m == nil || len(m.P) == 0 || len(m.Tri) == 0 || p.Name == "" {
			return nil, fmt.Errorf("author: prop %d needs a name and a mesh", i)
		}
		uv := make([]float64, 0, 2*len(m.UV))
		for _, t := range m.UV {
			uv = append(uv, t[0], t[1])
		}
		pos, err := d.AddAccessor(flat(m.P), "VEC3", gltfedit.ComponentFloat, false, vtx)
		if err != nil {
			return nil, err
		}
		nrm, err := d.AddAccessor(flat(weldNormals(Normals(m.P, m.Tri), m.seamGroups())), "VEC3", gltfedit.ComponentFloat, false, vtx)
		if err != nil {
			return nil, err
		}
		tex, err := d.AddAccessor(uv, "VEC2", gltfedit.ComponentFloat, false, vtx)
		if err != nil {
			return nil, err
		}
		var idx []int32
		for _, t := range m.Tri {
			idx = append(idx, int32(t[0]), int32(t[1]), int32(t[2]))
		}
		indices, err := d.AddAccessorInts(idx, "SCALAR", gltfedit.IndexComponentType(len(m.P)), false, gltfedit.TargetElementArrayBuffer)
		if err != nil {
			return nil, err
		}
		for _, a := range []int{nrm, tex, indices} {
			d.Accessors[a].Min, d.Accessors[a].Max = nil, nil
		}
		d.Meshes = append(d.Meshes, gltfedit.Mesh{Name: p.Name, Primitives: []gltfedit.Primitive{{
			Attributes: map[string]int{"POSITION": pos, "NORMAL": nrm, "TEXCOORD_0": tex}, Indices: &indices, Material: &material}}})
		mesh := len(d.Meshes) - 1
		roots = append(roots, d.AddNode(gltfedit.Node{Name: p.Name, Mesh: &mesh}))
	}
	d.Scenes = []gltfedit.Scene{{Nodes: roots}}
	d.MarkScenesChanged()
	if err := writeMaterial(d, s.Material); err != nil {
		return nil, err
	}
	return d, nil
}
