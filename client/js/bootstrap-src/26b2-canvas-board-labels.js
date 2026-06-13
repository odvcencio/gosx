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
// isolation by unit tests. It installs window.__gosx_canvas_board_labels_sync,
// window.__gosx_canvas_board_labels_dispose,
// window.__gosx_canvas_board_html_sync, and
// window.__gosx_canvas_board_html_dispose on evaluation.
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

  // ensureBoardOverlayHost lazily makes the direct canvas parent a positioning
  // context so overlay layers with inset:0 cover the canvas.
  //
  // Host position: the canvas2d mount path (_ensureSurfaceCanvas in
  // 26b-feature-engines-prefix.js) replaces a placeholder with a <canvas> in
  // the same parent — it does NOT guarantee position:relative on the parent.
  // The marquee overlay uses canvas.offsetLeft/offsetTop relative to the
  // positioned ancestor, which could be any ancestor — not the direct parent.
  // For board overlay layers we need them to cover the canvas precisely, so
  // we must ensure the direct parent is the positioned container. We guard the
  // set with the same pattern as 10-runtime-scene-core.js line 735:
  //   if (!mount.style.position || mount.style.position === "static") { ... }
  function ensureBoardOverlayHost(host) {
    try {
      if (!host.style.position || host.style.position === "static") {
        host.style.position = "relative";
      }
    } catch (e) { /* tolerate read-only style (SSR stubs) */ }
  }

  // ensureLabelLayer lazily creates (and caches on host.__gosxBoardLabelLayer)
  // a single overlay div covering the host. The overlay is positioned absolutely
  // with inset:0 so it covers the canvas exactly when the host is positioned.
  // pointer-events:none ensures the overlay never intercepts board events.
  function ensureLabelLayer(host) {
    if (host.__gosxBoardLabelLayer) {
      return host.__gosxBoardLabelLayer;
    }
    ensureBoardOverlayHost(host);
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

  // ensureHTMLLayer creates the keyed HTML overlay layer for CanvasBoard html
  // nodes. The layer itself is click-through; each entry controls its own
  // pointer-events so editable DOM can receive input without the empty overlay
  // plane swallowing board gestures.
  function ensureHTMLLayer(host) {
    if (host.__gosxBoardHTMLLayer) {
      return host.__gosxBoardHTMLLayer;
    }
    ensureBoardOverlayHost(host);
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
    layer.__gosxCanvasBoardHTMLByID = new Map();
    try {
      host.appendChild(layer);
    } catch (e) {
      return null;
    }
    host.__gosxBoardHTMLLayer = layer;
    return layer;
  }

  function boardNumber(value, fallback) {
    var n = Number(value);
    return Number.isFinite(n) ? n : fallback;
  }

  function boardHTMLID(entry, index) {
    var id = entry && entry.id != null ? String(entry.id) : "";
    return id !== "" ? id : "html:" + index;
  }

  function boardHTMLMarkup(entry) {
    if (!entry || typeof entry !== "object") return "";
    if (typeof entry.markup === "string") return entry.markup;
    if (typeof entry.html === "string") return entry.html;
    return "";
  }

  function boardHTMLPointerEvents(entry) {
    var value = entry && typeof entry.pointerEvents === "string" ? entry.pointerEvents : "";
    return value !== "" ? value : "auto";
  }

  function boardHTMLWorldPoint(entry) {
    var pos = entry && entry.position && typeof entry.position === "object" ? entry.position : {};
    return {
      x: boardNumber(entry && Object.prototype.hasOwnProperty.call(entry, "x") ? entry.x : pos.x, 0),
      y: boardNumber(entry && Object.prototype.hasOwnProperty.call(entry, "y") ? entry.y : pos.y, 0),
    };
  }

  function boardContainsActiveElement(element) {
    if (!element || typeof document === "undefined") return false;
    var active = document.activeElement || null;
    return !!(active && active !== document.body && typeof element.contains === "function" && element.contains(active));
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

  // __gosx_canvas_board_html_sync reconciles RenderBundle.HTML entries for one
  // frame. Entries are keyed by id, positioned with the same OrthoCamera2D math
  // as labels, and keep their DOM content stable while focused so in-place
  // editing does not lose user input/caret state.
  function boardHTMLSync(host, htmlEntries, camera, cssWidth, cssHeight) {
    if (!host) return;
    var layer = ensureHTMLLayer(host);
    if (!layer) return;

    var cam = camera || {};
    var zoom = typeof cam.z === "number" && cam.z > 0 ? cam.z : 1;
    var panX = typeof cam.x === "number" ? cam.x : 0;
    var panY = typeof cam.y === "number" ? cam.y : 0;
    var w = cssWidth > 0 ? cssWidth : 1;
    var h = cssHeight > 0 ? cssHeight : 1;
    var halfW = w / 2;
    var halfH = h / 2;
    var list = Array.isArray(htmlEntries) ? htmlEntries : [];
    var byID = layer.__gosxCanvasBoardHTMLByID;
    if (!byID || typeof byID.get !== "function") {
      byID = new Map();
      layer.__gosxCanvasBoardHTMLByID = byID;
    }
    var seen = new Set();

    for (var i = 0; i < list.length; i++) {
      var entry = list[i] || {};
      var id = boardHTMLID(entry, i);
      seen.add(id);
      var el = byID.get(id);
      if (!el) {
        try {
          var ownerDoc = layer.ownerDocument || (typeof document !== "undefined" ? document : null);
          if (!ownerDoc) continue;
          el = ownerDoc.createElement("div");
          el.style.cssText = "position:absolute;left:0;top:0;box-sizing:border-box;will-change:transform;";
          el.setAttribute("data-gosx-canvas-html", id);
          byID.set(id, el);
          layer.appendChild(el);
        } catch (e) {
          continue;
        }
      } else if (el.parentNode !== layer) {
        try { layer.appendChild(el); } catch (e) { /* tolerate */ }
      }

      var point = boardHTMLWorldPoint(entry);
      var width = Math.max(0, boardNumber(entry.width, 0) * zoom);
      var height = Math.max(0, boardNumber(entry.height, 0) * zoom);
      var screenX = (point.x - panX) * zoom + halfW;
      var screenY = halfH - (point.y - panY) * zoom;
      // CanvasBoard HTML entries use x/y as bottom-left world bounds, but DOM
      // transforms position top-left CSS boxes.
      var topY = screenY - height;
      var pointerEvents = boardHTMLPointerEvents(entry);
      var markup = boardHTMLMarkup(entry);

      var tx = Math.round(screenX * 100) / 100;
      var ty = Math.round(topY * 100) / 100;
      var transform = "translate(" + tx + "px," + ty + "px)";
      if (el._gosxTransform !== transform) {
        el.style.transform = transform;
        el._gosxTransform = transform;
      }
      var widthCSS = width + "px";
      if (el._gosxWidth !== widthCSS) {
        el.style.width = widthCSS;
        el._gosxWidth = widthCSS;
      }
      var heightCSS = height + "px";
      if (el._gosxHeight !== heightCSS) {
        el.style.height = heightCSS;
        el._gosxHeight = heightCSS;
      }
      if (el._gosxPointerEvents !== pointerEvents) {
        el.style.pointerEvents = pointerEvents;
        el.setAttribute("data-gosx-canvas-html-pointer-events", pointerEvents);
        el._gosxPointerEvents = pointerEvents;
      }
      if (el._gosxMarkup !== markup && !boardContainsActiveElement(el)) {
        el.innerHTML = markup;
        el._gosxMarkup = markup;
      }
    }

    if (byID && typeof byID.forEach === "function") {
      var remove = [];
      byID.forEach(function(el, id) {
        if (!seen.has(id)) {
          remove.push(id);
        }
      });
      for (var r = 0; r < remove.length; r++) {
        var removeID = remove[r];
        var removeEl = byID.get(removeID);
        byID.delete(removeID);
        try {
          if (removeEl && removeEl.parentNode) {
            removeEl.parentNode.removeChild(removeEl);
          }
        } catch (e) { /* tolerate */ }
      }
    }
  }

  // __gosx_canvas_board_html_dispose removes the HTML overlay layer and clears
  // the keyed entry cache on the host. Called by the WebGPU board teardown.
  function boardHTMLDispose(host) {
    if (!host || !host.__gosxBoardHTMLLayer) return;
    var layer = host.__gosxBoardHTMLLayer;
    try {
      if (layer.parentNode) {
        layer.parentNode.removeChild(layer);
      }
    } catch (e) { /* tolerate */ }
    if (layer.__gosxCanvasBoardHTMLByID && typeof layer.__gosxCanvasBoardHTMLByID.clear === "function") {
      layer.__gosxCanvasBoardHTMLByID.clear();
    }
    delete host.__gosxBoardHTMLLayer;
  }

  window.__gosx_canvas_board_labels_sync = boardLabelSync;
  window.__gosx_canvas_board_labels_dispose = boardLabelDispose;
  window.__gosx_canvas_board_html_sync = boardHTMLSync;
  window.__gosx_canvas_board_html_dispose = boardHTMLDispose;
})();
