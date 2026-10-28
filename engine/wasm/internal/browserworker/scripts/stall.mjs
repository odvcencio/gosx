// Drives the stall test. The page writes a call for a worker engine and then
// keeps its own main thread busy for longer than the channel's timeout, so the
// page cannot take the call's mark, or report it, until the stall ends. The
// server the test runs counts how often each call's handler ran (see
// stall_browser_test.go); this script records what the page saw. Results land in
// window.__calls.
export async function init(page) {
  await page.addInitScript(() => {
    window.__calls = { states: [], steps: [], replies: [] };
    // Every value the page's store holds for the result signal, as a subscriber
    // sees it. An island renders only the newest, so this is how the test knows
    // each reply the page was given.
    const record = () => {
      const subscribe = window.__gosx_subscribe_shared_signal;
      if (typeof subscribe !== "function") { setTimeout(record, 20); return; }
      subscribe("$bv.stall.result", value => window.__calls.replies.push(JSON.parse(JSON.stringify(value))), { immediate: false });
    };
    document.addEventListener("DOMContentLoaded", record);
    const watch = () => {
      const state = document.getElementById("state");
      if (!state) { setTimeout(watch, 20); return; }
      const push = () => { const v = state.textContent || ""; const list = window.__calls.states; if (list[list.length - 1] !== v) list.push(v); };
      push();
      new MutationObserver(push).observe(document.body, { childList: true, subtree: true, characterData: true });
    };
    document.addEventListener("DOMContentLoaded", watch);
  });
}

const waitText = (page, id, re, timeout = 15000) =>
  page.waitForFunction(([i, source]) => new RegExp(source).test((document.getElementById(i) || {}).textContent || ""), [id, re.source], { timeout, polling: 20 });
const step = (page, name, value) => page.evaluate(([n, v]) => window.__calls.steps.push({ name: n, value: v, at: performance.now() }), [name, value]);
const effects = page => page.evaluate(() => fetch("/probe/effects").then(r => r.json()));
const result = () => window.__gosx.sharedSignals.values.get("$bv.stall.result");

export async function run(page) {
  await waitText(page, "state", /^running\/worker\//);

  // Control: a call written while the page is free runs once.
  await page.evaluate(() => window.__gosx_set_shared_signal("$bv.stall", JSON.stringify({ ID: 1, Name: "control", Args: { A: 1, B: 1 } })));
  await page.waitForFunction(() => { const r = window.__gosx.sharedSignals.values.get("$bv.stall.result"); return r && r.ID === 1; }, null, { timeout: 15000, polling: 20 });
  await step(page, "control", await effects(page));

  // The page writes call 2 and then stays busy for 800 ms, much longer than the
  // channel's 300 ms timeout. The worker gets the call and writes its mark, but
  // the page cannot take the mark, or tell the worker so, until it is free.
  await page.evaluate(() => {
    window.__gosx_set_shared_signal("$bv.stall", JSON.stringify({ ID: 2, Name: "order", Args: { A: 20, B: 22 } }));
    const start = performance.now();
    while (performance.now() - start < 800) { /* the page's main thread is busy */ }
  });
  // The page's own watchdog may write "timeout" for call 2 once it is free. Wait
  // for any other answer, then give a second run of the call time to show.
  await page.waitForFunction(() => {
    const r = window.__gosx.sharedSignals.values.get("$bv.stall.result");
    return r && r.ID === 2 && r.Error !== "timeout";
  }, null, { timeout: 15000, polling: 20 });
  await page.waitForTimeout(400);
  await step(page, "result", await page.evaluate(result));
  await step(page, "effects", await effects(page));
  await step(page, "done", true);
}
