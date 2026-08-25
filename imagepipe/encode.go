package imagepipe

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"sync"

	tqwebp "m31labs.dev/tqwebp"
)

// Format identifies a build-time output image encoding.
type Format string

const (
	// FormatWebP names the built-in, pure-Go tqwebp output format.
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

// defaultJPEGQuality retains GoSX's historical JPEG default. WebP uses
// tqwebp.DefaultQuality when EncodeOptions.Quality is zero.
const defaultJPEGQuality = 82

// EncodeOptions configures Encode and Process.
type EncodeOptions struct {
	// Quality is passed to the format's own encoder, in [1,100]. Zero (the
	// default) selects a per-format default: 82 for JPEG and
	// tqwebp.DefaultQuality for WebP. PNG ignores Quality; it is always
	// lossless. A registered Encoder decides its own zero-value behavior.
	Quality int
}

// Encoder produces encoded image bytes for one build-time output Format.
// RegisterEncoder adds a new format or overrides the built-in WebP encoder.
// JPEG and PNG cannot be overridden.
type Encoder interface {
	Encode(img image.Image, opts EncodeOptions) ([]byte, error)
}

// EncoderFunc adapts a plain function into an Encoder.
type EncoderFunc func(img image.Image, opts EncodeOptions) ([]byte, error)

// Encode calls f.
func (f EncoderFunc) Encode(img image.Image, opts EncodeOptions) ([]byte, error) {
	return f(img, opts)
}

var (
	encoderMu       sync.RWMutex
	encoderRegistry = map[Format]Encoder{}
)

// RegisterEncoder registers enc as Encode's (and so also Process's)
// implementation for format. A later call for the same format replaces the
// previous registration -- "last one wins", the same convention
// server.RegisterImageResolver already uses for its own named registry.
//
// format must not be FormatJPEG or FormatPNG: both are built into Encode and
// never consult this registry. FormatWebP remains registrable for backward
// compatibility with projects that supplied an encoder before tqwebp became
// the built-in default.
func RegisterEncoder(format Format, enc Encoder) error {
	if format == FormatJPEG || format == FormatPNG {
		return fmt.Errorf("imagepipe: format %q is built in and cannot be overridden", format)
	}
	if strings.TrimSpace(string(format)) == "" {
		return fmt.Errorf("imagepipe: format is required")
	}
	if enc == nil {
		return fmt.Errorf("imagepipe: encoder for format %q is nil", format)
	}
	encoderMu.Lock()
	encoderRegistry[format] = enc
	encoderMu.Unlock()
	return nil
}

// UnregisterEncoder removes a RegisterEncoder registration for format, if
// any -- a no-op if none is registered. It exists mainly for tests: an
// ordinary build process registers an encoder once, at startup, and never
// needs to remove it.
func UnregisterEncoder(format Format) {
	encoderMu.Lock()
	delete(encoderRegistry, format)
	encoderMu.Unlock()
}

// EncoderRegistered reports whether format has a registered Encoder.
// It reports explicit registry state, not whether a format has a built-in
// encoder.
func EncoderRegistered(format Format) bool {
	encoderMu.RLock()
	_, ok := encoderRegistry[format]
	encoderMu.RUnlock()
	return ok
}

// Encode encodes img at the given format and returns the finished bytes.
//
// FormatJPEG and FormatPNG use the standard library directly. FormatWebP
// uses a registered override when present and otherwise the pure-Go tqwebp
// encoder. Every other format requires a RegisterEncoder registration.
func Encode(img image.Image, format Format, opts EncodeOptions) ([]byte, error) {
	var buf bytes.Buffer

	switch format {
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
	case FormatWebP:
		encoderMu.RLock()
		enc, ok := encoderRegistry[format]
		encoderMu.RUnlock()
		if ok {
			data, err := enc.Encode(img, opts)
			if err != nil {
				return nil, fmt.Errorf("imagepipe: encode %s: %w", format, err)
			}
			return data, nil
		}

		quality := opts.Quality
		if quality < 0 {
			quality = 0
		} else if quality > 100 {
			quality = 100
		}
		if err := tqwebp.Encode(&buf, img, &tqwebp.Options{Quality: quality}); err != nil {
			return nil, fmt.Errorf("imagepipe: encode webp: %w", err)
		}
	default:
		encoderMu.RLock()
		enc, ok := encoderRegistry[format]
		encoderMu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("imagepipe: no encoder registered for format %q (jpeg, png, and webp are built in); call imagepipe.RegisterEncoder(%q, ...) before Encode/Process to add one", format, format)
		}
		data, err := enc.Encode(img, opts)
		if err != nil {
			return nil, fmt.Errorf("imagepipe: encode %s: %w", format, err)
		}
		return data, nil
	}

	return buf.Bytes(), nil
}
