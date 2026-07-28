// 19a-scene-ktx2 — the KTX2 container reader and the block-texture uploader.
//
// Why this file exists
//
// The asset pipeline writes block-compressed KTX2 textures and no browser code
// read them. Against rgba8unorm a BC7 base colour map costs a quarter of the
// GPU memory, a BC5 tangent-space normal map costs a quarter, and a BC4 mask
// costs an eighth. This reader is the last step between that build output and
// the saving.
//
// The file carries no transcoder, and that is the point. A build-time tool
// cannot know the device, so toktx and glTF Transform must ship a universal
// format plus a Basis decoder of about 200 KB. GoSX writes one native file per
// block family and the client picks the family the device proved it has, so the
// transcoder cost here is zero.
//
// Scope
//
// Native block-compressed material textures plus the two uncompressed
// half-float products written by assetpipe/ibl. The latter cannot go through an
// image element: they carry cube faces, a roughness mip chain and linear HDR
// values that an image decoder would flatten or tone map.
//
// Chunks: bootstrap.js and bootstrap-feature-scene3d-gltf.js. It must load
// before 19-scene-gltf.js, which reads sceneKTX2UploadPathReady.

  var KTX2_IDENTIFIER = [0xAB, 0x4B, 0x54, 0x58, 0x20, 0x32, 0x30, 0xBB, 0x0D, 0x0A, 0x1A, 0x0A];

  // Supercompression scheme numbers, KTX2 specification section 3.1.
  var KTX2_SCHEME_NONE = 0;
  var KTX2_SCHEME_BASISLZ = 1;
  var KTX2_SCHEME_ZSTD = 2;
  var KTX2_SCHEME_ZLIB = 3;

  // SCENE_KTX2_FORMATS maps one VkFormat onto its upload description:
  //   0 the WebGPU GPUTextureFormat name
  //   1 the WebGL2 compressed internal format
  //   2 the texel block width
  //   3 the texel block height
  //   4 the bytes one texel block holds
  //   5 the WebGL2 extension that turns the internal format on ("" for core)
  //   6 whether the format is block compressed
  //   7 the WebGL2 external format for uncompressed payloads
  //   8 the WebGL2 element type for uncompressed payloads
  //
  // The VkFormat numbers come from render/bundle/ktx2/ktx2.go. The WebGPU names
  // come from ktx2FormatToGPU in render/bundle/ktx2_loader.go composed with
  // encodeTextureFormat in render/gpu/jsgpu/encode.go. The WebGL2 numbers come
  // from the Khronos glext.h header. Read those three before you change a value.
  // A wrong number here uploads a texture as the wrong format, or not at all.
  //
  // BC1 without alpha and BC1 with one alpha bit share one WebGPU name, because
  // WebGPU has no opaque BC1 format and an opaque payload decodes the same under
  // either. WebGL2 keeps the two apart, so those rows differ in column 1.
  //
  // ASTC and ETC2 have no row. No GoSX build step writes those payloads, so a
  // row for them would describe a file that never exists.
  var SCENE_KTX2_FORMATS = {
    // VK_FORMAT_R16G16_SFLOAT / VK_FORMAT_R16G16B16A16_SFLOAT. WebGL enum
    // values are RG16F/RG and RGBA16F/RGBA; HALF_FLOAT is 0x140B.
    83:  ["rg16float", 0x822F, 1, 1, 4, "OES_texture_float_linear", false, 0x8227, 0x140B],
    97:  ["rgba16float", 0x881A, 1, 1, 8, "OES_texture_float_linear", false, 0x1908, 0x140B],
    131: ["bc1-rgba-unorm", 0x83F0, 4, 4, 8, "WEBGL_compressed_texture_s3tc", true, 0, 0],
    132: ["bc1-rgba-unorm-srgb", 0x8C4C, 4, 4, 8, "WEBGL_compressed_texture_s3tc_srgb", true, 0, 0],
    133: ["bc1-rgba-unorm", 0x83F1, 4, 4, 8, "WEBGL_compressed_texture_s3tc", true, 0, 0],
    134: ["bc1-rgba-unorm-srgb", 0x8C4D, 4, 4, 8, "WEBGL_compressed_texture_s3tc_srgb", true, 0, 0],
    137: ["bc3-rgba-unorm", 0x83F3, 4, 4, 16, "WEBGL_compressed_texture_s3tc", true, 0, 0],
    138: ["bc3-rgba-unorm-srgb", 0x8C4F, 4, 4, 16, "WEBGL_compressed_texture_s3tc_srgb", true, 0, 0],
    139: ["bc4-r-unorm", 0x8DBB, 4, 4, 8, "EXT_texture_compression_rgtc", true, 0, 0],
    141: ["bc5-rg-unorm", 0x8DBD, 4, 4, 16, "EXT_texture_compression_rgtc", true, 0, 0],
    145: ["bc7-rgba-unorm", 0x8E8C, 4, 4, 16, "EXT_texture_compression_bptc", true, 0, 0],
    146: ["bc7-rgba-unorm-srgb", 0x8E8D, 4, 4, 16, "EXT_texture_compression_bptc", true, 0, 0],
  };

  // sceneKTX2Error names the failure, so a caller branches on a code instead of
  // matching a message that a later edit may reword.
  function sceneKTX2Error(code, message) {
    var error = new Error("ktx2: " + message);
    error.code = code;
    return error;
  }

  function sceneKTX2Bytes(source) {
    if (source instanceof Uint8Array) {
      return source;
    }
    if (typeof ArrayBuffer !== "undefined" && source instanceof ArrayBuffer) {
      return new Uint8Array(source);
    }
    if (source && source.buffer && typeof source.byteLength === "number") {
      return new Uint8Array(source.buffer, source.byteOffset, source.byteLength);
    }
    throw sceneKTX2Error("source", "want an ArrayBuffer or a typed array");
  }

  // sceneKTX2FormatInfo returns the upload description of one VkFormat.
  function sceneKTX2FormatInfo(vkFormat) {
    var row = SCENE_KTX2_FORMATS[vkFormat];
    if (!row) {
      throw sceneKTX2Error("format", "vkFormat " + vkFormat + " has no block upload path");
    }
    return {
      webgpuFormat: row[0],
      webglInternalFormat: row[1],
      blockWidth: row[2],
      blockHeight: row[3],
      bytesPerBlock: row[4],
      webglExtension: row[5],
      compressed: row[6],
      webglFormat: row[7],
      webglType: row[8],
    };
  }

  // sceneKTX2Parse reads the 12-byte identifier, the 68-byte header and the
  // 24-byte-per-level index, then slices each level.
  //
  // It decompresses nothing, so it stays synchronous. Call sceneKTX2Decode for
  // an image you intend to upload.
  function sceneKTX2Parse(source) {
    var bytes = sceneKTX2Bytes(source);
    if (bytes.byteLength < 80) {
      throw sceneKTX2Error("truncated", "header wants 80 bytes, got " + bytes.byteLength);
    }
    for (var i = 0; i < 12; i++) {
      if (bytes[i] !== KTX2_IDENTIFIER[i]) {
        throw sceneKTX2Error("identifier", "not a KTX2 file");
      }
    }
    var view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    function u32(offset) {
      return view.getUint32(offset, true);
    }
    // A level offset never exceeds 2^53 bytes, so a Number keeps every bit that
    // matters and the reader avoids BigInt arithmetic on the load path.
    function u64(offset) {
      return view.getUint32(offset, true) + view.getUint32(offset + 4, true) * 4294967296;
    }
    var image = {
      vkFormat: u32(12),
      width: u32(20),
      height: u32(24),
      // KTX2 writes 0 for a plain 2D texture and for a single layer or face.
      depth: u32(28) || 1,
      layers: u32(32) || 1,
      faces: u32(36) || 1,
      levelCount: u32(40) || 1,
      supercompressionScheme: u32(44),
      keyValues: {},
      levels: [],
    };
    if (image.width < 1 || image.height < 1) {
      throw sceneKTX2Error("shape", "level 0 is " + image.width + "x" + image.height);
    }
    var indexEnd = 80 + image.levelCount * 24;
    if (bytes.byteLength < indexEnd) {
      throw sceneKTX2Error("truncated", "the level index runs past the file");
    }
    for (var level = 0; level < image.levelCount; level++) {
      var entry = 80 + level * 24;
      var offset = u64(entry);
      var length = u64(entry + 8);
      // A level may not start inside the header or the level index, and may not
      // end past the file. Either says the index disagrees with the payload.
      if (offset < indexEnd || length < 0 || offset + length > bytes.byteLength) {
        throw sceneKTX2Error("level-range", "level " + level + " spans " + offset + " to " + (offset + length));
      }
      image.levels.push({
        width: Math.max(1, image.width >> level),
        height: Math.max(1, image.height >> level),
        uncompressedByteLength: u64(entry + 16),
        bytes: bytes.subarray(offset, offset + length),
      });
    }
    // KTX2 key/value data is a sequence of uint32 byte lengths followed by a
    // NUL-terminated UTF-8 key, the value bytes, and 4-byte padding. IBL uses
    // it to pin role/color/model metadata. Malformed optional metadata is a
    // container error: accepting it would make the shader convention a guess.
    var kvdOffset = u32(56);
    var kvdLength = u32(60);
    if (kvdLength > 0) {
      if (kvdOffset < indexEnd || kvdOffset + kvdLength > bytes.byteLength) {
        throw sceneKTX2Error("kvd-range", "key/value data runs past the file");
      }
      var decoder = typeof TextDecoder === "function" ? new TextDecoder("utf-8") : null;
      var kvCursor = kvdOffset;
      var kvEnd = kvdOffset + kvdLength;
      while (kvCursor + 4 <= kvEnd) {
        var pairLength = u32(kvCursor);
        kvCursor += 4;
        if (pairLength < 2 || kvCursor + pairLength > kvEnd) {
          throw sceneKTX2Error("kvd-entry", "invalid key/value entry length " + pairLength);
        }
        var pair = bytes.subarray(kvCursor, kvCursor + pairLength);
        var zero = pair.indexOf(0);
        if (zero <= 0) {
          throw sceneKTX2Error("kvd-entry", "key/value entry has no key terminator");
        }
        var keyBytes = pair.subarray(0, zero);
        var valueBytes = pair.subarray(zero + 1);
        // render/bundle/ktx2 writes key\0value\0. Remove exactly the one
        // KTX2 string terminator, not arbitrary NULs inside a binary value.
        if (valueBytes.length > 0 && valueBytes[valueBytes.length - 1] === 0) {
          valueBytes = valueBytes.subarray(0, valueBytes.length - 1);
        }
        var key = decoder ? decoder.decode(keyBytes) : String.fromCharCode.apply(null, keyBytes);
        var value = decoder ? decoder.decode(valueBytes) : String.fromCharCode.apply(null, valueBytes);
        image.keyValues[key] = value;
        kvCursor += pairLength;
        kvCursor = (kvCursor + 3) & ~3;
      }
    }
    return image;
  }

  // sceneKTX2Inflate reads one supercompression scheme 3 payload.
  //
  // Scheme 3 is DEFLATE inside a zlib wrapper, which is what
  // DecompressionStream("deflate") reads. "deflate-raw" would drop the wrapper
  // and fail on the first byte.
  function sceneKTX2Inflate(bytes, expected) {
    if (typeof DecompressionStream !== "function" || typeof Response !== "function") {
      return Promise.reject(sceneKTX2Error("inflate", "no DecompressionStream, so scheme 3 cannot be read"));
    }
    var stream = new Response(bytes).body.pipeThrough(new DecompressionStream("deflate"));
    return new Response(stream).arrayBuffer().then(function(buffer) {
      var out = new Uint8Array(buffer);
      if (expected && out.byteLength !== expected) {
        throw sceneKTX2Error("inflate", "inflated to " + out.byteLength + " bytes, index says " + expected);
      }
      return out;
    });
  }

  // sceneKTX2Decode inflates every level and checks each one against the block
  // arithmetic of its format. The result is ready for an upload call.
  //
  // Scheme 2 is Zstandard. No GoSX writer emits it and no browser decodes it, so
  // the reader refuses it by name rather than return wrong pixels.
  function sceneKTX2Decode(source) {
    var image;
    var layout;
    try {
      image = sceneKTX2Parse(source);
      layout = sceneKTX2FormatInfo(image.vkFormat);
    } catch (error) {
      return Promise.reject(error);
    }
    var scheme = image.supercompressionScheme;
    if (scheme === KTX2_SCHEME_ZSTD) {
      return Promise.reject(sceneKTX2Error("scheme-zstd", "Zstandard is not supported"));
    }
    if (scheme === KTX2_SCHEME_BASISLZ) {
      return Promise.reject(sceneKTX2Error("scheme-basislz", "BasisLZ is not supported"));
    }
    if (scheme !== KTX2_SCHEME_NONE && scheme !== KTX2_SCHEME_ZLIB) {
      return Promise.reject(sceneKTX2Error("scheme", "unknown scheme " + scheme));
    }
    image.layout = layout;
    var pending = Promise.resolve();
    image.levels.forEach(function(level, index) {
      pending = pending.then(function() {
        if (scheme !== KTX2_SCHEME_ZLIB) {
          return level.bytes;
        }
        return sceneKTX2Inflate(level.bytes, level.uncompressedByteLength);
      }).then(function(decoded) {
        level.bytes = decoded;
        level.blockColumns = Math.ceil(level.width / layout.blockWidth);
        level.blockRows = Math.ceil(level.height / layout.blockHeight);
        var slices = image.layers * image.faces * Math.max(1, image.depth >> index);
        var want = level.blockColumns * level.blockRows * layout.bytesPerBlock * slices;
        // Trust the file, and fail loudly when it lies. A level that disagrees
        // with its own block arithmetic uploads as garbage on every backend.
        if (decoded.byteLength !== want) {
          throw sceneKTX2Error("level-size", "level " + index + " holds " + decoded.byteLength + " bytes, want " + want);
        }
      });
    });
    return pending.then(function() {
      return image;
    });
  }

  // sceneKTX2Load fetches a container and decodes it.
  function sceneKTX2Load(url, init) {
    return fetch(url, init || { credentials: "same-origin" }).then(function(response) {
      if (!response.ok) {
        throw sceneKTX2Error("fetch", "GET " + url + " gave HTTP " + response.status);
      }
      return response.arrayBuffer();
    }).then(sceneKTX2Decode);
  }

  function sceneKTX2DecodedLayout(image) {
    if (!image || !image.layout || !image.levels || !image.levels.length || !image.levels[0].blockColumns) {
      throw sceneKTX2Error("undecoded", "decode before upload");
    }
    return image.layout;
  }

  // sceneKTX2UploadWebGPU creates the texture and writes one mip level per call.
  //
  // WebGPU validates a compressed copy against the PHYSICAL mip size, which
  // rounds up to whole texel blocks. A 2x2 mip of a 4x4 block format therefore
  // copies a 4x4 extent. WebGL2 wants the LOGICAL size instead, so the two
  // upload paths pass different numbers for the same level on purpose.
  function sceneKTX2UploadWebGPU(device, image, options) {
    var layout = sceneKTX2DecodedLayout(image);
    var opts = options || {};
    var slices = image.layers * image.faces;
    var texture = device.createTexture({
      label: opts.label || "gosx.ktx2",
      size: { width: image.width, height: image.height, depthOrArrayLayers: slices },
      dimension: "2d",
      format: layout.webgpuFormat,
      mipLevelCount: image.levels.length,
      // TEXTURE_BINDING | COPY_DST. The bit values are stable WebGPU spec
      // numbers, which encodeTextureUsage in render/gpu/jsgpu also writes out.
      usage: 0x04 | 0x02,
    });
    for (var i = 0; i < image.levels.length; i++) {
      var level = image.levels[i];
      device.queue.writeTexture(
        { texture: texture, mipLevel: i },
        level.bytes,
        { offset: 0, bytesPerRow: level.blockColumns * layout.bytesPerBlock, rowsPerImage: level.blockRows },
        {
          width: level.blockColumns * layout.blockWidth,
          height: level.blockRows * layout.blockHeight,
          depthOrArrayLayers: slices,
        }
      );
    }
    return texture;
  }

  // sceneKTX2UploadWebGL2 binds a texture and uploads one mip level per call.
  //
  // getExtension is what turns a compressed internal format on, so the uploader
  // asks for the one this format needs before the first upload. A context
  // without it returns null and the uploader stops with a named error rather
  // than raise INVALID_ENUM inside the driver.
  function sceneKTX2UploadWebGL2(gl, image, options) {
    var layout = sceneKTX2DecodedLayout(image);
    var opts = options || {};
    if (layout.webglExtension && (typeof gl.getExtension !== "function" || !gl.getExtension(layout.webglExtension))) {
      throw sceneKTX2Error("extension", "this context has no " + layout.webglExtension);
    }
    var texture = opts.texture || gl.createTexture();
    var cube = image.faces === 6;
    if (image.faces !== 1 && !cube) {
      throw sceneKTX2Error("faces", "WebGL upload supports 1 or 6 faces, got " + image.faces);
    }
    var target = cube ? gl.TEXTURE_CUBE_MAP : gl.TEXTURE_2D;
    gl.bindTexture(target, texture);
    for (var i = 0; i < image.levels.length; i++) {
      var level = image.levels[i];
      var faceBytes = level.bytes.byteLength / image.faces;
      if (!Number.isInteger(faceBytes)) {
        throw sceneKTX2Error("face-size", "level " + i + " does not divide into " + image.faces + " faces");
      }
      for (var face = 0; face < image.faces; face++) {
        var faceTarget = cube ? gl.TEXTURE_CUBE_MAP_POSITIVE_X + face : gl.TEXTURE_2D;
        var payload = level.bytes.subarray(face * faceBytes, (face + 1) * faceBytes);
        if (layout.compressed) {
          gl.compressedTexImage2D(faceTarget, i, layout.webglInternalFormat, level.width, level.height, 0, payload);
        } else {
          gl.texImage2D(faceTarget, i, layout.webglInternalFormat, level.width, level.height, 0, layout.webglFormat, layout.webglType, payload);
        }
      }
    }
    gl.texParameteri(target, gl.TEXTURE_MAX_LEVEL, image.levels.length - 1);
    gl.texParameteri(target, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(target, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    if (cube && gl.TEXTURE_WRAP_R !== undefined) {
      gl.texParameteri(target, gl.TEXTURE_WRAP_R, gl.CLAMP_TO_EDGE);
    }
    return texture;
  }

  // sceneKTX2UploadPathReady reports whether a renderer registered a texture
  // loader that can upload a KTX2 container.
  //
  // The variant swap in 19-scene-gltf.js reads this flag. The Go side already
  // refuses a variant whose file was never built; this is the same rule applied
  // to the consumer. A renderer that still loads every image URI through an
  // image element cannot decode a .ktx2 file, so swapping the URI would trade a
  // working texture for a broken one. A renderer registers the loader by setting
  // window.__gosx_scene3d_ktx2_texture_loader.
  function sceneKTX2UploadPathReady() {
    return typeof window !== "undefined" && window.__gosx_scene3d_ktx2_texture_loader != null;
  }

  if (typeof window !== "undefined") {
    window.__gosx_scene3d_ktx2 = {
      parse: sceneKTX2Parse,
      decode: sceneKTX2Decode,
      load: sceneKTX2Load,
      formatInfo: sceneKTX2FormatInfo,
      uploadWebGPU: sceneKTX2UploadWebGPU,
      uploadWebGL2: sceneKTX2UploadWebGL2,
      uploadPathReady: sceneKTX2UploadPathReady,
    };
  }
