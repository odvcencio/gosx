  // SceneIR schema mirror. This is intentionally runtime-light: production
  // render paths can ignore it, while tests and dev tooling can assert the
  // compiler/planner contract before a backend sees a scene.

  const SCENE_IR_VERSION = 1;
  const SCENE_IR_SCHEMA = "gosx.scene3d.ir.v1";
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
   * @property {SceneComputeParticles[]} [computeParticles]
   * @property {SceneWaterSystem[]} [waterSystems]
   * @property {object[]} [html]
   */

  /**
   * @typedef {object} SceneComputeParticles
   * @property {string} id
   * @property {number} count
   * @property {object} emitter
   * @property {object[]} [forces]
   * @property {object} material
   * @property {number} [bounds]
   * @property {string} [computeWGSL] - Optional Elio/custom kernel override WGSL source.
   * @property {string} [computeEntry] - Entry point for computeWGSL (default "simulate").
   * @property {string} [computeBackend] - Kernel authoring back-end name (e.g. "elio").
   * @property {string} [computeWGSLRef] - shaderLib ref replacing computeWGSL when deduplicated.
   */

  /**
   * @typedef {object} SceneWaterSystem
   * @property {string} id
   * @property {number} [resolution]
   * @property {string} [poolShape]
   * @property {number} [poolWidth]
   * @property {number} [poolHeight]
   * @property {number} [poolLength]
   * @property {number} [cornerRadius]
   * @property {number} [waveSpeed]
   * @property {number} [damping]
   * @property {number} [normalScale]
   * @property {number} [seedDrops]
   * @property {number} [dropRadius]
   * @property {number} [dropStrength]
   * @property {number} [dropEventID] - Monotonic one-shot water drop event id.
   * @property {number} [dropX] - Drop center in normalized water coordinates, -1 to 1.
   * @property {number} [dropZ] - Drop center in normalized water coordinates, -1 to 1.
   * @property {number} [dropEventRadius]
   * @property {number} [dropEventStrength]
   * @property {string} [shallowColor]
   * @property {string} [deepColor]
   * @property {boolean} [paused]
   * @property {string} [objectKind] - Displacement primitive kind, such as "sphere" or "cube".
   * @property {number} [objectX]
   * @property {number} [objectY]
   * @property {number} [objectZ]
   * @property {boolean} [objectPreviousSet] - Use objectPreviousX/Y/Z as the prior displacement volume for one transition.
   * @property {number} [objectPreviousX]
   * @property {number} [objectPreviousY]
   * @property {number} [objectPreviousZ]
   * @property {number} [objectRadius]
   * @property {number} [objectHalfSizeX]
   * @property {number} [objectHalfSizeY]
   * @property {number} [objectHalfSizeZ]
   * @property {number} [objectDriftX]
   * @property {number} [objectDriftY]
   * @property {number} [objectDriftZ]
   * @property {number} [objectBobAmplitude]
   * @property {number} [objectBobSpeed]
   * @property {number} [objectDisplacementScale]
   * @property {Array<{offsetX?: number, offsetY?: number, offsetZ?: number, radius: number}>} [objectDisplacementSpheres]
   * @property {Record<string, string>} [computeSourceFiles] - Elio pass name to source path manifest.
   * @property {Record<string, string>} [materialSourceFiles] - Selena pass name to source path manifest.
   * @property {string} [computeBackend] - Kernel authoring back-end name (e.g. "elio").
   * @property {string} [materialBackend] - Material authoring back-end name (e.g. "selena").
   */

  /**
   * @typedef {Object.<string,string>} SceneShaderLib
   * Map of content-hash IDs (e.g. "sl:aabb1122...") to shader source strings.
   * Present at the scene root when large shader strings are deduplicated. The
   * JS hydrate path (inflateManifestShaderLibs) expands refs before renderers
   * see the scene, so downstream code never needs to handle this field.
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
   * @property {string} [shaderBackend]
   * @property {object} [shaderLayout]
   * @property {string} [shaderSource]
   * @property {Object.<string,string>} [shaderSourceFiles]
   */

  /**
   * @typedef {object} SceneNode
   * @property {"mesh"|"points"|"instanced-mesh"|"compute-particles"|"sprite"|"label"|"html"|string} kind
   * @property {string} [id]
   * @property {number} [materialIndex]
   * @property {object} transform
   * @property {object} [mesh]
   * @property {object} [points]
   * @property {object} [instancedMesh]
   * @property {object} [compute]
   * @property {object} [sprite]
   * @property {object} [label]
   * @property {object} [html]
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
    if (ir.schema != null && ir.schema !== SCENE_IR_SCHEMA) {
      errors.push("schema must be " + SCENE_IR_SCHEMA);
    }
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
    validateSceneIRArray(ir, "waterSystems", errors);

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
      "html": "html",
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
        node.html,
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
    validateSceneIRArray(bundle, "waterSystems", errors);
    validateSceneIRArray(bundle, "labels", errors);
    validateSceneIRArray(bundle, "sprites", errors);
    validateSceneIRArray(bundle, "html", errors);
    if (!bundle.objects && !bundle.meshObjects && !bundle.points && !bundle.instancedMeshes && !bundle.computeParticles && !bundle.waterSystems && !bundle.labels && !bundle.sprites && !bundle.html) {
      errors.push("scene IR must include nodes or render-bundle arrays");
    }
  }
