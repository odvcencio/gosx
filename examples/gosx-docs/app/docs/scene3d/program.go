package docs

import (
	"github.com/odvcencio/gosx/scene"
)

func DemoScene() scene.Props {
	return scene.Props{
		Width:      800,
		Height:     500,
		Background: "#08151f",
		Responsive: scene.Bool(true),
		Controls:   "orbit",
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 1.5, 5),
			FOV:      60,
		},
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.2,
			Exposure:         1.1,
			ToneMapping:      "aces",
		},
		Graph: scene.NewGraph(
			scene.DirectionalLight{
				Color:      "#fff1d6",
				Intensity:  1.0,
				Direction:  scene.Vec3(0.3, -1, -0.5),
				CastShadow: true,
			},
			scene.PointLight{
				Color:     "#D4AF37",
				Intensity: 0.6,
				Position:  scene.Vec3(-2, 3, 2),
				Range:     15,
			},
			scene.Mesh{
				Geometry: scene.SphereGeometry{Segments: 32},
				Material: scene.StandardMaterial{
					Color:     "#D4AF37",
					Roughness: 0.3,
					Metalness: 0.9,
				},
				Position:      scene.Vec3(-1.5, 0.5, 0),
				CastShadow:    true,
				ReceiveShadow: true,
			},
			scene.Mesh{
				Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1},
				Material: scene.StandardMaterial{
					Color:     "#E8E8E8",
					Roughness: 0.1,
					Metalness: 1.0,
				},
				Position:      scene.Vec3(1.5, 0.5, 0),
				Spin:          scene.Rotate(0, 0.004, 0),
				CastShadow:    true,
				ReceiveShadow: true,
			},
			scene.Mesh{
				Geometry: scene.TorusGeometry{
					Radius:          0.5,
					Tube:            0.15,
					RadialSegments:  32,
					TubularSegments: 64,
				},
				Material: scene.StandardMaterial{
					Color:     "#c9a227",
					Roughness: 0.4,
					Metalness: 0.7,
				},
				Position:      scene.Vec3(0, 0.5, 0),
				Spin:          scene.Rotate(0.003, 0.005, 0),
				CastShadow:    true,
				ReceiveShadow: true,
			},
			scene.Mesh{
				Geometry: scene.PlaneGeometry{Width: 8, Height: 8},
				Material: scene.StandardMaterial{
					Color:     "#1a1a18",
					Roughness: 0.8,
					Metalness: 0.1,
				},
				Rotation:      scene.Rotate(-1.5708, 0, 0),
				ReceiveShadow: true,
			},
		),
	}
}
