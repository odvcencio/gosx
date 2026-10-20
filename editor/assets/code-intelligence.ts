(function () {
  "use strict";

  const mountKey = Symbol.for("gosx.editor.intelligence");
  if (window[mountKey]) {
    window[mountKey]();
    return;
  }
  const runtimePromises = new Map();
  const languagePromises = new Map();
  const forms = new Map();
  const selector = "form[data-code-intelligence-runtime], form[data-code-intelligence-server]";
  const firstPaint = waitForFirstContentfulPaint();
  function refresh() {
    for (const [form, state] of forms) {
      if (!form.isConnected || form.querySelector("textarea[name=content]") !== state.source) {
        state.cleanup();
        forms.delete(form);
      }
    }
    for (const form of document.querySelectorAll(selector)) {
      if (!forms.has(form)) void mount(form);
      else forms.get(form).refresh();
    }
  }
  window[mountKey] = refresh;
  firstPaint.then(refresh);
  new MutationObserver(records => {
    const relevant = node => node.nodeType === 1 && (node.matches(selector + ", textarea[name=content]") || node.querySelector(selector));
    if (records.some(record => record.type === "attributes"
      ? record.target.matches(selector + ", .editor-page-native")
      : record.target.matches?.("textarea[name=content]") || Array.from(record.addedNodes).some(relevant) || Array.from(record.removedNodes).some(relevant))) {
      firstPaint.then(refresh);
    }
  }).observe(document.documentElement, {
    childList: true, subtree: true, attributes: true,
    attributeFilter: ["class", "data-code-intelligence-lane", "data-highlight-span-count"],
  });

  function waitForFirstContentfulPaint() {
    if (performance.getEntriesByName("first-contentful-paint", "paint").length > 0) return Promise.resolve();
    if (typeof PerformanceObserver !== "function") {
      return new Promise(resolve => setTimeout(resolve, 1500));
    }
    return new Promise(resolve => {
      let settled = false;
      const finish = () => {
        if (settled) return;
        settled = true;
        observer.disconnect();
        resolve();
      };
      const observer = new PerformanceObserver(entries => {
        if (entries.getEntries().some(entry => entry.name === "first-contentful-paint")) finish();
      });
      observer.observe({type: "paint", buffered: true});
    });
  }

  async function mount(form) {
    const source = form.querySelector("textarea[name=content]");
    const highlight = form.querySelector("#editor-highlight-content");
    if (!source || !highlight) return;

    const cfg = form.dataset;
    const documentID = "gosx-editor:" + (cfg.collaborationCell || "local") + ":" +
      (cfg.collaborationPath || Math.random().toString(36).slice(2));
    let timer = 0;
    let runtime;
    let runtimeOpen = false;
    let latestTags = [];
    let serverFallback = false;
    let requestSequence = 0;
    let latestAnalysis;
    let analyzedSource;
    let refreshAnalysis = () => {};
    let disposed = false;
    const lifetime = new AbortController();
    const live = () => !disposed && form.isConnected;
    const cleanup = () => {
      disposed = true;
      requestSequence++;
      window.clearTimeout(timer);
      lifetime.abort();
      if (runtimeOpen) runtime.close(documentID);
    };
    forms.set(form, {source, cleanup, refresh: () => refreshAnalysis()});
    const initialSource = source.value;
    const acceptAnalysis = analysis => {
      latestTags = analysis.tags || [];
      latestAnalysis = analysis;
      analyzedSource = source.value;
      applyAnalysis(form, source, highlight, analysis);
    };

    try {
      let initial;
      if (cfg.codeIntelligenceWasmExec && cfg.codeIntelligenceRuntime && cfg.codeIntelligenceGrammar && cfg.codeIntelligenceHighlights) {
        runtime = await loadRuntime(cfg.codeIntelligenceWasmExec, cfg.codeIntelligenceRuntime);
        if (!live()) return;
        await loadLanguage(runtime, cfg);
        if (!live()) return;
        initial = requireOK(runtime.open(cfg.codeIntelligenceLanguage, documentID, source.value));
        runtimeOpen = true;
      } else if (cfg.codeIntelligenceServer) {
        serverFallback = true;
        initial = await requestServerAnalysis(form, cfg.codeIntelligenceServer, cfg.collaborationPath, initialSource, lifetime.signal);
      } else {
        throw new Error("Code intelligence has neither browser assets nor a server endpoint.");
      }
      if (!live()) return;
      if (source.value === initialSource) {
        acceptAnalysis(initial);
      }
    } catch (error) {
      if (!live()) return;
      if (!cfg.codeIntelligenceServer) {
        reportError(form, error);
        return;
      }
      try {
        serverFallback = true;
        const initial = await requestServerAnalysis(form, cfg.codeIntelligenceServer, cfg.collaborationPath, initialSource, lifetime.signal);
        if (!live()) return;
        if (source.value === initialSource) {
          acceptAnalysis(initial);
        }
      } catch (fallbackError) {
        if (!live()) return;
        reportError(form, fallbackError);
        // Keep the input binding so an edit can retry a transient outage.
      }
    }

    const update = () => {
      if (!live()) return;
      window.clearTimeout(timer);
      const sequence = ++requestSequence;
      timer = window.setTimeout(async () => {
        const value = source.value;
        try {
          const analysis = serverFallback
            ? await requestServerAnalysis(form, cfg.codeIntelligenceServer, cfg.collaborationPath, value, lifetime.signal)
            : requireOK(runtime.update(documentID, value));
          if (!live() || sequence !== requestSequence || value !== source.value) return;
          acceptAnalysis(analysis);
        } catch (error) {
          if (live() && sequence === requestSequence) reportError(form, error);
        }
      }, 40);
    };
    source.addEventListener("input", update, {signal: lifetime.signal});
    source.addEventListener("gosx:remote-input", update, {signal: lifetime.signal});
    source.addEventListener("keydown", (event) => {
      if (event.isComposing || event.keyCode === 229) return;
      if (event.key === "F12") {
        const target = definitionAtCursor(source, latestTags);
        if (!target) return;
        event.preventDefault();
        const offset = Number(target.nameRange?.start16 ?? tagStart(target));
        source.setSelectionRange(offset, offset);
        source.focus();
        return;
      }
      if (event.altKey && event.shiftKey && event.key === "ArrowUp") {
        const target = enclosingTag(source, latestTags);
        if (!target) return;
        event.preventDefault();
        source.setSelectionRange(tagStart(target), tagEnd(target));
      }
    }, {signal: lifetime.signal});
    window.addEventListener("pagehide", cleanup, {once: true, signal: lifetime.signal});
    refreshAnalysis = () => {
      if (!live()) return;
      if (analyzedSource !== source.value) {
        update();
      } else if (latestAnalysis && (form.dataset.codeIntelligenceLane !== (latestAnalysis.lane || "wasm") ||
        form.dataset.highlightSpanCount !== String((latestAnalysis.highlights || []).length))) {
        applyAnalysis(form, source, highlight, latestAnalysis);
      }
    };
    if (source.value !== initialSource) update();
  }

  async function requestServerAnalysis(form, url, path, source, signal) {
    const response = await fetch(url, {
      signal,
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": form.querySelector("[name='csrf_token']")?.value || "",
        "X-Mercutio-Capability": form.querySelector("[name='capability']")?.value || "",
      },
      body: JSON.stringify({path: path || "", content: source}),
    });
    if (!response.ok) throw new Error("Server code intelligence returned " + response.status + ".");
    const analysis = await response.json();
    if (analysis.error) throw new Error(analysis.error);
    const offsets = byteToUTF16Offsets(source);
    const fromByte = offset => offsets[Math.max(0, Math.min(offsets.length - 1, Number(offset || 0)))];
    return {
      highlights: (analysis.highlights || []).map(range => ({
		startByte: range.startByte,
		endByte: range.endByte,
		startUTF16: fromByte(range.startByte),
		endUTF16: fromByte(range.endByte),
        start16: fromByte(range.startByte),
        end16: fromByte(range.endByte),
        capture: range.capture,
      })),
      tags: (analysis.symbols || []).map(symbol => ({
        kind: symbol.kind,
        name: symbol.name,
        range: {start16: fromByte(symbol.range?.startByte), end16: fromByte(symbol.range?.endByte)},
        nameRange: {start16: fromByte(symbol.nameRange?.startByte), end16: fromByte(symbol.nameRange?.endByte)},
      })),
      hasError: Boolean(analysis.hasErrors),
      lane: "server",
    };
  }

  function byteToUTF16Offsets(source) {
    const encoder = new TextEncoder();
    const offsets = new Uint32Array(encoder.encode(source).length + 1);
    let byteOffset = 0;
    let utf16Offset = 0;
    for (const character of source) {
      byteOffset += encoder.encode(character).length;
      utf16Offset += character.length;
      offsets[byteOffset] = utf16Offset;
    }
    return offsets;
  }

  function tagStart(tag) {
    return Number(tag.range?.start16 ?? tag.nameRange?.start16 ?? 0);
  }

  function tagEnd(tag) {
    return Number(tag.range?.end16 ?? tag.nameRange?.end16 ?? tagStart(tag));
  }

  function enclosingTag(source, tags) {
    const start = source.selectionStart || 0;
    const end = source.selectionEnd || start;
    return tags
      .filter(tag => tagStart(tag) <= start && tagEnd(tag) >= end &&
        (tagStart(tag) < start || tagEnd(tag) > end))
      .sort((a, b) => (tagEnd(a) - tagStart(a)) - (tagEnd(b) - tagStart(b)))[0];
  }

  function definitionAtCursor(source, tags) {
    const cursor = source.selectionStart || 0;
    const text = source.value;
    let start = cursor;
    let end = cursor;
    while (start > 0 && /[\p{L}\p{N}_$]/u.test(text[start - 1])) start--;
    while (end < text.length && /[\p{L}\p{N}_$]/u.test(text[end])) end++;
    const name = text.slice(start, end);
    if (!name) return null;
    return tags.find(tag => String(tag.kind || "").startsWith("definition.") && tag.name === name) || null;
  }

  function loadRuntime(wasmExecURL, runtimeURL) {
    if (!wasmExecURL || !runtimeURL) return Promise.reject(new Error("Code intelligence runtime URLs are incomplete."));
    const key = wasmExecURL + "\n" + runtimeURL;
    if (runtimePromises.has(key)) return runtimePromises.get(key);
    const promise = (async () => {
      await loadScript(wasmExecURL);
      if (typeof window.Go !== "function") throw new Error("Go WASM bootstrap did not initialize.");
      const go = new window.Go();
      const response = await fetch(runtimeURL, {credentials: "same-origin"});
      if (!response.ok) throw new Error("Code intelligence runtime returned " + response.status + ".");
      const instance = await WebAssembly.instantiate(await response.arrayBuffer(), go.importObject);
      void go.run(instance.instance).catch(error => reportGlobalError(error));
      for (let attempt = 0; attempt < 200 && !window.gotreesitter; attempt++) {
        await new Promise(resolve => window.setTimeout(resolve, 5));
      }
      if (!window.gotreesitter) throw new Error("Code intelligence runtime did not become ready.");
      return window.gotreesitter;
    })();
    runtimePromises.set(key, promise);
    return promise;
  }

  function loadLanguage(runtime, cfg) {
    const language = cfg.codeIntelligenceLanguage;
    const key = [language, cfg.codeIntelligenceGrammar, cfg.codeIntelligenceHighlights, cfg.codeIntelligenceTags].join("\n");
    if (languagePromises.has(key)) return languagePromises.get(key);
    const promise = Promise.all([
      fetchBytes(cfg.codeIntelligenceGrammar),
      fetchText(cfg.codeIntelligenceHighlights),
      cfg.codeIntelligenceTags ? fetchText(cfg.codeIntelligenceTags) : Promise.resolve("")
    ]).then(([grammar, highlights, tags]) => requireOK(runtime.loadBlob(language, grammar, highlights, tags)));
    languagePromises.set(key, promise);
    return promise;
  }

  function applyAnalysis(form, source, highlight, analysis) {
    const lane = analysis.lane || "wasm";
    if (form.dataset.codeIntelligenceLane !== lane) form.dataset.codeIntelligenceLane = lane;
    renderHighlights(highlight, source.value, analysis.highlights || []);
	form.dispatchEvent(new CustomEvent("gosx:highlight-spans", {detail: {spans: analysis.highlights || []}}));
    renderOutline(form, source, analysis.tags || []);
    const diagnostics = form.querySelector("#editor-diagnostics");
    if (diagnostics) diagnostics.textContent = analysis.hasError ? "Syntax tree contains an error node." : "";
    form.dispatchEvent(new CustomEvent("gosx:code-analysis", {detail: analysis}));
  }

  function renderHighlights(target, source, ranges) {
    const fragment = document.createDocumentFragment();
    let cursor = 0;
    for (const range of ranges) {
	  const start = Math.max(cursor, Math.min(source.length, Number(range.startUTF16 ?? range.start16 ?? 0)));
	  const end = Math.max(start, Math.min(source.length, Number(range.endUTF16 ?? range.end16 ?? 0)));
      if (start > cursor) fragment.appendChild(document.createTextNode(source.slice(cursor, start)));
      if (end > start) {
        const span = document.createElement("span");
        span.className = "syntax-" + String(range.capture || "plain").toLowerCase().replace(/[^a-z0-9_-]+/g, "-");
        span.textContent = source.slice(start, end);
        fragment.appendChild(span);
      }
      cursor = end;
    }
    if (cursor < source.length) fragment.appendChild(document.createTextNode(source.slice(cursor)));
    fragment.appendChild(document.createTextNode("\n"));
    target.replaceChildren(fragment);
  }

  function renderOutline(form, source, tags) {
    const nav = form.querySelector("#editor-outline-headings");
    if (!nav) return;
    const definitions = tags.filter(tag => String(tag.kind || "").startsWith("definition."));
    if (definitions.length === 0) {
      const empty = document.createElement("p");
      empty.className = "editor-preview-empty";
      empty.textContent = "No symbols in this file.";
      nav.replaceChildren(empty);
      return;
    }
    const nodes = definitions.map(tag => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "editor-outline-item";
      button.dataset.offset = String(tag.nameRange?.start16 ?? tag.range?.start16 ?? 0);
      button.textContent = tag.name + " · " + String(tag.kind).slice("definition.".length);
      return button;
    });
    nav.replaceChildren(...nodes);
  }

  async function fetchBytes(url) {
    if (!url) throw new Error("Code intelligence grammar URL is missing.");
    const response = await fetch(url, {credentials: "same-origin"});
    if (!response.ok) throw new Error("Grammar asset returned " + response.status + ".");
    return new Uint8Array(await response.arrayBuffer());
  }

  async function fetchText(url) {
    if (!url) throw new Error("Code intelligence query URL is missing.");
    const response = await fetch(url, {credentials: "same-origin"});
    if (!response.ok) throw new Error("Query asset returned " + response.status + ".");
    return response.text();
  }

  function loadScript(url) {
    const existing = document.querySelector('script[data-gosx-runtime="' + CSS.escape(url) + '"]');
    if (existing) return existing.__gosxPromise || Promise.resolve();
    const script = document.createElement("script");
    script.src = url;
    script.defer = true;
    script.dataset.gosxRuntime = url;
    script.__gosxPromise = new Promise((resolve, reject) => {
      script.addEventListener("load", resolve, {once: true});
      script.addEventListener("error", () => reject(new Error("Unable to load " + url + ".")), {once: true});
    });
    document.head.appendChild(script);
    return script.__gosxPromise;
  }

  function requireOK(result) {
    if (!result || result.ok !== true) throw new Error(result?.error || "Code intelligence operation failed.");
    return result;
  }

  function reportError(form, error) {
    const diagnostics = form.querySelector("#editor-diagnostics");
    if (diagnostics) diagnostics.textContent = "Code intelligence unavailable: " + error.message;
    form.dispatchEvent(new CustomEvent("gosx:code-analysis-error", {detail: {message: error.message}}));
  }

  function reportGlobalError(error) {
    for (const form of forms.keys()) reportError(form, error instanceof Error ? error : new Error(String(error)));
  }
})();
