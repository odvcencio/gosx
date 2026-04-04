package docs

import (
	"math"

	"github.com/odvcencio/gosx/scene"
)

// GeometryZooProgram returns a full-viewport Scene3D showcasing every geometry
// primitive with PBR materials arranged in a semicircle over a shadowed floor.
func GeometryZooProgram() scene.Props {
	// Six geometry types placed in a semicircle of radius 3.2 around the origin.
	// Angles: 0°, 36°, 72°, 108°, 144°, 180° → left-to-right arc facing camera.
	radius := 3.2
	type slot struct {
		angle    float64 // degrees, 0 = right, 180 = left
		geometry scene.Geometry
		material scene.StandardMaterial
		name     string
	}

	slots := []slot{
		{
			angle:    0,
			geometry: scene.SphereGeometry{Radius: 0.62, Segments: 40},
			material: scene.StandardMaterial{
				Color:     "#D4AF37",
				Roughness: 0.15,
				Metalness: 0.95,
			},
			name: "Sphere",
		},
		{
			angle:    36,
			geometry: scene.BoxGeometry{Width: 1.1, Height: 1.1, Depth: 1.1},
			material: scene.StandardMaterial{
				Color:     "#E8E8E8",
				Roughness: 0.1,
				Metalness: 1.0,
			},
			name: "Box",
		},
		{
			angle:    72,
			geometry: scene.PyramidGeometry{Width: 1.0, Height: 1.4, Depth: 1.0},
			material: scene.StandardMaterial{
				Color:     "#1a1a18",
				Roughness: 0.45,
				Metalness: 0.6,
			},
			name: "Pyramid",
		},
		{
			angle:    108,
			geometry: scene.CylinderGeometry{RadiusTop: 0.4, RadiusBottom: 0.5, Height: 1.2, Segments: 32},
			material: scene.StandardMaterial{
				Color:     "#9BA0A8",
				Roughness: 0.4,
				Metalness: 0.9,
			},
			name: "Cylinder",
		},
		{
			angle:    144,
			geometry: scene.TorusGeometry{Radius: 0.52, Tube: 0.18, RadialSegments: 32, TubularSegments: 64},
			material: scene.StandardMaterial{
				Color:     "#c9a227",
				Roughness: 0.3,
				Metalness: 0.8,
			},
			name: "Torus",
		},
		{
			angle:    180,
			geometry: scene.SphereGeometry{Radius: 0.55, Segments: 32},
			material: scene.StandardMaterial{
				Color:     "#B87333",
				Roughness: 0.7,
				Metalness: 0.2,
			},
			name: "Sphere (clay)",
		},
	}

	nodes := []scene.Node{
		// Key light — directional with shadow
		scene.DirectionalLight{
			ID:         "key",
			Color:      "#fff8e7",
			Intensity:  1.2,
			Direction:  scene.Vec3(0.4, -1.0, -0.6),
			CastShadow: true,
			ShadowBias: -0.001,
			ShadowSize: 2048,
		},
		// Gold accent fill
		scene.PointLight{
			ID:        "fill",
			Color:     "#D4AF37",
			Intensity: 0.7,
			Position:  scene.Vec3(-3, 4, 3),
			Range:     18,
			Decay:     2,
		},
		// Floor plane
		scene.Mesh{
			ID: "floor",
			Geometry: scene.PlaneGeometry{Width: 14, Height: 14},
			Material: scene.StandardMaterial{
				Color:     "#111110",
				Roughness: 0.85,
				Metalness: 0.05,
			},
			Rotation:      scene.Rotate(-math.Pi/2, 0, 0),
			ReceiveShadow: true,
		},
	}

	// Place each geometry type along the semicircle.
	for _, s := range slots {
		rad := s.angle * math.Pi / 180.0
		x := radius * math.Cos(rad)
		z := radius * math.Sin(rad)
		// Bring the arc forward so it faces the default camera (+Z).
		// The arc spans 0–180° in XZ, so shift Z so the centre is at z≈0.
		z = z - (radius * 0.5)

		mesh := scene.Mesh{
			ID:            s.name,
			Geometry:      s.geometry,
			Material:      s.material,
			Position:      scene.Vec3(x-radius*0.5, 0.65, z),
			CastShadow:    true,
			ReceiveShadow: true,
		}
		nodes = append(nodes, mesh)
	}

	return scene.Props{
		Background:  "#09141e",
		Responsive:  scene.Bool(true),
		FillHeight:  scene.Bool(true),
		Controls:    "orbit",
		AutoRotate:  scene.Bool(false),
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 2.5, 8),
			FOV:      58,
			Near:     0.1,
			Far:      200,
		},
		ControlTarget: scene.Vec3(0, 0.5, 0),
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.18,
			Exposure:         1.1,
			ToneMapping:      "aces",
		},
		Graph: scene.NewGraph(nodes...),
	}
}
