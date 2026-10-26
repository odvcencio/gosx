package scene

// TextureDeliveryProfile caps the resolution of model material maps selected
// for one Scene3D mount. Empty retains the pre-profile selection policy.
type TextureDeliveryProfile string

const (
	TextureDeliveryAuto     TextureDeliveryProfile = "auto"
	TextureDeliveryLow      TextureDeliveryProfile = "low"
	TextureDeliveryStandard TextureDeliveryProfile = "standard"
	TextureDeliveryHigh     TextureDeliveryProfile = "high"
)

// SurfaceOptions changes live renderer options without rebuilding the graph.
// Nil fields preserve the current setting. Zero frame rate restores the default ambient cap and display-rate input.
type SurfaceOptions struct {
	MaxFrameRate    *float64
	PostFXMaxPixels *int
}
