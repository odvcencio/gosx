package docs

import (
	"math"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

const wreckX, wreckFrontZ = -14.0, 20.0

// A small hull's seven open ribs protrude from the western dunes. Its high
// broken prow hints at the find from the shore; walk around the stern into
// the rib cage for the striped shadows. All 28 rib timbers share one draw.
func blackglassBeachWreck() []scene.Node {
	wood := scene.StandardMaterial{Color: "#73533b", Roughness: .94}
	ribs := scene.InstancedMesh{ID: "wreck-ribs", Count: 28,
		Geometry: scene.CylinderGeometry{RadiusTop: .08, RadiusBottom: .11, Height: 1, Segments: 6},
		Material: wood, CastShadow: true, ReceiveShadow: true}
	for i := 0; i < 7; i++ {
		z := wreckFrontZ + 1.1*float64(i)
		width := blackglassBeachWreckWidth(i)
		for _, side := range []float64{-1, 1} {
			// Each joint uses the terrain under it, so the buried bottom and
			// both raised ends follow the dune rather than one flat plane.
			points := [3]scene.Vector3{}
			for j, p := range [][2]float64{{.12, -.13}, {.7 * width, .65}, {width, 1.8}} {
				x := wreckX + side*p[0]
				points[j] = scene.Vec3(x, beachgen.TerrainHeight(x, z, beachgen.Seed)+p[1], z)
			}
			for j := 1; j < len(points); j++ {
				a, b := points[j-1], points[j]
				dx, dy := b.X-a.X, b.Y-a.Y
				ribs.Positions = append(ribs.Positions, scene.Vec3((a.X+b.X)/2, (a.Y+b.Y)/2, z))
				ribs.Rotations = append(ribs.Rotations, scene.Euler{Z: math.Atan2(-dx, dy)})
				ribs.Scales = append(ribs.Scales, scene.Vec3(1, math.Hypot(dx, dy), 1))
			}
		}
	}
	centreZ := wreckFrontZ + 3.3
	ground := beachgen.TerrainHeight(wreckX, centreZ, beachgen.Seed)
	prowZ := wreckFrontZ - .6
	return []scene.Node{ribs,
		scene.Mesh{ID: "wreck-keel", Geometry: scene.BoxGeometry{Width: .24, Height: .2, Depth: 8},
			Position: scene.Vec3(wreckX, ground-.08, centreZ), Material: wood, ReceiveShadow: true},
		scene.Mesh{ID: "wreck-prow", Geometry: scene.CylinderGeometry{RadiusTop: .09, RadiusBottom: .18, Height: 2.9, Segments: 6},
			Position: scene.Vec3(wreckX, beachgen.TerrainHeight(wreckX, prowZ, beachgen.Seed)+1.25, prowZ),
			Material: wood, CastShadow: true, ReceiveShadow: true},
	}
}

func blackglassBeachWreckWidth(i int) float64 {
	return 1.65 - .2*math.Abs(float64(i-3))
}

func blackglassBeachWreckColliders() []scene.WalkCollider {
	var out []scene.WalkCollider
	for i := 0; i < 7; i++ {
		z := wreckFrontZ + 1.1*float64(i)
		for _, side := range []float64{-1, 1} {
			x := wreckX + side*.7*blackglassBeachWreckWidth(i)
			out = append(out, scene.WalkCollider{Kind: "cylinder", X: x, Z: z,
				Y: beachgen.TerrainHeight(x, z, beachgen.Seed) - .15, Radius: .18, Height: 2.2})
		}
	}
	return append(out, scene.WalkCollider{Kind: "cylinder", X: wreckX, Z: wreckFrontZ - .6,
		Y: beachgen.TerrainHeight(wreckX, wreckFrontZ-.6, beachgen.Seed) - .2, Radius: .18, Height: 2.9})
}
