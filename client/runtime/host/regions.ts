// @ts-check
// GoSX browser host: declarative server-fragment regions.
// 07-declarative-regions.js — bootstrap-owned declarative server-fragment regions.
//
// An htmx-lite region that re-fetches an HTML fragment and swaps it into place
// when a named shared signal changes, a hub event fires, or (gosx#217) a
// plain interval elapses — so apps express "live panel" behavior
// declaratively, with no bespoke fetch/innerHTML JS. Self-contained IIFE;
// resolves runtime globals lazily.
//
//   <div data-gosx-region
//        data-gosx-region-url="/api/.../selection/{value}"  (the {value} token,
//             if present, is filled from the signal's current value, URL-encoded;
//             an empty value suppresses the fetch)
//        data-gosx-region-signal="$some.signal"  (optional: refetch on change)
//        data-gosx-region-on="change"            (optional: hub event names —
//             space/comma separated — that also retrigger a refetch)
//        data-gosx-region-field="tree_html"      (optional: JSON field to inject;
//             absent → the raw response body is the HTML)
//        data-gosx-region-interval="20s">        (optional, gosx#217: also refetch on
//             this plain interval — the same whole-seconds/whole-minutes duration
//             subset data-gosx-revalidate-interval uses; composes with -signal and
//             -on rather than replacing them)
//     ...server-rendered initial fragment...
//   </div>
//
// The interval trigger alone skips a tick, and retries next tick, while the
// document is hidden, a navigation is in flight, or the region contains the
// document's focused element or an element under an active pointer — a
// signal or hub-event trigger answers its discrete, user-caused event
// immediately regardless, the same way it always has. Every trigger shares
// one fetch path (fetchRegion below), which sends `If-None-Match` once a
// response has carried an `ETag` and treats a 304 as "unchanged", and
// restores the region element's own scrollTop/scrollLeft across a swap —
// both apply no matter which trigger caused the refresh.
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
  gosxHost.state = gosxHost.state || {};
  if (typeof document === "undefined" || gosxHost.state.declarativeRegions) return;
  gosxHost.state.declarativeRegions = true;

  // applySceneCommandScripts finds every data-gosx-scene-commands payload
  // under `root` (a freshly-swapped region element, or `document` for the
  // initial-load pass) and applies each one.
  var sceneCommandsSelector = 'script[type="application/json"][data-gosx-scene-commands]';

  function hasSceneCommandScripts(root) {
    return root && typeof root.querySelectorAll === "function" && root.querySelectorAll(sceneCommandsSelector).length;
  }

  function applySceneCommandScripts(root) {
    var engines = window.__gosx && window.__gosx.engines;
    if (engines && engines.size && hasSceneCommandScripts(root) && typeof window.__gosx_scene3d_apply_command_scripts === "function") {
      window.__gosx_scene3d_apply_command_scripts(root);
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

  // Periodic region polling (data-gosx-region-interval, gosx#217): a region
  // can already refresh() on a signal change or a hub event — both discrete,
  // user-caused triggers this file answers immediately. -interval adds a
  // third trigger, a plain timer, composing with the other two rather than
  // replacing them: the same record.refresh() call, guarded here the way a
  // discrete trigger deliberately is not — a signal or hub-event refresh
  // must still land the instant the user causes it, even while the same
  // element retains focus (picking an option and having its own region
  // refresh immediately is the whole point), but an unattended timer tick
  // must never land under an active pointer or a focused control anywhere
  // inside the region, and retries next tick instead.
  //
  // REGION_POLL_MAX_INTERVAL_MS and REGION_POLL_INTERVAL_CLAMP_MS mirror
  // parseRevalidateInterval's own bounds in navigation.ts; this file shares
  // no functions with that one (each browser host file resolves runtime
  // globals independently), so the small duration grammar is duplicated
  // here rather than imported.
  var REGION_POLL_MAX_INTERVAL_MS = 2147483647;
  var REGION_POLL_INTERVAL_CLAMP_MS = 60 * 60 * 1000;

  function parseRegionPollInterval(value) {
    var trimmed = String(value == null ? "" : value).trim();
    var match = /^([0-9]+)(s|m)$/.exec(trimmed);
    if (!match) return null;
    var amount = Number(match[1]);
    if (!isFinite(amount) || amount <= 0) return null;
    var ms = match[2] === "m" ? amount * 60000 : amount * 1000;
    if (ms < 1000 || ms > REGION_POLL_MAX_INTERVAL_MS) return null;
    return ms;
  }

  // activeRegionPointerTarget tracks the element under a currently-held
  // pointer, document-wide, for regionPollBlocked below — one delegated
  // listener pair for every polled region, not one per region. Installed
  // lazily, at most once, the first time bindRegion actually binds a
  // data-gosx-region-interval element — never on a page that polls no
  // region at all, so a page without this feature adds no listener
  // overhead for it.
  var activeRegionPointerTarget = null;
  var regionPointerListenersInstalled = false;

  function ensureRegionPointerListeners() {
    if (regionPointerListenersInstalled) return;
    if (typeof document === "undefined" || typeof document.addEventListener !== "function") return;
    regionPointerListenersInstalled = true;
    document.addEventListener("pointerdown", function (event) {
      activeRegionPointerTarget = (event && event.target) || null;
    }, { passive: true });
    var clearActiveRegionPointerTarget = function () { activeRegionPointerTarget = null; };
    document.addEventListener("pointerup", clearActiveRegionPointerTarget, { passive: true });
    document.addEventListener("pointercancel", clearActiveRegionPointerTarget, { passive: true });
  }

  function regionDocumentHidden() {
    if (typeof document === "undefined") return false;
    if (typeof document.hidden === "boolean") return document.hidden;
    return String(document.visibilityState || "visible").toLowerCase() === "hidden";
  }

  // regionNavigationInFlight reads the navigation runtime's own public
  // getState() (window.__gosx.navigation), never its private state — this
  // file works standalone, with or without the navigation runtime present,
  // the same "resolves runtime globals lazily" contract this file's own
  // header comment documents. Its absence, or a getState() failure, simply
  // never blocks a poll on this condition.
  function regionNavigationInFlight() {
    var nav = window.__gosx && window.__gosx.navigation;
    if (!nav || typeof nav.getState !== "function") return false;
    try {
      var state = nav.getState();
      return !!state && state.phase === "pending";
    } catch (_e) {
      return false;
    }
  }

  function regionElementContains(root, node) {
    if (!root || !node) return false;
    if (root === node) return true;
    return !root.contains || root.contains(node);
  }

  // regionPollBlocked is the interval trigger's own interaction guard,
  // scoped to the one region: the document's focused element (ignoring the
  // default "nothing focused" state, where document.activeElement reads as
  // <body> and would otherwise match every region), or the element under an
  // active pointer, anywhere inside el.
  function regionPollBlocked(el) {
    if (regionDocumentHidden() || regionNavigationInFlight()) return true;
    var active = typeof document !== "undefined" ? document.activeElement : null;
    if (active && active !== document.body && regionElementContains(el, active)) return true;
    return regionElementContains(el, activeRegionPointerTarget);
  }

  // pollRegionTick is the timer's own callback: record.pollInFlight is a
  // skip-if-busy guard independent of fetchRegion's own AbortController
  // (which stays an abort-and-restart policy for a discrete signal or
  // hub-event trigger) — an unattended timer never overlaps its own
  // previous tick, and a slow response is worth waiting for rather than
  // restarting. A refresh() the guard above suppressed, or one skipped
  // because {value} is still empty (see bindRegion below), resolves false
  // and never blocks the next tick either way.
  function pollRegionTick(record) {
    if (record.disposed || record.pollInFlight || regionPollBlocked(record.element)) return;
    record.pollInFlight = true;
    var release = function () { record.pollInFlight = false; };
    var result = record.refresh();
    if (result && typeof result.then === "function") {
      result.then(release, release);
    } else {
      release();
    }
  }

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
    if (state === "pending") {
      el.setAttribute("aria-busy", "true");
    } else if (typeof el.removeAttribute === "function") {
      el.removeAttribute("aria-busy");
    }
    if (url) el.setAttribute("data-gosx-region-request", url);
    else if (typeof el.removeAttribute === "function") el.removeAttribute("data-gosx-region-request");
  }

  function regionHTTPStatus(response) {
    var status = Number(response && response.status);
    if (!Number.isFinite(status) || status < 100 || status > 599) return 0;
    return Math.floor(status);
  }

  // Region error events are an application observation surface, not a
  // response-inspection escape hatch. Never attach a Response, body, Error,
  // headers, or status text; status is the only transport-derived field.
  function emitRegionError(el, url, status) {
    emit("gosx:region:error", {
      element: el,
      url: url,
      status: status,
    });
  }

  function regionRequestController(record) {
    if (record.requestController && typeof record.requestController.abort === "function") {
      record.requestController.abort();
    }
    record.requestController = typeof AbortController === "function" ? new AbortController() : null;
    return record.requestController;
  }

  // fetchRegion is shared by every trigger kind — signal, hub event, manual
  // refresh(), and the periodic poll bindRegion below starts for
  // data-gosx-region-interval (gosx#217). If-None-Match/ETag support lives
  // here rather than only on the poll path: it is backward compatible by
  // construction (record.etag starts empty, so the header is never sent
  // until a response has actually carried one; a server that never sends
  // ETag behaves exactly as before) and benefits a signal- or
  // hub-event-triggered region just as much as a polled one.
  function fetchRegion(record, url) {
    var el = record.element;
    var controller = null;
    if (!record.transport) record.transport = createRegionTransport();
    var requestHeaders = { Accept: record.field ? "application/json" : "text/html" };
    if (record.etag) requestHeaders["If-None-Match"] = record.etag;
    var request = { headers: requestHeaders };
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
        if (response && response.status === 304) return { notModified: true };
        if (!response || !response.ok) {
          return { httpError: true, status: regionHTTPStatus(response) };
        }
        var nextEtag = response.headers && typeof response.headers.get === "function"
          ? (response.headers.get("ETag") || "")
          : "";
        if (nextEtag) record.etag = nextEtag;
        return (record.field ? response.json() : response.text()).then(function (data) {
          return { data: data };
        });
      })
      .then(function (result) {
        if (record.disposed || (controller && controller.signal.aborted) || result == null) return;
        if (result.httpError) {
          setRegionState(el, "error", "");
          emitRegionError(el, url, result.status);
          observeRegion("error", "region refresh rejected", { url: url, status: result.status });
          return;
        }
        if (result.notModified) {
          setRegionState(el, "ready", "");
          observeRegion("debug", "region refresh not modified", { url: url });
          return;
        }
        var data = result.data;
        var html = record.field ? (data && data[record.field]) : data;
        if (typeof html !== "string") return;
        // Preserve the region's own scroll position across the swap when the
        // region element is itself a scrolling container (an internal
        // overflow list, for example a signal-wire feed) — restored after
        // the new content mounts below. A harmless no-op for a region that
        // does not scroll on its own axis.
        var scrollTop = typeof el.scrollTop === "number" ? el.scrollTop : null;
        var scrollLeft = typeof el.scrollLeft === "number" ? el.scrollLeft : null;
        // gosxHost.dom.replace is a forwarding shim compatibility.ts installs
        // unconditionally, so it is always a function; probe the ambient
        // name it forwards to instead of the shim to tell "dom.ts never
        // loaded" (run the unmanaged fallback below, as before) apart from
        // "dom.ts loaded and replaceRuntimeContent failed" (see dom.ts:
        // on failure it already disposed the old subtree and may have
        // partially written the new one, so re-running the unmanaged
        // dispose/innerHTML/mount fallback would double-apply on top of
        // that). The latter throws instead, so the catch below reports the
        // failure and sets the region to the error state, matching the
        // pre-migration behavior.
        if (typeof gosxHostCompatibility.read("__gosx_replace_runtime_content") === "function") {
          if (gosxHost.dom.replace(el, html) === false) {
            throw new Error("runtime content replacement failed");
          }
        } else {
          if (gosxHost.surfaces && typeof gosxHost.surfaces.dispose === "function") {
            gosxHost.surfaces.dispose(el);
          }
          el.innerHTML = html;
          applySceneCommandScripts(el);
          if (gosxHost.surfaces && typeof gosxHost.surfaces.mount === "function") {
            gosxHost.surfaces.mount(el);
          }
        }
        if (scrollTop != null) el.scrollTop = scrollTop;
        if (scrollLeft != null) el.scrollLeft = scrollLeft;
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
        emitRegionError(el, url, 0);
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
    var rawInterval = el.getAttribute("data-gosx-region-interval");
    var pollIntervalMs = null;
    if (rawInterval) {
      pollIntervalMs = parseRegionPollInterval(rawInterval);
      if (pollIntervalMs == null) {
        console.warn(
          "[gosx] invalid data-gosx-region-interval value " + JSON.stringify(String(rawInterval))
          + "; periodic region refresh is disabled for this element"
        );
      } else if (pollIntervalMs > REGION_POLL_INTERVAL_CLAMP_MS) {
        console.warn(
          "[gosx] data-gosx-region-interval value " + JSON.stringify(String(rawInterval))
          + " exceeds the 1 hour maximum for periodic region refresh; clamping to 1 hour"
        );
        pollIntervalMs = REGION_POLL_INTERVAL_CLAMP_MS;
      }
    }
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
      etag: "",
      pollInFlight: false,
      pollTimerHandle: null,
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
    // The interval trigger fires an immediate first tick at bind time
    // (subject to the same guard as every later one), rather than waiting
    // out a full interval — the tick's own action is a bounded fragment
    // swap, not a decision about whether a heavier operation is worth
    // doing, so there is no reason to leave freshly-mounted content stale
    // for up to one whole interval.
    if (pollIntervalMs != null) {
      ensureRegionPointerListeners();
      pollRegionTick(record);
      record.pollTimerHandle = setInterval(function () { pollRegionTick(record); }, pollIntervalMs);
    }
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
      if (record.pollTimerHandle != null) clearInterval(record.pollTimerHandle);
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

  var regionsAPI = {
    mount: scan,
    dispose: dispose,
    refresh: refresh,
    applySceneCommands: applySceneCommandScripts,
    bindings: regionBindings,
  };
  gosxHost.regions = regionsAPI;
  gosxHostCompatibility.install("__gosx_mount_declarative_regions", scan);
  gosxHostCompatibility.install("__gosx_dispose_declarative_regions", dispose);
  gosxHostCompatibility.install("__gosx_apply_scene_command_scripts", applySceneCommandScripts);
  gosxHostCompatibility.install("__gosx_declarative_regions", regionsAPI);
  window.__gosx.regions = Object.assign(window.__gosx.regions || {}, regionsAPI);

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () { scan(document); });
  } else {
    scan(document);
  }
})();
