// Drives the island half of the calls test. It never touches the worker: it
// clicks island buttons and records what the island DOM shows, as the user
// would see it. Results land in window.__calls for the Go test to check.
//
// Query flags: crash=1 also presses the crash button at the end.
export async function init(page) {
  await page.addInitScript(() => {
    window.__calls = { states: [], results: [], renders: 0, steps: [], box: [], late: [] };
    // Every value the page's store holds for the mailbox channel's result signal,
    // as a subscriber sees it. An island renders only the newest, so this is how
    // the test knows each reply reached the page.
    const record = () => {
      const subscribe = window.__gosx_subscribe_shared_signal;
      if (typeof subscribe !== "function") { setTimeout(record, 20); return; }
      subscribe("$bv.box.result", value => window.__calls.box.push(JSON.parse(JSON.stringify(value))), { immediate: false });
      subscribe("$bv.late.result", value => window.__calls.late.push(JSON.parse(JSON.stringify(value))), { immediate: false });
    };
    document.addEventListener("DOMContentLoaded", record);
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

  // The mailbox channel (bv.box), served on goroutines. One island handler
  // writes two calls; the engine runs both and the page gets both replies.
  const seen = () => page.evaluate(() => window.__calls.box.map(r => [r.ID, r.Value && r.Value.Total, r.Error]));
  await page.click("#pair");
  await waitText(page, "recent", /^1=3\/;2=30\/;$/);
  await waitText(page, "pending", /^0$/);
  await step(page, "pair", { box: await text(page, "box"), recent: await text(page, "recent"), pending: await text(page, "pending"), seen: await seen() });
  // A handler that waits for a promise only the event loop can settle.
  await page.click("#wait");
  await waitText(page, "box", /^3:121::(worker|main)$/);
  await step(page, "promise", { box: await text(page, "box"), seen: await seen() });
  // Call 4 takes 150 ms and call 5 is quick; the engine runs them in order, so
  // the page gets 4 before 5.
  await page.click("#ordered");
  await waitText(page, "box", /^5:10::(worker|main)$/);
  await step(page, "ordered", { box: await text(page, "box"), recent: await text(page, "recent"), pending: await text(page, "pending"), seen: await seen() });
  if (mode !== "worker") return;

  // The page times out two slow calls. Call 1's late reply puts the result's ID
  // back to 1 while call 2 still runs. The island writes a call then and numbers it
  // from Seen: 2 was watched by the page and accepted by the engine, so the new
  // call must be 3. A call that reused 2 would never run, and no reply would ever
  // carry its answer.
  await page.click("#latepair");
  await waitText(page, "late", /^2:0:timeout:2$/);
  await step(page, "late-timeouts", await text(page, "late"));
  await waitText(page, "late", /^1:901::\d+$/, 15000);
  await step(page, "late-reply", await text(page, "late"));
  await page.click("#latenext");
  await waitText(page, "late", /^3:15::3$/, 15000);
  await step(page, "late", { final: await text(page, "late"), seen: await page.evaluate(() => window.__calls.late.map(r => [r.ID, r.Value && r.Value.Total, r.Error, r.Seen])) });

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
