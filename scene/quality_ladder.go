package scene

import "strings"

// QualityRungIR is the typed compatibility record for one QualityRung,
// lowered from Props.QualityLadder into the legacy scene.qualityLadder[i]
// prop bag shape the client governor reads. See QualityRung for field
// semantics; this type additionally guarantees the lowering-time
// normalization (clamped ranges, non-empty Name) documented on
// resolveQualityRung.
type QualityRungIR struct {
	Name                 string   `json:"name,omitempty"`
	PostEffects          []string `json:"postEffects,omitempty"`
	LayerGroups          []string `json:"layerGroups,omitempty"`
	ComputeBudgetScale   float64  `json:"computeBudgetScale,omitempty"`
	ExpensivePassCadence int      `json:"expensivePassCadence,omitempty"`
}

// QualityRung is one authored step of a Props.QualityLadder.
//
// Design law (verbatim from the product owner): "Lower fidelity should never
// feel fuzzy or badly blurry. Everything should read cleanly always."
// Degradation reduces WORK (effects, layers, particle budgets, cadence),
// never CLARITY. This struct has NO resolution/DPR/postFX-pixel-budget field
// — that is enforced by construction, not by a runtime check, because there
// is nothing to check: a ladder author physically cannot author a
// resolution-ish knob here. Low rungs converge toward the crisp RAW
// composite (post-FX off, native DPR) — never toward a blurred full-effects
// scene scaled down.
type QualityRung struct {
	// Name is a stable, author-chosen label surfaced verbatim on
	// data-gosx-scene3d-quality-rung-name and in quality-rung-transition
	// events. An empty Name falls back to "rung-<index>" at lowering time.
	Name string

	// PostEffects lists the post-processing effect kinds (e.g. "bloom",
	// "toneMapping", "fxaa") or CustomPost.Name values admitted at this
	// rung. Empty means "raw" — post-FX off, the crisp native-DPR composite
	// the PRIME DIRECTIVE calls the floor every rung converges toward.
	//
	// Composes with the G1 live-patchable postFXMaxPixels mechanism: an
	// admitted effect always runs at full postFX resolution. An effect not
	// listed here is simply ABSENT at this rung — never rendered at reduced
	// resolution.
	PostEffects []string

	// LayerGroups lists the author-tagged Mesh.QualityGroup values visible
	// at this rung. A mesh with no QualityGroup is unconditionally visible
	// at every rung; LayerGroups only gates meshes that opted in. Toggling
	// is instant (no transition) — the app choreographs any visual
	// blending itself.
	LayerGroups []string

	// ComputeBudgetScale scales particle/compute counts at this rung where
	// the runtime supports it. Range [0, 1]; 1 = full authored budget,
	// values are clamped at lowering time.
	//
	// v1 note: the client governor currently PASSES THIS THROUGH to
	// data-gosx-scene3d-quality-rung-compute-budget-scale and the rung
	// telemetry object rather than actually scaling any compute/particle
	// dispatch — wiring an actual scale into the compute-particle or
	// instanced-mesh count would touch the WebGL/WebGPU backend dispatch
	// paths, which is deliberately out of scope for this milestone (see the
	// G2 report's deferred items). Apps that want real work reduction today
	// should read the attribute/event and drive their own particle counts.
	ComputeBudgetScale float64

	// ExpensivePassCadence: 1 = run every frame, N = run every Nth frame.
	// This is a WORK reduction (allowed by the design law), not a
	// resolution reduction. Values below 1 are clamped to 1 at lowering.
	//
	// v1 note: same pass-through caveat as ComputeBudgetScale — see the G2
	// report. The value ships on the rung telemetry/attrs; it is not yet
	// wired into the water/shadow expensive-pass cadence machinery (that
	// lives in the WebGL/WebGPU backend files, out of scope here to avoid
	// touching renderer internals owned by concurrent work).
	ExpensivePassCadence int
}

// resolveQualityRung normalizes one authored QualityRung for IR emission:
// clamps ComputeBudgetScale to [0,1], ExpensivePassCadence to >=1, and
// falls back to "rung-<index>" when Name is blank. Called once per rung by
// sceneIR() below.
func resolveQualityRung(r QualityRung, index int) QualityRungIR {
	name := strings.TrimSpace(r.Name)
	if name == "" {
		name = "rung-" + intString(index)
	}
	// ComputeBudgetScale follows the same "zero means unset, gets the sane
	// default" idiom used throughout postfx_ir.go (Bloom.Scale, SSAO.Bias,
	// etc.): a rung author who says nothing gets the full authored budget,
	// not an accidentally-starved 0.
	scale := r.ComputeBudgetScale
	if scale == 0 {
		scale = 1
	} else if scale < 0 {
		scale = 0
	} else if scale > 1 {
		scale = 1
	}
	cadence := r.ExpensivePassCadence
	if cadence < 1 {
		cadence = 1
	}
	var postEffects []string
	for _, e := range r.PostEffects {
		e = strings.TrimSpace(e)
		if e != "" {
			postEffects = append(postEffects, e)
		}
	}
	var layerGroups []string
	for _, g := range r.LayerGroups {
		g = strings.TrimSpace(g)
		if g != "" {
			layerGroups = append(layerGroups, g)
		}
	}
	return QualityRungIR{
		Name:                 name,
		PostEffects:          postEffects,
		LayerGroups:          layerGroups,
		ComputeBudgetScale:   scale,
		ExpensivePassCadence: cadence,
	}
}

// legacyProps mirrors QualityRungIR's json tags for the map-tree
// Props.LegacyProps() path (SceneIR.legacyProps()) — kept in sync with the
// struct tags by hand, same convention as the rest of this file's IR types
// (see e.g. ObjectIR.legacyProps()).
func (r QualityRungIR) legacyProps() map[string]any {
	out := map[string]any{}
	setString(out, "name", r.Name)
	if len(r.PostEffects) > 0 {
		out["postEffects"] = append([]string(nil), r.PostEffects...)
	}
	if len(r.LayerGroups) > 0 {
		out["layerGroups"] = append([]string(nil), r.LayerGroups...)
	}
	setNumeric(out, "computeBudgetScale", r.ComputeBudgetScale)
	setInt(out, "expensivePassCadence", r.ExpensivePassCadence)
	return out
}

// sceneIR lowers a []QualityRung into the wire IR shape. Returns nil for an
// empty/nil ladder so SceneIR.QualityLadder stays omitted (no ladder
// authored — legacy dprCap-tier client behavior applies).
func qualityLadderSceneIR(rungs []QualityRung) []QualityRungIR {
	if len(rungs) == 0 {
		return nil
	}
	out := make([]QualityRungIR, 0, len(rungs))
	for i, r := range rungs {
		out = append(out, resolveQualityRung(r, i))
	}
	return out
}

// resolveQualityStartRung clamps QualityStartRung into a valid index for a
// non-empty ladder. Returns 0 for an empty ladder (the field is meaningless
// there) regardless of the input value.
func resolveQualityStartRung(rungs []QualityRung, start int) int {
	if len(rungs) == 0 {
		return 0
	}
	if start < 0 {
		return 0
	}
	if start >= len(rungs) {
		return len(rungs) - 1
	}
	return start
}

// QualityLadderWarnings validates a QualityLadder authoring for the
// anti-patterns the PRIME DIRECTIVE rules out that a Go struct alone cannot
// enforce (there is no resolution/DPR field to check — see QualityRung's
// doc comment; that half of the design law is enforced by construction).
// Warnings are advisory: the runtime still degrades gracefully even if a
// warning fires, but authors should fix the underlying ambiguity.
//
// Call with the Props values directly: p.QualityLadderWarnings().
func (p Props) QualityLadderWarnings() []string {
	return qualityLadderWarnings(p.QualityLadder, p.QualityStartRung, p.AdaptiveQuality)
}

func qualityLadderWarnings(rungs []QualityRung, startRung int, adaptiveQuality *bool) []string {
	if len(rungs) == 0 {
		return nil
	}
	var warnings []string
	// A ladder supersedes the legacy dprCap-tier adaptiveQuality governor
	// entirely (see the G2 client governor) — authoring both is not a hard
	// conflict (the client always prefers the ladder), but it silently
	// strands the adaptiveQuality config, which is very likely not what the
	// author intended.
	if adaptiveQuality != nil && *adaptiveQuality {
		warnings = append(warnings, "QualityLadder is authored alongside AdaptiveQuality=true (dprCap tiers); "+
			"the ladder supersedes the dprCap-tier governor and AdaptiveQuality's tier/profile behavior will not run — "+
			"remove one of the two to avoid ambiguity")
	}
	if startRung < 0 || startRung >= len(rungs) {
		warnings = append(warnings, "QualityStartRung "+intString(startRung)+" is out of range for a "+
			intString(len(rungs))+"-rung QualityLadder; it will be clamped to a valid index")
	}
	for i, r := range rungs {
		if r.ComputeBudgetScale < 0 || r.ComputeBudgetScale > 1 {
			warnings = append(warnings, "QualityLadder["+intString(i)+"].ComputeBudgetScale must be in [0,1]; it will be clamped")
		}
		if r.ExpensivePassCadence < 1 {
			warnings = append(warnings, "QualityLadder["+intString(i)+"].ExpensivePassCadence must be >= 1; it will be clamped to 1")
		}
	}
	return warnings
}
