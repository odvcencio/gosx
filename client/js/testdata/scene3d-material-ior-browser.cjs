'use strict';
/* Browser-verification probe for the Scene3D authored material IOR feature.
 * Real Chrome + real WebGL2/WebGPU PBR + real glTF loading through the built
 * client/js/bootstrap.js. Node builtin-only (Node >= 22 has global WebSocket),
 * no npm dependencies, no production edits, no generated assets checked in.
 *
 * Run: node scene3d-material-ior-browser.cjs <repoRoot> [artifactDir]
 * Env: GOSX_IOR_REQUIRE_WEBGPU=1 -> WebGPU is REQUIRED: any adapter
 *      unavailability, skip, or production renderer fallback is a hard
 *      failure (never a warning or silent skip). Without the env var a
 *      genuine, explicitly-reasoned adapter-unavailable skip is allowed for
 *      the WebGPU cases only.
 *
 * Evidence gathered per case (one sequential browser page/scene at a time,
 * fixed camera/lighting/FOV, no animation):
 *  - native u_dielectricF0 uniform values observed at production GL draw
 *    calls (getUniformLocation program->location tracking + getParameter
 *    CURRENT_PROGRAM + getUniform at draw time, instanced forms included),
 *    and the 176-byte WebGPU material upload with F0 read at exactly float
 *    index 40 (bytes 160:164); all wrappers strictly forward and observation
 *    errors are recorded and fail the probe without changing native behavior;
 *  - actual rendered pixels via CDP screenshot clipped to the real canvas
 *    bounding rect, decoded with a native browser Image + 2D canvas, with
 *    foreground-vs-measured-corner-background threshold + coverage asserted
 *    in ALL cases (including IOR 0 / F0 1);
 *  - omitted vs explicit ior 1.5 both F0 0.04 with zero changed bytes/pixels;
 *  - ior 1 -> 0, 1.33/2.42/>5 -> ((ior-1)/(ior+1))^2, explicit 0 -> 1;
 *  - fully metallic images invariant to IOR while uniforms stay distinct;
 *  - real GLB with KHR_materials_ior 2.42, model override 1.33, omitted
 *    instancedGLB batch preserving loaded 2.42, explicit zero batch override;
 *  - named-material table reference; real CSS var(--ior) 1.33 -> 2.42 change
 *    via documentElement.style.setProperty with observed revision advance,
 *    new uniform value and changed pixels (no remount, no manual writes);
 *  - dispose removes scene state; bounded waits; overall 3m watchdog;
 *  - graceful+bounded Chrome teardown, CDP/server close, owned tmp profile
 *    removal on success/failure/timeout.
 * GPU hardware acceleration type is not certified (SwiftShader possible).
 */

const fs = require('fs'), os = require('os'), path = require('path');
const http = require('http');
const { spawn } = require('child_process');

const REPO = process.argv[2];
if (!REPO) {
  console.error('usage: node scene3d-material-ior-browser.cjs <repoRoot> [artifactDir]');
  process.exit(2);
}
const BOOTSTRAP = path.join(REPO, 'client', 'js', 'bootstrap.js');
if (!fs.existsSync(BOOTSTRAP)) {
  console.error('missing built runtime: ' + BOOTSTRAP);
  process.exit(2);
}
// The production Scene3D WebGPU path lazily loads its feature chunk from
// /gosx/bootstrap-feature-scene3d-webgpu.js (fallback URL in the built
// bootstrap). Serve the real built chunk like the real production origin.
const WG_CHUNK = path.join(REPO, 'client', 'js', 'bootstrap-feature-scene3d-webgpu.js');
if (!fs.existsSync(WG_CHUNK)) {
  console.error('missing built runtime asset: ' + WG_CHUNK);
  process.exit(2);
}
const ART = process.argv[3] || null;
if (ART && (!fs.existsSync(ART) || !fs.statSync(ART).isDirectory())) {
  console.error('artifactDir, if supplied, must be an existing directory: ' + ART);
  process.exit(2);
}
const REQUIRE_WGPU = process.env.GOSX_IOR_REQUIRE_WEBGPU === '1';

const errors = [], warnings = [];
const fail = (m) => { errors.push(m); };
const F0 = (ior) => ((ior - 1) / (ior + 1)) * ((ior - 1) / (ior + 1));
const sleep = (ms) => new Promise((res) => setTimeout(res, ms));

const ENGINE = 'gosx-engine-ior-browser';
const MOUNT = 'scene-ior-browser';
const W = 256, H = 192;
const OVERALL_MS = 180000;
const CASE_WAIT_MS = 20000;
const SETTLE_MS = 600;
const FG_THRESHOLD = 12;   // min channel delta vs measured corner background
const FG_COVERAGE = 0.01;  // min fraction of foreground pixels

// ---- GLB fixture: one quad facing +Z, positions + normals, metallic 0 ----
function buildQuadGLB(withIor) {
  const pos = new Float32Array([-1, -1, 0, 1, -1, 0, 1, 1, 0, -1, 1, 0]);
  const nrm = new Float32Array([0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1]);
  const idx = new Uint16Array([0, 1, 2, 0, 2, 3]);
  const parts = []; const views = []; let off = 0;
  function addView(typed, target) {
    const bytes = Buffer.from(typed.buffer, typed.byteOffset, typed.byteLength);
    parts.push(bytes);
    views.push({ buffer: 0, byteOffset: off, byteLength: bytes.length, target });
    off += bytes.length;
    const pad = (4 - (off % 4)) % 4;
    if (pad) { parts.push(Buffer.alloc(pad)); off += pad; }
    return views.length - 1;
  }
  const pv = addView(pos, 34962), nv = addView(nrm, 34962), iv = addView(idx, 34963);
  const bin = Buffer.concat(parts);
  const material = {
    pbrMetallicRoughness: {
      baseColorFactor: [0.69, 0.31, 0.24, 1],
      metallicFactor: 0, // glTF 2.0 spelling (metalnessFactor is invalid and
                         // silently loads as metallic 1 in strict loaders)
      roughnessFactor: 0.35,
    },
  };
  if (withIor) {
    material.extensions = { KHR_materials_ior: { ior: 2.42 } };
  }
  const json = {
    asset: { version: '2.0', generator: 'scene3d-material-ior-browser probe' },
    scene: 0, scenes: [{ nodes: [0] }], nodes: [{ mesh: 0, name: 'quad' }],
    meshes: [{ name: 'quad', primitives: [{ attributes: { POSITION: pv, NORMAL: nv }, indices: iv, mode: 4, material: 0 }] }],
    materials: [material], accessors: [
      { bufferView: pv, componentType: 5126, count: 4, type: 'VEC3', min: [-1, -1, 0], max: [1, 1, 0] },
      { bufferView: nv, componentType: 5126, count: 4, type: 'VEC3', min: [0, 0, 1], max: [0, 0, 1] },
      { bufferView: iv, componentType: 5123, count: 6, type: 'SCALAR', min: [0], max: [3] },
    ], bufferViews: views, buffers: [{ byteLength: bin.length }],
  };
  if (withIor) json.extensionsUsed = ['KHR_materials_ior'];
  let jsonBuf = Buffer.from(JSON.stringify(json), 'utf8');
  const jp = (4 - (jsonBuf.length % 4)) % 4;
  if (jp) jsonBuf = Buffer.concat([jsonBuf, Buffer.alloc(jp, 0x20)]);
  const bp = (4 - (bin.length % 4)) % 4;
  const binP = bp ? Buffer.concat([bin, Buffer.alloc(bp)]) : bin;
  const header = Buffer.alloc(12);
  header.writeUInt32LE(0x46546C67, 0); header.writeUInt32LE(2, 4);
  header.writeUInt32LE(12 + 8 + jsonBuf.length + 8 + binP.length, 8);
  const jh = Buffer.alloc(8); jh.writeUInt32LE(jsonBuf.length, 0); jh.writeUInt32LE(0x4E4F534A, 4);
  const bh = Buffer.alloc(8); bh.writeUInt32LE(binP.length, 0); bh.writeUInt32LE(0x004E4942, 4);
  return Buffer.concat([header, jh, jsonBuf, bh, binP]);
}
const glb242 = buildQuadGLB(true);

// ---- Case table (one object/scene per page; sequential, never batched) ----
// Explicit unindexed quad mesh (6 triangle vertices). A bare kind:'box' would
// also generate primitive geometry in normalizeSceneObject, but this fixture
// supplies explicit, identical triangle data (positions/normals/uvs/tangents)
// to isolate material behavior and keep cross-case pixel comparisons stable;
// only IOR/material inputs vary.
const QUAD_VERTICES = (function () {
  const positions = [
    -0.6, -0.6, 0, 0.6, -0.6, 0, 0.6, 0.6, 0,
    -0.6, -0.6, 0, 0.6, 0.6, 0, -0.6, 0.6, 0,
  ];
  const normals = [];
  const uvs = [0, 0, 1, 0, 1, 1, 0, 0, 1, 1, 0, 1];
  const tangents = [];
  for (let i = 0; i < 6; i += 1) {
    normals.push(0, 0, 1);
    tangents.push(1, 0, 0, 1);
  }
  return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, count: 6 };
})();
const OBJ = (extra) => Object.assign({ id: 'probe', kind: 'box', materialKind: 'standard',
  wireframe: false, color: '#b0503c', roughness: 0.35, metalness: 0,
  vertices: QUAD_VERTICES }, extra);
const OBJNAMED = { id: 'probe', kind: 'box', materialKind: 'standard', wireframe: false,
  material: 'dielectric', roughness: 0.35, metalness: 0, vertices: QUAD_VERTICES };
const MODEL = (extra) => Object.assign({ id: 'quad', src: '/models/quad242.glb', static: true }, extra);
const BATCH = (extra) => Object.assign({ id: 'batch', src: '/models/quad242.glb',
  materialKind: 'standard', roughness: 0.35, metalness: 0,
  instances: [{ id: 'i0', x: 0, y: 0, z: 0 }] }, extra);
const WG = (c) => Object.assign({ webgpu: true }, c);

const CASES = [
  { name: 'obj-omitted', obj: OBJ({}), f0: 0.04, base: 'omit' },
  { name: 'obj-ior15', obj: OBJ({ ior: 1.5 }), f0: 0.04, same: 'obj-omitted' },
  { name: 'obj-ior1', obj: OBJ({ ior: 1 }), f0: 0, differs: 'obj-omitted', minChanged: 1 },
  { name: 'obj-ior133', obj: OBJ({ ior: 1.33 }), f0: F0(1.33), base: 'd133' },
  { name: 'obj-ior242', obj: OBJ({ ior: 2.42 }), f0: F0(2.42), differs: 'obj-ior133', minChanged: 50 },
  { name: 'obj-ior10', obj: OBJ({ ior: 10 }), f0: F0(10), differs: 'obj-ior133', minChanged: 50 },
  { name: 'obj-ior0', obj: OBJ({ ior: 0 }), f0: 1, differs: 'obj-ior133', minChanged: 50 },
  { name: 'obj-metal133', obj: OBJ({ ior: 1.33, metalness: 1 }), f0: F0(1.33), base: 'm133' },
  { name: 'obj-metal242', obj: OBJ({ ior: 2.42, metalness: 1 }), f0: F0(2.42), same: 'obj-metal133' },
  { name: 'glb-khr242', model: MODEL({}), f0: F0(2.42), base: 'g242' },
  { name: 'glb-override133', model: MODEL({ ior: 1.33 }), f0: F0(1.33), differs: 'glb-khr242', minChanged: 50 },
  { name: 'glb-batch-omit', instanced: BATCH({}), f0: F0(2.42), base: 'b242' },
  { name: 'glb-batch-zero', instanced: BATCH({ ior: 0 }), f0: 1, differs: 'glb-batch-omit', minChanged: 50 },
  { name: 'named-material', materials: [{ id: 'dielectric', materialKind: 'standard',
    roughness: 0.35, metalness: 0, ior: 2.42, color: '#b0503c' }], obj: OBJNAMED,
    f0: F0(2.42), base: 'n242' },
  { name: 'css-var', cssVar: true, obj: OBJ({ ior: 'var(--ior)' }), f0: F0(1.33), base: 'cssvar' },
  WG({ name: 'wg-omit', obj: OBJ({}), f0: 0.04, base: 'womit' }),
  WG({ name: 'wg-ior15', obj: OBJ({ ior: 1.5 }), f0: 0.04, same: 'wg-omit' }),
  WG({ name: 'wg-ior133', obj: OBJ({ ior: 1.33 }), f0: F0(1.33), base: 'wd133' }),
  WG({ name: 'wg-ior242', obj: OBJ({ ior: 2.42 }), f0: F0(2.42), differs: 'wg-ior133', minChanged: 50 }),
  WG({ name: 'wg-metal133', obj: OBJ({ ior: 1.33, metalness: 1 }), f0: F0(1.33), base: 'wm133' }),
  WG({ name: 'wg-metal242', obj: OBJ({ ior: 2.42, metalness: 1 }), f0: F0(2.42), same: 'wg-metal133' }),
];
const byName = {};
CASES.forEach((c) => { byName[c.name] = c; });

function propsFor(c) {
  const p = { width: W, height: H, autoRotate: false, animation: false,
    responsive: false, maxDevicePixelRatio: 1,
    forceWebGL: !c.webgpu, requireWebGL: !c.webgpu,
    preferWebGPU: Boolean(c.webgpu),
    background: '#101418',
    camera: { x: 0, y: 0, z: 4, fov: 50 },
    lights: [{ id: 'key', kind: 'directional', intensity: 1.2,
      directionX: 0, directionY: 0, directionZ: -1 }] };
  if (c.materials) p.materials = c.materials;
  if (c.obj) p.objects = [c.obj];
  if (c.model) p.models = [c.model];
  if (c.instanced) p.instancedGLBMeshes = [c.instanced];
  return p;
}

function htmlFor(c) {
  const manifest = JSON.stringify({ engines: [{ id: ENGINE, component: 'GoSXScene3D',
    kind: 'surface', mountId: MOUNT, props: propsFor(c) }] });
  return '<!doctype html><html><head><meta charset="utf-8">' +
    '<style>:root{--ior:1.33;}' +
    'html,body{margin:0;padding:0;background:#101418;overflow:hidden;' +
    'width:' + W + 'px;height:' + H + 'px;}' +
    '#' + MOUNT + '{width:' + W + 'px;height:' + H + 'px;overflow:hidden;}' +
    'canvas{display:block;}</style></head><body>' +
    '<div id="' + MOUNT + '" width="' + W + '" height="' + H + '"></div>' +
    '<script type="application/json" id="gosx-manifest">' + manifest + '</script>' +
    '<script src="/bootstrap.js"></script></body></html>';
}

let server = http.createServer((req, res) => {
  if (req.url === '/models/quad242.glb') {
    res.writeHead(200, { 'content-type': 'model/gltf-binary', 'content-length': glb242.length });
    res.end(glb242);
  } else if (req.url === '/bootstrap.js' || req.url === '/client/js/bootstrap.js') {
    const js = fs.readFileSync(BOOTSTRAP);
    res.writeHead(200, { 'content-type': 'text/javascript', 'content-length': js.length });
    res.end(js);
  } else if (req.url === '/gosx/bootstrap-feature-scene3d-webgpu.js') {
    const js = fs.readFileSync(WG_CHUNK);
    res.writeHead(200, { 'content-type': 'text/javascript', 'content-length': js.length });
    res.end(js);
  } else if (req.url === '/' || (req.url && req.url.indexOf('/?') === 0)) {
    // Valid served loopback HTTP origin used before requestAdapter probing.
    res.writeHead(200, { 'content-type': 'text/html' });
    res.end('<!doctype html><html><head><meta charset="utf-8"><title>probe origin</title></head><body>probe-origin</body></html>');
  } else if (req.url && req.url.indexOf('/case/') === 0) {
    const name = req.url.slice('/case/'.length).split('?')[0];
    const c = CASES.find((x) => x.name === name);
    if (!c) { res.writeHead(404); res.end(); return; }
    res.writeHead(200, { 'content-type': 'text/html' });
    res.end(htmlFor(c));
  } else { res.writeHead(404); res.end(); }
});

// ---- Owned resources + central cleanup (normal / error / watchdog) ----
let ws = null, chrome = null, profile = null, port = null, BASE = null;
let msgId = 0, cleaned = false, finished = false, printed = false, exitCode = 0;
const pending = new Map(), listeners = [];

function emit(obj) {
  if (printed) return;
  printed = true;
  console.log(JSON.stringify(obj, null, 2));
}

async function cleanup(immediate) {
  if (cleaned) return;
  cleaned = true;
  try { if (ws) ws.close(); } catch (e) {}
  ws = null;
  if (chrome) {
    const ch = chrome; chrome = null;
    const exited = new Promise((res) => { try { ch.once('exit', () => res()); } catch (e) { res(); } });
    try { ch.kill(immediate ? 'SIGKILL' : 'SIGTERM'); } catch (e) {}
    const graceful = await Promise.race([exited.then(() => true), sleep(3000).then(() => false)]);
    if (!graceful) {
      try { ch.kill('SIGKILL'); } catch (e) {}
      await Promise.race([exited, sleep(2000)]);
    }
  }
  if (profile) {
    // Only ever remove a profile we created via mkdtemp with our prefix.
    const prefix = path.join(os.tmpdir(), 'gosx-ior-probe-');
    if (typeof profile === 'string' && profile.indexOf(prefix) === 0) {
      try { fs.rmSync(profile, { recursive: true, force: true }); }
      catch (e) { warnings.push('profile cleanup skipped: ' + e.message); }
    }
    profile = null;
  }
  if (server && server.listening) {
    await Promise.race([
      new Promise((res) => { try { server.close(() => res()); } catch (e) { res(); } }),
      sleep(2000),
    ]);
  }
}

// ---- CDP plumbing (bounded, strict) ----
function cdpSend(method, params, sessionId, timeoutMs) {
  if (!ws) return Promise.reject(new Error('CDP connection closed'));
  const id = ++msgId;
  return new Promise((resolve, reject) => {
    const t = setTimeout(() => { pending.delete(id); reject(new Error('CDP timeout: ' + method)); },
      timeoutMs || 15000);
    pending.set(id, { resolve, reject, t });
    try {
      ws.send(JSON.stringify(Object.assign({ id, method }, params ? { params } : {},
        sessionId ? { sessionId } : {})));
    } catch (e) { clearTimeout(t); pending.delete(id); reject(e); }
  });
}
function waitForEvent(name, timeoutMs) {
  return new Promise((resolve, reject) => {
    const entry = { name, resolve, timer: setTimeout(() => {
      const i = listeners.indexOf(entry); if (i >= 0) listeners.splice(i, 1);
      reject(new Error('event timeout: ' + name)); }, timeoutMs || 15000) };
    listeners.push(entry);
  });
}
function dispatch(raw) {
  let m;
  try { m = JSON.parse(raw); } catch (e) { return; }
  if (m.id && pending.has(m.id)) {
    const p = pending.get(m.id); pending.delete(m.id); clearTimeout(p.t);
    if (m.error) p.reject(new Error(m.error.message));
    else if (m.result && m.result.exceptionDetails) {
      const d = m.result.exceptionDetails;
      p.reject(new Error('Runtime.evaluate exception: ' + ((d.exception && d.exception.description) || d.text)));
    } else p.resolve(m.result);
  } else if (m.method) {
    for (let i = listeners.length - 1; i >= 0; i -= 1) {
      if (listeners[i].name === m.method) {
        const e = listeners[i]; clearTimeout(e.timer); listeners.splice(i, 1); e.resolve(m.params || {});
      }
    }
    if (m.method === 'Runtime.consoleAPICalled' && m.params && m.params.args) {
      const text = m.params.args.map((x) => x.value || x.description || '').join(' ');
      if (m.params.type === 'error') errors.push('console.error: ' + text);
      else if (m.params.type === 'warning') warnings.push(text);
    }
    if (m.method === 'Runtime.exceptionThrown' && m.params && m.params.exceptionDetails) {
      errors.push('page exception: ' + ((m.params.exceptionDetails.exception &&
        m.params.exceptionDetails.exception.description) || m.params.exceptionDetails.text));
    }
  }
}

async function evalSend(send, expression, extra) {
  const r = await send('Runtime.evaluate', Object.assign({ expression, returnByValue: true }, extra || {}));
  return r && r.result && r.result.value;
}

// Strict wrappers only: every wrapped native forwards arguments/result/this
// unchanged. WebGL observation reads the REAL uniform state at draw time:
// getUniformLocation tracks program->location for u_dielectricF0; at each
// draw (including instanced forms) we read CURRENT_PROGRAM and getUniform.
// Nothing is inferred from uniform1f/useProgram. GPUQueue.writeBuffer is
// wrapped with its true signature (buffer, bufferOffset, data, dataOffset?,
// size?) with correct element/byte dataOffset+size semantics, capturing only
// 176-byte material uploads and reading F0 at exactly float index 40.
const PRELOAD = `
  window.__gosxIOR = { draws: 0, pbrDraws: 0, lastDrawF0: null, f0s: [], obsErrors: [], gl: null,
    programInfo: null, queriedUniforms: [] };
window.__gosxWGPU = { materialUploads: 0, dumps: [], obsErrors: [] };
(function () {
  function noteErr(store, e) {
    if (store.length < 16) store.push(String((e && e.message) || e));
  }
  var W1 = (typeof WebGLRenderingContext !== "undefined") ? WebGLRenderingContext.prototype : null;
  var W2 = (typeof WebGL2RenderingContext !== "undefined") ? WebGL2RenderingContext.prototype : null;
  function observeDraw() {
    window.__gosxIOR.draws += 1;
    window.__gosxIOR.gl = (typeof WebGL2RenderingContext !== "undefined" &&
      this instanceof WebGL2RenderingContext) ? "webgl2" : "webgl";
    try {
      var cp = this.__origGetParameter.call(this, this.CURRENT_PROGRAM);
      if (cp && !window.__gosxIOR.programInfo) {
        // First actual draw: enumerate the real uniforms of the current
        // program via the native getActiveUniform and read its true
        // LINK_STATUS. All values come from the original native calls.
        var info = { linkStatus: null, activeUniforms: [], trackedF0: false };
        try {
          info.linkStatus = !!this.__origGetProgramParameter.call(this, cp, 0x8B82);
          var ucount = this.__origGetProgramParameter.call(this, cp, 0x8B86);
          var names = [];
          for (var i = 0; i < ucount && names.length < 100; i += 1) {
            var ui = this.__origGetActiveUniform.call(this, cp, i);
            if (ui && ui.name) names.push(String(ui.name));
          }
          info.activeUniforms = names;
        } catch (e2) { noteErr(window.__gosxIOR.obsErrors, e2); }
        var fm = this.__f0locs;
        info.trackedF0 = !!(fm && fm.has(cp));
        window.__gosxIOR.programInfo = info;
      }
      var m = this.__f0locs;
      if (cp && m && m.has(cp)) {
        var v = this.__origGetUniform.call(this, cp, m.get(cp));
        if (typeof v === "number" && Number.isFinite(v)) {
          window.__gosxIOR.pbrDraws += 1;
          window.__gosxIOR.lastDrawF0 = v;
          if (window.__gosxIOR.f0s.length < 4096) window.__gosxIOR.f0s.push(v);
        }
      }
    } catch (e) { noteErr(window.__gosxIOR.obsErrors, e); }
  }
  if (W1) {
    // WebGL1 and WebGL2 are separate interfaces: wrap each prototype once,
    // snapshotting its own natives. All observed F0 comes from the native
    // getUniform at the current program, read inside the draw observer.
    wrap(W1);
  }
  if (W2) {
    wrap(W2);
  }
  function wrap(proto) {
    if (!proto || proto.__gosxIORWrapped) return;
    proto.__gosxIORWrapped = true;
    var gu = proto.getUniformLocation, gp = proto.getParameter, guf = proto.getUniform,
        gpp = proto.getProgramParameter, gau = proto.getActiveUniform,
        da = proto.drawArrays, de = proto.drawElements,
        dai = proto.drawArraysInstanced, dei = proto.drawElementsInstanced;
    proto.__origGetParameter = gp;
    proto.__origGetUniform = guf;
    proto.__origGetProgramParameter = gpp;
    proto.__origGetActiveUniform = gau;
    // Note: stored on the prototype (shared by contexts) is fine because the
    // draw observer receives the context as |this|.
    proto.getUniformLocation = function (p, n) {
      // Strict forwarding: the native is called with the exact original
      // arguments and its return value is passed through unchanged.
      var loc = gu.apply(this, arguments);
      try {
        var q = window.__gosxIOR.queriedUniforms ||
          (window.__gosxIOR.queriedUniforms = []);
        if (q.length < 64 && q.indexOf(String(n)) < 0) q.push(String(n));
        if (n === "u_dielectricF0") {
          var m = this.__f0locs || (this.__f0locs = new Map());
          if (loc) m.set(p, loc); else m.delete(p);
        }
      } catch (e) { noteErr(window.__gosxIOR.obsErrors, e); }
      return loc;
    };
    if (da) proto.drawArrays = function () { observeDraw.call(this); return da.apply(this, arguments); };
    if (de) proto.drawElements = function () { observeDraw.call(this); return de.apply(this, arguments); };
    if (dai) proto.drawArraysInstanced = function () { observeDraw.call(this); return dai.apply(this, arguments); };
    if (dei) proto.drawElementsInstanced = function () { observeDraw.call(this); return dei.apply(this, arguments); };
  }
  if (typeof GPUQueue !== "undefined" && GPUQueue.prototype && GPUQueue.prototype.writeBuffer) {
    var wb = GPUQueue.prototype.writeBuffer;
    GPUQueue.prototype.writeBuffer = function (buffer, bufferOffset, data, dataOffset, size) {
      try {
        if (data && window.__gosxWGPU.dumps.length < 256) {
          // Signature: writeBuffer(buffer, bufferOffset, data, dataOffset?, size?).
          // dataOffset/size are in ELEMENTS for typed arrays, BYTES for
          // ArrayBuffer/ArrayBufferView-without-elements.
          // Signature: writeBuffer(buffer, bufferOffset, data, dataOffset?,
          // size?). dataOffset/size are in ELEMENTS for typed arrays
          // (DataView counts in bytes, element unit 1), BYTES for a bare
          // ArrayBuffer. ArrayBuffer.isView covers both; data.buffer is the
          // underlying storage for views.
          var isView = ArrayBuffer.isView(data);
          var buf = isView ? data.buffer : data;
          var base = isView ? data.byteOffset : 0;
          var elem = data.BYTES_PER_ELEMENT ? data.BYTES_PER_ELEMENT : 1;
          var totalBytes = data.byteLength;
          var byteOff = (dataOffset == null) ? 0 : dataOffset * elem;
          var byteLen = (size == null) ? (totalBytes - byteOff) : size * elem;
          if (byteLen === 176 && byteOff >= 0 && byteOff + 176 <= totalBytes) {
            var dv = new DataView(buf, base + byteOff, 176);
            var floats = new Array(44);
            for (var i = 0; i < 44; i++) floats[i] = dv.getFloat32(i * 4, true);
            // Production writes Float32Array(44); F0 lives at float index 40
            // (bytes 160:164). Slots 0..39 are pre-existing uniform data.
            window.__gosxWGPU.dumps.push({ f0: floats[40], floats: floats });
            window.__gosxWGPU.materialUploads += 1;
          }
        }
      } catch (e) { noteErr(window.__gosxWGPU.obsErrors, e); }
      return wb.apply(this, arguments);
    };
  }
})();
`;

function assertClose(actual, expected, label, tol) {
  const t = tol == null ? 2e-4 : tol;
  if (typeof actual !== 'number' || !Number.isFinite(actual) || Math.abs(actual - expected) >= t) {
    fail(label + ': got ' + actual + ' want ' + expected + ' (+/-' + t + ')');
  }
}

// BATCH hydration yields instancedMeshes (not necessarily objects-map entries),
// so accept either a real object or a real instancedMesh. Real PBR output is
// required: pbrDraws>0, or (WebGPU) materialUploads plus an actually presented
// frame (frame-seq>0) so bare uploads without an eventual draw never pass.
const READY = '(function(){var m=document.getElementById("' + MOUNT + '");' +
  'var s=m&&m.__gosxScene3DState;' +
  'var objs=!!(s&&s.objects&&s.objects.size>0);' +
  'var im=s&&s.instancedMeshes;' +
  'var inst=!!im&&((typeof im.size==="number"&&im.size>0)||(typeof im.length==="number"&&im.length>0));' +
  'var drawn=!!(window.__gosxIOR&&window.__gosxIOR.pbrDraws>0);' +
  'var uploaded=!!(window.__gosxWGPU&&window.__gosxWGPU.materialUploads>0&&' +
  'Number(m.getAttribute("data-gosx-scene3d-webgpu-frame-seq"))>0);' +
  'return !!(s&&(objs||inst)&&(drawn||uploaded));})()';

const READ = '(function(){var m=document.getElementById("' + MOUNT + '");' +
  'return {mounted:m&&m.getAttribute("data-gosx-scene3d-mounted"),' +
  'renderer:m&&m.getAttribute("data-gosx-scene3d-renderer"),' +
  'fallback:m&&m.getAttribute("data-gosx-scene3d-renderer-fallback"),' +
  'frameSeq:m&&m.getAttribute("data-gosx-scene3d-webgpu-frame-seq"),' +
  'meshDraws:m&&m.getAttribute("data-gosx-scene3d-webgpu-mesh-draw-calls"),' +
  'bundleState:m&&m.getAttribute("data-gosx-scene3d-webgpu-bundle-state"),' +
  'bundleEncodes:m&&m.getAttribute("data-gosx-scene3d-webgpu-bundle-encodes"),' +
  'bundleReplays:m&&m.getAttribute("data-gosx-scene3d-webgpu-bundle-replays"),' +
  'bundleDraws:m&&m.getAttribute("data-gosx-scene3d-webgpu-bundle-draws"),' +
  'wgpuErr:m&&m.getAttribute("data-gosx-scene3d-webgpu-last-error"),' +
  'rev:m&&m.__gosxScene3DCSSRevision,' +
  'objects:m&&m.__gosxScene3DState&&m.__gosxScene3DState.objects&&m.__gosxScene3DState.objects.size,' +
  'instances:(function(){var st=m&&m.__gosxScene3DState;var im=st&&st.instancedMeshes;' +
  'if(!im)return 0;' +
  'if(typeof im.size==="number")return im.size;' +
  'if(typeof im.length==="number")return im.length;' +
  'return Object.keys(im).length;})(),' +
  'ior:window.__gosxIOR?{draws:window.__gosxIOR.draws,pbrDraws:window.__gosxIOR.pbrDraws,' +
  'lastDrawF0:window.__gosxIOR.lastDrawF0,gl:window.__gosxIOR.gl,' +
  'linkStatus:(window.__gosxIOR.programInfo&&window.__gosxIOR.programInfo.linkStatus!==null?window.__gosxIOR.programInfo.linkStatus:null),' +
  'trackedF0:!!(window.__gosxIOR.programInfo&&window.__gosxIOR.programInfo.trackedF0),' +
  'activeUniforms:((window.__gosxIOR.programInfo&&window.__gosxIOR.programInfo.activeUniforms)||[]).slice(0,100),' +
  'queriedUniforms:(window.__gosxIOR.queriedUniforms||[]).slice(0,64),' +
  'obsErrors:(window.__gosxIOR.obsErrors||[]).slice(0,4)}:null,' +
  'wgpu:window.__gosxWGPU?{uploads:window.__gosxWGPU.materialUploads,' +
  'dumps:window.__gosxWGPU.dumps.slice(-4),' +
  'obsErrors:(window.__gosxWGPU.obsErrors||[]).slice(0,4)}:null};})()';

// Decode the actual screenshot with a native Image + 2D canvas. Measures the
// real corner background from the image itself, then foreground pixels that
// differ from it by >= FG_THRESHOLD per channel (geometry, not assumption).
function decodeExpr(b64) {
  var expr = 'new Promise(function(res){var img=new Image();' +
    'img.onload=function(){try{var c=document.createElement("canvas");c.width=img.width;c.height=img.height;' +
    'var x=c.getContext("2d");x.drawImage(img,0,0);var d=x.getImageData(0,0,c.width,c.height).data;' +
    'var n=d.length/4;' +
    'var cr=0,cg=0,cb=0,cn=0;' +
    'var corners=[[0,0],[c.width-4,0],[0,c.height-4],[c.width-4,c.height-4]];' +
    'for(var k=0;k<corners.length;k++){for(var dy=0;dy<4;dy++){for(var dx=0;dx<4;dx++){' +
    'var i=((corners[k][1]+dy)*c.width+(corners[k][0]+dx))*4;cr+=d[i];cg+=d[i+1];cb+=d[i+2];cn++;}}}' +
    'var bg=[Math.round(cr/cn),Math.round(cg/cn),Math.round(cb/cn)];' +
    'var fg=0,maxDelta=0,bgPixels=0;' +
    'for(var i=0;i<d.length;i+=4){' +
    'var df=Math.max(Math.abs(d[i]-bg[0]),Math.abs(d[i+1]-bg[1]),Math.abs(d[i+2]-bg[2]));' +
    'if(df>=FG_THRESHOLD){fg++;if(df>maxDelta)maxDelta=df;}' +
    'if(d[i]===bg[0]&&d[i+1]===bg[1]&&d[i+2]===bg[2])bgPixels++;}' +
    'var png=null;try{png=c.toDataURL("image/png").split(",")[1];}catch(e){}' +
    'res({w:c.width,h:c.height,bg:bg,fgPixels:fg,fgFrac:fg/n,maxDelta:maxDelta,bgPixels:bgPixels,png:png});}catch(e){res(null);}};' +
    'img.onerror=function(){res(null);};' +
    'img.src="data:image/png;base64,' + b64 + '";})';
  // Interpolate the threshold into the ENTIRE assembled expression so the
  // browser-side code never references a Node lexical constant.
  return expr.replace(/FG_THRESHOLD/g, String(FG_THRESHOLD));
}

// Compare two plain-base64 PNGs. exactBytes/exactPixels = zero-tolerance
// equality; meanChanged/maxDelta = meaningful-channel difference (>2 / max).
function diffExpr(a, b) {
  return 'new Promise(function(res){var A=new Image(),B=new Image();var n=0;' +
    'function done(){try{if(++n<2)return;' +
    'if(A.width!==B.width||A.height!==B.height){res({dimsMatch:false});return;}' +
    'var c=document.createElement("canvas");c.width=A.width;c.height=A.height;' +
    'var x=c.getContext("2d");x.drawImage(A,0,0);var d1=x.getImageData(0,0,c.width,c.height).data;' +
    'x.clearRect(0,0,c.width,c.height);x.drawImage(B,0,0);var d2=x.getImageData(0,0,c.width,c.height).data;' +
    'var eb=0,ep=0,mp=0,md=0;' +
    'for(var i=0;i<d1.length;i+=4){' +
    'var mx=Math.max(Math.abs(d1[i]-d2[i]),Math.abs(d1[i+1]-d2[i+1]),Math.abs(d1[i+2]-d2[i+2]),Math.abs(d1[i+3]-d2[i+3]));' +
    'if(mx>0){if(d1[i]!==d2[i])eb++;if(d1[i+1]!==d2[i+1])eb++;' +
    'if(d1[i+2]!==d2[i+2])eb++;if(d1[i+3]!==d2[i+3])eb++;ep++;' +
    'if(mx>md)md=mx;if(mx>2){mp++;}}}' +
    'res({dimsMatch:true,exactBytes:eb,exactPixels:ep,meanChanged:mp,maxDelta:md});}catch(e){res(null);}}' +
    'A.onload=B.onload=done;A.onerror=B.onerror=function(){res(null);};' +
    'A.src="data:image/png;base64,' + a + '";B.src="data:image/png;base64,' + b + '";})';
}

async function capture(send) {
  const rect = await evalSend(send,
    '(function(){var m=document.getElementById("' + MOUNT + '");' +
    'var cv=m&&m.querySelector("canvas");if(!cv)return null;' +
    'var b=cv.getBoundingClientRect();' +
    'return {x:b.x,y:b.y,width:b.width,height:b.height,dpr:window.devicePixelRatio||1};})()');
  if (!rect) return null;
  const r = await send('Page.captureScreenshot', { format: 'png', fromSurface: true,
    clip: { x: rect.x, y: rect.y, width: rect.width, height: rect.height, scale: rect.dpr } });
  if (!r || !r.data) return null;
  const metrics = await evalSend(send, decodeExpr(r.data), { awaitPromise: true });
  if (!metrics || !metrics.png) return null;
  return { clip: rect, base64: metrics.png, metrics,
    expectW: Math.round(rect.width * rect.dpr), expectH: Math.round(rect.height * rect.dpr) };
}

async function diffShots(send, a, b) {
  return evalSend(send, diffExpr(a, b), { awaitPromise: true });
}

async function dispose(send) {
  return evalSend(send,
    '(function(){try{if(typeof __gosx_dispose_engine!=="function")return false;' +
    '__gosx_dispose_engine("' + ENGINE + '");' +
    'var m=document.getElementById("' + MOUNT + '");' +
    'return !!(m&&!m.__gosxScene3DState);}catch(e){return false;}})()');
}

function saveArtifact(name, base64) {
  if (!ART || !base64) return;
  try { fs.writeFileSync(path.join(ART, name), Buffer.from(base64, 'base64')); }
  catch (e) { warnings.push('artifact write failed for ' + name + ': ' + e.message); }
}

// ---- Overall watchdog (bounded, triggers the same central cleanup) ----
setTimeout(() => {
  if (finished) return;
  finished = true;
  errors.push('overall watchdog: probe exceeded ' + OVERALL_MS + 'ms');
  exitCode = 1;
  emit({ errors, warnings, fatal: 'overall watchdog' });
  cleanup(true).then(() => process.exit(exitCode));
  setTimeout(() => process.exit(1), 5000).unref();
}, OVERALL_MS);

(async () => {
  await new Promise((res, rej) => {
    server.once('error', rej);
    server.listen(0, '127.0.0.1', () => res());
  });
  port = server.address().port;
  BASE = 'http://127.0.0.1:' + port;
  profile = fs.mkdtempSync(path.join(os.tmpdir(), 'gosx-ior-probe-'));
  const CHROME = process.env.GOSX_CHROME_BIN || '/usr/bin/google-chrome';
  chrome = spawn(CHROME, [
    '--headless=new', '--no-sandbox', '--use-gl=angle', '--use-angle=gl-egl',
    '--ignore-gpu-blocklist', '--enable-unsafe-swiftshader', '--enable-unsafe-webgpu',
    '--disable-dev-shm-usage', '--user-data-dir=' + profile, '--remote-debugging-port=0', 'about:blank',
  ], { stdio: ['ignore', 'ignore', 'pipe'] });
  // Spawn errors are routed through the awaited wsUrl promise below (its
  // chrome.once('error', onErr) handler); a synchronous-event throw here
  // would escape the promise chain and skip central cleanup.
  const wsUrl = await new Promise((resolve, reject) => {
    let buf = '';
    const t = setTimeout(() => reject(new Error('no DevTools ws URL')), 20000);
    const onExit = () => { clearTimeout(t); reject(new Error('chrome exited early: ' + buf)); };
    const onErr = (e) => { clearTimeout(t); reject(new Error('chrome spawn error: ' + e.message)); };
    chrome.stderr.on('data', (d) => {
      buf += d.toString();
      const m = buf.match(/ws:\/\/127\.0\.0\.1:\d+\/devtools\/browser\/[^\s]+/);
      if (m) {
        clearTimeout(t); chrome.removeListener('exit', onExit); chrome.removeListener('error', onErr);
        resolve(m[0]);
      }
    });
    chrome.once('exit', onExit);
    chrome.once('error', onErr);
  });
  ws = new WebSocket(wsUrl);
  await new Promise((res, rej) => {
    const t = setTimeout(() => rej(new Error('ws connect timeout')), 20000);
    ws.onopen = () => { clearTimeout(t); res(); };
    ws.onerror = () => { clearTimeout(t); rej(new Error('ws error')); };
  });
  ws.onmessage = (ev) => dispatch(ev.data);

  const { targetId } = await cdpSend('Target.createTarget', { url: 'about:blank' });
  const { sessionId } = await cdpSend('Target.attachToTarget', { targetId, flatten: true });
  const send = (method, params, to) => cdpSend(method, params, sessionId, to || CASE_WAIT_MS);
  await send('Page.enable'); await send('Runtime.enable');
  await send('Page.addScriptToEvaluateOnNewDocument', { source: PRELOAD });

  // ---- WebGPU adapter probe: on a real served loopback HTTP origin, with a
  // bounded awaited load; explicit available:true required. Never on
  // about:blank, never on an invalid/unserved origin.
  let adapterAvailable = false, adapterReason = null;
  {
    const loadP = waitForEvent('Page.loadEventFired', CASE_WAIT_MS);
    await send('Page.navigate', { url: BASE + '/' });
    try { await loadP; }
    catch (e) { throw new Error('adapter-probe origin load failed: ' + e.message); }
    try {
      const v = await evalSend(send,
        '(navigator.gpu&&navigator.gpu.requestAdapter)?' +
        'navigator.gpu.requestAdapter().then(function(a){return a?' +
        '({available:true}):({available:false,reason:"requestAdapter resolved null"});}):' +
        'Promise.resolve({available:false,reason:"navigator.gpu unavailable"})',
        { awaitPromise: true });
      if (v && v.available === true) adapterAvailable = true;
      else adapterReason = (v && v.reason) || 'adapter probe returned no explicit available:true';
    } catch (e) {
      adapterAvailable = false;
      adapterReason = 'adapter probe failed: ' + e.message;
    }
    if (!adapterAvailable && REQUIRE_WGPU) {
      fail('GOSX_IOR_REQUIRE_WEBGPU=1: real WebGPU adapter required but unavailable: ' + adapterReason);
    }
  }

  const shots = {}; const evidence = [];

  // After the first readiness fatal, stop attempting further cases promptly:
  // record the remaining cases as aborted (preserving all 21 entries) and exit
  // nonzero via the diagnostic failure below. Normal cases still all run.
  let readinessFatal = false;
  for (const c of CASES) {
    if (readinessFatal) {
      evidence.push({ name: c.name, skipped: true, skipReason: 'aborted after readiness fatal in earlier case' });
      continue;
    }
    if (c.webgpu && !adapterAvailable) {
      if (REQUIRE_WGPU) {
        // Required mode: a skip is always a failure.
        fail(c.name + ': skipped but WebGPU is required (reason: ' + adapterReason + ')');
        evidence.push({ name: c.name, skipped: true, skipReason: adapterReason });
      } else {
        // Normal mode: genuine, explicitly-reasoned adapter-unavailable skip.
        evidence.push({ name: c.name, skipped: true, skipReason: adapterReason });
      }
      continue;
    }

    const rec = { name: c.name, skipped: false };
    evidence.push(rec);
    let cap = null;
    try {
      const loadP = waitForEvent('Page.loadEventFired', CASE_WAIT_MS);
      await send('Page.navigate', { url: BASE + '/case/' + c.name });
      try { await loadP; }
      catch (e) { throw new Error('page load failed: ' + e.message); }
      process.stderr.write('[ior-probe] ' + c.name + ': page loaded, waiting for scene readiness\n');

      let ready = false;
      const deadline = Date.now() + CASE_WAIT_MS;
      while (Date.now() < deadline) {
        if ((await evalSend(send, READY)) === true) { ready = true; break; }
        await sleep(100);
      }
      if (!ready) {
        readinessFatal = true;
        let diag = '';
        try {
          const rs = await evalSend(send, READ);
          diag = rs ? JSON.stringify(rs) : String(rs);
        } catch (e) {
          diag = 'diagnostic read failed: ' + e.message;
        }
        fail(c.name + ': scene not ready (real PBR object/instancedMesh + draws/uploads) within ' +
          CASE_WAIT_MS + 'ms; mount/backend/counter state: ' + diag);
      }
      await sleep(SETTLE_MS);
      const s = await evalSend(send, READ);
      if (!s || !s.ior || !s.wgpu) throw new Error('evidence read failed');

      Object.assign(rec, {
        mounted: s.mounted, renderer: s.renderer, fallback: s.fallback,
        frameSeq: s.frameSeq, meshDraws: s.meshDraws, wgpuErr: s.wgpuErr,
        bundleState: s.bundleState, bundleEncodes: s.bundleEncodes,
        bundleReplays: s.bundleReplays, bundleDraws: s.bundleDraws,
        objects: s.objects, glBackend: s.ior.gl,
        draws: s.ior.draws, pbrDraws: s.ior.pbrDraws, uniformF0: s.ior.lastDrawF0,
        wgpuUploads: s.wgpu.uploads,
      });

      if (s.mounted !== 'true') fail(c.name + ': data-gosx-scene3d-mounted not true');
      if (s.ior.obsErrors.length) fail(c.name + ': GL observation errors: ' + s.ior.obsErrors.join('; '));
      if (s.wgpu.obsErrors.length) fail(c.name + ': WebGPU observation errors: ' + s.wgpu.obsErrors.join('; '));

      if (c.webgpu) {
        if (s.renderer !== 'webgpu') {
          // Adapter exists but production fell back: FAIL, never warn-through.
          fail(c.name + ': WebGPU renderer-fallback: renderer=' + s.renderer +
            ' fallback=' + s.fallback + ' lastError=' + s.wgpuErr);
        } else {
          if (!(Number(s.frameSeq) > 0)) {
            fail(c.name + ': data-gosx-scene3d-webgpu-frame-seq missing or not > 0');
          }
          if (s.bundleState === 'direct') {
            // Direct encoder path: the mesh draw counter is the real evidence.
            if (!(Number(s.meshDraws) > 0)) {
              fail(c.name + ': data-gosx-scene3d-webgpu-mesh-draw-calls missing or not > 0');
            }
          } else if (s.bundleState === 'encoded') {
            // Bundle encoded this frame: requires actual bundle draws plus a
            // positive encode count from the bundle cache stats.
            if (!(Number(s.bundleDraws) > 0) || !(Number(s.bundleEncodes) > 0)) {
              fail(c.name + ': bundleState=encoded but bundleDraws=' + s.bundleDraws +
                ' bundleEncodes=' + s.bundleEncodes + ' (both must be > 0)');
            }
          } else if (s.bundleState === 'replayed') {
            // Cached bundle replayed: requires actual bundle draws plus a
            // positive replay count from the bundle cache stats.
            if (!(Number(s.bundleDraws) > 0) || !(Number(s.bundleReplays) > 0)) {
              fail(c.name + ': bundleState=replayed but bundleDraws=' + s.bundleDraws +
                ' bundleReplays=' + s.bundleReplays + ' (both must be > 0)');
            }
          } else {
            // Unknown/missing state: fail, never silently accept.
            fail(c.name + ': unknown/missing data-gosx-scene3d-webgpu-bundle-state: ' + s.bundleState);
          }
          if (!(s.wgpu.uploads > 0)) fail(c.name + ': no 176-byte material uploads observed');
          const hit = (s.wgpu.dumps || []).some((d) =>
            typeof d.f0 === 'number' && Number.isFinite(d.f0) && Math.abs(d.f0 - c.f0) < 1e-4);
          rec.f0InUpload = hit;
          if (!hit) {
            fail(c.name + ': expected F0 ' + c.f0 + ' not found at float index 40 of any 176-byte upload');
          }
          if (s.wgpuErr) {
            fail(c.name + ': WebGPU renderer produced nonempty wgpuErr: ' + s.wgpuErr);
          }
        }
      } else {
        if (s.renderer !== 'webgl') {
          fail(c.name + ': data-gosx-scene3d-renderer not webgl (got ' + s.renderer +
            ', fallback=' + s.fallback + ')');
        } else if (s.fallback && s.fallback !== 'false' && s.fallback !== '0') {
          fail(c.name + ': unexpected renderer fallback attr: ' + s.fallback);
        }
        if (!(s.ior.pbrDraws > 0)) {
          fail(c.name + ': no production PBR draws with u_dielectricF0 observed (draws=' + s.ior.draws + ')');
        }
        assertClose(s.ior.lastDrawF0, c.f0, c.name + ' u_dielectricF0 at draw');
      }

      cap = await capture(send);
      if (!cap) throw new Error('screenshot capture/decode failed');
      if (cap.clip.width !== W || cap.clip.height !== H) {
        fail(c.name + ': canvas rect ' + cap.clip.width + 'x' + cap.clip.height + ' != expected ' + W + 'x' + H);
      }
      if (cap.metrics.w !== cap.expectW || cap.metrics.h !== cap.expectH) {
        fail(c.name + ': screenshot dimensions ' + cap.metrics.w + 'x' + cap.metrics.h +
          ' != expected ' + cap.expectW + 'x' + cap.expectH);
      }
      const m = cap.metrics;
      rec.litPixels = m.fgPixels; rec.fgFrac = m.fgFrac; rec.meanRGB = m.bg;
      // Foreground-vs-background proof in ALL cases (including IOR 0 / F0 1).
      // A pure background image (fg=0) fails this assertion.
      if (!(m.fgPixels > 0) || !(m.fgFrac >= FG_COVERAGE) || !(m.maxDelta >= FG_THRESHOLD)) {
        fail(c.name + ': no measurable geometry foreground vs measured corner background ' +
          '(fg=' + m.fgPixels + ', frac=' + m.fgFrac.toFixed(4) + ', maxDelta=' + m.maxDelta + ')');
      }
      saveArtifact(c.name + '.png', cap.base64);
      shots[c.name] = cap;

      // CSS var case: real documentElement.style.setProperty --ior 1.33 -> 2.42.
      // Wait for the runtime's own MutationObserver-driven revision advance AND
      // the new observed uniform value AND changed pixels (no remount, no
      // manual state/revision writes).
      if (c.cssVar) {
        await evalSend(send, 'document.documentElement.style.setProperty("--ior","1.33")');
        await sleep(SETTLE_MS);
        const revBefore = Number(s.rev || 0);
        await evalSend(send, 'document.documentElement.style.setProperty("--ior","2.42")');
        let s2 = null, advanced = false;
        const dl = Date.now() + CASE_WAIT_MS;
        while (Date.now() < dl) {
          s2 = await evalSend(send, READ);
          if (s2 && Number(s2.rev || 0) > revBefore && s2.ior &&
              typeof s2.ior.lastDrawF0 === 'number' &&
              Math.abs(s2.ior.lastDrawF0 - F0(2.42)) < 2e-4) { advanced = true; break; }
          await sleep(100);
        }
        if (!advanced) {
          fail('css-var: revision advance + new u_dielectricF0=' + F0(2.42) +
            ' not observed after real style setProperty 1.33 -> 2.42');
        }
        if (s2) {
          if (s2.ior.obsErrors.length) fail('css-var: GL observation errors after change: ' + s2.ior.obsErrors.join('; '));
          if (!(s2.ior.pbrDraws > s.ior.pbrDraws)) fail('css-var: no new PBR draws after CSS change');
        }
      const cap2 = await capture(send);
      if (!cap2) { fail('css-var: after-change capture/decode failed'); }
      else {
          if (cap2.clip.width !== cap.clip.width || cap2.clip.height !== cap.clip.height) {
            fail('css-var: after-change canvas rect ' + cap2.clip.width + 'x' + cap2.clip.height +
              ' != initial ' + cap.clip.width + 'x' + cap.clip.height);
          }
          if (cap2.metrics.w !== cap2.expectW || cap2.metrics.h !== cap2.expectH) {
            fail('css-var: after-change screenshot dimensions ' + cap2.metrics.w + 'x' + cap2.metrics.h +
              ' != expected ' + cap2.expectW + 'x' + cap2.expectH);
          }
          if (!(cap2.metrics.fgPixels > 0) || !(cap2.metrics.fgFrac >= FG_COVERAGE) ||
              !(cap2.metrics.maxDelta >= FG_THRESHOLD)) {
            fail('css-var: no measurable geometry foreground vs measured corner background after CSS change ' +
              '(fg=' + cap2.metrics.fgPixels + ', frac=' + cap2.metrics.fgFrac.toFixed(4) +
              ', maxDelta=' + cap2.metrics.maxDelta + ')');
          }
          const d = await diffShots(send, cap.base64, cap2.base64);
          rec.cssAfter = { rev: s2 && s2.rev, uniformF0: s2 && s2.ior.lastDrawF0, pixelDiff: d };
          if (!d || !d.dimsMatch || !(d.meanChanged >= 50) || !(d.maxDelta >= 3)) {
            fail('css-var: pixels did not change meaningfully after real CSS var change ' +
              '(meanChanged=' + (d && d.meanChanged) + ', maxDelta=' + (d && d.maxDelta) + ')');
          }
          saveArtifact(c.name + '-after.png', cap2.base64);
        }
      }
    } catch (e) {
      fail(c.name + ': ' + String((e && e.message) || e));
    } finally {
      // Per-case dispose, even when evidence gathering / decoding failed.
      if (!rec.skipped) {
        let disposed = false;
        try { disposed = await dispose(send); }
        catch (e) { fail(c.name + ': dispose threw: ' + e.message); }
        rec.disposeRemovedState = disposed === true;
        if (!disposed) fail(c.name + ': __gosx_dispose_engine did not remove scene state');
      }
    }
  }

  // ---- Cross-case image comparisons (real rendered output, fixed camera) ----
  for (const c of CASES) {
    const rec = evidence.find((r) => r.name === c.name);
    const A = shots[c.name];
    if (!rec || rec.skipped || !A) continue;
    try {
      if (c.same != null) {
        const B = shots[c.same];
        const recB = evidence.find((r) => r.name === c.same);
        if (!B || !recB) { fail(c.name + ': missing capture for equality vs ' + c.same); }
        else if (rec.renderer !== recB.renderer) {
          rec.sameAs = { target: c.same, skipped: 'renderer mismatch (' + rec.renderer + ' vs ' + recB.renderer + ')' };
          fail(c.name + ': renderer mismatch vs ' + c.same + ' (' + rec.renderer + ' vs ' + recB.renderer + ')');
        } else {
          const d = await diffShots(send, A.base64, B.base64);
          rec.sameAs = { target: c.same, diff: d };
          // Exact equality: zero changed pixels AND zero changed RGBA bytes.
          if (!d || !d.dimsMatch || d.exactPixels !== 0 || d.exactBytes !== 0) {
            fail(c.name + ': image must be byte-identical to ' + c.same +
              ' (exactChangedPixels=' + (d && d.exactPixels) + ', exactChangedBytes=' + (d && d.exactBytes) + ')');
          }
        }
      }
      if (c.differs != null) {
        const B = shots[c.differs];
        const recB = evidence.find((r) => r.name === c.differs);
        if (!B || !recB) { fail(c.name + ': missing capture for difference vs ' + c.differs); }
        else if (rec.renderer !== recB.renderer) {
          rec.differsFrom = { target: c.differs, skipped: 'renderer mismatch (' + rec.renderer + ' vs ' + recB.renderer + ')' };
          fail(c.name + ': renderer mismatch vs ' + c.differs + ' (' + rec.renderer + ' vs ' + recB.renderer + ')');
        } else {
          const d = await diffShots(send, A.base64, B.base64);
          rec.differsFrom = { target: c.differs, diff: d };
          // Distinct IOR: meaningful change (channel > 2) AND maxDelta >= 3.
          if (!d || !d.dimsMatch || !(d.meanChanged >= (c.minChanged || 1)) || !(d.maxDelta >= 3)) {
            fail(c.name + ': distinct IOR must change visible pixels vs ' + c.differs +
              ' (meanChanged=' + (d && d.meanChanged) + ', maxDelta=' + (d && d.maxDelta) + ')');
          }
        }
      }
    } catch (e) {
      fail(c.name + ': comparison failed: ' + String((e && e.message) || e));
    }
  }

  const ran = evidence.filter((r) => !r.skipped);
  const out = {
    requireWebGPU: REQUIRE_WGPU,
    webgpuAdapterProbe: adapterAvailable ? { available: true }
      : { available: false, reason: adapterReason },
    cases: evidence.map((r) => ({ name: r.name, skipped: r.skipped, skipReason: r.skipReason || undefined,
      mounted: r.mounted, renderer: r.renderer, fallback: r.fallback,
      webgpuFrameSeq: r.frameSeq, webgpuMeshDrawCalls: r.meshDraws, webgpuLastError: r.wgpuErr,
      webgpuBundleState: r.bundleState, webgpuBundleEncodes: r.bundleEncodes,
      webgpuBundleReplays: r.bundleReplays, webgpuBundleDraws: r.bundleDraws,
      glBackend: r.glBackend, draws: r.draws, pbrDraws: r.pbrDraws,
      uniformF0: r.uniformF0, f0InUpload: r.f0InUpload, wgpuUploads: r.wgpuUploads,
      objects: r.objects, fgPixels: r.litPixels, fgFrac: r.fgFrac, cornerBG: r.meanRGB,
      cssAfter: r.cssAfter || undefined, sameAs: r.sameAs || undefined,
      differsFrom: r.differsFrom || undefined, disposeRemovedState: r.disposeRemovedState })),
    disposal: ran.length > 0 && ran.every((r) => r.disposeRemovedState === true),
    artifacts: ART || undefined,
    errors, warnings,
    note: 'Real Chrome + real WebGL2/WebGPU PBR with the actual built bootstrap.js, real GLB ' +
      'loading and native draws. u_dielectricF0 read from real uniform state at production draw ' +
      'calls (getUniformLocation tracking + CURRENT_PROGRAM + getUniform, instanced forms) and at ' +
      'float index 40 of 176-byte WebGPU material uploads; all wrappers strictly forward and ' +
      'observation errors fail the probe. Pixels come from CDP screenshots clipped to the real ' +
      'canvas rect, decoded with a native Image+2D canvas, with foreground-vs-measured-background ' +
      'proof in every case. GPU hardware acceleration type is NOT certified (SwiftShader possible).',
  };
  emit(out);
  if (errors.length) exitCode = 1;
})().catch((e) => {
  errors.push('fatal: ' + String((e && e.stack) || e));
  exitCode = 1;
  emit({ errors, warnings, fatal: String((e && e.stack) || e) });
}).then(async () => {
  finished = true;
  await cleanup(false);
  if (!printed) emit({ errors, warnings, note: 'probe ended without report' });
  process.exit(exitCode);
});
