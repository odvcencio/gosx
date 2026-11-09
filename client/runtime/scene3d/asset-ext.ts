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

  ext.uv1 = uv1;
})();
