(function() {
  "use strict";

  var _nodeTransforms = new Map();
  var _childSet = new Set();
  var _mixerResults = new Map();

  function sceneAnimBuildNodeTransforms(nodes, animatedTransforms) {
    _nodeTransforms.clear();

    function walkNode(nodeIndex, parentWorld) {
      var node = nodes[nodeIndex];
      if (!node) return;
      var anim = animatedTransforms ? animatedTransforms.get(nodeIndex) : null;

      var local = sceneTRSToMat4(
        anim && anim.position ? anim.position : (node.translation || [0, 0, 0]),
        anim && anim.rotation ? anim.rotation : (node.rotation || [0, 0, 0, 1]),
        anim && anim.scale ? anim.scale : (node.scale || [1, 1, 1])
      );

      var world = parentWorld ? sceneMat4Multiply(parentWorld, local) : local;
      _nodeTransforms.set(nodeIndex, world);

      var children = node.children || [];
      for (var i = 0; i < children.length; i++) {
        walkNode(children[i], world);
      }
    }

    _childSet.clear();
    for (var n = 0; n < nodes.length; n++) {
      var ch = nodes[n] && nodes[n].children;
      if (ch) {
        for (var ci = 0; ci < ch.length; ci++) _childSet.add(ch[ci]);
      }
    }
    for (var i = 0; i < nodes.length; i++) {
      if (!_childSet.has(i)) walkNode(i, null);
    }

    return _nodeTransforms;
  }

  function sceneAnimComputeJointMatrices(skin, nodeTransforms) {
    var jointCount = skin.joints.length;
    var matrices = skin._jointMatricesBuffer;
    if (!matrices || matrices.length !== jointCount * 16) {
      matrices = new Float32Array(jointCount * 16);
      skin._jointMatricesBuffer = matrices;
    }

    for (var i = 0; i < jointCount; i++) {
      var jointNodeIndex = skin.joints[i];
      var worldTransform = nodeTransforms.get(jointNodeIndex) || SCENE_IDENTITY_MAT4;
      var inverseBindOffset = i * 16;
      var inverseBind = skin.inverseBindMatrices.subarray(inverseBindOffset, inverseBindOffset + 16);

      sceneMat4MultiplyInto(_sceneMat4ScratchA, worldTransform, inverseBind);
      matrices.set(_sceneMat4ScratchA, i * 16);
    }

    return matrices;
  }

  function sceneAnimLerpVec(a, b, t) {
    var result = new Array(a.length);
    for (var i = 0; i < a.length; i++) {
      result[i] = a[i] + (b[i] - a[i]) * t;
    }
    return result;
  }

  function sceneAnimLerpVecInto(out, a, b, t) {
    for (var i = 0; i < a.length; i++) {
      out[i] = a[i] + (b[i] - a[i]) * t;
    }
    return out;
  }

  function sceneAnimNormalizeQuat(q) {
    var len = Math.sqrt(q[0] * q[0] + q[1] * q[1] + q[2] * q[2] + q[3] * q[3]);
    if (len < 1e-10) return [0, 0, 0, 1];
    return [q[0] / len, q[1] / len, q[2] / len, q[3] / len];
  }

  function sceneAnimSlerpQuat(a, b, t) {
    var dot = a[0] * b[0] + a[1] * b[1] + a[2] * b[2] + a[3] * b[3];

    var bx = b[0], by = b[1], bz = b[2], bw = b[3];
    if (dot < 0) {
      dot = -dot;
      bx = -bx; by = -by; bz = -bz; bw = -bw;
    }

    if (dot > 0.9995) {
      return sceneAnimNormalizeQuat(sceneAnimLerpVec(a, [bx, by, bz, bw], t));
    }

    var theta = Math.acos(dot);
    var sinTheta = Math.sin(theta);
    var w0 = Math.sin((1 - t) * theta) / sinTheta;
    var w1 = Math.sin(t * theta) / sinTheta;

    return [
      a[0] * w0 + bx * w1,
      a[1] * w0 + by * w1,
      a[2] * w0 + bz * w1,
      a[3] * w0 + bw * w1,
    ];
  }

  function sceneAnimSlerpQuatInto(out, a, b, t) {
    var dot = a[0] * b[0] + a[1] * b[1] + a[2] * b[2] + a[3] * b[3];

    var bx = b[0], by = b[1], bz = b[2], bw = b[3];
    if (dot < 0) {
      dot = -dot;
      bx = -bx; by = -by; bz = -bz; bw = -bw;
    }

    if (dot > 0.9995) {
      sceneAnimLerpVecInto(out, a, [bx, by, bz, bw], t);
      var len = Math.sqrt(out[0] * out[0] + out[1] * out[1] + out[2] * out[2] + out[3] * out[3]);
      if (len < 1e-10) { out[0] = 0; out[1] = 0; out[2] = 0; out[3] = 1; }
      else { out[0] /= len; out[1] /= len; out[2] /= len; out[3] /= len; }
      return out;
    }

    var theta = Math.acos(dot);
    var sinTheta = Math.sin(theta);
    var w0 = Math.sin((1 - t) * theta) / sinTheta;
    var w1 = Math.sin(t * theta) / sinTheta;

    out[0] = a[0] * w0 + bx * w1;
    out[1] = a[1] * w0 + by * w1;
    out[2] = a[2] * w0 + bz * w1;
    out[3] = a[3] * w0 + bw * w1;
    return out;
  }

  function _sceneAnimSlerpQuatOffset(out, arrA, offA, arrB, offB, t) {
    var a0 = arrA[offA], a1 = arrA[offA + 1], a2 = arrA[offA + 2], a3 = arrA[offA + 3];
    var bx = arrB[offB], by = arrB[offB + 1], bz = arrB[offB + 2], bw = arrB[offB + 3];
    var dot = a0 * bx + a1 * by + a2 * bz + a3 * bw;

    if (dot < 0) {
      dot = -dot;
      bx = -bx; by = -by; bz = -bz; bw = -bw;
    }

    if (dot > 0.9995) {
      out[0] = a0 + (bx - a0) * t;
      out[1] = a1 + (by - a1) * t;
      out[2] = a2 + (bz - a2) * t;
      out[3] = a3 + (bw - a3) * t;
      var len = Math.sqrt(out[0] * out[0] + out[1] * out[1] + out[2] * out[2] + out[3] * out[3]);
      if (len < 1e-10) { out[0] = 0; out[1] = 0; out[2] = 0; out[3] = 1; }
      else { out[0] /= len; out[1] /= len; out[2] /= len; out[3] /= len; }
      return out;
    }

    var theta = Math.acos(dot);
    var sinTheta = Math.sin(theta);
    var w0 = Math.sin((1 - t) * theta) / sinTheta;
    var w1 = Math.sin(t * theta) / sinTheta;

    out[0] = a0 * w0 + bx * w1;
    out[1] = a1 * w0 + by * w1;
    out[2] = a2 * w0 + bz * w1;
    out[3] = a3 * w0 + bw * w1;
    return out;
  }

  function sceneAnimInterpolateChannel(channel, time) {
    var times = channel.times;
    var values = channel.values;
    var isRotation = channel.property === "rotation";
    var componentCount = isRotation ? 4 : 3;
    var scratch = isRotation ? _animScratch4 : _animScratch3;

    if (time <= times[0]) {
      channel._lastIndex = 0;
      for (var si = 0; si < componentCount; si++) scratch[si] = values[si];
      return scratch;
    }
    if (time >= times[times.length - 1]) {
      channel._lastIndex = 0;
      var start = (times.length - 1) * componentCount;
      for (var si = 0; si < componentCount; si++) scratch[si] = values[start + si];
      return scratch;
    }

    var i = channel._lastIndex || 0;
    if (i >= times.length - 1 || times[i] > time) i = 0;
    while (i < times.length - 1 && times[i + 1] < time) i++;
    channel._lastIndex = i;

    var t0 = times[i];
    var t1 = times[i + 1];
    var alpha = (time - t0) / (t1 - t0);

    var start0 = i * componentCount;
    var start1 = (i + 1) * componentCount;

    if (channel.interpolation === "STEP") {
      for (var si = 0; si < componentCount; si++) scratch[si] = values[start0 + si];
      return scratch;
    }

    if (isRotation) {
      _sceneAnimSlerpQuatOffset(scratch, values, start0, values, start1, alpha);
    } else {
      scratch[0] = values[start0]     + (values[start1]     - values[start0])     * alpha;
      scratch[1] = values[start0 + 1] + (values[start1 + 1] - values[start0 + 1]) * alpha;
      scratch[2] = values[start0 + 2] + (values[start1 + 2] - values[start0 + 2]) * alpha;
    }
    return scratch;
  }

  function sceneAnimBlendValue(existing, newValue, weight, property) {
    var t = weight / (existing.totalWeight + weight);
    existing.totalWeight += weight;

    if (property === "rotation") {
      existing.value = sceneAnimSlerpQuat(existing.value, newValue, t);
    } else {
      existing.value = sceneAnimLerpVec(existing.value, newValue, t);
    }
  }

  function createSceneAnimationMixer() {
    var clips = new Map();  // name -> clip data
    var active = [];        // active playback entries

    function addClip(name, clip) {
      if (clip && clip.channels) {
        for (var ci = 0; ci < clip.channels.length; ci++) {
          var ch = clip.channels[ci];
          ch._key = ch.targetID + ":" + ch.property;
          ch._lastIndex = 0;
        }
      }
      clips.set(name, clip);
    }

    function removeClip(name) {
      stop(name, { fadeOut: 0 });
      clips.delete(name);
    }

    function findActive(name) {
      for (var i = 0; i < active.length; i++) {
        if (active[i].name === name) return active[i];
      }
      return null;
    }

    function play(name, options) {
      var clip = clips.get(name);
      if (!clip) {
        console.warn("[gosx] animation clip not found:", name);
        return;
      }

      var opts = options || {};
      var loop = opts.loop !== undefined ? opts.loop : true;
      var speed = opts.speed !== undefined ? opts.speed : 1.0;
      var fadeIn = opts.fadeIn !== undefined ? opts.fadeIn : 0.3;
      var weight = opts.weight !== undefined ? opts.weight : 1.0;

      var existing = findActive(name);
      if (existing) {
        existing.speed = speed;
        existing.targetWeight = weight;
        if (!existing.stopping) {
          existing.weight = weight;
        }
        return;
      }

      var entry = {
        name: name,
        clip: clip,
        time: 0,
        weight: fadeIn > 0 ? 0 : weight,
        targetWeight: weight,
        speed: speed,
        loop: loop,
        fadeIn: fadeIn,
        fadeOut: 0,
        fadeTime: 0,
        stopping: false,
      };
      active.push(entry);
    }

    function stop(name, options) {
      var entry = findActive(name);
      if (!entry) return;

      var opts = options || {};
      var fadeOut = opts.fadeOut !== undefined ? opts.fadeOut : 0.3;

      if (fadeOut > 0) {
        entry.stopping = true;
        entry.fadeOut = fadeOut;
        entry.fadeTime = 0;
      } else {
        for (var i = active.length - 1; i >= 0; i--) {
          if (active[i].name === name) {
            active.splice(i, 1);
            break;
          }
        }
      }
    }

    function stopAll() {
      active.length = 0;
    }

    function update(deltaTime, applyTransform) {
      var i, entry, channel, value, key, existing;

      for (i = 0; i < active.length; i++) {
        entry = active[i];
        entry.time += deltaTime * entry.speed;

        if (entry.loop && entry.clip.duration > 0 && entry.time >= entry.clip.duration) {
          entry.time = entry.time % entry.clip.duration;
        }

        if (entry.fadeIn > 0 && !entry.stopping && entry.fadeTime < entry.fadeIn) {
          entry.fadeTime += deltaTime;
          entry.weight = Math.min(1.0, entry.fadeTime / entry.fadeIn) * entry.targetWeight;
        }

        if (entry.stopping && entry.fadeOut > 0) {
          entry.fadeTime += deltaTime;
          entry.weight = Math.max(0, 1.0 - entry.fadeTime / entry.fadeOut) * entry.targetWeight;
        }
      }

      for (i = active.length - 1; i >= 0; i--) {
        entry = active[i];
        if (entry.stopping && (entry.fadeOut <= 0 || entry.fadeTime >= entry.fadeOut)) {
          active.splice(i, 1);
          continue;
        }
        if (!entry.loop && entry.clip.duration > 0 && entry.time >= entry.clip.duration) {
          active.splice(i, 1);
          continue;
        }
      }

      _mixerResults.clear();

      for (i = 0; i < active.length; i++) {
        entry = active[i];
        if (entry.weight <= 0) continue;

        for (var c = 0; c < entry.clip.channels.length; c++) {
          channel = entry.clip.channels[c];
          value = sceneAnimInterpolateChannel(channel, entry.time);
          key = channel._key;

          existing = _mixerResults.get(key);
          if (!existing) {
            var componentCount = channel.property === "rotation" ? 4 : 3;
            var copied = new Array(componentCount);
            for (var vi = 0; vi < componentCount; vi++) copied[vi] = value[vi];
            _mixerResults.set(key, {
              targetID: channel.targetID,
              property: channel.property,
              value: copied,
              totalWeight: entry.weight,
            });
          } else {
            sceneAnimBlendValue(existing, value, entry.weight, channel.property);
          }
        }
      }

      _mixerResults.forEach(function(result) {
        applyTransform(result.targetID, result.property, result.value);
      });
    }

    function hasClips() {
      return clips.size > 0;
    }

    function isPlaying(name) {
      return findActive(name) !== null;
    }

    function dispose() {
      active.length = 0;
      clips.clear();
    }

    return {
      addClip: addClip,
      removeClip: removeClip,
      play: play,
      stop: stop,
      stopAll: stopAll,
      update: update,
      hasClips: hasClips,
      isPlaying: isPlaying,
      dispose: dispose,
    };
  }

  if (typeof window !== "undefined") {
    window.__gosx_scene3d_animation_api = {
      createMixer: createSceneAnimationMixer,
      buildNodeTransforms: sceneAnimBuildNodeTransforms,
      computeJointMatrices: sceneAnimComputeJointMatrices,
    };
    window.__gosx_scene3d_animation_loaded = true;
  }

  window.__gosx_scene3d_animation_api = {
    createMixer: createSceneAnimationMixer,
    buildNodeTransforms: sceneAnimBuildNodeTransforms,
    computeJointMatrices: sceneAnimComputeJointMatrices,
  };

  window.__gosx_scene3d_animation_loaded = true;

})();
//# sourceMappingURL=bootstrap-feature-scene3d-animation.js.map
