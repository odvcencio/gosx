#!/usr/bin/env node
// Measure active Scene3D draw cadence on the reference Windows GPU. Alternating
// wheel input keeps event-driven scenes rendering during the 240-frame sample.
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import {
  hasRepresentativeDrawCadence,
  MIN_DRAW_CADENCE_SAMPLES,
} from './showcase-gpu-cadence.mjs';

const require = createRequire(import.meta.url);
const toolsDir = process.env.SHOWCASE_TOOLS_DIR || '/home/draco/.local/state/nightwatch/reports/gosx-showcase/tools';
const { chromium } = require(path.join(toolsDir, 'node_modules/playwright'));
const [base, outDir, label, ...paths] = process.argv.slice(2);
if (!base || !outDir || !label || paths.length === 0) {
  throw new Error('usage: showcase-gpu-capture.mjs <base-url> <out-dir> <label> <path...>');
}

const initScript = `(() => {
  const nativeRAF = window.requestAnimationFrame.bind(window);
  window.__probe = { draws: [], cpu: [], rafT: 0 };
  let last = -1;
  window.requestAnimationFrame = callback => nativeRAF(timestamp => {
    window.__probe.rafT = timestamp;
    const start = performance.now();
    try { return callback(timestamp); }
    finally { window.__probe.cpu.push(performance.now() - start); }
  });
  const mark = () => {
    const timestamp = window.__probe.rafT;
    if (timestamp > 0 && timestamp !== last) {
      last = timestamp;
      window.__probe.draws.push(timestamp);
    }
  };
  if (window.GPUCanvasContext) {
    const original = GPUCanvasContext.prototype.getCurrentTexture;
    GPUCanvasContext.prototype.getCurrentTexture = function (...args) {
      const texture = original.apply(this, args);
      mark();
      return texture;
    };
  }
  if (window.WebGL2RenderingContext) {
    for (const name of ['drawArrays', 'drawElements', 'drawArraysInstanced', 'drawElementsInstanced']) {
      const original = WebGL2RenderingContext.prototype[name];
      WebGL2RenderingContext.prototype[name] = function (...args) {
        const result = original.apply(this, args);
        mark();
        return result;
      };
    }
  }
})();`;

const sortedStats = values => {
  if (!values.length) return null;
  const sorted = [...values].sort((a, b) => a - b);
  return {
    n: values.length,
    p50: Number(sorted[Math.floor(sorted.length * 0.5)].toFixed(2)),
    p95: Number(sorted[Math.floor(sorted.length * 0.95)].toFixed(2)),
    max: Number(sorted.at(-1).toFixed(2)),
  };
};

fs.mkdirSync(outDir, { recursive: true });
const browser = await chromium.connectOverCDP(process.env.CDP_URL || 'http://172.29.240.1:8119');
const rows = [];
try {
  for (const route of paths) {
    for (const backend of ['webgpu', 'webgl']) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 });
      const page = await context.newPage();
      await page.addInitScript(initScript);
      if (backend === 'webgl') await page.addInitScript('window.__gosx_scene3d_force_webgl = true;');
      const errors = [];
      page.on('pageerror', error => errors.push(`pageerror: ${String(error).slice(0, 250)}`));
      page.on('console', message => {
        if (message.type() === 'error') errors.push(`console: ${message.text().slice(0, 250)}`);
      });
      page.on('response', response => {
        if (response.status() >= 400) errors.push(`http ${response.status()} ${response.url()}`);
      });
      page.on('requestfailed', request => {
        const failure = request.failure()?.errorText || 'unknown request failure';
        if (failure === 'net::ERR_ABORTED' && new URL(request.url()).pathname === '/_gosx/client-events') return;
        errors.push(`requestfailed: ${failure} ${request.url()}`);
      });

      const row = { path: route, backend, vp: 'd1440', errors };
      const startedAt = Date.now();
      try {
        await page.goto(base + route, { waitUntil: 'load', timeout: 60000 });
        row.loadMs = Date.now() - startedAt;
        row.firstDrawMs = await page.evaluate(() => new Promise(resolve => {
          const start = performance.now();
          const check = () => {
            if (window.__probe.draws.length) return resolve(Math.round(window.__probe.draws[0]));
            if (performance.now() - start > 20000) return resolve(null);
            setTimeout(check, 50);
          };
          check();
        }));
        if (row.firstDrawMs == null) throw new Error('no GPU draw was observed within 20 seconds');

        const canvas = page.locator('canvas').first();
        const bounds = await canvas.boundingBox();
        if (!bounds) throw new Error('Scene3D canvas has no visible bounds');
        const centerX = bounds.x + bounds.width / 2;
        const centerY = bounds.y + bounds.height / 2;
        await page.evaluate(() => { window.__probe.draws = []; window.__probe.cpu = []; });
        await page.mouse.move(centerX, centerY);

        const movement = (async () => {
          for (let index = 0; index < 240; index++) {
            await page.mouse.wheel(0, index % 2 === 0 ? 2 : -2);
          }
        })();

        const sample = await page.evaluate(async () => {
          const frameIntervals = [];
          let previous = 0;
          await new Promise(resolve => {
            const next = timestamp => {
              if (previous) frameIntervals.push(timestamp - previous);
              previous = timestamp;
              if (frameIntervals.length < 240) requestAnimationFrame(next);
              else resolve();
            };
            requestAnimationFrame(next);
          });
          const draws = window.__probe.draws;
          const cadence = draws.slice(1).map((timestamp, index) => timestamp - draws[index]);
          const marker = document.querySelector('[data-gosx-scene3d-renderer]') || document.querySelector('[data-gosx-scene3d-backend]');
          const attrs = {};
          if (marker) for (const attribute of marker.getAttributeNames()) {
            if (/renderer|backend|dropped|quality|pacing|postfx/.test(attribute)) attrs[attribute] = (marker.getAttribute(attribute) || '').slice(0, 160);
          }
          let adapter = null;
          try {
            const gl = document.createElement('canvas').getContext('webgl2');
            const extension = gl?.getExtension('WEBGL_debug_renderer_info');
            if (extension) adapter = gl.getParameter(extension.UNMASKED_RENDERER_WEBGL);
          } catch (_) {}
          return {
            drawCount: draws.length,
            drawIntervals: cadence,
            frameIntervals,
            cpu: window.__probe.cpu,
            attrs,
            adapter,
          };
        });
        await movement;
        const renderer = sample.attrs['data-gosx-scene3d-renderer'];
        const wantedRenderer = backend === 'webgpu' ? 'webgpu' : 'webgl';
        if (renderer !== wantedRenderer) throw new Error(`requested ${backend}, runtime selected ${renderer || 'no renderer'}`);
        const drawCadence = sortedStats(sample.drawIntervals);
        const raf = sortedStats(sample.frameIntervals);
        if (!hasRepresentativeDrawCadence(drawCadence) || !raf) {
          throw new Error(
            `alternating wheel input produced ${sample.drawCount} draws and ${sample.frameIntervals.length} RAF intervals; ` +
            `draw cadence needs at least ${MIN_DRAW_CADENCE_SAMPLES} intervals`,
          );
        }
        row.stats = {
          renderedFps: Number((1000 / raf.p50).toFixed(1)),
          drawCadence,
          raf,
          rafCpu: sortedStats(sample.cpu),
          attrs: sample.attrs,
          adapter: sample.adapter,
          canvases: await page.locator('canvas').count(),
        };
        await page.screenshot({ path: path.join(outDir, `${route.replace(/^\//, '').replaceAll('/', '_') || 'home'}-${backend}-d1440.png`) });
      } catch (error) {
        row.error = String(error).slice(0, 500);
      }

      rows.push(row);
      process.stderr.write(`${label} ${route} ${backend} draw=${row.stats?.drawCadence?.n || 0} p95=${row.stats?.drawCadence?.p95 ?? 'null'} errors=${errors.length}${row.error ? ` error=${row.error}` : ''}\n`);
      await context.close();
      fs.writeFileSync(path.join(outDir, `gpu-${label}.json`), JSON.stringify(rows, null, 1));
    }
  }
} finally {
  await browser.close();
}
