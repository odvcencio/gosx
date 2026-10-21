// Multi-deck walking surfaces for a vessel. Decks are polygons in ship-local
// metres; this module keeps the ship's pose and answers height queries in world space.
(function() {
  const STEP = .5, ABOVE = 3.5;
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function polygon(flat) {
    const n = flat.length / 2;
    let x0 = Infinity, x1 = -Infinity, z0 = Infinity, z1 = -Infinity;
    for (let i = 0; i < n; i++) {x0 = Math.min(x0, flat[2 * i]);x1 = Math.max(x1, flat[2 * i]);z0 = Math.min(z0, flat[2 * i + 1]);z1 = Math.max(z1, flat[2 * i + 1]);}
    return {p: Float64Array.from(flat), n, x0, x1, z0, z1};
  }
  // Even-odd ray cast, the same rule as vessel.Polygon.Contains in Go.
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function inside(poly, x, z) {
    if (x < poly.x0 || x > poly.x1 || z < poly.z0 || z > poly.z1) return false;
    const p = poly.p;
    let hit = false;
    for (let i = 0, j = poly.n - 1; i < poly.n; j = i++) {
      const ax = p[2 * i], az = p[2 * i + 1], bx = p[2 * j], bz = p[2 * j + 1];
      if ((az > z) !== (bz > z) && x < (bx - ax) * (z - az) / (bz - az) + ax) hit = !hit;
    }
    return hit;
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function onDeck(deck, x, z) {
    for (const h of deck.holes) if (inside(h, x, z)) return false;
    for (const w of deck.walk) if (inside(w, x, z)) return true;
    return false;
  }
  // @ts-ignore TS7006 -- this governed module is also evaluated as plain JS in Node tests.
  function create(config, physics) {
    // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
    const list = config.map((d, index) => ({index, name: d.name || '', y: d.y, walk: (d.polygons || []).map(polygon), holes: (d.holes || []).map(polygon), ramps: d.ramps || []}));
    // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
    list.sort((a, b) => a.y - b.y);
    // m holds the world image of the ship's X, Y and Z axes; o is the origin.
    const m = new Float64Array(9), o = {x: 0, y: 0, z: 0}, probe = {x: 0, y: 0, z: 0, heading: 0, pitch: 0, roll: 0};
    // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
    function update(pose) {
      Object.assign(probe, {x: pose.x, y: pose.y, z: pose.z, heading: pose.heading, pitch: pose.pitch, roll: pose.roll});
      const origin = physics.localPoint(probe, 0, 0, 0);
      o.x = origin.x;o.y = origin.y;o.z = origin.z;
      for (let axis = 0; axis < 3; axis++) {
        const p = physics.localPoint(probe, axis === 0 ? 1 : 0, axis === 1 ? 1 : 0, axis === 2 ? 1 : 0);
        m[3 * axis] = p.x - origin.x;m[3 * axis + 1] = p.y - origin.y;m[3 * axis + 2] = p.z - origin.z;
      }
    }
    // The deck is a plane in world space. Given a world x and z, find the point of that plane above them.
    // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
    function height(x, z, feet) {
      const dx = x - o.x, dz = z - o.z, limit = (feet === undefined ? Infinity : feet) + STEP;
      const ax = m[0], ay = m[1], az = m[2], bx = m[3], by = m[4], bz = m[5], cx = m[6], cy = m[7], cz = m[8];
      let best = -Infinity;
      for (const d of list) {
        const dy = (d.y - bx * dx - bz * dz) / by, wy = o.y + dy;
        if (wy > limit || wy <= best) continue;
        if (onDeck(d, ax * dx + ay * dy + az * dz, cx * dx + cy * dy + cz * dz)) best = wy;
        for (const r of d.ramps) {
          const k = (r.toY - r.fromY) / r.sizeZ, z0 = r.z - r.sizeZ / 2;
          const ry = (r.fromY + k * (cx * dx + cz * dz - z0) - (bx * dx + bz * dz)) / (by - k * cy), rw = o.y + ry;
          if (rw > limit || rw <= best) continue;
          if (Math.abs(ax * dx + ay * ry + az * dz - r.x) <= r.sizeX / 2 && Math.abs(cx * dx + cy * ry + cz * dz - r.z) <= r.sizeZ / 2) best = rw;
        }
      }
      return best;
    }
    // World to ship-local, using the last update.
    // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
    function toLocal(w) {
      const dx = w.x - o.x, dy = w.y - o.y, dz = w.z - o.z;
      return {x: m[0] * dx + m[1] * dy + m[2] * dz, y: m[3] * dx + m[4] * dy + m[5] * dz, z: m[6] * dx + m[7] * dy + m[8] * dz};
    }
    // The deck index (as sent by Go) under a ship-local point for a walker whose feet are at local.y, or -1.
    // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
    function deckAt(local) {
      for (let i = list.length - 1; i >= 0; i--) if (list[i].y <= local.y + STEP && onDeck(list[i], local.x, local.z)) return list[i].index;
      return -1;
    }
    // True when a ship-local point is on or just above any deck: the walker rides the ship.
    // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
    function aboard(local) {
      for (const d of list) if (local.y >= d.y - STEP && local.y <= d.y + ABOVE && onDeck(d, local.x, local.z)) return true;
      return false;
    }
    // A walk surface for Walk.Surfaces: height(x, z, terrain, feet) asks it through sample.
    // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
    const surface = {sample: (x, z, feet) => height(x, z, feet)};
    // @ts-ignore TS7006 -- plain JS parameters are exercised without transpilation.
    return {list, axes: m, origin: o, update, height, toLocal, deckAt, aboard, surface, y: index => list.find(d => d.index === index)?.y};
  }
  window.__gosx_scene3d_vessel_decks = {create, polygon, inside};
})();
