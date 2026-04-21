package anim

import (
	"fmt"
	"math"
	"strings"

	"github.com/odvcencio/gosx/engine"
)

// Joint describes one skeleton joint in palette order.
type Joint struct {
	ID          string
	Parent      int
	Translation [3]float32
	Rotation    [4]float32
	Scale       [3]float32
	InverseBind [16]float32
}

type jointPose struct {
	translation [3]float32
	rotation    [4]float32
	scale       [3]float32
	matrix      [16]float32
	hasMatrix   bool
}

// EvaluateBonePalette samples clip at timeSeconds and returns column-major
// mat4 values ready for bundle.Renderer.UploadBonePalette. If joints is
// empty, a flat skeleton is derived from the clip's target IDs in first-seen
// order.
func EvaluateBonePalette(clip engine.RenderAnimation, timeSeconds float64, joints []Joint) ([]float32, error) {
	if len(joints) == 0 {
		joints = jointsFromClip(clip)
	}
	if len(joints) == 0 {
		return nil, nil
	}

	index := make(map[string]int, len(joints))
	for i, joint := range joints {
		id := strings.TrimSpace(joint.ID)
		if id == "" {
			return nil, fmt.Errorf("anim.EvaluateBonePalette: joint %d has empty ID", i)
		}
		if _, exists := index[id]; exists {
			return nil, fmt.Errorf("anim.EvaluateBonePalette: duplicate joint ID %q", id)
		}
		index[id] = i
	}

	poses := make([]jointPose, len(joints))
	for i, joint := range joints {
		poses[i] = basePose(joint)
	}
	for _, ch := range clip.Channels {
		target, ok := index[strings.TrimSpace(ch.TargetID)]
		if !ok {
			continue
		}
		applyChannel(&poses[target], ch, timeSeconds)
	}

	globals := make([][16]float32, len(joints))
	out := make([]float32, len(joints)*16)
	for i, joint := range joints {
		local := poseMatrix(poses[i])
		if joint.Parent >= 0 {
			if joint.Parent >= i {
				return nil, fmt.Errorf("anim.EvaluateBonePalette: joint %q parent %d must precede child %d", joint.ID, joint.Parent, i)
			}
			globals[i] = mulMat4(globals[joint.Parent], local)
		} else {
			globals[i] = local
		}
		palette := mulMat4(globals[i], matrixOrIdentity(joint.InverseBind))
		copy(out[i*16:(i+1)*16], palette[:])
	}
	return out, nil
}

func jointsFromClip(clip engine.RenderAnimation) []Joint {
	seen := map[string]struct{}{}
	var joints []Joint
	for _, ch := range clip.Channels {
		id := strings.TrimSpace(ch.TargetID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		joints = append(joints, Joint{ID: id, Parent: -1, Rotation: [4]float32{0, 0, 0, 1}, Scale: [3]float32{1, 1, 1}})
	}
	return joints
}

func basePose(joint Joint) jointPose {
	pose := jointPose{
		translation: joint.Translation,
		rotation:    joint.Rotation,
		scale:       joint.Scale,
	}
	if pose.rotation == ([4]float32{}) {
		pose.rotation = [4]float32{0, 0, 0, 1}
	}
	if pose.scale == ([3]float32{}) {
		pose.scale = [3]float32{1, 1, 1}
	}
	return pose
}

func applyChannel(pose *jointPose, ch engine.RenderAnimationChannel, timeSeconds float64) {
	stride := propertyStride(ch.Property)
	if pose == nil || stride == 0 || len(ch.Times) == 0 || len(ch.Values) < stride {
		return
	}
	values := sampleChannel(ch, stride, timeSeconds)
	switch normalizeProperty(ch.Property) {
	case "translation":
		copy(pose.translation[:], values[:3])
	case "rotation":
		copy(pose.rotation[:], values[:4])
		pose.rotation = normalizeQuat(pose.rotation)
	case "scale":
		copy(pose.scale[:], values[:3])
	case "matrix":
		copy(pose.matrix[:], values[:16])
		pose.hasMatrix = true
	}
}

func sampleChannel(ch engine.RenderAnimationChannel, stride int, timeSeconds float64) []float32 {
	frameCount := min(len(ch.Times), len(ch.Values)/stride)
	out := make([]float32, stride)
	if frameCount <= 0 {
		return out
	}
	if frameCount == 1 || timeSeconds <= ch.Times[0] {
		copyFrame(out, ch.Values, 0, stride)
		return out
	}
	last := frameCount - 1
	if timeSeconds >= ch.Times[last] {
		copyFrame(out, ch.Values, last, stride)
		return out
	}

	hi := 1
	for hi < frameCount && ch.Times[hi] < timeSeconds {
		hi++
	}
	lo := hi - 1
	if strings.EqualFold(ch.Interpolation, "STEP") {
		copyFrame(out, ch.Values, lo, stride)
		return out
	}
	t0, t1 := ch.Times[lo], ch.Times[hi]
	alpha := 0.0
	if t1 > t0 {
		alpha = (timeSeconds - t0) / (t1 - t0)
	}
	if normalizeProperty(ch.Property) == "rotation" && stride == 4 {
		q0 := frameQuat(ch.Values, lo)
		q1 := frameQuat(ch.Values, hi)
		q := nlerpQuat(q0, q1, float32(alpha))
		copy(out, q[:])
		return out
	}
	for i := 0; i < stride; i++ {
		a := ch.Values[lo*stride+i]
		b := ch.Values[hi*stride+i]
		out[i] = float32(a + (b-a)*alpha)
	}
	return out
}

func copyFrame(dst []float32, values []float64, frame, stride int) {
	for i := 0; i < stride; i++ {
		dst[i] = float32(values[frame*stride+i])
	}
}

func frameQuat(values []float64, frame int) [4]float32 {
	base := frame * 4
	return normalizeQuat([4]float32{
		float32(values[base+0]),
		float32(values[base+1]),
		float32(values[base+2]),
		float32(values[base+3]),
	})
}

func propertyStride(property string) int {
	switch normalizeProperty(property) {
	case "translation", "scale":
		return 3
	case "rotation":
		return 4
	case "matrix":
		return 16
	default:
		return 0
	}
}

func normalizeProperty(property string) string {
	switch strings.ToLower(strings.TrimSpace(property)) {
	case "translation", "translate", "position":
		return "translation"
	case "rotation", "quaternion":
		return "rotation"
	case "scale":
		return "scale"
	case "matrix", "transform":
		return "matrix"
	default:
		return strings.ToLower(strings.TrimSpace(property))
	}
}

func poseMatrix(pose jointPose) [16]float32 {
	if pose.hasMatrix {
		return matrixOrIdentity(pose.matrix)
	}
	return composeTRS(pose.translation, pose.rotation, pose.scale)
}

func composeTRS(t [3]float32, q [4]float32, s [3]float32) [16]float32 {
	q = normalizeQuat(q)
	x, y, z, w := q[0], q[1], q[2], q[3]
	xx, yy, zz := x*x, y*y, z*z
	xy, xz, yz := x*y, x*z, y*z
	wx, wy, wz := w*x, w*y, w*z
	m := identity()
	m[0] = (1 - 2*(yy+zz)) * s[0]
	m[1] = (2 * (xy + wz)) * s[0]
	m[2] = (2 * (xz - wy)) * s[0]
	m[4] = (2 * (xy - wz)) * s[1]
	m[5] = (1 - 2*(xx+zz)) * s[1]
	m[6] = (2 * (yz + wx)) * s[1]
	m[8] = (2 * (xz + wy)) * s[2]
	m[9] = (2 * (yz - wx)) * s[2]
	m[10] = (1 - 2*(xx+yy)) * s[2]
	m[12], m[13], m[14] = t[0], t[1], t[2]
	return m
}

func mulMat4(a, b [16]float32) [16]float32 {
	var out [16]float32
	for col := 0; col < 4; col++ {
		for row := 0; row < 4; row++ {
			out[col*4+row] =
				a[0*4+row]*b[col*4+0] +
					a[1*4+row]*b[col*4+1] +
					a[2*4+row]*b[col*4+2] +
					a[3*4+row]*b[col*4+3]
		}
	}
	return out
}

func matrixOrIdentity(m [16]float32) [16]float32 {
	if m == ([16]float32{}) {
		return identity()
	}
	return m
}

func identity() [16]float32 {
	return [16]float32{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}

func normalizeQuat(q [4]float32) [4]float32 {
	if q == ([4]float32{}) {
		return [4]float32{0, 0, 0, 1}
	}
	l := float32(math.Sqrt(float64(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3])))
	if l < 1e-6 {
		return [4]float32{0, 0, 0, 1}
	}
	return [4]float32{q[0] / l, q[1] / l, q[2] / l, q[3] / l}
}

func nlerpQuat(a, b [4]float32, t float32) [4]float32 {
	if dotQuat(a, b) < 0 {
		b = [4]float32{-b[0], -b[1], -b[2], -b[3]}
	}
	return normalizeQuat([4]float32{
		a[0] + (b[0]-a[0])*t,
		a[1] + (b[1]-a[1])*t,
		a[2] + (b[2]-a[2])*t,
		a[3] + (b[3]-a[3])*t,
	})
}

func dotQuat(a, b [4]float32) float32 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] + a[3]*b[3]
}
