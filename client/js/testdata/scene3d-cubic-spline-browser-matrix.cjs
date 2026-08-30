'use strict';
/* Bounded, causal four-mode verifier for the Scene3D CUBICSPLINE browser
 * renderer proof. One selected Chrome binary runs every mode:
 *
 *   positive      native WebGL2 canvas + WebGPU private-target renderer proof
 *   gap100        the same proof with a 100ms atomic restore scheduler gap
 *   no-draw       WebGPU draw suppression must fail only the pixel oracle
 *   no-submit     WebGPU submit suppression must fail forwarding + pixels
 *
 * A negative mode is accepted only when its exact WebGPU oracle diagnostics
 * appear while WebGL pixels, WebGPU pass/submit counters, analytic vertices,
 * identity, restoration, telemetry, and network surfaces remain green. */

const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const util = require('util');
const { spawn, spawnSync } = require('child_process');

const REPO = process.argv[2] ? path.resolve(process.argv[2]) : '';
const ART = process.argv[3] ? path.resolve(process.argv[3]) : '';
if (!REPO || !ART) {
  console.error('usage: node scene3d-cubic-spline-browser-matrix.cjs ' +
    '<repoRoot> <existingArtifactDir>');
  process.exit(2);
}
try {
  if (!fs.statSync(ART).isDirectory()) throw new Error('not a directory');
} catch (e) {
  console.error('artifact dir not usable: ' + ART + ' (' + e.message + ')');
  process.exit(2);
}

const HARNESS = path.join(REPO, 'client', 'js', 'testdata',
  'scene3d-cubic-spline-browser.cjs');
const fixture = require(path.join(REPO, 'client', 'js', 'testdata',
  'cubic-spline-fixture.cjs'));
const CHROME_BIN = process.env.GOSX_CHROME_BIN || '/usr/bin/google-chrome';
const EXPECTED_VERSION_ENV = 'GOSX_EXPECTED_CHROME_VERSION';
const IDENTITY_SELF_CHECK_ENV = 'GOSX_SCENE3D_CUBIC_IDENTITY_SELF_CHECK_ONLY';
const IDENTITY_SELF_CHECK_ONLY = process.env[IDENTITY_SELF_CHECK_ENV] || '';
const MODE_TIMEOUT_MS = 115000;
const MATRIX_REPORT = path.join(ART, 'matrix-report.json');
const EXPECTED_PROOF_TARGET = 'private-texture';
const EXPECTED_CHROME_GPU_FLAGS = [
  '--ignore-gpu-blocklist',
  '--enable-unsafe-swiftshader',
  '--enable-unsafe-webgpu',
  '--use-gl=angle',
  '--use-angle=swiftshader',
  '--use-webgpu-adapter=swiftshader',
  '--use-gpu-in-tests',
];
const EXPECTED_CHROME_WINDOW_FLAGS = ['--headless=new'];
const CHROME_SWAP_DIAGNOSTICS = [
  'webgpuswapchaintexture',
  'sharedimagebackingfactory',
  'unable to create shared image',
  'non-existent mailbox',
];
const CHROME_PRE_TEARDOWN_LIFECYCLE_DIAGNOSTICS = [
  'external instance',
  'device was destroyed',
  'device has been lost',
  'gpu device lost',
];

if (IDENTITY_SELF_CHECK_ONLY && IDENTITY_SELF_CHECK_ONLY !== 'wrong-product-same-version') {
  console.error('unsupported ' + IDENTITY_SELF_CHECK_ENV + ': ' + IDENTITY_SELF_CHECK_ONLY);
  process.exit(2);
}

const MODES = [
  { name: 'positive', mutation: '', gapMS: 0, expectedExitCode: 0 },
  { name: 'gap100', mutation: '', gapMS: 100, expectedExitCode: 0 },
  { name: 'no-draw', mutation: 'webgpu-no-draw', gapMS: 0, expectedExitCode: 1 },
  { name: 'no-submit', mutation: 'webgpu-no-submit', gapMS: 0, expectedExitCode: 1 },
];

const NEGATIVE_ERRORS = {
  'no-draw': [
    '[wg] baseline mapped renderer target contains 0 non-background pixels, expected > 20',
    '[wg] playing mapped renderer target contains 0 non-background pixels, expected > 20',
    '[wg] playing frame changed 0 pixels, expected > 20',
    '[wg] restored mapped renderer target contains 0 non-background pixels, expected > 20',
  ],
  'no-submit': [
    '[wg] baseline product queue submission was not forwarded',
    '[wg] baseline mapped renderer target contains 0 non-background pixels, expected > 20',
    '[wg] playing product queue submission was not forwarded',
    '[wg] playing mapped renderer target contains 0 non-background pixels, expected > 20',
    '[wg] playing frame changed 0 pixels, expected > 20',
    '[wg] restored product queue submission was not forwarded',
    '[wg] restored mapped renderer target contains 0 non-background pixels, expected > 20',
  ],
};

function digestFile(file) {
  return crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
}

function verifyHostedClaimSources() {
  const files = {
    harness: HARNESS,
    matrix: __filename,
    workflow: path.join(REPO, '.github', 'workflows', 'ci.yml'),
    corpus: path.join(REPO, 'scene', 'harness', 'testdata', 'v1-corpus.json'),
    readme: path.join(REPO, 'README.md'),
    support: path.join(REPO, 'docs', 'scene3d-v1-support.md'),
  };
  const source = Object.fromEntries(Object.entries(files).map(([name, file]) =>
    [name, fs.readFileSync(file, 'utf8')]));
  // The verifier itself names the rejected phrases below, so evaluate only
  // the hosted evidence sources those assertions govern.
  const combinedProofSources = [source.harness, source.workflow, source.corpus,
    source.readme, source.support]
    .join('\n');
  const forbidden = [
    'scene3d-v1-browser-proof',
    'Run Scene3D CUBICSPLINE browser proof',
    'mapped presentation',
    'COPY_SRC presentation texture',
    'WebGPU presentation proof',
    'native WebGL2 + WebGPU presentation proof',
    'remains hardware-certified',
  ];
  const found = forbidden.filter((phrase) => combinedProofSources.includes(phrase));
  if (found.length > 0) {
    throw new Error('hosted claim sources retain forbidden presentation wording: ' +
      JSON.stringify(found));
  }
  if (/canvasPresented\s*:\s*true/.test(combinedProofSources)) {
    throw new Error('private-target proof sources must not claim canvasPresented:true');
  }
  for (const required of [
    'scene3d-v1-browser-renderer-proof',
    'Run Scene3D CUBICSPLINE browser renderer proof',
    'GOSX_SCENE3D_CUBIC_WEBGPU_TARGET: private-texture',
    'proof-private-gpu-texture',
    'canvasPresented: false',
    'proof-private GPU target',
    'Actual WebGPU canvas presentation remains part of the release-pinned hardware',
  ]) {
    if (!combinedProofSources.includes(required)) {
      throw new Error('hosted claim source marker missing: ' + required);
    }
  }
  return {
    verified: true,
    forbiddenPhrasesAbsent: forbidden,
    canvasPresentedTrueAbsent: true,
    fileSHA256: Object.fromEntries(Object.entries(files).map(([name, file]) =>
      [name, digestFile(file)])),
  };
}

const FOUR_PART_VERSION = /^\d+\.\d+\.\d+\.\d+$/;
const CHROME_CLI_PRODUCT = /^(Google Chrome|Google Chrome for Testing) (\d+\.\d+\.\d+\.\d+)$/;

function parseChromeCLI(invocation) {
  const match = CHROME_CLI_PRODUCT.exec(invocation);
  if (!match) {
    throw new Error('selected browser CLI identity must be exactly ' +
      'Google Chrome <four-part> or Google Chrome for Testing <four-part>; got ' +
      JSON.stringify(invocation));
  }
  return { product: match[1], version: match[2] };
}

function selectedBrowser() {
  if (!fs.existsSync(CHROME_BIN)) throw new Error('chrome binary not found: ' + CHROME_BIN);
  const probe = spawnSync(CHROME_BIN, ['--version'], {
    encoding: 'utf8', timeout: 10000, windowsHide: true,
  });
  if (probe.error || probe.status !== 0) {
    throw new Error('selected Chrome --version failed: ' +
      String(probe.error && probe.error.message || probe.stderr || probe.status));
  }
  const invocation = String(probe.stdout || probe.stderr || '').trim();
  const cli = parseChromeCLI(invocation);
  const expectedIsSet = Object.prototype.hasOwnProperty.call(process.env, EXPECTED_VERSION_ENV);
  const actionVersion = expectedIsSet ? process.env[EXPECTED_VERSION_ENV] : null;
  if (expectedIsSet && !FOUR_PART_VERSION.test(actionVersion)) {
    throw new Error(EXPECTED_VERSION_ENV + ' must be an exact four-part version; got ' +
      JSON.stringify(actionVersion));
  }
  const expectedVersion = expectedIsSet ? actionVersion : cli.version;
  if (cli.version !== expectedVersion) {
    throw new Error('selected browser CLI version ' + cli.version +
      ' does not equal expected action version ' + expectedVersion);
  }
  return {
    configuredPath: CHROME_BIN,
    realPath: fs.realpathSync(CHROME_BIN),
    invocation,
    cliProduct: cli.product,
    version: cli.version,
    expectedVersion,
    expectedVersionSource: expectedIsSet ? EXPECTED_VERSION_ENV : 'anchored-cli',
  };
}

const receipt = {
  schemaVersion: 1,
  startedAt: new Date().toISOString(),
  nodeVersion: process.version,
  selectedBrowser: null,
  webgpuProofTarget: EXPECTED_PROOF_TARGET,
  modeTimeoutMS: MODE_TIMEOUT_MS,
  modes: [],
  errors: [],
};

function writeReceipt() {
  fs.writeFileSync(MATRIX_REPORT, JSON.stringify(receipt, null, 2));
}

function check(failures, condition, message) {
  if (!condition) failures.push(message);
}

function exact(failures, actual, expected, label) {
  check(failures, util.isDeepStrictEqual(actual, expected), label + ': got ' +
    JSON.stringify(actual) + ', want ' + JSON.stringify(expected));
}

function containsSwiftShader(value) {
  try { return JSON.stringify(value).toLowerCase().includes('swiftshader'); }
  catch (_error) { return false; }
}

function countDiagnostic(content, needle) {
  let count = 0;
  let offset = 0;
  for (;;) {
    const found = content.indexOf(needle, offset);
    if (found < 0) return count;
    count += 1;
    offset = found + Math.max(1, needle.length);
  }
}

function scanChromeDiagnostics(raw, boundary) {
  const all = raw.toString('utf8').toLowerCase();
  const bounded = Number.isInteger(boundary)
    ? Math.max(0, Math.min(raw.length, boundary)) : raw.length;
  const beforeTeardown = raw.subarray(0, bounded).toString('utf8').toLowerCase();
  return {
    scannedBytes: raw.length,
    webgpuIntentionalTeardownStderrByte: bounded,
    swapFindings: CHROME_SWAP_DIAGNOSTICS.map((needle) => ({
      needle, count: countDiagnostic(all, needle),
    })).filter((entry) => entry.count > 0),
    preTeardownLifecycleFindings:
      CHROME_PRE_TEARDOWN_LIFECYCLE_DIAGNOSTICS.map((needle) => ({
        needle, count: countDiagnostic(beforeTeardown, needle),
      })).filter((entry) => entry.count > 0),
    scanError: '',
  };
}

function checkCDPBrowserIdentity(failures, value, browser, label) {
  exact(failures, value && value.product, 'Chrome/' + browser.expectedVersion,
    label + '.product');
  check(failures, value && typeof value.protocolVersion === 'string' &&
    value.protocolVersion.trim() !== '', label + '.protocolVersion must be non-empty');
  check(failures, value && typeof value.revision === 'string' &&
    value.revision.trim() !== '', label + '.revision must be non-empty');
}

function browserIdentitySelfCheck(browser) {
  const wrongCDPProduct = 'NotChrome/' + browser.expectedVersion;
  const cdpFailures = [];
  checkCDPBrowserIdentity(cdpFailures, {
    product: wrongCDPProduct, protocolVersion: '1.3', revision: '@adversarial-mutation',
  }, browser, 'wrongSameVersionCDP');
  if (cdpFailures.length !== 1 ||
      !cdpFailures[0].includes('wrongSameVersionCDP.product')) {
    throw new Error('same-version wrong CDP product was not rejected exactly: ' +
      JSON.stringify(cdpFailures));
  }

  const wrongVersionCDPProduct = 'Chrome/0.0.0.0';
  const wrongVersionFailures = [];
  checkCDPBrowserIdentity(wrongVersionFailures, {
    product: wrongVersionCDPProduct, protocolVersion: '1.3',
    revision: '@adversarial-version-mutation',
  }, browser, 'wrongVersionCDP');
  if (wrongVersionFailures.length !== 1 ||
      !wrongVersionFailures[0].includes('wrongVersionCDP.product')) {
    throw new Error('wrong CDP version was not rejected exactly: ' +
      JSON.stringify(wrongVersionFailures));
  }

  const wrongCLIInvocation = 'Chromium ' + browser.expectedVersion;
  let wrongCLIError = '';
  try { parseChromeCLI(wrongCLIInvocation); }
  catch (error) { wrongCLIError = String(error && error.message || error); }
  if (!wrongCLIError) {
    throw new Error('same-version wrong CLI product was accepted: ' + wrongCLIInvocation);
  }
  return {
    mutation: 'wrong-product-same-version',
    wrongCDPProduct,
    wrongCDPRejected: true,
    wrongCDPFailures: cdpFailures,
    wrongVersionCDPProduct,
    wrongVersionRejected: true,
    wrongVersionFailures,
    wrongCLIInvocation,
    wrongCLIRejected: true,
    wrongCLIError,
  };
}

function closeArray(failures, actual, expected, label, tolerance) {
  if (!Array.isArray(actual) || actual.length !== expected.length) {
    failures.push(label + ': wrong shape ' + JSON.stringify(actual));
    return;
  }
  for (let i = 0; i < expected.length; i += 1) {
    const value = Number(actual[i]);
    if (!Number.isFinite(value) || Math.abs(value - expected[i]) >= tolerance) {
      failures.push(label + '[' + i + ']: got ' + actual[i] + ', want ' +
        expected[i] + ' within ' + tolerance);
    }
  }
}

function checkIdentity(failures, value, label) {
  exact(failures, value, {
    sameMount: true, sameCanvas: true, sameState: true, sameRecord: true,
  }, label);
}

function checkTelemetry(failures, value, label) {
  check(failures, value && typeof value === 'object', label + ': missing');
  if (!value || typeof value !== 'object') return;
  exact(failures, value.enabled, true, label + '.enabled');
  for (const field of ['queueDepth', 'pendingRequests', 'droppedOverflowEvents',
    'droppedSerializationEvents', 'failedEvents', 'failedBatches',
    'beaconFailures', 'fetchFailures']) {
    exact(failures, value[field], 0, label + '.' + field);
  }
  for (const field of ['emittedEvents', 'attemptedEvents', 'attemptedBatches',
    'dispatchedEvents', 'dispatchedBatches', 'serverAcceptedEvents',
    'serverAcceptedBatches']) {
    exact(failures, value[field], 1, label + '.' + field);
  }
  exact(failures, value.lastFlushReason, 'manual-drain', label + '.lastFlushReason');
  exact(failures, value.lastFailureReason, '', label + '.lastFailureReason');
}

function checkCommonCase(failures, value, name) {
  check(failures, value && typeof value === 'object', name + ': case missing');
  if (!value || typeof value !== 'object') return;
  const webgpu = name === 'wg';
  exact(failures, value.name, name, name + '.name');
  exact(failures, value.webgpu, webgpu, name + '.webgpu');
  exact(failures, value.mount, 'scene-cubic-' + name, name + '.mount');
  exact(failures, value.engine, 'gosx-engine-cubic-' + name, name + '.engine');
  exact(failures, value.attrs, {
    mounted: 'true', renderer: webgpu ? 'webgpu' : 'webgl', fallback: null,
  }, name + '.attrs');

  const load = value.load || {};
  exact(failures, load.mixer, true, name + '.load.mixer');
  exact(failures, load.wasm, false, name + '.load.wasm');
  exact(failures, load.playing, true, name + '.load.playing');
  exact(failures, load.animation, 'curve', name + '.load.animation');
  exact(failures, load.animatedCount, 2, name + '.load.animatedCount');
  exact(failures, load.clock, 0, name + '.load.clock');
  exact(failures, load.meshID, 'cubic/tri-prim-0', name + '.load.meshID');
  closeArray(failures, load.verts, fixture.BASE_POSITIONS, name + '.load.verts', 1e-6);
  closeArray(failures, load.t0 && load.t0.translation, [0, 0, 0],
    name + '.load.translation', 1e-6);
  closeArray(failures, load.t0 && load.t0.rotation, [0, 0, 0, 1],
    name + '.load.rotation', 1e-6);
  closeArray(failures, load.t0 && load.t0.scale, [1, 1, 1],
    name + '.load.scale', 1e-6);
  closeArray(failures, load.t0 && load.t0.weights, [0, 0],
    name + '.load.weights', 1e-6);
  closeArray(failures, load.t1 && load.t1.translation, [0, 0, 0],
    name + '.load.clockTranslation', 1e-6);

  const frozen = value.frozen || {};
  const clock = Number(frozen.clock);
  check(failures, Number.isFinite(clock) && clock >= 0.9 && clock <= 2.5,
    name + '.frozen.clock outside [0.9,2.5]: ' + frozen.clock);
  check(failures, Number(value.observedClock) >= 0.9 && Number(value.observedClock) <= 2.5,
    name + '.observedClock outside [0.9,2.5]: ' + value.observedClock);
  exact(failures, value.frozenClock, frozen.clock, name + '.frozenClock');
  exact(failures, frozen.mixer, true, name + '.frozen.mixer');
  exact(failures, frozen.wasm, false, name + '.frozen.wasm');
  exact(failures, frozen.playing, true, name + '.frozen.playing');
  exact(failures, frozen.animation, 'curve', name + '.frozen.animation');
  exact(failures, frozen.animatedCount, 2, name + '.frozen.animatedCount');
  exact(failures, frozen.meshID, 'cubic/tri-prim-0', name + '.frozen.meshID');
  if (Number.isFinite(clock)) {
    closeArray(failures, frozen.t0 && frozen.t0.translation, fixture.evalTranslation(clock),
      name + '.frozen.translation', 2e-3);
    closeArray(failures, frozen.t0 && frozen.t0.rotation, fixture.evalRotation(clock),
      name + '.frozen.rotation', 2e-3);
    closeArray(failures, frozen.t0 && frozen.t0.scale, fixture.evalScale(clock),
      name + '.frozen.scale', 2e-3);
    closeArray(failures, frozen.t0 && frozen.t0.weights, fixture.evalWeights(clock),
      name + '.frozen.weights', 2e-3);
    closeArray(failures, frozen.t1 && frozen.t1.translation, [clock, 0, 0],
      name + '.frozen.clockTranslation', 2e-3);
    closeArray(failures, frozen.verts, fixture.expectedWorldPositions(clock),
      name + '.frozen.verts', 2e-3);
  }

  checkIdentity(failures, value.identityAfterPlayback, name + '.identityAfterPlayback');
  checkIdentity(failures, value.identityAfterRestore, name + '.identityAfterRestore');

  const restored = value.restored || {};
  exact(failures, restored.playing, false, name + '.restored.playing');
  exact(failures, restored.animation, '', name + '.restored.animation');
  exact(failures, restored.animatedCount, 0, name + '.restored.animatedCount');
  exact(failures, restored.t0, null, name + '.restored.t0');
  exact(failures, restored.t1, null, name + '.restored.t1');
  exact(failures, restored.clock, null, name + '.restored.clock');
  exact(failures, restored.meshID, 'cubic/tri-prim-0', name + '.restored.meshID');
  closeArray(failures, restored.verts, fixture.BASE_POSITIONS,
    name + '.restored.verts', 1e-6);
  check(failures, value.restoreDiff && value.restoreDiff.dimsMatch === true &&
    value.restoreDiff.exactPixels === 0 && value.restoreDiff.exactBytes === 0,
  name + '.restoreDiff is not an exact pixel restore: ' + JSON.stringify(value.restoreDiff));
  exact(failures, value.disposed, true, name + '.disposed');
  checkTelemetry(failures, value.telemetry, name + '.telemetry');
}

function checkGL(failures, value) {
  checkCommonCase(failures, value, 'gl');
  if (!value) return;
  exact(failures, value.load && value.load.gl, 'webgl2', 'gl.load.gl');
  check(failures, value.frozen && value.load && value.frozen.draws > value.load.draws,
    'gl draw counter did not advance during playback');
  check(failures, value.restored && value.frozen && value.restored.draws > value.frozen.draws,
    'gl draw counter did not advance through restore');
  check(failures, value.playDiff && value.playDiff.dimsMatch === true &&
    value.playDiff.exactPixels > 20,
  'gl playback did not change real pixels: ' + JSON.stringify(value.playDiff));
}

function checkWG(failures, value, mode) {
  checkCommonCase(failures, value, 'wg');
  if (!value) return;
  const diagnostics = value.webgpuDiagnostics;
  check(failures, diagnostics && typeof diagnostics === 'object',
    'wg.webgpuDiagnostics: missing');
  if (diagnostics && typeof diagnostics === 'object') {
    exact(failures, diagnostics.ready, true, 'wg.webgpuDiagnostics.ready');
    exact(failures, diagnostics.adapterAvailable, true,
      'wg.webgpuDiagnostics.adapterAvailable');
    exact(failures, diagnostics.deviceAvailable, true,
      'wg.webgpuDiagnostics.deviceAvailable');
    exact(failures, diagnostics.error, '', 'wg.webgpuDiagnostics.error');
    exact(failures, diagnostics.lost, null, 'wg.webgpuDiagnostics.lost');
    check(failures, diagnostics.adapterInfo && typeof diagnostics.adapterInfo === 'object',
      'wg.webgpuDiagnostics.adapterInfo: missing');
    check(failures, diagnostics.adapterInfo &&
      String(diagnostics.adapterInfo.architecture || '').toLowerCase() === 'swiftshader',
    'wg.webgpuDiagnostics.adapterInfo.architecture is not SwiftShader');
    check(failures, Array.isArray(diagnostics.deviceFeatures),
      'wg.webgpuDiagnostics.deviceFeatures: missing');
    check(failures, diagnostics.deviceLimits && typeof diagnostics.deviceLimits === 'object',
      'wg.webgpuDiagnostics.deviceLimits: missing');
  }
  check(failures, value.load && value.load.wgPasses > 0 && value.load.wgSubmits > 0,
    'wg initial render-pass/submission counters are not positive');
  check(failures, value.frozen && value.load &&
    value.frozen.wgPasses > value.load.wgPasses &&
    value.frozen.wgSubmits > value.load.wgSubmits,
  'wg legacy render-pass/submission counters did not advance during playback');
  check(failures, value.restored && value.frozen &&
    value.restored.wgPasses > value.frozen.wgPasses &&
    value.restored.wgSubmits > value.frozen.wgSubmits,
  'wg legacy render-pass/submission counters did not advance through restore');

  const proof = value.webgpuProof;
  check(failures, proof && typeof proof === 'object', 'wg.webgpuProof: missing');
  if (proof && typeof proof === 'object') {
    exact(failures, proof.proofTarget, EXPECTED_PROOF_TARGET,
      'wg.webgpuProof.proofTarget');
    exact(failures, proof.renderTargetKind, 'proof-private-gpu-texture',
      'wg.webgpuProof.renderTargetKind');
    exact(failures, proof.canvasPresented, false, 'wg.webgpuProof.canvasPresented');
    check(failures, Number.isInteger(proof.interceptedConfigureCalls) &&
      proof.interceptedConfigureCalls > 0,
    'wg.webgpuProof.interceptedConfigureCalls is not positive');
    check(failures, Number.isInteger(proof.interceptedGetCurrentTextureCalls) &&
      proof.interceptedGetCurrentTextureCalls > 0,
    'wg.webgpuProof.interceptedGetCurrentTextureCalls is not positive');
    exact(failures, proof.nativeConfigureCalls, 0,
      'wg.webgpuProof.nativeConfigureCalls');
    exact(failures, proof.nativeGetCurrentTextureCalls, 0,
      'wg.webgpuProof.nativeGetCurrentTextureCalls');
    check(failures, Number.isInteger(proof.productSubmitCalls) && proof.productSubmitCalls > 0,
      'wg.webgpuProof.productSubmitCalls is not positive');
    exact(failures, proof.proofCopySubmitCalls, 3,
      'wg.webgpuProof.proofCopySubmitCalls');
    check(failures, Number.isInteger(proof.reusedConfigureCalls) &&
      proof.reusedConfigureCalls >= 0,
    'wg.webgpuProof.reusedConfigureCalls is invalid');
    exact(failures, proof.failures, [], 'wg.webgpuProof.failures');
    exact(failures, proof.capturedLabels,
      ['wg-baseline', 'wg-playing', 'wg-restored'], 'wg.webgpuProof.capturedLabels');
    const configuredTargets = Array.isArray(proof.configuredTargets)
      ? proof.configuredTargets : [];
    check(failures, configuredTargets.length > 0,
      'wg.webgpuProof.configuredTargets must not be empty');
    exact(failures, proof.targetGenerationCount, configuredTargets.length,
      'wg.webgpuProof.targetGenerationCount');
    exact(failures, proof.interceptedConfigureCalls,
      configuredTargets.length + proof.reusedConfigureCalls,
      'wg.webgpuProof configure/create/reuse count');
    exact(failures, proof.contextCount,
      new Set(configuredTargets.map((target) => target && target.contextID)).size,
      'wg.webgpuProof.contextCount');
    const returnedTargets = configuredTargets.filter((target) =>
      target && target.returnedToProduct === true);
    exact(failures, returnedTargets.length, 1,
      'wg.webgpuProof returned target count');
    const target = returnedTargets[0];
    if (target) {
      exact(failures, target.renderTargetKind, 'proof-private-gpu-texture',
        'wg.webgpuProof.configuredTargets[0].renderTargetKind');
      exact(failures, target.canvasPresented, false,
        'wg.webgpuProof.configuredTargets[0].canvasPresented');
      exact(failures, target.width, 320, 'wg.webgpuProof.configuredTargets[0].width');
      exact(failures, target.height, 180, 'wg.webgpuProof.configuredTargets[0].height');
      check(failures, Number.isInteger(target.targetUsage) &&
        (target.targetUsage & 0x01) === 0x01 && (target.targetUsage & 0x10) === 0x10,
      'wg.webgpuProof.configuredTargets[0].targetUsage lacks COPY_SRC|RENDER_ATTACHMENT');
      check(failures, Number.isInteger(target.configureCalls) && target.configureCalls > 0,
        'wg.webgpuProof.configuredTargets[0].configureCalls is not positive');
      check(failures, Number.isInteger(target.reusedConfigureCalls) &&
        target.reusedConfigureCalls >= 0 &&
        target.configureCalls === target.reusedConfigureCalls + 1,
      'wg.webgpuProof.configuredTargets[0] configure reuse receipt is invalid');
      check(failures, target.alphaMode === null ||
        typeof target.alphaMode === 'string',
      'wg.webgpuProof.configuredTargets[0].alphaMode is invalid');
      check(failures, target.colorSpace === null ||
        typeof target.colorSpace === 'string',
      'wg.webgpuProof.configuredTargets[0].colorSpace is invalid');
      check(failures, target.toneMapping === null ||
        target.toneMapping && typeof target.toneMapping === 'object' &&
        !Array.isArray(target.toneMapping),
      'wg.webgpuProof.configuredTargets[0].toneMapping is invalid');
      exact(failures, target.returnedToProduct, true,
        'wg.webgpuProof.configuredTargets[0].returnedToProduct');
      for (const field of ['getCurrentTextureCalls', 'targetViewCalls',
        'linkedRenderPasses', 'linkedCommandBuffers', 'linkedProductSubmits']) {
        check(failures, Number.isInteger(target[field]) && target[field] > 0,
          'wg.webgpuProof.configuredTargets[0].' + field + ' is not positive');
      }
    }
    for (const inactive of configuredTargets.filter((candidate) => candidate !== target)) {
      exact(failures, inactive && inactive.returnedToProduct, false,
        'wg.webgpuProof inactiveTarget.returnedToProduct');
      for (const field of ['getCurrentTextureCalls', 'targetViewCalls',
        'linkedRenderPasses', 'linkedCommandBuffers', 'linkedProductSubmits']) {
        exact(failures, inactive && inactive[field], 0,
          'wg.webgpuProof inactiveTarget.' + field);
      }
    }
  }

  const negative = mode.name === 'no-draw' || mode.name === 'no-submit';
  const forwarded = mode.name !== 'no-submit';
  let firstReadback = null;
  let previousReadback = null;
  const readbacks = Object.create(null);
  for (const [field, label, sequence] of [
    ['baselineReadback', 'wg-baseline', 1],
    ['playingReadback', 'wg-playing', 2],
    ['restoredReadback', 'wg-restored', 3],
  ]) {
    const readback = value[field];
    check(failures, readback && !readback.error, 'wg.' + field + ' failed: ' +
      JSON.stringify(readback));
    if (!readback) continue;
    readbacks[field] = readback;
    exact(failures, readback.label, label, 'wg.' + field + '.label');
    exact(failures, readback.sequence, sequence, 'wg.' + field + '.sequence');
    exact(failures, readback.width, 320, 'wg.' + field + '.width');
    exact(failures, readback.height, 180, 'wg.' + field + '.height');
    exact(failures, readback.byteLength, 230400, 'wg.' + field + '.byteLength');
    check(failures, typeof readback.pixelSHA256 === 'string' &&
      /^[a-f0-9]{64}$/.test(readback.pixelSHA256),
    'wg.' + field + '.pixelSHA256 is invalid');
    exact(failures, readback.renderTargetKind, 'proof-private-gpu-texture',
      'wg.' + field + '.renderTargetKind');
    exact(failures, readback.canvasPresented, false,
      'wg.' + field + '.canvasPresented');
    for (const id of ['contextID', 'deviceID', 'queueID', 'targetID']) {
      check(failures, typeof readback[id] === 'string' && readback[id] !== '',
        'wg.' + field + '.' + id + ' is missing');
    }
    for (const linked of ['returnedToProduct', 'productRenderPassLinked',
      'productCommandBufferLinked', 'productQueueMatched']) {
      exact(failures, readback[linked], true, 'wg.' + field + '.' + linked);
    }
    exact(failures, readback.proofCommandKinds, ['copyTextureToBuffer'],
      'wg.' + field + '.proofCommandKinds');
    exact(failures, readback.nativeConfigureCalls, 0,
      'wg.' + field + '.nativeConfigureCalls');
    exact(failures, readback.nativeGetCurrentTextureCalls, 0,
      'wg.' + field + '.nativeGetCurrentTextureCalls');
    check(failures, Number.isInteger(readback.interceptedConfigureCalls) &&
      readback.interceptedConfigureCalls > 0,
    'wg.' + field + '.interceptedConfigureCalls is not positive');
    check(failures, Number.isInteger(readback.interceptedGetCurrentTextureCalls) &&
      readback.interceptedGetCurrentTextureCalls > 0,
    'wg.' + field + '.interceptedGetCurrentTextureCalls is not positive');
    exact(failures, readback.proofCopySequence, sequence,
      'wg.' + field + '.proofCopySequence');
    check(failures, Number.isInteger(readback.targetUsage) &&
      (readback.targetUsage & 0x01) === 0x01 && (readback.targetUsage & 0x10) === 0x10,
    'wg.' + field + '.targetUsage lacks COPY_SRC|RENDER_ATTACHMENT');
    if (!firstReadback) {
      firstReadback = readback;
      const targets = proof && Array.isArray(proof.configuredTargets)
        ? proof.configuredTargets : [];
      const target = targets.find((candidate) =>
        candidate && candidate.returnedToProduct === true);
      if (target) {
        for (const same of ['contextID', 'deviceID', 'queueID', 'targetID',
          'targetGeneration', 'configureSequence', 'format', 'targetUsage',
          'alphaMode', 'colorSpace', 'toneMapping']) {
          exact(failures, readback[same], target[same],
            'wg.' + field + '.configuredTarget.' + same);
        }
      }
    } else {
      for (const same of ['contextID', 'deviceID', 'queueID', 'targetID',
        'targetGeneration', 'configureSequence', 'format', 'targetUsage',
        'alphaMode', 'colorSpace', 'toneMapping']) {
        exact(failures, readback[same], firstReadback[same],
          'wg.' + field + '.' + same);
      }
    }
    if (previousReadback) {
      for (const increasing of ['getCurrentTextureSequence', 'targetViewSequence',
        'renderPassSequence', 'commandBufferSequence', 'productSubmitSequence']) {
        check(failures, Number.isInteger(readback[increasing]) &&
          readback[increasing] > previousReadback[increasing],
        'wg.' + field + '.' + increasing + ' did not strictly increase');
      }
    }
    previousReadback = readback;
    exact(failures, readback.productSubmissionForwarded, forwarded,
      'wg.' + field + '.productSubmissionForwarded');
    if (negative) {
      exact(failures, readback.foregroundPixels, 0,
        'wg.' + field + '.foregroundPixels');
    } else {
      check(failures, readback.foregroundPixels > 20,
        'wg.' + field + '.foregroundPixels is not > 20: ' + readback.foregroundPixels);
    }
  }
  if (readbacks.baselineReadback && readbacks.playingReadback &&
      readbacks.restoredReadback) {
    exact(failures, readbacks.restoredReadback.pixelSHA256,
      readbacks.baselineReadback.pixelSHA256,
      'wg.restoredReadback.pixelSHA256 exact baseline restore');
    if (negative) {
      exact(failures, readbacks.playingReadback.pixelSHA256,
        readbacks.baselineReadback.pixelSHA256,
        'wg mutation playing pixel SHA-256');
    } else {
      check(failures, readbacks.playingReadback.pixelSHA256 !==
        readbacks.baselineReadback.pixelSHA256,
      'wg positive playing pixel SHA-256 did not change');
    }
  }
  if (negative) {
    check(failures, value.playDiff && value.playDiff.dimsMatch === true &&
      value.playDiff.exactPixels === 0 && value.playDiff.exactBytes === 0,
    'wg mutation playback pixels were not exactly unchanged: ' +
      JSON.stringify(value.playDiff));
  } else {
    check(failures, value.playDiff && value.playDiff.dimsMatch === true &&
      value.playDiff.exactPixels > 20,
    'wg positive playback did not change mapped pixels: ' + JSON.stringify(value.playDiff));
  }
}

function checkTopLevel(failures, report, mode, browser) {
  exact(failures, report.mutation, mode.mutation || null, 'report.mutation');
  exact(failures, report.restoreAtomicGapMS, mode.gapMS, 'report.restoreAtomicGapMS');
  exact(failures, report.webgpuProofTarget, EXPECTED_PROOF_TARGET,
    'report.webgpuProofTarget');
  exact(failures, report.renderTargetKind, 'proof-private-gpu-texture',
    'report.renderTargetKind');
  exact(failures, report.canvasPresented, false, 'report.canvasPresented');
  exact(failures, report.nativeCaps, { webgl2: true, webgpu: true }, 'report.nativeCaps');
  exact(failures, report.capabilityWebGLContextReleased, true,
    'report.capabilityWebGLContextReleased');
  exact(failures, report.warnings, [], 'report.warnings');
  exact(failures, report.notFound, [], 'report.notFound');
  exact(failures, report.unexpectedRequests, [], 'report.unexpectedRequests');
  exact(failures, report.networkFailures, [], 'report.networkFailures');
  exact(failures, report.clientEventResponses, [
    { method: 'POST', path: '/_gosx/client-events', status: 204 },
    { method: 'POST', path: '/_gosx/client-events', status: 204 },
  ], 'report.clientEventResponses');
  exact(failures, report.intentionalNoContent, [
    { method: 'POST', path: '/_gosx/client-events', status: 204,
      cdpTerminal: 'loadingFailed:net::ERR_ABORTED' },
    { method: 'POST', path: '/_gosx/client-events', status: 204,
      cdpTerminal: 'loadingFailed:net::ERR_ABORTED' },
  ], 'report.intentionalNoContent');
  check(failures, !Object.prototype.hasOwnProperty.call(report, 'fatal'),
    'report unexpectedly contains fatal: ' + JSON.stringify(report.fatal));

  check(failures, report.chromeLaunch && typeof report.chromeLaunch === 'object',
    'report.chromeLaunch: missing');
  if (report.chromeLaunch && typeof report.chromeLaunch === 'object') {
    exact(failures, report.chromeLaunch.browserMode, 'headless',
      'report.chromeLaunch.browserMode');
    exact(failures, report.chromeLaunch.windowFlags, EXPECTED_CHROME_WINDOW_FLAGS,
      'report.chromeLaunch.windowFlags');
    exact(failures, report.chromeLaunch.waylandDisplay, null,
      'report.chromeLaunch.waylandDisplay');
    exact(failures, report.chromeLaunch.display, null, 'report.chromeLaunch.display');
    exact(failures, report.chromeLaunch.gpuFlags, EXPECTED_CHROME_GPU_FLAGS,
      'report.chromeLaunch.gpuFlags');
    exact(failures, report.chromeLaunch.stderrFile, 'chrome-stderr.log',
      'report.chromeLaunch.stderrFile');
    check(failures, Number.isInteger(report.chromeLaunch.stderrBytes) &&
      report.chromeLaunch.stderrBytes >= 0,
    'report.chromeLaunch.stderrBytes must be non-negative');
    exact(failures, report.chromeLaunch.stderrWriteError, '',
      'report.chromeLaunch.stderrWriteError');
  }
  check(failures, report.chromeDiagnostics &&
    typeof report.chromeDiagnostics === 'object', 'report.chromeDiagnostics: missing');
  if (report.chromeDiagnostics && typeof report.chromeDiagnostics === 'object') {
    exact(failures, report.chromeDiagnostics.scannedBytes,
      report.chromeLaunch && report.chromeLaunch.stderrBytes,
      'report.chromeDiagnostics.scannedBytes');
    check(failures,
      Number.isInteger(report.chromeDiagnostics.webgpuIntentionalTeardownStderrByte) &&
      report.chromeDiagnostics.webgpuIntentionalTeardownStderrByte >= 0 &&
      report.chromeDiagnostics.webgpuIntentionalTeardownStderrByte <=
        report.chromeDiagnostics.scannedBytes,
    'report.chromeDiagnostics.webgpuIntentionalTeardownStderrByte is invalid');
    exact(failures, report.chromeDiagnostics.swapFindings, [],
      'report.chromeDiagnostics.swapFindings');
    exact(failures, report.chromeDiagnostics.preTeardownLifecycleFindings, [],
      'report.chromeDiagnostics.preTeardownLifecycleFindings');
    exact(failures, report.chromeDiagnostics.scanError, '',
      'report.chromeDiagnostics.scanError');
    const maxByte = report.chromeDiagnostics.scannedBytes;
    check(failures, report.capabilityStderrRange &&
      Number.isInteger(report.capabilityStderrRange.startByte) &&
      Number.isInteger(report.capabilityStderrRange.afterTargetCloseByte) &&
      report.capabilityStderrRange.startByte >= 0 &&
      report.capabilityStderrRange.afterTargetCloseByte >=
        report.capabilityStderrRange.startByte &&
      report.capabilityStderrRange.afterTargetCloseByte <= maxByte,
    'report.capabilityStderrRange is invalid');
    exact(failures, Array.isArray(report.caseStderrRanges) &&
      report.caseStderrRanges.map((entry) => entry && entry.name),
    ['gl', 'wg'], 'report.caseStderrRanges names');
    if (Array.isArray(report.caseStderrRanges)) {
      for (const [index, entry] of report.caseStderrRanges.entries()) {
        check(failures, entry && Number.isInteger(entry.startByte) &&
          Number.isInteger(entry.beforeTargetCloseByte) && entry.startByte >= 0 &&
          entry.beforeTargetCloseByte >= entry.startByte &&
          entry.beforeTargetCloseByte <= maxByte,
        'report.caseStderrRanges[' + index + '] is invalid');
      }
    }
  }
  check(failures, report.browserGPU && typeof report.browserGPU === 'object',
    'report.browserGPU: missing');
  check(failures, report.capabilityAdapterInfo &&
    typeof report.capabilityAdapterInfo === 'object',
  'report.capabilityAdapterInfo: missing');
  exact(failures,
    String(report.capabilityAdapterInfo &&
      report.capabilityAdapterInfo.architecture || '').toLowerCase(),
    'swiftshader', 'report.capabilityAdapterInfo.architecture');
  check(failures, Array.isArray(report.browserGPU && report.browserGPU.devices) &&
    report.browserGPU.devices.some((device) => containsSwiftShader(device)),
  'report.browserGPU.devices do not identify SwiftShader');
  check(failures, String(report.browserGPU && report.browserGPU.auxAttributes &&
    report.browserGPU.auxAttributes.glImplementationParts || '')
    .toLowerCase().includes('angle=swiftshader'),
  'report.browserGPU ANGLE implementation is not SwiftShader');

  checkCDPBrowserIdentity(failures, report.selectedBrowser, browser,
    'report.selectedBrowser');

  const expectedErrors = NEGATIVE_ERRORS[mode.name] || [];
  exact(failures, report.errors, expectedErrors, 'report.errors');
  exact(failures, Array.isArray(report.cases) && report.cases.map((value) => value.name),
    ['gl', 'wg'], 'report.case order');
}

function verifyReport(report, mode, browser) {
  const failures = [];
  check(failures, report && typeof report === 'object', 'report is not an object');
  if (!report || typeof report !== 'object') return failures;
  checkTopLevel(failures, report, mode, browser);
  const gl = Array.isArray(report.cases) ? report.cases.find((value) => value.name === 'gl') : null;
  const wg = Array.isArray(report.cases) ? report.cases.find((value) => value.name === 'wg') : null;
  checkGL(failures, gl);
  checkWG(failures, wg, mode);
  return failures;
}

function verifyWrongProductReportMutation(browser) {
  const reportPath = path.join(ART, 'positive', 'report.json');
  const report = JSON.parse(fs.readFileSync(reportPath, 'utf8'));
  const baselineFailures = verifyReport(report, MODES[0], browser);
  if (baselineFailures.length !== 0) {
    throw new Error('positive report was not green before identity mutation: ' +
      JSON.stringify(baselineFailures));
  }
  const mutated = JSON.parse(JSON.stringify(report));
  const wrongProduct = 'NotChrome/' + browser.expectedVersion;
  mutated.selectedBrowser.product = wrongProduct;
  const mutationFailures = verifyReport(mutated, MODES[0], browser);
  if (mutationFailures.length !== 1 ||
      !mutationFailures[0].includes('report.selectedBrowser.product')) {
    throw new Error('same-version wrong-product report mutation was not rejected exactly: ' +
      JSON.stringify(mutationFailures));
  }
  return {
    mutation: 'wrong-product-same-version',
    sourceReport: path.relative(ART, reportPath),
    sourceReportSHA256: digestFile(reportPath),
    wrongProduct,
    rejected: true,
    verificationFailures: mutationFailures,
  };
}

function verifyWrongVersionReportMutation(browser) {
  const reportPath = path.join(ART, 'positive', 'report.json');
  const report = JSON.parse(fs.readFileSync(reportPath, 'utf8'));
  const baselineFailures = verifyReport(report, MODES[0], browser);
  if (baselineFailures.length !== 0) {
    throw new Error('positive report was not green before version mutation: ' +
      JSON.stringify(baselineFailures));
  }
  const mutated = JSON.parse(JSON.stringify(report));
  const wrongVersion = 'Chrome/0.0.0.0';
  mutated.selectedBrowser.product = wrongVersion;
  const mutationFailures = verifyReport(mutated, MODES[0], browser);
  if (mutationFailures.length !== 1 ||
      !mutationFailures[0].includes('report.selectedBrowser.product')) {
    throw new Error('wrong-version report mutation was not rejected exactly: ' +
      JSON.stringify(mutationFailures));
  }
  return {
    mutation: 'wrong-browser-version',
    sourceReport: path.relative(ART, reportPath),
    sourceReportSHA256: digestFile(reportPath),
    wrongVersion,
    rejected: true,
    verificationFailures: mutationFailures,
  };
}

function verifyTargetLinkageReportMutation(browser) {
  const reportPath = path.join(ART, 'positive', 'report.json');
  const report = JSON.parse(fs.readFileSync(reportPath, 'utf8'));
  const baselineFailures = verifyReport(report, MODES[0], browser);
  if (baselineFailures.length !== 0) {
    throw new Error('positive report was not green before target-linkage mutation: ' +
      JSON.stringify(baselineFailures));
  }
  const mutated = JSON.parse(JSON.stringify(report));
  const wg = mutated.cases.find((value) => value && value.name === 'wg');
  if (!wg || !wg.playingReadback || !Number.isInteger(wg.playingReadback.targetGeneration)) {
    throw new Error('positive report lacks target generation for linkage mutation');
  }
  wg.playingReadback.targetGeneration += 1;
  const mutationFailures = verifyReport(mutated, MODES[0], browser);
  if (mutationFailures.length !== 1 ||
      !mutationFailures[0].includes('wg.playingReadback.targetGeneration')) {
    throw new Error('wrong target-generation mutation was not rejected exactly: ' +
      JSON.stringify(mutationFailures));
  }
  return {
    mutation: 'wrong-private-target-generation',
    sourceReport: path.relative(ART, reportPath),
    sourceReportSHA256: digestFile(reportPath),
    rejected: true,
    verificationFailures: mutationFailures,
  };
}

function runMode(mode, browser) {
  return new Promise((resolve) => {
    const modeDir = path.join(ART, mode.name);
    fs.mkdirSync(modeDir, { recursive: true });
    const stdoutPath = path.join(modeDir, 'stdout.log');
    const stderrPath = path.join(modeDir, 'stderr.log');
    const stdoutFD = fs.openSync(stdoutPath, 'w');
    const stderrFD = fs.openSync(stderrPath, 'w');
    const env = Object.assign({}, process.env, {
      GOSX_CHROME_BIN: CHROME_BIN,
      GOSX_SCENE3D_CUBIC_WEBGPU_TARGET: EXPECTED_PROOF_TARGET,
    });
    delete env.GOSX_SCENE3D_CUBIC_MUTATION;
    delete env.GOSX_SCENE3D_CUBIC_RESTORE_ATOMIC_GAP_MS;
    delete env.GOSX_SCENE3D_CUBIC_BROWSER_MODE;
    if (mode.mutation) env.GOSX_SCENE3D_CUBIC_MUTATION = mode.mutation;
    if (mode.gapMS) env.GOSX_SCENE3D_CUBIC_RESTORE_ATOMIC_GAP_MS = String(mode.gapMS);

    const startedAt = new Date().toISOString();
    const startedMS = Date.now();
    let spawnError = null;
    let timedOut = false;
    let forceTimer = null;
    const child = spawn(process.execPath, [HARNESS, REPO, modeDir], {
      cwd: REPO, env, stdio: ['ignore', stdoutFD, stderrFD], windowsHide: true,
    });
    child.once('error', (error) => { spawnError = error; });
    const timer = setTimeout(() => {
      timedOut = true;
      child.kill('SIGTERM');
      forceTimer = setTimeout(() => child.kill('SIGKILL'), 5000);
    }, MODE_TIMEOUT_MS);

    child.once('close', (code, signal) => {
      clearTimeout(timer);
      if (forceTimer) clearTimeout(forceTimer);
      fs.closeSync(stdoutFD);
      fs.closeSync(stderrFD);
      const stdout = fs.readFileSync(stdoutPath, 'utf8');
      const stderr = fs.readFileSync(stderrPath, 'utf8');
      process.stdout.write('\n=== Scene3D browser mode: ' + mode.name + ' ===\n');
      if (stdout) process.stdout.write(stdout);
      if (stderr) process.stderr.write(stderr);

      const reportPath = path.join(modeDir, 'report.json');
      let report = null;
      let parseError = null;
      if (fs.existsSync(reportPath)) {
        try { report = JSON.parse(fs.readFileSync(reportPath, 'utf8')); }
        catch (error) { parseError = error; }
      }
      const verificationFailures = [];
      if (spawnError) verificationFailures.push('spawn error: ' + spawnError.message);
      if (timedOut) verificationFailures.push('mode exceeded ' + MODE_TIMEOUT_MS + 'ms');
      if (code !== mode.expectedExitCode || signal !== null) {
        verificationFailures.push('process exit: got code=' + code + ' signal=' + signal +
          ', want code=' + mode.expectedExitCode + ' signal=null');
      }
      if (!fs.existsSync(reportPath)) verificationFailures.push('report.json missing');
      if (parseError) verificationFailures.push('report.json parse failed: ' + parseError.message);
      if (report) verificationFailures.push(...verifyReport(report, mode, browser));
      const chromeStderrPath = path.join(modeDir, 'chrome-stderr.log');
      let chromeStderrBytes = null;
      let chromeStderrSHA256 = null;
      let chromeDiagnostics = null;
      if (!fs.existsSync(chromeStderrPath)) {
        verificationFailures.push('chrome-stderr.log missing');
      } else {
        const chromeStderrRaw = fs.readFileSync(chromeStderrPath);
        chromeStderrBytes = chromeStderrRaw.length;
        chromeStderrSHA256 = digestFile(chromeStderrPath);
        if (report && report.chromeLaunch &&
            report.chromeLaunch.stderrBytes !== chromeStderrBytes) {
          verificationFailures.push('chrome stderr byte receipt: got ' +
            report.chromeLaunch.stderrBytes + ', file has ' + chromeStderrBytes);
        }
        const boundary = report && report.chromeDiagnostics &&
          report.chromeDiagnostics.webgpuIntentionalTeardownStderrByte;
        chromeDiagnostics = scanChromeDiagnostics(chromeStderrRaw, boundary);
        if (chromeDiagnostics.swapFindings.length > 0) {
          verificationFailures.push('raw Chrome stderr contains forbidden swap/SharedImage ' +
            'diagnostics: ' + JSON.stringify(chromeDiagnostics.swapFindings));
        }
        if (chromeDiagnostics.preTeardownLifecycleFindings.length > 0) {
          verificationFailures.push('raw Chrome stderr contains pre-teardown WebGPU lifecycle ' +
            'diagnostics: ' +
            JSON.stringify(chromeDiagnostics.preTeardownLifecycleFindings));
        }
        if (report && !util.isDeepStrictEqual(report.chromeDiagnostics, chromeDiagnostics)) {
          verificationFailures.push('raw Chrome stderr diagnostic receipt mismatch: got ' +
            JSON.stringify(report.chromeDiagnostics) + ', recomputed ' +
            JSON.stringify(chromeDiagnostics));
        }
      }

      const entry = {
        name: mode.name,
        mutation: mode.mutation || null,
        restoreAtomicGapMS: mode.gapMS,
        expectedExitCode: mode.expectedExitCode,
        exitCode: code,
        signal,
        timedOut,
        startedAt,
        durationMS: Date.now() - startedMS,
        reportPath: path.relative(ART, reportPath),
        reportSHA256: fs.existsSync(reportPath) ? digestFile(reportPath) : null,
        chromeStderrPath: path.relative(ART, chromeStderrPath),
        chromeStderrBytes,
        chromeStderrSHA256,
        chromeDiagnostics,
        cdpBrowser: report && report.selectedBrowser || null,
        verified: verificationFailures.length === 0,
        verificationFailures,
      };
      resolve(entry);
    });
  });
}

(async () => {
  receipt.hostedClaimSourceContract = verifyHostedClaimSources();
  receipt.selectedBrowser = selectedBrowser();
  console.log('selected browser binary: ' + receipt.selectedBrowser.realPath);
  console.log('selected browser version: ' + receipt.selectedBrowser.invocation);
  console.log('expected browser version: ' + receipt.selectedBrowser.expectedVersion +
    ' (' + receipt.selectedBrowser.expectedVersionSource + ')');
  receipt.browserIdentitySelfCheck = browserIdentitySelfCheck(receipt.selectedBrowser);
  receipt.identitySelfCheckOnly = IDENTITY_SELF_CHECK_ONLY || null;
  writeReceipt();
  if (IDENTITY_SELF_CHECK_ONLY) {
    receipt.finishedAt = new Date().toISOString();
    receipt.verified = true;
    writeReceipt();
    console.log('browser identity wrong-product mutation rejected');
    return;
  }
  for (const mode of MODES) {
    const entry = await runMode(mode, receipt.selectedBrowser);
    receipt.modes.push(entry);
    if (!entry.verified) {
      receipt.errors.push(mode.name + ': ' + entry.verificationFailures.join('; '));
    }
    writeReceipt();
  }
  receipt.browserIdentityReportMutation = verifyWrongProductReportMutation(
    receipt.selectedBrowser);
  receipt.browserVersionReportMutation = verifyWrongVersionReportMutation(
    receipt.selectedBrowser);
  receipt.targetLinkageReportMutation = verifyTargetLinkageReportMutation(
    receipt.selectedBrowser);
  receipt.finishedAt = new Date().toISOString();
  receipt.verified = receipt.errors.length === 0 && receipt.modes.length === MODES.length;
  writeReceipt();
  if (!receipt.verified) {
    console.error('Scene3D browser-renderer proof matrix failed causal verification');
    for (const error of receipt.errors) console.error('  ' + error);
    process.exit(1);
  }
  console.log('Scene3D browser-renderer proof matrix verified all four modes');
})().catch((error) => {
  receipt.finishedAt = new Date().toISOString();
  receipt.verified = false;
  receipt.errors.push(String(error && error.stack || error));
  try { writeReceipt(); } catch (_writeError) {}
  console.error(error && error.stack || error);
  process.exit(1);
});
