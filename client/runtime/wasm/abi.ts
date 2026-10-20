// GoSX direct runtime ABI validation. Constants come exclusively from the
// generated runtime contract included immediately before this source.
// @ts-check

/**
 * @typedef {object} GoSXRuntimeHandshakeProbe
 * @property {number} abiVersion
 * @property {number} featureMask
 * @property {string} variant
 * @property {number} mailboxVersion
 * @property {string} manifestHash
 */

(function() {
  "use strict";
  if (typeof window === "undefined") return;

  const contract = window.__gosx_runtime_contract;
  if (!contract) {
    console.error("[gosx] generated runtime ABI contract is missing");
    return;
  }

  /** @param {string} variant */
  function featureMaskForVariant(variant) {
    const name = String(variant || "");
    return Number(contract.variants[name] || (contract.compatibilityVariants && contract.compatibilityVariants[name]) || 0) >>> 0;
  }

  /**
   * @param {number} required
   * @param {object} [published]
   */
  function selectVariant(required, published) {
    const mask = Number(required || 0) >>> 0;
    const candidates = Object.keys(contract.variants).filter(function(variant) {
      const features = featureMaskForVariant(variant);
      return (features & mask) === mask && (!published || published[variant]);
    });
    if (published) {
      candidates.sort(function(left, right) {
        const leftSize = Number(published[left] && published[left].size) || Number.MAX_SAFE_INTEGER;
        const rightSize = Number(published[right] && published[right].size) || Number.MAX_SAFE_INTEGER;
        if (leftSize !== rightSize) return leftSize - rightSize;
        return left < right ? -1 : left > right ? 1 : 0;
      });
    }
    return candidates[0] || "";
  }

  function referenceError(runtimeRef) {
    if (!runtimeRef || typeof runtimeRef.path !== "string" || runtimeRef.path === "") return "runtime path is missing";
    if (!/^[0-9a-f]{16,64}$/i.test(String(runtimeRef.hash || ""))) return "runtime asset hash is missing or invalid";
    const expectedFeatures = featureMaskForVariant(runtimeRef.variant);
    if (!expectedFeatures) return "runtime variant is missing or unknown";
    if ((Number(runtimeRef.featureMask) >>> 0) !== expectedFeatures) return "runtime reference feature mask does not match its variant";
    if (String(runtimeRef.manifestHash || "") !== contract.manifestHash) return "runtime manifest identity is missing or stale";
    return "";
  }

  /**
   * @param {GoSXRuntimeHandshakeProbe|null|undefined} handshake
   * @param {object|number} [expected]
   */
  function validationError(handshake, expected) {
    if (!handshake) return "runtime handshake is missing";
    if (Number(handshake.abiVersion) !== contract.abiVersion) return "runtime ABI version mismatch";
    if (Number(handshake.mailboxVersion) !== contract.mailboxVersion) return "runtime mailbox version mismatch";
    const declaredFeatures = featureMaskForVariant(handshake.variant);
    if (!declaredFeatures) return "runtime handshake variant is unknown";
    if ((Number(handshake.featureMask) >>> 0) !== declaredFeatures) return "runtime handshake feature mask does not match its variant";
    if (String(handshake.manifestHash || "") !== contract.manifestHash) return "runtime handshake manifest identity mismatch";

    if (expected && typeof expected === "object") {
      const refError = referenceError(expected);
      if (refError) return refError;
      if (String(handshake.variant) !== String(expected.variant)) return "runtime handshake variant does not match the manifest reference";
      if ((Number(handshake.featureMask) >>> 0) !== (Number(expected.featureMask) >>> 0)) return "runtime handshake feature mask does not match the manifest reference";
      if (String(handshake.manifestHash) !== String(expected.manifestHash)) return "runtime handshake manifest identity does not match the manifest reference";
      return "";
    }

    const required = Number(expected || 0) >>> 0;
    if ((declaredFeatures & required) !== required) return "runtime is missing required capabilities";
    return "";
  }

  gosxRuntime.support = {
    abiVersion: contract.abiVersion,
    mailboxVersion: contract.mailboxVersion,
    manifestHash: contract.manifestHash,
    featureMaskForVariant: featureMaskForVariant,
    selectVariant: selectVariant,
    referenceError: referenceError,
    validationError: validationError,
    validateHandshake: function(handshake, expected) { return validationError(handshake, expected) === ""; },
    requiredFeatureMask: function(required) { return Number(required || 0) >>> 0; },
  };
})();
