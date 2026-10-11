// @ts-check
// GoSX browser host: page commands in an opt-in feature chunk.
(function() {
  "use strict";
  const registerFeature = window.__gosx_register_bootstrap_feature;
  if (typeof registerFeature !== "function") return;
  registerFeature("workbench", function(api) {
    const readSignal = api.gosxReadSharedSignal;
    const setSignal = api.setSharedSignalValue;
    const listeners = [];
    function listen(type, fn) {
      document.addEventListener(type, fn);
      listeners.push([type, fn]);
    }
    function disposeAll() {
      for (const [type, fn] of listeners.splice(0)) document.removeEventListener(type, fn);
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
          if (shortcuts) element.setAttribute("aria-keyshortcuts", shortcuts);
          else element.removeAttribute("aria-keyshortcuts");
          if (entry.command.reserved) element.setAttribute("aria-disabled", "true");
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
          disposeAll();
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
    const commands = createCommandRegistry();
    const workbench = { version: 1, commands: commands.api, debug: {
      setSignal, parseChord: commands.parseChord, refreshCommands: commands.decorateCommandElements,
    } };
    window.__gosx.commands = commands.api;
    window.__gosx.workbench = workbench;
    if (window.__gosx.host) window.__gosx.host.workbench = workbench;
    return {
      runtimeReady(manifest) { commands.bind(manifest && manifest.commands || []); },
      disposePage() { commands.unbind(); disposeAll(); },
    };
  });
})();
