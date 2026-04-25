package engine

import (
	"encoding/json"
	"math"
)

// PixelSurface creates an engine Config for a managed pixel framebuffer.
// The runtime creates a canvas at the logical resolution, handles
// GPU-accelerated scaling to the mount element, and exposes the raw RGBA
// buffer to the engine.
//
// This is the simplest way to get hardware-accelerated 2D rendering in GoSX.
// Write pixels into the buffer, call present, and the framework handles the rest.
func PixelSurface(name string, width, height int, opts ...PixelSurfaceOption) Config {
	ps := &PixelSurfaceConfig{
		Width:   width,
		Height:  height,
		Scaling: ScalePixelPerfect,
	}
	for _, opt := range opts {
		opt(ps)
	}

	caps := []Capability{CapPixelSurface, CapCanvas}
	propsJSON, _ := json.Marshal(map[string]any{
		"width":  width,
		"height": height,
	})

	return Config{
		Name:                 name,
		Kind:                 KindSurface,
		MountID:              "gosx-pixel-" + name,
		Capabilities:         caps,
		RequiredCapabilities: caps,
		PixelSurface:         ps,
		Props:                propsJSON,
	}
}

// PixelSurfaceOption configures a pixel surface.
type PixelSurfaceOption func(*PixelSurfaceConfig)

// WithScaling sets the scaling mode for the pixel surface.
func WithScaling(mode ScalingMode) PixelSurfaceOption {
	return func(ps *PixelSurfaceConfig) {
		ps.Scaling = mode
	}
}

// WithClearColor sets the letterbox background color.
func WithClearColor(r, g, b, a uint8) PixelSurfaceOption {
	return func(ps *PixelSurfaceConfig) {
		ps.ClearColor = [4]uint8{r, g, b, a}
	}
}

// WithVSync enables or disables vertical sync.
func WithVSync(enabled bool) PixelSurfaceOption {
	return func(ps *PixelSurfaceConfig) {
		ps.VSync = &enabled
	}
}

// ScalingTransform computes the pixel-perfect or fill scaling transform for a
// logical buffer displayed on a surface of the given size. Returns the scale
// factor and the offset (in surface pixels) where the scaled buffer begins.
func ScalingTransform(bufW, bufH, surfW, surfH int, mode ScalingMode) (scale float64, offsetX, offsetY float64) {
	if bufW <= 0 || bufH <= 0 || surfW <= 0 || surfH <= 0 {
		return 1, 0, 0
	}

	scaleX := float64(surfW) / float64(bufW)
	scaleY := float64(surfH) / float64(bufH)

	switch mode {
	case ScalePixelPerfect:
		s := math.Min(scaleX, scaleY)
		scale = math.Floor(s)
		if scale < 1 {
			scale = 1
		}
	case ScaleStretch:
		// Non-uniform, just return the X factor; caller handles Y separately.
		return scaleX, 0, 0
	default: // ScaleFill
		scale = math.Min(scaleX, scaleY)
	}

	scaledW := float64(bufW) * scale
	scaledH := float64(bufH) * scale
	offsetX = (float64(surfW) - scaledW) / 2
	offsetY = (float64(surfH) - scaledH) / 2

	return scale, offsetX, offsetY
}

// WindowToPixel converts a window/surface coordinate to the logical pixel
// coordinate in the buffer. Returns the pixel position and whether the point
// is inside the buffer bounds.
func WindowToPixel(windowX, windowY float64, bufW, bufH, surfW, surfH int, mode ScalingMode) (pixelX, pixelY int, inside bool) {
	scale, offsetX, offsetY := ScalingTransform(bufW, bufH, surfW, surfH, mode)
	if scale <= 0 {
		return 0, 0, false
	}

	px := (windowX - offsetX) / scale
	py := (windowY - offsetY) / scale

	pixelX = int(px)
	pixelY = int(py)
	inside = pixelX >= 0 && pixelX < bufW && pixelY >= 0 && pixelY < bufH
	return pixelX, pixelY, inside
}
