// Drives the remount test. The page writes two calls for the engine, disposes
// the engine while the first call blocks the worker, and mounts the engine
// again, as a page does that replaces an engine. The server the test runs
// counts how often each call's handler ran (see remount_browser_test.go); this
// script records what the page saw. Results land in window.__calls.
export async function init(page) {
  await page.addInitScript(() => {
    window.__calls = { states: [], steps: [], replies: [] };
    // Every value the page's store holds for the result signal, as a subscriber
    // sees it. An island renders only the newest, so this is how the test knows
    // each reply the page was given.
    const record = () => {
      const subscribe = window.__gosx_subscribe_shared_signal;
      if (typeof subscribe !== "function") { setTimeout(record, 20); return; }
      subscribe("$bv.save.result", value => window.__calls.replies.push(JSON.parse(JSON.stringify(value))), { immediate: false });
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

const text = (page, id) => page.evaluate(i => (document.getElementById(i) || {}).textContent || "", id);
const waitText = (page, id, re, timeout = 15000) =>
  page.waitForFunction(([i, source]) => new RegExp(source).test((document.getElementById(i) || {}).textContent || ""), [id, re.source], { timeout, polling: 20 });
const step = (page, name, value) => page.evaluate(([n, v]) => window.__calls.steps.push({ name: n, value: v, at: performance.now() }), [name, value]);
const effects = page => page.evaluate(() => fetch("/probe/effects").then(r => r.json()));

export async function run(page) {
  await waitText(page, "state", /^running\/worker\//);

  // One write of two calls, as an island handler makes. Call 1 does its work and
  // then blocks the worker at the gate; call 2 waits behind it.
  await page.evaluate(() => window.__gosx_set_shared_signal("$bv.save", JSON.stringify([
    { ID: 1, Name: "save", Args: { A: 1, B: 1 } },
    { ID: 2, Name: "order", Args: { A: 20, B: 22 } },
  ])));
  await page.waitForFunction(async () => {
    const e = await fetch("/probe/effects").then(r => r.json());
    return e.save === 1 && e.gate === 1;
  }, null, { timeout: 15000, polling: 50 });
  await step(page, "blocked", await effects(page));

  // The page disposes the engine. The worker is stuck at the gate, so it cannot
  // handle the request until the gate opens.
  await page.evaluate(() => window.__gosx_dispose_engine("gosx-engine-bv"));
  await page.evaluate(() => fetch("/probe/open").then(r => r.text()));
  await waitText(page, "state", /^disposed\//);
  await step(page, "disposed", await effects(page));

  // The page mounts the engine again on the same shared signals.
  await page.evaluate(() => window.__gosx_mount_late_engine_factory("WorkerFixture"));
  await waitText(page, "state", /^running\/worker\//);
  await page.waitForFunction(() => {
    const r = window.__gosx.sharedSignals.values.get("$bv.save.result");
    return r && r.ID === 2;
  }, null, { timeout: 15000, polling: 50 });
  await page.waitForTimeout(400); // a second run of call 2, if any, would show by now
  await step(page, "result", await page.evaluate(() => window.__gosx.sharedSignals.values.get("$bv.save.result")));
  await step(page, "effects", await effects(page));
  await step(page, "done", true);
}
