  // Scene decompression — TurboQuant scalar dequantizer for compressed vertex data.
  // Decompresses compressedPositions/compressedSizes/compressedTransforms arrays
  // that were scalar-quantized on the Go side (min/max per chunk, b-bit indices).

  function sceneDecompressArray(chunks) {
    if (!Array.isArray(chunks) || chunks.length === 0) return null;
    var result = [];
    for (var ci = 0; ci < chunks.length; ci++) {
      var chunk = chunks[ci];
      var packed = chunk.packed;
      var minVal = chunk.norm;      // "norm" field stores the min value
      var maxVal = chunk.maxVal;
      var count = chunk.count;
      var bitWidth = chunk.bitWidth;

      if (!packed || count < 2) continue;

      // Decode base64 packed bytes if needed
      var bytes;
      if (typeof packed === "string") {
        bytes = sceneBase64Decode(packed);
      } else if (packed instanceof Uint8Array) {
        bytes = packed;
      } else if (Array.isArray(packed)) {
        bytes = new Uint8Array(packed);
      } else {
        continue;
      }

      var levels = (1 << bitWidth) - 1;
      var step = levels > 0 ? (maxVal - minVal) / levels : 0;

      // Unpack b-bit indices and dequantize
      var indices = sceneUnpackIndices(bytes, count, bitWidth);
      for (var i = 0; i < count; i++) {
        result.push(minVal + indices[i] * step);
      }
    }
    return result.length > 0 ? result : null;
  }

  function sceneUnpackIndices(src, count, bitWidth) {
    var indices = new Int32Array(count);
    switch (bitWidth) {
      case 1:
        for (var i = 0; i < count; i++) {
          indices[i] = (src[i >> 3] >> (i & 7)) & 1;
        }
        break;
      case 2:
        for (var i = 0; i < count; i++) {
          indices[i] = (src[i >> 2] >> ((i & 3) * 2)) & 3;
        }
        break;
      case 4:
        for (var i = 0; i < count; i++) {
          indices[i] = (src[i >> 1] >> ((i & 1) * 4)) & 15;
        }
        break;
      case 8:
        for (var i = 0; i < count; i++) {
          indices[i] = src[i];
        }
        break;
      default:
        var bitPos = 0;
        var mask = (1 << bitWidth) - 1;
        for (var i = 0; i < count; i++) {
          var val = 0;
          for (var b = 0; b < bitWidth; b++) {
            if (src[bitPos >> 3] & (1 << (bitPos & 7))) {
              val |= 1 << b;
            }
            bitPos++;
          }
          indices[i] = val & mask;
        }
    }
    return indices;
  }

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

  // Decompress a points entry in place — replaces compressedPositions/compressedSizes
  // with decompressed positions/sizes arrays so the render pipeline sees plain float arrays.
  function sceneDecompressPointsEntry(entry) {
    if (entry.compressedPositions && !entry.positions) {
      entry.positions = sceneDecompressArray(entry.compressedPositions);
      if (entry.positions) {
        delete entry.compressedPositions;
      }
    }
    if (entry.compressedSizes && !entry.sizes) {
      entry.sizes = sceneDecompressArray(entry.compressedSizes);
      if (entry.sizes) {
        delete entry.compressedSizes;
      }
    }
  }

  // Decompress an instanced mesh entry in place.
  function sceneDecompressInstancedMeshEntry(entry) {
    if (entry.compressedTransforms && !entry.transforms) {
      entry.transforms = sceneDecompressArray(entry.compressedTransforms);
      if (entry.transforms) {
        delete entry.compressedTransforms;
      }
    }
  }

  // Decompress all compressed data in scene props. Called once at scene init
  // before the render loop starts. Mutates entries in place for zero-copy.
  //
  // Progressive mode: decompress preview first (fast, low quality), store full
  // resolution as pending. A follow-up call to sceneUpgradeProgressive() swaps
  // in the full-quality data.
  //
  // LOD mode: keep both preview and full decompressed. The render loop selects
  // which to use based on camera distance.
  function sceneDecompressProps(props) {
    var scene = sceneProps(props);
    var comp = props && props.compression;
    var progressive = comp && comp.progressive;
    var lod = comp && comp.lod;
    var lodThreshold = comp && comp.lodThreshold || 20;

    var points = scene && Array.isArray(scene.points) ? scene.points :
                 (props && Array.isArray(props.points) ? props.points : []);
    for (var i = 0; i < points.length; i++) {
      var entry = points[i];
      if (progressive || lod) {
        // Decompress preview immediately for fast first paint
        if (entry.previewPositions && !entry.positions) {
          entry.positions = sceneDecompressArray(entry.previewPositions);
          // Store full-res data for upgrade
          entry._pendingPositions = entry.compressedPositions;
          entry._previewActive = true;
          delete entry.previewPositions;
        }
        if (entry.previewSizes && !entry.sizes) {
          entry.sizes = sceneDecompressArray(entry.previewSizes);
          entry._pendingSizes = entry.compressedSizes;
          delete entry.previewSizes;
        }
        if (lod) {
          // Also decompress full-res for LOD switching
          if (entry._pendingPositions) {
            entry._fullPositions = sceneDecompressArray(entry._pendingPositions);
            entry._previewPositions = entry.positions;
            entry._lodThreshold = lodThreshold;
          }
        }
      } else {
        sceneDecompressPointsEntry(entry);
      }
    }

    var meshes = scene && Array.isArray(scene.instancedMeshes) ? scene.instancedMeshes :
                 (props && Array.isArray(props.instancedMeshes) ? props.instancedMeshes : []);
    for (var i = 0; i < meshes.length; i++) {
      var entry = meshes[i];
      if (progressive || lod) {
        if (entry.previewTransforms && !entry.transforms) {
          entry.transforms = sceneDecompressArray(entry.previewTransforms);
          entry._pendingTransforms = entry.compressedTransforms;
          entry._previewActive = true;
          delete entry.previewTransforms;
        }
        if (lod && entry._pendingTransforms) {
          entry._fullTransforms = sceneDecompressArray(entry._pendingTransforms);
          entry._previewTransforms = entry.transforms;
          entry._lodThreshold = lodThreshold;
        }
      } else {
        sceneDecompressInstancedMeshEntry(entry);
      }
    }
  }

  // Upgrade progressive entries from preview to full resolution.
  // Called after the first frame renders (requestIdleCallback or setTimeout).
  function sceneUpgradeProgressive(props) {
    var scene = sceneProps(props);
    var points = scene && Array.isArray(scene.points) ? scene.points :
                 (props && Array.isArray(props.points) ? props.points : []);
    for (var i = 0; i < points.length; i++) {
      var entry = points[i];
      if (entry._pendingPositions) {
        entry.positions = sceneDecompressArray(entry._pendingPositions);
        delete entry._pendingPositions;
        delete entry._previewActive;
        // Clear cached typed arrays so the renderer rebuilds buffers
        delete entry._cachedPos;
      }
      if (entry._pendingSizes) {
        entry.sizes = sceneDecompressArray(entry._pendingSizes);
        delete entry._pendingSizes;
        delete entry._cachedSizes;
      }
    }
    var meshes = scene && Array.isArray(scene.instancedMeshes) ? scene.instancedMeshes :
                 (props && Array.isArray(props.instancedMeshes) ? props.instancedMeshes : []);
    for (var i = 0; i < meshes.length; i++) {
      var entry = meshes[i];
      if (entry._pendingTransforms) {
        entry.transforms = sceneDecompressArray(entry._pendingTransforms);
        delete entry._pendingTransforms;
        delete entry._previewActive;
      }
    }
  }

  // LOD: swap positions based on camera distance to object center.
  // Called per-frame in the render loop for LOD-enabled scenes.
  function sceneApplyLOD(entry, cameraX, cameraY, cameraZ) {
    if (!entry._fullPositions || !entry._previewPositions) return;
    var dx = (entry.x || 0) - cameraX;
    var dy = (entry.y || 0) - cameraY;
    var dz = (entry.z || 0) - cameraZ;
    var dist = Math.sqrt(dx * dx + dy * dy + dz * dz);
    var threshold = entry._lodThreshold || 20;
    var wantFull = dist < threshold;
    var hasFull = entry.positions === entry._fullPositions;
    if (wantFull && !hasFull) {
      entry.positions = entry._fullPositions;
      delete entry._cachedPos; // force buffer rebuild
    } else if (!wantFull && hasFull) {
      entry.positions = entry._previewPositions;
      delete entry._cachedPos;
    }
  }
