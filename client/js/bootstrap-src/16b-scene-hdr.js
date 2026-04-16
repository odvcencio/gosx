  // Radiance HDR parsing helpers for Scene3D environment maps.
  //
  // Kept separate from the WebGL renderer so format decoding is testable and
  // reusable by WebGL/WebGPU texture pipelines.

  function sceneHDRReadLine(bytes, cursor) {
    var start = cursor.offset;
    while (cursor.offset < bytes.length && bytes[cursor.offset] !== 10) {
      cursor.offset++;
    }
    var end = cursor.offset;
    if (cursor.offset < bytes.length && bytes[cursor.offset] === 10) {
      cursor.offset++;
    }
    if (end > start && bytes[end - 1] === 13) {
      end--;
    }
    var out = "";
    for (var i = start; i < end; i++) {
      out += String.fromCharCode(bytes[i]);
    }
    return out;
  }

  function sceneHDRRGBEToFloat(out, outOffset, r, g, b, e) {
    if (!e) {
      out[outOffset] = 0;
      out[outOffset + 1] = 0;
      out[outOffset + 2] = 0;
      return;
    }
    var scale = Math.pow(2, e - 136);
    out[outOffset] = r * scale;
    out[outOffset + 1] = g * scale;
    out[outOffset + 2] = b * scale;
  }

  function sceneHDRParseResolution(line) {
    var match = /^\s*([+-])Y\s+(\d+)\s+([+-])X\s+(\d+)\s*$/.exec(String(line || ""));
    if (!match) {
      match = /^\s*([+-])X\s+(\d+)\s+([+-])Y\s+(\d+)\s*$/.exec(String(line || ""));
      if (!match) {
        return null;
      }
      return {
        width: Math.max(0, parseInt(match[2], 10) || 0),
        height: Math.max(0, parseInt(match[4], 10) || 0),
      };
    }
    return {
      width: Math.max(0, parseInt(match[4], 10) || 0),
      height: Math.max(0, parseInt(match[2], 10) || 0),
    };
  }

  function sceneHDRDecodeRawRGBE(bytes, offset, width, height) {
    var count = width * height;
    if (bytes.length - offset < count * 4) {
      throw new Error("Radiance HDR data is truncated");
    }
    var data = new Float32Array(count * 3);
    for (var i = 0; i < count; i++) {
      var src = offset + i * 4;
      sceneHDRRGBEToFloat(data, i * 3, bytes[src], bytes[src + 1], bytes[src + 2], bytes[src + 3]);
    }
    return data;
  }

  function sceneHDRDecodeRLEScanlines(bytes, offset, width, height) {
    var data = new Float32Array(width * height * 3);
    var scanline = new Uint8Array(width * 4);
    var cursor = offset;

    for (var y = 0; y < height; y++) {
      if (cursor + 4 > bytes.length) {
        throw new Error("Radiance HDR scanline is truncated");
      }
      var b0 = bytes[cursor++];
      var b1 = bytes[cursor++];
      var b2 = bytes[cursor++];
      var b3 = bytes[cursor++];
      var scanWidth = (b2 << 8) | b3;
      if (b0 !== 2 || b1 !== 2 || scanWidth !== width) {
        return sceneHDRDecodeRawRGBE(bytes, offset, width, height);
      }

      for (var channel = 0; channel < 4; channel++) {
        var x = 0;
        while (x < width) {
          if (cursor >= bytes.length) {
            throw new Error("Radiance HDR RLE data is truncated");
          }
          var code = bytes[cursor++];
          if (code > 128) {
            var run = code - 128;
            if (cursor >= bytes.length || x + run > width) {
              throw new Error("Radiance HDR RLE run is invalid");
            }
            var value = bytes[cursor++];
            for (var ri = 0; ri < run; ri++) {
              scanline[(x + ri) * 4 + channel] = value;
            }
            x += run;
          } else {
            var literal = code;
            if (literal === 0 || cursor + literal > bytes.length || x + literal > width) {
              throw new Error("Radiance HDR RLE literal is invalid");
            }
            for (var li = 0; li < literal; li++) {
              scanline[(x + li) * 4 + channel] = bytes[cursor++];
            }
            x += literal;
          }
        }
      }

      for (var px = 0; px < width; px++) {
        var src = px * 4;
        var dst = (y * width + px) * 3;
        sceneHDRRGBEToFloat(data, dst, scanline[src], scanline[src + 1], scanline[src + 2], scanline[src + 3]);
      }
    }

    return data;
  }

  function sceneParseRadianceHDR(arrayBuffer) {
    var bytes = arrayBuffer instanceof Uint8Array
      ? arrayBuffer
      : new Uint8Array(arrayBuffer || []);
    var cursor = { offset: 0 };
    var first = sceneHDRReadLine(bytes, cursor);
    if (first.indexOf("#?RADIANCE") !== 0 && first.indexOf("#?RGBE") !== 0) {
      throw new Error("unsupported Radiance HDR header");
    }

    var resolution = null;
    for (var guard = 0; guard < 128 && cursor.offset < bytes.length; guard++) {
      var line = sceneHDRReadLine(bytes, cursor);
      var parsed = sceneHDRParseResolution(line);
      if (parsed) {
        resolution = parsed;
        break;
      }
    }
    if (!resolution || resolution.width <= 0 || resolution.height <= 0) {
      throw new Error("Radiance HDR resolution is missing");
    }
    if (resolution.width > 4096 || resolution.height > 2048) {
      throw new Error("Radiance HDR exceeds Scene3D size limit");
    }

    var data = (resolution.width >= 8 && resolution.width <= 32767)
      ? sceneHDRDecodeRLEScanlines(bytes, cursor.offset, resolution.width, resolution.height)
      : sceneHDRDecodeRawRGBE(bytes, cursor.offset, resolution.width, resolution.height);
    return {
      width: resolution.width,
      height: resolution.height,
      data: data,
    };
  }

  if (typeof window !== "undefined") {
    window.__gosx_scene3d_resource_api = Object.assign(window.__gosx_scene3d_resource_api || {}, {
      parseRadianceHDR: sceneParseRadianceHDR,
    });
  }
