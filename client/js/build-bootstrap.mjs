#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import zlib from "node:zlib";
import * as esbuild from "esbuild";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const DEBUG_SOURCEMAPS = process.env.GOSX_BUNDLE_DEBUG === "1";
const BROTLI_OPTIONS = {
  params: {
    [zlib.constants.BROTLI_PARAM_QUALITY]: zlib.constants.BROTLI_MAX_QUALITY,
    [zlib.constants.BROTLI_PARAM_MODE]: zlib.constants.BROTLI_MODE_TEXT,
  },
};

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
const RUNTIME_SCENE_CORE_FILE = "bootstrap-src/10-runtime-scene-core.js";
const RUNTIME_PRIMITIVES_FILE = "bootstrap-src/10-runtime-primitives.js";
const RUNTIME_UTILS_START = `  // Pending manifest reference, set during init, consumed when runtime is ready.
`;
const RUNTIME_UTILS_END = `  function sceneCSSVarReference(value) {
`;
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
const SECTION_RUNTIME_CAPABILITY_PROBE = `  function entryRequiresAsyncWebGPUProbe(entry) {
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
      sourceFile("bootstrap-src/04-telemetry.js"),
      sourceFile("bootstrap-src/05-document-env.js"),
      sourceFile(RUNTIME_PRIMITIVES_FILE),
      sourceFile(RUNTIME_SCENE_CORE_FILE),
      sourceFile("bootstrap-src/11-scene-math.js"),
      sourceFile("bootstrap-src/11a-scene-decompress.js"),
      sourceFile("bootstrap-src/12-scene-geometry.js"),
      sourceFile("bootstrap-src/13-scene-material.js"),
      sourceFile("bootstrap-src/14-scene-lighting.js"),
      sourceFile("bootstrap-src/15-scene-ir-schema.js"),
      sourceFile("bootstrap-src/15-scene-ir-schema-strict.js"),
      sourceFile("bootstrap-src/15-scene-draw-plan.js"),
      sourceFile("bootstrap-src/15b-scene-planner.js"),
      sourceFile("bootstrap-src/15c-scene-backend-registry.js"),
      sourceFile("bootstrap-src/15a-scene-postfx-shared.js"),
      sourceFile("bootstrap-src/16b-scene-hdr.js"),
      sourceFile("bootstrap-src/16-scene-webgl.js"),
      // 16z provides _externalProbe and window.__gosx_scene3d_webgpu_probe,
      // which 16a-scene-webgpu.js references at runtime. Without it the
      // legacy monolithic bootstrap.js throws ReferenceError the first
      // time the scene3d mount path touches the webgpu probe, which in
      // turn aborts GoSXScene3D engine registration and kills 38 tests
      // in runtime.test.js that rely on scene3d mount.
      sourceFile("bootstrap-src/16z-scene-webgpu-probe.js"),
      sourceFile("bootstrap-src/16a-scene-webgpu.js"),
      sourceFile("bootstrap-src/16b-scene-compute.js"),
      sourceFile("bootstrap-src/17-scene-input.js"),
      sourceFile("bootstrap-src/18-scene-canvas.js"),
      sourceFile("bootstrap-src/19-scene-gltf.js"),
      sourceFile("bootstrap-src/19a-scene-animation.js"),
      sourceFile("bootstrap-src/20-scene-mount.js"),
      // 28 installs window.__gosx_video_sync_js_create — the pure-JS drift
      // engine the video factory (in 30-tail.js) uses on the brain-absent
      // path. It must load before the tail.
      sourceFile("bootstrap-src/28-video-sync-fallback.js"),
      sourceFile(TAIL_FILE),
    ],
  },
  {
    path: path.join(__dirname, "bootstrap-lite.js"),
    sources: [
      sourceFile("bootstrap-src/00-textlayout.js"),
      sourceFile("bootstrap-src/04-telemetry.js"),
      sourceFile("bootstrap-src/05-document-env.js"),
      sourceFile("bootstrap-src/25-lite-tail.js"),
    ],
  },
  {
    path: path.join(__dirname, "bootstrap-runtime.js"),
    sources: [
      sourceFile("bootstrap-src/00-textlayout.js"),
      sourceFile("bootstrap-src/04-telemetry.js"),
      sourceFile("bootstrap-src/05-document-env.js"),
      sourceExtract(RUNTIME_SCENE_CORE_FILE, "runtime-utils", RUNTIME_UTILS_START, RUNTIME_UTILS_END),
      sourceFile(RUNTIME_PRIMITIVES_FILE),
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
      // 26b1 installs window.__gosx_paint_canvas_bundle — the standalone 2D
      // painter the canvas2d surface-kind render loop (in 26b-prefix's
      // _startCanvasSurfaceRAF) calls each frame. Self-contained IIFE; load
      // order is immaterial since the loop resolves the global at rAF time.
      sourceFile("bootstrap-src/26b1-canvas2d-painter.js"),
      // 26b2 installs window.__gosx_canvas_board_labels_sync — the DOM label
      // overlay that positions real HTML <span> elements over the WebGPU/canvas
      // board so text stays in the DOM (subpixel rendering, future editability).
      // Self-contained IIFE; the slice-4 RAF loop calls sync each frame.
      sourceFile("bootstrap-src/26b2-canvas-board-labels.js"),
      // 28 installs window.__gosx_video_sync_js_create, the pure-JS drift
      // engine the video factory uses when the WASM brain is absent. The
      // engines feature carries the video factory, so it must carry the
      // fallback engine too.
      sourceFile("bootstrap-src/28-video-sync-fallback.js"),
      sourceExtract(TAIL_FILE, "runtime-capability-probe", SECTION_RUNTIME_CAPABILITY_PROBE, `  async function hydrateIsland(entry) {
`),
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
      sourceFile(RUNTIME_PRIMITIVES_FILE),
      sourceFile(RUNTIME_SCENE_CORE_FILE),
      sourceFile("bootstrap-src/11-scene-math.js"),
      sourceFile("bootstrap-src/11a-scene-decompress.js"),
      sourceFile("bootstrap-src/12-scene-geometry.js"),
      sourceFile("bootstrap-src/13-scene-material.js"),
      sourceFile("bootstrap-src/14-scene-lighting.js"),
      sourceFile("bootstrap-src/15-scene-ir-schema.js"),
      sourceFile("bootstrap-src/15-scene-ir-schema-strict.js"),
      sourceFile("bootstrap-src/15-scene-draw-plan.js"),
      sourceFile("bootstrap-src/15b-scene-planner.js"),
      sourceFile("bootstrap-src/15c-scene-backend-registry.js"),
      sourceFile("bootstrap-src/15a-scene-postfx-shared.js"),
      sourceFile("bootstrap-src/16b-scene-hdr.js"),
      sourceFile("bootstrap-src/16b-scene-compute.js"),
      sourceFile("bootstrap-src/16-scene-webgl.js"),
      // 16a-scene-webgpu.js is NOT here — it moved to
      // bootstrap-feature-scene3d-webgpu.js so WebGL-only pages (Safari,
      // Firefox on most platforms, ForceWebGL) don't parse WebGPU code
      // they'll never run. 16b-scene-compute.js stays in this chunk
      // because WebGL uses its CPU particle-system path. 16z holds the
      // tiny stub + adapter probe so the WebGL mount path stays sync.
      sourceFile("bootstrap-src/16z-scene-webgpu-probe.js"),
      sourceFile("bootstrap-src/17-scene-input.js"),
      sourceFile("bootstrap-src/18-scene-canvas.js"),
      // 19-scene-gltf.js is NOT here — it moved to
      // bootstrap-feature-scene3d-gltf.js so pages that don't load .glb/
      // .gltf model assets (galaxies, particle systems, CSS-driven 3D
      // scenes — the majority of Scene3D consumers) don't pay the ~30KB
      // parse cost. 20-scene-mount.js lazy-fetches the chunk on first
      // model request via ensureGLTFFeatureLoaded().
      //
      // 19a-scene-animation.js is NOT here either — it moved to
      // bootstrap-feature-scene3d-animation.js. Pages that don't use
      // keyframe animations or skeletal clips skip ~16KB of bone math
      // and quaternion slerp. Consumers that DO need the mixer can
      // lazy-load it via window.__gosx_scene3d_animation_api.
      sourceFile("bootstrap-src/20-scene-mount.js"),
      sourceFile("bootstrap-src/26d-feature-scene3d-suffix.js"),
    ],
  },
  {
    path: path.join(__dirname, "bootstrap-feature-scene3d-webgpu.js"),
    sources: [
      sourceFile("bootstrap-src/26e-feature-scene3d-webgpu-prefix.js"),
      sourceFile("bootstrap-src/16a-scene-webgpu.js"),
      sourceFile("bootstrap-src/16b-scene-compute.js"),
      sourceFile("bootstrap-src/26e-feature-scene3d-webgpu-suffix.js"),
    ],
  },
  {
    path: path.join(__dirname, "bootstrap-feature-scene3d-gltf.js"),
    sources: [
      sourceFile("bootstrap-src/26f-feature-scene3d-gltf-prefix.js"),
      sourceFile("bootstrap-src/19-scene-gltf.js"),
      sourceFile("bootstrap-src/26f-feature-scene3d-gltf-suffix.js"),
    ],
  },
  {
    path: path.join(__dirname, "bootstrap-feature-scene3d-animation.js"),
    sources: [
      sourceFile("bootstrap-src/26g-feature-scene3d-animation-prefix.js"),
      sourceFile("bootstrap-src/19a-scene-animation.js"),
      sourceFile("bootstrap-src/26g-feature-scene3d-animation-suffix.js"),
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

function compressedSidecars(filePath, code) {
  const raw = Buffer.from(code, "utf8");
  if (raw.length === 0) {
    return [];
  }
  return [
    {
      path: `${filePath}.gz`,
      bytes: zlib.gzipSync(raw, { level: zlib.constants.Z_BEST_COMPRESSION }),
    },
    {
      path: `${filePath}.br`,
      bytes: zlib.brotliCompressSync(raw, BROTLI_OPTIONS),
    },
  ].map((sidecar) => ({
    ...sidecar,
    bytes: sidecar.bytes.length < raw.length ? sidecar.bytes : null,
  }));
}

function writeCompressedSidecars(filePath, code) {
  for (const sidecar of compressedSidecars(filePath, code)) {
    if (sidecar.bytes) {
      fs.writeFileSync(sidecar.path, sidecar.bytes);
      continue;
    }
    if (fs.existsSync(sidecar.path)) {
      fs.rmSync(sidecar.path);
    }
  }
}

function sidecarsMatch(filePath, code) {
  for (const sidecar of compressedSidecars(filePath, code)) {
    const current = fs.existsSync(sidecar.path) ? fs.readFileSync(sidecar.path) : null;
    if (!sidecar.bytes) {
      if (current) {
        return false;
      }
      continue;
    }
    if (!current || !current.equals(sidecar.bytes)) {
      return false;
    }
  }
  return true;
}

function sourceMapDataURL(map) {
  return `data:application/json;base64,${Buffer.from(map, "utf8").toString("base64")}`;
}

function normalizeGeneratedCode(code, mapPath) {
  let next = String(code || "").replace(/\r\n?/g, "\n");
  if (!next.endsWith("\n")) {
    next += "\n";
  }
  if (DEBUG_SOURCEMAPS) {
    next += `//# sourceMappingURL=${path.basename(mapPath)}\n`;
  }
  return next;
}

function normalizeGeneratedMap(map, fileName) {
  const parsed = JSON.parse(map);
  parsed.file = fileName;
  return JSON.stringify(parsed);
}

async function minifyBootstrapBundle(entry, built) {
  const input = `${built.code}\n//# sourceMappingURL=${sourceMapDataURL(built.map)}`;
  const result = await esbuild.transform(input, {
    charset: "utf8",
    legalComments: "none",
    loader: "js",
    minify: true,
    sourcefile: path.basename(entry.path),
    sourcemap: "external",
    target: "es2020",
  });
  return {
    code: normalizeGeneratedCode(result.code, entry.mapPath),
    map: normalizeGeneratedMap(result.map, path.basename(entry.path)),
  };
}

function buildCompactedBootstrapBundle(entry) {
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

  if (!code.endsWith("\n")) {
    code += "\n";
  }

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

async function buildBootstrapBundle(entry) {
  return minifyBootstrapBundle(entry, buildCompactedBootstrapBundle(entry));
}

async function main(argv) {
  const args = new Set(argv.slice(2));
  if (args.has("--check")) {
    const stale = [];
    for (const entry of outputs) {
      const next = await buildBootstrapBundle(entry);
      const currentCode = fs.existsSync(entry.path) ? fs.readFileSync(entry.path, "utf8") : "";
      const currentMap = fs.existsSync(entry.mapPath) ? fs.readFileSync(entry.mapPath, "utf8") : "";
      if (currentCode !== next.code || currentMap !== next.map || !sidecarsMatch(entry.path, next.code)) {
        stale.push(entry);
      }
    }
    if (stale.length > 0) {
      process.stderr.write("bootstrap runtime assets are out of date. Run `npm run build:bootstrap`.\n");
      process.exit(1);
    }
    return;
  }
  for (const entry of outputs) {
    const built = await buildBootstrapBundle(entry);
    fs.writeFileSync(entry.path, built.code, "utf8");
    fs.writeFileSync(entry.mapPath, built.map, "utf8");
    writeCompressedSidecars(entry.path, built.code);
  }
}

main(process.argv).catch((err) => {
  process.stderr.write(`${err && err.stack ? err.stack : err}\n`);
  process.exit(1);
});
