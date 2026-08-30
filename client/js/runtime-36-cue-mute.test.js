"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { navigationSource, FakeElement, createContext, runScript, installManualClock, installManualTimers, createFakeAudioContextHarness } = require("./runtime-test-harness.js");

function cueCountdown() {
  const el = new FakeElement("span", null);
  el.setAttribute("data-gosx-countdown", "1970-01-01T00:00:05Z");
  el.setAttribute("data-gosx-countdown-format", "mm:ss");
  el.setAttribute("data-gosx-countdown-cue", "5s:beep");
  return el;
}
function toggleButton() {
  const button = new FakeElement("button", null);
  button.setAttribute("data-gosx-cue-toggle", "");
  button.setAttribute("data-gosx-cue-label-on", "Sound on");
  button.setAttribute("data-gosx-cue-label-off", "Sound off");
  button.textContent = "Sound on";
  return button;
}
function fakeStorage(seed) {
  const store = Object.assign({}, seed || {});
  return { getItem: (k) => (k in store ? store[k] : null), setItem: (k, v) => { store[k] = String(v); }, store };
}
function boot(elements, storage) {
  const audio = createFakeAudioContextHarness();
  const env = createContext({ elements, AudioContext: audio.AudioContext });
  if (storage) env.context.window.localStorage = storage;
  const clock = installManualClock(env.context, 0);
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  return { audio, env, clock, timers, cues: env.context.__gosx.cues };
}

test("a muted runtime skips a cue instead of queuing it; unmute plays only the next one", () => {
  const { audio, env, clock, timers, cues } = boot([cueCountdown()]);
  env.document.dispatchEvent({ type: "pointerdown" }); // primes the AudioContext (:3326)
  const seen = [];
  env.document.addEventListener("gosx:cue:muted", (event) => seen.push(event.detail.muted));
  cues.mute();
  assert.equal(cues.muted(), true);
  clock.advance(1000); timers.runInterval(1000); // remainder 4s: the 5s tier crosses now
  assert.deepEqual(env.context.__gosx.navigation.debugCueLog(), [], "a muted cue never schedules a tone");
  assert.equal(audio.instances[0].oscillators.length, 0);
  cues.unmute();
  clock.advance(1000); timers.runInterval(1000);
  assert.deepEqual(env.context.__gosx.navigation.debugCueLog(), [], "the skipped crossing is not replayed");
  assert.deepEqual(seen, [true, false]);
});

test("a data-gosx-cue-toggle click flips the state and a control swapped in later re-syncs", () => {
  const region = new FakeElement("div", null);
  const first = toggleButton();
  region.appendChild(first);
  const { env, cues } = boot([region]);
  assert.equal(first.getAttribute("aria-pressed"), "true");
  assert.equal(first.getAttribute("data-gosx-cue-state"), "on");
  env.document.dispatchEvent({ type: "click", target: first });
  assert.equal(cues.muted(), true);
  assert.equal(first.getAttribute("aria-pressed"), "false");
  assert.equal(first.getAttribute("data-gosx-cue-state"), "off");
  assert.equal(first.textContent, "Sound off");
  region.removeChild(first);
  const second = toggleButton(); // the server renders "on" after a swap
  second.setAttribute("aria-pressed", "true");
  region.appendChild(second);
  env.document.dispatchEvent({ type: "gosx:region:after", detail: { element: region, url: "/command" } });
  assert.equal(second.getAttribute("aria-pressed"), "false", "the rescan re-syncs a swapped-in control");
  assert.equal(second.textContent, "Sound off");
  env.document.dispatchEvent({ type: "click", target: second });
  assert.equal(cues.muted(), false);
  assert.equal(second.textContent, "Sound on");
});

test("the muted state persists under gosx:cues:muted and is read at boot before any cue can fire", () => {
  const storage = fakeStorage({ "gosx:cues:muted": "1" });
  const { env, clock, timers, cues } = boot([cueCountdown(), toggleButton()], storage);
  assert.equal(cues.muted(), true, "boot reads the stored state");
  env.document.dispatchEvent({ type: "pointerdown" });
  clock.advance(1000); timers.runInterval(1000);
  assert.deepEqual(env.context.__gosx.navigation.debugCueLog(), []);
  cues.toggle();
  assert.equal(storage.store["gosx:cues:muted"], "0");
  cues.toggle();
  assert.equal(storage.store["gosx:cues:muted"], "1");
});

test("a missing or throwing localStorage never throws and leaves cues unmuted", () => {
  assert.equal(boot([toggleButton()]).cues.muted(), false);
  const hostile = { getItem() { throw new Error("private mode"); }, setItem() { throw new Error("private mode"); } };
  const { cues } = boot([toggleButton()], hostile);
  assert.equal(cues.muted(), false);
  assert.doesNotThrow(() => cues.mute());
  assert.equal(cues.muted(), true, "the in-memory state still flips when storage is unavailable");
});
