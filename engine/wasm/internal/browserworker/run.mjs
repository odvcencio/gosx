// Opens the engine-worker test page in Chromium and prints the page's reports
// as one JSON line. Usage: node run.mjs <url> <waitExpression> [timeoutMs] [shotsJSON]
// shotsJSON: [{"name": "...", "when": "<expression>", "selector": "#arena"}]. When
// the expression turns true the element is captured and the position of the
// pixels that differ from the background (#08151f) is reported (count and mean x).
// With ENGINE_WORKER_SHOT_DIR set, each capture is also written there as a PNG.
// Needs Playwright in the repo's node_modules. ENGINE_WORKER_CHROME (or
// NETPLAY_CHROME) selects the Chromium or chrome-headless-shell binary.
import { chromium } from "playwright";

const [url, waitExpr, timeoutArg, shotsArg, scriptArg] = process.argv.slice(2);
const timeoutMs = Number(timeoutArg || 30000);
const shots = shotsArg ? JSON.parse(shotsArg) : [];
const executablePath = process.env.ENGINE_WORKER_CHROME || process.env.NETPLAY_CHROME || undefined;
const browser = await chromium.launch({
  headless: true,
  executablePath,
  args: ["--use-gl=angle", "--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--enable-webgl", "--ignore-gpu-blocklist"],
});
const log = [];
let page;
try {
  page = await browser.newPage({ viewport: { width: 600, height: 500 } });
  page.on("console", m => log.push(m.type() + ": " + m.text()));
  page.on("pageerror", e => log.push("pageerror: " + e.message));
  page.on("response", r => { if (r.status() >= 400) log.push("http " + r.status() + " " + r.url()); });
  const script = scriptArg ? await import(new URL(scriptArg, "file://" + process.cwd() + "/").href) : null;
  if (script && script.init) await script.init(page);
  await page.goto(url);
  if (script && script.run) await script.run(page);
  const analyzer = await browser.newPage();
  const measured = [];
  for (const shot of shots) {
    await page.waitForFunction(shot.when, null, { timeout: timeoutMs, polling: 25 });
    const png = await page.locator(shot.selector).screenshot();
    if (process.env.ENGINE_WORKER_SHOT_DIR) (await import("node:fs")).writeFileSync(process.env.ENGINE_WORKER_SHOT_DIR + "/" + shot.name + ".png", png);
    const stats = await analyzer.evaluate(async b64 => {
      const img = new Image();
      img.src = "data:image/png;base64," + b64;
      await img.decode();
      const canvas = document.createElement("canvas");
      canvas.width = img.width; canvas.height = img.height;
      const g = canvas.getContext("2d");
      g.drawImage(img, 0, 0);
      const d = g.getImageData(0, 0, img.width, img.height).data;
      let n = 0, sx = 0;
      for (let y = 0; y < img.height; y++) for (let x = 0; x < img.width; x++) {
        const i = (y * img.width + x) * 4;
        // Foreground: any pixel far from the scene background (#08151f).
        if (Math.abs(d[i] - 8) + Math.abs(d[i + 1] - 21) + Math.abs(d[i + 2] - 31) > 40) { n++; sx += x; }
      }
      return { width: img.width, height: img.height, orange: n, meanX: n ? sx / n : null };
    }, png.toString("base64"));
    measured.push({ name: shot.name, ...stats });
  }
  await page.waitForFunction(waitExpr, null, { timeout: timeoutMs, polling: 50 });
  const state = await page.evaluate(() => ({
    reports: window.__reports || [],
    events: window.__events || [],
    frames: window.__frames || [],
    calls: window.__calls || null,
    signal: window.__gosx && window.__gosx.sharedSignals ? (window.__gosx.sharedSignals.values.get("$sim") ?? null) : null,
    issues: window.__gosx && window.__gosx.listIssues ? window.__gosx.listIssues().map(i => ({ code: i.code, scope: i.scope, type: i.type, severity: i.severity, message: i.message, fallback: i.fallback })) : [],
    mainTicks: window.__mainTicks || 0,
    ua: navigator.userAgent,
  }));
  console.log(JSON.stringify({ ...state, shots: measured, log: log.slice(-40) }));
} catch (err) {
  let partial = null;
  try {
    partial = await page.evaluate(() => ({ reports: window.__reports, events: window.__events, frames: window.__frames,
      issues: window.__gosx && window.__gosx.listIssues ? window.__gosx.listIssues().map(i => i.message) : [] }));
  } catch {}
  console.log(JSON.stringify({ error: String(err), partial, log: log.slice(-60) }));
  process.exitCode = 1;
} finally {
  await browser.close();
}
