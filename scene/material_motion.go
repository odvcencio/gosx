package scene

import (
	"strings"

	"m31labs.dev/gosx/motion"
)

// materialArity maps a MaterialUniformAnim.Arity (component width 1/3/4) and the
// uniform name to a motion.ValueArity plus the flat-slice chunk width.
//
//   - 1 → ArityScalar (width 1) — e.g. roughness, metalness.
//   - 3 → ArityVec3   (width 3) — e.g. an RGB color triple.
//   - 4 → ArityColor when the uniform name implies a color (emissive/color/...),
//     otherwise ArityVec4 (both width 4). The distinction is purely semantic;
//     the evaluator lerps all width-4 arities component-wise. ArityColor lets the
//     runtime route the value to a color uniform without name re-parsing.
//
// Any other arity returns width 0 so the caller skips the malformed entry.
func materialArity(arity int, uniform string) (motion.ValueArity, int) {
	switch arity {
	case 1:
		return motion.ArityScalar, 1
	case 3:
		return motion.ArityVec3, 3
	case 4:
		if uniformIsColor(uniform) {
			return motion.ArityColor, 4
		}
		return motion.ArityVec4, 4
	default:
		return 0, 0
	}
}

// uniformIsColor reports whether a uniform name implies a color value, so a
// width-4 animation is tagged ArityColor rather than ArityVec4.
func uniformIsColor(uniform string) bool {
	u := strings.ToLower(strings.TrimSpace(uniform))
	switch u {
	case "color", "emissive", "emissivecolor", "basecolor", "tint", "albedo", "diffuse", "specular", "sheencolor":
		return true
	}
	return strings.Contains(u, "color")
}

// materialMotionTracks lowers a mesh's MaterialAnims into MotionIR keyframe
// Tracks targeting the material identified by ref (the mesh's id). Each anim
// becomes one Track with Target{Kind: TargetMaterial, Ref: ref}, Prop = the
// uniform name, and keys chunked from Times/Values by the resolved arity width.
//
// NOTE: writing an animated uniform on a SHARED named material mutates the
// customUniforms bag for all meshes referencing it; per-mesh isolation would
// require cloning the material instance before applying the frame value.
//
// Malformed entries are silently skipped (no panic):
//   - unknown arity (not 1/3/4),
//   - empty Uniform or empty Times,
//   - Values too short for len(Times)*width.
//
// "STEP" maps to InterpStep; anything else maps to InterpLinear.
func materialMotionTracks(anims []MaterialUniformAnim, ref string) []motion.Track {
	if len(anims) == 0 {
		return nil
	}
	out := make([]motion.Track, 0, len(anims))
	for _, a := range anims {
		uniform := strings.TrimSpace(a.Uniform)
		if uniform == "" {
			continue
		}

		// Generator paths REPLACE the keyframe path. Spring takes precedence if
		// both are set. Malformed generator specs are skipped (no panic).
		if a.Spring != nil {
			if tr, ok := materialSpringTrack(a, uniform, ref); ok {
				out = append(out, tr)
			}
			continue
		}
		if a.Oscillator != nil {
			if tr, ok := materialOscillatorTrack(a, uniform, ref); ok {
				out = append(out, tr)
			}
			continue
		}

		arity, w := materialArity(a.Arity, uniform)
		if w == 0 {
			continue
		}
		nKeys := len(a.Times)
		if nKeys == 0 {
			continue
		}
		if len(a.Values) < nKeys*w {
			// Malformed — skip rather than panic.
			continue
		}

		keys := make([]motion.Key, nKeys)
		for i := 0; i < nKeys; i++ {
			offset := i * w
			var f [4]float64
			for j := 0; j < w; j++ {
				f[j] = a.Values[offset+j]
			}
			keys[i] = motion.Key{
				T:     a.Times[i],
				Value: motion.Value{Arity: arity, F: f},
			}
		}

		out = append(out, motion.Track{
			Target: motion.Target{
				Kind: motion.TargetMaterial,
				Ref:  ref,
			},
			Prop:   uniform,
			Keys:   keys,
			Interp: interpFromString(a.Interp),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// materialSpringTrack lowers a MaterialUniformAnim with a Spring spec into a
// GenSpring generator Track on a SCALAR uniform. The generator emits ArityScalar
// (Spring.Value(from, to, t)); Base.F = [from, to]. Zero spring constants fall
// back to motion.Spring defaults at eval time. Returns ok=false if the spec is
// nil (caller-guarded) — there is nothing else to validate, a scalar spring is
// always well-formed.
func materialSpringTrack(a MaterialUniformAnim, uniform, ref string) (motion.Track, bool) {
	s := a.Spring
	if s == nil {
		return motion.Track{}, false
	}
	return motion.Track{
		Target: motion.Target{
			Kind: motion.TargetMaterial,
			Ref:  ref,
		},
		Prop: uniform,
		Gen: &motion.Generator{
			Kind: motion.GenSpring,
			// Base.F = [from, to, ...] — width-2 (ArityVec2) satisfies the
			// evaluator guard (needs >= 2); the generator emits ArityScalar.
			Base: motion.Value{
				Arity: motion.ArityVec2,
				F:     [4]float64{s.From, s.To, 0, 0},
			},
			Spring: motion.Spring{
				Mass:      s.Mass,
				Stiffness: s.Stiffness,
				Damping:   s.Damping,
				Velocity:  s.Velocity,
			},
		},
	}, true
}

// materialOscillatorTrack lowers a MaterialUniformAnim with an Oscillator spec
// into a GenOscillator generator Track at the uniform's arity. Per-component
// arrays are copied into fixed [4]float64 slots (missing components default to
// 0). Returns ok=false for a malformed arity (not 1/3/4) so the entry is skipped.
func materialOscillatorTrack(a MaterialUniformAnim, uniform, ref string) (motion.Track, bool) {
	o := a.Oscillator
	if o == nil {
		return motion.Track{}, false
	}
	arity, w := materialArity(a.Arity, uniform)
	if w == 0 {
		return motion.Track{}, false
	}

	var base, amp, freq, phase [4]float64
	copyComponents(&base, o.Base, w)
	copyComponents(&amp, o.Amplitude, w)
	copyComponents(&freq, o.Freq, w)
	copyComponents(&phase, o.Phase, w)

	return motion.Track{
		Target: motion.Target{
			Kind: motion.TargetMaterial,
			Ref:  ref,
		},
		Prop: uniform,
		Gen: &motion.Generator{
			Kind:     motion.GenOscillator,
			OscArity: arity,
			OscBase:  base,
			OscAmp:   amp,
			OscFreq:  freq,
			OscPhase: phase,
		},
	}, true
}

// copyComponents copies up to w (and up to 4) leading values from src into dst.
// Missing/extra source entries are ignored (dst stays zero past len(src)).
func copyComponents(dst *[4]float64, src []float64, w int) {
	n := w
	if n > 4 {
		n = 4
	}
	if n > len(src) {
		n = len(src)
	}
	for i := 0; i < n; i++ {
		dst[i] = src[i]
	}
}

// MaterialMotionTimeline returns a scene-level motion.Timeline wrapping every
// accumulated material-uniform Track at absolute time 0. Returns nil when there
// are no material tracks. The returned timeline is NOT wire-prepared; the caller
// runs PrepareTracks with fresh interners before encoding.
func (ir SceneIR) MaterialMotionTimeline() *motion.Timeline {
	if len(ir.MaterialTracks) == 0 {
		return nil
	}
	children := make([]motion.Positioned, len(ir.MaterialTracks))
	for i := range ir.MaterialTracks {
		children[i] = motion.Positioned{
			At:    motion.Position{Kind: motion.PosAbs, Val: 0},
			Track: &ir.MaterialTracks[i],
		}
	}
	return &motion.Timeline{
		Children: children,
		Loop:     -1, // infinite; runtime player honours per-anim Loop hints
		Autoplay: true,
	}
}
