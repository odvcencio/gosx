(() => {
  if (window.__gosxBootWatchdog) return;
  window.__gosxBootWatchdog = true;
  const attach = () => {
    const root = document.querySelector('[data-boot-root]');
    if (!root || root.__gosxBoot) return !!root && !!root.__gosxBoot;
    root.__gosxBoot = true;
    const budget = Number(root.dataset.bootBudgetMs) || 12000;
    const key = 'gosx-boot:' + location.pathname + location.search;
    let remaining = budget, began = 0, timer = 0, spent = false, stopped = false;
    const store = {
      get() { try { return sessionStorage.getItem(key); } catch (e) { return null; } },
      set() { try { sessionStorage.setItem(key, 'retried'); return true; } catch (e) { return false; } },
      del() { try { sessionStorage.removeItem(key); } catch (e) {} },
    };
    const text = (sel, value) => { const el = root.querySelector(sel); if (el && value) el.textContent = value; };
    const show = (state) => {
      if (root.dataset.boot === 'ready') return;
      root.dataset.boot = state;
      text('[data-boot-title]', root.dataset['boot' + state[0].toUpperCase() + state.slice(1) + 'Title']);
      text('[data-boot-copy]', root.dataset['boot' + state[0].toUpperCase() + state.slice(1) + 'Copy']);
    };
    const stop = () => { stopped = true; clearTimeout(timer); document.removeEventListener('visibilitychange', onVisible); mo.disconnect(); };
    const check = () => {
      if (stopped) return;
      if (!root.isConnected) { stop(); return; }
      if (root.dataset.boot === 'ready') { store.del(); stop(); }
    };
    const recover = () => {
      if (stopped || spent) return;
      // The Go gate may already have used the automatic reload.
      if (store.get() !== 'retried' && store.set()) {
        show('recovering'); stop();
        setTimeout(() => { if (root.isConnected) location.reload(); }, 250);
      } else {
        spent = true; clearTimeout(timer); began = 0;
        show('failed');
      }
    };
    const schedule = () => {
      if (stopped || spent || document.visibilityState === 'hidden') return;
      began = Date.now(); timer = setTimeout(recover, remaining);
    };
    function onVisible() {
      if (document.visibilityState === 'hidden') {
        if (began) remaining = Math.max(0, remaining - (Date.now() - began));
        began = 0; clearTimeout(timer);
      } else { check(); schedule(); }
    }
    const retry = root.querySelector('[data-boot-retry]');
    if (retry) retry.addEventListener('click', () => { store.del(); location.reload(); });
    const mo = new MutationObserver(check);
    mo.observe(root, { attributes: true, attributeFilter: ['data-boot'] });
    document.addEventListener('visibilitychange', onVisible);
    check(); schedule();
    return true;
  };
  // The parser may deliver the root after this script, so watch until found.
  if (attach()) return;
  const find = new MutationObserver(() => { if (attach()) find.disconnect(); });
  find.observe(document.documentElement, { childList: true, subtree: true });
})();
