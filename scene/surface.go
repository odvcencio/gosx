package scene

// SurfaceOptions changes live renderer options without rebuilding the graph.
// Nil fields preserve the current setting. Zero frame rate restores the default ambient cap and display-rate input.
type SurfaceOptions struct {
	MaxFrameRate    *float64
	PostFXMaxPixels *int
}
