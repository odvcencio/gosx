// @ts-check
// Native document navigation starts before pagehide: its response can rotate
// session cookies while the old document is still executing async factories.
// Install this in the core bootstrap, before any engine feature/module awaits.
(function() {
  if (typeof gosxHost.lifecycle.documentActive === "function") return;

  let active = true;
  let hidden = false;
  let transition = 0;
  let controller = new AbortController();
  const waiters = new Set();

  function suspend() {
    if (!active) return;
    active = false;
    controller.abort();
  }

  function resume() {
    if (hidden || active) return;
    active = true;
    controller = new AbortController();
    for (const resolve of waiters) resolve();
    waiters.clear();
  }

  Object.assign(gosxHost.lifecycle, {
    documentActive() { return active; },
    documentSignal() { return controller.signal; },
    whenDocumentActive() {
      return active ? Promise.resolve() : new Promise(function(resolve) { waiters.add(resolve); });
    },
  });

  const navigation = window.navigation;
  if (navigation && typeof navigation.addEventListener === "function") {
    navigation.addEventListener("navigate", function(event) {
      // Hash/history changes keep the document and its credentials. GoSX's
      // enhanced navigation already owns its own per-page disposal lifecycle.
      if (event.destination && event.destination.sameDocument) return;
      const current = ++transition;
      suspend();
      if (event.signal) event.signal.addEventListener("abort", function() {
        // Aborting one navigation can synchronously start its replacement.
        // Never let the old abort re-enable the replacement's outgoing page.
        queueMicrotask(function() { if (current === transition) resume(); });
      }, { once: true });
    });
    navigation.addEventListener("navigatesuccess", resume);
    navigation.addEventListener("navigateerror", resume);
  }
  // Also covers reload/close and browsers without the Navigation API. This
  // listener never prevents unloading or asks the browser to show a prompt.
  window.addEventListener("beforeunload", suspend);
  window.addEventListener("pagehide", function() { hidden = true; suspend(); });
  window.addEventListener("pageshow", function(event) {
    // A cold outgoing document can finish its initial load while its native
    // form request is pending. Only an actual BFCache restoration revives it.
    if (!event.persisted) return;
    hidden = false;
    ++transition;
    resume();
  });
})();
