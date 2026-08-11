  // @ts-check
  // GoSX browser host: declarative headless controllers.

  /**
   * @typedef {object} GoSXControllerRecord
   * @property {string} id
   * @property {string} name
   * @property {object} config
   * @property {Document|Element} root
   * @property {Record<string, string>} outputs
   * @property {Record<string, unknown>} inputs
   * @property {Record<string, AbortController>} resourceAbort
   * @property {Array<Array<unknown>>} listeners
   * @property {Function[]} unsubscribers
   * @property {number[]} timers
   * @property {AbortController[]} abortControllers
   * @property {Promise<unknown>[]} ready
   * @property {boolean} disposed
   */

  function controllerList(manifest) {
    return manifest && Array.isArray(manifest.controllers) ? manifest.controllers : [];
  }

  function controllerOutputMap(config) {
    const outputs = Object.create(null);
    const list = config && Array.isArray(config.outputs) ? config.outputs : [];
    for (const output of list) {
      if (!output || !output.signal) continue;
      const name = String(output.name || output.signal || "").trim();
      if (!name) continue;
      outputs[name] = String(output.signal || "").trim();
    }
    return outputs;
  }

  function controllerOutputSignal(record, name) {
    const outputName = String(name || "").trim();
    if (!outputName) return "";
    return record.outputs[outputName] || outputName;
  }

  function controllerPublish(record, output, payload) {
    const signal = controllerOutputSignal(record, output);
    if (!signal) return;
    const body = Object.assign({
      controllerId: record.id,
      controller: record.name,
      at: Date.now(),
    }, payload || {});
    try {
      const result = setSharedSignalValue(signal, body);
      if (typeof result === "string" && result !== "") {
        console.error("[gosx] controller signal error (" + record.id + "/" + signal + "):", result);
      }
    } catch (error) {
      console.error("[gosx] controller signal error (" + record.id + "/" + signal + "):", error);
    }
  }

  function controllerSetValue(record, output, value) {
    const signal = controllerOutputSignal(record, output);
    if (!signal) return;
    try {
      const result = setSharedSignalValue(signal, value);
      if (typeof result === "string" && result !== "") {
        console.error("[gosx] controller signal error (" + record.id + "/" + signal + "):", result);
      }
    } catch (error) {
      console.error("[gosx] controller signal error (" + record.id + "/" + signal + "):", error);
    }
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
    };
    if (target) {
      payload.target = {
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
    const outputs = record.config && Array.isArray(record.config.outputs) ? record.config.outputs : [];
    for (const output of outputs) {
      if (!output || !output.signal || !Object.prototype.hasOwnProperty.call(output, "initial")) continue;
      controllerSetValue(record, output.name || output.signal, output.initial);
    }
  }

  function controllerInstallInputs(record) {
    const inputs = record.config && Array.isArray(record.config.inputs) ? record.config.inputs : [];
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

  function controllerInstallEvents(record) {
    const events = record.config && Array.isArray(record.config.events) ? record.config.events : [];
    const root = record.root;
    for (const binding of events) {
      if (!binding || !binding.type || !binding.output) continue;
      const listener = function(event) {
        const target = controllerEventTarget(root, event);
        const matched = controllerMatchesTarget(root, target, binding.target);
        if (!matched) return;
        if (!binding.allowEditable && controllerEditableTarget(target)) return;
        if (binding.preventDefault && event && typeof event.preventDefault === "function") event.preventDefault();
        if (binding.stopPropagation && event && typeof event.stopPropagation === "function") event.stopPropagation();
        controllerPublish(record, binding.output, {
          kind: "event",
          name: String(binding.type || ""),
          event: controllerEventPayload(event, matched),
        });
      };
      controllerAddListener(record, root === document ? document : root, String(binding.type), listener, Boolean(binding.capture));
    }
  }

  function controllerInstallKeys(record) {
    const keys = record.config && Array.isArray(record.config.keys) ? record.config.keys : [];
    for (const binding of keys) {
      if (!binding || !binding.output) continue;
      const eventType = String(binding.event || "keydown");
      const scope = String(binding.scope || "global").toLowerCase();
      const target = scope === "root" ? record.root : document;
      const listener = function(event) {
        if (!binding.allowEditable && controllerEditableTarget(event && event.target)) return;
        if (!controllerKeyMatches(binding, event)) return;
        if (binding.preventDefault && event && typeof event.preventDefault === "function") event.preventDefault();
        controllerPublish(record, binding.output, {
          kind: "key",
          name: String(binding.name || binding.code || binding.key || ""),
          event: controllerEventPayload(event, event && event.target),
        });
      };
      controllerAddListener(record, target, eventType, listener, false);
    }
  }

  function controllerInstallTimers(record) {
    const timers = record.config && Array.isArray(record.config.timers) ? record.config.timers : [];
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
    const resources = record.config && Array.isArray(record.config.resources) ? record.config.resources : [];
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

  function controllerStorageArea(area) {
    const name = String(area || "local").toLowerCase();
    try {
      if (name === "session") return window.sessionStorage || null;
      return window.localStorage || null;
    } catch (_error) {
      return null;
    }
  }

  function controllerStorageKey(config, key) {
    const ns = String(config.namespace || "gosx:controller").trim();
    return ns + ":" + String(key || "").trim();
  }

  function controllerInstallStorage(record) {
    const storageConfig = record.config && record.config.storage;
    if (!storageConfig) return;
    const area = controllerStorageArea(storageConfig.area);
    if (!area) return;
    const loads = Array.isArray(storageConfig.load) ? storageConfig.load : [];
    for (const slot of loads) {
      if (!slot || !slot.key || !slot.output) continue;
      let value = null;
      try {
        const raw = area.getItem(controllerStorageKey(storageConfig, slot.key));
        value = raw == null || raw === "" ? null : JSON.parse(raw);
      } catch (_error) {}
      controllerPublish(record, slot.output, { kind: "storage", name: String(slot.key), value: value });
    }
    const saves = Array.isArray(storageConfig.save) ? storageConfig.save : [];
    for (const slot of saves) {
      if (!slot || !slot.key || !slot.signal) continue;
      const unsub = gosxSubscribeSharedSignal(slot.signal, function(value) {
        try {
          area.setItem(controllerStorageKey(storageConfig, slot.key), JSON.stringify(value == null ? null : value));
        } catch (error) {
          controllerPublish(record, slot.output || slot.signal, {
            kind: "storage",
            name: String(slot.key),
            error: String(error && error.message || error || "storage failed"),
          });
        }
      }, { immediate: false });
      record.unsubscribers.push(unsub);
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
    controllerInstallEvents(record);
    controllerInstallKeys(record);
    controllerInstallTimers(record);
    controllerInstallResources(record);
    controllerInstallStorage(record);
    return record;
  }

  function mountAllControllers(manifest) {
    const controllers = controllerList(manifest);
    const pending = [];
    for (const entry of controllers) {
      const record = mountController(entry);
      if (record && Array.isArray(record.ready) && record.ready.length > 0) {
        pending.push.apply(pending, record.ready);
      }
    }
    if (pending.length === 0) return Promise.resolve();
    return Promise.all(pending).then(function() {});
  }

  window.__gosx_dispose_controller = function(controllerID) {
    if (!window.__gosx.controllers) return;
    const record = window.__gosx.controllers.get(controllerID);
    controllerDisposeRecord(record);
    window.__gosx.controllers.delete(controllerID);
  };
