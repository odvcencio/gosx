// 05a — the page's one audio mixer (window.__gosx.audioMixer).
//
// Chunks: bootstrap.js, bootstrap-lite.js, bootstrap-runtime.js (after 05-document-env.ts,
// whose gosxNumber, gosxAudioClamp01 and gosxAudioVector3 helpers it shares). It cannot be lazy:
// sound must start an AudioContext synchronously inside the user gesture that calls it.

  // The page's one audio mixer. Every GoSX audio path (clips from
  // window.__gosx.audio, the arcade synth, streamed <audio> elements, and the
  // Go game/audio package) shares this AudioContext and these buses:
  //   sources -> bus gain -> master gain -> compressor -> destination
  // Each bus gain is volume * duck, or 0 while muted. Volumes and mutes are data
  // first, so settings applied before the first user gesture survive until the
  // context exists.
  const GOSX_MIXER_STANDARD_BUSES = ["music", "sfx", "voice", "ui"];
  const gosxMixerState = {
    context: null,
    master: null,
    compressor: null,
    buses: new Map(),
    listeners: new Set(),
    followsCamera: true,
    cameraKey: "",
    pose: null,
  };

  function gosxMixerBusID(id) {
    return String(id || "master").trim() || "master";
  }

  function gosxMixerBus(id, parent) {
    const key = gosxMixerBusID(id);
    let bus = gosxMixerState.buses.get(key);
    if (!bus) {
      bus = { id: key, parent: key === "master" ? "" : gosxMixerBusID(parent), volume: 1, muted: false, ducks: new Map(), node: null };
      gosxMixerState.buses.set(key, bus);
    } else if (parent && key !== "master") {
      bus.parent = gosxMixerBusID(parent);
    }
    return bus;
  }

  function gosxMixerEffective(bus) {
    if (bus.muted) {
      return 0;
    }
    let duck = 1;
    bus.ducks.forEach(function(amount) {
      duck = Math.min(duck, amount);
    });
    return bus.volume * duck;
  }

  function gosxMixerApply(bus, seconds) {
    if (!bus.node || !bus.node.gain) {
      return;
    }
    const value = gosxMixerEffective(bus);
    const param = bus.node.gain;
    const context = gosxMixerState.context;
    const constant = Math.max(0.001, seconds === undefined ? 0.015 : seconds);
    if (context && typeof param.setTargetAtTime === "function") {
      if (typeof param.cancelScheduledValues === "function") {
        param.cancelScheduledValues(context.currentTime || 0);
      }
      param.setTargetAtTime(value, context.currentTime || 0, constant / 3);
    } else {
      param.value = value;
    }
  }

  function gosxMixerNotify(bus) {
    const detail = { bus: bus.id, volume: bus.volume, muted: bus.muted, effective: gosxMixerEffective(bus) };
    gosxMixerState.listeners.forEach(function(listener) {
      try {
        listener(detail);
      } catch (_error) {}
    });
  }

  function gosxMixerBuildNode(bus) {
    const context = gosxMixerState.context;
    if (!context || bus.node || typeof context.createGain !== "function") {
      return;
    }
    bus.node = context.createGain();
    bus.node.gain.value = gosxMixerEffective(bus);
    if (bus.id === "master") {
      gosxMixerState.master = bus.node;
      let tail = bus.node;
      if (typeof context.createDynamicsCompressor === "function") {
        const compressor = context.createDynamicsCompressor();
        if (compressor.threshold) compressor.threshold.value = -12;
        if (compressor.knee) compressor.knee.value = 18;
        if (compressor.ratio) compressor.ratio.value = 3;
        if (compressor.attack) compressor.attack.value = 0.008;
        if (compressor.release) compressor.release.value = 0.2;
        bus.node.connect(compressor);
        tail = compressor;
        gosxMixerState.compressor = compressor;
      }
      tail.connect(context.destination);
      return;
    }
    const parent = gosxMixerBus(bus.parent || "master");
    gosxMixerBuildNode(parent);
    if (parent.node) {
      bus.node.connect(parent.node);
    }
  }

  // gosxMixerContext returns the shared AudioContext, creating it on first use.
  // options ({sampleRate, latencyHint}) apply only to that first creation; a
  // caller that needs a specific rate compares context.sampleRate afterwards.
  function gosxMixerContext(options) {
    if (gosxMixerState.context) {
      return gosxMixerState.context;
    }
    const Ctor = gosxAudioContextConstructor();
    if (!Ctor) {
      return null;
    }
    try {
      const settings = {};
      if (options && options.sampleRate > 0) settings.sampleRate = options.sampleRate;
      if (options && options.latencyHint) settings.latencyHint = options.latencyHint;
      gosxMixerState.context = new Ctor(settings);
    } catch (_error) {
      gosxMixerState.context = null;
      return null;
    }
    Array.from(gosxMixerState.buses.values()).forEach(gosxMixerBuildNode);
    if (gosxMixerState.pose) {
      gosxMixerSetListener(gosxMixerState.pose[0], gosxMixerState.pose[1], gosxMixerState.pose[2]);
    }
    return gosxMixerState.context;
  }

  // gosxMixerInput returns the node a source connects to for bus id. An
  // unknown id registers a new bus under master. Null without WebAudio.
  function gosxMixerInput(id) {
    const context = gosxMixerContext();
    if (!context) {
      return null;
    }
    const bus = gosxMixerBus(id, "master");
    gosxMixerBuildNode(bus);
    return bus.node;
  }

  function gosxMixerSetVolume(id, volume) {
    const bus = gosxMixerBus(id, "master");
    bus.volume = gosxAudioClamp01(volume, 1);
    gosxMixerApply(bus);
    gosxMixerNotify(bus);
    return bus.volume;
  }

  function gosxMixerSetMuted(id, muted) {
    const bus = gosxMixerBus(id, "master");
    bus.muted = Boolean(muted);
    gosxMixerApply(bus);
    gosxMixerNotify(bus);
    return bus.muted;
  }

  // gosxMixerDuck lowers bus id to the linear factor amount (0..1) over attack
  // seconds. key names the ducker, so two callers do not undo each other; the
  // lowest factor wins. gosxMixerUnduck(id, key, release) restores the bus.
  function gosxMixerDuck(id, amount, attack, key) {
    const bus = gosxMixerBus(id, "master");
    bus.ducks.set(String(key || "default"), gosxAudioClamp01(amount, 1));
    gosxMixerApply(bus, gosxNumber(attack, 0.05));
    gosxMixerNotify(bus);
  }

  function gosxMixerUnduck(id, key, release) {
    const bus = gosxMixerBus(id, "master");
    bus.ducks.delete(String(key || "default"));
    gosxMixerApply(bus, gosxNumber(release, 0.3));
    gosxMixerNotify(bus);
  }

  function gosxMixerSnapshot() {
    const out = {};
    gosxMixerState.buses.forEach(function(bus, id) {
      out[id] = { volume: bus.volume, muted: bus.muted, effective: gosxMixerEffective(bus), parent: bus.parent };
    });
    return out;
  }

  // gosxMixerRouteElement sends a media element's sound through bus id, so
  // streamed audio obeys the same volume, mute and duck state as clips. It
  // returns false (and leaves the element alone) when WebAudio is missing.
  function gosxMixerRouteElement(element, id) {
    const context = gosxMixerContext();
    if (!context || !element || typeof context.createMediaElementSource !== "function") {
      return false;
    }
    if (element.__gosxMixerSource) {
      return true;
    }
    const input = gosxMixerInput(id);
    if (!input) {
      return false;
    }
    try {
      const source = context.createMediaElementSource(element);
      source.connect(input);
      element.__gosxMixerSource = source;
      return true;
    } catch (_error) {
      return false;
    }
  }

  function gosxMixerListenerParam(param, value, time) {
    if (param && typeof param.setValueAtTime === "function") {
      param.setValueAtTime(value, time);
    } else if (param) {
      param.value = value;
    }
  }

  // gosxMixerSetListener places the shared AudioListener. Call it each frame
  // with the camera's position, forward direction and up vector.
  function gosxMixerSetListener(position, forward, up) {
    // Do not create the AudioContext just to move a listener: browsers keep a
    // new context suspended until a user gesture, and frames run before one.
    gosxMixerState.pose = [position, forward, up];
    const context = gosxMixerState.context;
    const listener = context && context.listener;
    if (!listener) {
      return false;
    }
    const p = gosxAudioVector3(position) || { x: 0, y: 0, z: 0 };
    const f = gosxAudioVector3(forward) || { x: 0, y: 0, z: -1 };
    const u = gosxAudioVector3(up) || { x: 0, y: 1, z: 0 };
    const now = context.currentTime || 0;
    if (listener.positionX) {
      gosxMixerListenerParam(listener.positionX, p.x, now);
      gosxMixerListenerParam(listener.positionY, p.y, now);
      gosxMixerListenerParam(listener.positionZ, p.z, now);
      gosxMixerListenerParam(listener.forwardX, f.x, now);
      gosxMixerListenerParam(listener.forwardY, f.y, now);
      gosxMixerListenerParam(listener.forwardZ, f.z, now);
      gosxMixerListenerParam(listener.upX, u.x, now);
      gosxMixerListenerParam(listener.upY, u.y, now);
      gosxMixerListenerParam(listener.upZ, u.z, now);
    } else {
      if (typeof listener.setPosition === "function") listener.setPosition(p.x, p.y, p.z);
      if (typeof listener.setOrientation === "function") listener.setOrientation(f.x, f.y, f.z, u.x, u.y, u.z);
    }
    return true;
  }

  // gosxMixerRotate applies a Scene3D camera's X, Y, Z rotation to v, the same
  // order sceneRotatePoint uses to take camera space to world space.
  function gosxMixerRotate(v, rx, ry, rz) {
    let y = v[1] * Math.cos(rx) - v[2] * Math.sin(rx);
    let z = v[1] * Math.sin(rx) + v[2] * Math.cos(rx);
    let x = v[0] * Math.cos(ry) + z * Math.sin(ry);
    z = -v[0] * Math.sin(ry) + z * Math.cos(ry);
    const nx = x * Math.cos(rz) - y * Math.sin(rz);
    const ny = x * Math.sin(rz) + y * Math.cos(rz);
    return { x: nx, y: ny, z };
  }

  // gosxMixerFollowCamera moves the listener to a Scene3D camera ({x, y, z,
  // rotationX, rotationY, rotationZ}). The renderer calls it every frame; it
  // does nothing while following is off (gosxMixerAPI.followCamera(false)) and
  // skips a camera that has not moved.
  function gosxMixerFollowCamera(camera) {
    if (!gosxMixerState.followsCamera || !camera) {
      return false;
    }
    const rx = gosxNumber(camera.rotationX, 0);
    const ry = gosxNumber(camera.rotationY, 0);
    const rz = gosxNumber(camera.rotationZ, 0);
    const key = [camera.x, camera.y, camera.z, rx, ry, rz].join(",");
    if (key === gosxMixerState.cameraKey) {
      return false;
    }
    gosxMixerState.cameraKey = key;
    return gosxMixerSetListener(
      { x: gosxNumber(camera.x, 0), y: gosxNumber(camera.y, 0), z: gosxNumber(camera.z, 0) },
      gosxMixerRotate([0, 0, -1], rx, ry, rz),
      gosxMixerRotate([0, 1, 0], rx, ry, rz),
    );
  }

  const gosxMixerAPI = {
    followCamera: function(enabled) {
      gosxMixerState.followsCamera = enabled !== false;
      gosxMixerState.cameraKey = "";
    },
    updateCamera: gosxMixerFollowCamera,
    buses: ["master"].concat(GOSX_MIXER_STANDARD_BUSES),
    context: gosxMixerContext,
    input: gosxMixerInput,
    registerBus: function(id, parent) {
      gosxMixerBus(id, parent);
    },
    setVolume: gosxMixerSetVolume,
    volume: function(id) {
      return gosxMixerBus(id).volume;
    },
    setMuted: gosxMixerSetMuted,
    muted: function(id) {
      return gosxMixerBus(id).muted;
    },
    duck: gosxMixerDuck,
    unduck: gosxMixerUnduck,
    snapshot: gosxMixerSnapshot,
    routeElement: gosxMixerRouteElement,
    setListener: gosxMixerSetListener,
    onChange: function(listener) {
      if (typeof listener !== "function") {
        return function() {};
      }
      gosxMixerState.listeners.add(listener);
      return function() {
        gosxMixerState.listeners.delete(listener);
      };
    },
  };
  gosxMixerBus("master");
  GOSX_MIXER_STANDARD_BUSES.forEach(function(id) {
    gosxMixerBus(id, "master");
  });
  window.__gosx = window.__gosx || {};
  window.__gosx.audioMixer = gosxMixerAPI;

