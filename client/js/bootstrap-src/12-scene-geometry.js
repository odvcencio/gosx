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
