package bridge

const bootstrapScript = `(function () {
  "use strict";

  var root = window;
  var existing = root.gosxDesktop;
  if (existing && existing.__gosxDesktopBridge === true) {
    return;
  }

  var pending = new Map();
  var listeners = new Map();
  var nextID = 1;

  function nextRequestID() {
    return "gosx-" + Date.now().toString(36) + "-" + (nextID++).toString(36);
  }

  function makeError(env) {
    var src = env && env.error ? env.error : {};
    var err = new Error(src.message || "desktop bridge error");
    err.name = "GosxDesktopError";
    err.code = src.code || "bridge.error";
    if (src.detail) {
      err.detail = src.detail;
    }
    err.envelope = env;
    return err;
  }

  function postEnvelope(env) {
    var raw = JSON.stringify(env);
    if (root.chrome && root.chrome.webview && typeof root.chrome.webview.postMessage === "function") {
      root.chrome.webview.postMessage(raw);
      return;
    }
    throw new Error("gosx desktop bridge transport is unavailable");
  }

  function send(op, method, payload, id) {
    if (typeof method !== "string" || method.length === 0) {
      throw new TypeError("gosxDesktop method must be a non-empty string");
    }
    var env = { op: op, method: method };
    if (id) {
      env.id = id;
    }
    if (payload !== undefined) {
      env.payload = payload;
    }
    postEnvelope(env);
  }

  function call(method, payload, options) {
    var id = nextRequestID();
    var onFrame = options && typeof options.onFrame === "function" ? options.onFrame : null;
    return new Promise(function (resolve, reject) {
      pending.set(id, { resolve: resolve, reject: reject, onFrame: onFrame });
      try {
        send("req", method, payload, id);
      } catch (err) {
        pending.delete(id);
        reject(err);
      }
    });
  }

  function emit(method, payload) {
    send("evt", method, payload);
  }

  function on(method, handler) {
    if (typeof method !== "string" || method.length === 0) {
      throw new TypeError("gosxDesktop event method must be a non-empty string");
    }
    if (typeof handler !== "function") {
      throw new TypeError("gosxDesktop event handler must be a function");
    }
    var set = listeners.get(method);
    if (!set) {
      set = new Set();
      listeners.set(method, set);
    }
    set.add(handler);
    return function unsubscribe() {
      set.delete(handler);
      if (set.size === 0) {
        listeners.delete(method);
      }
    };
  }

  function dispatchEvent(env) {
    if (env.method === "gosx.dev.reload") {
      root.location.reload();
      return;
    }
    var set = listeners.get(env.method);
    if (!set) {
      return;
    }
    Array.from(set).forEach(function (handler) {
      handler(env.payload, env);
    });
  }

  function handleEnvelope(env) {
    if (!env || typeof env.op !== "string") {
      return;
    }
    if (env.op === "evt") {
      dispatchEvent(env);
      return;
    }

    var id = env.id;
    var request = id ? pending.get(id) : null;
    if (!request) {
      return;
    }

    if (env.op === "res") {
      pending.delete(id);
      request.resolve(env.payload);
      return;
    }
    if (env.op === "err") {
      pending.delete(id);
      request.reject(makeError(env));
      return;
    }
    if (env.op === "frame") {
      if (request.onFrame) {
        try {
          request.onFrame(env.payload, env);
        } catch (err) {
          pending.delete(id);
          request.reject(err);
        }
      }
      return;
    }
    if (env.op === "end") {
      pending.delete(id);
      request.resolve(undefined);
    }
  }

  function receive(raw) {
    var env = typeof raw === "string" ? JSON.parse(raw) : raw;
    handleEnvelope(env);
  }

  function onWebViewMessage(event) {
    receive(event.data);
  }

  function asString(value) {
    return value == null ? "" : String(value);
  }

  function asSize(width, height) {
    return {
      width: Number(width) || 0,
      height: Number(height) || 0
    };
  }

  var appAPI = Object.freeze({
    info: function () {
      return call("gosx.desktop.app.info");
    },
    close: function () {
      return call("gosx.desktop.app.close");
    }
  });

  var windowAPI = Object.freeze({
    minimize: function () {
      return call("gosx.desktop.window.minimize");
    },
    maximize: function () {
      return call("gosx.desktop.window.maximize");
    },
    restore: function () {
      return call("gosx.desktop.window.restore");
    },
    focus: function () {
      return call("gosx.desktop.window.focus");
    },
    setTitle: function (title) {
      return call("gosx.desktop.window.setTitle", { title: asString(title) });
    },
    setFullscreen: function (enabled) {
      return call("gosx.desktop.window.setFullscreen", { enabled: !!enabled });
    },
    setMinSize: function (width, height) {
      return call("gosx.desktop.window.setMinSize", asSize(width, height));
    },
    setMaxSize: function (width, height) {
      return call("gosx.desktop.window.setMaxSize", asSize(width, height));
    }
  });

  var dialogAPI = Object.freeze({
    openFile: function (options) {
      return call("gosx.desktop.dialog.openFile", options || {});
    },
    saveFile: function (options) {
      return call("gosx.desktop.dialog.saveFile", options || {});
    }
  });

  var clipboardAPI = Object.freeze({
    readText: function () {
      return call("gosx.desktop.clipboard.readText");
    },
    writeText: function (text) {
      return call("gosx.desktop.clipboard.writeText", { text: asString(text) });
    }
  });

  var shellAPI = Object.freeze({
    openExternal: function (url) {
      return call("gosx.desktop.shell.openExternal", { url: asString(url) });
    }
  });

  var notificationAPI = Object.freeze({
    show: function (options) {
      return call("gosx.desktop.notification.show", options || {});
    }
  });

  var api = Object.freeze({
    __gosxDesktopBridge: true,
    call: call,
    emit: emit,
    on: on,
    app: appAPI,
    window: windowAPI,
    dialog: dialogAPI,
    clipboard: clipboardAPI,
    shell: shellAPI,
    notification: notificationAPI
  });

  Object.defineProperty(root, "gosxDesktop", {
    configurable: false,
    enumerable: true,
    value: api,
    writable: false
  });

  if (root.chrome && root.chrome.webview && typeof root.chrome.webview.addEventListener === "function") {
    root.chrome.webview.addEventListener("message", onWebViewMessage);
  }
}());`

// BootstrapScript returns the JavaScript page bootstrap for the typed
// desktop IPC bridge. Inject it once during page initialization; the
// script is idempotence-guarded and installs window.gosxDesktop.
func BootstrapScript() string {
	return bootstrapScript
}
