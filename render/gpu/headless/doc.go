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
//   - Clear operations are executed: surface and offscreen color attachments
//     with LoadOpClear retain their clear value on pass begin.
//   - Texture uploads and copyTextureToBuffer readbacks preserve CPU-side
//     bytes, including explicit mip levels, for the formats used by the
//     render milestones.
//   - The bundle present pass copies the retained HDR color target into the
//     CPU framebuffer, so headless frames follow the same HDR -> present path
//     as the browser renderer.
//   - The R1 unlit RenderPassBundle path has a narrow software rasterizer:
//     triangle-list position/color vertex buffers are transformed by the
//     bound MVP uniform and written into the color attachment.
//   - Instanced meshes can run through the renderer's cull compute pass and
//     DrawIndirect path. Headless treats all uploaded instances as visible,
//     then rasterizes the lit pipeline as a deterministic material/vertex-color
//     approximation.
//   - The lit approximation reads the same scene lighting uniform block and
//     shadow-map binding as the WebGPU shader, applying Lambertian directional
//     light, hemisphere ambient, and linear comparison cascaded-shadow
//     sampling.
//   - Lit materials read the material uniform block and base-color texture
//     binding, including linear repeat-addressed UV sampling for deterministic
//     texture-path validation.
//   - Main-pass depth clears, compares, and writes are honored for the
//     rasterized paths, so golden/thumbnail checks get the same nearest-pixel
//     ordering as the GPU renderer for simple scenes.
//   - Pipeline color write masks and the blend factors exposed by render/gpu
//     are applied when writing rasterized color targets.
//   - Fully near/far clipped geometry is rejected before rasterization.
//   - Depth-only shadow passes execute the same instanced draw path and write
//     into CPU-backed depth textures, including per-layer shadow views.
//   - Compute-particle update and render passes execute deterministically on
//     CPU: the update shader's state buffer advances, and the render pipeline
//     draws small additive discs into the HDR target.
//   - Resource tracking: buffers/textures record their size + usage so tests
//     can assert without a GPU driver.
//
// # Explicitly deferred
//
//   - Full WebGPU-equivalent rasterization. Cook-Torrance PBR lighting,
//     soft-shadow filtering, exact billboard axes, color-space parity, and
//     triangle-edge clipping are still approximated or no-op in headless.
//   - Multi-sample and cubemaps.
//
// # Breakout path
//
// If the rasterizer grows into a real engineering effort, this package
// and render/gpu stay stable. The rasterizer lands as a sibling that
// wraps a headless.Device and fills in the draw-call ops.
package headless
