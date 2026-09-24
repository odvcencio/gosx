// Package docs provides the Blackglass Coast showcase at /demos/beacon.
package docs

import (
	"encoding/json"

	waterdemo "m31labs.dev/gosx/examples/gosx-docs/app/demos/water"
	"m31labs.dev/gosx/scene"
)

const (
	blackglassCoastNodeBudget           = 14
	blackglassCoastExpandedVertexBudget = 72000
	blackglassCoastMaxPixels            = scene.PostFXMaxPixels720p
	// This count stays below the WebGL2 CPU particle limit of 10,000.
	blackglassEmberPlumeCount = 320
)

// BlackglassBeaconProgram keeps the original route and test entry point.
func BlackglassBeaconProgram() scene.Props {
	return BlackglassCoastProgram("overlook", "high-sun")
}

// BlackglassCoastProgram renders one camera station in one light period.
// Both choices are server-authored and can be linked without page JavaScript.
func BlackglassCoastProgram(viewID, periodID string) scene.Props {
	contract := BlackglassCoastRuntimeContract()
	period := blackglassPeriodFor(periodID)
	camera, target := blackglassViewPose(contract, viewID)
	return scene.Props{
		Width: 1280, Height: 720,
		Label:      "Blackglass Coast — " + blackglassViewName(viewID) + " at " + period.Name,
		AriaLabel:  "Blackglass Coast. Explore the cove, arrival beach, ruin arch, and beacon terrace.",
		Background: period.Horizon, Controls: scene.ControlOrbit, AutoRotate: scene.Bool(false), Responsive: scene.Bool(true), FillHeight: scene.Bool(true),
		PreferWebGPU: scene.Bool(true), CanvasAlpha: scene.Bool(false),
		UnsupportedMessage: "Interactive 3D is unavailable in this browser. Blackglass Coast requires a supported GPU path.", Stats: scene.Bool(true),
		ControlTarget: target, ControlMinDistance: 6, ControlMaxDistance: 48,
		MaxFPS: 60, MaxDevicePixelRatio: 1.5, MaxPixels: blackglassCoastMaxPixels,
		AdaptiveQuality: scene.Bool(true), AdaptiveTargetFrameMS: 16.7, AdaptiveWarmupFrames: 18, AdaptivePostFX: scene.Bool(true),
		Camera: scene.PerspectiveCamera{Position: camera, FOV: 50, Near: 0.1, Far: 150},
		Environment: scene.Environment{
			AmbientColor: period.Ambient, AmbientIntensity: period.AmbientPower,
			IBL: blackglassPeriodIBL(period.ID), EnvironmentMap: "/env/blackglass/" + period.ID + ".hdr", EnvIntensity: 0.6,
			Sky:      &scene.Sky{Mode: "gradient", TopColor: period.Top, HorizonColor: period.Horizon, BottomColor: period.Bottom},
			FogColor: period.Horizon, FogDensity: 0.004,
		},
		PostFX: scene.PostFX{MaxPixels: scene.PostFXMaxPixels540p, Effects: []scene.PostEffect{
			scene.Bloom{Threshold: 1.06, Strength: 0.24, Radius: 7, Scale: 0.35},
			scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.1},
			scene.Vignette{Intensity: 0.12},
			scene.FXAA{},
		}},
		Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixels512},
		Graph:   scene.NewGraph(blackglassCoastNodes(contract, period)...),
	}
}

func blackglassCoastNodes(contract BlackglassCoastContract, period blackglassPeriod) []scene.Node {
	local := contract.Local
	nodes := []scene.Node{
		scene.DirectionalLight{ID: "coast-sun", Color: period.Sun, Intensity: period.SunStrength, Direction: scene.Vec3(-0.55, -0.82, -0.24), CastShadow: true, ShadowBias: -0.001, ShadowSize: 512},
		scene.HemisphereLight{ID: "coast-sky", SkyColor: period.Horizon, GroundColor: "#4a3629", Intensity: 0.48},
		scene.PointLight{ID: "beacon-fire", Color: "#ff8a48", Intensity: period.BeaconPower, Position: local(scene.Vec3(8, 7.5, -4)), Range: 26, Decay: 2},
		scene.SpotLight{ID: "beacon-terrace-light", Color: "#ffb36c", Intensity: 1.5, Position: local(scene.Vec3(8, 8.3, -4)), Direction: scene.Vec3(0, -1, 0), Angle: 0.88, Penumbra: 0.45, Range: 14, Decay: 2, CastShadow: true, ShadowBias: -0.0008, ShadowSize: 512},
		blackglassCoveWater(contract, period),
		blackglassModel("shore", contract, 42),
		blackglassModel("basalt", contract, 40),
		blackglassModel("ruins", contract, 12),
		blackglassModel("beacon", contract, 12),
		scene.Mesh{ID: "beacon-lens", Geometry: scene.SphereGeometry{Radius: 0.72, Segments: 24}, Material: blackglassBeaconLensMaterial(contract), Position: local(scene.Vec3(8, 8.1, -4)), CastShadow: false},
		blackglassBeaconEmberPlume(contract),
	}
	return append(nodes, blackglassArtifacts(contract, period)...)
}

func blackglassBeaconEmberPlume(contract BlackglassCoastContract) scene.ComputeParticles {
	local := contract.Local
	return scene.ComputeParticles{
		ID: "beacon-ember-plume", Count: blackglassEmberPlumeCount,
		Emitter: scene.ParticleEmitter{Kind: "point", Position: local(scene.Vec3(8, 8.1, -4)), Rate: 30, Lifetime: 3.2, Scatter: 0.12},
		Forces: []scene.ParticleForce{
			{Kind: "gravity", Strength: 0.3, Direction: scene.Vec3(0, 1, 0)},
			{Kind: "turbulence", Strength: 0.18, Frequency: 1.6},
			{Kind: "wind", Strength: 0.1, Direction: scene.Vec3(0.4, 0, 0.2)},
		},
		Material: scene.ParticleMaterial{
			Color: "#ffcf8a", ColorEnd: "#ff9c4e", Style: scene.PointStyleGlow,
			Size: 0.05, SizeEnd: 0.015, Opacity: 0.85, OpacityEnd: 0,
			BlendMode: scene.BlendAdditive, Attenuation: true,
		},
		Bounds: 7,
	}
}

func blackglassCoveWater(contract BlackglassCoastContract, period blackglassPeriod) scene.WaterSystem {
	zone := contract.Water
	// The WebGL2 water renderer consumes the same Selena-compiled GLES passes
	// as the water demo. Without them it rejects the cove, even with WebGL2.
	shaderData, err := waterdemo.WaterDemoData()
	if err != nil {
		panic("compile Blackglass Coast water shaders: " + err.Error())
	}
	shader := func(key string) string {
		value, ok := shaderData[key].(string)
		if !ok || value == "" {
			panic("missing Blackglass Coast water shader: " + key)
		}
		return value
	}
	descriptors, ok := shaderData["waterShaderDescriptors"].(map[string]json.RawMessage)
	if !ok || len(descriptors) == 0 {
		panic("missing Blackglass Coast water shader descriptors")
	}
	return scene.WaterSystem{
		ID: zone.ID, InteractionProfile: zone.RuntimeProfile, InteractionTarget: "blackglass-coast", InteractionObject: "cove-drifter",
		// WaterSystem takes horizontal half extents; the Studio zone stores full
		// size. The world zone's height is a buoyancy volume, not a visible wall.
		Resolution: 128, SurfaceResolution: 96, PoolShape: "Box", PoolWidth: zone.Size.X / 2, PoolHeight: 0.35, PoolLength: zone.Size.Z / 2, RenderPool: scene.Bool(false),
		CornerRadius: 1.2, WaveSpeed: 0.75, Damping: 0.995, NormalScale: 0.2, SeedDrops: 8, DropRadius: 0.035, DropStrength: 0.006,
		TileTexture: "/water/tiles.jpg", CubeMap: "/water/",
		ShallowColor: period.ShallowWater, DeepColor: period.DeepWater, AboveWaterColor: scene.Vec3(0.18, 0.78, 0.98),
		CausticsResolution: 256, ObjectTextureResolutionMode: "viewport", ObjectTexturePixelBudget: 786432, ObjectShadowResolution: 256,
		Caustics: true, Reflection: true, Refraction: true, FollowCamera: false, LightDirection: scene.Vec3(-0.55, 0.82, 0.24),
		ActiveObject: "none", ObjectKind: "none", ObjectDriftX: zone.Current.X, ObjectDriftZ: zone.Current.Z,
		ComputeBackend: "builtin", MaterialBackend: "builtin",
		SeedSelenaWGSL: shader("waterSeedSelenaWGSL"), DropSelenaWGSL: shader("waterDropSelenaWGSL"),
		DisplacementSelenaWGSL: shader("waterDisplacementSelenaWGSL"), SimulationSelenaWGSL: shader("waterSimulationSelenaWGSL"), NormalSelenaWGSL: shader("waterNormalSelenaWGSL"),
		PoolSelenaWGSL: shader("waterPoolSelenaWGSL"), SurfaceSelenaWGSL: shader("waterSurfaceSelenaWGSL"), SurfaceBelowSelenaWGSL: shader("waterSurfaceBelowSelenaWGSL"),
		CausticsSelenaWGSL: shader("waterCausticsSelenaWGSL"), ObjectShadowSelenaWGSL: shader("waterObjectShadowSelenaWGSL"),
		CompoundShadowSelenaWGSL: shader("waterCompoundShadowSelenaWGSL"), ObjectMeshShadowSelenaWGSL: shader("waterObjectMeshShadowSelenaWGSL"),
		SeedVertexGLES: shader("waterSeedVertexGLES"), SeedFragmentGLES: shader("waterSeedFragmentGLES"),
		DropVertexGLES: shader("waterDropVertexGLES"), DropFragmentGLES: shader("waterDropFragmentGLES"),
		DisplacementVertexGLES: shader("waterDisplacementVertexGLES"), DisplacementFragmentGLES: shader("waterDisplacementFragmentGLES"),
		SimulationVertexGLES: shader("waterSimulationVertexGLES"), SimulationFragmentGLES: shader("waterSimulationFragmentGLES"),
		NormalVertexGLES: shader("waterNormalVertexGLES"), NormalFragmentGLES: shader("waterNormalFragmentGLES"),
		PoolVertexGLES: shader("waterPoolVertexGLES"), PoolFragmentGLES: shader("waterPoolFragmentGLES"),
		SurfaceVertexGLES: shader("waterSurfaceVertexGLES"), SurfaceFragmentGLES: shader("waterSurfaceFragmentGLES"),
		SurfaceBelowVertexGLES: shader("waterSurfaceBelowVertexGLES"), SurfaceBelowFragmentGLES: shader("waterSurfaceBelowFragmentGLES"),
		CausticsVertexGLES: shader("waterCausticsVertexGLES"), CausticsFragmentGLES: shader("waterCausticsFragmentGLES"),
		ObjectShadowVertexGLES: shader("waterObjectShadowVertexGLES"), ObjectShadowFragmentGLES: shader("waterObjectShadowFragmentGLES"),
		ObjectMaterialVertexGLES: shader("waterObjectPassVertexGLES"), ObjectMaterialFragmentGLES: shader("waterObjectPassFragmentGLES"),
		ShaderDescriptors: descriptors,
	}
}
