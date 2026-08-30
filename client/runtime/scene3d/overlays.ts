// overlays.ts — the Scene3D stats and inspector overlays.
// @ts-check
//
// Both are development aids. createSceneStatsOverlay draws the frame counter
// and createSceneInspectorOverlay draws the scene-graph inspector. Each stays
// inert unless the scene props enable it.
//
// This file is a gate candidate: a production scene enables neither.

/**
 * @typedef {object} GoSXSceneOverlayController
 * @property {HTMLElement} element
 * @property {(bundle: object, frameStart: number, renderer: object, viewport: object) => void} update
 */
  function createSceneStatsOverlay(mount, enabled) {
    if (!mount || !enabled || typeof document.createElement !== "function") {
      return null;
    }
    const element = document.createElement("div");
    element.setAttribute("data-gosx-scene3d-stats", "true");
    element.setAttribute("aria-hidden", "true");
    element.style.position = "absolute";
    element.style.left = "8px";
    element.style.top = "8px";
    element.style.zIndex = "8";
    element.style.pointerEvents = "none";
    element.style.font = "11px/1.35 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace";
    element.style.color = "#d9f7ff";
    element.style.background = "rgba(4, 10, 16, 0.72)";
    element.style.border = "1px solid rgba(141, 225, 255, 0.22)";
    element.style.borderRadius = "6px";
    element.style.padding = "6px 7px";
    element.style.whiteSpace = "pre";
    element.style.backdropFilter = "blur(6px)";
    mount.appendChild(element);
    return {
      element,
      update(bundle, frameStart, renderer, viewport) {
        const now = typeof performance !== "undefined" && performance.now ? performance.now() : Date.now();
        const frameMS = Math.max(0, now - sceneNumber(frameStart, now));
        const fps = frameMS > 0 ? Math.min(999, 1000 / frameMS) : 0;
        const meshes = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects.length : 0;
        const world = Array.isArray(bundle && bundle.objects) ? bundle.objects.length : 0;
        const points = Array.isArray(bundle && bundle.points) ? bundle.points.length : 0;
        const instanced = Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes.length : 0;
        const surfaces = Array.isArray(bundle && bundle.surfaces) ? bundle.surfaces.length : 0;
        const drawCalls = meshes + world + points + instanced + surfaces;
        const materialCount = Array.isArray(bundle && bundle.materials) ? bundle.materials.length : 0;
        element.textContent =
          "fps " + fps.toFixed(0) + "  ms " + frameMS.toFixed(1) + "\n" +
          "draw " + drawCalls + "  mat " + materialCount + "\n" +
          "meshV " + Math.floor(sceneNumber(bundle && bundle.worldMeshVertexCount, 0)) +
          "  lineV " + Math.floor(sceneNumber(bundle && bundle.worldVertexCount, 0)) + "\n" +
          String(renderer && (renderer.type || renderer.kind) || "renderer") +
          "  " + Math.floor(sceneNumber(viewport && viewport.cssWidth, 0)) + "x" + Math.floor(sceneNumber(viewport && viewport.cssHeight, 0));
      },
      dispose() {
        if (element.parentNode === mount) {
          mount.removeChild(element);
        }
      },
    };
  }

  function createSceneInspectorOverlay(mount, enabled, readSnapshot) {
    if (!mount || !enabled || typeof document.createElement !== "function") {
      return null;
    }
    const element = document.createElement("div");
    element.setAttribute("data-gosx-scene3d-inspector", "true");
    element.setAttribute("aria-hidden", "true");
    element.style.position = "absolute";
    element.style.right = "8px";
    element.style.top = "8px";
    element.style.zIndex = "9";
    element.style.pointerEvents = "none";
    element.style.font = "11px/1.35 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace";
    element.style.color = "#edf8f2";
    element.style.background = "rgba(3, 12, 10, 0.78)";
    element.style.border = "1px solid rgba(139, 235, 190, 0.24)";
    element.style.borderRadius = "6px";
    element.style.padding = "7px 8px";
    element.style.whiteSpace = "pre";
    element.style.minWidth = "190px";
    element.style.maxWidth = "280px";
    element.style.backdropFilter = "blur(6px)";
    mount.setAttribute("data-gosx-scene3d-inspector-enabled", "true");
    mount.appendChild(element);

    function update() {
      const snapshot = typeof readSnapshot === "function" ? readSnapshot() : null;
      if (!snapshot) {
        element.textContent = "Scene3D\npending";
        return;
      }
      const counts = snapshot.counts || {};
      const viewport = snapshot.viewport || {};
      const resources = snapshot.gpuResources || {};
      const renderLoop = snapshot.renderLoop || {};
      const htmlTextures = resources.htmlTextures || {};
      const diagnostics = Array.isArray(snapshot.diagnostics) ? snapshot.diagnostics : [];
      let warnCount = 0;
      let errorCount = 0;
      for (let i = 0; i < diagnostics.length; i += 1) {
        const severity = String(diagnostics[i] && diagnostics[i].severity || "").toLowerCase();
        if (severity === "warn" || severity === "warning") {
          warnCount += 1;
        } else if (severity === "error" || severity === "fatal") {
          errorCount += 1;
        }
      }
      const renderer = snapshot.renderer || "unknown";
      const fallback = snapshot.fallbackReason ? " fallback " + snapshot.fallbackReason : "";
      const lastPick = snapshot.lastPick || null;
      const pickTarget = lastPick && (lastPick.targetID || lastPick.objectID || lastPick.hoverID || lastPick.downID || lastPick.id);
      const pickType = lastPick && lastPick.type ? String(lastPick.type) : "pick";
      const meshCount = sceneNumber(counts.meshObjects, 0) + sceneNumber(counts.worldObjects, 0);
      const lines = [
        "Scene3D",
        "backend " + renderer + fallback,
        "ready " + (snapshot.ready ? "yes" : "no") + "  active " + (snapshot.active ? "yes" : "no"),
        "loop " + (renderLoop.active ? "active" : "stopped") + "  " + (renderLoop.reason || "unknown"),
        "view " + Math.floor(sceneNumber(viewport.cssWidth, 0)) + "x" + Math.floor(sceneNumber(viewport.cssHeight, 0)) +
          "  dpr " + sceneNumber(viewport.devicePixelRatio, 1).toFixed(2),
        "draw " + Math.floor(sceneNumber(counts.drawCalls, 0)) + "  mat " + Math.floor(sceneNumber(counts.materials, 0)),
        "mesh " + Math.floor(meshCount) + "  inst " + Math.floor(sceneNumber(counts.instancedMeshes, 0)),
        "html " + Math.floor(sceneNumber(counts.html, 0)) + "  tex " +
          Math.floor(sceneNumber(htmlTextures.ready, 0)) + "/" + Math.floor(sceneNumber(htmlTextures.count, 0)),
        "diag " + diagnostics.length + "  warn " + warnCount + "  err " + errorCount,
      ];
      if (pickTarget) {
        lines.push("pick " + pickType + " " + String(pickTarget));
      }
      element.textContent = lines.join("\n");
    }

    update();
    return {
      element,
      update,
      dispose() {
        if (element.parentNode === mount) {
          mount.removeChild(element);
        }
      },
    };
  }
