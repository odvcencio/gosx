package bundle

import (
	"fmt"

	"github.com/odvcencio/gosx/render/bundle/ktx2"
	"github.com/odvcencio/gosx/render/gpu"
)

// LoadKTX2Texture parses data as a KTX2 container, creates a matching GPU
// texture, and uploads mip level 0 via Queue.WriteTexture. Returns the
// texture + default view, ready to bind into a material group.
//
// Multi-level mipmap upload is a follow-up — the parser already hands
// back all mip bytes, but gpu.Queue.WriteTexture currently only targets
// mip level 0. When an explicit mip-level parameter lands on WriteTexture
// in R5, this function will loop over img.Levels unchanged.
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
		Width:  img.Width,
		Height: img.Height,
		Format: gpuFormat,
		Usage:  gpu.TextureUsageTextureBinding | gpu.TextureUsageCopyDst,
		Label:  "bundle.ktx2",
	})
	if err != nil {
		return nil, fmt.Errorf("bundle.LoadKTX2Texture: create texture: %w", err)
	}

	bpp := ktx2.BytesPerPixel(img.Format)
	if len(img.Levels) > 0 && bpp > 0 {
		l0 := img.Levels[0]
		r.device.Queue().WriteTexture(tex, l0.Bytes, l0.Width*bpp, l0.Width, l0.Height)
	}

	return &textureResources{tex: tex, view: tex.CreateView()}, nil
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
	}
	return gpu.FormatUndefined
}
