// Motion blur settings and per-pass history shared by the WebGPU and WebGL2
// passes. The blur follows McGuire et al., "A Reconstruction Filter for
// Plausible Motion Blur" (2012): a velocity buffer, a per-tile maximum, a
// neighbor maximum, then a gather along the dominant velocity that weighs each
// tap by depth and by how far its own velocity reaches.
(() => {
  const fx = (window as any).__gosx_scene3d_depthfx_api;
  if (!fx) return;

  const OFF_TIERS = ["survival", "minimal", "low", "off"];
  const CHEAP_TIERS = ["reduced", "balanced", "medium"];
  const PRESETS: any = { low: { samples: 6, tile: 32 }, medium: { samples: 10, tile: 24 }, high: { samples: 16, tile: 16 } };

  // Plan for one frame: tap count, tile size, blur limit in pixels, shutter
  // fraction. skip is true when the quality tier says to draw nothing.
  const plan = { skip: false, samples: 10, tile: 24, maxBlur: 32, strength: 0.5, quality: "medium" };
  function motionBlurPlan(effect: any, frame: any, heightPx: number): typeof plan {
    const tier = fx.qualityTier(frame);
    plan.skip = OFF_TIERS.indexOf(tier) >= 0;
    let quality = typeof effect.quality === "string" && PRESETS[effect.quality] ? effect.quality : "medium";
    if (CHEAP_TIERS.indexOf(tier) >= 0) quality = "low";
    const preset = PRESETS[quality];
    plan.quality = quality;
    plan.samples = preset.samples; plan.tile = preset.tile;
    const strength = Number(effect.strength);
    plan.strength = isFinite(strength) && strength > 0 ? Math.min(1, strength) : 0.5;
    const maxBlur = Number(effect.maxBlur);
    const scaled = (isFinite(maxBlur) && maxBlur > 0 ? Math.min(128, maxBlur) : 32) * Math.max(0.25, heightPx / 1080);
    plan.maxBlur = Math.max(1, Math.min(scaled, plan.tile));
    return plan;
  }

  // Previous-frame view-projection with cut detection. step() answers whether a
  // usable previous matrix exists for the current frame; commit() stores this
  // frame's matrix for the next one.
  function createMotionHistory() {
    const prev = new Float32Array(16);
    const prevEye = new Float32Array(3);
    let valid = false, lastNow = 0;
    return {
      get prev() { return prev; },
      step(now: number, eye: Float32Array): boolean {
        if (!valid) return false;
        if (now - lastNow > 250) return false;
        const dx = eye[0] - prevEye[0], dy = eye[1] - prevEye[1], dz = eye[2] - prevEye[2];
        return dx * dx + dy * dy + dz * dz < 100;
      },
      commit(vp: Float32Array, now: number, eye: Float32Array): void {
        prev.set(vp); prevEye.set(eye); lastNow = now; valid = true;
      },
      reset(): void { valid = false; },
    };
  }

  // Camera eye and forward from an inverse view-projection. near and far are
  // the clip-space depths of the two NDC planes (WebGPU 0 and 1, WebGL -1 and 1).
  const p0 = new Float32Array(4), p1 = new Float32Array(4);
  function unproject(out: Float32Array, inv: Float32Array, x: number, y: number, z: number): void {
    const wx = inv[0] * x + inv[4] * y + inv[8] * z + inv[12];
    const wy = inv[1] * x + inv[5] * y + inv[9] * z + inv[13];
    const wz = inv[2] * x + inv[6] * y + inv[10] * z + inv[14];
    const ww = inv[3] * x + inv[7] * y + inv[11] * z + inv[15];
    out[0] = wx / ww; out[1] = wy / ww; out[2] = wz / ww;
  }
  function eyeAndForward(inv: Float32Array, near: number, far: number, eye: Float32Array, forward: Float32Array): void {
    unproject(p0, inv, 0, 0, near); unproject(p1, inv, 0, 0, far);
    const dx = p1[0] - p0[0], dy = p1[1] - p0[1], dz = p1[2] - p0[2];
    const l = Math.hypot(dx, dy, dz) || 1;
    forward[0] = dx / l; forward[1] = dy / l; forward[2] = dz / l;
    eye[0] = p0[0]; eye[1] = p0[1]; eye[2] = p0[2];
  }

  fx.motionBlurPlan = motionBlurPlan;
  fx.createMotionHistory = createMotionHistory;
  fx.eyeAndForward = eyeAndForward;
})();
