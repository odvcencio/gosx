// Accessible DOM controls that mirror the existing Scene3D ray picker.
// The mount owns calling syncSceneNodeFocusProxies with each current render bundle.
function setupSceneNodeFocusProxies(layer) {
  if (!layer) return null;
  layer.style.position = "absolute";
  layer.style.inset = "0";
  layer.style.width = "auto";
  layer.style.height = "auto";
  layer.style.overflow = "hidden";
  layer.style.clipPath = "inset(50%)";
  layer.style.pointerEvents = "none";
  return { layer: layer, elements: new Map(), hoverID: "", pressedID: "", disposed: false };
}

function sceneFocusCandidates(bundle) {
  if (!bundle || typeof bundle !== "object") return [];
  const candidates = [];
  const seen = new Set();
  const pools = [bundle.meshObjects, bundle.objects, bundle.models];
  for (const pool of pools) {
    if (!Array.isArray(pool)) continue;
    for (const node of pool) {
      if (!node || typeof node !== "object" || node.interactive !== true) continue;
      const id = typeof node.id === "string" ? node.id.trim() : "";
      const label = typeof node.label === "string" ? node.label.trim() : "";
      if (!id || !label || seen.has(id)) continue;
      seen.add(id);
      const rawOrder = Number(node.interactiveOrder);
      const order = Number.isFinite(rawOrder) && rawOrder > 0 ? rawOrder : Number.MAX_SAFE_INTEGER;
      candidates.push({ id: id, label: label, order: order });
    }
  }
  candidates.sort(function(left, right) { return left.order - right.order; });
  return candidates;
}

function sceneFocusDispatch(element, type) {
  if (!element || typeof element.dispatchEvent !== "function") return;
  let event;
  if (typeof Event === "function") {
    event = new Event(type, { bubbles: true, cancelable: true });
  } else {
    event = { type: type, bubbles: true, cancelable: true };
  }
  element.dispatchEvent(event);
}

function sceneFocusSetHover(state, id) {
  if (!state || state.hoverID === id) return;
  const previous = state.elements.get(state.hoverID);
  if (previous) {
    previous.removeAttribute("data-hover");
    sceneFocusDispatch(previous, "pointerleave");
  }
  state.hoverID = id || "";
  const next = state.elements.get(state.hoverID);
  if (next) {
    next.setAttribute("data-hover", "true");
    sceneFocusDispatch(next, "pointerenter");
  }
}

function syncSceneNodeFocusProxies(state, bundle) {
  if (!state || state.disposed) return;
  const candidates = sceneFocusCandidates(bundle);
  if (candidates.length === 0 && state.elements.size === 0) return;
  const nextIDs = new Set(candidates.map(function(item) { return item.id; }));
  for (const [id, element] of state.elements) {
    if (nextIDs.has(id)) continue;
    if (element.parentNode === state.layer) state.layer.removeChild(element);
    state.elements.delete(id);
    if (state.hoverID === id) state.hoverID = "";
    if (state.pressedID === id) state.pressedID = "";
  }
  for (let index = 0; index < candidates.length; index += 1) {
    const item = candidates[index];
    let element = state.elements.get(item.id);
    if (!element) {
      element = document.createElement("button");
      element.setAttribute("type", "button");
      element.setAttribute("role", "button");
      element.setAttribute("data-gosx-scene-node", item.id);
      element.style.pointerEvents = "none";
      element.style.position = "absolute";
      element.style.width = "1px";
      element.style.height = "1px";
      element.style.padding = "0";
      element.style.border = "0";
      element.style.margin = "-1px";
      element.addEventListener("focus", function() { sceneFocusSetHover(state, item.id); });
      element.addEventListener("blur", function() {
        if (state.hoverID === item.id) sceneFocusSetHover(state, "");
      });
      element.addEventListener("keydown", function(event) {
        if (event.key !== "Enter" && event.key !== " ") return;
        if (typeof event.preventDefault === "function") event.preventDefault();
        sceneFocusDispatch(element, "click");
      });
      state.elements.set(item.id, element);
    }
    // Keep DOM order equal to interactive order so Tab follows scene order.
    const atPosition = state.layer.children && state.layer.children[index];
    if (atPosition !== element) state.layer.insertBefore(element, atPosition || null);
    element.setAttribute("aria-label", item.label);
    element.setAttribute("tabindex", "0");
  }
}

function dispatchSceneNodeFocusPointer(state, detail) {
  if (!state || state.disposed || !detail) return;
  const type = typeof detail.type === "string" ? detail.type : "";
  const targetID = typeof detail.targetID === "string" ? detail.targetID.trim() : "";
  if (type === "move") {
    sceneFocusSetHover(state, targetID);
  } else if (type === "leave") {
    if (!targetID || state.hoverID === targetID) sceneFocusSetHover(state, "");
  } else if (type === "down") {
    const element = state.elements.get(targetID);
    if (element) {
      state.pressedID = targetID;
      element.setAttribute("data-pressed", "true");
      sceneFocusDispatch(element, "pointerdown");
    }
  } else if (type === "up" || type === "cancel") {
    const pressedID = state.pressedID || targetID;
    const element = state.elements.get(pressedID);
    if (element) {
      element.removeAttribute("data-pressed");
      sceneFocusDispatch(element, type === "up" ? "pointerup" : "pointercancel");
      if (type === "up" && detail.clicked === true) sceneFocusDispatch(element, "click");
    }
    state.pressedID = "";
  }
}

function disposeSceneNodeFocusProxies(state) {
  if (!state || state.disposed) return;
  state.disposed = true;
  for (const element of state.elements.values()) {
    if (element.parentNode === state.layer) state.layer.removeChild(element);
  }
  state.elements.clear();
  state.hoverID = "";
  state.pressedID = "";
}
