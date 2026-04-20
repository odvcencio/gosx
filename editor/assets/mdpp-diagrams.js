(() => {
  const mermaidRuntimeURL = "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs";
  let mermaidRuntimePromise = null;
  let activeDialog = null;

  const escapeHTML = (value) => String(value)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");

  const ready = (fn) => {
    if (document.readyState === "loading") {
      document.addEventListener("DOMContentLoaded", fn, { once: true });
      return;
    }
    fn();
  };

  const loadMermaidRuntime = async () => {
    if (window.mermaid) return window.mermaid;
    if (!mermaidRuntimePromise) {
      mermaidRuntimePromise = import(mermaidRuntimeURL).then((mod) => {
        const runtime = mod.default || mod.mermaid || window.mermaid;
        if (runtime && typeof runtime.initialize === "function") {
          runtime.initialize({
            startOnLoad: false,
            securityLevel: "strict",
            theme: "dark",
          });
        }
        return runtime;
      });
    }
    return mermaidRuntimePromise;
  };

  const diagramSource = (figure) => {
    const code = figure && figure.querySelector("pre code");
    return code ? code.textContent.trim() : "";
  };

  const diagramLabel = (figure) => {
    const kind = figure?.dataset?.diagramKind || "diagram";
    return `${kind} diagram`;
  };

  const render = async (root = document) => {
    if (!root) return;
    const figures = Array.from(root.querySelectorAll('.mdpp-diagram[data-diagram-syntax="mermaid"]:not([data-mdpp-diagram-state])'));
    const pending = figures.filter((figure) => diagramSource(figure) !== "");
    if (pending.length === 0) return;

    let mermaid;
    try {
      mermaid = await loadMermaidRuntime();
    } catch (_) {
      return;
    }
    if (!mermaid || typeof mermaid.render !== "function") return;

    for (const [index, figure] of pending.entries()) {
      const source = diagramSource(figure);
      if (source === "") continue;
      const id = `mdpp-mermaid-${Date.now()}-${index}`;
      figure.dataset.mdppDiagramState = "rendering";
      try {
        const result = await mermaid.render(id, source);
        const svg = typeof result === "string" ? result : result && result.svg;
        if (!svg) throw new Error("Mermaid returned no SVG");
        figure.innerHTML = [
          '<div class="mdpp-diagram-rendered" role="button" tabindex="0">',
          svg,
          '<span class="mdpp-diagram-expand-hint" aria-hidden="true">Expand</span>',
          '</div>',
          '<details class="mdpp-diagram-source">',
          '<summary>Source</summary>',
          `<pre><code class="language-mermaid">${escapeHTML(source)}</code></pre>`,
          '</details>',
        ].join("");
        const rendered = figure.querySelector(".mdpp-diagram-rendered");
        if (rendered) {
          const label = `Open ${diagramLabel(figure)}`;
          rendered.setAttribute("aria-label", label);
          rendered.addEventListener("click", () => openZoom(figure));
          rendered.addEventListener("keydown", (event) => {
            if (event.key !== "Enter" && event.key !== " ") return;
            event.preventDefault();
            openZoom(figure);
          });
        }
        if (result && typeof result.bindFunctions === "function") {
          result.bindFunctions(figure);
        }
        figure.dataset.mdppDiagramState = "rendered";
      } catch (err) {
        figure.dataset.mdppDiagramState = "error";
        const message = err && err.message ? err.message : "Could not render diagram.";
        figure.insertAdjacentHTML("beforeend", `<p class="mdpp-diagram-error">${escapeHTML(message)}</p>`);
      }
    }
  };

  const openZoom = (figure) => {
    const rendered = figure?.querySelector(".mdpp-diagram-rendered");
    const svg = rendered?.querySelector("svg");
    if (!svg) return;
    closeZoom();

    const dialog = document.createElement("div");
    dialog.className = "mdpp-diagram-zoom";
    const scope = figure.closest("[data-gosx-s]");
    if (scope) {
      dialog.setAttribute("data-gosx-s", scope.getAttribute("data-gosx-s"));
    }
    dialog.setAttribute("role", "dialog");
    dialog.setAttribute("aria-modal", "true");
    dialog.setAttribute("aria-label", `Expanded ${diagramLabel(figure)}`);
    dialog.innerHTML = [
      '<div class="mdpp-diagram-zoom-backdrop" data-diagram-close="true"></div>',
      '<div class="mdpp-diagram-zoom-panel">',
      '<div class="mdpp-diagram-zoom-toolbar">',
      `<span class="mdpp-diagram-zoom-title">${escapeHTML(diagramLabel(figure))}</span>`,
      '<div class="mdpp-diagram-zoom-actions">',
      '<button type="button" data-diagram-zoom="out" aria-label="Zoom out">-</button>',
      '<button type="button" data-diagram-zoom="reset" aria-label="Reset zoom">Reset</button>',
      '<button type="button" data-diagram-zoom="in" aria-label="Zoom in">+</button>',
      '<button type="button" data-diagram-close="true" aria-label="Close diagram">Close</button>',
      '</div>',
      '</div>',
      '<div class="mdpp-diagram-zoom-viewport">',
      '<div class="mdpp-diagram-zoom-content"></div>',
      '</div>',
      '</div>',
    ].join("");
    const content = dialog.querySelector(".mdpp-diagram-zoom-content");
    content.appendChild(svg.cloneNode(true));
    document.body.appendChild(dialog);

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    activeDialog = { dialog, previousOverflow, opener: rendered };

    const viewport = dialog.querySelector(".mdpp-diagram-zoom-viewport");
    const state = { scale: 1, x: 0, y: 0, dragging: false, startX: 0, startY: 0, originX: 0, originY: 0 };
    const clampScale = (value) => Math.max(0.4, Math.min(4, value));
    const apply = () => {
      content.style.transform = `translate(${state.x}px, ${state.y}px) scale(${state.scale})`;
    };
    const zoomBy = (delta, centerX = viewport.clientWidth / 2, centerY = viewport.clientHeight / 2) => {
      const next = clampScale(state.scale * delta);
      const ratio = next / state.scale;
      state.x = centerX - (centerX - state.x) * ratio;
      state.y = centerY - (centerY - state.y) * ratio;
      state.scale = next;
      apply();
    };
    const reset = () => {
      state.scale = 1;
      state.x = 0;
      state.y = 0;
      apply();
    };

    viewport.addEventListener("wheel", (event) => {
      event.preventDefault();
      const rect = viewport.getBoundingClientRect();
      const factor = event.deltaY < 0 ? 1.12 : 0.89;
      zoomBy(factor, event.clientX - rect.left, event.clientY - rect.top);
    }, { passive: false });

    viewport.addEventListener("pointerdown", (event) => {
      if (event.button !== 0) return;
      state.dragging = true;
      state.startX = event.clientX;
      state.startY = event.clientY;
      state.originX = state.x;
      state.originY = state.y;
      viewport.setPointerCapture(event.pointerId);
      viewport.classList.add("is-panning");
    });
    viewport.addEventListener("pointermove", (event) => {
      if (!state.dragging) return;
      state.x = state.originX + event.clientX - state.startX;
      state.y = state.originY + event.clientY - state.startY;
      apply();
    });
    const endPan = (event) => {
      if (!state.dragging) return;
      state.dragging = false;
      viewport.classList.remove("is-panning");
      if (viewport.hasPointerCapture(event.pointerId)) {
        viewport.releasePointerCapture(event.pointerId);
      }
    };
    viewport.addEventListener("pointerup", endPan);
    viewport.addEventListener("pointercancel", endPan);

    dialog.addEventListener("click", (event) => {
      const closeTarget = event.target.closest("[data-diagram-close]");
      const zoomTarget = event.target.closest("[data-diagram-zoom]");
      if (closeTarget) {
        closeZoom();
        return;
      }
      if (!zoomTarget) return;
      switch (zoomTarget.dataset.diagramZoom) {
        case "in":
          zoomBy(1.2);
          break;
        case "out":
          zoomBy(0.83);
          break;
        case "reset":
          reset();
          break;
      }
    });
    const keyHandler = (event) => {
      if (activeDialog?.dialog !== dialog) return;
      if (event.key === "Escape") {
        closeZoom();
        return;
      }
      if (event.key === "Tab") {
        const focusables = Array.from(dialog.querySelectorAll("button, [href], [tabindex]:not([tabindex='-1'])"))
          .filter((el) => !el.disabled && el.offsetParent !== null);
        if (focusables.length > 0) {
          const first = focusables[0];
          const last = focusables[focusables.length - 1];
          if (event.shiftKey && document.activeElement === first) {
            event.preventDefault();
            last.focus();
          } else if (!event.shiftKey && document.activeElement === last) {
            event.preventDefault();
            first.focus();
          }
        }
        return;
      }
      if (event.key === "+" || event.key === "=") {
        event.preventDefault();
        zoomBy(1.2);
      }
      if (event.key === "-") {
        event.preventDefault();
        zoomBy(0.83);
      }
      if (event.key === "0") {
        event.preventDefault();
        reset();
      }
    };
    dialog._diagramKeyHandler = keyHandler;
    document.addEventListener("keydown", keyHandler);
    dialog.querySelector("button[data-diagram-close]")?.focus();
    apply();
  };

  const closeZoom = () => {
    if (!activeDialog) return;
    const { dialog, previousOverflow, opener } = activeDialog;
    if (dialog._diagramKeyHandler) document.removeEventListener("keydown", dialog._diagramKeyHandler);
    document.body.style.overflow = previousOverflow;
    dialog.remove();
    activeDialog = null;
    if (opener && document.contains(opener)) {
      try {
        opener.focus({ preventScroll: true });
      } catch (_) {
        opener.focus();
      }
    }
  };

  window.M31Diagrams = { render, closeZoom };
  ready(() => { void render(document); });
})();
