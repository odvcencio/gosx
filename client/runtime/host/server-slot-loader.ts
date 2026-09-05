// @ts-check
// Only projected islands request this separately verified host capability.
function ensureServerSlotHost(ref) {
  if (!ref || ref.version !== 1 || !/^[a-f0-9]{16}$/.test(ref.hash || "") || ref.path !== "/gosx/server-slots.js?v=" + ref.hash || !/^sha256-[A-Za-z0-9+/]{43}=$/.test(ref.integrity || "")) return Promise.reject(new Error("ServerSlot host descriptor unavailable or invalid"));
  const host = window.__gosx.host;
  const key = ref.hash + ":" + ref.integrity;
  if (host.serverSlotLoading) {
    if (host.serverSlotLoading.key !== key) return Promise.reject(new Error("ServerSlot host identity changed; reload required"));
    return host.serverSlotLoading.promise;
  }
  const pending = { key, promise: null, script: null, cancelled: false };
  host.serverSlotLoading = pending;
  pending.promise = new Promise((resolve, reject) => {
    let script, timer, settled = false;
    const fail = error => {
      if (settled) return;
      settled = true;
	  pending.cancelled = true;
      clearTimeout(timer);
      try { if (script) script.remove(); } catch (_) {}
      if (host.serverSlotLoading === pending) delete host.serverSlotLoading;
      reject(error);
    };
    try {
      timer = setTimeout(() => fail(new Error("ServerSlot host load timeout")), 15000);
      script = document.createElement("script");
	  pending.script = script;
      script.src = ref.path;
      script.integrity = ref.integrity;
      script.crossOrigin = "anonymous";
      script.referrerPolicy = "no-referrer";
      script.setAttribute("data-gosx-capability-hash", ref.hash);
      const nonceSource = document.querySelector("script[nonce]");
      if (nonceSource) script.nonce = nonceSource.nonce;
      script.onload = () => {
        if (settled) return;
        const api = host.serverSlots;
        if (!api || api.version !== 1 || api.hash !== ref.hash || typeof api.hydrate !== "function" || typeof api.validatePatches !== "function") { fail(new Error("ServerSlot host capability mismatch")); return; }
        settled = true; clearTimeout(timer); resolve(api);
      };
      script.onerror = () => fail(new Error("ServerSlot host load failed"));
      document.head.appendChild(script);
    } catch (error) { fail(error); }
  });
  return pending.promise;
}
