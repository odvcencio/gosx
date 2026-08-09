// Lantern 🏮 — the built-in gosx Scene3D dev inspector.
//
// Served by the gosx server at /gosx/devtools-lantern.js. Apps opt in by
// including the script tag (for example only when devtools are enabled or a
// debug query param is present); the script never loads for ordinary
// visitors unless the app injects it. Toggle the overlay with Shift+D.
//
// Lantern consumes the runtime debug registry that already ships in the
// production bundle:
//   - window.__gosx_scene3d_debug.listSurfaces() / .inspect(id) — live scene
//     graph: per-node-type counts, feature matrix, camera, GPU resources,
//     renderer diagnostics. (depth-6 deep clone per call — poll at ~1.5 Hz,
//     never at frame rate.)
//   - window.__gosx_scene3d_telemetry(mount) — cheap backend/tier/quality strip.
//   - mount.__gosxScene3DWebGPUStats — per-frame {frameSeq, frameAt} for a
//     truthful render-FPS readout (WebGPU only; WebGL falls back to rAF).
//   - window.__gosx.engines.get(engineID).handle.getCamera() — live camera.
//
// Read-only by design today: it reflects the truth the framework already
// knows. Live write-back (signal-bound controls, node mutation via the scene
// command bus) is the planned phase-2 increment.

// Enable cull survivor-count readback before the scene mounts (zero overhead
// when absent). Harmless on pages with no scene.
window.__gosx_scene3d_cull_telemetry = true;

(function () {
  "use strict";

  var PANEL_ID = "lantern-panel";
  var STYLE_ID = "lantern-style";
  var POLL_MS = 650;

  var state = {
    panel: null,
    body: null,
    select: null,
    timer: null,
    open: false,
    selectedSurface: null,
    fps: {}, // surfaceID -> { seq, at, value, mode }
  };

  // ---- runtime accessors (all defensive: the page may have no scene) ------

  function debug() {
    return (typeof window.__gosx_scene3d_debug === "object" && window.__gosx_scene3d_debug) || null;
  }

  function listSurfaces() {
    var d = debug();
    if (!d || typeof d.listSurfaces !== "function") return [];
    try {
      var s = d.listSurfaces();
      return Array.isArray(s) ? s : [];
    } catch (_e) {
      return [];
    }
  }

  function inspect(id) {
    var d = debug();
    if (!d || typeof d.inspect !== "function") return null;
    try {
      return d.inspect(id);
    } catch (_e) {
      return null;
    }
  }

  // Mount DOM elements carry data-gosx-scene3d-backend; the telemetry fn reads
  // its data-* attributes. This also gives us the element holding per-frame
  // WebGPU stats.
  function sceneMounts() {
    try {
      return Array.prototype.slice.call(document.querySelectorAll("[data-gosx-scene3d-backend]"));
    } catch (_e) {
      return [];
    }
  }

  function telemetry(mount) {
    if (typeof window.__gosx_scene3d_telemetry !== "function") return null;
    try {
      return window.__gosx_scene3d_telemetry(mount || null);
    } catch (_e) {
      return null;
    }
  }

  function engineHandle(engineID) {
    try {
      var reg = window.__gosx && window.__gosx.engines;
      if (!reg || typeof reg.get !== "function" || !engineID) return null;
      var entry = reg.get(engineID);
      return (entry && entry.handle) || null;
    } catch (_e) {
      return null;
    }
  }

  // Find the mount element for a snapshot (by id / mountID), to read per-frame
  // stats. Falls back to the first scene mount on the page.
  function mountForSnapshot(snap) {
    var mounts = sceneMounts();
    if (!snap) return mounts[0] || null;
    for (var i = 0; i < mounts.length; i++) {
      var el = mounts[i];
      if (el.id && (el.id === snap.mountID || el.id === snap.id)) return el;
    }
    return mounts[0] || null;
  }

  // Truthful render FPS from the scene's own frame counter when available
  // (WebGPU), else a rAF-derived browser FPS labelled accordingly.
  function computeFps(surfaceID, mount) {
    var rec = state.fps[surfaceID] || { seq: null, at: null, value: null, mode: "" };
    var stats = mount && mount.__gosxScene3DWebGPUStats;
    var now = (window.performance && performance.now) ? performance.now() : Date.now();
    if (stats && typeof stats.frameSeq === "number" && typeof stats.frameAt === "number") {
      if (rec.seq != null && stats.frameAt > rec.at) {
        var df = stats.frameSeq - rec.seq;
        var dt = (stats.frameAt - rec.at) / 1000;
        if (dt > 0 && df >= 0) rec.value = df / dt;
      }
      rec.seq = stats.frameSeq;
      rec.at = stats.frameAt;
      rec.mode = "render";
    } else {
      // No per-frame counter (WebGL): approximate from wall clock + rAF tick.
      rec.mode = "rAF";
    }
    state.fps[surfaceID] = rec;
    return rec;
  }

  // ---- formatting ---------------------------------------------------------

  function num(v, digits) {
    return v != null && isFinite(v) ? Number(v).toFixed(digits == null ? 0 : digits) : "–";
  }

  function bool(v) {
    return v === true ? "yes" : v === false ? "no" : "–";
  }

  function el(tag, attrs, kids) {
    var node = document.createElement(tag);
    if (attrs) {
      for (var k in attrs) {
        if (!Object.prototype.hasOwnProperty.call(attrs, k)) continue;
        if (k === "class") node.className = attrs[k];
        else if (k === "text") node.textContent = attrs[k];
        else node.setAttribute(k, attrs[k]);
      }
    }
    if (kids) {
      for (var i = 0; i < kids.length; i++) {
        if (kids[i] != null) node.appendChild(kids[i]);
      }
    }
    return node;
  }

  function row(label, value, cls) {
    return el("div", { class: "ln-row" + (cls ? " " + cls : "") }, [
      el("span", { class: "ln-k", text: label }),
      el("span", { class: "ln-v", text: value }),
    ]);
  }

  // ---- panel construction -------------------------------------------------

  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    var css = [
      "#" + PANEL_ID + "{position:fixed;top:12px;right:12px;width:340px;max-height:84vh;",
      "  overflow:auto;z-index:2147483000;background:rgba(14,9,3,0.92);color:#f5e6d0;",
      "  border:1px solid rgba(255,184,108,0.32);border-radius:10px;",
      "  box-shadow:0 8px 40px rgba(0,0,0,0.55),0 0 0 1px rgba(255,184,108,0.06);",
      "  font:11px/1.55 ui-monospace,SFMono-Regular,Menlo,monospace;",
      "  backdrop-filter:blur(8px);-webkit-backdrop-filter:blur(8px);}",
      "#" + PANEL_ID + " .ln-head{display:flex;align-items:center;gap:8px;padding:9px 12px;",
      "  position:sticky;top:0;background:rgba(20,12,4,0.96);",
      "  border-bottom:1px solid rgba(255,184,108,0.18);}",
      "#" + PANEL_ID + " .ln-title{font-weight:700;color:#ffcf8d;letter-spacing:.04em;}",
      "#" + PANEL_ID + " .ln-hint{margin-left:auto;color:#a08a6c;font-size:10px;}",
      "#" + PANEL_ID + " select{flex:1;min-width:0;background:rgba(0,0,0,0.4);color:#f5e6d0;",
      "  border:1px solid rgba(255,184,108,0.25);border-radius:5px;padding:2px 4px;font:inherit;}",
      "#" + PANEL_ID + " .ln-sec{padding:8px 12px;border-bottom:1px solid rgba(255,184,108,0.1);}",
      "#" + PANEL_ID + " .ln-sec-t{color:#ffb86c;text-transform:uppercase;letter-spacing:.08em;",
      "  font-size:9.5px;margin:0 0 6px;opacity:.85;}",
      "#" + PANEL_ID + " .ln-row{display:flex;justify-content:space-between;gap:10px;padding:1px 0;}",
      "#" + PANEL_ID + " .ln-k{color:#a08a6c;}",
      "#" + PANEL_ID + " .ln-v{color:#f5e6d0;text-align:right;word-break:break-word;}",
      "#" + PANEL_ID + " .ln-node{display:flex;justify-content:space-between;gap:10px;padding:3px 6px;",
      "  border-radius:5px;cursor:default;}",
      "#" + PANEL_ID + " .ln-node .ln-cnt{color:#ffcf8d;font-weight:600;}",
      "#" + PANEL_ID + " .ln-warn{color:#ff9f6e;}",
      "#" + PANEL_ID + " .ln-empty{color:#8a7558;padding:18px 12px;text-align:center;}",
    ].join("");
    var style = el("style", { id: STYLE_ID });
    style.textContent = css;
    (document.head || document.documentElement).appendChild(style);
  }

  function buildPanel() {
    ensureStyle();
    state.select = el("select", { title: "scene surface" });
    state.select.addEventListener("change", function () {
      state.selectedSurface = state.select.value || null;
      render();
    });
    var head = el("div", { class: "ln-head" }, [
      el("span", { class: "ln-title", text: "🏮 Lantern" }),
      state.select,
      el("span", { class: "ln-hint", text: "Shift+D" }),
    ]);
    state.body = el("div", { class: "ln-bodywrap" });
    state.panel = el("div", { id: PANEL_ID }, [head, state.body]);
    document.body.appendChild(state.panel);
  }

  // ---- rendering ----------------------------------------------------------

  // Ordered, friendly node-type rows for the outliner. Keys match the debug
  // snapshot's `counts` (the live render-bundle tally).
  var NODE_ROWS = [
    ["meshObjects", "△ meshes"],
    ["instancedMeshes", "▦ instanced"],
    ["instancedGLBMeshes", "▦ instanced GLB"],
    ["points", "∴ points"],
    ["computeParticles", "✦ compute particles"],
    ["lines", "╱ lines"],
    ["sprites", "◈ sprites"],
    ["labels", "T labels"],
    ["html", "⌗ html"],
    ["lights", "☀ lights"],
    ["postEffects", "❂ post FX"],
    ["materials", "◐ materials"],
  ];

  function syncSurfaceOptions(surfaces) {
    var sel = state.select;
    var want = surfaces.map(function (s) { return s.id; }).join("|");
    if (sel.getAttribute("data-keys") === want) return;
    sel.setAttribute("data-keys", want);
    sel.innerHTML = "";
    for (var i = 0; i < surfaces.length; i++) {
      var s = surfaces[i];
      var label = (s.component || s.id || "scene") + (s.mountID ? " #" + s.mountID : "");
      sel.appendChild(el("option", { value: s.id, text: label }));
    }
    if (!state.selectedSurface || want.indexOf(state.selectedSurface) === -1) {
      state.selectedSurface = surfaces.length ? surfaces[0].id : null;
    }
    sel.value = state.selectedSurface || "";
  }

  function renderEmpty() {
    state.body.innerHTML = "";
    state.body.appendChild(el("div", { class: "ln-empty", text:
      debug() ? "waiting for a scene to mount…" : "no gosx Scene3D runtime on this page" }));
  }

  function section(title, rows) {
    var kids = [el("p", { class: "ln-sec-t", text: title })];
    for (var i = 0; i < rows.length; i++) kids.push(rows[i]);
    return el("div", { class: "ln-sec" }, kids);
  }

  function render() {
    if (!state.open || !state.panel) return;
    var surfaces = listSurfaces();
    if (!surfaces.length) { renderEmpty(); return; }
    syncSurfaceOptions(surfaces);

    var id = state.selectedSurface;
    var snap = inspect(id);
    var mount = mountForSnapshot(snap);
    var tel = telemetry(mount);
    var fps = computeFps(id, mount);

    var frag = document.createDocumentFragment();

    // --- Perf strip ---
    var perfRows = [];
    var fpsTxt = fps.value != null ? num(fps.value, 0) + " fps" : (fps.mode === "rAF" ? "(rAF)" : "–");
    perfRows.push(row("fps", fpsTxt, fps.value != null && fps.value < 45 ? "ln-warn" : ""));
    if (tel) {
      perfRows.push(row("backend", String(tel.backend || "?")));
      perfRows.push(row("tier", String(tel.capabilityTier || "?")));
      perfRows.push(row("quality-ms", num(tel.qualityFrameMs, 1)));
      perfRows.push(row("dpr-cap", num(tel.qualityDprCap, 2)));
      perfRows.push(row("postfx-supp", bool(tel.qualityPostfxSuppressed)));
      perfRows.push(row("in-viewport", bool(tel.inViewport)));
      perfRows.push(row("dropped", String(tel.dropped != null ? tel.dropped : 0), (tel.dropped > 0 ? "ln-warn" : "")));
      perfRows.push(row("device-mem", (tel.deviceMemory > 0 ? tel.deviceMemory + " GB" : "–") + " / " + (tel.hardwareConcurrency > 0 ? tel.hardwareConcurrency + " cores" : "– cores")));
    } else if (snap) {
      perfRows.push(row("renderer", String(snap.renderer || "?")));
      perfRows.push(row("ready", bool(snap.ready)));
    }
    frag.appendChild(section("performance", perfRows));

    // --- Outliner (live node-type counts) ---
    var counts = (snap && snap.counts) || {};
    var nodeKids = [];
    for (var i = 0; i < NODE_ROWS.length; i++) {
      var key = NODE_ROWS[i][0], lbl = NODE_ROWS[i][1];
      var c = counts[key];
      if (!c) continue; // hide zero-count types to keep the tree focused
      nodeKids.push(el("div", { class: "ln-node" }, [
        el("span", { class: "ln-k", text: lbl }),
        el("span", { class: "ln-cnt", text: String(c) }),
      ]));
    }
    if (!nodeKids.length) nodeKids.push(el("div", { class: "ln-empty", text: "empty scene graph" }));
    frag.appendChild(section("scene graph" + (id ? " · " + id : ""), nodeKids));

    // --- Render detail ---
    if (snap) {
      var detRows = [];
      detRows.push(row("draw calls", String(counts.drawCalls != null ? counts.drawCalls : "–")));
      if (counts.worldVertexCount != null) detRows.push(row("vertices", String(counts.worldVertexCount)));
      if (snap.fallbackReason) detRows.push(row("fallback", String(snap.fallbackReason), "ln-warn"));
      if (snap.viewport) detRows.push(row("viewport", num(snap.viewport.cssWidth, 0) + "×" + num(snap.viewport.cssHeight, 0) + " @" + num(snap.viewport.devicePixelRatio, 2) + "x"));
      var cam = snap.camera;
      if (cam) {
        detRows.push(row("camera", num(cam.x, 1) + ", " + num(cam.y, 1) + ", " + num(cam.z, 1)));
        if (cam.fov != null) detRows.push(row("fov", num(cam.fov, 0) + "°"));
      } else {
        // Snapshot omitted camera (summary mode) — pull it live off the handle.
        var h = engineHandle(snap.engineID);
        if (h && typeof h.getCamera === "function") {
          try {
            var lc = h.getCamera();
            if (lc) detRows.push(row("camera", num(lc.x, 1) + ", " + num(lc.y, 1) + ", " + num(lc.z, 1)));
          } catch (_e) { /* ignore */ }
        }
      }
      if (snap.diagnostics && snap.diagnostics.length) {
        detRows.push(row("diagnostics", String(snap.diagnostics.length) + " note(s)", "ln-warn"));
      }
      frag.appendChild(section("render", detRows));
    }

    state.body.innerHTML = "";
    state.body.appendChild(frag);
  }

  // ---- lifecycle ----------------------------------------------------------

  function startPolling() {
    if (state.timer) return;
    render();
    state.timer = setInterval(render, POLL_MS);
  }

  function stopPolling() {
    if (state.timer) { clearInterval(state.timer); state.timer = null; }
  }

  function open() {
    if (!state.panel) buildPanel();
    state.panel.style.display = "";
    state.open = true;
    startPolling();
  }

  function close() {
    state.open = false;
    stopPolling();
    if (state.panel) state.panel.style.display = "none";
  }

  function toggle() {
    if (state.open) close(); else open();
  }

  // Shift+D toggles. Ignore while typing in a field, and don't fight browser
  // shortcuts that already use a modifier beyond Shift.
  function onKeyDown(e) {
    if (!e || !e.shiftKey || e.ctrlKey || e.metaKey || e.altKey) return;
    var code = e.code || "";
    var key = (e.key || "").toLowerCase();
    if (code !== "KeyD" && key !== "d") return;
    var t = e.target;
    if (t && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName || ""))) return;
    e.preventDefault();
    toggle();
  }

  document.addEventListener("keydown", onKeyDown, true);
  window.addEventListener("beforeunload", function () {
    stopPolling();
    document.removeEventListener("keydown", onKeyDown, true);
  }, { once: true });

  // Discoverability: one quiet console hint (dev-only surface anyway).
  try {
    if (window.console && console.info) {
      console.info("%c🏮 Lantern%c gosx Scene3D inspector ready — press Shift+D",
        "color:#ffcf8d;font-weight:700", "color:#a08a6c");
    }
  } catch (_e) { /* ignore */ }
})();
