// Wave constants from scene/ocean_waves.json; regenerate with scripts/generate-ocean-waves.mjs.
(function() {
  const constants = {"ratio":[1,0.73,0.53,0.39,0.28,0.21],"angle":[0,0.38,-0.46,0.83,-0.95,1.4],"weight":[1,0.62,0.42,0.28,0.19,0.13],"gravity":9.81,"phaseStep":0.6180339887};
  // @ts-ignore TS7006 -- shared wave packer is also evaluated as plain JS by Node.
  function write(ocean, quality, out) {
    const o = ocean || {}, count = quality === "low" ? 4 : 6;
    const height = Number.isFinite(o.waveHeight) ? o.waveHeight : 0.8;
    const length = Number.isFinite(o.waveLength) ? o.waveLength : 18;
    const chop = Number.isFinite(o.choppiness) ? o.choppiness : 0.6;
    const wind = (Number.isFinite(o.windDirection) ? o.windDirection : 0) * Math.PI / 180;
    const speed = Math.fround(Number.isFinite(o.speed) ? o.speed : 1);
    let weights = 0;
    for (let i = 0; i < count; i++) weights += constants.weight[i] ** 2;
    out[27] = count;
    for (let i = 0; i < count; i++) {
      const base = 36 + i * 8, angle = wind + constants.angle[i];
      const k = 2 * Math.PI / (length * constants.ratio[i]);
      out[base] = Math.sin(angle); out[base + 1] = Math.cos(angle); out[base + 2] = k;
      out[base + 3] = Math.sqrt(constants.gravity * k) * speed;
      out[base + 4] = constants.weight[i] * height / Math.sqrt(8 * weights);
      out[base + 5] = chop / (k * count);
      out[base + 6] = ((i * constants.phaseStep) % 1) * 2 * Math.PI;
    }
    return out;
  }
  window.__gosx_scene3d_ocean_waves = { constants, write };
})();
