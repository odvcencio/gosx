// Measures how clocks behave in a tab that is really hidden. Launches a headed
// Chrome (needs a display), opens the page, then opens a second tab so the
// first one becomes hidden. Usage: node run-hidden.mjs <chrome> <url> <hiddenSeconds> <port>
// Prints samples of the page's engine reports as one JSON line.
import { spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { connect, attach, evalIn } from "./cdp.mjs";

const [chrome, url, hiddenSecondsArg, portArg] = process.argv.slice(2);
const hiddenSeconds = Number(hiddenSecondsArg || 25);
const port = Number(portArg || 9444);
const profile = mkdtempSync(join(tmpdir(), "engine-worker-hidden-"));
// No flag here disables background throttling: the point is to see the browser's defaults.
const proc = spawn(chrome, [`--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, "--no-sandbox", "--no-first-run", "--no-default-browser-check", "--disable-search-engine-choice-screen", url], { stdio: "ignore" });
const sleep = ms => new Promise(r => setTimeout(r, ms));

// Per tag: the last clock report, and the longest gap between two clock ticks
// (measured inside the engine) over the reports that arrived since the last snapshot.
const SNAPSHOT = `(() => {
  const last = {}, tickGapMax = {}, since = window.__snapIdx || 0;
  const all = window.__reports || [];
  all.forEach((r, i) => {
    if (r.kind !== "clock") return;
    last[r.tag] = r;
    if (i >= since) tickGapMax[r.tag] = Math.max(tickGapMax[r.tag] || 0, r.tickGapMaxMs || 0);
  });
  window.__snapIdx = all.length;
  return { vis: document.visibilityState, mainTicks: window.__mainTicks, now: performance.now(), last, tickGapMax, reports: all.length };
})()`;

let c;
try {
  for (let i = 0; i < 40 && !c; i++) { try { c = await connect(port); } catch { await sleep(250); } }
  if (!c) throw new Error("could not connect to Chrome");
  let target;
  for (let i = 0; i < 40 && !target; i++) {
    const { targetInfos } = await c.send("Target.getTargets");
    target = targetInfos.find(t => t.type === "page" && t.url.startsWith("http"));
    if (!target) await sleep(250);
  }
  if (!target) throw new Error("page target not found");
  const session = await attach(c, target.targetId);
  const ua = await evalIn(c, session, "navigator.userAgent");
  const waitFor = async (expr, ms) => { const end = Date.now() + ms; while (Date.now() < end) { if (await evalIn(c, session, expr)) return true; await sleep(100); } return false; };
  const ready = await waitFor(`Object.keys(${SNAPSHOT}.last).length >= 3`, 30000);
  await evalIn(c, session, "window.__snapIdx = 0");
  if (!ready) throw new Error("engines did not report");
  await sleep(3000);
  const samples = [];
  const take = async label => { samples.push({ label, wall: Date.now(), ...(await evalIn(c, session, SNAPSHOT)) }); };
  await take("visible");
  const { targetId: other } = await c.send("Target.createTarget", { url: "about:blank", newWindow: false });
  await sleep(500);
  await take("hidden+0s");
  const steps = Math.max(1, Math.round(hiddenSeconds / 5));
  for (let i = 1; i <= steps; i++) { await sleep(5000); await take(`hidden+${i * 5}s`); }
  await c.send("Target.activateTarget", { targetId: target.targetId });
  await sleep(500);
  await take("shown+0s");
  await sleep(3000);
  await take("shown+3s");
  console.log(JSON.stringify({ ua, samples }));
} catch (err) {
  console.log(JSON.stringify({ error: String(err) }));
  process.exitCode = 1;
} finally {
  try { c && c.close(); } catch {}
  proc.kill();
  await sleep(300);
  try { rmSync(profile, { recursive: true, force: true }); } catch {}
}
