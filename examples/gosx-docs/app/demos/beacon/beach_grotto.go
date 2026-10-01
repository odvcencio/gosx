package docs

import (
	"math"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

const grottoX, grottoFrontZ, grottoBackZ = -25.6, 8.2, 13.2

// A basalt hollow at the western headland's foot faces the low north-west
// sun. Walk through the arch: the back of the hollow catches a warm patch
// only at golden hour. A faint emissive floor tint keeps this readable on
// tiers without shadows; it adds no lights, atmosphere passes or assets.
func blackglassBeachGrotto(periodID string) []scene.Node {
	rock := scene.StandardMaterial{Color: "#1e2428", Roughness: .92}
	arch := scene.BufferGeometry{Immutable: true, Revision: 1}
	point := func(angle, z float64, outer bool) scene.Vector3 {
		radius, height := 1.8, 2.4
		if outer {
			radius, height = 2.8, 3.3
		}
		x := grottoX + radius*math.Cos(angle)
		return scene.Vec3(x, beachgen.TerrainHeight(x, z, beachgen.Seed)+height*math.Sin(angle), z)
	}
	for i := 0; i < 8; i++ {
		a, b := math.Pi*float64(i)/8, math.Pi*float64(i+1)/8
		fi, fo := point(a, grottoFrontZ, false), point(a, grottoFrontZ, true)
		gi, nextOuter := point(b, grottoFrontZ, false), point(b, grottoFrontZ, true)
		bi, bo := point(a, grottoBackZ, false), point(a, grottoBackZ, true)
		ci, co := point(b, grottoBackZ, false), point(b, grottoBackZ, true)
		momentQuad(&arch, fi, gi, nextOuter, fo) // front face, toward the sea
		momentQuad(&arch, bi, bo, co, ci)
		momentQuad(&arch, fo, nextOuter, co, bo) // exterior roof
		momentQuad(&arch, fi, bi, ci, gi)        // interior vault
	}
	backGround := beachgen.TerrainHeight(grottoX, grottoBackZ, beachgen.Seed)
	patch := scene.BufferGeometry{Immutable: true, Revision: 1}
	floor := func(x, z float64) scene.Vector3 {
		return scene.Vec3(x, beachgen.TerrainHeight(x, z, beachgen.Seed)+.025, z)
	}
	momentQuad(&patch, floor(grottoX-.9, 10.7), floor(grottoX-.9, 12.6), floor(grottoX+.55, 12.6), floor(grottoX+.55, 10.7))
	warm := [3]float64{1, .42, .12}
	return []scene.Node{
		scene.Mesh{ID: "sun-grotto-arch", Geometry: arch, Material: rock, CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "sun-grotto-back", Geometry: scene.BoxGeometry{Width: 3.6, Height: 2.7, Depth: .5},
			Position: scene.Vec3(grottoX, backGround+1.15, grottoBackZ+.25), Material: rock, CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "sun-grotto-patch", Geometry: patch, Visible: scene.Bool(periodID == beachgen.PeriodGolden),
			Material: scene.StandardMaterial{Color: "#93602e", Roughness: .88, EmissiveColor: &warm, Emissive: .16}, ReceiveShadow: true},
	}
}

func blackglassBeachGrottoColliders() []scene.WalkCollider {
	ground := beachgen.TerrainHeight(grottoX, grottoFrontZ, beachgen.Seed)
	return []scene.WalkCollider{
		{Kind: "box", X: grottoX - 2.5, Y: ground + 2, Z: (grottoFrontZ + grottoBackZ) / 2, SizeX: 2, SizeY: 4, SizeZ: 5},
		{Kind: "box", X: grottoX + 2.5, Y: ground + 2, Z: (grottoFrontZ + grottoBackZ) / 2, SizeX: 2, SizeY: 4, SizeZ: 5},
		{Kind: "box", X: grottoX, Y: ground + 2, Z: grottoBackZ + .25, SizeX: 5.6, SizeY: 4, SizeZ: .5},
	}
}
