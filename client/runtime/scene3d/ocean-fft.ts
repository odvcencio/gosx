// WebGPU spectral ocean: the compute half (lazy chunk bootstrap-feature-scene3d-ocean-fft.js).
//
// Elio generates the kernels (ocean-fft-kernels.ts). Each frame, per cascade, this file runs
//   ocean_evolve -> fft rows and columns on buffers A and B -> ocean_assemble -> mip levels,
// then ocean_foam once for the persistent foam texture. ocean_spectrum runs when the spectrum
// changes. The WebGPU renderer calls the one function below from the frame encoder, before the main pass;
// the ocean pass reads engine.ready, engine.layout and engine.group to draw the surface.
//
// Pipelines are labelled gosx-ocean-fft-*; the pipeline recovery module maps those labels to the optional
// pass "oceanFFT", so a failure disposes this engine and the ocean falls back to Gerstner waves.
(function () {
  const K = window.__gosx_scene3d_elio_ocean.kernels;
  const SLOT = window.__gosx_scene3d_ocean_fft_surface.SLOT;
  const LOOP_PERIOD = 1000; // seconds; the kernels' default loop, and the time passed in is reduced modulo it
  const FOAM_WINDOW = 512; // metres covered by the persistent foam state
  const FOAM_RADIUS = 240; // metres from the camera where the shader stops trusting the state
  const TWO_PI = Math.PI * 2;
  // First estimates from the spec (Wave model); tune against the autocorrelation check.
  const DEFAULT_CASCADES = [
    { size: 1600, kMin: TWO_PI / 1600, kMax: 0.12 },
    { size: 250, kMin: 0.12, kMax: 0.8 },
    { size: 37, kMin: 0.8, kMax: 5.4 },
    { size: 5.3, kMin: 5.4, kMax: 37 },
  ];
  // Foam, in one place. Crests inject foam where the combined Jacobian J of the horizontal displacement drops below a
  // threshold (J < 1 compresses the surface, J < 0 folds it). Two choices keep foam tied to the sea state:
  //   - J is formed with lambda = foamLambda, not the authored choppiness, so a low Choppiness (the Blackglass cove
  //     uses 0.32) changes how far crests lean, not whether they break;
  //   - the threshold rises with wind speed U (m/s): 0 at 5 m/s and below, so a glassy sea carries no foam, and
  //     maxThreshold from windHigh up. threshold(U) = maxThreshold * smoothstep(windLow, windHigh, U).
  // Injected foam persists for tau seconds, which stretches each crest's injections into streaks behind it.
  //   gain           per 1/60 s frame (scaled by the frame time): how fast a crest below the threshold fills a texel
  //   statelessGain  the same ramp for the stateless foam the shader derives from J beyond the persistent window
  const FOAM = { tau: 6, foamLambda: 1, maxThreshold: 0.64, windLow: 5, windHigh: 9, gain: 0.9, statelessGain: 3.5 };
  function foamThreshold(windSpeed: number): number {
    const t = Math.min(1, Math.max(0, (windSpeed - FOAM.windLow) / (FOAM.windHigh - FOAM.windLow)));
    return FOAM.maxThreshold * t * t * (3 - 2 * t);
  }
  // Four-corner WGSL downsample. Elio's ocean_mip reads a storage buffer and writes a texture, so it cannot
  // chain levels on WebGPU; this reads the previous level as a texture instead (same 2x2 box filter).
  const MIP_WGSL = [
    "struct MipParams { w: u32, h: u32, a: u32, b: u32 };",
    "@group(0) @binding(0) var<uniform> mp: MipParams;",
    "@group(0) @binding(1) var src: texture_2d<f32>;",
    "@group(0) @binding(2) var dst: texture_storage_2d<rgba16float, write>;",
    "@compute @workgroup_size(8, 8) fn mip(@builtin(global_invocation_id) id: vec3<u32>) {",
    "  if (id.x >= mp.w || id.y >= mp.h) { return; }",
    "  let b = vec2<i32>(id.xy) * 2;",
    "  let s = textureLoad(src, b, 0) + textureLoad(src, b + vec2<i32>(1, 0), 0) + textureLoad(src, b + vec2<i32>(0, 1), 0) + textureLoad(src, b + vec2<i32>(1, 1), 0);",
    "  textureStore(dst, vec2<i32>(id.xy), s * 0.25);",
    "}",
  ].join("\n");
  const MIP_BINDINGS = [
    { group: 0, binding: 0, name: "mp", kind: "uniform" },
    { group: 0, binding: 1, name: "src", kind: "sampled-texture" },
    { group: 0, binding: 2, name: "dst", kind: "storage-texture", format: "rgba16float" },
  ];

  // The one place the Scene3D wind angle becomes the kernels' angle: waves travel toward (cos t, sin t) in (x, z).
  // Scene3D degrees: 0 travels toward +Z and positive turns toward +X.
  function travelAngle(degrees: number): number { return Math.PI / 2 - degrees * Math.PI / 180; }

  function finite(v: any, fallback: number, lo: number, hi: number): number {
    const x = Number.isFinite(v) && v !== 0 ? v : fallback;
    return Math.min(hi, Math.max(lo, x));
  }

  // resolve turns a normalized ocean (with ocean.spectrum) and a quality tier into the kernel parameters.
  function resolve(ocean: any, tier: any): any {
    const s = ocean.spectrum || {};
    const high = tier !== "low" && tier !== "medium";
    const nBig = tier === "low" ? 64 : tier === "medium" ? 128 : 256;
    const wanted = Array.isArray(s.cascades) && s.cascades.length ? s.cascades : DEFAULT_CASCADES;
    const list = tier === "low" && !(Array.isArray(s.cascades) && s.cascades.length) ? wanted.slice(0, 3) : wanted.slice(0, 4);
    const cascades = list.map(function (c: any, i: number) {
      const n = i === 0 ? 64 : nBig;
      const size = finite(c.size, DEFAULT_CASCADES[Math.min(i, 3)].size, 1, 1e5);
      // The Nyquist row and column stay empty only if kMax sits below the cascade's lowest allowed Nyquist wavenumber.
      const kMax = Math.min(c.kMax, 0.98 * Math.PI * 64 / size);
      return { size: size, kMin: Math.max(0, c.kMin), kMax: kMax, n: n };
    });
    return {
      high: high,
      windSpeed: finite(s.windSpeed, 9, 0.5, 40),
      fetchKm: finite(s.fetch, 300, 1, 2000),
      gamma: finite(s.peakEnhancement, 3.3, 1, 7),
      spread: finite(s.spread, 8, 1, 32),
      swell: Math.min(1, Math.max(0, Number.isFinite(s.swell) ? s.swell : 0)),
      windDir: travelAngle(Number.isFinite(ocean.windDirection) ? ocean.windDirection : 0),
      swellDir: travelAngle(Number.isFinite(s.swellDirection) ? s.swellDirection : (Number.isFinite(ocean.windDirection) ? ocean.windDirection : 0)),
      seed: (s.seed >>> 0),
      lambda: Math.max(0, Number.isFinite(ocean.choppiness) ? ocean.choppiness : 0.6),
      cascades: cascades,
    };
  }
  // layoutKey changes when buffers or textures must be rebuilt; paramKey when only ocean_spectrum must rerun.
  function layoutKey(r: any): string { return r.cascades.map(function (c: any) { return c.n + "@" + c.size; }).join(",") + "|" + (r.high ? 1 : 0); }
  function paramKey(r: any): string {
    return [r.windSpeed, r.fetchKm, r.gamma, r.spread, r.swell, r.windDir, r.swellDir, r.seed].join(",") + "|" +
      r.cascades.map(function (c: any) { return c.kMin + ":" + c.kMax; }).join(",");
  }

  // pack writes values into a uniform block laid out as the generated metadata says; a typo or a missing field throws.
  function pack(layout: any, values: any): ArrayBuffer {
    const out = new ArrayBuffer(Math.max(16, Math.ceil(layout.size / 16) * 16)), view = new DataView(out);
    const seen: any = {};
    for (const f of layout.fields) {
      let v = values[f.name];
      if (v === undefined) {
        if (!/^_pad/.test(f.name)) throw new Error("ocean-fft: uniform " + layout.struct + " lacks a value for " + f.name);
        v = 0;
      }
      seen[f.name] = true;
      if (f.type === "u32") view.setUint32(f.offset, v >>> 0, true);
      else if (f.type === "i32") view.setInt32(f.offset, v | 0, true);
      else if (f.type === "f32") view.setFloat32(f.offset, v, true);
      else throw new Error("ocean-fft: unsupported uniform type " + f.type);
    }
    for (const k of Object.keys(values)) if (!seen[k]) throw new Error("ocean-fft: uniform " + layout.struct + " has no field " + k);
    return out;
  }

  function layoutEntries(bindings: any[]): any[] {
    return bindings.map(function (b: any) {
      const e = Object.assign(Object.create(null), { binding: b.binding, visibility: GPUShaderStage.COMPUTE });
      if (b.kind === "uniform") e.buffer = { type: "uniform" };
      else if (b.kind === "storage-read") e.buffer = { type: "read-only-storage" };
      else if (b.kind === "storage-rw") e.buffer = { type: "storage" };
      else if (b.kind === "sampled-texture") e.texture = { sampleType: "float", viewDimension: "2d" };
      else e.storageTexture = { access: "write-only", format: b.format, viewDimension: "2d" };
      return e;
    });
  }

  function create(device: any, host: any): any {
    const label = "gosx-ocean-fft-";
    const pipelines: any = Object.create(null); // name -> { handle, layout }, kept across rebuilds
    const layouts: any = Object.create(null); // kernel -> bind group layout, shared by a kernel's entry points
    let res: any = null; // buffers, textures and bind groups of the current layout
    let key = "", pkey = "", resolved: any = null;
    let lastTime: number | null = null, foamOrigin: number[] | null = null, foamFlip = 0;
    const engine: any = {
      device: device, ready: false, layout: null, group: null, resolved: null, disposed: false,
      update: update, encode: encode, dispose: dispose, buildCount: 0, fail: fail,
    };
    // fail reports a problem outside pipeline validation (the surface shader patch) through the same optional-pass
    // route as a rejected pipeline: the guard disables "oceanFFT" and the ocean falls back to Gerstner waves.
    function fail(error: any): void {
      const message = "ocean-fft: " + (error && error.message ? error.message : String(error));
      if (host.guard) host.guard.fail("gosx-ocean-fft-surface", message);
    }
    const surfaceLayout = device.createBindGroupLayout({ label: label + "surface", entries: surfaceEntries() });
    engine.layout = surfaceLayout;
    const surfaceSampler = device.createSampler({ label: label + "sampler", magFilter: "linear", minFilter: "linear", mipmapFilter: "linear", addressModeU: "repeat", addressModeV: "repeat" });
    const surfaceUniform = device.createBuffer({ label: label + "surface", size: 96, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
    const surfaceData = new Float32Array(24);
    const dummy = device.createTexture({ label: label + "dummy", size: [1, 1, 1], format: "rgba16float", usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST });
    const dummyView = dummy.createView();

    function surfaceEntries() {
      const both = GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, entries = [];
      entries.push({ binding: SLOT.uniform, visibility: both, buffer: { type: "uniform" } },
        { binding: SLOT.sampler, visibility: both, sampler: { type: "filtering" } });
      for (let i = 0; i < 4; i++) entries.push({ binding: SLOT.disp + i, visibility: both, texture: { sampleType: "float" } }, { binding: SLOT.slope + i, visibility: both, texture: { sampleType: "float" } });
      entries.push({ binding: SLOT.foam, visibility: both, texture: { sampleType: "float" } });
      return entries;
    }
    function pipeline(name: string, kernelName: string, wgsl: string, bindings: any[], entry: string): any {
      if (pipelines[name]) return pipelines[name];
      const bgl = layouts[kernelName] || (layouts[kernelName] = device.createBindGroupLayout({ label: label + kernelName, entries: layoutEntries(bindings) }));
      const module = device.createShaderModule({ label: label + name, code: wgsl });
      const handle = host.create(device, "compute", {
        label: label + name, layout: device.createPipelineLayout({ bindGroupLayouts: [bgl] }),
        compute: { module: module, entryPoint: entry },
      });
      return pipelines[name] = { handle: handle, layout: bgl, kernel: kernelName };
    }
    function pipelinesFor(r: any): void {
      pipeline("spectrum", "oceanSpectrum", K.oceanSpectrum.wgsl, K.oceanSpectrum.bindings, K.oceanSpectrum.entry);
      pipeline("evolve", "oceanEvolve", K.oceanEvolve.wgsl, K.oceanEvolve.bindings, K.oceanEvolve.entry);
      pipeline("assemble", "oceanAssemble", K.oceanAssemble.wgsl, K.oceanAssemble.bindings, K.oceanAssemble.entry);
      pipeline("mip", "mip", MIP_WGSL, MIP_BINDINGS, "mip");
      pipeline("foam", "oceanFoam", K.oceanFoam.wgsl, K.oceanFoam.bindings, K.oceanFoam.entry);
      for (const c of r.cascades) {
        const fft = K["fftCompute" + c.n];
        pipeline("fft-rows-" + c.n, "fftCompute" + c.n, fft.wgsl, fft.bindings, "fft_rows");
        pipeline("fft-cols-" + c.n, "fftCompute" + c.n, fft.wgsl, fft.bindings, "fft_cols");
      }
    }
    function validated(p: any): any {
      const state = host.validation && host.validation.get(p.handle);
      return !state || state.ready === true ? (state && state.pipeline) || p.handle : null;
    }
    function allReady() {
      for (const name of Object.keys(pipelines)) if (!validated(pipelines[name])) return false;
      return true;
    }
    function buffer(name: string, size: number, usage: number): any {
      return device.createBuffer({ label: label + name, size: Math.max(16, Math.ceil(size / 4) * 4), usage: usage });
    }
    const STORAGE = GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST;
    const UNIFORM = GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST;
    function bind(p: any, resources: any): any {
      const kernel = p.kernel === "mip" ? { bindings: MIP_BINDINGS } : K[p.kernel];
      return device.createBindGroup({
        label: label + "bind", layout: p.layout,
        entries: kernel.bindings.map(function (b: any) {
          if (!(b.name in resources)) throw new Error("ocean-fft: no resource for binding " + b.name);
          return { binding: b.binding, resource: resources[b.name] };
        }),
      });
    }
    function layoutOf(kernel: any): any { return kernel.uniforms[0]; }

    function build(r: any): void {
      release();
      engine.buildCount++;
      const owned: any = { buffers: [], textures: [] }, cascades: any[] = [];
      const buf = function (name: string, size: number, usage: number) { const b = buffer(name, size, usage); owned.buffers.push(b); return b; };
      for (let ci = 0; ci < r.cascades.length; ci++) {
        const c = r.cascades[ci], n = c.n, texels = n * n;
        const levels = Math.log2(n) - 1; // down to 4x4
        const tex = function (name: string) {
          const t = device.createTexture({ label: label + name + ci, size: [n, n, 1], format: "rgba16float", mipLevelCount: levels,
            usage: GPUTextureUsage.STORAGE_BINDING | GPUTextureUsage.TEXTURE_BINDING });
          owned.textures.push(t);
          return t;
        };
        const casc = Object.assign(Object.create(null), { c: c, n: n, levels: levels, h0: buf("h0", texels * 16, STORAGE), omega: buf("omega", texels * 4, STORAGE),
          a: buf("fieldA", texels * 16, STORAGE), b: buf("fieldB", texels * 16, STORAGE),
          specU: buf("spectrum-u", 256, UNIFORM), evolveU: buf("evolve-u", 256, UNIFORM), assembleU: buf("assemble-u", 256, UNIFORM),
          disp: tex("disp"), slope: tex("slope") });
        const view = function (t: any, level: number) { return t.createView({ baseMipLevel: level, mipLevelCount: 1 }); };
        casc.spectrumGroup = bind(pipelines.spectrum, { spec: { buffer: casc.specU }, h0: { buffer: casc.h0 }, omega: { buffer: casc.omega } });
        casc.evolveGroup = bind(pipelines.evolve, { ev: { buffer: casc.evolveU }, h0: { buffer: casc.h0 }, omega: { buffer: casc.omega }, outA: { buffer: casc.a }, outB: { buffer: casc.b } });
        casc.fftA = bind(pipelines["fft-rows-" + n], { data: { buffer: casc.a } });
        casc.fftB = bind(pipelines["fft-rows-" + n], { data: { buffer: casc.b } });
        casc.assembleGroup = bind(pipelines.assemble, { assem: { buffer: casc.assembleU }, fieldA: { buffer: casc.a }, fieldB: { buffer: casc.b }, disp: view(casc.disp, 0), slope: view(casc.slope, 0) });
        casc.mips = [];
        for (let level = 1; level < levels; level++) {
          const w = n >> level;
          const u = buf("mip-u", 16, UNIFORM);
          device.queue.writeBuffer(u, 0, new Uint32Array([w, w, 0, 0]));
          for (const t of [casc.disp, casc.slope]) {
            casc.mips.push({ w: w, group: bind(pipelines.mip, { mp: { buffer: u }, src: view(t, level - 1), dst: view(t, level) }) });
          }
        }
        casc.dispView = casc.disp.createView();
        casc.slopeView = casc.slope.createView();
        cascades.push(casc);
      }
      const width = r.high ? 1024 : 512, cell = FOAM_WINDOW / width;
      const foam = Object.assign(Object.create(null), { width: width, cell: cell, buffers: [buf("foam-a", width * width * 4, STORAGE), buf("foam-b", width * width * 4, STORAGE)],
        u: buf("foam-u", 256, UNIFORM) });
      foam.tex = device.createTexture({ label: label + "foam", size: [width, width, 1], format: "rgba16float",
        usage: GPUTextureUsage.STORAGE_BINDING | GPUTextureUsage.TEXTURE_BINDING });
      owned.textures.push(foam.tex);
      foam.view = foam.tex.createView();
      foam.groups = [0, 1].map(function (i) {
        const fields: any = { foam: { buffer: foam.u }, foamIn: { buffer: foam.buffers[i] }, foamOut: { buffer: foam.buffers[1 - i] }, foamTex: foam.view };
        for (let j = 0; j < 4; j++) fields["fieldB" + j] = { buffer: cascades[Math.min(j, cascades.length - 1)].b };
        return bind(pipelines.foam, fields);
      });
      const entries = [{ binding: SLOT.uniform, resource: { buffer: surfaceUniform } }, { binding: SLOT.sampler, resource: surfaceSampler }];
      for (let i = 0; i < 4; i++) {
        entries.push({ binding: SLOT.disp + i, resource: cascades[i] ? cascades[i].dispView : dummyView });
        entries.push({ binding: SLOT.slope + i, resource: cascades[i] ? cascades[i].slopeView : dummyView });
      }
      entries.push({ binding: SLOT.foam, resource: foam.view });
      engine.group = device.createBindGroup({ label: label + "surface", layout: surfaceLayout, entries: entries });
      res = { owned: owned, cascades: cascades, foam: foam, needSpectrum: true };
      foamOrigin = null;
      foamFlip = 0;
    }
    function release() {
      if (!res) return;
      for (const b of res.owned.buffers) b.destroy();
      for (const t of res.owned.textures) t.destroy();
      res = null;
      engine.group = null;
    }

    // update builds or refreshes resources for this frame's spectrum and tier and decides readiness.
    function update(frame: any): boolean {
      const r = resolve(frame.ocean, frame.tier);
      resolved = engine.resolved = r;
      pipelinesFor(r);
      const lk = layoutKey(r), pk = paramKey(r);
      if (!res || lk !== key) { key = lk; build(r); pkey = pk; }
      else if (pk !== pkey) { pkey = pk; res.needSpectrum = true; }
      engine.ready = allReady();
      return engine.ready;
    }

    // encode writes the frame's uniforms and records every dispatch into one compute pass.
    function encode(encoder: any, frame: any): void {
      if (!engine.ready || !res) return;
      const r = resolved, q = device.queue;
      let t = frame.time % LOOP_PERIOD;
      if (!(t >= 0)) t = t < 0 ? t + LOOP_PERIOD : 0;
      const dt = lastTime === null ? 0 : Math.min(0.1, Math.max(0, frame.time - lastTime));
      lastTime = frame.time;
      const spectrumLayout = layoutOf(K.oceanSpectrum), evolveLayout = layoutOf(K.oceanEvolve), assembleLayout = layoutOf(K.oceanAssemble);
      const pass = encoder.beginComputePass({ label: label + "pass" });
      const run = function (p: any, group: any, x: number, y: number = 1) { pass.setPipeline(validated(p)); pass.setBindGroup(0, group); pass.dispatchWorkgroups(x, y, 1); };
      for (let ci = 0; ci < res.cascades.length; ci++) {
        const casc = res.cascades[ci], c = casc.c, n = casc.n, groups = Math.ceil(n * n / 64);
        if (res.needSpectrum) {
          q.writeBuffer(casc.specU, 0, pack(spectrumLayout, { n: n, cascade: ci, seed: r.seed, windSpeed: r.windSpeed, fetchKm: r.fetchKm, gamma: r.gamma,
            spread: r.spread, swell: r.swell, windDir: r.windDir, swellDir: r.swellDir, tile: c.size, kMin: c.kMin, kMax: c.kMax, loopPeriod: LOOP_PERIOD }));
          run(pipelines.spectrum, casc.spectrumGroup, groups);
        }
        q.writeBuffer(casc.evolveU, 0, pack(evolveLayout, { n: n, t: t, tile: c.size, loopPeriod: LOOP_PERIOD }));
        q.writeBuffer(casc.assembleU, 0, pack(assembleLayout, { n: n, lambda: r.lambda }));
        run(pipelines.evolve, casc.evolveGroup, groups);
        for (const data of [casc.fftA, casc.fftB]) {
          run(pipelines["fft-rows-" + n], data, n);
          run(pipelines["fft-cols-" + n], data, n);
        }
        run(pipelines.assemble, casc.assembleGroup, groups);
        for (const m of casc.mips) run(pipelines.mip, m.group, Math.ceil(m.w / 8), Math.ceil(m.w / 8));
      }
      res.needSpectrum = false;
      // Persistent foam: the window origin follows the camera in whole cells.
      const f = res.foam, cam = frame.camera || {};
      const ox = Math.floor((cam.x || 0) / f.cell) - f.width / 2, oz = Math.floor((cam.z || 0) / f.cell) - f.width / 2;
      const prev = foamOrigin || [ox, oz];
      foamOrigin = [ox, oz];
      const cs = res.cascades, sz = [1, 1, 1, 1], ns = [1, 1, 1, 1];
      for (let i = 0; i < cs.length; i++) { sz[i] = cs[i].c.size; ns[i] = cs[i].n; }
      q.writeBuffer(f.u, 0, pack(layoutOf(K.oceanFoam), { width: f.width, cascades: cs.length, originX: ox, originZ: oz, shiftX: ox - prev[0], shiftZ: oz - prev[1],
        dt: dt, tau: FOAM.tau, threshold: foamThreshold(r.windSpeed), gain: FOAM.gain * dt * 60, lambda: FOAM.foamLambda, cell: f.cell,
        n0: ns[0], n1: ns[1], n2: ns[2], n3: ns[3], size0: sz[0], size1: sz[1], size2: sz[2], size3: sz[3] }));
      run(pipelines.foam, f.groups[foamFlip], Math.ceil(f.width * f.width / 64));
      foamFlip = 1 - foamFlip;
      pass.end();
      // Surface uniform: per cascade (tile, band centre, texels, enabled), then foam and crest terms.
      const d = surfaceData;
      d.fill(0);
      for (let i = 0; i < cs.length; i++) {
        d[i * 4] = cs[i].c.size; d[i * 4 + 1] = Math.sqrt(Math.max(cs[i].c.kMin, 1e-6) * cs[i].c.kMax); d[i * 4 + 2] = cs[i].n; d[i * 4 + 3] = 1;
      }
      d[16] = 1 / (f.cell * f.width); d[17] = r.lambda > 0.01 ? FOAM.foamLambda / r.lambda : 0; d[18] = foamThreshold(r.windSpeed); d[19] = FOAM.statelessGain;
      d[20] = Math.max(0.3, 0.14 * r.windSpeed * r.windSpeed / 9.81); d[23] = FOAM_RADIUS;
      q.writeBuffer(surfaceUniform, 0, d);
    }

    function dispose() {
      engine.disposed = true; engine.ready = false;
      release();
      surfaceUniform.destroy(); dummy.destroy();
    }
    return engine;
  }

  // frame is the one call the WebGPU renderer makes per frame before the main pass. It owns the engine's lifetime through
  // resources.fft: created when the ocean has a spectrum, disposed when it does not, when the pass was disabled,
  // or when the device changed.
  function frame(resources: any, host: any): any {
    const ocean = host.environment && host.environment.ocean;
    let eng = resources.fft;
    if (!ocean || !ocean.spectrum || (host.guard && host.guard.disabled.has("oceanFFT")) || (eng && eng.device !== host.device)) {
      if (eng) { eng.dispose(); resources.fft = null; }
      return null;
    }
    if (!eng) eng = resources.fft = create(host.device, host);
    const f = { ocean: ocean, tier: host.tier, time: Number.isFinite(host.time) ? host.time : 0, camera: host.camera };
    if (eng.update(f)) eng.encode(host.encoder, f);
    return eng;
  }

  window.__gosx_scene3d_ocean_fft_api = { frame: frame, resolve: resolve, pack: pack, travelAngle: travelAngle, foamThreshold: foamThreshold, FOAM: FOAM, MIP_WGSL: MIP_WGSL, LOOP_PERIOD: LOOP_PERIOD };
})();
