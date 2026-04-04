#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const outputs = [
  {
    path: path.join(__dirname, "bootstrap.js"),
    sources: [
      "bootstrap-src/00-textlayout.js",
      "bootstrap-src/05-document-env.js",
      "bootstrap-src/10-runtime-scene-core.js",
      "bootstrap-src/11-scene-math.js",
      "bootstrap-src/12-scene-geometry.js",
      "bootstrap-src/13-scene-material.js",
      "bootstrap-src/14-scene-lighting.js",
      "bootstrap-src/15-scene-draw-plan.js",
      "bootstrap-src/16-scene-webgl.js",
      "bootstrap-src/17-scene-input.js",
      "bootstrap-src/18-scene-canvas.js",
      "bootstrap-src/19-scene-gltf.js",
      "bootstrap-src/19a-scene-animation.js",
      "bootstrap-src/20-scene-mount.js",
      "bootstrap-src/30-tail.js",
    ],
  },
  {
    path: path.join(__dirname, "bootstrap-lite.js"),
    sources: [
      "bootstrap-src/00-textlayout.js",
      "bootstrap-src/05-document-env.js",
      "bootstrap-src/25-lite-tail.js",
    ],
  },
].map((entry) => ({
  path: entry.path,
  mapPath: entry.path + ".map",
  sources: entry.sources.map((rel) => path.join(__dirname, rel)),
}));

const BASE64_CHARS = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

function readSource(file) {
  return fs.readFileSync(file, "utf8");
}

function compactSource(source) {
  const lines = String(source).replace(/\r\n?/g, "\n").split("\n");
  const out = [];
  const lineMap = [];
  let lastBlank = false;

  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    const trimmed = line.trim();
    if (trimmed.startsWith("//")) {
      continue;
    }

    const normalized = line.replace(/[ \t]+$/g, "");
    if (trimmed === "") {
      if (lastBlank) {
        continue;
      }
      lastBlank = true;
      out.push("");
      lineMap.push(index);
      continue;
    }

    lastBlank = false;
    out.push(normalized);
    lineMap.push(index);
  }

  while (out.length > 0 && out[0] === "") {
    out.shift();
    lineMap.shift();
  }
  while (out.length > 0 && out[out.length - 1] === "") {
    out.pop();
    lineMap.pop();
  }
  return {
    code: out.length > 0 ? out.join("\n") + "\n" : "",
    lineMap,
  };
}

function base64VLQEncode(value) {
  let current = value < 0 ? ((-value) << 1) | 1 : value << 1;
  let encoded = "";
  do {
    let digit = current & 31;
    current >>>= 5;
    if (current > 0) {
      digit |= 32;
    }
    encoded += BASE64_CHARS[digit];
  } while (current > 0);
  return encoded;
}

function encodeMappings(lines) {
  const segments = [];
  let previousSource = 0;
  let previousOriginalLine = 0;
  let previousOriginalColumn = 0;

  for (const line of lines) {
    if (!line) {
      segments.push("");
      continue;
    }
    const originalColumn = line.column || 0;
    segments.push([
      base64VLQEncode(0),
      base64VLQEncode(line.source - previousSource),
      base64VLQEncode(line.originalLine - previousOriginalLine),
      base64VLQEncode(originalColumn - previousOriginalColumn),
    ].join(""));
    previousSource = line.source;
    previousOriginalLine = line.originalLine;
    previousOriginalColumn = originalColumn;
  }

  return segments.join(";");
}

function buildBootstrapBundle(entry) {
  const sections = entry.sources.map((file) => {
    const source = readSource(file);
    return {
      file,
      relative: path.relative(__dirname, file).replace(/\\/g, "/"),
      raw: source.replace(/\r\n?/g, "\n"),
      compacted: compactSource(source),
    };
  });

  let code = "";
  const lines = [];
  for (let index = 0; index < sections.length; index += 1) {
    const section = sections[index];
    if (index > 0 && code !== "") {
      code += "\n";
      lines.push(null);
    }
    code += section.compacted.code;
    for (const originalLine of section.compacted.lineMap) {
      lines.push({
        source: index,
        originalLine,
        column: 0,
      });
    }
  }

  code += `//# sourceMappingURL=${path.basename(entry.mapPath)}\n`;
  lines.push(null);

  return {
    code,
    map: JSON.stringify({
      version: 3,
      file: path.basename(entry.path),
      sources: sections.map((section) => section.relative),
      sourcesContent: sections.map((section) => section.raw),
      names: [],
      mappings: encodeMappings(lines),
    }),
  };
}

function main(argv) {
  const args = new Set(argv.slice(2));
  if (args.has("--check")) {
    const stale = outputs.filter((entry) => {
      const next = buildBootstrapBundle(entry);
      const currentCode = fs.existsSync(entry.path) ? fs.readFileSync(entry.path, "utf8") : "";
      const currentMap = fs.existsSync(entry.mapPath) ? fs.readFileSync(entry.mapPath, "utf8") : "";
      return currentCode !== next.code || currentMap !== next.map;
    });
    if (stale.length > 0) {
      process.stderr.write("bootstrap runtime assets are out of date. Run `npm run build:bootstrap`.\n");
      process.exit(1);
    }
    return;
  }
  for (const entry of outputs) {
    const built = buildBootstrapBundle(entry);
    fs.writeFileSync(entry.path, built.code, "utf8");
    fs.writeFileSync(entry.mapPath, built.map, "utf8");
  }
}

main(process.argv);
