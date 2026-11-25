// SMAA (scene.SMAA, client/runtime/scene3d/smaa*.ts). GPU-free checks: the chunk's
// API and settings, the routing in both renderers, the passes each backend
// encodes, and a CPU model of the three passes on analytic edges against a
// supersampled reference. The CPU model mirrors the WGSL term by term, so it
// proves the algorithm, not the shader; e2e/scene3d_smaa_e2e_test.go runs the
// shaders on a GPU.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import test from 'node:test';

const ts = createRequire(new URL('../runtime/package.json', import.meta.url))('typescript');
const read = path => fs.readFileSync(new URL(path, import.meta.url), 'utf8');

function load(files = ['smaa', 'smaa-wgpu', 'smaa-gl']) {
  const window = { console };
  const context = vm.createContext({ window, console, Float32Array, Map, Object, Math, Number,
    GPUBufferUsage: { UNIFORM: 64, COPY_DST: 8 }, GPUShaderStage: { FRAGMENT: 2 }, GPUTextureUsage: { TEXTURE_BINDING: 4, RENDER_ATTACHMENT: 16 } });
  context.globalThis = context;
  for (const name of files) {
    vm.runInContext(ts.transpileModule(read('../runtime/scene3d/' + name + '.ts'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  }
  return window.__gosx_scene3d_smaa_api;
}

test('settings take the Go defaults and clamp to the documented ranges', () => {
  const api = load(['smaa']);
  assert.deepEqual({ ...api.settings({ kind: 'smaa' }) }, { threshold: 0.1, steps: 16, corner: 0.25 });
  assert.deepEqual({ ...api.settings({ threshold: 0.9, maxSearchSteps: 99, cornerRounding: 3 }) }, { threshold: 0.5, steps: 32, corner: 1 });
  assert.deepEqual({ ...api.settings({ threshold: 0.001, maxSearchSteps: 1, cornerRounding: -1 }) }, { threshold: 0.02, steps: 4, corner: 0.25 });
});

test('both renderers send smaa to the SMAA chunk through the depth-effect hook and present without it', () => {
  for (const [file, fn] of [['webgpu.ts', 'wgpuDepthFxPass'], ['webgl.ts', 'sceneWebGLDepthFxPass']]) {
    const source = read('../runtime/scene3d/' + file);
    const line = source.split('\n').find(l => l.includes('function ' + fn + '('));
    assert.match(line, /effect\.kind === "smaa" \? window\.__gosx_scene3d_smaa_api : window\.__gosx_scene3d_depthfx_api/, file);
    assert.match(line, /return api \? .* : rest\[[01]\]/, file + ': without the chunk the chain continues from its input');
    assert.match(source, /window\.__gosx_scene3d_smaa_api\?\.dispose\(state\)/, file + ': the post processor releases SMAA targets');
  }
  assert.match(read('../runtime/scene3d/mount-webgl.ts'), /ensureSceneGatedFeatureLoaded\("smaa", "gosxScene3dSmaaUrl", ""\)\.catch/, 'the mount fetches the chunk with the renderer, and only when advertised');
});

test('WebGPU encodes edges, weights and blend into RG8, RGBA8 and the chain target, and reuses its targets', () => {
  const api = load();
  const log = { textures: [], passes: [], writes: 0, groups: 0, pipelines: [] };
  const device = {
    createBindGroupLayout: d => ({ label: d.label }), createPipelineLayout: d => d, createShaderModule: d => d,
    createBuffer: d => ({ size: d.size, destroy() {} }),
    createRenderPipeline: d => { log.pipelines.push(d.fragment.targets[0].format); return { label: d.label, code: d.fragment.module.code }; },
    createBindGroup: d => { log.groups++; return d; },
    createTexture: d => { const t = { format: d.format, size: d.size, destroyed: false, createView: () => ({ texture: t }), destroy() { t.destroyed = true; } }; log.textures.push(t); return t; },
    queue: { writeBuffer: () => { log.writes++; } },
  };
  const encoder = { beginRenderPass: d => ({ setPipeline: p => log.passes.push({ label: p.label, view: d.colorAttachments[0].view }), setBindGroup() {}, draw: n => assert.equal(n, 3), end() {} }) };
  const state = { device };
  const input = { name: 'chain' }, output = { name: 'next' };
  for (let frame = 0; frame < 3; frame++) {
    assert.equal(api.webgpuRun(state, { kind: 'smaa' }, 4, encoder, input, output, 'rgba16float', null, 1920, 1080, null, null), output);
  }
  assert.deepEqual(log.textures.map(t => [t.format, ...t.size]), [['rg8unorm', 1920, 1080], ['rgba8unorm', 1920, 1080]]);
  assert.deepEqual(log.passes.slice(0, 3).map(p => p.label), ['gosx-smaa-edges', 'gosx-smaa-weights', 'gosx-smaa-blend']);
  assert.equal(log.passes[2].view, output);
  assert.equal(log.writes, 1, 'parameters upload once while they stay the same');
  assert.equal(log.groups, 3, 'bind groups are reused across frames');
  assert.deepEqual(log.pipelines, ['rg8unorm', 'rgba8unorm', 'rgba16float']);
  api.webgpuRun(state, { kind: 'smaa' }, 4, encoder, input, output, 'bgra8unorm', null, 1280, 720, null, null);
  assert.ok(log.textures.slice(0, 2).every(t => t.destroyed), 'a resize releases the old targets');
  assert.equal(log.pipelines.at(-1), 'bgra8unorm', 'the blend can present straight to the canvas');
  api.dispose(state);
  assert.ok(log.textures.every(t => t.destroyed));
  assert.deepEqual(Object.keys(state), ['device']);
});

test('shader sources: every pass reads whole pixels and the two backends share one rule set', () => {
  const api = load();
  for (const [name, code] of Object.entries({ ...api.wgsl })) {
    if (name === 'vertex') continue;
    assert.doesNotMatch(code, /textureSample\(/, name + ': whole-pixel loads only');
    assert.match(code, /@fragment fn fragmentMain\(@builtin\(position\) pos: vec4f\)/, name);
  }
  for (const code of [api.wgsl.edges, api.glsl.edges]) assert.match(code, /vec3f?\(0\.2126, 0\.7152, 0\.0722\)/);
  for (const code of [api.wgsl.weights, api.glsl.weights]) {
    assert.match(code, /0\.5 \* \(d2 \+ 1\.0 - d1\)/, 'the line meets the edge at the middle of the segment');
    assert.match(code, /\(b - a\) \* \(h0 \+ \(h1 - h0\) \* \(0\.5 \* \(a \+ b\) - x0\) \/ \(x1 - x0\)\)/, 'closed-form area');
  }
  for (const code of [api.wgsl.blend, api.glsl.blend]) assert.match(code, /vec3f?\(2\.2\)/, 'blend in linear light');
});

// ---- CPU model of the three passes (rows top to bottom, as in the WGSL) ----

function smaaModel(img, W, H, { threshold = 0.1, steps = 16, corner = 0.25 } = {}) {
  const clampi = (v, lo, hi) => Math.max(lo, Math.min(hi, v));
  const luma = (x, y) => Math.max(0, Math.min(1, img[clampi(y, 0, H - 1) * W + clampi(x, 0, W - 1)]));
  const edges = new Float32Array(W * H * 2);
  for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) {
    const c = luma(x, y), l = luma(x - 1, y), t = luma(x, y - 1);
    const d = [Math.abs(c - l), Math.abs(c - t)];
    const e = d.map(v => (v >= threshold ? 1 : 0));
    if (e[0] + e[1] === 0) continue;
    const m = Math.max(d[0], d[1], Math.abs(c - luma(x + 1, y)), Math.abs(c - luma(x, y + 1)), Math.abs(l - luma(x - 2, y)), Math.abs(t - luma(x, y - 2)));
    edges[(y * W + x) * 2] = e[0] * (m <= 2 * d[0] ? 1 : 0);
    edges[(y * W + x) * 2 + 1] = e[1] * (m <= 2 * d[1] ? 1 : 0);
  }
  const E = (x, y) => (x < 0 || y < 0 || x >= W || y >= H ? [0, 0] : [edges[(y * W + x) * 2], edges[(y * W + x) * 2 + 1]]);
  const endHeight = (cp, cq, kp, kq) => (cp * cq > 0 ? 0 : 0.5 * (cq * (kq > 0 ? corner : 1) - cp * (kp > 0 ? corner : 1)));
  function walk(px, py, s, k) {
    const a = k === 1 ? [1, 0] : [0, 1], q = k === 1 ? [0, -1] : [-1, 0], first = s < 0 ? 0 : 1;
    let d = 0;
    for (;;) {
      const bx = px + a[0] * (s * d + first), by = py + a[1] * (s * d + first);
      const cp = E(bx, by)[1 - k], cq = E(bx + q[0], by + q[1])[1 - k];
      if (cp + cq > 0) return [d, endHeight(cp, cq, E(bx - q[0], by - q[1])[1 - k], E(bx + 2 * q[0], by + 2 * q[1])[1 - k])];
      if (d >= steps || E(px + a[0] * s * (d + 1), py + a[1] * s * (d + 1))[k] === 0) break;
      d++;
    }
    return [d, 0];
  }
  const span = (x0, x1, h0, h1) => {
    const a = Math.max(x0, 0), b = Math.min(x1, 1);
    if (b <= a) return [0, 0];
    const v = (b - a) * (h0 + (h1 - h0) * (0.5 * (a + b) - x0) / (x1 - x0));
    return [Math.max(-v, 0), Math.max(v, 0)];
  };
  const area = (d1, d2, h1, h2) => { const m = 0.5 * (d2 + 1 - d1), p = span(-d1, m, h1, 0), r = span(m, d2 + 1, 0, h2); return [p[0] + r[0], p[1] + r[1]]; };
  const wts = new Float32Array(W * H * 4);
  for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) {
    const e = E(x, y), o = (y * W + x) * 4;
    if (e[1] > 0) { const l = walk(x, y, -1, 1), r = walk(x, y, 1, 1); [wts[o], wts[o + 1]] = area(l[0], r[0], l[1], r[1]); }
    if (e[0] > 0) { const u = walk(x, y, -1, 0), v = walk(x, y, 1, 0); [wts[o + 2], wts[o + 3]] = area(u[0], v[0], u[1], v[1]); }
  }
  const Wt = (x, y, c) => (x >= W || y >= H ? 0 : wts[(y * W + x) * 4 + c]);
  const lin = (x, y) => Math.pow(Math.max(img[clampi(y, 0, H - 1) * W + clampi(x, 0, W - 1)], 0), 2.2);
  const out = new Float32Array(W * H);
  for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) {
    const up = Wt(x, y, 0), left = Wt(x, y, 2), down = Wt(x, y + 1, 1), right = Wt(x + 1, y, 3);
    if (up + left + down + right === 0) { out[y * W + x] = img[y * W + x]; continue; }
    let a = [0, -1], wa = up, wb = down;
    if (Math.max(left, right) > Math.max(up, down)) { a = [-1, 0]; wa = left; wb = right; }
    const m = lin(x, y) * (1 - wa - wb) + lin(x + a[0], y + a[1]) * wa + lin(x - a[0], y - a[1]) * wb;
    out[y * W + x] = Math.pow(m, 1 / 2.2);
  }
  return out;
}

// A white half-plane on black, its edge through the image centre at angle deg
// from the x axis: the aliased image (pixel centres) and the 16 x 16 coverage
// reference, both display-encoded.
function halfPlane(deg, [ox, oy] = [0.37, 0.21], W = 96, H = 96) {
  const t = deg * Math.PI / 180, nx = -Math.sin(t), ny = Math.cos(t), cx = W / 2 + ox, cy = H / 2 + oy;
  const inside = (x, y) => (x - cx) * nx + (y - cy) * ny > 0;
  const aliased = new Float32Array(W * H), reference = new Float32Array(W * H);
  for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) {
    aliased[y * W + x] = inside(x + 0.5, y + 0.5) ? 1 : 0;
    let n = 0;
    for (let j = 0; j < 16; j++) for (let i = 0; i < 16; i++) n += inside(x + (i + 0.5) / 16, y + (j + 0.5) / 16) ? 1 : 0;
    reference[y * W + x] = Math.pow(n / 256, 1 / 2.2);
  }
  return { aliased, reference, W, H };
}

// Mean absolute display error over the pixels the edge crosses, away from the image border.
function edgeError(img, ref, W, H) {
  let sum = 0, n = 0;
  for (let y = 4; y < H - 4; y++) for (let x = 4; x < W - 4; x++) {
    const r = ref[y * W + x];
    if (r <= 0 || r >= 1) continue;
    sum += Math.abs(img[y * W + x] - r); n++;
  }
  return sum / n;
}

test('CPU model: edges at shallow and steep angles land much closer to the supersampled coverage', () => {
  const rows = [];
  for (const deg of [2, 4, 7, 11, 16, 24, 66, 74, 79, 83, 86, 88]) {
    const { aliased, reference, W, H } = halfPlane(deg);
    const none = edgeError(aliased, reference, W, H), smaa = edgeError(smaaModel(aliased, W, H), reference, W, H);
    rows.push(`${deg}deg none ${none.toFixed(3)} smaa ${smaa.toFixed(3)}`);
    assert.ok(smaa <= none * 0.6, `${deg} degrees: SMAA edge error ${smaa.toFixed(3)} against ${none.toFixed(3)} with no AA (${rows.join('; ')})`);
  }
});

// Near 45 degrees every stair is one pixel long, and the result depends on where
// the line crosses the pixel grid: at exactly 45 degrees one of these six offsets
// comes out worse than no AA (0.147 -> 0.231). Diagonal patterns are the named
// follow-up; until then the mean over offsets must still improve.
test('CPU model: near 45 degrees (no diagonal patterns yet) the mean edge error over line offsets still falls', () => {
  const offsets = [[0, 0], [0.1, 0], [0.25, 0], [0.5, 0], [0.37, 0.21], [0.05, 0.3]];
  for (const deg of [30, 38, 45, 52, 60]) {
    let none = 0, smaa = 0;
    for (const offset of offsets) {
      const { aliased, reference, W, H } = halfPlane(deg, offset);
      none += edgeError(aliased, reference, W, H) / offsets.length;
      smaa += edgeError(smaaModel(aliased, W, H), reference, W, H) / offsets.length;
    }
    assert.ok(smaa <= none * 0.75, `${deg} degrees: mean ${smaa.toFixed(3)} against ${none.toFixed(3)}`);
  }
});

test('CPU model: flat areas, pixel-aligned edges and a one-pixel stripe keep their values', () => {
  const W = 64, H = 64;
  const flat = new Float32Array(W * H).fill(0.5);
  assert.deepEqual([...smaaModel(flat, W, H)], [...flat]);
  const aligned = new Float32Array(W * H).map((_, i) => ((i % W) < 32 ? 0.1 : 0.9));
  assert.deepEqual([...smaaModel(aligned, W, H)], [...aligned], 'a straight pixel-aligned edge has no stair to smooth');
  const stripe = new Float32Array(W * H).map((_, i) => (Math.floor(i / W) === 20 ? 1 : 0));
  const out = smaaModel(stripe, W, H);
  for (let x = 4; x < W - 4; x++) assert.equal(out[20 * W + x], 1, 'a one-pixel horizontal line keeps full contrast');
  const noise = new Float32Array(W * H).map((_, i) => 0.5 + 0.03 * Math.sin(i * 12.9898));
  assert.deepEqual([...smaaModel(noise, W, H)], [...noise], 'texture below the threshold is untouched');
});
