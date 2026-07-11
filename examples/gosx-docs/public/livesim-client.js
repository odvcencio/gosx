// examples/gosx-docs/public/livesim-client.js
// Canvas renderer + WebSocket input sender for the Live Sim demo.
(function () {
  "use strict";

  var HUB_NAME = "livesim";
  var EVENT_TICK = "sim:tick";

  var canvas, ctx, hudFrame, hudCount, hudState, hudRender, spawnButton;
  var latestState = null;
  var previousState = null;
  var receivedAt = 0;
  var lastDrawAt = 0, renderFPS = 0, rafID = 0, mounted = false, intersecting = true;

  function mount() {
    canvas = document.getElementById("livesim-canvas");
    if (!canvas) return;
    ctx = canvas.getContext("2d");
    hudFrame = document.getElementById("livesim-frame");
    hudCount = document.getElementById("livesim-count");
    hudState = document.getElementById("livesim-state");
    hudRender = document.getElementById("livesim-render");
    spawnButton = document.getElementById("livesim-spawn");
    canvas.addEventListener("pointerdown", onPointer);
    canvas.addEventListener("keydown", onKeyDown);
    if (spawnButton) spawnButton.addEventListener("click", spawnCenter);
    document.addEventListener("gosx:hub:event", onHubEvent);
    document.addEventListener("visibilitychange", resumeIfVisible);
    document.addEventListener("gosx:navigate", teardown, { once: true });
    mounted = true;
    if (typeof IntersectionObserver === "function") {
      observer = new IntersectionObserver(function (entries) {
        intersecting = !!(entries[0] && entries[0].isIntersecting);
        resumeIfVisible();
      });
      observer.observe(canvas);
    }
    rafID = requestAnimationFrame(draw);
    if (hudState) hudState.textContent = "waiting…";
  }

  function onHubEvent(evt) {
    var detail = evt.detail;
    if (!detail || detail.hubName !== HUB_NAME) return;
    if (detail.event === "__welcome") { if (hudState) hudState.textContent = "connected · waiting"; return; }
    if (detail.event !== EVENT_TICK) return;

    var state = decodeState(detail.data);
    if (!state) return;

    previousState = latestState;
    latestState = state;
    receivedAt = performance.now();
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

  function onPointer(evt) {
    var rect = canvas.getBoundingClientRect();
    var scaleX = canvas.width / rect.width;
    var scaleY = canvas.height / rect.height;
    var x = Math.round((evt.clientX - rect.left) * scaleX);
    var y = Math.round((evt.clientY - rect.top) * scaleY);
    sendInput({ x: x, y: y });
  }

  function onKeyDown(evt) {
    if (evt.key !== "Enter" && evt.key !== " ") return;
    evt.preventDefault();
    spawnCenter();
  }

  function spawnCenter() { sendInput({ x: canvas.width / 2, y: canvas.height / 3 }); }

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

  function draw(now) {
    rafID = 0;
    if (!mounted || document.hidden || !intersecting) return;
    if (lastDrawAt) renderFPS = renderFPS * 0.9 + (1000 / (now - lastDrawAt)) * 0.1;
    lastDrawAt = now;
    if (hudRender) hudRender.textContent = Math.round(renderFPS) + " fps";
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
      var circles = interpolatedCircles(now);
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

    rafID = requestAnimationFrame(draw);
  }

  function interpolatedCircles(now) {
    var current = latestState.circles || [];
    if (!previousState) return current;
    var prior = previousState.circles || [];
    var byID = Object.create(null);
    for (var p = 0; p < prior.length; p++) byID[prior[p].id] = prior[p];
    var alpha = Math.max(0, Math.min(1, (now - receivedAt) / 50));
    return current.map(function (circle) {
      var old = byID[circle.id];
      if (!old) return circle;
      return { id: circle.id, x: old.x + (circle.x - old.x) * alpha, y: old.y + (circle.y - old.y) * alpha, r: circle.r };
    });
  }

  var observer = null;
  function resumeIfVisible() {
    if (mounted && !document.hidden && intersecting && !rafID) rafID = requestAnimationFrame(draw);
  }
  function teardown() {
    mounted = false;
    if (rafID) cancelAnimationFrame(rafID);
    document.removeEventListener("gosx:hub:event", onHubEvent);
    document.removeEventListener("visibilitychange", resumeIfVisible);
    canvas.removeEventListener("pointerdown", onPointer);
    canvas.removeEventListener("keydown", onKeyDown);
    if (spawnButton) spawnButton.removeEventListener("click", spawnCenter);
    if (observer) observer.disconnect();
  }

  // ─── Bootstrap ─────────────────────────────────────────────────────────────

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", mount);
  } else {
    mount();
  }
})();
