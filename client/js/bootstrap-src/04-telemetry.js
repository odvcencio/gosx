  // Client-event telemetry: ships structured events to the gosx server at
  // /_gosx/client-events. Installed eagerly inside the bootstrap IIFE so
  // downstream modules (scene3d, islands, engines) can emit before the
  // first render. Same-origin-only, kill-switched by the server via
  // GOSX_TELEMETRY=off (the POST endpoint returns 404, and queued events
  // are dropped silently on error).

  const GOSX_TELEMETRY_ENDPOINT = "/_gosx/client-events";
  const GOSX_TELEMETRY_FLUSH_MS_DEFAULT = 2000;
  const GOSX_TELEMETRY_BATCH_MAX_DEFAULT = 20;
  const GOSX_TELEMETRY_QUEUE_MAX_DEFAULT = 200;
  const GOSX_TELEMETRY_LEVELS = { debug: 1, info: 1, warn: 1, error: 1 };

  function gosxTelemetryConfig() {
    const cfg = (typeof window !== "undefined" && window.__gosx_telemetry_config) || {};
    return {
      endpoint: typeof cfg.endpoint === "string" && cfg.endpoint ? cfg.endpoint : GOSX_TELEMETRY_ENDPOINT,
      flushInterval: Math.max(0, Number(cfg.flushInterval) || GOSX_TELEMETRY_FLUSH_MS_DEFAULT),
      maxBatch: Math.max(1, Number(cfg.maxBatch) || GOSX_TELEMETRY_BATCH_MAX_DEFAULT),
      maxQueue: Math.max(1, Number(cfg.maxQueue) || GOSX_TELEMETRY_QUEUE_MAX_DEFAULT),
      enabled: cfg.enabled !== false,
    };
  }

  function gosxTelemetrySessionID() {
    const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789";
    let out = "s_";
    for (let i = 0; i < 10; i += 1) {
      out += alphabet[Math.floor(Math.random() * alphabet.length)];
    }
    return out;
  }

  function gosxTelemetryNormalizeLevel(level) {
    const key = String(level || "").toLowerCase();
    return GOSX_TELEMETRY_LEVELS[key] ? key : "info";
  }

  function gosxTelemetryCurrentURL() {
    try {
      if (typeof window !== "undefined" && window.location && window.location.pathname) {
        return String(window.location.pathname);
      }
    } catch (_err) {
      /* fall through */
    }
    return "";
  }

  function gosxTelemetryUserAgent() {
    try {
      if (typeof window !== "undefined" && window.navigator && typeof window.navigator.userAgent === "string") {
        return String(window.navigator.userAgent);
      }
    } catch (_err) {
      /* fall through */
    }
    return "";
  }

  function gosxInstallTelemetry() {
    if (typeof window === "undefined") {
      return;
    }
    if (window.__gosx_telemetry_installed) {
      return;
    }
    window.__gosx_telemetry_installed = true;

    const cfg = gosxTelemetryConfig();

    if (!cfg.enabled) {
      window.__gosx_emit = function () {};
      window.__gosx_telemetry_flush = function () {};
      return;
    }

    const sid = gosxTelemetrySessionID();
    const queue = [];
    let flushTimer = null;
    let uaSent = false;

    function emit(level, category, message, fields) {
      if (queue.length >= cfg.maxQueue) {
        return;
      }
      const event = {
        ts: Date.now(),
        lvl: gosxTelemetryNormalizeLevel(level),
        cat: typeof category === "string" && category ? category : "unknown",
        msg: typeof message === "string" ? message : String(message == null ? "" : message),
        url: gosxTelemetryCurrentURL(),
      };
      if (!uaSent) {
        event.ua = gosxTelemetryUserAgent();
        uaSent = true;
      }
      if (fields && typeof fields === "object") {
        event.fields = fields;
      }
      queue.push(event);
      scheduleFlush();
    }

    function scheduleFlush() {
      if (flushTimer != null || queue.length === 0) {
        return;
      }
      const delay = Math.max(0, cfg.flushInterval);
      flushTimer = setTimeout(function () {
        flushTimer = null;
        flushBatch(false);
      }, delay);
    }

    function buildPayload(batch) {
      return JSON.stringify({
        sid: sid,
        sent_at: Date.now(),
        events: batch,
      });
    }

    function flushBatch(preferBeacon) {
      if (queue.length === 0) {
        return;
      }
      const batch = queue.splice(0, cfg.maxBatch);
      const body = buildPayload(batch);

      if (preferBeacon) {
        try {
          const nav = window.navigator;
          if (nav && typeof nav.sendBeacon === "function") {
            if (nav.sendBeacon(cfg.endpoint, body)) {
              return;
            }
          }
        } catch (_err) {
          /* fall through to fetch */
        }
      }

      try {
        if (typeof window.fetch === "function") {
          const result = window.fetch(cfg.endpoint, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: body,
            keepalive: true,
            credentials: "omit",
          });
          if (result && typeof result.catch === "function") {
            result.catch(function () { /* swallow — telemetry must never surface to users */ });
          }
        }
      } catch (_err) {
        /* swallow */
      }

      if (queue.length > 0) {
        scheduleFlush();
      }
    }

    try {
      window.addEventListener("error", function (event) {
        emit("error", "runtime", (event && event.message) || "uncaught error", {
          filename: (event && event.filename) || "",
          lineno: (event && event.lineno) || 0,
          colno: (event && event.colno) || 0,
          stack: (event && event.error && event.error.stack) || "",
        });
      });
      window.addEventListener("unhandledrejection", function (event) {
        const reason = event && event.reason;
        const message = (reason && reason.message) || (typeof reason === "string" ? reason : "unhandledrejection");
        emit("error", "runtime", String(message), {
          stack: (reason && reason.stack) || "",
        });
      });
    } catch (_err) {
      /* older environments may not support addEventListener on window — skip */
    }

    try {
      if (typeof document !== "undefined" && typeof document.addEventListener === "function") {
        document.addEventListener("visibilitychange", function () {
          if (document.visibilityState === "hidden") {
            flushBatch(true);
          }
        });
      }
    } catch (_err) {
      /* skip */
    }

    window.__gosx_emit = emit;
    window.__gosx_telemetry_flush = function (opts) {
      flushBatch(Boolean(opts && opts.beacon));
    };
    window.__gosx_telemetry_session = function () {
      return sid;
    };
  }

  gosxInstallTelemetry();
