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
		ControlTarget:         scene.Vec3(0, 1.05, 0),
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
			Position: scene.Vec3(0, 2.7, 7.5),
			FOV:      48,
			Near:     0.1,
			Far:      40,
		},
		Environment: scene.Environment{
			AmbientColor:     "#f5e9d9",
			AmbientIntensity: 0.18,
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
			Color:      "#f5e9d9",
			Intensity:  1.4,
			Direction:  scene.Vec3(0.55, -1, -0.65),
			CastShadow: true,
			ShadowBias: -0.001,
			ShadowSize: 1024,
		},
		scene.PointLight{
			ID:        "showreel-accent",
			Color:     "#f6ba77",
			Intensity: 1.5,
			Position:  scene.Vec3(-3.2, 2.6, 1.8),
			Range:     14,
			Decay:     2,
		},
		scene.HemisphereLight{
			ID:          "showreel-hemi",
			SkyColor:    "#819ba2",
			GroundColor: "#0c1720",
			Intensity:   0.3,
		},
		scene.Mesh{
			ID:            "showreel-plinth",
			Geometry:      scene.CylinderGeometry{RadiusTop: 3.45, RadiusBottom: 3.65, Height: 0.18, Segments: 56},
			Material:      scene.StandardMaterial{Color: "#26333b", Roughness: 0.3, Metalness: 0.48, Clearcoat: 0.56},
			Position:      scene.Vec3(0, 0, 0),
			ReceiveShadow: true,
		},
		scene.Mesh{
			ID:         "showreel-core",
			Geometry:   scene.SphereGeometry{Radius: 0.72, Segments: 36},
			Material:   scene.StandardMaterial{Color: "#f6ba77", Roughness: 0.19, Metalness: 0.52, Clearcoat: 0.8, Emissive: 0.3},
			Position:   scene.Vec3(0, 1.18, 0),
			CastShadow: true,
		},
		scene.Mesh{
			ID:         "showreel-orbit-a",
			Geometry:   scene.TorusGeometry{Radius: 1.48, Tube: 0.055, RadialSegments: 12, TubularSegments: 72},
			Material:   scene.StandardMaterial{Color: "#f5e9d9", Roughness: 0.24, Metalness: 0.82, Clearcoat: 0.65},
			Position:   scene.Vec3(0, 1.18, 0),
			Rotation:   scene.Rotate(math.Pi/2.7, 0.2, 0),
			CastShadow: true,
		},
		scene.Mesh{
			ID:       "showreel-orbit-b",
			Geometry: scene.TorusGeometry{Radius: 1.92, Tube: 0.04, RadialSegments: 10, TubularSegments: 80},
			Material: scene.StandardMaterial{Color: "#a4aeb1", Roughness: 0.31, Metalness: 0.72, Clearcoat: 0.58},
			Position: scene.Vec3(0, 1.18, 0),
			Rotation: scene.Rotate(-math.Pi/3.4, 0.42, math.Pi/4.8),
		},
		scene.Mesh{
			ID:         "showreel-satellite-box",
			Geometry:   scene.BoxGeometry{Width: 0.48, Height: 0.48, Depth: 0.48},
			Material:   scene.StandardMaterial{Color: "#c8c7bc", Roughness: 0.3, Metalness: 0.7, Clearcoat: 0.55},
			Position:   scene.Vec3(-1.86, 1.6, 0.28),
			Rotation:   scene.Rotate(0.42, 0.66, 0.18),
			CastShadow: true,
		},
		scene.Mesh{
			ID:         "showreel-satellite-pyramid",
			Geometry:   scene.PyramidGeometry{Width: 0.58, Height: 0.78, Depth: 0.58},
			Material:   scene.StandardMaterial{Color: "#f6ba77", Roughness: 0.34, Metalness: 0.3, Clearcoat: 0.5},
			Position:   scene.Vec3(1.62, 0.92, 0.82),
			Rotation:   scene.Rotate(0.08, -0.48, 0.12),
			CastShadow: true,
		},
		scene.Mesh{
			ID:         "showreel-satellite-sphere",
			Geometry:   scene.SphereGeometry{Radius: 0.3, Segments: 24},
			Material:   scene.StandardMaterial{Color: "#f5e9d9", Roughness: 0.12, Metalness: 0.88, Clearcoat: 0.75},
			Position:   scene.Vec3(0.88, 2.48, -0.38),
			CastShadow: true,
		},
	}
	if len(nodes) > showreelNodeBudget {
		panic("demo showreel exceeds its scene node budget")
	}
	return nodes
}
