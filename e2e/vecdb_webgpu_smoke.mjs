#!/usr/bin/env node

import { createServer } from "node:http";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { execFile as execFileCallback } from "node:child_process";
import { promisify } from "node:util";

const execFile = promisify(execFileCallback);
const root = resolve(fileURLToPath(new URL("..", import.meta.url)));
const exampleDir = join(root, "examples", "vecdb-webgpu-smoke");

const contentTypes = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".wasm": "application/wasm",
};

async function buildWasm() {
  await mkdir(exampleDir, { recursive: true });
  await execFile("go", ["build", "-o", join(exampleDir, "main.wasm"), "./examples/vecdb-webgpu-smoke"], {
    cwd: root,
    env: { ...process.env, GOOS: "js", GOARCH: "wasm" },
  });
}

async function copyWasmExec() {
  const { stdout } = await execFile("go", ["env", "GOROOT"], { cwd: root });
  const goRoot = stdout.trim();
  const candidates = [
    join(goRoot, "lib", "wasm", "wasm_exec.js"),
    join(goRoot, "misc", "wasm", "wasm_exec.js"),
  ];
  for (const candidate of candidates) {
    try {
      const data = await readFile(candidate);
      await writeFile(join(exampleDir, "wasm_exec.js"), data);
      return;
    } catch {}
  }
  throw new Error("could not locate wasm_exec.js under GOROOT");
}

async function startServer() {
  const server = createServer(async (req, res) => {
    const url = new URL(req.url ?? "/", "http://127.0.0.1");
    const relPath = url.pathname === "/" ? "/index.html" : url.pathname;
    const filePath = join(exampleDir, relPath);
    try {
      const body = await readFile(filePath);
      res.writeHead(200, { "content-type": contentTypes[extname(filePath)] ?? "application/octet-stream" });
      res.end(body);
    } catch (error) {
      res.writeHead(404, { "content-type": "text/plain; charset=utf-8" });
      res.end(String(error));
    }
  });

  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string") {
    throw new Error("failed to resolve HTTP server address");
  }
  return { server, url: `http://127.0.0.1:${address.port}/` };
}

async function main() {
  const { chromium } = await import("playwright");

  await buildWasm();
  await copyWasmExec();

  const { server, url } = await startServer();
  const browser = await chromium.launch({
    headless: true,
    args: ["--enable-unsafe-webgpu"],
  });

  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    const consoleMessages = [];
    page.on("console", (message) => {
      consoleMessages.push(`[${message.type()}] ${message.text()}`);
    });
    page.on("pageerror", (error) => {
      consoleMessages.push(`[pageerror] ${error.stack ?? error.message}`);
    });

    await page.goto(url, { waitUntil: "domcontentloaded" });
    await page.waitForFunction(() => window.__vecdbWebGPUSmokeResult || window.__vecdbWebGPUSmokeError, undefined, {
      timeout: 30000,
    });

    const result = await page.evaluate(() => ({
      result: window.__vecdbWebGPUSmokeResult ?? null,
      error: window.__vecdbWebGPUSmokeError ?? null,
      gpu: typeof navigator !== "undefined" && "gpu" in navigator,
      status: document.getElementById("status")?.textContent ?? null,
    }));
    if (consoleMessages.length) {
      result.consoleMessages = consoleMessages;
    }
    console.log(JSON.stringify(result, null, 2));
    if (result.error || !result.result?.passed) {
      process.exitCode = 1;
    }
  } finally {
    server.close();
    await browser.close();
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
