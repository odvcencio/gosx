// 30c2 — arcadeAudio, the procedural WebAudio synth.
//
// Chunks: bootstrap.js, bootstrap-feature-hubs.js.
//
// Oscillator tones, frequency sweeps and filtered noise bursts, shaped by
// short gain envelopes and mixed through a shared compressor bus. It loads no
// assets.
//
// This file stays in the framework chunk on purpose. scene.AudioCue and
// scene.SynthPatch (scene/audio.go) are the framework's typed front door, and
// they deliver into window.__gosx.arcadeAudio, which this file publishes.
// 20-scene-mount.js applySceneAudioCue fires that path for any scene. The
// named cue vocabulary in playArcadeSFX is part of the same typed surface
// (see the Cue field on scene.AudioCue).

  const arcadeAudioState = {
    context: null,
    active: [],
    master: null,
    compressor: null,
    voiceLimit: 28,
  };

  function arcadeAudioContext() {
    const Ctor = window.AudioContext || window.webkitAudioContext;
    if (!Ctor) return null;
    if (!arcadeAudioState.context) {
      try {
        arcadeAudioState.context = new Ctor();
        arcadeConfigureOutput(arcadeAudioState.context);
      } catch (_e) {
        arcadeAudioState.context = null;
      }
    }
    return arcadeAudioState.context;
  }

  function arcadeConfigureOutput(audio) {
    if (!audio || arcadeAudioState.master) return;
    const destination = audio.destination;
    if (!destination || typeof audio.createGain !== "function") return;
    const master = audio.createGain();
    master.gain.value = 0.82;
    let tail = master;
    if (typeof audio.createDynamicsCompressor === "function") {
      const compressor = audio.createDynamicsCompressor();
      if (compressor.threshold) compressor.threshold.value = -18;
      if (compressor.knee) compressor.knee.value = 18;
      if (compressor.ratio) compressor.ratio.value = 4;
      if (compressor.attack) compressor.attack.value = 0.003;
      if (compressor.release) compressor.release.value = 0.12;
      master.connect(compressor);
      tail = compressor;
      arcadeAudioState.compressor = compressor;
    }
    tail.connect(destination);
    arcadeAudioState.master = master;
  }

  function arcadeOutput(audio) {
    arcadeConfigureOutput(audio);
    return arcadeAudioState.master || audio.destination;
  }

  function unlockArcadeAudio() {
    const audio = arcadeAudioContext();
    if (!audio || typeof audio.createOscillator !== "function" || typeof audio.createGain !== "function") return;
    if (typeof audio.resume === "function") audio.resume();
    return audio;
  }

  function arcadeClamp(value, min, max, fallback) {
    return Math.max(min, Math.min(max, hubInputNumber(value, fallback)));
  }

  function arcadeSoundOptions(options) {
    if (typeof options === "number") {
      return { delayMS: Math.max(0, hubInputNumber(options, 0)), intensity: 1, pan: 0, depth: 0 };
    }
    const raw = options && typeof options === "object" ? options : {};
    return {
      delayMS: Math.max(0, hubInputNumber(raw.delayMS, 0)),
      intensity: arcadeClamp(raw.intensity, 0.05, 1.35, 1),
      pan: arcadeClamp(raw.pan, -0.95, 0.95, 0),
      depth: arcadeClamp(raw.depth, -0.75, 0.75, 0),
      rate: arcadeClamp(raw.rate, 0.25, 2, 1),
    };
  }

  function playArcadeSFX(kind, options) {
    const audio = unlockArcadeAudio();
    if (!audio) return;
    const cue = String(kind || "move").trim().toLowerCase();
    const opts = arcadeSoundOptions(options);
    const heavy = Math.max(0.65, opts.intensity);
    if (cue === "confirm") {
      arcadeTone(audio, 220, 0.055, 0.08, "square", opts);
      arcadeTone(audio, 880, 0.09, 0.08, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 18 }));
      return;
    }
    if (cue === "round") {
      arcadeTone(audio, 196, 0.12, 0.075, "square", opts);
      arcadeTone(audio, 294, 0.12, 0.055, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 46 }));
      arcadeTone(audio, 392, 0.16, 0.05, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 92 }));
      return;
    }
    if (cue === "fight") {
      arcadeTone(audio, 330, 0.06, 0.075, "square", opts);
      arcadeTone(audio, 660, 0.075, 0.075, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 42 }));
      arcadeNoise(audio, 0.055, 0.04, "highpass", 1500, Object.assign({}, opts, { delayMS: opts.delayMS + 22 }));
      return;
    }
    if (cue === "ko" || cue === "match") {
      arcadeNoise(audio, 0.16, 0.095, "lowpass", 720, opts);
      arcadeSweep(audio, 190, 62, 0.32, 0.07, "sawtooth", opts);
      arcadeTone(audio, 82, 0.18, 0.08, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 65 }));
      return;
    }
    if (cue === "hit_light" || cue === "hit") {
      arcadeNoise(audio, 0.052, 0.075 * opts.intensity, "bandpass", 1900, opts);
      arcadeTone(audio, 118, 0.035, 0.05 * opts.intensity, "square", opts);
      arcadeTone(audio, 720, 0.026, 0.038 * opts.intensity, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 7 }));
      return;
    }
    if (cue === "hit_heavy") {
      arcadeNoise(audio, 0.082, 0.1 * heavy, "lowpass", 1100, opts);
      arcadeTone(audio, 74, 0.055, 0.075 * heavy, "square", opts);
      arcadeTone(audio, 540, 0.04, 0.054 * heavy, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 10 }));
      arcadeTone(audio, 1260, 0.024, 0.034 * heavy, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 22 }));
      return;
    }
    if (cue === "counter" || cue === "punish") {
      playArcadeSFX("hit_heavy", Object.assign({}, opts, { intensity: Math.min(1.25, opts.intensity + 0.12) }));
      arcadeTone(audio, cue === "punish" ? 990 : 1180, 0.075, 0.052, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 42 }));
      arcadeTone(audio, cue === "punish" ? 1320 : 1480, 0.05, 0.04, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 74 }));
      return;
    }
    if (cue === "launcher") {
      arcadeNoise(audio, 0.06, 0.07 * heavy, "highpass", 1100, opts);
      arcadeSweep(audio, 240, 980, 0.16, 0.06 * heavy, "sawtooth", opts);
      arcadeTone(audio, 1560, 0.04, 0.035, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 80 }));
      return;
    }
    if (cue === "block") {
      arcadeNoise(audio, 0.045, 0.058 * opts.intensity, "bandpass", 820, opts);
      arcadeTone(audio, 150, 0.035, 0.055 * opts.intensity, "square", opts);
      arcadeTone(audio, 270, 0.04, 0.035 * opts.intensity, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 10 }));
      return;
    }
    if (cue === "guard" || cue === "just_guard") {
      arcadeTone(audio, 420, 0.04, 0.05 * opts.intensity, "triangle", opts);
      arcadeTone(audio, 980, 0.035, 0.045 * opts.intensity, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 14 }));
      arcadeTone(audio, 1540, 0.04, 0.03, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 34 }));
      return;
    }
    if (cue === "guard_cancel") {
      playArcadeSFX("just_guard", opts);
      arcadeSweep(audio, 520, 1120, 0.12, 0.045, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 44 }));
      return;
    }
    if (cue === "armor") {
      arcadeNoise(audio, 0.09, 0.07 * heavy, "lowpass", 420, opts);
      arcadeTone(audio, 72, 0.08, 0.075 * heavy, "square", opts);
      arcadeTone(audio, 144, 0.06, 0.05 * heavy, "sawtooth", Object.assign({}, opts, { delayMS: opts.delayMS + 18 }));
      return;
    }
    if (cue === "throw") {
      arcadeNoise(audio, 0.075, 0.06 * heavy, "bandpass", 620, opts);
      arcadeSweep(audio, 420, 120, 0.11, 0.052 * heavy, "sawtooth", opts);
      arcadeTone(audio, 110, 0.065, 0.08 * heavy, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 24 }));
      return;
    }
    if (cue === "throw_tech") {
      arcadeTone(audio, 560, 0.035, 0.055, "square", opts);
      arcadeTone(audio, 1120, 0.05, 0.05, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 20 }));
      arcadeNoise(audio, 0.035, 0.045, "highpass", 1800, Object.assign({}, opts, { delayMS: opts.delayMS + 10 }));
      return;
    }
    if (cue === "surge" || cue === "surge_ready") {
      arcadeSweep(audio, cue === "surge" ? 160 : 320, cue === "surge" ? 920 : 1280, cue === "surge" ? 0.34 : 0.12, cue === "surge" ? 0.07 : 0.045, "sawtooth", opts);
      arcadeTone(audio, cue === "surge" ? 80 : 640, cue === "surge" ? 0.24 : 0.06, cue === "surge" ? 0.055 : 0.035, "square", Object.assign({}, opts, { delayMS: opts.delayMS + 38 }));
      return;
    }
    arcadeTone(audio, 440, 0.035, 0.045, "square", opts);
    arcadeTone(audio, 660, 0.04, 0.035, "triangle", Object.assign({}, opts, { delayMS: opts.delayMS + 12 }));
  }

  function arcadeConnectToOutput(audio, node, opts, nodes) {
    let tail = node;
    if (typeof audio.createStereoPanner === "function" && Math.abs(opts.pan) > 0.001) {
      const panner = audio.createStereoPanner();
      panner.pan.value = opts.pan;
      tail.connect(panner);
      tail = panner;
      nodes.push(panner);
    }
    tail.connect(arcadeOutput(audio));
  }

  function arcadeSetParam(param, value, time) {
    if (param && typeof param.setValueAtTime === "function") {
      param.setValueAtTime(value, time || 0);
      return;
    }
    if (param && Object.prototype.hasOwnProperty.call(param, "value")) {
      param.value = value;
    }
  }

  function arcadeRampParam(param, value, time, exponential) {
    if (param && exponential && typeof param.exponentialRampToValueAtTime === "function") {
      param.exponentialRampToValueAtTime(Math.max(0.0001, value), time);
      return;
    }
    if (param && typeof param.linearRampToValueAtTime === "function") {
      param.linearRampToValueAtTime(value, time);
      return;
    }
    arcadeSetParam(param, value, time);
  }

  function arcadeEnvelope(gain, now, volume, duration) {
    if (!gain || !gain.gain) return;
    arcadeSetParam(gain.gain, 0.0001, now);
    arcadeRampParam(gain.gain, Math.max(0.0001, volume), now + 0.006, true);
    arcadeRampParam(gain.gain, 0.0001, now + duration + 0.04, true);
  }

  function arcadeTrackVoice(record) {
    arcadeAudioState.active.push(record);
    while (arcadeAudioState.active.length > arcadeAudioState.voiceLimit) {
      releaseArcadeAudio(arcadeAudioState.active[0], true);
    }
  }

  function arcadeTone(audio, freq, duration, volume, type, options) {
    const opts = arcadeSoundOptions(options);
    const now = audio.currentTime || 0;
    const osc = audio.createOscillator();
    const gain = audio.createGain();
    osc.type = type || "square";
    arcadeSetParam(osc.frequency, freq * opts.rate, now);
    arcadeEnvelope(gain, now, volume * opts.intensity, duration);
    osc.connect(gain);
    const nodes = [osc, gain];
    arcadeConnectToOutput(audio, gain, opts, nodes);
    const record = { source: osc, nodes: [osc, gain] };
    record.nodes = nodes;
    arcadeTrackVoice(record);
    osc.onended = function() {
      releaseArcadeAudio(record, false);
    };
    const startAt = now + opts.delayMS / 1000;
    osc.start(startAt);
    osc.stop(startAt + duration + 0.08);
  }

  function arcadeSweep(audio, startFreq, endFreq, duration, volume, type, options) {
    const opts = arcadeSoundOptions(options);
    const now = audio.currentTime || 0;
    const osc = audio.createOscillator();
    const gain = audio.createGain();
    osc.type = type || "sawtooth";
    const startAt = now + opts.delayMS / 1000;
    arcadeSetParam(osc.frequency, Math.max(20, startFreq * opts.rate), startAt);
    arcadeRampParam(osc.frequency, Math.max(20, endFreq * opts.rate), startAt + duration, false);
    arcadeEnvelope(gain, startAt, volume * opts.intensity, duration);
    osc.connect(gain);
    const nodes = [osc, gain];
    arcadeConnectToOutput(audio, gain, opts, nodes);
    const record = { source: osc, nodes: nodes };
    arcadeTrackVoice(record);
    osc.onended = function() {
      releaseArcadeAudio(record, false);
    };
    osc.start(startAt);
    osc.stop(startAt + duration + 0.08);
  }

  function arcadeNoise(audio, duration, volume, filterType, frequency, options) {
    if (typeof audio.createBuffer !== "function" || typeof audio.createBufferSource !== "function") {
      arcadeTone(audio, frequency || 440, duration, volume * 0.7, "square", options);
      return;
    }
    const opts = arcadeSoundOptions(options);
    const sampleRate = Math.max(8000, audio.sampleRate || 44100);
    const length = Math.max(1, Math.floor(sampleRate * duration));
    const buffer = audio.createBuffer(1, length, sampleRate);
    const data = buffer.getChannelData(0);
    for (let i = 0; i < length; i += 1) {
      const falloff = 1 - i / length;
      data[i] = (Math.random() * 2 - 1) * falloff;
    }
    const source = audio.createBufferSource();
    const gain = audio.createGain();
    source.buffer = buffer;
    let tail = source;
    const nodes = [source, gain];
    if (typeof audio.createBiquadFilter === "function") {
      const filter = audio.createBiquadFilter();
      filter.type = filterType || "bandpass";
      if (filter.frequency) filter.frequency.value = Math.max(40, frequency || 1200);
      if (filter.Q) filter.Q.value = filter.type === "lowpass" ? 0.75 : 4.5;
      source.connect(filter);
      tail = filter;
      nodes.push(filter);
    }
    const now = audio.currentTime || 0;
    const startAt = now + opts.delayMS / 1000;
    arcadeEnvelope(gain, startAt, volume * opts.intensity, duration);
    tail.connect(gain);
    arcadeConnectToOutput(audio, gain, opts, nodes);
    const record = { source: source, nodes: nodes };
    arcadeTrackVoice(record);
    source.onended = function() {
      releaseArcadeAudio(record, false);
    };
    source.start(startAt);
    source.stop(startAt + duration + 0.08);
  }

  function releaseArcadeAudio(record, stop) {
    if (!record) return;
    const index = arcadeAudioState.active.indexOf(record);
    if (index >= 0) arcadeAudioState.active.splice(index, 1);
    if (record.source) {
      record.source.onended = null;
      if (stop && typeof record.source.stop === "function") {
        try {
          record.source.stop(0);
        } catch (_e) {}
      }
    }
    for (const node of record.nodes || []) {
      if (node && typeof node.disconnect === "function") {
        try {
          node.disconnect();
        } catch (_e) {}
      }
    }
  }

  function stopArcadeSFX() {
    arcadeAudioState.active.slice().forEach(function(record) {
      releaseArcadeAudio(record, true);
    });
  }

  function arcadeLayerGain(layer) {
    const gain = layer && typeof layer.gain === "number" ? layer.gain : 1;
    return Math.max(0, gain);
  }

  // arcadeMergePatchLayerOptions layers a SynthPatch layer's own pan/
  // delayMS/rate/envelope (see scene/audio.go's ToneLayer/SweepLayer/
  // NoiseLayer) on top of the patch-level options every layer starts
  // from, so a single arcadePlayPatch() call can fire layers that each
  // deviate from the shared pan/timing/rate.
  function arcadeMergePatchLayerOptions(layer, baseOpts) {
    const merged = Object.assign({}, baseOpts);
    if (layer && typeof layer.pan === "number") {
      merged.pan = arcadeClamp(layer.pan, -0.95, 0.95, baseOpts.pan);
    }
    if (layer && typeof layer.delayMS === "number" && layer.delayMS > 0) {
      merged.delayMS = Math.max(0, baseOpts.delayMS + layer.delayMS);
    }
    if (layer && typeof layer.rate === "number" && layer.rate > 0) {
      merged.rate = arcadeClamp(layer.rate, 0.05, 4, baseOpts.rate);
    }
    if (layer && layer.envelope && typeof layer.envelope === "object") {
      merged.envelope = layer.envelope;
    }
    return merged;
  }

  // arcadePlayPatch fires a SynthPatch (scene/audio.go): an arbitrary
  // combination of tone/sweep/noise layers, each an arcadeTone/
  // arcadeSweep/arcadeNoise call under the hood. This is the trigger seam
  // arcadeAudio previously lacked outside playArcadeSFX's fixed, hard-coded
  // cue vocabulary — see window.__gosx.arcadeAudio below and
  // 20-scene-mount.js's "audio" hub-event handling for how a server-driven
  // scene reaches this.
  function arcadePlayPatch(patch, options) {
    const audio = unlockArcadeAudio();
    if (!audio || !patch || typeof patch !== "object") return;
    const baseOpts = arcadeSoundOptions(options);
    (Array.isArray(patch.tones) ? patch.tones : []).forEach(function(tone) {
      if (!tone || typeof tone !== "object") return;
      arcadeTone(
        audio,
        hubInputNumber(tone.frequency, 440),
        Math.max(0.01, hubInputNumber(tone.duration, 0.05)),
        arcadeLayerGain(tone),
        tone.waveform || "square",
        arcadeMergePatchLayerOptions(tone, baseOpts),
      );
    });
    (Array.isArray(patch.sweeps) ? patch.sweeps : []).forEach(function(sweep) {
      if (!sweep || typeof sweep !== "object") return;
      arcadeSweep(
        audio,
        hubInputNumber(sweep.startFrequency, 440),
        hubInputNumber(sweep.endFrequency, 220),
        Math.max(0.01, hubInputNumber(sweep.duration, 0.12)),
        arcadeLayerGain(sweep),
        sweep.waveform || "sawtooth",
        arcadeMergePatchLayerOptions(sweep, baseOpts),
      );
    });
    (Array.isArray(patch.noises) ? patch.noises : []).forEach(function(noise) {
      if (!noise || typeof noise !== "object") return;
      arcadeNoise(
        audio,
        Math.max(0.01, hubInputNumber(noise.duration, 0.08)),
        arcadeLayerGain(noise),
        noise.filterType || "bandpass",
        hubInputNumber(noise.filterFrequency, 1200),
        arcadeMergePatchLayerOptions(noise, baseOpts),
      );
    });
  }

  // window.__gosx.arcadeAudio exposes the procedural synth engine outside
  // its previous sole caller (createHubInputController's onHubMessage,
  // above). This is the "minimal trigger seam" scene/audio.go's AudioCue
  // doc references: any code holding window.__gosx (e.g.
  // 20-scene-mount.js's "audio" hub-event handling) can fire a built-in
  // named cue or an inline SynthPatch without depending on this closure.
  window.__gosx.arcadeAudio = {
    play: playArcadeSFX,
    playPatch: arcadePlayPatch,
    stop: stopArcadeSFX,
    unlock: unlockArcadeAudio,
  };
