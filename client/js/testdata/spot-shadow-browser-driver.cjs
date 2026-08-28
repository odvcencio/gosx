'use strict';

// Dedicated native Chrome/CDP driver for the spot-shadow browser test.
// No test policy, no geometry constants, no state injection: only bounded
// transport, navigation, and screenshot plumbing.

const http = require('http');
const fs = require('fs');
const path = require('path');
const os = require('os');
const { spawn } = require('child_process');

const STEP_MS = 20000;
const ALLOWED_BASENAMES = new Set([
  'bootstrap.js',
  'bootstrap-feature-scene3d-webgl.js',
  'bootstrap-feature-scene3d-webgpu.js',
  'bootstrap-feature-scene3d-command.js',
  'bootstrap-feature-scene3d.js',
  'bootstrap-feature-scene3d-gltf.js',
  'bootstrap-feature-scene3d-animation.js',
]);

async function startDriver(opts) {
  const repoRoot = opts.repoRoot;
  const runtimeRoot = opts.runtimeRoot || path.join(repoRoot, 'client', 'js');
  const pages = opts.pages || {};
  const assets = opts.assets || {};
  const preload = opts.preload || '';
  const errors = [];
  const warnings = [];
  const notFound = [];

  const server = http.createServer((req, res) => {
    const url = (req.url || '/').split('?')[0];
    const base = url.slice(url.lastIndexOf('/') + 1);
    if (url === '/') {
      const body = '<!doctype html><html><head><meta charset="utf-8">' +
        '<link rel="icon" href="data:,"></head><body></body></html>';
      res.writeHead(200, { 'content-type': 'text/html', 'content-length': Buffer.byteLength(body) });
      res.end(body);
      return;
    }
    if (req.method === 'POST' && url === '/_gosx/client-events') {
      req.resume();
      req.on('end', () => {
        res.writeHead(204);
        res.end();
      });
      return;
    }
    if (Object.prototype.hasOwnProperty.call(pages, url)) {
      const body = Buffer.from(pages[url]);
      res.writeHead(200, { 'content-type': 'text/html', 'content-length': body.length });
      res.end(body);
      return;
    }
    if (Object.prototype.hasOwnProperty.call(assets, url)) {
      const asset = assets[url];
      const body = Buffer.from(asset.body);
      res.writeHead(200, {
        'content-type': asset.contentType || 'application/octet-stream',
        'content-length': body.length,
      });
      res.end(body);
      return;
    }
    if (ALLOWED_BASENAMES.has(base) && (url === '/' + base || url === '/gosx/' + base)) {
      const p = path.join(runtimeRoot, base);
      try {
        const body = fs.readFileSync(p);
        res.writeHead(200, { 'content-type': 'text/javascript', 'content-length': body.length });
        res.end(body);
        return;
      } catch (e) { /* fall through to 404 */ }
    }
    notFound.push(url);
    res.writeHead(404);
    res.end();
  });

  let ws = null;
  let chrome = null;
  let profile = null;
  let msgId = 0;
  const pending = new Map();
  const listeners = [];
  let closed = false;
  let closePromise = null;

  function cdpSend(method, params, sessionId, timeoutMs) {
    if (closed || !ws) return Promise.reject(new Error('CDP connection closed'));
    const id = ++msgId;
    return new Promise((resolve, reject) => {
      const t = setTimeout(() => { pending.delete(id); reject(new Error('CDP timeout: ' + method)); },
        timeoutMs || STEP_MS);
      pending.set(id, { resolve, reject, t });
      try {
        ws.send(JSON.stringify(Object.assign({ id, method }, params ? { params } : {},
          sessionId ? { sessionId } : {})));
      } catch (e) { clearTimeout(t); pending.delete(id); reject(e); }
    });
  }

  function settleEvent(entry, err, value) {
    const i = listeners.indexOf(entry);
    if (i >= 0) listeners.splice(i, 1);
    if (entry.timer) clearTimeout(entry.timer);
    if (err) entry.reject(err);
    else entry.resolve(value);
  }

  function waitForEvent(name, timeoutMs) {
    let entry;
    const promise = new Promise((resolve, reject) => {
      entry = { name, resolve, reject, timer: null };
      entry.timer = setTimeout(() => {
        settleEvent(entry, new Error('event timeout: ' + name));
      }, timeoutMs || STEP_MS);
      listeners.push(entry);
    });
    return {
      promise,
      cancel: () => {
        if (listeners.indexOf(entry) >= 0) {
          settleEvent(entry, new Error('event wait cancelled'));
        }
      },
    };
  }

  function dispatch(raw) {
    let m;
    try { m = JSON.parse(raw); } catch (e) { return; }
    if (m.id && pending.has(m.id)) {
      const p = pending.get(m.id);
      pending.delete(m.id);
      clearTimeout(p.t);
      if (m.error) p.reject(new Error(m.error.message));
      else if (m.result && m.result.exceptionDetails) {
        const d = m.result.exceptionDetails;
        p.reject(new Error('Runtime.evaluate exception: ' +
          ((d.exception && d.exception.description) || d.text)));
      } else p.resolve(m.result);
    } else if (m.method) {
      for (let i = listeners.length - 1; i >= 0; i -= 1) {
        if (listeners[i].name === m.method) {
          settleEvent(listeners[i], null, m.params || {});
        }
      }
      if (m.method === 'Runtime.consoleAPICalled' && m.params && m.params.args) {
        const text = m.params.args.map((x) => x.value !== undefined ? String(x.value) : (x.description || '')).join(' ');
        if (m.params.type === 'error') errors.push('console.error: ' + text);
        else if (m.params.type === 'warning') warnings.push('console.warning: ' + text);
      }
      if (m.method === 'Runtime.exceptionThrown' && m.params && m.params.exceptionDetails) {
        errors.push('page exception: ' + ((m.params.exceptionDetails.exception &&
          m.params.exceptionDetails.exception.description) || m.params.exceptionDetails.text));
      }
    }
  }

  function cleanup() {
    if (closePromise) return closePromise;
    closed = true;
    for (const p of Array.from(pending.values())) {
      clearTimeout(p.t);
      p.reject(new Error('CDP connection closed'));
    }
    pending.clear();
    for (const l of listeners.slice()) {
      if (l.timer) clearTimeout(l.timer);
      l.reject(new Error('event wait cancelled'));
    }
    listeners.length = 0;
    try { if (ws) ws.close(); } catch (e) {}
    ws = null;
    process.removeListener('SIGTERM', onSigterm);
    closePromise = (async () => {
      if (chrome) {
        const ch = chrome;
        chrome = null;
        const exited = new Promise((res) => {
          if (ch.exitCode !== null || ch.signalCode !== null) res();
          else ch.once('exit', res);
        });
        try { ch.kill('SIGKILL'); } catch (e) {}
        let fallbackTimer;
        await Promise.race([
          exited.then(() => true),
          new Promise((res) => { fallbackTimer = setTimeout(() => res(false), 5000); }),
        ]);
        clearTimeout(fallbackTimer);
      }
      if (profile) {
        const prof = profile;
        profile = null;
        try { fs.rmSync(prof, { recursive: true, force: true }); }
        catch (e) { warnings.push('profile cleanup skipped: ' + e.message); }
      }
      await new Promise((res) => {
        let done = false;
        const fin = () => { if (!done) { done = true; res(); } };
        const t = setTimeout(fin, 3000);
        try { server.close(() => { clearTimeout(t); fin(); }); }
        catch (e) { clearTimeout(t); fin(); }
      });
    })();
    return closePromise;
  }

  const onSigterm = () => { cleanup().then(() => process.exit(143), () => process.exit(1)); };
  process.on('SIGTERM', onSigterm);

  try {
    await new Promise((res, rej) => {
      server.once('error', rej);
      server.listen(0, '127.0.0.1', () => res());
    });
    const baseURL = 'http://127.0.0.1:' + server.address().port;

    profile = fs.mkdtempSync(path.join(os.tmpdir(), 'gosx-spot-shadow-driver-'));
    const chromeBin = process.env.GOSX_CHROME_BIN || '/usr/bin/google-chrome';
    if (!fs.existsSync(chromeBin)) throw new Error('chrome binary not found: ' + chromeBin);
    chrome = spawn(chromeBin, [
      '--headless=new', '--no-sandbox', '--use-gl=angle', '--use-angle=gl-egl',
      '--ignore-gpu-blocklist', '--enable-unsafe-swiftshader', '--enable-unsafe-webgpu',
      '--disable-dev-shm-usage', '--user-data-dir=' + profile,
      '--remote-debugging-port=0', 'about:blank',
    ], { stdio: ['ignore', 'ignore', 'pipe'] });

    const wsUrl = await new Promise((resolve, reject) => {
      let buf = '';
      const child = chrome; // stable handle; chrome may be nulled by cleanup
      let settled = false;
      let timer = null;
      let onExit = null;
      let onErr = null;
      const settle = (fn, val) => {
        if (settled) return;
        settled = true;
        if (timer) { clearTimeout(timer); timer = null; }
        if (onExit) child.removeListener('exit', onExit);
        if (onErr) child.removeListener('error', onErr);
        child.stderr.removeListener('data', onData);
        fn(val);
      };
      const onData = (d) => {
        if (settled) return;
        buf += d.toString();
        const m = buf.match(/ws:\/\/127\.0\.0\.1:\d+\/devtools\/browser\/[^\s]+/);
        if (m) settle(resolve, m[0]);
      };
      timer = setTimeout(() => settle(reject, new Error('no DevTools ws URL')), STEP_MS);
      onExit = () => settle(reject, new Error('chrome exited early: ' + buf));
      onErr = (e) => settle(reject, new Error('chrome spawn error: ' + e.message));
      child.stderr.on('data', onData);
      child.once('exit', onExit);
      child.once('error', onErr);
    });

    ws = new WebSocket(wsUrl);
    await new Promise((res, rej) => {
      const t = setTimeout(() => rej(new Error('ws connect timeout')), STEP_MS);
      ws.onopen = () => { clearTimeout(t); res(); };
      ws.onerror = () => { clearTimeout(t); rej(new Error('ws error')); };
    });
    ws.onmessage = (evData) => dispatch(evData.data);

    const { targetId } = await cdpSend('Target.createTarget', { url: 'about:blank' });
    const { sessionId } = await cdpSend('Target.attachToTarget', { targetId, flatten: true });
    const send = (method, params, to) => cdpSend(method, params, sessionId, to || STEP_MS);
    await send('Page.enable');
    await send('Runtime.enable');
    if (preload) await send('Page.addScriptToEvaluateOnNewDocument', { source: preload });

    async function evaluate(expression, awaitPromise) {
      const r = await send('Runtime.evaluate',
        { expression, returnByValue: true, awaitPromise: !!awaitPromise });
      return r && r.result ? r.result.value : undefined;
    }

    async function load(p) {
      const ev = waitForEvent('Page.loadEventFired', STEP_MS);
      const loaded = ev.promise;
      try {
        const [nav] = await Promise.all([
          send('Page.navigate', { url: baseURL + p }),
          loaded,
        ]);
        if (nav && nav.errorText) {
          throw new Error('navigate failed: ' + nav.errorText);
        }
      } finally {
        ev.cancel();
      }
    }

    async function capture(mountId) {
      const rect = await evaluate('(function(){var el=document.getElementById(' +
        JSON.stringify(mountId) + ');if(!el)return null;' +
        'var c=el instanceof HTMLCanvasElement?el:el.querySelector("canvas");' +
        'if(!c||!(c instanceof HTMLCanvasElement))return null;' +
        'var r=c.getBoundingClientRect();return {x:r.x,y:r.y,width:r.width,height:r.height};})()');
      if (!rect || !Number.isFinite(rect.width) || !Number.isFinite(rect.height) ||
          rect.width <= 0 || rect.height <= 0) {
        throw new Error('capture: canvas missing or zero-size for ' + JSON.stringify(mountId));
      }
      const shot = await send('Page.captureScreenshot', {
        format: 'png', captureBeyondViewport: false, clip: Object.assign({ scale: 1 }, rect),
      });
      return Buffer.from(shot.data, 'base64');
    }

    return {
      baseURL, send, eval: evaluate, load, capture, close: cleanup,
      errors, warnings, notFound,
    };
  } catch (e) {
    process.removeListener('SIGTERM', onSigterm);
    await cleanup();
    throw e;
  }
}

module.exports = { startDriver, STEP_MS };
