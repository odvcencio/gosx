// GoSX direct runtime ABI probe.
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

  const ABI_VERSION = 2;
  const MAILBOX_VERSION = 1;
  const FEATURE_CORE = 1 << 0;
  const FEATURE_ENGINE = 1 << 1;
  const FEATURE_COLLAB = 1 << 2;
  const FEATURE_SCENE3D = 1 << 3;
  const FEATURE_ISLANDS = 1 << 4;

  /** @param {string} variant */
  function featureMaskForVariant(variant) {
    switch (String(variant || "")) {
      case "core": return FEATURE_CORE | FEATURE_ISLANDS;
      case "engine": return FEATURE_CORE | FEATURE_ENGINE | FEATURE_SCENE3D;
      case "collab": return FEATURE_CORE | FEATURE_COLLAB | FEATURE_ISLANDS;
      case "islands": return FEATURE_CORE | FEATURE_ISLANDS;
      case "full": return FEATURE_CORE | FEATURE_ENGINE | FEATURE_COLLAB | FEATURE_SCENE3D | FEATURE_ISLANDS;
      default: return 0;
    }
  }

  /** @param {number} required */
  function selectVariant(required) {
    const mask = Number(required || 0) >>> 0;
    const candidates = ["core", "islands", "engine", "collab", "full"];
    for (const variant of candidates) {
      const features = featureMaskForVariant(variant);
      if ((features & mask) === mask) return variant;
    }
    return "";
  }

  /**
   * @param {GoSXRuntimeHandshakeProbe|null|undefined} handshake
   * @param {number} [requiredMask]
   */
  function validateHandshake(handshake, requiredMask) {
    const required = Number(requiredMask || 0) >>> 0;
    if (!handshake || Number(handshake.abiVersion) !== ABI_VERSION) return false;
    if (Number(handshake.mailboxVersion) !== MAILBOX_VERSION) return false;
    if (!featureMaskForVariant(handshake.variant)) return false;
    return ((Number(handshake.featureMask) >>> 0) & required) === required;
  }

  window.__gosx_runtime_abi_support = {
    abiVersion: ABI_VERSION,
    mailboxVersion: MAILBOX_VERSION,
    featureMaskForVariant: featureMaskForVariant,
    selectVariant: selectVariant,
    validateHandshake: validateHandshake,
    requiredFeatureMask: function(required) { return Number(required || 0) >>> 0; },
  };
})();
