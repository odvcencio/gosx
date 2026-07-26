package gltfedit

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Node is one entry of the glTF node graph. The struct models every field the
// specification defines, so a write loses nothing.
type Node struct {
	Camera      *int            `json:"camera,omitempty"`
	Children    []int           `json:"children,omitempty"`
	Skin        *int            `json:"skin,omitempty"`
	Matrix      []float64       `json:"matrix,omitempty"`
	Mesh        *int            `json:"mesh,omitempty"`
	Rotation    []float64       `json:"rotation,omitempty"`
	Scale       []float64       `json:"scale,omitempty"`
	Translation []float64       `json:"translation,omitempty"`
	Weights     []float64       `json:"weights,omitempty"`
	Name        string          `json:"name,omitempty"`
	Extensions  json.RawMessage `json:"extensions,omitempty"`
	Extras      json.RawMessage `json:"extras,omitempty"`
}

// Scene names the root nodes of one scene.
type Scene struct {
	Nodes      []int           `json:"nodes,omitempty"`
	Name       string          `json:"name,omitempty"`
	Extensions json.RawMessage `json:"extensions,omitempty"`
	Extras     json.RawMessage `json:"extras,omitempty"`
}

// Skin binds a mesh to a joint hierarchy.
type Skin struct {
	InverseBindMatrices *int            `json:"inverseBindMatrices,omitempty"`
	Skeleton            *int            `json:"skeleton,omitempty"`
	Joints              []int           `json:"joints"`
	Name                string          `json:"name,omitempty"`
	Extensions          json.RawMessage `json:"extensions,omitempty"`
	Extras              json.RawMessage `json:"extras,omitempty"`
}

// AnimationTarget names what one channel drives.
type AnimationTarget struct {
	Node       *int            `json:"node,omitempty"`
	Path       string          `json:"path"`
	Extensions json.RawMessage `json:"extensions,omitempty"`
	Extras     json.RawMessage `json:"extras,omitempty"`
}

// AnimationChannel joins one sampler to one target.
type AnimationChannel struct {
	Sampler    int             `json:"sampler"`
	Target     AnimationTarget `json:"target"`
	Extensions json.RawMessage `json:"extensions,omitempty"`
	Extras     json.RawMessage `json:"extras,omitempty"`
}

// AnimationSampler holds the keyframe times and values of one channel.
type AnimationSampler struct {
	Input         int             `json:"input"`
	Interpolation string          `json:"interpolation,omitempty"`
	Output        int             `json:"output"`
	Extensions    json.RawMessage `json:"extensions,omitempty"`
	Extras        json.RawMessage `json:"extras,omitempty"`
}

// Animation groups channels and samplers.
type Animation struct {
	Channels   []AnimationChannel `json:"channels"`
	Samplers   []AnimationSampler `json:"samplers"`
	Name       string             `json:"name,omitempty"`
	Extensions json.RawMessage    `json:"extensions,omitempty"`
	Extras     json.RawMessage    `json:"extras,omitempty"`
}

// AddNode appends a node and returns its index. Existing indices keep their
// meaning, so an animation channel or a joint list stays correct.
func (d *Document) AddNode(node Node) int {
	d.Nodes = append(d.Nodes, node)
	d.nodesDirty = true
	return len(d.Nodes) - 1
}

// SetNode replaces one node record.
func (d *Document) SetNode(index int, node Node) error {
	if index < 0 || index >= len(d.Nodes) {
		return fmt.Errorf("%w: node %d", ErrFormat, index)
	}
	d.Nodes[index] = node
	d.nodesDirty = true
	return nil
}

// MarkNodesChanged tells the writer to serialize the node array again. Use it
// after a direct change to a Nodes entry.
func (d *Document) MarkNodesChanged() { d.nodesDirty = true }

// MarkScenesChanged tells the writer to serialize the scene array again.
func (d *Document) MarkScenesChanged() { d.scenesDirty = true }

// MarkSkinsChanged tells the writer to serialize the skin array again.
func (d *Document) MarkSkinsChanged() { d.skinsDirty = true }

// MarkAnimationsChanged tells the writer to serialize the animation array
// again.
func (d *Document) MarkAnimationsChanged() { d.animationsDirty = true }

// DeclareExtension records an extension in extensionsUsed. It never adds the
// name to extensionsRequired, because the pipeline only writes extensions a
// loader may ignore without breaking the geometry.
func (d *Document) DeclareExtension(name string) {
	for _, existing := range d.ExtensionsUsed {
		if existing == name {
			return
		}
	}
	d.ExtensionsUsed = append(d.ExtensionsUsed, name)
	sort.Strings(d.ExtensionsUsed)
	d.extensionsDirty = true
}

// NodesByMesh returns the node indices that draw each mesh.
func (d *Document) NodesByMesh() map[int][]int {
	out := map[int][]int{}
	for index, node := range d.Nodes {
		if node.Mesh == nil {
			continue
		}
		out[*node.Mesh] = append(out[*node.Mesh], index)
	}
	return out
}

// MeshSkins returns the skin index each mesh is drawn with, and a flag that is
// true when at least one node draws the mesh without a skin.
func (d *Document) MeshSkins(mesh int) (skins []int, unskinned bool) {
	seen := map[int]bool{}
	for _, node := range d.Nodes {
		if node.Mesh == nil || *node.Mesh != mesh {
			continue
		}
		if node.Skin == nil {
			unskinned = true
			continue
		}
		if !seen[*node.Skin] {
			seen[*node.Skin] = true
			skins = append(skins, *node.Skin)
		}
	}
	sort.Ints(skins)
	return skins, unskinned
}
