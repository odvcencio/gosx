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
	var surfaces []scene.WalkSurface
	for _, s := range beachgen.JettySurfaces() {
		surfaces = append(surfaces, scene.WalkSurface{X: s[0], Y: s[1], Z: s[2], SizeX: s[3], SizeZ: s[4], SlopeX: s[5], SlopeZ: s[6]})
	}
	return &scene.Walk{
		Surfaces:  surfaces,
		EyeHeight: 1.7,
		MaxSlope:  34,
		Ground:    &ground,
		Water:     &scene.WalkWater{Level: 0, MaxDepth: 0.55},
		Colliders: colliders,
		Bounds:    &scene.WalkBounds{MinX: -115, MinZ: -180, MaxX: 115, MaxZ: 46},
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
