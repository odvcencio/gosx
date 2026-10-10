package scene

// Replacement commands carry zero volume controls explicitly. Initial scene
// records retain their compact serialization, and partial material patches can
// still omit fields without resetting them.
type instancedMeshReplacement struct {
	InstancedMeshIR
	ThicknessMap        string               `json:"thicknessMap"`
	SpecularAA          *GeometricSpecularAA `json:"specularAA"`
	Thickness           float64              `json:"thickness"`
	AttenuationDistance float64              `json:"attenuationDistance"`
	AttenuationColor    [3]float64           `json:"attenuationColor"`
}

func instancedMeshReplacements(meshes []InstancedMeshIR) []instancedMeshReplacement {
	if meshes == nil {
		return nil
	}
	out := make([]instancedMeshReplacement, len(meshes))
	for i, mesh := range meshes {
		tint := [3]float64{1, 1, 1}
		if mesh.AttenuationColor != nil {
			tint = *mesh.AttenuationColor
		}
		out[i] = instancedMeshReplacement{InstancedMeshIR: mesh, ThicknessMap: mesh.ThicknessMap, SpecularAA: copySpecularAA(mesh.SpecularAA), Thickness: mesh.Thickness, AttenuationDistance: mesh.AttenuationDistance, AttenuationColor: tint}
	}
	return out
}
