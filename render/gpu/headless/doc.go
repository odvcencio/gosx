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
// Both need "the render/bundle.Renderer runs, produces pixels."
//
// A third use case arrived with three.js material parity: this package is the
// only GPU-free oracle in the repository, so it also has to answer "does this
// material field reach a pixel, and with what value". It ran a Lambert term until
// 2026-07-26, which answered "no" for eleven of the material fields. It now runs
// the whole fragment stage of litWGSL in render/bundle/lit.go.
//
// # Current scope
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
//   - The lit path reads the same scene lighting uniform block and shadow-map
//     binding as the WebGPU shader. litProgram.shade in device.go runs the whole
//     fragment stage of litWGSL per covered pixel: a Cook-Torrance specular lobe
//     (GGX distribution, Hammon correlated Smith visibility, Schlick Fresnel), an
//     energy-conserving diffuse lobe, a three-term ambient dome, cubemap
//     image-based lighting, and linear comparison cascaded-shadow sampling.
//   - Lit materials read every lane of the material uniform block and all five
//     texture bindings: base colour, normal, roughness, metalness and emissive.
//     Every map is sampled per pixel with linear repeat-addressed UV filtering.
//     Clear coat, sheen, iridescence, anisotropy, transmission and emissive all
//     reach a pixel. render/gpu/headless/material_gap_test.go measures each one.
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
// # Approximation limits
//
//   - No tone mapping and no exposure. The browser path writes the lit colour to
//     a high dynamic range target and tone maps it on the way to the swap chain.
//     Every post-effect pass is an identity copy here, so a headless frame is the
//     raw linear value clipped to the byte range. A frame therefore reads darker
//     than the browser, and a bright emissive material clips instead of rolling
//     off. This is the largest remaining parity gap in the package.
//   - One directional light. The browser walks a runtime-sized array of six light
//     kinds; this package reads the first directional light and ignores the rest.
//   - Soft-shadow filtering, exact billboard axes, colour-space parity, and
//     multi-sample rasterization.
//   - No wireframe. The flag never reaches the material uniform.
//
// # Breakout path
//
// If the rasterizer grows into a real engineering effort, this package
// and render/gpu stay stable. The rasterizer lands as a sibling that
// wraps a headless.Device and fills in the draw-call ops.
package headless
