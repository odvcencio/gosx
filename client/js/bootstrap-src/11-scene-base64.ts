// Base64 decoding for Scene3D binary payloads.
//
// It sat in 11a-scene-decompress.ts until that file became the lazily fetched
// decompress chunk. This helper cannot follow it: 20-scene-mount.js calls
// sceneBase64Decode on every page that carries a motion program, and a motion
// program is not a compressed array. The two features are independent, so the
// eager base chunk keeps the twenty lines that serve both.

  function sceneBase64Decode(str) {
    if (typeof atob === "function") {
      var raw = atob(str);
      var bytes = new Uint8Array(raw.length);
      for (var i = 0; i < raw.length; i++) {
        bytes[i] = raw.charCodeAt(i);
      }
      return bytes;
    }
    // Node.js fallback for tests
    if (typeof Buffer !== "undefined") {
      return new Uint8Array(Buffer.from(str, "base64"));
    }
    return new Uint8Array(0);
  }
