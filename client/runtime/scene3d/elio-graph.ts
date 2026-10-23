// Generic Elio pass-graph runner (lazy chunk bootstrap-feature-scene3d-ocean-fft.js).
//
// Elio emits, per backend and quality tier, a pass graph next to its kernels (NAME.graphs["webgl2/high"], ...):
// resources with concrete sizes and formats, passes in order with kernel, bindings, dispatch or target size,
// uniform values as constants or named parameters, ping-pong pairs and the textures exported to a surface.
// This file is the one runner for every such graph. An effect (the ocean adapter) supplies only the graph's
// named parameter values each frame; it holds no pass logic.
//
// The two executors (elio-graph-gather.ts, elio-graph-compute.ts) register themselves in
// window.__gosx_elio_graph.executors. A graph that cannot be built or run is disabled, never thrown:
// runner.failed says why and the caller keeps whatever it draws without the graph.
//
//   const runner = window.__gosx_elio_graph.create({ module: kernelSet, backend: "webgl2", tier: "high", host: { gl, caps, textureCache } });
//   runner.setValue("windSpeed", 12);                       // a scene update changed a "value" parameter
//   if (runner.run({ time: 12.5, dt: 0.016, cameraX: 0, cameraY: 30, cameraZ: 0 })) surface.bind(runner.export("disp0"));
//
// Parameters come from the graph, not from effect code. Each declares its source: "value" (set from the scene, with an
// optional tween in presentation seconds), "frame" (time, dt, cameraX/Y/Z), "expr" (add, sub, mul, div, mod, floor, min,
// max over earlier parameters or numbers) or "accumulate" (integrates another parameter by dt). They are evaluated in
// declaration order every frame. A pass runs when its "when" holds: "always", "param:<p>" or "changed:<p1>,<p2>" (the
// first frame, and any frame where a listed parameter changed, including during a tween).
//
// host (webgl2): gl, caps(gl) -> { colorBufferFloat }, textureCache (its _sceneTextureEpoch is bumped), firstUnit (default 4).
// host (webgpu): device, create(device, "compute", descriptor) -> validated pipeline handle, validation map, guard.
(function () {
  // "ocean_evolve" -> "oceanEvolve", "fft_gather_64" -> "fftGather64": the module keys kernels in camel case.
  function kernelKey(name: string): string {
    return name.replace(/_([a-z0-9])/g, function (_m: string, c: string) { return c.toUpperCase(); });
  }

  function finiteNumber(v: any): number {
    if (typeof v === "boolean") return v ? 1 : 0;
    return Number(v);
  }
  // value resolves a uniform field value: a number, or "param:<name>" read from this frame's parameters.
  function value(v: any, params: any, where: string): number {
    if (typeof v === "string") {
      if (v.indexOf("param:") !== 0) throw new Error("elio-graph: " + where + ": bad value " + v);
      const name = v.slice(6);
      if (!(name in params)) throw new Error("elio-graph: parameter " + name + " was not supplied");
      return finiteNumber(params[name]);
    }
    return finiteNumber(v);
  }

  // pack writes one uniform block as the kernel's layout says (WGSL offsets on webgpu, std140 on webgl2). A missing
  // field or a field the layout does not have throws; padding fields default to zero.
  function pack(layout: any, values: any, params: any, where: string): ArrayBuffer {
    const out = new ArrayBuffer(Math.max(16, Math.ceil(layout.size / 16) * 16)), view = new DataView(out);
    const seen: any = {};
    for (const f of layout.fields) {
      let v = values[f.name];
      if (v === undefined) {
        if (!/^_pad/.test(f.name)) throw new Error("elio-graph: " + where + ": uniform " + layout.struct + " lacks a value for " + f.name);
        v = 0;
      }
      seen[f.name] = true;
      const n = value(v, params, where);
      if (f.type === "u32") view.setUint32(f.offset, n >>> 0, true);
      else if (f.type === "i32") view.setInt32(f.offset, n | 0, true);
      else if (f.type === "f32") view.setFloat32(f.offset, n, true);
      else throw new Error("elio-graph: " + where + ": unsupported uniform type " + f.type);
    }
    for (const k of Object.keys(values)) if (!seen[k]) throw new Error("elio-graph: " + where + ": uniform " + layout.struct + " has no field " + k);
    return out;
  }
  // isStatic: a uniform whose values hold no parameter reference is written once.
  function isStatic(values: any): boolean {
    for (const k of Object.keys(values)) if (typeof values[k] === "string") return false;
    return true;
  }


  const NODE_FIELDS: any = { x: 1, z: 1, heading: 1, speed: 1 };
  const FRAME_SOURCES: any = { time: 1, dt: 1, cameraX: 1, cameraY: 1, cameraZ: 1 };
  const OPS: any = {
    add: function (a: number, b: number) { return a + b; }, sub: function (a: number, b: number) { return a - b; },
    mul: function (a: number, b: number) { return a * b; }, div: function (a: number, b: number) { return b === 0 ? 0 : a / b; },
    mod: function (a: number, b: number) { return b === 0 ? 0 : a - Math.floor(a / b) * b; }, // GLSL/WGSL mod: the sign follows b
    floor: function (a: number) { return Math.floor(a); }, sin: function (a: number) { return Math.sin(a); }, cos: function (a: number) { return Math.cos(a); }, // radians
    min: function (a: number, b: number) { return Math.min(a, b); }, max: function (a: number, b: number) { return Math.max(a, b); },
  };
  // f32 parameters stay in float64 here and round when a uniform is packed, so chained expressions (a wrapped water offset)
  // lose nothing; integers and bools are coerced as their GPU types would be.
  function coerce(type: string, n: any): any {
    if (/\[\]$/.test(type)) return Float32Array.from(n || []);
    n = Number(n);
    if (!Number.isFinite(n)) return 0;
    if (type === "bool") return n ? 1 : 0;
    if (type === "u32") return Math.max(0, Math.floor(n)) >>> 0;
    if (type === "i32") return Math.trunc(n) | 0;
    return n;
  }
  // params evaluates a graph's parameters. State (tweens, accumulators, the previous frame's values) lives here.
  function createParams(decls: any[]): any {
    const current: any = {}, prev: any = {}, tweens: any = {}, accum: any = {}, targets: any = {};
    let first = true, tweenOverride: number | null = null, nodeMap: any = {};
    const lastPose: any = {}, held: any = {}; // per node id: the pose of the previous frame; per parameter: the last value read
    const tweenOf = function (d: any): number { return d.tween > 0 ? (tweenOverride === null ? d.tween : tweenOverride) : 0; };
    for (const d of decls) {
      const src = d.source || { kind: "value", value: 0 };
      if (src.kind === "value") targets[d.name] = coerce(d.type, /\[\]$/.test(d.type) ? src.value : (src.value || 0));
    }
    // ref evaluates an expression argument: a number, "param:<name>", or a nested { op, args } evaluated depth first in float64.
    const ref = function (a: any, scope?: any): number {
      const at = scope || current;
      if (typeof a === "number") return a;
      if (typeof a === "string" && a.indexOf("param:") === 0) { const n = a.slice(6); return n in at ? at[n] : 0; }
      if (a && typeof a === "object" && typeof a.op === "string") {
        const op = OPS[a.op];
        if (!op) throw new Error("elio-graph: unknown op " + a.op);
        return op(ref(a.args[0], at), a.args.length > 1 ? ref(a.args[1], at) : 0);
      }
      throw new Error("elio-graph: bad argument " + a);
    };
    const api: any = {
      current: current, changed: {},
      // setNodes maps the graph's logical node names to scene node ids (the scene's graphNodes); an unmapped name is taken as an id.
      setNodes: function (map: any) { nodeMap = map || {}; },
      // setTween is the scene's graph-level override: every parameter the graph tweens uses these seconds (0 means no tween).
      setTween: function (seconds: number) { tweenOverride = Number.isFinite(seconds) && seconds >= 0 ? seconds : null; },
      // set records a scene update of a "value" parameter; its tween (if any) starts on the next frame.
      set: function (name: string, v: number) {
        const d = decls.find(function (x: any) { return x.name === name; });
        if (!d || (d.source && d.source.kind !== "value")) throw new Error("elio-graph: " + name + " is not a value parameter");
        const n = coerce(d.type, v);
        if (n === targets[name]) return;
        if (typeof n !== "number") { targets[name] = n; return; } // an array parameter: a new array object marks it changed
        targets[name] = n;
        if (tweenOf(d) > 0 && !first) tweens[name] = { from: current[name], start: null };
        else delete tweens[name];
      },
      // snapshot copies this frame's values, the baseline a pass group that runs at a lower rate derives its deltas from.
      snapshot: function () { return Object.assign({}, current); },
      // derive returns this frame's values as a pass group that last ran at "since" sees them: dt is the time since its last run,
      // expressions follow dt, and deltas measure movement since that run (none without a snapshot). Stateful sources are kept.
      derive: function (dt: number, since: any) {
        const out: any = Object.assign({}, current);
        for (const d of decls) {
          const src = d.source || { kind: "value", value: 0 };
          if (src.kind === "frame" && src.name === "dt") out[d.name] = coerce(d.type, dt);
          else if (src.kind === "expr") out[d.name] = coerce(d.type, OPS[src.op](ref(src.args[0], out), src.args.length > 1 ? ref(src.args[1], out) : 0));
          else if (src.kind === "delta") {
            const of = String(src.of).replace(/^param:/, "");
            out[d.name] = coerce(d.type, since ? (out[of] || 0) - (since[of] || 0) : 0);
          }
        }
        return out;
      },
      evaluate: function (frame: any) {
        const time = Number(frame.time) || 0, dt = Number(frame.dt) || 0;
        const poses: any = {}; // this frame's pose per node id, with a derived speed
        const poseOf = function (id: string): any {
          if (id in poses) return poses[id];
          const raw = typeof frame.nodePose === "function" ? frame.nodePose(id) : null;
          if (!raw) return poses[id] = null;
          const before = lastPose[id];
          let speed = Number(raw.speed);
          if (!Number.isFinite(speed)) speed = before && dt > 0 ? Math.hypot(raw.x - before.x, raw.z - before.z) / dt : 0;
          return poses[id] = { x: Number(raw.x) || 0, z: Number(raw.z) || 0, heading: Number(raw.heading) || 0, speed: speed };
        };
        for (const d of decls) {
          const src = d.source || { kind: "value", value: 0 };
          let n = 0;
          if (src.kind === "value") {
            n = targets[d.name];
            const tw = tweens[d.name];
            if (tw) {
              if (tw.start === null) tw.start = time;
              const k = Math.min(1, Math.max(0, (time - tw.start) / tweenOf(d)));
              if (k >= 1) delete tweens[d.name]; else n = tw.from + (n - tw.from) * k;
            }
          } else if (src.kind === "frame") {
            if (!FRAME_SOURCES[src.name]) throw new Error("elio-graph: unknown frame source " + src.name);
            n = src.name === "time" ? time : src.name === "dt" ? dt : Number(frame[src.name]) || 0;
          } else if (src.kind === "expr") {
            const op = OPS[src.op];
            if (!op) throw new Error("elio-graph: unknown op " + src.op);
            n = op(ref(src.args[0]), src.args.length > 1 ? ref(src.args[1]) : 0);
          } else if (src.kind === "node") {
            if (!NODE_FIELDS[src.field]) throw new Error("elio-graph: unknown node field " + src.field);
            const pose = poseOf(src.node in nodeMap ? nodeMap[src.node] : src.node);
            // A missing node holds its last position and stands still.
            n = pose ? pose[src.field] : src.field === "speed" ? 0 : (held[d.name] || 0);
            held[d.name] = n;
          } else if (src.kind === "delta") {
            const of = String(src.of).replace(/^param:/, "");
            n = first ? 0 : (current[of] || 0) - (prev[of] || 0);
          } else if (src.kind === "accumulate") {
            accum[d.name] = (accum[d.name] || 0) + ref(src.of) * dt;
            n = accum[d.name];
          } else throw new Error("elio-graph: unknown parameter source " + src.kind);
          current[d.name] = /\[\]$/.test(d.type) ? targets[d.name] : coerce(d.type, n);
        }
        for (const id of Object.keys(poses)) if (poses[id]) lastPose[id] = { x: poses[id].x, z: poses[id].z };
        for (const d of decls) api.changed[d.name] = first || current[d.name] !== prev[d.name];
        for (const d of decls) prev[d.name] = current[d.name];
        first = false;
      },
    };
    return api;
  }
  // holds decides whether a pass runs this frame.
  function holds(when: any, params: any, changed: any): boolean {
    if (!when || when === "always") return true;
    if (when.indexOf("param:") === 0) return Boolean(params[when.slice(6)]);
    if (when.indexOf("changed:") === 0) return when.slice(8).split(",").some(function (n: string) { return Boolean(changed[n.trim()]); });
    throw new Error("elio-graph: bad when " + when);
  }



  // normalize fills in what the emitted JSON leaves out and returns a new graph (the module's graphs are frozen): a pass without a type is a
  // kernel, a kernel pass without an entry uses the kernel's primary entry, without when it runs always, a binding without a level reads level 0,
  // and missing params and exportParams are empty. A graph with paramsFrom takes the params and exportParams of the named graph of the same
  // backend, which must not itself have paramsFrom.
  function normalize(graph: any, module: any): any {
    const out: any = Object.assign({}, graph);
    if (graph.paramsFrom) {
      const donor = module && module.graphs && module.graphs[graph.paramsFrom];
      if (!donor || donor.backend !== graph.backend || donor.paramsFrom) throw new Error("elio-graph: paramsFrom " + graph.paramsFrom + " does not name a graph of " + graph.backend + " that owns its parameters");
      out.params = donor.params; out.exportParams = donor.exportParams;
    }
    delete out.paramsFrom;
    out.params = out.params || []; out.exportParams = out.exportParams || [];
    out.frameSwaps = out.frameSwaps || []; out.exports = out.exports || {};
    const pass = function (p: any): any {
      const q: any = Object.assign({}, p);
      q.type = p.type || "kernel";
      q.when = p.when || "always";
      if (q.type === "kernel") {
        const kernel = module && module.kernels && module.kernels[kernelKey(p.kernel)];
        if (!q.entry && kernel) q.entry = kernel.entry;
        if (p.bindings) q.bindings = p.bindings.map(function (b: any) { return Object.assign({ level: 0 }, b); });
        if (!q.uniforms) q.uniforms = [];
      }
      if (q.type === "repeat") q.body = (p.body || []).map(pass);
      return q;
    };
    out.passes = (graph.passes || []).map(pass);
    return out;
  }

  // indexValue evaluates an IndexExpr {index, offset, shr} for the repeat index i: i + offset, or max(shr >> (i + offset), 1) when shr is set.
  function indexValue(x: any, i: number): number {
    const v = i + (x.offset || 0);
    return x.shr ? Math.max(x.shr >> v, 1) : v;
  }
  // expand mirrors Elio's graph.Expand: every repeat pass becomes its iterations, in order, named "<repeat>[<i>].<pass>"; the index
  // parameter and IndexExprs become numbers in uniform values and binding levels; and the resources of each swap pair are exchanged
  // in the bindings that follow (swaps persist across iterations and must cancel by the end). The result holds only plain passes
  // with concrete resource names; the graph is not modified. A malformed repeat throws.
  function expand(graph: any): any[] {
    const res: any = {}, alias: any = {}, declared: any = {};
    for (const r of graph.resources) res[r.name] = r;
    for (const q of graph.params) declared[q.name] = true;
    const resolve = function (name: string): string { return name in alias ? alias[name] : name; };
    const walk = function (passes: any[], env: any, prefix: string, when: string): any[] {
      const out: any[] = [];
      for (const p of passes) {
        if (p.type !== "repeat") { out.push(plain(p, env, prefix, when)); continue; }
        const name = prefix + p.name;
        if (!(p.count >= 1)) throw new Error("elio-graph: repeat " + name + ": count " + p.count + ", want at least 1");
        if (!p.index || declared[p.index]) throw new Error("elio-graph: repeat " + name + ": index " + p.index + " is empty or names a declared param");
        if (!p.body || !p.body.length) throw new Error("elio-graph: repeat " + name + ": body is empty");
        if (p.index in env) throw new Error("elio-graph: repeat " + name + ": index " + p.index + " is already in use by an outer repeat");
        for (const sw of p.swap || []) if (!res[sw[0]] || !res[sw[1]]) throw new Error("elio-graph: repeat " + name + ": swap names an unknown resource");
        const inner = p.when && p.when !== "always" ? p.when : when;
        for (let i = 0; i < p.count; i++) {
          const env2 = Object.assign({}, env, { [p.index]: i });
          for (const q of walk(p.body, env2, name + "[" + i + "].", inner)) out.push(q);
          for (const sw of p.swap || []) { const a = resolve(sw[0]), b = resolve(sw[1]); alias[sw[0]] = b; alias[sw[1]] = a; }
        }
      }
      return out;
    };
    const plain = function (p: any, env: any, prefix: string, when: string): any {
      const q = Object.assign({}, p, { name: prefix + p.name });
      if (when && (!p.when || p.when === "always")) q.when = when;
      if (p.resource) q.resource = resolve(p.resource);
      if (p.from) q.from = resolve(p.from);
      if (p.to) q.to = resolve(p.to);
      if (p.bindings) {
        q.bindings = p.bindings.map(function (b: any) {
          const c = Object.assign({}, b, { resource: resolve(b.resource) });
          if (b.levelAt) {
            if (!(b.levelAt.index in env)) throw new Error("elio-graph: pass " + q.name + ": index " + b.levelAt.index + " is not in scope");
            c.level = indexValue(b.levelAt, env[b.levelAt.index]);
            delete c.levelAt;
          }
          return c;
        });
      }
      if (p.uniforms) {
        q.uniforms = p.uniforms.map(function (u: any) {
          const values: any = {};
          for (const k of Object.keys(u.values)) {
            let v = u.values[k];
            if (v && typeof v === "object") {
              if (!(v.index in env)) throw new Error("elio-graph: pass " + q.name + ": index " + v.index + " is not in scope");
              v = indexValue(v, env[v.index]);
            } else if (typeof v === "string" && v.indexOf("param:") === 0 && (v.slice(6) in env)) v = env[v.slice(6)];
            values[k] = v;
          }
          return Object.assign({}, u, { values: values });
        });
      }
      return q;
    };
    const out = walk(graph.passes, {}, "", "");
    for (const name of Object.keys(alias)) if (alias[name] !== name) throw new Error("elio-graph: swaps are not restored: " + name + " still refers to " + alias[name] + " at the end of the passes");
    return out;
  }

  // check rejects a graph the runner cannot execute before any GPU object exists. Elio validates deeper at emit time.
  function check(graph: any, module: any, backend: string): void {
    if (!graph || graph.version !== 1) throw new Error("elio-graph: unsupported graph version");
    if (graph.backend !== backend) throw new Error("elio-graph: graph is for " + graph.backend + ", not " + backend);
    const resources: any = {};
    for (const r of graph.resources) resources[r.name] = r;
    const params: any = {};
    for (const p of graph.params) params[p.name] = true;
    const ref = function (v: any) { if (typeof v === "string" && v.indexOf("param:") === 0 && !params[v.slice(6)]) throw new Error("elio-graph: undeclared parameter " + v.slice(6)); };
    for (const p of expand(graph)) {
      const w = p.when || "always";
      if (w.indexOf("param:") === 0) ref(w);
      else if (w.indexOf("changed:") === 0) { for (const n of w.slice(8).split(",")) if (!params[n.trim()]) throw new Error("elio-graph: undeclared parameter " + n.trim()); }
      else if (w !== "always") throw new Error("elio-graph: pass " + p.name + ": bad when " + w);
      if (p.type === "mipmap") { if (!resources[p.resource]) throw new Error("elio-graph: pass " + p.name + ": unknown resource " + p.resource); continue; }
      if (p.type === "copy") {
        if (backend !== "webgl2") throw new Error("elio-graph: pass " + p.name + ": copy passes exist only on webgl2");
        for (const n of [p.from, p.to]) if (!resources[n] || resources[n].kind !== "buffer") throw new Error("elio-graph: pass " + p.name + ": unknown resource " + n + " (a copy joins two buffers)");
        continue;
      }
      if (!module.kernels[kernelKey(p.kernel)]) throw new Error("elio-graph: pass " + p.name + ": the module has no kernel " + p.kernel);
      for (const b of p.bindings) if (!resources[b.resource]) throw new Error("elio-graph: pass " + p.name + ": unknown resource " + b.resource);
      for (const u of p.uniforms || []) for (const k of Object.keys(u.values)) ref(u.values[k]);
    }
    for (const n of graph.exportParams || []) if (!params[n]) throw new Error("elio-graph: exportParams names undeclared parameter " + n);
    for (const r of graph.resources) if (r.feed && !params[r.feed]) throw new Error("elio-graph: resource " + r.name + " is fed by undeclared parameter " + r.feed);
    for (const pair of graph.frameSwaps || []) for (const n of pair) if (!resources[n]) throw new Error("elio-graph: frameSwaps names unknown resource " + n);
    for (const k of Object.keys(graph.exports || {})) {
      const r = resources[graph.exports[k]];
      if (!r || (r.kind !== "texture" && backend !== "webgpu")) throw new Error("elio-graph: export " + k + " must name a texture resource");
    }
  }

  // create builds a runner for one graph. options: module (the generated kernel set), backend, tier, graph (optional override), host.
  function create(options: any): any {
    const backend = options.backend, module = options.module, host = options.host || {};
    const runner: any = { graph: null, backend: backend, ready: false, failed: null, disposed: false, buildCount: 0, run: run, nodePose: nodePoseOf, gpuTiming: gpuTiming, setValue: setValue, setNodes: setNodes, setTween: setTween, value: value, hasParam: hasParam, params: null, exportParams: exportParams, export: exportTexture, dispose: dispose, fail: fail };
    let executor: any = null, evaluator: any = null;
    const disable = function (error: any): void {
      if (runner.failed) return;
      runner.failed = (error && error.fallback) || "error";
      runner.error = error;
      if (executor) { try { executor.dispose(); } catch (_e) {} executor = null; }
      runner.ready = false;
      if (options.onFail) options.onFail(runner.failed, error);
      if (!(error && error.fallback) && typeof console !== "undefined" && console.warn) console.warn("elio-graph: graph disabled: " + (error && error.message ? error.message : String(error)));
    };
    try {
      let graph = options.graph || (module.graphs && module.graphs[backend + "/" + (options.tier || "high")]);
      if (!graph) throw Object.assign(new Error("elio-graph: no graph for " + backend + "/" + options.tier), { fallback: "no-graph" });
      graph = normalize(graph, module);
      check(graph, module, backend);
      const make = window.__gosx_elio_graph.executors[backend];
      if (!make) throw Object.assign(new Error("elio-graph: no executor for " + backend), { fallback: "no-executor" });
      runner.graph = graph;
      evaluator = runner.params = createParams(graph.params);
      executor = make({ steps: expand(graph).map(function (p: any) { return { pass: p }; }), graph: graph, module: module, host: host, pack: pack, isStatic: isStatic, kernelKey: kernelKey, holds: holds, label: options.label || graph.name });
      runner.buildCount = 1;
      runner.ready = executor.isReady();
    } catch (error) { disable(error); }

    function hasParam(name: string): boolean { return Boolean(runner.graph && runner.graph.params.some(function (p: any) { return p.name === name; })); }
    // nodePose reads a scene node's pose through the host (the same lookup the graph's node sources use), or null.
    function nodePoseOf(id: string): any { return host.nodePose ? host.nodePose(id) : null; }
    // gpuTiming is the graph's GPU time where the executor can measure it (WebGPU with timestamp-query), else null.
    function gpuTiming(): any { return executor && executor.gpuTiming ? executor.gpuTiming() : null; }
    function setNodes(map: any): void { if (evaluator) evaluator.setNodes(map); }
    function setTween(seconds: number): void { if (evaluator) evaluator.setTween(seconds); }
    // value reads a parameter as of the last frame (float64), for surfaces and CPU queries that must agree with the graph.
    function value(name: string): number {
      const held = executor && executor.held ? executor.held(name) : undefined;
      return held !== undefined ? held : evaluator && name in evaluator.current ? evaluator.current[name] : 0;
    }
    // setValue is how a scene update reaches a "value" parameter.
    function setValue(name: string, v: number): void {
      if (!evaluator || runner.failed) return;
      try { evaluator.set(name, v); } catch (error) { disable(error); }
    }
    // run evaluates the parameters for this frame and executes the passes. frame: time, dt, cameraX/Y/Z (parameter sources),
    // and for webgpu the command encoder. It returns false, without evaluating, while pipelines are still validating.
    function run(frame: any): boolean {
      if (!executor || runner.failed || runner.disposed) return false;
      try {
        runner.ready = executor.isReady();
        if (!runner.ready) return false;
        const f = frame && !frame.nodePose && host.nodePose ? Object.assign({}, frame, { nodePose: host.nodePose }) : (frame || {});
        evaluator.evaluate(f);
        return executor.run(evaluator.current, f, evaluator.changed, evaluator) !== false;
      } catch (error) { disable(error); return false; }
    }
    // exportParams returns the values of the graph's exportParams (float64), for the surface to bind as uniforms.
    function exportParams(): any {
      const out: any = {};
      if (evaluator && runner.graph) for (const n of runner.graph.exportParams || []) out[n] = value(n);
      return out;
    }
    function exportTexture(name: string): any { return executor && !runner.failed ? executor.export(name) : null; }
    function fail(reason: string): void { disable({ message: reason, fallback: reason }); }
    function dispose(): void {
      if (runner.disposed) return;
      runner.disposed = true; runner.ready = false;
      if (executor) { try { executor.dispose(); } catch (_e) {} executor = null; }
    }
    return runner;
  }

  window.__gosx_elio_graph = { create: create, expand: expand, normalize: normalize, pack: pack, kernelKey: kernelKey, check: check, holds: holds, createParams: createParams, executors: {} };
})();
