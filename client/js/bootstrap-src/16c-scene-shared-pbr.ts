  // Backend-agnostic Scene3D PBR helpers. This file stays in the base
  // scene3d chunk. 16-scene-webgl.js became a lazily fetched chunk
  // (bootstrap-feature-scene3d-webgl.js) because a WebGPU-capable browser
  // never runs it. These helpers are the part of the old WebGL file that the
  // base chunk and the WebGPU chunk still need, so they must stay eager.
  //
  // Three groups of callers depend on them:
  //   1. 15b-scene-planner.js sorts and classifies draw passes with
  //      scenePBRObjectRenderPass and scenePBRDepthSort.
  //   2. 10-runtime-scene-core.js and 20-scene-mount.js keep light and
  //      environment dirty hashes with hashLightContent and
  //      hashEnvironmentContent.
  //   3. 10-runtime-scene-core.js publishes the camera matrices, the shadow
  //      bounds and the instanced geometry generators on
  //      window.__gosx_scene3d_api. The WebGPU chunk reads them there through
  //      26e-feature-scene3d-webgpu-prefix.js.
  //
  // Every function here is pure math or plain object work. None of them
  // touches a WebGLRenderingContext. Keep it that way: a `gl.` call in this
  // file means the WebGL split leaked back into the eager path.
  //
  // The 37 declarations below are a closed dependency set. Adding a caller
  // here that reaches back into 16-scene-webgl.js breaks a WebGPU-only page
  // with a ReferenceError, which no test in this repo can catch without a
  // real GPU. Re-derive the closure before you move anything in or out.

  // Post-effect kind constants.
  var SCENE_POST_TONE_MAPPING = "toneMapping";

  var SCENE_POST_BLOOM = "bloom";

  var SCENE_POST_VIGNETTE = "vignette";

  var SCENE_POST_COLOR_GRADE = "colorGrade";

  var SCENE_POST_SSAO = "ssao";

  var SCENE_POST_DOF = "dof";

  var SCENE_POST_CUSTOM_POST = "customPost";

  var SCENE_POST_FXAA = "fxaa";

  // --- Camera matrices ---

  // Build a 4x4 view matrix from camera position and Euler rotation.
  //
  // The GoSX camera convention: the camera has position (x, y, z) and Euler
  // angles (rotationX, rotationY, rotationZ). The shared Scene3D contract
  // shifts world points by (-camX, -camY, -camZ) then applies inverse
  // rotation. Positive forward depth is -viewZ.
  //
  // To produce a standard 4x4 view matrix we construct:
  //   V = inverseRotation * translation(-camX, -camY, -camZ)
  //
  // The inverse rotation is computed by applying -rotZ, -rotY, -rotX
  // (reverse order, negative angles) — matching sceneInverseRotatePoint.
  // Build a 4x4 view matrix into `out` (or a new Float32Array if omitted).
  function scenePBRViewMatrix(camera, out) {
    const cam = sceneRenderCamera(camera);
    const tx = -cam.x;
    const ty = -cam.y;
    const tz = -cam.z;

    // Inverse Euler: apply -rotZ, then -rotY, then -rotX.
    const sx = Math.sin(-cam.rotationX);
    const cx = Math.cos(-cam.rotationX);
    const sy = Math.sin(-cam.rotationY);
    const cy = Math.cos(-cam.rotationY);
    const sz = Math.sin(-cam.rotationZ);
    const cz = Math.cos(-cam.rotationZ);

    // Rotation matrix = Rx(-rx) * Ry(-ry) * Rz(-rz), matching
    // sceneInverseRotatePoint's scalar sequence exactly.
    // Column-major order for WebGL.
    const r00 = cy * cz;
    const r01 = -cy * sz;
    const r02 = sy;

    const r10 = cx * sz + sx * sy * cz;
    const r11 = cx * cz - sx * sy * sz;
    const r12 = -sx * cy;

    const r20 = sx * sz - cx * sy * cz;
    const r21 = sx * cz + cx * sy * sz;
    const r22 = cx * cy;

    // Translation part: R * t
    const d0 = r00 * tx + r01 * ty + r02 * tz;
    const d1 = r10 * tx + r11 * ty + r12 * tz;
    const d2 = r20 * tx + r21 * ty + r22 * tz;

    // Column-major 4x4 matrix as Float32Array.
    var m = out || new Float32Array(16);
    m[0] = r00; m[1] = r10; m[2] = r20; m[3] = 0;
    m[4] = r01; m[5] = r11; m[6] = r21; m[7] = 0;
    m[8] = r02; m[9] = r12; m[10] = r22; m[11] = 0;
    m[12] = d0; m[13] = d1; m[14] = d2; m[15] = 1;
    return m;
  }

  // Build a perspective projection matrix into `out` (or a new Float32Array).
  // fov is in degrees, matching sceneRenderCamera output.
  function scenePBRProjectionMatrix(fov, aspect, near, far, out) {
    const fovRad = (fov * Math.PI) / 180;
    const f = 1 / Math.tan(fovRad * 0.5);
    const rangeInv = 1 / (near - far);

    // Column-major.
    var m = out || new Float32Array(16);
    m[0] = f / aspect; m[1] = 0; m[2] = 0; m[3] = 0;
    m[4] = 0; m[5] = f; m[6] = 0; m[7] = 0;
    m[8] = 0; m[9] = 0; m[10] = (near + far) * rangeInv; m[11] = -1;
    m[12] = 0; m[13] = 0; m[14] = 2 * near * far * rangeInv; m[15] = 0;
    return m;
  }

  function scenePBROrthographicProjectionMatrix(left, right, top, bottom, near, far, out) {
    var m = out || new Float32Array(16);
    const width = Math.max(0.000001, right - left);
    const height = Math.max(0.000001, top - bottom);
    const depth = Math.max(0.000001, far - near);
    m[0] = 2 / width; m[1] = 0; m[2] = 0; m[3] = 0;
    m[4] = 0; m[5] = 2 / height; m[6] = 0; m[7] = 0;
    m[8] = 0; m[9] = 0; m[10] = -2 / depth; m[11] = 0;
    m[12] = -(right + left) / width; m[13] = -(top + bottom) / height; m[14] = -(far + near) / depth; m[15] = 1;
    return m;
  }

  function scenePBRProjectionMatrixForCamera(camera, aspect, out) {
    const cam = sceneRenderCamera(camera);
    if (cam.kind === "orthographic") {
      const bounds = sceneOrthographicBounds(cam, Math.max(1, aspect * 1000), 1000);
      return scenePBROrthographicProjectionMatrix(bounds.left, bounds.right, bounds.top, bounds.bottom, cam.near, cam.far, out);
    }
    return scenePBRProjectionMatrix(cam.fov, aspect, cam.near, cam.far, out);
  }

  // --- Shadow Map Infrastructure ---

  // Build a perspective light-space matrix for ONE authored spot light.
  // Returns null when the spot cannot be projected faithfully; callers treat
  // null as "this spot takes no shadow slot and leaves no stale enabled
  // shadow":
  //   - position or direction missing/non-finite, or an exactly zero
  //     direction. Zero directions reaching this helper are never silently
  //     re-aimed here. Note these rules apply to the values as they arrive
  //     at this helper: callers that route through the public
  //     normalizeSceneLight first get zero-direction and nonpositive-angle
  //     defaults applied before this rejection runs, so a user-authored
  //     zero angle or zero direction is not necessarily rejected end-to-end;
  //   - Angle <= 0 or non-finite (as evaluated by this helper, post any
  //     caller-side normalization). An empty cone at this point shades
  //     nothing in the BRDF, so it casts no shadow and no cone is invented;
  //   - Angle >= pi/2. A single perspective map covers a half-angle strictly
  //     below 90 degrees: tan(halfFov) = tan(Angle) diverges at 90 degrees
  //     and fov = 2*Angle reaches 180 degrees there, which no planar
  //     frustum can contain. Cones that wide would need multiple faces
  //     (cube-style), which the one-map-per-spot budget cannot provide.
  //     This limitation is reported as null, never papered over by widening
  //     or re-aiming the authored cone.
  //
  // Direction normalization is overflow/underflow safe: components are
  // scaled by the largest magnitude BEFORE squaring, so very large finite
  // directions (1e200-scale) cannot overflow the square-sum to Infinity and
  // very small finite ones (1e-200-scale) cannot underflow to a rejected
  // zero. This guarantee covers the JS-side helper only: GPU float32 light
  // packing and the BRDF are not certified for such magnitudes.
  //
  // Far plane: the largest finite depth of the 8 scene-bounds corners along
  // the light direction (scene geometry gives coverage without author
  // input), floored to a small positive fallback and capped by a finite
  // positive Range. Nonpositive Range is unbounded in the current BRDF and
  // keeps the geometry-derived far plane. Near plane (documented finite
  // strategy): near = clamp(far * 0.001, 0.01, 0.1); if that lands at or
  // beyond far (degenerate bounds), near falls back to far/2. Geometry
  // CLOSER to the
  // light than this near plane lies outside the shadow clip volume and is
  // not shadow-covered: near is small relative to the scene extent, but
  // geometry arbitrarily close to the eye is NOT claimed to remain covered.
  //
  // The eye itself maps to w = 0 (never w > 0): the view Z row carries
  // -forward and the perspective w row is -viewZ, so every point in front of
  // the light gets w equal to its positive forward distance, and two points
  // on one forward ray share projected XY and order in depth.
  function sceneSpotShadowLightSpaceMatrix(light, sceneBounds) {
    var px = sceneNumber(light.x, NaN);
    var py = sceneNumber(light.y, NaN);
    var pz = sceneNumber(light.z, NaN);
    var dx = sceneNumber(light.directionX, NaN);
    var dy = sceneNumber(light.directionY, NaN);
    var dz = sceneNumber(light.directionZ, NaN);
    var angle = sceneNumber(light.angle, 0);
    var range = sceneNumber(light.range, 0);
    if (!isFinite(px) || !isFinite(py) || !isFinite(pz)) return null;
    if (!isFinite(dx) || !isFinite(dy) || !isFinite(dz)) return null;
    // Overflow/underflow-safe normalization (see comment above): ratios to
    // the max-magnitude component stay in [-1, 1] for ANY finite input, and
    // the sub-norm sqrt stays in [1, sqrt(3)], so no square overflows and no
    // small nonzero direction is mistaken for zero.
    var scale = Math.max(Math.abs(dx), Math.abs(dy), Math.abs(dz));
    if (!(scale > 0)) return null; // exactly zero direction: never re-aimed
    var sx = dx / scale, sy = dy / scale, sz = dz / scale;
    var sub = Math.sqrt(sx * sx + sy * sy + sz * sz);
    dx = sx / sub; dy = sy / sub; dz = sz / sub;
    if (!isFinite(angle) || !(angle > 0)) return null; // empty/invalid cone
    if (angle >= Math.PI / 2) return null; // single-perspective limit

    // Far plane along the light axis from the scene geometry bounds.
    var far = 0;
    if (
      sceneBounds &&
      isFinite(sceneBounds.minX) && isFinite(sceneBounds.maxX) &&
      isFinite(sceneBounds.minY) && isFinite(sceneBounds.maxY) &&
      isFinite(sceneBounds.minZ) && isFinite(sceneBounds.maxZ)
    ) {
      for (var corner = 0; corner < 8; corner++) {
        var bx = (corner & 1) ? sceneBounds.maxX : sceneBounds.minX;
        var by = (corner & 2) ? sceneBounds.maxY : sceneBounds.minY;
        var bz = (corner & 4) ? sceneBounds.maxZ : sceneBounds.minZ;
        var depth = (bx - px) * dx + (by - py) * dy + (bz - pz) * dz;
        if (isFinite(depth) && depth > far) far = depth;
      }
    }
    if (!(far > 0)) far = 10; // degenerate/absent bounds fallback
    if (range > 0 && isFinite(range)) far = Math.min(far, range);
    if (!isFinite(far) || far <= 0) return null;
    var near = Math.min(0.1, Math.max(0.01, far * 0.001));
    if (near >= far) near = far * 0.5;

    // LookAt view from the light position along its authored direction. Same
    // row convention as the directional fit; vertical directions swap the up
    // basis instead of re-aiming the authored direction (forward x up gives
    // right = (-1, 0, 0) for a straight-down spot, so the X row is negative).
    var fx = dx, fy = dy, fz = dz;
    var upX = 0, upY = 1, upZ = 0;
    if (Math.abs(fy) > 0.99) {
      upX = 0; upY = 0; upZ = 1;
    }
    var rx = fy * upZ - fz * upY;
    var ry = fz * upX - fx * upZ;
    var rz = fx * upY - fy * upX;
    var rLen = Math.sqrt(rx * rx + ry * ry + rz * rz);
    if (rLen < 0.0001) rLen = 1;
    rx /= rLen; ry /= rLen; rz /= rLen;
    upX = ry * fz - rz * fy;
    upY = rz * fx - rx * fz;
    upZ = rx * fy - ry * fx;

    var tx = -(rx * px + ry * py + rz * pz);
    var ty = -(upX * px + upY * py + upZ * pz);
    var tz = fx * px + fy * py + fz * pz;

    var view = new Float32Array([
      rx, upX, -fx, 0,
      ry, upY, -fy, 0,
      rz, upZ, -fz, 0,
      tx, ty, tz, 1,
    ]);

    // Perspective projection, aspect 1, vertical fov = 2*Angle (radians,
    // authored cone semantics preserved — never widened). tan(Angle) is
    // finite by the angle guard, but intermediate entries can still go
    // non-finite through pathological near/far magnitudes; the final
    // float32 finiteness sweep rejects any such matrix.
    var tanHalf = Math.tan(angle);
    var rangeInv = 1 / (near - far);
    var proj = new Float32Array([
      1 / tanHalf, 0, 0, 0,
      0, 1 / tanHalf, 0, 0,
      0, 0, (near + far) * rangeInv, -1,
      0, 0, 2 * near * far * rangeInv, 0,
    ]);

    var m = sceneMat4Multiply(proj, view);
    for (var i = 0; i < 16; i++) {
      if (!isFinite(m[i])) return null;
    }
    return m;
  }

  // Compute an orthographic light-space matrix for a directional light.
  // sceneBounds is { minX, minY, minZ, maxX, maxY, maxZ }.
  function sceneShadowLightSpaceMatrix(light, sceneBounds) {
    // Spot lights route through the perspective builder via this existing
    // exported entry point, so both backends — and the lazy WebGPU chunk's
    // existing sceneApi bridge — gain spot shadows with no new global API
    // export. Directional inputs keep the orthographic path unchanged.
    if (light && typeof light.kind === "string" &&
        light.kind.toLowerCase() === "spot") {
      return sceneSpotShadowLightSpaceMatrix(light, sceneBounds);
    }
    // Light direction (normalized).
    var dx = sceneNumber(light.directionX, 0);
    var dy = sceneNumber(light.directionY, -1);
    var dz = sceneNumber(light.directionZ, 0);
    var len = Math.sqrt(dx * dx + dy * dy + dz * dz);
    if (len < 0.0001) {
      dx = 0; dy = -1; dz = 0; len = 1;
    }
    dx /= len; dy /= len; dz /= len;

    // Scene center and radius from AABB.
    var cx = (sceneBounds.minX + sceneBounds.maxX) * 0.5;
    var cy = (sceneBounds.minY + sceneBounds.maxY) * 0.5;
    var cz = (sceneBounds.minZ + sceneBounds.maxZ) * 0.5;
    var ex = (sceneBounds.maxX - sceneBounds.minX) * 0.5;
    var ey = (sceneBounds.maxY - sceneBounds.minY) * 0.5;
    var ez = (sceneBounds.maxZ - sceneBounds.minZ) * 0.5;
    var radius = Math.sqrt(ex * ex + ey * ey + ez * ez);
    if (radius < 0.01) radius = 10;

    // Position the light camera behind the scene center along the light direction.
    var eyeX = cx - dx * radius * 2;
    var eyeY = cy - dy * radius * 2;
    var eyeZ = cz - dz * radius * 2;

    // Build a lookAt view matrix (light looking at scene center).
    // Forward = normalize(center - eye) = (dx, dy, dz).
    var fx = dx, fy = dy, fz = dz;

    // Choose an up vector not parallel to forward.
    var upX = 0, upY = 1, upZ = 0;
    if (Math.abs(fy) > 0.99) {
      upX = 0; upY = 0; upZ = 1;
    }

    // Right = normalize(forward x up).
    var rx = fy * upZ - fz * upY;
    var ry = fz * upX - fx * upZ;
    var rz = fx * upY - fy * upX;
    var rLen = Math.sqrt(rx * rx + ry * ry + rz * rz);
    if (rLen < 0.0001) rLen = 1;
    rx /= rLen; ry /= rLen; rz /= rLen;

    // Recompute up = right x forward.
    upX = ry * fz - rz * fy;
    upY = rz * fx - rx * fz;
    upZ = rx * fy - ry * fx;

    // View matrix (column-major).
    var tx = -(rx * eyeX + ry * eyeY + rz * eyeZ);
    var ty = -(upX * eyeX + upY * eyeY + upZ * eyeZ);
    var tz = fx * eyeX + fy * eyeY + fz * eyeZ;

    // Standard lookAt: the view Z row uses the negated forward vector
    // (including the translation term), so that geometry in front of the
    // light camera lands at negative view Z where the orthographic
    // projection below expects it. The X/Y rows and the projection matrix
    // are unchanged.
    var view = new Float32Array([
      rx,  upX, -fx,  0,
      ry,  upY, -fy,  0,
      rz,  upZ, -fz,  0,
      tx,  ty,  tz,  1,
    ]);

    // Orthographic projection matrix (column-major).
    // Maps [-radius, radius] in all axes to [-1, 1] clip space.
    var near = 0.01;
    var far = radius * 4;
    var l = -radius, rr = radius, b = -radius, t = radius;
    var proj = new Float32Array([
      2 / (rr - l),     0,              0,                    0,
      0,                2 / (t - b),    0,                    0,
      0,                0,              -2 / (far - near),    0,
      -(rr + l) / (rr - l), -(t + b) / (t - b), -(far + near) / (far - near), 1,
    ]);

    // Multiply proj * view (column-major).
    return sceneMat4Multiply(proj, view);
  }

  // Compute the AABB of all objects in the bundle.
  function sceneShadowComputeBounds(bundle, getInstancedGeometry) {
    var minX = Infinity, minY = Infinity, minZ = Infinity;
    var maxX = -Infinity, maxY = -Infinity, maxZ = -Infinity;
    var positions = bundle.worldMeshPositions;
    var objects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];

    for (var i = 0; i < objects.length; i++) {
      var obj = objects[i];
      if (!obj || obj.viewCulled) continue;
      if (obj.directVertices) {
        if (!obj.castShadow) continue;
        // GPU-skinned casters take precedence over retained bind bounds: the
        // GPU color path recognizes skin + streams, so when a caster carries
        // both, its bind-pose bounds describe the wrong geometry. Skinned
        // casters deform on the GPU, so obj.bounds (bind-pose or stale local)
        // does not describe the posed geometry. Fold a conservative world
        // bound: cached bind-pose per-joint influence AABBs transformed by
        // the live model*joint matrices on every fit. Posed bounds are never
        // cached because joint palettes mutate in place.
        var skinObj = obj.skin;
        var skinPalette = skinObj && skinObj.jointMatrices;
        // skin.jointMatrices is ONE FLAT Float32Array of jointCount*16 floats
        // (never an array of matrices): jointCount = floor(length / 16).
        var skinJointCount = skinPalette && typeof skinPalette.length === "number"
          ? Math.floor(skinPalette.length / 16)
          : 0;
        var skinVerts = obj.vertices;
        var skinPositions = skinVerts && skinVerts.positions;
        var skinJoints = skinVerts && skinVerts.joints;
        var skinWeights = skinVerts && skinVerts.weights;
        var skinModel = obj.modelMatrix;
        var skinCountRaw = skinVerts && skinVerts.count != null
          ? skinVerts.count
          : (obj.vertexCount != null ? obj.vertexCount : 0);
        var skinCount = Math.floor(Number(skinCountRaw));
        var isSkinnedCaster = Boolean(
          skinJointCount > 0 &&
          typeof skinPalette[0] === "number" &&
          skinPositions && typeof skinPositions.length === "number" && skinPositions.length > 0 &&
          skinJoints && typeof skinJoints.length === "number" && skinJoints.length > 0 &&
          skinWeights && typeof skinWeights.length === "number" && skinWeights.length > 0 &&
          skinModel && skinModel.length >= 16 &&
          Number.isFinite(skinCount) && skinCount > 0
        );
        if (isSkinnedCaster) {
          var skinRevision = -1;
          if (obj.geometryRevision != null) skinRevision = Math.floor(Number(obj.geometryRevision));
          else if (skinVerts.revision != null) skinRevision = Math.floor(Number(skinVerts.revision));
          if (!Number.isFinite(skinRevision) || skinRevision < 0) skinRevision = -1;
          var influenceCache = skinVerts._gosxSkinnedShadowInfluences;
          if (
            !influenceCache ||
            influenceCache.positions !== skinPositions ||
            influenceCache.joints !== skinJoints ||
            influenceCache.weights !== skinWeights ||
            influenceCache.count !== skinCount ||
            influenceCache.revision !== skinRevision ||
            influenceCache.jointCount !== skinJointCount
          ) {
            influenceCache = sceneSkinnedShadowInfluenceAABBs(
              skinPositions, skinJoints, skinWeights, skinCount, skinJointCount, skinRevision
            );
            skinVerts._gosxSkinnedShadowInfluences = influenceCache;
          }
          var skinInfluences = influenceCache.influences;
          for (var jointSlot = 0; jointSlot < skinInfluences.length; jointSlot++) {
            var influence = skinInfluences[jointSlot];
            if (!influence) continue;
            var jointOffset = jointSlot * 16;
            for (var influenceCorner = 0; influenceCorner < 8; influenceCorner++) {
              var localX = (influenceCorner & 1) ? influence.maxX : influence.minX;
              var localY = (influenceCorner & 2) ? influence.maxY : influence.minY;
              var localZ = (influenceCorner & 4) ? influence.maxZ : influence.minZ;
              var jointX = skinPalette[jointOffset] * localX + skinPalette[jointOffset + 4] * localY + skinPalette[jointOffset + 8] * localZ + skinPalette[jointOffset + 12];
              var jointY = skinPalette[jointOffset + 1] * localX + skinPalette[jointOffset + 5] * localY + skinPalette[jointOffset + 9] * localZ + skinPalette[jointOffset + 13];
              var jointZ = skinPalette[jointOffset + 2] * localX + skinPalette[jointOffset + 6] * localY + skinPalette[jointOffset + 10] * localZ + skinPalette[jointOffset + 14];
              var worldX = skinModel[0] * jointX + skinModel[4] * jointY + skinModel[8] * jointZ + skinModel[12];
              var worldY = skinModel[1] * jointX + skinModel[5] * jointY + skinModel[9] * jointZ + skinModel[13];
              var worldZ = skinModel[2] * jointX + skinModel[6] * jointY + skinModel[10] * jointZ + skinModel[14];
              if (!isFinite(worldX) || !isFinite(worldY) || !isFinite(worldZ)) continue;
              if (worldX < minX) minX = worldX;
              if (worldY < minY) minY = worldY;
              if (worldZ < minZ) minZ = worldZ;
              if (worldX > maxX) maxX = worldX;
              if (worldY > maxY) maxY = worldY;
              if (worldZ > maxZ) maxZ = worldZ;
            }
          }
          continue;
        }
        // Retained casters draw from model-space vertex buffers and never land
        // in the baked soup, so fold their transformed world bounds into the
        // light-frustum fit instead of skipping them outright.
        if (!obj.retainedGeometry) continue;
        var casterBounds = obj.bounds;
        if (
          casterBounds &&
          isFinite(casterBounds.minX) && isFinite(casterBounds.minY) && isFinite(casterBounds.minZ) &&
          isFinite(casterBounds.maxX) && isFinite(casterBounds.maxY) && isFinite(casterBounds.maxZ)
        ) {
          if (casterBounds.minX < minX) minX = casterBounds.minX;
          if (casterBounds.minY < minY) minY = casterBounds.minY;
          if (casterBounds.minZ < minZ) minZ = casterBounds.minZ;
          if (casterBounds.maxX > maxX) maxX = casterBounds.maxX;
          if (casterBounds.maxY > maxY) maxY = casterBounds.maxY;
          if (casterBounds.maxZ > maxZ) maxZ = casterBounds.maxZ;
        }
        continue;
      }
      var offset = obj.vertexOffset;
      var count = obj.vertexCount;
      if (!Number.isFinite(offset) || !Number.isFinite(count) || count <= 0) continue;

      for (var v = 0; v < count; v++) {
        var idx = (offset + v) * 3;
        var px = positions[idx];
        var py = positions[idx + 1];
        var pz = positions[idx + 2];
        if (px < minX) minX = px;
        if (py < minY) minY = py;
        if (pz < minZ) minZ = pz;
        if (px > maxX) maxX = px;
        if (py > maxY) maxY = py;
        if (pz > maxZ) maxZ = pz;
      }
    }

    // Instanced casters: fold conservative transformed bounds into the light
    // fit even when no soup geometry exists. The resolver (supplied by the
    // WebGL renderer) returns the actual cached instanced geometry so the
    // bounds cannot drift from what is drawn; the local AABB is cached on
    // that renderer-owned geometry object, so it is disposed alongside the
    // renderer's instanced geometry cache. Each authored instance's 8 local
    // AABB corners are transformed — a conservative bound of the transformed
    // geometry, not exact per-triangle bounds — reading the raw authored
    // transforms with no extra allocation. Offscreen instances matter:
    // viewCulled is deliberately not consulted here.
    if (typeof getInstancedGeometry === "function") {
      var instancedMeshes = Array.isArray(bundle.instancedMeshes) ? bundle.instancedMeshes : [];
      for (var im = 0; im < instancedMeshes.length; im++) {
        var instMesh = instancedMeshes[im];
        if (!instMesh || !instMesh.castShadow) continue;
        // Read the raw authored transforms (or the carried cached view)
        // directly — no per-frame allocation — and only consider the
        // matrices actually drawn: the authored instance count, floored,
        // capped by the matrices available.
        var instTransforms = instMesh._cachedTransforms || instMesh.transforms;
        if (!instTransforms || typeof instTransforms.length !== "number") continue;
        var instanceTotal = Math.max(0, Math.floor(sceneNumber(instMesh.instanceCount, sceneNumber(instMesh.count, 0))));
        instanceTotal = Math.min(instanceTotal, Math.floor(instTransforms.length / 16));
        if (instanceTotal <= 0) continue;
        var instGeom = getInstancedGeometry(instMesh);
        if (!instGeom || !instGeom.positions || !(instGeom.vertexCount > 0)) continue;
        var instAABB = instGeom._gosxShadowLocalAABB;
        if (!instAABB) {
          var gminX = Infinity, gminY = Infinity, gminZ = Infinity;
          var gmaxX = -Infinity, gmaxY = -Infinity, gmaxZ = -Infinity;
          var posLimit = Math.min(instGeom.positions.length, instGeom.vertexCount * 3);
          for (var gp = 0; gp + 2 < posLimit; gp += 3) {
            var gx = instGeom.positions[gp];
            var gy = instGeom.positions[gp + 1];
            var gz = instGeom.positions[gp + 2];
            if (gx < gminX) gminX = gx;
            if (gy < gminY) gminY = gy;
            if (gz < gminZ) gminZ = gz;
            if (gx > gmaxX) gmaxX = gx;
            if (gy > gmaxY) gmaxY = gy;
            if (gz > gmaxZ) gmaxZ = gz;
          }
          if (!isFinite(gminX)) continue;
          instAABB = { minX: gminX, minY: gminY, minZ: gminZ, maxX: gmaxX, maxY: gmaxY, maxZ: gmaxZ };
          instGeom._gosxShadowLocalAABB = instAABB;
        }
        for (var inst = 0; inst < instanceTotal; inst++) {
          var tb = inst * 16;
          for (var corner = 0; corner < 8; corner++) {
            var cx = (corner & 1) ? instAABB.maxX : instAABB.minX;
            var cy = (corner & 2) ? instAABB.maxY : instAABB.minY;
            var cz = (corner & 4) ? instAABB.maxZ : instAABB.minZ;
            var wx = instTransforms[tb] * cx + instTransforms[tb + 4] * cy + instTransforms[tb + 8] * cz + instTransforms[tb + 12];
            var wy = instTransforms[tb + 1] * cx + instTransforms[tb + 5] * cy + instTransforms[tb + 9] * cz + instTransforms[tb + 13];
            var wz = instTransforms[tb + 2] * cx + instTransforms[tb + 6] * cy + instTransforms[tb + 10] * cz + instTransforms[tb + 14];
            if (wx < minX) minX = wx;
            if (wy < minY) minY = wy;
            if (wz < minZ) minZ = wz;
            if (wx > maxX) maxX = wx;
            if (wy > maxY) maxY = wy;
            if (wz > maxZ) maxZ = wz;
          }
        }
      }
    }

    if (!isFinite(minX)) {
      return { minX: -10, minY: -10, minZ: -10, maxX: 10, maxY: 10, maxZ: 10 };
    }
    return { minX: minX, minY: minY, minZ: minZ, maxX: maxX, maxY: maxY, maxZ: maxZ };
  }

  // Conservative bind-pose influence AABBs for one GPU-skinned shadow caster,
  // cached on the vertices object by sceneShadowComputeBounds. The cache is
  // keyed by stream identities, vertex count, explicit geometry revision and
  // the joint count (floor(jointMatrices.length / 16) over the FLAT palette
  // Float32Array) so stream replacement or a revision bump invalidates it.
  // Zero and negative weights are not influences (glTF weights are normalized
  // and nonnegative); malformed vertices, weights or joint indices are skipped
  // rather than poisoning the boxes. Posed bounds are deliberately not cached:
  // joint palettes mutate in place, so the corners are transformed by the live
  // model*joint matrices on every light fit. The palette is read at its full
  // flat length — skeletons larger than the GL uniform cap (WGSL supports
  // more) are never truncated here.
  function sceneSkinnedShadowInfluenceAABBs(positions, joints, weights, count, jointCount, revision) {
    var influences = new Array(jointCount);
    var posLimit = Math.min(count * 3, positions.length);
    var attrLimit = Math.min(count * 4, weights.length, joints.length);
    for (var v = 0; v < count; v++) {
      var p = v * 3;
      var q = v * 4;
      if (p + 2 >= posLimit || q + 3 >= attrLimit) break;
      var px = positions[p];
      var py = positions[p + 1];
      var pz = positions[p + 2];
      if (!Number.isFinite(px) || !Number.isFinite(py) || !Number.isFinite(pz)) continue;
      for (var k = 0; k < 4; k++) {
        var w = Number(weights[q + k]);
        if (!Number.isFinite(w) || w <= 0) continue;
        var jointIndex = Math.floor(Number(joints[q + k]));
        if (!Number.isFinite(jointIndex) || jointIndex < 0 || jointIndex >= jointCount) continue;
        var box = influences[jointIndex];
        if (!box) {
          influences[jointIndex] = { minX: px, minY: py, minZ: pz, maxX: px, maxY: py, maxZ: pz };
        } else {
          if (px < box.minX) box.minX = px;
          if (py < box.minY) box.minY = py;
          if (pz < box.minZ) box.minZ = pz;
          if (px > box.maxX) box.maxX = px;
          if (py > box.maxY) box.maxY = py;
          if (pz > box.maxZ) box.maxZ = pz;
        }
      }
    }
    return {
      positions: positions,
      joints: joints,
      weights: weights,
      count: count,
      revision: revision,
      jointCount: jointCount,
      influences: influences,
    };
  }

  // Generate PBR vertex data (positions, normals, UVs, tangents) for a geometry
  // kind. Returns { positions, normals, uvs, tangents, vertexCount } where each
  // array is a Float32Array ready for GPU upload.
  function generateInstancedGeometry(kind, dims) {
    kind = normalizeInstancedGeometryKind(kind);
    var w = sceneNumber(dims && dims.width, 1);
    var h = sceneNumber(dims && dims.height, 1);
    var d = sceneNumber(dims && dims.depth, 1);
    var size = sceneNumber(dims && dims.size, 0);
    if (kind === "cube" && size > 0) {
      w = size;
      h = size;
      d = size;
    }

    if (kind === "sphere") {
      return generateInstancedSphereGeometry(
        sceneNumber(dims && dims.radius, 0.5),
        sceneNumber(dims && dims.segments, 32)
      );
    }
    if (kind === "plane") {
      return generateInstancedPlaneGeometry(w, d);
    }
    if (kind === "pyramid") {
      return generateInstancedPyramidGeometry(w, h, d);
    }
    if (kind === "cylinder") {
      return generateInstancedCylinderGeometry(
        sceneNumber(dims && dims.radiusTop, sceneNumber(dims && dims.radius, 0.5)),
        sceneNumber(dims && dims.radiusBottom, sceneNumber(dims && dims.radius, 0.5)),
        h,
        sceneNumber(dims && dims.segments, 32)
      );
    }
    if (kind === "cone") {
      return generateInstancedCylinderGeometry(
        0,
        sceneNumber(dims && dims.radiusBottom, sceneNumber(dims && dims.radius, 0.5)),
        h,
        sceneNumber(dims && dims.segments, 32)
      );
    }
    if (kind === "torus") {
      return generateInstancedTorusGeometry(
        sceneNumber(dims && dims.radius, 0.7),
        sceneNumber(dims && dims.tube, 0.3),
        sceneNumber(dims && dims.radialSegments, 32),
        sceneNumber(dims && dims.tubularSegments, 16)
      );
    }

    // Default: box geometry.
    return generateInstancedBoxGeometry(w, h, d);
  }

  function normalizeInstancedGeometryKind(kind) {
    if (typeof normalizeSceneKind === "function") {
      return normalizeSceneKind(kind);
    }
    var text = typeof kind === "string" ? kind.trim().toLowerCase() : "";
    switch (text) {
      case "cubegeometry":
        return "cube";
      case "boxgeometry":
        return "box";
      case "planegeometry":
      case "quad":
      case "quadgeometry":
        return "plane";
      case "pyramidgeometry":
        return "pyramid";
      case "spheregeometry":
      case "uvsphere":
      case "uvspheregeometry":
        return "sphere";
      case "cylindergeometry":
        return "cylinder";
      case "conegeometry":
        return "cone";
      case "torusgeometry":
        return "torus";
      case "torusknotgeometry":
      case "torus-knot":
        return "torusknot";
      default:
        return text || "box";
    }
  }

  // Generate a unit box with the given dimensions. 36 vertices (12 triangles).
  // Each face has outward normals, [0,1] UVs, and MikkTSpace-compatible tangents.
  function generateInstancedBoxGeometry(w, h, d) {
    var hw = w * 0.5, hh = h * 0.5, hd = d * 0.5;

    // 6 faces × 2 triangles × 3 vertices = 36 vertices.
    // Each face: normal, tangent(vec4), 4 corners → 2 triangles.
    var faces = [
      // +Z face (front)
      { n: [0, 0, 1], t: [1, 0, 0, 1], v: [[-hw,-hh,hd],[hw,-hh,hd],[hw,hh,hd],[-hw,hh,hd]] },
      // -Z face (back)
      { n: [0, 0,-1], t: [-1, 0, 0, 1], v: [[hw,-hh,-hd],[-hw,-hh,-hd],[-hw,hh,-hd],[hw,hh,-hd]] },
      // +X face (right)
      { n: [1, 0, 0], t: [0, 0,-1, 1], v: [[hw,-hh,hd],[hw,-hh,-hd],[hw,hh,-hd],[hw,hh,hd]] },
      // -X face (left)
      { n: [-1, 0, 0], t: [0, 0, 1, 1], v: [[-hw,-hh,-hd],[-hw,-hh,hd],[-hw,hh,hd],[-hw,hh,-hd]] },
      // +Y face (top)
      { n: [0, 1, 0], t: [1, 0, 0, 1], v: [[-hw,hh,hd],[hw,hh,hd],[hw,hh,-hd],[-hw,hh,-hd]] },
      // -Y face (bottom)
      { n: [0,-1, 0], t: [1, 0, 0, 1], v: [[-hw,-hh,-hd],[hw,-hh,-hd],[hw,-hh,hd],[-hw,-hh,hd]] },
    ];

    var quadUVs = [[0,0],[1,0],[1,1],[0,1]];
    var triIndices = [0,1,2, 0,2,3];

    var vertexCount = 36;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);

    var vi = 0;
    for (var fi = 0; fi < 6; fi++) {
      var face = faces[fi];
      for (var ti = 0; ti < 6; ti++) {
        var ci = triIndices[ti];
        var p = face.v[ci];
        positions[vi * 3]     = p[0];
        positions[vi * 3 + 1] = p[1];
        positions[vi * 3 + 2] = p[2];
        normals[vi * 3]     = face.n[0];
        normals[vi * 3 + 1] = face.n[1];
        normals[vi * 3 + 2] = face.n[2];
        uvs[vi * 2]     = quadUVs[ci][0];
        uvs[vi * 2 + 1] = quadUVs[ci][1];
        tangents[vi * 4]     = face.t[0];
        tangents[vi * 4 + 1] = face.t[1];
        tangents[vi * 4 + 2] = face.t[2];
        tangents[vi * 4 + 3] = face.t[3];
        vi++;
      }
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vertexCount };
  }

  // Generate a plane (quad) with the given width and depth, lying in the XZ plane.
  // 6 vertices (2 triangles), face normal pointing up (+Y).
  function generateInstancedPlaneGeometry(w, d) {
    var hw = w * 0.5, hd = d * 0.5;
    var vertexCount = 6;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);

    var corners = [[-hw, 0, hd], [hw, 0, hd], [hw, 0, -hd], [-hw, 0, -hd]];
    var cornerUVs = [[0, 0], [1, 0], [1, 1], [0, 1]];
    var triIndices = [0, 1, 2, 0, 2, 3];

    for (var i = 0; i < 6; i++) {
      var ci = triIndices[i];
      var p = corners[ci];
      positions[i * 3] = p[0]; positions[i * 3 + 1] = p[1]; positions[i * 3 + 2] = p[2];
      normals[i * 3] = 0; normals[i * 3 + 1] = 1; normals[i * 3 + 2] = 0;
      uvs[i * 2] = cornerUVs[ci][0]; uvs[i * 2 + 1] = cornerUVs[ci][1];
      tangents[i * 4] = 1; tangents[i * 4 + 1] = 0; tangents[i * 4 + 2] = 0; tangents[i * 4 + 3] = 1;
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vertexCount };
  }

  // Generate a UV sphere with the given radius and segment count.
  function generateInstancedSphereGeometry(radius, segments) {
    var slices = instancedSegmentCount(segments, 32, 3, 256);
    var rings = Math.max(2, Math.floor(slices / 2));

    // Count: each ring-slice quad = 2 triangles = 6 vertices,
    // except the top and bottom caps which are single triangles.
    var vertexCount = rings * slices * 6;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);
    var vi = 0;

    function spherePoint(ring, slice) {
      var phi = (ring / rings) * Math.PI;
      var theta = (slice / slices) * Math.PI * 2;
      var sp = Math.sin(phi);
      var nx = sp * Math.cos(theta);
      var ny = Math.cos(phi);
      var nz = sp * Math.sin(theta);
      return {
        px: nx * radius, py: ny * radius, pz: nz * radius,
        nx: nx, ny: ny, nz: nz,
        u: slice / slices, v: ring / rings,
        tx: -Math.sin(theta), ty: 0, tz: Math.cos(theta),
      };
    }

    function pushVert(pt) {
      positions[vi * 3] = pt.px; positions[vi * 3 + 1] = pt.py; positions[vi * 3 + 2] = pt.pz;
      normals[vi * 3] = pt.nx; normals[vi * 3 + 1] = pt.ny; normals[vi * 3 + 2] = pt.nz;
      uvs[vi * 2] = pt.u; uvs[vi * 2 + 1] = pt.v;
      tangents[vi * 4] = pt.tx; tangents[vi * 4 + 1] = pt.ty; tangents[vi * 4 + 2] = pt.tz; tangents[vi * 4 + 3] = 1;
      vi++;
    }

    for (var r = 0; r < rings; r++) {
      for (var s = 0; s < slices; s++) {
        var a = spherePoint(r, s);
        var b = spherePoint(r, s + 1);
        var c = spherePoint(r + 1, s + 1);
        var dd = spherePoint(r + 1, s);
        pushVert(a); pushVert(b); pushVert(c);
        pushVert(a); pushVert(c); pushVert(dd);
      }
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vi };
  }

  function instancedSegmentCount(value, fallback, minValue, maxValue) {
    var count = Math.round(sceneNumber(value, fallback));
    return Math.max(minValue, Math.min(maxValue, count));
  }

  function instancedPositiveNumber(value, fallback) {
    var number = sceneNumber(value, fallback);
    return number > 0 ? number : fallback;
  }

  function instancedNormalize3(x, y, z) {
    var length = Math.sqrt(x * x + y * y + z * z);
    if (!Number.isFinite(length) || length <= 0.000001) {
      return [0, 1, 0];
    }
    return [x / length, y / length, z / length];
  }

  function instancedTriangleNormal(a, b, c) {
    var abx = b[0] - a[0];
    var aby = b[1] - a[1];
    var abz = b[2] - a[2];
    var acx = c[0] - a[0];
    var acy = c[1] - a[1];
    var acz = c[2] - a[2];
    return instancedNormalize3(
      aby * acz - abz * acy,
      abz * acx - abx * acz,
      abx * acy - aby * acx
    );
  }

  function createInstancedGeometryWriter(vertexCount) {
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);
    var vi = 0;
    function push(position, normal, uv, tangent) {
      positions[vi * 3] = position[0];
      positions[vi * 3 + 1] = position[1];
      positions[vi * 3 + 2] = position[2];
      normals[vi * 3] = normal[0];
      normals[vi * 3 + 1] = normal[1];
      normals[vi * 3 + 2] = normal[2];
      uvs[vi * 2] = uv[0];
      uvs[vi * 2 + 1] = uv[1];
      tangents[vi * 4] = tangent[0];
      tangents[vi * 4 + 1] = tangent[1];
      tangents[vi * 4 + 2] = tangent[2];
      tangents[vi * 4 + 3] = tangent[3];
      vi++;
    }
    function build() {
      return {
        positions: vi * 3 === positions.length ? positions : positions.subarray(0, vi * 3),
        normals: vi * 3 === normals.length ? normals : normals.subarray(0, vi * 3),
        uvs: vi * 2 === uvs.length ? uvs : uvs.subarray(0, vi * 2),
        tangents: vi * 4 === tangents.length ? tangents : tangents.subarray(0, vi * 4),
        vertexCount: vi,
      };
    }
    return { push: push, build: build };
  }

  function pushInstancedFlatTri(writer, p0, p1, p2, uv0, uv1, uv2) {
    var normal = instancedTriangleNormal(p0, p1, p2);
    var tangent3 = instancedNormalize3(p1[0] - p0[0], p1[1] - p0[1], p1[2] - p0[2]);
    var tangent = [tangent3[0], tangent3[1], tangent3[2], 1];
    writer.push(p0, normal, uv0, tangent);
    writer.push(p1, normal, uv1, tangent);
    writer.push(p2, normal, uv2, tangent);
  }

  function generateInstancedPyramidGeometry(w, h, d) {
    var hw = instancedPositiveNumber(w, 1) * 0.5;
    var hh = instancedPositiveNumber(h, 1) * 0.5;
    var hd = instancedPositiveNumber(d, 1) * 0.5;
    var base = [[-hw, -hh, -hd], [hw, -hh, -hd], [hw, -hh, hd], [-hw, -hh, hd]];
    var apex = [0, hh, 0];
    var writer = createInstancedGeometryWriter(18);

    pushInstancedFlatTri(writer, base[0], base[1], base[2], [0, 0], [1, 0], [1, 1]);
    pushInstancedFlatTri(writer, base[0], base[2], base[3], [0, 0], [1, 1], [0, 1]);
    for (var i = 0; i < 4; i++) {
      pushInstancedFlatTri(writer, base[i], apex, base[(i + 1) % 4], [0, 1], [0.5, 0], [1, 1]);
    }
    return writer.build();
  }

  function generateInstancedCylinderGeometry(radiusTop, radiusBottom, height, segments) {
    var rt = Math.max(0, sceneNumber(radiusTop, 0.5));
    var rb = Math.max(0, sceneNumber(radiusBottom, 0.5));
    var h = instancedPositiveNumber(height, 1);
    if (rt === 0 && rb === 0) {
      rb = 0.5;
    }
    var count = instancedSegmentCount(segments, 32, 3, 256);
    var vertsPerSegment = (rt > 0 && rb > 0 ? 6 : 3) + (rb > 0 ? 3 : 0) + (rt > 0 ? 3 : 0);
    var writer = createInstancedGeometryWriter(count * vertsPerSegment);
    var halfH = h * 0.5;
    var slopeY = (rb - rt) / h;

    for (var i = 0; i < count; i++) {
      var u0 = i / count;
      var u1 = (i + 1) / count;
      var th0 = (Math.PI * 2 * i) / count;
      var th1 = (Math.PI * 2 * (i + 1)) / count;
      var c0 = Math.cos(th0);
      var s0 = Math.sin(th0);
      var c1 = Math.cos(th1);
      var s1 = Math.sin(th1);
      var n0 = instancedNormalize3(c0, slopeY, s0);
      var n1 = instancedNormalize3(c1, slopeY, s1);
      var t0 = [-s0, 0, c0, 1];
      var t1 = [-s1, 0, c1, 1];
      var b0 = [rb * c0, -halfH, rb * s0];
      var b1 = [rb * c1, -halfH, rb * s1];
      var top0 = [rt * c0, halfH, rt * s0];
      var top1 = [rt * c1, halfH, rt * s1];

      if (rb > 0 && rt > 0) {
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top1, n1, [u1, 0], t1);
        writer.push(b1, n1, [u1, 1], t1);
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top0, n0, [u0, 0], t0);
        writer.push(top1, n1, [u1, 0], t1);
      } else if (rt === 0) {
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top1, n1, [u1, 0], t1);
        writer.push(b1, n1, [u1, 1], t1);
      } else {
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top0, n0, [u0, 0], t0);
        writer.push(top1, n1, [u1, 0], t1);
      }

      if (rb > 0) {
        writer.push([0, -halfH, 0], [0, -1, 0], [0.5, 0.5], [1, 0, 0, 1]);
        writer.push(b0, [0, -1, 0], [0.5 + c0 * 0.5, 0.5 + s0 * 0.5], [1, 0, 0, 1]);
        writer.push(b1, [0, -1, 0], [0.5 + c1 * 0.5, 0.5 + s1 * 0.5], [1, 0, 0, 1]);
      }
      if (rt > 0) {
        writer.push([0, halfH, 0], [0, 1, 0], [0.5, 0.5], [1, 0, 0, 1]);
        writer.push(top1, [0, 1, 0], [0.5 + c1 * 0.5, 0.5 + s1 * 0.5], [1, 0, 0, 1]);
        writer.push(top0, [0, 1, 0], [0.5 + c0 * 0.5, 0.5 + s0 * 0.5], [1, 0, 0, 1]);
      }
    }
    return writer.build();
  }

  function generateInstancedTorusGeometry(radius, tube, radialSegments, tubularSegments) {
    var major = instancedPositiveNumber(radius, 0.7);
    var minor = instancedPositiveNumber(tube, 0.3);
    var radial = instancedSegmentCount(radialSegments, 32, 3, 256);
    var tubular = instancedSegmentCount(tubularSegments, 16, 3, 128);
    var writer = createInstancedGeometryWriter(radial * tubular * 6);

    function vertexAt(i, j) {
      var u = (Math.PI * 2 * i) / radial;
      var v = (Math.PI * 2 * j) / tubular;
      var cu = Math.cos(u);
      var su = Math.sin(u);
      var cv = Math.cos(v);
      var sv = Math.sin(v);
      var r = major + minor * cv;
      return {
        position: [r * cu, minor * sv, r * su],
        normal: instancedNormalize3(cv * cu, sv, cv * su),
        uv: [i / radial, j / tubular],
        tangent: [-su, 0, cu, 1],
      };
    }

    function pushTorusVertex(v) {
      writer.push(v.position, v.normal, v.uv, v.tangent);
    }

    for (var i = 0; i < radial; i++) {
      for (var j = 0; j < tubular; j++) {
        var a = vertexAt(i, j);
        var b = vertexAt(i, j + 1);
        var c = vertexAt(i + 1, j);
        var dd = vertexAt(i + 1, j + 1);
        pushTorusVertex(a);
        pushTorusVertex(b);
        pushTorusVertex(c);
        pushTorusVertex(c);
        pushTorusVertex(b);
        pushTorusVertex(dd);
      }
    }
    return writer.build();
  }

  // Shared scratch for number → u32 bit reinterpretation used by the light
  // hash. Allocated once at module level; safe because the hash function
  // is called synchronously per upload and never recursively.
  var _scenePBRLightsHashBuf = new ArrayBuffer(4);

  var _scenePBRLightsHashFloat = new Float32Array(_scenePBRLightsHashBuf);

  var _scenePBRLightsHashInt = new Uint32Array(_scenePBRLightsHashBuf);

  function scenePBRLightsHashNumber(h, n) {
    _scenePBRLightsHashFloat[0] = (typeof n === "number" && n === n) ? n : 0;
    return Math.imul((h ^ _scenePBRLightsHashInt[0]) >>> 0, 16777619) >>> 0;
  }

  function scenePBRLightsHashString(h, s) {
    var str = (typeof s === "string") ? s : "";
    var len = str.length;
    for (var i = 0; i < len; i++) {
      h = Math.imul((h ^ str.charCodeAt(i)) >>> 0, 16777619) >>> 0;
    }
    // Length-delimit to distinguish "ab" + "c" from "a" + "bc".
    return Math.imul((h ^ (len + 1)) >>> 0, 16777619) >>> 0;
  }

  // hashLightContent computes the per-light sub-hash the frame-level
  // scenePBRLightsHash combines. Called from normalizeSceneLight (in
  // 10-runtime-scene-core.js) whenever a light is created or patched,
  // so the expensive string/number walk runs at mutation time — rare —
  // instead of per-frame. The result is stamped onto the light object
  // as `_lightHash` and read by scenePBRLightsHash without rehashing.
  //
  // Kept in 16-scene-webgl.js alongside scenePBRLightsHash so the two
  // must agree on what fields contribute to the hash; moving either
  // without the other is a correctness bug.
  function hashLightContent(l) {
    if (!l) return 0;
    var h = 2166136261;
    h = scenePBRLightsHashString(h, l.kind);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.x, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.y, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.z, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionX, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionY, -1));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionZ, 0));
    h = scenePBRLightsHashString(h, l.color);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.intensity, 1));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.range, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.decay, 2));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.angle, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.penumbra, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.width, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.height, 0));
    h = scenePBRLightsHashString(h, l.groundColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowBias, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowSize, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowCascades, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowSoftness, 0));
    // SH light-probe coefficients: hash the nine RGB triples so a live
    // coefficient edit invalidates the frame hash even when every scalar
    // field is unchanged. Nonempty malformed sets hash by length plus a
    // marker instead, so repairing them still refreshes too. Absent
    // components (Go's omitempty drops zero Vector3 channels) hash as zero
    // via scenePBRLightsHashNumber, identical to explicit zeros.
    var probeCoefs = Array.isArray(l.coefficients) ? l.coefficients : null;
    if (probeCoefs !== null && probeCoefs.length > 0) {
      h = scenePBRLightsHashNumber(h, probeCoefs.length);
      if (scenePBRProbeCoefficientsValid(probeCoefs)) {
        for (var probeI = 0; probeI < 9; probeI++) {
          h = scenePBRLightsHashNumber(h, probeCoefs[probeI].x);
          h = scenePBRLightsHashNumber(h, probeCoefs[probeI].y);
          h = scenePBRLightsHashNumber(h, probeCoefs[probeI].z);
        }
      } else {
        h = scenePBRLightsHashString(h, "sh-malformed");
      }
    }
    return h;
  }

  // -- Second-order spherical-harmonic light probes --
  //
  // LightProbe.Coefficients are nine linear-RGB SH radiance coefficients
  // (one Vector3 each, basis order 0..8). Every structurally valid probe in
  // a frame aggregates into ONE coefficient set — probe intensity is
  // multiplied in exactly once here — which both renderers upload as a
  // fixed 9-entry tail and evaluate per fragment against the world-space
  // shading normal. Probes that are not structurally valid never reach the
  // GPU as SH: they fall back to the Color/Intensity ambient of an ordinary
  // flat light, so malformed data can neither upload NaN/Infinity nor
  // contaminate the valid aggregate. Coefficientless legacy probes are not
  // represented here at all; they keep the legacy ambient path.

  // scenePBRProbeComponentValid accepts one coefficient component. Absent
  // (undefined) means zero: Go's Vector3 JSON tags omit zero channels, so
  // sparse coefficients like {} and {x:1} are valid and mean zero for the
  // missing channels. Anything else present must be a finite number that
  // survives float32 rounding — null, strings, NaN, Infinity and f64 values
  // beyond float32 range are all rejected.
  function scenePBRProbeComponentValid(v) {
    if (v === undefined) return true;
    if (typeof v !== "number") return false;
    return isFinite(v) && isFinite(Math.fround(v));
  }

  // scenePBRProbeCoefficientsValid reports whether `coefficients` is a
  // structurally valid second-order probe: exactly 9 entries (plain
  // objects, not arrays) whose present x/y/z components pass
  // scenePBRProbeComponentValid. The check is structural only — nine zero
  // coefficients are legal and shade black rather than falling back to
  // ambient.
  function scenePBRProbeCoefficientsValid(coefficients) {
    if (!Array.isArray(coefficients) || coefficients.length !== 9) {
      return false;
    }
    for (var i = 0; i < 9; i++) {
      var c = coefficients[i];
      if (!c || typeof c !== "object" || Array.isArray(c)) return false;
      if (!scenePBRProbeComponentValid(c.x)) return false;
      if (!scenePBRProbeComponentValid(c.y)) return false;
      if (!scenePBRProbeComponentValid(c.z)) return false;
    }
    return true;
  }

  // Aggregate coefficients clamp to this bound, sufficiently below F32_MAX
  // that summing the 9 SH terms per fragment in float32 stays finite even
  // when the aggregate sits at the clamp. Accumulation itself runs in
  // double precision (see _scenePBRProbeAccum below); the clamp exists so
  // the single final float32 conversion cannot produce Infinity.
  var SCENE_PBR_PROBE_AGGREGATE_MAX = 1e30;

  // Shared double-precision accumulation scratch. Adding into a Float32Array
  // can overflow to Infinity mid-sum and then add -Infinity, producing NaN
  // that no final clamp can repair. Summing in f64 and converting once after
  // all probes makes +MAX followed by -MAX cancel to zero exactly. Cleared at
  // the top of every aggregate call; safe at module level because the
  // aggregate is synchronous and never reentrant.
  var _scenePBRProbeAccum = new Float64Array(27);

  // Shared result object: scenePBRProbeAggregate runs a few times per frame
  // at most and every caller reads it synchronously, so returning this
  // keeps the aggregate allocation-free.
  var _scenePBRProbeCensus = { valid: false, malformed: 0 };

  // scenePBRProbeAggregate sums every valid SH light probe among the first
  // `count` entries of `lights` into `out` (27 floats: 9 linear-RGB
  // coefficient triples, probe intensity already multiplied in exactly
  // once) and returns { valid, malformed }: valid is true when at least one
  // valid probe contributed, malformed counts nonempty-but-invalid probes
  // that the caller reports and shades as ambient instead. A malformed probe
  // never contributes, so it cannot contaminate the aggregate. Accumulation
  // is double precision; the single conversion to `out` clamps to
  // SCENE_PBR_PROBE_AGGREGATE_MAX. Irradiance is clamped nonnegative in the
  // shader, AFTER the linear combine of all probes.
  function scenePBRProbeAggregate(lights, count, out) {
    for (var i = 0; i < 27; i++) _scenePBRProbeAccum[i] = 0;
    var valid = false;
    var malformed = 0;
    var list = Array.isArray(lights) ? lights : [];
    var limit = Math.max(0, Math.min(count | 0, list.length));
    for (var li = 0; li < limit; li++) {
      var l = list[li];
      if (!l) continue;
      var kind = typeof l.kind === "string" ? l.kind.toLowerCase() : "";
      if (kind !== "light-probe") continue;
      var coefs = l.coefficients;
      if (!Array.isArray(coefs) || coefs.length === 0) continue;
      if (!scenePBRProbeCoefficientsValid(coefs)) {
        malformed += 1;
        continue;
      }
      // normalizeSceneLight clamps intensity to 0..6, but the shared
      // aggregate also sees raw, pre-normalization lights. Clamp here to
      // the same band so a pathological finite intensity cannot push the
      // double accumulator to Infinity, whose cancellation would surface
      // as NaN that no final clamp can repair.
      var intensity = sceneNumber(l.intensity, 1);
      if (!(intensity > 0)) intensity = 0;
      else if (intensity > 6) intensity = 6;
      for (var ci = 0; ci < 9; ci++) {
        var c = coefs[ci];
        // Absent channels mean zero (Go's Vector3 JSON tags omit zero
        // channels); multiplying them directly would poison the f64 sum
        // with NaN. Valid components are finite numbers by
        // scenePBRProbeCoefficientsValid, so only undefined needs the 0.
        _scenePBRProbeAccum[ci * 3] += intensity * (c.x === undefined ? 0 : c.x);
        _scenePBRProbeAccum[ci * 3 + 1] += intensity * (c.y === undefined ? 0 : c.y);
        _scenePBRProbeAccum[ci * 3 + 2] += intensity * (c.z === undefined ? 0 : c.z);
      }
      valid = true;
    }
    for (var fi = 0; fi < 27; fi++) {
      var v = _scenePBRProbeAccum[fi];
      if (v > SCENE_PBR_PROBE_AGGREGATE_MAX) {
        v = SCENE_PBR_PROBE_AGGREGATE_MAX;
      } else if (v < -SCENE_PBR_PROBE_AGGREGATE_MAX) {
        v = -SCENE_PBR_PROBE_AGGREGATE_MAX;
      }
      out[fi] = v;
    }
    _scenePBRProbeCensus.valid = valid;
    _scenePBRProbeCensus.malformed = malformed;
    return _scenePBRProbeCensus;
  }

  // hashEnvironmentContent is the env-side counterpart to hashLightContent.
  // Called from normalizeSceneEnvironment and sceneResolveLightingEnvironment
  // whenever the environment is normalized so the cached sub-hash travels
  // with the environment object downstream.
  function hashEnvironmentContent(env) {
    if (!env) return 0;
    var h = 2166136261;
    h = scenePBRLightsHashString(h, env.ambientColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.ambientIntensity, 0));
    h = scenePBRLightsHashString(h, env.skyColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.skyIntensity, 0));
    h = scenePBRLightsHashString(h, env.groundColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.groundIntensity, 0));
    h = scenePBRLightsHashString(h, env.envMap);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.envIntensity, 1));
    h = scenePBRLightsHashNumber(h, sceneNumber(env.envRotation, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(env.fogDensity, 0));
    h = scenePBRLightsHashString(h, env.fogColor);
    return h;
  }

  // Determine the render pass for an object given its material.
  function scenePBRObjectRenderPass(obj, material) {
    // Derived object passes are cached computed defaults; freshly evaluated
    // material routing (e.g. after real CSS substitution) wins. Raw and
    // explicit object passes keep precedence.
    if (obj && obj._renderPassDerived !== true &&
        typeof obj.renderPass === "string" && obj.renderPass) {
      const pass = obj.renderPass.toLowerCase();
      if (pass === "alpha" || pass === "additive" || pass === "opaque") {
        return pass;
      }
    }
    // Derived (computed) material routes must be re-evaluated from the
    // effective fields — notably after real CSS resolution replaced a
    // var() alphaCutoff string — instead of replaying cached strings. Raw
    // unmarked values keep the legacy thresholds (opacity < 1) below.
    if (material && (material._renderPassDerived === true ||
        material._blendModeDerived === true)) {
      return sceneMaterialRenderPass(material);
    }
    if (material && typeof material.renderPass === "string" && material.renderPass) {
      const pass = material.renderPass.toLowerCase();
      if (pass === "alpha" || pass === "additive" || pass === "opaque") {
        return pass;
      }
    }
    // Preserve explicit alpha/additive blend choices before mask-default
    // routing; unmarked raw values are authored.
    const materialBlend = material && typeof material.blendMode === "string"
      ? material.blendMode.toLowerCase() : "";
    if (materialBlend === "additive") {
      return "additive";
    }
    if (materialBlend === "alpha") {
      return "alpha";
    }
    if (sceneMaterialMaskOpaqueRouting(material)) {
      return "opaque";
    }
    // If material opacity < 1, default to alpha pass.
    if (material && sceneNumber(material.opacity, 1) < 1) {
      return "alpha";
    }
    return "opaque";
  }

  // Depth-based sort comparator for translucent objects (back-to-front).
  function scenePBRDepthSort(a, b) {
    const da = sceneNumber(a && a.depthCenter, 0);
    const db = sceneNumber(b && b.depthCenter, 0);
    if (da !== db) {
      return db - da;
    }
    return String(a && a.id || "").localeCompare(String(b && b.id || ""));
  }
