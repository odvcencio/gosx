// Drives the island half of the calls test. It never touches the worker: it
// clicks island buttons and records what the island DOM shows, as the user
// would see it. Results land in window.__calls for the Go test to check.
//
// Query flags: crash=1 also presses the crash button at the end.
export async function init(page) {
  await page.addInitScript(() => {
    window.__calls = { states: [], results: [], renders: 0, steps: [] };
    const watch = () => {
      const text = id => (document.getElementById(id) || {}).textContent || "";
      const state = document.getElementById("state"), result = document.getElementById("result"), tick = document.getElementById("tick");
      if (!state || !result || !tick) { setTimeout(watch, 20); return; }
      const push = (list, value) => { if (list[list.length - 1] !== value) list.push(value); };
      push(window.__calls.states, text("state")); push(window.__calls.results, text("result"));
      new MutationObserver(() => { push(window.__calls.states, text("state")); push(window.__calls.results, text("result")); }).observe(document.body, { childList: true, subtree: true, characterData: true });
      // Count how often the view signal re-renders the tick (a text change).
      new MutationObserver(() => { window.__calls.renders++; window.__calls.lastTick = text("tick"); }).observe(tick, { childList: true, subtree: true, characterData: true });
    };
    document.addEventListener("DOMContentLoaded", watch);
  });
}

const text = (page, id) => page.evaluate(i => (document.getElementById(i) || {}).textContent || "", id);
const waitText = (page, id, re, timeout = 15000) =>
  page.waitForFunction(([i, source]) => new RegExp(source).test((document.getElementById(i) || {}).textContent || ""), [id, re.source], { timeout, polling: 20 });
const step = (page, name, value) => page.evaluate(([n, v]) => window.__calls.steps.push({ name: n, value: v, at: performance.now() }), [name, value]);

export async function run(page) {
  const url = new URL(page.url());
  const mode = url.searchParams.get("mode") || "worker";
  if (mode === "required") {
    await waitText(page, "state", /^unsupported\//);
    await page.waitForTimeout(300);
    await step(page, "required-disabled", await page.evaluate(() => document.getElementById("add").disabled));
    return;
  }
  await waitText(page, "state", /^running\//);
  await waitText(page, "tick", /^[3-9]|^\d\d+/);

  // Pace: the engine ticks every 10 ms; the page should see about one render per 100 ms.
  const before = await page.evaluate(() => window.__calls.renders);
  await page.waitForTimeout(1000);
  const after = await page.evaluate(() => ({ renders: window.__calls.renders, tick: Number(window.__calls.lastTick) }));
  await step(page, "pace", { renders: after.renders - before, lastTick: after.tick });

  await page.click("#add");
  await waitText(page, "result", /^1:5::(worker|main)$/);
  await step(page, "add", await text(page, "result"));
  await page.click("#fail");
  await waitText(page, "result", /^2:0:refused:/);
  await step(page, "fail", await text(page, "result"));
  if (mode !== "worker") return;

  await page.click("#slow");
  await waitText(page, "result", /^3:0:timeout:/);
  await step(page, "timeout", await text(page, "result"));
  // The busy worker finishes later and its real reply replaces the timeout.
  await waitText(page, "result", /^3:-1::worker$/, 15000);
  await step(page, "late-reply", await text(page, "result"));
  await page.click("#add");
  await waitText(page, "result", /^4:5::worker$/);

  await page.click("#crash");
  await waitText(page, "state", /^crashed\/worker\//);
  await waitText(page, "result", /^5:0:worker-crashed:/);
  await step(page, "crash", { state: await text(page, "state"), result: await text(page, "result"), addDisabled: await page.evaluate(() => document.getElementById("add").disabled) });
}
