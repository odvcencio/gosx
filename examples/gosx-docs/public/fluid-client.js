// examples/gosx-docs/public/fluid-client.js
// Particle advection renderer for the Fluid demo.
// Subscribes to the "fluid" hub, decodes Quantized frames from field.PublishField,
// and drives 8000 CPU particles against the decoded velocity slice each rAF tick.
//
// Rendering: particles are trilinearly sampled against the decoded field (see
// sampleXYMiddleSlice) and drawn as small additive "glow" dots — a low-alpha
// halo square plus a crisp core square, both composited with
// globalCompositeOperation "lighter" so overlapping fast-moving particles
// visibly brighten. Draws are batched by a quantized speed→color bucket
// (built into one canvas path per bucket, then a single fill()/stroke() call)
// rather than per-particle fillStyle churn or save/restore, to keep 8000
// particles/frame cheap.
(function () {
  "use strict";

  var HUB_NAME = "fluid";
  var EVENT_NAME = "field:velocity"; // fieldEventPrefix + topic from field/stream.go

  // ── State ────────────────────────────────────────────────────────────────────

  var canvas, ctx;
  var hudEls = {};

  var previousField = null; // Float32Array — delta base (latest reconstructed absolute field)
  var currentField = null;  // Float32Array — in-use field
  var fieldResolution = [0, 0, 0];
  var fieldComponents = 0;
  var fieldBounds = null;

  var tick = 0;
  var lastFrameTime = 0;
  var fps = 0;
  var wireBytesEma = 0;
  var lastStreamAt = 0;
  var streamHz = 0;
  var sampleVX = 0, sampleVY = 0;
  var rafID = 0, visible = true, intersecting = true, mounted = false;
  var connectionTimer = 0;
  var canvasBg = "#0b0b0d"; // resolved from --demo-bg at mount time

  var REDUCED_MOTION = false;

  // ── Particles ────────────────────────────────────────────────────────────────

  var PARTICLE_COUNT = 8000;
  var SPEED_SCALE = 60; // pixels per (world unit / second)
  var SMOOTHING = 0.6;  // EMA weight kept from the previous sampled velocity

  // Parallel typed arrays (no per-particle objects, no per-particle GC churn).
  var posX, posY;     // current position
  var prevX, prevY;   // previous frame's position (drives streak segments)
  var velX, velY;     // EMA-smoothed field-sourced velocity (never includes the pointer nudge — see impulse note below)
  var velPrimed = false; // false until the first field arrives, so particles snap to it instead of easing in from 0

  // ── Speed-bucketed draw batching ────────────────────────────────────────────
  // Particles are grouped into BUCKETS color buckets by normalized speed each
  // frame. Each bucket accumulates coordinates into preallocated typed arrays,
  // then render() builds one canvas path per bucket and paints it with a
  // single fill()/stroke() call — so a frame costs at most a handful of
  // fillStyle/strokeStyle changes and draw calls, not PARTICLE_COUNT of them.

  var BUCKETS = 24;
  var DOT_SIZE = 2;
  var HALO_SIZE = 4;
  var DOT_ALPHA = 0.85;
  var HALO_ALPHA = 0.16;
  var STREAK_ALPHA = 0.5;
  var HIGH_SPEED_THRESHOLD = 0.55; // normalized 0..1; above this a particle draws as a short streak instead of a dot

  var bucketX, bucketY, bucketCount;
  var streakX0, streakY0, streakX1, streakY1, streakCount;
  var PALETTE_CORE, PALETTE_HALO, PALETTE_STREAK;

  // ── Pointer nudge (presentation-layer only) ─────────────────────────────────
  // Dragging inside the canvas injects a short-lived, purely visual velocity
  // impulse into nearby particles' on-screen displacement. It does NOT alter
  // the sampled field data (velX/velY stay field-truthful — see updateParticles),
  // is not sent to the server, and does not perturb the server's simulation in
  // any way. It is disabled entirely when prefers-reduced-motion is set.

  var IMPULSE_RADIUS = 140;      // px
  var IMPULSE_LIFETIME_MS = 500;
  var pointerDragging = false;
  var pointerPrevX = 0, pointerPrevY = 0, pointerPrevT = 0;
  var impulseVX = 0, impulseVY = 0, impulseCX = 0, impulseCY = 0, impulseAt = 0;

  function initBuckets() {
    bucketX = new Array(BUCKETS);
    bucketY = new Array(BUCKETS);
    streakX0 = new Array(BUCKETS);
    streakY0 = new Array(BUCKETS);
    streakX1 = new Array(BUCKETS);
    streakY1 = new Array(BUCKETS);
    bucketCount = new Int32Array(BUCKETS);
    streakCount = new Int32Array(BUCKETS);
    PALETTE_CORE = new Array(BUCKETS);
    PALETTE_HALO = new Array(BUCKETS);
    PALETTE_STREAK = new Array(BUCKETS);
    for (var b = 0; b < BUCKETS; b++) {
      bucketX[b] = new Float32Array(PARTICLE_COUNT);
      bucketY[b] = new Float32Array(PARTICLE_COUNT);
      streakX0[b] = new Float32Array(PARTICLE_COUNT);
      streakY0[b] = new Float32Array(PARTICLE_COUNT);
      streakX1[b] = new Float32Array(PARTICLE_COUNT);
      streakY1[b] = new Float32Array(PARTICLE_COUNT);

      // Color by speed: cyan (slow) → violet (fast) — same formula as before,
      // just precomputed once per bucket instead of per particle.
      var frac = b / (BUCKETS - 1);
      var r = Math.round(122 + frac * 50); // 122 → 172
      var g = Math.round(200 - frac * 80); // 200 → 120
      var bl = 255;
      PALETTE_CORE[b] = "rgba(" + r + "," + g + "," + bl + "," + DOT_ALPHA + ")";
      PALETTE_HALO[b] = "rgba(" + r + "," + g + "," + bl + "," + HALO_ALPHA + ")";
      PALETTE_STREAK[b] = "rgba(" + r + "," + g + "," + bl + "," + STREAK_ALPHA + ")";
    }
  }

  function initParticles() {
    posX = new Float32Array(PARTICLE_COUNT);
    posY = new Float32Array(PARTICLE_COUNT);
    prevX = new Float32Array(PARTICLE_COUNT);
    prevY = new Float32Array(PARTICLE_COUNT);
    velX = new Float32Array(PARTICLE_COUNT);
    velY = new Float32Array(PARTICLE_COUNT);
    velPrimed = false;
    for (var i = 0; i < PARTICLE_COUNT; i++) {
      var x = Math.random() * canvas.width;
      var y = Math.random() * canvas.height;
      posX[i] = x; posY[i] = y;
      prevX[i] = x; prevY[i] = y;
    }
    if (hudEls.particles) hudEls.particles.textContent = String(PARTICLE_COUNT);
  }

  // ── Mount ────────────────────────────────────────────────────────────────────

  function mount() {
    canvas = document.getElementById("fluid-canvas");
    if (!canvas) return;
    ctx = canvas.getContext("2d");

    hudEls.tick      = document.getElementById("fluid-tick");
    hudEls.wire      = document.getElementById("fluid-wire");
    hudEls.rate      = document.getElementById("fluid-rate");
    hudEls.particles = document.getElementById("fluid-particles");
    hudEls.state = document.getElementById("fluid-state");
    hudEls.compression = document.getElementById("fluid-compression");
    hudEls.frameKind = document.getElementById("fluid-frame-kind");

    REDUCED_MOTION = !!(window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches);

    var resolvedBg = getComputedStyle(canvas).getPropertyValue("--demo-bg");
    if (resolvedBg && resolvedBg.trim()) canvasBg = resolvedBg.trim();

    initBuckets();
    initParticles();
    document.addEventListener("gosx:hub:event", onHubEvent);
    mounted = true;
    document.addEventListener("visibilitychange", onVisibility);
    document.addEventListener("gosx:navigate", teardown, { once: true });
    connectionTimer = setInterval(refreshConnectionState, 1000);
    if (typeof IntersectionObserver === "function") {
      observer = new IntersectionObserver(function (entries) {
        intersecting = !!(entries[0] && entries[0].isIntersecting);
        visible = intersecting && !document.hidden;
        if (visible && !rafID) rafID = requestAnimationFrame(draw);
      });
      observer.observe(canvas);
    }
    if (!REDUCED_MOTION) {
      canvas.addEventListener("pointerdown", onPointerDown);
      document.addEventListener("pointermove", onPointerMove);
      document.addEventListener("pointerup", endPointerDrag);
      document.addEventListener("pointercancel", endPointerDrag);
    }
    rafID = requestAnimationFrame(draw);
  }

  // ── Hub event ────────────────────────────────────────────────────────────────

  function onHubEvent(evt) {
    var detail = evt.detail;
    if (!detail || detail.hubName !== HUB_NAME) return;
    if (detail.event !== EVENT_NAME) return;

    var q = detail.data;
    if (!q || !q.Packed) return;

    // A client that joins mid-stream may see a delta frame before it has ever
    // seen an absolute keyframe. There is no base to apply the delta against,
    // so ignore delta frames until the first keyframe arrives (the server
    // broadcasts one every 40 ticks / ~2s — see sim.go tickLoop).
    if (q.IsDelta && !previousField) {
      if (hudEls.state) hudEls.state.textContent = "waiting for keyframe";
      if (hudEls.frameKind) hudEls.frameKind.textContent = "delta skipped";
      return;
    }

    // Estimate wire size from base64 length (base64 ≈ 4/3 of binary).
    var wireBytes = Math.ceil(q.Packed.length * 0.75);
    wireBytesEma = wireBytesEma * 0.9 + wireBytes * 0.1;
    var now = performance.now();
    if (lastStreamAt) streamHz = streamHz * 0.8 + (1000 / (now - lastStreamAt)) * 0.2;
    lastStreamAt = now;

    var decoded = decodeQuantized(q);

    fieldResolution = q.Resolution;
    fieldComponents = q.Components;
    fieldBounds = q.Bounds;

    if (q.IsDelta) {
      if (previousField && previousField.length === decoded.length) {
        for (var i = 0; i < decoded.length; i++) {
          decoded[i] += previousField[i];
        }
      }
    }

    previousField = decoded.slice();
    currentField = decoded;
    if (hudEls.state) hudEls.state.textContent = "ready";
    if (hudEls.frameKind) hudEls.frameKind.textContent = q.IsDelta ? "delta" : "keyframe";
    var rawBytes = q.Resolution[0] * q.Resolution[1] * q.Resolution[2] * q.Components * 4;
    if (hudEls.compression) hudEls.compression.textContent = (rawBytes / wireBytes).toFixed(1) + "×";

    tick++;
    if (hudEls.tick) hudEls.tick.textContent = String(tick);
    if (hudEls.wire) {
      hudEls.wire.textContent = wireBytes + " B / " + Math.round(wireBytesEma) + " avg";
    }
  }

  // ── Decoder ──────────────────────────────────────────────────────────────────

  // Decode a JSON-serialized Quantized struct into a Float32Array of reconstructed
  // values in the same interleaved order as Field.Data:
  //   [v0.x, v0.y, v0.z, v1.x, v1.y, v1.z, ...]
  //
  // Indices in Packed are DEINTERLEAVED by component (all C0, then all C1, ...).
  function decodeQuantized(q) {
    var totalVoxels = q.Resolution[0] * q.Resolution[1] * q.Resolution[2];
    var packed = base64ToBytes(q.Packed);
    var indices = unpackBits(packed, totalVoxels * q.Components, q.BitWidth);
    var levels = (1 << q.BitWidth) - 1;
    var out = new Float32Array(totalVoxels * q.Components);

    for (var c = 0; c < q.Components; c++) {
      var range = q.Maxs[c] - q.Mins[c];
      var step = range / levels;
      var base = c * totalVoxels;
      for (var v = 0; v < totalVoxels; v++) {
        out[v * q.Components + c] = q.Mins[c] + indices[base + v] * step;
      }
    }
    return out;
  }

  function unpackBits(packed, count, bitWidth) {
    var out = new Int32Array(count);
    var bitPos = 0;
    for (var i = 0; i < count; i++) {
      var val = 0;
      for (var b = 0; b < bitWidth; b++) {
        var bytePos = bitPos >> 3;
        var bitInByte = bitPos & 7;
        if (packed[bytePos] & (1 << bitInByte)) val |= 1 << b;
        bitPos++;
      }
      out[i] = val;
    }
    return out;
  }

  function base64ToBytes(b64) {
    var bin = atob(b64);
    var out = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  }

  // ── Sampling ─────────────────────────────────────────────────────────────────

  // Trilinearly sample the field's XY velocity at canvas-pixel (px, py), fixed
  // to the middle Z slice. The Z midpoint is often fractional (e.g. resZ=24 ->
  // 11.5), so this already blends the two nearest Z slices in addition to
  // bilinear blending across X/Y — i.e. full trilinear interpolation — so
  // particle motion reads as continuous flow instead of stepping voxel-to-voxel.
  function sampleXYMiddleSlice(px, py) {
    if (!currentField || !fieldBounds) { sampleVX = 0; sampleVY = 0; return; }

    var resX = fieldResolution[0];
    var resY = fieldResolution[1];
    var resZ = fieldResolution[2];
    var c = fieldComponents;

    // Map canvas pixels to continuous voxel-space coordinates.
    var ix = (px / canvas.width) * (resX - 1);
    var iy = (py / canvas.height) * (resY - 1);
    var iz = (resZ - 1) * 0.5; // fixed midpoint Z slice

    var i0 = ix | 0; if (i0 < 0) i0 = 0; else if (i0 > resX - 1) i0 = resX - 1;
    var j0 = iy | 0; if (j0 < 0) j0 = 0; else if (j0 > resY - 1) j0 = resY - 1;
    var k0 = iz | 0; if (k0 < 0) k0 = 0; else if (k0 > resZ - 1) k0 = resZ - 1;
    var i1 = i0 + 1 < resX ? i0 + 1 : i0;
    var j1 = j0 + 1 < resY ? j0 + 1 : j0;
    var k1 = k0 + 1 < resZ ? k0 + 1 : k0;

    var tx = ix - i0; if (tx < 0) tx = 0; else if (tx > 1) tx = 1;
    var ty = iy - j0; if (ty < 0) ty = 0; else if (ty > 1) ty = 1;
    var tz = iz - k0; if (tz < 0) tz = 0; else if (tz > 1) tz = 1;

    var rowX = resX;
    var planeXY = resX * resY;

    var b000 = (i0 + j0 * rowX + k0 * planeXY) * c;
    var b100 = (i1 + j0 * rowX + k0 * planeXY) * c;
    var b010 = (i0 + j1 * rowX + k0 * planeXY) * c;
    var b110 = (i1 + j1 * rowX + k0 * planeXY) * c;
    var b001 = (i0 + j0 * rowX + k1 * planeXY) * c;
    var b101 = (i1 + j0 * rowX + k1 * planeXY) * c;
    var b011 = (i0 + j1 * rowX + k1 * planeXY) * c;
    var b111 = (i1 + j1 * rowX + k1 * planeXY) * c;

    var f = currentField;
    var ixt = 1 - tx, iyt = 1 - ty, izt = 1 - tz;

    sampleVX =
      (f[b000] * ixt + f[b100] * tx) * iyt * izt +
      (f[b010] * ixt + f[b110] * tx) * ty * izt +
      (f[b001] * ixt + f[b101] * tx) * iyt * tz +
      (f[b011] * ixt + f[b111] * tx) * ty * tz;
    sampleVY =
      (f[b000 + 1] * ixt + f[b100 + 1] * tx) * iyt * izt +
      (f[b010 + 1] * ixt + f[b110 + 1] * tx) * ty * izt +
      (f[b001 + 1] * ixt + f[b101 + 1] * tx) * iyt * tz +
      (f[b011 + 1] * ixt + f[b111 + 1] * tx) * ty * tz;
  }

  // ── Simulation step ──────────────────────────────────────────────────────────

  function updateParticles(dt, now) {
    for (var b = 0; b < BUCKETS; b++) { bucketCount[b] = 0; streakCount[b] = 0; }

    var w = canvas.width, h = canvas.height;
    var smoothing = velPrimed ? SMOOTHING : 0;

    var impulseAge = now - impulseAt;
    var impulseLive = impulseAt > 0 && impulseAge < IMPULSE_LIFETIME_MS;
    var impulseFalloffT = impulseLive ? 1 - impulseAge / IMPULSE_LIFETIME_MS : 0;

    for (var p = 0; p < PARTICLE_COUNT; p++) {
      var x = posX[p], y = posY[p];
      sampleXYMiddleSlice(x, y);

      // velX/velY always reflect the actual decoded field (EMA-smoothed only
      // to soften 20 Hz server ticks vs. 60 Hz rendering) — the pointer
      // impulse below never touches these, so color-by-speed and the HUD
      // stay truthful to the wire data.
      var vx = velX[p] = velX[p] * smoothing + sampleVX * (1 - smoothing);
      var vy = velY[p] = velY[p] * smoothing + sampleVY * (1 - smoothing);

      prevX[p] = x; prevY[p] = y;

      var dx = vx * SPEED_SCALE * dt;
      var dy = vy * SPEED_SCALE * dt;

      if (impulseLive) {
        var rx = x - impulseCX, ry = y - impulseCY;
        var dist = Math.sqrt(rx * rx + ry * ry);
        if (dist < IMPULSE_RADIUS) {
          var spatial = 1 - dist / IMPULSE_RADIUS;
          var k = impulseFalloffT * spatial * spatial;
          dx += impulseVX * dt * k;
          dy += impulseVY * dt * k;
        }
      }

      x += dx;
      y += dy;

      // Wrap around canvas edges.
      var wrapped = false;
      if (x < 0) { x += w; wrapped = true; }
      else if (x >= w) { x -= w; wrapped = true; }
      if (y < 0) { y += h; wrapped = true; }
      else if (y >= h) { y -= h; wrapped = true; }

      posX[p] = x; posY[p] = y;

      // Bucket by the field-truthful speed (not the rendered/impulse-nudged
      // displacement), so color always reports what the server actually sent.
      var speed = Math.min(1, Math.sqrt(vx * vx + vy * vy) / 2);
      var bucket = (speed * (BUCKETS - 1)) | 0;

      if (!REDUCED_MOTION && !wrapped && speed > HIGH_SPEED_THRESHOLD) {
        var si = streakCount[bucket]++;
        streakX0[bucket][si] = prevX[p]; streakY0[bucket][si] = prevY[p];
        streakX1[bucket][si] = x;        streakY1[bucket][si] = y;
      } else {
        var di = bucketCount[bucket]++;
        // Store the dot's top-left corner; render() derives the halo square
        // from the same point so no extra storage is needed for the glow pass.
        bucketX[bucket][di] = x - DOT_SIZE * 0.5;
        bucketY[bucket][di] = y - DOT_SIZE * 0.5;
      }
    }

    velPrimed = true;
  }

  function renderParticles() {
    // Additive blending: overlapping particles brighten instead of occluding,
    // which reads as glow without the per-particle cost of ctx.shadowBlur.
    ctx.globalCompositeOperation = REDUCED_MOTION ? "source-over" : "lighter";

    if (!REDUCED_MOTION) {
      // Halo pass: soft, larger, low-alpha squares underneath the core dot.
      var haloInset = (HALO_SIZE - DOT_SIZE) * 0.5;
      for (var b = 0; b < BUCKETS; b++) {
        var n = bucketCount[b];
        if (!n) continue;
        var hx = bucketX[b], hy = bucketY[b];
        ctx.beginPath();
        for (var i = 0; i < n; i++) ctx.rect(hx[i] - haloInset, hy[i] - haloInset, HALO_SIZE, HALO_SIZE);
        ctx.fillStyle = PALETTE_HALO[b];
        ctx.fill();
      }
    }

    // Core pass: crisp dots, one path + one fill() per non-empty bucket.
    for (var b2 = 0; b2 < BUCKETS; b2++) {
      var n2 = bucketCount[b2];
      if (!n2) continue;
      var cx = bucketX[b2], cy = bucketY[b2];
      ctx.beginPath();
      for (var j = 0; j < n2; j++) ctx.rect(cx[j], cy[j], DOT_SIZE, DOT_SIZE);
      ctx.fillStyle = PALETTE_CORE[b2];
      ctx.fill();
    }

    // Streak pass: short velocity-aligned segments for fast-moving particles,
    // one path + one stroke() per non-empty bucket.
    if (!REDUCED_MOTION) {
      ctx.lineWidth = 1.5;
      for (var b3 = 0; b3 < BUCKETS; b3++) {
        var n3 = streakCount[b3];
        if (!n3) continue;
        var x0 = streakX0[b3], y0 = streakY0[b3], x1 = streakX1[b3], y1 = streakY1[b3];
        ctx.beginPath();
        for (var k = 0; k < n3; k++) {
          ctx.moveTo(x0[k], y0[k]);
          ctx.lineTo(x1[k], y1[k]);
        }
        ctx.strokeStyle = PALETTE_STREAK[b3];
        ctx.stroke();
      }
    }

    ctx.globalCompositeOperation = "source-over";
  }

  // ── Render loop ───────────────────────────────────────────────────────────────

  function draw(now) {
    rafID = 0;
    if (!mounted || !visible || document.hidden) return;
    var dt = lastFrameTime ? (now - lastFrameTime) / 1000 : 0;
    lastFrameTime = now;
    fps = dt > 0 ? fps * 0.9 + (1 / dt) * 0.1 : fps;
    if (hudEls.rate) hudEls.rate.textContent = Math.round(fps) + " fps / " + Math.round(streamHz) + " Hz";

    // Fade the previous frame to produce motion trails, using the shell's
    // --demo-bg token (resolved once at mount) instead of a hardcoded hex so
    // the trail always matches the actual themed canvas background.
    ctx.globalCompositeOperation = "source-over";
    ctx.globalAlpha = REDUCED_MOTION ? 0.4 : 0.14; // shorter trails under reduced motion
    ctx.fillStyle = canvasBg;
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.globalAlpha = 1;

    if (currentField) {
      updateParticles(dt, now);
      renderParticles();
    }

    rafID = requestAnimationFrame(draw);
  }

  var observer = null;
  function onVisibility() {
    visible = !document.hidden && intersecting;
    if (visible && !rafID) rafID = requestAnimationFrame(draw);
  }
  function refreshConnectionState() {
    var hubs = window.__gosx && window.__gosx.hubs;
    var open = false, connecting = false;
    if (hubs) hubs.forEach(function (record) {
      if (!record || !record.entry || record.entry.name !== HUB_NAME || !record.socket) return;
      open = open || record.socket.readyState === 1;
      connecting = connecting || record.socket.readyState === 0;
    });
    if (!open && hudEls.state) hudEls.state.textContent = connecting ? "reconnecting…" : "offline";
    if (open && currentField && hudEls.state) hudEls.state.textContent = "ready";
  }

  // ── Pointer nudge handlers (presentation-layer only, see note above) ───────

  function onPointerDown(e) {
    if (!canvas) return;
    var r = canvas.getBoundingClientRect();
    pointerDragging = true;
    pointerPrevX = (e.clientX - r.left) * (canvas.width / r.width);
    pointerPrevY = (e.clientY - r.top) * (canvas.height / r.height);
    pointerPrevT = performance.now();
  }
  function onPointerMove(e) {
    if (!pointerDragging || !canvas) return;
    var r = canvas.getBoundingClientRect();
    var x = (e.clientX - r.left) * (canvas.width / r.width);
    var y = (e.clientY - r.top) * (canvas.height / r.height);
    var now = performance.now();
    var dt = (now - pointerPrevT) / 1000;
    if (dt > 0.001) {
      impulseVX = (x - pointerPrevX) / dt;
      impulseVY = (y - pointerPrevY) / dt;
      impulseCX = x;
      impulseCY = y;
      impulseAt = now;
    }
    pointerPrevX = x; pointerPrevY = y; pointerPrevT = now;
  }
  function endPointerDrag() {
    pointerDragging = false;
  }

  function teardown() {
    mounted = false;
    if (rafID) cancelAnimationFrame(rafID);
    rafID = 0;
    document.removeEventListener("gosx:hub:event", onHubEvent);
    document.removeEventListener("visibilitychange", onVisibility);
    document.removeEventListener("pointermove", onPointerMove);
    document.removeEventListener("pointerup", endPointerDrag);
    document.removeEventListener("pointercancel", endPointerDrag);
    clearInterval(connectionTimer);
    if (observer) observer.disconnect();
  }

  // ── Bootstrap ─────────────────────────────────────────────────────────────────

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", mount);
  } else {
    mount();
  }
})();
