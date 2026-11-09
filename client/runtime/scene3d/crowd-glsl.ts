// WebGL2 crowd pass (lazy chunk bootstrap-feature-scene3d-crowd.js).
//
// The renderer calls window.__gosx_scene3d_crowd_api.webgl once per frame
// after its opaque meshes and the ocean. Each crowd draws up to three
// instanced passes, one per level of detail, with one draw per run of
// visible blocks. The program pairs the crowd vertex shader with the
// renderer's own PBR fragment (opts.fragment), and opts.prepare uploads the
// camera, lights, environment, shadow and material uniforms as it does for
// the renderer's meshes, so fans take the scene's lighting, received shadows,
// fog, exposure and tone mapping. The fragment gains one input, v_crowdGlow,
// which handheld lights add after tone mapping.
//
// The palette and section textures sit on vertex-stage units above the
// fragment range, which the PBR program never binds.

const SCENE_CROWD_GLSL_VERTEX = `#version 300 es
precision highp float;
precision highp int;
layout(location = 0) in vec3 a_position;
layout(location = 1) in vec3 a_normal;
layout(location = 2) in uvec4 a_joints;
layout(location = 3) in vec4 a_weights;
layout(location = 4) in uvec4 a_part;
layout(location = 5) in vec4 a_seat;
layout(location = 6) in float a_wave;
layout(location = 7) in uvec2 a_ids;
layout(location = 8) in uvec4 a_look;
layout(location = 9) in uvec4 a_extra;
uniform mat4 u_viewMatrix;
uniform mat4 u_projectionMatrix;
uniform vec3 u_cameraPosition;
uniform vec4 u_crowd[32];
uniform highp sampler2D u_crowdPalette;
uniform highp sampler2D u_crowdSections;
out vec3 v_worldPosition;
out vec3 v_normal;
out vec2 v_uv;
out vec3 v_tangent;
out vec3 v_bitangent;
flat out vec4 v_instanceColor;
flat out vec4 v_crowdGlow;
vec4 crowdSection(int s, int t) { return texelFetch(u_crowdSections, ivec2(t, s), 0); }
mat4 crowdJoint(int j, int row) {
  int x = j * 4;
  return mat4(texelFetch(u_crowdPalette, ivec2(x, row), 0), texelFetch(u_crowdPalette, ivec2(x + 1, row), 0),
    texelFetch(u_crowdPalette, ivec2(x + 2, row), 0), texelFetch(u_crowdPalette, ivec2(x + 3, row), 0));
}
mat4 crowdPose(int a, float ph, int j) {
  vec4 clip = u_crowd[14 + a];
  float f = (a == 8 ? clamp(ph, 0.0, 1.0) : fract(ph)) * clip.y;
#if CROWD_TIER > 0
  return crowdJoint(j, int(clip.x + floor(f + 0.5)));
#else
  float f0 = min(floor(f), clip.y - 1.0);
  mat4 m0 = crowdJoint(j, int(clip.x + f0));
  mat4 m1 = crowdJoint(j, int(clip.x + f0) + 1);
  return m0 + (m1 - m0) * (f - f0);
#endif
}
// crowdJoin is how far a fan has joined its section's action (0 idles, 1
// joined). A fan joins once the excitement ramp passes its eagerness, over
// 0.4 seconds, and leaves the same way.
float crowdJoin(vec4 t1, vec4 t2, float eager, float now) {
  float tau = (eager - 64.0) / 192.0;
  bool was = t1.x > tau, is = t1.y > tau;
  if (was == is) return is ? 1.0 : 0.0;
  float crossing = t2.z + (tau - t1.x) / (t1.y - t1.x) * max(t2.w, 0.001);
  float j = clamp((now - crossing) / 0.4, 0.0, 1.0);
  return is ? j : 1.0 - j;
}
float crowdPhase(int a, bool synced, vec4 t1, vec4 t2, float now, float seed, float variant) {
  float cycle = max(u_crowd[14 + a].z, 0.05);
  if (synced) return (now - t2.x) * t1.z / cycle + t2.y * (seed - 0.5);
  return now * t1.z * (0.92 + 0.16 * variant) / cycle + seed;
}
void main() {
  int s = int(a_ids.x);
  vec4 t0 = crowdSection(s, 0), t1 = crowdSection(s, 1), t2 = crowdSection(s, 2);
  int flags = int(t1.w + 0.5);
  float now = u_crowd[4].x;
  float eager = float(a_extra.x);
  float variant = float(a_look.w) / 255.0;
  float seed = float(a_ids.y) / 65536.0;
  int rest = (flags & 8) != 0 ? 1 : 0;
  int fanA = int(t0.x + 0.5), fanB = int(t0.y + 0.5);
  float join = crowdJoin(t1, t2, eager, now);
  join = join * join * (3.0 - 2.0 * join);
  float k = clamp((now - t0.z - eager / 255.0 * 0.6) / max(t0.w, 0.001), 0.0, 1.0);
  k = k * k * (3.0 - 2.0 * k);
  bool synced = (flags & 1) != 0;
  float phA = crowdPhase(fanA, synced, t1, t2, now, seed, variant);
  float phB = crowdPhase(fanB, synced, t1, t2, now, seed, variant);
  float phR = crowdPhase(rest, false, t1, t2, now, seed, variant);
  float waveU = -1.0;
  if ((flags & 2) == 0 && a_extra.x < 246u) {
    for (int w = 0; w < 4; w++) {
      vec4 wa = u_crowd[23 + w * 2], wb = u_crowd[24 + w * 2];
      if (wa.w <= 0.0 || wa.z == 0.0 || now < wa.x || waveU >= 0.0) continue;
      float x = abs(wa.z) * (now - wa.x) - fract(sign(wa.z) * (a_wave - wa.y));
      float u = (x - clamp(floor(x + 0.5), 0.0, wb.x - 1.0)) / wa.w + 0.5;
      if (u > 0.0 && u < 1.0) waveU = u;
    }
  }
  float waveW = waveU < 0.0 ? 0.0 : smoothstep(0.0, 0.2, waveU) * (1.0 - smoothstep(0.8, 1.0, waveU));
  mat4 skin = mat4(0.0);
  for (int i = 0; i < 4; i++) {
    float wt = a_weights[i];
    if (wt <= 0.0) continue;
    int j = int(a_joints[i]);
    mat4 m;
    if (join <= 0.0) m = crowdPose(rest, phR, j);
    else {
      m = crowdPose(fanB, phB, j);
      if (k < 1.0 && fanA != fanB) { mat4 ma = crowdPose(fanA, phA, j); m = ma + (m - ma) * k; }
      if (join < 1.0) { mat4 mr = crowdPose(rest, phR, j); m = mr + (m - mr) * join; }
    }
    if (waveW > 0.0) m = m + (crowdPose(8, waveU, j) - m) * waveW;
    skin += m * wt;
  }
  vec3 local = (skin * vec4(a_position, 1.0)).xyz;
  vec3 n = mat3(skin) * a_normal;
  float scale = 0.94 + 0.12 * float(a_look.w & 15u) / 15.0;
  mat4 model = mat4(u_crowd[0], u_crowd[1], u_crowd[2], u_crowd[3]);
  float yaw = a_seat.w;
#if CROWD_TIER == 2
  vec3 seatWorld = (model * vec4(a_seat.xyz, 1.0)).xyz;
  vec3 toCamera = u_cameraPosition - seatWorld;
  yaw = atan(toCamera.x, toCamera.z) - u_crowd[4].y;
  n = normalize(n + vec3(0.0, 0.6, 0.0));
#endif
  float cy = cos(yaw), sy = sin(yaw);
  vec3 p = vec3(cy * local.x + sy * local.z, local.y, -sy * local.x + cy * local.z) * scale + a_seat.xyz;
  vec3 nr = vec3(cy * n.x + sy * n.z, n.y, -sy * n.x + cy * n.z);
  uint kind = a_part.y;
  bool hidden = kind == 5u ? (a_look.w & 0x90u) != 0x90u : (kind > 0u && (kind != a_look.z || (flags & 4) == 0));
  vec4 world = model * vec4(p, 1.0);
  v_worldPosition = world.xyz;
  v_normal = normalize(mat3(model) * nr);
  v_uv = vec2(0.0);
  v_tangent = normalize(abs(v_normal.y) < 0.9 ? cross(vec3(0.0, 1.0, 0.0), v_normal) : cross(vec3(1.0, 0.0, 0.0), v_normal));
  v_bitangent = cross(v_normal, v_tangent);
  uint slot = a_part.x;
  vec3 color = vec3(0.25, 0.2, 0.15);
  if (slot == 0u) color = crowdSection(s, 3 + int(a_look.x)).rgb;
  else if (slot == 1u) color = u_crowd[6 + int((a_look.w >> 4) & 7u) % max(int(u_crowd[5].w), 1)].rgb;
  else if (slot == 2u) color = u_crowd[5].rgb;
  else if (slot == 3u || slot == 4u) color = crowdSection(s, 7 + int(a_look.y)).rgb;
  v_instanceColor = vec4(color, 1.0);
  float flicker = 0.8 + 0.2 * sin(now * 7.0 + seed * 40.0);
  v_crowdGlow = slot == 4u ? vec4(color * u_crowd[4].w * flicker, 1.0) : vec4(0.0);
  gl_Position = hidden ? vec4(2.0, 2.0, 2.0, 1.0) : u_projectionMatrix * u_viewMatrix * world;
}`;

// sceneCrowdFragmentGLSL adds the glow input and term to the renderer's PBR
// fragment. It runs after tone mapping: a display-space add, doubled for the
// linear post chain so bloom picks the lights up.
function sceneCrowdFragmentGLSL(pbr: string): string {
  const hook = "void gosxApplyCustomFragment(inout vec3 color, inout float opacity, vec3 normal, vec3 worldPosition, vec2 uv) {}";
  if (pbr.indexOf(hook) < 0) throw new Error("crowd: PBR fragment hook missing");
  return pbr.replace(hook, "flat in vec4 v_crowdGlow;\nvoid gosxApplyCustomFragment(inout vec3 color, inout float opacity, vec3 normal, vec3 worldPosition, vec2 uv) { color += v_crowdGlow.rgb * (u_outputLinear == 1 ? 2.0 : 0.8); }");
}

function sceneCrowdGLShader(gl: any, type: number, source: string): any {
  const shader = gl.createShader(type);
  gl.shaderSource(shader, source);
  gl.compileShader(shader);
  if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
    const log = gl.getShaderInfoLog(shader);
    gl.deleteShader(shader);
    throw new Error("crowd shader: " + log);
  }
  return shader;
}

// sceneCrowdGLProgram builds one program per level of detail and fragment
// source. The renderer's hook answers o.fragment(material) with the PBR
// fragment that material needs (a Selena surface material splices its
// surface in), so fans and props share a program when their materials do.
function sceneCrowdGLProgram(res: any, gl: any, o: any, tier: number, material: any): any {
  const pbr = typeof o.fragment === "function" ? o.fragment(material) : o.fragment;
  const key = tier + "|" + String(pbr || "");
  if (res.programs.has(key)) return res.programs.get(key);
  const vertex = sceneCrowdGLShader(gl, gl.VERTEX_SHADER, SCENE_CROWD_GLSL_VERTEX.replace("precision highp int;", "precision highp int;\n#define CROWD_TIER " + tier));
  const fragment = sceneCrowdGLShader(gl, gl.FRAGMENT_SHADER, sceneCrowdFragmentGLSL(String(pbr || "")));
  const program = gl.createProgram();
  gl.attachShader(program, vertex); gl.attachShader(program, fragment);
  gl.linkProgram(program);
  gl.deleteShader(vertex); gl.deleteShader(fragment);
  if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
    const log = gl.getProgramInfoLog(program);
    gl.deleteProgram(program);
    throw new Error("crowd program: " + log);
  }
  const uniforms = typeof o.uniforms === "function" ? o.uniforms(gl, program) : {};
  uniforms.crowd = gl.getUniformLocation(program, "u_crowd[0]") || gl.getUniformLocation(program, "u_crowd");
  uniforms.crowdPalette = gl.getUniformLocation(program, "u_crowdPalette");
  uniforms.crowdSections = gl.getUniformLocation(program, "u_crowdSections");
  const entry = { program, uniforms };
  res.programs.set(key, entry);
  return entry;
}

function sceneCrowdGLTexture(gl: any, width: number, height: number, data: Float32Array): any {
  const texture = gl.createTexture();
  gl.bindTexture(gl.TEXTURE_2D, texture);
  gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA32F, width, height, 0, gl.RGBA, gl.FLOAT, data);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
  return texture;
}

// Instance attributes 5..9 read one 32-byte record per fan from offset.
function sceneCrowdGLInstanceAttributes(gl: any, offset: number): void {
  gl.vertexAttribPointer(5, 4, gl.FLOAT, false, 32, offset);
  gl.vertexAttribPointer(6, 1, gl.FLOAT, false, 32, offset + 16);
  gl.vertexAttribIPointer(7, 2, gl.UNSIGNED_SHORT, 32, offset + 20);
  gl.vertexAttribIPointer(8, 4, gl.UNSIGNED_BYTE, 32, offset + 24);
  gl.vertexAttribIPointer(9, 4, gl.UNSIGNED_BYTE, 32, offset + 28);
}

function sceneCrowdGLStatic(res: any, gl: any, height: number): void {
  if (res.meshes && res.fanHeight === height) return;
  if (res.meshes) for (const mesh of res.meshes) { gl.deleteBuffer(mesh.vbo); gl.deleteBuffer(mesh.ibo); gl.deleteVertexArray(mesh.vao); }
  if (res.palette) gl.deleteTexture(res.palette);
  res.fanHeight = height;
  res.bake = sceneCrowdBake(height);
  res.palette = sceneCrowdGLTexture(gl, res.bake.width, res.bake.height, res.bake.data);
  const previous = gl.getParameter(gl.VERTEX_ARRAY_BINDING);
  res.meshes = sceneCrowdMeshes(height).map(function(mesh: any) {
    const vao = gl.createVertexArray();
    gl.bindVertexArray(vao);
    const vbo = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, vbo);
    gl.bufferData(gl.ARRAY_BUFFER, mesh.vertices, gl.STATIC_DRAW);
    for (let a = 0; a < 5; a++) gl.enableVertexAttribArray(a);
    gl.vertexAttribPointer(0, 3, gl.FLOAT, false, 36, 0);
    gl.vertexAttribPointer(1, 3, gl.FLOAT, false, 36, 12);
    gl.vertexAttribIPointer(2, 4, gl.UNSIGNED_BYTE, 36, 24);
    gl.vertexAttribPointer(3, 4, gl.UNSIGNED_BYTE, true, 36, 28);
    gl.vertexAttribIPointer(4, 4, gl.UNSIGNED_BYTE, 36, 32);
    const ibo = gl.createBuffer();
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, ibo);
    gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, mesh.indices, gl.STATIC_DRAW);
    return { vao, vbo, ibo, indexCount: mesh.indexCount };
  });
  gl.bindVertexArray(previous);
}

// sceneCrowdGLCrowd uploads one crowd's records and section texture.
function sceneCrowdGLCrowd(res: any, gl: any, ir: any, store: any): any {
  let gpu = res.crowds.get(ir.id);
  if (!gpu || gpu.ir !== ir) {
    if (gpu) sceneCrowdGLDisposeCrowd(gl, gpu);
    const packed = sceneCrowdPack(ir);
    if (packed.error) throw new Error(packed.error);
    gpu = { ir, packed, model: sceneCrowdModel(ir), version: -1, uniforms: new Float32Array(SCENE_CROWD_UNIFORM_VEC4S * 4) };
    gpu.instances = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, gpu.instances);
    gl.bufferData(gl.ARRAY_BUFFER, packed.records, gl.STATIC_DRAW);
    gpu.sections = sceneCrowdGLTexture(gl, SCENE_CROWD_SECTION_TEXELS, ir.sections.length, store.sections);
    gpu.version = store.version;
    store.fans = packed.sectionFans;
    res.crowds.set(ir.id, gpu);
  }
  if (gpu.version !== store.version) {
    gl.bindTexture(gl.TEXTURE_2D, gpu.sections);
    gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, 0, SCENE_CROWD_SECTION_TEXELS, ir.sections.length, gl.RGBA, gl.FLOAT, store.sections);
    gpu.version = store.version;
  }
  return gpu;
}

function sceneCrowdGLDisposeCrowd(gl: any, gpu: any): void {
  if (gpu.instances) gl.deleteBuffer(gpu.instances);
  if (gpu.sections) gl.deleteTexture(gpu.sections);
  gpu.instances = gpu.sections = null;
}

function sceneCrowdGLDispose(gl: any): void {
  const res = sceneCrowdGLResources.get(gl);
  if (!res) return;
  sceneCrowdGLResources.delete(gl);
  for (const gpu of res.crowds.values()) sceneCrowdGLDisposeCrowd(gl, gpu);
  for (const entry of res.programs.values()) gl.deleteProgram(entry.program);
  if (res.meshes) for (const mesh of res.meshes) { gl.deleteBuffer(mesh.vbo); gl.deleteBuffer(mesh.ibo); gl.deleteVertexArray(mesh.vao); }
  if (res.palette) gl.deleteTexture(res.palette);
}

const sceneCrowdGLResources = new WeakMap<object, any>();

// sceneCrowdGLDraw is the renderer's per-frame entry. It returns the state it
// publishes as data-gosx-scene3d-crowd: none, ready or failed.
function sceneCrowdGLDraw(gl: any, o: any): string {
  const crowds = o.environment && Array.isArray(o.environment.crowds) ? o.environment.crowds : null;
  const stats = { state: "none", fans: 0, full: 0, low: 0, impostor: 0, draws: 0, cpuMs: 0, error: "" };
  const started = typeof performance !== "undefined" ? performance.now() : 0;
  if (!crowds || !crowds.length) {
    if (sceneCrowdGLResources.has(gl)) sceneCrowdGLDispose(gl);
    sceneCrowdPublish(o.mount, stats);
    return stats.state;
  }
  let res = sceneCrowdGLResources.get(gl);
  if (!res) {
    res = { crowds: new Map(), programs: new Map(), ext: gl.getExtension("WEBGL_draw_instanced_base_vertex_base_instance") };
    const combined = gl.getParameter(gl.MAX_COMBINED_TEXTURE_IMAGE_UNITS), fragmentUnits = gl.getParameter(gl.MAX_TEXTURE_IMAGE_UNITS);
    res.units = combined - 4 >= fragmentUnits ? [combined - 4, combined - 3] : [fragmentUnits - 2, fragmentUnits - 1];
    sceneCrowdGLResources.set(gl, res);
  }
  const previousVAO = gl.getParameter(gl.VERTEX_ARRAY_BINDING), cullWas = gl.isEnabled(gl.CULL_FACE);
  try {
    const now = Number(o.time) || 0;
    sceneCrowdNoteClock(o.mount, now);
    const live = new Set();
    for (const ir of crowds) {
      if (!ir || !ir.id) continue;
      live.add(ir.id);
      const store = sceneCrowdStore(o.mount, ir);
      store.lastNow = now;
      sceneCrowdGLStatic(res, gl, Number(ir.fan && ir.fan.height) || 1.75);
      const gpu = sceneCrowdGLCrowd(res, gl, ir, store);
      const pick = sceneCrowdSelect(gpu, gpu.packed, ir, o);
      stats.fans += gpu.packed.count; stats.full += pick.fans[0]; stats.low += pick.fans[1]; stats.impostor += pick.fans[2];
      sceneCrowdUniforms(ir, store, res.bake, gpu.model, now, gpu.uniforms);
      const materials = sceneCrowdMaterials(ir);
      for (let tier = 0; tier < 3; tier++) {
        const ranges = pick.ranges[tier];
        if (!ranges.length) continue;
        const mesh = res.meshes[tier];
        gl.bindVertexArray(mesh.vao);
        gl.bindBuffer(gl.ARRAY_BUFFER, gpu.instances);
        for (let a = 5; a < 10; a++) { gl.enableVertexAttribArray(a); gl.vertexAttribDivisor(a, 1); }
        if (res.ext) sceneCrowdGLInstanceAttributes(gl, 0);
        // Bodies with the fan material, then props with the prop material.
        for (const [material, first, count] of [[materials.fan, 0, mesh.bodyIndexCount], [materials.props, mesh.bodyIndexCount, mesh.indexCount - mesh.bodyIndexCount]]) {
          const program = sceneCrowdGLProgram(res, gl, o, tier, material), u = program.uniforms;
          gl.useProgram(program.program);
          if (typeof o.prepare === "function") o.prepare(u, material);
          if (typeof o.surface === "function") o.surface(program, material);
          if (u.receiveShadow) gl.uniform1i(u.receiveShadow, ir.receiveShadow === false ? 0 : 1);
          gl.uniform4fv(u.crowd, gpu.uniforms);
          gl.activeTexture(gl.TEXTURE0 + res.units[0]); gl.bindTexture(gl.TEXTURE_2D, res.palette); gl.uniform1i(u.crowdPalette, res.units[0]);
          gl.activeTexture(gl.TEXTURE0 + res.units[1]); gl.bindTexture(gl.TEXTURE_2D, gpu.sections); gl.uniform1i(u.crowdSections, res.units[1]);
          gl.disable(gl.BLEND); gl.enable(gl.DEPTH_TEST); gl.depthMask(true); gl.depthFunc(gl.LEQUAL);
          gl.enable(gl.CULL_FACE); gl.cullFace(gl.BACK);
          for (let r = 0; r < ranges.length; r += 2) {
            if (res.ext) res.ext.drawElementsInstancedBaseVertexBaseInstanceWEBGL(gl.TRIANGLES, count, gl.UNSIGNED_SHORT, first * 2, ranges[r + 1], 0, ranges[r]);
            else { sceneCrowdGLInstanceAttributes(gl, ranges[r] * 32); gl.drawElementsInstanced(gl.TRIANGLES, count, gl.UNSIGNED_SHORT, first * 2, ranges[r + 1]); }
            stats.draws++;
          }
        }
      }
    }
    for (const id of Array.from(res.crowds.keys())) if (!live.has(id)) { sceneCrowdGLDisposeCrowd(gl, res.crowds.get(id)); res.crowds.delete(id); }
    stats.state = "ready";
  } catch (error) {
    stats.state = "failed";
    stats.error = String(error && (error as any).message || error);
  } finally {
    gl.bindVertexArray(previousVAO);
    if (!cullWas) gl.disable(gl.CULL_FACE);
    gl.activeTexture(gl.TEXTURE0);
    if (o.textureCache) o.textureCache._sceneTextureEpoch = (o.textureCache._sceneTextureEpoch || 0) + 1;
    if (o.program) gl.useProgram(o.program);
    stats.cpuMs = typeof performance !== "undefined" ? performance.now() - started : 0;
    sceneCrowdPublish(o.mount, stats);
  }
  return stats.state;
}

// sceneCrowdPublish writes the crowd's frame summary onto the mount when it
// changes: data-gosx-scene3d-crowd (state), -crowd-fans, -crowd-tiers
// (full/low/impostor fans) and -crowd-draws, plus -crowd-error on failure.
// The last frame's numbers, and the main-thread time the pass took, stay on
// window.__gosx_scene3d_crowd_api.stats for tests.
function sceneCrowdPublish(mount: any, stats: any): void {
  const api = typeof window !== "undefined" && (window as any).__gosx_scene3d_crowd_api;
  if (api) api.stats = stats;
  if (!mount || typeof mount.setAttribute !== "function") return;
  const tiers = stats.full + "/" + stats.low + "/" + stats.impostor;
  const key = stats.state + "|" + stats.fans + "|" + tiers + "|" + stats.draws + "|" + stats.error;
  if (mount.__gosxCrowdPublished === key) return;
  mount.__gosxCrowdPublished = key;
  mount.setAttribute("data-gosx-scene3d-crowd", stats.state);
  mount.setAttribute("data-gosx-scene3d-crowd-fans", String(stats.fans));
  mount.setAttribute("data-gosx-scene3d-crowd-tiers", tiers);
  mount.setAttribute("data-gosx-scene3d-crowd-draws", String(stats.draws));
  if (stats.error) mount.setAttribute("data-gosx-scene3d-crowd-error", stats.error);
  else if (typeof mount.removeAttribute === "function") mount.removeAttribute("data-gosx-scene3d-crowd-error");
}
