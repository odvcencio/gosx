"use strict";
// The page's one audio mixer (05-document-env.ts): five buses, shared by clips,
// the arcade synth, streamed <audio> elements and the Go game/audio package.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { bootstrapSource, createContext, runScript } = require("./runtime-test-harness.js");

const bootstrapFeatureArcadeAudioSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-arcade-audio.js"), "utf8");

class Param {
  constructor(value) {
    this.value = value;
    this.targets = [];
  }
  setValueAtTime(value) {
    this.value = value;
  }
  setTargetAtTime(value, time, constant) {
    this.targets.push({ value, time, constant });
    this.value = value;
  }
  cancelScheduledValues() {}
  linearRampToValueAtTime(value) {
    this.value = value;
  }
  exponentialRampToValueAtTime(value) {
    this.value = value;
  }
}

class Node {
  constructor(kind) {
    this.kind = kind;
    this.outputs = [];
  }
  connect(target) {
    this.outputs.push(target);
    return target;
  }
  disconnect() {
    this.outputs = [];
  }
}

function makeFakeContext() {
  const instances = [];
  class FakeAudioContext {
    constructor(options) {
      this.options = options;
      this.currentTime = 0;
      this.sampleRate = (options && options.sampleRate) || 44100;
      this.destination = new Node("destination");
      this.listener = { positionX: new Param(0), positionY: new Param(0), positionZ: new Param(0), forwardX: new Param(0), forwardY: new Param(0), forwardZ: new Param(-1), upX: new Param(0), upY: new Param(1), upZ: new Param(0) };
      this.gains = [];
      this.oscillators = [];
      this.mediaSources = [];
      instances.push(this);
    }
    resume() {
      return Promise.resolve();
    }
    createGain() {
      const node = new Node("gain");
      node.gain = new Param(1);
      this.gains.push(node);
      return node;
    }
    createDynamicsCompressor() {
      const node = new Node("compressor");
      for (const name of ["threshold", "knee", "ratio", "attack", "release"]) node[name] = new Param(0);
      return node;
    }
    createOscillator() {
      const node = new Node("oscillator");
      node.frequency = new Param(440);
      node.start = () => {};
      node.stop = () => {};
      this.oscillators.push(node);
      return node;
    }
    createMediaElementSource(element) {
      const node = new Node("media");
      node.element = element;
      this.mediaSources.push(node);
      return node;
    }
    createBufferSource() {
      const node = new Node("buffer");
      node.playbackRate = new Param(1);
      node.start = () => {};
      node.stop = () => {};
      return node;
    }
  }
  return { AudioContext: FakeAudioContext, instances };
}

function boot() {
  const fake = makeFakeContext();
  const env = createContext({ AudioContext: fake.AudioContext });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  return { fake, env, mixer: env.context.__gosx.audioMixer };
}

// reaches reports whether node feeds destination through its outputs.
function reaches(node, target, seen = new Set()) {
  if (node === target) return true;
  if (seen.has(node)) return false;
  seen.add(node);
  return node.outputs.some((next) => reaches(next, target, seen));
}

test("the mixer has master, music, sfx, voice and ui buses before any audio plays", () => {
  const { fake, mixer } = boot();
  assert.deepEqual([...mixer.buses], ["master", "music", "sfx", "voice", "ui"]);
  assert.equal(fake.instances.length, 0, "no AudioContext until the first audio use");
  const snap = mixer.snapshot();
  for (const id of mixer.buses) assert.equal(snap[id].volume, 1, id);
});

test("volumes set before the context exists are applied when it is created", () => {
  const { fake, mixer } = boot();
  mixer.setVolume("music", 0.25);
  mixer.setMuted("sfx", true);
  const music = mixer.input("music");
  const sfx = mixer.input("sfx");
  assert.equal(fake.instances.length, 1);
  assert.equal(music.gain.value, 0.25);
  assert.equal(sfx.gain.value, 0);
});

test("every bus reaches the destination through master", () => {
  const { fake, mixer } = boot();
  for (const id of ["music", "sfx", "voice", "ui"]) {
    assert.ok(reaches(mixer.input(id), fake.instances[0].destination), id + " reaches destination");
    assert.ok(mixer.input(id).outputs.includes(mixer.input("master")), id + " feeds master");
  }
});

test("mute keeps the volume and unmute restores it", () => {
  const { mixer } = boot();
  const node = mixer.input("music");
  mixer.setVolume("music", 0.6);
  mixer.setMuted("music", true);
  assert.equal(node.gain.value, 0);
  assert.equal(mixer.volume("music"), 0.6);
  mixer.setMuted("music", false);
  assert.equal(node.gain.value, 0.6);
});

test("volume changes ramp instead of stepping", () => {
  const { mixer } = boot();
  const node = mixer.input("sfx");
  mixer.setVolume("sfx", 0.5);
  const last = node.gain.targets[node.gain.targets.length - 1];
  assert.equal(last.value, 0.5);
  assert.ok(last.constant > 0);
});

test("ducking: the lowest factor wins and unduck restores the volume", () => {
  const { mixer } = boot();
  const node = mixer.input("music");
  mixer.setVolume("music", 0.8);
  mixer.duck("music", 0.5, 0.1, "voice-line");
  assert.equal(node.gain.value, 0.4);
  mixer.duck("music", 0.25, 0.1, "stinger");
  assert.equal(node.gain.value, 0.2);
  mixer.unduck("music", "stinger", 0.3);
  assert.equal(node.gain.value, 0.4);
  mixer.unduck("music", "voice-line", 0.3);
  assert.equal(node.gain.value, 0.8);
  assert.equal(mixer.volume("music"), 0.8);
});

test("onChange reports volume, mute and duck changes and can unsubscribe", () => {
  const { mixer } = boot();
  const seen = [];
  const off = mixer.onChange((detail) => seen.push(detail));
  mixer.setVolume("ui", 0.3);
  mixer.setMuted("ui", true);
  off();
  mixer.setVolume("ui", 0.9);
  assert.equal(seen.length, 2);
  assert.deepEqual([seen[0].bus, seen[0].volume, seen[1].muted], ["ui", 0.3, true]);
});

test("the shared context honours a requested sample rate on first creation", () => {
  const { fake, mixer } = boot();
  const ctx = mixer.context({ sampleRate: 48000 });
  assert.equal(ctx.sampleRate, 48000);
  assert.equal(mixer.context({ sampleRate: 22050 }), ctx, "the second call returns the same context");
  assert.equal(fake.instances.length, 1);
});

test("window.__gosx.audio clips play on the bus they name, and setBusVolume reaches the mixer", async () => {
  const { fake, env, mixer } = boot();
  env.context.fetch = () => Promise.resolve({ ok: true, arrayBuffer: () => Promise.resolve(new ArrayBuffer(8)) });
  fake.AudioContext.prototype.decodeAudioData = () => Promise.resolve({ duration: 1 });
  const audio = env.context.__gosx.audio;
  audio.setBusVolume("music", 0.4);
  assert.equal(mixer.volume("music"), 0.4);
  audio.load("theme", "theme.ogg", { bus: "music" });
  const handle = await audio.play("theme", { volume: 0.5 });
  assert.ok(handle, "play returned a handle");
  const ctx = fake.instances[0];
  assert.equal(mixer.input("music").gain.value, 0.4, "bus gain lives on the bus node");
  const clipGain = ctx.gains.find((g) => g.gain.value === 0.5);
  assert.ok(clipGain, "clip gain carries only the clip volume");
  assert.ok(clipGain.outputs.includes(mixer.input("music")), "clip feeds the music bus");
});

test("streamed audio elements route through the mixer", async () => {
  const { fake, env, mixer } = boot();
  const element = { play: () => Promise.resolve(), addEventListener() {}, pause() {} };
  env.document.createElement = () => element;
  // Without fetch the clip cannot be decoded, so it streams through an <audio> element.
  env.context.fetch = undefined;
  mixer.setVolume("music", 0.3);
  env.context.__gosx.audio.load("stream", "stream.mp3", { bus: "music" });
  await env.context.__gosx.audio.play("stream", { volume: 0.5 });
  const ctx = fake.instances[0];
  assert.equal(ctx.mediaSources.length, 1, "the element was captured into the context");
  assert.ok(ctx.mediaSources[0].outputs.includes(mixer.input("music")));
  assert.equal(element.volume, 0.5, "element volume carries clip volume only; the bus applies the rest");
});

test("the listener follows the pose the game gives it", () => {
  const { fake, mixer } = boot();
  mixer.input("sfx"); // audio in use: the context exists
  assert.equal(mixer.setListener({ x: 1, y: 2, z: 3 }, [0, 0, -1], { x: 0, y: 1, z: 0 }), true);
  const l = fake.instances[0].listener;
  assert.deepEqual([l.positionX.value, l.positionY.value, l.positionZ.value], [1, 2, 3]);
  assert.deepEqual([l.forwardX.value, l.forwardZ.value, l.upY.value], [0, -1, 1]);
});

test("the listener follows a Scene3D camera once audio is in use, and only then", () => {
  const { fake, mixer } = boot();
  assert.equal(mixer.updateCamera({ x: 5, y: 1, z: 2, rotationX: 0, rotationY: 0, rotationZ: 0 }), false);
  assert.equal(fake.instances.length, 0, "following the camera must not create the AudioContext");
  mixer.input("sfx");
  const l = fake.instances[0].listener;
  assert.deepEqual([l.positionX.value, l.positionZ.value], [5, 2], "the pose given before the context existed is applied on creation");
  // A camera turned a quarter turn about Y looks along -X in world space.
  mixer.updateCamera({ x: 0, y: 0, z: 0, rotationX: 0, rotationY: Math.PI / 2, rotationZ: 0 });
  assert.ok(Math.abs(l.forwardX.value + 1) < 1e-9 && Math.abs(l.forwardZ.value) < 1e-9, "forward = " + [l.forwardX.value, l.forwardZ.value]);
  mixer.followCamera(false);
  mixer.updateCamera({ x: 99, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0 });
  assert.equal(l.positionX.value, 0, "following can be switched off");
});

test("the arcade synth plays on the sfx bus of the shared context", () => {
  const { fake, env, mixer } = boot();
  env.context.__gosx.arcadeAudio.play("confirm");
  assert.equal(fake.instances.length, 1, "synth reused the mixer's context");
  const ctx = fake.instances[0];
  assert.ok(ctx.oscillators.length > 0);
  mixer.setVolume("sfx", 0);
  assert.equal(mixer.input("sfx").gain.value, 0);
  // oscillator -> envelope gain -> trim gain -> sfx bus
  const trim = ctx.gains.find((g) => g.outputs.includes(mixer.input("sfx")));
  assert.ok(trim, "a synth trim gain feeds the sfx bus");
});

test("a synth cue can name another bus", () => {
  const { fake, env, mixer } = boot();
  env.context.__gosx.arcadeAudio.play("confirm", { bus: "ui" });
  const ctx = fake.instances[0];
  assert.ok(ctx.gains.find((g) => g.outputs.includes(mixer.input("ui"))));
});

test("the standalone arcade chunk shares the mixer bootstrap installed", () => {
  const fake = makeFakeContext();
  const env = createContext({ AudioContext: fake.AudioContext });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  delete env.context.__gosx.arcadeAudio;
  runScript(bootstrapFeatureArcadeAudioSource, env.context, "bootstrap-feature-arcade-audio.js");
  env.context.__gosx.arcadeAudio.play("move");
  assert.equal(fake.instances.length, 1);
});
