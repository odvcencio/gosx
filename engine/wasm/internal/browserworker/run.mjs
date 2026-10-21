// Opens the engine-worker test page in Chromium and prints the page's reports
// as one JSON line. Usage: node run.mjs <url> <waitExpression> [timeoutMs]
// Needs Playwright in the repo's node_modules. ENGINE_WORKER_CHROME (or
// NETPLAY_CHROME) selects the Chromium or chrome-headless-shell binary.
import { chromium } from "playwright";

const [url, waitExpr, timeoutArg] = process.argv.slice(2);
const timeoutMs = Number(timeoutArg || 30000);
const executablePath = process.env.ENGINE_WORKER_CHROME || process.env.NETPLAY_CHROME || undefined;
const browser = await chromium.launch({ headless: true, executablePath });
const log = [];
try {
  const page = await browser.newPage();
  page.on("console", m => log.push(m.type() + ": " + m.text()));
  page.on("pageerror", e => log.push("pageerror: " + e.message));
  await page.goto(url);
  await page.waitForFunction(waitExpr, null, { timeout: timeoutMs, polling: 50 });
  const state = await page.evaluate(() => ({
    reports: window.__reports || [],
    events: window.__events || [],
    issues: window.__gosx && window.__gosx.listIssues ? window.__gosx.listIssues().map(i => ({ code: i.code, scope: i.scope, type: i.type, severity: i.severity, message: i.message, fallback: i.fallback })) : [],
    ua: navigator.userAgent,
  }));
  console.log(JSON.stringify({ ...state, log: log.slice(-40) }));
} catch (err) {
  console.log(JSON.stringify({ error: String(err), log: log.slice(-60) }));
  process.exitCode = 1;
} finally {
  await browser.close();
}
