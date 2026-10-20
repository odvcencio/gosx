"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

const {
  buildOwnedChromeStderrRanges,
  chromeDiagnosticFailures,
  normalizeOwnedRanges,
  scanOwnedChromeDiagnostics,
} = require("./testdata/scene3d-browser-diagnostics.cjs");

const SWAP = ["non-existent mailbox"];
const LIFECYCLE = ["device was destroyed"];

function stderrFixture(gl, wg, startup = "startup noise\n") {
  const capability = "capability: non-existent mailbox\n";
  const glStart = Buffer.byteLength(startup + capability);
  const glContent = gl + "\n";
  const wgStart = glStart + Buffer.byteLength(glContent);
  const wgContent = wg + "\n";
  const teardown = "teardown: device was destroyed\n";
  return {
    raw: Buffer.from(startup + capability + glContent + wgContent + teardown),
    capabilityRange: {
      startByte: Buffer.byteLength(startup),
      afterTargetCloseByte: glStart,
    },
    ranges: [
      { name: "gl", startByte: glStart, beforeTargetCloseByte: wgStart },
      {
        name: "wg",
        startByte: wgStart,
        beforeTargetCloseByte: wgStart + Buffer.byteLength(wgContent),
      },
    ],
  };
}

test("native proof diagnostics are governed only inside live renderer ranges", () => {
  const outside = stderrFixture("clean gl", "clean wg");
  const outsideRanges = buildOwnedChromeStderrRanges(
    outside.capabilityRange, outside.ranges,
  );
  const excluded = scanOwnedChromeDiagnostics(
    outside.raw, outsideRanges, SWAP, LIFECYCLE,
  );
  assert.deepEqual(excluded.swapFindings, []);
  assert.deepEqual(excluded.preTeardownLifecycleFindings, []);
  assert.deepEqual(excluded.ownedStderrRanges, [
    { name: "chrome-startup", startByte: 0, beforeTargetCloseByte: 14 },
    ...outside.ranges,
  ]);
  assert.equal(excluded.ownedStderrBytes,
    Buffer.byteLength("startup noise\n") + Buffer.byteLength("clean gl\n") +
      Buffer.byteLength("clean wg\n"));

  const inside = stderrFixture(
    "renderer: non-existent mailbox",
    "renderer: device was destroyed",
  );
  const rejected = scanOwnedChromeDiagnostics(
    inside.raw,
    buildOwnedChromeStderrRanges(inside.capabilityRange, inside.ranges),
    SWAP,
    LIFECYCLE,
  );
  assert.deepEqual(rejected.swapFindings,
    [{ needle: "non-existent mailbox", count: 1 }]);
  assert.deepEqual(rejected.preTeardownLifecycleFindings,
    [{ needle: "device was destroyed", count: 1 }]);

  const startup = stderrFixture(
    "clean gl", "clean wg", "startup: non-existent mailbox\n",
  );
  const startupRejected = scanOwnedChromeDiagnostics(
    startup.raw,
    buildOwnedChromeStderrRanges(startup.capabilityRange, startup.ranges),
    SWAP,
    LIFECYCLE,
  );
  assert.deepEqual(startupRejected.swapFindings,
    [{ needle: "non-existent mailbox", count: 1 }]);
});

test("native proof range validation fails closed on missing, overlapping, or invalid ownership", () => {
  assert.throws(() => normalizeOwnedRanges(10, []), /ranges are missing/);
  assert.throws(() => normalizeOwnedRanges(10, [
    { name: "gl", startByte: 2, beforeTargetCloseByte: 8 },
    { name: "wg", startByte: 7, beforeTargetCloseByte: 9 },
  ]), /overlaps/);
  assert.throws(() => normalizeOwnedRanges(10, [
    { name: "gl", startByte: 2, beforeTargetCloseByte: 11 },
  ]), /invalid bounds/);
  assert.throws(() => normalizeOwnedRanges(10, [
    { name: "gl", startByte: 2, beforeTargetCloseByte: 3 },
    { name: "gl", startByte: 4, beforeTargetCloseByte: 5 },
  ]), /invalid name/);
  assert.throws(() => buildOwnedChromeStderrRanges(
    { startByte: 2, afterTargetCloseByte: 8 },
    [{ name: "gl", startByte: 7, beforeTargetCloseByte: 9 }],
  ), /overlaps capability teardown/);
});

test("capability teardown does not contaminate the no-submit causal error set", () => {
  const expectedNoSubmitErrors = [
    "[wg] baseline product queue submission was not forwarded",
    "[wg] baseline mapped renderer target contains 0 non-background pixels, expected > 20",
    "[wg] playing product queue submission was not forwarded",
    "[wg] playing mapped renderer target contains 0 non-background pixels, expected > 20",
    "[wg] playing frame changed 0 pixels, expected > 20",
    "[wg] restored product queue submission was not forwarded",
    "[wg] restored mapped renderer target contains 0 non-background pixels, expected > 20",
  ];
  const outside = stderrFixture("clean gl", "clean wg");
  const excluded = scanOwnedChromeDiagnostics(
    outside.raw,
    buildOwnedChromeStderrRanges(outside.capabilityRange, outside.ranges),
    SWAP,
    LIFECYCLE,
  );
  assert.deepEqual(
    expectedNoSubmitErrors.concat(chromeDiagnosticFailures({ ...excluded, scanError: "" })),
    expectedNoSubmitErrors,
  );

  const inside = stderrFixture("clean gl", "renderer: non-existent mailbox");
  const rejected = scanOwnedChromeDiagnostics(
    inside.raw,
    buildOwnedChromeStderrRanges(inside.capabilityRange, inside.ranges),
    SWAP,
    LIFECYCLE,
  );
  assert.deepEqual(chromeDiagnosticFailures({ ...rejected, scanError: "" }), [
    "Chrome stderr contains forbidden swap/SharedImage diagnostics: " +
      '[{"needle":"non-existent mailbox","count":1}]',
  ]);
});

test("adapter proof drains capability teardown before renderer case ownership", async () => {
  const source = fs.readFileSync(path.join(
    __dirname, "testdata", "scene3d-adapter-hydrate-browser.cjs",
  ), "utf8");
  const start = source.indexOf("async function drainCapabilityPage(send) {");
  const end = source.indexOf("\nasync function poll", start);
  assert.ok(start >= 0 && end > start, "capability drain helper must remain extractable");

  const events = [];
  let loaded;
  const context = {
    CAPABILITY_TEARDOWN_DRAIN_MS: 100,
    STEP_MS: 20000,
    currentCaseName: "",
    currentCasePhase: "",
    waitForEvent(name, timeout) {
      events.push(["wait", name, timeout]);
      return new Promise((resolve) => { loaded = resolve; });
    },
    async sleep(ms) { events.push(["sleep", ms]); },
  };
  const drain = vm.runInNewContext(source.slice(start, end) + "; drainCapabilityPage", context);
  await drain(async (method, params) => {
    events.push(["send", method, params]);
    loaded();
  });
  assert.equal(context.currentCaseName, "caps");
  assert.equal(context.currentCasePhase, "capability-probe");
  assert.deepEqual(JSON.parse(JSON.stringify(events)), [
    ["wait", "Page.loadEventFired", 20000],
    ["send", "Page.navigate", { url: "about:blank" }],
    ["sleep", 100],
  ]);

  const drainCall = source.indexOf("await drainCapabilityPage(send);");
  const firstCase = source.indexOf("for (const c of CASES) await runCase(send, c);");
  assert.ok(drainCall > source.indexOf("nativeCaps = await evalSend"));
  assert.ok(firstCase > drainCall);
});
