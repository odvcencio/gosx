package docs

import (
	"github.com/odvcencio/gosx/scene"
)

// GalaxyScene returns a full-viewport GPU particle galaxy scene.
// 2,800 particles emitted from a two-arm spiral, gold-to-white gradient,
// additive blending, size attenuation, and exponential fog.
func GalaxyScene() scene.Props {
	return scene.Props{
		Background: "#000000",
		Responsive: scene.Bool(true),
		FillHeight: scene.Bool(true),
		Controls:   "orbit",
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 4, 12),
			FOV:      75,
			Near:     0.1,
			Far:      1000,
		},
		Environment: scene.Environment{
			FogColor:   "#000000",
			FogDensity: 0.005,
		},
		Graph: scene.NewGraph(
			scene.ComputeParticles{
				ID:    "galaxy",
				Count: 2800,
				Emitter: scene.ParticleEmitter{
					Kind:     "spiral",
					Position: scene.Vec3(0, 0, 0),
					Radius:   6,
					Rate:     280,
					Lifetime: 10,
					Arms:     2,
					Wind:     0.0,
					Scatter:  0.4,
				},
				Forces: []scene.ParticleForce{
					{Kind: "orbit", Strength: 0.08},
					{Kind: "drag", Strength: 0.02},
				},
				Material: scene.ParticleMaterial{
					Color:       "#D4AF37",
					ColorEnd:    "#ffffff",
					Size:        0.06,
					SizeEnd:     0.02,
					Opacity:     0.9,
					OpacityEnd:  0.0,
					BlendMode:   scene.BlendAdditive,
					Attenuation: true,
				},
				Bounds: 14,
			},
		),
	}
}
