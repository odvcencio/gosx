// Package bundle is GoSX's high-level renderer. It consumes the
// engine.RenderBundle data structure — the declarative, batched per-frame
// payload produced by the engine runtime — and issues draw commands against
// a render/gpu.Device.
//
// R1 scope: unlit pipeline only. The renderer accepts bundles containing
// RenderPassBundle geometry plus RenderInstancedMesh entries, uploads the
// vertex/instance/uniform data to GPU buffers, and draws them. Shadow maps,
// PBR, textures, and post-FX land in R2–R4.
//
// The Renderer holds GPU resources keyed by lifecycle:
//
//   - Pipeline: created once, reused across frames.
//   - Vertex buffers: cached by RenderPassBundle.CacheKey where provided;
//     otherwise rebuilt each frame.
//   - Uniform buffer: a single ring buffer slot per draw (R1) — R2
//     revisits this with proper multi-frame in-flight uniforms.
//
// Callers drive the renderer from the client/wasm engine runtime; a new
// runtime entry point __gosx_render_engine_to_canvas will dispatch frames
// here once the renderer is wired into the engine lifecycle.
package bundle
