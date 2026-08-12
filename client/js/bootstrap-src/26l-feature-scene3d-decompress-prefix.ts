// GoSX Scene3D decompress sub-feature — fetched on demand.
//
// Two files ride here:
//
//   1. 11a-scene-decompress.ts — the quantized-array decoder, the progressive
//      and level-of-detail ladders, and the index unpacker.
//   2. 11b-scene-points-generate.ts — the procedural point generators, with
//      the bit-exact sin, log, exp and pow the Go side uses so a generated
//      cloud matches the server-rendered oracle.
//
// The two call each other: sceneDecompressProps expands a generator descriptor
// before it decompresses, and a generated layer can still arrive compressed.
// They therefore share one chunk.
//
// A scene with plain float arrays and no generator runs neither. Together they
// cost every Scene3D page 8_514 raw / 3_164 gzip / 2_602 brotli minified
// bytes. The mount fetches the chunk before it builds the scene state, and
// only when the props carry a compressed array, a generator descriptor or a
// compression policy. See sceneNeedsDecompressFeature in
// 20b-scene-mount-webgl-chunk.js.

(function() {
  "use strict";

  // Bail out when the base scene3d chunk did not run. Every bridge below
  // resolves through its API object, so none of them can work alone.
  if (!window.__gosx_scene3d_api) {
    console.warn("[gosx] scene3d-decompress chunk loaded without main scene3d bundle");
    return;
  }

  var sceneApi = window.__gosx_scene3d_api;

  // --- Scalars (10-runtime-primitives.ts, 11-scene-math.ts).
  var sceneNumber = sceneApi.sceneNumber || function(value, fallback) {
    var num = Number(value);
    return Number.isFinite(num) ? num : fallback;
  };
  // --- Props reader (10-runtime-scene-core.ts). sceneDecompressProps walks
  // either props.scene or the flat props shape through it.
  var sceneProps = sceneApi.sceneProps || function(props) {
    return props && props.scene && typeof props.scene === "object" ? props.scene : null;
  };
  // --- Base64 (11-scene-base64.ts). That file stayed in the base chunk
  // because the motion-program loader calls it on pages with no compression.
  var sceneBase64Decode = sceneApi.sceneBase64Decode || function() {
    return new Uint8Array(0);
  };

  // --- files 11a and 11b are concatenated next ---
