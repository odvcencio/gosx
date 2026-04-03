(function() {
  "use strict";

  const GOSX_VERSION = "0.2.0";

  const engineFactories = window.__gosx_engine_factories || Object.create(null);
  const loadedEngineScripts = new Map();
  window.__gosx_engine_factories = engineFactories;
  window.__gosx_register_engine_factory = function(name, factory) {
    if (!name || typeof factory !== "function") {
      console.error("[gosx] invalid engine factory registration");
      return;
    }
    engineFactories[name] = factory;
  };

  window.__gosx = {
    version: GOSX_VERSION,
    islands: new Map(),   // islandID -> { component, listeners, root }
    engines: new Map(),   // engineID -> { component, kind, mount, handle }
    hubs: new Map(),      // hubID -> { entry, socket, reconnectTimer }
    textLayouts: new Map(), // textLayoutID -> { element, result, config }
    sharedSignals: {
      values: new Map(),
      subscribers: new Map(),
      nextID: 0,
    },
    input: {
      pending: null,
      frameHandle: 0,
      providers: Object.create(null),
    },
    ready: false,
  };

  function gosxSharedSignalStore() {
    const current = window.__gosx && window.__gosx.sharedSignals;
    if (current && current.values instanceof Map && current.subscribers instanceof Map) {
      return current;
    }
    const store = {
      values: new Map(),
      subscribers: new Map(),
      nextID: 0,
    };
    if (window.__gosx) {
      window.__gosx.sharedSignals = store;
    }
    return store;
  }

  function parseSharedSignalJSON(valueJSON, fallback) {
    if (typeof valueJSON !== "string" || valueJSON === "") {
      return fallback;
    }
    if (valueJSON.startsWith("error:")) {
      return fallback;
    }
    try {
      return JSON.parse(valueJSON);
    } catch (_error) {
      return fallback;
    }
  }

  function gosxReadSharedSignal(name, fallback) {
    const signalName = String(name || "").trim();
    if (!signalName) {
      return fallback;
    }
    const store = gosxSharedSignalStore();
    if (store.values.has(signalName)) {
      return store.values.get(signalName);
    }
    const getter = window.__gosx_get_shared_signal;
    if (typeof getter !== "function") {
      return fallback;
    }
    try {
      const value = parseSharedSignalJSON(getter(signalName), fallback);
      store.values.set(signalName, value);
      return value;
    } catch (_error) {
      return fallback;
    }
  }

  function gosxNotifySharedSignal(name, valueJSON) {
    const signalName = String(name || "").trim();
    if (!signalName) {
      return null;
    }
    const store = gosxSharedSignalStore();
    const value = parseSharedSignalJSON(valueJSON, null);
    store.values.set(signalName, value);
    const listeners = store.subscribers.get(signalName);
    if (!listeners) {
      return null;
    }
    for (const entry of Array.from(listeners.values())) {
      try {
        entry(value, signalName);
      } catch (error) {
        console.error("[gosx] shared signal subscriber failed:", error);
      }
    }
    return null;
  }

  function gosxSubscribeSharedSignal(name, listener, options) {
    const signalName = String(name || "").trim();
    if (!signalName || typeof listener !== "function") {
      return function() {};
    }
    const store = gosxSharedSignalStore();
    let listeners = store.subscribers.get(signalName);
    if (!listeners) {
      listeners = new Map();
      store.subscribers.set(signalName, listeners);
    }
    store.nextID += 1;
    const id = "shared-signal-" + store.nextID;
    listeners.set(id, listener);
    if (!options || options.immediate !== false) {
      listener(gosxReadSharedSignal(signalName, null), signalName);
    }
    return function() {
      const current = store.subscribers.get(signalName);
      if (!current) {
        return;
      }
      current.delete(id);
      if (current.size === 0) {
        store.subscribers.delete(signalName);
      }
    };
  }

  window.__gosx_notify_shared_signal = gosxNotifySharedSignal;

  function gosxIssueStore() {
    if (!window.__gosx.issues || !Array.isArray(window.__gosx.issues.entries)) {
      window.__gosx.issues = {
        nextID: 0,
        entries: [],
      };
    }
    return window.__gosx.issues;
  }

  function gosxCloneIssue(issue) {
    return Object.assign({}, issue || {});
  }

  function gosxIssueText(value) {
    const text = String(value == null ? "" : value).trim();
    return text === "[object Object]" ? "" : text;
  }

  function gosxIssueMessage(issue) {
    const message = gosxIssueText(issue && issue.message);
    if (message) {
      return message;
    }
    const errorMessage = gosxIssueText(issue && issue.error && issue.error.message);
    if (errorMessage) {
      return errorMessage;
    }
    const errorText = gosxIssueText(issue && issue.error);
    if (errorText) {
      return errorText;
    }
    return "runtime failure";
  }

  function gosxMarkIssueElement(element, issue) {
    if (!element || typeof element.setAttribute !== "function") {
      return;
    }
    element.setAttribute("data-gosx-runtime-state", "error");
    element.setAttribute("data-gosx-runtime-issue", issue.type);
    if (issue.fallback) {
      element.setAttribute("data-gosx-fallback-active", issue.fallback);
    }
  }

  function gosxClearIssueState(element) {
    if (!element || typeof element.removeAttribute !== "function") {
      return;
    }
    element.setAttribute("data-gosx-runtime-state", "ready");
    element.removeAttribute("data-gosx-runtime-issue");
    element.removeAttribute("data-gosx-fallback-active");
  }

  function gosxReportRuntimeIssue(issue) {
    const store = gosxIssueStore();
    store.nextID += 1;
    const entry = {
      id: "gosx-issue-" + store.nextID,
      scope: gosxIssueText(issue && issue.scope) || "runtime",
      type: gosxIssueText(issue && issue.type) || "runtime",
      severity: gosxIssueText(issue && issue.severity) || "error",
      message: gosxIssueMessage(issue),
      component: gosxIssueText(issue && issue.component),
      ref: gosxIssueText(issue && issue.ref),
      source: gosxIssueText(issue && issue.source),
      phase: gosxIssueText(issue && issue.phase),
      fallback: gosxIssueText(issue && issue.fallback) || "server",
      elementID: gosxIssueText(issue && issue.element && issue.element.id),
      timestamp: Date.now(),
    };
    store.entries.push(entry);
    if (store.entries.length > 100) {
      store.entries.splice(0, store.entries.length - 100);
    }
    gosxMarkIssueElement(issue && issue.element, entry);
    if (document && typeof document.dispatchEvent === "function" && typeof CustomEvent === "function") {
      document.dispatchEvent(new CustomEvent("gosx:error", {
        detail: { issue: gosxCloneIssue(entry) },
      }));
    }
    return gosxCloneIssue(entry);
  }

  function gosxListIssues() {
    return gosxIssueStore().entries.map(gosxCloneIssue);
  }

  window.__gosx.reportIssue = gosxReportRuntimeIssue;
  window.__gosx.listIssues = gosxListIssues;
  window.__gosx.clearIssueState = gosxClearIssueState;

  const textMeasureCache = new Map();
  const textMeasureCacheLimit = 4096;
  const sceneLabelLayoutCacheLimit = 512;
  const textLayoutCache = new Map();
  const textLayoutCacheLimit = 1024;
  const textLayoutMetricsCache = new Map();
  const textLayoutMetricsCacheLimit = 1024;
  const textLayoutRangesCache = new Map();
  const textLayoutRangesCacheLimit = 1024;
  const TEXT_LAYOUT_ATTR = "data-gosx-text-layout";
  const TEXT_LAYOUT_ID_ATTR = "data-gosx-text-layout-id";
  const TEXT_LAYOUT_ROLE_ATTR = "data-gosx-text-layout-role";
  const TEXT_LAYOUT_SURFACE_ATTR = "data-gosx-text-layout-surface";
  const TEXT_LAYOUT_STATE_ATTR = "data-gosx-text-layout-state";
  const TEXT_LAYOUT_STYLE_ATTR = "data-gosx-text-layout-styles";
  const MANAGED_TEXT_LAYOUT_MUTATION_ATTRS = [
    "align",
    "data-gosx-text-layout-align",
    "data-gosx-text-layout-direction",
    "data-gosx-text-layout-font",
    "data-gosx-text-layout-locale",
    "data-gosx-text-layout-line-height",
    "data-gosx-text-layout-max-lines",
    "data-gosx-text-layout-max-width",
    "data-gosx-text-layout-observe",
    "data-gosx-text-layout-overflow",
    "data-gosx-text-layout-source",
    "data-gosx-text-layout-white-space",
    "dir",
    "lang",
  ];
  const textLayoutInvalidationListeners = new Set();
  const textLayoutRecordsByElement = new Map();
  let textMeasureContext = null;
  let textLayoutRevision = 0;
  let textLayoutFontObserverInstalled = false;
  let nextManagedTextLayoutID = 0;
  let textLayoutExternalImpl = typeof window.__gosx_text_layout === "function" ? window.__gosx_text_layout : null;
  let textLayoutMetricsExternalImpl = typeof window.__gosx_text_layout_metrics === "function" ? window.__gosx_text_layout_metrics : null;
  let textLayoutRangesExternalImpl = typeof window.__gosx_text_layout_ranges === "function" ? window.__gosx_text_layout_ranges : null;

  function gosxTextMeasureContext() {
    if (textMeasureContext) {
      return textMeasureContext;
    }
    const canvas = document.createElement("canvas");
    if (!canvas || typeof canvas.getContext !== "function") {
      return null;
    }
    textMeasureContext = canvas.getContext("2d");
    return textMeasureContext;
  }

  function gosxTextLayoutRevision() {
    return textLayoutRevision;
  }

  function invalidateTextLayoutCaches() {
    textLayoutRevision += 1;
    textMeasureCache.clear();
    textLayoutCache.clear();
    textLayoutMetricsCache.clear();
    textLayoutRangesCache.clear();
    for (const listener of Array.from(textLayoutInvalidationListeners)) {
      try {
        listener(textLayoutRevision);
      } catch (error) {
        console.error("[gosx] text layout invalidation listener failed:", error);
      }
    }
  }

  function onTextLayoutInvalidated(listener) {
    if (typeof listener !== "function") {
      return function() {};
    }
    textLayoutInvalidationListeners.add(listener);
    return function() {
      textLayoutInvalidationListeners.delete(listener);
    };
  }

  function installTextLayoutFontObserver() {
    if (textLayoutFontObserverInstalled) {
      return;
    }
    textLayoutFontObserverInstalled = true;

    const fonts = document.fonts;
    if (!fonts || typeof fonts !== "object") {
      return;
    }

    const onFontMetricsChanged = function() {
      invalidateTextLayoutCaches();
    };

    if (typeof fonts.addEventListener === "function") {
      fonts.addEventListener("loadingdone", onFontMetricsChanged);
      fonts.addEventListener("loadingerror", onFontMetricsChanged);
    }

    if (fonts.ready && typeof fonts.ready.then === "function") {
      fonts.ready.then(onFontMetricsChanged, function() {});
    }
  }

  window.__gosx_measure_text_batch = function(font, textsJSON) {
    let texts = textsJSON;
    if (typeof textsJSON === "string") {
      try {
        texts = JSON.parse(textsJSON);
      } catch (e) {
        console.error("[gosx] invalid text measurement payload:", e);
        return JSON.stringify([]);
      }
    }
    if (!Array.isArray(texts)) {
      return JSON.stringify([]);
    }

    const ctx = gosxTextMeasureContext();
    if (!ctx || typeof ctx.measureText !== "function") {
      return JSON.stringify(texts.map(function(value) {
        const text = value == null ? "" : String(value);
        return text.length;
      }));
    }

    if (font) {
      ctx.font = String(font);
    }

    const fontKey = font ? String(font) : "";
    const widths = texts.map(function(value) {
      const text = value == null ? "" : String(value);
      const cacheKey = fontKey + "\n" + text;
      if (textMeasureCache.has(cacheKey)) {
        return textMeasureCache.get(cacheKey);
      }
      const metrics = ctx.measureText(text);
      const width = metrics && typeof metrics.width === "number" ? metrics.width : 0;
      if (textMeasureCache.size >= textMeasureCacheLimit) {
        textMeasureCache.clear();
      }
      textMeasureCache.set(cacheKey, width);
      return width;
    });

    return JSON.stringify(widths);
  };

  window.__gosx_segment_words = function(text) {
    return JSON.stringify(segmentBrowserWordRun(text));
  };

  function normalizeTextLayoutWhiteSpace(value) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "pre-wrap":
        return "pre-wrap";
      case "pre":
        return "pre";
      default:
        return "normal";
    }
  }

  function normalizeTextLayoutAlign(value) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "left":
      case "right":
      case "center":
      case "justify":
      case "start":
      case "end":
        return mode;
      default:
        return "";
    }
  }

  function normalizeTextLayoutNewlines(text) {
    return String(text == null ? "" : text).replace(/\r\n/g, "\n").replace(/\r/g, "\n");
  }

  function textLayoutCodePointByteLength(codePoint) {
    if (codePoint <= 0x7F) return 1;
    if (codePoint <= 0x7FF) return 2;
    if (codePoint <= 0xFFFF) return 3;
    return 4;
  }

  function textLayoutIsWhitespace(char) {
    return /\s/u.test(char);
  }

  function textLayoutIsCJK(codePoint) {
    return (
      (codePoint >= 0x3400 && codePoint <= 0x4DBF) ||
      (codePoint >= 0x4E00 && codePoint <= 0x9FFF) ||
      (codePoint >= 0x3040 && codePoint <= 0x309F) ||
      (codePoint >= 0x30A0 && codePoint <= 0x30FF) ||
      (codePoint >= 0xAC00 && codePoint <= 0xD7AF)
    );
  }

  const textLayoutGraphemeSegmenters = new Map();
  const textLayoutWordSegmenters = new Map();

  function normalizeTextLayoutLocale(value) {
    const locale = typeof value === "string" ? value.trim() : "";
    if (!locale) {
      return "";
    }
    return locale.replace(/_/g, "-");
  }

  function textLayoutMakeSegmenter(granularity, locale) {
    if (!(typeof Intl === "object" && Intl && typeof Intl.Segmenter === "function")) {
      return null;
    }
    const normalizedLocale = normalizeTextLayoutLocale(locale);
    try {
      return new Intl.Segmenter(normalizedLocale || undefined, { granularity });
    } catch (_error) {
      try {
        return new Intl.Segmenter(undefined, { granularity });
      } catch (_error2) {
        return null;
      }
    }
  }

  function gosxTextLayoutGraphemeSegmenter(locale) {
    const key = normalizeTextLayoutLocale(locale);
    if (textLayoutGraphemeSegmenters.has(key)) {
      return textLayoutGraphemeSegmenters.get(key);
    }
    const segmenter = textLayoutMakeSegmenter("grapheme", key);
    textLayoutGraphemeSegmenters.set(key, segmenter || false);
    return segmenter;
  }

  function gosxTextLayoutWordSegmenter(locale) {
    const key = normalizeTextLayoutLocale(locale);
    if (textLayoutWordSegmenters.has(key)) {
      return textLayoutWordSegmenters.get(key);
    }
    const segmenter = textLayoutMakeSegmenter("word", key);
    textLayoutWordSegmenters.set(key, segmenter || false);
    return segmenter;
  }

  function normalizeTextLayoutWritingMode(value) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "vertical-rl":
      case "vertical-lr":
      case "sideways-rl":
      case "sideways-lr":
      case "horizontal-tb":
        return mode;
      default:
        return "";
    }
  }

  function textLayoutIsVerticalWritingMode(value) {
    const mode = normalizeTextLayoutWritingMode(value);
    return mode === "vertical-rl" || mode === "vertical-lr" || mode === "sideways-rl" || mode === "sideways-lr";
  }

  function textLayoutRuneCount(text) {
    return Array.from(String(text || "")).length;
  }

  function textLayoutByteLength(text) {
    let total = 0;
    for (const char of Array.from(String(text || ""))) {
      total += textLayoutCodePointByteLength(char.codePointAt(0));
    }
    return total;
  }

  function textLayoutLineEndProhibited(text) {
    const value = String(text || "");
    if (value === "") {
      return false;
    }
    for (const char of value) {
      switch (char) {
        case "\"":
        case "“":
        case "‘":
        case "«":
        case "‹":
        case "(":
        case "[":
        case "{":
        case "（":
        case "【":
        case "「":
        case "『":
        case "《":
        case "〈":
        case "〔":
        case "〖":
        case "〘":
        case "〚":
          break;
        default:
          return false;
      }
    }
    return true;
  }

  function segmentBrowserWordRun(text, locale) {
    const value = String(text || "");
    if (value === "") {
      return [];
    }
    const segmenter = gosxTextLayoutWordSegmenter(locale);
    if (segmenter) {
      const segments = [];
      for (const entry of segmenter.segment(value)) {
        segments.push(entry.segment);
      }
      if (segments.length > 0) {
        return segments;
      }
    }
    return Array.from(value);
  }

  function appendPreparedWordRun(tokens, text, byteStart, runeStart, locale) {
    const value = String(text || "");
    if (value === "") {
      return;
    }

    const segments = segmentBrowserWordRun(value, locale);
    let byteOffset = byteStart;
    let runeOffset = runeStart;
    let pending = null;
    let emitted = false;

    function emit(token, breakBefore) {
      if (breakBefore && emitted) {
        tokens.push({
          kind: "break",
          text: "",
          byteStart: token.byteStart,
          byteEnd: token.byteStart,
          runeStart: token.runeStart,
          runeEnd: token.runeStart,
        });
      }
      tokens.push(token);
      emitted = true;
    }

    function appendPending(token) {
      if (!pending) {
        pending = token;
        return;
      }
      pending.text += token.text;
      pending.byteEnd = token.byteEnd;
      pending.runeEnd = token.runeEnd;
    }

    for (const segment of segments) {
      const token = {
        kind: "word",
        text: segment,
        byteStart: byteOffset,
        byteEnd: byteOffset + textLayoutByteLength(segment),
        runeStart: runeOffset,
        runeEnd: runeOffset + textLayoutRuneCount(segment),
      };
      byteOffset = token.byteEnd;
      runeOffset = token.runeEnd;

      if (textLayoutLineEndProhibited(token.text)) {
        appendPending(token);
        continue;
      }
      if (browserTextLayoutLineStartProhibited(token.text)) {
        const previous = tokens.length > 0 ? tokens[tokens.length - 1] : null;
        if (previous && previous.kind === "word") {
          previous.text += token.text;
          previous.byteEnd = token.byteEnd;
          previous.runeEnd = token.runeEnd;
          emitted = true;
          continue;
        }
        if (pending) {
          appendPending(token);
          continue;
        }
        emit(token, emitted);
        continue;
      }

      if (pending) {
        token.text = pending.text + token.text;
        token.byteStart = pending.byteStart;
        token.runeStart = pending.runeStart;
        pending = null;
      }
      emit(token, emitted);
    }

    if (pending) {
      emit(pending, emitted);
    }
  }

  function splitPreparedTextLayoutToken(token, locale) {
    if (!token || token.kind === "newline" || token.kind === "tab" || token.kind === "soft-hyphen" || token.kind === "break" || !token.text) {
      return [token];
    }

    const graphemes = [];
    const segmenter = gosxTextLayoutGraphemeSegmenter(locale);
    if (segmenter) {
      for (const entry of segmenter.segment(token.text)) {
        graphemes.push(entry.segment);
      }
    } else {
      graphemes.push(...Array.from(token.text));
    }

    if (graphemes.length <= 1) {
      return [token];
    }

    const expanded = [];
    let byteOffset = token.byteStart;
    let runeOffset = token.runeStart;
    for (const grapheme of graphemes) {
      const runeLen = Array.from(grapheme).length;
      let byteLen = 0;
      for (const char of Array.from(grapheme)) {
        byteLen += textLayoutCodePointByteLength(char.codePointAt(0));
      }
      expanded.push({
        kind: token.kind,
        text: grapheme,
        byteStart: byteOffset,
        byteEnd: byteOffset + byteLen,
        runeStart: runeOffset,
        runeEnd: runeOffset + runeLen,
      });
      byteOffset += byteLen;
      runeOffset += runeLen;
    }
    return expanded;
  }

  function prepareBrowserTextLayout(text, whiteSpace, tabSize, locale) {
    const source = normalizeTextLayoutNewlines(text);
    const ws = normalizeTextLayoutWhiteSpace(whiteSpace);
    const normalizedLocale = normalizeTextLayoutLocale(locale);
    const tokens = [];
    const resolvedTabSize = Math.max(1, Math.floor(sceneNumber(tabSize, 8)));

    let word = "";
    let spaces = "";
    let wordByteStart = -1;
    let wordByteEnd = 0;
    let wordRuneStart = -1;
    let wordRuneEnd = 0;
    let spaceByteStart = -1;
    let spaceByteEnd = 0;
    let spaceRuneStart = -1;
    let spaceRuneEnd = 0;

    function flushWord() {
      if (!word) {
        return;
      }
      appendPreparedWordRun(tokens, word, wordByteStart, wordRuneStart, locale);
      word = "";
      wordByteStart = -1;
      wordByteEnd = 0;
      wordRuneStart = -1;
      wordRuneEnd = 0;
    }

    function flushSpaces() {
      if (!spaces) {
        return;
      }
      tokens.push({
        kind: "space",
        text: spaces,
        byteStart: spaceByteStart,
        byteEnd: spaceByteEnd,
        runeStart: spaceRuneStart,
        runeEnd: spaceRuneEnd,
      });
      spaces = "";
      spaceByteStart = -1;
      spaceByteEnd = 0;
      spaceRuneStart = -1;
      spaceRuneEnd = 0;
    }

    function appendCollapsedSpace(byteStart, byteEnd, runeStart, runeEnd) {
      flushWord();
      if (tokens.length === 0) {
        return;
      }
      const previous = tokens[tokens.length - 1];
      if (previous.kind === "space") {
        previous.byteEnd = byteEnd;
        previous.runeEnd = runeEnd;
        return;
      }
      tokens.push({
        kind: "space",
        text: " ",
        byteStart,
        byteEnd,
        runeStart,
        runeEnd,
      });
    }

    let runeIndex = 0;
    let byteOffset = 0;
    for (const char of source) {
      const codePoint = char.codePointAt(0);
      const byteStart = byteOffset;
      const byteEnd = byteStart + textLayoutCodePointByteLength(codePoint);
      const runeStart = runeIndex;
      const runeEnd = runeIndex + 1;

      if (char === "\n") {
        if (ws === "normal") {
          appendCollapsedSpace(byteStart, byteEnd, runeStart, runeEnd);
        } else {
          flushWord();
          flushSpaces();
          tokens.push({
            kind: "newline",
            text: "\n",
            byteStart,
            byteEnd,
            runeStart,
            runeEnd,
          });
        }
      } else if (char === "\t") {
        if (ws === "normal") {
          appendCollapsedSpace(byteStart, byteEnd, runeStart, runeEnd);
        } else {
          flushWord();
          flushSpaces();
          tokens.push({
            kind: "tab",
            text: "\t",
            byteStart,
            byteEnd,
            runeStart,
            runeEnd,
          });
        }
      } else if (char === "\u00ad") {
        flushWord();
        flushSpaces();
        tokens.push({
          kind: "soft-hyphen",
          text: "\u00ad",
          byteStart,
          byteEnd,
          runeStart,
          runeEnd,
        });
      } else if (char === "\u200b") {
        flushWord();
        flushSpaces();
        tokens.push({
          kind: "break",
          text: "\u200b",
          byteStart,
          byteEnd,
          runeStart,
          runeEnd,
        });
      } else if (textLayoutIsWhitespace(char)) {
        if (ws === "normal") {
          appendCollapsedSpace(byteStart, byteEnd, runeStart, runeEnd);
        } else {
          flushWord();
          if (spaceByteStart < 0) {
            spaceByteStart = byteStart;
            spaceRuneStart = runeStart;
          }
          spaceByteEnd = byteEnd;
          spaceRuneEnd = runeEnd;
          spaces += char;
        }
      } else {
        flushSpaces();
        if (wordByteStart < 0) {
          wordByteStart = byteStart;
          wordRuneStart = runeStart;
        }
        wordByteEnd = byteEnd;
        wordRuneEnd = runeEnd;
        word += char;
      }

      runeIndex += 1;
      byteOffset = byteEnd;
    }

    flushWord();
    flushSpaces();

    return {
      source,
      byteLen: byteOffset,
      runeCount: runeIndex,
      whiteSpace: ws,
      locale: normalizedLocale,
      tabSize: resolvedTabSize,
      tokens,
    };
  }

  function measurePreparedBrowserTextLayout(prepared, font) {
    const expandedTokens = [];
    for (const token of prepared.tokens) {
      expandedTokens.push(...splitPreparedTextLayoutToken(token, prepared.locale));
    }
    const measured = {
      source: prepared.source,
      byteLen: prepared.byteLen,
      runeCount: prepared.runeCount,
      whiteSpace: prepared.whiteSpace,
      tabSize: Math.max(1, Math.floor(sceneNumber(prepared.tabSize, 8))),
      locale: normalizeTextLayoutLocale(prepared.locale),
      spaceWidth: 0,
      hyphenWidth: 0,
      ellipsisWidth: 0,
      font: typeof font === "string" ? font : "",
      tokens: expandedTokens.map(function(token) {
        return Object.assign({ width: 0 }, token);
      }),
    };

    const texts = [];
    const indexes = [];
    let needSpaceWidth = false;
    let needHyphenWidth = false;
    let needEllipsisWidth = true;
    for (let i = 0; i < measured.tokens.length; i += 1) {
      if (measured.tokens[i].kind === "newline" || measured.tokens[i].kind === "tab" || measured.tokens[i].kind === "soft-hyphen" || measured.tokens[i].kind === "break") {
        if (measured.tokens[i].kind === "tab") {
          needSpaceWidth = true;
        }
        if (measured.tokens[i].kind === "soft-hyphen") {
          needHyphenWidth = true;
        }
        continue;
      }
      texts.push(measured.tokens[i].text);
      indexes.push(i);
    }
    let spaceIndex = -1;
    let hyphenIndex = -1;
    let ellipsisIndex = -1;
    if (needSpaceWidth) {
      spaceIndex = texts.length;
      texts.push(" ");
    }
    if (needHyphenWidth) {
      hyphenIndex = texts.length;
      texts.push("-");
    }
    if (needEllipsisWidth) {
      ellipsisIndex = texts.length;
      texts.push("…");
    }

    if (texts.length === 0) {
      return measured;
    }

    let widths = [];
    try {
      const raw = window.__gosx_measure_text_batch(measured.font, JSON.stringify(texts));
      widths = typeof raw === "string" ? JSON.parse(raw) : raw;
    } catch (_error) {
      widths = texts.map(function(value) {
        return String(value || "").length * 8;
      });
    }

    if (!Array.isArray(widths) || widths.length !== texts.length) {
      widths = texts.map(function(value) {
        return String(value || "").length * 8;
      });
    }

    for (let i = 0; i < widths.length; i += 1) {
      if (i < indexes.length) {
        measured.tokens[indexes[i]].width = sceneNumber(widths[i], 0);
      }
    }
    if (spaceIndex >= 0 && spaceIndex < widths.length) {
      measured.spaceWidth = sceneNumber(widths[spaceIndex], 0);
    }
    if (hyphenIndex >= 0 && hyphenIndex < widths.length) {
      measured.hyphenWidth = sceneNumber(widths[hyphenIndex], 0);
    }
    if (ellipsisIndex >= 0 && ellipsisIndex < widths.length) {
      measured.ellipsisWidth = sceneNumber(widths[ellipsisIndex], 0);
    }

    return measured;
  }

  function browserTextLayoutLineText(measured, start, end, softBreak) {
    const tokens = measured.tokens;
    let textEnd = end;
    if (normalizeTextLayoutWhiteSpace(measured.whiteSpace) === "normal") {
      while (textEnd > start && tokens[textEnd - 1].kind === "space") {
        textEnd -= 1;
      }
    }
    let text = "";
    for (let i = start; i < textEnd && i < tokens.length; i += 1) {
      if (tokens[i].kind === "newline" || tokens[i].kind === "soft-hyphen" || tokens[i].kind === "break") {
        continue;
      }
      text += tokens[i].text;
    }
    if (softBreak) {
      text += "-";
    }
    return text;
  }

  function browserTextLayoutTabAdvance(measured, lineWidth) {
    const tabSize = Math.max(1, Math.floor(sceneNumber(measured.tabSize, 8)));
    const spaceWidth = Math.max(1, sceneNumber(measured.spaceWidth, 1));
    const tabStop = tabSize * spaceWidth;
    const remainder = lineWidth % tabStop;
    if (Math.abs(remainder) <= 1e-6) {
      return tabStop;
    }
    return tabStop - remainder;
  }

  function browserTextLayoutHyphenAdvance(measured) {
    return Math.max(1, sceneNumber(measured.hyphenWidth, 1));
  }

  function browserTextLayoutTokenProgressWidth(measured, lineWidth, token) {
    switch (token.kind) {
      case "tab":
        return browserTextLayoutTabAdvance(measured, lineWidth);
      case "soft-hyphen":
      case "break":
      case "newline":
        return 0;
      default:
        return sceneNumber(token.width, 0);
    }
  }

  function browserTextLayoutTokenFitAdvance(measured, lineWidth, token) {
    switch (token.kind) {
      case "space":
        return normalizeTextLayoutWhiteSpace(measured.whiteSpace) === "normal" ? 0 : sceneNumber(token.width, 0);
      case "tab":
        return 0;
      case "soft-hyphen":
        return browserTextLayoutHyphenAdvance(measured);
      case "break":
      case "newline":
        return 0;
      default:
        return sceneNumber(token.width, 0);
    }
  }

  function browserTextLayoutTokenPaintAdvance(measured, lineWidth, token, softBreak) {
    switch (token.kind) {
      case "space":
        return normalizeTextLayoutWhiteSpace(measured.whiteSpace) === "normal" ? 0 : sceneNumber(token.width, 0);
      case "tab":
        return browserTextLayoutTabAdvance(measured, lineWidth);
      case "soft-hyphen":
        return softBreak ? browserTextLayoutHyphenAdvance(measured) : 0;
      case "break":
      case "newline":
        return 0;
      default:
        return sceneNumber(token.width, 0);
    }
  }

  function browserTextLayoutCanBreakAfter(token) {
    return token.kind === "space" || token.kind === "tab" || token.kind === "soft-hyphen" || token.kind === "break";
  }

  function browserTextLayoutLineStartProhibited(text) {
    const value = String(text || "");
    if (value === "") {
      return false;
    }
    for (const char of value) {
      switch (char) {
        case ".":
        case ",":
        case "!":
        case "?":
        case ":":
        case ";":
        case ")":
        case "]":
        case "}":
        case "%":
        case "\"":
        case "”":
        case "’":
        case "»":
        case "›":
        case "…":
        case "、":
        case "。":
        case "，":
        case "．":
        case "！":
        case "？":
        case "：":
        case "；":
        case "）":
        case "】":
        case "」":
        case "』":
        case "》":
        case "〉":
        case "〕":
        case "〗":
        case "〙":
        case "〛":
        case "ー":
        case "々":
        case "ゝ":
        case "ゞ":
        case "ヽ":
        case "ヾ":
          break;
        default:
          return false;
      }
    }
    return true;
  }

  function browserTextLayoutRangeWidth(measured, start, end, softBreak) {
    let progress = 0;
    let display = 0;
    for (let i = start; i < end && i < measured.tokens.length; i += 1) {
      const token = measured.tokens[i];
      const before = progress;
      progress += browserTextLayoutTokenProgressWidth(measured, progress, token);
      display = before + browserTextLayoutTokenPaintAdvance(measured, before, token, softBreak && i === end - 1 && token.kind === "soft-hyphen");
    }
    return display;
  }

  function buildBrowserTextLayoutRecord(measured, start, end, hardBreak, softBreak, width, includeText) {
    const tokens = measured.tokens;
    const line = {
      start,
      end,
      byteStart: 0,
      byteEnd: 0,
      runeStart: 0,
      runeEnd: 0,
      width: width == null ? browserTextLayoutRangeWidth(measured, start, end, softBreak) : width,
      hardBreak: Boolean(hardBreak),
      softBreak: Boolean(softBreak),
    };
    if (includeText) {
      line.text = browserTextLayoutLineText(measured, start, end, softBreak);
    }
    if (start < end && start < tokens.length) {
      line.byteStart = tokens[start].byteStart;
      line.byteEnd = tokens[end - 1].byteEnd;
      line.runeStart = tokens[start].runeStart;
      line.runeEnd = tokens[end - 1].runeEnd;
    }
    return line;
  }

  function buildBrowserTextLayoutLine(measured, start, end, hardBreak, softBreak, width) {
    return buildBrowserTextLayoutRecord(measured, start, end, hardBreak, softBreak, width, true);
  }

  function buildBrowserTextLayoutRange(measured, start, end, hardBreak, softBreak, width) {
    return buildBrowserTextLayoutRecord(measured, start, end, hardBreak, softBreak, width, false);
  }

  function emptyBrowserTextLayoutLineAtIndex(tokens, index, hardBreak) {
    const line = {
      start: index,
      end: index,
      byteStart: 0,
      byteEnd: 0,
      runeStart: 0,
      runeEnd: 0,
      width: 0,
      text: "",
      hardBreak: Boolean(hardBreak),
      softBreak: false,
    };
    if (index >= 0 && index < tokens.length) {
      line.byteStart = tokens[index].byteStart;
      line.byteEnd = tokens[index].byteStart;
      line.runeStart = tokens[index].runeStart;
      line.runeEnd = tokens[index].runeStart;
    }
    return line;
  }

  function emptyBrowserTextLayoutLineAtEnd(measured) {
    return {
      start: measured.tokens.length,
      end: measured.tokens.length,
      byteStart: measured.byteLen,
      byteEnd: measured.byteLen,
      runeStart: measured.runeCount,
      runeEnd: measured.runeCount,
      width: 0,
      text: "",
      hardBreak: false,
      softBreak: false,
    };
  }

  function browserTextLayoutEllipsisAdvance(measured) {
    return Math.max(0, sceneNumber(measured && measured.ellipsisWidth, 1)) || 1;
  }

  function trimBrowserTextLayoutDisplayEnd(measured, start, end) {
    if (normalizeTextLayoutWhiteSpace(measured.whiteSpace) !== "normal") {
      return end;
    }
    while (end > start && measured.tokens[end - 1].kind === "space") {
      end -= 1;
    }
    return end;
  }

  function hasMoreBrowserTextLayoutContent(measured, next) {
    if (next < measured.tokens.length) {
      return true;
    }
    return measured.tokens.length > 0 && measured.tokens[measured.tokens.length - 1].kind === "newline";
  }

  function clampBrowserTextLayoutLine(line, measured, maxWidth, overflow, includeText) {
    const clamped = Object.assign({}, line, {
      truncated: true,
      ellipsis: false,
      hardBreak: false,
      softBreak: false,
    });
    if (normalizeTextLayoutOverflow(overflow) !== "ellipsis") {
      return clamped;
    }

    const ellipsisWidth = browserTextLayoutEllipsisAdvance(measured);
    if (!(maxWidth > 0)) {
      clamped.ellipsis = true;
      clamped.width += ellipsisWidth;
      if (includeText) {
        clamped.text = String(clamped.text || "") + "…";
      }
      return clamped;
    }

    const allowedWidth = maxWidth - ellipsisWidth;
    let end = trimBrowserTextLayoutDisplayEnd(measured, clamped.start, clamped.end);
    while (end > clamped.start && browserTextLayoutRangeWidth(measured, clamped.start, end, false) > allowedWidth) {
      end -= 1;
      end = trimBrowserTextLayoutDisplayEnd(measured, clamped.start, end);
    }

    clamped.end = end;
    clamped.ellipsis = true;
    if (end > clamped.start) {
      clamped.byteEnd = measured.tokens[end - 1].byteEnd;
      clamped.runeEnd = measured.tokens[end - 1].runeEnd;
      clamped.width = Math.min(maxWidth, browserTextLayoutRangeWidth(measured, clamped.start, end, false) + ellipsisWidth);
      if (includeText) {
        clamped.text = browserTextLayoutLineText(measured, clamped.start, end, false) + "…";
      }
      return clamped;
    }

    clamped.byteEnd = clamped.byteStart;
    clamped.runeEnd = clamped.runeStart;
    clamped.width = Math.min(maxWidth, ellipsisWidth);
    if (includeText) {
      clamped.text = "…";
    }
    return clamped;
  }

  function normalizeBrowserTextLayoutLineStart(measured, start) {
    const whiteSpace = normalizeTextLayoutWhiteSpace(measured.whiteSpace);
    while (start < measured.tokens.length) {
      const kind = measured.tokens[start].kind;
      if (kind === "soft-hyphen" || kind === "break") {
        start += 1;
        continue;
      }
      if (kind === "space" && whiteSpace === "normal") {
        start += 1;
        continue;
      }
      break;
    }
    return start;
  }

  function normalizeBrowserTextLayoutNextStart(measured, start) {
    return normalizeBrowserTextLayoutLineStart(measured, start);
  }

  function layoutBrowserPreLine(measured, start) {
    const tokens = measured.tokens;
    for (let i = start; i < tokens.length; i += 1) {
      if (tokens[i].kind === "newline") {
        return [buildBrowserTextLayoutLine(measured, start, i, true, false), i + 1];
      }
    }
    return [buildBrowserTextLayoutLine(measured, start, tokens.length, false, false), tokens.length];
  }

  function layoutBrowserPreLineRange(measured, start) {
    const tokens = measured.tokens;
    for (let i = start; i < tokens.length; i += 1) {
      if (tokens[i].kind === "newline") {
        return [buildBrowserTextLayoutRange(measured, start, i, true, false), i + 1];
      }
    }
    return [buildBrowserTextLayoutRange(measured, start, tokens.length, false, false), tokens.length];
  }

  function layoutBrowserWrappedLine(measured, start, whiteSpace, maxWidth) {
    const tokens = measured.tokens;
    let lineWidth = 0;
    let lastBreak = -1;
    let lastBreakWidth = 0;
    let lastBreakSoft = false;

    for (let i = start; i < tokens.length; i += 1) {
      const token = tokens[i];
      if (token.kind === "newline") {
        return [buildBrowserTextLayoutLine(measured, start, i, true, false), i + 1];
      }

      const tokenWidth = browserTextLayoutTokenProgressWidth(measured, lineWidth, token);
      const fitAdvance = browserTextLayoutTokenFitAdvance(measured, lineWidth, token);
      const paintAdvance = browserTextLayoutTokenPaintAdvance(measured, lineWidth, token, token.kind === "soft-hyphen");

      if (browserTextLayoutCanBreakAfter(token)) {
        lastBreak = i + 1;
        lastBreakWidth = lineWidth + paintAdvance;
        lastBreakSoft = token.kind === "soft-hyphen";
      }

      const candidateWidth = lineWidth + tokenWidth;
      if (maxWidth > 0 && candidateWidth > maxWidth) {
        if (browserTextLayoutCanBreakAfter(token) && lineWidth + fitAdvance <= maxWidth) {
          return [
            buildBrowserTextLayoutLine(measured, start, i + 1, false, token.kind === "soft-hyphen"),
            normalizeBrowserTextLayoutNextStart(measured, i + 1),
          ];
        }
        if (lastBreak > start) {
          return [
            buildBrowserTextLayoutLine(measured, start, lastBreak, false, lastBreakSoft, lastBreakWidth),
            normalizeBrowserTextLayoutNextStart(measured, lastBreak),
          ];
        }
        if (i > start && textLayoutLineEndProhibited(tokens[i - 1].text)) {
          return [
            buildBrowserTextLayoutLine(measured, start, i + 1, false, false),
            normalizeBrowserTextLayoutNextStart(measured, i + 1),
          ];
        }
        if (lineWidth > 0 && browserTextLayoutLineStartProhibited(token.text)) {
          return [
            buildBrowserTextLayoutLine(measured, start, i + 1, false, false),
            normalizeBrowserTextLayoutNextStart(measured, i + 1),
          ];
        }
        if (lineWidth === 0) {
          return [
            buildBrowserTextLayoutLine(measured, start, i + 1, false, false),
            normalizeBrowserTextLayoutNextStart(measured, i + 1),
          ];
        }
        return [
          buildBrowserTextLayoutLine(measured, start, i, false, false),
          normalizeBrowserTextLayoutNextStart(measured, i),
        ];
      }

      lineWidth = candidateWidth;
    }

    return [buildBrowserTextLayoutLine(measured, start, tokens.length, false, false), tokens.length];
  }

  function layoutBrowserWrappedLineRange(measured, start, whiteSpace, maxWidth) {
    const tokens = measured.tokens;
    let lineWidth = 0;
    let lastBreak = -1;
    let lastBreakWidth = 0;
    let lastBreakSoft = false;

    for (let i = start; i < tokens.length; i += 1) {
      const token = tokens[i];
      if (token.kind === "newline") {
        return [buildBrowserTextLayoutRange(measured, start, i, true, false), i + 1];
      }

      const tokenWidth = browserTextLayoutTokenProgressWidth(measured, lineWidth, token);
      const fitAdvance = browserTextLayoutTokenFitAdvance(measured, lineWidth, token);
      const paintAdvance = browserTextLayoutTokenPaintAdvance(measured, lineWidth, token, token.kind === "soft-hyphen");

      if (browserTextLayoutCanBreakAfter(token)) {
        lastBreak = i + 1;
        lastBreakWidth = lineWidth + paintAdvance;
        lastBreakSoft = token.kind === "soft-hyphen";
      }

      const candidateWidth = lineWidth + tokenWidth;
      if (maxWidth > 0 && candidateWidth > maxWidth) {
        if (browserTextLayoutCanBreakAfter(token) && lineWidth + fitAdvance <= maxWidth) {
          return [
            buildBrowserTextLayoutRange(measured, start, i + 1, false, token.kind === "soft-hyphen"),
            normalizeBrowserTextLayoutNextStart(measured, i + 1),
          ];
        }
        if (lastBreak > start) {
          return [
            buildBrowserTextLayoutRange(measured, start, lastBreak, false, lastBreakSoft, lastBreakWidth),
            normalizeBrowserTextLayoutNextStart(measured, lastBreak),
          ];
        }
        if (i > start && textLayoutLineEndProhibited(tokens[i - 1].text)) {
          return [
            buildBrowserTextLayoutRange(measured, start, i + 1, false, false),
            normalizeBrowserTextLayoutNextStart(measured, i + 1),
          ];
        }
        if (lineWidth > 0 && browserTextLayoutLineStartProhibited(token.text)) {
          return [
            buildBrowserTextLayoutRange(measured, start, i + 1, false, false),
            normalizeBrowserTextLayoutNextStart(measured, i + 1),
          ];
        }
        if (lineWidth === 0) {
          return [
            buildBrowserTextLayoutRange(measured, start, i + 1, false, false),
            normalizeBrowserTextLayoutNextStart(measured, i + 1),
          ];
        }
        return [
          buildBrowserTextLayoutRange(measured, start, i, false, false),
          normalizeBrowserTextLayoutNextStart(measured, i),
        ];
      }

      lineWidth = candidateWidth;
    }

    return [buildBrowserTextLayoutRange(measured, start, tokens.length, false, false), tokens.length];
  }

  function layoutBrowserTextNextLine(measured, start, maxWidth) {
    const tokens = measured.tokens;
    if (start < 0) {
      start = 0;
    }
    if (start >= tokens.length) {
      return [emptyBrowserTextLayoutLineAtEnd(measured), tokens.length];
    }

    const whiteSpace = normalizeTextLayoutWhiteSpace(measured.whiteSpace);
    let lineStart = normalizeBrowserTextLayoutLineStart(measured, start);
    if (lineStart >= tokens.length) {
      return [emptyBrowserTextLayoutLineAtEnd(measured), tokens.length];
    }

    if (tokens[lineStart].kind === "newline") {
      return [emptyBrowserTextLayoutLineAtIndex(tokens, lineStart, true), lineStart + 1];
    }

    if (whiteSpace === "pre") {
      return layoutBrowserPreLine(measured, lineStart);
    }
    return layoutBrowserWrappedLine(measured, lineStart, whiteSpace, Math.max(0, sceneNumber(maxWidth, 0)));
  }

  function layoutBrowserTextNextRange(measured, start, maxWidth) {
    const tokens = measured.tokens;
    if (start < 0) {
      start = 0;
    }
    if (start >= tokens.length) {
      return [emptyBrowserTextLayoutLineAtEnd(measured), tokens.length];
    }

    const whiteSpace = normalizeTextLayoutWhiteSpace(measured.whiteSpace);
    let lineStart = normalizeBrowserTextLayoutLineStart(measured, start);
    if (lineStart >= tokens.length) {
      return [emptyBrowserTextLayoutLineAtEnd(measured), tokens.length];
    }

    if (tokens[lineStart].kind === "newline") {
      return [emptyBrowserTextLayoutLineAtIndex(tokens, lineStart, true), lineStart + 1];
    }

    if (whiteSpace === "pre") {
      return layoutBrowserPreLineRange(measured, lineStart);
    }
    return layoutBrowserWrappedLineRange(measured, lineStart, whiteSpace, Math.max(0, sceneNumber(maxWidth, 0)));
  }

  function layoutBrowserText(text, font, maxWidth, whiteSpace, lineHeight, options) {
    const normalizedOptions = normalizeTextLayoutRunOptions(options);
    const prepared = prepareBrowserTextLayout(text, whiteSpace, 8, normalizedOptions.locale);
    const measured = measurePreparedBrowserTextLayout(prepared, font);
    const resolvedLineHeight = Math.max(1, sceneNumber(lineHeight, 1));

    if (measured.tokens.length === 0) {
      return {
        lines: [{
          start: 0,
          end: 0,
          byteStart: measured.byteLen,
          byteEnd: measured.byteLen,
          runeStart: measured.runeCount,
          runeEnd: measured.runeCount,
          width: 0,
          text: "",
          hardBreak: false,
        }],
        lineCount: 1,
        height: resolvedLineHeight,
        maxLineWidth: 0,
        byteLen: measured.byteLen,
        runeCount: measured.runeCount,
        truncated: false,
      };
    }

    const lines = [];
    let truncated = false;
    let count = 0;
    for (let start = 0; start < measured.tokens.length;) {
      const nextLine = layoutBrowserTextNextLine(measured, start, maxWidth);
      let line = nextLine[0];
      count += 1;
      if (normalizedOptions.maxLines > 0 && count === normalizedOptions.maxLines && hasMoreBrowserTextLayoutContent(measured, nextLine[1])) {
        line = clampBrowserTextLayoutLine(line, measured, Math.max(0, sceneNumber(maxWidth, 0)), normalizedOptions.overflow, true);
        truncated = true;
        lines.push(line);
        break;
      }
      lines.push(line);
      start = nextLine[1] > start ? nextLine[1] : start + 1;
    }

    if (!truncated && measured.tokens[measured.tokens.length - 1].kind === "newline" && !(normalizedOptions.maxLines > 0 && lines.length >= normalizedOptions.maxLines)) {
      lines.push(emptyBrowserTextLayoutLineAtEnd(measured));
    }

    let maxLineWidth = 0;
    for (const line of lines) {
      if (line.width > maxLineWidth) {
        maxLineWidth = line.width;
      }
    }

    return {
      lines,
      lineCount: lines.length,
      height: lines.length * resolvedLineHeight,
      maxLineWidth,
      byteLen: measured.byteLen,
      runeCount: measured.runeCount,
      truncated,
    };
  }

  function layoutBrowserTextRanges(text, font, maxWidth, whiteSpace, lineHeight, options) {
    const normalizedOptions = normalizeTextLayoutRunOptions(options);
    const prepared = prepareBrowserTextLayout(text, whiteSpace, 8, normalizedOptions.locale);
    const measured = measurePreparedBrowserTextLayout(prepared, font);
    const resolvedLineHeight = Math.max(1, sceneNumber(lineHeight, 1));

    if (measured.tokens.length === 0) {
      return {
        lines: [{
          start: 0,
          end: 0,
          byteStart: measured.byteLen,
          byteEnd: measured.byteLen,
          runeStart: measured.runeCount,
          runeEnd: measured.runeCount,
          width: 0,
          hardBreak: false,
          softBreak: false,
        }],
        lineCount: 1,
        height: resolvedLineHeight,
        maxLineWidth: 0,
        byteLen: measured.byteLen,
        runeCount: measured.runeCount,
        truncated: false,
      };
    }

    const lines = [];
    let truncated = false;
    let count = 0;
    for (let start = 0; start < measured.tokens.length;) {
      const nextLine = layoutBrowserTextNextRange(measured, start, maxWidth);
      let line = nextLine[0];
      count += 1;
      if (normalizedOptions.maxLines > 0 && count === normalizedOptions.maxLines && hasMoreBrowserTextLayoutContent(measured, nextLine[1])) {
        line = clampBrowserTextLayoutLine(line, measured, Math.max(0, sceneNumber(maxWidth, 0)), normalizedOptions.overflow, false);
        truncated = true;
        lines.push(line);
        break;
      }
      lines.push(line);
      start = nextLine[1] > start ? nextLine[1] : start + 1;
    }

    if (!truncated && measured.tokens[measured.tokens.length - 1].kind === "newline" && !(normalizedOptions.maxLines > 0 && lines.length >= normalizedOptions.maxLines)) {
      lines.push(emptyBrowserTextLayoutLineAtEnd(measured));
    }

    let maxLineWidth = 0;
    for (const line of lines) {
      if (line.width > maxLineWidth) {
        maxLineWidth = line.width;
      }
    }

    return {
      lines,
      lineCount: lines.length,
      height: lines.length * resolvedLineHeight,
      maxLineWidth,
      byteLen: measured.byteLen,
      runeCount: measured.runeCount,
      truncated,
    };
  }

  function adoptTextLayoutImpl(candidate) {
    if (typeof candidate !== "function" || candidate === gosxTextLayout) {
      return;
    }
    if (textLayoutExternalImpl === candidate) {
      return;
    }
    textLayoutExternalImpl = candidate;
    invalidateTextLayoutCaches();
  }

  function adoptTextLayoutMetricsImpl(candidate) {
    if (typeof candidate !== "function" || candidate === gosxTextLayoutMetrics) {
      return;
    }
    if (textLayoutMetricsExternalImpl === candidate) {
      return;
    }
    textLayoutMetricsExternalImpl = candidate;
    invalidateTextLayoutCaches();
  }

  function adoptTextLayoutRangesImpl(candidate) {
    if (typeof candidate !== "function" || candidate === gosxTextLayoutRanges) {
      return;
    }
    if (textLayoutRangesExternalImpl === candidate) {
      return;
    }
    textLayoutRangesExternalImpl = candidate;
    invalidateTextLayoutCaches();
  }

  function currentTextLayoutImpl() {
    return typeof textLayoutExternalImpl === "function" ? textLayoutExternalImpl : null;
  }

  function normalizeTextLayoutOverflow(value) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    return mode === "ellipsis" ? "ellipsis" : "clip";
  }

  function normalizeTextLayoutRunOptions(options) {
    const input = options && typeof options === "object" ? options : {};
    return {
      maxLines: Math.max(0, Math.floor(sceneNumber(input.maxLines, 0))),
      overflow: normalizeTextLayoutOverflow(input.overflow),
      locale: normalizeTextLayoutLocale(input.locale),
    };
  }

  function textLayoutCacheKey(text, font, maxWidth, whiteSpace, lineHeight, options) {
    const normalized = normalizeTextLayoutRunOptions(options);
    return [
      gosxTextLayoutRevision(),
      String(text == null ? "" : text),
      String(font == null ? "" : font),
      sceneNumber(maxWidth, 0),
      normalizeTextLayoutWhiteSpace(whiteSpace),
      sceneNumber(lineHeight, 1),
      normalized.maxLines,
      normalized.overflow,
      normalized.locale,
      currentTextLayoutImpl() ? "external" : "browser",
    ].join("\n");
  }

  function textLayoutMetricsCacheKey(text, font, maxWidth, whiteSpace, lineHeight, options) {
    const normalized = normalizeTextLayoutRunOptions(options);
    return [
      gosxTextLayoutRevision(),
      String(text == null ? "" : text),
      String(font == null ? "" : font),
      sceneNumber(maxWidth, 0),
      normalizeTextLayoutWhiteSpace(whiteSpace),
      sceneNumber(lineHeight, 1),
      normalized.maxLines,
      normalized.overflow,
      normalized.locale,
      textLayoutMetricsExternalImpl ? "external" : "derived",
    ].join("\n");
  }

  function textLayoutRangesCacheKey(text, font, maxWidth, whiteSpace, lineHeight, options) {
    const normalized = normalizeTextLayoutRunOptions(options);
    return [
      gosxTextLayoutRevision(),
      String(text == null ? "" : text),
      String(font == null ? "" : font),
      sceneNumber(maxWidth, 0),
      normalizeTextLayoutWhiteSpace(whiteSpace),
      sceneNumber(lineHeight, 1),
      normalized.maxLines,
      normalized.overflow,
      normalized.locale,
      textLayoutRangesExternalImpl ? "external" : "browser",
    ].join("\n");
  }

  function normalizeTextLayoutLine(line, index, textByteLen, textRuneCount) {
    const item = line && typeof line === "object" ? line : {};
    const start = Math.max(0, Math.floor(sceneNumber(item.start, index)));
    const end = Math.max(start, Math.floor(sceneNumber(item.end, start)));
    const byteStart = Math.max(0, Math.floor(sceneNumber(item.byteStart, 0)));
    const byteEnd = Math.max(byteStart, Math.floor(sceneNumber(item.byteEnd, byteStart)));
    const runeStart = Math.max(0, Math.floor(sceneNumber(item.runeStart, 0)));
    const runeEnd = Math.max(runeStart, Math.floor(sceneNumber(item.runeEnd, runeStart)));
    return {
      start,
      end,
      byteStart: Math.min(byteStart, textByteLen),
      byteEnd: Math.min(byteEnd, textByteLen),
      runeStart: Math.min(runeStart, textRuneCount),
      runeEnd: Math.min(runeEnd, textRuneCount),
      width: Math.max(0, sceneNumber(item.width, 0)),
      text: typeof item.text === "string" ? item.text : "",
      hardBreak: Boolean(item.hardBreak),
      softBreak: Boolean(item.softBreak),
      truncated: Boolean(item.truncated),
      ellipsis: Boolean(item.ellipsis),
    };
  }

  function normalizeTextLayoutRange(line, index, textByteLen, textRuneCount) {
    const item = line && typeof line === "object" ? line : {};
    const start = Math.max(0, Math.floor(sceneNumber(item.start, index)));
    const end = Math.max(start, Math.floor(sceneNumber(item.end, start)));
    const byteStart = Math.max(0, Math.floor(sceneNumber(item.byteStart, 0)));
    const byteEnd = Math.max(byteStart, Math.floor(sceneNumber(item.byteEnd, byteStart)));
    const runeStart = Math.max(0, Math.floor(sceneNumber(item.runeStart, 0)));
    const runeEnd = Math.max(runeStart, Math.floor(sceneNumber(item.runeEnd, runeStart)));
    return {
      start,
      end,
      byteStart: Math.min(byteStart, textByteLen),
      byteEnd: Math.min(byteEnd, textByteLen),
      runeStart: Math.min(runeStart, textRuneCount),
      runeEnd: Math.min(runeEnd, textRuneCount),
      width: Math.max(0, sceneNumber(item.width, 0)),
      hardBreak: Boolean(item.hardBreak),
      softBreak: Boolean(item.softBreak),
      truncated: Boolean(item.truncated),
      ellipsis: Boolean(item.ellipsis),
    };
  }

  function normalizeTextLayoutResult(result, text, lineHeight) {
    const source = normalizeTextLayoutNewlines(text);
    const graphemes = Array.from(source);
    const runeCount = graphemes.length;
    const byteLen = graphemes.reduce(function(total, char) {
        return total + textLayoutCodePointByteLength(char.codePointAt(0));
      }, 0);
    const lines = Array.isArray(result && result.lines)
      ? result.lines.map(function(line, index) {
          return normalizeTextLayoutLine(line, index, byteLen, runeCount);
        })
      : [];

    return {
      lines,
      lineCount: Math.max(lines.length, Math.floor(sceneNumber(result && result.lineCount, lines.length))),
      height: Math.max(0, sceneNumber(result && result.height, lines.length * Math.max(1, sceneNumber(lineHeight, 1)))),
      maxLineWidth: Math.max(0, sceneNumber(result && result.maxLineWidth, 0)),
      byteLen: Math.max(0, Math.min(byteLen, Math.floor(sceneNumber(result && result.byteLen, byteLen)))),
      runeCount: Math.max(0, Math.min(runeCount, Math.floor(sceneNumber(result && result.runeCount, runeCount)))),
      truncated: Boolean(result && result.truncated) || lines.some(function(line) { return line.truncated; }),
    };
  }

  function normalizeTextLayoutRangeResult(result, text, lineHeight) {
    const source = normalizeTextLayoutNewlines(text);
    const graphemes = Array.from(source);
    const runeCount = graphemes.length;
    const byteLen = graphemes.reduce(function(total, char) {
      return total + textLayoutCodePointByteLength(char.codePointAt(0));
    }, 0);
    const lines = Array.isArray(result && result.lines)
      ? result.lines.map(function(line, index) {
          return normalizeTextLayoutRange(line, index, byteLen, runeCount);
        })
      : [];

    return {
      lines,
      lineCount: Math.max(lines.length, Math.floor(sceneNumber(result && result.lineCount, lines.length))),
      height: Math.max(0, sceneNumber(result && result.height, lines.length * Math.max(1, sceneNumber(lineHeight, 1)))),
      maxLineWidth: Math.max(0, sceneNumber(result && result.maxLineWidth, 0)),
      byteLen: Math.max(0, Math.min(byteLen, Math.floor(sceneNumber(result && result.byteLen, byteLen)))),
      runeCount: Math.max(0, Math.min(runeCount, Math.floor(sceneNumber(result && result.runeCount, runeCount)))),
      truncated: Boolean(result && result.truncated) || lines.some(function(line) { return line.truncated; }),
    };
  }

  function gosxTextLayout(text, font, maxWidth, whiteSpace, lineHeight, options) {
    const normalizedOptions = normalizeTextLayoutRunOptions(options);
    const cacheKey = textLayoutCacheKey(text, font, maxWidth, whiteSpace, lineHeight, normalizedOptions);
    if (textLayoutCache.has(cacheKey)) {
      return textLayoutCache.get(cacheKey);
    }

    const impl = currentTextLayoutImpl();
    let result = null;
    if (impl) {
      try {
        result = impl(text, font, maxWidth, whiteSpace, lineHeight, normalizedOptions);
      } catch (error) {
        console.error("[gosx] text layout implementation failed:", error);
      }
    }
    if (!result || !Array.isArray(result.lines)) {
      result = layoutBrowserText(text, font, maxWidth, whiteSpace, lineHeight, normalizedOptions);
    }
    result = normalizeTextLayoutResult(result, text, lineHeight);

    if (textLayoutCache.size >= textLayoutCacheLimit) {
      const oldest = textLayoutCache.keys().next();
      if (!oldest.done) {
        textLayoutCache.delete(oldest.value);
      }
    }
    textLayoutCache.set(cacheKey, result);
    return result;
  }

  function gosxTextLayoutMetrics(text, font, maxWidth, whiteSpace, lineHeight, options) {
    const normalizedOptions = normalizeTextLayoutRunOptions(options);
    const cacheKey = textLayoutMetricsCacheKey(text, font, maxWidth, whiteSpace, lineHeight, normalizedOptions);
    if (textLayoutMetricsCache.has(cacheKey)) {
      return textLayoutMetricsCache.get(cacheKey);
    }

    let result = null;
    if (typeof textLayoutMetricsExternalImpl === "function") {
      try {
        result = textLayoutMetricsExternalImpl(text, font, maxWidth, whiteSpace, lineHeight, normalizedOptions);
      } catch (error) {
        console.error("[gosx] text layout metrics implementation failed:", error);
      }
    }
    if (!result || typeof result !== "object") {
      const ranges = gosxTextLayoutRanges(text, font, maxWidth, whiteSpace, lineHeight, normalizedOptions);
      result = {
        lineCount: ranges.lineCount,
        height: ranges.height,
        maxLineWidth: ranges.maxLineWidth,
        byteLen: ranges.byteLen,
        runeCount: ranges.runeCount,
        truncated: ranges.truncated,
      };
    } else {
      const normalized = normalizeTextLayoutResult({
        lineCount: result.lineCount,
        height: result.height,
        maxLineWidth: result.maxLineWidth,
        byteLen: result.byteLen,
        runeCount: result.runeCount,
        truncated: result.truncated,
        lines: [],
      }, text, lineHeight);
      result = {
        lineCount: normalized.lineCount,
        height: normalized.height,
        maxLineWidth: normalized.maxLineWidth,
        byteLen: normalized.byteLen,
        runeCount: normalized.runeCount,
        truncated: normalized.truncated,
      };
    }

    if (textLayoutMetricsCache.size >= textLayoutMetricsCacheLimit) {
      const oldest = textLayoutMetricsCache.keys().next();
      if (!oldest.done) {
        textLayoutMetricsCache.delete(oldest.value);
      }
    }
    textLayoutMetricsCache.set(cacheKey, result);
    return result;
  }

  function gosxTextLayoutRanges(text, font, maxWidth, whiteSpace, lineHeight, options) {
    const normalizedOptions = normalizeTextLayoutRunOptions(options);
    const cacheKey = textLayoutRangesCacheKey(text, font, maxWidth, whiteSpace, lineHeight, normalizedOptions);
    if (textLayoutRangesCache.has(cacheKey)) {
      return textLayoutRangesCache.get(cacheKey);
    }

    let result = null;
    if (typeof textLayoutRangesExternalImpl === "function") {
      try {
        result = textLayoutRangesExternalImpl(text, font, maxWidth, whiteSpace, lineHeight, normalizedOptions);
      } catch (error) {
        console.error("[gosx] text layout ranges implementation failed:", error);
      }
    }
    if (!result || !Array.isArray(result.lines)) {
      result = layoutBrowserTextRanges(text, font, maxWidth, whiteSpace, lineHeight, normalizedOptions);
    }
    result = normalizeTextLayoutRangeResult(result, text, lineHeight);

    if (textLayoutRangesCache.size >= textLayoutRangesCacheLimit) {
      const oldest = textLayoutRangesCache.keys().next();
      if (!oldest.done) {
        textLayoutRangesCache.delete(oldest.value);
      }
    }
    textLayoutRangesCache.set(cacheKey, result);
    return result;
  }

  window.__gosx_text_layout = gosxTextLayout;
  window.__gosx_text_layout_metrics = gosxTextLayoutMetrics;
  window.__gosx_text_layout_ranges = gosxTextLayoutRanges;
  installTextLayoutFontObserver();
  installTextLayoutSurfaceStyles();

  function textLayoutNumberValue(value, fallback) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : fallback;
  }

  function textLayoutLengthValue(value, fallback) {
    if (typeof value === "number" && Number.isFinite(value)) {
      return value;
    }
    if (typeof value === "string") {
      const trimmed = value.trim().toLowerCase();
      if (!trimmed || trimmed === "none" || trimmed === "auto" || trimmed === "normal" || trimmed === "unset") {
        return fallback;
      }
      const parsed = Number.parseFloat(trimmed);
      return Number.isFinite(parsed) ? parsed : fallback;
    }
    return textLayoutNumberValue(value, fallback);
  }

  function textLayoutComputedStyle(element) {
    if (!element || typeof window.getComputedStyle !== "function") {
      return null;
    }
    try {
      return window.getComputedStyle(element);
    } catch (_error) {
      return null;
    }
  }

  function textLayoutComputedStyleValue(style, propertyName) {
    if (!style || !propertyName) {
      return "";
    }
    if (typeof style.getPropertyValue === "function") {
      const propertyValue = style.getPropertyValue(propertyName);
      if (typeof propertyValue === "string" && propertyValue.trim() !== "") {
        return propertyValue.trim();
      }
    }
    const camelName = propertyName.replace(/-([a-z])/g, function(_match, letter) {
      return String(letter || "").toUpperCase();
    });
    const value = style[camelName];
    return typeof value === "string" ? value.trim() : "";
  }

  function textLayoutComputedLineHeight(style, fallback) {
    const explicit = textLayoutLengthValue(
      textLayoutComputedStyleValue(style, "--gosx-text-layout-line-height")
      || textLayoutComputedStyleValue(style, "line-height"),
      NaN
    );
    if (Number.isFinite(explicit) && explicit > 0) {
      return explicit;
    }
    const fontSize = textLayoutLengthValue(textLayoutComputedStyleValue(style, "font-size"), fallback);
    return Math.max(1, fontSize * 1.35);
  }

  function textLayoutLogicalInlineSize(width, height, writingMode) {
    return textLayoutIsVerticalWritingMode(writingMode) ? Math.max(0, sceneNumber(height, 0)) : Math.max(0, sceneNumber(width, 0));
  }

  function textLayoutLogicalBlockSize(width, height, writingMode) {
    return textLayoutIsVerticalWritingMode(writingMode) ? Math.max(0, sceneNumber(width, 0)) : Math.max(0, sceneNumber(height, 0));
  }

  function textLayoutComputedMaxLines(style) {
    return Math.max(0, Math.floor(textLayoutLengthValue(
      textLayoutComputedStyleValue(style, "--gosx-text-layout-max-lines")
      || textLayoutComputedStyleValue(style, "-webkit-line-clamp")
      || textLayoutComputedStyleValue(style, "line-clamp"),
      0
    )));
  }

  function setStyleValue(style, name, value) {
    if (!style || typeof name !== "string") {
      return;
    }
    if (typeof style.setProperty === "function") {
      style.setProperty(name, String(value));
      return;
    }
    style[name] = String(value);
  }

  function clearStyleValue(style, name) {
    if (!style || typeof name !== "string") {
      return;
    }
    if (typeof style.removeProperty === "function") {
      style.removeProperty(name);
      return;
    }
    delete style[name];
  }

  function setAttrValue(element, name, value) {
    if (!element || typeof element.setAttribute !== "function" || typeof name !== "string" || !name) {
      return;
    }
    if (value == null || value === "") {
      if (typeof element.removeAttribute === "function") {
        element.removeAttribute(name);
      }
      return;
    }
    element.setAttribute(name, String(value));
  }

  function installTextLayoutSurfaceStyles() {
    if (!document || !document.head || typeof document.createElement !== "function") {
      return;
    }
    const headChildren = document.head.children || document.head.childNodes || [];
    for (const child of headChildren) {
      if (hasAttributeName(child, TEXT_LAYOUT_STYLE_ATTR)) {
        return;
      }
    }

    const style = document.createElement("style");
    style.setAttribute(TEXT_LAYOUT_STYLE_ATTR, "true");
    style.setAttribute("data-gosx-css-layer", "runtime");
    style.setAttribute("data-gosx-css-owner", "gosx-bootstrap");
    style.setAttribute("data-gosx-css-source", "gosx-runtime");
    style.textContent = [
      '[data-gosx-scene3d-mounted="true"] {',
      '  position: relative;',
      '  max-inline-size: 100%;',
      '  contain: layout paint style;',
      '}',
      '[data-gosx-scene3d-mounted="true"][data-gosx-scene3d-active="true"] {',
      '  --gosx-scene-active: 1;',
      '}',
      '[data-gosx-scene3d-mounted="true"][data-gosx-scene3d-active="false"] {',
      '  --gosx-scene-active: 0;',
      '}',
      '[data-gosx-scene3d-mounted="true"][data-gosx-scene3d-reduced-motion="true"] {',
      '  --gosx-scene-reduced-motion: 1;',
      '}',
      '[data-gosx-scene3d-canvas="true"] {',
      '  display: block;',
      '  inline-size: 100%;',
      '  block-size: auto;',
      '  max-inline-size: 100%;',
      '  border-radius: inherit;',
      '}',
      '[data-gosx-text-layout-role="block"] {',
      '  box-sizing: border-box;',
      '  min-block-size: var(--gosx-text-layout-height, auto);',
      '  white-space: var(--gosx-text-layout-white-space-mode, normal);',
      '  text-align: var(--gosx-text-layout-align, start);',
      '}',
      '[data-gosx-text-layout-role="block"][data-gosx-text-layout-max-width] {',
      '  max-inline-size: min(100%, var(--gosx-text-layout-max-width, 100%));',
      '}',
      '[data-gosx-text-layout-role="block"][data-gosx-text-layout-state="hint"] {',
      '  min-block-size: var(--gosx-text-layout-height-hint, var(--gosx-text-layout-height, auto));',
      '}',
      '[data-gosx-text-layout-role="block"][data-gosx-text-layout-state="truncated"] {',
      '  --gosx-text-layout-is-truncated: 1;',
      '}',
      '[data-gosx-text-layout-role="block"][data-gosx-text-layout-max-lines] {',
      '  overflow: hidden;',
      '}',
      '[data-gosx-text-layout-role="block"][data-gosx-text-layout-max-lines][data-gosx-text-layout-overflow="clip"] {',
      '  max-block-size: var(--gosx-text-layout-max-height, none);',
      '}',
      '[data-gosx-text-layout-role="block"][data-gosx-text-layout-max-lines][data-gosx-text-layout-overflow="ellipsis"] {',
      '  display: -webkit-box;',
      '  -webkit-box-orient: vertical;',
      '  -webkit-line-clamp: var(--gosx-text-layout-max-lines);',
      '}',
      '[data-gosx-scene3d-label-layer="true"] {',
      '  position: absolute;',
      '  inset: 0;',
      '  pointer-events: none;',
      '  overflow: hidden;',
      '  border-radius: inherit;',
      '}',
      '[data-gosx-scene-label] {',
      '  position: absolute;',
      '  box-sizing: border-box;',
      '  pointer-events: none;',
      '  left: var(--gosx-scene-label-left, 0px);',
      '  top: var(--gosx-scene-label-top, 0px);',
      '  width: var(--gosx-scene-label-width, auto);',
      '  max-width: var(--gosx-scene-label-max-width, none);',
      '  min-block-size: var(--gosx-scene-label-height, auto);',
      '  padding: 8px 10px;',
      '  border-radius: var(--gosx-scene-label-radius, 12px);',
      '  border: 1px solid var(--gosx-scene-label-border-color, var(--color-scene-line, rgba(141, 225, 255, 0.24)));',
      '  background: var(--gosx-scene-label-background, var(--color-scene-surface-glass-strong, rgba(8, 21, 31, 0.82)));',
      '  color: var(--gosx-scene-label-color, var(--color-ink-inverse, #ecf7ff));',
      '  font: var(--gosx-scene-label-font, 600 13px "IBM Plex Sans", "Segoe UI", sans-serif);',
      '  line-height: var(--gosx-scene-label-line-height, 18px);',
      '  text-align: var(--gosx-scene-label-align, center);',
      '  box-shadow: 0 14px 32px var(--gosx-scene-label-shadow, rgba(3, 10, 16, 0.28));',
      '  backdrop-filter: blur(var(--gosx-scene-label-blur, 10px));',
      '  -webkit-backdrop-filter: blur(var(--gosx-scene-label-blur, 10px));',
      '  will-change: left, top, transform;',
      '  z-index: var(--gosx-scene-label-z-index, 1);',
      '  opacity: var(--gosx-scene-label-opacity, 1);',
      '  transform: translate(calc(var(--gosx-scene-label-anchor-x, 0.5) * -100%), calc(var(--gosx-scene-label-anchor-y, 1) * -100%));',
      '  transition:',
      '    opacity var(--motion-fast, 180ms) var(--ease-out, cubic-bezier(0.16, 1, 0.3, 1)),',
      '    transform var(--motion-fast, 180ms) var(--ease-out, cubic-bezier(0.16, 1, 0.3, 1));',
      '}',
      '[data-gosx-scene-label][data-gosx-scene-label-visibility="hidden"] {',
      '  opacity: 0;',
      '  visibility: hidden;',
      '}',
      '[data-gosx-scene-label][data-gosx-scene-label-occluded="true"] {',
      '  --gosx-scene-label-opacity: 0.52;',
      '}',
      '[data-gosx-scene3d-mounted="true"][data-gosx-scene3d-active="false"] [data-gosx-scene-label] {',
      '  transition: none;',
      '}',
      '[data-gosx-scene3d-mounted="true"][data-gosx-scene3d-reduced-motion="true"] [data-gosx-scene-label] {',
      '  transition: none;',
      '}',
      '[data-gosx-scene-label-line] {',
      '  display: block;',
      '  white-space: var(--gosx-scene-label-white-space, normal);',
      '}',
    ].join("\\n");
    document.head.appendChild(style);
  }

  function applyTextLayoutPresentation(element, config, result, meta) {
    if (!element) {
      return;
    }
    const options = meta && typeof meta === "object" ? meta : {};
    const role = typeof options.role === "string" && options.role ? options.role : "block";
    const surface = typeof options.surface === "string" && options.surface ? options.surface : "dom";
    const state = typeof options.state === "string" && options.state ? options.state : (result ? "ready" : "pending");
    const align = normalizeTextLayoutAlign(options.align || (config && config.align));
    const revision = Number.isFinite(options.revision) ? options.revision : gosxTextLayoutRevision();
    const font = config && typeof config.font === "string" ? config.font : "";
    const locale = normalizeTextLayoutLocale(config && config.locale);
    const direction = config && typeof config.direction === "string" ? String(config.direction).trim().toLowerCase() : "";
    const writingMode = normalizeTextLayoutWritingMode(config && config.writingMode);
    const whiteSpace = normalizeTextLayoutWhiteSpace(config && config.whiteSpace);
    const lineHeight = Math.max(1, textLayoutNumberValue(config && config.lineHeight, 16));
    const maxLines = Math.max(0, Math.floor(textLayoutNumberValue(config && config.maxLines, 0)));
    const overflow = normalizeTextLayoutOverflow(config && config.overflow);
    const maxWidth = textLayoutNumberValue(config && config.maxWidth, 0);
    const inlineSize = Math.max(0, textLayoutNumberValue(config && config.inlineSize, 0));
    const blockSize = Math.max(0, textLayoutNumberValue(config && config.blockSize, 0));
    const ready = Boolean(result);
    const effectiveState = ready && result && result.truncated ? "truncated" : state;

    setAttrValue(element, TEXT_LAYOUT_ROLE_ATTR, role);
    setAttrValue(element, TEXT_LAYOUT_SURFACE_ATTR, surface);
    setAttrValue(element, TEXT_LAYOUT_STATE_ATTR, effectiveState);
    setAttrValue(element, "data-gosx-text-layout-ready", ready ? "true" : "false");
    setAttrValue(element, "data-gosx-text-layout-font", font);
    setAttrValue(element, "data-gosx-text-layout-locale", locale);
    setAttrValue(element, "data-gosx-text-layout-direction", direction);
    setAttrValue(element, "data-gosx-text-layout-writing-mode", writingMode);
    setAttrValue(element, "data-gosx-text-layout-white-space", whiteSpace === "normal" ? "" : whiteSpace);
    setAttrValue(element, "data-gosx-text-layout-align", align);
    setAttrValue(element, "data-gosx-text-layout-line-height", lineHeight > 0 ? lineHeight : "");
    setAttrValue(element, "data-gosx-text-layout-max-lines", maxLines > 0 ? maxLines : "");
    setAttrValue(element, "data-gosx-text-layout-overflow", maxLines > 0 ? overflow : "");
    setAttrValue(element, "data-gosx-text-layout-revision", revision);
    setAttrValue(element, "data-gosx-text-layout-inline-size", inlineSize > 0 ? inlineSize : "");
    setAttrValue(element, "data-gosx-text-layout-block-size", blockSize > 0 ? blockSize : "");

    setStyleValue(element.style, "--gosx-text-layout-ready", ready ? "1" : "0");
    setStyleValue(element.style, "--gosx-text-layout-line-height", lineHeight + "px");
    setStyleValue(element.style, "--gosx-text-layout-white-space-mode", whiteSpace);
    if (direction) {
      setStyleValue(element.style, "--gosx-text-layout-direction", direction);
    } else {
      clearStyleValue(element.style, "--gosx-text-layout-direction");
    }
    if (writingMode) {
      setStyleValue(element.style, "--gosx-text-layout-writing-mode", writingMode);
    } else {
      clearStyleValue(element.style, "--gosx-text-layout-writing-mode");
    }
    if (inlineSize > 0) {
      setStyleValue(element.style, "--gosx-text-layout-inline-size", inlineSize + "px");
    } else {
      clearStyleValue(element.style, "--gosx-text-layout-inline-size");
    }
    if (blockSize > 0) {
      setStyleValue(element.style, "--gosx-text-layout-block-size", blockSize + "px");
    } else {
      clearStyleValue(element.style, "--gosx-text-layout-block-size");
    }
    if (align) {
      setStyleValue(element.style, "--gosx-text-layout-align", align);
    } else {
      clearStyleValue(element.style, "--gosx-text-layout-align");
    }
    if (maxLines > 0) {
      setStyleValue(element.style, "--gosx-text-layout-max-lines", String(maxLines));
      setStyleValue(element.style, "--gosx-text-layout-max-height", (lineHeight * maxLines) + "px");
    } else {
      clearStyleValue(element.style, "--gosx-text-layout-max-lines");
      clearStyleValue(element.style, "--gosx-text-layout-max-height");
    }
    setStyleValue(element.style, "--gosx-text-layout-overflow", overflow);
    if (maxWidth > 0 && maxWidth < Number.MAX_SAFE_INTEGER) {
      setAttrValue(element, "data-gosx-text-layout-max-width", maxWidth);
      setAttrValue(element, "data-gosx-text-layout-max-inline-size", maxWidth);
      setStyleValue(element.style, "--gosx-text-layout-width", maxWidth + "px");
      setStyleValue(element.style, "--gosx-text-layout-max-width", maxWidth + "px");
      setStyleValue(element.style, "--gosx-text-layout-max-inline-size", maxWidth + "px");
    } else {
      setAttrValue(element, "data-gosx-text-layout-max-width", "");
      setAttrValue(element, "data-gosx-text-layout-max-inline-size", "");
      clearStyleValue(element.style, "--gosx-text-layout-width");
      clearStyleValue(element.style, "--gosx-text-layout-max-width");
      clearStyleValue(element.style, "--gosx-text-layout-max-inline-size");
    }

    if (!result || typeof result !== "object") {
      setAttrValue(element, "data-gosx-text-layout-truncated", "");
      clearStyleValue(element.style, "--gosx-text-layout-truncated");
      return;
    }

    setAttrValue(element, "data-gosx-text-layout-line-count", result.lineCount);
    setAttrValue(element, "data-gosx-text-layout-height", result.height);
    setAttrValue(element, "data-gosx-text-layout-max-line-width", result.maxLineWidth);
    setAttrValue(element, "data-gosx-text-layout-byte-length", result.byteLen);
    setAttrValue(element, "data-gosx-text-layout-rune-count", result.runeCount);
    setAttrValue(element, "data-gosx-text-layout-truncated", result.truncated ? "true" : "false");

    setStyleValue(element.style, "--gosx-text-layout-height", result.height + "px");
    setStyleValue(element.style, "--gosx-text-layout-line-count", String(result.lineCount));
    setStyleValue(element.style, "--gosx-text-layout-max-line-width", result.maxLineWidth + "px");
    setStyleValue(element.style, "--gosx-text-layout-byte-length", String(result.byteLen));
    setStyleValue(element.style, "--gosx-text-layout-rune-count", String(result.runeCount));
    setStyleValue(element.style, "--gosx-text-layout-truncated", result.truncated ? "1" : "0");
  }

  function textLayoutElementID(element) {
    if (!element || typeof element.getAttribute !== "function") {
      return "";
    }
    const existing = element.getAttribute(TEXT_LAYOUT_ID_ATTR);
    if (existing) {
      return existing;
    }
    const derived = element.id ? ("gosx-text-layout:" + element.id) : ("gosx-text-layout-" + (++nextManagedTextLayoutID));
    if (typeof element.setAttribute === "function") {
      element.setAttribute(TEXT_LAYOUT_ID_ATTR, derived);
    }
    return derived;
  }

  function walkElementTree(root, visit) {
    if (!root) {
      return;
    }
    if (root.nodeType === 1) {
      visit(root);
    }
    const children = root.children || root.childNodes || [];
    for (const child of children) {
      if (child && child.nodeType === 1) {
        walkElementTree(child, visit);
      }
    }
  }

  function collectManagedTextLayoutElements(root) {
    const elements = [];
    walkElementTree(root, function(element) {
      if (hasAttributeName(element, TEXT_LAYOUT_ATTR)) {
        elements.push(element);
      }
    });
    return elements;
  }

  function normalizeManagedTextLayoutConfig(element, options) {
    const config = options && typeof options === "object" ? options : {};
    const hasOwn = Object.prototype.hasOwnProperty;
    const presentation = window.__gosx.presentation && typeof window.__gosx.presentation.read === "function"
      ? window.__gosx.presentation.read(element)
      : null;
    const computed = presentation && presentation.style ? presentation.style : textLayoutComputedStyle(element);
    const locale = normalizeTextLayoutLocale(
      hasOwn.call(config, "locale")
        ? config.locale
        : (
          textLayoutComputedStyleValue(computed, "--gosx-text-layout-locale")
          || (presentation && presentation.lang)
          || (element.getAttribute && element.getAttribute("data-gosx-text-layout-locale"))
          || (element.getAttribute && element.getAttribute("lang"))
        )
    );
    const direction = (function() {
      const value = hasOwn.call(config, "direction")
        ? config.direction
        : (
          textLayoutComputedStyleValue(computed, "--gosx-text-layout-direction")
          || (presentation && presentation.direction)
          || textLayoutComputedStyleValue(computed, "direction")
          || (element.getAttribute && element.getAttribute("data-gosx-text-layout-direction"))
          || (element.getAttribute && element.getAttribute("dir"))
        );
      switch (String(value || "").trim().toLowerCase()) {
        case "ltr":
        case "rtl":
        case "auto":
          return String(value).trim().toLowerCase();
        default:
          return "";
      }
    })();
    const writingMode = normalizeTextLayoutWritingMode(
      hasOwn.call(config, "writingMode")
        ? config.writingMode
        : (
          textLayoutComputedStyleValue(computed, "--gosx-text-layout-writing-mode")
          || (presentation && presentation.writingMode)
          || textLayoutComputedStyleValue(computed, "writing-mode")
        )
    );
    const inlineSize = Math.max(0, textLayoutLengthValue(
      hasOwn.call(config, "inlineSize")
        ? config.inlineSize
        : (presentation && presentation.inlineSize),
      0
    ));
    const blockSize = Math.max(0, textLayoutLengthValue(
      hasOwn.call(config, "blockSize")
        ? config.blockSize
        : (presentation && presentation.blockSize),
      0
    ));
    const font = hasOwn.call(config, "font")
      ? String(config.font == null ? "" : config.font)
      : String(
        textLayoutComputedStyleValue(computed, "--gosx-text-layout-font")
        || textLayoutComputedStyleValue(computed, "font")
        || (element.getAttribute && element.getAttribute("data-gosx-text-layout-font"))
        || ""
      );
    const align = normalizeTextLayoutAlign(
      hasOwn.call(config, "align")
        ? config.align
        : (
          textLayoutComputedStyleValue(computed, "--gosx-text-layout-align")
          || (presentation && presentation.textAlign)
          || textLayoutComputedStyleValue(computed, "text-align")
          || (element.getAttribute && element.getAttribute("data-gosx-text-layout-align"))
          || (element.getAttribute && element.getAttribute("align"))
        )
    );
    const whiteSpace = normalizeTextLayoutWhiteSpace(
      hasOwn.call(config, "whiteSpace")
        ? config.whiteSpace
        : (
          textLayoutComputedStyleValue(computed, "--gosx-text-layout-white-space")
          || (presentation && presentation.whiteSpace)
          || textLayoutComputedStyleValue(computed, "white-space")
          || (element.getAttribute && element.getAttribute("data-gosx-text-layout-white-space"))
        )
    );
    const lineHeight = Math.max(1, textLayoutLengthValue(
      hasOwn.call(config, "lineHeight")
        ? config.lineHeight
        : (
          (presentation && presentation.lineHeight)
          || textLayoutComputedLineHeight(computed, NaN)
          || (element.getAttribute && element.getAttribute("data-gosx-text-layout-line-height"))
          || 16
        ),
      16
    ));
    const maxLines = Math.max(0, Math.floor(textLayoutNumberValue(
      hasOwn.call(config, "maxLines")
        ? config.maxLines
        : (
          textLayoutComputedMaxLines(computed)
          || (element.getAttribute && element.getAttribute("data-gosx-text-layout-max-lines"))
        ),
      0
    )));
    let maxWidth = textLayoutLengthValue(
      hasOwn.call(config, "maxWidth")
        ? config.maxWidth
        : (
          textLayoutComputedStyleValue(computed, "--gosx-text-layout-max-inline-size")
          || textLayoutComputedStyleValue(computed, "max-inline-size")
          || textLayoutComputedStyleValue(computed, "--gosx-text-layout-max-width")
          || (presentation && presentation.maxInlineSize)
          || (presentation && presentation.maxWidth)
          || textLayoutComputedStyleValue(computed, "max-width")
          || (element.getAttribute && element.getAttribute("data-gosx-text-layout-max-width"))
        ),
      0
    );
    if (!(maxWidth > 0) && presentation) {
      maxWidth = textLayoutNumberValue(presentation.inlineSize, 0);
    }
    if (!(maxWidth > 0) && inlineSize > 0) {
      maxWidth = inlineSize;
    }
    if (!(maxWidth > 0) && element && typeof element.getBoundingClientRect === "function") {
      const rect = element.getBoundingClientRect();
      maxWidth = textLayoutLogicalInlineSize(rect && rect.width, rect && rect.height, writingMode);
    }
    if (!(maxWidth > 0) && element) {
      maxWidth = textLayoutLogicalInlineSize(
        element.clientWidth || element.offsetWidth || element.width,
        element.clientHeight || element.offsetHeight || element.height,
        writingMode
      );
    }
    if (!(maxWidth > 0)) {
      maxWidth = Number.MAX_SAFE_INTEGER;
    }
    const observe = hasOwn.call(config, "observe")
      ? Boolean(config.observe)
      : String((element.getAttribute && element.getAttribute("data-gosx-text-layout-observe")) || "true").toLowerCase() !== "false";
    const sourceText = hasOwn.call(config, "text")
      ? String(config.text == null ? "" : config.text)
      : String((element.getAttribute && element.getAttribute("data-gosx-text-layout-source")) || element.textContent || "");

    return {
      font,
      whiteSpace,
      align,
      lineHeight,
      maxLines,
      locale,
      direction,
      writingMode,
      inlineSize,
      blockSize,
      overflow: normalizeTextLayoutOverflow(
        hasOwn.call(config, "overflow")
          ? config.overflow
          : (
            textLayoutComputedStyleValue(computed, "--gosx-text-layout-overflow")
            || (textLayoutComputedStyleValue(computed, "text-overflow") === "ellipsis" ? "ellipsis" : "")
            || (element.getAttribute && element.getAttribute("data-gosx-text-layout-overflow"))
          )
      ),
      maxWidth,
      observe,
      text: sourceText,
      heightHint: Math.max(0, textLayoutNumberValue(element.getAttribute && element.getAttribute("data-gosx-text-layout-height-hint"), 0)),
      lineCountHint: Math.max(0, Math.floor(textLayoutNumberValue(element.getAttribute && element.getAttribute("data-gosx-text-layout-line-count-hint"), 0))),
    };
  }

  function applyManagedTextLayoutHint(element, config) {
    if (!element) {
      return;
    }
    applyTextLayoutPresentation(element, config, null, {
      role: "block",
      surface: "dom",
      state: (config.heightHint > 0 || config.lineCountHint > 0) ? "hint" : "pending",
      revision: gosxTextLayoutRevision(),
    });
    if (config.heightHint > 0) {
      setStyleValue(element.style, "--gosx-text-layout-height-hint", config.heightHint + "px");
      setStyleValue(element.style, "--gosx-text-layout-height", config.heightHint + "px");
      setAttrValue(element, "data-gosx-text-layout-height-hint", config.heightHint);
    }
    if (config.lineCountHint > 0) {
      setStyleValue(element.style, "--gosx-text-layout-line-count-hint", String(config.lineCountHint));
      setStyleValue(element.style, "--gosx-text-layout-line-count", String(config.lineCountHint));
      setAttrValue(element, "data-gosx-text-layout-line-count-hint", config.lineCountHint);
    }
  }

  function dispatchManagedTextLayoutEvent(record, reason) {
    if (!record || typeof CustomEvent !== "function") {
      return;
    }
    const detail = {
      id: record.id,
      element: record.element,
      reason: reason || "refresh",
      revision: gosxTextLayoutRevision(),
      config: record.config,
      result: record.result,
    };
    const event = new CustomEvent("gosx:textlayout", { detail });
    if (record.element && typeof record.element.dispatchEvent === "function") {
      record.element.dispatchEvent(event);
    }
    if (typeof document.dispatchEvent === "function") {
      document.dispatchEvent(new CustomEvent("gosx:textlayout", { detail }));
    }
  }

  function applyManagedTextLayoutResult(record, config, result, reason) {
    const element = record.element;
    record.config = config;
    record.result = result;
    if (!element) {
      return result;
    }
    applyTextLayoutPresentation(element, config, result, {
      role: "block",
      surface: "dom",
      state: "ready",
      revision: gosxTextLayoutRevision(),
    });
    element.__gosxTextLayout = result;

    if (typeof record.onUpdate === "function") {
      try {
        record.onUpdate(result, config);
      } catch (error) {
        console.error("[gosx] text layout onUpdate failed:", error);
      }
    }

    window.__gosx.textLayouts.set(record.id, {
      element,
      config,
      result,
    });
    dispatchManagedTextLayoutEvent(record, reason);
    return result;
  }

  function refreshManagedTextLayoutRecord(record, reason) {
    if (!record || !record.element) {
      return null;
    }
    const config = normalizeManagedTextLayoutConfig(record.element, record.options);
    const layoutKey = [
      gosxTextLayoutRevision(),
      config.text,
      config.font,
      config.locale,
      config.direction,
      config.writingMode,
      config.align,
      config.whiteSpace,
      config.lineHeight,
      config.maxLines,
      config.overflow,
      config.maxWidth,
      config.inlineSize,
      config.blockSize,
    ].join("\n");
    if (layoutKey === record.layoutKey && record.result) {
      return record.result;
    }
    record.layoutKey = layoutKey;
    const result = gosxTextLayoutRanges(config.text, config.font, config.maxWidth, config.whiteSpace, config.lineHeight, {
      maxLines: config.maxLines,
      overflow: config.overflow,
    });
    return applyManagedTextLayoutResult(record, config, result, reason);
  }

  function disconnectManagedTextLayoutObservers(record) {
    if (!record) {
      return;
    }
    gosxCancelInvalidation(record);
    if (record.resizeObserver && typeof record.resizeObserver.disconnect === "function") {
      record.resizeObserver.disconnect();
      record.resizeObserver = null;
    }
    if (record.mutationObserver && typeof record.mutationObserver.disconnect === "function") {
      record.mutationObserver.disconnect();
      record.mutationObserver = null;
    }
    if (record.windowResizeListener && typeof window.removeEventListener === "function") {
      window.removeEventListener("resize", record.windowResizeListener);
      record.windowResizeListener = null;
    }
    if (typeof record.stopPresentation === "function") {
      record.stopPresentation();
      record.stopPresentation = null;
    }
    if (typeof record.stopInvalidation === "function") {
      record.stopInvalidation();
      record.stopInvalidation = null;
    }
  }

  function scheduleManagedTextLayoutRefresh(record, reason) {
    if (!record) {
      return;
    }
    gosxScheduleVisualInvalidation(record, reason || "textlayout", function(nextReason) {
      refreshManagedTextLayoutRecord(record, nextReason);
    });
  }

  function connectManagedTextLayoutMutationObserver(record) {
    if (!record || record.mutationObserver || typeof MutationObserver !== "function" || !record.element) {
      return;
    }
    record.mutationObserver = new MutationObserver(function(records) {
      for (const mutation of records || []) {
        const target = mutation && mutation.target;
        if (!target || target === record.element || mutation.type !== "attributes") {
          scheduleManagedTextLayoutRefresh(record, "mutation");
          return;
        }
      }
    });
    if (typeof record.mutationObserver.observe === "function") {
      record.mutationObserver.observe(record.element, {
        subtree: true,
        childList: true,
        characterData: true,
        attributes: true,
        attributeFilter: MANAGED_TEXT_LAYOUT_MUTATION_ATTRS,
      });
    }
  }

  function disposeManagedTextLayout(target) {
    let record = null;
    if (typeof target === "string") {
      const current = window.__gosx.textLayouts.get(target);
      if (current && current.element) {
        record = textLayoutRecordsByElement.get(current.element) || null;
      }
    } else if (target) {
      record = textLayoutRecordsByElement.get(target) || null;
    }
    if (!record) {
      return;
    }
    disconnectManagedTextLayoutObservers(record);
    if (record.element) {
      textLayoutRecordsByElement.delete(record.element);
      record.element.__gosxTextLayout = null;
    }
    window.__gosx.textLayouts.delete(record.id);
  }

  function observeManagedTextLayout(element, options) {
    if (!element || typeof element !== "object") {
      return { refresh: function() { return null; }, dispose: function() {} };
    }

    const existing = textLayoutRecordsByElement.get(element);
    if (existing) {
      if (options && typeof options === "object") {
        disposeManagedTextLayout(element);
      } else {
        refreshManagedTextLayoutRecord(existing, "observe");
        return {
          id: existing.id,
          element: existing.element,
          refresh: function(reason) {
            return refreshManagedTextLayoutRecord(existing, reason || "manual");
          },
          read: function() {
            return existing.result;
          },
          dispose: function() {
            disposeManagedTextLayout(existing.element);
          },
        };
      }
    }

    const record = {
      id: textLayoutElementID(element),
      element,
      options: options && typeof options === "object" ? Object.assign({}, options) : {},
      result: null,
      config: null,
      layoutKey: "",
      onUpdate: options && typeof options.onUpdate === "function" ? options.onUpdate : null,
      resizeObserver: null,
      mutationObserver: null,
      windowResizeListener: null,
      stopInvalidation: null,
      stopPresentation: null,
    };

    textLayoutRecordsByElement.set(element, record);
    applyManagedTextLayoutHint(element, normalizeManagedTextLayoutConfig(element, record.options));
    refreshManagedTextLayoutRecord(record, "mount");

    if (record.config && record.config.observe) {
      record.stopInvalidation = onTextLayoutInvalidated(function() {
        scheduleManagedTextLayoutRefresh(record, "invalidate");
      });

      if (window.__gosx.presentation && typeof window.__gosx.presentation.observe === "function") {
        record.stopPresentation = window.__gosx.presentation.observe(element, function() {
          scheduleManagedTextLayoutRefresh(record, "presentation");
        }, { immediate: false });
      } else if (typeof ResizeObserver === "function") {
        record.resizeObserver = new ResizeObserver(function() {
          scheduleManagedTextLayoutRefresh(record, "resize");
        });
        if (typeof record.resizeObserver.observe === "function") {
          record.resizeObserver.observe(element);
        }
      } else if (typeof window.addEventListener === "function") {
        record.windowResizeListener = function() {
          scheduleManagedTextLayoutRefresh(record, "resize");
        };
        window.addEventListener("resize", record.windowResizeListener);
      }
      connectManagedTextLayoutMutationObserver(record);
    }

    return {
      id: record.id,
      element,
      refresh: function(reason) {
        return refreshManagedTextLayoutRecord(record, reason || "manual");
      },
      read: function() {
        return record.result;
      },
      dispose: function() {
        disposeManagedTextLayout(element);
      },
    };
  }

  function mountManagedTextLayouts(root) {
    const targetRoot = root || document.body || document.documentElement;
    const elements = collectManagedTextLayoutElements(targetRoot);
    for (const element of elements) {
      observeManagedTextLayout(element);
    }
  }

  function refreshManagedTextLayouts() {
    for (const snapshot of Array.from(window.__gosx.textLayouts.values())) {
      if (snapshot && snapshot.element) {
        const record = textLayoutRecordsByElement.get(snapshot.element);
        if (record) {
          refreshManagedTextLayoutRecord(record, "refresh");
        }
      }
    }
  }

  function disposeManagedTextLayouts() {
    for (const id of Array.from(window.__gosx.textLayouts.keys())) {
      disposeManagedTextLayout(id);
    }
  }

  window.__gosx.textLayout = {
    layout: gosxTextLayout,
    metrics: gosxTextLayoutMetrics,
    ranges: gosxTextLayoutRanges,
    revision: gosxTextLayoutRevision,
    observe: observeManagedTextLayout,
    mountAll: mountManagedTextLayouts,
    refresh(target) {
      if (target) {
        const handle = observeManagedTextLayout(target);
        return handle.refresh("manual");
      }
      refreshManagedTextLayouts();
      return null;
    },
    read(element) {
      const record = element ? textLayoutRecordsByElement.get(element) : null;
      if (record) {
        return record.result;
      }
      return element && element.__gosxTextLayout ? element.__gosxTextLayout : null;
    },
    dispose: disposeManagedTextLayout,
  };

  const gosxEnvironmentListeners = new Set();
  const gosxDocumentListeners = new Set();
  const gosxPresentationRecordsByElement = new Map();
  let gosxEnvironmentState = null;
  let gosxDocumentState = null;
  let gosxEnvironmentObserversInstalled = false;
  let gosxDocumentObserversInstalled = false;
  const GOSX_DOCUMENT_CSS_LAYERS = ["global", "layout", "page", "runtime"];
  const GOSX_DOCUMENT_ENHANCEMENT_LAYERS = ["html", "bootstrap", "runtime"];
  const gosxStateInvalidations = new Map();
  const gosxVisualInvalidations = new Map();
  let gosxStateInvalidationScheduled = false;
  let gosxVisualInvalidationScheduled = false;
  let gosxVisualInvalidationHandle = 0;
  const GOSX_MOTION_ATTR = "data-gosx-motion";
  const GOSX_MOTION_MUTATION_ATTRS = [
    GOSX_MOTION_ATTR,
    "data-gosx-motion-preset",
    "data-gosx-motion-trigger",
    "data-gosx-motion-duration",
    "data-gosx-motion-delay",
    "data-gosx-motion-easing",
    "data-gosx-motion-distance",
    "data-gosx-motion-respect-reduced",
  ];
  const gosxManagedMotionRecordsByElement = new Map();
  let gosxManagedMotionObserver = null;
  let gosxManagedMotionNextID = 0;
  let gosxPresentationResizeObserver = null;
  let gosxPresentationMutationObserver = null;
  let gosxPresentationStopEnvironment = null;
  let gosxPresentationStopDocument = null;
  const GOSX_PRESENTATION_MUTATION_ATTRS = ["class", "style", "dir", "lang", "hidden"];
  const gosxAppliedEnhancementKindAttrs = new Set();

  function gosxArrayFrom(listLike) {
    return Array.prototype.slice.call(listLike || []);
  }

  function gosxMergedReason(current, next) {
    const value = String(next || "").trim();
    if (!value) {
      return current || "";
    }
    if (!current) {
      return value;
    }
    const parts = current.split("|");
    if (parts.includes(value)) {
      return current;
    }
    parts.push(value);
    return parts.join("|");
  }

  function gosxScheduleStateInvalidation(key, reason, callback) {
    if (!key || typeof callback !== "function") {
      return;
    }
    const entry = gosxStateInvalidations.get(key) || { callback: null, reason: "" };
    entry.callback = callback;
    entry.reason = gosxMergedReason(entry.reason, reason || "state");
    gosxStateInvalidations.set(key, entry);
    if (gosxStateInvalidationScheduled) {
      return;
    }
    gosxStateInvalidationScheduled = true;
    const flush = function() {
      gosxStateInvalidationScheduled = false;
      const pending = Array.from(gosxStateInvalidations.values());
      gosxStateInvalidations.clear();
      for (const item of pending) {
        if (!item || typeof item.callback !== "function") {
          continue;
        }
        item.callback(item.reason || "state");
      }
    };
    if (typeof queueMicrotask === "function") {
      queueMicrotask(flush);
      return;
    }
    Promise.resolve().then(flush);
  }

  function gosxScheduleVisualInvalidation(key, reason, callback) {
    if (!key || typeof callback !== "function") {
      return;
    }
    const entry = gosxVisualInvalidations.get(key) || { callback: null, reason: "" };
    entry.callback = callback;
    entry.reason = gosxMergedReason(entry.reason, reason || "visual");
    gosxVisualInvalidations.set(key, entry);
    if (gosxVisualInvalidationScheduled) {
      return;
    }
    gosxVisualInvalidationScheduled = true;
    const flush = function(frameTime) {
      gosxVisualInvalidationScheduled = false;
      gosxVisualInvalidationHandle = 0;
      while (gosxVisualInvalidations.size > 0) {
        const pending = Array.from(gosxVisualInvalidations.values());
        gosxVisualInvalidations.clear();
        for (const item of pending) {
          if (!item || typeof item.callback !== "function") {
            continue;
          }
          item.callback(item.reason || "visual", frameTime);
        }
      }
    };
    if (typeof requestAnimationFrame === "function") {
      gosxVisualInvalidationHandle = requestAnimationFrame(flush);
      return;
    }
    gosxVisualInvalidationHandle = setTimeout(function() {
      flush(Date.now());
    }, 0);
  }

  function gosxCancelInvalidation(key) {
    if (!key) {
      return;
    }
    gosxStateInvalidations.delete(key);
    gosxVisualInvalidations.delete(key);
  }

  function gosxManagedMotionElements(root) {
    const elements = [];
    walkElementTree(root, function(element) {
      if (element && typeof element.hasAttribute === "function" && element.hasAttribute(GOSX_MOTION_ATTR)) {
        elements.push(element);
      }
    });
    return elements;
  }

  function gosxMotionStringAttr(element, name, fallback) {
    const value = element && typeof element.getAttribute === "function"
      ? String(element.getAttribute(name) || "").trim()
      : "";
    return value || fallback;
  }

  function gosxMotionBoolAttr(element, name, fallback) {
    const value = gosxMotionStringAttr(element, name, "");
    if (!value) {
      return fallback;
    }
    switch (value.toLowerCase()) {
      case "false":
      case "0":
      case "off":
      case "no":
        return false;
      default:
        return true;
    }
  }

  function normalizeGosxMotionPreset(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "slide-up":
      case "slide-down":
      case "slide-left":
      case "slide-right":
      case "zoom-in":
        return String(value).trim().toLowerCase();
      default:
        return "fade";
    }
  }

  function normalizeGosxMotionTrigger(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "view":
        return "view";
      default:
        return "load";
    }
  }

  function gosxManagedMotionConfig(element) {
    return {
      preset: normalizeGosxMotionPreset(gosxMotionStringAttr(element, "data-gosx-motion-preset", "fade")),
      trigger: normalizeGosxMotionTrigger(gosxMotionStringAttr(element, "data-gosx-motion-trigger", "load")),
      duration: Math.max(1, Math.round(gosxNumber(gosxMotionStringAttr(element, "data-gosx-motion-duration", 220), 220))),
      delay: Math.max(0, Math.round(gosxNumber(gosxMotionStringAttr(element, "data-gosx-motion-delay", 0), 0))),
      easing: gosxMotionStringAttr(element, "data-gosx-motion-easing", "cubic-bezier(0.16, 1, 0.3, 1)"),
      distance: Math.max(0, gosxNumber(gosxMotionStringAttr(element, "data-gosx-motion-distance", 18), 18)),
      respectReducedMotion: gosxMotionBoolAttr(element, "data-gosx-motion-respect-reduced", true),
    };
  }

  function gosxManagedMotionReduced(config) {
    if (!config || !config.respectReducedMotion) {
      return false;
    }
    const environment = gosxEnvironmentState || refreshGosxEnvironmentState("motion");
    return Boolean(environment && environment.reducedMotion);
  }

  function gosxManagedMotionKeyframes(config) {
    const distance = Math.max(0, gosxNumber(config && config.distance, 18));
    switch (config && config.preset) {
      case "slide-up":
        return [
          { opacity: 0, transform: "translate3d(0, " + distance + "px, 0)" },
          { opacity: 1, transform: "translate3d(0, 0, 0)" },
        ];
      case "slide-down":
        return [
          { opacity: 0, transform: "translate3d(0, -" + distance + "px, 0)" },
          { opacity: 1, transform: "translate3d(0, 0, 0)" },
        ];
      case "slide-left":
        return [
          { opacity: 0, transform: "translate3d(" + distance + "px, 0, 0)" },
          { opacity: 1, transform: "translate3d(0, 0, 0)" },
        ];
      case "slide-right":
        return [
          { opacity: 0, transform: "translate3d(-" + distance + "px, 0, 0)" },
          { opacity: 1, transform: "translate3d(0, 0, 0)" },
        ];
      case "zoom-in": {
        const scale = Math.max(0.72, 1 - (Math.min(distance, 64) / 200));
        return [
          { opacity: 0, transform: "scale(" + scale + ")" },
          { opacity: 1, transform: "scale(1)" },
        ];
      }
      default:
        return [
          { opacity: 0 },
          { opacity: 1 },
        ];
    }
  }

  function gosxManagedMotionState(record, state) {
    if (!record || !record.element) {
      return;
    }
    setAttrValue(record.element, "data-gosx-motion-state", state || "idle");
  }

  function cancelManagedMotionAnimation(record) {
    if (!record || !record.animation || typeof record.animation.cancel !== "function") {
      record.animation = null;
      return;
    }
    try {
      record.animation.cancel();
    } catch (_error) {
    }
    record.animation = null;
  }

  function playManagedMotion(record, reason) {
    if (!record || !record.element || record.played) {
      return;
    }
    record.config = gosxManagedMotionConfig(record.element);
    if (gosxManagedMotionReduced(record.config)) {
      record.played = true;
      gosxManagedMotionState(record, "reduced");
      return;
    }
    record.played = true;
    gosxManagedMotionState(record, "running");
    cancelManagedMotionAnimation(record);
    if (typeof record.element.animate !== "function") {
      gosxManagedMotionState(record, "finished");
      return;
    }
    const animation = record.element.animate(gosxManagedMotionKeyframes(record.config), {
      duration: record.config.duration,
      delay: record.config.delay,
      easing: record.config.easing,
      fill: "both",
    });
    record.animation = animation || null;
    if (animation && animation.finished && typeof animation.finished.then === "function") {
      animation.finished.then(function() {
        if (record.animation !== animation) {
          return;
        }
        record.animation = null;
        gosxManagedMotionState(record, "finished");
      }, function() {
        if (record.animation !== animation) {
          return;
        }
        record.animation = null;
        gosxManagedMotionState(record, "idle");
      });
      return;
    }
    gosxManagedMotionState(record, "finished");
  }

  function connectManagedMotion(record) {
    if (!record || !record.element) {
      return;
    }
    if (record.stopTrigger) {
      record.stopTrigger();
      record.stopTrigger = null;
    }
    if (record.config.trigger === "view" && typeof IntersectionObserver === "function") {
      const observer = new IntersectionObserver(function(entries) {
        for (const entry of entries || []) {
          if (!entry || entry.target !== record.element) {
            continue;
          }
          if (entry.isIntersecting !== false && gosxNumber(entry.intersectionRatio, 1) > 0) {
            playManagedMotion(record, "view");
          }
        }
      }, { threshold: [0, 0.01, 0.2] });
      observer.observe(record.element);
      record.stopTrigger = function() {
        observer.disconnect();
      };
      return;
    }
    gosxScheduleVisualInvalidation(record.key, "motion-load", function(nextReason) {
      playManagedMotion(record, nextReason || "load");
    });
  }

  function observeManagedMotion(element) {
    if (!element) {
      return null;
    }
    let record = gosxManagedMotionRecordsByElement.get(element);
    if (record) {
      record.config = gosxManagedMotionConfig(element);
      if (!record.played) {
        connectManagedMotion(record);
      }
      return record;
    }
    gosxManagedMotionNextID += 1;
    record = {
      key: "motion:" + gosxManagedMotionNextID,
      element: element,
      config: gosxManagedMotionConfig(element),
      played: false,
      animation: null,
      stopTrigger: null,
    };
    gosxManagedMotionRecordsByElement.set(element, record);
    gosxManagedMotionState(record, "idle");
    connectManagedMotion(record);
    return record;
  }

  function disposeManagedMotionElement(element) {
    const record = element ? gosxManagedMotionRecordsByElement.get(element) : null;
    if (!record) {
      return;
    }
    gosxCancelInvalidation(record.key);
    if (record.stopTrigger) {
      record.stopTrigger();
      record.stopTrigger = null;
    }
    cancelManagedMotionAnimation(record);
    gosxManagedMotionRecordsByElement.delete(element);
  }

  function installManagedMotionObserver(root) {
    if (gosxManagedMotionObserver || typeof MutationObserver !== "function" || !root) {
      return;
    }
    gosxManagedMotionObserver = new MutationObserver(function(records) {
      for (const record of records || []) {
        if (!record) {
          continue;
        }
        if (record.type === "attributes" && record.target && record.attributeName && GOSX_MOTION_MUTATION_ATTRS.includes(record.attributeName)) {
          if (record.target.hasAttribute && record.target.hasAttribute(GOSX_MOTION_ATTR)) {
            observeManagedMotion(record.target);
          } else {
            disposeManagedMotionElement(record.target);
          }
        }
        for (const node of gosxArrayFrom(record.addedNodes)) {
          if (node && node.nodeType === 1) {
            mountManagedMotion(node);
          }
        }
        for (const node of gosxArrayFrom(record.removedNodes)) {
          if (!node || node.nodeType !== 1) {
            continue;
          }
          for (const element of gosxManagedMotionElements(node)) {
            disposeManagedMotionElement(element);
          }
        }
      }
    });
    gosxManagedMotionObserver.observe(root, {
      subtree: true,
      childList: true,
      attributes: true,
      attributeFilter: GOSX_MOTION_MUTATION_ATTRS,
    });
  }

  function mountManagedMotion(root) {
    const targetRoot = root || document.body || document.documentElement;
    for (const element of gosxManagedMotionElements(targetRoot)) {
      observeManagedMotion(element);
    }
    installManagedMotionObserver(document.body || document.documentElement);
  }

  function disposeManagedMotion() {
    if (gosxManagedMotionObserver) {
      gosxManagedMotionObserver.disconnect();
      gosxManagedMotionObserver = null;
    }
    for (const element of Array.from(gosxManagedMotionRecordsByElement.keys())) {
      disposeManagedMotionElement(element);
    }
  }

  window.__gosx.motion = {
    mountAll: mountManagedMotion,
    observe: observeManagedMotion,
    dispose: disposeManagedMotionElement,
  };

  function gosxMediaQueryList(query) {
    if (!query || typeof window.matchMedia !== "function") {
      return null;
    }
    try {
      return window.matchMedia(query);
    } catch (_error) {
      return null;
    }
  }

  function gosxMediaQueryMatches(query) {
    const media = gosxMediaQueryList(query);
    return Boolean(media && media.matches);
  }

  function gosxNavigatorConnection() {
    const navigatorRef = window && window.navigator ? window.navigator : null;
    if (!navigatorRef || typeof navigatorRef !== "object") {
      return null;
    }
    return navigatorRef.connection || navigatorRef.mozConnection || navigatorRef.webkitConnection || null;
  }

  function gosxNumber(value, fallback) {
    const number = Number(value);
    return Number.isFinite(number) ? number : fallback;
  }

  function gosxPointerMode() {
    if (gosxMediaQueryMatches("(pointer: fine)")) {
      return "fine";
    }
    if (gosxMediaQueryMatches("(pointer: coarse)") || gosxMediaQueryMatches("(any-pointer: coarse)")) {
      return "coarse";
    }
    return "none";
  }

  function gosxContrastMode() {
    if (gosxMediaQueryMatches("(prefers-contrast: more)")) {
      return "more";
    }
    if (gosxMediaQueryMatches("(prefers-contrast: less)")) {
      return "less";
    }
    return "no-preference";
  }

  function gosxColorSchemeMode() {
    if (gosxMediaQueryMatches("(prefers-color-scheme: dark)")) {
      return "dark";
    }
    if (gosxMediaQueryMatches("(prefers-color-scheme: light)")) {
      return "light";
    }
    return "no-preference";
  }

  function gosxEnvironmentSnapshot() {
    const navigatorRef = window && window.navigator ? window.navigator : {};
    const connection = gosxNavigatorConnection();
    const visualViewport = window.visualViewport || null;
    const pageVisible = String(document && document.visibilityState || "visible").toLowerCase() !== "hidden";
    const coarsePointer = gosxPointerMode() === "coarse";
    const hover = gosxMediaQueryMatches("(hover: hover)") || gosxMediaQueryMatches("(any-hover: hover)");
    const saveData = Boolean(connection && connection.saveData);
    const reducedData = saveData || gosxMediaQueryMatches("(prefers-reduced-data: reduce)");
    const deviceMemory = Math.max(0, gosxNumber(navigatorRef && navigatorRef.deviceMemory, 0));
    const hardwareConcurrency = Math.max(0, Math.floor(gosxNumber(navigatorRef && navigatorRef.hardwareConcurrency, 0)));
    const maxTouchPoints = Math.max(0, Math.floor(gosxNumber(navigatorRef && navigatorRef.maxTouchPoints, 0)));
    const lowPower = reducedData || (coarsePointer && ((deviceMemory > 0 && deviceMemory <= 4) || (hardwareConcurrency > 0 && hardwareConcurrency <= 4)));
    const viewportWidth = Math.max(0, gosxNumber(window.innerWidth, document && document.documentElement && document.documentElement.clientWidth || 0));
    const viewportHeight = Math.max(0, gosxNumber(window.innerHeight, document && document.documentElement && document.documentElement.clientHeight || 0));

    return {
      pageVisible,
      pointer: gosxPointerMode(),
      coarsePointer,
      hover,
      reducedMotion: gosxMediaQueryMatches("(prefers-reduced-motion: reduce)"),
      reducedData,
      saveData,
      contrast: gosxContrastMode(),
      colorScheme: gosxColorSchemeMode(),
      devicePixelRatio: Math.max(1, gosxNumber(window.devicePixelRatio, 1)),
      viewportWidth,
      viewportHeight,
      visualViewportWidth: Math.max(0, gosxNumber(visualViewport && visualViewport.width, viewportWidth)),
      visualViewportHeight: Math.max(0, gosxNumber(visualViewport && visualViewport.height, viewportHeight)),
      visualViewportOffsetLeft: gosxNumber(visualViewport && visualViewport.offsetLeft, 0),
      visualViewportOffsetTop: gosxNumber(visualViewport && visualViewport.offsetTop, 0),
      visualViewportActive: Boolean(visualViewport),
      deviceMemory,
      hardwareConcurrency,
      maxTouchPoints,
      lowPower,
    };
  }

  function gosxEnvironmentChanged(prev, next) {
    if (!prev || !next) {
      return true;
    }
    const keys = Object.keys(next);
    for (const key of keys) {
      if (prev[key] !== next[key]) {
        return true;
      }
    }
    return false;
  }

  function cloneGosxEnvironment(state) {
    return state ? Object.assign({}, state) : null;
  }

  function applyGosxEnvironmentState(state) {
    const root = document.documentElement || document.body;
    const body = document.body || root;
    if (!root || !state) {
      return;
    }
    setAttrValue(root, "data-gosx-env-page-visible", state.pageVisible ? "true" : "false");
    setAttrValue(root, "data-gosx-env-pointer", state.pointer);
    setAttrValue(root, "data-gosx-env-hover", state.hover ? "true" : "false");
    setAttrValue(root, "data-gosx-env-reduced-motion", state.reducedMotion ? "true" : "false");
    setAttrValue(root, "data-gosx-env-reduced-data", state.reducedData ? "true" : "false");
    setAttrValue(root, "data-gosx-env-contrast", state.contrast);
    setAttrValue(root, "data-gosx-env-color-scheme", state.colorScheme);
    setAttrValue(root, "data-gosx-env-low-power", state.lowPower ? "true" : "false");
    setAttrValue(root, "data-gosx-env-visual-viewport", state.visualViewportActive ? "true" : "false");
    setStyleValue(root.style, "--gosx-env-viewport-width", state.viewportWidth + "px");
    setStyleValue(root.style, "--gosx-env-viewport-height", state.viewportHeight + "px");
    setStyleValue(root.style, "--gosx-env-visual-viewport-width", state.visualViewportWidth + "px");
    setStyleValue(root.style, "--gosx-env-visual-viewport-height", state.visualViewportHeight + "px");
    setStyleValue(root.style, "--gosx-env-visual-viewport-offset-left", state.visualViewportOffsetLeft + "px");
    setStyleValue(root.style, "--gosx-env-visual-viewport-offset-top", state.visualViewportOffsetTop + "px");
    setStyleValue(root.style, "--gosx-env-device-pixel-ratio", String(state.devicePixelRatio));
    if (body && body !== root) {
      setAttrValue(body, "data-gosx-env-page-visible", state.pageVisible ? "true" : "false");
      setAttrValue(body, "data-gosx-env-reduced-motion", state.reducedMotion ? "true" : "false");
      setAttrValue(body, "data-gosx-env-reduced-data", state.reducedData ? "true" : "false");
      setAttrValue(body, "data-gosx-env-low-power", state.lowPower ? "true" : "false");
    }
  }

  function dispatchGosxEnvironment(reason) {
    if (typeof CustomEvent !== "function" || !document || typeof document.dispatchEvent !== "function") {
      return;
    }
    document.dispatchEvent(new CustomEvent("gosx:environment", {
      detail: {
        reason: reason || "refresh",
        state: cloneGosxEnvironment(gosxEnvironmentState),
      },
    }));
  }

  function refreshGosxEnvironmentState(reason) {
    const next = gosxEnvironmentSnapshot();
    const changed = gosxEnvironmentChanged(gosxEnvironmentState, next);
    gosxEnvironmentState = next;
    applyGosxEnvironmentState(next);
    if (!changed) {
      return cloneGosxEnvironment(next);
    }
    for (const listener of Array.from(gosxEnvironmentListeners)) {
      try {
        listener(cloneGosxEnvironment(next), reason || "refresh");
      } catch (error) {
        console.error("[gosx] environment listener failed:", error);
      }
    }
    dispatchGosxEnvironment(reason);
    return cloneGosxEnvironment(next);
  }

  function scheduleGosxEnvironmentRefresh(reason) {
    gosxScheduleStateInvalidation("environment", reason || "environment", function(nextReason) {
      refreshGosxEnvironmentState(nextReason);
    });
  }

  function installGosxEnvironmentObservers() {
    if (gosxEnvironmentObserversInstalled) {
      return;
    }
    gosxEnvironmentObserversInstalled = true;

    const refresh = function(reason) {
      scheduleGosxEnvironmentRefresh(reason);
    };

    if (document && typeof document.addEventListener === "function") {
      document.addEventListener("visibilitychange", function() {
        refresh("visibility");
      });
    }
    if (typeof window.addEventListener === "function") {
      window.addEventListener("resize", function() {
        refresh("viewport");
      });
      window.addEventListener("orientationchange", function() {
        refresh("viewport");
      });
      window.addEventListener("pageshow", function() {
        refresh("pageshow");
      });
    }
    if (window.visualViewport && typeof window.visualViewport.addEventListener === "function") {
      window.visualViewport.addEventListener("resize", function() {
        refresh("visual-viewport");
      });
      window.visualViewport.addEventListener("scroll", function() {
        refresh("visual-viewport");
      });
    }

    const queries = [
      "(prefers-reduced-motion: reduce)",
      "(prefers-reduced-data: reduce)",
      "(prefers-contrast: more)",
      "(prefers-contrast: less)",
      "(prefers-color-scheme: dark)",
      "(prefers-color-scheme: light)",
      "(pointer: fine)",
      "(pointer: coarse)",
      "(any-pointer: coarse)",
      "(hover: hover)",
      "(any-hover: hover)",
    ];
    for (const query of queries) {
      const media = gosxMediaQueryList(query);
      if (!media) {
        continue;
      }
      const onChange = function() {
        refresh("media");
      };
      if (typeof media.addEventListener === "function") {
        media.addEventListener("change", onChange);
      } else if (typeof media.addListener === "function") {
        media.addListener(onChange);
      }
    }

    const connection = gosxNavigatorConnection();
    if (connection && typeof connection.addEventListener === "function") {
      connection.addEventListener("change", function() {
        refresh("connection");
      });
    }
  }

  function observeGosxEnvironment(listener, options) {
    if (typeof listener !== "function") {
      return function() {};
    }
    installGosxEnvironmentObservers();
    gosxEnvironmentListeners.add(listener);
    if (!gosxEnvironmentState) {
      refreshGosxEnvironmentState("init");
    }
    if (!options || options.immediate !== false) {
      listener(cloneGosxEnvironment(gosxEnvironmentState), "init");
    }
    return function() {
      gosxEnvironmentListeners.delete(listener);
    };
  }

  function gosxManagedHeadMarkers() {
    const children = gosxArrayFrom(document.head && document.head.childNodes);
    let start = null;
    let end = null;
    for (const child of children) {
      if (!child || child.nodeType !== 1) {
        continue;
      }
      if (String(child.tagName || "").toUpperCase() !== "META") {
        continue;
      }
      const name = String(child.getAttribute && child.getAttribute("name") || "");
      if (name === "gosx-head-start") {
        start = child;
      } else if (name === "gosx-head-end") {
        end = child;
      }
    }
    return { start, end };
  }

  function gosxDocumentChildrenBetween(start, end) {
    if (!start || !end || !start.parentNode || start.parentNode !== end.parentNode) {
      return [];
    }
    const children = gosxArrayFrom(start.parentNode.childNodes);
    const startIndex = children.indexOf(start);
    const endIndex = children.indexOf(end);
    if (startIndex < 0 || endIndex <= startIndex) {
      return [];
    }
    return children.slice(startIndex + 1, endIndex);
  }

  function gosxReadDocumentContract() {
    const node = document.getElementById && document.getElementById("gosx-document");
    if (!node) {
      return null;
    }
    try {
      const payload = JSON.parse(String(node.textContent || "{}"));
      return payload && typeof payload === "object" ? payload : null;
    } catch (error) {
      console.error("[gosx] invalid document contract:", error);
      return null;
    }
  }

  function gosxCurrentEnhancementLayer() {
    const hasRuntime = Boolean(
      typeof window.__gosx_hydrate === "function"
      || (window.__gosx && window.__gosx.islands && window.__gosx.islands.size > 0)
      || (window.__gosx && window.__gosx.engines && window.__gosx.engines.size > 0)
      || (window.__gosx && window.__gosx.hubs && window.__gosx.hubs.size > 0)
    );
    return hasRuntime ? "runtime" : "bootstrap";
  }

  function gosxNormalizeDocumentCSSLayer(value, fallback) {
    const layer = String(value || fallback || "global").trim().toLowerCase();
    return GOSX_DOCUMENT_CSS_LAYERS.includes(layer) ? layer : "global";
  }

  function gosxDocumentCSSLayer(node, fallback) {
    const value = String(node && node.getAttribute && node.getAttribute("data-gosx-css-layer") || "").trim();
    return gosxNormalizeDocumentCSSLayer(value, fallback);
  }

  function gosxDocumentCSSOwner(node, fallback) {
    const value = String(node && node.getAttribute && node.getAttribute("data-gosx-css-owner") || "").trim();
    return value || fallback || "document";
  }

  function gosxDocumentCSSSource(node, fallback) {
    const value = String(node && node.getAttribute && node.getAttribute("data-gosx-css-source") || "").trim();
    return value || String(fallback || "");
  }

  function gosxDocumentStyleEntry(node, order, layerFallback, ownerFallback) {
    const file = String(node.getAttribute && node.getAttribute("data-gosx-file-css") || "");
    const scope = String(node.getAttribute && node.getAttribute("data-gosx-file-css-scope") || "");
    return {
      kind: "inline",
      file,
      href: "",
      media: "",
      layer: gosxDocumentCSSLayer(node, layerFallback),
      owner: gosxDocumentCSSOwner(node, ownerFallback),
      source: gosxDocumentCSSSource(node, file),
      order,
      scope,
    };
  }

  function gosxDocumentStylesheetEntry(node, order, layerFallback, ownerFallback) {
    const href = String(node.getAttribute && node.getAttribute("href") || "");
    return {
      kind: "stylesheet",
      file: "",
      href,
      media: String(node.getAttribute && node.getAttribute("media") || ""),
      layer: gosxDocumentCSSLayer(node, layerFallback),
      owner: gosxDocumentCSSOwner(node, ownerFallback),
      source: gosxDocumentCSSSource(node, href),
      order,
      scope: "",
    };
  }

  function gosxDocumentCSSLayerState(layer) {
    return {
      layer,
      count: 0,
      inlineCount: 0,
      stylesheetCount: 0,
      scopedCount: 0,
      owners: [],
      sources: [],
      entries: [],
    };
  }

  function gosxDocumentCSSLayers(entries) {
    const layers = Object.create(null);
    for (const layer of GOSX_DOCUMENT_CSS_LAYERS) {
      layers[layer] = gosxDocumentCSSLayerState(layer);
    }
    for (const entry of entries || []) {
      const layer = gosxNormalizeDocumentCSSLayer(entry && entry.layer, "global");
      const bucket = layers[layer] || gosxDocumentCSSLayerState(layer);
      const item = Object.assign({}, entry, { layer });
      bucket.count += 1;
      if (item.kind === "stylesheet") {
        bucket.stylesheetCount += 1;
      } else {
        bucket.inlineCount += 1;
      }
      if (item.scope) {
        bucket.scopedCount += 1;
      }
      if (item.owner && !bucket.owners.includes(item.owner)) {
        bucket.owners.push(item.owner);
      }
      if (item.source && !bucket.sources.includes(item.source)) {
        bucket.sources.push(item.source);
      }
      bucket.entries.push(item);
      layers[layer] = bucket;
    }
    return layers;
  }

  function gosxWalkDocumentElements(root, visit) {
    if (!root || typeof visit !== "function") {
      return;
    }
    if (root.nodeType === 1) {
      visit(root);
    }
    const children = root.children || root.childNodes || [];
    for (const child of children) {
      if (child && child.nodeType === 1) {
        gosxWalkDocumentElements(child, visit);
      }
    }
  }

  function gosxNormalizeEnhancementLayer(value, fallback) {
    const layer = String(value || fallback || "html").trim().toLowerCase();
    return GOSX_DOCUMENT_ENHANCEMENT_LAYERS.includes(layer) ? layer : "html";
  }

  function gosxDocumentEnhancementLayerState(layer) {
    return {
      layer,
      count: 0,
      kinds: [],
      fallbacks: [],
      entries: [],
    };
  }

  function gosxDocumentEnhancementKindState(kind) {
    return {
      kind,
      count: 0,
      layers: [],
      fallbacks: [],
      entries: [],
    };
  }

  function gosxDocumentEnhancementEntry(node, order) {
    const kind = String(node && node.getAttribute && node.getAttribute("data-gosx-enhance") || "").trim();
    if (!kind) {
      return null;
    }
    const layer = gosxNormalizeEnhancementLayer(node && node.getAttribute && node.getAttribute("data-gosx-enhance-layer"), "html");
    const fallback = String(node && node.getAttribute && node.getAttribute("data-gosx-fallback") || "").trim() || "none";
    return {
      kind,
      layer,
      fallback,
      id: String(node && node.getAttribute && node.getAttribute("id") || ""),
      tag: String(node && node.tagName || "").toLowerCase(),
      engine: String(node && node.getAttribute && node.getAttribute("data-gosx-engine") || ""),
      order,
    };
  }

  function gosxDocumentEnhancements() {
    const entries = [];
    let order = 0;
    gosxWalkDocumentElements(document.body || document.documentElement, function(node) {
      const entry = gosxDocumentEnhancementEntry(node, order);
      if (!entry) {
        return;
      }
      entries.push(entry);
      order += 1;
    });
    const layers = Object.create(null);
    for (const layer of GOSX_DOCUMENT_ENHANCEMENT_LAYERS) {
      layers[layer] = gosxDocumentEnhancementLayerState(layer);
    }
    const kinds = Object.create(null);
    for (const entry of entries) {
      const layerBucket = layers[entry.layer] || gosxDocumentEnhancementLayerState(entry.layer);
      layerBucket.count += 1;
      if (!layerBucket.kinds.includes(entry.kind)) {
        layerBucket.kinds.push(entry.kind);
      }
      if (!layerBucket.fallbacks.includes(entry.fallback)) {
        layerBucket.fallbacks.push(entry.fallback);
      }
      layerBucket.entries.push(Object.assign({}, entry));
      layers[entry.layer] = layerBucket;

      const kindBucket = kinds[entry.kind] || gosxDocumentEnhancementKindState(entry.kind);
      kindBucket.count += 1;
      if (!kindBucket.layers.includes(entry.layer)) {
        kindBucket.layers.push(entry.layer);
      }
      if (!kindBucket.fallbacks.includes(entry.fallback)) {
        kindBucket.fallbacks.push(entry.fallback);
      }
      kindBucket.entries.push(Object.assign({}, entry));
      kinds[entry.kind] = kindBucket;
    }
    return {
      count: entries.length,
      entries,
      layers,
      kinds,
    };
  }

  function gosxDocumentRuntimeAssets(contract, enhancement, layer) {
    const assets = contract && contract.assets && typeof contract.assets === "object" ? contract.assets : {};
    let bootstrapMode = String(assets.bootstrapMode || "").trim().toLowerCase();
    if (!bootstrapMode) {
      bootstrapMode = layer === "runtime" || Boolean(enhancement && enhancement.runtime) ? "full" : (Boolean(enhancement && enhancement.bootstrap) ? "lite" : "none");
    }
    if (bootstrapMode !== "none" && bootstrapMode !== "lite" && bootstrapMode !== "full") {
      bootstrapMode = "none";
    }
    return {
      bootstrapMode,
      manifest: Boolean(assets.manifest),
      runtimePath: String(assets.runtimePath || ""),
      wasmExecPath: String(assets.wasmExecPath || ""),
      patchPath: String(assets.patchPath || ""),
      bootstrapPath: String(assets.bootstrapPath || ""),
      hlsPath: String(assets.hlsPath || ""),
      islands: Math.max(0, gosxNumber(assets.islands, 0)),
      engines: Math.max(0, gosxNumber(assets.engines, 0)),
      hubs: Math.max(0, gosxNumber(assets.hubs, 0)),
    };
  }

  function gosxEnhancementAttrName(kind) {
    const value = String(kind || "").trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
    return value ? "data-gosx-enhancement-" + value + "-count" : "";
  }

  function gosxDocumentHeadAssets() {
    const markers = gosxManagedHeadMarkers();
    const nodes = gosxDocumentChildrenBetween(markers.start, markers.end);
    const ownedCSS = [];
    const stylesheets = [];
    const scripts = [];
    let cssOrder = 0;

    for (const node of nodes) {
      if (!node || node.nodeType !== 1) {
        continue;
      }
      const tagName = String(node.tagName || "").toUpperCase();
      if (tagName === "STYLE") {
        ownedCSS.push(gosxDocumentStyleEntry(
          node,
          gosxNumber(node.getAttribute && node.getAttribute("data-gosx-css-order"), cssOrder),
          "global",
          "document-global"
        ));
        cssOrder += 1;
        continue;
      }
      if (tagName === "LINK" && /\bstylesheet\b/i.test(String(node.getAttribute && node.getAttribute("rel") || ""))) {
        const entry = gosxDocumentStylesheetEntry(node, cssOrder, "global", "document-global");
        stylesheets.push(entry);
        ownedCSS.push(entry);
        cssOrder += 1;
        continue;
      }
      if (tagName === "SCRIPT") {
        scripts.push({
          role: String(node.getAttribute && node.getAttribute("data-gosx-script") || ""),
          navigation: node.hasAttribute && node.hasAttribute("data-gosx-navigation"),
          src: String(node.getAttribute && node.getAttribute("src") || ""),
          inline: !node.getAttribute || !node.getAttribute("src"),
        });
      }
    }

    for (const node of gosxArrayFrom(document.head && document.head.childNodes)) {
      if (!node || node.nodeType !== 1) {
        continue;
      }
      if (String(node.tagName || "").toUpperCase() !== "STYLE") {
        continue;
      }
      if (String(node.getAttribute && node.getAttribute("data-gosx-css-layer") || "") !== "runtime") {
        continue;
      }
      ownedCSS.push(gosxDocumentStyleEntry(node, cssOrder, "runtime", "gosx-bootstrap"));
      cssOrder += 1;
    }

    return {
      managed: Boolean(markers.start && markers.end),
      ownedCSS,
      stylesheets,
      scripts,
    };
  }

  function cloneGosxDocumentState(state) {
    if (!state) {
      return null;
    }
    return {
      page: Object.assign({}, state.page),
      enhancement: Object.assign({}, state.enhancement),
      enhancements: {
        count: state.enhancements.count,
        entries: state.enhancements.entries.map(function(entry) {
          return Object.assign({}, entry);
        }),
        layers: Object.fromEntries(GOSX_DOCUMENT_ENHANCEMENT_LAYERS.map(function(layer) {
          const bucket = state.enhancements.layers[layer] || gosxDocumentEnhancementLayerState(layer);
          return [layer, {
            layer: bucket.layer,
            count: bucket.count,
            kinds: bucket.kinds.slice(),
            fallbacks: bucket.fallbacks.slice(),
            entries: bucket.entries.map(function(entry) {
              return Object.assign({}, entry);
            }),
          }];
        })),
        kinds: Object.fromEntries(Object.keys(state.enhancements.kinds || {}).map(function(kind) {
          const bucket = state.enhancements.kinds[kind] || gosxDocumentEnhancementKindState(kind);
          return [kind, {
            kind: bucket.kind,
            count: bucket.count,
            layers: bucket.layers.slice(),
            fallbacks: bucket.fallbacks.slice(),
            entries: bucket.entries.map(function(entry) {
              return Object.assign({}, entry);
            }),
          }];
        })),
      },
      head: Object.assign({}, state.head),
      css: {
        owned: state.css.owned.map(function(entry) {
          return Object.assign({}, entry);
        }),
        stylesheets: state.css.stylesheets.map(function(entry) {
          return Object.assign({}, entry);
        }),
        layers: Object.fromEntries(GOSX_DOCUMENT_CSS_LAYERS.map(function(layer) {
          const bucket = state.css.layers[layer] || gosxDocumentCSSLayerState(layer);
          return [layer, {
            layer: bucket.layer,
            count: bucket.count,
            inlineCount: bucket.inlineCount,
            stylesheetCount: bucket.stylesheetCount,
            scopedCount: bucket.scopedCount,
            owners: bucket.owners.slice(),
            sources: bucket.sources.slice(),
            entries: bucket.entries.map(function(entry) {
              return Object.assign({}, entry);
            }),
          }];
        })),
      },
      assets: {
        runtime: Object.assign({}, state.assets.runtime),
        scripts: state.assets.scripts.map(function(entry) {
          return Object.assign({}, entry);
        }),
      },
    };
  }

  function gosxReadDocumentState() {
    const contract = gosxReadDocumentContract();
    const page = contract && contract.page && typeof contract.page === "object" ? contract.page : {};
    const enhancement = contract && contract.enhancement && typeof contract.enhancement === "object" ? contract.enhancement : {};
    const assets = gosxDocumentHeadAssets();
    const cssLayers = gosxDocumentCSSLayers(assets.ownedCSS);
    const layer = gosxCurrentEnhancementLayer();
    const runtimeAssets = gosxDocumentRuntimeAssets(contract, enhancement, layer);
    const enhancements = gosxDocumentEnhancements();

    return {
      page: {
        id: typeof page.id === "string" && page.id ? page.id : "gosx-doc-page",
        pattern: typeof page.pattern === "string" ? page.pattern : "",
        path: typeof page.path === "string" && page.path ? page.path : String(window.location && window.location.href || ""),
        title: typeof page.title === "string" && page.title ? page.title : String(document.title || ""),
        status: Number.isFinite(Number(page.status)) ? Number(page.status) : 200,
        requestID: typeof page.requestID === "string" ? page.requestID : "",
      },
      enhancement: {
        layer,
        bootstrap: runtimeAssets.bootstrapMode !== "none" || Boolean(enhancement.bootstrap),
        runtime: runtimeAssets.bootstrapMode === "full" || layer === "runtime" || Boolean(enhancement.runtime),
        navigation: Boolean(enhancement.navigation) || Boolean(window.__gosx_page_nav && typeof window.__gosx_page_nav.navigate === "function"),
        ready: Boolean(window.__gosx && window.__gosx.ready),
      },
      enhancements,
      head: {
        managed: assets.managed,
        ownedCSSCount: assets.ownedCSS.length,
        stylesheetCount: assets.stylesheets.length,
        scriptCount: assets.scripts.length,
      },
      css: {
        owned: assets.ownedCSS,
        stylesheets: assets.stylesheets,
        layers: cssLayers,
      },
      assets: {
        runtime: runtimeAssets,
        scripts: assets.scripts,
      },
    };
  }

  function gosxDocumentChanged(prev, next) {
    return JSON.stringify(prev || null) !== JSON.stringify(next || null);
  }

  function applyGosxDocumentState(state) {
    const root = document.documentElement || document.body;
    const body = document.body || root;
    if (!root || !state) {
      return;
    }
    setAttrValue(root, "data-gosx-document", "true");
    setAttrValue(root, "data-gosx-document-id", state.page.id);
    setAttrValue(root, "data-gosx-route-pattern", state.page.pattern);
    setAttrValue(root, "data-gosx-enhancement-layer", state.enhancement.layer);
    setAttrValue(root, "data-gosx-bootstrap-mode", state.assets && state.assets.runtime ? state.assets.runtime.bootstrapMode : "none");
    setAttrValue(root, "data-gosx-navigation", state.enhancement.navigation ? "true" : "false");
    setAttrValue(root, "data-gosx-runtime-ready", state.enhancement.ready ? "true" : "false");
    setAttrValue(root, "data-gosx-head-managed", state.head.managed ? "true" : "false");
    setAttrValue(root, "data-gosx-enhancement-count", state.enhancements && state.enhancements.count || 0);
    setAttrValue(root, "data-gosx-css-owned-count", state.head.ownedCSSCount);
    setStyleValue(root.style, "--gosx-document-owned-css-count", String(state.head.ownedCSSCount));
    setStyleValue(root.style, "--gosx-document-stylesheet-count", String(state.head.stylesheetCount));
    setStyleValue(root.style, "--gosx-document-enhancement-count", String(state.enhancements && state.enhancements.count || 0));
    for (const layer of GOSX_DOCUMENT_CSS_LAYERS) {
      const count = state.css && state.css.layers && state.css.layers[layer] ? state.css.layers[layer].count : 0;
      setAttrValue(root, "data-gosx-css-" + layer + "-count", count);
      setStyleValue(root.style, "--gosx-document-css-" + layer + "-count", String(count));
    }
    for (const layer of GOSX_DOCUMENT_ENHANCEMENT_LAYERS) {
      const count = state.enhancements && state.enhancements.layers && state.enhancements.layers[layer] ? state.enhancements.layers[layer].count : 0;
      setAttrValue(root, "data-gosx-enhancement-" + layer + "-count", count);
      setStyleValue(root.style, "--gosx-document-enhancement-" + layer + "-count", String(count));
    }
    const nextKindAttrs = new Set();
    for (const kind of Object.keys(state.enhancements && state.enhancements.kinds || {})) {
      const attrName = gosxEnhancementAttrName(kind);
      if (!attrName) {
        continue;
      }
      nextKindAttrs.add(attrName);
      setAttrValue(root, attrName, state.enhancements.kinds[kind].count);
    }
    for (const attrName of Array.from(gosxAppliedEnhancementKindAttrs)) {
      if (nextKindAttrs.has(attrName)) {
        continue;
      }
      setAttrValue(root, attrName, "");
      gosxAppliedEnhancementKindAttrs.delete(attrName);
    }
    for (const attrName of Array.from(nextKindAttrs)) {
      gosxAppliedEnhancementKindAttrs.add(attrName);
    }
    if (body && body !== root) {
      setAttrValue(body, "data-gosx-document-id", state.page.id);
      setAttrValue(body, "data-gosx-enhancement-layer", state.enhancement.layer);
      setAttrValue(body, "data-gosx-bootstrap-mode", state.assets && state.assets.runtime ? state.assets.runtime.bootstrapMode : "none");
      setAttrValue(body, "data-gosx-navigation", state.enhancement.navigation ? "true" : "false");
      setAttrValue(body, "data-gosx-runtime-ready", state.enhancement.ready ? "true" : "false");
      setAttrValue(body, "data-gosx-enhancement-count", state.enhancements && state.enhancements.count || 0);
      for (const layer of GOSX_DOCUMENT_CSS_LAYERS) {
        const count = state.css && state.css.layers && state.css.layers[layer] ? state.css.layers[layer].count : 0;
        setAttrValue(body, "data-gosx-css-" + layer + "-count", count);
      }
      for (const layer of GOSX_DOCUMENT_ENHANCEMENT_LAYERS) {
        const count = state.enhancements && state.enhancements.layers && state.enhancements.layers[layer] ? state.enhancements.layers[layer].count : 0;
        setAttrValue(body, "data-gosx-enhancement-" + layer + "-count", count);
      }
      for (const attrName of Array.from(gosxAppliedEnhancementKindAttrs)) {
        const kindName = attrName.replace(/^data-gosx-enhancement-/, "").replace(/-count$/, "");
        const bucket = state.enhancements && state.enhancements.kinds && state.enhancements.kinds[kindName];
        setAttrValue(body, attrName, bucket ? bucket.count : "");
      }
    }
  }

  function dispatchGosxDocument(reason) {
    if (typeof CustomEvent !== "function" || !document || typeof document.dispatchEvent !== "function") {
      return;
    }
    document.dispatchEvent(new CustomEvent("gosx:document", {
      detail: {
        reason: reason || "refresh",
        state: cloneGosxDocumentState(gosxDocumentState),
      },
    }));
  }

  function refreshGosxDocumentState(reason) {
    const next = gosxReadDocumentState();
    const changed = gosxDocumentChanged(gosxDocumentState, next);
    gosxDocumentState = next;
    applyGosxDocumentState(next);
    if (!changed) {
      return cloneGosxDocumentState(next);
    }
    for (const listener of Array.from(gosxDocumentListeners)) {
      try {
        listener(cloneGosxDocumentState(next), reason || "refresh");
      } catch (error) {
        console.error("[gosx] document listener failed:", error);
      }
    }
    dispatchGosxDocument(reason);
    return cloneGosxDocumentState(next);
  }

  function scheduleGosxDocumentRefresh(reason) {
    gosxScheduleStateInvalidation("document", reason || "refresh", function(nextReason) {
      refreshGosxDocumentState(nextReason);
    });
  }

  function installGosxDocumentObservers() {
    if (gosxDocumentObserversInstalled) {
      return;
    }
    gosxDocumentObserversInstalled = true;
    if (document && typeof document.addEventListener === "function") {
      document.addEventListener("gosx:navigate", function() {
        scheduleGosxDocumentRefresh("navigate");
        scheduleGosxEnvironmentRefresh("navigate");
      });
      document.addEventListener("gosx:ready", function() {
        scheduleGosxDocumentRefresh("ready");
      });
    }
    if (typeof window.addEventListener === "function") {
      window.addEventListener("pageshow", function() {
        scheduleGosxDocumentRefresh("pageshow");
      });
    }
    if (typeof MutationObserver === "function" && document && document.head) {
      const headObserver = new MutationObserver(function() {
        scheduleGosxDocumentRefresh("head-mutation");
      });
      if (typeof headObserver.observe === "function") {
        headObserver.observe(document.head, {
          subtree: true,
          childList: true,
          attributes: true,
          characterData: true,
          attributeFilter: [
            "href",
            "media",
            "rel",
            "name",
            "content",
            "src",
            "data-gosx-css-layer",
            "data-gosx-css-owner",
            "data-gosx-css-source",
            "data-gosx-css-order",
            "data-gosx-css-scope",
            "data-gosx-file-css",
            "data-gosx-file-css-scope",
            "data-gosx-script",
            "data-gosx-navigation",
          ],
        });
      }
    }
  }

  function observeGosxDocument(listener, options) {
    if (typeof listener !== "function") {
      return function() {};
    }
    installGosxDocumentObservers();
    gosxDocumentListeners.add(listener);
    if (!gosxDocumentState) {
      refreshGosxDocumentState("init");
    }
    if (!options || options.immediate !== false) {
      listener(cloneGosxDocumentState(gosxDocumentState), "init");
    }
    return function() {
      gosxDocumentListeners.delete(listener);
    };
  }

  function gosxInheritedElementAttribute(element, name) {
    if (!element || !name) {
      return "";
    }
    let current = element;
    while (current) {
      if (typeof current.getAttribute === "function") {
        const value = String(current.getAttribute(name) || "").trim();
        if (value) {
          return value;
        }
      }
      current = current.parentNode || null;
    }
    if (document && document.documentElement && document.documentElement !== element && typeof document.documentElement.getAttribute === "function") {
      return String(document.documentElement.getAttribute(name) || "").trim();
    }
    return "";
  }

  function gosxPresentationSnapshot(element) {
    if (!element || typeof element !== "object") {
      return null;
    }
    const style = textLayoutComputedStyle(element);
    const rect = element && typeof element.getBoundingClientRect === "function" ? element.getBoundingClientRect() : null;
    const width = Math.max(0, gosxNumber(rect && rect.width, element && (element.clientWidth || element.offsetWidth || element.width) || 0));
    const height = Math.max(0, gosxNumber(rect && rect.height, element && (element.clientHeight || element.offsetHeight || element.height) || 0));
    const lang = gosxInheritedElementAttribute(element, "lang");
    const directionAttr = gosxInheritedElementAttribute(element, "dir");
    const writingMode = normalizeTextLayoutWritingMode(textLayoutComputedStyleValue(style, "writing-mode"));
    const inlineSize = textLayoutLogicalInlineSize(width, height, writingMode);
    const blockSize = textLayoutLogicalBlockSize(width, height, writingMode);
    const maxInlineSize = textLayoutLengthValue(
      textLayoutComputedStyleValue(style, "--gosx-text-layout-max-inline-size")
      || textLayoutComputedStyleValue(style, "max-inline-size")
      || textLayoutComputedStyleValue(style, "--gosx-text-layout-max-width")
      || textLayoutComputedStyleValue(style, "max-width"),
      inlineSize,
    );
    return {
      style,
      width,
      height,
      inlineSize,
      blockSize,
      maxWidth: maxInlineSize > 0 ? maxInlineSize : inlineSize,
      maxInlineSize: maxInlineSize > 0 ? maxInlineSize : inlineSize,
      direction: textLayoutComputedStyleValue(style, "direction") || directionAttr || "",
      writingMode,
      lang,
      font: textLayoutComputedStyleValue(style, "font") || "",
      lineHeight: textLayoutLengthValue(
        textLayoutComputedStyleValue(style, "--gosx-text-layout-line-height")
        || textLayoutComputedStyleValue(style, "line-height"),
        0
      ),
      textAlign: textLayoutComputedStyleValue(style, "text-align") || "",
      whiteSpace: textLayoutComputedStyleValue(style, "white-space") || "",
      display: textLayoutComputedStyleValue(style, "display") || "",
      visibility: textLayoutComputedStyleValue(style, "visibility") || "",
      containerType: textLayoutComputedStyleValue(style, "container-type") || "",
      environment: cloneGosxEnvironment(gosxEnvironmentState || refreshGosxEnvironmentState("presentation")),
    };
  }

  function observeGosxPresentation(element, listener, options) {
    if (!element || typeof listener !== "function") {
      return function() {};
    }
    let record = gosxPresentationRecordsByElement.get(element) || null;
    if (!record) {
      record = {
        element,
        listeners: new Set(),
      };
      gosxPresentationRecordsByElement.set(element, record);
      ensureGosxPresentationObservers();
      if (gosxPresentationResizeObserver && typeof gosxPresentationResizeObserver.observe === "function") {
        gosxPresentationResizeObserver.observe(element);
      }
    }
    record.listeners.add(listener);
    if (!options || options.immediate !== false) {
      listener(gosxPresentationSnapshot(element), "init");
    }
    return function() {
      const current = gosxPresentationRecordsByElement.get(element);
      if (!current) {
        return;
      }
      current.listeners.delete(listener);
      if (current.listeners.size > 0) {
        return;
      }
      gosxCancelInvalidation(current);
      if (gosxPresentationResizeObserver && typeof gosxPresentationResizeObserver.unobserve === "function") {
        gosxPresentationResizeObserver.unobserve(element);
      }
      gosxPresentationRecordsByElement.delete(element);
      teardownGosxPresentationObservers();
    };
  }

  function gosxNotifyPresentationRecord(record, reason) {
    if (!record || !record.element || record.listeners.size === 0) {
      return;
    }
    const snapshot = gosxPresentationSnapshot(record.element);
    for (const listener of Array.from(record.listeners)) {
      try {
        listener(snapshot, reason || "presentation");
      } catch (error) {
        console.error("[gosx] presentation listener failed:", error);
      }
    }
  }

  function gosxSchedulePresentationRecord(record, reason) {
    if (!record) {
      return;
    }
    gosxScheduleVisualInvalidation(record, reason || "presentation", function(nextReason) {
      gosxNotifyPresentationRecord(record, nextReason);
    });
  }

  function gosxSchedulePresentationRefresh(reason) {
    for (const record of Array.from(gosxPresentationRecordsByElement.values())) {
      gosxSchedulePresentationRecord(record, reason || "presentation");
    }
  }

  function gosxPresentationMutationAffectsRecord(record, target) {
    if (!record || !record.element) {
      return false;
    }
    if (!target || target === record.element || target === document.documentElement || target === document.body) {
      return true;
    }
    return typeof target.contains === "function" && target.contains(record.element);
  }

  function ensureGosxPresentationObservers() {
    if (gosxPresentationRecordsByElement.size === 0) {
      return;
    }
    if (!gosxPresentationResizeObserver && typeof ResizeObserver === "function") {
      gosxPresentationResizeObserver = new ResizeObserver(function(entries) {
        for (const entry of entries || []) {
          const record = entry && entry.target ? gosxPresentationRecordsByElement.get(entry.target) : null;
          if (record) {
            gosxSchedulePresentationRecord(record, "presentation-resize");
          }
        }
      });
      for (const record of Array.from(gosxPresentationRecordsByElement.values())) {
        if (record.element && typeof gosxPresentationResizeObserver.observe === "function") {
          gosxPresentationResizeObserver.observe(record.element);
        }
      }
    }
    if (!gosxPresentationStopEnvironment) {
      gosxPresentationStopEnvironment = observeGosxEnvironment(function() {
        gosxSchedulePresentationRefresh("presentation-environment");
      }, { immediate: false });
    }
    if (!gosxPresentationStopDocument) {
      gosxPresentationStopDocument = observeGosxDocument(function() {
        gosxSchedulePresentationRefresh("presentation-document");
      }, { immediate: false });
    }
    if (!gosxPresentationMutationObserver && typeof MutationObserver === "function" && document && document.documentElement) {
      gosxPresentationMutationObserver = new MutationObserver(function(records) {
        const affected = new Set();
        for (const mutation of records || []) {
          const target = mutation && mutation.target;
          for (const record of Array.from(gosxPresentationRecordsByElement.values())) {
            if (gosxPresentationMutationAffectsRecord(record, target)) {
              affected.add(record);
            }
          }
        }
        for (const record of Array.from(affected)) {
          gosxSchedulePresentationRecord(record, "presentation-mutation");
        }
      });
      if (typeof gosxPresentationMutationObserver.observe === "function") {
        gosxPresentationMutationObserver.observe(document.documentElement, {
          subtree: true,
          attributes: true,
          attributeFilter: GOSX_PRESENTATION_MUTATION_ATTRS,
        });
      }
    }
  }

  function teardownGosxPresentationObservers() {
    if (gosxPresentationRecordsByElement.size > 0) {
      return;
    }
    if (gosxPresentationResizeObserver && typeof gosxPresentationResizeObserver.disconnect === "function") {
      gosxPresentationResizeObserver.disconnect();
    }
    gosxPresentationResizeObserver = null;
    if (gosxPresentationMutationObserver && typeof gosxPresentationMutationObserver.disconnect === "function") {
      gosxPresentationMutationObserver.disconnect();
    }
    gosxPresentationMutationObserver = null;
    if (typeof gosxPresentationStopEnvironment === "function") {
      gosxPresentationStopEnvironment();
    }
    gosxPresentationStopEnvironment = null;
    if (typeof gosxPresentationStopDocument === "function") {
      gosxPresentationStopDocument();
    }
    gosxPresentationStopDocument = null;
  }

  window.__gosx.environment = {
    get() {
      if (!gosxEnvironmentState) {
        refreshGosxEnvironmentState("read");
      }
      return cloneGosxEnvironment(gosxEnvironmentState);
    },
    refresh(reason) {
      return refreshGosxEnvironmentState(reason || "manual");
    },
    observe: observeGosxEnvironment,
  };

  window.__gosx.document = {
    get() {
      if (!gosxDocumentState) {
        refreshGosxDocumentState("read");
      }
      return cloneGosxDocumentState(gosxDocumentState);
    },
    refresh(reason) {
      return refreshGosxDocumentState(reason || "manual");
    },
    css(layer) {
      if (!gosxDocumentState) {
        refreshGosxDocumentState("read");
      }
      if (!layer) {
        return cloneGosxDocumentState(gosxDocumentState).css;
      }
      const key = gosxNormalizeDocumentCSSLayer(layer, "global");
      return cloneGosxDocumentState(gosxDocumentState).css.layers[key];
    },
    enhancements(kind) {
      if (!gosxDocumentState) {
        refreshGosxDocumentState("read");
      }
      const snapshot = cloneGosxDocumentState(gosxDocumentState).enhancements;
      if (!kind) {
        return snapshot;
      }
      return snapshot.kinds[String(kind)] || gosxDocumentEnhancementKindState(String(kind));
    },
    observe: observeGosxDocument,
  };

  window.__gosx.presentation = {
    read: gosxPresentationSnapshot,
    observe: observeGosxPresentation,
  };

  installGosxEnvironmentObservers();
  installGosxDocumentObservers();
  refreshGosxEnvironmentState("bootstrap");
  refreshGosxDocumentState("bootstrap");

  let pendingManifest = null;

  function runtimeReady() {
    return (
      typeof window.__gosx_hydrate === "function" ||
      typeof window.__gosx_action === "function" ||
      typeof window.__gosx_set_shared_signal === "function"
    );
  }

  function loadManifest() {
    const el = document.getElementById("gosx-manifest");
    if (!el) return null;

    try {
      return JSON.parse(el.textContent);
    } catch (e) {
      console.error("[gosx] failed to parse manifest:", e);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "bootstrap",
          type: "manifest",
          source: "gosx-manifest",
          element: el,
          message: "failed to parse gosx manifest",
          error: e,
          fallback: "server",
        });
      }
      return null;
    }
  }

  async function loadRuntime(runtimeRef) {
    if (typeof Go === "undefined") {
      console.error("[gosx] wasm_exec.js must be loaded before bootstrap.js");
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "bootstrap",
          type: "runtime",
          source: runtimeRef && runtimeRef.path,
          ref: runtimeRef && runtimeRef.path,
          message: "wasm_exec.js must be loaded before bootstrap.js",
          fallback: "server",
        });
      }
      return;
    }

    const go = new Go();

    try {
      const response = await fetchRuntimeResponse(runtimeRef);
      const result = await instantiateRuntimeModule(response, go.importObject);
      go.run(result.instance);
    } catch (e) {
      console.error("[gosx] failed to load WASM runtime:", e);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "bootstrap",
          type: "runtime",
          source: runtimeRef && runtimeRef.path,
          ref: runtimeRef && runtimeRef.path,
          message: "failed to load wasm runtime",
          error: e,
          fallback: "server",
        });
      }
    }
  }

  async function fetchRuntimeResponse(runtimeRef) {
    const response = await fetch(runtimeRef.path);
    if (!response.ok) {
      throw new Error("runtime fetch failed with status " + response.status);
    }
    return response;
  }

  async function instantiateRuntimeModule(response, importObject) {
    if (supportsInstantiateStreaming()) {
      return instantiateRuntimeStreaming(response, importObject);
    }
    return instantiateRuntimeBytes(response, importObject);
  }

  function supportsInstantiateStreaming() {
    return typeof WebAssembly.instantiateStreaming === "function";
  }

  async function instantiateRuntimeStreaming(response, importObject) {
    try {
      return await WebAssembly.instantiateStreaming(response.clone(), importObject);
    } catch (streamErr) {
      return instantiateRuntimeBytes(response, importObject);
    }
  }

  async function instantiateRuntimeBytes(response, importObject) {
    const bytes = await response.arrayBuffer();
    return WebAssembly.instantiate(bytes, importObject);
  }

  async function fetchProgram(programRef, programFormat) {
    try {
      const resp = await fetch(programRef);
      if (!resp.ok) {
        console.error(`[gosx] failed to fetch program ${programRef}: ${resp.status}`);
        return null;
      }

      if (programFormat === "wasm" || programFormat === "bin") {
        return new Uint8Array(await resp.arrayBuffer());
      }
      return await resp.text();
    } catch (e) {
      console.error(`[gosx] error fetching program ${programRef}:`, e);
      return null;
    }
  }

  function inferProgramFormat(entry) {
    if (entry.programFormat) return entry.programFormat;
    if (typeof entry.programRef === "string" && entry.programRef.endsWith(".gxi")) {
      return "bin";
    }
    return "json";
  }

  async function loadEngineScript(jsRef) {
    if (!jsRef) return;
    if (loadedEngineScripts.has(jsRef)) {
      return loadedEngineScripts.get(jsRef);
    }

    const promise = (async function() {
      try {
        const resp = await fetch(jsRef);
        if (!resp.ok) {
          throw new Error("engine script fetch failed with status " + resp.status);
        }

        const source = await resp.text();
        (0, eval)(String(source) + "\n//# sourceURL=" + jsRef);
      } catch (e) {
        console.error(`[gosx] failed to load engine script ${jsRef}:`, e);
      }
    })();

    loadedEngineScripts.set(jsRef, promise);
    return promise;
  }

  function engineFrame(callback) {
    if (typeof window.requestAnimationFrame === "function") {
      return window.requestAnimationFrame(callback);
    }
    return setTimeout(function() {
      callback(Date.now());
    }, 16);
  }

  function cancelEngineFrame(handle) {
    if (typeof window.cancelAnimationFrame === "function") {
      window.cancelAnimationFrame(handle);
      return;
    }
    clearTimeout(handle);
  }

  function gosxInputState() {
    if (!window.__gosx.input) {
      window.__gosx.input = {
        pending: null,
        frameHandle: 0,
        providers: Object.create(null),
      };
    }
    return window.__gosx.input;
  }

  function queueInputSignal(name, value) {
    if (!name) return;
    const state = gosxInputState();
    if (!state.pending) {
      state.pending = Object.create(null);
    }
    state.pending[name] = value;
    scheduleInputFlush();
  }

  function scheduleInputFlush() {
    const state = gosxInputState();
    if (state.frameHandle) return;
    state.frameHandle = engineFrame(function() {
      state.frameHandle = 0;
      flushInputSignals();
    });
  }

  function flushInputSignals() {
    const state = gosxInputState();
    const payload = state.pending;
    state.pending = null;
    if (!payload) return;

    const setInputBatch = window.__gosx_set_input_batch;
    if (typeof setInputBatch !== "function") return;

    try {
      const result = setInputBatch(JSON.stringify(payload));
      if (typeof result === "string" && result !== "") {
        console.error("[gosx] input batch error:", result);
      }
    } catch (e) {
      console.error("[gosx] input batch error:", e);
    }
  }

  function capabilityList(entry) {
    return Array.isArray(entry && entry.capabilities) ? entry.capabilities : [];
  }

  function activateInputProviders(entry) {
    for (const capability of capabilityList(entry)) {
      activateInputProvider(capability);
    }
  }

  function activateInputProvider(capability) {
    const state = gosxInputState();
    const current = state.providers[capability];
    if (current) {
      current.refCount += 1;
      return;
    }

    const provider = createInputProvider(capability);
    if (!provider) {
      return;
    }

    provider.refCount = 1;
    state.providers[capability] = provider;
  }

  function releaseInputProviders(record) {
    for (const capability of capabilityList(record)) {
      releaseInputProvider(capability);
    }
  }

  function releaseInputProvider(capability) {
    const state = gosxInputState();
    const provider = state.providers[capability];
    if (!provider) return;

    provider.refCount -= 1;
    if (provider.refCount > 0) {
      return;
    }

    if (typeof provider.dispose === "function") {
      provider.dispose();
    }
    delete state.providers[capability];
  }

  function createInputProvider(capability) {
    switch (capability) {
      case "keyboard":
        return createKeyboardInputProvider();
      case "pointer":
        return createPointerInputProvider();
      case "gamepad":
        return createGamepadInputProvider();
      default:
        return null;
    }
  }

  function createKeyboardInputProvider() {
    const pressed = new Set();

    function onKey(event) {
      const key = normalizeKeyName(event);
      if (!key) return;
      const active = event.type === "keydown";
      if (active) {
        pressed.add(key);
      } else {
        pressed.delete(key);
      }
      queueInputSignal("$input.key." + key, active);
    }

    function onBlur() {
      for (const key of Array.from(pressed)) {
        queueInputSignal("$input.key." + key, false);
      }
      pressed.clear();
    }

    return bindInputProviderListeners([
      [document, "keydown", onKey],
      [document, "keyup", onKey],
      [window, "blur", onBlur],
    ]);
  }

  function createPointerInputProvider() {
    const state = { lastX: null, lastY: null };

    function publishPointer(event) {
      publishPointerSignals(resolvePointerSample(event, state), event);
    }

    function onBlur() {
      resetPointerSignals();
    }

    return bindInputProviderListeners([
      [document, "pointermove", publishPointer],
      [document, "pointerdown", publishPointer],
      [document, "pointerup", publishPointer],
      [window, "blur", onBlur],
    ]);
  }

  function createGamepadInputProvider() {
    let active = true;
    let frameHandle = 0;

    function pollGamepad() {
      if (!active) return;
      const navigatorRef = window.navigator;
      if (navigatorRef && typeof navigatorRef.getGamepads === "function") {
        const pads = navigatorRef.getGamepads() || [];
        const pad = pads[0];
        if (pad) {
          publishGamepadSignals(pad);
        } else {
          queueInputSignal("$input.gamepad0.connected", false);
        }
      }
      frameHandle = engineFrame(pollGamepad);
    }

    frameHandle = engineFrame(pollGamepad);

    return {
      dispose() {
        active = false;
        if (frameHandle) {
          cancelEngineFrame(frameHandle);
          frameHandle = 0;
        }
      },
    };
  }

  function bindInputProviderListeners(bindings) {
    for (const binding of bindings) {
      binding[0].addEventListener(binding[1], binding[2]);
    }
    return {
      dispose() {
        for (const binding of bindings) {
          binding[0].removeEventListener(binding[1], binding[2]);
        }
      },
    };
  }

  function normalizeKeyName(event) {
    const raw = event && (event.key || event.code);
    if (!raw) return "";
    return String(raw).trim().toLowerCase();
  }

  function resolvePointerSample(event, state) {
    const previousX = state.lastX == null ? 0 : state.lastX;
    const previousY = state.lastY == null ? 0 : state.lastY;
    const x = sceneNumber(event && event.clientX, previousX);
    const y = sceneNumber(event && event.clientY, previousY);
    const sample = {
      x,
      y,
      deltaX: sceneNumber(event && event.movementX, state.lastX == null ? 0 : x - previousX),
      deltaY: sceneNumber(event && event.movementY, state.lastY == null ? 0 : y - previousY),
      buttons: event && typeof event.buttons !== "undefined" ? sceneNumber(event.buttons, 0) : null,
      button: event && typeof event.button === "number" ? event.button : null,
      active: event ? event.type !== "pointerup" : false,
    };
    state.lastX = x;
    state.lastY = y;
    return sample;
  }

  function publishPointerSignals(sample, event) {
    queueInputSignal("$input.pointer.x", sample.x);
    queueInputSignal("$input.pointer.y", sample.y);
    queueInputSignal("$input.pointer.deltaX", sample.deltaX);
    queueInputSignal("$input.pointer.deltaY", sample.deltaY);
    if (sample.buttons != null) {
      queueInputSignal("$input.pointer.buttons", sample.buttons);
    }
    if (sample.button != null) {
      queueInputSignal("$input.pointer.button" + sample.button, sample.active);
    }
  }

  function resetPointerSignals() {
    queueInputSignal("$input.pointer.deltaX", 0);
    queueInputSignal("$input.pointer.deltaY", 0);
    queueInputSignal("$input.pointer.buttons", 0);
  }

  function sceneClamp(value, min, max) {
    return Math.max(min, Math.min(max, value));
  }

  function sceneLocalPointerPoint(event, canvas, width, height) {
    const rect = canvas.getBoundingClientRect();
    const safeWidth = Math.max(rect.width || 0, 1);
    const safeHeight = Math.max(rect.height || 0, 1);
    return {
      x: sceneClamp(((sceneNumber(event && event.clientX, rect.left) - rect.left) / safeWidth) * width, 0, width),
      y: sceneClamp(((sceneNumber(event && event.clientY, rect.top) - rect.top) / safeHeight) * height, 0, height),
    };
  }

  function sceneLocalPointerSample(event, canvas, width, height, state, phase) {
    const previousX = state.lastX == null ? width / 2 : state.lastX;
    const previousY = state.lastY == null ? height / 2 : state.lastY;
    const hasPointerPosition = Number.isFinite(sceneNumber(event && event.clientX, NaN)) && Number.isFinite(sceneNumber(event && event.clientY, NaN));
    const point = hasPointerPosition ? sceneLocalPointerPoint(event, canvas, width, height) : { x: previousX, y: previousY };
    const sample = {
      x: point.x,
      y: point.y,
      deltaX: point.x - previousX,
      deltaY: point.y - previousY,
      buttons: phase === "end" ? 0 : 1,
      button: phase === "start" || phase === "end" ? 0 : null,
      active: phase !== "end",
    };
    state.lastX = point.x;
    state.lastY = point.y;
    return sample;
  }

  function resetScenePointerSample(width, height, state) {
    state.lastX = width / 2;
    state.lastY = height / 2;
    publishPointerSignals({
      x: state.lastX,
      y: state.lastY,
      deltaX: 0,
      deltaY: 0,
      buttons: 0,
      button: 0,
      active: false,
    });
  }

  function sceneDragSignalNamespace(props) {
    const value = props && props.dragSignalNamespace;
    return typeof value === "string" ? value.trim() : "";
  }

  function publishSceneDragSignals(namespace, state, active) {
    if (!namespace) {
      return;
    }
    queueInputSignal(namespace + ".x", sceneNumber(state.orbitX, 0));
    queueInputSignal(namespace + ".y", sceneNumber(state.orbitY, 0));
    queueInputSignal(namespace + ".targetIndex", Math.max(-1, Math.floor(sceneNumber(state.targetIndex, -1))));
    queueInputSignal(namespace + ".active", Boolean(active));
  }

  function sceneBoundsSize(bounds) {
    if (!bounds || typeof bounds !== "object") return [0, 0, 0];
    return [
      Math.abs(sceneNumber(bounds.maxX, 0) - sceneNumber(bounds.minX, 0)),
      Math.abs(sceneNumber(bounds.maxY, 0) - sceneNumber(bounds.minY, 0)),
      Math.abs(sceneNumber(bounds.maxZ, 0) - sceneNumber(bounds.minZ, 0)),
    ].sort(function(a, b) { return b - a; });
  }

  function sceneObjectAllowsPointerDrag(object) {
    if (!object || object.kind === "plane" || object.viewCulled) {
      return false;
    }
    const extents = sceneBoundsSize(object.bounds);
    return extents[0] > 0.6 && extents[1] > 0.35;
  }

  function sceneWorldPointAt(source, vertexIndex) {
    if (!source || typeof source.length !== "number") {
      return null;
    }
    const offset = Math.max(0, vertexIndex * 3);
    if (offset + 2 >= source.length) {
      return null;
    }
    return {
      x: sceneNumber(source[offset], 0),
      y: sceneNumber(source[offset + 1], 0),
      z: sceneNumber(source[offset + 2], 0),
    };
  }

  function sceneProjectedObjectSegments(bundle, object, width, height) {
    if (!bundle || !bundle.camera || !object) {
      return [];
    }
    const vertexOffset = Math.max(0, Math.floor(sceneNumber(object.vertexOffset, 0)));
    const vertexCount = Math.max(0, Math.floor(sceneNumber(object.vertexCount, 0)));
    if (vertexCount < 2) {
      return [];
    }
    const source = bundle.worldPositions;
    if (!source || typeof source.length !== "number") {
      return [];
    }
    const segments = [];
    for (let i = 0; i + 1 < vertexCount; i += 2) {
      const fromWorld = sceneWorldPointAt(source, vertexOffset + i);
      const toWorld = sceneWorldPointAt(source, vertexOffset + i + 1);
      if (!fromWorld || !toWorld) {
        continue;
      }
      const from = projectPoint(fromWorld, bundle.camera, width, height);
      const to = projectPoint(toWorld, bundle.camera, width, height);
      if (!from || !to) {
        continue;
      }
      segments.push([from, to]);
    }
    return segments;
  }

  function sceneProjectedSegmentsBounds(segments) {
    if (!Array.isArray(segments) || !segments.length) {
      return null;
    }
    let minX = segments[0][0].x;
    let maxX = segments[0][0].x;
    let minY = segments[0][0].y;
    let maxY = segments[0][0].y;
    for (const segment of segments) {
      for (const point of segment) {
        minX = Math.min(minX, point.x);
        maxX = Math.max(maxX, point.x);
        minY = Math.min(minY, point.y);
        maxY = Math.max(maxY, point.y);
      }
    }
    return { minX, maxX, minY, maxY };
  }

  function scenePointerPadding(bounds) {
    if (!bounds) {
      return 12;
    }
    const span = Math.max(bounds.maxX - bounds.minX, bounds.maxY - bounds.minY);
    return sceneClamp(span * 0.08, 12, 22);
  }

  function sceneDistanceToSegment(point, from, to) {
    const deltaX = to.x - from.x;
    const deltaY = to.y - from.y;
    const lengthSquared = deltaX * deltaX + deltaY * deltaY;
    if (lengthSquared <= 0.0001) {
      return Math.hypot(point.x - from.x, point.y - from.y);
    }
    const t = sceneClamp(((point.x - from.x) * deltaX + (point.y - from.y) * deltaY) / lengthSquared, 0, 1);
    const closestX = from.x + deltaX * t;
    const closestY = from.y + deltaY * t;
    return Math.hypot(point.x - closestX, point.y - closestY);
  }

  function sceneProjectedObjectHull(segments) {
    const points = [];
    const seen = new Set();
    for (const segment of segments) {
      for (const point of segment) {
        const key = point.x.toFixed(3) + ":" + point.y.toFixed(3);
        if (seen.has(key)) {
          continue;
        }
        seen.add(key);
        points.push({ x: point.x, y: point.y });
      }
    }
    if (points.length < 3) {
      return points;
    }
    points.sort(function(a, b) {
      return a.x === b.x ? a.y - b.y : a.x - b.x;
    });
    const lower = [];
    for (const point of points) {
      while (lower.length >= 2 && sceneTurnDirection(lower[lower.length - 2], lower[lower.length - 1], point) <= 0) {
        lower.pop();
      }
      lower.push(point);
    }
    const upper = [];
    for (let i = points.length - 1; i >= 0; i -= 1) {
      const point = points[i];
      while (upper.length >= 2 && sceneTurnDirection(upper[upper.length - 2], upper[upper.length - 1], point) <= 0) {
        upper.pop();
      }
      upper.push(point);
    }
    lower.pop();
    upper.pop();
    return lower.concat(upper);
  }

  function sceneTurnDirection(a, b, c) {
    return (b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x);
  }

  function scenePointInPolygon(point, polygon) {
    if (!Array.isArray(polygon) || polygon.length < 3) {
      return false;
    }
    let inside = false;
    for (let i = 0, j = polygon.length - 1; i < polygon.length; j = i, i += 1) {
      const xi = polygon[i].x;
      const yi = polygon[i].y;
      const xj = polygon[j].x;
      const yj = polygon[j].y;
      const intersects = ((yi > point.y) !== (yj > point.y)) &&
        (point.x < ((xj - xi) * (point.y - yi)) / ((yj - yi) || 0.000001) + xi);
      if (intersects) {
        inside = !inside;
      }
    }
    return inside;
  }

  function sceneObjectDepthCenter(object, camera) {
    const bounds = object && object.bounds;
    if (!bounds) {
      return sceneNumber(camera && camera.z, 6);
    }
    const minZ = sceneNumber(bounds.minZ, 0);
    const maxZ = sceneNumber(bounds.maxZ, minZ);
    return ((minZ + maxZ) / 2) + sceneNumber(camera && camera.z, 6);
  }

  function sceneObjectPointerCapture(bundle, object, point, width, height) {
    const segments = sceneProjectedObjectSegments(bundle, object, width, height);
    if (!segments.length) {
      return null;
    }
    const bounds = sceneProjectedSegmentsBounds(segments);
    if (!bounds) {
      return null;
    }
    const padding = scenePointerPadding(bounds);
    if (
      point.x < bounds.minX - padding ||
      point.x > bounds.maxX + padding ||
      point.y < bounds.minY - padding ||
      point.y > bounds.maxY + padding
    ) {
      return null;
    }
    let minDistance = Number.POSITIVE_INFINITY;
    for (const segment of segments) {
      minDistance = Math.min(minDistance, sceneDistanceToSegment(point, segment[0], segment[1]));
    }
    const inside = scenePointInPolygon(point, sceneProjectedObjectHull(segments));
    if (!inside && minDistance > padding) {
      return null;
    }
    return {
      inside,
      distance: inside ? 0 : minDistance,
      depth: sceneObjectDepthCenter(object, bundle.camera),
      area: Math.max(1, (bounds.maxX - bounds.minX) * (bounds.maxY - bounds.minY)),
    };
  }

  function scenePointerCaptureIsBetter(candidate, current) {
    if (!current) {
      return true;
    }
    if (candidate.inside !== current.inside) {
      return candidate.inside;
    }
    if (Math.abs(candidate.distance - current.distance) > 0.5) {
      return candidate.distance < current.distance;
    }
    if (Math.abs(candidate.depth - current.depth) > 0.01) {
      return candidate.depth < current.depth;
    }
    return candidate.area < current.area;
  }

  function sceneBundlePointerDragTarget(bundle, point, width, height) {
    if (!bundle || !bundle.camera || !Array.isArray(bundle.objects) || !bundle.objects.length) {
      return null;
    }
    let best = null;
    for (let index = 0; index < bundle.objects.length; index += 1) {
      const object = bundle.objects[index];
      if (!sceneObjectAllowsPointerDrag(object)) {
        continue;
      }
      const capture = sceneObjectPointerCapture(bundle, object, point, width, height);
      if (!capture) {
        continue;
      }
      const candidate = {
        index,
        object,
        inside: capture.inside,
        distance: capture.distance,
        depth: capture.depth,
        area: capture.area,
      };
      if (scenePointerCaptureIsBetter(candidate, best)) {
        best = candidate;
      }
    }
    return best;
  }

  function sceneViewportValue(viewport, key, fallback) {
    return sceneNumber(viewport && viewport[key], fallback);
  }

  function sceneDragViewportMetrics(readViewport, initialWidth, initialHeight) {
    const viewport = typeof readViewport === "function" ? readViewport() : null;
    return {
      width: Math.max(1, sceneViewportValue(viewport, "cssWidth", initialWidth)),
      height: Math.max(1, sceneViewportValue(viewport, "cssHeight", initialHeight)),
    };
  }

  function createSceneDragState(initialWidth, initialHeight) {
    return {
      active: false,
      orbitX: 0,
      orbitY: 0,
      pointerId: null,
      targetIndex: -1,
      lastX: initialWidth / 2,
      lastY: initialHeight / 2,
    };
  }

  function sceneDragMatchesActivePointer(state, event) {
    if (!state.active || state.pointerId == null) {
      return state.active;
    }
    if (!event || event.type === "lostpointercapture") {
      return true;
    }
    if (event.pointerId == null) {
      return true;
    }
    return event.pointerId === state.pointerId;
  }

  function scenePointerCanStartDrag(state, event) {
    if (state.active) {
      return false;
    }
    if (!event) {
      return false;
    }
    if (event.pointerType === "mouse") {
      return event.button === 0;
    }
    return event.button == null || event.button === 0;
  }

  function sceneDragTargetAtEvent(event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight) {
    const metrics = sceneDragViewportMetrics(readViewport, initialWidth, initialHeight);
    const pointer = sceneLocalPointerPoint(event, canvas, metrics.width, metrics.height);
    return sceneBundlePointerDragTarget(readSceneBundle && readSceneBundle(), pointer, metrics.width, metrics.height);
  }

  function updateSceneDragOrbit(state, sample, width, height) {
    state.orbitX = sceneClamp(state.orbitX + sample.deltaX / Math.max(width / 2, 1), -1.35, 1.35);
    state.orbitY = sceneClamp(state.orbitY - sample.deltaY / Math.max(height / 2, 1), -1.1, 1.1);
  }

  function publishSceneDragInteraction(canvas, event, phase, state, dragNamespace, readViewport, initialWidth, initialHeight) {
    const metrics = sceneDragViewportMetrics(readViewport, initialWidth, initialHeight);
    const sample = sceneLocalPointerSample(event, canvas, metrics.width, metrics.height, state, phase);
    if (!dragNamespace) {
      publishPointerSignals(sample);
      return;
    }
    if (phase === "move") {
      updateSceneDragOrbit(state, sample, metrics.width, metrics.height);
    }
    publishSceneDragSignals(dragNamespace, state, phase !== "end");
  }

  function resetSceneDragInteraction(state, dragNamespace, readViewport, initialWidth, initialHeight) {
    state.pointerId = null;
    state.targetIndex = -1;
    if (dragNamespace) {
      return;
    }
    const metrics = sceneDragViewportMetrics(readViewport, initialWidth, initialHeight);
    resetScenePointerSample(metrics.width, metrics.height, state);
  }

  function setupSceneDragInteractions(canvas, props, readViewport, readSceneBundle) {
    if (!canvas || !sceneBool(props.dragToRotate, false)) {
      return { dispose() {} };
    }

    const dragNamespace = sceneDragSignalNamespace(props);
    const initialMetrics = sceneDragViewportMetrics(readViewport, sceneNumber(props.width, 720), sceneNumber(props.height, 420));
    const initialWidth = initialMetrics.width;
    const initialHeight = initialMetrics.height;
    const state = createSceneDragState(initialWidth, initialHeight);
    let documentListenersAttached = false;

    canvas.style.cursor = "grab";
    canvas.style.touchAction = "none";

    function attachDocumentListeners() {
      if (documentListenersAttached) {
        return;
      }
      documentListenersAttached = true;
      document.addEventListener("pointermove", onPointerMove);
      document.addEventListener("pointerup", finishDrag);
      document.addEventListener("pointercancel", finishDrag);
    }

    function detachDocumentListeners() {
      if (!documentListenersAttached) {
        return;
      }
      documentListenersAttached = false;
      document.removeEventListener("pointermove", onPointerMove);
      document.removeEventListener("pointerup", finishDrag);
      document.removeEventListener("pointercancel", finishDrag);
    }

    function onPointerDown(event) {
      if (!scenePointerCanStartDrag(state, event)) {
        return;
      }
      const target = sceneDragTargetAtEvent(event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight);
      if (!target) {
        return;
      }
      state.active = true;
      state.pointerId = event.pointerId;
      state.targetIndex = target.index;
      canvas.style.cursor = "grabbing";
      attachDocumentListeners();
      if (typeof canvas.setPointerCapture === "function") {
        canvas.setPointerCapture(event.pointerId);
      }
      event.preventDefault();
      event.stopPropagation();
      publishSceneDragInteraction(canvas, event, "start", state, dragNamespace, readViewport, initialWidth, initialHeight);
    }

    function onPointerMove(event) {
      if (!sceneDragMatchesActivePointer(state, event)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      publishSceneDragInteraction(canvas, event, "move", state, dragNamespace, readViewport, initialWidth, initialHeight);
    }

    function finishDrag(event) {
      if (!sceneDragMatchesActivePointer(state, event)) {
        return;
      }
      const wasActive = state.active;
      state.active = false;
      canvas.style.cursor = "grab";
      detachDocumentListeners();
      if (!wasActive) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      if (state.pointerId != null && typeof canvas.releasePointerCapture === "function") {
        try {
          canvas.releasePointerCapture(state.pointerId);
        } catch (_) {}
      }
      state.pointerId = null;
      state.targetIndex = -1;
      publishSceneDragInteraction(canvas, event, "end", state, dragNamespace, readViewport, initialWidth, initialHeight);
      resetSceneDragInteraction(state, dragNamespace, readViewport, initialWidth, initialHeight);
    }

    canvas.addEventListener("pointerdown", onPointerDown);
    canvas.addEventListener("pointermove", onPointerMove);
    canvas.addEventListener("pointerup", finishDrag);
    canvas.addEventListener("pointercancel", finishDrag);
    canvas.addEventListener("lostpointercapture", finishDrag);

    return {
      dispose() {
        canvas.removeEventListener("pointerdown", onPointerDown);
        canvas.removeEventListener("pointermove", onPointerMove);
        canvas.removeEventListener("pointerup", finishDrag);
        canvas.removeEventListener("pointercancel", finishDrag);
        canvas.removeEventListener("lostpointercapture", finishDrag);
        detachDocumentListeners();
        canvas.style.cursor = "";
        canvas.style.touchAction = "";
        if (state.active && dragNamespace) {
          state.active = false;
          state.pointerId = null;
          state.targetIndex = -1;
          publishSceneDragSignals(dragNamespace, state, false);
        } else {
          state.active = false;
        }
        resetSceneDragInteraction(state, dragNamespace, readViewport, initialWidth, initialHeight);
      },
    };
  }

  function publishGamepadSignals(pad) {
    const axes = Array.isArray(pad.axes) ? pad.axes : [];
    queueInputSignal("$input.gamepad0.connected", true);
    queueInputSignal("$input.gamepad0.leftX", sceneNumber(axes[0], 0));
    queueInputSignal("$input.gamepad0.leftY", sceneNumber(axes[1], 0));
    queueInputSignal("$input.gamepad0.rightX", sceneNumber(axes[2], 0));
    queueInputSignal("$input.gamepad0.rightY", sceneNumber(axes[3], 0));
    queueInputSignal("$input.gamepad0.buttonA", gamepadButtonPressed(pad, 0));
    queueInputSignal("$input.gamepad0.buttonB", gamepadButtonPressed(pad, 1));
  }

  function gamepadButtonPressed(pad, index) {
    return Boolean(pad && pad.buttons && pad.buttons[index] && pad.buttons[index].pressed);
  }

  function sceneNumber(value, fallback) {
    const num = Number(value);
    return Number.isFinite(num) ? num : fallback;
  }

  function sceneBool(value, fallback) {
    if (typeof value === "boolean") return value;
    if (typeof value === "string") {
      const lowered = value.trim().toLowerCase();
      if (lowered === "true") return true;
      if (lowered === "false") return false;
    }
    return fallback;
  }

  function defaultSceneObjects() {
    return [
      {
        kind: "cube",
        size: 1.8,
        x: -1.1,
        y: 0.3,
        z: 0,
        color: "#8de1ff",
        spinX: 0.42,
        spinY: 0.74,
        spinZ: 0.16,
      },
      {
        kind: "cube",
        size: 1.1,
        x: 1.6,
        y: -0.7,
        z: 1.4,
        color: "#ffd48f",
        spinX: -0.24,
        spinY: 0.48,
        spinZ: 0.12,
      },
    ];
  }

  function rawSceneObjects(props) {
    const scene = sceneProps(props);
    return sceneObjectList(scene && scene.objects) || sceneObjectList(props && props.objects) || defaultSceneObjects();
  }

  function rawSceneLabels(props) {
    const scene = sceneProps(props);
    if (scene && Array.isArray(scene.labels)) {
      return scene.labels;
    }
    return props && Array.isArray(props.labels) ? props.labels : [];
  }

  function sceneProps(props) {
    return props && props.scene && typeof props.scene === "object" ? props.scene : null;
  }

  function sceneObjectList(value) {
    return Array.isArray(value) && value.length > 0 ? value : null;
  }

  function sceneObjectMaterialSource(item) {
    return item && item.material && typeof item.material === "object" ? item.material : null;
  }

  function sceneObjectMaterialKindValue(item) {
    if (!item || typeof item !== "object") {
      return "";
    }
    if (typeof item.material === "string" && item.material.trim()) {
      return item.material.trim();
    }
    if (typeof item.materialKind === "string" && item.materialKind.trim()) {
      return item.materialKind.trim();
    }
    const material = sceneObjectMaterialSource(item);
    if (material && typeof material.kind === "string" && material.kind.trim()) {
      return material.kind.trim();
    }
    return "";
  }

  function sceneObjectMaterialValue(item, name) {
    if (!item || typeof item !== "object") {
      return undefined;
    }
    const material = sceneObjectMaterialSource(item);
    if (material && Object.prototype.hasOwnProperty.call(material, name)) {
      return material[name];
    }
    return Object.prototype.hasOwnProperty.call(item, name) ? item[name] : undefined;
  }

  function sceneObjectBlendModeValue(item) {
    const direct = sceneObjectMaterialValue(item, "blendMode");
    if (direct !== undefined) {
      return direct;
    }
    const material = sceneObjectMaterialSource(item);
    if (material && Object.prototype.hasOwnProperty.call(material, "blend")) {
      return material.blend;
    }
    return item && Object.prototype.hasOwnProperty.call(item, "blend") ? item.blend : undefined;
  }

  function normalizeSceneMaterialKind(value) {
    const kind = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (kind) {
      case "ghost":
      case "glass":
      case "glow":
      case "matte":
        return kind;
      default:
        return "flat";
    }
  }

  function sceneDefaultMaterialOpacity(kind) {
    switch (normalizeSceneMaterialKind(kind)) {
      case "ghost":
        return 0.42;
      case "glass":
        return 0.28;
      case "glow":
        return 0.92;
      default:
        return 1;
    }
  }

  function sceneDefaultMaterialEmissive(kind) {
    switch (normalizeSceneMaterialKind(kind)) {
      case "ghost":
        return 0.12;
      case "glass":
        return 0.08;
      case "glow":
        return 0.42;
      default:
        return 0;
    }
  }

  function sceneDefaultMaterialBlendMode(kind) {
    switch (normalizeSceneMaterialKind(kind)) {
      case "ghost":
      case "glass":
        return "alpha";
      case "glow":
        return "additive";
      default:
        return "opaque";
    }
  }

  function normalizeSceneMaterialBlendMode(value, kind, opacity) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "opaque":
      case "solid":
        return "opaque";
      case "alpha":
      case "transparent":
      case "translucent":
        return "alpha";
      case "add":
      case "additive":
      case "glow":
      case "emissive":
        return "additive";
      default: {
        const fallback = sceneDefaultMaterialBlendMode(kind);
        if (fallback !== "opaque") {
          return fallback;
        }
        return opacity < 0.999 ? "alpha" : "opaque";
      }
    }
  }

  function normalizeSceneMaterialRenderPass(value, blendMode, opacity) {
    const pass = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (pass) {
      case "opaque":
      case "alpha":
      case "additive":
        return pass;
      case "add":
        return "additive";
      case "transparent":
      case "translucent":
        return "alpha";
      default:
        if (blendMode === "additive") {
          return "additive";
        }
        return blendMode === "alpha" || opacity < 0.999 ? "alpha" : "opaque";
    }
  }

  function normalizeSceneObject(object, index) {
    const item = object && typeof object === "object" ? object : {};
    const size = sceneNumber(item.size, 1.2);
    const materialKind = normalizeSceneMaterialKind(sceneObjectMaterialKindValue(item));
    const materialColor = sceneObjectMaterialValue(item, "color");
    const opacity = clamp01(sceneNumber(sceneObjectMaterialValue(item, "opacity"), sceneDefaultMaterialOpacity(materialKind)));
    const blendMode = normalizeSceneMaterialBlendMode(sceneObjectBlendModeValue(item), materialKind, opacity);
    const normalized = {
      id: item.id || ("scene-object-" + index),
      kind: normalizeSceneKind(item.kind),
      size,
      width: sceneNumber(item.width, size),
      height: sceneNumber(item.height, size),
      depth: sceneNumber(item.depth, size),
      radius: sceneNumber(item.radius, size / 2),
      segments: sceneSegmentResolution(item.segments),
      x: sceneNumber(item.x, 0),
      y: sceneNumber(item.y, 0),
      z: sceneNumber(item.z, 0),
      materialKind,
      color: typeof materialColor === "string" && materialColor ? materialColor : "#8de1ff",
      opacity,
      emissive: clamp01(sceneNumber(sceneObjectMaterialValue(item, "emissive"), sceneDefaultMaterialEmissive(materialKind))),
      blendMode,
      renderPass: normalizeSceneMaterialRenderPass(sceneObjectMaterialValue(item, "renderPass"), blendMode, opacity),
      wireframe: sceneBool(sceneObjectMaterialValue(item, "wireframe"), true),
      rotationX: sceneNumber(item.rotationX, 0),
      rotationY: sceneNumber(item.rotationY, 0),
      rotationZ: sceneNumber(item.rotationZ, 0),
      spinX: sceneNumber(item.spinX, 0),
      spinY: sceneNumber(item.spinY, 0),
      spinZ: sceneNumber(item.spinZ, 0),
      shiftX: sceneNumber(item.shiftX, 0),
      shiftY: sceneNumber(item.shiftY, 0),
      shiftZ: sceneNumber(item.shiftZ, 0),
      driftSpeed: sceneNumber(item.driftSpeed, 0),
      driftPhase: sceneNumber(item.driftPhase, 0),
      viewCulled: sceneBool(item.viewCulled, false),
    };
    normalized.static = sceneBool(item.static, !sceneObjectAnimated(normalized));
    return normalized;
  }

  function normalizeSceneLabel(label, index) {
    const item = label && typeof label === "object" ? label : {};
    return {
      id: item.id || ("scene-label-" + index),
      text: typeof item.text === "string" ? item.text : "",
      className: sceneLabelClassName(item),
      x: sceneNumber(item.x, 0),
      y: sceneNumber(item.y, 0),
      z: sceneNumber(item.z, 0),
      priority: sceneNumber(item.priority, 0),
      shiftX: sceneNumber(item.shiftX, 0),
      shiftY: sceneNumber(item.shiftY, 0),
      shiftZ: sceneNumber(item.shiftZ, 0),
      driftSpeed: sceneNumber(item.driftSpeed, 0),
      driftPhase: sceneNumber(item.driftPhase, 0),
      maxWidth: Math.max(48, sceneNumber(item.maxWidth, 180)),
      maxLines: Math.max(0, Math.floor(sceneNumber(item.maxLines, 0))),
      overflow: normalizeTextLayoutOverflow(item.overflow),
      font: typeof item.font === "string" && item.font ? item.font : '600 13px "IBM Plex Sans", "Segoe UI", sans-serif',
      lineHeight: Math.max(12, sceneNumber(item.lineHeight, 18)),
      color: typeof item.color === "string" && item.color ? item.color : "#ecf7ff",
      background: typeof item.background === "string" && item.background ? item.background : "rgba(8, 21, 31, 0.82)",
      borderColor: typeof item.borderColor === "string" && item.borderColor ? item.borderColor : "rgba(141, 225, 255, 0.24)",
      offsetX: sceneNumber(item.offsetX, 0),
      offsetY: sceneNumber(item.offsetY, -14),
      anchorX: Math.max(0, Math.min(1, sceneNumber(item.anchorX, 0.5))),
      anchorY: Math.max(0, Math.min(1, sceneNumber(item.anchorY, 1))),
      collision: normalizeSceneLabelCollision(item.collision),
      occlude: sceneBool(item.occlude, false),
      whiteSpace: normalizeSceneLabelWhiteSpace(item.whiteSpace),
      textAlign: normalizeSceneLabelAlign(item.textAlign),
    };
  }

  function sceneLabelClassName(item) {
    if (!item || typeof item !== "object") {
      return "";
    }
    if (typeof item.className === "string" && item.className.trim()) {
      return item.className.trim();
    }
    if (typeof item.class === "string" && item.class.trim()) {
      return item.class.trim();
    }
    return "";
  }

  function normalizeSceneKind(value) {
    const kind = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (kind) {
      case "box":
      case "plane":
      case "pyramid":
      case "sphere":
        return kind;
      default:
        return "cube";
    }
  }

  function sceneSegmentResolution(value) {
    const segments = Math.round(sceneNumber(value, 12));
    return Math.max(6, Math.min(24, segments));
  }

  function sceneObjects(props) {
    return rawSceneObjects(props).map(function(object, index) {
      return normalizeSceneObject(object, index);
    });
  }

  function normalizeSceneLabelWhiteSpace(value) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "pre-wrap":
        return "pre-wrap";
      case "pre":
        return "pre";
      default:
        return "normal";
    }
  }

  function normalizeSceneLabelAlign(value) {
    const align = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (align) {
      case "left":
      case "start":
        return "left";
      case "right":
      case "end":
        return "right";
      default:
        return "center";
    }
  }

  function normalizeSceneLabelCollision(value) {
    const mode = typeof value === "string" ? value.trim().toLowerCase() : "";
    switch (mode) {
      case "allow":
      case "none":
      case "overlap":
        return "allow";
      default:
        return "avoid";
    }
  }

  function sceneLabels(props) {
    const raw = rawSceneLabels(props);
    return raw
      .map(function(label, index) {
        return normalizeSceneLabel(label, index);
      })
      .filter(function(label) {
        return label.text.trim() !== "";
      });
  }

  function sceneCamera(props) {
    const raw = props && props.camera && typeof props.camera === "object" ? props.camera : {};
    return normalizeSceneCamera(raw, {
      x: 0,
      y: 0,
      z: 6,
      fov: 75,
      near: 0.05,
      far: 128,
    });
  }

  function normalizeSceneCamera(raw, fallback) {
    const base = fallback || {};
    return {
      x: sceneNumber(raw.x, sceneNumber(base.x, 0)),
      y: sceneNumber(raw.y, sceneNumber(base.y, 0)),
      z: sceneNumber(raw.z, sceneNumber(base.z, 6)),
      fov: sceneNumber(raw.fov, sceneNumber(base.fov, 75)),
      near: sceneNumber(raw.near, sceneNumber(base.near, 0.05)),
      far: sceneNumber(raw.far, sceneNumber(base.far, 128)),
    };
  }

  function createSceneState(props) {
    const state = {
      background: typeof props.background === "string" && props.background ? props.background : "#08151f",
      camera: sceneCamera(props),
      objects: new Map(),
      labels: new Map(),
    };
    for (const object of sceneObjects(props)) {
      state.objects.set(object.id, object);
    }
    for (const label of sceneLabels(props)) {
      state.labels.set(label.id, label);
    }
    return state;
  }

  function sceneStateObjects(state) {
    return Array.from(state.objects.values());
  }

  function sceneStateLabels(state) {
    return Array.from(state.labels.values());
  }

  function sceneObjectAnimated(object) {
    if (!object || typeof object !== "object") {
      return false;
    }
    if (sceneNumber(object.spinX, 0) !== 0 || sceneNumber(object.spinY, 0) !== 0 || sceneNumber(object.spinZ, 0) !== 0) {
      return true;
    }
    if (sceneNumber(object.driftSpeed, 0) === 0) {
      return false;
    }
    return sceneNumber(object.shiftX, 0) !== 0 || sceneNumber(object.shiftY, 0) !== 0 || sceneNumber(object.shiftZ, 0) !== 0;
  }

  function sceneLabelAnimated(label) {
    if (!label || typeof label !== "object") {
      return false;
    }
    if (sceneNumber(label.driftSpeed, 0) === 0) {
      return false;
    }
    return sceneNumber(label.shiftX, 0) !== 0 || sceneNumber(label.shiftY, 0) !== 0 || sceneNumber(label.shiftZ, 0) !== 0;
  }

  const SCENE_CMD_CREATE_OBJECT = 0;
  const SCENE_CMD_REMOVE_OBJECT = 1;
  const SCENE_CMD_SET_TRANSFORM = 2;
  const SCENE_CMD_SET_MATERIAL = 3;
  const SCENE_CMD_SET_LIGHT = 4;
  const SCENE_CMD_SET_CAMERA = 5;
  const SCENE_CMD_SET_PARTICLES = 6;

  function applySceneCommands(state, commands) {
    if (!state || !Array.isArray(commands) || commands.length === 0) return;
    for (const command of commands) {
      applySceneCommand(state, command);
    }
  }

  function applySceneCommand(state, command) {
    if (!command || typeof command !== "object") return;
    switch (command.kind) {
      case SCENE_CMD_CREATE_OBJECT:
        applySceneCreateCommand(state, command.objectId, command.data);
        return;
      case SCENE_CMD_REMOVE_OBJECT:
        state.objects.delete(sceneObjectKey(command.objectId));
        state.labels.delete(sceneObjectKey(command.objectId));
        return;
      case SCENE_CMD_SET_TRANSFORM:
      case SCENE_CMD_SET_MATERIAL:
        applySceneObjectPatch(state, command.objectId, command.data);
        return;
      case SCENE_CMD_SET_CAMERA:
        state.camera = normalizeSceneCamera(command.data || {}, state.camera);
        return;
      case SCENE_CMD_SET_LIGHT:
      case SCENE_CMD_SET_PARTICLES:
      default:
        return;
    }
  }

  function applySceneCreateCommand(state, objectID, payload) {
    if (!payload || typeof payload !== "object") return;
    if (payload.kind === "camera") {
      state.camera = normalizeSceneCamera(payload.props || {}, state.camera);
      return;
    }
    if (payload.kind === "light" || payload.kind === "particles") {
      return;
    }
    if (payload.kind === "label") {
      const label = sceneLabelFromPayload(objectID, payload, state.labels.get(sceneObjectKey(objectID)));
      if (label) {
        state.labels.set(sceneObjectKey(objectID), label);
      }
      return;
    }
    const key = sceneObjectKey(objectID);
    const next = sceneObjectFromPayload(objectID, payload, state.objects.get(key));
    if (next) {
      state.objects.set(key, next);
    }
  }

  function applySceneObjectPatch(state, objectID, patch) {
    const key = sceneObjectKey(objectID);
    const current = state.objects.get(key);
    if (current) {
      const next = sceneObjectFromPayload(objectID, {
        geometry: current.kind,
        props: Object.assign({}, current, patch || {}),
      }, current);
      if (next) {
        state.objects.set(key, next);
      }
      return;
    }
    const currentLabel = state.labels.get(key);
    if (!currentLabel) return;
    const nextLabel = sceneLabelFromPayload(objectID, {
      props: Object.assign({}, currentLabel, patch || {}),
    }, currentLabel);
    if (nextLabel) {
      state.labels.set(key, nextLabel);
    }
  }

  function sceneObjectKey(objectID) {
    return String(objectID);
  }

  function sceneObjectFromPayload(objectID, payload, fallback) {
    const current = fallback && typeof fallback === "object" ? fallback : {};
    const props = payload && payload.props && typeof payload.props === "object" ? payload.props : {};
    const geometry = payload && typeof payload.geometry === "string" && payload.geometry ? payload.geometry : current.kind;
    const merged = Object.assign({}, current, props);
    merged.id = current.id || merged.id || ("scene-object-" + objectID);
    merged.kind = normalizeSceneKind(merged.kind || geometry);
    return normalizeSceneObject(merged, objectID);
  }

  function sceneLabelFromPayload(objectID, payload, fallback) {
    const current = fallback && typeof fallback === "object" ? fallback : {};
    const props = payload && payload.props && typeof payload.props === "object" ? payload.props : {};
    const merged = Object.assign({}, current, props);
    merged.id = current.id || merged.id || ("scene-label-" + objectID);
    const label = normalizeSceneLabel(merged, objectID);
    if (!label.text.trim()) {
      return null;
    }
    return label;
  }

  function clearChildren(node) {
    while (node && node.firstChild) {
      node.removeChild(node.firstChild);
    }
  }

  function boxVertices(width, height, depth) {
    const halfWidth = width / 2;
    const halfHeight = height / 2;
    const halfDepth = depth / 2;
    return [
      { x: -halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: halfHeight, z: -halfDepth },
      { x: -halfWidth, y: halfHeight, z: -halfDepth },
      { x: -halfWidth, y: -halfHeight, z: halfDepth },
      { x: halfWidth, y: -halfHeight, z: halfDepth },
      { x: halfWidth, y: halfHeight, z: halfDepth },
      { x: -halfWidth, y: halfHeight, z: halfDepth },
    ];
  }

  const boxEdgePairs = [
    [0, 1], [1, 2], [2, 3], [3, 0],
    [4, 5], [5, 6], [6, 7], [7, 4],
    [0, 4], [1, 5], [2, 6], [3, 7],
  ];

  function indexSegments(points, edgePairs) {
    return edgePairs.map(function(edge) {
      return [points[edge[0]], points[edge[1]]];
    });
  }

  function boxSegments(object) {
    return indexSegments(boxVertices(object.width, object.height, object.depth), boxEdgePairs);
  }

  function planeSegments(object) {
    const vertices = boxVertices(object.width, 0, object.depth);
    return indexSegments(vertices.slice(0, 4), [
      [0, 1], [1, 2], [2, 3], [3, 0],
    ]);
  }

  function pyramidSegments(object) {
    const halfWidth = object.width / 2;
    const halfDepth = object.depth / 2;
    const halfHeight = object.height / 2;
    const vertices = [
      { x: -halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: -halfHeight, z: halfDepth },
      { x: -halfWidth, y: -halfHeight, z: halfDepth },
      { x: 0, y: halfHeight, z: 0 },
    ];
    return indexSegments(vertices, [
      [0, 1], [1, 2], [2, 3], [3, 0],
      [0, 4], [1, 4], [2, 4], [3, 4],
    ]);
  }

  function circleSegments(radius, axis, segments) {
    const points = [];
    for (let i = 0; i < segments; i += 1) {
      const angle = (Math.PI * 2 * i) / segments;
      points.push(circlePoint(radius, axis, angle));
    }
    const out = [];
    for (let i = 0; i < points.length; i += 1) {
      out.push([points[i], points[(i + 1) % points.length]]);
    }
    return out;
  }

  function circlePoint(radius, axis, angle) {
    const sin = Math.sin(angle) * radius;
    const cos = Math.cos(angle) * radius;
    switch (axis) {
      case "xy":
        return { x: cos, y: sin, z: 0 };
      case "yz":
        return { x: 0, y: cos, z: sin };
      default:
        return { x: cos, y: 0, z: sin };
    }
  }

  function sphereSegments(object) {
    return []
      .concat(circleSegments(object.radius, "xy", object.segments))
      .concat(circleSegments(object.radius, "xz", object.segments))
      .concat(circleSegments(object.radius, "yz", object.segments));
  }

  function sceneObjectSegments(object) {
    switch (object.kind) {
      case "box":
      case "cube":
        return boxSegments(object);
      case "plane":
        return planeSegments(object);
      case "pyramid":
        return pyramidSegments(object);
      case "sphere":
        return sphereSegments(object);
      default:
        return boxSegments(object);
    }
  }

  function rotatePoint(point, rotationX, rotationY, rotationZ) {
    let x = point.x;
    let y = point.y;
    let z = point.z;

    const sinX = Math.sin(rotationX);
    const cosX = Math.cos(rotationX);
    let nextY = y * cosX - z * sinX;
    let nextZ = y * sinX + z * cosX;
    y = nextY;
    z = nextZ;

    const sinY = Math.sin(rotationY);
    const cosY = Math.cos(rotationY);
    let nextX = x * cosY + z * sinY;
    nextZ = -x * sinY + z * cosY;
    x = nextX;
    z = nextZ;

    const sinZ = Math.sin(rotationZ);
    const cosZ = Math.cos(rotationZ);
    nextX = x * cosZ - y * sinZ;
    nextY = x * sinZ + y * cosZ;

    return { x: nextX, y: nextY, z: z };
  }

  function projectPoint(point, camera, width, height) {
    const normalizedCamera = sceneRenderCamera(camera);
    const depth = sceneNumber(point && point.z, 0) + normalizedCamera.z;
    if (depth <= normalizedCamera.near || depth >= normalizedCamera.far) return null;
    const focal = (Math.min(width, height) / 2) / Math.tan((normalizedCamera.fov * Math.PI) / 360);
    return {
      x: width / 2 + ((sceneNumber(point && point.x, 0) - normalizedCamera.x) * focal) / depth,
      y: height / 2 - ((sceneNumber(point && point.y, 0) - normalizedCamera.y) * focal) / depth,
      depth,
    };
  }

  function strokeLine(ctx2d, from, to) {
    ctx2d.beginPath();
    ctx2d.moveTo(from.x, from.y);
    ctx2d.lineTo(to.x, to.y);
    ctx2d.stroke();
  }

  function sceneColorRGBA(value, fallback) {
    const base = Array.isArray(fallback) && fallback.length === 4 ? fallback.slice() : [0.55, 0.88, 1, 1];
    if (typeof value !== "string") {
      return base;
    }

    const trimmed = value.trim();
    const shortHex = trimmed.match(/^#([0-9a-f]{3})$/i);
    if (shortHex) {
      return [
        parseInt(shortHex[1][0] + shortHex[1][0], 16) / 255,
        parseInt(shortHex[1][1] + shortHex[1][1], 16) / 255,
        parseInt(shortHex[1][2] + shortHex[1][2], 16) / 255,
        1,
      ];
    }

    const fullHex = trimmed.match(/^#([0-9a-f]{6})$/i);
    if (fullHex) {
      return [
        parseInt(fullHex[1].slice(0, 2), 16) / 255,
        parseInt(fullHex[1].slice(2, 4), 16) / 255,
        parseInt(fullHex[1].slice(4, 6), 16) / 255,
        1,
      ];
    }

    const rgba = trimmed.match(/^rgba?\(([^)]+)\)$/i);
    if (rgba) {
      const parts = rgba[1].split(",").map(function(part) {
        return Number(part.trim());
      });
      if (parts.length >= 3 && parts.every(function(part, index) {
        return Number.isFinite(part) && (index < 3 || index === 3);
      })) {
        return [
          Math.max(0, Math.min(255, parts[0])) / 255,
          Math.max(0, Math.min(255, parts[1])) / 255,
          Math.max(0, Math.min(255, parts[2])) / 255,
          parts.length > 3 ? Math.max(0, Math.min(1, parts[3])) : 1,
        ];
      }
    }

    return base;
  }

  function sceneClipPoint(point, width, height) {
    return {
      x: (point.x / width) * 2 - 1,
      y: 1 - (point.y / height) * 2,
    };
  }

  function sceneRenderCamera(camera) {
    return {
      x: sceneNumber(camera && camera.x, 0),
      y: sceneNumber(camera && camera.y, 0),
      z: sceneNumber(camera && camera.z, 6),
      fov: sceneNumber(camera && camera.fov, 75),
      near: sceneNumber(camera && camera.near, 0.05),
      far: sceneNumber(camera && camera.far, 128),
    };
  }

  function sceneObjectMaterialProfile(object) {
    const kind = normalizeSceneMaterialKind(object && object.materialKind);
    const opacity = clamp01(sceneNumber(object && object.opacity, sceneDefaultMaterialOpacity(kind)));
    const profile = {
      kind,
      color: object && typeof object.color === "string" && object.color ? object.color : "#8de1ff",
      opacity,
      wireframe: sceneBool(object && object.wireframe, true),
      blendMode: normalizeSceneMaterialBlendMode(object && object.blendMode, kind, opacity),
      emissive: clamp01(sceneNumber(object && object.emissive, sceneDefaultMaterialEmissive(kind))),
    };
    profile.renderPass = normalizeSceneMaterialRenderPass(object && object.renderPass, profile.blendMode, profile.opacity);
    profile.key = sceneMaterialProfileKey(profile);
    profile.shaderData = sceneMaterialShaderData(profile);
    return profile;
  }

  function sceneMaterialProfileKey(profile) {
    return [
      normalizeSceneMaterialKind(profile && profile.kind),
      String(profile && profile.color || ""),
      clamp01(sceneNumber(profile && profile.opacity, 1)).toFixed(3),
      String(sceneBool(profile && profile.wireframe, true)),
      String(profile && profile.blendMode || "opaque"),
      String(profile && profile.renderPass || "opaque"),
      clamp01(sceneNumber(profile && profile.emissive, 0)).toFixed(3),
    ].join("|");
  }

  function sceneBundleMaterialIndex(bundle, materialLookup, profile) {
    if (!bundle || !Array.isArray(bundle.materials)) {
      return 0;
    }
    const key = profile && profile.key ? profile.key : sceneMaterialProfileKey(profile);
    if (materialLookup && materialLookup.has(key)) {
      return materialLookup.get(key);
    }
    const index = bundle.materials.length;
    bundle.materials.push(profile);
    if (materialLookup) {
      materialLookup.set(key, index);
    }
    return index;
  }

  function sceneMaterialStrokeColor(material) {
    const rgba = sceneColorRGBA(material && material.color, [0.55, 0.88, 1, 1]);
    rgba[3] = clamp01(rgba[3] * sceneMaterialOpacity(material));
    return "rgba(" +
      Math.round(rgba[0] * 255) + ", " +
      Math.round(rgba[1] * 255) + ", " +
      Math.round(rgba[2] * 255) + ", " +
      rgba[3].toFixed(3) + ")";
  }

  function sceneBoundsDepthMetrics(bounds, camera) {
    if (!bounds) {
      const depth = sceneWorldPointDepth(0, camera);
      return { near: depth, far: depth, center: depth };
    }
    const a = sceneWorldPointDepth(bounds.minZ, camera);
    const b = sceneWorldPointDepth(bounds.maxZ, camera);
    const near = Math.min(a, b);
    const far = Math.max(a, b);
    return {
      near,
      far,
      center: (near + far) / 2,
    };
  }

  function sceneBoundsViewCulled(bounds, camera) {
    if (!bounds) {
      return false;
    }
    const depth = sceneBoundsDepthMetrics(bounds, camera);
    const near = sceneNumber(camera && camera.near, 0.05);
    const far = sceneNumber(camera && camera.far, 128);
    return depth.far <= near || depth.near >= far;
  }

  function createSceneRenderBundle(width, height, background, camera, objects, labels, timeSeconds) {
    const bundle = {
      background: background,
      camera: sceneRenderCamera(camera),
      materials: [],
      objects: [],
      labels: [],
      lines: [],
      positions: [],
      colors: [],
      worldPositions: [],
      worldColors: [],
      vertexCount: 0,
      worldVertexCount: 0,
      objectCount: 0,
    };
    const materialLookup = new Map();
    appendSceneGridToBundle(bundle, width, height);
    for (const object of objects) {
      appendSceneObjectToBundle(bundle, materialLookup, camera, width, height, object, timeSeconds);
    }
    for (const label of labels || []) {
      appendSceneLabelToBundle(bundle, camera, width, height, label, timeSeconds);
    }
    bundle.positions = new Float32Array(bundle.positions);
    bundle.colors = new Float32Array(bundle.colors);
    bundle.vertexCount = bundle.positions.length / 2;
    bundle.worldPositions = new Float32Array(bundle.worldPositions);
    bundle.worldColors = new Float32Array(bundle.worldColors);
    bundle.worldVertexCount = bundle.worldPositions.length / 3;
    bundle.objectCount = bundle.objects.length;
    return bundle;
  }

  function projectSceneObject(object, camera, width, height, timeSeconds) {
    return sceneObjectSegments(object).map(function(segment) {
      return [
        projectPoint(translateScenePoint(segment[0], object, timeSeconds), camera, width, height),
        projectPoint(translateScenePoint(segment[1], object, timeSeconds), camera, width, height),
      ];
    });
  }

  function translateScenePoint(point, object, timeSeconds) {
    const rotated = rotatePoint(
      point,
      object.rotationX + object.spinX * timeSeconds,
      object.rotationY + object.spinY * timeSeconds,
      object.rotationZ + object.spinZ * timeSeconds,
    );
    const motion = sceneMotionOffset(object, timeSeconds);
    return {
      x: rotated.x + object.x + motion.x,
      y: rotated.y + object.y + motion.y,
      z: rotated.z + object.z + motion.z,
    };
  }

  function sceneMotionOffset(object, timeSeconds) {
    if (!object || (!object.shiftX && !object.shiftY && !object.shiftZ)) {
      return { x: 0, y: 0, z: 0 };
    }
    const angle = sceneNumber(object.driftPhase, 0) + timeSeconds * sceneNumber(object.driftSpeed, 0);
    return {
      x: Math.cos(angle) * sceneNumber(object.shiftX, 0),
      y: Math.sin(angle * 0.82 + sceneNumber(object.driftPhase, 0) * 0.35) * sceneNumber(object.shiftY, 0),
      z: Math.sin(angle) * sceneNumber(object.shiftZ, 0),
    };
  }

  function appendSceneGridToBundle(bundle, width, height) {
    for (let x = 0; x <= width; x += 48) {
      appendSceneLine(bundle, width, height, { x: x, y: 0 }, { x: x, y: height }, "rgba(141, 225, 255, 0.14)", 1);
    }
    for (let y = 0; y <= height; y += 48) {
      appendSceneLine(bundle, width, height, { x: 0, y: y }, { x: width, y: y }, "rgba(141, 225, 255, 0.14)", 1);
    }
  }

  function appendSceneObjectToBundle(bundle, materialLookup, camera, width, height, object, timeSeconds) {
    const worldSegments = sceneWorldObjectSegments(object, timeSeconds);
    const vertexOffset = bundle.worldPositions.length / 3;
    const material = sceneObjectMaterialProfile(object);
    const strokeColor = sceneMaterialStrokeColor(material);
    const rgba = sceneColorRGBA(material.color, [0.55, 0.88, 1, 1]);
    let bounds = null;
    let vertexCount = 0;
    for (const segment of worldSegments) {
      const fromWorld = segment[0];
      const toWorld = segment[1];
      bundle.worldPositions.push(fromWorld.x, fromWorld.y, fromWorld.z, toWorld.x, toWorld.y, toWorld.z);
      bundle.worldColors.push(rgba[0], rgba[1], rgba[2], rgba[3], rgba[0], rgba[1], rgba[2], rgba[3]);
      bounds = sceneExpandWorldBounds(bounds, fromWorld);
      bounds = sceneExpandWorldBounds(bounds, toWorld);
      vertexCount += 2;
      const from = projectPoint(fromWorld, camera, width, height);
      const to = projectPoint(toWorld, camera, width, height);
      if (!from || !to) continue;
      appendSceneLine(bundle, width, height, from, to, strokeColor, 1.8);
    }
    if (vertexCount > 0) {
      const materialIndex = sceneBundleMaterialIndex(bundle, materialLookup, material);
      const depth = sceneBoundsDepthMetrics(bounds, camera);
      bundle.objects.push({
        id: object.id,
        kind: object.kind,
        materialIndex: materialIndex,
        renderPass: sceneWorldObjectRenderPass(object, material),
        vertexOffset: vertexOffset,
        vertexCount: vertexCount,
        static: Boolean(object.static),
        bounds: bounds || {
          minX: 0,
          minY: 0,
          minZ: 0,
          maxX: 0,
          maxY: 0,
          maxZ: 0,
        },
        depthNear: depth.near,
        depthFar: depth.far,
        depthCenter: depth.center,
        viewCulled: Boolean(object.viewCulled) || sceneBoundsViewCulled(bounds, camera),
      });
    }
  }

  function sceneLabelPoint(label, timeSeconds) {
    const offset = sceneLabelOffset(label, timeSeconds);
    return {
      x: label.x + offset.x,
      y: label.y + offset.y,
      z: label.z + offset.z,
    };
  }

  function sceneLabelOffset(label, timeSeconds) {
    if (!label || (!label.shiftX && !label.shiftY && !label.shiftZ)) {
      return { x: 0, y: 0, z: 0 };
    }
    const angle = sceneNumber(label.driftPhase, 0) + timeSeconds * sceneNumber(label.driftSpeed, 0);
    return {
      x: Math.cos(angle) * sceneNumber(label.shiftX, 0),
      y: Math.sin(angle * 0.82 + sceneNumber(label.driftPhase, 0) * 0.35) * sceneNumber(label.shiftY, 0),
      z: Math.sin(angle) * sceneNumber(label.shiftZ, 0),
    };
  }

  function appendSceneLabelToBundle(bundle, camera, width, height, label, timeSeconds) {
    const point = sceneLabelPoint(label, timeSeconds);
    const projected = projectPoint(point, camera, width, height);
    if (!projected) {
      return;
    }

    const marginX = Math.max(24, sceneNumber(label.maxWidth, 180));
    const marginY = Math.max(24, sceneNumber(label.lineHeight, 18) * 2);
    if (projected.x < -marginX || projected.x > width + marginX || projected.y < -marginY || projected.y > height + marginY) {
      return;
    }

    bundle.labels.push({
      id: label.id,
      text: label.text,
      className: label.className,
      position: { x: projected.x, y: projected.y },
      depth: projected.depth,
      priority: sceneNumber(label.priority, 0),
      maxWidth: sceneNumber(label.maxWidth, 180),
      maxLines: Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
      overflow: normalizeTextLayoutOverflow(label.overflow),
      font: label.font,
      lineHeight: sceneNumber(label.lineHeight, 18),
      color: label.color,
      background: label.background,
      borderColor: label.borderColor,
      offsetX: sceneNumber(label.offsetX, 0),
      offsetY: sceneNumber(label.offsetY, -14),
      anchorX: sceneNumber(label.anchorX, 0.5),
      anchorY: sceneNumber(label.anchorY, 1),
      collision: normalizeSceneLabelCollision(label.collision),
      occlude: Boolean(label.occlude),
      whiteSpace: normalizeSceneLabelWhiteSpace(label.whiteSpace),
      textAlign: normalizeSceneLabelAlign(label.textAlign),
    });
  }

  function sceneWorldObjectSegments(object, timeSeconds) {
    return sceneObjectSegments(object).map(function(segment) {
      return [
        translateScenePoint(segment[0], object, timeSeconds),
        translateScenePoint(segment[1], object, timeSeconds),
      ];
    });
  }

  function sceneExpandWorldBounds(bounds, point) {
    const next = bounds || {
      minX: point.x,
      minY: point.y,
      minZ: point.z,
      maxX: point.x,
      maxY: point.y,
      maxZ: point.z,
    };
    next.minX = Math.min(next.minX, point.x);
    next.minY = Math.min(next.minY, point.y);
    next.minZ = Math.min(next.minZ, point.z);
    next.maxX = Math.max(next.maxX, point.x);
    next.maxY = Math.max(next.maxY, point.y);
    next.maxZ = Math.max(next.maxZ, point.z);
    return next;
  }

  function appendSceneLine(bundle, width, height, from, to, color, lineWidth) {
    if (!from || !to) return;
    const rgba = sceneColorRGBA(color, [0.55, 0.88, 1, 1]);
    const fromClip = sceneClipPoint(from, width, height);
    const toClip = sceneClipPoint(to, width, height);
    bundle.lines.push({
      from: from,
      to: to,
      color: color,
      lineWidth: lineWidth,
    });
    bundle.positions.push(fromClip.x, fromClip.y, toClip.x, toClip.y);
    bundle.colors.push(rgba[0], rgba[1], rgba[2], rgba[3], rgba[0], rgba[1], rgba[2], rgba[3]);
  }

  function createSceneCanvasRenderer(ctx2d, canvas) {
    return {
      kind: "canvas",
      render(bundle, viewport) {
        const devicePixelRatio = Math.max(1, sceneViewportValue(viewport, "devicePixelRatio", 1));
        const lines = Array.isArray(bundle && bundle.lines) ? bundle.lines : [];
        ctx2d.clearRect(0, 0, canvas.width, canvas.height);
        ctx2d.fillStyle = bundle && bundle.background ? bundle.background : "#08151f";
        ctx2d.fillRect(0, 0, canvas.width, canvas.height);
        if (typeof ctx2d.save === "function") {
          ctx2d.save();
        }
        if (devicePixelRatio !== 1 && typeof ctx2d.scale === "function") {
          ctx2d.scale(devicePixelRatio, devicePixelRatio);
        }
        for (const line of lines) {
          ctx2d.strokeStyle = line.color;
          ctx2d.lineWidth = line.lineWidth;
          strokeLine(ctx2d, line.from, line.to);
        }
        if (typeof ctx2d.restore === "function") {
          ctx2d.restore();
        }
      },
      dispose() {},
    };
  }

  function createSceneWebGLRenderer(canvas, options) {
    if (!canvas || typeof canvas.getContext !== "function") {
      return null;
    }
    const contextOptions = {
      alpha: false,
      antialias: !(options && options.antialias === false),
      powerPreference: options && options.powerPreference ? options.powerPreference : "high-performance",
      preserveDrawingBuffer: false,
    };
    const gl = canvas.getContext("webgl", contextOptions) || canvas.getContext("experimental-webgl", contextOptions);
    if (!gl) {
      return null;
    }

    const program = createSceneWebGLProgram(gl);
    if (!program) {
      return null;
    }

    const resources = createSceneWebGLResources(gl, program);
    return {
      kind: "webgl",
      render(bundle) {
        const geometry = sceneWebGLBundleGeometry(bundle);
        prepareSceneWebGLFrame(gl, canvas, bundle, geometry.usePerspective, resources);
        if (!bundle || geometry.vertexCount === 0 || !geometry.positions || !geometry.colors) {
          return;
        }
        gl.useProgram(program);
        applySceneWebGLUniforms(gl, bundle, canvas, geometry.usePerspective, resources);
        if (geometry.usePerspective && renderSceneWebGLWorldBundle(gl, bundle, resources)) {
          applySceneWebGLBlend(gl, "opaque", resources.stateCache);
          applySceneWebGLDepth(gl, "opaque", resources.stateCache);
          return;
        }
        renderSceneWebGLFallbackBundle(gl, geometry, resources);
      },
      dispose() {
        disposeSceneWebGLRenderer(gl, program, resources);
      },
    };
  }

  function createSceneWebGLResources(gl, program) {
    return {
      fallbackBuffers: createSceneWebGLBufferSet(gl),
      passBuffers: {
        staticOpaque: createSceneWebGLBufferSet(gl),
        alpha: createSceneWebGLBufferSet(gl),
        additive: createSceneWebGLBufferSet(gl),
        dynamicOpaque: createSceneWebGLBufferSet(gl),
      },
      drawScratch: createSceneWorldDrawScratch(),
      positionLocation: gl.getAttribLocation(program, "a_position"),
      colorLocation: gl.getAttribLocation(program, "a_color"),
      materialLocation: gl.getAttribLocation(program, "a_material"),
      cameraLocation: gl.getUniformLocation(program, "u_camera"),
      aspectLocation: gl.getUniformLocation(program, "u_aspect"),
      perspectiveLocation: gl.getUniformLocation(program, "u_use_perspective"),
      floatType: typeof gl.FLOAT === "number" ? gl.FLOAT : 0x1406,
      arrayBuffer: typeof gl.ARRAY_BUFFER === "number" ? gl.ARRAY_BUFFER : 0x8892,
      staticDraw: typeof gl.STATIC_DRAW === "number" ? gl.STATIC_DRAW : 0x88E4,
      dynamicDraw: typeof gl.DYNAMIC_DRAW === "number" ? gl.DYNAMIC_DRAW : 0x88E8,
      colorBufferBit: typeof gl.COLOR_BUFFER_BIT === "number" ? gl.COLOR_BUFFER_BIT : 0x4000,
      depthBufferBit: typeof gl.DEPTH_BUFFER_BIT === "number" ? gl.DEPTH_BUFFER_BIT : 0x0100,
      linesMode: typeof gl.LINES === "number" ? gl.LINES : 0x0001,
      passCache: {
        staticOpaque: {
          key: "",
          vertexCount: 0,
        },
      },
      stateCache: {
        blendMode: "",
        depthMode: "",
      },
    };
  }

  function sceneWebGLBundleGeometry(bundle) {
    const usePerspective = Boolean(bundle && bundle.worldVertexCount > 0 && bundle.worldPositions && bundle.worldColors);
    return {
      usePerspective,
      positions: usePerspective ? bundle.worldPositions : bundle && bundle.positions,
      colors: usePerspective ? bundle.worldColors : bundle && bundle.colors,
      vertexCount: usePerspective ? bundle && bundle.worldVertexCount : bundle && bundle.vertexCount,
    };
  }

  function prepareSceneWebGLFrame(gl, canvas, bundle, usePerspective, resources) {
    const background = sceneColorRGBA(bundle && bundle.background, [0.03, 0.08, 0.12, 1]);
    gl.viewport(0, 0, canvas.width, canvas.height);
    gl.clearColor(background[0], background[1], background[2], background[3]);
    if (usePerspective && typeof gl.clearDepth === "function") {
      gl.clearDepth(1);
    }
    gl.clear(usePerspective ? resources.colorBufferBit | resources.depthBufferBit : resources.colorBufferBit);
  }

  function applySceneWebGLUniforms(gl, bundle, canvas, usePerspective, resources) {
    const aspect = Math.max(0.0001, canvas.width / Math.max(1, canvas.height));
    if (typeof gl.uniform4f === "function" && resources.cameraLocation) {
      const camera = bundle.camera || {};
      gl.uniform4f(
        resources.cameraLocation,
        sceneNumber(camera.x, 0),
        sceneNumber(camera.y, 0),
        sceneNumber(camera.z, 6),
        sceneNumber(camera.fov, 75),
      );
    }
    if (typeof gl.uniform1f === "function" && resources.aspectLocation) {
      gl.uniform1f(resources.aspectLocation, aspect);
    }
    if (typeof gl.uniform1f === "function" && resources.perspectiveLocation) {
      gl.uniform1f(resources.perspectiveLocation, usePerspective ? 1 : 0);
    }
  }

  function renderSceneWebGLWorldBundle(gl, bundle, resources) {
    const bundledPasses = createSceneWorldWebGLPassesFromBundle(bundle, resources.passBuffers, {
      staticDraw: resources.staticDraw,
      dynamicDraw: resources.dynamicDraw,
    });
    if (bundledPasses.length > 0) {
      drawSceneWebGLPasses(gl, resources.arrayBuffer, resources.floatType, resources.linesMode, resources.positionLocation, resources.colorLocation, resources.materialLocation, bundledPasses, resources.passCache, resources.stateCache);
      return true;
    }
    const drawPlan = buildSceneWorldDrawPlan(bundle, resources.drawScratch);
    if (!drawPlan) {
      return false;
    }
    const worldPasses = createSceneWorldWebGLPasses(drawPlan, resources.passBuffers, {
      staticDraw: resources.staticDraw,
      dynamicDraw: resources.dynamicDraw,
    });
    drawSceneWebGLPasses(gl, resources.arrayBuffer, resources.floatType, resources.linesMode, resources.positionLocation, resources.colorLocation, resources.materialLocation, worldPasses, resources.passCache, resources.stateCache);
    return true;
  }

  function renderSceneWebGLFallbackBundle(gl, geometry, resources) {
    applySceneWebGLDepth(gl, "disabled", resources.stateCache);
    applySceneWebGLBlend(gl, "opaque", resources.stateCache);
    uploadSceneWebGLBuffers(
      gl,
      resources.arrayBuffer,
      resources.dynamicDraw,
      resources.fallbackBuffers.position,
      resources.fallbackBuffers.color,
      resources.fallbackBuffers.material,
      geometry.positions,
      geometry.colors,
      sceneFallbackMaterialData(geometry.vertexCount),
    );
    drawSceneWebGLLines(
      gl,
      resources.arrayBuffer,
      resources.floatType,
      resources.linesMode,
      resources.positionLocation,
      resources.colorLocation,
      resources.materialLocation,
      resources.fallbackBuffers.position,
      resources.fallbackBuffers.color,
      resources.fallbackBuffers.material,
      geometry.vertexCount,
      geometry.usePerspective ? 3 : 2,
    );
  }

  function disposeSceneWebGLRenderer(gl, program, resources) {
    if (typeof gl.deleteBuffer === "function") {
      deleteSceneWebGLBufferSet(gl, resources.fallbackBuffers);
      deleteSceneWebGLBufferSet(gl, resources.passBuffers.staticOpaque);
      deleteSceneWebGLBufferSet(gl, resources.passBuffers.alpha);
      deleteSceneWebGLBufferSet(gl, resources.passBuffers.additive);
      deleteSceneWebGLBufferSet(gl, resources.passBuffers.dynamicOpaque);
    }
    if (typeof gl.deleteProgram === "function") {
      gl.deleteProgram(program);
    }
  }

  function createSceneWebGLBufferSet(gl) {
    return {
      position: gl.createBuffer(),
      color: gl.createBuffer(),
      material: gl.createBuffer(),
    };
  }

  function deleteSceneWebGLBufferSet(gl, buffers) {
    if (!buffers) {
      return;
    }
    gl.deleteBuffer(buffers.position);
    gl.deleteBuffer(buffers.color);
    gl.deleteBuffer(buffers.material);
  }

  function createSceneWorldWebGLPasses(drawPlan, buffers, usages) {
    const passes = [];
    passes.push({
      name: "staticOpaque",
      blend: "opaque",
      depth: "opaque",
      usage: usages.staticDraw,
      cacheSlot: "staticOpaque",
      cacheKey: drawPlan.staticOpaqueKey,
      buffers: buffers.staticOpaque,
      positions: drawPlan.staticOpaquePositions,
      colors: drawPlan.staticOpaqueColors,
      materials: drawPlan.staticOpaqueMaterials,
      vertexCount: drawPlan.staticOpaqueVertexCount,
    });
    passes.push({
      name: "dynamicOpaque",
      blend: "opaque",
      depth: "opaque",
      usage: usages.dynamicDraw,
      buffers: buffers.dynamicOpaque,
      positions: drawPlan.dynamicOpaquePositions,
      colors: drawPlan.dynamicOpaqueColors,
      materials: drawPlan.dynamicOpaqueMaterials,
      vertexCount: drawPlan.dynamicOpaqueVertexCount,
    });
    if (drawPlan.hasAlphaPass) {
      passes.push({
        name: "alpha",
        blend: "alpha",
        depth: "translucent",
        usage: usages.dynamicDraw,
        buffers: buffers.alpha,
        positions: drawPlan.alphaPositions,
        colors: drawPlan.alphaColors,
        materials: drawPlan.alphaMaterials,
        vertexCount: drawPlan.alphaVertexCount,
      });
    }
    if (drawPlan.hasAdditivePass) {
      passes.push({
        name: "additive",
        blend: "additive",
        depth: "translucent",
        usage: usages.dynamicDraw,
        buffers: buffers.additive,
        positions: drawPlan.additivePositions,
        colors: drawPlan.additiveColors,
        materials: drawPlan.additiveMaterials,
        vertexCount: drawPlan.additiveVertexCount,
      });
    }
    return passes;
  }

  function createSceneWorldWebGLPassesFromBundle(bundle, buffers, usages) {
    const sourcePasses = Array.isArray(bundle && bundle.passes) ? bundle.passes : [];
    const passes = [];
    for (const source of sourcePasses) {
      const pass = sceneWorldWebGLPassFromSource(source, buffers, usages);
      if (pass) {
        passes.push(pass);
      }
    }
    return passes;
  }

  function sceneWorldWebGLPassFromSource(source, buffers, usages) {
    const name = sceneWorldWebGLPassName(source);
    if (!name) {
      return null;
    }
    const targetBuffers = buffers[name];
    if (!targetBuffers) {
      return null;
    }
    const isStatic = Boolean(source && source.static);
    const positions = sceneTypedFloatArray(source && source.positions);
    const colors = sceneTypedFloatArray(source && source.colors);
    const materials = sceneTypedFloatArray(source && source.materials);
    return {
      name,
      blend: sceneWorldWebGLPassMode(source && source.blend, "opaque"),
      depth: sceneWorldWebGLPassMode(source && source.depth, "opaque"),
      usage: isStatic ? usages.staticDraw : usages.dynamicDraw,
      cacheSlot: sceneWorldWebGLPassCacheSlot(name, isStatic),
      cacheKey: String(source && source.cacheKey || ""),
      buffers: targetBuffers,
      positions,
      colors,
      materials,
      vertexCount: sceneWorldWebGLPassVertexCount(source, positions, colors, materials),
    };
  }

  function sceneWorldWebGLPassName(source) {
    return String(source && source.name || "");
  }

  function sceneWorldWebGLPassMode(value, fallback) {
    const mode = String(value || fallback);
    return mode || fallback;
  }

  function sceneWorldWebGLPassCacheSlot(name, isStatic) {
    if (!isStatic) {
      return "";
    }
    return name;
  }

  function sceneWorldWebGLPassVertexCount(source, positions, colors, materials) {
    const requested = Math.max(0, Math.floor(sceneNumber(source && source.vertexCount, NaN)));
    const positionCount = Math.floor((positions && positions.length || 0) / 3);
    const colorCount = Math.floor((colors && colors.length || 0) / 4);
    const materialCount = Math.floor((materials && materials.length || 0) / 3);
    const maxCount = Math.max(0, Math.min(positionCount, colorCount, materialCount));
    if (Number.isFinite(requested)) {
      return Math.min(requested, maxCount);
    }
    return maxCount;
  }

  function drawSceneWebGLPasses(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, passes, cache, stateCache) {
    for (const pass of passes) {
      const vertexCount = uploadSceneWebGLPass(gl, arrayBuffer, pass, cache);
      if (!vertexCount) {
        continue;
      }
      applySceneWebGLDepth(gl, pass.depth, stateCache);
      applySceneWebGLBlend(gl, pass.blend, stateCache);
      drawSceneWebGLLines(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, pass.buffers.position, pass.buffers.color, pass.buffers.material, vertexCount, 3);
    }
  }

  function uploadSceneWebGLPass(gl, arrayBuffer, pass, cache) {
    if (!pass || !pass.buffers) {
      return 0;
    }
    if (pass.cacheSlot) {
      const record = cache[pass.cacheSlot] || (cache[pass.cacheSlot] = { key: "", vertexCount: 0 });
      if (record.key !== pass.cacheKey) {
        uploadSceneWebGLBuffers(gl, arrayBuffer, pass.usage, pass.buffers.position, pass.buffers.color, pass.buffers.material, pass.positions, pass.colors, pass.materials);
        record.key = pass.cacheKey;
        record.vertexCount = pass.vertexCount;
      }
      return record.vertexCount;
    }
    if (!pass.vertexCount) {
      return 0;
    }
    uploadSceneWebGLBuffers(gl, arrayBuffer, pass.usage, pass.buffers.position, pass.buffers.color, pass.buffers.material, pass.positions, pass.colors, pass.materials);
    return pass.vertexCount;
  }

  function uploadSceneWebGLBuffers(gl, arrayBuffer, usage, positionBuffer, colorBuffer, materialBuffer, positions, colors, materials) {
    gl.bindBuffer(arrayBuffer, positionBuffer);
    gl.bufferData(arrayBuffer, positions, usage);
    gl.bindBuffer(arrayBuffer, colorBuffer);
    gl.bufferData(arrayBuffer, colors, usage);
    gl.bindBuffer(arrayBuffer, materialBuffer);
    gl.bufferData(arrayBuffer, materials, usage);
  }

  function drawSceneWebGLLines(gl, arrayBuffer, floatType, linesMode, positionLocation, colorLocation, materialLocation, positionBuffer, colorBuffer, materialBuffer, vertexCount, positionSize) {
    if (!vertexCount) {
      return;
    }
    gl.bindBuffer(arrayBuffer, positionBuffer);
    gl.enableVertexAttribArray(positionLocation);
    gl.vertexAttribPointer(positionLocation, positionSize, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, colorBuffer);
    gl.enableVertexAttribArray(colorLocation);
    gl.vertexAttribPointer(colorLocation, 4, floatType, false, 0, 0);

    gl.bindBuffer(arrayBuffer, materialBuffer);
    gl.enableVertexAttribArray(materialLocation);
    gl.vertexAttribPointer(materialLocation, 3, floatType, false, 0, 0);

    gl.drawArrays(linesMode, 0, vertexCount);
  }

  function sceneTypedFloatArray(values) {
    if (values instanceof Float32Array) {
      return values;
    }
    const list = Array.isArray(values) ? values : [];
    const typed = new Float32Array(list.length);
    for (let i = 0; i < list.length; i += 1) {
      typed[i] = sceneNumber(list[i], 0);
    }
    return typed;
  }

  function applySceneWebGLBlend(gl, mode, stateCache) {
    if (sceneWebGLStateUnchanged(stateCache, "blendMode", mode)) {
      return;
    }
    const blendConst = typeof gl.BLEND === "number" ? gl.BLEND : 0x0BE2;
    const one = typeof gl.ONE === "number" ? gl.ONE : 1;
    const srcAlpha = typeof gl.SRC_ALPHA === "number" ? gl.SRC_ALPHA : 0x0302;
    const oneMinusSrcAlpha = typeof gl.ONE_MINUS_SRC_ALPHA === "number" ? gl.ONE_MINUS_SRC_ALPHA : 0x0303;
    const config = sceneWebGLBlendConfig(mode, srcAlpha, oneMinusSrcAlpha, one);
    rememberSceneWebGLState(stateCache, "blendMode", mode);
    setSceneWebGLCapability(gl, blendConst, config.enabled);
    if (config.enabled && typeof gl.blendFunc === "function") {
      gl.blendFunc(config.src, config.dst);
    }
  }

  function applySceneWebGLDepth(gl, mode, stateCache) {
    if (sceneWebGLStateUnchanged(stateCache, "depthMode", mode)) {
      return;
    }
    const depthTest = typeof gl.DEPTH_TEST === "number" ? gl.DEPTH_TEST : 0x0B71;
    const lequal = typeof gl.LEQUAL === "number" ? gl.LEQUAL : 0x0203;
    const config = sceneWebGLDepthConfig(mode);
    rememberSceneWebGLState(stateCache, "depthMode", mode);
    setSceneWebGLCapability(gl, depthTest, config.enabled);
    if (!config.enabled) {
      return;
    }
    if (typeof gl.depthFunc === "function") {
      gl.depthFunc(lequal);
    }
    if (typeof gl.depthMask === "function") {
      gl.depthMask(config.mask);
    }
  }

  function sceneWebGLStateUnchanged(stateCache, key, mode) {
    return Boolean(stateCache && stateCache[key] === mode);
  }

  function rememberSceneWebGLState(stateCache, key, mode) {
    if (!stateCache) {
      return;
    }
    stateCache[key] = mode;
  }

  function setSceneWebGLCapability(gl, capability, enabled) {
    if (enabled) {
      if (typeof gl.enable === "function") {
        gl.enable(capability);
      }
      return;
    }
    if (typeof gl.disable === "function") {
      gl.disable(capability);
    }
  }

  function sceneWebGLBlendConfig(mode, srcAlpha, oneMinusSrcAlpha, one) {
    switch (mode) {
    case "alpha":
      return { enabled: true, src: srcAlpha, dst: oneMinusSrcAlpha };
    case "additive":
      return { enabled: true, src: srcAlpha, dst: one };
    default:
      return { enabled: false };
    }
  }

  function sceneWebGLDepthConfig(mode) {
    switch (mode) {
    case "opaque":
      return { enabled: true, mask: true };
    case "translucent":
      return { enabled: true, mask: false };
    default:
      return { enabled: false, mask: false };
    }
  }

  function buildSceneWorldDrawPlan(bundle, scratch) {
    const objects = Array.isArray(bundle.objects) ? bundle.objects : [];
    const materials = Array.isArray(bundle.materials) ? bundle.materials : [];
    if (!objects.length || !materials.length) {
      return null;
    }
    const drawScratch = resetSceneWorldDrawScratch(scratch || createSceneWorldDrawScratch());
    for (let index = 0; index < objects.length; index += 1) {
      collectSceneWorldDrawObject(drawScratch, bundle, materials, objects[index], index);
    }
    return finalizeSceneWorldDrawPlan(bundle, drawScratch);
  }

  function collectSceneWorldDrawObject(drawScratch, bundle, materials, object, order) {
    if (!sceneWorldObjectRenderable(object)) {
      return;
    }
    const material = materials[object.materialIndex] || null;
    const renderPass = sceneWorldObjectRenderPass(object, material);
    if (renderPass === "additive" || renderPass === "alpha") {
      collectSceneWorldTranslucentObject(drawScratch, bundle, object, material, renderPass, order);
      return;
    }
    collectSceneWorldOpaqueObject(drawScratch, bundle, object, material);
  }

  function sceneWorldObjectRenderable(object) {
    return Boolean(
      object &&
      Number.isFinite(object.vertexOffset) &&
      Number.isFinite(object.vertexCount) &&
      object.vertexCount > 0 &&
      !sceneWorldObjectCulled(object)
    );
  }

  function collectSceneWorldOpaqueObject(drawScratch, bundle, object, material) {
    const target = object.static ? {
      positions: drawScratch.staticOpaquePositions,
      colors: drawScratch.staticOpaqueColors,
      materials: drawScratch.staticOpaqueMaterials,
    } : {
      positions: drawScratch.dynamicOpaquePositions,
      colors: drawScratch.dynamicOpaqueColors,
      materials: drawScratch.dynamicOpaqueMaterials,
    };
    if (object.static) {
      drawScratch.staticOpaqueObjects.push(object);
      drawScratch.staticOpaqueMaterialProfiles.push(material);
    }
    appendSceneWorldObjectSlice(target.positions, target.colors, target.materials, bundle.worldPositions, bundle.worldColors, object, material);
  }

  function collectSceneWorldTranslucentObject(drawScratch, bundle, object, material, renderPass, order) {
    const targetEntries = renderPass === "additive" ? drawScratch.additiveEntries : drawScratch.alphaEntries;
    targetEntries.push(createSceneWorldPassEntry(object, material, bundle.worldPositions, bundle.camera, order));
  }

  function finalizeSceneWorldDrawPlan(bundle, drawScratch) {
    const typedStaticOpaque = createSceneWorldOpaqueBuffers(drawScratch, "typedStaticOpaque", drawScratch.staticOpaquePositions, drawScratch.staticOpaqueColors, drawScratch.staticOpaqueMaterials);
    const typedDynamicOpaque = createSceneWorldOpaqueBuffers(drawScratch, "typedDynamicOpaque", drawScratch.dynamicOpaquePositions, drawScratch.dynamicOpaqueColors, drawScratch.dynamicOpaqueMaterials);
    const typedAlphaPlan = createSceneWorldPassPlan(bundle.worldPositions, bundle.worldColors, drawScratch.alphaEntries, drawScratch.alphaPlan);
    const typedAdditivePlan = createSceneWorldPassPlan(bundle.worldPositions, bundle.worldColors, drawScratch.additiveEntries, drawScratch.additivePlan);
    const plan = drawScratch.plan;
    plan.staticOpaqueKey = sceneStaticDrawKey(drawScratch.staticOpaqueObjects, drawScratch.staticOpaqueMaterialProfiles, bundle.camera);
    plan.staticOpaquePositions = typedStaticOpaque.positions;
    plan.staticOpaqueColors = typedStaticOpaque.colors;
    plan.staticOpaqueMaterials = typedStaticOpaque.materials;
    plan.staticOpaqueVertexCount = typedStaticOpaque.vertexCount;
    plan.dynamicOpaquePositions = typedDynamicOpaque.positions;
    plan.dynamicOpaqueColors = typedDynamicOpaque.colors;
    plan.dynamicOpaqueMaterials = typedDynamicOpaque.materials;
    plan.dynamicOpaqueVertexCount = typedDynamicOpaque.vertexCount;
    plan.alphaPositions = typedAlphaPlan.positions;
    plan.alphaColors = typedAlphaPlan.colors;
    plan.alphaMaterials = typedAlphaPlan.materials;
    plan.alphaVertexCount = typedAlphaPlan.vertexCount;
    plan.additivePositions = typedAdditivePlan.positions;
    plan.additiveColors = typedAdditivePlan.colors;
    plan.additiveMaterials = typedAdditivePlan.materials;
    plan.additiveVertexCount = typedAdditivePlan.vertexCount;
    plan.hasAlphaPass = typedAlphaPlan.vertexCount > 0;
    plan.hasAdditivePass = typedAdditivePlan.vertexCount > 0;
    return plan;
  }

  function createSceneWorldOpaqueBuffers(drawScratch, keyPrefix, positions, colors, materials) {
    const typedPositions = sceneWriteFloatArray(drawScratch, keyPrefix + "Positions", positions);
    const typedColors = sceneWriteFloatArray(drawScratch, keyPrefix + "Colors", colors);
    const typedMaterials = sceneWriteFloatArray(drawScratch, keyPrefix + "Materials", materials);
    return {
      positions: typedPositions,
      colors: typedColors,
      materials: typedMaterials,
      vertexCount: typedPositions.length / 3,
    };
  }

  function createSceneWorldPassEntry(object, material, sourcePositions, camera, order) {
    return {
      object,
      material,
      order,
      depth: sceneWorldObjectDepth(sourcePositions, object, camera),
    };
  }

  function createSceneWorldPassPlan(sourcePositions, sourceColors, entries, scratch) {
    const passScratch = resetSceneWorldPassScratch(scratch || createSceneWorldPassScratch());
    if (!entries.length) {
      passScratch.typedPositions = sceneWriteFloatArray(passScratch, "typedPositions", passScratch.positions);
      passScratch.typedColors = sceneWriteFloatArray(passScratch, "typedColors", passScratch.colors);
      passScratch.typedMaterials = sceneWriteFloatArray(passScratch, "typedMaterials", passScratch.materials);
      passScratch.vertexCount = 0;
      return passScratch;
    }
    const positions = passScratch.positions;
    const colors = passScratch.colors;
    const materials = passScratch.materials;
    entries.sort(compareSceneWorldPassEntries);
    for (const entry of entries) {
      appendSceneWorldObjectSlice(positions, colors, materials, sourcePositions, sourceColors, entry.object, entry.material);
    }
    passScratch.typedPositions = sceneWriteFloatArray(passScratch, "typedPositions", positions);
    passScratch.typedColors = sceneWriteFloatArray(passScratch, "typedColors", colors);
    passScratch.typedMaterials = sceneWriteFloatArray(passScratch, "typedMaterials", materials);
    passScratch.vertexCount = passScratch.typedPositions.length / 3;
    return passScratch;
  }

  function compareSceneWorldPassEntries(a, b) {
    if (a.depth !== b.depth) {
      return b.depth - a.depth;
    }
    return a.order - b.order;
  }

  function sceneWorldObjectDepth(sourcePositions, object, camera) {
    if (object && Number.isFinite(object.depthCenter)) {
      return sceneNumber(object.depthCenter, sceneWorldPointDepth(0, camera));
    }
    const vertexOffset = Math.max(0, Math.floor(sceneNumber(object && object.vertexOffset, 0)));
    const vertexCount = Math.max(0, Math.floor(sceneNumber(object && object.vertexCount, 0)));
    if (!vertexCount) {
      return sceneWorldPointDepth(0, camera);
    }
    const start = vertexOffset * 3 + 2;
    const end = start + vertexCount * 3;
    let depth = 0;
    let count = 0;
    for (let i = start; i < end; i += 3) {
      depth += sceneNumber(sourcePositions[i], 0);
      count += 1;
    }
    return depth / Math.max(1, count) + sceneWorldPointDepth(0, camera);
  }

  function sceneWorldObjectCulled(object) {
    return Boolean(object && object.viewCulled);
  }

  function appendSceneWorldObjectSlice(targetPositions, targetColors, targetMaterials, sourcePositions, sourceColors, object, material) {
    const vertexOffset = Math.max(0, Math.floor(sceneNumber(object.vertexOffset, 0)));
    const vertexCount = Math.max(0, Math.floor(sceneNumber(object.vertexCount, 0)));
    const opacity = sceneMaterialOpacity(material);
    const materialData = sceneMaterialShaderData(material);
    const startPosition = vertexOffset * 3;
    const endPosition = startPosition + vertexCount * 3;
    const startColor = vertexOffset * 4;
    const endColor = startColor + vertexCount * 4;
    for (let i = startPosition; i < endPosition; i += 1) {
      targetPositions.push(sceneNumber(sourcePositions[i], 0));
    }
    for (let i = startColor; i < endColor; i += 4) {
      targetColors.push(
        sceneNumber(sourceColors[i], 0),
        sceneNumber(sourceColors[i + 1], 0),
        sceneNumber(sourceColors[i + 2], 0),
        sceneNumber(sourceColors[i + 3], 1) * opacity,
      );
      targetMaterials.push(materialData[0], materialData[1], materialData[2]);
    }
  }

  function createSceneWorldDrawScratch() {
    return {
      staticOpaquePositions: [],
      staticOpaqueColors: [],
      staticOpaqueMaterials: [],
      dynamicOpaquePositions: [],
      dynamicOpaqueColors: [],
      dynamicOpaqueMaterials: [],
      staticOpaqueObjects: [],
      staticOpaqueMaterialProfiles: [],
      alphaEntries: [],
      additiveEntries: [],
      typedStaticOpaquePositions: new Float32Array(0),
      typedStaticOpaqueColors: new Float32Array(0),
      typedStaticOpaqueMaterials: new Float32Array(0),
      typedDynamicOpaquePositions: new Float32Array(0),
      typedDynamicOpaqueColors: new Float32Array(0),
      typedDynamicOpaqueMaterials: new Float32Array(0),
      alphaPlan: createSceneWorldPassScratch(),
      additivePlan: createSceneWorldPassScratch(),
      plan: {},
    };
  }

  function resetSceneWorldDrawScratch(scratch) {
    scratch.staticOpaquePositions.length = 0;
    scratch.staticOpaqueColors.length = 0;
    scratch.staticOpaqueMaterials.length = 0;
    scratch.dynamicOpaquePositions.length = 0;
    scratch.dynamicOpaqueColors.length = 0;
    scratch.dynamicOpaqueMaterials.length = 0;
    scratch.staticOpaqueObjects.length = 0;
    scratch.staticOpaqueMaterialProfiles.length = 0;
    scratch.alphaEntries.length = 0;
    scratch.additiveEntries.length = 0;
    resetSceneWorldPassScratch(scratch.alphaPlan);
    resetSceneWorldPassScratch(scratch.additivePlan);
    return scratch;
  }

  function createSceneWorldPassScratch() {
    return {
      positions: [],
      colors: [],
      materials: [],
      typedPositions: new Float32Array(0),
      typedColors: new Float32Array(0),
      typedMaterials: new Float32Array(0),
      vertexCount: 0,
    };
  }

  function resetSceneWorldPassScratch(scratch) {
    scratch.positions.length = 0;
    scratch.colors.length = 0;
    scratch.materials.length = 0;
    scratch.vertexCount = 0;
    return scratch;
  }

  function sceneWriteFloatArray(target, key, values) {
    let buffer = target[key];
    if (!buffer || buffer.length !== values.length) {
      buffer = new Float32Array(values.length);
      target[key] = buffer;
    }
    for (let i = 0; i < values.length; i += 1) {
      buffer[i] = sceneNumber(values[i], 0);
    }
    return buffer;
  }

  function sceneWorldPointDepth(z, camera) {
    return sceneNumber(z, 0) + sceneNumber(camera && camera.z, 6);
  }

  function sceneMaterialOpacity(material) {
    if (!material || typeof material !== "object") {
      return 1;
    }
    return clamp01(sceneNumber(material.opacity, 1));
  }

  function sceneMaterialEmissive(material) {
    if (!material || typeof material !== "object") {
      return 0;
    }
    return clamp01(sceneNumber(material.emissive, 0));
  }

  function sceneMaterialUsesAlpha(material) {
    return sceneMaterialRenderPass(material) !== "opaque";
  }

  function sceneMaterialRenderPass(material) {
    if (!material || typeof material !== "object") {
      return "opaque";
    }
    const renderPass = String(material.renderPass || "").toLowerCase();
    if (renderPass === "opaque" || renderPass === "alpha" || renderPass === "additive") {
      return renderPass;
    }
    const blendMode = String(material.blendMode || "").toLowerCase();
    if (blendMode === "additive") {
      return "additive";
    }
    if (blendMode === "alpha" || sceneMaterialOpacity(material) < 0.999) {
      return "alpha";
    }
    return "opaque";
  }

  function sceneMaterialShaderData(material) {
    if (material && Array.isArray(material.shaderData) && material.shaderData.length >= 3) {
      return [
        sceneNumber(material.shaderData[0], 0),
        sceneNumber(material.shaderData[1], 0),
        sceneNumber(material.shaderData[2], 1),
      ];
    }
    if (!material || typeof material !== "object") {
      return [0, 0, 1];
    }
    const kind = String(material.kind || "").toLowerCase();
    switch (kind) {
    case "ghost":
      return [1, sceneMaterialEmissive(material), 0.3];
    case "glass":
      return [2, sceneMaterialEmissive(material), 0.7];
    case "glow":
      return [3, sceneMaterialEmissive(material), 1];
    case "matte":
      return [4, sceneMaterialEmissive(material), 0.2];
    default:
      return [0, sceneMaterialEmissive(material), 1];
    }
  }

  function sceneWorldObjectRenderPass(object, material) {
    const renderPass = String(object && object.renderPass || "").toLowerCase();
    if (renderPass === "opaque" || renderPass === "alpha" || renderPass === "additive") {
      return renderPass;
    }
    return sceneMaterialRenderPass(material);
  }

  function sceneFallbackMaterialData(vertexCount) {
    const values = new Float32Array(vertexCount * 3);
    for (let i = 0; i < vertexCount; i += 1) {
      values[i * 3 + 2] = 1;
    }
    return values;
  }

  function clamp01(value) {
    return Math.max(0, Math.min(1, value));
  }

  function sceneStaticDrawKey(objects, materials, camera) {
    let hash = 2166136261 >>> 0;
    hash = sceneHashCamera(hash, camera);
    for (const object of objects) {
      hash = sceneHashStaticObject(hash, object);
    }
    for (const material of materials) {
      hash = sceneHashMaterialProfile(hash, material);
    }
    return String(hash) + ":" + objects.length + ":" + materials.length;
  }

  function sceneHashCamera(hash, camera) {
    hash = sceneHashNumber(hash, sceneNumber(camera && camera.x, 0));
    hash = sceneHashNumber(hash, sceneNumber(camera && camera.y, 0));
    hash = sceneHashNumber(hash, sceneNumber(camera && camera.z, 6));
    hash = sceneHashNumber(hash, sceneNumber(camera && camera.fov, 75));
    hash = sceneHashNumber(hash, sceneNumber(camera && camera.near, 0.05));
    return sceneHashNumber(hash, sceneNumber(camera && camera.far, 128));
  }

  const sceneStaticObjectStringFields = ["id", "kind"];
  const sceneStaticObjectNumberFields = [
    ["materialIndex", 0],
    ["vertexOffset", 0],
    ["vertexCount", 0],
    ["depthNear", 0],
    ["depthFar", 0],
    ["depthCenter", 0],
  ];
  const sceneStaticObjectFlagFields = ["static", "viewCulled"];
  const sceneBoundsNumberFields = [
    ["minX", 0],
    ["minY", 0],
    ["minZ", 0],
    ["maxX", 0],
    ["maxY", 0],
    ["maxZ", 0],
  ];
  const sceneMaterialStringFields = ["kind", "color", "blendMode", "renderPass"];
  const sceneMaterialNumberFields = [
    ["opacity", 1],
    ["emissive", 0],
  ];
  const sceneMaterialFlagFields = ["wireframe"];

  function sceneHashStaticObject(hash, object) {
    hash = sceneHashFieldStrings(hash, object, sceneStaticObjectStringFields);
    hash = sceneHashFieldNumbers(hash, object, sceneStaticObjectNumberFields);
    hash = sceneHashFieldFlags(hash, object, sceneStaticObjectFlagFields);
    return sceneHashBounds(hash, object && object.bounds);
  }

  function sceneHashBounds(hash, bounds) {
    return sceneHashFieldNumbers(hash, bounds, sceneBoundsNumberFields);
  }

  function sceneHashMaterialProfile(hash, material) {
    const key = material && material.key;
    if (key) {
      return sceneHashString(hash, key);
    }
    hash = sceneHashFieldStrings(hash, material, sceneMaterialStringFields);
    hash = sceneHashFieldNumbers(hash, material, sceneMaterialNumberFields);
    return sceneHashFieldFlags(hash, material, sceneMaterialFlagFields);
  }

  function sceneHashFieldStrings(hash, source, fields) {
    for (const field of fields) {
      hash = sceneHashString(hash, source && source[field] || "");
    }
    return hash;
  }

  function sceneHashFieldNumbers(hash, source, fields) {
    for (const field of fields) {
      hash = sceneHashNumber(hash, sceneNumber(source && source[field[0]], field[1]));
    }
    return hash;
  }

  function sceneHashFieldFlags(hash, source, fields) {
    for (const field of fields) {
      hash = sceneHashNumber(hash, source && source[field] ? 1 : 0);
    }
    return hash;
  }

  function sceneHashNumber(hash, value) {
    const scaled = Math.round(sceneNumber(value, 0) * 1000);
    hash ^= scaled;
    return Math.imul(hash, 16777619) >>> 0;
  }

  function sceneHashString(hash, value) {
    const text = String(value || "");
    for (let i = 0; i < text.length; i += 1) {
      hash ^= text.charCodeAt(i);
      hash = Math.imul(hash, 16777619) >>> 0;
    }
    return hash;
  }

  function createSceneWebGLProgram(gl) {
    const vertexSource = [
      "attribute vec3 a_position;",
      "attribute vec4 a_color;",
      "attribute vec3 a_material;",
      "uniform vec4 u_camera;",
      "uniform float u_aspect;",
      "uniform float u_use_perspective;",
      "varying vec4 v_color;",
      "varying vec3 v_material;",
      "void main() {",
      "  vec4 clip = vec4(a_position.xy, 0.0, 1.0);",
      "  if (u_use_perspective > 0.5) {",
      "    float depth = a_position.z + u_camera.z;",
      "    if (depth <= 0.001) {",
      "      clip = vec4(2.0, 2.0, 0.0, 1.0);",
      "    } else {",
      "      float focal = 1.0 / tan(radians(u_camera.w) * 0.5);",
      "      vec2 projected = vec2((a_position.x - u_camera.x) * focal / depth, (a_position.y - u_camera.y) * focal / depth);",
      "      projected.x /= max(u_aspect, 0.0001);",
      "      float clipDepth = clamp(depth / 128.0, 0.0, 1.0) * 2.0 - 1.0;",
      "      clip = vec4(projected, clipDepth, 1.0);",
      "    }",
      "  }",
      "  gl_Position = clip;",
      "  v_color = a_color;",
      "  v_material = a_material;",
      "}",
    ].join("\n");
    const fragmentSource = [
      "precision mediump float;",
      "varying vec4 v_color;",
      "varying vec3 v_material;",
      "void main() {",
      "  vec4 color = v_color;",
      "  float kind = floor(v_material.x + 0.5);",
      "  float emissive = max(v_material.y, 0.0);",
      "  float tone = clamp(v_material.z, 0.0, 1.0);",
      "  if (kind > 3.5) {",
      "    color.rgb *= mix(0.78, 1.0, tone);",
      "  } else if (kind > 2.5) {",
      "    color.rgb *= 1.0 + emissive * 0.75;",
      "  } else if (kind > 1.5) {",
      "    color.rgb = mix(color.rgb, vec3(0.92, 0.98, 1.0), 0.28 + tone * 0.16);",
      "    color.a *= 0.84;",
      "  } else if (kind > 0.5) {",
      "    color.rgb = mix(color.rgb, vec3(0.84, 0.94, 1.0), 0.18 + tone * 0.12);",
      "    color.a *= 0.9;",
      "  } else {",
      "    color.rgb *= mix(0.9, 1.0, tone);",
      "  }",
      "  gl_FragColor = vec4(clamp(color.rgb, 0.0, 1.0), clamp(color.a, 0.0, 1.0));",
      "}",
    ].join("\n");

    const vertexShader = createSceneShader(gl, gl.VERTEX_SHADER, vertexSource);
    const fragmentShader = createSceneShader(gl, gl.FRAGMENT_SHADER, fragmentSource);
    if (!vertexShader || !fragmentShader) {
      return null;
    }

    const program = gl.createProgram();
    gl.attachShader(program, vertexShader);
    gl.attachShader(program, fragmentShader);
    gl.linkProgram(program);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      console.warn("[gosx] Scene3D WebGL link failed");
      return null;
    }
    return program;
  }

  function createSceneShader(gl, type, source) {
    const shader = gl.createShader(type);
    gl.shaderSource(shader, source);
    gl.compileShader(shader);
    if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
      console.warn("[gosx] Scene3D WebGL shader compile failed");
      return null;
    }
    return shader;
  }

  function createSceneRenderer(canvas, props, capability) {
    const webglPreference = sceneCapabilityWebGLPreference(props, capability);
    if (webglPreference === "prefer" || webglPreference === "force") {
      const webglRenderer = createSceneWebGLRenderer(canvas, {
        antialias: capability.tier === "full" && !capability.lowPower && !capability.reducedData,
        powerPreference: capability.lowPower || capability.tier === "constrained" ? "low-power" : "high-performance",
      });
      if (webglRenderer) {
        return {
          renderer: webglRenderer,
          fallbackReason: "",
        };
      }
    }
    const ctx2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
    if (!ctx2d) {
      return null;
    }
    return {
      renderer: createSceneCanvasRenderer(ctx2d, canvas),
      fallbackReason: sceneRendererFallbackReason(props, capability, "canvas"),
    };
  }

  function normalizeSceneCapabilityTier(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "constrained":
      case "balanced":
      case "full":
        return String(value).trim().toLowerCase();
      default:
        return "";
    }
  }

  function sceneMediaQueryMatches(query) {
    if (!query || typeof window.matchMedia !== "function") {
      return false;
    }
    try {
      return Boolean(window.matchMedia(query).matches);
    } catch (_error) {
      return false;
    }
  }

  function sceneEnvironmentState() {
    if (window.__gosx
      && window.__gosx.environment
      && typeof window.__gosx.environment.get === "function") {
      return window.__gosx.environment.get();
    }
    return null;
  }

  function sceneCapabilityProfile(props) {
    const requestedTier = normalizeSceneCapabilityTier(props && props.capabilityTier);
    const environment = sceneEnvironmentState();
    const navigatorRef = window && window.navigator ? window.navigator : {};
    const coarsePointer = environment ? Boolean(environment.coarsePointer) : (sceneMediaQueryMatches("(pointer: coarse)") || sceneMediaQueryMatches("(any-pointer: coarse)"));
    const hover = environment ? Boolean(environment.hover) : (sceneMediaQueryMatches("(hover: hover)") || sceneMediaQueryMatches("(any-hover: hover)"));
    const reducedData = environment ? Boolean(environment.reducedData) : sceneMediaQueryMatches("(prefers-reduced-data: reduce)");
    const lowPower = environment ? Boolean(environment.lowPower) : false;
    const visualViewportActive = environment ? Boolean(environment.visualViewportActive) : Boolean(window.visualViewport);
    const deviceMemory = sceneNumber(environment && environment.deviceMemory, sceneNumber(navigatorRef && navigatorRef.deviceMemory, 0));
    const hardwareConcurrency = Math.max(0, Math.floor(sceneNumber(environment && environment.hardwareConcurrency, sceneNumber(navigatorRef && navigatorRef.hardwareConcurrency, 0))));
    const constrainedHardware = lowPower || reducedData || (deviceMemory > 0 && deviceMemory <= 4) || (hardwareConcurrency > 0 && hardwareConcurrency <= 4);

    let tier = requestedTier;
    if (!tier) {
      if ((coarsePointer && constrainedHardware) || reducedData || lowPower) {
        tier = "constrained";
      } else if (coarsePointer) {
        tier = "balanced";
      } else {
        tier = "full";
      }
    }

    return {
      tier,
      coarsePointer,
      hover,
      reducedData,
      lowPower,
      visualViewportActive,
      deviceMemory,
      hardwareConcurrency,
    };
  }

  function sceneCapabilityWebGLPreference(props, capability) {
    if (!sceneBool(props && props.preferWebGL, true)) {
      return "disabled";
    }
    if (sceneBool(props && props.forceWebGL, false)) {
      return "force";
    }
    if (sceneBool(props && props.preferCanvas, false)) {
      return "avoid";
    }
    if (!capability) {
      return "prefer";
    }
    if (capability.reducedData || capability.lowPower) {
      return "avoid";
    }
    if (capability.tier === "constrained" && capability.coarsePointer) {
      return "avoid";
    }
    return "prefer";
  }

  function sceneRendererFallbackReason(props, capability, rendererKind) {
    if (rendererKind === "webgl") {
      return "";
    }
    switch (sceneCapabilityWebGLPreference(props, capability)) {
      case "disabled":
        return "webgl-disabled";
      case "avoid":
        return "environment-constrained";
      default:
        return sceneBool(props && props.preferWebGL, true) ? "webgl-unavailable" : "";
    }
  }

  function sceneCapabilityChanged(prev, next) {
    if (!prev || !next) {
      return true;
    }
    return prev.tier !== next.tier
      || prev.coarsePointer !== next.coarsePointer
      || prev.hover !== next.hover
      || prev.reducedData !== next.reducedData
      || prev.lowPower !== next.lowPower
      || prev.visualViewportActive !== next.visualViewportActive
      || prev.deviceMemory !== next.deviceMemory
      || prev.hardwareConcurrency !== next.hardwareConcurrency;
  }

  function defaultSceneMaxDevicePixelRatio(capability) {
    if (capability && (capability.reducedData || capability.lowPower)) {
      switch (capability.tier) {
        case "constrained":
          return 1.25;
        case "balanced":
          return 1.5;
        default:
          return 1.75;
      }
    }
    switch (capability && capability.tier) {
      case "constrained":
        return 1.5;
      case "balanced":
        return 1.75;
      default:
        return 2;
    }
  }

  function applySceneCapabilityState(mount, props, capability) {
    if (!mount || !capability) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-capability-tier", capability.tier);
    setAttrValue(mount, "data-gosx-scene3d-coarse-pointer", capability.coarsePointer ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-hover", capability.hover ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-reduced-data", capability.reducedData ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-low-power", capability.lowPower ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-visual-viewport", capability.visualViewportActive ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-webgl-preference", sceneCapabilityWebGLPreference(props, capability));
    setAttrValue(mount, "data-gosx-scene3d-device-memory", capability.deviceMemory > 0 ? capability.deviceMemory : "");
    setAttrValue(mount, "data-gosx-scene3d-hardware-concurrency", capability.hardwareConcurrency > 0 ? capability.hardwareConcurrency : "");
  }

  function applySceneRendererState(mount, renderer, fallbackReason) {
    if (!mount) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-renderer", renderer && renderer.kind ? renderer.kind : "");
    setAttrValue(mount, "data-gosx-scene3d-renderer-fallback", fallbackReason || "");
  }

  function observeSceneCapability(mount, props, capability, onChange) {
    if (!mount || !capability || typeof onChange !== "function") {
      return function() {};
    }
    applySceneCapabilityState(mount, props, capability);
    if (!(window.__gosx.environment && typeof window.__gosx.environment.observe === "function")) {
      return function() {};
    }
    return window.__gosx.environment.observe(function() {
      const next = sceneCapabilityProfile(props);
      if (!sceneCapabilityChanged(capability, next)) {
        return;
      }
      capability.tier = next.tier;
      capability.coarsePointer = next.coarsePointer;
      capability.hover = next.hover;
      capability.reducedData = next.reducedData;
      capability.lowPower = next.lowPower;
      capability.visualViewportActive = next.visualViewportActive;
      capability.deviceMemory = next.deviceMemory;
      capability.hardwareConcurrency = next.hardwareConcurrency;
      applySceneCapabilityState(mount, props, capability);
      onChange("capability");
    }, { immediate: false });
  }

  function sceneViewportBase(props) {
    const width = Math.max(240, sceneNumber(props && props.width, 720));
    const height = Math.max(180, sceneNumber(props && props.height, 420));
    const explicitMaxDevicePixelRatio = sceneNumber(props && (props.maxDevicePixelRatio || props.maxPixelRatio), 0);
    return {
      baseWidth: width,
      baseHeight: height,
      aspectRatio: width / Math.max(1, height),
      responsive: sceneBool(props && props.responsive, true),
      explicitMaxDevicePixelRatio,
    };
  }

  function sceneViewportDevicePixelRatio(props, maxDevicePixelRatio) {
    const environment = sceneEnvironmentState();
    const preferred = sceneNumber(
      props && (props.devicePixelRatio || props.pixelRatio),
      sceneNumber(window && window.devicePixelRatio, sceneNumber(environment && environment.devicePixelRatio, 1)),
    );
    return Math.max(1, Math.min(Math.max(1, maxDevicePixelRatio || 1), preferred));
  }

  function sceneViewportFromMount(mount, props, base, canvas, capability) {
    let cssWidth = base.baseWidth;
    let cssHeight = base.baseHeight;
    const useMeasuredHeight = sceneBool(props && (props.fillHeight || props.responsiveHeight), false);
    if (base.responsive) {
      const mountRect = mount && typeof mount.getBoundingClientRect === "function"
        ? mount.getBoundingClientRect()
        : null;
      const canvasRect = canvas && typeof canvas.getBoundingClientRect === "function"
        ? canvas.getBoundingClientRect()
        : null;
      const measuredCanvasWidth = sceneNumber(canvasRect && canvasRect.width, 0);
      const measuredMountWidth = sceneNumber(mountRect && mountRect.width, 0);
      if (measuredCanvasWidth > 0 && (measuredMountWidth <= 0 || measuredCanvasWidth <= measuredMountWidth * 1.5)) {
        cssWidth = measuredCanvasWidth;
      } else if (measuredMountWidth > 0) {
        cssWidth = measuredMountWidth;
      }
      const measuredHeight = measuredCanvasWidth > 0 && (measuredMountWidth <= 0 || measuredCanvasWidth <= measuredMountWidth * 1.5)
        ? sceneNumber(canvasRect && canvasRect.height, 0)
        : sceneNumber(mountRect && mountRect.height, 0);
      if (useMeasuredHeight && measuredHeight > 0) {
        cssHeight = measuredHeight;
      } else if (cssWidth > 0) {
        cssHeight = cssWidth / Math.max(0.0001, base.aspectRatio);
      }
    }
    cssWidth = Math.max(1, Math.round(cssWidth));
    cssHeight = Math.max(1, Math.round(cssHeight));
    const maxDevicePixelRatio = Math.max(1, base.explicitMaxDevicePixelRatio > 0 ? base.explicitMaxDevicePixelRatio : defaultSceneMaxDevicePixelRatio(capability));
    const devicePixelRatio = sceneViewportDevicePixelRatio(props, maxDevicePixelRatio);
    return {
      cssWidth,
      cssHeight,
      devicePixelRatio,
      pixelWidth: Math.max(1, Math.round(cssWidth * devicePixelRatio)),
      pixelHeight: Math.max(1, Math.round(cssHeight * devicePixelRatio)),
    };
  }

  function sceneViewportChanged(prev, next) {
    if (!prev || !next) {
      return true;
    }
    return prev.cssWidth !== next.cssWidth
      || prev.cssHeight !== next.cssHeight
      || prev.pixelWidth !== next.pixelWidth
      || prev.pixelHeight !== next.pixelHeight
      || Math.abs(sceneNumber(prev.devicePixelRatio, 1) - sceneNumber(next.devicePixelRatio, 1)) > 0.001;
  }

  function applySceneViewport(mount, canvas, labelLayer, viewport, base) {
    if (!mount || !canvas || !viewport) {
      return viewport;
    }
    setAttrValue(mount, "data-gosx-scene3d-css-width", viewport.cssWidth);
    setAttrValue(mount, "data-gosx-scene3d-css-height", viewport.cssHeight);
    setAttrValue(mount, "data-gosx-scene3d-pixel-ratio", viewport.devicePixelRatio);
    setStyleValue(mount.style, "--gosx-scene-css-width", viewport.cssWidth + "px");
    setStyleValue(mount.style, "--gosx-scene-css-height", viewport.cssHeight + "px");
    setStyleValue(mount.style, "--gosx-scene-pixel-ratio", String(viewport.devicePixelRatio));
    canvas.width = viewport.pixelWidth;
    canvas.height = viewport.pixelHeight;
    canvas.setAttribute("width", String(viewport.pixelWidth));
    canvas.setAttribute("height", String(viewport.pixelHeight));
    if (labelLayer) {
      const mountRect = typeof mount.getBoundingClientRect === "function" ? mount.getBoundingClientRect() : null;
      const canvasRect = typeof canvas.getBoundingClientRect === "function" ? canvas.getBoundingClientRect() : null;
      const left = mountRect && canvasRect ? Math.max(0, sceneNumber(canvasRect.left, 0) - sceneNumber(mountRect.left, 0)) : 0;
      const top = mountRect && canvasRect ? Math.max(0, sceneNumber(canvasRect.top, 0) - sceneNumber(mountRect.top, 0)) : 0;
      labelLayer.style.left = left + "px";
      labelLayer.style.top = top + "px";
      labelLayer.style.right = "auto";
      labelLayer.style.bottom = "auto";
      labelLayer.style.width = viewport.cssWidth + "px";
      labelLayer.style.height = viewport.cssHeight + "px";
    }
    if (base && !base.responsive) {
      canvas.style.width = viewport.cssWidth + "px";
      canvas.style.height = viewport.cssHeight + "px";
    } else {
      canvas.style.width = "100%";
      canvas.style.height = "auto";
    }
    return viewport;
  }

  function observeSceneViewport(mount, refresh) {
    if (!mount || typeof refresh !== "function") {
      return function() {};
    }
    let resizeObserver = null;
    let windowResizeListener = null;
    let stopEnvironment = null;

    if (typeof ResizeObserver === "function") {
      resizeObserver = new ResizeObserver(function() {
        refresh("resize");
      });
      if (typeof resizeObserver.observe === "function") {
        resizeObserver.observe(mount);
      }
    } else if (typeof window.addEventListener === "function") {
      windowResizeListener = function() {
        refresh("resize");
      };
      window.addEventListener("resize", windowResizeListener);
    }

    if (window.__gosx.environment && typeof window.__gosx.environment.observe === "function") {
      stopEnvironment = window.__gosx.environment.observe(function() {
        refresh("environment");
      }, { immediate: false });
    }

    return function() {
      if (resizeObserver && typeof resizeObserver.disconnect === "function") {
        resizeObserver.disconnect();
      }
      if (windowResizeListener && typeof window.removeEventListener === "function") {
        window.removeEventListener("resize", windowResizeListener);
      }
      if (typeof stopEnvironment === "function") {
        stopEnvironment();
      }
    };
  }

  function initialSceneLifecycleState() {
    const environment = sceneEnvironmentState();
    return {
      pageVisible: environment ? Boolean(environment.pageVisible) : String(document && document.visibilityState || "visible").toLowerCase() !== "hidden",
      inViewport: true,
    };
  }

  function initialSceneMotionState(props) {
    const respectReducedMotion = sceneBool(props && props.respectReducedMotion, true);
    const environment = sceneEnvironmentState();
    return {
      respectReducedMotion,
      reducedMotion: respectReducedMotion && environment
        ? Boolean(environment.reducedMotion)
        : sceneMediaQueryMatches("(prefers-reduced-motion: reduce)"),
    };
  }

  function applySceneMotionState(mount, motion) {
    if (!mount || !motion) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-reduced-motion", motion.reducedMotion ? "true" : "false");
  }

  function observeSceneMotion(mount, motion, onChange) {
    if (!mount || !motion || typeof onChange !== "function") {
      return function() {};
    }

    applySceneMotionState(mount, motion);
    if (!motion.respectReducedMotion || !(window.__gosx.environment && typeof window.__gosx.environment.observe === "function")) {
      return function() {};
    }

    return window.__gosx.environment.observe(function(environment) {
      const next = Boolean(environment && environment.reducedMotion);
      if (motion.reducedMotion === next) {
        return;
      }
      motion.reducedMotion = next;
      applySceneMotionState(mount, motion);
      onChange("motion");
    }, { immediate: false });
  }

  function applySceneLifecycleState(mount, lifecycle) {
    if (!mount || !lifecycle) {
      return;
    }
    setAttrValue(mount, "data-gosx-scene3d-page-visible", lifecycle.pageVisible ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-in-viewport", lifecycle.inViewport ? "true" : "false");
    setAttrValue(mount, "data-gosx-scene3d-active", lifecycle.pageVisible && lifecycle.inViewport ? "true" : "false");
  }

  function observeSceneLifecycle(mount, lifecycle, onChange) {
    if (!mount || !lifecycle || typeof onChange !== "function") {
      return function() {};
    }

    let stopIntersection = null;
    let stopEnvironment = null;

    if (typeof IntersectionObserver === "function") {
      const observer = new IntersectionObserver(function(entries) {
        for (const entry of entries || []) {
          if (!entry || entry.target !== mount) {
            continue;
          }
          const next = entry.isIntersecting !== false && sceneNumber(entry.intersectionRatio, 1) > 0;
          if (lifecycle.inViewport === next) {
            continue;
          }
          lifecycle.inViewport = next;
          applySceneLifecycleState(mount, lifecycle);
          onChange("intersection");
        }
      }, { threshold: [0, 0.01, 0.25] });
      if (typeof observer.observe === "function") {
        observer.observe(mount);
      }
      stopIntersection = function() {
        if (typeof observer.disconnect === "function") {
          observer.disconnect();
        }
      };
    }

    if (window.__gosx.environment && typeof window.__gosx.environment.observe === "function") {
      stopEnvironment = window.__gosx.environment.observe(function(environment) {
        const next = Boolean(environment && environment.pageVisible);
        if (lifecycle.pageVisible === next) {
          return;
        }
        lifecycle.pageVisible = next;
        applySceneLifecycleState(mount, lifecycle);
        onChange("visibility");
      }, { immediate: false });
    }

    applySceneLifecycleState(mount, lifecycle);
    return function() {
      if (stopIntersection) {
        stopIntersection();
      }
      if (typeof stopEnvironment === "function") {
        stopEnvironment();
      }
    };
  }

  function sceneLabelLayoutKey(label) {
    return [
      gosxTextLayoutRevision(),
      label.text,
      label.font,
      sceneNumber(label.maxWidth, 180),
      Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
      normalizeTextLayoutOverflow(label.overflow),
      normalizeSceneLabelWhiteSpace(label.whiteSpace),
      sceneNumber(label.lineHeight, 18),
      normalizeSceneLabelAlign(label.textAlign),
    ].join("\n");
  }

  function sceneMeasureTextWidth(font, text) {
    if (typeof window.__gosx_measure_text_batch !== "function") {
      return String(text || "").length * 8;
    }
    try {
      const raw = window.__gosx_measure_text_batch(font, JSON.stringify([String(text || "")]));
      const widths = typeof raw === "string" ? JSON.parse(raw) : raw;
      return Array.isArray(widths) && widths.length > 0 ? sceneNumber(widths[0], String(text || "").length * 8) : String(text || "").length * 8;
    } catch (_error) {
      return String(text || "").length * 8;
    }
  }

  function fallbackSceneLabelLayout(label) {
    return layoutBrowserText(
      String(label.text || ""),
      label.font,
      sceneNumber(label.maxWidth, 180),
      normalizeSceneLabelWhiteSpace(label.whiteSpace),
      sceneNumber(label.lineHeight, 18),
      {
        maxLines: Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
        overflow: normalizeTextLayoutOverflow(label.overflow),
      },
    );
  }

  function layoutSceneLabel(label, layoutCache) {
    const revision = gosxTextLayoutRevision();
    if (layoutCache.__gosxRevision !== revision) {
      layoutCache.clear();
      layoutCache.__gosxRevision = revision;
    }
    const cacheKey = sceneLabelLayoutKey(label);
    if (layoutCache.has(cacheKey)) {
      return {
        key: cacheKey,
        value: layoutCache.get(cacheKey),
      };
    }

    let layout = null;
    if (typeof window.__gosx_text_layout === "function") {
      try {
        layout = window.__gosx_text_layout(
          label.text,
          label.font,
          sceneNumber(label.maxWidth, 180),
          normalizeSceneLabelWhiteSpace(label.whiteSpace),
          sceneNumber(label.lineHeight, 18),
          {
            maxLines: Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
            overflow: normalizeTextLayoutOverflow(label.overflow),
          },
        );
      } catch (error) {
        console.error("[gosx] scene label layout failed:", error);
      }
    }

    if (!layout || !Array.isArray(layout.lines)) {
      layout = fallbackSceneLabelLayout(label);
    }
    if (layoutCache.size >= sceneLabelLayoutCacheLimit) {
      const oldest = layoutCache.keys().next();
      if (!oldest.done) {
        layoutCache.delete(oldest.value);
      }
    }
    layoutCache.set(cacheKey, layout);
    return {
      key: cacheKey,
      value: layout,
    };
  }

  const sceneLabelPaddingX = 10;
  const sceneLabelPaddingY = 8;

  function sceneLabelBoxMetrics(label, layout) {
    const contentWidth = Math.max(
      1,
      Math.min(
        sceneNumber(label.maxWidth, 180),
        Math.max(1, Math.ceil(sceneNumber(layout && layout.maxLineWidth, 0) || sceneMeasureTextWidth(label.font, label.text)))
      )
    );
    const contentHeight = Math.max(
      sceneNumber(label.lineHeight, 18),
      Math.ceil(sceneNumber(layout && layout.height, sceneNumber(label.lineHeight, 18)))
    );
    return {
      contentWidth,
      contentHeight,
      totalWidth: contentWidth + (sceneLabelPaddingX * 2),
      totalHeight: contentHeight + (sceneLabelPaddingY * 2),
      maxTotalWidth: Math.max(contentWidth + (sceneLabelPaddingX * 2), sceneNumber(label.maxWidth, 180) + (sceneLabelPaddingX * 2)),
    };
  }

  function sceneLabelBounds(label, metrics) {
    const anchorX = sceneNumber(label.anchorX, 0.5);
    const anchorY = sceneNumber(label.anchorY, 1);
    const anchorPointX = sceneNumber(label.position && label.position.x, 0) + sceneNumber(label.offsetX, 0);
    const anchorPointY = sceneNumber(label.position && label.position.y, 0) + sceneNumber(label.offsetY, 0);
    const left = anchorPointX - (anchorX * metrics.totalWidth);
    const top = anchorPointY - (anchorY * metrics.totalHeight);
    return {
      left,
      top,
      right: left + metrics.totalWidth,
      bottom: top + metrics.totalHeight,
      anchor: { x: anchorPointX, y: anchorPointY },
      center: { x: left + (metrics.totalWidth / 2), y: top + (metrics.totalHeight / 2) },
    };
  }

  function sceneRectArea(box) {
    if (!box) {
      return 0;
    }
    return Math.max(0, box.right - box.left) * Math.max(0, box.bottom - box.top);
  }

  function sceneRectOverlapArea(a, b) {
    if (!a || !b) {
      return 0;
    }
    const overlapX = Math.max(0, Math.min(a.right, b.maxX == null ? b.right : b.maxX) - Math.max(a.left, b.minX == null ? b.left : b.minX));
    const overlapY = Math.max(0, Math.min(a.bottom, b.maxY == null ? b.bottom : b.maxY) - Math.max(a.top, b.minY == null ? b.top : b.minY));
    return overlapX * overlapY;
  }

  function sceneRectsIntersect(a, b) {
    return sceneRectOverlapArea(a, b) > 0;
  }

  function sceneBoundsContainPoint(bounds, point) {
    if (!bounds || !point) {
      return false;
    }
    return point.x >= bounds.minX && point.x <= bounds.maxX && point.y >= bounds.minY && point.y <= bounds.maxY;
  }

  function buildSceneLabelOccluders(bundle, width, height) {
    if (!bundle || !bundle.camera || !Array.isArray(bundle.objects) || !bundle.objects.length) {
      return [];
    }
    const occluders = [];
    for (const object of bundle.objects) {
      if (!object || object.viewCulled) {
        continue;
      }
      const segments = sceneProjectedObjectSegments(bundle, object, width, height);
      if (!segments.length) {
        continue;
      }
      const bounds = sceneProjectedSegmentsBounds(segments);
      if (!bounds) {
        continue;
      }
      occluders.push({
        depth: sceneNumber(object.depthCenter, sceneObjectDepthCenter(object, bundle.camera)),
        bounds,
        hull: sceneProjectedObjectHull(segments),
      });
    }
    occluders.sort(function(a, b) {
      return a.depth - b.depth;
    });
    return occluders;
  }

  function sceneLabelOccluded(entry, occluders) {
    if (!entry || !entry.label || !entry.label.occlude || !Array.isArray(occluders) || !occluders.length) {
      return false;
    }
    const labelDepth = sceneNumber(entry.label.depth, 0);
    for (const occluder of occluders) {
      if (occluder.depth > labelDepth + 0.05) {
        continue;
      }
      if (!sceneRectsIntersect(entry.box, occluder.bounds)) {
        continue;
      }
      if (scenePointInPolygon(entry.box.anchor, occluder.hull) || sceneBoundsContainPoint(occluder.bounds, entry.box.anchor)) {
        return true;
      }
      if (scenePointInPolygon(entry.box.center, occluder.hull)) {
        return true;
      }
      const overlapRatio = sceneRectOverlapArea(entry.box, occluder.bounds) / Math.max(1, sceneRectArea(entry.box));
      if (overlapRatio >= 0.28) {
        return true;
      }
    }
    return false;
  }

  function sceneLabelPriorityCompare(a, b) {
    const priorityDiff = sceneNumber(b && b.label && b.label.priority, 0) - sceneNumber(a && a.label && a.label.priority, 0);
    if (Math.abs(priorityDiff) > 0.001) {
      return priorityDiff;
    }
    const depthDiff = sceneNumber(a && a.label && a.label.depth, 0) - sceneNumber(b && b.label && b.label.depth, 0);
    if (Math.abs(depthDiff) > 0.001) {
      return depthDiff;
    }
    return sceneNumber(a && a.order, 0) - sceneNumber(b && b.order, 0);
  }

  function prepareSceneLabelEntries(bundle, layoutCache, width, height) {
    const labels = bundle && Array.isArray(bundle.labels) ? bundle.labels : [];
    const occluders = buildSceneLabelOccluders(bundle, width, height);
    const entries = [];
    for (let index = 0; index < labels.length; index += 1) {
      const label = labels[index];
      if (!label || typeof label.text !== "string" || label.text.trim() === "") {
        continue;
      }
      const layout = layoutSceneLabel(label, layoutCache);
      const metrics = sceneLabelBoxMetrics(label, layout.value);
      const box = sceneLabelBounds(label, metrics);
      entries.push({
        id: label.id || ("scene-label-" + index),
        order: index,
        label,
        layoutKey: layout.key,
        layout: layout.value,
        metrics,
        box,
        occluded: false,
        hidden: false,
      });
    }

    const sorted = entries.slice().sort(sceneLabelPriorityCompare);
    const occupied = [];
    for (const entry of sorted) {
      entry.occluded = sceneLabelOccluded(entry, occluders);
      if (entry.occluded) {
        entry.hidden = true;
        continue;
      }
      if (normalizeSceneLabelCollision(entry.label.collision) !== "allow") {
        for (const prior of occupied) {
          if (sceneRectsIntersect(entry.box, prior)) {
            entry.hidden = true;
            break;
          }
        }
      }
      if (!entry.hidden) {
        occupied.push(entry.box);
      }
    }

    return entries;
  }

  function renderSceneLabelElement(element, label, layoutKey, layout, metrics, box, hidden, occluded) {
    const align = normalizeSceneLabelAlign(label.textAlign);
    const whiteSpace = normalizeSceneLabelWhiteSpace(label.whiteSpace);
    const zIndex = Math.max(1, 1000 + Math.round(sceneNumber(label.priority, 0) * 10) - Math.round(sceneNumber(label.depth, 0) * 10));

    element.setAttribute("data-gosx-scene-label", label.id || "");
    setAttrValue(element, "class", label.className ? ("gosx-scene-label " + label.className) : "gosx-scene-label");
    setAttrValue(element, "data-gosx-scene-label-collision", normalizeSceneLabelCollision(label.collision));
    setAttrValue(element, "data-gosx-scene-label-occlude", label.occlude ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-label-occluded", occluded ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-label-visibility", hidden ? "hidden" : "visible");
    setAttrValue(element, "data-gosx-scene-label-priority", sceneNumber(label.priority, 0));
    setAttrValue(element, "data-gosx-scene-label-depth", sceneNumber(label.depth, 0));
    setAttrValue(element, "data-gosx-scene-label-truncated", layout && layout.truncated ? "true" : "false");

    applyTextLayoutPresentation(element, {
      font: label.font,
      whiteSpace: whiteSpace,
      lineHeight: sceneNumber(label.lineHeight, 18),
      maxLines: Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
      overflow: normalizeTextLayoutOverflow(label.overflow),
      maxWidth: sceneNumber(label.maxWidth, 180),
    }, layout, {
      role: "label",
      surface: "scene3d",
      state: "ready",
      align: align,
      revision: gosxTextLayoutRevision(),
    });

    setStyleValue(element.style, "--gosx-scene-label-left", box.anchor.x + "px");
    setStyleValue(element.style, "--gosx-scene-label-top", box.anchor.y + "px");
    setStyleValue(element.style, "--gosx-scene-label-anchor-x", String(sceneNumber(label.anchorX, 0.5)));
    setStyleValue(element.style, "--gosx-scene-label-anchor-y", String(sceneNumber(label.anchorY, 1)));
    setStyleValue(element.style, "--gosx-scene-label-width", metrics.totalWidth + "px");
    setStyleValue(element.style, "--gosx-scene-label-max-width", metrics.maxTotalWidth + "px");
    setStyleValue(element.style, "--gosx-scene-label-height", metrics.totalHeight + "px");
    setStyleValue(element.style, "--gosx-scene-label-line-height", sceneNumber(label.lineHeight, 18) + "px");
    setStyleValue(element.style, "--gosx-scene-label-align", align);
    setStyleValue(element.style, "--gosx-scene-label-white-space", whiteSpace);
    setStyleValue(element.style, "--gosx-scene-label-font", label.font || '600 13px "IBM Plex Sans", "Segoe UI", sans-serif');
    setStyleValue(element.style, "--gosx-scene-label-color", label.color || "#ecf7ff");
    setStyleValue(element.style, "--gosx-scene-label-background", label.background || "rgba(8, 21, 31, 0.82)");
    setStyleValue(element.style, "--gosx-scene-label-border-color", label.borderColor || "rgba(141, 225, 255, 0.24)");
    setStyleValue(element.style, "--gosx-scene-label-z-index", String(zIndex));
    setStyleValue(element.style, "--gosx-scene-label-depth", String(sceneNumber(label.depth, 0)));
    element.__gosxTextLayout = layout;

    if (element.__gosxLayoutKey === layoutKey) {
      return;
    }

    clearChildren(element);
    const lines = Array.isArray(layout.lines) && layout.lines.length > 0 ? layout.lines : [{ text: label.text }];
    for (const line of lines) {
      const lineElement = document.createElement("div");
      lineElement.setAttribute("data-gosx-scene-label-line", "");
      lineElement.textContent = line && typeof line.text === "string" && line.text !== "" ? line.text : "\u00a0";
      if (whiteSpace !== "normal") {
        lineElement.style.whiteSpace = whiteSpace;
      }
      element.appendChild(lineElement);
    }
    element.__gosxLayoutKey = layoutKey;
  }

  function renderSceneLabels(layer, bundle, layoutCache, elements, width, height) {
    if (!layer) {
      return;
    }

    const labels = prepareSceneLabelEntries(bundle, layoutCache, width, height);
    const active = new Set();

    for (const entry of labels) {
      const id = entry.id;
      active.add(id);
      let element = elements.get(id);
      if (!element) {
        element = document.createElement("div");
        layer.appendChild(element);
        elements.set(id, element);
      }
      renderSceneLabelElement(element, entry.label, entry.layoutKey, entry.layout, entry.metrics, entry.box, entry.hidden, entry.occluded);
    }

    for (const [id, element] of elements.entries()) {
      if (active.has(id)) {
        continue;
      }
      if (element.parentNode === layer) {
        layer.removeChild(element);
      }
      elements.delete(id);
    }
  }

  window.__gosx_register_engine_factory("GoSXScene3D", async function(ctx) {
    if (!ctx.mount || typeof document.createElement !== "function") {
      console.warn("[gosx] Scene3D requires a mount element");
      return {};
    }

    const props = ctx.props || {};
    const capability = sceneCapabilityProfile(props);
    const viewportBase = sceneViewportBase(props);
    const sceneState = createSceneState(props);
    const runtimeScene = ctx.runtimeMode === "shared" && Boolean(ctx.programRef);
    const objects = sceneStateObjects(sceneState);
    const lifecycle = initialSceneLifecycleState();
    const motion = initialSceneMotionState(props);

    function sceneShouldAnimate() {
      if (motion.reducedMotion) {
        return false;
      }
      if (runtimeScene || sceneBool(props.autoRotate, true)) {
        return true;
      }
      return sceneStateObjects(sceneState).some(sceneObjectAnimated) || sceneStateLabels(sceneState).some(sceneLabelAnimated);
    }

    clearChildren(ctx.mount);
    ctx.mount.setAttribute("data-gosx-scene3d-mounted", "true");
    ctx.mount.setAttribute("aria-label", props.ariaLabel || props.label || "Interactive GoSX 3D scene");
    applySceneCapabilityState(ctx.mount, props, capability);
    if (!ctx.mount.style.position) {
      ctx.mount.style.position = "relative";
    }
    const canvas = document.createElement("canvas");
    canvas.setAttribute("data-gosx-scene3d-canvas", "true");
    canvas.setAttribute("role", "img");
    canvas.setAttribute("aria-label", props.label || "Interactive GoSX 3D scene");
    canvas.style.maxWidth = "100%";
    canvas.style.borderRadius = "inherit";
    canvas.width = viewportBase.baseWidth;
    canvas.height = viewportBase.baseHeight;
    canvas.setAttribute("width", String(viewportBase.baseWidth));
    canvas.setAttribute("height", String(viewportBase.baseHeight));
    ctx.mount.appendChild(canvas);

    const labelLayer = document.createElement("div");
    labelLayer.setAttribute("data-gosx-scene3d-label-layer", "true");
    labelLayer.setAttribute("aria-hidden", "true");
    ctx.mount.appendChild(labelLayer);

    let viewport = applySceneViewport(ctx.mount, canvas, labelLayer, sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability), viewportBase);

    const initialRenderer = createSceneRenderer(canvas, props, capability);
    if (!initialRenderer || !initialRenderer.renderer) {
      console.warn("[gosx] Scene3D could not acquire a renderer");
      return {
        dispose() {
          if (canvas.parentNode === ctx.mount) {
            ctx.mount.removeChild(canvas);
          }
          if (labelLayer.parentNode === ctx.mount) {
            ctx.mount.removeChild(labelLayer);
          }
        },
      };
    }
    let renderer = initialRenderer.renderer;
    applySceneRendererState(ctx.mount, renderer, initialRenderer.fallbackReason || "");
    let latestBundle = null;
    const labelLayoutCache = new Map();
    const labelElements = new Map();
    let labelRefreshHandle = null;
    const dragHandle = setupSceneDragInteractions(canvas, props, function() {
      return viewport;
    }, function() {
      return latestBundle;
    });
    const releaseTextLayoutListener = onTextLayoutInvalidated(function() {
      if (disposed || !latestBundle || !sceneCanRender()) {
        return;
      }
      if (labelRefreshHandle != null) {
        return;
      }
      labelRefreshHandle = engineFrame(function() {
        labelRefreshHandle = null;
        if (disposed || !latestBundle) {
          return;
        }
        renderSceneLabels(labelLayer, latestBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
      });
    });

    let frameHandle = null;
    let scheduledRenderHandle = null;
    let disposed = false;

    function swapRenderer(nextRenderer, fallbackReason) {
      if (!nextRenderer) {
        return false;
      }
      const previous = renderer;
      renderer = nextRenderer;
      applySceneRendererState(ctx.mount, renderer, fallbackReason);
      if (previous && previous !== renderer && typeof previous.dispose === "function") {
        previous.dispose();
      }
      return true;
    }

    function fallbackSceneRenderer(reason) {
      const ctx2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
      if (!ctx2d) {
        return false;
      }
      return swapRenderer(createSceneCanvasRenderer(ctx2d, canvas), reason || "webgl-unavailable");
    }

    function restoreSceneWebGLRenderer(reason) {
      const webglPreference = sceneCapabilityWebGLPreference(props, capability);
      if (!(webglPreference === "prefer" || webglPreference === "force")) {
        return false;
      }
      const webglRenderer = createSceneWebGLRenderer(canvas, {
        antialias: capability.tier === "full" && !capability.lowPower && !capability.reducedData,
        powerPreference: capability.lowPower || capability.tier === "constrained" ? "low-power" : "high-performance",
      });
      if (!webglRenderer) {
        return false;
      }
      return swapRenderer(webglRenderer, reason || "");
    }

    function onWebGLContextLost(event) {
      if (!renderer || renderer.kind !== "webgl") {
        return;
      }
      if (event && typeof event.preventDefault === "function") {
        event.preventDefault();
      }
      fallbackSceneRenderer("webgl-context-lost");
      scheduleRender("webgl-context-lost");
    }

    function onWebGLContextRestored() {
      if (restoreSceneWebGLRenderer("")) {
        scheduleRender("webgl-context-restored");
      }
    }

    canvas.addEventListener("webglcontextlost", onWebGLContextLost);
    canvas.addEventListener("webglcontextrestored", onWebGLContextRestored);

    function sceneCanRender() {
      return lifecycle.pageVisible && lifecycle.inViewport;
    }

    function sceneWantsAnimation() {
      return sceneShouldAnimate() && sceneCanRender();
    }

    function cancelFrame() {
      if (frameHandle != null) {
        cancelEngineFrame(frameHandle);
        frameHandle = null;
      }
    }

    function cancelScheduledRender() {
      if (scheduledRenderHandle != null) {
        cancelEngineFrame(scheduledRenderHandle);
        scheduledRenderHandle = null;
      }
    }

    function scheduleRender(reason) {
      if (disposed) {
        return;
      }
      const nextViewport = sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability);
      if (sceneViewportChanged(viewport, nextViewport)) {
        viewport = applySceneViewport(ctx.mount, canvas, labelLayer, nextViewport, viewportBase);
      }
      if (!sceneCanRender()) {
        cancelFrame();
        cancelScheduledRender();
        return;
      }
      if (scheduledRenderHandle != null) {
        return;
      }
      scheduledRenderHandle = engineFrame(function(now) {
        scheduledRenderHandle = null;
        renderFrame(typeof now === "number" ? now : 0, reason || "refresh");
      });
    }

    const releaseViewportObserver = observeSceneViewport(ctx.mount, scheduleRender);
    const releaseCapabilityObserver = observeSceneCapability(ctx.mount, props, capability, function(reason) {
      const nextViewport = sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability);
      if (sceneViewportChanged(viewport, nextViewport)) {
        viewport = applySceneViewport(ctx.mount, canvas, labelLayer, nextViewport, viewportBase);
      }
      const desiredFallback = sceneRendererFallbackReason(props, capability, renderer && renderer.kind);
      const webglPreference = sceneCapabilityWebGLPreference(props, capability);
      if (renderer && renderer.kind === "webgl" && !(webglPreference === "prefer" || webglPreference === "force")) {
        fallbackSceneRenderer(desiredFallback || "environment-constrained");
      } else if (renderer && renderer.kind !== "webgl" && (webglPreference === "prefer" || webglPreference === "force")) {
        if (!restoreSceneWebGLRenderer("")) {
          applySceneRendererState(ctx.mount, renderer, desiredFallback);
        }
      } else {
        applySceneRendererState(ctx.mount, renderer, desiredFallback);
      }
      scheduleRender(reason || "capability");
    });
    const releaseLifecycleObserver = observeSceneLifecycle(ctx.mount, lifecycle, function(reason) {
      if (!sceneCanRender()) {
        cancelFrame();
        cancelScheduledRender();
        if (labelRefreshHandle != null) {
          cancelEngineFrame(labelRefreshHandle);
          labelRefreshHandle = null;
        }
        return;
      }
      scheduleRender(reason || "lifecycle");
    });
    const releaseMotionObserver = observeSceneMotion(ctx.mount, motion, function(reason) {
      cancelFrame();
      cancelScheduledRender();
      scheduleRender(reason || "motion");
    });

    if (runtimeScene) {
      if (ctx.runtime && ctx.runtime.available()) {
        applySceneCommands(sceneState, await ctx.runtime.hydrateFromProgramRef());
      } else {
        console.warn("[gosx] Scene3D runtime requested but shared engine runtime is unavailable");
      }
    }

    function renderFrame(now) {
      if (disposed) return;
      viewport = applySceneViewport(ctx.mount, canvas, labelLayer, sceneViewportFromMount(ctx.mount, props, viewportBase, canvas, capability), viewportBase);
      if (!sceneCanRender()) {
        cancelFrame();
        return;
      }
      const timeSeconds = now / 1000;
      if (runtimeScene && ctx.runtime && typeof ctx.runtime.renderFrame === "function") {
        const runtimeBundle = ctx.runtime.renderFrame(timeSeconds, viewport.cssWidth, viewport.cssHeight);
        if (runtimeBundle) {
          latestBundle = runtimeBundle;
          renderer.render(runtimeBundle, viewport);
          renderSceneLabels(labelLayer, runtimeBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
          if (sceneWantsAnimation()) {
            frameHandle = engineFrame(renderFrame);
          }
          return;
        }
      }
      if (runtimeScene && ctx.runtime) {
        applySceneCommands(sceneState, ctx.runtime.tick());
      }
      latestBundle = createSceneRenderBundle(
        viewport.cssWidth,
        viewport.cssHeight,
        sceneState.background,
        sceneState.camera,
        sceneStateObjects(sceneState),
        sceneStateLabels(sceneState),
        timeSeconds,
      );
      renderer.render(latestBundle, viewport);
      renderSceneLabels(labelLayer, latestBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
      if (sceneWantsAnimation()) {
        frameHandle = engineFrame(renderFrame);
      }
    }

    renderFrame(0);

    ctx.emit("mounted", {
      width: viewport.cssWidth,
      height: viewport.cssHeight,
      objects: objects.length,
      labels: sceneStateLabels(sceneState).length,
    });

    return {
      applyCommands(commands) {
        applySceneCommands(sceneState, commands);
        scheduleRender("commands");
      },
      dispose() {
        disposed = true;
        canvas.removeEventListener("webglcontextlost", onWebGLContextLost);
        canvas.removeEventListener("webglcontextrestored", onWebGLContextRestored);
        releaseViewportObserver();
        releaseCapabilityObserver();
        releaseLifecycleObserver();
        releaseMotionObserver();
        releaseTextLayoutListener();
        dragHandle.dispose();
        renderer.dispose();
        cancelFrame();
        cancelScheduledRender();
        if (labelRefreshHandle != null) {
          cancelEngineFrame(labelRefreshHandle);
        }
        if (canvas.parentNode === ctx.mount) {
          ctx.mount.removeChild(canvas);
        }
        if (labelLayer.parentNode === ctx.mount) {
          ctx.mount.removeChild(labelLayer);
        }
      },
    };
  });

  const DELEGATED_EVENTS = [
    "click", "input", "change", "submit",
    "keydown", "keyup", "focus", "blur",
  ];

  function extractEventData(e) {
    const data = { type: e.type };

    switch (e.type) {
      case "click":
        if (e.target && e.target.value !== undefined) {
          const value = String(e.target.value == null ? "" : e.target.value);
          if (value !== "") {
            data.value = value;
          }
        }
        break;
      case "input":
      case "change":
        if (e.target && e.target.value !== undefined) {
          data.value = e.target.value;
        }
        break;
      case "keydown":
      case "keyup":
        data.key = e.key;
        break;
      case "submit":
        e.preventDefault();
        break;
    }

    return data;
  }

  function findHandlerForEvent(target, root, eventType) {
    const specificAttr = handlerAttrName(eventType);

    let el = target;
    while (el && el !== root.parentNode) {
      const handlerName = elementHandlerName(el, eventType, specificAttr);
      if (handlerName) {
        return handlerName;
      }
      el = el.parentNode;
    }
    return null;
  }

  function handlerAttrName(eventType) {
    return "data-gosx-on-" + eventType;
  }

  function elementHandlerName(el, eventType, specificAttr) {
    if (hasAttributeName(el, specificAttr)) {
      return el.getAttribute(specificAttr);
    }
    if (eventType === "click" && hasAttributeName(el, "data-gosx-handler")) {
      return el.getAttribute("data-gosx-handler");
    }
    return null;
  }

  function hasAttributeName(el, attr) {
    return Boolean(el && el.hasAttribute && el.hasAttribute(attr));
  }

  function setupEventDelegation(islandRoot, islandID) {
    const entries = [];

    for (const eventType of DELEGATED_EVENTS) {
      const listener = createDelegatedListener(islandRoot, islandID, eventType);
      const useCapture = delegatedEventCapture(eventType);
      islandRoot.addEventListener(eventType, listener, useCapture);
      entries.push({ type: eventType, listener, capture: useCapture });
    }

    return entries;
  }

  function delegatedEventCapture(eventType) {
    return eventType === "focus" || eventType === "blur";
  }

  function createDelegatedListener(islandRoot, islandID, eventType) {
    return function(e) {
      if (e.__gosx_handled) return;

      const handlerName = findHandlerForEvent(e.target, islandRoot, eventType);
      if (!handlerName) return;

      e.__gosx_handled = true;
      dispatchIslandAction(islandID, handlerName, extractEventData(e));
    };
  }

  function dispatchIslandAction(islandID, handlerName, eventData) {
    const actionFn = window.__gosx_action;
    if (typeof actionFn !== "function") return;

    try {
      const result = actionFn(islandID, handlerName, JSON.stringify(eventData));
      if (typeof result === "string" && result !== "") {
        console.error(`[gosx] action error (${islandID}/${handlerName}):`, result);
      }
    } catch (err) {
      console.error(`[gosx] action error (${islandID}/${handlerName}):`, err);
    }
  }

  function resolveEngineFactory(entry) {
    const builtin = resolveBuiltinEngineFactory(entry);
    if (builtin) {
      return builtin;
    }
    const exportName = engineExportName(entry);
    if (!exportName) return null;
    return engineFactories[exportName] || null;
  }

  function resolveBuiltinEngineFactory(entry) {
    if (!entry || !engineKindUsesBuiltinFactory(entry.kind)) {
      return null;
    }
    if (entry.kind === "video") {
      return createBuiltInVideoEngine;
    }
    return null;
  }

  function engineExportName(entry) {
    return entry.jsExport || entry.component;
  }

  function normalizeEngineHandle(result) {
    if (typeof result === "function") {
      return { dispose: result };
    }
    if (result && typeof result === "object") {
      return result;
    }
    return {};
  }

  function engineUsesSharedRuntime(entry) {
    return entry && entry.runtime === "shared";
  }

  const pendingEngineRuntimes = new Map();

  function pixelSurfaceCapabilityEnabled(entry) {
    return Boolean(entry && Array.isArray(entry.capabilities) && entry.capabilities.includes("pixel-surface"));
  }

  function pixelSurfaceDimension(value, fallback) {
    const num = Math.floor(Number(value));
    return Number.isFinite(num) && num > 0 ? num : fallback;
  }

  function normalizePixelSurfaceScaling(value) {
    const mode = String(value || "pixel-perfect").trim().toLowerCase();
    switch (mode) {
      case "fill":
      case "stretch":
        return mode;
      default:
        return "pixel-perfect";
    }
  }

  function normalizePixelSurfaceClearColor(value) {
    const color = Array.isArray(value) ? value : [0, 0, 0, 255];
    const out = [0, 0, 0, 255];
    for (let i = 0; i < out.length; i += 1) {
      const num = Math.max(0, Math.min(255, Math.floor(Number(color[i])) || 0));
      out[i] = num;
    }
    return out;
  }

  function pixelSurfaceBackgroundColor(clearColor) {
    return "rgba(" + clearColor[0] + ", " + clearColor[1] + ", " + clearColor[2] + ", " + (clearColor[3] / 255) + ")";
  }

  function resolvePixelSurfaceConfig(entry, mount) {
    if (!pixelSurfaceCapabilityEnabled(entry)) {
      return null;
    }
    const source = entry && entry.pixelSurface && typeof entry.pixelSurface === "object" ? entry.pixelSurface : {};
    const widthAttr = mount && typeof mount.getAttribute === "function" ? mount.getAttribute("data-gosx-pixel-width") : "";
    const heightAttr = mount && typeof mount.getAttribute === "function" ? mount.getAttribute("data-gosx-pixel-height") : "";
    const scalingAttr = mount && typeof mount.getAttribute === "function" ? mount.getAttribute("data-gosx-pixel-scaling") : "";
    const width = pixelSurfaceDimension(source.width, pixelSurfaceDimension(widthAttr, 0));
    const height = pixelSurfaceDimension(source.height, pixelSurfaceDimension(heightAttr, 0));
    if (width <= 0 || height <= 0) {
      return null;
    }
    return {
      width,
      height,
      scaling: normalizePixelSurfaceScaling(source.scaling || scalingAttr),
      clearColor: normalizePixelSurfaceClearColor(source.clearColor),
      vsync: source.vsync !== false,
    };
  }

  function pixelSurfaceLayout(config, mount) {
    const rect = mount && typeof mount.getBoundingClientRect === "function" ? mount.getBoundingClientRect() : null;
    const surfaceWidth = Math.max(1, pixelSurfaceDimension(rect && rect.width, pixelSurfaceDimension(mount && mount.width, config.width)));
    const surfaceHeight = Math.max(1, pixelSurfaceDimension(rect && rect.height, pixelSurfaceDimension(mount && mount.height, config.height)));
    let drawWidth = surfaceWidth;
    let drawHeight = surfaceHeight;
    let scaleX = surfaceWidth / config.width;
    let scaleY = surfaceHeight / config.height;

    switch (config.scaling) {
      case "stretch":
        break;
      case "fill": {
        const scale = Math.min(scaleX, scaleY);
        drawWidth = config.width * scale;
        drawHeight = config.height * scale;
        scaleX = scale;
        scaleY = scale;
        break;
      }
      default: {
        const scale = Math.max(1, Math.floor(Math.min(scaleX, scaleY)));
        drawWidth = config.width * scale;
        drawHeight = config.height * scale;
        scaleX = scale;
        scaleY = scale;
        break;
      }
    }

    return {
      surfaceWidth,
      surfaceHeight,
      drawWidth,
      drawHeight,
      left: Math.max(0, (surfaceWidth - drawWidth) / 2),
      top: Math.max(0, (surfaceHeight - drawHeight) / 2),
      scaleX,
      scaleY,
    };
  }

  function pixelSurfaceWindowToPixel(windowX, windowY, mount, layout, config) {
    const rect = mount && typeof mount.getBoundingClientRect === "function"
      ? mount.getBoundingClientRect()
      : { left: 0, top: 0 };
    const localX = Number(windowX) - Number(rect.left || 0) - layout.left;
    const localY = Number(windowY) - Number(rect.top || 0) - layout.top;
    const pixelX = Math.floor(localX / Math.max(0.0001, layout.scaleX));
    const pixelY = Math.floor(localY / Math.max(0.0001, layout.scaleY));
    return {
      x: pixelX,
      y: pixelY,
      inside: pixelX >= 0 && pixelX < config.width && pixelY >= 0 && pixelY < config.height,
    };
  }

  function createPixelSurfaceRuntime(entry, mount) {
    const config = resolvePixelSurfaceConfig(entry, mount);
    if (!config || !mount || entry.kind !== "surface") {
      return null;
    }

    const pixels = new Uint8ClampedArray(config.width * config.height * 4);
    const fallbackChildren = mount && mount.childNodes ? Array.from(mount.childNodes) : [];
    const initialPosition = mount && mount.style ? String(mount.style.position || "") : "";
    const initialOverflow = mount && mount.style ? String(mount.style.overflow || "") : "";
    const initialBackgroundColor = mount && mount.style ? String(mount.style.backgroundColor || "") : "";
    let canvas = null;
    let ctx2d = null;
    let imageData = null;
    let layout = null;
    let resizeObserver = null;
    let presentHandle = 0;
    let disposed = false;

    function restoreMountFallback() {
      if (!mount) {
        return;
      }
      if (canvas && canvas.parentNode === mount) {
        mount.removeChild(canvas);
      }
      mount.removeAttribute("data-gosx-pixel-surface-mounted");
      mount.style.position = initialPosition;
      mount.style.overflow = initialOverflow;
      mount.style.backgroundColor = initialBackgroundColor;
      if (mount.childNodes && mount.childNodes.length === 0) {
        for (const child of fallbackChildren) {
          if (child && child.parentNode !== mount) {
            mount.appendChild(child);
          }
        }
      }
    }

    function ensureCanvas() {
      if (disposed) {
        return null;
      }
      if (canvas && ctx2d) {
        return canvas;
      }

      const nextCanvas = document.createElement("canvas");
      nextCanvas.setAttribute("data-gosx-pixel-surface", "true");
      nextCanvas.setAttribute("width", String(config.width));
      nextCanvas.setAttribute("height", String(config.height));
      nextCanvas.width = config.width;
      nextCanvas.height = config.height;
      nextCanvas.style.position = "absolute";
      nextCanvas.style.maxWidth = "none";
      nextCanvas.style.maxHeight = "none";
      nextCanvas.style.imageRendering = config.scaling === "pixel-perfect" ? "pixelated" : "auto";

      const nextCtx2d = typeof nextCanvas.getContext === "function" ? nextCanvas.getContext("2d") : null;
      if (!nextCtx2d) {
        return null;
      }
      if ("imageSmoothingEnabled" in nextCtx2d) {
        nextCtx2d.imageSmoothingEnabled = config.scaling !== "pixel-perfect";
      }

      canvas = nextCanvas;
      ctx2d = nextCtx2d;
      clearChildren(mount);
      if (!mount.style.position) {
        mount.style.position = "relative";
      }
      mount.style.overflow = "hidden";
      mount.style.backgroundColor = pixelSurfaceBackgroundColor(config.clearColor);
      mount.setAttribute("data-gosx-pixel-surface-mounted", "true");
      mount.appendChild(canvas);
      applyLayout();
      if (!resizeObserver && typeof ResizeObserver === "function") {
        resizeObserver = new ResizeObserver(function() {
          applyLayout();
        });
        resizeObserver.observe(mount);
      }
      return canvas;
    }

    function applyLayout() {
      if (!canvas) {
        return null;
      }
      layout = pixelSurfaceLayout(config, mount);
      canvas.style.left = layout.left + "px";
      canvas.style.top = layout.top + "px";
      canvas.style.width = layout.drawWidth + "px";
      canvas.style.height = layout.drawHeight + "px";
      return layout;
    }

    function copyPixelsIntoImageData() {
      if (!ctx2d) {
        return null;
      }
      if ((!imageData || imageData.width !== config.width || imageData.height !== config.height) && typeof ctx2d.createImageData === "function") {
        imageData = ctx2d.createImageData(config.width, config.height);
      }
      if (imageData && imageData.data && typeof imageData.data.set === "function") {
        imageData.data.set(pixels);
        return imageData;
      }
      return {
        width: config.width,
        height: config.height,
        data: pixels,
      };
    }

    function drawNow() {
      presentHandle = 0;
      if (!ensureCanvas() || !ctx2d || typeof ctx2d.putImageData !== "function") {
        return;
      }
      const data = copyPixelsIntoImageData();
      if (!data) {
        return;
      }
      ctx2d.putImageData(data, 0, 0);
    }

    function present() {
      if (!ensureCanvas()) {
        return api;
      }
      if (!config.vsync) {
        drawNow();
        return api;
      }
      if (!presentHandle) {
        presentHandle = engineFrame(function() {
          drawNow();
        });
      }
      return api;
    }

    const api = {
      id: entry.id,
      width: config.width,
      height: config.height,
      stride: config.width * 4,
      scaling: config.scaling,
      clearColor: config.clearColor.slice(),
      vsync: config.vsync,
      pixels,
      get mount() {
        return mount;
      },
      get canvas() {
        ensureCanvas();
        return canvas;
      },
      get context() {
        ensureCanvas();
        return ctx2d;
      },
      clear() {
        for (let i = 0; i < pixels.length; i += 4) {
          pixels[i] = config.clearColor[0];
          pixels[i + 1] = config.clearColor[1];
          pixels[i + 2] = config.clearColor[2];
          pixels[i + 3] = config.clearColor[3];
        }
        return api;
      },
      layout() {
        ensureCanvas();
        return applyLayout();
      },
      present,
      toPixel(windowX, windowY) {
        ensureCanvas();
        const currentLayout = layout || applyLayout();
        if (!currentLayout) {
          return { x: 0, y: 0, inside: false };
        }
        return pixelSurfaceWindowToPixel(windowX, windowY, mount, currentLayout, config);
      },
      dispose() {
        disposed = true;
        if (presentHandle) {
          cancelEngineFrame(presentHandle);
          presentHandle = 0;
        }
        if (resizeObserver && typeof resizeObserver.disconnect === "function") {
          resizeObserver.disconnect();
        }
        resizeObserver = null;
        restoreMountFallback();
        canvas = null;
        ctx2d = null;
        imageData = null;
        layout = null;
      },
    };
    api.clear();
    return api;
  }

  function createEngineRuntime(entry, mount) {
    let programPromise = null;
    let pixelSurface = undefined;

    async function loadProgram() {
      if (!entry.programRef) {
        return null;
      }
      if (!programPromise) {
        const format = inferProgramFormat(entry);
        programPromise = fetchProgram(entry.programRef, format).then(function(data) {
          return data == null ? null : { data, format };
        });
      }
      return programPromise;
    }

    function frame() {
      if (pixelSurface === undefined) {
        pixelSurface = createPixelSurfaceRuntime(entry, mount);
      }
      return pixelSurface || null;
    }

    return {
      mode: entry.runtime || "",
      available() {
        return sharedEngineRuntimeAvailable(entry);
      },
      async hydrateFromProgramRef() {
        const program = await loadProgram();
        return hydrateSharedEngineProgram(entry, program);
      },
      tick() {
        return tickSharedEngineRuntime(entry);
      },
      renderFrame(timeSeconds, width, height) {
        return renderSharedEngineFrame(entry, timeSeconds, width, height);
      },
      frame,
      pixelSurface: frame,
      dispose() {
        const currentFrame = frame();
        if (currentFrame && typeof currentFrame.dispose === "function") {
          currentFrame.dispose();
        }
        disposeSharedEngineRuntime(entry);
      },
    };
  }

  function sharedEngineRuntimeBridge() {
    return {
      hydrate: window.__gosx_hydrate_engine,
      tick: window.__gosx_tick_engine,
      render: window.__gosx_render_engine,
      dispose: window.__gosx_engine_dispose,
    };
  }

  function sharedEngineRuntimeAvailable(entry) {
    const bridge = sharedEngineRuntimeBridge();
    return engineUsesSharedRuntime(entry)
      && typeof bridge.hydrate === "function"
      && typeof bridge.tick === "function"
      && typeof bridge.render === "function"
      && typeof bridge.dispose === "function";
  }

  function hydrateSharedEngineProgram(entry, program) {
    const bridge = sharedEngineRuntimeBridge();
    if (!engineUsesSharedRuntime(entry) || typeof bridge.hydrate !== "function" || !program) {
      return [];
    }
    return decodeEngineCommands(bridge.hydrate(
      entry.id,
      entry.component,
      JSON.stringify(entry.props || {}),
      program.data,
      program.format || "json",
    ));
  }

  function tickSharedEngineRuntime(entry) {
    const bridge = sharedEngineRuntimeBridge();
    if (!engineUsesSharedRuntime(entry) || typeof bridge.tick !== "function") {
      return [];
    }
    return decodeEngineCommands(bridge.tick(entry.id));
  }

  function renderSharedEngineFrame(entry, timeSeconds, width, height) {
    const bridge = sharedEngineRuntimeBridge();
    if (!engineUsesSharedRuntime(entry) || typeof bridge.render !== "function") {
      return null;
    }
    return decodeEngineRenderBundle(bridge.render(entry.id, timeSeconds, width, height));
  }

  function disposeSharedEngineRuntime(entry) {
    const bridge = sharedEngineRuntimeBridge();
    if (!engineUsesSharedRuntime(entry) || typeof bridge.dispose !== "function") {
      return;
    }
    bridge.dispose(entry.id);
  }

  function decodeEngineCommands(result) {
    if (result == null) {
      return [];
    }
    if (typeof result !== "string") {
      return [];
    }
    if (result === "" || result === "[]") {
      return [];
    }
    if (result.startsWith("error:") || result.startsWith("marshal:")) {
      console.error("[gosx] engine runtime error:", result);
      return [];
    }
    try {
      const commands = JSON.parse(result);
      return Array.isArray(commands) ? commands : [];
    } catch (e) {
      console.error("[gosx] failed to decode engine commands:", e);
      return [];
    }
  }

  function decodeEngineRenderBundle(result) {
    if (result == null || typeof result !== "string" || result === "") {
      return null;
    }
    if (result.startsWith("error:") || result.startsWith("marshal:")) {
      console.error("[gosx] engine runtime error:", result);
      return null;
    }
    try {
      const bundle = JSON.parse(result);
      return normalizeEngineRenderBundle(bundle);
    } catch (e) {
      console.error("[gosx] failed to decode engine render bundle:", e);
      return null;
    }
  }

  function normalizeEngineRenderBundle(bundle) {
    if (!bundle || typeof bundle !== "object") {
      return null;
    }
    bundle.camera = sceneRenderCamera(bundle.camera);
    bundle.labels = Array.isArray(bundle.labels) ? bundle.labels.map(function(label, index) {
      const item = label && typeof label === "object" ? label : {};
      return {
        id: item.id || ("scene-label-" + index),
        text: typeof item.text === "string" ? item.text : "",
        className: sceneLabelClassName(item),
        position: {
          x: sceneNumber(item.position && item.position.x, 0),
          y: sceneNumber(item.position && item.position.y, 0),
        },
      depth: sceneNumber(item.depth, 0),
      priority: sceneNumber(item.priority, 0),
      maxWidth: Math.max(48, sceneNumber(item.maxWidth, 180)),
      maxLines: Math.max(0, Math.floor(sceneNumber(item.maxLines, 0))),
      overflow: normalizeTextLayoutOverflow(item.overflow),
      font: typeof item.font === "string" && item.font ? item.font : '600 13px "IBM Plex Sans", "Segoe UI", sans-serif',
        lineHeight: Math.max(12, sceneNumber(item.lineHeight, 18)),
        color: typeof item.color === "string" && item.color ? item.color : "#ecf7ff",
        background: typeof item.background === "string" && item.background ? item.background : "rgba(8, 21, 31, 0.82)",
        borderColor: typeof item.borderColor === "string" && item.borderColor ? item.borderColor : "rgba(141, 225, 255, 0.24)",
        offsetX: sceneNumber(item.offsetX, 0),
        offsetY: sceneNumber(item.offsetY, -14),
        anchorX: Math.max(0, Math.min(1, sceneNumber(item.anchorX, 0.5))),
        anchorY: Math.max(0, Math.min(1, sceneNumber(item.anchorY, 1))),
        collision: normalizeSceneLabelCollision(item.collision),
        occlude: sceneBool(item.occlude, false),
        whiteSpace: normalizeSceneLabelWhiteSpace(item.whiteSpace),
        textAlign: normalizeSceneLabelAlign(item.textAlign),
      };
    }).filter(function(label) {
      return label.text.trim() !== "";
    }) : [];
    bundle.positions = sceneFloatArray(bundle.positions);
    bundle.colors = sceneFloatArray(bundle.colors);
    bundle.worldPositions = sceneFloatArray(bundle.worldPositions);
    bundle.worldColors = sceneFloatArray(bundle.worldColors);
    return bundle;
  }

  function sceneFloatArray(values) {
    if (values instanceof Float32Array) {
      return values;
    }
    if (Array.isArray(values)) {
      return new Float32Array(values);
    }
    return new Float32Array(0);
  }

  function engineKindNeedsMount(kind) {
    return kind === "surface" || kind === "video";
  }

  function engineKindUsesBuiltinFactory(kind) {
    return kind === "video";
  }

  function videoPropValue(props, names, fallback) {
    const source = props && typeof props === "object" ? props : {};
    const list = Array.isArray(names) ? names : [names];
    for (const name of list) {
      if (!name) {
        continue;
      }
      if (Object.prototype.hasOwnProperty.call(source, name) && source[name] != null) {
        return source[name];
      }
    }
    return fallback;
  }

  function videoSignalName(name) {
    return "$video." + name;
  }

  function videoRuntimeAssets() {
    if (window.__gosx && window.__gosx.document && typeof window.__gosx.document.get === "function") {
      const documentState = window.__gosx.document.get();
      if (documentState && documentState.assets && documentState.assets.runtime) {
        return documentState.assets.runtime;
      }
    }
    return {};
  }

  function readVideoSignal(name, fallback) {
    const value = gosxReadSharedSignal(videoSignalName(name), fallback);
    return value == null ? fallback : value;
  }

  function writeVideoSignal(name, value) {
    const signalName = videoSignalName(name);
    const payload = JSON.stringify(value == null ? null : value);
    const setSharedSignal = window.__gosx_set_shared_signal;
    if (typeof setSharedSignal === "function") {
      try {
        const result = setSharedSignal(signalName, payload);
        if (typeof result === "string" && result !== "") {
          console.error("[gosx] shared signal update error (" + signalName + "):", result);
          gosxNotifySharedSignal(signalName, payload);
        }
        return;
      } catch (error) {
        console.error("[gosx] shared signal update error (" + signalName + "):", error);
      }
    }
    gosxNotifySharedSignal(signalName, payload);
  }

  function subscribeVideoSignal(name, listener) {
    return gosxSubscribeSharedSignal(videoSignalName(name), function(value) {
      listener(value);
    }, { immediate: true });
  }

  function videoClearChildren(node) {
    if (!node) {
      return;
    }
    while (node.firstChild) {
      node.removeChild(node.firstChild);
    }
  }

  function videoRestoreChildren(node, children) {
    if (!node) {
      return;
    }
    videoClearChildren(node);
    for (const child of children || []) {
      if (child) {
        node.appendChild(child);
      }
    }
  }

  function videoNeedsHLS(source) {
    return /\.m3u8(?:$|[?#])/i.test(String(source || "").trim());
  }

  function videoSupportsNativeHLS(video) {
    if (!video || typeof video.canPlayType !== "function") {
      return false;
    }
    const result = String(video.canPlayType("application/vnd.apple.mpegurl") || "");
    return result !== "" && result !== "no";
  }

  async function ensureVideoHLSLibrary() {
    if (typeof window.Hls === "function") {
      return window.Hls;
    }
    const runtimeAssets = videoRuntimeAssets();
    const path = String(runtimeAssets.hlsPath || "/gosx/hls.min.js").trim();
    if (!path) {
      return null;
    }
    await loadEngineScript(path);
    return typeof window.Hls === "function" ? window.Hls : null;
  }

  function videoBufferedAhead(video) {
    if (!video || !video.buffered || typeof video.buffered.length !== "number" || typeof video.buffered.end !== "function") {
      return 0;
    }
    const current = Math.max(0, sceneNumber(video.currentTime, 0));
    for (let i = 0; i < video.buffered.length; i += 1) {
      const end = sceneNumber(video.buffered.end(i), current);
      const start = typeof video.buffered.start === "function" ? sceneNumber(video.buffered.start(i), 0) : 0;
      if (current >= start && current <= end + 0.1) {
        return Math.max(0, end - current);
      }
    }
    return 0;
  }

  function videoViewportSize(mount) {
    const rect = mount && typeof mount.getBoundingClientRect === "function"
      ? mount.getBoundingClientRect()
      : { width: 0, height: 0 };
    return [
      Math.max(0, Math.round(sceneNumber(rect && rect.width, sceneNumber(mount && mount.width, 0)))),
      Math.max(0, Math.round(sceneNumber(rect && rect.height, sceneNumber(mount && mount.height, 0)))),
    ];
  }

  function videoNormalizeSourceInfo(item, index) {
    const source = item && typeof item === "object" ? item : {};
    const src = String(videoPropValue(source, ["src", "source", "url"], "") || "").trim();
    if (!src) {
      return null;
    }
    const type = String(videoPropValue(source, ["type"], "") || "").trim();
    const media = String(videoPropValue(source, ["media"], "") || "").trim();
    const id = String(videoPropValue(source, ["id", "name"], src || ("source-" + index)) || "").trim();
    return {
      id: id || ("source-" + index),
      src: src,
      type: type,
      media: media,
    };
  }

  function videoSourcesFromProps(props) {
    const sources = videoPropValue(props, ["sources"], []);
    if (!Array.isArray(sources)) {
      return [];
    }
    const out = [];
    for (let index = 0; index < sources.length; index += 1) {
      const source = videoNormalizeSourceInfo(sources[index], index);
      if (source) {
        out.push(source);
      }
    }
    return out;
  }

  function videoNormalizeTrackInfo(item, index) {
    const source = item && typeof item === "object" ? item : {};
    const src = String(videoPropValue(source, ["src"], "") || "").trim();
    const srcLang = String(videoPropValue(source, ["srclang", "srcLang"], "") || "").trim();
    const language = String(videoPropValue(source, ["language", "lang", "srclang", "srcLang"], srcLang) || "").trim();
    const title = String(videoPropValue(source, ["title", "label", "name"], language || ("Track " + (index + 1))) || "").trim();
    const id = String(videoPropValue(source, ["id", "trackID", "trackId"], language || title || ("track-" + index)) || "").trim();
    const kind = String(videoPropValue(source, ["kind"], "subtitles") || "subtitles").trim().toLowerCase() || "subtitles";
    return {
      id: id || ("track-" + index),
      language: language,
      srclang: srcLang || language,
      title: title,
      kind: kind,
      src: src,
      default: sceneBool(videoPropValue(source, ["default"], false), false),
      forced: sceneBool(videoPropValue(source, ["forced"], false), false),
    };
  }

  function videoTracksFromProps(props) {
    const tracks = videoPropValue(props, ["subtitleTracks", "subtitle_tracks"], []);
    if (!Array.isArray(tracks)) {
      return [];
    }
    return tracks.map(videoNormalizeTrackInfo);
  }

  function videoTrackURL(track, props) {
    const explicit = String(videoPropValue(track, ["src"], "") || "").trim();
    if (explicit) {
      return explicit;
    }
    const subtitleBase = String(videoPropValue(props, ["subtitleBase", "subtitle_base"], "") || "").trim();
    const id = String(track && track.id || "").trim();
    if (!subtitleBase || !id) {
      return "";
    }
    return subtitleBase.replace(/\/$/, "") + "/" + encodeURIComponent(id) + ".vtt";
  }

  function videoFirstFallbackNode(children, tagName) {
    for (const child of children || []) {
      if (child && child.nodeType === 1 && child.tagName === tagName) {
        return child;
      }
    }
    return null;
  }

  function videoCanUseSourceNatively(video, source) {
    if (!source || typeof source !== "object") {
      return false;
    }
    const src = String(source.src || "").trim();
    if (!src) {
      return false;
    }
    if (videoNeedsHLS(src)) {
      return videoSupportsNativeHLS(video);
    }
    const type = String(source.type || "").trim();
    if (!type || !video || typeof video.canPlayType !== "function") {
      return true;
    }
    const result = String(video.canPlayType(type) || "").trim().toLowerCase();
    return result !== "" && result !== "no";
  }

  function videoEnsureAuthoredChildren(video, props) {
    if (!video || !props || typeof props !== "object") {
      return;
    }
    let hasSourceChildren = false;
    let hasTrackChildren = false;
    for (const child of Array.from(video.childNodes || [])) {
      if (!child || child.nodeType !== 1) {
        continue;
      }
      if (child.tagName === "SOURCE") {
        hasSourceChildren = true;
      } else if (child.tagName === "TRACK") {
        hasTrackChildren = true;
      }
    }
    if (!hasSourceChildren) {
      for (const source of videoSourcesFromProps(props)) {
        const sourceNode = document.createElement("source");
        sourceNode.setAttribute("src", source.src);
        if (source.type) {
          sourceNode.setAttribute("type", source.type);
        }
        if (source.media) {
          sourceNode.setAttribute("media", source.media);
        }
        video.appendChild(sourceNode);
      }
    }
    if (!hasTrackChildren) {
      for (const track of videoTracksFromProps(props)) {
        const trackURL = videoTrackURL(track, props);
        if (!trackURL) {
          continue;
        }
        const trackNode = document.createElement("track");
        trackNode.setAttribute("src", trackURL);
        trackNode.setAttribute("kind", track.kind || "subtitles");
        if (track.srclang) {
          trackNode.setAttribute("srclang", track.srclang);
        }
        if (track.title) {
          trackNode.setAttribute("label", track.title);
        }
        if (track.default) {
          trackNode.setAttribute("default", "true");
        }
        video.appendChild(trackNode);
      }
    }
  }

  function videoSanitizeCueHTML(text) {
    const escaped = String(text == null ? "" : text)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
    return escaped
      .replace(/&lt;(\/?)(b|i|u|s)&gt;/gi, "<$1$2>");
  }

  function videoParseTimestamp(value) {
    const text = String(value || "").trim();
    if (!text) {
      return -1;
    }
    const parts = text.split(":");
    if (parts.length < 2 || parts.length > 3) {
      return -1;
    }
    let hours = 0;
    let minutes = 0;
    let seconds = 0;
    if (parts.length === 3) {
      hours = Number(parts[0]);
      minutes = Number(parts[1]);
      seconds = Number(parts[2].replace(",", "."));
    } else {
      minutes = Number(parts[0]);
      seconds = Number(parts[1].replace(",", "."));
    }
    if (!Number.isFinite(hours) || !Number.isFinite(minutes) || !Number.isFinite(seconds)) {
      return -1;
    }
    return Math.round(((hours * 3600) + (minutes * 60) + seconds) * 1000);
  }

  function parseVideoVTT(text) {
    const raw = String(text == null ? "" : text).replace(/\r/g, "");
    const lines = raw.split("\n");
    const cues = [];
    let index = 0;

    while (index < lines.length) {
      let line = String(lines[index] || "").trim();
      if (!line) {
        index += 1;
        continue;
      }
      if (/^WEBVTT/i.test(line)) {
        index += 1;
        continue;
      }
      if (/^NOTE\b/i.test(line)) {
        index += 1;
        while (index < lines.length && String(lines[index] || "").trim() !== "") {
          index += 1;
        }
        continue;
      }
      if (!line.includes("-->")) {
        index += 1;
        line = String(lines[index] || "").trim();
      }
      if (!line.includes("-->")) {
        index += 1;
        continue;
      }
      const timing = line.split("-->");
      const startMS = videoParseTimestamp(timing[0]);
      const endBits = String(timing[1] || "").trim().split(/\s+/);
      const endMS = videoParseTimestamp(endBits[0]);
      index += 1;
      const textLines = [];
      while (index < lines.length && String(lines[index] || "").trim() !== "") {
        textLines.push(String(lines[index] || ""));
        index += 1;
      }
      if (startMS < 0 || endMS <= startMS) {
        continue;
      }
      cues.push({
        startMS,
        endMS,
        text: videoSanitizeCueHTML(textLines.join("\n")),
      });
    }

    cues.sort(function(a, b) {
      if (a.startMS !== b.startMS) {
        return a.startMS - b.startMS;
      }
      return a.endMS - b.endMS;
    });
    return cues;
  }

  function videoActiveCues(cues, currentTimeSeconds) {
    const currentMS = Math.max(0, Math.round(sceneNumber(currentTimeSeconds, 0) * 1000));
    const active = [];
    for (const cue of cues || []) {
      if (cue.startMS <= currentMS && currentMS < cue.endMS) {
        active.push({ text: cue.text });
      }
      if (cue.startMS > currentMS) {
        break;
      }
    }
    return active;
  }

  function videoApplyElementProps(video, props) {
    if (!video || !props || typeof props !== "object") {
      return;
    }
    const stringAttrs = [
      ["poster", "poster"],
      ["preload", "preload"],
      ["crossorigin", "crossOrigin"],
      ["crossorigin", "crossorigin"],
    ];
    for (const entry of stringAttrs) {
      const value = videoPropValue(props, [entry[1], entry[0]], "");
      if (value == null || value === "") {
        continue;
      }
      video.setAttribute(entry[0], String(value));
    }
    const boolAttrs = [
      ["autoplay", ["autoplay", "autoPlay"]],
      ["controls", ["controls"]],
      ["loop", ["loop"]],
      ["muted", ["muted"]],
      ["playsinline", ["playsinline", "playsInline"]],
    ];
    for (const entry of boolAttrs) {
      const enabled = sceneBool(videoPropValue(props, entry[1], false), false);
      if (enabled) {
        video.setAttribute(entry[0], "true");
      } else if (typeof video.removeAttribute === "function") {
        video.removeAttribute(entry[0]);
      }
    }
    if (sceneBool(videoPropValue(props, ["muted"], false), false)) {
      video.muted = true;
    }
    const width = Math.max(0, Math.round(sceneNumber(videoPropValue(props, ["width"], 0), 0)));
    const height = Math.max(0, Math.round(sceneNumber(videoPropValue(props, ["height"], 0), 0)));
    if (width > 0) {
      video.setAttribute("width", String(width));
      video.width = width;
    }
    if (height > 0) {
      video.setAttribute("height", String(height));
      video.height = height;
    }
  }

  function videoSyncURL(path) {
    const source = String(path || "").trim();
    if (!source) {
      return "";
    }
    return isAbsoluteHubURL(source) ? source : hubURL(source);
  }

  async function createBuiltInVideoEngine(ctx) {
    const mount = ctx && ctx.mount;
    if (!mount) {
      return {};
    }

    const props = ctx && ctx.props && typeof ctx.props === "object" ? ctx.props : {};
    const fallbackChildren = mount && mount.childNodes ? Array.from(mount.childNodes) : [];
    const authoredSources = videoSourcesFromProps(props);
    const video = videoFirstFallbackNode(fallbackChildren, "VIDEO") || document.createElement("video");
    const unsubscribers = [];
    const eventListeners = [];
    const subtitleState = {
      tracks: videoTracksFromProps(props),
      loadedID: "",
      activeID: "",
      cues: [],
      lastSignature: "",
      status: "idle",
    };
    let disposed = false;
    let hls = null;
    let syncSocket = null;
    let reconnectTimer = 0;
    let followTimer = 0;
    let lastLeadSendAt = 0;
    let followState = null;
    let requestedRate = Math.max(0.1, sceneNumber(readVideoSignal("rate", videoPropValue(props, ["rate"], 1)), 1));
    let lastError = "";
    let stalled = false;
    let resizeObserver = null;
    let currentSource = "";

    function setError(message) {
      lastError = String(message || "").trim();
      writeVideoSignal("error", lastError);
    }

    function clearError() {
      if (!lastError) {
        return;
      }
      lastError = "";
      writeVideoSignal("error", "");
    }

    function updateSubtitleOutputs() {
      writeVideoSignal("subtitleTracks", subtitleState.tracks.slice());
      writeVideoSignal("subtitleStatus", subtitleState.status);
    }

    function updateCueOutputs() {
      const next = videoActiveCues(subtitleState.cues, sceneNumber(video.currentTime, 0));
      const signature = JSON.stringify(next);
      if (signature === subtitleState.lastSignature) {
        return;
      }
      subtitleState.lastSignature = signature;
      writeVideoSignal("activeCues", next);
    }

    function updateVideoOutputs() {
      const duration = Math.max(0, sceneNumber(video.duration, 0));
      const viewport = videoViewportSize(mount);
      const playing = !sceneBool(video.paused, true) && !sceneBool(video.ended, false);
      writeVideoSignal("position", Math.max(0, sceneNumber(video.currentTime, 0)));
      writeVideoSignal("duration", duration);
      writeVideoSignal("playing", playing);
      writeVideoSignal("buffered", videoBufferedAhead(video));
      writeVideoSignal("stalled", stalled);
      writeVideoSignal("fullscreen", Boolean(document && document.fullscreenElement && (document.fullscreenElement === mount || document.fullscreenElement === video)));
      writeVideoSignal("viewport", viewport);
      writeVideoSignal("ready", sceneNumber(video.readyState, 0) >= 2);
      writeVideoSignal("muted", Boolean(video.muted));
      writeVideoSignal("actualRate", sceneNumber(video.playbackRate, requestedRate));
      writeVideoSignal("syncConnected", Boolean(syncSocket && syncSocket.readyState === 1));
      updateSubtitleOutputs();
      updateCueOutputs();
      if (!lastError) {
        writeVideoSignal("error", "");
      }
    }

    function addListener(target, type, listener) {
      if (!target || typeof target.addEventListener !== "function") {
        return;
      }
      target.addEventListener(type, listener);
      eventListeners.push({ target, type, listener });
    }

    function teardownHLS() {
      if (hls && typeof hls.destroy === "function") {
        hls.destroy();
      }
      hls = null;
    }

    function projectedFollowPosition(state) {
      if (!state) {
        return 0;
      }
      let position = Math.max(0, sceneNumber(state.position, 0));
      const playing = sceneBool(state.playing, false);
      if (playing) {
        const sentAtMS = sceneNumber(state.sentAtMS, 0);
        const rate = Math.max(0.1, sceneNumber(state.rate, 1));
        if (sentAtMS > 0) {
          position += Math.max(0, (Date.now() - sentAtMS) / 1000) * rate;
        }
      }
      return position;
    }

    function applyRequestedRate() {
      const mode = String(videoPropValue(props, ["syncMode", "sync_mode"], "follow") || "follow").trim().toLowerCase();
      if (mode === "follow" && followState) {
        return;
      }
      video.playbackRate = requestedRate;
      updateVideoOutputs();
    }

    function safePlay() {
      try {
        const result = video.play();
        if (result && typeof result.then === "function") {
          return result.catch(function(error) {
            setError(error && error.message ? error.message : "playback failed");
            updateVideoOutputs();
          });
        }
        clearError();
      } catch (error) {
        setError(error && error.message ? error.message : "playback failed");
      }
      updateVideoOutputs();
      return Promise.resolve();
    }

    function applyFollowState() {
      if (!followState || disposed) {
        return;
      }
      const strategy = String(videoPropValue(props, ["syncStrategy", "sync_strategy"], "nudge") || "nudge").trim().toLowerCase();
      const playing = sceneBool(followState.playing, false);
      const target = projectedFollowPosition(followState);
      const drift = Math.max(-9999, Math.min(9999, sceneNumber(video.currentTime, 0) - target, 0));
      if (playing) {
        safePlay();
      } else {
        video.pause();
      }
      if (strategy === "snap") {
        if (Math.abs(drift) > 1) {
          video.currentTime = Math.max(0, target);
        }
        video.playbackRate = requestedRate;
      } else if (Math.abs(drift) > 5) {
        video.currentTime = Math.max(0, target);
        video.playbackRate = requestedRate;
      } else if (drift > 0.5) {
        video.playbackRate = 0.92;
      } else if (drift < -0.5) {
        video.playbackRate = 1.08;
      } else {
        video.playbackRate = requestedRate;
      }
      updateVideoOutputs();
    }

    function clearFollowTimer() {
      if (followTimer) {
        clearInterval(followTimer);
        followTimer = 0;
      }
    }

    function ensureFollowTimer() {
      if (followTimer || String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase() !== "follow") {
        return;
      }
      followTimer = setInterval(applyFollowState, 500);
    }

    function sendLeadSnapshot(force) {
      if (!syncSocket || syncSocket.readyState !== 1) {
        return;
      }
      const mode = String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase();
      if (mode !== "lead") {
        return;
      }
      const now = Date.now();
      if (!force && now-lastLeadSendAt < 250) {
        return;
      }
      lastLeadSendAt = now;
      try {
        syncSocket.send(JSON.stringify({
          type: "sync",
          mediaID: currentSource,
          position: Math.max(0, sceneNumber(video.currentTime, 0)),
          playing: !sceneBool(video.paused, true) && !sceneBool(video.ended, false),
          rate: sceneNumber(video.playbackRate, requestedRate),
          sentAtMS: now,
        }));
      } catch (_error) {
      }
    }

    function clearReconnectTimer() {
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = 0;
      }
    }

    function closeSyncSocket() {
      clearReconnectTimer();
      clearFollowTimer();
      if (syncSocket && typeof syncSocket.close === "function") {
        syncSocket.close();
      }
      syncSocket = null;
      writeVideoSignal("syncConnected", false);
    }

    function connectSync(attempt) {
      const rawURL = videoSyncURL(videoPropValue(props, ["sync"], ""));
      if (!rawURL || typeof WebSocket !== "function" || disposed) {
        writeVideoSignal("syncConnected", false);
        return;
      }
      const retryAttempt = Math.max(0, attempt || 0);
      const socket = new WebSocket(rawURL);
      syncSocket = socket;
      socket.onopen = function() {
        writeVideoSignal("syncConnected", true);
        updateVideoOutputs();
        if (String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase() === "lead") {
          sendLeadSnapshot(true);
        } else {
          ensureFollowTimer();
        }
      };
      socket.onclose = function() {
        writeVideoSignal("syncConnected", false);
        updateVideoOutputs();
        if (disposed) {
          return;
        }
        const delay = Math.min(30000, Math.max(1000, 1000 * Math.pow(2, retryAttempt)));
        reconnectTimer = setTimeout(function() {
          connectSync(retryAttempt + 1);
        }, delay);
      };
      socket.onerror = function() {
        writeVideoSignal("syncConnected", false);
      };
      socket.onmessage = function(event) {
        let message = null;
        try {
          message = JSON.parse(String(event && event.data || ""));
        } catch (_error) {
          return;
        }
        if (!message || message.type !== "sync") {
          return;
        }
        if (message.mediaID && currentSource && String(message.mediaID) !== String(currentSource)) {
          return;
        }
        followState = message;
        if (String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase() === "follow") {
          ensureFollowTimer();
          applyFollowState();
        }
      };
    }

    async function loadSubtitleTrack(trackID) {
      const selected = String(trackID || "").trim();
      subtitleState.activeID = selected;
      subtitleState.cues = [];
      subtitleState.lastSignature = "";
      if (!selected) {
        subtitleState.loadedID = "";
        subtitleState.status = subtitleState.tracks.length > 0 ? "ready" : "idle";
        updateSubtitleOutputs();
        updateCueOutputs();
        return;
      }
      const localTrack = subtitleState.tracks.find(function(track) {
        return track.id === selected;
      });
      if (!localTrack) {
        subtitleState.status = "error";
        updateSubtitleOutputs();
        return;
      }
      if (hls && Array.isArray(hls.subtitleTracks) && Object.prototype.hasOwnProperty.call(hls, "subtitleTrack")) {
        const nextIndex = hls.subtitleTracks.findIndex(function(track) {
          return videoNormalizeTrackInfo(track, 0).id === selected;
        });
        if (nextIndex >= 0) {
          hls.subtitleTrack = nextIndex;
        }
      }
      const subtitleURL = videoTrackURL(localTrack, props);
      if (!subtitleURL) {
        subtitleState.status = "ready";
        updateSubtitleOutputs();
        updateCueOutputs();
        return;
      }
      subtitleState.status = "loading";
      updateSubtitleOutputs();
      for (let attempt = 0; attempt < 60; attempt += 1) {
        const response = await fetch(subtitleURL);
        if (response.status === 202) {
          subtitleState.status = "warming";
          updateSubtitleOutputs();
          await new Promise(function(resolve) {
            setTimeout(resolve, 1500);
          });
          continue;
        }
        if (!response.ok) {
          subtitleState.status = "error";
          setError("subtitle fetch failed");
          updateSubtitleOutputs();
          return;
        }
        const text = await response.text();
        subtitleState.cues = parseVideoVTT(text);
        subtitleState.loadedID = selected;
        subtitleState.status = "ready";
        clearError();
        updateSubtitleOutputs();
        updateCueOutputs();
        return;
      }
      subtitleState.status = "error";
      setError("subtitle warmup timed out");
      updateSubtitleOutputs();
    }

    async function applySource(source) {
      const requestedSource = String(source || "").trim();
      let nextSource = requestedSource;
      let useAuthoredSources = false;
      if (!nextSource && authoredSources.length > 0) {
        let nativeCandidate = null;
        for (const candidate of authoredSources) {
          if (videoCanUseSourceNatively(video, candidate)) {
            nativeCandidate = candidate;
            break;
          }
        }
        if (nativeCandidate) {
          nextSource = nativeCandidate.src;
          useAuthoredSources = true;
        } else {
          const hlsCandidate = authoredSources.find(function(candidate) {
            return videoNeedsHLS(candidate && candidate.src);
          });
          if (hlsCandidate) {
            nextSource = hlsCandidate.src;
          } else {
            nextSource = authoredSources[0].src;
            useAuthoredSources = true;
          }
        }
      }
      currentSource = nextSource;
      clearError();
      followState = null;
      clearFollowTimer();
      teardownHLS();
      subtitleState.cues = [];
      subtitleState.lastSignature = "";
      updateCueOutputs();
      if (!nextSource && !useAuthoredSources) {
        if (typeof video.removeAttribute === "function") {
          video.removeAttribute("src");
        }
        try {
          video.src = "";
        } catch (_error) {
        }
        if (typeof video.load === "function") {
          video.load();
        }
        updateVideoOutputs();
        return;
      }
      if (useAuthoredSources) {
        if (typeof video.removeAttribute === "function") {
          video.removeAttribute("src");
        }
        try {
          video.src = "";
        } catch (_error) {
        }
        if (typeof video.load === "function") {
          video.load();
        }
      } else if (videoNeedsHLS(nextSource) && !videoSupportsNativeHLS(video)) {
        const HlsCtor = await ensureVideoHLSLibrary();
        if (!HlsCtor) {
          setError("HLS.js unavailable");
          updateVideoOutputs();
          return;
        }
        const supported = typeof HlsCtor.isSupported === "function" ? HlsCtor.isSupported() : true;
        if (!supported) {
          setError("HLS playback unsupported");
          updateVideoOutputs();
          return;
        }
        hls = new HlsCtor(videoPropValue(props, ["hls", "hlsConfig"], {}));
        if (hls && typeof hls.attachMedia === "function") {
          hls.attachMedia(video);
        }
        if (hls && typeof hls.loadSource === "function") {
          hls.loadSource(nextSource);
        }
        if (hls && typeof hls.on === "function" && HlsCtor.Events) {
          if (HlsCtor.Events.MANIFEST_PARSED) {
            hls.on(HlsCtor.Events.MANIFEST_PARSED, function() {
              if (Array.isArray(hls.subtitleTracks) && hls.subtitleTracks.length > 0) {
                subtitleState.tracks = hls.subtitleTracks.map(videoNormalizeTrackInfo);
                updateSubtitleOutputs();
              }
              clearError();
              updateVideoOutputs();
            });
          }
          if (HlsCtor.Events.SUBTITLE_TRACKS_UPDATED) {
            hls.on(HlsCtor.Events.SUBTITLE_TRACKS_UPDATED, function(_event, data) {
              const tracks = data && Array.isArray(data.subtitleTracks) ? data.subtitleTracks : (Array.isArray(hls.subtitleTracks) ? hls.subtitleTracks : []);
              subtitleState.tracks = tracks.map(videoNormalizeTrackInfo);
              updateSubtitleOutputs();
            });
          }
          if (HlsCtor.Events.ERROR) {
            hls.on(HlsCtor.Events.ERROR, function(_event, data) {
              if (data && data.fatal) {
                setError(data && data.details ? data.details : "video transport failed");
                updateVideoOutputs();
              }
            });
          }
        }
      } else {
        video.src = nextSource;
        if (typeof video.load === "function") {
          video.load();
        }
      }
      updateVideoOutputs();
      const activeSubtitleTrack = readVideoSignal("subtitleTrack", videoPropValue(props, ["subtitleTrack", "subtitle_track"], ""));
      await loadSubtitleTrack(activeSubtitleTrack);
      if (String(videoPropValue(props, ["sync"], "")).trim() !== "") {
        closeSyncSocket();
        connectSync(0);
      }
    }

    video.setAttribute("data-gosx-video", "true");
    videoApplyElementProps(video, props);
    videoEnsureAuthoredChildren(video, props);
    videoClearChildren(mount);
    mount.appendChild(video);
    subtitleState.status = subtitleState.tracks.length > 0 ? "ready" : "idle";
    writeVideoSignal("subtitleTracks", subtitleState.tracks.slice());
    writeVideoSignal("subtitleStatus", subtitleState.status);
    writeVideoSignal("activeCues", []);
    writeVideoSignal("syncConnected", false);
    writeVideoSignal("error", "");

    addListener(video, "timeupdate", function() {
      updateVideoOutputs();
      sendLeadSnapshot(false);
    });
    addListener(video, "durationchange", updateVideoOutputs);
    addListener(video, "loadedmetadata", updateVideoOutputs);
    addListener(video, "canplay", function() {
      stalled = false;
      clearError();
      updateVideoOutputs();
    });
    addListener(video, "play", function() {
      stalled = false;
      clearError();
      updateVideoOutputs();
      sendLeadSnapshot(true);
    });
    addListener(video, "pause", function() {
      stalled = false;
      updateVideoOutputs();
      sendLeadSnapshot(true);
    });
    addListener(video, "seeked", function() {
      updateVideoOutputs();
      sendLeadSnapshot(true);
    });
    addListener(video, "waiting", function() {
      stalled = true;
      updateVideoOutputs();
    });
    addListener(video, "stalled", function() {
      stalled = true;
      updateVideoOutputs();
    });
    addListener(video, "volumechange", function() {
      updateVideoOutputs();
    });
    addListener(video, "ratechange", function() {
      updateVideoOutputs();
      sendLeadSnapshot(true);
    });
    addListener(video, "error", function() {
      const mediaError = video && video.error && video.error.message ? video.error.message : "video playback failed";
      setError(mediaError);
      updateVideoOutputs();
    });
    addListener(document, "fullscreenchange", updateVideoOutputs);

    if (typeof ResizeObserver === "function") {
      resizeObserver = new ResizeObserver(function() {
        updateVideoOutputs();
      });
      resizeObserver.observe(mount);
    }

    unsubscribers.push(subscribeVideoSignal("src", function(value) {
      applySource(value);
    }));
    unsubscribers.push(subscribeVideoSignal("seek", function(value) {
      const mode = String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase();
      if (mode === "follow" && String(videoPropValue(props, ["sync"], "")).trim() !== "") {
        return;
      }
      const nextTime = sceneNumber(value, -1);
      if (nextTime >= 0) {
        video.currentTime = nextTime;
        updateVideoOutputs();
      }
    }));
    unsubscribers.push(subscribeVideoSignal("command", function(value) {
      const mode = String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase();
      if (mode === "follow" && String(videoPropValue(props, ["sync"], "")).trim() !== "") {
        return;
      }
      const command = String(value || "").trim().toLowerCase();
      if (!command) {
        return;
      }
      if (command === "play") {
        safePlay();
      } else if (command === "pause") {
        video.pause();
      } else if (command === "toggle") {
        if (sceneBool(video.paused, true)) {
          safePlay();
        } else {
          video.pause();
        }
      } else if ((command === "enter-fullscreen" || command === "toggle-fullscreen") && mount && typeof mount.requestFullscreen === "function") {
        mount.requestFullscreen();
      } else if (command === "exit-fullscreen" && document && typeof document.exitFullscreen === "function") {
        document.exitFullscreen();
      }
      updateVideoOutputs();
    }));
    unsubscribers.push(subscribeVideoSignal("volume", function(value) {
      const volume = Math.max(0, Math.min(1, sceneNumber(value, sceneNumber(video.volume, 1))));
      video.volume = volume;
      updateVideoOutputs();
    }));
    unsubscribers.push(subscribeVideoSignal("mute", function(value) {
      video.muted = sceneBool(value, Boolean(video.muted));
      updateVideoOutputs();
    }));
    unsubscribers.push(subscribeVideoSignal("rate", function(value) {
      requestedRate = Math.max(0.1, sceneNumber(value, requestedRate));
      const mode = String(videoPropValue(props, ["syncMode", "sync_mode"], "follow")).trim().toLowerCase();
      if (!(mode === "follow" && String(videoPropValue(props, ["sync"], "")).trim() !== "")) {
        applyRequestedRate();
      }
    }));
    unsubscribers.push(subscribeVideoSignal("subtitleTrack", function(value) {
      loadSubtitleTrack(value);
    }));

    const initialVolume = Math.max(0, Math.min(1, sceneNumber(readVideoSignal("volume", videoPropValue(props, ["volume"], 1)), 1)));
    video.volume = initialVolume;
    video.muted = sceneBool(readVideoSignal("mute", videoPropValue(props, ["muted"], false)), false);
    video.playbackRate = requestedRate;
    updateVideoOutputs();

    const initialSource = readVideoSignal("src", videoPropValue(props, ["src", "Src"], ""));
    await applySource(initialSource);

    return {
      video,
      dispose() {
        disposed = true;
        closeSyncSocket();
        teardownHLS();
        if (resizeObserver && typeof resizeObserver.disconnect === "function") {
          resizeObserver.disconnect();
        }
        for (const entry of eventListeners) {
          if (entry.target && typeof entry.target.removeEventListener === "function") {
            entry.target.removeEventListener(entry.type, entry.listener);
          }
        }
        for (const unsub of unsubscribers) {
          if (typeof unsub === "function") {
            unsub();
          }
        }
        videoRestoreChildren(mount, fallbackChildren);
      },
    };
  }

  function createEngineContext(entry, mount, runtime) {
    return {
      id: entry.id,
      kind: entry.kind,
      component: entry.component,
      mount: mount,
      props: entry.props || {},
      capabilities: entry.capabilities || [],
      programRef: entry.programRef || "",
      runtimeMode: entry.runtime || "",
      jsRef: entry.jsRef || "",
      jsExport: entry.jsExport || "",
      runtime: runtime,
      emit: function(name, detail) {
        if (typeof document.dispatchEvent === "function" && typeof CustomEvent === "function") {
          document.dispatchEvent(new CustomEvent("gosx:engine:" + name, {
            detail: {
              engineID: entry.id,
              component: entry.component,
              detail: detail,
            },
          }));
        }
      },
    };
  }

  async function mountEngine(entry) {
    const existing = window.__gosx.engines.get(entry.id);
    if (existing) {
      window.__gosx_dispose_engine(entry.id);
    }

    const mount = resolveEngineMount(entry);
    if (engineKindNeedsMount(entry.kind) && !mount) return;
    const runtime = createEngineRuntime(entry, mount);
    const ctx = createEngineContext(entry, mount, runtime);
    pendingEngineRuntimes.set(entry.id, runtime);

    const factory = await resolveMountedEngineFactory(entry);
    if (typeof factory !== "function") {
      pendingEngineRuntimes.delete(entry.id);
      console.warn(`[gosx] no engine factory registered for ${entry.component}`);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "engine",
          type: "factory",
          component: entry.component,
          source: entry.id,
          ref: entry.jsRef || entry.component,
          element: mount,
          message: `no engine factory registered for ${entry.component}`,
          fallback: "server",
        });
      }
      return;
    }

    try {
      const mounted = await runEngineFactory(factory, ctx);
      pendingEngineRuntimes.delete(entry.id);
      rememberMountedEngine(entry, mount, mounted.context, mounted.handle);
    } catch (e) {
      pendingEngineRuntimes.delete(entry.id);
      if (runtime && typeof runtime.dispose === "function") {
        runtime.dispose();
      }
      console.error(`[gosx] failed to mount engine ${entry.id}:`, e);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "engine",
          type: "mount",
          component: entry.component,
          source: entry.id,
          ref: entry.jsRef || entry.programRef,
          element: mount,
          message: `failed to mount engine ${entry.id}`,
          error: e,
          fallback: "server",
        });
      }
    }
  }

  function resolveEngineMount(entry) {
    if (!engineKindNeedsMount(entry.kind)) {
      return null;
    }
    const mountID = entry.mountId || entry.id;
    const mount = document.getElementById(mountID);
    if (!mount) {
      console.warn(`[gosx] engine mount #${mountID} not found for ${entry.id}`);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "engine",
          type: "mount",
          component: entry.component,
          source: entry.id,
          ref: mountID,
          message: `engine mount #${mountID} not found`,
          fallback: "server",
        });
      }
      return null;
    }
    return mount;
  }

  async function resolveMountedEngineFactory(entry) {
    let factory = resolveEngineFactory(entry);
    if (!factory && entry.jsRef && !engineKindUsesBuiltinFactory(entry.kind)) {
      await loadEngineScript(entry.jsRef);
      factory = resolveEngineFactory(entry);
    }
    return factory;
  }

  async function runEngineFactory(factory, ctx) {
    let result = factory(ctx);
    if (result && typeof result.then === "function") {
      result = await result;
    }
    return {
      context: ctx,
      handle: normalizeEngineHandle(result),
    };
  }

  function rememberMountedEngine(entry, mount, context, handle) {
    if (window.__gosx && typeof window.__gosx.clearIssueState === "function") {
      window.__gosx.clearIssueState(mount);
    }
    activateInputProviders(entry);
    window.__gosx.engines.set(entry.id, {
      component: entry.component,
      kind: entry.kind,
      capabilities: capabilityList(entry),
      runtime: context.runtime,
      mount: mount,
      handle: handle,
    });
  }

  async function mountAllEngines(manifest) {
    if (!manifest.engines || manifest.engines.length === 0) return;

    const videoEngines = manifest.engines.filter(function(entry) {
      return entry && entry.kind === "video";
    });
    if (videoEngines.length > 1) {
      console.error("[gosx] only one video engine is supported per page");
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        for (const entry of videoEngines.slice(1)) {
          window.__gosx.reportIssue({
            scope: "engine",
            type: "mount",
            component: entry.component,
            source: entry.id,
            ref: entry.id,
            message: "only one video engine is supported per page",
            fallback: "server",
          });
        }
      }
    }

    const promises = manifest.engines.filter(function(entry, index) {
      return entry.kind !== "video" || videoEngines.indexOf(entry) === 0;
    }).map(function(entry) {
      return mountEngine(entry).catch(function(e) {
        console.error(`[gosx] unexpected error mounting engine ${entry.id}:`, e);
      });
    });

    await Promise.all(promises);
  }

  function hubURL(path) {
    if (!path) return "";
    if (isAbsoluteHubURL(path)) {
      return path;
    }
    return hubOrigin() + normalizeHubPath(path);
  }

  function isAbsoluteHubURL(path) {
    return path.startsWith("ws://") || path.startsWith("wss://");
  }

  function hubOrigin() {
    return hubScheme() + hubHost();
  }

  function hubScheme() {
    return window.location && window.location.protocol === "https:" ? "wss://" : "ws://";
  }

  function hubHost() {
    return window.location && window.location.host ? window.location.host : "";
  }

  function normalizeHubPath(path) {
    return path.startsWith("/") ? path : "/" + path;
  }

  function applyHubBindings(entry, message) {
    if (!entry.bindings || entry.bindings.length === 0) return;
    const setSharedSignal = window.__gosx_set_shared_signal;
    if (typeof setSharedSignal !== "function") return;

    for (const binding of entry.bindings) {
      applyHubBinding(entry, binding, message, setSharedSignal);
    }
  }

  function applyHubBinding(entry, binding, message, setSharedSignal) {
    if (!binding || binding.event !== message.event || !binding.signal) return;
    try {
      const result = setSharedSignal(binding.signal, JSON.stringify(message.data));
      if (typeof result === "string" && result !== "") {
        console.error(`[gosx] hub binding error (${entry.id}/${binding.signal}):`, result);
      }
    } catch (e) {
      console.error(`[gosx] hub binding error (${entry.id}/${binding.signal}):`, e);
    }
  }

  function connectHub(entry) {
    if (!canConnectHub(entry)) return;

    window.__gosx_disconnect_hub(entry.id);
    const record = createHubRecord(entry);
    window.__gosx.hubs.set(entry.id, record);
    attachHubSocketHandlers(record);
  }

  function canConnectHub(entry) {
    return Boolean(entry && entry.id && entry.path && typeof WebSocket === "function");
  }

  function createHubRecord(entry) {
    return {
      entry: entry,
      socket: new WebSocket(hubURL(entry.path)),
      reconnectTimer: null,
    };
  }

  function attachHubSocketHandlers(record) {
    const entry = record.entry;
    const socket = record.socket;
    socket.onmessage = function(evt) {
      const message = decodeHubMessage(entry, evt.data);
      if (!message) return;

      applyHubBindings(entry, message);
      emitHubEvent(entry, message);
    };

    socket.onclose = function() {
      scheduleHubReconnect(record);
    };

    socket.onerror = function(e) {
      console.error(`[gosx] hub connection error for ${entry.id}:`, e);
    };
  }

  function decodeHubMessage(entry, raw) {
    try {
      return JSON.parse(raw);
    } catch (e) {
      console.error(`[gosx] failed to decode hub message for ${entry.id}:`, e);
      return null;
    }
  }

  function emitHubEvent(entry, message) {
    if (typeof document.dispatchEvent !== "function" || typeof CustomEvent !== "function") {
      return;
    }
    document.dispatchEvent(new CustomEvent("gosx:hub:event", {
      detail: {
        hubID: entry.id,
        hubName: entry.name,
        event: message.event,
        data: message.data,
      },
    }));
  }

  function scheduleHubReconnect(record) {
    const entry = record.entry;
    const socket = record.socket;
    const current = window.__gosx.hubs.get(entry.id);
    if (!current || current.socket !== socket) return;
    current.reconnectTimer = setTimeout(function() {
      connectHub(entry);
    }, 1000);
  }

  async function connectAllHubs(manifest) {
    if (!manifest.hubs || manifest.hubs.length === 0) return;
    for (const entry of manifest.hubs) {
      connectHub(entry);
    }
  }

  window.__gosx_dispose_island = function(islandID) {
    const record = window.__gosx.islands.get(islandID);
    if (!record) return;

    if (record.root && record.listeners) {
      for (const entry of record.listeners) {
        record.root.removeEventListener(entry.type, entry.listener, entry.capture);
      }
    }

    if (typeof window.__gosx_dispose === "function") {
      try {
        window.__gosx_dispose(islandID);
      } catch (e) {
        console.error(`[gosx] dispose error for ${islandID}:`, e);
      }
    }

    window.__gosx.islands.delete(islandID);
  };

  window.__gosx_dispose_engine = function(engineID) {
    const pending = pendingEngineRuntimes.get(engineID);
    if (pending && typeof pending.dispose === "function") {
      try {
        pending.dispose();
      } catch (e) {
        console.error(`[gosx] pending runtime dispose error for engine ${engineID}:`, e);
      }
    }
    pendingEngineRuntimes.delete(engineID);

    const record = window.__gosx.engines.get(engineID);
    if (!record) return;

    releaseInputProviders(record);

    if (record.runtime && typeof record.runtime.dispose === "function") {
      try {
        record.runtime.dispose();
      } catch (e) {
        console.error(`[gosx] runtime dispose error for engine ${engineID}:`, e);
      }
    }

    if (record.handle && typeof record.handle.dispose === "function") {
      try {
        record.handle.dispose();
      } catch (e) {
        console.error(`[gosx] dispose error for engine ${engineID}:`, e);
      }
    }

    window.__gosx.engines.delete(engineID);
  };

  window.__gosx_engine_frame = function(engineID) {
    const pending = pendingEngineRuntimes.get(engineID);
    if (pending && typeof pending.frame === "function") {
      return pending.frame();
    }
    const record = window.__gosx.engines.get(engineID);
    if (!record || !record.runtime || typeof record.runtime.frame !== "function") {
      return null;
    }
    return record.runtime.frame();
  };

  window.__gosx_disconnect_hub = function(hubID) {
    const record = window.__gosx.hubs.get(hubID);
    if (!record) return;

    if (record.reconnectTimer) {
      clearTimeout(record.reconnectTimer);
      record.reconnectTimer = null;
    }
    if (record.socket && typeof record.socket.close === "function") {
      try {
        record.socket.close();
      } catch (e) {
        console.error(`[gosx] disconnect error for hub ${hubID}:`, e);
      }
    }

    window.__gosx.hubs.delete(hubID);
  };

  async function disposePage() {
    for (const islandID of Array.from(window.__gosx.islands.keys())) {
      window.__gosx_dispose_island(islandID);
    }
    for (const engineID of Array.from(window.__gosx.engines.keys())) {
      window.__gosx_dispose_engine(engineID);
    }
    for (const hubID of Array.from(window.__gosx.hubs.keys())) {
      window.__gosx_disconnect_hub(hubID);
    }
    disposeManagedMotion();
    disposeManagedTextLayouts();
    pendingManifest = null;
    window.__gosx.ready = false;
  }

  async function hydrateIsland(entry) {
    const root = islandRoot(entry);
    if (!root) return;
    if (entry.static) return;

    const program = await loadIslandProgram(entry, root);
    if (!program) return;
    if (!runIslandHydration(entry, root, program)) return;
    const listeners = setupEventDelegation(root, entry.id);
    rememberHydratedIsland(entry, root, listeners);
  }

  function islandRoot(entry) {
    const root = document.getElementById(entry.id);
    if (!root) {
      console.warn(`[gosx] island root #${entry.id} not found in DOM`);
      return null;
    }
    return root;
  }

  async function loadIslandProgram(entry, root) {
    const programFormat = inferProgramFormat(entry);
    if (!entry.programRef) {
      console.error(`[gosx] skipping island ${entry.id} — missing programRef`);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "island",
          type: "program",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          element: root,
          message: `missing programRef for island ${entry.id}`,
          fallback: "server",
        });
      }
      return null;
    }

    const programData = await fetchProgram(entry.programRef, programFormat);
    if (programData === null) {
      console.error(`[gosx] skipping island ${entry.id} — program fetch failed`);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "island",
          type: "program",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          element: root,
          message: `failed to fetch island program for ${entry.id}`,
          fallback: "server",
        });
      }
      return null;
    }
    return { data: programData, format: programFormat };
  }

  function runIslandHydration(entry, root, program) {
    const hydrateFn = window.__gosx_hydrate;
    if (typeof hydrateFn !== "function") {
      console.error("[gosx] __gosx_hydrate not available — cannot hydrate island", entry.id);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "island",
          type: "hydrate",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          element: root,
          message: `__gosx_hydrate not available for island ${entry.id}`,
          fallback: "server",
        });
      }
      return false;
    }

    try {
      const result = hydrateFn(
        entry.id,
        entry.component,
        JSON.stringify(entry.props || {}),
        program.data,
        program.format
      );
      if (typeof result === "string" && result !== "") {
        console.error(`[gosx] failed to hydrate island ${entry.id}: ${result}`);
        if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
          window.__gosx.reportIssue({
            scope: "island",
            type: "hydrate",
            component: entry.component,
            source: entry.id,
            ref: entry.programRef,
            element: root,
            message: result,
            fallback: "server",
          });
        }
        return false;
      }
      return true;
    } catch (e) {
      console.error(`[gosx] failed to hydrate island ${entry.id}:`, e);
      if (window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue({
          scope: "island",
          type: "hydrate",
          component: entry.component,
          source: entry.id,
          ref: entry.programRef,
          element: root,
          message: `failed to hydrate island ${entry.id}`,
          error: e,
          fallback: "server",
        });
      }
      return false;
    }
  }

  function rememberHydratedIsland(entry, root, listeners) {
    if (window.__gosx && typeof window.__gosx.clearIssueState === "function") {
      window.__gosx.clearIssueState(root);
    }
    window.__gosx.islands.set(entry.id, {
      component: entry.component,
      root: root,
      listeners: listeners,
    });
  }

  async function hydrateAllIslands(manifest) {
    if (!manifest.islands || manifest.islands.length === 0) return;

    const promises = manifest.islands.map(function(entry) {
      return hydrateIsland(entry).catch(function(e) {
        console.error(`[gosx] unexpected error hydrating ${entry.id}:`, e);
      });
    });

    await Promise.all(promises);
  }

  window.__gosx_runtime_ready = function() {
    if (typeof window.__gosx_text_layout === "function" && window.__gosx_text_layout !== gosxTextLayout) {
      adoptTextLayoutImpl(window.__gosx_text_layout);
      window.__gosx_text_layout = gosxTextLayout;
    }
    if (typeof window.__gosx_text_layout_metrics === "function" && window.__gosx_text_layout_metrics !== gosxTextLayoutMetrics) {
      adoptTextLayoutMetricsImpl(window.__gosx_text_layout_metrics);
      window.__gosx_text_layout_metrics = gosxTextLayoutMetrics;
    }
    if (typeof window.__gosx_text_layout_ranges === "function" && window.__gosx_text_layout_ranges !== gosxTextLayoutRanges) {
      adoptTextLayoutRangesImpl(window.__gosx_text_layout_ranges);
      window.__gosx_text_layout_ranges = gosxTextLayoutRanges;
    }
    refreshManagedTextLayouts();
    refreshGosxDocumentState("runtime-ready");
    refreshGosxEnvironmentState("runtime-ready");
    if (!pendingManifest) {
      window.__gosx.ready = true;
      refreshGosxDocumentState("ready");
      return;
    }

    mountAllEngines(pendingManifest).then(function() {
      return Promise.all([
        hydrateAllIslands(pendingManifest),
        connectAllHubs(pendingManifest),
      ]);
    }).then(function() {
      window.__gosx.ready = true;
      refreshGosxDocumentState("ready");
      document.dispatchEvent(new CustomEvent("gosx:ready"));
    }).catch(function(e) {
      console.error("[gosx] bootstrap failed:", e);
      window.__gosx.ready = true;
      refreshGosxDocumentState("ready");
    });
  };

  async function bootstrapPage() {
    refreshGosxEnvironmentState("bootstrap-page");
    refreshGosxDocumentState("bootstrap-page");
    mountManagedMotion(document.body || document.documentElement);
    mountManagedTextLayouts(document.body || document.documentElement);

    const manifest = loadManifest();
    if (!manifest) {
      pendingManifest = null;
      window.__gosx.ready = true;
      refreshGosxDocumentState("ready");
      return;
    }

    pendingManifest = manifest;
    window.__gosx.ready = false;

    if (manifestNeedsRuntime(manifest)) {
      if (runtimeReady()) {
        window.__gosx_runtime_ready();
      } else {
        await loadRuntime(manifest.runtime);
      }
    } else {
      if (manifestNeedsRuntimeBridge(manifest)) {
        console.error("[gosx] islands and hub bindings require manifest.runtime.path");
      }
      window.__gosx_runtime_ready();
    }
  }

  function manifestNeedsRuntimeBridge(manifest) {
    return manifestHasEntries(manifest, "islands")
      || manifestHasEntries(manifest, "hubs")
      || manifestNeedsVideoBridge(manifest)
      || manifestNeedsEngineInputBridge(manifest)
      || manifestNeedsSharedEngineRuntime(manifest);
  }

  function manifestNeedsRuntime(manifest) {
    return Boolean(manifestNeedsRuntimeBridge(manifest) && manifest.runtime && manifest.runtime.path);
  }

  function manifestNeedsEngineInputBridge(manifest) {
    if (!manifestHasEntries(manifest, "engines")) {
      return false;
    }
    return manifest.engines.some(function(entry) {
      const capabilities = capabilityList(entry);
      return capabilities.includes("keyboard") || capabilities.includes("pointer") || capabilities.includes("gamepad");
    });
  }

  function manifestNeedsSharedEngineRuntime(manifest) {
    if (!manifestHasEntries(manifest, "engines")) {
      return false;
    }
    return manifest.engines.some(function(entry) {
      return engineUsesSharedRuntime(entry);
    });
  }

  function manifestNeedsVideoBridge(manifest) {
    if (!manifestHasEntries(manifest, "engines")) {
      return false;
    }
    return manifest.engines.some(function(entry) {
      return entry && entry.kind === "video";
    });
  }

  function manifestHasEntries(manifest, key) {
    return Boolean(manifest && manifest[key] && manifest[key].length > 0);
  }

  window.__gosx_bootstrap_page = bootstrapPage;
  window.__gosx_dispose_page = disposePage;

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", bootstrapPage);
  } else {
    bootstrapPage();
  }
})();
//# sourceMappingURL=bootstrap.js.map
