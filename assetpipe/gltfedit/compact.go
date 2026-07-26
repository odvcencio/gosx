package gltfedit

import (
	"encoding/json"
	"sort"
)

// InstancingExtension is the name EXT_mesh_gpu_instancing carries in a node.
const InstancingExtension = "EXT_mesh_gpu_instancing"

// CompactAccessors drops accessors nothing reads and renumbers the rest. It
// returns the number of accessors it removed.
//
// The pass runs only when the writer can see every reference. It refuses the
// work when the document declares an extension the package does not model, when
// a primitive carries an extension, or when an accessor is sparse. In each of
// those cases a reference may live in JSON the package treats as opaque, and a
// wrong renumbering would silently point a primitive at the wrong data.
func (d *Document) CompactAccessors() int {
	if !d.accessorGCSafe() {
		return 0
	}
	reachable := make([]bool, len(d.Accessors))
	mark := func(index int) {
		if index >= 0 && index < len(reachable) {
			reachable[index] = true
		}
	}
	for _, mesh := range d.Meshes {
		for _, primitive := range mesh.Primitives {
			for _, accessor := range primitive.Attributes {
				mark(accessor)
			}
			if primitive.Indices != nil {
				mark(*primitive.Indices)
			}
			for _, target := range primitive.Targets {
				for _, accessor := range target {
					mark(accessor)
				}
			}
		}
	}
	for _, skin := range d.Skins {
		if skin.InverseBindMatrices != nil {
			mark(*skin.InverseBindMatrices)
		}
	}
	for _, animation := range d.Animations {
		for _, sampler := range animation.Samplers {
			mark(sampler.Input)
			mark(sampler.Output)
		}
	}
	for _, node := range d.Nodes {
		for _, accessor := range nodeInstanceAccessors(node) {
			mark(accessor)
		}
	}

	remap := make([]int, len(d.Accessors))
	kept := make([]Accessor, 0, len(d.Accessors))
	removed := 0
	for index, accessor := range d.Accessors {
		if !reachable[index] {
			remap[index] = -1
			removed++
			continue
		}
		remap[index] = len(kept)
		kept = append(kept, accessor)
	}
	if removed == 0 {
		return 0
	}
	d.Accessors = kept

	for meshIndex := range d.Meshes {
		for primitiveIndex := range d.Meshes[meshIndex].Primitives {
			primitive := &d.Meshes[meshIndex].Primitives[primitiveIndex]
			for name, accessor := range primitive.Attributes {
				primitive.Attributes[name] = remap[accessor]
			}
			if primitive.Indices != nil {
				moved := remap[*primitive.Indices]
				primitive.Indices = &moved
			}
			for targetIndex := range primitive.Targets {
				for name, accessor := range primitive.Targets[targetIndex] {
					primitive.Targets[targetIndex][name] = remap[accessor]
				}
			}
		}
	}
	for skinIndex := range d.Skins {
		if d.Skins[skinIndex].InverseBindMatrices == nil {
			continue
		}
		moved := remap[*d.Skins[skinIndex].InverseBindMatrices]
		d.Skins[skinIndex].InverseBindMatrices = &moved
		d.skinsDirty = true
	}
	for animationIndex := range d.Animations {
		for samplerIndex := range d.Animations[animationIndex].Samplers {
			sampler := &d.Animations[animationIndex].Samplers[samplerIndex]
			sampler.Input = remap[sampler.Input]
			sampler.Output = remap[sampler.Output]
		}
		d.animationsDirty = true
	}
	for nodeIndex := range d.Nodes {
		if remapNodeInstanceAccessors(&d.Nodes[nodeIndex], remap) {
			d.nodesDirty = true
		}
	}
	return removed
}

// accessorGCSafe reports whether every accessor reference sits in a field this
// package models.
func (d *Document) accessorGCSafe() bool {
	if !d.bufferViewGCSafe() {
		return false
	}
	for _, accessor := range d.Accessors {
		if len(accessor.Sparse) > 0 {
			return false
		}
	}
	for _, node := range d.Nodes {
		if len(node.Extensions) == 0 {
			continue
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(node.Extensions, &decoded); err != nil {
			return false
		}
		for name := range decoded {
			if name != InstancingExtension {
				return false
			}
		}
	}
	return true
}

// InstanceAttributes returns the accessors an EXT_mesh_gpu_instancing block
// names, keyed by attribute name.
func InstanceAttributes(node Node) map[string]int { return decodeInstanceAttributes(node) }

// OnlyInstancingExtension reports whether a node carries
// EXT_mesh_gpu_instancing and nothing else.
func OnlyInstancingExtension(node Node) bool {
	if len(node.Extensions) == 0 {
		return false
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(node.Extensions, &decoded); err != nil {
		return false
	}
	if len(decoded) != 1 {
		return false
	}
	_, ok := decoded[InstancingExtension]
	return ok
}

// nodeInstanceAccessors returns the accessors an EXT_mesh_gpu_instancing block
// names.
func nodeInstanceAccessors(node Node) []int {
	attributes := decodeInstanceAttributes(node)
	out := make([]int, 0, len(attributes))
	for _, accessor := range attributes {
		out = append(out, accessor)
	}
	sort.Ints(out)
	return out
}

func decodeInstanceAttributes(node Node) map[string]int {
	if len(node.Extensions) == 0 {
		return nil
	}
	var decoded map[string]struct {
		Attributes map[string]int `json:"attributes"`
	}
	if err := json.Unmarshal(node.Extensions, &decoded); err != nil {
		return nil
	}
	block, ok := decoded[InstancingExtension]
	if !ok {
		return nil
	}
	return block.Attributes
}

func remapNodeInstanceAccessors(node *Node, remap []int) bool {
	attributes := decodeInstanceAttributes(*node)
	if len(attributes) == 0 {
		return false
	}
	moved := map[string]int{}
	for name, accessor := range attributes {
		if accessor < 0 || accessor >= len(remap) || remap[accessor] < 0 {
			return false
		}
		moved[name] = remap[accessor]
	}
	return SetInstanceAttributes(node, moved) == nil
}

// SetInstanceAttributes writes an EXT_mesh_gpu_instancing block onto a node,
// keeping any other extension the node already carries.
func SetInstanceAttributes(node *Node, attributes map[string]int) error {
	decoded := map[string]json.RawMessage{}
	if len(node.Extensions) > 0 {
		if err := json.Unmarshal(node.Extensions, &decoded); err != nil {
			return err
		}
	}
	block, err := json.Marshal(map[string]any{"attributes": attributes})
	if err != nil {
		return err
	}
	decoded[InstancingExtension] = block
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return err
	}
	node.Extensions = encoded
	return nil
}
