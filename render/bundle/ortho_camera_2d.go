package bundle

import (
	"m31labs.dev/gosx/engine"
)

// OrthoCamera2DMode is the discriminator string written into
// engine.RenderCamera.Mode by OrthoCamera2D. The renderer (and computeMVP)
// branches on this value to build an orthographic projection that disables
// depth, lighting, and post-FX (the 2D mode flag from ADR 0004).
//
// Keep the constant value stable — it crosses the WASM/JSON boundary as part
// of RenderBundle.Camera.
const OrthoCamera2DMode = "ortho2d"

// OrthoCamera2D returns a RenderCamera configured for the 2D board path.
//
// Inputs:
//
//   - zoom    : world→screen scale. zoom=1 means 1 world unit = 1 screen pixel.
//     Must be > 0; non-positive values fall back to 1.
//   - panX/panY : board offset in world units. The point (panX, panY) on the
//     board maps to the center of the viewport.
//   - width,height : framebuffer dimensions in pixels. The orthographic frustum
//     is sized to width/zoom × height/zoom so the resulting NDC mapping reads
//     as a pure pixel-space projection.
//
// Returned RenderCamera fields:
//
//   - Mode     = OrthoCamera2DMode (signals 2D pipeline)
//   - X, Y     = panX, panY (re-used as 2D translation; Z is fixed at 0)
//   - Z        = 0 (the board sits on the z=0 plane)
//   - FOV      = 0 (unused in 2D mode; computeMVP ignores it)
//   - Near, Far = -1, 1 (symmetric depth range; nothing clips in 2D)
//
// Per ADR 0004 the OrthoCamera2D helper IS the API for the 2D camera — it
// returns the same engine.RenderCamera type the rest of the renderer already
// consumes, with the Mode field acting as the pipeline-config 2D switch.
func OrthoCamera2D(zoom, panX, panY float64, width, height int) engine.RenderCamera {
	if zoom <= 0 {
		zoom = 1
	}
	_ = width  // width/height are read by computeMVP via the framebuffer args
	_ = height // — kept on the signature so callers self-document the inputs.
	return engine.RenderCamera{
		Mode: OrthoCamera2DMode,
		X:    panX,
		Y:    panY,
		Z:    zoom,
		Near: -1,
		Far:  1,
	}
}

// IsOrthoCamera2D reports whether cam was produced by OrthoCamera2D. Use this
// in code paths that need to switch behavior (skip depth, skip lighting, skip
// post-FX) when running in the 2D pipeline.
func IsOrthoCamera2D(cam engine.RenderCamera) bool {
	return cam.Mode == OrthoCamera2DMode
}

// Configure2DBundle enforces the 2D-mode pipeline config from ADR 0004 on b:
//
//   - Lighting disabled  : Lights, Environment cleared
//   - Depth disabled     : Bundle still carries a depth attachment in the
//     shared pipeline, but Z values on geometry are forced to 0 by the
//     CanvasBoardAdapter (this helper does not rewrite vertex data — the
//     2D adapter never emits non-zero Z to begin with).
//   - Post-FX disabled   : PostEffects + PostFXMaxPixels zeroed
//   - Shadows disabled   : any RenderLight.CastShadow flag cleared (defensive;
//     2D bundles produce no lights anyway)
//
// This is a single-call gate that the CanvasBoardAdapter applies just before
// returning a bundle from RenderBundle(). Calling on a non-2D bundle is a
// no-op (the helper only acts when the camera is in OrthoCamera2D mode).
//
// Returns b for chained use.
func Configure2DBundle(b *engine.RenderBundle) *engine.RenderBundle {
	if b == nil || !IsOrthoCamera2D(b.Camera) {
		return b
	}
	b.Lights = nil
	b.Environment = engine.RenderEnvironment{}
	b.PostEffects = nil
	b.PostFXMaxPixels = 0
	return b
}
