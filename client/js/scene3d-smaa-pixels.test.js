"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const http = require("node:http");
const { spawn } = require("node:child_process");

// Optional shader-compile proof, skipped by default like the post-pixel test.
// It loads the built SMAA chunk into software-rendered Chrome and runs the three
// passes on both backends with the chunk's own pass objects: no shader or
// pipeline error, and the output differs from the input only beside an edge.
// Run with GOSX_POST_PIXEL_CHROME=<a Chrome or Chromium that can start>.
// The scene-mounted path and the error numbers are in e2e/scene3d_smaa_e2e_test.go.
test("SMAA passes compile on WebGPU and WebGL2 and change only edge pixels", {
  skip: !process.env.GOSX_POST_PIXEL_CHROME,
  timeout: 120000,
}, async () => {
  const chunk = fs.readFileSync(path.join(__dirname, "bootstrap-feature-scene3d-smaa.js"), "utf8");
  const server = http.createServer((_, res) => res.end("<!doctype html><title>smaa pixels</title>"));
  const profile = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-smaa-pixels-"));
  let chrome, socket;
  try {
    await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
    const url = "http://127.0.0.1:" + server.address().port;
    chrome = spawn(process.env.GOSX_POST_PIXEL_CHROME, ["--headless=new", "--no-sandbox",
      "--use-angle=swiftshader", "--enable-unsafe-webgpu", "--enable-unsafe-swiftshader",
      "--disable-dev-shm-usage", "--user-data-dir=" + profile, "--remote-debugging-port=0", url],
    { stdio: ["ignore", "ignore", "pipe"] });
    const endpoint = await new Promise((resolve, reject) => {
      let output = "";
      const timer = setTimeout(() => reject(new Error("Chrome DevTools did not start")), 20000);
      chrome.once("error", error => { clearTimeout(timer); reject(error); });
      chrome.once("exit", code => { clearTimeout(timer); reject(new Error("Chrome exited " + code + ": " + output)); });
      chrome.stderr.on("data", chunkData => {
        output += chunkData;
        const match = output.match(/DevTools listening on (ws:\/\/[^\s]+)/);
        if (match) { clearTimeout(timer); resolve(match[1]); }
      });
    });
    const targets = await (await fetch(endpoint.replace(/^ws:/, "http:").replace(/\/devtools\/browser\/.*/, "/json/list"))).json();
    socket = new WebSocket(targets.find(t => t.type === "page").webSocketDebuggerUrl);
    await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject; });
    let id = 0;
    const pending = new Map();
    socket.onmessage = event => {
      const message = JSON.parse(event.data);
      if (pending.has(message.id)) { pending.get(message.id)(message); pending.delete(message.id); }
    };
    const call = (method, params) => new Promise(resolve => {
      pending.set(++id, resolve); socket.send(JSON.stringify({ id, method, params }));
    });
    await call("Page.navigate", { url });
    for (let attempt = 0; attempt < 100; attempt++) {
      const ready = await call("Runtime.evaluate", { expression: "location.origin === " + JSON.stringify(url) + " && document.readyState === 'complete'" });
      if (ready.result?.result?.value) break;
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    const response = await call("Runtime.evaluate", { awaitPromise: true, returnByValue: true,
      expression: "(" + runBoth.toString() + ")(" + JSON.stringify({ chunk, width: 64, height: 48 }) + ")" });
    assert.ok(!response.result.exceptionDetails, JSON.stringify(response.result.exceptionDetails));
    const out = response.result.result.value;
    assert.deepEqual(out.errors, [], "no shader, pipeline or GL error");
    for (const backend of ["webgpu", "webgl"]) {
      const r = out[backend];
      assert.ok(r, backend + " ran");
      assert.equal(r.applied, true, backend + ": the three passes ran");
      assert.ok(r.changed >= 20, backend + ": SMAA changed edge pixels, got " + r.changed);
      assert.equal(r.farChanged, 0, backend + ": pixels away from every edge are unchanged");
    }
    assert.ok(out.maxBackendGap <= 3, "the two backends agree within 3 codes, got " + out.maxBackendGap);
  } finally {
    socket?.close();
    if (chrome && chrome.exitCode === null) {
      chrome.kill("SIGTERM");
      await new Promise(resolve => { const timer = setTimeout(() => { chrome.kill("SIGKILL"); resolve(); }, 3000); chrome.once("exit", () => { clearTimeout(timer); resolve(); }); });
    }
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
    fs.rmSync(profile, { recursive: true, force: true });
  }
});

// Runs in the page. The input is a shallow slanted edge between a light and a
// dark area, 8-bit display codes, rows from the top.
async function runBoth({ chunk, width, height }) {
  const errors = [];
  const origError = console.error;
  console.error = (...a) => { errors.push(a.join(" ")); };
  (0, eval)(chunk);
  const api = window.__gosx_scene3d_smaa_api;
  if (!api || !api.webgpu || !api.webgl) { errors.push("chunk did not register both backends"); return { errors }; }
  const code = (x, y) => (y < 14 + 0.15 * x ? 235 : 40);
  const rows = new Uint8Array(width * height * 4);
  for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
    const o = (y * width + x) * 4;
    rows[o] = rows[o + 1] = rows[o + 2] = code(x, y); rows[o + 3] = 255;
  }
  // Pixels within one pixel of an input edge may change; all others may not.
  const near = (x, y) => {
    for (let j = -2; j <= 2; j++) for (let i = -2; i <= 2; i++) {
      const a = code(x + i, y + j), b = code(x + i + 1, y + j), c = code(x + i, y + j + 1);
      if (a !== b || a !== c) return true;
    }
    return false;
  };
  const effect = { kind: "smaa" };
  const compare = (output, backend) => {
    let changed = 0, farChanged = 0;
    for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
      const o = (y * width + x) * 4;
      if (output[o] === rows[o] && output[o + 1] === rows[o + 1] && output[o + 2] === rows[o + 2]) continue;
      changed++;
      if (!near(x, y)) farChanged++;
    }
    return { applied: true, changed, farChanged, backend };
  };
  const result = { errors };

  // WebGPU.
  const adapter = await navigator.gpu.requestAdapter();
  if (!adapter) { errors.push("WebGPU adapter unavailable"); return result; }
  const device = await adapter.requestDevice();
  device.pushErrorScope("validation"); device.pushErrorScope("internal");
  const usage = GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.COPY_SRC | GPUTextureUsage.COPY_DST;
  const input = device.createTexture({ size: [width, height], format: "rgba8unorm", usage });
  const output = device.createTexture({ size: [width, height], format: "rgba8unorm", usage });
  device.queue.writeTexture({ texture: input }, rows, { bytesPerRow: width * 4 }, [width, height]);
  const pass = api.webgpu(device);
  const encoder = device.createCommandEncoder();
  const applied = pass.apply(effect, encoder, input.createView(), output.createView(), "rgba8unorm", width, height);
  const bytesPerRow = Math.ceil(width * 4 / 256) * 256;
  const readback = device.createBuffer({ size: bytesPerRow * height, usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ });
  encoder.copyTextureToBuffer({ texture: output }, { buffer: readback, bytesPerRow }, [width, height]);
  device.queue.submit([encoder.finish()]);
  await readback.mapAsync(GPUMapMode.READ);
  const mapped = new Uint8Array(readback.getMappedRange());
  const gpuOut = new Uint8Array(width * height * 4);
  for (let y = 0; y < height; y++) gpuOut.set(mapped.subarray(y * bytesPerRow, y * bytesPerRow + width * 4), y * width * 4);
  for (const scope of ["internal", "validation"]) {
    const e = await device.popErrorScope();
    if (e) errors.push("webgpu " + scope + ": " + e.message);
  }
  result.webgpu = { ...compare(gpuOut, "webgpu"), applied };

  // WebGL2. Framebuffer rows run bottom to top, so rows are flipped on upload and read back.
  const canvas = document.createElement("canvas");
  canvas.width = width; canvas.height = height;
  const gl = canvas.getContext("webgl2");
  if (!gl) { errors.push("WebGL2 unavailable"); return result; }
  const flipped = new Uint8Array(width * height * 4);
  for (let y = 0; y < height; y++) flipped.set(rows.subarray(y * width * 4, (y + 1) * width * 4), (height - 1 - y) * width * 4);
  const tex = (data) => {
    const t = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, t);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, width, height, 0, gl.RGBA, gl.UNSIGNED_BYTE, data);
    for (const [k, v] of [[gl.TEXTURE_MIN_FILTER, gl.NEAREST], [gl.TEXTURE_MAG_FILTER, gl.NEAREST], [gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE], [gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE]]) gl.texParameteri(gl.TEXTURE_2D, k, v);
    return t;
  };
  const inTex = tex(flipped), outTex = tex(null);
  const fbo = gl.createFramebuffer();
  gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
  gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, outTex, 0);
  const glPass = api.webgl(gl);
  const glApplied = glPass.apply(effect, inTex, { fbo, colorTex: outTex }, width, height, width, height);
  gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
  const raw = new Uint8Array(width * height * 4);
  gl.readPixels(0, 0, width, height, gl.RGBA, gl.UNSIGNED_BYTE, raw);
  const err = gl.getError();
  if (err) errors.push("gl error " + err);
  const glOut = new Uint8Array(width * height * 4);
  for (let y = 0; y < height; y++) glOut.set(raw.subarray((height - 1 - y) * width * 4, (height - y) * width * 4), y * width * 4);
  result.webgl = { ...compare(glOut, "webgl"), applied: glApplied };

  let gap = 0;
  for (let i = 0; i < glOut.length; i += 4) gap = Math.max(gap, Math.abs(glOut[i] - gpuOut[i]), Math.abs(glOut[i + 1] - gpuOut[i + 1]), Math.abs(glOut[i + 2] - gpuOut[i + 2]));
  result.maxBackendGap = gap;
  console.error = origError;
  return result;
}
