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
    const groups = new Map();
    const state = { disposed: false };
    let seq = 0;
    const exported: any = {};
    for (const k of Object.keys(graph.exports || {})) exported[graph.exports[k]] = true;
    const queue = device.queue;
    const STORAGE = GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST, UNIFORM = GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST;

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
        else if (b.kind === "sampled-texture") e.texture = { sampleType: /32float$/.test(b.format || "") ? "unfilterable-float" : "float", viewDimension: "2d" };
        else e.storageTexture = { access: "write-only", format: b.format, viewDimension: "2d" };
        return e;
      });
    }
    // One pipeline per (kernel, entry point); a kernel's entry points share its bind group layout.
    for (const pass of graph.passes) {
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

    const plans = graph.passes.map(function (pass: any) {
      const p = pipelines[pass.kernel + "/" + pass.entry];
      const blocks = (pass.uniforms || []).map(function (u: any) {
        const lay = p.kernel.uniforms.find(function (x: any) { return x.name === u.name; });
        if (!lay) throw new Error("elio-graph: pass " + pass.name + " has no uniform block " + u.name);
        const size = Math.max(16, Math.ceil(lay.size / 16) * 16);
        const buffer = device.createBuffer({ label: prefix + pass.name + "-" + u.name, size: size, usage: UNIFORM });
        buffers.push(buffer);
        return { name: u.name, layout: lay, values: u.values, buffer: buffer, constant: ctx.isStatic(u.values), written: false };
      });
      return { pass: pass, p: p, blocks: blocks };
    });

    // bindGroup is memoised on the identity of the bound resources, so a frameSwaps flip builds each variant once.
    function bindGroup(plan: any): any {
      const pass = plan.pass, entries: any[] = [], idParts: any[] = [pass.name];
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

    const lost = device.lost && device.lost.then ? device.lost : null;
    if (lost) lost.then(function () { dispose(); }, function () { dispose(); });

    function dispose(): void {
      if (state.disposed) return;
      state.disposed = true;
      for (const b of buffers) b.destroy();
      for (const t of textures) t.destroy();
      buffers.length = textures.length = 0;
      groups.clear();
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
        for (const plan of plans) {
          const pass = plan.pass;
          if (!ctx.holds(pass.when, params, changed)) continue;
          for (const b of plan.blocks) {
            if (b.constant && b.written) continue;
            queue.writeBuffer(b.buffer, 0, new Uint8Array(ctx.pack(b.layout, b.values, params, pass.name)));
            b.written = true;
          }
          const cp = encoder.beginComputePass({ label: prefix + pass.name });
          cp.setPipeline(validated(plan.p)); cp.setBindGroup(0, bindGroup(plan));
          cp.dispatchWorkgroups(pass.dispatch[0], pass.dispatch[1] || 1, pass.dispatch[2] || 1);
          cp.end();
        }
        for (const pair of graph.frameSwaps || []) { const a = resources[pair[0]]; resources[pair[0]] = resources[pair[1]]; resources[pair[1]] = a; }
        return true;
      },
      // export names the texture a surface binds: its view, size and mip count as of now.
      export: function (name: string) {
        const r = resources[graph.exports[name]];
        return r ? { texture: r.texture, view: r.texture.createView(), width: r.w, height: r.h, levels: r.levels } : null;
      },
      dispose: dispose,
    };
  }

  window.__gosx_elio_graph.executors.webgpu = make;
})();
