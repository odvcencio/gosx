(function() {
  "use strict";

  if (window.__gosx_page_nav && typeof window.__gosx_page_nav.navigate === "function") {
    return;
  }

  const HEAD_START = "gosx-head-start";
  const HEAD_END = "gosx-head-end";
  const SCRIPT_ROLE = "data-gosx-script";
  const LINK_ATTR = "data-gosx-link";
  const FORM_ATTR = "data-gosx-form";
  const PREFETCH_ATTR = "data-gosx-prefetch";
  const MAIN_ATTR = "data-gosx-main";
  const ANNOUNCE_ATTR = "data-gosx-announce";
  const ANNOUNCER_ATTR = "data-gosx-announcer";
  const MANAGED_FOCUS_ATTR = "data-gosx-focus-managed";
  const URL_ATTRS = ["href", "src", "action", "poster"];
  const scriptCache = window.__gosx_loaded_scripts || new Map();
  const pageCache = window.__gosx_page_cache || new Map();
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

  function absolutizeURL(value, baseURL) {
    if (!value) return value;
    const trimmed = String(value).trim();
    if (!trimmed || trimmed[0] === "#" || trimmed.startsWith("data:") || trimmed.startsWith("javascript:")) {
      return value;
    }
    try {
      return new URL(trimmed, baseURL || window.location.href).toString();
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
    const stack = [];
    if (root) {
      stack.push(root);
    }

    while (stack.length > 0) {
      const node = stack.pop();
      if (!node || node.nodeType !== 1) {
        continue;
      }
      if (predicate(node)) {
        return node;
      }

      const children = toArray(node.childNodes);
      for (let i = children.length - 1; i >= 0; i--) {
        stack.push(children[i]);
      }
    }

    return null;
  }

  function normalizeTextValue(value) {
    return String(value || "").replace(/\s+/g, " ").trim();
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
    if (!anchor || !anchor.hasAttribute || !anchor.hasAttribute(LINK_ATTR)) return false;
    if (event.defaultPrevented) return false;
    if (event.button !== 0) return false;
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return false;
    if (anchor.getAttribute("target") || anchor.hasAttribute("download")) return false;

    const href = anchor.getAttribute("href");
    if (!href || href.startsWith("#")) return false;

    const url = new URL(href, window.location.href);
    return url.origin === window.location.origin;
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
    if (form.getAttribute("target")) return false;

    const method = String(form.getAttribute("method") || "GET").toUpperCase();
    if (method !== "POST") return false;

    const action = form.getAttribute("action") || window.location.href;
    const url = new URL(action, window.location.href);
    return url.origin === window.location.origin;
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

  async function submitForm(form, submitter) {
    if (!form) return;

    const action = form.getAttribute("action") || window.location.href;
    const url = new URL(action, window.location.href);
    const formData = serializeForm(form, submitter);
    const previousPending = form.getAttribute("data-gosx-pending");

    form.setAttribute("data-gosx-pending", "true");

    try {
      const csrfToken = formCSRFToken(formData);
      const response = await fetch(url.href, {
        method: "POST",
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
        result = await response.json();
      } catch (_) {
        result = null;
      }

      if (result && result.data && typeof window.__gosx_set_input_batch === "function") {
        window.__gosx_set_input_batch(JSON.stringify(result.data));
      }

      if (result && result.redirect) {
        await navigate(new URL(result.redirect, window.location.href).href, { replace: false });
      }

      if (typeof document.dispatchEvent === "function" && typeof CustomEvent === "function") {
        document.dispatchEvent(new CustomEvent("gosx:form:result", {
          detail: {
            action: url.href,
            ok: response.ok,
            status: response.status,
            result: result,
          },
        }));
      }
    } catch (err) {
      console.error("[gosx] form action failed:", err);
      form.submit();
      return;
    } finally {
      if (previousPending == null) {
        form.removeAttribute("data-gosx-pending");
      } else {
        form.setAttribute("data-gosx-pending", previousPending);
      }
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

  function prefetchLink(anchor) {
    if (!anchor || !anchor.getAttribute) return;
    if (anchor.getAttribute(PREFETCH_ATTR) === "off") return;
    const url = new URL(anchor.getAttribute("href"), window.location.href);
    fetchPage(url.href).catch(function() {});
  }

  async function navigate(url, options) {
    const opts = options || {};
    const page = await fetchPage(url);
    const nextURL = page.url || url;
    const html = page.html;
    const nextDoc = parseDocument(html);

    await disposeCurrentPage();
    await replaceManagedHead(nextDoc, nextURL);
    replaceBody(nextDoc, nextURL);
    updateHistory(nextURL, !!opts.replace);

    const bootstrapLoadedNow = await ensureManagedScripts(nextDoc, nextURL);
    await bootstrapCurrentPage(bootstrapLoadedNow);

    const a11y = resolveNavigationA11y(nextURL);
    if (!opts.preserveScroll) {
      if (a11y.hashTarget && typeof a11y.hashTarget.scrollIntoView === "function") {
        a11y.hashTarget.scrollIntoView();
      } else if (typeof window.scrollTo === "function") {
        window.scrollTo(0, 0);
      }
    }
    focusElement(a11y.focusTarget, true);
    const announcement = announceNavigation(a11y.announcement);

    if (typeof document.dispatchEvent === "function" && typeof CustomEvent === "function") {
      document.dispatchEvent(new CustomEvent("gosx:navigate", {
        detail: {
          announcement: announcement,
          focusTargetId: a11y.focusTarget && a11y.focusTarget.getAttribute ? (a11y.focusTarget.getAttribute("id") || "") : "",
          url: nextURL,
          replace: !!opts.replace,
        },
      }));
    }
  }

  function onClick(event) {
    const anchor = closestLink(event.target);
    if (!shouldHandleLink(anchor, event)) return;
    event.preventDefault();
    const url = new URL(anchor.getAttribute("href"), window.location.href);
    navigate(url.href, { replace: false }).catch(function(err) {
      console.error("[gosx] navigation failed:", err);
      window.location.href = url.href;
    });
  }

  function onMouseOver(event) {
    const anchor = closestLink(event.target);
    if (!anchor || !anchor.hasAttribute || !anchor.hasAttribute(LINK_ATTR)) return;
    prefetchLink(anchor);
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
    prefetchLink(anchor);
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

  window.__gosx_page_nav = {
    navigate: navigate,
  };
})();
