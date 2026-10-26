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
      if (!animOverridesTRS && node.matrix && node.matrix.length === 16) {
        // Authored matrices survive weights-only poses. Copy the asset buffer:
        // traversal results are mutable and must never alias decoded node data.
        local = new Float32Array(node.matrix);
      } else {
        // Animated and authored TRS use the same defaults and composition path.
        local = sceneTRSToMat4(
          anim && (anim.translation || anim.position) ? (anim.translation || anim.position) : (node.translation || [0, 0, 0]),
          anim && anim.rotation ? anim.rotation : (node.rotation || [0, 0, 0, 1]),
          anim && anim.scale ? anim.scale : (node.scale || [1, 1, 1])
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

  // Non-allocating lerp: writes into pre-allocated `out`.
  function sceneAnimLerpVecInto(out, a, b, t) {
    for (var i = 0; i < a.length; i++) {
      out[i] = a[i] + (b[i] - a[i]) * t;
    }
    return out;
  }

  // Share the offset implementation so both channel interpolation and clip
  // blending use the same shortest-path math without temporary arrays.
  function sceneAnimSlerpQuatInto(out, a, b, t) {
    return _sceneAnimSlerpQuatOffset(out, a, 0, b, 0, t);
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
      sceneAnimSlerpQuatInto(existing.value, existing.value, newValue, t);
    } else {
      sceneAnimLerpVecInto(existing.value, existing.value, newValue, t);
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
            /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ declared <= _SCENE_ANIM_WASM_MAX_ID - _SCENE_ANIM_WASM_WEIGHT_PROP_BASE + 1) {
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
    var active = new Map(); // clip name -> playback entry, in blend order

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
      var existing = active.get(name);
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
      active.set(name, entry);
    }

    function stop(name, options) {
      var entry = active.get(name);
      if (!entry) return;

      var opts = options || {};
      var fadeOut = opts.fadeOut !== undefined ? opts.fadeOut : 0.3;

      if (fadeOut > 0) {
        entry.stopping = true;
        entry.fadeOut = fadeOut;
        entry.fadeTime = 0;
      } else {
        active.delete(name);
      }
    }

    function stopAll() {
      active.clear();
    }

    function update(deltaTime, applyTransform) {
      var channel, value, key, existing;

      // 1. Advance time, handle fading, and retire finished entries in one pass.
      for (const entry of active.values()) {
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

        // Deleting the current map entry leaves surviving blend order intact.
        if ((entry.stopping && (entry.fadeOut <= 0 || entry.fadeTime >= entry.fadeOut))
          || (!entry.loop && entry.clip.duration > 0 && entry.time >= entry.clip.duration)) {
          active.delete(entry.name);
        }
      }

      // 2. Interpolate channels and blend per target+property.
      _mixerResults.clear();

      for (const entry of active.values()) {
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

      // 3. Apply blended transforms.
      _mixerResults.forEach(function(result) {
        applyTransform(result.targetID, result.property, result.value);
      });
    }

    function hasClips() {
      return clips.size > 0;
    }

    function isPlaying(name) {
      return active.has(name);
    }

    function dispose() {
      active.clear();
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

  // ---------------------------------------------------------------------------
  // Sampled clip layers (GSP4): the caller supplies exact clip times
  // ---------------------------------------------------------------------------
  //
  // A sampler holds no clock. sample(layers, count, output) is a pure function
  // of the layers and the (immutable) clip data: the same layers always write
  // the same transforms, in any call order, including after a jump backwards.
  // Two details keep that true:
  //  - Keyframe search is stateless. The mixer caches channel._lastIndex, and a
  //    cached start index changes which key a time exactly on a key boundary
  //    picks (visible with STEP). The sampler recomputes the index by binary
  //    search before every interpolation.
  //  - Blending runs in layer order over reused result slots that are fully
  //    rewritten each call, so no value survives from an earlier call.
  // Time past the clip end follows the layer's loop flag via
  // sceneAnimationSampleTime: loop wraps (modulo duration), once clamps.

  // Smallest i in [0, n-2] with times[i + 1] >= time. This is the index a scan
  // from zero reaches, so the result does not depend on any earlier call. The
  // previous result (channel._ci) is only a hint: it is accepted when it, or a
  // neighbour, satisfies the same rule (times[i + 1] >= time and, for i > 0,
  // times[i] < time); otherwise a binary search finds the canonical index.
  // Time that moves a little per frame therefore costs O(1) per channel.
  //
  // The hot path passes time through a Float64Array slot (buf[0]) instead of a
  // number argument: V8 boxes a double argument into a heap number at a call it
  // does not inline, and the sampled path must not allocate.
  function sceneClipSeekBuf(channel: any, buf: Float64Array) {
    var times = channel.times;
    var time = buf[0];
    var last = times.length - 1;
    if (last < 1 || time <= times[0] || time >= times[last]) {
      channel._lastIndex = 0;
      return;
    }
    var i = channel._ci | 0;
    if (i >= last) i = 0;
    if (!(times[i + 1] >= time && (i === 0 || times[i] < time))) {
      if (i + 1 < last && times[i + 2] >= time && times[i + 1] < time) {
        i++;
      } else if (i > 0 && times[i] >= time && (i === 1 || times[i - 1] < time)) {
        i--;
      } else {
        var lo = 0, hi = last - 1;
        while (lo < hi) {
          var mid = (lo + hi) >> 1;
          if (times[mid + 1] >= time) hi = mid; else lo = mid + 1;
        }
        i = lo;
      }
    }
    channel._ci = i;
    channel._lastIndex = i;
  }

  var _clipSeekTime = new Float64Array(1);
  function sceneAnimSeekChannel(channel: any, time: number) {
    _clipSeekTime[0] = time;
    sceneClipSeekBuf(channel, _clipSeekTime);
  }

  // Quaternion slerp with t read from buf[ti]. Same arithmetic, in the same
  // order, as _sceneAnimSlerpQuatOffset.
  function sceneClipSlerp(out: any, arrA: any, offA: number, arrB: any, offB: number, buf: Float64Array, ti: number) {
    var t = buf[ti];
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
      return;
    }
    var theta = Math.acos(dot);
    var sinTheta = Math.sin(theta);
    var w0 = Math.sin((1 - t) * theta) / sinTheta;
    var w1 = Math.sin(t * theta) / sinTheta;
    out[0] = a0 * w0 + bx * w1;
    out[1] = a1 * w0 + by * w1;
    out[2] = a2 * w0 + bz * w1;
    out[3] = a3 * w0 + bw * w1;
  }

  // The same slerp for Float64Array inputs (blended pose storage). Two copies
  // keep each function monomorphic: one body fed both Float32Array and
  // Float64Array would use generic element loads, which box every double.
  function sceneClipSlerpF64(out: any, arrA: any, offA: number, arrB: any, offB: number, buf: Float64Array, ti: number) {
    var t = buf[ti];
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
      return;
    }
    var theta = Math.acos(dot);
    var sinTheta = Math.sin(theta);
    var w0 = Math.sin((1 - t) * theta) / sinTheta;
    var w1 = Math.sin(t * theta) / sinTheta;
    out[0] = a0 * w0 + bx * w1;
    out[1] = a1 * w0 + by * w1;
    out[2] = a2 * w0 + bz * w1;
    out[3] = a3 * w0 + bw * w1;
  }

  // Evaluate one channel at buf[0] into out[0..width), for translation,
  // rotation and scale channels. It is sceneAnimInterpolateChannel with the
  // cached-hint seek and no scratch return value, so its results are bit-equal
  // to the general path and it allocates nothing.
  function sceneClipEvalChannel(channel: any, buf: Float64Array, out: Float64Array) {
    var times = channel.times;
    var values = channel.values;
    var time = buf[0];
    var isRotation = channel.property === "rotation";
    var width = isRotation ? 4 : 3;
    var isCubic = channel.interpolation === "CUBICSPLINE";
    var keyStride = isCubic ? width * 3 : width;
    var valueOffset = isCubic ? width : 0;
    if (time <= times[0]) {
      for (var a = 0; a < width; a++) out[a] = values[valueOffset + a];
      return;
    }
    if (time >= times[times.length - 1]) {
      var start = (times.length - 1) * keyStride + valueOffset;
      for (var b = 0; b < width; b++) out[b] = values[start + b];
      return;
    }
    sceneClipSeekBuf(channel, buf);
    var i = channel._ci;
    var t0 = times[i];
    var t1 = times[i + 1];
    var alpha = (time - t0) / (t1 - t0);
    var start0 = i * keyStride + valueOffset;
    var start1 = (i + 1) * keyStride + valueOffset;
    if (channel.interpolation === "STEP") {
      for (var c = 0; c < width; c++) out[c] = values[start0 + c];
      return;
    }
    if (isCubic) {
      var dt = t1 - t0;
      var u = alpha;
      var u2 = u * u;
      var u3 = u2 * u;
      var h00 = 2 * u3 - 3 * u2 + 1;
      var h10 = u3 - 2 * u2 + u;
      var h01 = -2 * u3 + 3 * u2;
      var h11 = u3 - u2;
      var m0Off = i * keyStride + 2 * width;
      var m1Off = (i + 1) * keyStride;
      for (var d = 0; d < width; d++) {
        var p0 = values[start0 + d];
        var p1 = values[start1 + d];
        var m0 = values[m0Off + d] * dt;
        var m1 = values[m1Off + d] * dt;
        out[d] = h00 * p0 + h10 * m0 + h01 * p1 + h11 * m1;
      }
      if (isRotation) {
        var len = Math.sqrt(out[0] * out[0] + out[1] * out[1] + out[2] * out[2] + out[3] * out[3]);
        if (len < 1e-10) { out[0] = 0; out[1] = 0; out[2] = 0; out[3] = 1; }
        else { out[0] /= len; out[1] /= len; out[2] /= len; out[3] /= len; }
      }
      return;
    }
    if (isRotation) {
      buf[1] = alpha;
      sceneClipSlerp(out, values, start0, values, start1, buf, 1);
    } else {
      for (var e = 0; e < width; e++) out[e] = values[start0 + e] + (values[start1 + e] - values[start0 + e]) * alpha;
    }
  }

  function sceneClipChannelTarget(channel: any) {
    return channel.targetID != null ? channel.targetID : channel.targetNode;
  }

  // Authored horizontal translation of a root node (used when root motion is off).
  function sceneClipAuthoredHorizontal(node: any, axis: number) {
    if (node && node.translation && node.translation.length >= 3) return node.translation[axis];
    if (node && node.matrix && node.matrix.length === 16) return node.matrix[axis === 0 ? 12 : 14];
    return 0;
  }

  // Root nodes: glTF scene roots and the topmost joint of each skin.
  function sceneClipRootNodes(nodes: any[], skins: any, rootNodes: any) {
    var roots = new Set<any>();
    var list = Array.isArray(rootNodes) ? rootNodes : [];
    for (var i = 0; i < list.length; i++) roots.add(list[i]);
    var parent = new Map<any, any>();
    for (var n = 0; n < nodes.length; n++) {
      var children = nodes[n] && nodes[n].children;
      if (children) for (var c = 0; c < children.length; c++) parent.set(children[c], n);
    }
    var skinList = Array.isArray(skins) ? skins : [];
    for (var s = 0; s < skinList.length; s++) {
      var joints = skinList[s] && skinList[s].joints;
      if (!joints) continue;
      var jointSet = new Set<any>(joints);
      for (var j = 0; j < joints.length; j++) {
        if (!jointSet.has(parent.get(joints[j]))) roots.add(joints[j]);
      }
    }
    return roots;
  }

  // Preallocated pose evaluation for one sampled model. Everything the hot path
  // writes (blended TRS, world matrices, joint matrices) is sized once here, so
  // evaluating a frame allocates nothing. The arithmetic mirrors
  // sceneAnimBuildNodeTransforms and sceneAnimComputeJointMatrices exactly
  // (same multiply order, same float32 stores), so the results are bit-equal.
  function sceneClipBuildPoseState(nodes: any[], rootNodes: any, skins: any[]) {
    var n = nodes.length;
    var childSet = new Set<number>();
    for (var c = 0; c < n; c++) {
      var ch = nodes[c] && nodes[c].children;
      if (ch) for (var k = 0; k < ch.length; k++) childSet.add(ch[k]);
    }
    var roots: number[] = [];
    if (Array.isArray(rootNodes) && rootNodes.length) roots = rootNodes.slice();
    else for (var r = 0; r < n; r++) if (!childSet.has(r)) roots.push(r);
    var st: any = {
      n: n,
      order: [] as number[],
      parent: new Int32Array(n).fill(-1),
      worldBuf: new Float32Array(n * 16),
      world: [] as Float32Array[],
      flags: new Uint8Array(n),
      t: new Float64Array(n * 3), r: new Float64Array(n * 4), s: new Float64Array(n * 3),
      wt: new Float64Array(n * 3),
      authT: new Float64Array(n * 3), authR: new Float64Array(n * 4), authS: new Float64Array(n * 3),
      matrix: [] as any[],
      local: new Float32Array(16),
      tS: new Float64Array(3), rS: new Float64Array(4), sS: new Float64Array(3), q: new Float64Array(4),
      reached: new Uint8Array(n),
      buf: new Float64Array(4), v: new Float64Array(4),
      skins: [] as any[],
    };
    for (var i = 0; i < n; i++) {
      var node = nodes[i] || {};
      st.world.push(st.worldBuf.subarray(i * 16, i * 16 + 16));
      st.worldBuf.set(SCENE_IDENTITY_MAT4, i * 16);
      var tr = node.translation || [0, 0, 0], ro = node.rotation || [0, 0, 0, 1], sc = node.scale || [1, 1, 1];
      for (var a = 0; a < 3; a++) { st.authT[i * 3 + a] = tr[a]; st.authS[i * 3 + a] = sc[a]; }
      for (var b = 0; b < 4; b++) st.authR[i * 4 + b] = ro[b];
      st.matrix.push(node.matrix && node.matrix.length === 16 ? new Float32Array(node.matrix) : null);
    }
    function walk(index: number, parent: number) {
      if (!nodes[index] || st.reached[index]) return;
      st.reached[index] = 1;
      st.parent[index] = parent;
      st.order.push(index);
      var kids = nodes[index].children || [];
      for (var q = 0; q < kids.length; q++) walk(kids[q], index);
    }
    for (var ri = 0; ri < roots.length; ri++) walk(roots[ri], -1);
    for (var si = 0; si < skins.length; si++) {
      var skin = skins[si];
      var joints = skin && Array.isArray(skin.joints) ? skin.joints : [];
      var count = joints.length;
      var buffer = skin && skin._jointMatricesBuffer;
      if (count > 0 && (!buffer || buffer.length !== count * 16)) buffer = skin._jointMatricesBuffer = new Float32Array(count * 16);
      var ibm = skin && skin.inverseBindMatrices && typeof skin.inverseBindMatrices.length === "number" ? skin.inverseBindMatrices : null;
      var ent: any = { skin: skin, joints: joints, out: [] as Float32Array[], ibm: [] as Float32Array[], buffer: buffer };
      for (var j = 0; j < count; j++) {
        ent.out.push(buffer.subarray(j * 16, j * 16 + 16));
        ent.ibm.push(ibm && j * 16 + 16 <= ibm.length ? ibm.subarray(j * 16, j * 16 + 16) : SCENE_IDENTITY_MAT4);
      }
      st.skins.push(ent);
    }
    return st;
  }

  // Blend the channel value in st.v into the node's pose, with the layer weight
  // in st.buf[3]. The first layer to touch a property copies it; later layers
  // blend by running weight exactly as sceneAnimBlendValue does. Weights travel
  // through st.buf so no double crosses a call boundary.
  function sceneClipBlendChannel(st: any, node: number, code: number) {
    var bit = 1 << code, wi = node * 3 + code;
    var width = code === 1 ? 4 : 3;
    var store = code === 0 ? st.t : code === 1 ? st.r : st.s;
    var off = node * width;
    var weight = st.buf[3];
    if (!(st.flags[node] & bit)) {
      for (var v = 0; v < width; v++) store[off + v] = st.v[v];
      st.wt[wi] = weight;
      st.flags[node] |= bit;
      return;
    }
    var tw = weight / (st.wt[wi] + weight);
    st.wt[wi] += weight;
    if (code === 1) {
      st.buf[2] = tw;
      sceneClipSlerpF64(st.q, store, off, st.v, 0, st.buf, 2);
      for (var qv = 0; qv < 4; qv++) store[off + qv] = st.q[qv];
    } else {
      for (var lv = 0; lv < 3; lv++) store[off + lv] = store[off + lv] + (st.v[lv] - store[off + lv]) * tw;
    }
  }

  // options: { nodes, skins, rootNodes, rootMotion }. clips is the model's own
  // cloned clip list: a sampler writes channel._lastIndex, so clips must not be
  // shared with another sampler or mixer that depends on that cache.
  function sceneCreateClipSampler(clips: any[], options: any) {
    var opts = options || {};
    var nodes = Array.isArray(opts.nodes) ? opts.nodes : [];
    var rootSet = opts.rootMotion === true ? null : sceneClipRootNodes(nodes, opts.skins, opts.rootNodes);
    var byName = new Map<string, any>();
    for (var ci = 0; ci < clips.length; ci++) {
      if (clips[ci] && clips[ci].name && !byName.has(clips[ci].name)) byName.set(clips[ci].name, clips[ci]);
    }
    var missing = new Set<string>();
    var results = new Map<string, any>();   // channel key -> blend slot, rewritten each call
    var slots: any[] = [];             // slot pool, reused across calls
    var entries: any[] = [];           // pose entry pool, reused across calls
    var warned = missing;

    function resolve(layer: any) {
      if (layer.name) {
        var named = byName.get(layer.name);
        if (!named && !warned.has("n:" + layer.name)) { warned.add("n:" + layer.name); console.warn("[gosx] sampled animation clip not found:", layer.name); }
        return named || null;
      }
      var indexed = clips[layer.index];
      if (!indexed && !warned.has("i:" + layer.index)) { warned.add("i:" + layer.index); console.warn("[gosx] sampled animation clip index not found:", layer.index); }
      return indexed || null;
    }

    function sample(layers: any[], count: number, output: Map<any, any>) {
      results.clear();
      var used = 0;
      var n = Math.min(count, layers.length);
      for (var li = 0; li < n; li++) {
        var layer = layers[li];
        var weight = layer.weight;
        if (!(weight > 0)) continue;
        var clip = resolve(layer);
        if (!clip || !clip.channels) continue;
        var time = sceneAnimationSampleTime(layer.time, clip.duration, layer.loop);
        for (var c = 0; c < clip.channels.length; c++) {
          var channel = clip.channels[c];
          var target = sceneClipChannelTarget(channel);
          var key = channel._skey;
          if (key === undefined) key = channel._skey = target + ":" + channel.property;
          sceneAnimSeekChannel(channel, time);
          var value = sceneAnimInterpolateChannel(channel, time);
          var slot = results.get(key);
          if (!slot) {
            slot = slots[used];
            if (!slot) slot = slots[used] = { targetID: 0, property: "", value: [], totalWeight: 0 };
            used++;
            var width = channel.property === "rotation" ? 4 : sceneAnimChannelWidth(channel);
            slot.targetID = target;
            slot.property = channel.property;
            slot.value.length = width;
            for (var v = 0; v < width; v++) slot.value[v] = value[v];
            slot.totalWeight = weight;
            results.set(key, slot);
          } else {
            sceneAnimBlendValue(slot, value, weight, channel.property);
          }
        }
      }
      var entryCount = 0;
      results.forEach(function(slot: any) {
        if (rootSet && slot.property === "translation" && rootSet.has(slot.targetID)) {
          // The simulation owns position: keep the authored horizontal offset.
          var node = nodes[slot.targetID];
          slot.value[0] = sceneClipAuthoredHorizontal(node, 0);
          slot.value[2] = sceneClipAuthoredHorizontal(node, 2);
        }
        var entry = output.get(slot.targetID);
        if (!entry) {
          entry = entries[entryCount];
          if (!entry) entry = entries[entryCount] = {};
          else { entry.translation = entry.rotation = entry.scale = entry.weights = undefined; }
          entryCount++;
          output.set(slot.targetID, entry);
        }
        entry[slot.property] = slot.value;
      });
      return output;
    }

    var poseState: any = null;

    // pose evaluates the held layers straight into the model's skin joint
    // matrices without allocating. It returns false (and writes nothing) for a
    // model with morph or rigid node-animation targets, which keep the general
    // path through sample() and sceneAnimBuildNodeTransforms.
    function pose(record: any) {
      if ((record.morphTargets && record.morphTargets.length > 0) || (record.nodeAnimTargets && record.nodeAnimTargets.length > 0)) return false;
      var st = poseState || (poseState = sceneClipBuildPoseState(nodes, record.rootNodes, record.skins));
      var layers = record.sampledLayers, count = Math.min(record.sampledLayerCount, layers.length);
      st.flags.fill(0);
      for (var li = 0; li < count; li++) {
        var layer = layers[li];
        var weight = layer.weight;
        if (!(weight > 0)) continue;
        var clip = resolve(layer);
        if (!clip || !clip.channels) continue;
        // Same rule as sceneAnimationSampleTime, written inline so no double
        // crosses a call boundary: negative or non-finite time is 0, a looping
        // layer wraps, a once layer clamps, a zero-length clip shows key 0.
        var rawTime = layer.time, duration = clip.duration;
        if (!Number.isFinite(rawTime) || rawTime < 0) rawTime = 0;
        st.buf[0] = duration > 0 ? (layer.loop ? rawTime % duration : Math.min(rawTime, duration)) : 0;
        st.buf[3] = weight;
        var channels = clip.channels;
        for (var c = 0; c < channels.length; c++) {
          var channel = channels[c];
          var code = channel._pc;
          if (code === undefined) {
            code = channel.property === "translation" ? 0 : channel.property === "rotation" ? 1 : channel.property === "scale" ? 2 : -1;
            var tn = sceneClipChannelTarget(channel);
            channel._pc = code = (code >= 0 && Number.isInteger(tn) && tn >= 0 && tn < st.n) ? code : -1;
            channel._tn = tn;
          }
          if (code < 0) continue;
          var node = channel._tn;
          sceneClipEvalChannel(channel, st.buf, st.v);
          sceneClipBlendChannel(st, node, code);
        }
      }
      if (rootSet) {
        for (var ri = 0; ri < st.order.length; ri++) {
          var rn = st.order[ri];
          if ((st.flags[rn] & 1) && rootSet.has(rn)) {
            st.t[rn * 3] = st.authT[rn * 3];
            st.t[rn * 3 + 2] = st.authT[rn * 3 + 2];
          }
        }
      }
      var root = record.rootTransform || null;
      for (var oi = 0; oi < st.order.length; oi++) {
        var ni = st.order[oi], f = st.flags[ni];
        var local = st.local;
        if (f === 0 && st.matrix[ni]) {
          local.set(st.matrix[ni]);
        } else {
          for (var a = 0; a < 3; a++) {
            st.tS[a] = (f & 1) ? st.t[ni * 3 + a] : st.authT[ni * 3 + a];
            st.sS[a] = (f & 4) ? st.s[ni * 3 + a] : st.authS[ni * 3 + a];
          }
          for (var b = 0; b < 4; b++) st.rS[b] = (f & 2) ? st.r[ni * 4 + b] : st.authR[ni * 4 + b];
          sceneTRSToMat4Into(local, st.tS, st.rS, st.sS);
        }
        var pi = st.parent[ni];
        if (pi >= 0) sceneMat4MultiplyInto(st.world[ni], st.world[pi], local);
        else if (root) sceneMat4MultiplyInto(st.world[ni], root, local);
        else st.world[ni].set(local);
      }
      for (var si = 0; si < st.skins.length; si++) {
        var ent = st.skins[si];
        for (var j = 0; j < ent.joints.length; j++) {
          var jn = ent.joints[j];
          var w = jn >= 0 && jn < st.n && st.reached[jn] ? st.world[jn] : SCENE_IDENTITY_MAT4;
          sceneMat4MultiplyInto(ent.out[j], w, ent.ibm[j]);
        }
        if (ent.skin) ent.skin.jointMatrices = ent.buffer;
      }
      return true;
    }

    return { sample: sample, pose: pose };
  }

  // Sampled models (Model.ClipSampled) have no mixer and no clock. The caller
  // registers them in _modelAnimations only so a live transform patch
  // (poseDirty) re-skins them; isModelPlaying is false for them, so no frame
  // ever advances their time.
  function sceneConfigureSampledRecord(state: any, record: any, clips: any[], instanceModel: any) {
    record.clipSampler = sceneCreateClipSampler(clips, {
      nodes: record.nodes,
      skins: record.skins,
      rootNodes: record.rootNodes,
      rootMotion: instanceModel.rootMotion === true,
    });
    // The layer holder outlives the record: a scene replacement re-hydrates
    // models, and the new record re-poses from the last frame the app sent
    // instead of showing the bind pose until the next frame arrives.
    var holder = sceneClipHolder(state, record.id);
    record.sampledLayers = holder.layers;
    record.sampledLayerCount = holder.count;
  }

  // One holder per model ID: four reusable layer objects and a count. Frames
  // are copied into it, so applying a frame allocates nothing.
  function sceneClipHolder(state: any, id: string) {
    var map = state && state._clipFrameLayers;
    var holder = map && map.get(id);
    if (!holder) {
      holder = { layers: [] as any[], count: 0 };
      for (var i = 0; i < 4; i++) holder.layers.push({ name: "", index: 0, time: 0, weight: 0, loop: false });
      if (map) map.set(id, holder);
    }
    return holder;
  }

  // sceneApplyClipFrame applies a decoded GSP4 ClipFrame: [{ id, layers }].
  // Each model's layers are copied over its previous layers (no history is
  // kept) and the model is re-skinned immediately through applyPose(record, 0,
  // false). Validation happens before any write, so a rejected frame changes
  // nothing. In steady state this allocates nothing.
  function sceneApplyClipFrame(state: any, models: any, applyPose: any) {
    if (!state || !models || typeof models.length !== "number") return { applied: false, reason: "invalid-frame" };
    if (!(state._clipFrameLayers instanceof Map)) state._clipFrameLayers = new Map();
    var pending = Boolean(state._modelHydrationPromise || state._modelHydrationUncommitted);
    var skins = Array.isArray(state._modelSkins) ? state._modelSkins : [];
    var index = state._clipRecordIndex;
    if (!index || index.skins !== skins || index.length !== skins.length) {
      index = { skins: skins, length: skins.length, byID: new Map<any, any>(), found: [] as any[] };
      for (var i = 0; i < skins.length; i++) {
        if (skins[i] && skins[i].clipSampler && skins[i].id) index.byID.set(skins[i].id, skins[i]);
      }
      state._clipRecordIndex = index;
    }
    var found = index.found;
    found.length = 0;
    for (var m = 0; m < models.length; m++) {
      var record = index.byID.get(models[m].id);
      if (!record && !pending) return { applied: false, reason: "unknown-model", id: models[m].id };
      found.push(record || null);
    }
    for (var w = 0; w < models.length; w++) {
      var layers = models[w].layers;
      var holder = sceneClipHolder(state, models[w].id);
      for (var l = 0; l < layers.length; l++) {
        var src = layers[l], dst = holder.layers[l];
        dst.name = src.name; dst.index = src.index; dst.time = src.time; dst.weight = src.weight; dst.loop = src.loop;
      }
      holder.count = layers.length;
      var target = found[w];
      if (!target) continue;
      target.sampledLayers = holder.layers;
      target.sampledLayerCount = holder.count;
      applyPose(target, 0, false);
    }
    return SCENE_CLIP_APPLIED[pending ? 1 : 0];
  }
  var SCENE_CLIP_APPLIED = [{ applied: true, pending: false }, { applied: true, pending: true }];

  // Model playback control belongs to the animation authority. Core hydration
  // owns records and geometry; these adapters only drive their existing mixers.
  const sceneModelAnimationControlFields = ["loop", "animationSpeed", "animationWeight", "animationFadeInMS", "animationFadeOutMS"];

  function sceneAnimationOwns(source: any, key: string): boolean {
    return Boolean(source && Object.prototype.hasOwnProperty.call(source, key));
  }

  function sceneModelAnimationValue(model: any, key: string, fallback: number, divisor = 1) {
    if (!sceneAnimationOwns(model, key)) return fallback;
    const value = Number(model[key]);
    return Number.isFinite(value) ? Math.max(0, value) / divisor : fallback;
  }

  // Live control patches are copied into record.model before playback. Resolve
  // that one current model instead of layering the identical patch twice.
  function sceneModelAnimationPlayOptions(model: any, fadeIn = 0) {
    return {
      loop: !sceneAnimationOwns(model, "loop") || model.loop !== false,
      speed: sceneModelAnimationValue(model, "animationSpeed", 1),
      weight: sceneModelAnimationValue(model, "animationWeight", 1),
      fadeIn: sceneModelAnimationValue(model, "animationFadeInMS", fadeIn, 1000),
    };
  }

  // Route a clip play through the active mixer. opts is the JS-mixer options
  // shape ({loop, speed, weight, fadeIn}); the WASM mixer takes the same values
  // as positional arguments.
  function sceneModelRecordPlay(record: any, name: string, options: ReturnType<typeof sceneModelAnimationPlayOptions>) {
    if (record && record.wasmMixerActive) {
      if (typeof window !== "undefined" && typeof window.__gosx_motion_mixer_play === "function") {
        window.__gosx_motion_mixer_play(
          record.wasmMixer,
          name,
          options.fadeIn, options.loop, options.speed, options.weight
        );
      }
      return;
    }
    if (record && record.mixer) {
      record.mixer.play(name, options);
    }
  }

  // Route a clip stop through the active mixer. opts is the JS-mixer options
  // shape ({fadeOut}); the WASM mixer takes fadeOut positionally.
  function sceneModelRecordStop(record: any, name: string, fadeOut: number) {
    if (record && record.wasmMixerActive) {
      if (typeof window !== "undefined" && typeof window.__gosx_motion_mixer_stop === "function") {
        window.__gosx_motion_mixer_stop(record.wasmMixer, name, fadeOut);
      }
    } else if (record && record.mixer) {
      record.mixer.stop(name, { fadeOut });
    } else {
      return;
    }
    // One final pose tick restores morph and node defaults after stopping.
    if ((record.morphTargets && record.morphTargets.length > 0)
      || (record.nodeAnimTargets && record.nodeAnimTargets.length > 0)) {
      record.poseDirty = true;
    }
  }

  // Whether a named clip is playing on the record's active mixer, routed to the
  // WASM mixer when active (P4-M3) and the JS mixer otherwise.
  function sceneModelRecordWasPlaying(record: any, name: string) {
    if (!record || !name) {
      return false;
    }
    if (record.wasmMixerActive) {
      return Boolean(
        typeof window !== "undefined" &&
        typeof window.__gosx_motion_mixer_is_playing === "function" &&
        window.__gosx_motion_mixer_is_playing(record.wasmMixer, name)
      );
    }
    return Boolean(record.mixer && record.mixer.isPlaying(name));
  }

  // Whether a record's currently-selected animation is playing.
  function sceneModelRecordIsPlaying(record: any) {
    return record ? sceneModelRecordWasPlaying(record, record.animation) : false;
  }

  function sceneInitializeModelPlayback(record: any, model: any) {
    const animation = typeof model.animation === "string" ? model.animation.trim() : "";
    if (!animation) return;
    sceneModelRecordPlay(record, animation, sceneModelAnimationPlayOptions(model));
    if (sceneModelRecordWasPlaying(record, animation)) {
      record.animation = animation;
      record.animationSeq = typeof model.animationSeq === "string" ? model.animationSeq : "";
    }
  }

  function sceneApplyModelLiveAnimation(record: any, patch: any) {
    if (!record || (!record.mixer && !record.wasmMixerActive) || (!patch || typeof patch !== "object" || Array.isArray(patch) || ArrayBuffer.isView(patch))) {
      return false;
    }
    const hasAnimation = sceneAnimationOwns(patch, "animation");
    const hasControls = sceneModelAnimationControlFields.some(key => sceneAnimationOwns(patch, key));
    if (!hasAnimation && !hasControls) {
      return false;
    }
    const animation = hasAnimation
      ? (typeof patch.animation === "string" ? patch.animation.trim() : "")
      : record.animation;
    const hasSeq = sceneAnimationOwns(patch, "animationSeq");
    const animationSeq = hasSeq ? String(patch.animationSeq == null ? "" : patch.animationSeq) : "";
    const replay = Boolean(hasSeq && animationSeq && record.animation === animation && record.animationSeq !== animationSeq);
    if (record.model) {
      for (const key of sceneModelAnimationControlFields) {
        if (sceneAnimationOwns(patch, key)) record.model[key] = patch[key];
      }
    }
    const model = record.model || patch;
    if (!animation) {
      if (record.animation && sceneModelRecordIsPlaying(record)) {
        const fadeOut = sceneModelAnimationValue(model, "animationFadeOutMS", 0.05, 1000);
        sceneModelRecordStop(record, record.animation, fadeOut);
        if (fadeOut <= 0) {
          record.animation = "";
        }
      }
      record.animationSeq = animationSeq;
      record.poseDirty = true;
      return true;
    }
    if (record.animation === animation && sceneModelRecordIsPlaying(record) && !replay) {
      if (hasControls) {
        sceneModelRecordPlay(record, animation, sceneModelAnimationPlayOptions(model));
        record.poseDirty = true;
        return true;
      }
      return false;
    }
    if (record.animation && sceneModelRecordIsPlaying(record)) {
      sceneModelRecordStop(record, record.animation, sceneModelAnimationValue(model, "animationFadeOutMS", replay ? 0 : 0.05, 1000));
    }
    sceneModelRecordPlay(record, animation, sceneModelAnimationPlayOptions(model, replay ? 0 : 0.04));
    if (!sceneModelRecordWasPlaying(record, animation)) {
      return false;
    }
    record.animation = animation;
    record.animationSeq = animationSeq;
    record.poseDirty = true;
    return true;
  }

  // Publish the animation API onto window for the legacy monolithic
  // bootstrap.js bundle that inlines this source. The lazy chunk suffix
  // extends this same API with crowd helpers after their constants initialize.
  if (typeof window !== "undefined") {
    window.__gosx_scene3d_animation_api = {
      createMixer: createSceneAnimationMixer,
      initializeModelPlayback: sceneInitializeModelPlayback,
      isModelPlaying: sceneModelRecordIsPlaying,
      applyModelAnimation: sceneApplyModelLiveAnimation,
      buildNodeTransforms: sceneAnimBuildNodeTransforms,
      computeJointMatrices: sceneAnimComputeJointMatrices,
      wasmClipJSON: sceneAnimWasmClipJSON,
      wasmDecodePose: sceneAnimWasmDecodePose,
      createClipSampler: sceneCreateClipSampler,
      configureSampledRecord: sceneConfigureSampledRecord,
      applyClipFrame: sceneApplyClipFrame,
    };
    window.__gosx_scene3d_animation_loaded = true;
  }

  // Immutable CPU palettes are owned by decoded assets, never actor identities.
  const sceneCrowdAtlasCache = new WeakMap();
  let sceneCrowdAtlasSequence = 0;
  // Explicit CPU poses and scene-clock motion sample the same clip clock and
  // palette rows. Keep the output caller-owned on both paths.
  function sceneAnimationSampleTime(time: number, duration: number, loop: boolean) {
    if (!Number.isFinite(time) || time < 0) time = 0;
    return duration > 0 ? (loop ? time % duration : Math.min(time, duration)) : 0;
  }

  function sceneCrowdWritePoseRows(out: Float32Array, start: number, segments: number, duration: number, time: number, loop: boolean) {
    const sampled = sceneAnimationSampleTime(time, duration, loop);
    const frame = duration > 0 ? sampled / duration * segments : 0;
    const a = Math.min(segments, Math.floor(frame));
    out[0] = start + a;
    out[1] = start + Math.min(segments, a + 1);
    out[2] = frame - a;
    return out;
  }

  function sceneCrowdPoseRows(atlas, pose, out) {
    out = out || new Float32Array(3);
    const clip = atlas.clips.get(pose && pose.animation || "");
    if (!clip) {
      const name = pose && pose.animation;
      if (name && !atlas.missingClips.has(name)) { atlas.missingClips.add(name); console.warn("[gosx] crowd animation clip not found:", name); }
      out[0] = out[1] = out[2] = 0; return out;
    }
    return sceneCrowdWritePoseRows(out, clip.start, clip.segments, clip.duration, Number(pose.animationTime), pose.animationLoop);
  }

  function sceneCrowdIncludeBoundPoint(bounds, x, y, z) {
    if (!Number.isFinite(x) || !Number.isFinite(y) || !Number.isFinite(z)) throw new Error("nonfinite crowd bound");
    bounds.minX = Math.min(bounds.minX, x); bounds.maxX = Math.max(bounds.maxX, x);
    bounds.minY = Math.min(bounds.minY, y); bounds.maxY = Math.max(bounds.maxY, y);
    bounds.minZ = Math.min(bounds.minZ, z); bounds.maxZ = Math.max(bounds.maxZ, z);
  }

  function sceneCrowdChargeBoundsWork(budget, work) {
    if (!budget) return;
    budget.work += work;
    if (!Number.isSafeInteger(budget.work) || budget.work > 32 * 1024 * 1024) throw new Error("crowd bounds preprocessing budget exceeded");
  }

  function sceneCrowdPrimitiveBounds(vertices, jointCount, data, width, height, weights, budget = null) {
    const bounds = { minX: Infinity, minY: Infinity, minZ: Infinity, maxX: -Infinity, maxY: -Infinity, maxZ: -Infinity };
    const jointBounds = new Array(jointCount);
    const activeJoints = [];
    let maxWeightSumError = 0;
    let maxTransformTerms = 0;
    for (let i = 0; i < vertices.count; i++) {
      const x = vertices.positions[i * 3], y = vertices.positions[i * 3 + 1], z = vertices.positions[i * 3 + 2];
      if (!Number.isFinite(x) || !Number.isFinite(y) || !Number.isFinite(z)) throw new Error("nonfinite crowd vertex");
      let total = 0;
      for (let k = 0; k < 4; k++) total += weights[i * 4 + k];
      if (!total) {
        sceneCrowdIncludeBoundPoint(bounds, x, y, z);
        maxTransformTerms = Math.max(maxTransformTerms, Math.abs(x), Math.abs(y), Math.abs(z));
        continue;
      }
      maxWeightSumError = Math.max(maxWeightSumError, Math.abs(total - 1));
      for (let k = 0; k < 4; k++) {
        if (!weights[i * 4 + k]) continue;
        const joint = vertices.joints[i * 4 + k];
        let influenced = jointBounds[joint];
        if (!influenced) {
          influenced = jointBounds[joint] = { minX: Infinity, minY: Infinity, minZ: Infinity, maxX: -Infinity, maxY: -Infinity, maxZ: -Infinity };
          activeJoints.push(joint);
        }
        sceneCrowdIncludeBoundPoint(influenced, x, y, z);
      }
    }
    // The conservative algorithm scans vertices once, then transforms eight
    // influence-box corners per active joint/row, never every vertex/row.
    sceneCrowdChargeBoundsWork(budget, height * activeJoints.length * 8);
    for (let row = 0; row < height; row++) for (const joint of activeJoints) {
      const influenced = jointBounds[joint];
      const o = row * width * 4 + joint * 16;
      for (let corner = 0; corner < 8; corner++) {
        const x = corner & 1 ? influenced.maxX : influenced.minX;
        const y = corner & 2 ? influenced.maxY : influenced.minY;
        const z = corner & 4 ? influenced.maxZ : influenced.minZ;
        const px = data[o] * x + data[o + 4] * y + data[o + 8] * z + data[o + 12];
        const py = data[o + 1] * x + data[o + 5] * y + data[o + 9] * z + data[o + 13];
        const pz = data[o + 2] * x + data[o + 6] * y + data[o + 10] * z + data[o + 14];
        const termsX = Math.abs(data[o] * x) + Math.abs(data[o + 4] * y) + Math.abs(data[o + 8] * z) + Math.abs(data[o + 12]);
        const termsY = Math.abs(data[o + 1] * x) + Math.abs(data[o + 5] * y) + Math.abs(data[o + 9] * z) + Math.abs(data[o + 13]);
        const termsZ = Math.abs(data[o + 2] * x) + Math.abs(data[o + 6] * y) + Math.abs(data[o + 10] * z) + Math.abs(data[o + 14]);
        if (!Number.isFinite(termsX) || !Number.isFinite(termsY) || !Number.isFinite(termsZ)) throw new Error("nonfinite crowd bound");
        maxTransformTerms = Math.max(maxTransformTerms, termsX, termsY, termsZ);
        sceneCrowdIncludeBoundPoint(bounds, px, py, pz);
      }
    }
    if (Number.isFinite(bounds.minX)) {
      // Stored Float32 weights may sum just above or below one. Include that
      // drift and conservative float32 transform/interpolation error relative
      // to absolute coordinates, including translated meshes with tiny extent.
      const magnitude = Math.max(1, Math.abs(bounds.minX), Math.abs(bounds.minY), Math.abs(bounds.minZ), Math.abs(bounds.maxX), Math.abs(bounds.maxY), Math.abs(bounds.maxZ));
      const margin = magnitude * maxWeightSumError + Math.max(magnitude, maxTransformTerms) * 32 * 1.1920928955078125e-7;
      if (!Number.isFinite(margin)) throw new Error("nonfinite crowd bound");
      bounds.minX -= margin; bounds.minY -= margin; bounds.minZ -= margin;
      bounds.maxX += margin; bounds.maxY += margin; bounds.maxZ += margin;
      if (![bounds.minX, bounds.minY, bounds.minZ, bounds.maxX, bounds.maxY, bounds.maxZ].every(Number.isFinite)) throw new Error("nonfinite crowd bound");
    }
    return bounds;
  }

  // Palette construction and explicit playback materialize the same channel
  // samples. Copy scratch values before the next channel can overwrite them.
  function sceneSampleAnimationChannels(channels: any[], time: number, output: Map<number, any>, nodes?: any[]) {
    for (const channel of channels) {
      const node = channel.targetNode != null ? channel.targetNode : channel.targetID;
      if (nodes && (!Number.isInteger(node) || !nodes[node])) throw new Error("invalid crowd animation node");
      let entry = output.get(node);
      if (!entry) { entry = {}; output.set(node, entry); }
      entry[channel.property] = Array.from(sceneAnimInterpolateChannel(channel, time));
    }
    return output;
  }

  function sceneBuildCrowdAtlas(asset, skinIndex) {
    let cache = sceneCrowdAtlasCache.get(asset);
    if (!cache) { cache = new Map(); sceneCrowdAtlasCache.set(asset, cache); }
    if (cache.has(skinIndex)) return cache.get(skinIndex);
    const source = asset.skins && asset.skins[skinIndex];
    const joints = source && source.joints;
    if (!joints || !joints.length || joints.length > 64 || !Array.isArray(asset.nodes)) throw new Error("unsupported crowd skeleton");
    const skin = { joints: joints.slice(), inverseBindMatrices: source.inverseBindMatrices };
    const width = joints.length * 4;
    const clips = new Map();
    let height = 1;
    for (const clip of asset.animations || []) {
      if (!clip.name || clips.has(clip.name) || !Number.isFinite(clip.duration) || clip.duration < 0) throw new Error("invalid crowd clip");
      if ((clip.channels || []).some(ch => !["rotation", "translation", "scale"].includes(ch.property))) throw new Error("unsupported crowd animation channel");
      const segments = clip.duration > 0 ? Math.max(1, Math.ceil(clip.duration * 30)) : 0;
      clips.set(clip.name, { start: height, segments, duration: clip.duration });
      height += segments + 1;
    }
    if (height > 4096 || height * asset.nodes.length > 500000 || width * height * 16 > 16 * 1024 * 1024) throw new Error("crowd palette budget exceeded");
    const data = new Float32Array(width * height * 4);
    function sample(channels, time, row) {
      const pose = sceneSampleAnimationChannels(channels, time, new Map(), asset.nodes);
      // @ts-expect-error TS2554 -- rootNodes is optional for decoded asset sampling.
      const matrices = sceneAnimComputeJointMatrices(skin, sceneAnimBuildNodeTransforms(asset.nodes, pose, null));
      for (const value of matrices) if (!Number.isFinite(value)) throw new Error("nonfinite crowd matrix");
      data.set(matrices, row * width * 4);
    }
    sample([], 0, 0);
    for (const sourceClip of asset.animations || []) {
      const clip = clips.get(sourceClip.name);
      // Channel keyframe cursors are private to palette construction.
      const channels = sourceClip.channels.map(ch => Object.assign({}, ch, { _lastIndex: 0 }));
      for (let i = 0; i <= clip.segments; i++) sample(channels, clip.segments ? clip.duration * i / clip.segments : 0, clip.start + i);
    }
    const primitives = new Map();
    const boundsBudget = { work: 0 };
    for (const object of asset.objects || []) {
      if (object.skinIndex !== skinIndex) continue;
      const v = object.vertices;
      if (!v || !Number.isSafeInteger(v.count) || v.count < 0 || !v.positions || !v.joints || !v.weights || v.positions.length < v.count * 3 || v.joints.length < v.count * 4 || v.weights.length < v.count * 4) throw new Error("invalid crowd bind streams");
      if (primitives.has(v)) continue;
      // Bound all four four-influence loops: validation/sum, normalization,
      // normalized sum/drift, and influence boxes. Charge before stream copies;
      // palette construction has its own unchanged limits.
      sceneCrowdChargeBoundsWork(boundsBudget, v.count * 4);
      const weights = new Float32Array(v.weights);
      for (let i = 0; i < v.count; i++) {
        let sum = 0;
        for (let k = 0; k < 4; k++) {
          const w = weights[i * 4 + k], joint = v.joints[i * 4 + k];
          if (!Number.isFinite(w) || w < 0 || w > 0 && (!Number.isInteger(joint) || joint < 0 || joint >= joints.length)) throw new Error("invalid crowd skin influence");
          sum += w;
        }
        if (sum > 0) for (let k = 0; k < 4; k++) weights[i * 4 + k] /= sum;
      }
      const bounds = sceneCrowdPrimitiveBounds(v, joints.length, data, width, height, weights, boundsBudget);
      // Original streams remain immutable; only normalized weights are copied.
      primitives.set(v, { vertices: Object.assign({}, v, { weights, immutable: true, revision: 0, _rigidPool: true }), bounds });
    }
    const atlas = { id: ++sceneCrowdAtlasSequence, width, height, data, clips, primitives, skinIndex, missingClips: new Set() };
    cache.set(skinIndex, atlas);
    return atlas;
  }

  // Explicit-clock compatibility path for unsupported crowd assets/backends.
  // It deliberately uses the existing channel sampler (including morph weights).
  function sceneSampleExplicitAnimation(clips, pose, output) {
    output.clear();
    const clip = clips.find(c => c.name === (pose && pose.animation));
    if (!clip) return output;
    const t = sceneAnimationSampleTime(Number(pose.animationTime), clip.duration, pose.animationLoop);
    return sceneSampleAnimationChannels(clip.channels, t, output);
  }

  // ---------------------------------------------------------------------------
  // GPU-driven crowd motion — per-instance motion keys and animation state the
  // vertex shader evaluates from a single scene-clock uniform (uNow), instead
  // of the CPU recomputing a transform matrix and atlas pose rows for every
  // instance on every rendered frame. See scene/motion_frame.go (MotionFrame /
  // GSP3) for the wire format and command-runtime.ts for the decode/apply
  // path. webgl.ts's SCENE_CROWD_SKIN_MOTION_GLSL mirrors every function below
  // line for line; a change to one requires the other, and
  // scene3d-crowd-motion.test.mjs pins the two together.
  // ---------------------------------------------------------------------------

  // uNow uses the SAME clock as every other Scene3D auto-uniform
  // (performance.now() / 1000 -- see webgl.ts's sceneSelenaFrameTime), so a
  // MotionFrame's TPrev/TNext/ClipStartTime are timestamps on that clock, not
  // a caller-defined epoch.
  //
  // Past TNext the shader keeps extrapolating at the prev->next velocity for
  // this many seconds before holding at the extrapolated endpoint. This
  // bridges one or two missed/late network snapshots at a 10-20Hz snapshot
  // rate (50-100ms apart) without letting a stalled sender run an instance
  // away indefinitely.
  const SCENE_CROWD_MOTION_EXTRAPOLATION_SECONDS = 0.25;

  // sceneCrowdMotionClipTable builds (and caches on the atlas) a small
  // per-clip table a GPU-motion crowd batch uploads as a texture: one RGBA
  // texel per clip, (startRow, segments, duration, 0). Row 0 is a reserved
  // "no clip" sentinel (duration 0), matching wire clip index 0 in GSP2/GSP3
  // (the empty-string dictionary entry) -- a caller with no animation gets
  // clip row 0 and the shader's bind-pose fallback, with no lookup miss.
  // Built once per atlas: an atlas's clip set never changes after
  // sceneBuildCrowdAtlas returns it, so later calls return the cached table.
  // @ts-ignore TS7006 -- untyped like every sibling function in this file's
  // crowd/animation surface (see scene3d/noimplicitany-baseline.json).
  // the expect-error form would report this directive unused under
  // tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent
  // either way.
  function sceneCrowdMotionClipTable(atlas) {
    let table = atlas.motionClipTable;
    if (table) return table;
    const names = Array.from(atlas.clips.keys());
    const count = names.length + 1;
    const data = new Float32Array(count * 4);
    const index = new Map();
    for (let i = 0; i < names.length; i++) {
      const clip = atlas.clips.get(names[i]);
      const row = i + 1;
      data[row * 4] = clip.start;
      data[row * 4 + 1] = clip.segments;
      data[row * 4 + 2] = clip.duration;
      index.set(names[i], row);
    }
    table = { count, data, index };
    atlas.motionClipTable = table;
    return table;
  }

  // sceneCrowdMotionClipIndex resolves a wire clip name to its atlas-local
  // clip-table row, building the table on first use. An unknown clip name
  // warns once (reusing atlas.missingClips, the same one-time-warn set
  // sceneCrowdPoseRows uses) and resolves to the reserved "no clip" row, so a
  // single bad name degrades that instance to a bind pose rather than
  // rejecting the whole frame.
  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function sceneCrowdMotionClipIndex(atlas, name) {
    if (!name) return 0;
    const table = sceneCrowdMotionClipTable(atlas);
    const row = table.index.get(name);
    if (row !== undefined) return row;
    if (!atlas.missingClips.has(name)) { atlas.missingClips.add(name); console.warn("[gosx] crowd motion clip not found:", name); }
    return 0;
  }

  // sceneCrowdMotionPoseRows is the JS mirror of
  // SCENE_CROWD_SKIN_MOTION_GLSL's gosxCrowdMotionPoseRows in webgl.ts: given
  // a clip table, a resolved clip-table row, and the animation state, it
  // derives the same two atlas rows and blend fraction sceneCrowdPoseRows
  // computes from an explicit animationTime -- except elapsed clip time comes
  // from (now - clipStartTime) * playbackRate rather than being handed in
  // directly. Kept in lockstep with the GLSL by scene3d-crowd-motion.test.mjs.
  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function sceneCrowdMotionPoseRows(clipTable, clipRow, clipStartTime, loop, rate, now, out) {
    out = out || new Float32Array(3);
    const row = Math.max(0, Math.min(clipTable.count - 1, Math.floor(clipRow) || 0));
    const data = clipTable.data;
    const start = data[row * 4], segments = data[row * 4 + 1], duration = data[row * 4 + 2];
    return sceneCrowdWritePoseRows(out, start, segments, duration, (now - clipStartTime) * rate, loop);
  }

  // sceneCrowdMotionAngleLerp is the JS mirror of
  // SCENE_CROWD_SKIN_MOTION_GLSL's gosxWrapAngleLerp: it interpolates an
  // angle (radians) from a to b along the SHORTER arc, so a character
  // spinning from a yaw of +3.0 to -3.0 rad turns through PI (a hair past
  // "straight around"), never the long way through 0.
  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function sceneCrowdMotionAngleLerp(a, b, t) {
    const twoPi = Math.PI * 2;
    let d = b - a;
    d = d - twoPi * Math.floor((d + Math.PI) / twoPi);
    return a + d * t;
  }

  // sceneCrowdMotionInterpolationFactor is the JS mirror of
  // SCENE_CROWD_SKIN_MOTION_GLSL's gosxMotionModel's `t` computation: 0 at or
  // before tPrev, linear across [tPrev, tNext), and past tNext an
  // extrapolated factor capped at extrapolationSeconds past tNext (then
  // held, since a factor computed once at the cap stays constant while `now`
  // keeps advancing beyond it -- callers must recompute per frame, and this
  // function is pure, so "held" here means the CALLER keeps passing a `now`
  // that yields the same capped factor). tNext <= tPrev (a single known
  // sample, prev===next) is degenerate but SAFE: mix()/
  // sceneCrowdMotionAngleLerp with equal prev/next endpoints return that
  // same value for ANY finite t, however large.
  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function sceneCrowdMotionInterpolationFactor(now, tPrev, tNext, extrapolationSeconds) {
    const span = Math.max(tNext - tPrev, 1e-6);
    if (now <= tPrev) return 0;
    if (now < tNext) return (now - tPrev) / span;
    return 1 + Math.min(now - tNext, extrapolationSeconds) / span;
  }

  // sceneCrowdMotionComposeInto builds the SAME column-major TRS matrix
  // sceneObjectModelMatrix's no-parent branch (10-runtime-scene-core.ts) and
  // 11-scene-math.ts's sceneEulerMatrixInto both build (Rz*Ry*Rx, each
  // column scaled by the matching scale component, translation in column 3)
  // -- the identical formula webgl.ts's gosxMotionModel GLSL also uses. It
  // is inlined here, not called from 11-scene-math.ts, because the
  // animation feature chunk (bootstrap-feature-scene3d-animation.js) does
  // not bundle that file: `go run . -closure` (make test-js) fails the
  // build if this chunk reads an identifier nothing it bundles declares.
  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function sceneCrowdMotionComposeInto(out, rx, ry, rz, sx, sy, sz, x, y, z) {
    const cx = Math.cos(rx), sinx = Math.sin(rx);
    const cy = Math.cos(ry), siny = Math.sin(ry);
    const cz = Math.cos(rz), sinz = Math.sin(rz);
    out[0] = cy * cz * sx; out[1] = cy * sinz * sx; out[2] = -siny * sx; out[3] = 0;
    out[4] = (sinx * siny * cz - cx * sinz) * sy; out[5] = (sinx * siny * sinz + cx * cz) * sy; out[6] = sinx * cy * sy; out[7] = 0;
    out[8] = (cx * siny * cz + sinx * sinz) * sz; out[9] = (cx * siny * sinz - sinx * cz) * sz; out[10] = cx * cy * sz; out[11] = 0;
    out[12] = x; out[13] = y; out[14] = z; out[15] = 1;
    return out;
  }

  // SCENE_CROWD_MOTION_* name the SCENE_CROWD_MOTION_RECORD_FLOATS float32
  // slots of one instance's GPU-motion record: webgl.ts's crowdMotion
  // instance-buffer stride, and the layout applyMountedMotionFrame in
  // command-runtime.ts writes through sceneCrowdMotionWriteRecord. Named
  // offsets, not magic numbers, at every read and write site.
  const SCENE_CROWD_MOTION_RECORD_FLOATS = 24;
  const SCENE_CROWD_MOTION_CLIP_LIMIT = 32;
  const SCENE_CROWD_MOTION_PREV_POS = 0, SCENE_CROWD_MOTION_PREV_ROT = 3, SCENE_CROWD_MOTION_PREV_SCALE = 6, SCENE_CROWD_MOTION_TPREV = 9;
  const SCENE_CROWD_MOTION_NEXT_POS = 10, SCENE_CROWD_MOTION_NEXT_ROT = 13, SCENE_CROWD_MOTION_NEXT_SCALE = 16, SCENE_CROWD_MOTION_TNEXT = 19;
  const SCENE_CROWD_MOTION_CLIP_ROW = 20, SCENE_CROWD_MOTION_CLIP_START = 21, SCENE_CROWD_MOTION_LOOP = 22, SCENE_CROWD_MOTION_RATE = 23;

  // sceneCrowdMotionTransformInto is the JS mirror of
  // SCENE_CROWD_SKIN_MOTION_GLSL's gosxMotionModel: it interpolates position,
  // per-axis rotation (shortest arc), and scale between an instance record's
  // prev/next keys at `now`, then composes the same TRS matrix
  // sceneObjectModelMatrix would build for that instant. `record` is one
  // instance's SCENE_CROWD_MOTION_RECORD_FLOATS-length Float32Array.
  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function sceneCrowdMotionTransformInto(out, record, now, extrapolationSeconds) {
    const tPrev = record[SCENE_CROWD_MOTION_TPREV], tNext = record[SCENE_CROWD_MOTION_TNEXT];
    const t = sceneCrowdMotionInterpolationFactor(now, tPrev, tNext, extrapolationSeconds);
    const pp = SCENE_CROWD_MOTION_PREV_POS, np = SCENE_CROWD_MOTION_NEXT_POS;
    const pr = SCENE_CROWD_MOTION_PREV_ROT, nr = SCENE_CROWD_MOTION_NEXT_ROT;
    const ps = SCENE_CROWD_MOTION_PREV_SCALE, ns = SCENE_CROWD_MOTION_NEXT_SCALE;
    const x = record[pp] + (record[np] - record[pp]) * t;
    const y = record[pp + 1] + (record[np + 1] - record[pp + 1]) * t;
    const z = record[pp + 2] + (record[np + 2] - record[pp + 2]) * t;
    const rx = sceneCrowdMotionAngleLerp(record[pr], record[nr], t);
    const ry = sceneCrowdMotionAngleLerp(record[pr + 1], record[nr + 1], t);
    const rz = sceneCrowdMotionAngleLerp(record[pr + 2], record[nr + 2], t);
    const sx = record[ps] + (record[ns] - record[ps]) * t;
    const sy = record[ps + 1] + (record[ns + 1] - record[ps + 1]) * t;
    const sz = record[ps + 2] + (record[ns + 2] - record[ps + 2]) * t;
    return sceneCrowdMotionComposeInto(out, rx, ry, rz, sx, sy, sz, x, y, z);
  }

  // sceneCrowdMotionPoseRowsFromRecord reads the animation-state slots
  // straight out of an instance record -- the same slots
  // SCENE_CROWD_SKIN_MOTION_GLSL's a_animState attribute carries -- so a
  // caller (a test, or a CPU fallback) never duplicates the field layout.
  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function sceneCrowdMotionPoseRowsFromRecord(clipTable, record, now, out) {
    return sceneCrowdMotionPoseRows(
      clipTable, record[SCENE_CROWD_MOTION_CLIP_ROW], record[SCENE_CROWD_MOTION_CLIP_START],
      record[SCENE_CROWD_MOTION_LOOP] > 0.5, record[SCENE_CROWD_MOTION_RATE], now, out);
  }

  // sceneCrowdMotionWriteRecord fills `record` (a caller-owned, reused
  // Float32Array(SCENE_CROWD_MOTION_RECORD_FLOATS)) from one decoded GSP3
  // instance (see command-runtime.ts's decodeMotionFrame) and its resolved
  // atlas-local clip row. Pure and allocation-free on the hot (snapshot-rate)
  // path: applyMountedMotionFrame owns and reuses `record` across snapshots.
  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function sceneCrowdMotionWriteRecord(record, instance, clipRow) {
    record[SCENE_CROWD_MOTION_PREV_POS] = instance.prevX;
    record[SCENE_CROWD_MOTION_PREV_POS + 1] = instance.prevY;
    record[SCENE_CROWD_MOTION_PREV_POS + 2] = instance.prevZ;
    record[SCENE_CROWD_MOTION_PREV_ROT] = instance.prevRotationX;
    record[SCENE_CROWD_MOTION_PREV_ROT + 1] = instance.prevRotationY;
    record[SCENE_CROWD_MOTION_PREV_ROT + 2] = instance.prevRotationZ;
    record[SCENE_CROWD_MOTION_PREV_SCALE] = instance.prevScaleX;
    record[SCENE_CROWD_MOTION_PREV_SCALE + 1] = instance.prevScaleY;
    record[SCENE_CROWD_MOTION_PREV_SCALE + 2] = instance.prevScaleZ;
    record[SCENE_CROWD_MOTION_TPREV] = instance.tPrev;
    record[SCENE_CROWD_MOTION_NEXT_POS] = instance.nextX;
    record[SCENE_CROWD_MOTION_NEXT_POS + 1] = instance.nextY;
    record[SCENE_CROWD_MOTION_NEXT_POS + 2] = instance.nextZ;
    record[SCENE_CROWD_MOTION_NEXT_ROT] = instance.nextRotationX;
    record[SCENE_CROWD_MOTION_NEXT_ROT + 1] = instance.nextRotationY;
    record[SCENE_CROWD_MOTION_NEXT_ROT + 2] = instance.nextRotationZ;
    record[SCENE_CROWD_MOTION_NEXT_SCALE] = instance.nextScaleX;
    record[SCENE_CROWD_MOTION_NEXT_SCALE + 1] = instance.nextScaleY;
    record[SCENE_CROWD_MOTION_NEXT_SCALE + 2] = instance.nextScaleZ;
    record[SCENE_CROWD_MOTION_TNEXT] = instance.tNext;
    record[SCENE_CROWD_MOTION_CLIP_ROW] = clipRow;
    record[SCENE_CROWD_MOTION_CLIP_START] = instance.clipStartTime;
    record[SCENE_CROWD_MOTION_LOOP] = instance.animationLoop ? 1 : 0;
    record[SCENE_CROWD_MOTION_RATE] = instance.playbackRate;
    return record;
  }

  // sceneCrowdMotionLocalRadius returns a radius, centered on the model's
  // local origin, that contains localBounds (a primitive's bind-pose bounds
  // from sceneCrowdPrimitiveBounds, or the union of all of a model's
  // primitives) REGARDLESS of how that shape is rotated. Used to turn a
  // local AABB into a rotation-agnostic sweep radius for
  // sceneCrowdMotionSweptBoundsInto: an exact rotated AABB would need to
  // transform all eight corners by each candidate rotation, but the caller
  // needs a bound that stays valid across every rotation the GPU motion path
  // can interpolate through, not just the two sampled endpoints.
  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function sceneCrowdMotionLocalRadius(localBounds) {
    const cx = Math.max(Math.abs(localBounds.minX), Math.abs(localBounds.maxX));
    const cy = Math.max(Math.abs(localBounds.minY), Math.abs(localBounds.maxY));
    const cz = Math.max(Math.abs(localBounds.minZ), Math.abs(localBounds.maxZ));
    return Math.sqrt(cx * cx + cy * cy + cz * cz);
  }

  // sceneCrowdMotionSweptBoundsInto expands `bounds` (an object with
  // minX/minY/minZ/maxX/maxY/maxZ) to cover every world position and
  // orientation an instance's GPU motion interpolation (and brief
  // extrapolation past TNext) can produce, so a caller that culls or fits a
  // shadow frustum against `bounds` never clips a moving crowd instance
  // mid-interpolation. `localRadius` is
  // sceneCrowdMotionLocalRadius(primitive.bounds) for this instance's
  // geometry, precomputed once per instance. Conservative by construction:
  // it puts a sphere around the previous, next, and final extrapolated
  // positions. Its radius covers the largest scale at all three positions.
  // The extrapolation endpoint uses the same cap as the shader -- see
  // SCENE_CROWD_MOTION_EXTRAPOLATION_SECONDS. Position,
  // rotation (any axis, any angle -- a sphere is rotation-invariant), and
  // scale are all covered; this is deliberately a superset of the sampled
  // transforms' exact bounds, not a tight fit.
  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function sceneCrowdMotionSweptBoundsInto(bounds, record, localRadius) {
    const tPrev = record[SCENE_CROWD_MOTION_TPREV], tNext = record[SCENE_CROWD_MOTION_TNEXT];
    const span = Math.max(tNext - tPrev, 1e-6);
    const overshoot = SCENE_CROWD_MOTION_EXTRAPOLATION_SECONDS / span;
    const ps = SCENE_CROWD_MOTION_PREV_SCALE, ns = SCENE_CROWD_MOTION_NEXT_SCALE;
    let maxScale = 0;
    for (let i = 0; i < 3; i++) {
      const previous = record[ps + i], next = record[ns + i];
      maxScale = Math.max(maxScale, Math.abs(previous), Math.abs(next), Math.abs(next + (next - previous) * overshoot));
    }
    const radius = localRadius * maxScale;
    const pp = SCENE_CROWD_MOTION_PREV_POS, np = SCENE_CROWD_MOTION_NEXT_POS;
    const px = record[pp], py = record[pp + 1], pz = record[pp + 2];
    const nx = record[np], ny = record[np + 1], nz = record[np + 2];
    const ex = nx + (nx - px) * overshoot;
    const ey = ny + (ny - py) * overshoot;
    const ez = nz + (nz - pz) * overshoot;
    const minX = Math.min(px, nx, ex) - radius, maxX = Math.max(px, nx, ex) + radius;
    const minY = Math.min(py, ny, ey) - radius, maxY = Math.max(py, ny, ey) + radius;
    const minZ = Math.min(pz, nz, ez) - radius, maxZ = Math.max(pz, nz, ez) + radius;
    if (!Number.isFinite(minX) || !Number.isFinite(minY) || !Number.isFinite(minZ) ||
        !Number.isFinite(maxX) || !Number.isFinite(maxY) || !Number.isFinite(maxZ)) throw new Error("nonfinite crowd bound");
    bounds.minX = Math.min(bounds.minX, minX); bounds.maxX = Math.max(bounds.maxX, maxX);
    bounds.minY = Math.min(bounds.minY, minY); bounds.maxY = Math.max(bounds.maxY, maxY);
    bounds.minZ = Math.min(bounds.minZ, minZ); bounds.maxZ = Math.max(bounds.maxZ, maxZ);
    return bounds;
  }
