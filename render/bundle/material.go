package bundle

import (
	"fmt"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/gpu"
)

// materialFingerprint is the hashable identity of a material as seen by the
// GPU side of the renderer. Two InstancedMesh entries with the same
// fingerprint share a single uniform buffer + bind group; subsequent frames
// with the same bundle reuse both without re-uploading.
type materialFingerprint struct {
	// baseColor packs to vec4 (rgb, a unused). Stored as quantized uint16
	// so the type stays comparable with == and usable as a map key.
	baseColorR, baseColorG, baseColorB uint16
	metalness, roughness               uint16
	emissiveR, emissiveG, emissiveB    uint16
	emissiveStrength                   uint16
	// textureURL: "" means no texture; the fallback 1×1 white is bound so
	// the bind group layout stays static.
	textureURL string
	// normalURL: "" means no normal map; the shader keeps the interpolated
	// geometric normal and ignores the bound fallback texture.
	normalURL string
	// roughnessURL / metalnessURL / emissiveURL: separate glTF-PBR map
	// slots. When set the shader samples the map's red channel and
	// multiplies it against the scalar factor in pbrParams. Empty = fallback
	// white, which degrades to the flat factor only (no tint).
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
	buf       gpu.Buffer
	bindGroup gpu.BindGroup
}

// materialUniformSize is the std140-aligned size of the WGSL Material struct.
// Five vec4 records = 80 bytes (baseColor + pbrParams + emissive + two
// texture-flag vectors covering baseColor/normal/roughness/metal/emissive).
const materialUniformSize = 80

// resolveMaterialFingerprint picks the effective material for one instanced
// mesh. Lookup prefers bundle.Materials[MaterialIndex]; out-of-range or
// missing indices fall back to a sensible vertex-color default so legacy
// primitives still render lit.
func resolveMaterialFingerprint(b engine.RenderBundle, im engine.RenderInstancedMesh) materialFingerprint {
	if im.MaterialIndex < 0 || im.MaterialIndex >= len(b.Materials) {
		return defaultVertexColorMaterial()
	}
	mat := b.Materials[im.MaterialIndex]
	return materialFromRender(mat)
}

func defaultVertexColorMaterial() materialFingerprint {
	return materialFingerprint{
		baseColorR:       quantize(0.8),
		baseColorG:       quantize(0.8),
		baseColorB:       quantize(0.8),
		metalness:        quantize(0.0),
		roughness:        quantize(0.6),
		emissiveR:        0,
		emissiveG:        0,
		emissiveB:        0,
		emissiveStrength: 0,
		useVertexColor:   true,
	}
}

func materialFromRender(mat engine.RenderMaterial) materialFingerprint {
	base := parseCSSColor(mat.Color, [3]float32{0.8, 0.8, 0.8})
	metal := float32(mat.Metalness)
	rough := float32(mat.Roughness)
	if rough <= 0 {
		// 0 roughness mirrors to a near-perfect mirror which looks broken
		// without IBL. Clamp to a sensible default until R3 ships environment
		// maps.
		rough = 0.4
	}
	// RenderMaterial.Emissive is a scalar strength in the bundle schema; we
	// interpret it as a multiplier on the base color to avoid needing a
	// separate emissive color. A dedicated emissive color lands in R3.
	emissiveStrength := float32(mat.Emissive)
	return materialFingerprint{
		baseColorR:       quantize(base[0]),
		baseColorG:       quantize(base[1]),
		baseColorB:       quantize(base[2]),
		metalness:        quantize(metal),
		roughness:        quantize(rough),
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

	bg, err := r.device.CreateBindGroup(gpu.BindGroupDesc{
		Layout: r.litMaterialLayout,
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
		Label: "bundle.material.bindgroup",
	})
	if err != nil {
		buf.Destroy()
		return nil, fmt.Errorf("bundle: create material bind group: %w", err)
	}
	res := &materialResources{buf: buf, bindGroup: bg}
	r.materialCache[fp] = res
	return res, nil
}

// materialUniformBytes encodes the Material struct layout expected by
// litWGSL. Layout in bytes:
//
//   0..16  baseColor     vec4 (rgba)
//  16..32  pbrParams     vec4 (metalness, roughness, emissiveStrength, useVertexColor)
//  32..48  emissive      vec4 (rgba)
//  48..64  textureParams vec4 (hasBaseColor, hasNormal, hasRoughMap, hasMetalMap)
//  64..80  textureParams2 vec4 (hasEmissiveMap, 0, 0, 0)
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
		dequantize(fp.baseColorR), dequantize(fp.baseColorG), dequantize(fp.baseColorB), 1,
		// pbrParams (metal, rough, emissiveStrength, useVertexColor)
		dequantize(fp.metalness), dequantize(fp.roughness), dequantize(fp.emissiveStrength), useVertex,
		// emissive (rgba)
		dequantize(fp.emissiveR), dequantize(fp.emissiveG), dequantize(fp.emissiveB), 1,
		// textureParams (hasBaseColor, hasNormal, hasRough, hasMetal)
		flag(fp.textureURL), flag(fp.normalURL), flag(fp.roughnessURL), flag(fp.metalnessURL),
		// textureParams2 (hasEmissive, 0, 0, 0)
		flag(fp.emissiveURL), 0, 0, 0,
	}
	out := make([]byte, materialUniformSize)
	copy(out[:len(values)*4], float32sToBytes(values))
	return out
}
