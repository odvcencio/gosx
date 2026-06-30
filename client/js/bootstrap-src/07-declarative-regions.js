// 07-declarative-regions.js — bootstrap-owned declarative server-fragment regions.
//
// An htmx-lite region that re-fetches an HTML fragment and swaps it into place
// when a named shared signal changes or a hub event fires — so apps express
// "live panel" behavior declaratively, with no bespoke fetch/innerHTML JS.
// Self-contained IIFE; resolves runtime globals lazily.
//
//   <div data-gosx-region
//        data-gosx-region-url="/api/.../selection/{value}"  (the {value} token,
//             if present, is filled from the signal's current value, URL-encoded;
//             an empty value suppresses the fetch)
//        data-gosx-region-signal="$some.signal"  (optional: refetch on change)
//        data-gosx-region-on="change"            (optional: hub event names —
//             space/comma separated — that also retrigger a refetch)
//        data-gosx-region-field="tree_html">     (optional: JSON field to inject;
//             absent → the raw response body is the HTML)
//     ...server-rendered initial fragment...
//   </div>
(function () {
  if (typeof document === "undefined" || window.__gosxDeclarativeRegions) return;
  window.__gosxDeclarativeRegions = true;

  function splitEvents(spec) {
    return String(spec || "")
      .split(/[\s,]+/)
      .map(function (s) { return s.trim(); })
      .filter(Boolean);
  }

  function bindRegion(el) {
    var url = el.getAttribute("data-gosx-region-url");
    if (!url) return;
    var signalName = el.getAttribute("data-gosx-region-signal");
    var onEvents = splitEvents(el.getAttribute("data-gosx-region-on"));
    var field = el.getAttribute("data-gosx-region-field") || "";
    // allow-empty: still fetch when {value} is empty, substituting "" (e.g. a
    // tree fragment ?selected= with no selection). Without it, an empty {value}
    // suppresses the fetch (the default — avoids hitting e.g. /selection/ with a
    // blank id).
    var allowEmpty = el.hasAttribute("data-gosx-region-allow-empty");
    var lastValue = "";

    function refresh() {
      var u = url;
      if (u.indexOf("{value}") >= 0) {
        if (!lastValue && !allowEmpty) return; // no value yet → nothing to fetch
        u = u.replace("{value}", encodeURIComponent(lastValue || ""));
      }
      fetch(u, { headers: { Accept: field ? "application/json" : "text/html" } })
        .then(function (r) { return r.ok ? (field ? r.json() : r.text()) : null; })
        .then(function (data) {
          if (data == null) return;
          var html = field ? data[field] : data;
          if (typeof html === "string") el.innerHTML = html;
        })
        .catch(function (err) { console.warn("[gosx] region refresh", u, err); });
    }

    if (signalName && typeof window.__gosx_subscribe_shared_signal === "function") {
      window.__gosx_subscribe_shared_signal(
        signalName,
        function (v) {
          lastValue = v == null ? "" : String(v);
          refresh();
        },
        { immediate: false }
      );
    }
    if (onEvents.length) {
      document.addEventListener("gosx:hub:event", function (e) {
        var ev = e && e.detail && e.detail.event;
        if (ev && onEvents.indexOf(ev) >= 0) refresh();
      });
    }
  }

  function scan() {
    var nodes = document.querySelectorAll("[data-gosx-region]");
    for (var i = 0; i < nodes.length; i++) bindRegion(nodes[i]);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", scan);
  } else {
    scan();
  }
})();
