// @ts-check
// GoSX browser host: declarative headless controllers.
  // --------------------------------------------------------------------------
  // Declarative headless controllers
  // --------------------------------------------------------------------------

  function controllerList(config, key = "controllers") {
    return config && Array.isArray(config[key]) ? config[key] : [];
  }

  function controllerOutputMap(config) {
    const outputs = Object.create(null);
    const list = controllerList(config, "outputs");
    for (const output of list) {
      if (!output || (!output.signal && !output.event)) continue;
      const name = String(output.name || output.signal || output.event || "").trim();
      if (!name) continue;
      outputs[name] = output;
    }
    return outputs;
  }

  function controllerPublish(record, output, payload) {
    controllerSetValue(record, output, Object.assign({
      controllerId: record.id, controller: record.name, at: Date.now(),
    }, payload || {}));
  }

  function controllerSetValue(record, output, value) {
    if (record.disposed) return;
    const name = String(output || "").trim(), spec = record.outputs[name];
    const signal = spec ? String(spec.signal || "").trim() : name;
    if (spec && spec.event) record.root.dispatchEvent(new CustomEvent(spec.event, { detail: value, bubbles: true }));
    if (!signal) return;
    try {
      const result = setSharedSignalValue(signal, value);
      if (typeof result === "string" && result !== "") throw new Error(result);
    } catch (error) {
      console.error("[gosx] controller signal error (" + record.id + "/" + signal + "):", error);
    }
  }

  function controllerEmitBinding(record, binding, payload) {
    if (binding.project) {
      if (record.project) record.project(binding, payload);
    } else controllerPublish(record, binding.output, payload);
  }

  function controllerRoot(selector) {
    const value = String(selector || "").trim();
    if (!value) return document;
    try {
      return document.querySelector(value) || document;
    } catch (_error) {
      return document;
    }
  }

  function controllerAddListener(record, target, type, listener, options) {
    if (!target || typeof target.addEventListener !== "function" || !type) return;
    target.addEventListener(type, listener, options);
    record.listeners.push([target, type, listener, options]);
  }

  function controllerDisposeRecord(record) {
    if (!record || record.disposed) return;
    record.disposed = true;
    for (const binding of record.listeners.splice(0)) {
      try { binding[0].removeEventListener(binding[1], binding[2], binding[3]); } catch (_error) {}
    }
    for (const unsubscribe of record.unsubscribers.splice(0)) {
      try { unsubscribe(); } catch (_error) {}
    }
    for (const timer of record.timers.splice(0)) {
      try { clearInterval(timer); clearTimeout(timer); } catch (_error) {}
    }
    for (const controller of record.abortControllers.splice(0)) {
      try { controller.abort(); } catch (_error) {}
    }
  }

  function controllerEventTarget(root, event) {
    const target = event && event.target;
    if (!target || root === document || root === window) return target || null;
    return root.contains && root.contains(target) ? target : null;
  }

  function controllerMatchesTarget(root, target, selector) {
    const value = String(selector || "").trim();
    if (!value) return root === document || root === window ? target : root;
    if (!target || typeof target.closest !== "function") return null;
    try {
      const matched = target.closest(value);
      if (!matched) return null;
      if (root && root !== document && root !== window && root.contains && !root.contains(matched)) {
        return null;
      }
      return matched;
    } catch (_error) {
      return null;
    }
  }

  function controllerEditableTarget(target) {
    if (!target) return false;
    const tag = String(target.tagName || "").toLowerCase();
    if (tag === "input" || tag === "textarea" || tag === "select") return true;
    if (target.isContentEditable) return true;
    return Boolean(target.closest && target.closest("[contenteditable=true],[contenteditable='']"));
  }

  function controllerEventPayload(event, matched) {
    const target = matched || (event && event.target) || null;
    const payload = {
      type: String(event && event.type || ""),
      key: event && event.key,
      code: event && event.code,
      altKey: Boolean(event && event.altKey),
      ctrlKey: Boolean(event && event.ctrlKey),
      metaKey: Boolean(event && event.metaKey),
      shiftKey: Boolean(event && event.shiftKey),
      repeat: Boolean(event && event.repeat),
      detail: event && event.detail,
      pointerId: event && event.pointerId,
      clientX: event && event.clientX,
      clientY: event && event.clientY,
    };
    if (target) {
      payload.target = {
        dataset: Object.assign({}, target.dataset || {}),
        id: String(target.id || ""),
        name: String(target.name || ""),
        value: target.value !== undefined ? target.value : undefined,
        checked: target.checked !== undefined ? Boolean(target.checked) : undefined,
        text: target.textContent !== undefined ? String(target.textContent || "") : undefined,
      };
    }
    return payload;
  }

  function controllerKeyMatches(binding, event) {
    const wantedEvent = String(binding.event || "keydown").toLowerCase();
    if (wantedEvent !== String(event.type || "").toLowerCase()) return false;
    const key = String(binding.key || "").toLowerCase();
    const code = String(binding.code || "");
    if (key && key !== String(event.key || "").toLowerCase()) return false;
    if (code && code !== String(event.code || "")) return false;
    const modifiers = Array.isArray(binding.modifiers) ? binding.modifiers : [];
    for (const mod of modifiers) {
      switch (String(mod || "").toLowerCase()) {
        case "alt":
          if (!event.altKey) return false;
          break;
        case "ctrl":
        case "control":
          if (!event.ctrlKey) return false;
          break;
        case "meta":
        case "cmd":
        case "command":
          if (!event.metaKey) return false;
          break;
        case "shift":
          if (!event.shiftKey) return false;
          break;
      }
    }
    return Boolean(key || code);
  }

  function controllerInstallOutputs(record) {
    const outputs = controllerList(record.config, "outputs");
    for (const output of outputs) {
      if (!output || !output.signal || !Object.prototype.hasOwnProperty.call(output, "initial")) continue;
      controllerSetValue(record, output.name || output.signal, output.initial);
    }
  }

  function controllerInstallInputs(record) {
    const inputs = controllerList(record.config, "inputs");
    for (const input of inputs) {
      if (!input || !input.signal) continue;
      const name = String(input.name || input.signal).trim();
      const output = String(input.output || "").trim();
      const unsub = gosxSubscribeSharedSignal(input.signal, function(value) {
        record.inputs[name] = value;
        if (output) {
          controllerPublish(record, output, { kind: "input", name: name, value: value });
        }
      }, { immediate: input.immediate !== false });
      record.unsubscribers.push(unsub);
    }
  }

  function controllerDispatchEvent(record, binding, event, matched, kind, name) {
    if (!binding.allowEditable && controllerEditableTarget(event && event.target)) return;
    if (binding.preventDefault && event && typeof event.preventDefault === "function") event.preventDefault();
    if (kind === "event" && binding.stopPropagation && event && typeof event.stopPropagation === "function") event.stopPropagation();
    controllerEmitBinding(record, binding, { kind, name: String(name || ""), event: controllerEventPayload(event, matched) });
  }

  function controllerInstallEvents(record) {
    const root = record.root;
    for (const binding of record.config.events || []) {
      if (!binding || !binding.type || !binding.output) continue;
      controllerAddListener(record, root, String(binding.type), function(event) {
        const matched = controllerMatchesTarget(root, controllerEventTarget(root, event), binding.target);
        if (matched) controllerDispatchEvent(record, binding, event, matched, "event", binding.type);
      }, Boolean(binding.capture));
    }
  }

  function controllerInstallKeys(record) {
    for (const binding of record.config.keys || []) {
      if (!binding || !binding.output) continue;
      const eventType = String(binding.event || "keydown");
      const target = String(binding.scope || "global").toLowerCase() === "root" ? record.root : document;
      controllerAddListener(record, target, eventType, function(event) {
        if (controllerKeyMatches(binding, event)) controllerDispatchEvent(record, binding, event, event.target, "key", binding.name || binding.code || binding.key);
      }, false);
    }
  }

  function controllerInstallTimers(record) {
    const timers = controllerList(record.config, "timers");
    for (const spec of timers) {
      if (!spec || !spec.output) continue;
      const every = Math.max(1, Math.floor(Number(spec.everyMs || 0)));
      if (!Number.isFinite(every)) continue;
      let count = 0;
      const tick = function() {
        count += 1;
        controllerPublish(record, spec.output, {
          kind: "timer",
          name: String(spec.name || spec.output || ""),
          count: count,
          payload: Object.prototype.hasOwnProperty.call(spec, "payload") ? spec.payload : null,
        });
      };
      if (spec.immediate) tick();
      const handle = setInterval(tick, every);
      record.timers.push(handle);
    }
  }

  function controllerSameOriginURL(url) {
    try {
      const parsed = new URL(String(url || ""), window.location && window.location.href || document.baseURI || "http://localhost/");
      if (parsed.origin !== window.location.origin) return null;
      return parsed;
    } catch (_error) {
      return null;
    }
  }

  function controllerResourceImmediate(spec) {
    return !spec || spec.immediate !== false;
  }

  async function controllerFetchResource(record, spec) {
    const url = controllerSameOriginURL(spec && spec.url);
    if (!url) {
      controllerPublish(record, spec.errorOutput || spec.output, {
        kind: "resource",
        name: String(spec.name || spec.url || ""),
        error: "resource URL must be same-origin",
      });
      return;
    }
    if (record.resourceAbort[spec.name || spec.output]) {
      try { record.resourceAbort[spec.name || spec.output].abort(); } catch (_error) {}
    }
    const abort = new AbortController();
    const resourceKey = spec.name || spec.output;
    record.resourceAbort[resourceKey] = abort;
    record.abortControllers.push(abort);
    const headers = Object.assign({}, spec.headers || {});
    const init = {
      method: String(spec.method || "GET").toUpperCase(),
      headers: headers,
      signal: abort.signal,
      credentials: "same-origin",
    };
    let body = Object.prototype.hasOwnProperty.call(spec, "body") ? spec.body : undefined;
    if (spec.bodySignal) body = gosxReadSharedSignal(spec.bodySignal, body == null ? null : body);
    if (body !== undefined && body !== null && init.method !== "GET" && init.method !== "HEAD") {
      init.body = typeof body === "string" ? body : JSON.stringify(body);
      if (!headers["Content-Type"] && !headers["content-type"]) headers["Content-Type"] = "application/json";
    }
    try {
      const response = await fetch(url.href, init);
      if (abort.signal && abort.signal.aborted) return;
      const contentType = response.headers && response.headers.get ? String(response.headers.get("content-type") || "") : "";
      const text = await response.text();
      if (abort.signal && abort.signal.aborted) return;
      let data = text;
      if (contentType.includes("json") && text !== "") {
        try { data = JSON.parse(text); } catch (_error) {}
      }
      controllerPublish(record, spec.output, {
        kind: "resource",
        name: String(spec.name || spec.url || ""),
        result: {
          ok: Boolean(response.ok),
          status: response.status,
          statusText: response.statusText,
          url: url.pathname + url.search,
          data: data,
        },
      });
    } catch (error) {
      if (abort.signal && abort.signal.aborted) return;
      controllerPublish(record, spec.errorOutput || spec.output, {
        kind: "resource",
        name: String(spec.name || spec.url || ""),
        error: String(error && error.message || error || "fetch failed"),
      });
    } finally {
      if (record.resourceAbort[resourceKey] === abort) {
        delete record.resourceAbort[resourceKey];
      }
      const index = record.abortControllers.indexOf(abort);
      if (index >= 0) {
        record.abortControllers.splice(index, 1);
      }
    }
  }

  function controllerInstallResources(record) {
    const resources = controllerList(record.config, "resources");
    for (const spec of resources) {
      if (!spec || !spec.url || !spec.output) continue;
      const refresh = function() { controllerFetchResource(record, spec); };
      if (controllerResourceImmediate(spec)) {
        const pending = controllerFetchResource(record, spec);
        if (pending && typeof pending.then === "function") {
          record.ready.push(pending.catch(function() {}));
        }
      }
      if (spec.pollMs) {
        const poll = setInterval(refresh, Math.max(1, Math.floor(Number(spec.pollMs || 0))));
        record.timers.push(poll);
      }
      if (spec.refreshSignal) {
        record.unsubscribers.push(gosxSubscribeSharedSignal(spec.refreshSignal, refresh, { immediate: false }));
      }
    }
  }

  function mountController(entry) {
    if (!entry || !entry.id) return null;
    if (!window.__gosx.controllers) window.__gosx.controllers = new Map();
    const existing = window.__gosx.controllers.get(entry.id);
    if (existing) controllerDisposeRecord(existing);
    const config = entry.config || {};
    const record = {
      id: String(entry.id),
      name: String(config.name || entry.id),
      config: config,
      root: controllerRoot(config.root),
      outputs: controllerOutputMap(config),
      inputs: Object.create(null),
      resourceAbort: Object.create(null),
      listeners: [],
      unsubscribers: [],
      timers: [],
      abortControllers: [],
      ready: [],
      disposed: false,
    };
    window.__gosx.controllers.set(record.id, record);
    controllerInstallOutputs(record);
    controllerInstallInputs(record);
    if (gosxHost.controllers.installInput) gosxHost.controllers.installInput(record, {
      addListener: controllerAddListener, send: controllerSetValue, publish: controllerPublish,
      emit: controllerEmitBinding, matches: controllerMatchesTarget, editable: controllerEditableTarget,
      payload: controllerEventPayload, subscribe: gosxSubscribeSharedSignal,
    });
    controllerInstallEvents(record);
    controllerInstallKeys(record);
    controllerInstallTimers(record);
    controllerInstallResources(record);
    if (record.installStorage) record.installStorage();
    return record;
  }

  async function mountAllControllers(manifest) {
    // Page disposal replaces this map, invalidating mounts still loading input.
    const owner = window.__gosx.controllers || (window.__gosx.controllers = new Map());
    const controllers = controllerList(manifest);
    const path = window.__gosx.document?.get()?.assets?.runtime?.bootstrapControllerInputPath;
    if (path && !gosxHost.controllers.installInput) {
      await loadScriptTag(path, "controller-input");
    }
    if (window.__gosx.controllers !== owner) return;
    if (path && !gosxHost.controllers.installInput) throw new Error("controller input runtime unavailable");
    const pending = [];
    for (const entry of controllers) {
      const record = mountController(entry);
      if (record && Array.isArray(record.ready) && record.ready.length > 0) {
        pending.push.apply(pending, record.ready);
      }
    }
    if (pending.length) await Promise.all(pending);
  }

  function disposeController(controllerID) {
    if (!window.__gosx.controllers) return;
    const record = window.__gosx.controllers.get(controllerID);
    controllerDisposeRecord(record);
    window.__gosx.controllers.delete(controllerID);
  }

  gosxHost.controllers = Object.assign(gosxHost.controllers || {}, {
    mountAll: mountAllControllers,
    dispose: disposeController,
  });
  gosxHostCompatibility.install("__gosx_dispose_controller", disposeController);
