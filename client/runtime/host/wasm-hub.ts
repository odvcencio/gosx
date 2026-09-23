// @ts-check
// Raw WebSocket transport for Go-WASM engines. The framework owns the browser
// socket; a Go callback receives each text frame once and decodes the envelope.
// This deliberately does not join the declarative hub's JSON/DOM event path.
(function() {
  if (typeof window === "undefined") return;
  const host = window.__gosx && window.__gosx.host;
  if (!host) return;
  const defaultMaxFrameBytes = 8 * 1024 * 1024;

  function frameBytes(value) {
    let bytes = 0;
    for (let i = 0; i < value.length; i++) {
      const code = value.charCodeAt(i);
      if (code < 0x80) bytes++;
      else if (code < 0x800) bytes += 2;
      else if (code >= 0xD800 && code <= 0xDBFF && i + 1 < value.length &&
               value.charCodeAt(i + 1) >= 0xDC00 && value.charCodeAt(i + 1) <= 0xDFFF) {
        bytes += 4;
        i++;
      } else bytes += 3;
    }
    return bytes;
  }

  function withinLimit(value, limit) {
    if (value.length > limit) return false;
    // Most hub snapshots are ASCII and far below the cap. A UTF-16 code unit
    // can contribute at most three UTF-8 bytes, so this avoids scanning them.
    return value.length * 3 <= limit || frameBytes(value) <= limit;
  }

  function open(path, maxBytes, notify) {
    if (typeof WebSocket !== "function" || typeof notify !== "function") {
      throw new Error("GoSX WASM hub transport unavailable");
    }
    const rawPath = String(path || "");
    const url = /^(wss?:\/\/)/i.test(rawPath) ? rawPath
      : (window.location && window.location.protocol === "https:" ? "wss://" : "ws://") +
        (window.location && window.location.host || "") + (rawPath.startsWith("/") ? rawPath : "/" + rawPath);
    const limit = Math.min(defaultMaxFrameBytes, Math.max(1, Math.floor(Number(maxBytes) || defaultMaxFrameBytes)));
    const socket = new WebSocket(url);
    let active = true;
    const dispatch = function(kind, raw, code, reason) {
      if (active) notify(kind, raw, code, reason);
    };
    socket.onopen = function() { dispatch("open", "", 0, ""); };
    socket.onmessage = function(event) {
      if (!active || !event || typeof event.data !== "string") return;
      if (!withinLimit(event.data, limit)) {
        dispatch("error", "frame-too-large", 0, "");
        socket.close(4009, "frame too large");
        return;
      }
      dispatch("message", event.data, 0, "");
    };
    socket.onerror = function() { dispatch("error", "socket-error", 0, ""); };
    socket.onclose = function(event) {
      dispatch("close", "", Number(event && event.code || 0), String(event && event.reason || ""));
    };
    return {
      send(raw) {
        if (!active || socket.readyState !== 1 || typeof raw !== "string" || !withinLimit(raw, limit)) return false;
        try { socket.send(raw); return true; } catch (_) { return false; }
      },
      state() { return active ? socket.readyState : 3; },
      close(code, reason) {
        if (!active || socket.readyState >= 2) return;
        try {
          if (Number.isInteger(code) && (code === 1000 || code >= 3000 && code <= 4999)) socket.close(code, String(reason || ""));
          else socket.close();
        } catch (_) { try { socket.close(); } catch (_ignored) {} }
      },
      dispose() {
        if (!active) return;
        active = false;
        socket.onopen = socket.onmessage = socket.onerror = socket.onclose = null;
        if (socket.readyState < 2) { try { socket.close(); } catch (_) {} }
      },
    };
  }
  host.wasmHub = { open };
})();
