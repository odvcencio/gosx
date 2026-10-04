package scene

// Replacement commands carry zero volume controls explicitly. Initial scene
// records retain their compact serialization, and partial material patches can
// still omit fields without resetting them.
type instancedMeshReplacement struct {
	InstancedMeshIR
	Thickness           float64    `json:"thickness"`
	AttenuationDistance float64    `json:"attenuationDistance"`
	AttenuationColor    [3]float64 `json:"attenuationColor"`
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
		out[i] = instancedMeshReplacement{mesh, mesh.Thickness, mesh.AttenuationDistance, tint}
	}
	return out
}
