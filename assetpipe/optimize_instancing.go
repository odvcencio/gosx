package assetpipe

import (
	"sort"

	"m31labs.dev/gosx/assetpipe/gltfedit"
)

// instanceGroup holds sibling nodes that draw the same mesh with different
// transforms.
type instanceGroup struct {
	// parent is the node that holds the group, or -1 when the group sits at a
	// scene root.
	parent int
	// scene is the scene index of a root group, or -1.
	scene int
	mesh  int
	nodes []int
}

// detectInstancing finds repeated mesh references and reports them. It writes
// EXT_mesh_gpu_instancing only when the caller asks for it.
//
// Detection alone is useful: it answers whether the extension would pay for a
// given asset. Writing it changes what a loader must understand, so the choice
// stays with the caller.
func detectInstancing(document *gltfedit.Document, opts OptimizeOptions, summary *optimizeSummary) {
	threshold := opts.InstanceThreshold
	if threshold == 0 {
		threshold = DefaultInstanceThreshold
	}
	groups := findInstanceGroups(document, threshold)
	for _, group := range groups {
		summary.instanceGroups++
		summary.instanceNodes += len(group.nodes)
	}
	if !opts.EmitInstancing {
		return
	}
	var collapsed []int
	for _, group := range groups {
		if emitInstanceGroup(document, group) {
			summary.instanceEmitted++
			collapsed = append(collapsed, group.nodes...)
		}
	}
	// A collapsed node now draws nothing, so its JSON is pure cost. Removing it
	// is what makes the extension pay for its own accessors.
	summary.instanceNodesRemoved = document.RemoveNodes(collapsed)
}

// findInstanceGroups returns every group of at least threshold sibling nodes
// that draw one mesh and that the pass may safely collapse.
func findInstanceGroups(document *gltfedit.Document, threshold int) []instanceGroup {
	parents := make([]int, len(document.Nodes))
	for i := range parents {
		parents[i] = -1
	}
	multiParent := make([]bool, len(document.Nodes))
	for index, node := range document.Nodes {
		for _, child := range node.Children {
			if child < 0 || child >= len(parents) {
				continue
			}
			if parents[child] >= 0 {
				multiParent[child] = true
			}
			parents[child] = index
		}
	}
	sceneOf := make([]int, len(document.Nodes))
	for i := range sceneOf {
		sceneOf[i] = -1
	}
	for sceneIndex, scene := range document.Scenes {
		for _, root := range scene.Nodes {
			if root >= 0 && root < len(sceneOf) {
				sceneOf[root] = sceneIndex
			}
		}
	}

	animated := map[int]bool{}
	for _, animation := range document.Animations {
		for _, channel := range animation.Channels {
			if channel.Target.Node != nil {
				animated[*channel.Target.Node] = true
			}
		}
	}
	joint := map[int]bool{}
	for _, skin := range document.Skins {
		for _, index := range skin.Joints {
			joint[index] = true
		}
		if skin.Skeleton != nil {
			joint[*skin.Skeleton] = true
		}
	}

	type key struct {
		parent int
		scene  int
		mesh   int
	}
	buckets := map[key][]int{}
	for index, node := range document.Nodes {
		if node.Mesh == nil {
			continue
		}
		if len(node.Children) > 0 || node.Skin != nil || node.Camera != nil {
			continue
		}
		if len(node.Extensions) > 0 || len(node.Weights) > 0 {
			continue
		}
		if len(node.Matrix) == 16 {
			// A matrix may hold shear, which no translation, rotation and scale
			// triple can express. Leave those nodes alone.
			continue
		}
		if animated[index] || joint[index] || multiParent[index] {
			continue
		}
		bucket := key{parent: parents[index], scene: sceneOf[index], mesh: *node.Mesh}
		if bucket.parent < 0 && bucket.scene < 0 {
			// A node no scene reaches draws nothing.
			continue
		}
		buckets[bucket] = append(buckets[bucket], index)
	}

	out := make([]instanceGroup, 0, len(buckets))
	for bucket, nodes := range buckets {
		if len(nodes) < threshold {
			continue
		}
		sort.Ints(nodes)
		out = append(out, instanceGroup{parent: bucket.parent, scene: bucket.scene, mesh: bucket.mesh, nodes: nodes})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].mesh != out[j].mesh {
			return out[i].mesh < out[j].mesh
		}
		return out[i].parent < out[j].parent
	})
	return out
}

// emitInstanceGroup replaces the nodes of one group with a single instanced
// draw. It returns true when it wrote the extension.
func emitInstanceGroup(document *gltfedit.Document, group instanceGroup) bool {
	translations := make([]float64, 0, len(group.nodes)*3)
	rotations := make([]float64, 0, len(group.nodes)*4)
	scales := make([]float64, 0, len(group.nodes)*3)
	movedTranslation := false
	movedRotation := false
	movedScale := false
	for _, index := range group.nodes {
		node := document.Nodes[index]
		translation := []float64{0, 0, 0}
		if len(node.Translation) == 3 {
			translation = node.Translation
			movedTranslation = true
		}
		rotation := []float64{0, 0, 0, 1}
		if len(node.Rotation) == 4 {
			rotation = node.Rotation
			movedRotation = true
		}
		scale := []float64{1, 1, 1}
		if len(node.Scale) == 3 {
			scale = node.Scale
			movedScale = true
		}
		translations = append(translations, translation...)
		rotations = append(rotations, rotation...)
		scales = append(scales, scale...)
	}
	if !movedTranslation && !movedRotation && !movedScale {
		// Every instance shares one transform, so the group is a duplicate
		// draw, not an instanced one.
		return false
	}

	attributes := map[string]int{}
	add := func(name string, values []float64, accessorType string) bool {
		index, err := document.AddAccessor(values, accessorType, gltfedit.ComponentFloat, false, gltfedit.TargetArrayBuffer)
		if err != nil {
			return false
		}
		attributes[name] = index
		return true
	}
	if movedTranslation && !add("TRANSLATION", translations, "VEC3") {
		return false
	}
	if movedRotation && !add("ROTATION", rotations, "VEC4") {
		return false
	}
	if movedScale && !add("SCALE", scales, "VEC3") {
		return false
	}

	holder := gltfedit.Node{Mesh: intPointer(group.mesh), Name: "gosx-instances"}
	if err := gltfedit.SetInstanceAttributes(&holder, attributes); err != nil {
		return false
	}
	holderIndex := document.AddNode(holder)

	for _, index := range group.nodes {
		node := document.Nodes[index]
		node.Mesh = nil
		if err := document.SetNode(index, node); err != nil {
			return false
		}
	}
	if group.parent >= 0 {
		parent := document.Nodes[group.parent]
		parent.Children = append(parent.Children, holderIndex)
		if err := document.SetNode(group.parent, parent); err != nil {
			return false
		}
	} else if group.scene >= 0 && group.scene < len(document.Scenes) {
		document.Scenes[group.scene].Nodes = append(document.Scenes[group.scene].Nodes, holderIndex)
		document.MarkScenesChanged()
	}
	document.DeclareExtension(gltfedit.InstancingExtension)
	return true
}
