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
  var sceneCommandPendingByTarget = new Map();
  var sceneCommandReadyByTarget = new Map();
  var sceneCommandRevision = Math.max(0, Math.floor(Number(window.__gosx_scene3d_command_revision || 0) || 0));

  function nextSceneCommandRevision() {
    sceneCommandRevision += 1;
    window.__gosx_scene3d_command_revision = sceneCommandRevision;
    return sceneCommandRevision;
  }

  function sceneCommandTargetKey(target, options) {
    if (options && typeof options.engineID === "string" && options.engineID.trim()) return options.engineID.trim();
    if (typeof target === "string" && target.trim()) return target.trim();
    if (target && typeof target.id === "string" && target.id.trim()) return target.id.trim();
    if (target && target.__gosxScene3DHandle && typeof target.__gosxScene3DHandle.applyCommands === "function" && target.__gosxScene3DHandle.__gosxScene3DCommandTarget) {
      return target.__gosxScene3DHandle.__gosxScene3DCommandTarget;
    }
    return "";
  }

  function sceneCommandHandleReady(handle) {
    return Boolean(handle && typeof handle.applyCommands === "function" && handle.__gosxScene3DCommandReady === true);
  }

  function sceneCommandReadyHandle(target, options) {
    if (sceneCommandHandleReady(target)) return { handle: target, mount: null };
    if (target && target.__gosxScene3DHandle && sceneCommandHandleReady(target.__gosxScene3DHandle)) {
      return { handle: target.__gosxScene3DHandle, mount: target };
    }
    var key = sceneCommandTargetKey(target, options);
    if (!key) return null;
    var readyRecord = sceneCommandReadyByTarget.get(key);
    if (readyRecord && sceneCommandHandleReady(readyRecord.handle)) {
      return readyRecord;
    }
    var mount = typeof document !== "undefined" && typeof document.getElementById === "function"
      ? document.getElementById(key)
      : null;
    if (mount && mount.__gosxScene3DHandle && sceneCommandHandleReady(mount.__gosxScene3DHandle)) {
      return { handle: mount.__gosxScene3DHandle, mount: mount };
    }
    if (window.__gosx && window.__gosx.engines && typeof window.__gosx.engines.get === "function") {
      var record = window.__gosx.engines.get(key);
      if (record && sceneCommandHandleReady(record.handle)) {
        return { handle: record.handle, mount: record.mount || mount || null };
      }
    }
    return null;
  }

  function sceneCommandSetRevisionAttr(mount, attr, revision) {
    if (mount && typeof mount.setAttribute === "function") {
      mount.setAttribute(attr, String(revision));
    }
  }

  function sceneCommandApplyReady(handleRecord, commands, revision) {
    var handle = handleRecord && handleRecord.handle;
    var mount = handleRecord && handleRecord.mount;
    sceneCommandSetRevisionAttr(mount, "data-gosx-scene3d-command-revision", revision);
    var result;
    try {
      result = handle.applyCommands(commands);
    } catch (error) {
      return Promise.reject(error);
    }
    return Promise.resolve(result).then(function() {
      sceneCommandSetRevisionAttr(mount, "data-gosx-scene3d-command-applied-revision", revision);
      return { revision: revision, applied: true };
    });
  }

  function sceneCommandQueue(targetKey, entry) {
    var list = sceneCommandPendingByTarget.get(targetKey);
    if (!list) {
      list = [];
      sceneCommandPendingByTarget.set(targetKey, list);
    }
    list.push(entry);
  }

  function sceneCommandRejectEntry(entry, reason) {
    if (entry && entry.timer) {
      clearTimeout(entry.timer);
      entry.timer = null;
    }
    entry.reject(new Error(reason || "Scene3D command target did not become ready"));
  }

  function sceneCommandFlushTarget(targetKey, handle, mount) {
    if (!targetKey) return;
    var list = sceneCommandPendingByTarget.get(targetKey);
    if (!list || list.length === 0) return;
    sceneCommandPendingByTarget.delete(targetKey);
    for (var i = 0; i < list.length; i++) {
      (function(entry) {
        if (entry.timer) {
          clearTimeout(entry.timer);
          entry.timer = null;
        }
        sceneCommandApplyReady({ handle: handle, mount: mount || null }, entry.commands, entry.revision).then(entry.resolve, entry.reject);
      })(list[i]);
    }
  }

  function dispatchSceneCommands(target, commands, options) {
    if (!Array.isArray(commands)) {
      return Promise.reject(new TypeError("Scene3D commands must be an array"));
    }
    var revision = nextSceneCommandRevision();
    var ready = sceneCommandReadyHandle(target, options || {});
    if (ready) {
      return sceneCommandApplyReady(ready, commands, revision);
    }
    var targetKey = sceneCommandTargetKey(target, options || {});
    if (!targetKey) {
      return Promise.reject(new Error("Scene3D command target is not ready and has no stable id"));
    }
    return new Promise(function(resolve, reject) {
      var timeoutMS = Math.max(0, Math.floor(Number(options && options.timeoutMS) || 10000));
      var entry = {
        revision: revision,
        commands: commands,
        resolve: resolve,
        reject: reject,
        timer: null,
      };
      if (timeoutMS > 0) {
        entry.timer = setTimeout(function() {
          var list = sceneCommandPendingByTarget.get(targetKey);
          if (list) {
            var index = list.indexOf(entry);
            if (index >= 0) list.splice(index, 1);
            if (list.length === 0) sceneCommandPendingByTarget.delete(targetKey);
          }
          sceneCommandRejectEntry(entry, "Scene3D command target did not become ready: " + targetKey);
        }, timeoutMS);
      }
      sceneCommandQueue(targetKey, entry);
      var afterQueueReady = sceneCommandReadyHandle(targetKey, options || {});
      if (afterQueueReady) {
        sceneCommandFlushTarget(targetKey, afterQueueReady.handle, afterQueueReady.mount);
      }
    });
  }

  function markSceneCommandReady(mount, handle, options) {
    if (!handle || typeof handle.applyCommands !== "function") return false;
    var targetKey = sceneCommandTargetKey(mount, options || {}) || (options && options.engineID) || "";
    handle.__gosxScene3DCommandReady = true;
    handle.__gosxScene3DCommandTarget = targetKey;
    handle.__gosxScene3DCommandTargets = [];
    var readyRecord = { handle: handle, mount: mount || null };
    if (targetKey) {
      sceneCommandReadyByTarget.set(targetKey, readyRecord);
      handle.__gosxScene3DCommandTargets.push(targetKey);
    }
    if (mount && typeof mount.setAttribute === "function") {
      mount.setAttribute("data-gosx-scene3d-command-ready", "true");
    }
    sceneCommandFlushTarget(targetKey, handle, mount || null);
    var mountKey = sceneCommandTargetKey(mount, {});
    if (mountKey && mountKey !== targetKey) {
      sceneCommandReadyByTarget.set(mountKey, readyRecord);
      handle.__gosxScene3DCommandTargets.push(mountKey);
      sceneCommandFlushTarget(mountKey, handle, mount || null);
    }
    var engineKey = options && typeof options.engineID === "string" ? options.engineID.trim() : "";
    if (engineKey && engineKey !== targetKey && engineKey !== mountKey) {
      sceneCommandReadyByTarget.set(engineKey, readyRecord);
      handle.__gosxScene3DCommandTargets.push(engineKey);
      sceneCommandFlushTarget(engineKey, handle, mount || null);
    }
    return true;
  }

  function clearSceneCommandReady(target) {
    var keys = [];
    var directKey = sceneCommandTargetKey(target, {});
    if (directKey) keys.push(directKey);
    var handle = target && target.__gosxScene3DHandle ? target.__gosxScene3DHandle : null;
    if (!handle && sceneCommandHandleReady(target)) handle = target;
    if (handle && handle.__gosxScene3DCommandTarget) keys.push(String(handle.__gosxScene3DCommandTarget));
    if (handle && Array.isArray(handle.__gosxScene3DCommandTargets)) {
      for (var j = 0; j < handle.__gosxScene3DCommandTargets.length; j += 1) {
        keys.push(String(handle.__gosxScene3DCommandTargets[j]));
      }
    }
    for (var i = 0; i < keys.length; i += 1) {
      sceneCommandReadyByTarget.delete(keys[i]);
    }
    if (handle) {
      handle.__gosxScene3DCommandReady = false;
      handle.__gosxScene3DCommandTargets = [];
    }
  }

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

  function applySceneCommandsToMountedHandle(handle, commands) {
    try {
      var result = handle.applyCommands(commands);
      if (result && typeof result.then === "function") {
        result.catch(function (err) {
          console.warn("[gosx] scene command apply failed:", err);
        });
      }
    } catch (err) {
      console.warn("[gosx] scene command apply failed:", err);
    }
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
      applySceneCommandsToMountedHandle(handles[i], commands);
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

  var regionBindings = new Map();
  var readyListenerInstalled = false;

  function createRegionTransport() {
    if (window.__gosx && window.__gosx.transport && typeof window.__gosx.transport.scope === "function") {
      return window.__gosx.transport.scope();
    }
    return null;
  }

  function emit(type, detail) {
    if (typeof document.dispatchEvent !== "function" || typeof CustomEvent !== "function") return;
    document.dispatchEvent(new CustomEvent(type, { detail: detail }));
  }

  function observeRegion(level, message, fields) {
    if (typeof window.__gosx_emit !== "function") return;
    window.__gosx_emit(level, "region", message, fields || {});
  }

  function setRegionState(el, state, url) {
    if (!el || typeof el.setAttribute !== "function") return;
    el.setAttribute("data-gosx-region-state", state);
    if (url) el.setAttribute("data-gosx-region-request", url);
    else if (typeof el.removeAttribute === "function") el.removeAttribute("data-gosx-region-request");
  }

  function regionRequestController(record) {
    if (record.requestController && typeof record.requestController.abort === "function") {
      record.requestController.abort();
    }
    record.requestController = typeof AbortController === "function" ? new AbortController() : null;
    return record.requestController;
  }

  function fetchRegion(record, url) {
    var el = record.element;
    var controller = null;
    if (!record.transport) record.transport = createRegionTransport();
    var request = { headers: { Accept: record.field ? "application/json" : "text/html" } };
    var fetcher;
    if (record.transport && typeof record.transport.requestLatest === "function") {
      fetcher = function (input, init) {
        return record.transport.requestLatest("refresh", input, init);
      };
    } else {
      controller = regionRequestController(record);
      if (controller) request.signal = controller.signal;
      fetcher = window.__gosx && typeof window.__gosx.request === "function"
        ? window.__gosx.request
        : (typeof window.fetch === "function" ? window.fetch.bind(window) : fetch);
    }
    setRegionState(el, "pending", url);
    emit("gosx:region:before", { element: el, url: url });
    observeRegion("debug", "region refresh started", { url: url });
    return fetcher(url, request)
      .then(function (response) {
        if (!response || !response.ok) return null;
        return record.field ? response.json() : response.text();
      })
      .then(function (data) {
        if (record.disposed || (controller && controller.signal.aborted) || data == null) return;
        var html = record.field ? data[record.field] : data;
        if (typeof html !== "string") return;
        if (typeof window.__gosx_replace_runtime_content === "function") {
          if (!window.__gosx_replace_runtime_content(el, html)) {
            throw new Error("runtime content replacement failed");
          }
        } else {
          if (typeof window.__gosx_dispose_runtime_surfaces === "function") {
            window.__gosx_dispose_runtime_surfaces(el);
          }
          el.innerHTML = html;
          applySceneCommandScripts(el);
          if (typeof window.__gosx_mount_runtime_surfaces === "function") {
            window.__gosx_mount_runtime_surfaces(el);
          }
        }
        setRegionState(el, "ready", "");
        emit("gosx:region:after", { element: el, url: url, html: html });
        observeRegion("info", "region refresh completed", { url: url });
      })
      .catch(function (err) {
        if (
          record.disposed ||
          (controller && controller.signal.aborted) ||
          (err && String(err.name || "") === "AbortError")
        ) return;
        setRegionState(el, "error", "");
        if (window.__gosx && typeof window.__gosx.reportFailure === "function") {
          window.__gosx.reportFailure("region refresh", err, {
            scope: "region",
            type: "refresh",
            source: url,
            element: el,
            fallback: "server",
            telemetry: { url: url },
          });
        } else if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
          window.__gosx.reportIssue({
            scope: "region",
            type: "refresh",
            source: url,
            element: el,
            error: err,
            fallback: "server",
          });
        } else {
          console.warn("[gosx] region refresh", url, err);
        }
        emit("gosx:region:error", { element: el, url: url, error: err });
        observeRegion("error", "region refresh failed", { url: url });
      });
  }

  function bindRegion(el) {
    if (!el || regionBindings.has(el)) return regionBindings.get(el) || null;
    var url = el.getAttribute("data-gosx-region-url");
    if (!url) return null;
    var signalName = el.getAttribute("data-gosx-region-signal");
    var onEvents = splitEvents(el.getAttribute("data-gosx-region-on"));
    var field = el.getAttribute("data-gosx-region-field") || "";
    // allow-empty: still fetch when {value} is empty, substituting "" (e.g. a
    // tree fragment ?selected= with no selection). Without it, an empty {value}
    // suppresses the fetch (the default — avoids hitting e.g. /selection/ with a
    // blank id).
    var allowEmpty = el.hasAttribute("data-gosx-region-allow-empty");
    var record = {
      element: el,
      field: field,
      signalName: signalName,
      onEvents: onEvents,
      transport: createRegionTransport(),
      requestController: null,
      unsubscribe: null,
      hubListener: null,
      disposed: false,
      lastValue: "",
    };

    record.refresh = function () {
      if (record.disposed) return Promise.resolve(false);
      var requestURL = url;
      if (requestURL.indexOf("{value}") >= 0) {
        if (!record.lastValue && !allowEmpty) return Promise.resolve(false);
        requestURL = requestURL.replace("{value}", encodeURIComponent(record.lastValue || ""));
      }
      return fetchRegion(record, requestURL).then(function () { return true; });
    };

    if (signalName && typeof window.__gosx_subscribe_shared_signal === "function") {
      record.unsubscribe = window.__gosx_subscribe_shared_signal(
        signalName,
        function (value) {
          record.lastValue = value == null ? "" : String(value);
          record.refresh();
        },
        { immediate: false }
      );
    }
    if (onEvents.length) {
      record.hubListener = function (event) {
        var eventName = event && event.detail && event.detail.event;
        if (eventName && onEvents.indexOf(eventName) >= 0) record.refresh();
      };
      document.addEventListener("gosx:hub:event", record.hubListener);
    }
    regionBindings.set(el, record);
    setRegionState(el, "ready", "");
    return record;
  }

  function inRoot(root, element, options) {
    if (!root || root === document || root === document.body || root === document.documentElement) return true;
    if (options && options.preserveRoot && root === element) return false;
    return root === element || (typeof root.contains === "function" && root.contains(element));
  }

  function dispose(root, options) {
    for (var entry of Array.from(regionBindings.entries())) {
      var el = entry[0];
      var record = entry[1];
      if (!inRoot(root, el, options)) continue;
      record.disposed = true;
      if (record.requestController && typeof record.requestController.abort === "function") record.requestController.abort();
      if (record.transport && typeof record.transport.dispose === "function") record.transport.dispose();
      if (typeof record.unsubscribe === "function") record.unsubscribe();
      if (record.hubListener) document.removeEventListener("gosx:hub:event", record.hubListener);
      regionBindings.delete(el);
      setRegionState(el, "disposed", "");
    }
  }

  function scan(root) {
    var host = root || document;
    var nodes = [];
    if (host && typeof host.hasAttribute === "function" && host.hasAttribute("data-gosx-region")) {
      nodes.push(host);
    }
    if (host && typeof host.querySelectorAll === "function") {
      for (var candidate of Array.from(host.querySelectorAll("[data-gosx-region]"))) {
        if (nodes.indexOf(candidate) < 0) nodes.push(candidate);
      }
    }
    for (var i = 0; i < nodes.length; i++) bindRegion(nodes[i]);

    // Initial-load scene-command payloads (SSR-rendered, no swap involved).
    // A GoSXScene3D engine mounts asynchronously (WASM/program fetch), so this
    // synchronous pass is best-effort — it only reaches engines that happen to
    // already be mounted. gosx:ready is the reliable follow-up pass; both are
    // safe because scene commands are create-or-replace by objectId.
    applySceneCommandScripts(host === document ? document : host);
    if (!readyListenerInstalled) {
      readyListenerInstalled = true;
      document.addEventListener("gosx:ready", function () {
        applySceneCommandScripts(document);
      });
    }
    return regionBindings;
  }

  function refresh(element) {
    var record = regionBindings.get(element) || bindRegion(element);
    if (!record || typeof record.refresh !== "function") return Promise.resolve(false);
    return record.refresh();
  }

  window.__gosx_mount_declarative_regions = scan;
  window.__gosx_dispose_declarative_regions = dispose;
  window.__gosx_apply_scene_command_scripts = applySceneCommandScripts;
  window.__gosx_scene3d_dispatch_commands = dispatchSceneCommands;
  window.__gosx_scene3d_mark_command_ready = markSceneCommandReady;
  window.__gosx_scene3d_clear_command_ready = clearSceneCommandReady;
  window.__gosx = window.__gosx || {};
  window.__gosx.scene3d = window.__gosx.scene3d || {};
  window.__gosx.scene3d.dispatchCommands = dispatchSceneCommands;
  window.__gosx_scene3d = window.__gosx_scene3d || {};
  window.__gosx_scene3d.dispatchCommands = dispatchSceneCommands;
  var regionsAPI = {
    mount: scan,
    dispose: dispose,
    refresh: refresh,
    applySceneCommands: applySceneCommandScripts,
    bindings: regionBindings,
  };
  window.__gosx_declarative_regions = regionsAPI;
  window.__gosx = window.__gosx || {};
  window.__gosx.regions = Object.assign(window.__gosx.regions || {}, regionsAPI);

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () { scan(document); });
  } else {
    scan(document);
  }
})();
