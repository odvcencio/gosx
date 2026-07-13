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
	"strconv"
	"strings"

	"m31labs.dev/gosx/route"
)

// waterDiagDefaults are the shipped values. A knob absent from the URL keeps its
// default, so /demos/water is byte-identical to what it was without diag.
var waterDiagDefaults = map[string]any{
	"diag":            false,
	"dpr":             1.0, // hard-capped: see page.gsx (Apple cliff)
	"maxPixels":       1200000,
	"resolution":      192,
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
	"meshRes":         0,
	"causticsRes":     512,
	"shadowRes":       512,
	"caustics":        true,
	"reflection":      true,
	"refraction":      true,
	"objectTexBudget": 786432,
	// water=0 removes the WaterSystem from the scene graph entirely. It is the
	// coarsest bisection there is: the cost is either inside the water system or it
	// is not. Everything finer (caustics, reflection, refraction, resolution) failed
	// to move the frame rate at all, so the next question is whether the water is
	// even the thing that is slow.
	"water": true,
}

// WaterDiagConfig resolves the water system's cost knobs from the URL, falling back
// to the shipped defaults. Returns the values plus whether the overlay is on.
func WaterDiagConfig(ctx *route.RouteContext) map[string]any {
	out := make(map[string]any, len(waterDiagDefaults))
	for k, v := range waterDiagDefaults {
		out[k] = v
	}
	if ctx == nil || ctx.Request == nil {
		return out
	}

	out["diag"] = waterDiagBool(ctx, "diag", false)
	out["dpr"] = waterDiagFloat(ctx, "dpr", waterDiagDefaults["dpr"].(float64), 0.5, 3.0)
	out["maxPixels"] = waterDiagInt(ctx, "maxPixels", waterDiagDefaults["maxPixels"].(int), 100000, 16000000)
	out["resolution"] = waterDiagInt(ctx, "res", waterDiagDefaults["resolution"].(int), 16, 512)
	out["meshRes"] = waterDiagInt(ctx, "meshRes", waterDiagDefaults["meshRes"].(int), 0, 512)
	out["causticsRes"] = waterDiagInt(ctx, "causticsRes", waterDiagDefaults["causticsRes"].(int), 0, 2048)
	out["shadowRes"] = waterDiagInt(ctx, "shadowRes", waterDiagDefaults["shadowRes"].(int), 0, 2048)
	out["caustics"] = waterDiagBool(ctx, "caustics", waterDiagDefaults["caustics"].(bool))
	out["reflection"] = waterDiagBool(ctx, "reflection", waterDiagDefaults["reflection"].(bool))
	out["refraction"] = waterDiagBool(ctx, "refraction", waterDiagDefaults["refraction"].(bool))
	out["objectTexBudget"] = waterDiagInt(ctx, "objectTexBudget", waterDiagDefaults["objectTexBudget"].(int), 0, 8000000)
	out["water"] = waterDiagBool(ctx, "water", waterDiagDefaults["water"].(bool))
	return out
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
