// WebGL2 Environment.Terrain pass (lazy chunk bootstrap-feature-scene3d-terrain.js).
//
// The twin of terrain-wgsl.ts. The renderer calls
// window.__gosx_scene3d_terrain_api.webgl once per frame after its opaque
// meshes. The program pairs the terrain vertex shader with the renderer's own
// PBR fragment (opts.fragment), patched to read the terrain surface, and the
// renderer's opts.prepare callback uploads the camera, lights, environment,
// shadow and material uniforms exactly as it does for its own meshes.
//
// Texture units: the material units carry the terrain normal map (0, the
// albedo sampler), the splat map (1, the normal sampler) and the four layer
// albedos (2, 3, 4 and 7: roughness, metalness, emissive, specular color), so
// the fragment stage stays at the 14 samplers the PBR program already uses.
// The height texture is a vertex-stage sampler on a unit above the fragment
// range when the device has one. WebGL2 refines at twice the WebGPU pixel
// error, so it draws fewer, coarser patches.

const SCENE_TERRAIN_GL_PIXEL_SCALE = 2;
const SCENE_TERRAIN_GL_LAYER_UNITS = [2, 3, 4, 7];
const SCENE_TERRAIN_GL_LAYER_SAMPLERS = ["u_roughnessMap", "u_metalnessMap", "u_emissiveMap", "u_specularColorMap"];

const SCENE_TERRAIN_VERTEX_GLSL = `#version 300 es
precision highp float;
precision highp int;
in vec4 a_terrainTile;
uniform mat4 u_viewMatrix;
uniform mat4 u_projectionMatrix;
uniform vec3 u_cameraPosition;
uniform vec4 u_terrain[36];
uniform highp sampler2D u_terrainHeights;
out vec3 v_worldPosition;
out vec3 v_normal;
out vec2 v_uv;
out vec3 v_tangent;
out vec3 v_bitangent;
flat out vec4 v_instanceColor;
float terrainTexel(ivec2 c) {
  return texelFetch(u_terrainHeights, clamp(c, ivec2(0), ivec2(u_terrain[1].zw) - 1), 0).r;
}
vec2 terrainGrid(vec2 xz) {
  vec2 last = u_terrain[1].zw - 1.0;
  return clamp((xz - u_terrain[0].xy) / u_terrain[0].zw * last, vec2(0.0), last);
}
float terrainSurface(vec2 xz) {
  vec2 g = terrainGrid(xz);
  vec2 cell = min(floor(g), u_terrain[1].zw - 2.0);
  vec2 f = g - cell;
  ivec2 c = ivec2(cell);
  float h00 = terrainTexel(c);
  float h10 = terrainTexel(c + ivec2(1, 0));
  float h01 = terrainTexel(c + ivec2(0, 1));
  float h11 = terrainTexel(c + ivec2(1, 1));
  float h = f.x >= f.y ? h00 + f.x * (h10 - h00) + f.y * (h11 - h10) : h00 + f.y * (h01 - h00) + f.x * (h11 - h01);
  return u_terrain[1].x + h * u_terrain[1].y;
}
vec4 terrainTint(int level) {
  float k = float(level % 6);
  return vec4(0.55 + 0.45 * cos(6.2831 * (k / 6.0 + vec3(0.0, 0.33, 0.67))), 1.0);
}
void main() {
  int q = int(u_terrain[2].y);
  vec2 ij = vec2(float(gl_VertexID % (q + 1)), float(gl_VertexID / (q + 1)));
  int level = int(a_terrainTile.w);
  vec2 lo = u_terrain[0].xy;
  vec2 hi = lo + u_terrain[0].zw;
  float s = u_terrain[2].x;
  vec2 index = a_terrainTile.xy + ij * a_terrainTile.z;
  vec2 p0 = min(lo + index * s, hi);
  float d = distance(u_cameraPosition, vec3(p0.x, terrainSurface(p0), p0.y));
  vec4 m = u_terrain[4 + min(level, 11)];
  float k = clamp((d - m.x) * m.y, 0.0, 1.0);
  index -= (ij - 2.0 * floor(ij * 0.5)) * a_terrainTile.z * k;
  vec2 xz = min(lo + index * s, hi);
  vec2 texel = u_terrain[0].zw / (u_terrain[1].zw - 1.0);
  float dx = terrainSurface(xz + vec2(texel.x, 0.0)) - terrainSurface(xz - vec2(texel.x, 0.0));
  float dz = terrainSurface(xz + vec2(0.0, texel.y)) - terrainSurface(xz - vec2(0.0, texel.y));
  vec3 N = normalize(vec3(-dx / (2.0 * texel.x), 1.0, -dz / (2.0 * texel.y)));
  v_worldPosition = vec3(xz.x, terrainSurface(xz), xz.y);
  v_normal = N;
  v_uv = (terrainGrid(xz) + 0.5) / u_terrain[1].zw;
  v_tangent = normalize(vec3(1.0, 0.0, 0.0) - N * N.x);
  v_bitangent = cross(N, v_tangent);
  v_instanceColor = u_terrain[3].w > 0.5 ? terrainTint(level) : vec4(1.0);
  gl_Position = u_projectionMatrix * u_viewMatrix * vec4(v_worldPosition, 1.0);
}`;

// sceneTerrainFragmentGLSL patches the renderer's PBR fragment the same way
// sceneTerrainFragmentWGSL patches the WGSL one.
function sceneTerrainFragmentGLSL(pbr: string): string {
  const anchors = ["void main() {", "    vec3 V = normalize(u_cameraPosition - v_worldPosition);"];
  for (const anchor of anchors) if (pbr.indexOf(anchor) < 0) throw new Error("terrain: PBR fragment anchor missing: " + anchor.trim());
  return pbr.replace("void main() {", `uniform vec4 u_terrain[36];
float terrainMask(float lo, float hi, float blend, float v) {
  float b = blend * 0.5;
  return smoothstep(lo - b, lo + b, v) * (1.0 - smoothstep(hi - b, hi + b, v));
}
vec3 terrainTap(int layer, vec2 uv, vec2 gx, vec2 gy) {
  if (layer == 0) return textureGrad(u_roughnessMap, uv, gx, gy).rgb;
  if (layer == 1) return textureGrad(u_metalnessMap, uv, gx, gy).rgb;
  if (layer == 2) return textureGrad(u_emissiveMap, uv, gx, gy).rgb;
  return textureGrad(u_specularColorMap, uv, gx, gy).rgb;
}
vec3 terrainLayer(int layer, vec3 p, vec3 dx, vec3 dy, vec3 w, float tri) {
  vec4 a = u_terrain[16 + layer * 4];
  vec4 b = u_terrain[17 + layer * 4];
  if (b.y < 0.5) return a.rgb;
  float s = 1.0 / b.x;
  vec3 c = terrainTap(layer, p.xz * s, dx.xz * s, dy.xz * s);
  if (tri > 0.001) {
    vec3 cx = terrainTap(layer, p.zy * s, dx.zy * s, dy.zy * s);
    vec3 cz = terrainTap(layer, p.xy * s, dx.xy * s, dy.xy * s);
    c = mix(c, cx * w.x + c * w.y + cz * w.z, tri);
  }
  return a.rgb * c;
}
void terrainSurfaceAt(vec3 dx, vec3 dy, vec2 ux, vec2 uy, out vec3 albedoOut, out vec3 normalOut, out float roughOut) {
  vec3 N = normalize(textureGrad(u_albedoMap, v_uv, ux, uy).xyz * 2.0 - 1.0);
  float slope = degrees(acos(clamp(N.y, -1.0, 1.0)));
  vec3 p = v_worldPosition;
  float edge = u_terrain[2].w;
  float tri = edge < 89.5 ? smoothstep(edge - 5.0, edge + 5.0, slope) : 0.0;
  vec3 w = pow(abs(N), vec3(4.0));
  w /= max(w.x + w.y + w.z, 1e-5);
  vec2 dim = max(u_terrain[3].yz, vec2(1.0));
  vec2 suv = (clamp((p.xz - u_terrain[0].xy) / u_terrain[0].zw, vec2(0.0), vec2(1.0)) * (dim - 1.0) + 0.5) / dim;
  vec4 splat = u_terrain[3].x > 0.5 ? textureLod(u_normalMap, suv, 0.0) : vec4(1.0);
  vec3 albedo = terrainLayer(0, p, dx, dy, w, tri);
  float rough = u_terrain[16].w;
  int count = int(u_terrain[2].z);
  for (int i = 1; i < 4; i++) {
    if (i >= count) break;
    vec4 hb = u_terrain[18 + i * 4];
    vec4 sb = u_terrain[19 + i * 4];
    float paint = i == 1 ? splat.r : (i == 2 ? splat.g : splat.b);
    float m = terrainMask(hb.x, hb.y, hb.z, p.y) * terrainMask(sb.x, sb.y, sb.z, slope) * paint;
    if (m > 0.001) {
      albedo = mix(albedo, terrainLayer(i, p, dx, dy, w, tri), m);
      rough = mix(rough, u_terrain[16 + i * 4].w, m);
    }
  }
  albedoOut = albedo * v_instanceColor.rgb;
  normalOut = N;
  roughOut = clamp(rough, 0.04, 1.0);
}
void main() {
    vec3 terrainDx = dFdx(v_worldPosition);
    vec3 terrainDy = dFdy(v_worldPosition);
    vec2 terrainUx = dFdx(v_uv);
    vec2 terrainUy = dFdy(v_uv);`)
    .replace("    vec3 V = normalize(u_cameraPosition - v_worldPosition);", `    terrainSurfaceAt(terrainDx, terrainDy, terrainUx, terrainUy, albedo, N, roughness);
    vec3 V = normalize(u_cameraPosition - v_worldPosition);`);
}

function sceneTerrainGLShader(gl: any, type: number, source: string): any {
  const shader = gl.createShader(type);
  gl.shaderSource(shader, source);
  gl.compileShader(shader);
  if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
    const log = gl.getShaderInfoLog(shader);
    gl.deleteShader(shader);
    throw new Error("terrain shader: " + log);
  }
  return shader;
}

function sceneTerrainGLProgram(res: any, gl: any, o: any): any {
  if (res.program && res.programSource === o.fragment) return res.program;
  if (res.program) { gl.deleteProgram(res.program.program); res.program = null; }
  const vertex = sceneTerrainGLShader(gl, gl.VERTEX_SHADER, SCENE_TERRAIN_VERTEX_GLSL);
  const fragment = sceneTerrainGLShader(gl, gl.FRAGMENT_SHADER, sceneTerrainFragmentGLSL(o.fragment));
  const program = gl.createProgram();
  gl.attachShader(program, vertex); gl.attachShader(program, fragment);
  gl.bindAttribLocation(program, 0, "a_terrainTile");
  gl.linkProgram(program);
  gl.deleteShader(vertex); gl.deleteShader(fragment);
  if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
    const log = gl.getProgramInfoLog(program);
    gl.deleteProgram(program);
    throw new Error("terrain program: " + log);
  }
  const uniforms = typeof o.uniforms === "function" ? o.uniforms(gl, program) : {};
  uniforms.terrain = gl.getUniformLocation(program, "u_terrain[0]") || gl.getUniformLocation(program, "u_terrain");
  uniforms.terrainHeights = gl.getUniformLocation(program, "u_terrainHeights");
  uniforms.terrainMaps = ["u_albedoMap", "u_normalMap"].concat(SCENE_TERRAIN_GL_LAYER_SAMPLERS).map(function(name: string) { return gl.getUniformLocation(program, name); });
  res.program = { program, uniforms };
  res.programSource = o.fragment;
  return res.program;
}

function sceneTerrainGLTexture(gl: any, mips: any[], internalFormat: number, format: number, type: number, filter: boolean): any {
  const texture = gl.createTexture();
  gl.bindTexture(gl.TEXTURE_2D, texture);
  for (let level = 0; level < mips.length; level++) {
    const mip = mips[level];
    gl.texImage2D(gl.TEXTURE_2D, level, internalFormat, mip.width, mip.height, 0, format, type, mip.data);
  }
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, filter ? (mips.length > 1 ? gl.LINEAR_MIPMAP_LINEAR : gl.LINEAR) : gl.NEAREST);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, filter ? gl.LINEAR : gl.NEAREST);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
  return texture;
}

function sceneTerrainGLDisposeModel(res: any, gl: any): void {
  for (const key of ["heightTex", "normalTex", "splatTex"]) { if (res[key]) gl.deleteTexture(res[key]); res[key] = null; }
  res.glModel = null;
}

function sceneTerrainGLDispose(res: any): void {
  if (!res || res.kind !== "webgl") return;
  const gl = res.gl;
  sceneTerrainGLDisposeModel(res, gl);
  if (res.program) gl.deleteProgram(res.program.program);
  if (res.vao) gl.deleteVertexArray(res.vao);
  for (const key of ["patchBuffer", "indexBuffer"]) if (res[key]) gl.deleteBuffer(res[key]);
  if (res.white) gl.deleteTexture(res.white);
  Object.assign(res, { kind: "", program: null, vao: null, patchBuffer: null, indexBuffer: null, white: null, model: null, modelKey: "" });
}

function sceneTerrainGLStatic(res: any, gl: any): void {
  if (res.kind === "webgl" && res.gl === gl) return;
  sceneTerrainGLDispose(res);
  res.kind = "webgl"; res.gl = gl; res.dispose = function() { sceneTerrainGLDispose(res); };
  const previousVAO = gl.getParameter(gl.VERTEX_ARRAY_BINDING);
  res.vao = gl.createVertexArray();
  gl.bindVertexArray(res.vao);
  res.patchBuffer = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, res.patchBuffer);
  gl.bufferData(gl.ARRAY_BUFFER, SCENE_TERRAIN_MAX_PATCHES * 16, gl.DYNAMIC_DRAW);
  gl.enableVertexAttribArray(0);
  gl.vertexAttribPointer(0, 4, gl.FLOAT, false, 16, 0);
  gl.vertexAttribDivisor(0, 1);
  const indices = sceneTerrainIndices();
  res.indexBuffer = gl.createBuffer();
  gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, res.indexBuffer);
  gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, indices, gl.STATIC_DRAW);
  res.indexCount = indices.length;
  gl.bindVertexArray(previousVAO);
  res.white = sceneTerrainGLTexture(gl, [{ width: 1, height: 1, data: new Uint8Array([255, 255, 255, 255]) }], gl.RGBA8, gl.RGBA, gl.UNSIGNED_BYTE, true);
  res.uniformData = new Float32Array(SCENE_TERRAIN_VEC4S * 4);
  const combined = gl.getParameter(gl.MAX_COMBINED_TEXTURE_IMAGE_UNITS), fragmentUnits = gl.getParameter(gl.MAX_TEXTURE_IMAGE_UNITS);
  res.heightUnit = combined - 2 >= fragmentUnits ? combined - 2 : 8;
}

function sceneTerrainGLModel(res: any, gl: any, model: any): void {
  if (res.glModel === model) return;
  sceneTerrainGLDisposeModel(res, gl);
  const flip = gl.getParameter(gl.UNPACK_FLIP_Y_WEBGL), premultiply = gl.getParameter(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL), alignment = gl.getParameter(gl.UNPACK_ALIGNMENT);
  gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false); gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, false); gl.pixelStorei(gl.UNPACK_ALIGNMENT, 1);
  res.heightTex = sceneTerrainGLTexture(gl, [{ width: model.columns, height: model.rows, data: model.heights }], gl.R32F, gl.RED, gl.FLOAT, false);
  res.normalTex = sceneTerrainGLTexture(gl, model.normals, gl.RGBA8, gl.RGBA, gl.UNSIGNED_BYTE, true);
  const splat = sceneTerrainSplat(model.ir) || { width: 1, height: 1, data: new Uint8Array([255, 255, 255, 255]) };
  res.splatTex = sceneTerrainGLTexture(gl, [splat], gl.RGBA8, gl.RGBA, gl.UNSIGNED_BYTE, true);
  gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, flip); gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, premultiply); gl.pixelStorei(gl.UNPACK_ALIGNMENT, alignment);
  res.glModel = model;
}

// sceneTerrainGLDraw is the renderer's per-frame entry; the returned state is
// published as data-gosx-scene3d-terrain.
function sceneTerrainGLDraw(res: any, gl: any, o: any): string {
  const ir = o.environment && o.environment.terrain;
  let state = "none";
  try {
    sceneWindUpdate(o.environment, o.time);
    if (!ir) { if (res.kind) sceneTerrainGLDispose(res); return state; }
    sceneTerrainGLStatic(res, gl);
    const model = sceneTerrainModelFor(res, ir);
    state = res.modelError ? "failed" : "pending";
    if (!model) return state;
    sceneTerrainGLModel(res, gl, model);
    const program = sceneTerrainGLProgram(res, gl, o);
    const layers = Array.isArray(ir.layers) ? ir.layers : [];
    const flags = [0, 0, 0, 0], layerTextures = [res.white, res.white, res.white, res.white];
    for (let i = 0; i < 4 && i < layers.length; i++) {
      const record = layers[i] && layers[i].albedo && typeof o.load === "function" ? o.load(gl, layers[i].albedo, o.textureCache, null, "base-color", "srgb") : null;
      if (record && record.loaded && record.texture) { layerTextures[i] = record.texture; flags[i] = 1; }
    }
    const count = sceneTerrainFrame(res, model, o.camera, o.viewProj, o.height, SCENE_TERRAIN_GL_PIXEL_SCALE);
    sceneTerrainUniforms(model, res.ranges, flags, sceneTerrainTint(), res.uniformData);
    state = "ready";
    if (count <= 0) return state;
    gl.useProgram(program.program);
    const u = program.uniforms;
    if (typeof o.prepare === "function") o.prepare(u, { color: "#ffffff", roughness: 1, metalness: 0 });
    if (u.receiveShadow) gl.uniform1i(u.receiveShadow, ir.receiveShadow === false ? 0 : 1);
    gl.uniform4fv(u.terrain, res.uniformData);
    const units = [0, 1].concat(SCENE_TERRAIN_GL_LAYER_UNITS), textures = [res.normalTex, res.splatTex].concat(layerTextures);
    for (let i = 0; i < units.length; i++) {
      gl.activeTexture(gl.TEXTURE0 + units[i]);
      gl.bindTexture(gl.TEXTURE_2D, textures[i]);
      if (u.terrainMaps[i]) gl.uniform1i(u.terrainMaps[i], units[i]);
    }
    gl.activeTexture(gl.TEXTURE0 + res.heightUnit);
    gl.bindTexture(gl.TEXTURE_2D, res.heightTex);
    gl.uniform1i(u.terrainHeights, res.heightUnit);
    gl.disable(gl.BLEND); gl.enable(gl.DEPTH_TEST); gl.depthMask(true); gl.depthFunc(gl.LEQUAL);
    const previousVAO = gl.getParameter(gl.VERTEX_ARRAY_BINDING);
    gl.bindVertexArray(res.vao);
    gl.bindBuffer(gl.ARRAY_BUFFER, res.patchBuffer);
    gl.bufferSubData(gl.ARRAY_BUFFER, 0, res.patches, 0, count * 4);
    gl.drawElementsInstanced(gl.TRIANGLES, res.indexCount, gl.UNSIGNED_SHORT, 0, count);
    gl.bindVertexArray(previousVAO);
    gl.activeTexture(gl.TEXTURE0);
    if (o.textureCache) o.textureCache._sceneTextureEpoch = (o.textureCache._sceneTextureEpoch || 0) + 1;
    if (o.program) gl.useProgram(o.program);
  } catch (error) {
    res.error = String(error && (error as any).message || error);
    state = "failed";
  } finally {
    if (o.mount && typeof o.mount.setAttribute === "function") {
      o.mount.setAttribute("data-gosx-scene3d-terrain", state);
      o.mount.setAttribute("data-gosx-scene3d-terrain-patches", String(res.count || 0));
    }
  }
  return state;
}

function sceneTerrainHeightAt(model: any, x: number, z: number): number {
  return model ? sceneTerrainHeight(model, x, z) : 0;
}

if (typeof window !== "undefined") {
  (window as any).__gosx_scene3d_terrain_api = {
    webgpu: sceneTerrainGPUDraw,
    webgl: sceneTerrainGLDraw,
    build: sceneTerrainBuild,
    height: sceneTerrainHeightAt,
    select: sceneTerrainSelect,
    ranges: sceneTerrainRanges,
    samples: sceneTerrainSamples,
    bytes: sceneTerrainBytes,
    windFor: sceneWindFor,
    windUpdate: sceneWindUpdate,
    windGust: sceneWindGust,
  };
}
