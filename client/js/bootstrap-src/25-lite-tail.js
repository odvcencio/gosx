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
    if (gosxHost.dom && typeof gosxHost.dom.dispose === "function") {
      gosxHost.dom.dispose(document.body || document.documentElement);
    } else {
      if (gosxHost.regions && typeof gosxHost.regions.dispose === "function") {
        gosxHost.regions.dispose(document.body || document.documentElement);
      }
      if (gosxHost.surfaces && typeof gosxHost.surfaces.dispose === "function") {
        gosxHost.surfaces.dispose(document.body || document.documentElement);
      }
      disposeManagedMotion();
      disposeManagedTextLayouts();
    }
  }

  function bootstrapLitePage() {
    refreshGosxEnvironmentState("bootstrap-lite");
    refreshGosxDocumentState("bootstrap-lite");
    if (gosxHost.dom && typeof gosxHost.dom.mount === "function") {
      gosxHost.dom.mount(document.body || document.documentElement);
    } else {
      mountManagedMotion(document.body || document.documentElement);
      mountManagedTextLayouts(document.body || document.documentElement);
      if (gosxHost.surfaces && typeof gosxHost.surfaces.mount === "function") {
        gosxHost.surfaces.mount(document.body || document.documentElement);
      }
      if (gosxHost.stream && typeof gosxHost.stream.consume === "function") {
        gosxHost.stream.consume(document.body || document.documentElement);
      }
      if (gosxHost.regions && typeof gosxHost.regions.mount === "function") {
        gosxHost.regions.mount(document.body || document.documentElement);
      }
    }
    window.__gosx.ready = true;
    refreshGosxDocumentState("ready");
  }

  gosxHost.lifecycle = Object.assign(gosxHost.lifecycle || {}, {
    bootstrapPage: bootstrapLitePage,
    disposePage: disposeBootstrapOnlyPage,
  });
  gosxHostCompatibility.install("__gosx_bootstrap_page", bootstrapLitePage);
  gosxHostCompatibility.install("__gosx_dispose_page", disposeBootstrapOnlyPage);

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", bootstrapLitePage);
  } else {
    bootstrapLitePage();
  }
})();
