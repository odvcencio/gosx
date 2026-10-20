package docs

import (
	"math"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

const (
	grottoX, grottoZ          = -20.8, 10.7
	grottoFrontX, grottoBackX = grottoX + 2.5, grottoX - 2.5
)

// A basalt hollow at the western headland's foot faces the low eastern
// sun. Walk through the arch: the back of the hollow catches a warm patch
// only at golden hour. A faint emissive floor tint keeps this readable on
// tiers without shadows; it adds no lights, atmosphere passes or assets.
func blackglassBeachGrotto(periodID string) []scene.Node {
	rock := scene.StandardMaterial{Color: "#1e2428", Roughness: .92}
	arch := scene.BufferGeometry{Immutable: true, Revision: 1}
	point := func(angle, x float64, outer bool) scene.Vector3 {
		radius, height := 1.8, 2.4
		if outer {
			radius, height = 2.8, 3.3
		}
		z := grottoZ + radius*math.Cos(angle)
		return scene.Vec3(x, beachgen.TerrainHeight(x, z, beachgen.Seed)+height*math.Sin(angle), z)
	}
	for i := 0; i < 8; i++ {
		a, b := math.Pi*float64(i)/8, math.Pi*float64(i+1)/8
		fi, fo := point(a, grottoFrontX, false), point(a, grottoFrontX, true)
		gi, nextOuter := point(b, grottoFrontX, false), point(b, grottoFrontX, true)
		bi, bo := point(a, grottoBackX, false), point(a, grottoBackX, true)
		ci, co := point(b, grottoBackX, false), point(b, grottoBackX, true)
		momentQuad(&arch, fi, gi, nextOuter, fo) // front face, toward the sea
		momentQuad(&arch, bi, bo, co, ci)
		momentQuad(&arch, fo, nextOuter, co, bo) // exterior roof
		momentQuad(&arch, fi, bi, ci, gi)        // interior vault
	}
	backGround := beachgen.TerrainHeight(grottoBackX, grottoZ, beachgen.Seed)
	patch := scene.BufferGeometry{Immutable: true, Revision: 1}
	floor := func(x, z float64) scene.Vector3 {
		return scene.Vec3(x, beachgen.TerrainHeight(x, z, beachgen.Seed)+.025, z)
	}
	momentQuad(&patch, floor(grottoBackX+.6, grottoZ-1), floor(grottoBackX+.6, grottoZ+.45), floor(grottoBackX+2.5, grottoZ+.45), floor(grottoBackX+2.5, grottoZ-1))
	warm := [3]float64{1, .42, .12}
	return []scene.Node{
		// The thin vault casts onto the sand without receiving its own
		// coarse cascade samples on the interior walls.
		scene.Mesh{ID: "sun-grotto-arch", Geometry: arch, Material: rock, CastShadow: true},
		scene.Mesh{ID: "sun-grotto-back", Geometry: scene.BoxGeometry{Width: .5, Height: 2.7, Depth: 3.6},
			Position: scene.Vec3(grottoBackX-.25, backGround+1.15, grottoZ), Material: rock, CastShadow: true},
		scene.Mesh{ID: "sun-grotto-patch", Geometry: patch, Visible: scene.Bool(periodID == beachgen.PeriodGolden),
			Material: scene.StandardMaterial{Color: "#93602e", Roughness: .88, EmissiveColor: &warm, Emissive: .16}, ReceiveShadow: true},
	}
}

func blackglassBeachGrottoColliders() []scene.WalkCollider {
	ground := beachgen.TerrainHeight(grottoFrontX, grottoZ, beachgen.Seed)
	return []scene.WalkCollider{
		{Kind: "box", X: grottoX, Y: ground + 2, Z: grottoZ - 2.5, SizeX: 5, SizeY: 4, SizeZ: 2},
		{Kind: "box", X: grottoX, Y: ground + 2, Z: grottoZ + 2.5, SizeX: 5, SizeY: 4, SizeZ: 2},
		{Kind: "box", X: grottoBackX - .25, Y: ground + 2, Z: grottoZ, SizeX: .5, SizeY: 4, SizeZ: 5.6},
	}
}
