package scene

import (
	"fmt"
	"math"
	"strings"

	"m31labs.dev/gosx/internal/sceneaffine"
)

// ParentWorldMatrices samples retained groups and the object/model parents
// referenced by instanced batches. Missing named nodes leave batches in world
// space, matching live rendering while a parent is not present.
func (ir SceneIR) ParentWorldMatrices(seconds float64) (map[string][16]float64, error) {
	groups, err := GroupWorldMatrices(ir.Groups, seconds)
	if err != nil {
		return nil, err
	}
	var names []string
	require := func(id string) {
		if id == "" {
			return
		}
		if _, found := groups[id]; found {
			return
		}
		for _, name := range names {
			if name == id {
				return
			}
		}
		names = append(names, id)
	}
	for _, mesh := range ir.InstancedMeshes {
		require(mesh.ParentNode)
	}
	for _, mesh := range ir.InstancedGLBMeshes {
		require(mesh.ParentNode)
	}
	if len(names) == 0 {
		return groups, nil
	}

	if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return nil, fmt.Errorf("scene parent time must be finite")
	}
	// Resolve exact IDs before imported/vessel parts. Visit declaration order,
	// keeping diagnostics and prefix fallback deterministic.
	resolved := make([][16]float64, len(names))
	for i, name := range names {
		var target *ObjectIR
		visit := func(object *ObjectIR, exact bool) {
			if target == nil && (exact && object.ID == name || !exact && strings.HasPrefix(object.ID, name+"/")) {
				target = object
			}
		}
		for j := range ir.Objects {
			visit(&ir.Objects[j], true)
		}
		for j := range ir.Models {
			visit(&ir.Models[j].ObjectIR, true)
		}
		if target == nil {
			for j := range ir.Objects {
				visit(&ir.Objects[j], false)
			}
			for j := range ir.Models {
				visit(&ir.Models[j].ObjectIR, false)
			}
		}
		if target == nil {
			continue
		}
		object := *target
		spin := Rotate(object.SpinX, object.SpinY, object.SpinZ)
		if !validSpinTime(spin, seconds) {
			return nil, fmt.Errorf("scene parent %q has an invalid spin", name)
		}
		world := worldAffine(spinningTransform(Vec3(object.X, object.Y, object.Z), Rotate(object.RotationX, object.RotationY, object.RotationZ), Vec3(object.ScaleX, object.ScaleY, object.ScaleZ), spin, seconds))
		if len(object.ParentMatrix) > 0 {
			if !sceneaffine.ValidParentMatrix(object.ParentMatrix) {
				return nil, fmt.Errorf("scene parent %q has an invalid parent matrix", name)
			}
			var prefix affineMatrix
			copy(prefix[:], object.ParentMatrix)
			world = multiplyAffine(prefix, world)
		}
		if parent, found := groups[object.ParentNode]; found {
			world = multiplyAffine(affineMatrix(parent), world)
		}
		if !sceneaffine.ValidParentMatrix(world[:]) {
			return nil, fmt.Errorf("scene parent %q produces an invalid matrix", name)
		}
		resolved[i] = [16]float64(world)
	}
	for i, matrix := range resolved {
		if matrix[15] == 0 {
			continue
		}
		if groups == nil {
			groups = make(map[string][16]float64, len(names))
		}
		groups[names[i]] = matrix
	}

	return groups, nil
}

// The query resolves named parents lazily; ordinary structural graph queries
// do not lower a second graph or allocate a parent map.
type rayParentResolver struct {
	graph   Graph
	seconds float64
	loaded  bool
	world   map[string][16]float64
	err     error
}

func (r *rayParentResolver) resolve(id string) (worldTransform, bool) {
	parent := identityTransform()
	if r == nil {
		return parent, true
	}
	if !r.loaded {
		r.loaded = true
		r.world, r.err = r.graph.SceneIR().ParentWorldMatrices(r.seconds)
	}
	if r.err != nil {
		return parent, false
	}
	if matrix, found := r.world[id]; found {
		parent.HasMatrix = true
		parent.Matrix = affineMatrix(matrix)
		parent.Position = Vec3(matrix[12], matrix[13], matrix[14])
	}
	return parent, true
}
