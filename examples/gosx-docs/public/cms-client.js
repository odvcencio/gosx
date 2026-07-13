// examples/gosx-docs/public/cms-client.js
// CMS Editor: live typing preview, a working block palette, an honest
// publish round trip against the "publish" server Action, and an
// unsaved-changes indicator. Progressive enhancement — with this script
// absent, the form still works as a plain full-page POST (see page.gsx).
(function () {
  "use strict";

  var BLOCK_DEFS = {
    hero: {
      label: "Hero",
      fields: [
        { role: "title", tag: "input", label: "Title", placeholder: "Page title", required: true, maxlength: 120 },
        { role: "subtitle", tag: "input", label: "Subtitle", placeholder: "Supporting subtitle", maxlength: 240 },
      ],
    },
    feature: {
      label: "Feature",
      fields: [
        { role: "title", tag: "input", label: "Title", placeholder: "Feature title", required: true, maxlength: 120 },
        { role: "body", tag: "textarea", label: "Body", placeholder: "Feature description", maxlength: 1000 },
      ],
    },
    quote: {
      label: "Quote",
      fields: [
        { role: "text", tag: "textarea", label: "Quote", placeholder: "The quote text", required: true, maxlength: 500 },
        { role: "author", tag: "input", label: "Author", placeholder: "Author name", maxlength: 120 },
      ],
    },
  };

  var PULSE_MS = 700;
  var PUBLISHED_LABEL_MS = 1600;
  var FEEDBACK_MS = 4000;

  var form, blockList, previewCanvas, blockCountInput, blockTotalEl;
  var statusEl, statusDot, statusText, unsavedBadge, publishBtn, publishLabel, feedbackEl, announcer, csrfInput;
  var nextIndex = 0;
  var submitting = false;
  var baselineSnapshot = "";
  // Draft snapshot captured at the moment a publish was submitted. On success
  // the baseline resets to THIS snapshot, not the live draft — anything typed
  // during the fetch round trip was not published and must keep the
  // "Unsaved changes" badge visible.
  var submittedSnapshot = null;
  var feedbackTimer = null;
  var labelTimer = null;

  function mount() {
    form = document.getElementById("cms-content-form");
    if (!form) return;

    blockList = document.getElementById("cms-block-list");
    previewCanvas = document.getElementById("cms-preview-canvas");
    blockCountInput = document.getElementById("cms-block-count");
    blockTotalEl = document.getElementById("cms-block-total");
    statusEl = document.getElementById("cms-status");
    statusDot = document.getElementById("cms-status-dot");
    statusText = document.getElementById("cms-status-text");
    unsavedBadge = document.getElementById("cms-unsaved-badge");
    publishBtn = document.getElementById("cms-publish-btn");
    publishLabel = document.getElementById("cms-publish-label");
    feedbackEl = document.getElementById("cms-publish-feedback");
    announcer = document.getElementById("cms-announcer");
    csrfInput = form.elements["csrf_token"];

    if (!blockList || !previewCanvas || !blockCountInput) return;

    nextIndex = blockList.querySelectorAll("[data-block-index]").length;

    form.addEventListener("input", onDraftInput);
    form.addEventListener("submit", onSubmit);

    var palette = document.getElementById("cms-palette-list");
    if (palette) palette.addEventListener("click", onPaletteClick);

    baselineSnapshot = submittedSnapshot !== null ? submittedSnapshot : serializeDraft();
    submittedSnapshot = null;
    updateUnsavedState();
  }

  // ── Live typing preview (#1) ────────────────────────────────────────────

  function onDraftInput(evt) {
    var field = evt.target;
    if (!field || !field.getAttribute) return;
    var role = field.getAttribute("data-preview-field");
    if (!role) return;
    var wrapper = field.closest("[data-block-index]");
    if (!wrapper) return;
    var index = wrapper.getAttribute("data-block-index");
    var target = previewCanvas.querySelector(
      '[data-block-index="' + index + '"] [data-preview-field="' + role + '"]'
    );
    if (target) target.textContent = field.value;
    if (field.hasAttribute("aria-invalid")) field.removeAttribute("aria-invalid");
    updateUnsavedState();
  }

  // ── Working palette (#2) ────────────────────────────────────────────────

  function onPaletteClick(evt) {
    var card = evt.target.closest("[data-block-kind]");
    if (!card || card.disabled) return;
    addBlock(card.getAttribute("data-block-kind"));
  }

  function addBlock(kind) {
    var def = BLOCK_DEFS[kind];
    if (!def) return;

    var index = nextIndex++;
    var editorBlock = buildEditorBlock(kind, def, index);
    var previewBlock = buildPreviewBlock(kind, index);
    blockList.appendChild(editorBlock);
    previewCanvas.appendChild(previewBlock);
    blockCountInput.value = String(nextIndex);
    if (blockTotalEl) blockTotalEl.textContent = String(nextIndex);

    requestAnimationFrame(function () {
      editorBlock.classList.add("cms-editor-block--entering");
      previewBlock.classList.add("cms-preview-block--entering");
    });
    editorBlock.addEventListener("animationend", function () {
      editorBlock.classList.remove("cms-editor-block--entering");
    });
    previewBlock.addEventListener("animationend", function () {
      previewBlock.classList.remove("cms-preview-block--entering");
    });

    var firstField = editorBlock.querySelector(".cms-input");
    if (firstField) firstField.focus();

    announce(def.label + " block added");
    updateUnsavedState();
  }

  function buildEditorBlock(kind, def, index) {
    var article = document.createElement("article");
    article.className = "cms-editor-block cms-editor-block--" + kind;
    article.setAttribute("aria-label", kind + " block");
    article.setAttribute("data-block-index", String(index));
    article.setAttribute("data-block-kind", kind);

    var header = document.createElement("div");
    header.className = "cms-editor-block__header";
    var type = document.createElement("span");
    type.className = "cms-editor-block__type";
    type.textContent = kind;
    header.appendChild(type);
    article.appendChild(header);

    var fields = document.createElement("div");
    fields.className = "cms-editor-block__fields";

    var kindInput = document.createElement("input");
    kindInput.type = "hidden";
    kindInput.name = "block_" + index + "_kind";
    kindInput.value = kind;
    fields.appendChild(kindInput);

    def.fields.forEach(function (spec) {
      var label = document.createElement("label");
      label.className = "cms-field";
      var span = document.createElement("span");
      span.textContent = spec.label;
      label.appendChild(span);

      var input = document.createElement(spec.tag);
      input.className = spec.tag === "textarea" ? "cms-input cms-input--textarea" : "cms-input";
      input.setAttribute("data-preview-field", spec.role);
      if (spec.tag === "input") input.type = "text";
      input.name = "block_" + index + "_" + spec.role;
      input.placeholder = spec.placeholder;
      if (spec.required) input.required = true;
      if (spec.maxlength) input.maxLength = spec.maxlength;
      label.appendChild(input);
      fields.appendChild(label);
    });

    article.appendChild(fields);
    return article;
  }

  function buildPreviewBlock(kind, index) {
    var wrap = document.createElement("div");
    wrap.className = "cms-preview-block cms-preview-block--" + kind;
    wrap.setAttribute("data-block-index", String(index));

    if (kind === "hero") {
      var hero = document.createElement("div");
      hero.className = "cms-preview-hero";
      hero.appendChild(makeEl("h1", "title"));
      var divider = document.createElement("span");
      divider.className = "cms-preview-hero__divider";
      hero.appendChild(divider);
      hero.appendChild(makeEl("p", "subtitle"));
      wrap.appendChild(hero);
    } else if (kind === "feature") {
      var feature = document.createElement("div");
      feature.className = "cms-preview-feature";
      feature.appendChild(makeEl("h3", "title"));
      feature.appendChild(makeEl("p", "body"));
      wrap.appendChild(feature);
    } else if (kind === "quote") {
      var figure = document.createElement("figure");
      figure.className = "cms-preview-quote";
      figure.appendChild(makeEl("blockquote", "text"));
      figure.appendChild(document.createElement("hr"));
      var figcaption = document.createElement("figcaption");
      figcaption.appendChild(document.createTextNode("— "));
      figcaption.appendChild(makeEl("span", "author"));
      figure.appendChild(figcaption);
      wrap.appendChild(figure);
    }
    return wrap;
  }

  function makeEl(tag, role) {
    var el = document.createElement(tag);
    el.setAttribute("data-preview-field", role);
    return el;
  }

  function announce(message) {
    if (announcer) announcer.textContent = message;
  }

  // ── Unsaved-changes indicator (#5) ──────────────────────────────────────

  function serializeDraft() {
    var parts = [];
    var blocks = blockList.querySelectorAll("[data-block-kind]");
    for (var i = 0; i < blocks.length; i++) {
      var block = blocks[i];
      var kind = block.getAttribute("data-block-kind");
      var values = [kind];
      var fields = block.querySelectorAll("[data-preview-field]");
      for (var j = 0; j < fields.length; j++) {
        values.push(fields[j].getAttribute("data-preview-field") + "=" + fields[j].value);
      }
      parts.push(values.join("|"));
    }
    return parts.join("~~");
  }

  function updateUnsavedState() {
    var dirty = serializeDraft() !== baselineSnapshot;
    if (unsavedBadge) unsavedBadge.hidden = !dirty;
  }

  // ── Publish (#3, #4) ─────────────────────────────────────────────────────

  function onSubmit(evt) {
    if (submitting) {
      evt.preventDefault();
      return;
    }
    // No compile-time way to know fetch will succeed; if it throws before
    // reaching the network (e.g. no FormData support) the browser's native
    // submit — the no-JS fallback — still runs because we only
    // preventDefault() once we're committed to the fetch path below.
    if (typeof window.fetch !== "function" || typeof FormData !== "function") return;
    evt.preventDefault();

    submitting = true;
    submittedSnapshot = serializeDraft();
    setButtonBusy(true);
    setFeedback("Publishing…", "");

    var body = new URLSearchParams(new FormData(form));
    fetch(form.action, {
      method: (form.method || "POST").toUpperCase(),
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Accept: "application/json",
        "X-CSRF-Token": csrfInput ? csrfInput.value : "",
      },
      credentials: "same-origin",
      body: body,
    })
      .then(function (resp) {
        return resp
          .json()
          .catch(function () {
            return {};
          })
          .then(function (result) {
            return { ok: resp.ok, status: resp.status, result: result || {} };
          });
      })
      .then(function (payload) {
        submitting = false;
        setButtonBusy(false);
        if (payload.ok && payload.result.ok) {
          handleSuccess(payload.result);
        } else {
          handleFailure(payload.result, payload.status);
        }
      })
      .catch(function () {
        submitting = false;
        setButtonBusy(false);
        setFeedback("Network error — the draft was not published.", "error");
      });
  }

  function setButtonBusy(busy) {
    if (publishBtn) publishBtn.disabled = busy;
  }

  function handleSuccess(result) {
    var data = result.data || {};
    var count = typeof data.count === "number" ? data.count : blockList.querySelectorAll("[data-block-kind]").length;
    var at = data.at || "";
    var byKind = data.byKind || {};

    if (statusText) {
      var summary = "Published " + count + " blocks";
      var parts = [];
      ["hero", "feature", "quote"].forEach(function (kind) {
        if (byKind[kind]) parts.push(byKind[kind] + " " + kind);
      });
      if (parts.length) summary += " (" + parts.join(", ") + ")";
      if (at) summary += " · " + at;
      statusText.textContent = summary;
    }
    if (statusEl) statusEl.classList.add("cms-header__status--published");
    if (statusEl) statusEl.classList.remove("cms-header__status--error");

    pulse(statusDot, "cms-header__status-dot--pulse");
    pulse(publishBtn, "cms-btn--pulse");
    swapLabel("Published ✓");

    baselineSnapshot = serializeDraft();
    updateUnsavedState();
    setFeedback(result.message || "Published successfully.", "success");
    announce(result.message || "Published successfully.");
  }

  function handleFailure(result, status) {
    var message = result.message || (status === 429 ? "Slow down — try again in a moment." : "Publish failed.");
    setFeedback(message, "error");
    announce(message);
    if (statusEl) statusEl.classList.add("cms-header__status--error");

    var fieldErrors = result.fieldErrors || {};
    Object.keys(fieldErrors).forEach(function (name) {
      var field = form.elements[name];
      if (field && field.setAttribute) field.setAttribute("aria-invalid", "true");
    });
  }

  function pulse(el, className) {
    if (!el) return;
    el.classList.remove(className);
    // Force reflow so the animation restarts if it was just applied.
    void el.offsetWidth;
    el.classList.add(className);
    setTimeout(function () {
      el.classList.remove(className);
    }, PULSE_MS);
  }

  function swapLabel(text) {
    if (!publishLabel) return;
    var original = "Publish changes";
    publishLabel.textContent = text;
    clearTimeout(labelTimer);
    labelTimer = setTimeout(function () {
      publishLabel.textContent = original;
    }, PUBLISHED_LABEL_MS);
  }

  function setFeedback(message, kind) {
    if (!feedbackEl) return;
    feedbackEl.textContent = message;
    feedbackEl.classList.remove("cms-statusbar__hint--success", "cms-statusbar__hint--error");
    if (kind === "success") feedbackEl.classList.add("cms-statusbar__hint--success");
    if (kind === "error") feedbackEl.classList.add("cms-statusbar__hint--error");
    feedbackEl.classList.add("cms-statusbar__hint--visible");
    clearTimeout(feedbackTimer);
    if (kind !== "") {
      feedbackTimer = setTimeout(function () {
        feedbackEl.classList.remove("cms-statusbar__hint--visible");
      }, FEEDBACK_MS);
    }
  }

  // ── Init ─────────────────────────────────────────────────────────────────

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", mount);
  } else {
    mount();
  }
})();
