package docs

import (
	"math"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

// Alternating wet boot prints start two metres in front of the shore view
// and end beside the glass. Two primitive batches make toes and heels for
// 24 prints; no decals, textures, proximity loop or extra walk obstacles.
func blackglassBeachTrail() []scene.Node {
	material := scene.StandardMaterial{Color: "#090d10", Roughness: .88}
	sole := scene.InstancedMesh{ID: "glass-trail-soles", Count: 24,
		Geometry: scene.CylinderGeometry{RadiusTop: 1, RadiusBottom: 1, Height: 1, Segments: 8}, Material: material, ReceiveShadow: true}
	heel := sole
	heel.ID = "glass-trail-heels"
	dx, dz := -4.95, -15.8
	length := math.Hypot(dx, dz)
	fx, fz := dx/length, dz/length
	yaw := math.Atan2(-fx, -fz)
	for i := 0; i < sole.Count; i++ {
		f := float64(i) / float64(sole.Count-1)
		side := .14
		if i%2 == 1 {
			side = -side
		}
		x, z := .15+dx*f-fz*side, 20+dz*f+fx*side
		for j, batch := range []*scene.InstancedMesh{&sole, &heel} {
			px, pz := x, z
			scale := scene.Vec3(.095, .008, .145)
			if j == 1 {
				px, pz = x-fx*.19, z-fz*.19
				scale = scene.Vec3(.07, .008, .065)
			}
			batch.Positions = append(batch.Positions, scene.Vec3(momentRound(px),
				momentRound(beachgen.TerrainHeight(px, pz, beachgen.Seed)+.018), momentRound(pz)))
			batch.Rotations = append(batch.Rotations, scene.Euler{Y: momentRound(yaw + side*.12)})
			batch.Scales = append(batch.Scales, scale)
		}
	}
	return []scene.Node{sole, heel}
}
