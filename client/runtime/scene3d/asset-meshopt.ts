// EXT_meshopt_compression decoder, shipped in the glTF chunk.
//
// window.__gosx_scene3d_asset_ext.meshopt.decode(count, stride, source, mode, filter)
// returns the decoded bytes of one bufferView: count elements of stride bytes.
//   mode    "ATTRIBUTES" (vertex codec, version 0) or "TRIANGLES" (index codec,
//           versions 0 and 1); "INDICES" throws
//   filter  "NONE", "OCTAHEDRAL" or "EXPONENTIAL"; QUATERNION throws
//
// The formats are those of meshoptimizer, which the extension specifies. The
// tests decode gltfpack output and compare it with the same asset written
// without compression.
(function () {
  var win: any = window;
  var ext: any = win.__gosx_scene3d_asset_ext || (win.__gosx_scene3d_asset_ext = {});

  var BLOCK_BYTES = 8192;
  var BLOCK_MAX_VERTICES = 256;
  var GROUP = 16;

  function fail(message: string): never {
    throw new Error("meshopt: " + message);
  }

  // One byte group: 16 values in 0, 2, 4 or 8 bit form. A packed value equal to
  // the form's maximum is an escape; the real byte follows the packed bits.
  function decodeGroup(src: Uint8Array, pos: number, mode: number, out: Uint8Array, outPos: number): number {
    var i: number;
    if (mode === 0) {
      for (i = 0; i < GROUP; i++) out[outPos + i] = 0;
      return pos;
    }
    if (mode === 3) {
      if (pos + GROUP > src.length) fail("truncated vertex data");
      for (i = 0; i < GROUP; i++) out[outPos + i] = src[pos + i];
      return pos + GROUP;
    }
    var bits = mode === 1 ? 2 : 4;
    var packed = (GROUP * bits) >> 3;
    var sentinel = (1 << bits) - 1;
    var escape = pos + packed;
    if (escape > src.length) fail("truncated vertex data");
    for (i = 0; i < GROUP; i++) {
      var byte = src[pos + ((i * bits) >> 3)];
      var shift = 8 - bits - ((i * bits) & 7);
      var value = (byte >> shift) & sentinel;
      if (value === sentinel) {
        if (escape >= src.length) fail("truncated vertex data");
        value = src[escape++];
      }
      out[outPos + i] = value;
    }
    return escape;
  }

  function decodeVertexBuffer(count: number, stride: number, src: Uint8Array): Uint8Array {
    if (stride <= 0 || stride > 256 || stride % 4) fail("vertex stride " + stride + " is not a multiple of 4 up to 256");
    if (src.length < 1 + stride) fail("vertex buffer is too short");
    if ((src[0] & 0xf0) !== 0xa0) fail("bad vertex buffer header");
    if ((src[0] & 0x0f) !== 0) fail("vertex codec version " + (src[0] & 0x0f) + " is not supported");
    var out = new Uint8Array(count * stride);
    var last = src.slice(src.length - stride);
    var block = Math.min(BLOCK_MAX_VERTICES, ((BLOCK_BYTES / stride) | 0) & ~(GROUP - 1));
    var end = src.length - stride;
    var pos = 1;
    var column = new Uint8Array(BLOCK_MAX_VERTICES);
    for (var start = 0; start < count; start += block) {
      var n = Math.min(block, count - start);
      var aligned = (n + GROUP - 1) & ~(GROUP - 1);
      var groups = aligned / GROUP;
      var headerSize = (groups + 3) >> 2;
      for (var k = 0; k < stride; k++) {
        if (pos + headerSize > end) fail("truncated vertex data");
        var header = pos;
        pos += headerSize;
        for (var g = 0; g < groups; g++) {
          pos = decodeGroup(src, pos, (src[header + (g >> 2)] >> ((g & 3) * 2)) & 3, column, g * GROUP);
        }
        var previous = last[k];
        for (var v = 0; v < n; v++) {
          var z = column[v];
          previous = (previous + ((z >> 1) ^ -(z & 1))) & 0xff;
          out[(start + v) * stride + k] = previous;
        }
        last[k] = previous;
      }
    }
    if (pos > end) fail("vertex data overruns its buffer");
    return out;
  }

  function readVByte(src: Uint8Array, state: { pos: number }): number {
    var value = 0;
    for (var shift = 0; shift < 35; shift += 7) {
      if (state.pos >= src.length) fail("truncated index data");
      var byte = src[state.pos++];
      value |= (byte & 0x7f) << shift;
      if (byte < 0x80) return value >>> 0;
    }
    return fail("index varint is too long");
  }

  function writeIndex(out: Uint8Array, at: number, size: number, value: number) {
    out[at * size] = value & 0xff;
    out[at * size + 1] = (value >> 8) & 0xff;
    if (size === 4) {
      out[at * size + 2] = (value >> 16) & 0xff;
      out[at * size + 3] = (value >>> 24) & 0xff;
    }
  }

  function decodeIndexBuffer(count: number, size: number, src: Uint8Array): Uint8Array {
    if (size !== 2 && size !== 4) fail("index size " + size + " is not 2 or 4");
    if (count % 3) fail("triangle index count " + count + " is not a multiple of 3");
    if (src.length < 1 + count / 3 + 16) fail("index buffer is too short");
    if ((src[0] & 0xf0) !== 0xe0 || (src[0] & 0x0f) > 1) fail("bad index buffer header");
    var out = new Uint8Array(count * size);
    var edgeA = new Uint32Array(16), edgeB = new Uint32Array(16), fifo = new Uint32Array(16);
    var edgeOffset = 0, fifoOffset = 0, next = 0, last = 0;
    var code = 1;
    var state = { pos: 1 + count / 3 };
    var aux = src.length - 16;

    function pushEdge(a: number, b: number) {
      edgeA[edgeOffset] = a; edgeB[edgeOffset] = b; edgeOffset = (edgeOffset + 1) & 15;
    }
    function pushVertex(v: number, advance: boolean) {
      fifo[fifoOffset] = v; if (advance) fifoOffset = (fifoOffset + 1) & 15;
    }
    function freeIndex(): number {
      var v = readVByte(src, state);
      last = (last + ((v >>> 1) ^ -(v & 1))) >>> 0;
      return last;
    }

    for (var i = 0; i < count; i += 3) {
      var tri = src[code++];
      var a: number, b: number, c: number;
      if (tri < 0xf0) {
        var fe = tri >> 4;
        var slot = (edgeOffset - 1 - fe) & 15;
        a = edgeA[slot]; b = edgeB[slot];
        var fec = tri & 15;
        // 0 is the next new vertex, 1 to 12 name a recent vertex, 13 and 14 are
        // the last explicit index minus or plus one, and 15 reads an explicit
        // index.
        if (fec === 0) { c = next++; pushVertex(c, true); }
        else if (fec <= 12) { c = fifo[(fifoOffset - 1 - fec) & 15]; }
        else if (fec === 13) { c = last = (last - 1) >>> 0; pushVertex(c, true); }
        else if (fec === 14) { c = last = (last + 1) >>> 0; pushVertex(c, true); }
        else { c = freeIndex(); pushVertex(c, true); }
        pushEdge(c, b);
        pushEdge(a, c);
      } else if (tri < 0xfe) {
        var table = src[aux + (tri & 15)];
        var feb = table >> 4, fc = table & 15;
        a = next++;
        if (feb === 0) b = next++; else b = fifo[(fifoOffset - feb) & 15];
        if (fc === 0) c = next++; else c = fifo[(fifoOffset - fc) & 15];
        pushVertex(a, true);
        if (feb === 0) pushVertex(b, true);
        if (fc === 0) pushVertex(c, true);
        pushEdge(b, a); pushEdge(c, b); pushEdge(a, c);
      } else {
        var packed = src[state.pos++];
        var f0 = tri === 0xfe ? 0 : 15;
        var f1 = packed >> 4, f2 = packed & 15;
        a = f0 === 0 ? next++ : 0;
        b = f1 === 0 ? next++ : f1 === 15 ? 0 : fifo[(fifoOffset - f1) & 15];
        c = f2 === 0 ? next++ : f2 === 15 ? 0 : fifo[(fifoOffset - f2) & 15];
        if (f0 === 15) a = freeIndex();
        if (f1 === 15) b = freeIndex();
        if (f2 === 15) c = freeIndex();
        pushVertex(a, true);
        pushVertex(b, f1 === 0 || f1 === 15);
        pushVertex(c, f2 === 0 || f2 === 15);
        pushEdge(b, a); pushEdge(c, b); pushEdge(a, c);
      }
      writeIndex(out, i, size, a);
      writeIndex(out, i + 1, size, b);
      writeIndex(out, i + 2, size, c);
    }
    return out;
  }

  function filterOctahedral(data: Uint8Array, count: number, stride: number) {
    if (stride !== 4 && stride !== 8) fail("octahedral filter needs a stride of 4 or 8");
    var wide = stride === 8;
    var view = new DataView(data.buffer, data.byteOffset, data.byteLength);
    var max = wide ? 32767 : 127;
    for (var i = 0; i < count; i++) {
      var o = i * stride;
      var x = wide ? view.getInt16(o, true) : view.getInt8(o);
      var y = wide ? view.getInt16(o + 2, true) : view.getInt8(o + 1);
      var z = (wide ? view.getInt16(o + 4, true) : view.getInt8(o + 2)) - Math.abs(x) - Math.abs(y);
      var t = z >= 0 ? 0 : z;
      x += x >= 0 ? t : -t;
      y += y >= 0 ? t : -t;
      var s = max / Math.sqrt(x * x + y * y + z * z);
      var xf = Math.trunc(x * s + (x >= 0 ? 0.5 : -0.5));
      var yf = Math.trunc(y * s + (y >= 0 ? 0.5 : -0.5));
      var zf = Math.trunc(z * s + (z >= 0 ? 0.5 : -0.5));
      if (wide) { view.setInt16(o, xf, true); view.setInt16(o + 2, yf, true); view.setInt16(o + 4, zf, true); }
      else { view.setInt8(o, xf); view.setInt8(o + 1, yf); view.setInt8(o + 2, zf); }
    }
  }

  function filterExponential(data: Uint8Array, count: number, stride: number) {
    var view = new DataView(data.buffer, data.byteOffset, data.byteLength);
    var words = (count * stride) >> 2;
    for (var i = 0; i < words; i++) {
      var v = view.getInt32(i * 4, true);
      view.setFloat32(i * 4, (v << 8 >> 8) * Math.pow(2, v >> 24), true);
    }
  }

  ext.meshopt = {
    decode: function (count: number, stride: number, source: Uint8Array, mode: string, filter: string): Uint8Array {
      var out: Uint8Array;
      if (mode === "ATTRIBUTES") out = decodeVertexBuffer(count, stride, source);
      else if (mode === "TRIANGLES") out = decodeIndexBuffer(count, stride, source);
      else return fail("unknown mode " + mode);
      if (filter === "OCTAHEDRAL") filterOctahedral(out, count, stride);
      else if (filter === "EXPONENTIAL") filterExponential(out, count, stride);
      else if (filter && filter !== "NONE") fail("filter " + filter + " is not supported");
      return out;
    },
  };
})();
