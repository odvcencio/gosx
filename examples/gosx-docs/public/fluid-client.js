// examples/gosx-docs/public/fluid-client.js
// Particle advection renderer for the Fluid demo.
// Subscribes to the "fluid" hub, decodes Quantized frames from field.PublishField,
// and drives 1500 CPU particles against the decoded velocity slice each rAF tick.
(function () {
  "use strict";

  var HUB_NAME = "fluid";
  var EVENT_NAME = "field:velocity"; // fieldEventPrefix + topic from field/stream.go

  // ── State ────────────────────────────────────────────────────────────────────

  var canvas, ctx;
  var hudEls = {};

  var previousField = null; // Float32Array — delta base
  var currentField = null;  // Float32Array — in-use field
  var fieldResolution = [0, 0, 0];
  var fieldComponents = 0;
  var fieldBounds = null;

  var tick = 0;
  var lastFrameTime = 0;
  var fps = 0;
  var wireBytesEma = 0;

  // ── Particles ────────────────────────────────────────────────────────────────

  var PARTICLE_COUNT = 1500;

  // Each particle: { x, y }
  var particles = [];

  function initParticles() {
    particles = new Array(PARTICLE_COUNT);
    for (var i = 0; i < PARTICLE_COUNT; i++) {
      particles[i] = {
        x: Math.random() * canvas.width,
        y: Math.random() * canvas.height,
      };
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

    initParticles();
    document.addEventListener("gosx:hub:event", onHubEvent);
    requestAnimationFrame(draw);
  }

  // ── Hub event ────────────────────────────────────────────────────────────────

  function onHubEvent(evt) {
    var detail = evt.detail;
    if (!detail || detail.hubName !== HUB_NAME) return;
    if (detail.event !== EVENT_NAME) return;

    var q = detail.data;
    if (!q || !q.Packed) return;

    // Estimate wire size from base64 length (base64 ≈ 4/3 of binary).
    var wireBytes = Math.ceil(q.Packed.length * 0.75);
    wireBytesEma = wireBytesEma * 0.9 + wireBytes * 0.1;

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

  // Sample the field's XY velocity at canvas-pixel (px, py) using the middle Z
  // slice. Uses nearest-voxel sampling — fast enough for 1500 particles/frame.
  function sampleXYMiddleSlice(px, py) {
    if (!currentField || !fieldBounds) return [0, 0];

    var resX = fieldResolution[0];
    var resY = fieldResolution[1];
    var resZ = fieldResolution[2];
    var minX = fieldBounds.Min[0];
    var minY = fieldBounds.Min[1];
    var minZ = fieldBounds.Min[2];
    var maxX = fieldBounds.Max[0];
    var maxY = fieldBounds.Max[1];
    var maxZ = fieldBounds.Max[2];

    // Map canvas pixels to voxel index (continuous).
    var ix = (px / canvas.width) * (resX - 1);
    var iy = (py / canvas.height) * (resY - 1);
    var iz = (resZ - 1) * 0.5; // midpoint Z slice

    // Nearest-voxel clamp.
    var i = Math.min(resX - 1, Math.max(0, Math.round(ix))) | 0;
    var j = Math.min(resY - 1, Math.max(0, Math.round(iy))) | 0;
    var k = Math.min(resZ - 1, Math.max(0, Math.round(iz))) | 0;

    var voxelIdx = i + j * resX + k * resX * resY;
    var c = fieldComponents;
    var vx = currentField[voxelIdx * c + 0];
    var vy = currentField[voxelIdx * c + 1];
    return [vx, vy];
  }

  // ── Render loop ───────────────────────────────────────────────────────────────

  function draw(now) {
    var dt = lastFrameTime ? (now - lastFrameTime) / 1000 : 0;
    lastFrameTime = now;
    fps = dt > 0 ? fps * 0.9 + (1 / dt) * 0.1 : fps;
    if (hudEls.rate) hudEls.rate.textContent = Math.round(fps) + " fps";

    // Fade the previous frame to produce motion trails.
    ctx.fillStyle = "rgba(11, 11, 13, 0.14)";
    ctx.fillRect(0, 0, canvas.width, canvas.height);

    if (currentField) {
      var speedScale = 60; // pixels per (world unit / second)

      for (var p = 0; p < PARTICLE_COUNT; p++) {
        var part = particles[p];
        var vel = sampleXYMiddleSlice(part.x, part.y);
        var vx = vel[0];
        var vy = vel[1];

        part.x += vx * speedScale * dt;
        part.y += vy * speedScale * dt;

        // Wrap around canvas edges.
        if (part.x < 0) part.x += canvas.width;
        if (part.x >= canvas.width) part.x -= canvas.width;
        if (part.y < 0) part.y += canvas.height;
        if (part.y >= canvas.height) part.y -= canvas.height;

        // Color by speed: cyan (slow) → violet (fast).
        var speed = Math.min(1, Math.sqrt(vx * vx + vy * vy) / 2);
        var r = Math.round(122 + speed * 50);  // 122 → 172
        var g = Math.round(200 - speed * 80);  // 200 → 120
        var b = 255;
        ctx.fillStyle = "rgba(" + r + "," + g + "," + b + ",0.9)";
        ctx.fillRect(Math.round(part.x), Math.round(part.y), 1, 1);
      }
    }

    requestAnimationFrame(draw);
  }

  // ── Bootstrap ─────────────────────────────────────────────────────────────────

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", mount);
  } else {
    mount();
  }
})();
