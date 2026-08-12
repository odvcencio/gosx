// @ts-check
// GoSX browser host: complete delegated event transport.
// 30a — delegated DOM event listeners for island roots.
//
// Chunks: bootstrap.js, bootstrap-feature-islands.js.
// Reads from the enclosing IIFE: window.__gosx, the island registry.
// Publishes: gosxHost.events.setup plus the event extraction/dispatch helpers.
  // --------------------------------------------------------------------------
  // Event delegation
  // --------------------------------------------------------------------------

  // Event types that may be delegated on each island root element. A modern
  // manifest declares the subset an island uses; older manifests attach all.
  const DELEGATED_EVENTS = [
    "click", "input", "change", "submit",
    "keydown", "keyup", "focus", "blur",
    "dragstart", "dragend", "dragover", "dragleave", "drop",
    "pointerdown", "pointermove", "pointerup", "pointercancel",
  ];

  // Convention-based events that intentionally escape an island root. Their
  // marker still lives on an element inside the island, but the underlying
  // listener belongs to document/window and is removed from that same target.
  const GLOBAL_DELEGATED_EVENTS = [
    { marker: "document-keydown", type: "keydown", target: document },
    { marker: "document-keyup", type: "keyup", target: document },
    { marker: "window-resize", type: "resize", target: window },
  ];

  // DataTransfer is an external input boundary. Authored
  // data-gosx-event-value strings are trusted application markup and are not
  // subject to this cap; only text/plain read from a drop is bounded before it
  // can enter JSON/WASM.
  const MAX_EXTERNAL_DROP_TEXT_BYTES = 64 * 1024;

  // Extract a small payload from a DOM event for forwarding to WASM.
  function extractEventData(e, handlerElement) {
    const data = { type: e.type };
    const target = e.target;

    switch (e.type) {
      case "click":
        if (target && target.value !== undefined) {
          const value = String(target.value == null ? "" : target.value);
          if (value !== "") data.value = value;
        }
        break;
      case "input":
      case "change":
        if (target && target.value !== undefined) {
          const value = String(target.value == null ? "" : target.value);
          if (value !== "") data.value = value;
        }
        break;
      case "keydown":
      case "keyup":
        if (e.key) data.key = String(e.key);
        if (e.code) data.code = e.code;
        break;
      case "submit":
        // The WASM handler decides when and how to submit.
        if (typeof e.preventDefault === "function") e.preventDefault();
        break;
      // click, focus, blur and pointer/drag events are completed below.
    }

    if (target) {
      if (target.checked === true) data.checked = true;
      if (typeof target.selectedIndex === "number" && target.selectedIndex !== 0) {
        data.selectedIndex = target.selectedIndex;
      }
      if (target.id) data.targetID = String(target.id);
      if (eventTargetEditable(target)) data.editable = true;
    }
    if (handlerElement && handlerElement.id) {
      data.currentTargetID = String(handlerElement.id);
    }
    if (e.ctrlKey) data.ctrlKey = true;
    if (e.metaKey) data.metaKey = true;
    if (e.altKey) data.altKey = true;
    if (e.shiftKey) data.shiftKey = true;
    if (e.repeat) data.repeat = true;
    // Keyboard timeStamp is retained even when zero: route chord timing
    // depends on the browser clock and cannot reconstruct it later. Other
    // event families omit it to keep their common payloads minimal.
    if (e.type === "keydown" || e.type === "keyup") {
      copyNumberField(data, e, "timeStamp", "timeStamp", true);
    }
    const pointerEvent = e.type.indexOf("pointer") === 0;
    const mouseEvent = pointerEvent || e.type === "click" || e.type === "drop" || e.type.indexOf("drag") === 0;
    if (pointerEvent) {
      copyNumberField(data, e, "pointerId", "pointerID");
      if (e.pointerType) data.pointerType = String(e.pointerType);
      if (e.isPrimary === true) data.isPrimary = true;
      copyNumberField(data, e, "pressure", "pressure");
      copyNumberField(data, e, "width", "width");
      copyNumberField(data, e, "height", "height");
    }
    if (mouseEvent) {
      copyNumberField(data, e, "clientX", "clientX");
      copyNumberField(data, e, "clientY", "clientY");
      copyNumberField(data, e, "button", "button");
      copyNumberField(data, e, "buttons", "buttons");
    }
    if (e.type === "resize") {
      if (typeof window.innerWidth === "number" && window.innerWidth !== 0) data.width = window.innerWidth;
      if (typeof window.innerHeight === "number" && window.innerHeight !== 0) data.height = window.innerHeight;
    }

    const dataset = handlerDataset(handlerElement);
    if (dataset) data.data = dataset;
    const authoredEventData = handlerElement && handlerElement.getAttribute
      ? handlerElement.getAttribute("data-gosx-event-value")
      : null;
    if (authoredEventData !== null) data.eventData = authoredEventData;
    if (e.type === "dragstart" && authoredEventData !== null && e.dataTransfer && typeof e.dataTransfer.setData === "function") {
      e.dataTransfer.setData("text/plain", authoredEventData);
    }
    if (e.type === "drop" && e.dataTransfer && typeof e.dataTransfer.getData === "function") {
      const transferred = e.dataTransfer.getData("text/plain");
      if (transferred !== "" && externalDropTextWithinLimit(transferred)) data.eventData = transferred;
    }

    return data;
  }

  function copyNumberField(data, event, sourceName, targetName, preserveZero) {
    const value = event && event[sourceName];
    if (typeof value === "number" && Number.isFinite(value) && (preserveZero || value !== 0)) {
      data[targetName] = value;
    }
  }

  function externalDropTextWithinLimit(text) {
    let bytes = 0;
    for (let index = 0; index < text.length; index++) {
      const code = text.charCodeAt(index);
      if (code < 0x80) bytes += 1;
      else if (code < 0x800) bytes += 2;
      else if (code >= 0xd800 && code <= 0xdbff && index + 1 < text.length &&
        text.charCodeAt(index + 1) >= 0xdc00 && text.charCodeAt(index + 1) <= 0xdfff) {
        bytes += 4;
        index++;
      } else bytes += 3;
      if (bytes > MAX_EXTERNAL_DROP_TEXT_BYTES) return false;
    }
    return true;
  }

  function eventTargetEditable(target) {
    if (!target) return false;
    if (target.isContentEditable || target.contentEditable === "true") return true;
    const tag = String(target.tagName || "").toLowerCase();
    return tag === "input" || tag === "textarea" || tag === "select";
  }

  function handlerDataset(element) {
    if (!element || !element.dataset) return null;
    // Framework-owned data-gosx-* markers describe wiring, not application
    // data. Excluding them keeps application datasets compact and intentional.
    const keys = Object.keys(element.dataset).filter(function(key) {
      return !key.startsWith("gosx");
    });
    if (keys.length === 0) return null;
    const data = {};
    for (const key of keys) data[key] = String(element.dataset[key]);
    return data;
  }

  function findHandlerForEvent(target, root, eventType) {
    if (!elementOwnedByIslandRoot(target, root)) return null;
    const specificAttr = handlerAttrName(eventType);
    let el = target;
    while (el) {
      const handlerName = elementHandlerName(el, eventType, specificAttr);
      if (handlerName) return { name: handlerName, element: el };
      if (el === root) break;
      el = el.parentNode;
    }
    return null;
  }

  function elementOwnedByIslandRoot(element, root) {
    let current = element;
    while (current) {
      if (current === root) return true;
      if (hasAttributeName(current, "data-gosx-island")) return false;
      current = current.parentNode;
    }
    return false;
  }

  function handlerAttrName(eventType) {
    return "data-gosx-on-" + eventType;
  }

  function elementHandlerName(el, eventType, specificAttr) {
    if (hasAttributeName(el, specificAttr)) return el.getAttribute(specificAttr);
    if (eventType === "click" && hasAttributeName(el, "data-gosx-handler")) {
      return el.getAttribute("data-gosx-handler");
    }
    return null;
  }

  function hasAttributeName(el, attr) {
    return Boolean(el && el.hasAttribute && el.hasAttribute(attr));
  }

  function setupEventDelegation(islandRoot, islandID, eventSlots) {
    const entries = [];
    const declared = delegatedEventSet(eventSlots);

    for (const eventType of DELEGATED_EVENTS) {
      if (declared && !declared.has(eventType)) continue;
      const listener = createDelegatedListener(islandRoot, islandID, eventType);
      const useCapture = delegatedEventCapture(eventType);
      islandRoot.addEventListener(eventType, listener, useCapture);
      entries.push({ target: islandRoot, type: eventType, listener, capture: useCapture });
    }

    for (const config of GLOBAL_DELEGATED_EVENTS) {
      if (declared && !declared.has(config.marker)) continue;
      if (!findGlobalHandler(islandRoot, config.marker)) continue;
      const listener = createGlobalDelegatedListener(islandRoot, islandID, config);
      config.target.addEventListener(config.type, listener, false);
      entries.push({ target: config.target, type: config.type, listener, capture: false });
    }

    return entries;
  }

  // A missing events property means a pre-selective-listener manifest. An
  // explicit array, including an empty one, is authoritative and de-duplicated.
  function delegatedEventSet(eventSlots) {
    if (!Array.isArray(eventSlots)) return null;
    const declared = new Set();
    for (const slot of eventSlots) {
      const eventType = normalizeDelegatedEventName(slot && slot.eventType);
      if (eventType) declared.add(eventType);
    }
    return declared;
  }

  function normalizeDelegatedEventName(value) {
    const eventType = String(value || "").trim().replace(/[_\s]+/g, "-").toLowerCase();
    const compact = eventType.replace(/-/g, "");
    if (compact === "documentkeydown") return "document-keydown";
    if (compact === "documentkeyup") return "document-keyup";
    if (compact === "windowresize") return "window-resize";
    return eventType;
  }

  function findGlobalHandler(root, marker) {
    const attr = "data-gosx-on-" + marker;
    let element = hasAttributeName(root, attr) ? root : null;
    if (!element && root && typeof root.querySelectorAll === "function") {
      try {
        const matches = root.querySelectorAll("[" + attr + "]");
        for (let index = 0; index < matches.length; index++) {
          if (elementOwnedByIslandRoot(matches[index], root)) {
            element = matches[index];
            break;
          }
        }
      } catch (_) {
        element = null;
      }
    }
    if (!element) return null;
    const name = element.getAttribute(attr);
    return name ? { name, element } : null;
  }

  function delegatedEventCapture(eventType) {
    return eventType === "focus" || eventType === "blur";
  }

  function createDelegatedListener(islandRoot, islandID, eventType) {
    return function(e) {
      if (e.__gosx_handled) return;
      const match = findHandlerForEvent(e.target, islandRoot, eventType);
      if (!match) return;
      if ((eventType === "dragover" || eventType === "drop") && typeof e.preventDefault === "function") {
        e.preventDefault();
      }
      e.__gosx_handled = true;
      dispatchIslandAction(islandID, match.name, extractEventData(e, match.element), e, match.element);
    };
  }

  function createGlobalDelegatedListener(islandRoot, islandID, config) {
    return function(e) {
      // Global conventions fan out to every registered island. A regular
      // __gosx_handled marker only prevents duplicate root delegation; an
      // explicit browser.StopPropagation sets this separate fanout stop bit.
      if (e.__gosx_stop_island_fanout) return;
      const match = findGlobalHandler(islandRoot, config.marker);
      if (!match) return;
      e.__gosx_handled = true;
      dispatchIslandAction(islandID, match.name, extractEventData(e, match.element), e, match.element);
    };
  }

  function dispatchIslandAction(islandID, handlerName, eventData, event, handlerElement) {
    const actionFn = window.__gosx_action;
    if (typeof actionFn !== "function") return;

    const previousEvent = gosxHostCompatibility.read("__gosx_current_event");
    const previousHandler = gosxHostCompatibility.read("__gosx_current_handler");
    gosxHostCompatibility.install("__gosx_current_event", event);
    gosxHostCompatibility.install("__gosx_current_handler", handlerElement);
    try {
      const result = actionFn(islandID, handlerName, JSON.stringify(eventData));
      if (typeof result === "string" && result !== "") {
        console.error(`[gosx] action error (${islandID}/${handlerName}):`, result);
      }
    } catch (err) {
      console.error(`[gosx] action error (${islandID}/${handlerName}):`, err);
    } finally {
      if (previousEvent === undefined) gosxHostCompatibility.clear("__gosx_current_event");
      else gosxHostCompatibility.install("__gosx_current_event", previousEvent);
      if (previousHandler === undefined) gosxHostCompatibility.clear("__gosx_current_handler");
      else gosxHostCompatibility.install("__gosx_current_handler", previousHandler);
    }
  }

  gosxHost.events = Object.assign(gosxHost.events || {}, {
    setup: setupEventDelegation,
    extract: extractEventData,
    dispatch: dispatchIslandAction,
  });
