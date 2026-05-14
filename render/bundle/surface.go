package bundle

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/gpu"
)

const surfaceWGSL = `
struct Scene {
  viewProj : mat4x4<f32>,
};

struct Material {
  baseColor     : vec4<f32>,
  pbrParams     : vec4<f32>, // x=metalness, y=roughness, z=emissiveStrength, w=useVertexColor
  emissive      : vec4<f32>,
  textureParams : vec4<f32>,
  textureParams2: vec4<f32>,
  physicalParams : vec4<f32>,
  physicalParams2: vec4<f32>,
};

@group(0) @binding(0) var<uniform> scene             : Scene;
@group(1) @binding(0) var<uniform> material          : Material;
@group(1) @binding(1) var          baseColorTexture  : texture_2d<f32>;
@group(1) @binding(2) var          baseColorSampler  : sampler;
@group(1) @binding(3) var          normalMapTexture  : texture_2d<f32>;
@group(1) @binding(4) var          normalMapSampler  : sampler;
@group(1) @binding(5) var          roughnessMapTex   : texture_2d<f32>;
@group(1) @binding(6) var          metalnessMapTex   : texture_2d<f32>;
@group(1) @binding(7) var          emissiveMapTex    : texture_2d<f32>;

struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) uv       : vec2<f32>,
  @location(1) @interpolate(flat) pickId : u32,
};

struct FSOut {
  @location(0) color  : vec4<f32>,
  @location(1) pickId : u32,
};

@vertex
fn vs_main(
  @location(0) pos : vec3<f32>,
  @location(1) uv  : vec2<f32>,
  @location(2) pickId : u32,
) -> VSOut {
  var out : VSOut;
  out.pos = scene.viewProj * vec4<f32>(pos, 1.0);
  out.uv = uv;
  out.pickId = pickId;
  return out;
}

@fragment
fn fs_main(in : VSOut) -> FSOut {
  let sampled = textureSample(baseColorTexture, baseColorSampler, in.uv);
  let emissiveBoost = 1.0 + max(material.pbrParams.z, 0.0) * 0.5;
  var out : FSOut;
  out.color = vec4<f32>(
    clamp(sampled.rgb * material.baseColor.rgb * emissiveBoost, vec3<f32>(0.0), vec3<f32>(1.0)),
    clamp(sampled.a * material.baseColor.a, 0.0, 1.0),
  );
  out.pickId = in.pickId;
  return out;
}
`

const (
	surfacePassOpaque   = "opaque"
	surfacePassAlpha    = "alpha"
	surfacePassAdditive = "additive"
)

func (r *Renderer) buildSurfacePipelines() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{
		SourceWGSL: surfaceWGSL,
		Label:      "bundle.surface",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildSurfacePipelines: %w", err)
	}
	for _, mode := range []string{surfacePassOpaque, surfacePassAlpha, surfacePassAdditive} {
		pipeline, err := r.device.CreateRenderPipeline(gpu.RenderPipelineDesc{
			Vertex: gpu.VertexStageDesc{
				Module:     shader,
				EntryPoint: "vs_main",
				Buffers: []gpu.VertexBufferLayout{
					{ArrayStride: 12, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
						{ShaderLocation: 0, Offset: 0, Format: gpu.VertexFormatFloat32x3},
					}},
					{ArrayStride: 8, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
						{ShaderLocation: 1, Offset: 0, Format: gpu.VertexFormatFloat32x2},
					}},
					{ArrayStride: 4, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
						{ShaderLocation: 2, Offset: 0, Format: gpu.VertexFormatUint32},
					}},
				},
			},
			Fragment: gpu.FragmentStageDesc{
				Module:     shader,
				EntryPoint: "fs_main",
				Targets: []gpu.ColorTargetState{
					{Format: r.hdrFormat, Blend: surfaceBlendState(mode), WriteMask: gpu.ColorWriteAll},
					{Format: gpu.FormatR32Uint, WriteMask: gpu.ColorWriteAll},
				},
			},
			Primitive: gpu.PrimitiveState{
				Topology:  gpu.TopologyTriangleList,
				CullMode:  gpu.CullNone,
				FrontFace: gpu.FrontFaceCCW,
			},
			DepthStencil: &gpu.DepthStencilState{
				Format:            r.depthFormat,
				DepthWriteEnabled: mode == surfacePassOpaque,
				DepthCompare:      gpu.CompareLessEqual,
			},
			AutoLayout: true,
			Label:      "bundle.surface." + mode,
		})
		if err != nil {
			return fmt.Errorf("bundle.buildSurfacePipelines (%s): %w", mode, err)
		}
		r.surfacePipelines[mode] = pipeline
		r.surfaceBGLayouts[mode] = pipeline.GetBindGroupLayout(0)
		r.surfaceMaterialLayouts[mode] = pipeline.GetBindGroupLayout(1)
	}
	return nil
}

func surfaceBlendState(mode string) *gpu.BlendState {
	switch mode {
	case surfacePassAlpha:
		return alphaBlendState()
	case surfacePassAdditive:
		return &gpu.BlendState{
			Color: gpu.BlendComponent{SrcFactor: gpu.BlendSrcAlpha, DstFactor: gpu.BlendOne, Operation: gpu.BlendOpAdd},
			Alpha: gpu.BlendComponent{SrcFactor: gpu.BlendOne, DstFactor: gpu.BlendOne, Operation: gpu.BlendOpAdd},
		}
	default:
		return nil
	}
}

func (r *Renderer) drawSurfaces(pass gpu.RenderPassEncoder, b engine.RenderBundle) error {
	if len(b.Surfaces) == 0 {
		return nil
	}
	r.pruneSurfaceCache(b.Surfaces)
	for _, mode := range []string{surfacePassOpaque, surfacePassAlpha, surfacePassAdditive} {
		entries := surfaceEntriesForPass(b.Surfaces, mode)
		if len(entries) == 0 {
			continue
		}
		pipeline := r.surfacePipelines[mode]
		sceneBG := r.surfaceBindGrps[mode]
		materialLayout := r.surfaceMaterialLayouts[mode]
		if pipeline == nil || sceneBG == nil || materialLayout == nil {
			return fmt.Errorf("bundle.surface: pipeline resources for %s are not built", mode)
		}
		pass.SetPipeline(pipeline)
		pass.SetBindGroup(0, sceneBG)
		for _, ref := range entries {
			surface := b.Surfaces[ref.index]
			res, err := r.ensureSurfaceBuffers(ref.index, surface)
			if err != nil {
				return err
			}
			if res == nil || res.vertexCount == 0 {
				continue
			}
			fp := materialForSurface(b, surface)
			mat, err := r.ensureMaterial(fp)
			if err != nil {
				return err
			}
			materialBG, err := r.ensureSurfaceMaterialBindGroup(mat, fp, mode, materialLayout)
			if err != nil {
				return err
			}
			pass.SetBindGroup(1, materialBG)
			pass.SetVertexBuffer(0, res.positions)
			pass.SetVertexBuffer(1, res.uvs)
			pass.SetVertexBuffer(2, res.pickIDs)
			pass.Draw(res.vertexCount, 1, 0, 0)
		}
	}
	return nil
}

type surfaceRef struct {
	index int
	item  engine.RenderSurface
}

func surfaceEntriesForPass(surfaces []engine.RenderSurface, mode string) []surfaceRef {
	entries := make([]surfaceRef, 0, len(surfaces))
	for i, surface := range surfaces {
		if !surfaceDrawable(surface) || surfaceRenderPass(surface) != mode {
			continue
		}
		entries = append(entries, surfaceRef{index: i, item: surface})
	}
	if mode != surfacePassOpaque {
		sort.SliceStable(entries, func(i, j int) bool {
			left, right := entries[i].item, entries[j].item
			if left.DepthCenter != right.DepthCenter {
				return left.DepthCenter > right.DepthCenter
			}
			return left.ID < right.ID
		})
	}
	return entries
}

func surfaceDrawable(surface engine.RenderSurface) bool {
	if surface.ViewCulled {
		return false
	}
	if surface.SourceKind == "html" && !surface.TextureReady {
		return false
	}
	vertexCount := surfaceVertexCount(surface)
	return vertexCount > 0 && len(surface.Positions) >= vertexCount*3 && len(surface.UV) >= vertexCount*2
}

func surfaceVertexCount(surface engine.RenderSurface) int {
	vertexCount := surface.VertexCount
	if vertexCount <= 0 {
		vertexCount = min(len(surface.Positions)/3, len(surface.UV)/2)
	}
	if maxByBuffers := min(len(surface.Positions)/3, len(surface.UV)/2); vertexCount > maxByBuffers {
		vertexCount = maxByBuffers
	}
	if vertexCount < 0 {
		return 0
	}
	return vertexCount
}

func surfaceRenderPass(surface engine.RenderSurface) string {
	mode := strings.ToLower(strings.TrimSpace(surface.RenderPass))
	switch mode {
	case surfacePassAlpha, surfacePassAdditive:
		return mode
	default:
		return surfacePassOpaque
	}
}

func materialForSurface(b engine.RenderBundle, surface engine.RenderSurface) materialFingerprint {
	if surface.MaterialIndex < 0 || surface.MaterialIndex >= len(b.Materials) {
		fp := defaultVertexColorMaterial()
		fp.useVertexColor = false
		if strings.TrimSpace(surface.TextureKey) != "" {
			fp.textureURL = strings.TrimSpace(surface.TextureKey)
		}
		return fp
	}
	mat := b.Materials[surface.MaterialIndex]
	if strings.TrimSpace(mat.Texture) == "" && strings.TrimSpace(surface.TextureKey) != "" {
		mat.Texture = strings.TrimSpace(surface.TextureKey)
	}
	return materialFromRender(mat)
}

func (r *Renderer) ensureSurfaceMaterialBindGroup(mat *materialResources, fp materialFingerprint, mode string, layout gpu.BindGroupLayout) (gpu.BindGroup, error) {
	if mat == nil {
		return nil, fmt.Errorf("bundle.surface: nil material")
	}
	if mat.surfaceBindGroups == nil {
		mat.surfaceBindGroups = make(map[string]gpu.BindGroup)
	}
	if bg := mat.surfaceBindGroups[mode]; bg != nil {
		return bg, nil
	}
	tex, err := r.ensureMaterialTexture(fp.textureURL)
	if err != nil {
		return nil, fmt.Errorf("bundle.surface: resolve material texture: %w", err)
	}
	normalTex, err := r.ensureMaterialTexture(fp.normalURL)
	if err != nil {
		return nil, fmt.Errorf("bundle.surface: resolve normal map: %w", err)
	}
	roughTex, err := r.ensureMaterialTexture(fp.roughnessURL)
	if err != nil {
		return nil, fmt.Errorf("bundle.surface: resolve roughness map: %w", err)
	}
	metalTex, err := r.ensureMaterialTexture(fp.metalnessURL)
	if err != nil {
		return nil, fmt.Errorf("bundle.surface: resolve metalness map: %w", err)
	}
	emissiveTex, err := r.ensureMaterialTexture(fp.emissiveURL)
	if err != nil {
		return nil, fmt.Errorf("bundle.surface: resolve emissive map: %w", err)
	}
	bg, err := r.createMaterialBindGroup(layout, mat.buf, tex, normalTex, roughTex, metalTex, emissiveTex, "bundle.surface.material."+mode)
	if err != nil {
		return nil, fmt.Errorf("bundle.surface: create material bind group: %w", err)
	}
	mat.surfaceBindGroups[mode] = bg
	return bg, nil
}

func (r *Renderer) ensureSurfaceBuffers(index int, surface engine.RenderSurface) (*surfaceResources, error) {
	vertexCount := surfaceVertexCount(surface)
	if vertexCount <= 0 {
		return nil, nil
	}
	key := surfaceCacheKey(index, surface)
	posBytes := float64sToFloat32Bytes(surface.Positions[:vertexCount*3])
	uvBytes := float64sToFloat32Bytes(surface.UV[:vertexCount*2])
	pickBytes := surfacePickIDBytes(r.pickBaseForSurface(index), vertexCount)
	if cached := r.surfaceCache[key]; cached != nil && cached.positionLen == len(posBytes) && cached.uvLen == len(uvBytes) && cached.pickIDLen == len(pickBytes) {
		r.device.Queue().WriteBuffer(cached.positions, 0, posBytes)
		r.device.Queue().WriteBuffer(cached.uvs, 0, uvBytes)
		r.device.Queue().WriteBuffer(cached.pickIDs, 0, pickBytes)
		cached.vertexCount = vertexCount
		return cached, nil
	}
	if old := r.surfaceCache[key]; old != nil {
		destroySurfaceResources(old)
		delete(r.surfaceCache, key)
	}
	posBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  len(posBytes),
		Usage: gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
		Label: "bundle.surface.positions:" + key,
	})
	if err != nil {
		return nil, fmt.Errorf("bundle.surface: create positions buffer: %w", err)
	}
	uvBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  len(uvBytes),
		Usage: gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
		Label: "bundle.surface.uvs:" + key,
	})
	if err != nil {
		posBuf.Destroy()
		return nil, fmt.Errorf("bundle.surface: create uv buffer: %w", err)
	}
	pickBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  len(pickBytes),
		Usage: gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
		Label: "bundle.surface.pickIDs:" + key,
	})
	if err != nil {
		posBuf.Destroy()
		uvBuf.Destroy()
		return nil, fmt.Errorf("bundle.surface: create pick-id buffer: %w", err)
	}
	res := &surfaceResources{
		positions:   posBuf,
		uvs:         uvBuf,
		pickIDs:     pickBuf,
		positionLen: len(posBytes),
		uvLen:       len(uvBytes),
		pickIDLen:   len(pickBytes),
		vertexCount: vertexCount,
	}
	r.surfaceCache[key] = res
	r.device.Queue().WriteBuffer(posBuf, 0, posBytes)
	r.device.Queue().WriteBuffer(uvBuf, 0, uvBytes)
	r.device.Queue().WriteBuffer(pickBuf, 0, pickBytes)
	return res, nil
}

func surfacePickIDBytes(pickID uint32, vertexCount int) []byte {
	out := make([]byte, max(0, vertexCount)*4)
	for i := 0; i < vertexCount; i++ {
		putUint32LE(out[i*4:i*4+4], pickID)
	}
	return out
}

func raycastSurface(ray pickRay, surface engine.RenderSurface) (primitiveHit, bool) {
	vertexCount := surfaceVertexCount(surface)
	var best primitiveHit
	best.depth = float32(math.Inf(1))
	found := false
	for tri := 0; tri+2 < vertexCount; tri += 3 {
		p0 := surfacePositionAt(surface, tri)
		p1 := surfacePositionAt(surface, tri+1)
		p2 := surfacePositionAt(surface, tri+2)
		dist, u, v, ok := rayIntersectsTriangle(ray.origin, ray.dir, p0, p1, p2)
		if !ok || dist >= best.depth {
			continue
		}
		w := 1 - u - v
		best = primitiveHit{
			triangleIndex: tri / 3,
			localPosition: barycentric3(p0, p1, p2, w, u, v),
			worldPosition: [3]float32{
				ray.origin[0] + ray.dir[0]*dist,
				ray.origin[1] + ray.dir[1]*dist,
				ray.origin[2] + ray.dir[2]*dist,
			},
			uv:    barycentric2(surfaceUVAt(surface, tri), surfaceUVAt(surface, tri+1), surfaceUVAt(surface, tri+2), w, u, v),
			depth: dist,
		}
		found = true
	}
	return best, found
}

func surfacePositionAt(surface engine.RenderSurface, vertex int) [3]float32 {
	idx := vertex * 3
	if idx < 0 || idx+2 >= len(surface.Positions) {
		return [3]float32{}
	}
	return [3]float32{
		float32(surface.Positions[idx+0]),
		float32(surface.Positions[idx+1]),
		float32(surface.Positions[idx+2]),
	}
}

func surfaceUVAt(surface engine.RenderSurface, vertex int) [2]float32 {
	idx := vertex * 2
	if idx < 0 || idx+1 >= len(surface.UV) {
		return [2]float32{}
	}
	return [2]float32{float32(surface.UV[idx+0]), float32(surface.UV[idx+1])}
}

func (r *Renderer) pruneSurfaceCache(surfaces []engine.RenderSurface) {
	if len(r.surfaceCache) == 0 {
		return
	}
	live := make(map[string]struct{}, len(surfaces))
	for i, surface := range surfaces {
		if !surfaceDrawable(surface) {
			continue
		}
		live[surfaceCacheKey(i, surface)] = struct{}{}
	}
	for key, res := range r.surfaceCache {
		if _, ok := live[key]; ok {
			continue
		}
		destroySurfaceResources(res)
		delete(r.surfaceCache, key)
	}
}

func surfaceCacheKey(index int, surface engine.RenderSurface) string {
	id := strings.TrimSpace(surface.ID)
	if id == "" {
		id = fmt.Sprintf("surface-%d", index)
	}
	texture := strings.TrimSpace(surface.TextureKey)
	return fmt.Sprintf("%d:%s:%s:%d:%d", index, id, texture, surface.MaterialIndex, surfaceVertexCount(surface))
}

func destroySurfaceResources(s *surfaceResources) {
	if s == nil {
		return
	}
	if s.positions != nil {
		s.positions.Destroy()
	}
	if s.uvs != nil {
		s.uvs.Destroy()
	}
	if s.pickIDs != nil {
		s.pickIDs.Destroy()
	}
}
