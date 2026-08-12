// GoSX typed WASM loader. It verifies artifact bytes, gates the ready callback,
// and enforces exact manifest/variant/feature/ABI identity before hydration.
// @ts-check

/**
 * @typedef {object} GoSXRuntimeReference
 * @property {string} path
 * @property {string} hash
 * @property {string} manifestHash
 * @property {number} featureMask
 * @property {string} variant
 *
 * @typedef {object} GoSXRuntimeLoader
 * @property {(runtimeRef: GoSXRuntimeReference) => Promise<void>} load
 * @property {() => object|null} handshake
 * @property {(requiredMask?: number) => boolean} supports
 */

(function() {
  "use strict";
  if (typeof window === "undefined" || window.__gosx_runtime_wasm_loader) return;

  let activeLoad = null;

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

  function referenceError(runtimeRef) {
    const support = window.__gosx_runtime_abi_support;
    if (!support || typeof support.referenceError !== "function") return "generated runtime ABI support is missing";
    return support.referenceError(runtimeRef);
  }

  /** @param {GoSXRuntimeReference} runtimeRef */
  function load(runtimeRef) {
    if (activeLoad) return activeLoad;
    activeLoad = loadVerified(runtimeRef).finally(function() { activeLoad = null; });
    return activeLoad;
  }

  /** @param {GoSXRuntimeReference} runtimeRef */
  async function loadVerified(runtimeRef) {
    try {
      const refError = referenceError(runtimeRef);
      if (refError) throw new Error(refError);
      if (typeof Go === "undefined") throw new Error("wasm_exec.js must be loaded before bootstrap.js");

      const go = new Go();
      const response = await fetch(runtimeRef.path);
      if (!response.ok) throw new Error("runtime fetch failed with status " + response.status);
      const bytes = await response.clone().arrayBuffer();
      await verifyAssetHash(runtimeRef.hash, bytes);
      const result = await instantiate(response, bytes, go.importObject);
      await runUntilVerifiedReady(go, result.instance, runtimeRef);
    } catch (error) {
      reportRuntimeIssue(runtimeRef, "failed to load verified WASM runtime", error);
      throw error;
    }
  }

  async function verifyAssetHash(expectedHash, bytes) {
    const subtle = window.crypto && window.crypto.subtle;
    if (!subtle || typeof subtle.digest !== "function") throw new Error("Web Crypto SHA-256 is required to verify the runtime artifact");
    const digest = new Uint8Array(await subtle.digest("SHA-256", bytes));
    let actual = "";
    for (const value of digest) actual += value.toString(16).padStart(2, "0");
    const expected = String(expectedHash || "").toLowerCase();
    if (actual.slice(0, expected.length) !== expected) throw new Error("runtime asset hash mismatch");
  }

  async function instantiate(response, bytes, importObject) {
    if (typeof WebAssembly.instantiateStreaming === "function") {
      try {
        return await WebAssembly.instantiateStreaming(response, importObject);
      } catch (_) {
        // Content-Type and older browser quirks fall back to verified bytes.
      }
    }
    return WebAssembly.instantiate(bytes, importObject);
  }

  async function runUntilVerifiedReady(go, instance, runtimeRef) {
    const previousReady = window.__gosx_runtime_ready;
    let settled = false;
    let resolveReady;
    const readySignal = new Promise(function(resolve) { resolveReady = resolve; });
    window.__gosx_runtime_ready = function() {
      if (!settled) {
        settled = true;
        resolveReady();
      }
    };

    try {
      let runResult;
      try {
        runResult = go.run(instance);
      } catch (error) {
        throw error;
      }
      const runFailure = runResult && typeof runResult.then === "function"
        ? runResult.then(function() { throw new Error("WASM runtime exited before readiness"); }, function(error) { throw error; })
        : new Promise(function() {});
      const configuredTimeout = Number(window.__gosx_runtime_ready_timeout_ms);
      const timeoutMS = configuredTimeout > 0 ? configuredTimeout : 10000;
      const timeout = new Promise(function(_, reject) {
        setTimeout(function() { reject(new Error("WASM runtime did not signal readiness")); }, timeoutMS);
      });
      await Promise.race([readySignal, runFailure, timeout]);

      const current = handshake();
      const support = window.__gosx_runtime_abi_support;
      const validationError = support && typeof support.validationError === "function"
        ? support.validationError(current, runtimeRef)
        : "generated runtime ABI support is missing";
      if (validationError) throw new Error(validationError);
    } finally {
      window.__gosx_runtime_ready = previousReady;
    }

    if (typeof previousReady === "function") previousReady();
  }

  function handshake() {
    const abi = window.__gosx_runtime_abi;
    if (!abi || typeof abi.handshake !== "function") return null;
    return abi.handshake();
  }

  /** @param {number} [requiredMask] */
  function supports(requiredMask) {
    const current = handshake();
    const support = window.__gosx_runtime_abi_support;
    return !!(support && typeof support.validateHandshake === "function" && support.validateHandshake(current, requiredMask || 0));
  }

  /** @type {GoSXRuntimeLoader} */
  window.__gosx_runtime_wasm_loader = { load: load, handshake: handshake, supports: supports };
})();
