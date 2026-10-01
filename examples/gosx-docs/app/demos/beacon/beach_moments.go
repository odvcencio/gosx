package docs

import (
	"math"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

// The beacon: a lighthouse on the eastern headland whose beam sweeps the haze.
// It is built from primitive meshes and one spinning beam mesh, so it adds no
// asset bytes. The beam is strong at blue hour and faint in daylight.
const (
	beaconX, beaconZ = 39.0, -33.0
	beaconTower      = 9.0
)

func blackglassBeachBeacon(periodID string) []scene.Node {
	ground := beachgen.TerrainHeight(beaconX, beaconZ, beachgen.Seed)
	lantern := ground + beaconTower + 0.8
	glow, beamCore, beamHalo := 1.5, 0.05, 0.02
	if periodID == beachgen.PeriodBlue {
		glow, beamCore, beamHalo = 9, 0.22, 0.07
	}
	white := scene.StandardMaterial{Color: "#e9e4dc", Roughness: 0.7}
	red := scene.StandardMaterial{Color: "#8e2a22", Roughness: 0.6}
	dark := scene.StandardMaterial{Color: "#1c1d1f", Roughness: 0.5, Metalness: 0.6}
	warm := [3]float64{1, 0.82, 0.55}
	light := scene.StandardMaterial{Color: "#fff1d6", EmissiveColor: &warm, Emissive: glow, Roughness: 0.2}
	beam := func(id string, length, radius, opacity float64) scene.Mesh {
		return scene.Mesh{ID: id, Geometry: blackglassBeamGeometry(length, radius, 12),
			Material: scene.StandardMaterial{Color: "#ffe9c4", EmissiveColor: &warm, Emissive: 2, Opacity: scene.Float(opacity),
				BlendMode: scene.BlendAdditive},
			Position: scene.Vec3(beaconX, lantern, beaconZ), Spin: scene.Euler{Y: 0.45},
			DepthWrite: scene.Bool(false), CastShadow: false, ReceiveShadow: false}
	}
	y := func(h float64) scene.Vector3 { return scene.Vec3(beaconX, ground+h, beaconZ) }
	return []scene.Node{
		scene.Mesh{ID: "beacon-tower", Geometry: scene.CylinderGeometry{RadiusTop: 1.1, RadiusBottom: 1.6, Height: beaconTower, Segments: 24},
			Material: white, Position: y(beaconTower / 2), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "beacon-band", Geometry: scene.CylinderGeometry{RadiusTop: 1.24, RadiusBottom: 1.33, Height: 1.6, Segments: 24},
			Material: red, Position: y(beaconTower * 0.55), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "beacon-gallery", Geometry: scene.CylinderGeometry{RadiusTop: 1.5, RadiusBottom: 1.5, Height: 0.25, Segments: 24},
			Material: dark, Position: y(beaconTower + 0.1), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "beacon-lantern", Geometry: scene.CylinderGeometry{RadiusTop: 0.8, RadiusBottom: 0.8, Height: 1.1, Segments: 16},
			Material: light, Position: y(beaconTower + 0.8)},
		scene.Mesh{ID: "beacon-roof", Geometry: scene.CylinderGeometry{RadiusTop: 0.05, RadiusBottom: 1.1, Height: 0.9, Segments: 16},
			Material: red, Position: y(beaconTower + 1.8), CastShadow: true},
		beam("beacon-beam", 70, 6, beamCore),
		beam("beacon-beam-halo", 55, 12, beamHalo),
	}
}

// blackglassBeamGeometry builds two opposed open, double-sided cones with their
// apex at the origin, pointing along +X and -X, so a Y spin sweeps them around
// the lantern.
func blackglassBeamGeometry(length, radius float64, segments int) scene.BufferGeometry {
	var positions, normals []float64
	var indices []int
	for _, dir := range []float64{1, -1} {
		apex := len(positions) / 3
		positions = append(positions, 0, 0, 0)
		normals = append(normals, 0, 1, 0)
		for i := 0; i <= segments; i++ {
			a := 2 * math.Pi * float64(i) / float64(segments)
			cy, cz := math.Cos(a), math.Sin(a)
			positions = append(positions, dir*length, momentRound(radius*cy), momentRound(radius*cz))
			normals = append(normals, 0, momentRound(cy), momentRound(cz))
		}
		// Both windings: the cone reads from outside and inside, and the near
		// and far walls add up so the core looks denser than the edges.
		for i := 0; i < segments; i++ {
			indices = append(indices, apex, apex+1+i, apex+2+i, apex, apex+2+i, apex+1+i)
		}
	}
	return scene.BufferGeometry{Positions: positions, Normals: normals, Indices: indices, Immutable: true, Revision: 1}
}

// Millimetre precision keeps procedural geometry compact on the wire.
func momentRound(v float64) float64 { return math.Round(v*1000) / 1000 }

func blackglassBeachBeaconCollider() scene.WalkCollider {
	return scene.WalkCollider{Kind: "cylinder", X: beaconX, Z: beaconZ,
		Y: beachgen.TerrainHeight(beaconX, beaconZ, beachgen.Seed), Radius: 1.6, Height: beaconTower + 2.25}
}
