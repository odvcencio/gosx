(function() {
  "use strict";

  var sceneApi = window.__gosx_scene3d_api || {};
  var SCENE_IDENTITY_MAT4 = sceneApi.SCENE_IDENTITY_MAT4;
  var sceneMat4Multiply = sceneApi.sceneMat4Multiply;
  var sceneTRSToMat4 = sceneApi.sceneTRSToMat4;

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

    var magic = view.getUint32(0, true);
    if (magic !== 0x46546C67) {
      throw new Error("Invalid GLB magic");
    }
    var version = view.getUint32(4, true);
    if (version !== 2) {
      throw new Error("Unsupported glTF version: " + version);
    }

    var jsonChunkLength = view.getUint32(12, true);
    var jsonBytes = new Uint8Array(arrayBuffer, 20, jsonChunkLength);
    var json = JSON.parse(sceneDecodeUTF8Bytes(jsonBytes));

    var binaryBuffer = null;
    var binaryOffset = 20 + jsonChunkLength;
    if (binaryOffset < arrayBuffer.byteLength) {
      var binChunkLength = view.getUint32(binaryOffset, true);
      binaryBuffer = arrayBuffer.slice(binaryOffset + 8, binaryOffset + 8 + binChunkLength);
    }

    return { json: json, binaryBuffer: binaryBuffer };
  }

  var GLTF_COMPONENT_SIZES = {
    5120: 1,  // INT8
    5121: 1,  // UINT8
    5122: 2,  // INT16
    5123: 2,  // UINT16
    5125: 4,  // UINT32
    5126: 4,  // FLOAT32
  };

  function gltfAccessorTypeCount(type) {
    switch (type) {
      case "SCALAR": return 1;
      case "VEC2":   return 2;
      case "VEC3":   return 3;
      case "VEC4":   return 4;
      case "MAT2":   return 4;
      case "MAT3":   return 9;
      case "MAT4":   return 16;
      default:       return 1;
    }
  }

  function gltfTypedArrayView(buffer, byteOffset, componentType, count) {
    switch (componentType) {
      case 5120: return new Int8Array(buffer, byteOffset, count);
      case 5121: return new Uint8Array(buffer, byteOffset, count);
      case 5122: return new Int16Array(buffer, byteOffset, count);
      case 5123: return new Uint16Array(buffer, byteOffset, count);
      case 5125: return new Uint32Array(buffer, byteOffset, count);
      case 5126: return new Float32Array(buffer, byteOffset, count);
      default:   return new Float32Array(buffer, byteOffset, count);
    }
  }

  function gltfNormalizeAccessorValues(values, componentType) {
    var normalized = new Float32Array(values.length);
    var divisor = 1;
    var signed = false;
    switch (componentType) {
      case 5120: divisor = 127; signed = true; break;
      case 5121: divisor = 255; break;
      case 5122: divisor = 32767; signed = true; break;
      case 5123: divisor = 65535; break;
      case 5125: divisor = 4294967295; break;
      default:
        for (var f = 0; f < values.length; f++) {
          normalized[f] = values[f];
        }
        return normalized;
    }
    for (var i = 0; i < values.length; i++) {
      var value = values[i] / divisor;
      normalized[i] = signed && value < -1 ? -1 : value;
    }
    return normalized;
  }

  function gltfReadAccessor(gltf, accessorIndex, binaryBuffer) {
    var accessor = gltf.accessors[accessorIndex];
    var bufferView = gltf.bufferViews[accessor.bufferView];
    var buffer = binaryBuffer;

    var byteOffset = (bufferView.byteOffset || 0) + (accessor.byteOffset || 0);
    var componentCount = gltfAccessorTypeCount(accessor.type);
    var componentSize = GLTF_COMPONENT_SIZES[accessor.componentType] || 4;
    var stride = bufferView.byteStride || 0;
    var totalElements = accessor.count * componentCount;

    if (!stride || stride === componentCount * componentSize) {
      var packed = gltfTypedArrayView(buffer, byteOffset, accessor.componentType, totalElements);
      return accessor.normalized ? gltfNormalizeAccessorValues(packed, accessor.componentType) : packed;
    }

    var result = new Float32Array(totalElements);
    var src = new DataView(buffer);
    for (var i = 0; i < accessor.count; i++) {
      var elemOffset = byteOffset + i * stride;
      for (var c = 0; c < componentCount; c++) {
        var co = elemOffset + c * componentSize;
        switch (accessor.componentType) {
          case 5120: result[i * componentCount + c] = src.getInt8(co); break;
          case 5121: result[i * componentCount + c] = src.getUint8(co); break;
          case 5122: result[i * componentCount + c] = src.getInt16(co, true); break;
          case 5123: result[i * componentCount + c] = src.getUint16(co, true); break;
          case 5125: result[i * componentCount + c] = src.getUint32(co, true); break;
          case 5126: result[i * componentCount + c] = src.getFloat32(co, true); break;
          default:   result[i * componentCount + c] = src.getFloat32(co, true); break;
        }
      }
    }
    return accessor.normalized ? gltfNormalizeAccessorValues(result, accessor.componentType) : result;
  }

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

  function gltfGenerateDefaultUVs(vertexCount) {
    return new Float32Array(vertexCount * 2);
  }

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

    for (var i = 0; i < vertexCount; i++) {
      var nx = normals[i * 3], ny = normals[i * 3 + 1], nz = normals[i * 3 + 2];
      var t1x = tan1[i * 3], t1y = tan1[i * 3 + 1], t1z = tan1[i * 3 + 2];

      var dot = nx * t1x + ny * t1y + nz * t1z;
      var ox = t1x - nx * dot;
      var oy = t1y - ny * dot;
      var oz = t1z - nz * dot;
      var len = Math.sqrt(ox * ox + oy * oy + oz * oz);
      if (len > 1e-8) { ox /= len; oy /= len; oz /= len; }
      else { ox = 1; oy = 0; oz = 0; }

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

  function gltfExpandIndexed(positions, normals, uvs, tangents, joints, weights, indices) {
    var count = indices.length;
    var outPos = new Float32Array(count * 3);
    var outNrm = new Float32Array(count * 3);
    var outUV  = new Float32Array(count * 2);
    var outTan = tangents ? new Float32Array(count * 4) : null;
    var outJoints = joints ? new Float32Array(count * 4) : null;
    var outWeights = weights ? new Float32Array(count * 4) : null;

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

      if (outTan) {
        outTan[i * 4]     = tangents[idx * 4];
        outTan[i * 4 + 1] = tangents[idx * 4 + 1];
        outTan[i * 4 + 2] = tangents[idx * 4 + 2];
        outTan[i * 4 + 3] = tangents[idx * 4 + 3];
      }

      if (outJoints) {
        outJoints[i * 4]     = joints[idx * 4];
        outJoints[i * 4 + 1] = joints[idx * 4 + 1];
        outJoints[i * 4 + 2] = joints[idx * 4 + 2];
        outJoints[i * 4 + 3] = joints[idx * 4 + 3];
      }

      if (outWeights) {
        outWeights[i * 4]     = weights[idx * 4];
        outWeights[i * 4 + 1] = weights[idx * 4 + 1];
        outWeights[i * 4 + 2] = weights[idx * 4 + 2];
        outWeights[i * 4 + 3] = weights[idx * 4 + 3];
      }
    }

    return {
      positions: outPos,
      normals: outNrm,
      uvs: outUV,
      tangents: outTan,
      joints: outJoints,
      weights: outWeights,
    };
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
    var order = [];
    if (indices && indices.length) {
      for (var i = 0; i < indices.length; i++) {
        order.push(Math.floor(indices[i]));
      }
    } else {
      for (var p = 0; p < pointCount; p++) {
        order.push(p);
      }
    }

    var segments = [];
    if (mode === 1) {
      for (var pair = 0; pair + 1 < order.length; pair += 2) {
        segments.push([order[pair], order[pair + 1]]);
      }
      return segments;
    }

    for (var s = 0; s + 1 < order.length; s++) {
      segments.push([order[s], order[s + 1]]);
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
    var colors = new Float32Array(count * 4);
    for (var i = 0; i < count; i++) {
      var src = i * componentCount;
      colors[i * 4] = gltfColorComponent(record.values[src], record.accessor.componentType);
      colors[i * 4 + 1] = gltfColorComponent(record.values[src + 1], record.accessor.componentType);
      colors[i * 4 + 2] = gltfColorComponent(record.values[src + 2], record.accessor.componentType);
      colors[i * 4 + 3] = componentCount > 3
        ? gltfColorComponent(record.values[src + 3], record.accessor.componentType)
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
    var componentCount = gltfAccessorTypeCount(record.accessor.type);
    var sizes = new Float32Array(count);
    for (var i = 0; i < count; i++) {
      sizes[i] = Math.max(0, Number(record.values[i * componentCount]) || 0);
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
    "rotationZ", "spinX", "spinY", "spinZ", "transition", "inState",
    "outState", "live",
  ];

  var GLTF_OBJECT_EXTRA_KEYS = [
    "id", "material", "materialKind", "color", "texture", "opacity",
    "emissive", "roughness", "metalness", "blendMode", "renderPass",
    "wireframe", "depthWrite", "x", "y", "z", "rotationX", "rotationY",
    "rotationZ", "spinX", "spinY", "spinZ", "transition", "inState",
    "outState", "live", "static", "pickable", "castShadow", "receiveShadow",
  ];

  function gltfExtractMeshPrimitive(gltf, primitive, binaryBuffer) {
    var positions = gltfReadAccessor(gltf, primitive.attributes.POSITION, binaryBuffer);

    var normals = primitive.attributes.NORMAL != null
      ? gltfReadAccessor(gltf, primitive.attributes.NORMAL, binaryBuffer)
      : null;

    var uvs = primitive.attributes.TEXCOORD_0 != null
      ? gltfReadAccessor(gltf, primitive.attributes.TEXCOORD_0, binaryBuffer)
      : null;

    var tangentsRaw = primitive.attributes.TANGENT != null
      ? gltfReadAccessor(gltf, primitive.attributes.TANGENT, binaryBuffer)
      : null;

    var joints = primitive.attributes.JOINTS_0 != null
      ? gltfReadAccessor(gltf, primitive.attributes.JOINTS_0, binaryBuffer)
      : null;
    if (joints && !(joints instanceof Float32Array)) {
      joints = new Float32Array(joints);
    }

    var weights = primitive.attributes.WEIGHTS_0 != null
      ? gltfReadAccessor(gltf, primitive.attributes.WEIGHTS_0, binaryBuffer)
      : null;
    if (weights && !(weights instanceof Float32Array)) {
      weights = new Float32Array(weights);
    }

    var indices = primitive.indices != null
      ? gltfReadAccessor(gltf, primitive.indices, binaryBuffer)
      : null;

    if (indices) {
      var expanded = gltfExpandIndexed(
        positions,
        normals || positions, // placeholder; we generate normals after expansion
        uvs || gltfGenerateDefaultUVs(positions.length / 3),
        tangentsRaw,
        joints,
        weights,
        indices
      );
      positions = expanded.positions;
      if (normals) {
        normals = expanded.normals;
      } else {
        normals = gltfGenerateFlatNormals(positions);
      }
      uvs = expanded.uvs;
      tangentsRaw = expanded.tangents;
      joints = expanded.joints;
      weights = expanded.weights;
    } else {
      if (!normals) {
        normals = gltfGenerateFlatNormals(positions);
      }
      if (!uvs) {
        uvs = gltfGenerateDefaultUVs(positions.length / 3);
      }
    }

    var tangents = tangentsRaw || gltfComputeTangents(positions, normals, uvs);

    return {
      positions: positions,
      normals: normals,
      uvs: uvs,
      tangents: tangents,
      joints: joints,
      weights: weights,
      count: positions.length / 3,
    };
  }

  function gltfNodeTransform(node) {
    if (node.matrix) {
      return new Float32Array(node.matrix);
    }

    var t = node.translation || [0, 0, 0];
    var r = node.rotation    || [0, 0, 0, 1];
    var s = node.scale       || [1, 1, 1];

    return sceneTRSToMat4(t, r, s);
  }

  function gltfTransformPoint(m, x, y, z) {
    return {
      x: m[0] * x + m[4] * y + m[8]  * z + m[12],
      y: m[1] * x + m[5] * y + m[9]  * z + m[13],
      z: m[2] * x + m[6] * y + m[10] * z + m[14],
    };
  }

  function gltfTransformDirection(m, x, y, z) {
    return {
      x: m[0] * x + m[4] * y + m[8]  * z,
      y: m[1] * x + m[5] * y + m[9]  * z,
      z: m[2] * x + m[6] * y + m[10] * z,
    };
  }

  function gltfNormalMatrix(m) {
    var a00 = m[0], a01 = m[1], a02 = m[2];
    var a10 = m[4], a11 = m[5], a12 = m[6];
    var a20 = m[8], a21 = m[9], a22 = m[10];

    var det = a00 * (a11 * a22 - a12 * a21)
            - a01 * (a10 * a22 - a12 * a20)
            + a02 * (a10 * a21 - a11 * a20);

    if (Math.abs(det) < 1e-10) {
      return [1, 0, 0, 0, 1, 0, 0, 0, 1];
    }

    var invDet = 1.0 / det;

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

  function gltfBaseColorToHex(factor) {
    var r = Math.round(Math.max(0, Math.min(1, factor[0])) * 255);
    var g = Math.round(Math.max(0, Math.min(1, factor[1])) * 255);
    var b = Math.round(Math.max(0, Math.min(1, factor[2])) * 255);
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
      emissiveMap: "",
      alphaMode: "OPAQUE",
      doubleSided: false,
    };
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
    if (!texture || texture.source == null) {
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

    if (image.uri) {
      return image.uri;
    }

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

  function gltfExtractMaterial(gltf, materialIndex, binaryBuffer) {
    if (materialIndex == null || !gltf.materials || materialIndex >= gltf.materials.length) {
      return gltfDefaultPBRMaterial();
    }
    var mat = gltf.materials[materialIndex];
    var pbr = mat.pbrMetallicRoughness || {};
    var baseColorFactor = pbr.baseColorFactor || [1, 1, 1, 1];

    var metallicRoughnessURL = gltfResolveTexture(gltf, pbr.metallicRoughnessTexture, binaryBuffer);

    var emissiveFactor = mat.emissiveFactor || [0, 0, 0];
    var emissiveStrength = Math.max(emissiveFactor[0], emissiveFactor[1], emissiveFactor[2]);

    return {
      kind: "standard",
      color: gltfBaseColorToHex(baseColorFactor),
      roughness: pbr.roughnessFactor != null ? pbr.roughnessFactor : 1.0,
      metalness: pbr.metallicFactor != null ? pbr.metallicFactor : 0.0,
      opacity: baseColorFactor[3],
      emissive: emissiveStrength,
      texture: gltfResolveTexture(gltf, pbr.baseColorTexture, binaryBuffer),
      normalMap: gltfResolveTexture(gltf, mat.normalTexture, binaryBuffer),
      roughnessMap: metallicRoughnessURL,
      metalnessMap: metallicRoughnessURL,
      emissiveMap: gltfResolveTexture(gltf, mat.emissiveTexture, binaryBuffer),
      alphaMode: mat.alphaMode || "OPAQUE",
      doubleSided: mat.doubleSided || false,
    };
  }

  function gltfExtractMeshNode(gltf, meshIndex, binaryBuffer, worldTransform, result, skinIndex, node) {
    var mesh = gltf.meshes[meshIndex];
    if (!mesh) {
      return;
    }

    var normalMat = gltfNormalMatrix(worldTransform);
    var skin = skinIndex != null && result.skins ? result.skins[skinIndex] : null;
    var isSkinned = Boolean(skin);

    for (var p = 0; p < mesh.primitives.length; p++) {
      var primitive = mesh.primitives[p];
      var mode = primitive.mode != null ? primitive.mode : 4;
      var material = gltfExtractMaterial(gltf, primitive.material, binaryBuffer);
      var extras = gltfCollectScene3DExtras(node, mesh, primitive);

      if (mode === 0) {
        var positionRecord = gltfReadPrimitiveAttribute(gltf, primitive, ["POSITION"], binaryBuffer);
        if (!positionRecord || !positionRecord.values || positionRecord.values.length < 3) {
          continue;
        }
        var pointPositions = gltfTransformPositions(positionRecord.values, worldTransform);
        var pointCount = Math.floor(pointPositions.length / 3);
        var pointColors = gltfPointColorBuffer(gltf, primitive, binaryBuffer, pointCount);
        var pointSizes = gltfPointSizeBuffer(gltf, primitive, binaryBuffer, pointCount);
        var pointID = mesh.name ? (mesh.name + "-points-" + p) : ("mesh-" + meshIndex + "-points-" + p);
        var pointEntry = {
          id: pointID,
          count: pointCount,
          positions: pointPositions,
          sizes: pointSizes || [],
          colors: pointColors || [],
          color: material.color || "#ffffff",
          size: 1,
          opacity: material.opacity != null ? material.opacity : 1,
          blendMode: (material.alphaMode === "BLEND" || material.opacity < 0.999) ? "alpha" : "",
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
        gltfApplyScene3DExtras(pointEntry, extras, GLTF_POINT_EXTRA_KEYS);
        result.points.push(pointEntry);
        continue;
      }

      if (mode === 1 || mode === 2 || mode === 3) {
        var linePositionRecord = gltfReadPrimitiveAttribute(gltf, primitive, ["POSITION"], binaryBuffer);
        if (!linePositionRecord || !linePositionRecord.values || linePositionRecord.values.length < 6) {
          continue;
        }
        var linePositions = gltfTransformPositions(linePositionRecord.values, worldTransform);
        var lineCount = Math.floor(linePositions.length / 3);
        var lineIndices = primitive.indices != null
          ? gltfReadAccessor(gltf, primitive.indices, binaryBuffer)
          : null;
        var lineID = mesh.name ? (mesh.name + "-lines-" + p) : ("mesh-" + meshIndex + "-lines-" + p);
        var lineObject = {
          id: lineID,
          kind: "lines",
          points: gltfPositionsToLinePoints(linePositions),
          lineSegments: gltfLineSegments(mode, lineCount, lineIndices),
          material: material,
          color: material.color || "#cccccc",
          opacity: material.opacity != null ? material.opacity : 1,
          blendMode: (material.alphaMode === "BLEND" || material.opacity < 0.999) ? "alpha" : "",
        };
        gltfApplyScene3DExtras(lineObject, extras, GLTF_OBJECT_EXTRA_KEYS);
        result.objects.push(lineObject);
        result.materials.push(material);
        continue;
      }

      if (mode !== 4) {
        continue;
      }

      var geometry = gltfExtractMeshPrimitive(gltf, primitive, binaryBuffer);
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

      var renderPass = "opaque";
      if (material.alphaMode === "BLEND" || material.opacity < 0.999) {
        renderPass = "alpha";
      }

      var objectID = "mesh-" + meshIndex + "-prim-" + p;
      if (mesh.name) {
        objectID = mesh.name + "-prim-" + p;
      }

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

      gltfApplyScene3DExtras(object, extras, GLTF_OBJECT_EXTRA_KEYS);
      result.objects.push(object);

      result.materials.push(material);
    }
  }

  function gltfWalkNode(gltf, nodeIndex, binaryBuffer, parentTransform, result) {
    var node = gltf.nodes[nodeIndex];
    if (!node) {
      return;
    }

    var localTransform = gltfNodeTransform(node);
    var worldTransform = sceneMat4Multiply(parentTransform, localTransform);

    if (node.mesh != null) {
      gltfExtractMeshNode(gltf, node.mesh, binaryBuffer, worldTransform, result, node.skin != null ? node.skin : null, node);
    }

    var children = node.children || [];
    for (var i = 0; i < children.length; i++) {
      gltfWalkNode(gltf, children[i], binaryBuffer, worldTransform, result);
    }
  }

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

        channels.push({
          targetID: ch.target.node,
          targetNode: ch.target.node,
          property: ch.target.path,
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

  function gltfExtractScene(gltf, binaryBuffer) {
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

    if (gltf.skins) {
      for (var s = 0; s < gltf.skins.length; s++) {
        var skin = gltfExtractSkin(gltf, s, binaryBuffer);
        result.skins[s] = skin;
      }
    }

    var identity = new Float32Array(SCENE_IDENTITY_MAT4);
    for (var i = 0; i < scene.nodes.length; i++) {
      gltfWalkNode(gltf, scene.nodes[i], binaryBuffer, identity, result);
    }

    result.animations = gltfExtractAnimations(gltf, binaryBuffer);

    return result;
  }

  async function gltfFetchExternalBuffers(gltf, baseURL) {
    if (!gltf.buffers || !gltf.buffers.length) {
      return null;
    }

    var buffer0 = gltf.buffers[0];
    if (!buffer0 || !buffer0.uri) {
      return null;
    }

    var uri = buffer0.uri;

    if (uri.indexOf("data:") === 0) {
      var response = await fetch(uri);
      return await response.arrayBuffer();
    }

    var resolved = new URL(uri, baseURL).toString();
    var response = await fetch(resolved, { credentials: "same-origin" });
    if (!response.ok) {
      throw new Error("Failed to fetch glTF buffer: " + resolved + " (HTTP " + response.status + ")");
    }
    return await response.arrayBuffer();
  }

  async function sceneLoadGLTFModel(url) {
    var isGLB = url.toLowerCase().endsWith(".glb");
    var response;

    if (isGLB) {
      response = await fetch(url, { credentials: "same-origin" });
      if (!response.ok) {
        throw new Error("Failed to fetch GLB: " + url + " (HTTP " + response.status + ")");
      }
      var arrayBuffer = await response.arrayBuffer();
      var parsed = sceneParseGLB(arrayBuffer);
      return gltfExtractScene(parsed.json, parsed.binaryBuffer);
    }

    response = await fetch(url, { credentials: "same-origin" });
    if (!response.ok) {
      throw new Error("Failed to fetch glTF: " + url + " (HTTP " + response.status + ")");
    }
    var json = await response.json();
    var bufferData = await gltfFetchExternalBuffers(json, url);
    return gltfExtractScene(json, bufferData);
  }

  function gltfSceneToModelAsset(scene, src) {
    return {
      src: src || "",
      objects: scene.objects || [],
      points: scene.points || [],
      labels: scene.labels || [],
      sprites: scene.sprites || [],
      lights: scene.lights || [],
      animations: scene.animations || [],
      skins: scene.skins || [],
      nodes: scene.nodes || [],
    };
  }

  if (typeof window !== "undefined") {
    window.__gosx_scene3d_gltf_api = {
      sceneLoadGLTFModel: sceneLoadGLTFModel,
      gltfSceneToModelAsset: gltfSceneToModelAsset,
    };
    window.__gosx_scene3d_gltf_loaded = true;
  }

  window.__gosx_scene3d_gltf_api = {
    sceneLoadGLTFModel: sceneLoadGLTFModel,
    gltfSceneToModelAsset: gltfSceneToModelAsset,
  };

  window.__gosx_scene3d_gltf_loaded = true;

})();
//# sourceMappingURL=bootstrap-feature-scene3d-gltf.js.map
