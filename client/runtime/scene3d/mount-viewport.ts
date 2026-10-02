// mount-viewport.ts — viewport, motion and lifecycle observers.
// @ts-check
//
// Resolves the device pixel ratio and the canvas size, watches the mount for
// resize, watches the reduced-motion preference, and pauses a scene that
// scrolls out of view. Every scene needs this.
/**
 * @typedef {object} GoSXSceneViewportState
 * @property {number} cssWidth
 * @property {number} cssHeight
 * @property {number} drawWidth
 * @property {number} drawHeight
 */
  function scenePhoneDevicePixelRatioCap(capability: any) {
    const environment = sceneEnvironmentState() || {};
    const coarse = Boolean(capability && capability.coarsePointer) || sceneMediaQueryMatches("(pointer: coarse)");
    const display = typeof window !== "undefined" && window.screen;
    const width = sceneNumber(display && display.width, sceneNumber(environment.viewportWidth, 0));
    const height = sceneNumber(display && display.height, sceneNumber(environment.viewportHeight, 0));
    const shortSide = width > 0 && height > 0 ? Math.min(width, height) : width;
    return coarse && shortSide > 0 && shortSide <= 600 ? 1.5 : Infinity;
  }

  function sceneViewportDevicePixelRatio(props, maxDevicePixelRatio, minDevicePixelRatio) {
    const environment = sceneEnvironmentState();
    const preferred = sceneNumber(
      props && (props.devicePixelRatio || props.pixelRatio),
      sceneNumber(window && window.devicePixelRatio, sceneNumber(environment && environment.devicePixelRatio, 1)),
    );
    const cap = Math.max(1, sceneNumber(maxDevicePixelRatio, 1));
    return Math.max(Math.max(1, Math.min(cap, sceneNumber(minDevicePixelRatio, 1))), Math.min(cap, preferred));
  }

  function sceneViewportFromMount(mount, props, base, canvas, capability, adaptiveQuality) {
    let cssWidth = base.baseWidth;
    let cssHeight = base.baseHeight;
    const useMeasuredHeight = sceneBool(props && (props.fillHeight || props.responsiveHeight), false);
    if (base.responsive) {
      const mountRect = mount && typeof mount.getBoundingClientRect === "function"
        ? mount.getBoundingClientRect()
        : null;
      const canvasRect = canvas && typeof canvas.getBoundingClientRect === "function"
        ? canvas.getBoundingClientRect()
        : null;
      // Canvas backing size and projected overlays share local CSS coordinates.
      // Bounding rectangles include ancestor transforms (e.g. a fitted slide).
      const measuredCanvasWidth = sceneNumber(canvas?.clientWidth || canvasRect?.width, 0);
      const measuredMountWidth = sceneNumber(mount?.clientWidth || mountRect?.width, 0);
      const measuredCanvasHeight = sceneNumber(canvas?.clientHeight || canvasRect?.height, 0);
      let measuredMountHeight = sceneNumber(mountRect && mountRect.height, 0);
      const mountClientHeight = sceneNumber(mount && mount.clientHeight, 0);
      if (mountClientHeight > 0 && typeof window !== "undefined" && window && typeof window.getComputedStyle === "function") {
        let mountStyle = null;
        try {
          mountStyle = window.getComputedStyle(mount);
        } catch (_error) {}
        const paddingTop = Math.max(0, sceneNumber(parseFloat(mountStyle && mountStyle.paddingTop), 0));
        const paddingBottom = Math.max(0, sceneNumber(parseFloat(mountStyle && mountStyle.paddingBottom), 0));
        // clientHeight excludes borders; subtract padding to match the content box
        // used by the canvas's width:100% sizing.
        measuredMountHeight = Math.max(0, mountClientHeight - paddingTop - paddingBottom);
      }
      if (measuredCanvasWidth > 0 && (measuredMountWidth <= 0 || measuredCanvasWidth <= measuredMountWidth * 1.5)) {
        cssWidth = measuredCanvasWidth;
      } else if (measuredMountWidth > 0) {
        cssWidth = measuredMountWidth;
      }
      // A fill-bound scene uses the mount's content-box height, not the
      // canvas's replaced-element aspect ratio. The canvas is block-level and
      // the label layer is absolute, so a content-sized mount feeds back only
      // the canvas height and cannot add line-box, padding, or overlay height.
      // Use the canvas only when the mount has not acquired a real box yet.
      const measuredHeight = useMeasuredHeight && measuredMountHeight > 0
        ? measuredMountHeight
        : measuredCanvasWidth > 0 && (measuredMountWidth <= 0 || measuredCanvasWidth <= measuredMountWidth * 1.5)
          ? measuredCanvasHeight
          : measuredMountHeight;
      if (useMeasuredHeight && measuredHeight > 0) {
        cssHeight = measuredHeight;
      } else if (cssWidth > 0) {
        cssHeight = cssWidth / Math.max(0.0001, base.aspectRatio);
      }
    }
    cssWidth = Math.max(1, Math.round(cssWidth));
    cssHeight = Math.max(1, Math.round(cssHeight));
    const capabilityMaxDevicePixelRatio = defaultSceneMaxDevicePixelRatio(capability);
    const minDevicePixelRatio = Math.max(1, Math.min(2, sceneNumber(
      props && (props.minDevicePixelRatio != null ? props.minDevicePixelRatio : props.minPixelRatio),
      1,
    )));
    let maxDevicePixelRatio = Math.max(
      1,
      base.explicitMaxDevicePixelRatio > 0
        ? Math.min(base.explicitMaxDevicePixelRatio, capabilityMaxDevicePixelRatio)
        : capabilityMaxDevicePixelRatio,
    );
    if (adaptiveQuality && adaptiveQuality.enabled && adaptiveQuality.currentMaxDevicePixelRatio > 0) {
      maxDevicePixelRatio = Math.max(
        1,
        Math.min(maxDevicePixelRatio, adaptiveQuality.currentMaxDevicePixelRatio),
      );
    }
    maxDevicePixelRatio = Math.max(maxDevicePixelRatio, minDevicePixelRatio);
    if (base.explicitMaxDevicePixelRatio > 0) {
      maxDevicePixelRatio = Math.min(maxDevicePixelRatio, base.explicitMaxDevicePixelRatio);
    }
    // maxPixels caps the render target by TOTAL backing pixels, which
    // maxDevicePixelRatio cannot: a ratio knows nothing about how large the
    // display is. The same props that push ~2 MP on a 1080p/DPR-1 screen push
    // ~6 MP on a Retina laptop, so any fill-bound scene (large soft sprites,
    // a big water surface, a multi-pass post-FX chain) silently costs 3x more
    // there and falls off a cliff. Deriving the ratio from a pixel budget makes
    // the cap resolution-aware. Opt-in: unset means the old behaviour exactly.
    const maxPixels = sceneNumber(props && (props.maxPixels || props.maxRenderPixels), 0);
    if (maxPixels > 0) {
      const cssPixels = Math.max(1, cssWidth * cssHeight);
      const budgetRatio = Math.sqrt(maxPixels / cssPixels);
      maxDevicePixelRatio = Math.max(1, Math.min(maxDevicePixelRatio, budgetRatio));
    }
    // An authored maximum overrides the automatic phone cap. Quality and
    // total-pixel budgets still apply, and the canvas keeps its CSS dimensions.
    if (!(base.explicitMaxDevicePixelRatio > 0)) {
      maxDevicePixelRatio = Math.min(maxDevicePixelRatio, scenePhoneDevicePixelRatioCap(capability));
    }
    let devicePixelRatio = sceneViewportDevicePixelRatio(props, maxDevicePixelRatio, minDevicePixelRatio);
    let pixelWidth = Math.max(1, Math.round(cssWidth * devicePixelRatio));
    let pixelHeight = Math.max(1, Math.round(cssHeight * devicePixelRatio));
    if (maxPixels > 0 && pixelWidth * pixelHeight > maxPixels) {
      const budgetRatio = Math.sqrt(maxPixels / Math.max(1, cssWidth * cssHeight));
      if (budgetRatio >= 1) {
        devicePixelRatio = Math.max(1, Math.min(devicePixelRatio, budgetRatio));
        pixelWidth = Math.max(1, Math.floor(cssWidth * devicePixelRatio));
        pixelHeight = Math.max(1, Math.floor(cssHeight * devicePixelRatio));
      }
    }
    return {
      cssWidth,
      cssHeight,
      devicePixelRatio,
      pixelWidth,
      pixelHeight,
    };
  }

  function sceneViewportEnvironmentSignature(environment) {
    if (!environment || typeof environment !== "object") {
      return "";
    }
    return [
      sceneNumber(environment.devicePixelRatio, 1).toFixed(3),
      Math.round(sceneNumber(environment.viewportWidth, 0)),
      Math.round(sceneNumber(environment.viewportHeight, 0)),
      Math.round(sceneNumber(environment.visualViewportWidth, 0)),
      Math.round(sceneNumber(environment.visualViewportHeight, 0)),
      environment.visualViewportActive ? "1" : "0",
    ].join("|");
  }

  function applySceneViewport(mount, canvas, labelLayer, viewport, base) {
    if (!mount || !canvas || !viewport) {
      return viewport;
    }
    setAttrValue(mount, "data-gosx-scene3d-css-width", viewport.cssWidth);
    setAttrValue(mount, "data-gosx-scene3d-css-height", viewport.cssHeight);
    setAttrValue(mount, "data-gosx-scene3d-pixel-ratio", viewport.devicePixelRatio);
    setStyleValue(mount.style, "--gosx-scene-css-width", viewport.cssWidth + "px");
    setStyleValue(mount.style, "--gosx-scene-css-height", viewport.cssHeight + "px");
    setStyleValue(mount.style, "--gosx-scene-pixel-ratio", String(viewport.devicePixelRatio));
    canvas.width = viewport.pixelWidth;
    canvas.height = viewport.pixelHeight;
    canvas.setAttribute("width", String(viewport.pixelWidth));
    canvas.setAttribute("height", String(viewport.pixelHeight));
    if (labelLayer) {
      // The canvas is a direct child of the positioned mount. Its offsets
      // share the absolute label layer's local CSS coordinate system.
      const left = sceneNumber(canvas.offsetLeft, 0);
      const top = sceneNumber(canvas.offsetTop, 0);
      labelLayer.style.position = "absolute";
      labelLayer.style.left = left + "px";
      labelLayer.style.top = top + "px";
      labelLayer.style.right = "auto";
      labelLayer.style.bottom = "auto";
      labelLayer.style.width = viewport.cssWidth + "px";
      labelLayer.style.height = viewport.cssHeight + "px";
    }
    if (base && !base.responsive) {
      canvas.style.width = viewport.cssWidth + "px";
      canvas.style.height = viewport.cssHeight + "px";
    } else {
      canvas.style.width = "100%";
      canvas.style.height = "auto";
      // A responsive Scene3D canvas is a block so it does not add an inline
      // baseline gap to a content-sized mount. The label layer is an absolute
      // overlay above, so neither child can add another viewport height.
      canvas.style.display = "block";
    }
    return viewport;
  }

  function observeSceneViewport(mount, refresh) {
    if (!mount || typeof refresh !== "function") {
      return function() {};
    }
    let resizeObserver = null;
    let windowResizeListener = null;
    let stopEnvironment = null;

    // Coalesce ResizeObserver / window.resize fires via a microtask flag so
    // rapid-fire events (e.g., Firefox subpixel canvas dim fluctuations during
    // scroll) collapse into at most one refresh per synchronous burst. Without
    // this guard, each fire calls scheduleRender unconditionally and piles
    // renders on top of the already-running rAF loop — observed as progressive
    // scroll jank on Firefox. A microtask-based flag is used rather than rAF
    // so the dedup doesn't leak a pending rAF across viewport activation
    // transitions (see runtime.test.js offscreen-rerender deferral test).
    var resizeRefreshPending = false;
    function scheduleResizeRefresh() {
      if (resizeRefreshPending) {
        return;
      }
      resizeRefreshPending = true;
      if (typeof Promise === "function") {
        Promise.resolve().then(function() {
          resizeRefreshPending = false;
          refresh("resize");
        });
      } else {
        resizeRefreshPending = false;
        refresh("resize");
      }
    }

    if (typeof ResizeObserver === "function") {
      resizeObserver = new ResizeObserver(scheduleResizeRefresh);
      if (typeof resizeObserver.observe === "function") {
        resizeObserver.observe(mount);
      }
    } else if (typeof window.addEventListener === "function") {
      windowResizeListener = scheduleResizeRefresh;
      window.addEventListener("resize", windowResizeListener);
    }

    if (window.__gosx.environment && typeof window.__gosx.environment.observe === "function") {
      let environmentSignature = sceneViewportEnvironmentSignature(sceneEnvironmentState());
      stopEnvironment = window.__gosx.environment.observe(function(environment) {
        const nextSignature = sceneViewportEnvironmentSignature(environment);
        if (environmentSignature === nextSignature) {
          return;
        }
        environmentSignature = nextSignature;
        refresh("environment");
      }, { immediate: false });
    }

    return function() {
      if (resizeObserver && typeof resizeObserver.disconnect === "function") {
        resizeObserver.disconnect();
      }
      if (windowResizeListener && typeof window.removeEventListener === "function") {
        window.removeEventListener("resize", windowResizeListener);
      }
      if (typeof stopEnvironment === "function") {
        stopEnvironment();
      }
    };
  }

  function initialSceneLifecycleState() {
    const environment = sceneEnvironmentState();
    return {
      pageVisible: environment ? Boolean(environment.pageVisible) : String(document && document.visibilityState || "visible").toLowerCase() !== "hidden",
      inViewport: true,
    };
  }

  function initialSceneMotionState(props) {
    const respectReducedMotion = sceneBool(props && props.respectReducedMotion, true);
    const environment = sceneEnvironmentState();
    return {
      respectReducedMotion,
      reducedMotion: respectReducedMotion && environment
        ? Boolean(environment.reducedMotion)
        : sceneMediaQueryMatches("(prefers-reduced-motion: reduce)"),
    };
  }

  function applySceneMotionState(mount, motion) {
    if (!mount || !motion) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-reduced-motion", motion.reducedMotion ? "true" : "false");
  }

  function observeSceneMotion(mount, motion, onChange) {
    if (!mount || !motion || typeof onChange !== "function") {
      return function() {};
    }

    applySceneMotionState(mount, motion);
    if (!motion.respectReducedMotion || !(window.__gosx.environment && typeof window.__gosx.environment.observe === "function")) {
      return function() {};
    }

    return window.__gosx.environment.observe(function(environment) {
      const next = Boolean(environment && environment.reducedMotion);
      if (motion.reducedMotion === next) {
        return;
      }
      motion.reducedMotion = next;
      applySceneMotionState(mount, motion);
      onChange("motion");
    }, { immediate: false });
  }

  function applySceneLifecycleState(mount, lifecycle) {
    if (!mount || !lifecycle) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-page-visible", lifecycle.pageVisible ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-in-viewport", lifecycle.inViewport ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-active", lifecycle.pageVisible && lifecycle.inViewport ? "true" : "false");
  }

  function sceneLifecyclePinnedToViewport(mount) {
    if (!mount || typeof window.getComputedStyle !== "function") {
      return false;
    }
    try {
      const position = String(window.getComputedStyle(mount).position || "").toLowerCase();
      return position === "fixed";
    } catch (_error) {
      return false;
    }
  }

  function observeSceneLifecycle(mount, lifecycle, onChange) {
    if (!mount || !lifecycle || typeof onChange !== "function") {
      return function() {};
    }

    let stopIntersection = null;
    let stopEnvironment = null;

    if (sceneLifecyclePinnedToViewport(mount)) {
      lifecycle.inViewport = true;
    } else if (typeof IntersectionObserver === "function") {
      const observer = new IntersectionObserver(function(entries) {
        for (const entry of entries || []) {
          if (!entry || entry.target !== mount) {
            continue;
          }
          const next = entry.isIntersecting !== false && sceneNumber(entry.intersectionRatio, 1) > 0;
          if (lifecycle.inViewport === next) {
            continue;
          }
          lifecycle.inViewport = next;
          applySceneLifecycleState(mount, lifecycle);
          onChange("intersection");
        }
      }, { threshold: [0, 0.01, 0.25] });
      if (typeof observer.observe === "function") {
        observer.observe(mount);
      }
      stopIntersection = function() {
        if (typeof observer.disconnect === "function") {
          observer.disconnect();
        }
      };
    }

    if (window.__gosx.environment && typeof window.__gosx.environment.observe === "function") {
      stopEnvironment = window.__gosx.environment.observe(function(environment) {
        const next = Boolean(environment && environment.pageVisible);
        if (lifecycle.pageVisible === next) {
          return;
        }
        lifecycle.pageVisible = next;
        applySceneLifecycleState(mount, lifecycle);
        onChange("visibility");
      }, { immediate: false });
    }

    applySceneLifecycleState(mount, lifecycle);
    return function() {
      if (stopIntersection) {
        stopIntersection();
      }
      if (typeof stopEnvironment === "function") {
        stopEnvironment();
      }
    };
  }
