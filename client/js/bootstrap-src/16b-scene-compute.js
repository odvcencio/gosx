  // GPU compute particle system for GoSX Scene3D.
  // WGSL compute shaders for particle simulation + JS CPU fallback.
  // Shared IIFE scope: sceneNumber, sceneColorRGBA available from scene-math.js.

  // --- WGSL Compute Shader Source ---

  var SCENE_COMPUTE_PARTICLE_SOURCE = [
    "struct Particle {",
    "    posX: f32, posY: f32, posZ: f32,",
    "    velX: f32, velY: f32, velZ: f32,",
    "    age: f32,",
    "    lifetime: f32,",
    "};",
    "",
    "struct RenderVertex {",
    "    posX: f32, posY: f32, posZ: f32,",
    "    size: f32,",
    "    r: f32, g: f32, b: f32, a: f32,",
    "};",
    "",
    "struct SimParams {",
    "    deltaTime: f32,",
    "    totalTime: f32,",
    "    count: u32,",
    "    _pad0: u32,",
    "    emitterKind: u32,",
    "    emitterX: f32, emitterY: f32, emitterZ: f32,",
    "    emitterRadius: f32,",
    "    emitterRate: f32,",
    "    emitterLifetime: f32,",
    "    _pad1: u32,",
    "    emitterArms: u32,",
    "    emitterWind: f32,",
    "    emitterScatter: f32,",
    "    emitterRotX: f32, emitterRotY: f32, emitterRotZ: f32,",
    "    _pad2: u32,",
    "    sizeStart: f32, sizeEnd: f32,",
    "    colorStartR: f32, colorStartG: f32, colorStartB: f32,",
    "    colorEndR: f32, colorEndG: f32, colorEndB: f32,",
    "    opacityStart: f32, opacityEnd: f32,",
    "    forceCount: u32,",
    "    _pad3: u32,",
    "};",
    "",
    "struct Force {",
    "    kind: u32,",
    "    strength: f32,",
    "    dirX: f32, dirY: f32, dirZ: f32,",
    "    frequency: f32,",
    "    _pad0: f32, _pad1: f32,",
    "};",
    "",
    "@group(0) @binding(0) var<storage, read_write> particles: array<Particle>;",
    "@group(0) @binding(1) var<storage, read_write> renderData: array<RenderVertex>;",
    "@group(0) @binding(2) var<uniform> params: SimParams;",
    "@group(0) @binding(3) var<storage, read> forces: array<Force>;",
    "",
    "fn hash(seed: u32) -> f32 {",
    "    var s = seed;",
    "    s = s ^ (s >> 16u);",
    "    s = s * 0x45d9f3bu;",
    "    s = s ^ (s >> 16u);",
    "    s = s * 0x45d9f3bu;",
    "    s = s ^ (s >> 16u);",
    "    return f32(s) / f32(0xffffffffu);",
    "}",
    "",
    "fn hash2(a: u32, b: u32) -> f32 {",
    "    return hash(a * 1597334677u + b * 3812015801u);",
    "}",
    "",
    "fn emitPoint(index: u32, p: SimParams) -> Particle {",
    "    var out: Particle;",
    "    out.posX = p.emitterX;",
    "    out.posY = p.emitterY;",
    "    out.posZ = p.emitterZ;",
    "    out.velX = (hash2(index, 0u) - 0.5) * 0.1;",
    "    out.velY = (hash2(index, 1u) - 0.5) * 0.1;",
    "    out.velZ = (hash2(index, 2u) - 0.5) * 0.1;",
    "    out.age = 0.0;",
    "    out.lifetime = p.emitterLifetime;",
    "    return out;",
    "}",
    "",
    "fn emitSphere(index: u32, p: SimParams) -> Particle {",
    "    var out: Particle;",
    "    let theta = hash2(index, 10u) * 6.283185;",
    "    let phi = acos(2.0 * hash2(index, 11u) - 1.0);",
    "    let r = p.emitterRadius * pow(hash2(index, 12u), 0.333);",
    "    out.posX = p.emitterX + r * sin(phi) * cos(theta);",
    "    out.posY = p.emitterY + r * cos(phi);",
    "    out.posZ = p.emitterZ + r * sin(phi) * sin(theta);",
    "    out.velX = 0.0; out.velY = 0.0; out.velZ = 0.0;",
    "    out.age = 0.0;",
    "    out.lifetime = p.emitterLifetime;",
    "    return out;",
    "}",
    "",
    "fn emitDisc(index: u32, p: SimParams) -> Particle {",
    "    var out: Particle;",
    "    let angle = hash2(index, 20u) * 6.283185;",
    "    let r = p.emitterRadius * sqrt(hash2(index, 21u));",
    "    out.posX = p.emitterX + r * cos(angle);",
    "    out.posY = p.emitterY;",
    "    out.posZ = p.emitterZ + r * sin(angle);",
    "    out.velX = 0.0; out.velY = 0.0; out.velZ = 0.0;",
    "    out.age = 0.0;",
    "    out.lifetime = p.emitterLifetime;",
    "    return out;",
    "}",
    "",
    "fn rotateEulerZYX(lx: f32, ly: f32, lz: f32, rx: f32, ry: f32, rz: f32) -> vec3<f32> {",
    "    let cx = cos(rx); let sx = sin(rx);",
    "    let cy = cos(ry); let sy = sin(ry);",
    "    let cz = cos(rz); let sz = sin(rz);",
    "    return vec3<f32>(",
    "        lx*(cy*cz) + ly*(sx*sy*cz - cx*sz) + lz*(cx*sy*cz + sx*sz),",
    "        lx*(cy*sz) + ly*(sx*sy*sz + cx*cz) + lz*(cx*sy*sz - sx*cz),",
    "        lx*(-sy)   + ly*(sx*cy)             + lz*(cx*cy)",
    "    );",
    "}",
    "",
    "fn emitSpiral(index: u32, p: SimParams) -> Particle {",
    "    var out: Particle;",
    "    let radius = hash2(index, 30u) * p.emitterRadius;",
    "    let arm = index % p.emitterArms;",
    "    let armAngle = f32(arm) * 3.14159265 / f32(max(p.emitterArms / 2u, 1u));",
    "    let spiralAngle = armAngle + (radius / p.emitterRadius) * p.emitterWind;",
    "    let scatter = (hash2(index, 31u) - 0.5) * radius * p.emitterScatter;",
    "    let lx = cos(spiralAngle) * radius + scatter;",
    "    let ly = (hash2(index, 32u) - 0.5) * p.emitterRadius * 0.05;",
    "    let lz = sin(spiralAngle) * radius + (hash2(index, 33u) - 0.5) * radius * p.emitterScatter;",
    "    let rotated = rotateEulerZYX(lx, ly, lz, p.emitterRotX, p.emitterRotY, p.emitterRotZ);",
    "    out.posX = p.emitterX + rotated.x;",
    "    out.posY = p.emitterY + rotated.y;",
    "    out.posZ = p.emitterZ + rotated.z;",
    "    out.velX = 0.0; out.velY = 0.0; out.velZ = 0.0;",
    "    out.age = 0.0;",
    "    out.lifetime = p.emitterLifetime;",
    "    return out;",
    "}",
    "",
    "fn emitParticle(index: u32, p: SimParams) -> Particle {",
    "    switch (p.emitterKind) {",
    "        case 1u: { return emitSphere(index, p); }",
    "        case 2u: { return emitDisc(index, p); }",
    "        case 3u: { return emitSpiral(index, p); }",
    "        default: { return emitPoint(index, p); }",
    "    }",
    "}",
    "",
    "fn applyGravity(part: Particle, f: Force, dt: f32) -> vec3f {",
    "    return vec3f(f.dirX, f.dirY, f.dirZ) * f.strength * dt;",
    "}",
    "",
    "fn applyWind(part: Particle, f: Force, dt: f32) -> vec3f {",
    "    return vec3f(f.dirX, f.dirY, f.dirZ) * f.strength * dt;",
    "}",
    "",
    "fn applyTurbulence(part: Particle, f: Force, dt: f32, time: f32) -> vec3f {",
    "    let freq = f.frequency;",
    "    let nx = sin(part.posX * freq + time * 1.3) * cos(part.posZ * freq + time * 0.7);",
    "    let ny = sin(part.posY * freq + time * 0.9) * cos(part.posX * freq + time * 1.1);",
    "    let nz = sin(part.posZ * freq + time * 1.7) * cos(part.posY * freq + time * 0.5);",
    "    return vec3f(nx, ny, nz) * f.strength * dt;",
    "}",
    "",
    "fn applyOrbit(part: Particle, f: Force, dt: f32) -> vec3f {",
    "    let dx = part.posX;",
    "    let dz = part.posZ;",
    "    let dist = max(sqrt(dx * dx + dz * dz), 0.001);",
    "    return vec3f(-dz / dist, 0.0, dx / dist) * f.strength * dt;",
    "}",
    "",
    "fn applyDrag(part: Particle, f: Force, dt: f32) -> vec3f {",
    "    return vec3f(-part.velX, -part.velY, -part.velZ) * f.strength * dt;",
    "}",
    "",
    "@compute @workgroup_size(64)",
    "fn simulate(@builtin(global_invocation_id) id: vec3u) {",
    "    let i = id.x;",
    "    if (i >= params.count) { return; }",
    "",
    "    var p = particles[i];",
    "",
    "    if (p.age < 0.0) {",
    "        p = emitParticle(i, params);",
    "    }",
    "",
    "    p.age += params.deltaTime;",
    "",
    "    if (p.lifetime > 0.0 && p.age >= p.lifetime) {",
    "        p = emitParticle(i, params);",
    "    }",
    "",
    "    for (var fi = 0u; fi < params.forceCount; fi++) {",
    "        let f = forces[fi];",
    "        switch (f.kind) {",
    "            case 0u: { let dv = applyGravity(p, f, params.deltaTime); p.velX += dv.x; p.velY += dv.y; p.velZ += dv.z; }",
    "            case 1u: { let dv = applyWind(p, f, params.deltaTime); p.velX += dv.x; p.velY += dv.y; p.velZ += dv.z; }",
    "            case 2u: { let dv = applyTurbulence(p, f, params.deltaTime, params.totalTime); p.velX += dv.x; p.velY += dv.y; p.velZ += dv.z; }",
    "            case 3u: { let dv = applyOrbit(p, f, params.deltaTime); p.velX += dv.x; p.velY += dv.y; p.velZ += dv.z; }",
    "            case 4u: { let dv = applyDrag(p, f, params.deltaTime); p.velX += dv.x; p.velY += dv.y; p.velZ += dv.z; }",
    "            default: {}",
    "        }",
    "    }",
    "",
    "    p.posX += p.velX * params.deltaTime;",
    "    p.posY += p.velY * params.deltaTime;",
    "    p.posZ += p.velZ * params.deltaTime;",
    "",
    "    particles[i] = p;",
    "",
    "    let t = select(p.age / p.lifetime, 0.0, p.lifetime <= 0.0);",
    "    var rv: RenderVertex;",
    "    rv.posX = p.posX;",
    "    rv.posY = p.posY;",
    "    rv.posZ = p.posZ;",
    "    rv.size = mix(params.sizeStart, params.sizeEnd, t);",
    "    rv.r = mix(params.colorStartR, params.colorEndR, t);",
    "    rv.g = mix(params.colorStartG, params.colorEndG, t);",
    "    rv.b = mix(params.colorStartB, params.colorEndB, t);",
    "    rv.a = mix(params.opacityStart, params.opacityEnd, t);",
    "    renderData[i] = rv;",
    "}",
  ].join("\n");

  var sceneParticleForceBuiltins = { gravity: 0, wind: 1, turbulence: 2, orbit: 3, drag: 4 };
  var sceneParticleForceAliases = Object.create(null);
  var sceneParticleForceHandlers = Object.create(null);
  var sceneParticleForceRegistryVersion = 0;

  function sceneParticleForceKindKey(value) {
    var key = typeof value === "string" ? value.trim().toLowerCase() : "";
    return key && /^[a-z][a-z0-9_-]*$/.test(key) ? key : "";
  }

  function sceneParticleForceKindCode(kind) {
    var key = sceneParticleForceKindKey(kind);
    if (Object.prototype.hasOwnProperty.call(sceneParticleForceBuiltins, key)) {
      return sceneParticleForceBuiltins[key];
    }
    if (Object.prototype.hasOwnProperty.call(sceneParticleForceAliases, key)) {
      return sceneParticleForceAliases[key];
    }
    return 0;
  }

  function registerSceneParticleForceKind(kind, targetKind) {
    var key = sceneParticleForceKindKey(kind);
    var target = sceneParticleForceKindKey(targetKind);
    if (!key) {
      return false;
    }
    var code;
    if (Object.prototype.hasOwnProperty.call(sceneParticleForceBuiltins, target)) {
      code = sceneParticleForceBuiltins[target];
    } else if (Object.prototype.hasOwnProperty.call(sceneParticleForceAliases, target)) {
      code = sceneParticleForceAliases[target];
    } else {
      return false;
    }
    sceneParticleForceAliases[key] = code;
    sceneParticleForceRegistryVersion += 1;
    return true;
  }

  function registerSceneParticleForce(kind, force) {
    var key = sceneParticleForceKindKey(kind);
    if (!key) {
      return false;
    }
    if (typeof force === "function") {
      sceneParticleForceHandlers[key] = force;
      sceneParticleForceRegistryVersion += 1;
      return true;
    }
    if (force && typeof force === "object") {
      var handler = typeof force.apply === "function" ? force.apply : (typeof force.update === "function" ? force.update : null);
      var aliasRegistered = false;
      if (typeof force.kind === "string") {
        aliasRegistered = registerSceneParticleForceKind(key, force.kind);
      }
      if (typeof force.kind === "string" && !aliasRegistered) {
        return false;
      }
      if (handler) {
        sceneParticleForceHandlers[key] = handler;
        sceneParticleForceRegistryVersion += 1;
        return true;
      }
      if (aliasRegistered) {
        return true;
      }
    }
    return false;
  }

  function unregisterSceneParticleForce(kind) {
    var key = sceneParticleForceKindKey(kind);
    var removed = false;
    if (Object.prototype.hasOwnProperty.call(sceneParticleForceAliases, key)) {
      delete sceneParticleForceAliases[key];
      removed = true;
    }
    if (Object.prototype.hasOwnProperty.call(sceneParticleForceHandlers, key)) {
      delete sceneParticleForceHandlers[key];
      removed = true;
    }
    if (removed) {
      sceneParticleForceRegistryVersion += 1;
    }
    return removed;
  }

  function listSceneParticleForces() {
    var keys = Object.keys(sceneParticleForceBuiltins)
      .concat(Object.keys(sceneParticleForceAliases))
      .concat(Object.keys(sceneParticleForceHandlers));
    return Array.from(new Set(keys)).sort().map(function(kind) {
      return {
        kind: kind,
        builtin: Object.prototype.hasOwnProperty.call(sceneParticleForceBuiltins, kind),
        alias: Object.prototype.hasOwnProperty.call(sceneParticleForceAliases, kind),
        handler: Object.prototype.hasOwnProperty.call(sceneParticleForceHandlers, kind),
      };
    });
  }

  function sceneParticleForceHandler(kind) {
    var key = sceneParticleForceKindKey(kind);
    return key && sceneParticleForceHandlers[key] || null;
  }

  function sceneApplyParticleForceHandler(handler, context) {
    var result = handler(context);
    if (!result) {
      return context.velocity;
    }
    if (Array.isArray(result) || (result && typeof result.length === "number")) {
      context.velocity.x += sceneNumber(result[0], 0);
      context.velocity.y += sceneNumber(result[1], 0);
      context.velocity.z += sceneNumber(result[2], 0);
      return context.velocity;
    }
    var nextVelocity = result.velocity && typeof result.velocity === "object" ? result.velocity : null;
    if (nextVelocity) {
      context.velocity.x = sceneNumber(nextVelocity.x, context.velocity.x);
      context.velocity.y = sceneNumber(nextVelocity.y, context.velocity.y);
      context.velocity.z = sceneNumber(nextVelocity.z, context.velocity.z);
      return context.velocity;
    }
    context.velocity.x += sceneNumber(Object.prototype.hasOwnProperty.call(result, "x") ? result.x : result.vx, 0);
    context.velocity.y += sceneNumber(Object.prototype.hasOwnProperty.call(result, "y") ? result.y : result.vy, 0);
    context.velocity.z += sceneNumber(Object.prototype.hasOwnProperty.call(result, "z") ? result.z : result.vz, 0);
    return context.velocity;
  }

  // --- SimParams Upload Helper ---

  function sceneComputeUploadSimParams(device, buffer, entry, deltaTime, totalTime) {
    var emitter = entry.emitter || {};
    var material = entry.material || {};
    var forces = entry.forces || [];

    var kindMap = { point: 0, sphere: 1, disc: 2, spiral: 3 };
    var emitterKind = kindMap[emitter.kind] || 0;

    var colorStart = sceneColorRGBA(material.color || "#ffffff", [1, 1, 1, 1]);
    var colorEnd = sceneColorRGBA(material.colorEnd || material.color || "#ffffff", [1, 1, 1, 1]);

    var buf = new ArrayBuffer(256);
    var view = new DataView(buf);
    var offset = 0;

    // SimParams layout (must match WGSL struct alignment):
    view.setFloat32(offset, deltaTime, true); offset += 4;            // deltaTime
    view.setFloat32(offset, totalTime, true); offset += 4;            // totalTime
    view.setUint32(offset, entry.count, true); offset += 4;           // count
    view.setUint32(offset, 0, true); offset += 4;                     // _pad0

    view.setUint32(offset, emitterKind, true); offset += 4;           // emitterKind
    view.setFloat32(offset, sceneNumber(emitter.x, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.y, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.z, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.radius, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.rate, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.lifetime, 0), true); offset += 4;
    view.setUint32(offset, 0, true); offset += 4;                     // _pad1

    view.setUint32(offset, sceneNumber(emitter.arms, 2), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.wind, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.scatter, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.rotationX, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.rotationY, 0), true); offset += 4;
    view.setFloat32(offset, sceneNumber(emitter.rotationZ, 0), true); offset += 4;
    view.setUint32(offset, 0, true); offset += 4;                     // _pad2

    // Material interpolation
    view.setFloat32(offset, sceneNumber(material.size, 1), true); offset += 4;
    view.setFloat32(offset, sceneNumber(material.sizeEnd, material.size || 1), true); offset += 4;
    view.setFloat32(offset, colorStart[0], true); offset += 4;
    view.setFloat32(offset, colorStart[1], true); offset += 4;
    view.setFloat32(offset, colorStart[2], true); offset += 4;
    view.setFloat32(offset, colorEnd[0], true); offset += 4;
    view.setFloat32(offset, colorEnd[1], true); offset += 4;
    view.setFloat32(offset, colorEnd[2], true); offset += 4;
    view.setFloat32(offset, sceneNumber(material.opacity, 1), true); offset += 4;
    view.setFloat32(offset, sceneNumber(material.opacityEnd, material.opacity || 1), true); offset += 4;

    // Force count
    view.setUint32(offset, forces.length, true); offset += 4;
    view.setUint32(offset, 0, true); offset += 4;                     // _pad3

    device.queue.writeBuffer(buffer, 0, buf, 0, 256);
  }

  function sceneComputeUploadForces(device, buffer, forces) {
    var byteLen = Math.max(32, forces.length * 32);
    var buf = new ArrayBuffer(byteLen);
    var view = new DataView(buf);

    for (var i = 0; i < forces.length; i++) {
      var f = forces[i];
      var off = i * 32;
      view.setUint32(off, sceneParticleForceKindCode(f && f.kind), true);
      view.setFloat32(off + 4, sceneNumber(f.strength, 0), true);
      view.setFloat32(off + 8, sceneNumber(f.x, 0), true);
      view.setFloat32(off + 12, sceneNumber(f.y, 0), true);
      view.setFloat32(off + 16, sceneNumber(f.z, 0), true);
      view.setFloat32(off + 20, sceneNumber(f.frequency, 1), true);
      // 8 bytes padding (offsets 24-31)
    }

    device.queue.writeBuffer(buffer, 0, buf);
  }

  // --- GPU Compute Particle System ---

  function createSceneComputeParticleSystem(device, entry) {
    var count = entry.count || 0;
    if (count <= 0) return null;

    // Particle state: 8 floats per particle (pos xyz, vel xyz, age, lifetime).
    var particleBuffer = device.createBuffer({
      size: count * 32,
      usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,
    });

    // Render output: 8 floats per vertex (pos xyz, size, rgba).
    var renderBuffer = device.createBuffer({
      size: count * 32,
      usage: GPUBufferUsage.STORAGE | GPUBufferUsage.VERTEX,
    });

    // Uniform buffer for simulation parameters.
    var paramsBuffer = device.createBuffer({
      size: 256,
      usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
    });

    // Storage buffer for force descriptors.
    var forceCount = (entry.forces || []).length;
    var forceBuffer = device.createBuffer({
      size: Math.max(32, forceCount * 32),
      usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,
    });

    // Initialize all particle ages to -1 (triggers first-frame emission in shader).
    var initData = new Float32Array(count * 8);
    for (var i = 0; i < count; i++) {
      initData[i * 8 + 6] = -1.0;
    }
    device.queue.writeBuffer(particleBuffer, 0, initData);

    // Upload static force data.
    sceneComputeUploadForces(device, forceBuffer, entry.forces || []);

    // Compile compute pipeline (async).
    var computePipeline = null;
    var bindGroup = null;
    var ready = false;

    var shaderModule = device.createShaderModule({
      code: SCENE_COMPUTE_PARTICLE_SOURCE,
    });

    var bindGroupLayout = device.createBindGroupLayout({
      entries: [
        { binding: 0, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
        { binding: 1, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
        { binding: 2, visibility: GPUShaderStage.COMPUTE, buffer: { type: "uniform" } },
        { binding: 3, visibility: GPUShaderStage.COMPUTE, buffer: { type: "read-only-storage" } },
      ],
    });

    var pipelineLayout = device.createPipelineLayout({
      bindGroupLayouts: [bindGroupLayout],
    });

    device.createComputePipelineAsync({
      layout: pipelineLayout,
      compute: {
        module: shaderModule,
        entryPoint: "simulate",
      },
    }).then(function(pipeline) {
      computePipeline = pipeline;
      bindGroup = device.createBindGroup({
        layout: bindGroupLayout,
        entries: [
          { binding: 0, resource: { buffer: particleBuffer } },
          { binding: 1, resource: { buffer: renderBuffer } },
          { binding: 2, resource: { buffer: paramsBuffer } },
          { binding: 3, resource: { buffer: forceBuffer } },
        ],
      });
      ready = true;
    }).catch(function(err) {
      console.warn("[gosx] Compute particle pipeline creation failed:", err);
    });

    return {
      count: count,
      renderBuffer: renderBuffer,
      entry: entry,
      isReady: function() {
        return ready;
      },

      update: function(device, encoder, deltaTime, totalTime) {
        if (!ready) return;

        sceneComputeUploadSimParams(device, paramsBuffer, entry, deltaTime, totalTime);

        var pass = encoder.beginComputePass();
        pass.setPipeline(computePipeline);
        pass.setBindGroup(0, bindGroup);
        pass.dispatchWorkgroups(Math.ceil(count / 64));
        pass.end();
      },

      dispose: function() {
        particleBuffer.destroy();
        renderBuffer.destroy();
        paramsBuffer.destroy();
        forceBuffer.destroy();
        computePipeline = null;
        bindGroup = null;
        ready = false;
      },
    };
  }

  // --- CPU Fallback Particle System ---
  // Mirrors the WGSL compute shader logic in plain JS for WebGL2 environments.
  // Produces positions/sizes/colors arrays compatible with drawPointsEntries.

  function createSceneCPUParticleSystem(entry) {
    var count = Math.min(entry.count || 0, 10000);
    if (count <= 0) return null;

    // Particle state: same 8-float layout as GPU (posXYZ, velXYZ, age, lifetime).
    var particles = new Float32Array(count * 8);

    // Output arrays for the Points renderer.
    var positions = new Float32Array(count * 3);
    var sizes = new Float32Array(count);
    var colors = new Float32Array(count * 3);
    var opacities = new Float32Array(count);

    // Initialize ages to -1 (uninitialized marker).
    for (var i = 0; i < count; i++) {
      particles[i * 8 + 6] = -1.0;
    }

    // Deterministic hash RNG (same as WGSL).
    function hash(seed) {
      var s = seed >>> 0;
      s = s ^ (s >>> 16);
      s = Math.imul(s, 0x45d9f3b) >>> 0;
      s = s ^ (s >>> 16);
      s = Math.imul(s, 0x45d9f3b) >>> 0;
      s = s ^ (s >>> 16);
      return (s >>> 0) / 0xffffffff;
    }

    function hash2(a, b) {
      return hash(Math.imul(a >>> 0, 1597334677) + Math.imul(b >>> 0, 3812015801));
    }

    // Emitter kind map.
    var kindMap = { point: 0, sphere: 1, disc: 2, spiral: 3 };

    function currentEmitterConfig() {
      var emitter = entry && entry.emitter && typeof entry.emitter === "object" ? entry.emitter : {};
      // NOTE: emit in LOCAL space (origin-centered, no rotation). The compute
      // particle render bridge sets x/y/z/rotation/spin on the rendered Points
      // entry so the model matrix applies position + rotation + spin at draw time.
      // This matches how static Points work and gives us free galaxy spin.
      return {
        kind: kindMap[emitter.kind] || 0,
        x: 0,
        y: 0,
        z: 0,
        radius: sceneNumber(emitter.radius, 0),
        lifetime: sceneNumber(emitter.lifetime, 0),
        arms: Math.max(1, Math.floor(sceneNumber(emitter.arms, 2))),
        wind: sceneNumber(emitter.wind, 0),
        scatter: sceneNumber(emitter.scatter, 0),
      };
    }

    function currentMaterialConfig() {
      var material = entry && entry.material && typeof entry.material === "object" ? entry.material : {};
      return {
        colorStart: sceneColorRGBA(material.color || "#ffffff", [1, 1, 1, 1]),
        colorEnd: sceneColorRGBA(material.colorEnd || material.color || "#ffffff", [1, 1, 1, 1]),
        sizeStart: sceneNumber(material.size, 1),
        sizeEnd: sceneNumber(material.sizeEnd, material.size || 1),
        opacityStart: sceneNumber(material.opacity, 1),
        opacityEnd: sceneNumber(material.opacityEnd, material.opacity || 1),
      };
    }

    function currentForces() {
      return Array.isArray(entry && entry.forces) ? entry.forces : [];
    }

    function emitParticle(index, base, emitterConfig) {
      // All emitters produce LOCAL coordinates (origin-centered). The render
      // pipeline applies the emitter's world position, rotation, and spin via
      // the model matrix on the Points entry.
      switch (emitterConfig.kind) {
        case 1: { // sphere
          var theta = hash2(index, 10) * 6.283185;
          var phi = Math.acos(2.0 * hash2(index, 11) - 1.0);
          var r = emitterConfig.radius * Math.pow(hash2(index, 12), 0.333);
          base[0] = r * Math.sin(phi) * Math.cos(theta);
          base[1] = r * Math.cos(phi);
          base[2] = r * Math.sin(phi) * Math.sin(theta);
          base[3] = 0; base[4] = 0; base[5] = 0;
          break;
        }
        case 2: { // disc
          var angle = hash2(index, 20) * 6.283185;
          var dr = emitterConfig.radius * Math.sqrt(hash2(index, 21));
          base[0] = dr * Math.cos(angle);
          base[1] = 0;
          base[2] = dr * Math.sin(angle);
          base[3] = 0; base[4] = 0; base[5] = 0;
          break;
        }
        case 3: { // spiral (local space)
          var radius = hash2(index, 30) * emitterConfig.radius;
          var arm = index % emitterConfig.arms;
          var armAngle = arm * 3.14159265 / Math.max(emitterConfig.arms / 2, 1);
          var spiralAngle = armAngle + (radius / Math.max(emitterConfig.radius, 0.001)) * emitterConfig.wind;
          var scatter = (hash2(index, 31) - 0.5) * radius * emitterConfig.scatter;
          base[0] = Math.cos(spiralAngle) * radius + scatter;
          base[1] = (hash2(index, 32) - 0.5) * emitterConfig.radius * 0.05;
          base[2] = Math.sin(spiralAngle) * radius + (hash2(index, 33) - 0.5) * radius * emitterConfig.scatter;
          base[3] = 0; base[4] = 0; base[5] = 0;
          break;
        }
        default: { // point
          base[0] = 0;
          base[1] = 0;
          base[2] = 0;
          base[3] = (hash2(index, 0) - 0.5) * 0.1;
          base[4] = (hash2(index, 1) - 0.5) * 0.1;
          base[5] = (hash2(index, 2) - 0.5) * 0.1;
          break;
        }
      }
      base[6] = 0.0;              // age
      base[7] = emitterConfig.lifetime;   // lifetime
    }

    return {
      count: count,
      positions: positions,
      sizes: sizes,
      colors: colors,
      opacities: opacities,
      entry: entry,

      update: function(deltaTime, totalTime) {
        var emitterConfig = currentEmitterConfig();
        var materialConfig = currentMaterialConfig();
        var forces = currentForces();
        var customForceContext = null;
        for (var i = 0; i < count; i++) {
          var base = i * 8;

          // Read particle state.
          var posX = particles[base];
          var posY = particles[base + 1];
          var posZ = particles[base + 2];
          var velX = particles[base + 3];
          var velY = particles[base + 4];
          var velZ = particles[base + 5];
          var age = particles[base + 6];
          var lifetime = particles[base + 7];

          // Initialize on first frame.
          if (age < 0) {
            emitParticle(i, particles.subarray(base, base + 8), emitterConfig);
            posX = particles[base];
            posY = particles[base + 1];
            posZ = particles[base + 2];
            velX = particles[base + 3];
            velY = particles[base + 4];
            velZ = particles[base + 5];
            age = particles[base + 6];
            lifetime = particles[base + 7];
          }

          // Age.
          age += deltaTime;

          // Respawn dead particles.
          if (lifetime > 0 && age >= lifetime) {
            emitParticle(i, particles.subarray(base, base + 8), emitterConfig);
            posX = particles[base];
            posY = particles[base + 1];
            posZ = particles[base + 2];
            velX = particles[base + 3];
            velY = particles[base + 4];
            velZ = particles[base + 5];
            age = particles[base + 6];
            lifetime = particles[base + 7];
          }

          // Apply forces.
          for (var fi = 0; fi < forces.length; fi++) {
            var f = forces[fi];
            var handler = sceneParticleForceHandler(f && f.kind);
            if (handler) {
              if (!customForceContext) {
                customForceContext = {
                  index: 0,
                  deltaTime: 0,
                  totalTime: 0,
                  age: 0,
                  lifetime: 0,
                  force: null,
                  position: { x: 0, y: 0, z: 0 },
                  velocity: { x: 0, y: 0, z: 0 },
                };
              }
              customForceContext.index = i;
              customForceContext.deltaTime = deltaTime;
              customForceContext.totalTime = totalTime;
              customForceContext.age = age;
              customForceContext.lifetime = lifetime;
              customForceContext.force = f;
              customForceContext.position.x = posX;
              customForceContext.position.y = posY;
              customForceContext.position.z = posZ;
              customForceContext.velocity.x = velX;
              customForceContext.velocity.y = velY;
              customForceContext.velocity.z = velZ;
              var customVelocity = sceneApplyParticleForceHandler(handler, customForceContext);
              velX = customVelocity.x;
              velY = customVelocity.y;
              velZ = customVelocity.z;
              continue;
            }
            var fKind = sceneParticleForceKindCode(f && f.kind);
            var str = sceneNumber(f.strength, 0);
            var fx = sceneNumber(f.x, 0);
            var fy = sceneNumber(f.y, 0);
            var fz = sceneNumber(f.z, 0);
            var freq = sceneNumber(f.frequency, 1);

            switch (fKind) {
              case 0: { // gravity
                velX += fx * str * deltaTime;
                velY += fy * str * deltaTime;
                velZ += fz * str * deltaTime;
                break;
              }
              case 1: { // wind
                velX += fx * str * deltaTime;
                velY += fy * str * deltaTime;
                velZ += fz * str * deltaTime;
                break;
              }
              case 2: { // turbulence
                var nx = Math.sin(posX * freq + totalTime * 1.3) * Math.cos(posZ * freq + totalTime * 0.7);
                var ny = Math.sin(posY * freq + totalTime * 0.9) * Math.cos(posX * freq + totalTime * 1.1);
                var nz = Math.sin(posZ * freq + totalTime * 1.7) * Math.cos(posY * freq + totalTime * 0.5);
                velX += nx * str * deltaTime;
                velY += ny * str * deltaTime;
                velZ += nz * str * deltaTime;
                break;
              }
              case 3: { // orbit
                var dx = posX;
                var dz = posZ;
                var dist = Math.max(Math.sqrt(dx * dx + dz * dz), 0.001);
                velX += (-dz / dist) * str * deltaTime;
                velZ += (dx / dist) * str * deltaTime;
                break;
              }
              case 4: { // drag
                velX += -velX * str * deltaTime;
                velY += -velY * str * deltaTime;
                velZ += -velZ * str * deltaTime;
                break;
              }
            }
          }

          // Integrate position.
          posX += velX * deltaTime;
          posY += velY * deltaTime;
          posZ += velZ * deltaTime;

          // Write state back.
          particles[base] = posX;
          particles[base + 1] = posY;
          particles[base + 2] = posZ;
          particles[base + 3] = velX;
          particles[base + 4] = velY;
          particles[base + 5] = velZ;
          particles[base + 6] = age;
          particles[base + 7] = lifetime;

          // Compute interpolation t.
          var t = lifetime > 0 ? age / lifetime : 0;

          // Write render output.
          positions[i * 3] = posX;
          positions[i * 3 + 1] = posY;
          positions[i * 3 + 2] = posZ;
          sizes[i] = materialConfig.sizeStart + (materialConfig.sizeEnd - materialConfig.sizeStart) * t;
          colors[i * 3] = materialConfig.colorStart[0] + (materialConfig.colorEnd[0] - materialConfig.colorStart[0]) * t;
          colors[i * 3 + 1] = materialConfig.colorStart[1] + (materialConfig.colorEnd[1] - materialConfig.colorStart[1]) * t;
          colors[i * 3 + 2] = materialConfig.colorStart[2] + (materialConfig.colorEnd[2] - materialConfig.colorStart[2]) * t;
          opacities[i] = materialConfig.opacityStart + (materialConfig.opacityEnd - materialConfig.opacityStart) * t;
        }
      },

      dispose: function() {
        particles = null;
        positions = null;
        sizes = null;
        colors = null;
        opacities = null;
      },
    };
  }

  // --- Factory ---
  // Creates either a GPU compute system (when WebGPU device is available) or
  // a CPU fallback that feeds into the existing Points/WebGL2 renderer.

  function createSceneParticleSystem(device, entry) {
    if (device) {
      return createSceneComputeParticleSystem(device, entry);
    }
    return createSceneCPUParticleSystem(entry);
  }

  function sceneComputeSystemSignature(entry) {
    return JSON.stringify({
      count: Math.max(0, Math.floor(sceneNumber(entry && entry.count, 0))),
      forces: Array.isArray(entry && entry.forces) ? entry.forces : [],
      forceRegistryVersion: sceneParticleForceRegistryVersion,
    });
  }

  if (typeof window !== "undefined" && window.__gosx_scene3d_api) {
    Object.assign(window.__gosx_scene3d_api, {
      createSceneParticleSystem,
      listSceneParticleForces,
      registerSceneParticleForce,
      registerSceneParticleForceKind,
      sceneComputeSystemSignature,
      unregisterSceneParticleForce,
    });
  }
