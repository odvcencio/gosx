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
 *  - native u_specularF0 (vec3) + u_specularF90 uniform values observed at
 *    production GL draw calls (getUniformLocation program->location tracking
 *    + getParameter CURRENT_PROGRAM + getUniform at draw time, instanced
 *    forms included), and the 208-byte WebGPU material upload with the
 *    effective F0 read at float indices 44..46 and F90 at 47 (bytes
 *    176:192, unchanged), the loaded-intensity flag at u32 index 41 (byte
 *    164) and the loaded-color flag at u32 index 51 (byte 204); all wrappers
 *    strictly forward and observation errors are
 *    recorded and fail the probe without changing native behavior;
 *  - actual rendered pixels via CDP screenshot clipped to the real canvas
 *    bounding rect, decoded with a native browser Image + 2D canvas, with
 *    foreground-vs-measured-corner-background threshold + coverage asserted
 *    in ALL cases (including IOR 0 / F0 1);
 *  - omitted vs explicit ior 1.5 both F0 0.04 with zero changed bytes/pixels;
 *  - ior 1 -> 0, 1.33/2.42/>5 -> ((ior-1)/(ior+1))^2, explicit 0 -> 1;
 *  - fully metallic images invariant to IOR while uniforms stay distinct;
 *  - real GLB with KHR_materials_ior 2.42, model override 1.33, omitted
 *    instancedGLB batch preserving loaded 2.42, explicit zero batch override;
 *  - real GLB KHR_materials_specular FACTORS through the importer: omitted
 *    extension on a default-IOR asset (F0 .04/F90 1 baseline), explicit
 *    factor 1 / color [1,1,1] pixel-identical to that baseline, factor 0
 *    (F0/F90 both 0), IOR 2.42 with HDR factors .5 / [100,.5,2] giving
 *    F0 [.5, F0(2.42)*.25, F0(2.42)] and F90 .5, a model specularIntensity:0
 *    override of that asset, and an instanced GLB batch inheriting the loaded
 *    factors exactly (no batch specular overrides) vs batch
 *    specularIntensity:0;
 *  - real GLB KHR_materials_specular specularTexture (intensity-alpha slice)
 *    on BOTH backends: saturated/zero/fractional/RGB-irrelevance/fully-
 *    metallic/IBL-isolated textured cases plus the untextured fractional
 *    control. WebGPU observes the real 208-byte upload flag; WebGL observes
 *    the real u_hasSpecularIntensityMap uniform at production draw time
 *    (missing locations/observations are null and never pass);
 *  - real GLB KHR_materials_specular specularColorTexture: white/black/
 *    tinted/HDR color textures against untextured linear-factor controls,
 *    texture-alpha irrelevance for the color role, a combined color+intensity
 *    texture case, fully metallic and IBL-isolated color-texture cases, with
 *    both loaded flags and both actual texture fetches observed;
 *  - named-material table reference; real CSS var(--ior) 1.33 -> 2.42 change
 *    via documentElement.style.setProperty with observed revision advance,
 *    new uniform value and changed pixels (no remount, no manual writes);
 *  - dispose removes scene state; bounded waits; overall 4.5min watchdog;
 *  - graceful+bounded Chrome teardown, CDP/server close, owned tmp profile
 *    removal on success/failure/timeout.
 * GPU hardware acceleration type is not certified (SwiftShader possible).
 * Also covers real specular-IBL isolation: the verified Go fixture bakes a
 * constant radiance cube (RGB [.75,.875,1], 2 mips), zero diffuse irradiance
 * and a 1x1 BRDF LUT (A=.5, B=.25), so with F0=0 the pixel response isolates
 * B*F90. Positive IBL pairs (specularIntensity 1 vs 0) must differ; no-IBL
 * negative controls (direct light zeroed, IBL disabled, no IBL assets fetched)
 * must be pixel-identical. Case count is dynamic (CASES.length).
 */

const fs = require('fs'), os = require('os'), path = require('path');
const http = require('http');
const zlib = require('zlib');
const { spawn, execFileSync } = require('child_process');

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
const GLTF_CHUNK = path.join(REPO, 'client', 'js', 'bootstrap-feature-scene3d-gltf.js');
if (!fs.existsSync(GLTF_CHUNK)) {
  console.error('missing built runtime asset: ' + GLTF_CHUNK);
  process.exit(2);
}
// Skinned-shadow cases exercise the animation feature chunk (mixer play/stop)
// through the real built production asset. A missing chunk is fatal.
const ANIM_CHUNK = path.join(REPO, 'client', 'js', 'bootstrap-feature-scene3d-animation.js');
if (!fs.existsSync(ANIM_CHUNK)) {
  console.error('missing built runtime asset: ' + ANIM_CHUNK);
  process.exit(2);
}
// Per-case served counters for the skinned-shadow GLB assets (reset on each
// /case/ load, asserted positive for every case that mounts a skinned model).
let skinnedGlbServed = { rest: 0, pose: 0 };
// Counts actual /bootstrap-feature-scene3d-animation.js route serves for the
// current case (reset on each /case/ load).
let animChunkServed = 0;
// Verified deterministic IBL fixture: one JSON object on stdout with base64
// KTX2 radiance/irradiance/brdfLUT plus the real environment.ibl descriptor.
// A missing or invalid fixture is fatal: no probe runs without it.
let IBL_FIXTURE = null;
try {
  IBL_FIXTURE = JSON.parse(execFileSync('go', ['run', './client/js/testdata/specular-ibl-fixture'],
    { cwd: REPO, encoding: 'utf8', timeout: 60000 }));
  if (!IBL_FIXTURE || !IBL_FIXTURE.radiance || !IBL_FIXTURE.irradiance ||
      !IBL_FIXTURE.brdfLUT || !IBL_FIXTURE.descriptor) {
    throw new Error('incomplete fixture payload');
  }
} catch (e) {
  console.error('specular-ibl-fixture failed: ' + ((e && e.message) || e));
  process.exit(2);
}
const b64buf = (b) => Buffer.from(String(b), 'base64');
let iblAssetCount = { radiance: 0, irradiance: 0, brdfLUT: 0 };
const REQUIRE_WGPU = process.env.GOSX_IOR_REQUIRE_WEBGPU === '1';

const errors = [], warnings = [];
const fail = (m) => { errors.push(m); };
const F0 = (ior) => ((ior - 1) / (ior + 1)) * ((ior - 1) / (ior + 1));
const sleep = (ms) => new Promise((res) => setTimeout(res, ms));

const ENGINE = 'gosx-engine-ior-browser';
const MOUNT = 'scene-ior-browser';
const W = 256, H = 192;
// Raised 270000 -> 300000: probe grew from 251 to 267 cases plus four new
// live skin transitions; the last run reached the final wg-instlife-lifecycle
// case, then aborted solely on 'overall watchdog: probe exceeded 270000ms',
// so the budget needs more headroom for the added whole-suite work.
const OVERALL_MS = 300000;
const CASE_WAIT_MS = 20000;
const SETTLE_MS = 600;
const FG_THRESHOLD = 12;   // min channel delta vs measured corner background
const FG_COVERAGE = 0.01;  // min fraction of foreground pixels

// ---- GLB fixture: one quad facing +Z, positions + normals, metallic 0 ----
// buildQuadGLB(true) remains byte-identical to the original IOR 2.42 fixture;
// the optional second argument adds KHR_materials_specular factor inputs that
// only the importer (never model/batch duplication) consumes; the optional
// third argument adds the opacity alpha inputs, also importer-only.
function buildQuadGLB(withIor, spec, alpha) {
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
      baseColorFactor: [0.69, 0.31, 0.24, alpha ? alpha.alpha : 1],
      metallicFactor: 0, // glTF 2.0 spelling (metalnessFactor is invalid and
                         // silently loads as metallic 1 in strict loaders)
      roughnessFactor: 0.35,
    },
  };
  if (withIor) {
    material.extensions = { KHR_materials_ior: { ior: 2.42 } };
  }
  if (alpha && alpha.mode) material.alphaMode = alpha.mode;
  // MASK fixtures only: a defined cutoff (including the explicit 0) is
  // copied to material.alphaCutoff and nothing else in the fixture changes.
  if (alpha && alpha.cutoff !== undefined) material.alphaCutoff = alpha.cutoff;
  if (spec) {
    material.extensions = material.extensions || {};
    material.extensions.KHR_materials_specular = {
      specularFactor: spec.factor, specularColorFactor: spec.color };
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
  const extUsed = [];
  if (withIor) extUsed.push('KHR_materials_ior');
  if (spec) extUsed.push('KHR_materials_specular');
  if (extUsed.length) json.extensionsUsed = extUsed;
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
// Default-IOR asset with the specular extension omitted entirely: the
// importer must default specularIntensity to 1 and specularColor to linear
// white, so F0 .04 (default IOR 1.5) / F90 1. This is the GLB baseline for
// the factor cases (the authored-object fixtures use a different base color,
// so cross-family pixel comparison is not valid).
const glbDefaultIor = buildQuadGLB(false);
// Explicit factor 1 / linear white must be indistinguishable from the omitted
// extension; factor 0 must zero both F0 and F90.
const glbSpecWhite = buildQuadGLB(false, { factor: 1, color: [1, 1, 1] });
const glbSpecZero = buildQuadGLB(false, { factor: 0, color: [1, 1, 1] });
// IOR 2.42 asset with HDR specular factors served through the importer only:
// F0 channel clamp happens before the intensity scale, so [100,.5,2] with
// factor .5 yields F0 [.5, F0(2.42)*.25, F0(2.42)] and F90 .5. These inputs
// are deliberately NOT duplicated as model or batch overrides.
const glbSpecIor242 = buildQuadGLB(true, { factor: 0.5, color: [100, 0.5, 2] });

// ---- Specular-INTENSITY-ALPHA texture slice (ALPHA-only addition) ---------
// Deterministic RGBA PNG fixtures built with Node builtin zlib plus a local
// CRC32, served as image/png. Each texture is a flat 4x4 block: production
// consumes only the ALPHA channel as per-pixel specular intensity; the RGB
// channels must be ignored by the sampler path (asserted below).
function crc32(buf) {
  let table = crc32.table;
  if (!table) {
    table = crc32.table = new Int32Array(256);
    for (let n = 0; n < 256; n += 1) {
      let c = n;
      for (let k = 0; k < 8; k += 1) c = (c & 1) ? (0xEDB88320 ^ (c >>> 1)) : (c >>> 1);
      table[n] = c;
    }
  }
  let c = -1;
  for (let i = 0; i < buf.length; i += 1) c = (c >>> 8) ^ table[(c ^ buf[i]) & 0xFF];
  return (c ^ -1) >>> 0;
}
function makePNG(rgb, alpha) {
  const width = 4, height = 4;
  const stride = 1 + width * 4;
  const raw = Buffer.alloc(height * stride);
  for (let y = 0; y < height; y += 1) {
    raw[y * stride] = 0; // filter type None
    for (let x = 0; x < width; x += 1) {
      const o = y * stride + 1 + x * 4;
      raw[o] = rgb[0]; raw[o + 1] = rgb[1]; raw[o + 2] = rgb[2]; raw[o + 3] = alpha;
    }
  }
  function chunk(type, data) {
    const out = Buffer.alloc(8 + data.length + 4);
    out.writeUInt32BE(data.length, 0);
    out.write(type, 4, 'ascii');
    data.copy(out, 8);
    out.writeUInt32BE(crc32(out.subarray(4, 8 + data.length)), 8 + data.length);
    return out;
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(width, 0); ihdr.writeUInt32BE(height, 4);
  ihdr[8] = 8;  // bit depth
  ihdr[9] = 6;  // color type RGBA
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]),
    chunk('IHDR', ihdr),
    chunk('IDAT', zlib.deflateSync(raw)),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}
const TEX_PNGS = {
  '/tex/spec-alpha255.png': makePNG([200, 120, 60], 255),
  '/tex/spec-alpha0-white.png': makePNG([255, 255, 255], 0),
  '/tex/spec-alpha128-black.png': makePNG([0, 0, 0], 128),
  '/tex/spec-alpha128-red.png': makePNG([255, 0, 0], 128),
  // Specular-COLOR texture slice: deterministic flat RGBA blocks. Only the
  // sRGB RGB channels carry the color role; the alpha channel must not affect
  // it (asserted by the tint vs tint-alpha0 exact pair below).
  '/tex/spec-color-white.png': makePNG([255, 255, 255], 255),
  '/tex/spec-color-black.png': makePNG([0, 0, 0], 255),
  '/tex/spec-color-tint.png': makePNG([128, 64, 255], 255),
  '/tex/spec-color-tint-alpha0.png': makePNG([128, 64, 255], 0),
  '/tex/spec-color-tint-alpha128.png': makePNG([128, 64, 255], 128),
  '/tex/spec-color-hdr.png': makePNG([64, 128, 255], 255),
  // Base-color texture slice: flat white RGB with varied alpha, so the RGB
  // role is fixed and only the alpha channel varies across the trio.
  '/tex/alb-white-a0.png': makePNG([255, 255, 255], 0),
  '/tex/alb-white-a128.png': makePNG([255, 255, 255], 128),
  '/tex/alb-white-a255.png': makePNG([255, 255, 255], 255),
};
let texServed = {};

// Textured quad GLB variant: same geometry/material family as buildQuadGLB
// but with valid TEXCOORD_0 UVs and KHR_materials_specular.specularTexture
// pointing at a served same-origin PNG. The texture-derived intensity is NOT
// duplicated as any model or batch specular override. The pre-existing
// untextured buildQuadGLB fixtures stay byte-identical.
function buildQuadGLBTex(opts) {
  const pos = new Float32Array([-1, -1, 0, 1, -1, 0, 1, 1, 0, -1, 1, 0]);
  const nrm = new Float32Array([0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1]);
  const uv = new Float32Array([0, 1, 1, 1, 1, 0, 0, 0]);
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
  const pv = addView(pos, 34962), nv = addView(nrm, 34962);
  const uvv = addView(uv, 34962), iv = addView(idx, 34963);
  const bin = Buffer.concat(parts);
  const material = {
    pbrMetallicRoughness: {
      baseColorFactor: [0.69, 0.31, 0.24, 1],
      metallicFactor: opts.metallic ? 1 : 0,
      roughnessFactor: 0.35,
    },
  };
  const images = [];
  const textures = [];
  if (opts.png) { textures.push({ sampler: 0, source: images.length }); images.push({ uri: opts.png }); }
  if (opts.colorTex) { textures.push({ sampler: 0, source: images.length }); images.push({ uri: opts.colorTex }); }
  // Optional KHR_materials_specular inputs: extension texture indices are
  // assigned from the ACTUAL textures array ordering (intensity texture is
  // pushed first when present, color texture second when present), so a
  // color-only fixture correctly references index 0 and a combined fixture
  // references indices 0/1. Omitted factor inputs keep the importer defaults
  // (intensity 1, white color), so the pre-existing png-only fixtures keep
  // their exact semantics.
  const specExt = {};
  if (opts.png) specExt.specularTexture = { index: 0 };
  if (opts.colorTex) specExt.specularColorTexture = { index: textures.length - 1 };
  if (opts.specFactor != null) specExt.specularFactor = opts.specFactor;
  if (opts.specColor) specExt.specularColorFactor = opts.specColor;
  if (opts.baseAlpha !== undefined) {
    material.pbrMetallicRoughness.baseColorFactor = [0.69, 0.31, 0.24, opts.baseAlpha];
  }
  const baseColorIndex = opts.baseColorTex !== undefined
    ? textures.length
    : undefined;
  const extUsed = [];
  if (Object.keys(specExt).length) {
    material.extensions = { KHR_materials_specular: specExt };
    extUsed.push('KHR_materials_specular');
  }
  if (opts.ior != null) {
    material.extensions = material.extensions || {};
    material.extensions.KHR_materials_ior = { ior: opts.ior };
    extUsed.push('KHR_materials_ior');
  }
  if (opts.alphaMode !== undefined) material.alphaMode = opts.alphaMode;
  if (opts.alphaCutoff !== undefined) material.alphaCutoff = opts.alphaCutoff;
  if (opts.baseColorTex !== undefined) {
    material.pbrMetallicRoughness.baseColorTexture = { index: baseColorIndex };
  }
  if (opts.unlit) {
    material.extensions = material.extensions || {};
    material.extensions.KHR_materials_unlit = {};
    extUsed.push('KHR_materials_unlit');
  }
  if (opts.baseColorTex !== undefined) {
    textures.push({ sampler: 0, source: images.length });
    images.push({ uri: opts.baseColorTex });
  }
  const json = {
    asset: { version: '2.0', generator: 'scene3d-material-ior-browser probe' },
    scene: 0, scenes: [{ nodes: [0] }], nodes: [{ mesh: 0, name: 'quad' }],
    meshes: [{ name: 'quad', primitives: [{ attributes: { POSITION: pv, NORMAL: nv, TEXCOORD_0: uvv }, indices: iv, mode: 4, material: 0 }] }],
    materials: [material],
    accessors: [
      { bufferView: pv, componentType: 5126, count: 4, type: 'VEC3', min: [-1, -1, 0], max: [1, 1, 0] },
      { bufferView: nv, componentType: 5126, count: 4, type: 'VEC3', min: [0, 0, 1], max: [0, 0, 1] },
      { bufferView: uvv, componentType: 5126, count: 4, type: 'VEC2', min: [0, 0], max: [1, 1] },
      { bufferView: iv, componentType: 5123, count: 6, type: 'SCALAR', min: [0], max: [3] },
    ],
    textures: textures,
    samplers: [{ magFilter: 9729, minFilter: 9987, wrapS: 33071, wrapT: 33071 }],
    images: images,
    bufferViews: views, buffers: [{ byteLength: bin.length }],
    extensionsUsed: extUsed,
  };
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
const glbTexAlpha255 = buildQuadGLBTex({ png: '/tex/spec-alpha255.png' });
const glbTexAlpha0 = buildQuadGLBTex({ png: '/tex/spec-alpha0-white.png' });
const glbTexAlpha128Black = buildQuadGLBTex({ png: '/tex/spec-alpha128-black.png' });
const glbTexAlpha128Red = buildQuadGLBTex({ png: '/tex/spec-alpha128-red.png' });
const glbTexMetalAlpha0 = buildQuadGLBTex({ png: '/tex/spec-alpha0-white.png', metallic: true });
const glbTexMetalAlpha255 = buildQuadGLBTex({ png: '/tex/spec-alpha255.png', metallic: true });
const glbTexIblAlpha0 = buildQuadGLBTex({ png: '/tex/spec-alpha0-white.png', ior: 1 });
const glbTexIblAlpha255 = buildQuadGLBTex({ png: '/tex/spec-alpha255.png', ior: 1 });
const glbSpec128 = buildQuadGLB(false, { factor: 128 / 255, color: [1, 1, 1] });
// ---- Masked/unlit baseColorTexture fixtures (masktex slice) ---------------
// Each fixture is a quad with a baseColorTexture (alpha comes from the PNG
// texels), ior 2.42, and combinations of alphaMode/alphaCutoff/unlit and
// baseAlpha factor. Expected opacity/cutoff/emptiness is authored per-case
// below, never derived from shader math.
const glbMasktexControl = buildQuadGLBTex({
  baseColorTex: '/tex/alb-white-a255.png', ior: 2.42 });
const glbMasktexOpaqueA0 = buildQuadGLBTex({
  baseColorTex: '/tex/alb-white-a0.png', ior: 2.42 });
const glbMasktexMaskA0 = buildQuadGLBTex({
  baseColorTex: '/tex/alb-white-a0.png', ior: 2.42,
  alphaMode: 'MASK', alphaCutoff: 0.5 });
const glbMasktexMaskA255 = buildQuadGLBTex({
  baseColorTex: '/tex/alb-white-a255.png', ior: 2.42,
  alphaMode: 'MASK', alphaCutoff: 0.5 });
const glbMasktexMaskA128 = buildQuadGLBTex({
  baseColorTex: '/tex/alb-white-a128.png', ior: 2.42,
  alphaMode: 'MASK', alphaCutoff: 0.5 });
const glbMasktexMaskA128F5 = buildQuadGLBTex({
  baseColorTex: '/tex/alb-white-a128.png', ior: 2.42,
  alphaMode: 'MASK', alphaCutoff: 0.5, baseAlpha: 0.5 });
const glbMasktexMaskC0F0 = buildQuadGLBTex({
  baseColorTex: '/tex/alb-white-a0.png', ior: 2.42,
  alphaMode: 'MASK', alphaCutoff: 0, baseAlpha: 0 });
const glbMasktexUnlitControl = buildQuadGLBTex({
  baseColorTex: '/tex/alb-white-a255.png', ior: 2.42, unlit: true });
const glbMasktexUnlitDiscard = buildQuadGLBTex({
  baseColorTex: '/tex/alb-white-a128.png', ior: 2.42,
  alphaMode: 'MASK', alphaCutoff: 0.5, baseAlpha: 0.5, unlit: true });
const glbMasktexUnlitSurvive = buildQuadGLBTex({
  baseColorTex: '/tex/alb-white-a128.png', ior: 2.42,
  alphaMode: 'MASK', alphaCutoff: 0.5, unlit: true });
const GLB_FILES = {
  '/models/quad-tex-alpha255.glb': glbTexAlpha255,
  '/models/quad-tex-alpha0.glb': glbTexAlpha0,
  '/models/quad-tex-alpha128-black.glb': glbTexAlpha128Black,
  '/models/quad-tex-alpha128-red.glb': glbTexAlpha128Red,
  '/models/quad-tex-metal-alpha0.glb': glbTexMetalAlpha0,
  '/models/quad-tex-metal-alpha255.glb': glbTexMetalAlpha255,
  '/models/quad-tex-ibl-alpha0.glb': glbTexIblAlpha0,
  '/models/quad-tex-ibl-alpha255.glb': glbTexIblAlpha255,
  '/models/quad-spec-128.glb': glbSpec128,
  '/models/gl-masktex-control.glb': glbMasktexControl,
  '/models/gl-masktex-opaque-a0.glb': glbMasktexOpaqueA0,
  '/models/gl-masktex-mask-a0.glb': glbMasktexMaskA0,
  '/models/gl-masktex-mask-a255.glb': glbMasktexMaskA255,
  '/models/gl-masktex-mask-a128.glb': glbMasktexMaskA128,
  '/models/gl-masktex-mask-a128-f5.glb': glbMasktexMaskA128F5,
  '/models/gl-masktex-mask-c0-f0.glb': glbMasktexMaskC0F0,
  '/models/gl-masktex-unlit-control.glb': glbMasktexUnlitControl,
  '/models/gl-masktex-unlit-discard.glb': glbMasktexUnlitDiscard,
  '/models/gl-masktex-unlit-survive.glb': glbMasktexUnlitSurvive,
};

// ---- Specular-COLOR texture fixtures (color-texture slice) ----------------
// Exact sRGB transfer function (IEC 61966-2-1), NOT a gamma-2.2
// approximation: GPU sRGB texture formats decode with this exact curve, so
// the untextured controls below author their LINEAR factors as the exactly
// decoded texel values. Color factors are LINEAR; the color texture RGB is
// sRGB and is decoded by the sampler path.
function srgbToLinear8(c8) {
  const c = c8 / 255;
  return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
}
const TINT_SRGB = [128, 64, 255];
const TINT_LINEAR = TINT_SRGB.map(srgbToLinear8);
const HDRTEX_SRGB = [64, 128, 255];
const HDRTEX_LINEAR = HDRTEX_SRGB.map(srgbToLinear8);
const glbTexColorWhite = buildQuadGLBTex({ colorTex: '/tex/spec-color-white.png' });
const glbTexColorBlack = buildQuadGLBTex({ colorTex: '/tex/spec-color-black.png' });
const glbTexColorTint = buildQuadGLBTex({ colorTex: '/tex/spec-color-tint.png' });
const glbTexColorTintAlpha0 = buildQuadGLBTex({ colorTex: '/tex/spec-color-tint-alpha0.png' });
const glbTexColorTintAlpha128 = buildQuadGLBTex({
  colorTex: '/tex/spec-color-tint-alpha128.png', png: '/tex/spec-alpha128-black.png',
  specFactor: 0.5 });
const glbTexColorHdr = buildQuadGLBTex({ colorTex: '/tex/spec-color-hdr.png',
  specFactor: 0.5, specColor: [100, 50, 2] });
const glbTexColorInt0 = buildQuadGLBTex({ colorTex: '/tex/spec-color-white.png', specFactor: 0 });
const glbTexColorMetal = buildQuadGLBTex({ colorTex: '/tex/spec-color-tint.png', metallic: true });
const glbTexIblColorBlack = buildQuadGLBTex({ colorTex: '/tex/spec-color-black.png',
  specColor: [4, 1, 1] });
const glbTexIblColorWhite = buildQuadGLBTex({ colorTex: '/tex/spec-color-white.png',
  specColor: [4, 1, 1] });
// Untextured importer-only factor controls (never duplicated as model or
// batch overrides): each matches its textured case above per the case table.
const glbSpecColorBlack = buildQuadGLB(false, { factor: 1, color: [0, 0, 0] });
const glbSpecColorTint = buildQuadGLB(false, { factor: 1,
  color: [TINT_LINEAR[0], TINT_LINEAR[1], TINT_LINEAR[2]] });
const glbSpecColorHdr = buildQuadGLB(false, { factor: 0.5,
  color: [100 * HDRTEX_LINEAR[0], 50 * HDRTEX_LINEAR[1], 2 * HDRTEX_LINEAR[2]] });
const glbSpecColorInt = buildQuadGLB(false, { factor: 0.5 * (128 / 255),
  color: [TINT_LINEAR[0], TINT_LINEAR[1], TINT_LINEAR[2]] });
Object.assign(GLB_FILES, {
  '/models/quad-tex-color-white.glb': glbTexColorWhite,
  '/models/quad-tex-color-black.glb': glbTexColorBlack,
  '/models/quad-tex-color-tint.glb': glbTexColorTint,
  '/models/quad-tex-color-tint-alpha0.glb': glbTexColorTintAlpha0,
  '/models/quad-tex-color-tint-int128.glb': glbTexColorTintAlpha128,
  '/models/quad-tex-color-hdr.glb': glbTexColorHdr,
  '/models/quad-tex-color-int0.glb': glbTexColorInt0,
  '/models/quad-tex-color-metal.glb': glbTexColorMetal,
  '/models/quad-tex-ibl-color-black.glb': glbTexIblColorBlack,
  '/models/quad-tex-ibl-color-white.glb': glbTexIblColorWhite,
  '/models/quad-spec-color-black.glb': glbSpecColorBlack,
  '/models/quad-spec-color-tint.glb': glbSpecColorTint,
  '/models/quad-spec-color-hdr.glb': glbSpecColorHdr,
  '/models/quad-spec-color-int.glb': glbSpecColorInt,
 });

// Alpha variants of the SAME valid quad (identical positions/normals/indices,
// KHR IOR 2.42 in every variant); only baseColorFactor alpha and alphaMode
// vary. Served at distinct /models/alpha-*.glb paths. No alpha textures and
// no unlit variants; OPAQUE forces effective opacity 1 regardless of authored
// alpha, BLEND preserves it. The MASK variants add only material.alphaCutoff
// (factor-only masking, cutoff .5 / 0 / 2); no texture-based MASK fixtures.
const glbAlphaOmitA0 = buildQuadGLB(true, null, { alpha: 0 });
const glbAlphaOPA0 = buildQuadGLB(true, null, { alpha: 0, mode: 'OPAQUE' });
const glbAlphaOPA25 = buildQuadGLB(true, null, { alpha: 0.25, mode: 'OPAQUE' });
const alphaGLBs = {
  'alpha-opaque-a1': buildQuadGLB(true, null, { alpha: 1, mode: 'OPAQUE' }),
  'alpha-omit-a0': glbAlphaOmitA0,
  'alpha-opaque-a0': glbAlphaOPA0,
  'alpha-opaque-a25': glbAlphaOPA25,
  'alpha-blend-a25': buildQuadGLB(true, null, { alpha: 0.25, mode: 'BLEND' }),
  'alpha-blend-a1': buildQuadGLB(true, null, { alpha: 1, mode: 'BLEND' }),
  'alpha-override-a25': glbAlphaOmitA0,
  'alpha-mask-c5-f25': buildQuadGLB(true, null, { alpha: 0.25, mode: 'MASK' }),
  'alpha-mask-c5-f5': buildQuadGLB(true, null, { alpha: 0.5, mode: 'MASK', cutoff: 0.5 }),
  'alpha-mask-c0-f0': buildQuadGLB(true, null, { alpha: 0, mode: 'MASK', cutoff: 0 }),
  'alpha-mask-c2-f1': buildQuadGLB(true, null, { alpha: 1, mode: 'MASK', cutoff: 2 }),
  'alpha-mask-c5-f1': buildQuadGLB(true, null, { alpha: 1, mode: 'MASK', cutoff: 0.5 }),
};

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
const AMODEL = (glb, extra) => Object.assign({ id: 'quad', src: '/models/' + glb + '.glb',
  static: true }, extra);
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
  // Alpha pairing: the opaque-alpha1 control establishes the reference render;
  // omitted mode at alpha0, and explicit OPAQUE at alpha0/alpha0.25 must all
  // be pixel-identical to it (effective opacity forced to 1). Each backend
  // also renders an explicit GoSX alpha-pass control: the same OPAQUE alpha1
  // GLB rendered with the renderPass 'alpha' override, which must match the
  // BLEND alpha1 byte-for-byte (GL blend pass edges are the only reason BLEND
  // alpha1 need not equal the opaque control, so cross-pass identity at full
  // opacity is documented but NOT asserted). BLEND at alpha0.25 preserves
  // 0.25 and visibly differs from the opaque control. The override case
  // imports an OPAQUE alpha0 GLB but applies the GoSX model overrides
  // { opacity: 0.25, renderPass: 'alpha' }; an explicit imported renderPass
  // takes precedence in WebGL, so it runs in the alpha pass at opacity 0.25
  // and must match BLEND alpha0.25 pixel-for-pixel. Every alpha case keeps
  // F0(2.42) and asserts the actual observed opacity.
  { name: 'glb-alpha-pass1', model: AMODEL('alpha-opaque-a1', { renderPass: 'alpha' }),
    f0: F0(2.42), expectedOpacity: 1, base: 'gap1' },
  { name: 'glb-alpha-opaque1', model: AMODEL('alpha-opaque-a1'), f0: F0(2.42),
    expectedOpacity: 1, base: 'ga1' },
  { name: 'glb-alpha-omit0', model: AMODEL('alpha-omit-a0'), f0: F0(2.42),
    expectedOpacity: 1, same: 'glb-alpha-opaque1' },
  { name: 'glb-alpha-opaque0', model: AMODEL('alpha-opaque-a0'), f0: F0(2.42),
    expectedOpacity: 1, same: 'glb-alpha-opaque1' },
  { name: 'glb-alpha-opaque25', model: AMODEL('alpha-opaque-a25'), f0: F0(2.42),
    expectedOpacity: 1, same: 'glb-alpha-opaque1' },
  { name: 'glb-alpha-blend25', model: AMODEL('alpha-blend-a25'), f0: F0(2.42),
    expectedOpacity: 0.25, differs: 'glb-alpha-opaque1', minChanged: 50 },
  { name: 'glb-alpha-blend1', model: AMODEL('alpha-blend-a1'), f0: F0(2.42),
    expectedOpacity: 1, same: 'glb-alpha-pass1' },
  { name: 'glb-alpha-override25',
    model: AMODEL('alpha-override-a25', { opacity: 0.25, renderPass: 'alpha' }),
    f0: F0(2.42), expectedOpacity: 0.25, same: 'glb-alpha-blend25' },
  // glTF MASK: real COLOR-PASS alpha-mask checks on BOTH backends restricted
  // to factor-only fill masking (no alpha-mask texture, no cutout shadow,
  // no wireframe). Every MASK case forces wireframe:false so the comparison
  // is FILL pixels only, made against the dedicated per-backend FILL control
  // (alpha-opaque-a1 with wireframe:false and no authored cutoff; the WebGL
  // glb-mask-fill-control and the WebGPU wg-mask-fill-control), not a
  // default-wireframe opaque case, and same/diff references always name the
  // same-backend FILL control. The cutoff uniform (WebGL) / 208-byte upload
  // float index 42 (WebGPU) is observed at the SAME opacity/F0/F90-qualified
  // PBR draw/upload as opacity.
  // c5-f25: fill alpha 0.25 < cutoff 0.5 -> every fill fragment discarded,
  // strict empty screenshot (full background, zero foreground), meaningfully
  // different from the FILL control. c5-f5: fill alpha == cutoff keeps the
  // fragment (>= comparison), pixel-identical to the FILL control. c0-f0:
  // explicit cutoff 0 keeps fill-alpha-0 fragments, still matching the FILL
  // control (CPU hide-gate regression for a cutoff of exactly 0). c2-f1:
  // cutoff 2 discards fill alpha 1 -> strict empty screenshot. c5-f1:
  // survives and matches the FILL control. expectedEmpty cases still perform
  // a real PBR draw with expected uniforms and full readiness; only the
  // pixel-content assertion differs (strict all-background, zero-foreground).
  { name: 'glb-mask-fill-control',
    model: AMODEL('alpha-opaque-a1', { wireframe: false }), f0: F0(2.42),
    expectedOpacity: 1, expectedAlphaCutoff: -1 },
  { name: 'glb-mask-c5-f25', model: AMODEL('alpha-mask-c5-f25', { wireframe: false }),
    expectedOpacity: 0.25, expectedAlphaCutoff: 0.5, expectedEmpty: true,
    f0: F0(2.42), differs: 'glb-mask-fill-control', minChanged: 50 },
  { name: 'glb-mask-c5-f5', model: AMODEL('alpha-mask-c5-f5', { wireframe: false }),
    f0: F0(2.42), expectedOpacity: 0.5, expectedAlphaCutoff: 0.5,
    same: 'glb-mask-fill-control' },
  { name: 'glb-mask-c0-f0', model: AMODEL('alpha-mask-c0-f0', { wireframe: false }),
    f0: F0(2.42), expectedOpacity: 0, expectedAlphaCutoff: 0,
    same: 'glb-mask-fill-control' },
  { name: 'glb-mask-c2-f1', model: AMODEL('alpha-mask-c2-f1', { wireframe: false }),
    expectedOpacity: 1, expectedAlphaCutoff: 2, expectedEmpty: true,
    f0: F0(2.42), differs: 'glb-mask-fill-control', minChanged: 50 },
  { name: 'glb-mask-c5-f1', model: AMODEL('alpha-mask-c5-f1', { wireframe: false }),
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: 0.5,
    same: 'glb-mask-fill-control' },
  WG({ name: 'wg-alpha-pass1', model: AMODEL('alpha-opaque-a1', { renderPass: 'alpha' }),
    f0: F0(2.42), expectedOpacity: 1, base: 'wgap1' }),
  WG({ name: 'wg-alpha-opaque1', model: AMODEL('alpha-opaque-a1'), f0: F0(2.42),
    expectedOpacity: 1, base: 'wga1' }),
  WG({ name: 'wg-alpha-omit0', model: AMODEL('alpha-omit-a0'), f0: F0(2.42),
    expectedOpacity: 1, same: 'wg-alpha-opaque1' }),
  WG({ name: 'wg-alpha-opaque0', model: AMODEL('alpha-opaque-a0'), f0: F0(2.42),
    expectedOpacity: 1, same: 'wg-alpha-opaque1' }),
  WG({ name: 'wg-alpha-opaque25', model: AMODEL('alpha-opaque-a25'), f0: F0(2.42),
    expectedOpacity: 1, same: 'wg-alpha-opaque1' }),
  WG({ name: 'wg-alpha-blend25', model: AMODEL('alpha-blend-a25'), f0: F0(2.42),
    expectedOpacity: 0.25, differs: 'wg-alpha-opaque1', minChanged: 50 }),
  WG({ name: 'wg-alpha-blend1', model: AMODEL('alpha-blend-a1'), f0: F0(2.42),
    expectedOpacity: 1, same: 'wg-alpha-pass1' }),
  WG({ name: 'wg-alpha-override25',
    model: AMODEL('alpha-override-a25', { opacity: 0.25, renderPass: 'alpha' }),
    f0: F0(2.42), expectedOpacity: 0.25, same: 'wg-alpha-blend25' }),
  // WebGPU MASK mirror of the glb-mask-* cases: same fixtures, same
  // wireframe:false FILL-only comparisons, same expectedEmpty semantics;
  // cutoff is validated in the 208-byte material upload (float index 42).
  WG({ name: 'wg-mask-fill-control',
    model: AMODEL('alpha-opaque-a1', { wireframe: false }), f0: F0(2.42),
    expectedOpacity: 1, expectedAlphaCutoff: -1 }),
  WG({ name: 'wg-mask-c5-f25', model: AMODEL('alpha-mask-c5-f25', { wireframe: false }),
    expectedOpacity: 0.25, expectedAlphaCutoff: 0.5, expectedEmpty: true,
    f0: F0(2.42), differs: 'wg-mask-fill-control', minChanged: 50 }),
  WG({ name: 'wg-mask-c5-f5', model: AMODEL('alpha-mask-c5-f5', { wireframe: false }),
    f0: F0(2.42), expectedOpacity: 0.5, expectedAlphaCutoff: 0.5,
    same: 'wg-mask-fill-control' }),
  WG({ name: 'wg-mask-c0-f0', model: AMODEL('alpha-mask-c0-f0', { wireframe: false }),
    f0: F0(2.42), expectedOpacity: 0, expectedAlphaCutoff: 0,
    same: 'wg-mask-fill-control' }),
  WG({ name: 'wg-mask-c2-f1', model: AMODEL('alpha-mask-c2-f1', { wireframe: false }),
    expectedOpacity: 1, expectedAlphaCutoff: 2, expectedEmpty: true,
    f0: F0(2.42), differs: 'wg-mask-fill-control', minChanged: 50 }),
  WG({ name: 'wg-mask-c5-f1', model: AMODEL('alpha-mask-c5-f1', { wireframe: false }),
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: 0.5,
    same: 'wg-mask-fill-control' }),
];

// ---- Masked/unlit baseColorTexture cases (both backends) ------------------
// Controls precede their dependents; every same/differs reference points at
// the SAME-backend control. expectedOpacity/expectedAlphaCutoff/expectedEmpty/
// expectedUnlit are authored explicit values, never derived from shader
// coverage math or observed runtime state.
CASES.push(...[
  { name: 'gl-masktex-control',
    model: AMODEL('gl-masktex-control', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a255.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: -1,
    expectedUnlit: false },
  { name: 'gl-masktex-opaque-a0',
    model: AMODEL('gl-masktex-opaque-a0', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a0.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: -1,
    same: 'gl-masktex-control', expectedUnlit: false },
  { name: 'gl-masktex-mask-a0',
    model: AMODEL('gl-masktex-mask-a0', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a0.png'],
    f0: F0(2.42), expectedEmpty: true,
    differs: 'gl-masktex-control', minChanged: 50,
    expectedOpacity: 1, expectedAlphaCutoff: 0.5,
    expectedUnlit: false },
  { name: 'gl-masktex-mask-a255',
    model: AMODEL('gl-masktex-mask-a255', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a255.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: 0.5,
    same: 'gl-masktex-control', expectedUnlit: false },
  { name: 'gl-masktex-mask-a128',
    model: AMODEL('gl-masktex-mask-a128', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a128.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: 0.5,
    same: 'gl-masktex-control', expectedUnlit: false },
  { name: 'gl-masktex-mask-a128-f5',
    model: AMODEL('gl-masktex-mask-a128-f5', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a128.png'],
    f0: F0(2.42), expectedEmpty: true,
    differs: 'gl-masktex-control', minChanged: 50,
    expectedOpacity: 0.5, expectedAlphaCutoff: 0.5,
    expectedUnlit: false },
  { name: 'gl-masktex-mask-c0-f0',
    model: AMODEL('gl-masktex-mask-c0-f0', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a0.png'],
    f0: F0(2.42), expectedOpacity: 0, expectedAlphaCutoff: 0,
    same: 'gl-masktex-control', expectedUnlit: false },
  { name: 'gl-masktex-unlit-control',
    model: AMODEL('gl-masktex-unlit-control', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a255.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: -1,
    expectedUnlit: true },
  { name: 'gl-masktex-unlit-discard',
    model: AMODEL('gl-masktex-unlit-discard', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a128.png'],
    f0: F0(2.42), expectedEmpty: true,
    differs: 'gl-masktex-unlit-control', minChanged: 50,
    expectedOpacity: 0.5, expectedAlphaCutoff: 0.5,
    expectedUnlit: true },
  { name: 'gl-masktex-unlit-survive',
    model: AMODEL('gl-masktex-unlit-survive', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a128.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: 0.5,
    same: 'gl-masktex-unlit-control', expectedUnlit: true },
  WG({ name: 'wg-masktex-control',
    model: AMODEL('gl-masktex-control', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a255.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: -1,
    expectedUnlit: false }),
  WG({ name: 'wg-masktex-opaque-a0',
    model: AMODEL('gl-masktex-opaque-a0', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a0.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: -1,
    same: 'wg-masktex-control', expectedUnlit: false }),
  WG({ name: 'wg-masktex-mask-a0',
    model: AMODEL('gl-masktex-mask-a0', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a0.png'],
    f0: F0(2.42), expectedEmpty: true,
    differs: 'wg-masktex-control', minChanged: 50,
    expectedOpacity: 1, expectedAlphaCutoff: 0.5,
    expectedUnlit: false }),
  WG({ name: 'wg-masktex-mask-a255',
    model: AMODEL('gl-masktex-mask-a255', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a255.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: 0.5,
    same: 'wg-masktex-control', expectedUnlit: false }),
  WG({ name: 'wg-masktex-mask-a128',
    model: AMODEL('gl-masktex-mask-a128', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a128.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: 0.5,
    same: 'wg-masktex-control', expectedUnlit: false }),
  WG({ name: 'wg-masktex-mask-a128-f5',
    model: AMODEL('gl-masktex-mask-a128-f5', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a128.png'],
    f0: F0(2.42), expectedEmpty: true,
    differs: 'wg-masktex-control', minChanged: 50,
    expectedOpacity: 0.5, expectedAlphaCutoff: 0.5,
    expectedUnlit: false }),
  WG({ name: 'wg-masktex-mask-c0-f0',
    model: AMODEL('gl-masktex-mask-c0-f0', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a0.png'],
    f0: F0(2.42), expectedOpacity: 0, expectedAlphaCutoff: 0,
    same: 'wg-masktex-control', expectedUnlit: false }),
  WG({ name: 'wg-masktex-unlit-control',
    model: AMODEL('gl-masktex-unlit-control', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a255.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: -1,
    expectedUnlit: true }),
  WG({ name: 'wg-masktex-unlit-discard',
    model: AMODEL('gl-masktex-unlit-discard', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a128.png'],
    f0: F0(2.42), expectedEmpty: true,
    differs: 'wg-masktex-unlit-control', minChanged: 50,
    expectedOpacity: 0.5, expectedAlphaCutoff: 0.5,
    expectedUnlit: true }),
  WG({ name: 'wg-masktex-unlit-survive',
    model: AMODEL('gl-masktex-unlit-survive', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a128.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: 0.5,
    same: 'wg-masktex-unlit-control', expectedUnlit: true }),
]);

// ---- Unlit-flag propagation + light-invariance cases (both backends) ------
// Authored expectations only: expectedUnlit is never derived from observed or
// runtime state. The dark cases run keyLightIntensity:0; unlit-dark must be
// byte-identical to the SAME-backend unlit control (light-independent), while
// lit-dark must differ from the SAME-backend lit control with the key light ON
CASES.push(...[
  { name: 'gl-unlit-dark',
    model: AMODEL('gl-masktex-unlit-control', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a255.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: -1,
    keyLightIntensity: 0,
    same: 'gl-masktex-unlit-control', expectedUnlit: true },
  { name: 'gl-lit-dark',
    model: AMODEL('gl-masktex-control', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a255.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: -1,
    keyLightIntensity: 0,
    differs: 'gl-masktex-control', minChanged: 50,
    expectedUnlit: false },
  WG({ name: 'wg-unlit-dark',
    model: AMODEL('gl-masktex-unlit-control', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a255.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: -1,
    keyLightIntensity: 0,
    same: 'wg-masktex-unlit-control', expectedUnlit: true }),
  WG({ name: 'wg-lit-dark',
    model: AMODEL('gl-masktex-control', { wireframe: false }),
    albedoTex: true, requiredTex: ['/tex/alb-white-a255.png'],
    f0: F0(2.42), expectedOpacity: 1, expectedAlphaCutoff: -1,
    keyLightIntensity: 0,
    differs: 'wg-masktex-control', minChanged: 50,
    expectedUnlit: false }),
]);

// ---- Direct-light specular-factor cases (both backends) ----
// Each definition below is instantiated once on WebGL and once on the required
// WebGPU backend by the loop that follows, reusing the OBJ/OBJNAMED/WG helpers.
// Expectations may be a scalar (all channels) or an authored linear RGB triple
// plus an optional per-case f90 (defaulting to 1). Metals intentionally keep
// uniform checks (their F0/F90 differ) while the image must stay identical.
[
  // Explicit white specular color at intensity 1: exactly the omitted baseline.
  { name: 'obj-spec-white', wgName: 'wg-spec-white',
    obj: OBJ({ specularColor: [1, 1, 1], specularIntensity: 1 }),
    f0: 0.04, same: 'obj-omitted', wgSame: 'wg-omit' },
  // Intensity 0: F0 and F90 both zero, image changes.
  { name: 'obj-spec-int0', wgName: 'wg-spec-int0',
    obj: OBJ({ specularIntensity: 0 }),
    f0: [0, 0, 0], f90: 0, differs: 'obj-omitted', wgDiffers: 'wg-omit', minChanged: 20 },
  // Black specular color: F0 zero but F90 stays 1, image changes.
  { name: 'obj-spec-black', wgName: 'wg-spec-black',
    obj: OBJ({ specularColor: [0, 0, 0] }),
    f0: [0, 0, 0], f90: 1, differs: 'obj-omitted', wgDiffers: 'wg-omit', minChanged: 20 },
  // Tinted specular color scales per-channel F0 linearly (0.04 * [4,1,1]).
  { name: 'obj-spec-tint', wgName: 'wg-spec-tint',
    obj: OBJ({ specularColor: [4, 1, 1] }),
    f0: [0.16, 0.04, 0.04], f90: 1, differs: 'obj-omitted', wgDiffers: 'wg-omit', minChanged: 20 },
  // HDR clamp order at default IOR 1.5: per-channel F0 is clamped to 1 BEFORE
  // the intensity scale, so 0.04*100 lands on 1 then *0.5 gives 0.5 (not 2).
  { name: 'obj-spec-hdr', wgName: 'wg-spec-hdr',
    obj: OBJ({ specularColor: [100, 0.5, 2], specularIntensity: 0.5 }),
    f0: [0.5, 0.01, 0.04], f90: 0.5, differs: 'obj-omitted', wgDiffers: 'wg-omit', minChanged: 20 },
  // Nonclipping metal baseline: metals shade from base color, so the dielectric
  // F0/F90 uniforms are still observed (0.04 / 1) while the image is stable.
  { name: 'obj-metal-spec-base', wgName: 'wg-metal-spec-base',
    obj: OBJ({ metalness: 1, roughness: 0.7, color: '#806040' }), f0: 0.04, f90: 1, base: 'metb' },
  // Metal with intensity 0: dielectric uniform F0/F90 drop to 0, but the image
  // must be EXACTLY identical to the metal baseline (no metallic F90 mixing in
  // the uploaded dielectric uniform).
  { name: 'obj-metal-spec-int0', wgName: 'wg-metal-spec-int0',
    obj: OBJ({ metalness: 1, roughness: 0.7, color: '#806040', specularIntensity: 0 }),
    f0: [0, 0, 0], f90: 0, same: 'obj-metal-spec-base', wgSame: 'wg-metal-spec-base' },
  // Metal with HDR specular factors: uniforms change (clamped+scaled), image
  // must remain EXACTLY identical to the metal baseline.
  { name: 'obj-metal-spec-hdr', wgName: 'wg-metal-spec-hdr',
    obj: OBJ({ metalness: 1, roughness: 0.7, color: '#806040',
      specularColor: [100, 2, 0.5], specularIntensity: 0.5 }),
    f0: [0.5, 0.04, 0.01], f90: 0.5, same: 'obj-metal-spec-base', wgSame: 'wg-metal-spec-base' },
  // Named material table carries the factors; the object only references the
  // material id (no duplicated factors on the object).
  { name: 'named-spec', wgName: 'wg-named-spec',
    materials: [{ id: 'dielectric', materialKind: 'standard', roughness: 0.35, metalness: 0,
      ior: 1.5, color: '#b0503c', specularColor: [0.5, 1, 1.5], specularIntensity: 1 }],
    obj: OBJNAMED, f0: [0.02, 0.04, 0.06], f90: 1,
    differs: 'obj-omitted', wgDiffers: 'wg-omit', minChanged: 20 },
  // GLB specular FACTOR cases (real importer path). Same asset family only:
  // case 1 is its own GLB baseline (default IOR, no specular extension);
  // cases 2-4 share that baseline for comparison within the same backend.
  // Omitted extension on a default-IOR GLB: importer defaults intensity 1,
  // linear white color -> F0 .04 / F90 1.
  { name: 'glb-spec-omit', wgName: 'wg-glb-spec-omit',
    model: MODEL({ src: '/models/quad-default.glb' }),
    f0: 0.04, base: 'gsdo' },
  // Explicit specularFactor 1 + specularColorFactor [1,1,1]: must render and
  // upload EXACTLY like the omitted-extension baseline.
  { name: 'glb-spec-white', wgName: 'wg-glb-spec-white',
    model: MODEL({ src: '/models/quad-spec-white.glb' }),
    f0: 0.04, same: 'glb-spec-omit', wgSame: 'wg-glb-spec-omit' },
  // Explicit specularFactor 0: F0 and F90 both zero, image changes.
  { name: 'glb-spec-zero', wgName: 'wg-glb-spec-zero',
    model: MODEL({ src: '/models/quad-spec-zero.glb' }),
    f0: 0, f90: 0, differs: 'glb-spec-omit', wgDiffers: 'wg-glb-spec-omit',
    minChanged: 20 },
  // IOR 2.42 with HDR factors served only through the importer (no model or
  // batch duplication): F0 [.5, F0(2.42)*.25, F0(2.42)], F90 .5.
  { name: 'glb-spec-ior242', wgName: 'wg-glb-spec-ior242',
    model: MODEL({ src: '/models/quad-spec-ior242.glb' }),
    f0: [0.5, F0(2.42) * 0.25, F0(2.42)], f90: 0.5,
    differs: 'glb-spec-omit', wgDiffers: 'wg-glb-spec-omit', minChanged: 20 },
  // Normal model instance of the same asset with an explicit
  // specularIntensity: 0 override: F0/F90 zero, image differs from the
  // inherited-factor render.
  { name: 'glb-spec-ior242-int0', wgName: 'wg-glb-spec-ior242-int0',
    model: MODEL({ src: '/models/quad-spec-ior242.glb', specularIntensity: 0 }),
    f0: 0, f90: 0, differs: 'glb-spec-ior242', wgDiffers: 'wg-glb-spec-ior242',
    minChanged: 20 },
  // Instanced GLB batch with specular fields OMITTED from the batch config
  // (BATCH keeps only its existing roughness/metalness overrides): the loaded
  // factors [.5, F0(2.42)*.25, F0(2.42)] / .5 must be inherited exactly.
  { name: 'glb-batch-spec-inherit', wgName: 'wg-glb-batch-spec-inherit',
    instanced: BATCH({ src: '/models/quad-spec-ior242.glb' }),
    f0: [0.5, F0(2.42) * 0.25, F0(2.42)], f90: 0.5, base: 'gsbi' },
  // Same instanced GLB with a batch specularIntensity: 0 override: F0/F90
  // zero and pixels differ from the inheriting batch.
  { name: 'glb-batch-spec-int0', wgName: 'wg-glb-batch-spec-int0',
    instanced: BATCH({ src: '/models/quad-spec-ior242.glb', specularIntensity: 0 }),
    f0: 0, f90: 0, differs: 'glb-batch-spec-inherit',
    wgDiffers: 'wg-glb-batch-spec-inherit', minChanged: 20 },
  // Specular IBL isolation: metallic 0, direct light intensity 0,
  // ambient/sky/ground intensities 0. With F0=0 the IBL specular path
  // reduces to B*F90, so specularIntensity 1 vs 0 must produce meaningfully
  // different images. Negative controls run with IBL disabled (envIntensity
  // 1 preserved so lighting matches the positive pair) and must not fetch
  // IBL products; they must render identical pixels.
  { name: 'ibl-f0zero-int1', wgName: 'wg-ibl-f0zero-int1',
    keyLightIntensity: 0, requiresIBL: true,
    materials: [{ id: 'dielectric', materialKind: 'standard', roughness: 0.35, metalness: 0,
      ior: 1.0, color: '#b0503c', specularIntensity: 1 }],
    obj: OBJNAMED, f0: 0, f90: 1,
    environment: { ambientIntensity: 0, skyIntensity: 0, groundIntensity: 0,
      envIntensity: 1, ibl: IBL_FIXTURE.descriptor } },
  { name: 'ibl-f0zero-int0', wgName: 'wg-ibl-f0zero-int0',
    keyLightIntensity: 0, requiresIBL: true,
    materials: [{ id: 'dielectric', materialKind: 'standard', roughness: 0.35, metalness: 0,
      ior: 1.0, color: '#b0503c', specularIntensity: 0 }],
    obj: OBJNAMED, f0: 0, f90: 0,
    environment: { ambientIntensity: 0, skyIntensity: 0, groundIntensity: 0,
      envIntensity: 1, ibl: IBL_FIXTURE.descriptor },
    differs: 'ibl-f0zero-int1', minChanged: 20,
    wgDiffers: 'wg-ibl-f0zero-int1' },
  { name: 'noibl-f0zero-int1', wgName: 'wg-noibl-f0zero-int1',
    keyLightIntensity: 0, noIBL: true,
    materials: [{ id: 'dielectric', materialKind: 'standard', roughness: 0.35, metalness: 0,
      ior: 1.0, color: '#b0503c', specularIntensity: 1 }],
    obj: OBJNAMED, f0: 0, f90: 1,
    environment: { ambientIntensity: 0, skyIntensity: 0, groundIntensity: 0, envIntensity: 1 } },
  { name: 'noibl-f0zero-int0', wgName: 'wg-noibl-f0zero-int0',
    keyLightIntensity: 0, noIBL: true,
    materials: [{ id: 'dielectric', materialKind: 'standard', roughness: 0.35, metalness: 0,
      ior: 1.0, color: '#b0503c', specularIntensity: 0 }],
    obj: OBJNAMED, f0: 0, f90: 0,
    environment: { ambientIntensity: 0, skyIntensity: 0, groundIntensity: 0, envIntensity: 1 },
    same: 'noibl-f0zero-int1', minChanged: 0,
    wgSame: 'wg-noibl-f0zero-int1' },
].forEach((d) => {
  CASES.push(d);
  const w = WG(Object.assign({}, d, { name: d.wgName }));
  if (d.base) w.base = 'w' + d.base;
  if (d.wgSame) { w.same = d.wgSame; } else { delete w.same; }
  if (d.wgDiffers) { w.differs = d.wgDiffers; } else { delete w.differs; }
  delete w.wgName; delete w.wgSame; delete w.wgDiffers;
  CASES.push(w);
});
// ---- Specular-intensity-ALPHA texture cases (WebGPU only, ALPHA slice) ----
// CPU factors stay at the pre-texture values (specularFactor defaults to 1),
// so the 208-byte upload assertions expect F0 .04 / F90 1 while the final
// per-pixel intensity comes from the texture ALPHA channel; those CPU factors
// are NOT the final per-pixel factors. The observed hasSpecularIntensityMap
// flag (float index 41 of the real upload) must reach 1 before capture and is
// recorded in the evidence.
[
  // alpha255 (non-white RGB): pixel-identical to the factor-1 GLB render,
  // proving a saturated alpha is neutral and the texture RGB is ignored.
  WG({ name: 'wg-tex-alpha255', model: MODEL({ src: '/models/quad-tex-alpha255.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha255.png'], f0: 0.04, f90: 1,
    same: 'wg-glb-spec-white' }),
  // alpha0 (white RGB): pixel-identical to the factor-0 GLB and visibly
  // different from factor 1.
  WG({ name: 'wg-tex-alpha0', model: MODEL({ src: '/models/quad-tex-alpha0.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha0-white.png'], f0: 0.04, f90: 1,
    same: 'wg-glb-spec-zero', differs: 'wg-glb-spec-white', minChanged: 20 }),
  // Fractional alpha 128/255 with black RGB: must match the untextured
  // specularFactor 128/255 GLB within 1 channel quantization step (the only
  // new fractional comparison uses the near comparison below) and differ
  // visibly from factor 1.
  WG({ name: 'wg-tex-alpha128-black',
    model: MODEL({ src: '/models/quad-tex-alpha128-black.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha128-black.png'], f0: 0.04, f90: 1,
    differs: 'wg-glb-spec-white', minChanged: 1, nearSame: 'wg-glb-spec-128' }),
  // Untextured fractional baseline for the near comparison above.
  WG({ name: 'wg-glb-spec-128', model: MODEL({ src: '/models/quad-spec-128.glb' }),
    f0: 0.04 * (128 / 255), f90: 128 / 255 }),
  // Identical alpha 128 with different RGB: pixel-identical to the black RGB
  // variant, proving the texture RGB is ignored.
  WG({ name: 'wg-tex-alpha128-red',
    model: MODEL({ src: '/models/quad-tex-alpha128-red.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha128-red.png'], f0: 0.04, f90: 1,
    same: 'wg-tex-alpha128-black' }),
  // Fully metallic pair: alpha 0 vs alpha 255 with identical geometry and
  // metalness must stay pixel-identical.
  WG({ name: 'wg-tex-metal-alpha0',
    model: MODEL({ src: '/models/quad-tex-metal-alpha0.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha0-white.png'], f0: 0.04, f90: 1,
    same: 'wg-tex-metal-alpha255' }),
  WG({ name: 'wg-tex-metal-alpha255',
    model: MODEL({ src: '/models/quad-tex-metal-alpha255.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha255.png'], f0: 0.04, f90: 1 }),
  // IBL-only isolation pair at ior 1 (F0 = 0 isolates F90): the same textured
  // quad geometry and the shared IBL fixture environment; alpha 0 vs alpha
  // 255 must differ.
  WG({ name: 'wg-tex-ibl-alpha0', model: MODEL({ src: '/models/quad-tex-ibl-alpha0.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha0-white.png'], f0: 0, f90: 1,
    requiresIBL: true, keyLightIntensity: 0,
    environment: { ambientIntensity: 0, skyIntensity: 0, groundIntensity: 0,
      envIntensity: 1, ibl: IBL_FIXTURE.descriptor },
    differs: 'wg-tex-ibl-alpha255', minChanged: 20 }),
  WG({ name: 'wg-tex-ibl-alpha255', model: MODEL({ src: '/models/quad-tex-ibl-alpha255.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha255.png'], f0: 0, f90: 1,
    requiresIBL: true, keyLightIntensity: 0,
    environment: { ambientIntensity: 0, skyIntensity: 0, groundIntensity: 0,
      envIntensity: 1, ibl: IBL_FIXTURE.descriptor } }),
].forEach((c) => CASES.push(c));

// ---- Specular-intensity-ALPHA texture cases (WebGL counterparts) ----------
// WebGL equivalents of ALL nine WebGPU intensity cases above, reusing the
// exact same real GLB/PNG assets. The production WebGL change declares
// u_hasSpecularIntensityMap + u_specularIntensityMap, samples the texture
// ALPHA channel only, scales the shared dielectric F0/F90 before metallic
// mixing, and keeps the CPU uniforms at the authored pre-texture values, so
// the draw-time uniform assertions below expect F0 .04 / F90 1 (or 0 / 1 for
// the IOR 1 IBL pair) exactly like the WebGPU upload assertions. The REAL
// u_hasSpecularIntensityMap uniform is observed at production PBR draw time
// (getUniformLocation tracking + getUniform) and must read true/1 within the
// bounded timeout before capture; a missing location or observation is null
// and never invented as readiness. Comparisons reuse the existing WebGL
// factor controls (glb-spec-white / glb-spec-zero), remapped from the WebGPU
// reference names; no WebGL color-texture cases are added here.
[
  // alpha255 (non-white RGB): pixel-identical to the WebGL factor-1 GLB
  // render, proving a saturated alpha is neutral and the texture RGB is
  // ignored by the sampler path.
  { name: 'gl-tex-alpha255', model: MODEL({ src: '/models/quad-tex-alpha255.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha255.png'], f0: 0.04, f90: 1,
    same: 'glb-spec-white' },
  // alpha0 (white RGB): pixel-identical to the WebGL factor-0 GLB and visibly
  // different from factor 1.
  { name: 'gl-tex-alpha0', model: MODEL({ src: '/models/quad-tex-alpha0.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha0-white.png'], f0: 0.04, f90: 1,
    same: 'glb-spec-zero', differs: 'glb-spec-white', minChanged: 20 },
  // Fractional alpha 128/255 with black RGB: must match the untextured
  // specularFactor 128/255 GLB within 1 channel quantization step and differ
  // visibly from factor 1.
  { name: 'gl-tex-alpha128-black', model: MODEL({ src: '/models/quad-tex-alpha128-black.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha128-black.png'], f0: 0.04, f90: 1,
    differs: 'glb-spec-white', minChanged: 1, nearSame: 'glb-spec-128' },
  // Untextured WebGL fractional control for the near comparison above (the
  // WebGPU block has its own wg-glb-spec-128 counterpart).
  { name: 'glb-spec-128', model: MODEL({ src: '/models/quad-spec-128.glb' }),
    f0: 0.04 * (128 / 255), f90: 128 / 255 },
  // Identical alpha 128 with different RGB: pixel-identical to the black RGB
  // variant, proving the texture RGB is ignored.
  { name: 'gl-tex-alpha128-red', model: MODEL({ src: '/models/quad-tex-alpha128-red.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha128-red.png'], f0: 0.04, f90: 1,
    same: 'gl-tex-alpha128-black' },
  // Fully metallic pair: alpha 0 vs alpha 255 with identical geometry and
  // metalness must stay pixel-identical (intensity scales dielectric F0/F90
  // before metallic mixing only).
  { name: 'gl-tex-metal-alpha0', model: MODEL({ src: '/models/quad-tex-metal-alpha0.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha0-white.png'], f0: 0.04, f90: 1,
    same: 'gl-tex-metal-alpha255' },
  { name: 'gl-tex-metal-alpha255', model: MODEL({ src: '/models/quad-tex-metal-alpha255.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha255.png'], f0: 0.04, f90: 1 },
  // IBL-only isolation pair at ior 1 (F0 = 0 isolates F90): the same textured
  // quad geometry and the shared IBL fixture environment; alpha 0 vs alpha
  // 255 must differ. WebGL IBL readiness is the existing lastDrawHasIBL
  // observation.
  { name: 'gl-tex-ibl-alpha0', model: MODEL({ src: '/models/quad-tex-ibl-alpha0.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha0-white.png'], f0: 0, f90: 1,
    requiresIBL: true, keyLightIntensity: 0,
    environment: { ambientIntensity: 0, skyIntensity: 0, groundIntensity: 0,
      envIntensity: 1, ibl: IBL_FIXTURE.descriptor },
    differs: 'gl-tex-ibl-alpha255', minChanged: 20 },
  { name: 'gl-tex-ibl-alpha255', model: MODEL({ src: '/models/quad-tex-ibl-alpha255.glb' }),
    specTex: true, requiredTex: ['/tex/spec-alpha255.png'], f0: 0, f90: 1,
    requiresIBL: true, keyLightIntensity: 0,
    environment: { ambientIntensity: 0, skyIntensity: 0, groundIntensity: 0,
      envIntensity: 1, ibl: IBL_FIXTURE.descriptor } },
].forEach((c) => CASES.push(c));

// ---- Specular-COLOR texture cases (WebGPU only, color slice) --------------
// CPU factors stay at the pre-texture authored values, so the 208-byte upload
// assertions expect the authored F0/F90 (float indices 44..47, bytes 176:192,
// unchanged) while the final per-pixel F0 comes from the sRGB-decoded color
// texture RGB; the texture ALPHA channel must not affect the color role. The
// observed hasSpecularColorMap flag (u32 index 51, byte 204) must reach 1
// before capture for every color-texture case, and BOTH flags must reach 1
// for the combined color+intensity case. All comparisons stay within the
// same quad geometry/backend/lighting family (never the OBJ fixture).
const SPEC_COLOR_ENV = () => ({ ambientIntensity: 0, skyIntensity: 0, groundIntensity: 0,
  envIntensity: 1, ibl: IBL_FIXTURE.descriptor });
[
  // 1. White color texture: exactly the existing factor-1 GLB render.
  WG({ name: 'wg-tex-color-white', model: MODEL({ src: '/models/quad-tex-color-white.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-white.png'], f0: 0.04, f90: 1,
    same: 'wg-glb-spec-white' }),
  // 2. Black color texture: equals the untextured black-color control
  //    (F0 0, F90 1) exactly and differs from the white texture. The CPU
  //    upload F0/F90 stay the authored pre-texture factors (.04 / 1): the
  //    per-pixel black is sampled only on the GPU from the color texture,
  //    never uploaded as the CPU F0.
  WG({ name: 'wg-tex-color-black', model: MODEL({ src: '/models/quad-tex-color-black.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-black.png'], f0: 0.04, f90: 1,
    same: 'wg-glb-spec-color-black', differs: 'wg-tex-color-white', minChanged: 20 }),
  WG({ name: 'wg-glb-spec-color-black', model: MODEL({ src: '/models/quad-spec-color-black.glb' }),
    f0: [0, 0, 0], f90: 1 }),
  // 3. Tinted sRGB texel RGB [128,64,255]: matches an untextured GLB whose
  //    linear color factors equal the exactly decoded RGB (within at most 1
  //    channel quantization step) and differs visibly from white.
  WG({ name: 'wg-tex-color-tint', model: MODEL({ src: '/models/quad-tex-color-tint.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-tint.png'], f0: 0.04, f90: 1,
    nearSame: 'wg-glb-spec-color-tint', differs: 'wg-tex-color-white', minChanged: 1 }),
  WG({ name: 'wg-glb-spec-color-tint', model: MODEL({ src: '/models/quad-spec-color-tint.glb' }),
    f0: [0.04 * TINT_LINEAR[0], 0.04 * TINT_LINEAR[1], 0.04 * TINT_LINEAR[2]], f90: 1 }),
  // 4. Same tinted RGB with alpha 0: must match the alpha-255 texture EXACTLY
  //    (alpha must not affect the color role; this exposes any image-decoding
  //    alpha loss).
  WG({ name: 'wg-tex-color-tint-alpha0',
    model: MODEL({ src: '/models/quad-tex-color-tint-alpha0.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-tint-alpha0.png'], f0: 0.04, f90: 1,
    same: 'wg-tex-color-tint' }),
  // 5. HDR authored color [100,50,2] at intensity 0.5 with texel RGB
  //    [64,128,255]: equals an untextured control whose factors are the
  //    authored color multiplied by the decoded RGB BEFORE clamping. The GPU
  //    upload expectations remain the pre-texture authored factors
  //    (clamp-before-intensity gives [0.5, 0.5, 0.04] / 0.5), not the final
  //    pixels.
  WG({ name: 'wg-tex-color-hdr', model: MODEL({ src: '/models/quad-tex-color-hdr.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-hdr.png'],
    f0: [0.5, 0.5, 0.04], f90: 0.5,
    nearSame: 'wg-glb-spec-color-hdr' }),
  WG({ name: 'wg-glb-spec-color-hdr', model: MODEL({ src: '/models/quad-spec-color-hdr.glb' }),
    f0: [Math.min(0.04 * 100 * HDRTEX_LINEAR[0], 1) * 0.5,
         Math.min(0.04 * 50 * HDRTEX_LINEAR[1], 1) * 0.5,
         Math.min(0.04 * 2 * HDRTEX_LINEAR[2], 1) * 0.5], f90: 0.5 }),
  // 6. Combined color + intensity textures (alpha 128/255) at authored
  //    intensity 0.5: matches an untextured control using the decoded color
  //    and intensity 0.5*128/255. Both loaded flags and both actual texture
  //    fetches are observed.
  WG({ name: 'wg-tex-color-tint-int128',
    model: MODEL({ src: '/models/quad-tex-color-tint-int128.glb' }),
    specTex: true, specColorTex: true,
    requiredTex: ['/tex/spec-color-tint-alpha128.png', '/tex/spec-alpha128-black.png'],
    f0: 0.02, f90: 0.5,
    nearSame: 'wg-glb-spec-color-int' }),
  WG({ name: 'wg-glb-spec-color-int', model: MODEL({ src: '/models/quad-spec-color-int.glb' }),
    f0: [0.04 * 0.5 * (128 / 255) * TINT_LINEAR[0], 0.04 * 0.5 * (128 / 255) * TINT_LINEAR[1],
         0.04 * 0.5 * (128 / 255) * TINT_LINEAR[2]], f90: 0.5 * (128 / 255) }),
  // 7. Authored intensity 0 plus a loaded color texture: exactly the existing
  //    factor-0 GLB render (authored intensity is retained through the
  //    texture path; the combined F90 is 0 so the color texel cannot revive
  //    the specular response).
  WG({ name: 'wg-tex-color-int0', model: MODEL({ src: '/models/quad-tex-color-int0.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-white.png'], f0: 0, f90: 0,
    same: 'wg-glb-spec-zero' }),
  // 8. Fully metallic textured-color case: the metal branch ignores the
  //    dielectric lane, so the image equals the existing metallic alpha-255
  //    texture case exactly while the dielectric uniforms stay 0.04 / 1.
  WG({ name: 'wg-tex-color-metal', model: MODEL({ src: '/models/quad-tex-color-metal.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-tint.png'], f0: 0.04, f90: 1,
    same: 'wg-tex-metal-alpha255' }),
  // 9. Isolated IBL color pair: same real IBL fixture, direct/ambient/sky/
  //    ground intensities 0, authored color [4,1,1]. Black vs white texels
  //    must differ; the black case must match the existing same-quad
  //    wg-tex-ibl-alpha255 case EXACTLY (IOR 1 gives F0=0 and F90=1 there;
  //    the black texel forces F0=0 here and F90 stays authored 1, so the IBL
  //    response B*F90 is identical and F90 remains active for black color).
  WG({ name: 'wg-tex-ibl-color-black',
    model: MODEL({ src: '/models/quad-tex-ibl-color-black.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-black.png'],
    f0: [0.16, 0.04, 0.04], f90: 1,
    requiresIBL: true, keyLightIntensity: 0, environment: SPEC_COLOR_ENV(),
    same: 'wg-tex-ibl-alpha255' }),
  WG({ name: 'wg-tex-ibl-color-white',
    model: MODEL({ src: '/models/quad-tex-ibl-color-white.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-white.png'],
    f0: [0.16, 0.04, 0.04], f90: 1,
    requiresIBL: true, keyLightIntensity: 0, environment: SPEC_COLOR_ENV(),
    differs: 'wg-tex-ibl-color-black', minChanged: 20 }),
].forEach((c) => CASES.push(c));

// ---- Specular-COLOR texture cases (WebGL counterparts) --------------------
// WebGL equivalents of ALL fourteen WebGPU color cases above, reusing the
// exact same real GLB/PNG assets. The production WebGL change declares
// u_hasSpecularColorMap + u_specularColorMap and samples the sRGB-decoded
// texture RGB as the per-pixel specular color (the texture ALPHA channel
// never affects the color role), while the CPU uniforms stay at the authored
// pre-texture values, so the draw-time uniform assertions below expect the
// authored F0/F90 exactly like the WebGPU upload assertions. The REAL
// u_hasSpecularColorMap uniform is observed at production PBR draw time
// (getUniformLocation tracking + getUniform) and must read true/1 within the
// bounded timeout before capture; a missing location or observation is null
// and never invented as readiness. Combined color+intensity readiness
// requires BOTH draw-time flags true in the SAME sT.ior snapshot/PBR draw.
// Comparisons reuse the existing WebGL factor controls (glb-spec-white /
// glb-spec-zero / gl-tex-metal-alpha255 / gl-tex-ibl-alpha255) and the new
// untextured GL color controls below, remapped from the WebGPU names; all
// comparisons stay within the same quad geometry/backend/lighting family.
[
  // White color texture: exactly the existing WebGL factor-1 GLB render.
  { name: 'gl-tex-color-white', model: MODEL({ src: '/models/quad-tex-color-white.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-white.png'], f0: 0.04, f90: 1,
    same: 'glb-spec-white' },
  // Black color texture: equals the untextured black-color control exactly
  // and differs from the white texture. The CPU draw-time F0/F90 stay the
  // authored pre-texture factors (.04 / 1): the per-pixel black is sampled
  // only on the GPU from the color texture, never uploaded as the CPU F0.
  { name: 'gl-tex-color-black', model: MODEL({ src: '/models/quad-tex-color-black.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-black.png'], f0: 0.04, f90: 1,
    same: 'glb-spec-color-black', differs: 'gl-tex-color-white', minChanged: 20 },
  { name: 'glb-spec-color-black', model: MODEL({ src: '/models/quad-spec-color-black.glb' }),
    f0: [0, 0, 0], f90: 1 },
  // Tinted sRGB texel RGB [128,64,255]: matches an untextured GLB whose
  // linear color factors equal the exactly decoded RGB (within at most 1
  // channel quantization step) and differs visibly from white.
  { name: 'gl-tex-color-tint', model: MODEL({ src: '/models/quad-tex-color-tint.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-tint.png'], f0: 0.04, f90: 1,
    nearSame: 'glb-spec-color-tint', differs: 'gl-tex-color-white', minChanged: 1 },
  { name: 'glb-spec-color-tint', model: MODEL({ src: '/models/quad-spec-color-tint.glb' }),
    f0: [0.04 * TINT_LINEAR[0], 0.04 * TINT_LINEAR[1], 0.04 * TINT_LINEAR[2]], f90: 1 },
  // Same tinted RGB with alpha 0: must match the alpha-255 texture EXACTLY
  // (alpha must not affect the color role; this exposes any image-decoding
  // alpha loss).
  { name: 'gl-tex-color-tint-alpha0',
    model: MODEL({ src: '/models/quad-tex-color-tint-alpha0.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-tint-alpha0.png'], f0: 0.04, f90: 1,
    same: 'gl-tex-color-tint' },
  // HDR authored color [100,50,2] at intensity 0.5 with texel RGB
  // [64,128,255]: equals an untextured control whose factors are the
  // authored color multiplied by the decoded RGB BEFORE clamping. The
  // draw-time uniform expectations remain the pre-texture authored factors
  // (clamp-before-intensity gives [0.5, 0.5, 0.04] / 0.5), not the pixels.
  { name: 'gl-tex-color-hdr', model: MODEL({ src: '/models/quad-tex-color-hdr.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-hdr.png'],
    f0: [0.5, 0.5, 0.04], f90: 0.5,
    nearSame: 'glb-spec-color-hdr' },
  { name: 'glb-spec-color-hdr', model: MODEL({ src: '/models/quad-spec-color-hdr.glb' }),
    f0: [Math.min(0.04 * 100 * HDRTEX_LINEAR[0], 1) * 0.5,
         Math.min(0.04 * 50 * HDRTEX_LINEAR[1], 1) * 0.5,
         Math.min(0.04 * 2 * HDRTEX_LINEAR[2], 1) * 0.5], f90: 0.5 },
  // Combined color + intensity textures (alpha 128/255) at authored
  // intensity 0.5: matches an untextured control using the decoded color
  // and intensity 0.5*128/255. BOTH draw-time flags (same snapshot) and both
  // actual texture fetches are observed.
  { name: 'gl-tex-color-tint-int128',
    model: MODEL({ src: '/models/quad-tex-color-tint-int128.glb' }),
    specTex: true, specColorTex: true,
    requiredTex: ['/tex/spec-color-tint-alpha128.png', '/tex/spec-alpha128-black.png'],
    f0: 0.02, f90: 0.5,
    nearSame: 'glb-spec-color-int' },
  { name: 'glb-spec-color-int', model: MODEL({ src: '/models/quad-spec-color-int.glb' }),
    f0: [0.04 * 0.5 * (128 / 255) * TINT_LINEAR[0], 0.04 * 0.5 * (128 / 255) * TINT_LINEAR[1],
         0.04 * 0.5 * (128 / 255) * TINT_LINEAR[2]], f90: 0.5 * (128 / 255) },
  // Authored intensity 0 plus a loaded color texture: exactly the existing
  // factor-0 GLB render (authored intensity is retained through the texture
  // path; the combined F90 is 0 so the color texel cannot revive the
  // specular response).
  { name: 'gl-tex-color-int0', model: MODEL({ src: '/models/quad-tex-color-int0.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-white.png'], f0: 0, f90: 0,
    same: 'glb-spec-zero' },
  // Fully metallic textured-color case: the metal branch ignores the
  // dielectric lane, so the image equals the existing WebGL metallic
  // alpha-255 texture case exactly while the dielectric uniforms stay .04/1.
  { name: 'gl-tex-color-metal', model: MODEL({ src: '/models/quad-tex-color-metal.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-tint.png'], f0: 0.04, f90: 1,
    same: 'gl-tex-metal-alpha255' },
  // Isolated IBL color pair: same real IBL fixture, direct/ambient/sky/
  // ground intensities 0, authored color [4,1,1]. Black vs white texels must
  // differ; the black case must match the existing same-quad
  // gl-tex-ibl-alpha255 case EXACTLY (IOR 1 gives F0=0 and F90=1 there; the
  // black texel forces F0=0 here and F90 stays authored 1, so the IBL
  // response B*F90 is identical and F90 remains active for black color).
  { name: 'gl-tex-ibl-color-black',
    model: MODEL({ src: '/models/quad-tex-ibl-color-black.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-black.png'],
    f0: [0.16, 0.04, 0.04], f90: 1,
    requiresIBL: true, keyLightIntensity: 0, environment: SPEC_COLOR_ENV(),
    same: 'gl-tex-ibl-alpha255' },
  { name: 'gl-tex-ibl-color-white',
    model: MODEL({ src: '/models/quad-tex-ibl-color-white.glb' }),
    specColorTex: true, requiredTex: ['/tex/spec-color-white.png'],
    f0: [0.16, 0.04, 0.04], f90: 1,
    requiresIBL: true, keyLightIntensity: 0, environment: SPEC_COLOR_ENV(),
    differs: 'gl-tex-ibl-color-black', minChanged: 20 },
].forEach((c) => CASES.push(c));
// ---- Real-browser shadow-budget cases: the existing OBJ fixture with
// castShadow/receiveShadow, two directional castShadow lights each requesting
// 4 cascades at size 256 with distinct directions, explicit camera near/far,
// and the served envMap /tex/spec-color-white.png. Each case forces the GL
// MAX_TEXTURE_IMAGE_UNITS value reported to the engine (16 or 32) via a
// strictly forwarding getParameter wrapper, exercising the shadow-map
// allocator's unit budget on real GL. This tests allocator limits only; it is
// NOT an actual 16-unit hardware certification.
[
  { name: 'gl-shadow-budget-16',
    obj: OBJ({ castShadow: true, receiveShadow: true }),
    shadowBudget: 16, f0: 0.04, f90: 1,
    lights: [
      { id: 'shadowKey', kind: 'directional', intensity: 1.2,
        directionX: 0.5, directionY: 1, directionZ: 0.5,
        castShadow: true, shadowCascades: 4, shadowSize: 256 },
      { id: 'shadowFill', kind: 'directional', intensity: 0.8,
        directionX: -0.6, directionY: 0.8, directionZ: -0.4,
        castShadow: true, shadowCascades: 4, shadowSize: 256 },
    ],
    cameraNear: 0.1, cameraFar: 100,
    environment: { envMap: '/tex/spec-color-white.png' },
    requiredTex: ['/tex/spec-color-white.png'] },
  { name: 'gl-shadow-budget-32',
    obj: OBJ({ castShadow: true, receiveShadow: true }),
    shadowBudget: 32, f0: 0.04, f90: 1,
    lights: [
      { id: 'shadowKey', kind: 'directional', intensity: 1.2,
        directionX: 0.5, directionY: 1, directionZ: 0.5,
        castShadow: true, shadowCascades: 4, shadowSize: 256 },
      { id: 'shadowFill', kind: 'directional', intensity: 0.8,
        directionX: -0.6, directionY: 0.8, directionZ: -0.4,
        castShadow: true, shadowCascades: 4, shadowSize: 256 },
    ],
    cameraNear: 0.1, cameraFar: 100,
    environment: { envMap: '/tex/spec-color-white.png' },
    requiredTex: ['/tex/spec-color-white.png'] },
].forEach((c) => CASES.push(c));

function shadowCase(name, webgpu, mat) {
  return {
    name, webgpu,
    lights: [{ id: 'key', kind: 'directional', intensity: 1.2,
      directionX: 0.7, directionY: -0.7, directionZ: -1,
      castShadow: true, shadowCascades: 1, shadowSize: 512 }],
    objects: [
      Object.assign({ id: 'caster', kind: 'box',
        width: 0.55, height: 0.55, depth: 0.15,
        x: -0.55, y: 0.35, z: 0.5,
        materialKind: 'standard', color: '#ff0000',
        wireframe: false, ior: 1.5, roughness: 1, metalness: 0,
        castShadow: true, receiveShadow: false }, mat),
      { id: 'receiver', kind: 'box', width: 3, height: 2.2, depth: 0.1,
        x: 0, y: 0, z: -0.5,
        materialKind: 'standard', color: '#ffffff',
        wireframe: false, ior: 1.5, roughness: 1, metalness: 0,
        castShadow: false, receiveShadow: true },
    ],
    shadowROI: { x: 125, y: 99, width: 50, height: 35 },
  };
}
[false, true].forEach((webgpu) => {
  const pfx = webgpu ? 'wg-shadow-' : 'gl-shadow-';
  const push = (name, mat, extra) => CASES.push(Object.assign(
    shadowCase(pfx + name, webgpu, mat || {}), { f0: 0.04, f90: 1 },
    extra || {}));
  push('control');
  push('no-cast', { castShadow: false },
    { differs: pfx + 'control', minChanged: 50 });
  push('factor-discard', { opacity: 0.25, alphaCutoff: 0.5 },
    { same: pfx + 'no-cast' });
  push('factor-survive', { opacity: 0.5, alphaCutoff: 0.5 },
    { same: pfx + 'control' });
  push('tex-discard',
    { opacity: 1, alphaCutoff: 0.5, texture: '/tex/alb-white-a0.png' },
    { same: pfx + 'no-cast', requiredTex: ['/tex/alb-white-a0.png'] });
  push('tex-survive',
    { opacity: 1, alphaCutoff: 0.5, texture: '/tex/alb-white-a255.png' },
    { same: pfx + 'control', requiredTex: ['/tex/alb-white-a255.png'] });
  push('zero',
    { opacity: 0, alphaCutoff: 0, texture: '/tex/alb-white-a0.png' },
    { same: pfx + 'control', requiredTex: ['/tex/alb-white-a0.png'] });
  push('opaque-a0',
    { opacity: 1, texture: '/tex/alb-white-a0.png' },
    { same: pfx + 'control', requiredTex: ['/tex/alb-white-a0.png'] });
  push('factor-tex',
    { opacity: 0.5, alphaCutoff: 0.5, texture: '/tex/alb-white-a128.png' },
    { same: pfx + 'no-cast', requiredTex: ['/tex/alb-white-a128.png'] });
  push('threshold',
    { opacity: 1, alphaCutoff: 0.5, texture: '/tex/alb-white-a128.png' },
    { same: pfx + 'control', requiredTex: ['/tex/alb-white-a128.png'] });
});

// ---- Alpha-mask ROUTING defaults: direct built-in masked meshes on BOTH
// backends. One single standard-material, wireframe:false OBJ mesh per case
// (common shape/color/roughness/IOR); no imported GLB, because glTF already
// authors an explicit pass. All expectations are authored constants, never
// computed from runtime values. New native depth/blend observations gate
// ONLY this group.
const MASKROUTE_MAT = { color: '#3a7bd5', roughness: 0.35, metalness: 0, ior: 1.5 };
[false, true].forEach((webgpu) => {
  const pfx = webgpu ? 'wg-maskroute-' : 'gl-maskroute-';
  const push = (name, objExtra, extra) => CASES.push(Object.assign({
    name: pfx + name, webgpu, f0: 0.04, f90: 1,
    obj: OBJ(Object.assign({}, MASKROUTE_MAT, objExtra)),
  }, extra));
  // Opaque control: direct built-in opaque PBR route, no authored cutoff
  // (uniform/upload sentinel -1), depth-writing, no blend.
  push('opaque', {},
    { expectedOpacity: 1, expectedAlphaCutoff: -1,
      expectedDepthWrite: true, expectedBlend: false });
  // Built-in MASK inputs (.5/.5) still route as an opaque, depth-writing
  // PBR draw (no blend pass); pixels match the opaque control.
  push('mask-c5-f5', { opacity: 0.5, alphaCutoff: 0.5 },
    { expectedOpacity: 0.5, expectedAlphaCutoff: 0.5,
      expectedDepthWrite: true, expectedBlend: false, same: pfx + 'opaque' });
  // Zero/zero: alpha 0 is not below cutoff 0, so nothing is discarded and
  // the draw survives on the opaque route, matching the control.
  push('mask-c0-f0', { opacity: 0, alphaCutoff: 0 },
    { expectedOpacity: 0, expectedAlphaCutoff: 0,
      expectedDepthWrite: true, expectedBlend: false, same: pfx + 'opaque' });
  // Factor .25 with cutoff .5: every fragment discarded, strict empty image.
  push('discard-c5-f25', { opacity: 0.25, alphaCutoff: 0.5 },
    { expectedOpacity: 0.25, expectedAlphaCutoff: 0.5,
      expectedDepthWrite: true, expectedBlend: false, expectedEmpty: true,
      differs: pfx + 'opaque', minChanged: 1 });
  // Explicit renderPass:'alpha' control: blended route, no depth write,
  // sentinel cutoff, opaque factor 1.
  push('alpha-opaque', { opacity: 1, renderPass: 'alpha' },
    { expectedOpacity: 1, expectedAlphaCutoff: -1,
      expectedDepthWrite: false, expectedBlend: true });
  // MASK inputs on the explicit alpha route: surviving fragments force
  // output alpha 1 in the built-in shader, so pixels match the alpha
  // control with factor 1; routing (no depth write, blend on) is identical.
  push('alpha-mask-c5-f5', { opacity: 0.5, alphaCutoff: 0.5, renderPass: 'alpha' },
    { expectedOpacity: 0.5, expectedAlphaCutoff: 0.5,
      expectedDepthWrite: false, expectedBlend: true, same: pfx + 'alpha-opaque' });
  // Mask disabled (explicit alphaCutoff null): fractional opacity routes to
  // the blended alpha pass; cutoff uniform/upload sentinel -1.
  push('mask-disabled', { opacity: 0.5, alphaCutoff: null },
    { expectedOpacity: 0.5, expectedAlphaCutoff: -1,
      expectedDepthWrite: false, expectedBlend: true });
  // CSS-authored cutoff: the raw input stays the string
  // var(--mask-cutoff, 0.5); the runtime effective cutoff is the numeric
  // 0.5 resolved through the CSS var fallback (no htmlFor change needed).
  // Still the opaque route.
  push('mask-css-cutoff', { opacity: 0.5, alphaCutoff: 'var(--mask-cutoff, 0.5)' },
    { expectedOpacity: 0.5, expectedAlphaCutoff: 0.5,
      expectedDepthWrite: true, expectedBlend: false, same: pfx + 'opaque' });
});
// ---- Instanced-shadow receiver-pixel cases (both backends) ----
// Every existing case, timeout, threshold and assertion is unchanged. The
// caster is a real primitive instancedMeshes entry (NOT instancedGLBMeshes)
// with count 1, a FLAT column-major 4x4 translation transform and FLAT
// per-instance colors (the normalizer does not flatten nested arrays); the
// receiver is the same static box geometry/light/camera/ROI family as
// shadowCase, with the receiver ROI kept separate from the caster color so
// the same/differs comparisons prove real received shadows, not caster
// pixels.
function instShadowCase(name, webgpu, inst, extra) {
  const caster = Object.assign({
    id: 'inst-caster', count: 1, kind: 'box',
    width: 0.55, height: 0.55, depth: 0.15,
    materialKind: 'standard', color: '#2040c0',
    wireframe: false, ior: 1.5, roughness: 1, metalness: 0,
    castShadow: true, receiveShadow: false,
    // Flat column-major 4x4 translation (tx,ty,tz at indices 12..14); tx
    // -0.55 aligns the caster with the shadowCase receiver ROI.
    transforms: [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, -0.55, 0.35, 0.5, 1],
    colors: [0.2, 0.4, 1, 1],
  }, inst || {});
  return Object.assign({
    name, webgpu, instancedShadow: true, f0: 0.04, f90: 1,
    lights: [{ id: 'key', kind: 'directional', intensity: 1.2,
      directionX: 0.7, directionY: -0.7, directionZ: -1,
      castShadow: true, shadowCascades: 1, shadowSize: 512 }],
    instancedMeshes: [caster],
    objects: [
      { id: 'receiver', kind: 'box', width: 3, height: 2.2, depth: 0.1,
        x: 0, y: 0, z: -0.5,
        materialKind: 'standard', color: '#ffffff',
        wireframe: false, ior: 1.5, roughness: 1, metalness: 0,
        castShadow: false, receiveShadow: true },
    ],
    shadowROI: { x: 125, y: 99, width: 50, height: 35 },
  }, extra || {});
}
[false, true].forEach((webgpu) => {
  const pfx = webgpu ? 'wg-instshadow-' : 'gl-instshadow-';
  const push = (name, inst, extra) => CASES.push(instShadowCase(
    pfx + name, webgpu, inst || {}, extra || {}));
  push('control');
  push('no-cast', { castShadow: false },
    { expectInstShadowDraws: false, differs: pfx + 'control', minChanged: 50 });
  push('cast-omit', null,
    { expectInstShadowDraws: false, same: pfx + 'no-cast' });
  // cast-omit must remove the field entirely from the completed caster
  // object (the production check treats undefined and false alike).
  delete CASES[CASES.length - 1].instancedMeshes[0].castShadow;
  push('mask-discard', { opacity: 0.25, alphaCutoff: 0.5 },
    { same: pfx + 'no-cast' });
  push('mask-survive', { opacity: 0.5, alphaCutoff: 0.5 },
    { same: pfx + 'control' });
  push('inst-alpha-discard',
    { opacity: 1, alphaCutoff: 0.5, colors: [0.2, 0.4, 1, 0.25] },
    { same: pfx + 'no-cast' });
  push('inst-alpha-survive',
    { opacity: 1, alphaCutoff: 0.5, colors: [0.2, 0.4, 1, 0.5] },
    { same: pfx + 'control' });
  push('zero-zero',
    { opacity: 0, alphaCutoff: 0, colors: [0.2, 0.4, 1, 0] },
    { same: pfx + 'control' });
  push('tex-discard',
    { opacity: 1, alphaCutoff: 0.5, texture: '/tex/alb-white-a0.png' },
    { same: pfx + 'no-cast', requiredTex: ['/tex/alb-white-a0.png'] });
  push('tex-survive',
    { opacity: 1, alphaCutoff: 0.5, texture: '/tex/alb-white-a255.png' },
    { same: pfx + 'control', requiredTex: ['/tex/alb-white-a255.png'] });
  push('combined-discard',
    { opacity: 0.5, alphaCutoff: 0.5, colors: [0.2, 0.4, 1, 0.5] },
    { same: pfx + 'no-cast' });
  push('cutoff2', { opacity: 1, alphaCutoff: 2 },
    { same: pfx + 'no-cast' });
});
const byName = {};
// Verified typed AlphaCutoff fixture: one JSON object on stdout carrying
// exactly five same-geometry standard-material scenes lowered through the
// real Go Props{Graph}.SceneIR pipeline plus three real DiffCommands
// transitions. A missing or invalid fixture is fatal: no probe runs
// without it. The generated Go objects are used verbatim — no manual
// alteration of object or material field data.
let MASK_FIXTURE = null;
try {
  MASK_FIXTURE = JSON.parse(execFileSync('go',
    ['run', './client/js/testdata/alpha-mask-typed-fixture'],
    { cwd: REPO, encoding: 'utf8', timeout: 60000 }));
  if (!MASK_FIXTURE || MASK_FIXTURE.schema !== 'gosx.alpha-cutoff.fixture.v1') {
    throw new Error('bad fixture schema');
  }
  const maskSceneNames = Object.keys(MASK_FIXTURE.scenes || {}).sort();
  if (JSON.stringify(maskSceneNames) !==
      JSON.stringify(['disabled', 'discard', 'mask', 'opaque', 'zero'])) {
    throw new Error('expected exactly five fixture scenes');
  }
  for (const n of maskSceneNames) {
    const s = MASK_FIXTURE.scenes[n];
    if (!s || !Array.isArray(s.objects) || s.objects.length !== 1) {
      throw new Error('scene ' + n + ': expected exactly one object');
    }
    if (!s.objects[0] || s.objects[0].id !== 'typed-mask') {
      throw new Error('scene ' + n + ': missing typed-mask object');
    }
  }
  const maskTransitionNames = (MASK_FIXTURE.transitions || [])
    .map((t) => t && t.name).sort();
  if (JSON.stringify(maskTransitionNames) !==
      JSON.stringify(['absent-to-zero', 'mask-to-absent', 'mask-to-disabled'])) {
    throw new Error('expected exactly three fixture transitions');
  }
} catch (e) {
  console.error('alpha-mask-typed-fixture failed: ' + ((e && e.message) || e));
  process.exit(2);
}
// ---- Typed AlphaCutoff ROUTING: the real Go fixture objects on BOTH
// backends. Each case uses the generated fixture object verbatim (same
// geometry/color/roughness/IOR; no raw-JS object substitution, no material
// rewrites). All expectations are authored constants from the fixture
// contract, never computed from runtime values. The existing native
// depth/blend/uniform/pixel gates above apply unchanged to this group.
// Note: discard changes PIXELS only — its routing stays the depth-writing
// opaque draw, exactly like the opaque control.
[false, true].forEach((webgpu) => {
  const pfx = webgpu ? 'wg-typedmask-' : 'gl-typedmask-';
  const push = (suffix, sceneName, extra) => CASES.push(Object.assign({
    name: pfx + suffix, webgpu, f0: 0.04, f90: 1,
    obj: MASK_FIXTURE.scenes[sceneName].objects[0],
  }, extra));
  // Opaque control: no authored cutoff (sentinel -1), depth-writing,
  // no blend, factor 1.
  push('opaque', 'opaque',
    { expectedOpacity: 1, expectedAlphaCutoff: -1,
      expectedDepthWrite: true, expectedBlend: false });
  // Mask .5/.5: opaque, depth-writing PBR draw; pixels match the control.
  push('mask', 'mask',
    { expectedOpacity: 0.5, expectedAlphaCutoff: 0.5,
      expectedDepthWrite: true, expectedBlend: false, same: pfx + 'opaque' });
  // Zero/zero: alpha 0 is not below cutoff 0; draw survives on the opaque
  // route, matching the control.
  push('zero', 'zero',
    { expectedOpacity: 0, expectedAlphaCutoff: 0,
      expectedDepthWrite: true, expectedBlend: false, same: pfx + 'opaque' });
  // Cutoff disabled (null): fractional opacity routes to the blended alpha
  // pass; sentinel cutoff.
  push('disabled', 'disabled',
    { expectedOpacity: 0.5, expectedAlphaCutoff: -1,
      expectedDepthWrite: false, expectedBlend: true,
      differs: pfx + 'opaque', minChanged: 1 });
  // Discard 1/2: every fragment discarded, strict empty image, still the
  // depth-writing opaque route (routing unchanged from the control).
  push('discard', 'discard',
    { expectedOpacity: 1, expectedAlphaCutoff: 2,
      expectedDepthWrite: true, expectedBlend: false, expectedEmpty: true,
      differs: pfx + 'opaque', minChanged: 1 });
});
CASES.forEach((c) => { byName[c.name] = c; });

// ---- Native skinned-shadow regression slice (both backends, 16 cases) ----
// Pure fixture helper is required directly from its testdata module: the
// generated GLBs are validated here (chunk headers, accessor byte bounds,
// skin, indices, outward triangle winding, materials, clip) before any case
// is registered. A missing or invalid fixture is fatal.
let SKIN_FIXTURE = null;
try {
  const skFixtureModule = require(path.join(REPO, 'client', 'js', 'testdata',
    'skinned-shadow-fixture.cjs'));
  SKIN_FIXTURE = skFixtureModule.buildSkinnedShadowFixture();
  if (!SKIN_FIXTURE || !SKIN_FIXTURE.rest || !SKIN_FIXTURE.pose || !SKIN_FIXTURE.meta) {
    throw new Error('incomplete skinned-shadow fixture payload');
  }
  skFixtureModule.validateSkinnedShadowGLB(Buffer.from(SKIN_FIXTURE.rest, 'base64'), false);
  skFixtureModule.validateSkinnedShadowGLB(Buffer.from(SKIN_FIXTURE.pose, 'base64'), true);
} catch (e) {
  console.error('skinned-shadow-fixture failed: ' + ((e && e.message) || e));
  process.exit(2);
}

// Static order per backend (8): empty(receiver only), ref-rest(unskinned box
// at the baked caster location), ref-pose(box at +0.8x), rest(real skinned
// GLB, no animation), pose(real GLB with the constant clip played at load),
// mask-survive(.5 alpha /.5 cutoff on the posed skinned GLB), mask-discard
// (.25/.5), live(rest -> play pose -> stop -> rest on the SAME mount via the
// public hub event). Same/differs references always name the SAME backend.
function addSkinnedShadowCases() {
  const ROI = { x: 125, y: 99, width: 95, height: 35 };
  const SK_LIGHTS = [{ id: 'key', kind: 'directional', intensity: 1.2,
    directionX: 0.7, directionY: -0.7, directionZ: -1,
    castShadow: true, shadowCascades: 1, shadowSize: 512 }];
  const SK_RECEIVER = { id: 'receiver', kind: 'box', width: 3, height: 2.2,
    depth: 0.1, x: 0, y: 0, z: -0.5, materialKind: 'standard', color: '#ffffff',
    wireframe: false, ior: 1.5, roughness: 1, metalness: 0,
    castShadow: false, receiveShadow: true };
  const refCaster = (x) => ({ id: 'ref-caster', kind: 'box', width: 0.55,
    height: 0.55, depth: 0.15, x, y: 0.35, z: 0.5, materialKind: 'standard',
    color: '#ff0000', wireframe: false, ior: 1.5, roughness: 1, metalness: 0,
    castShadow: true, receiveShadow: false });
  const skModel = (glb, extra) => Object.assign({ id: 'skin-caster',
    src: '/models/skinned-box-' + glb + '.glb', wireframe: false,
    roughness: 1, metalness: 0, castShadow: true, receiveShadow: false },
    extra || {});
  [false, true].forEach((webgpu) => {
    const pfx = webgpu ? 'wg-sk-' : 'gl-sk-';
    const mk = (name, extra) => CASES.push(Object.assign({
      name: pfx + name, webgpu, skinnedShadowGroup: true, f0: 0.04, f90: 1,
      lights: SK_LIGHTS, objects: [SK_RECEIVER],
      shadowROI: { x: ROI.x, y: ROI.y, width: ROI.width, height: ROI.height },
    }, extra || {}));
    mk('empty');
    mk('ref-rest', { objects: [SK_RECEIVER, refCaster(-0.55)],
      differs: pfx + 'empty', minChanged: 50 });
    mk('ref-pose', { objects: [SK_RECEIVER, refCaster(0.25)],
      differs: pfx + 'ref-rest', minChanged: 50 });
    mk('rest', { model: skModel('rest'), skinnedGlb: 'rest',
      same: pfx + 'ref-rest', differs: pfx + 'empty', minChanged: 50 });
    mk('pose', { model: skModel('pose', { animation: 'pose', loop: true }),
      skinnedGlb: 'pose', skinnedExpectPose: true, skinnedExpectMixer: true,
      same: pfx + 'ref-pose', differs: pfx + 'empty', minChanged: 50 });
    mk('mask-survive', { model: skModel('pose', { animation: 'pose', loop: true,
        opacity: 0.5, alphaCutoff: 0.5 }),
      skinnedGlb: 'pose', skinnedExpectPose: true, skinnedExpectMixer: true,
      skinnedMaskOpacity: 0.5, skinnedMaskCutoff: 0.5,
      same: pfx + 'ref-pose' });
    mk('mask-discard', { model: skModel('pose', { animation: 'pose', loop: true,
        opacity: 0.25, alphaCutoff: 0.5 }),
      skinnedGlb: 'pose', skinnedExpectPose: true, skinnedExpectMixer: true,
      skinnedMaskOpacity: 0.25, skinnedMaskCutoff: 0.5,
      same: pfx + 'empty', differs: pfx + 'ref-pose', minChanged: 50 });
    mk('live', { model: skModel('pose', { live: ['skin-pose'] }),
      skinnedGlb: 'pose', skinnedLive: true, skinnedExpectMixer: true,
      same: pfx + 'ref-rest' });
  });
}
addSkinnedShadowCases();
CASES.forEach((c) => { byName[c.name] = c; });

// ---- Typed instanced-shadow lifecycle fixture (multi-instance, both
// backends) ----
// Verified Go fixture: one JSON object on stdout carrying exactly nine
// same-lighting SceneIR scenes (empty, reference-left, reference-right,
// both, left, right, moved-opaque, moved, one) lowered through the real
// scene.Props{Graph}.SceneIR pipeline plus six chained DiffCommands
// transitions whose commands are all kind 8 (instancedMeshes). A missing or
// invalid fixture is fatal: no probe runs without it. The old alpha-mask
// typed fixture is left byte-for-byte unchanged.
let INST_FIXTURE = null;
try {
  INST_FIXTURE = JSON.parse(execFileSync('go',
    ['run', './client/js/testdata/instanced-shadow-typed-fixture'],
    { cwd: REPO, encoding: 'utf8', timeout: 60000 }));
  if (!INST_FIXTURE || INST_FIXTURE.schema !== 'gosx.instanced-shadow.fixture.v1') {
    throw new Error('bad fixture schema');
  }
  const instSceneNames = Object.keys(INST_FIXTURE.scenes || {}).sort();
  if (JSON.stringify(instSceneNames) !== JSON.stringify([
      'both', 'empty', 'left', 'moved', 'moved-opaque', 'one',
      'reference-left', 'reference-right', 'right'])) {
    throw new Error('expected exactly nine fixture scenes');
  }
  const INST_SCENE_COUNTS = { both: 2, left: 2, right: 2, 'moved-opaque': 2,
    moved: 2, one: 1, empty: 0, 'reference-left': 0, 'reference-right': 0 };
  for (const n of instSceneNames) {
    const sc = INST_FIXTURE.scenes[n];
    if (!sc || !Array.isArray(sc.objects) || sc.objects.length < 1 ||
        !Array.isArray(sc.lights) || sc.lights.length < 1) {
      throw new Error('scene ' + n + ': missing generated objects/lights');
    }
    const wantBatches = INST_SCENE_COUNTS[n] > 0 ? 1 : 0;
    const im = Array.isArray(sc.instancedMeshes) ? sc.instancedMeshes : [];
    if (im.length !== wantBatches) {
      throw new Error('scene ' + n + ': expected ' + wantBatches +
        ' instancedMeshes entries, got ' + im.length);
    }
    if (wantBatches === 1 && Number(im[0] && im[0].count) !== INST_SCENE_COUNTS[n]) {
      throw new Error('scene ' + n + ': instancedMeshes count mismatch');
    }
  }
  const instTransitions = Array.isArray(INST_FIXTURE.transitions)
    ? INST_FIXTURE.transitions : [];
  const wantChain = [['both', 'left'], ['left', 'right'], ['right', 'moved'],
    ['moved', 'one'], ['one', 'empty'], ['empty', 'both']];
  if (instTransitions.length !== 6) {
    throw new Error('expected exactly six fixture transitions');
  }
  instTransitions.forEach((t, i) => {
    if (!t || t.name !== (wantChain[i][0] + '-to-' + wantChain[i][1]) ||
        t.from !== wantChain[i][0] || t.to !== wantChain[i][1] ||
        !Array.isArray(t.commands) || t.commands.length !== 1 ||
        !t.commands.every((cmd) => cmd && cmd.kind === 8)) {
      throw new Error('transition ' + i +
        ': bad chain or commands (exactly one command, strict kind 8)');
    }
  });
} catch (e) {
  console.error('instanced-shadow-typed-fixture failed: ' + ((e && e.message) || e));
  process.exit(2);
}

// Register the 20 instanced-lifecycle cases (ten per backend: nine typed
// fixture scenes plus one live six-step sequence). Static order per backend:
// empty, reference-left, reference-right, both, left, right, moved-opaque,
// moved, one,
// then the lifecycle case. Coverage bounds: these cases certify ONLY real
// native multi-instance scene lifecycle (typed fixture scenes, chained
// kind-8 instancedMeshes commands, per-step instance/batch counts, read-only
// transform/color state, receiver-ROI pixel identity against the fresh
// static target scenes, and per-step native render advances). They are
// deliberately NOT tagged instancedShadow — that tag belongs to the existing
// 24-case single-instance group — and no renderer algorithms are duplicated
// here: all evidence comes from the existing wrappers, the READ snapshot and
// CDP screenshots.
function registerInstancedLifecycleCases(fixture) {
  const ORDER = ['empty', 'reference-left', 'reference-right', 'both',
    'left', 'right', 'moved-opaque', 'moved', 'one'];
  const COUNTS = { empty: 0, 'reference-left': 0, 'reference-right': 0,
    both: 2, left: 2, right: 2, 'moved-opaque': 2, moved: 2, one: 1 };
  // Receiver-only ROI: excludes the caster silhouettes so same/differs
  // comparisons prove received shadows, not caster pixels.
  const ROI = { x: 125, y: 99, width: 95, height: 35 };
  [false, true].forEach((webgpu) => {
    const pfx = webgpu ? 'wg-instlife-' : 'gl-instlife-';
    ORDER.forEach((sceneName) => {
      const c = {
        name: pfx + sceneName, webgpu,
        typedInstancedScene: fixture.scenes[sceneName],
        instancedLifecycleScene: sceneName,
        expectedInstancedCount: COUNTS[sceneName],
        instancedLifecycleGroup: true,
        f0: 0.04, f90: 1,
        shadowROI: { x: ROI.x, y: ROI.y, width: ROI.width, height: ROI.height },
      };
      // Receiver-ROI expectations: left and one are byte-identical to the
      // ordinary unmasked reference-left render; right to reference-right;
      // reference-left/reference-right/both differ from empty by >=50
      // pixels; moved differs from right by >=50. The existing same/differs
      // comparison machinery carries both directions via shadowROI.
      if (sceneName === 'left' || sceneName === 'one') c.same = pfx + 'reference-left';
      if (sceneName === 'right') c.same = pfx + 'reference-right';
      if (sceneName === 'reference-left' || sceneName === 'reference-right' ||
          sceneName === 'both') {
        c.differs = pfx + 'empty';
        c.minChanged = 50;
      }
      if (sceneName === 'moved') { c.differs = pfx + 'right'; c.minChanged = 50; }
      // Full-image control: moved must be byte-identical to the opaque
      // moved-opaque render across the COMPLETE screenshot (no ROI). This
      // is the regression assertion for the flat instanceColor fix.
      if (sceneName === 'moved') { c.sameFull = pfx + 'moved-opaque'; }
      CASES.push(c);
    });
    // Live sequence: starts at the typed 'both' scene; after the initial
    // normal capture, the six fixture transitions are applied on the SAME
    // mount/canvas/state via the public gosx:scene3d:commands mount event
    // (never internal applySceneCommands, never remount/reload, never
    // synthesized commands).
    CASES.push({
      name: pfx + 'lifecycle', webgpu,
      typedInstancedScene: fixture.scenes.both,
      instancedLifecycleScene: 'both',
      expectedInstancedCount: 2,
      instancedLifecycleGroup: true,
      lifecycle: true,
      lifecycleTransitions: fixture.transitions,
      f0: 0.04, f90: 1,
      shadowROI: { x: ROI.x, y: ROI.y, width: ROI.width, height: ROI.height },
    });
  });
}
registerInstancedLifecycleCases(INST_FIXTURE);
// Keep the full registration (CASES and byName) intact alongside the
// optional group filter below.
CASES.forEach((c) => { byName[c.name] = c; });

// ---- Optional diagnostic case-group filter (execution only) ----
// GOSX_IOR_CASE_GROUP=instanced-lifecycle selects exactly the 20 new
// instanced-lifecycle cases (static references included). Unset => all
// cases. Any other nonempty value is rejected. RUN_CASES drives execution
// AND comparison only; CASES/byName stay fully registered, and the final
// report carries caseGroup + registeredCaseCount + selectedCaseCount so a
// filtered run can never masquerade as a full one.
const CASE_GROUP_ENV = process.env.GOSX_IOR_CASE_GROUP;
const CASE_GROUP = CASE_GROUP_ENV || 'all';
let RUN_CASES = CASES;
if (CASE_GROUP_ENV != null && CASE_GROUP_ENV !== '' &&
    CASE_GROUP_ENV !== 'instanced-lifecycle' &&
    CASE_GROUP_ENV !== 'skinned-shadow') {
  console.error('GOSX_IOR_CASE_GROUP: unsupported group ' +
    JSON.stringify(CASE_GROUP_ENV) +
    ' (only "instanced-lifecycle", "skinned-shadow" or unset)');
  process.exit(2);
}
if (CASE_GROUP_ENV === 'instanced-lifecycle') {
  RUN_CASES = CASES.filter((c) => c.instancedLifecycleGroup === true);
  if (RUN_CASES.length !== 20) {
    console.error('instanced-lifecycle group: expected exactly 20 cases, got ' +
      RUN_CASES.length);
    process.exit(2);
  }
}
if (CASE_GROUP_ENV === 'skinned-shadow') {
  if (CASES.length !== 267) {
    console.error('skinned-shadow group: expected exactly 267 registered cases, got ' +
      CASES.length);
    process.exit(2);
  }
  RUN_CASES = CASES.filter((c) => c.skinnedShadowGroup === true);
  if (RUN_CASES.length !== 16) {
    console.error('skinned-shadow group: expected exactly 16 cases, got ' +
      RUN_CASES.length);
    process.exit(2);
  }
}
if (!Array.isArray(RUN_CASES) || RUN_CASES.length === 0) {
  console.error('no cases selected for execution');
  process.exit(2);
}

function propsFor(c) {
  const p = { width: W, height: H, autoRotate: false, animation: false,
    responsive: false, maxDevicePixelRatio: 1,
    forceWebGL: !c.webgpu, requireWebGL: !c.webgpu,
    preferWebGPU: Boolean(c.webgpu),
    background: '#101418',
    camera: { x: 0, y: 0, z: 4, fov: 50 },
    lights: [{ id: 'key', kind: 'directional', intensity: 1.2,
      directionX: 0, directionY: 0, directionZ: -1 }] };
  // Explicit-zero preservation for the IBL isolation cases: direct light
  // zero, ambient/sky/ground zero; prior cases keep the 1.2 key light.
  if (typeof c.keyLightIntensity === 'number') p.lights[0].intensity = c.keyLightIntensity;
  // IBL isolation cases: normalizeSceneEnvironment treats an all-zero or
  // color-less environment descriptor as unspecified and then adds a default
  // fill. Explicit black colors keep the zero corresponding intensities but
  // disable that fill, so the only environment contribution is the IBL asset
  // itself. This does not change production semantics for these cases.
  if (c.environment) p.environment = c.environment;
  if (c.requiresIBL || c.noIBL) {
    p.environment = {
      ...c.environment,
      ambientColor: '#000000',
      skyColor: '#000000',
      groundColor: '#000000',
    };
  }
  if (c.materials) p.materials = c.materials;
  if (c.obj) p.objects = [c.obj];
  if (c.objects) p.objects = c.objects;
  if (c.model) p.models = [c.model];
  if (c.instanced) p.instancedGLBMeshes = [c.instanced];
  if (c.instancedMeshes) p.instancedMeshes = c.instancedMeshes;
  // Shadow-budget cases: explicit scoped lights and camera near/far so the
  // two directional castShadow lights and depth range are exactly the ones
  // under test.
  if (c.lights) p.lights = c.lights;
  if (typeof c.cameraNear === 'number') {
    p.camera.near = c.cameraNear;
    p.camera.far = c.cameraFar;
  }
  // Typed instanced-lifecycle cases: the generated Go fixture SceneIR arrays
  // (objects, lights, instancedMeshes) are passed through VERBATIM — no
  // manual rewriting of object, light or instanced data.
  if (c.typedInstancedScene) {
    if (Array.isArray(c.typedInstancedScene.lights)) p.lights = c.typedInstancedScene.lights;
    if (Array.isArray(c.typedInstancedScene.objects)) p.objects = c.typedInstancedScene.objects;
    if (Array.isArray(c.typedInstancedScene.instancedMeshes)) {
      p.instancedMeshes = c.typedInstancedScene.instancedMeshes;
    }
  }
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
  if (GLB_FILES[req.url]) {
    const b = GLB_FILES[req.url];
    res.writeHead(200, { 'content-type': 'model/gltf-binary', 'content-length': b.length });
    res.end(b);
  } else if (TEX_PNGS[req.url]) {
    texServed[req.url] = (texServed[req.url] || 0) + 1;
    const b = TEX_PNGS[req.url];
    res.writeHead(200, { 'content-type': 'image/png', 'content-length': b.length });
    res.end(b);
  } else if (req.url === '/models/quad242.glb') {
    res.writeHead(200, { 'content-type': 'model/gltf-binary', 'content-length': glb242.length });
    res.end(glb242);
  } else if (req.url && req.url.indexOf('/models/alpha-') === 0) {
    const aname = req.url.slice('/models/'.length).split('?')[0].replace(/\.glb$/, '');
    const ab = alphaGLBs[aname];
    if (!ab) { res.writeHead(404); res.end(); return; }
    res.writeHead(200, { 'content-type': 'model/gltf-binary', 'content-length': ab.length });
    res.end(ab);
  } else if (req.url === '/models/quad-default.glb') {
    res.writeHead(200, { 'content-type': 'model/gltf-binary', 'content-length': glbDefaultIor.length });
    res.end(glbDefaultIor);
  } else if (req.url === '/models/quad-spec-white.glb') {
    res.writeHead(200, { 'content-type': 'model/gltf-binary', 'content-length': glbSpecWhite.length });
    res.end(glbSpecWhite);
  } else if (req.url === '/models/quad-spec-zero.glb') {
    res.writeHead(200, { 'content-type': 'model/gltf-binary', 'content-length': glbSpecZero.length });
    res.end(glbSpecZero);
  } else if (req.url === '/models/quad-spec-ior242.glb') {
    res.writeHead(200, { 'content-type': 'model/gltf-binary', 'content-length': glbSpecIor242.length });
    res.end(glbSpecIor242);
  } else if (req.url === '/models/skinned-box-rest.glb') {
    skinnedGlbServed.rest += 1;
    const b = Buffer.from(SKIN_FIXTURE.rest, 'base64');
    res.writeHead(200, { 'content-type': 'model/gltf-binary', 'content-length': b.length });
    res.end(b);
  } else if (req.url === '/models/skinned-box-pose.glb') {
    skinnedGlbServed.pose += 1;
    const b = Buffer.from(SKIN_FIXTURE.pose, 'base64');
    res.writeHead(200, { 'content-type': 'model/gltf-binary', 'content-length': b.length });
    res.end(b);
  } else if (req.url === '/bootstrap.js' || req.url === '/client/js/bootstrap.js') {
    const js = fs.readFileSync(BOOTSTRAP);
    res.writeHead(200, { 'content-type': 'text/javascript', 'content-length': js.length });
    res.end(js);
  } else if (req.url === '/gosx/bootstrap-feature-scene3d-webgpu.js') {
    const js = fs.readFileSync(WG_CHUNK);
    res.writeHead(200, { 'content-type': 'text/javascript', 'content-length': js.length });
    res.end(js);
  } else if (req.url === '/gosx/bootstrap-feature-scene3d-gltf.js') {
    const js = fs.readFileSync(GLTF_CHUNK);
    res.writeHead(200, { 'content-type': 'text/javascript', 'content-length': js.length });
    res.end(js);
  } else if (req.url === '/gosx/bootstrap-feature-scene3d-animation.js') {
    animChunkServed += 1;
    const js = fs.readFileSync(ANIM_CHUNK);
    res.writeHead(200, { 'content-type': 'text/javascript', 'content-length': js.length });
    res.end(js);
  } else if (req.url === '/ibl/spec-radiance.ktx2') {
    iblAssetCount.radiance += 1;
    const b = b64buf(IBL_FIXTURE.radiance);
    res.writeHead(200, { 'content-type': 'application/octet-stream', 'content-length': b.length });
    res.end(b);
  } else if (req.url === '/ibl/spec-irradiance.ktx2') {
    iblAssetCount.irradiance += 1;
    const b = b64buf(IBL_FIXTURE.irradiance);
    res.writeHead(200, { 'content-type': 'application/octet-stream', 'content-length': b.length });
    res.end(b);
  } else if (req.url === '/ibl/spec-brdf-lut.ktx2') {
    iblAssetCount.brdfLUT += 1;
    const b = b64buf(IBL_FIXTURE.brdfLUT);
    res.writeHead(200, { 'content-type': 'application/octet-stream', 'content-length': b.length });
    res.end(b);
  } else if (req.url === '/' || (req.url && req.url.indexOf('/?') === 0)) {
    // Valid served loopback HTTP origin used before requestAdapter probing.
    res.writeHead(200, { 'content-type': 'text/html' });
    res.end('<!doctype html><html><head><meta charset="utf-8"><title>probe origin</title></head><body>probe-origin</body></html>');
  } else if (req.url && req.url.indexOf('/case/') === 0) {
    const name = req.url.slice('/case/'.length).split('?')[0];
    const c = CASES.find((x) => x.name === name);
    if (!c) { res.writeHead(404); res.end(); return; }
    iblAssetCount = { radiance: 0, irradiance: 0, brdfLUT: 0 };
    texServed = {};
    Object.keys(TEX_PNGS).forEach((k) => { texServed[k] = 0; });
    skinnedGlbServed = { rest: 0, pose: 0 };
    animChunkServed = 0;
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
// getUniformLocation tracks program->location for u_specularF0 and
// u_specularF90; at each draw (including instanced forms) we read
// CURRENT_PROGRAM and getUniform.
// Nothing is inferred from uniform1f/useProgram. GPUQueue.writeBuffer is
// wrapped with its true signature (buffer, bufferOffset, data, dataOffset?,
// size?) with correct element/byte dataOffset+size semantics, capturing only
  // 208-byte material uploads and reading F0 at float indices 44..46 and F90
// at 47, plus the alpha cutoff at float index 42 (byte 168).
const PRELOAD = `
  window.__gosxIOR = { draws: 0, pbrDraws: 0, lastDrawF0: null, lastDrawF90: null, f0s: [], obsErrors: [], gl: null,
    lastDrawOpacity: null,
    lastDrawAlphaCutoff: null,
    lastDrawDepthWrite: null,
    lastDrawBlend: null,
    lastDrawUnlit: null,
    lastDrawHasIBL: null, lastDrawHasSpecIntensityMap: null, lastDrawHasSpecColorMap: null,
    lastDrawHasAlbedoMap: null,
    instShadowDraws: 0, lastInstShadowInstances: 0,
    programInfo: null, queriedUniforms: [], shadow: null, nativeCap: null, forcedCap: null,
    skinColor: { draws: 0, lastHasSkin: null, lastProgramId: null,
      lastModelMatrix: null, lastPalette0: null, lastOpacity: null,
      lastAlphaCutoff: null },
    skinShadow: { draws: 0, lastHasSkin: null, lastProgramId: null,
      lastModelMatrix: null, lastPalette0: null, lastOpacity: null,
      lastAlphaCutoff: null },
    deletedProgramIds: [] };
  // Forced MAX_TEXTURE_IMAGE_UNITS caps, selected by the served case pathname
  // so no other case is affected. Used only by the shadow-budget cases.
  var __path = (typeof location !== "undefined" && location.pathname) || "";
  if (__path.indexOf("/case/gl-shadow-budget-16") === 0) window.__gosxIORForcedCap = 16;
  if (__path.indexOf("/case/gl-shadow-budget-32") === 0) window.__gosxIORForcedCap = 32;
  var __gosxShadowEnabled = !!window.__gosxIORForcedCap;
  var __shadowTexIds = (typeof WeakMap !== "undefined") ? new WeakMap() : null;
  var __shadowNextTexId = 0;
  var __shadowDepthIds = new Set();
  // WeakMap has no .size: assign stable unique native texture IDs from a
  // monotonic integer counter instead.
  function __shadowTexId(t) {
    if (!t || !__shadowTexIds) return null;
    var id = __shadowTexIds.get(t);
    if (id === undefined) {
      __shadowNextTexId += 1;
      id = __shadowNextTexId;
      __shadowTexIds.set(t, id);
    }
    return id;
  }
  window.__gosxWGPU = { materialUploads: 0, dumps: [], obsErrors: [] };
  window.__gosxWGPUDraws = { observed: 0, lastDepthWrite: null, lastBlend: null };
(function () {
  var latest80 = (typeof WeakMap !== "undefined") ? new WeakMap() : null;
  var frameBindGroups = (typeof WeakMap !== "undefined") ? new WeakMap() : null;
  var boundEnvBuffer = null;
  // Dynamic read of the latest environment words for the currently bound
  // environment buffer: resolved at read time (never a stale snapshot), and
  // never a raw buffer/weakmap exposed in the JSON evidence.
  window.__gosxWGPUReadEnvWords = function () {
    return (latest80 && boundEnvBuffer) ? (latest80.get(boundEnvBuffer) || null) : null;
  };
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
        var fm0 = this.__sf0locs, fm90 = this.__sf90locs;
        info.trackedF0 = !!(fm0 && fm0.has(cp) && fm90 && fm90.has(cp));
        window.__gosxIOR.programInfo = info;
      }
      var mf0 = this.__sf0locs, mf90 = this.__sf90locs;
      if (cp && mf0 && mf0.has(cp) && mf90 && mf90.has(cp)) {
        var v0 = this.__origGetUniform.call(this, cp, mf0.get(cp));
        var v90 = this.__origGetUniform.call(this, cp, mf90.get(cp));
        if (v0 && typeof v0.length === "number" && v0.length === 3 &&
            typeof v90 === "number" && Number.isFinite(v90)) {
          var vec = [v0[0], v0[1], v0[2]];
          window.__gosxIOR.pbrDraws += 1;
          window.__gosxIOR.lastDrawF0 = vec;
          window.__gosxIOR.lastDrawF90 = v90;
          // u_opacity read through the native getUniform at the SAME
          // F0/F90-qualified PBR draw. Missing/inactive uniform stays null.
          var om = this.__oplocs;
          var ov = (om && om.has(cp)) ? this.__origGetUniform.call(this, cp, om.get(cp)) : null;
          window.__gosxIOR.lastDrawOpacity =
            (typeof ov === "number" && Number.isFinite(ov)) ? ov : null;
          // u_alphaCutoff read through the native getUniform at the SAME
          // F0/F90-qualified PBR draw, mirroring u_opacity. Missing/inactive
          // uniform or a nonfinite value stays null and fails the gate.
          var omc = this.__aclocs;
          var cvv = (omc && omc.has(cp)) ? this.__origGetUniform.call(this, cp, omc.get(cp)) : null;
          window.__gosxIOR.lastDrawAlphaCutoff =
            (typeof cvv === "number" && Number.isFinite(cvv)) ? cvv : null;
          // Native per-draw depth/blend routing observations at the SAME
          // F0/F90-qualified PBR draw: the real DEPTH_WRITEMASK via the
          // saved native getParameter and the real BLEND enable state via
          // the saved native isEnabled. Strict booleans only; a missing or
          // failed read is recorded as null and fails the route gates; it
          // never defaults to an assumed routing state.
          try {
            var dwm = this.__origGetParameter.call(this, this.DEPTH_WRITEMASK);
            window.__gosxIOR.lastDrawDepthWrite =
              (dwm === true) ? true : (dwm === false) ? false : null;
          } catch (e4) {
            window.__gosxIOR.lastDrawDepthWrite = null;
            noteErr(window.__gosxIOR.obsErrors, e4);
          }
          try {
            var blv = this.__origIsEnabled
              ? this.__origIsEnabled.call(this, this.BLEND) : null;
            window.__gosxIOR.lastDrawBlend =
              (blv === true) ? true : (blv === false) ? false : null;
          } catch (e5) {
            window.__gosxIOR.lastDrawBlend = null;
            noteErr(window.__gosxIOR.obsErrors, e5);
          }
          var mul = this.__ulocs;
          var ulv = (mul && mul.has(cp)) ? this.__origGetUniform.call(this, cp, mul.get(cp)) : null;
          // u_unlit read through the native getUniform at the SAME F0/F90-
          // qualified PBR draw. Only an explicit boolean true/false or
          // numeric 1/0 is accepted; missing/inactive becomes null EVERY draw.
          window.__gosxIOR.lastDrawUnlit =
            (ulv === true || ulv === false) ? ulv :
            (ulv === 1 || ulv === 0) ? ulv === 1 : null;
          var malb = this.__alblocs;
          var abv = (malb && malb.has(cp)) ? this.__origGetUniform.call(this, cp, malb.get(cp)) : null;
          // u_hasAlbedoMap read through the native getUniform at the SAME
          // F0/F90-qualified PBR draw. Only an explicit boolean true/false or
          // numeric 1/0 is accepted; missing/inactive becomes null EACH draw.
          window.__gosxIOR.lastDrawHasAlbedoMap =
            (abv === true || abv === false) ? abv :
            (abv === 1 || abv === 0) ? abv === 1 : null;
          var mibl = this.__sibllocs;
          if (mibl && mibl.has(cp)) {
            var vI = this.__origGetUniform.call(this, cp, mibl.get(cp));
            // Preserve an explicit boolean true/false or numeric 1/0 as a
            // boolean; anything else is null. Missing is NOT disabled.
            window.__gosxIOR.lastDrawHasIBL =
              (vI === true || vI === false) ? vI :
              (vI === 1 || vI === 0) ? vI === 1 : null;
          } else {
            // No tracked u_hasIBL location for this PBR program: clear any
            // previous observation so it cannot leak across draws.
            window.__gosxIOR.lastDrawHasIBL = null;
          }
          var mhas = this.__shaspeclocs;
          if (mhas && mhas.has(cp)) {
            var vH = this.__origGetUniform.call(this, cp, mhas.get(cp));
            // Real draw-time u_hasSpecularIntensityMap state: only an
            // explicit boolean true/false or numeric 1/0 is recorded; any
            // other value, and any missing tracked location, is null so
            // readiness is never invented.
            window.__gosxIOR.lastDrawHasSpecIntensityMap =
              (vH === true) ? true :
              (vH === false) ? false :
              (vH === 1 || vH === 1.0) ? true :
              (vH === 0 || vH === 0.0) ? false : null;
          } else {
            // No tracked u_hasSpecularIntensityMap location for this PBR
            // program: clear any previous observation so it cannot leak
            // across draws and can never fake readiness.
            window.__gosxIOR.lastDrawHasSpecIntensityMap = null;
          }
          var mcol = this.__shascolorlocs;
          if (mcol && mcol.has(cp)) {
            var vC = this.__origGetUniform.call(this, cp, mcol.get(cp));
            // Real draw-time u_hasSpecularColorMap state, recorded with the
            // same strict boolean/1-0 rules as the intensity flag and read at
            // the SAME PBR draw as the intensity flag; any other value, and
            // any missing tracked location, is null so combined readiness is
            // never invented or leaked across draws.
            window.__gosxIOR.lastDrawHasSpecColorMap =
              (vC === true) ? true :
              (vC === false) ? false :
              (vC === 1 || vC === 1.0) ? true :
              (vC === 0 || vC === 0.0) ? false : null;
          } else {
            // No tracked u_hasSpecularColorMap location for this PBR
            // program: clear any previous observation so it cannot leak
            // across draws and can never fake readiness.
            window.__gosxIOR.lastDrawHasSpecColorMap = null;
          }
          // Shadow-budget cases: on each native PBR draw, capture the real
          // has/cascade/light-index uniforms for both slots and the actual
          // u_shadowMap{slot}_{cascade} sampler unit bindings for the active
          // cascades, read from the current PBR program via the saved native
          // calls (the previously active unit is restored in a finally
          // block). A whole-unit scan would be wrong here: the source binds
          // many MATERIAL textures too.
          if (__gosxShadowEnabled) {
            var sh = { cascades: [], has: [], lightIndices: [], units: [],
              textures: [], linkStatus: null,
              depthAttachmentCount: 0, depthAttachmentIds: [], error: null };
            try {
              // Native LINK_STATUS of the CURRENT program at this draw.
              sh.linkStatus = this.__origGetProgramParameter.call(this, cp, 0x8B82 /* LINK_STATUS */);
              if (this.__scasc0locs && this.__scasc0locs.has(cp) &&
                  this.__scasc1locs && this.__scasc1locs.has(cp) &&
                  this.__shas0locs && this.__shas0locs.has(cp) &&
                  this.__shas1locs && this.__shas1locs.has(cp) &&
                  this.__sli0locs && this.__sli0locs.has(cp) &&
                  this.__sli1locs && this.__sli1locs.has(cp)) {
                sh.cascades.push(this.__origGetUniform.call(this, cp, this.__scasc0locs.get(cp)));
                sh.cascades.push(this.__origGetUniform.call(this, cp, this.__scasc1locs.get(cp)));
                sh.has.push(this.__origGetUniform.call(this, cp, this.__shas0locs.get(cp)));
                sh.has.push(this.__origGetUniform.call(this, cp, this.__shas1locs.get(cp)));
                sh.lightIndices.push(this.__origGetUniform.call(this, cp, this.__sli0locs.get(cp)));
                sh.lightIndices.push(this.__origGetUniform.call(this, cp, this.__sli1locs.get(cp)));
              } else {
                sh.error = 'shadow uniform locations missing at PBR draw';
              }
              var natCap = window.__gosxIOR.nativeCap;
              if (!sh.error && typeof natCap === "number" && natCap > 0 &&
                  this.__origActiveTexture && this.__origGetUniformLocation) {
                var prevUnit = this.__origGetParameter.call(this, 0x84E0 /* ACTIVE_TEXTURE */);
                try {
                  for (var slot = 0; slot < 2 && !sh.error; slot += 1) {
                    var ncasc = sh.cascades[slot];
                    for (var ci = 0; ci < ncasc; ci += 1) {
                      var sname = 'u_shadowMap' + slot + '_' + ci;
                      var sloc = this.__origGetUniformLocation.call(this, cp, sname);
                      if (!sloc) { sh.error = 'missing shadow sampler uniform ' + sname; break; }
                      var unit = this.__origGetUniform.call(this, cp, sloc);
                      var forcedNow = window.__gosxIORForcedCap;
                      if (typeof unit !== 'number' || unit < 0 || unit >= natCap ||
                          (typeof forcedNow === 'number' && unit >= forcedNow)) {
                        sh.error = 'sampler ' + sname + ' bound to out-of-range unit ' + unit; break;
                      }
                      // Switch to that exact unit only; never scan all units.
                      this.__origActiveTexture.call(this, 0x84C0 + unit /* TEXTURE0 + unit */);
                      var tobj = this.__origGetParameter.call(this, 0x8069 /* TEXTURE_BINDING_2D */);
                      var tid = tobj ? __shadowTexId(tobj) : null;
                      if (tid === null) { sh.error = 'no texture bound on unit ' + unit + ' for ' + sname; break; }
                      sh.units.push(unit);
                      sh.textures.push(tid);
                    }
                  }
                } finally {
                  this.__origActiveTexture.call(this, prevUnit);
                }
              } else if (!sh.error) {
                sh.error = 'native cap or native GL accessors missing at PBR draw';
              }
              sh.depthAttachmentCount = __shadowDepthIds.size;
              sh.depthAttachmentIds = Array.from(__shadowDepthIds).sort(function (a, b) { return a - b; });
            } catch (se) {
              sh.error = String((se && se.message) || se);
              // Observation errors are fatal: record them on every draw so
              // they cannot be lost if a later draw overwrites the snapshot.
              noteErr(window.__gosxIOR.obsErrors, se);
            }
            if (sh.error) noteErr(window.__gosxIOR.obsErrors, sh.error);
            window.__gosxIOR.shadow = sh;
          }
          if (window.__gosxIOR.f0s.length < 4096) window.__gosxIOR.f0s.push(vec);
        }
      }
    } catch (e) { noteErr(window.__gosxIOR.obsErrors, e); }
  }
  // Instanced shadow-depth draw observation: identifies the real instanced
  // shadow program by a tracked u_lightViewProjection location plus a
  // nonnegative a_instanceMatrix0 attribute location, both read through the
  // saved native calls only. The native draw is always forwarded unchanged;
  // any observation error is recorded (and fails the case) without altering
  // native behavior.
  function observeInstShadow(instanceCount) {
    try {
      var cp = this.__origGetParameter.call(this, this.CURRENT_PROGRAM);
      if (!cp) return;
      var lv = this.__lvlocs;
      if (!lv || !lv.has(cp)) return;
      var al = this.__origGetAttribLocation
        ? this.__origGetAttribLocation.call(this, cp, "a_instanceMatrix0") : -1;
      if (typeof al !== "number" || al < 0) return;
      window.__gosxIOR.instShadowDraws += 1;
      var n = (typeof instanceCount === "number" &&
        Number.isFinite(instanceCount) && instanceCount > 0) ? instanceCount : 0;
      // Record the most recent qualified draw unconditionally: per-step and
      // settled assertions need the LAST observed normalized instance count,
      // never a running maximum.
      window.__gosxIOR.lastInstShadowInstances = n;
    } catch (e) { noteErr(window.__gosxIOR.obsErrors, e); }
  }
  // Stable native program IDs via WeakMap: each WebGLProgram object gets a
  // monotonic integer id, assigned on first sight.
  var __progIds = (typeof WeakMap !== "undefined") ? new WeakMap() : null;
  var __progNextId = 0;
  // Observer-owned qualification set. Skinned-draw qualification is
  // recorded here instead of on the production WebGLProgram handle, so
  // the observer never writes properties onto engine-owned objects.
  var qualifiedPrograms = (typeof WeakSet !== "undefined") ? new WeakSet() : null;
  function progId(p) {
    if (!p || !__progIds) return null;
    var id = __progIds.get(p);
    if (id === undefined) {
      __progNextId += 1;
      id = __progNextId;
      __progIds.set(p, id);
    }
    return id;
  }
  // Skinned forward-draw observation: called ONLY inside the forwarded
  // non-instanced drawArrays/drawElements wrappers (never the instanced
  // observer). Qualification requires the tracked u_hasSkin location whose
  // NATIVE value at this draw is 1/true AND a tracked u_jointMatrices[0]
  // location. COLOR draws additionally require tracked native specF0/F90
  // locations; SHADOW draws additionally require the tracked
  // u_lightViewProjection location. All reads are native getUniform at the
  // CURRENT program; the native draw is never altered.
  function observeSkinnedDraw(channel) {
    try {
      var cp = this.__origGetParameter.call(this, this.CURRENT_PROGRAM);
      if (!cp) return;
      var hs = this.__hslocs && this.__hslocs.get(cp);
      var jm = this.__jm0locs && this.__jm0locs.get(cp);
      if (!hs || !jm) return;
      var hasSkin = this.__origGetUniform.call(this, cp, hs);
      if (hasSkin !== 1 && hasSkin !== true) return;
      var mm = this.__mmlocs && this.__mmlocs.get(cp);
      var op = this.__oplocs && this.__oplocs.get(cp);
      var ac = this.__aclocs && this.__aclocs.get(cp);
      var extraA = null, extraB = null;
      if (channel === "skinColor") {
        extraA = this.__sf0locs && this.__sf0locs.get(cp);
        extraB = this.__sf90locs && this.__sf90locs.get(cp);
        if (!extraA || !extraB) return;
      } else {
        extraA = this.__lvlocs && this.__lvlocs.get(cp);
        if (!extraA) return;
      }
      var rec = window.__gosxIOR[channel];
      if (!rec) return;
      if (qualifiedPrograms) qualifiedPrograms.add(cp);
      var model = mm ? this.__origGetUniform.call(this, cp, mm) : null;
      var pal0 = this.__origGetUniform.call(this, cp, jm);
      rec.draws += 1;
      rec.lastHasSkin = true;
      rec.lastProgramId = progId(cp);
      rec.lastModelMatrix = (model && model.length === 16) ?
        Array.prototype.slice.call(model) : null;
      rec.lastPalette0 = (pal0 && pal0.length === 16) ?
        Array.prototype.slice.call(pal0) : null;
      rec.lastOpacity = (op != null) ?
        this.__origGetUniform.call(this, cp, op) : null;
      rec.lastAlphaCutoff = (ac != null) ?
        this.__origGetUniform.call(this, cp, ac) : null;
      if (channel === "skinColor") {
        rec.lastSpecF0 = this.__origGetUniform.call(this, cp, extraA);
        rec.lastSpecF90 = this.__origGetUniform.call(this, cp, extraB);
      } else {
        var lvp = this.__origGetUniform.call(this, cp, extraA);
        rec.lastLightViewProjection = (lvp && lvp.length === 16) ?
          Array.prototype.slice.call(lvp) : null;
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
        gal = proto.getAttribLocation,
        ie = proto.isEnabled,
        da = proto.drawArrays, de = proto.drawElements,
        dai = proto.drawArraysInstanced, dei = proto.drawElementsInstanced,
        gat = proto.activeTexture, gfbt = proto.framebufferTexture2D;
    proto.__origGetParameter = gp;
    proto.__origGetUniform = guf;
    proto.__origGetProgramParameter = gpp;
    proto.__origGetActiveUniform = gau;
    if (ie) proto.__origIsEnabled = ie;
    if (gat) proto.__origActiveTexture = gat;
    proto.__origGetUniformLocation = gu;
    if (gal) proto.__origGetAttribLocation = gal;
    // Forced-cap interception for MAX_TEXTURE_IMAGE_UNITS (0x8872) only, on
    // the shadow-budget case pages. The true native cap is recorded once via
    // the saved native; every other call forwards unchanged. A forced 32 is
    // never claimed to prove 32-unit hardware: the engine sees
    // min(forced, native), and the runner fails the case explicitly when the
    // native cap is below the forced value.
    proto.getParameter = function (p) {
      var forced = window.__gosxIORForcedCap;
      if (p === 0x8872 && typeof forced === "number") {
        if (window.__gosxIOR.nativeCap === null) {
          var nat = gp.call(this, p);
          window.__gosxIOR.nativeCap = nat;
        }
        var eff = Math.min(forced, window.__gosxIOR.nativeCap);
        window.__gosxIOR.forcedCap = eff;
        return eff;
      }
      return gp.apply(this, arguments);
    };
    // Strictly forwarded framebufferTexture2D: the native is called with the
    // exact original arguments and its return value passes through. DEPTH
    // _ATTACHMENT (0x8D00) textures get stable counter IDs for evidence,
    // tracked only on shadow-budget pages (texture is argument 3).
    if (gfbt) {
      proto.framebufferTexture2D = function () {
        var r = gfbt.apply(this, arguments);
        try {
          if (__gosxShadowEnabled && arguments[1] === 0x8D00 && arguments.length > 3 && arguments[3]) {
            var id = __shadowTexId(arguments[3]);
            if (id !== null) __shadowDepthIds.add(id);
          }
        } catch (e) { noteErr(window.__gosxIOR.obsErrors, e); }
        return r;
      };
    }
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
        if (n === "u_opacity") {
          var mop = this.__oplocs || (this.__oplocs = new Map());
          if (loc) mop.set(p, loc); else mop.delete(p);
        }
        if (n === "u_alphaCutoff") {
          var mac = this.__aclocs || (this.__aclocs = new Map());
          if (loc) mac.set(p, loc); else mac.delete(p);
        }
        if (n === "u_hasAlbedoMap") {
          var mab = this.__alblocs || (this.__alblocs = new Map());
          if (loc) mab.set(p, loc); else mab.delete(p);
        }
        if (n === "u_unlit") {
          var mul = this.__ulocs || (this.__ulocs = new Map());
          if (loc) mul.set(p, loc); else mul.delete(p);
        }
        if (n === "u_specularF0") {
          var m0 = this.__sf0locs || (this.__sf0locs = new Map());
          if (loc) m0.set(p, loc); else m0.delete(p);
        }
        if (n === "u_specularF90") {
          var m90 = this.__sf90locs || (this.__sf90locs = new Map());
          if (loc) m90.set(p, loc); else m90.delete(p);
        }
        if (n === "u_hasIBL") {
          var mibl = this.__sibllocs || (this.__sibllocs = new Map());
          if (loc) mibl.set(p, loc); else mibl.delete(p);
        }
        if (n === "u_hasSpecularIntensityMap") {
          var mhas = this.__shaspeclocs || (this.__shaspeclocs = new Map());
          if (loc) mhas.set(p, loc); else mhas.delete(p);
        }
        if (n === "u_hasSpecularColorMap") {
          var mcol = this.__shascolorlocs || (this.__shascolorlocs = new Map());
          if (loc) mcol.set(p, loc); else mcol.delete(p);
        }
        if (n === "u_hasShadow0") {
          var ms0 = this.__shas0locs || (this.__shas0locs = new Map());
          if (loc) ms0.set(p, loc); else ms0.delete(p);
        }
        if (n === "u_hasShadow1") {
          var ms1 = this.__shas1locs || (this.__shas1locs = new Map());
          if (loc) ms1.set(p, loc); else ms1.delete(p);
        }
        if (n === "u_shadowCascades0") {
          var msc0 = this.__scasc0locs || (this.__scasc0locs = new Map());
          if (loc) msc0.set(p, loc); else msc0.delete(p);
        }
        if (n === "u_shadowCascades1") {
          var msc1 = this.__scasc1locs || (this.__scasc1locs = new Map());
          if (loc) msc1.set(p, loc); else msc1.delete(p);
        }
        if (n === "u_shadowLightIndex0") {
          var msli0 = this.__sli0locs || (this.__sli0locs = new Map());
          if (loc) msli0.set(p, loc); else msli0.delete(p);
        }
        if (n === "u_shadowLightIndex1") {
          var msli1 = this.__sli1locs || (this.__sli1locs = new Map());
          if (loc) msli1.set(p, loc); else msli1.delete(p);
        }
        if (n === "u_lightViewProjection") {
          var mlvp = this.__lvlocs || (this.__lvlocs = new Map());
          if (loc) mlvp.set(p, loc); else mlvp.delete(p);
        }
        if (n === "u_hasSkin") {
          var mhs = this.__hslocs || (this.__hslocs = new Map());
          if (loc) mhs.set(p, loc); else mhs.delete(p);
        }
        if (n === "u_jointMatrices[0]") {
          var mjm0 = this.__jm0locs || (this.__jm0locs = new Map());
          if (loc) mjm0.set(p, loc); else mjm0.delete(p);
        }
        if (n === "u_modelMatrix") {
          var mmm = this.__mmlocs || (this.__mmlocs = new Map());
          if (loc) mmm.set(p, loc); else mmm.delete(p);
        }
      } catch (e) { noteErr(window.__gosxIOR.obsErrors, e); }
      return loc;
    };
    if (da) proto.drawArrays = function () {
      observeDraw.call(this);
      observeSkinnedDraw.call(this, "skinColor");
      observeSkinnedDraw.call(this, "skinShadow");
      return da.apply(this, arguments);
    };
    if (de) proto.drawElements = function () {
      observeDraw.call(this);
      observeSkinnedDraw.call(this, "skinColor");
      observeSkinnedDraw.call(this, "skinShadow");
      return de.apply(this, arguments);
    };
    if (dai) proto.drawArraysInstanced = function () {
      observeDraw.call(this);
      observeInstShadow.call(this, arguments[3]);
      return dai.apply(this, arguments);
    };
    if (dei) proto.drawElementsInstanced = function () {
      observeDraw.call(this);
      observeInstShadow.call(this, arguments[4]);
      return dei.apply(this, arguments);
    };
    // Strict forward, then record only programs that carried a QUALIFIED
    // skinned draw (flagged inside observeSkinnedDraw).
    var dp = proto.deleteProgram;
    if (dp) proto.deleteProgram = function (program) {
      var r = dp.apply(this, arguments);
      try {
        if (program && qualifiedPrograms && qualifiedPrograms.has(program)) {
          var id = progId(program);
          if (id !== null && window.__gosxIOR.deletedProgramIds.indexOf(id) < 0 &&
              window.__gosxIOR.deletedProgramIds.length < 256) {
            window.__gosxIOR.deletedProgramIds.push(id);
          }
        }
      } catch (e) { noteErr(window.__gosxIOR.obsErrors, e); }
      return r;
    };
  }
  if (typeof GPUQueue !== "undefined" && GPUQueue.prototype && GPUQueue.prototype.writeBuffer) {
    var wb = GPUQueue.prototype.writeBuffer;
    GPUQueue.prototype.writeBuffer = function (buffer, bufferOffset, data, dataOffset, size) {
      try {
        if (data) {
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
          if (byteLen === 80 && bufferOffset === 0 && byteOff >= 0 && byteOff + 80 <= totalBytes) {
            // 80-byte environment word snapshot: hasIBL/mips/hasEnvMap are
            // words 14/15/16 (bytes 56/60/64). Every bounded 80-byte full
            // write is snapshotted, independently of the 192-byte material
            // dump cap below.
            var dv80 = new DataView(buf, base + byteOff, 80);
            if (latest80) latest80.set(buffer, {
              hasIBL: dv80.getUint32(14 * 4, true),
              mips: dv80.getUint32(15 * 4, true),
              hasEnvMap: dv80.getUint32(16 * 4, true)
            });
          }
          if (byteLen === 208 && window.__gosxWGPU.dumps.length < 256 &&
              byteOff >= 0 && byteOff + 208 <= totalBytes) {
            // 208-byte material capture: production writes Float32Array(52);
            // the effective specular F0 lives at float indices 44..46 and F90
            // at 47 (bytes 176:192, unchanged). Slots 0..43 are pre-existing
            // uniform data; 48..50 are the color log coefficients. Capped by
            // the 256-entry dump limit.
            var dv = new DataView(buf, base + byteOff, 208);
            var floats = new Array(52);
            for (var i = 0; i < 52; i++) floats[i] = dv.getFloat32(i * 4, true);
            // float index 6 is the material opacity and float index 42
            // (byte 168) is the alpha cutoff (-1 disabled, 0 valid, >1
            // finite), both captured from the same actual 208-byte upload
            // as F0/F90.
            window.__gosxWGPU.dumps.push({
              f0: [floats[44], floats[45], floats[46]], f90: floats[47], opacity: floats[6],
              alphaCutoff: floats[42],
              floats: floats,
              // hasSpecularIntensityMap flag at float index 41 (byte 164) and
              // hasSpecularColorMap flag at float index 51 (byte 204), each
              // read as an integer word, never via Float32 reinterpretation.
              hasSpecIntensityMap: dv.getUint32(164, true),
              hasSpecColorMap: dv.getUint32(204, true),
              // hasAlbedoMap flag at byte 52 (u32 index 13), read as an
              // integer word, never via Float32 reinterpretation.
              hasAlbedoMap: dv.getUint32(52, true),
              // unlit flag at byte 48 (u32 index 12), read as an integer
              // word, never via Float32 reinterpretation.
              unlit: dv.getUint32(48, true) });
            window.__gosxWGPU.materialUploads += 1;
          }
        }
      } catch (e) { noteErr(window.__gosxWGPU.obsErrors, e); }
      return wb.apply(this, arguments);
    };
  }
  if (typeof GPUDevice !== "undefined" && GPUDevice.prototype && GPUDevice.prototype.createBindGroup) {
    var cbg = GPUDevice.prototype.createBindGroup;
    GPUDevice.prototype.createBindGroup = function () {
      var group = cbg.apply(this, arguments);
      try {
        var d = arguments[0];
        var entries = d && d.entries;
        if (frameBindGroups && entries && entries.length === 15) {
          var ok = true;
          for (var i = 0; i < 15; i += 1) {
            var entry = entries[i];
            var res = entry && entry.resource;
            var isBufferSlot = (i === 0 || i === 1 || i === 2 || i === 3 || i === 8);
            var isViewSlot = (i === 9 || i === 10 || i === 11);
            var isSamplerSlot = (i === 12);
            // The production frame bind group uses a dense layout: entry
            // slot i must carry actual binding i, so the identified binding 3
            // below corresponds to the REAL binding 3.
            if (!entry || entry.binding !== i) { ok = false; break; }
            if (!res || typeof res !== "object") { ok = false; break; }
            if (isBufferSlot && !(res.buffer && typeof res.buffer === "object")) { ok = false; break; }
            if (isViewSlot && (res.buffer || typeof GPUTextureView === "undefined" ||
                !(res instanceof GPUTextureView))) { ok = false; break; }
            if (isSamplerSlot && (res.buffer || typeof GPUSampler === "undefined" ||
                !(res instanceof GPUSampler))) { ok = false; break; }
          }
          // Production frame bind group shape: buffers at 0/1/2/3/8,
          // texture views at 9/10/11, sampler at 12. Binding 3 carries
          // the per-frame environment buffer.
          if (ok && entries[3].resource && entries[3].resource.buffer) {
            frameBindGroups.set(group, entries[3].resource.buffer);
          }
        }
      } catch (e) { noteErr(window.__gosxWGPU.obsErrors, e); }
      return group;
    };
  }
  function wrapSetBindGroup(proto) {
    if (!proto || !proto.setBindGroup || proto.__gosxSBGWrapped) return;
    proto.__gosxSBGWrapped = true;
    var sbg = proto.setBindGroup;
    proto.setBindGroup = function () {
      var r = sbg.apply(this, arguments);
      try {
        if (arguments[0] === 0 && frameBindGroups) {
          // Keep the bound environment buffer identity, including null when
          // the frame bind group is unknown; readEnvWords resolves the words
          // dynamically so a later 80-byte write is never shadowed by a
          // stale snapshot taken at bind time.
          boundEnvBuffer = frameBindGroups.get(arguments[1]) || null;
        }
      } catch (e) { noteErr(window.__gosxWGPU.obsErrors, e); }
      return r;
    };
  }
  if (typeof GPURenderPassEncoder !== "undefined") wrapSetBindGroup(GPURenderPassEncoder.prototype);
  if (typeof GPURenderBundleEncoder !== "undefined") wrapSetBindGroup(GPURenderBundleEncoder.prototype);
  if (typeof GPUDevice !== "undefined" && GPUDevice.prototype &&
      GPUDevice.prototype.createRenderPipeline) {
    var crp = GPUDevice.prototype.createRenderPipeline;
    var crpa = GPUDevice.prototype.createRenderPipelineAsync;
    var wgpuPipelineRoute = (typeof WeakMap !== "undefined") ? new WeakMap() : null;
    // Snapshot only plain routing fields from the descriptor, synchronously;
    // GPU handles are never stored, copied, or reported.
    function snapshotRouteDescriptor(d) {
      if (!d || typeof d !== "object") return null;
      try {
        var frag = d.fragment;
        var tg = frag ? frag.targets : null;
        var t0 = (tg && tg.length > 0) ? tg[0] : null;
        var ds = d.depthStencil;
        // PBR qualification: real gosx-pbr- label, built-in vertex and
        // fragment entry points, an actual color target AND an explicit
        // depth-stencil state. The label never supplies the depth/blend
        // values; shadow-only or generic pipelines never qualify.
        if (typeof d.label === "string" && d.label.indexOf("gosx-pbr-") === 0 &&
            d.vertex && d.vertex.entryPoint === "vertexMain" &&
            frag && frag.entryPoint === "fragmentMain" &&
            t0 && typeof t0 === "object" && ds && typeof ds === "object") {
          return {
            depthWriteEnabled:
              ds.depthWriteEnabled === true ? true :
              ds.depthWriteEnabled === false ? false : null,
            blend: (t0.blend && typeof t0.blend === "object") ? true : false
          };
        }
      } catch (e) { noteErr(window.__gosxWGPU.obsErrors, e); }
      return null;
    }
    GPUDevice.prototype.createRenderPipeline = function () {
      var p = crp.apply(this, arguments);
      try {
        var s = snapshotRouteDescriptor(arguments[0]);
        if (s && wgpuPipelineRoute) wgpuPipelineRoute.set(p, s);
      } catch (e) { noteErr(window.__gosxWGPU.obsErrors, e); }
      return p;
    };
    if (crpa) {
      GPUDevice.prototype.createRenderPipelineAsync = function () {
        var args = arguments;
        // Snapshot synchronously BEFORE awaiting native creation; the
        // descriptor may be mutated or reused later and is never re-read.
        var s = null;
        try { s = snapshotRouteDescriptor(args[0]); }
        catch (e) { noteErr(window.__gosxWGPU.obsErrors, e); }
        return crpa.apply(this, arguments).then(function (p) {
          try { if (s && wgpuPipelineRoute) wgpuPipelineRoute.set(p, s); }
          catch (e2) { noteErr(window.__gosxWGPU.obsErrors, e2); }
          return p;
        });
      };
    }
    // Track the ACTUAL pipeline bound through setPipeline on both encoder
    // kinds, then record draw count and routing states for successful
    // native draws only: the native draw runs FIRST, observation after.
    function wrapPipelineTrack(proto) {
      if (!proto || !proto.setPipeline || proto.__gosxSPWrapped) return;
      proto.__gosxSPWrapped = true;
      var bound = (typeof WeakMap !== "undefined") ? new WeakMap() : null;
      var sp = proto.setPipeline;
      proto.setPipeline = function () {
        var r = sp.apply(this, arguments);
        try {
          if (bound && arguments[0]) bound.set(this, arguments[0]);
        } catch (e) { noteErr(window.__gosxWGPU.obsErrors, e); }
        return r;
      };
      ["draw", "drawIndexed"].forEach(function (m) {
        var orig = proto[m];
        if (!orig) return;
        proto[m] = function () {
          var r = orig.apply(this, arguments);
          try {
            var pi = bound ? bound.get(this) : null;
            var s = (pi && wgpuPipelineRoute) ? wgpuPipelineRoute.get(pi) : null;
            if (s) {
              window.__gosxWGPUDraws.observed += 1;
              window.__gosxWGPUDraws.lastDepthWrite = s.depthWriteEnabled;
              window.__gosxWGPUDraws.lastBlend = s.blend;
            }
          } catch (e) { noteErr(window.__gosxWGPU.obsErrors, e); }
          return r;
        };
      });
    }
    if (typeof GPURenderPassEncoder !== "undefined") wrapPipelineTrack(GPURenderPassEncoder.prototype);
    if (typeof GPURenderBundleEncoder !== "undefined") wrapPipelineTrack(GPURenderBundleEncoder.prototype);
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
  'instBatches:(function(){var st=m&&m.__gosxScene3DState;var im=st&&st.instancedMeshes;' +
  'if(!im)return 0;' +
  'if(typeof im.size==="number")return im.size;' +
  'if(typeof im.length==="number")return im.length;' +
  'return 0;})(),' +
  'ior:window.__gosxIOR?{draws:window.__gosxIOR.draws,pbrDraws:window.__gosxIOR.pbrDraws,' +
  'lastDrawF0:window.__gosxIOR.lastDrawF0,lastDrawF90:window.__gosxIOR.lastDrawF90,gl:window.__gosxIOR.gl,' +
  'lastDrawOpacity:(typeof window.__gosxIOR.lastDrawOpacity==="number"?' +
  'window.__gosxIOR.lastDrawOpacity:null),' +
  'lastDrawAlphaCutoff:(typeof window.__gosxIOR.lastDrawAlphaCutoff==="number"?' +
  'window.__gosxIOR.lastDrawAlphaCutoff:null),' +
  'lastDrawDepthWrite:(window.__gosxIOR.lastDrawDepthWrite===true||' +
  'window.__gosxIOR.lastDrawDepthWrite===false?window.__gosxIOR.lastDrawDepthWrite:null),' +
  'lastDrawBlend:(window.__gosxIOR.lastDrawBlend===true||' +
  'window.__gosxIOR.lastDrawBlend===false?window.__gosxIOR.lastDrawBlend:null),' +
  'lastDrawHasIBL:window.__gosxIOR.lastDrawHasIBL,' +
  'lastDrawHasSpecIntensityMap:window.__gosxIOR.lastDrawHasSpecIntensityMap,' +
  'lastDrawHasSpecColorMap:window.__gosxIOR.lastDrawHasSpecColorMap,' +
  'lastDrawHasAlbedoMap:window.__gosxIOR.lastDrawHasAlbedoMap,' +
  'lastDrawUnlit:window.__gosxIOR.lastDrawUnlit,' +
  'instShadowDraws:window.__gosxIOR.instShadowDraws,' +
  'instShadowInstances:window.__gosxIOR.lastInstShadowInstances,' +
  'linkStatus:(window.__gosxIOR.programInfo&&window.__gosxIOR.programInfo.linkStatus!==null?window.__gosxIOR.programInfo.linkStatus:null),' +
  'trackedF0:!!(window.__gosxIOR.programInfo&&window.__gosxIOR.programInfo.trackedF0),' +
  'activeUniforms:((window.__gosxIOR.programInfo&&window.__gosxIOR.programInfo.activeUniforms)||[]).slice(0,100),' +
  'queriedUniforms:(window.__gosxIOR.queriedUniforms||[]).slice(0,64),' +
  'shadow:(window.__gosxIOR?window.__gosxIOR.shadow:null),' +
  'nativeCap:(window.__gosxIOR?window.__gosxIOR.nativeCap:null),' +
  'forcedCap:(window.__gosxIOR?window.__gosxIOR.forcedCap:null),' +
  'skinColor:(window.__gosxIOR.skinColor||null),' +
  'skinShadow:(window.__gosxIOR.skinShadow||null),' +
  'deletedProgramIds:(window.__gosxIOR.deletedProgramIds||[]),' +
  'obsErrors:(window.__gosxIOR.obsErrors||[]).slice(0,4)}:null,' +
  'wgpu:window.__gosxWGPU?{uploads:window.__gosxWGPU.materialUploads,' +
  'dumps:window.__gosxWGPU.dumps.slice(-4),' +
  'envWords:((typeof window.__gosxWGPUReadEnvWords === "function") ? window.__gosxWGPUReadEnvWords() : null),' +
  'obsErrors:(window.__gosxWGPU.obsErrors||[]).slice(0,4),' +
  'wgdraws:(window.__gosxWGPUDraws?{observed:window.__gosxWGPUDraws.observed,' +
  'lastDepthWrite:(window.__gosxWGPUDraws.lastDepthWrite===true||' +
  'window.__gosxWGPUDraws.lastDepthWrite===false?window.__gosxWGPUDraws.lastDepthWrite:null),' +
  'lastBlend:(window.__gosxWGPUDraws.lastBlend===true||' +
  'window.__gosxWGPUDraws.lastBlend===false?window.__gosxWGPUDraws.lastBlend:null)}:null)}:null,' +
  'skinState:(function(){try{var st=m&&m.__gosxScene3DState;' +
  'if(!st||!st._modelSkins)return null;' +
  'var ms=st._modelSkins;var list=[];' +
  'if(Array.isArray(ms)){list=ms;}' +
  'else if(ms&&typeof ms.forEach==="function"){ms.forEach(function(s){list.push(s);});}' +
  'var rec=null;for(var i=0;i<list.length;i+=1){' +
  'if(list[i]&&list[i].id==="skin-caster"){rec=list[i];break;}}' +
  'if(!rec)return null;' +
  'var skin=(rec.skins&&rec.skins[0])||null;' +
  'var pal=(skin&&skin.jointMatrices)?skin.jointMatrices:null;' +
  'var palette=null;var paletteLength=0;' +
  'if(pal&&pal.length===16){palette=Array.prototype.slice.call(pal);paletteLength=16;}' +
  'else if(pal&&pal.length){paletteLength=pal.length;}' +
  'var root=(rec.rootTransform&&rec.rootTransform.length===16)?' +
  'Array.prototype.slice.call(rec.rootTransform):null;' +
  'var objs=st.objects;var oid=(rec.objectIDs&&rec.objectIDs.length?' +
  'rec.objectIDs[0]:null);' +
  'var o=(objs&&oid!==null&&typeof objs.get==="function")?objs.get(oid):null;' +
  'var obj=null;' +
  'if(o&&o.skin&&o.vertices&&o.vertices.joints&&o.vertices.weights){' +
  'obj={id:o.id,hasSkin:true,' +
  'jointsLength:o.vertices.joints.length,weightsLength:o.vertices.weights.length,' +
  'joints:Array.prototype.slice.call(o.vertices.joints,0,8),' +
  'weights:Array.prototype.slice.call(o.vertices.weights,0,8),' +
  'castShadow:(typeof o.castShadow==="boolean")?o.castShadow:null};}' +
  'var anim=(typeof rec.animation==="string")?rec.animation:"";' +
  'var api=window.__gosx_scene3d_animation_api;' +
  'var apiCaps=(!!api&&typeof api.buildNodeTransforms==="function"&&' +
  'typeof api.computeJointMatrices==="function");' +
  'return {records:list.length,recordID:(rec.id||null),' +
  'animation:(typeof anim==="string"?anim:(anim&&anim.name!==undefined?anim.name:null)),' +
  'hasMixer:Boolean(rec.mixer||rec.wasmMixerActive),rootTransform:root,palette:palette,' +
  'animationApiCapabilities:apiCaps,' +
  'animationApiMatchesRecord:(!!api&&rec.animationApi===api),' +
  'paletteLength:paletteLength,' +
  'objectCount:(objs&&typeof objs.size==="number"?objs.size:list.length),' +
  'object:obj};}catch(e){return null;}})()};})()';

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
    'var ci=((c.height>>1)*c.width+(c.width>>1))*4;var center=[d[ci],d[ci+1],d[ci+2]];' +
    'var fg=0,maxDelta=0,bgPixels=0;' +
    'for(var i=0;i<d.length;i+=4){' +
    'var df=Math.max(Math.abs(d[i]-bg[0]),Math.abs(d[i+1]-bg[1]),Math.abs(d[i+2]-bg[2]));' +
    'if(df>=FG_THRESHOLD){fg++;if(df>maxDelta)maxDelta=df;}' +
    'if(d[i]===bg[0]&&d[i+1]===bg[1]&&d[i+2]===bg[2])bgPixels++;}' +
    'var png=null;try{png=c.toDataURL("image/png").split(",")[1];}catch(e){}' +
    'res({w:c.width,h:c.height,bg:bg,center:center,fgPixels:fg,fgFrac:fg/n,maxDelta:maxDelta,bgPixels:bgPixels,png:png});}catch(e){res(null);}};' +
    'img.onerror=function(){res(null);};' +
    'img.src="data:image/png;base64,' + b64 + '";})';
  // Interpolate the threshold into the ENTIRE assembled expression so the
  // browser-side code never references a Node lexical constant.
  return expr.replace(/FG_THRESHOLD/g, String(FG_THRESHOLD));
}

// Compare two plain-base64 PNGs. exactBytes/exactPixels = zero-tolerance
// equality; meanChanged/maxDelta = meaningful-channel difference (>2 / max).
function diffExpr(a, b, roi) {
  // Optional ROI: absent/null compares the whole image. A provided ROI must
  // be Number.isInteger for ALL of x/y/width/height (no flooring, no
  // coercion, no whole-image fallback) with non-negative origin and
  // positive size, otherwise the expression resolves null, which fails the
  // same/diff assertion downstream.
  if (roi != null) {
    if (!Number.isInteger(roi.x) || !Number.isInteger(roi.y) ||
        !Number.isInteger(roi.width) || !Number.isInteger(roi.height) ||
        roi.x < 0 || roi.y < 0 || roi.width <= 0 || roi.height <= 0) {
      return 'Promise.resolve(null)';
    }
  }
  const R = roi ? JSON.stringify({ x: roi.x, y: roi.y,
    width: roi.width, height: roi.height }) : 'null';
  return 'new Promise(function(res){var A=new Image(),B=new Image(),R=' + R + ';var n=0;' +
    'function done(){try{if(++n<2)return;' +
    'if(A.width!==B.width||A.height!==B.height){res({dimsMatch:false});return;}' +
    'var c=document.createElement("canvas");c.width=A.width;c.height=A.height;' +
    'var x=c.getContext("2d");x.drawImage(A,0,0);var d1=x.getImageData(0,0,c.width,c.height).data;' +
    'x.clearRect(0,0,c.width,c.height);x.drawImage(B,0,0);var d2=x.getImageData(0,0,c.width,c.height).data;' +
    'var x0=0,y0=0,w=c.width,h=c.height;' +
    'if(R){x0=R.x;y0=R.y;w=R.width;h=R.height;' +
    'if(x0+w>c.width||y0+h>c.height){res(null);return;}}' +
    'var eb=0,ep=0,mp=0,md=0;' +
    'for(var yy=y0;yy<y0+h;yy++){for(var xx=x0;xx<x0+w;xx++){' +
    'var i=(yy*c.width+xx)*4;' +
    'var mx=Math.max(Math.abs(d1[i]-d2[i]),Math.abs(d1[i+1]-d2[i+1]),Math.abs(d1[i+2]-d2[i+2]),Math.abs(d1[i+3]-d2[i+3]));' +
    'if(mx>0){if(d1[i]!==d2[i])eb++;if(d1[i+1]!==d2[i+1])eb++;' +
    'if(d1[i+2]!==d2[i+2])eb++;if(d1[i+3]!==d2[i+3])eb++;ep++;' +
    'if(mx>md)md=mx;if(mx>2){mp++;}}}}' +
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

async function diffShots(send, a, b, roi) {
  return evalSend(send, diffExpr(a, b, roi), { awaitPromise: true });
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

// ---- Instanced multi-instance lifecycle execution helpers ----
// Test-only: production state is read through the existing READ snapshot
// plus read-only state extraction; renderer state, caches and revisions are
// never written and applySceneCommands is never called directly. Authored
// per-transition instance totals come from the fixture contract
// (both->left->right->moved->one->empty->both).
const INSTLIFE_TARGET_COUNTS = { left: 2, right: 2, moved: 2, one: 1,
  empty: 0, both: 2 };
const INSTLIFE_ID_SET = '(function(){var m=document.getElementById("' + MOUNT + '");' +
  'if(!m)return false;var cv=m.querySelector("canvas");var st=m.__gosxScene3DState;' +
  'if(!cv||!st)return false;' +
  'window.__instlifeRefs={mount:m,canvas:cv,state:st};return true;})()';
const INSTLIFE_ID_CHECK = '(function(){var r=window.__instlifeRefs;if(!r)return null;' +
  'var m=document.getElementById("' + MOUNT + '");var cv=m&&m.querySelector("canvas");' +
  'var st=m&&m.__gosxScene3DState;' +
  'return {sameMount:m===r.mount,sameCanvas:cv===r.canvas,sameState:st===r.state};})()';
const INSTLIFE_STATE_READ = '(function(){var m=document.getElementById("' + MOUNT + '");' +
  'var st=m&&m.__gosxScene3DState;if(!st)return null;' +
  'var im=st.instancedMeshes;if(!Array.isArray(im))return null;' +
  'var total=0;var entries=[];' +
  'for(var i=0;i<im.length;i+=1){var e=im[i];' +
  'if(!e||typeof e.count!=="number"||!isFinite(e.count)||e.count<0)return null;' +
  'if(e.transforms!==undefined&&!Array.isArray(e.transforms))return null;' +
  'if(e.colors!==undefined&&!Array.isArray(e.colors))return null;' +
  'total+=e.count;' +
  'entries.push({count:e.count,' +
  'transforms:Array.isArray(e.transforms)?e.transforms.slice():null,' +
  'colors:Array.isArray(e.colors)?e.colors.slice():null});}' +
  'return {batches:im.length,instances:total,entries:entries};})()';
function instlifeDispatchExpr(cmds, revision, commandCount) {
  return '(function(){return new Promise(function(res){' +
    'var cmds=JSON.parse(' + JSON.stringify(JSON.stringify(cmds)) + ');' +
    'var REV=' + Number(revision) + ',COUNT=' + Number(commandCount) + ';' +
    'var WAIT=' + CASE_WAIT_MS + ';' +
    'var m=document.getElementById("' + MOUNT + '");' +
    'if(!m||typeof m.dispatchEvent!=="function")return res(false);' +
    'var timer=null,h=null,done=false;' +
    'var finish=function(v){if(done)return;done=true;' +
    'if(timer!==null)clearTimeout(timer);' +
    'if(m.removeEventListener)m.removeEventListener("gosx:scene3d:commands-applied",h);' +
    'res(v);};' +
    'h=function(ev){var d=(ev&&ev.detail)||{};' +
    'if(typeof d.revision==="number"&&d.revision===REV&&' +
    'typeof d.commandCount==="number"&&d.commandCount===COUNT)finish(true);};' +
    'm.addEventListener("gosx:scene3d:commands-applied",h);' +
    'timer=setTimeout(function(){finish(false);},WAIT);' +
    'try{m.dispatchEvent(new CustomEvent("gosx:scene3d:commands",' +
    '{detail:{revision:REV,commands:cmds}}));}catch(e){finish(false);}});})()';
}
async function runInstancedLifecycleSteps(send, c, rec, initialCap, shots) {
  const steps = [];
  const targetPfx = c.webgpu ? 'wg-instlife-' : 'gl-instlife-';
  const refsOk = await evalSend(send, INSTLIFE_ID_SET);
  if (refsOk !== true) {
    fail(c.name + ': could not retain test-only mount/canvas/state references');
  }
  let prevCap = initialCap || null;
  for (let i = 0; i < c.lifecycleTransitions.length; i += 1) {
    const tr = c.lifecycleTransitions[i];
    const target = tr.to;
    const expCount = INSTLIFE_TARGET_COUNTS[target];
    const before = await evalSend(send, READ);
    if (!before || !before.ior || !before.wgpu) {
      throw new Error('lifecycle step ' + (i + 1) + ': pre-step evidence read failed');
    }
    // Public mount command seam only: gosx:scene3d:commands with a monotonic
    // revision; the applied event must match revision AND commandCount and
    // arrive within CASE_WAIT_MS (listener+timer cleaned up in-page).
    const ack = await evalSend(send,
      instlifeDispatchExpr(tr.commands, i + 1, tr.commands.length),
      { awaitPromise: true });
    if (ack !== true) {
      fail(c.name + ': step ' + (i + 1) + ' (' + tr.name + '): gosx:scene3d:commands-applied' +
        ' (revision ' + (i + 1) + ', commandCount ' + tr.commands.length + ')' +
        ' not observed within ' + CASE_WAIT_MS + 'ms');
    }
    // Positive new native render after the ack, on the expected backend with
    // no fallback/error and empty observer error arrays. WebGL evidence is
    // the pbrDraws advance; WebGPU evidence is the actual presented frame
    // sequence advance (never synthetic GL counters).
    let after = null;
    let advanced = false;
    const dl = Date.now() + CASE_WAIT_MS;
    while (Date.now() < dl) {
      after = await evalSend(send, READ);
      if (after && after.ior && after.wgpu) {
        const adv = c.webgpu
          ? Number(after.frameSeq) > Number(before.frameSeq || 0)
          : after.ior.pbrDraws > before.ior.pbrDraws;
        const backendOk = c.webgpu
          ? after.renderer === 'webgpu' && after.fallback !== 'true' && !after.wgpuErr
          : after.renderer === 'webgl';
        if (adv && backendOk &&
            after.ior.obsErrors.length === 0 && after.wgpu.obsErrors.length === 0) {
          advanced = true;
          break;
        }
      }
      await sleep(100);
    }
    if (!advanced || !after) {
      fail(c.name + ': step ' + (i + 1) + ' (' + tr.name + '): no positive new native render' +
        ' advance after command ack on the expected backend');
    }
    // WebGL native shadow-depth evidence only: additional instanced shadow
    // draws with the expected nonempty targets, and NO additional instanced
    // shadow draws on removal while the receiver PBR path still advances.
    let shadowDelta = null;
    if (!c.webgpu && advanced) {
      shadowDelta = after.ior.instShadowDraws - before.ior.instShadowDraws;
      if (expCount > 0) {
        if (!(shadowDelta > 0)) {
          fail(c.name + ': step ' + (i + 1) + ' (' + tr.name + '): expected additional' +
            ' instanced shadow-depth draws for instanceCount ' + expCount);
        } else if (after.ior.instShadowInstances !== expCount) {
          fail(c.name + ': step ' + (i + 1) + ' (' + tr.name + '): instanced shadow-depth' +
            ' last instanceCount ' + after.ior.instShadowInstances +
            ' != expected ' + expCount);
        }
      } else if (shadowDelta !== 0) {
        fail(c.name + ': step ' + (i + 1) + ' (' + tr.name + '): expected NO additional' +
          ' instanced shadow draws on removal, got +' + shadowDelta);
      }
    }
    // Same canvas, mount and state identities across updates, proven through
    // test-only retained references (production state is never mutated).
    const ident = advanced ? await evalSend(send, INSTLIFE_ID_CHECK) : null;
    if (!ident || ident.sameMount !== true || ident.sameCanvas !== true ||
        ident.sameState !== true) {
      fail(c.name + ': step ' + (i + 1) + ': mount/canvas/state identity changed across update');
    }
    // Actual normalized instanced state: exact authored instance total and
    // batch count, plus read-only transform/color match against the target
    // fixture scene arrays. Absent or malformed evidence fails closed.
    const st = advanced ? await evalSend(send, INSTLIFE_STATE_READ) : null;
    if (!st) {
      fail(c.name + ': step ' + (i + 1) + ': instanced state read failed');
    } else {
      if (st.instances !== expCount) {
        fail(c.name + ': step ' + (i + 1) + ' (' + tr.name + '): expected total authored' +
          ' instance count ' + expCount + ', got ' + st.instances);
      }
      if (st.batches !== (expCount > 0 ? 1 : 0)) {
        fail(c.name + ': step ' + (i + 1) + ': expected ' + (expCount > 0 ? 1 : 0) +
          ' instanced batch(es), got ' + st.batches);
      }
      const fxMesh = (INST_FIXTURE.scenes[target] &&
        Array.isArray(INST_FIXTURE.scenes[target].instancedMeshes))
        ? INST_FIXTURE.scenes[target].instancedMeshes[0] : null;
      if (expCount > 0) {
        if (!fxMesh || !st.entries[0]) {
          fail(c.name + ': step ' + (i + 1) + ': missing fixture or observed instanced' +
            ' entry for ' + target);
        } else {
          const obs = st.entries[0];
          const fxT = Array.isArray(fxMesh.transforms) ? fxMesh.transforms : null;
          const fxC = Array.isArray(fxMesh.colors) ? fxMesh.colors : null;
          if (!fxT || fxT.length !== 16 * expCount ||
              !fxC || fxC.length !== expCount) {
            fail(c.name + ': step ' + (i + 1) + ': fixture scene ' + target +
              ' transforms/colors not resolvable for ' + expCount + ' instances');
          } else if (!Array.isArray(obs.transforms) || obs.transforms.length !== 16 * expCount ||
                     !Array.isArray(obs.colors) || obs.colors.length !== expCount) {
            fail(c.name + ': step ' + (i + 1) + ': observed instanced transforms/colors' +
              ' are not flat arrays of the expected size');
          } else if (JSON.stringify(obs.transforms) !== JSON.stringify(fxT) ||
                     JSON.stringify(obs.colors) !== JSON.stringify(fxC)) {
            fail(c.name + ': step ' + (i + 1) + ' (' + tr.name + '): observed instanced' +
              ' transforms/colors are not exactly equal to fixture scene ' + target);
          }
        }
      }
    }
    // Settle, re-check dimensions/errors, then capture this step.
    await sleep(SETTLE_MS);
    const settled = advanced ? await evalSend(send, READ) : null;
    if (!settled || !settled.ior || !settled.wgpu) {
      throw new Error('lifecycle step ' + (i + 1) + ': settled evidence read failed');
    }
    if (settled.mounted !== 'true') {
      fail(c.name + ': step ' + (i + 1) + ': data-gosx-scene3d-mounted not true after settle');
    }
    if (settled.ior.obsErrors.length) {
      fail(c.name + ': step ' + (i + 1) + ': GL observation errors: ' +
        settled.ior.obsErrors.join('; '));
    }
    if (settled.wgpu.obsErrors.length) {
      fail(c.name + ': step ' + (i + 1) + ': WebGPU observation errors: ' +
        settled.wgpu.obsErrors.join('; '));
    }
    if (c.webgpu && (settled.renderer !== 'webgpu' || settled.fallback === 'true' ||
        settled.wgpuErr)) {
      fail(c.name + ': step ' + (i + 1) + ': WebGPU backend not active after settle');
    }
    if (!c.webgpu && settled.renderer !== 'webgl') {
      fail(c.name + ': step ' + (i + 1) + ': WebGL backend not active after settle');
    }
    // Settled re-verification: a transient early good state must never
    // certify a bad settled state. Identity, observed count/batches and the
    // GL shadow evidence are all re-checked on the settled read before any
    // screenshot is taken.
    const settledIdent = await evalSend(send, INSTLIFE_ID_CHECK);
    if (!settledIdent || settledIdent.sameMount !== true ||
        settledIdent.sameCanvas !== true || settledIdent.sameState !== true) {
      fail(c.name + ': step ' + (i + 1) + ': mount/canvas/state identity changed after settle');
    }
    const settledState = await evalSend(send, INSTLIFE_STATE_READ);
    if (!settledState) {
      fail(c.name + ': step ' + (i + 1) + ': settled instanced state read failed or malformed');
    } else {
      if (settledState.instances !== expCount) {
        fail(c.name + ': step ' + (i + 1) + ' (' + tr.name + '): settled instance count ' +
          settledState.instances + ' != expected ' + expCount);
      }
      if (settledState.batches !== (expCount > 0 ? 1 : 0)) {
        fail(c.name + ': step ' + (i + 1) + ': settled batch count ' +
          settledState.batches + ' != expected ' + (expCount > 0 ? 1 : 0));
      }
    }
    if (!c.webgpu) {
      const settledDelta = settled.ior.instShadowDraws - before.ior.instShadowDraws;
      if (expCount > 0) {
        if (!(typeof settledDelta === 'number' && Number.isFinite(settledDelta) &&
              settledDelta > 0)) {
          fail(c.name + ': step ' + (i + 1) + ' (' + tr.name + '): settled instanced' +
            ' shadow-depth draws did not advance for instanceCount ' + expCount);
        } else if (settled.ior.instShadowInstances !== expCount) {
          fail(c.name + ': step ' + (i + 1) + ' (' + tr.name + '): settled instanced' +
            ' shadow-depth last instanceCount ' + settled.ior.instShadowInstances +
            ' != expected ' + expCount);
        }
      } else if (settledDelta !== 0) {
        fail(c.name + ': step ' + (i + 1) + ' (' + tr.name + '): settled instanced shadow' +
          ' draws changed on removal, got +' + settledDelta);
      }
    }
    const capStep = await capture(send);
    if (!capStep) throw new Error('lifecycle step ' + (i + 1) + ': capture/decode failed');
    if (capStep.clip.width !== W || capStep.clip.height !== H) {
      fail(c.name + ': step ' + (i + 1) + ': canvas rect ' + capStep.clip.width + 'x' +
        capStep.clip.height + ' != expected ' + W + 'x' + H);
    }
    if (capStep.metrics.w !== capStep.expectW || capStep.metrics.h !== capStep.expectH) {
      fail(c.name + ': step ' + (i + 1) + ': screenshot dimensions ' + capStep.metrics.w +
        'x' + capStep.metrics.h + ' != expected ' + capStep.expectW + 'x' + capStep.expectH);
    }
    if (!(capStep.metrics.fgPixels > 0) || !(capStep.metrics.fgFrac >= FG_COVERAGE) ||
        !(capStep.metrics.maxDelta >= FG_THRESHOLD)) {
      fail(c.name + ': step ' + (i + 1) + ': no measurable geometry foreground after settle' +
        ' (fg=' + capStep.metrics.fgPixels + ', frac=' + capStep.metrics.fgFrac.toFixed(4) + ')');
    }
    // Receiver ROI must be byte-for-byte identical to the FRESH static target
    // scene captured earlier on the same backend.
    const targetShot = shots[targetPfx + target];
    let dTarget = null;
    if (!targetShot) {
      fail(c.name + ': step ' + (i + 1) + ': missing fresh static target capture for ' + target);
    } else {
      dTarget = await diffShots(send, capStep.base64, targetShot.base64, c.shadowROI);
      if (!dTarget || !dTarget.dimsMatch || dTarget.exactPixels !== 0 ||
          dTarget.exactBytes !== 0) {
        fail(c.name + ': step ' + (i + 1) + ' (' + tr.name + '): receiver ROI not' +
          ' byte-identical to fresh static ' + target + ' (exactPixels=' +
          (dTarget && dTarget.exactPixels) + ', exactBytes=' +
          (dTarget && dTarget.exactBytes) + ')');
      }
    }
    // Meaningful visible change vs the previous lifecycle snapshot.
    let dPrev = null;
    if (prevCap) {
      dPrev = await diffShots(send, prevCap.base64, capStep.base64, c.shadowROI);
      if (!dPrev || !dPrev.dimsMatch || !(dPrev.meanChanged >= 50)) {
        fail(c.name + ': step ' + (i + 1) + ' (' + tr.name + '): expected >=50 changed' +
          ' receiver ROI pixels vs the previous lifecycle snapshot (meanChanged=' +
          (dPrev && dPrev.meanChanged) + ')');
      }
    }
    saveArtifact(c.name + '-step' + (i + 1) + '.png', capStep.base64);
    // Report the SETTLED evidence: draw/frame advances and identity are taken
    // from the settled read, not the transient post-update read.
    const renderAdvance = (advanced && settled) ? (c.webgpu
      ? { frameSeqBefore: Number(before.frameSeq || 0),
          frameSeqAfter: Number(settled.frameSeq || 0) }
      : { pbrDrawsBefore: before.ior.pbrDraws,
          pbrDrawsAfter: settled.ior.pbrDraws }) : null;
    steps.push({
      step: i + 1, transition: tr.name, from: tr.from, to: target,
      commandAck: ack === true, revision: i + 1, commandCount: tr.commands.length,
      renderAdvance,
      shadowDepthDrawsDelta: shadowDelta,
      settledShadowDepthDrawsDelta: c.webgpu ? null :
        (settled.ior.instShadowDraws - before.ior.instShadowDraws),
      shadowDepthLastInstanceCount: c.webgpu ? null : settled.ior.instShadowInstances,
      observedInstanceCount: settledState ? settledState.instances : null,
      observedBatches: settledState ? settledState.batches : null,
      identity: settledIdent,
      targetPixelDiff: dTarget,
      beforeAfterPixelDiff: dPrev,
    });
    prevCap = capStep;
  }
  rec.lifecycleSteps = steps;
}

// ---- Skinned (INITIAL) shared pure helpers ----
// skinMatrixMatches: exactly 16 finite numbers, identity except index 12 == tx.
function skinMatrixMatches(actual, tx) {
  // Finite tx guard: a NaN translation must never let a matrix "match".
  if (!Number.isFinite(tx)) return false;
  if (!Array.isArray(actual) || actual.length !== 16) return false;
  for (let i = 0; i < 16; i += 1) {
    const v = actual[i];
    if (typeof v !== 'number' || !Number.isFinite(v)) return false;
    const want = (i === 12) ? tx :
      ((i === 0 || i === 5 || i === 10 || i === 15) ? 1 : 0);
    if (Math.abs(v - want) > 1e-5) return false;
  }
  return true;
}
// skinStateReady: exactly one skin-caster record, real skinned object state,
// full 16-entry palette, identity root transform, and the mixer/animation
// fields this case expects. Shared by both backends for readiness and for
// the post-settle re-assertion.
function skinStateReady(snapshot, c, tx) {
  const st = snapshot && snapshot.skinState;
  if (!st || st.records !== 1 || st.recordID !== 'skin-caster') return false;
  const o = st.object;
  if (!o || o.hasSkin !== true || o.castShadow !== true) return false;
  if (!(o.jointsLength >= 4) || !(o.weightsLength >= 4)) return false;
  const j4 = (o.joints || []).slice(0, 4);
  const w4 = (o.weights || []).slice(0, 4);
  if (j4.length < 4 || w4.length < 4) return false;
  for (let i = 0; i < 4; i += 1) {
    // joints first 4 all 0; weights first 4 exactly [1, 0, 0, 0].
    if (j4[i] !== 0) return false;
    if (w4[i] !== (i === 0 ? 1 : 0)) return false;
  }
  if (st.paletteLength !== 16) return false;
  // The ACTUAL joint palette must match the expected translation tx.
  if (!skinMatrixMatches(st.palette, tx)) return false;
  // The model root is ALWAYS identity; tx lives in the joint palette only.
  if (!skinMatrixMatches(st.rootTransform, 0)) return false;
  if (Boolean(c.skinnedExpectMixer) !== (st.hasMixer === true)) return false;
  // Fail closed: the actually published animation API must expose the real
  // capabilities and be the very API stored on the skin record.
  if (st.animationApiCapabilities !== true ||
      st.animationApiMatchesRecord !== true) return false;
  return st.animation === (c.skinnedExpectPose ? 'pose' : '');
}

// skinDrawMatches: pure predicate over the ACTUAL native observer record
// shape produced by observeSkinnedDraw (skinColor / skinShadow): draws,
// lastHasSkin, lastProgramId, lastPalette0, lastModelMatrix, lastOpacity,
// lastAlphaCutoff. Opacity/cutoff compare EXACTLY (0.5 / 0.25 are exact
// binary fractions); tx is finite-guarded inside skinMatrixMatches.
function skinDrawMatches(draw, tx, opacity, cutoff) {
  if (!draw || !(draw.draws > 0)) return false;
  if (!(draw.lastHasSkin === true || draw.lastHasSkin === 1)) return false;
  if (typeof draw.lastProgramId !== 'number' ||
      !Number.isInteger(draw.lastProgramId) || !(draw.lastProgramId > 0)) {
    return false;
  }
  if (!skinMatrixMatches(draw.lastPalette0, tx)) return false;
  if (!skinMatrixMatches(draw.lastModelMatrix, 0)) return false;
  return draw.lastOpacity === opacity && draw.lastAlphaCutoff === cutoff;
}

// ---- Skinned LIVE 2-step public hub event run (test-only; state read-only).
// Driven exclusively through the public document event seam (gosx:hub:event).
// All MOUNT-dependent expressions are built inside these called functions so
// the VM helper unit slice (skinMatrixMatches..watchdog) needs no MOUNT
// global at definition time; cleanup deletes only the test-only window refs.
function skinLiveHubDetail(animation) {
  return { event: 'skin-pose', data: { 'skin-caster': {
    animation: animation, loop: animation === 'pose',
    animationFadeInMS: 0, animationFadeOutMS: 0 } } };
}
function skinLiveRefsSetExpr() {
  return '(function(){var m=document.getElementById(' + JSON.stringify(MOUNT) + ');' +
    'if(!m)return false;var cv=m.querySelector("canvas");var st=m.__gosxScene3DState;' +
    'if(!cv||!st)return false;var sk=st._modelSkins;if(!Array.isArray(sk))return false;' +
    'var rec=null;for(var i=0;i<sk.length;i+=1){' +
    'if(sk[i]&&sk[i].id==="skin-caster"){rec=sk[i];break;}}if(!rec)return false;' +
    'window.__skinLiveRefs={mount:m,canvas:cv,state:st,record:rec};return true;})()';
}
function skinLiveRefsCheckExpr() {
  return '(function(){var r=window.__skinLiveRefs;if(!r)return null;' +
    'var m=document.getElementById(' + JSON.stringify(MOUNT) + ');' +
    'var cv=m&&m.querySelector("canvas");var st=m&&m.__gosxScene3DState;' +
    'var rec=null;var sk=st&&st._modelSkins;if(Array.isArray(sk)){' +
    'for(var i=0;i<sk.length;i+=1){' +
    'if(sk[i]&&sk[i].id==="skin-caster"){rec=sk[i];break;}}}' +
    'return {sameMount:m===r.mount,sameCanvas:cv===r.canvas,' +
    'sameState:st===r.state,sameRecord:rec===r.record};})()';
}
function skinLiveDispatchExpr(detail) {
  return '(function(){try{document.dispatchEvent(new CustomEvent("gosx:hub:event",' +
    '{detail:' + JSON.stringify(detail) + '}));return true;}catch(e){return false;}})()';
}
async function runSkinnedShadowLiveSteps(send, c, rec, cap, shots, evidence) {
  const steps = [];
  const targetPfx = c.webgpu ? 'wg-sk-' : 'gl-sk-';
  const phases = [{ name: 'pose', animation: 'pose', tx: 0.8, expectPose: true },
    { name: 'rest', animation: '', tx: 0, expectPose: false }];
  const identCheck = async (label) => {
    const id = await evalSend(send, skinLiveRefsCheckExpr());
    const ok = !!id && id.sameMount === true && id.sameCanvas === true &&
      id.sameState === true && id.sameRecord === true;
    if (!ok) fail(c.name + ': phase ' + label + ': mount/canvas/state/record identity changed');
    return id;
  };
  const checkCommon = (snap, label) => {
    if (snap.mounted !== 'true') fail(c.name + ': phase ' + label + ': mounted flag not true');
    if (snap.ior.obsErrors.length || snap.wgpu.obsErrors.length) {
      fail(c.name + ': phase ' + label + ': observation errors: GL=[' +
        snap.ior.obsErrors.join('; ') + '] WG=[' + snap.wgpu.obsErrors.join('; ') + ']');
    }
    if (snap.renderer !== (c.webgpu ? 'webgpu' : 'webgl') ||
        (c.webgpu && (snap.fallback === 'true' || snap.wgpuErr))) {
      fail(c.name + ': phase ' + label + ': expected backend not active');
    }
  };
  const checkShot = (s, label) => {
    if (!s) throw new Error('skinned live phase ' + label + ': capture/decode failed');
    if (s.clip.width !== W || s.clip.height !== H ||
        s.metrics.w !== s.expectW || s.metrics.h !== s.expectH) {
      fail(c.name + ': phase ' + label + ': capture dims mismatch rect=' + s.clip.width +
        'x' + s.clip.height + ' shot=' + s.metrics.w + 'x' + s.metrics.h + ' expect=' +
        s.expectW + 'x' + s.expectH);
    }
    if (!(s.metrics.fgPixels > 0) || !(s.metrics.fgFrac >= FG_COVERAGE) ||
        !(s.metrics.maxDelta >= FG_THRESHOLD)) {
      fail(c.name + ': phase ' + label + ': no measurable geometry foreground (fg=' +
        s.metrics.fgPixels + ', frac=' + s.metrics.fgFrac.toFixed(4) + ')');
    }
  };
  try {
    if (await evalSend(send, skinLiveRefsSetExpr()) !== true) {
      fail(c.name + ': could not retain test-only mount/canvas/state/record references');
    }
    let prevCap = cap || null;
    for (let i = 0; i < phases.length; i += 1) {
      const ph = phases[i];
      const before = await evalSend(send, READ);
      if (!before || !before.ior || !before.wgpu || !before.skinState ||
          before.ior.obsErrors.length || before.wgpu.obsErrors.length) {
        throw new Error('skinned live phase ' + (i + 1) +
          ': before-event evidence read failed or observation errors present');
      }
      const prePose = skinStateReady(before, { ...c, skinnedExpectPose: !ph.expectPose },
        ph.expectPose ? 0 : 0.8);
      if (!prePose || (!c.webgpu && (!before.ior.skinColor || !before.ior.skinShadow))) {
        throw new Error('skinned live phase ' + (i + 1) + ': before-event skin state is not ' +
          (ph.expectPose ? 'rest tx=0' : 'pose tx=0.8') + ' or GL skin draw records missing');
      }
      // Public seam only: dispatch accepted merely means no throw; the
      // recorded animation, actual palette pose and render advance prove it.
      const dispatched = await evalSend(send,
        skinLiveDispatchExpr(skinLiveHubDetail(ph.animation)), { awaitPromise: true });
      if (dispatched !== true) fail(c.name + ': phase ' + ph.name + ': gosx:hub:event dispatch threw');
      let applied = null;
      const dl = Date.now() + CASE_WAIT_MS;
      for (;;) {
        applied = await evalSend(send, READ);
        if (applied && applied.skinState && applied.ior && applied.wgpu) {
          const stable = applied.mounted === 'true' &&
            applied.renderer === (c.webgpu ? 'webgpu' : 'webgl') &&
            (!c.webgpu || (applied.fallback !== 'true' && !applied.wgpuErr)) &&
            applied.ior.obsErrors.length === 0 &&
            applied.wgpu.obsErrors.length === 0;
          const stateOk = skinStateReady(applied,
            { ...c, skinnedExpectPose: ph.expectPose }, ph.tx);
          const glOk = !c.webgpu && applied.ior.skinColor && applied.ior.skinShadow &&
            applied.ior.skinColor.draws > before.ior.skinColor.draws &&
            applied.ior.skinShadow.draws > before.ior.skinShadow.draws &&
            skinDrawMatches(applied.ior.skinColor, ph.tx, 1, -1) &&
            skinDrawMatches(applied.ior.skinShadow, ph.tx, 1, -1);
          const wgOk = c.webgpu && !applied.wgpuErr &&
            Number(applied.frameSeq || 0) > Number(before.frameSeq || 0);
          if (stable && stateOk && (glOk || wgOk)) break;
        }
        if (Date.now() >= dl) break;
        await sleep(50);
      }
      if (!applied || !applied.skinState || !applied.ior || !applied.wgpu) {
        throw new Error('skinned live phase ' + (i + 1) + ': post-dispatch evidence read failed');
      }
      if (applied.skinState.animation !== ph.animation) fail(c.name + ': phase ' + ph.name + ': event accepted but recorded animation did not become ' + JSON.stringify(ph.animation) + ' within CASE_WAIT_MS');
      checkCommon(applied, ph.name);
      if (!skinStateReady(applied, { ...c, skinnedExpectPose: ph.expectPose }, ph.tx)) {
        fail(c.name + ': phase ' + ph.name + ': actual skin state/palette did not reach ' + (ph.expectPose ? 'pose tx=0.8' : 'rest tx=0'));
      }
      await identCheck(ph.name);
      await sleep(SETTLE_MS);
      const settled = await evalSend(send, READ);
      if (!settled || !settled.ior || !settled.wgpu || !settled.skinState) {
        throw new Error('skinned live phase ' + (i + 1) + ': settled evidence read failed');
      }
      checkCommon(settled, ph.name);
      if (!skinStateReady(settled, { ...c, skinnedExpectPose: ph.expectPose }, ph.tx)) {
        fail(c.name + ': phase ' + ph.name + ': settled skin state/palette is not ' + (ph.expectPose ? 'pose tx=0.8' : 'rest tx=0'));
      }
      const glDelta = c.webgpu ? null : settled.ior.skinColor.draws - before.ior.skinColor.draws;
      const shDelta = c.webgpu ? null : settled.ior.skinShadow.draws - before.ior.skinShadow.draws;
      if (!c.webgpu && (!(glDelta > 0) || !(shDelta > 0))) {
        fail(c.name + ': phase ' + ph.name + ': settled GL skin draws did not advance (color=+' + glDelta + ', shadow=+' + shDelta + ')');
      }
      if (!c.webgpu && (!skinDrawMatches(settled.ior.skinColor, ph.tx, 1, -1) ||
          !skinDrawMatches(settled.ior.skinShadow, ph.tx, 1, -1))) {
        fail(c.name + ': phase ' + ph.name + ': settled GL color/shadow draw records do not match palette tx=' + ph.tx + ' (opacity 1, alphaCutoff -1)');
      }
      if (c.webgpu && !(Number(settled.frameSeq || 0) > Number(before.frameSeq || 0))) fail(c.name + ': phase ' + ph.name + ': settled frame sequence did not advance');
      const settledIdent = await identCheck(ph.name + ' settled');
      const capStep = await capture(send);
      checkShot(capStep, ph.name);
      const targetName = targetPfx + 'ref-' + ph.name;
      const targetShot = shots[targetName];
      const refEv = (Array.isArray(evidence) ? evidence : []).find(
        (ev) => ev && ev.name === targetName);
      if (!refEv || refEv.skipped ||
          refEv.renderer !== (c.webgpu ? 'webgpu' : 'webgl')) {
        fail(c.name + ': phase ' + ph.name + ': reference evidence for ' + targetName +
          ' missing, skipped, or actual renderer is not ' + (c.webgpu ? 'webgpu' : 'webgl'));
      }
      let dTarget = null;
      if (!targetShot) {
        fail(c.name + ': phase ' + ph.name + ': missing same-backend static reference capture ' + targetName);
      } else {
        dTarget = await diffShots(send, capStep.base64, targetShot.base64, c.shadowROI);
        if (!dTarget || !dTarget.dimsMatch || dTarget.exactPixels !== 0 || dTarget.exactBytes !== 0) {
          fail(c.name + ': phase ' + ph.name + ': receiver ROI not byte-identical to static ref-' + ph.name + ' (exactPixels=' + (dTarget && dTarget.exactPixels) + ', exactBytes=' + (dTarget && dTarget.exactBytes) + ')');
        }
      }
      let dPrev = null;
      if (prevCap) {
        dPrev = await diffShots(send, prevCap.base64, capStep.base64, c.shadowROI);
        if (!dPrev || !dPrev.dimsMatch || !(dPrev.meanChanged >= 50)) {
          fail(c.name + ': phase ' + ph.name + ': expected >=50 changed receiver ROI pixels vs previous snapshot (meanChanged=' + (dPrev && dPrev.meanChanged) + ')');
        }
      }
      saveArtifact(c.name + '-sklive-' + ph.name + '.png', capStep.base64);
      steps.push({
        phase: ph.name, eventDispatched: dispatched === true, identitySettled: settledIdent,
        actualPalette: Array.isArray(settled.skinState.palette) ?
          settled.skinState.palette.slice() : null,
        animation: settled.skinState.animation,
        renderAdvance: c.webgpu ? { frameSeqBefore: Number(before.frameSeq || 0),
          frameSeqAfter: Number(settled.frameSeq || 0) } :
          { skinColorDrawsBefore: before.ior.skinColor.draws,
            skinColorDrawsAfter: settled.ior.skinColor.draws,
            skinShadowDrawsBefore: before.ior.skinShadow.draws,
            skinShadowDrawsAfter: settled.ior.skinShadow.draws },
        glShadowDelta: shDelta, targetPixelDiff: dTarget, beforeAfterPixelDiff: dPrev,
      });
      prevCap = capStep;
    }
    if (steps.length !== 2) {
      fail(c.name + ': expected 2 skinned live steps, ran ' + steps.length);
    }
    rec.skinnedLiveSteps = steps;
  } finally {
    await evalSend(send, '(function(){try{delete window.__skinLiveRefs;}catch(e){}return true;})()');
  }
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
  // record the remaining cases as aborted (preserving all cases) and exit
  // nonzero via the diagnostic failure below. Normal cases still all run.
  let readinessFatal = false;
  for (const c of RUN_CASES) {
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
    if (c.expectedOpacity !== undefined) rec.expectedOpacity = c.expectedOpacity;
    if (c.expectedAlphaCutoff !== undefined) rec.expectedAlphaCutoff = c.expectedAlphaCutoff;
    if (c.expectedDepthWrite !== undefined) rec.expectedDepthWrite = c.expectedDepthWrite;
    if (c.expectedBlend !== undefined) rec.expectedBlend = c.expectedBlend;
    if (typeof c.expectedUnlit === 'boolean') rec.expectedUnlit = c.expectedUnlit;
    if (c.expectedEmpty === true) rec.expectedEmpty = true;
    evidence.push(rec);
    // IBL expectation: requiresIBL === true selects the positive gate;
    // noIBL === true selects the explicit-disabled assertions. Cases
    // without either flag are not IBL-asserted.
    const iblExpected = c.requiresIBL === true;
    const iblDisabled = c.noIBL === true;
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
      if (iblExpected && ready) {
        // Actual IBL readiness gate: normal scene readiness alone is not
        // enough for IBL-required cases. Poll the observation READ until the
        // selected backend reports real IBL state, before settle/screenshot.
        let iblReady = false;
        const iblDeadline = Date.now() + CASE_WAIT_MS;
        while (Date.now() < iblDeadline) {
          const sI = await evalSend(send, READ);
          if (sI && (c.webgpu
            ? sI.wgpu && sI.wgpu.envWords && sI.wgpu.envWords.hasIBL === 1 &&
              sI.wgpu.envWords.mips === 2 && sI.wgpu.envWords.hasEnvMap === 0
            : sI.ior && sI.ior.lastDrawHasIBL === true)) { iblReady = true; break; }
          await sleep(100);
        }
        if (!iblReady) {
          readinessFatal = true;
          fail(c.name + ': IBL not observed ready (hasIBL/mips/hasEnvMap) within ' +
            CASE_WAIT_MS + 'ms');
        }
      }
      if ((c.specTex || c.specColorTex) && ready) {
        // Texture cases must additionally wait, boundedly, for the REAL
        // texture-loaded state before settling/capturing. WebGPU: the
        // intensity flag at u32 index 41 (byte 164) and the color flag at u32
        // index 51 (byte 204) of actual 208-byte uploads, each read as a
        // uint32 word; combined color+intensity cases require BOTH flags
        // present in the SAME 208-byte dump (separate partial dumps never
        // satisfy combined readiness). WebGL: the real u_hasSpecularIntensityMap
        // uniform observed at production PBR draw time (getUniformLocation
        // tracking + getUniform), read as true/1; missing locations or
        // observations are null and never pass as readiness.
        let intFlagReady = !c.specTex;
        let colorFlagReady = !c.specColorTex;
        const texDeadline = Date.now() + CASE_WAIT_MS;
        while (Date.now() < texDeadline && !(intFlagReady && colorFlagReady)) {
          const sT = await evalSend(send, READ);
          if (c.webgpu) {
            const dumpsT = (sT && sT.wgpu && sT.wgpu.dumps) || [];
            if (c.specTex && c.specColorTex) {
              if (dumpsT.some((d) => d.hasSpecIntensityMap === 1 && d.hasSpecColorMap === 1)) {
                intFlagReady = true;
                colorFlagReady = true;
              }
            } else {
              if (!intFlagReady && dumpsT.some((d) => d.hasSpecIntensityMap === 1)) intFlagReady = true;
              if (!colorFlagReady && dumpsT.some((d) => d.hasSpecColorMap === 1)) colorFlagReady = true;
            }
          } else if (c.specTex && c.specColorTex) {
            // WebGL combined color+intensity case: BOTH draw-time flags must
            // be observed true/1 in the SAME sT.ior snapshot (the same PBR
            // draw); readiness is never accumulated from different draws.
            if (sT && sT.ior &&
                (sT.ior.lastDrawHasSpecIntensityMap === true ||
                 sT.ior.lastDrawHasSpecIntensityMap === 1) &&
                (sT.ior.lastDrawHasSpecColorMap === true ||
                 sT.ior.lastDrawHasSpecColorMap === 1)) {
              intFlagReady = true;
              colorFlagReady = true;
            }
          } else {
            // WebGL single-role texture cases: only an explicit draw-time
            // observation of true/1 counts as loaded.
            if (!intFlagReady && sT && sT.ior &&
                (sT.ior.lastDrawHasSpecIntensityMap === true ||
                 sT.ior.lastDrawHasSpecIntensityMap === 1)) {
              intFlagReady = true;
            }
            if (!colorFlagReady && sT && sT.ior &&
                (sT.ior.lastDrawHasSpecColorMap === true ||
                 sT.ior.lastDrawHasSpecColorMap === 1)) {
              colorFlagReady = true;
            }
          }
          if (intFlagReady && colorFlagReady) break;
          await sleep(100);
        }
        // Report each flag only for a role the case actually uses; never
        // claim a flag was loaded for an unused role.
        if (c.specTex) rec.specIntensityMapFlag = intFlagReady ? 1 : 0;
        if (c.specColorTex) rec.specColorMapFlag = colorFlagReady ? 1 : 0;
        if (!intFlagReady) {
          fail(c.name + (c.webgpu
            ? ': hasSpecularIntensityMap flag not observed as 1 in any 208-byte upload within '
            : ': u_hasSpecularIntensityMap not observed loaded (true/1) at production draw within ') +
            CASE_WAIT_MS + 'ms');
        }
        if (!colorFlagReady) {
          fail(c.name + (c.webgpu
            ? ': hasSpecularColorMap flag not observed as 1 in any 208-byte upload within '
            : ': u_hasSpecularColorMap not observed loaded (true/1) at production draw within ') +
            CASE_WAIT_MS + 'ms');
        }
      }
      let albedoReady = false;
      if (c.albedoTex && ready) {
        const albDeadline = Date.now() + CASE_WAIT_MS;
        while (Date.now() < albDeadline && !albedoReady) {
          const ast = await evalSend(send, READ);
          if (ast && ast.ior && ast.wgpu) {
            albedoReady = c.webgpu
              ? (ast.wgpu.dumps || []).some((d) => d.hasAlbedoMap === 1)
              : ast.ior.lastDrawHasAlbedoMap === true;
          }
          if (!albedoReady) await sleep(50);
        }
        if (!albedoReady) {
          fail(c.name + (c.webgpu
            ? ': hasAlbedoMap flag not observed as 1 in any 208-byte upload within '
            : ': u_hasAlbedoMap not observed loaded (true/1) at production draw within ') +
            CASE_WAIT_MS + 'ms');
        }
      }
      // Shared skinned expectations: initial palette tx (rest/live 0, pose
      // and mask 0.8) plus this case's exact draw opacity/cutoff values.
      const skinTx = c.skinnedExpectPose ? 0.8 : 0;
      const wantOp = (c.skinnedMaskOpacity !== undefined) ?
        c.skinnedMaskOpacity : 1;
      const wantCut = (c.skinnedMaskCutoff !== undefined) ?
        c.skinnedMaskCutoff : -1;
      if (c.skinnedGlb && ready) {
        // INITIAL skinned readiness: the real skin state record plus actual
        // backend skin output, before settle/READ/capture. Stable backends
        // and empty observer error lists are part of the readiness
        // predicate. Shadow draws are deliberately NOT required here: the
        // old baseline has none, and that must be a recorded failure after
        // ready, not a readiness abort.
        const skinBackendStable = (sK) => Boolean(sK) &&
          sK.fallback !== 'true' &&
          Boolean(sK.ior) && sK.ior.obsErrors.length === 0 &&
          Boolean(sK.wgpu) && sK.wgpu.obsErrors.length === 0;
        let skinReady = false;
        let skinDiag = null;
        const skinDeadline = Date.now() + CASE_WAIT_MS;
        while (Date.now() < skinDeadline) {
          const sK = await evalSend(send, READ);
          skinDiag = sK;
          if (skinBackendStable(sK) && skinStateReady(sK, c, skinTx) &&
              (c.webgpu
                ? !sK.wgpuErr && Number(sK.frameSeq || 0) > 0
                : skinDrawMatches(sK.ior.skinColor, skinTx, wantOp, wantCut))) {
            skinReady = true;
            break;
          }
          await sleep(100);
        }
        if (!skinReady) {
          readinessFatal = true;
          fail(c.name + ': skinned scene state/output not ready within ' +
            CASE_WAIT_MS + 'ms; skinState: ' +
            (skinDiag ? JSON.stringify(skinDiag.skinState) : String(skinDiag)));
        }
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
        uniformF90: s.ior.lastDrawF90,
        wgpuUploads: s.wgpu.uploads,
      });

      if (c.skinnedGlb) {
        rec.skinEvidence = {
          state: s.skinState, color: s.ior.skinColor,
          shadow: s.ior.skinShadow,
          glbServed: skinnedGlbServed[c.skinnedGlb],
          animationChunkServed: animChunkServed,
          animationSource: (animChunkServed > 0) ? 'chunk' :
            ((s.skinState && s.skinState.animationApiCapabilities) ? 'embedded' : 'none'),
          animationApiObserved: Boolean(s.skinState && s.skinState.animationApiCapabilities),
          animationApiMatchesRecord: Boolean(s.skinState && s.skinState.animationApiMatchesRecord),
          frameSeq: Number(s.frameSeq || 0),
        };
        if (!(rec.skinEvidence.glbServed > 0)) {
          fail(c.name + ': skinned GLB asset not served: ' + c.skinnedGlb);
        }
        // The monolithic bootstrap embeds window.__gosx_scene3d_animation_api,
        // so ensureAnimationFeatureLoaded normally resolves without a chunk
        // request. The published API must be observed either way (embedded or
        // an actually served chunk); report the real chunk count only.
        if (!rec.skinEvidence.animationApiObserved) {
          fail(c.name + ': scene3d animation API not observed (embedded API absent and no animation chunk served)');
        }
        if (!s.skinState || !s.skinState.object) {
          fail(c.name + ': missing skinned state record/object (missing evidence fails, not skips)');
        } else {
          // Same predicates as readiness, asserted again post-settle (not
          // transient-only) against ACTUAL observer fields.
          if (!skinStateReady(s, c, skinTx)) {
            fail(c.name + ': skinned state not ready after settle: ' +
              JSON.stringify(s.skinState));
          }
          if (s.skinState.animationApiMatchesRecord !== true) {
            fail(c.name + ': observed animation API does not match the skin record animationApi');
          }
          if (!c.webgpu) {
            // BOTH channels must satisfy the full native draw record shape
            // (the old baseline intentionally fails missing shadow later).
            if (!skinDrawMatches(s.ior.skinColor, skinTx, wantOp, wantCut)) {
              fail(c.name + ': skinned color draw record mismatch: ' +
                JSON.stringify(s.ior.skinColor));
            }
            if (!skinDrawMatches(s.ior.skinShadow, skinTx, wantOp, wantCut)) {
              fail(c.name + ': skinned shadow draw record mismatch: ' +
                JSON.stringify(s.ior.skinShadow));
            }
          } else if (!(Number(s.frameSeq || 0) > 0)) {
            fail(c.name + ': expected actual WebGPU frame presentation (frameSeq>0)');
          }
        }
      }

      if (s.mounted !== 'true') fail(c.name + ': data-gosx-scene3d-mounted not true');
      if (s.ior.obsErrors.length) fail(c.name + ': GL observation errors: ' + s.ior.obsErrors.join('; '));
      if (s.wgpu.obsErrors.length) fail(c.name + ': WebGPU observation errors: ' + s.wgpu.obsErrors.join('; '));
      if (c.requiredTex) {
        // Every texture asset used by a texture case must actually have been
        // served by this probe origin for this case.
        rec.textureServed = c.requiredTex.map((u) => ({ url: u, count: texServed[u] || 0 }));
        c.requiredTex.forEach((u) => {
          if (!(texServed[u] > 0)) fail(c.name + ': required texture asset not served: ' + u);
        });
      }
      if (c.instancedShadow) {
        rec.instancedShadow = true;
        // Positive scene-state instance count: guards against accidentally
        // testing only the static receiver geometry (READ.instances is the
        // actual instancedMeshes state record count).
        if (!(s.instances > 0)) {
          fail(c.name + ': expected positive instancedMeshes scene-state count, got ' + s.instances);
        }
        if (!c.webgpu) {
          rec.instShadowDraws = s.ior.instShadowDraws;
          rec.instShadowInstances = s.ior.instShadowInstances;
          if (c.expectInstShadowDraws === false) {
            if (s.ior.instShadowDraws !== 0) {
              fail(c.name + ': expected strict 0 instanced shadow-depth draws, got ' +
                s.ior.instShadowDraws);
            }
          } else if (!(s.ior.instShadowDraws > 0)) {
            fail(c.name + ': no instanced shadow-depth draws observed (u_lightViewProjection ' +
              '+ a_instanceMatrix0 program)');
          } else if (!(s.ior.instShadowInstances > 0)) {
            fail(c.name + ': instanced shadow-depth draws reported zero instances');
          }
        }
      }

      // Typed instanced-lifecycle static cases: the actual scene-state
      // instance count and batch count must match the authored fixture
      // contract exactly (read-only; never relaxed for missing evidence).
      if (c.typedInstancedScene) {
        rec.instancedLifecycleScene = c.instancedLifecycleScene;
        rec.expectedInstancedCount = c.expectedInstancedCount;
        const instRead = await evalSend(send, INSTLIFE_STATE_READ);
        if (!instRead) {
          fail(c.name + ': instanced scene-state read failed or malformed');
        } else {
          rec.observedInstanceCount = instRead.instances;
          rec.observedBatches = instRead.batches;
          if (instRead.instances !== c.expectedInstancedCount) {
            fail(c.name + ': expected ' + c.expectedInstancedCount +
              ' instanced scene-state instances, got ' + instRead.instances);
          }
          if (c.expectedInstancedCount > 0 && instRead.batches !== 1) {
            fail(c.name + ': expected exactly 1 instanced batch, got ' + instRead.batches);
          }
          if (c.expectedInstancedCount === 0 && instRead.batches !== 0) {
            fail(c.name + ': expected 0 instanced batches, got ' + instRead.batches);
          }
          if (!c.webgpu) {
            rec.initialInstShadowDraws = s.ior.instShadowDraws;
            rec.initialInstShadowInstances = s.ior.instShadowInstances;
            if (c.expectedInstancedCount === 0 && s.ior.instShadowDraws !== 0) {
              fail(c.name + ': expected strictly 0 instanced shadow draws for' +
                ' the static empty/reference GL scene, got ' +
                s.ior.instShadowDraws);
            }
            if (c.expectedInstancedCount > 0) {
              if (!(s.ior.instShadowDraws > 0)) {
                fail(c.name + ': expected instanced shadow draws > 0 for the' +
                  ' static GL scene with ' + c.expectedInstancedCount +
                  ' instance(s), got ' + s.ior.instShadowDraws);
              }
              if (s.ior.instShadowInstances !== c.expectedInstancedCount) {
                fail(c.name + ': static GL instanced shadow-depth last' +
                  ' instanceCount ' + s.ior.instShadowInstances +
                  ' != expected ' + c.expectedInstancedCount);
              }
            }
          }
        }
      }

      if (iblExpected || iblDisabled) {
        // Record the IBL state from the SELECTED backend only: a GL boolean
        // or the WebGPU environment-word object. iblAssetsServed snapshots
        // the served IBL asset counter at the final settled read.
        rec.iblState = c.webgpu
          ? (s.wgpu.envWords ? { hasIBL: s.wgpu.envWords.hasIBL,
              mips: s.wgpu.envWords.mips, hasEnvMap: s.wgpu.envWords.hasEnvMap } : null)
          : s.ior.lastDrawHasIBL;
        rec.iblAssetsServed = {
          radiance: iblAssetCount.radiance,
          irradiance: iblAssetCount.irradiance,
          brdfLUT: iblAssetCount.brdfLUT,
        };
      }
      if (iblExpected) {
        // Positive: selected backend active AND positive served asset count
        // at the final settled read. No cross-backend OR.
        if (c.webgpu) {
          if (!rec.iblState || rec.iblState.hasIBL !== 1 ||
              rec.iblState.mips !== 2 || rec.iblState.hasEnvMap !== 0) {
            fail(c.name + ': expected IBL enabled on WebGPU, got ' + JSON.stringify(rec.iblState));
          }
          if (s.renderer !== 'webgpu' || s.fallback === 'true') {
            fail(c.name + ': expected active WebGPU backend at settled read');
          }
        } else {
          if (rec.iblState !== true) {
            fail(c.name + ': expected lastDrawHasIBL true, got ' + JSON.stringify(rec.iblState));
          }
          if (!s.ior.gl) fail(c.name + ': expected active GL backend at settled read');
        }
        if (!(rec.iblAssetsServed.radiance > 0) ||
            !(rec.iblAssetsServed.irradiance > 0) ||
            !(rec.iblAssetsServed.brdfLUT > 0)) {
          fail(c.name + ': expected positive served IBL asset counts, got ' +
            JSON.stringify(rec.iblAssetsServed));
        }
      } else if (iblDisabled) {
        // Negative: IBL must be EXPLICITLY disabled — missing observations
        // (null / absent env words) do not pass.
        if (c.webgpu) {
          if (!rec.iblState || rec.iblState.hasIBL !== 0 ||
              rec.iblState.mips !== 0 || rec.iblState.hasEnvMap !== 0) {
            fail(c.name + ': expected IBL explicitly disabled on WebGPU, got ' + JSON.stringify(rec.iblState));
          }
        } else if (rec.iblState !== false) {
          fail(c.name + ': expected lastDrawHasIBL explicitly false, got ' + JSON.stringify(rec.iblState));
        }
        if (rec.iblAssetsServed.radiance !== 0 ||
            rec.iblAssetsServed.irradiance !== 0 ||
            rec.iblAssetsServed.brdfLUT !== 0) {
          fail(c.name + ': expected zero served IBL asset counts, got ' +
            JSON.stringify(rec.iblAssetsServed));
        }
      }

      // Per-case expected uniform values: scalar f0 applies to all channels;
      // authored RGB triples and optional f90 override per case. Explicit zeros
      // must NOT be defaulted away (hence the typeof/Array checks, no ||).
      const expF0 = Array.isArray(c.f0) ? c.f0 : [c.f0, c.f0, c.f0];
      const expF90 = typeof c.f90 === 'number' ? c.f90 : 1;

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
          if (!(s.wgpu.uploads > 0)) fail(c.name + ': no 208-byte material uploads observed');
          let hit = null;
          (s.wgpu.dumps || []).forEach((d) => {
            if (hit || !Array.isArray(d.f0) || d.f0.length !== 3) return;
            for (var ci = 0; ci < 3; ci += 1) {
              if (!(typeof d.f0[ci] === 'number' && Number.isFinite(d.f0[ci]) &&
                    Math.abs(d.f0[ci] - expF0[ci]) < 1e-4)) return;
            }
            if (!(typeof d.f90 === 'number' && Number.isFinite(d.f90) &&
                  Math.abs(d.f90 - expF90) < 1e-4)) return;
            hit = { f0: [d.f0[0], d.f0[1], d.f0[2]], f90: d.f90 };
          });
          rec.f0InUpload = Boolean(hit);
          if (hit) { rec.uploadF0 = hit.f0; rec.uploadF90 = hit.f90; }
          if (c.expectedOpacity != null || c.expectedAlphaCutoff != null ||
              c.albedoTex || typeof c.expectedUnlit === 'boolean') {
            // Opacity (float index 6) and the alpha cutoff (float index 42,
            // byte 168) must be present with the expected RGB F0 and F90 in
            // the SAME 208-byte upload, never sighted independently.
            let ohit = null;
            (s.wgpu.dumps || []).forEach((d) => {
              if (ohit || !Array.isArray(d.f0) || d.f0.length !== 3) return;
              for (var ci = 0; ci < 3; ci += 1) {
                if (!(typeof d.f0[ci] === 'number' && Number.isFinite(d.f0[ci]) &&
                      Math.abs(d.f0[ci] - expF0[ci]) < 1e-4)) return;
              }
              if (!(typeof d.f90 === 'number' && Number.isFinite(d.f90) &&
                    Math.abs(d.f90 - expF90) < 1e-4)) return;
              if (c.expectedOpacity != null &&
                  !(typeof d.opacity === 'number' && Number.isFinite(d.opacity) &&
                    Math.abs(d.opacity - c.expectedOpacity) < 1e-4)) return;
              if (c.expectedAlphaCutoff != null &&
                  !(typeof d.alphaCutoff === 'number' && Number.isFinite(d.alphaCutoff) &&
                    Math.abs(d.alphaCutoff - c.expectedAlphaCutoff) < 1e-4)) return;
              // u_unlit upload flag must match the expected boolean exactly
              // (1 for true, 0 for false) whenever the case carries one.
              if (typeof c.expectedUnlit === 'boolean' &&
                  d.unlit !== (c.expectedUnlit ? 1 : 0)) return;
              if (c.albedoTex && d.hasAlbedoMap !== 1) return;
              ohit = d;
            });
            if (typeof c.expectedUnlit === 'boolean') {
              rec.unlitInUpload = Boolean(ohit);
              if (ohit) rec.uploadUnlit = ohit.unlit;
            }
            if (c.expectedOpacity != null) rec.opacityInUpload = Boolean(ohit);
            if (c.expectedAlphaCutoff != null) {
              rec.alphaCutoffInUpload = Boolean(ohit);
              if (ohit) rec.uploadAlphaCutoff = ohit.alphaCutoff;
            }
            if (c.albedoTex) rec.albedoMapFlag = ohit ? ohit.hasAlbedoMap : 0;
            if (!ohit) {
              fail(c.name + ': expected' +
                (c.expectedOpacity != null ? ' opacity ' + c.expectedOpacity : '') +
                (c.expectedAlphaCutoff != null ? ' cutoff ' + c.expectedAlphaCutoff : '') +
                (typeof c.expectedUnlit === 'boolean' ? ' unlit ' + (c.expectedUnlit ? 1 : 0) : '') +
                (c.albedoTex ? ' + hasAlbedoMap===1' : '') +
                ' with F0 [' + expF0.join(',') + '] + F90 ' + expF90 +
                ' not found together in any 208-byte upload (floats[6]/floats[42]/floats[44..47])');
            }
          }
          if (!hit) {
            fail(c.name + ': expected F0 [' + expF0.join(',') + '] + F90 ' + expF90 +
              ' not found at float indices 44..47 of any 208-byte upload');
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
          fail(c.name + ': no production PBR draws with u_specularF0/u_specularF90 observed (draws=' + s.ior.draws + ')');
        }
        var f0v = s.ior.lastDrawF0;
        if (!Array.isArray(f0v) || f0v.length !== 3) {
          fail(c.name + ': u_specularF0 not observed as a vec3 at draw');
        } else {
          for (var ch = 0; ch < 3; ch += 1) {
            assertClose(f0v[ch], expF0[ch], c.name + ' u_specularF0[' + ch + '] at draw');
          }
        }
        assertClose(s.ior.lastDrawF90, expF90, c.name + ' u_specularF90 at draw');
        if (c.shadowBudget) {
          rec.shadowBudget = c.shadowBudget;
          rec.nativeCap = s.ior.nativeCap;
          rec.forcedCap = s.ior.forcedCap;
          if (typeof s.ior.nativeCap !== 'number' || s.ior.nativeCap <= 0) {
            fail(c.name + ': true native MAX_TEXTURE_IMAGE_UNITS was not observed');
          }
          if (rec.forcedCap !== c.shadowBudget) {
            fail(c.name + ': effective forced cap ' + rec.forcedCap + ' != requested shadowBudget ' + c.shadowBudget);
          }
          if (s.ior.nativeCap < c.shadowBudget) {
            fail(c.name + ': forced cap ' + c.shadowBudget + ' requested but native cap is only ' + s.ior.nativeCap +
              '; this case cannot be validated on this hardware/driver');
          }
          const sh = s.ior.shadow;
          if (!sh) {
            fail(c.name + ': no shadow snapshot captured at a real PBR draw');
          } else {
            rec.shadow = sh;
            if (sh.error) fail(c.name + ': shadow snapshot error: ' + sh.error);
            // The first draw may use the depth program; require the native
            // LINK_STATUS of the CURRENT PBR program captured in the snapshot.
            if (sh.linkStatus !== true) {
              fail(c.name + ': current PBR program native LINK_STATUS not true at snapshot');
            }
            if (!(s.ior.pbrDraws > 0)) {
              fail(c.name + ': no PBR draws at shadow snapshot');
            }
            const expCasc = (c.shadowBudget === 16) ? [4, 1] : [4, 4];
            if (JSON.stringify(sh.cascades) !== JSON.stringify(expCasc)) {
              fail(c.name + ': expected shadow cascades [' + expCasc.join(',') +
                '], got [' + (sh.cascades || []).join(',') + ']');
            }
            const hasStr = JSON.stringify(sh.has || []);
            if (hasStr !== JSON.stringify([1, 1]) && hasStr !== JSON.stringify([true, true])) {
              fail(c.name + ': expected u_hasShadow0/1 both enabled, got [' + (sh.has || []).join(',') + ']');
            }
            if (JSON.stringify(sh.lightIndices || []) !== JSON.stringify([0, 1])) {
              fail(c.name + ': expected shadow light indices [0,1], got [' +
                (sh.lightIndices || []).join(',') + ']');
            }
            const expUnits = (c.shadowBudget === 16) ? 5 : 8;
            const units = sh.units || [];
            const uniq = Array.from(new Set(units));
            if (units.length !== expUnits || uniq.length !== expUnits) {
              fail(c.name + ': expected exactly ' + expUnits + ' distinct in-range active sampler units, got ' +
                JSON.stringify(units));
            }
            if (!units.every((u) => Number.isInteger(u) && u >= 0 &&
                u < Math.min(s.ior.nativeCap, c.shadowBudget))) {
              fail(c.name + ': sampler units must be integers below both caps, got ' + JSON.stringify(units));
            }
            const texs = sh.textures || [];
            if (texs.length !== expUnits || texs.some((t) => t == null) ||
                new Set(texs).size !== expUnits) {
              fail(c.name + ': expected ' + expUnits + ' distinct non-null bound textures, got ' +
                JSON.stringify(texs));
            }
            const depthIds = sh.depthAttachmentIds || [];
            if (sh.depthAttachmentCount !== expUnits || depthIds.length !== expUnits) {
              fail(c.name + ': expected ' + expUnits + ' distinct depth attachments, got ' +
                sh.depthAttachmentCount);
            }
            if (!texs.every((t) => depthIds.indexOf(t) >= 0)) {
              fail(c.name + ': not every selected sampler texture was observed as a DEPTH_ATTACHMENT');
            }
          }
        }
        if (c.expectedOpacity != null) {
          if (typeof s.ior.lastDrawOpacity !== 'number' || !Number.isFinite(s.ior.lastDrawOpacity)) {
            fail(c.name + ': u_opacity not observed at the F0/F90-qualified PBR draw');
          }
          rec.uniformOpacity = s.ior.lastDrawOpacity;
          assertClose(s.ior.lastDrawOpacity, c.expectedOpacity, c.name + ' u_opacity at draw');
        }
        if (c.expectedAlphaCutoff !== undefined) {
          if (typeof s.ior.lastDrawAlphaCutoff !== 'number' ||
              !Number.isFinite(s.ior.lastDrawAlphaCutoff)) {
            fail(c.name + ': u_alphaCutoff not observed at the F0/F90-qualified PBR draw');
          }
          rec.uniformAlphaCutoff = s.ior.lastDrawAlphaCutoff;
          assertClose(s.ior.lastDrawAlphaCutoff, c.expectedAlphaCutoff, c.name + ' u_alphaCutoff at draw');
        }
        if (typeof c.expectedUnlit === 'boolean') {
          // u_unlit must be observed as a strict boolean at the SAME draw;
          // null/missing never defaults and never passes.
          if (typeof s.ior.lastDrawUnlit !== 'boolean') {
            fail(c.name + ': u_unlit not observed as boolean (got ' +
              JSON.stringify(s.ior.lastDrawUnlit) + ') at the F0/F90-qualified PBR draw');
            rec.uniformUnlit = null;
          } else {
            rec.uniformUnlit = s.ior.lastDrawUnlit;
            if (s.ior.lastDrawUnlit !== c.expectedUnlit) {
              fail(c.name + ': u_unlit actual ' + s.ior.lastDrawUnlit +
                ' != expected ' + c.expectedUnlit + ' at the F0/F90-qualified PBR draw');
            }
          }
        }
        if (c.albedoTex && !c.webgpu) {
          if (s.ior.lastDrawHasAlbedoMap !== true) {
            fail(c.name + ': u_hasAlbedoMap not observed loaded (true/1) at the F0/F90-qualified PBR draw');
            rec.albedoMapFlag = (s.ior.lastDrawHasAlbedoMap === false) ? 0 : null;
          } else {
            rec.albedoMapFlag = 1;
          }
        }
      }

      // Native per-draw depth/blend routing gates: only cases carrying
      // authored expectedDepthWrite/expectedBlend (the alpha-mask routing
      // group) are gated. WebGL reads the real DEPTH_WRITEMASK and
      // isEnabled(BLEND) at the F0/F90-qualified PBR draw; WebGPU reads the
      // actually drawn PBR-qualified pipeline's descriptor snapshot (never
      // creation alone, never a requested input, no raw GPU handles). A
      // missing or failed observation fails; it never defaults.
      if (c.expectedDepthWrite !== undefined || c.expectedBlend !== undefined) {
        if (c.webgpu) {
          const wd = (s.wgpu && s.wgpu.wgdraws) ? s.wgpu.wgdraws : null;
          if (!wd || !(wd.observed > 0)) {
            fail(c.name + ': no drawn PBR-qualified WebGPU pipeline observed');
          } else {
            rec.drawnPipeline = { depthWrite: wd.lastDepthWrite, blend: wd.lastBlend };
            if (wd.lastDepthWrite !== c.expectedDepthWrite) {
              fail(c.name + ': drawn pipeline depthWriteEnabled=' +
                wd.lastDepthWrite + ' != expected ' + c.expectedDepthWrite);
            }
            if (wd.lastBlend !== c.expectedBlend) {
              fail(c.name + ': drawn pipeline blend=' +
                wd.lastBlend + ' != expected ' + c.expectedBlend);
            }
          }
        } else {
          if (s.ior.lastDrawDepthWrite !== c.expectedDepthWrite) {
            fail(c.name + ': DEPTH_WRITEMASK=' + s.ior.lastDrawDepthWrite +
              ' != expected ' + c.expectedDepthWrite + ' at the F0/F90-qualified PBR draw');
          }
          if (s.ior.lastDrawBlend !== c.expectedBlend) {
            fail(c.name + ': isEnabled(BLEND)=' + s.ior.lastDrawBlend +
              ' != expected ' + c.expectedBlend + ' at the F0/F90-qualified PBR draw');
          }
          rec.drawnDepthWrite = s.ior.lastDrawDepthWrite;
          rec.drawnBlend = s.ior.lastDrawBlend;
        }
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
      rec.centerRGB = Array.isArray(m.center) ? m.center : null;
      // Only the explicitly tagged new expectedEmpty:true MASK cases assert
      // an exact background image (every pixel classified background, zero
      // foreground); readiness, the real PBR draw and the uniform gates above
      // are never relaxed for them. All older cases keep the unchanged
      // foreground-vs-background proof (including IOR 0 / F0 1): a pure
      // background image (fg=0) fails that assertion.
      if (c.expectedEmpty === true) {
        if (m.bgPixels !== m.w * m.h || m.fgPixels !== 0) {
          fail(c.name + ': expectedEmpty case must leave only background ' +
            '(bg=' + m.bgPixels + ' != ' + (m.w * m.h) + ' or fg=' + m.fgPixels + ')');
        }
      } else if (!(m.fgPixels > 0) || !(m.fgFrac >= FG_COVERAGE) || !(m.maxDelta >= FG_THRESHOLD)) {
        fail(c.name + ': no measurable geometry foreground vs measured corner background ' +
          '(fg=' + m.fgPixels + ', frac=' + m.fgFrac.toFixed(4) + ', maxDelta=' + m.maxDelta + ')');
      }
      // WebGL half-float regression probe: a bad texImage2D view fails with a
      // driver-side INVALID_OPERATION (no JS throw) and leaves the IBL black
      // or a white placeholder. The fixed quad covers the screenshot center,
      // so a real numeric three-element center RGB observation is required
      // and the black controls must actually sample black there.
      const centerMustBeBlack = Boolean(c.noIBL) ||
        (Boolean(c.requiresIBL) && c.f90 === 0);
      if (centerMustBeBlack) {
        if (!Array.isArray(m.center) || m.center.length !== 3 ||
            !m.center.every((v) => typeof v === 'number' && Number.isFinite(v))) {
          fail(c.name + ': missing numeric center RGB observation (' +
            JSON.stringify(m.center) + ')');
        } else if (m.center.some((v) => v > 1)) {
          fail(c.name + ': center RGB ' + JSON.stringify(m.center) +
            ' exceeds black; WebGL half-float IBL upload likely broken');
        }
      }
      saveArtifact(c.name + '.png', cap.base64);
      shots[c.name] = cap;

      // Instanced multi-instance lifecycle: apply the six typed fixture
      // transitions on THIS same mount via the public command seam, with
      // per-step native evidence, screenshots and ROI comparisons.
      if (c.lifecycle && Array.isArray(c.lifecycleTransitions)) {
        await runInstancedLifecycleSteps(send, c, rec, cap, shots);
      }
      // Skinned LIVE 2-step public hub event run on this same mount.
      if (c.skinnedLive) {
        await runSkinnedShadowLiveSteps(send, c, rec, cap, shots, evidence);
      }

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
              Array.isArray(s2.ior.lastDrawF0) && s2.ior.lastDrawF0.length === 3 &&
              Math.abs(s2.ior.lastDrawF0[0] - F0(2.42)) < 2e-4 &&
              Math.abs(s2.ior.lastDrawF0[1] - F0(2.42)) < 2e-4 &&
              Math.abs(s2.ior.lastDrawF0[2] - F0(2.42)) < 2e-4 &&
              Math.abs((s2.ior.lastDrawF90 || 0) - 1) < 2e-4) { advanced = true; break; }
          await sleep(100);
        }
        if (!advanced) {
          fail('css-var: revision advance + new u_specularF0=' + F0(2.42) +
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
          rec.cssAfter = { rev: s2 && s2.rev, uniformF0: s2 && s2.ior.lastDrawF0,
            uniformF90: s2 && s2.ior.lastDrawF90, pixelDiff: d };
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
        if (c.skinnedGlb && !c.webgpu) {
          let sPost = null;
          try { sPost = await evalSend(send, READ); } catch (e) { sPost = null; }
          const preId = (rec.skinEvidence && rec.skinEvidence.shadow &&
            rec.skinEvidence.shadow.lastProgramId != null) ?
            rec.skinEvidence.shadow.lastProgramId : null;
          rec.skinShadowProgramDeleted = !!(preId !== null && sPost &&
            sPost.ior && Array.isArray(sPost.ior.deletedProgramIds) &&
            sPost.ior.deletedProgramIds.indexOf(preId) !== -1);
          if (preId === null) {
            fail(c.name + ': no skinned shadow program id observed before dispose');
          } else if (!rec.skinShadowProgramDeleted) {
            fail(c.name + ': skinned shadow program ' + preId +
              ' not present in deletedProgramIds after dispose');
          }
        }
      }
    }
  }

  // ---- Cross-case image comparisons (real rendered output, fixed camera) ----
  for (const c of RUN_CASES) {
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
          const d = await diffShots(send, A.base64, B.base64, c.shadowROI);
          rec.sameAs = { target: c.same, roi: c.shadowROI || undefined, diff: d };
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
          const d = await diffShots(send, A.base64, B.base64, c.shadowROI);
          rec.differsFrom = { target: c.differs, roi: c.shadowROI || undefined, diff: d };
          // Distinct IOR: meaningful change (channel > 2) AND maxDelta >= 3.
          if (!d || !d.dimsMatch || !(d.meanChanged >= (c.minChanged || 1)) || !(d.maxDelta >= 3)) {
            fail(c.name + ': distinct IOR must change visible pixels vs ' + c.differs +
              ' (meanChanged=' + (d && d.meanChanged) + ', maxDelta=' + (d && d.maxDelta) + ')');
          }
        }
      }
      if (c.sameFull != null) {
        // Full-image comparison: same backend, COMPLETE screenshots via
        // diffShots WITHOUT ROI; exact changed pixels AND bytes both 0.
        const B = shots[c.sameFull];
        const recB = evidence.find((r) => r.name === c.sameFull);
        if (!B || !recB) { fail(c.name + ': missing capture for full-image comparison vs ' + c.sameFull); }
        else if (rec.renderer !== recB.renderer) {
          rec.fullImageComparison = { target: c.sameFull, skipped: 'renderer mismatch (' + rec.renderer + ' vs ' + recB.renderer + ')' };
          fail(c.name + ': renderer mismatch vs ' + c.sameFull + ' (' + rec.renderer + ' vs ' + recB.renderer + ')');
        } else {
          const d = await diffShots(send, A.base64, B.base64);
          rec.fullImageComparison = { target: c.sameFull, diff: d };
          if (!d || !d.dimsMatch || d.exactPixels !== 0 || d.exactBytes !== 0) {
            fail(c.name + ': full image must be byte-identical to ' + c.sameFull +
              ' (exactChangedPixels=' + (d && d.exactPixels) + ', exactChangedBytes=' + (d && d.exactBytes) + ')');
          }
        }
      }
      if (c.nearSame != null) {
        // Fractional texture-intensity comparison: same backend, same
        // dimensions, at most one channel quantization step of difference.
        const B = shots[c.nearSame];
        const recB = evidence.find((r) => r.name === c.nearSame);
        if (!B || !recB) { fail(c.name + ': missing capture for near comparison vs ' + c.nearSame); }
        else if (rec.renderer !== recB.renderer) {
          rec.nearSameAs = { target: c.nearSame, skipped: 'renderer mismatch (' + rec.renderer + ' vs ' + recB.renderer + ')' };
          fail(c.name + ': renderer mismatch vs ' + c.nearSame + ' (' + rec.renderer + ' vs ' + recB.renderer + ')');
        } else {
          const d = await diffShots(send, A.base64, B.base64);
          rec.nearSameAs = { target: c.nearSame, diff: d };
          if (!d || !d.dimsMatch || !(d.maxDelta <= 1)) {
            fail(c.name + ': fractional intensity must match ' + c.nearSame +
              ' within 1 channel quantization step (maxDelta=' + (d && d.maxDelta) + ')');
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
      uniformF0: r.uniformF0, uniformF90: r.uniformF90, f0InUpload: r.f0InUpload, wgpuUploads: r.wgpuUploads,
      uniformOpacity: r.uniformOpacity, opacityInUpload: r.opacityInUpload,
      expectedOpacity: r.expectedOpacity,
      expectedAlphaCutoff: r.expectedAlphaCutoff,
      uniformAlphaCutoff: r.uniformAlphaCutoff,
      alphaCutoffInUpload: r.alphaCutoffInUpload, uploadAlphaCutoff: r.uploadAlphaCutoff,
      expectedDepthWrite: r.expectedDepthWrite, expectedBlend: r.expectedBlend,
      drawnDepthWrite: r.drawnDepthWrite, drawnBlend: r.drawnBlend,
      drawnPipeline: r.drawnPipeline || undefined,
      expectedUnlit: r.expectedUnlit,
      uniformUnlit: r.uniformUnlit,
      unlitInUpload: r.unlitInUpload, uploadUnlit: r.uploadUnlit,
      expectedEmpty: r.expectedEmpty,
      specIntensityMapFlag: r.specIntensityMapFlag, specColorMapFlag: r.specColorMapFlag,
      albedoMapFlag: r.albedoMapFlag,
      textureServed: r.textureServed,
      iblState: r.iblState, iblAssetsServed: r.iblAssetsServed,
      uploadF0: r.uploadF0, uploadF90: r.uploadF90,
      objects: r.objects, fgPixels: r.litPixels, fgFrac: r.fgFrac, cornerBG: r.meanRGB, centerRGB: r.centerRGB,
      cssAfter: r.cssAfter || undefined, sameAs: r.sameAs || undefined,
      differsFrom: r.differsFrom || undefined, nearSameAs: r.nearSameAs || undefined,
      fullImageComparison: r.fullImageComparison || undefined,
      disposeRemovedState: r.disposeRemovedState,
      instancedLifecycleScene: r.instancedLifecycleScene,
      expectedInstancedCount: r.expectedInstancedCount,
      observedInstanceCount: r.observedInstanceCount,
      observedBatches: r.observedBatches,
      lifecycleSteps: r.lifecycleSteps || undefined,
      skinnedLiveSteps: r.skinnedLiveSteps || undefined,
      shadowBudget: r.shadowBudget || undefined, shadow: r.shadow || undefined,
      nativeCap: r.nativeCap !== undefined ? r.nativeCap : undefined,
      instShadowDraws: r.instShadowDraws, instShadowInstances: r.instShadowInstances,
      initialInstShadowDraws: r.initialInstShadowDraws,
      initialInstShadowInstances: r.initialInstShadowInstances,
      instancedShadow: r.instancedShadow,
      forcedCap: r.forcedCap !== undefined ? r.forcedCap : undefined,
      skinEvidence: r.skinEvidence || undefined,
      skinShadowProgramDeleted: r.skinShadowProgramDeleted,
    })),
    caseGroup: CASE_GROUP,
    registeredCaseCount: CASES.length,
    selectedCaseCount: RUN_CASES.length,
    disposal: ran.length > 0 && ran.every((r) => r.disposeRemovedState === true),
    artifacts: ART || undefined,
    errors, warnings,
    note: 'Real Chrome + real WebGL2/WebGPU PBR with the actual built bootstrap.js, real GLB ' +
      'loading and native draws. u_specularF0/u_specularF90 read from real uniform state at ' +
      'production draw calls (getUniformLocation tracking + CURRENT_PROGRAM + getUniform, ' +
      'instanced forms) and at float indices 44..47 of 208-byte WebGPU material uploads; ' +
      'the intensity-texture loaded state is observed per backend as the real ' +
      'u_hasSpecularIntensityMap draw-time uniform (WebGL, missing = null) or the byte-164 ' +
      'upload flag (WebGPU), and the color-texture loaded state as the real ' +
      'u_hasSpecularColorMap draw-time uniform (WebGL, missing = null) or the byte-204 ' +
      'upload flag (WebGPU), with combined color+intensity readiness requiring BOTH flags ' +
      'in the same draw/snapshot; ' +
      'the new base-color PNG alpha/unlit cases additionally carry actual loaded-albedo ' +
      'evidence, observed per backend as the real u_hasAlbedoMap draw-time uniform ' +
      '(WebGL, missing = null) or the byte-52 upload flag (WebGPU), required in the same ' +
      'draw/snapshot as opacity, cutoff, F0 and F90, and are distinct from the older ' +
      'factor-only FILL MASK cases, with no matching cutout-shadow, instancing, or ' +
      'wireframe certification made for them; ' +
      'the unlit cases add observed u_unlit evidence per backend as the real draw-time ' +
      'uniform (WebGL, boolean true/false or 1/0, missing = null, compared against the ' +
      'expected boolean, absent = fail) or the byte-48 upload flag (WebGPU, required ' +
      'exactly 1/0 alongside opacity, cutoff, albedo, F0 and F90), including the paired ' +
      'light-invariance case pairs, without claiming full glTF compliance, shadows, ' +
      'vertex colors, or instancing; ' +
      'Older MASK cases on BOTH backends validate factor-only FILL masking against ' +
      'the dedicated per-backend wireframe:false opaque FILL control ' +
      '(alpha-opaque-a1, no authored cutoff; glb-mask-fill-control and ' +
      'wg-mask-fill-control), with expectedEmpty cases asserting strict ' +
      'full-background + zero-foreground screenshots and no alpha-mask ' +
      'texture, cutout-shadow, or wireframe claims made for those older ' +
      'cases only; ' +
      'the new alpha-mask routing cases add native per-draw depth/blend ' +
      'observations gating only their own cases: WebGL reads the real ' +
      'DEPTH_WRITEMASK/isEnabled(BLEND) at the F0/F90-qualified PBR draw and ' +
      'WebGPU observes the actually drawn PBR pipeline descriptor ' +
      '(depthWriteEnabled and color-target blend presence) tracked through ' +
      'setPipeline on render-pass and render-bundle encoders; these certify ' +
      'draw-route state only, not physical GPU depth hardware behavior or ' +
      'full geometry correctness; ' +
      'all wrappers strictly forward and ' +
      'observation errors fail the probe. Pixels come from CDP screenshots clipped to the real ' +
      'canvas rect, decoded with a native Image+2D canvas, with foreground-vs-measured-background ' +
      'observations recorded for every case: foreground controls prove actual visible draw, while ' +
      'expectedEmpty cases are strict background-only, proving zero-foreground full-background ' +
      'output rather than visible draw. Forced 16/32 MAX_TEXTURE_IMAGE_UNITS allocator-cap testing on native ' +
      'WebGL is not physical 16-unit hardware certification. GPU hardware acceleration type is ' +
      'NOT certified (SwiftShader possible).',
  };
  if (ART) {
    // Persist the full report to the artifact path before emitting, so the
    // artifact set is self-contained. A write error fails the probe.
    try { fs.writeFileSync(path.join(ART, 'report.json'), JSON.stringify(out, null, 2)); }
    catch (e) { errors.push('failed to write report to ' + path.join(ART, 'report.json') + ': ' + ((e && e.message) || e)); }
  }
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
