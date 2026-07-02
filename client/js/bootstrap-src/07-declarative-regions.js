// 07-declarative-regions.js — bootstrap-owned declarative server-fragment regions.
//
// An htmx-lite region that re-fetches an HTML fragment and swaps it into place
// when a named shared signal changes or a hub event fires — so apps express
// "live panel" behavior declaratively, with no bespoke fetch/innerHTML JS.
// Self-contained IIFE; resolves runtime globals lazily.
//
//   <div data-gosx-region
//        data-gosx-region-url="/api/.../selection/{value}"  (the {value} token,
//             if present, is filled from the signal's current value, URL-encoded;
//             an empty value suppresses the fetch)
//        data-gosx-region-signal="$some.signal"  (optional: refetch on change)
//        data-gosx-region-on="change"            (optional: hub event names —
//             space/comma separated — that also retrigger a refetch)
//        data-gosx-region-field="tree_html">     (optional: JSON field to inject;
//             absent → the raw response body is the HTML)
//     ...server-rendered initial fragment...
//   </div>
//
// Declarative scene commands ("P6"): a region's swapped-in fragment (or any
// server-rendered markup present at initial load) may embed
//
//   <script type="application/json" data-gosx-scene-commands>
//     [{"kind":0,"objectId":"...","data":{"kind":"label","props":{...}}}]
//   </script>
//
// — the same command array shape GoSXScene3D's public handle.applyCommands()
// already accepts (10-runtime-scene-core.js: applySceneCommands /
// applySceneCreateCommand). After every region swap, and once for whatever
// such payloads are present at initial load, this file parses each tag and
// broadcasts its commands to every mounted GoSXScene3D engine — no bespoke
// per-app MutationObserver/JS required (this generalizes the pattern kiln's
// workspace_comments.go used for live-syncing comment pins before P6 existed).
// Commands are a create-or-replace by objectId (see applySceneCreateCommand),
// so re-applying the same payload more than once (e.g. both the best-effort
// synchronous initial scan below AND the gosx:ready follow-up, for engines
// that mount asynchronously after DOMContentLoaded) is safe and idempotent.
// Malformed JSON warns once and is skipped — it never throws.
(function () {
  if (typeof document === "undefined" || window.__gosxDeclarativeRegions) return;
  window.__gosxDeclarativeRegions = true;

  var SCENE_COMMANDS_SELECTOR = 'script[type="application/json"][data-gosx-scene-commands]';

  // sceneCommandEngineHandles collects the live handle of every mounted
  // GoSXScene3D engine (including each surface in quad-viewport mode) via the
  // same window.__gosx.engines registry the runtime maintains for every
  // engine kind. window.__gosx.engines is a plain Map keyed by engine id,
  // populated by 00-textlayout.js before this file runs in every bundle that
  // carries it (bootstrap.js / bootstrap-lite.js / bootstrap-runtime.js), so
  // it is always safe to read here even before any engine has mounted.
  function sceneCommandEngineHandles() {
    var out = [];
    if (window.__gosx && window.__gosx.engines && typeof window.__gosx.engines.forEach === "function") {
      window.__gosx.engines.forEach(function (rec) {
        if (rec && rec.component === "GoSXScene3D" && rec.handle && typeof rec.handle.applyCommands === "function") {
          out.push(rec.handle);
        }
      });
    }
    return out;
  }

  // applySceneCommandsJSON parses one script tag's textContent as a scene
  // command array and broadcasts it to every mounted GoSXScene3D handle.
  // Malformed JSON, a non-array payload, or zero mounted engines are all
  // silent no-ops (a warn for the former, nothing for the latter two) — this
  // must never throw and break the region swap or page load it runs from.
  function applySceneCommandsJSON(raw) {
    var commands;
    try {
      commands = JSON.parse(raw || "[]");
    } catch (err) {
      console.warn("[gosx] scene command payload parse failed:", err);
      return;
    }
    if (!Array.isArray(commands) || commands.length === 0) return;
    var handles = sceneCommandEngineHandles();
    for (var i = 0; i < handles.length; i++) {
      handles[i].applyCommands(commands);
    }
  }

  // applySceneCommandScripts finds every data-gosx-scene-commands payload
  // under `root` (a freshly-swapped region element, or `document` for the
  // initial-load pass) and applies each one.
  function applySceneCommandScripts(root) {
    if (!root || typeof root.querySelectorAll !== "function") return;
    var tags = root.querySelectorAll(SCENE_COMMANDS_SELECTOR);
    for (var i = 0; i < tags.length; i++) {
      applySceneCommandsJSON(tags[i].textContent);
    }
  }

  function splitEvents(spec) {
    return String(spec || "")
      .split(/[\s,]+/)
      .map(function (s) { return s.trim(); })
      .filter(Boolean);
  }

  function bindRegion(el) {
    var url = el.getAttribute("data-gosx-region-url");
    if (!url) return;
    var signalName = el.getAttribute("data-gosx-region-signal");
    var onEvents = splitEvents(el.getAttribute("data-gosx-region-on"));
    var field = el.getAttribute("data-gosx-region-field") || "";
    // allow-empty: still fetch when {value} is empty, substituting "" (e.g. a
    // tree fragment ?selected= with no selection). Without it, an empty {value}
    // suppresses the fetch (the default — avoids hitting e.g. /selection/ with a
    // blank id).
    var allowEmpty = el.hasAttribute("data-gosx-region-allow-empty");
    var lastValue = "";

    function refresh() {
      var u = url;
      if (u.indexOf("{value}") >= 0) {
        if (!lastValue && !allowEmpty) return; // no value yet → nothing to fetch
        u = u.replace("{value}", encodeURIComponent(lastValue || ""));
      }
      fetch(u, { headers: { Accept: field ? "application/json" : "text/html" } })
        .then(function (r) { return r.ok ? (field ? r.json() : r.text()) : null; })
        .then(function (data) {
          if (data == null) return;
          var html = field ? data[field] : data;
          if (typeof html === "string") {
            el.innerHTML = html;
            applySceneCommandScripts(el);
          }
        })
        .catch(function (err) { console.warn("[gosx] region refresh", u, err); });
    }

    if (signalName && typeof window.__gosx_subscribe_shared_signal === "function") {
      window.__gosx_subscribe_shared_signal(
        signalName,
        function (v) {
          lastValue = v == null ? "" : String(v);
          refresh();
        },
        { immediate: false }
      );
    }
    if (onEvents.length) {
      document.addEventListener("gosx:hub:event", function (e) {
        var ev = e && e.detail && e.detail.event;
        if (ev && onEvents.indexOf(ev) >= 0) refresh();
      });
    }
  }

  function scan() {
    var nodes = document.querySelectorAll("[data-gosx-region]");
    for (var i = 0; i < nodes.length; i++) bindRegion(nodes[i]);

    // Initial-load scene-command payloads (SSR-rendered, no swap involved).
    // A GoSXScene3D engine mounts asynchronously (WASM/program fetch), so this
    // synchronous pass is best-effort — it only reaches engines that happen to
    // already be mounted (e.g. in tests, or a page with no WASM runtime at
    // all). gosx:ready fires exactly once, after every manifest engine has
    // finished mounting, and is the reliable pass for the common case; both
    // are safe to run since commands are a create-or-replace by objectId.
    applySceneCommandScripts(document);
    document.addEventListener("gosx:ready", function () {
      applySceneCommandScripts(document);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", scan);
  } else {
    scan();
  }
})();
