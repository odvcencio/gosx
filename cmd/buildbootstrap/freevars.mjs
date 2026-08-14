// freevars.mjs — parse the concatenation of the given files as one script and
// report every identifier read that no enclosing scope declares. This is the
// definitive check that a lazily fetched chunk cannot throw ReferenceError for
// a symbol a split left behind.
//
// Usage:
//
//   node cmd/buildbootstrap/freevars.mjs <file...>
//   node cmd/buildbootstrap/freevars.mjs --chunk bootstrap-feature-scene3d.js
//
// --chunk reads client/js/bootstrap-src/chunks.json, the manifest
// cmd/buildbootstrap generates, so the file list cannot drift from the build.
//
// This is the acorn cross-check of the Go implementation in closure.go, which
// runs on every build and every --check. closure.go needs no npm. This script
// needs acorn; point ACORN_PATH at a checkout when the default lookup fails.
//
// Two classes of report are noise, and this script suppresses both:
//
//   1. A name the code tests with `typeof X === "function"` before it reads it.
//      `typeof` never throws for an undeclared name, so the pattern is a
//      deliberate optional-symbol probe, not a latent crash. Pass
//      --show-guarded to list them anyway; a guard can still hide a real loss
//      when the guarded path is the one that matters.
//   2. A name that only appears inside a comment. acorn drops comments, so the
//      parser never reports those. An earlier grep-based reading of this tool's
//      output raised two false alarms for exactly these two reasons.
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";

async function loadAcorn() {
  const candidates = [
    process.env.ACORN_PATH,
    "acorn",
    "/home/draco/work/gotreesitter-gophercon26/node_modules/acorn/dist/acorn.mjs",
  ].filter(Boolean);
  for (const candidate of candidates) {
    try {
      return await import(candidate);
    } catch (_err) {
      // try the next candidate
    }
  }
  try {
    return createRequire(import.meta.url)("acorn");
  } catch (_err) {
    console.error(
      "freevars.mjs needs acorn. Install it, or set ACORN_PATH to an acorn build.\n" +
      "The Go implementation in cmd/buildbootstrap/closure.go needs no npm and runs on every build.",
    );
    process.exit(2);
  }
}
const acorn = await loadAcorn();

const argv = process.argv.slice(2);
const showGuarded = argv.includes("--show-guarded");
let files = argv.filter((a) => !a.startsWith("--"));
const chunkFlag = argv.indexOf("--chunk");
if (chunkFlag !== -1) {
  const name = argv[chunkFlag + 1];
  const clientJS = path.resolve(path.dirname(new URL(import.meta.url).pathname), "..", "..", "client", "js");
  const manifest = JSON.parse(fs.readFileSync(path.join(clientJS, "bootstrap-src", "chunks.json"), "utf8"));
  const chunk = manifest.chunks.find((c) => c.name === name);
  if (!chunk) {
    console.error("unknown chunk:", name, "\nknown:", manifest.chunks.map((c) => c.name).join(", "));
    process.exit(2);
  }
  files = chunk.sources.map((rel) => path.join(clientJS, rel));
}
if (!files.length) {
  console.error("usage: freevars.mjs <file...> | --chunk <bundle-name> [--show-guarded]");
  process.exit(2);
}
let src = "";
const marks = [];
for (const f of files) {
  marks.push({ f, at: src.length });
  src += fs.readFileSync(f, "utf8") + "\n";
}
const fileOf = (pos) => {
  let cur = marks[0].f;
  for (const m of marks) if (m.at <= pos) cur = m.f;
  return cur;
};

const ast = acorn.parse(src, { ecmaVersion: 2022, sourceType: "script", allowReturnOutsideFunction: true });

const GLOBAL = new Set([
  "globalThis","window","document","navigator","console","Math","JSON","Object","Array","String","Number",
  "Boolean","Symbol","BigInt","Function","RegExp","Date","Error","EvalError","RangeError","ReferenceError",
  "SyntaxError","TypeError","URIError","AggregateError","Promise","Proxy","Reflect","Map","Set","WeakMap",
  "WeakSet","WeakRef","ArrayBuffer","SharedArrayBuffer","DataView","Atomics","Int8Array","Uint8Array",
  "Uint8ClampedArray","Int16Array","Uint16Array","Int32Array","Uint32Array","Float32Array","Float64Array",
  "BigInt64Array","BigUint64Array","Infinity","NaN","undefined","isNaN","isFinite","parseInt","parseFloat",
  "decodeURI","decodeURIComponent","encodeURI","encodeURIComponent","escape","unescape","eval","Intl",
  "setTimeout","clearTimeout","setInterval","clearInterval","setImmediate","queueMicrotask","structuredClone",
  "requestAnimationFrame","cancelAnimationFrame","requestIdleCallback","performance","fetch","Headers",
  "Request","Response","AbortController","AbortSignal","Blob","File","FileReader","FormData","URL",
  "URLSearchParams","TextEncoder","TextDecoder","atob","btoa","crypto","localStorage","sessionStorage",
  "history","location","screen","matchMedia","getComputedStyle","alert","confirm","prompt","open","close",
  "Image","Audio","Option","Event","CustomEvent","MessageChannel","MessagePort","BroadcastChannel",
  "EventTarget","Node","Element","HTMLElement","HTMLCanvasElement","HTMLImageElement","HTMLVideoElement",
  "HTMLMediaElement","HTMLScriptElement","CanvasRenderingContext2D","OffscreenCanvas",
  "OffscreenCanvasRenderingContext2D","ImageBitmap","createImageBitmap","ImageData","Path2D","DOMMatrix",
  "DOMPoint","DOMPointReadOnly","DOMRect","DOMRectReadOnly","MutationObserver","ResizeObserver",
  "IntersectionObserver","PerformanceObserver","ReportingObserver","WebSocket","EventSource","Worker",
  "SharedWorker","ServiceWorker","XMLHttpRequest","DOMParser","XMLSerializer","NodeFilter","NodeIterator",
  "TreeWalker","Range","Selection","CSS","CSSStyleSheet","FontFace","ResizeObserverEntry",
  "WebGLRenderingContext","WebGL2RenderingContext","WebGLBuffer","WebGLProgram","WebGLShader",
  "WebGLTexture","WebGLFramebuffer","WebGLRenderbuffer","WebGLUniformLocation","WebGLVertexArrayObject",
  "WebGLQuery","WebGLSync","WebGLSampler","WebGLTransformFeedback",
  "GPU","GPUAdapter","GPUDevice","GPUBuffer","GPUTexture","GPUSampler","GPUShaderModule",
  "GPUBufferUsage","GPUTextureUsage","GPUShaderStage","GPUMapMode","GPUColorWrite","GPUOutOfMemoryError",
  "GPUValidationError","GPUInternalError","GPUCanvasContext","GPUQueue","GPUCommandEncoder",
  "AudioContext","webkitAudioContext","AudioBuffer","GainNode","OscillatorNode","BiquadFilterNode",
  "DynamicsCompressorNode","StereoPannerNode","AudioBufferSourceNode","PannerNode","ConstantSourceNode",
  "MediaSource","SourceBuffer","VTTCue","TextTrack","TextTrackCue","ResizeObserverSize","WeakSet",
  "process","Buffer","require","module","exports","__dirname","__filename","arguments",
  "WebAssembly","FinalizationRegistry","Float16Array","OfflineAudioContext","AnalyserNode",
  "ManagedMediaSource","ReadableStream","WritableStream","TransformStream","CompressionStream",
  "DecompressionStream","DOMException","indexedDB","caches","cancelIdleCallback","self",
]);

// typeofGuarded collects every name the script feeds to the `typeof` operator.
// `typeof X` never throws for an undeclared X, so those names are optional by
// construction and must not be reported as latent ReferenceErrors.
const typeofGuarded = new Set();

// --- scope construction -----------------------------------------------------
function newScope(parent, kind) {
  return { parent, kind, vars: new Set(), funcScope: kind === "function" || kind === "global" };
}
function declare(scope, name, kind) {
  if (!name) return;
  if (kind === "var" || kind === "function") {
    let s = scope;
    while (s && !s.funcScope) s = s.parent;
    (s || scope).vars.add(name);
  } else {
    scope.vars.add(name);
  }
}
function bindPattern(scope, node, kind) {
  if (!node) return;
  switch (node.type) {
    case "Identifier": declare(scope, node.name, kind); break;
    case "ObjectPattern":
      for (const p of node.properties) {
        if (p.type === "RestElement") bindPattern(scope, p.argument, kind);
        else bindPattern(scope, p.value, kind);
      }
      break;
    case "ArrayPattern": for (const e of node.elements) bindPattern(scope, e, kind); break;
    case "AssignmentPattern": bindPattern(scope, node.left, kind); break;
    case "RestElement": bindPattern(scope, node.argument, kind); break;
    default: break;
  }
}

const misses = new Map();
const CHILD_KEYS = new WeakMap();
function childNodes(node) {
  const out = [];
  for (const k of Object.keys(node)) {
    if (k === "type" || k === "start" || k === "end" || k === "loc" || k === "range") continue;
    const v = node[k];
    if (Array.isArray(v)) { for (const c of v) if (c && typeof c.type === "string") out.push([k, c]); }
    else if (v && typeof v.type === "string") out.push([k, v]);
  }
  return out;
}

// Pre-pass: hoist declarations found directly in a scope body.
function hoist(scope, body) {
  const stack = [...body];
  while (stack.length) {
    const n = stack.shift();
    if (!n) continue;
    switch (n.type) {
      case "VariableDeclaration":
        for (const d of n.declarations) bindPattern(scope, d.id, n.kind);
        break;
      case "FunctionDeclaration":
        if (n.id) declare(scope, n.id.name, "function");
        continue; // do not descend: its body is its own scope
      case "ClassDeclaration":
        if (n.id) declare(scope, n.id.name, "let");
        continue;
      case "ImportDeclaration":
        for (const s of n.specifiers) declare(scope, s.local.name, "let");
        continue;
      default: break;
    }
    // descend through statements that share the scope for `var`
    for (const [, c] of childNodes(n)) {
      if (c.type === "FunctionDeclaration" || c.type === "FunctionExpression" ||
          c.type === "ArrowFunctionExpression" || c.type === "ClassDeclaration" ||
          c.type === "ClassExpression") {
        // `var` inside a nested function belongs to that function
        continue;
      }
      stack.push(c);
    }
  }
}

function walk(node, scope, parentKey) {
  if (!node) return;
  switch (node.type) {
    case "UnaryExpression": {
      if (node.operator === "typeof" && node.argument && node.argument.type === "Identifier") {
        typeofGuarded.add(node.argument.name);
      }
      walk(node.argument, scope, "argument");
      return;
    }
    case "Program": {
      hoist(scope, node.body);
      for (const s of node.body) walk(s, scope);
      return;
    }
    case "FunctionDeclaration":
    case "FunctionExpression":
    case "ArrowFunctionExpression": {
      const inner = newScope(scope, "function");
      if (node.type === "FunctionExpression" && node.id) inner.vars.add(node.id.name);
      inner.vars.add("arguments");
      for (const p of node.params) bindPattern(inner, p, "let");
      if (node.body.type === "BlockStatement") {
        hoist(inner, node.body.body);
        for (const s of node.body.body) walk(s, inner);
      } else {
        walk(node.body, inner);
      }
      return;
    }
    case "ClassDeclaration":
    case "ClassExpression": {
      const inner = newScope(scope, "block");
      if (node.id) inner.vars.add(node.id.name);
      if (node.superClass) walk(node.superClass, inner);
      walk(node.body, inner);
      return;
    }
    case "BlockStatement": {
      const inner = newScope(scope, "block");
      for (const s of node.body) {
        if (s.type === "FunctionDeclaration" && s.id) inner.vars.add(s.id.name);
        if (s.type === "ClassDeclaration" && s.id) inner.vars.add(s.id.name);
        if (s.type === "VariableDeclaration" && s.kind !== "var") {
          for (const d of s.declarations) bindPattern(inner, d.id, s.kind);
        }
      }
      for (const s of node.body) walk(s, inner);
      return;
    }
    case "ForStatement": case "ForInStatement": case "ForOfStatement": {
      const inner = newScope(scope, "block");
      const init = node.init || node.left;
      if (init && init.type === "VariableDeclaration") {
        for (const d of init.declarations) bindPattern(inner, d.id, init.kind === "var" ? "var" : init.kind);
      }
      for (const [k, c] of childNodes(node)) walk(c, inner, k);
      return;
    }
    case "CatchClause": {
      const inner = newScope(scope, "block");
      if (node.param) bindPattern(inner, node.param, "let");
      walk(node.body, inner);
      return;
    }
    case "MemberExpression": {
      walk(node.object, scope, "object");
      if (node.computed) walk(node.property, scope, "property");
      return;
    }
    case "Property": {
      if (node.computed) walk(node.key, scope, "key");
      walk(node.value, scope, "value");
      return;
    }
    case "PropertyDefinition": case "MethodDefinition": {
      if (node.computed) walk(node.key, scope, "key");
      walk(node.value, scope, "value");
      return;
    }
    case "LabeledStatement": { walk(node.body, scope); return; }
    case "BreakStatement": case "ContinueStatement": return;
    case "Identifier": {
      const name = node.name;
      let s = scope;
      while (s) { if (s.vars.has(name)) return; s = s.parent; }
      if (GLOBAL.has(name)) return;
      if (!misses.has(name)) misses.set(name, []);
      misses.get(name).push(node.start);
      return;
    }
    default: {
      for (const [k, c] of childNodes(node)) walk(c, scope, k);
      return;
    }
  }
}

const global = newScope(null, "global");
walk(ast, global);

// Report guarded names separately. They are not crashes, but a guard can hide
// a real loss: 16z-scene-webgpu-probe.ts reads loadManifest behind a guard, so
// dropping the file that declares it turned a crash into a silently wrong
// WebGPU adapter request.
const all = [...misses.keys()].sort();
const guarded = all.filter((n) => typeofGuarded.has(n));
const names = all.filter((n) => !typeofGuarded.has(n));

function line(n) {
  const pos = misses.get(n)[0];
  return `  ${n.padEnd(42)} x${String(misses.get(n).length).padStart(4)}  first at ${fileOf(pos)}`;
}

if (showGuarded && guarded.length) {
  console.log(`TYPEOF-GUARDED, not crashes (${guarded.length}):`);
  for (const n of guarded) console.log(line(n));
}
if (!names.length) {
  console.log("no free identifiers — chunk is closed over its own scope plus real globals");
  process.exit(0);
}
console.log(`FREE IDENTIFIERS (${names.length}):`);
for (const n of names) console.log(line(n));
process.exit(1);
