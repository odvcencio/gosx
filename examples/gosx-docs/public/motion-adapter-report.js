// Motion docs demo: shows which renderer and GPU adapter the demo's Scene3D
// runs on, so a reader (or a screenshot) can tell real GPU rendering from a
// software fallback.
//
// Markup: <output data-gosx-motion-adapter-report="#scene-selector">.
// Scene3D publishes its renderer truth on the mount as the
// data-gosx-scene3d-render-backend-truth attribute and the
// __gosxScene3DRenderBackendTruth property; this script copies it into the
// report.
//
// The observer watches that one attribute and nothing else. The report is
// written inside the observed document, so an observer that also watched
// childList would see its own textContent write, run again, write again, and
// never let the page yield. Writes are also skipped when nothing changed.
(function () {
  "use strict";

  if (typeof window.__gosxDocsMotionAdapterReport === "function") {
    window.__gosxDocsMotionAdapterReport();
    return;
  }

  function sceneTruth(scene) {
    if (scene.__gosxScene3DRenderBackendTruth) return scene.__gosxScene3DRenderBackendTruth;
    try {
      return JSON.parse(scene.getAttribute("data-gosx-scene3d-render-backend-truth") || "null");
    } catch (_error) {
      return null;
    }
  }

  function setText(element, text) {
    if (element.textContent !== text) element.textContent = text;
  }

  function setAttr(element, name, value) {
    if (element.getAttribute(name) !== value) element.setAttribute(name, value);
  }

  function update() {
    var reports = document.querySelectorAll("[data-gosx-motion-adapter-report]");
    for (var i = 0; i < reports.length; i++) {
      var report = reports[i];
      var scene = null;
      try {
        scene = document.querySelector(report.getAttribute("data-gosx-motion-adapter-report") || "");
      } catch (_error) {}
      var truth = scene ? sceneTruth(scene) : null;
      if (!truth) continue;
      var info = truth.adapterInfo || {};
      var adapter = [info.vendor, info.architecture, info.device, info.description].filter(Boolean).join(" ")
        || truth.adapter || "adapter details unavailable";
      var backend = truth.backend || "";
      setText(report, (backend || "renderer") + " adapter: " + adapter);
      setAttr(report, "data-gosx-motion-renderer", backend);
      setAttr(report, "data-gosx-motion-adapter", adapter);
    }
  }

  if (typeof MutationObserver === "function") {
    new MutationObserver(update).observe(document.documentElement, {
      subtree: true,
      attributes: true,
      attributeFilter: ["data-gosx-scene3d-render-backend-truth"],
    });
  }
  // The script loads once per document; soft navigation back to this page
  // swaps the body, so refresh from the navigation event too.
  document.addEventListener("gosx:navigate", update);
  window.__gosxDocsMotionAdapterReport = update;
  update();
})();
