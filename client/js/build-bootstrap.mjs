#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

function sourceFile(rel) {
  return {
    kind: "file",
    file: path.join(__dirname, rel),
    relative: rel.replace(/\\/g, "/"),
  };
}

function sourceExtract(rel, id, start, end) {
  return {
    kind: "extract",
    file: path.join(__dirname, rel),
    relative: `${rel.replace(/\\/g, "/")}#${id}`,
    start,
    end,
  };
}

const TAIL_FILE = "bootstrap-src/30-tail.js";
const SECTION_ENGINE_MOUNTING = `  // --------------------------------------------------------------------------
  // Engine mounting
  // --------------------------------------------------------------------------
`;
const SECTION_HUB_CONNECTIONS = `  // --------------------------------------------------------------------------
  // Hub connections
  // --------------------------------------------------------------------------
`;
const SECTION_ISLAND_DISPOSAL = `  // --------------------------------------------------------------------------
  // Island disposal
  // --------------------------------------------------------------------------
`;
const SECTION_HYDRATION = `  // --------------------------------------------------------------------------
  // Hydration
  // --------------------------------------------------------------------------
`;
const SECTION_RUNTIME_READY = `  // --------------------------------------------------------------------------
  // Runtime ready callback
  // --------------------------------------------------------------------------
`;
const SECTION_EVENT_DELEGATION = `  // --------------------------------------------------------------------------
  // Event delegation
  // --------------------------------------------------------------------------
`;

const outputs = [
  {
    path: path.join(__dirname, "bootstrap.js"),
    sources: [
      sourceFile("bootstrap-src/00-textlayout.js"),
      sourceFile("bootstrap-src/05-document-env.js"),
      sourceFile("bootstrap-src/10-runtime-scene-core.js"),
      sourceFile("bootstrap-src/11-scene-math.js"),
      sourceFile("bootstrap-src/11a-scene-decompress.js"),
      sourceFile("bootstrap-src/12-scene-geometry.js"),
      sourceFile("bootstrap-src/13-scene-material.js"),
      sourceFile("bootstrap-src/14-scene-lighting.js"),
      sourceFile("bootstrap-src/15-scene-draw-plan.js"),
      sourceFile("bootstrap-src/15a-scene-postfx-shared.js"),
      sourceFile("bootstrap-src/16-scene-webgl.js"),
      sourceFile("bootstrap-src/16a-scene-webgpu.js"),
      sourceFile("bootstrap-src/16b-scene-compute.js"),
      sourceFile("bootstrap-src/17-scene-input.js"),
      sourceFile("bootstrap-src/18-scene-canvas.js"),
      sourceFile("bootstrap-src/19-scene-gltf.js"),
      sourceFile("bootstrap-src/19a-scene-animation.js"),
      sourceFile("bootstrap-src/20-scene-mount.js"),
      sourceFile(TAIL_FILE),
    ],
  },
  {
    path: path.join(__dirname, "bootstrap-lite.js"),
    sources: [
      sourceFile("bootstrap-src/00-textlayout.js"),
      sourceFile("bootstrap-src/05-document-env.js"),
      sourceFile("bootstrap-src/25-lite-tail.js"),
    ],
  },
  {
    path: path.join(__dirname, "bootstrap-runtime.js"),
    sources: [
      sourceFile("bootstrap-src/00-textlayout.js"),
      sourceFile("bootstrap-src/05-document-env.js"),
      sourceFile("bootstrap-src/10-runtime-scene-core.js"),
      sourceFile("bootstrap-src/26-runtime-tail.js"),
    ],
  },
  {
    path: path.join(__dirname, "bootstrap-feature-islands.js"),
    sources: [
      sourceFile("bootstrap-src/26a-feature-islands-prefix.js"),
      sourceExtract(TAIL_FILE, "islands-event-delegation", SECTION_EVENT_DELEGATION, SECTION_ENGINE_MOUNTING),
      sourceExtract(TAIL_FILE, "islands-dispose", `  window.__gosx_dispose_island = function(islandID) {
`, `
  window.__gosx_dispose_engine = function(engineID) {
`),
      sourceExtract(TAIL_FILE, "islands-hydration", SECTION_HYDRATION, SECTION_RUNTIME_READY),
      sourceFile("bootstrap-src/26a-feature-islands-suffix.js"),
    ],
  },
  {
    path: path.join(__dirname, "bootstrap-feature-engines.js"),
    sources: [
      sourceFile("bootstrap-src/26b-feature-engines-prefix.js"),
      sourceExtract(TAIL_FILE, "engines-mounting", SECTION_ENGINE_MOUNTING, SECTION_HUB_CONNECTIONS),
      sourceExtract(TAIL_FILE, "engines-dispose", `  window.__gosx_dispose_engine = function(engineID) {
`, `
  window.__gosx_disconnect_hub = function(hubID) {
`),
      sourceFile("bootstrap-src/26b-feature-engines-suffix.js"),
    ],
  },
  {
    path: path.join(__dirname, "bootstrap-feature-hubs.js"),
    sources: [
      sourceFile("bootstrap-src/26c-feature-hubs-prefix.js"),
      sourceExtract(TAIL_FILE, "hubs-connections", SECTION_HUB_CONNECTIONS, SECTION_ISLAND_DISPOSAL),
      sourceExtract(TAIL_FILE, "hubs-disconnect", `  window.__gosx_disconnect_hub = function(hubID) {
`, `
  async function disposePage() {
`),
      sourceFile("bootstrap-src/26c-feature-hubs-suffix.js"),
    ],
  },
  {
    path: path.join(__dirname, "bootstrap-feature-scene3d.js"),
    sources: [
      sourceFile("bootstrap-src/26d-feature-scene3d-prefix.js"),
      sourceFile("bootstrap-src/11-scene-math.js"),
      sourceFile("bootstrap-src/11a-scene-decompress.js"),
      sourceFile("bootstrap-src/12-scene-geometry.js"),
      sourceFile("bootstrap-src/13-scene-material.js"),
      sourceFile("bootstrap-src/14-scene-lighting.js"),
      sourceFile("bootstrap-src/15-scene-draw-plan.js"),
      sourceFile("bootstrap-src/15a-scene-postfx-shared.js"),
      sourceFile("bootstrap-src/16-scene-webgl.js"),
      sourceFile("bootstrap-src/16a-scene-webgpu.js"),
      sourceFile("bootstrap-src/16b-scene-compute.js"),
      sourceFile("bootstrap-src/17-scene-input.js"),
      sourceFile("bootstrap-src/18-scene-canvas.js"),
      sourceFile("bootstrap-src/19-scene-gltf.js"),
      sourceFile("bootstrap-src/19a-scene-animation.js"),
      sourceFile("bootstrap-src/20-scene-mount.js"),
      sourceFile("bootstrap-src/26d-feature-scene3d-suffix.js"),
    ],
  },
].map((entry) => ({
  path: entry.path,
  mapPath: entry.path + ".map",
  sources: entry.sources,
}));

const BASE64_CHARS = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

function readSource(file) {
  return fs.readFileSync(file, "utf8");
}

function extractSource(source, descriptor) {
  const startIndex = source.indexOf(descriptor.start);
  if (startIndex < 0) {
    throw new Error(`missing start marker for ${descriptor.relative}`);
  }
  const searchFrom = startIndex + descriptor.start.length;
  const endIndex = source.indexOf(descriptor.end, searchFrom);
  if (endIndex < 0) {
    throw new Error(`missing end marker for ${descriptor.relative}`);
  }
  return source.slice(startIndex, endIndex);
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
  const sections = entry.sources.map((descriptor) => {
    const source = readSource(descriptor.file);
    const resolved = descriptor.kind === "extract" ? extractSource(source, descriptor) : source;
    return {
      file: descriptor.file,
      relative: descriptor.relative || path.relative(__dirname, descriptor.file).replace(/\\/g, "/"),
      raw: resolved.replace(/\r\n?/g, "\n"),
      compacted: compactSource(resolved),
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
