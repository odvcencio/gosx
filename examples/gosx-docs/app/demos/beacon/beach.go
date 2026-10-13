package docs

import (
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"sync"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

// Blackglass Beach: a black volcanic-sand beach, open ocean to the horizon,
// basalt sea stacks and one obsidian monolith, lit by an analytic sky. Every
// asset comes from beachgen, and the IBL is baked from the same sky the
// renderer draws.

const blackglassBeachModelRoot = "/models/blackglass/"

// Raster assets are cached longer than models. Fingerprint changed content
// so returning visitors get the new preview and matching ocean depth field.
const blackglassBeachPosterSrc = "/demos/posters/beacon.webp?v=03f3c71ad727"
const blackglassBeachHeightSrc = blackglassBeachModelRoot + "beach-v2-height.png?v=df420d015fd8"

type blackglassBeachPeriod struct {
	ID, Name string
	SunColor string
	SunPower float64
	Exposure float32
}

func blackglassBeachPeriodFor(raw string) blackglassBeachPeriod {
	switch raw {
	case beachgen.PeriodBlue:
		return blackglassBeachPeriod{beachgen.PeriodBlue, "Blue hour", "#9fb4ff", 0.6, 0.95}
	case beachgen.PeriodNoon:
		return blackglassBeachPeriod{beachgen.PeriodNoon, "Midday", "#fff4e6", 9, 0.5}
	default:
		return blackglassBeachPeriod{beachgen.PeriodGolden, "Golden hour", "#ffc58d", 6.4, 0.68}
	}
}

type blackglassBeachView struct {
	ID, Name         string
	Position, Target scene.Vector3
}

func blackglassBeachViewFor(raw string) blackglassBeachView {
	switch raw {
	case "beacon":
		p := scene.Vec3(beaconX+4, beachgen.TerrainHeight(beaconX+4, beaconZ+4, beachgen.Seed)+1.7, beaconZ+4)
		return blackglassBeachView{"beacon", "The lighthouse", p, scene.Vec3(beaconX, p.Y+4, beaconZ)}
	case "hollow":
		p := scene.Vec3(grottoFrontX+3, beachgen.TerrainHeight(grottoFrontX+3, grottoZ, beachgen.Seed)+1.7, grottoZ)
		return blackglassBeachView{"hollow", "The sun hollow", p, scene.Vec3(grottoBackX, p.Y, grottoZ)}
	case "pools":
		return blackglassBeachView{"pools", "The tide pools", scene.Vec3(10.8, 3.6, 10.6), scene.Vec3(11.1, .5, 5.3)}
	case "glass":
		return blackglassBeachView{"glass", "The glass", scene.Vec3(-10.8, beachgen.TerrainHeight(-10.8, 8.8, beachgen.Seed)+1.7, 8.8), scene.Vec3(-6.2, 1.7, 2.6)}
	case "ship":
		// On the jetty deck beside the clipper's helm: press E to take it.
		return blackglassBeachView{"ship", "At the helm", scene.Vec3(21, 4.2, -33.5), scene.Vec3(17, 5, -60)}
	case "cliff":
		return blackglassBeachView{"cliff", "From the cliff", scene.Vec3(-33, beachgen.TerrainHeight(-33, 6, beachgen.Seed)+2.5, 6), scene.Vec3(-8, 1, -25)}
	default:
		return blackglassBeachView{"shore", "The shore", scene.Vec3(3.2, beachgen.TerrainHeight(3.2, 17, beachgen.Seed)+1.7, 17), scene.Vec3(-18.8, .6, -28)}
	}
}

var (
	blackglassBeachIBLOnce sync.Once
	blackglassBeachIBL     map[string]scene.EnvironmentIBL
)

//go:embed ibl-beach/*.json
var blackglassBeachIBLFiles embed.FS

func blackglassBeachPeriodIBL(period string) scene.EnvironmentIBL {
	blackglassBeachIBLOnce.Do(func() {
		blackglassBeachIBL = make(map[string]scene.EnvironmentIBL, len(beachgen.Periods))
		for _, id := range beachgen.Periods {
			data, err := blackglassBeachIBLFiles.ReadFile("ibl-beach/" + id + ".json")
			if err != nil {
				panic("read Blackglass Beach IBL: " + err.Error())
			}
			var value scene.EnvironmentIBL
			if err := json.Unmarshal(data, &value); err != nil {
				panic("decode Blackglass Beach IBL: " + err.Error())
			}
			blackglassBeachIBL[id] = value
		}
	})
	return blackglassBeachIBL[period]
}

// blackglassBeachHorizon is the sky's mean horizon color as sRGB hex, used
// for distance fog so land haze matches the sea's horizon fade.
func blackglassBeachHorizon(sky scene.Sky, exposure float32) string {
	sun := sky.SunDirection
	flat := math.Hypot(sun.X, sun.Z)
	if flat < 1e-6 {
		flat, sun.X, sun.Z = 1, 0, -1
	}
	toward := scene.Vec3(sun.X/flat, 0.03, sun.Z/flat)
	away := scene.Vec3(-sun.X/flat, 0.03, -sun.Z/flat)
	side := scene.Vec3(sun.Z/flat, 0.03, -sun.X/flat)
	var c [3]float64
	for _, d := range []scene.Vector3{toward, away, side, scene.Vec3(-side.X, 0.03, -side.Z)} {
		r, g, b := sky.PhysicalRadiance(d, false)
		c[0] += r / 4
		c[1] += g / 4
		c[2] += b / 4
	}
	enc := func(x float64) int {
		x *= float64(exposure)
		v := (x * (2.51*x + 0.03)) / (x*(2.43*x+0.59) + 0.14)
		v = math.Max(0, math.Min(1, v))
		if v <= 0.0031308 {
			v *= 12.92
		} else {
			v = 1.055*math.Pow(v, 1/2.4) - 0.055
		}
		return int(math.Round(v * 255))
	}
	return fmt.Sprintf("#%02x%02x%02x", enc(c[0]), enc(c[1]), enc(c[2]))
}

// BlackglassBeachProgram renders one camera view in one light period.
func BlackglassBeachProgram(viewID, periodID string) scene.Props {
	period := blackglassBeachPeriodFor(periodID)
	view := blackglassBeachViewFor(viewID)
	sky := beachgen.PeriodSky(period.ID)
	sky.Clouds = &scene.SkyClouds{Coverage: 0.50, Altitude: 1100, Scale: 4800, Opacity: 0.72, Direction: 20}
	sun := sky.SunDirection
	horizon := blackglassBeachHorizon(sky, period.Exposure)
	program := scene.Props{
		Width: 1280, Height: 720,
		Label:       "Blackglass Beach — " + view.Name + " at " + period.Name,
		AriaLabel:   "A black-sand coast at " + period.Name + ": basalt cliffs, sea stacks, footprints to black glass, tide pools with marine life, a wreck and a lighthouse. A violet handheld rests beside the pools; a clipper waits at the jetty.",
		ControlZoom: scene.Bool(true), ControlTarget: view.Target, ControlMinDistance: 2, ControlMaxDistance: 240,
		Background: horizon, Controls: blackglassBeachControls(view.ID), Walk: blackglassBeachWalk(), Vessel: blackglassBeachVessel(), PointerLock: scene.Bool(true),
		AutoRotate: scene.Bool(false), Responsive: scene.Bool(true), FillHeight: scene.Bool(true),
		PreferWebGPU: scene.Bool(true), CanvasAlpha: scene.Bool(false), Stats: scene.Bool(false),
		UnsupportedMessage: "Interactive 3D is unavailable in this browser.",
		MaxFPS:             120, MaxDevicePixelRatio: 2, MaxPixels: scene.PostFXMaxPixels1440p,
		RenderBeforeModels: scene.Bool(true),
		AdaptiveQuality:    scene.Bool(true), AdaptiveTargetFrameMS: 16.7, AdaptiveWarmupFrames: 24, AdaptivePostFX: scene.Bool(true),
		Camera: scene.PerspectiveCamera{Position: view.Position, Rotation: blackglassBeachLookRotation(view.Position, view.Target), FOV: 50, PortraitFOV: 70, Near: 0.1, Far: 900},
		Environment: scene.Environment{
			IBL: blackglassBeachPeriodIBL(period.ID), EnvIntensity: 0.85,
			Sky:      &sky,
			FogColor: horizon, FogDensity: 0.0008,
			Haze: &scene.Haze{Density: 0.0011, HeightFalloff: 0.07, SunScatter: 0.32},
			Ocean: &scene.Ocean{
				WindDirection: -28, WaveHeight: 1.15, WaveLength: 30, Choppiness: 0.42, Speed: 0.85,
				DeepColor: "#03121b", ShallowColor: "#123b43", ScatterColor: "#16695e", FoamColor: "#dde9e5",
				Clarity: 2.8, Roughness: 0.095, Foam: 0.55, Surf: 0.65, Extent: 4000,
				Reflections: &scene.OceanReflections{Mode: "ssr+planar", Resolution: 0.5, Strength: 1},
				Bathymetry: &scene.OceanBathymetry{
					Src:  blackglassBeachHeightSrc,
					MinX: beachgen.BathymetryMinX, MinZ: beachgen.BathymetryMinZ, MaxX: beachgen.BathymetryMaxX, MaxZ: beachgen.BathymetryMaxZ,
					MinHeight: beachgen.BathymetryMinHeight, MaxHeight: beachgen.BathymetryMaxHeight, Encoding: beachgen.BathymetryEncoding,
				},
			},
		},
		PostFX: scene.PostFX{MaxPixels: scene.PostFXMaxPixels1440p, Effects: []scene.PostEffect{
			scene.GodRays{Intensity: 0.12},
			scene.Bloom{Mode: "mip", Threshold: 1.6, Strength: 0.08, Radius: 5, Scale: 0.5},
			scene.Tonemap{Mode: scene.TonemapACES, Exposure: period.Exposure},
			scene.Vignette{Intensity: 0.18},
			scene.FXAA{},
			scene.Grain{Intensity: 0.008},
		}},
		Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixels2048},
		Graph: scene.NewGraph(append([]scene.Node{
			scene.DirectionalLight{ID: "sun", Color: period.SunColor, Intensity: period.SunPower, Direction: scene.Vec3(-sun.X, -sun.Y, -sun.Z),
				CastShadow: true, ShadowBias: -0.0018, ShadowSize: 2048, ShadowCascades: 3, ShadowSoftness: 1.5},
			scene.Model{ID: "beach", Src: blackglassBeachModelRoot + "beach-v2.glb", Bounds: 90, CastShadow: true, ReceiveShadow: true, Detail: blackglassBeachDetail()},
			scene.Model{ID: "sea-stacks", Src: blackglassBeachModelRoot + "stacks-v2.glb", Bounds: 60, CastShadow: true, ReceiveShadow: true, Detail: blackglassBeachRockDetail()},
			scene.Model{ID: "jetty", Src: blackglassBeachModelRoot + "jetty.glb", CastShadow: true, ReceiveShadow: true},
			scene.Model{ID: "clipper", Src: blackglassBeachModelRoot + "clipper-high.glb", CastShadow: true, ReceiveShadow: true},
			scene.Model{ID: "clipper-mid", Src: blackglassBeachModelRoot + "clipper-mid.glb", Visible: scene.Bool(false), CastShadow: true, ReceiveShadow: true},
			scene.Model{ID: "clipper-low", Src: blackglassBeachModelRoot + "clipper-low.glb", Visible: scene.Bool(false), CastShadow: true, ReceiveShadow: true},
			scene.Model{ID: "dune-grass", Src: blackglassBeachModelRoot + "dune-grass.glb", ReceiveShadow: true},
			scene.Model{ID: "tideline-wrack", Src: blackglassBeachModelRoot + "tideline-wrack.glb", CastShadow: true, ReceiveShadow: true},
			blackglassBeachGulls(),
			scene.Model{ID: "monolith", Src: blackglassBeachModelRoot + "monolith-v2.glb", Bounds: 4,
				Position: scene.Vec3(beachgen.MonolithX, beachgen.MonolithY, beachgen.MonolithZ), Rotation: scene.Euler{Y: beachgen.MonolithYaw}, CastShadow: true, ReceiveShadow: true,
				Material: scene.StandardMaterial{Color: "#596f6b", Roughness: 0.065, Metalness: 0, Clearcoat: 0.95, Transmission: 0.88, IOR: scene.Float(1.48),
					Thickness: .45, AttenuationDistance: .3, AttenuationColor: &[3]float64{.12, .24, .21}}},
		}, append(blackglassBeachBakedMoments(period.ID), blackglassBeachPoolRipple())...)...),
	}
	// Scenery is not a pick target: keep pointer queries bounded to the three
	// pools and the collectible, rather than ray-testing the entire coastline.
	for i, node := range program.Graph.Nodes {
		switch n := node.(type) {
		case scene.Model:
			if !n.Interactive {
				n.Pickable = scene.Bool(false)
				program.Graph.Nodes[i] = n
			}
		case scene.Mesh:
			if !n.Interactive {
				n.Pickable = scene.Bool(false)
				program.Graph.Nodes[i] = n
			}
		case scene.InstancedMesh:
			n.Pickable = scene.Bool(false)
			program.Graph.Nodes[i] = n
		}
	}
	if view.ID == "pools" {
		program.Camera.PortraitFOV = 95
	}
	return program
}

// Scenic presets dolly around their subject; shore and ship remain walkable.
func blackglassBeachControls(viewID string) string {
	if viewID == "glass" || viewID == "cliff" || viewID == "pools" {
		return scene.ControlOrbit
	}
	return scene.ControlFirstPerson
}
