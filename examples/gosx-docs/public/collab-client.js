// examples/gosx-docs/public/collab-client.js
// Collab Editor: hub sync + minimal markdown renderer.
(function () {
  "use strict";

  var HUB_NAME = "collab";
  var EVENT_UPDATE = "doc:update";
  var EVENT_EDIT = "doc:edit";
  var DEBOUNCE_MS = 100;

  var textarea, preview, statusEl, shell;
  var currentVersion = 0;
  var editTimer = null;
  var suppressEvent = false;

  // ── Bootstrap ──────────────────────────────────────────────────────────────

  function mount() {
    textarea  = document.getElementById("collab-source");
    preview   = document.getElementById("collab-preview");
    statusEl  = document.getElementById("collab-status");
    shell     = document.querySelector(".collab");
    if (!textarea || !preview) return;

    textarea.addEventListener("input", onLocalInput);
    document.addEventListener("gosx:hub:event", onHubEvent);

    // Render whatever SSR seeded into the textarea.
    renderPreview(textarea.value);
    setStatus("connecting…");
  }

  // ── Local editing ──────────────────────────────────────────────────────────

  function onLocalInput() {
    if (suppressEvent) return;
    clearTimeout(editTimer);
    editTimer = setTimeout(sendEdit, DEBOUNCE_MS);
    renderPreview(textarea.value);
  }

  function sendEdit() {
    sendHub(EVENT_EDIT, { text: textarea.value, version: currentVersion });
  }

  // ── Hub events ─────────────────────────────────────────────────────────────

  function onHubEvent(evt) {
    var detail = evt.detail;
    if (!detail || detail.hubName !== HUB_NAME) return;

    if (detail.event === "__welcome") {
      // Connected — flip status.
      setStatus("connected");
      if (shell) shell.classList.add("collab--connected");
      return;
    }

    if (detail.event !== EVENT_UPDATE) return;

    var state = detail.data;
    if (!state || typeof state.text !== "string") return;

    // Already in sync — just update our version tracker.
    if (state.version <= currentVersion && state.text === textarea.value) {
      currentVersion = state.version;
      setStatus("synced");
      return;
    }

    applyRemoteState(state);
  }

  function applyRemoteState(state) {
    currentVersion = state.version;
    setStatus("synced");
    if (state.text === textarea.value) return;

    // Preserve cursor position best-effort.
    var start = textarea.selectionStart;
    var end   = textarea.selectionEnd;
    suppressEvent = true;
    textarea.value = state.text;
    suppressEvent = false;
    try { textarea.setSelectionRange(start, end); } catch (_) {}
    renderPreview(state.text);
  }

  // ── Hub send ───────────────────────────────────────────────────────────────

  function sendHub(event, data) {
    var hubs = window.__gosx && window.__gosx.hubs;
    if (!hubs) return;
    hubs.forEach(function (record) {
      if (
        record &&
        record.entry &&
        record.entry.name === HUB_NAME &&
        record.socket &&
        record.socket.readyState === 1
      ) {
        record.socket.send(JSON.stringify({ event: event, data: data }));
      }
    });
  }

  function setStatus(text) {
    if (statusEl) statusEl.textContent = text;
  }

  // ── Markdown renderer ──────────────────────────────────────────────────────

  function renderPreview(source) {
    if (!preview) return;
    preview.innerHTML = renderMarkdown(source || "");
  }

  function escapeHtml(s) {
    return s
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  }

  // Inline markdown: **bold**, _italic_, `code`, [text](url)
  function renderInline(text) {
    var s = escapeHtml(text);
    // Bold: **...**
    s = s.replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>");
    // Italic: _..._  (avoid matching inside words)
    s = s.replace(/(^|[\s(>])_([^_\n]+)_(?=[\s).!?,;]|$)/g, "$1<em>$2</em>");
    // Inline code: `...`
    s = s.replace(/`([^`\n]+)`/g, function (_, code) {
      return "<code>" + code + "</code>"; // already HTML-escaped above
    });
    // Links: [text](url)
    s = s.replace(/\[([^\]\n]+)\]\(([^)\n]+)\)/g, function (_, linkText, url) {
      // Sanitize: only allow http/https/mailto hrefs.
      var safe = /^(https?:|mailto:)/i.test(url) ? url : "#";
      return '<a href="' + escapeHtml(safe) + '" rel="noopener">' + linkText + "</a>";
    });
    return s;
  }

  function renderMarkdown(src) {
    var out = [];
    var lines = src.split("\n");
    var i = 0;

    while (i < lines.length) {
      var line = lines[i];

      // ── Fenced code block ````lang ... ``` ────────────────────────────────
      if (/^```/.test(line)) {
        var lang = escapeHtml(line.slice(3).trim());
        var codeBuf = [];
        i++;
        while (i < lines.length && !/^```/.test(lines[i])) {
          codeBuf.push(escapeHtml(lines[i]));
          i++;
        }
        i++; // consume closing ```
        var langAttr = lang ? ' class="language-' + lang + '"' : "";
        out.push("<pre><code" + langAttr + ">" + codeBuf.join("\n") + "</code></pre>");
        continue;
      }

      // ── Heading # / ## / ### / #### ───────────────────────────────────────
      var hm = line.match(/^(#{1,4})\s+(.*)/);
      if (hm) {
        var level = hm[1].length;
        out.push("<h" + level + ">" + renderInline(hm[2]) + "</h" + level + ">");
        i++;
        continue;
      }

      // ── Blockquote > ──────────────────────────────────────────────────────
      if (/^>\s?/.test(line)) {
        var qBuf = [];
        while (i < lines.length && /^>\s?/.test(lines[i])) {
          qBuf.push(lines[i].replace(/^>\s?/, ""));
          i++;
        }
        out.push("<blockquote><p>" + renderInline(qBuf.join(" ")) + "</p></blockquote>");
        continue;
      }

      // ── Unordered list - / * / + ──────────────────────────────────────────
      if (/^[-*+]\s/.test(line)) {
        var listBuf = [];
        while (i < lines.length && /^[-*+]\s/.test(lines[i])) {
          listBuf.push("<li>" + renderInline(lines[i].replace(/^[-*+]\s/, "")) + "</li>");
          i++;
        }
        out.push("<ul>" + listBuf.join("") + "</ul>");
        continue;
      }

      // ── Ordered list 1. ───────────────────────────────────────────────────
      if (/^\d+\.\s/.test(line)) {
        var olBuf = [];
        while (i < lines.length && /^\d+\.\s/.test(lines[i])) {
          olBuf.push("<li>" + renderInline(lines[i].replace(/^\d+\.\s/, "")) + "</li>");
          i++;
        }
        out.push("<ol>" + olBuf.join("") + "</ol>");
        continue;
      }

      // ── Horizontal rule --- / *** ──────────────────────────────────────────
      if (/^(\-{3,}|\*{3,})$/.test(line.trim())) {
        out.push("<hr>");
        i++;
        continue;
      }

      // ── Blank line — paragraph break ─────────────────────────────────────
      if (line.trim() === "") {
        i++;
        continue;
      }

      // ── Paragraph — accumulate until blank line ───────────────────────────
      var paraBuf = [];
      while (
        i < lines.length &&
        lines[i].trim() !== "" &&
        !/^(#{1,4}\s|```|[-*+]\s|\d+\.\s|>\s?)/.test(lines[i]) &&
        !/^(\-{3,}|\*{3,})$/.test(lines[i].trim())
      ) {
        paraBuf.push(lines[i]);
        i++;
      }
      if (paraBuf.length > 0) {
        out.push("<p>" + renderInline(paraBuf.join(" ")) + "</p>");
      }
    }

    return out.join("\n");
  }

  // ── Init ───────────────────────────────────────────────────────────────────

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", mount);
  } else {
    mount();
  }
})();
