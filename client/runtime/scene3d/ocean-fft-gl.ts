// WebGL2 spectral ocean: the gather half (lazy chunk bootstrap-feature-scene3d-ocean-fft.js).
//
// WebGL2 has no compute shaders, so the Elio gather kernels (ocean-fft-kernels.ts, glsl and gatherLayout) run as
// fullscreen fragment passes into float render targets. Per frame, per cascade:
//   ocean_evolve -> fft_stage x (2 log2 n) on transform A and on B -> ocean_assemble -> generateMipmap,
// then ocean_foam once for the persistent foam texture. ocean_spectrum runs when the spectrum changes.
// WebGL2 renders RGBA16F for what the surface samples (it is filterable and renderable with
// EXT_color_buffer_float) and RGBA32F for the FFT buffers.
//
// Gather kernels view every buffer as an elio_width-wide texture and index it linearly. The foam kernel reads the
// four cascades' B fields and its own 1,024-wide state with one elio_width, so each cascade's B field is also
// copied (pixel buffer, GPU to GPU) into a texture as wide as the foam state; that copy is the one place the
// layouts change.
//
// Without EXT_color_buffer_float (or a kernel set without GLSL), frame() returns null and the caller keeps the
// Gerstner ocean; resources.fftDisabled says why and the mount carries data-gosx-scene3d-ocean-fft-fallback.
(function () {
  const K = window.__gosx_scene3d_elio_ocean.kernels;
  const API = window.__gosx_scene3d_ocean_fft_api;
  const SURFACE = window.__gosx_scene3d_ocean_fft_surface_gl;
  const SHARED = API.shared;
  const INPUT_UNIT = 4; // gather inputs use texture units 4 and up; units 0 to 3 belong to the ocean pass
  const FULLSCREEN_VERTEX = "#version 300 es\nvoid main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2. - 1., 0., 1.); }";

  function tick(textureCache: any): void { if (textureCache) textureCache._sceneTextureEpoch = (textureCache._sceneTextureEpoch || 0) + 1; }

  // State the passes disturb: restored when they finish, as webgl-detail.ts does around the atlas bake.
  function saveState(gl: any): any {
    return {
      draw: gl.getParameter(gl.DRAW_FRAMEBUFFER_BINDING), read: gl.getParameter(gl.READ_FRAMEBUFFER_BINDING), viewport: gl.getParameter(gl.VIEWPORT),
      program: gl.getParameter(gl.CURRENT_PROGRAM), vao: gl.getParameter(gl.VERTEX_ARRAY_BINDING), active: gl.getParameter(gl.ACTIVE_TEXTURE),
      blend: gl.isEnabled(gl.BLEND), depth: gl.isEnabled(gl.DEPTH_TEST), cull: gl.isEnabled(gl.CULL_FACE), scissor: gl.isEnabled(gl.SCISSOR_TEST),
      colorMask: gl.getParameter(gl.COLOR_WRITEMASK), depthMask: gl.getParameter(gl.DEPTH_WRITEMASK),
    };
  }
  function restoreState(gl: any, s: any): void {
    gl.bindBuffer(gl.PIXEL_PACK_BUFFER, null); gl.bindBuffer(gl.PIXEL_UNPACK_BUFFER, null); gl.bindBuffer(gl.UNIFORM_BUFFER, null); gl.bindBufferBase(gl.UNIFORM_BUFFER, 0, null);
    gl.bindFramebuffer(gl.DRAW_FRAMEBUFFER, s.draw); gl.bindFramebuffer(gl.READ_FRAMEBUFFER, s.read);
    gl.viewport(s.viewport[0], s.viewport[1], s.viewport[2], s.viewport[3]);
    gl.useProgram(s.program); gl.bindVertexArray(s.vao); gl.activeTexture(s.active);
    const set = function (cap: number, on: boolean) { if (on) gl.enable(cap); else gl.disable(cap); };
    set(gl.BLEND, s.blend); set(gl.DEPTH_TEST, s.depth); set(gl.CULL_FACE, s.cull); set(gl.SCISSOR_TEST, s.scissor);
    gl.colorMask(s.colorMask[0], s.colorMask[1], s.colorMask[2], s.colorMask[3]); gl.depthMask(s.depthMask);
  }

  function create(gl: any, host: any): any {
    const caps = host.caps ? host.caps(gl) : {};
    if (!caps.colorBufferFloat) throw Object.assign(new Error("EXT_color_buffer_float is not available"), { fallback: "EXT_color_buffer_float" });
    const number = function (pname: number, fallback: number) { const v = gl.getParameter(pname); return typeof v === "number" ? v : fallback; };
    if (number(gl.MAX_VERTEX_TEXTURE_IMAGE_UNITS, 0) < 9 || number(gl.MAX_TEXTURE_IMAGE_UNITS, 0) < 13 || number(gl.MAX_DRAW_BUFFERS, 0) < 2) {
      throw Object.assign(new Error("too few texture units or draw buffers"), { fallback: "texture-units" });
    }
    for (const name of ["oceanSpectrum", "oceanEvolve", "oceanAssemble", "oceanFoam", "fftGather64", "fftGather128", "fftGather256"]) {
      if (!K[name] || !K[name].glsl || !K[name].gatherLayout) throw Object.assign(new Error("kernel " + name + " has no GLSL gather form"), { fallback: "kernel:" + name });
    }
    const align = Math.max(16, number(gl.UNIFORM_BUFFER_OFFSET_ALIGNMENT, 256));
    const programs: any = Object.create(null), shaders: any[] = [], buffers: any[] = [], textures: any[] = [], fbos: any[] = [], fftBuffers: any = Object.create(null);
    const engine: any = { gl: gl, ready: false, disposed: false, resolved: null, buildCount: 0, split: false, bind: bind, update: update, encode: encode, dispose: dispose };
    const vao = gl.createVertexArray();
    let vertexShader: any = null;
    let res: any = null, key = "", pkey = "", lastTime: number | null = null, foamOrigin: number[] | null = null;
    const surfaceData = new Float32Array(24);
    const bindCache = new Map();

    function compile(type: number, source: string): any {
      const shader = gl.createShader(type);
      shaders.push(shader);
      gl.shaderSource(shader, source); gl.compileShader(shader);
      if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) throw new Error("ocean-fft: shader did not compile: " + gl.getShaderInfoLog(shader));
      return shader;
    }
    // program links one gather kernel with the fullscreen vertex shader and wires its samplers and uniform block.
    function program(name: string, kernel: any): any {
      if (programs[name]) return programs[name];
      if (!vertexShader) vertexShader = compile(gl.VERTEX_SHADER, FULLSCREEN_VERTEX);
      const fragment = compile(gl.FRAGMENT_SHADER, kernel.glsl);
      const handle = gl.createProgram();
      gl.attachShader(handle, vertexShader); gl.attachShader(handle, fragment); gl.linkProgram(handle);
      const p: any = programs[name] = { handle: handle, name: name, layout: kernel.gatherLayout, uniform: kernel.uniforms[0], size: Math.max(16, Math.ceil(kernel.uniforms[0].size / 16) * 16) };
      if (!gl.getProgramParameter(handle, gl.LINK_STATUS)) throw new Error("ocean-fft: program " + name + " did not link: " + gl.getProgramInfoLog(handle));
      gl.useProgram(handle);
      p.width = gl.getUniformLocation(handle, "elio_width");
      p.lens = p.layout.inputs.map(function (input: any) { return gl.getUniformLocation(handle, input.length); });
      p.layout.inputs.forEach(function (input: any, i: number) { gl.uniform1i(gl.getUniformLocation(handle, input.sampler), INPUT_UNIT + i); });
      gl.uniformBlockBinding(handle, gl.getUniformBlockIndex(handle, p.layout.uniforms[0].block), 0);
      return p;
    }
    function buffer(size: number): any {
      const b = gl.createBuffer();
      buffers.push(b);
      return b;
    }
    function uniformBuffer(bytes: number): any {
      const b = buffer(bytes);
      gl.bindBuffer(gl.UNIFORM_BUFFER, b); gl.bufferData(gl.UNIFORM_BUFFER, bytes, gl.DYNAMIC_DRAW);
      return b;
    }
    // texture allocates immutable storage; mips make a filterable, repeating texture with a full chain.
    function texture(internal: number, w: number, h: number, levels: number, filtered: boolean): any {
      const t = gl.createTexture();
      textures.push(t);
      gl.activeTexture(gl.TEXTURE0 + INPUT_UNIT); gl.bindTexture(gl.TEXTURE_2D, t);
      gl.texStorage2D(gl.TEXTURE_2D, levels, internal, w, h);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, filtered ? (levels > 1 ? gl.LINEAR_MIPMAP_LINEAR : gl.LINEAR) : gl.NEAREST);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, filtered ? gl.LINEAR : gl.NEAREST);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, filtered ? gl.REPEAT : gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, filtered ? gl.REPEAT : gl.CLAMP_TO_EDGE);
      return { tex: t, w: w, h: h, count: w * h };
    }
    // framebuffer attaches each texture at the colour attachment equal to its kernel output location.
    function framebuffer(list: any[]): any {
      const f = gl.createFramebuffer();
      fbos.push(f);
      gl.bindFramebuffer(gl.FRAMEBUFFER, f);
      const draw: number[] = [];
      for (const o of list) {
        gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0 + o.location, gl.TEXTURE_2D, o.t.tex, 0);
        while (draw.length <= o.location) draw.push(gl.NONE);
        draw[o.location] = gl.COLOR_ATTACHMENT0 + o.location;
      }
      gl.drawBuffers(draw);
      return { fbo: f, complete: gl.checkFramebufferStatus(gl.FRAMEBUFFER) === gl.FRAMEBUFFER_COMPLETE, w: list[0].t.w, h: list[0].t.h };
    }
    // target is one framebuffer holding every output of a pass. If the driver rejects that combination, completeness is
    // reported once and every output gets its own framebuffer: the same kernel then runs once per output.
    function target(list: any[]): any {
      if (list.length === 1) { const f = framebuffer(list); if (!f.complete) throw new Error("ocean-fft: framebuffer incomplete"); return { fbos: [f] }; }
      if (!engine.split) {
        const f = framebuffer(list);
        if (f.complete) return { fbos: [f] };
        engine.split = true;
      }
      return { fbos: list.map(function (o) { const f = framebuffer([o]); if (!f.complete) throw new Error("ocean-fft: framebuffer incomplete"); return f; }) };
    }

    // pass runs one gather kernel as a fullscreen triangle over w by h texels; named inputs bind to units 4 and up.
    function pass(p: any, w: number, h: number, inputs: any, ubo: any, offset: number, out: any): void {
      gl.useProgram(p.handle);
      gl.uniform1ui(p.width, w);
      p.layout.inputs.forEach(function (input: any, i: number) {
        const t = inputs[input.name];
        gl.activeTexture(gl.TEXTURE0 + INPUT_UNIT + i); gl.bindTexture(gl.TEXTURE_2D, t.tex);
        if (p.lens[i]) gl.uniform1ui(p.lens[i], t.count);
      });
      gl.bindBufferRange(gl.UNIFORM_BUFFER, 0, ubo, offset, p.size);
      gl.viewport(0, 0, w, h);
      for (const f of out.fbos) { gl.bindFramebuffer(gl.FRAMEBUFFER, f.fbo); gl.drawArrays(gl.TRIANGLES, 0, 3); }
    }
    function write(ubo: any, offset: number, p: any, values: any): void {
      gl.bindBuffer(gl.UNIFORM_BUFFER, ubo);
      gl.bufferSubData(gl.UNIFORM_BUFFER, offset, new Uint8Array(API.pack(p.uniform, values)));
    }
    // stages returns the buffer holding the n-sized FFT stage parameters, one aligned record per (axis, stage).
    function stages(n: number): any {
      if (fftBuffers[n]) return fftBuffers[n];
      const L = Math.log2(n), p = program("fft" + n, K["fftGather" + n]), ubo = uniformBuffer(align * 2 * L);
      for (let axis = 0; axis < 2; axis++) for (let stage = 0; stage < L; stage++) write(ubo, (axis * L + stage) * align, p, { n: n, stage: stage, axis: axis });
      return fftBuffers[n] = { ubo: ubo, p: p, L: L };
    }

    function build(r: any): void {
      release();
      engine.buildCount++;
      const cascades: any[] = [];
      const pSpectrum = program("spectrum", K.oceanSpectrum), pEvolve = program("evolve", K.oceanEvolve), pAssemble = program("assemble", K.oceanAssemble), pFoam = program("foam", K.oceanFoam);
      const width = r.high ? 1024 : 512, cell = SHARED.FOAM_WINDOW / width;
      for (let ci = 0; ci < r.cascades.length; ci++) {
        const c = r.cascades[ci], n = c.n, levels = Math.log2(n) - 1;
        const h0 = texture(gl.RGBA32F, n, n, 1, false), omega = texture(gl.R32F, n, n, 1, false);
        const a = [texture(gl.RGBA32F, n, n, 1, false), texture(gl.RGBA32F, n, n, 1, false)], b = [texture(gl.RGBA32F, n, n, 1, false), texture(gl.RGBA32F, n, n, 1, false)];
        const disp = texture(gl.RGBA16F, n, n, levels, true), slope = texture(gl.RGBA16F, n, n, levels, true);
        const wide = texture(gl.RGBA32F, width, n * n / width, 1, false);
        const single = function (t: any) { return target([{ t: t, location: 0 }]); };
        cascades.push({ c: c, n: n, h0: h0, omega: omega, a: a, b: b, disp: disp, slope: slope, wide: wide,
          specU: uniformBuffer(pSpectrum.size), evolveU: uniformBuffer(pEvolve.size), assembleU: uniformBuffer(pAssemble.size),
          spectrumT: target([{ t: h0, location: 0 }, { t: omega, location: 1 }]), evolveT: target([{ t: a[0], location: 0 }, { t: b[0], location: 1 }]),
          assembleT: target([{ t: disp, location: 0 }, { t: slope, location: 1 }]), singleA: a.map(single), singleB: b.map(single), fft: stages(n),
          pbo: (function () { const p = buffer(n * n * 16); gl.bindBuffer(gl.PIXEL_PACK_BUFFER, p); gl.bufferData(gl.PIXEL_PACK_BUFFER, n * n * 16, gl.STREAM_COPY); gl.bindBuffer(gl.PIXEL_PACK_BUFFER, null); return p; })() });
      }
      const state = [texture(gl.R32F, width, width, 1, false), texture(gl.R32F, width, width, 1, false)], foamTex = texture(gl.RGBA16F, width, width, 1, true);
      const foam = { width: width, cell: cell, state: state, tex: foamTex, u: uniformBuffer(pFoam.size),
        targets: [target([{ t: state[1], location: 0 }, { t: foamTex, location: 1 }]), target([{ t: state[0], location: 0 }, { t: foamTex, location: 1 }])], flip: 0 };
      res = { cascades: cascades, foam: foam, needSpectrum: true, dummy: texture(gl.RGBA16F, 1, 1, 1, true) };
      foamOrigin = null;
      bindCache.clear();
    }
    function release() {
      if (!res) return;
      for (const t of textures) gl.deleteTexture(t);
      for (const f of fbos) gl.deleteFramebuffer(f);
      for (const b of buffers) gl.deleteBuffer(b);
      textures.length = fbos.length = buffers.length = 0;
      for (const k of Object.keys(fftBuffers)) delete fftBuffers[k];
      res = null;
    }

    function update(frame: any): boolean {
      const r = engine.resolved = API.resolve(frame.ocean, frame.tier);
      const lk = SHARED.layoutKey(r), pk = SHARED.paramKey(r);
      if (!res || lk !== key) { key = lk; const saved = saveState(gl); try { build(r); } finally { restoreState(gl, saved); } pkey = pk; }
      else if (pk !== pkey) { pkey = pk; res.needSpectrum = true; }
      engine.ready = true;
      return true;
    }

    // transform runs the 2D inverse FFT of one field in place: log2 n stages along x, then along y, ping-ponging
    // between the two buffers. The pass count is even, so the result is back in buffer 0.
    function transform(casc: any, pair: any[], singles: any[]): void {
      const f = casc.fft, n = casc.n;
      let cur = 0;
      for (let axis = 0; axis < 2; axis++) for (let stage = 0; stage < f.L; stage++) {
        pass(f.p, n, n, { src: pair[cur] }, f.ubo, (axis * f.L + stage) * align, singles[1 - cur]);
        cur = 1 - cur;
      }
    }
    // relayout copies a cascade's B field into the foam kernel's layout (the same linear order, foam-width texels per row).
    function relayout(casc: any, wide: any): void {
      const f = casc.singleB[0].fbos[0], n = casc.n;
      gl.bindFramebuffer(gl.READ_FRAMEBUFFER, f.fbo);
      gl.bindBuffer(gl.PIXEL_PACK_BUFFER, casc.pbo); gl.readPixels(0, 0, n, n, gl.RGBA, gl.FLOAT, 0); gl.bindBuffer(gl.PIXEL_PACK_BUFFER, null);
      gl.bindBuffer(gl.PIXEL_UNPACK_BUFFER, casc.pbo);
      gl.activeTexture(gl.TEXTURE0 + INPUT_UNIT); gl.bindTexture(gl.TEXTURE_2D, wide.tex);
      gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, 0, wide.w, wide.h, gl.RGBA, gl.FLOAT, 0);
      gl.bindBuffer(gl.PIXEL_UNPACK_BUFFER, null);
    }

    function encode(frame: any): void {
      if (!engine.ready || !res) return;
      const r = engine.resolved, saved = saveState(gl);
      let t = frame.time % SHARED.LOOP_PERIOD;
      if (!(t >= 0)) t = t < 0 ? t + SHARED.LOOP_PERIOD : 0;
      const dt = lastTime === null ? 0 : Math.min(0.1, Math.max(0, frame.time - lastTime));
      lastTime = frame.time;
      gl.disable(gl.BLEND); gl.disable(gl.DEPTH_TEST); gl.disable(gl.CULL_FACE); gl.disable(gl.SCISSOR_TEST);
      gl.colorMask(true, true, true, true); gl.bindVertexArray(vao);
      const pSpectrum = programs.spectrum, pEvolve = programs.evolve, pAssemble = programs.assemble, pFoam = programs.foam;
      for (let ci = 0; ci < res.cascades.length; ci++) {
        const casc = res.cascades[ci], c = casc.c, n = casc.n;
        if (res.needSpectrum) {
          write(casc.specU, 0, pSpectrum, { n: n, cascade: ci, seed: r.seed, windSpeed: r.windSpeed, fetchKm: r.fetchKm, gamma: r.gamma, spread: r.spread,
            swell: r.swell, windDir: r.windDir, swellDir: r.swellDir, tile: c.size, kMin: c.kMin, kMax: c.kMax, loopPeriod: SHARED.LOOP_PERIOD });
          pass(pSpectrum, n, n, {}, casc.specU, 0, casc.spectrumT);
        }
        write(casc.evolveU, 0, pEvolve, { n: n, t: t, tile: c.size, loopPeriod: SHARED.LOOP_PERIOD });
        write(casc.assembleU, 0, pAssemble, { n: n, lambda: r.lambda });
        pass(pEvolve, n, n, { h0: casc.h0, omega: casc.omega }, casc.evolveU, 0, casc.evolveT);
        transform(casc, casc.a, casc.singleA);
        transform(casc, casc.b, casc.singleB);
        relayout(casc, casc.wide);
        pass(pAssemble, n, n, { fieldA: casc.a[0], fieldB: casc.b[0] }, casc.assembleU, 0, casc.assembleT);
        for (const m of [casc.disp, casc.slope]) { gl.activeTexture(gl.TEXTURE0 + INPUT_UNIT); gl.bindTexture(gl.TEXTURE_2D, m.tex); gl.generateMipmap(gl.TEXTURE_2D); }
      }
      res.needSpectrum = false;
      // Persistent foam: the window origin follows the camera in whole cells.
      const f = res.foam, cam = frame.camera || {};
      const ox = Math.floor((cam.x || 0) / f.cell) - f.width / 2, oz = Math.floor((cam.z || 0) / f.cell) - f.width / 2;
      const prev = foamOrigin || [ox, oz];
      foamOrigin = [ox, oz];
      const cs = res.cascades, sz = [1, 1, 1, 1], ns = [1, 1, 1, 1];
      for (let i = 0; i < cs.length; i++) { sz[i] = cs[i].c.size; ns[i] = cs[i].n; }
      write(f.u, 0, pFoam, { width: f.width, cascades: cs.length, originX: ox, originZ: oz, shiftX: ox - prev[0], shiftZ: oz - prev[1],
        dt: dt, tau: SHARED.FOAM.tau, threshold: SHARED.FOAM.threshold, gain: SHARED.FOAM.gain * dt * 60, lambda: r.lambda, cell: f.cell,
        n0: ns[0], n1: ns[1], n2: ns[2], n3: ns[3], size0: sz[0], size1: sz[1], size2: sz[2], size3: sz[3] });
      const inputs: any = { foamIn: f.state[f.flip] };
      for (let j = 0; j < 4; j++) inputs["fieldB" + j] = cs[Math.min(j, cs.length - 1)].wide;
      pass(pFoam, f.width, f.width, inputs, f.u, 0, f.targets[f.flip]);
      f.flip = 1 - f.flip;
      restoreState(gl, saved);
      tick(frame.textureCache);
      // Surface uniform: per cascade (tile, band centre, texels, enabled), then foam and crest terms (as ocean-fft.ts writes them).
      const d = surfaceData;
      d.fill(0);
      for (let i = 0; i < cs.length; i++) {
        d[i * 4] = cs[i].c.size; d[i * 4 + 1] = Math.sqrt(Math.max(cs[i].c.kMin, 1e-6) * cs[i].c.kMax); d[i * 4 + 2] = cs[i].n; d[i * 4 + 3] = 1;
      }
      d[16] = 1 / (f.cell * f.width); d[17] = 1; d[18] = SHARED.FOAM.threshold; d[19] = SHARED.FOAM.statelessGain;
      d[20] = Math.max(0.3, 0.14 * r.windSpeed * r.windSpeed / 9.81); d[23] = SHARED.FOAM_RADIUS;
    }

    // bind attaches the cascade textures to units 4 to 12 and uploads the surface uniform; call it with the ocean program in use.
    function bind(g: any, handle: any): void {
      if (!res) return;
      let loc = bindCache.get(handle);
      if (!loc) {
        const u = function (name: string) { return g.getUniformLocation(handle, name); }, names = ["Disp", "Slope"].map(function (kind) { return [0, 1, 2, 3].map(function (i) { return u("u_sp" + kind + i); }); });
        loc = { sp: u("u_sp[0]"), disp: names[0], slope: names[1], foam: u("u_spFoam") };
        bindCache.set(handle, loc);
      }
      const unit = function (u: number, location: any, t: any) { g.activeTexture(g.TEXTURE0 + u); g.bindTexture(g.TEXTURE_2D, t.tex); g.uniform1i(location, u); };
      for (let i = 0; i < 4; i++) {
        const casc = res.cascades[i];
        unit(SURFACE.UNIT.disp + i, loc.disp[i], casc ? casc.disp : res.dummy);
        unit(SURFACE.UNIT.slope + i, loc.slope[i], casc ? casc.slope : res.dummy);
      }
      unit(SURFACE.UNIT.foam, loc.foam, res.foam.tex);
      g.uniform4fv(loc.sp, surfaceData);
    }

    function dispose() {
      if (engine.disposed) return;
      engine.disposed = true; engine.ready = false;
      for (const t of textures) gl.deleteTexture(t);
      for (const f of fbos) gl.deleteFramebuffer(f);
      for (const b of buffers) gl.deleteBuffer(b);
      textures.length = fbos.length = buffers.length = 0;
      for (const k of Object.keys(programs)) gl.deleteProgram(programs[k].handle);
      for (const s of shaders) gl.deleteShader(s);
      gl.deleteVertexArray(vao);
      res = null;
    }
    return engine;
  }

  // frame is the one call the WebGL renderer makes per frame before the ocean draw. It owns the engine's lifetime
  // through resources.fft: created when the ocean has a spectrum, disposed when it does not or when the context
  // changed. Anything that goes wrong disposes the engine, records resources.fftDisabled and returns null: the
  // caller keeps the Gerstner ocean.
  function frame(resources: any, gl: any, host: any): any {
    let eng = resources.fft;
    if (eng && (eng.gl !== gl || eng.disposed)) { eng.dispose(); eng = resources.fft = null; }
    if (resources.fftDisabled) return null;
    if (gl.isContextLost && gl.isContextLost()) return null;
    const give = function (error: any) {
      if (eng) eng.dispose();
      resources.fft = null;
      resources.fftDisabled = (error && error.fallback) || "error";
      if (host.mount && host.mount.setAttribute) host.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback", resources.fftDisabled);
      if (!(error && error.fallback) && typeof console !== "undefined" && console.warn) console.warn("ocean-fft: WebGL2 spectral ocean disabled: " + (error && error.message ? error.message : String(error)));
      return null;
    };
    try {
      if (!eng) { eng = resources.fft = create(gl, host); eng.fail = function (reason: string) { fail(resources, host, reason); }; }
      const f = { ocean: host.ocean, tier: host.tier, time: Number.isFinite(host.time) ? host.time : 0, camera: host.camera, textureCache: host.textureCache };
      eng.update(f);
      eng.encode(f);
    } catch (error) { return give(error); }
    return eng;
  }

  // fail lets the caller report a problem outside the passes (the surface shader would not build) through the same route.
  function fail(resources: any, host: any, reason: string): void {
    if (resources.fft) resources.fft.dispose();
    resources.fft = null; resources.fftDisabled = reason;
    if (host && host.mount && host.mount.setAttribute) host.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback", reason);
  }

  window.__gosx_scene3d_ocean_fft_gl_api = { frame: frame, fail: fail, create: create };
})();
