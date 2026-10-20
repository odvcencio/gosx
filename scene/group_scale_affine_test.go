package scene

import (
	"encoding/json"
	"math"
	"math/rand"
	"testing"
)

func TestGroupScaleKeepsScaleFreeWireBytes(t *testing.T) {
	child := Mesh{ID: "box", Geometry: BoxGeometry{Width: 2}, Position: Vector3{X: 1}, Rotation: Euler{Y: 0.25}}
	base := Props{Graph: NewGraph(Group{Position: Vector3{Y: 2}, Rotation: Euler{Z: 0.5}, Children: []Node{child}})}
	explicitUnit := Props{Graph: NewGraph(Group{Position: Vector3{Y: 2}, Rotation: Euler{Z: 0.5}, Scale: Vector3{X: 1, Y: 1, Z: 1}, Children: []Node{child}})}
	baseBytes, err := json.Marshal(base.SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	unitBytes, err := json.Marshal(explicitUnit.SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	if string(baseBytes) != string(unitBytes) {
		t.Fatalf("explicit unit Group.Scale changed legacy bytes\nbase: %s\nunit: %s", baseBytes, unitBytes)
	}
	if matrix := base.SceneIR().Objects[0].ParentMatrix; matrix != nil {
		t.Fatalf("scale-free object parentMatrix = %v, want omitted", matrix)
	}
}

func TestGroupScaleResolvesZeroComponentsIndividually(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		scale Vector3
		want  Vector3
	}{
		{name: "omitted", scale: Vector3{}, want: sceneUnitScale()},
		{name: "unit", scale: Vector3{X: 1, Y: 1, Z: 1}, want: sceneUnitScale()},
		{name: "partial zero", scale: Vector3{X: 2, Z: -3}, want: Vector3{X: 2, Y: 1, Z: -3}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			props := Props{Graph: NewGraph(Group{Scale: testCase.scale, Children: []Node{Mesh{ID: "mesh", Geometry: BoxGeometry{}}}})}
			object := props.SceneIR().Objects[0]
			if testCase.want == sceneUnitScale() {
				if object.ParentMatrix != nil {
					t.Fatalf("unit scale emitted parent matrix: %v", object.ParentMatrix)
				}
				return
			}
			assertMatrixClose(t, object.ParentMatrix, affineFromTRS(Vector3{}, quaternion{W: 1}, testCase.want), 1e-12)
		})
	}
}

func TestMeshScaleStaysLeafOnlyUnderScaledHierarchy(t *testing.T) {
	ir := (Props{Graph: NewGraph(Group{
		Scale: Vector3{X: 2, Y: 3, Z: 4},
		Children: []Node{Mesh{
			ID: "parent", Geometry: BoxGeometry{}, Scale: Vector3{X: 9, Y: 8, Z: 7}, Position: Vector3{X: 1},
			Children: []Node{Mesh{ID: "child", Geometry: BoxGeometry{}, Position: Vector3{Y: 2}}},
		}},
	})}).SceneIR()
	if len(ir.Objects) != 2 {
		t.Fatalf("objects = %d, want 2", len(ir.Objects))
	}
	parent, child := ir.Objects[0], ir.Objects[1]
	if parent.ScaleX != 9 || parent.ScaleY != 8 || parent.ScaleZ != 7 {
		t.Fatalf("parent leaf scale lost: %#v", parent)
	}
	if child.ScaleX != 0 || child.ScaleY != 0 || child.ScaleZ != 0 {
		t.Fatalf("mesh scale propagated into child: %#v", child)
	}
	wantParent := affineFromTRS(Vector3{}, quaternion{W: 1}, Vector3{X: 2, Y: 3, Z: 4})
	wantChildParent := multiplyAffine(wantParent, affineFromTRS(Vector3{X: 1}, quaternion{W: 1}, sceneUnitScale()))
	assertMatrixClose(t, child.ParentMatrix, wantChildParent, 1e-12)
}

func TestGroupScaleLowersExactAffineParent(t *testing.T) {
	outer := Group{
		Position: Vector3{X: 4, Y: -2, Z: 1},
		Rotation: Euler{Y: 0.3},
		Scale:    Vector3{X: 2, Y: 3, Z: 4},
		Children: []Node{Group{
			Position: Vector3{X: 1, Y: 2},
			Rotation: Euler{Z: math.Pi / 4},
			Children: []Node{Mesh{
				ID:       "sheared",
				Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
				Position: Vector3{X: 0.5, Y: -0.25, Z: 2},
				Rotation: Euler{X: 0.2},
				Scale:    Vector3{X: 0.5, Y: 2, Z: -1},
			}},
		}},
	}
	ir := (Props{Graph: NewGraph(outer)}).SceneIR()
	if len(ir.Objects) != 1 {
		t.Fatalf("objects = %d, want 1", len(ir.Objects))
	}
	object := ir.Objects[0]
	wantParent := multiplyAffine(
		affineFromTRS(outer.Position, quaternionFromEuler(outer.Rotation), outer.Scale),
		affineFromTRS(Vector3{X: 1, Y: 2}, quaternionFromEuler(Euler{Z: math.Pi / 4}), sceneUnitScale()),
	)
	assertMatrixClose(t, object.ParentMatrix, wantParent, 1e-12)
	if object.X != 0.5 || object.Y != -0.25 || object.Z != 2 {
		t.Fatalf("leaf position flattened to (%v,%v,%v), want authored local position", object.X, object.Y, object.Z)
	}
	if object.RotationX != 0.2 || object.RotationY != 0 || object.RotationZ != 0 {
		t.Fatalf("leaf rotation = (%v,%v,%v), want authored local rotation", object.RotationX, object.RotationY, object.RotationZ)
	}
	if object.ScaleX != 0.5 || object.ScaleY != 2 || object.ScaleZ != -1 {
		t.Fatalf("leaf scale = (%v,%v,%v), want authored leaf scale", object.ScaleX, object.ScaleY, object.ScaleZ)
	}
	// Non-uniform parent scale and child rotation produce non-orthogonal basis
	// columns. This proves the wire retained shear instead of decomposing it.
	dot := wantParent[0]*wantParent[4] + wantParent[1]*wantParent[5] + wantParent[2]*wantParent[6]
	if math.Abs(dot) < 1e-6 {
		t.Fatalf("parent matrix basis dot = %g, want non-zero shear", dot)
	}
}

func TestGroupScaleNodePolicyRoster(t *testing.T) {
	group := Group{
		Position: Vector3{X: 3, Y: 4, Z: 5},
		Scale:    Vector3{X: 2, Y: 3, Z: 4},
		Children: []Node{
			Mesh{ID: "mesh", Geometry: BoxGeometry{}},
			Points{ID: "points", Count: 1, Positions: []Vector3{{X: 1}}},
			InstancedMesh{ID: "instances", Count: 1, Geometry: BoxGeometry{}, Positions: []Vector3{{Y: 1}}, Scales: []Vector3{{X: 1, Y: 2, Z: 1}}},
			Model{ID: "model", Src: "/model.glb", Scale: Vector3{X: 1, Y: 2, Z: 1}},
			InstancedGLBMesh{ID: "glb", Src: "/model.glb", Instances: []MeshInstance{{ID: "one", Position: Vector3{Z: 1}}}},
			ComputeParticles{ID: "compute", Count: 1, Emitter: ParticleEmitter{Kind: "point", Position: Vector3{X: 1}}},
			PointLight{ID: "point-light", Position: Vector3{Y: 1}},
			DirectionalLight{ID: "sun", Direction: Vector3{X: 1, Y: 1}},
			Label{ID: "label", Text: "label", Position: Vector3{X: 1}},
			Sprite{ID: "sprite", Src: "/sprite.png", Position: Vector3{Y: 1}},
			HTML{ID: "html", Markup: "<b>x</b>", Position: Vector3{Z: 1}},
		},
	}
	ir := (Props{Graph: NewGraph(group)}).SceneIR()
	if len(ir.Objects) != 1 || len(ir.Objects[0].ParentMatrix) != 16 {
		t.Fatalf("mesh parent matrix missing: %#v", ir.Objects)
	}
	if len(ir.Points) != 1 || len(ir.Points[0].ParentMatrix) != 16 {
		t.Fatalf("points parent matrix missing: %#v", ir.Points)
	}
	if len(ir.Models) != 1 || len(ir.Models[0].ParentMatrix) != 16 {
		t.Fatalf("model parent matrix missing: %#v", ir.Models)
	}
	if len(ir.InstancedGLBMeshes) != 1 || len(ir.InstancedGLBMeshes[0].Instances[0].ParentMatrix) != 16 {
		t.Fatalf("GLB instance parent matrix missing: %#v", ir.InstancedGLBMeshes)
	}
	wantGroup := affineFromTRS(group.Position, quaternionFromEuler(group.Rotation), group.Scale)
	assertMatrixClose(t, ir.InstancedMeshes[0].Transforms[:16], multiplyAffine(wantGroup, affineFromTRS(Vector3{Y: 1}, quaternion{W: 1}, Vector3{X: 1, Y: 2, Z: 1})), 1e-12)
	wantEmitter := affinePoint(wantGroup, Vector3{X: 1})
	if got := ir.ComputeParticles[0].Emitter; got.X != wantEmitter.X || got.Y != wantEmitter.Y || got.Z != wantEmitter.Z {
		t.Fatalf("emitter = (%v,%v,%v), want %v", got.X, got.Y, got.Z, wantEmitter)
	}
	wantPointLight := affinePoint(wantGroup, Vector3{Y: 1})
	if got := ir.Lights[0]; got.X != wantPointLight.X || got.Y != wantPointLight.Y || got.Z != wantPointLight.Z {
		t.Fatalf("point light = (%v,%v,%v), want %v", got.X, got.Y, got.Z, wantPointLight)
	}
	wantDirection := normalizeVector(affineVector(wantGroup, Vector3{X: 1, Y: 1}))
	if got := ir.Lights[1]; !vectorClose(Vector3{X: got.DirectionX, Y: got.DirectionY, Z: got.DirectionZ}, wantDirection, 1e-12) {
		t.Fatalf("directional light = (%v,%v,%v), want %v", got.DirectionX, got.DirectionY, got.DirectionZ, wantDirection)
	}
	if !vectorClose(Vector3{X: ir.Labels[0].X, Y: ir.Labels[0].Y, Z: ir.Labels[0].Z}, affinePoint(wantGroup, Vector3{X: 1}), 1e-12) {
		t.Fatalf("label anchor was not transformed by Group.Scale: %#v", ir.Labels[0])
	}
	if !vectorClose(Vector3{X: ir.Sprites[0].X, Y: ir.Sprites[0].Y, Z: ir.Sprites[0].Z}, affinePoint(wantGroup, Vector3{Y: 1}), 1e-12) {
		t.Fatalf("sprite anchor was not transformed by Group.Scale: %#v", ir.Sprites[0])
	}
	if !vectorClose(Vector3{X: ir.HTML[0].X, Y: ir.HTML[0].Y, Z: ir.HTML[0].Z}, affinePoint(wantGroup, Vector3{Z: 1}), 1e-12) {
		t.Fatalf("HTML anchor was not transformed by Group.Scale: %#v", ir.HTML[0])
	}
}

func TestGroupScaleRaycastAndBVHExactShearParity(t *testing.T) {
	graph := NewGraph(Group{
		Position: Vector3{X: 2, Y: -1, Z: 0.5},
		Scale:    Vector3{X: 2, Y: 3, Z: -0.75},
		Children: []Node{Group{
			Rotation: Euler{Z: math.Pi / 4},
			Children: []Node{Mesh{ID: "target", Geometry: CubeGeometry{Size: 2}, Rotation: Euler{Y: 0.35}}},
		}},
	})
	parent := combineTransforms(identityTransform(), localScaledTransform(Vector3{X: 2, Y: -1, Z: 0.5}, Euler{}, Vector3{X: 2, Y: 3, Z: -0.75}))
	parent = combineTransforms(parent, localTransform(Vector3{}, Euler{Z: math.Pi / 4}))
	world := combineTransforms(parent, localTransform(Vector3{}, Euler{Y: 0.35}))
	matrix := worldAffineWithScale(world, sceneUnitScale())
	localOrigin := Vector3{Z: 4}
	localSurface := Vector3{Z: 1}
	worldOrigin := affinePoint(matrix, localOrigin)
	worldSurface := affinePoint(matrix, localSurface)
	ray := Ray{Origin: worldOrigin, Direction: normalizeVector(subVectors(worldSurface, worldOrigin))}

	walk, ok := RaycastGraph(graph, ray)
	if !ok {
		t.Fatal("walk raycast missed sheared cube")
	}
	accelerated, ok := NewSceneAccelerator(graph).Raycast(ray)
	if !ok {
		t.Fatal("BVH raycast missed sheared cube")
	}
	wantNormal := affineNormal(matrix, Vector3{Z: 1})
	if !vectorClose(walk.Point, worldSurface, 1e-9) || !vectorClose(walk.Normal, wantNormal, 1e-9) {
		t.Fatalf("walk hit = %#v, want point %v normal %v", walk, worldSurface, wantNormal)
	}
	if !vectorClose(accelerated.Point, walk.Point, 1e-9) || !vectorClose(accelerated.Normal, walk.Normal, 1e-9) || math.Abs(accelerated.Distance-walk.Distance) > 1e-9 {
		t.Fatalf("BVH hit = %#v, walk = %#v", accelerated, walk)
	}
}

func TestGroupScaleRandomizedWalkBVHParity(t *testing.T) {
	random := rand.New(rand.NewSource(20260830))
	randomVector := func(spread float64) Vector3 {
		return Vector3{X: (random.Float64()*2 - 1) * spread, Y: (random.Float64()*2 - 1) * spread, Z: (random.Float64()*2 - 1) * spread}
	}
	randomScale := func(index int) Vector3 {
		scale := Vector3{X: 0.3 + random.Float64()*3, Y: 0.3 + random.Float64()*3, Z: 0.3 + random.Float64()*3}
		if index%2 == 0 {
			scale.X = -scale.X
		}
		if index%5 == 0 {
			scale.Z = -scale.Z
		}
		return scale
	}
	for index := 0; index < 128; index++ {
		groups := [3]Group{}
		for level := range groups {
			groups[level].Position = randomVector(2)
			groups[level].Rotation = Euler{X: random.Float64() - 0.5, Y: random.Float64() - 0.5, Z: random.Float64() - 0.5}
			groups[level].Scale = randomScale(index + level)
		}
		mesh := Mesh{
			ID: "target", Geometry: CubeGeometry{Size: 2},
			Position: randomVector(1),
			Rotation: Euler{X: random.Float64() - 0.5, Y: random.Float64() - 0.5, Z: random.Float64() - 0.5},
			Scale:    randomScale(index + 7),
		}
		groups[2].Children = []Node{mesh}
		groups[1].Children = []Node{groups[2]}
		groups[0].Children = []Node{groups[1]}
		graph := NewGraph(groups[0])

		world := identityTransform()
		for _, group := range groups {
			world = combineTransforms(world, localScaledTransform(group.Position, group.Rotation, group.Scale))
		}
		world = combineTransforms(world, localTransform(mesh.Position, mesh.Rotation))
		matrix := worldAffineWithScale(world, mesh.Scale)
		worldOrigin := affinePoint(matrix, Vector3{Z: 4})
		worldSurface := affinePoint(matrix, Vector3{Z: 1})
		ray := Ray{Origin: worldOrigin, Direction: normalizeVector(subVectors(worldSurface, worldOrigin))}
		walk, walkOK := RaycastGraph(graph, ray)
		bvh, bvhOK := NewSceneAccelerator(graph).Raycast(ray)
		if !walkOK || !bvhOK {
			t.Fatalf("case %d hit presence: walk=%v bvh=%v", index, walkOK, bvhOK)
		}
		wantNormal := affineNormal(matrix, Vector3{Z: 1})
		if !vectorClose(walk.Point, worldSurface, 1e-8) || !vectorClose(walk.Normal, wantNormal, 1e-8) {
			t.Fatalf("case %d walk=%#v want point=%v normal=%v", index, walk, worldSurface, wantNormal)
		}
		if !vectorClose(bvh.Point, walk.Point, 1e-8) || !vectorClose(bvh.Normal, walk.Normal, 1e-8) || math.Abs(bvh.Distance-walk.Distance) > 1e-8 {
			t.Fatalf("case %d bvh=%#v walk=%#v", index, bvh, walk)
		}
	}
}

func TestAffineInverseScaleExtremesAndFailClosed(t *testing.T) {
	const nearMax = 9e307
	identity := affineMatrix{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
	valid := map[string]affineMatrix{
		"uniform large": {1e150, 0, 0, 0, 0, 1e150, 0, 0, 0, 0, 1e150, 0, 0, 0, 0, 1},
		"uniform small": {1e-150, 0, 0, 0, 0, 1e-150, 0, 0, 0, 0, 1e-150, 0, 0, 0, 0, 1},
		"sheared reflected large": {
			-2e150, 0, 0, 0, 1e150, 3e150, 0, 0, 0, 0, 4e150, 0, 0, 0, 0, 1,
		},
		"sheared reflected small": {
			-2e-150, 0, 0, 0, 1e-150, 3e-150, 0, 0, 0, 0, 4e-150, 0, 0, 0, 0, 1,
		},
		"near max finite": {
			nearMax, -nearMax, 0, 0, nearMax, nearMax, 0, 0, 0, 0, nearMax, 0, 0, 0, 0, 1,
		},
		"anisotropic tiny diagonal": {
			1e-297, 0, 0, 0, 0, 2e-303, 0, 0, 0, 0, 2e-303, 0, 0, 0, 0, 1,
		},
		"anisotropic tiny shear": {
			1e-297, 0, 0, 0, 5e-298, 2e-303, 0, 0, -4e-298, 1e-303, 2e-303, 0, 0, 0, 0, 1,
		},
		"anisotropic tiny transposed shear": {
			1e-297, 5e-298, -4e-298, 0, 0, 2e-303, 1e-303, 0, 0, 0, 2e-303, 0, 0, 0, 0, 1,
		},
		"anisotropic tiny reflected shear": {
			-1e-297, 0, 0, 0, 5e-298, 2e-303, 0, 0, -4e-298, 1e-303, 2e-303, 0, 0, 0, 0, 1,
		},
		"just above normalized determinant threshold": {
			1e-297, 0, 0, 0, 0, 1e-303, 0, 0, 0, 0, 1.000001e-303, 0, 0, 0, 0, 1,
		},
	}
	for name, matrix := range valid {
		t.Run(name, func(t *testing.T) {
			if !ValidParentMatrix(matrix[:]) {
				t.Fatal("public parent-matrix validator rejected valid affine inverse")
			}
			inverse, ok := inverseAffine(matrix)
			if !ok {
				t.Fatal("valid affine inverse was rejected")
			}
			linearNonZero := false
			for index, value := range inverse {
				if math.IsNaN(value) || math.IsInf(value, 0) {
					t.Fatalf("inverse[%d] is non-finite: %v", index, value)
				}
				if index < 12 && index%4 != 3 && value != 0 {
					linearNonZero = true
				}
			}
			if !linearNonZero {
				t.Fatal("inverse linear basis is all zero")
			}
			assertMatrixClose(t, affineSlice(multiplyAffine(matrix, inverse)), identity, 1e-9)
		})
	}
	tinyInverse, ok := inverseAffine(valid["anisotropic tiny diagonal"])
	if !ok || math.Abs(tinyInverse[0]/1e297-1) > 1e-15 || math.Abs(tinyInverse[5]/5e302-1) > 1e-15 || math.Abs(tinyInverse[10]/5e302-1) > 1e-15 {
		t.Fatalf("anisotropic tiny inverse = %v, want diag(1e297,5e302,5e302)", tinyInverse)
	}

	for name, matrix := range map[string]affineMatrix{
		"singular":   {1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1},
		"non finite": {math.Inf(1), 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1},
		"inverse coefficient overflow": {
			1e-308, 0, 0, 0, 0, 1e-308, 0, 0, 0, 0, 2e-320, 0, 0, 0, 0, 1,
		},
		"below normalized determinant threshold": {
			1e-297, 0, 0, 0, 0, 1e-303, 0, 0, 0, 0, 0.999999e-303, 0, 0, 0, 0, 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if ValidParentMatrix(matrix[:]) {
				t.Fatal("invalid affine matrix passed the public validator")
			}
			if inverse, ok := inverseAffine(matrix); ok {
				t.Fatalf("invalid inverse reported success: %v", inverse)
			}
		})
	}
	translationOverflow := affineMatrix{1e-308, 0, 0, 0, 0, 1e-308, 0, 0, 0, 0, 1e-308, 0, 9e307, 0, 0, 1}
	if !ValidParentMatrix(translationOverflow[:]) {
		t.Fatal("representable linear inverse was rejected because of translation")
	}
	if inverse, ok := inverseAffine(translationOverflow); ok {
		t.Fatalf("non-finite translated inverse reported success: %v", inverse)
	}
}

func TestGroupScaleCanonicalIRClonesParentMatrix(t *testing.T) {
	props := Props{Graph: NewGraph(Group{Scale: Vector3{X: 2, Y: 3, Z: 4}, Children: []Node{Mesh{ID: "box", Geometry: BoxGeometry{}}}})}
	legacy := props.SceneIR()
	canonical := props.CanonicalIR()
	if len(canonical.Nodes) != 1 || len(canonical.Nodes[0].Transform.ParentMatrix) != 16 {
		t.Fatalf("canonical parentMatrix missing: %#v", canonical.Nodes)
	}
	original := legacy.Objects[0].ParentMatrix[0]
	canonical.Nodes[0].Transform.ParentMatrix[0] = 99
	if legacy.Objects[0].ParentMatrix[0] != original {
		t.Fatal("canonical IR parentMatrix aliases compatibility IR")
	}
}

func TestGroupScaleParentMatrixRoundTripsCompatibilityWire(t *testing.T) {
	original := (Props{Graph: NewGraph(Group{
		Position: Vector3{X: 1, Y: 2, Z: 3}, Rotation: Euler{Z: 0.4}, Scale: Vector3{X: 2, Y: 3, Z: -4},
		Children: []Node{
			Mesh{ID: "mesh", Geometry: BoxGeometry{}, Scale: Vector3{X: 0.5, Y: 2, Z: 1}},
			Points{ID: "points", Count: 1, Positions: []Vector3{{X: 1}}},
			Model{ID: "model", Src: "/model.glb"},
			InstancedGLBMesh{ID: "instances", Src: "/model.glb", Instances: []MeshInstance{{ID: "one"}}},
		},
	})}).SceneIR()
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SceneIR
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	assertMatrixClose(t, decoded.Objects[0].ParentMatrix, affineMatrixFromSlice(t, original.Objects[0].ParentMatrix), 0)
	assertMatrixClose(t, decoded.Points[0].ParentMatrix, affineMatrixFromSlice(t, original.Points[0].ParentMatrix), 0)
	assertMatrixClose(t, decoded.Models[0].ParentMatrix, affineMatrixFromSlice(t, original.Models[0].ParentMatrix), 0)
	assertMatrixClose(t, decoded.InstancedGLBMeshes[0].Instances[0].ParentMatrix, affineMatrixFromSlice(t, original.InstancedGLBMeshes[0].Instances[0].ParentMatrix), 0)
}

func TestGroupScaleDeepHierarchyIsDeterministic(t *testing.T) {
	var node Node = Mesh{ID: "leaf", Geometry: CubeGeometry{Size: 1}}
	for depth := 0; depth < 40; depth++ {
		node = Group{
			Position: Vector3{X: 0.01 * float64(depth)},
			Rotation: Euler{Z: 0.005 * float64(depth)},
			Scale:    Vector3{X: 1 + 0.001*float64(depth), Y: 1, Z: 1},
			Children: []Node{node},
		}
	}
	props := Props{Graph: NewGraph(node)}
	first, err := json.Marshal(props.SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(props.SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("deep hierarchy lowering is not deterministic")
	}
	matrix := props.SceneIR().Objects[0].ParentMatrix
	if len(matrix) != 16 {
		t.Fatalf("deep hierarchy parent matrix length = %d", len(matrix))
	}
	for index, value := range matrix {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			t.Fatalf("deep hierarchy parentMatrix[%d] = %v", index, value)
		}
	}
}

func TestGroupScaleTransformPatchSetsChangesAndResetsParentMatrix(t *testing.T) {
	first := affineSlice(affineFromTRS(Vector3{X: 1}, quaternion{W: 1}, Vector3{X: 2, Y: 3, Z: 4}))
	second := append([]float64(nil), first...)
	second[12] = 9
	previous := SceneIR{Objects: []ObjectIR{{ID: "box", Kind: "box"}}}
	next := SceneIR{Objects: []ObjectIR{{ID: "box", Kind: "box", ParentMatrix: first}}}

	assertPatchMatrix := func(want []float64) {
		t.Helper()
		diff := DiffScene(previous, next, DiffOptions{PatchTransforms: true})
		if len(diff.Commands) != 1 || diff.Commands[0].Kind != CommandSetTransform {
			t.Fatalf("commands = %#v, want one set-transform", diff.Commands)
		}
		patch := diff.Commands[0].Data.(TransformPatch)
		if want == nil {
			if patch.ParentMatrix != nil {
				t.Fatalf("parentMatrix = %v, want nil reset", patch.ParentMatrix)
			}
			return
		}
		if patch.ParentMatrix == nil {
			t.Fatal("parentMatrix omitted from transform patch")
		}
		for index, value := range want {
			if (*patch.ParentMatrix)[index] != value {
				t.Fatalf("parentMatrix[%d] = %v, want %v", index, (*patch.ParentMatrix)[index], value)
			}
		}
	}

	assertPatchMatrix(first)
	previous, next = next, SceneIR{Objects: []ObjectIR{{ID: "box", Kind: "box", ParentMatrix: second}}}
	assertPatchMatrix(second)
	previous, next = next, SceneIR{Objects: []ObjectIR{{ID: "box", Kind: "box"}}}
	assertPatchMatrix(nil)

	previous, next = SceneIR{Objects: []ObjectIR{{ID: "box", Kind: "box"}}}, SceneIR{Objects: []ObjectIR{{ID: "box", Kind: "box", Color: "red", ParentMatrix: first}}}
	diff := DiffScene(previous, next, DiffOptions{PatchTransforms: true})
	if len(diff.Commands) != 2 || diff.Commands[0].Kind != CommandRemoveObject || diff.Commands[1].Kind != CommandCreateObject {
		t.Fatalf("mixed matrix/material change = %#v, want atomic remove/create", diff.Commands)
	}
}

func BenchmarkGroupScaleLower1000(b *testing.B) {
	props, unscaled := groupScaleBenchmarkProps()
	baseBytes, err := json.Marshal(unscaled.SceneIR())
	if err != nil {
		b.Fatal(err)
	}
	scaledBytes, err := json.Marshal(props.SceneIR())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.ReportMetric(float64(len(scaledBytes)-len(baseBytes)), "wire-delta-B")
	b.ReportMetric(float64(len(scaledBytes)), "wire-B")
	for index := 0; index < b.N; index++ {
		ir := props.SceneIR()
		if len(ir.Objects) != 1000 {
			b.Fatal(len(ir.Objects))
		}
	}
}

func BenchmarkGroupScaleMarshal1000(b *testing.B) {
	props, unscaled := groupScaleBenchmarkProps()
	ir := props.SceneIR()
	baseBytes, err := json.Marshal(unscaled.SceneIR())
	if err != nil {
		b.Fatal(err)
	}
	scaledBytes, err := json.Marshal(ir)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.ReportMetric(float64(len(scaledBytes)-len(baseBytes)), "wire-delta-B")
	b.ReportMetric(float64(len(scaledBytes)), "wire-B")
	for index := 0; index < b.N; index++ {
		encoded, marshalErr := json.Marshal(ir)
		if marshalErr != nil || len(encoded) != len(scaledBytes) {
			b.Fatalf("marshal = %d bytes, %v", len(encoded), marshalErr)
		}
	}
}

func groupScaleBenchmarkProps() (Props, Props) {
	children := make([]Node, 1000)
	for index := range children {
		children[index] = Mesh{ID: "mesh", Geometry: CubeGeometry{Size: 1}, Position: Vector3{X: float64(index % 25), Y: float64(index / 25)}}
	}
	return Props{Graph: NewGraph(Group{Scale: Vector3{X: 2, Y: 3, Z: 4}, Rotation: Euler{Z: 0.25}, Children: children})},
		Props{Graph: NewGraph(Group{Rotation: Euler{Z: 0.25}, Children: children})}
}

func assertMatrixClose(t *testing.T, got []float64, want affineMatrix, tolerance float64) {
	t.Helper()
	if len(got) != 16 {
		t.Fatalf("matrix length = %d, want 16", len(got))
	}
	for index := range want {
		if math.Abs(got[index]-want[index]) > tolerance {
			t.Fatalf("matrix[%d] = %.15g, want %.15g", index, got[index], want[index])
		}
	}
}

func affineMatrixFromSlice(t *testing.T, values []float64) affineMatrix {
	t.Helper()
	matrix, ok := affineFromValues(values)
	if !ok {
		t.Fatalf("invalid affine values: %v", values)
	}
	return matrix
}

func vectorClose(got, want Vector3, tolerance float64) bool {
	return math.Abs(got.X-want.X) <= tolerance && math.Abs(got.Y-want.Y) <= tolerance && math.Abs(got.Z-want.Z) <= tolerance
}
