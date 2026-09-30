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
	shapes := beachgen.WalkColliders(beachgen.Seed)
	colliders := make([]scene.WalkCollider, 0, len(shapes))
	for _, s := range shapes {
		colliders = append(colliders, scene.WalkCollider{Kind: s.Kind, X: s.X, Y: s.Y, Z: s.Z, Radius: s.Radius, Height: s.Height})
	}
	return &scene.Walk{
		EyeHeight: 1.7,
		MaxSlope:  34,
		Ground:    &ground,
		Water:     &scene.WalkWater{Level: 0, MaxDepth: 0.55},
		Colliders: colliders,
		Bounds:    &scene.WalkBounds{MinX: -44, MinZ: -30, MaxX: 44, MaxZ: 46},
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
