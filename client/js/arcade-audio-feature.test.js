"use strict";
// The standalone arcade-audio feature chunk gives a page without hubs the same
// window.__gosx.arcadeAudio the hubs chunk publishes.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const { createContext, runScript } = require("./runtime-test-harness.js");

const source = fs.readFileSync(path.join(__dirname, "bootstrap-feature-arcade-audio.js"), "utf8");
const hubsSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-hubs.js"), "utf8");

test("arcade-audio chunk publishes the synth seam without any hub code", () => {
  const env = createContext({});
  delete env.context.__gosx.arcadeAudio;
  runScript(source, env.context, "bootstrap-feature-arcade-audio.js");
  const audio = env.context.__gosx.arcadeAudio;
  assert.ok(audio, "arcadeAudio is published");
  for (const method of ["play", "playPatch", "stop", "unlock"]) {
    assert.equal(typeof audio[method], "function", method);
  }
  assert.ok(!env.context.__gosx.hubs || env.context.__gosx.hubs.size === 0, "no hub was connected");
  assert.doesNotMatch(source, /hubConnections|createHubInputController/, "no hub transport in the chunk");
  assert.ok(source.length < hubsSource.length / 2, "the chunk is a fraction of the hubs chunk");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("arcade-audio chunk keeps a synth another bundle already published", () => {
  const env = createContext({});
  const existing = { play() {}, playPatch() {}, stop() {}, unlock() {} };
  env.context.__gosx.arcadeAudio = existing;
  runScript(source, env.context, "bootstrap-feature-arcade-audio.js");
  assert.equal(env.context.__gosx.arcadeAudio, existing);
});
