// data-texture.ts — host-written Selena data textures.
//
// A Selena material declares `param spectrum : texture2d format r16float` and
// reads it with load(spectrum, x, y). A Go/WASM engine owns the texels: it
// creates a texture under a key and writes rectangles into it through the
// binary GSXT frame (scene.DataTextureFrame in scene/data_texture.go). A
// material binds the texture with the uniform value "gosx:data:<key>".
//
// This chunk (bootstrap-feature-scene3d-data-texture.js) is advertised only
// when a scene's Selena material reads a data texture, and the mount handle's
// applyDataTexture / preloadDataTexture load it on first use. It holds:
//   1. The frame decoder and the store: one CPU copy per key, so a lost device
//      or context, or a second renderer, rebuilds the texture without help
//      from the producer. Keys are page-wide.
//   2. The WebGPU upload: createTexture in the matching format and
//      queue.writeTexture for the changed rectangle only.
//   3. The WebGL2 upload: texImage2D allocation and texSubImage2D for the
//      changed rectangle, NEAREST filtering, CLAMP_TO_EDGE.
// The renderers reach it only through window.__gosx_scene3d_data_texture_api.
(function() {
  "use strict";
  if (typeof window === "undefined" || window.__gosx_scene3d_data_texture_api) return;

  var MAX_SIZE = 8192, MAX_BYTES = 256 * 1024 * 1024, HEADER = 44, LOG_LIMIT = 32;
  var REF_PREFIX = "gosx:data:";
  // Index = the format byte in the frame. gl names resolve against a WebGL2 context.
  var FORMATS = [
    { name: "rgba8unorm", bytes: 4, view: Uint8Array, gl: ["RGBA8", "RGBA", "UNSIGNED_BYTE"] },
    { name: "rgba16float", bytes: 8, view: Uint16Array, gl: ["RGBA16F", "RGBA", "HALF_FLOAT"] },
    { name: "rgba32float", bytes: 16, view: Float32Array, gl: ["RGBA32F", "RGBA", "FLOAT"] },
    { name: "r16float", bytes: 2, view: Uint16Array, gl: ["R16F", "RED", "HALF_FLOAT"] },
    { name: "r32float", bytes: 4, view: Float32Array, gl: ["R32F", "RED", "FLOAT"] },
  ];

  var textures = new Map();   // key -> record
  var revisions = new Map();  // key -> last applied revision; survives delete

  // --- failures: console.error once per cause, plus a window event ---

  var reported = Object.create(null);
  function fail(key, reason, mount) {
    var id = key + "|" + reason;
    if (!reported[id]) {
      reported[id] = true;
      try { console.error("[gosx] scene3d data texture " + (key ? '"' + key + '": ' : "") + reason); } catch (_e) {}
      var detail = { key: key || "", reason: reason };
      var event = typeof CustomEvent === "function" ? new CustomEvent("gosx:scene3d:data-texture-error", { detail: detail, bubbles: true }) : { type: "gosx:scene3d:data-texture-error", detail: detail };
      var target = mount && typeof mount.dispatchEvent === "function" ? mount : window;
      if (typeof target.dispatchEvent === "function") target.dispatchEvent(event);
    }
    return { applied: false, reason: reason };
  }

  // --- decode ---

  // parse returns the frame, or a string naming why the bytes are not one.
  function parse(bytes) {
    var u8 = bytes instanceof Uint8Array ? bytes : bytes && bytes.buffer instanceof ArrayBuffer ? new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength) : bytes instanceof ArrayBuffer ? new Uint8Array(bytes) : null;
    if (!u8 || u8.length < HEADER || u8[0] !== 0x47 || u8[1] !== 0x53 || u8[2] !== 0x58 || u8[3] !== 0x54) return "not a GSXT data texture frame";
    if (u8[4] !== 1) return "unsupported frame version " + u8[4];
    var view = new DataView(u8.buffer, u8.byteOffset, u8.byteLength);
    var keyLen = view.getUint16(40, true), payload = (HEADER + keyLen + 3) & ~3;
    if (u8.length < payload) return "frame is shorter than its key";
    var frame = {
      op: u8[5], format: u8[6], revision: view.getUint32(8, true) + view.getUint32(12, true) * 4294967296,
      width: view.getUint32(16, true), height: view.getUint32(20, true), x: view.getUint32(24, true), y: view.getUint32(28, true),
      w: view.getUint32(32, true), h: view.getUint32(36, true), key: "", texels: u8.subarray(payload),
    };
    try { frame.key = new TextDecoder("utf-8", { fatal: true }).decode(u8.subarray(HEADER, HEADER + keyLen)); } catch (_e) { return "key is not valid UTF-8"; }
    if (!frame.key) return "empty key";
    if (frame.op > 2) return "unknown op " + frame.op;
    if (!(frame.revision > 0)) return "revision must be positive";
    if (frame.op === 2) return frame;
    var f = FORMATS[frame.format];
    if (!f) return "unknown format " + frame.format;
    if (frame.width < 1 || frame.height < 1 || frame.width > MAX_SIZE || frame.height > MAX_SIZE || frame.width * frame.height * f.bytes > MAX_BYTES) return "size " + frame.width + "x" + frame.height + " " + f.name + " is outside the supported range";
    if (frame.op === 1) return frame;
    if (frame.w < 1 || frame.h < 1 || frame.x + frame.w > frame.width || frame.y + frame.h > frame.height) return "write rectangle " + frame.w + "x" + frame.h + " at (" + frame.x + "," + frame.y + ") falls outside the " + frame.width + "x" + frame.height + " texture";
    if (frame.texels.length !== frame.w * frame.h * f.bytes) return "write of " + frame.w + "x" + frame.h + " " + f.name + " needs " + frame.w * frame.h * f.bytes + " bytes, got " + frame.texels.length;
    return frame;
  }

  // --- store ---

  function destroy(record) {
    record.dead = true;
    for (var i = 0; i < record.live.length; i++) {
      try { record.live[i].destroy(); } catch (_e) {}
    }
    record.live.length = 0;
  }

  function create(frame) {
    var f = FORMATS[frame.format], buffer = new ArrayBuffer(frame.width * frame.height * f.bytes);
    return {
      key: frame.key, width: frame.width, height: frame.height, format: frame.format, bytes: f.bytes,
      buffer: buffer, u8: new Uint8Array(buffer), typed: new f.view(buffer),
      seq: 0, floor: 0, log: [], targets: new WeakMap(), live: [], dead: false,
    };
  }

  function write(record, frame) {
    var rowBytes = frame.w * record.bytes, stride = record.width * record.bytes;
    if (frame.w === record.width) {
      record.u8.set(frame.texels, frame.y * stride);
    } else {
      for (var row = 0; row < frame.h; row++) record.u8.set(frame.texels.subarray(row * rowBytes, (row + 1) * rowBytes), (frame.y + row) * stride + frame.x * record.bytes);
    }
    record.seq++;
    record.log.push({ seq: record.seq, x: frame.x, y: frame.y, w: frame.w, h: frame.h });
    if (record.log.length > LOG_LIMIT) record.floor = record.log.shift().seq;
  }

  // dirty returns the rectangle a target at sequence `since` must upload, or
  // null if it is current. A target older than the trimmed log gets everything.
  function dirty(record, since) {
    if (since === record.seq) return null;
    if (since < record.floor) return { x: 0, y: 0, w: record.width, h: record.height };
    var x0 = record.width, y0 = record.height, x1 = 0, y1 = 0;
    for (var i = 0; i < record.log.length; i++) {
      var e = record.log[i];
      if (e.seq <= since) continue;
      x0 = Math.min(x0, e.x); y0 = Math.min(y0, e.y); x1 = Math.max(x1, e.x + e.w); y1 = Math.max(y1, e.y + e.h);
    }
    return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 };
  }

  // apply decodes one frame into the store. It returns {applied, ...} and never
  // throws; a bad frame fails visibly through fail().
  function apply(bytes, scheduleRender, mount) {
    var frame = parse(bytes);
    if (typeof frame === "string") return fail("", frame, mount);
    var key = frame.key, last = revisions.get(key) || 0;
    if (frame.revision <= last) return { applied: false, reason: "stale-revision" };
    var record = textures.get(key);
    if (frame.op === 2) {
      if (record) { destroy(record); textures.delete(key); }
    } else {
      var same = record && record.width === frame.width && record.height === frame.height && record.format === frame.format;
      if (frame.op === 0 && record && !same) return fail(key, "write describes a " + frame.width + "x" + frame.height + " " + FORMATS[frame.format].name + " texture, but the texture is " + record.width + "x" + record.height + " " + FORMATS[record.format].name + "; create it again to resize", mount);
      if (!record || frame.op === 1) {
        if (record) destroy(record);
        record = create(frame);
        textures.set(key, record);
      }
      if (frame.op === 0) write(record, frame);
    }
    revisions.set(key, frame.revision);
    if (typeof scheduleRender === "function") scheduleRender("data-texture");
    return { applied: true, revision: frame.revision };
  }

  // --- material references ---

  // refKey returns the data texture key a material binds to a texture
  // descriptor, or null when the value is not a "gosx:data:" reference.
  function refKey(material, tex) {
    var values = material && material.customUniforms, value = values && tex && tex.name ? values[tex.name] : undefined;
    if (value && typeof value === "object") value = value.resource || value.ref || value.sceneResource;
    value = typeof value === "string" ? value.trim() : "";
    return value.indexOf(REF_PREFIX) === 0 ? value.slice(REF_PREFIX.length) : null;
  }

  // A descriptor with no format means the host default, rgba8unorm.
  function lookup(key, tex) {
    var record = textures.get(key);
    if (!record) return null;
    var want = tex.format || "rgba8unorm";
    if (FORMATS[record.format].name !== want) {
      fail(key, "texture is " + FORMATS[record.format].name + " but the material declares " + want, null);
      return null;
    }
    return record;
  }

  // --- WebGPU ---

  function webgpuView(device, material, tex) {
    var key = refKey(material, tex);
    var record = key === null ? null : lookup(key, tex);
    if (!record) return null;
    var entry = record.targets.get(device), f = FORMATS[record.format];
    if (!entry) {
      var max = device.limits && device.limits.maxTextureDimension2D;
      if (max && (record.width > max || record.height > max)) { fail(key, record.width + "x" + record.height + " exceeds this device's maxTextureDimension2D " + max, null); return null; }
      var texture = device.createTexture({ label: "gosx-data-texture:" + key, size: [record.width, record.height], format: f.name, usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST });
      entry = { texture: texture, view: texture.createView(), seq: 0, destroy: function() { texture.destroy(); } };
      record.targets.set(device, entry);
      record.live.push(entry);
    }
    var box = dirty(record, entry.seq);
    if (box) {
      var stride = record.width * record.bytes;
      device.queue.writeTexture({ texture: entry.texture, origin: [box.x, box.y] }, record.u8, { offset: box.y * stride + box.x * record.bytes, bytesPerRow: stride, rowsPerImage: record.height }, [box.w, box.h]);
      entry.seq = record.seq;
    }
    return entry.view;
  }

  // --- WebGL2 ---

  function glEntry(gl, record, key) {
    var entry = record.targets.get(gl), f = FORMATS[record.format];
    if (entry && gl.isTexture && !gl.isTexture(entry.texture)) entry = null; // context was lost and restored
    if (!entry) {
      if (typeof gl.texSubImage2D !== "function" || gl[f.gl[0]] === undefined) { fail(key, f.name + " needs a WebGL2 context", null); return null; }
      var max = gl.getParameter(gl.MAX_TEXTURE_SIZE);
      if (record.width > max || record.height > max) { fail(key, record.width + "x" + record.height + " exceeds this context's MAX_TEXTURE_SIZE " + max, null); return null; }
      var texture = gl.createTexture();
      gl.bindTexture(gl.TEXTURE_2D, texture);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
      gl.pixelStorei(gl.UNPACK_ALIGNMENT, 1);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl[f.gl[0]], record.width, record.height, 0, gl[f.gl[1]], gl[f.gl[2]], null);
      entry = { texture: texture, seq: 0, destroy: function() { gl.deleteTexture(texture); } };
      record.targets.set(gl, entry);
      record.live.push(entry);
    }
    var box = dirty(record, entry.seq);
    if (box) {
      gl.bindTexture(gl.TEXTURE_2D, entry.texture);
      gl.pixelStorei(gl.UNPACK_ALIGNMENT, 1);
      gl.pixelStorei(gl.UNPACK_ROW_LENGTH, record.width);
      gl.pixelStorei(gl.UNPACK_SKIP_PIXELS, box.x);
      gl.pixelStorei(gl.UNPACK_SKIP_ROWS, box.y);
      gl.texSubImage2D(gl.TEXTURE_2D, 0, box.x, box.y, box.w, box.h, gl[f.gl[1]], gl[f.gl[2]], record.typed);
      gl.pixelStorei(gl.UNPACK_ROW_LENGTH, 0);
      gl.pixelStorei(gl.UNPACK_SKIP_PIXELS, 0);
      gl.pixelStorei(gl.UNPACK_SKIP_ROWS, 0);
      gl.pixelStorei(gl.UNPACK_ALIGNMENT, 4);
      entry.seq = record.seq;
    }
    return entry;
  }

  // webglBind binds the texture a material names for descriptor `tex` to
  // `unit`, or `placeholder` while it is unwritten or unusable, and points the
  // sampler uniform `loc` at the unit. It also sets the `_texSize_<name>` vec2
  // that requires.glTexSizeUniforms lists (GLSL ES 1.00, which has no
  // textureSize); WebGL2's GLES 3.00 target reads the size off the texture and
  // has no such uniform. It returns false when the material does not name a
  // data texture here, so the caller binds the texture the ordinary way.
  function webglBind(gl, info, unit, loc, material, tex, placeholder) {
    var key = refKey(material, tex);
    if (key === null) return false;
    // Select the unit first: creating or updating the texture binds it, and that
    // must not replace whatever another unit holds.
    gl.activeTexture(gl.TEXTURE0 + unit);
    var record = lookup(key, tex), entry = record ? glEntry(gl, record, key) : null;
    gl.bindTexture(gl.TEXTURE_2D, entry ? entry.texture : placeholder);
    gl.uniform1i(loc, unit);
    var listed = info && info.layout && info.layout.requires && info.layout.requires.glTexSizeUniforms, name = "_texSize_" + tex.name;
    if (listed && listed.indexOf(name) >= 0) {
      var sizeLoc = gl.getUniformLocation(info.program, name);
      if (sizeLoc) gl.uniform2f(sizeLoc, entry ? record.width : 1, entry ? record.height : 1);
    }
    return true;
  }

  window.__gosx_scene3d_data_texture_api = {
    apply: apply,
    parse: parse,
    webgpuView: webgpuView,
    webglBind: webglBind,
    // info reports a stored texture, or null.
    info: function(key) {
      var r = textures.get(key);
      return r ? { key: key, width: r.width, height: r.height, format: FORMATS[r.format].name, seq: r.seq } : null;
    },
  };
})();
