// The Go query embeds this same table. Keep GPU and CPU wave packing together.
import fs from 'node:fs';
const table = fs.readFileSync(new URL('../scene/ocean_waves.json', import.meta.url), 'utf8').trim();
const source = `// Wave constants from scene/ocean_waves.json; regenerate with scripts/generate-ocean-waves.mjs.
(function() {
  const constants = ${table};
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
`;
const target = new URL('../client/runtime/scene3d/ocean-waves.ts', import.meta.url);
if (process.argv.includes('--check')) {
  if (fs.readFileSync(target, 'utf8') !== source) throw new Error('regenerate ocean-waves.ts');
} else fs.writeFileSync(target, source);
