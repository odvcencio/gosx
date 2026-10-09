"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const {
  FakeTextNode, createContext, runScript, flushAsyncWork, installManualRAF, makeFakeGPUDevice,
  disposeRuntimeTestContext, activeTestContexts, buildMinimalGLBBytes,
} = require("./runtime-test-harness.js");

const dir = path.join(__dirname, "testdata", "perf-loader");
const files = fs.readdirSync(dir).filter(name => name.endsWith(".json")).sort();
const sources = new Map();
const timeline = JSON.parse(fs.readFileSync(path.join(__dirname, "../../scene/testdata/timeline.json")));
const burst = JSON.parse(fs.readFileSync(path.join(__dirname, "../../scene/testdata/particle_burst.json")));
function body(asset) {
  if (!asset.source) return Buffer.from(asset.body, "base64");
  if (!sources.has(asset.source)) sources.set(asset.source, fs.readFileSync(path.join(__dirname, asset.source)));
  return sources.get(asset.source);
}

// This parser only materializes the generated server fixture vocabulary. It
// never selects scripts or invents manifest fields. Script text is raw HTML;
// URL attributes use HTML character references, just as in a browser.
function materialize(page, env) {
  const decode = value => value.replace(/&(amp|quot|lt|gt|#39|#34);/g, (_, key) => ({amp:"&",quot:'"',lt:"<",gt:">","#39":"'","#34":'"'}[key]));
  const stack = [env.document.documentElement];
  const scripts = [];
  const initial = new Set();
  const tokens = page.match(/<script\b[^>]*>[\s\S]*?<\/script\s*>|<[^>]*>|[^<]+/gi) || [];
  const loader = env.document.scriptLoader;
  env.document.scriptLoader = null;
  for (const token of tokens) {
    if (/^<!/i.test(token)) continue;
    if (/^<\//.test(token)) { if (stack.length > 1) stack.pop(); continue; }
    if (token[0] !== "<") { stack.at(-1).appendChild(new FakeTextNode(decode(token), env.document)); continue; }
    const match = token.match(/^<([\w-]+)([^>]*?)>/);
    if (!match) continue;
    const tag = match[1].toLowerCase();
    const node = tag === "html" ? env.document.documentElement : tag === "head" ? env.document.head : tag === "body" ? env.document.body : env.document.createElement(tag);
    for (const attr of match[2].matchAll(/([^\s=/>]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+)))?/g)) {
      node.setAttribute(attr[1], decode(attr[2] ?? attr[3] ?? attr[4] ?? ""));
    }
    if (!["html", "head", "body"].includes(tag)) stack.at(-1).appendChild(node);
    if (tag === "script") {
      node.textContent = token.slice(match[0].length).replace(/<\/script\s*>$/i, "");
      if (node.getAttribute("type") !== "application/json") scripts.push(node);
      if (node.getAttribute("src")) initial.add(node.getAttribute("src"));
    } else if (tag === "link") {
      const rel = node.getAttribute("rel");
      if (["preload", "prefetch", "modulepreload"].includes(rel)) initial.add(node.getAttribute("href"));
    } else if (!["meta", "link", "input", "br", "img", "source"].includes(tag)) stack.push(node);
  }
  env.document.scriptLoader = loader;
  return {scripts, initial};
}

async function settle(env) {
  for (let i = 0; i < 100; i++) {
    const count = env.fetchCalls.length;
    env.advance(16);
    await flushAsyncWork();
    if (env.context.__gosx?.ready === true && env.fetchCalls.length === count) {
      await flushAsyncWork();
      if (env.fetchCalls.length === count) return;
    }
  }
  assert.fail("production bootstrap did not reach ready: " + JSON.stringify(env.consoleLogs));
}

// Browser and native VM primitives use the existing harness. Loading decisions,
// feature registration, backend fallback and public command bridges remain
// the unmodified production implementations.
async function visit(pair, page) {
  let loseDevice;
  let gpuAvailable = pair.gpu === "usable";
  const lost = new Promise(resolve => { loseDevice = resolve; });
  const gpu = makeFakeGPUDevice({lost});
  gpu.device.features.add("texture-compression-bc");
  const fetchRoutes = {};
  for (const asset of pair.assets) {
    const bytes = body(asset);
    fetchRoutes[asset.url] = {text: bytes.toString(), bytes: [...bytes]};
  }
  fetchRoutes["/fixture.glb"] = {bytes: [...buildMinimalGLBBytes()]};
  fetchRoutes["/fixture.ktx2"] = {bytes: [...fs.readFileSync(path.join(__dirname, "../../render/bundle/ktx2/testdata/bc1.ktx2"))]};
  const env = createContext({
    fetchRoutes, enableWebGL: true, enableWebGL2: true,
    enableWebGPU: pair.gpu !== "absent", webgpuDevice: gpu.device,
    navigatorGPU: {requestAdapter: async () => gpuAvailable ? {requestDevice: async () => gpu.device, features: gpu.device.features, limits: gpu.device.limits} : null, getPreferredCanvasFormat: () => "rgba8unorm"},
    engineFactories: {Example: () => ({dispose() {}})},
  });
  // The shared fake DOM omits Node.isConnected. Presentation and recovery
  // use the browser property, so derive it from the actual fixture tree.
  const connected = node => {
    Object.defineProperty(node, "isConnected", {get() {
      let root = this;
      while (root.parentNode) root = root.parentNode;
      return root === env.document.documentElement;
    }});
    return node;
  };
  for (const node of [env.document.documentElement, env.document.head, env.document.body]) connected(node);
  const create = env.document.createElement.bind(env.document);
  env.document.createElement = tag => connected(create(tag));
  env.frameMS = 0;
  env.context.performance.now = () => env.frameMS;
  const timers = new Map();
  let timerID = 0;
  env.context.setInterval = (fn, ms) => {
    const id = ++timerID;
    timers.set(id, {fn, ms, next: env.frameMS + ms});
    return id;
  };
  env.context.clearInterval = id => timers.delete(id);
  env.advance = ms => {
    env.frameMS += ms;
    for (const timer of timers.values()) {
      if (timer.next <= env.frameMS) {
        timer.next = env.frameMS + timer.ms;
        timer.fn();
      }
    }
    env.raf.flush(env.frameMS);
  };

  env.context.GPUBufferUsage = {MAP_READ: 1, MAP_WRITE: 2, COPY_SRC: 4, COPY_DST: 8, INDEX: 16, VERTEX: 32, UNIFORM: 64, STORAGE: 128, INDIRECT: 256, QUERY_RESOLVE: 512};
  env.context.GPUTextureUsage = {COPY_SRC: 1, COPY_DST: 2, TEXTURE_BINDING: 4, STORAGE_BINDING: 8, RENDER_ATTACHMENT: 16};
  env.context.GPUShaderStage = {VERTEX: 1, FRAGMENT: 2, COMPUTE: 4};

  env.raf = installManualRAF(env.context);
  env.context.self = env.context;
  env.document.readyState = "loading";
  const {scripts, initial} = materialize(page, env);
  const manifest = JSON.parse(env.document.getElementById("gosx-manifest")?.textContent || "{}");
  if (manifest.runtime?.path) {
    env.context.__gosx.runtime.abi.handshake = () => ({abiVersion: env.context.__gosx_runtime_contract.abiVersion, mailboxVersion: env.context.__gosx_runtime_contract.mailboxVersion,
      manifestHash: manifest.runtime.manifestHash, variant: manifest.runtime.variant, featureMask: manifest.runtime.featureMask});
  }
  try {
    // Parser-started async loads precede defer execution; the emitted inline
    // navigator.gpu loader itself decides whether to insert its script.
    for (const script of scripts.filter(node => !node.hasAttribute("defer"))) {
      const url = script.getAttribute("src");
      runScript(url ? body(pair.assets.find(asset => asset.url === url)).toString() : script.textContent, env.context, url || "inline-loader.js");
    }
    for (const script of scripts.filter(node => node.hasAttribute("defer"))) {
      const url = script.getAttribute("src");
      const asset = pair.assets.find(asset => asset.url === url);
      assert.ok(asset, "emitted script is outside the graph: " + url);
      runScript(body(asset).toString(), env.context, url);
    }
    env.document.readyState = "complete";
    env.document.dispatchEvent(new env.context.CustomEvent("DOMContentLoaded"));
    await settle(env);
    if (manifest.engines?.some(entry => entry.component === "GoSXScene3D")) {
      const mount = env.document.querySelector("[data-gosx-scene3d-command-ready]");
      assert.ok(mount, "Scene3D did not mount before ready");
      assert.equal(mount.getAttribute("data-gosx-scene3d-backend"), pair.gpu === "usable" ? "webgpu" : "webgl", "browser boundary selected the wrong backend");
    }
    const runtimeURL = call => !/\/fixture\.(?:glb|ktx2)$/.test(String(call.url ?? call));
    const startup = new Set([...initial].filter(runtimeURL).concat(env.fetchCalls.filter(runtimeURL).map(call => String(call.url))));

    assert.deepEqual([...startup].sort(), pair.startup, "startup requests disagree");
    const boundary = env.fetchCalls.length;
    for (const trigger of pair.triggers) {
      const api = env.context.__gosx.scene3d;
      const target = env.document.querySelector("[data-gosx-scene3d-command-ready]");
      switch (trigger) {
        case "device-loss":
          gpuAvailable = false;
          loseDevice({reason: "unknown", message: "fixture"});
          // Production recovery is polled by its watchdog. Advance the browser
          // clock through that interval with adapter reacquisition unavailable.
          await flushAsyncWork();
          for (let i = 0; i < 12; i++) { env.advance(250); await flushAsyncWork(); }
          break;
        case "command": await api.dispatchCommands(target, []); break;
        case "timeline": await api.playTimeline(target, timeline); break;
        case "burst": await api.burstParticles(target, burst); break;
        case "instance-stream": {
          runScript("window.__perf_stream = new Uint8Array(" + JSON.stringify([...Buffer.from(pair.stream, "base64")]) + ");", env.context, "fixture-stream.js");
          const result = await env.context.__gosx_scene3d_apply_instance_stream_frame({instancedMeshes: [{id: "fixture", count: 0}]}, env.context.__perf_stream, () => {}, target);
          assert.equal(result.applied, true);
          break;
        }
        case "animation": await env.context.__gosx_ensure_scene3d_animation_loaded(); break;
        case "late-text": await env.context.__gosx_load_text_layout_engine(); break;
        default: assert.fail("unknown trigger " + trigger);
      }
      await settle(env);
    }
    const after = new Set(env.fetchCalls.slice(boundary).filter(runtimeURL).map(call => String(call.url)).filter(url => !startup.has(url)));
    assert.deepEqual([...after].sort(), pair.afterReady, "after-ready requests disagree");
    for (const url of pair.dormant) assert.ok(!startup.has(url) && !after.has(url), "dormant asset requested: " + url);
    assert.equal(env.consoleLogs.error.length, 0, JSON.stringify(env.consoleLogs.error));
  } finally {
    await disposeRuntimeTestContext(env);
    activeTestContexts.delete(env);
  }
}

test("production loader agrees with every rendered Go graph pair", async t => {
  assert.equal(files.length, 468, "fixed corpus size changed");
  for (const file of files) {
    const pair = JSON.parse(fs.readFileSync(path.join(dir, file)));
    await t.test(`${file} ${pair.variant} ${pair.base} ${pair.features.join("+")}`, {timeout: 30000}, async () => {
      await visit(pair, fs.readFileSync(path.join(dir, file.replace(/\.json$/, ".html")), "utf8"));
    });
  }
});
