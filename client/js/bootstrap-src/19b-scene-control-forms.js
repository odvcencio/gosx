  const SCENE_MANAGED_FLUID_OBJECT_DEFAULT_PROFILE = {
    hiddenY: 8,
    inactiveY: 8,
    physics: {
      gravityY: -4,
      bounce: 0.7,
      defaultBuoyancyScale: 1.1,
    },
    interaction: {
      pointerDrops: true,
      keyboard: true,
      dropRadius: 0.03,
      dropStrength: 0.01,
      minZoomDistance: 2,
      maxZoomDistance: 10,
    },
    objects: {},
  };

  function sceneManagedControlParseJSON(value) {
    if (typeof value !== "string" || value.trim() === "") return null;
    try {
      return JSON.parse(value);
    } catch (_err) {
      return null;
    }
  }

  function sceneManagedControlData(form) {
    if (!form) return "";
    const inline = form.getAttribute("data-gosx-scene3d-control-data") || "";
    const ref = form.getAttribute("data-gosx-scene3d-control-data-ref") || "";
    const refNode = ref && typeof document !== "undefined" ? document.getElementById(ref) : null;
    return inline || (refNode ? refNode.textContent || "" : "");
  }

  function sceneManagedControlScope(node) {
    if (typeof document === "undefined") return null;
    const selectors = [
      "[data-gosx-scene3d-control-scope]",
      "[data-gosx-scene3d-panel-scope]",
    ];
    if (node && typeof node.closest === "function") {
      for (let i = 0; i < selectors.length; i += 1) {
        const scoped = node.closest(selectors[i]);
        if (scoped) return scoped;
      }
    }
    for (let i = 0; i < selectors.length; i += 1) {
      const scoped = document.querySelector(selectors[i]);
      if (scoped) return scoped;
    }
    return null;
  }

  function sceneManagedControlSetOpen(root, toggle, body, open) {
    const value = open ? "true" : "false";
    if (root) root.setAttribute("data-gosx-scene3d-control-open", value);
    if (toggle) toggle.setAttribute("aria-expanded", value);
    if (body) body.hidden = !open;
  }

  function sceneManagedControlBindDisclosure(form) {
    if (!form || typeof form.querySelector !== "function") return function() {};
    const toggle = form.querySelector("[data-gosx-scene3d-control-toggle]");
    const body = form.querySelector("[data-gosx-scene3d-control-body]");
    if (!toggle) return function() {};
    const prior = form.__gosxScene3DControlDisclosure;
    if (prior && typeof prior.dispose === "function") prior.dispose();
    let open = form.getAttribute("data-gosx-scene3d-control-open") === "true";
    function apply() {
      sceneManagedControlSetOpen(form, toggle, body, open);
    }
    function onClick(event) {
      if (event && typeof event.preventDefault === "function") event.preventDefault();
      open = !open;
      apply();
    }
    toggle.addEventListener("click", onClick);
    apply();
    const binding = {
      dispose: function() {
        toggle.removeEventListener("click", onClick);
        if (form.__gosxScene3DControlDisclosure === binding) {
          delete form.__gosxScene3DControlDisclosure;
        }
      },
    };
    form.__gosxScene3DControlDisclosure = binding;
    return binding.dispose;
  }

  function sceneManagedControlSetPanelOpen(panel, toggle, open) {
    const value = open ? "true" : "false";
    if (panel) panel.setAttribute("data-gosx-scene3d-panel-open", value);
    if (toggle) toggle.setAttribute("aria-expanded", value);
    const scope = (panel && panel.closest && panel.closest("[data-gosx-scene3d-panel-scope]"))
      || (toggle && toggle.closest && toggle.closest("[data-gosx-scene3d-panel-scope]"));
    if (scope) {
      const id = panel && panel.id ? panel.id : "";
      if (open && id) {
        scope.setAttribute("data-gosx-scene3d-active-panel", id);
      } else if (scope.getAttribute("data-gosx-scene3d-active-panel") === id) {
        scope.removeAttribute("data-gosx-scene3d-active-panel");
      }
    }
  }

  function sceneManagedControlBindPanelToggles(root) {
    if (typeof document === "undefined") return function() {};
    const scope = root && typeof root.querySelectorAll === "function" ? root : document;
    const toggles = Array.prototype.slice.call(scope.querySelectorAll("[data-gosx-scene3d-panel-toggle]"));
    const disposers = [];
    toggles.forEach(function(toggle) {
      const target = toggle.getAttribute("data-gosx-scene3d-panel-toggle") || toggle.getAttribute("aria-controls") || "";
      const panel = target ? document.getElementById(target) : null;
      if (!panel) return;
      const prior = toggle.__gosxScene3DPanelToggle;
      if (prior && typeof prior.dispose === "function") prior.dispose();
      let open = panel.getAttribute("data-gosx-scene3d-panel-open") === "true";
      function apply() {
        sceneManagedControlSetPanelOpen(panel, toggle, open);
      }
      function onClick(event) {
        if (event && typeof event.preventDefault === "function") event.preventDefault();
        open = !open;
        apply();
      }
      toggle.addEventListener("click", onClick);
      apply();
      const binding = {
        dispose: function() {
          toggle.removeEventListener("click", onClick);
          if (toggle.__gosxScene3DPanelToggle === binding) {
            delete toggle.__gosxScene3DPanelToggle;
          }
        },
      };
      toggle.__gosxScene3DPanelToggle = binding;
      disposers.push(binding.dispose);
    });
    return function() {
      disposers.forEach(function(dispose) { dispose(); });
    };
  }

  function sceneManagedFluidObjectProfile(form) {
    const raw = sceneManagedControlData(form);
    const prior = form && form.__gosxScene3DFluidObjectProfile;
    if (prior && prior.raw === raw) return prior.profile;
    const parsed = sceneManagedControlParseJSON(raw);
    const profile = Object.assign({}, SCENE_MANAGED_FLUID_OBJECT_DEFAULT_PROFILE, parsed && typeof parsed === "object" ? parsed : {});
    profile.physics = Object.assign({}, SCENE_MANAGED_FLUID_OBJECT_DEFAULT_PROFILE.physics, parsed && parsed.physics && typeof parsed.physics === "object" ? parsed.physics : {});
    profile.interaction = Object.assign({}, SCENE_MANAGED_FLUID_OBJECT_DEFAULT_PROFILE.interaction, parsed && parsed.interaction && typeof parsed.interaction === "object" ? parsed.interaction : {});
    profile.objects = parsed && parsed.objects && typeof parsed.objects === "object" ? parsed.objects : {};
    if (form) {
      form.__gosxScene3DFluidObjectProfile = { raw, profile };
    }
    return profile;
  }

  function sceneManagedFluidObjectObjects(profile) {
    return profile && profile.objects && typeof profile.objects === "object" ? profile.objects : {};
  }

  function sceneManagedFluidObjectHiddenY(profile) {
    return sceneNumber(profile && profile.hiddenY, SCENE_MANAGED_FLUID_OBJECT_DEFAULT_PROFILE.hiddenY);
  }

  function sceneManagedFluidObjectInactiveY(profile) {
    return sceneNumber(profile && Object.prototype.hasOwnProperty.call(profile, "inactiveY") ? profile.inactiveY : null, sceneManagedFluidObjectHiddenY(profile));
  }

  function sceneManagedFluidObjectInteractionSettings(profile) {
    const interaction = profile && profile.interaction && typeof profile.interaction === "object" ? profile.interaction : {};
    return {
      pointerDrops: interaction.pointerDrops !== false,
      keyboard: interaction.keyboard !== false,
      dropRadius: Math.max(0.0001, sceneNumber(interaction.dropRadius, SCENE_MANAGED_FLUID_OBJECT_DEFAULT_PROFILE.interaction.dropRadius)),
      dropStrength: sceneNumber(interaction.dropStrength, SCENE_MANAGED_FLUID_OBJECT_DEFAULT_PROFILE.interaction.dropStrength),
      minZoomDistance: Math.max(0.001, sceneNumber(interaction.minZoomDistance, SCENE_MANAGED_FLUID_OBJECT_DEFAULT_PROFILE.interaction.minZoomDistance)),
      maxZoomDistance: Math.max(0.001, sceneNumber(interaction.maxZoomDistance, SCENE_MANAGED_FLUID_OBJECT_DEFAULT_PROFILE.interaction.maxZoomDistance)),
    };
  }

  function sceneManagedFluidObjectClamp(value, min, max) {
    return Math.max(min, Math.min(max, value));
  }

  const SCENE_MANAGED_FLUID_OBJECT_MIN_STRAIGHT_POOL_EDGE = 0.05;

  function sceneManagedFluidObjectMaxCornerRadius(poolWidth, poolLength) {
    return Math.max(0, Math.min(
      sceneManagedFluidObjectClamp(sceneNumber(poolWidth, 1), 0.5, 3),
      sceneManagedFluidObjectClamp(sceneNumber(poolLength, 1), 0.5, 3)
    ) - SCENE_MANAGED_FLUID_OBJECT_MIN_STRAIGHT_POOL_EDGE);
  }

  function sceneManagedFluidObjectClampCornerRadius(radius, poolWidth, poolLength) {
    return sceneManagedFluidObjectClamp(
      sceneNumber(radius, 0.1),
      0,
      sceneManagedFluidObjectMaxCornerRadius(poolWidth, poolLength)
    );
  }

  function sceneManagedFluidObjectClone(value) {
    if (value == null) return value;
    try {
      return JSON.parse(JSON.stringify(value));
    } catch (_err) {
      return value;
    }
  }

  function sceneManagedFluidObjectField(form, name) {
    return form && form.elements && form.elements[name] ? form.elements[name] : null;
  }

  function sceneManagedFluidObjectSetDisabled(form, name, disabled) {
    const field = sceneManagedFluidObjectField(form, name);
    if (!field) return false;
    const next = !!disabled;
    if (field.disabled === next) return false;
    field.disabled = next;
    return true;
  }

  function sceneManagedFluidObjectRequestFrame(callback) {
    if (typeof window !== "undefined" && typeof window.requestAnimationFrame === "function") {
      return window.requestAnimationFrame(callback);
    }
    return setTimeout(callback, 16);
  }

  function sceneManagedFluidObjectCancelFrame(id) {
    if (!id) return;
    if (typeof window !== "undefined" && typeof window.cancelAnimationFrame === "function") {
      window.cancelAnimationFrame(id);
      return;
    }
    clearTimeout(id);
  }

  function sceneManagedFluidObjectNowSeconds() {
    if (typeof performance !== "undefined" && typeof performance.now === "function") {
      return performance.now() / 1000;
    }
    return Date.now() / 1000;
  }

  function sceneManagedFluidObjectReadControls(form) {
    const objectField = sceneManagedFluidObjectField(form, "object");
    const poolShapeField = sceneManagedFluidObjectField(form, "poolShape");
    const poolWidth = sceneManagedFluidObjectClamp(sceneNumber(sceneManagedFluidObjectField(form, "poolWidth") && sceneManagedFluidObjectField(form, "poolWidth").value, 1), 0.5, 3);
    const poolHeight = sceneManagedFluidObjectClamp(sceneNumber(sceneManagedFluidObjectField(form, "poolHeight") && sceneManagedFluidObjectField(form, "poolHeight").value, 1), 0.3, 2);
    const poolLength = sceneManagedFluidObjectClamp(sceneNumber(sceneManagedFluidObjectField(form, "poolLength") && sceneManagedFluidObjectField(form, "poolLength").value, 1), 0.5, 3);
    return {
      paused: !!(sceneManagedFluidObjectField(form, "paused") && sceneManagedFluidObjectField(form, "paused").checked),
      object: objectField && objectField.value ? objectField.value : "Sphere",
      gravity: !!(sceneManagedFluidObjectField(form, "gravity") && sceneManagedFluidObjectField(form, "gravity").checked),
      densityEnabled: !!(sceneManagedFluidObjectField(form, "densityEnabled") && sceneManagedFluidObjectField(form, "densityEnabled").checked),
      density: sceneManagedFluidObjectClamp(sceneNumber(sceneManagedFluidObjectField(form, "density") && sceneManagedFluidObjectField(form, "density").value, 0.9), 0.2, 2),
      poolShape: poolShapeField && poolShapeField.value ? poolShapeField.value : "Box",
      cornerRadius: sceneManagedFluidObjectClampCornerRadius(sceneManagedFluidObjectField(form, "cornerRadius") && sceneManagedFluidObjectField(form, "cornerRadius").value, poolWidth, poolLength),
      poolWidth,
      poolHeight,
      poolLength,
      followCamera: !!(sceneManagedFluidObjectField(form, "followCamera") && sceneManagedFluidObjectField(form, "followCamera").checked),
    };
  }

  function sceneManagedFluidObjectEffectivePoolControls(controls) {
    const rounded = controls && controls.poolShape === "Rounded Box";
    const poolWidth = rounded ? sceneManagedFluidObjectClamp(sceneNumber(controls && controls.poolWidth, 1), 0.5, 3) : 1;
    const poolHeight = rounded ? sceneManagedFluidObjectClamp(sceneNumber(controls && controls.poolHeight, 1), 0.3, 2) : 1;
    const poolLength = rounded ? sceneManagedFluidObjectClamp(sceneNumber(controls && controls.poolLength, 1), 0.5, 3) : 1;
    return {
      poolShape: rounded ? "Rounded Box" : "Box",
      poolWidth,
      poolHeight,
      poolLength,
      cornerRadius: rounded ? sceneManagedFluidObjectClampCornerRadius(controls && controls.cornerRadius, poolWidth, poolLength) : 0,
    };
  }

  function sceneManagedFluidObjectPoolKey(controls) {
    const pool = sceneManagedFluidObjectEffectivePoolControls(controls);
    return [
      pool.poolShape,
      sceneNumber(pool.poolWidth, 1).toFixed(4),
      sceneNumber(pool.poolHeight, 1).toFixed(4),
      sceneNumber(pool.poolLength, 1).toFixed(4),
      sceneNumber(pool.cornerRadius, 0).toFixed(4),
    ].join("|");
  }

  function sceneManagedFluidObjectControlState(form) {
    if (!form) return null;
    if (!form.__gosxScene3DFluidObjectState) {
      form.__gosxScene3DFluidObjectState = {
        initialized: false,
        object: "",
        previousObject: "",
        transitionPending: false,
        poolKey: "",
        sharedPosition: null,
        lastStepTime: 0,
        dropEventID: 0,
        dropEvent: null,
        objectEventID: 0,
        objectDisplacementEvents: [],
        settingLightDirection: false,
        lightDirection: null,
        lightCameraKey: "",
        pointerMode: "",
        pointerDrag: null,
        objects: {},
      };
    }
    return form.__gosxScene3DFluidObjectState;
  }

  function sceneManagedFluidObjectConfigPosition(config) {
    return {
      x: sceneNumber(config && config.objectX, 0),
      y: sceneNumber(config && config.objectY, 0),
      z: sceneNumber(config && config.objectZ, 0),
    };
  }

  function sceneManagedFluidObjectCopyVec3(value) {
    return {
      x: sceneNumber(value && value.x, 0),
      y: sceneNumber(value && value.y, 0),
      z: sceneNumber(value && value.z, 0),
    };
  }

  function sceneManagedFluidObjectObjectKey(config) {
    return config && (config.label || config.id || config.objectKind) ? String(config.label || config.id || config.objectKind) : "";
  }

  function sceneManagedFluidObjectObjectState(state, config) {
    if (!state || !config) return null;
    const key = sceneManagedFluidObjectObjectKey(config);
    if (!key) return null;
    if (!state.objects[key]) {
      const position = sceneManagedFluidObjectConfigPosition(config);
      state.objects[key] = {
        position,
        previousPosition: sceneManagedFluidObjectCopyVec3(position),
        velocity: { x: 0, y: 0, z: 0 },
        pendingPrevious: null,
      };
    }
    return state.objects[key];
  }

  function sceneManagedFluidObjectObjectMetrics(config) {
    const radius = Math.max(0, sceneNumber(config && config.objectRadius, 0));
    const halfX = Math.max(0, sceneNumber(config && config.objectHalfSizeX, 0));
    const halfY = Math.max(0, sceneNumber(config && config.objectHalfSizeY, 0));
    const halfZ = Math.max(0, sceneNumber(config && config.objectHalfSizeZ, 0));
    return {
      buoyancyRadius: Math.max(0.0001, sceneNumber(config && config.buoyancyRadius, halfY || radius || 0.25)),
      floorClearance: Math.max(0, sceneNumber(config && config.floorClearance, halfY || radius || 0.25)),
      xLimitRadius: Math.max(0, sceneNumber(config && config.xLimitRadius, halfX || radius || 0.25)),
      zLimitRadius: Math.max(0, sceneNumber(config && config.zLimitRadius, halfZ || radius || 0.25)),
      meshYOffset: sceneNumber(config && config.meshYOffset, sceneNumber(config && config.mesh && config.mesh.y, 0) - sceneNumber(config && config.objectY, 0)),
    };
  }

  function sceneManagedFluidObjectPhysicsSettings(profile) {
    const settings = profile && profile.physics && typeof profile.physics === "object" ? profile.physics : {};
    const defaults = SCENE_MANAGED_FLUID_OBJECT_DEFAULT_PROFILE.physics;
    return {
      gravityY: sceneNumber(settings.gravityY, defaults.gravityY),
      bounce: sceneManagedFluidObjectClamp(sceneNumber(settings.bounce, defaults.bounce), 0, 1),
      defaultBuoyancyScale: Math.max(0, sceneNumber(settings.defaultBuoyancyScale, defaults.defaultBuoyancyScale)),
    };
  }

  function sceneManagedFluidObjectClampObjectPosition(position, config, controls) {
    const metrics = sceneManagedFluidObjectObjectMetrics(config);
    const pool = sceneManagedFluidObjectEffectivePoolControls(controls);
    const limitX = Math.max(0, pool.poolWidth - metrics.xLimitRadius);
    const limitZ = Math.max(0, pool.poolLength - metrics.zLimitRadius);
    position.x = sceneManagedFluidObjectClamp(sceneNumber(position.x, 0), -limitX, limitX);
    position.y = sceneManagedFluidObjectClamp(sceneNumber(position.y, 0), metrics.floorClearance - pool.poolHeight, 10);
    position.z = sceneManagedFluidObjectClamp(sceneNumber(position.z, 0), -limitZ, limitZ);
    return position;
  }

  function sceneManagedFluidObjectStepPhysics(objectState, config, controls, profile, seconds) {
    if (!objectState || !config || !controls.gravity || controls.paused || seconds <= 0) return;
    const metrics = sceneManagedFluidObjectObjectMetrics(config);
    const settings = sceneManagedFluidObjectPhysicsSettings(profile);
    const radius = metrics.buoyancyRadius;
    const position = objectState.position;
    const velocity = objectState.velocity;
    const density = Math.max(0.0001, sceneNumber(controls.density, 0.9));
    const buoyancyScale = controls.densityEnabled ? 1 / density : settings.defaultBuoyancyScale;
    const percentUnderWater = sceneManagedFluidObjectClamp((radius - position.y) / (2 * radius), 0, 1);
    velocity.y += settings.gravityY * (seconds - buoyancyScale * seconds * percentUnderWater);
    const speedSq = velocity.x * velocity.x + velocity.y * velocity.y + velocity.z * velocity.z;
    if (speedSq > 0) {
      const speed = Math.sqrt(speedSq);
      const drag = -percentUnderWater * seconds * speedSq;
      velocity.x += velocity.x / speed * drag;
      velocity.y += velocity.y / speed * drag;
      velocity.z += velocity.z / speed * drag;
    }
    position.x += velocity.x * seconds;
    position.y += velocity.y * seconds;
    position.z += velocity.z * seconds;
    const pool = sceneManagedFluidObjectEffectivePoolControls(controls);
    const floor = metrics.floorClearance - pool.poolHeight;
    if (position.y < floor) {
      position.y = floor;
      velocity.y = Math.abs(velocity.y) * settings.bounce;
    }
    sceneManagedFluidObjectClampObjectPosition(position, config, controls);
  }

  function sceneManagedFluidObjectQueueObjectDisplacementEvent(state, config, previous, position) {
    if (!state || !config || !previous || !position) return null;
    state.objectEventID = Math.max(0, Math.floor(sceneNumber(state.objectEventID, 0))) + 1;
    const event = {
      id: state.objectEventID,
      activeObject: config.label || config.id || config.objectKind || "",
      objectKind: config.objectKind || "",
      objectSubtype: config.objectSubtype || "",
      objectX: position.x,
      objectY: position.y,
      objectZ: position.z,
      objectPreviousSet: true,
      objectPreviousX: previous.x,
      objectPreviousY: previous.y,
      objectPreviousZ: previous.z,
      objectRadius: sceneNumber(config.objectRadius, 0),
      objectHalfSizeX: sceneNumber(config.objectHalfSizeX, 0),
      objectHalfSizeY: sceneNumber(config.objectHalfSizeY, 0),
      objectHalfSizeZ: sceneNumber(config.objectHalfSizeZ, 0),
      objectDisplacementScale: sceneNumber(config.objectDisplacementScale, 1),
      objectDisplacementSpheres: Array.isArray(config.objectDisplacementSpheres) ? config.objectDisplacementSpheres.map(sceneManagedFluidObjectClone) : [],
    };
    if (!Array.isArray(state.objectDisplacementEvents)) state.objectDisplacementEvents = [];
    state.objectDisplacementEvents.push(event);
    if (state.objectDisplacementEvents.length > 12) {
      state.objectDisplacementEvents = state.objectDisplacementEvents.slice(state.objectDisplacementEvents.length - 12);
    }
    return event;
  }

  function sceneManagedFluidObjectObserveSelection(state, controls, profile) {
    if (!state || !controls) return;
    const objects = sceneManagedFluidObjectObjects(profile);
    const selected = controls.object || "None";
    if (!state.initialized) {
      state.initialized = true;
      state.object = selected;
      state.previousObject = selected;
      state.transitionPending = false;
      const initialConfig = selected === "None" ? null : objects[selected] || objects.Sphere || null;
      const initialState = sceneManagedFluidObjectObjectState(state, initialConfig);
      state.sharedPosition = initialState ? sceneManagedFluidObjectCopyVec3(initialState.position) : null;
      return;
    }
    if (selected !== state.object) {
      const previousConfig = state.object === "None" ? null : objects[state.object] || null;
      const previousState = sceneManagedFluidObjectObjectState(state, previousConfig);
      if (previousState) {
        state.sharedPosition = sceneManagedFluidObjectCopyVec3(previousState.position);
        previousState.velocity = { x: 0, y: 0, z: 0 };
        sceneManagedFluidObjectQueueObjectDisplacementEvent(state, previousConfig, state.sharedPosition, {
          x: state.sharedPosition.x,
          y: sceneManagedFluidObjectInactiveY(profile),
          z: state.sharedPosition.z,
        });
      }
      state.previousObject = state.object;
      state.object = selected;
      state.transitionPending = true;
      const nextConfig = selected === "None" ? null : objects[selected] || objects.Sphere || null;
      const nextState = sceneManagedFluidObjectObjectState(state, nextConfig);
      if (nextState) {
        nextState.position = sceneManagedFluidObjectClampObjectPosition(sceneManagedFluidObjectCopyVec3(state.sharedPosition || sceneManagedFluidObjectConfigPosition(nextConfig)), nextConfig, controls);
        nextState.pendingPrevious = {
          x: nextState.position.x,
          y: sceneManagedFluidObjectInactiveY(profile),
          z: nextState.position.z,
        };
      }
    }
  }

  function sceneManagedFluidObjectObjectStep(state, controls, config, profile, options) {
    if (!state || !config) return null;
    const objectState = sceneManagedFluidObjectObjectState(state, config);
    if (!objectState) return null;
    const stepOptions = options && typeof options === "object" ? options : {};
    const now = sceneManagedFluidObjectNowSeconds();
    let seconds = state.lastStepTime > 0 ? now - state.lastStepTime : 0;
    state.lastStepTime = now;
    if (!Number.isFinite(seconds) || seconds < 0) seconds = 0;
    if (seconds > 1) seconds = 0;
    seconds = Math.min(seconds, 0.05);
    const hadPendingPrevious = !!objectState.pendingPrevious;
    const previous = hadPendingPrevious
      ? sceneManagedFluidObjectCopyVec3(objectState.pendingPrevious)
      : sceneManagedFluidObjectCopyVec3(objectState.position);
    objectState.pendingPrevious = null;
    const syncPoolPrevious = !!(stepOptions.poolChanged && !state.transitionPending && !hadPendingPrevious);
    if (syncPoolPrevious) {
      const metrics = sceneManagedFluidObjectObjectMetrics(config);
      const pool = sceneManagedFluidObjectEffectivePoolControls(controls);
      const floor = metrics.floorClearance - pool.poolHeight;
      if (objectState.position.y < floor) {
        objectState.position.y = floor;
        objectState.velocity.y = 0;
      }
    } else {
      sceneManagedFluidObjectStepPhysics(objectState, config, controls, profile, seconds);
    }
    const position = sceneManagedFluidObjectClampObjectPosition(sceneManagedFluidObjectCopyVec3(objectState.position), config, controls);
    objectState.position = position;
    if (syncPoolPrevious) objectState.previousPosition = sceneManagedFluidObjectCopyVec3(position);
    state.sharedPosition = sceneManagedFluidObjectCopyVec3(position);
    const moved = Math.abs(position.x - previous.x) > 0.00001
      || Math.abs(position.y - previous.y) > 0.00001
      || Math.abs(position.z - previous.z) > 0.00001;
    return {
      position,
      previous,
      previousSet: syncPoolPrevious ? false : (moved || !!state.transitionPending),
      metrics: sceneManagedFluidObjectObjectMetrics(config),
    };
  }

  function sceneManagedFluidObjectPushPatch(commands, id, data) {
    commands.push({
      kind: SCENE_CMD_SET_TRANSFORM,
      objectId: id,
      data,
    });
  }

  function sceneManagedFluidObjectVisibleMeshPatch(config, controls, objectStep) {
    const patch = Object.assign({}, config.mesh);
    patch.visible = true;
    if (objectStep && objectStep.position) {
      patch.x = objectStep.position.x;
      patch.y = objectStep.position.y + objectStep.metrics.meshYOffset;
      patch.z = objectStep.position.z;
      patch.driftX = 0;
      patch.driftY = 0;
      patch.driftZ = 0;
      patch.bobAmplitude = 0;
    }
    return patch;
  }

  function sceneManagedFluidObjectPatchObjects(commands, activeConfig, controls, profile, objectStep) {
    const objects = sceneManagedFluidObjectObjects(profile);
    const hiddenY = sceneManagedFluidObjectHiddenY(profile);
    Object.keys(objects).forEach(function(name) {
      const config = objects[name];
      if (config.model) return;
      if (activeConfig && config.id === activeConfig.id) {
        sceneManagedFluidObjectPushPatch(commands, config.id, sceneManagedFluidObjectVisibleMeshPatch(config, controls, objectStep));
      } else {
        sceneManagedFluidObjectPushPatch(commands, config.id, {
          y: hiddenY,
          visible: false,
          driftX: 0,
          driftY: 0,
          driftZ: 0,
          bobAmplitude: 0,
        });
      }
    });
  }

  function sceneManagedFluidObjectPatchModels(commands, state, activeConfig, controls, profile, objectStep) {
    let models = Array.isArray(state.models) ? state.models.map(sceneManagedFluidObjectClone) : [];
    const objects = sceneManagedFluidObjectObjects(profile);
    const hiddenY = sceneManagedFluidObjectHiddenY(profile);
    const modelConfigs = Object.keys(objects).map(function(name) { return objects[name]; }).filter(function(config) {
      return !!(config && config.model && config.id);
    });
    const modelIDs = new Set(modelConfigs.map(function(config) { return config.id; }));
    const activeModel = activeConfig && activeConfig.model ? activeConfig : null;
    let changed = false;
    let found = false;
    models = models.map(function(model) {
      if (!model || !modelIDs.has(model.id)) return model;
      const active = !!(activeModel && model.id === activeModel.id);
      if (active) {
        found = true;
        changed = true;
        return Object.assign({}, model, sceneManagedFluidObjectVisibleMeshPatch(activeModel, controls, objectStep));
      }
      if (sceneNumber(model.y, hiddenY) !== hiddenY) {
        changed = true;
      }
      return Object.assign({}, model, { y: hiddenY, visible: false });
    });
    if (!found && activeModel) {
      changed = true;
      models.push(Object.assign({
        id: activeModel.id,
        src: activeModel.src,
      }, sceneManagedFluidObjectVisibleMeshPatch(activeModel, controls, objectStep)));
    }
    if (changed) {
      commands.push({
        kind: SCENE_CMD_SET_MODELS,
        data: { models },
      });
    }
  }

  function sceneManagedFluidObjectBuildWater(current, controls, config, waterID, profile, controlState, objectStep) {
    const next = Object.assign({}, current || {});
    const dropEvent = controlState && controlState.dropEvent ? controlState.dropEvent : null;
    const pool = sceneManagedFluidObjectEffectivePoolControls(controls);
    next.id = next.id || waterID || "water-main";
    next.resolution = next.resolution || 256;
    next.poolShape = pool.poolShape;
    next.poolWidth = pool.poolWidth;
    next.poolHeight = pool.poolHeight;
    next.poolLength = pool.poolLength;
    next.cornerRadius = pool.cornerRadius;
    next.paused = controls.paused;
    next.followCamera = controls.followCamera;
    next.computeBackend = next.computeBackend || "elio";
    next.materialBackend = next.materialBackend || "selena";
    const lightDirection = controlState && controlState.lightDirection ? controlState.lightDirection : sceneManagedFluidObjectDefaultLightDirection();
    next.lightDirectionX = sceneNumber(lightDirection.x, 2);
    next.lightDirectionY = sceneNumber(lightDirection.y, 2);
    next.lightDirectionZ = sceneNumber(lightDirection.z, -1);
    if (dropEvent && dropEvent.id > 0) {
      next.dropEventID = dropEvent.id;
      next.dropX = dropEvent.x;
      next.dropZ = dropEvent.z;
      next.dropEventRadius = dropEvent.radius;
      next.dropEventStrength = dropEvent.strength;
    }
    const objectEvents = controlState && Array.isArray(controlState.objectDisplacementEvents) ? controlState.objectDisplacementEvents : [];
    if (objectEvents.length > 0) {
      next.objectDisplacementEvents = objectEvents.map(sceneManagedFluidObjectClone);
    } else if (Object.prototype.hasOwnProperty.call(next, "objectDisplacementEvents")) {
      delete next.objectDisplacementEvents;
    }
    if (!config) {
      Object.assign(next, {
        activeObject: "None",
        objectKind: "none",
        objectSubtype: "",
        objectX: 0,
        objectY: 0,
        objectZ: 0,
        objectPreviousSet: false,
        objectPreviousX: 0,
        objectPreviousY: 0,
        objectPreviousZ: 0,
        objectRadius: 0,
        objectHalfSizeX: 0,
        objectHalfSizeY: 0,
        objectHalfSizeZ: 0,
        objectDriftX: 0,
        objectDriftY: 0,
        objectDriftZ: 0,
        objectBobAmplitude: 0,
        objectBobSpeed: 0,
        objectDisplacementScale: 0,
        objectDisplacementSpheres: [],
      });
      return next;
    }
    next.activeObject = config.label;
    next.objectKind = config.objectKind;
    next.objectSubtype = config.objectSubtype || "";
    const position = objectStep && objectStep.position ? objectStep.position : sceneManagedFluidObjectConfigPosition(config);
    const previous = objectStep && objectStep.previous ? objectStep.previous : null;
    next.objectX = position.x;
    next.objectY = position.y;
    next.objectZ = position.z;
    next.objectPreviousSet = !!(objectStep && objectStep.previousSet);
    next.objectPreviousX = next.objectPreviousSet && previous ? previous.x : 0;
    next.objectPreviousY = next.objectPreviousSet && previous ? previous.y : 0;
    next.objectPreviousZ = next.objectPreviousSet && previous ? previous.z : 0;
    next.objectRadius = config.objectRadius;
    next.objectHalfSizeX = config.objectHalfSizeX;
    next.objectHalfSizeY = config.objectHalfSizeY;
    next.objectHalfSizeZ = config.objectHalfSizeZ;
    next.objectDriftX = 0;
    next.objectDriftY = 0;
    next.objectDriftZ = 0;
    next.objectBobAmplitude = 0;
    next.objectBobSpeed = 0;
    next.objectDisplacementScale = sceneNumber(config.objectDisplacementScale, 1);
    next.objectDisplacementSpheres = Array.isArray(config.objectDisplacementSpheres) ? config.objectDisplacementSpheres.map(sceneManagedFluidObjectClone) : [];
    return next;
  }

  function sceneManagedFluidObjectSystemPayload(state, controls, config, waterID, profile, controlState, objectStep) {
    const systems = Array.isArray(state && state.waterSystems) ? state.waterSystems : [];
    const selected = sceneManagedFluidObjectWaterSystemByID(systems, waterID);
    const water = selected ? sceneManagedFluidObjectClone(selected) : {};
    return {
      waterSystems: [sceneManagedFluidObjectBuildWater(water, controls, config, waterID, profile, controlState, objectStep)],
    };
  }

  function sceneManagedFluidObjectReflectForm(form, controls, ready, profile, sceneState) {
    const status = form.querySelector("[data-gosx-scene3d-control-status]");
    const active = controls.object === "None" ? "None" : controls.object;
    const physicsAvailable = active !== "None";
    const rounded = controls.poolShape === "Rounded Box";
    const pool = sceneManagedFluidObjectEffectivePoolControls(controls);
    const maxCornerRadius = sceneManagedFluidObjectMaxCornerRadius(pool.poolWidth, pool.poolLength);
    const interactionProfile = sceneManagedFluidObjectInteractionProfile(form, sceneState, profile);
    sceneManagedFluidObjectSetDisabled(form, "gravity", !physicsAvailable);
    sceneManagedFluidObjectSetDisabled(form, "densityEnabled", !physicsAvailable);
    sceneManagedFluidObjectSetDisabled(form, "density", !physicsAvailable || !controls.densityEnabled);
    const cornerField = sceneManagedFluidObjectField(form, "cornerRadius");
    if (cornerField) {
      cornerField.max = String(maxCornerRadius);
      if (rounded && sceneNumber(cornerField.value, 0) !== pool.cornerRadius) {
        cornerField.value = String(pool.cornerRadius);
      }
    }
    form.dataset.physicsAvailable = String(physicsAvailable);
    form.dataset.densityOpen = String(physicsAvailable && controls.densityEnabled);
    form.dataset.roundedOpen = String(rounded);
    form.dataset.gosxScene3dInteractionProfile = interactionProfile;
    form.setAttribute("data-gosx-scene3d-interaction-profile", interactionProfile);
    form.dataset.cornerRadius = String(rounded ? pool.cornerRadius : 0);
    form.dataset.maxCornerRadius = String(rounded ? maxCornerRadius : 0);
    form.dataset.fluidObject = active;
    form.dataset.gosxScene3dControlsReady = ready ? "true" : "false";
    form.dataset.gosxScene3dFluidObjectControlsReady = ready ? "true" : "false";
    if (status) {
      status.value = active + (controls.paused ? " paused" : "");
      status.textContent = status.value;
    }
    const root = sceneManagedControlScope(form);
    if (root) {
      root.setAttribute("data-gosx-scene3d-controls-ready", ready ? "true" : "false");
      root.setAttribute("data-gosx-scene3d-fluid-object-controls-ready", ready ? "true" : "false");
      root.setAttribute("data-gosx-scene3d-interaction-profile", interactionProfile);
    }
  }

  function sceneManagedFluidObjectReflectObjectState(form, controlState, config, objectStep) {
    if (!form) return;
    const position = objectStep && objectStep.position ? objectStep.position : (config ? sceneManagedFluidObjectConfigPosition(config) : null);
    const previous = objectStep && objectStep.previous ? objectStep.previous : null;
    const root = sceneManagedControlScope(form);
    const targets = root && root !== form ? [form, root] : [form];
    targets.forEach(function(target) {
      if (!target || typeof target.setAttribute !== "function") return;
      target.setAttribute("data-gosx-scene3d-fluid-object-active", config ? String(config.label || config.id || config.objectKind || "") : "None");
      target.setAttribute("data-gosx-scene3d-fluid-pointer-mode", String(controlState && controlState.pointerMode || ""));
      if (position) {
        target.setAttribute("data-gosx-scene3d-fluid-object-x", String(sceneNumber(position.x, 0)));
        target.setAttribute("data-gosx-scene3d-fluid-object-y", String(sceneNumber(position.y, 0)));
        target.setAttribute("data-gosx-scene3d-fluid-object-z", String(sceneNumber(position.z, 0)));
      } else {
        target.setAttribute("data-gosx-scene3d-fluid-object-x", "0");
        target.setAttribute("data-gosx-scene3d-fluid-object-y", "0");
        target.setAttribute("data-gosx-scene3d-fluid-object-z", "0");
      }
      if (previous) {
        target.setAttribute("data-gosx-scene3d-fluid-object-previous-x", String(sceneNumber(previous.x, 0)));
        target.setAttribute("data-gosx-scene3d-fluid-object-previous-y", String(sceneNumber(previous.y, 0)));
        target.setAttribute("data-gosx-scene3d-fluid-object-previous-z", String(sceneNumber(previous.z, 0)));
      } else {
        target.setAttribute("data-gosx-scene3d-fluid-object-previous-x", "0");
        target.setAttribute("data-gosx-scene3d-fluid-object-previous-y", "0");
        target.setAttribute("data-gosx-scene3d-fluid-object-previous-z", "0");
      }
    });
  }

  function sceneManagedFluidObjectReflectPointerMode(form, controlState) {
    if (!form) return;
    const value = String(controlState && controlState.pointerMode || "");
    form.setAttribute("data-gosx-scene3d-fluid-pointer-mode", value);
    const root = sceneManagedControlScope(form);
    if (root && root !== form && typeof root.setAttribute === "function") {
      root.setAttribute("data-gosx-scene3d-fluid-pointer-mode", value);
    }
  }

  function sceneManagedFluidObjectReflectLightDirection(form, controlState) {
    if (!form) return;
    const light = controlState && controlState.lightDirection
      ? controlState.lightDirection
      : sceneManagedFluidObjectDefaultLightDirection();
    const root = sceneManagedControlScope(form);
    const targets = root && root !== form ? [form, root] : [form];
    targets.forEach(function(target) {
      if (!target || typeof target.setAttribute !== "function") return;
      target.setAttribute("data-gosx-scene3d-fluid-light-x", String(sceneNumber(light.x, 2)));
      target.setAttribute("data-gosx-scene3d-fluid-light-y", String(sceneNumber(light.y, 2)));
      target.setAttribute("data-gosx-scene3d-fluid-light-z", String(sceneNumber(light.z, -1)));
      target.setAttribute("data-gosx-scene3d-fluid-light-setting", String(!!(controlState && controlState.settingLightDirection)));
    });
  }

  function sceneManagedFluidObjectFormTargetsMount(form, mount) {
    if (!form || !mount) return false;
    const target = form.getAttribute("data-gosx-scene3d-control-target")
      || form.getAttribute("data-gosx-scene3d-target")
      || form.getAttribute("data-scene-target")
      || form.getAttribute("aria-controls")
      || "";
    return target.trim() === "" || target === mount.id;
  }

  function sceneManagedFluidObjectMountCanvas(mount) {
    if (!mount) return null;
    if (String(mount.tagName || "").toLowerCase() === "canvas") return mount;
    return mount.querySelector ? mount.querySelector("canvas") : null;
  }

  function sceneManagedFluidObjectSubjectID(form) {
    return form && (form.getAttribute("data-gosx-scene3d-control-subject")
      || "water-main") || "water-main";
  }

  function sceneManagedFluidObjectSceneSystem(form, sceneState) {
    const waterID = sceneManagedFluidObjectSubjectID(form);
    const systems = Array.isArray(sceneState && sceneState.waterSystems) ? sceneState.waterSystems : [];
    return sceneManagedFluidObjectWaterSystemByID(systems, waterID);
  }

  function sceneManagedFluidObjectWaterSystemByID(systems, waterID) {
    const targetID = String(waterID || "");
    for (let i = 0; i < systems.length; i += 1) {
      if (systems[i] && String(systems[i].id || "") === targetID) return systems[i];
    }
    return systems[0] || null;
  }

  function sceneManagedFluidObjectInteractionProfile(form, sceneState, profile) {
    const system = sceneManagedFluidObjectSceneSystem(form, sceneState);
    if (system && typeof system.interactionProfile === "string" && system.interactionProfile.trim()) {
      return system.interactionProfile.trim();
    }
    const interaction = profile && profile.interaction && typeof profile.interaction === "object" ? profile.interaction : {};
    return typeof interaction.profile === "string" ? interaction.profile.trim() : "";
  }

  function sceneManagedControlCamera(sceneState, options) {
    if (options && typeof options.getCamera === "function") {
      const camera = options.getCamera();
      if (camera) return camera;
    }
    return sceneState && sceneState.camera ? sceneState.camera : null;
  }

  function sceneManagedControlOrbitState(sceneState, options) {
    if (options && typeof options.getOrbitState === "function") {
      const orbit = options.getOrbitState();
      if (orbit) return orbit;
    }
    return sceneState && sceneState.orbit ? sceneState.orbit : null;
  }

  function sceneManagedFluidObjectDefaultLightDirection() {
    return { x: 2, y: 2, z: -1 };
  }

  function sceneManagedFluidObjectNormalizeLightDirection(light) {
    const length = Math.hypot(
      sceneNumber(light && light.x, 0),
      sceneNumber(light && light.y, 0),
      sceneNumber(light && light.z, 0),
    );
    if (length <= 0.000001) return null;
    return {
      x: sceneNumber(light && light.x, 0) / length,
      y: sceneNumber(light && light.y, 0) / length,
      z: sceneNumber(light && light.z, 0) / length,
    };
  }

  function sceneManagedFluidObjectOrbitLightDirection(orbit) {
    if (!orbit) return null;
    const pitch = sceneNumber(orbit.pitch, 0);
    const yaw = sceneNumber(orbit.yaw, 0);
    const cosPitch = Math.cos(pitch);
    return sceneManagedFluidObjectNormalizeLightDirection({
      x: Math.sin(yaw) * cosPitch,
      y: Math.sin(pitch),
      z: -Math.cos(yaw) * cosPitch,
    });
  }

  function sceneManagedFluidObjectCameraLightDirection(camera, orbit) {
    const orbitLight = sceneManagedFluidObjectOrbitLightDirection(orbit);
    if (orbitLight) return orbitLight;
    const cam = typeof sceneRenderCamera === "function" ? sceneRenderCamera(camera) : camera;
    const pitch = sceneNumber(cam && cam.rotationX, 0);
    const yaw = sceneNumber(cam && cam.rotationY, 0);
    const cosPitch = Math.cos(pitch);
    return sceneManagedFluidObjectNormalizeLightDirection({
      x: -Math.sin(yaw) * cosPitch,
      y: Math.sin(pitch),
      z: -Math.cos(yaw) * cosPitch,
    });
  }

  function sceneManagedFluidObjectCameraLightKey(camera) {
    const cam = typeof sceneRenderCamera === "function" ? sceneRenderCamera(camera) : camera;
    if (!cam) return "";
    return [
      sceneNumber(cam.rotationX, 0),
      sceneNumber(cam.rotationY, 0),
      sceneNumber(cam.rotationZ, 0),
    ].map(function(value) { return value.toFixed(6); }).join("|");
  }

  function sceneManagedFluidObjectLightCameraChanged(controlState, sceneState, options) {
    if (!controlState) return false;
    const key = sceneManagedFluidObjectCameraLightKey(sceneManagedControlCamera(sceneState, options));
    return !!(key && key !== controlState.lightCameraKey);
  }

  function sceneManagedFluidObjectSyncLightDirection(controlState, controls, sceneState, options) {
    if (!controlState) return sceneManagedFluidObjectDefaultLightDirection();
    if ((controls && controls.followCamera) || controlState.settingLightDirection) {
      const camera = sceneManagedControlCamera(sceneState, options);
      const orbit = sceneManagedControlOrbitState(sceneState, options);
      const lightDirection = sceneManagedFluidObjectCameraLightDirection(camera, orbit);
      if (lightDirection) {
        controlState.lightDirection = lightDirection;
        controlState.lightCameraKey = sceneManagedFluidObjectCameraLightKey(camera);
      }
    }
    if (controlState.lightDirection) return controlState.lightDirection;
    controlState.lightDirection = sceneManagedFluidObjectDefaultLightDirection();
    controlState.lightCameraKey = "";
    return controlState.lightDirection;
  }

  function sceneManagedFluidObjectPointerID(event) {
    return event && event.pointerId != null ? event.pointerId : "mouse";
  }

  function sceneManagedFluidObjectTouchPoint(event) {
    return {
      x: sceneNumber(event && event.clientX, 0),
      y: sceneNumber(event && event.clientY, 0),
    };
  }

  function sceneManagedFluidObjectTouchDistance(touchPointers) {
    if (!touchPointers || typeof touchPointers.values !== "function" || touchPointers.size < 2) return 0;
    const values = Array.from(touchPointers.values());
    const first = values[0];
    const second = values[1];
    const dx = sceneNumber(second && second.x, 0) - sceneNumber(first && first.x, 0);
    const dy = sceneNumber(second && second.y, 0) - sceneNumber(first && first.y, 0);
    return Math.hypot(dx, dy);
  }

  function sceneManagedFluidObjectControlTarget(sceneState, options) {
    if (options && typeof options.getControlTarget === "function") {
      const target = options.getControlTarget();
      if (target) return target;
    }
    return sceneState && sceneState.controlTarget ? sceneState.controlTarget : { x: 0, y: -0.5, z: 0 };
  }

  function sceneManagedFluidObjectZoomCameraByScale(sceneState, options, scale, profile) {
    if (!options || typeof options.setCamera !== "function") return false;
    const safeScale = sceneNumber(scale, 1);
    if (!(safeScale > 0)) return false;
    const camera = sceneManagedControlCamera(sceneState, options);
    const cam = typeof sceneRenderCamera === "function" ? sceneRenderCamera(camera) : camera;
    if (!cam) return false;
    const target = sceneManagedFluidObjectControlTarget(sceneState, options);
    const interaction = sceneManagedFluidObjectInteractionSettings(profile);
    const minDistance = Math.min(interaction.minZoomDistance, interaction.maxZoomDistance);
    const maxDistance = Math.max(interaction.minZoomDistance, interaction.maxZoomDistance);
    const world = {
      x: sceneNumber(cam.x, 0),
      y: sceneNumber(cam.y, 0),
      z: -sceneNumber(cam.z, 0),
    };
    const offset = {
      x: world.x - sceneNumber(target && target.x, 0),
      y: world.y - sceneNumber(target && target.y, 0),
      z: world.z - sceneNumber(target && target.z, 0),
    };
    const distance = Math.hypot(offset.x, offset.y, offset.z);
    if (distance <= 0.000001) return false;
    const nextDistance = sceneManagedFluidObjectClamp(distance * safeScale, minDistance, maxDistance);
    const factor = nextDistance / distance;
    const nextWorld = {
      x: sceneNumber(target && target.x, 0) + offset.x * factor,
      y: sceneNumber(target && target.y, 0) + offset.y * factor,
      z: sceneNumber(target && target.z, 0) + offset.z * factor,
    };
    return !!options.setCamera(Object.assign({}, cam, {
      x: nextWorld.x,
      y: nextWorld.y,
      z: -nextWorld.z,
    }));
  }

  function sceneManagedFluidObjectZoomCameraByWheel(sceneState, options, event, profile) {
    return sceneManagedFluidObjectZoomCameraByScale(
      sceneState,
      options,
      Math.exp(sceneNumber(event && event.deltaY, 0) * 0.001),
      profile,
    );
  }

  function sceneManagedFluidObjectPointerSample(event, canvas) {
    if (!event || !canvas || typeof canvas.getBoundingClientRect !== "function") return null;
    const rect = canvas.getBoundingClientRect();
    if (!rect || rect.width <= 0 || rect.height <= 0) return null;
    return {
      rect,
      pointerX: sceneManagedFluidObjectClamp(event.clientX - rect.left, 0, rect.width),
      pointerY: sceneManagedFluidObjectClamp(event.clientY - rect.top, 0, rect.height),
    };
  }

  function sceneManagedFluidObjectPointerRay(event, canvas, sceneState, options) {
    const sample = sceneManagedFluidObjectPointerSample(event, canvas);
    if (!sample || typeof sceneScreenToRay !== "function") return null;
    const camera = sceneManagedControlCamera(sceneState, options);
    if (!camera) return null;
    return {
      sample,
      camera,
      ray: sceneScreenToRay(sample.pointerX, sample.pointerY, sample.rect.width, sample.rect.height, camera),
    };
  }

  function sceneManagedFluidObjectPointerPoint(event, canvas, sceneState, controls, options) {
    const raySample = sceneManagedFluidObjectPointerRay(event, canvas, sceneState, options);
    const pool = sceneManagedFluidObjectEffectivePoolControls(controls);
    if (raySample && typeof sceneRayIntersectYPlane === "function") {
      const hit = sceneRayIntersectYPlane(raySample.ray, 0);
      if (hit) {
        return {
          x: sceneManagedFluidObjectClamp(hit.x / Math.max(0.001, pool.poolWidth), -1, 1),
          z: sceneManagedFluidObjectClamp(hit.z / Math.max(0.001, pool.poolLength), -1, 1),
          worldX: hit.x,
          worldY: hit.y,
          worldZ: hit.z,
          source: "ray-plane",
        };
      }
    }
    const sample = raySample ? raySample.sample : sceneManagedFluidObjectPointerSample(event, canvas);
    if (!sample) return null;
    const u = sample.pointerX / sample.rect.width;
    const v = sample.pointerY / sample.rect.height;
    return {
      x: sceneManagedFluidObjectClamp(u * 2 - 1, -1, 1),
      z: sceneManagedFluidObjectClamp(v * 2 - 1, -1, 1),
      source: "canvas",
    };
  }

  function sceneManagedFluidObjectReadBundle(options, camera) {
    const bundle = options && typeof options.getBundle === "function" ? options.getBundle() : null;
    if (!bundle) return null;
    return camera && bundle.camera !== camera ? Object.assign({}, bundle, { camera }) : bundle;
  }

  function sceneManagedFluidObjectHitObjectID(hit) {
    const object = hit && hit.object ? hit.object : null;
    if (!object) return "";
    return String(object.id || object.objectID || object.name || object.label || "").trim();
  }

  function sceneManagedFluidObjectHitMatchesConfig(hit, config) {
    const id = sceneManagedFluidObjectHitObjectID(hit);
    const wanted = String(config && (config.id || config.label || config.objectID) || "").trim();
    return !!id && !!wanted && id === wanted;
  }

  function sceneManagedFluidObjectFindBundleObject(entries, config) {
    if (!Array.isArray(entries) || !config) return null;
    const wanted = String(config.id || config.objectID || config.label || "").trim();
    if (!wanted) return null;
    for (let i = 0; i < entries.length; i += 1) {
      const entry = entries[i];
      const id = String(entry && (entry.id || entry.objectID || entry.name || entry.label) || "").trim();
      if (id === wanted) return entry;
    }
    return null;
  }

  function sceneManagedFluidObjectHitTestMode(config) {
    const raw = String(config && (config.objectHitTest || config.hitTest || config.hitMode) || "").trim().toLowerCase();
    const mode = raw.replace(/[\s_-]+/g, "");
    if (mode === "mesh" || mode === "geometry" || mode === "raycast") return "mesh";
    if (mode === "box" || mode === "aabb" || mode === "cube") return "box";
    if (mode === "sphere" || mode === "boundingsphere" || mode === "boundsphere") return "sphere";
    if (mode === "none" || mode === "disabled") return "none";
    return "auto";
  }

  function sceneManagedFluidObjectPickConfigObject(raySample, bundle, config) {
    if (!raySample || !bundle || !config || typeof sceneRaycastPickGroup !== "function") return null;
    const meshObject = sceneManagedFluidObjectFindBundleObject(bundle.meshObjects, config);
    if (meshObject && bundle.worldMeshPositions) {
      const meshHit = sceneRaycastPickGroup(raySample.ray, [meshObject], bundle.worldMeshPositions, 0, bundle.worldMeshUVs);
      if (meshHit) return meshHit;
    }
    const object = sceneManagedFluidObjectFindBundleObject(bundle.objects, config);
    if (object && bundle.worldPositions) {
      return sceneRaycastPickGroup(raySample.ray, [object], bundle.worldPositions, 0, null);
    }
    return null;
  }

  function sceneManagedFluidObjectAnalyticObjectHit(controlState, controls, config, raySample) {
    if (!controlState || !config || !raySample || !raySample.ray) return null;
    const hitMode = sceneManagedFluidObjectHitTestMode(config);
    if (hitMode === "mesh" || hitMode === "none") return null;
    const objectState = sceneManagedFluidObjectObjectState(controlState, config);
    if (!objectState || !objectState.position) return null;
    const metrics = sceneManagedFluidObjectObjectMetrics(config);
    const center = {
      x: sceneNumber(objectState.position.x, 0),
      y: sceneNumber(objectState.position.y, 0) + sceneNumber(metrics.meshYOffset, 0),
      z: sceneNumber(objectState.position.z, 0),
    };
    const kind = String(config.objectKind || "").toLowerCase();
    let hit = null;
    if ((hitMode === "box" || (hitMode === "auto" && kind === "cube")) && typeof sceneRayIntersectAABB === "function") {
      const halfX = Math.max(0.0001, sceneNumber(config.objectHalfSizeX, metrics.xLimitRadius));
      const halfY = Math.max(0.0001, sceneNumber(config.objectHalfSizeY, metrics.floorClearance));
      const halfZ = Math.max(0.0001, sceneNumber(config.objectHalfSizeZ, metrics.zLimitRadius));
      hit = sceneRayIntersectAABB(raySample.ray, {
        x: center.x - halfX,
        y: center.y - halfY,
        z: center.z - halfZ,
      }, {
        x: center.x + halfX,
        y: center.y + halfY,
        z: center.z + halfZ,
      });
    } else if ((hitMode === "sphere" || hitMode === "auto") && typeof sceneRayIntersectSphere === "function") {
      hit = sceneRayIntersectSphere(raySample.ray, center, Math.max(metrics.xLimitRadius, metrics.zLimitRadius, metrics.buoyancyRadius));
    }
    if (!hit) return null;
    return {
      object: { id: config.id || config.label || "" },
      distance: hit.distance,
      point: hit,
      worldPosition: hit,
    };
  }

  function sceneManagedFluidObjectActiveObjectHit(form, mount, sceneState, controls, profile, event, options) {
    if (!form || !mount || !controls || controls.object === "None") return null;
    const controlState = sceneManagedFluidObjectControlState(form);
    const objects = sceneManagedFluidObjectObjects(profile);
    const config = objects[controls.object] || null;
    if (!config) return null;
    const canvas = sceneManagedFluidObjectMountCanvas(mount);
    const raySample = sceneManagedFluidObjectPointerRay(event, canvas, sceneState, options);
    if (!raySample) return null;
    const hitMode = sceneManagedFluidObjectHitTestMode(config);
    const analyticHit = sceneManagedFluidObjectAnalyticObjectHit(controlState, controls, config, raySample);
    if (analyticHit) {
      return {
        config,
        hit: analyticHit,
        point: analyticHit.worldPosition || analyticHit.point || null,
        camera: raySample.camera,
      };
    }
    if (hitMode === "sphere" || hitMode === "box" || hitMode === "none") return null;
    const bundle = sceneManagedFluidObjectReadBundle(options, raySample.camera);
    if (!bundle) return null;
    const hit = sceneManagedFluidObjectPickConfigObject(raySample, bundle, config);
    if (!hit || !sceneManagedFluidObjectHitMatchesConfig(hit, config)) return null;
    return {
      config,
      hit,
      point: hit.worldPosition || hit.point || null,
      camera: raySample.camera,
    };
  }

  function sceneManagedFluidObjectCameraDragNormal(camera, hitPoint) {
    const cam = typeof sceneRenderCamera === "function" ? sceneRenderCamera(camera) : camera;
    if (cam && typeof sceneRotatePoint === "function") {
      const normal = sceneRotatePoint(
        { x: 0, y: 0, z: 1 },
        sceneNumber(cam.rotationX, 0),
        sceneNumber(cam.rotationY, 0),
        sceneNumber(cam.rotationZ, 0),
      );
      const normalLength = Math.hypot(
        sceneNumber(normal && normal.x, 0),
        sceneNumber(normal && normal.y, 0),
        sceneNumber(normal && normal.z, 0),
      );
      if (normalLength > 0.000001) {
        return {
          x: sceneNumber(normal.x, 0) / normalLength,
          y: sceneNumber(normal.y, 0) / normalLength,
          z: sceneNumber(normal.z, 0) / normalLength,
        };
      }
    }
    const hx = sceneNumber(hitPoint && hitPoint.x, 0);
    const hy = sceneNumber(hitPoint && hitPoint.y, 0);
    const hz = sceneNumber(hitPoint && hitPoint.z, 0);
    const nx = sceneNumber(cam && cam.x, 0) - hx;
    const ny = sceneNumber(cam && cam.y, 0) - hy;
    const nz = -sceneNumber(cam && cam.z, 0) - hz;
    const length = Math.hypot(nx, ny, nz);
    if (length <= 0.000001) return { x: 0, y: 0, z: 1 };
    return { x: nx / length, y: ny / length, z: nz / length };
  }

  function sceneManagedFluidObjectPoolHit(point, controls) {
    if (!point) return false;
    const pool = sceneManagedFluidObjectEffectivePoolControls(controls);
    return Math.abs(sceneNumber(point.x, 0)) < Math.max(0.001, pool.poolWidth) && Math.abs(sceneNumber(point.z, 0)) < Math.max(0.001, pool.poolLength);
  }

  function sceneManagedFluidObjectConsumePointerEvent(event) {
    if (!event) return;
    if (typeof event.preventDefault === "function") event.preventDefault();
    if (typeof event.stopImmediatePropagation === "function") event.stopImmediatePropagation();
    else if (typeof event.stopPropagation === "function") event.stopPropagation();
  }

  function sceneManagedFluidObjectStopCameraInertia(options) {
    if (!options || typeof options.stopCameraInertia !== "function") return false;
    try {
      return options.stopCameraInertia() !== false;
    } catch (_err) {
      return false;
    }
  }

  function sceneManagedFluidObjectStartInteraction(form, mount, sceneState, event, options) {
    const profile = sceneManagedFluidObjectProfile(form);
    if (sceneManagedFluidObjectInteractionProfile(form, sceneState, profile) !== "water-object-drop-orbit") {
      return "OrbitCamera";
    }
    sceneManagedFluidObjectStopCameraInertia(options);
    const controls = sceneManagedFluidObjectReadControls(form);
    const controlState = sceneManagedFluidObjectControlState(form);
    if (!controlState) return "OrbitCamera";
    const objectHit = sceneManagedFluidObjectActiveObjectHit(form, mount, sceneState, controls, profile, event, options);
    if (objectHit && objectHit.point) {
      controlState.pointerMode = "MoveObject";
      controlState.pointerDrag = {
        previousHit: sceneManagedFluidObjectCopyVec3(objectHit.point),
        dragPlaneNormal: sceneManagedFluidObjectCameraDragNormal(objectHit.camera, objectHit.point),
      };
      return "MoveObject";
    }
    const canvas = sceneManagedFluidObjectMountCanvas(mount);
    const raySample = sceneManagedFluidObjectPointerRay(event, canvas, sceneState, options);
    if (raySample && typeof sceneRayIntersectYPlane === "function") {
      const hit = sceneRayIntersectYPlane(raySample.ray, 0);
      const interaction = sceneManagedFluidObjectInteractionSettings(profile);
      if (interaction.pointerDrops && sceneManagedFluidObjectPoolHit(hit, controls)) {
        controlState.pointerMode = "AddDrops";
        controlState.pointerDrag = null;
        return "AddDrops";
      }
    }
    controlState.pointerMode = "OrbitCamera";
    controlState.pointerDrag = null;
    return "OrbitCamera";
  }

  function sceneManagedFluidObjectDragObject(form, mount, sceneState, applyCommands, event, options) {
    const controlState = sceneManagedFluidObjectControlState(form);
    const drag = controlState && controlState.pointerDrag;
    if (!drag || !drag.previousHit || !drag.dragPlaneNormal || typeof sceneRayIntersectPlane !== "function") return false;
    const canvas = sceneManagedFluidObjectMountCanvas(mount);
    const raySample = sceneManagedFluidObjectPointerRay(event, canvas, sceneState, options);
    if (!raySample) return false;
    const nextHit = sceneRayIntersectPlane(raySample.ray, drag.previousHit, drag.dragPlaneNormal);
    if (!nextHit) return false;
    const controls = sceneManagedFluidObjectReadControls(form);
    const profile = sceneManagedFluidObjectProfile(form);
    const objects = sceneManagedFluidObjectObjects(profile);
    const config = controls.object === "None" ? null : objects[controls.object] || null;
    const objectState = sceneManagedFluidObjectObjectState(controlState, config);
    if (!objectState) return false;
    const delta = {
      x: sceneNumber(nextHit.x, 0) - sceneNumber(drag.previousHit.x, 0),
      y: sceneNumber(nextHit.y, 0) - sceneNumber(drag.previousHit.y, 0),
      z: sceneNumber(nextHit.z, 0) - sceneNumber(drag.previousHit.z, 0),
    };
    objectState.previousPosition = sceneManagedFluidObjectCopyVec3(objectState.position);
    objectState.position.x += delta.x;
    objectState.position.y += delta.y;
    objectState.position.z += delta.z;
    objectState.velocity = { x: 0, y: 0, z: 0 };
    sceneManagedFluidObjectClampObjectPosition(objectState.position, config, controls);
    drag.previousHit = sceneManagedFluidObjectCopyVec3(nextHit);
    sceneManagedFluidObjectApply(form, sceneState, applyCommands, options);
    return true;
  }

  function sceneManagedFluidObjectReflectDropEvent(form, dropEvent) {
    if (!form || !dropEvent) return;
    form.dataset.fluidDropEvents = String(dropEvent.id);
    form.dataset.gosxScene3dFluidDropEvents = String(dropEvent.id);
    form.dataset.fluidDropX = String(dropEvent.x);
    form.dataset.fluidDropZ = String(dropEvent.z);
    form.dataset.fluidDropSource = String(dropEvent.source || "");
    if (Number.isFinite(dropEvent.worldX)) form.dataset.fluidDropWorldX = String(dropEvent.worldX);
    if (Number.isFinite(dropEvent.worldZ)) form.dataset.fluidDropWorldZ = String(dropEvent.worldZ);
    const root = sceneManagedControlScope(form);
    if (root) {
      root.setAttribute("data-gosx-scene3d-fluid-drop-events", String(dropEvent.id));
    }
  }

  function sceneManagedFluidObjectQueueDrop(form, mount, sceneState, applyCommands, event, options) {
    const profile = sceneManagedFluidObjectProfile(form);
    const interaction = sceneManagedFluidObjectInteractionSettings(profile);
    if (!interaction.pointerDrops) return false;
    const controls = sceneManagedFluidObjectReadControls(form);
    const canvas = sceneManagedFluidObjectMountCanvas(mount);
    const point = sceneManagedFluidObjectPointerPoint(event, canvas, sceneState, controls, options);
    if (!point) return false;
    const controlState = sceneManagedFluidObjectControlState(form);
    if (!controlState) return false;
    controlState.dropEventID = Math.max(0, Math.floor(sceneNumber(controlState.dropEventID, 0))) + 1;
    controlState.dropEvent = {
      id: controlState.dropEventID,
      x: point.x,
      z: point.z,
      worldX: point.worldX,
      worldY: point.worldY,
      worldZ: point.worldZ,
      source: point.source,
      radius: interaction.dropRadius,
      strength: interaction.dropStrength,
    };
    sceneManagedFluidObjectReflectDropEvent(form, controlState.dropEvent);
    sceneManagedFluidObjectApply(form, sceneState, applyCommands, options);
    return true;
  }

  function sceneManagedFluidObjectSetChecked(form, name, checked) {
    const field = sceneManagedFluidObjectField(form, name);
    if (!field || typeof field.checked !== "boolean") return false;
    if (field.disabled) return false;
    if (field.checked === checked) return false;
    field.checked = checked;
    return true;
  }

  function sceneManagedFluidObjectApply(form, sceneState, applyCommands, options) {
    const controls = sceneManagedFluidObjectReadControls(form);
    const profile = sceneManagedFluidObjectProfile(form);
    const controlState = sceneManagedFluidObjectControlState(form);
    const poolKey = sceneManagedFluidObjectPoolKey(controls);
    const priorPoolKey = controlState && controlState.poolKey ? controlState.poolKey : "";
    const poolChanged = !!(controlState && priorPoolKey && priorPoolKey !== poolKey);
    sceneManagedFluidObjectObserveSelection(controlState, controls, profile);
    sceneManagedFluidObjectSyncLightDirection(controlState, controls, sceneState, options);
    const objects = sceneManagedFluidObjectObjects(profile);
    const config = controls.object === "None" ? null : objects[controls.object] || objects.Sphere || null;
    const objectStep = config ? sceneManagedFluidObjectObjectStep(controlState, controls, config, profile, { poolChanged }) : null;
    const ready = !!(sceneState && typeof applyCommands === "function");
    sceneManagedFluidObjectReflectForm(form, controls, ready, profile, sceneState);
    sceneManagedFluidObjectReflectObjectState(form, controlState, config, objectStep);
    sceneManagedFluidObjectReflectLightDirection(form, controlState);
    if (controlState) controlState.poolKey = poolKey;
    if (!ready) return false;
    const waterID = sceneManagedFluidObjectSubjectID(form);
    const commands = [];
    sceneManagedFluidObjectPatchObjects(commands, config, controls, profile, objectStep);
    sceneManagedFluidObjectPatchModels(commands, sceneState, config, controls, profile, objectStep);
    commands.push({
      kind: SCENE_CMD_SET_PARTICLES,
      data: sceneManagedFluidObjectSystemPayload(sceneState, controls, config, waterID, profile, controlState, objectStep),
    });
    applyCommands(commands);
    if (controlState) {
      controlState.transitionPending = false;
      controlState.poolKey = poolKey;
    }
    return true;
  }

  function sceneManagedFluidObjectBindForm(form, mount, sceneState, applyCommands, options) {
    if (!form || !sceneManagedFluidObjectFormTargetsMount(form, mount)) return null;
    const prior = form.__gosxScene3DFluidObjectControls;
    if (prior && typeof prior.dispose === "function") {
      prior.dispose();
    }
    let disposed = false;
    let scheduled = false;
    let physicsFrame = 0;
    let lightFrame = 0;
    let readyTimer = 0;
    let pointerActive = false;
    let activePointerId = null;
    const touchPointers = new Map();
    let pinchDistance = null;
    let pinching = false;
    let suppressMouseUntil = 0;
    const pointerTarget = sceneManagedFluidObjectMountCanvas(mount) || mount;
    const disposeDisclosure = sceneManagedControlBindDisclosure(form);
    function shouldAnimatePhysics() {
      const controls = sceneManagedFluidObjectReadControls(form);
      return !disposed && controls.gravity && !controls.paused && controls.object !== "None";
    }
    function schedulePhysics() {
      if (disposed || physicsFrame || !shouldAnimatePhysics()) return;
      physicsFrame = sceneManagedFluidObjectRequestFrame(function() {
        physicsFrame = 0;
        if (disposed) return;
        sceneManagedFluidObjectApply(form, sceneState, applyCommands, options);
        schedulePhysics();
      });
    }
    function shouldSyncLightDirection() {
      const controls = sceneManagedFluidObjectReadControls(form);
      const controlState = sceneManagedFluidObjectControlState(form);
      return !disposed && !!((controls && controls.followCamera) || (controlState && controlState.settingLightDirection));
    }
    function scheduleLightSync() {
      if (disposed || lightFrame || !shouldSyncLightDirection()) return;
      lightFrame = sceneManagedFluidObjectRequestFrame(function() {
        lightFrame = 0;
        if (disposed) return;
        const controls = sceneManagedFluidObjectReadControls(form);
        const controlState = sceneManagedFluidObjectControlState(form);
        if (!((controls && controls.followCamera) || (controlState && controlState.settingLightDirection))) return;
        if (sceneManagedFluidObjectLightCameraChanged(controlState, sceneState, options)) {
          sceneManagedFluidObjectApply(form, sceneState, applyCommands, options);
          schedulePhysics();
        }
        scheduleLightSync();
      });
    }
    function schedule() {
      if (disposed || scheduled) return;
      scheduled = true;
      sceneManagedFluidObjectRequestFrame(function() {
        scheduled = false;
        if (!disposed) {
          sceneManagedFluidObjectApply(form, sceneState, applyCommands, options);
          schedulePhysics();
          scheduleLightSync();
        }
      });
    }
    function onSubmit(event) {
      event.preventDefault();
    }
    function onPointerDown(event) {
      if (event && typeof event.type === "string" && event.type.indexOf("pointer") === 0) {
        suppressMouseUntil = sceneManagedFluidObjectNowSeconds() + 0.8;
      }
      const pointerType = String(event && event.pointerType || "");
      if (pointerType === "mouse" && event && event.button != null && event.button !== 0) return;
      if (pointerType !== "touch" && activePointerId !== null) return;
      if (pointerType !== "touch" && event && event.isPrimary === false) return;
      if (pointerType === "touch" && touchPointers.size >= 2) {
        sceneManagedFluidObjectConsumePointerEvent(event);
        return;
      }
      if (pointerTarget && typeof pointerTarget.setPointerCapture === "function" && event && event.pointerId != null) {
        try { pointerTarget.setPointerCapture(event.pointerId); } catch (_err) {}
      }
      if (pointerType === "touch") {
        touchPointers.set(sceneManagedFluidObjectPointerID(event), sceneManagedFluidObjectTouchPoint(event));
        if (touchPointers.size === 2) {
          pinching = true;
          pinchDistance = sceneManagedFluidObjectTouchDistance(touchPointers);
          sceneManagedFluidObjectStopCameraInertia(options);
          pointerActive = false;
          activePointerId = null;
          const controlState = sceneManagedFluidObjectControlState(form);
          if (controlState) {
            controlState.pointerMode = "";
            controlState.pointerDrag = null;
            sceneManagedFluidObjectReflectPointerMode(form, controlState);
          }
          sceneManagedFluidObjectConsumePointerEvent(event);
          return;
        }
        if (pinching || touchPointers.size > 1) {
          sceneManagedFluidObjectConsumePointerEvent(event);
          return;
        }
      }
      const mode = sceneManagedFluidObjectStartInteraction(form, mount, sceneState, event, options);
      if (mode === "OrbitCamera") {
        pointerActive = false;
        activePointerId = null;
        return;
      }
      pointerActive = true;
      activePointerId = sceneManagedFluidObjectPointerID(event);
      sceneManagedFluidObjectConsumePointerEvent(event);
      if (mode === "AddDrops" && sceneManagedFluidObjectQueueDrop(form, mount, sceneState, applyCommands, event, options)) {
        schedulePhysics();
      }
    }
    function onPointerMove(event) {
      const pointerType = String(event && event.pointerType || "");
      const pointerID = sceneManagedFluidObjectPointerID(event);
      if (pointerType === "touch" && touchPointers.has(pointerID)) {
        touchPointers.set(pointerID, sceneManagedFluidObjectTouchPoint(event));
        if (pinching && pinchDistance !== null && touchPointers.size >= 2) {
          const nextDistance = sceneManagedFluidObjectTouchDistance(touchPointers);
          if (nextDistance > 0) {
            const profile = sceneManagedFluidObjectProfile(form);
            if (sceneManagedFluidObjectZoomCameraByScale(sceneState, options, pinchDistance / nextDistance, profile)) {
              scheduleLightSync();
            }
            pinchDistance = nextDistance;
          }
          sceneManagedFluidObjectConsumePointerEvent(event);
          return;
        }
      }
      if (!pointerActive || activePointerId !== pointerID) return;
      const controlState = sceneManagedFluidObjectControlState(form);
      const mode = controlState && controlState.pointerMode;
      if (mode === "AddDrops") {
        if (sceneManagedFluidObjectQueueDrop(form, mount, sceneState, applyCommands, event, options)) {
          sceneManagedFluidObjectConsumePointerEvent(event);
        }
      } else if (mode === "MoveObject") {
        if (sceneManagedFluidObjectDragObject(form, mount, sceneState, applyCommands, event, options)) {
          sceneManagedFluidObjectConsumePointerEvent(event);
        }
      }
    }
    function onPointerEnd(event) {
      const pointerType = String(event && event.pointerType || "");
      const pointerID = sceneManagedFluidObjectPointerID(event);
      if (pointerType === "touch") {
        touchPointers.delete(pointerID);
        if (pinching) {
          if (touchPointers.size < 2) pinchDistance = null;
          if (touchPointers.size === 0) pinching = false;
        }
      }
      const wasActive = pointerActive && activePointerId === pointerID;
      if (wasActive) {
        pointerActive = false;
        activePointerId = null;
        const controlState = sceneManagedFluidObjectControlState(form);
        if (controlState) {
          controlState.pointerMode = "";
          controlState.pointerDrag = null;
          sceneManagedFluidObjectReflectPointerMode(form, controlState);
        }
      }
      if (pointerTarget && typeof pointerTarget.releasePointerCapture === "function" && event && event.pointerId != null) {
        try { pointerTarget.releasePointerCapture(event.pointerId); } catch (_err) {}
      }
    }
    function ignoreSyntheticMouseFallback() {
      return sceneManagedFluidObjectNowSeconds() < suppressMouseUntil;
    }
    function onMouseDown(event) {
      if (ignoreSyntheticMouseFallback()) return;
      onPointerDown(event);
    }
    function onMouseMove(event) {
      if (ignoreSyntheticMouseFallback()) return;
      onPointerMove(event);
    }
    function onMouseEnd(event) {
      if (ignoreSyntheticMouseFallback()) return;
      onPointerEnd(event);
    }
    function onKeyDown(event) {
      const profile = sceneManagedFluidObjectProfile(form);
      if (!sceneManagedFluidObjectInteractionSettings(profile).keyboard || !event) return;
      const targetName = String(event.target && event.target.tagName || "").toLowerCase();
      if (targetName === "input" || targetName === "select" || targetName === "textarea" || (event.target && event.target.isContentEditable)) {
        return;
      }
      let changed = false;
      if (event.code === "Space" && !event.repeat) {
        const paused = sceneManagedFluidObjectField(form, "paused");
        changed = sceneManagedFluidObjectSetChecked(form, "paused", !(paused && paused.checked));
      } else if (event.code === "KeyG" && !event.repeat) {
        const gravity = sceneManagedFluidObjectField(form, "gravity");
        changed = sceneManagedFluidObjectSetChecked(form, "gravity", !(gravity && gravity.checked));
      } else if (event.code === "KeyL") {
        const controlState = sceneManagedFluidObjectControlState(form);
        if (controlState) {
          controlState.settingLightDirection = true;
          sceneManagedFluidObjectSyncLightDirection(controlState, sceneManagedFluidObjectReadControls(form), sceneState, options);
          changed = true;
        }
      }
      if (changed) {
        if (typeof event.preventDefault === "function") event.preventDefault();
        schedule();
      }
    }
    function onKeyUp(event) {
      const profile = sceneManagedFluidObjectProfile(form);
      if (!sceneManagedFluidObjectInteractionSettings(profile).keyboard || !event || event.code !== "KeyL") return;
      const controlState = sceneManagedFluidObjectControlState(form);
      if (controlState && controlState.settingLightDirection) {
        controlState.settingLightDirection = false;
        sceneManagedFluidObjectReflectLightDirection(form, controlState);
        if (typeof event.preventDefault === "function") event.preventDefault();
        schedule();
      }
    }
    function onWheel(event) {
      const profile = sceneManagedFluidObjectProfile(form);
      if (sceneManagedFluidObjectInteractionProfile(form, sceneState, profile) !== "water-object-drop-orbit") return;
      sceneManagedFluidObjectStopCameraInertia(options);
      if (sceneManagedFluidObjectZoomCameraByWheel(sceneState, options, event, profile)) {
        sceneManagedFluidObjectConsumePointerEvent(event);
        scheduleLightSync();
      }
    }
    form.addEventListener("submit", onSubmit);
    form.addEventListener("input", schedule);
    form.addEventListener("change", schedule);
    if (pointerTarget && typeof pointerTarget.addEventListener === "function") {
      pointerTarget.addEventListener("pointerdown", onPointerDown, true);
      pointerTarget.addEventListener("pointermove", onPointerMove, true);
      pointerTarget.addEventListener("pointerup", onPointerEnd, true);
      pointerTarget.addEventListener("pointercancel", onPointerEnd, true);
      pointerTarget.addEventListener("lostpointercapture", onPointerEnd, true);
      pointerTarget.addEventListener("mousedown", onMouseDown, true);
      pointerTarget.addEventListener("mousemove", onMouseMove, true);
      pointerTarget.addEventListener("mouseup", onMouseEnd, true);
      pointerTarget.addEventListener("mouseleave", onMouseEnd, true);
      pointerTarget.addEventListener("wheel", onWheel, { passive: false, capture: true });
    }
    if (typeof window !== "undefined") {
      window.addEventListener("keydown", onKeyDown, true);
      window.addEventListener("keyup", onKeyUp, true);
    }
    schedule();
    let tries = 0;
    readyTimer = window.setInterval(function() {
      tries += 1;
      if (disposed || sceneManagedFluidObjectApply(form, sceneState, applyCommands, options) || tries > 180) {
        window.clearInterval(readyTimer);
        readyTimer = 0;
      }
    }, 100);
    const binding = {
      apply: function() { return sceneManagedFluidObjectApply(form, sceneState, applyCommands, options); },
      read: function() { return sceneManagedFluidObjectReadControls(form); },
      dispose: function() {
        disposed = true;
        form.removeEventListener("submit", onSubmit);
        form.removeEventListener("input", schedule);
        form.removeEventListener("change", schedule);
        if (pointerTarget && typeof pointerTarget.removeEventListener === "function") {
          pointerTarget.removeEventListener("pointerdown", onPointerDown, true);
          pointerTarget.removeEventListener("pointermove", onPointerMove, true);
          pointerTarget.removeEventListener("pointerup", onPointerEnd, true);
          pointerTarget.removeEventListener("pointercancel", onPointerEnd, true);
          pointerTarget.removeEventListener("lostpointercapture", onPointerEnd, true);
          pointerTarget.removeEventListener("mousedown", onMouseDown, true);
          pointerTarget.removeEventListener("mousemove", onMouseMove, true);
          pointerTarget.removeEventListener("mouseup", onMouseEnd, true);
          pointerTarget.removeEventListener("mouseleave", onMouseEnd, true);
          pointerTarget.removeEventListener("wheel", onWheel, true);
        }
        if (typeof window !== "undefined") {
          window.removeEventListener("keydown", onKeyDown, true);
          window.removeEventListener("keyup", onKeyUp, true);
        }
        if (readyTimer) {
          window.clearInterval(readyTimer);
          readyTimer = 0;
        }
        if (physicsFrame) {
          sceneManagedFluidObjectCancelFrame(physicsFrame);
          physicsFrame = 0;
        }
        if (lightFrame) {
          sceneManagedFluidObjectCancelFrame(lightFrame);
          lightFrame = 0;
        }
        disposeDisclosure();
        if (form.__gosxScene3DFluidObjectControls === binding) {
          delete form.__gosxScene3DFluidObjectControls;
        }
        if (form.__gosxScene3DFluidObjectState) {
          delete form.__gosxScene3DFluidObjectState;
        }
      },
    };
    form.__gosxScene3DFluidObjectControls = binding;
    if (typeof window !== "undefined") {
      window.__gosx_scene3d_fluid_object_controls = binding;
    }
    return binding;
  }

  const SCENE_MANAGED_CONTROL_PROFILES = {};

  function registerSceneManagedControlProfile(name, profile) {
    const key = String(name || "").trim();
    if (!key || !profile || typeof profile.selector !== "string" || typeof profile.bind !== "function") {
      return false;
    }
    SCENE_MANAGED_CONTROL_PROFILES[key] = {
      selector: profile.selector,
      bind: profile.bind,
    };
    return true;
  }

  function publishSceneManagedControlProfiles() {
    if (typeof window === "undefined") return;
    window.__gosx_scene3d_control_profiles = SCENE_MANAGED_CONTROL_PROFILES;
    window.__gosx_scene3d_register_control_profile = registerSceneManagedControlProfile;
    if (!window.__gosx_scene3d_api) {
      window.__gosx_scene3d_api = {};
    }
    Object.assign(window.__gosx_scene3d_api, {
      registerSceneManagedControlProfile,
      sceneManagedControlProfiles: SCENE_MANAGED_CONTROL_PROFILES,
    });
    if (window.document && window.document.documentElement) {
      window.document.documentElement.setAttribute(
        "data-gosx-scene3d-control-profiles",
        Object.keys(SCENE_MANAGED_CONTROL_PROFILES).sort().join(","),
      );
    }
  }

  registerSceneManagedControlProfile("fluid-object", {
    selector: '[data-gosx-scene3d-control-form="fluid-object"]',
    bind: sceneManagedFluidObjectBindForm,
  });
  publishSceneManagedControlProfiles();

  function bindSceneManagedControlForms(mount, sceneState, applyCommands, options) {
    if (typeof document === "undefined") return function() {};
    const bindings = [];
    const disposePanels = sceneManagedControlBindPanelToggles(document);
    Object.keys(SCENE_MANAGED_CONTROL_PROFILES).forEach(function(profileName) {
      const profile = SCENE_MANAGED_CONTROL_PROFILES[profileName];
      const forms = Array.prototype.slice.call(document.querySelectorAll(profile.selector));
      forms.forEach(function(form) {
        const binding = profile.bind(form, mount, sceneState, applyCommands, options || {});
        if (binding) bindings.push(binding);
      });
    });
    return function() {
      bindings.forEach(function(binding) {
        if (binding && typeof binding.dispose === "function") binding.dispose();
      });
      disposePanels();
    };
  }

  publishSceneManagedControlProfiles();
