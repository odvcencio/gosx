// Package headless is a server-side, pure-Go implementation of gpu.Device
// that renders into a CPU-backed RGBA framebuffer instead of a real GPU.
//
// # Why
//
// Two GoSX use cases want GPU-driven rendering without a browser:
//
//  1. Thumbnail generation — a workspace/scene preview on a saved asset,
//     rendered server-side and cached as a PNG.
//  2. Server-side validation — CI-run regression tests that render a
//     canonical scene and pixel-diff against a golden image, with no
//     WebGPU driver dependency.
//
// Both need "the render/bundle.Renderer runs, produces pixels." Neither
// needs a physically-correct PBR image. This package delivers pixels.
//
// # Current scope (R5 scaffold)
//
//   - Full gpu.Device interface implementation. A render/bundle.Renderer
//     built on top of this device constructs all pipelines, uniforms,
//     bind groups, and command encoders without error.
//   - Clear operations are executed: color attachments with LoadOpClear
//     fill the framebuffer with the clear value on pass begin. This is
//     enough to validate the bundle pipeline and produce non-trivial
//     thumbnails when scenes animate.
//   - Resource tracking: buffers/textures record their size + usage so
//     tests can assert without pixels.
//
// # Explicitly deferred
//
//   - Real triangle rasterization. A proper software rasterizer is its
//     own project — probably a sibling package at render/gpu/raster. For
//     now draw calls are no-ops; the framebuffer shows clear color plus
//     anything WriteTexture blitted in.
//   - Multi-sample, mipmaps, cubemaps.
//
// # Breakout path
//
// If the rasterizer grows into a real engineering effort, this package
// and render/gpu stay stable. The rasterizer lands as a sibling that
// wraps a headless.Device and fills in the draw-call ops.
package headless
