// WebGL2 executor of the Elio graph runner (see elio-graph.ts).
//
// WebGL2 has no compute shaders, so each kernel pass is the kernel's GLSL ES 3.00 gather form drawn as a fullscreen
// triangle into float render targets (EXT_color_buffer_float). A graph buffer is an RGBA32F texture `width` wide;
// a graph texture keeps its format (RGBA16F is filterable and renders, so surfaces can sample it with mips).
// Gather kernels index every buffer as an elio_width-wide texture, and a kernel never reads what it writes: the
// runner checks that for every pass and fails the graph if a pass breaks it.
//
// The executor saves and restores the GL state it disturbs (framebuffers, viewport, program, VAO, blend, depth,
// cull, scissor, masks, buffer bindings), keeps gather inputs on texture units firstUnit and up (default 4), and
// bumps textureCache._sceneTextureEpoch after each frame so material bindings are rebound.
(function () {
  const FULLSCREEN_VERTEX = "#version 300 es\nvoid main() { vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2)); gl_Position = vec4(p * 2. - 1., 0., 1.); }";

  function saveState(gl: any): any {
    return {
      draw: gl.getParameter(gl.DRAW_FRAMEBUFFER_BINDING), read: gl.getParameter(gl.READ_FRAMEBUFFER_BINDING), viewport: gl.getParameter(gl.VIEWPORT),
      program: gl.getParameter(gl.CURRENT_PROGRAM), vao: gl.getParameter(gl.VERTEX_ARRAY_BINDING), active: gl.getParameter(gl.ACTIVE_TEXTURE),
      blend: gl.isEnabled(gl.BLEND), depth: gl.isEnabled(gl.DEPTH_TEST), cull: gl.isEnabled(gl.CULL_FACE), scissor: gl.isEnabled(gl.SCISSOR_TEST),
      colorMask: gl.getParameter(gl.COLOR_WRITEMASK), depthMask: gl.getParameter(gl.DEPTH_WRITEMASK),
    };
  }
  function restoreState(gl: any, s: any): void {
    gl.bindBuffer(gl.UNIFORM_BUFFER, null); gl.bindBufferBase(gl.UNIFORM_BUFFER, 0, null);
    gl.bindFramebuffer(gl.DRAW_FRAMEBUFFER, s.draw); gl.bindFramebuffer(gl.READ_FRAMEBUFFER, s.read);
    gl.viewport(s.viewport[0], s.viewport[1], s.viewport[2], s.viewport[3]);
    gl.useProgram(s.program); gl.bindVertexArray(s.vao); gl.activeTexture(s.active);
    const set = function (cap: number, on: boolean): void { if (on) gl.enable(cap); else gl.disable(cap); };
    set(gl.BLEND, s.blend); set(gl.DEPTH_TEST, s.depth); set(gl.CULL_FACE, s.cull); set(gl.SCISSOR_TEST, s.scissor);
    gl.colorMask(s.colorMask[0], s.colorMask[1], s.colorMask[2], s.colorMask[3]); gl.depthMask(s.depthMask);
  }

  function make(ctx: any): any {
    const gl = ctx.host.gl, graph = ctx.graph, module = ctx.module, K = module.kernels;
    const firstUnit = ctx.host.firstUnit || 4;
    const caps = ctx.host.caps ? ctx.host.caps(gl) : {};
    if (!caps.colorBufferFloat) throw Object.assign(new Error("EXT_color_buffer_float is not available"), { fallback: "EXT_color_buffer_float" });
    const number = function (pname: number, fallback: number): number { const v = gl.getParameter(pname); return typeof v === "number" ? v : fallback; };
    if (number(gl.MAX_DRAW_BUFFERS, 0) < 2 || number(gl.MAX_TEXTURE_IMAGE_UNITS, 0) < firstUnit + 5) throw Object.assign(new Error("too few texture units or draw buffers"), { fallback: "texture-units" });
    for (const step of ctx.steps) {
      const p = step.pass;
      if (!p || p.type !== "kernel") continue;
      const k = K[ctx.kernelKey(p.kernel)];
      if (!k.glsl || !k.gatherLayout) throw Object.assign(new Error("kernel " + p.kernel + " has no GLSL gather form"), { fallback: "kernel:" + p.kernel });
    }
    const textures: any[] = [], fbos: any[] = [], buffers: any[] = [], shaders: any[] = [], programs: any = Object.create(null);
    const exported: any = {};
    for (const k of Object.keys(graph.exports || {})) exported[graph.exports[k]] = true;
    const vao = gl.createVertexArray();
    const state: any = { split: false, disposed: false };
    let vertexShader: any = null;
    const resources: any = Object.create(null); // name -> resource object; frameSwaps exchange these
    const fboCache = new Map();
    let seq = 0;

    const FORMATS: any = { rgba16float: gl.RGBA16F, rgba32float: gl.RGBA32F, r32float: gl.R32F };
    function texture(w: number, h: number, internal: number, levels: number, filtered: boolean): any {
      const t = gl.createTexture();
      textures.push(t);
      gl.activeTexture(gl.TEXTURE0 + firstUnit); gl.bindTexture(gl.TEXTURE_2D, t);
      gl.texStorage2D(gl.TEXTURE_2D, levels, internal, w, h);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, filtered ? (levels > 1 ? gl.LINEAR_MIPMAP_LINEAR : gl.LINEAR) : gl.NEAREST);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, filtered ? gl.LINEAR : gl.NEAREST);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, filtered ? gl.REPEAT : gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, filtered ? gl.REPEAT : gl.CLAMP_TO_EDGE);
      return t;
    }
    function compile(type: number, source: string): any {
      const shader = gl.createShader(type);
      shaders.push(shader);
      gl.shaderSource(shader, source); gl.compileShader(shader);
      if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) throw new Error("elio-graph: shader did not compile: " + gl.getShaderInfoLog(shader));
      return shader;
    }
    // program links one gather kernel with the fullscreen vertex shader and wires its samplers and uniform blocks.
    function program(kernelName: string): any {
      if (programs[kernelName]) return programs[kernelName];
      const kernel = K[ctx.kernelKey(kernelName)];
      if (!vertexShader) vertexShader = compile(gl.VERTEX_SHADER, FULLSCREEN_VERTEX);
      const fragment = compile(gl.FRAGMENT_SHADER, kernel.glsl);
      const handle = gl.createProgram();
      gl.attachShader(handle, vertexShader); gl.attachShader(handle, fragment); gl.linkProgram(handle);
      const layout = kernel.gatherLayout;
      const p: any = programs[kernelName] = { handle: handle, layout: layout, kernel: kernel };
      if (!gl.getProgramParameter(handle, gl.LINK_STATUS)) throw new Error("elio-graph: program " + kernelName + " did not link: " + gl.getProgramInfoLog(handle));
      gl.useProgram(handle);
      p.width = gl.getUniformLocation(handle, "elio_width");
      p.lens = layout.inputs.map(function (input: any) { return gl.getUniformLocation(handle, input.length); });
      layout.inputs.forEach(function (input: any, i: number) { gl.uniform1i(gl.getUniformLocation(handle, input.sampler), firstUnit + i); });
      layout.uniforms.forEach(function (u: any, i: number) { gl.uniformBlockBinding(handle, gl.getUniformBlockIndex(handle, u.block), i); });
      return p;
    }
    function uniformLayout(kernel: any, name: string): any {
      const list = kernel.uniformsStd140 || kernel.uniforms;
      const u = list.find(function (x: any) { return x.name === name; });
      if (!u) throw new Error("elio-graph: kernel has no uniform block " + name);
      return u;
    }

    // Resources: a buffer is an RGBA32F texture `width` wide; a texture keeps its format.
    function build(): void {
      for (const r of graph.resources) {
        let res: any;
        if (r.kind === "buffer") {
          const h = Math.ceil(r.count / r.width);
          res = { name: r.name, id: ++seq, w: r.width, h: h, levels: 1, tex: texture(r.width, h, gl.RGBA32F, 1, false) };
        } else {
          const levels = r.mipLevels || 1, internal = FORMATS[r.format];
          if (!internal) throw new Error("elio-graph: unsupported format " + r.format);
          // Only 16-bit formats filter without an extension; surfaces sample exported and mipmapped ones.
          const filtered = r.format === "rgba16float" && (levels > 1 || Boolean(exported[r.name]));
          res = { name: r.name, id: ++seq, w: r.width, h: r.height, levels: levels, tex: texture(r.width, r.height, internal, levels, filtered) };
        }
        resources[r.name] = res;
      }
    }
    // framebuffer attaches each output at the colour attachment equal to its kernel output location.
    function framebuffer(list: any[]): any {
      const f = gl.createFramebuffer();
      fbos.push(f);
      gl.bindFramebuffer(gl.FRAMEBUFFER, f);
      const draw: number[] = [];
      for (const o of list) {
        gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0 + o.location, gl.TEXTURE_2D, o.res.tex, o.level);
        while (draw.length <= o.location) draw.push(gl.NONE);
        draw[o.location] = gl.COLOR_ATTACHMENT0 + o.location;
      }
      gl.drawBuffers(draw);
      return { fbo: f, complete: gl.checkFramebufferStatus(gl.FRAMEBUFFER) === gl.FRAMEBUFFER_COMPLETE };
    }
    // targetFor returns the framebuffers holding a pass's outputs. If the driver rejects a multi-target combination,
    // completeness is reported once and every output gets its own framebuffer: the kernel then runs once per output.
    function targetFor(list: any[]): any[] {
      const key = list.map(function (o: any) { return o.res.id + "@" + o.level + ":" + o.location; }).join(",");
      const hit = fboCache.get(key + (state.split ? "s" : ""));
      if (hit) return hit;
      let out: any[];
      const single = function (o: any) { const f = framebuffer([o]); if (!f.complete) throw new Error("elio-graph: framebuffer incomplete"); return f; };
      if (list.length === 1) out = [single(list[0])];
      else {
        const f = state.split ? null : framebuffer(list);
        if (f && f.complete) out = [f];
        else { state.split = true; out = list.map(single); }
      }
      fboCache.set(key + (state.split ? "s" : ""), out);
      return out;
    }

    function teardown(): void {
      if (state.disposed) return;
      state.disposed = true;
      for (const t of textures) gl.deleteTexture(t);
      for (const f of fbos) gl.deleteFramebuffer(f);
      for (const b of buffers) gl.deleteBuffer(b);
      for (const k of Object.keys(programs)) gl.deleteProgram(programs[k].handle);
      for (const sh of shaders) gl.deleteShader(sh);
      gl.deleteVertexArray(vao);
      textures.length = fbos.length = buffers.length = 0;
      fboCache.clear();
    }

    // Per-pass plan: resolved once; resource objects are looked up by name each frame because swaps exchange them.
    let plans: any[] = [];
    const saved = saveState(gl);
    try {
    plans = ctx.steps.map(function (step: any) {
      if (step.swap) return { swap: step.swap };
      const pass = step.pass;
      if (pass.type === "mipmap") return { pass: pass };
      const p = program(pass.kernel), layout = p.layout;
      const inputs = layout.inputs.map(function (input: any) {
        const b = pass.bindings.find(function (x: any) { return x.name === input.name; });
        if (!b) throw new Error("elio-graph: pass " + pass.name + " does not bind input " + input.name);
        return b;
      });
      const outputs = layout.outputs.map(function (out: any) {
        const b = pass.bindings.find(function (x: any) { return x.name === out.name; });
        if (!b) throw new Error("elio-graph: pass " + pass.name + " does not bind output " + out.name);
        return { binding: b, location: out.location };
      });
      const blocks = (pass.uniforms || []).map(function (u: any) {
        const index = layout.uniforms.findIndex(function (x: any) { return x.name === u.name; });
        if (index < 0) throw new Error("elio-graph: pass " + pass.name + " has no uniform block " + u.name);
        const lay = uniformLayout(p.kernel, u.name), buf = gl.createBuffer(), size = Math.max(16, Math.ceil(lay.size / 16) * 16);
        buffers.push(buf);
        gl.bindBuffer(gl.UNIFORM_BUFFER, buf); gl.bufferData(gl.UNIFORM_BUFFER, size, gl.DYNAMIC_DRAW);
        const block: any = { index: index, layout: lay, values: u.values, buffer: buf, size: size, written: false, constant: ctx.isStatic(u.values) };
        return block;
      });
      return { pass: pass, p: p, inputs: inputs, outputs: outputs, blocks: blocks };
    });

    build();
    } catch (error) { teardown(); throw error; } finally { restoreState(gl, saved); }

    function exchange(pairs: any[]): void { for (const pair of pairs) { const a = resources[pair[0]]; resources[pair[0]] = resources[pair[1]]; resources[pair[1]] = a; } }
    // feed uploads each fed parameter (a buffer parameter, such as probe points) into its resource.
    function feed(params: any): void {
      for (const r of graph.resources) {
        if (!r.feed) continue;
        const res = resources[r.name], data = new Float32Array(res.w * res.h * 4);
        data.set(params[r.feed].subarray(0, data.length));
        gl.activeTexture(gl.TEXTURE0 + firstUnit); gl.bindTexture(gl.TEXTURE_2D, res.tex);
        gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, 0, res.w, res.h, gl.RGBA, gl.FLOAT, data);
      }
    }
    function draw(plan: any, params: any): void {
      const pass = plan.pass, p = plan.p, t = pass.target;
      const outs = plan.outputs.map(function (o: any) { return { res: resources[o.binding.resource], level: o.binding.level || 0, location: o.location }; });
      const ins = plan.inputs.map(function (b: any) { return resources[b.resource]; });
      // A pass may read level L-1 of a texture and write level L (mip passes); the same image is a feedback loop WebGL2 rejects.
      plan.inputs.forEach(function (b: any, k: number) {
        for (const o of outs) if (o.res === ins[k] && o.level === (b.level || 0)) throw new Error("elio-graph: pass " + pass.name + " reads " + o.res.name + ", which it writes");
      });
      gl.useProgram(p.handle);
      gl.uniform1ui(p.width, t.elioWidth);
      ins.forEach(function (r: any, i: number) {
        gl.activeTexture(gl.TEXTURE0 + firstUnit + i); gl.bindTexture(gl.TEXTURE_2D, r.tex);
        if (p.lens[i]) gl.uniform1ui(p.lens[i], r.w * r.h);
      });
      for (const b of plan.blocks) {
        if (!b.constant || !b.written) {
          gl.bindBuffer(gl.UNIFORM_BUFFER, b.buffer);
          gl.bufferSubData(gl.UNIFORM_BUFFER, 0, new Uint8Array(ctx.pack(b.layout, b.values, params, pass.name)));
          b.written = true;
        }
        gl.bindBufferRange(gl.UNIFORM_BUFFER, b.index, b.buffer, 0, b.size);
      }
      gl.viewport(0, 0, t.width, t.height);
      for (const f of targetFor(outs)) { gl.bindFramebuffer(gl.FRAMEBUFFER, f.fbo); gl.drawArrays(gl.TRIANGLES, 0, 3); }
    }

    return {
      isReady: function () { return !state.disposed; },
      run: function (params: any, _frame: any, changed: any) {
        if (gl.isContextLost && gl.isContextLost()) return false; // the renderer's dispose path releases everything
        const s = saveState(gl);
        try {
          gl.disable(gl.BLEND); gl.disable(gl.DEPTH_TEST); gl.disable(gl.CULL_FACE); gl.disable(gl.SCISSOR_TEST);
          gl.colorMask(true, true, true, true); gl.bindVertexArray(vao);
          feed(params);
          for (const plan of plans) {
            if (plan.swap) { exchange(plan.swap); continue; }
            if (!ctx.holds(plan.pass.when, params, changed)) continue;
            if (plan.pass.type === "mipmap") {
              gl.activeTexture(gl.TEXTURE0 + firstUnit); gl.bindTexture(gl.TEXTURE_2D, resources[plan.pass.resource].tex); gl.generateMipmap(gl.TEXTURE_2D);
            } else draw(plan, params);
          }
          exchange(graph.frameSwaps || []);
        } finally { restoreState(gl, s); }
        const cache = ctx.host.textureCache;
        if (cache) cache._sceneTextureEpoch = (cache._sceneTextureEpoch || 0) + 1;
        return true;
      },
      // export names the texture a surface binds: { tex, width, height, levels } of the resource as of now.
      export: function (name: string) {
        const r = resources[graph.exports[name]];
        return r ? { tex: r.tex, width: r.w, height: r.h, levels: r.levels } : null;
      },
      split: function () { return state.split; },
      dispose: teardown,
    };
  }

  window.__gosx_elio_graph.executors.webgl2 = make;
})();
