package scene

import "math"

// finiteMorphWeights returns a copy of weights without empty names or
// non-finite values (JSON cannot carry NaN or Inf). It returns nil when
// nothing is left, so the field is omitted from the IR.
func finiteMorphWeights(weights map[string]float64) map[string]float64 {
	var out map[string]float64
	for name, w := range weights {
		if name == "" || math.IsNaN(w) || math.IsInf(w, 0) {
			continue
		}
		if out == nil {
			out = make(map[string]float64, len(weights))
		}
		out[name] = w
	}
	return out
}

// ModelPatch changes one mounted Model in place, without loading it again:
// the model keeps its skeleton, its pose and its place in the scene. A nil
// field leaves that property as it is.
type ModelPatch struct {
	// ID names the Model (Model.ID).
	ID string
	// MorphWeights replaces the model's own morph weights (see
	// Model.MorphWeights). Targets the map does not name return to their
	// authored weight, so an empty non-nil map clears every weight. A model
	// declared with MorphWeights changes on the next frame; a model declared
	// without them loads again, once, to keep its morph targets.
	MorphWeights map[string]float64
	// Scale replaces the model's scale.
	Scale *Vector3
	// Visible shows or hides the model.
	Visible *bool
}

// PatchModelsCommand changes mounted models in place (ModelPatch). Patches
// that name no mounted model change nothing.
func PatchModelsCommand(patches []ModelPatch) Command {
	out := make([]map[string]any, 0, len(patches))
	for _, p := range patches {
		if p.ID == "" {
			continue
		}
		entry := map[string]any{"id": p.ID}
		if p.MorphWeights != nil {
			weights := finiteMorphWeights(p.MorphWeights)
			if weights == nil {
				weights = map[string]float64{}
			}
			entry["morphWeights"] = weights
		}
		if p.Scale != nil {
			entry["scaleX"], entry["scaleY"], entry["scaleZ"] = p.Scale.X, p.Scale.Y, p.Scale.Z
		}
		if p.Visible != nil {
			entry["visible"] = *p.Visible
		}
		out = append(out, entry)
	}
	return Command{Kind: CommandPatchModels, Data: map[string]any{"models": out}}
}
