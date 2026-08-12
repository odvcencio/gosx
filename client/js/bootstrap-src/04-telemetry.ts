  // Client-event telemetry: ships structured events to the gosx server at
  // /_gosx/client-events. Transport failures never enter application control
  // flow, but remain visible through window.__gosx.telemetry.snapshot().

  const GOSX_TELEMETRY_ENDPOINT = "/_gosx/client-events";
  const GOSX_TELEMETRY_FLUSH_MS_DEFAULT = 2000;
  const GOSX_TELEMETRY_BATCH_MAX_DEFAULT = 20;
  const GOSX_TELEMETRY_QUEUE_MAX_DEFAULT = 200;
  const GOSX_TELEMETRY_LEVELS = { debug: 1, info: 1, warn: 1, error: 1 };

  function gosxTelemetryPositiveInteger(value, fallback) {
    const parsed = Number(value);
    return Number.isFinite(parsed) && parsed > 0 ? Math.max(1, Math.floor(parsed)) : fallback;
  }

  function gosxTelemetryConfig() {
    const cfg = (typeof window !== "undefined" && window.__gosx_telemetry_config) || {};
    const rawFlushInterval = Number(cfg.flushInterval);
    return {
      endpoint: typeof cfg.endpoint === "string" && cfg.endpoint ? cfg.endpoint : GOSX_TELEMETRY_ENDPOINT,
      flushInterval: Math.max(0, Number.isFinite(rawFlushInterval) ? rawFlushInterval : GOSX_TELEMETRY_FLUSH_MS_DEFAULT),
      maxBatch: gosxTelemetryPositiveInteger(cfg.maxBatch, GOSX_TELEMETRY_BATCH_MAX_DEFAULT),
      maxQueue: gosxTelemetryPositiveInteger(cfg.maxQueue, GOSX_TELEMETRY_QUEUE_MAX_DEFAULT),
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
      return window.location && window.location.pathname ? String(window.location.pathname) : "";
    } catch (_err) {
      return "";
    }
  }

  function gosxTelemetryUserAgent() {
    try {
      return window.navigator && typeof window.navigator.userAgent === "string"
        ? String(window.navigator.userAgent)
        : "";
    } catch (_err) {
      return "";
    }
  }

  function gosxInstallTelemetry() {
    if (typeof window === "undefined" || window.__gosx_telemetry_installed) return;
    window.__gosx_telemetry_installed = true;

    const cfg = gosxTelemetryConfig();
    const sid = cfg.enabled ? gosxTelemetrySessionID() : "";
    const queue = [];
    const snapshotFields = (
      "enabled,session,queueDepth,queueCapacity,batchCapacity,emittedEvents," +
      "attemptedEvents,attemptedBatches,dispatchedEvents,dispatchedBatches," +
      "browserAcceptedEvents,browserAcceptedBatches,serverAcceptedEvents,serverAcceptedBatches," +
      "droppedOverflowEvents,droppedSerializationEvents,failedEvents,failedBatches," +
      "beaconFailures,fetchFailures,pendingRequests,lastFlushAt,lastFlushReason,lastFailureAt,lastFailureReason"
    ).split(",");
    const telemetryState = [
      cfg.enabled, sid, 0, cfg.maxQueue, cfg.maxBatch,
      0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, "", 0, "",
    ];
    const T_QUEUE_DEPTH = 2;
    const T_EMITTED = 5;
    const T_ATTEMPTED = 6;
    const T_DISPATCHED = 8;
    const T_BROWSER_ACCEPTED = 10;
    const T_SERVER_ACCEPTED = 12;
    const T_DROPPED_OVERFLOW = 14;
    const T_DROPPED_SERIALIZATION = 15;
    const T_FAILED = 16;
    const T_BEACON_FAILURES = 18;
    const T_FETCH_FAILURES = 19;
    const T_PENDING = 20;
    const T_LAST_FLUSH_AT = 21;
    const T_LAST_FLUSH_REASON = 22;
    const T_LAST_FAILURE_AT = 23;
    const T_LAST_FAILURE_REASON = 24;

    function snapshot() {
      telemetryState[T_QUEUE_DEPTH] = queue.length;
      const result = {};
      for (let i = 0; i < snapshotFields.length; i += 1) result[snapshotFields[i]] = telemetryState[i];
      return Object.freeze(result);
    }

    window.__gosx_telemetry_snapshot = snapshot;
    window.__gosx_telemetry_session = function () { return sid; };

    if (!cfg.enabled) {
      window.__gosx_emit = function () {};
      window.__gosx_telemetry_flush = function () {};
      return;
    }

    let flushTimer = null;
    let uaSent = false;

    function addPair(eventIndex, eventCount) {
      telemetryState[eventIndex] += eventCount;
      telemetryState[eventIndex + 1] += 1;
    }

    function recordFailure(counterIndex, reason, eventCount, finalFailure) {
      if (counterIndex >= 0) telemetryState[counterIndex] += 1;
      if (finalFailure) addPair(T_FAILED, eventCount);
      telemetryState[T_LAST_FAILURE_AT] = Date.now();
      telemetryState[T_LAST_FAILURE_REASON] = reason;
    }

    function scheduleFlush() {
      if (flushTimer != null || queue.length === 0) return;
      flushTimer = setTimeout(function () {
        flushTimer = null;
        flush(false, "timer", false);
      }, cfg.flushInterval);
    }

    function clearFlushTimer() {
      if (flushTimer == null) return;
      clearTimeout(flushTimer);
      flushTimer = null;
    }

    function emit(level, category, message, fields) {
      telemetryState[T_EMITTED] += 1;
      if (queue.length >= cfg.maxQueue) {
        telemetryState[T_DROPPED_OVERFLOW] += 1;
        telemetryState[T_LAST_FAILURE_AT] = Date.now();
        telemetryState[T_LAST_FAILURE_REASON] = "queue-overflow";
        return;
      }
      try {
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
        if (fields && typeof fields === "object") event.fields = fields;
        queue.push(event);
        scheduleFlush();
      } catch (_err) {
        telemetryState[T_DROPPED_SERIALIZATION] += 1;
        telemetryState[T_FAILED] += 1;
        telemetryState[T_LAST_FAILURE_AT] = Date.now();
        telemetryState[T_LAST_FAILURE_REASON] = "event-normalization-error";
      }
    }

    function settleFetch(response, eventCount) {
      try {
        const status = Number(response && response.status);
        if (response && (response.ok === true || (response.ok !== false && status >= 200 && status < 300))) {
          addPair(T_SERVER_ACCEPTED, eventCount);
          return;
        }
        recordFailure(
          T_FETCH_FAILURES,
          Number.isFinite(status) && status > 0 ? "fetch-status-" + status : "fetch-response-unaccepted",
          eventCount,
          true,
        );
      } catch (_err) {
        recordFailure(T_FETCH_FAILURES, "fetch-response-error", eventCount, true);
      }
    }

    function dispatchFetch(body, eventCount) {
      let request;
      let usesCoreRequest = false;
      try {
        usesCoreRequest = Boolean(window.__gosx && typeof window.__gosx.request === "function");
        request = usesCoreRequest
          ? window.__gosx.request
          : (typeof window.fetch === "function" ? window.fetch.bind(window) : null);
      } catch (_err) {
        recordFailure(T_FETCH_FAILURES, "fetch-access-error", eventCount, true);
        return;
      }
      if (!request) {
        recordFailure(T_FETCH_FAILURES, "fetch-unavailable", eventCount, true);
        return;
      }

      const options = {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: body,
        keepalive: true,
        credentials: "omit",
      };
      if (usesCoreRequest) options.csrf = false;

      let result;
      try {
        result = request(cfg.endpoint, options);
        addPair(T_DISPATCHED, eventCount);
      } catch (_err) {
        recordFailure(T_FETCH_FAILURES, "fetch-exception", eventCount, true);
        return;
      }

      try {
        if (!result || typeof result.then !== "function") {
          settleFetch(result, eventCount);
          return;
        }
        telemetryState[T_PENDING] += 1;
        Promise.resolve(result).then(function (response) {
          telemetryState[T_PENDING] -= 1;
          settleFetch(response, eventCount);
        }, function () {
          telemetryState[T_PENDING] -= 1;
          recordFailure(T_FETCH_FAILURES, "fetch-rejected", eventCount, true);
        });
      } catch (_err) {
        recordFailure(T_FETCH_FAILURES, "fetch-promise-error", eventCount, true);
      }
    }

    function flush(preferBeacon, reason, drain) {
      clearFlushTimer();
      telemetryState[T_LAST_FLUSH_AT] = Date.now();
      telemetryState[T_LAST_FLUSH_REASON] = reason;
      const limit = drain ? Math.ceil(cfg.maxQueue / cfg.maxBatch) : 1;
      for (let batchIndex = 0; queue.length > 0 && batchIndex < limit; batchIndex += 1) {
        const batch = queue.splice(0, cfg.maxBatch);
        const eventCount = batch.length;
        addPair(T_ATTEMPTED, eventCount);
        let body;
        try {
          body = JSON.stringify({ sid: sid, sent_at: Date.now(), events: batch });
        } catch (_err) {
          telemetryState[T_DROPPED_SERIALIZATION] += eventCount;
          recordFailure(-1, "serialization-error", eventCount, true);
          continue;
        }

        let sent = false;
        if (preferBeacon) {
          try {
            const nav = window.navigator;
            if (nav && typeof nav.sendBeacon === "function") {
              if (nav.sendBeacon(cfg.endpoint, body)) {
                addPair(T_DISPATCHED, eventCount);
                addPair(T_BROWSER_ACCEPTED, eventCount);
                sent = true;
              } else {
                recordFailure(T_BEACON_FAILURES, "beacon-rejected", eventCount, false);
              }
            }
          } catch (_err) {
            recordFailure(T_BEACON_FAILURES, "beacon-exception", eventCount, false);
          }
        }
        if (!sent) dispatchFetch(body, eventCount);
      }
      if (queue.length > 0) scheduleFlush();
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
        emit(
          "error",
          "runtime",
          String((reason && reason.message) || (typeof reason === "string" ? reason : "unhandledrejection")),
          { stack: (reason && reason.stack) || "" },
        );
      });
      window.addEventListener("pagehide", function () {
        flush(true, "pagehide", true);
      });
    } catch (_err) {
      /* older environments may not support window events */
    }

    try {
      if (typeof document !== "undefined" && typeof document.addEventListener === "function") {
        document.addEventListener("visibilitychange", function () {
          if (document.visibilityState === "hidden") flush(true, "visibility-hidden", true);
        });
      }
    } catch (_err) {
      /* skip */
    }

    window.__gosx_emit = emit;
    window.__gosx_telemetry_flush = function (options) {
      const beacon = Boolean(options && options.beacon);
      flush(beacon, beacon ? "manual-beacon" : (options && options.drain ? "manual-drain" : "manual"), beacon || Boolean(options && options.drain));
    };
  }

  function gosxPublishTelemetryAPI() {
    if (typeof window === "undefined") return;
    window.__gosx = window.__gosx || {};
    const telemetry = window.__gosx.telemetry && typeof window.__gosx.telemetry === "object"
      ? window.__gosx.telemetry
      : {};
    telemetry.emit = function (level, category, message, fields) {
      if (typeof window.__gosx_emit !== "function") return undefined;
      return window.__gosx_emit(level, category, message, fields);
    };
    telemetry.flush = function (options) {
      if (typeof window.__gosx_telemetry_flush !== "function") return undefined;
      return window.__gosx_telemetry_flush(options);
    };
    telemetry.session = typeof window.__gosx_telemetry_session === "function"
      ? window.__gosx_telemetry_session
      : function () { return ""; };
    telemetry.snapshot = typeof window.__gosx_telemetry_snapshot === "function"
      ? window.__gosx_telemetry_snapshot
      : function () { return Object.freeze({ enabled: false }); };
    telemetry.enabled = !((window.__gosx_telemetry_config || {}).enabled === false);
    window.__gosx.telemetry = telemetry;
  }

  gosxInstallTelemetry();
  gosxPublishTelemetryAPI();
