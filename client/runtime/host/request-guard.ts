// Request guards stay in the host: ordinary requests never cross into WASM.
// An inert wrapper can remain inside a third-party wrapper after disposal;
// forwarding therefore keeps its original function alive without Go callbacks.
(function () {
  "use strict";
  if (typeof window === "undefined" || !window.__gosx) return;
  const host = window.__gosx.host || (window.__gosx.host = {});
  const requests = host.requests || (host.requests = {});
  if (typeof requests.guard === "function") return;

  requests.guard = function (policy) {
    const paths = new Set((policy && policy.blockedPaths) || []);
    const installed = [];
    let active = true;

    function blocked(input) {
      if (!active || paths.size === 0 || input == null) return false;
      let raw = input;
      if (typeof input === "object") {
        if (typeof input.url === "string") raw = input.url;
        else if (typeof input.href === "string") raw = input.href;
      }
      try {
        // Use a neutral base so relative URL dot segments behave like fetch.
        // Policy deliberately matches paths across origins, including queries.
        const base = window.location && window.location.href || "http://localhost/";
        return paths.has(decodeURIComponent(new URL(String(raw), base).pathname));
      } catch (_) { return false; }
    }

    function install(target, name, beacon) {
      if (!target || typeof target[name] !== "function") return;
      const original = target[name];
      const wrapper = function (...args) {
        if (blocked(args[0])) {
          if (beacon) return true;
          // Return a real Response where available, preserving json/text/body
          // behavior for request callers that inspect more than status and ok.
          return Promise.resolve(typeof Response === "function"
            ? new Response(null, { status: 204 }) : { ok: true, status: 204 });
        }
        return Reflect.apply(original, this, args);
      };
      target[name] = wrapper;
      installed.push({ target, name, original, wrapper });
    }

    const dispose = function () {
      if (!active) return;
      active = false;
      for (const entry of installed) {
        if (entry.target[entry.name] === entry.wrapper) entry.target[entry.name] = entry.original;
      }
      installed.length = 0;
      paths.clear();
    };
    try {
      install(window, "fetch", false);
      install(window.__gosx, "request", false);
      install(window.navigator, "sendBeacon", true);
    } catch (error) { dispose(); throw error; }
    return { dispose };
  };
})();
