package vm

import (
	"encoding/base64"
	"hash/fnv"
	"strings"

	"m31labs.dev/gosx/motion"
)

// materialMotionPlayer drives SceneIR.MaterialMotionProgram on the shared
// render-bundle path. The wire program is the binary-encoded motion.Timeline
// produced by Graph.SceneIR() from every mesh's MaterialAnims (target refs are
// mesh IDs, prop refs are material uniform names). Each frame it is evaluated
// once at scene time and the decoded per-target uniform writes are applied to
// the matching objects' material fields BEFORE ensureRenderMaterial packs the
// frame's RenderMaterial records, so both WebGL2 and WebGPU observe animated
// emissive/roughness/metalness/... through one common path.
//
// This closes the same substrate gap applyWasmMaterialMotionFrame covers on
// the opt-in JS fall-through seam: production shared-runtime scenes never run
// that seam (mount.ts returns after ctx.runtime.renderFrame), so without this
// player declared MaterialAnims choreography is inert on the shipped path.
type materialMotionPlayer struct {
	tl         *motion.Timeline
	targetRefs []string
	propRefs   []string
	buf        *motion.WriteBuf
	// key fingerprints the base64 payload this player was decoded from, so the
	// per-frame lookup can reuse the decoded timeline across frames and
	// re-decode only when the program genuinely changes.
	key uint64
}

// sceneMaterialMotionProgram extracts the base64 MaterialMotionProgram payload
// from props. It rides under props.scene like the rest of the lowered SceneIR.
func sceneMaterialMotionProgram(props map[string]any) string {
	if payload, ok := sceneValue(props, "materialMotionProgram").(string); ok {
		return strings.TrimSpace(payload)
	}
	if payload, ok := propValue(props, "materialMotionProgram").(string); ok {
		return strings.TrimSpace(payload)
	}
	return ""
}

// materialMotionForFrame returns the decoded player for this frame's program,
// decoding (and caching on sc) at most once per distinct payload. A missing or
// undecodable program disables material motion for the frame; declared
// choreography then simply does not advance rather than corrupting materials.
func materialMotionForFrame(props map[string]any, sc *spinScratch) *materialMotionPlayer {
	payload := sceneMaterialMotionProgram(props)
	if payload == "" {
		return nil
	}
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(payload))
	key := hasher.Sum64()
	if sc != nil && sc.matMotion != nil && sc.matMotion.key == key {
		return sc.matMotion
	}
	blob, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil
	}
	tl, targetRefs, propRefs, err := motion.DecodeProgram(blob)
	if err != nil {
		return nil
	}
	player := &materialMotionPlayer{
		tl:         tl,
		targetRefs: targetRefs,
		propRefs:   propRefs,
		buf:        motion.NewWriteBuf(32),
		key:        key,
	}
	if sc != nil {
		sc.matMotion = player
	}
	return player
}

// applyMaterialMotionFrame evaluates the program at t and writes each decoded
// uniform value into the object whose ID matches the track's target ref.
// Writes follow the shared motion packet layout [targetID, propID, arity,
// comps...]; unknown targets/uniforms are skipped silently, mirroring the JS
// decoder's tolerance.
func applyMaterialMotionFrame(player *materialMotionPlayer, objects []sceneObject, t float64) {
	if player == nil || player.tl == nil || len(objects) == 0 {
		return
	}
	byID := make(map[string]*sceneObject, len(objects))
	for i := range objects {
		if id := strings.TrimSpace(objects[i].ID); id != "" {
			byID[id] = &objects[i]
		}
	}
	player.buf.Reset()
	motion.Eval(player.tl, t, motion.Policy{}, player.buf)
	f := player.buf.Writes()
	for i := 0; i+3 <= len(f); {
		targetIdx := int(f[i])
		propIdx := int(f[i+1])
		width := motion.ValueArity(f[i+2]).Width()
		comps := i + 3
		if comps+width > len(f) {
			break
		}
		i = comps + width
		if targetIdx < 0 || targetIdx >= len(player.targetRefs) {
			continue
		}
		if propIdx < 0 || propIdx >= len(player.propRefs) {
			continue
		}
		object, ok := byID[strings.TrimSpace(player.targetRefs[targetIdx])]
		if !ok {
			continue
		}
		applySceneObjectUniform(object, player.propRefs[propIdx], f[comps:comps+width])
	}
}

// applySceneObjectUniform routes one evaluated uniform write onto the object's
// material fields. Scalar uniforms map onto the packed RenderMaterial scalars;
// wider arities keep their first component for scalar-only fields (the lowering
// clamps scalar tracks to ArityScalar, so this is defensive only). Vector
// uniforms that have no scalar field here (e.g. RGB color) are ignored on this
// path — custom-material color motion keeps flowing through the renderer-side
// customUniforms seam instead of being silently misapplied.
func applySceneObjectUniform(object *sceneObject, uniform string, comps []float64) {
	if object == nil || len(comps) == 0 {
		return
	}
	value := comps[0]
	switch strings.ToLower(strings.TrimSpace(uniform)) {
	case "emissive":
		object.Emissive = value
		object.HasEmissive = true
	case "roughness":
		object.Roughness = value
		object.HasRoughness = true
	case "metalness":
		object.Metalness = value
		object.HasMetalness = true
	case "clearcoat":
		object.Clearcoat = value
		object.HasClearcoat = true
	case "sheen":
		object.Sheen = value
		object.HasSheen = true
	case "transmission":
		object.Transmission = value
		object.HasTransmission = true
	case "iridescence":
		object.Iridescence = value
		object.HasIridescence = true
	case "anisotropy":
		object.Anisotropy = value
		object.HasAnisotropy = true
	case "opacity":
		object.Opacity = value
		object.HasOpacity = true
	}
}
