// CPU mirror of the GLSL/WGSL ocean vertex pass. Plain JS for server-side tools.
(function() {
  // @ts-ignore TS7006 -- plain JS query is also evaluated by Node without transpilation.
  const smooth = (a, b, x) => { const t = Math.max(0, Math.min(1, (x - a) / (b - a))); return t * t * (3 - 2 * t); };
  // @ts-ignore TS7006 -- plain JS query is also evaluated by Node without transpilation.
  const slope = (a, b, x) => { const t = Math.max(0, Math.min(1, (x - a) / (b - a))); return 6 * t * (1 - t) / (b - a); };
  // @ts-ignore TS7006 -- plain JS query is also evaluated by Node without transpilation.
  function create(ocean, quality, floor) {
    const o = ocean || {}, data = new Float32Array(84);
    data[0] = o.level || 0; data[3] = Number.isFinite(o.speed) ? o.speed : 1;
    data[19] = Number.isFinite(o.surf) ? o.surf : 0.5;
    window.__gosx_scene3d_ocean_waves.write(o, quality, data);
    return { data, floor: floor || (() => -1e4) };
  }
  // Normal deliberately uses the GPU tangents: the shader does not differentiate
  // shoaling or run-up spatially. Velocity is the material (water-particle) velocity.
  // @ts-ignore TS7006 -- plain JS query is also evaluated by Node without transpilation.
  function evaluate(query, x, z, seconds) {
    const u = query.data, t = Math.fround(seconds), depth = u[0] - query.floor(x, z);
    let px = x, y = u[0], pz = z, vx = 0, vy = 0, vz = 0;
    let dxx = 1, dxy = 0, dxz = 0, dzx = 0, dzy = 0, dzz = 1;
    for (let i = 0; i < u[27]; i++) {
      const b = 36 + i * 8, wx = u[b], wz = u[b + 1], k = u[b + 2], omega = u[b + 3];
      const shoal = smooth(0, 1.2, depth * k), a = u[b + 4] * shoal, qa = u[b + 5] * shoal;
      const phase = k * (wx * x + wz * z) - omega * t + u[b + 6], s = Math.sin(phase), c = Math.cos(phase);
      px += qa * wx * c; y += a * s; pz += qa * wz * c;
      dxx -= k * qa * wx * wx * s; dxy += k * a * wx * c; dxz -= k * qa * wx * wz * s;
      dzx -= k * qa * wx * wz * s; dzy += k * a * wz * c; dzz -= k * qa * wz * wz * s;
      vx += qa * wx * omega * s; vy -= a * omega * c; vz += qa * wz * omega * s;
    }
    const phase = t * u[3] / 9 + 0.15 * Math.sin(x * 0.07 + 1.3) + 0.08 * Math.sin(x * 0.19);
    const ph = phase - Math.floor(phase), up = smooth(0, 0.28, ph), down = smooth(0.28, 1, ph);
    const runup = u[19] * 0.45 * (1 - smooth(0.3, 4, depth));
    y += runup * up * (1 - down);
    vy += runup * (slope(0, 0.28, ph) * (1 - down) - up * slope(0.28, 1, ph)) * u[3] / 9;
    const nx = dzy * dxz - dzz * dxy, ny = dzz * dxx - dzx * dxz, nz = dzx * dxy - dzy * dxx;
    const length = Math.hypot(nx, ny, nz) || 1;
    return { x: px, y, z: pz, normal: { x: nx / length, y: ny / length, z: nz / length },
      velocity: { x: vx, y: vy, z: vz }, tangentX: { x: dxx, y: dxy, z: dxz }, tangentZ: { x: dzx, y: dzy, z: dzz } };
  }
  // Two fixed-point steps supply a starting point. Newton refinement includes
  // floor gradients numerically; this also converges through a sloping shoreline.
  // @ts-ignore TS7006 -- plain JS query is also evaluated by Node without transpilation.
  function sample(query, x, z, seconds) {
    let qx = x, qz = z;
    for (let i = 0; i < 2; i++) {
      const p = evaluate(query, qx, qz, seconds); qx += x - p.x; qz += z - p.z;
    }
    for (let i = 0; i < 8; i++) {
      const p = evaluate(query, qx, qz, seconds), ex = p.x - x, ez = p.z - z;
      if (Math.hypot(ex, ez) < 1e-8) break;
      const h = 0.001, dx = evaluate(query, qx + h, qz, seconds), dz = evaluate(query, qx, qz + h, seconds);
      const a = (dx.x - p.x) / h, b = (dz.x - p.x) / h, c = (dx.z - p.z) / h, d = (dz.z - p.z) / h;
      const determinant = a * d - b * c;
      if (Math.abs(determinant) < 1e-8) { qx -= ex * 0.5; qz -= ez * 0.5; }
      else { qx -= (d * ex - b * ez) / determinant; qz -= (a * ez - c * ex) / determinant; }
    }
    return Object.assign(evaluate(query, qx, qz, seconds), { parameterX: qx, parameterZ: qz });
  }
  // Match textureSampleLevel/textureLod clamp-to-edge bilinear filtering in R,
  // then decode. Interpolating decoded signed-sqrt heights would be different.
  // @ts-ignore TS7006 -- plain JS query is also evaluated by Node without transpilation.
  function bathymetry(image, mapping) {
    const { width, height, data } = image, b = mapping;
  // @ts-ignore TS7006 -- plain JS query is also evaluated by Node without transpilation.
    return (x, z) => {
      const px = Math.max(0, Math.min(width - 1, (x - b.minX) / (b.maxX - b.minX) * width - 0.5));
      const pz = Math.max(0, Math.min(height - 1, (z - b.minZ) / (b.maxZ - b.minZ) * height - 0.5));
      const ix = Math.floor(px), iz = Math.floor(pz), jx = Math.min(ix + 1, width - 1), jz = Math.min(iz + 1, height - 1);
  // @ts-ignore TS7006 -- plain JS query is also evaluated by Node without transpilation.
      const tx = px - ix, tz = pz - iz, r = (xx, zz) => data[(zz * width + xx) * 4] / 255;
      const value = (r(ix, iz) * (1 - tx) + r(jx, iz) * tx) * (1 - tz) + (r(ix, jz) * (1 - tx) + r(jx, jz) * tx) * tz;
      if (b.encoding === "signed-sqrt") { const s = 2 * value - 1; return Math.sign(s) * s * s * Math.max(Math.abs(b.minHeight), Math.abs(b.maxHeight)); }
      return b.minHeight + value * (b.maxHeight - b.minHeight);
    };
  }
  window.__gosx_scene3d_ocean_query = { create, evaluate, sample, bathymetry };
})();
