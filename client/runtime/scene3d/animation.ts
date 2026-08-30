  // animation.ts — Scene3D animation mixer — keyframe interpolation, clip playback,
  // @ts-check
  // crossfading, and quaternion slerp for skeletal and transform animation.
  //
  // Matrix math (SCENE_IDENTITY_MAT4, sceneMat4Multiply, sceneMat4MultiplyInto,
  // sceneTRSToMat4, sceneTRSToMat4Into) and scratch buffers (_sceneMat4ScratchA,
  // _sceneMat4ScratchB, _animScratch3, _animScratch4) are defined in
  // 11-scene-math.ts.

  /**
   * @typedef {object} GoSXSceneAnimationClip
   * @property {string} [name]
   * @property {number} duration
   * @property {Array<object>} channels
   */

  // ---------------------------------------------------------------------------
  // Cached containers for node-transform traversal (avoid per-frame alloc)
  // ---------------------------------------------------------------------------

  var _nodeTransforms = new Map();
  var _childSet = new Set();
  var _mixerResults = new Map();

  // Walk the node hierarchy and compute world transforms for every node.
  // nodes: glTF node array (each has optional children, translation, rotation, scale).
  // animatedTransforms: Map<nodeIndex, { translation, position, rotation, scale }> from mixer.
  // rootTransform: optional model-instance transform prepended to root nodes.
  // Returns Map<nodeIndex, Float32Array(16)> of world transforms (reused map).
  function sceneAnimBuildNodeTransforms(nodes, animatedTransforms, rootTransform, rootNodes) {
    _nodeTransforms.clear();

    function walkNode(nodeIndex, parentWorld) {
      var node = nodes[nodeIndex];
      if (!node) return;
      var anim = animatedTransforms ? animatedTransforms.get(nodeIndex) : null;

      // A pose entry only overrides TRS when it animates translation, rotation,
      // or scale. A weights-only morph pose is not a TRS override, so an
      // authored glTF node matrix must survive it.
      var animOverridesTRS = !!(anim && (anim.translation || anim.position || anim.rotation || anim.scale));

      var local;
      if (animOverridesTRS) {
        local = sceneTRSToMat4(
          anim && (anim.translation || anim.position) ? (anim.translation || anim.position) : (node.translation || [0, 0, 0]),
          anim && anim.rotation ? anim.rotation : (node.rotation || [0, 0, 0, 1]),
          anim && anim.scale ? anim.scale : (node.scale || [1, 1, 1])
        );
      } else if (node.matrix && node.matrix.length === 16) {
        // Authored 4x4 node matrix (column-major, translation in elements
        // 12-14). Copy it: the traversal map is reused across frames and its
        // entries are handed to callers, so they must never alias or mutate
        // the source asset buffers.
        local = new Float32Array(node.matrix);
      } else {
        local = sceneTRSToMat4(
          node.translation || [0, 0, 0],
          node.rotation || [0, 0, 0, 1],
          node.scale || [1, 1, 1]
        );
      }

      var world = parentWorld ? sceneMat4Multiply(parentWorld, local) : local;
      _nodeTransforms.set(nodeIndex, world);

      var children = node.children || [];
      for (var i = 0; i < children.length; i++) {
        walkNode(children[i], world);
      }
    }

    if (Array.isArray(rootNodes) && rootNodes.length) {
      for (var ri = 0; ri < rootNodes.length; ri++) {
        walkNode(rootNodes[ri], rootTransform || null);
      }
    } else {
      // Find root nodes (not referenced as a child of any other node).
      _childSet.clear();
      for (var n = 0; n < nodes.length; n++) {
        var ch = nodes[n] && nodes[n].children;
        if (ch) {
          for (var ci = 0; ci < ch.length; ci++) _childSet.add(ch[ci]);
        }
      }
      for (var i = 0; i < nodes.length; i++) {
        if (!_childSet.has(i)) walkNode(i, rootTransform || null);
      }
    }

    return _nodeTransforms;
  }

  // Compute per-joint skinning matrices from the current animation pose.
  // skin: { joints: [...], inverseBindMatrices: Float32Array, _jointMatricesBuffer?: Float32Array }
  // nodeTransforms: Map<nodeIndex, Float32Array(16)> — world transform per node.
  // Returns Float32Array(jointCount * 16) ready for GPU upload.
  // If skin._jointMatricesBuffer exists, it is reused to avoid allocation.
  function sceneAnimComputeJointMatrices(skin, nodeTransforms) {
    var joints = skin && Array.isArray(skin.joints) ? skin.joints : [];
    var jointCount = joints.length;
    if (jointCount <= 0) {
      return new Float32Array(0);
    }
    var inverseBindMatrices = skin.inverseBindMatrices && typeof skin.inverseBindMatrices.length === "number"
      ? skin.inverseBindMatrices
      : null;
    var matrices = skin._jointMatricesBuffer;
    if (!matrices || matrices.length !== jointCount * 16) {
      matrices = new Float32Array(jointCount * 16);
      skin._jointMatricesBuffer = matrices;
    }

    for (var i = 0; i < jointCount; i++) {
      var jointNodeIndex = joints[i];
      var worldTransform = nodeTransforms && typeof nodeTransforms.get === "function"
        ? (nodeTransforms.get(jointNodeIndex) || SCENE_IDENTITY_MAT4)
        : SCENE_IDENTITY_MAT4;
      var inverseBindOffset = i * 16;
      var inverseBind = inverseBindMatrices && inverseBindOffset + 16 <= inverseBindMatrices.length
        ? inverseBindMatrices.subarray(inverseBindOffset, inverseBindOffset + 16)
        : SCENE_IDENTITY_MAT4;

      sceneMat4MultiplyInto(_sceneMat4ScratchA, worldTransform, inverseBind);
      matrices.set(_sceneMat4ScratchA, i * 16);
    }

    return matrices;
  }

  // ---------------------------------------------------------------------------
  // Scalar math helpers
  // ---------------------------------------------------------------------------

  function sceneAnimLerpVec(a, b, t) {
    var result = new Array(a.length);
    for (var i = 0; i < a.length; i++) {
      result[i] = a[i] + (b[i] - a[i]) * t;
    }
    return result;
  }

  // Non-allocating lerp: writes into pre-allocated `out`.
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

    // Ensure shortest path.
    var bx = b[0], by = b[1], bz = b[2], bw = b[3];
    if (dot < 0) {
      dot = -dot;
      bx = -bx; by = -by; bz = -bz; bw = -bw;
    }

    // When quaternions are very close, fall back to normalized lerp.
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

  // Non-allocating slerp: writes into pre-allocated `out`.
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

  // Non-allocating slerp from offset into flat value arrays.
  // Reads 4 elements from arrA[offA..] and arrB[offB..], writes into out[0..3].
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

  // ---------------------------------------------------------------------------
  // Keyframe interpolation
  // ---------------------------------------------------------------------------

  // Resolve how many values one keyframe of this channel holds. glTF morph
  // "weights" channels carry one value per morph target, so the width cannot
  // come from the property name alone. gltfExtractAnimations records the true
  // width; fall back to the TRS widths for hand-built clips.
  function sceneAnimChannelWidth(channel) {
    var declared = channel && channel.componentCount;
    if (typeof declared === "number" && declared > 0) {
      return Math.floor(declared);
    }
    return channel && channel.property === "rotation" ? 4 : 3;
  }

  // Return a scratch array of the requested width. Widths of 3 and 4 reuse the
  // shared buffers. Wider channels keep one cached buffer each, so a morph
  // weights channel still allocates nothing per frame.
  function sceneAnimChannelScratch(channel, width) {
    if (width === 4) {
      return _animScratch4;
    }
    if (width === 3) {
      return _animScratch3;
    }
    if (!channel._scratch || channel._scratch.length !== width) {
      channel._scratch = new Float32Array(width);
    }
    return channel._scratch;
  }

  function sceneAnimInterpolateChannel(channel, time) {
    var times = channel.times;
    var values = channel.values;
    var isRotation = channel.property === "rotation";
    var componentCount = isRotation ? 4 : sceneAnimChannelWidth(channel);
    var scratch = isRotation ? _animScratch4 : sceneAnimChannelScratch(channel, componentCount);

    // CUBICSPLINE (glTF 2.0 Appendix C) stores three width-wide vectors per
    // key: [inTangent, value, outTangent]. Every other mode stores one wide
    // vector per key. The PROPERTY value sits one vector into each key.
    var isCubic = channel.interpolation === "CUBICSPLINE";
    var keyStride = isCubic ? componentCount * 3 : componentCount;
    var valueOffset = isCubic ? componentCount : 0;

    // Clamp before first keyframe.
    if (time <= times[0]) {
      channel._lastIndex = 0;
      for (var si = 0; si < componentCount; si++) scratch[si] = values[valueOffset + si];
      return scratch;
    }
    // Clamp after last keyframe.
    if (time >= times[times.length - 1]) {
      channel._lastIndex = 0;
      var start = (times.length - 1) * keyStride + valueOffset;
      for (var si = 0; si < componentCount; si++) scratch[si] = values[start + si];
      return scratch;
    }

    // Find the pair of keyframes surrounding `time`.
    // Start from _lastIndex — time advances monotonically within a loop
    // iteration, so the common case is O(1). Reset on wrap (time < cached).
    var i = channel._lastIndex || 0;
    if (i >= times.length - 1 || times[i] > time) i = 0;
    while (i < times.length - 1 && times[i + 1] < time) i++;
    channel._lastIndex = i;

    var t0 = times[i];
    var t1 = times[i + 1];
    var alpha = (time - t0) / (t1 - t0);

    var start0 = i * keyStride + valueOffset;
    var start1 = (i + 1) * keyStride + valueOffset;

    if (channel.interpolation === "STEP") {
      for (var si = 0; si < componentCount; si++) scratch[si] = values[start0 + si];
      return scratch;
    }

    if (isCubic) {
      // Cubic Hermite between key i and key i + 1 (glTF 2.0 Appendix C).
      // The stored tangents are derivatives, so they scale by the actual
      // interval duration. All components interpolate independently — for
      // rotations this is deliberately NOT slerp and the tangent controls
      // are NOT sign-flipped; the quaternion is normalized afterwards.
      var dt = t1 - t0;
      var u = alpha;
      var u2 = u * u;
      var u3 = u2 * u;
      var h00 = 2 * u3 - 3 * u2 + 1;
      var h10 = u3 - 2 * u2 + u;
      var h01 = -2 * u3 + 3 * u2;
      var h11 = u3 - u2;
      var m0Off = i * keyStride + 2 * componentCount;   // outTangent of key i
      var m1Off = (i + 1) * keyStride;                  // inTangent of key i + 1
      for (var ci = 0; ci < componentCount; ci++) {
        var p0 = values[start0 + ci];
        var p1 = values[start1 + ci];
        var m0 = values[m0Off + ci] * dt;
        var m1 = values[m1Off + ci] * dt;
        scratch[ci] = h00 * p0 + h10 * m0 + h01 * p1 + h11 * m1;
      }
      if (isRotation) {
        var len = Math.sqrt(
          scratch[0] * scratch[0] + scratch[1] * scratch[1] +
          scratch[2] * scratch[2] + scratch[3] * scratch[3]
        );
        if (len < 1e-10) {
          scratch[0] = 0; scratch[1] = 0; scratch[2] = 0; scratch[3] = 1;
        } else {
          scratch[0] /= len; scratch[1] /= len; scratch[2] /= len; scratch[3] /= len;
        }
      }
      return scratch;
    }

    // LINEAR — quaternion slerp for rotations, vector lerp otherwise.
    // Inline into scratch arrays to avoid per-frame allocation.
    if (isRotation) {
      _sceneAnimSlerpQuatOffset(scratch, values, start0, values, start1, alpha);
    } else {
      for (var li = 0; li < componentCount; li++) {
        scratch[li] = values[start0 + li] + (values[start1 + li] - values[start0 + li]) * alpha;
      }
    }
    return scratch;
  }

  // ---------------------------------------------------------------------------
  // Value blending (weighted mix of multiple clips targeting the same property)
  // ---------------------------------------------------------------------------

  function sceneAnimBlendValue(existing, newValue, weight, property) {
    var t = weight / (existing.totalWeight + weight);
    existing.totalWeight += weight;

    if (property === "rotation") {
      existing.value = sceneAnimSlerpQuat(existing.value, newValue, t);
    } else {
      existing.value = sceneAnimLerpVec(existing.value, newValue, t);
    }
  }

  // ---------------------------------------------------------------------------
  // WASM motion-mixer bridge (P4-M3) — opt-in via window.__gosx_motion_wasm.
  //
  // These helpers translate between the JS-side glTF clip/pose representation
  // and the Go WASM motion mixer. They are pure functions with no side effects
  // on the WASM runtime, so they unit-test in isolation. Mount code (in
  // 20-scene-mount.js) wires them to the actual __gosx_motion_mixer_* exports
  // behind the flag; when the flag is off none of this runs.
  // ---------------------------------------------------------------------------

  // Build the JSON payload for __gosx_motion_mixer_add_clip from a parsed glTF
  // clip (as produced by gltfExtractAnimations / sceneCloneModelAnimations).
  // Each channel carries a node index (targetID/targetNode), a property string
  // ("translation"|"rotation"|"scale"|"weights"), an interpolation string, and
  // typed times/values arrays which are flattened to plain number arrays.
  //
  // Morph "weights" channels hold one value per morph target, so their width
  // cannot be derived from the property name. The declared componentCount is
  // forwarded as the "weightCount" JSON key — the exact key the Go WASM bridge
  // reads — so both ends agree. When that width is missing or invalid the key
  // is omitted rather than guessed at 3 or 4, letting the native side reject
  // the channel. The caller's clip, channels and typed arrays are never
  // mutated or aliased.
  function sceneAnimWasmClipJSON(clip) {
    var channels = clip && Array.isArray(clip.channels) ? clip.channels : [];
    var out = { duration: clip && typeof clip.duration === "number" ? clip.duration : 0, channels: [] };
    for (var i = 0; i < channels.length; i++) {
      var ch = channels[i];
      if (!ch) continue;
      var record = {
        node: ch.targetID != null ? ch.targetID : ch.targetNode,
        property: typeof ch.property === "string" ? ch.property : "translation",
        interpolation: typeof ch.interpolation === "string" && ch.interpolation ? ch.interpolation : "LINEAR",
        times: Array.from(ch.times || []),
        values: Array.from(ch.values || []),
      };
      if (record.property === "weights") {
        var declared = ch.componentCount;
        // Native morph IDs are propID = 1000 + weightIndex with weightIndex
        // in [0, weightCount - 1], so weightCount must satisfy
        // 1000 + weightCount - 1 <= _SCENE_ANIM_WASM_MAX_ID. Anything above
        // that bound (including huge finite values like 1e100 that the Go
        // int unmarshaler rejects) would invalidate the whole clip, so the
        // key is omitted and the channel is rejected per-channel instead.
        if (Number.isInteger(declared) && declared > 0 &&
            declared <= _SCENE_ANIM_WASM_MAX_ID - _SCENE_ANIM_WASM_WEIGHT_PROP_BASE + 1) {
          record.weightCount = declared;
        }
      }
      out.channels.push(record);
    }
    return JSON.stringify(out);
  }

  // Map a packed propID to the TRS property name written by the mixer:
  // translation=0, rotation=1, scale=2. Morph-weight writes use
  // propID = 1000 + weightIndex (see _SCENE_ANIM_WASM_WEIGHT_PROP_BASE).
  var _SCENE_ANIM_WASM_PROPS = ["translation", "rotation", "scale"];

  // Component width per motion ValueArity ordinal (motion/ir.go): scalar=0,
  // vec2=1, vec3=2, vec4=3, quat=4, color=5. Only consulted for records whose
  // width the propID cannot imply; an ordinal outside this table leaves the
  // record width unknowable and decoding must stop rather than desync.
  var _SCENE_ANIM_WASM_ARITY_WIDTHS = [1, 2, 3, 4, 4, 4];

  // Native BuildClipTimeline emits one scalar packed write per morph weight,
  // [nodeID, 1000 + weightIndex, ArityScalar, value], with ArityScalar
  // (ordinal 0 — not 1) carrying exactly one float. Values are raw weights:
  // negative and >1 are legal and are never clamped or normalized.
  var _SCENE_ANIM_WASM_WEIGHT_PROP_BASE = 1000;

  // Native validates signed int32 target/prop IDs; the decoder mirrors that
  // bound so NaN, fractional, negative or >2^31-1 floats can never mint node
  // or weight entries.
  var _SCENE_ANIM_WASM_MAX_ID = 2147483647;

  function _sceneAnimWasmIsValidID(v) {
    return Number.isInteger(v) && v >= 0 && v <= _SCENE_ANIM_WASM_MAX_ID;
  }

  // Decode the packed LE-float64 buffer written by __gosx_motion_mixer_update
  // into the animatedTransforms Map
  // (Map<nodeIndex, {translation,rotation,scale,weights}>).
  // Layout per write: [targetID, propID, arity, comps...] where targetID is the
  // glTF node index, propID is 0(translation)/1(rotation)/2(scale) or
  // 1000+weightIndex for morph weights, and arity is the motion ValueArity
  // enum ordinal (ArityScalar=0, ArityVec2=1, ArityVec3=2, ArityVec4=3,
  // ArityQuat=4, ArityColor=5) — NOT the component count.
  //
  // Width resolution:
  // - Known TRS propIDs keep the legacy semantic width (rotation(1) → 4
  //   quaternion floats; translation(0)/scale(2) → 3 floats) and the arity
  //   slot is ignored for them: historical fixtures record the component
  //   count there (vec3 → 3), which is not the ArityVec3 ordinal (2).
  // - Every other record derives its stride from the arity ordinal, so scalar
  //   weight writes (arity 0 → 1 float) no longer desync the translation /
  //   quaternion / scale writes around them. An ordinal outside the known
  //   table leaves the width unknowable and stops the walk safely.
  //
  // Weight records merge into entry.weights[weightIndex] on the same per-node
  // entry as TRS writes. Records may arrive in any order; weights may be
  // negative or >1 (no clamping or normalization) and any target count is
  // accepted — no width cap.
  // A weight slot is accepted only inside the established vector length plus
  // the records this packet staged for its node — dense emits from native
  // carry every scalar track — so a slot beyond that bound is a malformed
  // sparse outlier: it is dropped uncounted instead of materializing an
  // untrusted-ID-sized array, and its valid siblings are kept.
  //
  // Header defense: target/prop IDs must be whole non-negative int32s.
  // Records with bad headers are skipped in stride while their width is
  // still known and never create node or weight entries. `count` is clamped
  // to f.length, a record that would run past it ends the walk, and the
  // input buffer is only ever read (decoded arrays are fresh copies).
  function sceneAnimWasmDecodePose(f, count, animatedTransforms) {
    if (!f || !animatedTransforms ||
        typeof animatedTransforms.get !== "function" || typeof animatedTransforms.set !== "function") return 0;
    if (typeof count !== "number" || !(count >= 3)) return 0;
    if (count > f.length) count = f.length;

    var writes = 0;
    // nodeID -> flat [slot, value, slot, value, ...] staged pairs. The map is
    // allocated up front and materialization below runs unconditionally (a
    // forEach over an empty map is a no-op), so the hot loop carries no
    // lazy-init branch; shuffled records cost no per-write copies, entries
    // are touched exactly once per decode, and sparse outliers are filtered
    // once the packet's full per-node record count is known.
    var stagedWeights = new Map();

    var i = 0;
    while (i + 3 <= count) {
      var targetID = f[i], propID = f[i + 1], arity = f[i + 2];

      // Known TRS propIDs index straight into the property-name table; the
      // arity slot is not trusted for them (see the compatibility note above).
      var prop = _SCENE_ANIM_WASM_PROPS[propID];
      if (!prop && !(Number.isInteger(arity) && arity >= 0 && arity <= 5)) {
        break; // unknown arity ordinal — record width cannot be known
      }
      var width = prop ? (propID === 1 ? 4 : 3) : _SCENE_ANIM_WASM_ARITY_WIDTHS[arity];

      var c = i + 3;
      if (c + width > count) break; // truncated record — stop before reading it

      if (_sceneAnimWasmIsValidID(targetID) && _sceneAnimWasmIsValidID(propID)) {
        if (prop) {
          var entry = animatedTransforms.get(targetID);
          if (!entry) animatedTransforms.set(targetID, (entry = {}));
          var value = new Array(width);
          for (var k = 0; k < width; k++) value[k] = f[c + k];
          entry[prop] = value;
          writes++;
        } else if (propID >= _SCENE_ANIM_WASM_WEIGHT_PROP_BASE && width === 1) {
          // Scalar morph-weight write: [nodeID, 1000+weightIndex, ArityScalar, value].
          // Staged as one flat (slot, value) pair; acceptance and its write
          // count are settled at materialization, after the walk.
          var staged = stagedWeights.get(targetID);
          if (!staged) stagedWeights.set(targetID, (staged = []));
          staged.push(propID - _SCENE_ANIM_WASM_WEIGHT_PROP_BASE, f[c]);
        }
        // Any other known-arity property is skipped in stride only.
      }

      i = c + width;
    }

    // Materialize each node's weight vector once after the walk: a fresh,
    // dense, per-node array (never shared between entries, never aliased to
    // the input buffer, never mutating a default array another node may
    // still share). A write at slot K implies a dense vector of K+1 slots,
    // but the dense length may only grow by what this packet actually paid
    // for: native emits every scalar track of a new or extended vector, so a
    // slot is accepted only when it lands inside the established length plus
    // this packet's record count for the node (existingLen + the staged pair
    // count). A slot beyond that bound is a malformed sparse outlier — an
    // untrusted ID must never buy allocation or loop time — and is dropped
    // while its valid siblings are kept. The scratch vector is sized by that
    // same paid-for bound and trimmed to the kept maximum afterwards, so
    // neither pass ever scales with a raw ID value; slots below the kept
    // maximum that no record targeted keep their previous value, defaulting
    // to the glTF morph weight 0. One O(established + received) pass per
    // node — no per-write reallocation. A node whose every staged slot was
    // an outlier is left untouched.
    stagedWeights.forEach(function (staged, nodeID) {
      var entry = animatedTransforms.get(nodeID);
      var existing = entry && entry.weights;
      var existingLen = existing && typeof existing.length === "number" ? existing.length : 0;
      var limit = existingLen + (staged.length >> 1);
      var weights = new Array(limit);
      for (var wi = 0; wi < limit; wi++) {
        var prev = wi < existingLen ? existing[wi] : 0;
        weights[wi] = typeof prev === "number" ? prev : 0;
      }
      var accepted = 0;
      var total = existingLen;
      for (var r = 0; r < staged.length; r += 2) {
        var slot = staged[r];
        if (slot < limit) {
          weights[slot] = staged[r + 1];
          accepted++;
          if (slot >= total) total = slot + 1;
        }
      }
      if (accepted === 0) return; // every staged slot was a sparse outlier
      weights.length = total;
      if (!entry) animatedTransforms.set(nodeID, (entry = {}));
      entry.weights = weights;
      writes += accepted;
    });

    return writes;
  }

  // ---------------------------------------------------------------------------
  // AnimationMixer factory
  // ---------------------------------------------------------------------------

  function createSceneAnimationMixer() {
    var clips = new Map();  // name -> clip data
    var active = [];        // active playback entries

    function addClip(name, clip) {
      // Pre-compute composite keys on channels to avoid per-frame string concat.
      // Initialize _lastIndex for monotonic keyframe search caching.
      if (clip && clip.channels) {
        for (var ci = 0; ci < clip.channels.length; ci++) {
          var ch = clip.channels[ci];
          if (ch.targetID == null && ch.targetNode != null) {
            ch.targetID = ch.targetNode;
          }
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

      // If already playing, update mutable options and return.
      var existing = findActive(name);
      if (existing) {
        existing.speed = speed;
        existing.loop = loop;
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
        // Immediate removal.
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

      // 1. Advance time and handle fading for every active entry.
      for (i = 0; i < active.length; i++) {
        entry = active[i];
        entry.time += deltaTime * entry.speed;

        // Looping.
        if (entry.loop && entry.clip.duration > 0 && entry.time >= entry.clip.duration) {
          entry.time = entry.time % entry.clip.duration;
        }

        // Fade-in.
        if (entry.fadeIn > 0 && !entry.stopping && entry.fadeTime < entry.fadeIn) {
          entry.fadeTime += deltaTime;
          entry.weight = Math.min(1.0, entry.fadeTime / entry.fadeIn) * entry.targetWeight;
        }

        // Fade-out (stopping).
        if (entry.stopping && entry.fadeOut > 0) {
          entry.fadeTime += deltaTime;
          entry.weight = Math.max(0, 1.0 - entry.fadeTime / entry.fadeOut) * entry.targetWeight;
        }
      }

      // 2. Remove finished entries (iterate backwards for safe splicing).
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

      // 3. Interpolate channels and blend per target+property.
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
            // Copy scratch array — it will be overwritten by the next interpolation.
            var componentCount = channel.property === "rotation" ? 4 : sceneAnimChannelWidth(channel);
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

      // 4. Apply blended transforms.
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

  // Publish the animation API onto window for the legacy monolithic
  // bootstrap.js bundle that inlines 19a-scene-animation.js. The split
  // bootstrap-feature-scene3d-animation.js bundle also publishes in
  // 26g-feature-scene3d-animation-suffix.js; both writing the same
  // value is a harmless double-set.
  if (typeof window !== "undefined") {
    window.__gosx_scene3d_animation_api = {
      createMixer: createSceneAnimationMixer,
      buildNodeTransforms: sceneAnimBuildNodeTransforms,
      computeJointMatrices: sceneAnimComputeJointMatrices,
      wasmClipJSON: sceneAnimWasmClipJSON,
      wasmDecodePose: sceneAnimWasmDecodePose,
    };
    window.__gosx_scene3d_animation_loaded = true;
  }
