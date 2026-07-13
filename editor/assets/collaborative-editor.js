(function () {
  "use strict";
  const forms = Array.from(document.querySelectorAll("form[data-collaboration-hub]"));
  for (const form of forms) mount(form);

  function mount(form) {
    const source = form.querySelector("textarea[name=content]");
    if (!source) return;
    const cfg = form.dataset;
    let socket, editTimer, reconnectTimer, rotationTimer, reconnect, closed = false, revision = 0, localDirty = false;
    let lastPublished = source.value;
    const cursors = new Map();
    const status = document.createElement("div");
    status.className = "editor-collaboration-status";
    status.setAttribute("aria-live", "polite");
    source.parentElement.appendChild(status);

    const wsURL = capability => {
      const raw = cfg.collaborationHub || "";
      const value = /^wss?:/.test(raw) ? raw : (location.protocol === "https:" ? "wss://" : "ws://") + location.host + (raw[0] === "/" ? raw : "/" + raw);
      const url = new URL(value);
      if (capability) url.searchParams.set("capability", capability);
      return url.toString();
    };
    const send = (event, data) => {
      if (!socket || socket.readyState !== WebSocket.OPEN) return false;
      socket.send(JSON.stringify({event: event, data: data || {}}));
      return true;
    };
    const connect = async () => {
      if (closed) return;
      let capability = "";
      if (cfg.collaborationCapabilityUrl) {
        try {
          const response = await fetch(cfg.collaborationCapabilityUrl, {credentials: "same-origin", cache: "no-store"});
          if (!response.ok) throw new Error("capability request failed");
          const grant = await response.json();
          capability = String(grant.token || "");
          const expiresAt = Number(grant.expiresAt || 0) * 1000;
          clearTimeout(rotationTimer);
          if (expiresAt > Date.now()) {
            rotationTimer = setTimeout(() => {
              if (socket) socket.close(4001, "capability rotation");
            }, Math.max(1000, expiresAt - Date.now() - 15000));
          }
        } catch (_) {
          status.textContent = "Collaborative · authorization failed";
          clearTimeout(reconnectTimer); reconnectTimer = setTimeout(connect, reconnect || 250); reconnect = Math.min((reconnect || 250) * 2, 8000);
          return;
        }
      }
      socket = new WebSocket(wsURL(capability));
      socket.addEventListener("open", () => { reconnect = 250; status.textContent = "Collaborative · connected"; });
      socket.addEventListener("message", event => {
        if (typeof event.data !== "string") return;
        let message; try { message = JSON.parse(event.data); } catch (_) { return; }
        let data = message.data; if (typeof data === "string") try { data = JSON.parse(data); } catch (_) { return; }
        if (message.event === cfg.collaborationUpdateEvent) applySnapshot(data);
        if (message.event === cfg.collaborationCursorEvent) applyCursor(data || {});
      });
      socket.addEventListener("close", () => { status.textContent = "Collaborative · reconnecting"; clearTimeout(reconnectTimer); reconnectTimer = setTimeout(connect, reconnect || 250); reconnect = Math.min((reconnect || 250) * 2, 8000); });
    };
    const applySnapshot = snapshot => {
      if (!snapshot || snapshot.id !== cfg.collaborationCell || Number(snapshot.revision || 0) < revision) return;
      const file = (snapshot.files || []).find(item => item.path === cfg.collaborationPath);
      if (!file) return;
      revision = Number(snapshot.revision || revision);
      if (file.content === source.value) {
        localDirty = false;
        lastPublished = file.content;
        return;
      }
      // A server snapshot can race a browser typing burst. Preserve the local
      // buffer until the server echoes that exact content; the next debounced
      // publish carries the complete authoritative human edit.
      if (localDirty) return;
      const start = source.selectionStart, end = source.selectionEnd;
      source.value = file.content;
      lastPublished = file.content;
      source.setSelectionRange(Math.min(start, source.value.length), Math.min(end, source.value.length));
      source.dispatchEvent(new Event("gosx:remote-input", {bubbles: true}));
    };
    const minimalSplice = (before, after) => {
      const oldRunes = Array.from(before), newRunes = Array.from(after);
      let start = 0;
      while (start < oldRunes.length && start < newRunes.length && oldRunes[start] === newRunes[start]) start++;
      let oldEnd = oldRunes.length, newEnd = newRunes.length;
      while (oldEnd > start && newEnd > start && oldRunes[oldEnd - 1] === newRunes[newEnd - 1]) { oldEnd--; newEnd--; }
      return {index: start, deleteCount: oldEnd - start, insert: newRunes.slice(start, newEnd).join("")};
    };
    const encodeSplice = splice => {
      const encoder = new TextEncoder();
      const path = encoder.encode(cfg.collaborationPath || "");
      const insert = encoder.encode(splice.insert);
      if (path.length > 65535) throw new Error("collaboration path is too long");
      const frame = new ArrayBuffer(22 + path.length + insert.length);
      const view = new DataView(frame);
      view.setUint8(0, 0x4d); view.setUint8(1, 0x58); view.setUint8(2, 0x53); view.setUint8(3, 0x50);
      view.setUint16(4, path.length, false);
      view.setUint32(6, revision >>> 0, false);
      view.setUint32(10, splice.index >>> 0, false);
      view.setUint32(14, splice.deleteCount >>> 0, false);
      view.setUint32(18, insert.length >>> 0, false);
      new Uint8Array(frame, 22, path.length).set(path);
      new Uint8Array(frame, 22 + path.length, insert.length).set(insert);
      return frame;
    };
    const publishEdit = () => {
      if (cfg.collaborationBinarySplices === "true") {
        if (!socket || socket.readyState !== WebSocket.OPEN) return false;
        const splice = minimalSplice(lastPublished, source.value);
        if (splice.deleteCount === 0 && splice.insert === "") return true;
        socket.send(encodeSplice(splice));
        lastPublished = source.value;
        return true;
      }
      return send(cfg.collaborationEditEvent, {cellID: cfg.collaborationCell, path: cfg.collaborationPath, content: source.value});
    };
    const publishCursor = () => send(cfg.collaborationCursorEvent, {cellID: cfg.collaborationCell, path: cfg.collaborationPath, start: source.selectionStart, end: source.selectionEnd});
    const applyCursor = data => {
      if (data.cellID !== cfg.collaborationCell || data.path !== cfg.collaborationPath || !data.clientID) return;
      cursors.set(data.clientID, data); status.textContent = "Collaborative · " + cursors.size + " remote cursor" + (cursors.size === 1 ? "" : "s");
      form.dispatchEvent(new CustomEvent("gosx:remote-cursor", {detail: data}));
    };
    source.addEventListener("input", () => { localDirty = true; clearTimeout(editTimer); editTimer = setTimeout(publishEdit, 60); });
    source.addEventListener("select", publishCursor); source.addEventListener("keyup", publishCursor); source.addEventListener("pointerup", publishCursor);
    source.addEventListener("focus", () => send(cfg.collaborationFocusEvent, {cellID: cfg.collaborationCell, path: cfg.collaborationPath, focused: true}));
    source.addEventListener("blur", () => send(cfg.collaborationFocusEvent, {cellID: cfg.collaborationCell, path: cfg.collaborationPath, focused: false}));
    window.addEventListener("pagehide", () => { closed = true; clearTimeout(editTimer); clearTimeout(reconnectTimer); clearTimeout(rotationTimer); if (socket) socket.close(); }, {once: true});
    connect();
  }
})();
