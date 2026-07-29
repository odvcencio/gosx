// Package docs provides the Blackglass Coast showcase at /demos/beacon.
package docs

import (
	"math"

	"m31labs.dev/gosx/scene"
)

const (
	blackglassCoastNodeBudget           = 16
	blackglassCoastExpandedVertexBudget = 72000
	blackglassCoastMaxPixels            = scene.PostFXMaxPixels720p
)

// BlackglassBeaconProgram remains the route-compatible name for the original
// showcase. Its presentation is now Blackglass Coast: a Studio-authored cove
// with the framework WaterSystem at the centre of the runtime coordinate
// system, rather than an isolated wireframe-style tower.
func BlackglassBeaconProgram() scene.Props {
	contract := BlackglassCoastRuntimeContract()
	opening, _ := contract.Marker("opening-camera")
	target, _ := contract.Marker("cinematic-beacon")
	return scene.Props{
		Width: 1280, Height: 720,
		Label:      "Blackglass Coast — sunlit volcanic water world",
		AriaLabel:  "Blackglass Coast, a sunlit volcanic cove with a living water surface, basalt shelves, ruins, and a distant beacon",
		Background: "#7bb8cf", Controls: "orbit", AutoRotate: scene.Bool(false), Responsive: scene.Bool(true), FillHeight: scene.Bool(true),
		PreferWebGPU: scene.Bool(true), CanvasAlpha: scene.Bool(false),
		UnsupportedMessage: "Interactive 3D is unavailable in this browser. Blackglass Coast requires a supported GPU path.",
		ControlTarget:      contract.Local(target.Position), ControlMinDistance: 9, ControlMaxDistance: 42,
		MaxFPS: 60, MaxDevicePixelRatio: 1.5, MaxPixels: blackglassCoastMaxPixels,
		AdaptiveQuality: scene.Bool(true), AdaptiveTargetFrameMS: 16.7, AdaptiveWarmupFrames: 18, AdaptivePostFX: scene.Bool(true),
		Camera:      scene.PerspectiveCamera{Position: contract.Local(opening.Position), FOV: 48, Near: 0.1, Far: 150},
		Environment: scene.Environment{AmbientColor: "#c2e2ea", AmbientIntensity: 0.5},
		PostFX: scene.PostFX{MaxPixels: scene.PostFXMaxPixels540p, Effects: []scene.PostEffect{
			scene.Bloom{Threshold: 1.06, Strength: 0.24, Radius: 7, Scale: 0.35},
			scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.1},
			scene.Vignette{Intensity: 0.16},
		}},
		Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixels512},
		Graph:   scene.NewGraph(blackglassCoastNodes(contract)...),
	}
}

func blackglassCoastNodes(contract BlackglassCoastContract) []scene.Node {
	local := contract.Local
	sand := scene.StandardMaterial{Color: "#d9af70", Roughness: 0.9}
	basalt := scene.StandardMaterial{Color: "#1f2428", Roughness: 0.42, Metalness: 0.08, Clearcoat: 0.18}
	ruin := scene.StandardMaterial{Color: "#716450", Roughness: 0.76}
	blackglass := scene.StandardMaterial{Color: "#131b21", Roughness: 0.24, Metalness: 0.78, Clearcoat: 0.55}
	ember := scene.StandardMaterial{Color: "#ff9c4e", Roughness: 0.2, Metalness: 0.12, Emissive: 1.25}

	rocks := []scene.Vector3{
		local(scene.Vec3(-23, 1.2, 4)), local(scene.Vec3(-20, 2.1, 7)), local(scene.Vec3(-16, 0.9, 9)),
		local(scene.Vec3(-8, 1.3, 13)), local(scene.Vec3(0, 1.0, 10)), local(scene.Vec3(13, 1.6, 2)),
		local(scene.Vec3(15, 1.2, -3)), local(scene.Vec3(10, 1.0, -11)), local(scene.Vec3(-3, 1.1, -14)),
		local(scene.Vec3(-13, 1.2, -16)), local(scene.Vec3(-22, 1.0, -10)), local(scene.Vec3(-26, 0.8, -2)),
	}
	rockScales := []scene.Vector3{scene.Vec3(2.0, 1.4, 1.5), scene.Vec3(1.5, 2.2, 1.2), scene.Vec3(2.1, 1.0, 1.8), scene.Vec3(1.6, 1.5, 1.1), scene.Vec3(1.2, 1.1, 1.5), scene.Vec3(1.7, 1.3, 1.4), scene.Vec3(1.5, 1.8, 1.3), scene.Vec3(1.1, 1.4, 1.7), scene.Vec3(1.9, 1.0, 1.4), scene.Vec3(1.4, 1.7, 1.2), scene.Vec3(1.8, 1.2, 1.5), scene.Vec3(1.2, 1.1, 1.8)}
	rockRotations := make([]scene.Euler, len(rocks))
	for i := range rockRotations {
		rockRotations[i] = scene.Rotate(0.18*float64(i%3), 0.53*float64(i), 0.12*float64((i+1)%2))
	}

	return []scene.Node{
		scene.DirectionalLight{ID: "coast-sun", Color: "#fff0d0", Intensity: 2.1, Direction: scene.Vec3(-0.55, -0.82, -0.24), CastShadow: true, ShadowBias: -0.001, ShadowSize: 512},
		scene.HemisphereLight{ID: "coast-sky", SkyColor: "#b9d8e8", GroundColor: "#4a3629", Intensity: 0.48},
		scene.PointLight{ID: "beacon-fire", Color: "#ff8a48", Intensity: 2.6, Position: local(scene.Vec3(8, 7.5, -4)), Range: 26, Decay: 2},
		blackglassCoveWater(contract),
		scene.Mesh{ID: "coast-sand", Geometry: scene.PlaneGeometry{Width: 50, Height: 36}, Material: sand, Position: local(scene.Vec3(-5, -0.35, 5)), Rotation: scene.Rotate(-math.Pi/2, 0, 0), ReceiveShadow: true},
		scene.Mesh{ID: "coast-cliff-west", Geometry: scene.SphereGeometry{Radius: 1, Segments: 28}, Material: basalt, Position: local(scene.Vec3(-18, 4.1, -2)), Scale: scene.Vec3(8, 6, 5), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "coast-cliff-east", Geometry: scene.SphereGeometry{Radius: 1, Segments: 28}, Material: basalt, Position: local(scene.Vec3(12, 3.4, -8)), Scale: scene.Vec3(7, 5, 4), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "coast-cliff-overlook", Geometry: scene.SphereGeometry{Radius: 1, Segments: 28}, Material: basalt, Position: local(scene.Vec3(4, 5, 11)), Scale: scene.Vec3(9, 7, 4), CastShadow: true, ReceiveShadow: true},
		scene.InstancedMesh{ID: "coast-basalt-scatter", Count: len(rocks), Geometry: scene.SphereGeometry{Radius: 1, Segments: 16}, Material: basalt, Positions: rocks, Scales: rockScales, Rotations: rockRotations, CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "ruin-arch-left", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}, Material: ruin, Position: local(scene.Vec3(2.6, 1.8, 5.3)), Scale: scene.Vec3(0.8, 3.6, 0.8), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "ruin-arch-right", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}, Material: ruin, Position: local(scene.Vec3(6.4, 1.8, 5.3)), Scale: scene.Vec3(0.8, 3.6, 0.8), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "ruin-arch-lintel", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}, Material: ruin, Position: local(scene.Vec3(4.5, 4.7, 5.3)), Scale: scene.Vec3(2.7, 0.65, 0.8), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "beacon-plinth", Geometry: scene.CylinderGeometry{RadiusTop: 2.2, RadiusBottom: 2.7, Height: 1.6, Segments: 32}, Material: ruin, Position: local(scene.Vec3(8, 0.8, -4)), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "blackglass-beacon", Geometry: scene.CylinderGeometry{RadiusTop: 0.55, RadiusBottom: 1.25, Height: 6.4, Segments: 24}, Material: blackglass, Position: local(scene.Vec3(8, 4.7, -4)), CastShadow: true, ReceiveShadow: true},
		scene.Mesh{ID: "beacon-lens", Geometry: scene.SphereGeometry{Radius: 0.72, Segments: 24}, Material: ember, Position: local(scene.Vec3(8, 8.1, -4)), CastShadow: true},
		scene.Mesh{ID: "arrival-marker", Geometry: scene.SphereGeometry{Radius: 0.46, Segments: 20}, Material: scene.StandardMaterial{Color: "#e7bd6b", Roughness: 0.4, Metalness: 0.3}, Position: local(scene.Vec3(-1.8, 0.2, 9.2)), CastShadow: true},
	}
}

func blackglassCoveWater(contract BlackglassCoastContract) scene.WaterSystem {
	zone := contract.Water
	return scene.WaterSystem{
		ID: zone.ID, InteractionProfile: zone.RuntimeProfile, InteractionTarget: "blackglass-coast", InteractionObject: "cove-drifter",
		Resolution: 128, SurfaceResolution: 96, PoolShape: "Box", PoolWidth: zone.Size.X, PoolHeight: zone.Size.Y, PoolLength: zone.Size.Z,
		CornerRadius: 1.2, WaveSpeed: 1.15, Damping: 0.995, NormalScale: 1.0, SeedDrops: 18, DropRadius: 0.035, DropStrength: 0.012,
		ShallowColor: "#289ab2", DeepColor: "#06283e", AboveWaterColor: scene.Vec3(0.18, 0.78, 0.98),
		CausticsResolution: 256, ObjectTextureResolutionMode: "viewport", ObjectTexturePixelBudget: 786432, ObjectShadowResolution: 256,
		Caustics: true, Reflection: true, Refraction: true, FollowCamera: false, LightDirection: scene.Vec3(-0.55, 0.82, 0.24),
		ActiveObject: "cove-drifter", ObjectKind: "sphere", ObjectX: -2.8, ObjectY: -0.2, ObjectZ: 1.6, ObjectRadius: 0.46,
		ObjectDriftX: zone.Current.X, ObjectDriftZ: zone.Current.Z, ObjectDisplacementScale: 1,
		ComputeBackend: "builtin", MaterialBackend: "builtin",
	}
}
