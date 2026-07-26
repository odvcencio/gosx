package texture

import "fmt"

// Channel indexes one component of an Image.
type Channel int

// Channel indexes.
const (
	ChannelRed Channel = iota
	ChannelGreen
	ChannelBlue
	ChannelAlpha
)

// Source names one input channel for Pack. A nil Image contributes Constant.
type Source struct {
	Image    *Image
	Channel  Channel
	Constant float32
}

// Pack gathers up to four input channels into one image.
//
// Every non-nil source must share the target size. Pack does not resize, so the
// caller resizes first. That keeps one resample per input instead of one per
// packed output.
func Pack(width, height int, sources []Source) (*Image, error) {
	if width < 1 || height < 1 {
		return nil, fmt.Errorf("%w: target %dx%d", ErrShape, width, height)
	}
	if len(sources) == 0 || len(sources) > 4 {
		return nil, fmt.Errorf("%w: %d sources, want 1 to 4", ErrShape, len(sources))
	}
	for i, source := range sources {
		if source.Image == nil {
			continue
		}
		if source.Image.Width != width || source.Image.Height != height {
			return nil, fmt.Errorf("%w: source %d is %dx%d, target is %dx%d",
				ErrShape, i, source.Image.Width, source.Image.Height, width, height)
		}
		if source.Channel < ChannelRed || source.Channel > ChannelAlpha {
			return nil, fmt.Errorf("%w: source %d names channel %d", ErrShape, i, source.Channel)
		}
	}
	out := NewImage(width, height)
	// Alpha defaults to opaque so a three-channel pack stays usable.
	for i := 3; i < len(out.Pix); i += 4 {
		out.Pix[i] = 1
	}
	for target, source := range sources {
		for pixel := 0; pixel < width*height; pixel++ {
			value := source.Constant
			if source.Image != nil {
				value = source.Image.Pix[pixel*4+int(source.Channel)]
			}
			out.Pix[pixel*4+target] = value
		}
	}
	return out, nil
}

// PackMetallicRoughness builds the texture glTF already expects.
//
// The glTF 2.0 specification puts roughness in the green channel and metalness
// in the blue channel of metallicRoughnessTexture, and KHR_materials_* leaves
// the red channel free for the occlusion map that
// occlusionTexture shares with it. This function follows that layout:
//
//	red    occlusion, or 1 when no occlusion map exists
//	green  roughness
//	blue   metalness
//	alpha  1
//
// All three inputs are data, not colour, so they must decode as Linear. Passing
// an sRGB-decoded roughness map here produces a material that looks wrong in
// every renderer, and no later stage can detect it.
//
// Each input contributes its own red channel, because an authoring tool exports
// a single-factor map as three equal channels. A nil roughness or metalness
// source contributes the glTF default of 1.
func PackMetallicRoughness(occlusion, roughness, metalness *Image) (*Image, error) {
	width, height := 0, 0
	for _, candidate := range []*Image{occlusion, roughness, metalness} {
		if candidate == nil {
			continue
		}
		if width == 0 {
			width, height = candidate.Width, candidate.Height
			continue
		}
		if candidate.Width != width || candidate.Height != height {
			return nil, fmt.Errorf("%w: metallic-roughness inputs disagree on size", ErrShape)
		}
	}
	if width == 0 {
		return nil, fmt.Errorf("%w: metallic-roughness needs at least one input", ErrShape)
	}
	return Pack(width, height, []Source{
		{Image: occlusion, Channel: ChannelRed, Constant: 1},
		{Image: roughness, Channel: ChannelRed, Constant: 1},
		{Image: metalness, Channel: ChannelRed, Constant: 1},
	})
}
