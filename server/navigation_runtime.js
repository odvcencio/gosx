(function() {
  "use strict";

  if (window.__gosx_page_nav && typeof window.__gosx_page_nav.navigate === "function") {
    return;
  }

  const HEAD_START = "gosx-head-start";
  const HEAD_END = "gosx-head-end";
  const SCRIPT_ROLE = "data-gosx-script";
  const LINK_ATTR = "data-gosx-link";
  const PREFETCH_ATTR = "data-gosx-prefetch";
  const scriptCache = window.__gosx_loaded_scripts || new Map();
  const pageCache = window.__gosx_page_cache || new Map();
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

  function cloneIntoDocument(node) {
    if (node && typeof node.cloneNode === "function") {
      return node.cloneNode(true);
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

  function replaceManagedHead(nextDoc) {
    document.title = nextDoc.title || "";

    const currentMarkers = ensureHeadMarkers();
    const head = document.head;
    let children = toArray(head.childNodes);
    let startIdx = children.indexOf(currentMarkers.start);
    let endIdx = children.indexOf(currentMarkers.end);

    while (endIdx > startIdx + 1) {
      head.removeChild(children[startIdx + 1]);
      children = toArray(head.childNodes);
      endIdx = children.indexOf(currentMarkers.end);
    }

    const nextNodes = collectManagedHeadNodes(nextDoc.head);
    for (const node of nextNodes) {
      head.insertBefore(cloneIntoDocument(node), currentMarkers.end);
    }
  }

  function attributeEntries(element) {
    if (!element || !element.attributes) return [];
    if (typeof element.attributes.entries === "function") {
      return Array.from(element.attributes.entries()).map(([name, value]) => ({ name, value }));
    }
    return Array.from(element.attributes).map((attr) => ({ name: attr.name, value: attr.value }));
  }

  function replaceBody(nextDoc) {
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
      body.appendChild(cloneIntoDocument(child));
    }
  }

  function collectManagedScripts(root) {
    const found = [];
    function walk(node) {
      if (!node || !node.childNodes) return;
      for (const child of toArray(node.childNodes)) {
        if (isElement(child, "SCRIPT") && child.hasAttribute(SCRIPT_ROLE) && child.getAttribute("src")) {
          found.push({
            role: child.getAttribute(SCRIPT_ROLE),
            src: child.getAttribute("src"),
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

  async function ensureManagedScripts(nextDoc) {
    const scripts = collectManagedScripts(nextDoc.head).concat(collectManagedScripts(nextDoc.body));
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
    replaceManagedHead(nextDoc);
    replaceBody(nextDoc);
    updateHistory(nextURL, !!opts.replace);

    const bootstrapLoadedNow = await ensureManagedScripts(nextDoc);
    await bootstrapCurrentPage(bootstrapLoadedNow);

    if (!opts.preserveScroll && typeof window.scrollTo === "function") {
      window.scrollTo(0, 0);
    }

    if (typeof document.dispatchEvent === "function" && typeof CustomEvent === "function") {
      document.dispatchEvent(new CustomEvent("gosx:navigate", {
        detail: {
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
  if (typeof window.addEventListener === "function") {
    window.addEventListener("popstate", onPopState);
  }

  window.__gosx_page_nav = {
    navigate: navigate,
  };
})();
