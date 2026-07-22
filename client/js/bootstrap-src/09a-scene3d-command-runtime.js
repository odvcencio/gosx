(function() {
  if (typeof window === "undefined" || window.__gosx_scene3d_command_bridge) return;
  var revision = 0;
  var selector = 'script[type="application/json"][data-gosx-scene-commands]';

  function key(target, options) {
    if (options && typeof options.engineID === "string" && options.engineID.trim()) return options.engineID.trim();
    if (typeof target === "string" && target.trim()) return target.trim();
    if (target && typeof target.id === "string" && target.id.trim()) return target.id.trim();
    return "";
  }

  function ready(handle) {
    return Boolean(handle && handle.__gosxScene3DCommandReady === true && typeof handle.applyCommands === "function");
  }

  function record(target, options) {
    if (ready(target)) return { handle: target, mount: null };
    if (target && ready(target.__gosxScene3DHandle)) return { handle: target.__gosxScene3DHandle, mount: target };
    var id = key(target, options || {});
    var mount = id && document && typeof document.getElementById === "function" ? document.getElementById(id) : null;
    if (mount && ready(mount.__gosxScene3DHandle)) return { handle: mount.__gosxScene3DHandle, mount: mount };
    var engine = id && window.__gosx && window.__gosx.engines && typeof window.__gosx.engines.get === "function" ? window.__gosx.engines.get(id) : null;
    return engine && ready(engine.handle) ? { handle: engine.handle, mount: engine.mount || mount || null } : null;
  }

  function setAttr(mount, name, value) {
    if (mount && typeof mount.setAttribute === "function") mount.setAttribute(name, String(value));
  }

  function apply(rec, commands, rev) {
    setAttr(rec.mount, "data-gosx-scene3d-command-revision", rev);
    return Promise.resolve(rec.handle.applyCommands(commands)).then(function() {
      setAttr(rec.mount, "data-gosx-scene3d-command-applied-revision", rev);
      return { revision: rev, applied: true };
    });
  }

  function dispatchCommands(target, commands, options) {
    if (!Array.isArray(commands)) return Promise.reject(new TypeError("Scene3D commands must be an array"));
    var opts = options || {};
    var id = key(target, opts);
    var deadline = Date.now() + Math.max(0, Math.floor(Number(opts.timeoutMS) || 10000));
    var rev = ++revision;
    function poll(resolve, reject) {
      var rec = record(target, opts);
      if (rec) return apply(rec, commands, rev).then(resolve, reject);
      if (!id) return reject(new Error("Scene3D command target is not ready and has no stable id"));
      if (Date.now() >= deadline) return reject(new Error("Scene3D command target did not become ready: " + id));
      setTimeout(function() { poll(resolve, reject); }, 16);
    }
    return new Promise(poll);
  }

  function applyCommandScripts(root) {
    if (!root || typeof root.querySelectorAll !== "function" || !window.__gosx || !window.__gosx.engines) return;
    var tags = root.querySelectorAll(selector);
    for (var i = 0; i < tags.length; i++) {
      var commands;
      try {
        commands = JSON.parse(tags[i].textContent || "[]");
      } catch (err) {
        console.warn("[gosx] scene command payload parse failed:", err);
        continue;
      }
      if (!Array.isArray(commands) || !commands.length) continue;
      window.__gosx.engines.forEach(function(rec) {
        if (rec && rec.component === "GoSXScene3D" && rec.handle && typeof rec.handle.applyCommands === "function") {
          rec.handle.applyCommands(commands);
        }
      });
    }
  }

  window.__gosx_scene3d_command_bridge = {
    dispatchCommands: dispatchCommands,
    applyCommandScripts: applyCommandScripts,
  };
})();
