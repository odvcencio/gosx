(function () {
  "use strict";

  const runtimePromises = new Map();
  const languagePromises = new Map();
  const forms = Array.from(document.querySelectorAll("form[data-code-intelligence-runtime]"));
  for (const form of forms) mount(form);

  async function mount(form) {
    const source = form.querySelector("textarea[name=content]");
    const highlight = form.querySelector("#editor-highlight-content");
    if (!source || !highlight) return;

    const cfg = form.dataset;
    const documentID = "gosx-editor:" + (cfg.collaborationCell || "local") + ":" +
      (cfg.collaborationPath || Math.random().toString(36).slice(2));
    let timer = 0;
    let runtime;

    try {
      runtime = await loadRuntime(cfg.codeIntelligenceWasmExec, cfg.codeIntelligenceRuntime);
      await loadLanguage(runtime, cfg);
      applyAnalysis(form, source, highlight,
        requireOK(runtime.open(cfg.codeIntelligenceLanguage, documentID, source.value)));
    } catch (error) {
      reportError(form, error);
      return;
    }

    const update = () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(() => {
        try {
          applyAnalysis(form, source, highlight, requireOK(runtime.update(documentID, source.value)));
        } catch (error) {
          reportError(form, error);
        }
      }, 40);
    };
    source.addEventListener("input", update);
    source.addEventListener("gosx:remote-input", update);
    window.addEventListener("pagehide", () => runtime.close(documentID), {once: true});
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
    renderHighlights(highlight, source.value, analysis.highlights || []);
    renderOutline(form, source, analysis.tags || []);
    const diagnostics = form.querySelector("#editor-diagnostics");
    if (diagnostics && analysis.hasError) diagnostics.textContent = "Syntax tree contains an error node.";
    form.dispatchEvent(new CustomEvent("gosx:code-analysis", {detail: analysis}));
  }

  function renderHighlights(target, source, ranges) {
    const fragment = document.createDocumentFragment();
    let cursor = 0;
    for (const range of ranges) {
      const start = Math.max(cursor, Math.min(source.length, Number(range.start16 || 0)));
      const end = Math.max(start, Math.min(source.length, Number(range.end16 || 0)));
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
    for (const form of forms) reportError(form, error instanceof Error ? error : new Error(String(error)));
  }
})();
