import fs from 'node:fs';
import path from 'node:path';
import { brotliCompressSync, constants as zlibConstants } from 'node:zlib';
import {
  hasRepresentativeDrawCadence,
  MIN_DRAW_CADENCE_SAMPLES,
} from './showcase-gpu-cadence.mjs';

const [evidenceDir, distDir, outFile] = process.argv.slice(2);
if (!evidenceDir || !distDir || !outFile) {
  throw new Error('usage: node scripts/showcase-receipts.mjs <evidence-dir> <dist-dir> <output-json>');
}

const readJSON = file => JSON.parse(fs.readFileSync(file, 'utf8'));
const meta = readJSON(path.join(evidenceDir, 'measurement-meta.json'));
const slug = value => value.replace(/^\//, '').replaceAll('/', '_') || 'home';
const median = values => {
  const sorted = [...values].sort((a, b) => a - b);
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
};

const lighthouseFiles = fs.readdirSync(path.join(evidenceDir, 'lighthouse'));
const lhPages = [];
let lighthouseVersion = '';
let userAgent = '';
const lighthousePaths = ['/', '/docs', '/docs/getting-started/', '/demos/', meta.featuredPath, '/capabilities/', '/performance/'];
for (const route of lighthousePaths) {
  const runFiles = [1, 2, 3].map(run => {
    const file = path.join(evidenceDir, 'lighthouse', `${slug(route)}-run${run}.json`);
    if (!lighthouseFiles.includes(path.basename(file))) throw new Error(`missing Lighthouse run: ${file}`);
    return readJSON(file);
  });
  for (const [index, report] of runFiles.entries()) {
    const cls = report.audits?.['cumulative-layout-shift']?.numericValue;
    if (!report.categories || typeof cls !== 'number') {
      throw new Error(`incomplete Lighthouse result for ${route} run ${index + 1}; inspect ${slug(route)}-run${index + 1}.err`);
    }
  }
  lighthouseVersion ||= runFiles[0].lighthouseVersion || 'unknown Lighthouse';
  userAgent ||= runFiles[0].userAgent || runFiles[0].environment?.networkUserAgent || 'unknown Chrome';
  const runs = runFiles.map(report => {
    const categories = report.categories;
    const value = id => Math.round(categories[id].score * 100);
    return {
      performance: value('performance'),
      accessibility: value('accessibility'),
      bestPractices: value('best-practices'),
      seo: value('seo'),
      cls: Number(report.audits['cumulative-layout-shift'].numericValue.toFixed(4)),
    };
  });
  const keys = Object.keys(runs[0]);
  const med = Object.fromEntries(keys.map(key => [key, Number(median(runs.map(run => run[key])).toFixed(4))]));
  lhPages.push({ path: route, runs, median: med });
}

const gpuRows = readJSON(path.join(evidenceDir, 'gpu', 'gpu-showcase-receipts.json'));
const desktopRows = gpuRows.filter(row => row.vp === 'd1440');
const scenesByPath = new Map();
const titles = new Map([
  ['/demos/tabletop', 'Tabletop'], ['/demos/tabletop/', 'Tabletop'], ['/demos/showreel/', 'Showreel'],
  ['/demos/checkers/', 'Chinese Checkers'], ['/demos/beacon', 'Blackglass Coast'], ['/demos/orrery/', 'Lodestar Meridian'],
  ['/demos/water', 'Water'], ['/demos/scene3d/', 'Geometry Zoo'],
  ['/demos/html-surface/', 'HTML Surface'], ['/demos/scene3d-bench', 'Scene3D Bench'],
  ['/demos/lodestar/', 'Lodestar Meridian'],
]);
for (const row of desktopRows) {
  if (row.errors?.length || row.error || !row.stats?.rafCpu || row.firstDrawMs == null) {
    throw new Error(`incomplete GPU timing at ${row.path} (${row.backend}): ${JSON.stringify(row.errors || row.error)}`);
  }
  if (!hasRepresentativeDrawCadence(row.stats?.drawCadence)) {
    throw new Error(
      `GPU timing at ${row.path} (${row.backend}) needs at least ${MIN_DRAW_CADENCE_SAMPLES} draw intervals; ` +
      `found ${row.stats?.drawCadence?.n ?? 0}`,
    );
  }
  const rendererBackend = row.backend === 'webgl' ? 'webgl2' : row.backend;
  if (rendererBackend !== 'webgpu' && rendererBackend !== 'webgl2') throw new Error(`unexpected GPU backend ${rendererBackend}`);
  const backend = rendererBackend === 'webgpu' ? 'WebGPU' : 'WebGL2';
  const selected = row.stats.attrs?.['data-gosx-scene3d-renderer'];
  const expectedRenderer = rendererBackend === 'webgpu' ? 'webgpu' : 'webgl';
  if (selected !== expectedRenderer) {
    throw new Error(`${row.path} requested ${backend} but the runtime reported ${selected || 'no renderer'}`);
  }
  const scene = scenesByPath.get(row.path) || { path: row.path, title: titles.get(row.path) || row.path, backends: [] };
  scene.backends.push({
    backend,
    firstDrawMs: Number(row.firstDrawMs.toFixed(2)),
    cadenceP50Ms: Number(row.stats.drawCadence.p50.toFixed(2)),
    cadenceP95Ms: Number(row.stats.drawCadence.p95.toFixed(2)),
    rafCpuP95Ms: Number(row.stats.rafCpu.p95.toFixed(2)),
  });
  scenesByPath.set(row.path, scene);
}
const scenes = [...scenesByPath.values()].sort((a, b) => a.path.localeCompare(b.path));
for (const scene of scenes) {
  scene.backends.sort((a, b) => a.backend.localeCompare(b.backend));
  if (scene.backends.length !== 2 || !scene.backends.some(row => row.backend === 'WebGPU') || !scene.backends.some(row => row.backend === 'WebGL2')) {
    throw new Error(`${scene.path} needs measured WebGPU and WebGL2 rows at 1440x900`);
  }
}

const browserMeta = readJSON(path.join(evidenceDir, 'gpu', 'browser-version.json'));
const browserVersion = browserMeta.Browser || browserMeta['User-Agent'] || 'Windows Chrome';

function listFiles(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap(entry => {
    const full = path.join(dir, entry.name);
    return entry.isDirectory() ? listFiles(full) : [full];
  });
}
const files = listFiles(distDir);
const runtimeAssetPath = file => {
  const relative = path.relative(distDir, file).split(path.sep).join('/');
  return /^(?:client\/js|assets\/runtime)\//i.test(relative);
};
const jsFiles = files.filter(file => /\.js$/i.test(file) && runtimeAssetPath(file));
const wasmFiles = files.filter(file => /\.wasm$/i.test(file) && runtimeAssetPath(file));
const relevantJS = jsFiles.filter(file => /bootstrap/i.test(path.basename(file)) || /bootstrap[\\/]chunks/i.test(file));
if (!relevantJS.length) throw new Error(`no bootstrap JavaScript chunks found under ${distDir}`);
if (!wasmFiles.length) throw new Error(`no WASM runtime assets found under ${distDir}`);
const bundles = [...relevantJS.map(file => ({ file, kind: 'client-js' })), ...wasmFiles.map(file => ({ file, kind: 'wasm' }))]
  .sort((a, b) => a.file.localeCompare(b.file))
  .map(({ file, kind }) => {
    const bytes = fs.readFileSync(file);
    return {
      name: path.basename(file),
      kind,
      path: path.relative(distDir, file).split(path.sep).join('/'),
      rawBytes: bytes.length,
      brotliBytes: brotliCompressSync(bytes, { params: { [zlibConstants.BROTLI_PARAM_QUALITY]: 11 } }).length,
    };
  });

const receipt = {
  schemaVersion: 1,
  measuredAt: meta.measuredAt,
  commit: meta.commit,
  tree: meta.tree,
  featuredPath: meta.featuredPath,
  machine: meta.machine,
  lighthouse: {
    browser: `${userAgent} · Lighthouse ${lighthouseVersion}`,
    method: 'Lighthouse mobile emulation against the local production-shaped build; fresh browser profile for each run; median of three cold runs.',
    loadAverageStart: meta.lighthouseLoadAverageStart,
    loadAverageEnd: meta.lighthouseLoadAverageEnd,
    runCount: 3,
    pages: lhPages,
  },
  gpu: {
    label: 'RTX 5070 Ti desktop, not a mid-range laptop',
    browser: browserVersion,
    method: 'Windows Chrome with ANGLE D3D11 at 1440x900; forced WebGL2 runs; each scene received 240 alternating 2 px wheel-zoom inputs while 240 animation frames were sampled.',
    scenes,
  },
  bundles,
  quickstart: {
    command: 'go install m31labs.dev/gosx/cmd/gosx@latest',
    method: 'Cold run used fresh isolated GOMODCACHE and GOCACHE directories. Warm run reused both caches. Times include compilation and module download.',
    coldSeconds: meta.quickstart.coldSeconds,
    warmSeconds: meta.quickstart.warmSeconds,
  },
};

const requiredPages = new Set(['/', '/docs', '/docs/getting-started/', '/demos/', '/capabilities/', '/performance/']);
for (const page of receipt.lighthouse.pages) requiredPages.delete(page.path);
if (requiredPages.size) throw new Error(`missing required Lighthouse paths: ${[...requiredPages].join(', ')}`);
if (!receipt.lighthouse.pages.some(page => page.path === meta.featuredPath)) throw new Error('missing featured demo Lighthouse run');
if (!receipt.gpu.scenes.length) throw new Error('missing GPU scene receipts');
if (!receipt.bundles.some(bundle => bundle.kind === 'client-js') || !receipt.bundles.some(bundle => bundle.kind === 'wasm')) throw new Error('missing JS or WASM bundle sizes');

fs.writeFileSync(outFile, JSON.stringify(receipt, null, 2) + '\n');
process.stdout.write(`Wrote ${receipt.lighthouse.pages.length} Lighthouse pages, ${receipt.gpu.scenes.length} GPU scenes, and ${receipt.bundles.length} bundles.\n`);
