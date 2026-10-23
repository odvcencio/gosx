// Page-side helpers for the real-GPU frame scripts: run a demo on WebGL2 (hide WebGPU) and time its frames with
// EXT_disjoint_timer_query_webgl2. The init script wraps requestAnimationFrame so each callback's GL work sits inside one
// TIME_ELAPSED query (GL allows one at a time), and keeps the milliseconds of every finished query.
export const webglInit = `(() => {
  Object.defineProperty(Navigator.prototype, 'gpu', { get() { return undefined; }, configurable: true });
  const T = window.__glTimer = { ctx: null, ext: null, active: false, pending: [], ms: [] };
  const getContext = HTMLCanvasElement.prototype.getContext;
  HTMLCanvasElement.prototype.getContext = function (type, ...rest) {
    const c = getContext.call(this, type, ...rest);
    if (type === 'webgl2' && c && !T.ctx) { T.ctx = c; T.ext = c.getExtension('EXT_disjoint_timer_query_webgl2'); }
    return c;
  };
  // One query spans each display frame: from its first animation-frame callback to the first callback of the next frame, so it covers
  // every GL command the page issues in between wherever it issues them (the renderer's own frame timer then sees a query in flight and stays out).
  const raf = window.requestAnimationFrame.bind(window);
  const finish = () => {
    const gl = T.ctx, ext = T.ext;
    if (!gl || !ext) return;
    while (T.pending.length && gl.getQueryParameter(T.pending[0], gl.QUERY_RESULT_AVAILABLE)) {
      const done = T.pending.shift(), bad = gl.getParameter(ext.GPU_DISJOINT_EXT), ns = gl.getQueryParameter(done, gl.QUERY_RESULT);
      gl.deleteQuery(done);
      if (!bad) { T.ms.push(ns / 1e6); if (T.ms.length > 600) T.ms.shift(); }
    }
  };
  window.requestAnimationFrame = (cb) => raf((t) => {
    const gl = T.ctx, ext = T.ext;
    if (gl && ext && t !== T.lastT) {
      if (T.active) { gl.endQuery(ext.TIME_ELAPSED_EXT); T.active = false; T.pending.push(T.current); }
      if (!gl.getQuery(ext.TIME_ELAPSED_EXT, gl.CURRENT_QUERY)) { T.current = gl.createQuery(); gl.beginQuery(ext.TIME_ELAPSED_EXT, T.current); T.active = true; }
      T.lastT = t;
      finish();
    }
    cb(t);
  });
})();`;

// glTiming reads the whole-frame GPU milliseconds the init script collected: the last `n` finished frames.
export async function glTiming(page, n = 120) {
  return page.evaluate((count) => {
    const T = window.__glTimer;
    if (!T) return null;
    const all = T.ms.slice(-count), v = all.filter((x) => x > 0.02).sort((a, b) => a - b); // display frames the scene did not render in measure ~0
    const at = (p) => v.length ? v[Math.min(v.length - 1, Math.floor(v.length * p))] : null;
    return { samples: v.length, displayFrames: all.length, timer: Boolean(T.ext), p50: at(0.5), p95: at(0.95), renderer: T.ctx && (() => { const d = T.ctx.getExtension('WEBGL_debug_renderer_info'); return d ? T.ctx.getParameter(d.UNMASKED_RENDERER_WEBGL) : T.ctx.getParameter(T.ctx.RENDERER); })() };
  }, n);
}
export function describeTiming(t) {
  return t && t.samples ? `GL frame p50 ${t.p50.toFixed(3)} / p95 ${t.p95.toFixed(3)} ms (${t.samples} of ${t.displayFrames} display frames rendered, ${t.renderer})` : 'GL timing n/a' + (t && !t.timer ? ' (no EXT_disjoint_timer_query_webgl2)' : '');
}
