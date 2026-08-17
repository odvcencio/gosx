package imagepipe

import "fmt"

// Variant is one resized, encoded rung of a source image: Data is the
// finished, ready-to-write file content for Width at Format.
type Variant struct {
	Width  int
	Format Format
	Data   []byte
}

// Process is the package's one build-time entry point: it probes and fully
// decodes path once, resizes the decoded image to each width in widths
// (already capped by Ladder -- Process does not cap them itself), encodes
// every resize at every format in formats, and returns the source's
// intrinsic Dimensions plus every encoded Variant.
//
// Process performs no disk writes; the caller content-hashes and writes
// each Variant.Data itself.
func Process(path string, widths []int, formats []Format, opts EncodeOptions) (Dimensions, []Variant, error) {
	img, _, err := Decode(path)
	if err != nil {
		return Dimensions{}, nil, err
	}

	bounds := img.Bounds()
	dims := Dimensions{Width: bounds.Dx(), Height: bounds.Dy()}
	if dims.Width <= 0 || dims.Height <= 0 {
		return Dimensions{}, nil, fmt.Errorf("imagepipe: %s has no pixels (%dx%d)", path, dims.Width, dims.Height)
	}

	variants := make([]Variant, 0, len(widths)*len(formats))
	for _, width := range widths {
		resized, err := Resize(img, width)
		if err != nil {
			return Dimensions{}, nil, fmt.Errorf("imagepipe: resize %s to %dpx: %w", path, width, err)
		}
		for _, format := range formats {
			data, err := Encode(resized, format, opts)
			if err != nil {
				return Dimensions{}, nil, fmt.Errorf("imagepipe: encode %s at %dpx %s: %w", path, width, format, err)
			}
			variants = append(variants, Variant{Width: width, Format: format, Data: data})
		}
	}
	return dims, variants, nil
}
