package ouroboros

const ouroborosProbeJS = `
(function(){
  "use strict";
  if (window.__gosxOuroborosProbe) return;
  var limit = 8192;
  function safeURL(value) {
    value = String(value || "");
    var cut = value.length;
    var q = value.indexOf("?");
    var h = value.indexOf("#");
    if (q >= 0 && q < cut) cut = q;
    if (h >= 0 && h < cut) cut = h;
    return value.slice(0, cut);
  }
  var probe = window.__gosxOuroborosProbe = {
    version: "1",
    phase: "cold-load",
    events: [],
    setPhase: function(phase) {
      probe.phase = String(phase || "unknown");
      probe.mark("phase", {phase: probe.phase});
    },
    record: function(kind, name, detail) {
      var entry = {
        kind: String(kind || "mark"),
        phase: String(probe.phase || "unknown"),
        name: String(name || ""),
        startTime: performance.now ? performance.now() : 0,
        detail: sanitize(detail)
      };
      probe.events.push(entry);
      if (probe.events.length > limit) {
        probe.events.splice(0, Math.floor(limit / 2));
      }
      return entry;
    },
    mark: function(name, detail) {
      return probe.record("mark", name, detail);
    }
  };
  function sanitize(value) {
    if (value == null) return null;
    try {
      return JSON.parse(JSON.stringify(value));
    } catch (_) {
      return {unserializable: true, type: typeof value};
    }
  }
  probe.mark("preload-installed", {url: safeURL(location.href)});
  window.addEventListener("DOMContentLoaded", function(){
    probe.setPhase("route-load");
    probe.record("navigation", "DOMContentLoaded", {url: safeURL(location.href)});
  }, {once:true});
  window.addEventListener("load", function(){
    probe.setPhase("route-load");
    probe.record("navigation", "load", {url: safeURL(location.href)});
  }, {once:true});
  document.addEventListener("gosx:ready", function(event){
    probe.setPhase("route-load");
    probe.record("ready", "gosx:ready", event && event.detail || null);
  }, {once:true});
  window.addEventListener("error", function(event){
    probe.record("error", "window.error", {
      message: event.message || "",
      filename: event.filename || "",
      lineno: event.lineno || 0,
      colno: event.colno || 0
    });
  });
  window.addEventListener("unhandledrejection", function(event){
    probe.record("error", "unhandledrejection", {
      reason: String(event.reason && event.reason.message || event.reason || "")
    });
  });
  try {
    var observer = new PerformanceObserver(function(list) {
      var entries = list.getEntries();
      for (var i = 0; i < entries.length; i++) {
        probe.record("longtask", entries[i].name || "longtask", {
          duration: entries[i].duration,
          startTime: entries[i].startTime
        });
      }
    });
    observer.observe({type: "longtask", buffered: true});
  } catch (_) {}
})();`
