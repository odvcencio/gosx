'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const util = require('node:util');
const vm = require('node:vm');
const { test } = require('node:test');

const producer = fs.readFileSync(path.join(__dirname, 'testdata/scene3d-cubic-spline-browser.cjs'), 'utf8');
const verifier = fs.readFileSync(path.join(__dirname, 'testdata/scene3d-cubic-spline-browser-matrix.cjs'), 'utf8');
const names = ['affine-gl', 'affine-wg', 'gl', 'wg', 'textured-gl', 'textured-wg'];

function eventHarness() {
  // Execute the actual CDP network event branches without starting Chrome or
  // replacing their completion/error policy with a mock implementation.
  const begin = producer.indexOf("    if (m.method === 'Network.requestWillBeSent'");
  const end = producer.indexOf("    if (m.method === 'Log.entryAdded'", begin);
  assert.ok(begin >= 0 && end > begin);
  const context = vm.createContext({ URL, Map });
  vm.runInContext(`
    const BASE = 'http://127.0.0.1:12345';
    const networkRequests = new Map();
    const clientEventCompletions = [];
    const networkFailures = [];
    let activeCaseName = null;
    function networkFail(message) { networkFailures.push(message); }
    function inspectNetworkRequest() {}
    function receive(m, caseName) { activeCaseName = caseName;
      ${producer.slice(begin, end)}
    }
    function snapshot() { return { completions: clientEventCompletions, failures: networkFailures }; }
  `, context);
  return {
    send(method, params, caseName = 'gl') { context.receive({ method, params }, caseName); },
    snapshot() { return JSON.parse(JSON.stringify(context.snapshot())); },
  };
}

function request(x, id, caseName = 'gl', url = 'http://127.0.0.1:12345/_gosx/client-events', method = 'POST') {
  x.send('Network.requestWillBeSent', { requestId: id, request: { method, url } }, caseName);
  return url;
}
function headers(x, id, url, status = 204) {
  x.send('Network.responseReceived', { requestId: id, response: { url, status } });
}
function complete(x, id) { x.send('Network.loadingFinished', { requestId: id }); }

function reportHarness() {
  const begin = verifier.indexOf('function checkClientEventCompletion(');
  const end = verifier.indexOf('function checkTopLevel(', begin);
  assert.ok(begin >= 0 && end > begin);
  const context = vm.createContext({ util });
  vm.runInContext(`const EXPECTED_CASE_NAMES = ${JSON.stringify(names)};`, context);
  const helpers = verifier.slice(verifier.indexOf('function check('), verifier.indexOf('function containsSwiftShader('));
  vm.runInContext(helpers + verifier.slice(begin, end), context);
  return report => {
    context.reportJSON = JSON.stringify(report);
    return JSON.parse(vm.runInContext(`(function () {
      const failures = [];
      checkClientEventCompletion(failures, JSON.parse(reportJSON));
      return JSON.stringify(failures);
    })()`, context));
  };
}
function completedReport() {
  const x = eventHarness();
  for (const name of names) {
    const url = request(x, name, name);
    headers(x, name, url);
    complete(x, name);
  }
  return {
    networkFailures: x.snapshot().failures,
    clientEventCompletions: x.snapshot().completions,
    clientEventResponses: names.map(() => ({ method: 'POST', path: '/_gosx/client-events', status: 204 })),
  };
}

test('204 headers do not count as client-event completion', () => {
  const x = eventHarness();
  const url = request(x, 'one');
  headers(x, 'one', url);
  assert.deepEqual(x.snapshot(), { completions: [], failures: [] });
  complete(x, 'one');
  assert.deepEqual(x.snapshot().completions, [{ caseName: 'gl', method: 'POST', path: '/_gosx/client-events', status: 204, cdpTerminal: 'loadingFinished' }]);
});

test('204 ERR_ABORTED is fatal and never supplies completion evidence', () => {
  const x = eventHarness();
  const url = request(x, 'one');
  headers(x, 'one', url);
  x.send('Network.loadingFailed', { requestId: 'one', errorText: 'net::ERR_ABORTED', canceled: true });
  complete(x, 'one');
  assert.deepEqual(x.snapshot().completions, []);
  assert.equal(x.snapshot().failures.length, 1);
  assert.match(x.snapshot().failures[0], /net::ERR_ABORTED after HTTP 204 \(canceled\)/);
});

test('other failed and unknown requests remain fatal', () => {
  const x = eventHarness();
  x.send('Network.loadingFailed', { requestId: 'unknown', errorText: 'net::ERR_CONNECTION_RESET' });
  assert.equal(x.snapshot().failures.length, 1);
  assert.match(x.snapshot().failures[0], /request unknown.*ERR_CONNECTION_RESET/);
});

test('only the owned POST204 endpoint may produce completion evidence', () => {
  const x = eventHarness();
  for (const [id, url, method, status] of [
    ['foreign', 'http://other.invalid/_gosx/client-events', 'POST', 204],
    ['query', 'http://127.0.0.1:12345/_gosx/client-events?x=1', 'POST', 204],
    ['asset', 'http://127.0.0.1:12345/bootstrap.js', 'GET', 204],
    ['wrong-status', 'http://127.0.0.1:12345/_gosx/client-events', 'POST', 200],
  ]) {
    request(x, id, 'gl', url, method); headers(x, id, url, status); complete(x, id);
  }
  complete(x, 'unknown');
  assert.deepEqual(x.snapshot().completions, []);
});

test('a duplicate loadingFinished cannot count a request twice', () => {
  const x = eventHarness(); const url = request(x, 'one'); headers(x, 'one', url);
  complete(x, 'one'); complete(x, 'one');
  assert.equal(x.snapshot().completions.length, 1);
});

test('matrix accepts actual completions attributed to all six renderer cases', () => {
  assert.deepEqual(reportHarness()(completedReport()), []);
});

test('matrix rejects missing terminal evidence even with six server responses', () => {
  const report = completedReport(); report.clientEventCompletions.pop();
  assert.match(reportHarness()(report).join('\n'), /clientEventCompletions/);
});

test('matrix rejects the former intentional204abort exemption', () => {
  const report = completedReport();
  report.clientEventCompletions[0].cdpTerminal = 'loadingFailed:net::ERR_ABORTED';
  assert.match(reportHarness()(report).join('\n'), /clientEventCompletions/);
});

test('matrix rejects completed probes belonging to the wrong case', () => {
  const report = completedReport(); report.clientEventCompletions[1].caseName = 'affine-gl';
  assert.match(reportHarness()(report).join('\n'), /clientEventCompletions/);
});

test('matrix rejects any abort or network error alongside completed probes', () => {
  const report = completedReport(); report.networkFailures.push('loadingFailed net::ERR_ABORTED');
  assert.match(reportHarness()(report).join('\n'), /networkFailures/);
});

test('browser renderer receipts retain primitive metadata for async pipelines', async () => {
  const pipeline = {};
  function GPUDevice() {}
  GPUDevice.prototype.createRenderPipeline = () => pipeline;
  GPUDevice.prototype.createRenderPipelineAsync = () => Promise.resolve(pipeline);
  const context = vm.createContext({ GPUDevice, wgPipelines: new WeakMap(), wgBindGroups: new WeakMap(), wgBufferID: () => 0 });
  const wrapStart = producer.indexOf('  function wrap(proto, name, handler)');
  const wrapEnd = producer.indexOf('  function wrapGL(', wrapStart);
  const start = producer.indexOf("  if (typeof GPUDevice !== 'undefined' && GPUDevice.prototype) {");
  const end = producer.indexOf("  if (typeof GPUCommandEncoder !== 'undefined'", start);
  assert.ok(wrapStart >= 0 && wrapEnd > wrapStart && start >= 0 && end > start);
  vm.runInContext(producer.slice(wrapStart, wrapEnd) + producer.slice(start, end), context);
  const device = new GPUDevice();
  const pending = device.createRenderPipelineAsync({ primitive: { frontFace: 'cw', cullMode: 'none' } });
  assert.ok(pending instanceof Promise);
  assert.equal(await pending, pipeline, 'the hook must forward the native pipeline');
  assert.equal(context.wgPipelines.get(pipeline)?.frontFace, 'cw');
  assert.equal(context.wgPipelines.get(pipeline)?.cullMode, 'none');
});
test('affine receipt joins the model arena through the recorded dynamic offset', () => {
  const local = [-0.4, -0.4, 0, 0.4, -0.4, 0, 0, 0.4, 0];
  const model = [-2,0,0,0,0,1,0,0,0,0,1,0,0.5,0.5,1,1];
  const identity = [1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1];
  const material = Array(72).fill(0); material.splice(20,16,...identity);
  const arena = Array(80).fill(0); arena.splice(0,16,...identity); arena.splice(64,16,...model);
  const record = { kind:'drawIndexed', positionBuffer:1, materialBuffer:2, modelBuffer:3,
    modelOffset:256, bundle:1, frontFace:'cw', cullMode:'none' };
  const window = { __affineRefs:{object:{parentMatrix:model},mount:{id:'affine-wg'}},
    __affineWGUploads:[{buffer:1,values:local},{buffer:2,values:material},{buffer:3,values:arena}],
    __affineWGDrawRecords:[record], __affineWGExecutedBundles:[1] };
  const begin = producer.indexOf('function affineEvidenceExpr() {');
  const end = producer.indexOf('\nfunction hubExpr(', begin);
  const context = vm.createContext({window, AFFINE_LOCAL_POSITIONS:local, AFFINE_MODEL_MATRIX:model});
  vm.runInContext(producer.slice(begin,end), context);
  const evidence = () => JSON.parse(JSON.stringify(vm.runInContext(context.affineEvidenceExpr(),context)));
  const joined = evidence();
  assert.deepEqual(joined.wgModel.values, model);
  assert.equal(joined.wgDraw.modelBuffer, joined.wgModel.buffer);
  assert.equal(joined.wgMaterial.buffer, record.materialBuffer);
  window.__affineWGUploads[2] = {buffer:3,offset:256,values:model};
  assert.deepEqual(evidence().wgModel.values, model, 'partial uploads join at the actual buffer offset');
  record.modelOffset = 0;
  assert.equal(evidence().wgDraw, null, 'binding the identity row cannot produce an affine receipt');
  record.modelOffset = 256; record.modelBuffer = 4;
  assert.equal(evidence().wgDraw, null, 'an unbound model upload cannot produce an affine receipt');
});

test('affine GPU instrumentation records the bound model buffer and typed dynamic-offset range', () => {
  class Device {
    createBuffer() { return {}; }
    createBindGroup() { return {}; }
    createRenderPipeline() { return {}; }
  }
  class Pass {
    setVertexBuffer() {} setBindGroup() {} setPipeline() {} draw() {} drawIndexed() {} executeBundles() {}
  }
  const window = {__affineWGDrawRecords:[],__affineWGExecutedBundles:[]};
  const begin = producer.indexOf('  var nextWGBufferID =');
  const end = producer.indexOf("  document.addEventListener('gosx:scene3d:input'",begin);
  assert.ok(begin > 0 && end > begin);
  const context = vm.createContext({window,GPUDevice:Device,GPURenderPassEncoder:Pass,Float32Array});
  vm.runInContext(`function floats(v){return v instanceof Float32Array?Array.from(v):null;}
    function wrap(proto,name,handler){const original=proto[name];if(original)proto[name]=function(){return handler.call(this,original,arguments);};}
    ${producer.slice(begin,end)}`,context);
  const device = new Device(), position = device.createBuffer(), material = device.createBuffer(), model = device.createBuffer();
  const group = device.createBindGroup({entries:[{binding:0,resource:{buffer:material}},
    {binding:19,resource:{buffer:model,offset:0,size:64}}]});
  const pass = new Pass(); pass.setVertexBuffer(0,position);
  pass.setBindGroup(1,group,new Uint32Array([0,256,512]),1,1);
  pass.drawIndexed(3);
  const receipt = window.__affineWGDrawRecords[0];
  assert.equal(receipt.positionBuffer,1); assert.equal(receipt.materialBuffer,2);
  assert.equal(receipt.modelBuffer,3); assert.equal(receipt.modelOffset,256);
});
