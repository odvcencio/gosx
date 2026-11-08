// sceneGLShadow(gl): the state that passes save and restore, answered from memory (gl.getParameter and gl.isEnabled wait on the GPU process).
// Read once on install and after a lost context returns; the context's state methods are wrapped to keep it current.
function sceneGLShadow(gl: any): any {
  if (gl.__shadow) return gl.__shadow;
  const s: any = {}, N: any = { [gl.BLEND]: "blend", [gl.DEPTH_TEST]: "depth", [gl.CULL_FACE]: "cull", [gl.SCISSOR_TEST]: "scissor" }, q = (p: any): any => gl.getParameter(p);
  const load = () => {
    Object.assign(s, { ok: 1, draw: q(gl.DRAW_FRAMEBUFFER_BINDING), read: q(gl.READ_FRAMEBUFFER_BINDING), viewport: Array.from(q(gl.VIEWPORT) || [0, 0, 0, 0]),
      program: q(gl.CURRENT_PROGRAM), vao: q(gl.VERTEX_ARRAY_BINDING), active: q(gl.ACTIVE_TEXTURE), colorMask: Array.from(q(gl.COLOR_WRITEMASK) || [1, 1, 1, 1]), depthMask: q(gl.DEPTH_WRITEMASK) });
    for (const c in N) s[N[c]] = gl.isEnabled(+c);
  };
  const ready = (): any => (s.ok || gl.isContextLost?.() || load(), s);
  const hook = (name: string, after: any) => {
    const o = gl[name];
    if (o) gl[name] = (a: any, b: any, c: any, d: any) => { const r = o.call(gl, a, b, c, d); if (s.ok) after(a, b, c, d); return r; };
  };
  hook("bindFramebuffer", (t: number, f: any) => { if (t !== gl.READ_FRAMEBUFFER) s.draw = f; if (t !== gl.DRAW_FRAMEBUFFER) s.read = f; });
  hook("deleteFramebuffer", (f: any) => { if (f === s.draw) s.draw = null; if (f === s.read) s.read = null; });
  hook("deleteVertexArray", (v: any) => { if (v === s.vao) s.vao = null; });
  hook("viewport", (x: number, y: number, w: number, h: number) => { s.viewport = [x, y, w, h]; });
  hook("useProgram", (p: any) => { s.program = p; });
  hook("bindVertexArray", (v: any) => { s.vao = v; });
  hook("activeTexture", (u: number) => { s.active = u; });
  hook("enable", (c: number) => { if (N[c]) s[N[c]] = true; });
  hook("disable", (c: number) => { if (N[c]) s[N[c]] = false; });
  hook("colorMask", (r: any, g: any, b: any, a: any) => { s.colorMask = [r, g, b, a]; });
  hook("depthMask", (f: any) => { s.depthMask = f; });
  for (const e of ["webglcontextlost", "webglcontextrestored"]) gl.canvas?.addEventListener?.(e, () => { s.ok = 0; });
  load();
  return gl.__shadow = {
    snapshot: () => { const t = ready(); return { ...t, viewport: t.viewport.slice(), colorMask: t.colorMask.slice() }; },
    isEnabled: (cap: number): boolean => N[cap] ? ready()[N[cap]] : gl.isEnabled(cap),
    framebuffer: () => ready().draw,
  };
}
if (typeof window !== "undefined") (window as any).__gosx_gl_shadow = sceneGLShadow; // the Elio graph chunk reaches the shadow through this
