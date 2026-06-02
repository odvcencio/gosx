// Canvas2D painter for gosx.CanvasBoard surfaces.
//
// paintCanvasBundle replays a RenderBundle (the JSON __gosx_render_canvas
// returns) onto a 2D context. It is the JS half of the canvas2d paint loop
// mounted in 26b-feature-engines-prefix.js; the Go half (CanvasBoardAdapter +
// the __gosx_render_canvas WASM global) produces the bundle.
//
// The screen transform replicates render/bundle.OrthoCamera2D exactly so the
// canvas2d board paints identically to the GPU path. OrthoCamera2D packs
// pan into camera.x/.y and zoom into camera.z, and maps the world point
// (panX, panY) to the viewport center, with 1 world unit = 1 CSS pixel at
// zoom 1. NDC has +Y up; a DOM canvas has +Y down, so the Y axis is flipped:
//
//     screenX = (worldX - panX) * zoom + cssWidth / 2
//     screenY = (cssHeight / 2) - (worldY - panY) * zoom
//
// This file is standalone (no feature-registry wrapper) so it can be folded
// into the engines bundle AND loaded in isolation by the painter unit tests.
// It installs window.__gosx_paint_canvas_bundle on evaluation.
(function() {
  "use strict";

  // canvasBoardScreenTransform builds the world→screen mapping from a bundle
  // camera. Defaults keep a missing/garbled camera from blanking the board:
  // zoom falls back to 1 and pan to (0, 0).
  function canvasBoardScreenTransform(camera, cssWidth, cssHeight) {
    var cam = camera || {};
    var zoom = typeof cam.z === "number" && cam.z > 0 ? cam.z : 1;
    var panX = typeof cam.x === "number" ? cam.x : 0;
    var panY = typeof cam.y === "number" ? cam.y : 0;
    var halfW = cssWidth / 2;
    var halfH = cssHeight / 2;
    return {
      zoom: zoom,
      x: function(worldX) {
        return (worldX - panX) * zoom + halfW;
      },
      // Y is flipped: world +Y (up in NDC) maps to a smaller canvas Y.
      y: function(worldY) {
        return halfH - (worldY - panY) * zoom;
      },
    };
  }

  function materialColor(bundle, index, fallback) {
    var materials = bundle && bundle.materials;
    if (Array.isArray(materials) && typeof index === "number" && index >= 0 && index < materials.length) {
      var color = materials[index] && materials[index].color;
      if (typeof color === "string" && color !== "") {
        return color;
      }
    }
    return fallback;
  }

  // paintCanvasBundle clears ctx with bundle.background, then draws every
  // rect object, line, and label through the OrthoCamera2D screen transform.
  // Tolerant of missing arrays and fields. All drawing is in CSS-logical
  // pixels: cssWidth/cssHeight are the logical viewport the transform centers
  // on, and the CALLER is responsible for any device-pixel-ratio scaling
  // (the render loop pre-applies ctx.setTransform(dpr, …) before calling).
  // dpr is accepted for API symmetry / future use but does not affect the
  // logical clear/fill region.
  function paintCanvasBundle(ctx, bundle, cssWidth, cssHeight, dpr) {
    if (!ctx || !bundle) return;
    var w = cssWidth > 0 ? cssWidth : 1;
    var h = cssHeight > 0 ? cssHeight : 1;
    void dpr;

    // Clear + paint the logical viewport. The caller's setTransform(dpr, …)
    // scales these CSS-pixel rects to cover the full device backing store.
    ctx.clearRect(0, 0, w, h);

    var bg = bundle.background;
    if (typeof bg === "string" && bg !== "") {
      ctx.fillStyle = bg;
      ctx.fillRect(0, 0, w, h);
    }

    var t = canvasBoardScreenTransform(bundle.camera, w, h);

    // Rect objects. RenderObject carries world-space bounds + a material index;
    // color comes from bundle.materials[materialIndex].color (the same lookup
    // the GPU path uses — see render/bundle/object_mesh.go).
    var objects = Array.isArray(bundle.objects) ? bundle.objects : [];
    for (var i = 0; i < objects.length; i++) {
      var obj = objects[i];
      if (!obj || obj.kind !== "rect") continue;
      var b = obj.bounds || {};
      var minX = typeof b.minX === "number" ? b.minX : 0;
      var maxX = typeof b.maxX === "number" ? b.maxX : 0;
      var minY = typeof b.minY === "number" ? b.minY : 0;
      var maxY = typeof b.maxY === "number" ? b.maxY : 0;
      // Top-left of the on-screen rect: smallest screenX (minX) and smallest
      // screenY (which is maxY after the Y flip).
      var sx = t.x(minX);
      var syTop = t.y(maxY);
      var width = (maxX - minX) * t.zoom;
      var height = (maxY - minY) * t.zoom;
      ctx.fillStyle = materialColor(bundle, obj.materialIndex, "#888888");
      ctx.fillRect(sx, syTop, width, height);
    }

    // Lines.
    var lines = Array.isArray(bundle.lines) ? bundle.lines : [];
    for (var j = 0; j < lines.length; j++) {
      var line = lines[j];
      if (!line) continue;
      var from = line.from || {};
      var to = line.to || {};
      ctx.strokeStyle = typeof line.color === "string" && line.color !== "" ? line.color : "#cccccc";
      ctx.lineWidth = typeof line.lineWidth === "number" && line.lineWidth > 0 ? line.lineWidth : 1;
      ctx.beginPath();
      ctx.moveTo(t.x(from.x || 0), t.y(from.y || 0));
      ctx.lineTo(t.x(to.x || 0), t.y(to.y || 0));
      ctx.stroke();
    }

    // Sprites: no image decode in this slice. Draw a faint placeholder box at
    // the sprite's screen position so authors can see the slot, then move on.
    // (Full image loading is a follow-up — see the canvas2d interaction slice.)
    var sprites = Array.isArray(bundle.sprites) ? bundle.sprites : [];
    for (var s = 0; s < sprites.length; s++) {
      var sprite = sprites[s];
      if (!sprite) continue;
      var pos = sprite.position || {};
      var spx = t.x(pos.x || 0);
      var spy = t.y(pos.y || 0);
      var sw = (typeof sprite.width === "number" ? sprite.width : 0) * t.zoom;
      var sh = (typeof sprite.height === "number" ? sprite.height : 0) * t.zoom;
      if (sw > 0 && sh > 0) {
        ctx.fillStyle = "rgba(140,140,160,0.25)";
        ctx.fillRect(spx, spy - sh, sw, sh);
      }
    }

    // Labels.
    var labels = Array.isArray(bundle.labels) ? bundle.labels : [];
    for (var k = 0; k < labels.length; k++) {
      var label = labels[k];
      if (!label) continue;
      var p = label.position || {};
      if (typeof label.font === "string" && label.font !== "") {
        ctx.font = label.font;
      }
      ctx.fillStyle = typeof label.color === "string" && label.color !== "" ? label.color : "#e6edf3";
      ctx.fillText(String(label.text == null ? "" : label.text), t.x(p.x || 0), t.y(p.y || 0));
    }
  }

  window.__gosx_paint_canvas_bundle = paintCanvasBundle;
  window.__gosx_canvas_board_screen_transform = canvasBoardScreenTransform;
})();
