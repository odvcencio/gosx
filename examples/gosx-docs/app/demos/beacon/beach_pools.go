package docs

import (
	"fmt"
	"math"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

// Three still pools on the eastern shore catch the sun between their basalt
// rims. Approach the silver patches and circle them: the PBR specular and
// grazing-angle rim brighten as the walker changes view, in every light tier.
func blackglassBeachPoolSites() [][3]float64 {
	return [][3]float64{{9, 5, .9}, {10.8, 3.8, .65}, {11.8, 5.8, 1.05}}
}

func blackglassBeachPoolLevel(x, z, radius float64) float64 {
	h := beachgen.TerrainHeight(x, z, beachgen.Seed)
	for i := 0; i < 12; i++ {
		a := 2 * math.Pi * float64(i) / 12
		h = math.Max(h, beachgen.TerrainHeight(x+radius*math.Cos(a), z+radius*math.Sin(a), beachgen.Seed))
	}
	return h + .025
}

func blackglassBeachPools() []scene.Node {
	water := scene.StandardMaterial{Color: "#183c40", Roughness: .035, Clearcoat: 1, IOR: scene.Float(1.333),
		RimColor: &[3]float64{.55, .72, .8}, RimPower: 5, RimStrength: .3}
	rim := scene.BufferGeometry{Immutable: true, Revision: 1}
	nodes := make([]scene.Node, 0, 4)
	for i, site := range blackglassBeachPoolSites() {
		x, z, radius := site[0], site[1], site[2]
		level := blackglassBeachPoolLevel(x, z, radius)
		nodes = append(nodes, scene.Mesh{ID: fmt.Sprintf("tide-pool-%d", i),
			Geometry: scene.CylinderGeometry{RadiusTop: radius, RadiusBottom: radius, Height: .02, Segments: 12},
			Position: scene.Vec3(x, level-.01, z), Material: water, ReceiveShadow: true})
		point := func(angle, r float64, inner bool) scene.Vector3 {
			px, pz := x+r*math.Cos(angle), z+r*math.Sin(angle)
			y := beachgen.TerrainHeight(px, pz, beachgen.Seed) + .018
			if inner {
				y = level + .045
			}
			return scene.Vec3(px, y, pz)
		}
		for s := 0; s < 12; s++ {
			a, b := 2*math.Pi*float64(s)/12, 2*math.Pi*float64(s+1)/12
			momentQuad(&rim, point(a, radius, true), point(b, radius, true), point(b, radius+.24, false), point(a, radius+.24, false))
		}
	}
	return append(nodes, scene.Mesh{ID: "tide-pool-rims", Geometry: rim,
		Material: scene.StandardMaterial{Color: "#23292a", Roughness: .65}, CastShadow: true, ReceiveShadow: true})
}
