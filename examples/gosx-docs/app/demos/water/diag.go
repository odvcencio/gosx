package docs

// Water demo perf diagnostics.
//
// The water demo falls off a cliff on Apple/Metal (measured: 502x383 CSS canvas,
// DPR 1.0 -> 0.192 MP -> 120fps; DPR 1.6 -> 0.49 MP -> 17fps — 2.5x the pixels for
// 7x the time) while a desktop RTX absorbs it. Reasoning about that from a Linux
// box with a software rasteriser has repeatedly produced confident, wrong answers,
// and the headless pixel/perf gates cannot adjudicate it: SwiftShader is
// non-deterministic against ITSELF (same code, two runs: up to 8/255 per-channel
// drift over 8-20% of pixels).
//
// So this makes the real machine the instrument. Every expensive knob in the water
// system becomes a URL parameter, and an on-page overlay reports the frame cost the
// browser actually delivers. Flip one knob at a time on the affected hardware and
// the cost attributes itself.
//
//	/demos/water?diag=1                        overlay only, stock settings
//	/demos/water?diag=1&dpr=1.6                the configuration that dies
//	/demos/water?diag=1&dpr=1.6&caustics=0     ...is it the caustics pass?
//	/demos/water?diag=1&dpr=1.6&reflection=0   ...the reflection ray?
//	/demos/water?diag=1&dpr=1.6&refraction=0   ...the refraction ray?
//	/demos/water?diag=1&dpr=1.6&res=96         ...the mesh/sim density?
//
// Nothing here changes the demo for anyone who does not ask for it: with no
// parameters the values are exactly the shipped ones.

import (
	"math"
	"strconv"
	"strings"

	"m31labs.dev/gosx/route"
)

// waterDiagDefaults are the shipped Balanced values. A knob absent from the
// URL keeps the selected quality profile's value; with no quality query the
// profile is Balanced, preserving the measured configuration below.
//
// These defaults ARE the shipped configuration: page.gsx binds every knob to
// the resolved diag value, so this table is the single source of truth. That
// binding regressed once — the v0.31.6 water merge replaced the data.diag*
// bindings with hardcoded literals (sim 256 / mesh 201 / caustics 1024), which
// silently shipped the surface at the tessellation this file's own fps table
// identifies as the 17 fps cliff, and turned every knob below into a dead
// no-op. If page.gsx and this table ever disagree again, page.gsx is wrong.
var waterDiagDefaults = map[string]any{
	"diag":           false,
	"quality":        "balanced",
	"dpr":            1.6,
	"maxPixels":      1200000,
	"msaa":           0,
	"antialias":      nil,
	"capabilityTier": "",
	// 256 matches the reference implementation's simulation grid. Simulation,
	// normals and shading sample the heightfield by uv at this resolution
	// regardless of mesh density, and the sim passes are cheap (fixed 256^2
	// compute), so ripple fidelity is preserved even with a coarse mesh.
	"resolution": 256,
	// meshRes tessellates the surface INDEPENDENTLY of the simulation. 0 = match
	// resolution, i.e. exactly what shipped. The surface is currently drawn at
	// roughly one triangle per 1.4 screen pixels; a GPU shades in 2x2 quads, so
	// every sub-pixel triangle still bills a full four-lane quad of the expensive
	// reflection/refraction shader. That predicts cost tracks triangle count rather
	// than pixel count -- which is exactly what the measurements say: 2.56x the
	// pixels (dpr 1.6) cost only 1.27x the time, while dropping resolution moved it
	// enormously. Simulation, normals and shading all stay at full resolution
	// because both surface shaders sample the heightfield by normalized uv.
	//
	//	/demos/water?diag=1&meshRes=96   quarter the triangles, same sim
	//	/demos/water?diag=1&meshRes=64   ninth the triangles, same sim
	//
	// 48 is the shipped default, measured on Apple/Metal:
	//
	//	mesh 192 (= sim)  17 fps     mesh 48   ~60 fps
	//	mesh  96          30 fps     mesh 16   120 fps
	//
	// The cost is PER-TRIANGLE, not per-pixel. At mesh 192 the surface is ~72,000 triangles
	// over ~100k screen pixels -- well under two pixels each -- and a GPU shades in 2x2
	// quads, so every sliver still bills a full four-lane quad of a fragment shader that
	// does six dependent heightfield taps plus normal reconstruction. Coarser triangles
	// collapse that waste: same pixels, same shading, a fraction of the quad overdraw.
	// Turning reflection/refraction off changes nothing, which is what proved the cost is
	// the quad amplification rather than the optional lighting work.
	//
	// Shading is unaffected by mesh density: the fragment stage samples the heightfield by
	// normalized uv at the full simulation resolution, so all visible ripple detail lives in
	// the normal, not the geometry. The mesh only carries the low-frequency swell.
	// meshRes=0 restores a mesh matching the simulation, for comparison.
	"meshRes":         48,
	"causticsRes":     1024,
	"shadowRes":       1024,
	"caustics":        true,
	"reflection":      true,
	"refraction":      true,
	"objectTexBudget": 393216,
	// water=0 removes the WaterSystem from the scene graph entirely. It is the
	// coarsest bisection there is: the cost is either inside the water system or it
	// is not. Everything finer (caustics, reflection, refraction, resolution) failed
	// to move the frame rate at all, so the next question is whether the water is
	// even the thing that is slow.
	"water": true,
}

// waterQualityProfiles are presentation presets, not hidden capability
// switches. Every tier keeps simulation, reflection, refraction, caustics and
// shadows enabled; the lower tiers reduce tessellation and offscreen work
// before reducing canvas clarity. The existing diagnostic query knobs remain
// authoritative and can override any individual value after a profile is
// selected.
var waterQualityProfiles = map[string]map[string]any{
	"hero": {
		"dpr": 1.9, "maxPixels": 2073600, "msaa": 4, "antialias": true, "capabilityTier": "full", "resolution": 256, "meshRes": 64,
		"causticsRes": 1024, "shadowRes": 1024, "objectTexBudget": 786432,
		"caustics": true, "reflection": true, "refraction": true, "water": true,
	},
	"balanced": {
		"dpr": 1.6, "maxPixels": 1200000, "msaa": 0, "antialias": nil, "capabilityTier": "", "resolution": 256, "meshRes": 48,
		"causticsRes": 1024, "shadowRes": 1024, "objectTexBudget": 393216,
		"caustics": true, "reflection": true, "refraction": true, "water": true,
	},
	"battery": {
		"dpr": 1.25, "maxPixels": 921600, "msaa": 1, "antialias": false, "capabilityTier": "constrained", "resolution": 256, "meshRes": 32,
		"causticsRes": 512, "shadowRes": 512, "objectTexBudget": 230400,
		"caustics": true, "reflection": true, "refraction": true, "water": true,
	},
}

func waterQualityProfile(name string) (string, map[string]any) {
	name = strings.ToLower(strings.TrimSpace(name))
	profile, ok := waterQualityProfiles[name]
	if !ok {
		name = "balanced"
		profile = waterQualityProfiles[name]
	}
	out := make(map[string]any, len(waterDiagDefaults))
	for key, value := range waterDiagDefaults {
		out[key] = value
	}
	for key, value := range profile {
		out[key] = value
	}
	out["quality"] = name
	return name, out
}

// WaterDiagConfig resolves the water system's cost knobs from the URL, falling back
// to the selected profile. Returns the values plus whether the overlay is on.
func WaterDiagConfig(ctx *route.RouteContext) map[string]any {
	if ctx == nil || ctx.Request == nil {
		_, out := waterQualityProfile("")
		out["qualityProfiles"] = waterAdaptiveQualityProfiles(out)
		waterSetQualityCurrent(out)
		return out
	}

	_, out := waterQualityProfile(ctx.Query("quality"))
	out["diag"] = waterDiagBool(ctx, "diag", false)
	out["dpr"] = waterDiagFloat(ctx, "dpr", out["dpr"].(float64), 0.5, 3.0)
	out["maxPixels"] = waterDiagInt(ctx, "maxPixels", out["maxPixels"].(int), 100000, 16000000)
	out["msaa"] = waterDiagInt(ctx, "msaa", out["msaa"].(int), 0, 8)
	out["resolution"] = waterDiagInt(ctx, "res", out["resolution"].(int), 16, 512)
	out["meshRes"] = waterDiagInt(ctx, "meshRes", out["meshRes"].(int), 0, 512)
	out["causticsRes"] = waterDiagInt(ctx, "causticsRes", out["causticsRes"].(int), 0, 2048)
	out["shadowRes"] = waterDiagInt(ctx, "shadowRes", out["shadowRes"].(int), 0, 2048)
	out["caustics"] = waterDiagBool(ctx, "caustics", out["caustics"].(bool))
	out["reflection"] = waterDiagBool(ctx, "reflection", out["reflection"].(bool))
	out["refraction"] = waterDiagBool(ctx, "refraction", out["refraction"].(bool))
	out["objectTexBudget"] = waterDiagInt(ctx, "objectTexBudget", out["objectTexBudget"].(int), 0, 8000000)
	out["water"] = waterDiagBool(ctx, "water", out["water"].(bool))
	out["qualityProfiles"] = waterAdaptiveQualityProfiles(out)
	waterSetQualityCurrent(out)
	return out
}

func waterSetQualityCurrent(config map[string]any) {
	quality, _ := config["quality"].(string)
	config["qualityHeroCurrent"] = ""
	config["qualityBalancedCurrent"] = ""
	config["qualityBatteryCurrent"] = ""
	switch quality {
	case "hero":
		config["qualityHeroCurrent"] = "page"
	case "battery":
		config["qualityBatteryCurrent"] = "page"
	default:
		config["qualityBalancedCurrent"] = "page"
	}
}

// waterAdaptiveQualityProfiles makes the selected demo profile the adaptive
// governor's initial "full" rung. Without this local override, Scene3D's
// generic full rung truthfully renders the authored mesh but publishes and
// applies its unrelated 1.6 DPR/160-grid budget, masking Battery's requested
// caps in runtime telemetry. The local balanced/survival rungs scale every
// expensive axis monotonically so adaptive demotion can never increase work.
func waterAdaptiveQualityProfiles(config map[string]any) map[string]map[string]any {
	baseDPR := config["dpr"].(float64)
	baseSurface := max(32, config["meshRes"].(int))
	baseCaustics := max(64, config["causticsRes"].(int))
	baseShadow := max(64, config["shadowRes"].(int))
	baseObjectBudget := max(65536, config["objectTexBudget"].(int))

	profile := func(dprScale, surfaceScale, offscreenScale, objectScale float64, cadence int) map[string]any {
		objectBudget := max(65536, int(math.Floor(float64(baseObjectBudget)*objectScale)))
		objectMaxSide := int(math.Ceil(math.Sqrt(float64(objectBudget))))
		return map[string]any{
			"dprCap":                   math.Max(1, baseDPR*dprScale),
			"surfaceResolution":        max(32, int(math.Floor(float64(baseSurface)*surfaceScale))),
			"causticsResolution":       max(64, int(math.Floor(float64(baseCaustics)*offscreenScale))),
			"objectShadowResolution":   max(64, int(math.Floor(float64(baseShadow)*offscreenScale))),
			"objectTextureMaxSide":     max(64, min(2048, objectMaxSide)),
			"objectTexturePixelBudget": objectBudget,
			"expensivePassCadence":     cadence,
		}
	}
	return map[string]map[string]any{
		"full":     profile(1, 1, 1, 1, 1),
		"balanced": profile(0.8, 0.75, 0.5, 0.5, 2),
		"survival": profile(0.6, 0.5, 0.25, 0.25, 3),
	}
}

func waterDiagBool(ctx *route.RouteContext, name string, fallback bool) bool {
	raw := strings.TrimSpace(ctx.Query(name))
	if raw == "" {
		return fallback
	}
	switch strings.ToLower(raw) {
	case "1", "true", "on", "yes":
		return true
	case "0", "false", "off", "no":
		return false
	}
	return fallback
}

func waterDiagInt(ctx *route.RouteContext, name string, fallback, min, max int) int {
	raw := strings.TrimSpace(ctx.Query(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < min || v > max {
		return fallback
	}
	return v
}

func waterDiagFloat(ctx *route.RouteContext, name string, fallback, min, max float64) float64 {
	raw := strings.TrimSpace(ctx.Query(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v < min || v > max {
		return fallback
	}
	return v
}
