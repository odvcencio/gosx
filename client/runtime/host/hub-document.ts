// @ts-check
// Opt-in text-document views over GoSX Hubs. Applications declare the binding
// in Go and render their own markup; this host owns browser state and disposal.
(function() {
  "use strict";
  const register = window.__gosx_register_bootstrap_feature;
  if (typeof register !== "function") return;
  register("hub-document", function() {
    let views = [];

    function mount(entry) {
      const config = entry.document;
      const root = document.getElementById(config.rootId);
      const source = root?.querySelector("textarea[data-gosx-document-source]");
      if (!root || !source) return null;
      const find = name => root.querySelector("[data-gosx-document-" + name + "]");
      const preview = find("preview"), cursors = find("cursors");
      const listeners = [], peers = new Map();
      const events = config.events;
      let version = config.version, serverText = source.value, lastVersion = -1;
      let previewText = source.value, changedBlocks = [];
      let layoutText = null, layoutRevision = 0, lineStarts = [];
      let disposed = false, dirty = false, composing = false, sent = null, selfID = "";
      let editTimer = null, cursorTimer = null, sweepTimer = null, flashTimer = null;
      let frame = null, sweepAt = 0, lastCursorAt = 0, syncSocket = null;
      const encoder = new TextEncoder();
      const canvas = document.createElement("canvas");
      const measure = canvas.getContext?.("2d");

      function listen(target, type, callback, options) {
        target.addEventListener(type, callback, options);
        listeners.push(() => target.removeEventListener(type, callback, options));
      }
      function text(name, value) {
        const node = find(name);
        if (node) node.textContent = String(value);
      }
      function status(value) { text("status", value); }
      function record() { return window.__gosx?.hubs?.get(entry.id); }
      function send(event, data) {
        const hub = record();
        if (!hub || hub.disposed || hub.suspended || hub.socket?.readyState !== 1) return false;
        try { hub.socket.send(JSON.stringify({event, data})); return true; }
        catch (_) { return false; }
      }
      function sync() {
        const socket = record()?.socket;
        if (socket && socket !== syncSocket && send(events.sync, {})) syncSocket = socket;
      }
      function connected(open) {
        root.setAttribute("data-gosx-document-connected", open ? "true" : "false");
        if (open) { sync(); if (!dirty) status(lastVersion >= 0 ? "synced" : "connected"); }
        else { sent = null; status(dirty ? "offline · edit kept locally" : "reconnecting…"); }
      }

      function sendEdit() {
        if (editTimer != null) clearTimeout(editTimer);
        editTimer = null;
        if (disposed || composing || !dirty) return;
        const value = source.value;
        if (encoder.encode(value).byteLength > config.maxTextBytes) { status("document is too large"); return; }
        if (sent && sent.text === value && sent.version === version) return;
        if (send(events.edit, {text: value, version})) {
          sent = {text: value, version};
          status("pending");
        } else status("offline · edit kept locally");
      }
      function scheduleEdit() {
        if (editTimer != null) clearTimeout(editTimer);
        editTimer = null;
        if (!composing && dirty) editTimer = setTimeout(sendEdit, config.debounceMs);
      }
      function localInput() {
        dirty = source.value !== serverText;
        if (!dirty) sent = null;
        status(dirty ? "pending" : "synced");
        scheduleEdit(); schedulePositions(); scheduleCursor();
      }
      function cursor() {
        cursorTimer = null;
        if (disposed || composing) return;
        lastCursorAt = Date.now();
        send(events.cursor, {offset: source.selectionStart, selEnd: source.selectionEnd});
      }
      function scheduleCursor() {
        if (composing || cursorTimer != null) return;
        const delay = config.cursorThrottleMs - (Date.now() - lastCursorAt);
        if (delay <= 0) cursor(); else cursorTimer = setTimeout(cursor, delay);
      }

      // Best-effort selection mapping for a last-write-wins replacement. This
      // preserves positions outside the changed range, including UTF-16 offsets.
      function replaceText(value) {
        const previous = source.value;
        if (previous === value) return;
        let prefix = 0, suffix = 0;
        while (prefix < previous.length && prefix < value.length && previous[prefix] === value[prefix]) prefix++;
        while (suffix < previous.length - prefix && suffix < value.length - prefix && previous[previous.length - 1 - suffix] === value[value.length - 1 - suffix]) suffix++;
        const map = offset => Math.min(value.length, offset <= prefix ? offset : offset >= previous.length - suffix ? offset + value.length - previous.length : value.length - suffix);
        const start = map(source.selectionStart), end = map(source.selectionEnd), direction = source.selectionDirection;
        const top = source.scrollTop, left = source.scrollLeft;
        source.value = value;
        source.setSelectionRange(start, end, direction);
        source.scrollTop = top; source.scrollLeft = left;
      }
      function snapshot(value) {
        if (!value || typeof value.text !== "string" || !Number.isSafeInteger(value.version) || value.version < version) return;
        if (value.version === lastVersion && value.text !== serverText) return;
        if (encoder.encode(value.text).byteLength > config.maxTextBytes) return;
        const changed = value.text !== serverText;
        version = value.version; lastVersion = version; serverText = value.text;
        text("version", version);
        // A delayed acknowledgement or a remote edit cannot erase newer local
        // input. Rebase its next submission onto the authoritative version.
        if (composing || (dirty && source.value !== value.text)) {
          sent = null; scheduleEdit(); status("pending"); return;
        }
        dirty = false; sent = null;
        if (editTimer != null) clearTimeout(editTimer);
        editTimer = null;
        const remote = source.value !== value.text;
        replaceText(value.text);
        if (preview) {
          if (typeof value.html === "string") preview.innerHTML = value.html;
          else preview.textContent = value.text;
        }
        status("synced");
        if (changed && remote) {
          for (const block of changedBlocks) block.removeAttribute("data-gosx-document-changed");
          changedBlocks = [];
          const oldLines = previewText.split("\n"), newLines = value.text.split("\n");
          let first = 0;
          while (first < oldLines.length && first < newLines.length && oldLines[first] === newLines[first]) first++;
          let oldEnd = oldLines.length - 1, newEnd = newLines.length - 1;
          while (oldEnd > first && newEnd > first && oldLines[oldEnd] === newLines[newEnd]) { oldEnd--; newEnd--; }
          if (preview) for (const block of preview.querySelectorAll("[data-gosx-document-line-start], [data-mdpp-source-line]")) {
            const start = Number(block.getAttribute("data-gosx-document-line-start") || block.getAttribute("data-mdpp-source-line"));
            const end = Number(block.getAttribute("data-gosx-document-line-end") || block.getAttribute("data-mdpp-source-end-line"));
            if (start <= newEnd + 1 && end >= first + 1) {
              block.setAttribute("data-gosx-document-changed", "true"); changedBlocks.push(block);
            }
          }
          root.setAttribute("data-gosx-document-updated", "true");
          if (flashTimer != null) clearTimeout(flashTimer);
          flashTimer = setTimeout(() => {
            flashTimer = null; root.removeAttribute("data-gosx-document-updated");
            for (const block of changedBlocks) block.removeAttribute("data-gosx-document-changed");
            changedBlocks = [];
          }, 1000);
        }
        previewText = value.text;
        schedulePositions();
      }

      function removePeer(id) {
        const peer = peers.get(id);
        if (peer) { peer.node.remove(); peers.delete(id); }
      }
      function armSweep() {
        if (!peers.size) {
          if (sweepTimer != null) clearTimeout(sweepTimer);
          sweepTimer = null; sweepAt = 0; return;
        }
        let next = Infinity;
        for (const peer of peers.values()) next = Math.min(next, peer.seen + config.cursorStaleMs);
        if (sweepTimer != null && next >= sweepAt) return;
        if (sweepTimer != null) clearTimeout(sweepTimer);
        sweepAt = next;
        sweepTimer = setTimeout(() => {
          sweepTimer = null; sweepAt = 0;
          const now = Date.now();
          for (const [id, peer] of peers) if (now - peer.seen >= config.cursorStaleMs) removePeer(id);
          armSweep();
        }, Math.max(0, next - Date.now()));
      }
      function color(value) {
        return typeof value === "string" && /^#[0-9a-f]{3,8}$/i.test(value) ? value : "#9ca3af";
      }
      function peer(value) {
        if (!cursors || !value || typeof value.id !== "string" || value.id === selfID || !Number.isSafeInteger(value.offset) || value.offset < 0) return;
        let current = peers.get(value.id);
        if (!current) {
          // Bound DOM work even if a malformed server sends an unbounded roster.
          if (peers.size >= 128) return;
          const node = document.createElement("span");
          node.setAttribute("data-gosx-document-caret", "");
          node.setAttribute("aria-hidden", "true");
          cursors.appendChild(node);
          current = {node, offset: 0, seen: 0}; peers.set(value.id, current);
        }
        current.offset = value.offset; current.seen = Date.now();
        current.node.setAttribute("data-label", String(value.name || "Editor"));
        current.node.style.setProperty("--gosx-document-cursor-color", color(value.color));
        armSweep(); schedulePositions();
      }
      function positionPeers() {
        frame = null;
        if (disposed || !peers.size) return;
        const style = getComputedStyle(source);
        const height = parseFloat(style.lineHeight) || (parseFloat(style.fontSize) || 16) * 1.2;
        const top = parseFloat(style.paddingTop) || 0, left = parseFloat(style.paddingLeft) || 0;
        const font = style.font || style.fontSize + " " + style.fontFamily;
        if (measure) measure.font = font;
        const text = source.value;
        if (layoutText !== text) {
          layoutText = text; layoutRevision++; lineStarts = [0];
          for (let at = text.indexOf("\n"); at >= 0; at = text.indexOf("\n", at + 1)) lineStarts.push(at + 1);
        }
        for (const current of peers.values()) {
          const offset = Math.min(current.offset, text.length);
          let layout = current.layout;
          if (!layout || layout.revision !== layoutRevision || layout.offset !== offset || layout.font !== font) {
            let low = 0, high = lineStarts.length;
            while (low + 1 < high) {
              const middle = (low + high) >>> 1;
              if (lineStarts[middle] <= offset) low = middle; else high = middle;
            }
            const line = text.slice(lineStarts[low], offset);
            const width = measure ? measure.measureText(line).width : line.length * (parseFloat(style.fontSize) || 16) * 0.6;
            layout = {revision: layoutRevision, offset, font, row: low, width}; current.layout = layout;
          }
          current.node.style.left = (left + layout.width - source.scrollLeft) + "px";
          current.node.style.top = (top + layout.row * height - source.scrollTop) + "px";
          current.node.style.height = height + "px";
        }
      }
      function schedulePositions() {
        if (frame == null && peers.size) frame = requestAnimationFrame(positionPeers);
      }
      function hubEvent(event) {
        const message = event.detail;
        if (disposed || !message || message.hubID !== entry.id) return;
        const value = message.data;
        switch (message.event) {
          case "__connection": connected(value?.state === "open"); break;
          case "__welcome": connected(true); break;
          case events.update: snapshot(value); break;
          case events.cursor: peer(value); break;
          case events.roster: if (Array.isArray(value)) for (const item of value.slice(0, 128)) peer(item); break;
          case events.leave: if (value?.id) { removePeer(value.id); armSweep(); } break;
          case events.presence:
            if (Number.isSafeInteger(value?.count) && value.count >= 0) text("presence", value.count + (value.count === 1 ? " editor" : " editors"));
            break;
          case events.self:
            if (typeof value?.id === "string") {
              selfID = value.id; removePeer(selfID); armSweep();
              text("identity", "You: " + String(value.name || "Editor"));
              const identity = find("identity");
              if (identity) identity.style.color = color(value.color);
            }
            break;
        }
      }
      listen(document, "gosx:hub:event", hubEvent);
      // Navigation announces pending state before disposing shared transports.
      // Commit a debounced edit while this page still owns its open socket.
      listen(document, "gosx:navigation:state", event => {
        if (event.detail?.state?.phase === "pending") sendEdit();
      });
      listen(source, "input", localInput);
      listen(source, "compositionstart", () => { composing = true; if (editTimer != null) clearTimeout(editTimer); editTimer = null; });
      listen(source, "compositionend", () => { composing = false; localInput(); });
      for (const type of ["keyup", "mouseup", "select", "focus"]) listen(source, type, scheduleCursor);
      listen(source, "scroll", schedulePositions, {passive: true});
      listen(window, "resize", schedulePositions, {passive: true});
      listen(root, "click", event => {
        const button = event.target?.closest?.("button[data-gosx-document-view]");
        if (!button || !root.contains(button)) return;
        root.setAttribute("data-gosx-document-view", button.getAttribute("data-gosx-document-view"));
        for (const item of root.querySelectorAll("button[data-gosx-document-view]")) item.setAttribute("aria-pressed", item === button ? "true" : "false");
      });
      // All feature factories finish mounting in this turn, including hubs.
      // A reused connection has no new open event, so explicitly resynchronize.
      queueMicrotask(() => { if (!disposed && record()?.socket?.readyState === 1) connected(true); });
      return {
        dispose() {
          if (disposed) return;
          if (editTimer != null) clearTimeout(editTimer);
          sendEdit(); disposed = true;
          for (const remove of listeners) remove();
          for (const timer of [editTimer, cursorTimer, sweepTimer, flashTimer]) if (timer != null) clearTimeout(timer);
          if (frame != null) cancelAnimationFrame(frame);
          for (const id of peers.keys()) removePeer(id);
          for (const block of changedBlocks) block.removeAttribute("data-gosx-document-changed");
          root.removeAttribute("data-gosx-document-updated");
        },
      };
    }
    function dispose() { for (const view of views) view.dispose(); views = []; }
    return {
      runtimeReady(manifest) {
        dispose();
        for (const entry of manifest?.hubs || []) if (entry.document) {
          const view = mount(entry); if (view) views.push(view);
        }
      },
      disposePage: dispose,
    };
  });
})();
