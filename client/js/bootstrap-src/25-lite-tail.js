  // --------------------------------------------------------------------------
  // Bootstrap-only initialization
  // --------------------------------------------------------------------------

  function hasAttributeName(el, attr) {
    return Boolean(el && el.hasAttribute && el.hasAttribute(attr));
  }

  function sceneNumber(value, fallback) {
    const number = Number(value);
    return Number.isFinite(number) ? number : fallback;
  }

  function disposeBootstrapOnlyPage() {
    disposeManagedMotion();
    disposeManagedTextLayouts();
  }

  function bootstrapLitePage() {
    refreshGosxEnvironmentState("bootstrap-lite");
    refreshGosxDocumentState("bootstrap-lite");
    mountManagedMotion(document.body || document.documentElement);
    mountManagedTextLayouts(document.body || document.documentElement);
    window.__gosx.ready = true;
    refreshGosxDocumentState("ready");
  }

  window.__gosx_bootstrap_page = bootstrapLitePage;
  window.__gosx_dispose_page = disposeBootstrapOnlyPage;

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", bootstrapLitePage);
  } else {
    bootstrapLitePage();
  }
})();
