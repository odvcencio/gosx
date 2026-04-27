(function() {
  "use strict";

  const registerFeature = window.__gosx_register_bootstrap_feature;
  if (typeof registerFeature !== "function") {
    console.error("[gosx] runtime bootstrap feature registry missing");
    return;
  }

  registerFeature("islands", function(api) {
    const fetchProgram = api.fetchProgram;
    const inferProgramFormat = api.inferProgramFormat;
    const runtimeCapabilityStatus = api.runtimeCapabilityStatus;
    const applyRuntimeCapabilityState = api.applyRuntimeCapabilityState;
    const activateInputProviders = api.activateInputProviders;
    const releaseInputProviders = api.releaseInputProviders;
    const capabilityList = api.capabilityList;

  const DELEGATED_EVENTS = [
    "click", "input", "change", "submit",
    "keydown", "keyup", "focus", "blur",
  ];

  function extractEventData(e) {
    const data = { type: e.type };

    switch (e.type) {
      case "click":
        if (e.target && e.target.value !== undefined) {
          const value = String(e.target.value == null ? "" : e.target.value);
          if (value !== "") {
            data.value = value;
          }
        }
        break;
      case "input":
      case "change":
        if (e.target && e.target.value !== undefined) {
          data.value = e.target.value;
        }
        break;
      case "keydown":
      case "keyup":
        data.key = e.key;
        break;
      case "submit":
        e.preventDefault();
        break;
    }

    return data;
  }

  function findHandlerForEvent(target, root, eventType) {
    const specificAttr = handlerAttrName(eventType);

    let el = target;
    while (el && el !== root.parentNode) {
      const handlerName = elementHandlerName(el, eventType, specificAttr);
      if (handlerName) {
        return handlerName;
      }
      el = el.parentNode;
    }
    return null;
  }

  function handlerAttrName(eventType) {
    return "data-gosx-on-" + eventType;
  }

  function elementHandlerName(el, eventType, specificAttr) {
    if (hasAttributeName(el, specificAttr)) {
      return el.getAttribute(specificAttr);
    }
    if (eventType === "click" && hasAttributeName(el, "data-gosx-handler")) {
      return el.getAttribute("data-gosx-handler");
    }
    return null;
  }

  function hasAttributeName(el, attr) {
    return Boolean(el && el.hasAttribute && el.hasAttribute(attr));
  }

  function setupEventDelegation(islandRoot, islandID) {
    const entries = [];

    for (const eventType of DELEGATED_EVENTS) {
      const listener = createDelegatedListener(islandRoot, islandID, eventType);
      const useCapture = delegatedEventCapture(eventType);
      islandRoot.addEventListener(eventType, listener, useCapture);
      entries.push({ type: eventType, listener, capture: useCapture });
    }

    return entries;
  }

  function delegatedEventCapture(eventType) {
    return eventType === "focus" || eventType === "blur";
  }

  function createDelegatedListener(islandRoot, islandID, eventType) {
    return function(e) {
      if (e.__gosx_handled) return;

      const handlerName = findHandlerForEvent(e.target, islandRoot, eventType);
      if (!handlerName) return;

      e.__gosx_handled = true;
      dispatchIslandAction(islandID, handlerName, extractEventData(e));
    };
  }

  function dispatchIslandAction(islandID, handlerName, eventData) {
    const actionFn = window.__gosx_action;
    if (typeof actionFn !== "function") return;

    try {
      const result = actionFn(islandID, handlerName, JSON.stringify(eventData));
      if (typeof result === "string" && result !== "") {
        console.error(`[gosx] action error (${islandID}/${handlerName}):`, result);
      }
    } catch (err) {
      console.error(`[gosx] action error (${islandID}/${handlerName}):`, err);
    }
  }

  window.__gosx_dispose_island = function(islandID) {
    const record = window.__gosx.islands.get(islandID);
    if (!record) return;

    if (record.root && record.listeners) {
      for (const entry of record.listeners) {
        record.root.removeEventListener(entry.type, entry.listener, entry.capture);
      }
    }

    if (typeof window.__gosx_dispose === "function") {
      try {
        window.__gosx_dispose(islandID);
      } catch (e) {
        console.error(`[gosx] dispose error for ${islandID}:`, e);
      }
    }

    window.__gosx.islands.delete(islandID);
  };

  window.__gosx_dispose_compute_island = function(islandID) {
    const record = window.__gosx.computeIslands && window.__gosx.computeIslands.get(islandID);
    if (!record) return;

    releaseInputProviders(record);

    if (typeof window.__gosx_dispose === "function") {
      try {
        window.__gosx_dispose(islandID);
      } catch (e) {
        console.error(`[gosx] dispose error for compute island ${islandID}:`, e);
      }
    }

    window.__gosx.computeIslands.delete(islandID);
  };

  async function hydrateIsland(entry) {
    const root = islandRoot(entry);
    if (!root) return;
    if (entry.static) return;

    const program = await loadIslandProgram(entry, root);
    if (!program) return;
    if (!runIslandHydration(entry, root, program)) return;
    const listeners = setupEventDelegation(root, entry.id);
    rememberHydratedIsland(entry, root, listeners);
  }

  async function hydrateComputeIsland(entry) {
    if (!entry || entry.static) return;
    const capabilityStatus = runtimeCapabilityStatus(entry);
    if (!capabilityStatus.ok) {
      reportMissingComputeIslandCapabilities(entry, capabilityStatus);
      return;
    }

    const program = await loadIslandProgram(entry, null);
    if (!program) return;
    if (!runComputeIslandHydration(entry, program)) return;
    activateInputProviders(entry);
    rememberHydratedComputeIsland(entry);
  }

  function reportMissingComputeIslandCapabilities(entry, status) {
    const missing = status.missing.join(" ");
    console.error(`[gosx] missing required compute island capabilities for ${entry.id}: ${missing}`);
    if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
      window.__gosx_emit("error", "compute-island", "missing required compute island capabilities", {
        islandID: String(entry.id || ""),
        component: String(entry.component || ""),
        missing,
      });
    }
    if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
      window.__gosx.reportIssue({
        scope: "compute-island",
        type: "capability",
        component: entry.component,
        source: entry.id,
        message: `missing required compute island capabilities: ${missing}`,
        fallback: "none",
      });
    }
  }

  function islandRoot(entry) {
    const root = document.getElementById(entry.id);
    if (!root) {
      console.warn(`[gosx] island root #${entry.id} not found in DOM`);
      return null;
    }
    return root;
  }

  async function loadIslandProgram(entry, root) {
    const programFormat = inferProgramFormat(entry);
    if (!entry.programRef) {
      console.error(`[gosx] skipping island ${entry.id} — missing programRef`);
      if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
        window.__gosx_emit("error", "island", "missing programRef", {
          islandID: String(entry.id || ""),
          component: String(entry.component || ""),
        });
      }
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "island",
          type: "program",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          element: root,
          message: `missing programRef for island ${entry.id}`,
          fallback: "server",
        });
      }
      return null;
    }

    const programData = await fetchProgram(entry.programRef, programFormat);
    if (programData === null) {
      console.error(`[gosx] skipping island ${entry.id} — program fetch failed`);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "island",
          type: "program",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          element: root,
          message: `failed to fetch island program for ${entry.id}`,
          fallback: "server",
        });
      }
      return null;
    }
    return { data: programData, format: programFormat };
  }

  function runIslandHydration(entry, root, program) {
    const hydrateFn = window.__gosx_hydrate;
    if (typeof hydrateFn !== "function") {
      console.error("[gosx] __gosx_hydrate not available — cannot hydrate island", entry.id);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "island",
          type: "hydrate",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          element: root,
          message: `__gosx_hydrate not available for island ${entry.id}`,
          fallback: "server",
        });
      }
      return false;
    }

    try {
      const result = hydrateFn(
        entry.id,
        entry.component,
        JSON.stringify(entry.props || {}),
        program.data,
        program.format
      );
      if (typeof result === "string" && result !== "") {
        console.error(`[gosx] failed to hydrate island ${entry.id}: ${result}`);
        if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
          window.__gosx_emit("error", "island", "failed to hydrate island", {
            islandID: String(entry.id || ""),
            component: String(entry.component || ""),
            programRef: String(entry.programRef || ""),
            reason: String(result),
          });
        }
        if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
          window.__gosx.reportIssue({
            scope: "island",
            type: "hydrate",
            component: entry.component,
            source: entry.id,
            ref: entry.programRef,
            element: root,
            message: result,
            fallback: "server",
          });
        }
        return false;
      }
      return true;
    } catch (e) {
      console.error(`[gosx] failed to hydrate island ${entry.id}:`, e);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "island",
          type: "hydrate",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          element: root,
          message: `failed to hydrate island ${entry.id}`,
          error: e,
          fallback: "server",
        });
      }
      return false;
    }
  }

  function runComputeIslandHydration(entry, program) {
    const hydrateFn = typeof window.__gosx_hydrate_compute === "function"
      ? window.__gosx_hydrate_compute
      : window.__gosx_hydrate;
    if (typeof hydrateFn !== "function") {
      console.error("[gosx] __gosx_hydrate_compute not available — cannot hydrate compute island", entry.id);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "compute-island",
          type: "hydrate",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          message: `__gosx_hydrate_compute not available for compute island ${entry.id}`,
          fallback: "none",
        });
      }
      return false;
    }

    try {
      const result = hydrateFn(
        entry.id,
        entry.component,
        JSON.stringify(entry.props || {}),
        program.data,
        program.format
      );
      if (typeof result === "string" && result !== "") {
        console.error(`[gosx] failed to hydrate compute island ${entry.id}: ${result}`);
        if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
          window.__gosx_emit("error", "compute-island", "failed to hydrate compute island", {
            islandID: String(entry.id || ""),
            component: String(entry.component || ""),
            programRef: String(entry.programRef || ""),
            reason: String(result),
          });
        }
        if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
          window.__gosx.reportIssue({
            scope: "compute-island",
            type: "hydrate",
            component: entry.component,
            source: entry.id,
            ref: entry.programRef,
            message: result,
            fallback: "none",
          });
        }
        return false;
      }
      return true;
    } catch (e) {
      console.error(`[gosx] failed to hydrate compute island ${entry.id}:`, e);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "compute-island",
          type: "hydrate",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          message: `failed to hydrate compute island ${entry.id}`,
          error: e,
          fallback: "none",
        });
      }
      return false;
    }
  }

  function rememberHydratedIsland(entry, root, listeners) {
    if (window.__gosx && typeof window.__gosx.clearIssueState === "function") {
      window.__gosx.clearIssueState(root);
    }
    window.__gosx.islands.set(entry.id, {
      component: entry.component,
      root: root,
      listeners: listeners,
    });
  }

  function rememberHydratedComputeIsland(entry) {
    if (!window.__gosx.computeIslands) {
      window.__gosx.computeIslands = new Map();
    }
    window.__gosx.computeIslands.set(entry.id, {
      component: entry.component,
      capabilities: capabilityList(entry),
    });
  }

  async function hydrateAllIslands(manifest) {
    const islands = Array.isArray(manifest && manifest.islands) ? manifest.islands : [];
    const computeIslands = Array.isArray(manifest && manifest.computeIslands) ? manifest.computeIslands : [];
    if (islands.length === 0 && computeIslands.length === 0) return;

    const promises = islands.map(function(entry) {
      return hydrateIsland(entry).catch(function(e) {
        console.error(`[gosx] unexpected error hydrating ${entry.id}:`, e);
      });
    });
    for (const entry of computeIslands) {
      promises.push(hydrateComputeIsland(entry).catch(function(e) {
        console.error(`[gosx] unexpected error hydrating compute island ${entry.id}:`, e);
      }));
    }

    await Promise.all(promises);
  }

    return {
      runtimeReady(manifest) {
        return hydrateAllIslands(manifest);
      },
      disposePage() {
        for (const islandID of Array.from(window.__gosx.islands.keys())) {
          window.__gosx_dispose_island(islandID);
        }
        if (window.__gosx.computeIslands) {
          for (const islandID of Array.from(window.__gosx.computeIslands.keys())) {
            window.__gosx_dispose_compute_island(islandID);
          }
        }
      },
      disposeIsland: window.__gosx_dispose_island,
      disposeComputeIsland: window.__gosx_dispose_compute_island,
    };
  });
})();
//# sourceMappingURL=bootstrap-feature-islands.js.map
