  // Scene input — pointer, drag, and pick interaction handling for 3D scenes.

  var SCENE_DRAG_MIN_EXTENT_X = 0.6;
  var SCENE_DRAG_MIN_EXTENT_Y = 0.35;
  var SCENE_PICK_MIN_EXTENT_X = 0.12;
  var SCENE_PICK_MIN_EXTENT_Y = 0.08;
  var SCENE_ORBIT_PITCH_MIN = -1.35;
  var SCENE_ORBIT_PITCH_MAX = 1.35;
  var SCENE_ORBIT_YAW_MIN = -1.1;
  var SCENE_ORBIT_YAW_MAX = 1.1;
  var SCENE_POINTER_PAD_MIN = 12;
  var SCENE_POINTER_PAD_RANGE = 22;
  var SCENE_POINTER_PAD_SCALE = 0.08;

  function sceneLocalPointerPoint(event, canvas, width, height) {
    const rect = canvas.getBoundingClientRect();
    const safeWidth = Math.max(rect.width || 0, 1);
    const safeHeight = Math.max(rect.height || 0, 1);
    return {
      x: sceneClamp(((sceneNumber(event && event.clientX, rect.left) - rect.left) / safeWidth) * width, 0, width),
      y: sceneClamp(((sceneNumber(event && event.clientY, rect.top) - rect.top) / safeHeight) * height, 0, height),
    };
  }

  function sceneLocalPointerSample(event, canvas, width, height, state, phase) {
    const previousX = state.lastX == null ? width / 2 : state.lastX;
    const previousY = state.lastY == null ? height / 2 : state.lastY;
    const hasPointerPosition = Number.isFinite(sceneNumber(event && event.clientX, NaN)) && Number.isFinite(sceneNumber(event && event.clientY, NaN));
    const point = hasPointerPosition ? sceneLocalPointerPoint(event, canvas, width, height) : { x: previousX, y: previousY };
    const sample = {
      x: point.x,
      y: point.y,
      deltaX: point.x - previousX,
      deltaY: point.y - previousY,
      buttons: phase === "end" ? 0 : 1,
      button: phase === "start" || phase === "end" ? 0 : null,
      active: phase !== "end",
    };
    state.lastX = point.x;
    state.lastY = point.y;
    return sample;
  }

  function resetScenePointerSample(width, height, state) {
    state.lastX = width / 2;
    state.lastY = height / 2;
    publishPointerSignals({
      x: state.lastX,
      y: state.lastY,
      deltaX: 0,
      deltaY: 0,
      buttons: 0,
      button: 0,
      active: false,
    });
  }

  function sceneDragSignalNamespace(props) {
    const value = props && props.dragSignalNamespace;
    return typeof value === "string" ? value.trim() : "";
  }

  function scenePickSignalNamespace(props) {
    const value = props && props.pickSignalNamespace;
    return typeof value === "string" ? value.trim() : "";
  }

  function sceneEventSignalNamespace(props) {
    const value = props && props.eventSignalNamespace;
    return typeof value === "string" ? value.trim() : "";
  }

  function sceneSignalSegment(value, fallback) {
    const source = typeof value === "string" ? value.trim().toLowerCase() : "";
    if (!source) {
      return fallback;
    }
    const normalized = source
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "");
    return normalized || fallback;
  }

  function sceneTargetIndex(target) {
    return target && target.index != null ? Math.max(-1, Math.floor(sceneNumber(target.index, -1))) : -1;
  }

  function sceneTargetID(target) {
    return target && target.object && typeof target.object.id === "string" ? target.object.id : "";
  }

  function sceneTargetKind(target) {
    return target && target.object && typeof target.object.kind === "string" ? target.object.kind : "";
  }

  function sceneObjectSignalSlug(index, id, kind) {
    const targetID = typeof id === "string" ? id.trim() : "";
    if (targetID) {
      return sceneSignalSegment(targetID, "object");
    }
    const targetKind = typeof kind === "string" ? kind.trim() : "";
    if (targetKind && index >= 0) {
      return sceneSignalSegment(targetKind + "-" + index, "object-" + index);
    }
    if (index >= 0) {
      return "object-" + index;
    }
    return "";
  }

  function publishSceneDragSignals(namespace, state, active) {
    if (!namespace) {
      return;
    }
    queueInputSignal(namespace + ".x", sceneNumber(state.orbitX, 0));
    queueInputSignal(namespace + ".y", sceneNumber(state.orbitY, 0));
    queueInputSignal(namespace + ".targetIndex", Math.max(-1, Math.floor(sceneNumber(state.targetIndex, -1))));
    queueInputSignal(namespace + ".active", Boolean(active));
  }

  function publishScenePickSignals(namespace, state) {
    if (!namespace) {
      return;
    }
    const snapshot = scenePickSignalSnapshot(state);
    const nextKey = scenePickSignalSnapshotKey(snapshot);
    if (nextKey === state.publishedKey) {
      return;
    }
    state.publishedKey = nextKey;
    queueInputSignal(namespace + ".hovered", snapshot.hovered);
    queueInputSignal(namespace + ".hoverIndex", snapshot.hoverIndex);
    queueInputSignal(namespace + ".hoverID", snapshot.hoverID);
    queueInputSignal(namespace + ".down", snapshot.down);
    queueInputSignal(namespace + ".downIndex", snapshot.downIndex);
    queueInputSignal(namespace + ".downID", snapshot.downID);
    queueInputSignal(namespace + ".selected", snapshot.selected);
    queueInputSignal(namespace + ".selectedIndex", snapshot.selectedIndex);
    queueInputSignal(namespace + ".selectedID", snapshot.selectedID);
    queueInputSignal(namespace + ".clickCount", snapshot.clickCount);
    queueInputSignal(namespace + ".pointerX", snapshot.pointerX);
    queueInputSignal(namespace + ".pointerY", snapshot.pointerY);
  }

  function publishSceneEventSignals(namespace, state) {
    if (!namespace) {
      return;
    }
    const snapshot = sceneInteractionSnapshot(state);
    const nextKey = sceneInteractionSnapshotKey(snapshot);
    if (nextKey === state.publishedEventKey) {
      return;
    }
    state.publishedEventKey = nextKey;
    queueInputSignal(namespace + ".revision", snapshot.revision);
    queueInputSignal(namespace + ".type", snapshot.type);
    queueInputSignal(namespace + ".targetIndex", snapshot.targetIndex);
    queueInputSignal(namespace + ".targetID", snapshot.targetID);
    queueInputSignal(namespace + ".targetKind", snapshot.targetKind);
    queueInputSignal(namespace + ".hovered", snapshot.hovered);
    queueInputSignal(namespace + ".hoverIndex", snapshot.hoverIndex);
    queueInputSignal(namespace + ".hoverID", snapshot.hoverID);
    queueInputSignal(namespace + ".hoverKind", snapshot.hoverKind);
    queueInputSignal(namespace + ".down", snapshot.down);
    queueInputSignal(namespace + ".downIndex", snapshot.downIndex);
    queueInputSignal(namespace + ".downID", snapshot.downID);
    queueInputSignal(namespace + ".downKind", snapshot.downKind);
    queueInputSignal(namespace + ".selected", snapshot.selected);
    queueInputSignal(namespace + ".selectedIndex", snapshot.selectedIndex);
    queueInputSignal(namespace + ".selectedID", snapshot.selectedID);
    queueInputSignal(namespace + ".selectedKind", snapshot.selectedKind);
    queueInputSignal(namespace + ".clickCount", snapshot.clickCount);
    queueInputSignal(namespace + ".pointerX", snapshot.pointerX);
    queueInputSignal(namespace + ".pointerY", snapshot.pointerY);
    publishSceneObjectEventSignals(namespace, state, snapshot);
  }

  function scenePickSignalSnapshot(state) {
    return {
      hovered: Boolean(state.hoverIndex >= 0),
      hoverIndex: Math.max(-1, Math.floor(sceneNumber(state.hoverIndex, -1))),
      hoverID: state.hoverID || "",
      down: Boolean(state.downIndex >= 0),
      downIndex: Math.max(-1, Math.floor(sceneNumber(state.downIndex, -1))),
      downID: state.downID || "",
      selected: Boolean(state.selectedIndex >= 0),
      selectedIndex: Math.max(-1, Math.floor(sceneNumber(state.selectedIndex, -1))),
      selectedID: state.selectedID || "",
      clickCount: Math.max(0, Math.floor(sceneNumber(state.clickCount, 0))),
      pointerX: sceneNumber(state.pointerX, 0),
      pointerY: sceneNumber(state.pointerY, 0),
    };
  }

  function sceneInteractionSnapshot(state) {
    var pick = scenePickSignalSnapshot(state);
    pick.revision = Math.max(0, Math.floor(sceneNumber(state.eventRevision, 0)));
    pick.type = state.eventType || "";
    pick.targetIndex = Math.max(-1, Math.floor(sceneNumber(state.eventTargetIndex, -1)));
    pick.targetID = state.eventTargetID || "";
    pick.targetKind = state.eventTargetKind || "";
    pick.hoverKind = state.hoverKind || "";
    pick.downKind = state.downKind || "";
    pick.selectedKind = state.selectedKind || "";
    return pick;
  }

  function scenePickSignalSnapshotKey(snapshot) {
    return [
      snapshot.hovered ? 1 : 0,
      snapshot.hoverIndex,
      snapshot.hoverID,
      snapshot.down ? 1 : 0,
      snapshot.downIndex,
      snapshot.downID,
      snapshot.selected ? 1 : 0,
      snapshot.selectedIndex,
      snapshot.selectedID,
      snapshot.clickCount,
      snapshot.pointerX,
      snapshot.pointerY,
    ].join("|");
  }

  function sceneInteractionSnapshotKey(snapshot) {
    return [
      snapshot.revision,
      snapshot.type,
      snapshot.targetIndex,
      snapshot.targetID,
      snapshot.targetKind,
      snapshot.hovered ? 1 : 0,
      snapshot.hoverIndex,
      snapshot.hoverID,
      snapshot.hoverKind,
      snapshot.down ? 1 : 0,
      snapshot.downIndex,
      snapshot.downID,
      snapshot.downKind,
      snapshot.selected ? 1 : 0,
      snapshot.selectedIndex,
      snapshot.selectedID,
      snapshot.selectedKind,
      snapshot.clickCount,
      snapshot.pointerX,
      snapshot.pointerY,
    ].join("|");
  }

  function publishSceneObjectEventSignals(namespace, state, snapshot) {
    publishSceneObjectBoolSignal(namespace, "hovered", state.publishedHoverSlug, sceneObjectSignalSlug(snapshot.hoverIndex, snapshot.hoverID, snapshot.hoverKind));
    state.publishedHoverSlug = sceneObjectSignalSlug(snapshot.hoverIndex, snapshot.hoverID, snapshot.hoverKind);
    publishSceneObjectBoolSignal(namespace, "down", state.publishedDownSlug, sceneObjectSignalSlug(snapshot.downIndex, snapshot.downID, snapshot.downKind));
    state.publishedDownSlug = sceneObjectSignalSlug(snapshot.downIndex, snapshot.downID, snapshot.downKind);
    publishSceneObjectBoolSignal(namespace, "selected", state.publishedSelectedSlug, sceneObjectSignalSlug(snapshot.selectedIndex, snapshot.selectedID, snapshot.selectedKind));
    state.publishedSelectedSlug = sceneObjectSignalSlug(snapshot.selectedIndex, snapshot.selectedID, snapshot.selectedKind);

    const nextCounts = state.objectClickCounts || Object.create(null);
    const previousCounts = state.publishedObjectClickCounts || Object.create(null);
    const slugs = new Set(Object.keys(previousCounts).concat(Object.keys(nextCounts)));
    slugs.forEach(function(slug) {
      const nextCount = Math.max(0, Math.floor(sceneNumber(nextCounts[slug], 0)));
      const previousCount = Math.max(0, Math.floor(sceneNumber(previousCounts[slug], 0)));
      if (nextCount === previousCount) {
        return;
      }
      queueInputSignal(namespace + ".object." + slug + ".clickCount", nextCount);
    });
    state.publishedObjectClickCounts = Object.assign(Object.create(null), nextCounts);
  }

  function publishSceneObjectBoolSignal(namespace, key, previousSlug, nextSlug) {
    if (previousSlug && previousSlug !== nextSlug) {
      queueInputSignal(namespace + ".object." + previousSlug + "." + key, false);
    }
    if (nextSlug && nextSlug !== previousSlug) {
      queueInputSignal(namespace + ".object." + nextSlug + "." + key, true);
    }
  }

  function sceneBoundsSize(bounds) {
    if (!bounds || typeof bounds !== "object") return [0, 0, 0];
    return [
      Math.abs(sceneNumber(bounds.maxX, 0) - sceneNumber(bounds.minX, 0)),
      Math.abs(sceneNumber(bounds.maxY, 0) - sceneNumber(bounds.minY, 0)),
      Math.abs(sceneNumber(bounds.maxZ, 0) - sceneNumber(bounds.minZ, 0)),
    ].sort(function(a, b) { return b - a; });
  }

  function sceneObjectAllowsPointerDrag(object) {
    if (!object || object.kind === "plane" || object.viewCulled) {
      return false;
    }
    const extents = sceneBoundsSize(object.bounds);
    return extents[0] > SCENE_DRAG_MIN_EXTENT_X && extents[1] > SCENE_DRAG_MIN_EXTENT_Y;
  }

  function sceneObjectAllowsPointerPick(object) {
    if (!object || object.viewCulled) {
      return false;
    }
    if (typeof object.pickable === "boolean") {
      return object.pickable;
    }
    if (object.kind === "plane") {
      return false;
    }
    const extents = sceneBoundsSize(object.bounds);
    return extents[0] > SCENE_PICK_MIN_EXTENT_X && extents[1] > SCENE_PICK_MIN_EXTENT_Y;
  }

  function sceneWorldPointAt(source, vertexIndex) {
    if (!source || typeof source.length !== "number") {
      return null;
    }
    const offset = Math.max(0, vertexIndex * 3);
    if (offset + 2 >= source.length) {
      return null;
    }
    return {
      x: sceneNumber(source[offset], 0),
      y: sceneNumber(source[offset + 1], 0),
      z: sceneNumber(source[offset + 2], 0),
    };
  }

  function sceneProjectedObjectSegments(bundle, object, width, height) {
    if (!bundle || !bundle.camera || !object) {
      return [];
    }
    const vertexOffset = Math.max(0, Math.floor(sceneNumber(object.vertexOffset, 0)));
    const vertexCount = Math.max(0, Math.floor(sceneNumber(object.vertexCount, 0)));
    if (vertexCount < 2) {
      return [];
    }
    const source = bundle.worldPositions;
    if (!source || typeof source.length !== "number") {
      return [];
    }
    const segments = [];
    for (let i = 0; i + 1 < vertexCount; i += 2) {
      const fromWorld = sceneWorldPointAt(source, vertexOffset + i);
      const toWorld = sceneWorldPointAt(source, vertexOffset + i + 1);
      if (!fromWorld || !toWorld) {
        continue;
      }
      const from = sceneProjectPoint(fromWorld, bundle.camera, width, height);
      const to = sceneProjectPoint(toWorld, bundle.camera, width, height);
      if (!from || !to) {
        continue;
      }
      segments.push([from, to]);
    }
    return segments;
  }

  function sceneProjectedSegmentsBounds(segments) {
    if (!Array.isArray(segments) || !segments.length) {
      return null;
    }
    let minX = segments[0][0].x;
    let maxX = segments[0][0].x;
    let minY = segments[0][0].y;
    let maxY = segments[0][0].y;
    for (const segment of segments) {
      for (const point of segment) {
        minX = Math.min(minX, point.x);
        maxX = Math.max(maxX, point.x);
        minY = Math.min(minY, point.y);
        maxY = Math.max(maxY, point.y);
      }
    }
    return { minX, maxX, minY, maxY };
  }

  function scenePointerPadding(bounds) {
    if (!bounds) {
      return SCENE_POINTER_PAD_MIN;
    }
    const span = Math.max(bounds.maxX - bounds.minX, bounds.maxY - bounds.minY);
    return sceneClamp(span * SCENE_POINTER_PAD_SCALE, SCENE_POINTER_PAD_MIN, SCENE_POINTER_PAD_RANGE);
  }

  function sceneDistanceToSegment(point, from, to) {
    const deltaX = to.x - from.x;
    const deltaY = to.y - from.y;
    const lengthSquared = deltaX * deltaX + deltaY * deltaY;
    if (lengthSquared <= 0.0001) {
      return Math.hypot(point.x - from.x, point.y - from.y);
    }
    const t = sceneClamp(((point.x - from.x) * deltaX + (point.y - from.y) * deltaY) / lengthSquared, 0, 1);
    const closestX = from.x + deltaX * t;
    const closestY = from.y + deltaY * t;
    return Math.hypot(point.x - closestX, point.y - closestY);
  }

  function sceneProjectedObjectHull(segments) {
    const points = [];
    const seen = new Set();
    for (const segment of segments) {
      for (const point of segment) {
        const key = point.x.toFixed(3) + ":" + point.y.toFixed(3);
        if (seen.has(key)) {
          continue;
        }
        seen.add(key);
        points.push({ x: point.x, y: point.y });
      }
    }
    if (points.length < 3) {
      return points;
    }
    points.sort(function(a, b) {
      return a.x === b.x ? a.y - b.y : a.x - b.x;
    });
    const lower = [];
    for (const point of points) {
      while (lower.length >= 2 && sceneTurnDirection(lower[lower.length - 2], lower[lower.length - 1], point) <= 0) {
        lower.pop();
      }
      lower.push(point);
    }
    const upper = [];
    for (let i = points.length - 1; i >= 0; i -= 1) {
      const point = points[i];
      while (upper.length >= 2 && sceneTurnDirection(upper[upper.length - 2], upper[upper.length - 1], point) <= 0) {
        upper.pop();
      }
      upper.push(point);
    }
    lower.pop();
    upper.pop();
    return lower.concat(upper);
  }

  function sceneTurnDirection(a, b, c) {
    return (b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x);
  }

  function scenePointInPolygon(point, polygon) {
    if (!Array.isArray(polygon) || polygon.length < 3) {
      return false;
    }
    let inside = false;
    for (let i = 0, j = polygon.length - 1; i < polygon.length; j = i, i += 1) {
      const xi = polygon[i].x;
      const yi = polygon[i].y;
      const xj = polygon[j].x;
      const yj = polygon[j].y;
      const intersects = ((yi > point.y) !== (yj > point.y)) &&
        (point.x < ((xj - xi) * (point.y - yi)) / ((yj - yi) || 0.000001) + xi);
      if (intersects) {
        inside = !inside;
      }
    }
    return inside;
  }

  function sceneObjectDepthCenter(object, camera) {
    const bounds = object && object.bounds;
    if (!bounds) {
      return sceneWorldPointDepth(0, camera);
    }
    return sceneBoundsDepthMetrics(bounds, camera).center;
  }

  function sceneObjectPointerCapture(bundle, object, point, width, height) {
    const segments = sceneProjectedObjectSegments(bundle, object, width, height);
    if (!segments.length) {
      return null;
    }
    const bounds = sceneProjectedSegmentsBounds(segments);
    if (!bounds) {
      return null;
    }
    const padding = scenePointerPadding(bounds);
    if (
      point.x < bounds.minX - padding ||
      point.x > bounds.maxX + padding ||
      point.y < bounds.minY - padding ||
      point.y > bounds.maxY + padding
    ) {
      return null;
    }
    let minDistance = Number.POSITIVE_INFINITY;
    for (const segment of segments) {
      minDistance = Math.min(minDistance, sceneDistanceToSegment(point, segment[0], segment[1]));
    }
    const inside = scenePointInPolygon(point, sceneProjectedObjectHull(segments));
    if (!inside && minDistance > padding) {
      return null;
    }
    return {
      inside,
      distance: inside ? 0 : minDistance,
      depth: sceneObjectDepthCenter(object, bundle.camera),
      area: Math.max(1, (bounds.maxX - bounds.minX) * (bounds.maxY - bounds.minY)),
    };
  }

  function scenePointerCaptureIsBetter(candidate, current) {
    if (!current) {
      return true;
    }
    if (candidate.inside !== current.inside) {
      return candidate.inside;
    }
    if (Math.abs(candidate.distance - current.distance) > 0.5) {
      return candidate.distance < current.distance;
    }
    if (Math.abs(candidate.depth - current.depth) > 0.01) {
      return candidate.depth < current.depth;
    }
    return candidate.area < current.area;
  }

  function sceneRaycastPick(pointerX, pointerY, width, height, camera, bundle) {
    if (!bundle || !Array.isArray(bundle.objects) || !bundle.objects.length) {
      return null;
    }

    var ray = sceneScreenToRay(pointerX, pointerY, width, height, camera);
    var closest = null;

    for (var i = 0; i < bundle.objects.length; i++) {
      var obj = bundle.objects[i];

      if (!sceneObjectAllowsPointerPick(obj)) continue;
      if (obj.viewCulled) continue;

      // Broad phase: AABB test.
      var bounds = obj.bounds;
      if (!bounds) continue;

      var boundsMin = { x: sceneNumber(bounds.minX, 0), y: sceneNumber(bounds.minY, 0), z: sceneNumber(bounds.minZ, 0) };
      var boundsMax = { x: sceneNumber(bounds.maxX, 0), y: sceneNumber(bounds.maxY, 0), z: sceneNumber(bounds.maxZ, 0) };

      var aabbDist = sceneRayIntersectsAABB(ray.origin, ray.dir, boundsMin, boundsMax);
      if (aabbDist < 0) continue;

      // Narrow phase: triangle intersection against world positions.
      var positions = bundle.worldPositions;
      if (!positions || typeof positions.length !== "number") continue;

      var vertexOffset = Math.max(0, Math.floor(sceneNumber(obj.vertexOffset, 0)));
      var vertexCount = Math.max(0, Math.floor(sceneNumber(obj.vertexCount, 0)));

      for (var tri = 0; tri + 2 < vertexCount; tri += 3) {
        var v0 = sceneWorldPointAt(positions, vertexOffset + tri);
        var v1 = sceneWorldPointAt(positions, vertexOffset + tri + 1);
        var v2 = sceneWorldPointAt(positions, vertexOffset + tri + 2);
        if (!v0 || !v1 || !v2) continue;

        var hit = sceneRayIntersectsTriangle(ray.origin, ray.dir, v0, v1, v2);
        if (hit && (!closest || hit.distance < closest.distance)) {
          closest = {
            index: i,
            object: obj,
            distance: hit.distance,
            inside: true,
            depth: hit.distance,
            area: Math.max(1, (boundsMax.x - boundsMin.x) * (boundsMax.y - boundsMin.y)),
            point: {
              x: ray.origin.x + ray.dir.x * hit.distance,
              y: ray.origin.y + ray.dir.y * hit.distance,
              z: ray.origin.z + ray.dir.z * hit.distance,
            },
          };
        }
      }
    }

    return closest;
  }

  function sceneBundlePointerTarget(bundle, point, width, height, allowObject) {
    if (!bundle || !bundle.camera || !Array.isArray(bundle.objects) || !bundle.objects.length) {
      return null;
    }
    let best = null;
    for (let index = 0; index < bundle.objects.length; index += 1) {
      const object = bundle.objects[index];
      if (typeof allowObject === "function" && !allowObject(object)) {
        continue;
      }
      const capture = sceneObjectPointerCapture(bundle, object, point, width, height);
      if (!capture) {
        continue;
      }
      const candidate = {
        index,
        object,
        inside: capture.inside,
        distance: capture.distance,
        depth: capture.depth,
        area: capture.area,
      };
      if (scenePointerCaptureIsBetter(candidate, best)) {
        best = candidate;
      }
    }
    return best;
  }

  function sceneBundlePointerDragTarget(bundle, point, width, height) {
    return sceneBundlePointerTarget(bundle, point, width, height, sceneObjectAllowsPointerDrag);
  }

  function sceneBundlePointerPickTarget(bundle, point, width, height) {
    // Try raycast-based picking first when world positions are available.
    if (bundle && bundle.camera && bundle.worldPositions) {
      var rayHit = sceneRaycastPick(point.x, point.y, width, height, bundle.camera, bundle);
      if (rayHit) {
        return rayHit;
      }
    }
    // Fall back to bounds-based picking.
    return sceneBundlePointerTarget(bundle, point, width, height, sceneObjectAllowsPointerPick);
  }

  function sceneViewportValue(viewport, key, fallback) {
    return sceneNumber(viewport && viewport[key], fallback);
  }

  function sceneDragViewportMetrics(readViewport, initialWidth, initialHeight) {
    const viewport = typeof readViewport === "function" ? readViewport() : null;
    return {
      width: Math.max(1, sceneViewportValue(viewport, "cssWidth", initialWidth)),
      height: Math.max(1, sceneViewportValue(viewport, "cssHeight", initialHeight)),
    };
  }

  function createSceneDragState(initialWidth, initialHeight) {
    return {
      active: false,
      orbitX: 0,
      orbitY: 0,
      pointerId: null,
      targetIndex: -1,
      lastX: initialWidth / 2,
      lastY: initialHeight / 2,
    };
  }

  function createScenePickState(initialWidth, initialHeight) {
    return {
      pointerId: null,
      hoverIndex: -1,
      hoverID: "",
      hoverKind: "",
      downIndex: -1,
      downID: "",
      downKind: "",
      selectedIndex: -1,
      selectedID: "",
      selectedKind: "",
      clickCount: 0,
      pointerX: initialWidth / 2,
      pointerY: initialHeight / 2,
      eventRevision: 0,
      eventType: "",
      eventTargetIndex: -1,
      eventTargetID: "",
      eventTargetKind: "",
      publishedKey: "",
      publishedEventKey: "",
      publishedHoverSlug: "",
      publishedDownSlug: "",
      publishedSelectedSlug: "",
      objectClickCounts: Object.create(null),
      publishedObjectClickCounts: Object.create(null),
    };
  }

  function sceneDragMatchesActivePointer(state, event) {
    if (!state.active || state.pointerId == null) {
      return state.active;
    }
    if (!event || event.type === "lostpointercapture") {
      return true;
    }
    if (event.pointerId == null) {
      return true;
    }
    return event.pointerId === state.pointerId;
  }

  function scenePointerCanStartDrag(state, event) {
    if (state.active) {
      return false;
    }
    if (!event) {
      return false;
    }
    if (event.pointerType === "mouse") {
      return event.button === 0;
    }
    return event.button == null || event.button === 0;
  }

  function sceneDragTargetAtEvent(event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight) {
    const metrics = sceneDragViewportMetrics(readViewport, initialWidth, initialHeight);
    const pointer = sceneLocalPointerPoint(event, canvas, metrics.width, metrics.height);
    return sceneBundlePointerDragTarget(readSceneBundle && readSceneBundle(), pointer, metrics.width, metrics.height);
  }

  function scenePickMetricsAtEvent(event, canvas, readViewport, initialWidth, initialHeight) {
    const metrics = sceneDragViewportMetrics(readViewport, initialWidth, initialHeight);
    return {
      metrics,
      pointer: sceneLocalPointerPoint(event, canvas, metrics.width, metrics.height),
    };
  }

  function scenePickTargetAtEvent(event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight) {
    const sample = scenePickMetricsAtEvent(event, canvas, readViewport, initialWidth, initialHeight);
    return {
      metrics: sample.metrics,
      pointer: sample.pointer,
      target: sceneBundlePointerPickTarget(readSceneBundle && readSceneBundle(), sample.pointer, sample.metrics.width, sample.metrics.height),
    };
  }

  function updateSceneDragOrbit(state, sample, width, height) {
    state.orbitX = sceneClamp(state.orbitX + sample.deltaX / Math.max(width / 2, 1), SCENE_ORBIT_PITCH_MIN, SCENE_ORBIT_PITCH_MAX);
    state.orbitY = sceneClamp(state.orbitY - sample.deltaY / Math.max(height / 2, 1), SCENE_ORBIT_YAW_MIN, SCENE_ORBIT_YAW_MAX);
  }

  function publishSceneDragInteraction(canvas, event, phase, state, dragNamespace, readViewport, initialWidth, initialHeight) {
    const metrics = sceneDragViewportMetrics(readViewport, initialWidth, initialHeight);
    const sample = sceneLocalPointerSample(event, canvas, metrics.width, metrics.height, state, phase);
    if (!dragNamespace) {
      publishPointerSignals(sample);
      return;
    }
    if (phase === "move") {
      updateSceneDragOrbit(state, sample, metrics.width, metrics.height);
    }
    publishSceneDragSignals(dragNamespace, state, phase !== "end");
  }

  function resetSceneDragInteraction(state, dragNamespace, readViewport, initialWidth, initialHeight) {
    state.pointerId = null;
    state.targetIndex = -1;
    if (dragNamespace) {
      return;
    }
    const metrics = sceneDragViewportMetrics(readViewport, initialWidth, initialHeight);
    resetScenePointerSample(metrics.width, metrics.height, state);
  }

  function scenePrimaryPointerEvent(event) {
    if (!event) {
      return false;
    }
    if (event.pointerType === "mouse") {
      return event.button === 0 || event.button == null;
    }
    return event.button == null || event.button === 0;
  }

  function scenePickMatchesPointer(state, event) {
    if (state.pointerId == null) {
      return true;
    }
    if (!event || event.type === "lostpointercapture") {
      return true;
    }
    if (event.pointerId == null) {
      return true;
    }
    return event.pointerId === state.pointerId;
  }

  function sceneApplyPickTarget(state, sample) {
    const target = sample && sample.target ? sample.target : null;
    const pointer = sample && sample.pointer ? sample.pointer : { x: 0, y: 0 };
    state.pointerX = sceneNumber(pointer.x, 0);
    state.pointerY = sceneNumber(pointer.y, 0);
    state.hoverIndex = target ? target.index : -1;
    state.hoverID = sceneTargetID(target);
    state.hoverKind = sceneTargetKind(target);
    return target;
  }

  function sceneClearPickDown(state) {
    state.pointerId = null;
    state.downIndex = -1;
    state.downID = "";
    state.downKind = "";
  }

  function sceneSelectPickTarget(state, target) {
    state.selectedIndex = target ? target.index : -1;
    state.selectedID = sceneTargetID(target);
    state.selectedKind = sceneTargetKind(target);
  }

  function scenePickTargetsMatch(target, index, id) {
    if (!target) {
      return false;
    }
    const targetID = target.object && typeof target.object.id === "string" ? target.object.id : "";
    return target.index === index && targetID === id;
  }

  function scenePointerID(event) {
    return event && event.pointerId != null ? event.pointerId : null;
  }

  function sceneCapturePointer(canvas, pointerID) {
    if (pointerID == null || !canvas || typeof canvas.setPointerCapture !== "function") {
      return;
    }
    try {
      canvas.setPointerCapture(pointerID);
    } catch (_) {}
  }

  function sceneReleasePointer(canvas, pointerID) {
    if (pointerID == null || !canvas || typeof canvas.releasePointerCapture !== "function") {
      return;
    }
    try {
      canvas.releasePointerCapture(pointerID);
    } catch (_) {}
  }

  function sceneSnapshotTarget(index, id, kind) {
    const targetIndex = Math.max(-1, Math.floor(sceneNumber(index, -1)));
    const targetID = typeof id === "string" ? id : "";
    const targetKind = typeof kind === "string" ? kind : "";
    if (targetIndex < 0 && !targetID && !targetKind) {
      return null;
    }
    return {
      index: targetIndex,
      object: {
        id: targetID,
        kind: targetKind,
      },
    };
  }

  function scenePickStateSnapshot(state) {
    return {
      hover: sceneSnapshotTarget(state.hoverIndex, state.hoverID, state.hoverKind),
      down: sceneSnapshotTarget(state.downIndex, state.downID, state.downKind),
      selected: sceneSnapshotTarget(state.selectedIndex, state.selectedID, state.selectedKind),
      clickCount: Math.max(0, Math.floor(sceneNumber(state.clickCount, 0))),
      pointerX: sceneNumber(state.pointerX, 0),
      pointerY: sceneNumber(state.pointerY, 0),
    };
  }

  function sceneTargetsEqual(left, right) {
    if (!left || !right) {
      return left === right;
    }
    return sceneTargetIndex(left) === sceneTargetIndex(right) &&
      sceneTargetID(left) === sceneTargetID(right) &&
      sceneTargetKind(left) === sceneTargetKind(right);
  }

  function sceneDeriveInteractionEvent(action, before, after) {
    switch (action) {
      case "move":
        if (!sceneTargetsEqual(before.hover, after.hover)) {
          return after.hover ? { type: "hover", target: after.hover } : before.hover ? { type: "leave", target: before.hover } : null;
        }
        return null;
      case "down":
        if (after.down) {
          return { type: "down", target: after.down };
        }
        if (before.selected && !after.selected) {
          return { type: "deselect", target: before.selected };
        }
        return null;
      case "up":
        if (after.selected && (!sceneTargetsEqual(before.selected, after.selected) || after.clickCount !== before.clickCount)) {
          return { type: "select", target: after.selected };
        }
        if (before.selected && !after.selected) {
          return { type: "deselect", target: before.selected };
        }
        return null;
      case "cancel":
        return before.down ? { type: "cancel", target: before.down } : null;
      case "leave":
        return before.hover && !after.hover ? { type: "leave", target: before.hover } : null;
      default:
        return null;
    }
  }

  function sceneRecordInteractionEvent(state, interaction) {
    if (!state || !interaction || !interaction.type) {
      return null;
    }
    state.eventRevision = Math.max(0, Math.floor(sceneNumber(state.eventRevision, 0))) + 1;
    state.eventType = interaction.type;
    state.eventTargetIndex = sceneTargetIndex(interaction.target);
    state.eventTargetID = sceneTargetID(interaction.target);
    state.eventTargetKind = sceneTargetKind(interaction.target);
    const detail = sceneInteractionSnapshot(state);
    return {
      type: detail.type,
      revision: detail.revision,
      targetIndex: detail.targetIndex,
      targetID: detail.targetID,
      targetKind: detail.targetKind,
      hovered: detail.hovered,
      hoverIndex: detail.hoverIndex,
      hoverID: detail.hoverID,
      hoverKind: detail.hoverKind,
      down: detail.down,
      downIndex: detail.downIndex,
      downID: detail.downID,
      downKind: detail.downKind,
      selected: detail.selected,
      selectedIndex: detail.selectedIndex,
      selectedID: detail.selectedID,
      selectedKind: detail.selectedKind,
      clickCount: detail.clickCount,
      pointerX: detail.pointerX,
      pointerY: detail.pointerY,
    };
  }

  function publishSceneInteractionState(pickNamespace, eventNamespace, state) {
    publishScenePickSignals(pickNamespace, state);
    publishSceneEventSignals(eventNamespace, state);
  }


  function sceneHandlePickMove(state, event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight) {
    if (!scenePickMatchesPointer(state, event)) {
      return false;
    }
    sceneApplyPickTarget(state, scenePickTargetAtEvent(event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight));
    return true;
  }

  function sceneHandlePickDown(state, event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight) {
    if (!scenePrimaryPointerEvent(event)) {
      return false;
    }
    const sample = scenePickTargetAtEvent(event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight);
    const target = sceneApplyPickTarget(state, sample);
    if (!target) {
      sceneClearPickDown(state);
      sceneSelectPickTarget(state, null);
      return false;
    }
    state.pointerId = scenePointerID(event);
    state.downIndex = target.index;
    state.downID = sceneTargetID(target);
    state.downKind = sceneTargetKind(target);
    return true;
  }

  function sceneHandlePickUp(state, event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight) {
    if (!scenePickMatchesPointer(state, event)) {
      return { handled: false, pointerId: null };
    }
    const downIndex = state.downIndex;
    const downID = state.downID;
    const pointerID = state.pointerId;
    const sample = scenePickTargetAtEvent(event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight);
    const target = sceneApplyPickTarget(state, sample);
    if (downIndex >= 0) {
      if (scenePickTargetsMatch(target, downIndex, downID)) {
        state.clickCount += 1;
        sceneSelectPickTarget(state, target);
        const selectedSlug = sceneObjectSignalSlug(state.selectedIndex, state.selectedID, state.selectedKind);
        if (selectedSlug) {
          const previousCount = state.objectClickCounts && state.objectClickCounts[selectedSlug];
          state.objectClickCounts[selectedSlug] = Math.max(0, Math.floor(sceneNumber(previousCount, 0))) + 1;
        }
      } else if (!target) {
        sceneSelectPickTarget(state, null);
      }
    }
    sceneClearPickDown(state);
    return { handled: pointerID != null || downIndex >= 0, pointerId: pointerID };
  }

  function sceneHandlePickCancel(state, event) {
    if (!scenePickMatchesPointer(state, event)) {
      return { handled: false, pointerId: null };
    }
    const pointerID = state.pointerId;
    const handled = pointerID != null || state.downIndex >= 0;
    sceneClearPickDown(state);
    return { handled, pointerId: pointerID };
  }

  function sceneHandlePickLeave(state) {
    if (state.pointerId != null) {
      return false;
    }
    state.hoverIndex = -1;
    state.hoverID = "";
    state.hoverKind = "";
    return true;
  }

  function setupSceneDragInteractions(canvas, props, readViewport, readSceneBundle) {
    if (!canvas || !sceneBool(props.dragToRotate, false)) {
      return { dispose() {} };
    }

    const dragNamespace = sceneDragSignalNamespace(props);
    const initialMetrics = sceneDragViewportMetrics(readViewport, sceneNumber(props.width, 720), sceneNumber(props.height, 420));
    const initialWidth = initialMetrics.width;
    const initialHeight = initialMetrics.height;
    const state = createSceneDragState(initialWidth, initialHeight);
    let documentListenersAttached = false;

    canvas.style.cursor = "grab";
    canvas.style.touchAction = "none";

    function attachDocumentListeners() {
      if (documentListenersAttached) {
        return;
      }
      documentListenersAttached = true;
      document.addEventListener("pointermove", onPointerMove);
      document.addEventListener("pointerup", finishDrag);
      document.addEventListener("pointercancel", finishDrag);
    }

    function detachDocumentListeners() {
      if (!documentListenersAttached) {
        return;
      }
      documentListenersAttached = false;
      document.removeEventListener("pointermove", onPointerMove);
      document.removeEventListener("pointerup", finishDrag);
      document.removeEventListener("pointercancel", finishDrag);
    }

    function onPointerDown(event) {
      if (!scenePointerCanStartDrag(state, event)) {
        return;
      }
      const target = sceneDragTargetAtEvent(event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight);
      if (!target) {
        return;
      }
      state.active = true;
      state.pointerId = event.pointerId;
      state.targetIndex = target.index;
      canvas.style.cursor = "grabbing";
      attachDocumentListeners();
      if (typeof canvas.setPointerCapture === "function") {
        canvas.setPointerCapture(event.pointerId);
      }
      event.preventDefault();
      event.stopPropagation();
      publishSceneDragInteraction(canvas, event, "start", state, dragNamespace, readViewport, initialWidth, initialHeight);
    }

    function onPointerMove(event) {
      if (!sceneDragMatchesActivePointer(state, event)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      publishSceneDragInteraction(canvas, event, "move", state, dragNamespace, readViewport, initialWidth, initialHeight);
    }

    function finishDrag(event) {
      if (!sceneDragMatchesActivePointer(state, event)) {
        return;
      }
      const wasActive = state.active;
      state.active = false;
      canvas.style.cursor = "grab";
      detachDocumentListeners();
      if (!wasActive) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      if (state.pointerId != null && typeof canvas.releasePointerCapture === "function") {
        try {
          canvas.releasePointerCapture(state.pointerId);
        } catch (_) {}
      }
      state.pointerId = null;
      state.targetIndex = -1;
      publishSceneDragInteraction(canvas, event, "end", state, dragNamespace, readViewport, initialWidth, initialHeight);
      resetSceneDragInteraction(state, dragNamespace, readViewport, initialWidth, initialHeight);
    }

    canvas.addEventListener("pointerdown", onPointerDown);
    canvas.addEventListener("pointermove", onPointerMove);
    canvas.addEventListener("pointerup", finishDrag);
    canvas.addEventListener("pointercancel", finishDrag);
    canvas.addEventListener("lostpointercapture", finishDrag);

    return {
      dispose() {
        canvas.removeEventListener("pointerdown", onPointerDown);
        canvas.removeEventListener("pointermove", onPointerMove);
        canvas.removeEventListener("pointerup", finishDrag);
        canvas.removeEventListener("pointercancel", finishDrag);
        canvas.removeEventListener("lostpointercapture", finishDrag);
        detachDocumentListeners();
        canvas.style.cursor = "";
        canvas.style.touchAction = "";
        if (state.active && dragNamespace) {
          state.active = false;
          state.pointerId = null;
          state.targetIndex = -1;
          publishSceneDragSignals(dragNamespace, state, false);
        } else {
          state.active = false;
        }
        resetSceneDragInteraction(state, dragNamespace, readViewport, initialWidth, initialHeight);
      },
    };
  }

  function setupScenePickInteractions(canvas, props, readViewport, readSceneBundle, emitInteraction) {
    const pickNamespace = scenePickSignalNamespace(props);
    const eventNamespace = sceneEventSignalNamespace(props);
    if (!canvas || (!pickNamespace && !eventNamespace)) {
      return { dispose() {} };
    }

    const initialMetrics = sceneDragViewportMetrics(readViewport, sceneNumber(props.width, 720), sceneNumber(props.height, 420));
    const initialWidth = initialMetrics.width;
    const initialHeight = initialMetrics.height;
    const state = createScenePickState(initialWidth, initialHeight);
    let documentListenersAttached = false;

    function publish() {
      publishSceneInteractionState(pickNamespace, eventNamespace, state);
    }

    function emit(action, before) {
      const interaction = sceneDeriveInteractionEvent(action, before, scenePickStateSnapshot(state));
      const detail = sceneRecordInteractionEvent(state, interaction);
      if (detail && typeof emitInteraction === "function") {
        emitInteraction(detail);
      }
    }

    function attachDocumentListeners() {
      if (documentListenersAttached) {
        return;
      }
      documentListenersAttached = true;
      document.addEventListener("pointermove", onPointerMove);
      document.addEventListener("pointerup", onPointerUp);
      document.addEventListener("pointercancel", onPointerCancel);
    }

    function detachDocumentListeners() {
      if (!documentListenersAttached) {
        return;
      }
      documentListenersAttached = false;
      document.removeEventListener("pointermove", onPointerMove);
      document.removeEventListener("pointerup", onPointerUp);
      document.removeEventListener("pointercancel", onPointerCancel);
    }

    function onPointerMove(event) {
      const before = scenePickStateSnapshot(state);
      if (!sceneHandlePickMove(state, event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight)) {
        return;
      }
      emit("move", before);
      publish();
    }

    function onPointerDown(event) {
      const before = scenePickStateSnapshot(state);
      const handled = sceneHandlePickDown(state, event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight);
      emit("down", before);
      if (handled) {
        attachDocumentListeners();
        sceneCapturePointer(canvas, state.pointerId);
        if (typeof event.preventDefault === "function") {
          event.preventDefault();
        }
        if (typeof event.stopPropagation === "function") {
          event.stopPropagation();
        }
      }
      publish();
    }

    function onPointerUp(event) {
      const before = scenePickStateSnapshot(state);
      const result = sceneHandlePickUp(state, event, canvas, readViewport, readSceneBundle, initialWidth, initialHeight);
      if (!result.handled) {
        return;
      }
      emit("up", before);
      detachDocumentListeners();
      sceneReleasePointer(canvas, result.pointerId);
      if (typeof event.preventDefault === "function") {
        event.preventDefault();
      }
      if (typeof event.stopPropagation === "function") {
        event.stopPropagation();
      }
      publish();
    }

    function onPointerCancel(event) {
      const before = scenePickStateSnapshot(state);
      const result = sceneHandlePickCancel(state, event);
      if (!result.handled) {
        return;
      }
      emit("cancel", before);
      detachDocumentListeners();
      sceneReleasePointer(canvas, result.pointerId);
      publish();
    }

    function onPointerLeave() {
      const before = scenePickStateSnapshot(state);
      if (!sceneHandlePickLeave(state)) {
        return;
      }
      emit("leave", before);
      publish();
    }

    canvas.addEventListener("pointermove", onPointerMove);
    canvas.addEventListener("pointerdown", onPointerDown);
    canvas.addEventListener("pointerup", onPointerUp);
    canvas.addEventListener("pointercancel", onPointerCancel);
    canvas.addEventListener("pointerleave", onPointerLeave);
    canvas.addEventListener("lostpointercapture", onPointerCancel);
    publish();

    return {
      dispose() {
        canvas.removeEventListener("pointermove", onPointerMove);
        canvas.removeEventListener("pointerdown", onPointerDown);
        canvas.removeEventListener("pointerup", onPointerUp);
        canvas.removeEventListener("pointercancel", onPointerCancel);
        canvas.removeEventListener("pointerleave", onPointerLeave);
        canvas.removeEventListener("lostpointercapture", onPointerCancel);
        detachDocumentListeners();
        sceneReleasePointer(canvas, state.pointerId);
        state.pointerId = null;
        state.hoverIndex = -1;
        state.hoverID = "";
        state.hoverKind = "";
        state.downIndex = -1;
        state.downID = "";
        state.downKind = "";
        state.selectedIndex = -1;
        state.selectedID = "";
        state.selectedKind = "";
        state.clickCount = 0;
        state.eventType = "";
        state.eventTargetIndex = -1;
        state.eventTargetID = "";
        state.eventTargetKind = "";
        state.objectClickCounts = Object.create(null);
        publish();
      },
    };
  }
