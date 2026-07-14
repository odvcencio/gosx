// examples/gosx-docs/public/collab-client.js
// Collab Editor: hub sync + minimal markdown renderer + presence/cursors.
(function () {
  "use strict";

  var HUB_NAME = "collab";
  var EVENT_UPDATE = "doc:update";
  var EVENT_EDIT = "doc:edit";
  var EVENT_CURSOR = "cursor:update";
  var EVENT_PRESENCE_COUNT = "presence:count";
  var EVENT_PRESENCE_SELF = "presence:self";
  var EVENT_CURSOR_ROSTER = "cursor:roster";
  var EVENT_CURSOR_LEAVE = "cursor:leave";
  var DEBOUNCE_MS = 100;
  var CURSOR_THROTTLE_MS = 60;
  var CURSOR_STALE_MS = 15000;

  var textarea, preview, statusEl, versionEl, presenceEl, selfEl, statusDotEl, cursorLayer, shell;
  var currentVersion = 0;
  var editTimer = null;
  var suppressEvent = false;
  var ownClientId = null;

  // Remote caret markers, keyed by hub client ID.
  // { [id]: { el, name, color, offset, selEnd, lastSeen } }
  var remoteCursors = {};

  // Cached textarea metrics used to place caret markers without a real
  // caret-position API (plain <textarea> has none). Monospace font, so
  // column math is a flat multiply; row math uses the computed line-height.
  var metrics = { lineHeight: 22, charWidth: 8, paddingTop: 16, paddingLeft: 16 };

  // ── Bootstrap ──────────────────────────────────────────────────────────────

  function mount() {
    textarea    = document.getElementById("collab-source");
    preview     = document.getElementById("collab-preview");
    statusEl    = document.getElementById("collab-status");
    versionEl   = document.getElementById("collab-version");
    presenceEl  = document.getElementById("collab-presence");
    selfEl      = document.getElementById("collab-self");
    statusDotEl = document.getElementById("collab-status-dot");
    cursorLayer = document.getElementById("collab-cursors");
    shell       = document.querySelector(".collab");
    if (!textarea || !preview) return;
    currentVersion = Number(shell && shell.getAttribute("data-initial-version")) || 0;

    measureMetrics();

    textarea.addEventListener("input", onLocalInput);
    textarea.addEventListener("keyup", onCaretMove);
    textarea.addEventListener("mouseup", onCaretMove);
    textarea.addEventListener("scroll", repositionAllCursors);
    window.addEventListener("resize", onWindowResize);
    document.addEventListener("gosx:hub:event", onHubEvent);
    document.addEventListener("gosx:navigate", teardown, { once: true });
    socketTimer = setInterval(refreshSocketState, 1000);
    staleSweepTimer = setInterval(sweepStaleCursors, 4000);

    // Render whatever SSR seeded into the textarea.
    renderPreview(textarea.value);
    setStatus("connecting…");
  }

  // ── Local editing ──────────────────────────────────────────────────────────

  function onLocalInput() {
    if (suppressEvent) return;
    clearTimeout(editTimer);
    setStatus("pending");
    editTimer = setTimeout(sendEdit, DEBOUNCE_MS);
    renderPreview(textarea.value);
    // Local edits reshuffle line/row layout — keep remote markers honest.
    repositionAllCursors();
    scheduleCursorSend();
  }

  function sendEdit() {
    editTimer = null;
    if (!sendHub(EVENT_EDIT, { text: textarea.value, version: currentVersion })) {
      setStatus("offline · edit kept locally");
    }
  }

  // ── Local caret/selection broadcast ─────────────────────────────────────────

  function onCaretMove() {
    scheduleCursorSend();
  }

  var cursorThrottleTimer = null;
  var cursorLastSentAt = 0;

  function scheduleCursorSend() {
    var now = Date.now();
    var elapsed = now - cursorLastSentAt;
    if (elapsed >= CURSOR_THROTTLE_MS) {
      sendCursor();
      return;
    }
    if (cursorThrottleTimer) return;
    cursorThrottleTimer = setTimeout(function () {
      cursorThrottleTimer = null;
      sendCursor();
    }, CURSOR_THROTTLE_MS - elapsed);
  }

  function sendCursor() {
    cursorLastSentAt = Date.now();
    if (!textarea) return;
    sendHub(EVENT_CURSOR, { offset: textarea.selectionStart, selEnd: textarea.selectionEnd });
  }

  // ── Hub events ─────────────────────────────────────────────────────────────

  function onHubEvent(evt) {
    var detail = evt.detail;
    if (!detail || detail.hubName !== HUB_NAME) return;

    if (detail.event === "__welcome") {
      // Connected — flip status. The welcome payload carries our own client
      // ID, which we need to filter our own cursor broadcasts back out.
      if (detail.data && detail.data.clientId) ownClientId = detail.data.clientId;
      setStatus("connected");
      if (shell) shell.classList.add("collab--connected");
      return;
    }

    if (detail.event === EVENT_PRESENCE_COUNT) {
      updatePresenceCount(detail.data);
      return;
    }

    if (detail.event === EVENT_PRESENCE_SELF) {
      setSelfIdentity(detail.data);
      return;
    }

    if (detail.event === EVENT_CURSOR) {
      upsertCursor(detail.data);
      return;
    }

    if (detail.event === EVENT_CURSOR_ROSTER) {
      var peers = detail.data;
      if (Array.isArray(peers)) {
        for (var p = 0; p < peers.length; p++) upsertCursor(peers[p]);
      }
      return;
    }

    if (detail.event === EVENT_CURSOR_LEAVE) {
      if (detail.data && detail.data.id) removeCursor(detail.data.id);
      return;
    }

    if (detail.event !== EVENT_UPDATE) return;

    var state = detail.data;
    if (!state || typeof state.text !== "string") return;

    // Already in sync — just update our version tracker.
    if (state.version <= currentVersion && state.text === textarea.value) {
      currentVersion = state.version;
      if (versionEl) versionEl.textContent = String(currentVersion);
      setStatus("synced");
      return;
    }

    applyRemoteState(state);
  }

  function applyRemoteState(state) {
    clearTimeout(editTimer);
    editTimer = null;
    currentVersion = state.version;
    if (versionEl) versionEl.textContent = String(currentVersion);
    setStatus("synced");
    if (state.text === textarea.value) {
      repositionAllCursors();
      return;
    }

    var previousText = textarea.value;
    // A remote client's edit actually changed our content — this is the
    // moment convergence becomes visible: flash the dot and highlight
    // whichever preview blocks the diff touched.
    flashStatusDot();

    // Preserve cursor position best-effort.
    var start = textarea.selectionStart;
    var end   = textarea.selectionEnd;
    suppressEvent = true;
    textarea.value = state.text;
    suppressEvent = false;
    try { textarea.setSelectionRange(start, end); } catch (_) {}
    renderPreview(state.text);
    flashChangedLines(computeChangedLineRange(previousText, state.text));
    repositionAllCursors();
  }

  // ── Remote-convergence flash ─────────────────────────────────────────────────

  var statusDotFlashTimer = null;

  function flashStatusDot() {
    if (!statusDotEl) return;
    statusDotEl.classList.remove("collab__status-dot--flash");
    void statusDotEl.offsetWidth; // restart the animation if retriggered quickly
    statusDotEl.classList.add("collab__status-dot--flash");
    clearTimeout(statusDotFlashTimer);
    statusDotFlashTimer = setTimeout(function () {
      statusDotEl.classList.remove("collab__status-dot--flash");
    }, 700);
  }

  // computeChangedLineRange does a cheap two-pointer diff: trim the common
  // prefix and common suffix of the old/new line arrays, and whatever is
  // left in the new text is "the changed region". Not a real diff (no
  // reordering/move detection) — good enough to decide which preview blocks
  // to flash.
  function computeChangedLineRange(oldText, newText) {
    var oldLines = oldText.split("\n");
    var newLines = newText.split("\n");
    var i = 0;
    var prefixMax = Math.min(oldLines.length, newLines.length);
    while (i < prefixMax && oldLines[i] === newLines[i]) i++;

    var oldEnd = oldLines.length - 1;
    var newEnd = newLines.length - 1;
    while (oldEnd >= i && newEnd >= i && oldLines[oldEnd] === newLines[newEnd]) {
      oldEnd--;
      newEnd--;
    }

    if (newEnd < i) {
      // Pure deletion collapsed onto the boundary — flash the line the
      // deletion happened at/near so something is still visible.
      var anchor = Math.max(0, Math.min(i, newLines.length - 1));
      return { start: anchor, end: anchor };
    }
    return { start: i, end: newEnd };
  }

  function flashChangedLines(range) {
    if (!preview || !range) return;
    var blocks = preview.querySelectorAll(".collab__block");
    for (var b = 0; b < blocks.length; b++) {
      var el = blocks[b];
      var s = Number(el.getAttribute("data-line-start"));
      var e = Number(el.getAttribute("data-line-end"));
      if (isNaN(s) || isNaN(e) || e < range.start || s > range.end) continue;
      triggerBlockFlash(el);
    }
  }

  function triggerBlockFlash(el) {
    el.classList.remove("collab__block--flash");
    void el.offsetWidth; // restart the animation if retriggered quickly
    el.classList.add("collab__block--flash");
    setTimeout(function () {
      el.classList.remove("collab__block--flash");
    }, 1000);
  }

  // ── Presence: connected-editor count + self identity ────────────────────────

  function updatePresenceCount(data) {
    var count = data && typeof data.count === "number" ? data.count : null;
    if (count === null || !presenceEl) return;
    presenceEl.textContent = count + (count === 1 ? " editor online" : " editors online");
    if (shell) shell.classList.toggle("collab--crowded", count > 1);
  }

  function setSelfIdentity(data) {
    if (!data) return;
    if (data.id) ownClientId = data.id;
    if (!selfEl) return;
    selfEl.textContent = "you: " + (data.name || "guest");
    selfEl.style.setProperty("--collab-self-color", data.color || "");
  }

  // ── Remote cursors ───────────────────────────────────────────────────────────

  function upsertCursor(evt) {
    if (!evt || !evt.id || evt.id === ownClientId || !cursorLayer) return;
    var entry = remoteCursors[evt.id];
    if (!entry) {
      var el = document.createElement("div");
      el.className = "collab__cursor";
      cursorLayer.appendChild(el);
      entry = remoteCursors[evt.id] = { el: el };
    }
    entry.name = evt.name || "guest";
    entry.color = evt.color || "";
    entry.offset = typeof evt.offset === "number" ? evt.offset : 0;
    entry.selEnd = typeof evt.selEnd === "number" ? evt.selEnd : entry.offset;
    entry.lastSeen = Date.now();
    entry.el.setAttribute("data-label", entry.name);
    entry.el.style.setProperty("--cursor-color", entry.color);
    positionCursor(entry);
  }

  function removeCursor(id) {
    var entry = remoteCursors[id];
    if (!entry) return;
    if (entry.el && entry.el.parentNode) entry.el.parentNode.removeChild(entry.el);
    delete remoteCursors[id];
  }

  function positionCursor(entry) {
    if (!textarea || !entry || !entry.el) return;
    var pos = offsetToRowCol(textarea.value, entry.offset);
    var top = metrics.paddingTop + pos.row * metrics.lineHeight - textarea.scrollTop;
    var left = metrics.paddingLeft + pos.col * metrics.charWidth - textarea.scrollLeft;
    entry.el.style.top = top + "px";
    entry.el.style.left = left + "px";
    // Hide markers scrolled outside the visible viewport instead of letting
    // them clip messily against overflow:hidden.
    var within = top > -metrics.lineHeight && top < textarea.clientHeight;
    entry.el.style.opacity = within ? "" : "0";
  }

  function repositionAllCursors() {
    for (var id in remoteCursors) {
      if (Object.prototype.hasOwnProperty.call(remoteCursors, id)) {
        positionCursor(remoteCursors[id]);
      }
    }
  }

  function offsetToRowCol(text, offset) {
    var clamped = Math.max(0, Math.min(offset || 0, text.length));
    var upto = text.slice(0, clamped);
    var lines = upto.split("\n");
    return { row: lines.length - 1, col: lines[lines.length - 1].length };
  }

  var staleSweepTimer = null;

  function sweepStaleCursors() {
    var now = Date.now();
    for (var id in remoteCursors) {
      if (!Object.prototype.hasOwnProperty.call(remoteCursors, id)) continue;
      if (now - (remoteCursors[id].lastSeen || 0) > CURSOR_STALE_MS) removeCursor(id);
    }
  }

  var resizeTimer = null;

  function onWindowResize() {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(function () {
      measureMetrics();
      repositionAllCursors();
    }, 120);
  }

  function measureMetrics() {
    if (!textarea) return;
    var cs = getComputedStyle(textarea);
    var lh = parseFloat(cs.lineHeight);
    metrics.lineHeight = isNaN(lh) ? parseFloat(cs.fontSize) * 1.2 : lh;
    metrics.paddingTop = parseFloat(cs.paddingTop) || 0;
    metrics.paddingLeft = parseFloat(cs.paddingLeft) || 0;

    var canvas = measureMetrics._canvas || (measureMetrics._canvas = document.createElement("canvas"));
    var ctx = canvas.getContext && canvas.getContext("2d");
    if (ctx) {
      ctx.font = cs.fontSize + " " + cs.fontFamily;
      var w = ctx.measureText("0").width;
      if (w) metrics.charWidth = w;
    }
  }

  // ── Hub send ───────────────────────────────────────────────────────────────

  function sendHub(event, data) {
    var hubs = window.__gosx && window.__gosx.hubs;
    if (!hubs) return false;
    var sent = false;
    hubs.forEach(function (record) {
      if (
        record &&
        record.entry &&
        record.entry.name === HUB_NAME &&
        record.socket &&
        record.socket.readyState === 1
      ) {
        record.socket.send(JSON.stringify({ event: event, data: data }));
        sent = true;
      }
    });
    return sent;
  }

  function setStatus(text) {
    if (statusEl) statusEl.textContent = text;
  }

  var socketTimer = null;
  function refreshSocketState() {
    var hubs = window.__gosx && window.__gosx.hubs;
    var open = false, connecting = false;
    if (hubs) hubs.forEach(function (record) {
      if (!record || !record.entry || record.entry.name !== HUB_NAME || !record.socket) return;
      open = open || record.socket.readyState === 1;
      connecting = connecting || record.socket.readyState === 0;
    });
    if (!open && !editTimer) setStatus(connecting ? "reconnecting…" : "offline");
    if (shell) shell.classList.toggle("collab--connected", open);
  }

  function teardown() {
    clearTimeout(editTimer);
    clearInterval(socketTimer);
    clearInterval(staleSweepTimer);
    clearTimeout(cursorThrottleTimer);
    clearTimeout(statusDotFlashTimer);
    clearTimeout(resizeTimer);
    window.removeEventListener("resize", onWindowResize);
    document.removeEventListener("gosx:hub:event", onHubEvent);
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

  // blockAttrs tags a top-level preview element with the source line range
  // it was rendered from, so a later remote update can cheaply diff old vs.
  // new document text (see computeChangedLineRange) and flash exactly the
  // blocks that changed.
  function blockAttrs(startLine, endLine) {
    return ' class="collab__block" data-line-start="' + startLine + '" data-line-end="' + endLine + '"';
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
        var codeStart = i;
        var lang = escapeHtml(line.slice(3).trim());
        var codeBuf = [];
        i++;
        while (i < lines.length && !/^```/.test(lines[i])) {
          codeBuf.push(escapeHtml(lines[i]));
          i++;
        }
        i++; // consume closing ```
        var langAttr = lang ? ' class="language-' + lang + '"' : "";
        out.push("<pre" + blockAttrs(codeStart, i - 1) + "><code" + langAttr + ">" + codeBuf.join("\n") + "</code></pre>");
        continue;
      }

      // ── Heading # / ## / ### / #### ───────────────────────────────────────
      var hm = line.match(/^(#{1,4})\s+(.*)/);
      if (hm) {
        var level = hm[1].length;
        out.push("<h" + level + blockAttrs(i, i) + ">" + renderInline(hm[2]) + "</h" + level + ">");
        i++;
        continue;
      }

      // ── Blockquote > ──────────────────────────────────────────────────────
      if (/^>\s?/.test(line)) {
        var quoteStart = i;
        var qBuf = [];
        while (i < lines.length && /^>\s?/.test(lines[i])) {
          qBuf.push(lines[i].replace(/^>\s?/, ""));
          i++;
        }
        out.push("<blockquote" + blockAttrs(quoteStart, i - 1) + "><p>" + renderInline(qBuf.join(" ")) + "</p></blockquote>");
        continue;
      }

      // ── Unordered list - / * / + ──────────────────────────────────────────
      if (/^[-*+]\s/.test(line)) {
        var ulStart = i;
        var listBuf = [];
        while (i < lines.length && /^[-*+]\s/.test(lines[i])) {
          listBuf.push("<li>" + renderInline(lines[i].replace(/^[-*+]\s/, "")) + "</li>");
          i++;
        }
        out.push("<ul" + blockAttrs(ulStart, i - 1) + ">" + listBuf.join("") + "</ul>");
        continue;
      }

      // ── Ordered list 1. ───────────────────────────────────────────────────
      if (/^\d+\.\s/.test(line)) {
        var olStart = i;
        var olBuf = [];
        while (i < lines.length && /^\d+\.\s/.test(lines[i])) {
          olBuf.push("<li>" + renderInline(lines[i].replace(/^\d+\.\s/, "")) + "</li>");
          i++;
        }
        out.push("<ol" + blockAttrs(olStart, i - 1) + ">" + olBuf.join("") + "</ol>");
        continue;
      }

      // ── Horizontal rule --- / *** ──────────────────────────────────────────
      if (/^(\-{3,}|\*{3,})$/.test(line.trim())) {
        out.push("<hr" + blockAttrs(i, i) + ">");
        i++;
        continue;
      }

      // ── Blank line — paragraph break ─────────────────────────────────────
      if (line.trim() === "") {
        i++;
        continue;
      }

      // ── Paragraph — accumulate until blank line ───────────────────────────
      var paraStart = i;
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
        out.push("<p" + blockAttrs(paraStart, i - 1) + ">" + renderInline(paraBuf.join(" ")) + "</p>");
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
