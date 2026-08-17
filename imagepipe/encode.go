package imagepipe

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"sync"
)

// Format identifies a build-time output image encoding.
type Format string

const (
	// FormatWebP names the WebP output format. gosx ships no WebP encoder
	// (no foreign wasm runtime, no FFI shim -- see this package's doc
	// comment): Encode and Process only ever produce FormatWebP output for
	// a project that first calls RegisterEncoder(FormatWebP, ...) itself.
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
	// default) selects a per-format default: 82 for JPEG, matching
	// server/image.go's own default. PNG ignores Quality; it is always
	// lossless. A registered Encoder decides its own default for a
	// zero Quality.
	Quality int
}

// Encoder produces encoded image bytes for one build-time output Format
// that Encode does not build in. Register an implementation with
// RegisterEncoder to add an output format -- for example WebP, via
// github.com/gen2brain/webp or any other encoder -- to gosx build's own
// binary, without that dependency ever reaching a deployed application (see
// this package's doc comment for the isolation this buys).
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
// format must not be FormatJPEG or FormatPNG: both are built into Encode
// and never consult this registry.
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
// cmd/gosx's build stage consults this before adding a non-native format to
// one source's encode list, so a project that never registers an encoder
// for a format never asks Process to produce it (see imagePipeExtraFormats
// in cmd/gosx/imagepipe_stage.go).
func EncoderRegistered(format Format) bool {
	encoderMu.RLock()
	_, ok := encoderRegistry[format]
	encoderMu.RUnlock()
	return ok
}

// Encode encodes img at the given format and returns the finished bytes.
//
// FormatJPEG and FormatPNG use the standard library directly. Every other
// format -- including FormatWebP -- looks up a RegisterEncoder
// registration; Encode returns a clear, extension-point-naming error when
// none is registered, rather than silently falling back to a different
// format or linking a foreign runtime to provide one by default.
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
	default:
		encoderMu.RLock()
		enc, ok := encoderRegistry[format]
		encoderMu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("imagepipe: no encoder registered for format %q -- gosx ships no built-in encoder for it (jpeg and png are the only built-ins); call imagepipe.RegisterEncoder(%q, ...) before Encode/Process to add one", format, format)
		}
		data, err := enc.Encode(img, opts)
		if err != nil {
			return nil, fmt.Errorf("imagepipe: encode %s: %w", format, err)
		}
		return data, nil
	}

	return buf.Bytes(), nil
}
