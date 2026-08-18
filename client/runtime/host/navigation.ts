// @ts-check
// GoSX browser host: soft-navigation lifecycle.
(function() {
  "use strict";

  if (gosxHost.navigation && typeof gosxHost.navigation.navigate === "function") {
    return;
  }

  const HEAD_START = "gosx-head-start";
  const HEAD_END = "gosx-head-end";
  const SCRIPT_ROLE = "data-gosx-script";
  const LINK_ATTR = "data-gosx-link";
  const LINK_STATE_ATTR = "data-gosx-link-state";
  const LINK_CURRENT_ATTR = "data-gosx-link-current";
  const LINK_CURRENT_POLICY_ATTR = "data-gosx-link-current-policy";
  const LINK_PREFETCH_STATE_ATTR = "data-gosx-prefetch-state";
  const LINK_MANAGED_CURRENT_ATTR = "data-gosx-aria-current-managed";
  const FORM_ATTR = "data-gosx-form";
  // FORM_MANAGED_SHORTHAND_ATTR is the .gsx template shorthand for the full
  // managed-form contract (gosx#179). The server expands it into FORM_ATTR
  // and the rest of the contract at render time when it can — see
  // gosx.ManagedFormShorthandTruthy and route.fileManagedFormShorthandTruthy
  // in the Go source — but a form built by client-side JS (an island's
  // re-render, or hand-authored markup that bypasses gosx rendering
  // entirely) never passes through that expansion. The runtime accepts the
  // shorthand directly at the matching level so those forms are still
  // discovered and intercepted.
  const FORM_MANAGED_SHORTHAND_ATTR = "data-gosx-managed";
  const FORM_MODE_ATTR = "data-gosx-form-mode";
  const FORM_STATE_ATTR = "data-gosx-form-state";
  const FORM_PENDING_ATTR = "data-gosx-pending";
  const FORM_PROJECT_ATTR = "data-gosx-form-project";
  const FORM_ERROR_DESCRIPTION_ATTR = "data-gosx-form-error-describedby";
  const PREFETCH_ATTR = "data-gosx-prefetch";
  const NAVIGATION_BEACON_ATTR = "data-gosx-navigation-beacon";
  const NAV_STATE_ATTR = "data-gosx-navigation-state";
  const NAV_CURRENT_PATH_ATTR = "data-gosx-navigation-current-path";
  const NAV_PENDING_URL_ATTR = "data-gosx-navigation-pending-url";
  const REVALIDATE_INTERVAL_ATTR = "data-gosx-revalidate-interval";
  const REVALIDATE_SRC_ATTR = "data-gosx-revalidate-src";
  const COUNTDOWN_ATTR = "data-gosx-countdown";
  const COUNTDOWN_FORMAT_ATTR = "data-gosx-countdown-format";
  const COUNTDOWN_SEGMENT_ATTR = "data-gosx-countdown-segment";
  // COUNTDOWN_WARN_ATTR and COUNTDOWN_CUE_ATTR (gosx#213) share one
  // grammar: a comma-separated list of threshold:token pairs. -warn's
  // token is a CSS class the runtime toggles on the countdown root at or
  // below the threshold; -cue's token is a name from the fixed synthesized
  // tone vocabulary (see "Shared synthesized audio cues" below), fired
  // once the first time the remainder crosses at or below the threshold.
  // Before gosx#213 COUNTDOWN_WARN_ATTR took one bare duration and always
  // toggled a single fixed class (gosx-countdown--warn); that single-value
  // form is no longer accepted; see the CHANGELOG entry for gosx#213.
  const COUNTDOWN_WARN_ATTR = "data-gosx-countdown-warn";
  const COUNTDOWN_CUE_ATTR = "data-gosx-countdown-cue";
  const COUNTDOWN_THEN_ATTR = "data-gosx-countdown-then";
  const COUNTDOWN_SEGMENT_NAMES = ["days", "hours", "minutes", "seconds"];
  const COUNTDOWN_TICK_MS = 1000;
  // data-gosx-watch (gosx#214): see "Attention watcher" below for the full
  // grammar and lifecycle.
  const WATCH_ATTR = "data-gosx-watch";
  const WATCH_EFFECT_ATTR = "data-gosx-watch-effect";
  const WATCH_TITLE_ATTR = "data-gosx-watch-title";
  const WATCH_TITLE_FLASH_INTERVAL_MS = 1000;
  // Declarative reorder (data-gosx-reorder, gosx#212). See the "Declarative
  // reorder" section below for the full contract; these are the attribute,
  // class, and field-name constants it reads and writes.
  const REORDER_CONTAINER_ATTR = "data-gosx-reorder";
  const REORDER_ACTION_ATTR = "data-gosx-reorder-action";
  const REORDER_ITEM_ATTR = "data-gosx-reorder-item";
  const REORDER_HANDLE_ATTR = "data-gosx-reorder-handle";
  const REORDER_HANDLE_READY_ATTR = "data-gosx-reorder-handle-ready";
  const REORDER_ITEM_FIELD_ATTR = "data-gosx-reorder-item-field";
  const REORDER_INDEX_FIELD_ATTR = "data-gosx-reorder-index-field";
  const REORDER_PLACEHOLDER_ATTR = "data-gosx-reorder-placeholder";
  const REORDER_DEFAULT_ITEM_FIELD = "item_id";
  const REORDER_DEFAULT_INDEX_FIELD = "index";
  const REORDER_DRAGGING_CLASS = "gosx-reorder--dragging";
  const REORDER_LIFTED_CLASS = "gosx-reorder-item--lifted";
  const REORDER_PLACEHOLDER_CLASS = "gosx-reorder-item--placeholder";
  const REORDER_GRABBED_CLASS = "gosx-reorder-item--grabbed";
  // The auto-scroll edge zone, in CSS pixels measured inward from each end of
  // the container's own border box, and the fastest scroll speed a pointer
  // pinned at the very edge of that zone reaches (pixels per tick — see
  // REORDER_AUTOSCROLL_TICK_MS below).
  const REORDER_AUTOSCROLL_EDGE_PX = 48;
  const REORDER_AUTOSCROLL_MAX_PX = 18;
  const REORDER_AUTOSCROLL_TICK_MS = 16;
  // Live-bound text regions (data-gosx-live-*, gosx#217). See "Live-bound
  // regions" below for the full contract; these are the attribute
  // constants it reads.
  const LIVE_SRC_ATTR = "data-gosx-live-src";
  const LIVE_INTERVAL_ATTR = "data-gosx-live-interval";
  const LIVE_BIND_ATTR = "data-gosx-live-bind";
  const LIVE_FLASH_CLASS_ATTR = "data-gosx-live-flash-class";
  // Declarative list filter (data-gosx-filter, gosx#215). See "Declarative
  // list filter" below for the full contract; these are the attribute,
  // class-hook, and timing constants it reads and writes.
  const FILTER_ATTR = "data-gosx-filter";
  const FILTER_TEXT_ATTR = "data-gosx-filter-text";
  const FILTER_ANNOUNCE_ATTR = "data-gosx-filter-announce";
  const FILTER_HIDDEN_CLASS = "gosx-filter-row--hidden";
  const FILTER_DEBOUNCE_MS = 150;
  // Visibility-aware heartbeat ping (data-gosx-heartbeat, gosx#216). See
  // "Visibility-aware heartbeat" below for the full contract.
  const HEARTBEAT_ATTR = "data-gosx-heartbeat";
  const HEARTBEAT_INTERVAL_ATTR = "data-gosx-heartbeat-interval";
  const MAIN_ATTR = "data-gosx-main";
  const ANNOUNCE_ATTR = "data-gosx-announce";
  const ANNOUNCER_ATTR = "data-gosx-announcer";
  const MANAGED_FOCUS_ATTR = "data-gosx-focus-managed";
  const NAV_INLINE_REPLAY_ATTR = "data-gosx-navigation-replay";
  const NAV_INLINE_REPLAYED_ATTR = "data-gosx-navigation-replayed";
  const URL_ATTRS = ["href", "src", "action", "poster"];
  const SUBMITTER_ATTRS = {
    formAction: "formaction",
    formMethod: "formmethod",
    formTarget: "formtarget",
  };
  // A prefetched page never revalidates on its own, so a stale entry can
  // outlive the session that time-boxed its content (rotating tokens,
  // bucketed data). PAGE_CACHE_TTL_MS bounds how long a cached page answers
  // before the next lookup treats it as a miss and refetches.
  const PAGE_CACHE_TTL_MS = 5 * 60 * 1000;
  const PAGE_CACHE_OPT_OUT_META = "gosx-page-cache";
  const PAGE_CACHE_OPT_OUT_VALUE = "no-store";
  const scriptCache = gosxHost.navigationScriptCache || new Map();
  const pageCache = gosxHost.navigationPageCache || new Map();
  let navigationState = {
    phase: "idle",
    currentURL: String(window.location && window.location.href || ""),
    pendingURL: "",
  };
  let navigationSequence = 0;
  let navigationFetchStarted = 0;
  let navigationFetchApplied = 0;
  let activeNavigationController = null;
  let activeNavigationURL = "";
  let announceSeq = 0;
  let formErrorSeq = 0;
  let navigationFrameSequence = 0;
  // A Set (not a WeakSet) so its size doubles as the "a managed form
  // submission is in flight" signal periodic revalidation reads — see
  // navigationOrFormSubmissionInFlight below. submitForm's try/finally keeps
  // every entry reliably removed once its submission settles.
  const pendingManagedForms = new Set();
  const sentNavigationBeacons = new Set();
  let revalidateTimerHandle = null;
  let revalidateSrc = "";
  let revalidateHasBaseline = false;
  let revalidateLastBody = null;
  // revalidateGeneration is bumped every time setupPageRevalidation runs
  // (page boot and every soft navigation). pollRevalidateSrc captures it
  // before its fetch and re-checks it after: a poll that started on the
  // page this counter belonged to, but settles after navigation moved on,
  // discards its response instead of writing a baseline (or triggering a
  // revalidate) for whatever page is current now.
  let revalidateGeneration = 0;
  let revalidateIntervalMs = 0;
  // Set the instant the document goes hidden, cleared on the next visible
  // transition. Backs the visibilitychange catch-up tick below.
  let revalidateHiddenSince = null;
  // True while pollRevalidateSrc's own fetch/text() awaits are unresolved.
  // Guards against an interval tick or visibility catch-up starting a
  // second overlapping poll before the first one has settled.
  let revalidatePollInFlight = false;
  // countdownRoots holds one state record per data-gosx-countdown element
  // discovered by the current generation's setupPageCountdowns() call; a
  // single shared setInterval (countdownTimerHandle) ticks all of them
  // together every second. countdownGeneration is bumped every time
  // setupPageCountdowns runs (page boot and every soft navigation) — see
  // its own doc comment and revalidateGeneration above. It discards a
  // countdown-triggered revalidation's failure report once navigation has
  // moved past the generation that started it (see triggerCountdownThen);
  // the then="revalidate" trigger's own re-fire guard lives in
  // countdownFiredTargets below instead, deliberately OUTSIDE this
  // per-generation counter — see its doc comment for why.
  let countdownRoots = [];
  let countdownTimerHandle = null;
  let countdownGeneration = 0;
  // countdownFiredTargets and countdownThenLastFiredAt back
  // then="revalidate" (gosx#178 review finding B1): see triggerCountdownThen
  // and updateCountdownState's doc comments for the full design. Both are
  // module-level and deliberately never reset by setupPageCountdowns or
  // countdownGeneration — their whole job is to survive the very rescan a
  // countdown's own trigger causes.
  const countdownFiredTargets = new Set();
  let countdownThenLastFiredAt = -Infinity;
  // countdownCueFiredKeys backs data-gosx-countdown-cue (gosx#213): a
  // one-shot-per-crossing Set exactly like countdownFiredTargets above,
  // keyed by "<targetMs>|<thresholdSeconds>|<cueName>" instead of just
  // targetMs, since one countdown can carry several independently-firing
  // cue tiers. A fixed targetMs's remainder only ever counts down, never
  // back up, so once a tier's key is in this Set it can never legitimately
  // need to fire again FOR THAT INSTANT — a rescan that rebuilds a fresh
  // state record for the same still-elapsed instant (see
  // countdownFiredTargets' own doc comment for why that happens) must not
  // replay it. A genuinely new instant (the countdown resets to a later
  // target) mints an entirely new key, which is what "re-arms when the
  // countdown resets" means here. Deliberately never reset by
  // countdownGeneration, for the same reason countdownFiredTargets never
  // is.
  const countdownCueFiredKeys = new Set();
  // watchRoots/watchGeneration mirror countdownRoots/countdownGeneration:
  // one record per data-gosx-watch element, rebuilt by setupPageWatchers on
  // every boot and soft navigation. See setupPageWatchers' own doc comment.
  let watchRoots = [];
  let watchGeneration = 0;
  // watchActiveState survives across watchGeneration the same way
  // countdownCueFiredKeys survives across countdownGeneration: a watcher's
  // DOM node is destroyed and rebuilt fresh on every revalidation swap (see
  // replaceBody), so "was this condition already true" cannot live on the
  // node itself. Keyed by each record's own key (its id, or its position
  // among data-gosx-watch elements when it has none — see buildWatchState).
  const watchActiveState = new Map();
  // titleFlashHandle/titleFlashOriginalTitle/titleFlashOwnerKey back
  // data-gosx-watch-effect's "title" effect (gosx#214). document.title is
  // one shared global resource, so only one watcher flashes it at a time;
  // a later transition takes ownership from an earlier still-flashing one
  // rather than stacking. See startTitleFlash/stopTitleFlash below.
  let titleFlashHandle = null;
  let titleFlashOriginalTitle = null;
  let titleFlashOwnerKey = null;
  // filterRoots/filterGeneration mirror watchRoots/watchGeneration: one
  // record per data-gosx-filter input, rebuilt by setupPageFilters on every
  // boot and soft navigation — see its own doc comment below.
  let filterRoots = [];
  let filterGeneration = 0;
  // filterQueryState survives across filterGeneration the same way
  // watchActiveState survives across watchGeneration above: a revalidation
  // swap destroys and rebuilds the filter input's own DOM node wholesale
  // (replaceBody clones a fresh document's body contents — see replaceBody
  // below), so the text a visitor already typed cannot live on the node
  // itself. Keyed by each record's own key (its id, or its position among
  // data-gosx-filter elements when it has none — see buildFilterState).
  const filterQueryState = new Map();
  // filterHoverRow is the one data-gosx-filter-text row the pointer
  // currently sits over, kept live by the delegated onFilterPointerOver /
  // onFilterPointerOut listeners below — see rowIsFilterGuarded's own doc
  // comment for why. null when the pointer is over no row at all.
  let filterHoverRow = null;
  // heartbeatTimerHandle/heartbeatSrc/heartbeatIntervalMs/heartbeatHiddenSince
  // mirror the periodic-revalidation state above almost exactly (gosx#216):
  // a same-origin poll on an interval, paused while the document is hidden,
  // with one catch-up tick on visibility return if a full interval elapsed
  // while hidden. See setupPageHeartbeat's own doc comment for what
  // differs from periodic revalidation.
  let heartbeatTimerHandle = null;
  let heartbeatSrc = "";
  let heartbeatIntervalMs = 0;
  let heartbeatHiddenSince = null;
  // True while a heartbeat GET's fetch await is unresolved — guards against
  // an interval tick or a visibility catch-up starting a second overlapping
  // ping, exactly like revalidatePollInFlight above.
  let heartbeatPingInFlight = false;
  // Bumped by every setupPageHeartbeat call, the same way revalidateGeneration
  // is bumped by setupPageRevalidation: a ping's fetch that settles after
  // navigation has moved on to a page with a different (or no) heartbeat
  // must not clear heartbeatPingInFlight for the generation current now.
  let heartbeatGeneration = 0;
  gosxHost.navigationScriptCache = scriptCache;
  gosxHost.navigationPageCache = pageCache;
  gosxHostCompatibility.install("__gosx_loaded_scripts", scriptCache);
  gosxHostCompatibility.install("__gosx_page_cache", pageCache);

  function gosxRuntimeRequest(input, init) {
    if (window.__gosx && typeof window.__gosx.request === "function") {
      return window.__gosx.request(input, init);
    }
    return fetch(input, init);
  }

  function gosxRuntimeFrame(callback) {
    const scheduler = window.__gosx && window.__gosx.scheduler;
    if (scheduler && typeof scheduler.frame === "function") {
      navigationFrameSequence += 1;
      return scheduler.frame("navigation:" + navigationFrameSequence, callback);
    }
    if (typeof requestAnimationFrame === "function") {
      return requestAnimationFrame(callback);
    }
    return setTimeout(callback, 16);
  }

  function toArray(listLike) {
    return Array.prototype.slice.call(listLike || []);
  }

  function isElement(node, tagName) {
    return !!node && node.nodeType === 1 && String(node.tagName || "").toUpperCase() === tagName;
  }

  function isMarker(node, name) {
    return isElement(node, "META") && node.getAttribute("name") === name;
  }

  function childIndex(parent, child) {
    const children = toArray(parent && parent.childNodes);
    return children.indexOf(child);
  }

  function windowLocationHref() {
    return String(window.location && window.location.href || "");
  }

  function scriptNonceValue(script) {
    if (!script) return "";
    return String(script.nonce || script.getAttribute && script.getAttribute("nonce") || "");
  }

  function currentDocumentNonce() {
    const current = scriptNonceValue(document.currentScript);
    if (current) return current;
    const selectors = [
      "script[nonce][data-gosx-navigation]",
      "script[nonce][data-gosx-document-contract]",
      "script[nonce][data-gosx-script]",
      "script[nonce]",
    ];
    for (const selector of selectors) {
      const found = document.querySelector(selector);
      const nonce = scriptNonceValue(found);
      if (nonce) return nonce;
    }
    return "";
  }

  function applyCurrentNonce(script) {
    const nonce = currentDocumentNonce();
    if (nonce && script) {
      script.nonce = nonce;
    }
  }

  function keepsLiteralURL(value) {
    return !value || value[0] === "#" || value.startsWith("data:") || value.startsWith("javascript:");
  }

  function absolutizeURL(value, baseURL) {
    if (!value) return value;
    const trimmed = String(value).trim();
    if (!trimmed || keepsLiteralURL(trimmed)) {
      return value;
    }
    try {
      return new URL(trimmed, baseURL || windowLocationHref()).toString();
    } catch (_) {
      return value;
    }
  }

  function absolutizeSrcset(value, baseURL) {
    if (!value) return value;
    return String(value).split(",").map(function(candidate) {
      const trimmed = candidate.trim();
      if (!trimmed) return trimmed;

      const parts = trimmed.split(/\s+/);
      if (parts.length === 0) return trimmed;
      parts[0] = absolutizeURL(parts[0], baseURL);
      return parts.join(" ");
    }).join(", ");
  }

  function normalizeNodeURLs(node, baseURL) {
    if (!node || node.nodeType !== 1) {
      return;
    }

    for (const attr of URL_ATTRS) {
      if (node.hasAttribute && node.hasAttribute(attr)) {
        node.setAttribute(attr, absolutizeURL(node.getAttribute(attr), baseURL));
      }
    }
    if (node.hasAttribute && node.hasAttribute("srcset")) {
      node.setAttribute("srcset", absolutizeSrcset(node.getAttribute("srcset"), baseURL));
    }

    if (!node.childNodes) return;
    for (const child of toArray(node.childNodes)) {
      normalizeNodeURLs(child, baseURL);
    }
  }

  function cloneIntoDocument(node, baseURL) {
    if (node && typeof node.cloneNode === "function") {
      const clone = node.cloneNode(true);
      normalizeNodeURLs(clone, baseURL);
      return clone;
    }
    return node;
  }

  function findHeadMarkers(head) {
    const children = toArray(head && head.childNodes);
    let start = null;
    let end = null;
    for (const child of children) {
      if (isMarker(child, HEAD_START)) start = child;
      if (isMarker(child, HEAD_END)) end = child;
    }
    return { start, end };
  }

  function walkElements(root, visit) {
    const stack = [];
    if (root) {
      stack.push(root);
    }

    while (stack.length > 0) {
      const node = stack.pop();
      if (!node || node.nodeType !== 1) {
        continue;
      }
      if (visit(node) === false) {
        return;
      }
      const children = toArray(node.childNodes);
      for (let i = children.length - 1; i >= 0; i--) {
        stack.push(children[i]);
      }
    }
  }

  function ensureHeadMarkers() {
    const head = document.head;
    let markers = findHeadMarkers(head);
    if (markers.start && markers.end) return markers;

    const start = document.createElement("meta");
    start.setAttribute("name", HEAD_START);
    start.setAttribute("content", "");
    const end = document.createElement("meta");
    end.setAttribute("name", HEAD_END);
    end.setAttribute("content", "");
    head.appendChild(start);
    head.appendChild(end);
    return { start, end };
  }

  function collectManagedHeadNodes(head) {
    const markers = findHeadMarkers(head);
    if (!markers.start || !markers.end) return [];

    const children = toArray(head.childNodes);
    const startIdx = children.indexOf(markers.start);
    const endIdx = children.indexOf(markers.end);
    if (startIdx < 0 || endIdx < 0 || endIdx <= startIdx) return [];
    return children.slice(startIdx + 1, endIdx);
  }

  function serializeNodeSignature(node) {
    if (!node) return "";
    if (node.nodeType !== 1) {
      return String(node.nodeType) + ":" + String(node.textContent || "");
    }

    const tagName = String(node.tagName || node.nodeName || "").toLowerCase();
    const attrs = attributeEntries(node)
      .map(function(entry) {
        return [String(entry.name), String(entry.value)];
      })
      .sort(function(a, b) {
        if (a[0] === b[0]) {
          return a[1] < b[1] ? -1 : a[1] > b[1] ? 1 : 0;
        }
        return a[0] < b[0] ? -1 : 1;
      })
      .map(function(entry) {
        return entry[0] + "=" + JSON.stringify(entry[1]);
      })
      .join(" ");

    let content = "";
    for (const child of toArray(node.childNodes)) {
      content += serializeNodeSignature(child);
    }
    if (!content) {
      content = String(node.textContent || "");
    }

    return "<" + tagName + (attrs ? " " + attrs : "") + ">" + content + "</" + tagName + ">";
  }

  function headNodeSignature(node, baseURL) {
    if (!node) return "";
    if (node.nodeType !== 1) {
      return String(node.nodeType) + ":" + String(node.textContent || "");
    }
    const clone = cloneIntoDocument(node, baseURL);
    if (clone && typeof clone.outerHTML === "string") {
      return clone.outerHTML;
    }
    return serializeNodeSignature(clone || node);
  }

  function isStylesheetLink(node) {
    return isElement(node, "LINK")
      && /\bstylesheet\b/i.test(String(node.getAttribute("rel") || ""))
      && !!node.getAttribute("href");
  }

  function isDOMLoadedManagedScript(node) {
    return isElement(node, "SCRIPT")
      && node.hasAttribute(SCRIPT_ROLE)
      && !!node.getAttribute("src")
      && node.getAttribute("data-gosx-script-load") === "dom";
  }

  function waitForStylesheet(node) {
    if (!isStylesheetLink(node)) {
      return Promise.resolve();
    }
    if (node.sheet) {
      return Promise.resolve();
    }

    return new Promise(function(resolve, reject) {
      let settled = false;
      const cleanup = function() {
        if (settled) return;
        settled = true;
        node.removeEventListener("load", onLoad);
        node.removeEventListener("error", onError);
      };
      const onLoad = function() {
        cleanup();
        resolve();
      };
      const onError = function() {
        cleanup();
        reject(new Error("stylesheet failed to load: " + (node.getAttribute("href") || "")));
      };

      node.addEventListener("load", onLoad);
      node.addEventListener("error", onError);

      const finalizeIfReady = function() {
        if (settled || !node.sheet) return;
        cleanup();
        resolve();
      };

      gosxRuntimeFrame(finalizeIfReady);
    });
  }

  async function replaceManagedHead(nextDoc, baseURL) {
    document.title = nextDoc.title || "";

    const currentMarkers = ensureHeadMarkers();
    const head = document.head;
    const currentNodes = collectManagedHeadNodes(head);
    const currentBuckets = new Map();
    for (const node of currentNodes) {
      const signature = headNodeSignature(node, window.location.href);
      if (!currentBuckets.has(signature)) {
        currentBuckets.set(signature, []);
      }
      currentBuckets.get(signature).push(node);
    }

    const nextNodes = collectManagedHeadNodes(nextDoc.head);
    const orderedNodes = [];
    const insertedNodes = [];
    for (const node of nextNodes) {
      const signature = headNodeSignature(node, baseURL);
      const bucket = currentBuckets.get(signature);
      if (bucket && bucket.length > 0) {
        orderedNodes.push(bucket.shift());
        continue;
      }

      // Scripts parsed through DOMParser are inert. DOM-owned managed scripts
      // must be created by loadManagedScriptTag so CSP and browser execution
      // semantics apply; do not leave an inert clone that looks preloaded.
      if (isDOMLoadedManagedScript(node)) {
        continue;
      }

      const clone = cloneIntoDocument(node, baseURL);
      if (isElement(clone, "SCRIPT") && clone.hasAttribute(SCRIPT_ROLE) && clone.getAttribute("src")) {
        clone.setAttribute("data-gosx-script-loaded", "pending");
      }
      head.insertBefore(clone, currentMarkers.end);
      orderedNodes.push(clone);
      insertedNodes.push(clone);
    }

    await Promise.all(insertedNodes.map(waitForStylesheet));

    const retained = new Set(orderedNodes);
    for (const node of currentNodes) {
      if (!retained.has(node) && node.parentNode === head) {
        head.removeChild(node);
      }
    }

    for (const node of orderedNodes) {
      if (node.parentNode === head) {
        head.insertBefore(node, currentMarkers.end);
      }
    }
  }

  function attributeEntries(element) {
    if (!element || !element.attributes) return [];
    if (typeof element.attributes.entries === "function") {
      return Array.from(element.attributes.entries()).map(([name, value]) => ({ name, value }));
    }
    return Array.from(element.attributes).map((attr) => ({ name: attr.name, value: attr.value }));
  }

  function findElement(root, predicate) {
    let found = null;
    walkElements(root, function(node) {
      if (!predicate(node)) {
        return true;
      }
      found = node;
      return false;
    });
    return found;
  }

  function normalizeTextValue(value) {
    return String(value || "").replace(/\s+/g, " ").trim();
  }

  function setOptionalAttr(node, name, value) {
    if (!node || !node.setAttribute || !name) {
      return;
    }
    if (value == null || value === "") {
      if (typeof node.removeAttribute === "function") {
        node.removeAttribute(name);
      }
      return;
    }
    node.setAttribute(name, String(value));
  }

  function parsedNavigationURL(value) {
    if (!value) return null;
    try {
      return new URL(value, windowLocationHref());
    } catch (_error) {
      return null;
    }
  }

  function normalizedNavigationPath(pathname) {
    let path = String(pathname || "/");
    if (!path.startsWith("/")) {
      path = "/" + path;
    }
    if (path.length > 1) {
      path = path.replace(/\/+$/, "");
    }
    return path || "/";
  }

  function navigationURLParts(value) {
    const parsed = parsedNavigationURL(value);
    if (!parsed) {
      return null;
    }
    return {
      protocol: parsed.protocol,
      origin: parsed.origin,
      path: normalizedNavigationPath(parsed.pathname),
      search: String(parsed.search || ""),
      href: parsed.href,
    };
  }

  function isHTTPNavigationURL(url) {
    return !!url && (url.protocol === "http:" || url.protocol === "https:");
  }

  function sameNavigationURL(left, right) {
    return !!left && !!right && left.origin === right.origin && left.path === right.path && left.search === right.search;
  }

  function navigationUUID() {
    const cryptoRef = window.crypto || null;
    if (cryptoRef && typeof cryptoRef.randomUUID === "function") {
      return cryptoRef.randomUUID();
    }
    const bytes = new Uint8Array(16);
    if (cryptoRef && typeof cryptoRef.getRandomValues === "function") {
      cryptoRef.getRandomValues(bytes);
    } else {
      for (let i = 0; i < bytes.length; i += 1) {
        bytes[i] = Math.floor(Math.random() * 256);
      }
    }
    bytes[6] = (bytes[6] & 0x0f) | 0x40;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;
    const hex = Array.prototype.map.call(bytes, function(byte) {
      return byte.toString(16).padStart(2, "0");
    }).join("");
    return [
      hex.slice(0, 8),
      hex.slice(8, 12),
      hex.slice(12, 16),
      hex.slice(16, 20),
      hex.slice(20),
    ].join("-");
  }

  function navigationBeaconConfigs() {
    return collectElements(document.head, function(node) {
      return isElement(node, "SCRIPT")
        && node.hasAttribute
        && node.hasAttribute(NAVIGATION_BEACON_ATTR);
    }).map(function(node) {
      try {
        const config = JSON.parse(String(node.textContent || ""));
        return config && typeof config === "object" ? config : null;
      } catch (error) {
        reportNavigationFailure("navigation beacon config", error, {
          source: NAVIGATION_BEACON_ATTR,
        });
        return null;
      }
    }).filter(Boolean);
  }

  function sendNavigationBeacons(url) {
    const parsed = parsedNavigationURL(url);
    if (!parsed) return;
    const path = String(parsed.pathname || "/") + String(parsed.search || "");
    for (const config of navigationBeaconConfigs()) {
      const endpoint = String(config.url || "").trim();
      if (!endpoint) continue;
      const endpointURL = parsedNavigationURL(endpoint);
      if (!endpointURL || endpointURL.origin !== parsed.origin) {
        observeNavigation("warning", "navigation beacon rejected", {
          name: String(config.name || ""),
          reason: "cross-origin-endpoint",
        });
        continue;
      }
      const key = [String(config.name || ""), endpointURL.href, path].join("\n");
      if (sentNavigationBeacons.has(key)) continue;
      sentNavigationBeacons.add(key);

      const navigationID = navigationUUID();
      const payload = {};
      payload[String(config.pathField || "path")] = path;
      payload[String(config.navigationIDField || "navigation_id")] = navigationID;
      gosxRuntimeRequest(endpoint, {
        method: String(config.method || "POST").toUpperCase(),
        credentials: String(config.credentials || "same-origin"),
        keepalive: config.keepalive !== false,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      }).then(function(response) {
        if (!response || !response.ok) {
          throw new Error("navigation beacon failed with status " + String(response && response.status || 0));
        }
        observeNavigation("debug", "navigation beacon sent", {
          name: String(config.name || ""),
          path: path,
          navigationID: navigationID,
        });
      }).catch(function(error) {
        reportNavigationFailure("navigation beacon", error, {
          source: endpoint,
          telemetry: {
            name: String(config.name || ""),
            path: path,
            navigationID: navigationID,
          },
        });
      });
    }
  }

  function ancestorNavigationURL(parent, child) {
    if (!parent || !child || parent.origin !== child.origin) {
      return false;
    }
    if (parent.path === "/" || parent.search) {
      return false;
    }
    return child.path === parent.path || child.path.startsWith(parent.path + "/");
  }

  function collectElements(root, predicate) {
    const found = [];
    walkElements(root, function(node) {
      if (!predicate || predicate(node)) {
        found.push(node);
      }
    });

    return found;
  }

  function managedLinks(root) {
    return collectElements(root || document.body, function(node) {
      return node.hasAttribute && node.hasAttribute(LINK_ATTR);
    });
  }

  function currentNavigationURL() {
    return navigationURLParts(navigationState.currentURL || windowLocationHref()) || navigationURLParts(windowLocationHref());
  }

  function mediaQueryMatches(query) {
    return typeof window.matchMedia === "function" && window.matchMedia(query).matches;
  }

  function reducedDataMode() {
    return Boolean(
      (window.navigator && window.navigator.connection && window.navigator.connection.saveData)
      || mediaQueryMatches("(prefers-reduced-data: reduce)")
    );
  }

  function coarsePointerMode() {
    return Boolean(
      mediaQueryMatches("(pointer: coarse)")
      || mediaQueryMatches("(any-pointer: coarse)")
    );
  }

  function currentNavigationSnapshot() {
    const current = currentNavigationURL();
    return {
      phase: navigationState.phase || "idle",
      currentURL: current ? current.href : windowLocationHref(),
      currentPath: current ? current.path : "/",
      pendingURL: String(navigationState.pendingURL || ""),
      reducedData: reducedDataMode(),
      coarsePointer: coarsePointerMode(),
    };
  }

  function applyNavigationState() {
    const snapshot = currentNavigationSnapshot();
    const root = document.documentElement || document.body;
    const body = document.body || root;
    for (const node of [root, body]) {
      if (!node) continue;
      setOptionalAttr(node, NAV_STATE_ATTR, snapshot.phase);
      setOptionalAttr(node, NAV_CURRENT_PATH_ATTR, snapshot.currentPath);
      setOptionalAttr(node, NAV_PENDING_URL_ATTR, snapshot.pendingURL);
    }
    refreshManagedLinks(snapshot.currentURL);
    refreshManagedForms();
  }

  function dispatchNavigationState(reason) {
    dispatchManagedEvent("gosx:navigation:state", {
      detail: {
        reason: reason || "navigation",
        state: currentNavigationSnapshot(),
      },
    });
  }

  function setNavigationState(next, reason) {
    navigationState = Object.assign({}, navigationState, next || {});
    applyNavigationState();
    dispatchNavigationState(reason);
  }

  function dispatchManagedEvent(type, init) {
    if (typeof document.dispatchEvent !== "function" || typeof CustomEvent !== "function") {
      return;
    }
    document.dispatchEvent(new CustomEvent(type, init || {}));
  }

  function observeNavigation(level, message, fields) {
    if (typeof window.__gosx_emit !== "function") return;
    window.__gosx_emit(level, "navigation", message, fields || {});
  }

  function reportNavigationFailure(operation, error, fields) {
    if (error && String(error.name || "") === "AbortError") return null;
    const options = fields || {};
    if (window.__gosx && typeof window.__gosx.reportFailure === "function") {
      return window.__gosx.reportFailure(operation, error, Object.assign({
        scope: "navigation",
        type: "navigation",
        severity: "warning",
        fallback: "native",
        telemetry: options.telemetry || {},
      }, options));
    }
    if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
      return window.__gosx.reportIssue(Object.assign({
        scope: "navigation",
        type: "navigation",
        severity: "warning",
        error,
        fallback: "native",
      }, options));
    }
    return null;
  }

  function linkPrefetchMode(anchor) {
    const value = String(anchor && anchor.getAttribute && anchor.getAttribute(PREFETCH_ATTR) || "").trim().toLowerCase();
    return value || "intent";
  }

  function shouldPrefetchLink(anchor, trigger) {
    if (!anchor || !anchor.getAttribute) return false;
    const target = navigationURLParts(anchor.getAttribute("href"));
    if (!isSameOriginNavigation(target && target.href, windowLocationHref())) return false;
    if (sameNavigationURL(target, currentNavigationURL())) return false;
    const mode = linkPrefetchMode(anchor);
    if (mode === "off") return false;
    const snapshot = currentNavigationSnapshot();
    if (snapshot.reducedData && mode !== "force") {
      return false;
    }
    if (trigger === "render") {
      return mode === "render" || mode === "force";
    }
    if (trigger === "hover" && snapshot.coarsePointer && mode !== "force") {
      return false;
    }
    return mode === "intent" || mode === "render" || mode === "force";
  }

  function normalizeManagedLinkRelation(value, allowAuto) {
    const relation = String(value || "").trim().toLowerCase();
    if (!relation) {
      return "";
    }
    if (allowAuto && relation === "auto") {
      return "auto";
    }
    if (relation === "page" || relation === "ancestor" || relation === "none") {
      return relation;
    }
    return "none";
  }

  function managedAutoCurrentRelation(anchor, currentURL) {
    const href = anchor && anchor.getAttribute ? anchor.getAttribute("href") : "";
    const target = navigationURLParts(href);
    const current = navigationURLParts(currentURL || window.location.href);
    if (!target || !current || target.origin !== current.origin) {
      return "none";
    }
    if (sameNavigationURL(target, current)) {
      return "page";
    }
    if (ancestorNavigationURL(target, current)) {
      return "ancestor";
    }
    return "none";
  }

  function managedCurrentPolicy(anchor, currentURL) {
    if (!anchor || !anchor.getAttribute) {
      return "auto";
    }
    if (anchor.hasAttribute && anchor.hasAttribute(LINK_CURRENT_POLICY_ATTR)) {
      return normalizeManagedLinkRelation(anchor.getAttribute(LINK_CURRENT_POLICY_ATTR), true) || "auto";
    }
    const legacy = normalizeManagedLinkRelation(anchor.getAttribute(LINK_CURRENT_ATTR), false);
    if (!legacy) {
      return "auto";
    }
    const auto = managedAutoCurrentRelation(anchor, currentURL);
    return legacy === auto ? "auto" : legacy;
  }

  function managedCurrentRelation(anchor, currentURL) {
    const policy = managedCurrentPolicy(anchor, currentURL);
    if (anchor && anchor.setAttribute) {
      anchor.setAttribute(LINK_CURRENT_POLICY_ATTR, policy);
    }
    if (policy !== "auto") {
      return policy;
    }
    return managedAutoCurrentRelation(anchor, currentURL);
  }

  function syncManagedAriaCurrent(anchor, relation) {
    if (!anchor || !anchor.setAttribute) {
      return;
    }
    if (relation === "page") {
      if (!anchor.hasAttribute("aria-current")) {
        anchor.setAttribute("aria-current", "page");
        anchor.setAttribute(LINK_MANAGED_CURRENT_ATTR, "true");
      }
      return;
    }
    if (anchor.getAttribute && anchor.getAttribute(LINK_MANAGED_CURRENT_ATTR) === "true") {
      anchor.removeAttribute("aria-current");
      anchor.removeAttribute(LINK_MANAGED_CURRENT_ATTR);
    }
  }

  function refreshManagedLinks(currentURL) {
    const current = navigationURLParts(currentURL || window.location.href);
    const pending = navigationURLParts(navigationState.pendingURL);
    for (const anchor of managedLinks(document.body)) {
      const href = navigationURLParts(anchor.getAttribute("href"));
      const relation = managedCurrentRelation(anchor, current && current.href);
      anchor.setAttribute(LINK_CURRENT_ATTR, relation);
      syncManagedAriaCurrent(anchor, relation);
      const state = navigationState.phase === "pending" && href && pending && sameNavigationURL(href, pending) ? "pending" : "idle";
      anchor.setAttribute(LINK_STATE_ATTR, state);
      if (!anchor.hasAttribute(LINK_PREFETCH_STATE_ATTR)) {
        anchor.setAttribute(LINK_PREFETCH_STATE_ATTR, "idle");
      }
    }
  }

  // managedFormShorthandTruthy mirrors the server's truthy rule for
  // FORM_MANAGED_SHORTHAND_ATTR (gosx.ManagedFormShorthandTruthy in the Go
  // source, the single definition all three server render paths call): a
  // bare attribute and its `=""` spelling are indistinguishable once
  // parsed and both are truthy, "false" (case-insensitive, surrounding
  // whitespace ignored) opts out, and any other value is truthy — so
  // `data-gosx-managed`, `data-gosx-managed=""`, and
  // `data-gosx-managed="true"` all opt in. A null value (the attribute is
  // absent) is falsy; isManagedFormElement below only calls this after
  // confirming the attribute exists, so that branch is defensive only.
  function managedFormShorthandTruthy(value) {
    if (value == null) return false;
    const trimmed = String(value).trim();
    if (trimmed === "") return true;
    return trimmed.toLowerCase() !== "false";
  }

  // isManagedFormElement is the single place the runtime decides whether a
  // <form> is under managed navigation, so a form still carrying the raw
  // FORM_MANAGED_SHORTHAND_ATTR (never expanded server-side, or built
  // directly by client JS) is discovered exactly like one already carrying
  // the full FORM_ATTR contract.
  //
  // The shorthand branch below is scoped to <form> elements only (gosx#179
  // F5): every server render path leaves the shorthand inert on a non-form
  // element (see managedFormAttrs in route/fileprogram.go), so a
  // data-gosx-managed on, say, a <div> is not a managed form and must not
  // gain form-only lifecycle attributes (data-gosx-form-mode,
  // data-gosx-form-state) from refreshManagedForms. FORM_ATTR is left
  // unguarded: it is a framework-written attribute, never hand-authored on
  // a non-form element, so no equivalent risk exists there.
  function isManagedFormElement(node) {
    if (!node || !node.hasAttribute) return false;
    if (node.hasAttribute(FORM_ATTR)) return true;
    if (isElement(node, "FORM") && node.hasAttribute(FORM_MANAGED_SHORTHAND_ATTR)) {
      return managedFormShorthandTruthy(node.getAttribute(FORM_MANAGED_SHORTHAND_ATTR));
    }
    return false;
  }

  function managedForms(root) {
    return collectElements(root, isManagedFormElement);
  }

  function normalizeManagedFormMode(value) {
    const mode = String(value || "").trim().toLowerCase();
    if (mode === "get" || mode === "post") {
      return mode;
    }
    return "";
  }

  function managedFormMode(form, submitter) {
    const submitterMethod = submitterAttribute(submitter, "formMethod");
    if (submitterMethod) {
      return normalizeManagedFormMode(submitterMethod);
    }
    if (submitter && submitter.hasAttribute && submitter.hasAttribute(FORM_MODE_ATTR)) {
      return normalizeManagedFormMode(submitter.getAttribute(FORM_MODE_ATTR));
    }
    if (form && form.hasAttribute && form.hasAttribute(FORM_MODE_ATTR)) {
      return normalizeManagedFormMode(form.getAttribute(FORM_MODE_ATTR));
    }
    if (form && form.hasAttribute && form.hasAttribute("method")) {
      return normalizeManagedFormMode(form.getAttribute("method"));
    }
    return "get";
  }

  function refreshManagedForms() {
    for (const form of managedForms(document.body)) {
      const mode = managedFormMode(form, null);
      if (mode) {
        form.setAttribute(FORM_MODE_ATTR, mode);
      } else if (form.hasAttribute(FORM_MODE_ATTR)) {
        form.removeAttribute(FORM_MODE_ATTR);
      }
      if (!form.hasAttribute(FORM_STATE_ATTR)) {
        form.setAttribute(FORM_STATE_ATTR, "idle");
      }
    }
  }

  function submitAction(action, fields, options) {
    const opts = options || {};
    const host = actionFormHost(opts);
    const form = document.createElement("form");
    const method = normalizeManagedFormMode(opts.method) || "post";
    form.setAttribute("method", method);
    form.setAttribute("action", String(action || window.location.href));
    form.setAttribute(FORM_ATTR, "");
    form.setAttribute(FORM_STATE_ATTR, "idle");
    form.setAttribute("hidden", "");
    form.hidden = true;

    const entries = actionFieldEntries(fields);
    for (const entry of entries) {
      appendActionField(form, entry[0], entry[1]);
    }

    if (!entries.some(function(entry) { return entry[0] === "csrf_token"; })) {
      const csrfToken = resolveActionCSRFToken(host, opts);
      if (csrfToken) {
        appendActionField(form, "csrf_token", csrfToken);
      }
    }

    host.appendChild(form);
    refreshManagedForms();

    const done = submitForm(form, null).finally(function() {
      if (opts.keepForm === true) {
        return;
      }
      if (form.parentNode && typeof form.parentNode.removeChild === "function") {
        form.parentNode.removeChild(form);
      }
    });
    form.__gosxSubmitPromise = done;
    return form;
  }

  function actionFormHost(options) {
    const root = options && options.root;
    if (root && typeof root.appendChild === "function") {
      return root;
    }
    return resolveMainTarget(document.body) || document.body;
  }

  function actionFieldEntries(fields) {
    const entries = [];
    if (!fields) {
      return entries;
    }
    if (typeof fields.forEach === "function") {
      fields.forEach(function(value, key) {
        entries.push([String(key), value]);
      });
      return entries;
    }
    if (Array.isArray(fields)) {
      for (const entry of fields) {
        if (!entry || entry.length < 1) continue;
        entries.push([String(entry[0]), entry.length > 1 ? entry[1] : ""]);
      }
      return entries;
    }
    for (const key of Object.keys(fields)) {
      entries.push([String(key), fields[key]]);
    }
    return entries;
  }

  function appendActionField(form, name, value) {
    if (Array.isArray(value)) {
      for (const item of value) {
        appendActionField(form, name, item);
      }
      return;
    }
    const input = document.createElement("input");
    input.setAttribute("type", "hidden");
    input.setAttribute("name", String(name));
    input.value = value == null ? "" : String(value);
    input.setAttribute("value", input.value);
    form.appendChild(input);
  }

  function resolveActionCSRFToken(host, options) {
    if (options && options.csrf != null) {
      return String(options.csrf);
    }
    return csrfTokenFromElement(host)
      || csrfTokenFromElement(document.documentElement)
      || csrfTokenFromInput(host)
      || csrfTokenFromInput(document.body)
      || csrfTokenFromMeta();
  }

  function csrfTokenFromElement(element) {
    if (!element || !element.getAttribute) {
      return "";
    }
    return String(
      element.getAttribute("data-gosx-csrf-token")
      || element.getAttribute("data-csrf-token")
      || element.getAttribute("data-csrf")
      || ""
    );
  }

  function csrfTokenFromInput(root) {
    const input = findElement(root, function(node) {
      return isElement(node, "INPUT") && node.getAttribute && node.getAttribute("name") === "csrf_token";
    });
    return input ? String(input.value || input.getAttribute("value") || "") : "";
  }

  function csrfTokenFromMeta() {
    const meta = findElement(document.head, function(node) {
      return isElement(node, "META")
        && node.getAttribute
        && (node.getAttribute("name") === "csrf-token" || node.getAttribute("name") === "gosx-csrf-token");
    });
    return meta ? String(meta.getAttribute("content") || "") : "";
  }

  function prefetchManagedLinks(trigger) {
    for (const anchor of managedLinks(document.body)) {
      prefetchLink(anchor, trigger);
    }
  }

  function findElementByID(root, id) {
    if (!id) return null;
    return findElement(root, function(node) {
      return node.getAttribute && node.getAttribute("id") === id;
    });
  }

  function isNaturallyFocusable(node) {
    if (!node || node.nodeType !== 1) {
      return false;
    }
    if (node.hasAttribute && node.hasAttribute("tabindex")) {
      return true;
    }

    switch (String(node.tagName || "").toUpperCase()) {
      case "A":
        return !!node.getAttribute("href");
      case "AUDIO":
      case "VIDEO":
        return node.hasAttribute("controls");
      case "BUTTON":
      case "IFRAME":
      case "INPUT":
      case "SELECT":
      case "SUMMARY":
      case "TEXTAREA":
        return !node.hasAttribute("disabled");
      default:
        return node.hasAttribute && node.hasAttribute("contenteditable");
    }
  }

  function ensureFocusable(node) {
    if (!node || !node.setAttribute || isNaturallyFocusable(node)) {
      return;
    }
    if (!node.hasAttribute("tabindex")) {
      node.setAttribute("tabindex", "-1");
      node.setAttribute(MANAGED_FOCUS_ATTR, "");
    }
  }

  function focusElement(node, preventScroll) {
    if (!node || typeof node.focus !== "function") {
      return;
    }

    ensureFocusable(node);
    try {
      node.focus(preventScroll ? { preventScroll: true } : undefined);
    } catch (_) {
      node.focus();
    }
  }

  function ensureNavigationAnnouncer() {
    const existing = findElement(document.body, function(node) {
      return node.hasAttribute && node.hasAttribute(ANNOUNCER_ATTR);
    });
    if (existing) {
      return existing;
    }

    const region = document.createElement("div");
    region.setAttribute(ANNOUNCER_ATTR, "");
    region.setAttribute("role", "status");
    region.setAttribute("aria-live", "polite");
    region.setAttribute("aria-atomic", "true");
    region.setAttribute("style", "position:absolute;left:-9999px;width:1px;height:1px;overflow:hidden;");
    document.body.appendChild(region);
    return region;
  }

  function announceNavigation(message) {
    const text = normalizeTextValue(message);
    if (!text) {
      return "";
    }

    const region = ensureNavigationAnnouncer();
    region.textContent = "";
    announceSeq += 1;
    const currentSeq = announceSeq;
    Promise.resolve().then(function() {
      if (currentSeq !== announceSeq) {
        return;
      }
      region.textContent = text;
    });
    return text;
  }

  function customAnnouncement(root) {
    const node = findElement(root, function(candidate) {
      return candidate.hasAttribute && candidate.hasAttribute(ANNOUNCE_ATTR);
    });
    if (!node) {
      return "";
    }

    const attrValue = normalizeTextValue(node.getAttribute(ANNOUNCE_ATTR));
    if (attrValue) {
      return attrValue;
    }
    return normalizeTextValue(node.textContent);
  }

  function resolveMainTarget(root) {
    return findElement(root, function(node) {
      return node.hasAttribute && node.hasAttribute(MAIN_ATTR);
    }) || findElement(root, function(node) {
      return isElement(node, "MAIN");
    }) || findElement(root, function(node) {
      return String((node.getAttribute && node.getAttribute("role")) || "").toLowerCase() === "main";
    }) || findElement(root, function(node) {
      return isElement(node, "H1");
    }) || document.body;
  }

  function resolveHashTarget(url) {
    const hash = String(url && url.hash || "");
    if (hash.length <= 1) {
      return null;
    }

    let targetID = hash.slice(1);
    try {
      targetID = decodeURIComponent(targetID);
    } catch (_) {}

    return findElementByID(document.body, targetID);
  }

  function resolveNavigationA11y(nextURL) {
    const url = new URL(nextURL, window.location.href);
    const hashTarget = resolveHashTarget(url);
    const focusTarget = hashTarget || resolveMainTarget(document.body);
    const announcement = customAnnouncement(document.body)
      || normalizeTextValue(document.title)
      || normalizeTextValue(focusTarget && focusTarget.textContent);

    return {
      announcement: announcement,
      focusTarget: focusTarget,
      hashTarget: hashTarget,
    };
  }

  // adoptOrClone mirrors cloneIntoDocument's deep-clone-and-normalize
  // behavior EXCEPT for elements whose id is a key in `reused` — those are
  // moved (not cloned) from the CURRENT live document, preserving their
  // identity. This is what lets a reused Scene3D engine's canvas keep its
  // WebGL/WebGPU rendering context across a soft navigation: a same-document
  // move (appendChild on a node already attached elsewhere) preserves the
  // context; cloneNode(true) + discarding the original does not — it
  // produces a brand-new canvas with no context at all, which is exactly
  // the "full re-mount" behavior reuse is meant to skip. Recurses so a
  // reusable element nested inside a freshly-cloned wrapper (its ancestor
  // isn't itself reused) still gets adopted.
  function adoptOrClone(node, baseURL, reused) {
    if (!reused || reused.size === 0) {
      return cloneIntoDocument(node, baseURL);
    }
    if (node && node.nodeType === 1) {
      const id = node.getAttribute && node.getAttribute("id");
      if (id && reused.has(id)) {
        return reused.get(id);
      }
    }
    if (node && typeof node.cloneNode === "function") {
      const shallow = node.cloneNode(false);
      normalizeNodeURLs(shallow, baseURL);
      for (const child of toArray(node.childNodes)) {
        shallow.appendChild(adoptOrClone(child, baseURL, reused));
      }
      return shallow;
    }
    return node;
  }

  function replaceBody(nextDoc, baseURL, reuseIDs) {
    const body = document.body;
    const nextBody = nextDoc.body;
    const existingAttrs = attributeEntries(body);
    for (const entry of existingAttrs) {
      body.removeAttribute(entry.name);
    }
    for (const entry of attributeEntries(nextBody)) {
      body.setAttribute(entry.name, entry.value);
    }

    // Detach (not destroy) any live mount elements this navigation is
    // reusing — captured BEFORE the body is wiped below so their rendering
    // context survives the swap. See window.__gosx_reusable_engines and
    // adoptOrClone above. Resolved via the engine registry's OWN `mount`
    // element reference (not a fresh getElementById(engineID) lookup) since
    // the reuse set is keyed by engine id, while the actual DOM element id
    // is entry.mountId (defaults to entry.id, but is not guaranteed equal) —
    // record.mount.id sidesteps that distinction entirely by using whatever
    // id the live element actually has.
    const reused = new Map();
    if (reuseIDs && typeof reuseIDs.forEach === "function" && window.__gosx && window.__gosx.engines) {
      reuseIDs.forEach(function(engineID) {
        const record = window.__gosx.engines.get(engineID);
        const el = record && record.mount;
        if (el && el.id) reused.set(el.id, el);
      });
    }

    while (body.firstChild) {
      body.removeChild(body.firstChild);
    }

    const children = toArray(nextBody.childNodes);
    for (const child of children) {
      if (isElement(child, "SCRIPT") && child.hasAttribute(SCRIPT_ROLE) && child.getAttribute("src")) {
        continue;
      }
      body.appendChild(adoptOrClone(child, baseURL, reused));
    }
  }

  function inlineNavigationScriptCanReplay(script) {
    if (!isElement(script, "SCRIPT")) return false;
    if (!script.hasAttribute(NAV_INLINE_REPLAY_ATTR)) return false;
    if (script.hasAttribute(SCRIPT_ROLE)) return false;
    if (script.getAttribute("src")) return false;
    if (script.getAttribute(NAV_INLINE_REPLAYED_ATTR) === "true") return false;
    const type = String(script.getAttribute("type") || "").trim().toLowerCase();
    return !type
      || type === "text/javascript"
      || type === "application/javascript"
      || type === "module";
  }

  // The marker value selects when the script replays during navigation.
  // "pre-bootstrap" runs before managed scripts load and before the engine
  // bootstrap consumes the manifest. Every other value replays after
  // bootstrap completes.
  function inlineNavigationReplayPhase(script) {
    const value = String(script.getAttribute(NAV_INLINE_REPLAY_ATTR) || "").trim().toLowerCase();
    return value === "pre-bootstrap" ? "pre-bootstrap" : "post";
  }

  function replayInlineNavigationScripts(root, phase) {
    if (!root || typeof root.querySelectorAll !== "function") return;
    const activePhase = phase === "pre-bootstrap" ? "pre-bootstrap" : "post";
    const scripts = root.querySelectorAll("script[" + NAV_INLINE_REPLAY_ATTR + "]");
    for (const script of toArray(scripts)) {
      if (!inlineNavigationScriptCanReplay(script) || !script.parentNode) {
        continue;
      }
      if (inlineNavigationReplayPhase(script) !== activePhase) {
        continue;
      }
      const executable = document.createElement("script");
      for (const attr of attributeEntries(script)) {
        if (attr.name === NAV_INLINE_REPLAYED_ATTR) continue;
        executable.setAttribute(attr.name, attr.value);
      }
      executable.setAttribute(NAV_INLINE_REPLAYED_ATTR, "true");
      executable.textContent = script.textContent || "";
      script.setAttribute(NAV_INLINE_REPLAYED_ATTR, "true");
      script.parentNode.insertBefore(executable, script);
      script.parentNode.removeChild(script);
    }
  }

  function collectManagedScripts(root, baseURL) {
    const found = [];
    function walk(node) {
      if (!node || !node.childNodes) return;
      for (const child of toArray(node.childNodes)) {
        if (isElement(child, "SCRIPT") && child.hasAttribute(SCRIPT_ROLE) && child.getAttribute("src")) {
          found.push({
            role: child.getAttribute(SCRIPT_ROLE),
            src: absolutizeURL(child.getAttribute("src"), baseURL),
            load: child.getAttribute("data-gosx-script-load") || "",
          });
        }
        walk(child);
      }
    }
    walk(root);
    return found;
  }

  function findLoadedScript(src, includePending) {
    const scripts = document.querySelectorAll("script[src]");
    for (const script of scripts) {
      if (absolutizeURL(script.getAttribute("src"), windowLocationHref()) === src) {
        if (!includePending && script.getAttribute("data-gosx-script-loaded") === "pending") {
          continue;
        }
        return script;
      }
    }
    return null;
  }

  function loadManagedScriptTag(role, src) {
    const existing = findLoadedScript(src);
    if (existing) {
      existing.setAttribute(SCRIPT_ROLE, existing.getAttribute(SCRIPT_ROLE) || role || "managed");
      return Promise.resolve(false);
    }
    return new Promise(function(resolve, reject) {
      const script = document.createElement("script");
      script.src = src;
      script.async = false;
      script.setAttribute(SCRIPT_ROLE, role || "managed");
      script.setAttribute("data-gosx-script-load", "dom");
      applyCurrentNonce(script);
      script.onload = function() {
        script.setAttribute("data-gosx-script-loaded", "true");
        resolve(false);
      };
      script.onerror = function() {
        reject(new Error("script load failed: " + src));
      };
      (document.head || document.documentElement).appendChild(script);
    });
  }

  async function loadManagedScript(role, src, load) {
    if (!src) return false;
    // gosxHost.lifecycle.bootstrapPage is a forwarding shim installed by
    // compatibility.ts on every page (see compatibility.ts), so it is always
    // a function — probing it can never detect whether the real bootstrap
    // bundle already ran. Probe the ambient name directly instead: it stays
    // absent until the bootstrap bundle installs it, matching the pre-typed
    // behavior this guard restores.
    if (role === "bootstrap" && typeof gosxHostCompatibility.read("__gosx_bootstrap_page") === "function") {
      return false;
    }
    const effectiveLoad = load === "dom" || currentDocumentNonce() ? "dom" : "eval";
    const cacheKey = effectiveLoad + ":" + src;
    // The initial document already executed its deferred runtime chunks, but
    // the navigation cache starts empty. Reusing the exact same chunk on the
    // next route must not fetch+eval it again: Scene3D deliberately publishes
    // several non-writable globals and is not a re-entrant module body.
    if (findLoadedScript(src)) {
      scriptCache.set(cacheKey, Promise.resolve());
      return false;
    }
    if (scriptCache.has(cacheKey)) {
      await scriptCache.get(cacheKey);
      return false;
    }

    const promise = effectiveLoad === "dom"
      ? loadManagedScriptTag(role, src)
      : (async function() {
        const resp = await gosxRuntimeRequest(src);
        if (!resp.ok) {
          throw new Error("script fetch failed: " + src + " (" + resp.status + ")");
        }
        const source = await resp.text();
        (0, eval)(String(source) + "\n//# sourceURL=" + src);
        const marker = findLoadedScript(src, true);
        if (marker) marker.setAttribute("data-gosx-script-loaded", "true");
      })();

    scriptCache.set(cacheKey, promise);
    await promise;
    return role === "bootstrap";
  }

  async function ensureManagedScripts(nextDoc, baseURL, collectedScripts) {
    const scripts = Array.isArray(collectedScripts)
      ? collectedScripts
      : collectManagedScripts(nextDoc.head, baseURL).concat(collectManagedScripts(nextDoc.body, baseURL));
    scripts.sort(function(a, b) {
      // The wrapped standard-Go shim captures its Go constructor without
      // replacing TinyGo's shared constructor. Load it first, then TinyGo,
      // before any bootstrap can instantiate either runtime.
      const order = {
        "standard-go-wasm-exec": 0,
        "wasm-exec": 1,
        patch: 2,
        bootstrap: 3,
        lifecycle: 4,
        managed: 5,
      };
      const left = Object.prototype.hasOwnProperty.call(order, a.role) ? order[a.role] : 99;
      const right = Object.prototype.hasOwnProperty.call(order, b.role) ? order[b.role] : 99;
      return left - right;
    });

    let bootstrapLoadedNow = false;
    for (const script of scripts) {
      if (await loadManagedScript(script.role, script.src, script.load)) {
        bootstrapLoadedNow = true;
      }
    }
    return bootstrapLoadedNow;
  }

  // Both hooks resolve through the ambient compatibility name AT CALL TIME,
  // not through gosxHost.lifecycle.disposePage/bootstrapPage directly. Once
  // page-disposal.ts and 30k-tail-init.js run, they bind gosxHost.lifecycle
  // to their own concrete closures (Object.assign), which freezes those two
  // properties to whatever ran first. A later installer of the ambient name
  // — stripe-bridge.ts wraps __gosx_bootstrap_page/__gosx_dispose_page to
  // add its own mount/dispose step, and origin/main's navigation runtime
  // always read window.__gosx_bootstrap_page/__gosx_dispose_page live at
  // call time — would then be silently skipped. Forwarding through
  // gosxHostCompatibility keeps every later installer live, matching main.
  async function disposeCurrentPage(reuseIDs) {
    await gosxHostCompatibility.forward("__gosx_dispose_page", [reuseIDs]);
  }

  async function bootstrapCurrentPage(bootstrapLoadedNow, reuseIDs) {
    if (!bootstrapLoadedNow) {
      await gosxHostCompatibility.forward("__gosx_bootstrap_page", [reuseIDs]);
    }
  }

  function updateHistory(url, replace) {
    if (!window.history) return;
    if (replace && typeof window.history.replaceState === "function") {
      window.history.replaceState({}, "", url);
      return;
    }
    if (typeof window.history.pushState === "function") {
      window.history.pushState({}, "", url);
    }
  }

  function shouldHandleLink(anchor, event) {
    if (!isManagedNavigationLink(anchor)) return false;
    if (!isPrimaryNavigationEvent(event)) return false;
    if (!allowsManagedLinkHandling(anchor)) return false;
    return isSameOriginNavigation(anchor.getAttribute("href"), windowLocationHref());
  }

  function isManagedNavigationLink(anchor) {
    return !!anchor && !!anchor.hasAttribute && anchor.hasAttribute(LINK_ATTR);
  }

  function isPrimaryNavigationEvent(event) {
    return !!event
      && !event.defaultPrevented
      && event.button === 0
      && !event.metaKey
      && !event.ctrlKey
      && !event.shiftKey
      && !event.altKey;
  }

  function allowsManagedLinkHandling(anchor) {
    if (!anchor) {
      return false;
    }
    if (anchor.getAttribute("target") || anchor.hasAttribute("download")) {
      return false;
    }
    const href = String(anchor.getAttribute("href") || "");
    return !!href && href[0] !== "#";
  }

  function isSameOriginNavigation(value, baseURL) {
    const url = navigationURLParts(value);
    const current = navigationURLParts(baseURL || windowLocationHref());
    return isHTTPNavigationURL(url) && isHTTPNavigationURL(current) && url.origin === current.origin;
  }

  function closestLink(node) {
    let current = node;
    while (current) {
      if (current.hasAttribute && current.hasAttribute(LINK_ATTR)) {
        return current;
      }
      current = current.parentNode;
    }
    return null;
  }

  function shouldHandleForm(form, event) {
    if (!isManagedFormElement(form)) return false;
    if (event.defaultPrevented) return false;
    const submitter = event && event.submitter ? event.submitter : null;
    if (formSubmitTarget(form, submitter)) return false;

    const method = formSubmissionMethod(form, submitter);
    if (method !== "GET" && method !== "POST") return false;

    const action = formSubmissionAction(form, submitter) || window.location.href;
    return isSameOriginNavigation(action, windowLocationHref());
  }

  function submitterAttribute(submitter, name) {
    if (!submitter) return "";
    const attrName = submitterAttributeName(name);
    if (!submitter.hasAttribute || !submitter.hasAttribute(attrName)) {
      return "";
    }
    const property = submitterProperty(submitter, name);
    if (property) {
      return property;
    }
    return typeof submitter.getAttribute === "function" ? String(submitter.getAttribute(attrName) || "") : "";
  }

  function submitterProperty(submitter, name) {
    if (!submitter || !name) return "";
    const value = submitter[name];
    return typeof value === "string" && value ? value : "";
  }

  function submitterAttributeName(name) {
    const key = String(name || "").trim();
    return SUBMITTER_ATTRS[key] || key.toLowerCase();
  }

  function formSubmissionMethod(form, submitter) {
    return managedFormMode(form, submitter).toUpperCase();
  }

  function formSubmissionAction(form, submitter) {
    return String(
      submitterAttribute(submitter, "formAction")
      || (form && form.getAttribute ? form.getAttribute("action") : "")
      || window.location.href
    );
  }

  function formSubmitTarget(form, submitter) {
    return String(
      submitterAttribute(submitter, "formTarget")
      || (form && form.getAttribute ? form.getAttribute("target") : "")
      || ""
    ).trim();
  }

  function serializeForm(form, submitter) {
    const formData = new FormData(form);
    const submitterName = submitter && (submitter.name || (typeof submitter.getAttribute === "function" ? submitter.getAttribute("name") : ""));
    const submitterValue = submitter && (submitter.value || (typeof submitter.getAttribute === "function" ? submitter.getAttribute("value") : "") || "");
    if (submitterName && !formData.has(submitterName)) {
      formData.append(submitterName, submitterValue);
    }
    return formData;
  }

  function formCSRFToken(formData) {
    if (!formData || typeof formData.get !== "function") return "";
    const token = formData.get("csrf_token");
    if (token == null) return "";
    return String(token);
  }

  function captureManagedFormState(form) {
    if (!form || !form.getAttribute) {
      return { pending: null, state: null };
    }
    return {
      pending: form.getAttribute(FORM_PENDING_ATTR),
      state: form.getAttribute(FORM_STATE_ATTR),
    };
  }

  function setManagedFormPending(form) {
    if (!form || !form.setAttribute) {
      return;
    }
    form.setAttribute(FORM_PENDING_ATTR, "true");
    form.setAttribute(FORM_STATE_ATTR, "pending");
  }

  function restoreManagedFormState(form, snapshot) {
    if (!form) {
      return;
    }
    const previous = snapshot || { pending: null, state: null };
    if (previous.pending == null) {
      if (form.removeAttribute) {
        form.removeAttribute(FORM_PENDING_ATTR);
      }
    } else if (form.setAttribute) {
      form.setAttribute(FORM_PENDING_ATTR, previous.pending);
    }
    if (previous.state == null) {
      if (form.setAttribute) {
        form.setAttribute(FORM_STATE_ATTR, "idle");
      }
    } else if (form.setAttribute) {
      form.setAttribute(FORM_STATE_ATTR, previous.state);
    }
  }

  function dispatchManagedFormNavigate(action, method) {
    dispatchManagedEvent("gosx:form:navigate", {
      detail: {
        action: action,
        method: method,
      },
    });
  }

  function dispatchManagedFormResult(action, method, response, result) {
    dispatchManagedEvent("gosx:form:result", {
      detail: {
        action: action,
        method: method,
        ok: !!(response && response.ok),
        status: response ? response.status : 0,
        result: result,
      },
    });
  }

  async function parseJSONResponse(response) {
    if (window.__gosx && window.__gosx.transport && typeof window.__gosx.transport.json === "function") {
      return window.__gosx.transport.json(response);
    }
    try {
      return await response.json();
    } catch (_) {
      return null;
    }
  }

  function applyManagedFormData(result) {
    if (result && result.data && typeof window.__gosx_set_input_batch === "function") {
      window.__gosx_set_input_batch(JSON.stringify(result.data));
    }
  }

  function hasClass(node, className) {
    const value = String(node && node.getAttribute && node.getAttribute("class") || "");
    return value.split(/\s+/).indexOf(className) >= 0;
  }

  function managedFormControls(form) {
    return collectElements(form, function(node) {
      const tag = String(node.tagName || "").toUpperCase();
      return (tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA")
        && !!node.getAttribute("name");
    });
  }

  function managedFormFieldControls(form, name, controls) {
    const fieldName = String(name);
    const css = window.CSS;
    if (form && typeof form.querySelectorAll === "function" && css && typeof css.escape === "function") {
      try {
        const matches = toArray(form.querySelectorAll('[name="' + css.escape(fieldName) + '"]')).filter(function(control) {
          return !form.contains || form.contains(control);
        });
        if (matches.length > 0) return matches;
      } catch (_) {}
    }
    return controls.filter(function(control) {
      return String(control.getAttribute("name") || "") === fieldName;
    });
  }

  function managedFormErrorNodes(form) {
    return collectElements(form, function(node) {
      return hasClass(node, "form-error");
    });
  }

  function describedFormError(form, control) {
    const ids = String(control && control.getAttribute("aria-describedby") || "").split(/\s+/).filter(Boolean);
    for (const id of ids) {
      const node = document.getElementById(id);
      if (node && hasClass(node, "form-error") && (!form.contains || form.contains(node))) return node;
    }
    return null;
  }

  function managedFieldError(form, controls, fieldName, errors) {
    for (const control of controls) {
      const described = describedFormError(form, control);
      if (described) return described;
    }
    for (const node of errors) {
      if (String(node.getAttribute("data-gosx-field-error") || node.getAttribute("data-field-error") || "") === fieldName) {
        return node;
      }
    }
    return errors.length === 1 ? errors[0] : null;
  }

  function linkControlDescription(control, errorNode) {
    if (!control || !errorNode) return;
    let id = errorNode.getAttribute("id");
    if (!id) {
      formErrorSeq += 1;
      id = "gosx-form-error-" + formErrorSeq;
      errorNode.setAttribute("id", id);
    }
    const ids = String(control.getAttribute("aria-describedby") || "").split(/\s+/).filter(Boolean);
    if (ids.indexOf(id) < 0) {
      ids.push(id);
      control.setAttribute("aria-describedby", ids.join(" "));
      const managed = String(control.getAttribute(FORM_ERROR_DESCRIPTION_ATTR) || "").split(/\s+/).filter(Boolean);
      if (managed.indexOf(id) < 0) managed.push(id);
      control.setAttribute(FORM_ERROR_DESCRIPTION_ATTR, managed.join(" "));
    }
  }

  function clearManagedControlDescription(control) {
    const managed = String(control.getAttribute(FORM_ERROR_DESCRIPTION_ATTR) || "").split(/\s+/).filter(Boolean);
    if (managed.length === 0) return;
    const ids = String(control.getAttribute("aria-describedby") || "").split(/\s+/).filter(Boolean).filter(function(id) {
      return managed.indexOf(id) < 0;
    });
    if (ids.length > 0) {
      control.setAttribute("aria-describedby", ids.join(" "));
    } else {
      control.removeAttribute("aria-describedby");
    }
    control.removeAttribute(FORM_ERROR_DESCRIPTION_ATTR);
  }

  function managedFormStatus(form) {
    return findElement(form, function(node) {
      return hasClass(node, "form-status") || hasClass(node, "action-message");
    });
  }

  function managedFormControlFocusable(control, form) {
    if (!control || control.disabled === true || control.hasAttribute("disabled")) return false;
    if (String(control.tagName || "").toUpperCase() === "INPUT"
        && String(control.getAttribute("type") || "").toLowerCase() === "hidden") return false;
    let node = control;
    while (node) {
      if ((node.hasAttribute && node.hasAttribute("hidden")) || node.hidden === true) return false;
      if (typeof window.getComputedStyle === "function") {
        const style = window.getComputedStyle(node);
        if (style && (style.display === "none" || style.visibility === "hidden")) return false;
      }
      if (node === form) break;
      node = node.parentNode;
    }
    return true;
  }

  function projectManagedFormResult(form, response, result) {
    if (!form || String(form.getAttribute(FORM_PROJECT_ATTR) || "").toLowerCase() === "off") return;
    const controls = managedFormControls(form);
    const errorNodes = managedFormErrorNodes(form);
    for (const control of controls) {
      control.removeAttribute("aria-invalid");
      clearManagedControlDescription(control);
    }
    for (const node of errorNodes) node.textContent = "";
    const status = managedFormStatus(form);
    if (status) status.textContent = "";

    const fieldErrors = result && result.fieldErrors && typeof result.fieldErrors === "object"
      ? result.fieldErrors
      : {};
    const names = Object.keys(fieldErrors);
    for (const name of names) {
      const fieldControls = managedFormFieldControls(form, name, controls);
      if (fieldControls.length === 0) continue;
      const errorNode = managedFieldError(form, fieldControls, name, errorNodes);
      if (errorNode) {
        errorNode.textContent = String(fieldErrors[name] || "");
      }
      for (const control of fieldControls) {
        control.setAttribute("aria-invalid", "true");
        if (errorNode) linkControlDescription(control, errorNode);
      }
    }

    let firstInvalid = null;
    for (const control of controls) {
      if (control.getAttribute("aria-invalid") === "true" && managedFormControlFocusable(control, form)) {
        firstInvalid = control;
        break;
      }
    }

    const failed = !response || !response.ok || !!(result && result.ok === false) || names.length > 0;
    const message = normalizeTextValue(result && result.message);
    const announcement = message || (failed ? "Action failed." : "Action completed.");
    if (status) status.textContent = announcement;
    form.setAttribute(FORM_STATE_ATTR, failed ? "error" : "success");
    announceNavigation(announcement);
    if (firstInvalid) {
      focusElement(firstInvalid, true);
      if (typeof firstInvalid.scrollIntoView === "function") {
        firstInvalid.scrollIntoView({
          behavior: mediaQueryMatches("(prefers-reduced-motion: reduce)") ? "auto" : "smooth",
          block: "center",
        });
      }
    }
  }

  async function submitManagedGetForm(url, method, formData) {
    await navigate(formNavigationURL(url, formData).href, { replace: false });
    dispatchManagedFormNavigate(url.href, method);
  }

  function hardNavigate(url, replace) {
    const location = window.location;
    if (replace && typeof location.replace === "function") {
      location.replace(url);
    } else if (!replace && typeof location.assign === "function") {
      location.assign(url);
    } else {
      location.href = url;
    }
  }

  function reportManagedActionResponseFailure(operation, error, url, method) {
    console.error("[gosx] " + operation + " failed:", error);
    reportNavigationFailure(operation, error, {
      source: url,
      telemetry: { url: url, method: method },
    });
  }

  async function submitManagedActionForm(url, method, formData) {
    const csrfToken = formCSRFToken(formData);
    const response = await gosxRuntimeRequest(url.href, {
      method: method,
      headers: {
        Accept: "application/json",
        "X-Requested-With": "XMLHttpRequest",
        ...(csrfToken ? { "X-CSRF-Token": csrfToken } : {}),
      },
      body: formData,
      redirect: "follow",
    });
    let result = null;
    try {
      result = await parseJSONResponse(response);
      applyManagedFormData(result);
    } catch (err) {
      reportManagedActionResponseFailure("form action response", err, url.href, method);
    }
    let redirected = false;
    if (result && result.redirect) {
      const redirectURL = navigationURLParts(result.redirect);
      if (!redirectURL || !isSameOriginNavigation(redirectURL.href, windowLocationHref())) {
        reportManagedActionResponseFailure(
          "form action redirect",
          new Error("blocked unsafe action redirect"),
          url.href,
          method,
        );
      } else {
        redirected = true;
        const redirectsCurrent = sameNavigationURL(redirectURL, currentNavigationURL());
        try {
          await navigate(redirectURL.href, {
            force: true,
            revalidate: true,
            mutationBarrier: true,
            replace: redirectsCurrent,
            preserveScroll: redirectsCurrent,
          });
        } catch (err) {
          reportManagedActionResponseFailure("form action redirect", err, redirectURL.href, method);
          try {
            hardNavigate(redirectURL.href, redirectsCurrent);
          } catch (fallbackError) {
            reportManagedActionResponseFailure("form action redirect fallback", fallbackError, redirectURL.href, method);
          }
        }
      }
    }
    try {
      dispatchManagedFormResult(url.href, method, response, result);
    } catch (err) {
      reportManagedActionResponseFailure("form action result", err, url.href, method);
    }
    return { response: response, result: result, redirected: redirected };
  }

  async function submitForm(form, submitter) {
    if (!form || pendingManagedForms.has(form)) return;
    pendingManagedForms.add(form);
    try {
      return await submitFormOnce(form, submitter);
    } finally {
      pendingManagedForms.delete(form);
    }
  }

  async function submitFormOnce(form, submitter) {

    const method = formSubmissionMethod(form, submitter);
    const action = formSubmissionAction(form, submitter) || window.location.href;
    const url = new URL(action, window.location.href);
    const formData = serializeForm(form, submitter);
    const previous = captureManagedFormState(form);
    let outcome = null;

    setManagedFormPending(form);

    try {
      if (method === "GET") {
        await submitManagedGetForm(url, method, formData);
        return;
      }
      outcome = await submitManagedActionForm(url, method, formData);
    } catch (err) {
      console.error("[gosx] form action failed:", err);
      reportNavigationFailure("form action", err, {
        source: url.href,
        telemetry: { url: url.href, method: method },
      });
      nativeSubmitForm(form, submitter);
      return;
    } finally {
      try {
        restoreManagedFormState(form, previous);
        if (outcome && !outcome.redirected
            && (!document.documentElement.contains || document.documentElement.contains(form))) {
          projectManagedFormResult(form, outcome.response, outcome.result);
        }
      } catch (err) {
        reportManagedActionResponseFailure("form action projection", err, url.href, method);
      }
    }
  }

  function formNavigationURL(url, formData) {
    const next = new URL(url.href);
    const params = new URLSearchParams();
    if (formData && typeof formData.forEach === "function") {
      formData.forEach(function(value, key) {
        params.append(String(key), value == null ? "" : String(value));
      });
    }
    next.search = params.toString();
    return next;
  }

  function nativeSubmitForm(form, submitter) {
    if (!form || !isManagedFormElement(form)) return;
    // Strip every attribute that makes isManagedFormElement true — FORM_ATTR
    // and/or the shorthand — so form.requestSubmit() below dispatches a
    // fresh "submit" event that shouldHandleForm lets through natively,
    // instead of re-intercepting it. Both are restored once the native
    // submission has been requested.
    const hadForm = form.hasAttribute(FORM_ATTR);
    const previousForm = hadForm ? form.getAttribute(FORM_ATTR) : null;
    const hadShorthand = form.hasAttribute(FORM_MANAGED_SHORTHAND_ATTR);
    const previousShorthand = hadShorthand ? form.getAttribute(FORM_MANAGED_SHORTHAND_ATTR) : null;
    if (hadForm) form.removeAttribute(FORM_ATTR);
    if (hadShorthand) form.removeAttribute(FORM_MANAGED_SHORTHAND_ATTR);
    try {
      if (typeof form.requestSubmit === "function") {
        if (submitter) {
          form.requestSubmit(submitter);
        } else {
          form.requestSubmit();
        }
        return;
      }
      if (submitter && typeof submitter.click === "function") {
        submitter.click();
        return;
      }
      if (typeof form.submit === "function") {
        form.submit();
      }
    } finally {
      if (hadForm) form.setAttribute(FORM_ATTR, previousForm);
      if (hadShorthand) form.setAttribute(FORM_MANAGED_SHORTHAND_ATTR, previousShorthand);
    }
  }

  function parseDocument(html) {
    if (typeof DOMParser === "undefined") {
      throw new Error("DOMParser is not available");
    }
    return new DOMParser().parseFromString(html, "text/html");
  }

  // pageCacheOptsOut reads the FETCHED page's own head, not the live
  // document's — a page opts its own HTML out of pageCache with <meta
  // name="gosx-page-cache" content="no-store">, the same meta/attribute
  // lookup shape as csrfTokenFromMeta above.
  function pageCacheOptsOut(html) {
    let doc;
    try {
      doc = parseDocument(html);
    } catch (_e) {
      return false;
    }
    const meta = findElement(doc && doc.head, function(node) {
      return isElement(node, "META")
        && node.getAttribute
        && node.getAttribute("name") === PAGE_CACHE_OPT_OUT_META;
    });
    return !!meta && String(meta.getAttribute("content") || "").toLowerCase() === PAGE_CACHE_OPT_OUT_VALUE;
  }

  // A pageCache entry is a Promise, decorated with its own insertion time
  // (__gosxCachedAt) rather than wrapped in a {value, insertedAt} record, so
  // the map keeps its original Map<string, Promise<{html,url}>> shape for
  // any code outside this module that already reads window.__gosx_page_cache.
  function pageCacheEntryExpired(entry) {
    return typeof entry.__gosxCachedAt === "number"
      && (Date.now() - entry.__gosxCachedAt) > PAGE_CACHE_TTL_MS;
  }

  async function fetchPage(url, signal, trackFetch) {
    const key = String(url);
    const cached = pageCache.get(key);
    if (cached) {
      if (pageCacheEntryExpired(cached)) {
        pageCache.delete(key);
      } else {
        return cached;
      }
    }

    const fetchID = trackFetch ? ++navigationFetchStarted : 0;
    const request = (async function() {
      const request = {
        headers: {
          Accept: "text/html",
          "X-GoSX-Navigation": "1",
        },
      };
      if (signal) request.signal = signal;
      const response = await gosxRuntimeRequest(url, request);
      if (!response.ok) {
        throw new Error("navigation fetch failed with status " + response.status);
      }
      const responseURL = response.url || key;
      if (!isSameOriginNavigation(responseURL, windowLocationHref())) {
        throw new Error("blocked cross-origin navigation response");
      }
      return {
        html: await response.text(),
        url: responseURL,
        fetchID: fetchID,
      };
    })();

    pageCache.set(key, request);
    request.__gosxCachedAt = Date.now();
    try {
      const page = await request;
      if (pageCacheOptsOut(page.html) && pageCache.get(key) === request) {
        pageCache.delete(key);
      }
      return page;
    } catch (err) {
      if (pageCache.get(key) === request) pageCache.delete(key);
      throw err;
    }
  }

  function prefetchLink(anchor, trigger) {
    if (!anchor || !anchor.getAttribute) return Promise.resolve(false);
    if (!shouldPrefetchLink(anchor, trigger || "intent")) return Promise.resolve(false);
    const url = navigationURLParts(anchor.getAttribute("href"));
    if (!url) return Promise.resolve(false);
    anchor.setAttribute(LINK_PREFETCH_STATE_ATTR, "pending");
    return fetchPage(url.href).then(function() {
      anchor.setAttribute(LINK_PREFETCH_STATE_ATTR, "ready");
      return true;
    }).catch(function() {
      anchor.setAttribute(LINK_PREFETCH_STATE_ATTR, "error");
      return false;
    });
  }

  function navigationIsCurrent(sequence) {
    return sequence === navigationSequence;
  }

  function navigationAbortError(error) {
    return !!error && String(error.name || "") === "AbortError";
  }

  async function navigate(url, options) {
    const opts = options || {};
    const target = navigationURLParts(url);
    if (!isHTTPNavigationURL(target)) {
      throw new Error("blocked unsafe navigation URL");
    }
    const current = navigationURLParts(windowLocationHref());
    if (!isHTTPNavigationURL(current) || target.origin !== current.origin) {
      ++navigationSequence;
      if (activeNavigationController && typeof activeNavigationController.abort === "function") {
        activeNavigationController.abort();
      }
      activeNavigationController = null;
      activeNavigationURL = "";
      hardNavigate(target.href, !!opts.replace);
      return true;
    }
    const sequence = ++navigationSequence;
    const sharesPendingFetch = !opts.mutationBarrier
      && navigationState.phase === "pending"
      && activeNavigationURL === target.href;
    if (!sharesPendingFetch) {
      if (activeNavigationController && typeof activeNavigationController.abort === "function") {
        activeNavigationController.abort();
      }
      if (opts.revalidate) pageCache.delete(target.href);
    }
    if (!sharesPendingFetch && !opts.force && sameNavigationURL(target, currentNavigationURL())) {
      activeNavigationController = null;
      activeNavigationURL = "";
      setNavigationState({
        phase: "idle",
        currentURL: target.href,
        pendingURL: "",
      }, "navigate:current");
      observeNavigation("debug", "navigation already current", { url: target.href });
      finalizeNavigation(target.href, opts, resolveNavigationA11y(target.href));
      return true;
    }
    if (!sharesPendingFetch) {
      activeNavigationController = typeof AbortController === "function" ? new AbortController() : null;
      activeNavigationURL = target.href;
    }
    const signal = activeNavigationController ? activeNavigationController.signal : null;
    startNavigation(target.href);
    try {
      const page = await resolveNavigationPage(target.href, signal);
      if (!navigationIsCurrent(sequence)) {
        observeNavigation("debug", "navigation superseded", { url: String(url || "") });
        return false;
      }
      await applyNavigatedPage(
        page.nextDoc,
        page.nextURL,
        !!opts.replace,
        () => navigationIsCurrent(sequence),
      );
      if (!navigationIsCurrent(sequence)) {
        observeNavigation("debug", "navigation superseded", { url: String(url || "") });
        return false;
      }
      completeNavigation(page.nextURL);
      if (page.fetchID > navigationFetchApplied) navigationFetchApplied = page.fetchID;
      finalizeNavigation(page.nextURL, opts, resolveNavigationA11y(page.nextURL));
      return true;
    } catch (err) {
      if (!navigationIsCurrent(sequence) || navigationAbortError(err)) return false;
      failNavigation(err, url);
      throw err;
    } finally {
      if (navigationIsCurrent(sequence)) {
        activeNavigationController = null;
        activeNavigationURL = "";
      }
    }
  }

  function startNavigation(url) {
    setNavigationState({
      phase: "pending",
      pendingURL: String(url || ""),
    }, "navigate:start");
    observeNavigation("info", "navigation started", { url: String(url || "") });
  }

  function completeNavigation(url) {
    setNavigationState({
      phase: "idle",
      currentURL: String(url),
      pendingURL: "",
    }, "navigate:complete");
    observeNavigation("info", "navigation completed", { url: String(url || "") });
    sendNavigationBeacons(url);
    prefetchManagedLinks("render");
  }

  function failNavigation(error, url) {
    setNavigationState({
      phase: "idle",
      pendingURL: "",
    }, "navigate:error");
    reportNavigationFailure("navigation", error, {
      source: String(url || ""),
      telemetry: { url: String(url || "") },
    });
    observeNavigation("error", "navigation failed", {});
  }

  async function resolveNavigationPage(url, signal) {
    const page = await fetchPage(url, signal, true);
    const nextURL = page.url || url;
    return {
      nextURL: nextURL,
      nextDoc: parseDocument(page.html),
      fetchID: Number(page.fetchID || 0),
    };
  }

  // reusableEngineIDs asks the mounted runtime (client/js/bootstrap-src/30-tail.js,
  // window.__gosx_reusable_engines) which currently-mounted engines are safe
  // to carry across this navigation instead of disposing and remounting —
  // same component, same mountId, byte-identical serialized scene props. See
  // that function's doc comment for the full (deliberately conservative)
  // rule. Absent in older bootstrap bundles or non-Scene3D pages, in which
  // case navigation behaves exactly as before (dispose + remount).
  function reusableEngineIDs(nextDoc) {
    if (typeof window.__gosx_reusable_engines !== "function") {
      return new Set();
    }
    try {
      const ids = window.__gosx_reusable_engines(nextDoc);
      return ids instanceof Set ? ids : new Set();
    } catch (_e) {
      return new Set();
    }
  }

  async function applyNavigatedPage(nextDoc, nextURL, replace, isCurrent) {
    if (isCurrent && !isCurrent()) return;
    // Computed BEFORE disposal — it compares the OUTGOING (still-live)
    // engines against the INCOMING manifest parsed from nextDoc.
    const reuseIDs = reusableEngineIDs(nextDoc);
    await disposeCurrentPage(reuseIDs);
    if (isCurrent && !isCurrent()) return;
    // Head/body replacement adopts nodes out of the parsed document. Capture
    // managed scripts first so head-owned patch/lifecycle chunks are not lost
    // before the ordered loader sees them.
    const managedScripts = collectManagedScripts(nextDoc.head, nextURL)
      .concat(collectManagedScripts(nextDoc.body, nextURL));
    await replaceManagedHead(nextDoc, nextURL);
    if (isCurrent && !isCurrent()) return;
    replaceBody(nextDoc, nextURL, reuseIDs);
    updateHistory(nextURL, !!replace);
    // Pre-bootstrap replays run before the managed scene chunks load and
    // before bootstrap consumes the manifest. A manifest-rewriting boot
    // script therefore observes the same ordering a full page load gives it.
    replayInlineNavigationScripts(document.body, "pre-bootstrap");
    if (isCurrent && !isCurrent()) return;
    const bootstrapLoadedNow = await ensureManagedScripts(nextDoc, nextURL, managedScripts);
    if (isCurrent && !isCurrent()) return;
    await bootstrapCurrentPage(bootstrapLoadedNow, reuseIDs);
    if (isCurrent && !isCurrent()) return;
    replayInlineNavigationScripts(document.body, "post");
  }

  function applyNavigationScroll(a11y, preserveScroll) {
    if (preserveScroll) {
      return;
    }
    if (a11y.hashTarget && typeof a11y.hashTarget.scrollIntoView === "function") {
      a11y.hashTarget.scrollIntoView({ behavior: "instant" });
    } else if (typeof window.scrollTo === "function") {
      window.scrollTo({ top: 0, left: 0, behavior: "instant" });
    }
  }

  function dispatchNavigate(url, replace, announcement, focusTarget) {
    dispatchManagedEvent("gosx:navigate", {
      detail: {
        announcement: announcement,
        focusTargetId: focusTarget && focusTarget.getAttribute ? (focusTarget.getAttribute("id") || "") : "",
        url: url,
        replace: !!replace,
      },
    });
  }

  function finalizeNavigation(url, options, a11y) {
    const opts = options || {};
    applyNavigationScroll(a11y, !!opts.preserveScroll);
    focusElement(a11y.focusTarget, true);
    const announcement = announceNavigation(a11y.announcement);
    dispatchNavigate(url, opts.replace, announcement, a11y.focusTarget);
    // Runs after every soft navigation this function completes, whether it
    // fetched a new document or reconciled the already-current page — see
    // setupPageRevalidation's doc comment.
    setupPageRevalidation();
    // setupPageHeartbeat (gosx#216) follows the exact same rescan lifecycle
    // — see its own doc comment above.
    setupPageHeartbeat();
    setupPageCountdowns();
    // setupPageWatchers (gosx#214) and setupPageFilters (gosx#215) both
    // follow the exact same rescan lifecycle — see their own doc comments
    // above.
    setupPageWatchers();
    // setupLiveRegions (gosx#217) follows the exact same rescan lifecycle,
    // generalized to many independently-timed roots — see its own doc
    // comment above.
    setupLiveRegions();
    setupPageFilters();
  }

  // documentIsHidden prefers the real, read-only document.hidden a browser
  // provides. Test doubles (and any host that only tracks visibilityState)
  // fall back to the same "hidden" comparison mount-viewport.ts already uses
  // for its own pageVisible fallback.
  function documentIsHidden() {
    if (typeof document.hidden === "boolean") {
      return document.hidden;
    }
    return String(document.visibilityState || "visible").toLowerCase() === "hidden";
  }

  function focusedControlBlocksRevalidation() {
    const active = document.activeElement;
    if (!active) return false;
    switch (String(active.tagName || "").toUpperCase()) {
      case "INPUT":
      case "TEXTAREA":
      case "SELECT":
        return true;
      default:
        return false;
    }
  }

  // Reuses the runtime's own in-flight state: navigationState.phase covers a
  // soft navigation (including a GET form, which navigates) and
  // pendingManagedForms covers a POST/action form submission, which fetches
  // outside the navigate() path.
  function navigationOrFormSubmissionInFlight() {
    return navigationState.phase === "pending" || pendingManagedForms.size > 0;
  }

  // revalidateSuspendCount backs suspendRevalidation (gosx#212): an active
  // reorder drag holds one of these for its whole gesture (pointerdown/grab
  // through drop, cancel, or pointercancel) so a periodic-revalidation DOM
  // swap can never land mid-drag and pull the dragged element out from under
  // the user's pointer or focus. It is a count, not a flag, so two callers
  // that both need revalidation held off (unlikely today — only one reorder
  // gesture can be active at a time — but a real future caller might exist)
  // compose instead of one release accidentally waking the tick for the
  // other.
  let revalidateSuspendCount = 0;

  function revalidationSuspended() {
    return revalidateSuspendCount > 0;
  }

  // suspendRevalidation is the ONLY supported way to hold off periodic
  // revalidation from outside this file — it is deliberately not exposed on
  // navigationAPI: every current caller (the reorder section below) lives in
  // this same closure and can call it directly. Returns a release function;
  // call it exactly once when the interaction that needed quiet ends. A
  // leaked suspension (a release that is never called) disables periodic
  // revalidation for the rest of the page's life, so every caller releases
  // from a finally-equivalent cleanup path, never only from the success path.
  function suspendRevalidation() {
    revalidateSuspendCount += 1;
    let released = false;
    return function releaseRevalidation() {
      if (released) return;
      released = true;
      revalidateSuspendCount = Math.max(0, revalidateSuspendCount - 1);
    };
  }

  // A JS timer's delay is a 32-bit signed int internally; a larger value
  // does not error, it just fires almost immediately (or never, depending
  // on the engine) instead of after the requested delay. Reject a
  // declarative interval that would exceed this bound outright, the same
  // way any other malformed value is rejected below.
  const REVALIDATE_MAX_INTERVAL_MS = 2147483647;
  // A poll slower than this is a config mistake, not a real requirement —
  // clamp instead of rejecting outright so the page still revalidates.
  const REVALIDATE_INTERVAL_CLAMP_MS = 60 * 60 * 1000;

  // parseRevalidateInterval accepts only whole-second or whole-minute
  // Go-style duration literals ("4s", "90s", "2m") — this is a small
  // declarative subset, not a general Go duration parser. Anything else, a
  // duration under one second, or a duration past the 32-bit timer bound,
  // is invalid.
  function parseRevalidateInterval(value) {
    const trimmed = String(value == null ? "" : value).trim();
    const match = /^([0-9]+)(s|m)$/.exec(trimmed);
    if (!match) return null;
    const amount = Number(match[1]);
    if (!Number.isFinite(amount) || amount <= 0) return null;
    const ms = match[2] === "m" ? amount * 60000 : amount * 1000;
    if (ms < 1000 || ms > REVALIDATE_MAX_INTERVAL_MS) return null;
    return ms;
  }

  function findRevalidateElement() {
    return findElement(document.body, function(node) {
      return node.hasAttribute && node.hasAttribute(REVALIDATE_INTERVAL_ATTR);
    });
  }

  function teardownPageRevalidation() {
    if (revalidateTimerHandle != null) {
      clearInterval(revalidateTimerHandle);
    }
    revalidateTimerHandle = null;
    revalidateSrc = "";
    revalidateHasBaseline = false;
    revalidateLastBody = null;
    revalidateIntervalMs = 0;
    revalidateHiddenSince = null;
  }

  function triggerPeriodicRevalidation() {
    revalidateNavigation().catch(function(error) {
      reportNavigationFailure("periodic revalidation", error, {
        source: revalidateSrc || windowLocationHref(),
      });
    });
  }

  async function pollRevalidateSrc() {
    // Skip if the previous poll's fetch/text() awaits have not settled yet
    // — an overlapping request in flight for the same src answers nothing
    // an already-pending one won't, and would race it for revalidateLastBody.
    if (revalidatePollInFlight) {
      return;
    }
    revalidatePollInFlight = true;
    const generation = revalidateGeneration;
    try {
      let response;
      try {
        response = await gosxRuntimeRequest(revalidateSrc, {
          headers: { Accept: "application/json" },
          cache: "no-store",
        });
      } catch (_error) {
        return; // Fetch errors skip silently; the next tick tries again.
      }
      if (generation !== revalidateGeneration) {
        // Navigation moved on while this fetch was in flight. This response
        // belongs to whatever page was current when it started, not the one
        // current now — discard it rather than writing its baseline.
        return;
      }
      if (!response || !response.ok) {
        return;
      }
      let body;
      try {
        body = await response.text();
      } catch (_error) {
        return;
      }
      if (generation !== revalidateGeneration) {
        return;
      }
      if (!revalidateHasBaseline) {
        // The first successful poll only records the baseline — it never
        // triggers a revalidation on its own.
        revalidateHasBaseline = true;
        revalidateLastBody = body;
        return;
      }
      if (body === revalidateLastBody) {
        return;
      }
      revalidateLastBody = body;
      triggerPeriodicRevalidation();
    } finally {
      revalidatePollInFlight = false;
    }
  }

  function runRevalidateTick() {
    if (
      documentIsHidden()
      || focusedControlBlocksRevalidation()
      || navigationOrFormSubmissionInFlight()
      || revalidationSuspended()
    ) {
      return;
    }
    if (!revalidateSrc) {
      triggerPeriodicRevalidation();
      return;
    }
    pollRevalidateSrc();
  }

  // onRevalidateVisibilityChange runs a catch-up tick the moment the
  // document becomes visible again, if at least one full interval elapsed
  // while it was hidden — runRevalidateTick's own documentIsHidden() guard
  // otherwise means a page hidden for hours only revalidates once it is
  // looked at again, which can be long after its data went stale.
  function onRevalidateVisibilityChange() {
    if (documentIsHidden()) {
      if (revalidateHiddenSince == null) {
        revalidateHiddenSince = Date.now();
      }
      return;
    }
    const hiddenSince = revalidateHiddenSince;
    revalidateHiddenSince = null;
    if (hiddenSince == null || !revalidateIntervalMs) {
      return;
    }
    if (Date.now() - hiddenSince >= revalidateIntervalMs) {
      runRevalidateTick();
    }
  }

  // setupPageRevalidation scans for the FIRST element carrying
  // data-gosx-revalidate-interval on page boot and after every soft
  // navigation (see finalizeNavigation and the initial-document replay
  // below), tearing down any previous timer first so a page without the
  // attribute, or with new attribute values, always gets a fresh read.
  function setupPageRevalidation() {
    // Every call — page boot and every soft navigation — starts a new
    // generation, even one that ends up disabling or not finding periodic
    // revalidation. See revalidateGeneration's declaration for why.
    revalidateGeneration += 1;
    teardownPageRevalidation();
    const target = findRevalidateElement();
    if (!target) {
      return;
    }

    const rawInterval = target.getAttribute(REVALIDATE_INTERVAL_ATTR);
    let intervalMs = parseRevalidateInterval(rawInterval);
    if (intervalMs == null) {
      console.warn(
        "[gosx] invalid " + REVALIDATE_INTERVAL_ATTR + " value "
        + JSON.stringify(String(rawInterval || "")) + "; periodic revalidation is disabled for this page",
      );
      return;
    }
    if (intervalMs > REVALIDATE_INTERVAL_CLAMP_MS) {
      console.warn(
        "[gosx] " + REVALIDATE_INTERVAL_ATTR + " value " + JSON.stringify(String(rawInterval || ""))
        + " exceeds the 1 hour maximum for periodic revalidation; clamping to 1 hour",
      );
      intervalMs = REVALIDATE_INTERVAL_CLAMP_MS;
    }

    const rawSrc = target.hasAttribute(REVALIDATE_SRC_ATTR) ? target.getAttribute(REVALIDATE_SRC_ATTR) : "";
    if (rawSrc) {
      if (!isSameOriginNavigation(rawSrc, windowLocationHref())) {
        console.warn(
          "[gosx] " + REVALIDATE_SRC_ATTR + " must be same-origin: " + JSON.stringify(String(rawSrc))
          + "; periodic revalidation is disabled for this page",
        );
        return;
      }
      const parsedSrc = navigationURLParts(rawSrc);
      revalidateSrc = parsedSrc ? parsedSrc.href : "";
    }

    revalidateIntervalMs = intervalMs;
    revalidateTimerHandle = setInterval(runRevalidateTick, intervalMs);
  }

  // ---------------------------------------------------------------------
  // Visibility-aware heartbeat ping (data-gosx-heartbeat, gosx#216)
  //
  // HEARTBEAT_ATTR, on an element (or on <body> itself — findElement below
  // walks the root it is given too, so body qualifies with no special
  // case), names a same-origin endpoint the runtime pings with a plain GET
  // while the document is visible. HEARTBEAT_INTERVAL_ATTR, alongside it
  // on the same element, is the ping period in the same small duration
  // grammar data-gosx-revalidate-interval accepts (parseRevalidateInterval
  // above): a bare whole-second or whole-minute literal ("30s", "2m").
  // Anything else, under one second, or past the 32-bit timer bound leaves
  // the heartbeat disabled with one console.warn — the same fail-closed
  // shape every other declarative attribute in this file uses for a value
  // it cannot parse at all. Unlike data-gosx-revalidate-interval, an
  // over-long value is not clamped to a 1-hour ceiling: a heartbeat GET is
  // a cheap, single small request, not a full-page re-render, so there is
  // no equivalent cost runaway to guard against beyond the shared parser's
  // own 32-bit timer bound.
  //
  // The lifecycle mirrors periodic revalidation immediately above almost
  // exactly: paused entirely while the document is hidden
  // (runHeartbeatTick's own documentIsHidden() guard), one immediate
  // catch-up ping on visibility return if at least one full interval
  // elapsed while hidden (onHeartbeatVisibilityChange, mirroring
  // onRevalidateVisibilityChange), and never more than one ping in flight
  // at a time — an interval tick or a catch-up that lands while the
  // previous ping's fetch has not settled is skipped outright rather than
  // queued or raced (heartbeatPingInFlight, mirroring
  // revalidatePollInFlight). Unlike revalidation, a ping never mutates the
  // page: this is presence detection, not content staleness, so its
  // response is read and discarded, and BOTH a network failure and a
  // non-2xx response are silent by contract — presence is a best-effort
  // signal a visitor's dropped connection or a transient server error must
  // never surface as a console error for.
  // ---------------------------------------------------------------------

  function findHeartbeatElement() {
    return findElement(document.body, function(node) {
      return node.hasAttribute && node.hasAttribute(HEARTBEAT_ATTR);
    });
  }

  function teardownPageHeartbeat() {
    if (heartbeatTimerHandle != null) {
      clearInterval(heartbeatTimerHandle);
    }
    heartbeatTimerHandle = null;
    heartbeatSrc = "";
    heartbeatIntervalMs = 0;
    heartbeatHiddenSince = null;
  }

  // pingHeartbeat is the whole GET: no headers, no body, credentials
  // "same-origin" so an authenticated session's cookies ride along the way
  // any other same-origin fetch on this page would. Both branches of the
  // settle — success or failure — only ever clear heartbeatPingInFlight,
  // and only for the generation that started this ping; see
  // heartbeatGeneration's own declaration for why a stale settle must not
  // touch state a later setupPageHeartbeat call now owns.
  function pingHeartbeat() {
    if (heartbeatPingInFlight) {
      return;
    }
    heartbeatPingInFlight = true;
    const generation = heartbeatGeneration;
    gosxRuntimeRequest(heartbeatSrc, {
      method: "GET",
      credentials: "same-origin",
      cache: "no-store",
    }).catch(function() {
      // Silent by contract — see this section's own doc comment above.
    }).then(function() {
      if (generation !== heartbeatGeneration) return;
      heartbeatPingInFlight = false;
    });
  }

  function runHeartbeatTick() {
    if (documentIsHidden()) {
      return;
    }
    pingHeartbeat();
  }

  // onHeartbeatVisibilityChange mirrors onRevalidateVisibilityChange above:
  // one immediate catch-up ping the moment the document becomes visible
  // again, if at least one full interval elapsed while it was hidden —
  // runHeartbeatTick's own documentIsHidden() guard otherwise means a tab
  // backgrounded for hours only pings once it is looked at again.
  function onHeartbeatVisibilityChange() {
    if (!heartbeatSrc) return;
    if (documentIsHidden()) {
      if (heartbeatHiddenSince == null) {
        heartbeatHiddenSince = Date.now();
      }
      return;
    }
    const hiddenSince = heartbeatHiddenSince;
    heartbeatHiddenSince = null;
    if (hiddenSince == null || !heartbeatIntervalMs) {
      return;
    }
    if (Date.now() - hiddenSince >= heartbeatIntervalMs) {
      pingHeartbeat();
    }
  }

  // setupPageHeartbeat scans for the FIRST element carrying HEARTBEAT_ATTR
  // on page boot and after every soft navigation (see finalizeNavigation
  // and the initial-document replay below) — the same lifecycle
  // setupPageRevalidation follows just above, tearing down any previous
  // timer first so a page without the attribute, or with new attribute
  // values, always gets a fresh read.
  function setupPageHeartbeat() {
    heartbeatGeneration += 1;
    teardownPageHeartbeat();
    const target = findHeartbeatElement();
    if (!target) {
      return;
    }
    const rawSrc = target.getAttribute(HEARTBEAT_ATTR);
    if (!rawSrc || !isSameOriginNavigation(rawSrc, windowLocationHref())) {
      console.warn(
        "[gosx] " + HEARTBEAT_ATTR + " must be a same-origin URL: " + JSON.stringify(String(rawSrc || ""))
        + "; the heartbeat is disabled for this page",
      );
      return;
    }
    const parsedSrc = navigationURLParts(rawSrc);
    if (!parsedSrc) {
      return;
    }
    const rawInterval = target.getAttribute(HEARTBEAT_INTERVAL_ATTR);
    const intervalMs = parseRevalidateInterval(rawInterval);
    if (intervalMs == null) {
      console.warn(
        "[gosx] invalid " + HEARTBEAT_INTERVAL_ATTR + " value " + JSON.stringify(String(rawInterval || ""))
        + "; the heartbeat is disabled for this page",
      );
      return;
    }
    heartbeatSrc = parsedSrc.href;
    heartbeatIntervalMs = intervalMs;
    heartbeatTimerHandle = setInterval(runHeartbeatTick, intervalMs);
  }

  // ---------------------------------------------------------------------
  // Shared synthesized audio cues (data-gosx-countdown-cue and
  // data-gosx-watch's "cue" effect, gosx#213 / gosx#214)
  //
  // One AudioContext for the whole runtime, constructed lazily on the
  // page's first user gesture — a pointerdown or a keydown, whichever
  // comes first, through the once-listeners registered near the bottom of
  // this file — and never before, so construction never races a browser's
  // autoplay gate with nothing to resume it. Both data-gosx-countdown-cue
  // below and data-gosx-watch's "cue" effect (see "Attention watcher")
  // call the same playCountdownCue entry point against the same context
  // and the same fixed, tiny tone vocabulary; there is exactly one audio
  // subsystem here, not two kept in sync by hand.
  //
  // This is deliberately independent of window.__gosx.arcadeAudio
  // (client/js/bootstrap-src/30c2-tail-arcade-audio.ts): arcadeAudio ships
  // only inside the islands/hubs bootstrap bundle, while this file is the
  // always-on navigation runtime every page loads — a plain countdown-only
  // page must get its cues without depending on a bundle it may never
  // load.
  // ---------------------------------------------------------------------

  const AUDIO_CUE_NAMES = { beep: true, chime: true };
  const AUDIO_CUE_DEBUG_LOG_LIMIT = 200;
  let audioCueContext = null;
  // audioCueDebugLog is the small, honest test hook the runtime exposes
  // for proving a cue actually fired instead of a test only trusting that
  // playCountdownCue was CALLED: window.__gosx.navigation.debugCueLog()
  // returns a copy of every {cue, at} entry recorded the moment a named
  // tone was actually scheduled (never for a call that no-opped for want
  // of a primed context or an unrecognized name). Capped at
  // AUDIO_CUE_DEBUG_LOG_LIMIT entries, oldest dropped first, the same
  // bounded-growth shape missingPatchPathWarnings uses in patch.ts.
  const audioCueDebugLog = [];

  function audioCueContextConstructor() {
    return (typeof window !== "undefined" && (window.AudioContext || window.webkitAudioContext)) || null;
  }

  // primeAudioCueContext is the pointerdown/keydown once-listener body. It
  // is the ONLY place that ever constructs audioCueContext — playCountdownCue
  // below never does — so a cue threshold crossed before the visitor's
  // first gesture stays silent instead of constructing a context nothing
  // has unlocked yet.
  function primeAudioCueContext() {
    if (audioCueContext) {
      resumeAudioCueContextIfSuspended();
      return;
    }
    const Ctor = audioCueContextConstructor();
    if (!Ctor) return;
    try {
      audioCueContext = new Ctor();
    } catch (_e) {
      audioCueContext = null;
      return;
    }
    resumeAudioCueContextIfSuspended();
  }

  // resumeAudioCueContextIfSuspended is best-effort and fire-and-forget:
  // AudioContext#resume() returns a promise this runtime never awaits, so
  // a call site right after this can still observe state === "suspended"
  // for one more tick even on a successful resume. playCountdownCue below
  // schedules its tone regardless — a still-suspended context safely
  // queues a scheduled node in every browser gosx targets, and a context a
  // browser has permanently blocked drops the audio there, not here.
  function resumeAudioCueContextIfSuspended() {
    if (!audioCueContext) return;
    if (audioCueContext.state === "suspended" && typeof audioCueContext.resume === "function") {
      try {
        audioCueContext.resume();
      } catch (_e) {
        // Best-effort: a later gesture or cue attempt tries again.
      }
    }
  }

  function recordAudioCueDebug(name) {
    audioCueDebugLog.push({ cue: name, at: Date.now() });
    if (audioCueDebugLog.length > AUDIO_CUE_DEBUG_LOG_LIMIT) {
      audioCueDebugLog.shift();
    }
  }

  // scheduleAudioCueTone plays one sine tone with a short linear gain
  // envelope — a 5ms attack, then a decay to silence by the tone's own end
  // — so it starts and stops with no audible click. startOffset is
  // seconds from now, on the SAME context.currentTime base every tone in
  // one playNamedCue call shares, so a multi-tone cue's notes land in the
  // sequence its own definition below describes.
  function scheduleAudioCueTone(context, frequency, startOffset, duration) {
    const oscillator = context.createOscillator();
    const gain = context.createGain();
    oscillator.type = "sine";
    const startAt = context.currentTime + startOffset;
    if (oscillator.frequency && typeof oscillator.frequency.setValueAtTime === "function") {
      oscillator.frequency.setValueAtTime(frequency, startAt);
    } else if (oscillator.frequency) {
      oscillator.frequency.value = frequency;
    }
    const peakAt = startAt + 0.005;
    const endAt = startAt + duration;
    if (gain.gain && typeof gain.gain.setValueAtTime === "function") {
      gain.gain.setValueAtTime(0.0001, startAt);
      gain.gain.linearRampToValueAtTime(0.25, peakAt);
      gain.gain.linearRampToValueAtTime(0.0001, endAt);
    } else if (gain.gain) {
      gain.gain.value = 0.25;
    }
    oscillator.connect(gain);
    gain.connect(context.destination);
    oscillator.start(startAt);
    oscillator.stop(endAt + 0.02);
  }

  // playNamedCue holds the fixed, tiny synthesized tone vocabulary
  // data-gosx-countdown-cue and data-gosx-watch's "cue" effect both draw
  // from (gosx#213 / gosx#214): "beep" is one short tone; "chime" is two,
  // a rising fifth (660Hz then 990Hz), for a friendlier two-note alert.
  // Every duration and frequency here is deliberately hard-coded — this is
  // not a general sound design API, just the two names the two
  // declarative attributes need. Returns whether name was recognized (and
  // therefore actually scheduled), so playCountdownCue below only logs a
  // debug entry for a cue that really played.
  function playNamedCue(context, name) {
    if (name === "beep") {
      scheduleAudioCueTone(context, 880, 0, 0.14);
      return true;
    }
    if (name === "chime") {
      scheduleAudioCueTone(context, 660, 0, 0.12);
      scheduleAudioCueTone(context, 990, 0.11, 0.16);
      return true;
    }
    return false;
  }

  // playCountdownCue is the single entry point data-gosx-countdown-cue
  // (below) and data-gosx-watch's "cue" effect (see "Attention watcher")
  // both call. It is a silent no-op — never a thrown error, never a
  // console warning — when no AudioContext has been primed yet, when
  // construction failed, or when this browser exposes no AudioContext at
  // all: a page nobody has clicked or typed into yet is the expected
  // common case for a countdown or a watcher whose threshold or condition
  // is reached before the visitor's first gesture, not a bug to report.
  function playCountdownCue(name) {
    if (!audioCueContext) return;
    resumeAudioCueContextIfSuspended();
    if (typeof audioCueContext.createOscillator !== "function" || typeof audioCueContext.createGain !== "function") return;
    let played = false;
    try {
      played = playNamedCue(audioCueContext, name);
    } catch (_e) {
      played = false;
    }
    if (played) {
      recordAudioCueDebug(name);
    }
  }

  // ---------------------------------------------------------------------
  // Declarative countdown (data-gosx-countdown, gosx#178)
  //
  // The author writes the element's initial text (compact) or each
  // segment's initial value directly, so the page already shows a
  // correct value with no JavaScript at all — see the runtime guide's
  // declarative countdown section for the recipe that computes that
  // initial text from the server's own clock in the page loader. This
  // block only keeps the value moving from there: one shared 1-second
  // timer drives every countdown root on the page at once,
  // generation-guarded across navigations the same way setupPageRevalidation
  // guards revalidateGeneration above. A root is left exactly as it was
  // written until the first tick fires one second later — setup never
  // blanks it or writes NaN text.
  // ---------------------------------------------------------------------

  // daysFromCivil converts a proleptic-Gregorian calendar date to a day
  // count since the Unix epoch (Howard Hinnant's days_from_civil
  // algorithm). parseCountdownInstant uses this instead of `new Date(...)`
  // because a test's installManualClock replaces the global Date with a
  // now()-only double — the countdown timer must still parse instants
  // under that double the same way every other clock read in this file
  // only ever calls Date.now().
  //
  // The reference algorithm special-cases a negative `y` (era = floor((y
  // >= 0 ? y : y - 399) / 400)) to work around C++ integer division
  // truncating toward zero. JavaScript's Math.floor already floors
  // negative numbers correctly, and COUNTDOWN_INSTANT_RE's year group is
  // exactly four digits — the only negative `y` that expression can ever
  // reach is -1 (year "0000" with month <= 2) — so the special case is a
  // no-op here: Math.floor(y / 400) alone gives the identical result for
  // every reachable input. Verified against the reference formula across
  // the full reachable range with no divergence.
  function daysFromCivil(year, month, day) {
    const y = month <= 2 ? year - 1 : year;
    const era = Math.floor(y / 400);
    const yoe = y - era * 400;
    const doy = Math.floor((153 * (month + (month > 2 ? -3 : 9)) + 2) / 5) + day - 1;
    const doe = yoe * 365 + Math.floor(yoe / 4) - Math.floor(yoe / 100) + doy;
    return era * 146097 + doe - 719468;
  }

  // daysInMonth returns how many days `month` (1-12) has in `year`,
  // derived from daysFromCivil itself — the gap between the first day of
  // `month` and the first day of the next one — instead of a separate
  // 28/29/30/31 table with its own leap-year rule to keep in sync with the
  // civil math above.
  function daysInMonth(year, month) {
    const nextMonth = month === 12 ? 1 : month + 1;
    const nextYear = month === 12 ? year + 1 : year;
    return daysFromCivil(nextYear, nextMonth, 1) - daysFromCivil(year, month, 1);
  }

  // No `i` flag and no trimming: Go's time.Parse(time.RFC3339, ...) is
  // case-sensitive ("T" and "Z" only, never "t"/"z") and rejects leading
  // or trailing whitespace. gosx#178 review finding m13 — the runtime must
  // fail closed on exactly the same strings the check-time validator
  // (ir/validate.go) and Go's own parser do, not a superset of them.
  const COUNTDOWN_INSTANT_RE =
    /^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$/;

  // parseCountdownInstant returns the target instant in epoch
  // milliseconds, or null for anything that is not a valid RFC3339
  // instant. A static value this rejects never reaches the browser —
  // `gosx check` fails closed on it (see ir/validate.go) — so null here
  // means either a dynamic expression value that evaluated to a bad
  // string at render time, or hand-authored markup that bypassed gosx
  // rendering entirely.
  //
  // The day and zone-offset bounds below (gosx#178 review finding M5)
  // mirror exactly what Go's time.Parse(time.RFC3339, ...) accepts: a day
  // bounded by the actual length of its month (leap years included, via
  // daysInMonth above), and a zone offset whose hour is at most 24 and
  // whose minute is at most 60 — Go's own zone-offset parser uses `>` (not
  // `>=`) against those two figures, so e.g. "+24:00" is accepted by both
  // sides and "+99:99" is rejected by both. Differential-tested against
  // time.Parse across a 531-case corpus (every month/day boundary across
  // eight years, every offset edge case, every case/whitespace variant):
  // zero acceptance or value mismatches beyond the documented sub-millisecond
  // fraction rounding.
  function parseCountdownInstant(value) {
    const trimmed = String(value == null ? "" : value);
    const match = COUNTDOWN_INSTANT_RE.exec(trimmed);
    if (!match) return null;
    const year = Number(match[1]);
    const month = Number(match[2]);
    const day = Number(match[3]);
    const hour = Number(match[4]);
    const minute = Number(match[5]);
    const second = Number(match[6]);
    if (month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) || hour > 23 || minute > 59 || second > 59) {
      return null;
    }
    const fraction = match[7] ? Number("0" + match[7]) : 0;
    let offsetMinutes = 0;
    const zone = match[8];
    if (zone !== "Z") {
      const sign = zone[0] === "-" ? -1 : 1;
      const offsetHour = Number(zone.slice(1, 3));
      const offsetMinute = Number(zone.slice(4, 6));
      if (offsetHour > 24 || offsetMinute > 60) {
        return null;
      }
      offsetMinutes = sign * (offsetHour * 60 + offsetMinute);
    }
    const days = daysFromCivil(year, month, day);
    const utcSeconds = days * 86400 + hour * 3600 + minute * 60 + second - offsetMinutes * 60;
    const ms = utcSeconds * 1000 + Math.round(fraction * 1000);
    return Number.isFinite(ms) ? ms : null;
  }

  // parseCountdownThresholdSeconds accepts the same small Go-style
  // duration subset parseRevalidateInterval does above (whole-number
  // amounts), extended to combine hour/minute/second components in one
  // value ("1m30s") and to accept a bare integer as whole seconds ("30"),
  // per gosx#178. Named for what it now parses generically — one
  // threshold half of a data-gosx-countdown-warn or data-gosx-countdown-cue
  // pair (gosx#213) — rather than only the warn attribute, which used to
  // be its only caller.
  function parseCountdownThresholdSeconds(value) {
    const trimmed = String(value == null ? "" : value).trim();
    if (!trimmed) return null;
    if (/^[0-9]+$/.test(trimmed)) {
      return Number(trimmed);
    }
    const match = /^(?:([0-9]+)h)?(?:([0-9]+)m)?(?:([0-9]+)s)?$/.exec(trimmed);
    if (!match || (!match[1] && !match[2] && !match[3])) {
      return null;
    }
    return Number(match[1] || 0) * 3600 + Number(match[2] || 0) * 60 + Number(match[3] || 0);
  }

  // isValidCountdownWarnClassToken accepts any non-empty, whitespace-free
  // token as a CSS class name for data-gosx-countdown-warn (gosx#213):
  // this is not a full CSS identifier grammar, just enough to reject a
  // token that could never work as one attribute-value class (embedded
  // whitespace would silently become two class names, or none, once
  // written through setAttribute("class", ...)).
  function isValidCountdownWarnClassToken(token) {
    return !/\s/.test(token);
  }

  // isValidCountdownCueToken accepts only a name from the fixed
  // synthesized tone vocabulary (see "Shared synthesized audio cues"
  // above) for data-gosx-countdown-cue (gosx#213).
  function isValidCountdownCueToken(token) {
    return Object.prototype.hasOwnProperty.call(AUDIO_CUE_NAMES, token);
  }

  // parseCountdownTierPairs parses the shared "threshold:token[,threshold:
  // token]..." grammar data-gosx-countdown-warn and data-gosx-countdown-cue
  // both use (gosx#213): a comma-separated list of pairs, each a threshold
  // (parseCountdownThresholdSeconds' small duration subset) and a token
  // the caller's own isValidToken predicate accepts — a CSS class for
  // -warn, a fixed cue name for -cue.
  //
  // Returns null — not a partial list — the instant ANY pair in the value
  // fails to parse. Partial application of a tier ladder (some thresholds
  // live, one silently dropped) is a worse silent surprise than dropping
  // the whole declaration and warning once: the same fail-closed choice
  // buildCountdownState already makes for every other countdown attribute
  // below.
  function parseCountdownTierPairs(value, isValidToken) {
    const trimmed = String(value == null ? "" : value).trim();
    if (!trimmed) return null;
    const tiers = [];
    for (const rawPair of trimmed.split(",")) {
      const pair = rawPair.trim();
      const splitAt = pair.indexOf(":");
      if (splitAt <= 0 || splitAt === pair.length - 1) return null;
      const seconds = parseCountdownThresholdSeconds(pair.slice(0, splitAt));
      const token = pair.slice(splitAt + 1).trim();
      if (seconds == null || !token || !isValidToken(token)) return null;
      tiers.push({ seconds: seconds, token: token });
    }
    return tiers;
  }

  function countdownPad2(n) {
    return String(n).padStart(2, "0");
  }

  function countdownComponents(totalSeconds) {
    const clamped = Math.max(0, totalSeconds);
    return {
      days: Math.floor(clamped / 86400),
      hours: Math.floor((clamped % 86400) / 3600),
      minutes: Math.floor((clamped % 3600) / 60),
      seconds: Math.floor(clamped % 60),
    };
  }

  function formatCountdownDHMS(totalSeconds) {
    const c = countdownComponents(totalSeconds);
    return c.days + "d " + countdownPad2(c.hours) + ":" + countdownPad2(c.minutes) + ":" + countdownPad2(c.seconds);
  }

  function formatCountdownMMSS(totalSeconds) {
    const clamped = Math.max(0, totalSeconds);
    const minutes = Math.floor(clamped / 60);
    const seconds = Math.floor(clamped % 60);
    return minutes + ":" + countdownPad2(seconds);
  }

  function elementClassNames(el) {
    return String((el.getAttribute && el.getAttribute("class")) || "").split(/\s+/).filter(Boolean);
  }

  // setElementClassActive adds or removes one named class by editing the
  // class attribute directly rather than through element.classList —
  // matching every other DOM-attribute read/write in this file, and
  // keeping the toggle testable through the same getAttribute/setAttribute
  // surface the rest of the runtime's test doubles already implement.
  // Shared by data-gosx-countdown-warn's tiers below and data-gosx-watch's
  // "class" effect (gosx#213 / gosx#214) — both are the same "toggle one
  // class by name on some element" operation.
  //
  // It compares against the CURRENT state before writing anything: most
  // calls pass the same `active` value as the call before (a countdown
  // tick re-asserting a tier that has not changed, or a watcher rescan
  // re-asserting a still-true condition), and a real DOM write on every
  // one of them would both burn a style/layout recalculation for nothing
  // and re-serialize the class attribute (author whitespace collapsed to
  // single spaces) even though nothing changed. A real toggle still
  // normalizes the attribute the same way it always did.
  function setElementClassActive(el, className, active) {
    if (!el || typeof el.getAttribute !== "function" || typeof el.setAttribute !== "function") return;
    const classes = elementClassNames(el);
    const idx = classes.indexOf(className);
    const has = idx !== -1;
    if (active === has) {
      return;
    }
    if (active) {
      classes.push(className);
    } else {
      classes.splice(idx, 1);
    }
    if (classes.length) {
      el.setAttribute("class", classes.join(" "));
    } else if (typeof el.removeAttribute === "function") {
      el.removeAttribute("class");
    }
  }

  // findCountdownSegments collects every descendant of root carrying
  // data-gosx-countdown-segment, keyed by its segment name. The app owns
  // this markup entirely — the runtime only ever writes textContent on an
  // element already carrying the attribute, so anything else under root
  // (whitespace, labels, other children) is left untouched.
  //
  // The walk stops descending at a NESTED data-gosx-countdown root (gosx#178
  // review finding m9): that element owns its own descendants through its
  // own findCountdownRootElements/buildCountdownState call, so an outer
  // root's scan must never claim an inner root's segment children. This
  // uses its own recursive walk rather than the shared walkElements helper
  // because walkElements' `visit() === false` return aborts the ENTIRE
  // walk (used by findElement to stop at the first match); this needs the
  // narrower "skip this subtree, keep scanning its siblings" behavior
  // instead.
  //
  // `segments` is created with Object.create(null) and every lookup goes
  // through hasOwnProperty (gosx#178 review finding B2): a segment name is
  // untrusted, author-controlled attribute data, and a plain object
  // literal answers `segments["constructor"]` and `segments["toString"]`
  // truthy without ever calling .push — the old `if (segments[name])`
  // guard let a data-driven segment name reach `.push` on a function
  // value and throw, taking the whole navigation runtime's boot down with
  // it (see setupPageCountdowns' try/catch below for the second layer of
  // the same defense). A null-prototype object has no such inherited
  // properties to collide with, and the explicit hasOwnProperty check
  // means only the four names this object was seeded with can ever match,
  // even for a value like "__proto__" that behaves unusually as a plain
  // object's live key.
  function findCountdownSegments(root) {
    const segments = Object.create(null);
    for (const name of COUNTDOWN_SEGMENT_NAMES) {
      segments[name] = [];
    }
    collectCountdownSegments(root, segments);
    return segments;
  }

  function collectCountdownSegments(node, segments) {
    for (const child of toArray(node.childNodes)) {
      if (!child || child.nodeType !== 1) continue;
      if (child.hasAttribute && child.hasAttribute(COUNTDOWN_ATTR)) {
        continue;
      }
      if (child.hasAttribute && child.hasAttribute(COUNTDOWN_SEGMENT_ATTR)) {
        const name = child.getAttribute(COUNTDOWN_SEGMENT_ATTR);
        if (Object.prototype.hasOwnProperty.call(segments, name)) {
          segments[name].push(child);
        }
      }
      collectCountdownSegments(child, segments);
    }
  }

  function findCountdownRootElements() {
    const found = [];
    walkElements(document.body, function(node) {
      if (node.hasAttribute && node.hasAttribute(COUNTDOWN_ATTR)) {
        found.push(node);
      }
      return true;
    });
    return found;
  }

  // buildCountdownState turns one data-gosx-countdown element into the
  // internal record runCountdownTick reads every second, or returns null
  // for an invalid instant — the element is then left exactly as the
  // server rendered it, matching data-gosx-revalidate-interval's own
  // "disabled, not an error" handling of a bad declarative value.
  function buildCountdownState(root) {
    const rawInstant = root.getAttribute(COUNTDOWN_ATTR);
    const targetMs = parseCountdownInstant(rawInstant);
    if (targetMs == null) {
      console.warn(
        "[gosx] invalid " + COUNTDOWN_ATTR + " value " + JSON.stringify(String(rawInstant || ""))
        + "; this countdown is disabled",
      );
      return null;
    }

    const segments = findCountdownSegments(root);
    const hasSegments = COUNTDOWN_SEGMENT_NAMES.some(function(name) { return segments[name].length > 0; });

    let format = null;
    const hasFormatAttr = root.hasAttribute(COUNTDOWN_FORMAT_ATTR);
    if (!hasSegments) {
      const rawFormat = root.getAttribute(COUNTDOWN_FORMAT_ATTR);
      if (rawFormat === "dhms" || rawFormat === "mm:ss") {
        format = rawFormat;
      }
    }

    const thenRevalidate = root.getAttribute(COUNTDOWN_THEN_ATTR) === "revalidate";

    // gosx#178 review finding m6: a root with no segment children and no
    // valid compact format renders nothing at all — the timer still ticks
    // every second for no visible effect. That is silent and easy to miss
    // in review, so warn once at setup, unless then="revalidate" makes the
    // countdown useful for its side effect alone even with nothing to
    // render.
    if (!hasSegments && format == null && !thenRevalidate) {
      if (hasFormatAttr) {
        console.warn(
          "[gosx] invalid " + COUNTDOWN_FORMAT_ATTR + " value " + JSON.stringify(String(root.getAttribute(COUNTDOWN_FORMAT_ATTR) || ""))
          + "; this countdown renders nothing (no valid format and no " + COUNTDOWN_SEGMENT_ATTR + " children)",
        );
      } else {
        console.warn(
          "[gosx] " + COUNTDOWN_ATTR + " has no " + COUNTDOWN_FORMAT_ATTR + " and no " + COUNTDOWN_SEGMENT_ATTR
          + " children; this countdown renders nothing",
        );
      }
    }

    // gosx#178 review finding m7: a segment set missing one or more of the
    // four names still renders — each present segment just shows its own
    // remainder modulo its own unit (a seconds-only segment on a 5 minute
    // countdown shows "59", not "299"), which reads as a bug to an author
    // who did not intend it. Warn once at setup so an incomplete set is a
    // deliberate choice, not a silent surprise.
    if (hasSegments) {
      const missing = COUNTDOWN_SEGMENT_NAMES.filter(function(name) { return segments[name].length === 0; });
      if (missing.length > 0) {
        console.warn(
          "[gosx] " + COUNTDOWN_ATTR + " segment set on this element is missing " + missing.join(", ")
          + "; each present segment shows only its own remainder (for example seconds wraps at 60), not the total across the missing units",
        );
      }
    }

    // warnTiers and cueTiers (gosx#213) share parseCountdownTierPairs'
    // fail-closed-as-a-whole behavior: an empty array here means "this
    // countdown has no live tiers of this kind", whether because the
    // author never wrote the attribute or because what they wrote failed
    // to parse.
    let warnTiers = [];
    if (root.hasAttribute(COUNTDOWN_WARN_ATTR)) {
      const rawWarn = root.getAttribute(COUNTDOWN_WARN_ATTR);
      const parsedWarn = parseCountdownTierPairs(rawWarn, isValidCountdownWarnClassToken);
      if (parsedWarn == null) {
        console.warn(
          "[gosx] invalid " + COUNTDOWN_WARN_ATTR + " value " + JSON.stringify(String(rawWarn || ""))
          + "; must be a comma-separated list of threshold:class pairs, such as \"30s:is-warn,10s:is-critical\""
          + "; the warn thresholds are disabled for this countdown",
        );
      } else {
        warnTiers = parsedWarn;
      }
    }

    let cueTiers = [];
    if (root.hasAttribute(COUNTDOWN_CUE_ATTR)) {
      const rawCue = root.getAttribute(COUNTDOWN_CUE_ATTR);
      const parsedCue = parseCountdownTierPairs(rawCue, isValidCountdownCueToken);
      if (parsedCue == null) {
        console.warn(
          "[gosx] invalid " + COUNTDOWN_CUE_ATTR + " value " + JSON.stringify(String(rawCue || ""))
          + "; must be a comma-separated list of threshold:cue pairs using \"beep\" or \"chime\", such as \"10s:beep\""
          + "; the cue thresholds are disabled for this countdown",
        );
      } else {
        cueTiers = parsedCue;
      }
    }

    return {
      root: root,
      targetMs: targetMs,
      hasSegments: hasSegments,
      segments: segments,
      format: format,
      warnTiers: warnTiers,
      cueTiers: cueTiers,
      then: thenRevalidate,
    };
  }

  function renderCountdownState(state, remainderSeconds) {
    if (state.hasSegments) {
      const c = countdownComponents(remainderSeconds);
      const values = { days: c.days, hours: c.hours, minutes: c.minutes, seconds: c.seconds };
      COUNTDOWN_SEGMENT_NAMES.forEach(function(name) {
        const text = countdownPad2(values[name]);
        state.segments[name].forEach(function(el) {
          el.textContent = text;
        });
      });
      return;
    }
    if (state.format === "dhms") {
      state.root.textContent = formatCountdownDHMS(remainderSeconds);
    } else if (state.format === "mm:ss") {
      state.root.textContent = formatCountdownMMSS(remainderSeconds);
    }
  }

  // triggerCountdownThen fires the one-time then="revalidate" action, and
  // reports whether it actually did (the caller only marks a target
  // "fired" — see countdownFiredTargets below — on a true return, so a
  // target this backs off on gets to retry on a later tick instead of
  // being silently abandoned).
  //
  // It no-ops when the page has no ACTIVE revalidation poll — reusing
  // revalidateTimerHandle (set by setupPageRevalidation above) instead of
  // re-scanning the DOM means this reads the same "is periodic
  // revalidation actually running" answer the revalidate poll itself
  // relies on. revalidateTimerHandle is also null when a revalidate root
  // element IS present but its interval failed validation or its src is
  // cross-origin (gosx#178 review finding m12) — "no active poll", not "no
  // revalidate root", is the precise condition.
  //
  // gosx#178 review finding M3: this applies the exact same three guards
  // runRevalidateTick does before its own periodic poll — an in-progress
  // navigation or form submission, a hidden document, or a focused form
  // control all block a countdown-triggered revalidation exactly like they
  // block the periodic one. A blocked attempt returns false and is retried
  // on the next 1-second countdown tick, same as the periodic poll retries
  // on its own next interval tick.
  function triggerCountdownThen() {
    if (revalidateTimerHandle == null) {
      return false;
    }
    if (documentIsHidden() || focusedControlBlocksRevalidation() || navigationOrFormSubmissionInFlight()) {
      return false;
    }
    // gosx#178 review finding B1, second layer: countdownFiredTargets below
    // stops a re-render that still carries the SAME target instant from
    // re-arming the trigger, but a DYNAMIC data-gosx-countdown value
    // ({...}) can legitimately compute a slightly different target on
    // every render (an app bug, or simply "5 seconds from now" recomputed
    // each time) — every such render mints a fresh key the Set has never
    // seen. This cooldown is independent of which target asked: no
    // countdown-triggered revalidation fires more than once per
    // revalidateIntervalMs, the same cadence its own author already
    // declared acceptable for periodic revalidation on this page.
    const now = Date.now();
    if (now - countdownThenLastFiredAt < revalidateIntervalMs) {
      return false;
    }
    countdownThenLastFiredAt = now;
    const generation = countdownGeneration;
    revalidateNavigation().catch(function(error) {
      if (generation !== countdownGeneration) return;
      reportNavigationFailure("countdown revalidation", error, {
        source: windowLocationHref(),
      });
    });
    return true;
  }

  // updateCountdownState renders one tick for one countdown and reports
  // whether that countdown is now fully "finished": its remainder has
  // clamped to zero AND (it has no then="revalidate" action, or that
  // action has already fired). A finished countdown can never change
  // again — render output stays clamped, warn class stays set, and the
  // then action (if any) has run — so runCountdownTick below stops the
  // shared timer once every countdown on the page is finished (gosx#178
  // review finding m8).
  //
  // then="revalidate" firing is keyed by the countdown's OWN target
  // instant (countdownFiredTargets), not a flag on this per-generation
  // record (gosx#178 review finding B1). setupPageCountdowns rebuilds a
  // fresh record — with no memory of any previous firing — every time it
  // runs, including after the very revalidation a countdown's own trigger
  // just caused; if the refreshed document still shows the same expired
  // instant (the common real case: a draft pick's clock at zero while the
  // server has not advanced yet), a per-record flag re-arms and fires
  // again next tick, forever. Keying by the immutable target value itself,
  // in a Set that lives for the page's whole lifetime (never reset by
  // setupPageCountdowns/countdownGeneration), means the SAME instant is
  // recognized as already handled no matter how many times a rescan
  // rebuilds a state record for it.
  function updateCountdownState(state, nowMs) {
    const remainderMs = Math.max(0, state.targetMs - nowMs);
    const remainderSeconds = Math.floor(remainderMs / 1000);
    renderCountdownState(state, remainderSeconds);
    // warnTiers are level-triggered, recomputed every tick from the
    // current remainder — a countdown that resets to a later target
    // re-arms every tier for free, with no crossing memory needed at all.
    for (const tier of state.warnTiers) {
      setElementClassActive(state.root, tier.token, remainderSeconds <= tier.seconds);
    }
    // cueTiers are edge-triggered: see countdownCueFiredKeys' own doc
    // comment for why a (targetMs, threshold, cue) key, not a per-tick
    // level comparison, is what "fires once per downward crossing" means
    // here.
    for (const tier of state.cueTiers) {
      if (remainderSeconds > tier.seconds) continue;
      const cueKey = state.targetMs + "|" + tier.seconds + "|" + tier.token;
      if (countdownCueFiredKeys.has(cueKey)) continue;
      countdownCueFiredKeys.add(cueKey);
      playCountdownCue(tier.token);
    }
    if (state.then && remainderMs <= 0 && !countdownFiredTargets.has(state.targetMs)) {
      if (triggerCountdownThen()) {
        countdownFiredTargets.add(state.targetMs);
      }
    }
    return remainderMs <= 0 && (!state.then || countdownFiredTargets.has(state.targetMs));
  }

  function runCountdownTick() {
    const now = Date.now();
    let allFinished = true;
    for (const state of countdownRoots) {
      if (!updateCountdownState(state, now)) {
        allFinished = false;
      }
    }
    if (allFinished && countdownTimerHandle != null) {
      // gosx#178 review finding m8: nothing left on the page can ever
      // change again — stop paying for a 1-second timer that would only
      // ever re-render the same clamped, already-final values.
      clearInterval(countdownTimerHandle);
      countdownTimerHandle = null;
    }
  }

  function teardownPageCountdowns() {
    if (countdownTimerHandle != null) {
      clearInterval(countdownTimerHandle);
    }
    countdownTimerHandle = null;
    countdownRoots = [];
  }

  // setupPageCountdowns scans for every data-gosx-countdown element on
  // page boot and after every soft navigation (see finalizeNavigation and
  // the initial-document replay below) — the same lifecycle
  // setupPageRevalidation follows just above. It never writes to a
  // countdown element itself: the server-rendered text (or segment
  // values) stays exactly as rendered until the first tick, one second
  // later, moves it.
  function setupPageCountdowns() {
    // Every call — page boot and every soft navigation — starts a new
    // generation, even one that ends up finding no countdown roots at
    // all. See countdownGeneration's declaration for why.
    countdownGeneration += 1;
    teardownPageCountdowns();
    const states = [];
    for (const root of findCountdownRootElements()) {
      // gosx#178 review finding B2, second layer: buildCountdownState (and
      // everything it calls) must never be able to take the whole
      // navigation runtime's boot down. findCountdownSegments' own guard
      // (Object.create(null) plus hasOwnProperty) is the primary defense
      // for the known prototype-collision shape; this try/catch is the
      // backstop for any other countdown defect this or a future change
      // introduces — one bad root must degrade to "this countdown is
      // disabled", never to "window.__gosx.navigation never publishes".
      let state = null;
      try {
        state = buildCountdownState(root);
      } catch (error) {
        reportNavigationFailure("countdown setup", error, {
          source: windowLocationHref(),
        });
      }
      if (state) states.push(state);
    }
    if (!states.length) {
      return;
    }
    countdownRoots = states;
    countdownTimerHandle = setInterval(runCountdownTick, COUNTDOWN_TICK_MS);
  }

  // ---------------------------------------------------------------------
  // Attention watcher (data-gosx-watch, gosx#214)
  //
  // An element declares a condition over one of its own attributes with
  // data-gosx-watch="<attrName>=<valueRef>": <attrName> is read live off
  // the watch element itself, and <valueRef> is either a literal string
  // (compared verbatim) or a reference to another element's live content
  // or attribute, written "@<selector>" (that element's trimmed
  // textContent) or "@<selector>[<attrName>]" (that element's own named
  // attribute). See parseWatchCondition below for the exact grammar. This
  // is deliberately the smallest condition contract that covers the first
  // consumer — gridiron-2000's draft pick clock, whose data-on-clock
  // attribute the SERVER already renders as "true" or "false" per viewer,
  // so the common case is the plain literal form
  // data-gosx-watch="data-on-clock=true" — while still covering a
  // same-page cross-element comparison (for example against another
  // element's rendered seat id) without requiring the app to pre-compute
  // a boolean for every such comparison server-side.
  //
  // data-gosx-watch-effect declares what happens on a false-to-true
  // transition, a comma-separated list of:
  //   - "class:<name>"            add <name> to the watch element itself.
  //   - "class:<name>@<selector>" add <name> to the FIRST element matched
  //                               by <selector> instead.
  //   - "title"                   flash document.title with the message
  //                               from data-gosx-watch-title on the same
  //                               element, until window focus or the
  //                               condition returns to false, then restore
  //                               the original title exactly.
  //   - "cue:<name>"              play a named cue from the shared
  //                               synthesized tone vocabulary above
  //                               ("beep" or "chime").
  // "class" and "title" effects both track the condition directly — level-
  // triggered, re-evaluated on every rescan, the same model
  // data-gosx-countdown-warn's tiers use for their own class toggle. A
  // class effect is unaffected by anything BUT this runtime (nothing else
  // here ever touches an author-added class), so "re-evaluated every
  // rescan" and "fired once on the edge, undone once on the reverse edge"
  // are externally indistinguishable for it. document.title is different:
  // a soft navigation applies the incoming page's own <title> before
  // watchers evaluate (the same way a real browser navigation updates the
  // tab title), so "title" MUST re-assert itself on every rescan where the
  // condition is still true, or an unrelated swap silently overwrites an
  // in-progress flash — see startTitleFlash's own doc comment. Only "cue"
  // is genuinely one-shot: an audible alert firing again on a swap whose
  // condition merely stays true would be a real, user-visible bug, so it
  // fires exactly once, strictly on the false-to-true edge.
  // An unrecognized or malformed token is dropped on its own, with one
  // console.warn — unlike data-gosx-countdown-warn/-cue's pairs, these are
  // independent side effects rather than one coherent tier ladder, so a
  // broken "cue:bogus" token should not also disable a valid "class:..."
  // token in the same list.
  //
  // SCOPE: a watch condition is evaluated exactly twice per page
  // lifetime-unit — once at setupPageWatchers' own call (page boot, and
  // again after every soft navigation or revalidation swap; see
  // finalizeNavigation and the initial-document replay below) — the same
  // rescan lifecycle data-gosx-countdown follows, and matches the issue's
  // own first consumer exactly ("...becomes true...after a revalidation
  // swap"). It is NOT a live MutationObserver: this runtime already runs
  // on pages with high-frequency DOM writes of their own (Scene3D/WebGL
  // telemetry, in-place patch ops — see patch.ts), and an unfiltered
  // subtree attribute observer sitting under all of that is a real,
  // ongoing cost for a condition this contract only promises to notice
  // "after a swap". An attribute changed by hand-authored script with no
  // swap in between is not observed until the next one.
  // ---------------------------------------------------------------------

  const WATCH_CUE_NAMES = AUDIO_CUE_NAMES;

  function safeQuerySelector(selector) {
    if (!selector || typeof document.querySelector !== "function") return null;
    try {
      return document.querySelector(selector);
    } catch (_e) {
      return null;
    }
  }

  // parseWatchCondition splits data-gosx-watch's value at the FIRST "="
  // (an attribute name cannot itself contain "="). A valueRef beginning
  // with "@" is a selector reference, optionally followed by "[<attrName>]"
  // to read that target's named attribute instead of its textContent; any
  // other valueRef is a literal, compared verbatim including the empty
  // string. Returns null for a value with no "=" or an empty attrName —
  // buildWatchState below disables the whole watcher and warns once, the
  // same fail-closed shape every other declarative attribute in this file
  // uses for a value it cannot parse at all.
  function parseWatchCondition(rawValue) {
    const raw = String(rawValue == null ? "" : rawValue);
    const splitAt = raw.indexOf("=");
    if (splitAt <= 0) return null;
    const attrName = raw.slice(0, splitAt).trim();
    if (!attrName) return null;
    const rawValueRef = raw.slice(splitAt + 1);
    if (rawValueRef.charAt(0) !== "@") {
      return { attrName: attrName, selector: null, refAttr: null, literal: rawValueRef };
    }
    const ref = rawValueRef.slice(1);
    const bracket = ref.indexOf("[");
    if (bracket === -1) {
      if (!ref) return null;
      return { attrName: attrName, selector: ref, refAttr: null, literal: null };
    }
    if (bracket === 0 || ref.charAt(ref.length - 1) !== "]") return null;
    const selector = ref.slice(0, bracket);
    const refAttr = ref.slice(bracket + 1, ref.length - 1).trim();
    if (!selector || !refAttr) return null;
    return { attrName: attrName, selector: selector, refAttr: refAttr, literal: null };
  }

  // resolveWatchValueRef reads the LIVE comparison value a condition's
  // valueRef currently points at: the literal string itself, or a fresh
  // document.querySelector lookup's textContent/attribute. A selector that
  // currently matches nothing resolves to null, which evaluateWatchCondition
  // below treats as "never equal" rather than throwing — a target that has
  // not rendered yet (or was removed) is an ordinary false condition, not
  // an error.
  function resolveWatchValueRef(condition) {
    if (condition.selector == null) return condition.literal;
    const target = safeQuerySelector(condition.selector);
    if (!target) return null;
    if (condition.refAttr) {
      return typeof target.getAttribute === "function" ? target.getAttribute(condition.refAttr) : null;
    }
    const text = target.textContent;
    return text == null ? null : String(text).trim();
  }

  // evaluateWatchCondition is the whole condition contract: strict string
  // equality between the watch element's own live attrName attribute and
  // the resolved valueRef. A missing attrName attribute (getAttribute
  // returns null) never matches anything, including a literal empty
  // string — an absent attribute and an explicitly empty one are
  // different states, and only the latter is "equal to \"\"".
  function evaluateWatchCondition(record) {
    const condition = record.condition;
    const el = record.root;
    if (!el || typeof el.getAttribute !== "function") return false;
    const current = el.getAttribute(condition.attrName);
    if (current == null) return false;
    const expected = resolveWatchValueRef(condition);
    if (expected == null) return false;
    return current === expected;
  }

  // parseWatchEffects parses data-gosx-watch-effect's comma-separated
  // token list. Each token is validated and normalized independently;
  // an unrecognized or malformed token is dropped with one console.warn,
  // and every other valid token in the same list still applies — see this
  // section's own doc comment above for why that differs from
  // parseCountdownTierPairs' fail-the-whole-attribute choice.
  function parseWatchEffects(rawValue, root) {
    const raw = String(rawValue == null ? "" : rawValue);
    const effects = [];
    for (const rawToken of raw.split(",")) {
      const token = rawToken.trim();
      if (!token) continue;
      if (token === "title") {
        const message = root.getAttribute && root.getAttribute(WATCH_TITLE_ATTR);
        if (!message) {
          console.warn(
            "[gosx] " + WATCH_EFFECT_ATTR + " \"title\" has no " + WATCH_TITLE_ATTR
            + " on this element; the title effect is disabled",
          );
          continue;
        }
        effects.push({ kind: "title", message: message });
        continue;
      }
      if (token.indexOf("class:") === 0) {
        const rest = token.slice("class:".length);
        const at = rest.indexOf("@");
        const name = (at === -1 ? rest : rest.slice(0, at)).trim();
        const selector = at === -1 ? null : rest.slice(at + 1).trim();
        if (!name || (at !== -1 && !selector)) {
          console.warn(
            "[gosx] invalid " + WATCH_EFFECT_ATTR + " token " + JSON.stringify(token) + "; this effect is disabled",
          );
          continue;
        }
        effects.push({ kind: "class", name: name, selector: selector });
        continue;
      }
      if (token.indexOf("cue:") === 0) {
        const name = token.slice("cue:".length).trim();
        if (!Object.prototype.hasOwnProperty.call(WATCH_CUE_NAMES, name)) {
          console.warn(
            "[gosx] invalid " + WATCH_EFFECT_ATTR + " cue name " + JSON.stringify(name)
            + "; must be \"beep\" or \"chime\"",
          );
          continue;
        }
        effects.push({ kind: "cue", name: name });
        continue;
      }
      console.warn("[gosx] unrecognized " + WATCH_EFFECT_ATTR + " token " + JSON.stringify(token) + "; this effect is disabled");
    }
    return effects;
  }

  function findWatchRootElements() {
    const found = [];
    walkElements(document.body, function(node) {
      if (node.hasAttribute && node.hasAttribute(WATCH_ATTR)) {
        found.push(node);
      }
      return true;
    });
    return found;
  }

  // buildWatchState turns one data-gosx-watch element into the internal
  // record evaluateWatchRecord reads, or null for a condition that fails
  // to parse at all.
  //
  // record.key is what watchActiveState (module-level, never reset by
  // watchGeneration) uses to remember whether this watcher was already
  // active across a rescan: the watch element's own id when it has one
  // ("id:" prefix), or its position among data-gosx-watch elements in
  // document order otherwise ("pos:" prefix). A DOM node carrying
  // data-gosx-watch does not survive a revalidation swap (replaceBody
  // clones a fresh document's body contents wholesale — see replaceBody
  // above), so node identity itself cannot back this memory across a
  // swap; id or document position are the two stable-enough proxies for
  // "the same logical watcher" available with no further author
  // cooperation. An app whose watch elements have no id AND whose count
  // or order can change between renders will not get correct cross-swap
  // transition memory from the positional fallback — give a watch element
  // a stable id whenever its position in the document can change.
  function buildWatchState(root, index) {
    const rawCondition = root.getAttribute(WATCH_ATTR);
    const condition = parseWatchCondition(rawCondition);
    if (!condition) {
      console.warn(
        "[gosx] invalid " + WATCH_ATTR + " value " + JSON.stringify(String(rawCondition || ""))
        + "; must be \"<attrName>=<value>\" or \"<attrName>=@<selector>[<attrName>]\""
        + "; this watcher is disabled",
      );
      return null;
    }
    const effects = root.hasAttribute(WATCH_EFFECT_ATTR)
      ? parseWatchEffects(root.getAttribute(WATCH_EFFECT_ATTR), root)
      : [];
    const key = root.id ? ("id:" + root.id) : ("pos:" + index);
    return { root: root, key: key, condition: condition, effects: effects };
  }

  function applyWatchClassEffect(record, effect, active) {
    const target = effect.selector ? safeQuerySelector(effect.selector) : record.root;
    if (!target) return;
    setElementClassActive(target, effect.name, active);
  }

  // startTitleFlash begins alternating document.title between `message`
  // and the pre-flash original every WATCH_TITLE_FLASH_INTERVAL_MS, the
  // classic "New message!" tab-flash pattern. Only one flash runs at a
  // time (document.title is one shared global) — a later watcher's
  // false-to-true transition takes ownership and replaces the message,
  // but the ORIGINAL title captured here is only ever the one seen before
  // the FIRST flash of the current run, never an already-flashing value.
  //
  // Called on every evaluation where the owning record's condition is
  // active, not only on the false-to-true edge (see applyWatchTitleEffect
  // below): a soft navigation applies the incoming document's own <title>
  // BEFORE setupPageWatchers runs (the same way a real browser navigation
  // updates the tab title), which would otherwise silently overwrite an
  // in-progress flash on every swap where the condition merely STAYS
  // true. A call for the CURRENT owner while already flashing is cheap
  // and idempotent: it only re-asserts `message`, without disturbing
  // titleFlashOriginalTitle or restarting the blink cycle.
  function startTitleFlash(ownerKey, message) {
    if (typeof document === "undefined" || typeof document.title !== "string") return;
    if (titleFlashOwnerKey === ownerKey && titleFlashHandle != null) {
      document.title = message;
      return;
    }
    if (titleFlashHandle == null) {
      titleFlashOriginalTitle = document.title;
    } else {
      clearInterval(titleFlashHandle);
    }
    titleFlashOwnerKey = ownerKey;
    let showingMessage = true;
    document.title = message;
    titleFlashHandle = setInterval(function() {
      showingMessage = !showingMessage;
      document.title = showingMessage ? message : titleFlashOriginalTitle;
    }, WATCH_TITLE_FLASH_INTERVAL_MS);
  }

  // stopTitleFlash restores the original title exactly and clears the
  // timer, but only if `ownerKey` is still the flash's current owner — a
  // stale stop request (for example a watcher whose condition already
  // cleared once, arriving after another watcher has since taken over the
  // flash) must not cut off the CURRENT owner's alert.
  function stopTitleFlash(ownerKey) {
    if (titleFlashOwnerKey !== ownerKey) return;
    if (titleFlashHandle != null) {
      clearInterval(titleFlashHandle);
      titleFlashHandle = null;
    }
    if (titleFlashOriginalTitle != null) {
      document.title = titleFlashOriginalTitle;
    }
    titleFlashOwnerKey = null;
    titleFlashOriginalTitle = null;
  }

  function onTitleFlashWindowFocus() {
    if (titleFlashOwnerKey != null) {
      stopTitleFlash(titleFlashOwnerKey);
    }
  }

  // applyWatchTitleEffect is "title"'s half of evaluateWatchRecord's
  // per-effect dispatch below — level-tied to `active`, exactly like
  // applyWatchClassEffect, and for the same reason startTitleFlash's own
  // doc comment above explains: unlike a CSS class (nothing else in this
  // runtime ever touches one an author added), document.title is reset by
  // navigation itself on every swap, so "only fire on the edge" would lose
  // the flash on the very next swap where the condition simply stays true.
  function applyWatchTitleEffect(record, effect, active) {
    if (active) {
      startTitleFlash(record.key, effect.message);
    } else if (titleFlashOwnerKey === record.key) {
      stopTitleFlash(record.key);
    }
  }

  // evaluateWatchRecord is the entire per-watcher evaluation
  // setupPageWatchers below runs once per record, at page boot and after
  // every soft navigation/revalidation swap (see this section's own scope
  // note above). "class" and "title" effects are level-tied — reapplied
  // every call from the current `active` value, regardless of whether it
  // differs from the last evaluation. Only "cue" is genuinely
  // edge-triggered: an audible alert must never replay on a swap whose
  // condition merely stays true, so it fires exactly once, on the specific
  // false-to-true edge watchActiveState's remembered value reveals.
  function evaluateWatchRecord(record) {
    const active = evaluateWatchCondition(record);
    const wasActive = watchActiveState.get(record.key) === true;
    for (const effect of record.effects) {
      if (effect.kind === "class") {
        applyWatchClassEffect(record, effect, active);
      } else if (effect.kind === "title") {
        applyWatchTitleEffect(record, effect, active);
      }
    }
    if (active && !wasActive) {
      for (const effect of record.effects) {
        if (effect.kind === "cue") {
          playCountdownCue(effect.name);
        }
      }
    }
    watchActiveState.set(record.key, active);
  }

  function teardownPageWatchers() {
    watchRoots = [];
  }

  // setupPageWatchers scans for every data-gosx-watch element on page boot
  // and after every soft navigation (see finalizeNavigation and the
  // initial-document replay below) — the same lifecycle setupPageCountdowns
  // follows just above, and the mechanism this section's own scope note
  // documents. Each record is built AND evaluated in the same pass: this
  // is what lets a watcher whose condition is already true the first time
  // it is ever seen (the primary gosx#214 scenario — a revalidation swap
  // that introduces a freshly-true data-on-clock attribute) fire its
  // effects immediately, since watchActiveState.get(key) reads undefined
  // (never "already active") for a key it has not seen before.
  function setupPageWatchers() {
    // Every call — page boot and every soft navigation — starts a new
    // generation, even one that ends up finding no watch roots at all.
    // See watchGeneration's declaration for why.
    watchGeneration += 1;
    teardownPageWatchers();
    const records = [];
    const roots = findWatchRootElements();
    for (let i = 0; i < roots.length; i += 1) {
      // Mirrors setupPageCountdowns' own try/catch below: one bad watcher
      // must degrade to "this watcher is disabled", never take down the
      // rest of the navigation runtime's boot.
      let record = null;
      try {
        record = buildWatchState(roots[i], i);
      } catch (error) {
        reportNavigationFailure("watch setup", error, { source: windowLocationHref() });
      }
      if (record) records.push(record);
    }
    watchRoots = records;
    for (const record of watchRoots) {
      try {
        evaluateWatchRecord(record);
      } catch (error) {
        reportNavigationFailure("watch evaluate", error, { source: windowLocationHref() });
      }
    }
  }

  // ---------------------------------------------------------------------
  // Declarative list filter (data-gosx-filter, gosx#215)
  //
  // An input declares FILTER_ATTR set to the list it filters: an element
  // id, or (when no element carries that id) a CSS selector — the same
  // "try an id first, fall back to a selector" convenience
  // resolveFilterTarget below gives an author who does not want to write
  // "#" for the common case. Each row inside that target — any descendant,
  // not only a direct child, so a <table>/<tbody>/<tr> shape works exactly
  // like a flat list — carries FILTER_TEXT_ATTR with the text to search;
  // the runtime reads this attribute, never a row's own rendered
  // textContent, so the server can normalize case, whitespace, and fold in
  // search terms (a player's team or position) that never render visibly.
  //
  // Filtering itself is a case-insensitive substring match against the
  // trimmed, lower-cased input value, applied FILTER_DEBOUNCE_MS after the
  // last keystroke (via the delegated "input" listener below) so a fast
  // typist does not force a reflow on every character. An empty input
  // (after trimming) matches every row — "shows all" is the literal empty
  // case of the same match rule, not a special path.
  //
  // A row that would be hidden is instead left alone — never fought out
  // from under the visitor — while it is under active interaction:
  // rowIsFilterGuarded below covers a row containing the focused control
  // (document.activeElement) and a row the pointer currently sits over
  // (filterHoverRow, kept live by the delegated "mouseover"/"mouseout"
  // listeners below — the same delegation shape onMouseOver above already
  // uses for prefetch, rather than a live ":hover" match this runtime
  // would otherwise have to re-derive per row on every apply). The guard
  // is re-checked on every apply, not cached — so the very next apply
  // (another keystroke, or the next rescan) hides a once-guarded row as
  // soon as the interaction has actually ended by then. Leaving an
  // interaction does not itself trigger a fresh apply; the row waits for
  // whatever apply comes next.
  //
  // FILTER_HIDDEN_CLASS is a class hook, exactly like the reorder section's
  // own class hooks below: the runtime toggles the class, the application's
  // own CSS decides what a hidden row actually looks like (display: none,
  // an animated collapse, or something else entirely). FILTER_ANNOUNCE_ATTR
  // (any truthy value under managedFormShorthandTruthy's rule) opts an
  // input into an "N of M shown" live-region announcement after every
  // apply, reusing the shared aria-live region announceNavigation already
  // maintains — the cheapest possible accessible count, one shared region
  // rather than a second one this feature would otherwise have to own.
  //
  // Like data-gosx-watch, a filter is rebuilt from scratch at page boot and
  // after every soft navigation or revalidation swap (setupPageFilters,
  // called from finalizeNavigation and the initial-document replay below)
  // — replaceBody clones a fresh document's body contents wholesale, so
  // neither the input's typed value nor the rows' hidden state survives a
  // swap on their own. filterQueryState (module-level, keyed the same way
  // watchActiveState is — see its own declaration above) remembers the
  // query text ACROSS that rebuild: setupPageFilters restores it onto the
  // freshly-rendered input's own .value and re-applies it against the
  // freshly-rendered rows in the same pass, so a swap mid-search neither
  // loses what the visitor already typed nor reverts an already-filtered
  // list back to showing everything.
  // ---------------------------------------------------------------------

  // resolveFilterTarget tries `value` as an element id first (the common,
  // no-punctuation-required case: data-gosx-filter="draft-pool-list") and
  // falls back to a CSS selector (data-gosx-filter=".draft-pool tbody")
  // when no element has that id. Returns null for an empty value or one
  // that matches nothing either way.
  function resolveFilterTarget(rawValue) {
    const value = String(rawValue == null ? "" : rawValue).trim();
    if (!value) return null;
    if (typeof document.getElementById === "function") {
      const byId = document.getElementById(value);
      if (byId) return byId;
    }
    return safeQuerySelector(value);
  }

  function findFilterInputElements() {
    return collectElements(document.body, function(node) {
      return node.hasAttribute && node.hasAttribute(FILTER_ATTR);
    });
  }

  // buildFilterState turns one data-gosx-filter input into the internal
  // record applyFilterRecord below reads, or null for a target that fails
  // to resolve at all. record.key mirrors buildWatchState's own key
  // (gosx#214): the input's own id ("id:" prefix) when it has one, or its
  // position among data-gosx-filter inputs in document order otherwise
  // ("pos:" prefix) — see filterQueryState's declaration above for why
  // this key, not the DOM node, is what carries the query across a swap.
  function buildFilterState(input, index) {
    const rawTarget = input.getAttribute(FILTER_ATTR);
    const target = resolveFilterTarget(rawTarget);
    if (!target) {
      console.warn(
        "[gosx] " + FILTER_ATTR + " target " + JSON.stringify(String(rawTarget || ""))
        + " does not match any element id or selector; this filter is disabled",
      );
      return null;
    }
    const key = input.id ? ("id:" + input.id) : ("pos:" + index);
    const rawQuery = filterQueryState.has(key) ? filterQueryState.get(key) : "";
    return {
      input: input,
      target: target,
      key: key,
      rawQuery: rawQuery,
      announce: input.hasAttribute(FILTER_ANNOUNCE_ATTR)
        && managedFormShorthandTruthy(input.getAttribute(FILTER_ANNOUNCE_ATTR)),
      debounceHandle: null,
    };
  }

  function normalizeFilterQuery(raw) {
    return String(raw == null ? "" : raw).trim().toLowerCase();
  }

  function filterRowsOf(target) {
    return collectElements(target, function(node) {
      return node.hasAttribute && node.hasAttribute(FILTER_TEXT_ATTR);
    });
  }

  // closestFilterRow walks up from `node` to the nearest ancestor (or
  // `node` itself) carrying FILTER_TEXT_ATTR — mirrors closestLink's own
  // ancestor walk above, applied to filter rows instead of managed links.
  function closestFilterRow(node) {
    let current = node;
    while (current) {
      if (current.hasAttribute && current.hasAttribute(FILTER_TEXT_ATTR)) {
        return current;
      }
      current = current.parentNode;
    }
    return null;
  }

  // onFilterPointerOver keeps filterHoverRow live: every "mouseover" (which
  // bubbles and re-fires as the pointer crosses each descendant) recomputes
  // the current row from the live event target, so this stays correct
  // through a drag or a scroll with no polling of its own.
  function onFilterPointerOver(event) {
    filterHoverRow = closestFilterRow(event.target);
  }

  // onFilterPointerOut clears filterHoverRow only when the pointer has
  // actually left the current row — not on every "mouseout" a move between
  // two descendants OF THE SAME row also fires. event.relatedTarget is the
  // element the pointer is entering; if that element (or one of its
  // ancestors) is still the same row, onFilterPointerOver above already
  // re-asserts it as part of the same pointer move and there is nothing to
  // clear here.
  function onFilterPointerOut(event) {
    if (closestFilterRow(event.relatedTarget) === filterHoverRow) {
      return;
    }
    filterHoverRow = null;
  }

  // rowIsFilterGuarded reports whether `row` is under active interaction
  // right now — see this section's own doc comment above for why a
  // guarded row is left alone rather than hidden.
  function rowIsFilterGuarded(row) {
    const active = document.activeElement;
    if (active && typeof row.contains === "function" && row.contains(active)) {
      return true;
    }
    return row === filterHoverRow;
  }

  // applyFilterRecord is the whole match-and-hide pass for one filter
  // input: read every row's FILTER_TEXT_ATTR, compare it against the
  // record's current query, and toggle FILTER_HIDDEN_CLASS accordingly — a
  // row that fails the match but is currently guarded (see
  // rowIsFilterGuarded above) keeps its current visibility instead of
  // being hidden out from under the visitor. Called after every debounced
  // keystroke and once per record on every rescan (setupPageFilters
  // below), so an empty query (or a guard that has since cleared) always
  // converges to the correct shown/hidden state within one apply.
  function applyFilterRecord(record) {
    if (!record.target) return;
    const rows = filterRowsOf(record.target);
    const query = normalizeFilterQuery(record.rawQuery);
    let shown = 0;
    for (const row of rows) {
      const text = String((row.getAttribute && row.getAttribute(FILTER_TEXT_ATTR)) || "").toLowerCase();
      const matches = !query || text.indexOf(query) !== -1;
      const hide = !matches && !rowIsFilterGuarded(row);
      setElementClassActive(row, FILTER_HIDDEN_CLASS, hide);
      if (!hide) shown += 1;
    }
    if (record.announce) {
      announceNavigation(shown + " of " + rows.length + " shown");
    }
  }

  function findFilterRecordForInput(node) {
    for (const record of filterRoots) {
      if (record.input === node) return record;
    }
    return null;
  }

  // onFilterInput is the delegated "input" listener every data-gosx-filter
  // input shares (see the document.addEventListener call near the bottom
  // of this file), the same delegation shape onClick/onMouseOver/onFocusIn
  // already use. The raw value is remembered — both on the record and in
  // filterQueryState, so a swap mid-debounce still carries the visitor's
  // latest keystroke — immediately; only the actual match-and-hide pass
  // (applyFilterRecord) waits out FILTER_DEBOUNCE_MS of quiet.
  function onFilterInput(event) {
    const record = findFilterRecordForInput(event.target);
    if (!record) return;
    const raw = String(event.target.value == null ? "" : event.target.value);
    record.rawQuery = raw;
    filterQueryState.set(record.key, raw);
    if (record.debounceHandle != null) {
      clearTimeout(record.debounceHandle);
    }
    record.debounceHandle = setTimeout(function() {
      record.debounceHandle = null;
      applyFilterRecord(record);
    }, FILTER_DEBOUNCE_MS);
  }

  function teardownPageFilters() {
    for (const record of filterRoots) {
      if (record.debounceHandle != null) {
        clearTimeout(record.debounceHandle);
      }
    }
    filterRoots = [];
  }

  // setupPageFilters scans for every data-gosx-filter input on page boot
  // and after every soft navigation (see finalizeNavigation and the
  // initial-document replay below) — the same rescan lifecycle
  // setupPageWatchers follows just above. Each record restores its
  // remembered query (filterQueryState) onto the freshly-rendered input's
  // own .value and re-applies it against the freshly-rendered rows in the
  // same pass — see this section's own doc comment for why both halves of
  // that are necessary.
  function setupPageFilters() {
    filterGeneration += 1;
    teardownPageFilters();
    const inputs = findFilterInputElements();
    const records = [];
    for (let i = 0; i < inputs.length; i += 1) {
      // Mirrors setupPageWatchers' own try/catch above: one bad filter
      // must degrade to "this filter is disabled", never take down the
      // rest of the navigation runtime's boot.
      let record = null;
      try {
        record = buildFilterState(inputs[i], i);
      } catch (error) {
        reportNavigationFailure("filter setup", error, { source: windowLocationHref() });
      }
      if (!record) continue;
      if (record.rawQuery && typeof record.input.value !== "undefined") {
        record.input.value = record.rawQuery;
      }
      records.push(record);
    }
    filterRoots = records;
    for (const record of filterRoots) {
      try {
        applyFilterRecord(record);
      } catch (error) {
        reportNavigationFailure("filter apply", error, { source: windowLocationHref() });
      }
    }
  }

  // Declarative reorder (data-gosx-reorder, gosx#212)
  //
  // A container marks itself REORDER_CONTAINER_ATTR; each direct child that
  // can move carries REORDER_ITEM_ATTR set to that item's identity, and one
  // element inside the item (or the item itself, if none is marked) is its
  // drag handle, REORDER_HANDLE_ATTR. The container also carries
  // REORDER_ACTION_ATTR — the SAME "METHOD /url" spec data-gosx-action uses
  // (see actions.ts) — naming the endpoint a drop or a keyboard commit posts
  // the new order to. It is a dedicated attribute, not a literal
  // data-gosx-action, because actions.ts already delegates plain clicks on
  // any data-gosx-action element; a reorder container is very often a
  // clickable, non-form element, and sharing the attribute would fire a
  // spurious empty click-triggered action from the SAME element.
  //
  // Unlike countdown and revalidate above, dragging itself needs no
  // per-navigation setup/teardown pair: every drag listener below is a
  // single document-level delegated handler (the same pattern actions.ts
  // uses for its click and submit listeners), so it keeps working after a
  // soft-navigation DOM swap with nothing to re-scan — the delegated
  // listener reads live attributes off whatever element the event landed
  // on, on every event, forever. Handle PREPARATION (tabindex, role,
  // aria-grabbed, touch-action) is the one piece that cannot wait for a
  // first interaction — see prepareAllReorderHandles below, called on page
  // load and after every soft navigation the same way actions.ts's own
  // refreshBindings is.
  //
  // Only one reorder gesture — pointer or keyboard — is ever active at a
  // time across the whole page; a grab attempt while one is already active,
  // or while its container's own action submission is still in flight (the
  // gosx#212 "second drag during an in-flight submit" case; see
  // reorderContainerPending), is refused outright. This is a documented
  // BLOCK policy, not a queue: the refused gesture never starts, and the
  // list the user sees never differs from the order the last accepted
  // gesture committed (or reverted to).
  //
  // Pointer drag never moves the real item element in the DOM while the
  // gesture is in progress. It moves a placeholder — a clone of the item,
  // marked REORDER_PLACEHOLDER_CLASS and REORDER_PLACEHOLDER_ATTR, with
  // REORDER_ITEM_ATTR stripped so it is invisible to every function in this
  // section that reads "the items" (reorderItems, the index math, the
  // announcements) — through the list as the pointer crosses sibling
  // midpoints. The real item stays exactly where it started, marked
  // REORDER_LIFTED_CLASS, and follows the pointer with exactly one inline
  // style: `transform: translateY(...)`. Author CSS for that class supplies
  // `position: absolute` (with no top/left — an absolutely positioned box
  // with neither keeps its static, in-flow position, so it starts exactly
  // where translateY(0) should be) so the item leaves no gap of its own
  // alongside the placeholder's gap. On drop, the real item replaces the
  // placeholder at whatever slot the placeholder last reached; on cancel,
  // the placeholder is simply discarded and the item — which never moved —
  // needs no repositioning at all.
  //
  // Keyboard reorder has no pointer to float a lifted element toward, so it
  // skips the lift/placeholder split entirely and just moves the real item
  // one slot per arrow press, live, via insertBefore/insertAfter — the
  // browser's own reflow is the only "animation" a keyboard reorder needs.
  //
  // Revalidation is paused (see suspendRevalidation above) for the full
  // gesture: pointerdown/grab through drop, cancel, or pointercancel. It
  // resumes the moment the gesture ends, not after the follow-up action
  // submission settles — a revalidation DOM swap after the user's finger or
  // pointer has already left the list is no longer a hazard the way one
  // mid-drag is; pendingManagedForms-style protection for the submission
  // itself is unnecessary because nothing else touches the container's
  // FORM_STATE_ATTR/FORM_PENDING_ATTR pair while it is set (see
  // reorderContainerPending).
  // ---------------------------------------------------------------------

  // reorderTargetIndex is the pure pointer-position -> target-index function
  // gosx#212 requires (see client/js's reorder unit tests). `pointerY` is a
  // viewport-space Y coordinate — the same space PointerEvent.clientY and
  // Element.getBoundingClientRect() both use, so a scrolled container needs
  // no special handling here: its items' rects already reflect the scroll
  // position, the same way they reflect anything else about layout. `rects`
  // lists every OTHER item in the sortable list — everything except the one
  // being dragged — in current top-to-bottom order, each as { top, height }.
  //
  // The rule is a midpoint crossing: the dragged item belongs immediately
  // before the first remaining item whose vertical midpoint the pointer has
  // not yet reached. The return value is an insertion index into `rects`:
  // 0 places the dragged item before rects[0], rects.length places it after
  // the last one. An empty `rects` (a single-item list, dragging the only
  // item) always returns 0 — there is nowhere else it could go.
  function reorderTargetIndex(pointerY, rects) {
    const list = rects || [];
    for (let i = 0; i < list.length; i += 1) {
      const rect = list[i];
      const midpoint = rect.top + rect.height / 2;
      if (pointerY < midpoint) {
        return i;
      }
    }
    return list.length;
  }

  // activeReorderDrag holds the in-progress POINTER drag, or null.
  let activeReorderDrag = null;
  // activeReorderKeyboard holds the in-progress KEYBOARD grab, or null.
  let activeReorderKeyboard = null;

  function addManagedClass(node, className) {
    if (!node || !node.setAttribute || !node.getAttribute) return;
    const current = String(node.getAttribute("class") || "").split(/\s+/).filter(Boolean);
    if (current.indexOf(className) < 0) {
      current.push(className);
      node.setAttribute("class", current.join(" "));
    }
  }

  function removeManagedClass(node, className) {
    if (!node || !node.setAttribute || !node.getAttribute) return;
    const current = String(node.getAttribute("class") || "").split(/\s+/).filter(Boolean);
    const next = current.filter(function(name) { return name !== className; });
    if (next.length !== current.length) {
      node.setAttribute("class", next.join(" "));
    }
  }

  function reorderContainerTruthy(value) {
    // Mirrors managedFormShorthandTruthy's rule (gosx#179): a bare attribute
    // and "" are truthy, "false" opts out, anything else is truthy.
    return managedFormShorthandTruthy(value);
  }

  function isReorderContainer(node) {
    return !!(node
      && node.hasAttribute
      && node.hasAttribute(REORDER_CONTAINER_ATTR)
      && reorderContainerTruthy(node.getAttribute(REORDER_CONTAINER_ATTR)));
  }

  // reorderItems lists CONTAINER's direct children that carry
  // REORDER_ITEM_ATTR, in current DOM order. A pointer-drag placeholder has
  // that attribute stripped at creation (see beginReorderPointerDrag), so it
  // never appears here — every caller below can treat this list as "the
  // real, identity-bearing items" with no further filtering.
  function reorderItems(container) {
    return toArray(container && container.children).filter(function(child) {
      return child.nodeType === 1 && child.hasAttribute && child.hasAttribute(REORDER_ITEM_ATTR);
    });
  }

  // closestReorderAncestor walks up from `node` (inclusive) the same way
  // closestLink above walks up to find a managed link — this file's
  // established idiom for "nearest ancestor with attribute X" — rather than
  // the native Element.closest(), which not every embedding DOM guarantees.
  function closestReorderAncestor(node, attrName) {
    let current = node;
    while (current) {
      if (current.hasAttribute && current.hasAttribute(attrName)) {
        return current;
      }
      current = current.parentNode;
    }
    return null;
  }

  function reorderHandleFromTarget(target) {
    return closestReorderAncestor(target, REORDER_HANDLE_ATTR);
  }

  function reorderItemFromHandle(handle) {
    return closestReorderAncestor(handle, REORDER_ITEM_ATTR);
  }

  function reorderContainerFromItem(item) {
    return item && item.parentNode && item.parentNode.nodeType === 1 ? item.parentNode : null;
  }

  // reorderHandleForTarget resolves an event target (a pointerdown target or
  // a focused keydown target) to { handle, item, container }, or null if the
  // target is not part of any sortable list. A descendant explicitly marked
  // REORDER_HANDLE_ATTR is always the handle; failing that, the item itself
  // is the handle ONLY when it declares no dedicated handle anywhere in its
  // subtree — a click on an item's body text is not a drag gesture when that
  // item has its own handle element elsewhere.
  function reorderHandleForTarget(target) {
    const explicitHandle = reorderHandleFromTarget(target);
    if (explicitHandle) {
      const item = reorderItemFromHandle(explicitHandle);
      const container = reorderContainerFromItem(item);
      if (item && container && isReorderContainer(container)) {
        return { handle: explicitHandle, item: item, container: container };
      }
      return null;
    }
    const item = closestReorderAncestor(target, REORDER_ITEM_ATTR);
    if (!item || (item.querySelector && item.querySelector("[" + REORDER_HANDLE_ATTR + "]"))) {
      return null;
    }
    const container = reorderContainerFromItem(item);
    if (!container || !isReorderContainer(container)) return null;
    return { handle: item, item: item, container: container };
  }

  // prepareReorderHandle runs the first time a handle is ever resolved
  // (pointer or keyboard), not on every scan — REORDER_HANDLE_READY_ATTR
  // marks it done. It never runs twice for the same element, even across a
  // soft navigation that leaves the element in place.
  function prepareReorderHandle(handle) {
    if (!handle || (handle.hasAttribute && handle.hasAttribute(REORDER_HANDLE_READY_ATTR))) {
      return;
    }
    handle.setAttribute(REORDER_HANDLE_READY_ATTR, "true");
    if (!handle.hasAttribute("tabindex")) {
      handle.setAttribute("tabindex", "0");
    }
    if (!handle.hasAttribute("role")) {
      handle.setAttribute("role", "button");
    }
    if (!handle.hasAttribute("aria-roledescription")) {
      handle.setAttribute("aria-roledescription", "Sortable item");
    }
    handle.setAttribute("aria-grabbed", "false");
    // touch-action: none is required so a touch-drag on the handle is not
    // raced by the browser's own scroll-gesture recognizer before
    // setPointerCapture can claim the pointer (gosx#212). It is scoped to
    // the handle element only — every other pixel of the page, including
    // the rest of a sortable item outside its handle, keeps native scroll
    // behavior untouched. This is a functional/behavioral style, not a
    // visual one, so it sits outside the "transform only" rule that governs
    // REORDER_LIFTED_CLASS positioning below.
    if (handle.style) {
      handle.style.touchAction = "none";
    }
  }

  function reorderHandleForItem(item) {
    if (!item) return null;
    const explicit = item.querySelector ? item.querySelector("[" + REORDER_HANDLE_ATTR + "]") : null;
    return explicit || item;
  }

  // prepareAllReorderHandles eagerly prepares every handle on the page —
  // page load, every soft navigation, and every DOMContentLoaded replay (the
  // same three call sites actions.ts's own refreshBindings uses; see its
  // doc comment) — so a keyboard-only user can Tab to a handle that has
  // never received a pointer or keydown event. Grabbing and dragging stay
  // pure delegated listeners with no scan of their own (see this section's
  // opening comment); this is the one piece of the contract — a11y
  // reachability — that genuinely cannot wait for a first interaction.
  // prepareReorderHandle's own REORDER_HANDLE_READY_ATTR guard makes a
  // repeat scan free for every element already prepared.
  function prepareAllReorderHandles() {
    for (const container of collectElements(document.body, isReorderContainer)) {
      for (const item of reorderItems(container)) {
        prepareReorderHandle(reorderHandleForItem(item));
      }
    }
  }

  function reorderItemLabel(item) {
    const explicit = item && item.getAttribute && item.getAttribute("aria-label");
    if (explicit) return normalizeTextValue(explicit);
    return normalizeTextValue(item && item.textContent) || "item";
  }

  function reorderAnnouncement(verb, item, container) {
    const items = reorderItems(container);
    const index = items.indexOf(item);
    const total = items.length || 1;
    const position = index < 0 ? total : index + 1;
    const label = reorderItemLabel(item);
    switch (verb) {
      case "grab":
        return "Grabbed " + label + ". Position " + position + " of " + total
          + ". Use arrow keys to move, space to drop, escape to cancel.";
      case "move":
        return "Moved to position " + position + " of " + total + ".";
      case "drop":
        return "Dropped " + label + " at position " + position + " of " + total + ".";
      default:
        return "";
    }
  }

  function reorderContainerPending(container) {
    return !!(container && container.getAttribute && container.getAttribute(FORM_PENDING_ATTR) === "true");
  }

  function reorderFieldName(container, attrName, fallback) {
    const value = container && container.getAttribute && container.getAttribute(attrName);
    const trimmed = String(value || "").trim();
    return trimmed || fallback;
  }

  // parseReorderActionSpec reads REORDER_ACTION_ATTR using the exact
  // "METHOD /url" / "/url" grammar data-gosx-action uses (see actions.ts
  // parseAction). It prefers the live actions.ts parser through the public
  // facade when that module is loaded (the common case — actions.ts ships in
  // every bootstrap bundle) and falls back to an inline equivalent so
  // reorder still works if a page somehow loads navigation.ts without it.
  function parseReorderActionSpec(container) {
    const raw = String((container && container.getAttribute && container.getAttribute(REORDER_ACTION_ATTR)) || "").trim();
    if (window.__gosx && window.__gosx.actions && typeof window.__gosx.actions.parse === "function") {
      return window.__gosx.actions.parse(raw, "POST");
    }
    const space = raw.indexOf(" ");
    if (space > 0) {
      return { method: raw.slice(0, space).toUpperCase(), url: raw.slice(space + 1).trim() };
    }
    return { method: "POST", url: raw };
  }

  // submitReorderAction is a lean, dedicated POST/PUT/PATCH transport for
  // gosx#212 — deliberately NOT submitForm/submitAction. Those exist to
  // progressively enhance a real, user-visible <form>, and on a network
  // failure they fall back to a native form submission that navigates the
  // whole page — exactly wrong for a background reorder POST the user never
  // sees as a form. This function instead reports failure back to its caller
  // (commitReorderResult), which reverts the optimistic DOM move and
  // surfaces the error through the SAME FORM_STATE_ATTR/FORM_PENDING_ATTR/
  // announceNavigation vocabulary managed forms use (see
  // setManagedFormPending, captureManagedFormState, restoreManagedFormState
  // above) — never through a page navigation.
  async function submitReorderAction(container, method, url, fieldPairs) {
    const target = navigationURLParts(url);
    if (!target) {
      return { ok: false, error: new Error("data-gosx-reorder-action has no URL: " + JSON.stringify(url)) };
    }
    const body = new URLSearchParams();
    for (const pair of fieldPairs) {
      body.append(pair[0], pair[1]);
    }
    const csrfToken = csrfTokenFromElement(container) || csrfTokenFromMeta();
    let response = null;
    try {
      response = await gosxRuntimeRequest(target.href, {
        method: method,
        headers: Object.assign(
          { Accept: "application/json", "X-Requested-With": "XMLHttpRequest" },
          csrfToken ? { "X-CSRF-Token": csrfToken } : {},
        ),
        body: body,
      });
    } catch (error) {
      return { ok: false, error: error };
    }
    let result = null;
    try {
      result = await parseJSONResponse(response);
    } catch (_error) {
      result = null;
    }
    const failed = !response.ok || !!(result && result.ok === false);
    return { ok: !failed, response: response, result: result };
  }

  // restoreReorderItemPosition puts `item` back at `index` among `parent`'s
  // OTHER current child nodes — an index, not a nextSibling reference: this
  // file's fake-DOM test harness (and, more importantly, DocumentFragment
  // nodes elsewhere in this file) do not reliably expose Node.nextSibling,
  // so childIndex-style array lookups are this file's established idiom for
  // "the node originally after this position" (see childIndex above).
  function restoreReorderItemPosition(item, parent, index) {
    if (!item || !parent) return;
    const siblings = toArray(parent.childNodes).filter(function(node) { return node !== item; });
    const before = index != null && index >= 0 && index < siblings.length ? siblings[index] : null;
    if (before) {
      parent.insertBefore(item, before);
    } else {
      parent.appendChild(item);
    }
  }

  // commitReorderResult runs once per completed gesture (pointer drop or
  // keyboard drop), AFTER the real item element already sits at its new DOM
  // position — the optimistic reorder gosx#212 asks for. `originalParent`/
  // `originalIndex` are the item's pre-gesture position, read back only if
  // the submission fails.
  async function commitReorderResult(container, item, originalParent, originalIndex) {
    const spec = parseReorderActionSpec(container);
    if (!spec.url) {
      reportNavigationFailure("reorder action", new Error(REORDER_ACTION_ATTR + " is missing a URL"), {
        source: windowLocationHref(),
      });
      return;
    }

    const items = reorderItems(container);
    const index = items.indexOf(item);
    const itemField = reorderFieldName(container, REORDER_ITEM_FIELD_ATTR, REORDER_DEFAULT_ITEM_FIELD);
    const indexField = reorderFieldName(container, REORDER_INDEX_FIELD_ATTR, REORDER_DEFAULT_INDEX_FIELD);
    const itemIdentity = String((item.getAttribute && item.getAttribute(REORDER_ITEM_ATTR)) || "");

    const previousState = captureManagedFormState(container);
    setManagedFormPending(container);
    dispatchManagedEvent("gosx:reorder:submit", {
      detail: { container: container, item: item, itemId: itemIdentity, index: index },
    });

    const outcome = await submitReorderAction(container, spec.method, spec.url, [
      [itemField, itemIdentity],
      [indexField, String(index)],
    ]);

    restoreManagedFormState(container, previousState);

    if (!outcome.ok) {
      restoreReorderItemPosition(item, originalParent, originalIndex);
      container.setAttribute(FORM_STATE_ATTR, "error");
      announceNavigation("Reorder failed. Order restored.");
      dispatchManagedEvent("gosx:reorder:error", {
        detail: {
          container: container,
          item: item,
          itemId: itemIdentity,
          index: index,
          response: outcome.response || null,
        },
      });
      reportNavigationFailure(
        "reorder action",
        outcome.error || new Error("reorder action failed with status " + (outcome.response ? outcome.response.status : 0)),
        {
          source: spec.url,
          telemetry: { method: spec.method, url: spec.url, itemId: itemIdentity, index: index },
        },
      );
      return;
    }

    container.setAttribute(FORM_STATE_ATTR, "success");
    dispatchManagedEvent("gosx:reorder:result", {
      detail: { container: container, item: item, itemId: itemIdentity, index: index, result: outcome.result || null },
    });
  }

  // --- keyboard reorder ---------------------------------------------------

  function beginKeyboardReorder(handle) {
    const item = reorderItemFromHandle(handle) || handle;
    const container = reorderContainerFromItem(item);
    if (!container || !item || activeReorderKeyboard || activeReorderDrag || reorderContainerPending(container)) {
      return;
    }
    activeReorderKeyboard = {
      handle: handle,
      item: item,
      container: container,
      originalParent: item.parentNode,
      originalIndex: toArray(item.parentNode.childNodes).indexOf(item),
      releaseRevalidation: suspendRevalidation(),
    };
    addManagedClass(container, REORDER_DRAGGING_CLASS);
    addManagedClass(item, REORDER_GRABBED_CLASS);
    handle.setAttribute("aria-grabbed", "true");
    announceNavigation(reorderAnnouncement("grab", item, container));
  }

  function moveKeyboardReorder(step) {
    const state = activeReorderKeyboard;
    if (!state) return;
    const items = reorderItems(state.container);
    const index = items.indexOf(state.item);
    const targetIndex = index + step;
    if (index < 0 || targetIndex < 0 || targetIndex >= items.length) {
      return;
    }
    if (step < 0) {
      state.container.insertBefore(state.item, items[targetIndex]);
    } else {
      const after = items[targetIndex + 1] || null;
      if (after) {
        state.container.insertBefore(state.item, after);
      } else {
        state.container.appendChild(state.item);
      }
    }
    announceNavigation(reorderAnnouncement("move", state.item, state.container));
  }

  function commitKeyboardReorder() {
    const state = activeReorderKeyboard;
    if (!state) return;
    activeReorderKeyboard = null;
    removeManagedClass(state.container, REORDER_DRAGGING_CLASS);
    removeManagedClass(state.item, REORDER_GRABBED_CLASS);
    state.handle.setAttribute("aria-grabbed", "false");
    state.releaseRevalidation();
    announceNavigation(reorderAnnouncement("drop", state.item, state.container));
    focusElement(state.handle, true);
    commitReorderResult(state.container, state.item, state.originalParent, state.originalIndex);
  }

  function cancelKeyboardReorder() {
    const state = activeReorderKeyboard;
    if (!state) return;
    activeReorderKeyboard = null;
    restoreReorderItemPosition(state.item, state.originalParent, state.originalIndex);
    removeManagedClass(state.container, REORDER_DRAGGING_CLASS);
    removeManagedClass(state.item, REORDER_GRABBED_CLASS);
    state.handle.setAttribute("aria-grabbed", "false");
    state.releaseRevalidation();
    announceNavigation("Reorder cancelled.");
    focusElement(state.handle, true);
  }

  // --- pointer reorder -----------------------------------------------------

  function reorderAutoScrollDelta(clientY, containerRect) {
    if (!containerRect || containerRect.height <= 0) return 0;
    if (clientY <= containerRect.top) return -REORDER_AUTOSCROLL_MAX_PX;
    if (clientY >= containerRect.bottom) return REORDER_AUTOSCROLL_MAX_PX;
    const topZoneEnd = containerRect.top + REORDER_AUTOSCROLL_EDGE_PX;
    if (clientY < topZoneEnd) {
      const depth = (topZoneEnd - clientY) / REORDER_AUTOSCROLL_EDGE_PX;
      return -Math.max(1, Math.round(depth * REORDER_AUTOSCROLL_MAX_PX));
    }
    const bottomZoneStart = containerRect.bottom - REORDER_AUTOSCROLL_EDGE_PX;
    if (clientY > bottomZoneStart) {
      const depth = (clientY - bottomZoneStart) / REORDER_AUTOSCROLL_EDGE_PX;
      return Math.max(1, Math.round(depth * REORDER_AUTOSCROLL_MAX_PX));
    }
    return 0;
  }

  function reorderAutoScrollTick() {
    const state = activeReorderDrag;
    if (!state) return;
    const containerRect = state.container.getBoundingClientRect();
    const delta = reorderAutoScrollDelta(state.lastClientY, containerRect);
    if (delta !== 0 && state.container && typeof state.container.scrollTop === "number") {
      state.container.scrollTop += delta;
    }
  }

  function updateReorderPointerDrag(event) {
    const state = activeReorderDrag;
    if (!state || event.pointerId !== state.pointerId) return;
    state.lastClientY = event.clientY;
    state.item.style.transform = "translateY(" + (event.clientY - state.startClientY) + "px)";

    const others = reorderItems(state.container).filter(function(candidate) {
      return candidate !== state.item;
    });
    const rects = others.map(function(candidate) {
      const rect = candidate.getBoundingClientRect();
      return { top: rect.top, height: rect.height };
    });
    const targetIndex = reorderTargetIndex(event.clientY, rects);
    if (targetIndex === state.currentIndex) {
      return;
    }
    state.currentIndex = targetIndex;
    if (targetIndex >= others.length) {
      state.container.appendChild(state.placeholder);
    } else {
      state.container.insertBefore(state.placeholder, others[targetIndex]);
    }
  }

  function clearReorderTransform(item) {
    if (!item || !item.style) return;
    item.style.transform = "";
  }

  function endReorderPointerDrag(event, commit) {
    const state = activeReorderDrag;
    if (!state) return;
    if (event && event.pointerId !== state.pointerId) return;
    activeReorderDrag = null;

    if (state.scrollIntervalHandle != null) {
      clearInterval(state.scrollIntervalHandle);
    }
    if (state.handle.removeEventListener) {
      state.handle.removeEventListener("pointermove", state.onMove);
      state.handle.removeEventListener("pointerup", state.onUp);
      state.handle.removeEventListener("pointercancel", state.onCancel);
      state.handle.removeEventListener("lostpointercapture", state.onCancel);
    }
    try {
      if (typeof state.handle.releasePointerCapture === "function") {
        state.handle.releasePointerCapture(state.pointerId);
      }
    } catch (_error) {}

    removeManagedClass(state.container, REORDER_DRAGGING_CLASS);
    removeManagedClass(state.item, REORDER_LIFTED_CLASS);
    clearReorderTransform(state.item);
    state.releaseRevalidation();

    if (!commit) {
      if (state.placeholder.parentNode) {
        state.placeholder.parentNode.removeChild(state.placeholder);
      }
      announceNavigation("Reorder cancelled.");
      focusElement(state.handle, true);
      return;
    }

    // Move the real item into the placeholder's slot, then discard the
    // placeholder — insertBefore + removeChild rather than replaceChild,
    // which not every embedding DOM in this codebase's test surfaces
    // implements.
    const placeholderParent = state.placeholder.parentNode;
    if (placeholderParent) {
      placeholderParent.insertBefore(state.item, state.placeholder);
      placeholderParent.removeChild(state.placeholder);
    }
    announceNavigation(reorderAnnouncement("drop", state.item, state.container));
    focusElement(state.handle, true);
    commitReorderResult(state.container, state.item, state.originalParent, state.originalIndex);
  }

  function beginReorderPointerDrag(event, handle, item, container) {
    if (activeReorderDrag || activeReorderKeyboard || reorderContainerPending(container)) {
      return;
    }
    if (typeof event.pointerId !== "number" && typeof event.pointerId !== "string") {
      return;
    }

    // Captured BEFORE the placeholder is inserted, so this is the item's true
    // pre-gesture position — the position restoreReorderItemPosition returns
    // it to if the follow-up submission fails (see commitReorderResult).
    const originalParent = item.parentNode;
    const originalIndex = toArray(originalParent.childNodes).indexOf(item);

    const placeholder = item.cloneNode(true);
    if (placeholder.removeAttribute) {
      placeholder.removeAttribute("id");
      placeholder.removeAttribute(REORDER_ITEM_ATTR);
    }
    addManagedClass(placeholder, REORDER_PLACEHOLDER_CLASS);
    if (placeholder.setAttribute) {
      placeholder.setAttribute("aria-hidden", "true");
      placeholder.setAttribute(REORDER_PLACEHOLDER_ATTR, "true");
    }
    originalParent.insertBefore(placeholder, item);

    activeReorderDrag = {
      pointerId: event.pointerId,
      handle: handle,
      item: item,
      container: container,
      placeholder: placeholder,
      originalParent: originalParent,
      originalIndex: originalIndex,
      startClientY: event.clientY,
      lastClientY: event.clientY,
      currentIndex: -1,
      scrollIntervalHandle: null,
      releaseRevalidation: suspendRevalidation(),
      onMove: null,
      onUp: null,
      onCancel: null,
    };

    addManagedClass(container, REORDER_DRAGGING_CLASS);
    addManagedClass(item, REORDER_LIFTED_CLASS);
    item.style.transform = "translateY(0px)";

    try {
      if (typeof handle.setPointerCapture === "function") {
        handle.setPointerCapture(event.pointerId);
      }
    } catch (_error) {}

    const onMove = function(moveEvent) { updateReorderPointerDrag(moveEvent); };
    const onUp = function(upEvent) { endReorderPointerDrag(upEvent, true); };
    const onCancel = function(cancelEvent) { endReorderPointerDrag(cancelEvent || event, false); };
    activeReorderDrag.onMove = onMove;
    activeReorderDrag.onUp = onUp;
    activeReorderDrag.onCancel = onCancel;
    if (handle.addEventListener) {
      handle.addEventListener("pointermove", onMove);
      handle.addEventListener("pointerup", onUp);
      handle.addEventListener("pointercancel", onCancel);
      handle.addEventListener("lostpointercapture", onCancel);
    }
    activeReorderDrag.scrollIntervalHandle = setInterval(reorderAutoScrollTick, REORDER_AUTOSCROLL_TICK_MS);

    announceNavigation(reorderAnnouncement("grab", item, container));
  }

  document.addEventListener("pointerdown", function(event) {
    if (event.isPrimary === false) return;
    if (event.pointerType === "mouse" && event.button !== 0) return;
    const resolved = reorderHandleForTarget(event.target);
    if (!resolved) return;
    prepareReorderHandle(resolved.handle);
    event.preventDefault();
    beginReorderPointerDrag(event, resolved.handle, resolved.item, resolved.container);
  });

  document.addEventListener("keydown", function(event) {
    const key = event.key;
    if (activeReorderKeyboard) {
      if (key === "Escape") {
        event.preventDefault();
        cancelKeyboardReorder();
        return;
      }
      if (key === " " || key === "Spacebar") {
        event.preventDefault();
        commitKeyboardReorder();
        return;
      }
      if (key === "ArrowUp") {
        event.preventDefault();
        moveKeyboardReorder(-1);
        return;
      }
      if (key === "ArrowDown") {
        event.preventDefault();
        moveKeyboardReorder(1);
        return;
      }
      return;
    }
    if (key !== " " && key !== "Spacebar" && key !== "Enter") return;
    const resolved = reorderHandleForTarget(event.target);
    if (!resolved) return;
    prepareReorderHandle(resolved.handle);
    event.preventDefault();
    beginKeyboardReorder(resolved.handle);
  });

  // A soft navigation mid-gesture is an edge case (revalidation is already
  // paused for the whole gesture, and nothing else initiates navigation
  // without the user releasing the pointer or keyboard first), but this
  // guards it anyway: neither cleanup path assumes its nodes are still
  // attached to the current document, and both always release their
  // revalidation suspension.
  document.addEventListener("gosx:navigate", function() {
    if (activeReorderDrag) {
      endReorderPointerDrag(null, false);
    }
    if (activeReorderKeyboard) {
      cancelKeyboardReorder();
    }
    prepareAllReorderHandles();
  });
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", prepareAllReorderHandles, { once: true });
  }
  prepareAllReorderHandles();

  const reorderAPI = {
    targetIndexForPointer: reorderTargetIndex,
    autoScrollDeltaForPointer: reorderAutoScrollDelta,
  };
  gosxHost.reorder = reorderAPI;
  window.__gosx.reorder = Object.assign(window.__gosx.reorder || {}, reorderAPI);

  // ---------------------------------------------------------------------
  // Live-bound regions (data-gosx-live-*, gosx#217)
  //
  // A live region declares LIVE_SRC_ATTR (a same-origin JSON object) and
  // LIVE_INTERVAL_ATTR (the same whole-seconds/whole-minutes duration
  // subset parseRevalidateInterval accepts) on its own root. Every
  // descendant carrying LIVE_BIND_ATTR — the root itself counts as its own
  // descendant here — has its text kept in sync with one key from that
  // object: a bare top-level key ("mode"), or a "."-separated chain
  // through nested objects for one level of grouping ("status.mode").
  // There is no array-index or selector syntax; a record keyed by an
  // identity the app cares about (a team, a matchup) is the app's own flat
  // key server-side ("score:t42"), the same way the polled object's shape
  // is already the app's choice. Only a string, number, or boolean value
  // is bindable — a missing key, a null value, or an object/array value
  // leaves the element's current text untouched rather than blanking it.
  // LIVE_FLASH_CLASS_ATTR, on the same bound element, names a class the
  // runtime removes and re-adds (retriggering a CSS animation) whenever
  // that element's resolved text actually changes.
  //
  // Unlike REVALIDATE_INTERVAL_ATTR's single page-wide root, MANY
  // independent live regions can exist on one page — findLiveRegionRoots
  // below returns every one — each on its own timer, because each is
  // free to poll a different source at a different cadence (a fast score
  // tick, a slower roster note). And unlike the page-wide poll, each
  // fires its first tick immediately at setup (subject to the same
  // guards below), rather than waiting out a full interval, because the
  // tick's own action here is the cheap text patch itself, not a
  // decision about whether a much heavier full-page revalidation is
  // worth doing.
  //
  // A tick skips entirely, with no fetch and no DOM write, in the same
  // cases a page-wide revalidate tick does (document hidden, a
  // navigation or form submission in flight), plus one more scoped to
  // the region itself: the document's focused element, or an element
  // under an active pointer, anywhere inside the region root. This is
  // the same "never disturb an interaction in progress" contract
  // REORDER_CONTAINER_ATTR's own periodic-revalidation pause
  // (suspendRevalidation above) enforces for its drag gesture, scoped
  // here to the one region instead of the whole page — patching a
  // sibling's score text should never stall because a manager is filling
  // in a form somewhere else on the page, but it must never land under a
  // pointer or a focused control it would otherwise disturb.
  //
  // Each region also sends `If-None-Match` once a response has carried
  // an `ETag`, treating a 304 as "unchanged", and skips re-applying a
  // response whose body is byte-identical to the last one even without
  // an `ETag` — the same body-diff short-circuit pollRevalidateSrc above
  // already uses for the page-wide poll.
  let liveRegionRecords = [];

  // activeLivePointerTarget tracks the element under a currently-held
  // pointer, document-wide, for the interaction guard below — one
  // delegated listener pair for every live region, not one per region,
  // mirroring the reorder section's own single delegated pointerdown
  // listener pattern above. Installed lazily, at most once, the first time
  // setupLiveRegions actually finds a live region — never on a page that
  // carries no data-gosx-live-interval element at all, so a page without
  // this feature adds no listener overhead for it.
  let activeLivePointerTarget = null;
  let livePointerListenersInstalled = false;

  function clearActiveLivePointerTarget() {
    activeLivePointerTarget = null;
  }

  function ensureLivePointerListeners() {
    if (livePointerListenersInstalled) return;
    livePointerListenersInstalled = true;
    document.addEventListener("pointerdown", function(event) {
      activeLivePointerTarget = (event && event.target) || null;
    }, { passive: true });
    document.addEventListener("pointerup", clearActiveLivePointerTarget, { passive: true });
    document.addEventListener("pointercancel", clearActiveLivePointerTarget, { passive: true });
  }

  function elementContainsNode(root, node) {
    if (!root || !node) return false;
    if (root === node) return true;
    return !root.contains || root.contains(node);
  }

  // liveRegionInteractionActive is the region-scoped counterpart to
  // focusedControlBlocksRevalidation above: true while the document's own
  // focused element (ignoring the default "nothing focused" state, where
  // document.activeElement reads as <body> and would otherwise match
  // every region) or the element under an active pointer sits anywhere
  // inside root.
  function liveRegionInteractionActive(root) {
    const active = document.activeElement;
    if (active && active !== document.body && elementContainsNode(root, active)) {
      return true;
    }
    return elementContainsNode(root, activeLivePointerTarget);
  }

  function findLiveRegionRoots() {
    const found = [];
    walkElements(document.body, function(node) {
      if (node.hasAttribute && node.hasAttribute(LIVE_INTERVAL_ATTR)) found.push(node);
      return true;
    });
    return found;
  }

  function findLiveBindElements(root) {
    const found = [];
    walkElements(root, function(node) {
      if (node.hasAttribute && node.hasAttribute(LIVE_BIND_ATTR)) found.push(node);
      return true;
    });
    return found;
  }

  // resolveLiveBindValue walks a "."-separated key chain through plain
  // objects only — never through an array, and never past a missing key
  // — mirroring isValidLiveBindKeyValue's own shape check in
  // ir/validate.go.
  function resolveLiveBindValue(payload, key) {
    if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
      return undefined;
    }
    let cursor = payload;
    for (const segment of String(key || "").split(".")) {
      if (!segment || cursor == null || typeof cursor !== "object" || Array.isArray(cursor)) {
        return undefined;
      }
      if (!Object.prototype.hasOwnProperty.call(cursor, segment)) {
        return undefined;
      }
      cursor = cursor[segment];
    }
    return cursor;
  }

  // liveBindTextValue stringifies only the three JSON scalar kinds a text
  // node can meaningfully show; null, an object, and an array all return
  // null so the caller leaves the element's current text alone instead of
  // rendering "null", "[object Object]", or a JSON dump.
  function liveBindTextValue(value) {
    if (typeof value === "string") return value;
    if (typeof value === "number" || typeof value === "boolean") return String(value);
    return null;
  }

  // flashLiveBindElement removes then re-adds className, editing the class
  // attribute directly rather than through element.classList — the same
  // getAttribute/setAttribute convention setElementClassActive above
  // documents for every other class read/write in this file. Unlike
  // setElementClassActive, this write is unconditional: a flash must
  // restart its CSS animation even when className was already present from
  // a still-running previous flash, so there is no "already has it, skip"
  // short-circuit here.
  function flashLiveBindElement(node, className) {
    if (!className || !node || typeof node.getAttribute !== "function" || typeof node.setAttribute !== "function") {
      return;
    }
    const withoutFlashClass = elementClassNames(node).filter(function(name) {
      return name !== className;
    });
    node.setAttribute("class", withoutFlashClass.join(" "));
    // A real browser needs one reflow between the remove and the re-add for
    // the class's CSS animation to restart. Reading an undefined property
    // on a detached or synthetic element (a test double) is a harmless
    // no-op rather than a throw, so this needs no extra guard.
    void node.offsetWidth;
    withoutFlashClass.push(className);
    node.setAttribute("class", withoutFlashClass.join(" "));
  }

  // applyLiveBindPayload patches every LIVE_BIND_ATTR descendant of root
  // from one polled JSON object, comparing each element's NEXT text
  // directly against its CURRENT text (not a remembered "last applied"
  // value) — the same compare-then-flash-only-on-change contract the
  // gridiron-2000 score sync this feature replaces already used, so a
  // region's very first tick can still apply (silently, unless the value
  // actually differs from what the server rendered) without a separate
  // "baseline" concept. Malformed JSON is a silent failure, same as any
  // other poll body this file cannot use — the next tick tries again.
  function applyLiveBindPayload(root, rawBody) {
    let payload;
    try {
      payload = JSON.parse(rawBody);
    } catch (_error) {
      return;
    }
    for (const node of findLiveBindElements(root)) {
      const next = liveBindTextValue(resolveLiveBindValue(payload, node.getAttribute(LIVE_BIND_ATTR)));
      if (next == null) continue;
      if (String(node.textContent || "").trim() === next) continue;
      node.textContent = next;
      flashLiveBindElement(node, node.getAttribute(LIVE_FLASH_CLASS_ATTR));
    }
  }

  // createLiveRegionRecord validates root's LIVE_SRC_ATTR/LIVE_INTERVAL_ATTR
  // pair and returns a fresh per-region poll record, or null (logging one
  // console warning) for a missing or malformed pair — the same
  // "disabled, not an error" handling setupPageRevalidation gives a bad
  // data-gosx-revalidate-interval.
  function createLiveRegionRecord(root) {
    const rawSrc = root.getAttribute(LIVE_SRC_ATTR);
    if (!rawSrc) {
      console.warn(
        "[gosx] " + LIVE_INTERVAL_ATTR + " requires " + LIVE_SRC_ATTR + " on the same element; "
        + "this live region is disabled",
      );
      return null;
    }
    if (!isSameOriginNavigation(rawSrc, windowLocationHref())) {
      console.warn(
        "[gosx] " + LIVE_SRC_ATTR + " must be same-origin: " + JSON.stringify(String(rawSrc))
        + "; this live region is disabled",
      );
      return null;
    }
    const parsedSrc = navigationURLParts(rawSrc);
    const src = parsedSrc ? parsedSrc.href : "";
    if (!src) return null;
    const rawInterval = root.getAttribute(LIVE_INTERVAL_ATTR);
    let intervalMs = parseRevalidateInterval(rawInterval);
    if (intervalMs == null) {
      console.warn(
        "[gosx] invalid " + LIVE_INTERVAL_ATTR + " value " + JSON.stringify(String(rawInterval || ""))
        + "; this live region is disabled",
      );
      return null;
    }
    if (intervalMs > REVALIDATE_INTERVAL_CLAMP_MS) intervalMs = REVALIDATE_INTERVAL_CLAMP_MS;
    return {
      root: root,
      src: src,
      intervalMs: intervalMs,
      etag: "",
      lastBody: null,
      inFlight: false,
      disposed: false,
      hiddenSince: null,
      timerHandle: null,
    };
  }

  // runLiveRegionTick is the per-record counterpart to runRevalidateTick +
  // pollRevalidateSrc above, generalized to many independent records
  // instead of one shared module-level poll. record.disposed (set by
  // teardownLiveRegions, which always runs before a fresh setupLiveRegions
  // scan) stands in for revalidateGeneration's staleness check: a fetch
  // started before a soft navigation that resolves after it discards its
  // response instead of writing it into a record no longer on
  // liveRegionRecords.
  async function runLiveRegionTick(record) {
    if (record.disposed || record.inFlight) return;
    if (
      documentIsHidden()
      || navigationOrFormSubmissionInFlight()
      || liveRegionInteractionActive(record.root)
    ) {
      return;
    }
    record.inFlight = true;
    try {
      const headers = { Accept: "application/json" };
      if (record.etag) headers["If-None-Match"] = record.etag;
      let response;
      try {
        response = await gosxRuntimeRequest(record.src, { headers: headers, cache: "no-store" });
      } catch (_error) {
        return; // Fetch errors skip silently; the next tick tries again.
      }
      if (record.disposed) return;
      if (response && response.status === 304) return;
      if (!response || !response.ok) return;
      let body;
      try {
        body = await response.text();
      } catch (_error) {
        return;
      }
      if (record.disposed) return;
      const nextEtag = response.headers && typeof response.headers.get === "function"
        ? (response.headers.get("ETag") || "")
        : "";
      if (nextEtag) record.etag = nextEtag;
      if (body === record.lastBody) return;
      record.lastBody = body;
      try {
        applyLiveBindPayload(record.root, body);
      } catch (error) {
        reportNavigationFailure("live region apply", error, { source: record.src });
      }
    } finally {
      record.inFlight = false;
    }
  }

  function teardownLiveRegions() {
    for (const record of liveRegionRecords) {
      record.disposed = true;
      if (record.timerHandle != null) clearInterval(record.timerHandle);
    }
    liveRegionRecords = [];
  }

  // setupLiveRegions scans for every LIVE_INTERVAL_ATTR element on page
  // boot and after every soft navigation (see finalizeNavigation and the
  // initial-document replay below) — the same rescan lifecycle
  // setupPageRevalidation follows for the page-wide poll, generalized to
  // many roots.
  function setupLiveRegions() {
    teardownLiveRegions();
    const roots = findLiveRegionRoots();
    if (roots.length === 0) return;
    ensureLivePointerListeners();
    const records = [];
    for (const root of roots) {
      const record = createLiveRegionRecord(root);
      if (!record) continue;
      records.push(record);
      runLiveRegionTick(record);
      record.timerHandle = setInterval(function() {
        runLiveRegionTick(record);
      }, record.intervalMs);
    }
    liveRegionRecords = records;
  }

  // onLiveRegionsVisibilityChange runs a catch-up tick, per record, the
  // moment the document becomes visible again if at least one full
  // interval elapsed while it was hidden — the same reasoning
  // onRevalidateVisibilityChange documents above, generalized to many
  // independently-timed records instead of one shared interval.
  function onLiveRegionsVisibilityChange() {
    const hidden = documentIsHidden();
    for (const record of liveRegionRecords) {
      if (hidden) {
        if (record.hiddenSince == null) record.hiddenSince = Date.now();
        continue;
      }
      const hiddenSince = record.hiddenSince;
      record.hiddenSince = null;
      if (hiddenSince == null) continue;
      if (Date.now() - hiddenSince >= record.intervalMs) {
        runLiveRegionTick(record);
      }
    }
  }

  document.addEventListener("visibilitychange", onLiveRegionsVisibilityChange);

  function revalidateNavigation(options) {
    // Force a same-URL revalidation through the normal navigation lifecycle.
    // The returned promise rejects without mutating the current document when
    // fetch fails, so callers can choose an appropriate hard-load fallback.
    const opts = Object.assign({
      replace: true,
      preserveScroll: true,
    }, options || {}, {
      force: true,
      revalidate: true,
    });
    return navigate(windowLocationHref(), opts);
  }

  function refreshNavigationState() {
    // Legacy refresh() is synchronous and state-only.
    applyNavigationState();
    return currentNavigationSnapshot();
  }

  function refreshInitialDocumentNavigation() {
    // The navigation host can execute from <head> while the parser is still
    // building <body>. Replay after DOMContentLoaded so initial-document links
    // receive the same current/prefetch state as links installed by soft nav.
    // Bindings depend on that current marker, so refresh them only after the
    // navigation replay. Both operations are synchronous and idempotent.
    refreshNavigationState();
    prefetchManagedLinks("render");
    setupPageRevalidation();
    setupPageHeartbeat();
    setupPageCountdowns();
    setupPageWatchers();
    setupLiveRegions();
    setupPageFilters();
    const actions = window.__gosx && window.__gosx.actions;
    if (actions && typeof actions.refreshBindings === "function") {
      actions.refreshBindings();
    }
  }

  function currentNavigationFetchEpoch() {
    return {
      started: navigationFetchStarted,
      applied: navigationFetchApplied,
    };
  }

  function onClick(event) {
    const anchor = closestLink(event.target);
    if (!shouldHandleLink(anchor, event)) return;
    event.preventDefault();
    const url = new URL(anchor.getAttribute("href"), window.location.href);
    navigate(url.href, { replace: false, sourceLink: anchor }).catch(function(err) {
      console.error("[gosx] navigation failed:", err);
      window.location.href = url.href;
    });
  }

  function onMouseOver(event) {
    const anchor = closestLink(event.target);
    if (!anchor || !anchor.hasAttribute || !anchor.hasAttribute(LINK_ATTR)) return;
    prefetchLink(anchor, "hover");
  }

  function onSubmit(event) {
    const form = event.target;
    if (!shouldHandleForm(form, event)) return;
    event.preventDefault();
    submitForm(form, event.submitter || null).catch(function(err) {
      console.error("[gosx] form submit failed:", err);
      form.submit();
    });
  }

  function onFocusIn(event) {
    const anchor = closestLink(event.target);
    if (!anchor || !anchor.hasAttribute || !anchor.hasAttribute(LINK_ATTR)) return;
    prefetchLink(anchor, "focus");
  }

  function onPopState() {
    navigate(window.location.href, { replace: true, preserveScroll: true }).catch(function(err) {
      console.error("[gosx] popstate navigation failed:", err);
      // The browser already moved the URL. A soft-nav failure here would
      // leave the old DOM under the new URL, so fall back to a hard load
      // of the URL the history entry points at — the same safety net
      // onClick has.
      window.location.reload();
    });
  }

  document.addEventListener("click", onClick);
  document.addEventListener("mouseover", onMouseOver);
  document.addEventListener("focusin", onFocusIn);
  document.addEventListener("submit", onSubmit);
  // onFilterInput (gosx#215) is the delegated listener for every
  // data-gosx-filter input — see its own doc comment above.
  document.addEventListener("input", onFilterInput);
  // onFilterPointerOver/Out (gosx#215) keep filterHoverRow live, alongside
  // — not instead of — onMouseOver above, which is unrelated (prefetch).
  document.addEventListener("mouseover", onFilterPointerOver);
  document.addEventListener("mouseout", onFilterPointerOut);
  document.addEventListener("visibilitychange", onRevalidateVisibilityChange);
  // onHeartbeatVisibilityChange (gosx#216) runs alongside, not instead of,
  // onRevalidateVisibilityChange above — the two features pause and catch
  // up independently of each other.
  document.addEventListener("visibilitychange", onHeartbeatVisibilityChange);
  if (typeof window.addEventListener === "function") {
    window.addEventListener("popstate", onPopState);
    // onTitleFlashWindowFocus (gosx#214) stops the current data-gosx-watch
    // title flash, if any, the moment the visitor focuses this tab/window
    // again — one of the two documented ways a flash ends, alongside its
    // owning condition returning to false (see stopTitleFlash's callers).
    window.addEventListener("focus", onTitleFlashWindowFocus);
  }
  // primeAudioCueContext (gosx#213) constructs the shared AudioContext on
  // the page's first user gesture and never before — a pointerdown or a
  // keydown, whichever comes first. Each listener is `once`: it removes
  // itself after its own first firing, so a page the visitor only ever
  // clicks on still primes audio, and one that only ever types still
  // primes audio, with no listener left registered past the first of
  // either.
  document.addEventListener("pointerdown", primeAudioCueContext, { once: true, passive: true });
  document.addEventListener("keydown", primeAudioCueContext, { once: true, passive: true });

  setNavigationState({
    phase: "idle",
    currentURL: String(window.location && window.location.href || ""),
    pendingURL: "",
  }, "init");
  prefetchManagedLinks("render");
  setupPageRevalidation();
  setupPageHeartbeat();
  setupPageCountdowns();
  setupPageWatchers();
  setupLiveRegions();
  setupPageFilters();
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", refreshInitialDocumentNavigation, { once: true });
  }

  const navigationAPI = {
    navigate: navigate,
    submitAction: submitAction,
    getState: currentNavigationSnapshot,
    getFetchEpoch: currentNavigationFetchEpoch,
    refresh: refreshNavigationState,
    refreshState: refreshNavigationState,
    revalidate: revalidateNavigation,
    // debugCueLog is a small, honest test/debug hook (gosx#213): a copy of
    // every {cue, at} entry recorded the moment a named tone actually
    // played (see recordAudioCueDebug above), not merely requested.
    // Array.from (rather than .slice()) rebuilds the copy through
    // whatever Array constructor this script's own execution realm
    // exposes as the global "Array" identifier — for the embedded
    // navigation runtime that is the same realm as everything else on the
    // page, but it also keeps a caller executing this script inside a
    // separate realm (for example a test harness's vm context) from
    // handing back an array a strict cross-realm equality check would
    // reject over prototype identity alone.
    debugCueLog: function() {
      return Array.from(audioCueDebugLog);
    },
    // debugWatchActive is the same kind of hook for gosx#214: whether
    // watchActiveState currently remembers `key` (a watch record's own
    // "id:<id>" or "pos:<index>" key, see buildWatchState) as active.
    debugWatchActive: function(key) {
      return watchActiveState.get(key) === true;
    },
  };
  // Keep the original global for compatibility while publishing the
  // navigation runtime through the shared GoSX namespace. This lets optional
  // surfaces observe or initiate navigation without depending on a private
  // global name.
  gosxHost.navigation = navigationAPI;
  window.__gosx.navigation = navigationAPI;
  gosxHostCompatibility.install("__gosx_page_nav", navigationAPI);
  gosxHostCompatibility.install("__gosx_submit_action", submitAction);
})();
