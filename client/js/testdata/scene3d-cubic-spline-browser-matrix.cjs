'use strict';
/* Bounded, causal four-mode verifier for the native Scene3D CUBICSPLINE
 * browser proof. One selected Chrome binary runs every mode:
 *
 *   positive      native WebGL2 + WebGPU presentation proof
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
const MODE_TIMEOUT_MS = 115000;
const MATRIX_REPORT = path.join(ART, 'matrix-report.json');

const MODES = [
  { name: 'positive', mutation: '', gapMS: 0, expectedExitCode: 0 },
  { name: 'gap100', mutation: '', gapMS: 100, expectedExitCode: 0 },
  { name: 'no-draw', mutation: 'webgpu-no-draw', gapMS: 0, expectedExitCode: 1 },
  { name: 'no-submit', mutation: 'webgpu-no-submit', gapMS: 0, expectedExitCode: 1 },
];

const NEGATIVE_ERRORS = {
  'no-draw': [
    '[wg] baseline mapped presentation contains 0 non-background pixels, expected > 20',
    '[wg] playing mapped presentation contains 0 non-background pixels, expected > 20',
    '[wg] playing frame changed 0 pixels, expected > 20',
    '[wg] restored mapped presentation contains 0 non-background pixels, expected > 20',
  ],
  'no-submit': [
    '[wg] baseline product queue submission was not forwarded',
    '[wg] baseline mapped presentation contains 0 non-background pixels, expected > 20',
    '[wg] playing product queue submission was not forwarded',
    '[wg] playing mapped presentation contains 0 non-background pixels, expected > 20',
    '[wg] playing frame changed 0 pixels, expected > 20',
    '[wg] restored product queue submission was not forwarded',
    '[wg] restored mapped presentation contains 0 non-background pixels, expected > 20',
  ],
};

function digestFile(file) {
  return crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
}

function versionNumber(value) {
  const match = String(value || '').match(/\b(\d+\.\d+\.\d+\.\d+)\b/);
  return match ? match[1] : '';
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
  const version = versionNumber(invocation);
  if (!version) throw new Error('selected Chrome version was not parseable: ' + invocation);
  return {
    configuredPath: CHROME_BIN,
    realPath: fs.realpathSync(CHROME_BIN),
    invocation,
    version,
  };
}

const receipt = {
  schemaVersion: 1,
  startedAt: new Date().toISOString(),
  nodeVersion: process.version,
  selectedBrowser: null,
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

  const negative = mode.name === 'no-draw' || mode.name === 'no-submit';
  const forwarded = mode.name !== 'no-submit';
  for (const [field, label, sequence] of [
    ['baselineReadback', 'wg-baseline', 1],
    ['playingReadback', 'wg-playing', 2],
    ['restoredReadback', 'wg-restored', 3],
  ]) {
    const readback = value[field];
    check(failures, readback && !readback.error, 'wg.' + field + ' failed: ' +
      JSON.stringify(readback));
    if (!readback) continue;
    exact(failures, readback.label, label, 'wg.' + field + '.label');
    exact(failures, readback.sequence, sequence, 'wg.' + field + '.sequence');
    exact(failures, readback.width, 320, 'wg.' + field + '.width');
    exact(failures, readback.height, 180, 'wg.' + field + '.height');
    exact(failures, readback.byteLength, 230400, 'wg.' + field + '.byteLength');
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
  exact(failures, report.nativeCaps, { webgl2: true, webgpu: true }, 'report.nativeCaps');
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

  const cdpProduct = report.selectedBrowser && report.selectedBrowser.product;
  exact(failures, versionNumber(cdpProduct), browser.version,
    'report.selectedBrowser.product version');
  check(failures, report.selectedBrowser &&
    typeof report.selectedBrowser.protocolVersion === 'string' &&
    typeof report.selectedBrowser.revision === 'string',
  'report.selectedBrowser is incomplete: ' + JSON.stringify(report.selectedBrowser));

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

function runMode(mode, browser) {
  return new Promise((resolve) => {
    const modeDir = path.join(ART, mode.name);
    fs.mkdirSync(modeDir, { recursive: true });
    const stdoutPath = path.join(modeDir, 'stdout.log');
    const stderrPath = path.join(modeDir, 'stderr.log');
    const stdoutFD = fs.openSync(stdoutPath, 'w');
    const stderrFD = fs.openSync(stderrPath, 'w');
    const env = Object.assign({}, process.env, { GOSX_CHROME_BIN: CHROME_BIN });
    delete env.GOSX_SCENE3D_CUBIC_MUTATION;
    delete env.GOSX_SCENE3D_CUBIC_RESTORE_ATOMIC_GAP_MS;
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
        cdpBrowser: report && report.selectedBrowser || null,
        verified: verificationFailures.length === 0,
        verificationFailures,
      };
      resolve(entry);
    });
  });
}

(async () => {
  receipt.selectedBrowser = selectedBrowser();
  console.log('selected browser binary: ' + receipt.selectedBrowser.realPath);
  console.log('selected browser version: ' + receipt.selectedBrowser.invocation);
  writeReceipt();
  for (const mode of MODES) {
    const entry = await runMode(mode, receipt.selectedBrowser);
    receipt.modes.push(entry);
    if (!entry.verified) {
      receipt.errors.push(mode.name + ': ' + entry.verificationFailures.join('; '));
    }
    writeReceipt();
  }
  receipt.finishedAt = new Date().toISOString();
  receipt.verified = receipt.errors.length === 0 && receipt.modes.length === MODES.length;
  writeReceipt();
  if (!receipt.verified) {
    console.error('Scene3D browser-proof matrix failed causal verification');
    for (const error of receipt.errors) console.error('  ' + error);
    process.exit(1);
  }
  console.log('Scene3D browser-proof matrix verified all four modes');
})().catch((error) => {
  receipt.finishedAt = new Date().toISOString();
  receipt.verified = false;
  receipt.errors.push(String(error && error.stack || error));
  try { writeReceipt(); } catch (_writeError) {}
  console.error(error && error.stack || error);
  process.exit(1);
});
