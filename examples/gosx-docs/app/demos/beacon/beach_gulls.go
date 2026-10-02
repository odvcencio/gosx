package docs

import (
	"m31labs.dev/gosx/scene"
	"math"
)

// A single retained flock circles slowly over the surf. Its banked wing
// silhouettes give scale without a sprite texture or a simulation loop.
func blackglassBeachGulls() scene.Mesh {
	g := scene.BufferGeometry{Immutable: true, Revision: 1}
	for i := 0; i < 6; i++ {
		a := float64(i) * 1.27
		x, z := math.Cos(a)*(12+float64(i)*2), math.Sin(a)*(10+float64(i)*2)
		y := float64(i%3) * 1.6
		for _, side := range []float64{-1, 1} {
			momentQuad(&g, scene.Vec3(x, y, z), scene.Vec3(x+side*.45, y+.13, z+.14),
				scene.Vec3(x+side*.9, y+.02, z+.33), scene.Vec3(x+side*.38, y+.06, z+.35))
		}
	}
	return scene.Mesh{ID: "coastal-gulls", Geometry: g, Position: scene.Vec3(-3, 9, -40), Spin: scene.Euler{Y: .028},
		Material: scene.StandardMaterial{Color: "#dddcd2", Roughness: .8}}
}
