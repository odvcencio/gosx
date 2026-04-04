package docs

import (
	"math"

	"github.com/odvcencio/gosx/scene"
)

func HeroScene() scene.Props {
	return scene.Props{
		Width:      1920,
		Height:     1080,
		Background: "#000000",
		Responsive: scene.Bool(true),
		FillHeight: scene.Bool(true),
		Controls:   "orbit",
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 2, 8),
			FOV:      65,
			Near:     0.1,
			Far:      1000,
		},
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.15,
			Exposure:         1.2,
			ToneMapping:      "aces",
			FogColor:         "#000000",
			FogDensity:       0.008,
		},
		Graph: scene.NewGraph(
			scene.DirectionalLight{
				Color:      "#fff1d6",
				Intensity:  1.2,
				Direction:  scene.Vec3(0.3, -1, -0.5),
				CastShadow: true,
			},
			scene.PointLight{
				Color:     "#D4AF37",
				Intensity: 0.8,
				Position:  scene.Vec3(-3, 4, 2),
				Range:     20,
			},
			scene.Mesh{
				Geometry: scene.TorusGeometry{
					Radius:          2.5,
					Tube:            0.08,
					RadialSegments:  64,
					TubularSegments: 128,
				},
				Material: scene.StandardMaterial{
					Color:     "#D4AF37",
					Roughness: 0.2,
					Metalness: 0.9,
				},
				Position:      scene.Vec3(0, 1.5, 0),
				Rotation:      scene.Rotate(math.Pi/6, 0, 0),
				Spin:          scene.Rotate(0, 0.003, 0),
				CastShadow:    true,
				ReceiveShadow: true,
			},
			scene.Mesh{
				Geometry: scene.SphereGeometry{Segments: 48},
				Material: scene.StandardMaterial{
					Color:     "#E8E8E8",
					Roughness: 0.1,
					Metalness: 1.0,
				},
				Position:      scene.Vec3(0, 1.5, 0),
				CastShadow:    true,
				ReceiveShadow: true,
			},
			scene.Mesh{
				Geometry: scene.BoxGeometry{Width: 0.6, Height: 0.6, Depth: 0.6},
				Material: scene.StandardMaterial{
					Color:     "#C0C0C0",
					Roughness: 0.3,
					Metalness: 0.8,
				},
				Position:      scene.Vec3(3.5, 0.8, -1),
				Spin:          scene.Rotate(0, 0.005, 0),
				Drift:         scene.Vec3(0, 0.3, 0),
				DriftSpeed:    0.8,
				CastShadow:    true,
				ReceiveShadow: true,
			},
			scene.Mesh{
				Geometry: scene.PyramidGeometry{},
				Material: scene.StandardMaterial{
					Color:     "#c9a227",
					Roughness: 0.4,
					Metalness: 0.7,
				},
				Position:      scene.Vec3(-3, 0.6, 1),
				Spin:          scene.Rotate(0, -0.004, 0),
				Drift:         scene.Vec3(0, 0.2, 0),
				DriftSpeed:    1.2,
				CastShadow:    true,
				ReceiveShadow: true,
			},
			scene.Mesh{
				Geometry: scene.PlaneGeometry{},
				Material: scene.StandardMaterial{
					Color:     "#1a1a18",
					Roughness: 0.8,
					Metalness: 0.1,
				},
				Position:      scene.Vec3(0, -0.5, 0),
				Rotation:      scene.Rotate(-math.Pi/2, 0, 0),
				ReceiveShadow: true,
			},
			scene.ComputeParticles{
				Count: 2000,
				Emitter: scene.ParticleEmitter{
					Kind:     "sphere",
					Position: scene.Vec3(0, 2, 0),
					Radius:   5,
					Rate:     200,
					Lifetime: 4,
				},
				Forces: []scene.ParticleForce{
					{Kind: "gravity", Strength: 0.5, Direction: scene.Vec3(0, -1, 0)},
					{Kind: "turbulence", Strength: 0.3},
				},
				Material: scene.ParticleMaterial{
					Color:       "#D4AF37",
					ColorEnd:    "#ffffff",
					Size:        0.04,
					SizeEnd:     0.01,
					Opacity:     0.8,
					OpacityEnd:  0.0,
					BlendMode:   scene.BlendAdditive,
					Attenuation: true,
				},
				Bounds: 12,
			},
		),
	}
}
