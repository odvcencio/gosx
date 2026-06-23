package bundle

import (
	"math"
	"strconv"
	"strings"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/motion"
)

type nativeAnimationState struct {
	matrix       mat4
	hasMatrix    bool
	translation  [3]float32
	hasTranslate bool
	rotation     [3]float32
	hasRotation  bool
	// rotationQuat carries a slerped orientation (x,y,z,w) when useQuat is set.
	// It supersedes the per-axis Euler `rotation` field for the shared-Times
	// slerp path; nativeAnimationMatrix branches on useQuat.
	rotationQuat [4]float32
	useQuat      bool
	scale        [3]float32
	hasScale     bool
}

func applyNativeAnimations(b engine.RenderBundle, timeSeconds float64) engine.RenderBundle {
	if len(b.Animations) == 0 || len(b.InstancedMeshes) == 0 {
		return b
	}
	animated := make(map[int]mat4)
	for _, clip := range b.Animations {
		targets := sampleNativeAnimationClip(clip, timeSeconds)
		if len(targets) == 0 {
			continue
		}
		for index, mesh := range b.InstancedMeshes {
			state, ok := animationStateForMesh(targets, index, mesh.ID)
			if !ok {
				continue
			}
			m := nativeAnimationMatrix(state)
			if prev, ok := animated[index]; ok {
				animated[index] = mat4Mul(prev, m)
			} else {
				animated[index] = m
			}
		}
	}
	if len(animated) == 0 {
		return b
	}
	meshes := append([]engine.RenderInstancedMesh(nil), b.InstancedMeshes...)
	for index, m := range animated {
		if index < 0 || index >= len(meshes) {
			continue
		}
		mesh := meshes[index]
		if mesh.InstanceCount <= 0 || len(mesh.Transforms) == 0 {
			continue
		}
		mesh.Transforms = applyAnimationMatrixToTransforms(mesh.Transforms, mesh.InstanceCount, m)
		meshes[index] = mesh
	}
	b.InstancedMeshes = meshes
	return b
}

func sampleNativeAnimationClip(clip engine.RenderAnimation, timeSeconds float64) map[string]nativeAnimationState {
	sampleTime := timeSeconds
	if clip.Duration > 0 {
		sampleTime = math.Mod(timeSeconds, clip.Duration)
		if sampleTime < 0 {
			sampleTime += clip.Duration
		}
	}
	out := make(map[string]nativeAnimationState)
	// Group the rotationX/Y/Z channels per target so we can interpolate
	// orientation as a quaternion (slerp) when they share a common Times axis,
	// instead of lerping each Euler scalar independently.
	rotationByTarget := make(map[string]map[string]engine.RenderAnimationChannel)
	for _, ch := range clip.Channels {
		targetID := strings.TrimSpace(ch.TargetID)
		if targetID == "" {
			continue
		}
		switch normalizeNativeAnimationProperty(ch.Property) {
		case "rotationx", "rotationy", "rotationz":
			axes := rotationByTarget[targetID]
			if axes == nil {
				axes = make(map[string]engine.RenderAnimationChannel)
				rotationByTarget[targetID] = axes
			}
			axes[normalizeNativeAnimationProperty(ch.Property)] = ch
			continue
		}
		values, ok := sampleNativeAnimationChannel(ch, sampleTime)
		if !ok {
			continue
		}
		state := out[targetID]
		applyNativeAnimationChannel(&state, ch.Property, values)
		out[targetID] = state
	}
	for targetID, axes := range rotationByTarget {
		state := out[targetID]
		applyNativeRotationAxes(&state, axes, sampleTime)
		out[targetID] = state
	}
	return out
}

// applyNativeRotationAxes interpolates a target's rotation. When the supplied
// rotationX/Y/Z axis channels share an identical Times array, the orientation
// is interpolated as a quaternion slerp between the surrounding keyframes
// (constant-velocity geodesic). Otherwise — mismatched/absent Times — it falls
// back to the legacy per-axis Euler scalar lerp so edge cases stay correct and
// the blast radius stays bounded.
func applyNativeRotationAxes(state *nativeAnimationState, axes map[string]engine.RenderAnimationChannel, sampleTime float64) {
	if state == nil || len(axes) == 0 {
		return
	}
	if times, ok := sharedRotationTimes(axes); ok {
		lo, hi, alpha := surroundingFrame(times, sampleTime)
		eulerLo := eulerAt(axes, lo)
		eulerHi := eulerAt(axes, hi)
		stepInterp := allStepRotation(axes)
		if stepInterp || lo == hi {
			alpha = 0
		}
		qLo := motion.QuatFromEuler(eulerLo[0], eulerLo[1], eulerLo[2])
		var q motion.Quat
		if alpha == 0 {
			q = qLo
		} else {
			qHi := motion.QuatFromEuler(eulerHi[0], eulerHi[1], eulerHi[2])
			q = motion.Slerp(qLo, qHi, alpha)
		}
		qn := q.Normalize()
		state.rotationQuat = [4]float32{float32(qn.X), float32(qn.Y), float32(qn.Z), float32(qn.W)}
		state.useQuat = true
		state.hasRotation = true
		return
	}
	// Fallback: per-axis Euler scalar lerp (legacy behavior).
	for prop, ch := range axes {
		values, ok := sampleNativeAnimationChannel(ch, sampleTime)
		if !ok {
			continue
		}
		applyNativeAnimationChannel(state, prop, values)
	}
}

// sharedRotationTimes returns the common Times array if every provided rotation
// axis channel uses an identical (same length, same values) Times slice and has
// enough values to sample. Otherwise ok is false and the caller must fall back.
func sharedRotationTimes(axes map[string]engine.RenderAnimationChannel) (times []float64, ok bool) {
	var ref []float64
	for _, ch := range axes {
		if len(ch.Times) == 0 || len(ch.Values) < len(ch.Times) {
			return nil, false
		}
		if ref == nil {
			ref = ch.Times
			continue
		}
		if len(ch.Times) != len(ref) {
			return nil, false
		}
		for i := range ch.Times {
			if ch.Times[i] != ref[i] {
				return nil, false
			}
		}
	}
	if len(ref) == 0 {
		return nil, false
	}
	return ref, true
}

// surroundingFrame locates the keyframe indices bracketing sampleTime within a
// shared Times array, plus the [0,1] interpolation factor. Clamps at both ends.
func surroundingFrame(times []float64, sampleTime float64) (lo, hi int, alpha float64) {
	last := len(times) - 1
	if last <= 0 || sampleTime <= times[0] {
		return 0, 0, 0
	}
	if sampleTime >= times[last] {
		return last, last, 0
	}
	hi = 1
	for hi < len(times) && times[hi] < sampleTime {
		hi++
	}
	lo = hi - 1
	t0, t1 := times[lo], times[hi]
	if t1 > t0 {
		alpha = (sampleTime - t0) / (t1 - t0)
	}
	return lo, hi, alpha
}

// eulerAt reads the (x,y,z) Euler triple at keyframe index frame from the axis
// channels. Missing axes contribute 0. Each rotation axis channel is stride-1.
func eulerAt(axes map[string]engine.RenderAnimationChannel, frame int) [3]float64 {
	var out [3]float64
	axisIndex := map[string]int{"rotationx": 0, "rotationy": 1, "rotationz": 2}
	for prop, ch := range axes {
		idx, ok := axisIndex[prop]
		if !ok {
			continue
		}
		if frame >= 0 && frame < len(ch.Values) {
			out[idx] = ch.Values[frame]
		}
	}
	return out
}

// allStepRotation reports whether every rotation axis channel uses STEP
// interpolation, in which case the orientation holds at the low keyframe.
func allStepRotation(axes map[string]engine.RenderAnimationChannel) bool {
	for _, ch := range axes {
		if !strings.EqualFold(ch.Interpolation, "STEP") {
			return false
		}
	}
	return true
}

func sampleNativeAnimationChannel(ch engine.RenderAnimationChannel, timeSeconds float64) ([]float32, bool) {
	stride := nativeAnimationStride(ch)
	if stride <= 0 || len(ch.Times) == 0 || len(ch.Values) < stride {
		return nil, false
	}
	frameCount := min(len(ch.Times), len(ch.Values)/stride)
	if frameCount <= 0 {
		return nil, false
	}
	out := make([]float32, stride)
	if frameCount == 1 || timeSeconds <= ch.Times[0] {
		copyNativeAnimationFrame(out, ch.Values, 0, stride)
		return out, true
	}
	last := frameCount - 1
	if timeSeconds >= ch.Times[last] {
		copyNativeAnimationFrame(out, ch.Values, last, stride)
		return out, true
	}
	hi := 1
	for hi < frameCount && ch.Times[hi] < timeSeconds {
		hi++
	}
	lo := hi - 1
	if strings.EqualFold(ch.Interpolation, "STEP") {
		copyNativeAnimationFrame(out, ch.Values, lo, stride)
		return out, true
	}
	t0, t1 := ch.Times[lo], ch.Times[hi]
	alpha := 0.0
	if t1 > t0 {
		alpha = (timeSeconds - t0) / (t1 - t0)
	}
	for i := 0; i < stride; i++ {
		a := ch.Values[lo*stride+i]
		b := ch.Values[hi*stride+i]
		out[i] = float32(a + (b-a)*alpha)
	}
	return out, true
}

func nativeAnimationStride(ch engine.RenderAnimationChannel) int {
	property := normalizeNativeAnimationProperty(ch.Property)
	switch property {
	case "translation":
		return 3
	case "rotationx", "rotationy", "rotationz", "scalex", "scaley", "scalez":
		return 1
	case "scale":
		frameCount := max(1, len(ch.Times))
		if len(ch.Values)/frameCount >= 3 {
			return 3
		}
		return 1
	case "matrix":
		return 16
	default:
		return 0
	}
}

func normalizeNativeAnimationProperty(property string) string {
	switch strings.ToLower(strings.TrimSpace(property)) {
	case "translation", "translate", "position":
		return "translation"
	case "rotationx", "rotatex":
		return "rotationx"
	case "rotationy", "rotatey":
		return "rotationy"
	case "rotationz", "rotatez":
		return "rotationz"
	case "scale", "scalex", "scaley", "scalez":
		return strings.ToLower(strings.TrimSpace(property))
	case "matrix", "transform":
		return "matrix"
	default:
		return strings.ToLower(strings.TrimSpace(property))
	}
}

func copyNativeAnimationFrame(dst []float32, values []float64, frame, stride int) {
	for i := 0; i < stride; i++ {
		dst[i] = float32(values[frame*stride+i])
	}
}

func applyNativeAnimationChannel(state *nativeAnimationState, property string, values []float32) {
	if state == nil || len(values) == 0 {
		return
	}
	switch normalizeNativeAnimationProperty(property) {
	case "translation":
		if len(values) >= 3 {
			copy(state.translation[:], values[:3])
			state.hasTranslate = true
		}
	case "rotationx":
		state.rotation[0] = values[0]
		state.hasRotation = true
	case "rotationy":
		state.rotation[1] = values[0]
		state.hasRotation = true
	case "rotationz":
		state.rotation[2] = values[0]
		state.hasRotation = true
	case "scale":
		if len(values) >= 3 {
			copy(state.scale[:], values[:3])
		} else {
			state.scale = [3]float32{values[0], values[0], values[0]}
		}
		state.hasScale = true
	case "scalex":
		if !state.hasScale {
			state.scale = [3]float32{1, 1, 1}
		}
		state.scale[0] = values[0]
		state.hasScale = true
	case "scaley":
		if !state.hasScale {
			state.scale = [3]float32{1, 1, 1}
		}
		state.scale[1] = values[0]
		state.hasScale = true
	case "scalez":
		if !state.hasScale {
			state.scale = [3]float32{1, 1, 1}
		}
		state.scale[2] = values[0]
		state.hasScale = true
	case "matrix":
		if len(values) >= 16 {
			for i := 0; i < 16; i++ {
				state.matrix[i] = values[i]
			}
			state.hasMatrix = true
		}
	}
}

func animationStateForMesh(targets map[string]nativeAnimationState, index int, id string) (nativeAnimationState, bool) {
	if strings.TrimSpace(id) != "" {
		if state, ok := targets[strings.TrimSpace(id)]; ok {
			return state, true
		}
	}
	if state, ok := targets[strconv.Itoa(index)]; ok {
		return state, true
	}
	if state, ok := targets[strconv.Itoa(index+1)]; ok {
		return state, true
	}
	return nativeAnimationState{}, false
}

func nativeAnimationMatrix(state nativeAnimationState) mat4 {
	if state.hasMatrix {
		return state.matrix
	}
	m := mat4Identity()
	if state.hasTranslate {
		m = mat4Mul(m, mat4Translate(state.translation[0], state.translation[1], state.translation[2]))
	}
	if state.hasRotation {
		if state.useQuat {
			q := motion.Quat{
				X: float64(state.rotationQuat[0]),
				Y: float64(state.rotationQuat[1]),
				Z: float64(state.rotationQuat[2]),
				W: float64(state.rotationQuat[3]),
			}
			m = mat4Mul(m, mat4FromQuat(q))
		} else {
			m = mat4Mul(m, mat4RotateX(state.rotation[0]))
			m = mat4Mul(m, mat4RotateY(state.rotation[1]))
			m = mat4Mul(m, mat4RotateZ(state.rotation[2]))
		}
	}
	if state.hasScale {
		m = mat4Mul(m, mat4Scale(state.scale[0], state.scale[1], state.scale[2]))
	}
	return m
}

func mat4RotateZ(a float32) mat4 {
	c := float32(math.Cos(float64(a)))
	s := float32(math.Sin(float64(a)))
	m := mat4Identity()
	m[0], m[1], m[4], m[5] = c, s, -s, c
	return m
}

func mat4Scale(x, y, z float32) mat4 {
	m := mat4Identity()
	m[0], m[5], m[10] = x, y, z
	return m
}

func applyAnimationMatrixToTransforms(transforms []float64, instanceCount int, animation mat4) []float64 {
	out := append([]float64(nil), transforms...)
	for instance := 0; instance < instanceCount; instance++ {
		offset := instance * 16
		if offset+16 > len(out) {
			break
		}
		base := mat4FromFloat64(out[offset : offset+16])
		animated := mat4Mul(animation, base)
		for i := 0; i < 16; i++ {
			out[offset+i] = float64(animated[i])
		}
	}
	return out
}

func mat4FromFloat64(values []float64) mat4 {
	m := mat4Identity()
	for i := 0; i < len(values) && i < 16; i++ {
		m[i] = float32(values[i])
	}
	return m
}
