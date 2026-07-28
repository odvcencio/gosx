package bundle

import (
	"fmt"
	"strings"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/gpu"
)

// materialFingerprint is the hashable identity of a material as seen by the
// GPU side of the renderer. Two InstancedMesh entries with the same
// fingerprint share a single uniform buffer + bind group; subsequent frames
// with the same bundle reuse both without re-uploading.
type materialFingerprint struct {
	// baseColor packs to vec4 (rgba). Stored as quantized uint16
	// so the type stays comparable with == and usable as a map key.
	baseColorR, baseColorG, baseColorB, opacity uint16
	metalness, roughness                        uint16
	clearcoat, sheen, transmission, iridescence uint16
	anisotropy                                  int16
	emissiveR, emissiveG, emissiveB             uint16
	emissiveStrength                            uint16
	// textureURL: "" means no texture; the fallback 1×1 white is bound so
	// the bind group layout stays static.
	textureURL string
	// normalURL: "" means no normal map; the shader keeps the interpolated
	// geometric normal and ignores the bound fallback texture.
	normalURL string
	// roughnessURL / metalnessURL / emissiveURL: separate glTF map slots.
	// litWGSL samples roughness from green and metalness from blue, the
	// channels glTF 2.0 defines, and multiplies each one against the scalar
	// factor in pbrParams. The emissive map replaces the emissive colour, it
	// does not tint it. Empty means the 1x1 white fallback, which leaves the
	// flat factor unchanged.
	roughnessURL string
	metalnessURL string
	emissiveURL  string
	// useVertexColor = 1 when the renderer should mix in the per-vertex color
	// as baseColor. Unlit-fallback legacy primitives (cube/plane/sphere)
	// set this; explicit RenderMaterial entries clear it.
	useVertexColor bool
}

// materialResources holds the GPU-side material record.
type materialResources struct {
	buf               gpu.Buffer
	bindGroup         gpu.BindGroup
	skinnedBindGroup  gpu.BindGroup
	surfaceBindGroups map[string]gpu.BindGroup
}

// materialUniformSize is the std140-aligned size of the WGSL Material struct.
// Seven vec4 records = 112 bytes (baseColor + pbrParams + emissive + two
// texture-flag vectors + two physical-material vectors).
const materialUniformSize = 112

// resolveMaterialFingerprint picks the effective material for one instanced
// mesh. Lookup prefers materials[MaterialIndex]; out-of-range or missing
// indices fall back to a sensible vertex-color default so legacy primitives
// still render lit.
//
// The parameter is the material slice, not the whole bundle. A RenderBundle is
// 864 bytes and a RenderInstancedMesh is 296 bytes, and the previous signature
// copied both on every call. prepareMeshStates calls this once per mesh per
// frame, so at a thousand meshes the copies alone cost more than the work.
func resolveMaterialFingerprint(materials []engine.RenderMaterial, materialIndex int) materialFingerprint {
	if materialIndex < 0 || materialIndex >= len(materials) {
		return defaultVertexColorMaterial()
	}
	return materialFromRender(materials[materialIndex])
}

// materialFingerprintMemo holds one fingerprint per bundle material index for
// the frame in progress. resolveMaterialFingerprint parses a CSS colour string
// and quantizes fifteen floats, and prepareMeshStates used to repeat that for
// every mesh. Scenes normally share a handful of materials across many meshes,
// so the repeat was the largest single cost in a high-mesh-count frame: a CPU
// profile of a 1000-mesh frame put 26 percent of all samples in that one
// function.
//
// The memo lives for one frame. Materials may change between frames, and
// engine.RenderMaterial holds maps, so it cannot be compared with == to detect
// that. Rebuilding once per material per frame is cheap and always correct.
type materialFingerprintMemo struct {
	fps   []materialFingerprint
	valid []bool
}

// reset sizes the memo for a bundle with count materials and marks every slot
// stale. The slices only grow, so a steady-state frame allocates nothing.
func (m *materialFingerprintMemo) reset(count int) {
	if cap(m.fps) < count {
		m.fps = make([]materialFingerprint, count)
		m.valid = make([]bool, count)
	}
	m.fps = m.fps[:count]
	m.valid = m.valid[:count]
	clear(m.valid)
}

// lookup returns the fingerprint for one material index, computing it on first
// request in the frame and reusing it afterwards.
func (m *materialFingerprintMemo) lookup(materials []engine.RenderMaterial, materialIndex int) materialFingerprint {
	if materialIndex < 0 || materialIndex >= len(m.valid) {
		// Out of range indices take the default, which costs no parsing.
		return resolveMaterialFingerprint(materials, materialIndex)
	}
	if m.valid[materialIndex] {
		return m.fps[materialIndex]
	}
	fp := resolveMaterialFingerprint(materials, materialIndex)
	m.fps[materialIndex] = fp
	m.valid[materialIndex] = true
	return fp
}

func defaultVertexColorMaterial() materialFingerprint {
	return materialFingerprint{
		baseColorR:       quantize(0.8),
		baseColorG:       quantize(0.8),
		baseColorB:       quantize(0.8),
		opacity:          quantize(1),
		metalness:        quantize(0.0),
		roughness:        quantize(0.6),
		clearcoat:        0,
		sheen:            0,
		transmission:     0,
		iridescence:      0,
		anisotropy:       0,
		emissiveR:        0,
		emissiveG:        0,
		emissiveB:        0,
		emissiveStrength: 0,
		useVertexColor:   true,
	}
}

func materialFromRender(mat engine.RenderMaterial) materialFingerprint {
	base := parseCSSColor(mat.Color, [3]float32{0.8, 0.8, 0.8})
	opacity := materialOpacity(mat)
	metal := float32(mat.Metalness)
	rough := float32(mat.Roughness)
	if rough <= 0 {
		// 0 roughness mirrors to a near-perfect mirror. Clamp unspecified
		// materials to a practical default while preserving explicit values.
		rough = 0.4
	}
	// RenderMaterial.Emissive is a scalar strength in the bundle schema, so the
	// emissive colour has to come from somewhere else. Both browser renderers
	// start from the shaded albedo. litWGSL now does the same: it takes the base
	// colour it already sampled, and lets an emissive map replace it. The
	// emissiveR/G/B lanes below therefore hold the flat base colour, and litWGSL
	// no longer reads them. Keep them: they hold the Material.emissive vec4 that
	// fixes the offset of every later field, and a dedicated emissive colour
	// will fill them.
	emissiveStrength := float32(mat.Emissive)
	return materialFingerprint{
		baseColorR:       quantize(base[0]),
		baseColorG:       quantize(base[1]),
		baseColorB:       quantize(base[2]),
		opacity:          quantize(opacity),
		metalness:        quantize(metal),
		roughness:        quantize(rough),
		clearcoat:        quantize(clamp01f(float32(mat.Clearcoat))),
		sheen:            quantize(clamp01f(float32(mat.Sheen))),
		transmission:     quantize(clamp01f(float32(mat.Transmission))),
		iridescence:      quantize(clamp01f(float32(mat.Iridescence))),
		anisotropy:       quantizeSignedUnit(float32(mat.Anisotropy)),
		emissiveR:        quantize(base[0]),
		emissiveG:        quantize(base[1]),
		emissiveB:        quantize(base[2]),
		emissiveStrength: quantize(emissiveStrength),
		textureURL:       mat.Texture,
		normalURL:        mat.NormalMap,
		roughnessURL:     mat.RoughnessMap,
		metalnessURL:     mat.MetalnessMap,
		emissiveURL:      mat.EmissiveMap,
		useVertexColor:   false,
	}
}

func materialOpacity(mat engine.RenderMaterial) float32 {
	mode := strings.ToLower(strings.TrimSpace(mat.BlendMode))
	pass := strings.ToLower(strings.TrimSpace(mat.RenderPass))
	explicitTransparentPass := mode == "alpha" || mode == "additive" || pass == "alpha" || pass == "additive"
	if mat.Opacity <= 0 && !explicitTransparentPass {
		return 1
	}
	opacity := float32(mat.Opacity)
	if opacity < 0 {
		return 0
	}
	if opacity > 1 {
		return 1
	}
	return opacity
}

// quantize folds a float in a reasonable range ([0, 4]) down to a uint16 for
// the fingerprint key. 1/1024 LSB is precise enough for the cache to reject
// visually-different materials without over-fragmenting on FP rounding.
func quantize(v float32) uint16 {
	if v < 0 {
		v = 0
	}
	if v > 4 {
		v = 4
	}
	return uint16(v * 1024)
}

func dequantize(u uint16) float32 { return float32(u) / 1024 }

func clamp01f(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func quantizeSignedUnit(v float32) int16 {
	if v < -1 {
		v = -1
	}
	if v > 1 {
		v = 1
	}
	return int16(v * 1024)
}

func dequantizeSignedUnit(v int16) float32 { return float32(v) / 1024 }

// ensureMaterial vends a (uniform-buffer, bind-group) pair for this
// fingerprint, creating + uploading on cache miss. The bind group includes
// the material uniform buffer, base-color texture view, normal-map texture
// view, and shared color sampler — WebGPU bind-group layouts are fixed, so
// unspecified texture URLs fall back to the 1×1 white texture.
func (r *Renderer) ensureMaterial(fp materialFingerprint) (*materialResources, error) {
	if r.litMaterialLayout == nil {
		return nil, fmt.Errorf("bundle: material layout not built")
	}
	if cached, ok := r.materialCache[fp]; ok {
		return cached, nil
	}
	buf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  materialUniformSize,
		Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst,
		Label: "bundle.material.uniform",
	})
	if err != nil {
		return nil, fmt.Errorf("bundle: create material buffer: %w", err)
	}
	r.device.Queue().WriteBuffer(buf, 0, materialUniformBytes(fp))

	tex, err := r.ensureMaterialTexture(fp.textureURL)
	if err != nil {
		buf.Destroy()
		return nil, fmt.Errorf("bundle: resolve material texture: %w", err)
	}
	normalTex, err := r.ensureMaterialTexture(fp.normalURL)
	if err != nil {
		buf.Destroy()
		return nil, fmt.Errorf("bundle: resolve material normal map: %w", err)
	}
	roughTex, err := r.ensureMaterialTexture(fp.roughnessURL)
	if err != nil {
		buf.Destroy()
		return nil, fmt.Errorf("bundle: resolve roughness map: %w", err)
	}
	metalTex, err := r.ensureMaterialTexture(fp.metalnessURL)
	if err != nil {
		buf.Destroy()
		return nil, fmt.Errorf("bundle: resolve metalness map: %w", err)
	}
	emissiveTex, err := r.ensureMaterialTexture(fp.emissiveURL)
	if err != nil {
		buf.Destroy()
		return nil, fmt.Errorf("bundle: resolve emissive map: %w", err)
	}

	bg, err := r.createMaterialBindGroup(r.litMaterialLayout, buf, tex, normalTex, roughTex, metalTex, emissiveTex, "bundle.material.bindgroup")
	if err != nil {
		buf.Destroy()
		return nil, fmt.Errorf("bundle: create material bind group: %w", err)
	}
	skinnedBG, err := r.createMaterialBindGroup(r.skinnedLitMaterialLayout, buf, tex, normalTex, roughTex, metalTex, emissiveTex, "bundle.material.skinned.bindgroup")
	if err != nil {
		buf.Destroy()
		bg.Destroy()
		return nil, fmt.Errorf("bundle: create skinned material bind group: %w", err)
	}
	res := &materialResources{buf: buf, bindGroup: bg, skinnedBindGroup: skinnedBG, surfaceBindGroups: make(map[string]gpu.BindGroup)}
	r.materialCache[fp] = res
	return res, nil
}

func (r *Renderer) createMaterialBindGroup(layout gpu.BindGroupLayout, buf gpu.Buffer, tex, normalTex, roughTex, metalTex, emissiveTex *textureResources, label string) (gpu.BindGroup, error) {
	return r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: buf, Size: materialUniformSize},
			{Binding: 1, TextureView: tex.view},
			{Binding: 2, Sampler: r.materialSampler},
			{Binding: 3, TextureView: normalTex.view},
			{Binding: 4, Sampler: r.materialSampler},
			{Binding: 5, TextureView: roughTex.view},
			{Binding: 6, TextureView: metalTex.view},
			{Binding: 7, TextureView: emissiveTex.view},
		},
		Label: label,
	})
}

// materialUniformBytes encodes the Material struct layout expected by
// litWGSL. Layout in bytes:
//
//	 0..16  baseColor     vec4 (rgba)
//	16..32  pbrParams     vec4 (metalness, roughness, emissiveStrength, useVertexColor)
//	32..48  emissive      vec4 (rgba) — reserved, litWGSL reads baseColor
//	48..64  textureParams vec4 (hasBaseColor, hasNormal, hasRoughMap, hasMetalMap)
//	64..80  textureParams2 vec4 (hasEmissiveMap, 0, 0, 0)
//	80..96  physicalParams vec4 (clearcoat, sheen, transmission, iridescence)
//	96..112 physicalParams2 vec4 (anisotropy, 0, 0, 0)
func materialUniformBytes(fp materialFingerprint) []byte {
	useVertex := float32(0)
	if fp.useVertexColor {
		useVertex = 1
	}
	flag := func(url string) float32 {
		if url == "" {
			return 0
		}
		return 1
	}
	values := []float32{
		// baseColor (rgba)
		dequantize(fp.baseColorR), dequantize(fp.baseColorG), dequantize(fp.baseColorB), dequantize(fp.opacity),
		// pbrParams (metal, rough, emissiveStrength, useVertexColor)
		dequantize(fp.metalness), dequantize(fp.roughness), dequantize(fp.emissiveStrength), useVertex,
		// emissive (rgba)
		dequantize(fp.emissiveR), dequantize(fp.emissiveG), dequantize(fp.emissiveB), 1,
		// textureParams (hasBaseColor, hasNormal, hasRough, hasMetal)
		flag(fp.textureURL), flag(fp.normalURL), flag(fp.roughnessURL), flag(fp.metalnessURL),
		// textureParams2 (hasEmissive, 0, 0, 0)
		flag(fp.emissiveURL), 0, 0, 0,
		// physicalParams (clearcoat, sheen, transmission, iridescence)
		dequantize(fp.clearcoat), dequantize(fp.sheen), dequantize(fp.transmission), dequantize(fp.iridescence),
		// physicalParams2 (anisotropy, 0, 0, 0)
		dequantizeSignedUnit(fp.anisotropy), 0, 0, 0,
	}
	out := make([]byte, materialUniformSize)
	copy(out[:len(values)*4], float32sToBytes(values))
	return out
}
