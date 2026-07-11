// examples/gosx-docs/public/livesim-client.js
// Canvas renderer + WebSocket input sender for the Live Sim demo.
(function () {
  "use strict";

  var HUB_NAME = "livesim";
  var EVENT_TICK = "sim:tick";
  var EVENT_CURSOR = "presence:cursor";
  var EVENT_LEAVE = "presence:leave";

  var canvas, ctx, hudFrame, hudCount, hudState, hudRender, hudViewers, spawnButton, burstButton;
  var latestState = null;
  var previousState = null;
  var receivedAt = 0;
  var lastDrawAt = 0, renderFPS = 0, rafID = 0, mounted = false, intersecting = true;

  // Own hub client ID, learned from "__welcome". Used to skip rendering a
  // ghost cursor for ourselves.
  var selfClientId = "";

  var reducedMotion = false;

  // id -> { x, y, name, color, lastSeen }. Other connected clients' pointer
  // positions, fed by "presence:cursor" broadcasts.
  var ghosts = Object.create(null);

  // circle id -> recent [{x,y}, ...] positions (oldest first, capped),
  // sampled once per network tick (not per animation frame) for a short
  // motion trail. Reduced-motion users get flat discs with no trail.
  var trails = Object.create(null);
  var TRAIL_LENGTH = 5;

  // Brief radial flashes for collision contacts and attributed spawns.
  // {x, y, color, createdAt, ttl, kind}
  var flashes = [];
  var FLASH_MAX = 64;
  var FLASH_TTL_CONTACT = 220;
  var FLASH_TTL_SPAWN = 360;

  // Speed (px/s) at which a circle reads as fully "hot". Below 0 it's calm.
  var HEAT_MAX_SPEED = 620;

  var lastCursorSentAt = 0;
  var CURSOR_SEND_INTERVAL_MS = 90;

  function mount() {
    canvas = document.getElementById("livesim-canvas");
    if (!canvas) return;
    ctx = canvas.getContext("2d");
    hudFrame = document.getElementById("livesim-frame");
    hudCount = document.getElementById("livesim-count");
    hudState = document.getElementById("livesim-state");
    hudRender = document.getElementById("livesim-render");
    hudViewers = document.getElementById("livesim-viewers");
    spawnButton = document.getElementById("livesim-spawn");
    burstButton = document.getElementById("livesim-burst");

    reducedMotion = !!(
      typeof window.matchMedia === "function" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches
    );

    canvas.addEventListener("pointerdown", onPointer);
    canvas.addEventListener("pointermove", onPointerMove);
    canvas.addEventListener("keydown", onKeyDown);
    if (spawnButton) spawnButton.addEventListener("click", spawnCenter);
    if (burstButton) burstButton.addEventListener("click", burst);
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

    if (detail.event === "__welcome") {
      if (detail.data && detail.data.clientId) selfClientId = String(detail.data.clientId);
      if (hudState) hudState.textContent = "connected · waiting";
      return;
    }
    if (detail.event === EVENT_CURSOR) {
      handleCursor(detail.data);
      return;
    }
    if (detail.event === EVENT_LEAVE) {
      handleLeave(detail.data);
      return;
    }
    if (detail.event !== EVENT_TICK) return;

    var state = decodeState(detail.data);
    if (!state) return;

    previousState = latestState;
    latestState = state;
    receivedAt = performance.now();
    if (hudFrame) hudFrame.textContent = String(detail.data.frame || state.frame || 0);
    if (hudCount) hudCount.textContent = String((state.circles || []).length);
    if (hudViewers) hudViewers.textContent = String(state.viewers != null ? state.viewers : 1);
    if (hudState) hudState.textContent = "live";

    updateTrails(state.circles || []);
    ingestContacts(state.contacts);
    ingestSpawns(state.spawns);
  }

  function handleCursor(data) {
    if (!data || !data.id || data.id === selfClientId) return;
    ghosts[data.id] = {
      x: data.x,
      y: data.y,
      name: data.name || "Guest",
      color: data.color || "#f59e0b",
      lastSeen: performance.now(),
    };
  }

  function handleLeave(data) {
    if (data && data.id) delete ghosts[data.id];
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

  // ─── Trails / contacts / spawns ingestion ────────────────────────────────

  function updateTrails(circles) {
    if (reducedMotion) return;
    var seen = Object.create(null);
    for (var i = 0; i < circles.length; i++) {
      var c = circles[i];
      seen[c.id] = true;
      var arr = trails[c.id];
      if (!arr) { arr = trails[c.id] = []; }
      arr.push({ x: c.x, y: c.y });
      if (arr.length > TRAIL_LENGTH) arr.shift();
    }
    for (var id in trails) {
      if (!seen[id]) delete trails[id];
    }
  }

  function ingestContacts(contacts) {
    if (reducedMotion || !contacts || !contacts.length) return;
    var now = performance.now();
    for (var i = 0; i < contacts.length; i++) {
      var ct = contacts[i];
      flashes.push({
        x: ct.x,
        y: ct.y,
        color: heatColor(ct.mag),
        createdAt: now,
        ttl: FLASH_TTL_CONTACT,
        kind: "contact",
      });
    }
    trimFlashes();
  }

  function ingestSpawns(spawns) {
    if (reducedMotion || !spawns || !spawns.length) return;
    var now = performance.now();
    for (var i = 0; i < spawns.length; i++) {
      var sp = spawns[i];
      var ghost = ghosts[sp.id];
      var color = ghost ? ghost.color : "#f59e0b";
      flashes.push({
        x: sp.x,
        y: sp.y,
        color: color,
        createdAt: now,
        ttl: sp.burst ? FLASH_TTL_SPAWN * 1.4 : FLASH_TTL_SPAWN,
        kind: "spawn",
        burst: !!sp.burst,
      });
    }
    trimFlashes();
  }

  function trimFlashes() {
    if (flashes.length > FLASH_MAX) flashes.splice(0, flashes.length - FLASH_MAX);
  }

  // ─── Input ────────────────────────────────────────────────────────────────

  function canvasPoint(evt) {
    var rect = canvas.getBoundingClientRect();
    var scaleX = canvas.width / rect.width;
    var scaleY = canvas.height / rect.height;
    return {
      x: Math.round((evt.clientX - rect.left) * scaleX),
      y: Math.round((evt.clientY - rect.top) * scaleY),
    };
  }

  function onPointer(evt) {
    var pt = canvasPoint(evt);
    sendInput({ x: pt.x, y: pt.y });
    sendCursor(pt.x, pt.y, true);
  }

  function onPointerMove(evt) {
    var pt = canvasPoint(evt);
    sendCursor(pt.x, pt.y, false);
  }

  function onKeyDown(evt) {
    if (evt.key !== "Enter" && evt.key !== " ") return;
    evt.preventDefault();
    spawnCenter();
  }

  function spawnCenter() { sendInput({ x: canvas.width / 2, y: canvas.height / 3 }); }

  // Burst spawns a server-chosen cluster (~15–20) of circles around a point
  // — the server decides the exact count and jitter, this button only asks
  // for a burst at a location. See livesim/game.go's Burst handling.
  function burst() { sendInput({ x: canvas.width / 2, y: canvas.height / 4, burst: true }); }

  function sendCursor(x, y, force) {
    var now = performance.now();
    if (!force && now - lastCursorSentAt < CURSOR_SEND_INTERVAL_MS) return;
    lastCursorSentAt = now;
    sendHubEvent("presence:move", { x: x, y: y });
  }

  function sendInput(data) { sendHubEvent("input", data); }

  function sendHubEvent(event, data) {
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
        record.socket.send(JSON.stringify({ event: event, data: data }));
      }
    });
  }

  // ─── Heat color ─────────────────────────────────────────────────────────────
  // Calm circles sit near the demo's amber accent; fast ones shift toward a
  // hotter red-orange — same warm family as --demo-accent-livesim, no
  // unrelated hues introduced.

  function speedOf(c) {
    var vx = c.vx || 0, vy = c.vy || 0;
    return Math.sqrt(vx * vx + vy * vy);
  }

  function heatColor(speed) {
    var t = Math.max(0, Math.min(1, speed / HEAT_MAX_SPEED));
    var hue = 38 - t * 24;   // 38° amber -> 14° red-orange
    var sat = 45 + t * 50;   // 45% -> 95%
    var light = 40 + t * 18; // 40% -> 58%
    return "hsl(" + hue.toFixed(1) + "," + sat.toFixed(0) + "%," + light.toFixed(0) + "%)";
  }

  // ─── Pre-rendered sphere-shading / contact-shadow sprites ────────────────
  // Built once per radius bucket, reused every frame — no per-ball gradient
  // construction in the render loop.

  var shadeSpriteCache = Object.create(null);
  function shadeSpriteFor(r) {
    var key = Math.max(4, Math.round(r));
    var cached = shadeSpriteCache[key];
    if (cached) return cached;
    var size = key * 2 + 4;
    var off = document.createElement("canvas");
    off.width = size;
    off.height = size;
    var octx = off.getContext("2d");
    var cx = size / 2, cy = size / 2;

    // Rim shadow, lower-right.
    var rim = octx.createRadialGradient(cx + key * 0.35, cy + key * 0.35, key * 0.15, cx, cy, key * 1.05);
    rim.addColorStop(0, "rgba(0,0,0,0)");
    rim.addColorStop(0.6, "rgba(0,0,0,0)");
    rim.addColorStop(1, "rgba(0,0,0,0.45)");
    octx.fillStyle = rim;
    octx.beginPath();
    octx.arc(cx, cy, key, 0, Math.PI * 2);
    octx.fill();

    // Highlight, upper-left.
    var hi = octx.createRadialGradient(cx - key * 0.35, cy - key * 0.4, 0, cx - key * 0.35, cy - key * 0.4, key * 0.9);
    hi.addColorStop(0, "rgba(255,255,255,0.55)");
    hi.addColorStop(0.5, "rgba(255,255,255,0.12)");
    hi.addColorStop(1, "rgba(255,255,255,0)");
    octx.fillStyle = hi;
    octx.beginPath();
    octx.arc(cx, cy, key, 0, Math.PI * 2);
    octx.fill();

    var sprite = { canvas: off, size: size };
    shadeSpriteCache[key] = sprite;
    return sprite;
  }

  var shadowSpriteCache = Object.create(null);
  function shadowSpriteFor(r) {
    var key = Math.max(4, Math.round(r));
    var cached = shadowSpriteCache[key];
    if (cached) return cached;
    var w = key * 2.6, h = key * 1.15;
    var off = document.createElement("canvas");
    off.width = Math.ceil(w);
    off.height = Math.ceil(h);
    var octx = off.getContext("2d");
    var g = octx.createRadialGradient(w / 2, h / 2, 0, w / 2, h / 2, w / 2);
    g.addColorStop(0, "rgba(0,0,0,0.38)");
    g.addColorStop(0.7, "rgba(0,0,0,0.16)");
    g.addColorStop(1, "rgba(0,0,0,0)");
    octx.fillStyle = g;
    octx.beginPath();
    octx.ellipse(w / 2, h / 2, w / 2, h / 2, 0, 0, Math.PI * 2);
    octx.fill();
    var sprite = { canvas: off, w: w, h: h };
    shadowSpriteCache[key] = sprite;
    return sprite;
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
      drawGrid(w, h);
      drawFloor(w, h);

      var circles = interpolatedCircles(now);

      drawTrails(circles);
      drawGroundShadows(circles, h);
      drawCircles(circles);
      drawFlashes(now);
      drawGhosts(now);

      ctx.globalAlpha = 1.0;
    }

    rafID = requestAnimationFrame(draw);
  }

  function drawGrid(w, h) {
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
  }

  function drawFloor(w, h) {
    ctx.strokeStyle = "#f59e0b";
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.moveTo(0, h - 1);
    ctx.lineTo(w, h - 1);
    ctx.stroke();
  }

  function drawTrails(circles) {
    if (reducedMotion) return;
    for (var i = 0; i < circles.length; i++) {
      var c = circles[i];
      var pts = trails[c.id];
      if (!pts || pts.length < 2) continue;
      var color = heatColor(speedOf(c));
      var n = pts.length;
      for (var j = 0; j < n - 1; j++) {
        var p = pts[j];
        var alpha = (0.2 * (j + 1)) / n;
        var rr = c.r * (0.3 + 0.5 * (j + 1) / n);
        ctx.globalAlpha = alpha;
        ctx.fillStyle = color;
        ctx.beginPath();
        ctx.arc(p.x, p.y, rr, 0, Math.PI * 2);
        ctx.fill();
      }
    }
    ctx.globalAlpha = 1.0;
  }

  function drawGroundShadows(circles, worldH) {
    var floorY = worldH - 1;
    for (var i = 0; i < circles.length; i++) {
      var c = circles[i];
      var lift = Math.max(0, floorY - (c.y + c.r));
      var alpha = Math.max(0.08, 0.4 - lift / 260);
      var scale = Math.max(0.45, 1 - lift / 320);
      var sprite = shadowSpriteFor(c.r);
      ctx.save();
      ctx.globalAlpha = alpha;
      ctx.translate(c.x, Math.min(c.y + c.r * 0.6, floorY - (sprite.h * scale) / 3));
      ctx.scale(scale, scale);
      ctx.drawImage(sprite.canvas, -sprite.w / 2, -sprite.h / 2);
      ctx.restore();
    }
    ctx.globalAlpha = 1.0;
  }

  function drawCircles(circles) {
    for (var i = 0; i < circles.length; i++) {
      var c = circles[i];
      var color = heatColor(speedOf(c));

      ctx.globalAlpha = 0.28;
      ctx.fillStyle = color;
      ctx.beginPath();
      ctx.arc(c.x, c.y, c.r, 0, Math.PI * 2);
      ctx.fill();

      ctx.globalAlpha = 1.0;
      ctx.strokeStyle = color;
      ctx.lineWidth = 1.5;
      ctx.beginPath();
      ctx.arc(c.x, c.y, c.r, 0, Math.PI * 2);
      ctx.stroke();

      var sprite = shadeSpriteFor(c.r);
      ctx.drawImage(sprite.canvas, c.x - sprite.size / 2, c.y - sprite.size / 2);
    }
    ctx.globalAlpha = 1.0;
  }

  function drawFlashes(now) {
    if (!flashes.length) return;
    var kept = [];
    for (var i = 0; i < flashes.length; i++) {
      var f = flashes[i];
      var age = now - f.createdAt;
      if (age > f.ttl) continue;
      kept.push(f);
      var t = age / f.ttl;
      var maxRadius = f.kind === "spawn" ? (f.burst ? 34 : 24) : 18;
      var radius = 6 + t * maxRadius;
      ctx.globalAlpha = Math.max(0, 0.6 * (1 - t));
      ctx.strokeStyle = f.color;
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.arc(f.x, f.y, radius, 0, Math.PI * 2);
      ctx.stroke();
    }
    ctx.globalAlpha = 1.0;
    flashes = kept;
  }

  function drawGhosts(now) {
    for (var id in ghosts) {
      var g = ghosts[id];
      if (now - g.lastSeen > 6000) {
        delete ghosts[id];
        continue;
      }
      ctx.globalAlpha = 0.85;
      ctx.fillStyle = g.color;
      ctx.beginPath();
      ctx.arc(g.x, g.y, 5, 0, Math.PI * 2);
      ctx.fill();

      ctx.globalAlpha = 0.5;
      ctx.strokeStyle = g.color;
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.arc(g.x, g.y, 9, 0, Math.PI * 2);
      ctx.stroke();

      ctx.globalAlpha = 0.9;
      ctx.font = "10px 'JetBrains Mono', monospace";
      ctx.fillStyle = g.color;
      ctx.fillText(g.name, g.x + 10, g.y - 8);
    }
    ctx.globalAlpha = 1.0;
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
      return {
        id: circle.id,
        x: old.x + (circle.x - old.x) * alpha,
        y: old.y + (circle.y - old.y) * alpha,
        r: circle.r,
        vx: circle.vx,
        vy: circle.vy,
      };
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
    canvas.removeEventListener("pointermove", onPointerMove);
    canvas.removeEventListener("keydown", onKeyDown);
    if (spawnButton) spawnButton.removeEventListener("click", spawnCenter);
    if (burstButton) burstButton.removeEventListener("click", burst);
    if (observer) observer.disconnect();
  }

  // ─── Bootstrap ─────────────────────────────────────────────────────────────

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", mount);
  } else {
    mount();
  }
})();
