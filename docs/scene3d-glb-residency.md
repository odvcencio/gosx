# Scene3D GLB residency

A mounted Scene3D can prepare an upcoming GLB family without inserting invisible
instances or issuing draw calls:

```js
const result = await window.__gosx.scene3d.preloadGLB(mountElement, "/models/creep.glb", { mode: "crowd" });
if (result.gpuReady) {
  // Begin the wave using the same GLB URL and this mounted scene.
}
window.__gosx.scene3d.releasePreloadedGLB(mountElement, "/models/creep.glb");
```

`mode: "crowd"` prepares the skin atlas and shared instanced mesh streams;
`mode: "rigid"` prepares unskinned imported mesh streams. The call fetches and
decodes the asset in the mount's selected texture variant, uploads WebGL vertex
and index buffers, compiles the relevant instancing program, and waits for any
referenced material textures to finish uploading. It returns `decoded`,
`gpuReady`, `backend`, `mode`, and a reason when readiness cannot be guaranteed.
A missing mount, failed GLB, unsupported mesh kind, WebGPU/canvas backend, or
renderer replacement reports `gpuReady: false`. The earlier `preloadModel(src)`
API remains a neutral fetch/decode hook and makes no GPU readiness claim.

Residency is scoped to the mounted WebGL renderer. It holds at most eight
families and 48 MiB of explicitly pinned mesh streams, with a separate 32 MiB
crowd palette cap already enforced by WebGL. Least recently prepared families
lose their pins first. `releasePreloadedGLB` unpins one family and retires
buffers and textures used only by its preload; assets used by live actors
remain in the renderer cache. Renderer disposal releases
all pins, buffers, textures, shaders, and palettes. A context replacement
requires a new preload call.

`gpuReady` proves the explicit decode, atlas, geometry, shader, and material
texture work completed for this renderer. It does not promise zero first-use
CPU work: actor membership, transforms, draw-list preparation, and any new
material override still occur when actors spawn. Match the preloaded GLB and
material family to the eventual instances, and measure first-wave frame times
on the target GPU before claiming a stall reduction.
