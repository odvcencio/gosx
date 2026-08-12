// mount-quality.ts — the G2 QualityLadder governor: bidirectional work-based adaptive
// @ts-check
// quality.
//
// Raises and lowers the render rung from measured frame work, filters objects
// and points by admitted quality group, and applies the post-effects state
// each rung allows.
//
// A scene that declares no AdaptiveQuality never leaves rung zero. This file
// is a gate candidate: the server knows at lowering time whether the scene
// declares a ladder.

/**
 * @typedef {object} GoSXSceneQualityState
 * @property {string} mode
 * @property {number} rung
 * @property {boolean} enabled
 */
  // --------------------------------------------------------------------------
  // G2 QualityLadder governor — bidirectional work-based ABR.
  //
  // Runs INSTEAD of the dprCap-tier body above whenever state.mode ===
  // "ladder" (see createSceneAdaptiveQualityState, which sets state.enabled
  // = false in that case so every dprCap-tier-gated code path elsewhere —
  // sceneViewportFromMount's DPR clamp in particular — goes inert; a ladder
  // NEVER touches DPR/resolution, per the PRIME DIRECTIVE).
  //
  // The frame-sampling block below is a deliberate near-duplicate of the
  // dprCap-tier body's sampling block (same helpers: sceneAdaptiveRendererSample,
  // sceneAdaptiveRendererTimingStatus, sceneAdaptiveP95; same warmup/resume/
  // renderer-timing-fallback handling) rather than a shared extraction, so
  // this milestone cannot regress the existing, heavily-tested dprCap-tier
  // controller by construction — the two bodies are fully independent code
  // paths that happen to read/write the same state fields (mutually
  // exclusive per mount: a mount is either in "tier" or "ladder" mode, never
  // both), reusing the "honest gpu-ms/interval sampling" the spec calls for.
  // --------------------------------------------------------------------------

  function sceneUpdateQualityLadder(state, mount, sceneState, viewport, frameStart, frameNowMS, renderer) {
    if (!state || state.mode !== "ladder" || !Array.isArray(state.ladder) || state.ladder.length === 0) {
      return false;
    }
    const now = typeof performance !== "undefined" && performance.now ? performance.now() : Date.now();
    const rafNow = Number.isFinite(Number(frameNowMS)) ? Number(frameNowMS) : now;
    const cpuDurationMS = Math.max(0, now - sceneNumber(frameStart, now));
    const rafIntervalMS = state.lastRAFNowMS != null && rafNow >= state.lastRAFNowMS ? rafNow - state.lastRAFNowMS : 0;
    state.lastRAFNowMS = rafNow;
    state.frameCount += 1;
    const timingStatus = sceneAdaptiveRendererTimingStatus(renderer);
    const rendererTimingLocked = Boolean(timingStatus && (timingStatus.available === true || timingStatus.active === true));
    let sample = sceneAdaptiveRendererSample(renderer, now);
    if (sample) state.missingRendererSamples = 0;
    else if (rendererTimingLocked) state.missingRendererSamples += 1;
    else state.missingRendererSamples = 0;
    const rendererTimingStale = rendererTimingLocked && state.missingRendererSamples >= state.rendererTimingFallbackFrames;
    if (!sample && (!rendererTimingLocked || rendererTimingStale) && rafIntervalMS > 0 && cpuDurationMS >= 0) {
      sample = {
        durationMS: Math.max(rafIntervalMS, cpuDurationMS),
        source: rendererTimingStale ? "cpu-raf-stale-renderer-timing" : "cpu-raf",
        atMS: now,
        rafIntervalMS,
        cpuDurationMS,
      };
    }
    if (state.resumePending || state.frameCount <= state.warmupFrames || !sample) {
      state.resumePending = false;
      applySceneAdaptiveQualityState(mount, state, now, false);
      return false;
    }
    const frameMS = sample.durationMS;
    const isRAFMeasured = sample.source.indexOf("cpu-raf") === 0;
    if (isRAFMeasured && sceneQualityLadderScrollInputActive(sceneState, now)) {
      state.lastFrameMS = frameMS;
      state.measurement = sample.source + "-scroll-interaction";
      state.lastMeasurement = sample;
      state.badFrames = 0;
      state.goodFrames = 0;
      state.severeFrames = 0;
      state.rungPromoteRule = "scroll-interaction-held";
      applySceneAdaptiveQualityState(mount, state, now, false);
      return false;
    }
    state.lastFrameMS = frameMS;
    state.measurement = sample.source;
    state.lastMeasurement = sample;
    state.validSamples += 1;
    state.ewmaFrameMS = state.ewmaFrameMS > 0
      ? state.ewmaFrameMS * 0.84 + frameMS * 0.16
      : frameMS;
    if (state.sampleWindow.length < 120) state.sampleWindow.push(frameMS);
    else {
      state.sampleWindow[state.sampleCursor] = frameMS;
      state.sampleCursor = (state.sampleCursor + 1) % 120;
    }
    if (state.validSamples === 1 || state.validSamples % 10 === 0) state.p95FrameMS = sceneAdaptiveP95(state);

    const target = Math.max(8, sceneNumber(isRAFMeasured ? state.cpuRAFBudgetMS : state.targetFrameMS, 16.7));
    const missesBudget = state.ewmaFrameMS > target * 1.15 || state.p95FrameMS > target * 1.35;
    const severeMiss = frameMS > target * 2;
    // rungPromoteRule: which promotion rule this frame's measurement source
    // uses — published on data-gosx-scene3d-quality-promote-rule below.
    state.rungPromoteRule = isRAFMeasured ? "raf-cadence" : "gpu-headroom";
    // Promote-eligible check differs by measurement source:
    //   - GPU-measured (real timestamp-query): frameMS has actual headroom
    //     below state.rungPromoteThreshold (default 0.7) × target — the
    //     original rule.
    //   - cpu-raf fallback: rAF interval on a vsync-locked display floors at
    //     ~1/refreshRate (~16.7ms @60Hz) even on a perfectly healthy page,
    //     so it can NEVER read below 0.7×target — that condition would keep
    //     every real session (no GPU timing) stuck at the boot rung forever
    //     (observed in production: webgpuPostEffects stayed 0 after 13k+
    //     frames at a locked 60fps). Promote instead on sustained
    //     vsync-CLEAN cadence: frameMS not exceeding target by more than a
    //     small (6%) margin, for rungPromoteFrames consecutive frames — "not
    //     missing cadence" rather than "has spare headroom", since rAF alone
    //     cannot observe spare headroom past the display's refresh floor.
    //     Demotion (below) is unchanged for either source.
    const promoteEligible = isRAFMeasured
      ? frameMS <= target * 1.06
      : frameMS < target * state.rungPromoteThreshold;
    if (missesBudget) {
      state.badFrames += 1;
      state.goodFrames = 0;
    } else if (promoteEligible) {
      state.goodFrames += 1;
      state.badFrames = 0;
    } else {
      state.badFrames = 0;
      state.goodFrames = 0;
    }
    state.severeFrames = severeMiss ? state.severeFrames + 1 : 0;

    let changed = false;
    if (now >= state.cooldownUntilMS && (state.badFrames >= 20 || state.severeFrames >= 3)) {
      // DEMOTE: the SAME sustained-miss condition the dprCap-tier controller
      // uses (badFrames >= 20 || severeFrames >= 3).
      changed = sceneQualityLadderSetRung(state, state.rungIndex - 1, severeMiss ? "severe" : "sustained", now);
    } else if (now >= state.cooldownUntilMS && state.goodFrames >= state.rungPromoteFrames) {
      // PROMOTE: N (default ~120) consecutive headroom frames — NOT gated on
      // a 300-sample floor like the dprCap-tier controller, since the ladder
      // has no postFX-suppression secondary shed step to reserve budget for.
      changed = sceneQualityLadderSetRung(state, state.rungIndex + 1, "recovered", now);
    }
    if (changed) {
      sceneApplyQualityLadderRung(sceneState, state);
      applyScenePostFXState(mount, sceneState);
      applySceneAdaptiveQualityState(mount, state, now, true);
      const rung = state.ladder[state.rungIndex];
      // "quality-rung-transition" mirrors the existing
      // "adaptive-quality-transition" event pattern above verbatim (same
      // gosxSceneEmit("warn", ...) call shape).
      gosxSceneEmit("warn", "quality-rung-transition", {
        reason: state.rungReason,
        rungIndex: state.rungIndex,
        rungName: rung ? rung.name : "",
        rungRevision: state.rungRevision,
        frameMS,
        ewmaFrameMS: state.ewmaFrameMS,
        p95FrameMS: state.p95FrameMS,
        targetFrameMS: target,
      });
      return true;
    }
    applySceneAdaptiveQualityState(mount, state, now, false);
    return false;
  }

  // sceneQualityLadderSetRung clamps to [0, ladder.length-1] and mirrors
  // sceneAdaptiveSetTier's shape/semantics: a genuine rung change resets the
  // hysteresis counters and starts a cooldown window; a no-op at either
  // boundary (promote past the top rung, demote past rung 0) leaves
  // everything untouched and returns false, exactly like
  // sceneAdaptiveSetTier's `state.activeTier === tier` guard — so a
  // saturated ladder at the floor doesn't spam cooldown/counter resets every
  // frame.
  function sceneQualityLadderSetRung(state, nextIndexRaw, reason, nowMS) {
    const nextIndex = Math.max(0, Math.min(state.ladder.length - 1, nextIndexRaw));
    if (nextIndex === state.rungIndex) {
      return false;
    }
    state.rungIndex = nextIndex;
    state.rungRevision += 1;
    state.rungReason = reason;
    state.cooldownUntilMS = nowMS + state.cooldownMS;
    state.badFrames = 0;
    state.goodFrames = 0;
    state.severeFrames = 0;
    return true;
  }

  // sceneApplyQualityLadderRung composes with G1's non-destructive
  // postFXMaxPixels mechanism: it swaps sceneState.postEffects between
  // subsets of the AUTHOR'S FULL postEffects list
  // (sceneAdaptivePostFXSource(sceneState) — the same source-of-truth field
  // the dprCap-tier controller's postFX suppression reads) based on which
  // effect kinds/names the active rung admits. An effect is either present
  // at full postFX resolution or entirely ABSENT — never resolution-scaled
  // (PRIME DIRECTIVE) — so this never touches sceneState.postFXMaxPixels.
  // Returns true when sceneState.postEffects actually changed.
  function sceneApplyQualityLadderRung(sceneState, ladderState) {
    if (!sceneState || !ladderState || !Array.isArray(ladderState.ladder)) {
      return false;
    }
    const rung = ladderState.ladder[ladderState.rungIndex];
    if (!rung) {
      return false;
    }
    const source = sceneAdaptivePostFXSource(sceneState);
    const admitted = rung.postEffects && rung.postEffects.length > 0
      ? source.filter(function(effect) { return sceneQualityLadderEffectAdmitted(effect, rung.postEffects); })
      : [];
    const current = Array.isArray(sceneState.postEffects) ? sceneState.postEffects : [];
    const same = current.length === admitted.length && current.every(function(effect, index) { return effect === admitted[index]; });
    if (same) {
      return false;
    }
    sceneState.postEffects = admitted;
    return true;
  }

  // sceneQualityLadderEffectAdmitted matches a post-effect against a rung's
  // admitted list by "kind" (built-in effect kinds like "bloom",
  // "toneMapping" — case-insensitive) or by "name"/"id" (CustomPost passes,
  // matched case-sensitively since author-chosen names are opaque
  // identifiers, unlike the built-in kinds).
  function sceneQualityLadderEffectAdmitted(effect, admittedList) {
    if (!effect || !Array.isArray(admittedList) || admittedList.length === 0) {
      return false;
    }
    const kind = typeof effect.kind === "string" ? effect.kind.trim().toLowerCase() : "";
    const name = typeof effect.name === "string" ? effect.name.trim() : "";
    const id = typeof effect.id === "string" ? effect.id.trim() : "";
    for (let i = 0; i < admittedList.length; i += 1) {
      const admitted = admittedList[i];
      if (!admitted) continue;
      if (kind && admitted.toLowerCase() === kind) return true;
      if (name && admitted === name) return true;
      if (id && admitted === id) return true;
    }
    return false;
  }

  // sceneQualityLadderAdmittedGroups reads the CURRENT rung's LayerGroups
  // straight off the governor state — no cached/applied step needed (unlike
  // postEffects, layer-group visibility has no sceneState.* field to mutate;
  // the render loop just re-reads the active rung fresh every frame, which
  // is "instant, no transitions" by construction). Returns null when no
  // ladder governs OR the active rung has no LayerGroups authored (back-
  // compat: sceneFilterObjectsByQualityGroups/sceneFilterPointsByQualityGroups
  // both treat null as "no filtering, admit everything").
  //
  // v0.33.1 fix: normalizeSceneQualityRung always materializes `layerGroups`
  // as an array — `[]` for a rung with no LayerGroups authored, never
  // undefined. An empty array is truthy in JS, so returning it verbatim (as
  // this function used to) made the filter functions' `!admittedGroups`
  // back-compat check false — they'd proceed to filter, and since
  // `[].indexOf(anything)` is always -1, EVERY tagged (non-empty
  // qualityGroup) entry got rejected instead of admitted. This reproduced
  // on ANY frame the active rung had empty/absent LayerGroups — most
  // visibly when QualityStartRung pointed straight at such a rung, so
  // authored-but-tagged points vanished from frame one. Explicitly
  // collapsing an empty layerGroups array to null here fixes it uniformly
  // for every rung regardless of whether it's reached via QualityStartRung
  // (init) or promotion/demotion (transition) — same function, same value,
  // every frame.
  function sceneQualityLadderAdmittedGroups(adaptiveQuality) {
    if (!adaptiveQuality || adaptiveQuality.mode !== "ladder" || !Array.isArray(adaptiveQuality.ladder)) {
      return null;
    }
    const rung = adaptiveQuality.ladder[adaptiveQuality.rungIndex];
    if (!rung || !Array.isArray(rung.layerGroups) || rung.layerGroups.length === 0) {
      return null;
    }
    return rung.layerGroups;
  }

  // sceneFilterObjectsByQualityGroups drops objects tagged with a
  // Mesh.QualityGroup (see normalizeSceneObject's qualityGroup field) that
  // the active rung does not admit. Untagged objects (qualityGroup === "")
  // are unconditionally visible — a ladder only gates objects that opted
  // in. Avoids allocating when nothing is actually filtered (hot per-frame
  // path).
  function sceneFilterObjectsByQualityGroups(objects, admittedGroups) {
    if (!admittedGroups || !Array.isArray(objects) || objects.length === 0) {
      return objects;
    }
    let filtered = null;
    for (let i = 0; i < objects.length; i += 1) {
      const object = objects[i];
      const group = object && typeof object.qualityGroup === "string" ? object.qualityGroup : "";
      const admitted = !group || admittedGroups.indexOf(group) !== -1;
      if (!admitted) {
        if (!filtered) filtered = objects.slice(0, i);
      } else if (filtered) {
        filtered.push(object);
      }
    }
    return filtered || objects;
  }

  // sceneEffectivePointQualityGroup resolves the QualityRung.LayerGroups tag
  // a points entry gates on: the entry's own authored qualityGroup (see
  // normalizeScenePointsEntry) when non-empty, else a scene-level name-based
  // fallback via Props.PointQualityGroups (scene.pointQualityGroups) keyed
  // by the SAME `material` field the named-material binding path already
  // matches by (sceneNamedMaterialForRecord / sceneApplyNamedMaterialToPoints)
  // — this is how GLB-baked point layers extracted at runtime by layer name
  // (which cannot carry Points.QualityGroup directly) opt into ladder
  // gating. Returns "" when neither source tags the entry (unconditionally
  // visible).
  function sceneEffectivePointQualityGroup(point, pointQualityGroups) {
    const own = point && typeof point.qualityGroup === "string" ? point.qualityGroup : "";
    if (own) {
      return own;
    }
    if (!pointQualityGroups || typeof pointQualityGroups.get !== "function") {
      return "";
    }
    const name = point && typeof point.material === "string" ? point.material : "";
    if (!name) {
      return "";
    }
    const mapped = pointQualityGroups.get(name);
    return typeof mapped === "string" ? mapped : "";
  }

  // sceneFilterPointsByQualityGroups is sceneFilterObjectsByQualityGroups's
  // Points counterpart — same admitted-groups semantics (untagged always
  // visible; no ladder / rung with no layerGroups draws everything;
  // per-frame, no remount needed for a rung transition to take effect), plus
  // the pointQualityGroups name-based fallback for GLB-extracted layers (see
  // sceneEffectivePointQualityGroup). The returned array carries a
  // `qualitySkippedCount` own property (0 when nothing was authored/no
  // ladder active) so callers can surface a point-quality-skipped stat
  // without a second pass over the array.
  function sceneFilterPointsByQualityGroups(points, admittedGroups, pointQualityGroups) {
    if (!admittedGroups || !Array.isArray(points) || points.length === 0) {
      if (Array.isArray(points) && points.qualitySkippedCount == null) {
        points.qualitySkippedCount = 0;
      }
      return points;
    }
    let filtered = null;
    let skipped = 0;
    for (let i = 0; i < points.length; i += 1) {
      const point = points[i];
      const group = sceneEffectivePointQualityGroup(point, pointQualityGroups);
      const admitted = !group || admittedGroups.indexOf(group) !== -1;
      if (!admitted) {
        skipped += 1;
        if (!filtered) filtered = points.slice(0, i);
      } else if (filtered) {
        filtered.push(point);
      }
    }
    const result = filtered || points;
    result.qualitySkippedCount = skipped;
    return result;
  }

  function sceneQualityLadderPointBudgetScale(adaptiveQuality) {
    if (!adaptiveQuality || adaptiveQuality.mode !== "ladder" || !Array.isArray(adaptiveQuality.ladder)) {
      return 1;
    }
    const rung = adaptiveQuality.ladder[adaptiveQuality.rungIndex];
    const scale = sceneNumber(rung && rung.pointBudgetScale, 0);
    if (scale === 0) {
      return 1;
    }
    return Math.max(0, Math.min(1, scale));
  }

  function scenePointSampleOutput(raw, length, previous) {
    if (previous && previous.length === length) {
      const rawIsFloat32 = raw instanceof Float32Array;
      if ((rawIsFloat32 && previous instanceof Float32Array) || (!rawIsFloat32 && Array.isArray(previous))) {
        return previous;
      }
    }
    return raw instanceof Float32Array ? new Float32Array(length) : new Array(length);
  }

  function sceneSamplePointField(raw, authoredCount, drawCount, components, previous) {
    if (!raw || authoredCount <= 0 || drawCount <= 0 || components <= 0 || typeof raw.length !== "number") {
      return null;
    }
    if (raw.length < authoredCount * components) {
      return null;
    }
    const out = scenePointSampleOutput(raw, drawCount * components, previous);
    for (let i = 0; i < drawCount; i += 1) {
      const src = Math.min(authoredCount - 1, Math.floor(((i + 0.5) * authoredCount) / drawCount));
      for (let c = 0; c < components; c += 1) {
        out[i * components + c] = raw[src * components + c];
      }
    }
    return out;
  }

  function sceneSamplePointScalarField(raw, authoredCount, drawCount, previous) {
    if (!raw || authoredCount <= 0 || drawCount <= 0 || typeof raw.length !== "number" || raw.length < authoredCount) {
      return null;
    }
    const out = scenePointSampleOutput(raw, drawCount, previous);
    for (let i = 0; i < drawCount; i += 1) {
      const src = Math.min(authoredCount - 1, Math.floor(((i + 0.5) * authoredCount) / drawCount));
      out[i] = raw[src];
    }
    return out;
  }

  function sceneScalePointEntryBudget(entry, drawCount, scale, index) {
    if (!entry || drawCount <= 0 || scale >= 1) {
      return entry;
    }
    const authoredCount = Math.max(0, Math.floor(sceneNumber(entry.count, 0)));
    const rawPositions = entry._cachedPos || entry.positions;
    const rawSizes = entry._cachedSizes || entry.sizes;
    const rawColors = entry._cachedColors || entry.colors;
    const cacheKey = String(authoredCount) + ":" + String(drawCount) + ":" + String(scale);
    const cache = entry._qualityPointBudgetCache;
    const canReuse = cache && cache.key === cacheKey && cache.entry;
    const next = canReuse ? Object.assign(cache.entry, entry) : Object.assign({}, entry);
    next.count = drawCount;
    next._qualityPointBudgetScale = scale;
    next._qualityPointAuthoredCount = authoredCount;
    next._qualityPointBudgetIndex = index;
    const sampledPositions = sceneSamplePointField(rawPositions, authoredCount, drawCount, 3, canReuse ? cache.sampledPositions : null);
    if (sampledPositions) {
      next.positions = sampledPositions;
      next._cachedPos = sampledPositions;
    } else {
      delete next._cachedPos;
    }
    const sampledSizes = sceneSamplePointScalarField(rawSizes, authoredCount, drawCount, canReuse ? cache.sampledSizes : null);
    if (sampledSizes) {
      next.sizes = sampledSizes;
      next._cachedSizes = sampledSizes;
    } else {
      delete next._cachedSizes;
    }
    let sampledColors = null;
    if (rawColors && typeof rawColors.length === "number") {
      if (Array.isArray(rawColors) && typeof rawColors[0] === "string" && rawColors.length >= authoredCount) {
        sampledColors = sceneSamplePointScalarField(rawColors, authoredCount, drawCount, canReuse ? cache.sampledColors : null);
      } else if (rawColors.length >= authoredCount * 4) {
        sampledColors = sceneSamplePointField(rawColors, authoredCount, drawCount, 4, canReuse ? cache.sampledColors : null);
      } else if (rawColors.length >= authoredCount * 3) {
        sampledColors = sceneSamplePointField(rawColors, authoredCount, drawCount, 3, canReuse ? cache.sampledColors : null);
      }
    }
    if (sampledColors) {
      next.colors = sampledColors;
      next._cachedColors = sampledColors;
    } else {
      delete next._cachedColors;
    }
    entry._qualityPointBudgetCache = {
      key: cacheKey,
      entry: next,
      sampledPositions,
      sampledSizes,
      sampledColors,
    };
    return next;
  }

  function sceneApplyPointBudgetScale(points, scale) {
    if (!Array.isArray(points) || points.length === 0) {
      return points;
    }
    const pointScale = Math.max(0, Math.min(1, sceneNumber(scale, 1)));
    let scaled = null;
    let authoredInstances = 0;
    let drawInstances = 0;
    let scaledEntries = 0;
    for (let i = 0; i < points.length; i += 1) {
      const entry = points[i];
      const authored = Math.max(0, Math.floor(sceneNumber(entry && entry.count, 0)));
      authoredInstances += authored;
      let drawCount = authored;
      if (pointScale < 1 && authored > 0) {
        drawCount = Math.max(1, Math.floor(authored * pointScale));
      }
      drawInstances += drawCount;
      if (drawCount !== authored) {
        scaledEntries += 1;
        if (!scaled) scaled = points.slice(0, i);
        scaled.push(sceneScalePointEntryBudget(entry, drawCount, pointScale, i));
      } else if (scaled) {
        scaled.push(entry);
      }
    }
    const result = scaled || points;
    result.qualityPointBudgetScale = pointScale;
    result.qualityPointAuthoredInstances = authoredInstances;
    result.qualityPointDrawInstances = drawInstances;
    result.qualityPointBudgetScaledEntries = scaledEntries;
    return result;
  }

  function sceneQualityLadderScrollInputActive(sceneState, nowMS) {
    const scrollCamera = sceneState && sceneState._scrollCamera;
    if (!scrollCamera) {
      return false;
    }
    const activeUntil = sceneNumber(scrollCamera._activeInputUntil, 0);
    return activeUntil > 0 && activeUntil + 1500 >= nowMS;
  }

  function sceneQualityLadderActiveRung(adaptiveQuality) {
    if (!adaptiveQuality || adaptiveQuality.mode !== "ladder" || !Array.isArray(adaptiveQuality.ladder)) {
      return null;
    }
    return adaptiveQuality.ladder[adaptiveQuality.rungIndex] || null;
  }

  function sceneQualityLadderComputeBudgetScale(adaptiveQuality) {
    const rung = sceneQualityLadderActiveRung(adaptiveQuality);
    if (!rung) {
      return 1;
    }
    return Math.max(0, Math.min(1, sceneNumber(rung.computeBudgetScale, 1)));
  }

  function sceneComputeParticlesInstanceCount(computeParticles) {
    if (!Array.isArray(computeParticles) || computeParticles.length === 0) {
      return 0;
    }
    let count = 0;
    for (let i = 0; i < computeParticles.length; i += 1) {
      count += Math.max(0, Math.floor(sceneNumber(computeParticles[i] && computeParticles[i].count, 0)));
    }
    return count;
  }

  function sceneScaleComputeParticlesByQualityRung(computeParticles, adaptiveQuality) {
    if (!Array.isArray(computeParticles) || computeParticles.length === 0) {
      return computeParticles;
    }
    const rung = sceneQualityLadderActiveRung(adaptiveQuality);
    if (!rung) {
      return computeParticles;
    }
    const scale = sceneQualityLadderComputeBudgetScale(adaptiveQuality);
    if (scale >= 1) {
      return computeParticles;
    }
    const scaled = [];
    let changed = false;
    for (let i = 0; i < computeParticles.length; i += 1) {
      const entry = computeParticles[i];
      const authoredCount = Math.max(0, Math.floor(sceneNumber(entry && entry.count, 0)));
      const nextCount = Math.max(0, Math.floor(authoredCount * scale));
      if (nextCount <= 0) {
        changed = true;
        continue;
      }
      if (nextCount !== authoredCount) {
        scaled.push(Object.assign({}, entry, { count: nextCount }));
        changed = true;
      } else {
        scaled.push(entry);
      }
    }
    return changed ? scaled : computeParticles;
  }

  // applySceneQualityLadderState is applySceneAdaptiveQualityState's ladder
  // counterpart — same publish-throttle shape (force / 250ms / revision-
  // changed gate via lastPublishedAtMS/lastPublishedRevision, reused
  // directly from the shared state object), different attribute set.
  function applySceneQualityLadderState(mount, state, nowMS, force) {
    if (!mount || !state) {
      return;
    }
    const rung = (state.ladder && state.ladder[state.rungIndex]) || null;
    mount.__gosxScene3DQualityState = {
      enabled: true,
      mode: "ladder",
      rungIndex: state.rungIndex,
      rungName: rung ? rung.name : "",
      rungCount: state.ladder ? state.ladder.length : 0,
      rungRevision: state.rungRevision,
      reason: state.rungReason,
      cooldownUntilMS: state.cooldownUntilMS,
      validSamples: state.validSamples,
      ewmaFrameMS: state.ewmaFrameMS,
      p95FrameMS: state.p95FrameMS,
      measurement: state.measurement,
      missingRendererSamples: state.missingRendererSamples,
      lastMeasurement: state.lastMeasurement,
      promoteRule: state.rungPromoteRule,
      rung,
    };
    const now = Number.isFinite(Number(nowMS)) ? Number(nowMS) : (typeof performance !== "undefined" && performance.now ? performance.now() : Date.now());
    const changed = state.lastPublishedRevision !== state.rungRevision;
    if (!force && !changed && now - state.lastPublishedAtMS < 250) return;
    state.lastPublishedAtMS = now;
    state.lastPublishedRevision = state.rungRevision;
    setAttrValue(mount, "data-gosx-scene3d-quality-ladder", "true");
    // Essential tier, per the G2 spec.
    setAttrValue(mount, "data-gosx-scene3d-quality-rung", String(state.rungIndex));
    setAttrValue(mount, "data-gosx-scene3d-quality-rung-name", rung ? rung.name : "");
    setAttrValue(mount, "data-gosx-scene3d-quality-rung-reason", state.rungReason || "");
    setAttrValue(mount, "data-gosx-scene3d-quality-rung-revision", String(Math.max(0, state.rungRevision || 0)));
    // Bonus telemetry, mirroring the dprCap-tier controller's richer
    // attribute set (data-gosx-scene3d-quality-frame-ms et al).
    setAttrValue(mount, "data-gosx-scene3d-quality-rung-count", String(state.ladder ? state.ladder.length : 0));
    setAttrValue(mount, "data-gosx-scene3d-quality-measurement", state.measurement || "none");
    // promote-rule: which promotion condition the last sample evaluated
    // under — "gpu-headroom" (real GPU timing, the original 0.7×-target
    // headroom rule) or "raf-cadence" (cpu-raf fallback: sustained
    // not-missing-cadence, since rAF alone floors at the display refresh
    // interval and can never show headroom below 0.7×target). See
    // sceneUpdateQualityLadder's promoteEligible branch.
    setAttrValue(mount, "data-gosx-scene3d-quality-promote-rule", state.rungPromoteRule || "gpu-headroom");
    setAttrValue(mount, "data-gosx-scene3d-quality-frame-ms", state.lastFrameMS > 0 ? state.lastFrameMS.toFixed(1) : "");
    setAttrValue(mount, "data-gosx-scene3d-quality-ewma-ms", state.ewmaFrameMS > 0 ? state.ewmaFrameMS.toFixed(2) : "");
    setAttrValue(mount, "data-gosx-scene3d-quality-p95-ms", state.p95FrameMS > 0 ? state.p95FrameMS.toFixed(2) : "");
    // ExpensivePassCadence remains published-only. ComputeBudgetScale also
    // drives compute-particle count scaling before bundle creation.
    setAttrValue(mount, "data-gosx-scene3d-quality-rung-compute-budget-scale", rung ? String(rung.computeBudgetScale) : "");
    setAttrValue(mount, "data-gosx-scene3d-quality-rung-point-budget-scale", rung ? String(rung.pointBudgetScale) : "");
    setAttrValue(mount, "data-gosx-scene3d-quality-rung-cadence", rung ? String(rung.expensivePassCadence) : "");
  }

  function applyScenePostFXState(mount, state) {
    if (!mount || !state) {
      return;
    }
    const deferred = Array.isArray(state._deferredPostEffects) && state._deferredPostEffects.length > 0;
    const enabled = Array.isArray(state.postEffects) && state.postEffects.length > 0;
    setAttrValue(mount, "data-gosx-scene3d-postfx", deferred ? "deferred" : (enabled ? "enabled" : "none"));
    // G1: live confirmation of the postFXMaxPixels cap actually driving the
    // postfx render targets this frame. Both backends read
    // sceneState.postFXMaxPixels fresh off the render bundle every frame and
    // resize their offscreen targets accordingly (createScenePostProcessor.
    // begin in 16-scene-webgl.js; ensureFBOs/getSceneTarget in
    // 16a-scene-webgpu.js) — this attribute is this frame's live value, not
    // whatever was true at mount time. See handle.updateSceneProps's
    // postFXMaxPixels branch, the only non-mount/non-command mutator.
    setAttrValue(mount, "data-gosx-scene3d-postfx-max-pixels", String(Math.max(0, Math.floor(sceneNumber(state.postFXMaxPixels, 0)))));
    // post-uniform-patches / post-uniform-patch-misses: cumulative counts
    // from SCENE_CMD_SET_POST_UNIFORMS (see applyScenePostUniformsCommand in
    // 10-runtime-scene-core.js) — how many named-pass uniform patches have
    // applied vs. targeted a name with no matching CustomPost pass.
    setAttrValue(mount, "data-gosx-scene3d-post-uniform-patches", String(Math.max(0, Math.floor(sceneNumber(state.postUniformPatches, 0)))));
    setAttrValue(mount, "data-gosx-scene3d-post-uniform-patch-misses", String(Math.max(0, Math.floor(sceneNumber(state.postUniformPatchMisses, 0)))));
  }
