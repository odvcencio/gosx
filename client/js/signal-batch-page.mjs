// A real GoSX WASM runtime in Node, behind the page's own signal code. No DOM, island
// render or presenter is included. signal-batch.bench.mjs (time and heap) and
// signal-batch-depth.mjs (stack depth) both start from this.
//
// The page's code is cut out of the sources in `tree` (00-textlayout.ts has the shared
// signal store, 30b-tail-engine-mounting.ts the engine context), so a tree from another
// build brings its own page code.
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";

/**
 * Starts the runtime and returns the engine context the page hands to a worker or engine view.
 *
 * With `direct`, the runtime's exports are published on __gosx_set_input_batch, as the loaders
 * do, and the call fails if the runtime does not export gosx_batch_apply: the page would
 * otherwise fall back to the function without a word.
 *
 * @param {{wasm: string, wasmExec: string, tree: string, direct?: boolean, id?: string, applyPatches?: Function}} options
 */
export async function bootPage(options) {
  const noop = () => {};
  const fakeElement = () => ({ style: {}, dataset: {}, setAttribute: noop, getAttribute: () => null, appendChild: noop, addEventListener: noop, removeEventListener: noop, querySelector: () => null, querySelectorAll: () => [], classList: { add: noop, remove: noop, contains: () => false } });
  globalThis.window = globalThis;
  globalThis.document = {
    addEventListener: noop, removeEventListener: noop, createElement: fakeElement, getElementById: () => null, querySelector: () => null,
    querySelectorAll: () => [], body: fakeElement(), head: fakeElement(), documentElement: fakeElement(), readyState: "complete", hidden: false, dispatchEvent: noop,
  };
  globalThis.CustomEvent = class CustomEvent { constructor(type, init) { this.type = type; this.detail = init && init.detail; } };
  globalThis.__gosx_runtime_ready = noop;
  // The island patches the runtime sends; the bootstrap applies them to the DOM.
  if (options.applyPatches) globalThis.__gosx_apply_patches = options.applyPatches;
  vm.runInThisContext(fs.readFileSync(options.wasmExec, "utf8"), { filename: options.wasmExec });

  // The page's own code, cut out of the sources in the tree under test.
  const layout = fs.readFileSync(path.join(options.tree, "client/js/bootstrap-src/00-textlayout.ts"), "utf8");
  const classStart = layout.indexOf("  // The page's shared signal values.");
  const storeStart = classStart >= 0 ? classStart : layout.indexOf("  const gosxNamespace = window.__gosx || {};");
  const storeEndMarker = "  window.__gosx_subscribe_shared_signal = gosxSubscribeSharedSignal;\n";
  const storeEnd = layout.indexOf(storeEndMarker);
  if (storeStart < 0 || storeEnd < 0) throw new Error("shared signal store not found in " + options.tree);
  const storeApi = new Function("window", "document", "console",
    "const GOSX_VERSION='bench';\n" + layout.slice(storeStart, storeEnd + storeEndMarker.length) +
    "\nreturn {gosxReadSharedSignal, gosxNotifySharedSignal, gosxSubscribeSharedSignal, setSharedSignalValue};")(globalThis, globalThis.document, console);
  const mounting = fs.readFileSync(path.join(options.tree, "client/js/bootstrap-src/30b-tail-engine-mounting.ts"), "utf8");
  const contextSource = mounting.slice(mounting.indexOf("function createEngineContext("), mounting.indexOf("async function mountEngine("));
  const makeContext = new Function("window", "document", "CustomEvent", "console", "gosxReadSharedSignal", "gosxNotifySharedSignal", "gosxSubscribeSharedSignal", "setSharedSignalValue", "pendingEngineOwned", "goWASMEnginePageGeneration",
    contextSource + "\nreturn createEngineContext;")(globalThis, globalThis.document, globalThis.CustomEvent, console,
    storeApi.gosxReadSharedSignal, storeApi.gosxNotifySharedSignal, storeApi.gosxSubscribeSharedSignal, storeApi.setSharedSignalValue, () => true, 1);

  const go = new globalThis.Go();
  const { instance } = await WebAssembly.instantiate(fs.readFileSync(options.wasm), go.importObject);
  let crashed = "";
  go.run(instance).catch((error) => { crashed = String(error && error.message || error); });
  for (let waited = 0; waited < 300 && typeof globalThis.__gosx_set_input_batch !== "function"; waited++) await new Promise((r) => setTimeout(r, 10));
  if (typeof globalThis.__gosx_set_input_batch !== "function") throw new Error("the runtime did not register __gosx_set_input_batch");
  if (options.direct) {
    if (typeof instance.exports.gosx_batch_apply !== "function") throw new Error("--direct: the runtime does not export gosx_batch_apply");
    globalThis.__gosx_set_input_batch.exports = instance.exports;
  }

  const id = options.id || "bench";
  const ctx = makeContext({ id, component: "Bench" }, {}, {}, { required: [] }, { generation: 1, closed: false });
  globalThis.__gosx.engines.set(id, { context: ctx, disposed: false });
  return {
    ctx,
    instance,
    // The text of the error that ended the runtime, if it has ended.
    crashed: () => crashed,
    memoryMiB: () => Math.round(instance.exports.memory.buffer.byteLength / 1048576),
  };
}
