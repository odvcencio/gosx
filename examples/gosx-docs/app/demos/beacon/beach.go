package docs

import (
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"sync"

	"m31labs.dev/gosx/examples/gosx-docs/app/demos/beacon/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

// Blackglass Beach: a black volcanic-sand beach, open ocean to the horizon,
// basalt sea stacks and one obsidian monolith, lit by an analytic sky. Every
// asset comes from beachgen, and the IBL is baked from the same sky the
// renderer draws.

const blackglassBeachModelRoot = "/models/blackglass/"

type blackglassBeachPeriod struct {
	ID, Name string
	SunColor string
	SunPower float64
	Exposure float32
}

func blackglassBeachPeriodFor(raw string) blackglassBeachPeriod {
	switch raw {
	case beachgen.PeriodBlue:
		return blackglassBeachPeriod{beachgen.PeriodBlue, "Blue hour", "#9fb4ff", 0.35, 0.95}
	case beachgen.PeriodNoon:
		return blackglassBeachPeriod{beachgen.PeriodNoon, "Noon", "#fff4e6", 3.2, 0.5}
	default:
		return blackglassBeachPeriod{beachgen.PeriodGolden, "Golden hour", "#ffc690", 2.6, 0.62}
	}
}

type blackglassBeachView struct {
	ID, Name         string
	Position, Target scene.Vector3
}

func blackglassBeachViewFor(raw string) blackglassBeachView {
	switch raw {
	case "glass":
		return blackglassBeachView{"glass", "The glass", scene.Vec3(-2.2, 1.6, 8.8), scene.Vec3(-6.2, 1.9, 2.6)}
	case "cliff":
		return blackglassBeachView{"cliff", "From the cliff", scene.Vec3(-26, 17, 22), scene.Vec3(-2, 0, -18)}
	default:
		return blackglassBeachView{"shore", "The shore", scene.Vec3(2.4, 2.9, 14.5), scene.Vec3(-2, 0.4, -22)}
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
func blackglassBeachHorizon(sky scene.Sky) string {
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
		x *= 0.6 // the scene exposure
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
	sun := sky.SunDirection
	horizon := blackglassBeachHorizon(sky)
	return scene.Props{
		Width: 1280, Height: 720,
		Label:      "Blackglass Beach — " + view.Name + " at " + period.Name,
		AriaLabel:  "A black sand beach at " + period.Name + ": waves run up the sand below basalt sea stacks, and an obsidian monolith stands at the waterline.",
		Background: horizon, Controls: scene.ControlOrbit, AutoRotate: scene.Bool(false), Responsive: scene.Bool(true), FillHeight: scene.Bool(true),
		PreferWebGPU: scene.Bool(true), CanvasAlpha: scene.Bool(false), Stats: scene.Bool(false),
		UnsupportedMessage: "Interactive 3D is unavailable in this browser.",
		ControlTarget:      view.Target, ControlMinDistance: 2, ControlMaxDistance: 80,
		MaxFPS: 60, MaxDevicePixelRatio: 2, MaxPixels: scene.PostFXMaxPixels1440p,
		AdaptiveQuality: scene.Bool(true), AdaptiveTargetFrameMS: 16.7, AdaptiveWarmupFrames: 24, AdaptivePostFX: scene.Bool(true),
		Camera: scene.PerspectiveCamera{Position: view.Position, FOV: 42, PortraitFOV: 70, Near: 0.1, Far: 900},
		Environment: scene.Environment{
			IBL: blackglassBeachPeriodIBL(period.ID), EnvIntensity: 1,
			Sky:      &sky,
			FogColor: horizon, FogDensity: 0.0035,
			Ocean: &scene.Ocean{
				WindDirection: 8, WaveHeight: 0.9, WaveLength: 17, Choppiness: 0.7, Speed: 1,
				DeepColor: "#021019", ShallowColor: "#1b5d63", ScatterColor: "#1f8f7c", FoamColor: "#eef3f2",
				Clarity: 3.5, Roughness: 0.05, Foam: 0.7, Surf: 0.6, Extent: 4000,
				Bathymetry: &scene.OceanBathymetry{
					Src:  blackglassBeachModelRoot + "beach-v2-height.png",
					MinX: beachgen.BathymetryMinX, MinZ: beachgen.BathymetryMinZ, MaxX: beachgen.BathymetryMaxX, MaxZ: beachgen.BathymetryMaxZ,
					MinHeight: beachgen.BathymetryMinHeight, MaxHeight: beachgen.BathymetryMaxHeight,
				},
			},
		},
		PostFX: scene.PostFX{MaxPixels: scene.PostFXMaxPixels1440p, Effects: []scene.PostEffect{
			scene.Bloom{Threshold: 1.2, Strength: 0.3, Radius: 6, Scale: 0.5},
			scene.Tonemap{Mode: scene.TonemapACES, Exposure: period.Exposure},
			scene.Vignette{Intensity: 0.18},
			scene.FXAA{},
		}},
		Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixels2048},
		Graph: scene.NewGraph(
			scene.DirectionalLight{ID: "sun", Color: period.SunColor, Intensity: period.SunPower, Direction: scene.Vec3(-sun.X, -sun.Y, -sun.Z),
				CastShadow: true, ShadowBias: -0.0006, ShadowSize: 2048, ShadowCascades: 3, ShadowSoftness: 1.5},
			scene.Model{ID: "beach", Src: blackglassBeachModelRoot + "beach-v2.glb", Bounds: 90, CastShadow: true, ReceiveShadow: true},
			scene.Model{ID: "sea-stacks", Src: blackglassBeachModelRoot + "stacks-v2.glb", Bounds: 60, CastShadow: true, ReceiveShadow: true},
			scene.Model{ID: "monolith", Src: blackglassBeachModelRoot + "monolith-v2.glb", Bounds: 4,
				Position: scene.Vec3(-6.2, 0.05, 2.6), Rotation: scene.Euler{Y: 0.75}, CastShadow: true, ReceiveShadow: true,
				Material: scene.StandardMaterial{Color: "#050608", Roughness: 0.035, Metalness: 0, Clearcoat: 1}},
		),
	}
}
