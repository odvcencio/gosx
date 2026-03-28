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

  let textLayoutGraphemeSegmenter = null;
  let textLayoutWordSegmenter = null;

  function gosxTextLayoutGraphemeSegmenter() {
    if (textLayoutGraphemeSegmenter !== null) {
      return textLayoutGraphemeSegmenter;
    }
    if (typeof Intl === "object" && Intl && typeof Intl.Segmenter === "function") {
      textLayoutGraphemeSegmenter = new Intl.Segmenter(undefined, { granularity: "grapheme" });
      return textLayoutGraphemeSegmenter;
    }
    textLayoutGraphemeSegmenter = false;
    return null;
  }

  function gosxTextLayoutWordSegmenter() {
    if (textLayoutWordSegmenter !== null) {
      return textLayoutWordSegmenter;
    }
    if (typeof Intl === "object" && Intl && typeof Intl.Segmenter === "function") {
      textLayoutWordSegmenter = new Intl.Segmenter(undefined, { granularity: "word" });
      return textLayoutWordSegmenter;
    }
    textLayoutWordSegmenter = false;
    return null;
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

  function segmentBrowserWordRun(text) {
    const value = String(text || "");
    if (value === "") {
      return [];
    }
    const segmenter = gosxTextLayoutWordSegmenter();
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

  function appendPreparedWordRun(tokens, text, byteStart, runeStart) {
    const value = String(text || "");
    if (value === "") {
      return;
    }

    const segments = segmentBrowserWordRun(value);
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

  function splitPreparedTextLayoutToken(token) {
    if (!token || token.kind === "newline" || token.kind === "tab" || token.kind === "soft-hyphen" || token.kind === "break" || !token.text) {
      return [token];
    }

    const graphemes = [];
    const segmenter = gosxTextLayoutGraphemeSegmenter();
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

  function prepareBrowserTextLayout(text, whiteSpace, tabSize) {
    const source = normalizeTextLayoutNewlines(text);
    const ws = normalizeTextLayoutWhiteSpace(whiteSpace);
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
      appendPreparedWordRun(tokens, word, wordByteStart, wordRuneStart);
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
      tabSize: resolvedTabSize,
      tokens,
    };
  }

  function measurePreparedBrowserTextLayout(prepared, font) {
    const expandedTokens = [];
    for (const token of prepared.tokens) {
      expandedTokens.push(...splitPreparedTextLayoutToken(token));
    }
    const measured = {
      source: prepared.source,
      byteLen: prepared.byteLen,
      runeCount: prepared.runeCount,
      whiteSpace: prepared.whiteSpace,
      tabSize: Math.max(1, Math.floor(sceneNumber(prepared.tabSize, 8))),
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
    const prepared = prepareBrowserTextLayout(text, whiteSpace, 8);
    const measured = measurePreparedBrowserTextLayout(prepared, font);
    const resolvedLineHeight = Math.max(1, sceneNumber(lineHeight, 1));
    const normalizedOptions = normalizeTextLayoutRunOptions(options);

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
    const prepared = prepareBrowserTextLayout(text, whiteSpace, 8);
    const measured = measurePreparedBrowserTextLayout(prepared, font);
    const resolvedLineHeight = Math.max(1, sceneNumber(lineHeight, 1));
    const normalizedOptions = normalizeTextLayoutRunOptions(options);

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
    style.textContent = [
      '[data-gosx-scene3d-mounted="true"] {',
      '  position: relative;',
      '  max-inline-size: 100%;',
      '  contain: layout paint style;',
      '}',
      '[data-gosx-scene3d-canvas="true"] {',
      '  display: block;',
      '  inline-size: 100%;',
      '  block-size: auto;',
      '  max-inline-size: 100%;',
      '  border-radius: inherit;',
      '}',
      '[data-gosx-text-layout-role="block"] {',
      '  min-block-size: var(--gosx-text-layout-height, auto);',
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
    const align = typeof options.align === "string" && options.align ? options.align : "";
    const revision = Number.isFinite(options.revision) ? options.revision : gosxTextLayoutRevision();
    const font = config && typeof config.font === "string" ? config.font : "";
    const whiteSpace = normalizeTextLayoutWhiteSpace(config && config.whiteSpace);
    const lineHeight = Math.max(1, textLayoutNumberValue(config && config.lineHeight, 16));
    const maxLines = Math.max(0, Math.floor(textLayoutNumberValue(config && config.maxLines, 0)));
    const overflow = normalizeTextLayoutOverflow(config && config.overflow);
    const maxWidth = textLayoutNumberValue(config && config.maxWidth, 0);
    const ready = Boolean(result);

    setAttrValue(element, TEXT_LAYOUT_ROLE_ATTR, role);
    setAttrValue(element, TEXT_LAYOUT_SURFACE_ATTR, surface);
    setAttrValue(element, TEXT_LAYOUT_STATE_ATTR, state);
    setAttrValue(element, "data-gosx-text-layout-ready", ready ? "true" : "false");
    setAttrValue(element, "data-gosx-text-layout-font", font);
    setAttrValue(element, "data-gosx-text-layout-white-space", whiteSpace === "normal" ? "" : whiteSpace);
    setAttrValue(element, "data-gosx-text-layout-align", align);
    setAttrValue(element, "data-gosx-text-layout-line-height", lineHeight > 0 ? lineHeight : "");
    setAttrValue(element, "data-gosx-text-layout-max-lines", maxLines > 0 ? maxLines : "");
    setAttrValue(element, "data-gosx-text-layout-overflow", maxLines > 0 ? overflow : "");
    setAttrValue(element, "data-gosx-text-layout-revision", revision);

    setStyleValue(element.style, "--gosx-text-layout-ready", ready ? "1" : "0");
    setStyleValue(element.style, "--gosx-text-layout-line-height", lineHeight + "px");
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
      setStyleValue(element.style, "--gosx-text-layout-width", maxWidth + "px");
      setStyleValue(element.style, "--gosx-text-layout-max-width", maxWidth + "px");
    } else {
      setAttrValue(element, "data-gosx-text-layout-max-width", "");
      clearStyleValue(element.style, "--gosx-text-layout-width");
      clearStyleValue(element.style, "--gosx-text-layout-max-width");
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
    const font = hasOwn.call(config, "font")
      ? String(config.font == null ? "" : config.font)
      : String((element.getAttribute && element.getAttribute("data-gosx-text-layout-font")) || "");
    const whiteSpace = normalizeTextLayoutWhiteSpace(
      hasOwn.call(config, "whiteSpace") ? config.whiteSpace : (element.getAttribute && element.getAttribute("data-gosx-text-layout-white-space"))
    );
    const lineHeight = Math.max(1, textLayoutNumberValue(
      hasOwn.call(config, "lineHeight") ? config.lineHeight : (element.getAttribute && element.getAttribute("data-gosx-text-layout-line-height")),
      16
    ));
    const maxLines = Math.max(0, Math.floor(textLayoutNumberValue(
      hasOwn.call(config, "maxLines") ? config.maxLines : (element.getAttribute && element.getAttribute("data-gosx-text-layout-max-lines")),
      0
    )));
    let maxWidth = textLayoutNumberValue(
      hasOwn.call(config, "maxWidth") ? config.maxWidth : (element.getAttribute && element.getAttribute("data-gosx-text-layout-max-width")),
      0
    );
    if (!(maxWidth > 0) && element && typeof element.getBoundingClientRect === "function") {
      const rect = element.getBoundingClientRect();
      maxWidth = textLayoutNumberValue(rect && rect.width, 0);
    }
    if (!(maxWidth > 0) && element) {
      maxWidth = textLayoutNumberValue(element.clientWidth || element.offsetWidth || element.width, 0);
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
      lineHeight,
      maxLines,
      overflow: normalizeTextLayoutOverflow(
        hasOwn.call(config, "overflow") ? config.overflow : (element.getAttribute && element.getAttribute("data-gosx-text-layout-overflow"))
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
      config.whiteSpace,
      config.lineHeight,
      config.maxLines,
      config.overflow,
      config.maxWidth,
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
    if (typeof record.stopInvalidation === "function") {
      record.stopInvalidation();
      record.stopInvalidation = null;
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
    };

    textLayoutRecordsByElement.set(element, record);
    applyManagedTextLayoutHint(element, normalizeManagedTextLayoutConfig(element, record.options));
    refreshManagedTextLayoutRecord(record, "mount");

    if (record.config && record.config.observe) {
      record.stopInvalidation = onTextLayoutInvalidated(function() {
        refreshManagedTextLayoutRecord(record, "invalidate");
      });

      if (typeof ResizeObserver === "function") {
        record.resizeObserver = new ResizeObserver(function() {
          refreshManagedTextLayoutRecord(record, "resize");
        });
        if (typeof record.resizeObserver.observe === "function") {
          record.resizeObserver.observe(element);
        }
      } else if (typeof window.addEventListener === "function") {
        record.windowResizeListener = function() {
          refreshManagedTextLayoutRecord(record, "resize");
        };
        window.addEventListener("resize", record.windowResizeListener);
      }

      if (typeof MutationObserver === "function") {
        record.mutationObserver = new MutationObserver(function() {
          refreshManagedTextLayoutRecord(record, "mutation");
        });
        if (typeof record.mutationObserver.observe === "function") {
          record.mutationObserver.observe(element, {
            subtree: true,
            childList: true,
            characterData: true,
            attributes: true,
          });
        }
      }
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
  // Pending manifest reference, set during init, consumed when runtime is ready.
  let pendingManifest = null;

  function runtimeReady() {
    return (
      typeof window.__gosx_hydrate === "function" ||
      typeof window.__gosx_action === "function" ||
      typeof window.__gosx_set_shared_signal === "function"
    );
  }

  // --------------------------------------------------------------------------
  // Manifest loading
  // --------------------------------------------------------------------------

  // Parse the inline JSON manifest from #gosx-manifest script tag.
  // Returns the parsed object, or null if missing/malformed.
  function loadManifest() {
    const el = document.getElementById("gosx-manifest");
    if (!el) return null;

    try {
      return JSON.parse(el.textContent);
    } catch (e) {
      console.error("[gosx] failed to parse manifest:", e);
      return null;
    }
  }

  // --------------------------------------------------------------------------
  // Shared WASM runtime loading
  // --------------------------------------------------------------------------

  // Load the single shared Go WASM binary referenced by the manifest runtime
  // entry. Uses Go's wasm_exec.js `Go` class. The WASM is expected to call
  // window.__gosx_runtime_ready() once it has finished initializing its
  // exported functions (__gosx_hydrate, __gosx_action, etc.).
  async function loadRuntime(runtimeRef) {
    if (typeof Go === "undefined") {
      console.error("[gosx] wasm_exec.js must be loaded before bootstrap.js");
      return;
    }

    const go = new Go();

    try {
      const response = await fetchRuntimeResponse(runtimeRef);
      const result = await instantiateRuntimeModule(response, go.importObject);
      // go.run is intentionally not awaited — it resolves when the Go main()
      // exits, but the runtime stays alive via syscall/js callbacks.
      go.run(result.instance);
    } catch (e) {
      console.error("[gosx] failed to load WASM runtime:", e);
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

  // --------------------------------------------------------------------------
  // Island program fetching
  // --------------------------------------------------------------------------

  // Fetch the compiled program data for a single island. Returns an
  // ArrayBuffer (for "wasm" format) or a string (for "json" or other text
  // formats). Returns null on failure.
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
      // Default: return as text (covers json, msgpack-base64, etc.)
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

  function setupSceneDragInteractions(canvas, props, readViewport, readSceneBundle) {
    if (!canvas || !sceneBool(props.dragToRotate, false)) {
      return { dispose() {} };
    }

    const dragNamespace = sceneDragSignalNamespace(props);
    const initialViewport = typeof readViewport === "function" ? readViewport() : null;
    const initialWidth = Math.max(1, sceneViewportValue(initialViewport, "cssWidth", sceneNumber(props.width, 720)));
    const initialHeight = Math.max(1, sceneViewportValue(initialViewport, "cssHeight", sceneNumber(props.height, 420)));
    const state = {
      active: false,
      orbitX: 0,
      orbitY: 0,
      pointerId: null,
      targetIndex: -1,
      lastX: initialWidth / 2,
      lastY: initialHeight / 2,
    };

    canvas.style.cursor = "grab";
    canvas.style.touchAction = "none";

    function publish(event, phase) {
      const viewport = typeof readViewport === "function" ? readViewport() : null;
      const width = Math.max(1, sceneViewportValue(viewport, "cssWidth", initialWidth));
      const height = Math.max(1, sceneViewportValue(viewport, "cssHeight", initialHeight));
      const sample = sceneLocalPointerSample(event, canvas, width, height, state, phase);
      if (!dragNamespace) {
        publishPointerSignals(sample);
        return;
      }
      if (phase === "move") {
        state.orbitX = sceneClamp(state.orbitX + sample.deltaX / Math.max(width / 2, 1), -1.35, 1.35);
        state.orbitY = sceneClamp(state.orbitY - sample.deltaY / Math.max(height / 2, 1), -1.1, 1.1);
      }
      publishSceneDragSignals(dragNamespace, state, phase !== "end");
    }

    function pointerMatchesActiveDrag(event) {
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

    function onPointerDown(event) {
      if (event.button !== 0) {
        return;
      }
      const viewport = typeof readViewport === "function" ? readViewport() : null;
      const width = Math.max(1, sceneViewportValue(viewport, "cssWidth", initialWidth));
      const height = Math.max(1, sceneViewportValue(viewport, "cssHeight", initialHeight));
      const pointer = sceneLocalPointerPoint(event, canvas, width, height);
      const target = sceneBundlePointerDragTarget(readSceneBundle && readSceneBundle(), pointer, width, height);
      if (!target) {
        return;
      }
      state.active = true;
      state.pointerId = event.pointerId;
      state.targetIndex = target.index;
      canvas.style.cursor = "grabbing";
      if (typeof canvas.setPointerCapture === "function") {
        canvas.setPointerCapture(event.pointerId);
      }
      event.preventDefault();
      event.stopPropagation();
      publish(event, "start");
    }

    function onPointerMove(event) {
      if (!pointerMatchesActiveDrag(event)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      publish(event, "move");
    }

    function finishDrag(event) {
      if (!pointerMatchesActiveDrag(event)) {
        return;
      }
      state.active = false;
      canvas.style.cursor = "grab";
      event.preventDefault();
      event.stopPropagation();
      if (state.pointerId != null && typeof canvas.releasePointerCapture === "function") {
        try {
          canvas.releasePointerCapture(state.pointerId);
        } catch (_) {}
      }
      state.pointerId = null;
      state.targetIndex = -1;
      if (dragNamespace) {
        publish(event, "end");
      } else {
        const viewport = typeof readViewport === "function" ? readViewport() : null;
        const width = Math.max(1, sceneViewportValue(viewport, "cssWidth", initialWidth));
        const height = Math.max(1, sceneViewportValue(viewport, "cssHeight", initialHeight));
        resetScenePointerSample(width, height, state);
      }
    }

    canvas.addEventListener("pointerdown", onPointerDown);
    canvas.addEventListener("pointermove", onPointerMove);
    canvas.addEventListener("pointerup", finishDrag);
    canvas.addEventListener("pointercancel", finishDrag);
    canvas.addEventListener("lostpointercapture", finishDrag);
    document.addEventListener("pointermove", onPointerMove);
    document.addEventListener("pointerup", finishDrag);
    document.addEventListener("pointercancel", finishDrag);

    return {
      dispose() {
        canvas.removeEventListener("pointerdown", onPointerDown);
        canvas.removeEventListener("pointermove", onPointerMove);
        canvas.removeEventListener("pointerup", finishDrag);
        canvas.removeEventListener("pointercancel", finishDrag);
        canvas.removeEventListener("lostpointercapture", finishDrag);
        document.removeEventListener("pointermove", onPointerMove);
        document.removeEventListener("pointerup", finishDrag);
        document.removeEventListener("pointercancel", finishDrag);
        canvas.style.cursor = "";
        canvas.style.touchAction = "";
        const viewport = typeof readViewport === "function" ? readViewport() : null;
        resetScenePointerSample(
          Math.max(1, sceneViewportValue(viewport, "cssWidth", initialWidth)),
          Math.max(1, sceneViewportValue(viewport, "cssHeight", initialHeight)),
          state,
        );
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

  function normalizeSceneObject(object, index) {
    const item = object && typeof object === "object" ? object : {};
    const size = sceneNumber(item.size, 1.2);
    return {
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
      color: typeof item.color === "string" && item.color ? item.color : "#8de1ff",
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
    };
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
    });
  }

  function normalizeSceneCamera(raw, fallback) {
    const base = fallback || {};
    return {
      x: sceneNumber(raw.x, sceneNumber(base.x, 0)),
      y: sceneNumber(raw.y, sceneNumber(base.y, 0)),
      z: sceneNumber(raw.z, sceneNumber(base.z, 6)),
      fov: sceneNumber(raw.fov, sceneNumber(base.fov, 75)),
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

  function createSceneRenderBundle(width, height, background, camera, objects, labels, timeSeconds) {
    const bundle = {
      background: background,
      camera: sceneRenderCamera(camera),
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
    appendSceneGridToBundle(bundle, width, height);
    for (const object of objects) {
      appendSceneObjectToBundle(bundle, camera, width, height, object, timeSeconds);
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

  function appendSceneObjectToBundle(bundle, camera, width, height, object, timeSeconds) {
    const worldSegments = sceneWorldObjectSegments(object, timeSeconds);
    const vertexOffset = bundle.worldPositions.length / 3;
    const rgba = sceneColorRGBA(object.color, [0.55, 0.88, 1, 1]);
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
      appendSceneLine(bundle, width, height, from, to, object.color, 1.8);
    }
    if (vertexCount > 0) {
      bundle.objects.push({
        id: object.id,
        kind: object.kind,
        vertexOffset: vertexOffset,
        vertexCount: vertexCount,
        bounds: bounds || {
          minX: 0,
          minY: 0,
          minZ: 0,
          maxX: 0,
          maxY: 0,
          maxZ: 0,
        },
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

  function createSceneWebGLRenderer(canvas) {
    if (!canvas || typeof canvas.getContext !== "function") {
      return null;
    }
    const gl = canvas.getContext("webgl", { antialias: true, alpha: false }) || canvas.getContext("experimental-webgl", { antialias: true, alpha: false });
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
      const name = String(source && source.name || "");
      const targetBuffers = buffers[name];
      if (!targetBuffers) {
        continue;
      }
      const isStatic = Boolean(source && source.static);
      const positions = sceneTypedFloatArray(source && source.positions);
      const colors = sceneTypedFloatArray(source && source.colors);
      const materials = sceneTypedFloatArray(source && source.materials);
      const vertexCount = Number.isFinite(source && source.vertexCount) ? source.vertexCount : positions.length / 3;
      passes.push({
        name,
        blend: String(source && source.blend || "opaque"),
        depth: String(source && source.depth || "opaque"),
        usage: isStatic ? usages.staticDraw : usages.dynamicDraw,
        cacheSlot: isStatic ? "staticOpaque" : "",
        cacheKey: String(source && source.cacheKey || ""),
        buffers: targetBuffers,
        positions,
        colors,
        materials,
        vertexCount,
      });
    }
    return passes;
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
      return [4, sceneMaterialEmissive(material), 0.78];
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
  const sceneMaterialStringFields = ["kind", "color", "blendMode"];
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
  function createSceneRenderer(canvas, props) {
    if (sceneBool(props.preferWebGL, true)) {
      const webglRenderer = createSceneWebGLRenderer(canvas);
      if (webglRenderer) {
        return webglRenderer;
      }
    }
    const ctx2d = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
    if (!ctx2d) {
      return null;
    }
    return createSceneCanvasRenderer(ctx2d, canvas);
  }

  function sceneViewportBase(props) {
    const width = Math.max(240, sceneNumber(props && props.width, 720));
    const height = Math.max(180, sceneNumber(props && props.height, 420));
    return {
      baseWidth: width,
      baseHeight: height,
      aspectRatio: width / Math.max(1, height),
      responsive: sceneBool(props && props.responsive, true),
      maxDevicePixelRatio: Math.max(1, sceneNumber(props && (props.maxDevicePixelRatio || props.maxPixelRatio), 2)),
    };
  }

  function sceneViewportDevicePixelRatio(props, maxDevicePixelRatio) {
    const preferred = sceneNumber(
      props && (props.devicePixelRatio || props.pixelRatio),
      sceneNumber(window && window.devicePixelRatio, 1),
    );
    return Math.max(1, Math.min(Math.max(1, maxDevicePixelRatio || 1), preferred));
  }

  function sceneViewportFromMount(mount, props, base, canvas) {
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
    const devicePixelRatio = sceneViewportDevicePixelRatio(props, base.maxDevicePixelRatio);
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
    let orientationListener = null;
    let viewportResizeListener = null;

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

    if (typeof window.addEventListener === "function") {
      orientationListener = function() {
        refresh("orientation");
      };
      window.addEventListener("orientationchange", orientationListener);
    }

    if (window.visualViewport && typeof window.visualViewport.addEventListener === "function") {
      viewportResizeListener = function() {
        refresh("visual-viewport");
      };
      window.visualViewport.addEventListener("resize", viewportResizeListener);
    }

    return function() {
      if (resizeObserver && typeof resizeObserver.disconnect === "function") {
        resizeObserver.disconnect();
      }
      if (windowResizeListener && typeof window.removeEventListener === "function") {
        window.removeEventListener("resize", windowResizeListener);
      }
      if (orientationListener && typeof window.removeEventListener === "function") {
        window.removeEventListener("orientationchange", orientationListener);
      }
      if (viewportResizeListener && window.visualViewport && typeof window.visualViewport.removeEventListener === "function") {
        window.visualViewport.removeEventListener("resize", viewportResizeListener);
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
    const viewportBase = sceneViewportBase(props);
    const sceneState = createSceneState(props);
    const runtimeScene = ctx.runtimeMode === "shared" && Boolean(ctx.programRef);
    const objects = sceneStateObjects(sceneState);

    function sceneShouldAnimate() {
      if (runtimeScene || sceneBool(props.autoRotate, true)) {
        return true;
      }
      return sceneStateObjects(sceneState).some(sceneObjectAnimated) || sceneStateLabels(sceneState).some(sceneLabelAnimated);
    }

    clearChildren(ctx.mount);
    ctx.mount.setAttribute("data-gosx-scene3d-mounted", "true");
    ctx.mount.setAttribute("aria-label", props.ariaLabel || props.label || "Interactive GoSX 3D scene");
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

    let viewport = applySceneViewport(ctx.mount, canvas, labelLayer, sceneViewportFromMount(ctx.mount, props, viewportBase, canvas), viewportBase);

    const renderer = createSceneRenderer(canvas, props);
    if (!renderer) {
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
    ctx.mount.setAttribute("data-gosx-scene3d-renderer", renderer.kind);
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
      if (disposed || !latestBundle) {
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
      const nextViewport = sceneViewportFromMount(ctx.mount, props, viewportBase, canvas);
      if (sceneViewportChanged(viewport, nextViewport)) {
        viewport = applySceneViewport(ctx.mount, canvas, labelLayer, nextViewport, viewportBase);
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

    if (runtimeScene) {
      if (ctx.runtime && ctx.runtime.available()) {
        applySceneCommands(sceneState, await ctx.runtime.hydrateFromProgramRef());
      } else {
        console.warn("[gosx] Scene3D runtime requested but shared engine runtime is unavailable");
      }
    }

    function renderFrame(now) {
      if (disposed) return;
      viewport = applySceneViewport(ctx.mount, canvas, labelLayer, sceneViewportFromMount(ctx.mount, props, viewportBase, canvas), viewportBase);
      const timeSeconds = now / 1000;
      if (runtimeScene && ctx.runtime && typeof ctx.runtime.renderFrame === "function") {
        const runtimeBundle = ctx.runtime.renderFrame(timeSeconds, viewport.cssWidth, viewport.cssHeight);
        if (runtimeBundle) {
          latestBundle = runtimeBundle;
          renderer.render(runtimeBundle, viewport);
          renderSceneLabels(labelLayer, runtimeBundle, labelLayoutCache, labelElements, viewport.cssWidth, viewport.cssHeight);
          if (sceneShouldAnimate()) {
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
      if (sceneShouldAnimate()) {
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
      },
      dispose() {
        disposed = true;
        releaseViewportObserver();
        releaseTextLayoutListener();
        dragHandle.dispose();
        renderer.dispose();
        if (frameHandle != null) {
          cancelEngineFrame(frameHandle);
        }
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
  // --------------------------------------------------------------------------
  // Event delegation
  // --------------------------------------------------------------------------

  // Event types that are delegated on each island root element.
  const DELEGATED_EVENTS = [
    "click", "input", "change", "submit",
    "keydown", "keyup", "focus", "blur",
  ];

  // Extract a small payload from a DOM event for forwarding to WASM.
  function extractEventData(e) {
    const data = { type: e.type };

    switch (e.type) {
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
        // Prevent default form submission — the WASM handler decides what to do.
        e.preventDefault();
        break;
      // click, focus, blur: no extra data needed beyond type
    }

    return data;
  }

  // Attach ONE delegated listener per event type on `islandRoot`. Each
  // listener walks the ancestor chain from event.target to the root looking
  // for a `data-gosx-handler` attribute. If found, it calls the WASM-side
  // __gosx_action(islandID, handlerName, eventDataJSON).
  //
  // Returns an array of { type, listener } objects so callers can remove them.
  // Handler attribute pattern: data-gosx-on-{eventType}="handlerName"
  // Examples: data-gosx-on-click="increment", data-gosx-on-input="updateName"
  // Falls back to data-gosx-handler for click-only (legacy/shorthand).
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

  // --------------------------------------------------------------------------
  // Engine mounting
  // --------------------------------------------------------------------------

  function resolveEngineFactory(entry) {
    const exportName = engineExportName(entry);
    if (!exportName) return null;
    return engineFactories[exportName] || null;
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

  function createEngineRuntime(entry) {
    let programPromise = null;

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
      dispose() {
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

  function createEngineContext(entry, mount) {
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
      runtime: createEngineRuntime(entry),
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
    if (entry.kind === "surface" && !mount) return;

    const factory = await resolveMountedEngineFactory(entry);
    if (typeof factory !== "function") {
      console.warn(`[gosx] no engine factory registered for ${entry.component}`);
      return;
    }

    try {
      const mounted = await runEngineFactory(factory, entry, mount);
      rememberMountedEngine(entry, mount, mounted.context, mounted.handle);
    } catch (e) {
      console.error(`[gosx] failed to mount engine ${entry.id}:`, e);
    }
  }

  function resolveEngineMount(entry) {
    if (entry.kind !== "surface") {
      return null;
    }
    const mountID = entry.mountId || entry.id;
    const mount = document.getElementById(mountID);
    if (!mount) {
      console.warn(`[gosx] engine mount #${mountID} not found for ${entry.id}`);
      return null;
    }
    return mount;
  }

  async function resolveMountedEngineFactory(entry) {
    let factory = resolveEngineFactory(entry);
    if (!factory && entry.jsRef) {
      await loadEngineScript(entry.jsRef);
      factory = resolveEngineFactory(entry);
    }
    return factory;
  }

  async function runEngineFactory(factory, entry, mount) {
    const ctx = createEngineContext(entry, mount);
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

    const promises = manifest.engines.map(function(entry) {
      return mountEngine(entry).catch(function(e) {
        console.error(`[gosx] unexpected error mounting engine ${entry.id}:`, e);
      });
    });

    await Promise.all(promises);
  }

  // --------------------------------------------------------------------------
  // Hub connections
  // --------------------------------------------------------------------------

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

  // --------------------------------------------------------------------------
  // Island disposal
  // --------------------------------------------------------------------------

  // Remove all delegated event listeners for an island and clear it from the
  // tracking map. Optionally calls the WASM-side __gosx_dispose if available.
  window.__gosx_dispose_island = function(islandID) {
    const record = window.__gosx.islands.get(islandID);
    if (!record) return;

    // Remove delegated listeners from the island root.
    if (record.root && record.listeners) {
      for (const entry of record.listeners) {
        record.root.removeEventListener(entry.type, entry.listener, entry.capture);
      }
    }

    // Notify WASM side if dispose function is available.
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
    disposeManagedTextLayouts();
    pendingManifest = null;
    window.__gosx.ready = false;
  }

  // --------------------------------------------------------------------------
  // Hydration
  // --------------------------------------------------------------------------

  // Hydrate a single island: fetch its program data, call __gosx_hydrate,
  // and set up event delegation on the island root element.
  async function hydrateIsland(entry) {
    const root = islandRoot(entry);
    if (!root) return;
    if (entry.static) return;

    const program = await loadIslandProgram(entry);
    if (!program) return;
    if (!runIslandHydration(entry, program)) return;
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

  async function loadIslandProgram(entry) {
    const programFormat = inferProgramFormat(entry);
    if (!entry.programRef) {
      console.error(`[gosx] skipping island ${entry.id} — missing programRef`);
      return null;
    }

    const programData = await fetchProgram(entry.programRef, programFormat);
    if (programData === null) {
      console.error(`[gosx] skipping island ${entry.id} — program fetch failed`);
      return null;
    }
    return { data: programData, format: programFormat };
  }

  function runIslandHydration(entry, program) {
    const hydrateFn = window.__gosx_hydrate;
    if (typeof hydrateFn !== "function") {
      console.error("[gosx] __gosx_hydrate not available — cannot hydrate island", entry.id);
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
        return false;
      }
      return true;
    } catch (e) {
      console.error(`[gosx] failed to hydrate island ${entry.id}:`, e);
      return false;
    }
  }

  function rememberHydratedIsland(entry, root, listeners) {
    window.__gosx.islands.set(entry.id, {
      component: entry.component,
      root: root,
      listeners: listeners,
    });
  }

  // Hydrate all islands from the manifest. Called once the WASM runtime
  // signals readiness via __gosx_runtime_ready.
  async function hydrateAllIslands(manifest) {
    if (!manifest.islands || manifest.islands.length === 0) return;

    // Hydrate islands concurrently — each is independent.
    const promises = manifest.islands.map(function(entry) {
      return hydrateIsland(entry).catch(function(e) {
        console.error(`[gosx] unexpected error hydrating ${entry.id}:`, e);
      });
    });

    await Promise.all(promises);
  }

  // --------------------------------------------------------------------------
  // Runtime ready callback
  // --------------------------------------------------------------------------

  // Called by the Go WASM binary once the runtime has finished initializing
  // and all exported functions (__gosx_hydrate, __gosx_action, etc.) are
  // registered. This is the signal that it is safe to hydrate islands.
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
    if (!pendingManifest) {
      window.__gosx.ready = true;
      return;
    }

    Promise.all([
      hydrateAllIslands(pendingManifest),
      mountAllEngines(pendingManifest),
      connectAllHubs(pendingManifest),
    ]).then(function() {
      window.__gosx.ready = true;
      document.dispatchEvent(new CustomEvent("gosx:ready"));
    }).catch(function(e) {
      console.error("[gosx] bootstrap failed:", e);
      window.__gosx.ready = true;
    });
  };

  // --------------------------------------------------------------------------
  // Main initialization
  // --------------------------------------------------------------------------

  async function bootstrapPage() {
    mountManagedTextLayouts(document.body || document.documentElement);

    const manifest = loadManifest();
    if (!manifest) {
      // No manifest — pure server-rendered page, no islands to hydrate.
      pendingManifest = null;
      window.__gosx.ready = true;
      return;
    }

    // Stash manifest for use when WASM signals readiness.
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

  function manifestHasEntries(manifest, key) {
    return Boolean(manifest && manifest[key] && manifest[key].length > 0);
  }

  window.__gosx_bootstrap_page = bootstrapPage;
  window.__gosx_dispose_page = disposePage;

  // Start when DOM is ready.
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", bootstrapPage);
  } else {
    bootstrapPage();
  }
})();
