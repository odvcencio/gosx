// Package bundle is GoSX's high-level renderer. It consumes the
// engine.RenderBundle data structure — the declarative, batched per-frame
// payload produced by the engine runtime — and issues draw commands against
// a render/gpu.Device.
//
// Current scope spans the R-series render path: legacy unlit pass bundles,
// instanced lit meshes, PBR material uniforms/textures, cascaded shadows,
// HDR present/post-FX, GPU picking, compute particles, skinning palette
// helpers, and headless-testable resource uploads.
//
// The Renderer holds GPU resources keyed by lifecycle:
//
//   - Pipeline: created once, reused across frames.
//   - Vertex buffers: cached by RenderPassBundle.CacheKey where provided;
//     otherwise rebuilt each frame.
//   - Uniform/storage buffers: scene, shadow, material, cull, particle, and
//     optional skinning state are uploaded as bundle data changes.
//
// Callers drive the renderer from the client/wasm engine runtime; a new
// runtime entry point __gosx_render_engine_to_canvas will dispatch frames
// here once the renderer is wired into the engine lifecycle.
package bundle
