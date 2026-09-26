# G09 — Real-WebGPU acceptance probe (headless Chromium + SwiftShader)

Depends on: G06. Scratch only: nothing in this task is committed.

## Goal

Prove on a real WebGPU implementation (Dawn/Tint over SwiftShader Vulkan) what
the fake device cannot:

1. every GPU-driven pipeline, layout and bind group validates (zero
   uncaptured errors, zero `[gosx] gpu-driven:` warnings);
2. the renderer's own frame, material and shadow bind groups bind to the
   host's pipelines (group-equivalent layouts, appendix B12);
3. the frame is pixel-identical to the classic path in frustum mode and in
   occlusion mode, with MSAA 1 and 4 (so both Hi-Z seed variants run);
4. occlusion actually culls: the camera survivor count drops.

## Setup

Use the preflight Chromium flags (01 step 6). Work in a scratch directory
outside the repo, for example `/tmp/gd-probe`, and copy the four bundles
built by G06 into it:

```sh
mkdir -p /tmp/gd-probe && cd /tmp/gd-probe
for f in bootstrap-runtime.js bootstrap-feature-scene3d.js bootstrap-feature-scene3d-compute.js bootstrap-feature-scene3d-webgpu.js; do
  cp <root>/gosx/client/js/$f .
done
```

Create the three files below, then serve the directory on 127.0.0.1 (WebGPU
needs a secure context) and run the driver:

```sh
python3 -m http.server 8941 --bind 127.0.0.1 &
node run.mjs
```

`run.mjs` imports Playwright from a global install and launches the
pre-installed Chromium; change the two paths at its top if yours differ
(`npm root -g` prints the global module directory).

### `page.html`

```html
<!doctype html><html><body>
<div id="mount"><canvas id="c" width="256" height="192"></canvas></div>
<script src="bootstrap-runtime.js"></script>
<script src="bootstrap-feature-scene3d.js"></script>
<script src="bootstrap-feature-scene3d-compute.js"></script>
<script type="module" src="probe.js"></script>
</body></html>
```

### `probe.js`

The probe patches `GPUCanvasContext.prototype.configure` to add `COPY_SRC`,
then copies the swapchain texture in the same task as the last render.
Reading the canvas back through a 2D `drawImage` returned transparent pixels
in headless SwiftShader, so do not use that.

```js
// Real-WebGPU acceptance probe for GPU-driven instancing (spec task G09).
// Renders one scene classic and GPU-driven and compares the pixels.
const out = { errors: [], warnings: [], cases: {} };
window.__gdL3 = null;
const origWarn = console.warn;
console.warn = function(...args) { out.warnings.push(args.map(String).join(" ")); origWarn.apply(console, args); };
try {
  const adapter = await navigator.gpu.requestAdapter();
  const device = await adapter.requestDevice();
  device.addEventListener("uncapturederror", (e) => out.errors.push(String(e.error && e.error.message)));
  window.__gosx_scene3d_webgpu_probe = () => ({ adapter, device, ready: true });
  // Let the probe copy the swapchain texture back.
  const configure = GPUCanvasContext.prototype.configure;
  GPUCanvasContext.prototype.configure = function(cfg) {
    return configure.call(this, Object.assign({}, cfg, { usage: (cfg.usage || GPUTextureUsage.RENDER_ATTACHMENT) | GPUTextureUsage.COPY_SRC }));
  };
  await new Promise((resolve, reject) => {
    const s = document.createElement("script");
    s.src = "bootstrap-feature-scene3d-webgpu.js"; s.onload = resolve; s.onerror = reject;
    document.head.appendChild(s);
  });
  const api = window.__gosx_scene3d_api;
  const canvas = document.getElementById("c");
  const mount = document.getElementById("mount");
  const renderer = window.__gosx_scene3d_webgpu_api.createRenderer(canvas, {});

  function transforms() {
    const t = [];
    // 400 small boxes on a grid behind the wall, 100 in front of it.
    for (let i = 0; i < 500; i++) {
      const x = (i % 25) - 12, z = i < 400 ? -6 - Math.floor(i / 25) : 4 - Math.floor((i - 400) / 25) * 0.5;
      const y = i < 400 ? 0 : -2.2;
      const s = 0.4 + (i % 3) * 0.1;
      t.push(s, 0, 0, 0, 0, s, 0, 0, 0, 0, s, 0, x * 0.9, y, z, 1);
    }
    return t;
  }
  const colors = Array.from({ length: 500 }, (_, i) => ["#ff5533", "#33aaff", "#88ee44"][i % 3]);
  function state(gpuDriven) {
    const scene = {
      lights: [
        { id: "sun", kind: "directional", castShadow: true, directionX: -0.4, directionY: -1, directionZ: -0.6, intensity: 1.6 },
        { id: "fill", kind: "ambient", intensity: 0.3 },
      ],
      instancedMeshes: [
        { id: "wall", count: 1, kind: "box", width: 30, height: 12, depth: 0.5, transforms: [1,0,0,0, 0,1,0,0, 0,0,1,0, 0,0,-3,1], color: "#cccccc" },
        { id: "boxes", count: 500, kind: "box", width: 1, height: 1, depth: 1, transforms: transforms(), colors, castShadow: true },
      ],
    };
    if (gpuDriven) scene.gpuDriven = gpuDriven;
    return api.createSceneState({ scene }, { tier: "full" });
  }
  async function draw(st, msaa, frames) {
    for (let f = 0; f < frames; f++) {
      const b = api.createSceneRenderBundle(256, 192, "#101820", { x: 0, y: 1.5, z: 14, fov: 55, near: 0.1, far: 120 },
        [], [], [], [], api.sceneStateLights(st), {}, 0, [], api.sceneStateInstancedMeshesWithMaterials(st), [], [], [], 0, false);
      b.gpuDriven = st.gpuDriven;
      b.msaaSamples = msaa;
      renderer.render(b, { width: 256, height: 192 }, { nowMS: 1000, active: true });
      if (f === frames - 1) {
        // Same task as the render: the canvas texture is not presented yet.
        const tex = canvas.getContext("webgpu").getCurrentTexture();
        const buf = device.createBuffer({ size: 1024 * 192, usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ });
        const enc = device.createCommandEncoder();
        enc.copyTextureToBuffer({ texture: tex }, { buffer: buf, bytesPerRow: 1024 }, [256, 192]);
        device.queue.submit([enc.finish()]);
        await buf.mapAsync(GPUMapMode.READ);
        const data = new Uint8Array(buf.getMappedRange().slice(0));
        buf.unmap();
        await device.queue.onSubmittedWorkDone();
        await new Promise((r) => setTimeout(r, 30));
        return data;
      }
      await device.queue.onSubmittedWorkDone();
      await new Promise((r) => setTimeout(r, 30));
    }
  }
  function attrs() {
    const o = {};
    for (const n of ["active", "reason", "meshes", "instances", "occlusion", "dispatches", "camera-visible", "late-visible", "shadow-casters"]) o[n] = mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-" + n);
    o.bundle = mount.getAttribute("data-gosx-scene3d-webgpu-bundle-state") + "/" + mount.getAttribute("data-gosx-scene3d-webgpu-bundle-reason");
    return o;
  }
  function diff(a, b) {
    let n = 0, max = 0;
    for (let i = 0; i < a.length; i++) { const d = Math.abs(a[i] - b[i]); if (d > 0) n++; if (d > max) max = d; }
    let lit = 0, alpha = 0, sum = 0; for (let i = 0; i < a.length; i += 4) { if (a[i] + a[i + 1] + a[i + 2] > 120) lit++; alpha += a[i + 3]; sum += a[i] + a[i + 1] + a[i + 2]; }
    const shadowAttrs = Array.from(mount.attributes).filter((x) => /shadow|frame-error|draw-calls|instanced/.test(x.name)).map((x) => x.name + "=" + x.value).slice(0, 12);
    return { differing: n, maxDelta: max, litPixels: lit, alpha, sum, shadowAttrs };
  }
  for (const msaa of [1, 4]) {
    const classic = await draw(state(null), msaa, 4);
    const frustum = await draw(state({}), msaa, 6);
    out.cases["frustum-msaa" + msaa] = Object.assign(diff(classic, frustum), attrs());
    const occl = await draw(state({ occlusion: true }), msaa, 6);
    out.cases["occlusion-msaa" + msaa] = Object.assign(diff(classic, occl), attrs());
    const noShadowCull = await draw(state({ shadowCulling: false }), msaa, 6);
    out.cases["no-shadow-cull-msaa" + msaa] = Object.assign(diff(classic, noShadowCull), attrs());
  }
} catch (err) {
  out.fatal = String(err && err.stack || err);
}
window.__gdL3 = out;
```

### `run.mjs`

```js
import { chromium } from '/opt/node22/lib/node_modules/playwright/index.mjs';
const browser = await chromium.launch({ headless: true, executablePath: '/opt/pw-browsers/chromium-1194/chrome-linux/chrome', args: ['--enable-unsafe-webgpu', '--enable-features=Vulkan', '--use-vulkan=swiftshader', '--use-webgpu-adapter=swiftshader', '--use-angle=swiftshader', '--disable-vulkan-surface'] });
const page = await browser.newPage();
page.on('console', (m) => { if (m.type() === 'error') console.log('console.error:', m.text()); });
await page.goto('http://127.0.0.1:8941/page.html');
await page.waitForFunction(() => window.__gdL3 !== null, null, { timeout: 300000 });
console.log(JSON.stringify(await page.evaluate(() => window.__gdL3), null, 1));
await browser.close();
```

## Expected output (validation run, Chromium 141, SwiftShader)

```
errors: []   warnings: []
frustum-msaa1        differing 0  maxDelta 0  reason frustum    camera-visible 471  bundle replayed/
occlusion-msaa1      differing 0  maxDelta 0  reason occlusion  camera-visible 71   bundle direct/gpu-driven-occlusion
no-shadow-cull-msaa1 differing 0  maxDelta 0  reason frustum    camera-visible 471  shadow-casters 500
frustum-msaa4        differing 0  maxDelta 0  reason frustum    camera-visible 471
occlusion-msaa4      differing 0  maxDelta 0  reason occlusion  camera-visible 71
no-shadow-cull-msaa4 differing 0  maxDelta 0  reason frustum    shadow-casters 500
```

`litPixels` was 34 048 in every case, so the comparison is not two blank
frames. `camera-visible` counts are readback values and lag a frame or two;
compare them only after 6 frames, as the probe does.

## Pass criteria

- `errors` and `warnings` are empty; no `fatal`.
- Every case has `differing: 0`.
- Occlusion cases report `reason: occlusion` and a `camera-visible` count
  below the frustum cases'.

## A finding to record, not to fix here

With shadow culling ON the directional light culls every caster
(`shadow-casters: 0`); with it off, 500 casters draw. The light matrix the
renderer passes to `renderShadowPass` places every caster at clip z ≈ −0.5
(outside WebGPU's [0, 1] depth range), so the rasterizer clips them anyway.
The cull therefore matches what the classic path draws, which is why the
pixels are identical. In this probe scene, switching the boxes' `castShadow`
on and off also changed no pixels on the classic path. Appendix C records
this; do not change `sceneShadowLightSpaceMatrix` as part of this spec.

## Report

Paste the probe's JSON into the task log.
