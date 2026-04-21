package bundle

import (
	"fmt"
	"strings"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/gpu"
)

const fallbackEnvironmentKey = "\x00env:fallback"

func (r *Renderer) ensureEnvironmentBindGroups(env engine.RenderEnvironment) error {
	key, tex, err := r.environmentTexture(strings.TrimSpace(env.EnvMap))
	if err != nil {
		return err
	}
	if key == r.envBindGroupKey {
		return nil
	}

	lit, err := r.createLitSceneBindGroup(r.litBGLayout, tex, "bundle.lit.bindgroup")
	if err != nil {
		return fmt.Errorf("bundle.environment lit bindgroup: %w", err)
	}
	skinned, err := r.createLitSceneBindGroup(r.skinnedLitBGLayout, tex, "bundle.lit.skinned.bindgroup")
	if err != nil {
		lit.Destroy()
		return fmt.Errorf("bundle.environment skinned lit bindgroup: %w", err)
	}
	if r.litBindGrp != nil {
		r.litBindGrp.Destroy()
	}
	if r.skinnedLitBindGrp != nil {
		r.skinnedLitBindGrp.Destroy()
	}
	r.litBindGrp = lit
	r.skinnedLitBindGrp = skinned
	r.envBindGroupKey = key
	return nil
}

func (r *Renderer) environmentTexture(url string) (string, *textureResources, error) {
	if url == "" {
		tex, err := r.ensureFallbackCubeTexture()
		return fallbackEnvironmentKey, tex, err
	}
	if cached, ok := r.textureCache[url]; ok && textureIsCube(cached) {
		return url, cached, nil
	}
	key := "\x00env:" + url
	if cached, ok := r.textureCache[key]; ok {
		return key, cached, nil
	}
	tex, err := r.createProceduralCubeTexture(url)
	if err != nil {
		return "", nil, err
	}
	r.textureCache[key] = tex
	return key, tex, nil
}

func textureIsCube(tex *textureResources) bool {
	if tex == nil {
		return false
	}
	switch tex.viewDimension {
	case gpu.TextureViewDimensionCube, gpu.TextureViewDimensionCubeArray:
		return true
	default:
		return false
	}
}

func (r *Renderer) ensureFallbackCubeTexture() (*textureResources, error) {
	if r.fallbackCubeTexture != nil {
		return r.fallbackCubeTexture, nil
	}
	tex, err := r.createCubeTexture([6][3]byte{
		{220, 232, 255},
		{190, 210, 255},
		{235, 240, 255},
		{120, 126, 138},
		{210, 220, 238},
		{180, 190, 210},
	}, "bundle.texture.env.fallback")
	if err != nil {
		return nil, err
	}
	r.fallbackCubeTexture = tex
	return tex, nil
}

func (r *Renderer) createProceduralCubeTexture(seed string) (*textureResources, error) {
	a, b := colorPairFromString(seed)
	return r.createCubeTexture([6][3]byte{
		a,
		{a[1], a[2], a[0]},
		b,
		{b[1], b[2], b[0]},
		{byte((uint16(a[0]) + uint16(b[0])) / 2), byte((uint16(a[1]) + uint16(b[1])) / 2), byte((uint16(a[2]) + uint16(b[2])) / 2)},
		{byte((uint16(a[2]) + uint16(b[1])) / 2), byte((uint16(a[0]) + uint16(b[2])) / 2), byte((uint16(a[1]) + uint16(b[0])) / 2)},
	}, "bundle.texture.env:"+seed)
}

func (r *Renderer) createCubeTexture(colors [6][3]byte, label string) (*textureResources, error) {
	tex, err := r.device.CreateTexture(gpu.TextureDesc{
		Width:              1,
		Height:             1,
		DepthOrArrayLayers: 6,
		Format:             gpu.FormatRGBA8UnormSRGB,
		Usage:              gpu.TextureUsageTextureBinding | gpu.TextureUsageCopyDst,
		Label:              label,
	})
	if err != nil {
		return nil, fmt.Errorf("bundle.createCubeTexture: %w", err)
	}
	for layer, c := range colors {
		r.device.Queue().WriteTextureLevelLayer(tex, 0, layer, []byte{c[0], c[1], c[2], 255}, 4, 1, 1, 1)
	}
	view := tex.CreateViewDesc(gpu.TextureViewDesc{
		Dimension:       gpu.TextureViewDimensionCube,
		ArrayLayerCount: 6,
	})
	return &textureResources{tex: tex, view: view, viewDimension: gpu.TextureViewDimensionCube}, nil
}

func environmentParams(env engine.RenderEnvironment) [4]float32 {
	if strings.TrimSpace(env.EnvMap) == "" {
		return [4]float32{}
	}
	intensity := float32(env.EnvIntensity)
	if intensity == 0 {
		intensity = 1
	}
	return [4]float32{intensity, float32(env.EnvRotation), 1, 0}
}
