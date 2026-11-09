(() => {
  if (window.__gosxKitUI) return;
  window.__gosxKitUI = true;
  // Turns clicks and input changes on kit elements into one "gosx:kit" event
  // that bubbles from the element. detail = {kind, name, value}.
  const send = (el, kind, name, value) =>
    el.dispatchEvent(new CustomEvent('gosx:kit', { bubbles: true, detail: { kind, name, value } }));
  const read = (el) => el.type === 'checkbox' ? String(el.checked) : el.value;
  document.addEventListener('click', (e) => {
    const t = e.target.closest && e.target.closest('[data-kit-action],[data-kit-rebind]');
    if (!t || t.disabled) return;
    if (t.dataset.kitRebind) send(t, 'rebind', t.dataset.kitRebind, '');
    else send(t, 'action', t.dataset.kitAction, t.dataset.kitValue || '');
  });
  const change = (e) => {
    const t = e.target.closest && e.target.closest('[data-kit-setting]');
    if (!t) return;
    const out = t.parentElement && t.parentElement.querySelector('.kit-value');
    if (out && t.type === 'range') out.textContent = t.value + (t.dataset.kitUnit || '');
    send(t, 'setting', t.dataset.kitSetting, read(t));
  };
  document.addEventListener('input', (e) => { if (e.target.type === 'range') change(e); });
  document.addEventListener('change', change);
  // Keep Tab inside a modal panel.
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Tab') return;
    const modal = e.target.closest && e.target.closest('[data-kit-modal]');
    if (!modal) return;
    const items = [...modal.querySelectorAll('button:not(:disabled),input,select,[tabindex="0"]')];
    if (!items.length) return;
    const first = items[0], last = items[items.length - 1];
    if (e.shiftKey && document.activeElement === first) { last.focus(); e.preventDefault(); }
    else if (!e.shiftKey && document.activeElement === last) { first.focus(); e.preventDefault(); }
  });
})();
