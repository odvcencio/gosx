package imagepipe

import (
	"fmt"
	"image"

	"golang.org/x/image/draw"
)

// Resize scales img to targetWidth, preserving aspect ratio, with
// golang.org/x/image/draw's Catmull-Rom resampler -- the same resampler
// server/image.go's request-time optimizer uses (renderImageVariant), so a
// build-time variant and a hand-requested runtime one look alike.
//
// Resize never upscales: it returns an error if targetWidth exceeds the
// source's own width, or is not positive. Ladder is the caller's usual
// source of already-capped widths; call it before Resize rather than
// relying on this check as the only guard.
//
// If targetWidth already equals the source width, Resize returns img
// unchanged -- no resample, no extra allocation -- since there is nothing
// to scale.
func Resize(img image.Image, targetWidth int) (image.Image, error) {
	if targetWidth <= 0 {
		return nil, fmt.Errorf("imagepipe: resize target width must be positive, got %d", targetWidth)
	}
	bounds := img.Bounds()
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return nil, fmt.Errorf("imagepipe: source image has no pixels (%dx%d)", sourceWidth, sourceHeight)
	}
	if targetWidth > sourceWidth {
		return nil, fmt.Errorf("imagepipe: refusing to upscale from %dpx to %dpx", sourceWidth, targetWidth)
	}
	if targetWidth == sourceWidth {
		return img, nil
	}

	targetHeight := max(1, int(float64(sourceHeight)*(float64(targetWidth)/float64(sourceWidth))))
	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
	return dst, nil
}
