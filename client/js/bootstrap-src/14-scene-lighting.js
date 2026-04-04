  // Scene lighting — CPU-side light contribution calculations (retained for canvas fallback).

  function sceneLightingActive(lights, environment) {
    return Boolean(
      (Array.isArray(lights) && lights.length > 0) ||
      sceneNumber(environment && environment.ambientIntensity, 0) > 0 ||
      sceneNumber(environment && environment.skyIntensity, 0) > 0 ||
      sceneNumber(environment && environment.groundIntensity, 0) > 0
    );
  }

  function sceneEnvironmentLightContribution(baseColor, normal, environment) {
    let lighting = { x: 0, y: 0, z: 0 };
    if (sceneNumber(environment && environment.ambientIntensity, 0) > 0) {
      lighting = sceneAddPoint(lighting, sceneMultiplyPoint(
        baseColor,
        sceneScalePoint(sceneColorPoint(environment && environment.ambientColor, { x: 1, y: 1, z: 1 }), sceneNumber(environment && environment.ambientIntensity, 0)),
      ));
    }
    if (sceneNumber(environment && environment.skyIntensity, 0) > 0 || sceneNumber(environment && environment.groundIntensity, 0) > 0) {
      const hemi = clamp01((normal.y * 0.5) + 0.5);
      const sky = sceneScalePoint(sceneColorPoint(environment && environment.skyColor, { x: 0.88, y: 0.94, z: 1 }), sceneNumber(environment && environment.skyIntensity, 0) * hemi);
      const ground = sceneScalePoint(sceneColorPoint(environment && environment.groundColor, { x: 0.12, y: 0.16, z: 0.22 }), sceneNumber(environment && environment.groundIntensity, 0) * (1 - hemi));
      lighting = sceneAddPoint(lighting, sceneMultiplyPoint(baseColor, sceneAddPoint(sky, ground)));
    }
    return lighting;
  }

  function sceneAmbientLightContribution(baseColor, light) {
    return sceneMultiplyPoint(
      baseColor,
      sceneScalePoint(sceneColorPoint(light && light.color, { x: 1, y: 1, z: 1 }), sceneNumber(light && light.intensity, 0)),
    );
  }

  function sceneDirectionalLightContribution(baseColor, normal, light) {
    const direction = sceneNormalizePoint({
      x: -sceneNumber(light && light.directionX, 0),
      y: -sceneNumber(light && light.directionY, -1),
      z: -sceneNumber(light && light.directionZ, 0),
    });
    const diffuse = clamp01(sceneDotPoint(normal, direction));
    if (diffuse <= 0) {
      return { x: 0, y: 0, z: 0 };
    }
    return sceneMultiplyPoint(
      baseColor,
      sceneScalePoint(sceneColorPoint(light && light.color, { x: 1, y: 1, z: 1 }), sceneNumber(light && light.intensity, 0) * diffuse),
    );
  }

  function scenePointLightContribution(baseColor, worldPoint, normal, light) {
    const offset = {
      x: sceneNumber(light && light.x, 0) - sceneNumber(worldPoint && worldPoint.x, 0),
      y: sceneNumber(light && light.y, 0) - sceneNumber(worldPoint && worldPoint.y, 0),
      z: sceneNumber(light && light.z, 0) - sceneNumber(worldPoint && worldPoint.z, 0),
    };
    const distance = Math.max(0.0001, scenePointLength(offset));
    const diffuse = clamp01(sceneDotPoint(normal, sceneScalePoint(offset, 1 / distance)));
    if (diffuse <= 0) {
      return { x: 0, y: 0, z: 0 };
    }
    const attenuation = scenePointLightAttenuation(light, distance);
    if (attenuation <= 0) {
      return { x: 0, y: 0, z: 0 };
    }
    return sceneMultiplyPoint(
      baseColor,
      sceneScalePoint(sceneColorPoint(light && light.color, { x: 1, y: 1, z: 1 }), sceneNumber(light && light.intensity, 0) * diffuse * attenuation),
    );
  }

  function sceneLightContribution(baseColor, worldPoint, normal, light) {
    switch (light && light.kind) {
      case "ambient":
        return sceneAmbientLightContribution(baseColor, light);
      case "directional":
        return sceneDirectionalLightContribution(baseColor, normal, light);
      case "point":
        return scenePointLightContribution(baseColor, worldPoint, normal, light);
      default:
        return { x: 0, y: 0, z: 0 };
    }
  }

  function sceneLitColorRGBA(material, worldPoint, normal, lights, environment) {
    const base = sceneColorRGBA(material && material.color, [0.55, 0.88, 1, 1]);
    if (!sceneLightingActive(lights, environment)) {
      return base;
    }
    const safeNormal = sceneSafeNormal(normal);
    const baseColor = { x: base[0], y: base[1], z: base[2] };
    const emissive = clamp01(sceneMaterialEmissive(material));
    let lighting = sceneEnvironmentLightContribution(baseColor, safeNormal, environment);
    for (const light of Array.isArray(lights) ? lights : []) {
      lighting = sceneAddPoint(lighting, sceneLightContribution(baseColor, worldPoint, safeNormal, light));
    }
    const exposure = sceneNumber(environment && environment.exposure, 1);
    let lit = sceneAddPoint(
      sceneScalePoint(baseColor, emissive),
      sceneScalePoint(lighting, exposure),
    );
    lit = sceneAddPoint(lit, sceneScalePoint(baseColor, 0.06));
    return [clamp01(lit.x), clamp01(lit.y), clamp01(lit.z), base[3]];
  }

  function sceneObjectWorldNormal(object, point, timeSeconds) {
    return sceneNormalizePoint(sceneRotatePoint(
      sceneObjectLocalNormal(object, point),
      sceneNumber(object && object.rotationX, 0) + sceneNumber(object && object.spinX, 0) * timeSeconds,
      sceneNumber(object && object.rotationY, 0) + sceneNumber(object && object.spinY, 0) * timeSeconds,
      sceneNumber(object && object.rotationZ, 0) + sceneNumber(object && object.spinZ, 0) * timeSeconds,
    ));
  }

  function sceneObjectLocalNormal(object, point) {
    const safePoint = point && typeof point === "object" ? point : { x: 0, y: 0, z: 0 };
    switch (object && object.kind) {
      case "lines":
        return sceneBoxNormal(object, safePoint);
      case "plane":
        return { x: 0, y: 1, z: 0 };
      case "sphere":
        return sceneNormalizePoint(safePoint);
      case "pyramid":
        return scenePyramidNormal(object, safePoint);
      default:
        return sceneBoxNormal(object, safePoint);
    }
  }

  function scenePyramidNormal(object, point) {
    const width = Math.max(sceneNumber(object && object.width, object && object.size) / 2, 0.0001);
    const height = Math.max(sceneNumber(object && object.height, object && object.size) / 2, 0.0001);
    const depth = Math.max(sceneNumber(object && object.depth, object && object.size) / 2, 0.0001);
    return sceneNormalizePoint({
      x: sceneNumber(point && point.x, 0) / width,
      y: (sceneNumber(point && point.y, 0) / height) + 0.35,
      z: sceneNumber(point && point.z, 0) / depth,
    });
  }

  function sceneBoxNormal(object, point) {
    const width = Math.max(sceneNumber(object && object.width, object && object.size) / 2, 0.0001);
    const height = Math.max(sceneNumber(object && object.height, object && object.size) / 2, 0.0001);
    const depth = Math.max(sceneNumber(object && object.depth, object && object.size) / 2, 0.0001);
    const x = sceneNumber(point && point.x, 0);
    const y = sceneNumber(point && point.y, 0);
    const z = sceneNumber(point && point.z, 0);
    const ax = Math.abs(x / width);
    const ay = Math.abs(y / height);
    const az = Math.abs(z / depth);
    if (ax >= ay && ax >= az) {
      return { x: Math.sign(x) || 1, y: 0, z: 0 };
    }
    if (ay >= az) {
      return { x: 0, y: Math.sign(y) || 1, z: 0 };
    }
    return { x: 0, y: 0, z: Math.sign(z) || 1 };
  }

  function scenePointLightAttenuation(light, distance) {
    const range = Math.max(0, sceneNumber(light && light.range, 0));
    const decay = Math.max(0.1, sceneNumber(light && light.decay, 1.35));
    if (range > 0) {
      return Math.pow(clamp01(1 - (distance / range)), decay);
    }
    return 1 / (1 + Math.pow(distance * 0.35, Math.max(decay, 1)));
  }
