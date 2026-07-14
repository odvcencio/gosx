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
    if (typeof window.__gosx_dispose_runtime_content === "function") {
      window.__gosx_dispose_runtime_content(document.body || document.documentElement);
    } else {
      if (typeof window.__gosx_dispose_declarative_regions === "function") {
        window.__gosx_dispose_declarative_regions(document.body || document.documentElement);
      }
      if (typeof window.__gosx_dispose_runtime_surfaces === "function") {
        window.__gosx_dispose_runtime_surfaces(document.body || document.documentElement);
      }
      disposeManagedMotion();
      disposeManagedTextLayouts();
    }
  }

  function bootstrapLitePage() {
    refreshGosxEnvironmentState("bootstrap-lite");
    refreshGosxDocumentState("bootstrap-lite");
    if (typeof window.__gosx_mount_runtime_content === "function") {
      window.__gosx_mount_runtime_content(document.body || document.documentElement);
    } else {
      mountManagedMotion(document.body || document.documentElement);
      mountManagedTextLayouts(document.body || document.documentElement);
      if (typeof window.__gosx_mount_runtime_surfaces === "function") {
        window.__gosx_mount_runtime_surfaces(document.body || document.documentElement);
      }
      if (typeof window.__gosx_mount_stream_templates === "function") {
        window.__gosx_mount_stream_templates(document.body || document.documentElement);
      }
      if (typeof window.__gosx_mount_declarative_regions === "function") {
        window.__gosx_mount_declarative_regions(document.body || document.documentElement);
      }
    }
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
