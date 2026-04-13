// examples/gosx-docs/public/livesim-client.js
// Canvas renderer + WebSocket input sender for the Live Sim demo.
(function () {
  "use strict";

  var HUB_NAME = "livesim";
  var EVENT_TICK = "sim:tick";

  var canvas, ctx, hudFrame, hudCount, hudState;
  var latestState = null;

  function mount() {
    canvas = document.getElementById("livesim-canvas");
    if (!canvas) return;
    ctx = canvas.getContext("2d");
    hudFrame = document.getElementById("livesim-frame");
    hudCount = document.getElementById("livesim-count");
    hudState = document.getElementById("livesim-state");
    canvas.addEventListener("click", onClick);
    document.addEventListener("gosx:hub:event", onHubEvent);
    requestAnimationFrame(draw);
    if (hudState) hudState.textContent = "waiting…";
  }

  function onHubEvent(evt) {
    var detail = evt.detail;
    if (!detail || detail.hubName !== HUB_NAME) return;
    if (detail.event !== EVENT_TICK) return;

    var state = decodeState(detail.data);
    if (!state) return;

    latestState = state;
    if (hudFrame) hudFrame.textContent = String(detail.data.frame || state.frame || 0);
    if (hudCount) hudCount.textContent = String((state.circles || []).length);
    if (hudState) hudState.textContent = "live";
  }

  function decodeState(payload) {
    if (!payload) return null;
    var stateField = payload.state;
    // Runner broadcasts state as []byte which JSON-marshals to base64.
    if (typeof stateField === "string") {
      try {
        var decoded = atob(stateField);
        return JSON.parse(decoded);
      } catch (e) {
        // Maybe it's not base64 after all — try direct parse.
        try { return JSON.parse(stateField); } catch (_) {}
        console.warn("[livesim] failed to parse state:", e);
        return null;
      }
    }
    if (stateField && typeof stateField === "object") return stateField;
    return null;
  }

  function onClick(evt) {
    var rect = canvas.getBoundingClientRect();
    var scaleX = canvas.width / rect.width;
    var scaleY = canvas.height / rect.height;
    var x = Math.round((evt.clientX - rect.left) * scaleX);
    var y = Math.round((evt.clientY - rect.top) * scaleY);
    sendInput({ x: x, y: y });
  }

  function sendInput(data) {
    // Reach the hub WebSocket via the global __gosx.hubs map that
    // bootstrap-feature-hubs.js establishes. Each entry has .socket and
    // .entry (with .name).
    var hubs = window.__gosx && window.__gosx.hubs;
    if (!hubs) return;
    hubs.forEach(function (record) {
      if (
        record &&
        record.entry &&
        record.entry.name === HUB_NAME &&
        record.socket &&
        record.socket.readyState === 1
      ) {
        record.socket.send(JSON.stringify({ event: "input", data: data }));
      }
    });
  }

  // ─── Renderer ──────────────────────────────────────────────────────────────

  function draw() {
    if (ctx && latestState) {
      var w = canvas.width;
      var h = canvas.height;

      ctx.clearRect(0, 0, w, h);

      // Soft amber grid.
      ctx.strokeStyle = "rgba(245, 158, 11, 0.08)";
      ctx.lineWidth = 1;
      for (var x = 0; x <= w; x += 40) {
        ctx.beginPath();
        ctx.moveTo(x, 0);
        ctx.lineTo(x, h);
        ctx.stroke();
      }
      for (var y = 0; y <= h; y += 40) {
        ctx.beginPath();
        ctx.moveTo(0, y);
        ctx.lineTo(w, y);
        ctx.stroke();
      }

      // Floor line.
      ctx.strokeStyle = "#f59e0b";
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.moveTo(0, h - 1);
      ctx.lineTo(w, h - 1);
      ctx.stroke();

      // Circles.
      var circles = latestState.circles || [];
      for (var i = 0; i < circles.length; i++) {
        var c = circles[i];
        ctx.globalAlpha = 0.25;
        ctx.fillStyle = "#f59e0b";
        ctx.beginPath();
        ctx.arc(c.x, c.y, c.r, 0, Math.PI * 2);
        ctx.fill();

        ctx.globalAlpha = 1.0;
        ctx.strokeStyle = "#f59e0b";
        ctx.lineWidth = 1.5;
        ctx.beginPath();
        ctx.arc(c.x, c.y, c.r, 0, Math.PI * 2);
        ctx.stroke();
      }

      ctx.globalAlpha = 1.0;
    }

    requestAnimationFrame(draw);
  }

  // ─── Bootstrap ─────────────────────────────────────────────────────────────

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", mount);
  } else {
    mount();
  }
})();
