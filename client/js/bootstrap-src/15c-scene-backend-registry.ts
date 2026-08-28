  // Scene3D backend registry. Backends register themselves when their file is
  // loaded; mount code asks the registry for candidates based on capabilities.

  var sceneBackendRegistry = (function() {
    const entries = [];
    const byKind = new Map();

    function register(kind, factory, options) {
      const normalizedKind = String(kind || "").trim();
      if (!normalizedKind) {
        throw new Error("scene backend kind is required");
      }
      const entry = normalizeBackendEntry(normalizedKind, factory, options);
      if (byKind.has(normalizedKind)) {
        const previous = byKind.get(normalizedKind);
        const index = entries.indexOf(previous);
        if (index >= 0) {
          entries.splice(index, 1, entry);
        }
      } else {
        entries.push(entry);
      }
      byKind.set(normalizedKind, entry);
      return entry;
    }

    function select(request) {
      const list = candidates(request);
      return list.length > 0 ? list[0] : null;
    }

    function candidates(request) {
      const req = request || {};
      const order = backendSelectionOrder(req);
      const out = [];
      for (const kind of order) {
        const entry = byKind.get(kind);
        if (!entry) {
          continue;
        }
        if (typeof entry.available === "function" && !entry.available(req)) {
          continue;
        }
        out.push(entry);
      }
      for (const entry of entries) {
        if (order.indexOf(entry.kind) >= 0) {
          continue;
        }
        if (!backendRequestAllowsKind(req, entry.kind)) {
          continue;
        }
        if (typeof entry.available === "function" && !entry.available(req)) {
          continue;
        }
        out.push(entry);
      }
      return out;
    }

    function list() {
      return entries.slice();
    }

    function dispose(kind) {
      if (kind == null) {
        entries.length = 0;
        byKind.clear();
        return;
      }
      const normalizedKind = String(kind || "").trim();
      const entry = byKind.get(normalizedKind);
      if (!entry) {
        return;
      }
      byKind.delete(normalizedKind);
      const index = entries.indexOf(entry);
      if (index >= 0) {
        entries.splice(index, 1);
      }
    }

    return {
      register,
      select,
      candidates,
      list,
      dispose,
    };
  })();

  function normalizeBackendEntry(kind, factory, options) {
    const opts = options || {};
    if (typeof factory === "function") {
      return {
        kind,
        capabilities: Array.isArray(opts.capabilities) ? opts.capabilities.slice() : [],
        create: factory,
        available: typeof opts.available === "function" ? opts.available : null,
        priority: sceneNumber(opts.priority, 0),
      };
    }
    const source = factory && typeof factory === "object" ? factory : {};
    return {
      kind,
      capabilities: Array.isArray(source.capabilities) ? source.capabilities.slice() : [],
      create: typeof source.create === "function" ? source.create : function() { return null; },
      available: typeof source.available === "function" ? source.available : null,
      priority: sceneNumber(source.priority, sceneNumber(opts.priority, 0)),
    };
  }

  function backendSelectionOrder(request) {
    const order = [];
    if (request.forceWebGL) {
      if (backendRequestAllowsKind(request, "webgl")) {
        order.push("webgl");
      }
      if (backendRequestAllowsKind(request, "canvas2d")) {
        order.push("canvas2d");
      }
      return order;
    }
    if (request.preferWebGPU && backendRequestAllowsKind(request, "webgpu")) {
      order.push("webgpu");
    }
    if (backendRequestAllowsKind(request, "webgl")) {
      order.push("webgl");
    }
    if (order.indexOf("webgpu") < 0 && backendRequestAllowsKind(request, "webgpu")) {
      order.push("webgpu");
    }
    if (backendRequestAllowsKind(request, "canvas2d")) {
      order.push("canvas2d");
    }
    return order;
  }

  function backendRequestAllowsKind(request, kind) {
    if (kind === "webgpu") {
      return request.webgpu !== false && (request.webgpu === true || request.preferWebGPU === true);
    }
    if (kind === "webgl") {
      return request.webgl !== false && (request.webgl === true || request.webgl2 === true);
    }
    if (kind === "canvas2d") {
      return request.canvas2d !== false && request.canvas !== false;
    }
    return request[kind] !== false;
  }

  if (window.__gosx_scene3d_api) {
    window.__gosx_scene3d_api.sceneBackendRegistry = sceneBackendRegistry;
  }
