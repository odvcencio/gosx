package gltfedit

import "sort"

// RemoveNodes deletes the named nodes and renumbers every reference. It returns
// the number of nodes it removed.
//
// The pass refuses the work when a reference could live in JSON this package
// treats as opaque. A node index appears in a child list, a scene root list, an
// animation channel target, and a skin joint list, and the package models all
// four. An extension the package does not model may hold another reference, and
// a wrong renumbering would move the wrong subtree.
//
// A node with children is never removed, because its children would lose their
// place in the hierarchy.
func (d *Document) RemoveNodes(remove []int) int {
	if len(remove) == 0 || !d.nodeGCSafe() {
		return 0
	}
	drop := make([]bool, len(d.Nodes))
	for _, index := range remove {
		if index < 0 || index >= len(d.Nodes) {
			continue
		}
		if len(d.Nodes[index].Children) > 0 {
			continue
		}
		drop[index] = true
	}
	remap := make([]int, len(d.Nodes))
	kept := make([]Node, 0, len(d.Nodes))
	removed := 0
	for index, node := range d.Nodes {
		if drop[index] {
			remap[index] = -1
			removed++
			continue
		}
		remap[index] = len(kept)
		kept = append(kept, node)
	}
	if removed == 0 {
		return 0
	}
	d.Nodes = kept

	for nodeIndex := range d.Nodes {
		d.Nodes[nodeIndex].Children = remapIndexList(d.Nodes[nodeIndex].Children, remap)
	}
	for sceneIndex := range d.Scenes {
		d.Scenes[sceneIndex].Nodes = remapIndexList(d.Scenes[sceneIndex].Nodes, remap)
	}
	for animationIndex := range d.Animations {
		for channelIndex := range d.Animations[animationIndex].Channels {
			target := &d.Animations[animationIndex].Channels[channelIndex].Target
			if target.Node == nil {
				continue
			}
			moved := remap[*target.Node]
			if moved < 0 {
				// The group selection never removes an animated node, so this
				// cannot happen. Drop the target rather than point it at another
				// node if it ever does.
				target.Node = nil
				continue
			}
			target.Node = &moved
		}
	}
	for skinIndex := range d.Skins {
		d.Skins[skinIndex].Joints = remapIndexList(d.Skins[skinIndex].Joints, remap)
		if d.Skins[skinIndex].Skeleton != nil {
			moved := remap[*d.Skins[skinIndex].Skeleton]
			if moved < 0 {
				d.Skins[skinIndex].Skeleton = nil
			} else {
				d.Skins[skinIndex].Skeleton = &moved
			}
		}
	}
	d.nodesDirty = true
	d.scenesDirty = true
	if len(d.Animations) > 0 {
		d.animationsDirty = true
	}
	if len(d.Skins) > 0 {
		d.skinsDirty = true
	}
	return removed
}

// nodeGCSafe reports whether every node reference sits in a field this package
// models.
func (d *Document) nodeGCSafe() bool {
	for _, extension := range d.ExtensionsUsed {
		if !nodeSafeExtensions[extension] {
			return false
		}
	}
	if _, hasRootExtensions := d.root["extensions"]; hasRootExtensions {
		// A root extension block can name nodes, as KHR_lights_punctual does
		// through node references in some tool output. Leave the graph alone.
		return false
	}
	return true
}

// nodeSafeExtensions lists the extensions that hold no node index outside the
// fields this package models.
var nodeSafeExtensions = map[string]bool{
	"KHR_materials_clearcoat":         true,
	"KHR_materials_sheen":             true,
	"KHR_materials_iridescence":       true,
	"KHR_materials_transmission":      true,
	"KHR_materials_anisotropy":        true,
	"KHR_materials_unlit":             true,
	"KHR_materials_emissive_strength": true,
	"KHR_materials_ior":               true,
	"KHR_materials_specular":          true,
	"KHR_materials_volume":            true,
	"KHR_texture_transform":           true,
	"KHR_texture_basisu":              true,
	"KHR_mesh_quantization":           true,
	"EXT_mesh_gpu_instancing":         true,
}

func remapIndexList(values []int, remap []int) []int {
	if len(values) == 0 {
		return values
	}
	out := make([]int, 0, len(values))
	for _, value := range values {
		if value < 0 || value >= len(remap) || remap[value] < 0 {
			continue
		}
		out = append(out, remap[value])
	}
	sort.Ints(out)
	return out
}
