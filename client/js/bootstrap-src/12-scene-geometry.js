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
    };
  }

  function scenePrimitiveMeshBuilder() {
    return { positions: [], normals: [], uvs: [], count: 0 };
  }

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
      scenePushMeshTriangle(out, a, b, c, face.normal, uv0, uv1, uv2);
      scenePushMeshTriangle(out, a, c, d, face.normal, uv0, uv2, uv3);
    }
    return sceneFinalizePrimitiveMesh(out);
  }

  function planeTriangleMesh(object) {
    const vertices = boxVertices(object.width, 0, object.depth).slice(0, 4);
    const out = scenePrimitiveMeshBuilder();
    const normal = { x: 0, y: 1, z: 0 };
    scenePushMeshTriangle(out, vertices[0], vertices[1], vertices[2], normal, { x: 0, y: 1 }, { x: 1, y: 1 }, { x: 1, y: 0 });
    scenePushMeshTriangle(out, vertices[0], vertices[2], vertices[3], normal, { x: 0, y: 1 }, { x: 1, y: 0 }, { x: 0, y: 0 });
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
        if (lat > 0) {
          scenePushMeshVertex(out, a.position, a.normal, a.uv);
          scenePushMeshVertex(out, b.position, b.normal, b.uv);
          scenePushMeshVertex(out, d.position, d.normal, d.uv);
        }
        if (lat < rings - 1) {
          scenePushMeshVertex(out, d.position, d.normal, d.uv);
          scenePushMeshVertex(out, b.position, b.normal, b.uv);
          scenePushMeshVertex(out, c.position, c.normal, c.uv);
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
        scenePushMeshVertex(out, a.position, a.normal, a.uv);
        scenePushMeshVertex(out, b.position, b.normal, b.uv);
        scenePushMeshVertex(out, c.position, c.normal, c.uv);
        scenePushMeshVertex(out, a.position, a.normal, a.uv);
        scenePushMeshVertex(out, c.position, c.normal, c.uv);
        scenePushMeshVertex(out, d.position, d.normal, d.uv);
      }
    }
    return sceneFinalizePrimitiveMesh(out);
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
