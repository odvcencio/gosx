  // SceneIR schema mirror. This is intentionally runtime-light: production
  // render paths can ignore it, while tests and dev tooling can assert the
  // compiler/planner contract before a backend sees a scene.

  const SCENE_IR_VERSION = 1;
  const SCENE_RENDER_BUNDLE_VERSION = 1;

  /**
   * @typedef {object} SceneIR
   * @property {number} version
   * @property {SceneCamera} camera
   * @property {SceneEnvironment} environment
   * @property {SceneMaterial[]} [materials]
   * @property {SceneLight[]} [lights]
   * @property {SceneNode[]} [nodes]
   * @property {ScenePostEffect[]} [postFX]
   */

  /**
   * @typedef {object} SceneRenderBundle
   * @property {number} bundleVersion
   * @property {SceneCamera} camera
   * @property {SceneEnvironment} environment
   * @property {SceneMaterial[]} [materials]
   * @property {object[]} [objects]
   * @property {object[]} [meshObjects]
   * @property {object[]} [points]
   * @property {object[]} [instancedMeshes]
   * @property {object[]} [computeParticles]
   * @property {object[]} [html]
   */

  /**
   * @typedef {object} SceneCamera
   * @property {string} [kind]
   * @property {number} [x]
   * @property {number} [y]
   * @property {number} [z]
   * @property {number} [rotationX]
   * @property {number} [rotationY]
   * @property {number} [rotationZ]
   * @property {number} [fov]
   * @property {number} [near]
   * @property {number} [far]
   */

  /**
   * @typedef {object} SceneEnvironment
   * @property {string} [background]
   * @property {string} [ambientColor]
   * @property {number} [ambientIntensity]
   * @property {string} [skyColor]
   * @property {number} [skyIntensity]
   * @property {string} [groundColor]
   * @property {number} [groundIntensity]
   * @property {string} [envMap]
   * @property {number} [envIntensity]
   * @property {number} [envRotation]
   * @property {number} [exposure]
   * @property {string} [toneMapping]
   * @property {string} [fogColor]
   * @property {number} [fogDensity]
   */

  /**
   * @typedef {object} SceneMaterial
   * @property {string} [name]
   * @property {string} [kind]
   * @property {string} [color]
   * @property {number} [opacity]
   * @property {number} [roughness]
   * @property {number} [metalness]
   */

  /**
   * @typedef {object} SceneNode
   * @property {"mesh"|"points"|"instanced-mesh"|"compute-particles"|"sprite"|"label"|string} kind
   * @property {string} [id]
   * @property {number} [materialIndex]
   * @property {object} transform
   * @property {object} [mesh]
   * @property {object} [points]
   * @property {object} [instancedMesh]
   * @property {object} [compute]
   * @property {object} [sprite]
   * @property {object} [label]
   */

  /**
   * @typedef {object} SceneLight
   * @property {string} kind
   * @property {string} [color]
   * @property {number} [intensity]
   * @property {boolean} [castShadow]
   * @property {number} [shadowBias]
   * @property {number} [shadowSize]
   * @property {number} [shadowCascades]
   * @property {number} [shadowSoftness]
   */

  /**
   * @typedef {object} ScenePostEffect
   * @property {string} kind
   */

  function validateSceneIR(ir) {
    const errors = [];
    if (!ir || typeof ir !== "object") {
      return { valid: false, errors: ["scene IR must be an object"] };
    }
    const isRenderBundle = ir.bundleVersion != null;
    if (isRenderBundle) {
      if (ir.bundleVersion != null && ir.bundleVersion !== SCENE_RENDER_BUNDLE_VERSION) {
        errors.push("bundleVersion must be " + SCENE_RENDER_BUNDLE_VERSION);
      }
      if (ir.version != null && ir.version !== SCENE_IR_VERSION) {
        errors.push("version must be " + SCENE_IR_VERSION);
      }
    } else if (ir.version !== SCENE_IR_VERSION) {
      errors.push("version must be " + SCENE_IR_VERSION);
    }
    if (!ir.camera || typeof ir.camera !== "object") {
      errors.push("camera must be an object");
    } else {
      validateSceneIRCamera(ir.camera, errors);
    }
    if (ir.environment != null && typeof ir.environment !== "object") {
      errors.push("environment must be an object");
    }
    validateSceneIRArray(ir, "materials", errors);
    validateSceneIRArray(ir, "lights", errors);
    validateSceneIRArray(ir, "nodes", errors);
    validateSceneIRArray(ir, "postFX", errors);
    validateSceneIRArray(ir, "postEffects", errors);

    if (isRenderBundle) {
      validateRenderBundleShape(ir, errors);
    } else {
      validateCanonicalSceneNodes(Array.isArray(ir.nodes) ? ir.nodes : [], Array.isArray(ir.materials) ? ir.materials.length : 0, errors);
    }
    return { valid: errors.length === 0, errors };
  }

  function validateSceneIRCamera(camera, errors) {
    const near = sceneNumber(camera.near, 0);
    const far = sceneNumber(camera.far, 0);
    if (near < 0) {
      errors.push("camera.near must be non-negative");
    }
    if (near > 0 && far > 0 && far <= near) {
      errors.push("camera.far must be greater than camera.near");
    }
  }

  function validateSceneIRArray(ir, key, errors) {
    if (ir[key] != null && !Array.isArray(ir[key])) {
      errors.push(key + " must be an array");
    }
  }

  function validateCanonicalSceneNodes(nodes, materialCount, errors) {
    const kindPayloads = {
      "mesh": "mesh",
      "points": "points",
      "instanced-mesh": "instancedMesh",
      "compute-particles": "compute",
      "sprite": "sprite",
      "label": "label",
    };
    for (let index = 0; index < nodes.length; index += 1) {
      const node = nodes[index];
      if (!node || typeof node !== "object") {
        errors.push("nodes[" + index + "] must be an object");
        continue;
      }
      const kind = typeof node.kind === "string" ? node.kind.trim() : "";
      if (!kind) {
        errors.push("nodes[" + index + "].kind is required");
      } else if (!Object.prototype.hasOwnProperty.call(kindPayloads, kind)) {
        errors.push("nodes[" + index + "].kind " + JSON.stringify(kind) + " is unknown");
      }
      if (node.materialIndex != null) {
        const materialIndex = sceneNumber(node.materialIndex, -1);
        if (materialIndex < 0 || Math.floor(materialIndex) !== materialIndex) {
          errors.push("nodes[" + index + "].materialIndex must be a non-negative integer");
        } else if (materialCount > 0 && materialIndex >= materialCount) {
          errors.push("nodes[" + index + "].materialIndex out of range");
        }
      }
      const payloadCount = [
        node.mesh,
        node.points,
        node.instancedMesh,
        node.compute,
        node.sprite,
        node.label,
      ].filter(Boolean).length;
      if (payloadCount !== 1) {
        errors.push("nodes[" + index + "] must set exactly one payload");
      }
      const expectedPayload = kindPayloads[kind];
      if (expectedPayload && !node[expectedPayload]) {
        errors.push("nodes[" + index + "]." + expectedPayload + " is required");
      }
      if (node.points) {
        validateSceneIRNonNegativeInteger(node.points.count, "nodes[" + index + "].points.count", errors);
      }
      if (node.instancedMesh) {
        validateSceneIRNonNegativeInteger(node.instancedMesh.count, "nodes[" + index + "].instancedMesh.count", errors);
      }
      if (node.compute) {
        validateSceneIRNonNegativeInteger(node.compute.count, "nodes[" + index + "].compute.count", errors);
      }
    }
  }

  function validateSceneIRNonNegativeInteger(value, label, errors) {
    if (value == null) {
      return;
    }
    const number = sceneNumber(value, -1);
    if (number < 0 || Math.floor(number) !== number) {
      errors.push(label + " must be non-negative");
    }
  }

  function validateRenderBundleShape(bundle, errors) {
    validateSceneIRArray(bundle, "objects", errors);
    validateSceneIRArray(bundle, "meshObjects", errors);
    validateSceneIRArray(bundle, "points", errors);
    validateSceneIRArray(bundle, "instancedMeshes", errors);
    validateSceneIRArray(bundle, "computeParticles", errors);
    validateSceneIRArray(bundle, "labels", errors);
    validateSceneIRArray(bundle, "sprites", errors);
    validateSceneIRArray(bundle, "html", errors);
    if (!bundle.objects && !bundle.meshObjects && !bundle.points && !bundle.instancedMeshes && !bundle.computeParticles && !bundle.labels && !bundle.sprites && !bundle.html) {
      errors.push("scene IR must include nodes or render-bundle arrays");
    }
  }
