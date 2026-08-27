  // gltf.ts — Scene3D glTF/GLB loader — parses glTF 2.0 binary and JSON formats into
  // @ts-check
  // the scene model asset structure consumed by 20-scene-mount.js.
  //
  // This file is concatenated into the bootstrap IIFE alongside all other
  // bootstrap-src/*.js files. No imports or exports.

  /**
   * @typedef {object} GoSXSceneModelAsset
   * @property {object} json
   * @property {ArrayBuffer|null} [binaryBuffer]
   */

  // ---------------------------------------------------------------------------
  // GLB binary parser
  // ---------------------------------------------------------------------------

  function sceneDecodeUTF8Bytes(bytes) {
    if (typeof TextDecoder === "function") {
      return new TextDecoder().decode(bytes);
    }
    var chunkSize = 0x8000;
    var decoded = "";
    for (var index = 0; index < bytes.length; index += chunkSize) {
      var chunk = bytes.subarray(index, Math.min(index + chunkSize, bytes.length));
      decoded += String.fromCharCode.apply(null, Array.prototype.slice.call(chunk));
    }
    try {
      return decodeURIComponent(escape(decoded));
    } catch (_error) {
      return decoded;
    }
  }

  function sceneParseGLB(arrayBuffer) {
    var view = new DataView(arrayBuffer);

    // Header: magic(4) + version(4) + length(4) = 12 bytes.
    var magic = view.getUint32(0, true);
    if (magic !== 0x46546C67) {
      throw new Error("Invalid GLB magic");
    }
    var version = view.getUint32(4, true);
    if (version !== 2) {
      throw new Error("Unsupported glTF version: " + version);
    }

    // Chunk 0: JSON (type 0x4E4F534A).
    var jsonChunkLength = view.getUint32(12, true);
    var jsonBytes = new Uint8Array(arrayBuffer, 20, jsonChunkLength);
    var json = JSON.parse(sceneDecodeUTF8Bytes(jsonBytes));

    // Chunk 1: binary buffer (type 0x004E4942), optional.
    var binaryBuffer = null;
    var binaryOffset = 20 + jsonChunkLength;
    if (binaryOffset < arrayBuffer.byteLength) {
      var binChunkLength = view.getUint32(binaryOffset, true);
      binaryBuffer = arrayBuffer.slice(binaryOffset + 8, binaryOffset + 8 + binChunkLength);
    }

    return { json: json, binaryBuffer: binaryBuffer };
  }

  // ---------------------------------------------------------------------------
  // Accessor / buffer-view reading
  // ---------------------------------------------------------------------------

  // Per-componentType record: [byte size, typed-array constructor, DataView
  // reader name, normalized-integer divisor, signed flag]. The signed flag
  // keeps the -1 endpoint of signed types alive through division rounding.
  // The flag is only recorded for signed types; normalization reads it by
  // truthiness, so unsigned records leave slot 4 undefined (falsy).
  // FLOAT32, an omitted view slot, and anything unlisted all resolve through
  // GLTF_FLOAT32_FORMAT below, so every fallback matches the old default
  // branches exactly (4-byte size, Float32Array view) and those same keys
  // copy through normalization unchanged. Every table literal opens with
  // __proto__: null, so a hostile or corrupt componentType or type string
  // ("constructor", "toString", "__proto__") misses and falls through to the
  // defaults instead of matching inherited Object.prototype members.
  var GLTF_COMPONENT_FORMATS = {
    __proto__: null,
    5120: [1, Int8Array, "getInt8", 127, true],
    5121: [1, Uint8Array, "getUint8", 255],
    5122: [2, Int16Array, "getInt16", 32767, true],
    5123: [2, Uint16Array, "getUint16", 65535],
    5125: [4, Uint32Array, "getUint32", 4294967295],
  };
  var GLTF_FLOAT32_FORMAT = [4, Float32Array, "getFloat32"];

  // Elements per accessor record by glTF type name; anything unlisted reads
  // as one scalar component.
  var GLTF_TYPE_COUNTS = {
    __proto__: null,
    SCALAR: 1,
    VEC2: 2,
    VEC3: 3,
    VEC4: 4,
    MAT2: 4,
    MAT3: 9,
    MAT4: 16,
  };

  function gltfAccessorTypeCount(type) {
    return GLTF_TYPE_COUNTS[type] || 1;
  }

  function gltfTypedArrayView(buffer, byteOffset, componentType, count) {
    var format = GLTF_COMPONENT_FORMATS[componentType] || GLTF_FLOAT32_FORMAT;
    return new format[1](buffer, byteOffset, count);
  }

  function gltfNormalizeAccessorValues(values, componentType) {
    var normalized = new Float32Array(values.length);
    var quantization = GLTF_COMPONENT_FORMATS[componentType];
    if (!quantization) {
      normalized.set(values);
      return normalized;
    }
    var divisor = quantization[3];
    var signed = quantization[4];
    for (var i = 0; i < values.length; i++) {
      var value = values[i] / divisor;
      normalized[i] = signed && value < -1 ? -1 : value;
    }
    return normalized;
  }

  // ---------------------------------------------------------------------------
  // Extension helpers
  // ---------------------------------------------------------------------------

  // Read one extension object from any glTF record (material, texture,
  // primitive, bufferView). Returns null when the record does not carry it.
  function gltfExtension(record, name) {
    if (!record || !record.extensions) {
      return null;
    }
    var value = record.extensions[name];
    return value && typeof value === "object" ? value : null;
  }

  // Read a scalar factor from an extension object, clamped to [min, max].
  function gltfExtensionFactor(extension, key, fallback, min, max) {
    if (!extension || extension[key] == null) {
      return fallback;
    }
    var value = Number(extension[key]);
    if (!isFinite(value)) {
      return fallback;
    }
    return Math.max(min, Math.min(max, value));
  }

  // Compression extensions rewrite the bytes a bufferView or primitive points
  // at. The loader has no decoder for them, so reading the raw bytes would
  // build a corrupt mesh. Throw instead, with the extension named.
  function gltfRejectCompressedBufferView(bufferView) {
    if (gltfExtension(bufferView, "EXT_meshopt_compression")) {
      throw new Error(
        "glTF uses EXT_meshopt_compression, which this loader cannot decode. " +
        "Re-export the asset without meshopt compression, or run gltfpack without -cc."
      );
    }
  }

  function gltfRejectCompressedPrimitive(primitive) {
    if (gltfExtension(primitive, "KHR_draco_mesh_compression")) {
      throw new Error(
        "glTF uses KHR_draco_mesh_compression, which this loader cannot decode. " +
        "Re-export the asset without Draco compression."
      );
    }
  }

  function gltfReadAccessor(gltf, accessorIndex, binaryBuffer) {
    var accessor = gltf.accessors[accessorIndex];
    var bufferView = gltf.bufferViews[accessor.bufferView];
    gltfRejectCompressedBufferView(bufferView);
    var buffer = binaryBuffer;

    var byteOffset = (bufferView.byteOffset || 0) + (accessor.byteOffset || 0);
    var componentCount = gltfAccessorTypeCount(accessor.type);
    var componentFormat = GLTF_COMPONENT_FORMATS[accessor.componentType] || GLTF_FLOAT32_FORMAT;
    var componentSize = componentFormat[0];
    var stride = bufferView.byteStride || 0;
    var totalElements = accessor.count * componentCount;

    // Fast path: tightly packed data with no stride.
    var result;
    if (!stride || stride === componentCount * componentSize) {
      result = gltfTypedArrayView(buffer, byteOffset, accessor.componentType, totalElements);
    } else {
      // Interleaved: copy element-by-element.
      result = new Float32Array(totalElements);
      var src = new DataView(buffer);
      for (var i = 0; i < accessor.count; i++) {
        var elemOffset = byteOffset + i * stride;
        for (var c = 0; c < componentCount; c++) {
          var co = elemOffset + c * componentSize;
          // One reader per componentType, little-endian throughout; single-byte
          // readers ignore the endianness argument and unknown types read
          // through the getFloat32 fallback recorded above.
          result[i * componentCount + c] =
            src[componentFormat[2]](co, true);
        }
      }
    }
    return accessor.normalized ? gltfNormalizeAccessorValues(result, accessor.componentType) : result;
  }

  // ---------------------------------------------------------------------------
  // Flat normal generation (when NORMAL attribute is absent)
  // ---------------------------------------------------------------------------

  function gltfGenerateFlatNormals(positions) {
    var normals = new Float32Array(positions.length);
    var triCount = positions.length / 9;
    for (var t = 0; t < triCount; t++) {
      var i = t * 9;
      var ax = positions[i],     ay = positions[i + 1], az = positions[i + 2];
      var bx = positions[i + 3], by = positions[i + 4], bz = positions[i + 5];
      var cx = positions[i + 6], cy = positions[i + 7], cz = positions[i + 8];

      var e1x = bx - ax, e1y = by - ay, e1z = bz - az;
      var e2x = cx - ax, e2y = cy - ay, e2z = cz - az;

      var nx = e1y * e2z - e1z * e2y;
      var ny = e1z * e2x - e1x * e2z;
      var nz = e1x * e2y - e1y * e2x;
      var len = Math.sqrt(nx * nx + ny * ny + nz * nz);
      if (len > 1e-8) { nx /= len; ny /= len; nz /= len; }
      else { nx = 0; ny = 1; nz = 0; }

      for (var v = 0; v < 3; v++) {
        normals[i + v * 3]     = nx;
        normals[i + v * 3 + 1] = ny;
        normals[i + v * 3 + 2] = nz;
      }
    }
    return normals;
  }

  // ---------------------------------------------------------------------------
  // Default UV generation (when TEXCOORD_0 is absent)
  // ---------------------------------------------------------------------------

  function gltfGenerateDefaultUVs(vertexCount) {
    return new Float32Array(vertexCount * 2);
  }

  // ---------------------------------------------------------------------------
  // Tangent computation (simplified MikkTSpace)
  // ---------------------------------------------------------------------------

  function gltfComputeTangents(positions, normals, uvs) {
    var vertexCount = positions.length / 3;
    var tangents = new Float32Array(vertexCount * 4);
    var tan1 = new Float32Array(vertexCount * 3);
    var tan2 = new Float32Array(vertexCount * 3);

    var triCount = vertexCount / 3;
    for (var t = 0; t < triCount; t++) {
      var i0 = t * 3, i1 = t * 3 + 1, i2 = t * 3 + 2;

      var p0x = positions[i0 * 3],     p0y = positions[i0 * 3 + 1], p0z = positions[i0 * 3 + 2];
      var p1x = positions[i1 * 3],     p1y = positions[i1 * 3 + 1], p1z = positions[i1 * 3 + 2];
      var p2x = positions[i2 * 3],     p2y = positions[i2 * 3 + 1], p2z = positions[i2 * 3 + 2];

      var u0 = uvs[i0 * 2], v0 = uvs[i0 * 2 + 1];
      var u1 = uvs[i1 * 2], v1 = uvs[i1 * 2 + 1];
      var u2 = uvs[i2 * 2], v2 = uvs[i2 * 2 + 1];

      var e1x = p1x - p0x, e1y = p1y - p0y, e1z = p1z - p0z;
      var e2x = p2x - p0x, e2y = p2y - p0y, e2z = p2z - p0z;

      var du1 = u1 - u0, dv1 = v1 - v0;
      var du2 = u2 - u0, dv2 = v2 - v0;

      var denom = du1 * dv2 - du2 * dv1;
      var r = Math.abs(denom) > 1e-10 ? 1.0 / denom : 0.0;

      var sx = (dv2 * e1x - dv1 * e2x) * r;
      var sy = (dv2 * e1y - dv1 * e2y) * r;
      var sz = (dv2 * e1z - dv1 * e2z) * r;

      var tx = (du1 * e2x - du2 * e1x) * r;
      var ty = (du1 * e2y - du2 * e1y) * r;
      var tz = (du1 * e2z - du2 * e1z) * r;

      for (var vi = 0; vi < 3; vi++) {
        var idx = (t * 3 + vi) * 3;
        tan1[idx] += sx; tan1[idx + 1] += sy; tan1[idx + 2] += sz;
        tan2[idx] += tx; tan2[idx + 1] += ty; tan2[idx + 2] += tz;
      }
    }

    // Orthogonalize and compute handedness.
    for (var i = 0; i < vertexCount; i++) {
      var nx = normals[i * 3], ny = normals[i * 3 + 1], nz = normals[i * 3 + 2];
      var t1x = tan1[i * 3], t1y = tan1[i * 3 + 1], t1z = tan1[i * 3 + 2];

      // Gram-Schmidt orthogonalize: tangent = normalize(t - n * dot(n, t))
      var dot = nx * t1x + ny * t1y + nz * t1z;
      var ox = t1x - nx * dot;
      var oy = t1y - ny * dot;
      var oz = t1z - nz * dot;
      var len = Math.sqrt(ox * ox + oy * oy + oz * oz);
      if (len > 1e-8) { ox /= len; oy /= len; oz /= len; }
      else { ox = 1; oy = 0; oz = 0; }

      // Handedness: sign(dot(cross(n, t), t2))
      var cx = ny * t1z - nz * t1y;
      var cy = nz * t1x - nx * t1z;
      var cz = nx * t1y - ny * t1x;
      var t2x = tan2[i * 3], t2y = tan2[i * 3 + 1], t2z = tan2[i * 3 + 2];
      var w = (cx * t2x + cy * t2y + cz * t2z) < 0 ? -1.0 : 1.0;

      tangents[i * 4]     = ox;
      tangents[i * 4 + 1] = oy;
      tangents[i * 4 + 2] = oz;
      tangents[i * 4 + 3] = w;
    }

    return tangents;
  }

  // ---------------------------------------------------------------------------
  // Index expansion — convert indexed geometry to flat triangle arrays
  // ---------------------------------------------------------------------------

  // One specialized fixed-width expansion for the optional stride-4 streams
  // (tangents, joints, weights): null streams stay null, empty index lists
  // yield zero-length outputs, byte-for-byte identical to the inline
  // branches this replaces.
  function gltfExpandIndexedWidth4(src, indices, count) {
    if (!src) {
      return null;
    }
    var out = new Float32Array(count * 4);
    for (var i = 0; i < count; i++) {
      var idx = indices[i] * 4;
      out[i * 4]     = src[idx];
      out[i * 4 + 1] = src[idx + 1];
      out[i * 4 + 2] = src[idx + 2];
      out[i * 4 + 3] = src[idx + 3];
    }
    return out;
  }

  function gltfExpandIndexed(positions, normals, uvs, tangents, joints, weights, indices) {
    var count = indices.length;
    var outPos = new Float32Array(count * 3);
    var outNrm = new Float32Array(count * 3);
    var outUV  = new Float32Array(count * 2);
    for (var i = 0; i < count; i++) {
      var idx = indices[i];
      outPos[i * 3]     = positions[idx * 3];
      outPos[i * 3 + 1] = positions[idx * 3 + 1];
      outPos[i * 3 + 2] = positions[idx * 3 + 2];

      outNrm[i * 3]     = normals[idx * 3];
      outNrm[i * 3 + 1] = normals[idx * 3 + 1];
      outNrm[i * 3 + 2] = normals[idx * 3 + 2];

      outUV[i * 2]     = uvs[idx * 2];
      outUV[i * 2 + 1] = uvs[idx * 2 + 1];
    }

    return {
      positions: outPos,
      normals: outNrm,
      uvs: outUV,
      tangents: gltfExpandIndexedWidth4(tangents, indices, count),
      joints: gltfExpandIndexedWidth4(joints, indices, count),
      weights: gltfExpandIndexedWidth4(weights, indices, count),
    };
  }

  // ---------------------------------------------------------------------------
  // Static morph-target folding (primitive.targets, load time only)
  // ---------------------------------------------------------------------------

  // Fold primitive.targets POSITION/NORMAL/TANGENT deltas into primitive-local
  // streams using the authored node-over-mesh weights:
  //   result = base + sum_i weights[i] * target[i]
  // Runs once per node instantiation during primitive extraction, never per
  // frame, and always BEFORE any node/world transform or skinning: the Khronos
  // glTF 2.0 invariant requires POSITION/NORMAL/TANGENT deltas to land in
  // primitive-local space first. Because extraction folds before UV baking and
  // fallback-tangent generation, computed tangents describe the morphed
  // surface. Each present channel reads straight from its target accessor and
  // walks the SAME index map as the base attributes, so delta vertex
  // indices[v] feeds corner v; unindexed primitives pair vertex v directly.
  // Corners whose delta index falls outside a short accessor are left
  // untouched, an incomplete trailing vertex shorter than one full stride is
  // likewise never folded, and target channels naming missing accessors are
  // skipped, so malformed assets degrade safely instead of poisoning the
  // streams. Tangent w survives because deltas displace xyz only.
  //
  // Copy-on-effective-fold: the streams handed in may still be views over the
  // shared GLB buffer (unindexed primitives skip the eager expansion copies),
  // so each channel is copied with its exact source length only when a valid
  // finite non-zero morph weight AND a present target accessor mean the fold
  // can actually write it. Absent, invalid, all-zero, and channel-missing
  // morphs allocate no stream copies and hand the untouched streams back. The
  // returned list echoes the inputs where a channel never wrote (positions is
  // always present; 3-wide channels are POSITION and NORMAL, TANGENT is the
  // lone 4-wide channel and the copy keeps its w). Weights are validated one
  // by one: non-array lists read as all-zero, and every entry must be a
  // finite non-zero number to apply.
  function gltfFoldMorphTargets(gltf, primitive, binaryBuffer, indices, positions, normals, tangents, weights) {
    var targets = primitive && primitive.targets;
    if (!targets || !targets.length || !positions) {
      return null;
    }
    // One slot per channel; the no-normal/no-tangent case skips two.
    var streams = normals || tangents ? [positions, normals, tangents] : [positions];
    // Per-channel copy flags: each channel detaches from the shared GLB view
    // exactly once, at the first corner a fold actually writes (a valid finite
    // non-zero weight over a present target accessor whose deltas carry at
    // least three components AND an in-range delta index over a COMPLETE
    // destination vertex), keeping the exact source length so a short view
    // stays short. A trailing fragment narrower than one stride never folds,
    // so a two-float base with a three-wide delta stays bit-identical.
    // POSITION/NORMAL are 3-wide; TANGENT is the lone 4-wide channel.
    var copied = [];
    function foldChannel(channel, accessorIndex, stride) {
      var values = streams[channel];
      if (!values || accessorIndex == null || !gltf.accessors[accessorIndex]) {
        return;
      }
      var deltas = gltfReadAccessor(gltf, accessorIndex, binaryBuffer);
      if (!deltas || !deltas.length) {
        return;
      }
      var srcVertices = Math.floor(deltas.length / 3);
      // Whole destination vertices only: no copy and no write for a trailing
      // fragment that cannot hold a full stride.
      var dstVertices = Math.floor(values.length / stride);
      for (var v = 0; v < dstVertices; v++) {
        var d = indices ? indices[v] : v;
        if (!(d >= 0 && d < srcVertices)) {
          continue;
        }
        if (!copied[channel]) {
          // First writable corner only: targets that index nothing real never
          // allocate a copy and hand the input stream straight back.
          // Float32Array(source) copies the input values directly.
          values = streams[channel] = new Float32Array(values);
          copied[channel] = true;
        }
        var offset = v * stride;
        values[offset]     += weight * deltas[d * 3];
        values[offset + 1] += weight * deltas[d * 3 + 1];
        values[offset + 2] += weight * deltas[d * 3 + 2];
      }
    }
    for (var t = 0; t < targets.length; t++) {
      var weight = Array.isArray(weights) && weights[t] != null ? Number(weights[t]) : 0;
      if (!isFinite(weight) || weight === 0) {
        continue;
      }
      var source = targets[t];
      if (!source || typeof source !== "object") {
        continue;
      }
      foldChannel(0, source.POSITION, 3);
      if (streams.length > 1) {
        foldChannel(1, source.NORMAL, 3);
        foldChannel(2, source.TANGENT, 4);
      }
    }
    return streams;
  }

  function gltfReadPrimitiveAttribute(gltf, primitive, names, binaryBuffer) {
    var attrs = primitive && primitive.attributes ? primitive.attributes : {};
    for (var i = 0; i < names.length; i++) {
      var name = names[i];
      if (!Object.prototype.hasOwnProperty.call(attrs, name)) {
        continue;
      }
      var accessorIndex = attrs[name];
      var accessor = gltf.accessors && gltf.accessors[accessorIndex];
      if (!accessor) {
        return null;
      }
      return {
        accessor: accessor,
        values: gltfReadAccessor(gltf, accessorIndex, binaryBuffer),
      };
    }
    return null;
  }

  function gltfTransformPositions(positions, worldTransform) {
    var count = Math.floor(positions.length / 3);
    var transformed = new Float32Array(count * 3);
    for (var i = 0; i < count; i++) {
      var point = gltfTransformPoint(
        worldTransform,
        positions[i * 3],
        positions[i * 3 + 1],
        positions[i * 3 + 2]
      );
      transformed[i * 3] = point.x;
      transformed[i * 3 + 1] = point.y;
      transformed[i * 3 + 2] = point.z;
    }
    return transformed;
  }

  function gltfPositionsToLinePoints(positions) {
    var count = Math.floor(positions.length / 3);
    var points = new Array(count);
    for (var i = 0; i < count; i++) {
      points[i] = {
        x: positions[i * 3],
        y: positions[i * 3 + 1],
        z: positions[i * 3 + 2],
      };
    }
    return points;
  }

  function gltfLineSegments(mode, pointCount, indices) {
    var indexed = indices && indices.length;
    var total = indexed ? indices.length : pointCount;
    var order = [];
    for (var i = 0; i < total; i++) {
      order.push(indexed ? Math.floor(indices[i]) : i);
    }

    // LINES pairs corners two by two; LINE_STRIP/LINE_LOOP chain them.
    var step = mode === 1 ? 2 : 1;
    var segments = [];
    for (var s = 0; s + 1 < order.length; s += step) {
      segments.push([order[s], order[s + 1]]);
    }
    if (mode === 1) {
      return segments;
    }
    if (mode === 2 && order.length > 2) {
      segments.push([order[order.length - 1], order[0]]);
    }
    return segments;
  }

  function gltfColorComponent(value, componentType) {
    var n = Number(value) || 0;
    if (n > 1 && componentType === 5121) {
      n = n / 255;
    } else if (n > 1 && componentType === 5123) {
      n = n / 65535;
    } else if (n > 1 && componentType === 5125) {
      n = n / 4294967295;
    }
    return Math.max(0, Math.min(1, n));
  }

  function gltfPointColorBuffer(gltf, primitive, binaryBuffer, count) {
    var record = gltfReadPrimitiveAttribute(gltf, primitive, ["COLOR_0"], binaryBuffer);
    if (!record || !record.values || !record.values.length) {
      return null;
    }
    var componentCount = gltfAccessorTypeCount(record.accessor.type);
    if (componentCount < 3) {
      return null;
    }
    var componentType = record.accessor.componentType;
    var colors = new Float32Array(count * 4);
    for (var i = 0; i < count; i++) {
      var src = i * componentCount;
      colors[i * 4] = gltfColorComponent(record.values[src], componentType);
      colors[i * 4 + 1] = gltfColorComponent(record.values[src + 1], componentType);
      colors[i * 4 + 2] = gltfColorComponent(record.values[src + 2], componentType);
      colors[i * 4 + 3] = componentCount > 3
        ? gltfColorComponent(record.values[src + 3], componentType)
        : 1;
    }
    return colors;
  }

  function gltfPointSizeBuffer(gltf, primitive, binaryBuffer, count) {
    var record = gltfReadPrimitiveAttribute(gltf, primitive, [
      "_POINT_SIZE",
      "_POINTSIZE",
      "_SIZE",
      "POINT_SIZE",
      "POINTSIZE",
      "SIZE",
      "PSIZE",
    ], binaryBuffer);
    if (!record || !record.values || !record.values.length) {
      return null;
    }
    // Quantized sizes: a builder may store _POINT_SIZE as a normalized integer
    // accessor (half the bytes of float32) and carry the dequantization factor
    // in the primitive extras. The accessor decodes to 0..1 above, and the
    // factor restores source units. A float32 accessor omits the factor and
    // multiplies by 1.
    var scale = 1;
    var extras = primitive.extras;
    var extrasGroup = extras && (extras.gosx || extras.scene3d);
    if (extrasGroup && Number(extrasGroup.pointSizeScale) > 0) {
      scale = Number(extrasGroup.pointSizeScale);
    }
    var componentCount = gltfAccessorTypeCount(record.accessor.type);
    var sizes = new Float32Array(count);
    for (var i = 0; i < count; i++) {
      sizes[i] = Math.max(0, Number(record.values[i * componentCount]) || 0) * scale;
    }
    return sizes;
  }

  function gltfMergeScene3DExtraObject(target, extras) {
    if (!extras || typeof extras !== "object") {
      return;
    }
    var keys = Object.keys(extras);
    for (var i = 0; i < keys.length; i++) {
      var key = keys[i];
      if (key === "gosx" || key === "scene3d" || key === "scene3D") {
        continue;
      }
      target[key] = extras[key];
    }
  }

  function gltfCollectScene3DExtras(node, mesh, primitive) {
    var target = {};
    function mergeRecord(record) {
      var extras = record && record.extras;
      if (!extras || typeof extras !== "object") {
        return;
      }
      gltfMergeScene3DExtraObject(target, extras);
      gltfMergeScene3DExtraObject(target, extras.gosx);
      gltfMergeScene3DExtraObject(target, extras.scene3d);
      gltfMergeScene3DExtraObject(target, extras.scene3D);
    }
    mergeRecord(node);
    mergeRecord(mesh);
    mergeRecord(primitive);
    return target;
  }

  function gltfApplyScene3DExtras(target, extras, allowedKeys) {
    if (!extras || typeof extras !== "object") {
      return target;
    }
    for (var i = 0; i < allowedKeys.length; i++) {
      var key = allowedKeys[i];
      if (Object.prototype.hasOwnProperty.call(extras, key)) {
        target[key] = extras[key];
      }
    }
    return target;
  }

  var GLTF_POINT_EXTRA_KEYS = [
    "id", "material", "color", "style", "size", "opacity", "blendMode",
    "depthWrite", "attenuation", "x", "y", "z", "rotationX", "rotationY",
    "rotationZ", "spinX", "spinY", "spinZ", "minPixelSize", "maxPixelSize",
    "transition", "inState", "outState", "live",
  ];

  var GLTF_OBJECT_EXTRA_KEYS = [
    "id", "material", "materialKind", "color", "texture", "opacity",
    "emissive", "roughness", "metalness", "blendMode", "renderPass",
    "wireframe", "depthWrite", "x", "y", "z", "rotationX", "rotationY",
    "rotationZ", "spinX", "spinY", "spinZ", "transition", "inState",
    "outState", "live", "static", "pickable", "castShadow", "receiveShadow",
  ];

  // ---------------------------------------------------------------------------
  // Mesh primitive extraction
  // ---------------------------------------------------------------------------

  // Renormalize a VEC3 stream in place. KHR_mesh_quantization stores a normal
  // as a normalized BYTE or SHORT, so the decoded vector is up to half a
  // lattice step short of unit length. The world-transform path renormalizes
  // already, but the skinned path keeps the model-space vector, so do it here.
  function gltfRenormalizeVec3(values) {
    for (var i = 0; i + 2 < values.length; i += 3) {
      var x = values[i], y = values[i + 1], z = values[i + 2];
      var length = Math.sqrt(x * x + y * y + z * z);
      if (length > 1e-8) {
        values[i] = x / length;
        values[i + 1] = y / length;
        values[i + 2] = z / length;
      }
    }
    return values;
  }

  // Hand back an owned Float32Array unless the stream already is one.
  function gltfToFloat32Array(values) {
    return values && !(values instanceof Float32Array)
      ? new Float32Array(values)
      : values;
  }

  // True when any glTF animation channel drives the morph weights of this
  // node. Raw glTF graph field: channel.target.path per glTF 2.0. Animated
  // morphs are the only reason to retain per-primitive morph metadata past
  // load time; static morph assets keep the load-time fold and allocate
  // nothing extra.
  function gltfNodeHasWeightAnimation(gltf, nodeIndex) {
    var animations = gltf && gltf.animations;
    if (!animations || !animations.length) {
      return false;
    }
    for (var a = 0; a < animations.length; a++) {
      var channels = animations[a] && animations[a].channels;
      if (!channels || !channels.length) {
        continue;
      }
      for (var c = 0; c < channels.length; c++) {
        var target = channels[c] && channels[c].target;
        if (target && target.node === nodeIndex && target.path === "weights") {
          return true;
        }
      }
    }
    return false;
  }

  // Node indices carrying a DIRECT rigid TRS animation channel (translation,
  // rotation, scale). Morph "weights" channels are excluded — they are
  // handled by the existing morph metadata path, and glTF 2.0 defines no
  // matrix animation channel. Ancestor propagation happens during the node
  // walk below, so a static child under an animated parent inherits the
  // flag without any per-primitive graph scan.
  function gltfDirectTRSNodes(gltf) {
    var animated = new Set();
    var animations = gltf && gltf.animations;
    if (!animations || !animations.length) {
      return animated;
    }
    for (var a = 0; a < animations.length; a++) {
      var channels = animations[a] && animations[a].channels;
      if (!channels || !channels.length) {
        continue;
      }
      for (var c = 0; c < channels.length; c++) {
        var target = channels[c] && channels[c].target;
        if (target && target.node != null && target.path !== "weights") {
          animated.add(target.node);
        }
      }
    }
    return animated;
  }

  // Materialize immutable primitive-local morph inputs for animated morphs:
  // base streams copied out of the (possibly GLB-backed) accessor views and
  // target deltas pre-expanded through the primitive's index map, so the
  // per-frame fold never re-reads accessors, never walks indices, and never
  // retains the GLB binary or the glTF graph. Channel rules mirror the
  // static fold exactly: deltas are VEC3 displacements, missing or short
  // accessors drop to absent channels, out-of-range delta indices expand to
  // zero, and defaults record the validated weights the static fold applied.
  function gltfBuildAnimatedMorphMetadata(gltf, primitive, binaryBuffer, indices, positions, normals, tangents, authoredWeights, nodeIndex, node) {
    var targets = primitive.targets || [];
    var vertexCount = Math.floor(positions.length / 3);
    var meta = {
      nodeIndex: nodeIndex,
      vertexCount: vertexCount,
      // Authored node TRS retained so a partially animated node rebuilds its
      // local matrix with the same per-component fallbacks
      // sceneAnimBuildNodeTransforms uses (anim component else authored).
      nodeTranslation: node && node.translation ? node.translation : null,
      nodeRotation: node && node.rotation ? node.rotation : null,
      nodeScale: node && node.scale ? node.scale : null,
      instanced: false,
      defaults: [],
      basePositions: new Float32Array(positions),
      baseNormals: normals ? new Float32Array(normals) : null,
      baseTangents: tangents ? new Float32Array(tangents) : null,
      baseUVs: null,
      targetPositions: [],
      targetNormals: [],
      targetTangents: [],
    };
    function readTargetDeltas(accessorIndex) {
      if (accessorIndex == null || !gltf.accessors || !gltf.accessors[accessorIndex]) {
        return null;
      }
      var deltas = gltfReadAccessor(gltf, accessorIndex, binaryBuffer);
      if (!deltas || !deltas.length) {
        return null;
      }
      var srcVertices = Math.floor(deltas.length / 3);
      var out = new Float32Array(vertexCount * 3);
      for (var v = 0; v < vertexCount; v++) {
        var d = indices ? indices[v] : v;
        if (!(d >= 0 && d < srcVertices)) {
          continue;
        }
        out[v * 3] = deltas[d * 3];
        out[v * 3 + 1] = deltas[d * 3 + 1];
        out[v * 3 + 2] = deltas[d * 3 + 2];
      }
      return out;
    }
    for (var t = 0; t < targets.length; t++) {
      var source = targets[t];
      if (!source || typeof source !== "object") {
        meta.defaults.push(0);
        meta.targetPositions.push(null);
        meta.targetNormals.push(null);
        meta.targetTangents.push(null);
        continue;
      }
      // Validate exactly like the static fold: finite numbers only — no
      // Number() coercion of strings/booleans/empty values.
      var raw = Array.isArray(authoredWeights) ? authoredWeights[t] : null;
      meta.defaults.push(typeof raw === "number" && isFinite(raw) ? raw : 0);
      meta.targetPositions.push(readTargetDeltas(source.POSITION));
      meta.targetNormals.push(source.NORMAL != null ? readTargetDeltas(source.NORMAL) : null);
      meta.targetTangents.push(source.TANGENT != null ? readTargetDeltas(source.TANGENT) : null);
    }
    return meta;
  }

  // Matrix snapshot helpers: transform-only changes (live model move or
  // animated node TRS) must re-apply even when the sampled weights did not
  // change, so the apply tracks the last node/model matrices per entry.
  function gltfMatrixChanged(last, current) {
    if (!last && !current) {
      return false;
    }
    if (!last || !current) {
      return true;
    }
    for (var i = 0; i < 16; i++) {
      if (last[i] !== current[i]) {
        return true;
      }
    }
    return false;
  }

  function gltfCopyMat4(m) {
    var out = new Float32Array(16);
    if (m) {
      for (var i = 0; i < 16 && i < m.length; i++) {
        out[i] = m[i];
      }
    }
    return out;
  }
  function gltfExtractMeshPrimitive(gltf, primitive, binaryBuffer, uvTransform, morphWeights, animatedMorph, nodeIndex, node) {
    // One named-attribute read: absent names hand back null exactly like the
    // inline guards they replace.
    function attrValues(name) {
      return primitive.attributes[name] != null
        ? gltfReadAccessor(gltf, primitive.attributes[name], binaryBuffer)
        : null;
    }
    var positions = gltfReadAccessor(gltf, primitive.attributes.POSITION, binaryBuffer);

    var normals = attrValues("NORMAL");
    // A normalized accessor already handed back a fresh Float32Array, so this
    // never writes through a view over the shared GLB buffer.
    if (normals && gltf.accessors[primitive.attributes.NORMAL].normalized) {
      normals = gltfRenormalizeVec3(normals);
    }

    var uvs = attrValues("TEXCOORD_0");

    // glTF 2.0: when NORMAL is absent, normals are calculated (flat) and both
    // authored tangents and morph TANGENT displacement must be ignored — the
    // tangent basis is recomputed from the final folded surface below.
    var tangentsRaw = normals ? attrValues("TANGENT") : null;

    var joints = gltfToFloat32Array(attrValues("JOINTS_0"));

    var weights = gltfToFloat32Array(attrValues("WEIGHTS_0"));

    var indices = primitive.indices != null
      ? gltfReadAccessor(gltf, primitive.indices, binaryBuffer)
      : null;

    // Expand indexed geometry to flat triangle arrays.
    if (indices) {
      var expanded = gltfExpandIndexed(
        positions,
        normals || positions, // placeholder; fallback normals come after folding
        uvs || gltfGenerateDefaultUVs(positions.length / 3),
        tangentsRaw,
        joints,
        weights,
        indices
      );
      positions = expanded.positions;
      if (normals) {
        normals = expanded.normals;
      }
      uvs = expanded.uvs;
      tangentsRaw = expanded.tangents;
      joints = expanded.joints;
      weights = expanded.weights;
    } else if (!uvs) {
      // Unindexed geometry pairs vertex v directly. The base streams stay as
      // the accessor handed them — the fold copies each channel lazily, only
      // when a morph weight actually writes it — and the fallback streams are
      // fresh either way.
      uvs = gltfGenerateDefaultUVs(positions.length / 3);
    }

    // Animated morphs retain their primitive-local inputs (base streams and
    // index-expanded target deltas) so the per-frame apply can re-fold from a
    // pristine base. Snapshot BEFORE the static fold: the fold's lazy copies
    // would otherwise become the "base" and double-apply on every frame.
    var morphMeta = animatedMorph && primitive.targets && primitive.targets.length && positions
      ? gltfBuildAnimatedMorphMetadata(
          gltf, primitive, binaryBuffer, indices,
          positions, normals, tangentsRaw, morphWeights, nodeIndex, node)
      : null;

    // Fold static morph-target deltas right here, once at load time: deltas
    // land in PRIMITIVE-LOCAL space before any node/world transform or
    // skinning (Khronos glTF 2.0), and folding before UV baking and
    // fallback-tangent generation keeps computed tangents on the morphed
    // surface. The fold copies a stream only when a weight actually writes
    // it, so a no-op morph hands the input views straight back and allocates
    // nothing.
    // With no authored NORMAL the fold sees null normals, so NORMAL deltas
    // are skipped here rather than perturbing normals that no longer
    // describe the folded surface.
    var morphedStreams = gltfFoldMorphTargets(
      gltf, primitive, binaryBuffer, indices,
      positions, normals, tangentsRaw, morphWeights);
    if (morphedStreams) {
      positions = morphedStreams[0];
      normals = morphedStreams[1] || normals;
      tangentsRaw = morphedStreams[2] || tangentsRaw;
    }

    // Fallback flat normals are generated here — AFTER the fold — from the
    // final primitive-local positions: generating them before the fold left
    // POSITION-only morph targets deforming the triangle under normals that
    // described the old surface and feeding that stale basis into tangent
    // generation. Generated exactly once, only when NORMAL is absent, so
    // assets with authored normals and static assets with no effective morph
    // pay nothing extra.
    if (!normals) {
      normals = gltfGenerateFlatNormals(positions);
    }

    // Bake KHR_texture_transform into the UVs before tangents are computed, so
    // the tangent basis matches the UVs the shader samples with. Copy first: a
    // tightly packed accessor hands back a view over the shared GLB buffer, and
    // two primitives can read the same accessor.
    if (uvTransform) {
      uvs = gltfApplyTextureTransform(new Float32Array(uvs), uvTransform);
    }

    // Compute tangents if not provided by the asset.
    var tangents = tangentsRaw || gltfComputeTangents(positions, normals, uvs);

    if (morphMeta && !tangentsRaw) {
      // Computed tangents are re-derived per pose change from the same UVs,
      // so keep the post-texture-transform UVs the load-time pass used.
      morphMeta.baseUVs = new Float32Array(uvs);
    }

    return {
      positions: positions,
      normals: normals,
      uvs: uvs,
      tangents: tangents,
      joints: joints,
      weights: weights,
      count: positions.length / 3,
      morphMeta: morphMeta,
    };
  }

  // ---------------------------------------------------------------------------
  // Animated morph weights — per-frame fold driven by the motion mixers
  // ---------------------------------------------------------------------------

  // Resolve the effective weight vector for one morph meta against the
  // decoded mixer pose. Finite numbers only (no coercion). Returns null when
  // the effective vector already matches lastWeights.
  function gltfMorphEffectiveWeights(meta, animatedWeights, lastWeights) {
    var defaults = meta.defaults;
    var count = defaults.length;
    if (!count) {
      return null;
    }
    var pose = animatedWeights && typeof animatedWeights.get === "function"
      ? animatedWeights.get(meta.nodeIndex)
      : null;
    var values = pose && pose.weights != null && typeof pose.weights.length === "number"
      ? pose.weights
      : null;
    var effective = new Array(count);
    var changed = !lastWeights || lastWeights.length !== count;
    for (var t = 0; t < count; t++) {
      var raw = values && t < values.length ? values[t] : NaN;
      if (typeof raw !== "number" || !isFinite(raw)) {
        raw = defaults[t];
      }
      effective[t] = raw;
      if (!changed && raw !== lastWeights[t]) {
        changed = true;
      }
    }
    return changed ? effective : null;
  }

  // Fold the weighted target deltas into FRESH primitive-local streams from
  // the immutable base — repeated applications can never accumulate.
  function gltfFoldAnimatedMorphStreams(meta, weights) {
    var positions = new Float32Array(meta.basePositions);
    var normals = meta.baseNormals ? new Float32Array(meta.baseNormals) : null;
    var tangents = meta.baseTangents ? new Float32Array(meta.baseTangents) : null;
    for (var t = 0; t < weights.length; t++) {
      var weight = weights[t];
      if (!isFinite(weight) || weight === 0) {
        continue;
      }
      var target = meta.targetPositions[t];
      if (target) {
        for (var i = 0; i < positions.length; i++) {
          positions[i] += weight * target[i];
        }
      }
      if (normals) {
        target = meta.targetNormals[t];
        if (target) {
          for (var n = 0; n < normals.length; n++) {
            normals[n] += weight * target[n];
          }
        }
      }
      if (tangents) {
        target = meta.targetTangents[t];
        if (target) {
          for (var v = 0; v < meta.vertexCount; v++) {
            tangents[v * 4] += weight * target[v * 3];
            tangents[v * 4 + 1] += weight * target[v * 3 + 1];
            tangents[v * 4 + 2] += weight * target[v * 3 + 2];
          }
        }
      }
    }
    if (!normals) {
      normals = gltfGenerateFlatNormals(positions);
    }
    if (!tangents) {
      tangents = gltfComputeTangents(positions, normals, meta.baseUVs || gltfGenerateDefaultUVs(meta.vertexCount));
    }
    return { positions: positions, normals: normals, tangents: tangents };
  }

  // Transform folded primitive-local streams by one matrix, mirroring the
  // load-time/static model-transform path (sceneApplyStaticModel
  // ObjectTransform): positions through the full transform, normals through
  // the inverse-transpose 3x3 (correct under non-uniform scale), tangent
  // xyz as directions through the linear 3x3 with renormalization, tangent
  // w preserved. Uses only this chunk's glTF matrix helpers — no
  // sceneModelTransform*/sceneNormalizeDirection cross-chunk calls.
  function gltfTransformMorphedStreams(streams, worldTransform) {
    var outPositions = new Float32Array(streams.positions.length);
    for (var p = 0; p < streams.positions.length; p += 3) {
      var point = gltfTransformPoint(worldTransform, streams.positions[p], streams.positions[p + 1], streams.positions[p + 2]);
      outPositions[p] = point.x;
      outPositions[p + 1] = point.y;
      outPositions[p + 2] = point.z;
    }
    var normalMatrix = gltfNormalMatrix(worldTransform);
    var outNormals = new Float32Array(streams.normals.length);
    for (var n = 0; n < streams.normals.length; n += 3) {
      var normal = gltfTransformNormal(normalMatrix, streams.normals[n], streams.normals[n + 1], streams.normals[n + 2]);
      outNormals[n] = normal.x;
      outNormals[n + 1] = normal.y;
      outNormals[n + 2] = normal.z;
    }
    var outTangents = new Float32Array(streams.tangents.length);
    for (var t = 0; t < streams.tangents.length; t += 4) {
      var tangent = gltfTransformDirection(worldTransform, streams.tangents[t], streams.tangents[t + 1], streams.tangents[t + 2]);
      var tangentLen = Math.sqrt(tangent.x * tangent.x + tangent.y * tangent.y + tangent.z * tangent.z);
      if (tangentLen > 1e-8) {
        tangent.x /= tangentLen;
        tangent.y /= tangentLen;
        tangent.z /= tangentLen;
      }
      outTangents[t] = tangent.x;
      outTangents[t + 1] = tangent.y;
      outTangents[t + 2] = tangent.z;
      var w = streams.tangents[t + 3];
      outTangents[t + 3] = typeof w === "number" && isFinite(w) ? w : 1;
    }
    return { positions: outPositions, normals: outNormals, tangents: outTangents };
  }

  // Cache keys the bounds/snapshot layers attach to vertices objects.
  var GLTF_MORPH_CACHE_KEYS = ["_skinnedLocalBounds", "_localBounds", "_bounds"];

  function gltfDropVertexCaches(vertices) {
    for (var i = 0; i < GLTF_MORPH_CACHE_KEYS.length; i++) {
      if (Object.prototype.hasOwnProperty.call(vertices, GLTF_MORPH_CACHE_KEYS[i])) {
        delete vertices[GLTF_MORPH_CACHE_KEYS[i]];
      }
    }
  }

  // Number coercion with fallback for live model fields riding node-anim
  // entries.
  function gltfAnimNumber(value, fallback) {
    var n = typeof value === "number" ? value : Number(value);
    return isFinite(n) ? n : fallback;
  }

  // Per-frame rigid node TRS playback, published as
  // window.__gosx_scene3d_gltf_api.applyNodeAnimPose. Entries are the
  // per-instance live records the mount layer builds (_nodeAnimLive): each
  // carries immutable pristine primitive-local inputs (retained at load,
  // never the baked world-transform outputs), the target node index, the
  // authored instance-local matrix for instanced copies, and the live
  // vertices/points/lines object it owns. nodeTransforms is the model-local
  // node map from sceneAnimBuildNodeTransforms(nodes, pose, null,
  // rootNodes); it always contains every node, so a stopped or reset mixer
  // yields the authored pose. The model/root transform is applied exactly
  // once (mesh entries via entry.modelMatrix refreshed from the record's
  // live root transform each tick; points/lines mirror the mount's split
  // scale/rotate/translate instantiation semantics using the captured
  // pre-model base fields plus the live model values). No cached asset
  // input is ever mutated: every changed frame writes fresh output arrays,
  // so point/line render caches receive genuinely new positions and
  // re-upload. Nothing is ever reconstructed by inverting a baked transform
  // — the compose is always animated node-world * authored instance-local *
  // primitive-local, plus the model transform once — so singular or
  // zero-scale authored transforms can never block a later valid pose.
  function gltfApplyNodeAnimPose(entries, nodeTransforms) {
    if (!Array.isArray(entries)) {
      return;
    }
    for (var i = 0; i < entries.length; i++) {
      var entry = entries[i];
      if (!entry) {
        continue;
      }
      var anim = nodeTransforms && typeof nodeTransforms.get === "function"
        ? nodeTransforms.get(entry.nodeIndex)
        : null;
      var instanceMatrix = entry.instanceMatrix || null;
      // Animated node world * authored instance-local. The entry.nodeMatrix
      // fallback is the authored world transform (already containing the
      // instance offset for instanced copies), so it is never multiplied a
      // second time.
      var nodeMatrix;
      if (anim) {
        nodeMatrix = instanceMatrix ? sceneMat4Multiply(anim, instanceMatrix) : anim;
      } else {
        nodeMatrix = entry.nodeMatrix || null;
      }
      if (entry.kind === "mesh" && entry.meta && entry.vertices) {
        var model = entry.modelMatrix || null;
        var nodeChanged = gltfMatrixChanged(entry.lastNodeMatrix, nodeMatrix);
        var modelChanged = gltfMatrixChanged(entry.lastModelMatrix, model);
        if (!nodeChanged && !modelChanged) {
          continue;
        }
        var meta = entry.meta;
        var base = {
          positions: meta.basePositions,
          normals: meta.baseNormals,
          tangents: meta.baseTangents,
        };
        // gltfTransformMorphedStreams: positions through the full transform,
        // normals through the inverse-transpose 3x3 (correct under
        // non-uniform scale), tangent xyz through the linear 3x3 with
        // renormalization and tangent w preserved. Skinned and
        // morph-animated primitives never register here, so their outputs
        // are never rigid-transformed a second time.
        var local = nodeMatrix ? gltfTransformMorphedStreams(base, nodeMatrix) : base;
        var finalStreams = model ? gltfTransformMorphedStreams(local, model) : local;
        entry.vertices.positions = finalStreams.positions;
        entry.vertices.normals = finalStreams.normals;
        entry.vertices.tangents = finalStreams.tangents;
        if (entry.modelLocalVertices && entry.modelLocalVertices.positions) {
          entry.modelLocalVertices.positions = local.positions;
          entry.modelLocalVertices.normals = local.normals;
          entry.modelLocalVertices.tangents = local.tangents;
          entry.modelLocalVertices.count = meta.vertexCount;
        }
        gltfDropVertexCaches(entry.vertices);
        entry.lastNodeMatrix = nodeMatrix ? gltfCopyMat4(nodeMatrix) : null;
        entry.lastModelMatrix = model ? gltfCopyMat4(model) : null;
      } else if ((entry.kind === "points" || entry.kind === "lines") && entry.object) {
        var source = entry.basePositions || null;
        var target = entry.object;
        if (source && source.length >= 3 && nodeMatrix) {
          var modelScaleX = gltfAnimNumber(entry.model && entry.model.scaleX, 1);
          var modelScaleY = gltfAnimNumber(entry.model && entry.model.scaleY, 1);
          var modelScaleZ = gltfAnimNumber(entry.model && entry.model.scaleZ, 1);
          var count3 = Math.floor(source.length / 3) * 3;
          if (entry.kind === "points") {
            var outPositions = new Float32Array(count3);
            for (var v = 0; v < count3; v += 3) {
              var pt = gltfTransformPoint(nodeMatrix, source[v], source[v + 1], source[v + 2]);
              outPositions[v] = pt.x * modelScaleX;
              outPositions[v + 1] = pt.y * modelScaleY;
              outPositions[v + 2] = pt.z * modelScaleZ;
            }
            // Fresh array identity every rebuilt frame: the static point VBO
            // cache keys on the typed array, so this forces a real upload.
            target.positions = outPositions;
            target._cachedPos = outPositions;
          } else {
            var linePoints = new Array(count3 / 3);
            for (var lv = 0; lv < count3; lv += 3) {
              var lp = gltfTransformPoint(nodeMatrix, source[lv], source[lv + 1], source[lv + 2]);
              linePoints[lv / 3] = { x: lp.x * modelScaleX, y: lp.y * modelScaleY, z: lp.z * modelScaleZ };
            }
            target.points = linePoints;
          }
        }
        // Model translate/rotate split, mirroring the mount instantiation:
        // positions above carry the model scale only and rotation rides the
        // object fields. The base origin is NOT simply additive with the
        // model translation: the mount runs sceneModelTransformPoint on the
        // captured base (scale, then rotate, then translate). gltf.ts
        // cannot call that mount-local helper, but entry.modelMatrix is the
        // live model root transform, so transforming the base origin
        // through it reproduces the exact same semantics self-contained.
        // No double application: the per-vertex streams above never see
        // the model translation or rotation.
        var basePose = entry.modelBase || null;
        var liveModel = entry.model || null;
        var poseModelMatrix = entry.modelMatrix || null;
        if (basePose && poseModelMatrix) {
          var origin = gltfTransformPoint(
            poseModelMatrix,
            gltfAnimNumber(basePose.x, 0),
            gltfAnimNumber(basePose.y, 0),
            gltfAnimNumber(basePose.z, 0)
          );
          target.x = origin.x;
          target.y = origin.y;
          target.z = origin.z;
        } else {
          target.x = (basePose ? gltfAnimNumber(basePose.x, 0) : 0) + gltfAnimNumber(liveModel && liveModel.x, 0);
          target.y = (basePose ? gltfAnimNumber(basePose.y, 0) : 0) + gltfAnimNumber(liveModel && liveModel.y, 0);
          target.z = (basePose ? gltfAnimNumber(basePose.z, 0) : 0) + gltfAnimNumber(liveModel && liveModel.z, 0);
        }
        target.rotationX = (basePose ? gltfAnimNumber(basePose.rotationX, 0) : 0) + gltfAnimNumber(liveModel && liveModel.rotationX, 0);
        target.rotationY = (basePose ? gltfAnimNumber(basePose.rotationY, 0) : 0) + gltfAnimNumber(liveModel && liveModel.rotationY, 0);
        target.rotationZ = (basePose ? gltfAnimNumber(basePose.rotationZ, 0) : 0) + gltfAnimNumber(liveModel && liveModel.rotationZ, 0);
      }
    }
  }

  // Published per-frame entry point (window.__gosx_scene3d_gltf_api.
  // applyMorphPose). nodeTransforms (optional) is the model-local node map
  // from sceneAnimBuildNodeTransforms(nodes, pose, null, rootNodes): animated
  // node TRS WITHOUT the model/root transform, so the model transform below
  // is applied exactly once and skinning/instancing are unaffected.
  function gltfApplyAnimatedMorphPose(entries, animatedWeights, nodeTransforms) {
    if (!Array.isArray(entries)) {
      return;
    }
    for (var i = 0; i < entries.length; i++) {
      var entry = entries[i];
      if (!entry || !entry.meta || !entry.vertices) {
        continue;
      }
      var meta = entry.meta;
      var effective = gltfMorphEffectiveWeights(meta, animatedWeights, entry.lastWeights);
      if (entry.skinned) {
        // Skinned instances stay primitive-local: node and model transforms
        // fold in at skin time through the joint matrices.
        if (!effective) {
          continue;
        }
        var skinnedFold = gltfFoldAnimatedMorphStreams(meta, effective);
        entry.vertices.positions = skinnedFold.positions;
        entry.vertices.normals = skinnedFold.normals;
        entry.vertices.tangents = skinnedFold.tangents;
        gltfDropVertexCaches(entry.vertices);
        entry.lastWeights = effective;
        entry.lastFolded = skinnedFold;
        continue;
      }
      // Node matrix: the animated model-local matrix when available, else
      // the authored asset matrix. Instanced primitives compose the animated
      // node world with their authored instance-local matrix; the baked
      // entry.nodeMatrix already contains that offset, never applied twice.
      var nodeMatrix = entry.nodeMatrix || null;
      var instanceMatrix = meta.instanceMatrix || null;
      var anim = null;
      if (nodeTransforms && typeof nodeTransforms.get === "function") {
        anim = nodeTransforms.get(meta.nodeIndex);
      } else if (!nodeTransforms && animatedWeights && typeof animatedWeights.get === "function") {
        // Bare-VM fallback (no mount): rebuild node-local TRS from the pose
        // with the same per-component fallbacks buildNodeTransforms uses.
        var pose = animatedWeights.get(meta.nodeIndex);
        if (pose && (pose.translation != null || pose.position != null || pose.rotation != null || pose.scale != null)) {
          anim = sceneTRSToMat4(
            (pose.translation || pose.position || meta.nodeTranslation || [0, 0, 0]),
            (pose.rotation || meta.nodeRotation || [0, 0, 0, 1]),
            (pose.scale || meta.nodeScale || [1, 1, 1])
          );
        }
      }
      if (anim && (!meta.instanced || instanceMatrix)) {
        nodeMatrix = instanceMatrix
          ? sceneMat4Multiply(anim, instanceMatrix)
          : anim;
      }
      var modelMatrix = entry.modelMatrix || null;
      var nodeChanged = gltfMatrixChanged(entry.lastNodeMatrix, nodeMatrix);
      var modelChanged = gltfMatrixChanged(entry.lastModelMatrix, modelMatrix);
      if (!effective && !nodeChanged && !modelChanged) {
        continue;
      }
      // Re-fold only when weights changed; transform-only changes reuse the
      // cached primitive-local fold.
      var folded = effective
        ? gltfFoldAnimatedMorphStreams(meta, effective)
        : (entry.lastFolded || gltfFoldAnimatedMorphStreams(meta, entry.lastWeights || meta.defaults));
      entry.lastFolded = folded;
      if (effective) {
        entry.lastWeights = effective;
      }
      // Node stage → model-local asset-space geometry (the _modelLocalVertices
      // contract: node transform INCLUDED). Model stage → world, once.
      var local = nodeMatrix ? gltfTransformMorphedStreams(folded, nodeMatrix) : folded;
      var finalStreams = modelMatrix ? gltfTransformMorphedStreams(local, modelMatrix) : local;
      entry.vertices.positions = finalStreams.positions;
      entry.vertices.normals = finalStreams.normals;
      entry.vertices.tangents = finalStreams.tangents;
      if (entry.modelLocalVertices && entry.modelLocalVertices.positions) {
        entry.modelLocalVertices.positions = local.positions;
        entry.modelLocalVertices.normals = local.normals;
        entry.modelLocalVertices.tangents = local.tangents;
        entry.modelLocalVertices.count = meta.vertexCount;
      }
      gltfDropVertexCaches(entry.vertices);
      entry.lastNodeMatrix = nodeMatrix ? gltfCopyMat4(nodeMatrix) : null;
      entry.lastModelMatrix = modelMatrix ? gltfCopyMat4(modelMatrix) : null;
    }
  }

  // ---------------------------------------------------------------------------
  // 4x4 matrix helpers — delegates to shared functions in 11-scene-math.ts
  // (SCENE_IDENTITY_MAT4, sceneMat4Multiply, sceneTRSToMat4)
  // ---------------------------------------------------------------------------

  // Build a 4x4 matrix from glTF node TRS or raw matrix.
  function gltfNodeTransform(node) {
    if (node.matrix) {
      return new Float32Array(node.matrix);
    }

    var t = node.translation || [0, 0, 0];
    var r = node.rotation    || [0, 0, 0, 1];
    var s = node.scale       || [1, 1, 1];

    return sceneTRSToMat4(t, r, s);
  }

  // Transform a vec3 position by a 4x4 matrix (w=1 homogeneous).
  function gltfTransformPoint(m, x, y, z) {
    return {
      x: m[0] * x + m[4] * y + m[8]  * z + m[12],
      y: m[1] * x + m[5] * y + m[9]  * z + m[13],
      z: m[2] * x + m[6] * y + m[10] * z + m[14],
    };
  }

  // Transform a vec3 direction by upper-left 3x3 of a 4x4 matrix.
  function gltfTransformDirection(m, x, y, z) {
    return {
      x: m[0] * x + m[4] * y + m[8]  * z,
      y: m[1] * x + m[5] * y + m[9]  * z,
      z: m[2] * x + m[6] * y + m[10] * z,
    };
  }

  // Compute the 3x3 normal matrix (inverse-transpose of upper-left 3x3).
  // For uniform-scale transforms, the upper-left 3x3 itself works, but
  // for non-uniform scale we need the proper inverse-transpose.
  function gltfNormalMatrix(m) {
    var a00 = m[0], a01 = m[1], a02 = m[2];
    var a10 = m[4], a11 = m[5], a12 = m[6];
    var a20 = m[8], a21 = m[9], a22 = m[10];

    var det = a00 * (a11 * a22 - a12 * a21)
            - a01 * (a10 * a22 - a12 * a20)
            + a02 * (a10 * a21 - a11 * a20);

    if (Math.abs(det) < 1e-10) {
      // Degenerate — return identity 3x3 as fallback.
      return [1, 0, 0, 0, 1, 0, 0, 0, 1];
    }

    var invDet = 1.0 / det;

    // Cofactor matrix (already transposed for inverse-transpose).
    return [
      (a11 * a22 - a12 * a21) * invDet,
      (a12 * a20 - a10 * a22) * invDet,
      (a10 * a21 - a11 * a20) * invDet,
      (a02 * a21 - a01 * a22) * invDet,
      (a00 * a22 - a02 * a20) * invDet,
      (a01 * a20 - a00 * a21) * invDet,
      (a01 * a12 - a02 * a11) * invDet,
      (a02 * a10 - a00 * a12) * invDet,
      (a00 * a11 - a01 * a10) * invDet,
    ];
  }

  function gltfTransformNormal(nm, x, y, z) {
    var rx = nm[0] * x + nm[3] * y + nm[6] * z;
    var ry = nm[1] * x + nm[4] * y + nm[7] * z;
    var rz = nm[2] * x + nm[5] * y + nm[8] * z;
    var len = Math.sqrt(rx * rx + ry * ry + rz * rz);
    if (len > 1e-8) { rx /= len; ry /= len; rz /= len; }
    return { x: rx, y: ry, z: rz };
  }

  // ---------------------------------------------------------------------------
  // PBR material extraction
  // ---------------------------------------------------------------------------

  function gltfLinearToSRGB(value) {
    value = Math.max(0, Math.min(1, value || 0));
    return value <= 0.0031308
      ? value * 12.92
      : 1.055 * Math.pow(value, 1 / 2.4) - 0.055;
  }

  function gltfBaseColorToHex(factor) {
    var r = Math.round(gltfLinearToSRGB(factor[0]) * 255);
    var g = Math.round(gltfLinearToSRGB(factor[1]) * 255);
    var b = Math.round(gltfLinearToSRGB(factor[2]) * 255);
    return "#" +
      (r < 16 ? "0" : "") + r.toString(16) +
      (g < 16 ? "0" : "") + g.toString(16) +
      (b < 16 ? "0" : "") + b.toString(16);
  }

  function gltfDefaultPBRMaterial() {
    return {
      kind: "standard",
      color: "#cccccc",
      roughness: 1.0,
      metalness: 0.0,
      opacity: 1.0,
      emissive: 0,
      texture: "",
      normalMap: "",
      roughnessMap: "",
      metalnessMap: "",
      occlusionMap: "",
      emissiveMap: "",
      alphaMode: "OPAQUE",
      doubleSided: false,
    };
  }

  function gltfTextureDescriptor(uri, role, colorSpace, channels) {
    uri = typeof uri === "string" ? uri.trim() : "";
    if (!uri) {
      return null;
    }
    return {
      uri: uri,
      role: role,
      colorSpace: colorSpace,
      channels: channels,
      view: "2d",
    };
  }

  function gltfMaterialTextureDescriptors(baseColor, normal, roughness, metalness, occlusion, emissive) {
    var descriptors = {};
    function add(name, descriptor) {
      if (descriptor) {
        descriptors[name] = descriptor;
      }
    }
    add("baseColor", gltfTextureDescriptor(baseColor, "base-color", "srgb", "rgba"));
    add("normal", gltfTextureDescriptor(normal, "normal", "linear", "rgb"));
    add("roughness", gltfTextureDescriptor(roughness, "roughness", "linear", "g"));
    add("metalness", gltfTextureDescriptor(metalness, "metalness", "linear", "b"));
    add("occlusion", gltfTextureDescriptor(occlusion, "ambient-occlusion", "linear", "r"));
    add("emissive", gltfTextureDescriptor(emissive, "emissive", "srgb", "rgb"));
    return descriptors;
  }

  function gltfResolveTexture(gltf, textureInfo, binaryBuffer) {
    if (!textureInfo || textureInfo.index == null) {
      return "";
    }
    var textures = gltf.textures;
    if (!textures || textureInfo.index >= textures.length) {
      return "";
    }
    var texture = textures[textureInfo.index];
    if (!texture) {
      return "";
    }
    if (texture.source == null) {
      // KHR_texture_basisu moves the image reference into the extension and
      // points it at a KTX2 container. The loader has no Basis transcoder, so
      // report the missing map and keep the mesh. Other maps still load.
      if (gltfExtension(texture, "KHR_texture_basisu")) {
        console.warn("[gosx] glTF texture uses KHR_texture_basisu (KTX2); this loader cannot transcode it. Rendering without that map.");
      }
      return "";
    }
    var images = gltf.images;
    if (!images || texture.source >= images.length) {
      return "";
    }
    var image = images[texture.source];
    if (!image) {
      return "";
    }

    // External URI or data URI.
    if (image.uri) {
      return image.uri;
    }

    // Embedded image: create a blob URL from the buffer view.
    if (image.bufferView != null && binaryBuffer) {
      return gltfCreateBlobURLFromBufferView(gltf, image, binaryBuffer);
    }

    return "";
  }

  function gltfCreateBlobURLFromBufferView(gltf, image, binaryBuffer) {
    var bufferView = gltf.bufferViews[image.bufferView];
    var byteOffset = bufferView.byteOffset || 0;
    var byteLength = bufferView.byteLength;
    var mimeType = image.mimeType || "application/octet-stream";
    var slice = binaryBuffer.slice(byteOffset, byteOffset + byteLength);
    var blob = new Blob([slice], { type: mimeType });
    return URL.createObjectURL(blob);
  }

  // ---------------------------------------------------------------------------
  // KHR_texture_transform
  //
  // GoSX shaders sample UVs directly and carry no texture matrix uniform, so
  // the loader bakes the transform into the UV buffer instead. Baking is exact
  // for every map that shares one transform, which is what Blender, Substance,
  // and gltfpack emit. Read the transform from the base colour texture and
  // apply it to the whole primitive.
  // ---------------------------------------------------------------------------

  // Build the 2x3 UV matrix the KHR_texture_transform spec defines as
  // translation * rotation * scale. Returns null for an identity transform so
  // the caller can skip the buffer rewrite.
  function gltfTextureTransformMatrix(textureInfo) {
    var transform = gltfExtension(textureInfo, "KHR_texture_transform");
    if (!transform) {
      return null;
    }
    var offset = Array.isArray(transform.offset) ? transform.offset : [0, 0];
    var scale = Array.isArray(transform.scale) ? transform.scale : [1, 1];
    var rotation = Number(transform.rotation) || 0;
    var offsetU = Number(offset[0]) || 0;
    var offsetV = Number(offset[1]) || 0;
    var scaleU = isFinite(Number(scale[0])) ? Number(scale[0]) : 1;
    var scaleV = isFinite(Number(scale[1])) ? Number(scale[1]) : 1;
    if (offsetU === 0 && offsetV === 0 && rotation === 0 && scaleU === 1 && scaleV === 1) {
      return null;
    }
    var cos = Math.cos(rotation);
    var sin = Math.sin(rotation);
    return {
      m00: cos * scaleU,
      m01: sin * scaleV,
      m02: offsetU,
      m10: -sin * scaleU,
      m11: cos * scaleV,
      m12: offsetV,
    };
  }

  // Rewrite a UV buffer in place with the texture transform.
  function gltfApplyTextureTransform(uvs, matrix) {
    if (!matrix || !uvs || !uvs.length) {
      return uvs;
    }
    for (var i = 0; i + 1 < uvs.length; i += 2) {
      var u = uvs[i];
      var v = uvs[i + 1];
      uvs[i] = matrix.m00 * u + matrix.m01 * v + matrix.m02;
      uvs[i + 1] = matrix.m10 * u + matrix.m11 * v + matrix.m12;
    }
    return uvs;
  }

  function gltfExtractMaterial(gltf, materialIndex, binaryBuffer) {
    if (materialIndex == null || !gltf.materials || materialIndex >= gltf.materials.length) {
      return gltfDefaultPBRMaterial();
    }
    var mat = gltf.materials[materialIndex];
    var pbr = mat.pbrMetallicRoughness || {};
    var baseColorFactor = pbr.baseColorFactor || [1, 1, 1, 1];

    // glTF metallicRoughnessTexture packs metalness in the B channel and
    // roughness in the G channel. The PBR shader already samples these
    // channels separately, so we assign the same texture to both maps.
    var baseColorURL = gltfResolveTexture(gltf, pbr.baseColorTexture, binaryBuffer);
    var normalURL = gltfResolveTexture(gltf, mat.normalTexture, binaryBuffer);
    var metallicRoughnessURL = gltfResolveTexture(gltf, pbr.metallicRoughnessTexture, binaryBuffer);
    var occlusionURL = gltfResolveTexture(gltf, mat.occlusionTexture, binaryBuffer);
    var emissiveURL = gltfResolveTexture(gltf, mat.emissiveTexture, binaryBuffer);

    var emissiveFactor = mat.emissiveFactor || [0, 0, 0];
    var emissiveStrength = Math.max(emissiveFactor[0], emissiveFactor[1], emissiveFactor[2]);

    // KHR_materials_emissive_strength scales the emissive factor above 1 so
    // HDR emitters keep their intensity. The PBR shaders take an unclamped
    // emissive scalar, so multiply straight through.
    var emissiveExtension = gltfExtension(mat, "KHR_materials_emissive_strength");
    if (emissiveExtension) {
      emissiveStrength *= gltfExtensionFactor(emissiveExtension, "emissiveStrength", 1, 0, 1000);
    }

    var textureDescriptors = gltfMaterialTextureDescriptors(
      baseColorURL,
      normalURL,
      metallicRoughnessURL,
      metallicRoughnessURL,
      occlusionURL,
      emissiveURL
    );
    var record = {
      kind: "standard",
      color: gltfBaseColorToHex(baseColorFactor),
      roughness: pbr.roughnessFactor != null ? pbr.roughnessFactor : 1.0,
      metalness: pbr.metallicFactor != null ? pbr.metallicFactor : 0.0,
      opacity: baseColorFactor[3],
      emissive: emissiveStrength,
      texture: baseColorURL,
      normalMap: normalURL,
      roughnessMap: metallicRoughnessURL,
      metalnessMap: metallicRoughnessURL,
      occlusionMap: occlusionURL,
      emissiveMap: emissiveURL,
      alphaMode: mat.alphaMode || "OPAQUE",
      doubleSided: mat.doubleSided || false,
    };
    if (Object.keys(textureDescriptors).length) {
      record.textureDescriptors = textureDescriptors;
    }

    // KHR_materials_clearcoat -> StandardMaterial.Clearcoat, range 0 to 1.
    var clearcoat = gltfExtension(mat, "KHR_materials_clearcoat");
    if (clearcoat) {
      record.clearcoat = gltfExtensionFactor(clearcoat, "clearcoatFactor", 0, 0, 1);
    }

    // KHR_materials_sheen carries a colour and a roughness. StandardMaterial
    // carries one scalar sheen strength, so take the colour peak. The sheen
    // roughness and the colour hue are dropped.
    var sheen = gltfExtension(mat, "KHR_materials_sheen");
    if (sheen) {
      var sheenColor = sheen.sheenColorFactor;
      record.sheen = Array.isArray(sheenColor) && sheenColor.length >= 3
        ? Math.max(0, Math.min(1, Math.max(
            Number(sheenColor[0]) || 0,
            Number(sheenColor[1]) || 0,
            Number(sheenColor[2]) || 0)))
        : 0;
    }

    // KHR_materials_transmission -> StandardMaterial.Transmission, 0 to 1.
    var transmission = gltfExtension(mat, "KHR_materials_transmission");
    if (transmission) {
      record.transmission = gltfExtensionFactor(transmission, "transmissionFactor", 0, 0, 1);
    }

    // KHR_materials_iridescence -> StandardMaterial.Iridescence, 0 to 1.
    var iridescence = gltfExtension(mat, "KHR_materials_iridescence");
    if (iridescence) {
      record.iridescence = gltfExtensionFactor(iridescence, "iridescenceFactor", 0, 0, 1);
    }

    // KHR_materials_anisotropy carries an unsigned strength and a rotation in
    // radians. StandardMaterial.Anisotropy is one signed scalar from -1 to 1,
    // where the sign selects the tangent or the bitangent direction. Project
    // the rotation onto that axis pair with cos(2 * rotation): a rotation of 0
    // gives +strength along the tangent and a rotation of pi/2 gives -strength
    // along the bitangent. Rotations between the two axes lose their exact
    // angle.
    var anisotropy = gltfExtension(mat, "KHR_materials_anisotropy");
    if (anisotropy) {
      var strength = gltfExtensionFactor(anisotropy, "anisotropyStrength", 0, 0, 1);
      var rotation = Number(anisotropy.anisotropyRotation) || 0;
      record.anisotropy = Math.max(-1, Math.min(1, strength * Math.cos(2 * rotation)));
    }

    // KHR_materials_ior records the index of refraction. The PBR shaders derive
    // F0 from a fixed 0.04, so nothing consumes this value yet. Carry it on the
    // material so a later shader pass can read it without a loader change.
    var ior = gltfExtension(mat, "KHR_materials_ior");
    if (ior) {
      record.ior = gltfExtensionFactor(ior, "ior", 1.5, 1, 5);
    }

    // KHR_materials_unlit switches to the flat shading path. Both the WebGL and
    // the WebGPU renderers read material.unlit already.
    if (gltfExtension(mat, "KHR_materials_unlit")) {
      record.unlit = true;
    }

    // KHR_texture_transform on the base colour texture. Record the matrix so
    // gltfExtractMeshNode can bake it into the UV buffer.
    var uvMatrix = gltfTextureTransformMatrix(pbr.baseColorTexture);
    if (uvMatrix) {
      record.uvTransform = uvMatrix;
    }

    return record;
  }

  // ---------------------------------------------------------------------------
  // Mesh node extraction — produces objects for the scene asset
  // ---------------------------------------------------------------------------

  // Synthesized primitive id: an authored mesh name wins over the positional
  // "mesh-<index>" form, and idSuffix marks instanced copies.
  function gltfPrimitiveID(mesh, meshIndex, channel, p, suffix) {
    return (mesh.name || ("mesh-" + meshIndex)) + "-" + channel + "-" + p + suffix;
  }

  // Shared alpha-pass gate: BLEND or sub-unit opacity renders in the alpha
  // pass. One predicate backs the points/lines blendMode strings and the mesh
  // renderPass so the three sites cannot drift apart.
  function gltfIsAlphaMaterial(material) {
    return material.alphaMode === "BLEND" || material.opacity < 0.999;
  }

  // Once-per-load reachability scan: which meshes are referenced by more than
  // one node in the selected scene. Reused meshes need per-node synthesized-id
  // disambiguation; single-use meshes keep their legacy ids untouched.
  function gltfCountMeshUses(gltf, nodeIndex, counts, visited) {
    if (visited[nodeIndex]) {
      return;
    }
    visited[nodeIndex] = true;
    var node = gltf.nodes && gltf.nodes[nodeIndex];
    if (!node) {
      return;
    }
    if (node.mesh != null) {
      counts[node.mesh] = (counts[node.mesh] || 0) + 1;
    }
    var children = node.children || [];
    for (var i = 0; i < children.length; i++) {
      gltfCountMeshUses(gltf, children[i], counts, visited);
    }
  }

  function gltfSharedMeshMap(gltf, scene) {
    var counts = {};
    var visited = {};
    if (scene && scene.nodes) {
      for (var i = 0; i < scene.nodes.length; i++) {
        gltfCountMeshUses(gltf, scene.nodes[i], counts, visited);
      }
    }
    var shared = {};
    for (var meshIndex in counts) {
      if (counts[meshIndex] > 1) {
        shared[meshIndex] = true;
      }
    }
    return shared;
  }

  // Node-identity suffix appended only when the node's mesh is reused by
  // multiple reachable nodes; empty otherwise.
  function gltfNodeSuffix(sharedMeshes, nodeIndex) {
    return sharedMeshes ? "-n" + nodeIndex : "";
  }

  // Read POSITION, transform it by the node matrix, and report vertex count.
  // Returns null when the attribute or its values are too short to draw.
  function gltfTransformedPositions(gltf, primitive, binaryBuffer, worldTransform, minValues) {
    var record = gltfReadPrimitiveAttribute(gltf, primitive, ["POSITION"], binaryBuffer);
    if (!record || !record.values || record.values.length < minValues) {
      return null;
    }
    var transformed = gltfTransformPositions(record.values, worldTransform);
    return { positions: transformed, count: Math.floor(transformed.length / 3) };
  }

  function gltfExtractMeshNode(gltf, meshIndex, binaryBuffer, worldTransform, result, skinIndex, node, idSuffix, nodeIndex, instanceMatrix, animateTRSFlag) {
    var mesh = gltf.meshes[meshIndex];
    if (!mesh) {
      return;
    }
    var suffix = idSuffix || "";
    var animateMorph = nodeIndex != null && gltfNodeHasWeightAnimation(gltf, nodeIndex);
    var animateTRS = animateTRSFlag === true;

    var normalMat = gltfNormalMatrix(worldTransform);
    var skin = skinIndex != null && result.skins ? result.skins[skinIndex] : null;
    var isSkinned = !!skin;

    for (var p = 0; p < mesh.primitives.length; p++) {
      var primitive = mesh.primitives[p];
      gltfRejectCompressedPrimitive(primitive);
      var mode = primitive.mode != null ? primitive.mode : 4;
      var material = gltfExtractMaterial(gltf, primitive.material, binaryBuffer);
      var extras = gltfCollectScene3DExtras(node, mesh, primitive);

      if (mode === 0) {
        var pointStream = gltfTransformedPositions(gltf, primitive, binaryBuffer, worldTransform, 3);
        if (!pointStream) {
          continue;
        }
        var pointPositions = pointStream.positions;
        var pointCount = pointStream.count;
        var pointColors = gltfPointColorBuffer(gltf, primitive, binaryBuffer, pointCount);
        var pointSizes = gltfPointSizeBuffer(gltf, primitive, binaryBuffer, pointCount);
        var pointID = gltfPrimitiveID(mesh, meshIndex, "points", p, suffix);
        var pointEntry = {
          id: pointID,
          count: pointCount,
          positions: pointPositions,
          sizes: pointSizes || [],
          colors: pointColors || [],
          color: material.color || "#ffffff",
          size: 1,
          opacity: material.opacity != null ? material.opacity : 1,
          blendMode: gltfIsAlphaMaterial(material) ? "alpha" : "",
          depthWrite: material.alphaMode !== "BLEND",
          attenuation: false,
        };
        pointEntry._cachedPos = pointPositions;
        if (pointSizes) {
          pointEntry._cachedSizes = pointSizes;
        }
        if (pointColors) {
          pointEntry._cachedColors = pointColors;
        }
        if (animateTRS) {
          // Retain pristine primitive-local positions so rigid playback can
          // re-transform every frame; the baked stream above remains the
          // authored-pose initial value.
          var pointLocal = gltfReadPrimitiveAttribute(gltf, primitive, ["POSITION"], binaryBuffer);
          if (pointLocal && pointLocal.values && pointLocal.values.length >= 3) {
            pointEntry._nodeAnim = {
              nodeIndex: nodeIndex,
              instanceMatrix: instanceMatrix ? gltfCopyMat4(instanceMatrix) : null,
              nodeMatrix: gltfCopyMat4(worldTransform),
              basePositions: new Float32Array(pointLocal.values),
            };
          }
        }
        gltfApplyScene3DExtras(pointEntry, extras, GLTF_POINT_EXTRA_KEYS);
        result.points.push(pointEntry);
        continue;
      }

      if (mode === 1 || mode === 2 || mode === 3) {
        var lineStream = gltfTransformedPositions(gltf, primitive, binaryBuffer, worldTransform, 6);
        if (!lineStream) {
          continue;
        }
        var linePositions = lineStream.positions;
        var lineCount = lineStream.count;
        var lineIndices = primitive.indices != null
          ? gltfReadAccessor(gltf, primitive.indices, binaryBuffer)
          : null;
        var lineID = gltfPrimitiveID(mesh, meshIndex, "lines", p, suffix);
        var lineObject = {
          id: lineID,
          kind: "lines",
          points: gltfPositionsToLinePoints(linePositions),
          lineSegments: gltfLineSegments(mode, lineCount, lineIndices),
          material: material,
          color: material.color || "#cccccc",
          opacity: material.opacity != null ? material.opacity : 1,
          blendMode: gltfIsAlphaMaterial(material) ? "alpha" : "",
        };
        if (animateTRS) {
          // Same pristine-local retention for line/strip/loop primitives;
          // lineSegments index into the per-frame rebuilt points array and
          // stay valid because the vertex count never changes.
          var lineLocal = gltfReadPrimitiveAttribute(gltf, primitive, ["POSITION"], binaryBuffer);
          if (lineLocal && lineLocal.values && lineLocal.values.length >= 6) {
            lineObject._nodeAnim = {
              nodeIndex: nodeIndex,
              instanceMatrix: instanceMatrix ? gltfCopyMat4(instanceMatrix) : null,
              nodeMatrix: gltfCopyMat4(worldTransform),
              basePositions: new Float32Array(lineLocal.values),
            };
          }
        }
        gltfApplyScene3DExtras(lineObject, extras, GLTF_OBJECT_EXTRA_KEYS);
        result.objects.push(lineObject);
        result.materials.push(material);
        continue;
      }

      // Only handle TRIANGLES mode (4) for mesh objects.
      if (mode !== 4) {
        continue;
      }

      // Resolve authored morph weights BEFORE primitive extraction: a node
      // instantiating this mesh overrides the mesh's own defaults wholesale
      // (glTF 2.0); entries beyond the authored list stay at zero and missing
      // entries read as zero. The fold itself happens inside extraction, on
      // primitive-local streams, before any transform or skinning. The weight
      // list is consumed there immediately — no morph metadata ever rides on
      // the extracted object.
      var authoredWeights = node && Array.isArray(node.weights)
        ? node.weights
        : mesh.weights;
      var geometry = gltfExtractMeshPrimitive(gltf, primitive, binaryBuffer, material.uvTransform, authoredWeights, animateMorph, nodeIndex, node);
      var vertCount = geometry.count;
      var primitiveSkinned = isSkinned && geometry.joints && geometry.weights;

      var objectPositions;
      var objectNormals;
      var objectTangents;

      if (primitiveSkinned) {
        objectPositions = new Float32Array(geometry.positions);
        objectNormals = new Float32Array(geometry.normals);
        objectTangents = new Float32Array(geometry.tangents);
      } else {
        // Apply world transform to positions and normals.
        objectPositions = new Float32Array(vertCount * 3);
        objectNormals = new Float32Array(vertCount * 3);
        for (var v = 0; v < vertCount; v++) {
          var px = geometry.positions[v * 3];
          var py = geometry.positions[v * 3 + 1];
          var pz = geometry.positions[v * 3 + 2];
          var wp = gltfTransformPoint(worldTransform, px, py, pz);
          objectPositions[v * 3]     = wp.x;
          objectPositions[v * 3 + 1] = wp.y;
          objectPositions[v * 3 + 2] = wp.z;

          var tnx = geometry.normals[v * 3];
          var tny = geometry.normals[v * 3 + 1];
          var tnz = geometry.normals[v * 3 + 2];
          var wn = gltfTransformNormal(normalMat, tnx, tny, tnz);
          objectNormals[v * 3]     = wn.x;
          objectNormals[v * 3 + 1] = wn.y;
          objectNormals[v * 3 + 2] = wn.z;
        }

        // Transform tangent directions by the upper-left 3x3.
        objectTangents = new Float32Array(vertCount * 4);
        for (var tv = 0; tv < vertCount; tv++) {
          var ttx = geometry.tangents[tv * 4];
          var tty = geometry.tangents[tv * 4 + 1];
          var ttz = geometry.tangents[tv * 4 + 2];
          var tw  = geometry.tangents[tv * 4 + 3];
          var wt = gltfTransformDirection(worldTransform, ttx, tty, ttz);
          var tlen = Math.sqrt(wt.x * wt.x + wt.y * wt.y + wt.z * wt.z);
          if (tlen > 1e-8) { wt.x /= tlen; wt.y /= tlen; wt.z /= tlen; }
          objectTangents[tv * 4]     = wt.x;
          objectTangents[tv * 4 + 1] = wt.y;
          objectTangents[tv * 4 + 2] = wt.z;
          objectTangents[tv * 4 + 3] = tw;
        }
      }

      // Determine render pass from material alpha mode.
      var renderPass = gltfIsAlphaMaterial(material) ? "alpha" : "opaque";

      var objectID = gltfPrimitiveID(mesh, meshIndex, "prim", p, suffix);

      var vertices = {
        positions: objectPositions,
        normals: objectNormals,
        uvs: geometry.uvs,
        tangents: objectTangents,
        count: vertCount,
      };

      var object = {
        id: objectID,
        kind: "gltf-mesh",
        vertices: vertices,
        material: material,
        transform: worldTransform,
        renderPass: renderPass,
        doubleSided: material.doubleSided,
      };

      if (primitiveSkinned) {
        vertices.joints = geometry.joints;
        vertices.weights = geometry.weights;
        object.skinIndex = skinIndex;
        object.skin = skin;
      }

      if (geometry.morphMeta) {
        // Private internal morph metadata (never a public morphTargets /
        // morphWeights field): the mount layer reads it at instantiation.
        // Immutable and shared by every clone; the GLB binary and glTF graph
        // are not retained — only copied streams and validated defaults.
        geometry.morphMeta.instanced = suffix.indexOf("-inst-") === 0;
        if (geometry.morphMeta.instanced && instanceMatrix) {
          // Authored instance-local matrix for morph time: composed after
          // the animated node-world matrix. The baked node matrix already
          // contains it, so it is never applied twice.
          geometry.morphMeta.instanceMatrix = gltfCopyMat4(instanceMatrix);
        }
        object._morphAnim = geometry.morphMeta;
      } else if (!primitiveSkinned && animateTRS) {
        // Rigid TRS playback: retain pristine primitive-local streams (post
        // static morph fold, pre world transform) plus node bookkeeping.
        // Skinned primitives are skipped — their node transforms fold in at
        // skin time through the joint matrices — and morph-animated
        // primitives are skipped — the morph apply already composes animated
        // node matrices so rigid transforms are never applied twice.
        object._nodeAnim = {
          nodeIndex: nodeIndex,
          instanced: suffix.indexOf("-inst-") === 0,
          instanceMatrix: instanceMatrix ? gltfCopyMat4(instanceMatrix) : null,
          nodeMatrix: gltfCopyMat4(worldTransform),
          vertexCount: vertCount,
          basePositions: new Float32Array(geometry.positions),
          baseNormals: new Float32Array(geometry.normals),
          baseTangents: new Float32Array(geometry.tangents),
        };
      }

      gltfApplyScene3DExtras(object, extras, GLTF_OBJECT_EXTRA_KEYS);
      result.objects.push(object);

      result.materials.push(material);
    }
  }

  // ---------------------------------------------------------------------------
  // Node hierarchy traversal
  // ---------------------------------------------------------------------------

  // EXT_mesh_gpu_instancing lists per-instance transforms in accessors on the
  // node. Return one 4x4 matrix per instance, in the node's own space. A loader
  // that ignores the extension draws one instance instead of every instance, so
  // reading it is a correctness fix, not an optimization.
  function gltfInstanceTransforms(gltf, node, binaryBuffer) {
    var instancing = gltfExtension(node, "EXT_mesh_gpu_instancing");
    var attributes = instancing && instancing.attributes;
    if (!attributes) {
      return null;
    }
    function stream(name) {
      return attributes[name] != null
        ? gltfReadAccessor(gltf, attributes[name], binaryBuffer)
        : null;
    }
    var t = stream("TRANSLATION");
    var r = stream("ROTATION");
    var s = stream("SCALE");
    var count = Math.max(
      t ? Math.floor(t.length / 3) : 0,
      r ? Math.floor(r.length / 4) : 0,
      s ? Math.floor(s.length / 3) : 0
    );
    if (!count) {
      return null;
    }
    var out = [];
    for (var i = 0; i < count; i++) {
      out.push(sceneTRSToMat4(
        t ? [t[i * 3], t[i * 3 + 1], t[i * 3 + 2]] : [0, 0, 0],
        r ? [r[i * 4], r[i * 4 + 1], r[i * 4 + 2], r[i * 4 + 3]] : [0, 0, 0, 1],
        s ? [s[i * 3], s[i * 3 + 1], s[i * 3 + 2]] : [1, 1, 1]
      ));
    }
    return out;
  }

  function gltfWalkNode(gltf, nodeIndex, binaryBuffer, parentTransform, result, animatedTRS, inheritedAnimated, sharedMeshes) {
    var node = gltf.nodes[nodeIndex];
    if (!node) {
      return;
    }

    // A node is rigid-animated when it carries a direct TRS channel or any
    // ancestor does; the flag rides the walk so a static child under an
    // animated parent retains pristine inputs without a per-primitive scan.
    var animated = inheritedAnimated === true
      || Boolean(animatedTRS && animatedTRS.has(nodeIndex));
    var localTransform = gltfNodeTransform(node);
    var worldTransform = sceneMat4Multiply(parentTransform, localTransform);

    if (node.mesh != null) {
      var skin = node.skin != null ? node.skin : null;
      var nodeSuffix = gltfNodeSuffix(sharedMeshes && sharedMeshes[node.mesh], nodeIndex);
      var instances = gltfInstanceTransforms(gltf, node, binaryBuffer);
      if (instances) {
        for (var n = 0; n < instances.length; n++) {
          gltfExtractMeshNode(
            gltf,
            node.mesh,
            binaryBuffer,
            sceneMat4Multiply(worldTransform, instances[n]),
            result,
            skin,
            node,
            "-inst-" + n + nodeSuffix,
            nodeIndex,
            instances[n],
            animated
          );
        }
      } else {
        gltfExtractMeshNode(gltf, node.mesh, binaryBuffer, worldTransform, result, skin, node, nodeSuffix, nodeIndex, null, animated);
      }
    }

    var children = node.children || [];
    for (var i = 0; i < children.length; i++) {
      gltfWalkNode(gltf, children[i], binaryBuffer, worldTransform, result, animatedTRS, animated, sharedMeshes);
    }
  }

  // ---------------------------------------------------------------------------
  // Animation extraction
  // ---------------------------------------------------------------------------

  function gltfExtractAnimations(gltf, binaryBuffer) {
    if (!gltf.animations || !gltf.animations.length) {
      return [];
    }
    var animations = [];
    for (var a = 0; a < gltf.animations.length; a++) {
      var anim = gltf.animations[a];
      var channels = [];
      var maxTime = 0;

      for (var c = 0; c < anim.channels.length; c++) {
        var ch = anim.channels[c];
        var sampler = anim.samplers[ch.sampler];
        var times = gltfReadAccessor(gltf, sampler.input, binaryBuffer);
        var values = gltfReadAccessor(gltf, sampler.output, binaryBuffer);

        if (times.length > 0) {
          var lastTime = times[times.length - 1];
          if (lastTime > maxTime) {
            maxTime = lastTime;
          }
        }

        // Component count per keyframe. Translation and scale carry 3, rotation
        // carries 4, and a morph "weights" channel carries one value per morph
        // target. The mixer reads this instead of guessing from the property
        // name, so a weights channel interpolates at its true width.
        var componentCount = times.length > 0 ? Math.max(1, Math.floor(values.length / (times.length * (sampler.interpolation === "CUBICSPLINE" ? 3 : 1)))) : 3;

        channels.push({
          targetID: ch.target.node,
          targetNode: ch.target.node,
          property: ch.target.path,
          componentCount: componentCount,
          interpolation: sampler.interpolation || "LINEAR",
          times: times instanceof Float32Array ? times : new Float32Array(times),
          values: values instanceof Float32Array ? values : new Float32Array(values),
        });
      }

      animations.push({
        name: anim.name || "",
        channels: channels,
        duration: maxTime,
      });
    }
    return animations;
  }

  // ---------------------------------------------------------------------------
  // Skin extraction (stored for downstream skeletal animation)
  // ---------------------------------------------------------------------------

  function gltfExtractSkin(gltf, skinIndex, binaryBuffer) {
    if (skinIndex == null || !gltf.skins || skinIndex >= gltf.skins.length) {
      return null;
    }
    var skin = gltf.skins[skinIndex];
    var joints = Array.isArray(skin.joints) ? skin.joints.slice() : [];
    if (joints.length > 64) {
      console.warn("[gosx] glTF skin has " + joints.length + " joints; max supported is 64. Rendering mesh as static:", skin.name || skinIndex);
      return null;
    }
    var ibm = skin.inverseBindMatrices != null
      ? new Float32Array(gltfReadAccessor(gltf, skin.inverseBindMatrices, binaryBuffer))
      : null;
    if (!ibm || ibm.length < joints.length * 16) {
      ibm = new Float32Array(joints.length * 16);
      for (var i = 0; i < joints.length; i++) {
        ibm[i * 16] = 1;
        ibm[i * 16 + 5] = 1;
        ibm[i * 16 + 10] = 1;
        ibm[i * 16 + 15] = 1;
      }
    }
    return {
      index: skinIndex,
      name: skin.name || "",
      joints: joints,
      inverseBindMatrices: ibm,
      skeleton: skin.skeleton != null ? skin.skeleton : null,
    };
  }

  // ---------------------------------------------------------------------------
  // Full scene extraction
  // ---------------------------------------------------------------------------

  // Extensions this loader reads. KHR_materials_* map onto StandardMaterial
  // fields the PBR shaders already consume. KHR_texture_transform bakes into
  // the UV buffer. Everything absent from this list is ignored, and the
  // compression extensions raise a named error at the point of use.
  var GLTF_SUPPORTED_EXTENSIONS = [
    // A quantized accessor is an ordinary accessor with a narrow component
    // type. gltfReadAccessor already reads every legal type and honours the
    // normalized flag, and the node transform carries the dequantization, so
    // the loader needs no decoder for this one.
    "KHR_mesh_quantization",
    "EXT_mesh_gpu_instancing",
    "KHR_materials_emissive_strength",
    "KHR_materials_ior",
    "KHR_materials_clearcoat",
    "KHR_materials_sheen",
    "KHR_materials_transmission",
    "KHR_materials_iridescence",
    "KHR_materials_anisotropy",
    "KHR_materials_unlit",
    "KHR_texture_transform",
  ];

  // Warn once per load about required extensions the loader ignores. A required
  // extension the loader drops changes how the asset looks, so name it.
  function gltfReportUnsupportedRequiredExtensions(gltf) {
    var required = gltf && Array.isArray(gltf.extensionsRequired) ? gltf.extensionsRequired : null;
    if (!required || !required.length) {
      return [];
    }
    var missing = [];
    for (var i = 0; i < required.length; i++) {
      if (GLTF_SUPPORTED_EXTENSIONS.indexOf(required[i]) === -1) {
        missing.push(required[i]);
      }
    }
    if (missing.length) {
      console.warn("[gosx] glTF requires extensions this loader ignores: " + missing.join(", "));
    }
    return missing;
  }

  function gltfExtractScene(gltf, binaryBuffer) {
    gltfReportUnsupportedRequiredExtensions(gltf);
    var result = {
      objects: [],
      points: [],
      materials: [],
      lights: [],
      labels: [],
      sprites: [],
      animations: [],
      skins: [],
      nodes: Array.isArray(gltf.nodes) ? gltf.nodes : [],
    };

    var sceneIndex = gltf.scene != null ? gltf.scene : 0;
    var scene = gltf.scenes && gltf.scenes[sceneIndex];
    if (!scene || !scene.nodes) {
      return result;
    }

    // Extract skins.
    if (gltf.skins) {
      for (var s = 0; s < gltf.skins.length; s++) {
        var skin = gltfExtractSkin(gltf, s, binaryBuffer);
        result.skins[s] = skin;
      }
    }

    var identity = new Float32Array(SCENE_IDENTITY_MAT4);
    var animatedTRS = gltfDirectTRSNodes(gltf);
    var sharedMeshes = gltfSharedMeshMap(gltf, scene);
    for (var i = 0; i < scene.nodes.length; i++) {
      gltfWalkNode(gltf, scene.nodes[i], binaryBuffer, identity, result, animatedTRS, false, sharedMeshes);
    }

    // Extract animations.
    result.animations = gltfExtractAnimations(gltf, binaryBuffer);

    return result;
  }

  // ---------------------------------------------------------------------------
  // Point overlay: split scene loading
  // ---------------------------------------------------------------------------
  //
  // A scene whose point colors change on a schedule but whose geometry does not
  // would otherwise re-ship the full GLB on every rotation. The split serves
  // the stable attributes (POSITION, _POINT_SIZE) once from an immutable,
  // content-addressed base file, and rotates only a small overlay GLB carrying
  // the attributes that actually change.
  //
  // The overlay names its base in the glTF root extras:
  //
  //   { "extras": { "gosx": { "baseSrc": "/galaxy/geo-<hash>.glb" } } }
  //
  // The model src points at the overlay. The loader fetches it, follows
  // baseSrc (resolved against the overlay URL), extracts the base scene, and
  // patches each point entry whose mesh name and primitive index match an
  // overlay mesh. The base URL changes whenever the geometry content changes,
  // so a base/overlay mismatch cannot arise from caching; the count guard
  // below defends against a server that writes the two files from different
  // layer sets.
  //
  // The page can hide the serial overlay-then-base fetch by preloading the
  // base URL, which it knows at render time.

  function gltfPointOverlayBaseSrc(gltf) {
    var extras = gltf && gltf.extras;
    var group = extras && (extras.gosx || extras.scene3d);
    var src = group && group.baseSrc;
    return typeof src === "string" && src.trim() ? src.trim() : "";
  }

  // Collect patchable point attributes from an overlay document, keyed by the
  // entry id the base extraction will produce for the same mesh name and
  // primitive index. Only mode-0 (points) primitives participate; an overlay
  // mesh may carry COLOR_0, POSITION, or both, and needs no other attributes.
  function gltfCollectPointOverlay(gltf, binaryBuffer) {
    var out = {};
    var sceneIndex = gltf.scene != null ? gltf.scene : 0;
    var scene = gltf.scenes && gltf.scenes[sceneIndex];
    if (!scene || !scene.nodes) {
      return out;
    }
    var identity = new Float32Array(SCENE_IDENTITY_MAT4);
    var sharedMeshes = gltfSharedMeshMap(gltf, scene);
    for (var i = 0; i < scene.nodes.length; i++) {
      gltfCollectPointOverlayNode(gltf, scene.nodes[i], binaryBuffer, identity, out, sharedMeshes);
    }
    return out;
  }

  // Count of one overlay attribute's accessor record, zero when the record is
  // absent or carries no count.
  function gltfOverlayAttributeCount(gltf, primitive, name) {
    var accessor = gltf.accessors && primitive.attributes[name] != null
      ? gltf.accessors[primitive.attributes[name]]
      : null;
    return (accessor && accessor.count) || 0;
  }

  function gltfCollectPointOverlayNode(gltf, nodeIndex, binaryBuffer, parentTransform, out, sharedMeshes) {
    var node = gltf.nodes && gltf.nodes[nodeIndex];
    if (!node) {
      return;
    }
    var worldTransform = sceneMat4Multiply(parentTransform, gltfNodeTransform(node));
    if (node.mesh != null) {
      var mesh = gltf.meshes && gltf.meshes[node.mesh];
      var primitives = mesh && mesh.primitives ? mesh.primitives : [];
      for (var p = 0; p < primitives.length; p++) {
        var primitive = primitives[p];
        var mode = primitive.mode != null ? primitive.mode : 4;
        if (mode !== 0 || !primitive.attributes) {
          continue;
        }
        var positions = null;
        var count = 0;
        if (primitive.attributes.POSITION != null) {
          var positionRecord = gltfReadPrimitiveAttribute(gltf, primitive, ["POSITION"], binaryBuffer);
          if (positionRecord && positionRecord.values && positionRecord.values.length >= 3) {
            positions = gltfTransformPositions(positionRecord.values, worldTransform);
            count = Math.floor(positions.length / 3);
          }
        }
        if (!count && primitive.attributes.COLOR_0 != null) {
          count = gltfOverlayAttributeCount(gltf, primitive, "COLOR_0");
        }
        if (!count && primitive.attributes._POINT_SIZE != null) {
          count = gltfOverlayAttributeCount(gltf, primitive, "_POINT_SIZE");
        }
        if (!count) {
          continue;
        }
        var colors = gltfPointColorBuffer(gltf, primitive, binaryBuffer, count);
        var sizes = gltfPointSizeBuffer(gltf, primitive, binaryBuffer, count);
        if (!colors && !positions && !sizes) {
          continue;
        }
        // Key by the id the base extraction will give the matching entry. An
        // authored extras id (node, mesh, or primitive level) overrides the
        // synthesized meshName-points-p id, exactly as gltfApplyScene3DExtras
        // does during base extraction, so the overlay must resolve it the
        // same way or every authored layer misses its patch.
        var extras = gltfCollectScene3DExtras(node, mesh, primitive);
        var nodeSuffix = gltfNodeSuffix(sharedMeshes && sharedMeshes[node.mesh], nodeIndex);
        var key = extras && typeof extras.id === "string" && extras.id
          ? extras.id
          : gltfPrimitiveID(mesh, node.mesh, "points", p, nodeSuffix);
        out[key] = { count: count, colors: colors, positions: positions, sizes: sizes };
      }
    }
    var children = node.children || [];
    for (var c = 0; c < children.length; c++) {
      gltfCollectPointOverlayNode(gltf, children[c], binaryBuffer, worldTransform, out, sharedMeshes);
    }
  }

  // Field pairs: overlay attribute name and its retained cache twin.
  var GLTF_POINT_PATCH_FIELDS = [
    ["colors", "_cachedColors"], ["positions", "_cachedPos"], ["sizes", "_cachedSizes"],
  ];

  // Patch base point entries in place. A count mismatch means the base and
  // overlay were built from different layer sets; the entry keeps its base
  // attributes so the scene stays renderable, and the skew is reported once
  // per entry rather than corrupting the buffers.
  function gltfApplyPointOverlay(scene, overlay) {
    var points = scene && scene.points;
    if (!points || !points.length) {
      return scene;
    }
    for (var i = 0; i < points.length; i++) {
      var entry = points[i];
      var patch = overlay[entry.id];
      if (!patch) {
        continue;
      }
      if (patch.count !== entry.count) {
        console.warn("[gosx] glb overlay skipped " + entry.id + ": overlay has " + patch.count + " points, base has " + entry.count);
        continue;
      }
      for (var f = 0; f < GLTF_POINT_PATCH_FIELDS.length; f++) {
        var field = GLTF_POINT_PATCH_FIELDS[f];
        var value = patch[field[0]];
        if (!value) {
          continue;
        }
        entry[field[0]] = value;
        entry[field[1]] = value;
      }
    }
    return scene;
  }

  // ---------------------------------------------------------------------------
  // External buffer fetching for .gltf (non-binary) files
  // ---------------------------------------------------------------------------

  // One model-side GET policy: same-origin credentials and one error shape.
  // kind names the asset role in the thrown message so each error stays
  // byte-identical to the inline copies it replaces.
  async function gltfFetchModelResource(url, kind) {
    var response = await fetch(url, { credentials: "same-origin" });
    if (!response.ok) {
      throw new Error("Failed to fetch " + kind + ": " + url + " (HTTP " + response.status + ")");
    }
    return response;
  }

  async function gltfFetchExternalBuffers(gltf, baseURL) {
    if (!gltf.buffers || !gltf.buffers.length) {
      return null;
    }

    // For .gltf files with a single buffer (the common case), fetch it
    // and return the ArrayBuffer directly. Multi-buffer gltf files are
    // rare; we handle only buffer 0 for now and fall back gracefully.
    var buffer0 = gltf.buffers[0];
    if (!buffer0 || !buffer0.uri) {
      return null;
    }

    var uri = buffer0.uri;

    // Data URI.
    if (uri.indexOf("data:") === 0) {
      var response = await fetch(uri);
      return await response.arrayBuffer();
    }

    // Relative or absolute URL.
    var resolved = new URL(uri, baseURL).toString();
    return (await gltfFetchModelResource(resolved, "glTF buffer")).arrayBuffer();
  }

  function gltfAbsoluteURL(url) {
    var raw = typeof url === "string" ? url.trim() : "";
    if (!raw) {
      return "";
    }
    try {
      return new URL(raw, window.location.href).toString();
    } catch (_error) {
      return raw;
    }
  }

  // ---------------------------------------------------------------------------
  // Texture variant selection
  // ---------------------------------------------------------------------------
  //
  // The asset pipeline writes several encodings of one texture and lists the
  // built ones in the manifest under textureVariants. The device reports which
  // block families it has, and this code swaps each image URI for the best
  // variant that device can upload. A BC7 base colour map costs a quarter of the
  // GPU memory of rgba8unorm and a BC4 mask costs an eighth.
  //
  // Three rules keep the swap safe:
  //
  //   - the manifest lists BUILT files only, so a selected URI always exists.
  //     assetpipe.BuildVariantManifest skips a planned variant and
  //     TestSelectVariantRefusesPlannedVariants proves the refusal;
  //   - only a block-compressed variant may replace the authored URI, and only
  //     when the device token set holds every capability that variant requires.
  //     An uncompressed KTX2 file saves no GPU memory and an image element
  //     already loads the authored source, so a device with no block feature
  //     keeps that source unchanged;
  //   - the swap runs only when a renderer registered a KTX2 upload path. A
  //     renderer that loads every image URI through an image element cannot
  //     decode a .ktx2 file, and swapping would trade a working texture for a
  //     broken one.
  //
  // The ranking mirrors SelectFromManifest in assetpipe/variantmanifest.go:
  // higher tier first, then the smaller file, then the lower URI.

  var GLTF_VARIANT_QUALITY_RANK = { ultra: 5, high: 4, standard: 3, medium: 3, low: 2 };

  // Canonical form for every device/quality token read from the manifest or a
  // renderer context: trimmed, lowercased text with a non-string reading as "".
  function gltfLowerToken(value) {
    return String(value || "").trim().toLowerCase();
  }

  function gltfVariantQualityRank(quality) {
    var rank = GLTF_VARIANT_QUALITY_RANK[gltfLowerToken(quality)];
    return rank || 1;
  }

  function gltfTextureVariantTable() {
    var manifest = typeof loadManifest === "function" ? loadManifest() : null;
    var table = manifest && manifest.textureVariants;
    return table && typeof table === "object" ? table : null;
  }

  // gltfTextureVariantTokens reads the explicit mount-scoped renderer snapshot.
  // No context means no evidence, which means no swap. In particular, never
  // infer the selected renderer from page globals: concurrent mounts may use
  // different backends and extension sets.
  function gltfTextureVariantTokens(context) {
    if (!context || context.uploadReady !== true || !Array.isArray(context.tokens) || !context.tokens.length) {
      return null;
    }
    var set = {};
    for (var i = 0; i < context.tokens.length; i++) {
      set[gltfLowerToken(context.tokens[i])] = true;
    }
    return set;
  }

  function gltfTextureVariantContext(value) {
    if (!value || typeof value !== "object") {
      return null;
    }
    return {
      backend: gltfLowerToken(value.backend),
      uploadReady: value.uploadReady === true,
      tokens: Array.isArray(value.tokens) ? value.tokens.slice() : [],
    };
  }

  // gltfVariantTableKeys lists the spellings a manifest may use for one image.
  // The table is keyed by SOURCE asset path, which is a build-relative path, and
  // the resolved URI is absolute. Both spellings are tried, with and without the
  // leading slash.
  function gltfVariantTableKeys(resolved, authored) {
    var keys = [];
    function push(value) {
      if (typeof value === "string" && value && keys.indexOf(value) < 0) keys.push(value);
    }
    push(authored);
    var pathname = "";
    try {
      pathname = new URL(resolved).pathname;
    } catch (_error) {
      pathname = "";
    }
    push(pathname);
    if (pathname.charAt(0) === "/") push(pathname.slice(1));
    push(resolved);
    return keys;
  }

  // gltfVariantEligible reports whether one variant is both block-compressed and
  // fully supported by the device.
  //
  // The block requirement is what keeps an uncompressed KTX2 file from replacing
  // the authored image. Only a block variant cuts GPU memory, and the authored
  // source already loads through an image element with no extra code.
  function gltfVariantEligible(variant, tokens) {
    if (!variant || typeof variant.uri !== "string" || !variant.uri) {
      return false;
    }
    var required = Array.isArray(variant.requiredCapabilities) ? variant.requiredCapabilities : [];
    var block = false;
    for (var i = 0; i < required.length; i++) {
      var token = gltfLowerToken(required[i]);
      if (!tokens[token]) {
        return false;
      }
      if (token.indexOf("device-feature:texture-compression-") === 0) {
        block = true;
      }
    }
    return block;
  }

  // gltfVariantOutranks is the comparison SelectFromManifest sorts with.
  function gltfVariantOutranks(candidate, best) {
    var left = gltfVariantQualityRank(candidate.quality);
    var right = gltfVariantQualityRank(best.quality);
    if (left !== right) {
      return left > right;
    }
    var leftBytes = Number(candidate.bytes) || 0;
    var rightBytes = Number(best.bytes) || 0;
    if (leftBytes !== rightBytes) {
      return leftBytes < rightBytes;
    }
    return String(candidate.uri) < String(best.uri);
  }

  function gltfSelectTextureVariant(variants, tokens) {
    if (!Array.isArray(variants)) {
      return null;
    }
    var best = null;
    for (var i = 0; i < variants.length; i++) {
      var variant = variants[i];
      if (!gltfVariantEligible(variant, tokens)) {
        continue;
      }
      if (best === null || gltfVariantOutranks(variant, best)) {
        best = variant;
      }
    }
    return best;
  }

  function gltfResolveExternalImageURIs(gltf, baseURL, variantContext) {
    if (!gltf || !gltf.images || !gltf.images.length) {
      return;
    }
    // Read the table and the renderer-scoped evidence once per document. Either
    // missing means every image keeps its authored URI.
    var context = gltfTextureVariantContext(variantContext);
    var tokens = gltfTextureVariantTokens(context);
    var table = tokens ? gltfTextureVariantTable() : null;
    if (!table) {
      tokens = null;
    }
    for (var i = 0; i < gltf.images.length; i += 1) {
      var image = gltf.images[i];
      if (!image || typeof image.uri !== "string" || !image.uri || image.uri.indexOf("data:") === 0) {
        continue;
      }
      var authored = image.uri;
      try {
        image.uri = new URL(image.uri, baseURL).toString();
      } catch (_error) {
        // Keep the authored URI; downstream texture loading will report any
        // remaining failure with the original value.
        continue;
      }
      if (!tokens) {
        continue;
      }
      var keys = gltfVariantTableKeys(image.uri, authored);
      for (var k = 0; k < keys.length; k++) {
        var winner = gltfSelectTextureVariant(table[keys[k]], tokens);
        if (!winner) {
          continue;
        }
        try {
          image.uri = new URL(winner.uri, baseURL).toString();
        } catch (_variantError) {
          // A manifest URI that will not resolve is not a reason to lose the
          // authored texture, so the loop leaves the resolved source in place.
        }
        break;
      }
    }
  }

  // ---------------------------------------------------------------------------
  // Main entry point
  // ---------------------------------------------------------------------------

  function sceneGLTFAssetFormat(url) {
    var raw = typeof url === "string" ? url.trim() : "";
    if (!raw) {
      return "";
    }
    var pathname = raw;
    try {
      pathname = new URL(raw, window.location.href).pathname;
    } catch (_error) {
      pathname = raw.split(/[?#]/, 1)[0];
    }
    var normalized = pathname.toLowerCase();
    if (normalized.endsWith(".glb")) {
      return "glb";
    }
    if (normalized.endsWith(".gltf")) {
      return "gltf";
    }
    return "";
  }

  async function sceneLoadGLTFModel(url, variantContext) {
    var isGLB = sceneGLTFAssetFormat(url) === "glb";
    var assetURL = gltfAbsoluteURL(url);
    var response;
    // Start backend settlement alongside network I/O. A mounted caller passes
    // its deferred renderer scope; direct/preload callers pass nothing and stay
    // deliberately neutral.
    var variantContextPromise = variantContext == null
      ? Promise.resolve(null)
      : Promise.resolve(variantContext).then(gltfTextureVariantContext, function() { return null; });

    if (isGLB) {
      response = await gltfFetchModelResource(url, "GLB");
      var arrayBuffer = await response.arrayBuffer();
      var parsed = sceneParseGLB(arrayBuffer);
      var baseSrc = gltfPointOverlayBaseSrc(parsed.json);
      if (baseSrc) {
        var baseURL = new URL(baseSrc, assetURL).toString();
        var baseResponse = await gltfFetchModelResource(baseURL, "GLB base");
        var baseParsed = sceneParseGLB(await baseResponse.arrayBuffer());
        gltfResolveExternalImageURIs(baseParsed.json, baseURL, await variantContextPromise);
        var baseScene = gltfExtractScene(baseParsed.json, baseParsed.binaryBuffer);
        gltfApplyPointOverlay(baseScene, gltfCollectPointOverlay(parsed.json, parsed.binaryBuffer));
        // A layer that exists only in this rotation — a phenomenon absent from
        // the reference geometry — ships as a full mesh in the overlay. Those
        // extract standalone here; append the ones the base does not already
        // carry so presence drift adds content instead of dropping it.
        // Attribute-only overlay meshes have no POSITION and never extract.
        var overlayScene = gltfExtractScene(parsed.json, parsed.binaryBuffer);
        if (overlayScene.points && overlayScene.points.length) {
          var basePointIDs = {};
          for (var bi = 0; bi < baseScene.points.length; bi++) {
            basePointIDs[baseScene.points[bi].id] = true;
          }
          for (var oi = 0; oi < overlayScene.points.length; oi++) {
            if (!basePointIDs[overlayScene.points[oi].id]) {
              baseScene.points.push(overlayScene.points[oi]);
            }
          }
        }
        return baseScene;
      }
      gltfResolveExternalImageURIs(parsed.json, assetURL, await variantContextPromise);
      return gltfExtractScene(parsed.json, parsed.binaryBuffer);
    }

    // .gltf JSON file.
    response = await gltfFetchModelResource(url, "glTF");
    var json = await response.json();
    // External buffers do not depend on image variant selection, so fetch them
    // while the renderer context is still settling.
    var bufferPromise = gltfFetchExternalBuffers(json, assetURL);
    // Mark an early network failure handled immediately. The original Promise
    // remains rejected, so the later await still throws the same error after
    // renderer-context settlement without opening an unhandledrejection window.
    bufferPromise.catch(function() {});
    gltfResolveExternalImageURIs(json, assetURL, await variantContextPromise);
    var bufferData = await bufferPromise;
    return gltfExtractScene(json, bufferData);
  }

  // ---------------------------------------------------------------------------
  // Convert extracted glTF scene to the model asset format expected by
  // parseSceneModelAsset / hydrateSceneStateModels in 20-scene-mount.js
  // ---------------------------------------------------------------------------

  function gltfSceneToModelAsset(scene, src) {
    // Collection names share their model-asset spellings, so one sweep copies
    // every present list and substitutes an empty one where absent.
    var collections = [
      "objects", "points", "labels", "sprites", "lights",
      "materials", "animations", "skins", "nodes",
    ];
    var asset = { src: src || "" };
    for (var i = 0; i < collections.length; i++) {
      asset[collections[i]] = scene[collections[i]] || [];
    }
    return asset;
  }

  // Publish the GLTF API onto window so ensureGLTFFeatureLoaded() in
  // 20-scene-mount.js finds it without trying to lazy-load the split
  // sub-feature chunk. Required for the legacy monolithic bootstrap.js
  // bundle that inlines 19-scene-gltf.js — without this publish, the
  // lazy-loader races to fetch bootstrap-feature-scene3d-gltf.js (which
  // in test environments and in pages that serve only bootstrap.js
  // doesn't exist), and every declarative model load times out. The
  // split bootstrap-feature-scene3d-gltf.js bundle also has its own
  // publish in 26f-feature-scene3d-gltf-suffix.js; both writing the
  // same value to the same global is a harmless double-set.
  if (typeof window !== "undefined") {
    window.__gosx_scene3d_gltf_api = {
      sceneLoadGLTFModel: sceneLoadGLTFModel,
      gltfSceneToModelAsset: gltfSceneToModelAsset,
      applyMorphPose: gltfApplyAnimatedMorphPose,
      applyNodeAnimPose: gltfApplyNodeAnimPose,
    };
    window.__gosx_scene3d_gltf_loaded = true;
  }
