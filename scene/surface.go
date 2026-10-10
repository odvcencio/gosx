package scene

// SurfaceOptions changes live renderer options without rebuilding the graph.
// Nil fields preserve the current setting. Zero frame rate follows the display.
type SurfaceOptions struct {
	MaxFrameRate    *float64
	PostFXMaxPixels *int
}
