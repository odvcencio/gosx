// SMAA: morphological edge anti-aliasing with no frame history (scene.SMAA).
//
// This chunk (bootstrap-feature-scene3d-smaa.js) loads only for a scene whose
// post chain lists "smaa"; the mount fetches it with the renderer chunk. It
// publishes window.__gosx_scene3d_smaa_api. Each renderer's post chain sends an
// "smaa" entry to it through the same one-line hook that serves the depth
// effects (wgpuDepthFxPass, sceneWebGLDepthFxPass). Until it loads, the entry
// draws nothing and the chain presents its input.
//
// Three passes run on the display-encoded image after the tone curve:
//   1. edges:   luma steps to the left and top neighbour (RG8), with local
//               contrast adaptation;
//   2. weights: for each edge pixel, walk along the edge to both ends, read the
//               crossing edges there, rebuild the silhouette line from the nearer
//               end to the middle of the edge and integrate its area over this
//               pixel in closed form (RGBA8: take-from-above, above-takes,
//               take-from-left, left-takes). No lookup tables;
//   3. blend:   mix each pixel with its neighbours along the dominant direction,
//               in linear light.
// smaa-wgpu.ts and smaa-gl.ts hold the two backends and register below.
(() => {
  const w = window as any;
  if (w.__gosx_scene3d_smaa_api) return;

  const num = (v: any, fallback: number, lo: number, hi: number): number => {
    const n = Number(v);
    return Number.isFinite(n) && n > 0 ? Math.max(lo, Math.min(hi, n)) : fallback;
  };

  // settings reads one effect's fields with the Go defaults and ranges.
  function settings(effect: any): { threshold: number; steps: number; corner: number } {
    return {
      threshold: num(effect && effect.threshold, 0.1, 0.02, 0.5),
      steps: Math.round(num(effect && effect.maxSearchSteps, 16, 4, 32)),
      corner: num(effect && effect.cornerRounding, 0.25, 0, 1),
    };
  }

  // One pass object per chain slot, rebuilt when the slot's effect kind changes;
  // depthfx.ts keys the same state object the same way.
  function slot(state: any, make: any, index: number): any {
    if (typeof make !== "function") return null;
    let entry = state[index];
    if (!entry || entry.kind !== "smaa") {
      if (entry && entry.pass) entry.pass.dispose();
      entry = state[index] = { kind: "smaa", pass: make(state.device || state.gl) };
    }
    return entry.pass;
  }

  // WebGPU hook, the depthfx.ts contract: draw input into output and return the
  // view the chain continues from.
  function webgpuRun(state: any, effect: any, index: number, encoder: any, input: any, output: any, format: string, _depth: any, width: number, height: number, _frame: any, chain: any): any {
    const pass = slot(state, api.webgpu, index);
    if (!pass || !pass.apply(effect, encoder, input, output, format, width, height)) return input;
    const truth = w.__gosx_scene3d_render_truth_api;
    if (chain && truth) truth.mark(chain, index, "ok", 1);
    api.stats.frames++;
    return output;
  }

  // WebGL2 hook: width and height are the pass size (the canvas for the last
  // pass); the chain's own size is on state. A null result means the pass drew
  // to the default framebuffer.
  function webglRun(state: any, effect: any, index: number, inputTex: any, targetFBO: any, width: number, height: number): any {
    const pass = slot(state, api.webgl, index);
    const srcW = state.width || width, srcH = state.height || height;
    if (!pass || !pass.apply(effect, inputTex, targetFBO, srcW, srcH, width, height)) return inputTex;
    api.stats.frames++;
    return targetFBO ? targetFBO.colorTex : null;
  }

  // dispose releases the SMAA slots only; depthfx.ts releases its own.
  function dispose(state: any): void {
    for (const key of Object.keys(state)) {
      const entry = state[key];
      if (entry && entry.kind === "smaa" && entry.pass) { entry.pass.dispose(); delete state[key]; }
    }
  }

  const api: any = { version: 1, settings, webgpuRun, webglRun, dispose, stats: { frames: 0 }, webgpu: null, webgl: null };
  w.__gosx_scene3d_smaa_api = api;
})();
