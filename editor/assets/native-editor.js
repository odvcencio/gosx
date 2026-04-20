(() => {
  const ready = (fn) => {
    if (document.readyState === "loading") {
      document.addEventListener("DOMContentLoaded", fn, { once: true });
      return;
    }
    fn();
  };

  const escapeHTML = (value) => String(value)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");

  const WORDS_PER_MINUTE = 225;

  const markdownStatsText = (source) => {
    const lines = String(source || "").replace(/\r\n?/g, "\n").split("\n");
    const out = [];
    let inFence = false;
    for (const line of lines) {
      const trimmed = line.trim();
      if (trimmed.startsWith("```") || trimmed.startsWith("~~~")) {
        inFence = !inFence;
        continue;
      }
      if (
        inFence ||
        /^\s*\[\[toc\]\]\s*$/i.test(trimmed) ||
        /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(trimmed)
      ) {
        continue;
      }

      let text = line
        .replace(/^\s{0,3}>\s?/, "")
        .replace(/^\s{0,3}#{1,6}\s+/, "")
        .replace(/^\s*(?:[-+*]|\d+[.)])\s+/, "")
        .replace(/^\s*\[[ xX]\]\s+/, "")
        .replace(/^\s*\[\^[^\]\n]+\]:\s*/, "")
        .replace(/\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*/gi, "")
        .replace(/!\[([^\]\n]*)\]\([^)]+\)/g, "$1")
        .replace(/\[([^\]\n]+)\]\([^)]+\)/g, "$1")
        .replace(/\[([^\]\n]+)\]\[[^\]\n]*\]/g, "$1")
        .replace(/\[\^[^\]\n]+\]/g, "")
        .replace(/<[^>\n]+>/g, " ")
        .replace(/:[a-zA-Z0-9_+-]+:/g, " ");
      text = text.replace(/[|`*_~]/g, " ");
      out.push(text);
    }
    return out.join("\n");
  };

  const computeEditorStats = (source) => {
    const words = markdownStatsText(source).match(/[\p{L}\p{N}]+(?:['\u2019_-][\p{L}\p{N}]+)*/gu) || [];
    const wordCount = words.length;
    return {
      words: wordCount,
      minutes: wordCount > 0 ? Math.max(1, Math.ceil(wordCount / WORDS_PER_MINUTE)) : 0,
    };
  };

  const replaceToken = (html, pattern, className) => html.replace(pattern, (match) => {
    return `<span class="${className}">${match}</span>`;
  });

  const emoji = (...points) => String.fromCodePoint(...points);

  const emojiItems = [
    { name: "smile", glyph: emoji(0x1f604), keywords: "happy grin joy" },
    { name: "sweat_smile", glyph: emoji(0x1f605), keywords: "relief nervous happy" },
    { name: "joy", glyph: emoji(0x1f602), keywords: "laugh tears funny" },
    { name: "thinking", glyph: emoji(0x1f914), keywords: "hmm question consider" },
    { name: "eyes", glyph: emoji(0x1f440), keywords: "look watching attention" },
    { name: "wave", glyph: emoji(0x1f44b), keywords: "hello bye hand" },
    { name: "thumbs_up", glyph: emoji(0x1f44d), keywords: "yes approve like" },
    { name: "clap", glyph: emoji(0x1f44f), keywords: "applause nice" },
    { name: "pray", glyph: emoji(0x1f64f), keywords: "thanks please hope" },
    { name: "heart", glyph: emoji(0x2764, 0xfe0f), keywords: "love favorite" },
    { name: "fire", glyph: emoji(0x1f525), keywords: "hot ship energy" },
    { name: "sparkles", glyph: emoji(0x2728), keywords: "polish magic shine" },
    { name: "tada", glyph: emoji(0x1f389), keywords: "celebrate shipped party" },
    { name: "rocket", glyph: emoji(0x1f680), keywords: "launch ship deploy" },
    { name: "zap", glyph: emoji(0x26a1), keywords: "fast lightning energy" },
    { name: "bulb", glyph: emoji(0x1f4a1), keywords: "idea light" },
    { name: "warning", glyph: emoji(0x26a0, 0xfe0f), keywords: "caution alert" },
    { name: "check", glyph: emoji(0x2705), keywords: "done pass yes" },
    { name: "x", glyph: emoji(0x274c), keywords: "no fail stop" },
    { name: "bug", glyph: emoji(0x1f41b), keywords: "issue defect" },
    { name: "wrench", glyph: emoji(0x1f527), keywords: "fix tool" },
    { name: "lock", glyph: emoji(0x1f512), keywords: "secure private" },
    { name: "key", glyph: emoji(0x1f511), keywords: "access secret" },
    { name: "memo", glyph: emoji(0x1f4dd), keywords: "note writing draft" },
    { name: "book", glyph: emoji(0x1f4d6), keywords: "docs read" },
    { name: "link", glyph: emoji(0x1f517), keywords: "url chain" },
    { name: "image", glyph: emoji(0x1f5bc, 0xfe0f), keywords: "picture media" },
    { name: "laptop", glyph: emoji(0x1f4bb), keywords: "code work computer" },
    { name: "coffee", glyph: emoji(0x2615), keywords: "caffeine focus" },
    { name: "music", glyph: emoji(0x1f3b5), keywords: "song audio" },
    { name: "headphones", glyph: emoji(0x1f3a7), keywords: "listening audio" },
    { name: "calendar", glyph: emoji(0x1f4c5), keywords: "schedule date" },
    { name: "hourglass", glyph: emoji(0x23f3), keywords: "time waiting" },
    { name: "star", glyph: emoji(0x2b50), keywords: "favorite rating" },
    { name: "sun", glyph: emoji(0x2600, 0xfe0f), keywords: "bright day" },
    { name: "moon", glyph: emoji(0x1f319), keywords: "night late" },
    { name: "cloud", glyph: emoji(0x2601, 0xfe0f), keywords: "weather" },
    { name: "rain", glyph: emoji(0x1f327, 0xfe0f), keywords: "weather storm" },
  ];

  const highlightInline = (line) => {
    let html = escapeHTML(line);
    html = html.replace(/(\[[^\]\n]+\])(\([^)]+\))/g, '<span class="mdpp-link-text">$1</span><span class="mdpp-link-url">$2</span>');
    html = replaceToken(html, /\[\^[^\]\n]+\]/g, "mdpp-footnote");
    html = replaceToken(html, /:[a-z0-9_+-]+:/gi, "mdpp-emoji");
    html = replaceToken(html, /`[^`\n]+`/g, "mdpp-inline-code");
    html = replaceToken(html, /\*\*[^*\n]+\*\*/g, "mdpp-strong");
    html = replaceToken(html, /~~[^~\n]+~~/g, "mdpp-strike");
    html = replaceToken(html, /(^|[^*])\*[^*\n]+\*/g, "mdpp-em");
    return html;
  };

  const highlightQuoteBody = (body) => {
    const quoteHeading = body.match(/^\[(?![!^])([^\]\n]+)\]$/);
    if (quoteHeading) {
      return `<span class="mdpp-quote-heading">[${highlightInline(quoteHeading[1])}]</span>`;
    }
    let html = highlightInline(body);
    html = html.replace(/\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]/gi, (match, type) => {
      return `<span class="mdpp-admonition mdpp-admonition-${type.toLowerCase()}">${match}</span>`;
    });
    return html;
  };

  const diagramFenceLanguage = (info) => {
    const lang = String(info || "").trim().split(/\s+/)[0].toLowerCase().replace(/_/g, "-");
    switch (lang) {
      case "mermaid":
      case "mmd":
      case "flow":
      case "flowchart":
      case "graph":
      case "erd":
      case "er":
      case "erdiagram":
      case "sequence":
      case "sequencediagram":
      case "class":
      case "classdiagram":
      case "state":
      case "statediagram":
      case "statediagram-v2":
      case "gantt":
      case "journey":
      case "pie":
      case "mindmap":
      case "timeline":
      case "gitgraph":
        return lang;
      default:
        return "";
    }
  };

  const highlightMarkdownPP = (source) => {
    const lines = String(source).split("\n");
    const out = [];
    let inFence = false;
    let fenceDiagramLang = "";
    for (const line of lines) {
      const fence = line.match(/^(\s*)(```+|~~~+)(.*)$/);
      if (fence) {
        const opening = !inFence;
        if (opening) fenceDiagramLang = diagramFenceLanguage(fence[3]);
        const className = fenceDiagramLang ? "mdpp-fence mdpp-diagram-fence" : "mdpp-fence";
        inFence = !inFence;
        if (!opening) fenceDiagramLang = "";
        out.push(`<span class="${className}">${escapeHTML(line)}</span>`);
        continue;
      }
      if (inFence) {
        const className = fenceDiagramLang ? "mdpp-code-line mdpp-diagram-line" : "mdpp-code-line";
        if (line === "") {
          out.push(`<span class="${className} mdpp-empty-line">&nbsp;</span>`);
        } else {
          out.push(`<span class="${className}">${escapeHTML(line)}</span>`);
        }
        continue;
      }
      if (line.trim() === "") {
        out.push('<span class="mdpp-empty-line">&nbsp;</span>');
        continue;
      }
      const heading = line.match(/^(#{1,6})(\s+.*)$/);
      if (heading) {
        out.push(`<span class="mdpp-heading-marker">${escapeHTML(heading[1])}</span><span class="mdpp-heading">${highlightInline(heading[2])}</span>`);
        continue;
      }
      const footnoteDef = line.match(/^(\[\^[^\]]+\]:)(.*)$/);
      if (footnoteDef) {
        out.push(`<span class="mdpp-footnote">${escapeHTML(footnoteDef[1])}</span>${highlightInline(footnoteDef[2])}`);
        continue;
      }
      const quote = line.match(/^(\s*>\s*)(.*)$/);
      if (quote) {
        out.push(`<span class="mdpp-quote">${escapeHTML(quote[1])}</span>${highlightQuoteBody(quote[2])}`);
        continue;
      }
      const list = line.match(/^(\s*(?:[-*+]|\d+\.)\s+(?:\[[ xX]\]\s+)?)(.*)$/);
      if (list) {
        out.push(`<span class="mdpp-list-marker">${escapeHTML(list[1])}</span>${highlightInline(list[2])}`);
        continue;
      }
      const rule = line.match(/^\s{0,3}(?:---+|\*\*\*+|___+)\s*$/);
      if (rule) {
        out.push(`<span class="mdpp-rule">${escapeHTML(line)}</span>`);
        continue;
      }
      const mathFence = line.match(/^\s*\${2}.*\${0,2}\s*$/);
      if (mathFence) {
        out.push(`<span class="mdpp-math">${escapeHTML(line)}</span>`);
        continue;
      }
      out.push(highlightInline(line));
    }
    return out.join("\n");
  };

  const commandSnippet = (command, selection) => {
    const sel = selection || "";
    switch (command) {
      case "bold": return `**${sel || "bold"}**`;
      case "italic": return `*${sel || "italic"}*`;
      case "strike": return `~~${sel || "text"}~~`;
      case "code": return `\n\`\`\`\n${sel || "code"}\n\`\`\`\n`;
      case "inlinecode": return `\`${sel || "code"}\``;
      case "link": return `[${sel || "text"}](url)`;
      case "h1": return `\n# ${sel || "Heading"}\n`;
      case "h2": return `\n## ${sel || "Heading"}\n`;
      case "h3": return `\n### ${sel || "Heading"}\n`;
      case "list": return `\n- ${sel || "item"}\n`;
      case "ordered_list": return `\n1. ${sel || "item"}\n`;
      case "task_list": return `\n- [ ] ${sel || "todo"}\n`;
      case "blockquote": return `\n> ${sel || "quote"}\n`;
      case "note": return `\n> [!NOTE]\n> ${sel || "Note content"}\n`;
      case "warning": return `\n> [!WARNING]\n> ${sel || "Warning content"}\n`;
      case "math": return `\n$$\n${sel || "E = mc^2"}\n$$\n`;
      case "diagram":
        return `\n\`\`\`mermaid\n${sel}\n\`\`\`\n`;
      case "footnote": return `[^${sel || "1"}]`;
      case "hr": return "\n---\n";
      case "scene3d":
        return `\n\`\`\`gosx-scene\n${sel || "title: Inline orbit\nshape: cube\ncolor: \"#d4af37\"\nbackground: \"#080b10\"\nheight: 320"}\n\`\`\`\n`;
      case "island":
        return `\n\`\`\`gosx-island\n${sel || "component: counter\ntitle: Counter island\ncount: 0"}\n\`\`\`\n`;
      default:
        return "";
    }
  };

  const selectedPanel = () => {
    const checked = document.querySelector(".editor-panel-radio:checked");
    return checked ? checked.value : "preview";
  };

  const formDataWithSource = (form, source) => {
    const params = new URLSearchParams(new FormData(form));
    params.set("content", source);
    return params;
  };

  const renderPreviewDiagrams = (root) => {
    if (!window.M31Diagrams || typeof window.M31Diagrams.render !== "function") return;
    return window.M31Diagrams.render(root);
  };

  ready(() => {
    const form = document.querySelector("form[data-editor-native='true']");
    const textarea = document.getElementById("editor-content");
    const highlight = document.getElementById("editor-highlight-content");
    const lineNumbers = document.getElementById("editor-line-numbers");
    const saveStatus = document.getElementById("editor-save-status");
    if (!form || !textarea || !highlight) return;

    const page = document.querySelector(".editor-page-native");
    page?.classList.add("editor-highlight-ready");

    let previewTimer = null;
    let previewRequest = 0;
    let previewInFlight = false;
    let previewPending = false;
    let metadataTimer = null;
    let metadataRequest = 0;
    let metadataInFlight = false;
    let autosaveTimer = null;
    let autosaveInFlight = false;
    let autosavePending = false;
    let lastAutosaveFingerprint = "";
    let emojiPicker = null;
    let emojiSearch = null;
    let emojiGrid = null;
    let emojiResults = [];
    let emojiActiveIndex = 0;
    let emojiReplaceRange = null;
    let galleryLoaded = false;
    let lastPreviewSource = null;
    let lineMeasure = null;
    let renderFrame = 0;
    let visualRowToLine = [];
    let visualRowMapSource = null;
    let visualRowMapWidth = 0;
    let preserveWhitespaceOnlyLines = false;
    const previewIdleDelay = 300;
    const metadataIdleDelay = 450;
    const autosaveDelay = 1800;

    const setSaveStatus = (state, label) => {
      if (!saveStatus) return;
      saveStatus.textContent = label;
      saveStatus.className = `editor-save-status editor-save-status-${state}`;
    };

    const renderHighlight = () => {
      highlight.innerHTML = highlightMarkdownPP(textarea.value) + "\n";
    };

    textarea.spellcheck = false;
    textarea.setAttribute("spellcheck", "false");

    const sourceLineHeight = () => {
      const style = getComputedStyle(textarea);
      const lineHeight = parseFloat(style.lineHeight);
      if (Number.isFinite(lineHeight) && lineHeight > 0) return lineHeight;
      const fontSize = parseFloat(style.fontSize);
      return Number.isFinite(fontSize) && fontSize > 0 ? fontSize * 1.6 : 24;
    };

    const sourceContentWidth = () => {
      const style = getComputedStyle(textarea);
      const paddingLeft = parseFloat(style.paddingLeft) || 0;
      const paddingRight = parseFloat(style.paddingRight) || 0;
      return Math.max(1, textarea.clientWidth - paddingLeft - paddingRight);
    };

    const ensureLineMeasure = () => {
      if (lineMeasure) return lineMeasure;
      lineMeasure = document.createElement("div");
      lineMeasure.setAttribute("aria-hidden", "true");
      lineMeasure.style.cssText = [
        "position:absolute",
        "left:0",
        "top:0",
        "z-index:-1",
        "visibility:hidden",
        "pointer-events:none",
        "overflow:visible",
        "white-space:pre-wrap",
        "overflow-wrap:break-word",
        "word-break:normal",
        "box-sizing:content-box",
        "padding:0",
        "border:0",
      ].join(";");
      textarea.parentElement.appendChild(lineMeasure);
      return lineMeasure;
    };

    const syncLineMeasureStyle = (measure, width) => {
      const style = getComputedStyle(textarea);
      measure.style.width = `${width}px`;
      measure.style.font = style.font;
      measure.style.fontFamily = style.fontFamily;
      measure.style.fontSize = style.fontSize;
      measure.style.fontWeight = style.fontWeight;
      measure.style.fontStyle = style.fontStyle;
      measure.style.fontVariantLigatures = style.fontVariantLigatures;
      measure.style.fontFeatureSettings = style.fontFeatureSettings;
      measure.style.letterSpacing = style.letterSpacing;
      measure.style.lineHeight = style.lineHeight;
      measure.style.tabSize = style.tabSize;
    };

    const normalizeWhitespaceOnlyLines = () => {
      const value = textarea.value;
      const normalized = value.replace(/(^|\n)[ \t]+(?=\n|$)/g, "$1");
      if (normalized === value) return false;
      const start = textarea.selectionStart || 0;
      const end = textarea.selectionEnd || start;
      const normalizeOffset = (offset) => value.slice(0, offset).replace(/(^|\n)[ \t]+(?=\n|$)/g, "$1").length;
      textarea.value = normalized;
      textarea.setSelectionRange(normalizeOffset(start), normalizeOffset(end));
      return true;
    };

    const buildVisualRows = () => {
      const html = [];
      const rowToLine = [];
      const lines = textarea.value.split("\n");
      const width = sourceContentWidth();
      const lineHeight = sourceLineHeight();
      const measure = ensureLineMeasure();
      syncLineMeasureStyle(measure, width);

      const fragment = document.createDocumentFragment();
      for (const line of lines) {
        const row = document.createElement("span");
        row.style.display = "block";
        row.style.minHeight = `${lineHeight}px`;
        row.textContent = line === "" ? "\u00a0" : line;
        fragment.appendChild(row);
      }
      measure.replaceChildren(fragment);

      Array.from(measure.children).forEach((row, index) => {
        const height = row.getBoundingClientRect().height;
        const visualRows = Math.max(1, Math.round(height / lineHeight));
        html.push(`<span>${index + 1}</span>`);
        rowToLine.push(index);
        for (let i = 1; i < visualRows; i += 1) {
          html.push('<span class="editor-line-continuation">.</span>');
          rowToLine.push(index);
        }
      });
      return { html: html.join(""), rowToLine, source: textarea.value, width };
    };

    const renderLineNumbers = () => {
      const rows = buildVisualRows();
      visualRowToLine = rows.rowToLine;
      visualRowMapSource = rows.source;
      visualRowMapWidth = rows.width;
      if (lineNumbers) lineNumbers.innerHTML = rows.html;
    };

    const ensureVisualRowMap = () => {
      const width = sourceContentWidth();
      if (visualRowMapSource === textarea.value && visualRowMapWidth === width) return;
      const rows = buildVisualRows();
      visualRowToLine = rows.rowToLine;
      visualRowMapSource = rows.source;
      visualRowMapWidth = rows.width;
    };

    const lineStartOffsets = () => {
      const offsets = [0];
      const value = textarea.value;
      for (let i = 0; i < value.length; i += 1) {
        if (value[i] === "\n") offsets.push(i + 1);
      }
      return offsets;
    };

    const focusBlankVisualRow = (event) => {
      const style = getComputedStyle(textarea);
      const rect = textarea.getBoundingClientRect();
      const lineHeight = parseFloat(style.lineHeight) || 24;
      const paddingTop = parseFloat(style.paddingTop) || 0;
      const paddingLeft = parseFloat(style.paddingLeft) || 0;
      const paddingRight = parseFloat(style.paddingRight) || 0;
      const x = event.clientX - rect.left;
      if (x < paddingLeft || x > textarea.clientWidth - paddingRight) return false;
      const y = event.clientY - rect.top - paddingTop + textarea.scrollTop;
      if (y < 0) return false;
      const visualRow = Math.floor(y / lineHeight);
      ensureVisualRowMap();
      const lines = textarea.value.split("\n");
      const offsets = lineStartOffsets();
      const lineIndex = visualRowToLine[visualRow];

      if (typeof lineIndex === "number") {
        if (lines[lineIndex] !== "") return false;
        event.preventDefault();
        textarea.focus();
        textarea.setSelectionRange(offsets[lineIndex], offsets[lineIndex]);
        return true;
      }

      const missingRows = visualRow - visualRowToLine.length + 1;
      if (missingRows <= 0) return false;
      event.preventDefault();
      textarea.focus();
      textarea.value += "\n".repeat(missingRows);
      textarea.setSelectionRange(textarea.value.length, textarea.value.length);
      textarea.dispatchEvent(new Event("input", { bubbles: true }));
      return true;
    };

    const syncScroll = () => {
      const layer = highlight.closest(".editor-highlight-layer");
      if (layer) {
        layer.scrollTop = textarea.scrollTop;
        layer.scrollLeft = textarea.scrollLeft;
      }
      if (lineNumbers) {
        lineNumbers.scrollTop = textarea.scrollTop;
      }
    };

    const rebuildOutline = () => {
      const nav = document.getElementById("editor-outline-headings");
      if (!nav) return;
      const headings = [];
      const re = /^(#{1,6})\s+(.+)$/gm;
      let match;
      while ((match = re.exec(textarea.value)) !== null) {
        headings.push({ depth: match[1].length, text: match[2].trim(), offset: match.index });
      }
      if (headings.length === 0) {
        nav.innerHTML = '<p class="editor-preview-empty">Start writing to see your outline.</p>';
        return;
      }
      nav.innerHTML = headings.map((h) => {
        return `<button type="button" class="editor-outline-item editor-outline-h${h.depth}" data-offset="${h.offset}">${escapeHTML(h.text)}</button>`;
      }).join("");
    };

    const flushPreview = async () => {
      previewTimer = null;
      if (!previewPending || previewInFlight) return;
      const preview = document.getElementById("editor-preview-content");
      const url = form.dataset.previewUrl;
      if (!preview || !url) return;
      const source = textarea.value;
      previewPending = false;
      if (source === lastPreviewSource) return;
      const request = ++previewRequest;
      previewInFlight = true;
      try {
        const res = await fetch(url, {
          method: "POST",
          headers: {
            "Accept": "application/json",
            "Content-Type": "application/x-www-form-urlencoded",
            "X-CSRF-Token": form.querySelector("[name='csrf_token']")?.value || "",
          },
          body: formDataWithSource(form, source),
        });
        if (!res.ok || request !== previewRequest) return;
        const json = await res.json();
        const data = json.data || json;
        if (data && typeof data.redirect === "string" && data.redirect !== "") {
          window.location.href = data.redirect;
          return;
        }
        const html = data ? data.html : "";
        if (typeof html === "string" && source === textarea.value) {
          preview.innerHTML = html || '<p class="editor-preview-empty">No content yet.</p>';
          void renderPreviewDiagrams(preview);
          lastPreviewSource = source;
          if (data && data.saved === true) {
            clearTimeout(autosaveTimer);
            autosavePending = false;
            lastAutosaveFingerprint = autosaveFingerprint();
            setSaveStatus("saved", "Saved");
          }
        }
      } catch (_) {
        // Keep the current server-rendered preview if the request fails.
      } finally {
        previewInFlight = false;
        if (selectedPanel() === "preview" && textarea.value !== lastPreviewSource) {
          updatePreview();
        }
      }
    };

    const updatePreview = () => {
      if (textarea.value === lastPreviewSource) return;
      previewPending = true;
      if (previewInFlight) return;
      clearTimeout(previewTimer);
      previewTimer = setTimeout(flushPreview, previewIdleDelay);
    };

    const metadataField = (name) => form.querySelector(`[name="${name}"]`);

    const setMetadataField = (name, value) => {
      const field = metadataField(name);
      if (!field || typeof value !== "string" || field.value === value) return;
      field.value = value;
      field.dispatchEvent(new Event("input", { bubbles: true }));
      field.dispatchEvent(new Event("change", { bubbles: true }));
    };

    const mergeTags = (current, suggested) => {
      const seen = new Set();
      const merged = [];
      for (const raw of `${current || ""},${suggested || ""}`.split(",")) {
        const tag = raw.trim();
        const key = tag.toLowerCase();
        if (!tag || seen.has(key)) continue;
        seen.add(key);
        merged.push(tag);
      }
      return merged.join(", ");
    };

    const updateExcerptPreview = (html) => {
      const preview = document.getElementById("editor-excerpt-preview");
      if (!preview || typeof html !== "string") return;
      preview.innerHTML = html;
    };

    const requestMetadata = async (mode = "preview") => {
      const url = form.dataset.metadataUrl;
      if (!url || metadataInFlight) return;
      const request = ++metadataRequest;
      metadataInFlight = true;
      try {
        const res = await fetch(url, {
          method: "POST",
          headers: {
            "Accept": "application/json",
            "Content-Type": "application/x-www-form-urlencoded",
            "X-CSRF-Token": form.querySelector("[name='csrf_token']")?.value || "",
          },
          body: formDataWithSource(form, textarea.value),
        });
        if (!res.ok || request !== metadataRequest) return;
        const json = await res.json();
        const data = json.data || json;
        updateExcerptPreview(data.excerpt_html || "");
        if (mode === "excerpt" && data.excerpt) {
          setMetadataField("excerpt", data.excerpt);
          updateExcerptPreview(data.generated_excerpt_html || data.excerpt_html || "");
        }
        if (mode === "tags" && data.tags_text) {
          const tags = metadataField("tags");
          setMetadataField("tags", mergeTags(tags ? tags.value : "", data.tags_text));
        }
        if (mode === "mood" && data.suggestedMood) {
          setMetadataField("mood", data.suggestedMood);
        }
      } catch (_) {
        // Metadata helpers are advisory; keep editing responsive on failure.
      } finally {
        metadataInFlight = false;
      }
    };

    const scheduleMetadataPreview = () => {
      if (!form.dataset.metadataUrl) return;
      clearTimeout(metadataTimer);
      metadataTimer = setTimeout(() => {
        void requestMetadata("preview");
      }, metadataIdleDelay);
    };

    const autosaveFormData = () => formDataWithSource(form, textarea.value);

    const autosaveFingerprint = () => {
      const params = autosaveFormData();
      params.delete("csrf_token");
      params.delete("editor_panel");
      return params.toString();
    };

    const queueAutosaveTimer = () => {
      clearTimeout(autosaveTimer);
      autosaveTimer = setTimeout(flushAutosave, autosaveDelay);
    };

    const scheduleAutosave = () => {
      if (!form.dataset.autosaveUrl) return;
      if (autosaveFingerprint() === lastAutosaveFingerprint) {
        if (!autosaveInFlight) setSaveStatus("saved", "Saved");
        return;
      }
      autosavePending = true;
      setSaveStatus("unsaved", "Unsaved");
      if (!autosaveInFlight) queueAutosaveTimer();
    };

    async function flushAutosave() {
      autosaveTimer = null;
      if (!autosavePending || autosaveInFlight) return;
      const url = form.dataset.autosaveUrl;
      if (!url) return;

      const fingerprint = autosaveFingerprint();
      if (fingerprint === lastAutosaveFingerprint) {
        autosavePending = false;
        setSaveStatus("saved", "Saved");
        return;
      }

      autosavePending = false;
      autosaveInFlight = true;
      setSaveStatus("saving", "Saving...");
      try {
        const res = await fetch(url, {
          method: "POST",
          headers: {
            "Accept": "application/json",
            "Content-Type": "application/x-www-form-urlencoded",
            "X-CSRF-Token": form.querySelector("[name='csrf_token']")?.value || "",
          },
          body: autosaveFormData(),
        });
        if (!res.ok) throw new Error("autosave failed");
        const json = await res.json().catch(() => ({}));
        const data = json.data || json;
        lastAutosaveFingerprint = fingerprint;
        setSaveStatus("saved", "Saved");
        if (data && typeof data.redirect === "string" && data.redirect !== "") {
          window.location.href = data.redirect;
        }
      } catch (_) {
        autosavePending = autosaveFingerprint() !== fingerprint;
        setSaveStatus("error", navigator.onLine === false ? "Offline" : "Save failed");
      } finally {
        autosaveInFlight = false;
        if (autosavePending) queueAutosaveTimer();
      }
    }

    const updateEditorStats = () => {
      const wordCount = document.getElementById("editor-word-count");
      const wordLabel = document.getElementById("editor-word-label");
      const readingTime = document.getElementById("editor-reading-time");
      const readingLabel = document.getElementById("editor-reading-label");
      if (!wordCount && !readingTime) return;

      const stats = computeEditorStats(textarea.value);
      if (wordCount) wordCount.textContent = String(stats.words);
      if (wordLabel) wordLabel.textContent = stats.words === 1 ? "word" : "words";
      if (readingTime) readingTime.textContent = String(stats.minutes);
      if (readingLabel) readingLabel.textContent = "min read";
    };

    const renderEditorFrame = () => {
      renderFrame = 0;
      renderHighlight();
      renderLineNumbers();
      syncScroll();
      rebuildOutline();
      updateEditorStats();
    };

    const scheduleEditorRender = () => {
      if (renderFrame) return;
      renderFrame = requestAnimationFrame(renderEditorFrame);
    };

    const dispatchEditorInput = (options = {}) => {
      preserveWhitespaceOnlyLines = Boolean(options.preserveWhitespaceOnlyLines);
      try {
        textarea.dispatchEvent(new Event("input", { bubbles: true }));
      } finally {
        preserveWhitespaceOnlyLines = false;
      }
    };

    const insertAtSelection = (snippet) => {
      if (!snippet) return;
      const start = textarea.selectionStart || 0;
      const end = textarea.selectionEnd || start;
      textarea.setRangeText(snippet, start, end, "end");
      dispatchEditorInput();
      textarea.focus();
    };

    const selectedLineRange = (start, end) => {
      const value = textarea.value;
      const blockStart = start <= 0 ? 0 : value.lastIndexOf("\n", start - 1) + 1;
      const blockEnd = end > start && value[end - 1] === "\n" ? end - 1 : end;
      return { blockStart, blockEnd };
    };

    const handleTabKey = (event) => {
      if (event.key !== "Tab" || event.shiftKey || event.altKey || event.ctrlKey || event.metaKey) return;
      event.preventDefault();

      const start = textarea.selectionStart || 0;
      const end = textarea.selectionEnd || start;
      const selection = textarea.value.slice(start, end);
      if (start !== end && selection.includes("\n")) {
        const { blockStart, blockEnd } = selectedLineRange(start, end);
        const block = textarea.value.slice(blockStart, blockEnd);
        const lineCount = block.split("\n").length;
        const indented = block.split("\n").map((line) => `\t${line}`).join("\n");
        textarea.setRangeText(indented, blockStart, blockEnd, "preserve");
        textarea.setSelectionRange(start + 1, end + lineCount);
      } else {
        textarea.setRangeText("\t", start, end, "end");
      }

      dispatchEditorInput({ preserveWhitespaceOnlyLines: true });
    };

    const filterEmojiItems = (query) => {
      const q = String(query || "").trim().replace(/^:/, "").toLowerCase();
      const matches = q === ""
        ? emojiItems
        : emojiItems.filter((item) => {
          return item.name.includes(q) || item.keywords.includes(q);
        });
      return matches.slice(0, 24);
    };

    const ensureEmojiPicker = () => {
      if (emojiPicker) return emojiPicker;

      emojiPicker = document.createElement("div");
      emojiPicker.id = "editor-emoji-picker";
      emojiPicker.className = "editor-emoji-picker";
      emojiPicker.hidden = true;
      emojiPicker.innerHTML = [
        '<label class="editor-emoji-search-label" for="editor-emoji-search">Emoji</label>',
        '<input id="editor-emoji-search" class="editor-emoji-search" type="search" autocomplete="off" spellcheck="false" placeholder="Search emoji">',
        '<div class="editor-emoji-grid" role="listbox" aria-label="Emoji results"></div>',
      ].join("");
      document.body.appendChild(emojiPicker);

      emojiSearch = emojiPicker.querySelector("#editor-emoji-search");
      emojiGrid = emojiPicker.querySelector(".editor-emoji-grid");
      emojiSearch.addEventListener("input", () => {
        emojiActiveIndex = 0;
        renderEmojiPicker(emojiSearch.value);
      });
      emojiSearch.addEventListener("keydown", (event) => {
        if (event.key === "ArrowDown") {
          event.preventDefault();
          moveEmojiSelection(1);
        } else if (event.key === "ArrowUp") {
          event.preventDefault();
          moveEmojiSelection(-1);
        } else if (event.key === "Enter") {
          event.preventDefault();
          acceptEmojiSelection();
        } else if (event.key === "Escape") {
          event.preventDefault();
          closeEmojiPicker({ restoreFocus: true });
        }
      });
      emojiGrid.addEventListener("mousemove", (event) => {
        const option = event.target.closest("[data-emoji-index]");
        if (!option) return;
        emojiActiveIndex = Number(option.dataset.emojiIndex || 0);
        renderEmojiPicker(emojiSearch.value);
      });
      emojiGrid.addEventListener("click", (event) => {
        const option = event.target.closest("[data-emoji-index]");
        if (!option) return;
        const item = emojiResults[Number(option.dataset.emojiIndex || 0)];
        if (item) insertEmojiItem(item);
      });
      return emojiPicker;
    };

    const renderEmojiPicker = (query = "") => {
      ensureEmojiPicker();
      emojiResults = filterEmojiItems(query);
      emojiActiveIndex = Math.min(Math.max(0, emojiActiveIndex), Math.max(0, emojiResults.length - 1));
      if (emojiResults.length === 0) {
        emojiGrid.innerHTML = '<p class="editor-emoji-empty">No matches.</p>';
        return;
      }
      emojiGrid.innerHTML = emojiResults.map((item, index) => {
        const active = index === emojiActiveIndex;
        const label = item.name.replace(/_/g, " ");
        return `<button type="button" class="editor-emoji-option${active ? " is-active" : ""}" role="option" aria-selected="${active ? "true" : "false"}" data-emoji-index="${index}" title="${escapeHTML(item.name)}">
          <span class="editor-emoji-glyph" aria-hidden="true">${item.glyph}</span>
          <span class="editor-emoji-name">${escapeHTML(label)}</span>
        </button>`;
      }).join("");
    };

    const positionEmojiPicker = (anchor) => {
      ensureEmojiPicker();
      const anchorRect = (anchor || textarea).getBoundingClientRect();
      const pickerWidth = emojiPicker.offsetWidth || Math.min(360, window.innerWidth - 24);
      const pickerHeight = emojiPicker.offsetHeight || 320;
      const preferredTop = anchor ? anchorRect.bottom + 8 : anchorRect.top + 12;
      const preferredLeft = anchor ? anchorRect.left : anchorRect.right - pickerWidth - 14;
      const top = Math.min(Math.max(12, preferredTop), Math.max(12, window.innerHeight - pickerHeight - 12));
      const left = Math.min(Math.max(12, preferredLeft), Math.max(12, window.innerWidth - pickerWidth - 12));
      emojiPicker.style.top = `${top}px`;
      emojiPicker.style.left = `${left}px`;
    };

    const openEmojiPicker = ({ query = "", replaceRange = null, anchor = null, focusSearch = true } = {}) => {
      ensureEmojiPicker();
      emojiReplaceRange = replaceRange;
      emojiActiveIndex = 0;
      emojiPicker.hidden = false;
      emojiSearch.value = query;
      renderEmojiPicker(query);
      positionEmojiPicker(anchor);
      if (focusSearch) {
        emojiSearch.focus({ preventScroll: true });
        emojiSearch.select();
      }
    };

    const closeEmojiPicker = ({ restoreFocus = false } = {}) => {
      if (!emojiPicker) return;
      emojiPicker.hidden = true;
      emojiReplaceRange = null;
      if (restoreFocus) textarea.focus({ preventScroll: true });
    };

    const moveEmojiSelection = (delta) => {
      if (emojiResults.length === 0) return;
      emojiActiveIndex = (emojiActiveIndex + delta + emojiResults.length) % emojiResults.length;
      renderEmojiPicker(emojiSearch ? emojiSearch.value : "");
    };

    const acceptEmojiSelection = () => {
      const item = emojiResults[emojiActiveIndex];
      if (item) insertEmojiItem(item);
    };

    const insertEmojiItem = (item) => {
      const start = emojiReplaceRange ? emojiReplaceRange.from : (textarea.selectionStart || 0);
      const end = emojiReplaceRange ? emojiReplaceRange.to : (textarea.selectionEnd || start);
      textarea.setRangeText(item.glyph, start, end, "end");
      dispatchEditorInput();
      closeEmojiPicker({ restoreFocus: true });
    };

    const emojiTriggerBeforeCursor = () => {
      const start = textarea.selectionStart || 0;
      const end = textarea.selectionEnd || start;
      if (start !== end) return null;
      const before = textarea.value.slice(0, start);
      const match = before.match(/(^|[\s([{>])(:[a-z0-9_+-]{1,24})$/i);
      if (!match) return null;
      const token = match[2];
      return { from: start - token.length, to: start, query: token.slice(1) };
    };

    const updateEmojiAutocomplete = () => {
      const trigger = emojiTriggerBeforeCursor();
      if (!trigger || trigger.query.length === 0) {
        if (emojiReplaceRange) closeEmojiPicker();
        return;
      }
      openEmojiPicker({
        query: trigger.query,
        replaceRange: { from: trigger.from, to: trigger.to },
        focusSearch: false,
      });
    };

    const handleEmojiAutocompleteKeydown = (event) => {
      if (!emojiPicker || emojiPicker.hidden || !emojiReplaceRange) return false;
      if (event.key === "ArrowDown") {
        event.preventDefault();
        moveEmojiSelection(1);
        return true;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        moveEmojiSelection(-1);
        return true;
      }
      if (event.key === "Enter") {
        event.preventDefault();
        acceptEmojiSelection();
        return true;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        closeEmojiPicker();
        return true;
      }
      return false;
    };

    const uploadImage = async (file) => {
      if (!file || !form.dataset.uploadUrl) return;
      const body = new FormData();
      body.append("file", file);
      const res = await fetch(form.dataset.uploadUrl, {
        method: "POST",
        headers: { "X-CSRF-Token": form.querySelector("[name='csrf_token']")?.value || "" },
        body,
      });
      if (!res.ok) return;
      const json = await res.json();
      const url = json.data ? json.data.url : json.url;
      if (url) insertAtSelection(`![${file.name}](${url})`);
      galleryLoaded = false;
      if (selectedPanel() === "images") void loadGallery();
    };

    const chooseImage = () => {
      const input = document.createElement("input");
      input.type = "file";
      input.accept = "image/*";
      input.addEventListener("change", () => uploadImage(input.files && input.files[0]));
      input.click();
    };

    const loadGallery = async () => {
      const grid = document.getElementById("editor-gallery-grid");
      if (!grid || !form.dataset.imagesUrl || galleryLoaded) return;
      grid.innerHTML = '<p class="editor-preview-empty">Loading images...</p>';
      try {
        const res = await fetch(form.dataset.imagesUrl, {
          method: "POST",
          headers: {
            "Accept": "application/json",
            "X-CSRF-Token": form.querySelector("[name='csrf_token']")?.value || "",
          },
        });
        if (!res.ok) throw new Error("image list failed");
        const json = await res.json();
        const images = (json.data && json.data.images) || json.images || [];
        galleryLoaded = true;
        if (!images.length) {
          grid.innerHTML = '<p class="editor-preview-empty">No uploaded images yet.</p>';
          return;
        }
        grid.innerHTML = images.map((img) => {
          const url = escapeHTML(img.url || img.URL || "");
          const name = escapeHTML(img.filename || img.Filename || url);
          return `<button type="button" class="editor-gallery-thumb" data-url="${url}" data-name="${name}">
            <img src="${url}" alt="">
            <span>${name}</span>
          </button>`;
        }).join("");
      } catch (_) {
        grid.innerHTML = '<p class="editor-preview-empty">Failed to load images.</p>';
      }
    };

    document.addEventListener("click", (event) => {
      if (
        emojiPicker &&
        !emojiPicker.hidden &&
        !emojiPicker.contains(event.target) &&
        !event.target.closest('[data-command="emoji"]')
      ) {
        closeEmojiPicker();
      }

      const segment = event.target.closest(".editor-segment[for]");
      if (segment && form.contains(segment)) {
        const target = document.getElementById(segment.getAttribute("for"));
        const none = document.getElementById("editor-panel-none");
        if (target && none && target.checked) {
          event.preventDefault();
          none.checked = true;
          none.dispatchEvent(new Event("change", { bubbles: true }));
          return;
        }
      }

      const toolbarButton = event.target.closest("[data-command]");
      if (toolbarButton && form.contains(toolbarButton)) {
        const command = toolbarButton.dataset.command || "";
        if (command === "emoji") {
          openEmojiPicker({ anchor: toolbarButton, focusSearch: true });
          return;
        }
        if (command === "image") {
          chooseImage();
          return;
        }
        const start = textarea.selectionStart || 0;
        const end = textarea.selectionEnd || start;
        insertAtSelection(commandSnippet(command, textarea.value.slice(start, end)));
        return;
      }

      const metadataButton = event.target.closest("[data-metadata-action]");
      if (metadataButton && form.contains(metadataButton)) {
        void requestMetadata(metadataButton.dataset.metadataAction || "preview");
        return;
      }

      const outlineItem = event.target.closest(".editor-outline-item");
      if (outlineItem) {
        const offset = Number(outlineItem.dataset.offset || 0);
        textarea.focus();
        textarea.setSelectionRange(offset, offset);
        return;
      }

      const galleryThumb = event.target.closest(".editor-gallery-thumb");
      if (galleryThumb) {
        insertAtSelection(`![${galleryThumb.dataset.name || "image"}](${galleryThumb.dataset.url || ""})`);
      }
    });

    document.querySelectorAll(".editor-panel-radio").forEach((radio) => {
      radio.addEventListener("change", () => {
        if (radio.checked && radio.value === "preview") updatePreview();
        if (radio.checked && radio.value === "metadata") void requestMetadata("preview");
        if (radio.checked && radio.value === "images") void loadGallery();
        if (radio.checked && radio.value === "outline") rebuildOutline();
      });
    });

    textarea.addEventListener("input", () => {
      if (!preserveWhitespaceOnlyLines) normalizeWhitespaceOnlyLines();
      scheduleEditorRender();
      if (selectedPanel() === "preview") updatePreview();
      if (selectedPanel() === "metadata") scheduleMetadataPreview();
      scheduleAutosave();
      updateEmojiAutocomplete();
    });
    textarea.addEventListener("keydown", (event) => {
      if (handleEmojiAutocompleteKeydown(event)) return;
      handleTabKey(event);
    });
    textarea.addEventListener("mousedown", focusBlankVisualRow);
    textarea.addEventListener("scroll", syncScroll);
    textarea.addEventListener("paste", (event) => {
      const items = event.clipboardData && event.clipboardData.items;
      if (!items) return;
      for (const item of items) {
        if (item.type && item.type.startsWith("image/")) {
          const file = item.getAsFile();
          if (file) {
            event.preventDefault();
            void uploadImage(file);
          }
          return;
        }
      }
    });
    textarea.addEventListener("drop", (event) => {
      const file = event.dataTransfer && event.dataTransfer.files && event.dataTransfer.files[0];
      if (file && file.type && file.type.startsWith("image/")) {
        event.preventDefault();
        void uploadImage(file);
      }
    });
    textarea.addEventListener("dragover", (event) => event.preventDefault());
    form.addEventListener("input", (event) => {
      if (event.target === textarea || event.target?.classList?.contains("editor-panel-radio")) return;
      if (event.target?.name === "excerpt") scheduleMetadataPreview();
      scheduleAutosave();
    });
    form.addEventListener("change", (event) => {
      if (event.target === textarea || event.target?.classList?.contains("editor-panel-radio")) return;
      scheduleAutosave();
    });
    window.addEventListener("online", () => {
      if (autosavePending) queueAutosaveTimer();
    });
    window.addEventListener("resize", () => {
      visualRowMapWidth = 0;
      scheduleEditorRender();
      if (emojiPicker && !emojiPicker.hidden) positionEmojiPicker();
    });

    normalizeWhitespaceOnlyLines();
    lastAutosaveFingerprint = autosaveFingerprint();
    setSaveStatus("saved", "Saved");
    renderEditorFrame();
    const initialPreview = document.getElementById("editor-preview-content");
    const initialPreviewIsPlaceholder = !!initialPreview?.querySelector(".editor-preview-empty");
    const initialPreviewHasHTML = !!initialPreview && !initialPreviewIsPlaceholder && initialPreview.innerHTML.trim() !== "";
    if (initialPreviewHasHTML) {
      void renderPreviewDiagrams(initialPreview);
    }
    if (selectedPanel() === "preview" && textarea.value.trim() !== "") {
      updatePreview();
    }
    if (document.getElementById("editor-excerpt-preview")) {
      void requestMetadata("preview");
    }
  });
})();
