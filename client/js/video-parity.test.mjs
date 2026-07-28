import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

function loadBench() {
  const src = fs.readFileSync(path.join(__dirname, "bootstrap.js"), "utf8");
  const listeners = [];
  const document = {
    readyState: "loading",
    body: null,
    documentElement: null,
    addEventListener(type, listener) {
      listeners.push({ type, listener });
    },
    dispatchEvent() {},
  };
  const window = {
    __gosx_bench_exports: true,
    __gosx: {
      islands: new Map(),
      engines: new Map(),
      hubs: new Map(),
      arcadeAudio: {},
    },
    location: { href: "https://example.test/watch", protocol: "https:", host: "example.test" },
    addEventListener() {},
  };
  const context = {
    window,
    document,
    console,
    setTimeout,
    clearTimeout,
    setInterval,
    clearInterval,
    URL,
    Map,
    Set,
    ArrayBuffer,
    Uint8Array,
    DataView,
    performance: { now: () => 0 },
  };
  context.globalThis = context;
  vm.createContext(context);
  vm.runInContext(src, context);
  assert.equal(listeners.some((entry) => entry.type === "DOMContentLoaded"), true);
  return context.window.__gosx_bench;
}

function cueTexts(cues) {
  return Array.from(cues, (cue) => cue.text);
}

function plain(value) {
  return JSON.parse(JSON.stringify(value));
}

test("video subtitles bridge short gaps without crossing the next cue", () => {
  const bench = loadBench();
  const cues = bench.parseVideoVTT(`WEBVTT

00:00:01.000 --> 00:00:02.000
hello

00:00:02.400 --> 00:00:03.000
there
`);

  assert.deepEqual(cueTexts(bench.videoActiveCues(cues, 2.2, { gapBridgeMS: 700 })), ["hello"]);
  assert.deepEqual(cueTexts(bench.videoActiveCues(cues, 2.4, { gapBridgeMS: 700 })), ["there"]);
  assert.deepEqual(cueTexts(bench.videoActiveCues(cues, 2.2, { gapBridgeMS: 100 })), []);
});

test("video subtitles parse bitmap cues and timing options", () => {
  const bench = loadBench();
  const cues = bench.parseVideoVTT(`WEBVTT

00:00:10.000 --> 00:00:11.000
sprite.png#xywh=10,20,200,80&canvas=1920,1080
`);

  assert.equal(cues[0].image.src, "sprite.png");
  assert.equal(cues[0].image.w, 200);
  assert.equal(bench.videoActiveCues(cues, 9.9, { offsetMS: 50, paintLeadMS: 50 }).length, 1);
  assert.equal(bench.videoSubtitleOptions({ subtitles: { scale: "L", style: "boxed", gapBridgeMs: 700 } }).scale, "l");
  assert.equal(bench.videoAudioSourceOptions({ audioSource: { queryParam: "audio" } }).queryParam, "audio");
});

test("video subtitle track normalization omits empty auth keys", () => {
  const bench = loadBench();

  assert.deepEqual(plain(bench.videoNormalizeTrackInfo({ id: "en", authKey: "   " }, 0)), {
    id: "en",
    language: "",
    srclang: "",
    title: "Track 1",
    kind: "subtitles",
    src: "",
    default: false,
    forced: false,
  });
  assert.equal(bench.videoNormalizeTrackInfo({ id: "en", authKey: " english-main " }, 0).authKey, "english-main");
});

test("video subtitle refresh payloads prefer auth key while selection keeps id", () => {
  const bench = loadBench();
  const props = { subtitleBase: "/secure/subs" };
  const track = { id: "en", authKey: "english-main" };

  assert.equal(bench.videoStableTrackIdentity(track, props), "en");
  assert.deepEqual(plain(bench.videoSubtitleRefreshPayload(track, props, "video-0")), {
    track: "english-main",
    src: "/secure/subs/en.vtt",
    engineID: "video-0",
  });
  assert.deepEqual(plain(bench.videoSubtitleRefreshPayload(track, props)), {
    track: "english-main",
    src: "/secure/subs/en.vtt",
  });
});
