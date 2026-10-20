package beachgen

import (
	"math"
	"sync"
)

// Walk data for first-person navigation. The heightfield samples the same
// terrain function the beach mesh uses, so the eye follows the sand the
// visitor sees. The colliders come from the same stack, boulder and monolith
// specs that build the meshes.

// Seed is the seed the committed demo assets were generated with.
const Seed int64 = 0xB1AC6A55

// WalkGrid is a row-major heightfield: rows along +Z, columns along +X.
type WalkGrid struct {
	MinX, MinZ, SizeX, SizeZ float64
	Cols, Rows               int
	Heights                  []float64
}

// WalkShape is one collider: a vertical cylinder or a sphere.
type WalkShape struct {
	Kind            string // "cylinder" or "sphere"
	X, Y, Z, Radius float64
	Height          float64 // cylinder height; 0 means unbounded
}

// Walk area and resolution. 97 x 73 samples at 1.25 m cover the whole
// terrain mesh (x -60..60, z -40..50) in about 14 KB of uint16 heights.
const (
	walkMinX, walkMinZ   = -60.0, -40.0
	walkSizeX, walkSizeZ = 120.0, 90.0
	walkCols, walkRows   = 97, 73
)

// WalkHeightfield samples the terrain for ground following.
func WalkHeightfield(seed int64) WalkGrid {
	n := newNoise(seed)
	heights := make([]float64, 0, walkCols*walkRows)
	for row := 0; row < walkRows; row++ {
		z := walkMinZ + walkSizeZ*float64(row)/float64(walkRows-1)
		for col := 0; col < walkCols; col++ {
			x := walkMinX + walkSizeX*float64(col)/float64(walkCols-1)
			heights = append(heights, terrainHeight(n, x, z))
		}
	}
	return WalkGrid{walkMinX, walkMinZ, walkSizeX, walkSizeZ, walkCols, walkRows, heights}
}

// MonolithX, MonolithZ and MonolithYaw place the obsidian monolith.
const (
	MonolithX, MonolithY, MonolithZ = -6.2, 0.05, 2.6
	MonolithYaw                     = -.4
)

// WalkColliders returns the solid shapes a walker must not enter.
func WalkColliders(seed int64) []WalkShape {
	if seed == Seed {
		seedCollidersOnce.Do(func() { seedColliders = walkColliders(seed) })
		return append([]WalkShape(nil), seedColliders...)
	}
	return walkColliders(seed)
}

var seedCollidersOnce sync.Once
var seedColliders []WalkShape

func walkColliders(seed int64) []WalkShape {
	n := newNoise(seed)
	var out []WalkShape
	for _, s := range stackSpecs(seed) {
		// Enclose the actual hull/wading band, including lean and buttresses.
		// A blanket percentage trapped the clipper in its adjacent mooring.
		g, radius := &geometry{}, 0.0
		addStack(g, s)
		for i := 0; i < len(g.positions); i += 3 {
			if g.positions[i+1] >= -2.5 && g.positions[i+1] <= 3 {
				radius = math.Max(radius, math.Hypot(g.positions[i]-s.x, g.positions[i+2]-s.z))
			}
		}
		radius = math.Ceil((radius+.05)*1000) / 1000
		out = append(out, WalkShape{Kind: "cylinder", X: s.x, Y: -6, Z: s.z, Radius: radius, Height: s.height + 6})
	}
	for _, b := range boulderSpecs(seed) {
		out = append(out, WalkShape{Kind: "sphere", X: b.x, Y: terrainHeight(n, b.x, b.z), Z: b.z, Radius: b.radius * 1.05})
	}
	// The monolith is a flattened blade about 1.9 m by 0.9 m and 3.4 m tall.
	out = append(out, WalkShape{Kind: "cylinder", X: MonolithX, Y: MonolithY, Z: MonolithZ, Radius: 0.75, Height: 3.5})
	return out
}
