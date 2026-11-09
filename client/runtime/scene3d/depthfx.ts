// Shared math and scene-light summary for the depth-driven post passes
// (projected decals, motion blur). This chunk loads on demand, only for scenes
// that declare one of those effects. It publishes window.__gosx_scene3d_depthfx_api;
// the WebGPU and WebGL2 post chains call into it through one small hook each.
(() => {
  const w = window as any;
  if (w.__gosx_scene3d_depthfx_api) return;

  type Mat = Float32Array | number[];

  function mul(out: Float32Array, a: Mat, b: Mat): Float32Array {
    for (let c = 0; c < 4; c++) {
      const b0 = b[c * 4], b1 = b[c * 4 + 1], b2 = b[c * 4 + 2], b3 = b[c * 4 + 3];
      for (let r = 0; r < 4; r++) out[c * 4 + r] = a[r] * b0 + a[4 + r] * b1 + a[8 + r] * b2 + a[12 + r] * b3;
    }
    return out;
  }

  // General 4x4 inverse. Returns false for a singular matrix and leaves out unchanged.
  function invert(out: Float32Array, m: Mat): boolean {
    const a00 = m[0], a01 = m[1], a02 = m[2], a03 = m[3], a10 = m[4], a11 = m[5], a12 = m[6], a13 = m[7];
    const a20 = m[8], a21 = m[9], a22 = m[10], a23 = m[11], a30 = m[12], a31 = m[13], a32 = m[14], a33 = m[15];
    const b00 = a00 * a11 - a01 * a10, b01 = a00 * a12 - a02 * a10, b02 = a00 * a13 - a03 * a10;
    const b03 = a01 * a12 - a02 * a11, b04 = a01 * a13 - a03 * a11, b05 = a02 * a13 - a03 * a12;
    const b06 = a20 * a31 - a21 * a30, b07 = a20 * a32 - a22 * a30, b08 = a20 * a33 - a23 * a30;
    const b09 = a21 * a32 - a22 * a31, b10 = a21 * a33 - a23 * a31, b11 = a22 * a33 - a23 * a32;
    let det = b00 * b11 - b01 * b10 + b02 * b09 + b03 * b08 - b04 * b07 + b05 * b06;
    if (!det || !isFinite(det)) return false;
    det = 1 / det;
    out[0] = (a11 * b11 - a12 * b10 + a13 * b09) * det;
    out[1] = (a02 * b10 - a01 * b11 - a03 * b09) * det;
    out[2] = (a31 * b05 - a32 * b04 + a33 * b03) * det;
    out[3] = (a22 * b04 - a21 * b05 - a23 * b03) * det;
    out[4] = (a12 * b08 - a10 * b11 - a13 * b07) * det;
    out[5] = (a00 * b11 - a02 * b08 + a03 * b07) * det;
    out[6] = (a32 * b02 - a30 * b05 - a33 * b01) * det;
    out[7] = (a20 * b05 - a22 * b02 + a23 * b01) * det;
    out[8] = (a10 * b10 - a11 * b08 + a13 * b06) * det;
    out[9] = (a01 * b08 - a00 * b10 - a03 * b06) * det;
    out[10] = (a30 * b04 - a31 * b02 + a33 * b00) * det;
    out[11] = (a21 * b02 - a20 * b04 - a23 * b00) * det;
    out[12] = (a11 * b07 - a10 * b09 - a12 * b06) * det;
    out[13] = (a00 * b09 - a01 * b07 + a02 * b06) * det;
    out[14] = (a31 * b01 - a30 * b03 - a32 * b00) * det;
    out[15] = (a20 * b03 - a21 * b01 + a22 * b00) * det;
    return true;
  }

  // Six frustum planes (nx, ny, nz, d), normalized, from a column-major
  // view-projection. zeroToOne selects the WebGPU clip range (0..w) for z;
  // otherwise z is -w..w (WebGL).
  function frustumPlanes(out: Float32Array, vp: Mat, zeroToOne: boolean): Float32Array {
    const row = (r: number, c: number): number => vp[c * 4 + r];
    for (let p = 0; p < 6; p++) {
      const axis = p >> 1, sign = (p & 1) ? -1 : 1;
      let x: number, y: number, z: number, d: number;
      if (axis === 2 && zeroToOne && sign === 1) { x = row(2, 0); y = row(2, 1); z = row(2, 2); d = row(2, 3); }
      else { x = row(3, 0) + sign * row(axis, 0); y = row(3, 1) + sign * row(axis, 1); z = row(3, 2) + sign * row(axis, 2); d = row(3, 3) + sign * row(axis, 3); }
      const l = Math.hypot(x, y, z) || 1;
      out[p * 4] = x / l; out[p * 4 + 1] = y / l; out[p * 4 + 2] = z / l; out[p * 4 + 3] = d / l;
    }
    return out;
  }

  function sphereVisible(planes: Float32Array, x: number, y: number, z: number, r: number): boolean {
    for (let p = 0; p < 6; p++) {
      if (planes[p * 4] * x + planes[p * 4 + 1] * y + planes[p * 4 + 2] * z + planes[p * 4 + 3] < -r) return false;
    }
    return true;
  }

  function toLinear(c: number): number {
    return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  }

  // Parse "#rgb" or "#rrggbb" (sRGB) into linear rgb written at out[at..at+2].
  function parseColor(text: any, out: Float32Array | number[], at: number, fallback: number): void {
    let r = fallback, g = fallback, b = fallback;
    if (typeof text === "string") {
      const hex = text.trim().replace(/^#/, "");
      if (/^[0-9a-f]{3}$/i.test(hex)) {
        r = parseInt(hex[0] + hex[0], 16) / 255; g = parseInt(hex[1] + hex[1], 16) / 255; b = parseInt(hex[2] + hex[2], 16) / 255;
      } else if (/^[0-9a-f]{6}([0-9a-f]{2})?$/i.test(hex)) {
        r = parseInt(hex.slice(0, 2), 16) / 255; g = parseInt(hex.slice(2, 4), 16) / 255; b = parseInt(hex.slice(4, 6), 16) / 255;
      } else return setRGB(out, at, fallback, fallback, fallback);
      return setRGB(out, at, toLinear(r), toLinear(g), toLinear(b));
    }
    setRGB(out, at, r, g, b);
  }
  function setRGB(out: Float32Array | number[], at: number, r: number, g: number, b: number): void {
    out[at] = r; out[at + 1] = g; out[at + 2] = b;
  }

  // Lighting summary the decal pass uses: ambient rgb, the first directional
  // light's travel direction and color. Written into reused buffers.
  const tmp = new Float32Array(3);
  function lightSummary(frame: any, ambient: Float32Array, sunDir: Float32Array, sunColor: Float32Array): boolean {
    ambient[0] = ambient[1] = ambient[2] = 0;
    const env = frame && frame.environment || {};
    const envK = Number(env.ambientIntensity);
    if (isFinite(envK) && envK > 0) {
      parseColor(env.ambientColor, tmp, 0, 1);
      for (let i = 0; i < 3; i++) ambient[i] += tmp[i] * envK;
    }
    const skyK = Number(env.skyIntensity), groundK = Number(env.groundIntensity);
    if (isFinite(skyK) && skyK > 0) { parseColor(env.skyColor, tmp, 0, 1); for (let i = 0; i < 3; i++) ambient[i] += tmp[i] * skyK * 0.5; }
    if (isFinite(groundK) && groundK > 0) { parseColor(env.groundColor, tmp, 0, 1); for (let i = 0; i < 3; i++) ambient[i] += tmp[i] * groundK * 0.25; }
    let sun = false;
    const lights = frame && frame.lights;
    if (Array.isArray(lights)) {
      for (let i = 0; i < lights.length; i++) {
        const light = lights[i];
        if (!light) continue;
        const kind = String(light.kind || "").toLowerCase();
        const k = isFinite(Number(light.intensity)) ? Number(light.intensity) : 1;
        if (kind === "ambient") {
          parseColor(light.color, tmp, 0, 1);
          for (let c = 0; c < 3; c++) ambient[c] += tmp[c] * k;
        } else if (kind === "hemisphere") {
          parseColor(light.color, tmp, 0, 1);
          for (let c = 0; c < 3; c++) ambient[c] += tmp[c] * k * 0.5;
        } else if (kind === "directional" && !sun) {
          let x = Number(light.directionX), y = Number(light.directionY), z = Number(light.directionZ);
          if (!isFinite(x) || !isFinite(y) || !isFinite(z)) { x = 0; y = -1; z = 0; }
          const l = Math.hypot(x, y, z) || 1;
          sunDir[0] = x / l; sunDir[1] = y / l; sunDir[2] = z / l;
          parseColor(light.color, sunColor, 0, 1);
          for (let c = 0; c < 3; c++) sunColor[c] *= k;
          sun = true;
        }
      }
    }
    if (!sun) { sunDir[0] = 0; sunDir[1] = -1; sunDir[2] = 0; sunColor[0] = sunColor[1] = sunColor[2] = 0; }
    return sun;
  }

  // Quality tier name from frame meta, matching sceneAtmosphereQuality.
  function qualityTier(frame: any): string {
    const meta = frame && frame.meta;
    if (!meta) return "full";
    const profile = meta.qualityProfile || {};
    return String(meta.qualityEnabled ? (profile.tier || meta.qualityTier || "full") : "full");
  }

  // Create a GPU texture-less placeholder-friendly image fetch. Resolves an
  // ImageBitmap with straight alpha and no color conversion.
  function loadBitmap(url: string): Promise<any> {
    return fetch(url, { mode: "cors", credentials: "same-origin" })
      .then((r: any) => { if (!r.ok) throw new Error("decal texture " + r.status + ": " + url); return r.blob(); })
      .then((blob: any) => createImageBitmap(blob, { premultiplyAlpha: "none", colorSpaceConversion: "none" } as any));
  }

  // Per-renderer state: one pass object per post-chain slot, rebuilt when the
  // slot's effect kind changes. state.device (WebGPU) or state.gl (WebGL2) is
  // set by the renderer that owns the state.
  function slot(state: any, backend: any, effect: any, index: number): any {
    const make = backend[effect.kind];
    if (!make) return null;
    let entry = state[index];
    if (!entry || entry.kind !== effect.kind) {
      if (entry) entry.pass.dispose();
      entry = state[index] = { kind: effect.kind, pass: make(state.device || state.gl) };
    }
    return entry.pass;
  }

  // WebGPU hook: draws the effect from input into output and returns the view
  // the chain continues from: output when the pass wrote it, else input. chain
  // is the render-truth record, or null.
  function webgpuRun(state: any, effect: any, index: number, encoder: any, input: any, output: any, format: string, depthView: any, width: number, height: number, frame: any, chain: any): any {
    const pass = slot(state, api.webgpu, effect, index);
    if (!pass || !pass.apply(effect, encoder, input, output, format, depthView, width, height, frame)) return input;
    const truth = (window as any).__gosx_scene3d_render_truth_api;
    if (chain && truth) truth.mark(chain, index, "ok", 1);
    return output;
  }

  // WebGL2 hook: same contract; targetFBO null means the default framebuffer, and
  // a null result means the pass already drew to it.
  function webglRun(state: any, effect: any, index: number, inputTex: any, targetFBO: any, width: number, height: number, depthTex: any, frame: any): any {
    const pass = slot(state, api.webgl, effect, index);
    if (!pass || !pass.apply(effect, inputTex, targetFBO, width, height, depthTex, frame)) return inputTex;
    return targetFBO ? targetFBO.colorTex : null;
  }

  function disposeState(state: any): void {
    for (const key of Object.keys(state)) {
      if (key === "device" || key === "gl") continue;
      const entry = state[key];
      if (entry && entry.pass) entry.pass.dispose();
      delete state[key];
    }
  }

  const api: any = {
    version: 1,
    mul, invert, frustumPlanes, sphereVisible, parseColor, lightSummary, qualityTier, loadBitmap,
    // Kinds this chunk draws.
    handles: (kind: string): boolean => kind === "decals" || kind === "motionBlur",
    webgpuRun, webglRun, dispose: disposeState,
    // Counters for tests and diagnostics: frames each pass drew, and for motion blur whether mover velocity was bound.
    stats: { decals: { frames: 0, visible: 0 }, motionBlur: { frames: 0, moverFrames: 0 } },
    webgpu: {} as any,
    webgl: {} as any,
  };
  w.__gosx_scene3d_depthfx_api = api;
})();
