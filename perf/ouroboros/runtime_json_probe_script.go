package ouroboros

const runtimeJSONProbeJS = `
(function(){
  "use strict";
  var knownNames = __GOSX_KNOWN_NAMES__;
  var g = window;
  var nativeJSONParse = JSON.parse;
  var nativeJSONStringify = JSON.stringify;
  var nativeDefineProperty = Object.defineProperty;
  var nativeGetOwnPropertyDescriptor = Object.getOwnPropertyDescriptor;
  var nativeGetOwnPropertyNames = Object.getOwnPropertyNames;
  var nativeNow = performance && typeof performance.now === "function" ? function(){ return performance.now(); } : function(){ return Date.now(); };
  var validPhases = {
    "cold-load": true, "route-load": true, input: true, dispatch: true,
    reconciliation: true, patch: true, frame: true, network: true,
    debug: true, telemetry: true, unknown: true
  };
  var limit = 8192;
  var inProbe = 0;
  var wrappers = Object.create(null);
  var originals = Object.create(null);
  var trappedGlobals = Object.create(null);
  var probeWrapped = typeof WeakSet === "function" ? new WeakSet() : null;
  var unwrapped = [];
  var dropped = 0;
  var safeDetails = typeof WeakSet === "function" ? new WeakSet() : null;

  function safeString(value) {
    try { return String(value); } catch (_) { return "[unstringable]"; }
  }
  function byteEstimate(value, depth) {
    if (value == null) return 0;
    var t = typeof value;
    if (t === "string") {
      if (typeof TextEncoder === "function") {
        try { return new TextEncoder().encode(value).length; } catch (_) {}
      }
      return value.length;
    }
    if (t === "number") return 8;
    if (t === "boolean") return 4;
    if (t === "bigint") return safeString(value).length;
    if (t === "undefined" || t === "symbol" || t === "function") return 0;
    try {
      if (typeof ArrayBuffer !== "undefined" && value instanceof ArrayBuffer) return value.byteLength || 0;
    } catch (_) {}
    try {
      if (typeof ArrayBuffer !== "undefined" && ArrayBuffer.isView && ArrayBuffer.isView(value)) return value.byteLength || 0;
    } catch (_) {}
    try {
      if (Array.isArray(value)) return value.length || 0;
    } catch (_) {}
    try {
      if (typeof Map !== "undefined" && value instanceof Map) return value.size || 0;
    } catch (_) {}
    try {
      if (typeof Set !== "undefined" && value instanceof Set) return value.size || 0;
    } catch (_) {}
    return 0;
  }
  function typeOf(value) {
    if (value === null) return "null";
    try { if (Array.isArray(value)) return "array"; } catch (_) { return "object"; }
    try { if (typeof ArrayBuffer !== "undefined" && value instanceof ArrayBuffer) return "arraybuffer"; } catch (_) { return "object"; }
    try { if (typeof ArrayBuffer !== "undefined" && ArrayBuffer.isView && ArrayBuffer.isView(value)) return "typedarray"; } catch (_) { return "object"; }
    return typeof value;
  }
  function hashText(text) {
    text = safeString(text || "");
    var h = 2166136261;
    for (var i = 0; i < text.length; i++) {
      h ^= text.charCodeAt(i);
      h += (h << 1) + (h << 4) + (h << 7) + (h << 8) + (h << 24);
    }
    return ("00000000" + (h >>> 0).toString(16)).slice(-8);
  }
  function stackDetail() {
    var stack = "";
    try { throw new Error(); } catch (e) { stack = safeString(e && e.stack || ""); }
    var lines = stack.split("\n");
    var source = {urlHash:"", path:"", line:0, column:0};
    for (var i = 1; i < lines.length; i++) {
      var line = lines[i] || "";
      if (isProbeStackFrame(line)) continue;
      var candidate = sanitizeStackLine(line);
      if (candidate.path) {
        source = candidate;
        break;
      }
    }
    return { stackHash: hashText(canonicalStackSource(source)), source: source };
  }
  function isProbeStackFrame(line) {
    line = safeString(line || "");
    if (line.indexOf("__gosxOuroborosProbe") >= 0 || line.indexOf("runtimeJSON") >= 0) return true;
    return /\b(stackDetail|sanitizeStackLine|canonicalStackSource|recordJSON|wrapJSON|wrapRuntimeFunction|safeDetail|internalDetail|baseProbe|probe\.record)\b/.test(line);
  }
  function sanitizeStackLine(line) {
    line = safeString(line || "");
    var url = "";
    var lineNo = 0;
    var columnNo = 0;
    var m = line.match(/(https?:\/\/[^\s)]+|file:\/\/[^\s)]+):(\d+):(\d+)\)?\s*$/);
    if (m) {
      url = stripQuery(m[1]);
      lineNo = Number(m[2]) || 0;
      columnNo = Number(m[3]) || 0;
    }
    var path = url ? pathOnly(url).slice(0, 160) : "";
    return {
      urlHash: path ? hashText(path) : "",
      path: path,
      line: lineNo,
      column: columnNo
    };
  }
  function stripQuery(url) {
    var cut = url.length;
    var q = url.indexOf("?");
    var h = url.indexOf("#");
    if (q >= 0 && q < cut) cut = q;
    if (h >= 0 && h < cut) cut = h;
    return url.slice(0, cut);
  }
  function pathOnly(url) {
    try {
      var u = new URL(url);
      return u.pathname || "";
    } catch (_) {}
    return "";
  }
  function canonicalStackSource(source) {
    source = source || {};
    if (!source.path) return "unknown";
    return [source.path, Number(source.line) || 0, Number(source.column) || 0].join(":");
  }
  function cleanName(value, max) {
    value = safeString(value || "");
    return value.replace(/[^\w:.$/-]/g, "").slice(0, max || 120);
  }
  function finiteIntegerInRange(value, min, max) {
    if (typeof value !== "number") return false;
    var finite = typeof Number.isFinite === "function" ? Number.isFinite(value) : isFinite(value);
    return finite && Math.floor(value) === value && value >= min && value <= max;
  }
  function internalDetail(detail) {
    if (detail && typeof detail === "object" && safeDetails) safeDetails.add(detail);
    return detail;
  }
  function safeDetail(kind, detail, routeID) {
    if (!detail || typeof detail !== "object" || (safeDetails && !safeDetails.has(detail))) detail = {};
    var out = {};
    if (routeID) out.routeID = cleanName(routeID, 32);
    if (kind === "json-call") {
      out.operation = cleanName(detail.operation, 32);
      out.payloadBytes = Number(detail.payloadBytes) || 0;
      out.resultBytes = Number(detail.resultBytes) || 0;
      out.exception = cleanName(detail.exception, 64);
      out.stackHash = cleanName(detail.stackHash, 32);
      out.source = sanitizeSource(detail.source);
    } else if (kind === "runtime-call") {
      out.argCount = Number(detail.argCount) || 0;
      out.argTypes = Array.isArray(detail.argTypes) ? detail.argTypes.slice(0, 16).map(function(v){return cleanName(v, 32);}) : [];
      out.argBytes = Array.isArray(detail.argBytes) ? detail.argBytes.slice(0, 16).map(function(v){return Number(v)||0;}) : [];
      out.resultType = cleanName(detail.resultType, 32);
      out.resultBytes = Number(detail.resultBytes) || 0;
      out.exception = cleanName(detail.exception, 64);
      out.async = !!detail.async;
      out.stackHash = cleanName(detail.stackHash, 32);
      out.source = sanitizeSource(detail.source);
      if (finiteIntegerInRange(detail.eventKind, 1, 5)) {
        out.eventKind = detail.eventKind;
      }
    } else if (kind === "probe") {
      out.knownGlobalCount = Number(detail.knownGlobalCount) || 0;
      out.wrappedCount = Number(detail.wrappedCount) || 0;
      out.unwrappedCount = Number(detail.unwrappedCount) || 0;
      out.nameHash = detail.nameHash ? cleanName(detail.nameHash, 32) : "";
      out.phase = cleanName(detail.phase, 32);
    }
    return out;
  }
  function sanitizeSource(source) {
    source = source || {};
    return {
      urlHash: cleanName(source.urlHash, 32),
      path: cleanName(source.path, 160),
      line: Number(source.line) || 0,
      column: Number(source.column) || 0
    };
  }
  function baseProbe() {
    var existing = g.__gosxOuroborosProbe;
    if (existing && existing.schemaVersion === 1 && existing.runtimeJSONProbe === true) return existing;
    var probe = existing || {};
    if (!probe.events) probe.events = [];
    probe.schemaVersion = 1;
    probe.version = "1";
    probe.runtimeJSONProbe = true;
    probe.phase = validPhases[probe.phase] ? probe.phase : "unknown";
    probe.routeID = "";
    probe.droppedCount = probe.droppedCount || 0;
    probe.wrappedGlobals = probe.wrappedGlobals || [];
    probe.unwrappedGlobals = probe.unwrappedGlobals || [];
    probe.setPhase = function(phase) {
      phase = safeString(phase || "unknown").replace(/\s+/g, "-").toLowerCase();
      probe.phase = validPhases[phase] ? phase : "unknown";
      probe.record("probe", "phase", internalDetail({phase: probe.phase}));
    };
    probe.setRoute = function(routeID) { probe.routeID = cleanName(routeID, 32); probe.record("probe", "route", internalDetail({})); };
    probe.refresh = function(){ scanGlobals(); probe.record("probe", "refresh", internalDetail({wrappedCount: probe.wrappedGlobals.length, unwrappedCount: probe.unwrappedGlobals.length})); };
    probe.record = function(kind, name, detail) {
      if (inProbe) return null;
      inProbe++;
      try {
        kind = safeString(kind || "probe");
        var entry = {
          kind: kind,
          phase: validPhases[probe.phase] ? probe.phase : "unknown",
          name: cleanName(name || "", 160),
          startTime: nativeNow(),
          detail: safeDetail(kind, detail, probe.routeID)
        };
        probe.events.push(entry);
        if (probe.events.length > limit) {
          var remove = Math.max(1, Math.floor(limit / 2));
          probe.events.splice(0, remove);
          dropped += remove;
          probe.droppedCount = (probe.droppedCount || 0) + remove;
        }
        return entry;
      } finally {
        inProbe--;
      }
    };
    probe.mark = function(name, detail) { return probe.record("mark", name, detail); };
    probe.snapshot = function() { scanGlobals(); return drain(false); };
    probe.drain = function() { scanGlobals(); return drain(true); };
    nativeDefineProperty(g, "__gosxOuroborosProbe", {value: probe, writable: true, configurable: true});
    return probe;
  }
  var probe = baseProbe();
  var installState = probe.__gosxOuroborosRuntimeJSONState;
  if (!installState || installState.schemaVersion !== 1) {
    installState = {schemaVersion: 1};
    try { nativeDefineProperty(probe, "__gosxOuroborosRuntimeJSONState", {value: installState, configurable: true}); } catch (_) { probe.__gosxOuroborosRuntimeJSONState = installState; }
  }
  if (installState.nativeJSONParse) nativeJSONParse = installState.nativeJSONParse; else installState.nativeJSONParse = nativeJSONParse;
  if (installState.nativeJSONStringify) nativeJSONStringify = installState.nativeJSONStringify; else installState.nativeJSONStringify = nativeJSONStringify;
  if (installState.nativeDefineProperty) nativeDefineProperty = installState.nativeDefineProperty; else installState.nativeDefineProperty = nativeDefineProperty;
  if (installState.nativeGetOwnPropertyDescriptor) nativeGetOwnPropertyDescriptor = installState.nativeGetOwnPropertyDescriptor; else installState.nativeGetOwnPropertyDescriptor = nativeGetOwnPropertyDescriptor;
  if (installState.nativeGetOwnPropertyNames) nativeGetOwnPropertyNames = installState.nativeGetOwnPropertyNames; else installState.nativeGetOwnPropertyNames = nativeGetOwnPropertyNames;
  wrappers = installState.wrappers || (installState.wrappers = wrappers);
  originals = installState.originals || (installState.originals = originals);
  trappedGlobals = installState.trappedGlobals || (installState.trappedGlobals = trappedGlobals);
  probeWrapped = installState.probeWrapped || (installState.probeWrapped = probeWrapped);
  safeDetails = installState.safeDetails || (installState.safeDetails = safeDetails);
  unwrapped = installState.unwrapped || (installState.unwrapped = unwrapped);

  function drain(clear) {
    var events = probe.events.slice();
    var out = {
      schemaVersion: "gosx.ouroboros.runtime-json-probe.v1",
      facadeSchemaVersion: 1,
      version: "1",
      phase: probe.phase || "unknown",
      routeID: probe.routeID || "",
      events: events,
      droppedCount: probe.droppedCount || dropped,
      wrappedGlobals: probe.wrappedGlobals.slice(),
      unwrappedGlobals: probe.unwrappedGlobals.slice(),
      knownGlobals: knownNames.slice(),
      limits: {eventLimit: limit}
    };
    if (clear) {
      probe.events.splice(0, probe.events.length);
      probe.droppedCount = 0;
      dropped = 0;
    }
    return out;
  }
  function recordJSON(op, payload, result, exception) {
    var st = stackDetail();
    probe.record("json-call", op, internalDetail({
      operation: op,
      payloadBytes: byteEstimate(payload),
      resultBytes: exception ? 0 : byteEstimate(result),
      exception: exception ? safeString(exception && exception.name || "Error") : "",
      stackHash: st.stackHash,
      source: st.source
    }));
  }
  function wrapJSON() {
    if (JSON.parse === installState.jsonParseWrapper && JSON.stringify === installState.jsonStringifyWrapper) return;
    var parseDesc = nativeGetOwnPropertyDescriptor(JSON, "parse");
    var stringifyDesc = nativeGetOwnPropertyDescriptor(JSON, "stringify");
    var parseWrapper = installState.jsonParseWrapper;
    if (!parseWrapper || JSON.parse !== parseWrapper) parseWrapper = function(text, reviver) {
      var result, err;
      try {
        result = nativeJSONParse.apply(this, arguments);
        return result;
      } catch (e) {
        err = e;
        throw e;
      } finally {
        if (!inProbe) recordJSON("JSON.parse", text, result, err);
      }
    };
    var stringifyWrapper = installState.jsonStringifyWrapper;
    if (!stringifyWrapper || JSON.stringify !== stringifyWrapper) stringifyWrapper = function(value, replacer, space) {
      var result, err;
      try {
        result = nativeJSONStringify.apply(this, arguments);
        return result;
      } catch (e) {
        err = e;
        throw e;
      } finally {
        if (!inProbe) recordJSON("JSON.stringify", value, result, err);
      }
    };
    copyFunctionShape(parseWrapper, nativeJSONParse);
    copyFunctionShape(stringifyWrapper, nativeJSONStringify);
    installState.jsonParseWrapper = parseWrapper;
    installState.jsonStringifyWrapper = stringifyWrapper;
    nativeDefineProperty(JSON, "parse", mergeValueDescriptor(parseDesc, parseWrapper));
    nativeDefineProperty(JSON, "stringify", mergeValueDescriptor(stringifyDesc, stringifyWrapper));
  }
  function copyFunctionShape(to, from) {
    try { nativeDefineProperty(to, "name", {value: from.name, configurable: true}); } catch (_) {}
    try { nativeDefineProperty(to, "length", {value: from.length, configurable: true}); } catch (_) {}
    try {
      var props = nativeGetOwnPropertyNames(from);
      for (var i = 0; i < props.length; i++) {
        if (props[i] === "name" || props[i] === "length" || props[i] === "prototype") continue;
        try { nativeDefineProperty(to, props[i], nativeGetOwnPropertyDescriptor(from, props[i])); } catch (_) {}
      }
    } catch (_) {}
    try { nativeDefineProperty(to, "toString", {value:function(){ return Function.prototype.toString.call(from); }, configurable:true}); } catch (_) {}
  }
  function mergeValueDescriptor(desc, value) {
    desc = desc || {writable:true, enumerable:false, configurable:true};
    return {value:value, writable: desc.writable !== false, enumerable: !!desc.enumerable, configurable: desc.configurable !== false};
  }
  function wrapRuntimeFunction(name, fn) {
    if (typeof fn !== "function") return fn;
    try { if (probeWrapped && probeWrapped.has(fn)) return fn; } catch (_) {}
    try { if (fn.__gosxOuroborosWrapped === true) return fn; } catch (_) {}
    if (wrappers[name] && originals[name] === fn) return wrappers[name];
    originals[name] = fn;
    var wrapped = function() {
      var args = Array.prototype.slice.call(arguments);
      var st = stackDetail();
      var detail = internalDetail({
        argCount: args.length,
        argTypes: args.map(typeOf),
        argBytes: args.map(function(v){ return byteEstimate(v); }),
        stackHash: st.stackHash,
        source: st.source
      });
      if (name === "__gosx_canvas_event" && args.length >= 2 && finiteIntegerInRange(args[1], 1, 5)) detail.eventKind = args[1];
      var result, err;
      try {
        result = fn.apply(this, arguments);
        return result;
      } catch (e) {
        err = e;
        throw e;
      } finally {
        detail.resultType = err ? "" : typeOf(result);
        detail.resultBytes = err ? 0 : byteEstimate(result);
        detail.exception = err ? safeString(err && err.name || "Error") : "";
        try { if (result && typeof result.then === "function") detail.async = true; } catch (_) {}
        if (!inProbe) probe.record("runtime-call", name, detail);
      }
    };
    copyFunctionShape(wrapped, fn);
    try { if (probeWrapped) probeWrapped.add(wrapped); } catch (_) {}
    try { nativeDefineProperty(wrapped, "__gosxOuroborosWrapped", {value:true, configurable:true}); } catch (_) {}
    wrappers[name] = wrapped;
    if (probe.wrappedGlobals.indexOf(name) < 0) probe.wrappedGlobals.push(name);
    return wrapped;
  }
  function trapRuntimeGlobal(name) {
    var desc;
    try { desc = nativeGetOwnPropertyDescriptor(g, name); } catch (e) { return cannotWrap(name, e); }
    var trapped = trappedGlobals[name];
    if (trapped && desc && desc.get === trapped.get && desc.set === trapped.set) return;
    if (desc && desc.configurable === false) return cannotWrap(name, "non-configurable");
    if (desc && "value" in desc && desc.writable === false) return cannotWrap(name, "non-writable");
    var current = desc && "value" in desc ? desc.value : undefined;
    var enumerable = desc ? !!desc.enumerable : true;
    var writable = !desc || desc.writable !== false;
    try {
      var getter = function(){ return typeof current === "function" ? wrapRuntimeFunction(name, current) : current; };
      var setter = function(value){ if (writable) current = value; };
      nativeDefineProperty(g, name, {
        configurable: true,
        enumerable: enumerable,
        get: getter,
        set: setter
      });
      trappedGlobals[name] = {get: getter, set: setter};
      if (typeof current === "function") wrapRuntimeFunction(name, current);
    } catch (e) {
      cannotWrap(name, e);
    }
  }
  function cannotWrap(name, reason) {
    var text = safeString(reason && reason.message || reason || "unknown");
    var row = name + ":" + text;
    if (unwrapped.indexOf(row) < 0) {
      unwrapped.push(row);
      probe.unwrappedGlobals.push(row);
      probe.record("probe", "unwrapped", internalDetail({nameHash: hashText(name), unwrappedCount: probe.unwrappedGlobals.length}));
    }
  }
  function scanGlobals() {
    var names = [];
    try { names = nativeGetOwnPropertyNames(g); } catch (_) {}
    for (var i = 0; i < names.length; i++) {
      if (names[i].indexOf("__gosx_") === 0) trapRuntimeGlobal(names[i]);
    }
  }
  function patchDefineProperty() {
    try {
      if (Object.defineProperty === installState.definePropertyWrapper) return;
      var definePropertyWrapper = function(target, prop, descriptor) {
        if (target === g && typeof prop === "string" && prop.indexOf("__gosx_") === 0 && arguments.length >= 3) {
          descriptor = wrappedDescriptor(prop, descriptor);
        }
        return nativeDefineProperty.apply(this, arguments.length >= 3 ? [target, prop, descriptor] : arguments);
      };
      copyFunctionShape(definePropertyWrapper, nativeDefineProperty);
      installState.definePropertyWrapper = definePropertyWrapper;
      nativeDefineProperty(Object, "defineProperty", mergeValueDescriptor(nativeGetOwnPropertyDescriptor(Object, "defineProperty"), definePropertyWrapper));
    } catch (e) {
      cannotWrap("Object.defineProperty", e);
    }
  }
  function descriptorKey(descriptor, key) {
    if (!(key in descriptor)) return {present:false, value:undefined};
    return {present:true, value:descriptor[key]};
  }
  function wrappedDescriptor(prop, descriptor) {
    var enumerable = descriptorKey(descriptor, "enumerable");
    var configurable = descriptorKey(descriptor, "configurable");
    var value = descriptorKey(descriptor, "value");
    var writable = descriptorKey(descriptor, "writable");
    var getter = descriptorKey(descriptor, "get");
    var setter = descriptorKey(descriptor, "set");
    if (getter.present && getter.value !== undefined && typeof getter.value !== "function") throw new TypeError("Getter must be a function");
    if (setter.present && setter.value !== undefined && typeof setter.value !== "function") throw new TypeError("Setter must be a function");
    if ((getter.present || setter.present) && (value.present || writable.present)) throw new TypeError("Invalid property descriptor");
    var out = {};
    if (enumerable.present) out.enumerable = !!enumerable.value;
    if (configurable.present) out.configurable = !!configurable.value;
    if (value.present) out.value = typeof value.value === "function" ? wrapRuntimeFunction(prop, value.value) : value.value;
    if (writable.present) out.writable = !!writable.value;
    if (getter.present) out.get = getter.value;
    if (setter.present) out.set = setter.value;
    return out;
  }
  wrapJSON();
  for (var i = 0; i < knownNames.length; i++) trapRuntimeGlobal(knownNames[i]);
  scanGlobals();
  patchDefineProperty();
  if (typeof setInterval === "function" && !installState.intervalID) installState.intervalID = setInterval(scanGlobals, 50);
  if (installState.installed) {
    probe.record("probe", "refresh", internalDetail({knownGlobalCount: knownNames.length, wrappedCount: probe.wrappedGlobals.length, unwrappedCount: probe.unwrappedGlobals.length}));
  } else {
    installState.installed = true;
    probe.record("probe", "install", internalDetail({knownGlobalCount: knownNames.length, wrappedCount: probe.wrappedGlobals.length, unwrappedCount: probe.unwrappedGlobals.length}));
  }
})();`
