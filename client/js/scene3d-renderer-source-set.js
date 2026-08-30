"use strict";

const fs = require("node:fs");
const path = require("node:path");
const childProcess = require("node:child_process");

const clientJS = __dirname;
const repoRoot = path.resolve(clientJS, "..", "..");
const manifest = JSON.parse(fs.readFileSync(path.join(clientJS, "bootstrap-src", "chunks.json"), "utf8"));
const architecture = JSON.parse(fs.readFileSync(path.join(clientJS, "testdata", "scene3d-renderer-architecture.json"), "utf8"));

function backendContract(backend, contract = architecture) {
  if (backend !== "webgl" && backend !== "webgpu") {
    throw new Error(`unknown Scene3D renderer backend ${JSON.stringify(backend)}`);
  }
  const entry = contract && contract.sourceSets && contract.sourceSets[backend];
  if (!entry || typeof entry.chunk !== "string" || !Array.isArray(entry.sources)) {
    throw new Error(`Scene3D renderer contract has no valid ${backend} source set`);
  }
  return entry;
}

function sameList(actual, expected) {
  return actual.length === expected.length && actual.every((value, index) => value === expected[index]);
}

function isRendererSource(backend, source) {
  const runtime = new RegExp(`^\\.\\./runtime/scene3d/${backend}(?:-[a-z0-9-]+)?\\.ts$`);
  if (runtime.test(source)) return true;
  return backend === "webgpu" && /^bootstrap-src\/16a\d+-scene-webgpu-[a-z0-9-]+\.ts$/.test(source);
}

function isPotentialRendererExtractionSource(backend, source) {
  if (!/\.ts$/.test(source)) return false;
  const name = path.posix.basename(source);
  if (isRendererSource(backend, source)) return true;
  if (name.startsWith("mount-")) return false;
  if (source.startsWith("../runtime/scene3d/")) {
    return name.includes(backend) || name.includes("renderer") || (backend === "webgpu" && /\bgpu\b|^gpu-|gpu-/.test(name));
  }
  if (source.startsWith("bootstrap-src/")) return backend === "webgpu" && /^16a\d+-scene-webgpu-/.test(name);
  return false;
}

function discoverRendererSources(backend, root = clientJS) {
  backendContract(backend);
  const runtimeDir = path.resolve(root, "..", "runtime", "scene3d");
  const runtime = fs.readdirSync(runtimeDir).map((name) => `../runtime/scene3d/${name}`);
  const supplemental = fs.readdirSync(path.join(root, "bootstrap-src"))
    .map((name) => `bootstrap-src/${name}`);
  for (const source of runtime.concat(supplemental)) {
    if (isPotentialRendererExtractionSource(backend, source) && !isRendererSource(backend, source)) {
      throw new Error(`${source} looks like an extracted ${backend} renderer source but violates the naming contract`);
    }
  }
  return runtime.concat(supplemental).filter((source) => isRendererSource(backend, source)).sort();
}

function rendererSourcesInChunk(backend, sources) {
  const selected = [];
  for (const source of sources) {
    if (isRendererSource(backend, source)) {
      selected.push(source);
      continue;
    }
    if (source.startsWith("../runtime/scene3d/")) {
      throw new Error(`${source} is a runtime Scene3D source in the ${backend} renderer chunk but is not named/listed as a governed renderer source`);
    }
  }
  return selected;
}

function rendererBackendSources(backend, options = {}) {
  const contract = options.contract || architecture;
  const entry = backendContract(backend, contract);
  protectedRendererReferenceSources(contract);
  validateProtectedSourceFiles(contract, options.root || clientJS, options);
  const sourceManifest = options.manifest || manifest;
  const chunks = sourceManifest && Array.isArray(sourceManifest.chunks)
    ? sourceManifest.chunks.filter((chunk) => chunk && chunk.name === entry.chunk)
    : [];
  if (chunks.length !== 1 || !Array.isArray(chunks[0].sources)) {
    throw new Error(`${entry.chunk} must appear exactly once with an ordered source list`);
  }
  const chunkSources = chunks[0].sources;
  if (Array.isArray(entry.chunkSources) && !sameList(chunkSources, entry.chunkSources)) {
    throw new Error(`${entry.chunk} source roster mismatch: want ${entry.chunkSources.join(" -> ")}; got ${chunkSources.join(" -> ")}`);
  }
  const duplicate = chunkSources.find((source, index) => chunkSources.indexOf(source) !== index);
  if (duplicate) throw new Error(`${entry.chunk} registers duplicate source ${duplicate}`);
  validateChunkSourceRoles(backend, chunkSources, entry, options.root || clientJS, options);

  const actual = rendererSourcesInChunk(backend, chunkSources);
  const expected = entry.sources;
  if (actual.length !== expected.length || actual.some((source, index) => source !== expected[index])) {
    throw new Error(`${backend} renderer source order mismatch: want ${expected.join(" -> ")}; got ${actual.join(" -> ")}`);
  }

  const available = options.availableSources || discoverRendererSources(backend, options.root || clientJS);
  const unregistered = available.filter((source) => isRendererSource(backend, source) && !chunkSources.includes(source));
  if (unregistered.length) {
    throw new Error(`${backend} renderer sources are not registered in ${entry.chunk}: ${unregistered.sort().join(", ")}`);
  }
  return actual.slice();
}

function allProtectedSourcePaths(contract = architecture) {
  const sources = [];
  for (const sourceSet of Object.values(contract.sourceSets || {})) {
    for (const source of sourceSet.sources || []) sources.push(source);
  }
  for (const source of contract.extraProtectedSources || []) sources.push(source);
  return sources;
}

function validateProtectedSourceFiles(contract = architecture, root = clientJS, options = {}) {
  validateGovernedSourceFiles(allProtectedSourcePaths(contract), root, options);
}

function validateGovernedSourceFiles(sources, root = clientJS, options = {}) {
  if (options.validateFiles === false) return;
  const roots = [
    path.resolve(root, "..", "runtime", "scene3d"),
    path.resolve(root, "bootstrap-src"),
  ];
  const realPaths = new Map();
  const fileIDs = new Map();
  for (const source of sources) {
    if (typeof source !== "string" || source.length === 0 || source.includes("\\") || path.posix.isAbsolute(source) || path.posix.normalize(source) !== source) {
      throw new Error(`Scene3D renderer source path ${JSON.stringify(source)} is malformed`);
    }
    const filename = path.resolve(root, source);
    const stat = fs.lstatSync(filename);
    if (stat.isSymbolicLink()) throw new Error(`${source} must not be a symlink`);
    validateNoSymlinkPathComponents(source, filename, root, roots);
    if (!stat.isFile()) throw new Error(`${source} is not a regular file`);
    const real = fs.realpathSync(filename);
    if (!roots.some((allowed) => real === allowed || real.startsWith(allowed + path.sep))) {
      throw new Error(`${source} resolves outside the approved Scene3D renderer source roots`);
    }
    const duplicate = realPaths.get(real);
    if (duplicate) throw new Error(`${source} aliases the same physical file as ${duplicate}`);
    realPaths.set(real, source);
    const fileID = `${stat.dev}:${stat.ino}`;
    const inodeDuplicate = fileIDs.get(fileID);
    if (inodeDuplicate) throw new Error(`${source} aliases the same physical file as ${inodeDuplicate}`);
    fileIDs.set(fileID, source);
    if (stat.nlink > 1) {
      throw new Error(`${source} has ${stat.nlink} hardlinks; governed renderer sources must have a single owned path`);
    }
  }
}

function validateNoSymlinkPathComponents(source, filename, root, roots) {
  const base = roots.find((allowed) => filename === allowed || filename.startsWith(allowed + path.sep));
  if (!base) return;
  const rel = path.relative(base, filename);
  const parts = rel ? rel.split(path.sep) : [];
  let current = base;
  const baseStat = fs.lstatSync(base);
  if (baseStat.isSymbolicLink()) throw new Error(`${source} traverses symlink component ${path.relative(root, base)}`);
  for (const part of parts) {
    current = path.join(current, part);
    const stat = fs.lstatSync(current);
    if (stat.isSymbolicLink()) throw new Error(`${source} traverses symlink component ${path.relative(root, current)}`);
  }
}

function validateChunkSourceRoles(backend, chunkSources, entry, root = clientJS, options = {}) {
  const roles = entry.chunkSourceRoles || {};
  const roleKeys = Object.keys(roles).sort();
  const rosterKeys = chunkSources.slice().sort();
  if (!sameList(roleKeys, rosterKeys)) {
    throw new Error(`${entry.chunk} chunkSourceRoles must exactly match chunkSources: want ${rosterKeys.join(", ")}; got ${roleKeys.join(", ")}`);
  }
  const authorities = chunkRoleAuthorities(backend, entry);
  const governed = new Set(entry.sources || []);
  for (const source of chunkSources) {
    const role = roles[source];
    if (!authorities.has(role)) {
      throw new Error(`${entry.chunk} source ${source} has unknown governed role ${JSON.stringify(role)}`);
    }
    if (!authorities.get(role).has(source)) {
      throw new Error(`${entry.chunk} source ${source} has role ${role}, but that role is not authorized for this path`);
    }
    if (governed.has(source)) {
      if (role !== "governed-renderer") throw new Error(`${entry.chunk} governed renderer source ${source} has role ${role}`);
      continue;
    }
    if (role === "governed-renderer") {
      throw new Error(`${entry.chunk} source ${source} is marked governed-renderer but is missing from sourceSets.${backend}.sources`);
    }
    if (isRendererOwnedChunkSource(backend, source)) {
      throw new Error(`${entry.chunk} source ${source} looks renderer-owned but is not governed by sourceSets.${backend}.sources`);
    }
  }
  validateGovernedSourceFiles(chunkSources, root, options);
}

function chunkRoleAuthorities(backend, entry) {
  const exact = {
    webgl: {
      "chunk-wrapper": [
        "bootstrap-src/26j-feature-scene3d-webgl-prefix.ts",
        "bootstrap-src/26j-feature-scene3d-webgl-suffix.ts",
      ],
      "shared-scene-support": [
        "bootstrap-src/15a1-scene-texture-budget.ts",
        "bootstrap-src/16b-scene-hdr.ts",
      ],
      "backend-support": [
        "bootstrap-src/16e-scene-webgl-legacy.ts",
      ],
    },
    webgpu: {
      "chunk-wrapper": [
        "bootstrap-src/26e-feature-scene3d-webgpu-prefix.ts",
        "bootstrap-src/26e-feature-scene3d-webgpu-suffix.ts",
      ],
      "backend-support": [
        "bootstrap-src/26e1-feature-scene3d-webgpu-compute-bridge.ts",
      ],
    },
  };
  const roles = new Map([["governed-renderer", new Set(entry.sources || [])]]);
  for (const [role, sources] of Object.entries(exact[backend] || {})) {
    roles.set(role, new Set(sources));
  }
  return roles;
}

function isRendererOwnedChunkSource(backend, source) {
  if (isRendererSource(backend, source)) return true;
  const name = path.posix.basename(source);
  if (source.startsWith("../runtime/scene3d/")) {
    return name.includes(backend) || name.includes("renderer") || (backend === "webgpu" && /\bgpu\b|^gpu-|gpu-/.test(name));
  }
  if (source.startsWith("bootstrap-src/")) {
    return name.includes("renderer") || (backend === "webgpu" && /^16a\d+-scene-webgpu-/.test(name));
  }
  return false;
}

function readSceneRendererBackendSrc(backend, options = {}) {
  const root = options.root || clientJS;
  const readSource = options.readSource || ((source) => fs.readFileSync(path.join(root, source), "utf8"));
  return rendererBackendSources(backend, options).map(readSource).join("\n");
}

function directRendererReadFindings(source, filename) {
  const findings = [];
  const patterns = [
    /\b(?:read|load)[A-Za-z0-9_$]*\s*\(\s*["'](?:[^"']*\/)?web(?:gl|gpu)\.ts["']/g,
    /\b(?:[A-Za-z0-9_$]+\.)*readFile(?:Sync)?\s*\([\s\S]{0,240}?["'](?:[^"']*\/)?web(?:gl|gpu)\.ts["'][\s\S]{0,120}?\)/g,
  ];
  for (const pattern of patterns) {
    for (const match of source.matchAll(pattern)) {
      findings.push({ filename, line: source.slice(0, match.index).split("\n").length, text: match[0] });
    }
  }
  return findings;
}

function walkedFiles(root) {
  const files = [];
  for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
    if (entry.name === ".git" || entry.name === "node_modules" || entry.name === "vendor") continue;
    const filename = path.join(root, entry.name);
    if (entry.isDirectory()) files.push(...walkedFiles(filename));
    else files.push(filename);
  }
  return files.sort();
}

function scanDirectRendererReads(root = clientJS) {
  const findings = [];
  const canonical = path.resolve(root, path.basename(__filename));
  for (const filename of walkedFiles(root)) {
    if (!isTextInventoryFile(root, filename)) continue;
    if (path.resolve(filename) === canonical) continue;
    findings.push(...directRendererReadFindings(fs.readFileSync(filename, "utf8"), filename));
  }
  return findings;
}

function protectedRendererReferenceSources(contract = architecture) {
  const names = new Set();
  for (const sourceSet of Object.values(contract.sourceSets || {})) {
    for (const source of sourceSet.sources || []) names.add(path.posix.basename(source));
  }
  for (const source of contract.extraProtectedSources || []) names.add(path.posix.basename(source));
  const listed = contract.directRendererReferenceSources;
  if (listed !== undefined) {
    const actual = [...names].sort();
    const expected = listed.slice().sort();
    if (!sameList(actual, expected)) {
      throw new Error(`directRendererReferenceSources must equal sourceSets plus extraProtectedSources: want ${actual.join(", ")}; got ${expected.join(", ")}`);
    }
  }
  return [...names].sort();
}

function gitInventoryFiles(root) {
  try {
    const output = childProcess.execFileSync("git", ["-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z"], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    });
    return output.split("\0")
      .filter(Boolean)
      .map((file) => path.join(root, file))
      .sort();
  } catch {
    return walkedFiles(root);
  }
}

function generatedBootstrapArtifactPaths(sourceManifest = manifest) {
  const names = new Set(["bootstrap.js"]);
  for (const chunk of sourceManifest.chunks || []) {
    if (chunk && typeof chunk.name === "string" && chunk.name.startsWith("bootstrap")) names.add(chunk.name);
  }
  const generated = new Set();
  for (const name of names) {
    for (const suffix of ["", ".map", ".gz", ".br"]) generated.add(`client/js/${name}${suffix}`);
  }
  return generated;
}

function isGeneratedInventoryPath(rel) {
  if (rel.startsWith("build/") || rel.startsWith("dist/") || rel.startsWith("tmp/")) return true;
  if (generatedBootstrapArtifactPaths().has(rel)) return true;
  return false;
}

function isTextInventoryFile(root, filename) {
  const rel = path.relative(root, filename).split(path.sep).join("/");
  if (isGeneratedInventoryPath(rel)) return false;
  let stat;
  try {
    stat = fs.lstatSync(filename);
  } catch {
    return false;
  }
  if (!stat.isFile() || stat.isSymbolicLink()) return false;
  const chunk = fs.readFileSync(filename);
  if (chunk.includes(0)) return false;
  return true;
}

function validateDirectRendererReferenceRegistry(contract = architecture) {
  const protectedNames = new Set(protectedRendererReferenceSources(contract));
  const seen = new Set();
  for (const entry of contract.intentionalDirectRendererReferences || []) {
    const key = `${entry.file}\0${entry.source}`;
    if (seen.has(key)) throw new Error(`duplicate direct renderer-source reference registry key ${entry.file}:${entry.source}`);
    seen.add(key);
    if (!protectedNames.has(entry.source)) throw new Error(`direct renderer-source registry protects unknown source ${entry.source}`);
    if (!entry.file || entry.file.includes("\\") || path.posix.isAbsolute(entry.file) || entry.file.split("/").includes("..")) {
      throw new Error(`direct renderer-source registry has malformed file key ${entry.file}`);
    }
    if (!Number.isInteger(entry.count) || entry.count < 1) {
      throw new Error(`direct renderer-source registry has invalid count for ${entry.file}:${entry.source}`);
    }
  }
}

function rendererReferenceCounts(root = repoRoot, options = {}) {
  const contract = options.contract || architecture;
  const sourceNames = options.sourceNames || protectedRendererReferenceSources(contract);
  const ignoreFiles = new Set([
    "client/runtime/scene3d/webgl.ts",
    "client/runtime/scene3d/webgpu.ts",
    "client/runtime/scene3d/mount-webgl.ts",
    "client/js/scene3d-renderer-source-set.js",
    "client/js/scene3d-renderer-architecture.test.js",
    "client/js/testdata/scene3d-renderer-architecture.json",
    "internal/scene3drenderersource/source.go",
    ...(options.ignoreFiles || []),
  ]);
  const counts = new Map();
  for (const filename of gitInventoryFiles(root)) {
    const rel = path.relative(root, filename).split(path.sep).join("/");
    if (ignoreFiles.has(rel)) continue;
    if (!isTextInventoryFile(root, filename)) continue;
    const source = fs.readFileSync(filename, "utf8");
    for (const sourceName of sourceNames) {
      const count = [...source.matchAll(new RegExp(`(?<![-A-Za-z0-9_$])${sourceName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}(?![A-Za-z0-9_$])`, "g"))].length;
      if (count > 0) counts.set(`${rel}\0${sourceName}`, { file: rel, source: sourceName, count });
    }
  }
  return [...counts.values()].sort((a, b) => (
    a.file.localeCompare(b.file) || a.source.localeCompare(b.source)
  ));
}

function directRendererReferenceRegistryFindings(root = repoRoot, options = {}) {
  const contract = options.contract || architecture;
  validateDirectRendererReferenceRegistry(contract);
  const allowed = new Map((contract.intentionalDirectRendererReferences || []).map((entry) => [
    `${entry.file}\0${entry.source}`,
    entry.count,
  ]));
  const actual = rendererReferenceCounts(root, options);
  const seen = new Set();
  const findings = [];
  for (const entry of actual) {
    const key = `${entry.file}\0${entry.source}`;
    seen.add(key);
    const want = allowed.get(key);
    if (want === undefined) {
      findings.push({ ...entry, reason: "unregistered direct renderer-source reference" });
    } else if (want !== entry.count) {
      findings.push({ ...entry, expected: want, reason: "direct renderer-source reference count changed" });
    }
  }
  for (const [key, count] of allowed) {
    if (seen.has(key)) continue;
    const [file, source] = key.split("\0");
    findings.push({ file, source, expected: count, count: 0, reason: "registered direct renderer-source reference disappeared" });
  }
  return findings;
}

module.exports = {
  architecture,
  directRendererReadFindings,
  directRendererReferenceRegistryFindings,
  rendererReferenceCounts,
  readSceneRendererBackendSrc,
  rendererBackendSources,
  rendererSourcesInChunk,
  generatedBootstrapArtifactPaths,
  scanDirectRendererReads,
};
