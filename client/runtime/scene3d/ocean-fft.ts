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
  const WAKE_DEBUG_RANGE = 0.3; // metres of wake height at the ends of the debug colour ramp
  const crestAmplitude = function (windSpeed: number): number { return Math.max(0.3, 0.14 * windSpeed * windSpeed / 9.81); };
  const SURFACE_FLOATS = 48; // c[4], f, m, w[4], k[2]: twelve vec4s, the layout both surface variants declare
  const CASCADES = 4;
  let timingSource: any = null; // the running WebGPU graph, for timing()
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
    runner.setNodes(ocean.graphNodes || {}); // the graph's logical nodes ("hull") to scene node ids
    feedNodes(runner, ocean);
  }

  // feedNodes sets the graph's pose parameters from the scene nodes it names. A graph reads a node with a "node" parameter source and needs
  // nothing here; one that declares hullX, hullZ, hullHeading and hullSpeed as value parameters (the wake's hull) gets them from the node
  // bound to "hull": position, the yaw whose forward is (sin, cos), and the speed through the water. No node, or none published: unchanged.
  const HULL: any = [["hullX", "x"], ["hullZ", "z"], ["hullHeading", "heading"], ["hullSpeed", "speed"]];
  function feedNodes(runner: any, ocean: any): void {
    const id = ocean.graphNodes && ocean.graphNodes.hull;
    if (!id) return;
    const pose = runner.nodePose ? runner.nodePose(id) : null;
    if (!pose) return;
    for (const entry of HULL) {
      const decl = runner.graph.params.find(function (p: any) { return p.name === entry[0]; });
      if (decl && decl.source.kind === "value") runner.setValue(entry[0], pose[entry[1]]);
    }
  }

  // surfaceData fills the surface uniform the spectral shaders read; see the declarations in the two surface files.
  function surfaceData(runner: any, info: any, out: Float32Array, ocean?: any, camera?: any): void {
    out.fill(0);
    const wrapped = runner.exportParams(), foam = info.foam;
    info.cascades.forEach(function (c: any, i: number) {
      out[i * 4] = c.tile; out[i * 4 + 1] = Math.sqrt(Math.max(c.kMin, 1e-6) * c.kMax); out[i * 4 + 2] = c.n; out[i * 4 + 3] = 1;
      out[24 + i * 4] = wrapped["waterModX" + i] || 0; out[24 + i * 4 + 1] = wrapped["waterModZ" + i] || 0;
    });
    const span = foam.cell * foam.width, wrap = function (v: number) { return v - Math.floor(v / span) * span; };
    // The wake window (the graph's wake export and wake* exportParams); all zero, so masked out, when the graph has no wake or it is off.
    const wakeTex = runner.export("wake");
    if (wakeTex) {
      // The window offsets are as of the wake's last run (see WAKE_THROTTLE); the camera moves every frame, so re-anchor them to it: the held
      // camera-relative offset is (window origin - held camera), and the offset to the camera now is that plus (held camera - camera now).
      const dx = camera ? runner.value("cameraX") - (camera.x || 0) : 0, dz = camera ? runner.value("cameraZ") - (camera.z || 0) : 0;
      out[40] = (wrapped.wakeCamRelX || 0) + dx; out[41] = (wrapped.wakeCamRelZ || 0) + dz; out[42] = wrapped.wakeOriginModX || 0; out[43] = wrapped.wakeOriginModZ || 0;
      out[44] = wrapped.wakeCell || 0; out[45] = wrapped.wakeOn || 0; out[46] = wakeTex.width;
      out[47] = ocean && ocean.wake && ocean.wake.debug ? WAKE_DEBUG_RANGE : 0; // the wake height debug view (ocean.wake.debug): ramp range in metres
    }
    out[16] = 1 / span; out[17] = 1; out[18] = runner.value("foamThreshold"); out[19] = STATELESS_GAIN;
    out[20] = crestAmplitude(runner.value("windSpeed")); out[21] = wrap(runner.value("waterX")); out[22] = wrap(runner.value("waterZ")); out[23] = FOAM_RADIUS;
  }

  // frameInputs are the graph's "frame" parameter sources.
  function frameInputs(state: any, host: any): any {
    const time = Number.isFinite(host.time) ? host.time : 0;
    const clock = state.last === null ? 0 : Math.max(0, time - state.last), dt = Math.min(MAX_DT, clock);
    state.last = time;
    const cam = host.camera || {};
    return { time: time, dt: dt, accumulateDt: clock, cameraX: cam.x || 0, cameraY: cam.y || 0, cameraZ: cam.z || 0 };
  }

  // nodePose is the graph runner's view of the scene: the pose a vessel (or any node that publishes) put in window.__gosx_scene3d_node_poses[id]
  // this frame, as { x, z, heading, speed } with heading the yaw whose forward direction is (sin, cos) in (x, z) and speed in m/s.
  function nodePose(id: string): any {
    const registry = (window as any).__gosx_scene3d_node_poses;
    return registry && registry[id] ? registry[id] : null;
  }

  // waterRelative turns a node pose's speed into the speed through the water: the pose's velocity (speed along its heading) minus the water's
  // drift velocity (the graph's driftX and driftZ), which is what shapes a wake. A ship at rest in drifting water moves through it at the drift
  // speed; without drift the speed is unchanged. `runner` is a function because a graph is built after its host.
  function waterRelative(pose: any, runner: () => any): any {
    const r = pose && runner();
    if (!r || !(Math.abs(r.value("driftX")) + Math.abs(r.value("driftZ")) > 0)) return pose;
    const vx = pose.speed * Math.sin(pose.heading) - r.value("driftX"), vz = pose.speed * Math.cos(pose.heading) - r.value("driftZ");
    return { x: pose.x, z: pose.z, heading: pose.heading, speed: Math.hypot(vx, vz) };
  }

  // water is the running graph's water frame, in float64: the origin of the water (what the drift has integrated) and the wave
  // time. A CPU query samples the wave field at (position - origin) at that time to agree with what is drawn.
  function water(): any { return lastWater; }
  // timing is the WebGPU graph's own GPU time (its compute passes, which the renderer's frame stamps do not cover): the latest sample and the
  // last 120, in milliseconds, or null without timestamp-query. The renderer reports its render passes separately.
  function timing(): any { return timingSource && !timingSource.failed ? timingSource.gpuTiming() : null; }
  // The record is reused, so publishing allocates nothing. scale, driftX and driftZ are the rates the water moves at (a host carries a
  // reading forward with them); the page's Scene3D ocean-water record copies the whole frame (see recordOceanWater in mount-backend.ts).
  const waterRecord: any = { x: 0, z: 0, time: 0, scale: 1, driftX: 0, driftZ: 0, gen: 0 };
  let generation = 0; // counts graph builds: a host sees Time, X and Z restart when it changes
  function publish(runner: any): void {
    if (runner.failed) { lastWater = null; return; }
    waterRecord.x = runner.value("waterX"); waterRecord.z = runner.value("waterZ"); waterRecord.time = runner.value("simTime");
    waterRecord.gen = runner.waterGen; waterRecord.scale = runner.value("timeScale"); waterRecord.driftX = runner.value("driftX"); waterRecord.driftZ = runner.value("driftZ");
    lastWater = waterRecord;
  }
  // fill writes the running graph's water frame into a host's record (indices 1 to 7 and 9 of the record) and answers whether there is one: false
  // means the Gerstner sea is what is drawn (see recordOceanWater in mount-backend.ts).
  function fill(w: Float64Array): boolean {
    const f = lastWater;
    hostProbe.arr = w; // the same array carries the host's probe (see HP below); the next frame's issue reads its points
    if (!f) return false;
    w[1] = f.time; w[2] = f.x; w[3] = f.z; w[4] = 1; w[5] = f.scale; w[6] = f.driftX; w[7] = f.driftZ; w[9] = f.gen;
    deliverProbe(w, f);
    return true;
  }

  // --- Host probe: drawn heights at host-given points (Scene3D.OceanProbe in Go) -------------------------------------
  //
  // The host's record (see recordOceanWater in mount-backend.ts) is one Float64Array: the water frame at 0 to 9, then the probe. The host writes
  // HP.COUNT, the points (x, z pairs from HP.PTS) and a request counter HP.REQ; the page answers at HP.SEQ to HP.TICK, echoes the counter of the
  // request the reading answers at HP.ECHO, and writes the per-point results (height, normal x, normal z) from HP.OUT. Nothing is issued while
  // HP.COUNT is 0. A frame that asks has its points read back asynchronously (WebGPU: a compute pass into a staging ring that is mapped after the
  // frame is submitted; WebGL2: probeGL's pass into a float target and a pixel buffer behind a fence), and the reading is tagged with the Seq and
  // Generation of the frame it was issued in: HP.SEQ is that frame's water-frame Seq, so the host's age in frames is the water frame's Seq minus
  // HP.SEQ. HP.LAT is that age when the page wrote the reading. Only a probe issued in the current ocean update and in a frame the renderer submitted
  // is tagged; any other is dropped. The heights are what the shaders write: the ocean level is already in them. The state is per page, like lastWater:
  // one ocean mount per page. Layout: the oceanProbe* constants in engine/wasm/scene_js.go.
  const HP = { COUNT: 10, SEQ: 11, GEN: 12, FLAGS: 13, N: 14, LAT: 15, AT: 16, TICK: 17, PTS: 18, OUT: 146, REQ: 338, ECHO: 339 }, F_VALID = 1, F_UNAVAILABLE = 4;
  const hostProbe: any = { arr: null, pending: null, ready: null, delivered: null, failed: "", reported: "", tick: 0 };
  let updateTick = 0; // counts ocean updates (one per frame the graph is asked to run), to tell this frame's probe from an older one
  function hostCount(): number { const a = hostProbe.arr; return a ? Math.max(0, Math.min(MAX_POINTS, Math.floor(a[HP.COUNT]) || 0)) : 0; }
  // unpack reads n records of (height, normal x, normal z, w) from a staging buffer's floats.
  function unpack(raw: Float32Array, n: number): any {
    const out: any = { heights: [], nx: [], nz: [] };
    for (let i = 0; i < n; i++) { out.heights.push(raw[i * 4]); out.nx.push(raw[i * 4 + 1]); out.nz.push(raw[i * 4 + 2]); }
    return out;
  }
  // issued records a probe issued in this update; completed makes a finished, tagged read the host's newest reading and drops an untagged one.
  function issued(meta: any): void { meta.tick = hostProbe.tick; meta.request = hostProbe.arr[HP.REQ]; hostProbe.pending = meta; }
  function completed(meta: any, raw: Float32Array): void {
    if (meta.seq === undefined) return;
    if (!hostProbe.ready || hostProbe.ready.meta.seq < meta.seq) { hostProbe.ready = unpack(raw, meta.n); hostProbe.ready.meta = meta; }
  }
  // deliverProbe tags the probe issued in this frame's render with the frame's Seq (the record counts it after fill returns), and writes the newest
  // completed reading, once, into the record. A host that asks when the probe cannot answer is told so, once per reason.
  function deliverProbe(w: Float64Array, f: any): void {
    const p = hostProbe, seq = w[0] + 1, m = p.pending;
    if (m) {
      p.pending = null;
      if (m.tick === p.tick && m.seq === undefined) { m.seq = seq; m.gen = f.gen; if (m.start) m.start(); } else if (m.drop) m.drop();
    }
    const r = p.ready;
    if (r && r !== p.delivered) {
      for (let i = 0; i < r.meta.n; i++) { w[HP.OUT + 3 * i] = r.heights[i]; w[HP.OUT + 3 * i + 1] = r.nx[i]; w[HP.OUT + 3 * i + 2] = r.nz[i]; }
      w[HP.SEQ] = r.meta.seq; w[HP.GEN] = r.meta.gen; w[HP.FLAGS] = F_VALID; w[HP.N] = r.meta.n; w[HP.LAT] = seq - r.meta.seq; w[HP.ECHO] = r.meta.request;
      w[HP.AT] = performance.timeOrigin + performance.now(); w[HP.TICK]++;
      p.delivered = r;
    } else if (p.failed && p.reported !== p.failed && hostCount()) {
      w[HP.SEQ] = seq; w[HP.GEN] = f.gen; w[HP.FLAGS] = F_UNAVAILABLE; w[HP.N] = 0; w[HP.LAT] = 0; w[HP.ECHO] = w[HP.REQ]; w[HP.AT] = performance.timeOrigin + performance.now(); w[HP.TICK]++;
      p.reported = p.failed;
    }
    if (!p.failed) p.reported = "";
  }
  // resetProbe forgets everything the host probe held; a new graph starts clean, and a failure of one does not outlive it.
  function resetProbe(): void { Object.assign(hostProbe, { arr: null, pending: null, ready: null, delivered: null, failed: "", reported: "" }); }


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
        const read = unpack(raw, meta.count);
        probe.previous = probe.latest;
        probe.latest = { at: meta.at, count: meta.count, points: meta.points, heights: read.heights, nx: read.nx, nz: read.nz };
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
      entries.push({ binding: SURFACE.SLOT.foam, visibility: both, texture: { sampleType: "float" } }, { binding: SURFACE.SLOT.wake, visibility: both, texture: { sampleType: "float" } });
      return entries;
    };
    const layout = device.createBindGroupLayout({ label: label + "surface", entries: surfaceEntries() });
    const sampler = device.createSampler({ label: label + "sampler", magFilter: "linear", minFilter: "linear", mipmapFilter: "linear", addressModeU: "repeat", addressModeV: "repeat" });
    const uniform = device.createBuffer({ label: label + "surface", size: SURFACE_FLOATS * 4, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
    const dummy = device.createTexture({ label: label + "dummy", size: [1, 1, 1], format: "rgba16float", usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST });
    const dummyView = dummy.createView(), data = new Float32Array(SURFACE_FLOATS);
    const state: any = { last: null, tier: "", runner: null, info: null, viewList: null };
    const ring = createProbeRing(device, function () { return host.now ? host.now() : (typeof performance !== "undefined" ? performance.now() : 0); });
    // hostGPU answers the host's probe (see "Host probe" above) with probeWGSL over the cascades' textures, then copies the output into a ring of three staging
    // buffers that are mapped after the frame is submitted (deliverProbe starts the map once the frame is known to be one): the reading is never awaited.
    // Nothing is created until a host asks. The pipeline is made outside the renderer's pass guard (a failure there would disable the whole ocean or fall back
    // to WebGL2): inside an error scope, and a failure only marks the probe unavailable.
    const hg: any = { pipeline: null, building: false, bgl: null, params: null, pts: null, out: null, slots: [], f32: new Float32Array(20), ptsData: new Float32Array(MAX_POINTS * 4), group: null, views: null };
    const hostGPU: any = {
      update: function (frame: any, runner: any, info: any): void {
        const n = hostCount();
        if (!n || hostProbe.failed || !info) return;
        let slot = hg.slots.find(function (x: any) { return x.state === "free"; });
        if (!slot && hg.slots.length >= RING) return; // all reads in flight
        try {
          if (!hg.bgl) {
            hg.bgl = device.createBindGroupLayout({ label: label + "probe", entries: ([
              { binding: 0, visibility: GPUShaderStage.COMPUTE, buffer: { type: "uniform" } }, { binding: 1, visibility: GPUShaderStage.COMPUTE, buffer: { type: "read-only-storage" } },
              { binding: 2, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } }, { binding: 3, visibility: GPUShaderStage.COMPUTE, sampler: { type: "filtering" } }] as any[])
              .concat([4, 5, 6, 7, 8, 9, 10, 11].map(function (b: number) { return { binding: b, visibility: GPUShaderStage.COMPUTE, texture: { sampleType: "float" } }; })) });
            hg.params = device.createBuffer({ label: label + "probe-params", size: 80, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
            hg.pts = device.createBuffer({ label: label + "probe-points", size: MAX_POINTS * RECORD_BYTES, usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST });
            hg.out = device.createBuffer({ label: label + "probe-out", size: MAX_POINTS * RECORD_BYTES, usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_SRC });
          }
          if (!hg.pipeline) {
            if (!hg.building) {
              hg.building = true;
              const scoped = typeof device.pushErrorScope === "function", failed = function (e: any): void { hostProbe.failed = "probe-pipeline: " + String(e && e.message || e); };
              if (scoped) device.pushErrorScope("validation");
              try {
                const desc = { label: "gosx-probe-ocean", layout: device.createPipelineLayout({ bindGroupLayouts: [hg.bgl] }), compute: { module: device.createShaderModule({ label: "gosx-probe-ocean", code: probeWGSL() }), entryPoint: "main" } };
                (device.createComputePipelineAsync ? device.createComputePipelineAsync(desc) : Promise.resolve(device.createComputePipeline(desc))).then(function (made: any) { hg.pipeline = made; }, failed);
              } catch (e) { failed(e); }
              if (scoped) device.popErrorScope().then(function (e: any) { if (e) failed(e); }, failed);
            }
            return; // the pipeline is still being built; asks again next frame
          }
          if (!slot) { slot = { state: "free", buffer: device.createBuffer({ label: label + "probe-staging", size: MAX_POINTS * RECORD_BYTES, usage: MAP_READ | COPY_DST }) }; hg.slots.push(slot); }
          const a = hostProbe.arr, wrapped = runner.exportParams(), f = hg.f32, level = Number(frame.ocean && frame.ocean.level) || 0, views: any[] = [];
          f.fill(0); new Uint32Array(f.buffer)[0] = n; f[1] = level;
          info.cascades.forEach(function (c: any, i: number) { f[4 + i * 4] = c.tile; f[5 + i * 4] = 1; f[6 + i * 4] = wrapped["waterModX" + i] || 0; f[7 + i * 4] = wrapped["waterModZ" + i] || 0; });
          hg.ptsData.fill(0); for (let i = 0; i < n; i++) { hg.ptsData[i * 4] = a[HP.PTS + 2 * i]; hg.ptsData[i * 4 + 1] = a[HP.PTS + 2 * i + 1]; }
          device.queue.writeBuffer(hg.params, 0, f); device.queue.writeBuffer(hg.pts, 0, hg.ptsData);
          for (let i = 0; i < CASCADES; i++) { const d = runner.export("disp" + i), sl = runner.export("slope" + i); views.push(d ? d.view : dummyView, sl ? sl.view : dummyView); }
          if (!hg.group || views.some(function (v: any, i: number) { return v !== hg.views[i]; })) { // the bind group changes only when a texture does (a tier change)
            const entries: any[] = [{ binding: 0, resource: { buffer: hg.params } }, { binding: 1, resource: { buffer: hg.pts } }, { binding: 2, resource: { buffer: hg.out } }, { binding: 3, resource: sampler }];
            for (let i = 0; i < CASCADES; i++) entries.push({ binding: 4 + i, resource: views[2 * i] }, { binding: 8 + i, resource: views[2 * i + 1] });
            hg.group = device.createBindGroup({ label: label + "probe", layout: hg.bgl, entries: entries }); hg.views = views;
          }
          const pass = frame.encoder.beginComputePass({ label: label + "probe" });
          pass.setPipeline(hg.pipeline); pass.setBindGroup(0, hg.group); pass.dispatchWorkgroups(1); pass.end();
          frame.encoder.copyBufferToBuffer(hg.out, 0, slot.buffer, 0, MAX_POINTS * RECORD_BYTES);
          const target = slot, meta: any = { n: n };
          slot.state = "copied";
          // Called by deliverProbe once the frame is known to be submitted; a probe of a frame that was not is dropped and its stale buffer never mapped.
          meta.start = function (): void {
            target.state = "mapping";
            target.buffer.mapAsync(MAP_READ).then(function () {
              if (target.state !== "mapping") return;
              const raw = new Float32Array(target.buffer.getMappedRange().slice(0)); target.buffer.unmap(); target.state = "free";
              completed(meta, raw);
            }, function () { target.state = "free"; });
          };
          meta.drop = function (): void { target.state = "free"; };
          issued(meta);
        } catch (error) { hostProbe.failed = String(error && error.message || error); }
      },
      dispose: function (): void {
        for (const slot of hg.slots) { slot.state = "disposed"; slot.buffer.destroy(); }
        for (const b of [hg.params, hg.pts, hg.out]) if (b) b.destroy();
      },
    };
    const engine: any = { device: device, ready: false, layout: layout, group: null, disposed: false, tier: "", update: update, dispose: dispose, fail: fail };

    // fail reports a problem outside the graph (the surface shader patch) through the optional-pass route: the guard disables the
    // pass and the ocean falls back to Gerstner waves.
    function fail(error: any): void {
      const message = "ocean-fft: " + (error && error.message ? error.message : String(error));
      if (host.guard) host.guard.fail("gosx-ocean-fft-surface", message);
    }
    function build(tier: string): void {
      if (state.runner) state.runner.dispose();
      const runner = G.create({ module: MODULE, backend: "webgpu", tier: tier, host: { device: device, create: host.create, validation: host.validation, nodePose: function (id: string) { return waterRelative(nodePose(id), function () { return state.runner; }); } } });
      runner.waterGen = ++generation; state.runner = runner; state.tier = tier; state.last = null; state.viewList = null; engine.group = null; engine.ready = false;
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
      const wake = runner.export("wake");
      entries.push({ binding: SURFACE.SLOT.wake, resource: wake ? wake.view : dummyView });
      views.push(wake ? wake.view : dummyView);
      if (state.viewList && views.every(function (v: any, i: number) { return v === state.viewList[i]; })) return;
      state.viewList = views;
      engine.group = device.createBindGroup({ label: label + "surface", layout: layout, entries: entries });
    }
    // update runs the graph for this frame in the frame's encoder.
    function update(frame: any): boolean {
      const tier = tierOf(frame.tier);
      hostProbe.tick = ++updateTick;
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
      surfaceData(runner, state.info, data, frame.ocean, frame.camera);
      device.queue.writeBuffer(uniform, 0, data);
      publish(runner); timingSource = runner;
      hostGPU.update(frame, runner, state.info);
      engine.ready = true;
      return true;
    }
    function dispose(): void {
      if (engine.disposed) return;
      engine.disposed = true; engine.ready = false; engine.group = null;
      if (state.runner) state.runner.dispose();
      lastWater = null; timingSource = null; probe.latest = probe.previous = null; resetProbe();
      ring.dispose(); hostGPU.dispose(); uniform.destroy(); dummy.destroy();
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

  // On WebGL2 the wake graph (the Kelvin-wave FFTs, about 40 small passes) runs every second frame with the dt and the window movement of
  // both frames; its window offsets and the camera they were made with are held, so the surface samples the texture where it was written.
  // The measured split is in the pull request; the graph runner's throttle option is documented in elio-graph-gather.ts.
  const WAKE_THROTTLE = [{ match: "^wake\\.", every: 2, hold: "^(wake|camera[XZ])" }];

  // diagnostics are harness hooks: window.__gosx_elio_diagnostics = { profile(name, fn), throttle: [{ match, every }] } (both optional).
  function diagnostics(): any { return (typeof window !== "undefined" && (window as any).__gosx_elio_diagnostics) || {}; }

  // probeFragment is the WebGL2 host probe's shader: it finds the surface point above a world (x, z) the way the ocean vertex shader displaces it
  // (the cascades' displacement textures at level zero, at the water offset wrapped by each tile, summed over the enabled cascades) by Newton
  // iteration on the horizontal map, and writes (height, normal x, normal z) for one point per texel of a row. It leaves out what the vertex shader adds
  // from a bathymetry (shoaling) and from a ship wake; both are zero for a deep-water sea without a wake.
  function probeFragment(): string {
    let s = "#version 300 es\nprecision highp float;\nprecision highp sampler2D;\nuniform vec4 pts[" + MAX_POINTS + "], cs[4]; uniform float level;\n";
    for (let i = 0; i < 4; i++) s += "uniform sampler2D d" + i + ", s" + i + ";\n";
    s += "out vec4 o;\nvoid ev(vec2 q, out vec3 s, out vec3 dx, out vec3 dz) {\n  s = vec3(0.); dx = vec3(1., 0., 0.); dz = vec3(0., 0., 1.);\n";
    for (let i = 0; i < 4; i++) s += "  if (cs[" + i + "].y > 0.5) { vec2 uv = (q - cs[" + i + "].zw) / cs[" + i + "].x; vec4 a = textureLod(d" + i + ", uv, 0.), b = textureLod(s" + i + ", uv, 0.); s += a.xyz; dx += vec3(b.z, b.x, a.w); dz += vec3(a.w, b.y, b.w); }\n";
    s += "}\nvoid main() {\n  vec2 t = pts[int(gl_FragCoord.x)].xy, q = t; vec3 s, dx, dz;\n";
    s += "  for (int k = 0; k < 8; k++) {\n    ev(q, s, dx, dz);\n    vec2 e = q + s.xz - t; float det = dx.x * dz.z - dz.x * dx.z;\n";
    s += "    if (abs(det) > 1e-4) q -= vec2(dz.z * e.x - dz.x * e.y, dx.x * e.y - dx.z * e.x) / det; else q -= e * 0.5;\n  }\n";
    s += "  ev(q, s, dx, dz); vec3 n = normalize(cross(dz, dx)); o = vec4(level + s.y, n.x, n.z, 1.);\n}\n";
    return s;
  }
  // probeWGSL is the same pass as probeFragment for WebGPU, as a compute shader over the cascades' displacement and slope textures. The host probe uses it, not the
  // graph's ocean_probe kernel, for the normals: since Elio v0.6.0 the kernel's heights match the drawn textures (max 0.001 m at 35 points on an RTX 5070 Ti) but its
  // normal is a 0.5 m central difference of height, which measured up to 0.2 in a component from the slope-texture normal this pass writes (and the shader draws).
  function probeWGSL(): string {
    let s = "struct E { s: vec3<f32>, dx: vec3<f32>, dz: vec3<f32> };\nstruct P { count: u32, level: f32, a: f32, b: f32, cs: array<vec4<f32>, 4> };\n";
    s += "@group(0) @binding(0) var<uniform> P0: P;\n@group(0) @binding(1) var<storage, read> pts: array<vec4<f32>>;\n@group(0) @binding(2) var<storage, read_write> outp: array<vec4<f32>>;\n@group(0) @binding(3) var smp: sampler;\n";
    for (let i = 0; i < 4; i++) s += "@group(0) @binding(" + (4 + i) + ") var d" + i + ": texture_2d<f32>;\n@group(0) @binding(" + (8 + i) + ") var s" + i + ": texture_2d<f32>;\n";
    s += "fn ev(q: vec2<f32>) -> E {\n  var e: E; e.s = vec3<f32>(0.0); e.dx = vec3<f32>(1.0, 0.0, 0.0); e.dz = vec3<f32>(0.0, 0.0, 1.0);\n";
    for (let i = 0; i < 4; i++) s += "  if (P0.cs[" + i + "].y > 0.5) { let uv = (q - P0.cs[" + i + "].zw) / P0.cs[" + i + "].x; let a = textureSampleLevel(d" + i + ", smp, uv, 0.0); let b = textureSampleLevel(s" + i + ", smp, uv, 0.0); e.s += a.xyz; e.dx += vec3<f32>(b.z, b.x, a.w); e.dz += vec3<f32>(a.w, b.y, b.w); }\n";
    s += "  return e;\n}\n@compute @workgroup_size(64) fn main(@builtin(global_invocation_id) g: vec3<u32>) {\n  let i = g.x; if (i >= P0.count) { return; }\n  let t = pts[i].xy; var q = t; var e: E;\n";
    s += "  for (var k = 0; k < 8; k = k + 1) {\n    e = ev(q); let ex = q.x + e.s.x - t.x; let ez = q.y + e.s.z - t.y; let det = e.dx.x * e.dz.z - e.dz.x * e.dx.z;\n";
    s += "    if (abs(det) > 1e-4) { q = q - vec2<f32>(e.dz.z * ex - e.dz.x * ez, e.dx.x * ez - e.dx.z * ex) / det; } else { q = q - vec2<f32>(ex, ez) * 0.5; }\n  }\n";
    s += "  e = ev(q); let n = normalize(cross(e.dz, e.dx)); outp[i] = vec4<f32>(P0.level + e.s.y, n.x, n.z, 1.0);\n}\n";
    return s;
  }
  const PROBE_VERTEX = "#version 300 es\nvoid main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2. - 1., 0., 1.); }";

  function createGL(gl: any, host: any): any {
    const state: any = { last: null };
    const runner = G.create({ module: MODULE, backend: "webgl2", tier: tierOf(host.tier), host: { gl: gl, caps: host.caps, textureCache: host.textureCache, profile: host.profile || diagnostics().profile, throttle: host.throttle || diagnostics().throttle || WAKE_THROTTLE, nodePose: function (id: string) { return waterRelative(nodePose(id), function () { return engine.runner; }); } } });
    const data = new Float32Array(SURFACE_FLOATS), cache = new Map();
    runner.waterGen = ++generation;
    const info = runner.graph ? describe(runner.graph) : null;
    const engine: any = { gl: gl, runner: runner, tier: tierOf(host.tier), disposed: false, update: update, bind: bind, dispose: dispose, fail: function (reason: string) { runner.fail(reason); } };
    function update(frame: any): boolean {
      if (runner.failed) return false;
      hostProbe.tick = ++updateTick;
      feed(runner, frame.ocean);
      if (!runner.run(frameInputs(state, frame))) return false;
      surfaceData(runner, info, data, frame.ocean, frame.camera);
      publish(runner);
      probeGL(frame);
      return true;
    }
    // probeGL answers the host's probe (see "Host probe" above): it polls the fences of earlier reads, and when the host asks it draws the
    // points into a row of a float target, reads the row into a pixel buffer and fences it. A frame that nobody asks about does nothing
    // but the poll, and creates nothing. The first ask compiles one small shader, which can cost a frame.
    const pg: any = { program: null, fbo: null, tex: null, slots: [] };
    function probeGL(frame: any): void {
      for (const slot of pg.slots) {
        if (slot.state !== "fenced") continue;
        const status = gl.clientWaitSync(slot.sync, 0, 0);
        if (status !== gl.ALREADY_SIGNALED && status !== gl.CONDITION_SATISFIED && status !== gl.WAIT_FAILED) continue; // not finished: never wait
        slot.state = "free"; gl.deleteSync(slot.sync); slot.sync = null;
        if (status === gl.WAIT_FAILED) { hostProbe.failed = "probe-fence"; continue; }
        if (slot.meta.seq === undefined) continue; // issued in a frame the renderer did not submit
        gl.bindBuffer(gl.PIXEL_PACK_BUFFER, slot.pbo); gl.getBufferSubData(gl.PIXEL_PACK_BUFFER, 0, slot.dst); gl.bindBuffer(gl.PIXEL_PACK_BUFFER, null);
        completed(slot.meta, slot.dst);
      }
      const n = hostCount();
      if (!n || hostProbe.failed) return;
      const slot = pg.slots.find(function (x: any) { return x.state === "free"; }) || (pg.slots.length < RING ? null : undefined);
      if (slot === undefined) return; // all reads in flight
      const saved = G.glState.save(gl), cascades = info.cascades, wrapped = runner.exportParams(), level = Number(frame.ocean && frame.ocean.level) || 0;
      try {
        if (!pg.program) {
          const shaders: any[] = [], compile = function (type: number, source: string): any { const sh = gl.createShader(type); shaders.push(sh); gl.shaderSource(sh, source); gl.compileShader(sh); if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) throw new Error("probe-compile"); return sh; };
          const prog = gl.createProgram();
          try {
            gl.attachShader(prog, compile(gl.VERTEX_SHADER, PROBE_VERTEX)); gl.attachShader(prog, compile(gl.FRAGMENT_SHADER, probeFragment())); gl.linkProgram(prog);
            if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) throw new Error("probe-link");
          } catch (error) { gl.deleteProgram(prog); throw error; } finally { for (const sh of shaders) gl.deleteShader(sh); }
          pg.program = prog; pg.tex = gl.createTexture(); gl.bindTexture(gl.TEXTURE_2D, pg.tex); gl.texStorage2D(gl.TEXTURE_2D, 1, gl.RGBA32F, MAX_POINTS, 1);
          pg.fbo = gl.createFramebuffer(); gl.bindFramebuffer(gl.FRAMEBUFFER, pg.fbo); gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, pg.tex, 0);
          if (gl.checkFramebufferStatus(gl.FRAMEBUFFER) !== gl.FRAMEBUFFER_COMPLETE) throw new Error("probe-target");
          gl.useProgram(prog);
          for (let i = 0; i < 4; i++) { gl.uniform1i(gl.getUniformLocation(prog, "d" + i), SURFACE_GL.UNIT.disp + i); gl.uniform1i(gl.getUniformLocation(prog, "s" + i), SURFACE_GL.UNIT.slope + i); }
          pg.loc = { pts: gl.getUniformLocation(prog, "pts[0]"), cs: gl.getUniformLocation(prog, "cs[0]"), level: gl.getUniformLocation(prog, "level") };
          pg.pts = new Float32Array(MAX_POINTS * 4); pg.cs = new Float32Array(16);
        }
        let target = slot;
        if (!target) { target = { state: "free", pbo: gl.createBuffer(), dst: new Float32Array(MAX_POINTS * 4), sync: null, meta: null }; gl.bindBuffer(gl.PIXEL_PACK_BUFFER, target.pbo); gl.bufferData(gl.PIXEL_PACK_BUFFER, MAX_POINTS * RECORD_BYTES, gl.STREAM_READ); pg.slots.push(target); }
        const a = hostProbe.arr;
        pg.pts.fill(0); pg.cs.fill(0);
        for (let i = 0; i < n; i++) { pg.pts[i * 4] = a[HP.PTS + 2 * i]; pg.pts[i * 4 + 1] = a[HP.PTS + 2 * i + 1]; }
        cascades.forEach(function (c: any, i: number) { pg.cs[i * 4] = c.tile; pg.cs[i * 4 + 1] = 1; pg.cs[i * 4 + 2] = wrapped["waterModX" + i] || 0; pg.cs[i * 4 + 3] = wrapped["waterModZ" + i] || 0; });
        gl.bindFramebuffer(gl.FRAMEBUFFER, pg.fbo); gl.viewport(0, 0, n, 1);
        gl.disable(gl.BLEND); gl.disable(gl.DEPTH_TEST); gl.disable(gl.CULL_FACE); gl.disable(gl.SCISSOR_TEST); gl.colorMask(true, true, true, true);
        gl.useProgram(pg.program); gl.uniform4fv(pg.loc.pts, pg.pts); gl.uniform4fv(pg.loc.cs, pg.cs); gl.uniform1f(pg.loc.level, level);
        for (let i = 0; i < 4; i++) {
          const d = runner.export("disp" + i), sl = runner.export("slope" + i);
          gl.activeTexture(gl.TEXTURE0 + SURFACE_GL.UNIT.disp + i); gl.bindTexture(gl.TEXTURE_2D, d ? d.tex : null);
          gl.activeTexture(gl.TEXTURE0 + SURFACE_GL.UNIT.slope + i); gl.bindTexture(gl.TEXTURE_2D, sl ? sl.tex : null);
        }
        gl.drawArrays(gl.TRIANGLES, 0, 3);
        gl.bindBuffer(gl.PIXEL_PACK_BUFFER, target.pbo); gl.readPixels(0, 0, n, 1, gl.RGBA, gl.FLOAT, 0); gl.bindBuffer(gl.PIXEL_PACK_BUFFER, null);
        target.sync = gl.fenceSync(gl.SYNC_GPU_COMMANDS_COMPLETE, 0); gl.flush();
        target.state = "fenced"; target.meta = { n: n };
        issued(target.meta);
      } catch (error) { hostProbe.failed = String(error && error.message || error); } finally { G.glState.restore(gl, saved); }
    }
    // bind attaches the cascade textures to units 4 to 12 and uploads the surface uniform; call it with the ocean program in use.
    function bind(g: any, program: any): void {
      let loc = cache.get(program);
      if (!loc) {
        const u = function (name: string) { return g.getUniformLocation(program, name); };
        loc = { sp: u("u_sp[0]"), disp: [0, 1, 2, 3].map(function (i) { return u("u_spDisp" + i); }), slope: [0, 1, 2, 3].map(function (i) { return u("u_spSlope" + i); }), foam: u("u_spFoam"), wake: u("u_spWake") };
        cache.set(program, loc);
      }
      const unit = function (n: number, location: any, exported: any) { g.activeTexture(g.TEXTURE0 + n); g.bindTexture(g.TEXTURE_2D, exported ? exported.tex : engine.dummy); g.uniform1i(location, n); };
      if (!engine.dummy) { engine.dummy = g.createTexture(); g.bindTexture(g.TEXTURE_2D, engine.dummy); g.texStorage2D(g.TEXTURE_2D, 1, g.RGBA16F, 1, 1); g.texParameteri(g.TEXTURE_2D, g.TEXTURE_MIN_FILTER, g.LINEAR); g.texParameteri(g.TEXTURE_2D, g.TEXTURE_MAG_FILTER, g.LINEAR); }
      for (let i = 0; i < CASCADES; i++) {
        unit(SURFACE_GL.UNIT.disp + i, loc.disp[i], runner.export("disp" + i));
        unit(SURFACE_GL.UNIT.slope + i, loc.slope[i], runner.export("slope" + i));
      }
      unit(SURFACE_GL.UNIT.foam, loc.foam, runner.export("foam"));
      unit(SURFACE_GL.UNIT.wake, loc.wake, runner.export("wake"));
      g.uniform4fv(loc.sp, data);
    }
    function dispose(): void {
      if (engine.disposed) return;
      engine.disposed = true; lastWater = null; resetProbe();
      for (const slot of pg.slots) { if (slot.sync) gl.deleteSync(slot.sync); gl.deleteBuffer(slot.pbo); }
      if (pg.program) { gl.deleteProgram(pg.program); gl.deleteFramebuffer(pg.fbo); gl.deleteTexture(pg.tex); }
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
    if (gl.isContextLost && gl.isContextLost()) { lastWater = null; return null; }
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

  window.__gosx_scene3d_ocean_fft_api = { frame: frame, water: water, fill: fill, timing: timing, nodePose: nodePose, waterRelative: waterRelative, sampler: sampler, probe: { begin: probeBegin, sample: probeSample }, probeState: probe, describe: describe, WAKE_DEBUG_RANGE: WAKE_DEBUG_RANGE, feed: feed, surfaceData: surfaceData };
  // The WebGL chunk's minifier renames some property names (frame among them); the method the two chunks share is called run.
  window.__gosx_scene3d_ocean_fft_gl_api = { run: frameGL, fail: failGL };
})();
