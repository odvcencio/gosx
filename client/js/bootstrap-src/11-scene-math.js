  // Scene math utilities — pure numeric helpers with no DOM or WebGL dependencies.

  function sceneClamp(value, min, max) {
    return Math.max(min, Math.min(max, value));
  }

  function sceneAddPoint(left, right) {
    return {
      x: sceneNumber(left && left.x, 0) + sceneNumber(right && right.x, 0),
      y: sceneNumber(left && left.y, 0) + sceneNumber(right && right.y, 0),
      z: sceneNumber(left && left.z, 0) + sceneNumber(right && right.z, 0),
    };
  }

  function sceneScalePoint(point, scale) {
    return {
      x: sceneNumber(point && point.x, 0) * scale,
      y: sceneNumber(point && point.y, 0) * scale,
      z: sceneNumber(point && point.z, 0) * scale,
    };
  }

  function sceneMultiplyPoint(left, right) {
    return {
      x: sceneNumber(left && left.x, 0) * sceneNumber(right && right.x, 0),
      y: sceneNumber(left && left.y, 0) * sceneNumber(right && right.y, 0),
      z: sceneNumber(left && left.z, 0) * sceneNumber(right && right.z, 0),
    };
  }

  function scenePointLength(point) {
    var px = sceneNumber(point && point.x, 0);
    var py = sceneNumber(point && point.y, 0);
    var pz = sceneNumber(point && point.z, 0);
    return Math.sqrt(px * px + py * py + pz * pz);
  }

  function sceneNormalizePoint(point) {
    const length = scenePointLength(point);
    if (length <= 0.000001) {
      return { x: 0, y: 0, z: 0 };
    }
    return sceneScalePoint(point, 1 / length);
  }

  function sceneDotPoint(left, right) {
    return (
      sceneNumber(left && left.x, 0) * sceneNumber(right && right.x, 0) +
      sceneNumber(left && left.y, 0) * sceneNumber(right && right.y, 0) +
      sceneNumber(left && left.z, 0) * sceneNumber(right && right.z, 0)
    );
  }

  function sceneRotatePoint(point, rotationX, rotationY, rotationZ) {
    let x = point.x;
    let y = point.y;
    let z = point.z;

    const sinX = Math.sin(rotationX);
    const cosX = Math.cos(rotationX);
    let nextY = y * cosX - z * sinX;
    let nextZ = y * sinX + z * cosX;
    y = nextY;
    z = nextZ;

    const sinY = Math.sin(rotationY);
    const cosY = Math.cos(rotationY);
    let nextX = x * cosY + z * sinY;
    nextZ = -x * sinY + z * cosY;
    x = nextX;
    z = nextZ;

    const sinZ = Math.sin(rotationZ);
    const cosZ = Math.cos(rotationZ);
    nextX = x * cosZ - y * sinZ;
    nextY = x * sinZ + y * cosZ;

    return { x: nextX, y: nextY, z: z };
  }

  function sceneInverseRotatePoint(point, rotationX, rotationY, rotationZ) {
    let x = sceneNumber(point && point.x, 0);
    let y = sceneNumber(point && point.y, 0);
    let z = sceneNumber(point && point.z, 0);

    const sinZ = Math.sin(-rotationZ);
    const cosZ = Math.cos(-rotationZ);
    let nextX = x * cosZ - y * sinZ;
    let nextY = x * sinZ + y * cosZ;
    x = nextX;
    y = nextY;

    const sinY = Math.sin(-rotationY);
    const cosY = Math.cos(-rotationY);
    nextX = x * cosY + z * sinY;
    let nextZ = -x * sinY + z * cosY;
    x = nextX;
    z = nextZ;

    const sinX = Math.sin(-rotationX);
    const cosX = Math.cos(-rotationX);
    nextY = y * cosX - z * sinX;
    nextZ = y * sinX + z * cosX;
    return { x, y: nextY, z: nextZ };
  }

  // Module-level camera scratch used by sceneProjectPoint's internal
  // normalization. sceneRenderCamera (with out-param form) mutates in
  // place so each call doesn't allocate a fresh camera struct. Safe
  // because sceneProjectPoint consumes the scratch fields inline and
  // returns before any downstream code can observe the scratch state.
  const _sceneProjectCameraScratch = {
    x: 0, y: 0, z: 0,
    rotationX: 0, rotationY: 0, rotationZ: 0,
    fov: 0, near: 0, far: 0,
  };

  function sceneProjectPoint(point, camera, width, height) {
    // Inlined camera normalization + local transform + perspective
    // projection. Previously this function allocated 5 intermediate
    // objects per call (a camera, a second camera inside
    // sceneCameraLocalPoint, a local point arg, the sceneInverseRotatePoint
    // result, and the final result) which turned into ~60k allocs/sec
    // on a line-heavy scene at 60 fps. Now allocates 1 (the returned
    // projected point — callers retain refs, so we can't scratch that).
    const cam = sceneRenderCamera(camera, _sceneProjectCameraScratch);

    // Step 1: translate into camera-local space.
    let lx = sceneNumber(point && point.x, 0) - cam.x;
    let ly = sceneNumber(point && point.y, 0) - cam.y;
    let lz = sceneNumber(point && point.z, 0) + cam.z;

    // Step 2: inverse-rotate into camera frame (was sceneInverseRotatePoint).
    const sinZ = Math.sin(-cam.rotationZ);
    const cosZ = Math.cos(-cam.rotationZ);
    let nextX = lx * cosZ - ly * sinZ;
    let nextY = lx * sinZ + ly * cosZ;
    lx = nextX;
    ly = nextY;

    const sinY = Math.sin(-cam.rotationY);
    const cosY = Math.cos(-cam.rotationY);
    nextX = lx * cosY + lz * sinY;
    let nextZ = -lx * sinY + lz * cosY;
    lx = nextX;
    lz = nextZ;

    const sinX = Math.sin(-cam.rotationX);
    const cosX = Math.cos(-cam.rotationX);
    nextY = ly * cosX - lz * sinX;
    nextZ = ly * sinX + lz * cosX;
    ly = nextY;
    lz = nextZ;

    // Step 3: perspective clip test.
    if (lz <= cam.near || lz >= cam.far) return null;

    // Step 4: perspective divide.
    const focal = (Math.min(width, height) / 2) / Math.tan((cam.fov * Math.PI) / 360);
    return {
      x: width / 2 + (lx * focal) / lz,
      y: height / 2 - (ly * focal) / lz,
      depth: lz,
    };
  }

  function sceneCameraLocalPoint(point, camera) {
    const normalizedCamera = sceneRenderCamera(camera);
    return sceneInverseRotatePoint({
      x: sceneNumber(point && point.x, 0) - normalizedCamera.x,
      y: sceneNumber(point && point.y, 0) - normalizedCamera.y,
      z: sceneNumber(point && point.z, 0) + normalizedCamera.z,
    }, normalizedCamera.rotationX, normalizedCamera.rotationY, normalizedCamera.rotationZ);
  }

  function sceneClipPoint(point, width, height) {
    return {
      x: (point.x / width) * 2 - 1,
      y: 1 - (point.y / height) * 2,
    };
  }

  function sceneSafeNormal(normal) {
    const normalized = sceneNormalizePoint(normal);
    return scenePointLength(normalized) > 0.0001 ? normalized : { x: 0, y: 1, z: 0 };
  }

  function sceneColorPoint(value, fallback) {
    const rgba = sceneColorRGBA(value, [sceneNumber(fallback && fallback.x, 1), sceneNumber(fallback && fallback.y, 1), sceneNumber(fallback && fallback.z, 1), 1]);
    return { x: rgba[0], y: rgba[1], z: rgba[2] };
  }

  function sceneRGBAString(rgba) {
    const color = Array.isArray(rgba) ? rgba : [0.55, 0.88, 1, 1];
    return "rgba(" +
      Math.round(clamp01(sceneNumber(color[0], 0.55)) * 255) + ", " +
      Math.round(clamp01(sceneNumber(color[1], 0.88)) * 255) + ", " +
      Math.round(clamp01(sceneNumber(color[2], 1)) * 255) + ", " +
      clamp01(sceneNumber(color[3], 1)).toFixed(3) + ")";
  }

  function sceneColorRGBA(value, fallback) {
    const base = Array.isArray(fallback) && fallback.length === 4 ? fallback.slice() : [0.55, 0.88, 1, 1];
    if (typeof value !== "string") {
      return base;
    }

    const trimmed = value.trim();
    const shortHex = trimmed.match(/^#([0-9a-f]{3})$/i);
    if (shortHex) {
      return [
        parseInt(shortHex[1][0] + shortHex[1][0], 16) / 255,
        parseInt(shortHex[1][1] + shortHex[1][1], 16) / 255,
        parseInt(shortHex[1][2] + shortHex[1][2], 16) / 255,
        1,
      ];
    }

    const fullHex = trimmed.match(/^#([0-9a-f]{6})$/i);
    if (fullHex) {
      return [
        parseInt(fullHex[1].slice(0, 2), 16) / 255,
        parseInt(fullHex[1].slice(2, 4), 16) / 255,
        parseInt(fullHex[1].slice(4, 6), 16) / 255,
        1,
      ];
    }

    const rgba = trimmed.match(/^rgba?\(([^)]+)\)$/i);
    if (rgba) {
      const parts = rgba[1].split(",").map(function(part) {
        return Number(part.trim());
      });
      if (parts.length >= 3 && parts.every(function(part, index) {
        return Number.isFinite(part) && (index < 3 || index === 3);
      })) {
        return [
          Math.max(0, Math.min(255, parts[0])) / 255,
          Math.max(0, Math.min(255, parts[1])) / 255,
          Math.max(0, Math.min(255, parts[2])) / 255,
          parts.length > 3 ? Math.max(0, Math.min(1, parts[3])) : 1,
        ];
      }
    }

    return base;
  }

  function sceneMixRGBA(left, right) {
    return [
      (sceneNumber(left && left[0], 0.55) + sceneNumber(right && right[0], 0.55)) / 2,
      (sceneNumber(left && left[1], 0.88) + sceneNumber(right && right[1], 0.88)) / 2,
      (sceneNumber(left && left[2], 1) + sceneNumber(right && right[2], 1)) / 2,
      (sceneNumber(left && left[3], 1) + sceneNumber(right && right[3], 1)) / 2,
    ];
  }

  // ---------------------------------------------------------------------------
  // Flat (non-allocating) math for tight loops — no {x,y,z} object allocation
  // ---------------------------------------------------------------------------

  // Flat dot product — no object allocation.
  function sceneDot3(ax, ay, az, bx, by, bz) {
    return ax * bx + ay * by + az * bz;
  }

  // Flat cross product — writes into out array at offset.
  function sceneCross3Into(out, offset, ax, ay, az, bx, by, bz) {
    out[offset] = ay * bz - az * by;
    out[offset + 1] = az * bx - ax * bz;
    out[offset + 2] = ax * by - ay * bx;
  }

  // Flat normalize — returns length, writes normalized into out.
  function sceneNormalize3Into(out, offset, x, y, z) {
    var len = Math.sqrt(x * x + y * y + z * z);
    if (len < 1e-10) { out[offset] = 0; out[offset + 1] = 0; out[offset + 2] = 0; return 0; }
    var inv = 1 / len;
    out[offset] = x * inv; out[offset + 1] = y * inv; out[offset + 2] = z * inv;
    return len;
  }

  // Ray intersection utilities for raycast-based scene picking.

  function sceneRayIntersectsAABB(rayOrigin, rayDir, boundsMin, boundsMax) {
    // Returns distance to intersection, or -1 if no hit.
    // rayOrigin, rayDir, boundsMin, boundsMax are all {x,y,z} objects.
    var tMin = -Infinity;
    var tMax = Infinity;

    for (var ai = 0; ai < 3; ai++) {
      var axis = ai === 0 ? "x" : ai === 1 ? "y" : "z";
      var origin = rayOrigin[axis];
      var dir = rayDir[axis];
      var min = boundsMin[axis];
      var max = boundsMax[axis];

      if (Math.abs(dir) < 1e-10) {
        // Ray parallel to slab.
        if (origin < min || origin > max) return -1;
      } else {
        var t1 = (min - origin) / dir;
        var t2 = (max - origin) / dir;
        if (t1 > t2) { var tmp = t1; t1 = t2; t2 = tmp; }
        tMin = Math.max(tMin, t1);
        tMax = Math.min(tMax, t2);
        if (tMin > tMax) return -1;
      }
    }

    if (tMax < 0) return -1;
    return tMin >= 0 ? tMin : tMax;
  }

  function sceneRayIntersectsTriangle(rayOrigin, rayDir, v0, v1, v2) {
    // Moller-Trumbore intersection. Returns { distance, u, v } or null.
    // Extract components once to avoid repeated property access in hot path.
    var EPSILON = 1e-7;

    var rox = rayOrigin.x, roy = rayOrigin.y, roz = rayOrigin.z;
    var rdx = rayDir.x, rdy = rayDir.y, rdz = rayDir.z;
    var v0x = v0.x, v0y = v0.y, v0z = v0.z;

    var edge1x = v1.x - v0x, edge1y = v1.y - v0y, edge1z = v1.z - v0z;
    var edge2x = v2.x - v0x, edge2y = v2.y - v0y, edge2z = v2.z - v0z;

    // cross(rayDir, edge2)
    var hx = rdy * edge2z - rdz * edge2y;
    var hy = rdz * edge2x - rdx * edge2z;
    var hz = rdx * edge2y - rdy * edge2x;

    var a = sceneDot3(edge1x, edge1y, edge1z, hx, hy, hz);
    if (a > -EPSILON && a < EPSILON) return null; // parallel

    var f = 1.0 / a;
    var sx = rox - v0x;
    var sy = roy - v0y;
    var sz = roz - v0z;

    var u = f * sceneDot3(sx, sy, sz, hx, hy, hz);
    if (u < 0.0 || u > 1.0) return null;

    // cross(s, edge1)
    var qx = sy * edge1z - sz * edge1y;
    var qy = sz * edge1x - sx * edge1z;
    var qz = sx * edge1y - sy * edge1x;

    var v = f * sceneDot3(rdx, rdy, rdz, qx, qy, qz);
    if (v < 0.0 || u + v > 1.0) return null;

    var t = f * sceneDot3(edge2x, edge2y, edge2z, qx, qy, qz);
    if (t < EPSILON) return null; // behind ray

    return { distance: t, u: u, v: v };
  }

  function clamp01(value) {
    return Math.max(0, Math.min(1, value));
  }

  // ---------------------------------------------------------------------------
  // Column-major 4x4 matrix utilities
  // ---------------------------------------------------------------------------

  var SCENE_IDENTITY_MAT4 = new Float32Array([
    1, 0, 0, 0,
    0, 1, 0, 0,
    0, 0, 1, 0,
    0, 0, 0, 1,
  ]);

  // Scratch buffers for hot-path multiply — avoids per-call allocations.
  var _sceneMat4ScratchA = new Float32Array(16);
  var _sceneMat4ScratchB = new Float32Array(16);

  // Allocating multiply: returns a new Float32Array(16).
  function sceneMat4Multiply(a, b) {
    var out = new Float32Array(16);
    for (var col = 0; col < 4; col++) {
      for (var row = 0; row < 4; row++) {
        out[col * 4 + row] =
          a[row]      * b[col * 4] +
          a[4 + row]  * b[col * 4 + 1] +
          a[8 + row]  * b[col * 4 + 2] +
          a[12 + row] * b[col * 4 + 3];
      }
    }
    return out;
  }

  // Non-allocating multiply: writes result into `out`.
  function sceneMat4MultiplyInto(out, a, b) {
    for (var col = 0; col < 4; col++) {
      for (var row = 0; row < 4; row++) {
        out[col * 4 + row] =
          a[row]      * b[col * 4] +
          a[4 + row]  * b[col * 4 + 1] +
          a[8 + row]  * b[col * 4 + 2] +
          a[12 + row] * b[col * 4 + 3];
      }
    }
    return out;
  }

  // Build a column-major 4x4 matrix from translation, quaternion rotation,
  // and scale components (TRS decomposition). Returns a new Float32Array(16).
  function sceneTRSToMat4(t, r, s) {
    var out = new Float32Array(16);
    var x = r[0], y = r[1], z = r[2], w = r[3];
    var x2 = x + x, y2 = y + y, z2 = z + z;
    var xx = x * x2, xy = x * y2, xz = x * z2;
    var yy = y * y2, yz = y * z2, zz = z * z2;
    var wx = w * x2, wy = w * y2, wz = w * z2;

    out[0]  = (1 - (yy + zz)) * s[0]; out[1]  = (xy + wz) * s[0];       out[2]  = (xz - wy) * s[0];       out[3]  = 0;
    out[4]  = (xy - wz) * s[1];       out[5]  = (1 - (xx + zz)) * s[1]; out[6]  = (yz + wx) * s[1];       out[7]  = 0;
    out[8]  = (xz + wy) * s[2];       out[9]  = (yz - wx) * s[2];       out[10] = (1 - (xx + yy)) * s[2]; out[11] = 0;
    out[12] = t[0];                    out[13] = t[1];                    out[14] = t[2];                    out[15] = 1;

    return out;
  }

  // Non-allocating TRS: writes result into `out`.
  function sceneTRSToMat4Into(out, t, r, s) {
    var x = r[0], y = r[1], z = r[2], w = r[3];
    var x2 = x + x, y2 = y + y, z2 = z + z;
    var xx = x * x2, xy = x * y2, xz = x * z2;
    var yy = y * y2, yz = y * z2, zz = z * z2;
    var wx = w * x2, wy = w * y2, wz = w * z2;

    out[0]  = (1 - (yy + zz)) * s[0]; out[1]  = (xy + wz) * s[0];       out[2]  = (xz - wy) * s[0];       out[3]  = 0;
    out[4]  = (xy - wz) * s[1];       out[5]  = (1 - (xx + zz)) * s[1]; out[6]  = (yz + wx) * s[1];       out[7]  = 0;
    out[8]  = (xz + wy) * s[2];       out[9]  = (yz - wx) * s[2];       out[10] = (1 - (xx + yy)) * s[2]; out[11] = 0;
    out[12] = t[0];                    out[13] = t[1];                    out[14] = t[2];                    out[15] = 1;

    return out;
  }

  // Scratch arrays for animation interpolation — avoids .slice() allocations.
  var _animScratch3 = [0, 0, 0];
  var _animScratch4 = [0, 0, 0, 0];

  // Lazy Scene3D sub-feature chunks run in their own IIFEs. Publish the
  // matrix helpers they need after the math module has initialized.
  if (typeof window !== "undefined" && window.__gosx_scene3d_api) {
    Object.assign(window.__gosx_scene3d_api, {
      SCENE_IDENTITY_MAT4,
      sceneMat4Multiply,
      sceneMat4MultiplyInto,
      sceneTRSToMat4,
      sceneTRSToMat4Into,
      _sceneMat4ScratchA,
      _sceneMat4ScratchB,
      _animScratch3,
      _animScratch4,
    });
  }

  function sceneScreenToRay(pointerX, pointerY, width, height, camera) {
    // Unproject screen coordinates into a world-space ray, consistent with sceneProjectPoint.
    var cam = sceneRenderCamera(camera);

    // Camera world position (z is negated, matching sceneCameraLocalPoint).
    var origin = { x: cam.x, y: cam.y, z: -cam.z };

    // Compute direction in camera-local space, matching the projection focal length.
    var halfFov = (cam.fov * Math.PI) / 360;
    var tanHalf = Math.tan(halfFov);
    var minDim = Math.min(width, height) / 2;
    var focal = minDim / tanHalf;

    // Invert the projection: screenX = w/2 + localX*focal/depth, screenY = h/2 - localY*focal/depth.
    // At unit depth (depth=1): localX = (screenX - w/2) / focal, localY = (h/2 - screenY) / focal.
    var dirCam = {
      x: (pointerX - width / 2) / focal,
      y: (height / 2 - pointerY) / focal,
      z: 1.0,
    };

    // Rotate from camera-local space to world space (inverse of sceneInverseRotatePoint).
    var dirWorld = sceneRotatePoint(dirCam, cam.rotationX, cam.rotationY, cam.rotationZ);

    // Normalize.
    var len = Math.sqrt(dirWorld.x * dirWorld.x + dirWorld.y * dirWorld.y + dirWorld.z * dirWorld.z);
    if (len < 1e-12) {
      return { origin: origin, dir: { x: 0, y: 0, z: 1 } };
    }

    return {
      origin: origin,
      dir: { x: dirWorld.x / len, y: dirWorld.y / len, z: dirWorld.z / len },
    };
  }
