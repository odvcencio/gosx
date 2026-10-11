// @ts-check
// GoSX browser host: commands and layout controls in an opt-in feature chunk.
(function() {
  "use strict";
  const registerFeature = window.__gosx_register_bootstrap_feature;
  if (typeof registerFeature !== "function") return;
  registerFeature("workbench", function(api) {
    const readSignal = api.gosxReadSharedSignal;
    const setSignal = api.setSharedSignalValue;
    const subscribeSignal = api.gosxSubscribeSharedSignal;
    const listeners = [], observers = [];
    const activeHandles = new Map(), bindings = new Map();
    let manifest = null, inputPromise = null, generation = 0, tabID = 0;
    function setAttr(element, name, value) { element.setAttribute(name, value); }
    function connected(el) { return document.documentElement.contains(el); }
    function binding(el) {
      if (!bindings.has(el)) bindings.set(el, { cleanup: [] });
      return bindings.get(el);
    }
    function listen(type, fn, target = document, options = false) {
      target.addEventListener(type, fn, options);
      listeners.push([target, type, fn, options]);
    }
    function releaseBinding(el, state) {
      activeHandles.get(el)?.cancel("dispose");
      for (const cleanup of state.cleanup) cleanup();
      bindings.delete(el);
    }
    function disposeAll() {
      generation++;
      for (const [el, state] of bindings) releaseBinding(el, state);
      for (const [target, type, fn, options] of listeners.splice(0)) target.removeEventListener(type, fn, options);
      for (const observer of observers.splice(0)) observer.disconnect();
    }
    function createCommandRegistry() {
      let entries = [];
      const namedKeys = {
        space: " ", plus: "+", minus: "-", enter: "Enter", escape: "Escape", esc: "Escape", tab: "Tab",
        delete: "Delete", del: "Delete", backspace: "Backspace", home: "Home", end: "End",
        pageup: "PageUp", pagedown: "PageDown", insert: "Insert",
        arrowup: "ArrowUp", up: "ArrowUp", arrowdown: "ArrowDown", down: "ArrowDown",
        arrowleft: "ArrowLeft", left: "ArrowLeft", arrowright: "ArrowRight", right: "ArrowRight",
      };
      function platformMod() {
        const platform = navigator.userAgentData && navigator.userAgentData.platform || navigator.platform || "";
        return /mac|iphone|ipad|ipod/i.test(platform) ? "meta" : "ctrl";
      }
      function parseChord(text) {
        const parts = String(text || "").split("+").map(part => part.trim().toLowerCase());
        const token = parts.pop();
        let key = token;
        if (!key) return null;
        if (Array.from(key).length !== 1) {
          key = Object.prototype.hasOwnProperty.call(namedKeys, key) ? namedKeys[key] : /^f[0-9]+$/.test(key) ? key.toUpperCase() : "";
        }
        if (!key) return null;
        const chord = { key, ctrl: false, alt: false, meta: false, shift: false };
        let mod = false;
        for (const token of parts) {
          switch (token) {
            case "mod": mod = true; break;
            case "ctrl": case "control": chord.ctrl = true; break;
            case "alt": case "option": chord.alt = true; break;
            case "shift": chord.shift = true; break;
            case "meta": case "cmd": case "command": case "super": chord.meta = true; break;
            default: return null;
          }
        }
        if (mod && (chord.ctrl || chord.meta)) return null;
        if (mod) chord[platformMod()] = true;
        return chord;
      }
      function chordMatches(chord, event) {
        if (!chord) return false;
        const symbolKey = chord.key.length === 1 && !/[a-z0-9 ]/i.test(chord.key);
        if (chord.ctrl !== !!event.ctrlKey || chord.alt !== !!event.altKey || chord.meta !== !!event.metaKey) return false;
        if (!symbolKey && chord.shift !== !!event.shiftKey) return false;
        if (symbolKey && chord.shift && !event.shiftKey) return false;
        const key = String(event.key || "");
        if (key.toLowerCase() === chord.key.toLowerCase()) return true;
        const code = String(event.code || "");
        return chord.key.length === 1 && (/[a-z]/.test(chord.key) ? code === "Key" + chord.key.toUpperCase() : /[0-9]/.test(chord.key) && code === "Digit" + chord.key);
      }
      function editableTarget(target) {
        if (!target) return false;
        if (/^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName) || target.isContentEditable) return true;
        return Boolean(target.closest && target.closest("[contenteditable=true],[contenteditable=''],[contenteditable=plaintext-only]"));
      }
      function available(cmd) {
        return Boolean(cmd && !cmd.reserved && Object.entries(cmd.when || {}).every(([name, value]) => JSON.stringify(readSignal(name, undefined)) === JSON.stringify(value)));
      }
      function run(cmd) {
        if (!available(cmd)) return false;
        const detail = { id: cmd.id, at: Date.now() };
        document.dispatchEvent(new CustomEvent("gosx:command", { detail }));
        const action = cmd.action || {};
        if (action.signal) setSignal(action.signal, action.value == null ? detail : action.value);
        const disclosure = window.__gosx.disclosure;
        for (const kind of ["open", "close"]) {
          if (action[kind] && disclosure && typeof disclosure[kind] === "function") {
            const element = document.querySelector(action[kind]);
            if (element) disclosure[kind](element);
          }
        }
        return true;
      }
      function onKeydown(event) {
        if (event.defaultPrevented || event.__gosx_stop_island_fanout) return;
        for (const entry of entries) {
          const cmd = entry.command;
          if (editableTarget(event.target) && !cmd.allowEditable) continue;
          if (!entry.chords.some(chord => chordMatches(chord, event))) continue;
          // Reserved commands consume their chord but can never run an action.
          if (cmd.reserved) {
            event.preventDefault();
            document.dispatchEvent(new CustomEvent("gosx:command:unavailable", { detail: { id: cmd.id, at: Date.now() } }));
            return;
          }
          if (!available(cmd)) continue;
          event.preventDefault();
          run(cmd);
          return;
        }
      }
      function aria(chord) {
        const parts = [];
        if (chord.ctrl) parts.push("Control");
        if (chord.alt) parts.push("Alt");
        if (chord.meta) parts.push("Meta");
        if (chord.shift) parts.push("Shift");
        parts.push(chord.key === " " ? "Space" : chord.key === "+" ? "Plus" : Array.from(chord.key).length === 1 ? chord.key.toUpperCase() : chord.key);
        return parts.join("+");
      }
      function find(id) { return entries.find(entry => entry.command.id === id)?.command; }
      function decorateCommandElements() {
        for (const element of document.querySelectorAll("[data-gosx-command]")) {
          const entry = entries.find(entry => entry.command.id === element.getAttribute("data-gosx-command"));
          if (!entry) continue;
          const shortcuts = entry.chords.map(aria).join(" ");
          if (shortcuts) setAttr(element, "aria-keyshortcuts", shortcuts);
          else element.removeAttribute("aria-keyshortcuts");
          if (entry.command.reserved) setAttr(element, "aria-disabled", "true");
          else element.removeAttribute("aria-disabled");
        }
      }
      const publicAPI = {
        list: () => entries.map(({ command: cmd }) => ({ id: cmd.id, title: cmd.title, keys: cmd.keys || [], group: cmd.group, reserved: !!cmd.reserved, available: available(cmd) })),
        run: id => run(find(id)),
        available: id => available(find(id)),
        platformMod,
        chord: parseChord,
      };
      return {
        api: publicAPI, parseChord, decorateCommandElements,
        bind(list) {
          entries = list.map(command => ({ command, chords: (command.keys || []).map(parseChord).filter(Boolean) }));
          listen("keydown", onKeydown);
          listen("click", event => {
            const element = event.target && event.target.closest && event.target.closest("[data-gosx-command]");
            if (!element || event.defaultPrevented) return;
            event.preventDefault();
            publicAPI.run(element.getAttribute("data-gosx-command"));
          });
          decorateCommandElements();
        },
        unbind() { entries = []; },
      };
    }
    function subscribe(el, name, fn, options) {
      binding(el).cleanup.push(subscribeSignal(name, value => { if (connected(el)) fn(value); }, options));
    }
    // Defaults: y axis, unbounded range, step = range/100 (else 1),
    // scale = range/axis size (else 1), Alt fine = 0.1, no reset.
    function parseDragHandle(el) {
      function number(name, fallback) {
        const value = el.getAttribute("data-gosx-drag-" + name);
        return value != null && value.trim() !== "" && Number.isFinite(Number(value)) ? Number(value) : fallback;
      }
      const axis = el.getAttribute("data-gosx-drag-axis") || "y";
      const min = number("min", -Infinity), max = number("max", Infinity);
      const bounded = Number.isFinite(min) && Number.isFinite(max);
      const size = axis === "x" ? el.clientWidth : axis === "xy" ? Math.max(el.clientWidth, el.clientHeight) : el.clientHeight;
      return { el, signal: el.getAttribute("data-gosx-drag"), axis, min, max,
        step: number("step", bounded && max > min ? (max - min) / 100 : 1),
        scale: number("scale", bounded && size > 0 ? (max - min) / size : 1),
        fine: number("fine", 0.1), reset: number("reset", undefined),
        end: el.getAttribute("data-gosx-drag-end") };
    }
    function cleanNumber(value) { return Number(value.toPrecision(14)); }
    function roundStep(value, step, origin) {
      return cleanNumber(step > 0 ? origin + Math.round((value - origin) / step) * step : value);
    }
    function clamp(h, value) { return cleanNumber(Math.min(h.max, Math.max(h.min, value))); }
    function applyDelta(h, start, px, fine) {
      return clamp(h, roundStep(start + px * h.scale * (fine ? h.fine : 1), h.step, Number.isFinite(h.min) ? h.min : 0));
    }
    function currentValue(h) {
      const value = readSignal(h.signal, undefined);
      if (value != null && Number.isFinite(Number(value))) return Number(value);
      const aria = h.el.getAttribute("aria-valuenow");
      return aria != null && Number.isFinite(Number(aria)) ? Number(aria) : Number.isFinite(h.min) ? h.min : 0;
    }
    function handleARIA(el, value) {
      if (!connected(el) || value == null || !Number.isFinite(Number(value))) return;
      for (const suffix of ["now", "text"]) setAttr(el, "aria-value" + suffix, value);
    }
    function writeHandle(h, value) { setSignal(h.signal, value); handleARIA(h.el, value); }
    function endHandle(h, value, commit) {
      const detail = { signal: h.signal, value, commit };
      if (h.end) setSignal(h.end, detail);
      if (connected(h.el)) h.el.dispatchEvent(new CustomEvent("gosx:drag:end", { bubbles: true, detail }));
    }
    function dragElement(event) { return event.target?.closest?.("[data-gosx-drag]"); }
    function ensureInputChunk() {
      const controllers = window.__gosx.host.controllers;
      if (controllers?.pointerGesture) return Promise.resolve(controllers.pointerGesture);
      if (!inputPromise) inputPromise = (async () => {
        if (manifest?.controllers?.length) await api.ensureBootstrapFeature("controllers");
        if (!window.__gosx.host.controllers?.pointerGesture) {
          const path = window.__gosx.document?.get()?.assets?.runtime?.bootstrapControllerInputPath;
          const base = document.querySelector('meta[name="gosx-base-path"]')?.getAttribute("content") || "";
          await api.loadScriptTag(path || base.replace(/\/$/, "") + "/gosx/bootstrap-controller-input.js", "controller-input");
        }
        const gesture = window.__gosx.host.controllers?.pointerGesture;
        if (!gesture) throw new Error("workbench pointer input unavailable");
        return gesture;
      })().finally(() => { inputPromise = null; });
      return inputPromise;
    }
    function onHandleKeydown(event) {
      const el = dragElement(event);
      if (!el || activeHandles.has(el) || event.defaultPrevented || event.ctrlKey || event.metaKey) return;
      const h = parseDragHandle(el), key = event.key;
      let value = currentValue(h), delta = 0;
      if ((key === "ArrowRight" || key === "ArrowLeft") && h.axis !== "y") delta = key === "ArrowRight" ? 1 : -1;
      else if ((key === "ArrowUp" || key === "ArrowDown") && h.axis !== "x") delta = key === "ArrowUp" ? 1 : -1;
      else if (key === "PageUp" || key === "PageDown") delta = key === "PageUp" ? 10 : -10;
      else if (key === "Home" && Number.isFinite(h.min)) value = h.min;
      else if (key === "End" && Number.isFinite(h.max)) value = h.max;
      else if ((key === "Backspace" || key === "Delete") && h.reset !== undefined) value = h.reset;
      else return;
      if (delta) {
        const arrow = key.startsWith("Arrow");
        value += delta * h.step * (arrow && event.shiftKey ? 10 : 1) * (arrow && event.altKey ? h.fine : 1);
      }
      event.preventDefault(); value = clamp(h, value); writeHandle(h, value); endHandle(h, value, true);
    }
    function bindHandles() {
      listen("keydown", onHandleKeydown);
      listen("pointerdown", event => {
        if (event.button != null && event.button !== 0 || event.isPrimary === false || event.defaultPrevented) return;
        const el = dragElement(event);
        if (!el) return;
        binding(el);
        event.preventDefault();
        if (activeHandles.has(el)) activeHandles.get(el).cancel("replaced");
        const h = parseDragHandle(el), start = currentValue(h), owner = generation;
        const pending = { pointerId: event.pointerId, pending: true, cancel() { activeHandles.delete(el); } };
        activeHandles.set(el, pending);
        ensureInputChunk().then(pointerGesture => {
          if (owner !== generation || activeHandles.get(el) !== pending || el.isConnected === false) return;
          const gesture = pointerGesture(el, event, { escape: true,
            onMove(move, dx, dy) { writeHandle(h, applyDelta(h, start, h.axis === "x" ? dx : h.axis === "xy" ? dx - dy : -dy, move.altKey)); },
            onEnd() { activeHandles.delete(el); endHandle(h, currentValue(h), true); },
            onCancel() { activeHandles.delete(el); writeHandle(h, start); endHandle(h, start, false); },
          });
          activeHandles.set(el, gesture);
        }).catch(error => { pending.cancel(); console.error("[gosx] workbench drag:", error); });
      });
      // A release before an asynchronous input load must not start a stale drag.
      function cancelPending(event) {
        for (const gesture of activeHandles.values()) {
          if (gesture.pending && (event.type === "blur" || gesture.pointerId === event.pointerId)) gesture.cancel();
        }
      }
      for (const type of ["pointerup", "pointercancel", "blur"]) listen(type, cancelPending, type === "blur" ? window : document);
      listen("dblclick", event => {
        const el = dragElement(event);
        if (!el || activeHandles.has(el)) return;
        const h = parseDragHandle(el);
        if (h.reset === undefined) return;
        event.preventDefault(); const value = clamp(h, h.reset); writeHandle(h, value); endHandle(h, value, true);
      });
    }
    function parseStyleBinds(spec) {
      return String(spec || "").split(",").map(pair => {
        const [prop, source, unit = ""] = pair.split(":").map(value => value.trim());
        return { prop, source, unit };
      }).filter(b => b.prop.startsWith("--") && (b.source?.startsWith("$") || b.source?.startsWith("@data-")));
    }
    function applyStyleBind(el, bind, value) {
      if (value == null) el.style.removeProperty(bind.prop);
      else el.style.setProperty(bind.prop, String(value) + bind.unit);
    }
    function bindStyleElement(el, force) {
      const state = binding(el);
      if (state.style) { if (force) state.style(); return; }
      const binds = parseStyleBinds(el.getAttribute("data-gosx-bind-style"));
      const attributes = binds.filter(b => b.source.startsWith("@"));
      const refresh = () => { if (connected(el)) for (const b of attributes) applyStyleBind(el, b, el.getAttribute(b.source.slice(1))); };
      state.style = refresh; refresh();
      for (const b of binds.filter(b => b.source.startsWith("$"))) {
        // Dotted signal names are independent signals. Also resolve object paths
        // from a parent signal, for bindings such as $layout.sidebar.
        const parts = b.source.split(".");
        for (let i = 1; i <= parts.length; i++) {
          const name = parts.slice(0, i).join("."), path = parts.slice(i);
          const update = value => {
            for (const key of path) if (value != null) value = Object.prototype.hasOwnProperty.call(value, key) ? value[key] : undefined;
            if (value !== undefined) applyStyleBind(el, b, value);
          };
          subscribe(el, name, update, { immediate: false });
          const initial = readSignal(name, undefined);
          if (initial !== undefined) update(initial);
        }
      }
      if (attributes.length) {
        const observer = new MutationObserver(refresh);
        observer.observe(el, { attributes: true, attributeFilter: attributes.map(b => b.source.slice(1)) }); state.cleanup.push(() => observer.disconnect());
      }
    }
    function bindStyleBinds(root, force = false) {
      for (const el of root.querySelectorAll("[data-gosx-bind-style]")) bindStyleElement(el, force);
    }
    function tabLinks(container) {
      return Array.from(container.querySelectorAll("[data-gosx-tab]")).filter(tab => tab.closest("[data-gosx-tabs]") === container);
    }
    function tabPanel(container, tab) {
      const id = tab.getAttribute("data-gosx-tab-panel");
      return Array.from(container.querySelectorAll("[id]")).find(panel => panel.id === id && panel.closest("[data-gosx-tabs]") === container);
    }
    function selectTab(container, tab, focus, publish = true) {
      if (!connected(container)) return;
      for (const item of tabLinks(container)) {
        const selected = item === tab;
        setAttr(item, "aria-selected", String(selected)); setAttr(item, "tabindex", selected ? "0" : "-1");
        if (selected) setAttr(item, "aria-current", "page"); else item.removeAttribute("aria-current");
        const panel = tabPanel(container, item);
        if (panel) { panel.hidden = !selected; if (selected) panel.removeAttribute("hidden"); else setAttr(panel, "hidden", ""); }
      }
      if (focus) tab.focus();
      const signal = container.getAttribute("data-gosx-tabs-signal");
      if (publish && signal) setSignal(signal, tab.getAttribute("data-gosx-tab"));
    }
    function upgradeTabs(container) {
      const state = binding(container);
      const links = tabLinks(container), nav = container.querySelector("nav");
      if (!links.length || !nav) return;
      setAttr(nav, "role", "tablist");
      for (const tab of links) {
        if (!tab.id) tab.id = "gosx-tab-" + (++tabID);
        setAttr(tab, "role", "tab");
        const id = tab.getAttribute("data-gosx-tab-panel");
        if (id) setAttr(tab, "aria-controls", id);
        const panel = tabPanel(container, tab);
        if (panel) { setAttr(panel, "role", "tabpanel"); setAttr(panel, "aria-labelledby", tab.id); }
      }
      const signal = container.getAttribute("data-gosx-tabs-signal");
      const value = readSignal(signal, undefined);
      selectTab(container, links.find(tab => tab.getAttribute("data-gosx-tab") === value) || links.find(tab => tab.getAttribute("aria-current") === "page") || links[0], false, false);
      // Enhance current children on every refresh; retain only one root subscription.
      if (state.tabs) return;
      state.tabs = true;
      if (signal) subscribe(container, signal, value => {
        const tab = tabLinks(container).find(item => item.getAttribute("data-gosx-tab") === value);
        if (tab) selectTab(container, tab, false, false);
      });
    }
    function bindTabs() {
      listen("click", event => {
        const tab = event.target?.closest?.("[data-gosx-tab]"), container = tab?.closest("[data-gosx-tabs]");
        if (!container || event.defaultPrevented || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
        event.preventDefault(); selectTab(container, tab, false);
      });
      listen("keydown", event => {
        const tab = event.target?.closest?.("[data-gosx-tab]"), container = tab?.closest("[data-gosx-tabs]");
        if (!container || event.defaultPrevented) return;
        const links = tabLinks(container), index = links.indexOf(tab);
        const vertical = container.querySelector("nav")?.getAttribute("aria-orientation") === "vertical";
        let next;
        const arrows = vertical ? ["ArrowUp", "ArrowDown"] : ["ArrowLeft", "ArrowRight"];
        if (arrows.includes(event.key)) next = links[(index + (event.key === arrows[0] ? links.length - 1 : 1)) % links.length];
        else if (event.key === "Home") next = links[0];
        else if (event.key === "End") next = links[links.length - 1];
        else if (event.key === "Enter" || event.key === " ") next = tab;
        else return;
        event.preventDefault(); selectTab(container, next, true);
      });
    }
    function bindCollapsible(el) {
      const state = binding(el);
      if (state.collapsible) return;
      state.collapsible = true;
      subscribe(el, el.getAttribute("data-gosx-collapsible"), value => {
        if (typeof value !== "boolean") return;
        el.open = value;
        if (value) setAttr(el, "open", ""); else el.removeAttribute("open");
      });
    }
    function refreshLayout() {
      for (const [el, state] of bindings) if (!connected(el)) releaseBinding(el, state);
      bindStyleBinds(document);
      for (const el of document.querySelectorAll("[data-gosx-drag]")) if (!binding(el).handle) {
        binding(el).handle = true;
        subscribe(el, el.getAttribute("data-gosx-drag"), value => handleARIA(el, value));
      }
      for (const el of document.querySelectorAll("[data-gosx-tabs]")) upgradeTabs(el);
      for (const el of document.querySelectorAll("[data-gosx-collapsible]")) bindCollapsible(el);
      commands.decorateCommandElements();
      if (document.querySelector("[data-gosx-drag]")) ensureInputChunk().catch(error => console.error("[gosx] workbench input:", error));
    }
    const commands = createCommandRegistry();
    const workbench = { version: 1, commands: commands.api, debug: {
      setSignal, parseChord: commands.parseChord, refreshCommands: commands.decorateCommandElements,
      parseDragHandle, applyDelta, refreshStyleBinds: () => bindStyleBinds(document, true),
    } };
    window.__gosx.commands = commands.api;
    window.__gosx.workbench = workbench;
    if (window.__gosx.host) window.__gosx.host.workbench = workbench;
    return {
      runtimeReady(nextManifest) {
        disposeAll(); manifest = nextManifest;
        bindHandles(); bindTabs(); commands.bind(manifest && manifest.commands || []);
        listen("toggle", event => {
          const el = event.target;
          const signal = el?.getAttribute?.("data-gosx-collapsible");
          if (signal && readSignal(signal, undefined) !== el.open) setSignal(signal, el.open);
        }, document, true);
        refreshLayout();
        let queued = false;
        const owner = generation;
        const observer = new MutationObserver(() => {
          if (queued) return;
          queued = true;
          queueMicrotask(() => { queued = false; if (owner === generation) refreshLayout(); });
        });
        observer.observe(document.body, { childList: true, subtree: true }); observers.push(observer);
      },
      disposePage() { commands.unbind(); disposeAll(); },
    };
  });
})();
