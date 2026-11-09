  // animation-rig.ts — humanoid bone mapping, rig resolution, and clip
  // retargeting for the animation graph chunk.
  //
  // Canonical bone names follow VRM 1.0 (hips, spine, chest, upperChest,
  // neck, head, leftUpperArm, ...). A name matcher maps the common skeleton
  // conventions onto them: Mixamo ("mixamorig:LeftUpLeg"), Rigify deform and
  // metarig bones ("DEF-thigh.L", "spine.003"), Unreal ("thigh_l",
  // "spine_01"), 3ds Max Biped ("Bip01 L Thigh"), Blender ".L/.R" rigs, and
  // short camel names ("lArm", "rShin"). Meshy auto-rigs use Mixamo-style
  // names without the prefix, which the same matcher reads.

  const RIG_BONES = [
    "hips", "spine", "chest", "upperChest", "neck", "head", "leftEye", "rightEye", "jaw",
    "leftUpperLeg", "leftLowerLeg", "leftFoot", "leftToes", "rightUpperLeg", "rightLowerLeg", "rightFoot", "rightToes",
    "leftShoulder", "leftUpperArm", "leftLowerArm", "leftHand", "rightShoulder", "rightUpperArm", "rightLowerArm", "rightHand",
  ];
  const RIG_FINGERS = ["Thumb", "Index", "Middle", "Ring", "Little"];
  const RIG_FINGER_SEGMENTS = [["Metacarpal", "Proximal", "Distal"], ["Proximal", "Intermediate", "Distal"]];
  for (const side of ["left", "right"]) {
    for (let f = 0; f < RIG_FINGERS.length; f++) {
      for (const seg of RIG_FINGER_SEGMENTS[f === 0 ? 0 : 1]) RIG_BONES.push(side + RIG_FINGERS[f] + seg);
    }
  }
  const RIG_BONE_SET = new Set(RIG_BONES);

  // Base-name synonyms after prefixes, side markers, separators and digits
  // are removed. "leg" is ambiguous (Mixamo LeftLeg is the shin; "lLeg" next
  // to "lShin" is the thigh) and resolves per side below.
  const RIG_SYNONYMS: Record<string, string> = {
    hips: "hips", hip: "hips", pelvis: "hips",
    neck: "neck", head: "head", eye: "Eye", jaw: "jaw",
    shoulder: "Shoulder", clavicle: "Shoulder", collar: "Shoulder", collarbone: "Shoulder",
    upperarm: "UpperArm", arm: "UpperArm", humerus: "UpperArm", uparm: "UpperArm",
    forearm: "LowerArm", lowerarm: "LowerArm", elbow: "LowerArm", lowarm: "LowerArm",
    hand: "Hand", wrist: "Hand",
    upperleg: "UpperLeg", upleg: "UpperLeg", thigh: "UpperLeg", femur: "UpperLeg",
    lowerleg: "LowerLeg", shin: "LowerLeg", calf: "LowerLeg", knee: "LowerLeg", tibia: "LowerLeg", lowleg: "LowerLeg",
    foot: "Foot", ankle: "Foot",
    toe: "Toes", toes: "Toes", toebase: "Toes", ball: "Toes",
  };
  const RIG_SPINE_WORDS = new Set(["spine", "torso", "abdomen", "body"]);
  const RIG_FINGER_WORDS: Record<string, number> = { thumb: 0, index: 1, middle: 2, ring: 3, pinky: 4, little: 4 };
  const RIG_IGNORE = /twist|roll|nub|helper|corrective|pole|target|metacarpal|palm|_?end$|top_?end|^ik(?![a-z])|(?<![a-z])ik$/;
  const RIG_CONTROL_PREFIX = /^(mch|ik|fk|ctrl|wgt|vis|tgt|pole)[-_.]/i;

  // Split one node name into side, base word, digit index and Blender
  // segment suffix.
  function rigParseName(raw: string) {
    let s = String(raw || "");
    const colon = s.lastIndexOf(":");
    if (colon >= 0) s = s.slice(colon + 1);
    s = s.replace(/^mixamorig\d*_/i, "");
    let def = false, control = false;
    const prefix = /^(def|org)[-_.]/i.exec(s);
    if (prefix) { def = prefix[1].toLowerCase() === "def"; s = s.slice(prefix[0].length); }
    else if (RIG_CONTROL_PREFIX.test(s)) control = true;
    s = s.replace(/^bip\d*[ _]/i, "");
    let seg = -1;
    const segment = /\.(\d{3})$/.exec(s);
    if (segment) { seg = parseInt(segment[1], 10); s = s.slice(0, -4); }
    let side = "";
    let m: RegExpExecArray | null;
    if ((m = /[._\- ](l|left)$/i.exec(s))) { side = "left"; s = s.slice(0, m.index); }
    else if ((m = /[._\- ](r|right)$/i.exec(s))) { side = "right"; s = s.slice(0, m.index); }
    else if ((m = /^(left|right)[._\- ]?/i.exec(s))) { side = m[1].toLowerCase(); s = s.slice(m[0].length); }
    else if ((m = /^(l|r)[._\- ]/i.exec(s))) { side = m[1].toLowerCase() === "l" ? "left" : "right"; s = s.slice(m[0].length); }
    else if ((m = /^(l|r)(?=[A-Z])/.exec(s))) { side = m[1] === "l" ? "left" : "right"; s = s.slice(1); }
    else if ((m = /(left|right)/i.exec(s))) { side = m[1].toLowerCase(); s = s.slice(0, m.index) + s.slice(m.index + m[0].length); }
    if (seg < 0) {
      const late = /\.(\d{3})$/.exec(s);
      if (late) { seg = parseInt(late[1], 10); s = s.slice(0, -4); }
    }
    const lower = s.toLowerCase();
    const ignore = control || RIG_IGNORE.test(lower);
    let base = lower.replace(/[^a-z0-9]/g, "");
    let num = -1;
    const digits = /(\d+)$/.exec(base);
    if (digits) { num = parseInt(digits[1], 10); base = base.slice(0, -digits[1].length); }
    return { side, base, num, seg, def, ignore };
  }

  // Map a skeleton's nodes to canonical bones. Returns {bone: Map<name,
  // node>, nodeBone: Int16Array (node -> RIG_BONES index or -1), scheme}.
  function rigMapHumanoid(nodes: any[], skins: any[], scheme: string, overrides: any) {
    const n = nodes.length;
    const parent = new Int32Array(n).fill(-1);
    for (let i = 0; i < n; i++) {
      const kids = nodes[i] && nodes[i].children;
      if (kids) for (let k = 0; k < kids.length; k++) if (kids[k] >= 0 && kids[k] < n) parent[kids[k]] = i;
    }
    const depth = new Int32Array(n);
    for (let i = 0; i < n; i++) { let d = 0, p = parent[i]; while (p >= 0 && d < n) { d++; p = parent[p]; } depth[i] = d; }
    const bone = new Map<string, number>();
    const take = (name: string, node: number) => {
      if (!RIG_BONE_SET.has(name) || node < 0) return;
      const have = bone.get(name);
      if (have === undefined || depth[node] < depth[have] || (depth[node] === depth[have] && node < have)) bone.set(name, node);
    };
    const names = nodes.map((node: any) => (node && typeof node.name === "string" ? node.name : ""));
    let joints: Set<number> | null = null;
    for (const skin of Array.isArray(skins) ? skins : []) {
      if (skin && Array.isArray(skin.joints)) { joints = joints || new Set<number>(); for (const j of skin.joints) joints.add(j); }
    }
    const parsed = names.map(rigParseName);
    const hasDEF = parsed.some((p: any) => p.def);
    const candidate = (i: number) => (!joints || joints.has(i)) && !parsed[i].ignore && (!hasDEF || parsed[i].def);
    let detected = "generic";
    if (names.some((x: string) => /mixamorig/i.test(x))) detected = "mixamo";
    else if (hasDEF || names.some((x: string) => /^(org-)?spine\.00[1-6]$/i.test(x))) detected = "rigify";
    else if (names.some((x: string) => /^bip\d*[ _]/i.test(x))) detected = "biped";
    else if (names.some((x: string) => /^pelvis$/i.test(x)) && names.some((x: string) => /_[lr]$/.test(x))) detected = "unreal";
    else if (names.some((x: string) => /\.[LR]$/.test(x))) detected = "blender";
    if (scheme !== "none") {
      const rigifySpine = detected === "rigify" && !parsed.some((p: any, i: number) => candidate(i) && (p.base === "hips" || p.base === "pelvis"));
      const spineChain: number[] = [];
      const legs: Record<string, number[]> = { left: [], right: [] };
      const sideHas: Record<string, Set<string>> = { left: new Set(), right: new Set() };
      for (let i = 0; i < n; i++) {
        if (!candidate(i)) continue;
        const p = parsed[i];
        if (p.base === "spine" && rigifySpine) {
          const which = p.seg < 0 ? "hips" : p.seg === 1 ? "spine" : p.seg === 2 ? "chest" : p.seg === 3 ? "upperChest" : p.seg === 4 ? "neck" : "";
          if (which) take(which, i);
          continue;
        }
        if (p.base === "chest" || p.base === "thorax") { if (!p.side) take("chest", i); continue; }
        if (p.base === "upperchest") { if (!p.side) take("upperChest", i); continue; }
        if (RIG_SPINE_WORDS.has(p.base) && !p.side) { spineChain.push(i); continue; }
        let word = p.base;
        if (word.startsWith("hand") && RIG_FINGER_WORDS[word.slice(4)] !== undefined) word = word.slice(4);
        else if (word.startsWith("f") && RIG_FINGER_WORDS[word.slice(1)] !== undefined) word = word.slice(1);
        if (RIG_FINGER_WORDS[word] !== undefined && p.side) {
          const finger = RIG_FINGER_WORDS[word];
          const index = (p.num >= 0 ? p.num : 1) - 1;
          const segs = RIG_FINGER_SEGMENTS[finger === 0 ? 0 : 1];
          if (index >= 0 && index < segs.length && p.seg <= 0) take(p.side + RIG_FINGERS[finger] + segs[index], i);
          continue;
        }
        if (word === "leg" && p.side) { legs[p.side].push(i); continue; }
        const mapped = RIG_SYNONYMS[word];
        if (!mapped) continue;
        if (mapped[0] === mapped[0].toUpperCase()) {
          if (!p.side || p.seg > 0) continue;
          sideHas[p.side].add(mapped);
          take(p.side + mapped, i);
        } else if (!p.side) {
          if ((mapped === "neck" || mapped === "head") && p.num > 1) continue;
          if (mapped === "head" && rigifySpine) continue;
          take(mapped, i);
        }
      }
      if (rigifySpine) {
        // Rigify: spine.006 is the head, spine.005 the upper neck.
        let headSeg = -1, head = -1;
        for (let i = 0; i < n; i++) if (candidate(i) && parsed[i].base === "spine" && parsed[i].seg >= 5 && parsed[i].seg > headSeg) { headSeg = parsed[i].seg; head = i; }
        if (head >= 0) take("head", head);
      }
      spineChain.sort((a, b) => depth[a] - depth[b] || a - b);
      const k = spineChain.length;
      if (k >= 1 && !bone.has("spine")) take("spine", spineChain[0]);
      if (k >= 2 && !bone.has("chest")) take("chest", spineChain[k >= 3 ? k >> 1 : 1]);
      if (k >= 3 && !bone.has("upperChest")) take("upperChest", spineChain[k - 1]);
      for (const side of ["left", "right"]) {
        for (const node of legs[side]) {
          const as = sideHas[side].has("UpperLeg") ? "LowerLeg" : "UpperLeg";
          take(side + as, node);
        }
      }
    }
    if (overrides && typeof overrides === "object") {
      for (const key of Object.keys(overrides)) {
        const index = names.indexOf(String(overrides[key]));
        if (RIG_BONE_SET.has(key) && index >= 0) bone.set(key, index);
      }
    }
    const nodeBone = new Int16Array(n).fill(-1);
    bone.forEach((node, name) => { nodeBone[node] = RIG_BONES.indexOf(name); });
    return { bone, nodeBone, scheme: scheme && scheme !== "auto" ? scheme : detected, parent };
  }

  // Look up a bone by canonical name, then by exact node name.
  function rigNode(m: any, map: any, name: string) {
    if (!name) return -1;
    const mapped = map && map.bone.get(name);
    if (mapped !== undefined) return mapped;
    for (let i = 0; i < m.nodes.length; i++) if (m.nodes[i] && m.nodes[i].name === name) return i;
    return -1;
  }

  // Rest-pose world rotations and linear parts (model space, root transform
  // excluded), from authored TRS or matrices.
  function rigRestWorld(nodes: any[], parent: Int32Array) {
    const n = nodes.length;
    const rot = new Float64Array(n * 4), lin = new Float64Array(n * 9), pos = new Float64Array(n * 3), done = new Uint8Array(n);
    const local = new Float64Array(4), m3 = new Float64Array(9), t = new Float64Array(3);
    function visit(i: number): void {
      if (done[i]) return;
      done[i] = 1;
      const p = parent[i];
      if (p >= 0) visit(p);
      const node = nodes[i] || {};
      if (node.matrix && node.matrix.length === 16) {
        rigQuatFromMatrix(local, node.matrix);
        for (let c = 0; c < 3; c++) for (let r = 0; r < 3; r++) m3[c * 3 + r] = node.matrix[c * 4 + r];
        t[0] = node.matrix[12]; t[1] = node.matrix[13]; t[2] = node.matrix[14];
      } else {
        const q = node.rotation || [0, 0, 0, 1], s = node.scale || [1, 1, 1], tr = node.translation || [0, 0, 0];
        local[0] = q[0]; local[1] = q[1]; local[2] = q[2]; local[3] = q[3];
        rigMat3FromQuat(m3, local, s);
        t[0] = tr[0]; t[1] = tr[1]; t[2] = tr[2];
      }
      if (p >= 0) {
        agQuatMul(rot, i * 4, rot, p * 4, local, 0);
        rigMat3Mul(lin, i * 9, lin, p * 9, m3);
        rigMat3Apply(lin, p * 9, t[0], t[1], t[2], t);
        pos[i * 3] = pos[p * 3] + t[0]; pos[i * 3 + 1] = pos[p * 3 + 1] + t[1]; pos[i * 3 + 2] = pos[p * 3 + 2] + t[2];
      } else {
        rot.set(local, i * 4);
        lin.set(m3, i * 9);
        pos.set(t, i * 3);
      }
      agNormalizeQuat(rot, i * 4);
    }
    for (let i = 0; i < n; i++) visit(i);
    return { rot, lin, pos };
  }

  function rigQuatFromMatrix(out: Float64Array, m: ArrayLike<number>) {
    const sx = Math.hypot(m[0], m[1], m[2]) || 1, sy = Math.hypot(m[4], m[5], m[6]) || 1, sz = Math.hypot(m[8], m[9], m[10]) || 1;
    const m00 = m[0] / sx, m10 = m[1] / sx, m20 = m[2] / sx, m01 = m[4] / sy, m11 = m[5] / sy, m21 = m[6] / sy, m02 = m[8] / sz, m12 = m[9] / sz, m22 = m[10] / sz;
    const trace = m00 + m11 + m22;
    if (trace > 0) {
      const s = 0.5 / Math.sqrt(trace + 1);
      out[3] = 0.25 / s; out[0] = (m21 - m12) * s; out[1] = (m02 - m20) * s; out[2] = (m10 - m01) * s;
    } else if (m00 > m11 && m00 > m22) {
      const s = 2 * Math.sqrt(1 + m00 - m11 - m22);
      out[3] = (m21 - m12) / s; out[0] = 0.25 * s; out[1] = (m01 + m10) / s; out[2] = (m02 + m20) / s;
    } else if (m11 > m22) {
      const s = 2 * Math.sqrt(1 + m11 - m00 - m22);
      out[3] = (m02 - m20) / s; out[0] = (m01 + m10) / s; out[1] = 0.25 * s; out[2] = (m12 + m21) / s;
    } else {
      const s = 2 * Math.sqrt(1 + m22 - m00 - m11);
      out[3] = (m10 - m01) / s; out[0] = (m02 + m20) / s; out[1] = (m12 + m21) / s; out[2] = 0.25 * s;
    }
    agNormalizeQuat(out, 0);
  }

  function rigMat3FromQuat(out: Float64Array, q: Float64Array, s: ArrayLike<number>) {
    const x = q[0], y = q[1], z = q[2], w = q[3];
    out[0] = (1 - 2 * (y * y + z * z)) * s[0]; out[1] = 2 * (x * y + w * z) * s[0]; out[2] = 2 * (x * z - w * y) * s[0];
    out[3] = 2 * (x * y - w * z) * s[1]; out[4] = (1 - 2 * (x * x + z * z)) * s[1]; out[5] = 2 * (y * z + w * x) * s[1];
    out[6] = 2 * (x * z + w * y) * s[2]; out[7] = 2 * (y * z - w * x) * s[2]; out[8] = (1 - 2 * (x * x + y * y)) * s[2];
  }

  function rigMat3Mul(out: Float64Array, o: number, a: Float64Array, ao: number, b: Float64Array) {
    const r = new Float64Array(9);
    for (let c = 0; c < 3; c++) for (let row = 0; row < 3; row++) {
      r[c * 3 + row] = a[ao + row] * b[c * 3] + a[ao + 3 + row] * b[c * 3 + 1] + a[ao + 6 + row] * b[c * 3 + 2];
    }
    out.set(r, o);
  }

  function rigMat3Inverse(m: Float64Array, o: number) {
    const a = m[o], b = m[o + 3], c = m[o + 6], d = m[o + 1], e = m[o + 4], f = m[o + 7], g = m[o + 2], h = m[o + 5], i = m[o + 8];
    const A = e * i - f * h, B = -(d * i - f * g), C = d * h - e * g;
    const det = a * A + b * B + c * C;
    if (Math.abs(det) < 1e-20) return null;
    const out = new Float64Array(9);
    // column-major inverse
    out[0] = A / det; out[1] = B / det; out[2] = C / det;
    out[3] = -(b * i - c * h) / det; out[4] = (a * i - c * g) / det; out[5] = -(a * h - b * g) / det;
    out[6] = (b * f - c * e) / det; out[7] = -(a * f - c * d) / det; out[8] = (a * e - b * d) / det;
    return out;
  }

  function rigMat3Apply(m: Float64Array, o: number, x: number, y: number, z: number, out: Float64Array) {
    out[0] = m[o] * x + m[o + 3] * y + m[o + 6] * z;
    out[1] = m[o + 1] * x + m[o + 4] * y + m[o + 7] * z;
    out[2] = m[o + 2] * x + m[o + 5] * y + m[o + 8] * z;
  }

  // Retarget clips from a source skeleton onto a target model by canonical
  // bone. For a mapped bone b with parents qs (source) and qt (target):
  //   L_t(t) = W_t(qt)^-1 W_s(qs) L_s(t) W_s(b)^-1 W_t(b)
  // where W are rest-pose world rotations. This transfers each bone's
  // rotation relative to its rest pose, so rigs whose bone axes differ (a
  // Mixamo clip on a Rigify rig) still move the same way, provided both rest
  // poses have the same shape (both T-pose, or both A-pose). The hips
  // translation transfers as a rest-relative offset scaled by hip height;
  // other translations, scales and morph weights are dropped.
  function rigRetargetClips(source: any, sourceMap: any, target: any, targetMap: any, prefix: string) {
    const sNodes = source.nodes, tNodes = target.nodes;
    const sRest = rigRestWorld(sNodes, sourceMap.parent), tRest = rigRestWorld(tNodes, targetMap.parent);
    const sHips = sourceMap.bone.get("hips"), tHips = targetMap.bone.get("hips");
    const sH = sHips === undefined ? 0 : sRest.pos[sHips * 3 + 1], tH = tHips === undefined ? 0 : tRest.pos[tHips * 3 + 1];
    const scale = sH > 1e-6 && tH > 1e-6 ? tH / sH : 1;
    const A = new Float64Array(4), B = new Float64Array(4), tmp = new Float64Array(4), q = new Float64Array(4), v3 = new Float64Array(3);
    const clips: any[] = [];
    for (const clip of Array.isArray(source.animations) ? source.animations : []) {
      if (!clip || !Array.isArray(clip.channels)) continue;
      const channels: any[] = [];
      for (const ch of clip.channels) {
        const sNode = ch.targetID != null ? ch.targetID : ch.targetNode;
        if (!Number.isInteger(sNode) || sNode < 0 || sNode >= sNodes.length) continue;
        const boneIndex = sourceMap.nodeBone[sNode];
        if (boneIndex < 0) continue;
        const tNode = targetMap.bone.get(RIG_BONES[boneIndex]);
        if (tNode === undefined) continue;
        const cubic = ch.interpolation === "CUBICSPLINE";
        const values = ch.values;
        if (ch.property === "rotation") {
          const qs = sourceMap.parent[sNode], qt = targetMap.parent[tNode];
          // A = W_t(qt)^-1 W_s(qs)
          tmp[0] = 0; tmp[1] = 0; tmp[2] = 0; tmp[3] = 1;
          if (qs >= 0) tmp.set(sRest.rot.subarray(qs * 4, qs * 4 + 4));
          if (qt >= 0) agQuatDelta(A, 0, tRest.rot, qt * 4, tmp, 0); else A.set(tmp);
          // B = W_s(b)^-1 W_t(b)
          agQuatDelta(B, 0, sRest.rot, sNode * 4, tRest.rot, tNode * 4);
          const out = new Float32Array(values.length);
          for (let k = 0; k + 4 <= values.length; k += 4) {
            q[0] = values[k]; q[1] = values[k + 1]; q[2] = values[k + 2]; q[3] = values[k + 3];
            agQuatMul(tmp, 0, A, 0, q, 0);
            agQuatMul(q, 0, tmp, 0, B, 0);
            out[k] = q[0]; out[k + 1] = q[1]; out[k + 2] = q[2]; out[k + 3] = q[3];
          }
          channels.push({ targetNode: tNode, targetID: tNode, property: "rotation", interpolation: ch.interpolation, componentCount: 4, times: ch.times, values: out });
        } else if (ch.property === "translation" && boneIndex === 0 && sHips !== undefined && tHips !== undefined) {
          const ps = sourceMap.parent[sNode], pt = targetMap.parent[tNode];
          const inv = pt >= 0 ? rigMat3Inverse(tRest.lin, pt * 9) : new Float64Array([1, 0, 0, 0, 1, 0, 0, 0, 1]);
          if (!inv) continue;
          const sRestT = (sNodes[sNode] && sNodes[sNode].translation) || [0, 0, 0];
          const tRestT = (tNodes[tNode] && tNodes[tNode].translation) || [0, 0, 0];
          const out = new Float32Array(values.length);
          const stride = cubic ? 9 : 3;
          for (let k = 0; k + 3 <= values.length; k += 3) {
            const isValue = !cubic || (k % stride) === 3;
            const dx = values[k] - (isValue ? sRestT[0] : 0), dy = values[k + 1] - (isValue ? sRestT[1] : 0), dz = values[k + 2] - (isValue ? sRestT[2] : 0);
            if (ps >= 0) rigMat3Apply(sRest.lin, ps * 9, dx, dy, dz, v3); else { v3[0] = dx; v3[1] = dy; v3[2] = dz; }
            const wx = v3[0] * scale, wy = v3[1] * scale, wz = v3[2] * scale;
            rigMat3Apply(inv, 0, wx, wy, wz, v3);
            out[k] = v3[0] + (isValue ? tRestT[0] : 0); out[k + 1] = v3[1] + (isValue ? tRestT[1] : 0); out[k + 2] = v3[2] + (isValue ? tRestT[2] : 0);
          }
          channels.push({ targetNode: tNode, targetID: tNode, property: "translation", interpolation: ch.interpolation, componentCount: 3, times: ch.times, values: out });
        }
      }
      clips.push({ name: (prefix || "") + (clip.name || ""), duration: clip.duration, channels });
    }
    return clips;
  }

  // ---------------------------------------------------------------------------
  // Rig resolution for one model
  // ---------------------------------------------------------------------------

  function agResolveRig(m: any, rig: any) {
    const map = rigMapHumanoid(m.nodes, m.record.skins, String(rig.humanoid || "auto"), rig.bones);
    const rest = rigRestWorld(m.nodes, m.parent);
    const out: any = { hash: rig.hash, map, rest, layers: [] as any[], ik: [] as any[], lookAt: [] as any[], hips: -1, maxDrop: 0, pelvis: 0, lastSerial: -1, missing: [] as string[] };
    const node = (name: string) => {
      const index = rigNode(m, map, name);
      if (index < 0 && name) out.missing.push(name);
      return index;
    };
    const masks: (Float32Array | null)[] = [];
    for (const mask of Array.isArray(rig.masks) ? rig.masks : []) {
      const w = new Float32Array(m.n);
      const subtree = (name: string, value: number) => {
        const i = node(name);
        if (i < 0 || m.pos[i] < 0) return;
        const from = m.pos[i], to = m.end[from];
        for (let k = from; k < to; k++) w[m.order[k]] = value;
      };
      for (const name of mask.include || []) subtree(name, 1);
      for (const name of mask.exclude || []) subtree(name, 0);
      const weights = mask.weights || {};
      for (const name of Object.keys(weights)) { const i = node(name); if (i >= 0) w[i] = Math.max(0, Math.min(1, Number(weights[name]) || 0)); }
      masks.push(w);
    }
    for (const layer of Array.isArray(rig.layers) ? rig.layers : []) {
      out.layers.push({
        additive: layer.additive === true,
        mask: Number.isInteger(layer.mask) && layer.mask >= 0 ? masks[layer.mask] || null : null,
        refClip: layer.refClip ? 1 : -1,
        refClipName: layer.refClip || "",
        refTime: Number(layer.refTime) || 0,
        refPose: null,
      });
    }
    for (const chain of Array.isArray(rig.ik) ? rig.ik : []) {
      const a = node(chain.root), b = node(chain.mid), c = node(chain.end);
      const valid = a >= 0 && b >= 0 && c >= 0 && m.parent[c] >= 0 && m.reached[a] && m.reached[b] && m.reached[c];
      out.ik.push({ name: chain.name, a, b, c, foot: chain.foot === true, align: chain.align === true, valid, offset: 0, feedback: new Float64Array(9), input: null });
    }
    for (const chain of Array.isArray(rig.lookAt) ? rig.lookAt : []) {
      const bones = (chain.bones || []).map(node);
      const valid = bones.length > 0 && bones.every((i: number) => i >= 0 && m.reached[i]);
      const pivot = chain.pivot ? node(chain.pivot) : bones[bones.length - 1];
      const axis = Array.isArray(chain.axis) && chain.axis.length === 3 ? chain.axis.map(Number) : [0, 0, 1];
      const len = Math.hypot(axis[0], axis[1], axis[2]) || 1;
      const axisLocal = new Float64Array(3);
      if (pivot >= 0) {
        // Express the bind-pose model-space axis in the pivot bone's frame.
        const qr = rest.rot.subarray(pivot * 4, pivot * 4 + 4);
        ikRotateVec(axisLocal, -qr[0], -qr[1], -qr[2], qr[3], axis[0] / len, axis[1] / len, axis[2] / len);
      }
      // The top-down node path from the first chain bone to the pivot, and
      // where each chain bone sits on it; a chain whose bones are not
      // ancestors of the pivot is invalid.
      const path: number[] = [];
      for (let k = pivot; k >= 0; k = m.parent[k]) { path.unshift(k); if (k === bones[0]) break; }
      const pathStart = bones.map((b: number) => path.indexOf(b));
      out.lookAt.push({
        name: chain.name, bones: Int32Array.from(bones.map((i: number) => (i < 0 ? 0 : i))), weights: Float64Array.from((chain.weights || []).map(Number)),
        pivot, axisLocal, maxAngle: Number(chain.maxAngle) > 0 ? Number(chain.maxAngle) : 1.2,
        valid: valid && pivot >= 0 && path[0] === bones[0] && pathStart.every((i: number) => i >= 0),
        path: Int32Array.from(path), pathStart: Int32Array.from(pathStart), feedback: new Float64Array(6), input: null,
      });
    }
    agDetailClasses(m, map);
    out.boneScale = [] as number[];
    const scales = rig.boneScale || {};
    for (const name of Object.keys(scales)) {
      const i = node(name), f = Number(scales[name]);
      if (i >= 0 && f > 0) out.boneScale.push(i, f);
    }
    if (rig.keepProportions === true) agKeepProportions(m);
    const pelvis = rig.pelvis || {};
    out.hips = node(pelvis.bone || "hips");
    out.maxDrop = Number(pelvis.maxDrop) > 0 ? Number(pelvis.maxDrop) : 0;
    if (out.missing.length && !m.warnedMissing) {
      m.warnedMissing = true;
      console.warn("[gosx] animation graph rig bones not found on " + (m.record.id || "model") + ":", out.missing.join(", "));
    }
    if (Array.isArray(rig.sources) && rig.sources.length) agAttachSources(m, rig.sources, map);
    return out;
  }

  // Level-of-detail classes: 1 for fingers, eyes and jaw; 2 for toes and for
  // unmapped bones with no mapped descendant (twist, accessory and end
  // bones); 0 for everything else. Without a humanoid map every joint stays
  // class 0.
  function agDetailClasses(m: any, map: any) {
    const cls = m.nodeClass;
    cls.fill(0);
    if (!map || map.bone.size === 0) return;
    const mappedBelow = new Uint8Array(m.n);
    for (let k = m.order.length - 1; k >= 0; k--) {
      const node = m.order[k];
      if (map.nodeBone[node] >= 0) mappedBelow[node] = 1;
      const p = m.parent[node];
      if (p >= 0 && mappedBelow[node]) mappedBelow[p] = 1;
    }
    for (let k = 0; k < m.order.length; k++) {
      const node = m.order[k];
      const bone = map.nodeBone[node] >= 0 ? RIG_BONES[map.nodeBone[node]] : "";
      if (bone) {
        if (/Eye$|^jaw$|Thumb|Index|Middle|Ring|Little/.test(bone)) cls[node] = 1;
        else if (/Toes$/.test(bone)) cls[node] = 2;
      } else if (!mappedBelow[node]) {
        // Under the skeleton (some ancestor is mapped) and not holding a
        // mapped bone up: a twist, accessory or end bone. Under a class 1
        // joint (a finger tip) it follows that joint.
        let p = m.parent[node];
        while (p >= 0 && map.nodeBone[p] < 0) p = m.parent[p];
        if (p >= 0) cls[node] = cls[p] === 1 ? 1 : 2;
      }
    }
  }

  // Drop translation channels on every node except root nodes from the
  // model's own clips, keeping this rig's bone lengths. The clip list is
  // replaced so held samples re-resolve.
  function agKeepProportions(m: any) {
    const next = { list: [] as any[], byName: m.clips.byName };
    for (const clip of m.clips.list) {
      next.list.push({ name: clip.name, duration: clip.duration, channels: clip.channels.filter((ch: any) => ch.code !== 0 || m.rootSet[ch.node]) });
    }
    m.clips = next;
  }

  // Clip sources load once per URL per page.
  const agSourceCache = new Map<string, Promise<any>>();

  function agLoadSource(src: string) {
    let promise = agSourceCache.get(src);
    if (!promise) {
      const api = window.__gosx_scene3d_gltf_api;
      if (!api || typeof api.sceneLoadGLTFModel !== "function") return Promise.reject(new Error("glTF loader not loaded"));
      promise = Promise.resolve(api.sceneLoadGLTFModel(src));
      agSourceCache.set(src, promise);
      promise.catch(function() { agSourceCache.delete(src); });
    }
    return promise;
  }

  // Retarget each source's clips onto the model and bind them after the
  // model's own clips. The clip list is replaced, not appended to, so held
  // samples re-resolve their clip names.
  function agAttachSources(m: any, sources: any[], targetMap: any) {
    if (m.sourcesRequested) return;
    m.sourcesRequested = true;
    for (const source of sources) {
      const src = source && typeof source.src === "string" ? source.src : "";
      if (!src) continue;
      agLoadSource(src).then(function(asset: any) {
        const nodes = asset && Array.isArray(asset.nodes) ? asset.nodes : [];
        const sourceMap = rigMapHumanoid(nodes, asset.skins || [], String(source.humanoid || "auto"), null);
        const clips = rigRetargetClips({ nodes, animations: asset.animations || [] }, sourceMap, { nodes: m.nodes }, targetMap, String(source.prefix || ""));
        const next = { list: m.clips.list.slice(), byName: new Map(m.clips.byName) };
        agBindClips(clips, m.n, next);
        m.clips = next;
        m.record.poseDirty = true;
      }, function(error: any) {
        console.warn("[gosx] animation graph clip source failed:", src, error && error.message ? error.message : error);
      });
    }
  }
