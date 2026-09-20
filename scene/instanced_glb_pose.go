package scene

import (
	"encoding/binary"
	"math"
	"reflect"
	"strconv"
	"strings"
)

const (
	// InstancedGLBPoseFrameMagic is ASCII "GIP1" read as a little-endian uint32.
	InstancedGLBPoseFrameMagic       uint32 = 0x31504947
	InstancedGLBPoseFrameVersion            = 1
	InstancedGLBPoseFrameRowFloats          = 10
	InstancedGLBPoseFrameHeaderBytes        = 24
	maxJavaScriptSafeInteger         uint64 = 1<<53 - 1
)

// PackInstancedGLBPoseFrame encodes one stable-membership pose update into dst.
// It returns ok=false unless previous and next have the same normalized batch
// and instance layout, identical non-pose declarations, identical animation
// clip/loop modes, and no parent matrices. Callers must use the full Scene3D
// command path when packing is refused. As on the full command path, batches
// with an empty normalized source or no instances are omitted from the packed
// layout; fallback batch IDs still use their original declaration indexes.
//
// Both declarations are immutable snapshots for the duration of the call.
// In particular, callers must not reuse and mutate maps, pointers, or instance
// slices shared by previous and next; doing so destroys the old declaration
// needed to prove that the packed update changes poses only.
//
// Wire v1 is little-endian:
//
//	magic u32, version u16, rowFloats u16, layoutHash u32, rowCount u32,
//	baseDeclarationRevision u64, then rowCount rows of ten float32 values:
//	x, y, z, rotationX, rotationY, rotationZ, scaleX, scaleY, scaleZ,
//	animationTime.
func PackInstancedGLBPoseFrame(dst []byte, baseRevision uint64, previous, next []InstancedGLBMeshIR) ([]byte, bool) {
	if baseRevision == 0 || baseRevision > maxJavaScriptSafeInteger || len(previous) == 0 || len(previous) != len(next) {
		return dst[:0], false
	}
	rowCount := 0
	for batchIndex := range previous {
		left, right := previous[batchIndex], next[batchIndex]
		if !instancedGLBPoseBatchCompatible(left, right, batchIndex) {
			return dst[:0], false
		}
		if !instancedGLBPoseBatchIncluded(right) {
			continue
		}
		if uint64(rowCount)+uint64(len(left.Instances)) > uint64(^uint32(0)) {
			return dst[:0], false
		}
		rowCount += len(left.Instances)
		for instanceIndex := range left.Instances {
			if !instancedGLBPoseInstanceCompatible(left.Instances[instanceIndex], right.Instances[instanceIndex], instanceIndex) {
				return dst[:0], false
			}
		}
	}
	if rowCount == 0 {
		return dst[:0], false
	}
	maxInt := int(^uint(0) >> 1)
	if rowCount > (maxInt-InstancedGLBPoseFrameHeaderBytes)/(InstancedGLBPoseFrameRowFloats*4) {
		return dst[:0], false
	}
	frameBytes := InstancedGLBPoseFrameHeaderBytes + rowCount*InstancedGLBPoseFrameRowFloats*4
	if cap(dst) < frameBytes {
		dst = make([]byte, frameBytes)
	} else {
		dst = dst[:frameBytes]
	}
	binary.LittleEndian.PutUint32(dst[0:4], InstancedGLBPoseFrameMagic)
	binary.LittleEndian.PutUint16(dst[4:6], InstancedGLBPoseFrameVersion)
	binary.LittleEndian.PutUint16(dst[6:8], InstancedGLBPoseFrameRowFloats)
	binary.LittleEndian.PutUint32(dst[8:12], instancedGLBPoseLayoutHash(next))
	binary.LittleEndian.PutUint32(dst[12:16], uint32(rowCount))
	binary.LittleEndian.PutUint64(dst[16:24], baseRevision)

	offset := InstancedGLBPoseFrameHeaderBytes
	for batchIndex := range next {
		if !instancedGLBPoseBatchIncluded(next[batchIndex]) {
			continue
		}
		for instanceIndex := range next[batchIndex].Instances {
			values, ok := instancedGLBPoseValues(next[batchIndex].Instances[instanceIndex])
			if !ok {
				return dst[:0], false
			}
			for _, value := range values {
				binary.LittleEndian.PutUint32(dst[offset:offset+4], math.Float32bits(value))
				offset += 4
			}
		}
	}
	return dst, true
}

func instancedGLBPoseBatchIncluded(batch InstancedGLBMeshIR) bool {
	return strings.TrimSpace(batch.Src) != "" && len(batch.Instances) > 0
}

func instancedGLBPoseBatchCompatible(left, right InstancedGLBMeshIR, index int) bool {
	if normalizedInstancedGLBBatchID(left.ID, index) != normalizedInstancedGLBBatchID(right.ID, index) ||
		strings.TrimSpace(left.Src) != strings.TrimSpace(right.Src) || len(left.Instances) != len(right.Instances) ||
		left.MaterialKind != right.MaterialKind || left.Color != right.Color || left.Texture != right.Texture ||
		left.BlendMode != right.BlendMode || left.CustomVertex != right.CustomVertex ||
		left.CustomFragment != right.CustomFragment || left.CustomVertexWGSL != right.CustomVertexWGSL ||
		left.CustomFragmentWGSL != right.CustomFragmentWGSL || left.CustomVertexRef != right.CustomVertexRef ||
		left.CustomFragmentRef != right.CustomFragmentRef || left.CustomVertexWGSLRef != right.CustomVertexWGSLRef ||
		left.CustomFragmentWGSLRef != right.CustomFragmentWGSLRef || left.ShaderBackend != right.ShaderBackend ||
		left.ShaderSource != right.ShaderSource || left.SharedAppearance != right.SharedAppearance ||
		!instancedGLBPoseFloat64PointerEqual(left.Opacity, right.Opacity) ||
		!instancedGLBPoseFloat64PointerEqual(left.Emissive, right.Emissive) ||
		!instancedGLBPoseAlphaCutoffEqual(left.AlphaCutoff, right.AlphaCutoff) ||
		!instancedGLBPoseFloat64Equal(left.Roughness, right.Roughness) ||
		!instancedGLBPoseFloat64Equal(left.Metalness, right.Metalness) ||
		!instancedGLBPoseFloat64PointerEqual(left.SpecularIntensity, right.SpecularIntensity) ||
		!instancedGLBPoseFloat64Array3PointerEqual(left.SpecularColor, right.SpecularColor) ||
		!instancedGLBPoseFloat64PointerEqual(left.IOR, right.IOR) ||
		!instancedGLBPoseJSONMapEqual(left.CustomUniforms, right.CustomUniforms) ||
		!instancedGLBPoseJSONMapEqual(left.ShaderLayout, right.ShaderLayout) ||
		!instancedGLBPoseStringMapEqual(left.ShaderSourceFiles, right.ShaderSourceFiles) ||
		!instancedGLBPoseBoolPointerEqual(left.Pickable, right.Pickable) ||
		!instancedGLBPoseBoolPointerEqual(left.Visible, right.Visible) ||
		!instancedGLBPoseBoolPointerEqual(left.Static, right.Static) {
		return false
	}
	return true
}

func instancedGLBPoseFloat64Equal(left, right float64) bool {
	return !math.IsNaN(left) && !math.IsNaN(right) && !math.IsInf(left, 0) && !math.IsInf(right, 0) &&
		math.Float64bits(left) == math.Float64bits(right)
}

func instancedGLBPoseFloat64PointerEqual(left, right *float64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return instancedGLBPoseFloat64Equal(*left, *right)
}

func instancedGLBPoseFloat64Array3PointerEqual(left, right *[3]float64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	for index := range left {
		if !instancedGLBPoseFloat64Equal(left[index], right[index]) {
			return false
		}
	}
	return true
}

func instancedGLBPoseAlphaCutoffEqual(left, right AlphaCutoff) bool {
	return left.state == right.state && instancedGLBPoseFloat64Equal(left.value, right.value)
}

func instancedGLBPoseBoolPointerEqual(left, right *bool) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func instancedGLBPoseJSONMapEqual(left, right map[string]any) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return provenJSONEqual(reflect.ValueOf(left), reflect.ValueOf(right))
}

func instancedGLBPoseStringMapEqual(left, right map[string]string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return provenJSONEqual(reflect.ValueOf(left), reflect.ValueOf(right))
}

func instancedGLBPoseInstanceCompatible(left, right MeshInstanceIR, index int) bool {
	if normalizedInstancedGLBInstanceID(left.ID, index) != normalizedInstancedGLBInstanceID(right.ID, index) ||
		strings.TrimSpace(left.Animation) != strings.TrimSpace(right.Animation) ||
		left.AnimationLoop != right.AnimationLoop || len(left.ParentMatrix) != 0 || len(right.ParentMatrix) != 0 {
		return false
	}
	// JSON omission controls whether the normalized model has a crowd-pose
	// declaration at all. Animation time may vary only after that mode exists.
	return instancedGLBHasAnimationPose(left) == instancedGLBHasAnimationPose(right)
}

func instancedGLBHasAnimationPose(instance MeshInstanceIR) bool {
	return instance.Animation != "" || instance.AnimationTime != 0 || instance.AnimationLoop
}

func normalizedInstancedGLBBatchID(id string, index int) string {
	if id = strings.TrimSpace(id); id != "" {
		return id
	}
	return "scene-instanced-glb-" + strconv.Itoa(index)
}

func normalizedInstancedGLBInstanceID(id string, index int) string {
	if id = strings.TrimSpace(id); id != "" {
		return id
	}
	return "instance-" + strconv.Itoa(index)
}

func instancedGLBPoseValues(instance MeshInstanceIR) ([InstancedGLBPoseFrameRowFloats]float32, bool) {
	var packed [InstancedGLBPoseFrameRowFloats]float32
	if math.IsNaN(instance.AnimationTime) || math.IsInf(instance.AnimationTime, 0) {
		return packed, false
	}
	values := [InstancedGLBPoseFrameRowFloats]float64{
		instance.X, instance.Y, instance.Z,
		instance.RotationX, instance.RotationY, instance.RotationZ,
		resolvedInstancedGLBScale(instance.ScaleX),
		resolvedInstancedGLBScale(instance.ScaleY),
		resolvedInstancedGLBScale(instance.ScaleZ),
		math.Max(0, instance.AnimationTime),
	}
	for index, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return packed, false
		}
		converted := float32(value)
		if math.IsNaN(float64(converted)) || math.IsInf(float64(converted), 0) {
			return packed, false
		}
		packed[index] = converted
	}
	return packed, true
}

func resolvedInstancedGLBScale(value float64) float64 {
	if value == 0 {
		return 1
	}
	return value
}

func instancedGLBPoseLayoutHash(batches []InstancedGLBMeshIR) uint32 {
	hash := uint32(2166136261)
	appendUint32 := func(value uint32) {
		for shift := uint(0); shift < 32; shift += 8 {
			hash ^= uint32(byte(value >> shift))
			hash *= 16777619
		}
	}
	appendString := func(value string) {
		appendUint32(uint32(len(value)))
		for index := 0; index < len(value); index++ {
			hash ^= uint32(value[index])
			hash *= 16777619
		}
	}
	included := uint32(0)
	for batchIndex := range batches {
		if instancedGLBPoseBatchIncluded(batches[batchIndex]) {
			included++
		}
	}
	appendUint32(included)
	for batchIndex := range batches {
		batch := batches[batchIndex]
		if !instancedGLBPoseBatchIncluded(batch) {
			continue
		}
		appendString(normalizedInstancedGLBBatchID(batch.ID, batchIndex))
		appendUint32(uint32(len(batch.Instances)))
		for instanceIndex := range batch.Instances {
			appendString(normalizedInstancedGLBInstanceID(batch.Instances[instanceIndex].ID, instanceIndex))
		}
	}
	return hash
}
