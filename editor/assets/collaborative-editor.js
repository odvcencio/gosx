(function () {
  "use strict";
  const forms = Array.from(document.querySelectorAll("form[data-collaboration-hub]"));
  for (const form of forms) mount(form);

  function mount(form) {
    const source = form.querySelector("textarea[name=content]");
    if (!source) return;
    const cfg = form.dataset;
    let socket, timer, reconnect, closed = false, revision = 0, localDirty = false;
    const cursors = new Map();
    const status = document.createElement("div");
    status.className = "editor-collaboration-status";
    status.setAttribute("aria-live", "polite");
    source.parentElement.appendChild(status);

    const wsURL = () => {
      const raw = cfg.collaborationHub || "";
      if (/^wss?:/.test(raw)) return raw;
      return (location.protocol === "https:" ? "wss://" : "ws://") + location.host + (raw[0] === "/" ? raw : "/" + raw);
    };
    const send = (event, data) => {
      if (!socket || socket.readyState !== WebSocket.OPEN) return false;
      socket.send(JSON.stringify({event: event, data: data || {}}));
      return true;
    };
    const connect = () => {
      if (closed) return;
      socket = new WebSocket(wsURL());
      socket.addEventListener("open", () => { reconnect = 250; status.textContent = "Collaborative · connected"; });
      socket.addEventListener("message", event => {
        if (typeof event.data !== "string") return;
        let message; try { message = JSON.parse(event.data); } catch (_) { return; }
        let data = message.data; if (typeof data === "string") try { data = JSON.parse(data); } catch (_) { return; }
        if (message.event === cfg.collaborationUpdateEvent) applySnapshot(data);
        if (message.event === cfg.collaborationCursorEvent) applyCursor(data || {});
      });
      socket.addEventListener("close", () => { status.textContent = "Collaborative · reconnecting"; clearTimeout(timer); timer = setTimeout(connect, reconnect || 250); reconnect = Math.min((reconnect || 250) * 2, 8000); });
    };
    const applySnapshot = snapshot => {
      if (!snapshot || snapshot.id !== cfg.collaborationCell || Number(snapshot.revision || 0) < revision) return;
      const file = (snapshot.files || []).find(item => item.path === cfg.collaborationPath);
      if (!file) return;
      revision = Number(snapshot.revision || revision);
      if (file.content === source.value) {
        localDirty = false;
        return;
      }
      // A server snapshot can race a browser typing burst. Preserve the local
      // buffer until the server echoes that exact content; the next debounced
      // publish carries the complete authoritative human edit.
      if (localDirty) return;
      const start = source.selectionStart, end = source.selectionEnd;
      source.value = file.content;
      source.setSelectionRange(Math.min(start, source.value.length), Math.min(end, source.value.length));
      source.dispatchEvent(new Event("gosx:remote-input", {bubbles: true}));
    };
    const publishEdit = () => send(cfg.collaborationEditEvent, {cellID: cfg.collaborationCell, path: cfg.collaborationPath, content: source.value});
    const publishCursor = () => send(cfg.collaborationCursorEvent, {cellID: cfg.collaborationCell, path: cfg.collaborationPath, start: source.selectionStart, end: source.selectionEnd});
    const applyCursor = data => {
      if (data.cellID !== cfg.collaborationCell || data.path !== cfg.collaborationPath || !data.clientID) return;
      cursors.set(data.clientID, data); status.textContent = "Collaborative · " + cursors.size + " remote cursor" + (cursors.size === 1 ? "" : "s");
      form.dispatchEvent(new CustomEvent("gosx:remote-cursor", {detail: data}));
    };
    source.addEventListener("input", () => { localDirty = true; clearTimeout(timer); timer = setTimeout(publishEdit, 60); });
    source.addEventListener("select", publishCursor); source.addEventListener("keyup", publishCursor); source.addEventListener("pointerup", publishCursor);
    source.addEventListener("focus", () => send(cfg.collaborationFocusEvent, {cellID: cfg.collaborationCell, path: cfg.collaborationPath, focused: true}));
    source.addEventListener("blur", () => send(cfg.collaborationFocusEvent, {cellID: cfg.collaborationCell, path: cfg.collaborationPath, focused: false}));
    window.addEventListener("pagehide", () => { closed = true; if (socket) socket.close(); }, {once: true});
    connect();
  }
})();
