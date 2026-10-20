package docs

import (
	"math"

	"m31labs.dev/gosx/scene"
)

const (
	showreelNodeBudget = 12
	showreelMaxPixels  = scene.PostFXMaxPixels540p
)

// DemoShowreelProgram is the bounded Scene3D proof in the demo studio. It is
// deterministic, asset-free, and intentionally has no autonomous motion so
// reduced-motion visitors receive the same composition without client logic.
func DemoShowreelProgram() scene.Props {
	return scene.Props{
		Width:                 960,
		Height:                620,
		Label:                 "GoSX Scene3D showreel",
		AriaLabel:             "Interactive orbital sculpture rendered by GoSX Scene3D",
		Background:            "#0c1720",
		Controls:              "orbit",
		AutoRotate:            scene.Bool(false),
		Responsive:            scene.Bool(true),
		FillHeight:            scene.Bool(true),
		PreferWebGPU:          scene.Bool(true),
		CanvasAlpha:           scene.Bool(false),
		DeferPostFX:           scene.Bool(true),
		DeferPostFXDelayMS:    1800,
		UnsupportedMessage:    "Interactive 3D is unavailable in this browser. The demo links remain available below.",
		ControlTarget:         scene.Vec3(0, 1.2, 0),
		ControlMinDistance:    4.8,
		ControlMaxDistance:    11,
		MaxFPS:                30,
		MaxDevicePixelRatio:   1.5,
		MaxPixels:             showreelMaxPixels,
		AdaptiveQuality:       scene.Bool(true),
		AdaptiveTargetFrameMS: 24,
		AdaptiveWarmupFrames:  12,
		AdaptivePostFX:        scene.Bool(true),
		Camera: scene.PerspectiveCamera{
			Position:    scene.Vec3(0, 2.55, 7.2),
			FOV:         46,
			PortraitFOV: 68,
			Near:        0.1,
			Far:         40,
		},
		Environment: scene.Environment{
			AmbientColor:     "#d6e2de",
			AmbientIntensity: 0.34,
			Sky:              &scene.Sky{Mode: "gradient", TopColor: "#0c1720", HorizonColor: "#263d48", BottomColor: "#304750"},
		},
		PostFX: scene.PostFX{
			MaxPixels: scene.PostFXMaxPixels540p,
			Effects: []scene.PostEffect{
				scene.Bloom{Threshold: 0.82, Strength: 0.22, Radius: 8, Scale: 0.35},
				scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.08},
				scene.Vignette{Intensity: 0.42},
			},
		},
		Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixels512},
		Graph:   scene.NewGraph(showreelNodes()...),
	}
}

func showreelNodes() []scene.Node {
	nodes := []scene.Node{
		scene.DirectionalLight{
			ID:         "showreel-key",
			Color:      "#fff0d4",
			Intensity:  1.8,
			Direction:  scene.Vec3(0.55, -1, -0.65),
			CastShadow: true,
			ShadowBias: -0.001,
			ShadowSize: 1024,
		},
		scene.PointLight{
			ID:        "showreel-accent",
			Color:     "#f6ba77",
			Intensity: 1.9,
			Position:  scene.Vec3(-3.2, 2.6, 1.8),
			Range:     14,
			Decay:     2,
		},
		scene.HemisphereLight{
			ID:          "showreel-hemi",
			SkyColor:    "#b7d2d2",
			GroundColor: "#52636a",
			Intensity:   0.5,
		},
		scene.Mesh{
			ID:            "showreel-plinth",
			Geometry:      scene.CylinderGeometry{RadiusTop: 2.65, RadiusBottom: 2.8, Height: 0.24, Segments: 64},
			Material:      scene.StandardMaterial{Color: "#4e656b", Roughness: 0.58, Metalness: 0.2, Clearcoat: 0.22},
			Position:      scene.Vec3(0, -0.26, 0),
			ReceiveShadow: true,
		},
		scene.Mesh{
			ID:            "showreel-plinth-top",
			Geometry:      scene.CylinderGeometry{RadiusTop: 2.38, RadiusBottom: 2.5, Height: 0.08, Segments: 64},
			Material:      scene.StandardMaterial{Color: "#293e47", Roughness: 0.48, Metalness: 0.34, Clearcoat: 0.35},
			Position:      scene.Vec3(0, -0.1, 0),
			ReceiveShadow: true,
		},
		scene.Mesh{
			ID:       "showreel-plinth-inlay",
			Geometry: scene.TorusGeometry{Radius: 2.27, Tube: 0.025, RadialSegments: 96, TubularSegments: 8},
			Material: scene.StandardMaterial{Color: "#f6ba77", Roughness: 0.3, Metalness: 0.7, Clearcoat: 0.45},
			Position: scene.Vec3(0, -0.045, 0),
		},
		scene.Mesh{
			ID:         "showreel-core",
			Geometry:   scene.SphereGeometry{Radius: 0.68, Segments: 40},
			Material:   scene.StandardMaterial{Color: "#e9d6b2", Roughness: 0.3, Metalness: 0.16, Clearcoat: 0.72, Emissive: 0.07},
			Position:   scene.Vec3(0, 1.52, 0),
			CastShadow: true,
		},
		scene.Mesh{
			ID:         "showreel-orbit-a",
			Geometry:   scene.TorusGeometry{Radius: 1.33, Tube: 0.075, RadialSegments: 96, TubularSegments: 16},
			Material:   scene.StandardMaterial{Color: "#f6ba77", Roughness: 0.27, Metalness: 0.68, Clearcoat: 0.62},
			Position:   scene.Vec3(0, 1.52, 0),
			Rotation:   scene.Rotate(math.Pi/2.7, 0.2, 0),
			CastShadow: true,
		},
		scene.Mesh{
			ID:       "showreel-orbit-b",
			Geometry: scene.TorusGeometry{Radius: 1.69, Tube: 0.055, RadialSegments: 96, TubularSegments: 12},
			Material: scene.StandardMaterial{Color: "#c9d8d4", Roughness: 0.3, Metalness: 0.56, Clearcoat: 0.55},
			Position: scene.Vec3(0, 1.72, 0),
			Rotation: scene.Rotate(-math.Pi/3.4, 0.42, math.Pi/4.8),
		},
		scene.Mesh{
			ID:         "showreel-satellite-one",
			Geometry:   scene.SphereGeometry{Radius: 0.24, Segments: 28},
			Material:   scene.StandardMaterial{Color: "#f4c383", Roughness: 0.22, Metalness: 0.5, Clearcoat: 0.65},
			Position:   scene.Vec3(-1.57, 2.28, 0.28),
			CastShadow: true,
		},
		scene.Mesh{
			ID:         "showreel-satellite-two",
			Geometry:   scene.SphereGeometry{Radius: 0.18, Segments: 24},
			Material:   scene.StandardMaterial{Color: "#cddbd5", Roughness: 0.26, Metalness: 0.38, Clearcoat: 0.58},
			Position:   scene.Vec3(1.58, 0.82, 0.54),
			CastShadow: true,
		},
		scene.Mesh{
			ID:         "showreel-satellite-three",
			Geometry:   scene.SphereGeometry{Radius: 0.14, Segments: 24},
			Material:   scene.StandardMaterial{Color: "#f4c383", Roughness: 0.24, Metalness: 0.45, Clearcoat: 0.65},
			Position:   scene.Vec3(0.48, 3.12, -0.38),
			CastShadow: true,
		},
	}
	if len(nodes) > showreelNodeBudget {
		panic("demo showreel exceeds its scene node budget")
	}
	return nodes
}
