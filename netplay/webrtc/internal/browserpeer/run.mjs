// Opens the netplay test page in headless Chromium and prints the Go WASM
// peer's result as one JSON line. Usage: node run.mjs <url> [timeoutMs]
// Needs the repo's node_modules (playwright). Set NETPLAY_CHROME to use a
// specific Chrome or Chromium binary.
import { chromium } from "playwright";

const [url, timeoutArg] = process.argv.slice(2);
const timeoutMs = Number(timeoutArg || 60000);
const executablePath = process.env.NETPLAY_CHROME || undefined;
const browser = await chromium.launch({
  headless: true,
  executablePath,
  // Chromium hides host candidates behind mDNS names by default. The Go relay
  // does not resolve them in this sandbox, so expose the real address.
  args: ["--disable-features=WebRtcHideLocalIpsWithMdns"],
});
const log = [];
try {
  const page = await browser.newPage();
  page.on("console", m => log.push(m.type() + ": " + m.text()));
  page.on("pageerror", e => log.push("pageerror: " + e.message));
  await page.goto(url);
  const handle = await page.waitForFunction(() => window.__netplayResult, null, { timeout: timeoutMs, polling: 100 });
  // Keep the result as the Go program's JSON text: it holds 64-bit checksums,
  // and a JS number cannot represent them exactly.
  const result = await handle.jsonValue();
  const ua = await page.evaluate(() => navigator.userAgent);
  console.log(JSON.stringify({ result, ua, log: log.slice(-20) }));
} catch (err) {
  console.log(JSON.stringify({ error: String(err), log: log.slice(-40) }));
  process.exitCode = 1;
} finally {
  await browser.close();
}
