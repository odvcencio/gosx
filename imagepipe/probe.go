package imagepipe

import (
	"fmt"
	"image"
	_ "image/gif"  // register GIF decoding for Probe/Decode
	_ "image/jpeg" // register JPEG decoding for Probe/Decode
	_ "image/png"  // register PNG decoding for Probe/Decode
	"os"

	_ "golang.org/x/image/webp" // register WebP decoding for Probe/Decode; cheap, pure-Go, no wazero runtime
)

// Dimensions holds an image's intrinsic pixel width and height.
type Dimensions struct {
	Width  int
	Height int
}

// Probe reads only the header of the file at path -- through
// image.DecodeConfig, so it never decodes pixel data -- and reports its
// intrinsic dimensions and the sniffed format name ("jpeg", "png", "gif",
// or "webp"). It returns an error for a source Probe cannot recognize or
// read.
func Probe(path string) (Dimensions, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return Dimensions{}, "", fmt.Errorf("imagepipe: open %s: %w", path, err)
	}
	defer f.Close()

	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return Dimensions{}, "", fmt.Errorf("imagepipe: probe %s: %w", path, err)
	}
	return Dimensions{Width: cfg.Width, Height: cfg.Height}, format, nil
}

// Decode fully decodes the image at path, using the same registered
// decoders Probe relies on, and reports the sniffed format name alongside
// the decoded image.
func Decode(path string) (image.Image, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("imagepipe: open %s: %w", path, err)
	}
	defer f.Close()

	img, format, err := image.Decode(f)
	if err != nil {
		return nil, "", fmt.Errorf("imagepipe: decode %s: %w", path, err)
	}
	return img, format, nil
}
