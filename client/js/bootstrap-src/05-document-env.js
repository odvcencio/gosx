  const gosxEnvironmentListeners = new Set();
  const gosxDocumentListeners = new Set();
  const gosxPresentationRecordsByElement = new Map();
  let gosxEnvironmentState = null;
  let gosxDocumentState = null;
  let gosxEnvironmentObserversInstalled = false;
  let gosxDocumentObserversInstalled = false;
  const GOSX_DOCUMENT_CSS_LAYERS = ["global", "layout", "page", "runtime"];
  const GOSX_DOCUMENT_ENHANCEMENT_LAYERS = ["html", "bootstrap", "runtime"];
  const gosxStateInvalidations = new Map();
  const gosxVisualInvalidations = new Map();
  let gosxStateInvalidationScheduled = false;
  let gosxVisualInvalidationScheduled = false;
  let gosxVisualInvalidationHandle = 0;
  const GOSX_MOTION_ATTR = "data-gosx-motion";
  const GOSX_MOTION_MUTATION_ATTRS = [
    GOSX_MOTION_ATTR,
    "data-gosx-motion-preset",
    "data-gosx-motion-trigger",
    "data-gosx-motion-duration",
    "data-gosx-motion-delay",
    "data-gosx-motion-easing",
    "data-gosx-motion-distance",
    "data-gosx-motion-respect-reduced",
  ];
  const gosxManagedMotionRecordsByElement = new Map();
  let gosxManagedMotionObserver = null;
  let gosxManagedMotionNextID = 0;
  let gosxPresentationResizeObserver = null;
  let gosxPresentationMutationObserver = null;
  let gosxPresentationStopEnvironment = null;
  let gosxPresentationStopDocument = null;
  const GOSX_PRESENTATION_MUTATION_ATTRS = ["class", "style", "dir", "lang", "hidden"];
  const gosxAppliedEnhancementKindAttrs = new Set();

  function gosxArrayFrom(listLike) {
    return Array.prototype.slice.call(listLike || []);
  }

  function gosxMergedReason(current, next) {
    const value = String(next || "").trim();
    if (!value) {
      return current || "";
    }
    if (!current) {
      return value;
    }
    const parts = current.split("|");
    if (parts.includes(value)) {
      return current;
    }
    parts.push(value);
    return parts.join("|");
  }

  function gosxScheduleStateInvalidation(key, reason, callback) {
    if (!key || typeof callback !== "function") {
      return;
    }
    const entry = gosxStateInvalidations.get(key) || { callback: null, reason: "" };
    entry.callback = callback;
    entry.reason = gosxMergedReason(entry.reason, reason || "state");
    gosxStateInvalidations.set(key, entry);
    if (gosxStateInvalidationScheduled) {
      return;
    }
    gosxStateInvalidationScheduled = true;
    const flush = function() {
      gosxStateInvalidationScheduled = false;
      const pending = Array.from(gosxStateInvalidations.values());
      gosxStateInvalidations.clear();
      for (const item of pending) {
        if (!item || typeof item.callback !== "function") {
          continue;
        }
        item.callback(item.reason || "state");
      }
    };
    if (typeof queueMicrotask === "function") {
      queueMicrotask(flush);
      return;
    }
    Promise.resolve().then(flush);
  }

  function gosxScheduleVisualInvalidation(key, reason, callback) {
    if (!key || typeof callback !== "function") {
      return;
    }
    const entry = gosxVisualInvalidations.get(key) || { callback: null, reason: "" };
    entry.callback = callback;
    entry.reason = gosxMergedReason(entry.reason, reason || "visual");
    gosxVisualInvalidations.set(key, entry);
    if (gosxVisualInvalidationScheduled) {
      return;
    }
    gosxVisualInvalidationScheduled = true;
    const flush = function(frameTime) {
      gosxVisualInvalidationScheduled = false;
      gosxVisualInvalidationHandle = 0;
      while (gosxVisualInvalidations.size > 0) {
        const pending = Array.from(gosxVisualInvalidations.values());
        gosxVisualInvalidations.clear();
        for (const item of pending) {
          if (!item || typeof item.callback !== "function") {
            continue;
          }
          item.callback(item.reason || "visual", frameTime);
        }
      }
    };
    if (typeof requestAnimationFrame === "function") {
      gosxVisualInvalidationHandle = requestAnimationFrame(flush);
      return;
    }
    gosxVisualInvalidationHandle = setTimeout(function() {
      flush(Date.now());
    }, 0);
  }

  function gosxCancelInvalidation(key) {
    if (!key) {
      return;
    }
    gosxStateInvalidations.delete(key);
    gosxVisualInvalidations.delete(key);
  }

  function gosxManagedMotionElements(root) {
    const elements = [];
    walkElementTree(root, function(element) {
      if (element && typeof element.hasAttribute === "function" && element.hasAttribute(GOSX_MOTION_ATTR)) {
        elements.push(element);
      }
    });
    return elements;
  }

  function gosxMotionStringAttr(element, name, fallback) {
    const value = element && typeof element.getAttribute === "function"
      ? String(element.getAttribute(name) || "").trim()
      : "";
    return value || fallback;
  }

  function gosxMotionBoolAttr(element, name, fallback) {
    const value = gosxMotionStringAttr(element, name, "");
    if (!value) {
      return fallback;
    }
    switch (value.toLowerCase()) {
      case "false":
      case "0":
      case "off":
      case "no":
        return false;
      default:
        return true;
    }
  }

  function normalizeGosxMotionPreset(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "slide-up":
      case "slide-down":
      case "slide-left":
      case "slide-right":
      case "zoom-in":
        return String(value).trim().toLowerCase();
      default:
        return "fade";
    }
  }

  function normalizeGosxMotionTrigger(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "view":
        return "view";
      default:
        return "load";
    }
  }

  function gosxManagedMotionConfig(element) {
    return {
      preset: normalizeGosxMotionPreset(gosxMotionStringAttr(element, "data-gosx-motion-preset", "fade")),
      trigger: normalizeGosxMotionTrigger(gosxMotionStringAttr(element, "data-gosx-motion-trigger", "load")),
      duration: Math.max(1, Math.round(gosxNumber(gosxMotionStringAttr(element, "data-gosx-motion-duration", 220), 220))),
      delay: Math.max(0, Math.round(gosxNumber(gosxMotionStringAttr(element, "data-gosx-motion-delay", 0), 0))),
      easing: gosxMotionStringAttr(element, "data-gosx-motion-easing", "cubic-bezier(0.16, 1, 0.3, 1)"),
      distance: Math.max(0, gosxNumber(gosxMotionStringAttr(element, "data-gosx-motion-distance", 18), 18)),
      respectReducedMotion: gosxMotionBoolAttr(element, "data-gosx-motion-respect-reduced", true),
    };
  }

  function gosxManagedMotionReduced(config) {
    if (!config || !config.respectReducedMotion) {
      return false;
    }
    const environment = gosxEnvironmentState || refreshGosxEnvironmentState("motion");
    return Boolean(environment && environment.reducedMotion);
  }

  function gosxManagedMotionKeyframes(config) {
    const distance = Math.max(0, gosxNumber(config && config.distance, 18));
    switch (config && config.preset) {
      case "slide-up":
        return [
          { opacity: 0, transform: "translate3d(0, " + distance + "px, 0)" },
          { opacity: 1, transform: "translate3d(0, 0, 0)" },
        ];
      case "slide-down":
        return [
          { opacity: 0, transform: "translate3d(0, -" + distance + "px, 0)" },
          { opacity: 1, transform: "translate3d(0, 0, 0)" },
        ];
      case "slide-left":
        return [
          { opacity: 0, transform: "translate3d(" + distance + "px, 0, 0)" },
          { opacity: 1, transform: "translate3d(0, 0, 0)" },
        ];
      case "slide-right":
        return [
          { opacity: 0, transform: "translate3d(-" + distance + "px, 0, 0)" },
          { opacity: 1, transform: "translate3d(0, 0, 0)" },
        ];
      case "zoom-in": {
        const scale = Math.max(0.72, 1 - (Math.min(distance, 64) / 200));
        return [
          { opacity: 0, transform: "scale(" + scale + ")" },
          { opacity: 1, transform: "scale(1)" },
        ];
      }
      default:
        return [
          { opacity: 0 },
          { opacity: 1 },
        ];
    }
  }

  function gosxManagedMotionState(record, state) {
    if (!record || !record.element) {
      return;
    }
    setAttrValue(record.element, "data-gosx-motion-state", state || "idle");
  }

  function cancelManagedMotionAnimation(record) {
    if (!record || !record.animation || typeof record.animation.cancel !== "function") {
      record.animation = null;
      return;
    }
    try {
      record.animation.cancel();
    } catch (_error) {
    }
    record.animation = null;
  }

  function playManagedMotion(record, reason) {
    if (!record || !record.element || record.played) {
      return;
    }
    record.config = gosxManagedMotionConfig(record.element);
    if (gosxManagedMotionReduced(record.config)) {
      record.played = true;
      gosxManagedMotionState(record, "reduced");
      return;
    }
    record.played = true;
    gosxManagedMotionState(record, "running");
    cancelManagedMotionAnimation(record);
    if (typeof record.element.animate !== "function") {
      gosxManagedMotionState(record, "finished");
      return;
    }
    const animation = record.element.animate(gosxManagedMotionKeyframes(record.config), {
      duration: record.config.duration,
      delay: record.config.delay,
      easing: record.config.easing,
      fill: "both",
    });
    record.animation = animation || null;
    if (animation && animation.finished && typeof animation.finished.then === "function") {
      animation.finished.then(function() {
        if (record.animation !== animation) {
          return;
        }
        record.animation = null;
        gosxManagedMotionState(record, "finished");
      }, function() {
        if (record.animation !== animation) {
          return;
        }
        record.animation = null;
        gosxManagedMotionState(record, "idle");
      });
      return;
    }
    gosxManagedMotionState(record, "finished");
  }

  function connectManagedMotion(record) {
    if (!record || !record.element) {
      return;
    }
    if (record.stopTrigger) {
      record.stopTrigger();
      record.stopTrigger = null;
    }
    if (record.config.trigger === "view" && typeof IntersectionObserver === "function") {
      const observer = new IntersectionObserver(function(entries) {
        for (const entry of entries || []) {
          if (!entry || entry.target !== record.element) {
            continue;
          }
          if (entry.isIntersecting !== false && gosxNumber(entry.intersectionRatio, 1) > 0) {
            playManagedMotion(record, "view");
          }
        }
      }, { threshold: [0, 0.01, 0.2] });
      observer.observe(record.element);
      record.stopTrigger = function() {
        observer.disconnect();
      };
      return;
    }
    gosxScheduleVisualInvalidation(record.key, "motion-load", function(nextReason) {
      playManagedMotion(record, nextReason || "load");
    });
  }

  function observeManagedMotion(element) {
    if (!element) {
      return null;
    }
    let record = gosxManagedMotionRecordsByElement.get(element);
    if (record) {
      record.config = gosxManagedMotionConfig(element);
      if (!record.played) {
        connectManagedMotion(record);
      }
      return record;
    }
    gosxManagedMotionNextID += 1;
    record = {
      key: "motion:" + gosxManagedMotionNextID,
      element: element,
      config: gosxManagedMotionConfig(element),
      played: false,
      animation: null,
      stopTrigger: null,
    };
    gosxManagedMotionRecordsByElement.set(element, record);
    gosxManagedMotionState(record, "idle");
    connectManagedMotion(record);
    return record;
  }

  function disposeManagedMotionElement(element) {
    const record = element ? gosxManagedMotionRecordsByElement.get(element) : null;
    if (!record) {
      return;
    }
    gosxCancelInvalidation(record.key);
    if (record.stopTrigger) {
      record.stopTrigger();
      record.stopTrigger = null;
    }
    cancelManagedMotionAnimation(record);
    gosxManagedMotionRecordsByElement.delete(element);
  }

  function installManagedMotionObserver(root) {
    if (gosxManagedMotionObserver || typeof MutationObserver !== "function" || !root) {
      return;
    }
    gosxManagedMotionObserver = new MutationObserver(function(records) {
      for (const record of records || []) {
        if (!record) {
          continue;
        }
        if (record.type === "attributes" && record.target && record.attributeName && GOSX_MOTION_MUTATION_ATTRS.includes(record.attributeName)) {
          if (record.target.hasAttribute && record.target.hasAttribute(GOSX_MOTION_ATTR)) {
            observeManagedMotion(record.target);
          } else {
            disposeManagedMotionElement(record.target);
          }
        }
        for (const node of gosxArrayFrom(record.addedNodes)) {
          if (node && node.nodeType === 1) {
            mountManagedMotion(node);
          }
        }
        for (const node of gosxArrayFrom(record.removedNodes)) {
          if (!node || node.nodeType !== 1) {
            continue;
          }
          for (const element of gosxManagedMotionElements(node)) {
            disposeManagedMotionElement(element);
          }
        }
      }
    });
    gosxManagedMotionObserver.observe(root, {
      subtree: true,
      childList: true,
      attributes: true,
      attributeFilter: GOSX_MOTION_MUTATION_ATTRS,
    });
  }

  function mountManagedMotion(root) {
    const targetRoot = root || document.body || document.documentElement;
    for (const element of gosxManagedMotionElements(targetRoot)) {
      observeManagedMotion(element);
    }
    installManagedMotionObserver(document.body || document.documentElement);
  }

  function disposeManagedMotion() {
    if (gosxManagedMotionObserver) {
      gosxManagedMotionObserver.disconnect();
      gosxManagedMotionObserver = null;
    }
    for (const element of Array.from(gosxManagedMotionRecordsByElement.keys())) {
      disposeManagedMotionElement(element);
    }
  }

  window.__gosx.motion = {
    mountAll: mountManagedMotion,
    observe: observeManagedMotion,
    dispose: disposeManagedMotionElement,
  };

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

  function scheduleGosxEnvironmentRefresh(reason) {
    gosxScheduleStateInvalidation("environment", reason || "environment", function(nextReason) {
      refreshGosxEnvironmentState(nextReason);
    });
  }

  function installGosxEnvironmentObservers() {
    if (gosxEnvironmentObserversInstalled) {
      return;
    }
    gosxEnvironmentObserversInstalled = true;

    const refresh = function(reason) {
      scheduleGosxEnvironmentRefresh(reason);
    };

    if (document && typeof document.addEventListener === "function") {
      document.addEventListener("visibilitychange", function() {
        refresh("visibility");
      });
    }
    // Passive listeners: these handlers never call preventDefault, and on
    // iOS Safari the absence of { passive: true } causes the browser to
    // block the scroll thread until the JS handler finishes — which
    // manifests as scroll jank and lingering stale canvas frames during
    // rubber-band / touch scrolls. Firefox is less strict but also benefits
    // from the hint since it lets the compositor skip extra coordination.
    const passive = { passive: true };
    if (typeof window.addEventListener === "function") {
      window.addEventListener("resize", function() {
        refresh("viewport");
      }, passive);
      window.addEventListener("orientationchange", function() {
        refresh("viewport");
      }, passive);
      window.addEventListener("pageshow", function() {
        refresh("pageshow");
      }, passive);
    }
    if (window.visualViewport && typeof window.visualViewport.addEventListener === "function") {
      window.visualViewport.addEventListener("resize", function() {
        refresh("visual-viewport");
      }, passive);
      window.visualViewport.addEventListener("scroll", function() {
        refresh("visual-viewport");
      }, passive);
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

  function gosxWalkDocumentElements(root, visit) {
    if (!root || typeof visit !== "function") {
      return;
    }
    if (root.nodeType === 1) {
      visit(root);
    }
    const children = root.children || root.childNodes || [];
    for (const child of children) {
      if (child && child.nodeType === 1) {
        gosxWalkDocumentElements(child, visit);
      }
    }
  }

  function gosxNormalizeEnhancementLayer(value, fallback) {
    const layer = String(value || fallback || "html").trim().toLowerCase();
    return GOSX_DOCUMENT_ENHANCEMENT_LAYERS.includes(layer) ? layer : "html";
  }

  function gosxDocumentEnhancementLayerState(layer) {
    return {
      layer,
      count: 0,
      kinds: [],
      fallbacks: [],
      entries: [],
    };
  }

  function gosxDocumentEnhancementKindState(kind) {
    return {
      kind,
      count: 0,
      layers: [],
      fallbacks: [],
      entries: [],
    };
  }

  function gosxDocumentEnhancementEntry(node, order) {
    const kind = String(node && node.getAttribute && node.getAttribute("data-gosx-enhance") || "").trim();
    if (!kind) {
      return null;
    }
    const layer = gosxNormalizeEnhancementLayer(node && node.getAttribute && node.getAttribute("data-gosx-enhance-layer"), "html");
    const fallback = String(node && node.getAttribute && node.getAttribute("data-gosx-fallback") || "").trim() || "none";
    return {
      kind,
      layer,
      fallback,
      id: String(node && node.getAttribute && node.getAttribute("id") || ""),
      tag: String(node && node.tagName || "").toLowerCase(),
      engine: String(node && node.getAttribute && node.getAttribute("data-gosx-engine") || ""),
      order,
    };
  }

  function gosxDocumentEnhancements() {
    const entries = [];
    let order = 0;
    gosxWalkDocumentElements(document.body || document.documentElement, function(node) {
      const entry = gosxDocumentEnhancementEntry(node, order);
      if (!entry) {
        return;
      }
      entries.push(entry);
      order += 1;
    });
    const layers = Object.create(null);
    for (const layer of GOSX_DOCUMENT_ENHANCEMENT_LAYERS) {
      layers[layer] = gosxDocumentEnhancementLayerState(layer);
    }
    const kinds = Object.create(null);
    for (const entry of entries) {
      const layerBucket = layers[entry.layer] || gosxDocumentEnhancementLayerState(entry.layer);
      layerBucket.count += 1;
      if (!layerBucket.kinds.includes(entry.kind)) {
        layerBucket.kinds.push(entry.kind);
      }
      if (!layerBucket.fallbacks.includes(entry.fallback)) {
        layerBucket.fallbacks.push(entry.fallback);
      }
      layerBucket.entries.push(Object.assign({}, entry));
      layers[entry.layer] = layerBucket;

      const kindBucket = kinds[entry.kind] || gosxDocumentEnhancementKindState(entry.kind);
      kindBucket.count += 1;
      if (!kindBucket.layers.includes(entry.layer)) {
        kindBucket.layers.push(entry.layer);
      }
      if (!kindBucket.fallbacks.includes(entry.fallback)) {
        kindBucket.fallbacks.push(entry.fallback);
      }
      kindBucket.entries.push(Object.assign({}, entry));
      kinds[entry.kind] = kindBucket;
    }
    return {
      count: entries.length,
      entries,
      layers,
      kinds,
    };
  }

  function gosxDocumentRuntimeAssets(contract, enhancement, layer) {
    const assets = contract && contract.assets && typeof contract.assets === "object" ? contract.assets : {};
    let bootstrapMode = String(assets.bootstrapMode || "").trim().toLowerCase();
    if (!bootstrapMode) {
      bootstrapMode = layer === "runtime" || Boolean(enhancement && enhancement.runtime) ? "full" : (Boolean(enhancement && enhancement.bootstrap) ? "lite" : "none");
    }
    if (bootstrapMode !== "none" && bootstrapMode !== "lite" && bootstrapMode !== "full") {
      bootstrapMode = "none";
    }
    return {
      bootstrapMode,
      manifest: Boolean(assets.manifest),
      runtimePath: String(assets.runtimePath || ""),
      wasmExecPath: String(assets.wasmExecPath || ""),
      patchPath: String(assets.patchPath || ""),
      bootstrapPath: String(assets.bootstrapPath || ""),
      bootstrapFeatureIslandsPath: String(assets.bootstrapFeatureIslandsPath || ""),
      bootstrapFeatureEnginesPath: String(assets.bootstrapFeatureEnginesPath || ""),
      bootstrapFeatureHubsPath: String(assets.bootstrapFeatureHubsPath || ""),
      hlsPath: String(assets.hlsPath || ""),
      islands: Math.max(0, gosxNumber(assets.islands, 0)),
      engines: Math.max(0, gosxNumber(assets.engines, 0)),
      hubs: Math.max(0, gosxNumber(assets.hubs, 0)),
    };
  }

  function gosxEnhancementAttrName(kind) {
    const value = String(kind || "").trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
    return value ? "data-gosx-enhancement-" + value + "-count" : "";
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
      enhancements: {
        count: state.enhancements.count,
        entries: state.enhancements.entries.map(function(entry) {
          return Object.assign({}, entry);
        }),
        layers: Object.fromEntries(GOSX_DOCUMENT_ENHANCEMENT_LAYERS.map(function(layer) {
          const bucket = state.enhancements.layers[layer] || gosxDocumentEnhancementLayerState(layer);
          return [layer, {
            layer: bucket.layer,
            count: bucket.count,
            kinds: bucket.kinds.slice(),
            fallbacks: bucket.fallbacks.slice(),
            entries: bucket.entries.map(function(entry) {
              return Object.assign({}, entry);
            }),
          }];
        })),
        kinds: Object.fromEntries(Object.keys(state.enhancements.kinds || {}).map(function(kind) {
          const bucket = state.enhancements.kinds[kind] || gosxDocumentEnhancementKindState(kind);
          return [kind, {
            kind: bucket.kind,
            count: bucket.count,
            layers: bucket.layers.slice(),
            fallbacks: bucket.fallbacks.slice(),
            entries: bucket.entries.map(function(entry) {
              return Object.assign({}, entry);
            }),
          }];
        })),
      },
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
        runtime: Object.assign({}, state.assets.runtime),
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
    const runtimeAssets = gosxDocumentRuntimeAssets(contract, enhancement, layer);
    const enhancements = gosxDocumentEnhancements();

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
        bootstrap: runtimeAssets.bootstrapMode !== "none" || Boolean(enhancement.bootstrap),
        runtime: runtimeAssets.bootstrapMode === "full" || layer === "runtime" || Boolean(enhancement.runtime),
        navigation: Boolean(enhancement.navigation) || Boolean(window.__gosx_page_nav && typeof window.__gosx_page_nav.navigate === "function"),
        ready: Boolean(window.__gosx && window.__gosx.ready),
      },
      enhancements,
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
        runtime: runtimeAssets,
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
    setAttrValue(root, "data-gosx-bootstrap-mode", state.assets && state.assets.runtime ? state.assets.runtime.bootstrapMode : "none");
    setAttrValue(root, "data-gosx-navigation", state.enhancement.navigation ? "true" : "false");
    setAttrValue(root, "data-gosx-runtime-ready", state.enhancement.ready ? "true" : "false");
    setAttrValue(root, "data-gosx-head-managed", state.head.managed ? "true" : "false");
    setAttrValue(root, "data-gosx-enhancement-count", state.enhancements && state.enhancements.count || 0);
    setAttrValue(root, "data-gosx-css-owned-count", state.head.ownedCSSCount);
    setStyleValue(root.style, "--gosx-document-owned-css-count", String(state.head.ownedCSSCount));
    setStyleValue(root.style, "--gosx-document-stylesheet-count", String(state.head.stylesheetCount));
    setStyleValue(root.style, "--gosx-document-enhancement-count", String(state.enhancements && state.enhancements.count || 0));
    for (const layer of GOSX_DOCUMENT_CSS_LAYERS) {
      const count = state.css && state.css.layers && state.css.layers[layer] ? state.css.layers[layer].count : 0;
      setAttrValue(root, "data-gosx-css-" + layer + "-count", count);
      setStyleValue(root.style, "--gosx-document-css-" + layer + "-count", String(count));
    }
    for (const layer of GOSX_DOCUMENT_ENHANCEMENT_LAYERS) {
      const count = state.enhancements && state.enhancements.layers && state.enhancements.layers[layer] ? state.enhancements.layers[layer].count : 0;
      setAttrValue(root, "data-gosx-enhancement-" + layer + "-count", count);
      setStyleValue(root.style, "--gosx-document-enhancement-" + layer + "-count", String(count));
    }
    const nextKindAttrs = new Set();
    for (const kind of Object.keys(state.enhancements && state.enhancements.kinds || {})) {
      const attrName = gosxEnhancementAttrName(kind);
      if (!attrName) {
        continue;
      }
      nextKindAttrs.add(attrName);
      setAttrValue(root, attrName, state.enhancements.kinds[kind].count);
    }
    for (const attrName of Array.from(gosxAppliedEnhancementKindAttrs)) {
      if (nextKindAttrs.has(attrName)) {
        continue;
      }
      setAttrValue(root, attrName, "");
      gosxAppliedEnhancementKindAttrs.delete(attrName);
    }
    for (const attrName of Array.from(nextKindAttrs)) {
      gosxAppliedEnhancementKindAttrs.add(attrName);
    }
    if (body && body !== root) {
      setAttrValue(body, "data-gosx-document-id", state.page.id);
      setAttrValue(body, "data-gosx-enhancement-layer", state.enhancement.layer);
      setAttrValue(body, "data-gosx-bootstrap-mode", state.assets && state.assets.runtime ? state.assets.runtime.bootstrapMode : "none");
      setAttrValue(body, "data-gosx-navigation", state.enhancement.navigation ? "true" : "false");
      setAttrValue(body, "data-gosx-runtime-ready", state.enhancement.ready ? "true" : "false");
      setAttrValue(body, "data-gosx-enhancement-count", state.enhancements && state.enhancements.count || 0);
      for (const layer of GOSX_DOCUMENT_CSS_LAYERS) {
        const count = state.css && state.css.layers && state.css.layers[layer] ? state.css.layers[layer].count : 0;
        setAttrValue(body, "data-gosx-css-" + layer + "-count", count);
      }
      for (const layer of GOSX_DOCUMENT_ENHANCEMENT_LAYERS) {
        const count = state.enhancements && state.enhancements.layers && state.enhancements.layers[layer] ? state.enhancements.layers[layer].count : 0;
        setAttrValue(body, "data-gosx-enhancement-" + layer + "-count", count);
      }
      for (const attrName of Array.from(gosxAppliedEnhancementKindAttrs)) {
        const kindName = attrName.replace(/^data-gosx-enhancement-/, "").replace(/-count$/, "");
        const bucket = state.enhancements && state.enhancements.kinds && state.enhancements.kinds[kindName];
        setAttrValue(body, attrName, bucket ? bucket.count : "");
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
    gosxScheduleStateInvalidation("document", reason || "refresh", function(nextReason) {
      refreshGosxDocumentState(nextReason);
    });
  }

  function installGosxDocumentObservers() {
    if (gosxDocumentObserversInstalled) {
      return;
    }
    gosxDocumentObserversInstalled = true;
    if (document && typeof document.addEventListener === "function") {
      document.addEventListener("gosx:navigate", function() {
        scheduleGosxDocumentRefresh("navigate");
        scheduleGosxEnvironmentRefresh("navigate");
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
    let record = gosxPresentationRecordsByElement.get(element) || null;
    if (!record) {
      record = {
        element,
        listeners: new Set(),
      };
      gosxPresentationRecordsByElement.set(element, record);
      ensureGosxPresentationObservers();
      if (gosxPresentationResizeObserver && typeof gosxPresentationResizeObserver.observe === "function") {
        gosxPresentationResizeObserver.observe(element);
      }
    }
    record.listeners.add(listener);
    if (!options || options.immediate !== false) {
      listener(gosxPresentationSnapshot(element), "init");
    }
    return function() {
      const current = gosxPresentationRecordsByElement.get(element);
      if (!current) {
        return;
      }
      current.listeners.delete(listener);
      if (current.listeners.size > 0) {
        return;
      }
      gosxCancelInvalidation(current);
      if (gosxPresentationResizeObserver && typeof gosxPresentationResizeObserver.unobserve === "function") {
        gosxPresentationResizeObserver.unobserve(element);
      }
      gosxPresentationRecordsByElement.delete(element);
      teardownGosxPresentationObservers();
    };
  }

  function gosxNotifyPresentationRecord(record, reason) {
    if (!record || !record.element || record.listeners.size === 0) {
      return;
    }
    const snapshot = gosxPresentationSnapshot(record.element);
    for (const listener of Array.from(record.listeners)) {
      try {
        listener(snapshot, reason || "presentation");
      } catch (error) {
        console.error("[gosx] presentation listener failed:", error);
      }
    }
  }

  function gosxSchedulePresentationRecord(record, reason) {
    if (!record) {
      return;
    }
    gosxScheduleVisualInvalidation(record, reason || "presentation", function(nextReason) {
      gosxNotifyPresentationRecord(record, nextReason);
    });
  }

  function gosxSchedulePresentationRefresh(reason) {
    for (const record of Array.from(gosxPresentationRecordsByElement.values())) {
      gosxSchedulePresentationRecord(record, reason || "presentation");
    }
  }

  function gosxPresentationMutationAffectsRecord(record, target) {
    if (!record || !record.element) {
      return false;
    }
    if (!target || target === record.element || target === document.documentElement || target === document.body) {
      return true;
    }
    return typeof target.contains === "function" && target.contains(record.element);
  }

  function ensureGosxPresentationObservers() {
    if (gosxPresentationRecordsByElement.size === 0) {
      return;
    }
    if (!gosxPresentationResizeObserver && typeof ResizeObserver === "function") {
      gosxPresentationResizeObserver = new ResizeObserver(function(entries) {
        for (const entry of entries || []) {
          const record = entry && entry.target ? gosxPresentationRecordsByElement.get(entry.target) : null;
          if (record) {
            gosxSchedulePresentationRecord(record, "presentation-resize");
          }
        }
      });
      for (const record of Array.from(gosxPresentationRecordsByElement.values())) {
        if (record.element && typeof gosxPresentationResizeObserver.observe === "function") {
          gosxPresentationResizeObserver.observe(record.element);
        }
      }
    }
    if (!gosxPresentationStopEnvironment) {
      gosxPresentationStopEnvironment = observeGosxEnvironment(function() {
        gosxSchedulePresentationRefresh("presentation-environment");
      }, { immediate: false });
    }
    if (!gosxPresentationStopDocument) {
      gosxPresentationStopDocument = observeGosxDocument(function() {
        gosxSchedulePresentationRefresh("presentation-document");
      }, { immediate: false });
    }
    if (!gosxPresentationMutationObserver && typeof MutationObserver === "function" && document && document.documentElement) {
      gosxPresentationMutationObserver = new MutationObserver(function(records) {
        const affected = new Set();
        for (const mutation of records || []) {
          const target = mutation && mutation.target;
          for (const record of Array.from(gosxPresentationRecordsByElement.values())) {
            if (gosxPresentationMutationAffectsRecord(record, target)) {
              affected.add(record);
            }
          }
        }
        for (const record of Array.from(affected)) {
          gosxSchedulePresentationRecord(record, "presentation-mutation");
        }
      });
      if (typeof gosxPresentationMutationObserver.observe === "function") {
        gosxPresentationMutationObserver.observe(document.documentElement, {
          subtree: true,
          attributes: true,
          attributeFilter: GOSX_PRESENTATION_MUTATION_ATTRS,
        });
      }
    }
  }

  function teardownGosxPresentationObservers() {
    if (gosxPresentationRecordsByElement.size > 0) {
      return;
    }
    if (gosxPresentationResizeObserver && typeof gosxPresentationResizeObserver.disconnect === "function") {
      gosxPresentationResizeObserver.disconnect();
    }
    gosxPresentationResizeObserver = null;
    if (gosxPresentationMutationObserver && typeof gosxPresentationMutationObserver.disconnect === "function") {
      gosxPresentationMutationObserver.disconnect();
    }
    gosxPresentationMutationObserver = null;
    if (typeof gosxPresentationStopEnvironment === "function") {
      gosxPresentationStopEnvironment();
    }
    gosxPresentationStopEnvironment = null;
    if (typeof gosxPresentationStopDocument === "function") {
      gosxPresentationStopDocument();
    }
    gosxPresentationStopDocument = null;
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
    enhancements(kind) {
      if (!gosxDocumentState) {
        refreshGosxDocumentState("read");
      }
      const snapshot = cloneGosxDocumentState(gosxDocumentState).enhancements;
      if (!kind) {
        return snapshot;
      }
      return snapshot.kinds[String(kind)] || gosxDocumentEnhancementKindState(String(kind));
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
