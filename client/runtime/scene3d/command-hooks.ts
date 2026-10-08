// Shared lifecycle adapters, installed by the demand-loaded command chunk.
(() => {
  const sceneAPI = window.__gosx_scene3d_api;
  if (sceneAPI.addCommandHook) return;
  // One adapter per handle, shared by optional presentation features. Lower
  // orders run first, regardless of the order in which chunks were loaded.
  const commandHooks = new WeakMap<object, any>();
  function addCommandHook(mount: any, handle: any, name: string, order: number,
    beforeCommands: (commands: any[]) => void, dispose: () => void, alive: () => boolean) {
    let rec = commandHooks.get(handle);
    if (!rec) {
      const apply = handle.applyCommands, teardown = handle.dispose;
      rec = { hooks: new Map(), ordered: [], observer: null };
      const close = () => {
        if (!commandHooks.delete(handle)) return;
        rec.observer?.disconnect();
        if (handle.applyCommands === rec.commands) handle.applyCommands = apply;
        if (handle.dispose === rec.dispose) handle.dispose = teardown;
        const hooks = rec.ordered;
        rec.hooks.clear(); rec.ordered = [];
        let failure;
        for (const hook of hooks) {
          try { hook.dispose(); } catch (error) { failure ??= error; }
        }
        if (failure) throw failure;
      };
      rec.commands = handle.applyCommands = function(commands: any[]) {
        for (const hook of rec.ordered) hook.beforeCommands(commands);
        return apply.call(handle, commands);
      };
      rec.dispose = handle.dispose = function() { try { close(); } finally { teardown?.apply(handle, arguments); } };
      rec.apply = (commands: any[]) => apply.call(handle, commands);
      if (typeof MutationObserver !== 'undefined') {
        rec.observer = new MutationObserver(() => { if (!alive() || !mount.isConnected) close(); });
        rec.observer.observe(mount, { attributes: true, attributeFilter: ['data-gosx-scene3d-command-ready'] });
        if (mount.parentNode) rec.observer.observe(mount.parentNode, { childList: true });
      }
      commandHooks.set(handle, rec);
    }
    rec.hooks.set(name, { order, beforeCommands, dispose });
    rec.ordered = Array.from(rec.hooks.values()).sort((a: any, b: any) => a.order - b.order);
    return rec.apply;
  }
  sceneAPI.addCommandHook = addCommandHook;
})();
