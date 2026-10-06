// @ts-check
// Optional controller input contracts. Loaded before mounting configured owners.
(function() {
  "use strict";
  const host = window.__gosx.host;
  let nextRequest = 0;
  const focusStack = [];
  let inertBranches = [];
  const own = (value, key) => value != null && Object.prototype.hasOwnProperty.call(value, key);
  const safe = key => !["__proto__", "prototype", "constructor"].includes(key);

  function readPath(value, path) {
    for (const key of String(path || "").split(".")) {
      if (!safe(key) || !own(value, key)) return undefined;
      value = value[key];
    }
    return value;
  }

  function clone(value) { return JSON.parse(JSON.stringify(value)); }

  function project(record, binding, payload, api) {
    const spec = binding.project;
    if (!spec) { api.publish(record, binding.output, payload); return; }
    try {
      const result = clone(spec.value || {});
      if (!result || typeof result !== "object" || Array.isArray(result)) return;
      const sources = Object.assign({ inputs: record.inputs }, payload);
      for (const path of Object.keys(spec.when || {})) {
        if (!Object.is(readPath(sources, path), spec.when[path])) return;
      }
      for (const key of Object.keys(spec.fields || {})) {
        if (!safe(key)) return;
        const value = readPath(sources, spec.fields[key]);
        if (value === undefined) return;
        result[key] = clone(value);
      }
      api.send(record, binding.output, result);
    } catch (_error) { /* Non-JSON or missing payloads do not produce intents. */ }
  }

  host.controllers.pickScene = function(mount, input, canvas, readViewport, readBundle, pickAtEvent, screenToRay) {
    if (typeof input?.requestId !== "string" || !Number.isFinite(input.clientX) || !Number.isFinite(input.clientY)) return;
    const bundle = readBundle();
    if (!bundle?.camera) return;
    const sample = pickAtEvent(input, canvas, readViewport, () => bundle, 720, 420);
    const ray = screenToRay(sample.pointer.x, sample.pointer.y, sample.metrics.width, sample.metrics.height, bundle.camera);
    const target = sample.target, object = target?.object;
    const hit = object?.id ? {
      id: String(object.id), kind: String(target.kind || object.kind || "mesh"),
      distance: Number(target.distance || 0), point: target.point || target.worldPosition || {},
      instanceIndex: target.instanceIndex >= 0 ? target.instanceIndex : undefined,
    } : null;
    mount.dispatchEvent(new CustomEvent("gosx:scene3d:input", { bubbles: true, detail: {
      kind: "ray", input: { requestId: input.requestId, ray: { origin: ray.origin, direction: ray.dir }, hit },
    } }));
  };

  function installDrag(record, binding, api) {
    if (!binding.source || !binding.output) return;
    let active = null, pending = null;
    const root = record.root;
    const threshold = binding.thresholdPx > 0 ? binding.thresholdPx : 4;
    function phase(output, drag) { if (output) api.publish(record, output, { kind: "drag", drag }); }
    function release(gesture) {
      if (gesture && gesture.element.releasePointerCapture) {
        try { gesture.element.releasePointerCapture(gesture.pointerId); } catch (_error) {}
      }
    }
    function cancel(reason) {
      const gesture = active || pending && pending.gesture;
      active = null;
      if (pending) clearTimeout(pending.timer);
      pending = null;
      release(gesture);
      if (gesture) phase(binding.cancelOutput, { ...gesture.drag, phase: "cancel", reason });
    }
    function finish(hit, target, gesture) {
      if (record.disposed) return;
      if (pending) clearTimeout(pending.timer);
      pending = null;
      if (target.scene && (!hit || !hit.id || target.hitIds && target.hitIds.length && !target.hitIds.includes(hit.id))) {
        phase(binding.cancelOutput, { ...gesture.drag, phase: "cancel", reason: "miss" });
        return;
      }
      api.emit(record, binding, { kind: "drag", drag: { ...gesture.drag,
        phase: "drop", target: target.target, hit: hit || null,
      } });
    }
    api.addListener(record, root, "pointerdown", function(event) {
      if (event.button != null && event.button !== 0 || event.isPrimary === false) return;
      const element = api.matches(root, event.target, binding.source);
      if (!element || api.editable(event.target)) return;
      cancel("replaced");
      active = { element, pointerId: event.pointerId, x: event.clientX, y: event.clientY, moved: false,
        drag: { phase: "start", pointerId: event.pointerId, source: api.payload(event, element).target, inputs: clone(record.inputs) } };
      if (element.setPointerCapture) { try { element.setPointerCapture(event.pointerId); } catch (_error) {} }
      if (binding.preventDefault) event.preventDefault();
      phase(binding.startOutput, active.drag);
    }, false);
    api.addListener(record, document, "pointermove", function(event) {
      if (!active || active.pointerId !== event.pointerId) return;
      active.moved = active.moved || Math.hypot(event.clientX - active.x, event.clientY - active.y) >= threshold;
      active.drag.clientX = event.clientX; active.drag.clientY = event.clientY;
      if (active.moved) phase(binding.moveOutput, { ...active.drag, phase: "move" });
      if (binding.preventDefault) event.preventDefault();
    }, false);
    api.addListener(record, document, "pointerup", function(event) {
      if (!active || active.pointerId !== event.pointerId) return;
      const gesture = active;
      if (!gesture.moved && Math.hypot(event.clientX - gesture.x, event.clientY - gesture.y) < threshold) { cancel("tap"); return; }
      active = null;
      release(gesture);
      gesture.drag.clientX = event.clientX; gesture.drag.clientY = event.clientY;
      const physical = document.elementFromPoint(event.clientX, event.clientY);
      for (const target of binding.targets || []) {
        const element = api.matches(root, physical, target.target);
        if (!element) continue;
        if (!target.scene) { finish(null, target, gesture); return; }
        const requestId = record.id + ":" + (++nextRequest);
        pending = { requestId, target, gesture, element, timer: null };
        pending.timer = setTimeout(() => cancel("timeout"), target.timeoutMs > 0 ? target.timeoutMs : 1000);
        element.dispatchEvent(new CustomEvent("gosx:scene3d:pick-request", {
          detail: { requestId, clientX: event.clientX, clientY: event.clientY, pointerId: event.pointerId },
        }));
        return;
      }
      phase(binding.cancelOutput, { ...gesture.drag, phase: "cancel", reason: "outside" });
    }, false);
    for (const type of ["pointercancel", "lostpointercapture"]) api.addListener(record, root === document ? document : root, type, function(event) {
      if (active && active.pointerId === event.pointerId) cancel(type);
    }, false);
    api.addListener(record, window, "blur", () => cancel("blur"), false);
    api.addListener(record, document, "gosx:scene3d:input", function(event) {
      const detail = event.detail, input = detail && detail.input;
      if (!pending || !detail || detail.kind !== "ray" || !input || input.requestId !== pending.requestId || event.target !== pending.element) return;
      if (pending.target.requestOutput && pending.target.resultSignal) {
        api.send(record, pending.target.requestOutput, { requestId: input.requestId, ray: input.ray });
      } else finish(input.hit, pending.target, pending.gesture);
    }, false);
    for (const target of binding.targets || []) if (target.resultSignal) {
      record.unsubscribers.push(api.subscribe(target.resultSignal, function(result) {
        if (!pending || pending.target !== target || !result || result.requestId !== pending.requestId) return;
        finish(result.hit, target, pending.gesture);
      }, { immediate: false }));
    }
    record.unsubscribers.push(() => cancel("dispose"));
  }

  function query(root, selector) {
    try { return selector && root.querySelector(selector); } catch (_error) { return null; }
  }
  function focusables(element) {
    return Array.from(element.querySelectorAll("a[href],button,input,textarea,select,[tabindex]"))
      .filter(item => !item.disabled && item.tabIndex >= 0 && !item.closest("[hidden],[inert]") && item.getClientRects().length);
  }
  function focusOwner(owner, last) {
    const items = focusables(owner.element);
    (last === undefined && query(owner.element, owner.spec.initialFocus) || items[last ? items.length - 1 : 0] || owner.element).focus();
  }
  function refreshInert() {
    for (const [element, inert] of inertBranches) element.inert = inert;
    inertBranches = [];
    const owner = focusStack[focusStack.length - 1];
    if (!owner) return;
    for (let child = owner.element; child && child !== document.body; child = child.parentElement) {
      if (!child.parentElement) break;
      for (const sibling of child.parentElement.children) if (sibling !== child) {
        inertBranches.push([sibling, sibling.inert]); sibling.inert = true;
      }
    }
  }
  function installFocus(record, spec, api) {
    if (!spec.target || !spec.openSignal) return;
    let owner = null, wanted = false;
    function close() {
      if (!owner) return;
      const index = focusStack.indexOf(owner), wasTop = index === focusStack.length - 1;
      if (index >= 0) focusStack.splice(index, 1);
      refreshInert();
      if (owner.addedTabindex) owner.element.removeAttribute("tabindex");
      if (wasTop) {
        const current = focusStack[focusStack.length - 1];
        const target = query(record.root, spec.returnFocus) || owner.previous;
        if (current && (!target || !current.element.contains(target))) focusOwner(current, false);
        else if (target && target.isConnected !== false && typeof target.focus === "function") target.focus();
      }
      owner = null;
    }
    record.unsubscribers.push(api.subscribe(spec.openSignal, function(value) {
      wanted = value === true;
      if (!wanted) { close(); return; }
      // Islands may create the modal in the same signal notification turn.
      queueMicrotask(function() {
        if (record.disposed || !wanted || owner) return;
        const element = query(record.root, spec.target);
        if (!element) return;
        owner = { element, spec, previous: document.activeElement, addedTabindex: !element.hasAttribute("tabindex") };
        if (owner.addedTabindex) element.setAttribute("tabindex", "-1");
        focusStack.push(owner); refreshInert(); focusOwner(owner);
      });
    }, { immediate: true }));
    api.addListener(record, document, "keydown", function(event) {
      if (!owner || focusStack[focusStack.length - 1] !== owner) return;
      if (event.key === "Escape") { event.preventDefault(); event.stopImmediatePropagation(); api.send(record, spec.openSignal, false); return; }
      if (event.key !== "Tab") return;
      const items = focusables(owner.element), first = items[0], last = items[items.length - 1];
      const active = document.activeElement;
      if (!items.length || !owner.element.contains(active) || event.shiftKey && active === first || !event.shiftKey && (active === last || active === owner.element)) {
        event.preventDefault(); focusOwner(owner, Boolean(event.shiftKey));
      }
    }, true);
    api.addListener(record, document, "focusin", function(event) {
      if (owner && focusStack[focusStack.length - 1] === owner && !owner.element.contains(event.target)) focusOwner(owner, false);
    }, true);
    record.unsubscribers.push(close);
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

  function controllerInstallStorage(record, api) {
    const storageConfig = record.config && record.config.storage;
    if (!storageConfig) return;
    const area = controllerStorageArea(storageConfig.area);
    if (!area) return;
    const loads = Array.isArray(storageConfig.load) ? storageConfig.load : [];
    for (const slot of loads) {
      if (!slot || !slot.key || (!slot.signal && !slot.output)) continue;
      let value = null;
      let decoded = false;
      try {
        const raw = area.getItem(controllerStorageKey(storageConfig, slot.key));
        if (raw != null && raw !== "") {
          value = JSON.parse(raw);
          decoded = true;
        }
      } catch (_error) {}
      if (slot.signal && decoded) api.send(record, slot.signal, value);
      if (slot.output) {
        api.publish(record, slot.output, { kind: "storage", name: String(slot.key), value: value });
      }
    }
    const saves = Array.isArray(storageConfig.save) ? storageConfig.save : [];
    for (const slot of saves) {
      if (!slot || !slot.key || !slot.signal) continue;
      const unsub = api.subscribe(slot.signal, function(value) {
        try {
          area.setItem(controllerStorageKey(storageConfig, slot.key), JSON.stringify(value == null ? null : value));
        } catch (error) {
          api.publish(record, slot.output || slot.signal, {
            kind: "storage",
            name: String(slot.key),
            error: String(error && error.message || error || "storage failed"),
          });
        }
      }, { immediate: false });
      record.unsubscribers.push(unsub);
    }
  }

  host.controllers.installInput = function(record, api) {
    record.installStorage = () => controllerInstallStorage(record, api);
    record.project = (binding, payload) => project(record, binding, payload, api);
    for (const binding of record.config.drags || []) installDrag(record, binding, api);
    for (const spec of record.config.focus || []) installFocus(record, spec, api);
  };
})();
