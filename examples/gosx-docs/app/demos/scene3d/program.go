package docs

import (
	"m31labs.dev/gosx/scene"
)

// GeometryZooProgram presents seven materials in one orbitable composition.
func GeometryZooProgram() scene.Props {
	return scene.Props{
		Width:               1200,
		Height:              760,
		Background:          "#111d26",
		Responsive:          scene.Bool(true),
		FillHeight:          scene.Bool(true),
		MaxFPS:              60,
		MaxDevicePixelRatio: 1.5,
		MaxPixels:           scene.PostFXMaxPixels540p,
		DeferPostFX:         scene.Bool(true),
		Controls:            "orbit",
		AutoRotate:          scene.Bool(true), // gentle turntable keeps the scene alive
		Camera:              cinematicCamera(),
		ControlTarget:       scene.Vec3(0, 1.4, 0),
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.26,
		},
		PostFX: postFX(),
		Graph:  scene.NewGraph(sceneNodes()...),
	}
}

// cinematicCamera frames the whole material study.
func cinematicCamera() scene.PerspectiveCamera {
	return scene.PerspectiveCamera{
		Position: scene.Vec3(0, 2.7, 8.3),
		FOV:      48,
		Near:     0.1,
		Far:      200,
	}
}

// postFX keeps highlight treatment within the shared scene pipeline.
func postFX() scene.PostFX {
	return scene.PostFX{
		Effects: []scene.PostEffect{
			scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.15},
			scene.Bloom{Threshold: 0.85, Strength: 0.45, Radius: 14},
			scene.Vignette{Intensity: 0.55},
			scene.ColorGrade{Exposure: 1.0, Contrast: 1.05, Saturation: 1.08},
		},
	}
}

// sceneNodes returns every light and mesh in the scene.
func sceneNodes() []scene.Node {
	nodes := []scene.Node{}
	nodes = append(nodes, lights()...)
	nodes = append(nodes, floor())
	nodes = append(nodes, heroMeshes()...)
	return nodes
}

// lights adds key, fill, rim, and ambient light.
func lights() []scene.Node {
	return []scene.Node{
		// Key light — warm white directional, primary shadow caster.
		scene.DirectionalLight{
			ID:         "key",
			Color:      "#fff2e0", // warm white, slight amber tint
			Intensity:  1.5,
			Direction:  scene.Vec3(0.5, -1.0, -0.7),
			CastShadow: true,
			ShadowBias: -0.001,
			ShadowSize: 2048,
		},
		scene.PointLight{
			ID:        "fill",
			Color:     "#a7bfbe",
			Intensity: 0.6,
			Position:  scene.Vec3(-4, 3, 2),
			Range:     22,
			Decay:     2,
		},
		// Rim light — hot warm point behind the scene, creates silhouette pop.
		scene.PointLight{
			ID:        "rim",
			Color:     "#ffe8c0", // warm amber-white
			Intensity: 2.2,
			Position:  scene.Vec3(1, 4, -8),
			Range:     28,
			Decay:     2,
		},
		// Hemisphere ambient — sky blue-grey, ground near-black, soft fill.
		scene.HemisphereLight{
			ID:          "hemi",
			SkyColor:    "#3a4f6a",
			GroundColor: "#1a1a1e",
			Intensity:   0.4,
		},
	}
}

// floor is a low circular plinth that anchors the floating specimens.
func floor() scene.Node {
	return scene.Mesh{
		ID:       "floor",
		Geometry: scene.CylinderGeometry{RadiusTop: 3.8, RadiusBottom: 4, Height: 0.24, Segments: 64},
		Material: scene.StandardMaterial{
			Color:     "#22313a",
			Roughness: 0.24,
			Metalness: 0.06,
			Clearcoat: 0.65,
		},
		Position:      scene.Vec3(0, -0.18, 0),
		ReceiveShadow: true,
	}
}

// heroMeshes groups seven different surfaces into a single orbitable study.
// The ring gives the composition a clear silhouette; the smaller specimens
// show how roughness and metalness change under the same light rig.
func heroMeshes() []scene.Node {
	return []scene.Node{
		scene.Mesh{ID: "gold-sphere", Geometry: scene.SphereGeometry{Radius: 0.88, Segments: 40}, Material: scene.StandardMaterial{Color: "#e2b46f", Roughness: 0.16, Metalness: 0.82, Clearcoat: 0.5}, Position: scene.Vec3(0, 1.7, 0.2), CastShadow: true},
		scene.Mesh{ID: "chrome-box", Geometry: scene.BoxGeometry{Width: 0.95, Height: 0.95, Depth: 0.95}, Material: scene.StandardMaterial{Color: "#dde6e5", Roughness: 0.28, Metalness: 0.9, Clearcoat: 0.55}, Position: scene.Vec3(-1.8, 0.95, -0.7), Rotation: scene.Rotate(0.22, 0.5, 0.12), Spin: scene.Rotate(0.0018, 0.0032, 0), CastShadow: true},
		scene.Mesh{ID: "matte-pyramid", Geometry: scene.PyramidGeometry{Width: 1.05, Height: 1.45, Depth: 1.05}, Material: scene.StandardMaterial{Color: "#748b87", Roughness: 0.86, Metalness: 0.02}, Position: scene.Vec3(1.75, 0.8, -0.7), Rotation: scene.Rotate(0, 0.35, 0), Spin: scene.Rotate(0, 0.0026, 0), CastShadow: true},
		scene.Mesh{ID: "dielectric-cylinder", Geometry: scene.CylinderGeometry{RadiusTop: 0.4, RadiusBottom: 0.5, Height: 1.25, Segments: 32}, Material: scene.StandardMaterial{Color: "#e5ddd1", Roughness: 0.18, Metalness: 0.04, Clearcoat: 0.75}, Position: scene.Vec3(-0.95, 2.85, -1.45), Rotation: scene.Rotate(0.18, 0, -0.25), Spin: scene.Rotate(0.0022, 0, 0.0014), CastShadow: true},
		scene.Mesh{ID: "aged-torus", Geometry: scene.TorusGeometry{Radius: 1.8, Tube: 0.1, RadialSegments: 24, TubularSegments: 80}, Material: scene.StandardMaterial{Color: "#b68a56", Roughness: 0.35, Metalness: 0.75, Clearcoat: 0.45}, Position: scene.Vec3(0, 1.7, 0), Rotation: scene.Rotate(0.35, 0.2, 0.48), Spin: scene.Rotate(0.0018, 0.0011, 0), CastShadow: true},
		scene.Mesh{ID: "rubber-sphere", Geometry: scene.SphereGeometry{Radius: 0.48, Segments: 32}, Material: scene.StandardMaterial{Color: "#30424a", Roughness: 0.9, Metalness: 0}, Position: scene.Vec3(2.25, 2.35, -1.1), CastShadow: true},
		scene.Mesh{ID: "stone-sphere", Geometry: scene.SphereGeometry{Radius: 0.55, Segments: 32}, Material: scene.StandardMaterial{Color: "#93918b", Roughness: 0.8, Metalness: 0.02}, Position: scene.Vec3(-2.35, 0.62, 0.6), CastShadow: true},
	}
}
