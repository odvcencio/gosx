"use strict";
// Built-in video and audio engines: subtitles, follow sync, HLS transport,
// picture-in-picture, input locking and persisted playback preferences.
//
// Split out of client/js/runtime.test.js. Every shared fake, sandbox builder
// and fixture factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const path = require("node:path");

const {
  bootstrapSource,
  FakeElement,
  createContext,
  installManualTimers,
  runScript,
  flushAsyncWork,
  sharedSignalValue,
  appendManagedHead,
  theatreSyncHeartbeat,
  theatrePing,
  VIDEO_PRIMITIVES_FAKE_HLS_SCRIPT,
  readBootstrapTailSrc,
} = require("./runtime-test-harness.js");

test("bootstrap mounts builtin video engines and bridges shared signals", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-root";
  mount.width = 640;
  mount.height = 360;

  let env;
  env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "PromoVideo",
          kind: "video",
          mountId: "video-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.mp4",
            volume: 0.5,
            subtitleTracks: [{ id: "en", language: "en", title: "English" }],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(env.context.__gosx.engines.size, 1);

  const video = mount.firstChild;
  assert.ok(video);
  assert.equal(video.tagName, "VIDEO");
  assert.equal(video.volume, 0.5);
  assert.ok(video.loadCalls.length >= 1);

  video.duration = 120;
  video.readyState = 4;
  video.currentTime = 10;
  video.buffered = {
    length: 1,
    start() {
      return 0;
    },
    end() {
      return 25;
    },
  };
  video.dispatchEvent({ type: "timeupdate", target: video });
  await flushAsyncWork();

  assert.equal(sharedSignalValue(env, "$video.position"), 10);
  assert.equal(sharedSignalValue(env, "$video.duration"), 120);
  assert.equal(sharedSignalValue(env, "$video.buffered"), 15);
  assert.deepEqual(sharedSignalValue(env, "$video.viewport"), [640, 360]);
  assert.deepEqual(sharedSignalValue(env, "$video.subtitleTracks"), [
    { id: "en", language: "en", srclang: "en", title: "English", kind: "subtitles", src: "", default: false, forced: false },
  ]);

  video.setAttribute("data-gosx-video-duration", "7200");
  video.duration = 120;
  video.dispatchEvent({ type: "durationchange", target: video });
  await flushAsyncWork();
  assert.equal(sharedSignalValue(env, "$video.duration"), 7200);

  video.duration = 7300;
  video.dispatchEvent({ type: "durationchange", target: video });
  await flushAsyncWork();
  assert.equal(sharedSignalValue(env, "$video.duration"), 7300);

  let rectCalls = 0;
  const originalGetBoundingClientRect = mount.getBoundingClientRect.bind(mount);
  mount.getBoundingClientRect = function() {
    rectCalls += 1;
    return originalGetBoundingClientRect();
  };
  const callsAfterFirstOutputUpdate = env.sharedSignalCalls.length;
  video.dispatchEvent({ type: "timeupdate", target: video });
  await flushAsyncWork();
  assert.equal(env.sharedSignalCalls.length, callsAfterFirstOutputUpdate);
  assert.equal(rectCalls, 0);

  mount.width = 800;
  mount.height = 450;
  env.resizeObservers.at(-1).trigger([mount]);
  await flushAsyncWork();
  assert.deepEqual(sharedSignalValue(env, "$video.viewport"), [800, 450]);
  assert.ok(rectCalls >= 1);

  env.context.__gosx_notify_shared_signal("$video.command", JSON.stringify("play"));
  await flushAsyncWork();
  assert.equal(video.playCalls.length, 1);
  assert.equal(video.paused, false);

  env.context.__gosx_notify_shared_signal("$video.seek", JSON.stringify(42));
  await flushAsyncWork();
  assert.equal(video.currentTime, 42);
});

test("bootstrap registers audio manifests and plays clips", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "audio-root";
  const audioGraph = { sources: [], gains: [], stereoPanners: [], panners: [], starts: 0 };
  class FakeAudioNode {
    constructor(kind) {
      this.kind = kind;
      this.connections = [];
    }

    connect(target) {
      this.connections.push(target);
      return target;
    }
  }
  class FakeAudioContext {
    constructor() {
      this.destination = new FakeAudioNode("destination");
    }

    createBufferSource() {
      const source = new FakeAudioNode("source");
      source.playbackRate = { value: 1 };
      source.start = () => {
        audioGraph.starts++;
      };
      source.stop = () => {};
      audioGraph.sources.push(source);
      return source;
    }

    createGain() {
      const gain = new FakeAudioNode("gain");
      gain.gain = { value: 1 };
      audioGraph.gains.push(gain);
      return gain;
    }

    createStereoPanner() {
      const panner = new FakeAudioNode("stereo");
      panner.pan = { value: 0 };
      audioGraph.stereoPanners.push(panner);
      return panner;
    }

    createPanner() {
      const panner = new FakeAudioNode("spatial");
      panner.positionX = { value: 0 };
      panner.positionY = { value: 0 };
      panner.positionZ = { value: 0 };
      audioGraph.panners.push(panner);
      return panner;
    }

    async decodeAudioData(data) {
      return { byteLength: data.byteLength };
    }
  }

  const env = createContext({
    elements: [mount],
    AudioContext: FakeAudioContext,
    fetchRoutes: {
      "/audio/hit.ogg": { bytes: [1, 2, 3, 4] },
    },
    engineFactories: {
      AudioProbe: () => ({}),
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-audio",
          component: "AudioProbe",
          kind: "surface",
          mountId: "audio-root",
          capabilities: ["audio"],
          props: {
            audio: {
              masterVolume: 0.5,
              buses: [{ id: "sfx", volume: 0.8 }],
              clips: [
                { id: "hit", uri: "/audio/hit.ogg", bus: "sfx", volume: 0.75, rate: 1.1 },
              ],
            },
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const audio = env.context.__gosx.audio;
  assert.equal(typeof audio.play, "function");
  let snapshot = audio.snapshot();
  assert.equal(snapshot.clips.length, 1);
  assert.equal(snapshot.clips[0].id, "hit");
  assert.equal(snapshot.clips[0].bus, "sfx");
  assert.ok(snapshot.buses.some((bus) => bus.id === "master" && bus.volume === 0.5));
  assert.ok(snapshot.buses.some((bus) => bus.id === "sfx" && bus.volume === 0.8));

  const handle = await audio.play("hit", {
    handle: "hit-1",
    volume: 0.5,
    position: { x: 2, y: 1.5, z: -4 },
    refDistance: 2,
    maxDistance: 64,
    rolloffFactor: 0.75,
  });
  assert.equal(handle, "hit-1");
  assert.equal(audioGraph.starts, 1);
  assert.ok(Math.abs(audioGraph.gains[0].gain.value - 0.15) < 0.000001);
  assert.equal(audioGraph.stereoPanners.length, 0);
  assert.equal(audioGraph.panners.length, 1);
  assert.equal(audioGraph.panners[0].positionX.value, 2);
  assert.equal(audioGraph.panners[0].positionY.value, 1.5);
  assert.equal(audioGraph.panners[0].positionZ.value, -4);
  assert.equal(audioGraph.panners[0].refDistance, 2);
  assert.equal(audioGraph.panners[0].maxDistance, 64);
  assert.equal(audioGraph.panners[0].rolloffFactor, 0.75);
  snapshot = audio.snapshot();
  assert.deepEqual(snapshot.handles, ["hit-1"]);
  assert.equal(audio.stop("hit"), true);
  assert.deepEqual(audio.snapshot().handles, []);
});

test("bootstrap mounts only the first video engine on a page", async () => {
  const firstMount = new FakeElement("div", null);
  firstMount.id = "video-root-a";
  const secondMount = new FakeElement("div", null);
  secondMount.id = "video-root-b";

  const env = createContext({
    elements: [firstMount, secondMount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "PromoVideoA",
          kind: "video",
          mountId: "video-root-a",
          capabilities: ["video", "fetch", "audio"],
          props: { src: "/media/a.mp4" },
        },
        {
          id: "gosx-engine-1",
          component: "PromoVideoB",
          kind: "video",
          mountId: "video-root-b",
          capabilities: ["video", "fetch", "audio"],
          props: { src: "/media/b.mp4" },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(firstMount.firstChild && firstMount.firstChild.tagName, "VIDEO");
  assert.equal(secondMount.firstChild, null);
  assert.ok(env.consoleLogs.error.some((entry) => entry.includes("only one video engine is supported per page")));
  const issues = env.context.__gosx.listIssues();
  assert.equal(issues.some((issue) => issue.scope === "engine" && issue.source === "gosx-engine-1"), true);
});

test("bootstrap upgrades server-rendered video fallbacks in place and loads explicit subtitle track URLs", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-fallback-root";
  mount.width = 960;
  mount.height = 540;

  const fallback = new FakeElement("video", null);
  fallback.setAttribute("poster", "/media/poster.jpg");
  fallback.setCanPlayType("video/webm", "probably");

  const source = new FakeElement("source", null);
  source.setAttribute("src", "/media/promo.webm");
  source.setAttribute("type", "video/webm");
  fallback.appendChild(source);

  const track = new FakeElement("track", null);
  track.setAttribute("src", "/subs/en-custom.vtt");
  track.setAttribute("kind", "captions");
  track.setAttribute("srclang", "en");
  track.setAttribute("label", "English");
  fallback.appendChild(track);

  mount.appendChild(fallback);

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/subs/en-custom.vtt": {
        text: "WEBVTT\n\n00:00:00.000 --> 00:00:02.000\nHello from track",
      },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "PromoVideo",
          kind: "video",
          mountId: "video-fallback-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            poster: "/media/poster.jpg",
            sources: [
              { src: "/media/promo.webm", type: "video/webm" },
              { src: "/media/promo.mp4", type: "video/mp4" },
            ],
            subtitleTrack: "en",
            subtitleTracks: [
              { id: "en", language: "en", title: "English", kind: "captions", src: "/subs/en-custom.vtt" },
            ],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const video = mount.firstChild;
  assert.equal(video, fallback);
  assert.equal(video.tagName, "VIDEO");
  assert.equal(video.getAttribute("data-gosx-video"), "true");
  assert.equal(video.getAttribute("poster"), "/media/poster.jpg");
  assert.equal(video.getAttribute("src"), null);
  assert.equal(video.children.length, 3);
  assert.equal(video.children[0], source);
  assert.equal(video.children[1], track);
  assert.equal(video.children[2].tagName, "TRACK");
  assert.equal(video.children[2].getAttribute("src"), "/subs/en-custom.vtt");
  assert.ok(video.loadCalls.length >= 1);
  assert.ok(env.fetchCalls.some((call) => call.url === "/subs/en-custom.vtt"));
  assert.deepEqual(sharedSignalValue(env, "$video.subtitleTracks"), [
    {
      id: "en",
      language: "en",
      srclang: "en",
      title: "English",
      kind: "captions",
      src: "/subs/en-custom.vtt",
      default: false,
      forced: false,
    },
  ]);
  assert.equal(sharedSignalValue(env, "$video.subtitleStatus"), "ready");
});

test("bootstrap video engines retry warming subtitle tracks with Retry-After", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-subtitle-root";

  let subtitleFetches = 0;
  let env;
  env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/subs/en.vtt": () => {
        subtitleFetches += 1;
        if (subtitleFetches === 1) {
          return { status: 202, headers: { "Retry-After": "0.5" } };
        }
        return {
          text: "WEBVTT\n\n00:00:00.000 --> 00:00:10.000\nLong span\n\n00:00:01.000 --> 00:00:01.500\nExpired overlap\n\n00:00:03.000 --> 00:00:04.000\nHello after warmup",
        };
      },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "PromoVideo",
          kind: "video",
          mountId: "video-subtitle-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.mp4",
            subtitleTrack: "en",
            subtitleTracks: [
              { id: "en", language: "en", title: "English", kind: "subtitles", src: "/subs/en.vtt" },
            ],
          },
        },
      ],
    },
  });
  const timers = installManualTimers(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(sharedSignalValue(env, "$video.subtitleStatus"), "warming");
  assert.equal(subtitleFetches, 1);

  assert.equal(timers.runDelay(500), 1);
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(subtitleFetches, 2);
  assert.equal(sharedSignalValue(env, "$video.subtitleStatus"), "ready");

  const video = mount.firstChild;
  const overlay = mount.children.find((child) => child.getAttribute("data-gosx-video-subtitles") === "true");
  assert.ok(overlay, "expected managed subtitle overlay");
  const nativeTrack = video.children.find((child) => child.tagName === "TRACK" && child.getAttribute("src") === "/subs/en.vtt");
  assert.ok(nativeTrack, "expected native subtitle mirror");
  assert.equal(nativeTrack.getAttribute("src"), "/subs/en.vtt");
  assert.equal(nativeTrack.mode, "hidden");
  assert.equal(overlay.hasAttribute("hidden"), false);
  assert.equal(overlay.children[0].innerHTML, "Long span");
  env.document.pictureInPictureElement = video;
  video.dispatchEvent({ type: "enterpictureinpicture", target: video });
  assert.equal(nativeTrack.mode, "showing");
  env.document.pictureInPictureElement = null;
  video.dispatchEvent({ type: "leavepictureinpicture", target: video });
  assert.equal(nativeTrack.mode, "hidden");
  video.currentTime = 3.2;
  video.dispatchEvent({ type: "timeupdate", target: video });
  await flushAsyncWork();
  assert.deepEqual(sharedSignalValue(env, "$video.activeCues"), [{ text: "Long span" }, { text: "Hello after warmup" }]);
  assert.equal(overlay.hasAttribute("hidden"), false);
  assert.equal(overlay.children[0].getAttribute("class"), "gosx-video-subtitle-cue subtitle-cue");
  assert.equal(overlay.children[0].innerHTML, "Long span");
  assert.equal(overlay.children[1].innerHTML, "Hello after warmup");
});

test("bootstrap video engines render bitmap WebVTT cues as positioned images", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-bitmap-subtitle-root";

  let env;
  env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/subs/bitmap.vtt": {
        text: "WEBVTT\n\n00:00:02.000 --> 00:00:05.000\n/subs/frame-001.png#xywh=100,720,1720,220&canvas=1920,1080",
      },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "BitmapVideo",
          kind: "video",
          mountId: "video-bitmap-subtitle-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.mp4",
            subtitleTrack: "pgs",
            subtitleTracks: [
              { id: "pgs", language: "eng", title: "English PGS", kind: "metadata", src: "/subs/bitmap.vtt" },
            ],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const video = mount.firstChild;
  const overlay = mount.children.find((child) => child.getAttribute("data-gosx-video-subtitles") === "true");
  assert.ok(overlay, "expected managed subtitle overlay");
  assert.equal(sharedSignalValue(env, "$video.subtitleStatus"), "ready");

  video.currentTime = 2.5;
  video.dispatchEvent({ type: "timeupdate", target: video });
  await flushAsyncWork();

  assert.deepEqual(sharedSignalValue(env, "$video.activeCues"), [
    {
      text: "/subs/frame-001.png#xywh=100,720,1720,220&amp;canvas=1920,1080",
      image: {
        src: "/subs/frame-001.png",
        x: 100,
        y: 720,
        w: 1720,
        h: 220,
        canvasW: 1920,
        canvasH: 1080,
      },
    },
  ]);
  assert.equal(overlay.hasAttribute("hidden"), false);
  assert.equal(overlay.children.length, 1);
  assert.equal(overlay.children[0].tagName, "IMG");
  assert.equal(overlay.children[0].getAttribute("class"), "subtitle-image");
  assert.equal(overlay.children[0].getAttribute("src"), "/subs/frame-001.png");
  assert.equal(overlay.children[0].style.left, "5.208333333333334%");
  assert.equal(overlay.children[0].style.top, "66.66666666666666%");
  assert.equal(overlay.children[0].style.width, "89.58333333333334%");
});

// Memoryless legacy nudge: 0.92 behind / 1.08 ahead, snap past 5s drift. This
// behavior now lives behind syncStrategy "nudge-legacy"; the default "nudge"
// path routes through the parity-locked JS/WASM drift engine.
test("bootstrap video follow sync nudges both ahead and behind without repeated play calls", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-sync-root";
  let socket = null;

  let env;
  env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    createWebSocket(url) {
      socket = {
        url,
        readyState: 1,
        sent: [],
        closeCalls: 0,
        send(payload) {
          this.sent.push(payload);
        },
        close() {
          this.closeCalls += 1;
          this.readyState = 3;
        },
      };
      return socket;
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "SyncedVideo",
          kind: "video",
          mountId: "video-sync-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.mp4",
            sync: "/api/theatre/ROOM01/ws",
            syncMode: "follow",
            syncStrategy: "nudge-legacy",
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.ok(socket);
  const video = mount.firstChild;
  video.paused = false;
  video.ended = false;

  socket.onmessage({ data: JSON.stringify({ type: "sync", position: 10, playing: true, rate: 1, sentAtMS: 0 }) });
  video.currentTime = 12;
  socket.onmessage({ data: JSON.stringify({ type: "sync", position: 10, playing: true, rate: 1, sentAtMS: 0 }) });
  assert.equal(video.playbackRate, 0.92);
  assert.equal(video.playCalls.length, 0);

  video.currentTime = 8;
  socket.onmessage({ data: JSON.stringify({ type: "sync", position: 10, playing: true, rate: 1, sentAtMS: 0 }) });
  assert.equal(video.playbackRate, 1.08);
  assert.equal(video.playCalls.length, 0);
});

// Uses syncStrategy "nudge-legacy" to assert the memoryless 0.92 nudge; the
// binary heartbeat/ping plumbing it also exercises is strategy-independent.
test("bootstrap video follow sync consumes binary heartbeats and answers pings", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-binary-sync-root";
  let socket = null;
  let env;

  env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    createWebSocket(url) {
      socket = {
        url,
        readyState: 1,
        sent: [],
        closeCalls: 0,
        send(payload) {
          this.sent.push(payload);
        },
        close() {
          this.closeCalls += 1;
          this.readyState = 3;
        },
      };
      return socket;
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "SyncedVideo",
          kind: "video",
          mountId: "video-binary-sync-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.mp4",
            sync: "/api/theatre/ROOM01/ws",
            syncMode: "follow",
            syncStrategy: "nudge-legacy",
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.ok(socket);
  assert.equal(socket.binaryType, "arraybuffer");

  const video = mount.firstChild;
  video.paused = false;
  video.ended = false;
  video.currentTime = 22;
  socket.onmessage({ data: theatreSyncHeartbeat({ position: 20, playing: true, viewerCount: 7 }) });
  assert.equal(video.playbackRate, 0.92);
  assert.equal(sharedSignalValue(env, "$video.viewerCount"), 7);

  const ping = theatrePing(1706000000000);
  socket.onmessage({ data: ping });
  assert.equal(socket.sent.length, 1);
  const pong = new Uint8Array(socket.sent[0]);
  assert.equal(pong[0], 0x04);
  assert.deepEqual(Array.from(pong.slice(1)), Array.from(new Uint8Array(ping).slice(1)));
});

test("video sync binary decode parses a 0x04 pong frame into an echoedTimestamp", () => {
  // Extract the module-private decode helpers from the unminified source so we
  // can exercise the new 0x04 case directly without a full DOM/brain mount.
  const videoSource = readBootstrapTailSrc();
  function extractFn(name) {
    const marker = "function " + name + "(";
    const start = videoSource.indexOf(marker);
    assert.notEqual(start, -1, "missing source function " + name);
    let depth = 0;
    let seenBody = false;
    for (let i = start; i < videoSource.length; i += 1) {
      const ch = videoSource[i];
      if (ch === "{") {
        depth += 1;
        seenBody = true;
      } else if (ch === "}") {
        depth -= 1;
        if (seenBody && depth === 0) {
          return videoSource.slice(start, i + 1);
        }
      }
    }
    throw new Error("unterminated source function " + name);
  }

  const factory = new Function(
    extractFn("videoBytesFromRaw") +
      "\n" +
      extractFn("videoReadU32BE") +
      "\n" +
      "function videoReadFloat32BE() { return 0; }\n" +
      "function videoEncodePong() { return null; }\n" +
      extractFn("videoDecodeBinarySyncMessage") +
      "\nreturn videoDecodeBinarySyncMessage;",
  );
  const decode = factory();

  // Craft a 9-byte 0x04 frame carrying a u64 big-endian timestamp.
  const timestamp = 1706000000000;
  const buffer = new ArrayBuffer(9);
  const bytes = new Uint8Array(buffer);
  const view = new DataView(buffer);
  bytes[0] = 0x04;
  view.setUint32(1, Math.floor(timestamp / 4294967296), false);
  view.setUint32(5, timestamp % 4294967296, false);

  const decoded = decode(buffer);
  assert.deepEqual(decoded, { type: "pong", echoedTimestamp: timestamp });

  // A short (sub-9-byte) 0x04 frame is rejected.
  assert.equal(decode(new Uint8Array([0x04, 0x00, 0x00]).buffer), null);
});

test("bootstrap video follow sync honors prepare countdown before server play", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-countdown-root";
  let socket = null;

  let env;
  env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    createWebSocket(url) {
      socket = {
        url,
        readyState: 1,
        sent: [],
        closeCalls: 0,
        send(payload) {
          this.sent.push(payload);
        },
        close() {
          this.closeCalls += 1;
          this.readyState = 3;
        },
      };
      return socket;
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "SyncedVideo",
          kind: "video",
          mountId: "video-countdown-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.mp4",
            sync: "/api/theatre/ROOM01/ws",
            syncMode: "follow",
            syncStrategy: "nudge",
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.ok(socket);
  const video = mount.firstChild;
  const overlay = mount.children.find((child) => child.getAttribute("data-gosx-video-sync-overlay") === "true");
  assert.ok(overlay);

  socket.onmessage({ data: JSON.stringify({ type: "sync_prepare", position: 14, start_at: Date.now() + 3000, countdown_ms: 3000 }) });
  assert.equal(video.currentTime, 14);
  assert.equal(video.paused, true);
  assert.equal(overlay.hasAttribute("hidden"), false);
  assert.match(overlay.textContent, /Starting in/);
  assert.equal(sharedSignalValue(env, "$video.syncPhase"), "countdown");

  socket.onmessage({ data: JSON.stringify({ type: "sync_play", position: 14, rate: 1, sentAtMS: 0 }) });
  assert.equal(video.playCalls.length, 1);
  assert.equal(video.paused, false);
  assert.equal(overlay.hasAttribute("hidden"), true);
});

test("bootstrap video follow sync falls back to muted autoplay", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-muted-autoplay-root";
  let socket = null;

  let env;
  env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    createWebSocket(url) {
      socket = {
        url,
        readyState: 1,
        sent: [],
        closeCalls: 0,
        send(payload) {
          this.sent.push(payload);
        },
        close() {
          this.closeCalls += 1;
          this.readyState = 3;
        },
      };
      return socket;
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "SyncedVideo",
          kind: "video",
          mountId: "video-muted-autoplay-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.mp4",
            sync: "/api/theatre/ROOM01/ws",
            syncMode: "follow",
            syncStrategy: "nudge-legacy",
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.ok(socket);
  const video = mount.firstChild;
  let firstPlay = true;
  video.play = function() {
    this.playCalls.push([]);
    if (firstPlay) {
      firstPlay = false;
      this.paused = true;
      return Promise.reject(new Error("autoplay blocked"));
    }
    this.paused = false;
    return Promise.resolve();
  };

  socket.onmessage({ data: JSON.stringify({ type: "sync_play", position: 14, rate: 1, sentAtMS: 0 }) });
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(video.playCalls.length, 2);
  assert.equal(video.muted, true);
  assert.equal(video.getAttribute("muted"), "true");
  assert.equal(video.paused, false);
  assert.equal(sharedSignalValue(env, "$video.muted"), true);
  assert.equal(sharedSignalValue(env, "$video.error"), "");
});

test("bootstrap video follow sync shows cache progress and blocks local play", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-cache-wait-root";
  let socket = null;

  let env;
  env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    createWebSocket(url) {
      socket = {
        url,
        readyState: 1,
        sent: [],
        closeCalls: 0,
        send(payload) {
          this.sent.push(payload);
        },
        close() {
          this.closeCalls += 1;
          this.readyState = 3;
        },
      };
      return socket;
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "SyncedVideo",
          kind: "video",
          mountId: "video-cache-wait-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.mp4",
            sync: "/api/theatre/ROOM01/ws",
            syncMode: "follow",
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.ok(socket);
  const video = mount.firstChild;
  const overlay = mount.children.find((child) => child.getAttribute("data-gosx-video-sync-overlay") === "true");
  assert.ok(overlay);

  socket.onmessage({
    data: JSON.stringify({
      type: "channel_status",
      state: {
        cache_paused: true,
        transcode_progress: 42,
        transcode_segments_finished: 123,
      },
    }),
  });
  assert.equal(overlay.hasAttribute("hidden"), false);
  assert.match(overlay.textContent, /42%/);
  assert.equal(sharedSignalValue(env, "$video.cacheWaiting"), true);

  video.paused = false;
  video.dispatchEvent({ type: "play", target: video });
  assert.equal(video.paused, true);
  assert.ok(video.pauseCalls.length >= 1);
  assert.equal(sharedSignalValue(env, "$video.syncPhase"), "buffering");
});

test("bootstrap video follow sync treats bare cache waiting attributes as active", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-initial-cache-wait-root";
  const fallback = new FakeElement("video", null);
  fallback.setAttribute("data-gosx-video-cache-waiting", "");
  fallback.setAttribute("data-gosx-video-cache-progress", "37");
  fallback.setAttribute("data-gosx-video-cache-segments", "55");
  mount.appendChild(fallback);
  let socket = null;
  let env;

  env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    createWebSocket(url) {
      socket = {
        url,
        readyState: 1,
        sent: [],
        closeCalls: 0,
        send(payload) {
          this.sent.push(payload);
        },
        close() {
          this.closeCalls += 1;
          this.readyState = 3;
        },
      };
      return socket;
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "SyncedVideo",
          kind: "video",
          mountId: "video-initial-cache-wait-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.mp4",
            sync: "/api/theatre/ROOM01/ws",
            syncMode: "follow",
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.ok(socket);
  const overlay = mount.children.find((child) => child.getAttribute("data-gosx-video-sync-overlay") === "true");
  assert.ok(overlay);
  assert.equal(overlay.hasAttribute("hidden"), false);
  assert.match(overlay.textContent, /37%/);
  assert.equal(sharedSignalValue(env, "$video.cacheWaiting"), true);
});

test("bootstrap video subtitle warmup does not block sync connection", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-subtitle-sync-root";
  let socket = null;
  let subtitleFetches = 0;
  let env;

  env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/subs/en.vtt": () => {
        subtitleFetches += 1;
        return { status: 202, headers: { "Retry-After": "5" } };
      },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    createWebSocket(url) {
      socket = {
        url,
        readyState: 1,
        sent: [],
        closeCalls: 0,
        send(payload) {
          this.sent.push(payload);
        },
        close() {
          this.closeCalls += 1;
          this.readyState = 3;
        },
      };
      return socket;
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "SyncedSubtitleVideo",
          kind: "video",
          mountId: "video-subtitle-sync-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.mp4",
            sync: "/api/theatre/ROOM01/ws",
            syncMode: "follow",
            subtitleTrack: "en",
            subtitleTracks: [
              { id: "en", language: "en", title: "English", kind: "subtitles", src: "/subs/en.vtt" },
            ],
          },
        },
      ],
    },
  });
  installManualTimers(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.ok(socket, "expected websocket to connect before subtitle warmup finishes");
  assert.equal(socket.binaryType, "arraybuffer");
  assert.ok(subtitleFetches >= 1);
  assert.equal(sharedSignalValue(env, "$video.subtitleStatus"), "warming");
});

test("bootstrap video engine owns hover interaction state", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-hover-root";

  let env;
  env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "HoverVideo",
          kind: "video",
          mountId: "video-hover-root",
          capabilities: ["video", "fetch", "audio"],
          props: { src: "/media/promo.mp4" },
        },
      ],
    },
  });
  const timers = installManualTimers(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const video = mount.firstChild;
  assert.equal(mount.getAttribute("data-gosx-video-interaction"), "active");
  assert.equal(video.getAttribute("data-gosx-video-interaction"), "active");

  video.paused = false;
  video.ended = false;
  mount.dispatchEvent({ type: "pointermove", target: mount });
  assert.equal(sharedSignalValue(env, "$video.interaction"), "active");
  assert.equal(timers.runDelay(2400), 1);
  assert.equal(mount.getAttribute("data-gosx-video-interaction"), "idle");
  assert.equal(video.getAttribute("data-gosx-video-interaction"), "idle");

  mount.dispatchEvent({ type: "pointerenter", target: mount });
  assert.equal(mount.getAttribute("data-gosx-video-interaction"), "active");
  assert.equal(sharedSignalValue(env, "$video.interaction"), "active");
});

test("bootstrap video engines load HLS.js from the document runtime contract", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-hls-root";
  mount.width = 1280;
  mount.height = 720;

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/runtime/vendor/hls.min.js": {
        text: `window.__hlsLoads = [];
window.Hls = function FakeHls() {
  this.attachMedia = function(video) { this.video = video; };
  this.loadSource = function(src) { window.__hlsLoads.push(src); };
  this.on = function() {};
  this.destroy = function() {};
};
window.Hls.isSupported = function() { return true; };
window.Hls.Events = {};`,
      },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "PromoVideo",
          kind: "video",
          mountId: "video-hls-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.m3u8",
          },
        },
      ],
    },
  });

  const contract = env.document.createElement("script");
  contract.id = "gosx-document";
  contract.setAttribute("type", "application/json");
  contract.setAttribute("data-gosx-document-contract", "");
  contract.textContent = JSON.stringify({
    version: 1,
    page: {
      id: "gosx-doc-video",
      pattern: "GET /video",
      path: "/video",
      title: "Video",
      status: 200,
    },
    enhancement: {
      bootstrap: true,
      runtime: true,
      navigation: false,
    },
    assets: {
      bootstrapMode: "full",
      manifest: true,
      runtimePath: "/runtime.wasm",
      wasmExecPath: "/wasm_exec.js",
      bootstrapPath: "/bootstrap.js",
      hlsPath: "/runtime/vendor/hls.min.js",
      engines: 1,
    },
  });
  appendManagedHead(env.document, [contract]);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(env.fetchCalls.some((call) => call.url === "/runtime/vendor/hls.min.js"), true);
  assert.deepEqual(Array.from(env.context.__hlsLoads || []), ["/media/promo.m3u8"]);

  const mounted = env.context.__gosx.engines.get("gosx-engine-0");
  assert.ok(mounted);
  assert.equal(mounted.handle.video.tagName, "VIDEO");
  assert.equal(
    mounted.handle.video.children.some((child) => child.tagName === "SOURCE" && String(child.getAttribute("src") || "").endsWith(".m3u8")),
    false,
  );
});

test("bootstrap video engines recover fatal HLS network and media errors", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-hls-recovery-root";
  mount.width = 1280;
  mount.height = 720;

  let env;
  env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/runtime/vendor/hls.min.js": {
        text: `window.__hlsInstances = [];
window.Hls = function FakeHls() {
  this.handlers = {};
  this.startLoadCalls = 0;
  this.recoverMediaErrorCalls = 0;
  this.attachMedia = function(video) { this.video = video; };
  this.loadSource = function(src) { this.src = src; };
  this.on = function(event, handler) { this.handlers[event] = handler; };
  this.startLoad = function() { this.startLoadCalls += 1; };
  this.recoverMediaError = function() { this.recoverMediaErrorCalls += 1; };
  this.destroy = function() {};
  window.__hlsInstances.push(this);
};
window.Hls.isSupported = function() { return true; };
window.Hls.Events = { ERROR: "hlsError" };
window.Hls.ErrorTypes = { NETWORK_ERROR: "networkError", MEDIA_ERROR: "mediaError" };`,
      },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "PromoVideo",
          kind: "video",
          mountId: "video-hls-recovery-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.m3u8",
          },
        },
      ],
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
  });

  const contract = env.document.createElement("script");
  contract.id = "gosx-document";
  contract.setAttribute("type", "application/json");
  contract.setAttribute("data-gosx-document-contract", "");
  contract.textContent = JSON.stringify({
    version: 1,
    page: {
      id: "gosx-doc-video",
      pattern: "GET /video",
      path: "/video",
      title: "Video",
      status: 200,
    },
    enhancement: {
      bootstrap: true,
      runtime: true,
      navigation: false,
    },
    assets: {
      bootstrapMode: "full",
      manifest: true,
      runtimePath: "/runtime.wasm",
      wasmExecPath: "/wasm_exec.js",
      bootstrapPath: "/bootstrap.js",
      hlsPath: "/runtime/vendor/hls.min.js",
      engines: 1,
    },
  });
  appendManagedHead(env.document, [contract]);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const hls = env.context.__hlsInstances[0];
  assert.ok(hls, "expected fake hls instance");
  assert.equal(typeof hls.handlers.hlsError, "function");

  hls.handlers.hlsError(null, { fatal: true, type: "networkError", details: "manifestLoadError" });
  await flushAsyncWork();
  assert.equal(hls.startLoadCalls, 1);
  assert.equal(hls.recoverMediaErrorCalls, 0);
  assert.equal(sharedSignalValue(env, "$video.error"), "manifestLoadError");

  hls.handlers.hlsError(null, { fatal: true, type: "mediaError", details: "bufferAppendError" });
  await flushAsyncWork();
  assert.equal(hls.startLoadCalls, 1);
  assert.equal(hls.recoverMediaErrorCalls, 1);
  assert.equal(sharedSignalValue(env, "$video.error"), "bufferAppendError");

  hls.handlers.hlsError(null, { fatal: true, type: "otherError", details: "levelLoadError" });
  await flushAsyncWork();
  assert.equal(hls.startLoadCalls, 1);
  assert.equal(hls.recoverMediaErrorCalls, 1);
  assert.equal(sharedSignalValue(env, "$video.error"), "levelLoadError");
});

test("bootstrap video engine exposes HLS audio tracks and applies audioTrack selection", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-audio-tracks-root";

  let env;
  env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime/vendor/hls.min.js": { text: VIDEO_PRIMITIVES_FAKE_HLS_SCRIPT },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-0",
          component: "AudioTracksVideo",
          kind: "video",
          mountId: "video-audio-tracks-root",
          capabilities: ["video", "fetch", "audio"],
          props: { src: "/media/promo.m3u8", audioTrack: "1" },
        },
      ],
    },
  });

  const contract = env.document.createElement("script");
  contract.id = "gosx-document";
  contract.setAttribute("type", "application/json");
  contract.setAttribute("data-gosx-document-contract", "");
  contract.textContent = JSON.stringify({
    version: 1,
    page: { id: "gosx-doc-video", pattern: "GET /video", path: "/video", title: "Video", status: 200 },
    enhancement: { bootstrap: true, runtime: true, navigation: false },
    assets: {
      bootstrapMode: "full",
      manifest: true,
      bootstrapPath: "/bootstrap.js",
      hlsPath: "/runtime/vendor/hls.min.js",
      engines: 1,
    },
  });
  appendManagedHead(env.document, [contract]);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const hls = env.context.__hlsInstances[0];
  assert.ok(hls, "expected fake hls instance");
  const tracks = [
    { id: 0, name: "English", lang: "en" },
    { id: 1, name: "Spanish", lang: "es" },
  ];
  hls.audioTracks = tracks;
  hls.handlers.hlsAudioTracksUpdated(null, { audioTracks: tracks });
  await flushAsyncWork();

  // The engine-side subscribeVideoSignal("audioTrack", ...) applies the
  // island's requested track once it's known which index the requested id
  // maps to.
  assert.deepEqual(Array.from(hls.audioTrackSets), [1]);
  assert.deepEqual(sharedSignalValue(env, "$video.audioTracks"), [
    { id: "0", index: 0, label: "English", language: "en", active: false },
    { id: "1", index: 1, label: "Spanish", language: "es", active: true },
  ]);

  env.context.__gosx_notify_shared_signal("$video.audioTrack", JSON.stringify("0"));
  await flushAsyncWork();
  assert.deepEqual(Array.from(hls.audioTrackSets), [1, 0]);
  assert.deepEqual(sharedSignalValue(env, "$video.audioTracks"), [
    { id: "0", index: 0, label: "English", language: "en", active: true },
    { id: "1", index: 1, label: "Spanish", language: "es", active: false },
  ]);
});

test("bootstrap video engine exposes HLS quality levels and switches via nextLevel", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-quality-levels-root";

  let env;
  env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime/vendor/hls.min.js": { text: VIDEO_PRIMITIVES_FAKE_HLS_SCRIPT },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-0",
          component: "QualityLevelsVideo",
          kind: "video",
          mountId: "video-quality-levels-root",
          capabilities: ["video", "fetch", "audio"],
          props: { src: "/media/promo.m3u8" },
        },
      ],
    },
  });

  const contract = env.document.createElement("script");
  contract.id = "gosx-document";
  contract.setAttribute("type", "application/json");
  contract.setAttribute("data-gosx-document-contract", "");
  contract.textContent = JSON.stringify({
    version: 1,
    page: { id: "gosx-doc-video", pattern: "GET /video", path: "/video", title: "Video", status: 200 },
    enhancement: { bootstrap: true, runtime: true, navigation: false },
    assets: {
      bootstrapMode: "full",
      manifest: true,
      bootstrapPath: "/bootstrap.js",
      hlsPath: "/runtime/vendor/hls.min.js",
      engines: 1,
    },
  });
  appendManagedHead(env.document, [contract]);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const hls = env.context.__hlsInstances[0];
  assert.ok(hls, "expected fake hls instance");
  const levels = [
    { height: 480, width: 854, bitrate: 800000, name: "480p" },
    { height: 1080, width: 1920, bitrate: 3000000, name: "1080p" },
  ];
  hls.levels = levels;
  hls.handlers.hlsLevelsUpdated(null, { levels });
  await flushAsyncWork();

  assert.deepEqual(sharedSignalValue(env, "$video.qualityLevels"), [
    { index: 0, height: 480, width: 854, bitrate: 800000, name: "480p", active: false },
    { index: 1, height: 1080, width: 1920, bitrate: 3000000, name: "1080p", active: false },
  ]);
  assert.equal(sharedSignalValue(env, "$video.qualityLevel"), -1);

  env.context.__gosx_notify_shared_signal("$video.qualityLevel", JSON.stringify(1));
  await flushAsyncWork();
  // nextLevel (not currentLevel) must be used so a manual quality switch
  // doesn't force an immediate buffer flush/stall.
  assert.deepEqual(Array.from(hls.nextLevelSets), [1]);

  hls._currentLevel = 1;
  hls.handlers.hlsLevelSwitched(null, { level: 1 });
  await flushAsyncWork();
  assert.equal(sharedSignalValue(env, "$video.qualityLevel"), 1);
  assert.deepEqual(sharedSignalValue(env, "$video.qualityLevels"), [
    { index: 0, height: 480, width: 854, bitrate: 800000, name: "480p", active: false },
    { index: 1, height: 1080, width: 1920, bitrate: 3000000, name: "1080p", active: true },
  ]);
});

test("bootstrap video engine reports seekable range, live state, and live edge lag", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-seekable-root";

  let env;
  env = createContext({
    elements: [mount],
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-0",
          component: "SeekableVideo",
          kind: "video",
          mountId: "video-seekable-root",
          capabilities: ["video", "fetch", "audio"],
          props: { src: "/media/promo.mp4" },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const video = mount.firstChild;
  assert.ok(video);
  video.seekable = {
    length: 1,
    start() { return 5; },
    end() { return 95; },
  };
  video.currentTime = 40;
  video.dispatchEvent({ type: "timeupdate", target: video });
  await flushAsyncWork();

  assert.deepEqual(sharedSignalValue(env, "$video.seekable"), [5, 95]);
  assert.equal(sharedSignalValue(env, "$video.isLive"), false);
  assert.equal(sharedSignalValue(env, "$video.liveEdgeLag"), null);

  video.duration = Infinity;
  video.dispatchEvent({ type: "timeupdate", target: video });
  await flushAsyncWork();

  assert.equal(sharedSignalValue(env, "$video.isLive"), true);
  assert.equal(sharedSignalValue(env, "$video.liveEdgeLag"), 55);
});

test("bootstrap video engine enters and exits picture-in-picture via command signal", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-pip-root";

  let env;
  env = createContext({
    elements: [mount],
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-0",
          component: "PiPVideo",
          kind: "video",
          mountId: "video-pip-root",
          capabilities: ["video", "fetch", "audio"],
          props: { src: "/media/promo.mp4" },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const video = mount.firstChild;
  assert.ok(video);

  env.context.__gosx_notify_shared_signal("$video.command", JSON.stringify("enter-pip"));
  await flushAsyncWork();
  assert.equal(sharedSignalValue(env, "$video.error"), "picture-in-picture unsupported");
  assert.equal(sharedSignalValue(env, "$video.pip"), false);

  env.document.pictureInPictureEnabled = true;
  video.requestPictureInPicture = function() {
    video.requestPictureInPictureCalls = (video.requestPictureInPictureCalls || 0) + 1;
    env.document.pictureInPictureElement = video;
    return Promise.resolve();
  };
  env.document.exitPictureInPicture = function() {
    env.document.pictureInPictureElement = null;
    return Promise.resolve();
  };

  env.context.__gosx_notify_shared_signal("$video.command", JSON.stringify("enter-pip"));
  await flushAsyncWork();
  assert.equal(video.requestPictureInPictureCalls, 1);
  assert.equal(sharedSignalValue(env, "$video.pip"), true);

  env.context.__gosx_notify_shared_signal("$video.command", JSON.stringify("toggle-pip"));
  await flushAsyncWork();
  assert.equal(env.document.pictureInPictureElement, null);
  assert.equal(sharedSignalValue(env, "$video.pip"), false);
});

test("bootstrap video engine lockInput swallows click/keyboard transport interaction and hides native controls", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-lock-root";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-0",
          component: "LockedVideo",
          kind: "video",
          mountId: "video-lock-root",
          capabilities: ["video", "fetch", "audio"],
          props: { src: "/media/promo.mp4", controls: true, lockInput: true },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const video = mount.firstChild;
  assert.ok(video);
  assert.equal(video.hasAttribute("controls"), false);
  assert.equal(sharedSignalValue(env, "$video.inputLocked"), true);

  let clickPrevented = false;
  let clickStopped = false;
  video.dispatchEvent({
    type: "click",
    target: video,
    preventDefault() { clickPrevented = true; },
    stopPropagation() { clickStopped = true; },
  });
  assert.equal(clickPrevented, true);
  assert.equal(clickStopped, true);

  let spacePrevented = false;
  video.dispatchEvent({
    type: "keydown",
    target: video,
    key: " ",
    preventDefault() { spacePrevented = true; },
    stopPropagation() {},
  });
  assert.equal(spacePrevented, true);

  let letterPrevented = false;
  video.dispatchEvent({
    type: "keydown",
    target: video,
    key: "a",
    preventDefault() { letterPrevented = true; },
    stopPropagation() {},
  });
  assert.equal(letterPrevented, false);
});

test("bootstrap video engine persists and restores playback preferences when persistPrefs is set", async () => {
  class FakeLocalStorage {
    constructor() {
      this.map = new Map();
    }

    getItem(key) {
      return this.map.has(key) ? this.map.get(key) : null;
    }

    setItem(key, value) {
      this.map.set(key, String(value));
    }

    removeItem(key) {
      this.map.delete(key);
    }
  }

  const storage = new FakeLocalStorage();
  const mountA = new FakeElement("div", null);
  mountA.id = "video-persist-root-a";

  let envA;
  envA = createContext({
    elements: [mountA],
    onSetSharedSignal(name, payload) {
      if (envA && typeof envA.context.__gosx_notify_shared_signal === "function") {
        envA.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-persist",
          component: "PersistVideoA",
          kind: "video",
          mountId: "video-persist-root-a",
          capabilities: ["video", "fetch", "audio"],
          props: { src: "/media/promo.mp4", persistPrefs: true },
        },
      ],
    },
  });
  envA.context.localStorage = storage;

  runScript(bootstrapSource, envA.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  envA.context.__gosx_notify_shared_signal("$video.volume", JSON.stringify(0.3));
  await flushAsyncWork();
  envA.context.__gosx_notify_shared_signal("$video.mute", JSON.stringify(true));
  await flushAsyncWork();

  const stored = JSON.parse(storage.getItem("gosx:video:gosx-engine-persist:prefs"));
  assert.equal(stored.volume, 0.3);
  assert.equal(stored.mute, true);

  // A fresh mount (simulating a page reload) with the same persistKey
  // namespace (defaulted from the engine id) restores the saved prefs
  // through the normal $video.volume / $video.mute signal path.
  const mountB = new FakeElement("div", null);
  mountB.id = "video-persist-root-b";

  const envB = createContext({
    elements: [mountB],
    manifest: {
      engines: [
        {
          id: "gosx-engine-persist",
          component: "PersistVideoB",
          kind: "video",
          mountId: "video-persist-root-b",
          capabilities: ["video", "fetch", "audio"],
          props: { src: "/media/promo.mp4", persistPrefs: true },
        },
      ],
    },
  });
  envB.context.localStorage = storage;

  runScript(bootstrapSource, envB.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const videoB = mountB.firstChild;
  assert.ok(videoB);
  assert.equal(videoB.volume, 0.3);
  assert.equal(sharedSignalValue(envB, "$video.muted"), true);
});
