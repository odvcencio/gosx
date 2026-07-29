  // Scene geometry — vertex generation for wireframe primitives.

  function sceneSegmentResolution(value) {
    const segments = Math.round(sceneNumber(value, 12));
    return Math.max(6, Math.min(24, segments));
  }

  function scenePrimitiveSegmentResolution(value, fallback, minValue, maxValue) {
    const segments = Math.round(sceneNumber(value, fallback));
    return Math.max(minValue, Math.min(maxValue, segments));
  }

  function scenePositiveNumber(value, fallback) {
    const number = sceneNumber(value, fallback);
    return number > 0 ? number : fallback;
  }

  function boxVertices(width, height, depth) {
    const halfWidth = width / 2;
    const halfHeight = height / 2;
    const halfDepth = depth / 2;
    return [
      { x: -halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: halfHeight, z: -halfDepth },
      { x: -halfWidth, y: halfHeight, z: -halfDepth },
      { x: -halfWidth, y: -halfHeight, z: halfDepth },
      { x: halfWidth, y: -halfHeight, z: halfDepth },
      { x: halfWidth, y: halfHeight, z: halfDepth },
      { x: -halfWidth, y: halfHeight, z: halfDepth },
    ];
  }

  const boxEdgePairs = [
    [0, 1], [1, 2], [2, 3], [3, 0],
    [4, 5], [5, 6], [6, 7], [7, 4],
    [0, 4], [1, 5], [2, 6], [3, 7],
  ];

  function indexSegments(points, edgePairs) {
    return edgePairs.map(function(edge) {
      return [points[edge[0]], points[edge[1]]];
    });
  }

  function boxSegments(object) {
    return indexSegments(boxVertices(object.width, object.height, object.depth), boxEdgePairs);
  }

  function planeSegments(object) {
    const vertices = boxVertices(object.width, 0, object.depth);
    return indexSegments(vertices.slice(0, 4), [
      [0, 1], [1, 2], [2, 3], [3, 0],
    ]);
  }

  function pyramidSegments(object) {
    const halfWidth = object.width / 2;
    const halfDepth = object.depth / 2;
    const halfHeight = object.height / 2;
    const vertices = [
      { x: -halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: -halfHeight, z: halfDepth },
      { x: -halfWidth, y: -halfHeight, z: halfDepth },
      { x: 0, y: halfHeight, z: 0 },
    ];
    return indexSegments(vertices, [
      [0, 1], [1, 2], [2, 3], [3, 0],
      [0, 4], [1, 4], [2, 4], [3, 4],
    ]);
  }

  function circleSegments(radius, axis, segments) {
    const points = [];
    for (let i = 0; i < segments; i += 1) {
      const angle = (Math.PI * 2 * i) / segments;
      points.push(circlePoint(radius, axis, angle));
    }
    const out = [];
    for (let i = 0; i < points.length; i += 1) {
      out.push([points[i], points[(i + 1) % points.length]]);
    }
    return out;
  }

  function circlePoint(radius, axis, angle) {
    const sin = Math.sin(angle) * radius;
    const cos = Math.cos(angle) * radius;
    switch (axis) {
      case "xy":
        return { x: cos, y: sin, z: 0 };
      case "yz":
        return { x: 0, y: cos, z: sin };
      default:
        return { x: cos, y: 0, z: sin };
    }
  }

  function sphereSegments(object) {
    return []
      .concat(circleSegments(object.radius, "xy", object.segments))
      .concat(circleSegments(object.radius, "xz", object.segments))
      .concat(circleSegments(object.radius, "yz", object.segments));
  }

  function cylinderSegments(object) {
    const segments = scenePrimitiveSegmentResolution(object && object.segments, 32, 3, 256);
    const radiusTop = scenePositiveNumber(object && object.radiusTop, scenePositiveNumber(object && object.radius, 0.5));
    const radiusBottom = scenePositiveNumber(object && object.radiusBottom, scenePositiveNumber(object && object.radius, 0.5));
    const halfHeight = scenePositiveNumber(object && object.height, 1) * 0.5;
    const bottom = [];
    const top = [];
    for (let i = 0; i < segments; i += 1) {
      const angle = (Math.PI * 2 * i) / segments;
      const cos = Math.cos(angle);
      const sin = Math.sin(angle);
      bottom.push({ x: radiusBottom * cos, y: -halfHeight, z: radiusBottom * sin });
      top.push({ x: radiusTop * cos, y: halfHeight, z: radiusTop * sin });
    }
    const out = [];
    for (let i = 0; i < segments; i += 1) {
      const next = (i + 1) % segments;
      out.push([bottom[i], bottom[next]]);
      out.push([top[i], top[next]]);
      out.push([bottom[i], top[i]]);
    }
    return out;
  }

  function coneSegments(object) {
    const segments = scenePrimitiveSegmentResolution(object && object.segments, 32, 3, 256);
    const radius = scenePositiveNumber(object && object.radiusBottom, scenePositiveNumber(object && object.radius, 0.5));
    const halfHeight = scenePositiveNumber(object && object.height, 1) * 0.5;
    const apex = { x: 0, y: halfHeight, z: 0 };
    const base = [];
    for (let i = 0; i < segments; i += 1) {
      const angle = (Math.PI * 2 * i) / segments;
      base.push({ x: radius * Math.cos(angle), y: -halfHeight, z: radius * Math.sin(angle) });
    }
    const out = [];
    for (let i = 0; i < segments; i += 1) {
      const next = (i + 1) % segments;
      out.push([base[i], base[next]]);
      out.push([base[i], apex]);
    }
    return out;
  }

  function torusSegments(object) {
    const radialSegments = scenePrimitiveSegmentResolution(object && object.radialSegments, 32, 3, 256);
    const tubularSegments = scenePrimitiveSegmentResolution(object && object.tubularSegments, 16, 3, 128);
    const radius = scenePositiveNumber(object && object.radius, 0.7);
    const tube = scenePositiveNumber(object && object.tube, 0.3);
    function point(i, j) {
      const u = (Math.PI * 2 * i) / radialSegments;
      const v = (Math.PI * 2 * j) / tubularSegments;
      const cu = Math.cos(u);
      const su = Math.sin(u);
      const cv = Math.cos(v);
      const r = radius + tube * cv;
      return { x: r * cu, y: tube * Math.sin(v), z: r * su };
    }
    const out = [];
    for (let i = 0; i < radialSegments; i += 1) {
      const next = (i + 1) % radialSegments;
      out.push([point(i, 0), point(next, 0)]);
      out.push([point(i, Math.floor(tubularSegments / 2)), point(next, Math.floor(tubularSegments / 2))]);
    }
    const radialStride = Math.max(1, Math.floor(radialSegments / 8));
    for (let i = 0; i < radialSegments; i += radialStride) {
      for (let j = 0; j < tubularSegments; j += 1) {
        out.push([point(i, j), point(i, (j + 1) % tubularSegments)]);
      }
    }
    return out;
  }

  function scenePushMeshVertex(out, position, normal, uv) {
    out.positions.push(position.x, position.y, position.z);
    out.normals.push(normal.x, normal.y, normal.z);
    out.uvs.push(uv.x, uv.y);
    out.count += 1;
  }

  function scenePushMeshTriangle(out, a, b, c, normal, uva, uvb, uvc) {
    scenePushMeshVertex(out, a, normal, uva || { x: 0, y: 0 });
    scenePushMeshVertex(out, b, normal, uvb || { x: 1, y: 0 });
    scenePushMeshVertex(out, c, normal, uvc || { x: 1, y: 1 });
  }

  function sceneFinalizePrimitiveMesh(out) {
    if (!out || out.count < 3) return null;
    return {
      positions: new Float32Array(out.positions),
      normals: new Float32Array(out.normals),
      uvs: new Float32Array(out.uvs),
      tangents: new Float32Array(0),
      count: out.count,
      immutable: true,
      revision: 0,
      dynamic: false,
    };
  }

  function scenePrimitiveMeshBuilder() {
    return { positions: [], normals: [], uvs: [], count: 0 };
  }

  // Winding convention for every solid mesh below.
  //
  // Wind each triangle counter-clockwise as seen from outside the surface. The
  // geometric normal that the right-hand rule gives then agrees with the outward
  // normals the three vertices carry.
  //
  // Three producers build the same primitive kinds, and one authored shape can
  // reach the screen through any of them:
  //   - this file, when the renderer draws the object on its own;
  //   - generateInstancedGeometry in 16c-scene-shared-pbr.js, when the renderer
  //     instances the object;
  //   - scene/geom in Go, for the native renderer and the headless oracle.
  //
  // box, plane, sphere and torus were wound the other way here. They measured
  // -1.000000, -1.000000, -0.999170 and -0.997526 against their own normals,
  // while 16c and scene/geom measured the same three figures positive. One
  // authored box therefore had opposite winding depending only on whether the
  // renderer instanced it.
  //
  // Four permissive defaults hid the split in the MAIN colour pass:
  //   - the WebGL main pass calls gl.disable(gl.CULL_FACE);
  //   - the WebGPU PBR pipeline sets cullMode "none";
  //   - sceneRayIntersectsTriangle reports a hit on both faces;
  //   - the native Go renderer reads scene/geom and never reads this file.
  //
  // FOUR browser draw paths DO cull. Read every one before you touch this file.
  //   - the WebGL shadow pass enables CULL_FACE and calls cullFace(gl.FRONT);
  //   - the WebGPU gosx-shadow pipeline sets cullMode "front";
  //   - the WebGPU gosx-shadow-instanced pipeline sets cullMode "front";
  //   - drawPBRObjects in 16a-scene-webgpu.js leaves a mesh object on
  //     getSelenaPipeline's cullMode "back" default whenever the object carries
  //     a Selena custom shader and doubleSided stays false.
  //
  // The three shadow sites keep the faces that point AWAY from the light, which
  // is the standard mitigation for peter-panning. So the winding below decides
  // which surface a browser shadow map records. render/bundle/renderer.go keeps
  // the opposite face natively, and render/bundle/shadow_drift_test.go pins all
  // three settings and states the verdict.
  //
  // render/bundle/renderer.go draws scene/geom with CullBack plus FrontFaceCCW,
  // and render/gpu/jsgpu/encode.go maps that pair to WebGPU cullMode "back" plus
  // frontFace "ccw" with no inversion, so the winding below is the winding that
  // pair expects.
  //
  // Only the vertex order inside each triangle changed. Every vertex, the
  // triangle count and the triangle order stay the same, so a pick still reports
  // the same triangle index and every raycast test keeps its answer.
  //
  // 12-scene-geometry-winding.test.mjs measures the dot product per generator and
  // fails on a reversed face. It also builds one shape through both browser paths
  // and compares the two signs directly.
  function boxTriangleMesh(object) {
    const vertices = boxVertices(object.width, object.height, object.depth);
    const out = scenePrimitiveMeshBuilder();
    const uv0 = { x: 0, y: 0 };
    const uv1 = { x: 1, y: 0 };
    const uv2 = { x: 1, y: 1 };
    const uv3 = { x: 0, y: 1 };
    const faces = [
      { normal: { x: 0, y: 0, z: -1 }, indices: [0, 1, 2, 3] },
      { normal: { x: 0, y: 0, z: 1 }, indices: [5, 4, 7, 6] },
      { normal: { x: -1, y: 0, z: 0 }, indices: [4, 0, 3, 7] },
      { normal: { x: 1, y: 0, z: 0 }, indices: [1, 5, 6, 2] },
      { normal: { x: 0, y: 1, z: 0 }, indices: [3, 2, 6, 7] },
      { normal: { x: 0, y: -1, z: 0 }, indices: [4, 5, 1, 0] },
    ];
    for (let i = 0; i < faces.length; i += 1) {
      const face = faces[i];
      const a = vertices[face.indices[0]];
      const b = vertices[face.indices[1]];
      const c = vertices[face.indices[2]];
      const d = vertices[face.indices[3]];
      // Each face lists its four corners clockwise about its own outward normal,
      // so the quad fan runs a, c, b and a, d, c. Each UV travels with its corner.
      scenePushMeshTriangle(out, a, c, b, face.normal, uv0, uv2, uv1);
      scenePushMeshTriangle(out, a, d, c, face.normal, uv0, uv3, uv2);
    }
    return sceneFinalizePrimitiveMesh(out);
  }

  function planeTriangleMesh(object) {
    // Take the four corners of the y-plane, not the first four boxVertices.
    // boxVertices lists the -z face first (indices 0..3), so slice(0, 4) with
    // height 0 gave four points that all share z = -depth/2: a zero-area strip
    // instead of a plane. Indices 0, 1, 5 and 4 are the corners that span x
    // and z.
    //
    // The four corners run clockwise about the +y normal, so the fan runs 0, 2, 1
    // and 0, 3, 2. That winds both triangles counter-clockwise seen from above,
    // which is where the +y normal points. generateInstancedPlaneGeometry in
    // 16c-scene-shared-pbr.js measures +1.000000 for the same quad.
    const box = boxVertices(object.width, 0, object.depth);
    const vertices = [box[0], box[1], box[5], box[4]];
    const out = scenePrimitiveMeshBuilder();
    const normal = { x: 0, y: 1, z: 0 };
    scenePushMeshTriangle(out, vertices[0], vertices[2], vertices[1], normal, { x: 0, y: 1 }, { x: 1, y: 0 }, { x: 1, y: 1 });
    scenePushMeshTriangle(out, vertices[0], vertices[3], vertices[2], normal, { x: 0, y: 1 }, { x: 0, y: 0 }, { x: 1, y: 0 });
    return sceneFinalizePrimitiveMesh(out);
  }

  function sphereTriangleMesh(object) {
    const radius = scenePositiveNumber(object && object.radius, 0.5);
    const segments = scenePrimitiveSegmentResolution(object && object.segments, 32, 6, 128);
    const rings = Math.max(3, Math.floor(segments / 2));
    const out = scenePrimitiveMeshBuilder();
    function point(lat, lon) {
      const theta = Math.PI * lat / rings;
      const phi = Math.PI * 2 * lon / segments;
      const sinTheta = Math.sin(theta);
      const normal = {
        x: Math.cos(phi) * sinTheta,
        y: Math.cos(theta),
        z: Math.sin(phi) * sinTheta,
      };
      return {
        position: { x: normal.x * radius, y: normal.y * radius, z: normal.z * radius },
        normal,
        uv: { x: lon / segments, y: lat / rings },
      };
    }
    for (let lat = 0; lat < rings; lat += 1) {
      for (let lon = 0; lon < segments; lon += 1) {
        const nextLon = lon + 1;
        const a = point(lat, lon);
        const b = point(lat + 1, lon);
        const c = point(lat + 1, nextLon);
        const d = point(lat, nextLon);
        // a and d sit on ring lat, b and c sit on ring lat + 1. Latitude grows
        // downward from the north pole, so a, d, b and d, c, b wind
        // counter-clockwise seen from outside the ball. The top and the bottom row
        // each drop one triangle, because a pole quad collapses to a sliver.
        if (lat > 0) {
          scenePushMeshVertex(out, a.position, a.normal, a.uv);
          scenePushMeshVertex(out, d.position, d.normal, d.uv);
          scenePushMeshVertex(out, b.position, b.normal, b.uv);
        }
        if (lat < rings - 1) {
          scenePushMeshVertex(out, d.position, d.normal, d.uv);
          scenePushMeshVertex(out, c.position, c.normal, c.uv);
          scenePushMeshVertex(out, b.position, b.normal, b.uv);
        }
      }
    }
    return sceneFinalizePrimitiveMesh(out);
  }

  function torusTriangleMesh(object) {
    const radialSegments = scenePrimitiveSegmentResolution(object && object.radialSegments, 32, 3, 128);
    const tubularSegments = scenePrimitiveSegmentResolution(object && object.tubularSegments, 16, 3, 64);
    const radius = scenePositiveNumber(object && object.radius, 0.7);
    const tube = scenePositiveNumber(object && object.tube, 0.3);
    const out = scenePrimitiveMeshBuilder();
    function point(i, j) {
      const u = Math.PI * 2 * i / radialSegments;
      const v = Math.PI * 2 * j / tubularSegments;
      const cu = Math.cos(u);
      const su = Math.sin(u);
      const cv = Math.cos(v);
      const sv = Math.sin(v);
      const r = radius + tube * cv;
      const normal = { x: cu * cv, y: sv, z: su * cv };
      return {
        position: { x: r * cu, y: tube * sv, z: r * su },
        normal,
        uv: { x: i / radialSegments, y: j / tubularSegments },
      };
    }
    for (let i = 0; i < radialSegments; i += 1) {
      for (let j = 0; j < tubularSegments; j += 1) {
        const a = point(i, j);
        const b = point(i + 1, j);
        const c = point(i + 1, j + 1);
        const d = point(i, j + 1);
        // i sweeps the major ring and j sweeps the tube cross-section, so the quad
        // a, b, c, d reads clockwise from outside the tube. Fan it a, c, b and
        // a, d, c to wind both triangles with the outward normals.
        // generateInstancedTorusGeometry in 16c-scene-shared-pbr.js measures
        // +0.997526 for the same default torus.
        scenePushMeshVertex(out, a.position, a.normal, a.uv);
        scenePushMeshVertex(out, c.position, c.normal, c.uv);
        scenePushMeshVertex(out, b.position, b.normal, b.uv);
        scenePushMeshVertex(out, a.position, a.normal, a.uv);
        scenePushMeshVertex(out, d.position, d.normal, d.uv);
        scenePushMeshVertex(out, c.position, c.normal, c.uv);
      }
    }
    return sceneFinalizePrimitiveMesh(out);
  }

  // Torus-knot mesh generator — (p=2, q=3) trefoil knot.
  //
  // Parameter conventions (match THREE.js TorusKnotGeometry, opposite of GoSX torus):
  //   tubularSegments — steps along the knot PATH (default 128; page.gsx uses 64)
  //   radialSegments  — steps around the tube CROSS-SECTION (default 16; page.gsx uses 8)
  //
  // Local-space orientation: primary loop in XY plane, Z oscillation.
  // The scene's rotationX={π/2} maps local→world via (x, -z, y), yielding the
  // world-space layout the water shader's analytic SDF uses:
  //   SDF: C = (rad·cos(2θ), −r·sin(3θ)/2, rad·sin(2θ))  [world, XZ-primary]
  //   local: C = (rad·cos(2θ), rad·sin(2θ), r·sin(3θ)/2) [XY-primary]
  //   After rotX(π/2): world.x=local.x ✓  world.y=−local.z ✓  world.z=local.y ✓
  function torusKnotTriangleMesh(object) {
    const tubularSegments = scenePrimitiveSegmentResolution(object && object.tubularSegments, 128, 8, 512);
    const radialSegments = scenePrimitiveSegmentResolution(object && object.radialSegments, 16, 3, 64);
    const radius = scenePositiveNumber(object && object.radius, 0.17);
    const tube = scenePositiveNumber(object && object.tube, 0.045);
    const p = 2, q = 3;
    function knotCurve(theta) {
      const rad = radius * (2.0 + Math.cos(q * theta)) * 0.5;
      return { x: rad * Math.cos(p * theta), y: rad * Math.sin(p * theta), z: radius * Math.sin(q * theta) * 0.5 };
    }
    function knotTangent(theta) {
      const h = 0.0001;
      const a = knotCurve(theta - h), b = knotCurve(theta + h);
      const dx = b.x - a.x, dy = b.y - a.y, dz = b.z - a.z;
      const len = Math.sqrt(dx * dx + dy * dy + dz * dz) || 1;
      return { x: dx / len, y: dy / len, z: dz / len };
    }
    // Build rotation-minimizing frames (parallel transport) to orient the tube
    // cross-sections stably without Frenet-frame flipping.
    const C_arr = [], T_arr = [], N_arr = [], B_arr = [];
    {
      const t0 = knotTangent(0);
      // Initial normal: axis least-parallel to T₀
      const ax = Math.abs(t0.x), ay = Math.abs(t0.y), az = Math.abs(t0.z);
      let n0;
      if (ax <= ay && ax <= az)        { n0 = { x: 0, y: -t0.z, z: t0.y }; }
      else if (ay <= az)               { n0 = { x: -t0.z, y: 0, z: t0.x }; }
      else                             { n0 = { x: -t0.y, y: t0.x, z: 0 }; }
      const nl = Math.sqrt(n0.x * n0.x + n0.y * n0.y + n0.z * n0.z) || 1;
      n0 = { x: n0.x / nl, y: n0.y / nl, z: n0.z / nl };
      const b0 = { x: t0.y * n0.z - t0.z * n0.y, y: t0.z * n0.x - t0.x * n0.z, z: t0.x * n0.y - t0.y * n0.x };
      C_arr.push(knotCurve(0)); T_arr.push(t0); N_arr.push(n0); B_arr.push(b0);
    }
    for (let i = 1; i <= tubularSegments; i++) {
      const theta = (Math.PI * 2 * i) / tubularSegments;
      const t = knotTangent(theta);
      const pn = N_arr[i - 1];
      const dot = pn.x * t.x + pn.y * t.y + pn.z * t.z;
      let nx = pn.x - dot * t.x, ny = pn.y - dot * t.y, nz = pn.z - dot * t.z;
      const nl = Math.sqrt(nx * nx + ny * ny + nz * nz) || 1;
      nx /= nl; ny /= nl; nz /= nl;
      const bx = t.y * nz - t.z * ny, by = t.z * nx - t.x * nz, bz = t.x * ny - t.y * nx;
      C_arr.push(knotCurve(theta));
      T_arr.push(t);
      N_arr.push({ x: nx, y: ny, z: nz });
      B_arr.push({ x: bx, y: by, z: bz });
    }
    // Seam correction: measure angular gap between frame[N] and frame[0],
    // distribute it linearly so the tube closes without a twist seam.
    {
      const nEnd = N_arr[tubularSegments], n0 = N_arr[0], tEnd = T_arr[tubularSegments];
      const dot = nEnd.x * n0.x + nEnd.y * n0.y + nEnd.z * n0.z;
      const cx = nEnd.y * n0.z - nEnd.z * n0.y;
      const cy = nEnd.z * n0.x - nEnd.x * n0.z;
      const cz = nEnd.x * n0.y - nEnd.y * n0.x;
      const sinA = cx * tEnd.x + cy * tEnd.y + cz * tEnd.z;
      const totalAngle = Math.atan2(sinA, dot);
      for (let i = 1; i <= tubularSegments; i++) {
        const angle = totalAngle * i / tubularSegments;
        const cos = Math.cos(angle), sin = Math.sin(angle);
        const n = N_arr[i], b = B_arr[i];
        N_arr[i] = { x: cos * n.x + sin * b.x, y: cos * n.y + sin * b.y, z: cos * n.z + sin * b.z };
        B_arr[i] = { x: cos * b.x - sin * n.x, y: cos * b.y - sin * n.y, z: cos * b.z - sin * n.z };
      }
    }
    function knotVertex(iSeg, jRad) {
      const phi = Math.PI * 2 * jRad / radialSegments;
      const cp = Math.cos(phi), sp = Math.sin(phi);
      const n = N_arr[iSeg], b = B_arr[iSeg], c = C_arr[iSeg];
      const nx = cp * n.x + sp * b.x, ny = cp * n.y + sp * b.y, nz = cp * n.z + sp * b.z;
      return {
        position: { x: c.x + tube * nx, y: c.y + tube * ny, z: c.z + tube * nz },
        normal: { x: nx, y: ny, z: nz },
        uv: { x: iSeg / tubularSegments, y: jRad / radialSegments },
      };
    }
    const out = scenePrimitiveMeshBuilder();
    // Wind each quad counter-clockwise as seen from outside the tube, so the
    // geometric normal of every triangle agrees with the outward normals its own
    // three vertices carry.
    //
    // The old order (a, b, c) and (a, c, d) opposed those normals at a dot
    // product of -0.998. Four permissive defaults hid it: the WebGL main pass
    // calls gl.disable(gl.CULL_FACE), the WebGPU pipeline sets cullMode "none",
    // sceneRayIntersectsTriangle accepts both faces, and the native renderer
    // skipped the shape. The native renderer culls back faces with a
    // counter-clockwise front face, so it needs this order to draw the near wall
    // of the tube instead of the far one.
    //
    // buildTorusKnot in scene/geom/primitives.go now emits the same two
    // triangles in the same order. Only the vertex order inside each triangle
    // changed here, so the triangle count, the triangle order and every vertex
    // stay the same and a pick still reports the same triangle index.
    for (let i = 0; i < tubularSegments; i++) {
      for (let j = 0; j < radialSegments; j++) {
        const a = knotVertex(i, j);
        const b = knotVertex(i + 1, j);
        const c = knotVertex(i + 1, j + 1);
        const d = knotVertex(i, j + 1);
        scenePushMeshVertex(out, a.position, a.normal, a.uv);
        scenePushMeshVertex(out, c.position, c.normal, c.uv);
        scenePushMeshVertex(out, b.position, b.normal, b.uv);
        scenePushMeshVertex(out, a.position, a.normal, a.uv);
        scenePushMeshVertex(out, d.position, d.normal, d.uv);
        scenePushMeshVertex(out, c.position, c.normal, c.uv);
      }
    }
    return sceneFinalizePrimitiveMesh(out);
  }

  // sceneInstancedTriangleMesh borrows a solid mesh from the instanced
  // geometry generators in 16c-scene-shared-pbr.js. Both files sit in the same
  // IIFE and function declarations hoist, so the call resolves lexically.
  //
  // The two families return the same shape and differ only in the count key:
  // this one uses `count`, the instanced one uses `vertexCount`. Tangents come
  // through as generated, which is better than the empty array
  // sceneFinalizePrimitiveMesh hands back.
  function sceneInstancedTriangleMesh(kind, object) {
    if (typeof generateInstancedGeometry !== "function") return null;
    const mesh = generateInstancedGeometry(kind, object || {});
    if (!mesh || !(mesh.vertexCount > 2)) return null;
    return {
      positions: mesh.positions,
      normals: mesh.normals,
      uvs: mesh.uvs,
      tangents: mesh.tangents || new Float32Array(0),
      count: mesh.vertexCount,
      immutable: true,
      revision: 0,
      dynamic: false,
    };
  }

  function scenePrimitiveTriangleMesh(object) {
    switch (object && object.kind) {
      case "box":
      case "cube":
        return boxTriangleMesh(object);
      case "plane":
        return planeTriangleMesh(object);
      case "sphere":
        return sphereTriangleMesh(object);
      case "torus":
        return torusTriangleMesh(object);
      case "torusknot":
        return torusKnotTriangleMesh(object);
      // cylinder, cone and pyramid had no case here, so
      // 10-runtime-scene-core.js never set vertices for them,
      // appendSceneObjectToBundle fell through to sceneObjectSegments, and
      // 15-scene-draw-plan.js kept the object on the line pass. Three
      // documented primitive kinds drew as wireframes when the author asked
      // for a solid mesh. The solid generators already existed in 16c.
      case "cylinder":
      case "cone":
      case "pyramid":
        return sceneInstancedTriangleMesh(object.kind, object);
      default:
        return null;
    }
  }

  function lineSegments(object) {
    const points = Array.isArray(object && object.points) ? object.points : [];
    const segments = Array.isArray(object && object.lineSegments) ? object.lineSegments : [];
    const out = [];
    for (const pair of segments) {
      if (!Array.isArray(pair) || pair.length < 2) {
        continue;
      }
      const from = points[pair[0]];
      const to = points[pair[1]];
      if (!from || !to) {
        continue;
      }
      out.push([from, to]);
    }
    return out;
  }

  function torusKnotSegments(object) {
    const tubularSegments = scenePrimitiveSegmentResolution(object && object.tubularSegments, 128, 8, 512);
    const radius = scenePositiveNumber(object && object.radius, 0.17);
    const p = 2, q = 3;
    function knotCurve(theta) {
      const rad = radius * (2.0 + Math.cos(q * theta)) * 0.5;
      return { x: rad * Math.cos(p * theta), y: rad * Math.sin(p * theta), z: radius * Math.sin(q * theta) * 0.5 };
    }
    const out = [];
    for (let i = 0; i < tubularSegments; i++) {
      const t0 = (Math.PI * 2 * i) / tubularSegments;
      const t1 = (Math.PI * 2 * (i + 1)) / tubularSegments;
      out.push([knotCurve(t0), knotCurve(t1)]);
    }
    return out;
  }

  function sceneObjectSegments(object) {
    switch (object.kind) {
      case "box":
      case "cube":
        return boxSegments(object);
      case "lines":
        return lineSegments(object);
      case "plane":
        return planeSegments(object);
      case "pyramid":
        return pyramidSegments(object);
      case "sphere":
        return sphereSegments(object);
      case "cylinder":
        return cylinderSegments(object);
      case "cone":
        return coneSegments(object);
      case "torus":
        return torusSegments(object);
      case "torusknot":
        return torusKnotSegments(object);
      default:
        return boxSegments(object);
    }
  }

  function scenePlaneLocalCorners(object) {
    return boxVertices(
      sceneNumber(object && object.width, 1),
      0,
      sceneNumber(object && object.depth, sceneNumber(object && object.height, 1)),
    ).slice(0, 4);
  }

  // Module-level scratch for scenePlaneSurfaceCorners. Four stable corner
  // objects wrapped in a stable array — the two callers in
  // 10-runtime-scene-core.js (appendSceneObjectToBundle bounds expansion
  // and appendSceneSurfaceToBundle positions serialization) consume the
  // returned corners immediately inside a for loop without retaining the
  // individual refs, so it's safe to share. Previously each call
  // allocated a 4-element array of fresh {x,y,z} objects through
  // translateScenePoint — 5 allocations per plane per frame.
  const _scenePlaneSurfaceCornersScratch = [
    { x: 0, y: 0, z: 0 },
    { x: 0, y: 0, z: 0 },
    { x: 0, y: 0, z: 0 },
    { x: 0, y: 0, z: 0 },
  ];

  function scenePlaneSurfaceCorners(object, timeSeconds) {
    const local = scenePlaneLocalCorners(object);
    const out = _scenePlaneSurfaceCornersScratch;
    for (let i = 0; i < 4; i += 1) {
      const p = local[i];
      translateScenePointInto(out[i], p && p.x, p && p.y, p && p.z, object, timeSeconds);
    }
    return out;
  }

  function scenePlaneSurfacePositions(corners) {
    if (!Array.isArray(corners) || corners.length < 4) {
      return [];
    }
    return [
      corners[0].x, corners[0].y, corners[0].z,
      corners[1].x, corners[1].y, corners[1].z,
      corners[2].x, corners[2].y, corners[2].z,
      corners[0].x, corners[0].y, corners[0].z,
      corners[2].x, corners[2].y, corners[2].z,
      corners[3].x, corners[3].y, corners[3].z,
    ];
  }

  function scenePlaneSurfaceUVs() {
    return [
      0, 1,
      1, 1,
      1, 0,
      0, 1,
      1, 0,
      0, 0,
    ];
  }
