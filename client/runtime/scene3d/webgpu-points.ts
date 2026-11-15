// Builtin points shaders. The fragment shading lives in pointsShade, so the
// hard entry point (WGSL_POINTS_FRAGMENT) and the soft-particle entry point
// share it without editing shader text.
  // -----------------------------------------------------------------------
  // Points Vertex Shader (WGSL) -- instanced billboard quads
  // -----------------------------------------------------------------------

  var WGSL_POINTS_VERTEX = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct ParticleInstance {",
    "    position: vec3f,",
    "    size: f32,",
    "    color: vec4f,",
    "};",
    "",
    "struct PointsUniforms {",
    "    modelMatrix: mat4x4f,",
    "    defaultColorAndSize: vec4f,",
    "    flags: vec4u,",
    "    params: vec4f,",
    "    fogColor: vec4f,",
    "};",
    "",
    "struct PointsOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) @interpolate(flat) color: vec3f,",
    "    @location(1) @interpolate(flat) fogFactor: f32,",
    "    @location(2) @interpolate(flat) alpha: f32,",
    "    @location(3) pointCoord: vec2f,",
    "    @location(4) @interpolate(flat) pointSize: f32,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "@group(2) @binding(0) var<uniform> points: PointsUniforms;",
    "@group(2) @binding(1) var<storage, read> particles: array<ParticleInstance>;",
    "",
    // Unit quad: 6 vertices for 2 triangles.
    "const quadPos = array<vec2f, 6>(",
    "    vec2f(-0.5, -0.5), vec2f(0.5, -0.5), vec2f(-0.5, 0.5),",
    "    vec2f(0.5, -0.5), vec2f(0.5, 0.5), vec2f(-0.5, 0.5),",
    ");",
    "",
    "@vertex fn vertexMain(",
    "    @builtin(vertex_index) vertexIndex: u32,",
    "    @builtin(instance_index) instanceIndex: u32,",
    ") -> PointsOutput {",
    "    let quad = quadPos[vertexIndex];",
    "    let p = particles[instanceIndex];",
    "",
    "    let worldPos = (points.modelMatrix * vec4f(p.position, 1.0)).xyz;",
    "    let viewPos = frame.viewMatrix * vec4f(worldPos, 1.0);",
    "",
    // Compute point size with optional attenuation.
    "    var rawSize = p.size;",
    "    if (points.flags.y == 0u) { rawSize = points.defaultColorAndSize.w; }",
    "",
    "    var pixelSize: f32;",
    "    if (points.flags.z != 0u) {",
    "        pixelSize = max(rawSize * (frame.viewportHeight * 0.5) / max(-viewPos.z, 0.001), 1.0);",
    "    } else {",
    "        pixelSize = max(rawSize, 1.0);",
    "    }",
    "    let minPixelSize = max(points.fogColor.a, 0.0);",
    "    if (minPixelSize > 0.0) {",
    "        pixelSize = max(pixelSize, minPixelSize);",
    "    }",
    "    if (points.params.w > 0.0) {",
    "        pixelSize = min(pixelSize, points.params.w);",
    "    }",
    "",
    // Billboard: offset in clip space by quad * pixelSize.
    "    let clipPos = frame.projMatrix * viewPos;",
    "    let viewport = max(vec2f(frame.viewportWidth, frame.viewportHeight), vec2f(1.0));",
    "    let ndcOffsetX = quad.x * pixelSize / viewport.x * clipPos.w * 2.0;",
    "    let ndcOffsetY = quad.y * pixelSize / viewport.y * clipPos.w * 2.0;",
    "",
    "    var out: PointsOutput;",
    "    out.clipPos = vec4f(clipPos.x + ndcOffsetX, clipPos.y + ndcOffsetY, clipPos.z, clipPos.w);",
    "",
    // Color.
    "    if (points.flags.x != 0u) {",
    "        out.color = p.color.rgb;",
    "    } else {",
    "        out.color = points.defaultColorAndSize.rgb;",
    "    }",
    "    out.alpha = p.color.a * points.params.x;",
    "    out.pointCoord = quad + vec2f(0.5, 0.5);",
    "    out.pointSize = pixelSize;",
    "",
    // Fog.
    "    if (points.params.y != 0.0) {",
    "        let dist = length(viewPos.xyz);",
    "        out.fogFactor = clamp(exp(-points.params.z * points.params.z * dist * dist), 0.0, 1.0);",
    "    } else {",
    "        out.fogFactor = 1.0;",
    "    }",
    "",
    "    return out;",
    "}",
  ].join("\n");

  var WGSL_POINTS_INSTANCED_VERTEX = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct PointsUniforms {",
    "    modelMatrix: mat4x4f,",
    "    defaultColorAndSize: vec4f,",
    "    flags: vec4u,",
    "    params: vec4f,",
    "    fogColor: vec4f,",
    "};",
    "",
    "struct PointsInput {",
    "    @location(0) position: vec3f,",
    "    @location(1) size: f32,",
    "    @location(2) color: vec4f,",
    "};",
    "",
    "struct PointsOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) @interpolate(flat) color: vec3f,",
    "    @location(1) @interpolate(flat) fogFactor: f32,",
    "    @location(2) @interpolate(flat) alpha: f32,",
    "    @location(3) pointCoord: vec2f,",
    "    @location(4) @interpolate(flat) pointSize: f32,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "@group(2) @binding(0) var<uniform> points: PointsUniforms;",
    "",
    // Unit quad: 6 vertices for 2 triangles.
    "const quadPos = array<vec2f, 6>(",
    "    vec2f(-0.5, -0.5), vec2f(0.5, -0.5), vec2f(-0.5, 0.5),",
    "    vec2f(0.5, -0.5), vec2f(0.5, 0.5), vec2f(-0.5, 0.5),",
    ");",
    "",
    "@vertex fn vertexMain(",
    "    @builtin(vertex_index) vertexIndex: u32,",
    "    in: PointsInput,",
    ") -> PointsOutput {",
    "    let quad = quadPos[vertexIndex];",
    "",
    "    let worldPos = (points.modelMatrix * vec4f(in.position, 1.0)).xyz;",
    "    let viewPos = frame.viewMatrix * vec4f(worldPos, 1.0);",
    "",
    // Compute point size with optional attenuation.
    "    var rawSize = in.size;",
    "    if (points.flags.y == 0u) { rawSize = points.defaultColorAndSize.w; }",
    "",
    "    var pixelSize: f32;",
    "    if (points.flags.z != 0u) {",
    "        pixelSize = max(rawSize * (frame.viewportHeight * 0.5) / max(-viewPos.z, 0.001), 1.0);",
    "    } else {",
    "        pixelSize = max(rawSize, 1.0);",
    "    }",
    "    let minPixelSize = max(points.fogColor.a, 0.0);",
    "    if (minPixelSize > 0.0) {",
    "        pixelSize = max(pixelSize, minPixelSize);",
    "    }",
    "    if (points.params.w > 0.0) {",
    "        pixelSize = min(pixelSize, points.params.w);",
    "    }",
    "",
    // Billboard: offset in clip space by quad * pixelSize.
    "    let clipPos = frame.projMatrix * viewPos;",
    "    let viewport = max(vec2f(frame.viewportWidth, frame.viewportHeight), vec2f(1.0));",
    "    let ndcOffsetX = quad.x * pixelSize / viewport.x * clipPos.w * 2.0;",
    "    let ndcOffsetY = quad.y * pixelSize / viewport.y * clipPos.w * 2.0;",
    "",
    "    var out: PointsOutput;",
    "    out.clipPos = vec4f(clipPos.x + ndcOffsetX, clipPos.y + ndcOffsetY, clipPos.z, clipPos.w);",
    "",
    // Color.
    "    if (points.flags.x != 0u) {",
    "        out.color = in.color.rgb;",
    "    } else {",
    "        out.color = points.defaultColorAndSize.rgb;",
    "    }",
    "    out.alpha = in.color.a * points.params.x;",
    "    out.pointCoord = quad + vec2f(0.5, 0.5);",
    "    out.pointSize = pixelSize;",
    "",
    // Fog.
    "    if (points.params.y != 0.0) {",
    "        let dist = length(viewPos.xyz);",
    "        out.fogFactor = clamp(exp(-points.params.z * points.params.z * dist * dist), 0.0, 1.0);",
    "    } else {",
    "        out.fogFactor = 1.0;",
    "    }",
    "",
    "    return out;",
    "}",
  ].join("\n");

  // PointsUniforms fields shared by the fragment entry points.
  var WGSL_POINTS_UNIFORM_FIELDS = [
    "    modelMatrix: mat4x4f,",
    "    defaultColorAndSize: vec4f,",
    "    flags: vec4u,",
    "    params: vec4f,",
    "    fogColor: vec4f,",
  ].join("\n");

  var WGSL_POINTS_SHADE = [
    "@group(2) @binding(0) var<uniform> points: PointsUniforms;",
    "",
    "struct PointsInput {",
    "    @location(0) @interpolate(flat) color: vec3f,",
    "    @location(1) @interpolate(flat) fogFactor: f32,",
    "    @location(2) @interpolate(flat) alpha: f32,",
    "    @location(3) pointCoord: vec2f,",
    "    @location(4) @interpolate(flat) pointSize: f32,",
    "};",
    "",
    "fn pointsShade(in: PointsInput) -> vec4f {",
    "    var color = in.color;",
    "    var alpha = in.alpha;",
    "    if (points.flags.w == 1u) {",
    "        let centered = in.pointCoord - vec2f(0.5, 0.5);",
    "        let radial = length(centered);",
    "        let square = max(abs(centered.x), abs(centered.y));",
    "        let focus = clamp((in.pointSize - 1.0) / 10.0, 0.0, 1.0);",
    "        let coreRadius = mix(0.49, 0.18, focus);",
    "        let core = 1.0 - smoothstep(coreRadius, coreRadius + 0.05, square);",
    "        let halo = (1.0 - smoothstep(0.12, 0.72, radial)) * focus;",
    "        let streakX = 1.0 - smoothstep(0.02, 0.16, abs(centered.x));",
    "        let streakY = 1.0 - smoothstep(0.02, 0.16, abs(centered.y));",
    "        let streak = max(streakX, streakY) * focus;",
    "        alpha = clamp(core + halo * 0.5 + streak * 0.2, 0.0, 1.0) * in.alpha;",
    "        color = mix(color, vec3f(1.0, 1.0, 1.0), clamp(focus * 0.22 + core * focus * 0.28, 0.0, 0.4));",
    "    } else if (points.flags.w == 2u) {",
    "        let centered = in.pointCoord - vec2f(0.5, 0.5);",
    "        let radial = length(centered) * 2.0;",
    "        if (radial > 1.0) {",
    "            discard;",
    "        }",
    "        let sizeFocus = clamp((in.pointSize - 4.0) / 48.0, 0.0, 1.0);",
    "        let falloff = mix(4.2, 3.2, sizeFocus);",
    "        let core = exp(-radial * radial * falloff);",
    "        let edgeFeather = 1.0 - smoothstep(0.78, 1.0, radial);",
    "        alpha = core * edgeFeather * in.alpha;",
    "    }",
    "    if (alpha <= 0.003) {",
    "        discard;",
    "    }",
    "    if (points.params.y != 0.0) {",
    "        color = mix(points.fogColor.rgb, color, in.fogFactor);",
    "    }",
    "    return vec4f(color, alpha);",
    "}",
  ].join("\n");

  var WGSL_POINTS_FRAGMENT = [
    "struct PointsUniforms {",
    WGSL_POINTS_UNIFORM_FIELDS,
    "};",
    WGSL_POINTS_SHADE,
    "@fragment fn fragmentMain(in: PointsInput) -> @location(0) vec4f { return pointsShade(in); }",
  ].join("\n");

  // wgpuComputeParticleModelMatrix fills a compute-particle emitter's model
  // matrix: translation and tilt from the emitter, then time-based spin in
  // local space. tilt is written only when the emitter spins, matching the
  // point model-matrix contract that reads it.
  function wgpuComputeParticleModelMatrix(emitter: any, timeSeconds: number, model: Float32Array, tilt: Float32Array, spin: Float32Array) {
    var px = sceneNumber(emitter.x, 0);
    var py = sceneNumber(emitter.y, 0);
    var pz = sceneNumber(emitter.z, 0);
    var hasSpin = sceneNumber(emitter.spinX, 0) !== 0 || sceneNumber(emitter.spinY, 0) !== 0 || sceneNumber(emitter.spinZ, 0) !== 0;

    if (hasSpin) {
      var spx = sceneNumber(emitter.spinX, 0) * timeSeconds;
      var spy = sceneNumber(emitter.spinY, 0) * timeSeconds;
      var spz = sceneNumber(emitter.spinZ, 0) * timeSeconds;
      var csx = Math.cos(spx), ssx = Math.sin(spx);
      var csy = Math.cos(spy), ssy = Math.sin(spy);
      var csz = Math.cos(spz), ssz = Math.sin(spz);
      spin[0] = csy*csz; spin[4] = ssx*ssy*csz-csx*ssz; spin[8]  = csx*ssy*csz+ssx*ssz; spin[12] = 0;
      spin[1] = csy*ssz; spin[5] = ssx*ssy*ssz+csx*csz; spin[9]  = csx*ssy*ssz-ssx*csz; spin[13] = 0;
      spin[2] = -ssy;    spin[6] = ssx*csy;             spin[10] = csx*csy;             spin[14] = 0;
      spin[3] = 0;       spin[7] = 0;                   spin[11] = 0;                   spin[15] = 1;

      var rx = sceneNumber(emitter.rotationX, 0);
      var ry = sceneNumber(emitter.rotationY, 0);
      var rz = sceneNumber(emitter.rotationZ, 0);
      var cxr = Math.cos(rx), sxr = Math.sin(rx);
      var cyr = Math.cos(ry), syr = Math.sin(ry);
      var czr = Math.cos(rz), szr = Math.sin(rz);
      tilt[0] = cyr*czr; tilt[4] = sxr*syr*czr-cxr*szr; tilt[8]  = cxr*syr*czr+sxr*szr; tilt[12] = px;
      tilt[1] = cyr*szr; tilt[5] = sxr*syr*szr+cxr*czr; tilt[9]  = cxr*syr*szr-sxr*czr; tilt[13] = py;
      tilt[2] = -syr;    tilt[6] = sxr*cyr;             tilt[10] = cxr*cyr;             tilt[14] = pz;
      tilt[3] = 0;       tilt[7] = 0;                   tilt[11] = 0;                   tilt[15] = 1;

      sceneMat4MultiplyInto(model, tilt, spin);
    } else {
      var rx2 = sceneNumber(emitter.rotationX, 0);
      var ry2 = sceneNumber(emitter.rotationY, 0);
      var rz2 = sceneNumber(emitter.rotationZ, 0);
      var cxr2 = Math.cos(rx2), sxr2 = Math.sin(rx2);
      var cyr2 = Math.cos(ry2), syr2 = Math.sin(ry2);
      var czr2 = Math.cos(rz2), szr2 = Math.sin(rz2);
      model[0] = cyr2*czr2; model[4] = sxr2*syr2*czr2-cxr2*szr2; model[8]  = cxr2*syr2*czr2+sxr2*szr2; model[12] = px;
      model[1] = cyr2*szr2; model[5] = sxr2*syr2*szr2+cxr2*czr2; model[9]  = cxr2*syr2*szr2-sxr2*czr2; model[13] = py;
      model[2] = -syr2;     model[6] = sxr2*cyr2;                model[10] = cxr2*cyr2;                model[14] = pz;
      model[3] = 0;         model[7] = 0;                        model[11] = 0;                        model[15] = 1;
    }
  }

  // wgpuComputeParticlePointsUniforms fills the 32-float PointsUniforms block
  // for one compute-particle system and returns the float view.
  function wgpuComputeParticlePointsUniforms(puF: Float32Array, puU: Uint32Array, entry: any, material: any, tilt: Float32Array, model: Float32Array, fogDensity: number, fogColorRGBA: number[]) {
    puF.fill(0);
    puF.set(sceneWebGPUPointModelMatrix(entry, tilt, model), 0);
    var defaultColorRGBA = sceneColorRGBA(material.color, [1, 1, 1, 1]);
    puF[16] = defaultColorRGBA[0];
    puF[17] = defaultColorRGBA[1];
    puF[18] = defaultColorRGBA[2];
    puF[19] = sceneNumber(material.size, 1);
    puU[20] = 1;
    puU[21] = 1;
    puU[22] = material.attenuation ? 1 : 0;
    puU[23] = scenePointStyleCode(material.style);
    puF[24] = 1;
    puF[25] = fogDensity > 0 ? 1 : 0;
    puF[26] = fogDensity;
    puF[27] = sceneNumber(material.maxPixelSize, 0);
    puF[28] = fogColorRGBA[0];
    puF[29] = fogColorRGBA[1];
    puF[30] = fogColorRGBA[2];
    puF[31] = sceneNumber(material.minPixelSize, 0);
    return puF;
  }
