package bundle

import (
	"fmt"

	"github.com/odvcencio/gosx/render/gpu"
)

// # GPU skinning scaffold (R3.5)
//
// This file lays down the architecture for hardware-accelerated skeletal
// animation — the per-frame vertex transforms read a bone-matrix palette
// from a storage buffer, blend up to 4 weighted bones per vertex, and
// feed the result into the existing lit pipeline.
//
// What's in this commit:
//   - The WGSL snippet that consumes joint indices + weights and blends
//     them into a world-space position + normal (applySkinning, below).
//   - A BonePalette type tracking the max-palette-size constant + byte
//     stride used by both the CPU clip evaluator and the vertex shader
//     bindings.
//   - An UploadBonePalette helper that pushes a flat []float32 of
//     column-major mat4 values into a pre-allocated storage buffer.
//
// What's intentionally deferred to R5:
//   - A skinned variant of the lit pipeline. When that lands it'll build a
//     second RenderPipeline with the 3 extra vertex slots
//     (joints, weights, bind-pose transform) — buildLitPipeline's layout
//     stays untouched.
//   - Per-skeleton scratch space, instancing, morph targets. All slot
//     naturally onto the pattern below.
//
// The shader snippet and palette helpers are callable from outside the
// package so a sibling animator (possibly in render/bundle/anim/) can
// drive the pipeline without yanking internals.

// MaxBonesPerPalette caps one skeleton at 128 bones. That's generous for
// any humanoid rig and small enough to keep the palette binding under
// 8 KB of uniform/storage space, well within WebGPU's default limits.
const MaxBonesPerPalette = 128

// BonePaletteSize is the byte stride of a single palette slot (one mat4).
const BonePaletteSize = 64

// skinningWGSL is a drop-in WGSL function callable from a skinned vertex
// shader. It reads the bound bone palette, blends up to 4 weighted
// transforms, and returns the skinned world-space position + normal.
//
// Expected bindings (declared by the skinned pipeline when it lands):
//
//	@group(2) @binding(0) var<storage, read> bonePalette : array<mat4x4<f32>>;
//
// Expected vertex attributes (in addition to the lit pipeline's):
//
//	@location(N+0) joints  : vec4<u32>
//	@location(N+1) weights : vec4<f32>
//
// The function blends joints[j] transforms weighted by weights[j]. Callers
// should normalize weights on export so the sum is ~1.0 per vertex.
const skinningWGSL = `
// Skinning helper: blend up to 4 weighted bone transforms from the palette.
// Call from a skinned @vertex entry; bindings must be wired per pipeline.

@group(2) @binding(0) var<storage, read> bonePalette : array<mat4x4<f32>>;

fn applySkinning(localPos : vec3<f32>, joints : vec4<u32>, weights : vec4<f32>) -> vec4<f32> {
  var skinned = vec4<f32>(0.0);
  let base = vec4<f32>(localPos, 1.0);
  skinned = skinned + (bonePalette[joints.x] * base) * weights.x;
  skinned = skinned + (bonePalette[joints.y] * base) * weights.y;
  skinned = skinned + (bonePalette[joints.z] * base) * weights.z;
  skinned = skinned + (bonePalette[joints.w] * base) * weights.w;
  // If weights don't sum to 1 (sloppy export), fall back to the rigid pose
  // so the mesh doesn't collapse toward origin.
  let total = weights.x + weights.y + weights.z + weights.w;
  if (total < 1e-4) {
    return base;
  }
  return skinned;
}

fn applySkinningNormal(localNrm : vec3<f32>, joints : vec4<u32>, weights : vec4<f32>) -> vec3<f32> {
  var nSkinned = vec3<f32>(0.0);
  let base = vec4<f32>(localNrm, 0.0);
  nSkinned = nSkinned + (bonePalette[joints.x] * base).xyz * weights.x;
  nSkinned = nSkinned + (bonePalette[joints.y] * base).xyz * weights.y;
  nSkinned = nSkinned + (bonePalette[joints.z] * base).xyz * weights.z;
  nSkinned = nSkinned + (bonePalette[joints.w] * base).xyz * weights.w;
  let total = weights.x + weights.y + weights.z + weights.w;
  if (total < 1e-4) {
    return localNrm;
  }
  return normalize(nSkinned);
}
`

// SkinningSource returns the WGSL source for the skinning helper. Exposed
// so downstream packages that build skinned pipelines can concatenate this
// with their own vertex/fragment code.
func SkinningSource() string { return skinningWGSL }

// BonePalette describes a skeleton's bone-matrix storage buffer. One
// instance per active skeleton; uploaded each frame from the CPU-side
// clip evaluator via UploadBonePalette.
type BonePalette struct {
	// Capacity is the number of bones allocated in the storage buffer
	// (always ≤ MaxBonesPerPalette).
	Capacity int

	// Buffer is the backing storage buffer, usage = storage | copy_dst.
	Buffer gpu.Buffer
}

// CreateBonePalette allocates a storage buffer sized for capacity bones.
// A skinned pipeline will bind Buffer as @group(2) @binding(0).
func (r *Renderer) CreateBonePalette(capacity int) (*BonePalette, error) {
	if capacity <= 0 || capacity > MaxBonesPerPalette {
		return nil, fmt.Errorf("bundle.CreateBonePalette: capacity %d out of range (1..%d)",
			capacity, MaxBonesPerPalette)
	}
	buf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  capacity * BonePaletteSize,
		Usage: gpu.BufferUsageStorage | gpu.BufferUsageCopyDst,
		Label: fmt.Sprintf("bundle.bonePalette(%d)", capacity),
	})
	if err != nil {
		return nil, fmt.Errorf("bundle.CreateBonePalette: %w", err)
	}
	return &BonePalette{Capacity: capacity, Buffer: buf}, nil
}

// UploadBonePalette writes a fresh palette into the storage buffer. matrices
// must be len = 16 * boneCount (column-major mat4 per bone), with
// boneCount ≤ palette.Capacity.
func (r *Renderer) UploadBonePalette(palette *BonePalette, matrices []float32) error {
	if palette == nil || palette.Buffer == nil {
		return fmt.Errorf("bundle.UploadBonePalette: nil palette")
	}
	if len(matrices)%16 != 0 {
		return fmt.Errorf("bundle.UploadBonePalette: len %% 16 must be 0 (got %d)", len(matrices))
	}
	boneCount := len(matrices) / 16
	if boneCount > palette.Capacity {
		return fmt.Errorf("bundle.UploadBonePalette: %d bones exceeds capacity %d",
			boneCount, palette.Capacity)
	}
	r.device.Queue().WriteBuffer(palette.Buffer, 0, float32sToBytes(matrices))
	return nil
}

// DestroyBonePalette frees the underlying storage buffer.
func (r *Renderer) DestroyBonePalette(palette *BonePalette) {
	if palette == nil || palette.Buffer == nil {
		return
	}
	palette.Buffer.Destroy()
	palette.Buffer = nil
}
