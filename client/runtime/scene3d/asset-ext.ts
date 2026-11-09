// Optional asset features for the renderers, shipped in the glTF chunk.
//
// The renderers cannot carry this code: their source sets are at their line
// caps, and most scenes never need it. A model whose material reads occlusion
// or emissive from a second UV set (TEXCOORD_1, an AO or light bake) loads this
// chunk anyway, because the glTF loader lives in it, so the API below is
// present before the first draw of such a model. Until it is present, the
// renderers draw the default path.
//
// window.__gosx_scene3d_asset_ext.uv1
//   wanted(obj, mat)               true when obj carries a second UV stream and
//                                  mat samples occlusion or emissive from it
//   vertex(src), fragment(src)     rewrite the PBR GLSL so v_uv1 reaches the
//                                  fragment stage and the two maps read it
//   use(gl, obj, mat, skinned, creators)  the variant program for a draw, or null
//   bind(gl, program, obj, mat)    bind a_uv1 and the per-map set flags (variant programs only)
//   dispose(gl)                    release the cached programs and buffer
//
// WebGPU counterparts, for world-baked meshes (not skinned, retained or morphed):
//   gpuWanted(obj, mat)            "" or "o", "e", "oe": which maps read UV1
//   gpuModules(device, vs, fs, c)  vertex and fragment modules for that code
//   gpuDraw(device, pass, obj, offset, count, bindRange, buffers)
//                                  bind the five vertex slots and draw
//   gpuDispose(device)             release the per-object buffers
(function () {
  var win: any = window;
  var ext: any = win.__gosx_scene3d_asset_ext || (win.__gosx_scene3d_asset_ext = {});
  var perContext: WeakMap<any, any> = new WeakMap();

  function state(gl: any) {
    var s = perContext.get(gl);
    if (!s) {
      s = { programs: Object.create(null), buffer: null, locations: new WeakMap() };
      perContext.set(gl, s);
    }
    return s;
  }

  // The second stream of an object: a per-object array for world-baked meshes,
  // or the retained vertices of skinned and cached meshes.
  function streamOf(obj: any): any {
    if (!obj) return null;
    if (obj.uvs1) return obj.uvs1;
    return obj.directVertices && obj.vertices && obj.vertices.uvs1 ? obj.vertices.uvs1 : null;
  }

  var uv1: any = {
    failed: false,
    gpuFailed: false,
    wanted: function (obj: any, mat: any): boolean {
      return !uv1.failed && !!mat && !mat.detail && (mat.occlusionTexCoord === 1 || mat.emissiveTexCoord === 1) && !!streamOf(obj);
    },

    vertex: function (src: string): string {
      return src
        .replace("out vec2 v_uv;", "out vec2 v_uv;\nout vec2 v_uv1;\nin vec2 a_uv1;")
        .replace(/(\n[ \t]*v_uv = [^;\n]+;)/, "$1\n    v_uv1 = a_uv1;");
    },

    fragment: function (src: string): string {
      return src
        .replace("in vec2 v_uv;", "in vec2 v_uv;\nin vec2 v_uv1;\nuniform bool u_occlusionUV1;\nuniform bool u_emissiveUV1;")
        .split("texture(u_occlusionMap, v_uv)").join("texture(u_occlusionMap, u_occlusionUV1 ? v_uv1 : v_uv)")
        .split("texture(u_emissiveMap, v_uv)").join("texture(u_emissiveMap, u_emissiveUV1 ? v_uv1 : v_uv)");
    },

    // The program a draw should use, or null for the default path. creators is
    // the renderer's own factories: base and skinned build a program for the
    // variant, usable filters out one that failed to link.
    use: function (gl: any, obj: any, mat: any, skinned: boolean, creators: any): any {
      if (!uv1.wanted(obj, mat) || mat.customVertex || mat.customFragment || mat.shaderBackend) return null;
      var s = state(gl);
      var key = skinned ? "skinned" : "base";
      if (!(key in s.programs)) {
        s.programs[key] = (skinned ? creators.skinned : creators.base)(gl, false, uv1);
        // A variant that fails to build stops being wanted, so the objects
        // fall back to the default program on the next frame.
        if (!s.programs[key]) uv1.failed = true;
      }
      return creators.usable(s.programs[key]);
    },

    bind: function (gl: any, program: any, obj: any, mat: any): void {
      var s = state(gl);
      var cached = s.locations.get(program);
      if (!cached) {
        cached = {
          variant: Object.keys(s.programs).some(function (key: string) { return s.programs[key] && s.programs[key].program === program; }),
          attrib: gl.getAttribLocation(program, "a_uv1"),
          occlusion: gl.getUniformLocation(program, "u_occlusionUV1"),
          emissive: gl.getUniformLocation(program, "u_emissiveUV1"),
        };
        s.locations.set(program, cached);
      }
      // Only the variant programs declare these; the default programs skip.
      if (!cached.variant) return;
      if (cached.occlusion) gl.uniform1i(cached.occlusion, mat && mat.occlusionTexCoord === 1 ? 1 : 0);
      if (cached.emissive) gl.uniform1i(cached.emissive, mat && mat.emissiveTexCoord === 1 ? 1 : 0);
      var data = streamOf(obj);
      if (cached.attrib < 0 || !data) return;
      if (!s.buffer) s.buffer = gl.createBuffer();
      gl.bindBuffer(gl.ARRAY_BUFFER, s.buffer);
      gl.bufferData(gl.ARRAY_BUFFER, data, gl.DYNAMIC_DRAW);
      gl.enableVertexAttribArray(cached.attrib);
      gl.vertexAttribPointer(cached.attrib, 2, gl.FLOAT, false, 0, 0);
    },

    dispose: function (gl: any): void {
      var s = perContext.get(gl);
      if (!s) return;
      for (var key in s.programs) {
        var p = s.programs[key];
        if (p) { gl.deleteProgram(p.program); gl.deleteShader(p.vertexShader); gl.deleteShader(p.fragmentShader); }
      }
      if (s.buffer) gl.deleteBuffer(s.buffer);
      perContext.delete(gl);
    },
  };


  // --- WebGPU -------------------------------------------------------------
  // The renderer's four vertex slots (position, normal, uv, tangent) plus a
  // fifth for the second UV set at shaderLocation 9. The vertex module carries
  // its layout, which the pipeline factory reads.
  var gpuLayout: any[] = [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 1 }] },
    { arrayStride: 8, stepMode: "vertex", attributes: [{ format: "float32x2", offset: 0, shaderLocation: 2 }] },
    { arrayStride: 16, stepMode: "vertex", attributes: [{ format: "float32x4", offset: 0, shaderLocation: 3 }] },
    { arrayStride: 8, stepMode: "vertex", attributes: [{ format: "float32x2", offset: 0, shaderLocation: 9 }] },
  ];
  var gpuStates: WeakMap<any, any> = new WeakMap();

  function gpuState(device: any) {
    var g = gpuStates.get(device);
    if (!g) { g = { modules: Object.create(null), buffers: new Map() }; gpuStates.set(device, g); }
    return g;
  }

  function wgslVertex(src: string): string {
    return src
      .replace("    @location(3) tangent: vec4f,\n};", "    @location(3) tangent: vec4f,\n    @location(9) uv1: vec2f,\n};")
      .replace("    @location(5) @interpolate(flat) instanceColor: vec4f,\n};", "    @location(5) @interpolate(flat) instanceColor: vec4f,\n    @location(6) uv1: vec2f,\n};")
      .replace("    out.uv = in.uv;\n", "    out.uv = in.uv;\n    out.uv1 = in.uv1;\n");
  }

  function wgslFragment(src: string, code: string): string {
    var out = src.replace("    @location(5) @interpolate(flat) instanceColor: vec4f,\n};", "    @location(5) @interpolate(flat) instanceColor: vec4f,\n    @location(6) uv1: vec2f,\n};");
    if (code.indexOf("o") >= 0) out = out.split("textureSample(occlusionTex, occlusionSamp, in.uv)").join("textureSample(occlusionTex, occlusionSamp, in.uv1)");
    if (code.indexOf("e") >= 0) out = out.split("textureSample(emissiveTex, emissiveSamp, in.uv)").join("textureSample(emissiveTex, emissiveSamp, in.uv1)");
    return out;
  }

  uv1.wgslVertex = wgslVertex;
  uv1.wgslFragment = wgslFragment;

  uv1.gpuWanted = function (obj: any, mat: any): string {
    if (uv1.gpuFailed || !obj || !mat || !obj.uvs1 || obj.directVertices || obj.retainedGeometry || obj.computedMorph || obj.skin) return "";
    if (mat.detail || mat.customVertexWGSL || mat.customFragmentWGSL || mat.shaderBackend) return "";
    return (mat.occlusionTexCoord === 1 ? "o" : "") + (mat.emissiveTexCoord === 1 ? "e" : "");
  };

  uv1.gpuModules = function (device: any, vertexSource: string, fragmentSource: string, code: string): any {
    var g = gpuState(device);
    if (!g.modules[code]) {
      var vertex = device.createShaderModule({ label: "pbr-vert-uv1" + code, code: wgslVertex(vertexSource) });
      vertex.gosxVertexLayout = gpuLayout;
      g.modules[code] = { vertex: vertex, fragment: device.createShaderModule({ label: "pbr-frag-uv1" + code, code: wgslFragment(fragmentSource, code) }) };
    }
    return g.modules[code];
  };

  // Binds the four world-mesh slots by sub-range, so the per-object UV1 buffer
  // starts at vertex 0, then draws. Returns false when a slot could not bind.
  uv1.gpuDraw = function (device: any, pass: any, obj: any, offset: number, count: number, bindRange: any, buffers: any): boolean {
    var data = obj.uvs1;
    if (!buffers || !data || data.length < count * 2) { uv1.gpuFailed = true; return false; }
    if (!bindRange(pass, 0, buffers.positions, offset, count) || !bindRange(pass, 1, buffers.normals, offset, count) ||
        !bindRange(pass, 2, buffers.uvs, offset, count) || !bindRange(pass, 3, buffers.tangents, offset, count)) return false;
    var g = gpuState(device);
    var bytes = Math.max(16, Math.ceil(count * 8 / 4) * 4);
    var entry = g.buffers.get(obj.id);
    if (!entry || entry.size < bytes) {
      if (entry) entry.buffer.destroy();
      entry = { size: bytes, buffer: device.createBuffer({ label: "uv1-" + obj.id, size: bytes, usage: GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST }) };
      g.buffers.set(obj.id, entry);
    }
    device.queue.writeBuffer(entry.buffer, 0, data.subarray(0, count * 2));
    pass.setVertexBuffer(4, entry.buffer, 0, count * 8);
    pass.draw(count);
    return true;
  };

  uv1.gpuDispose = function (device: any): void {
    var g = gpuStates.get(device);
    if (!g) return;
    g.buffers.forEach(function (entry: any) { entry.buffer.destroy(); });
    gpuStates.delete(device);
  };

  ext.uv1 = uv1;
})();
