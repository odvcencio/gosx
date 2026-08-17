package imagepipe

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	"github.com/gen2brain/webp"
)

// Format identifies a build-time output image encoding.
type Format string

const (
	FormatWebP Format = "webp"
	FormatJPEG Format = "jpeg"
	FormatPNG  Format = "png"
)

// Ext returns the filename extension (including the leading dot) gosx build
// writes hashed variant files with for this format.
func (f Format) Ext() string {
	switch f {
	case FormatWebP:
		return ".webp"
	case FormatJPEG:
		return ".jpg"
	case FormatPNG:
		return ".png"
	default:
		return ""
	}
}

// defaultJPEGQuality matches server/image.go's own request-time optimizer
// default (encodeImageVariant), so a build-time JPEG variant and a
// hand-requested runtime one compress alike when neither names a quality.
const defaultJPEGQuality = 82

// EncodeOptions configures Encode and Process.
type EncodeOptions struct {
	// Quality is passed to the format's own encoder, in [1,100]. Zero (the
	// default) selects a per-format default: 75 for WebP
	// (webp.DefaultQuality) and 82 for JPEG, matching
	// server/image.go's own default. PNG ignores Quality; it is always
	// lossless.
	Quality int
}

// Encode encodes img at the given format and returns the finished bytes.
//
// FormatWebP is lossy WebP via github.com/gen2brain/webp (libwebp under
// wazero, pinned to v0.5.5 in go.mod -- see the CHANGELOG entry for issue
// #200 for why v0.6.x is not safe to build with today). FormatJPEG and
// FormatPNG use the standard library.
func Encode(img image.Image, format Format, opts EncodeOptions) ([]byte, error) {
	var buf bytes.Buffer

	switch format {
	case FormatWebP:
		quality := opts.Quality
		if quality <= 0 {
			quality = webp.DefaultQuality
		} else if quality > 100 {
			quality = 100
		}
		if err := webp.Encode(&buf, img, webp.Options{Quality: quality, Method: webp.DefaultMethod}); err != nil {
			return nil, fmt.Errorf("imagepipe: encode webp: %w", err)
		}
	case FormatJPEG:
		quality := opts.Quality
		if quality <= 0 {
			quality = defaultJPEGQuality
		} else if quality > 100 {
			quality = 100
		}
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, fmt.Errorf("imagepipe: encode jpeg: %w", err)
		}
	case FormatPNG:
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("imagepipe: encode png: %w", err)
		}
	default:
		return nil, fmt.Errorf("imagepipe: unsupported output format %q", format)
	}

	return buf.Bytes(), nil
}
