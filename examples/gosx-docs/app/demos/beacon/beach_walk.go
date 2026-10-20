package docs

import (
	"math"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

// blackglassBeachWalk makes the beach walkable: the eye follows the same
// terrain the beach mesh is built from, the rocks and the monolith are solid,
// the sea is wadeable to knee depth, and the headlands stop the walker through
// the slope limit.
func blackglassBeachWalk() *scene.Walk {
	grid := beachgen.WalkHeightfield(beachgen.Seed)
	ground := scene.NewWalkGround(grid.MinX, grid.MinZ, grid.SizeX, grid.SizeZ, grid.Cols, grid.Rows, grid.Heights)
	shapes := append(beachgen.WalkColliders(beachgen.Seed), beachgen.JettyColliders()...)
	colliders := make([]scene.WalkCollider, 0, len(shapes))
	for _, s := range shapes {
		colliders = append(colliders, scene.WalkCollider{Kind: s.Kind, X: s.X, Y: s.Y, Z: s.Z, Radius: s.Radius, Height: s.Height})
	}
	colliders = append(colliders, blackglassBeachBeaconCollider())
	colliders = append(colliders, blackglassBeachWreckColliders()...)
	colliders = append(colliders, blackglassBeachGrottoColliders()...)
	var surfaces []scene.WalkSurface
	for _, s := range beachgen.JettySurfaces() {
		surfaces = append(surfaces, scene.WalkSurface{X: s[0], Y: s[1], Z: s[2], SizeX: s[3], SizeZ: s[4], SlopeX: s[5], SlopeZ: s[6]})
	}
	var grounds []scene.WalkGround
	for _, island := range beachgen.OffshoreIslands() {

		for _, shape := range beachgen.OffshoreRockColliders(island) {
			colliders = append(colliders, scene.WalkCollider{Kind: shape.Kind, X: shape.X, Y: shape.Y, Z: shape.Z, Radius: shape.Radius, Height: shape.Height})
		}
		g := beachgen.IslandGrid(island)
		grounds = append(grounds, scene.NewWalkGround(g.MinX, g.MinZ, g.SizeX, g.SizeZ, g.Cols, g.Rows, g.Heights))
		pier := beachgen.IslandPier(island)
		surfaces = append(surfaces, scene.WalkSurface{X: pier[0], Y: pier[1], Z: pier[2], SizeX: pier[3], SizeZ: pier[4]})
	}
	return &scene.Walk{
		Surfaces:  surfaces,
		EyeHeight: 1.7, HeadBob: scene.Float(0.012), Hint: "none",
		WalkSpeed: 3.6,
		MaxSlope:  34,
		Ground:    &ground,
		Grounds:   grounds, OutsideHeight: scene.Float(-1e4),
		Water:     &scene.WalkWater{Level: 0, MaxDepth: 0.55},
		Colliders: colliders,
		Bounds:    &scene.WalkBounds{MinX: -245, MinZ: -430, MaxX: 355, MaxZ: 46},
	}
}

// blackglassBeachLookRotation aims a camera at target with the same yaw and
// pitch the orbit controls derive, so a walk start frames the view exactly
// like the orbit hero shot did.
func blackglassBeachLookRotation(position, target scene.Vector3) scene.Euler {
	fx, fy, fz := target.X-position.X, target.Y-position.Y, target.Z-position.Z
	horizontal := math.Hypot(fx, fz)
	return scene.Euler{X: math.Atan2(fy, horizontal), Y: math.Atan2(-fx, -fz)}
}

// Scanned grain and fractured basalt use world-space projection so steep
// faces keep their scale. The sand's roughness stays in the terrain wetness
// map: a dry detail roughness must not dull the tideline.
func blackglassBeachDetail() *scene.Detail {
	root := blackglassBeachModelRoot + "detail/"
	return &scene.Detail{
		Ground: &scene.DetailLayer{Albedo: root + "sand-albedo.jpg", Normal: root + "sand-normal.jpg", Scale: 2.5, NormalScale: 0.35, AlbedoMix: .65},
		Steep: &scene.DetailLayer{Albedo: root + "basalt-albedo.jpg", Normal: root + "basalt-normal.jpg", Roughness: root + "basalt-rough.jpg",
			Scale: 6, NormalScale: 0.38, AlbedoMix: .4, RoughnessMix: 0.18},
		SlopeStart: 28, SlopeEnd: 42, FadeStart: 24, FadeEnd: 100,
	}
}

// blackglassBeachRockDetail gives the stacks basalt detail on every face.
func blackglassBeachRockDetail() *scene.Detail {
	// A single triplanar layer covers every face, including the crowns.
	// Avoid serializing and baking two identical basalt atlas layers.
	return &scene.Detail{Ground: blackglassBeachDetail().Steep, Triplanar: scene.Bool(true), FadeStart: 55, FadeEnd: 140}
}
