"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const http = require("node:http");
const { spawn } = require("node:child_process");
const { createBoardWebGPUHarness, makePointsBundle, flushAsyncWork } = require("./runtime-test-harness.js");

// Optional real-device proof; the normal Node suite still checks target formats
// and pass counts. Run with GOSX_POST_PIXEL_CHROME=/usr/bin/google-chrome.
test("direct WebGPU post output matches HDR effect plus present byte for byte", {
  skip: !process.env.GOSX_POST_PIXEL_CHROME,
  timeout: 120000,
}, async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  h.canvas.width = h.canvas.height = 64;
  const bundle = makePointsBundle({ id: "p", count: 1, positions: [0, 0, 0] });
  const shaders = [];
  try {
    for (const kind of ["toneMapping", "fxaa", "vignette", "colorGrade"]) {
      for (const direct of [false, true]) {
        bundle.postEffects = [{ kind }, ...direct ? [] : [{ kind: "unknown" }]];
        for (let frame = 0; frame < 10; frame++) {
          h.renderer.render(bundle, { width: 64, height: 64 });
          await flushAsyncWork();
        }
      }
      const matching = h.fake.state.renderPipelines.filter(p => p.desc.label === "gosx-post-" + kind || p.desc.label === "gosx-post-canvas-" + kind);
      const hdr = matching.find(p => p.desc.fragment.targets[0].format === "rgba16float");
      const direct = matching.find(p => p.desc.fragment.targets[0].format !== "rgba16float");
      assert.ok(hdr && direct, "both variants exist for " + kind);
      shaders.push({ kind, vertex: hdr.desc.vertex.module.code,
        hdr: hdr.desc.fragment.module.code, direct: direct.desc.fragment.module.code });
    }
    const blit = h.fake.state.renderPipelines.find(p => p.desc.label === "gosx-post-present").desc.fragment.module.code;
    const server = http.createServer((_, res) => res.end("<!doctype html><title>post pixels</title>"));
    const profile = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-frame-pixels-"));
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
        chrome.stderr.on("data", chunk => {
          output += chunk;
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
      // Half-float input includes HDR values, edges, and small values. Compare
      // the actual old two-pass path with the shipped direct shader on Chrome.
      for (const [width, height] of [[64, 64], [63, 47], [640, 480]]) {
        h.canvas.width = width;
        h.canvas.height = height;
        const values = Array.from({ length: width * height * 4 }, (_, i) =>
          i % 4 === 3 ? 1 : ((i * 7919 % 16381) / 4096));
        const input = Array.from(new Uint16Array(new Float16Array(values).buffer));
        const selected = [];
        for (const shader of shaders) {
          bundle.postEffects = [{ kind: shader.kind }];
          h.renderer.render(bundle, { width, height });
          await flushAsyncWork();
          const label = h.fake.state.renderPasses.at(-1).draws[0].pipeline.desc.label;
          const useDirect = label === "gosx-post-canvas-" + shader.kind;
          assert.ok(useDirect || label === "gosx-post-present");
          selected.push({ ...shader, useDirect });
        }
        const response = await call("Runtime.evaluate", { awaitPromise: true, returnByValue: true,
          expression: "(" + comparePixels.toString() + ")(" + JSON.stringify({ shaders: selected, blit, input, width, height }) + ")" });
        assert.ok(!response.result.exceptionDetails, JSON.stringify(response.result.exceptionDetails));
        assert.deepEqual(response.result.result.value, shaders.map(s => ({ kind: s.kind, differentBytes: 0 })), `${width}x${height}`);
      }
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
  } finally { h.renderer.dispose(); }
});

async function comparePixels({ shaders, blit, input, width, height }) {
  const adapter = await navigator.gpu.requestAdapter();
  if (!adapter) throw new Error("WebGPU adapter unavailable");
  const device = await adapter.requestDevice();
  const texture = format => device.createTexture({ size: [width, height], format,
    usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.COPY_SRC | GPUTextureUsage.COPY_DST });
  const source = texture("rgba16float");
  device.queue.writeTexture({ texture: source }, new Uint16Array(input), { bytesPerRow: width * 8 }, [width, height]);
  const sampler = device.createSampler({ minFilter: "linear", magFilter: "linear" });
  const results = [];
  device.pushErrorScope("validation");
  for (const shader of shaders) {
    const hdr = texture("rgba16float"), old = texture("rgba8unorm"), direct = texture("rgba8unorm");
    const params = device.createBuffer({ size: 16, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
    device.queue.writeBuffer(params, 0, new Float32Array(shader.kind === "colorGrade" ? [1.3, 0.9, 0.8, 0] : [1.3, 1, 0, 0]));
    const pipeline = (code, format) => device.createRenderPipelineAsync({ layout: "auto",
      vertex: { module: device.createShaderModule({ code: shader.vertex }), entryPoint: "vertexMain" },
      fragment: { module: device.createShaderModule({ code }), entryPoint: "fragmentMain", targets: [{ format }] },
      primitive: { topology: "triangle-strip" } });
    const encode = (encoder, pipeline, input, output, withParams) => {
      const entries = [{ binding: 0, resource: input.createView() }, { binding: 1, resource: sampler }];
      if (withParams) entries.push({ binding: 2, resource: { buffer: params } });
      const group = device.createBindGroup({ layout: pipeline.getBindGroupLayout(0), entries });
      const pass = encoder.beginRenderPass({ colorAttachments: [{ view: output.createView(), loadOp: "clear", storeOp: "store" }] });
      pass.setPipeline(pipeline); pass.setBindGroup(0, group); pass.draw(4); pass.end();
    };
    const [effect, present, final] = await Promise.all([pipeline(shader.hdr, "rgba16float"), pipeline(blit, "rgba8unorm"), pipeline(shader.direct, "rgba8unorm")]);
    const encoder = device.createCommandEncoder();
    encode(encoder, effect, source, hdr, shader.kind !== "fxaa");
    encode(encoder, present, hdr, old, false);
    if (shader.useDirect) encode(encoder, final, source, direct, shader.kind !== "fxaa");
    else encode(encoder, present, hdr, direct, false);
    const readbacks = [old, direct].map(texture => {
      const buffer = device.createBuffer({ size: Math.ceil(width / 64) * 256 * height, usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ });
      encoder.copyTextureToBuffer({ texture }, { buffer, bytesPerRow: Math.ceil(width / 64) * 256 }, [width, height]); return buffer;
    });
    device.queue.submit([encoder.finish()]);
    await Promise.all(readbacks.map(buffer => buffer.mapAsync(GPUMapMode.READ)));
    const bytes = readbacks.map(buffer => new Uint8Array(buffer.getMappedRange()));
    results.push({ kind: shader.kind, differentBytes: bytes[0].reduce((n, value, i) => n + (value !== bytes[1][i]), 0) });
    for (const buffer of readbacks) buffer.destroy();
    for (const target of [hdr, old, direct]) target.destroy();
    params.destroy();
  }
  const error = await device.popErrorScope();
  if (error) throw new Error(error.message);
  device.destroy();
  return results;
}
