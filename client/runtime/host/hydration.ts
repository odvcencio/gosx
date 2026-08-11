// @ts-check
// GoSX browser host: island hydration: fetch island program data, call the WASM hydrate
// entry point, and attach delegated listeners.
//
// Chunks: bootstrap.js, bootstrap-feature-islands.js.
// Calls 30h for the capability probe and 30a for event delegation.

/**
 * @typedef {object} GoSXIslandEntry
 * @property {string} id
 * @property {boolean} [static]
 * @property {string} [programRef]
 * @property {string} [programFormat]
 * @property {string} [component]
 */
  const RUNTIME_EXPORT_WAIT_MS = 500;

  function runtimeExport(name) {
    const fn = window[name];
    return typeof fn === "function" ? fn : null;
  }

  function chooseRuntimeExport(names) {
    for (const name of names) {
      const fn = runtimeExport(name);
      if (fn) return fn;
    }
    return null;
  }

  function runtimeExportWaitNow() {
    return typeof performance !== "undefined" && performance && typeof performance.now === "function"
      ? performance.now()
      : Date.now();
  }

  function waitForRuntimeExport(names) {
    const ready = chooseRuntimeExport(names);
    if (ready) return Promise.resolve(ready);

    const start = runtimeExportWaitNow();
    return new Promise(function(resolve) {
      function check() {
        const fn = chooseRuntimeExport(names);
        if (fn) {
          resolve(fn);
          return;
        }
        const now = runtimeExportWaitNow();
        if (now - start >= RUNTIME_EXPORT_WAIT_MS) {
          resolve(null);
          return;
        }
        setTimeout(check, 4);
      }
      setTimeout(check, 0);
    });
  }

  async function hydrateIsland(entry) {
    const root = islandRoot(entry);
    if (!root) return;
    if (entry.static) return;

    const program = await loadIslandProgram(entry, root);
    if (!program) return;
    const hydrateFn = await waitForRuntimeExport(["__gosx_hydrate"]);
    if (!runIslandHydration(entry, root, program, hydrateFn)) return;
    const listeners = setupEventDelegation(root, entry.id);
    rememberHydratedIsland(entry, root, listeners);
  }

  async function hydrateComputeIsland(entry) {
    if (!entry || entry.static) return;
    await prepareRuntimeCapabilityProbe(entry);
    const capabilityStatus = runtimeCapabilityStatus(entry);
    if (!capabilityStatus.ok) {
      reportMissingComputeIslandCapabilities(entry, capabilityStatus);
      return;
    }

    const program = await loadIslandProgram(entry, null);
    if (!program) return;
    const hydrateFn = await waitForRuntimeExport(["__gosx_hydrate_compute", "__gosx_hydrate"]);
    if (!runComputeIslandHydration(entry, program, hydrateFn)) return;
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

  function runIslandHydration(entry, root, program, hydrateFn) {
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

  function runComputeIslandHydration(entry, program, hydrateFn) {
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

  // Hydrate all islands from the manifest. Called once the WASM runtime
  // signals readiness via __gosx_runtime_ready.
  async function hydrateAllIslands(manifest) {
    const islands = Array.isArray(manifest && manifest.islands) ? manifest.islands : [];
    const computeIslands = Array.isArray(manifest && manifest.computeIslands) ? manifest.computeIslands : [];
    if (islands.length === 0 && computeIslands.length === 0) return;

    // Hydrate islands concurrently — each is independent.
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
