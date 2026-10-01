package docs

import (
	"math"

	"m31labs.dev/gosx/scene"
)

// The ocean's inexpensive reflection tiers capture opaque objects only.
// A broken, surface-level glint shares the beam's pivot and sweep, keeping
// the light present on those tiers without another reflection capture.
func blackglassBeachBeamReflection(opacity float64) scene.Mesh {
	g := scene.BufferGeometry{Revision: 1}
	for _, dir := range []float64{1, -1} {
		for i := 0; i < 12; i++ {
			x := 5 + float64(i)*4.8
			w := .16 + x*.014
			z := .25 * math.Sin(float64(i)*2.7)
			momentQuad(&g, scene.Vec3(dir*x, 0, z-w), scene.Vec3(dir*(x+2.4), 0, z-w),
				scene.Vec3(dir*(x+2.4), 0, z+w), scene.Vec3(dir*x, 0, z+w))
			g.UVs = append(g.UVs, momentRound(x/70), 0, momentRound((x+2.4)/70), 0,
				momentRound((x+2.4)/70), 1, momentRound(x/70), 1)
		}
	}
	return scene.Mesh{ID: "beacon-water-glint", Geometry: blackglassShaderGeometry(g),
		Material: blackglassBeamMaterial(opacity*.15, scene.Vec3(beaconX, .07, beaconZ)),
		Visible:  scene.Bool(opacity > 0),
		Position: scene.Vec3(beaconX, .07, beaconZ), Spin: scene.Euler{Y: .45}, DepthWrite: scene.Bool(false)}
}
