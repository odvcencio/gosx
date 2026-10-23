// WebGPU executor of the Elio graph runner (see elio-graph.ts).
//
// Each kernel pass is one compute pass in the frame's command encoder (the renderer calls runner.run before the main
// pass). A graph buffer is a storage buffer of `count` elements; a graph texture is a storage texture whose mip
// level a binding names. Pipelines come from host.create (wgpuCreateValidatedPipeline) labelled
// gosx-elio-<graph>-<kernel>: nothing is encoded until every pipeline has validated, and a rejected pipeline
// disables the graph through the host's pass guard (the pipeline-recovery module maps the label to the optional
// pass "elioGraph"), never the renderer. Uniform blocks follow the WGSL layout in the kernel's metadata, one buffer
// per pass so queued writes cannot overwrite each other.
(function () {
  const ELEMENT_BYTES: any = { "f32": 4, "vec2<f32>": 8, "vec4<f32>": 16 };

  function make(ctx: any): any {
    const device = ctx.host.device, graph = ctx.graph, K = ctx.module.kernels, host = ctx.host;
    if (!device) throw Object.assign(new Error("no device"), { fallback: "no-device" });
    const prefix = "gosx-elio-" + ctx.label + "-";
    const buffers: any[] = [], textures: any[] = [];
    const resources: any = Object.create(null), pipelines: any = Object.create(null), layouts: any = Object.create(null);
    const groups = new Map(), views = new Map(); // views: one texture view per texture, so a surface can compare them by identity
    const state = { disposed: false };
    let seq = 0;
    const exported: any = {};
    for (const k of Object.keys(graph.exports || {})) exported[graph.exports[k]] = true;
    const queue = device.queue;
    const STORAGE = GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST | GPUBufferUsage.COPY_SRC, UNIFORM = GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST;

    for (const r of graph.resources) {
      if (r.kind === "buffer") {
        const size = Math.max(16, Math.ceil(r.count * (ELEMENT_BYTES[r.elementType] || 16) / 4) * 4);
        const buffer = device.createBuffer({ label: prefix + r.name, size: size, usage: STORAGE });
        buffers.push(buffer);
        resources[r.name] = { name: r.name, id: ++seq, buffer: buffer };
      } else {
        const texture = device.createTexture({ label: prefix + r.name, size: [r.width, r.height, 1], format: r.format, mipLevelCount: r.mipLevels || 1,
          usage: GPUTextureUsage.STORAGE_BINDING | GPUTextureUsage.TEXTURE_BINDING });
        textures.push(texture);
        resources[r.name] = { name: r.name, id: ++seq, texture: texture, w: r.width, h: r.height, levels: r.mipLevels || 1, format: r.format };
      }
    }

    function layoutEntries(bindings: any[]): any[] {
      return bindings.map(function (b: any) {
        const e: any = { binding: b.binding, visibility: GPUShaderStage.COMPUTE };
        if (b.kind === "uniform") e.buffer = { type: "uniform" };
        else if (b.kind === "storage-read") e.buffer = { type: "read-only-storage" };
        else if (b.kind === "storage-rw") e.buffer = { type: "storage" };
        else if (b.kind === "sampled-texture" || b.kind === "texture") e.texture = { sampleType: "unfilterable-float", viewDimension: "2d" }; // textureLoad only, so any float format binds
        else e.storageTexture = { access: "write-only", format: b.format, viewDimension: "2d" };
        return e;
      });
    }
    // One pipeline per (kernel, entry point); a kernel's entry points share its bind group layout.
    for (const step of ctx.steps) {
      const pass = step.pass;
      if (!pass) continue;
      if (pass.type !== "kernel") throw new Error("elio-graph: pass " + pass.name + ": " + pass.type + " passes exist only on webgl2");
      const key = pass.kernel + "/" + pass.entry;
      if (pipelines[key]) continue;
      const kernel = K[ctx.kernelKey(pass.kernel)];
      const bgl = layouts[pass.kernel] || (layouts[pass.kernel] = device.createBindGroupLayout({ label: prefix + pass.kernel, entries: layoutEntries(kernel.bindings) }));
      const module = device.createShaderModule({ label: prefix + pass.kernel, code: kernel.wgsl });
      const handle = host.create(device, "compute", { label: prefix + pass.kernel, layout: device.createPipelineLayout({ bindGroupLayouts: [bgl] }),
        compute: { module: module, entryPoint: pass.entry } });
      pipelines[key] = { handle: handle, layout: bgl, kernel: kernel };
    }
    function validated(p: any): any {
      const v = host.validation && host.validation.get(p.handle);
      return !v || v.ready === true ? (v && v.pipeline) || p.handle : null;
    }

    let planSeq = 0;
    const plans = ctx.steps.map(function (step: any) {
      const pass = step.pass;
      const p = pipelines[pass.kernel + "/" + pass.entry];
      const blocks = (pass.uniforms || []).map(function (u: any) {
        const lay = p.kernel.uniforms.find(function (x: any) { return x.name === u.name; });
        if (!lay) throw new Error("elio-graph: pass " + pass.name + " has no uniform block " + u.name);
        const size = Math.max(16, Math.ceil(lay.size / 16) * 16);
        const buffer = device.createBuffer({ label: prefix + pass.name + "-" + u.name, size: size, usage: UNIFORM });
        buffers.push(buffer);
        return { name: u.name, layout: lay, values: u.values, buffer: buffer, constant: ctx.isStatic(u.values), written: false };
      });
      return { pass: pass, p: p, blocks: blocks, id: ++planSeq };
    });

    // bindGroup is memoised on the identity of the bound resources, so a frameSwaps flip builds each variant once.
    function bindGroup(plan: any): any {
      const pass = plan.pass, entries: any[] = [], idParts: any[] = [pass.name, plan.id];
      for (const b of plan.p.kernel.bindings) {
        let resource: any;
        if (b.kind === "uniform") resource = { buffer: plan.blocks.find(function (x: any) { return x.name === b.name; }).buffer };
        else {
          const bound = pass.bindings.find(function (x: any) { return x.name === b.name; });
          if (!bound) throw new Error("elio-graph: pass " + pass.name + " does not bind " + b.name);
          const r = resources[bound.resource];
          idParts.push(r.id + "@" + (bound.level || 0));
          if (b.kind === "storage-read" || b.kind === "storage-rw") resource = { buffer: r.buffer };
          else resource = r.texture.createView({ baseMipLevel: bound.level || 0, mipLevelCount: 1 });
        }
        entries.push({ binding: b.binding, resource: resource });
      }
      const key = idParts.join("|");
      let g = groups.get(key);
      if (!g) { g = device.createBindGroup({ label: prefix + pass.name, layout: plan.p.layout, entries: entries }); groups.set(key, g); }
      return g;
    }

    function exchange(pairs: any[]): void { for (const pair of pairs) { const a = resources[pair[0]]; resources[pair[0]] = resources[pair[1]]; resources[pair[1]] = a; } }


    // GPU time of the graph: one query set stamps the start of the first compute pass that runs and the end of the last, the two stamps are
    // resolved into a buffer, copied into a ring of three mappable buffers, and read a frame or two later (a result is never awaited).
    // It exists when the device offers timestamp-query and the host does not opt out with host.timing = false.
    const RING = 3, TIME_SAMPLES = 120;
    let timing: any = null;
    if (host.timing !== false && device.features && device.features.has && device.features.has("timestamp-query")) {
      try {
        const slots: any[] = [];
        const querySet = device.createQuerySet({ label: prefix + "time", type: "timestamp", count: 2 });
        const resolve = device.createBuffer({ label: prefix + "time-resolve", size: 16, usage: GPUBufferUsage.QUERY_RESOLVE | GPUBufferUsage.COPY_SRC });
        buffers.push(resolve);
        for (let i = 0; i < RING; i++) { const b = device.createBuffer({ label: prefix + "time-" + i, size: 16, usage: GPUBufferUsage.MAP_READ | GPUBufferUsage.COPY_DST }); buffers.push(b); slots.push({ buffer: b, state: "free" }); }
        timing = { querySet: querySet, resolve: resolve, slots: slots, samples: [] as number[], ms: null as any };
      } catch (_e) { timing = null; }
    }
    function pollTiming(): void {
      if (!timing) return;
      for (const slot of timing.slots) {
        if (slot.state !== "copied") continue;
        slot.state = "mapping";
        slot.buffer.mapAsync(1 /* GPUMapMode.READ */).then(function () {
          if (state.disposed || slot.state !== "mapping") return;
          const t = new BigUint64Array(slot.buffer.getMappedRange().slice(0, 16));
          slot.buffer.unmap();
          slot.state = "free";
          if (t[1] > t[0]) {
            timing.ms = Number(t[1] - t[0]) / 1e6;
            timing.samples.push(timing.ms);
            if (timing.samples.length > TIME_SAMPLES) timing.samples.shift();
          }
        }, function () { slot.state = "free"; });
      }
    }
    function recordTiming(encoder: any): void {
      const slot = timing.slots.find(function (x: any) { return x.state === "free"; });
      encoder.resolveQuerySet(timing.querySet, 0, 2, timing.resolve, 0);
      if (!slot) return; // every staging buffer is still in flight: skip this frame's copy
      encoder.copyBufferToBuffer(timing.resolve, 0, slot.buffer, 0, 16);
      slot.state = "copied";
    }

    const lost = device.lost && device.lost.then ? device.lost : null;
    if (lost) lost.then(function () { dispose(); }, function () { dispose(); });

    function dispose(): void {
      if (state.disposed) return;
      state.disposed = true;
      for (const b of buffers) b.destroy();
      for (const t of textures) t.destroy();
      buffers.length = textures.length = 0;
      groups.clear(); views.clear();
      if (timing) { timing.querySet.destroy && timing.querySet.destroy(); }
    }

    return {
      isReady: function () {
        if (state.disposed) return false;
        for (const k of Object.keys(pipelines)) if (!validated(pipelines[k])) return false;
        return true;
      },
      run: function (params: any, frame: any, changed: any) {
        const encoder = frame.encoder;
        if (!encoder) throw new Error("elio-graph: a webgpu frame needs frame.encoder");
        for (const r of graph.resources) if (r.feed) queue.writeBuffer(resources[r.name].buffer, 0, params[r.feed]);
        pollTiming();
        const running = plans.filter(function (plan: any) { return ctx.holds(plan.pass.when, params, changed); });
        running.forEach(function (plan: any, index: number) {
          const pass = plan.pass;
          for (const b of plan.blocks) {
            if (b.constant && b.written) continue;
            queue.writeBuffer(b.buffer, 0, new Uint8Array(ctx.pack(b.layout, b.values, params, pass.name)));
            b.written = true;
          }
          const descriptor: any = { label: prefix + pass.name };
          if (timing && (index === 0 || index === running.length - 1)) {
            descriptor.timestampWrites = { querySet: timing.querySet };
            if (index === 0) descriptor.timestampWrites.beginningOfPassWriteIndex = 0;
            if (index === running.length - 1) descriptor.timestampWrites.endOfPassWriteIndex = 1;
          }
          const cp = encoder.beginComputePass(descriptor);
          cp.setPipeline(validated(plan.p)); cp.setBindGroup(0, bindGroup(plan));
          cp.dispatchWorkgroups(pass.dispatch[0], pass.dispatch[1] || 1, pass.dispatch[2] || 1);
          cp.end();
        });
        if (timing && running.length) recordTiming(encoder);
        exchange(graph.frameSwaps || []);
        return true;
      },
      // export names the texture a surface binds: its view, size and mip count as of now.
      export: function (name: string) {
        const r = resources[graph.exports[name]];
        if (r && r.buffer) return { buffer: r.buffer };
        if (!r) return null;
        let view = views.get(r.texture);
        if (!view) { view = r.texture.createView(); views.set(r.texture, view); }
        return { texture: r.texture, view: view, width: r.w, height: r.h, levels: r.levels };
      },
      // gpuTiming reports the graph's GPU milliseconds (null without timestamp-query): the latest sample and the last 120.
      gpuTiming: function () { return timing ? { ms: timing.ms, samples: timing.samples.slice() } : null; },
      dispose: dispose,
    };
  }

  window.__gosx_elio_graph.executors.webgpu = make;
})();
