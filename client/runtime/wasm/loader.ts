// GoSX typed WASM loader and direct ABI handshake.
// @ts-check

/**
 * @typedef {object} GoSXRuntimeReference
 * @property {string} path
 * @property {number} [featureMask]
 * @property {string} [variant]
 *
 * @typedef {object} GoSXRuntimeLoader
 * @property {(runtimeRef: GoSXRuntimeReference) => Promise<void>} load
 * @property {() => object|null} handshake
 * @property {(requiredMask?: number) => boolean} supports
 */

(function() {
  "use strict";
  if (typeof window === "undefined" || window.__gosx_runtime_wasm_loader) return;

  function reportRuntimeIssue(runtimeRef, message, error) {
    console.error("[gosx] " + message, error || "");
    if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
      window.__gosx.reportIssue({
        scope: "bootstrap",
        type: "runtime",
        source: runtimeRef && runtimeRef.path,
        ref: runtimeRef && runtimeRef.path,
        message: message,
        error: error,
        fallback: "server",
      });
    }
  }

  /** @param {GoSXRuntimeReference} runtimeRef */
  async function load(runtimeRef) {
    if (typeof Go === "undefined") {
      reportRuntimeIssue(runtimeRef, "wasm_exec.js must be loaded before bootstrap.js");
      return;
    }
    const go = new Go();
    try {
      const response = await fetch(runtimeRef.path);
      if (!response.ok) throw new Error("runtime fetch failed with status " + response.status);
      const result = await instantiate(response, go.importObject);
      // The Go main function intentionally remains alive after this call.
      go.run(result.instance);
    } catch (error) {
      reportRuntimeIssue(runtimeRef, "failed to load WASM runtime", error);
    }
  }

  async function instantiate(response, importObject) {
    if (typeof WebAssembly.instantiateStreaming === "function") {
      try {
        return await WebAssembly.instantiateStreaming(response.clone(), importObject);
      } catch (_) {
        // Content-Type and older browser quirks fall back to bytes.
      }
    }
    return WebAssembly.instantiate(await response.arrayBuffer(), importObject);
  }

  function handshake() {
    const abi = window.__gosx_runtime_abi;
    if (!abi || typeof abi.handshake !== "function") return null;
    try {
      return abi.handshake();
    } catch (error) {
      reportRuntimeIssue(null, "runtime ABI handshake failed", error);
      return null;
    }
  }

  /** @param {number} [requiredMask] */
  function supports(requiredMask) {
    const current = handshake();
    const support = window.__gosx_runtime_abi_support;
    return !!(support && typeof support.validateHandshake === "function" && support.validateHandshake(current, requiredMask || 0));
  }

  /** @type {GoSXRuntimeLoader} */
  window.__gosx_runtime_wasm_loader = {
    load: load,
    handshake: handshake,
    supports: supports,
  };
})();
