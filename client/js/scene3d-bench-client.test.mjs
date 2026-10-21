import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

const source = fs.readFileSync(new URL('../../examples/gosx-docs/public/scene3d-bench-client.js', import.meta.url), 'utf8');

function bench({ workload = 'mixed', renderer = 'webgpu', mounted = true } = {}) {
  const attrs = new Map([
    ['data-gosx-scene3d-renderer', renderer],
    ['data-gosx-scene3d-mounted', String(mounted)],
  ]);
  const mount = { getAttribute: name => attrs.get(name) ?? null };
  const elements = new Map();
  for (const id of ['bench3d-overlay', 'bench3d-api', 'bench3d-gpu', 'bench3d-copy', 'bench3d-render-status', 'bench3d-action-status', 'bench3d-samples']) {
    elements.set(id, { textContent: '', listeners: {}, getAttribute: name => name === 'data-workload' ? workload : null, addEventListener(name, fn) { this.listeners[name] = fn; } });
  }
  let copied;
  let performanceObserver;
  const intervals = [];
  const context = {
    window: {}, console, URL, Float64Array, Date,
    navigator: { userAgent: 'unit-fixture', clipboard: { writeText: async text => { copied = JSON.parse(text); } } },
    document: {
      readyState: 'complete',
      getElementById: id => elements.get(id) ?? null,
      querySelector: () => mount,
      querySelectorAll: () => [],
      createElement: () => ({ getContext: () => null }),
    },
    performance: { now: () => 100, clearMeasures() {} },
    PerformanceObserver: class { constructor(fn) { performanceObserver = fn; } observe() {} },
    requestAnimationFrame() {},
    setTimeout() {},
    setInterval: fn => { intervals.push(fn); return intervals.length; },
    sessionStorage: { getItem: () => null },
  };
  vm.runInNewContext(source, context);
  return {
    attrs, elements,
    tick: () => intervals.forEach(fn => fn()),
    measure: duration => performanceObserver({ getEntries: () => [{ name: 'scene3d-render', entryType: 'measure', duration }] }),
    async snapshot() { elements.get('bench3d-copy').listeners.click(); await Promise.resolve(); return copied; },
  };
}

test('API and JSON follow a renderer fallback instead of latching WebGPU', async () => {
  const b = bench();
  b.measure(2);
  assert.equal((await b.snapshot()).backend, 'webgpu');
  b.attrs.set('data-gosx-scene3d-renderer', 'webgl');
  b.attrs.set('data-gosx-scene3d-renderer-fallback', 'webgpu-pipeline-failed');
  const exported = await b.snapshot();
  assert.equal(b.elements.get('bench3d-api').textContent, 'webgl');
  assert.equal(exported.backend, 'webgl');
  assert.equal(exported.rendererMounted, true);
  assert.equal(exported.rendererFallback, 'webgpu-pipeline-failed');
  assert.equal(exported.cpuSamples, 0, 'WebGPU samples must not be attributed to WebGL');
  assert.match(b.elements.get('bench3d-render-status').textContent, /fallback: webgpu-pipeline-failed/);
});

test('GPU-driven export identifies inactive fallback and excludes failed submit samples', async () => {
  const b = bench({ workload: 'gpu-driven', renderer: 'webgl' });
  b.attrs.set('data-gosx-scene3d-renderer-fallback', 'webgpu-pipeline-failed');
  b.attrs.set('data-gosx-scene3d-webgpu-gpu-driven-active', 'true');
  b.attrs.set('data-gosx-scene3d-webgpu-gpu-driven-reason', 'frustum');
  b.measure(11.2);
  const exported = await b.snapshot();
  assert.equal(exported.backend, 'webgl');
  assert.equal(exported.gpuDriven.requested, true);
  assert.equal(exported.gpuDriven.active, false);
  assert.equal(exported.gpuDriven.reason, 'webgpu-pipeline-failed', 'stale WebGPU attributes must not override the current fallback reason');
  assert.equal(exported.cpuSamples, 0);
  assert.match(b.elements.get('bench3d-render-status').textContent, /GPU-driven path unavailable/);
});

test('mounted state and GPU-driven activation are read again at export time', async () => {
  const b = bench({ workload: 'gpu-driven', mounted: false });
  assert.equal((await b.snapshot()).backend, 'starting');
  b.attrs.set('data-gosx-scene3d-mounted', 'true');
  b.attrs.set('data-gosx-scene3d-webgpu-gpu-driven-active', 'true');
  b.tick();
  b.measure(3);
  const exported = await b.snapshot();
  assert.equal(exported.backend, 'webgpu');
  assert.equal(exported.rendererMounted, true);
  assert.equal(exported.gpuDriven.active, true);
  assert.equal(exported.cpuSamples, 1);
});
