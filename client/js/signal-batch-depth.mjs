// Does a batch that needs a deep stack get through a real GoSX WASM runtime? Run it on a TinyGo
// build, because only TinyGo has a system stack with a size of its own:
//
//   node client/js/signal-batch-depth.mjs --direct --wasm core.wasm --wasm-exec wasm_exec.js
//
// A TinyGo runtime built with the direct batch entry (it exports gosx_batch_apply) applies a
// batch on its system stack: the decoding of the JSON in it, the island renders it wakes and
// the notifications all run there. --direct publishes the exports as the loaders do, so the
// page sends batches that way, and fails if the runtime lacks them. The system stack is as
// large as the linker makes it (cmd/gosx tinyGoBuildArgs sets 256 KiB, the size of a goroutine
// stack; the linker's own default is 64 KiB), and an overflow is a trap, "memory access out of
// bounds", after which every call into the runtime fails the same way.
//
// Two kinds of batch, each sent --batches (default 3) times at each depth:
//
//   island  one island whose shared-signal read sits under --island-depth (default 32,100)
//           nested elements; the batch changes that signal, and every batch must patch the
//           deepest text node of each island with the new value. With a 64 KiB system stack
//           the core runtime fails from 32 levels. With a 256 KiB one it takes about 128, as
//           the call that hydrates the island does on a goroutine stack of that size.
//   json    a batch whose value nests --json-depth (default 128,256) arrays deep; every batch
//           must be accepted, and the value must read back as the text that was sent. With a
//           64 KiB system stack it fails from about 105 levels; with a 256 KiB one it takes
//           about 410, as the call through __gosx_set_input_batch does on a goroutine stack
//           of that size.
//
// A depth list runs from the shallowest, and the first failure ends the run, because the runtime
// does not answer after it. At the end the runtime must still answer a plain call. Exit code 1
// if anything failed. The defaults keep a margin below the limits, so a new TinyGo that spends a
// little more stack on each level does not fail the gate; a build that loses the system stack
// flag still does.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { bootPage } from "./signal-batch-page.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const argv = process.argv.slice(2);
const has = (name) => argv.includes("--" + name);
function option(name, fallback) {
  const at = argv.indexOf("--" + name);
  return at < 0 ? fallback : argv[at + 1];
}
const depthList = (name, fallback) => String(option(name, fallback)).split(",").map((depth) => Number(depth.trim())).filter((depth) => depth > 0);

if (has("help") || argv.length === 0) {
  const source = fs.readFileSync(fileURLToPath(import.meta.url), "utf8");
  console.log(source.split("\n").filter((l) => l.startsWith("//")).map((l) => l.slice(3)).join("\n"));
  process.exit(argv.length === 0 ? 2 : 0);
}

const settings = {
  wasm: option("wasm"),
  wasmExec: option("wasm-exec"),
  tree: path.resolve(option("tree", path.resolve(here, "..", ".."))),
  label: option("label", "run"),
  direct: has("direct"),
  islandDepths: depthList("island-depth", "32,100"),
  jsonDepths: depthList("json-depth", "128,256"),
  batches: Number(option("batches", "3")),
};
if (!settings.wasm || !settings.wasmExec) {
  console.error("usage: --wasm <runtime.wasm> --wasm-exec <wasm_exec.js> [--direct] (--help for the rest)");
  process.exit(2);
}

// An island program whose only shared-signal read, $deep, sits under `depth` nested elements.
function deepIsland(depth) {
  const nodes = [];
  for (let i = 0; i < depth; i++) nodes.push({ kind: 0, tag: "div", text: "", expr: 0, attrs: null, children: [i + 1] });
  nodes.push({ kind: 2, tag: "", text: "", expr: 0, attrs: null, children: null });
  return JSON.stringify({
    name: "Deep", props: null, nodes, root: 0,
    exprs: [{ op: 5, operands: null, value: "$deep", type: 1 }, { op: 1, operands: null, value: "0", type: 1 }],
    signals: [{ name: "$deep", type: 1, init: 1 }], computeds: null, handlers: null, static_mask: null,
  });
}

const patches = [];
const page = await bootPage({
  wasm: settings.wasm, wasmExec: settings.wasmExec, tree: settings.tree, direct: settings.direct, id: "depth",
  applyPatches: (island, text) => patches.push({ island, text: String(text) }),
});
const { ctx } = page;
const problems = [];
const describe = (error) => String(error && error.message || error);
const summary = [];

// ---- island ---------------------------------------------------------------

// Every island stays on the page, so each batch also re-renders the shallower ones.
const islands = [];
let sent = 0;
for (const depth of settings.islandDepths) {
  if (problems.length) break;
  const id = "deep-" + depth;
  let refused = "";
  try {
    const result = globalThis.__gosx_hydrate(id, "Deep", "{}", deepIsland(depth), "json");
    refused = result === null || result === undefined ? "" : String(result);
  } catch (error) {
    refused = describe(error);
  }
  if (refused) {
    problems.push(`island of ${depth} levels: hydrating it failed (${refused}); the call through __gosx_hydrate runs on a goroutine stack of its own`);
    break;
  }
  islands.push({ id, depth, textPath: Array(depth).fill("0").join("/") });
  for (let i = 0; i < settings.batches && !problems.length; i++) {
    const value = String(100 + ++sent);
    const before = patches.length;
    try {
      ctx.setSignalsJSON(JSON.stringify({ $deep: Number(value) }), true);
    } catch (error) {
      problems.push(`island of ${depth} levels, batch ${i + 1}: ${describe(error)}`);
      break;
    }
    const heard = patches.slice(before);
    for (const island of islands) {
      const ops = heard.filter((patch) => patch.island === island.id).flatMap((patch) => JSON.parse(patch.text));
      if (!(ops.length === 1 && ops[0].path === island.textPath && ops[0].text === value)) {
        problems.push(`island of ${depth} levels, batch ${i + 1}: expected one patch of text ${value} for the island of ${island.depth} levels, got ${JSON.stringify(ops).slice(0, 200)}`);
        break;
      }
    }
  }
}
if (!problems.length && islands.length) summary.push(`island ${islands.map((island) => island.depth).join(", ")} levels: ${settings.batches} batches patched each`);

// ---- json -----------------------------------------------------------------

const jsonDone = [];
for (const depth of settings.jsonDepths) {
  if (problems.length) break;
  const name = "$json" + depth;
  const nested = (i) => "[".repeat(depth) + i + "]".repeat(depth);
  const heard = [];
  ctx.subscribeSignalJSON(name, (text) => { heard.push(text); }, { immediate: false });
  for (let i = 0; i < settings.batches && !problems.length; i++) {
    const want = nested(i);
    const before = heard.length;
    try {
      ctx.setSignalsJSON('{"' + name + '":' + want + "}", false);
    } catch (error) {
      problems.push(`json of ${depth} levels, batch ${i + 1}: ${describe(error)}`);
      break;
    }
    let held = "";
    try {
      held = String(globalThis.__gosx_get_shared_signal(name));
    } catch (error) {
      held = "threw: " + describe(error);
    }
    if (!(heard.length === before + 1 && heard[before] === want && held === want)) {
      problems.push(`json of ${depth} levels, batch ${i + 1}: the page heard ${heard.length - before} announcement(s) and the runtime holds ${held.slice(0, 40)}, not the text that was sent`);
    }
  }
  if (!problems.length) jsonDone.push(depth);
}
if (!problems.length && jsonDone.length) summary.push(`json ${jsonDone.join(", ")} levels: ${settings.batches} batches read back each`);

// ---- the runtime still works ------------------------------------------------

try {
  const answer = String(globalThis.__gosx_get_shared_signal("$deep"));
  if (answer.startsWith("error:")) problems.push("the runtime answered a plain call with " + answer);
} catch (error) {
  problems.push("the runtime no longer answers a plain call: " + describe(error));
}
await new Promise((resolve) => setTimeout(resolve, 20));
if (page.crashed()) problems.push("the runtime stopped: " + page.crashed());

if (problems.length) {
  console.log(`FAIL ${settings.label}${settings.direct ? " (direct)" : ""}:`);
  for (const problem of problems) console.log("  " + problem);
  process.exit(1);
}
console.log(`ok   ${settings.label}${settings.direct ? " (direct)" : ""}: ${summary.join("; ")}; the runtime still answers`);
process.exit(0);
