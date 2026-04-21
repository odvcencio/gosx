package bundle

import (
	"fmt"
	"strings"

	"github.com/odvcencio/gosx/render/bundle/ktx2"
	"github.com/odvcencio/gosx/render/gpu"
)

// LoadKTX2Texture parses data as a KTX2 container, creates a matching GPU
// texture, and uploads every provided mip level. Returns the texture +
// default view, ready to bind into a material group.
func (r *Renderer) LoadKTX2Texture(data []byte) (*textureResources, error) {
	img, err := ktx2.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("bundle.LoadKTX2Texture: parse: %w", err)
	}
	gpuFormat := ktx2FormatToGPU(img.Format)
	if gpuFormat == gpu.FormatUndefined {
		return nil, fmt.Errorf("bundle.LoadKTX2Texture: unmapped VkFormat %d", img.Format)
	}

	tex, err := r.device.CreateTexture(gpu.TextureDesc{
		Width:              img.Width,
		Height:             img.Height,
		DepthOrArrayLayers: ktx2DepthOrArrayLayers(img),
		Dimension:          ktx2TextureDimension(img),
		Format:             gpuFormat,
		Usage:              gpu.TextureUsageTextureBinding | gpu.TextureUsageCopyDst,
		MipLevelCount:      max(1, len(img.Levels)),
		Label:              "bundle.ktx2",
	})
	if err != nil {
		return nil, fmt.Errorf("bundle.LoadKTX2Texture: create texture: %w", err)
	}

	for level, mip := range img.Levels {
		rowPitch := mip.RowPitch(img.Format)
		rowsPerImage := mip.BlockRows(img.Format)
		if rowPitch <= 0 || rowsPerImage <= 0 {
			tex.Destroy()
			return nil, fmt.Errorf("bundle.LoadKTX2Texture: unsupported upload layout for VkFormat %d", img.Format)
		}
		sliceBytes := rowPitch * rowsPerImage
		slices := max(1, mip.Depth) * max(1, mip.Layers) * max(1, mip.Faces)
		if len(mip.Bytes) < sliceBytes*slices {
			tex.Destroy()
			return nil, fmt.Errorf("bundle.LoadKTX2Texture: level %d has %d bytes, need at least %d",
				level, len(mip.Bytes), sliceBytes*slices)
		}
		for slice := 0; slice < slices; slice++ {
			off := slice * sliceBytes
			r.device.Queue().WriteTextureLevelLayer(
				tex,
				level,
				slice,
				mip.Bytes[off:off+sliceBytes],
				rowPitch,
				rowsPerImage,
				mip.Width,
				mip.Height,
			)
		}
	}

	viewDesc := ktx2TextureViewDesc(img, gpuFormat)
	return &textureResources{tex: tex, view: tex.CreateViewDesc(viewDesc), viewDimension: viewDesc.Dimension}, nil
}

// RegisterKTX2Texture loads a KTX2 payload and caches it under key for later
// material or environment-map lookup. Cubemap KTX2 assets can then be selected
// by RenderEnvironment.EnvMap.
func (r *Renderer) RegisterKTX2Texture(key string, data []byte) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("bundle.RegisterKTX2Texture: key is required")
	}
	res, err := r.LoadKTX2Texture(data)
	if err != nil {
		return err
	}
	if old := r.textureCache[key]; old != nil && old.tex != nil {
		old.tex.Destroy()
	}
	r.textureCache[key] = res
	r.envBindGroupKey = ""
	return nil
}

// ktx2FormatToGPU maps parsed KTX2 VkFormat values to gpu.TextureFormat.
// Unknown formats return FormatUndefined so the loader fails with a clear
// error rather than creating a texture in the wrong format.
func ktx2FormatToGPU(vk int) gpu.TextureFormat {
	switch vk {
	case ktx2.VkFormatR8G8B8A8Unorm:
		return gpu.FormatRGBA8Unorm
	case ktx2.VkFormatR8G8B8A8SRGB:
		return gpu.FormatRGBA8UnormSRGB
	case ktx2.VkFormatB8G8R8A8Unorm:
		return gpu.FormatBGRA8Unorm
	case ktx2.VkFormatB8G8R8A8SRGB:
		return gpu.FormatBGRA8UnormSRGB
	case ktx2.VkFormatBC7UnormBlock:
		return gpu.FormatBC7RGBAUnorm
	case ktx2.VkFormatBC7SRGBBlock:
		return gpu.FormatBC7RGBAUnormSRGB
	case ktx2.VkFormatASTC4x4UnormBlock:
		return gpu.FormatASTC4x4Unorm
	case ktx2.VkFormatASTC4x4SRGBBlock:
		return gpu.FormatASTC4x4UnormSRGB
	case ktx2.VkFormatASTC6x6UnormBlock:
		return gpu.FormatASTC6x6Unorm
	case ktx2.VkFormatASTC6x6SRGBBlock:
		return gpu.FormatASTC6x6UnormSRGB
	case ktx2.VkFormatASTC8x8UnormBlock:
		return gpu.FormatASTC8x8Unorm
	case ktx2.VkFormatASTC8x8SRGBBlock:
		return gpu.FormatASTC8x8UnormSRGB
	case ktx2.VkFormatETC2R8G8B8UnormBlock:
		return gpu.FormatETC2RGB8Unorm
	case ktx2.VkFormatETC2R8G8B8SRGBBlock:
		return gpu.FormatETC2RGB8UnormSRGB
	case ktx2.VkFormatETC2R8G8B8A8UnormBlock:
		return gpu.FormatETC2RGBA8Unorm
	case ktx2.VkFormatETC2R8G8B8A8SRGBBlock:
		return gpu.FormatETC2RGBA8UnormSRGB
	}
	return gpu.FormatUndefined
}

func ktx2TextureDimension(img *ktx2.Image) gpu.TextureDimension {
	if img != nil && img.Depth > 1 {
		return gpu.TextureDimension3D
	}
	return gpu.TextureDimension2D
}

func ktx2DepthOrArrayLayers(img *ktx2.Image) int {
	if img == nil {
		return 1
	}
	if img.Depth > 1 {
		return img.Depth
	}
	return max(1, img.Layers) * max(1, img.Faces)
}

func ktx2TextureViewDesc(img *ktx2.Image, format gpu.TextureFormat) gpu.TextureViewDesc {
	if img == nil {
		return gpu.TextureViewDesc{Format: format}
	}
	desc := gpu.TextureViewDesc{
		Format:        format,
		MipLevelCount: max(1, len(img.Levels)),
	}
	switch {
	case img.Depth > 1:
		desc.Dimension = gpu.TextureViewDimension3D
	case img.Faces == 6 && img.Layers > 1:
		desc.Dimension = gpu.TextureViewDimensionCubeArray
		desc.ArrayLayerCount = img.Faces * img.Layers
	case img.Faces == 6:
		desc.Dimension = gpu.TextureViewDimensionCube
		desc.ArrayLayerCount = img.Faces
	case img.Layers > 1:
		desc.Dimension = gpu.TextureViewDimension2DArray
		desc.ArrayLayerCount = img.Layers
	}
	return desc
}
