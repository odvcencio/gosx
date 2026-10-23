// The spectral ocean on the Elio graph runner (lazy chunk bootstrap-feature-scene3d-ocean-fft.js).
//
// Elio emits the ocean's pass graph (spectrum, evolve, FFT, assemble, mips, foam, probe) per backend and tier, and the generic
// runner (elio-graph*.ts) executes it. This file is only the glue between three things:
//   - the normalized scene ocean: environment.ocean.graphParams (every value parameter of the graph, computed in Go) is fed to
//     the runner with setValue, and environment.ocean.graphTween is the graph-level tween override (the app's setting wins);
//   - the frame: scene time, dt and the camera, which are the graph's "frame" parameter sources;
//   - the surface: the graph's exports (disp0..3, slope0..3, foam) and exportParams (waterModX<c>/Z<c>, the water offset wrapped
//     by each cascade's tile) are bound to the spectral variant of the ocean shader (ocean-fft-surface.ts for WGSL,
//     ocean-fft-surface-gl.ts for GLSL).
// No simulation constant lives here: sizes, tile lengths, band edges, foam window and every default come from the graph and from
// graphParams. The surface-side tuning below (statelessGain, FOAM_RADIUS, the crest amplitude) shades the surface and is
// replaced when the surface moves to Selena.
//
// WebGPU: frame() runs the graph in the frame encoder before the main pass and returns an engine with the surface bind group
// (engine.layout, engine.group) once a frame has run. WebGL2: frameGL() runs the gather passes and returns an engine whose bind()
// attaches the cascade textures and the surface uniform to the ocean program. A failed graph returns null / not ready and the
// renderer draws the Gerstner ocean; the reason is in the pass guard (WebGPU) or on the mount (WebGL2).
(function () {
  const G = window.__gosx_elio_graph, MODULE = window.__gosx_scene3d_elio_ocean;
  const SURFACE = window.__gosx_scene3d_ocean_fft_surface, SURFACE_GL = window.__gosx_scene3d_ocean_fft_surface_gl;
  const MAX_DT = 0.25; // seconds; a long frame (a hidden tab) advances the graph by at most this much
  // Surface-side tuning, not simulation: statelessGain drives the distant foam the shader derives from J beyond the persistent
  // window; FOAM_RADIUS is where the shader stops trusting that window; the crest amplitude normalizes the crest shading.
  const STATELESS_GAIN = 3.5, FOAM_RADIUS = 240;
  const crestAmplitude = function (windSpeed: number): number { return Math.max(0.3, 0.14 * windSpeed * windSpeed / 9.81); };
  const SURFACE_FLOATS = 40; // c[4], f, m, w[4]: ten vec4s, the layout both surface variants declare
  const CASCADES = 4;
  let lastWater: any = null; // the most recent running graph's water frame, for CPU queries (see water())

  function tierOf(tier: any): string { return tier === "low" || tier === "medium" ? tier : "high"; }

  // describe reads what the surface needs from a graph: per cascade (tile, band edges, texels) from its ocean_spectrum passes and
  // the foam window (cell, width) from its ocean_foam pass.
  function describe(graph: any): any {
    const cascades: any[] = [];
    let foam: any = { cell: 0.5, width: 1024 };
    for (const pass of G.expand(graph)) {
      const values = pass.uniforms && pass.uniforms[0] && pass.uniforms[0].values;
      if (pass.kernel === "ocean_spectrum" && values) cascades.push({ tile: values.tile, kMin: values.kMin, kMax: values.kMax, n: values.n });
      else if (pass.kernel === "ocean_foam" && values) foam = { cell: values.cell, width: values.width };
    }
    return { cascades: cascades.slice(0, CASCADES), foam: foam };
  }

  // feed hands the scene's graph parameters to the runner: each value goes to setValue (unchanged values are ignored), and the
  // scene's tween overrides the graph's own.
  function feed(runner: any, ocean: any): void {
    const params = ocean.graphParams;
    if (params) for (const name of Object.keys(params)) if (Number.isFinite(params[name]) && runner.hasParam(name)) runner.setValue(name, params[name]); // a backend's graph may omit a parameter (no probe on WebGL2)
    if (Number.isFinite(ocean.graphTween)) runner.setTween(ocean.graphTween);
  }

  // surfaceData fills the surface uniform the spectral shaders read; see the declarations in the two surface files.
  function surfaceData(runner: any, info: any, out: Float32Array): void {
    out.fill(0);
    const wrapped = runner.exportParams(), foam = info.foam;
    info.cascades.forEach(function (c: any, i: number) {
      out[i * 4] = c.tile; out[i * 4 + 1] = Math.sqrt(Math.max(c.kMin, 1e-6) * c.kMax); out[i * 4 + 2] = c.n; out[i * 4 + 3] = 1;
      out[24 + i * 4] = wrapped["waterModX" + i] || 0; out[24 + i * 4 + 1] = wrapped["waterModZ" + i] || 0;
    });
    const span = foam.cell * foam.width, wrap = function (v: number) { return v - Math.floor(v / span) * span; };
    out[16] = 1 / span; out[17] = 1; out[18] = runner.value("foamThreshold"); out[19] = STATELESS_GAIN;
    out[20] = crestAmplitude(runner.value("windSpeed")); out[21] = wrap(runner.value("waterX")); out[22] = wrap(runner.value("waterZ")); out[23] = FOAM_RADIUS;
  }

  // frameInputs are the graph's "frame" parameter sources.
  function frameInputs(state: any, host: any): any {
    const time = Number.isFinite(host.time) ? host.time : 0;
    const dt = state.last === null ? 0 : Math.min(MAX_DT, Math.max(0, time - state.last));
    state.last = time;
    const cam = host.camera || {};
    return { time: time, dt: dt, cameraX: cam.x || 0, cameraY: cam.y || 0, cameraZ: cam.z || 0 };
  }

  // water is the running graph's water frame, in float64: the origin of the water (what the drift has integrated) and the wave
  // time. A CPU query samples the wave field at (position - origin) at that time to agree with what is drawn.
  function water(): any { return lastWater; }
  function publish(runner: any): void {
    lastWater = runner.failed ? null : { x: runner.value("waterX"), z: runner.value("waterZ"), time: runner.value("simTime") };
  }


  // --- Probe readback (WebGPU) ---------------------------------------------------------------------------------------
  //
  // A vessel samples the water at a few points each frame. On WebGPU those points go to the graph's ocean_probe kernel, which
  // sums the very cascades that are drawn, and the result comes back through a ring of three staging buffers:
  //   begin() starts a vessel update; sample(x, z, level) records the point (the k-th query of an update is point k) and answers
  //   from the latest readback, extrapolated with the velocity of the two latest timestamped samples, for up to STALE_MS;
  //   after that, or with no readback (WebGL2, a graph without a probe), it answers null and the caller uses the swell waves.
  // The glue feeds the recorded points to the graph (probeCount, probeOriginX/Z and the probePoints buffer, relative to the origin),
  // copies the kernel's output into a free staging slot after the graph ran, and polls the previous copies before encoding a new one:
  // a readback is never awaited. The wire layout is the kernel's: out[i] = (height, normal.x, normal.z, simulation time).
  const RING = 3, STALE_MS = 250, MAX_POINTS = 64, MAP_READ = 1, COPY_DST = 8, RECORD_BYTES = 16;
  const probe: any = { collect: [], k: 0, latest: null, previous: null };
  function probeBegin(): void { probe.collect = []; probe.k = 0; }
  function probeSample(x: number, z: number, level: number, nowMs?: number): any {
    const k = probe.k++;
    if (probe.collect.length < MAX_POINTS) probe.collect.push({ x: x, z: z });
    const now = Number.isFinite(nowMs) ? nowMs : (typeof performance !== "undefined" ? performance.now() : 0), last = probe.latest;
    if (!last || k >= last.count || now - last.at > STALE_MS) return null;
    if (Math.abs(last.points[k].x - x) > 8 || Math.abs(last.points[k].z - z) > 8) return null; // the vessel jumped; the readback is for elsewhere
    const prev = probe.previous && probe.previous.count > k ? probe.previous : null;
    const rate = prev && last.at > prev.at ? (last.heights[k] - prev.heights[k]) / ((last.at - prev.at) / 1000) : 0;
    const h = last.heights[k] + rate * ((now - last.at) / 1000);
    const nx = last.nx[k], nz = last.nz[k], ny = Math.sqrt(Math.max(0, 1 - nx * nx - nz * nz));
    return { x: x, y: level + h, z: z, normal: { x: nx, y: ny, z: nz }, velocity: { x: 0, y: rate, z: 0 } };
  }
  // probeRing owns the staging buffers of one WebGPU engine.
  function createProbeRing(device: any, now: () => number): any {
    const slots: any[] = [];
    for (let i = 0; i < RING; i++) slots.push({ buffer: device.createBuffer({ label: "gosx-ocean-fft-probe-" + i, size: MAX_POINTS * RECORD_BYTES, usage: MAP_READ | COPY_DST }), state: "free", meta: null });
    const take = function (slot: any): void {
      const buffer = slot.buffer, meta = slot.meta;
      slot.state = "mapping";
      buffer.mapAsync(MAP_READ).then(function () {
        if (slot.state !== "mapping") return;
        const raw = new Float32Array(buffer.getMappedRange().slice(0, meta.count * RECORD_BYTES));
        buffer.unmap();
        const heights: number[] = [], nx: number[] = [], nz: number[] = [];
        for (let i = 0; i < meta.count; i++) { heights.push(raw[i * 4]); nx.push(raw[i * 4 + 1]); nz.push(raw[i * 4 + 2]); }
        probe.previous = probe.latest;
        probe.latest = { at: meta.at, count: meta.count, points: meta.points, heights: heights, nx: nx, nz: nz };
        slot.state = "free";
      }, function () { slot.state = "free"; });
    };
    return {
      // poll maps every copy the last frame's submission finished with; call it before encoding this frame's copy.
      poll: function () { for (const slot of slots) if (slot.state === "copied") take(slot); },
      // copy records the probe output into a free slot after the graph's passes; with none free the frame is skipped.
      copy: function (encoder: any, source: any, points: any[]): void {
        const slot = slots.find(function (x: any) { return x.state === "free"; });
        if (!slot) return;
        encoder.copyBufferToBuffer(source, 0, slot.buffer, 0, MAX_POINTS * RECORD_BYTES);
        slot.state = "copied"; slot.meta = { at: now(), count: points.length, points: points };
      },
      dispose: function () { for (const slot of slots) { slot.state = "disposed"; slot.buffer.destroy(); } },
    };
  }
  // feedProbe hands the vessel's recorded points to the graph. Points are relative to their centroid, which the graph wraps per
  // cascade in float64 (probeOriginMod<c>); a graph without the parameters (WebGL2) has no probe.
  function feedProbe(runner: any, points: any[]): boolean {
    if (!runner.hasParam("probePoints")) return false;
    const n = Math.min(points.length, MAX_POINTS);
    if (!n) { runner.setValue("probeCount", 0); return false; }
    let ox = 0, oz = 0;
    for (let i = 0; i < n; i++) { ox += points[i].x; oz += points[i].z; }
    ox /= n; oz /= n;
    const data = new Float32Array(MAX_POINTS * 4);
    for (let i = 0; i < n; i++) { data[i * 4] = points[i].x - ox; data[i * 4 + 1] = points[i].z - oz; }
    runner.setValue("probeOriginX", ox); runner.setValue("probeOriginZ", oz); runner.setValue("probeCount", n); runner.setValue("probePoints", data);
    return true;
  }

  // sampler gives a vessel its water queries for one update: sample() evaluates the CPU swell waves in the water frame (position minus the
  // graph's water origin, at the graph's wave time), probed() answers from the probe readback first and falls back to sample(), and begin()
  // starts a probe pass. Without a running graph (the first frames, WebGL2 after a failure) sample() is the plain query at `seconds`.
  function sampler(query: any, q: any, seconds: number): any {
    const sample = function (x: number, z: number): any {
      const w = lastWater;
      if (!w) return query.sample(q, x, z, seconds);
      const r = query.sample(q, x - w.x, z - w.z, w.time);
      r.x += w.x; r.z += w.z;
      return r;
    };
    return { sample: sample, probed: function (x: number, z: number): any { return probeSample(x, z, q.data[0]) || sample(x, z); }, begin: probeBegin };
  }

  // --- WebGPU ------------------------------------------------------------------------------------------------------

  function createGPU(host: any): any {
    const device = host.device, label = "gosx-ocean-fft-";
    const surfaceEntries = function (): any[] {
      const both = GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, entries: any[] = [];
      entries.push({ binding: SURFACE.SLOT.uniform, visibility: both, buffer: { type: "uniform" } }, { binding: SURFACE.SLOT.sampler, visibility: both, sampler: { type: "filtering" } });
      for (let i = 0; i < CASCADES; i++) entries.push({ binding: SURFACE.SLOT.disp + i, visibility: both, texture: { sampleType: "float" } }, { binding: SURFACE.SLOT.slope + i, visibility: both, texture: { sampleType: "float" } });
      entries.push({ binding: SURFACE.SLOT.foam, visibility: both, texture: { sampleType: "float" } });
      return entries;
    };
    const layout = device.createBindGroupLayout({ label: label + "surface", entries: surfaceEntries() });
    const sampler = device.createSampler({ label: label + "sampler", magFilter: "linear", minFilter: "linear", mipmapFilter: "linear", addressModeU: "repeat", addressModeV: "repeat" });
    const uniform = device.createBuffer({ label: label + "surface", size: SURFACE_FLOATS * 4, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
    const dummy = device.createTexture({ label: label + "dummy", size: [1, 1, 1], format: "rgba16float", usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST });
    const dummyView = dummy.createView(), data = new Float32Array(SURFACE_FLOATS);
    const state: any = { last: null, tier: "", runner: null, info: null, viewList: null };
    const ring = createProbeRing(device, function () { return host.now ? host.now() : (typeof performance !== "undefined" ? performance.now() : 0); });
    const engine: any = { device: device, ready: false, layout: layout, group: null, disposed: false, tier: "", update: update, dispose: dispose, fail: fail };

    // fail reports a problem outside the graph (the surface shader patch) through the optional-pass route: the guard disables the
    // pass and the ocean falls back to Gerstner waves.
    function fail(error: any): void {
      const message = "ocean-fft: " + (error && error.message ? error.message : String(error));
      if (host.guard) host.guard.fail("gosx-ocean-fft-surface", message);
    }
    function build(tier: string): void {
      if (state.runner) state.runner.dispose();
      const runner = G.create({ module: MODULE, backend: "webgpu", tier: tier, host: { device: device, create: host.create, validation: host.validation } });
      state.runner = runner; state.tier = tier; state.last = null; state.viewList = null; engine.group = null; engine.ready = false;
      state.info = runner.graph ? describe(runner.graph) : null;
    }
    // bindGroup (re)builds the surface bind group when the graph's exported textures changed.
    function bindGroup(): void {
      const runner = state.runner, views: any[] = [], entries: any[] = [{ binding: SURFACE.SLOT.uniform, resource: { buffer: uniform } }, { binding: SURFACE.SLOT.sampler, resource: sampler }];
      for (let i = 0; i < CASCADES; i++) {
        const d = runner.export("disp" + i), s = runner.export("slope" + i);
        entries.push({ binding: SURFACE.SLOT.disp + i, resource: d ? d.view : dummyView }, { binding: SURFACE.SLOT.slope + i, resource: s ? s.view : dummyView });
        views.push(d ? d.view : dummyView, s ? s.view : dummyView);
      }
      const foam = runner.export("foam");
      entries.push({ binding: SURFACE.SLOT.foam, resource: foam ? foam.view : dummyView });
      views.push(foam ? foam.view : dummyView);
      if (state.viewList && views.every(function (v: any, i: number) { return v === state.viewList[i]; })) return;
      state.viewList = views;
      engine.group = device.createBindGroup({ label: label + "surface", layout: layout, entries: entries });
    }
    // update runs the graph for this frame in the frame's encoder.
    function update(frame: any): boolean {
      const tier = tierOf(frame.tier);
      if (!state.runner || tier !== state.tier) build(tier);
      const runner = state.runner;
      if (runner.failed) { engine.ready = false; engine.group = null; return false; }
      feed(runner, frame.ocean);
      ring.poll();
      const points = probe.collect.slice(0, MAX_POINTS), probing = feedProbe(runner, points);
      const ran = runner.run(Object.assign(frameInputs(state, frame), { encoder: frame.encoder }));
      if (runner.failed) {
        engine.ready = false; engine.group = null; lastWater = null;
        if (host.guard) host.guard.fail("gosx-elio-ocean", "ocean graph failed: " + runner.failed);
        return false;
      }
      if (!ran) return engine.ready;
      const out = probing ? runner.export("probe") : null;
      if (out && out.buffer) ring.copy(frame.encoder, out.buffer, points);
      bindGroup();
      surfaceData(runner, state.info, data);
      device.queue.writeBuffer(uniform, 0, data);
      publish(runner);
      engine.ready = true;
      return true;
    }
    function dispose(): void {
      if (engine.disposed) return;
      engine.disposed = true; engine.ready = false; engine.group = null;
      if (state.runner) state.runner.dispose();
      lastWater = null; probe.latest = probe.previous = null;
      ring.dispose(); uniform.destroy(); dummy.destroy();
    }
    Object.defineProperty(engine, "runner", { get: function () { return state.runner; } });
    return engine;
  }

  // frame is the one call the WebGPU renderer makes per frame before the main pass. It owns the engine's lifetime through
  // resources.fft: created when the ocean has a spectrum, disposed when it does not, when a pass was disabled, or when the device changed.
  function frame(resources: any, host: any): any {
    const ocean = host.environment && host.environment.ocean;
    let eng = resources.fft;
    const disabled = host.guard && (host.guard.disabled.has("oceanFFT") || host.guard.disabled.has("elioGraph"));
    if (!ocean || !ocean.spectrum || disabled || (eng && eng.device !== host.device)) {
      if (eng) { eng.dispose(); resources.fft = null; }
      return null;
    }
    if (!eng) eng = resources.fft = createGPU(host);
    eng.update({ ocean: ocean, tier: host.tier, time: host.time, camera: host.camera, encoder: host.encoder });
    return eng;
  }

  // --- WebGL2 ------------------------------------------------------------------------------------------------------

  function createGL(gl: any, host: any): any {
    const state: any = { last: null };
    const runner = G.create({ module: MODULE, backend: "webgl2", tier: tierOf(host.tier), host: { gl: gl, caps: host.caps, textureCache: host.textureCache } });
    const data = new Float32Array(SURFACE_FLOATS), cache = new Map();
    const info = runner.graph ? describe(runner.graph) : null;
    const engine: any = { gl: gl, runner: runner, tier: tierOf(host.tier), disposed: false, update: update, bind: bind, dispose: dispose, fail: function (reason: string) { runner.fail(reason); } };
    function update(frame: any): boolean {
      if (runner.failed) return false;
      feed(runner, frame.ocean);
      if (!runner.run(frameInputs(state, frame))) return false;
      surfaceData(runner, info, data);
      publish(runner);
      return true;
    }
    // bind attaches the cascade textures to units 4 to 12 and uploads the surface uniform; call it with the ocean program in use.
    function bind(g: any, program: any): void {
      let loc = cache.get(program);
      if (!loc) {
        const u = function (name: string) { return g.getUniformLocation(program, name); };
        loc = { sp: u("u_sp[0]"), disp: [0, 1, 2, 3].map(function (i) { return u("u_spDisp" + i); }), slope: [0, 1, 2, 3].map(function (i) { return u("u_spSlope" + i); }), foam: u("u_spFoam") };
        cache.set(program, loc);
      }
      const unit = function (n: number, location: any, exported: any) { g.activeTexture(g.TEXTURE0 + n); g.bindTexture(g.TEXTURE_2D, exported ? exported.tex : engine.dummy); g.uniform1i(location, n); };
      if (!engine.dummy) { engine.dummy = g.createTexture(); g.bindTexture(g.TEXTURE_2D, engine.dummy); g.texStorage2D(g.TEXTURE_2D, 1, g.RGBA16F, 1, 1); g.texParameteri(g.TEXTURE_2D, g.TEXTURE_MIN_FILTER, g.LINEAR); g.texParameteri(g.TEXTURE_2D, g.TEXTURE_MAG_FILTER, g.LINEAR); }
      for (let i = 0; i < CASCADES; i++) {
        unit(SURFACE_GL.UNIT.disp + i, loc.disp[i], runner.export("disp" + i));
        unit(SURFACE_GL.UNIT.slope + i, loc.slope[i], runner.export("slope" + i));
      }
      unit(SURFACE_GL.UNIT.foam, loc.foam, runner.export("foam"));
      g.uniform4fv(loc.sp, data);
    }
    function dispose(): void {
      if (engine.disposed) return;
      engine.disposed = true;
      runner.dispose();
      if (engine.dummy) gl.deleteTexture(engine.dummy);
    }
    return engine;
  }

  // frameGL is the one call the WebGL renderer makes per frame before the ocean draw. It owns the engine through resources.fft and
  // returns it while the graph runs; null means "draw Gerstner waves", with resources.fftDisabled saying why once it cannot recover.
  function frameGL(resources: any, gl: any, host: any): any {
    let eng = resources.fft;
    if (eng && (eng.gl !== gl || eng.disposed || eng.tier !== tierOf(host.tier))) { eng.dispose(); eng = resources.fft = null; }
    if (resources.fftDisabled) return null;
    if (gl.isContextLost && gl.isContextLost()) return null;
    if (!eng) eng = resources.fft = createGL(gl, host);
    const ok = eng.update({ ocean: host.ocean, time: host.time, camera: host.camera });
    if (eng.runner.failed) {
      resources.fftDisabled = eng.runner.failed;
      eng.dispose(); resources.fft = null; lastWater = null;
      if (host.mount && host.mount.setAttribute) host.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback", resources.fftDisabled);
      return null;
    }
    return ok ? eng : null;
  }
  // failGL lets the renderer report a surface problem (the patched shader would not build) through the same route.
  function failGL(resources: any, host: any, reason: string): void {
    if (resources.fft) resources.fft.dispose();
    resources.fft = null; resources.fftDisabled = reason; lastWater = null;
    if (host && host.mount && host.mount.setAttribute) host.mount.setAttribute("data-gosx-scene3d-ocean-fft-fallback", reason);
  }

  window.__gosx_scene3d_ocean_fft_api = { frame: frame, water: water, sampler: sampler, probe: { begin: probeBegin, sample: probeSample }, probeState: probe, describe: describe, feed: feed, surfaceData: surfaceData };
  // The WebGL chunk's minifier renames some property names (frame among them); the method the two chunks share is called run.
  window.__gosx_scene3d_ocean_fft_gl_api = { run: frameGL, fail: failGL };
})();
