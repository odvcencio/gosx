  // Shared scalar and DOM primitives used by the runtime, engines, and Scene3D
  // chunks. Keep these independent of scene-only state so selective bootstrap
  // bundles can include them without pulling the Scene3D normalizer.

  function sceneBool(value, fallback) {
    if (typeof value === "boolean") return value;
    if (typeof value === "string") {
      const lowered = value.trim().toLowerCase();
      if (lowered === "true") return true;
      if (lowered === "false") return false;
    }
    return fallback;
  }

  function clearChildren(node) {
    while (node && node.firstChild) {
      node.removeChild(node.firstChild);
    }
  }
