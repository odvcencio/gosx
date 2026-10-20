
  return { name: "textlayout" };
  }

  if (typeof window.__gosx_register_bootstrap_feature === "function") {
    window.__gosx_register_bootstrap_feature("textlayout", runTextLayoutEngine);
  } else {
    runTextLayoutEngine();
  }
})();
