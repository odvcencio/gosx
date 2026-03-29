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
  let announceSeq = 0;
  window.__gosx_loaded_scripts = scriptCache;
  window.__gosx_page_cache = pageCache;

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

      if (typeof requestAnimationFrame === "function") {
        requestAnimationFrame(finalizeIfReady);
      } else {
        setTimeout(finalizeIfReady, 16);
      }
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

      const clone = cloneIntoDocument(node, baseURL);
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

  function replaceBody(nextDoc, baseURL) {
    const body = document.body;
    const nextBody = nextDoc.body;
    const existingAttrs = attributeEntries(body);
    for (const entry of existingAttrs) {
      body.removeAttribute(entry.name);
    }
    for (const entry of attributeEntries(nextBody)) {
      body.setAttribute(entry.name, entry.value);
    }

    while (body.firstChild) {
      body.removeChild(body.firstChild);
    }

    const children = toArray(nextBody.childNodes);
    for (const child of children) {
      if (isElement(child, "SCRIPT") && child.hasAttribute(SCRIPT_ROLE) && child.getAttribute("src")) {
        continue;
      }
      body.appendChild(cloneIntoDocument(child, baseURL));
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
          });
        }
        walk(child);
      }
    }
    walk(root);
    return found;
  }

  async function loadManagedScript(role, src) {
    if (!src) return false;
    if (role === "bootstrap" && typeof window.__gosx_bootstrap_page === "function") {
      return false;
    }
    if (scriptCache.has(src)) {
      await scriptCache.get(src);
      return false;
    }

    const promise = (async function() {
      const resp = await fetch(src);
      if (!resp.ok) {
        throw new Error("script fetch failed: " + src + " (" + resp.status + ")");
      }
      const source = await resp.text();
      (0, eval)(String(source) + "\n//# sourceURL=" + src);
    })();

    scriptCache.set(src, promise);
    await promise;
    return role === "bootstrap";
  }

  async function ensureManagedScripts(nextDoc, baseURL) {
    const scripts = collectManagedScripts(nextDoc.head, baseURL).concat(collectManagedScripts(nextDoc.body, baseURL));
    scripts.sort(function(a, b) {
      const order = { "wasm-exec": 0, patch: 1, bootstrap: 2 };
      const left = Object.prototype.hasOwnProperty.call(order, a.role) ? order[a.role] : 99;
      const right = Object.prototype.hasOwnProperty.call(order, b.role) ? order[b.role] : 99;
      return left - right;
    });

    let bootstrapLoadedNow = false;
    for (const script of scripts) {
      if (await loadManagedScript(script.role, script.src)) {
        bootstrapLoadedNow = true;
      }
    }
    return bootstrapLoadedNow;
  }

  async function disposeCurrentPage() {
    if (typeof window.__gosx_dispose_page === "function") {
      await window.__gosx_dispose_page();
    }
  }

  async function bootstrapCurrentPage(bootstrapLoadedNow) {
    if (!bootstrapLoadedNow && typeof window.__gosx_bootstrap_page === "function") {
      await window.__gosx_bootstrap_page();
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
    const property = submitterProperty(submitter, name);
    if (property) {
      return property;
    }
    const attrName = submitterAttributeName(name);
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
    const response = await fetch(url.href, {
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

  async function fetchPage(url) {
    const key = String(url);
    if (pageCache.has(key)) {
      return pageCache.get(key);
    }

    const request = (async function() {
      const response = await fetch(url, {
        headers: {
          Accept: "text/html",
          "X-GoSX-Navigation": "1",
        },
      });
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

  async function navigate(url, options) {
    const opts = options || {};
    startNavigation(url);
    try {
      const page = await resolveNavigationPage(url);
      await applyNavigatedPage(page.nextDoc, page.nextURL, !!opts.replace);
      completeNavigation(page.nextURL);
      finalizeNavigation(page.nextURL, opts, resolveNavigationA11y(page.nextURL));
    } catch (err) {
      failNavigation();
      throw err;
    }
  }

  function startNavigation(url) {
    setNavigationState({
      phase: "pending",
      pendingURL: String(url || ""),
    }, "navigate:start");
  }

  function completeNavigation(url) {
    setNavigationState({
      phase: "idle",
      currentURL: String(url),
      pendingURL: "",
    }, "navigate:complete");
    prefetchManagedLinks("render");
  }

  function failNavigation() {
    setNavigationState({
      phase: "idle",
      pendingURL: "",
    }, "navigate:error");
  }

  async function resolveNavigationPage(url) {
    const page = await fetchPage(url);
    const nextURL = page.url || url;
    return {
      nextURL: nextURL,
      nextDoc: parseDocument(page.html),
    };
  }

  async function applyNavigatedPage(nextDoc, nextURL, replace) {
    await disposeCurrentPage();
    await replaceManagedHead(nextDoc, nextURL);
    replaceBody(nextDoc, nextURL);
    updateHistory(nextURL, !!replace);
    const bootstrapLoadedNow = await ensureManagedScripts(nextDoc, nextURL);
    await bootstrapCurrentPage(bootstrapLoadedNow);
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

  window.__gosx_page_nav = {
    navigate: navigate,
    getState: currentNavigationSnapshot,
    refresh: function() {
      applyNavigationState();
      return currentNavigationSnapshot();
    },
  };
})();
