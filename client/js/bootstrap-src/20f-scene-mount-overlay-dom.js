// 20f — the DOM overlay: labels, sprites, HTML elements and HTML textures.
//
// Projects scene-space anchors to screen space, lays out label text through
// the text-layout engine, resolves overlap by priority, and writes real DOM
// elements over the canvas. The HTML-texture path rasterizes an overlay
// element into a GPU texture instead.
//
// This file is a gate candidate: the server knows whether the scene declares
// any labels, sprites or html entries.
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
    const text = String(label.text || "");
    const lineHeight = sceneNumber(label.lineHeight, 18);
    const layout = layoutBrowserText(
      text,
      label.font,
      sceneNumber(label.maxWidth, 180),
      normalizeSceneLabelWhiteSpace(label.whiteSpace),
      lineHeight,
      {
        maxLines: Math.max(0, Math.floor(sceneNumber(label.maxLines, 0))),
        overflow: normalizeTextLayoutOverflow(label.overflow),
      },
    );
    if (layout && Array.isArray(layout.lines)) {
      return layout;
    }
    // layoutBrowserText comes from window.__gosx_runtime_api. A bundle mix
    // without that bridge — a Scene3D chunk loaded beside a runtime that never
    // published the text-layout API — makes it return null, and every caller of
    // layoutSceneLabel then reads layout.lines on null and throws. Return a
    // single-line layout so a label degrades to unwrapped text instead.
    return {
      lines: [{ text: text }],
      maxLineWidth: 0,
      height: lineHeight,
      truncated: false,
    };
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
    return sceneOverlayOccluded(entry, occluders, entry && entry.label && entry.label.occlude);
  }

  function sceneOverlayOccluded(entry, occluders, occlude) {
    if (!entry || !occlude || !Array.isArray(occluders) || !occluders.length) {
      return false;
    }
    const overlayDepth = sceneNumber(entry && entry.depth, 0);
    for (const occluder of occluders) {
      if (occluder.depth > overlayDepth + 0.05) {
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
        depth: sceneNumber(label.depth, 0),
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

  function sceneSpriteBounds(sprite) {
    const anchorX = sceneNumber(sprite.anchorX, 0.5);
    const anchorY = sceneNumber(sprite.anchorY, 0.5);
    const spriteWidth = Math.max(1, sceneNumber(sprite.width, 1));
    const spriteHeight = Math.max(1, sceneNumber(sprite.height, 1));
    const anchorPointX = sceneNumber(sprite.position && sprite.position.x, 0) + sceneNumber(sprite.offsetX, 0);
    const anchorPointY = sceneNumber(sprite.position && sprite.position.y, 0) + sceneNumber(sprite.offsetY, 0);
    const left = anchorPointX - (anchorX * spriteWidth);
    const top = anchorPointY - (anchorY * spriteHeight);
    return {
      left,
      top,
      right: left + spriteWidth,
      bottom: top + spriteHeight,
      anchor: { x: anchorPointX, y: anchorPointY },
      center: { x: left + (spriteWidth / 2), y: top + (spriteHeight / 2) },
    };
  }

  function sceneSpritePriorityCompare(a, b) {
    const priorityDiff = sceneNumber(b && b.sprite && b.sprite.priority, 0) - sceneNumber(a && a.sprite && a.sprite.priority, 0);
    if (Math.abs(priorityDiff) > 0.001) {
      return priorityDiff;
    }
    const depthDiff = sceneNumber(a && a.sprite && a.sprite.depth, 0) - sceneNumber(b && b.sprite && b.sprite.depth, 0);
    if (Math.abs(depthDiff) > 0.001) {
      return depthDiff;
    }
    return sceneNumber(a && a.order, 0) - sceneNumber(b && b.order, 0);
  }

  function prepareSceneSpriteEntries(bundle, width, height) {
    const sprites = bundle && Array.isArray(bundle.sprites) ? bundle.sprites : [];
    const occluders = buildSceneLabelOccluders(bundle, width, height);
    const entries = [];
    for (let index = 0; index < sprites.length; index += 1) {
      const sprite = sprites[index];
      if (!sprite || typeof sprite.src !== "string" || sprite.src.trim() === "") {
        continue;
      }
      const box = sceneSpriteBounds(sprite);
      entries.push({
        id: sprite.id || ("scene-sprite-" + index),
        order: index,
        sprite,
        depth: sceneNumber(sprite.depth, 0),
        box,
        occluded: false,
        hidden: false,
      });
    }
    const sorted = entries.slice().sort(sceneSpritePriorityCompare);
    for (const entry of sorted) {
      entry.occluded = sceneOverlayOccluded(entry, occluders, entry.sprite && entry.sprite.occlude);
      if (entry.occluded) {
        entry.hidden = true;
      }
    }
    return entries;
  }

  function renderSceneLabelElement(element, label, layoutKey, layout, metrics, box, hidden, occluded) {
    const align = normalizeSceneLabelAlign(label.textAlign);
    const whiteSpace = normalizeSceneLabelWhiteSpace(label.whiteSpace);
    const zIndex = Math.max(1, 1000 + Math.round(sceneNumber(label.priority, 0) * 10) - Math.round(sceneNumber(label.depth, 0) * 10));

    element.setAttribute("data-gosx-scene-label", label.id || "");
    setAttrValue(element, "aria-hidden", "true");
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

  function renderSceneSpriteElement(element, sprite, box, hidden, occluded) {
    const zIndex = Math.max(1, 1000 + Math.round(sceneNumber(sprite.priority, 0) * 10) - Math.round(sceneNumber(sprite.depth, 0) * 10));
    element.setAttribute("data-gosx-scene-sprite", sprite.id || "");
    setAttrValue(element, "aria-hidden", "true");
    setAttrValue(element, "class", sprite.className ? ("gosx-scene-sprite " + sprite.className) : "gosx-scene-sprite");
    setAttrValue(element, "data-gosx-scene-sprite-fit", normalizeSceneSpriteFit(sprite.fit));
    setAttrValue(element, "data-gosx-scene-sprite-occlude", sprite.occlude ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-sprite-occluded", occluded ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-sprite-visibility", hidden ? "hidden" : "visible");
    setAttrValue(element, "data-gosx-scene-sprite-priority", sceneNumber(sprite.priority, 0));
    setAttrValue(element, "data-gosx-scene-sprite-depth", sceneNumber(sprite.depth, 0));
    setStyleValue(element.style, "--gosx-scene-sprite-left", box.anchor.x + "px");
    setStyleValue(element.style, "--gosx-scene-sprite-top", box.anchor.y + "px");
    setStyleValue(element.style, "--gosx-scene-sprite-anchor-x", String(sceneNumber(sprite.anchorX, 0.5)));
    setStyleValue(element.style, "--gosx-scene-sprite-anchor-y", String(sceneNumber(sprite.anchorY, 0.5)));
    setStyleValue(element.style, "--gosx-scene-sprite-width", Math.max(1, sceneNumber(sprite.width, 1)) + "px");
    setStyleValue(element.style, "--gosx-scene-sprite-height", Math.max(1, sceneNumber(sprite.height, 1)) + "px");
    setStyleValue(element.style, "--gosx-scene-sprite-opacity", String(clamp01(sceneNumber(sprite.opacity, 1))));
    setStyleValue(element.style, "--gosx-scene-sprite-fit", normalizeSceneSpriteFit(sprite.fit));
    setStyleValue(element.style, "--gosx-scene-sprite-z-index", String(zIndex));
    setStyleValue(element.style, "--gosx-scene-sprite-depth", String(sceneNumber(sprite.depth, 0)));

    let image = element.firstChild;
    if (!image || image.tagName !== "IMG") {
      clearChildren(element);
      image = document.createElement("img");
      image.setAttribute("draggable", "false");
      image.setAttribute("alt", "");
      image.setAttribute("aria-hidden", "true");
      element.appendChild(image);
    }
    setAttrValue(image, "src", sprite.src || "");
    setStyleValue(image.style, "objectFit", normalizeSceneSpriteFit(sprite.fit) === "fill" ? "fill" : normalizeSceneSpriteFit(sprite.fit));
  }

  function renderSceneSprites(layer, bundle, elements, width, height) {
    if (!layer) {
      return;
    }

    const sprites = prepareSceneSpriteEntries(bundle, width, height);
    const active = new Set();
    for (const entry of sprites) {
      const id = entry.id;
      active.add(id);
      let element = elements.get(id);
      if (!element) {
        element = document.createElement("div");
        layer.appendChild(element);
        elements.set(id, element);
      }
      renderSceneSpriteElement(element, entry.sprite, entry.box, entry.hidden, entry.occluded);
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

  function sceneHTMLBounds(entry) {
    const anchorX = sceneNumber(entry.anchorX, 0.5);
    const anchorY = sceneNumber(entry.anchorY, 0.5);
    const htmlWidth = Math.max(1, sceneNumber(entry.width, 1));
    const htmlHeight = Math.max(1, sceneNumber(entry.height, 1));
    const anchorPointX = sceneNumber(entry.position && entry.position.x, 0) + sceneNumber(entry.offsetX, 0);
    const anchorPointY = sceneNumber(entry.position && entry.position.y, 0) + sceneNumber(entry.offsetY, 0);
    const left = anchorPointX - (anchorX * htmlWidth);
    const top = anchorPointY - (anchorY * htmlHeight);
    return {
      left,
      top,
      right: left + htmlWidth,
      bottom: top + htmlHeight,
      anchor: { x: anchorPointX, y: anchorPointY },
      center: { x: left + (htmlWidth / 2), y: top + (htmlHeight / 2) },
    };
  }

  function sceneHTMLPriorityCompare(a, b) {
    const priorityDiff = sceneNumber(b && b.html && b.html.priority, 0) - sceneNumber(a && a.html && a.html.priority, 0);
    if (Math.abs(priorityDiff) > 0.001) {
      return priorityDiff;
    }
    const depthDiff = sceneNumber(a && a.html && a.html.depth, 0) - sceneNumber(b && b.html && b.html.depth, 0);
    if (Math.abs(depthDiff) > 0.001) {
      return depthDiff;
    }
    return sceneNumber(a && a.order, 0) - sceneNumber(b && b.order, 0);
  }

  function prepareSceneHTMLEntries(bundle, width, height) {
    const htmlEntries = bundle && Array.isArray(bundle.html) ? bundle.html : [];
    const occluders = buildSceneLabelOccluders(bundle, width, height);
    const entries = [];
    for (let index = 0; index < htmlEntries.length; index += 1) {
      const htmlEntry = htmlEntries[index];
      if (!htmlEntry || typeof htmlEntry.html !== "string" || htmlEntry.html.trim() === "") {
        continue;
      }
      const box = sceneHTMLBounds(htmlEntry);
      entries.push({
        id: htmlEntry.id || ("scene-html-" + index),
        order: index,
        html: htmlEntry,
        depth: sceneNumber(htmlEntry.depth, 0),
        box,
        occluded: false,
        hidden: false,
      });
    }
    const sorted = entries.slice().sort(sceneHTMLPriorityCompare);
    for (const entry of sorted) {
      entry.occluded = sceneOverlayOccluded(entry, occluders, entry.html && entry.html.occlude);
      if (entry.occluded) {
        entry.hidden = true;
      }
    }
    return entries;
  }

  function renderSceneHTMLElement(element, htmlEntry, box, hidden, occluded) {
    const zIndex = Math.max(1, 1000 + Math.round(sceneNumber(htmlEntry.priority, 0) * 10) - Math.round(sceneNumber(htmlEntry.depth, 0) * 10));
    element.setAttribute("data-gosx-scene-html", htmlEntry.id || "");
    setAttrValue(element, "class", htmlEntry.className ? ("gosx-scene-html " + htmlEntry.className) : "gosx-scene-html");
    setAttrValue(element, "data-gosx-scene-html-target", htmlEntry.target || "");
    setAttrValue(element, "data-gosx-scene-html-mode", htmlEntry.mode || "dom");
    setAttrValue(element, "data-gosx-scene-html-fallback", htmlEntry.fallback || "");
    setAttrValue(element, "data-gosx-scene-html-fallback-reason", htmlEntry.fallbackReason || "");
    setAttrValue(element, "data-gosx-scene-html-texture-key", htmlEntry.textureKey || "");
    setAttrValue(element, "data-gosx-scene-html-texture-width", sceneNumber(htmlEntry.textureWidth, 0) > 0 ? sceneNumber(htmlEntry.textureWidth, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-texture-height", sceneNumber(htmlEntry.textureHeight, 0) > 0 ? sceneNumber(htmlEntry.textureHeight, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-texture-bytes", sceneNumber(htmlEntry.textureBytes, 0) > 0 ? sceneNumber(htmlEntry.textureBytes, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-texture-cap-bytes", sceneNumber(htmlEntry.textureMaxBytes, 0) > 0 ? sceneNumber(htmlEntry.textureMaxBytes, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-texture-over-budget", htmlEntry.textureOverBudget ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-html-texture-ready", htmlEntry.textureReady ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-html-texture-revision", sceneNumber(htmlEntry.textureRevision, 0) > 0 ? sceneNumber(htmlEntry.textureRevision, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-texture-dirty", htmlEntry.textureDirty ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-html-texture-dirty-bytes", sceneNumber(htmlEntry.textureDirtyBytes, 0) > 0 ? sceneNumber(htmlEntry.textureDirtyBytes, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-texture-upload-pending-bytes", sceneNumber(htmlEntry.texturePendingUploadBytes, 0) > 0 ? sceneNumber(htmlEntry.texturePendingUploadBytes, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-texture-manager", htmlEntry.textureManager || "");
    setAttrValue(element, "data-gosx-scene-html-texture-rasterized", htmlEntry.textureRasterized ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-html-texture-upload-bytes", sceneNumber(htmlEntry.textureUploadBytes, 0) > 0 ? sceneNumber(htmlEntry.textureUploadBytes, 0) : "");
    setAttrValue(element, "data-gosx-scene-html-occlude", htmlEntry.occlude ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-html-occluded", occluded ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-html-visibility", hidden ? "hidden" : "visible");
    setAttrValue(element, "aria-hidden", hidden ? "true" : "false");
    setAttrValue(element, "data-gosx-scene-html-priority", sceneNumber(htmlEntry.priority, 0));
    setAttrValue(element, "data-gosx-scene-html-depth", sceneNumber(htmlEntry.depth, 0));
    setAttrValue(element, "data-gosx-scene-html-pointer-events", normalizeSceneHTMLPointerEvents(htmlEntry.pointerEvents, "none"));
    setStyleValue(element.style, "--gosx-scene-html-left", box.anchor.x + "px");
    setStyleValue(element.style, "--gosx-scene-html-top", box.anchor.y + "px");
    setStyleValue(element.style, "--gosx-scene-html-anchor-x", String(sceneNumber(htmlEntry.anchorX, 0.5)));
    setStyleValue(element.style, "--gosx-scene-html-anchor-y", String(sceneNumber(htmlEntry.anchorY, 0.5)));
    setStyleValue(element.style, "--gosx-scene-html-width", Math.max(1, sceneNumber(htmlEntry.width, 1)) + "px");
    setStyleValue(element.style, "--gosx-scene-html-min-height", Math.max(1, sceneNumber(htmlEntry.height, 1)) + "px");
    setStyleValue(element.style, "--gosx-scene-html-opacity", String(clamp01(sceneNumber(htmlEntry.opacity, 1))));
    setStyleValue(element.style, "--gosx-scene-html-z-index", String(zIndex));
    setStyleValue(element.style, "--gosx-scene-html-depth", String(sceneNumber(htmlEntry.depth, 0)));
    setStyleValue(element.style, "--gosx-scene-html-pointer-events", normalizeSceneHTMLPointerEvents(htmlEntry.pointerEvents, "none"));
    if (element.__gosxHTMLMarkup !== htmlEntry.html) {
      element.innerHTML = htmlEntry.html;
      element.__gosxHTMLMarkup = htmlEntry.html;
    }
  }

  function sceneHTMLTextureTargetID(htmlEntry) {
    if (!htmlEntry || typeof htmlEntry !== "object") {
      return "";
    }
    if (typeof htmlEntry.target === "string" && htmlEntry.target.trim()) {
      return htmlEntry.target.trim();
    }
    if (typeof htmlEntry.targetID === "string" && htmlEntry.targetID.trim()) {
      return htmlEntry.targetID.trim();
    }
    return "";
  }

  function sceneHTMLTextureNumber(value, fallback) {
    const number = Number(value);
    return Number.isFinite(number) ? number : fallback;
  }

  function dispatchSceneHTMLTexturePointer(bundle, elements, detail) {
    if (!bundle || !elements || !detail || typeof detail !== "object") {
      return false;
    }
    const targetID = typeof detail.targetID === "string" ? detail.targetID.trim() : "";
    if (!targetID || !Array.isArray(bundle.html)) {
      return false;
    }
    let dispatched = false;
    for (const htmlEntry of bundle.html) {
      if (!htmlEntry || normalizeSceneHTMLMode(htmlEntry.mode, "dom") !== "texture") {
        continue;
      }
      const htmlTargetID = sceneHTMLTextureTargetID(htmlEntry);
      if (htmlTargetID !== targetID && htmlEntry.id !== targetID) {
        continue;
      }
      const id = htmlEntry.id || "";
      const element = elements.get(id);
      if (!element || typeof element.dispatchEvent !== "function") {
        continue;
      }
      const width = Math.max(1, sceneNumber(htmlEntry.width, 1));
      const height = Math.max(1, sceneNumber(htmlEntry.height, 1));
      const uvX = clamp01(sceneHTMLTextureNumber(detail.uvX, 0));
      const uvY = clamp01(sceneHTMLTextureNumber(detail.uvY, 0));
      const localX = uvX * width;
      const localY = uvY * height;
      const pointerDetail = {
        htmlID: id,
        targetID,
        type: typeof detail.type === "string" ? detail.type : "",
        pointerX: sceneHTMLTextureNumber(detail.pointerX, 0),
        pointerY: sceneHTMLTextureNumber(detail.pointerY, 0),
        uvX,
        uvY,
        localX,
        localY,
        width,
        height,
        fallback: htmlEntry.fallback || (normalizeSceneHTMLMode(htmlEntry.mode, "dom") === "texture" ? "dom-overlay" : ""),
        fallbackReason: htmlEntry.fallbackReason || (normalizeSceneHTMLMode(htmlEntry.mode, "dom") === "texture" ? "html-texture-manager-unavailable" : ""),
        scene: detail,
      };
      setAttrValue(element, "data-gosx-scene-html-hit-type", pointerDetail.type);
      setAttrValue(element, "data-gosx-scene-html-hit-target", pointerDetail.targetID);
      setAttrValue(element, "data-gosx-scene-html-hit-uv-x", pointerDetail.uvX);
      setAttrValue(element, "data-gosx-scene-html-hit-uv-y", pointerDetail.uvY);
      setAttrValue(element, "data-gosx-scene-html-hit-local-x", pointerDetail.localX);
      setAttrValue(element, "data-gosx-scene-html-hit-local-y", pointerDetail.localY);
      setStyleValue(element.style, "--gosx-scene-html-hit-uv-x", String(pointerDetail.uvX));
      setStyleValue(element.style, "--gosx-scene-html-hit-uv-y", String(pointerDetail.uvY));
      setStyleValue(element.style, "--gosx-scene-html-hit-local-x", pointerDetail.localX + "px");
      setStyleValue(element.style, "--gosx-scene-html-hit-local-y", pointerDetail.localY + "px");
      const event = typeof CustomEvent === "function"
        ? new CustomEvent("gosx:scene-html-texture-pointer", { detail: pointerDetail })
        : { type: "gosx:scene-html-texture-pointer", detail: pointerDetail };
      element.dispatchEvent(event);
      dispatched = true;
    }
    return dispatched;
  }

  function createSceneHTMLTextureState() {
    return {
      records: new Map(),
      revision: 0,
      disposed: 0,
      disposedBytes: 0,
      requestRender: null,
    };
  }

  function disposeSceneHTMLTextureState(state) {
    if (!state || !state.records) {
      return;
    }
    state.records.clear();
  }

  function sceneHTMLTextureLifecycleID(html, index) {
    if (html && typeof html.id === "string" && html.id.trim()) {
      return html.id.trim();
    }
    if (html && typeof html.textureKey === "string" && html.textureKey.trim()) {
      return html.textureKey.trim();
    }
    return "scene-html-" + index;
  }

  function sceneHTMLTextureLifecycleSignature(html, record) {
    const textureKey = record && record.textureKey === (html && html.textureKey) && record.sourceKey
      ? record.sourceKey
      : (html && html.textureKey ? html.textureKey : "");
    return [
      textureKey,
      sceneNumber(html && html.textureWidth, 0),
      sceneNumber(html && html.textureHeight, 0),
      sceneNumber(html && html.textureBytes, 0),
      sceneNumber(html && html.textureMaxBytes, 0),
      html && html.html ? html.html : "",
    ].join("|");
  }

  function syncSceneHTMLTextureState(state, entries) {
    const lifecycle = { dirty: 0, dirtyBytes: 0, pendingUploadBytes: 0, disposed: 0, disposedBytes: 0, revision: 0 };
    if (!state || !state.records) {
      return lifecycle;
    }
    const active = new Set();
    for (let index = 0; index < entries.length; index += 1) {
      const html = entries[index] && entries[index].html;
      if (!html || normalizeSceneHTMLMode(html.mode, "dom") !== "texture") {
        continue;
      }
      const id = sceneHTMLTextureLifecycleID(html, index);
      active.add(id);
      let record = state.records.get(id);
      if (!record) {
        record = { id, revision: 0, signature: "", bytes: 0, dirty: false, dirtyBytes: 0, pendingUploadBytes: 0 };
        state.records.set(id, record);
      }
      const signature = sceneHTMLTextureLifecycleSignature(html, record);
      const bytes = Math.max(0, Math.floor(sceneNumber(html.textureBytes, 0)));
      if (record.signature !== signature) {
        record.signature = signature;
        record.revision += 1;
        state.revision += 1;
        record.dirty = !html.textureOverBudget;
        record.dirtyBytes = record.dirty ? bytes : 0;
        record.pendingUploadBytes = record.dirty && !html.textureReady ? bytes : 0;
      }
      record.bytes = bytes;
      record.ready = Boolean(html.textureReady && !html.textureOverBudget);
      if (record.ready) {
        record.dirty = false;
        record.dirtyBytes = 0;
        record.pendingUploadBytes = 0;
      }
      html.textureRevision = record.revision;
      html.textureDirty = record.dirty;
      html.textureDirtyBytes = record.dirtyBytes;
      html.texturePendingUploadBytes = record.pendingUploadBytes;
      html.textureManager = record.manager || "";
      html.textureRasterized = Boolean(record.rasterized);
      html.textureUploadBytes = record.uploadBytes || 0;
      if (record.dirty) {
        lifecycle.dirty += 1;
        lifecycle.dirtyBytes += record.dirtyBytes;
      }
      lifecycle.pendingUploadBytes += record.pendingUploadBytes;
    }
    state.records.forEach(function(record, id) {
      if (active.has(id)) {
        return;
      }
      state.disposed += 1;
      state.disposedBytes += Math.max(0, Math.floor(sceneNumber(record && record.bytes, 0)));
      state.records.delete(id);
    });
    lifecycle.disposed = state.disposed;
    lifecycle.disposedBytes = state.disposedBytes;
    lifecycle.revision = state.revision;
    return lifecycle;
  }

  function sceneHTMLTextureStats(entries, lifecycle) {
    const stats = { bytes: 0, capBytes: 0, overBudget: 0, ready: 0, count: 0, dirty: 0, dirtyBytes: 0, pendingUploadBytes: 0, disposed: 0, disposedBytes: 0, revision: 0 };
    for (const entry of entries || []) {
      const html = entry && entry.html;
      if (!html || normalizeSceneHTMLMode(html.mode, "dom") !== "texture") {
        continue;
      }
      stats.count += 1;
      stats.bytes += Math.max(0, Math.floor(sceneNumber(html.textureBytes, 0)));
      stats.capBytes += Math.max(0, Math.floor(sceneNumber(html.textureMaxBytes, 0)));
      if (html.textureOverBudget) {
        stats.overBudget += 1;
      }
      if (html.textureReady) {
        stats.ready += 1;
      }
    }
    if (lifecycle) {
      stats.dirty = Math.max(0, Math.floor(sceneNumber(lifecycle.dirty, 0)));
      stats.dirtyBytes = Math.max(0, Math.floor(sceneNumber(lifecycle.dirtyBytes, 0)));
      stats.pendingUploadBytes = Math.max(0, Math.floor(sceneNumber(lifecycle.pendingUploadBytes, 0)));
      stats.disposed = Math.max(0, Math.floor(sceneNumber(lifecycle.disposed, 0)));
      stats.disposedBytes = Math.max(0, Math.floor(sceneNumber(lifecycle.disposedBytes, 0)));
      stats.revision = Math.max(0, Math.floor(sceneNumber(lifecycle.revision, 0)));
    }
    return stats;
  }

  function sceneHTMLTextureDataURL(html) {
    const width = Math.max(1, Math.floor(sceneNumber(html && html.textureWidth, 512)));
    const height = Math.max(1, Math.floor(sceneNumber(html && html.textureHeight, 320)));
    const markup = typeof html.html === "string" ? html.html : "";
    if (!markup.trim()) {
      return "";
    }
    const bodyStyle = [
      "box-sizing:border-box",
      "width:" + width + "px",
      "min-height:" + height + "px",
      "font:14px system-ui,-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif",
      "color:#fff",
    ].join(";");
    const svg = [
      '<svg xmlns="http://www.w3.org/2000/svg" width="' + width + '" height="' + height + '" viewBox="0 0 ' + width + " " + height + '">',
      '<foreignObject x="0" y="0" width="100%" height="100%">',
      '<div xmlns="http://www.w3.org/1999/xhtml" style="' + bodyStyle + '">',
      markup,
      "</div></foreignObject></svg>",
    ].join("");
    return "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svg);
  }

  function rasterizeSceneHTMLTextureEntry(textureState, html, element, index) {
    if (!textureState || !textureState.records || !html || normalizeSceneHTMLMode(html.mode, "dom") !== "texture") {
      return false;
    }
    const id = sceneHTMLTextureLifecycleID(html, index || 0);
    const record = textureState.records.get(id);
    if (!record || html.textureOverBudget || !record.dirty) {
      return false;
    }
    const textureKey = sceneHTMLTextureDataURL(html);
    if (!textureKey) {
      record.manager = "unavailable";
      return false;
    }
    record.sourceKey = html.textureKey || ("gosx-html://" + id);
    record.textureKey = textureKey;
    record.manager = "svg-foreignobject";
    record.rasterized = true;
    record.ready = true;
    record.dirty = false;
    record.dirtyBytes = 0;
    record.pendingUploadBytes = 0;
    record.uploadBytes = Math.max(0, Math.floor(sceneNumber(html.textureBytes, 0)));
    html.textureKey = textureKey;
    html.textureReady = true;
    html.textureManager = record.manager;
    html.textureRasterized = true;
    html.textureDirty = false;
    html.textureDirtyBytes = 0;
    html.texturePendingUploadBytes = 0;
    html.textureUploadBytes = record.uploadBytes;
    if (html.fallbackReason === "html-texture-manager-unavailable" || !html.fallbackReason) {
      html.fallbackReason = "html-texture-accessibility-mirror";
    }
    if (element) {
      element.__gosxHTMLTextureKey = textureKey;
    }
    if (typeof textureState.requestRender === "function") {
      textureState.requestRender("html-texture");
    }
    return true;
  }

  function applySceneHTMLTextureRecordsToState(sceneState, textureState) {
    if (!sceneState || !textureState || !textureState.records || typeof sceneStateHTML !== "function") {
      return;
    }
    const entries = sceneStateHTML(sceneState);
    for (let index = 0; index < entries.length; index += 1) {
      const entry = entries[index];
      if (!entry || normalizeSceneHTMLMode(entry.mode, "dom") !== "texture") {
        continue;
      }
      const record = textureState.records.get(sceneHTMLTextureLifecycleID(entry, index));
      if (!record || !record.ready || !record.textureKey) {
        continue;
      }
      entry.textureKey = record.textureKey;
      entry.textureReady = true;
      entry.textureManager = record.manager || "";
      entry.textureRasterized = Boolean(record.rasterized);
      entry.textureUploadBytes = record.uploadBytes || 0;
      entry.textureDirty = false;
      entry.textureDirtyBytes = 0;
      entry.texturePendingUploadBytes = 0;
      if (entry.fallbackReason === "html-texture-manager-unavailable" || !entry.fallbackReason) {
        entry.fallbackReason = "html-texture-accessibility-mirror";
      }
    }
  }

  function setSceneHTMLTextureLayerAttrs(layer, textureStats, entryCount) {
    setAttrValue(layer, "aria-hidden", entryCount > 0 ? "false" : "true");
    setAttrValue(layer, "data-gosx-scene-html-texture-count", textureStats.count > 0 ? textureStats.count : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-ready", textureStats.ready > 0 ? textureStats.ready : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-bytes", textureStats.bytes > 0 ? textureStats.bytes : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-cap-bytes", textureStats.capBytes > 0 ? textureStats.capBytes : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-over-budget", textureStats.overBudget > 0 ? textureStats.overBudget : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-dirty", textureStats.dirty > 0 ? textureStats.dirty : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-dirty-bytes", textureStats.dirtyBytes > 0 ? textureStats.dirtyBytes : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-upload-pending-bytes", textureStats.pendingUploadBytes > 0 ? textureStats.pendingUploadBytes : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-disposed", textureStats.disposed > 0 ? textureStats.disposed : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-disposed-bytes", textureStats.disposedBytes > 0 ? textureStats.disposedBytes : "");
    setAttrValue(layer, "data-gosx-scene-html-texture-revision", textureStats.revision > 0 ? textureStats.revision : "");
  }

  function renderSceneHTML(layer, bundle, elements, width, height, textureState) {
    if (!layer) {
      return;
    }
    const entries = prepareSceneHTMLEntries(bundle, width, height);
    const textureLifecycle = syncSceneHTMLTextureState(textureState, entries);
    const textureStats = sceneHTMLTextureStats(entries, textureLifecycle);
    setSceneHTMLTextureLayerAttrs(layer, textureStats, entries.length);
    const active = new Set();
    let rasterizedAny = false;
    for (const entry of entries) {
      const id = entry.id;
      active.add(id);
      let element = elements.get(id);
      if (!element) {
        element = document.createElement("div");
        layer.appendChild(element);
        elements.set(id, element);
      }
      renderSceneHTMLElement(element, entry.html, entry.box, entry.hidden, entry.occluded);
      if (rasterizeSceneHTMLTextureEntry(textureState, entry.html, element, entry.order)) {
        rasterizedAny = true;
        renderSceneHTMLElement(element, entry.html, entry.box, entry.hidden, entry.occluded);
      }
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
    if (rasterizedAny) {
      const nextLifecycle = syncSceneHTMLTextureState(textureState, entries);
      setSceneHTMLTextureLayerAttrs(layer, sceneHTMLTextureStats(entries, nextLifecycle), entries.length);
    }
  }

