// @ts-check
// Opaque server DOM adoption. No caller content enters the VM or patch stream.
(function() {
  "use strict";
  const gosxHost = window.__gosx.host;
	const script = document.currentScript;
	const loading = gosxHost.serverSlotLoading;
	if (!loading || loading.cancelled || loading.script !== script || !script || !loading.key.startsWith(script.getAttribute("data-gosx-capability-hash") + ":")) return;
  function serverSlotRuntime() {
    const api = window.__gosx && window.__gosx.runtime && window.__gosx.runtime.serverSlots;
    if (!api || api.version !== 1 || typeof api.prepare !== "function" || typeof api.commit !== "function" || typeof api.abort !== "function") {
      throw new Error("ServerSlot runtime capability unavailable");
    }
    return api;
  }

  function serverSlotInventory(entry, root) {
    const names = entry.serverSlots;
    if (!Array.isArray(names) || names.length === 0 || new Set(names).size !== names.length) throw new Error("ServerSlot invalid manifest inventory");
    const nodes = Array.from(root.querySelectorAll("[data-gosx-server-slot]"));
    if (nodes.length !== names.length) throw new Error("ServerSlot DOM inventory mismatch");
    const found = new Map();
    for (const node of nodes) {
      const name = node.getAttribute("data-gosx-server-slot");
      if (node.tagName !== "DIV" || !names.includes(name) || found.has(name) || node.closest("[data-gosx-island]") !== root) throw new Error("ServerSlot invalid DOM owner");
      if (node.querySelector("[data-gosx-island],[data-gosx-engine]")) throw new Error("ServerSlot nested owner is not yet supported");
      found.set(name, node);
    }
    return found;
  }

  function serverSlotResolve(root, path) {
    let node = root.firstElementChild || root;
    if (path === "") return node;
    if (!/^(0|[1-9][0-9]*)(\/(0|[1-9][0-9]*))*$/.test(path)) throw new Error("ServerSlot invalid patch path");
    for (const part of path.split("/")) {
      if (!node || (node.nodeType === 1 && node.hasAttribute("data-gosx-server-slot"))) throw new Error("ServerSlot patch crosses opaque boundary");
      node = node.childNodes[Number(part)];
    }
    if (!node) throw new Error("ServerSlot missing patch target");
    return node;
  }

  function validateServerSlotPatches(root, ops, inventory) {
    if (!Array.isArray(ops)) throw new Error("ServerSlot invalid patch batch");
    const current = serverSlotInventory({ serverSlots: Array.from(inventory.keys()) }, root);
    for (const [name, node] of inventory) if (current.get(name) !== node || !root.contains(node)) throw new Error("ServerSlot DOM ownership changed");
    for (const op of ops) {
      const target = serverSlotResolve(root, op.path);
      if (![0, 1, 2, 8].includes(op.kind)) throw new Error("ServerSlot structural patch requires a new ownership contract");
      for (const outlet of inventory.values()) {
        if (target === outlet && !([1, 2].includes(op.kind) && op.attrName === "hidden")) throw new Error("ServerSlot shell mutation refused");
        if (target !== outlet && target.contains && target.contains(outlet) && ![1, 2].includes(op.kind)) throw new Error("ServerSlot ancestor mutation refused");
      }
      if ([1, 2].includes(op.kind) && /^data-gosx-(server-slot|island)/.test(op.attrName || "")) throw new Error("ServerSlot owner identity mutation refused");
    }
  }

  function removeServerSlotListeners(root, listeners, removed) {
    for (const item of listeners || []) {
      (item.target || root).removeEventListener(item.type, item.listener, item.capture);
      removed.push(item);
    }
  }

  function snapshotServerSlotWrites(root, prepared, inventory) {
    const snapshots = new Map();
    const capture = node => {
      if (!snapshots.has(node)) snapshots.set(node, { attrs: new Map(), children: null, value: undefined, data: undefined });
      return snapshots.get(node);
    };
    for (const op of prepared.patches) {
      const node = serverSlotResolve(root, op.path);
      const saved = capture(node);
      if ([1, 2].includes(op.kind)) saved.attrs.set(op.attrName, node.getAttribute(op.attrName));
      if (op.kind === 0) {
        if (node.nodeType === 3) saved.data = node.data;
        else saved.children = Array.from(node.childNodes);
      }
      if (op.kind === 8) saved.value = node.value;
    }
    for (const node of inventory.values()) capture(node).attrs.set("hidden", node.getAttribute("hidden"));
    return snapshots;
  }

  function restoreServerSlotWrites(snapshots, attempt) {
    for (const [node, saved] of snapshots || []) {
      for (const [name, value] of saved.attrs) attempt(() => value === null ? node.removeAttribute(name) : node.setAttribute(name, value));
      if (saved.children) attempt(() => node.replaceChildren(...saved.children));
      if (saved.data !== undefined) attempt(() => { node.data = saved.data; });
      if (saved.value !== undefined) attempt(() => { node.value = saved.value; });
    }
  }

  function rollbackServerSlotAdoption(state) {
    const failures = [];
    const attempt = fn => { try { fn(); } catch (error) { failures.push(error); } };
    for (const item of state.listeners) attempt(() => (item.target || state.root).removeEventListener(item.type, item.listener, item.capture));
    restoreServerSlotWrites(state.snapshots, attempt);
    for (const item of state.removed) attempt(() => (item.target || state.root).addEventListener(item.type, item.listener, item.capture));
    attempt(() => state.previous ? window.__gosx.islands.set(state.id, state.previous) : window.__gosx.islands.delete(state.id));
    if (state.api && state.prepared && state.prepared.token) attempt(() => state.api.abort(state.id, state.prepared.token));
    if (failures.length) console.error("[gosx] ServerSlot owner retained; DOM restoration incomplete", failures);
  }

  function hydrateServerSlotIsland(entry, root, program) {
    let api, prepared, snapshots;
    const listeners = [];
    const removed = [];
    const previous = window.__gosx.islands.get(entry.id);
    try {
      api = serverSlotRuntime();
      const inventory = serverSlotInventory(entry, root);
      prepared = JSON.parse(api.prepare(entry.id, entry.component, JSON.stringify(entry.props || {}), program.data, program.format));
      if (prepared.error) throw new Error(prepared.error);
      if (!Array.isArray(prepared.slots) || prepared.slots.length !== inventory.size) throw new Error("ServerSlot staged inventory mismatch");
      const stagedNames = new Set();
      for (const shell of prepared.slots) {
        if (stagedNames.has(shell.name) || !inventory.has(shell.name) || typeof shell.hidden !== "boolean" || serverSlotResolve(root, shell.path) !== inventory.get(shell.name)) throw new Error("ServerSlot staged DOM mismatch");
        stagedNames.add(shell.name);
      }
      validateServerSlotPatches(root, prepared.patches, inventory);
      snapshots = snapshotServerSlotWrites(root, prepared, inventory);
      removeServerSlotListeners(root, previous && previous.listeners, removed);
      gosxHost.patch.apply(entry.id, prepared.patches);
      for (const shell of prepared.slots) inventory.get(shell.name).hidden = shell.hidden;
      gosxHost.events.setup(root, entry.id, entry.events, listeners);
      const current = serverSlotInventory(entry, root);
      for (const [name, node] of inventory) if (current.get(name) !== node) throw new Error("ServerSlot DOM changed during adoption");
      window.__gosx.islands.set(entry.id, { component: entry.component, root, listeners, serverSlots: inventory });
      const error = api.commit(entry.id, prepared.token);
      if (error) throw new Error(error);
    } catch (error) {
      rollbackServerSlotAdoption({ id: entry.id, root, previous, removed, snapshots, listeners, api, prepared });
      console.error("[gosx] ServerSlot adoption refused", error);
    }
  }

  gosxHost.serverSlots = { version: 1, hash: script.getAttribute("data-gosx-capability-hash"), hydrate: hydrateServerSlotIsland, validatePatches: validateServerSlotPatches, inventory: serverSlotInventory };
})();
