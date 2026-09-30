// Shared detail contract and shader maths. Atlas layers: albedo RGB/roughness A,
// normal RGB, repeated for Ground and Steep. One array sampler fits mobile limits.
function sceneDetailVariantKey(kind, detail) {
  return detail ? kind + "-detail" : kind;
}

function sceneDetailQualityEnabled(state) {
  if (!state) return true;
  if (state.mode === "ladder") {
    const rung = state.ladder[state.rungIndex];
    return rung && rung.detail != null ? rung.detail !== false : state.rungIndex > 0;
  }
  return !state.enabled || !state.activeProfile || state.activeProfile.detail !== false;
}

function sceneDetailUniformData(detail, masks, enabled) {
  const out = new Float32Array(24);
  const d = sceneNormalizeDetail(detail);
  if (!d) return out;
  for (let i = 0; i < 2; i++) {
    const layer = i ? d.steep : d.ground;
    if (layer) out.set([layer.scale, layer.normalScale, layer.albedoMix, layer.roughnessMix], i * 4);
    else out[i * 4] = 2;
    out.set(masks ? masks.slice(i * 3, i * 3 + 3) : [0, 0, 0], 16 + i * 4);
  }
  out.set([d.fadeStart, d.fadeEnd, d.slopeStart * Math.PI / 180, d.slopeEnd * Math.PI / 180], 8);
  out.set([d.stochastic ? 1 : 0, d.steep ? 1 : 0, d.triplanar === true ? 1 : 0, d.triplanar !== false ? 1 : 0], 12);
  out[19] = enabled === false ? 0 : 1;
  return out;
}

function sceneDetailTextureRecords(detail, load) {
  const d = sceneNormalizeDetail(detail);
  const records = [], masks = [];
  for (const layer of [d.ground, d.steep]) {
    for (const role of ["albedo", "normal", "roughness"]) {
      const record = layer && layer[role] ? load(layer[role], role) : null;
      records.push(record);
      masks.push(record && record.loaded && !record.failed ? 1 : 0);
    }
  }
  return { records: records, masks: masks };
}

// A small emitter keeps the GLSL/WGSL arithmetic identical. Only declarations,
// texture calls and uniform access differ; this is not a general transpiler.
function sceneDetailShaderFunction(language, name, args, result, body) {
  if (language === "glsl") return result + " " + name + "(" + args + ") {\n" + body + "\n}\n";
  const params = args.split(", ").filter(Boolean).map(function(arg) {
    const parts = arg.split(" "); return parts[1] + ": " + parts[0];
  }).join(", ");
  body = body.replace(/\b(vec[234]|mat2|float|int|DetailTap|DetailResult)\s+(\w+)\s*=/g, "var $2: $1 =")
    .replace(/for \(int (\w+) =/g, "for (var $1: int =");
  return ("fn " + name + "(" + params + ") -> " + result + " {\n" + body + "\n}\n")
    .replace(/\bvec([234])\b/g, "vec$1f").replace(/\bmat2\b/g, "mat2x2f")
    .replace(/\bfloat\b/g, "f32").replace(/\bint\b/g, "i32");
}

function sceneDetailShaderSource(language) {
  const glsl = language === "glsl";
  const uniform = glsl ? "u_detail" : "detail.data";
  let source = glsl
    ? "uniform vec4 u_detail[6];\nuniform highp sampler2DArray u_detailAtlas;\nstruct DetailTap { vec4 ar; vec3 normal; };\nstruct DetailResult { vec3 albedo; vec3 normal; float roughness; };\n"
    : "struct DetailUniform { data: array<vec4f, 6>, };\n@group(2) @binding(0) var<uniform> detail: DetailUniform;\n@group(2) @binding(1) var detailAtlas: texture_2d_array<f32>;\n@group(2) @binding(2) var detailSampler: sampler;\nstruct DetailTap { ar: vec4f, normal: vec3f, };\nstruct DetailResult { albedo: vec3f, normal: vec3f, roughness: f32, };\n";
  function emit(name, args, result, body) {
    source += sceneDetailShaderFunction(language, name, args, result, body.replace(/PARAM/g, uniform));
  }
  emit("detailHash", "vec2 p", "vec3", `
    vec3 q = fract(vec3(p.x, p.y, p.x) * vec3(0.1031, 0.1030, 0.0973));
    q += dot(q, q.yxz + 33.33);
    return fract((q.xxy + q.yzz) * q.zyx);`);
  // Quarter turns preserve the footprint with UNROTATED explicit gradients.
  emit("detailRotation", "vec2 cell", "mat2", `
    float angle = floor(detailHash(cell).z * 4.0) * 1.57079632679;
    float c = cos(angle); float s = sin(angle);
    return mat2(vec2(c, s), vec2(-s, c));`);
  const sample = glsl
    ? "textureGrad(u_detailAtlas, vec3(uv, float(index)), dx, dy)"
    : "textureSampleGrad(detailAtlas, detailSampler, uv, index, dx, dy)";
  emit("detailPatch", "vec2 uv, vec2 dx, vec2 dy, vec2 cell, int layer", "DetailTap", `
    mat2 rot = detailRotation(cell);
    if (PARAM[3].x > 0.5) { uv = rot * uv + detailHash(cell).xy; }
    vec4 ar = vec4(0.5, 0.5, 0.5, 0.5);
    vec3 n = vec3(0.0, 0.0, 1.0);
    vec4 mask = PARAM[4 + layer];
    int index = layer * 2;
    if (mask.x + mask.z > 0.0) { ar = ${sample}; }
    index += 1;
    if (mask.y > 0.0) {
      n = (${sample}).rgb * 2.0 - 1.0;
      if (PARAM[3].x > 0.5) { n = vec3(transpose(rot) * n.xy, n.z); }
    }
    return DetailTap(ar, n);`);
  emit("detailPlane", "vec2 uv, vec2 dx, vec2 dy, int layer", "DetailTap", `
    if (PARAM[3].x < 0.5) { return detailPatch(uv, dx, dy, vec2(0.0), layer); }
    vec2 grid = vec2(uv.x - uv.y * 0.57735026919, uv.y * 1.15470053838);
    vec2 cell = floor(grid); vec2 f = fract(grid);
    vec2 a = cell; vec2 b = cell + vec2(1.0, 0.0); vec2 c = cell + vec2(0.0, 1.0);
    vec3 w = vec3(1.0 - f.x - f.y, f.x, f.y);
    if (f.x + f.y > 1.0) {
      a = cell + vec2(1.0); b = cell + vec2(0.0, 1.0); c = cell + vec2(1.0, 0.0);
      w = vec3(f.x + f.y - 1.0, 1.0 - f.x, 1.0 - f.y);
    }
    w = w * w * w; w /= w.x + w.y + w.z;
    DetailTap ta = detailPatch(uv, dx, dy, a, layer);
    DetailTap tb = detailPatch(uv, dx, dy, b, layer);
    DetailTap tc = detailPatch(uv, dx, dy, c, layer);
    return DetailTap(ta.ar * w.x + tb.ar * w.y + tc.ar * w.z, ta.normal * w.x + tb.normal * w.y + tc.normal * w.z);`);
  emit("detailLayer", "vec3 p, vec3 baseN, vec3 dx, vec3 dy, int layer", "DetailTap", `
    vec4 params = PARAM[layer]; p /= params.x; dx /= params.x; dy /= params.x;
    float tri = PARAM[3].z; if (layer == 1) { tri = PARAM[3].w; }
    if (tri < 0.5) {
      DetailTap tap = detailPlane(p.xz, dx.xz, dy.xz, layer);
      return DetailTap(tap.ar, vec3(tap.normal.x, 0.0, tap.normal.y) * params.y / max(tap.normal.z, 0.1));
    }
    vec3 weights = abs(baseN); weights *= weights; weights *= weights;
    weights /= weights.x + weights.y + weights.z;
    vec4 ar = vec4(0.0); vec3 offset = vec3(0.0);
    for (int axis = 0; axis < 3; axis++) {
      if (weights[axis] > 0.00001) {
        float side = 1.0; if (baseN[axis] < 0.0) { side = -1.0; }
        vec2 uv = p.xy * vec2(side, 1.0); vec2 gx = dx.xy * vec2(side, 1.0); vec2 gy = dy.xy * vec2(side, 1.0);
        if (axis == 0) { uv = p.zy * vec2(-side, 1.0); gx = dx.zy * vec2(-side, 1.0); gy = dy.zy * vec2(-side, 1.0); }
        if (axis == 1) { uv = p.xz * vec2(1.0, -side); gx = dx.xz * vec2(1.0, -side); gy = dy.xz * vec2(1.0, -side); }
        DetailTap tap = detailPlane(uv, gx, gy, layer);
        vec2 n = tap.normal.xy * params.y / max(tap.normal.z, 0.1);
        vec3 world = vec3(n.x * side, n.y, 0.0);
        if (axis == 0) { world = vec3(0.0, n.y, -n.x * side); }
        if (axis == 1) { world = vec3(n.x, 0.0, -n.y * side); }
        ar += tap.ar * weights[axis]; offset += world * weights[axis];
      }
    }
    return DetailTap(ar, offset);`);
  emit("detailApply", "vec3 p, vec3 baseN, vec3 dx, vec3 dy, vec3 camera, vec3 albedo, vec3 normal, float roughness", "DetailResult", `
    float fade = 1.0 - smoothstep(PARAM[2].x, PARAM[2].y, distance(p, camera));
    if (fade > 0.0 && PARAM[4].w > 0.5) {
      float slope = acos(clamp(abs(baseN.y), 0.0, 1.0));
      float blend = smoothstep(PARAM[2].z, PARAM[2].w, slope) * PARAM[3].y;
      vec3 modulation = vec3(0.0); vec3 offset = vec3(0.0); float r = roughness;
      for (int layer = 0; layer < 2; layer++) {
        float w = 1.0 - blend; if (layer == 1) { w = blend; }
        if (w > 0.0) {
          DetailTap tap = detailLayer(p, baseN, dx, dy, layer);
          vec4 mask = PARAM[4 + layer]; vec4 params = PARAM[layer];
          modulation += (mix(vec3(1.0), clamp(2.0 * tap.ar.rgb, vec3(0.0), vec3(2.0)), params.z * mask.x) - vec3(1.0)) * w;
          offset += tap.normal * w;
          r += (tap.ar.a - roughness) * params.w * mask.z * w;
        }
      }
      albedo *= vec3(1.0) + modulation * fade;
      roughness = clamp(mix(roughness, r, fade), 0.04, 1.0);
      offset -= baseN * dot(offset, baseN);
      normal = normalize(normal + offset * fade);
    }
    return DetailResult(albedo, normal, roughness);`);
  return source;
}
