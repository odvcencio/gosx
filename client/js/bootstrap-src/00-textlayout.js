// GoSX Client Bootstrap v0.2.0
// Loads shared WASM runtime, fetches per-island programs, hydrates islands
// via event delegation. This is the only JavaScript in a GoSX app.
//
// Expects:
//   - wasm_exec.js loaded before this script (standard Go WASM support)
//   - <script id="gosx-manifest" type="application/json"> with island manifest
//   - WASM exports: __gosx_hydrate, __gosx_action, __gosx_dispose
//   - WASM calls window.__gosx_runtime_ready() when Go runtime is initialized

(function() {
  "use strict";

  const GOSX_VERSION = "0.2.0";

  // --- GoSX runtime namespace ---
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
    input: {
      pending: null,
      frameHandle: 0,
      providers: Object.create(null),
    },
    ready: false,
  };

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
