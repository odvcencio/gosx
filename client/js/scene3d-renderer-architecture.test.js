"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const {
  architecture,
  directRendererReadFindings,
  directRendererReferenceRegistryFindings,
  generatedBootstrapArtifactPaths,
  rendererBackendSources,
  rendererReferenceCounts,
  scanDirectRendererReads,
} = require("./scene3d-renderer-source-set.js");
const {
  createBoardWebGPUHarness,
  createWebGLRendererForPost,
} = require("./runtime-test-harness.js");

function manifest(chunk, sources) {
  return { chunks: [{ name: chunk, sources }] };
}

function sourceContract(backend, sources) {
  return {
    sourceSets: {
      [backend]: {
        chunk: `bootstrap-feature-scene3d-${backend}.js`,
        chunkSourceRoles: Object.fromEntries(sources.map((source) => [source, "governed-renderer"])),
        sources,
      },
    },
  };
}

function gitInit(root) {
  try {
    require("node:child_process").execFileSync("git", ["-C", root, "init", "-q"], { stdio: "ignore" });
    require("node:child_process").execFileSync("git", ["-C", root, "add", "."], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

function clone(value) {
  return JSON.parse(JSON.stringify(value));
}

test("renderer source sets are manifest-ordered and reject missing, reordered, duplicate, or unregistered sources", () => {
  for (const backend of ["webgl", "webgpu"]) {
    assert.deepEqual(rendererBackendSources(backend), architecture.sourceSets[backend].sources);
  }

  const backend = "webgpu";
  const sources = ["../runtime/scene3d/webgpu.ts", "bootstrap-src/16a1-scene-webgpu-selena-uniforms.ts"];
  const contract = sourceContract(backend, sources);
  const chunk = contract.sourceSets[backend].chunk;
  const resolve = (listed, available = sources) => rendererBackendSources(backend, {
    contract,
    manifest: manifest(chunk, listed),
    availableSources: available,
    validateFiles: false,
  });
  assert.throws(() => resolve(sources.slice(0, 1)), /chunkSourceRoles must exactly match chunkSources/);
  assert.throws(() => resolve(sources.slice().reverse()), /source order mismatch/);
  assert.throws(() => resolve([sources[0], sources[0], sources[1]]), /duplicate source/);
  assert.throws(
    () => resolve(sources, sources.concat("../runtime/scene3d/webgpu-resources.ts")),
    /not registered/,
  );
  assert.throws(
    () => rendererBackendSources(backend, {
      contract,
      manifest: manifest(chunk, sources.concat("../runtime/scene3d/gpu-resources.ts")),
      availableSources: sources,
      validateFiles: false,
    }),
    /chunkSourceRoles must exactly match chunkSources/,
  );
  assert.throws(
    () => resolve(sources.concat("bootstrap-src/16a9-scene-webgpu-secret-renderer.ts")),
    /chunkSourceRoles must exactly match chunkSources/,
  );
  assert.throws(
    () => rendererBackendSources("webgpu", {
      contract: { sourceSets: { webgpu: { chunk, chunkSources: sources, sources } } },
      manifest: manifest(chunk, sources.concat("bootstrap-src/16a9-renderer-webgpu-secret.ts")),
      availableSources: sources,
      validateFiles: false,
    }),
    /source roster mismatch/,
  );
  assert.throws(
    () => rendererBackendSources("webgpu", {
      contract: {
        sourceSets: { webgpu: { chunk, sources: ["../runtime/scene3d/webgpu-core.ts", sources[1]] } },
        directRendererReferenceSources: ["webgpu.ts", path.basename(sources[1])],
      },
      manifest: manifest(chunk, ["../runtime/scene3d/webgpu-core.ts", sources[1]]),
      availableSources: ["../runtime/scene3d/webgpu-core.ts", sources[1]],
      validateFiles: false,
    }),
    /directRendererReferenceSources must equal sourceSets plus extraProtectedSources/,
  );
});

test("backend chunk roles are closed, exact, and path-authorized", () => {
  assert.deepEqual(rendererBackendSources("webgl"), architecture.sourceSets.webgl.sources);
  assert.deepEqual(rendererBackendSources("webgpu"), architecture.sourceSets.webgpu.sources);

  const invented = clone(architecture);
  invented.sourceSets.webgpu.chunkSourceRoles["bootstrap-src/26e-feature-scene3d-webgpu-prefix.ts"] = "invented-role";
  assert.throws(() => rendererBackendSources("webgpu", { contract: invented }), /unknown governed role/);

  const swapped = clone(architecture);
  swapped.sourceSets.webgpu.chunkSourceRoles["bootstrap-src/26e-feature-scene3d-webgpu-prefix.ts"] = "backend-support";
  assert.throws(() => rendererBackendSources("webgpu", { contract: swapped }), /not authorized for this path/);

  const stale = clone(architecture);
  stale.sourceSets.webgpu.chunkSourceRoles["bootstrap-src/not-in-roster.ts"] = "governed-renderer";
  assert.throws(() => rendererBackendSources("webgpu", { contract: stale }), /chunkSourceRoles must exactly match chunkSources/);
});

test("validateFiles false still enforces semantic chunk role governance", () => {
  const backend = "webgpu";
  const source = "bootstrap-src/26e2-feature-scene3d-webgpu-core.ts";
  const chunk = "bootstrap-feature-scene3d-webgpu.js";
  const sources = [
    "../runtime/scene3d/webgpu.ts",
    "bootstrap-src/16a1-scene-webgpu-selena-uniforms.ts",
  ];
  const chunkSources = sources.concat(source);
  assert.throws(
    () => rendererBackendSources(backend, {
      contract: {
        sourceSets: {
          webgpu: {
            chunk,
            chunkSources,
            sources,
          },
        },
        directRendererReferenceSources: [
          "16a1-scene-webgpu-selena-uniforms.ts",
          "webgpu.ts",
        ],
      },
      manifest: manifest(chunk, chunkSources),
      availableSources: sources,
      validateFiles: false,
    }),
    /chunkSourceRoles must exactly match chunkSources/,
  );
});

test("renderer source consumers cannot bypass the canonical source-set helper", () => {
  assert.deepEqual(scanDirectRendererReads(), []);
  const fileFixture = ["fs.read", "FileSync(path.join(root, ", "\"webgpu.ts\"", "), ", "\"utf8\"", ")"].join("");
  assert.equal(directRendererReadFindings(fileFixture, "negative-file-fixture.js").length, 1);
  const helperFixture = ["readBootstrap", "Src(", "\"../runtime/scene3d/webgpu.ts\"", ")"].join("");
  assert.equal(directRendererReadFindings(helperFixture, "negative-helper-fixture.js").length, 1);
});

test("repo-wide renderer source references match the explicit registry", () => {
  assert.deepEqual(directRendererReferenceRegistryFindings(), []);
});

test("repo-wide renderer reference registry rejects Go, path alias, and cross-package bypasses", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-renderer-ref-"));
  try {
    fs.mkdirSync(path.join(root, "render", "bundle"), { recursive: true });
    fs.writeFileSync(path.join(root, "render", "bundle", "bypass_test.go"), [
      "package bundle",
      "import \"path/filepath\"",
      "var p = filepath.Join(\"..\", \"..\", \"client\", \"runtime\", \"scene3d\", \"webgpu.ts\")",
      "",
    ].join("\n"));
    fs.mkdirSync(path.join(root, "scene", "capability"), { recursive: true });
    fs.writeFileSync(path.join(root, "scene", "capability", "alias_test.go"), [
      "package capability",
      "const alias = \"../../client/runtime/scene3d/\" + \"webgl.ts\"",
      "",
    ].join("\n"));
    fs.mkdirSync(path.join(root, "client", "js"), { recursive: true });
    fs.writeFileSync(path.join(root, "client", "js", "consumer.cjs"), [
      "const source = readRenderer('../runtime/scene3d/mount-webgl.ts')",
      "",
    ].join("\n"));
    fs.mkdirSync(path.join(root, "scripts"), { recursive: true });
    fs.writeFileSync(path.join(root, "scripts", "renderer-check.sh"), "grep webgpu.ts \"$1\"\n");
    fs.mkdirSync(path.join(root, "client", "jsx"), { recursive: true });
    fs.writeFileSync(path.join(root, "client", "jsx", "probe.mts"), "export const p = 'webgpu.ts';\n");

    const findings = directRendererReferenceRegistryFindings(root, {
      contract: {
        sourceSets: architecture.sourceSets,
        extraProtectedSources: architecture.extraProtectedSources,
        directRendererReferenceSources: architecture.directRendererReferenceSources,
        intentionalDirectRendererReferences: [],
      },
    });
    assert.deepEqual(
      findings.map((finding) => `${finding.file}:${finding.source}:${finding.reason}`),
      [
        "client/js/consumer.cjs:mount-webgl.ts:unregistered direct renderer-source reference",
        "client/jsx/probe.mts:webgpu.ts:unregistered direct renderer-source reference",
        "render/bundle/bypass_test.go:webgpu.ts:unregistered direct renderer-source reference",
        "scene/capability/alias_test.go:webgl.ts:unregistered direct renderer-source reference",
        "scripts/renderer-check.sh:webgpu.ts:unregistered direct renderer-source reference",
      ],
    );
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("repo-wide renderer references scan Git-tracked text without suffix escape hatches", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-renderer-text-inventory-"));
  try {
    const fixtures = {
      Makefile: "probe:\n\t@echo webgpu.ts\n",
      "probe.gsx": "<script>const renderer = 'webgpu.ts'</script>\n",
      "probe.html": "<!-- webgpu.ts -->\n",
      "probe.py": "source = 'webgpu.ts'\n",
      "probe.yaml": "source: webgpu.ts\n",
      "probe.yml": "source: webgpu.ts\n",
      "probe.bash": "echo webgpu.ts\n",
      "client/js/runtime-probe.test.js": "const source = 'webgpu.ts';\n",
      "client/js/bootstrap-runtime-region-errors.test.js": "const source = 'webgpu.ts';\n",
      "client/js/bootstrap-feature-scene3d-webgpu.js": "const generated = 'webgpu.ts';\n",
      "client/js/bootstrap-feature-scene3d-webgpu.js.map": "{\"sources\":[\"webgpu.ts\"]}\n",
      "client/js/bootstrap-feature-scene3d-webgpu.js.gz": "webgpu.ts\n",
      "client/js/bootstrap-feature-scene3d-webgpu.js.br": "webgpu.ts\n",
      "binary.dat": Buffer.from([0x77, 0x65, 0x62, 0x67, 0x70, 0x75, 0x2e, 0x74, 0x73, 0x00]),
    };
    for (const [name, contents] of Object.entries(fixtures)) {
      fs.mkdirSync(path.dirname(path.join(root, name)), { recursive: true });
      fs.writeFileSync(path.join(root, name), contents);
    }
    gitInit(root);
    const counts = rendererReferenceCounts(root, {
      contract: {
        sourceSets: { webgpu: { sources: ["../runtime/scene3d/webgpu.ts"] } },
        directRendererReferenceSources: ["webgpu.ts"],
      },
    }).map((entry) => entry.file).sort();
    assert.deepEqual(counts, [
      "Makefile",
      "client/js/bootstrap-runtime-region-errors.test.js",
      "client/js/runtime-probe.test.js",
      "probe.bash",
      "probe.gsx",
      "probe.html",
      "probe.py",
      "probe.yaml",
      "probe.yml",
    ]);
    const generated = [...generatedBootstrapArtifactPaths()];
    assert.equal(generated.length, 64);
    assert.ok(generated.includes("client/js/bootstrap-feature-scene3d-webgpu.js"));
    assert.ok(generated.includes("client/js/bootstrap-feature-scene3d-webgpu.js.map"));
    assert.ok(generated.includes("client/js/bootstrap-feature-scene3d-webgpu.js.gz"));
    assert.ok(generated.includes("client/js/bootstrap-feature-scene3d-webgpu.js.br"));
    assert.ok(!generated.includes("client/js/bootstrap-runtime-region-errors.test.js"));
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("governed renderer sources reject symlink and duplicate physical aliases", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-renderer-alias-"));
  try {
    const clientRoot = path.join(root, "client", "js");
    fs.mkdirSync(path.join(root, "client", "runtime", "scene3d"), { recursive: true });
    fs.mkdirSync(path.join(clientRoot, "bootstrap-src"), { recursive: true });
    fs.writeFileSync(path.join(root, "client", "runtime", "scene3d", "webgpu.ts"), "function renderer() {}\n");
    fs.symlinkSync("webgpu.ts", path.join(root, "client", "runtime", "scene3d", "webgpu-alias.ts"));
    const chunk = "bootstrap-feature-scene3d-webgpu.js";
    const sources = ["../runtime/scene3d/webgpu.ts", "../runtime/scene3d/webgpu-alias.ts"];
    assert.throws(
      () => rendererBackendSources("webgpu", {
        root: clientRoot,
        contract: { sourceSets: { webgpu: { chunk, chunkSources: sources, sources } }, directRendererReferenceSources: ["webgpu-alias.ts", "webgpu.ts"] },
        manifest: manifest(chunk, sources),
        availableSources: sources,
      }),
      /must not be a symlink|aliases the same physical file/,
    );
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("extra protected renderer sources receive full physical validation", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-renderer-extra-"));
  try {
    const clientRoot = path.join(root, "client", "js");
    const runtimeRoot = path.join(root, "client", "runtime", "scene3d");
    fs.mkdirSync(runtimeRoot, { recursive: true });
    fs.mkdirSync(path.join(clientRoot, "bootstrap-src"), { recursive: true });
    fs.writeFileSync(path.join(runtimeRoot, "webgpu.ts"), "function renderer() {}\n");
    fs.writeFileSync(path.join(runtimeRoot, "mount-webgl.ts"), "function mount() {}\n");
    fs.writeFileSync(path.join(root, "outside.ts"), "function outside() {}\n");
    const chunk = "bootstrap-feature-scene3d-webgpu.js";
    const sources = ["../runtime/scene3d/webgpu.ts"];
    const baseContract = {
      sourceSets: {
        webgpu: {
          chunk,
          chunkSources: sources,
          chunkSourceRoles: { "../runtime/scene3d/webgpu.ts": "governed-renderer" },
          sources,
        },
      },
    };
    const run = (extraProtectedSources) => rendererBackendSources("webgpu", {
      root: clientRoot,
      contract: {
        ...baseContract,
        extraProtectedSources,
        directRendererReferenceSources: ["webgpu.ts"].concat(extraProtectedSources.map((source) => path.posix.basename(source))).sort(),
      },
      manifest: manifest(chunk, sources),
      availableSources: sources,
    });
    assert.deepEqual(run(["../runtime/scene3d/mount-webgl.ts"]), sources);
    assert.throws(() => run(["../runtime/scene3d/./mount-webgl.ts"]), /malformed/);
    assert.throws(() => run(["../runtime/scene3d/does-not-exist.ts"]), /no such file|ENOENT/);
    assert.throws(() => run(["../../outside.ts"]), /resolves outside/);
    fs.symlinkSync(path.join(root, "outside.ts"), path.join(runtimeRoot, "mount-webgl-symlink.ts"));
    assert.throws(() => run(["../runtime/scene3d/mount-webgl-symlink.ts"]), /must not be a symlink/);
    fs.symlinkSync(runtimeRoot, path.join(runtimeRoot, "alias-dir"));
    assert.throws(() => run(["../runtime/scene3d/alias-dir/mount-webgl.ts"]), /symlink component/);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("backend chunk rosters reject unclassified physical renderer-looking sources", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-renderer-roster-"));
  try {
    const clientRoot = path.join(root, "client", "js");
    const bootstrapRoot = path.join(clientRoot, "bootstrap-src");
    const runtimeRoot = path.join(root, "client", "runtime", "scene3d");
    fs.mkdirSync(bootstrapRoot, { recursive: true });
    fs.mkdirSync(runtimeRoot, { recursive: true });
    fs.writeFileSync(path.join(runtimeRoot, "webgpu.ts"), "function renderer() {}\n");
    fs.writeFileSync(path.join(bootstrapRoot, "16a9-renderer-webgpu-secret.ts"), "function rendererSecret() {}\n");
    fs.writeFileSync(path.join(bootstrapRoot, "26e2-feature-scene3d-webgpu-core.ts"), "function createSceneWebGPURendererSecret() {}\n");
    const chunk = "bootstrap-feature-scene3d-webgpu.js";
    const sources = ["../runtime/scene3d/webgpu.ts", "bootstrap-src/16a9-renderer-webgpu-secret.ts"];
    assert.throws(
      () => rendererBackendSources("webgpu", {
        root: clientRoot,
        contract: {
          sourceSets: {
            webgpu: {
              chunk,
              chunkSources: sources,
              chunkSourceRoles: { "../runtime/scene3d/webgpu.ts": "governed-renderer" },
              sources: ["../runtime/scene3d/webgpu.ts"],
            },
          },
          directRendererReferenceSources: ["webgpu.ts"],
        },
        manifest: manifest(chunk, sources),
        availableSources: ["../runtime/scene3d/webgpu.ts"],
      }),
      /chunkSourceRoles must exactly match chunkSources|looks renderer-owned/,
    );
    assert.throws(
      () => rendererBackendSources("webgpu", {
        root: clientRoot,
        contract: {
          sourceSets: {
            webgpu: {
              chunk,
              chunkSources: sources,
              chunkSourceRoles: {
                "../runtime/scene3d/webgpu.ts": "governed-renderer",
                "bootstrap-src/16a9-renderer-webgpu-secret.ts": "backend-support",
              },
              sources: ["../runtime/scene3d/webgpu.ts"],
            },
          },
          directRendererReferenceSources: ["webgpu.ts"],
        },
        manifest: manifest(chunk, sources),
        availableSources: ["../runtime/scene3d/webgpu.ts"],
      }),
      /not authorized for this path|looks renderer-owned/,
    );
    const camouflagedSources = ["../runtime/scene3d/webgpu.ts", "bootstrap-src/26e2-feature-scene3d-webgpu-core.ts"];
    assert.throws(
      () => rendererBackendSources("webgpu", {
        root: clientRoot,
        contract: {
          sourceSets: {
            webgpu: {
              chunk,
              chunkSources: camouflagedSources,
              chunkSourceRoles: {
                "../runtime/scene3d/webgpu.ts": "governed-renderer",
                "bootstrap-src/26e2-feature-scene3d-webgpu-core.ts": "backend-support",
              },
              sources: ["../runtime/scene3d/webgpu.ts"],
            },
          },
          directRendererReferenceSources: ["webgpu.ts"],
        },
        manifest: manifest(chunk, camouflagedSources),
        availableSources: ["../runtime/scene3d/webgpu.ts"],
      }),
      /not authorized for this path/,
    );
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("governed renderer sources reject hardlink path aliases", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-renderer-hardlink-"));
  try {
    const clientRoot = path.join(root, "client", "js");
    fs.mkdirSync(path.join(root, "client", "runtime", "scene3d"), { recursive: true });
    fs.mkdirSync(path.join(clientRoot, "bootstrap-src"), { recursive: true });
    const original = path.join(root, "client", "runtime", "scene3d", "webgpu.ts");
    const alias = path.join(root, "client", "runtime", "scene3d", "webgpu-hardlink.ts");
    fs.writeFileSync(original, "function renderer() {}\n");
    fs.linkSync(original, alias);
    const chunk = "bootstrap-feature-scene3d-webgpu.js";
    const sources = ["../runtime/scene3d/webgpu.ts", "../runtime/scene3d/webgpu-hardlink.ts"];
    assert.throws(
      () => rendererBackendSources("webgpu", {
        root: clientRoot,
        contract: { sourceSets: { webgpu: { chunk, chunkSources: sources, sources } }, directRendererReferenceSources: ["webgpu-hardlink.ts", "webgpu.ts"] },
        manifest: manifest(chunk, sources),
        availableSources: sources,
      }),
      /aliases the same physical file|hardlinks/,
    );
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("governed renderer sources reject single listed hardlinks with out-of-roster aliases", (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-renderer-single-hardlink-"));
  try {
    const clientRoot = path.join(root, "client", "js");
    const runtimeRoot = path.join(root, "client", "runtime", "scene3d");
    fs.mkdirSync(runtimeRoot, { recursive: true });
    fs.mkdirSync(path.join(clientRoot, "bootstrap-src"), { recursive: true });
    const outside = path.join(root, "outside-webgpu.ts");
    const governed = path.join(runtimeRoot, "webgpu.ts");
    fs.writeFileSync(outside, "function renderer() {}\n");
    try {
      fs.linkSync(outside, governed);
    } catch (err) {
      t.skip(`hardlinks unavailable on this filesystem: ${err.message}`);
    }
    const chunk = "bootstrap-feature-scene3d-webgpu.js";
    const sources = ["../runtime/scene3d/webgpu.ts"];
    assert.throws(
      () => rendererBackendSources("webgpu", {
        root: clientRoot,
        contract: { sourceSets: { webgpu: { chunk, chunkSources: sources, sources } }, directRendererReferenceSources: ["webgpu.ts"] },
        manifest: manifest(chunk, sources),
        availableSources: sources,
      }),
      /hardlinks/,
    );
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

function physicalLines(source) {
  if (source.length === 0) return 0;
  return source.split("\n").length - (source.endsWith("\n") ? 1 : 0);
}

test("renderer source-set and file lines can only decrease from the Canopy-derived baseline", () => {
  for (const [source, maxLines] of Object.entries(architecture.files)) {
    const contents = fs.readFileSync(path.join(__dirname, source), "utf8");
    assert.ok(physicalLines(contents) <= maxLines, `${source} exceeds ${maxLines} physical lines`);
  }
  for (const [backend, baseline] of Object.entries(architecture.sourceSets)) {
    const counts = baseline.sources.map((source) => physicalLines(fs.readFileSync(path.join(__dirname, source), "utf8")));
    assert.ok(counts.reduce((sum, count) => sum + count, 0) <= baseline.maxLines, `${backend} source set grew`);
    assert.ok(Math.max(...counts) <= baseline.maxFileLines, `${backend} maximum renderer file grew`);
  }
});

test("governance line budget records the live authored governance surface", () => {
  const files = [
    "scene3d-renderer-source-set.js",
    "scene3d-renderer-architecture.test.js",
    "testdata/scene3d-renderer-architecture.json",
    "../../cmd/buildbootstrap/scene3d_renderer_architecture_test.go",
    "../../internal/scene3drenderersource/source.go",
  ];
  const actual = files.reduce((sum, file) => sum + physicalLines(fs.readFileSync(path.join(__dirname, file), "utf8")), 0);
  assert.equal(actual, architecture.governanceBudgetRevision.actualGovernanceLinesAtReview);
  assert.ok(actual <= architecture.governanceBudgetRevision.revisedGovernanceLineBudget);
});

function rendererSetSizes(chunks) {
  const sum = (suffix) => chunks.reduce((total, chunk) => total + fs.statSync(path.join(__dirname, chunk + suffix)).size, 0);
  return { raw: sum(""), gzip: sum(".gz"), brotli: sum(".br") };
}

function assertAtOrBelow(actual, baseline, label) {
  for (const metric of ["raw", "gzip", "brotli"]) {
    assert.ok(actual[metric] <= baseline[metric], `${label} ${metric} ${actual[metric]} exceeds ${baseline[metric]}`);
  }
}

test("renderer-set bytes cannot hide growth between base and lazy chunks", () => {
  for (const [label, baseline] of Object.entries(architecture.rendererSets)) {
    assertAtOrBelow(rendererSetSizes(baseline.chunks), baseline, label);
  }
  assert.throws(
    () => assertAtOrBelow({ raw: 11, gzip: 8, brotli: 6 }, { raw: 10, gzip: 8, brotli: 6 }, "negative fixture"),
    /negative fixture raw 11 exceeds 10/,
  );
});

function assertRendererABI(renderer, backend) {
  const contract = architecture.rendererABI;
  assert.deepEqual(Object.keys(renderer).sort(), contract.implementations[backend]);
  assert.equal(renderer.kind, backend);
  assert.equal(typeof renderer.render, "function");
  assert.equal(typeof renderer.dispose, "function");
  for (const key of contract.optional) {
    if (!(key in renderer)) continue;
    if (key === "supportsRetainedGeometry") assert.equal(typeof renderer[key], "boolean");
    else if (key === "textureVariantContext") assert.equal(typeof renderer[key], "object");
    else assert.equal(typeof renderer[key], "function");
  }
}

test("WebGL and WebGPU pin the mount-facing renderer ABI while queuePick stays experimental", async () => {
  const webgl = createWebGLRendererForPost().renderer;
  const webgpu = (await createBoardWebGPUHarness()).renderer;
  try {
    assertRendererABI(webgl, "webgl");
    assertRendererABI(webgpu, "webgpu");
    assert.equal(typeof webgpu.queuePick, "function");
    assert.ok(architecture.rendererABI.experimental.includes("queuePick"));
    assert.ok(!architecture.rendererABI.required.includes("queuePick"));
  } finally {
    webgl.dispose();
    webgpu.dispose();
  }
});
