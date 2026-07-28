package docs

import (
	"math"

	"m31labs.dev/gosx/scene"
)

const (
	showreelNodeBudget = 12
	showreelMaxPixels  = scene.PostFXMaxPixels720p
)

// DemoShowreelProgram is the bounded Scene3D proof on the demos index. It is
// deterministic, asset-free, and intentionally has no autonomous motion so
// reduced-motion visitors receive the same composition without client logic.
func DemoShowreelProgram() scene.Props {
	return scene.Props{
		Width:                 960,
		Height:                620,
		Label:                 "GoSX Scene3D showreel",
		AriaLabel:             "Interactive orbital sculpture rendered by GoSX Scene3D",
		Background:            "#0b0b0d",
		Controls:              "orbit",
		AutoRotate:            scene.Bool(false),
		Responsive:            scene.Bool(true),
		FillHeight:            scene.Bool(true),
		PreferWebGPU:          scene.Bool(true),
		CanvasAlpha:           scene.Bool(false),
		UnsupportedMessage:    "Interactive 3D is unavailable in this browser. The demo links remain available below.",
		ControlTarget:         scene.Vec3(0, 1.05, 0),
		ControlMinDistance:    5.5,
		ControlMaxDistance:    11,
		MaxFPS:                30,
		MaxDevicePixelRatio:   1.5,
		MaxPixels:             showreelMaxPixels,
		AdaptiveQuality:       scene.Bool(true),
		AdaptiveTargetFrameMS: 24,
		AdaptiveWarmupFrames:  12,
		AdaptivePostFX:        scene.Bool(true),
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 2.8, 8.4),
			FOV:      48,
			Near:     0.1,
			Far:      40,
		},
		Environment: scene.Environment{
			AmbientColor:     "#f5f5ef",
			AmbientIntensity: 0.1,
		},
		PostFX: scene.PostFX{
			MaxPixels: scene.PostFXMaxPixels540p,
			Effects: []scene.PostEffect{
				scene.Bloom{Threshold: 0.82, Strength: 0.32, Radius: 8, Scale: 0.35},
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
			Color:      "#f5f5ef",
			Intensity:  1.4,
			Direction:  scene.Vec3(0.55, -1, -0.65),
			CastShadow: true,
			ShadowBias: -0.001,
			ShadowSize: 1024,
		},
		scene.PointLight{
			ID:        "showreel-accent",
			Color:     "#69e3c7",
			Intensity: 1.8,
			Position:  scene.Vec3(-3.2, 2.6, 1.8),
			Range:     14,
			Decay:     2,
		},
		scene.HemisphereLight{
			ID:          "showreel-hemi",
			SkyColor:    "#5fb4ff",
			GroundColor: "#0b0b0d",
			Intensity:   0.3,
		},
		scene.Mesh{
			ID:            "showreel-plinth",
			Geometry:      scene.CylinderGeometry{RadiusTop: 3.45, RadiusBottom: 3.65, Height: 0.18, Segments: 56},
			Material:      scene.StandardMaterial{Color: "#151519", Roughness: 0.2, Metalness: 0.62, Clearcoat: 0.72},
			Position:      scene.Vec3(0, 0, 0),
			ReceiveShadow: true,
		},
		scene.Mesh{
			ID:         "showreel-core",
			Geometry:   scene.SphereGeometry{Radius: 0.72, Segments: 36},
			Material:   scene.StandardMaterial{Color: "#69e3c7", Roughness: 0.14, Metalness: 0.58, Clearcoat: 0.86, Emissive: 0.42},
			Position:   scene.Vec3(0, 1.18, 0),
			CastShadow: true,
		},
		scene.Mesh{
			ID:         "showreel-orbit-a",
			Geometry:   scene.TorusGeometry{Radius: 1.48, Tube: 0.055, RadialSegments: 12, TubularSegments: 72},
			Material:   scene.StandardMaterial{Color: "#f5f5ef", Roughness: 0.2, Metalness: 0.9, Clearcoat: 0.72},
			Position:   scene.Vec3(0, 1.18, 0),
			Rotation:   scene.Rotate(math.Pi/2.7, 0.2, 0),
			CastShadow: true,
		},
		scene.Mesh{
			ID:       "showreel-orbit-b",
			Geometry: scene.TorusGeometry{Radius: 1.92, Tube: 0.04, RadialSegments: 10, TubularSegments: 80},
			Material: scene.StandardMaterial{Color: "#5fb4ff", Roughness: 0.28, Metalness: 0.82, Clearcoat: 0.66},
			Position: scene.Vec3(0, 1.18, 0),
			Rotation: scene.Rotate(-math.Pi/3.4, 0.42, math.Pi/4.8),
		},
		scene.Mesh{
			ID:         "showreel-satellite-box",
			Geometry:   scene.BoxGeometry{Width: 0.48, Height: 0.48, Depth: 0.48},
			Material:   scene.StandardMaterial{Color: "#c7c7c2", Roughness: 0.24, Metalness: 0.8, Clearcoat: 0.62},
			Position:   scene.Vec3(-1.86, 1.6, 0.28),
			Rotation:   scene.Rotate(0.42, 0.66, 0.18),
			CastShadow: true,
		},
		scene.Mesh{
			ID:         "showreel-satellite-pyramid",
			Geometry:   scene.PyramidGeometry{Width: 0.58, Height: 0.78, Depth: 0.58},
			Material:   scene.StandardMaterial{Color: "#69e3c7", Roughness: 0.3, Metalness: 0.3, Clearcoat: 0.54},
			Position:   scene.Vec3(1.62, 0.92, 0.82),
			Rotation:   scene.Rotate(0.08, -0.48, 0.12),
			CastShadow: true,
		},
		scene.Mesh{
			ID:         "showreel-satellite-sphere",
			Geometry:   scene.SphereGeometry{Radius: 0.3, Segments: 24},
			Material:   scene.StandardMaterial{Color: "#f5f5ef", Roughness: 0.09, Metalness: 0.94, Clearcoat: 0.8},
			Position:   scene.Vec3(0.88, 2.48, -0.38),
			CastShadow: true,
		},
	}
	if len(nodes) > showreelNodeBudget {
		panic("demo showreel exceeds its scene node budget")
	}
	return nodes
}
