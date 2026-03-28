#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const outputPath = path.join(__dirname, "bootstrap.js");
const sourcePaths = [
  "bootstrap-src/00-textlayout.js",
  "bootstrap-src/10-runtime-scene-core.js",
  "bootstrap-src/20-scene-mount.js",
  "bootstrap-src/30-tail.js",
].map((rel) => path.join(__dirname, rel));

function readSource(file) {
  return fs.readFileSync(file, "utf8");
}

function buildBootstrapSource() {
  return sourcePaths.map(readSource).join("");
}

function main(argv) {
  const args = new Set(argv.slice(2));
  const next = buildBootstrapSource();
  if (args.has("--check")) {
    const current = fs.existsSync(outputPath) ? fs.readFileSync(outputPath, "utf8") : "";
    if (current !== next) {
      process.stderr.write("bootstrap.js is out of date. Run `npm run build:bootstrap`.\n");
      process.exit(1);
    }
    return;
  }
  fs.writeFileSync(outputPath, next, "utf8");
}

main(process.argv);
