  const gosxEnvironmentListeners = new Set();
  const gosxDocumentListeners = new Set();
  let gosxEnvironmentState = null;
  let gosxDocumentState = null;
  let gosxEnvironmentObserversInstalled = false;
  let gosxDocumentObserversInstalled = false;
  let gosxDocumentRefreshScheduled = false;
  let gosxDocumentRefreshReason = "";
  const GOSX_DOCUMENT_CSS_LAYERS = ["global", "layout", "page", "runtime"];

  function gosxArrayFrom(listLike) {
    return Array.prototype.slice.call(listLike || []);
  }

  function gosxMediaQueryList(query) {
    if (!query || typeof window.matchMedia !== "function") {
      return null;
    }
    try {
      return window.matchMedia(query);
    } catch (_error) {
      return null;
    }
  }

  function gosxMediaQueryMatches(query) {
    const media = gosxMediaQueryList(query);
    return Boolean(media && media.matches);
  }

  function gosxNavigatorConnection() {
    const navigatorRef = window && window.navigator ? window.navigator : null;
    if (!navigatorRef || typeof navigatorRef !== "object") {
      return null;
    }
    return navigatorRef.connection || navigatorRef.mozConnection || navigatorRef.webkitConnection || null;
  }

  function gosxNumber(value, fallback) {
    const number = Number(value);
    return Number.isFinite(number) ? number : fallback;
  }

  function gosxPointerMode() {
    if (gosxMediaQueryMatches("(pointer: fine)")) {
      return "fine";
    }
    if (gosxMediaQueryMatches("(pointer: coarse)") || gosxMediaQueryMatches("(any-pointer: coarse)")) {
      return "coarse";
    }
    return "none";
  }

  function gosxContrastMode() {
    if (gosxMediaQueryMatches("(prefers-contrast: more)")) {
      return "more";
    }
    if (gosxMediaQueryMatches("(prefers-contrast: less)")) {
      return "less";
    }
    return "no-preference";
  }

  function gosxColorSchemeMode() {
    if (gosxMediaQueryMatches("(prefers-color-scheme: dark)")) {
      return "dark";
    }
    if (gosxMediaQueryMatches("(prefers-color-scheme: light)")) {
      return "light";
    }
    return "no-preference";
  }

  function gosxEnvironmentSnapshot() {
    const navigatorRef = window && window.navigator ? window.navigator : {};
    const connection = gosxNavigatorConnection();
    const visualViewport = window.visualViewport || null;
    const pageVisible = String(document && document.visibilityState || "visible").toLowerCase() !== "hidden";
    const coarsePointer = gosxPointerMode() === "coarse";
    const hover = gosxMediaQueryMatches("(hover: hover)") || gosxMediaQueryMatches("(any-hover: hover)");
    const saveData = Boolean(connection && connection.saveData);
    const reducedData = saveData || gosxMediaQueryMatches("(prefers-reduced-data: reduce)");
    const deviceMemory = Math.max(0, gosxNumber(navigatorRef && navigatorRef.deviceMemory, 0));
    const hardwareConcurrency = Math.max(0, Math.floor(gosxNumber(navigatorRef && navigatorRef.hardwareConcurrency, 0)));
    const maxTouchPoints = Math.max(0, Math.floor(gosxNumber(navigatorRef && navigatorRef.maxTouchPoints, 0)));
    const lowPower = reducedData || (coarsePointer && ((deviceMemory > 0 && deviceMemory <= 4) || (hardwareConcurrency > 0 && hardwareConcurrency <= 4)));
    const viewportWidth = Math.max(0, gosxNumber(window.innerWidth, document && document.documentElement && document.documentElement.clientWidth || 0));
    const viewportHeight = Math.max(0, gosxNumber(window.innerHeight, document && document.documentElement && document.documentElement.clientHeight || 0));

    return {
      pageVisible,
      pointer: gosxPointerMode(),
      coarsePointer,
      hover,
      reducedMotion: gosxMediaQueryMatches("(prefers-reduced-motion: reduce)"),
      reducedData,
      saveData,
      contrast: gosxContrastMode(),
      colorScheme: gosxColorSchemeMode(),
      devicePixelRatio: Math.max(1, gosxNumber(window.devicePixelRatio, 1)),
      viewportWidth,
      viewportHeight,
      visualViewportWidth: Math.max(0, gosxNumber(visualViewport && visualViewport.width, viewportWidth)),
      visualViewportHeight: Math.max(0, gosxNumber(visualViewport && visualViewport.height, viewportHeight)),
      visualViewportOffsetLeft: gosxNumber(visualViewport && visualViewport.offsetLeft, 0),
      visualViewportOffsetTop: gosxNumber(visualViewport && visualViewport.offsetTop, 0),
      visualViewportActive: Boolean(visualViewport),
      deviceMemory,
      hardwareConcurrency,
      maxTouchPoints,
      lowPower,
    };
  }

  function gosxEnvironmentChanged(prev, next) {
    if (!prev || !next) {
      return true;
    }
    const keys = Object.keys(next);
    for (const key of keys) {
      if (prev[key] !== next[key]) {
        return true;
      }
    }
    return false;
  }

  function cloneGosxEnvironment(state) {
    return state ? Object.assign({}, state) : null;
  }

  function applyGosxEnvironmentState(state) {
    const root = document.documentElement || document.body;
    const body = document.body || root;
    if (!root || !state) {
      return;
    }
    setAttrValue(root, "data-gosx-env-page-visible", state.pageVisible ? "true" : "false");
    setAttrValue(root, "data-gosx-env-pointer", state.pointer);
    setAttrValue(root, "data-gosx-env-hover", state.hover ? "true" : "false");
    setAttrValue(root, "data-gosx-env-reduced-motion", state.reducedMotion ? "true" : "false");
    setAttrValue(root, "data-gosx-env-reduced-data", state.reducedData ? "true" : "false");
    setAttrValue(root, "data-gosx-env-contrast", state.contrast);
    setAttrValue(root, "data-gosx-env-color-scheme", state.colorScheme);
    setAttrValue(root, "data-gosx-env-low-power", state.lowPower ? "true" : "false");
    setAttrValue(root, "data-gosx-env-visual-viewport", state.visualViewportActive ? "true" : "false");
    setStyleValue(root.style, "--gosx-env-viewport-width", state.viewportWidth + "px");
    setStyleValue(root.style, "--gosx-env-viewport-height", state.viewportHeight + "px");
    setStyleValue(root.style, "--gosx-env-visual-viewport-width", state.visualViewportWidth + "px");
    setStyleValue(root.style, "--gosx-env-visual-viewport-height", state.visualViewportHeight + "px");
    setStyleValue(root.style, "--gosx-env-visual-viewport-offset-left", state.visualViewportOffsetLeft + "px");
    setStyleValue(root.style, "--gosx-env-visual-viewport-offset-top", state.visualViewportOffsetTop + "px");
    setStyleValue(root.style, "--gosx-env-device-pixel-ratio", String(state.devicePixelRatio));
    if (body && body !== root) {
      setAttrValue(body, "data-gosx-env-page-visible", state.pageVisible ? "true" : "false");
      setAttrValue(body, "data-gosx-env-reduced-motion", state.reducedMotion ? "true" : "false");
      setAttrValue(body, "data-gosx-env-reduced-data", state.reducedData ? "true" : "false");
      setAttrValue(body, "data-gosx-env-low-power", state.lowPower ? "true" : "false");
    }
  }

  function dispatchGosxEnvironment(reason) {
    if (typeof CustomEvent !== "function" || !document || typeof document.dispatchEvent !== "function") {
      return;
    }
    document.dispatchEvent(new CustomEvent("gosx:environment", {
      detail: {
        reason: reason || "refresh",
        state: cloneGosxEnvironment(gosxEnvironmentState),
      },
    }));
  }

  function refreshGosxEnvironmentState(reason) {
    const next = gosxEnvironmentSnapshot();
    const changed = gosxEnvironmentChanged(gosxEnvironmentState, next);
    gosxEnvironmentState = next;
    applyGosxEnvironmentState(next);
    if (!changed) {
      return cloneGosxEnvironment(next);
    }
    for (const listener of Array.from(gosxEnvironmentListeners)) {
      try {
        listener(cloneGosxEnvironment(next), reason || "refresh");
      } catch (error) {
        console.error("[gosx] environment listener failed:", error);
      }
    }
    dispatchGosxEnvironment(reason);
    return cloneGosxEnvironment(next);
  }

  function installGosxEnvironmentObservers() {
    if (gosxEnvironmentObserversInstalled) {
      return;
    }
    gosxEnvironmentObserversInstalled = true;

    const refresh = function(reason) {
      refreshGosxEnvironmentState(reason);
    };

    if (document && typeof document.addEventListener === "function") {
      document.addEventListener("visibilitychange", function() {
        refresh("visibility");
      });
    }
    if (typeof window.addEventListener === "function") {
      window.addEventListener("resize", function() {
        refresh("viewport");
      });
      window.addEventListener("orientationchange", function() {
        refresh("viewport");
      });
      window.addEventListener("pageshow", function() {
        refresh("pageshow");
      });
    }
    if (window.visualViewport && typeof window.visualViewport.addEventListener === "function") {
      window.visualViewport.addEventListener("resize", function() {
        refresh("visual-viewport");
      });
      window.visualViewport.addEventListener("scroll", function() {
        refresh("visual-viewport");
      });
    }

    const queries = [
      "(prefers-reduced-motion: reduce)",
      "(prefers-reduced-data: reduce)",
      "(prefers-contrast: more)",
      "(prefers-contrast: less)",
      "(prefers-color-scheme: dark)",
      "(prefers-color-scheme: light)",
      "(pointer: fine)",
      "(pointer: coarse)",
      "(any-pointer: coarse)",
      "(hover: hover)",
      "(any-hover: hover)",
    ];
    for (const query of queries) {
      const media = gosxMediaQueryList(query);
      if (!media) {
        continue;
      }
      const onChange = function() {
        refresh("media");
      };
      if (typeof media.addEventListener === "function") {
        media.addEventListener("change", onChange);
      } else if (typeof media.addListener === "function") {
        media.addListener(onChange);
      }
    }

    const connection = gosxNavigatorConnection();
    if (connection && typeof connection.addEventListener === "function") {
      connection.addEventListener("change", function() {
        refresh("connection");
      });
    }
  }

  function observeGosxEnvironment(listener, options) {
    if (typeof listener !== "function") {
      return function() {};
    }
    installGosxEnvironmentObservers();
    gosxEnvironmentListeners.add(listener);
    if (!gosxEnvironmentState) {
      refreshGosxEnvironmentState("init");
    }
    if (!options || options.immediate !== false) {
      listener(cloneGosxEnvironment(gosxEnvironmentState), "init");
    }
    return function() {
      gosxEnvironmentListeners.delete(listener);
    };
  }

  function gosxManagedHeadMarkers() {
    const children = gosxArrayFrom(document.head && document.head.childNodes);
    let start = null;
    let end = null;
    for (const child of children) {
      if (!child || child.nodeType !== 1) {
        continue;
      }
      if (String(child.tagName || "").toUpperCase() !== "META") {
        continue;
      }
      const name = String(child.getAttribute && child.getAttribute("name") || "");
      if (name === "gosx-head-start") {
        start = child;
      } else if (name === "gosx-head-end") {
        end = child;
      }
    }
    return { start, end };
  }

  function gosxDocumentChildrenBetween(start, end) {
    if (!start || !end || !start.parentNode || start.parentNode !== end.parentNode) {
      return [];
    }
    const children = gosxArrayFrom(start.parentNode.childNodes);
    const startIndex = children.indexOf(start);
    const endIndex = children.indexOf(end);
    if (startIndex < 0 || endIndex <= startIndex) {
      return [];
    }
    return children.slice(startIndex + 1, endIndex);
  }

  function gosxReadDocumentContract() {
    const node = document.getElementById && document.getElementById("gosx-document");
    if (!node) {
      return null;
    }
    try {
      const payload = JSON.parse(String(node.textContent || "{}"));
      return payload && typeof payload === "object" ? payload : null;
    } catch (error) {
      console.error("[gosx] invalid document contract:", error);
      return null;
    }
  }

  function gosxCurrentEnhancementLayer() {
    const hasRuntime = Boolean(
      typeof window.__gosx_hydrate === "function"
      || (window.__gosx && window.__gosx.islands && window.__gosx.islands.size > 0)
      || (window.__gosx && window.__gosx.engines && window.__gosx.engines.size > 0)
      || (window.__gosx && window.__gosx.hubs && window.__gosx.hubs.size > 0)
    );
    return hasRuntime ? "runtime" : "bootstrap";
  }

  function gosxNormalizeDocumentCSSLayer(value, fallback) {
    const layer = String(value || fallback || "global").trim().toLowerCase();
    return GOSX_DOCUMENT_CSS_LAYERS.includes(layer) ? layer : "global";
  }

  function gosxDocumentCSSLayer(node, fallback) {
    const value = String(node && node.getAttribute && node.getAttribute("data-gosx-css-layer") || "").trim();
    return gosxNormalizeDocumentCSSLayer(value, fallback);
  }

  function gosxDocumentCSSOwner(node, fallback) {
    const value = String(node && node.getAttribute && node.getAttribute("data-gosx-css-owner") || "").trim();
    return value || fallback || "document";
  }

  function gosxDocumentCSSSource(node, fallback) {
    const value = String(node && node.getAttribute && node.getAttribute("data-gosx-css-source") || "").trim();
    return value || String(fallback || "");
  }

  function gosxDocumentStyleEntry(node, order, layerFallback, ownerFallback) {
    const file = String(node.getAttribute && node.getAttribute("data-gosx-file-css") || "");
    const scope = String(node.getAttribute && node.getAttribute("data-gosx-file-css-scope") || "");
    return {
      kind: "inline",
      file,
      href: "",
      media: "",
      layer: gosxDocumentCSSLayer(node, layerFallback),
      owner: gosxDocumentCSSOwner(node, ownerFallback),
      source: gosxDocumentCSSSource(node, file),
      order,
      scope,
    };
  }

  function gosxDocumentStylesheetEntry(node, order, layerFallback, ownerFallback) {
    const href = String(node.getAttribute && node.getAttribute("href") || "");
    return {
      kind: "stylesheet",
      file: "",
      href,
      media: String(node.getAttribute && node.getAttribute("media") || ""),
      layer: gosxDocumentCSSLayer(node, layerFallback),
      owner: gosxDocumentCSSOwner(node, ownerFallback),
      source: gosxDocumentCSSSource(node, href),
      order,
      scope: "",
    };
  }

  function gosxDocumentCSSLayerState(layer) {
    return {
      layer,
      count: 0,
      inlineCount: 0,
      stylesheetCount: 0,
      scopedCount: 0,
      owners: [],
      sources: [],
      entries: [],
    };
  }

  function gosxDocumentCSSLayers(entries) {
    const layers = Object.create(null);
    for (const layer of GOSX_DOCUMENT_CSS_LAYERS) {
      layers[layer] = gosxDocumentCSSLayerState(layer);
    }
    for (const entry of entries || []) {
      const layer = gosxNormalizeDocumentCSSLayer(entry && entry.layer, "global");
      const bucket = layers[layer] || gosxDocumentCSSLayerState(layer);
      const item = Object.assign({}, entry, { layer });
      bucket.count += 1;
      if (item.kind === "stylesheet") {
        bucket.stylesheetCount += 1;
      } else {
        bucket.inlineCount += 1;
      }
      if (item.scope) {
        bucket.scopedCount += 1;
      }
      if (item.owner && !bucket.owners.includes(item.owner)) {
        bucket.owners.push(item.owner);
      }
      if (item.source && !bucket.sources.includes(item.source)) {
        bucket.sources.push(item.source);
      }
      bucket.entries.push(item);
      layers[layer] = bucket;
    }
    return layers;
  }

  function gosxDocumentHeadAssets() {
    const markers = gosxManagedHeadMarkers();
    const nodes = gosxDocumentChildrenBetween(markers.start, markers.end);
    const ownedCSS = [];
    const stylesheets = [];
    const scripts = [];
    let cssOrder = 0;

    for (const node of nodes) {
      if (!node || node.nodeType !== 1) {
        continue;
      }
      const tagName = String(node.tagName || "").toUpperCase();
      if (tagName === "STYLE") {
        ownedCSS.push(gosxDocumentStyleEntry(
          node,
          gosxNumber(node.getAttribute && node.getAttribute("data-gosx-css-order"), cssOrder),
          "global",
          "document-global"
        ));
        cssOrder += 1;
        continue;
      }
      if (tagName === "LINK" && /\bstylesheet\b/i.test(String(node.getAttribute && node.getAttribute("rel") || ""))) {
        const entry = gosxDocumentStylesheetEntry(node, cssOrder, "global", "document-global");
        stylesheets.push(entry);
        ownedCSS.push(entry);
        cssOrder += 1;
        continue;
      }
      if (tagName === "SCRIPT") {
        scripts.push({
          role: String(node.getAttribute && node.getAttribute("data-gosx-script") || ""),
          navigation: node.hasAttribute && node.hasAttribute("data-gosx-navigation"),
          src: String(node.getAttribute && node.getAttribute("src") || ""),
          inline: !node.getAttribute || !node.getAttribute("src"),
        });
      }
    }

    for (const node of gosxArrayFrom(document.head && document.head.childNodes)) {
      if (!node || node.nodeType !== 1) {
        continue;
      }
      if (String(node.tagName || "").toUpperCase() !== "STYLE") {
        continue;
      }
      if (String(node.getAttribute && node.getAttribute("data-gosx-css-layer") || "") !== "runtime") {
        continue;
      }
      ownedCSS.push(gosxDocumentStyleEntry(node, cssOrder, "runtime", "gosx-bootstrap"));
      cssOrder += 1;
    }

    return {
      managed: Boolean(markers.start && markers.end),
      ownedCSS,
      stylesheets,
      scripts,
    };
  }

  function cloneGosxDocumentState(state) {
    if (!state) {
      return null;
    }
    return {
      page: Object.assign({}, state.page),
      enhancement: Object.assign({}, state.enhancement),
      head: Object.assign({}, state.head),
      css: {
        owned: state.css.owned.map(function(entry) {
          return Object.assign({}, entry);
        }),
        stylesheets: state.css.stylesheets.map(function(entry) {
          return Object.assign({}, entry);
        }),
        layers: Object.fromEntries(GOSX_DOCUMENT_CSS_LAYERS.map(function(layer) {
          const bucket = state.css.layers[layer] || gosxDocumentCSSLayerState(layer);
          return [layer, {
            layer: bucket.layer,
            count: bucket.count,
            inlineCount: bucket.inlineCount,
            stylesheetCount: bucket.stylesheetCount,
            scopedCount: bucket.scopedCount,
            owners: bucket.owners.slice(),
            sources: bucket.sources.slice(),
            entries: bucket.entries.map(function(entry) {
              return Object.assign({}, entry);
            }),
          }];
        })),
      },
      assets: {
        scripts: state.assets.scripts.map(function(entry) {
          return Object.assign({}, entry);
        }),
      },
    };
  }

  function gosxReadDocumentState() {
    const contract = gosxReadDocumentContract();
    const page = contract && contract.page && typeof contract.page === "object" ? contract.page : {};
    const enhancement = contract && contract.enhancement && typeof contract.enhancement === "object" ? contract.enhancement : {};
    const assets = gosxDocumentHeadAssets();
    const cssLayers = gosxDocumentCSSLayers(assets.ownedCSS);
    const layer = gosxCurrentEnhancementLayer();

    return {
      page: {
        id: typeof page.id === "string" && page.id ? page.id : "gosx-doc-page",
        pattern: typeof page.pattern === "string" ? page.pattern : "",
        path: typeof page.path === "string" && page.path ? page.path : String(window.location && window.location.href || ""),
        title: typeof page.title === "string" && page.title ? page.title : String(document.title || ""),
        status: Number.isFinite(Number(page.status)) ? Number(page.status) : 200,
        requestID: typeof page.requestID === "string" ? page.requestID : "",
      },
      enhancement: {
        layer,
        bootstrap: true,
        runtime: layer === "runtime" || Boolean(enhancement.runtime),
        navigation: Boolean(enhancement.navigation) || Boolean(window.__gosx_page_nav && typeof window.__gosx_page_nav.navigate === "function"),
        ready: Boolean(window.__gosx && window.__gosx.ready),
      },
      head: {
        managed: assets.managed,
        ownedCSSCount: assets.ownedCSS.length,
        stylesheetCount: assets.stylesheets.length,
        scriptCount: assets.scripts.length,
      },
      css: {
        owned: assets.ownedCSS,
        stylesheets: assets.stylesheets,
        layers: cssLayers,
      },
      assets: {
        scripts: assets.scripts,
      },
    };
  }

  function gosxDocumentChanged(prev, next) {
    return JSON.stringify(prev || null) !== JSON.stringify(next || null);
  }

  function applyGosxDocumentState(state) {
    const root = document.documentElement || document.body;
    const body = document.body || root;
    if (!root || !state) {
      return;
    }
    setAttrValue(root, "data-gosx-document", "true");
    setAttrValue(root, "data-gosx-document-id", state.page.id);
    setAttrValue(root, "data-gosx-route-pattern", state.page.pattern);
    setAttrValue(root, "data-gosx-enhancement-layer", state.enhancement.layer);
    setAttrValue(root, "data-gosx-navigation", state.enhancement.navigation ? "true" : "false");
    setAttrValue(root, "data-gosx-runtime-ready", state.enhancement.ready ? "true" : "false");
    setAttrValue(root, "data-gosx-head-managed", state.head.managed ? "true" : "false");
    setAttrValue(root, "data-gosx-css-owned-count", state.head.ownedCSSCount);
    setStyleValue(root.style, "--gosx-document-owned-css-count", String(state.head.ownedCSSCount));
    setStyleValue(root.style, "--gosx-document-stylesheet-count", String(state.head.stylesheetCount));
    for (const layer of GOSX_DOCUMENT_CSS_LAYERS) {
      const count = state.css && state.css.layers && state.css.layers[layer] ? state.css.layers[layer].count : 0;
      setAttrValue(root, "data-gosx-css-" + layer + "-count", count);
      setStyleValue(root.style, "--gosx-document-css-" + layer + "-count", String(count));
    }
    if (body && body !== root) {
      setAttrValue(body, "data-gosx-document-id", state.page.id);
      setAttrValue(body, "data-gosx-enhancement-layer", state.enhancement.layer);
      setAttrValue(body, "data-gosx-navigation", state.enhancement.navigation ? "true" : "false");
      setAttrValue(body, "data-gosx-runtime-ready", state.enhancement.ready ? "true" : "false");
      for (const layer of GOSX_DOCUMENT_CSS_LAYERS) {
        const count = state.css && state.css.layers && state.css.layers[layer] ? state.css.layers[layer].count : 0;
        setAttrValue(body, "data-gosx-css-" + layer + "-count", count);
      }
    }
  }

  function dispatchGosxDocument(reason) {
    if (typeof CustomEvent !== "function" || !document || typeof document.dispatchEvent !== "function") {
      return;
    }
    document.dispatchEvent(new CustomEvent("gosx:document", {
      detail: {
        reason: reason || "refresh",
        state: cloneGosxDocumentState(gosxDocumentState),
      },
    }));
  }

  function refreshGosxDocumentState(reason) {
    const next = gosxReadDocumentState();
    const changed = gosxDocumentChanged(gosxDocumentState, next);
    gosxDocumentState = next;
    applyGosxDocumentState(next);
    if (!changed) {
      return cloneGosxDocumentState(next);
    }
    for (const listener of Array.from(gosxDocumentListeners)) {
      try {
        listener(cloneGosxDocumentState(next), reason || "refresh");
      } catch (error) {
        console.error("[gosx] document listener failed:", error);
      }
    }
    dispatchGosxDocument(reason);
    return cloneGosxDocumentState(next);
  }

  function scheduleGosxDocumentRefresh(reason) {
    gosxDocumentRefreshReason = reason || gosxDocumentRefreshReason || "refresh";
    if (gosxDocumentRefreshScheduled) {
      return;
    }
    gosxDocumentRefreshScheduled = true;
    const run = function() {
      gosxDocumentRefreshScheduled = false;
      const nextReason = gosxDocumentRefreshReason || "refresh";
      gosxDocumentRefreshReason = "";
      refreshGosxDocumentState(nextReason);
    };
    setTimeout(run, 0);
  }

  function installGosxDocumentObservers() {
    if (gosxDocumentObserversInstalled) {
      return;
    }
    gosxDocumentObserversInstalled = true;
    if (document && typeof document.addEventListener === "function") {
      document.addEventListener("gosx:navigate", function() {
        scheduleGosxDocumentRefresh("navigate");
        refreshGosxEnvironmentState("navigate");
      });
      document.addEventListener("gosx:ready", function() {
        scheduleGosxDocumentRefresh("ready");
      });
    }
    if (typeof window.addEventListener === "function") {
      window.addEventListener("pageshow", function() {
        scheduleGosxDocumentRefresh("pageshow");
      });
    }
    if (typeof MutationObserver === "function" && document && document.head) {
      const headObserver = new MutationObserver(function() {
        scheduleGosxDocumentRefresh("head-mutation");
      });
      if (typeof headObserver.observe === "function") {
        headObserver.observe(document.head, {
          subtree: true,
          childList: true,
          attributes: true,
          characterData: true,
          attributeFilter: [
            "href",
            "media",
            "rel",
            "name",
            "content",
            "src",
            "data-gosx-css-layer",
            "data-gosx-css-owner",
            "data-gosx-css-source",
            "data-gosx-css-order",
            "data-gosx-css-scope",
            "data-gosx-file-css",
            "data-gosx-file-css-scope",
            "data-gosx-script",
            "data-gosx-navigation",
          ],
        });
      }
    }
  }

  function observeGosxDocument(listener, options) {
    if (typeof listener !== "function") {
      return function() {};
    }
    installGosxDocumentObservers();
    gosxDocumentListeners.add(listener);
    if (!gosxDocumentState) {
      refreshGosxDocumentState("init");
    }
    if (!options || options.immediate !== false) {
      listener(cloneGosxDocumentState(gosxDocumentState), "init");
    }
    return function() {
      gosxDocumentListeners.delete(listener);
    };
  }

  function gosxInheritedElementAttribute(element, name) {
    if (!element || !name) {
      return "";
    }
    let current = element;
    while (current) {
      if (typeof current.getAttribute === "function") {
        const value = String(current.getAttribute(name) || "").trim();
        if (value) {
          return value;
        }
      }
      current = current.parentNode || null;
    }
    if (document && document.documentElement && document.documentElement !== element && typeof document.documentElement.getAttribute === "function") {
      return String(document.documentElement.getAttribute(name) || "").trim();
    }
    return "";
  }

  function gosxPresentationSnapshot(element) {
    if (!element || typeof element !== "object") {
      return null;
    }
    const style = textLayoutComputedStyle(element);
    const rect = element && typeof element.getBoundingClientRect === "function" ? element.getBoundingClientRect() : null;
    const width = Math.max(0, gosxNumber(rect && rect.width, element && (element.clientWidth || element.offsetWidth || element.width) || 0));
    const height = Math.max(0, gosxNumber(rect && rect.height, element && (element.clientHeight || element.offsetHeight || element.height) || 0));
    const lang = gosxInheritedElementAttribute(element, "lang");
    const directionAttr = gosxInheritedElementAttribute(element, "dir");
    const writingMode = normalizeTextLayoutWritingMode(textLayoutComputedStyleValue(style, "writing-mode"));
    const inlineSize = textLayoutLogicalInlineSize(width, height, writingMode);
    const blockSize = textLayoutLogicalBlockSize(width, height, writingMode);
    const maxInlineSize = textLayoutLengthValue(
      textLayoutComputedStyleValue(style, "--gosx-text-layout-max-inline-size")
      || textLayoutComputedStyleValue(style, "max-inline-size")
      || textLayoutComputedStyleValue(style, "--gosx-text-layout-max-width")
      || textLayoutComputedStyleValue(style, "max-width"),
      inlineSize,
    );
    return {
      style,
      width,
      height,
      inlineSize,
      blockSize,
      maxWidth: maxInlineSize > 0 ? maxInlineSize : inlineSize,
      maxInlineSize: maxInlineSize > 0 ? maxInlineSize : inlineSize,
      direction: textLayoutComputedStyleValue(style, "direction") || directionAttr || "",
      writingMode,
      lang,
      font: textLayoutComputedStyleValue(style, "font") || "",
      lineHeight: textLayoutLengthValue(
        textLayoutComputedStyleValue(style, "--gosx-text-layout-line-height")
        || textLayoutComputedStyleValue(style, "line-height"),
        0
      ),
      textAlign: textLayoutComputedStyleValue(style, "text-align") || "",
      whiteSpace: textLayoutComputedStyleValue(style, "white-space") || "",
      display: textLayoutComputedStyleValue(style, "display") || "",
      visibility: textLayoutComputedStyleValue(style, "visibility") || "",
      containerType: textLayoutComputedStyleValue(style, "container-type") || "",
      environment: cloneGosxEnvironment(gosxEnvironmentState || refreshGosxEnvironmentState("presentation")),
    };
  }

  function observeGosxPresentation(element, listener, options) {
    if (!element || typeof listener !== "function") {
      return function() {};
    }
    let resizeObserver = null;
    let mutationObserver = null;
    let stopEnvironment = null;
    let stopDocument = null;
    const notify = function(reason) {
      listener(gosxPresentationSnapshot(element), reason || "presentation");
    };

    if (typeof ResizeObserver === "function") {
      resizeObserver = new ResizeObserver(function() {
        notify("presentation-resize");
      });
      if (typeof resizeObserver.observe === "function") {
        resizeObserver.observe(element);
      }
    }

    stopEnvironment = observeGosxEnvironment(function() {
      notify("presentation-environment");
    }, { immediate: false });

    stopDocument = observeGosxDocument(function() {
      notify("presentation-document");
    }, { immediate: false });

    if (typeof MutationObserver === "function" && document && document.documentElement) {
      mutationObserver = new MutationObserver(function(records) {
        for (const record of records || []) {
          const target = record && record.target;
          if (!target || target === element || target === document.documentElement || target === document.body) {
            notify("presentation-mutation");
            return;
          }
          if (typeof target.contains === "function" && target.contains(element)) {
            notify("presentation-mutation");
            return;
          }
        }
      });
      if (typeof mutationObserver.observe === "function") {
        mutationObserver.observe(document.documentElement, {
          subtree: true,
          attributes: true,
          attributeFilter: ["class", "style", "dir", "lang", "hidden"],
        });
      }
    }

    if (!options || options.immediate !== false) {
      notify("init");
    }

    return function() {
      if (resizeObserver && typeof resizeObserver.disconnect === "function") {
        resizeObserver.disconnect();
      }
      if (mutationObserver && typeof mutationObserver.disconnect === "function") {
        mutationObserver.disconnect();
      }
      if (typeof stopEnvironment === "function") {
        stopEnvironment();
      }
      if (typeof stopDocument === "function") {
        stopDocument();
      }
    };
  }

  window.__gosx.environment = {
    get() {
      if (!gosxEnvironmentState) {
        refreshGosxEnvironmentState("read");
      }
      return cloneGosxEnvironment(gosxEnvironmentState);
    },
    refresh(reason) {
      return refreshGosxEnvironmentState(reason || "manual");
    },
    observe: observeGosxEnvironment,
  };

  window.__gosx.document = {
    get() {
      if (!gosxDocumentState) {
        refreshGosxDocumentState("read");
      }
      return cloneGosxDocumentState(gosxDocumentState);
    },
    refresh(reason) {
      return refreshGosxDocumentState(reason || "manual");
    },
    css(layer) {
      if (!gosxDocumentState) {
        refreshGosxDocumentState("read");
      }
      if (!layer) {
        return cloneGosxDocumentState(gosxDocumentState).css;
      }
      const key = gosxNormalizeDocumentCSSLayer(layer, "global");
      return cloneGosxDocumentState(gosxDocumentState).css.layers[key];
    },
    observe: observeGosxDocument,
  };

  window.__gosx.presentation = {
    read: gosxPresentationSnapshot,
    observe: observeGosxPresentation,
  };

  installGosxEnvironmentObservers();
  installGosxDocumentObservers();
  refreshGosxEnvironmentState("bootstrap");
  refreshGosxDocumentState("bootstrap");
