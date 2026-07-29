// Package docs provides the Blackglass Beacon showcase at /demos/beacon.
package docs

import (
	"math"

	"m31labs.dev/gosx/scene"
)

const (
	beaconNodeBudget           = 19
	beaconExpandedVertexBudget = 40000
	beaconMaxPixels            = scene.PostFXMaxPixels720p
)

// BlackglassBeaconProgram is an asset-free Scene3D composition with a fixed
// cinematic camera and no autonomous motion.
func BlackglassBeaconProgram() scene.Props {
	return scene.Props{
		Width:                 1280,
		Height:                720,
		Label:                 "Blackglass Beacon — Eclipse Protocol",
		AriaLabel:             "Blackglass Beacon brutalist tower with a cyan eclipsed lantern and warm core",
		Background:            "#05070b",
		Controls:              "orbit",
		AutoRotate:            scene.Bool(false),
		Responsive:            scene.Bool(true),
		FillHeight:            scene.Bool(true),
		PreferWebGPU:          scene.Bool(true),
		CanvasAlpha:           scene.Bool(false),
		UnsupportedMessage:    "Interactive 3D is unavailable in this browser. The page overlay describes the Eclipse Protocol.",
		ControlTarget:         scene.Vec3(0, 2.15, 0),
		ControlMinDistance:    6.5,
		ControlMaxDistance:    12,
		MaxFPS:                30,
		MaxDevicePixelRatio:   1.5,
		MaxPixels:             beaconMaxPixels,
		AdaptiveQuality:       scene.Bool(true),
		AdaptiveTargetFrameMS: 24,
		AdaptiveWarmupFrames:  12,
		AdaptivePostFX:        scene.Bool(true),
		Camera:                scene.PerspectiveCamera{Position: scene.Vec3(7.1, 4.75, 9.6), FOV: 42, Near: 0.1, Far: 40},
		Environment:           scene.Environment{AmbientColor: "#d7e7ed", AmbientIntensity: 0.08},
		PostFX: scene.PostFX{MaxPixels: scene.PostFXMaxPixels540p, Effects: []scene.PostEffect{
			scene.Bloom{Threshold: 0.78, Strength: 0.42, Radius: 9, Scale: 0.35},
			scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.04},
			scene.Vignette{Intensity: 0.52},
		}},
		Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixels512},
		Graph:   scene.NewGraph(beaconNodes()...),
	}
}

func beaconNodes() []scene.Node {
	blackglass := scene.StandardMaterial{Color: "#090c11", Roughness: 0.3, Metalness: 0.82, Clearcoat: 0.5}
	rough := scene.StandardMaterial{Color: "#11161c", Roughness: 0.62, Metalness: 0.55}
	cyan := scene.StandardMaterial{Color: "#30e8f0", Roughness: 0.18, Metalness: 0.56, Clearcoat: 0.9, Emissive: 0.62}
	warm := scene.StandardMaterial{Color: "#ff9b48", Roughness: 0.16, Metalness: 0.36, Clearcoat: 0.82, Emissive: 0.72}
	return []scene.Node{
		scene.DirectionalLight{ID: "beacon-key", Color: "#d9efff", Intensity: 1.35, Direction: scene.Vec3(0.55, -1, -0.7), CastShadow: true, ShadowBias: -0.001, ShadowSize: 512},
		scene.PointLight{ID: "beacon-cyan-fill", Color: "#30e8f0", Intensity: 1.45, Position: scene.Vec3(-3.2, 5.8, 2.4), Range: 16, Decay: 2},
		scene.PointLight{ID: "beacon-warm-core-light", Color: "#ff9b48", Intensity: 1.7, Position: scene.Vec3(0, 2.45, 0.18), Range: 10, Decay: 2},
		scene.HemisphereLight{ID: "beacon-hemi", SkyColor: "#27465a", GroundColor: "#030406", Intensity: 0.28},
		scene.Mesh{ID: "beacon-ground", Geometry: scene.PlaneGeometry{Width: 18, Height: 18}, Material: rough, Rotation: scene.Rotate(-math.Pi/2, 0, 0), ReceiveShadow: true},
		scene.Mesh{ID: "beacon-plinth-base", Geometry: scene.BoxGeometry{Width: 5.8, Height: 0.36, Depth: 5.8}, Material: blackglass, Position: scene.Vec3(0, 0.18, 0), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "beacon-plinth-step", Geometry: scene.BoxGeometry{Width: 4.55, Height: 0.3, Depth: 4.55}, Material: rough, Position: scene.Vec3(0, 0.5, 0), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "beacon-plinth-slab", Geometry: scene.BoxGeometry{Width: 3.35, Height: 0.26, Depth: 3.35}, Material: blackglass, Position: scene.Vec3(0, 0.78, 0), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "beacon-tower-foot", Geometry: scene.BoxGeometry{Width: 1.82, Height: 1.1, Depth: 1.82}, Material: rough, Position: scene.Vec3(0, 1.45, 0), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "beacon-tower-shaft", Geometry: scene.BoxGeometry{Width: 1.02, Height: 2.6, Depth: 1.02}, Material: blackglass, Position: scene.Vec3(0, 3.22, 0), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "beacon-tower-spine", Geometry: scene.BoxGeometry{Width: 0.25, Height: 3.15, Depth: 0.22}, Material: cyan, Position: scene.Vec3(-0.63, 3.4, 0.43), CastShadow: true},
		scene.Mesh{ID: "beacon-tower-fin-left", Geometry: scene.PyramidGeometry{Width: 0.82, Height: 1.7, Depth: 0.32}, Material: rough, Position: scene.Vec3(-0.88, 3.85, 0), Rotation: scene.Rotate(0, 0, -math.Pi/2), CastShadow: true},
		scene.Mesh{ID: "beacon-tower-fin-right", Geometry: scene.PyramidGeometry{Width: 0.82, Height: 1.7, Depth: 0.32}, Material: rough, Position: scene.Vec3(0.88, 3.85, 0), Rotation: scene.Rotate(0, 0, math.Pi/2), CastShadow: true},
		scene.Mesh{ID: "beacon-crown-deck", Geometry: scene.BoxGeometry{Width: 2.3, Height: 0.2, Depth: 2.3}, Material: blackglass, Position: scene.Vec3(0, 4.62, 0), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "beacon-cyan-crown", Geometry: scene.TorusGeometry{Radius: 0.82, Tube: 0.09, RadialSegments: 14, TubularSegments: 48}, Material: cyan, Position: scene.Vec3(0, 5.08, 0), Rotation: scene.Rotate(math.Pi/2, 0, 0), CastShadow: true},
		scene.Mesh{ID: "beacon-lantern-cyan", Geometry: scene.SphereGeometry{Radius: 0.48, Segments: 28}, Material: cyan, Position: scene.Vec3(0, 5.08, 0), CastShadow: true},
		scene.Mesh{ID: "beacon-eclipse-disc", Geometry: scene.SphereGeometry{Radius: 0.42, Segments: 28}, Material: blackglass, Position: scene.Vec3(0.17, 5.12, 0.38), CastShadow: true},
		scene.Mesh{ID: "beacon-warm-core", Geometry: scene.SphereGeometry{Radius: 0.28, Segments: 24}, Material: warm, Position: scene.Vec3(0, 2.36, 0.54), CastShadow: true},
		scene.Mesh{ID: "beacon-eclipse-beam", Geometry: scene.LinesGeometry{Points: []scene.Vector3{scene.Vec3(-5.3, 0.55, 2.1), scene.Vec3(0.1, 5.08, 0.05), scene.Vec3(5.1, 7.7, -2.4)}, Segments: [][2]int{{0, 1}, {1, 2}}, Width: 2.4}, Material: scene.LineBasicMaterial{MaterialStyle: scene.MaterialStyle{Color: "#65f6ff", BlendMode: scene.BlendAdditive, RenderPass: scene.RenderAdditive}, Width: 2.4}},
	}
}
