// DOM label overlay for gosx.CanvasBoard surfaces.
//
// __gosx_canvas_board_labels_sync overlays real HTML <span> elements on top of
// a WebGPU/canvas board so text benefits from subpixel rendering and future
// in-place editing without touching the GPU painter. The GPU renders rects,
// lines, and sprites; text stays in the DOM.
//
// The screen transform mirrors render/bundle.OrthoCamera2D exactly (the same
// formula as 26b1-canvas2d-painter.js's canvasBoardScreenTransform):
//
//     screenX = (worldX - panX) * zoom + cssWidth / 2
//     screenY = (cssHeight / 2) - (worldY - panY) * zoom
//
// Labels are positioned via CSS transform: translate(screenX px, (screenY - ascent) px)
// where ascent vertically aligns the DOM text baseline with the canvas fillText
// baseline at screenY. Ascent is measured once per font string via an offscreen
// canvas; in test environments where canvas is absent the fallback
// 0.8 * fontSize provides a reasonable approximation (see ascentForFont).
//
// Reconciliation is index-keyed (labels carry no IDs); spans are reused in
// order, created when missing, and the tail is removed. This keeps steady-state
// frame cost allocation-free when the label list is unchanged.
//
// This file is standalone (no feature-registry wrapper) so it can be loaded in
// isolation by unit tests. It installs window.__gosx_canvas_board_labels_sync
// and window.__gosx_canvas_board_labels_dispose on evaluation.
(function() {
  "use strict";

  // DEFAULT_FONT and DEFAULT_COLOR match 26b1-canvas2d-painter.js's label path
  // (ctx.font is not explicitly set, so the context default "10px sans-serif"
  // would apply, but the label branch only sets font when label.font is present;
  // color falls back to "#e6edf3" when label.color is absent — see 26b1 lines
  // 143-147). We use system-ui at 14px as the DOM default to give labels crisp
  // rendering at a readable size.
  var DEFAULT_FONT = "14px system-ui, sans-serif";
  var DEFAULT_COLOR = "#e6edf3";

  // CULL_MARGIN: labels whose screen position is outside the viewport by more
  // than this many CSS pixels are hidden cheaply (display:none) rather than
  // removed — they can reappear on the next pan without DOM allocation.
  var CULL_MARGIN = 256;

  // ascentCache maps font strings to their measured actualBoundingBoxAscent.
  // Module-level: shared across all boards and frames; filled lazily.
  var ascentCache = new Map();

  // _probeCanvas is the single offscreen canvas used for font measurement.
  // Lazily created once; null signals "unavailable" (test/SSR environments).
  var _probeCanvas = null;
  var _probeCanvasTried = false;
  var _probeCtx = null;

  // ascentForFont returns the ascent in CSS pixels for `font`. It probes a
  // shared offscreen canvas on the first call for each font string and caches
  // the result. In environments without canvas support (Node test harness) the
  // canvas probe is attempted once, degrades to null, and every subsequent call
  // uses the fallback: 0.8 * parsedFontSizePx. The 0.8 coefficient approximates
  // the cap-height-to-em ratio for system fonts; an exact value is not required
  // because the same approximation applies to both the label and its painted
  // canvas2d counterpart.
  function ascentForFont(font) {
    if (ascentCache.has(font)) {
      return ascentCache.get(font);
    }
    var ascent = null;
    if (!_probeCanvasTried) {
      _probeCanvasTried = true;
      try {
        if (typeof document !== "undefined" && typeof document.createElement === "function") {
          _probeCanvas = document.createElement("canvas");
          _probeCanvas.width = 1;
          _probeCanvas.height = 1;
          _probeCtx = _probeCanvas.getContext("2d") || null;
        }
      } catch (e) {
        _probeCanvas = null;
        _probeCtx = null;
      }
    }
    if (_probeCtx) {
      try {
        _probeCtx.font = font;
        var m = _probeCtx.measureText("Mg");
        if (m && typeof m.actualBoundingBoxAscent === "number" && m.actualBoundingBoxAscent > 0) {
          ascent = m.actualBoundingBoxAscent;
        }
      } catch (e) {
        ascent = null;
      }
    }
    if (ascent === null) {
      // Fallback: parse the font-size token and multiply by 0.8 to approximate
      // actualBoundingBoxAscent. E.g. "14px system-ui" → 14 * 0.8 = 11.2.
      ascent = 0.8 * parseFontSizePx(font);
    }
    ascentCache.set(font, ascent);
    return ascent;
  }

  // parseFontSizePx extracts the leading pixel font-size from a CSS font
  // shorthand string (e.g. "14px system-ui, sans-serif" → 14). Returns 12 when
  // no px token is found (a safe fallback for the 0.8 ascent approximation).
  function parseFontSizePx(font) {
    var match = String(font || "").match(/(\d+(?:\.\d+)?)px/);
    return match ? parseFloat(match[1]) : 12;
  }

  // ensureLabelLayer lazily creates (and caches on host.__gosxBoardLabelLayer)
  // a single overlay div covering the host. The overlay is positioned absolutely
  // with inset:0 so it covers the canvas exactly when the host is positioned.
  // pointer-events:none ensures the overlay never intercepts board events.
  //
  // Host position: the canvas2d mount path (_ensureSurfaceCanvas in
  // 26b-feature-engines-prefix.js) replaces a placeholder with a <canvas> in
  // the same parent — it does NOT guarantee position:relative on the parent.
  // The marquee overlay uses canvas.offsetLeft/offsetTop relative to the
  // positioned ancestor, which could be any ancestor — not the direct parent.
  // For the label layer we need the overlay to cover the canvas precisely, so
  // we must ensure the direct parent is the positioned container. We guard the
  // set with the same pattern as 10-runtime-scene-core.js line 735:
  //   if (!mount.style.position || mount.style.position === "static") { ... }
  function ensureLabelLayer(host) {
    if (host.__gosxBoardLabelLayer) {
      return host.__gosxBoardLabelLayer;
    }
    // Guarantee the host is a positioning context so inset:0 works.
    try {
      if (!host.style.position || host.style.position === "static") {
        host.style.position = "relative";
      }
    } catch (e) { /* tolerate read-only style (SSR stubs) */ }
    var layer = null;
    try {
      if (typeof document !== "undefined" && typeof document.createElement === "function") {
        layer = document.createElement("div");
      } else if (host.ownerDocument && typeof host.ownerDocument.createElement === "function") {
        layer = host.ownerDocument.createElement("div");
      }
    } catch (e) {
      layer = null;
    }
    if (!layer) {
      return null;
    }
    layer.style.cssText = "position:absolute;inset:0;overflow:hidden;pointer-events:none;";
    try {
      host.appendChild(layer);
    } catch (e) {
      return null;
    }
    host.__gosxBoardLabelLayer = layer;
    return layer;
  }

  // __gosx_canvas_board_labels_sync reconciles the label overlay for one frame.
  //
  //   host       — the canvas's parent element (the board mount).
  //   labels     — engine.RenderLabel[]: [{position:{x,y}, text, font, color}].
  //                All fields are optional; missing font/color use the defaults.
  //   camera     — RenderCamera: {mode:"ortho2d", x:panX, y:panY, z:zoom}.
  //   cssWidth   — CSS (logical) viewport width in pixels.
  //   cssHeight  — CSS (logical) viewport height in pixels.
  //
  // Called every rAF frame by the render loop; allocation-light on steady state
  // (no per-frame innerHTML, no per-frame DOM churn when nothing changed).
  function boardLabelSync(host, labels, camera, cssWidth, cssHeight) {
    if (!host) return;
    var layer = ensureLabelLayer(host);
    if (!layer) return;

    var cam = camera || {};
    var zoom = typeof cam.z === "number" && cam.z > 0 ? cam.z : 1;
    var panX = typeof cam.x === "number" ? cam.x : 0;
    var panY = typeof cam.y === "number" ? cam.y : 0;
    var w = cssWidth > 0 ? cssWidth : 1;
    var h = cssHeight > 0 ? cssHeight : 1;
    var halfW = w / 2;
    var halfH = h / 2;

    var list = Array.isArray(labels) ? labels : [];
    var children = layer.childNodes;

    for (var i = 0; i < list.length; i++) {
      var label = list[i] || {};
      var pos = label.position || {};
      var worldX = typeof pos.x === "number" ? pos.x : 0;
      var worldY = typeof pos.y === "number" ? pos.y : 0;
      var font = typeof label.font === "string" && label.font !== "" ? label.font : DEFAULT_FONT;
      var color = typeof label.color === "string" && label.color !== "" ? label.color : DEFAULT_COLOR;
      var text = String(label.text == null ? "" : label.text);

      // OrthoCamera2D screen transform (parity with 26b1-canvas2d-painter.js):
      //   screenX = (worldX - panX) * zoom + halfW
      //   screenY = halfH - (worldY - panY) * zoom
      var screenX = (worldX - panX) * zoom + halfW;
      var screenY = halfH - (worldY - panY) * zoom;
      var ascent = ascentForFont(font);

      // Culling: labels far outside the viewport are hidden without removal
      // so they can re-appear cheaply on pan.
      var culled = (
        screenX < -CULL_MARGIN ||
        screenX > w + CULL_MARGIN ||
        screenY < -CULL_MARGIN ||
        screenY > h + CULL_MARGIN
      );

      var span;
      if (i < children.length) {
        span = children[i];
      } else {
        // Create and append new span; style is set below on first sync.
        try {
          var ownerDoc = layer.ownerDocument || (typeof document !== "undefined" ? document : null);
          if (!ownerDoc) continue;
          span = ownerDoc.createElement("span");
          span.style.cssText = "position:absolute;left:0;top:0;white-space:pre;will-change:transform;";
          layer.appendChild(span);
        } catch (e) {
          continue;
        }
      }

      if (culled) {
        if (span.style.display !== "none") {
          span.style.display = "none";
        }
        continue;
      }

      if (span.style.display === "none") {
        span.style.display = "";
      }

      // Update only changed properties to avoid layout thrash.
      if (span._gosxFont !== font) {
        span.style.font = font;
        span._gosxFont = font;
      }
      if (span._gosxColor !== color) {
        span.style.color = color;
        span._gosxColor = color;
      }
      if (span._gosxText !== text) {
        span.textContent = text;
        span._gosxText = text;
      }

      var tx = Math.round(screenX * 100) / 100;
      var ty = Math.round((screenY - ascent) * 100) / 100;
      var transform = "translate(" + tx + "px," + ty + "px)";
      if (span._gosxTransform !== transform) {
        span.style.transform = transform;
        span._gosxTransform = transform;
      }
    }

    // Remove excess spans (label list shrank).
    var targetLen = list.length;
    while (layer.childNodes.length > targetLen) {
      try {
        layer.removeChild(layer.childNodes[layer.childNodes.length - 1]);
      } catch (e) {
        break;
      }
    }
  }

  // __gosx_canvas_board_labels_dispose removes the label layer and clears the
  // cached reference on the host. Called by the render loop on unmount.
  function boardLabelDispose(host) {
    if (!host || !host.__gosxBoardLabelLayer) return;
    var layer = host.__gosxBoardLabelLayer;
    try {
      if (layer.parentNode) {
        layer.parentNode.removeChild(layer);
      }
    } catch (e) { /* tolerate */ }
    delete host.__gosxBoardLabelLayer;
  }

  window.__gosx_canvas_board_labels_sync = boardLabelSync;
  window.__gosx_canvas_board_labels_dispose = boardLabelDispose;
})();
