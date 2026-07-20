(function() {
  "use strict";

  if (window.__gosx_page_nav && typeof window.__gosx_page_nav.navigate === "function") {
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
  const FORM_MODE_ATTR = "data-gosx-form-mode";
  const FORM_STATE_ATTR = "data-gosx-form-state";
  const FORM_PENDING_ATTR = "data-gosx-pending";
  const PREFETCH_ATTR = "data-gosx-prefetch";
  const NAV_STATE_ATTR = "data-gosx-navigation-state";
  const NAV_CURRENT_PATH_ATTR = "data-gosx-navigation-current-path";
  const NAV_PENDING_URL_ATTR = "data-gosx-navigation-pending-url";
  const MAIN_ATTR = "data-gosx-main";
  const ANNOUNCE_ATTR = "data-gosx-announce";
  const ANNOUNCER_ATTR = "data-gosx-announcer";
  const MANAGED_FOCUS_ATTR = "data-gosx-focus-managed";
  const URL_ATTRS = ["href", "src", "action", "poster"];
  const SUBMITTER_ATTRS = {
    formAction: "formaction",
    formMethod: "formmethod",
    formTarget: "formtarget",
  };
  const scriptCache = window.__gosx_loaded_scripts || new Map();
  const pageCache = window.__gosx_page_cache || new Map();
  let navigationState = {
    phase: "idle",
    currentURL: String(window.location && window.location.href || ""),
    pendingURL: "",
  };
  let navigationSequence = 0;
  let activeNavigationController = null;
  let announceSeq = 0;
  let navigationFrameSequence = 0;
  window.__gosx_loaded_scripts = scriptCache;
  window.__gosx_page_cache = pageCache;

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
      origin: parsed.origin,
      path: normalizedNavigationPath(parsed.pathname),
      search: String(parsed.search || ""),
      href: parsed.href,
    };
  }

  function sameNavigationURL(left, right) {
    return !!left && !!right && left.origin === right.origin && left.path === right.path && left.search === right.search;
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

  function managedForms(root) {
    return collectElements(root, function(node) {
      return node.hasAttribute && node.hasAttribute(FORM_ATTR);
    });
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
    if (role === "bootstrap" && typeof window.__gosx_bootstrap_page === "function") {
      return false;
    }
    const cacheKey = (load === "dom" ? "dom:" : "eval:") + src;
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

    const promise = load === "dom"
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

  async function disposeCurrentPage(reuseIDs) {
    if (typeof window.__gosx_dispose_page === "function") {
      await window.__gosx_dispose_page(reuseIDs);
    }
  }

  async function bootstrapCurrentPage(bootstrapLoadedNow, reuseIDs) {
    if (!bootstrapLoadedNow && typeof window.__gosx_bootstrap_page === "function") {
      await window.__gosx_bootstrap_page(reuseIDs);
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
    return !!url && !!current && url.origin === current.origin;
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
    if (!form || !form.hasAttribute || !form.hasAttribute(FORM_ATTR)) return false;
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

  async function submitManagedGetForm(url, method, formData) {
    await navigate(formNavigationURL(url, formData).href, { replace: false });
    dispatchManagedFormNavigate(url.href, method);
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
    const result = await parseJSONResponse(response);
    applyManagedFormData(result);
    if (result && result.redirect) {
      await navigate(new URL(result.redirect, window.location.href).href, { replace: false });
    }
    dispatchManagedFormResult(url.href, method, response, result);
  }

  async function submitForm(form, submitter) {
    if (!form) return;

    const method = formSubmissionMethod(form, submitter);
    const action = formSubmissionAction(form, submitter) || window.location.href;
    const url = new URL(action, window.location.href);
    const formData = serializeForm(form, submitter);
    const previous = captureManagedFormState(form);

    setManagedFormPending(form);

    try {
      if (method === "GET") {
        await submitManagedGetForm(url, method, formData);
        return;
      }
      await submitManagedActionForm(url, method, formData);
    } catch (err) {
      console.error("[gosx] form action failed:", err);
      reportNavigationFailure("form action", err, {
        source: url.href,
        telemetry: { url: url.href, method: method },
      });
      nativeSubmitForm(form, submitter);
      return;
    } finally {
      restoreManagedFormState(form, previous);
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
    if (!form) return;
    const previousManaged = form.getAttribute(FORM_ATTR);
    if (previousManaged == null) {
      return;
    }
    form.removeAttribute(FORM_ATTR);
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
      form.setAttribute(FORM_ATTR, previousManaged);
    }
  }

  function parseDocument(html) {
    if (typeof DOMParser === "undefined") {
      throw new Error("DOMParser is not available");
    }
    return new DOMParser().parseFromString(html, "text/html");
  }

  async function fetchPage(url, signal) {
    const key = String(url);
    if (pageCache.has(key)) {
      return pageCache.get(key);
    }

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
      return {
        html: await response.text(),
        url: response.url || key,
      };
    })();

    pageCache.set(key, request);
    try {
      return await request;
    } catch (err) {
      pageCache.delete(key);
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
    const sequence = ++navigationSequence;
    if (activeNavigationController && typeof activeNavigationController.abort === "function") {
      activeNavigationController.abort();
    }
    activeNavigationController = typeof AbortController === "function" ? new AbortController() : null;
    const signal = activeNavigationController ? activeNavigationController.signal : null;
    startNavigation(url);
    try {
      const page = await resolveNavigationPage(url, signal);
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
      finalizeNavigation(page.nextURL, opts, resolveNavigationA11y(page.nextURL));
      return true;
    } catch (err) {
      if (!navigationIsCurrent(sequence) || navigationAbortError(err)) return false;
      failNavigation(err, url);
      throw err;
    } finally {
      if (navigationIsCurrent(sequence)) activeNavigationController = null;
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
    const page = await fetchPage(url, signal);
    const nextURL = page.url || url;
    return {
      nextURL: nextURL,
      nextDoc: parseDocument(page.html),
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
    const bootstrapLoadedNow = await ensureManagedScripts(nextDoc, nextURL, managedScripts);
    if (isCurrent && !isCurrent()) return;
    await bootstrapCurrentPage(bootstrapLoadedNow, reuseIDs);
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
    });
  }

  document.addEventListener("click", onClick);
  document.addEventListener("mouseover", onMouseOver);
  document.addEventListener("focusin", onFocusIn);
  document.addEventListener("submit", onSubmit);
  if (typeof window.addEventListener === "function") {
    window.addEventListener("popstate", onPopState);
  }

  setNavigationState({
    phase: "idle",
    currentURL: String(window.location && window.location.href || ""),
    pendingURL: "",
  }, "init");
  prefetchManagedLinks("render");

  const navigationAPI = {
    navigate: navigate,
    submitAction: submitAction,
    getState: currentNavigationSnapshot,
    refresh: function() {
      applyNavigationState();
      return currentNavigationSnapshot();
    },
  };
  // Keep the original global for compatibility while publishing the
  // navigation runtime through the shared GoSX namespace. This lets optional
  // surfaces observe or initiate navigation without depending on a private
  // global name.
  window.__gosx_page_nav = navigationAPI;
  window.__gosx = window.__gosx || {};
  window.__gosx.navigation = navigationAPI;
  window.__gosx_submit_action = submitAction;
})();
