// WebGL2 temporal resolve with motion vectors, used by the temporal history.
//
// Static geometry reprojects through the scene depth and the previous view, as
// before. Moving retained meshes and skinned meshes draw a velocity pass into
// an RGBA16F target with its own depth: rg = uv offset to the previous frame,
// b = clip w, a = 1. The resolve uses a mover's offset only where that mover is
// the visible surface (its w matches the scene depth), and then skips the
// depth-change rejection, so a moving mesh keeps its own history instead of
// restarting every frame. Pixels near an ocean or pool surface keep less
// history, because the water moves without depth. Objects without a record
// from the previous frame, CPU-baked world meshes and particles keep camera
// reprojection.
const SCENE_POST_TAA_SOURCE = `#version 300 es
precision highp float;
precision highp sampler2D;
in vec2 v_uv;
uniform sampler2D u_texture;
uniform sampler2D u_history;
uniform sampler2D u_historyDepth;
uniform sampler2D u_velocity;
uniform mat4 u_inverseView;
uniform mat4 u_previousView;
uniform mat4 u_previousProjection;
uniform vec4 u_temporalParams;
uniform vec4 u_temporalJitter;
uniform vec4 u_motion;
uniform vec4 u_ocean;
uniform vec4 u_pools[4];
out vec4 fragColor;
${SCENE_POST_VIEW_POSITION_GLSL}
vec3 toYCoCg(vec3 c) { return vec3(dot(c, vec3(0.25, 0.5, 0.25)), c.r * 0.5 - c.b * 0.5, -c.r * 0.25 + c.g * 0.5 - c.b * 0.25); }
vec3 fromYCoCg(vec3 c) { return vec3(c.x + c.y - c.z, c.x + c.z, c.x - c.y - c.z); }
bool waterAt(vec3 world) {
    if (u_ocean.z > 0.5 && abs(world.y - u_ocean.x) <= u_ocean.y) return true;
    for (int k = 0; k < 4; k++) {
        vec4 pool = u_pools[k];
        if (pool.w > 0.5 && abs(world.x) <= pool.x && abs(world.z) <= pool.y && abs(world.y) <= pool.z) return true;
    }
    return false;
}
void main() {
    vec4 current = texture(u_texture, v_uv);
    float depth = texture(u_depthTexture, v_uv).r;
    vec2 texel = 1.0 / vec2(textureSize(u_texture, 0));
    if (u_temporalParams.w < 0.5) { fragColor = current; return; }
    float nearDepth = depth, farDepth = depth;
    for (int y = -1; y <= 1; y++) for (int x = -1; x <= 1; x++) {
        float d = texture(u_depthTexture, v_uv + vec2(x, y) * texel).r;
        nearDepth = min(nearDepth, d); farDepth = max(farDepth, d);
    }
    if (nearDepth >= 0.999999) { fragColor = current; return; }
    bool coverageEdge = farDepth >= 0.999999;
    if (coverageEdge) depth = nearDepth;
    vec3 viewPosition = postViewPosition(v_uv, depth);
    vec4 world = u_inverseView * vec4(viewPosition, 1);
    vec4 previousPosition = u_previousView * world;
    vec4 clip = u_previousProjection * previousPosition;
    if (clip.w <= 0.0) { fragColor = current; return; }
    vec2 uv = clip.xy / clip.w * 0.5 + 0.5;
    bool mover = false;
    if (u_motion.x > 0.5) {
        vec4 velocity = texture(u_velocity, v_uv);
        if (velocity.a > 0.5 && abs(velocity.b + viewPosition.z) <= 0.02 * abs(viewPosition.z) + 0.02) { uv = v_uv + velocity.xy; mover = true; }
    }
    if (any(lessThan(uv, vec2(0))) || any(greaterThan(uv, vec2(1)))) { fragColor = current; return; }
    if (!mover) {
        float oldDepth = texture(u_historyDepth, uv).r;
        if (coverageEdge) for (int y = -1; y <= 1; y++) for (int x = -1; x <= 1; x++)
            oldDepth = min(oldDepth, texture(u_historyDepth, uv + vec2(x, y) * texel).r);
        oldDepth = oldDepth * 2.0 - 1.0;
        float oldZ = (u_previousProjection[3][2] - oldDepth * u_previousProjection[3][3]) /
                     (oldDepth * u_previousProjection[2][3] - u_previousProjection[2][2]);
        if (abs(oldZ - previousPosition.z) > u_temporalParams.z * max(1.0, abs(previousPosition.z))) { fragColor = current; return; }
    }
    vec4 low = vec4(1e20), high = vec4(-1e20), mean = vec4(0), square = vec4(0);
    for (int y = -1; y <= 1; y++) for (int x = -1; x <= 1; x++) {
        vec4 sampleColor = texture(u_texture, v_uv + vec2(x, y) * texel);
        vec4 c = vec4(toYCoCg(sampleColor.rgb), sampleColor.a);
        low = min(low, c); high = max(high, c); mean += c / 9.0; square += c * c / 9.0;
    }
    vec4 sigma = sqrt(max(vec4(0), square - mean * mean));
    low = max(low, mean - sigma * u_temporalParams.y);
    high = min(high, mean + sigma * u_temporalParams.y);
    vec2 historyUV = coverageEdge ? uv + u_temporalJitter.xy - u_temporalJitter.zw : uv;
    if (any(lessThan(historyUV, vec2(0))) || any(greaterThan(historyUV, vec2(1)))) { fragColor = current; return; }
    vec4 historySample = texture(u_history, historyUV);
    vec4 bounded = clamp(vec4(toYCoCg(historySample.rgb), historySample.a), low, high);
    vec3 history = fromYCoCg(bounded.rgb);
    float motion = length((historyUV - v_uv) / texel);
    float change = abs(toYCoCg(history).x - toYCoCg(current.rgb).x);
    float reactive = waterAt(world.xyz) ? 0.75 : 0.0;
    float weight = u_temporalParams.x * exp(-motion * 0.05) / (coverageEdge ? 1.0 : 1.0 + change * 8.0) * (1.0 - reactive * 0.85);
    fragColor = vec4(mix(current.rgb, history, weight), mix(current.a, bounded.a, weight));
}`;

// The velocity pass. GOSX_SKINNED adds the current and previous joint palettes
// (the same u_modelMatrix * skin order as SCENE_PBR_SKINNED_VERTEX_SOURCE).
const SCENE_WEBGL_MOTION_VERTEX_SOURCE = `
layout(location=0) in vec3 a_position;
layout(location=1) in vec4 a_joints;
layout(location=2) in vec4 a_weights;
uniform mat4 u_current;
uniform mat4 u_previous;
#ifdef GOSX_SKINNED
uniform mat4 u_joints[64];
uniform mat4 u_previousJoints[64];
#endif
out vec4 v_current;
out vec4 v_previous;
void main() {
    vec4 p = vec4(a_position, 1.0), q = p;
#ifdef GOSX_SKINNED
    ivec4 j = ivec4(a_joints);
    p = (a_weights.x * u_joints[j.x] + a_weights.y * u_joints[j.y] + a_weights.z * u_joints[j.z] + a_weights.w * u_joints[j.w]) * p;
    q = (a_weights.x * u_previousJoints[j.x] + a_weights.y * u_previousJoints[j.y] + a_weights.z * u_previousJoints[j.z] + a_weights.w * u_previousJoints[j.w]) * q;
#endif
    v_current = u_current * p;
    v_previous = u_previous * q;
    gl_Position = v_current;
}`;
const SCENE_WEBGL_MOTION_FRAGMENT_SOURCE = `#version 300 es
precision highp float;
in vec4 v_current;
in vec4 v_previous;
out vec4 fragColor;
void main() { fragColor = vec4((v_previous.xy / v_previous.w - v_current.xy / v_current.w) * 0.5, v_current.w, 1.0); }`;

// sceneWebGLTAAWaterRegions packs the ocean band and up to four pools, as
// sceneWebGPUTAAWaterRegions does for the WebGPU resolve.
function sceneWebGLTAAWaterRegions(bundle: any, out: Float32Array) {
  var ocean = bundle && bundle.environment && bundle.environment.ocean;
  out[0] = ocean ? sceneNumber(ocean.level, 0) : 0;
  out[1] = ocean ? Math.max(0.05, sceneNumber(ocean.waveHeight, 0.8) * 0.75) : 0;
  out[2] = ocean ? 1 : 0;
  out[3] = 0;
  var pools = Array.isArray(bundle && bundle.waterSystems) ? bundle.waterSystems : [];
  for (var k = 0; k < 4; k++) {
    var pool = pools[k], base = 4 + k * 4;
    out[base] = pool ? Math.max(0.001, sceneNumber(pool.poolWidth, 1)) : 0;
    out[base + 1] = pool ? Math.max(0.001, sceneNumber(pool.poolLength, 1)) : 0;
    out[base + 2] = pool ? 0.2 * Math.max(0.001, sceneNumber(pool.poolHeight, 1)) + 0.02 : 0;
    out[base + 3] = pool ? 1 : 0;
  }
}

// sceneWebGLMotionProgram compiles the rigid or skinned velocity program once.
// Two 64-joint palettes need 512 vertex uniform vectors; smaller GPUs skip
// skinned movers.
function sceneWebGLMotionProgram(st: any, skinned: boolean) {
  var gl = st.gl, key = skinned ? "skinned" : "rigid";
  if (st.programs[key] !== undefined) return st.programs[key];
  if (skinned && !(gl.getParameter(gl.MAX_VERTEX_UNIFORM_VECTORS) >= 528)) return st.programs[key] = null;
  var vertex = "#version 300 es\n" + (skinned ? "#define GOSX_SKINNED 1\n" : "") + SCENE_WEBGL_MOTION_VERTEX_SOURCE;
  return st.programs[key] = scenePBRCreateProgramInfo(gl, vertex, SCENE_WEBGL_MOTION_FRAGMENT_SOURCE, "Motion vector shader", function(p: any) {
    return { uniforms: { current: gl.getUniformLocation(p, "u_current"), previous: gl.getUniformLocation(p, "u_previous"), joints: gl.getUniformLocation(p, "u_joints[0]"), previousJoints: gl.getUniformLocation(p, "u_previousJoints[0]") } };
  });
}

function sceneWebGLMotionRelease(st: any) {
  var gl = st.gl, t = st.target;
  if (t) { gl.deleteFramebuffer(t.fbo); gl.deleteTexture(t.texture); gl.deleteRenderbuffer(t.depth); }
  st.target = null; st.width = st.height = 0;
}

// sceneWebGLMotionTarget returns the RGBA16F velocity target with its own depth.
function sceneWebGLMotionTarget(st: any, w: number, h: number) {
  if (st.target && st.width === w && st.height === h) return st.target;
  sceneWebGLMotionRelease(st);
  var gl = st.gl, texture = gl.createTexture(), depth = gl.createRenderbuffer(), fbo = gl.createFramebuffer();
  st.width = w; st.height = h;
  gl.bindTexture(gl.TEXTURE_2D, texture);
  gl.texStorage2D(gl.TEXTURE_2D, 1, gl.RGBA16F, w, h);
  for (var p of [gl.TEXTURE_MIN_FILTER, gl.TEXTURE_MAG_FILTER]) gl.texParameteri(gl.TEXTURE_2D, p, gl.NEAREST);
  for (var q of [gl.TEXTURE_WRAP_S, gl.TEXTURE_WRAP_T]) gl.texParameteri(gl.TEXTURE_2D, q, gl.CLAMP_TO_EDGE);
  gl.bindRenderbuffer(gl.RENDERBUFFER, depth);
  gl.renderbufferStorage(gl.RENDERBUFFER, gl.DEPTH_COMPONENT24, w, h);
  gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
  gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, texture, 0);
  gl.framebufferRenderbuffer(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.RENDERBUFFER, depth);
  return st.target = { texture: texture, depth: depth, fbo: fbo };
}

// sceneWebGLMotionRecord stores this frame's model matrix and joint palette,
// keeps last frame's beside them, and reports whether either changed. A first
// sighting only records.
function sceneWebGLMotionRecord(st: any, obj: any, model: ArrayLike<number>, joints: Float32Array | null): any {
  var size = joints ? Math.min(joints.length, 1024) : 0, record = st.records.get(obj.id);
  if (!record) {
    st.records.set(obj.id, { model: Float32Array.from(Array.prototype.slice.call(model, 0, 16)), previous: new Float32Array(16), joints: joints ? joints.slice(0, size) : null, previousJoints: null, seen: st.frame });
    return null;
  }
  record.seen = st.frame;
  record.previous.set(record.model);
  for (var k = 0; k < 16; k++) record.model[k] = model[k];
  var moved = record.previous.some(function(v: number, i: number) { return v !== record.model[i]; });
  if (!joints) { record.previousJoints = null; return moved ? record : null; }
  var reuse = record.previousJoints && record.previousJoints.length === size ? record.previousJoints : new Float32Array(size);
  record.previousJoints = record.joints && record.joints.length === size ? record.joints : joints.slice(0, size);
  reuse.set(joints.subarray(0, size));
  record.joints = reuse;
  return moved || record.joints.some(function(v: number, i: number) { return v !== record.previousJoints[i]; }) ? record : null;
}

// sceneWebGLMotionCollect returns the retained and skinned meshes that moved
// since last frame and forgets objects that left the scene.
function sceneWebGLMotionCollect(st: any, objects: any[]) {
  var movers: any[] = [];
  st.frame += 1;
  for (var i = 0; i < objects.length; i++) {
    var obj = objects[i];
    if (!obj || obj.viewCulled || !obj.id || !obj.directVertices) continue;
    var joints = objectIsSkinned(obj) && obj.skin.jointMatrices instanceof Float32Array && obj.skin.jointMatrices.length >= 16 ? obj.skin.jointMatrices : null;
    if (!obj.retainedGeometry && !joints) continue;
    var record = sceneWebGLMotionRecord(st, obj, obj.modelMatrix && obj.modelMatrix.length >= 16 ? obj.modelMatrix : SCENE_WEBGL_MOTION_IDENTITY, joints);
    if (record) movers.push({ obj: obj, record: record });
  }
  st.records.forEach(function(record: any, id: any) { if (record.seen !== st.frame) st.records.delete(id); });
  return movers;
}

// sceneWebGLMotionDrawOne draws one mover with its current and previous
// transforms (and joint palettes). It returns false when the mover cannot draw.
function sceneWebGLMotionDrawOne(st: any, item: any, helpers: any) {
  var gl = st.gl, obj = item.obj, record = item.record, skinned = !!record.previousJoints;
  var info = sceneWebGLMotionProgram(st, skinned), count = Math.max(0, Math.floor(sceneNumber(obj.vertexCount, 0)));
  if (!info || !scenePBRProgramReady(gl, info.program)) return false;
  if (!helpers.attribute(obj, "positions", 0, 3, helpers.data(obj.vertices, "positions", count, 3))) return false;
  if (skinned && !(helpers.attribute(obj, "joints", 1, 4, helpers.data(obj.vertices, "joints", count, 4)) && helpers.attribute(obj, "weights", 2, 4, helpers.data(obj.vertices, "weights", count, 4)))) return false;
  gl.useProgram(info.program);
  sceneMat4MultiplyInto(st.current, st.currentVP, record.model); sceneMat4MultiplyInto(st.previous, st.previousVP, record.previous);
  gl.uniformMatrix4fv(info.uniforms.current, false, st.current); gl.uniformMatrix4fv(info.uniforms.previous, false, st.previous);
  if (skinned) { gl.uniformMatrix4fv(info.uniforms.joints, false, record.joints); gl.uniformMatrix4fv(info.uniforms.previousJoints, false, record.previousJoints); }
  var indexCount = helpers.indices(obj);
  if (indexCount > 0) gl.drawElements(gl.TRIANGLES, indexCount, gl.UNSIGNED_INT, 0); else gl.drawArrays(gl.TRIANGLES, 0, count);
  return true;
}

// sceneWebGLMotionDraw renders the movers into the velocity target and puts
// back the blend, cull and depth-test switches it changed.
function sceneWebGLMotionDraw(st: any, movers: any[], helpers: any, w: number, h: number): number {
  var gl = st.gl, t = sceneWebGLMotionTarget(st, w, h), drawn = 0;
  var blend = gl.isEnabled(gl.BLEND), cull = gl.isEnabled(gl.CULL_FACE), depthTest = gl.isEnabled(gl.DEPTH_TEST);
  gl.bindFramebuffer(gl.FRAMEBUFFER, t.fbo); gl.viewport(0, 0, w, h);
  gl.disable(gl.BLEND); gl.disable(gl.CULL_FACE); gl.enable(gl.DEPTH_TEST); gl.depthFunc(gl.LESS); gl.depthMask(true);
  gl.clearColor(0, 0, 0, 0); gl.clearDepth(1); gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);
  if (!st.vao) st.vao = gl.createVertexArray();
  gl.bindVertexArray(st.vao);
  for (var m = 0; m < movers.length; m++) if (sceneWebGLMotionDrawOne(st, movers[m], helpers)) drawn += 1;
  gl.bindVertexArray(null);
  if (blend) gl.enable(gl.BLEND);
  if (cull) gl.enable(gl.CULL_FACE);
  if (!depthTest) gl.disable(gl.DEPTH_TEST);
  return drawn;
}

// sceneWebGLMotionUpdate runs once per TAA frame after the scene draws. It
// packs the water regions, updates the mover records, and, when helpers is
// set (the frame has history), draws the velocity pass. Projections are the
// jittered ones the resolve reprojects with.
function sceneWebGLMotionUpdate(st: any, bundle: any, helpers: any, projection: Float32Array, view: Float32Array, previousProjection: Float32Array, previousView: Float32Array, size: number[]) {
  sceneWebGLTAAWaterRegions(bundle, st.water);
  var movers = sceneWebGLMotionCollect(st, (bundle && bundle.meshObjects) || []);
  sceneMat4MultiplyInto(st.currentVP, projection, view); sceneMat4MultiplyInto(st.previousVP, previousProjection, previousView);
  var drawn = movers.length && helpers ? sceneWebGLMotionDraw(st, movers, helpers, size[0], size[1]) : 0;
  st.active = drawn > 0;
  var gl = st.gl;
  if (scenePBRRenderTruth().enabled() && gl.canvas && gl.canvas.parentNode) gl.canvas.parentNode.setAttribute("data-gosx-scene3d-render-taa-movers", String(drawn));
}

// sceneWebGLMotionBindResolve binds the velocity target to texture unit 4 and
// sets the motion and water uniforms on the resolve program.
function sceneWebGLMotionBindResolve(st: any, program: any) {
  var gl = st.gl;
  gl.activeTexture(gl.TEXTURE0 + 4); gl.bindTexture(gl.TEXTURE_2D, st.active && st.target ? st.target.texture : null);
  gl.uniform1i(sceneWaterUniformLocation(gl, program, "u_velocity"), 4);
  gl.uniform4f(sceneWaterUniformLocation(gl, program, "u_motion"), st.active ? 1 : 0, 0, 0, 0);
  gl.uniform4fv(sceneWaterUniformLocation(gl, program, "u_ocean"), st.ocean);
  gl.uniform4fv(sceneWaterUniformLocation(gl, program, "u_pools[0]"), st.pools);
}

const SCENE_WEBGL_MOTION_IDENTITY = new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);

// createSceneWebGLMotionVectors owns the velocity target and the per-object
// records of last frame's model matrix and joint palette. helpers comes from
// the PBR renderer: { attribute, data, indices } bind an object's cached
// direct streams and authored indices.
function createSceneWebGLMotionVectors(gl: any) {
  var water = new Float32Array(20);
  var st: any = { gl: gl, records: new Map(), programs: {}, target: null, vao: null, width: 0, height: 0, frame: 0, active: false,
    current: new Float32Array(16), previous: new Float32Array(16), currentVP: new Float32Array(16), previousVP: new Float32Array(16),
    water: water, ocean: water.subarray(0, 4), pools: water.subarray(4, 20) };
  return {
    begin: function() { st.active = false; },
    update: function(bundle: any, helpers: any, projection: Float32Array, view: Float32Array, previousProjection: Float32Array, previousView: Float32Array, size: number[]) {
      sceneWebGLMotionUpdate(st, bundle, helpers, projection, view, previousProjection, previousView, size);
    },
    bindResolve: function(program: any) { sceneWebGLMotionBindResolve(st, program); },
    reset: function() { st.records.clear(); st.active = false; },
    dispose: function() { sceneWebGLMotionRelease(st); st.records.clear(); if (st.vao) gl.deleteVertexArray(st.vao); st.vao = null; st.programs = {}; },
  };
}
