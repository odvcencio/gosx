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

// FrameStats is what a Scene3D mount measured over its last drawn frames (up
// to 120, about two seconds at 60 frames a second), for a readout a player can
// turn on. Surface.FrameStats reads it; it costs the renderer a few stores a
// frame and is worked out only when read.
type FrameStats struct {
	// Backend is the renderer drawing the mount: "webgpu" or "webgl"; "" until
	// one starts. Adapter is the GPU as the browser names it ("" when the
	// browser does not say). FallbackReason says why the mount is not on
	// WebGPU, when it is not.
	Backend        string `json:"backend"`
	Adapter        string `json:"adapter"`
	FallbackReason string `json:"fallbackReason"`
	// Tier is the quality tier the governor chose ("full", "balanced", ...,
	// or a ladder rung's name); Governed is false when the mount has no
	// governor.
	Tier     string `json:"tier"`
	Governed bool   `json:"governed"`
	// Frames is how many frames the window holds. FPS is the frames drawn a
	// second over it; FrameMS and FrameP95MS are the mean and 95th percentile
	// of the time between two drawn frames.
	Frames     int     `json:"frames"`
	FPS        float64 `json:"fps"`
	FrameMS    float64 `json:"frameMS"`
	FrameP95MS float64 `json:"frameP95MS"`
	// CPUMS and CPUP95MS are the main thread's work for a frame, from the
	// frame's start (an engine's frame hook included) until the renderer has
	// submitted it.
	CPUMS    float64 `json:"cpuMS"`
	CPUP95MS float64 `json:"cpuP95MS"`
	// GPUMS and GPUP95MS are the GPU's time for a frame, over the GPUSamples
	// frames the GPU timer reported in the window. GPUTimer names that timer
	// ("gpu-timestamp" on WebGPU, "webgl-timer" on WebGL); it is "" and the
	// GPU times are 0 when the GPU has no timer the browser exposes.
	GPUMS      float64 `json:"gpuMS"`
	GPUP95MS   float64 `json:"gpuP95MS"`
	GPUSamples int     `json:"gpuSamples"`
	GPUTimer   string  `json:"gpuTimer"`
	// PixelRatio is the device pixels drawn per CSS pixel; Width and Height
	// are the mount's size in CSS pixels.
	PixelRatio float64 `json:"pixelRatio"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
}
