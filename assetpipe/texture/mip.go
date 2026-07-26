package texture

import "fmt"

// MipLevelCount returns the full mip chain length for one base size, counting
// level zero and ending at one by one.
func MipLevelCount(width, height int) int {
	levels := 1
	for width > 1 || height > 1 {
		width = halve(width)
		height = halve(height)
		levels++
	}
	return levels
}

func halve(v int) int {
	if v <= 1 {
		return 1
	}
	return v / 2
}

// MipOptions controls the mip chain builder.
type MipOptions struct {
	// Filter selects the resample kernel. Zero means Lanczos3.
	Filter Filter
	// Levels caps the chain length. Zero builds the full chain down to one
	// by one.
	Levels int
	// AlphaAware premultiplies before filtering and restores straight alpha
	// afterwards. Leave it on unless the alpha channel is constant.
	AlphaAware bool
}

// MipChain builds the mip chain of img in linear light.
//
// Each level resamples from level zero with a widened kernel, not from the
// level above it. Resampling from the base keeps the full source detail in
// every level. A successive-halving pyramid applies the kernel again and again,
// so level five carries five rounds of blur.
//
// The chain is correct in linear light because Image already holds linear
// values. This is the error the package exists to avoid: averaging sRGB code
// values weights dark texels too heavily, so every level below zero comes out
// darker than the level above it. TestMipChainKeepsLinearMean asserts the
// linear mean survives.
func MipChain(img *Image, opts MipOptions) ([]*Image, error) {
	if img == nil || img.Width < 1 || img.Height < 1 {
		return nil, fmt.Errorf("%w: empty source", ErrShape)
	}
	total := MipLevelCount(img.Width, img.Height)
	if opts.Levels > 0 && opts.Levels < total {
		total = opts.Levels
	}

	base := img
	if opts.AlphaAware && img.Alpha == AlphaStraight {
		base = img.Clone()
		Premultiply(base)
	}

	out := make([]*Image, 0, total)
	width, height := img.Width, img.Height
	for level := 0; level < total; level++ {
		if level == 0 {
			out = append(out, base.Clone())
		} else {
			width, height = halve(width), halve(height)
			resized, err := Resize(base, width, height, opts.Filter)
			if err != nil {
				return nil, fmt.Errorf("mip level %d: %w", level, err)
			}
			out = append(out, resized)
		}
	}

	if opts.AlphaAware && img.Alpha == AlphaStraight {
		for _, level := range out {
			Unpremultiply(level)
		}
	}
	return out, nil
}
