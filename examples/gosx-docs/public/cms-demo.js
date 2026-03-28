(function() {
  "use strict";

  const ROOT_SELECTOR = "[data-cms-demo]";
  let sequence = 0;

  const paletteTemplates = {
    hero: function() {
      return {
        id: nextID("hero"),
        type: "hero",
        eyebrow: "Feature launch",
        title: "A new landing section is ready to lead the story.",
        body: "Use the hero block to set direction, place the main message, and keep the publish moment attached to the routed GoSX form.",
        cta: "Publish this story",
      };
    },
    feature: function() {
      return {
        id: nextID("feature"),
        type: "feature",
        stat: "05",
        title: "Signal card",
        body: "Feature blocks work well for product proof points, rollout milestones, or launch metrics.",
      };
    },
    quote: function() {
      return {
        id: nextID("quote"),
        type: "quote",
        body: "A quote block adds emphasis without breaking the overall document rhythm.",
        attribution: "Editorial desk",
      };
    },
  };

  function nextID(prefix) {
    sequence += 1;
    return prefix + "-" + String(sequence);
  }

  function escapeHTML(value) {
    return String(value || "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/\"/g, "&quot;");
  }

  function blockLabel(type) {
    switch (type) {
      case "hero":
        return "Hero";
      case "feature":
        return "Feature";
      case "quote":
        return "Quote";
      default:
        return "Block";
    }
  }

  function parseDocument(root) {
    const raw = root.getAttribute("data-cms-document") || "{\"blocks\":[]}";
    try {
      const parsed = JSON.parse(raw);
      if (!parsed || !Array.isArray(parsed.blocks)) {
        return { blocks: [] };
      }
      return {
        blocks: parsed.blocks
          .filter(function(block) {
            return block && typeof block === "object" && paletteTemplates[block.type];
          })
          .map(function(block, index) {
            return normalizeBlock(block, index);
          }),
      };
    } catch (error) {
      console.warn("[gosx] cms demo payload could not be parsed", error);
      return { blocks: [] };
    }
  }

  function normalizeBlock(block, index) {
    const base = paletteTemplates[block.type] ? paletteTemplates[block.type]() : paletteTemplates.feature();
    return {
      id: String(block.id || base.id || nextID(block.type || "block")),
      type: base.type,
      eyebrow: String(block.eyebrow || base.eyebrow || ""),
      title: String(block.title || base.title || ""),
      body: String(block.body || base.body || ""),
      cta: String(block.cta || base.cta || ""),
      stat: String(block.stat || base.stat || String(index + 1).padStart(2, "0")),
      attribution: String(block.attribution || base.attribution || ""),
    };
  }

  function totalWords(blocks) {
    return blocks.reduce(function(total, block) {
      const text = [block.eyebrow, block.title, block.body, block.cta, block.stat, block.attribution]
        .join(" ")
        .trim();
      if (!text) {
        return total;
      }
      return total + text.split(/\s+/).length;
    }, 0);
  }

  function editorMarkup(block) {
    const label = blockLabel(block.type);
    if (block.type === "hero") {
      return [
        '<article class="cms-block cms-block--hero" data-cms-block data-block-id="' + escapeHTML(block.id) + '">',
        '<div class="cms-block__head">',
        '<div class="cms-block__meta"><span class="eyebrow">' + label + ' block</span><p>Drag to reorder. Edits sync into the preview immediately.</p></div>',
        '<div class="cms-block__actions"><span class="chip cms-drag-handle" data-cms-drag-handle data-block-id="' + escapeHTML(block.id) + '" draggable="true">Drag</span><button type="button" class="chip" data-cms-remove="' + escapeHTML(block.id) + '">Remove</button></div>',
        "</div>",
        '<div class="cms-block__fields">',
        fieldMarkup("Eyebrow", "eyebrow", block.eyebrow, "Feature launch"),
        fieldMarkup("Headline", "title", block.title, "Launch headline"),
        fieldMarkup("Body", "body", block.body, "Hero copy", true),
        fieldMarkup("CTA label", "cta", block.cta, "Start the release"),
        "</div>",
        "</article>",
      ].join("");
    }
    if (block.type === "quote") {
      return [
        '<article class="cms-block cms-block--quote" data-cms-block data-block-id="' + escapeHTML(block.id) + '">',
        '<div class="cms-block__head">',
        '<div class="cms-block__meta"><span class="eyebrow">' + label + ' block</span><p>Drag to reorder. Edits sync into the preview immediately.</p></div>',
        '<div class="cms-block__actions"><span class="chip cms-drag-handle" data-cms-drag-handle data-block-id="' + escapeHTML(block.id) + '" draggable="true">Drag</span><button type="button" class="chip" data-cms-remove="' + escapeHTML(block.id) + '">Remove</button></div>',
        "</div>",
        '<div class="cms-block__fields">',
        fieldMarkup("Quote", "body", block.body, "Quote body", true),
        fieldMarkup("Attribution", "attribution", block.attribution, "Editorial desk"),
        "</div>",
        "</article>",
      ].join("");
    }
    return [
      '<article class="cms-block cms-block--feature" data-cms-block data-block-id="' + escapeHTML(block.id) + '">',
      '<div class="cms-block__head">',
      '<div class="cms-block__meta"><span class="eyebrow">' + label + ' block</span><p>Drag to reorder. Edits sync into the preview immediately.</p></div>',
      '<div class="cms-block__actions"><span class="chip cms-drag-handle" data-cms-drag-handle data-block-id="' + escapeHTML(block.id) + '" draggable="true">Drag</span><button type="button" class="chip" data-cms-remove="' + escapeHTML(block.id) + '">Remove</button></div>',
      "</div>",
      '<div class="cms-block__fields">',
      fieldMarkup("Stat", "stat", block.stat, "03"),
      fieldMarkup("Title", "title", block.title, "Feature headline"),
      fieldMarkup("Body", "body", block.body, "Feature copy", true),
      "</div>",
      "</article>",
    ].join("");
  }

  function fieldMarkup(label, name, value, placeholder, multiline) {
    if (multiline) {
      return [
        '<label class="field">',
        "<span>" + escapeHTML(label) + "</span>",
        '<textarea rows="4" data-cms-field="' + escapeHTML(name) + '" placeholder="' + escapeHTML(placeholder) + '">' + escapeHTML(value) + "</textarea>",
        "</label>",
      ].join("");
    }
    return [
      '<label class="field">',
      "<span>" + escapeHTML(label) + "</span>",
      '<input type="text" data-cms-field="' + escapeHTML(name) + '" value="' + escapeHTML(value) + '" placeholder="' + escapeHTML(placeholder) + '">',
      "</label>",
    ].join("");
  }

  function previewMarkup(block) {
    if (block.type === "hero") {
      return [
        '<section class="cms-preview-block cms-preview-block--hero">',
        '<span class="eyebrow">' + escapeHTML(block.eyebrow) + "</span>",
        "<h3>" + escapeHTML(block.title) + "</h3>",
        "<p>" + escapeHTML(block.body) + "</p>",
        '<span class="chip">' + escapeHTML(block.cta) + "</span>",
        "</section>",
      ].join("");
    }
    if (block.type === "quote") {
      return [
        '<blockquote class="cms-preview-block cms-preview-block--quote">',
        "<p>" + escapeHTML(block.body) + "</p>",
        "<footer>" + escapeHTML(block.attribution) + "</footer>",
        "</blockquote>",
      ].join("");
    }
    return [
      '<section class="cms-preview-block cms-preview-block--feature">',
      '<span class="cms-preview-block__stat">' + escapeHTML(block.stat) + "</span>",
      "<div><h3>" + escapeHTML(block.title) + "</h3><p>" + escapeHTML(block.body) + "</p></div>",
      "</section>",
    ].join("");
  }

  function renderEmpty(messageTitle, messageBody) {
    return [
      '<div class="cms-empty">',
      "<strong>" + escapeHTML(messageTitle) + "</strong>",
      "<p>" + escapeHTML(messageBody) + "</p>",
      "</div>",
    ].join("");
  }

  function sync(root, state) {
    const serialized = JSON.stringify({ blocks: state.blocks });
    root.setAttribute("data-cms-document", serialized);

    const input = root.querySelector("[data-cms-input]");
    if (input) {
      input.value = serialized;
    }

    const count = root.querySelector("[data-cms-count]");
    if (count) {
      count.textContent = String(state.blocks.length);
    }

    const words = root.querySelector("[data-cms-words]");
    if (words) {
      words.textContent = String(totalWords(state.blocks));
    }
  }

  function renderPreview(root, state) {
    const preview = root.querySelector("[data-cms-preview]");
    if (!preview) {
      return;
    }
    if (!state.blocks.length) {
      preview.innerHTML = renderEmpty("The preview is waiting.", "Add a block to populate the published page.");
      return;
    }
    preview.innerHTML = state.blocks.map(previewMarkup).join("");
  }

  function renderEditor(root, state) {
    const list = root.querySelector("[data-cms-block-list]");
    if (!list) {
      return;
    }
    if (!state.blocks.length) {
      list.innerHTML = renderEmpty("No blocks yet.", "Drag one from the palette to start the page.");
      return;
    }
    list.innerHTML = state.blocks.map(editorMarkup).join("");
  }

  function render(root, state) {
    renderEditor(root, state);
    renderPreview(root, state);
    sync(root, state);
  }

  function moveBlock(blocks, id, toIndex) {
    const currentIndex = blocks.findIndex(function(block) {
      return block.id === id;
    });
    if (currentIndex < 0) {
      return blocks;
    }
    const next = blocks.slice();
    const moved = next.splice(currentIndex, 1)[0];
    const target = Math.max(0, Math.min(toIndex, next.length));
    next.splice(target, 0, moved);
    return next;
  }

  function shiftBlock(blocks, id, direction) {
    const currentIndex = blocks.findIndex(function(block) {
      return block.id === id;
    });
    if (currentIndex < 0) {
      return blocks;
    }
    const target = direction === "up" ? currentIndex - 1 : currentIndex + 1;
    return moveBlock(blocks, id, target);
  }

  function insertBlock(blocks, type, index) {
    if (!paletteTemplates[type]) {
      return blocks;
    }
    const next = blocks.slice();
    const block = normalizeBlock(paletteTemplates[type](), next.length);
    const target = Math.max(0, Math.min(index, next.length));
    next.splice(target, 0, block);
    return next;
  }

  function clearDropState(root) {
    root.querySelectorAll(".is-drop-target").forEach(function(element) {
      element.classList.remove("is-drop-target");
    });
    const list = root.querySelector("[data-cms-block-list]");
    if (list) {
      list.removeAttribute("data-cms-drop");
    }
  }

  function resolveDropIndex(list, event, draggedID) {
    const blocks = Array.from(list.querySelectorAll("[data-cms-block]")).filter(function(element) {
      return element.getAttribute("data-block-id") !== draggedID;
    });
    if (!blocks.length) {
      return { index: 0, target: null };
    }

    let target = null;
    let index = blocks.length;
    blocks.forEach(function(block, position) {
      const rect = block.getBoundingClientRect();
      const midpoint = rect.top + rect.height / 2;
      if (!target && event.clientY < midpoint) {
        target = block;
        index = position;
      }
    });
    return { index: index, target: target };
  }

  function boot(root) {
    if (!root || root.__cmsDemo) {
      return;
    }

    const state = parseDocument(root);
    const model = {
      state: state,
      drag: null,
    };

    function handleInput(event) {
      const field = event.target && event.target.getAttribute("data-cms-field");
      const block = event.target && event.target.closest("[data-cms-block]");
      if (!field || !block) {
        return;
      }
      const blockID = block.getAttribute("data-block-id");
      model.state.blocks = model.state.blocks.map(function(entry) {
        if (entry.id !== blockID) {
          return entry;
        }
        return Object.assign({}, entry, {
          [field]: event.target.value,
        });
      });
      renderPreview(root, model.state);
      sync(root, model.state);
    }

    function handleClick(event) {
      const add = event.target && event.target.closest("[data-cms-add-type]");
      if (add) {
        model.state.blocks = insertBlock(model.state.blocks, add.getAttribute("data-cms-add-type"), model.state.blocks.length);
        render(root, model.state);
        return;
      }

      const remove = event.target && event.target.closest("[data-cms-remove]");
      if (remove) {
        const blockID = remove.getAttribute("data-cms-remove");
        model.state.blocks = model.state.blocks.filter(function(block) {
          return block.id !== blockID;
        });
        render(root, model.state);
        return;
      }

      const move = event.target && event.target.closest("[data-cms-move]");
      if (move) {
        model.state.blocks = shiftBlock(
          model.state.blocks,
          move.getAttribute("data-cms-move"),
          move.getAttribute("data-cms-direction")
        );
        render(root, model.state);
      }
    }

    function handleDragStart(event) {
      const paletteCard = event.target && event.target.closest("[data-cms-palette-card]");
      if (paletteCard) {
        model.drag = {
          kind: "palette",
          type: paletteCard.getAttribute("data-cms-type"),
          blockID: "",
        };
      }

      const handle = event.target && event.target.closest("[data-cms-drag-handle]");
      if (handle) {
        model.drag = {
          kind: "existing",
          type: "",
          blockID: handle.getAttribute("data-block-id"),
        };
      }

      if (!model.drag || !event.dataTransfer) {
        return;
      }

      event.dataTransfer.effectAllowed = model.drag.kind === "palette" ? "copy" : "move";
      event.dataTransfer.setData("text/plain", JSON.stringify(model.drag));
    }

    function handleDragOver(event) {
      const list = root.querySelector("[data-cms-block-list]");
      if (!list || !event.target || !event.target.closest("[data-cms-block-list]")) {
        return;
      }
      event.preventDefault();
      clearDropState(root);
      const drop = resolveDropIndex(list, event, model.drag && model.drag.blockID);
      if (drop.target) {
        drop.target.classList.add("is-drop-target");
      } else {
        list.setAttribute("data-cms-drop", "empty");
      }
    }

    function handleDrop(event) {
      const list = root.querySelector("[data-cms-block-list]");
      if (!list || !event.target || !event.target.closest("[data-cms-block-list]")) {
        return;
      }
      event.preventDefault();
      if (!model.drag && event.dataTransfer) {
        try {
          model.drag = JSON.parse(event.dataTransfer.getData("text/plain"));
        } catch {}
      }
      if (!model.drag) {
        clearDropState(root);
        return;
      }

      const drop = resolveDropIndex(list, event, model.drag.blockID);
      if (model.drag.kind === "palette") {
        model.state.blocks = insertBlock(model.state.blocks, model.drag.type, drop.index);
      } else if (model.drag.kind === "existing") {
        model.state.blocks = moveBlock(model.state.blocks, model.drag.blockID, drop.index);
      }
      model.drag = null;
      clearDropState(root);
      render(root, model.state);
    }

    function handleDragEnd() {
      model.drag = null;
      clearDropState(root);
    }

    root.addEventListener("input", handleInput);
    root.addEventListener("click", handleClick);
    root.addEventListener("dragstart", handleDragStart);
    root.addEventListener("dragover", handleDragOver);
    root.addEventListener("drop", handleDrop);
    root.addEventListener("dragend", handleDragEnd);

    root.__cmsDemo = model;
    render(root, model.state);
  }

  function bootAll() {
    document.querySelectorAll(ROOT_SELECTOR).forEach(boot);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", bootAll, { once: true });
  } else {
    bootAll();
  }

  document.addEventListener("gosx:navigate", bootAll);
})();
