// Opt in: GOSX_BROWSER_TEST=1 CHROME_BIN=/path/to/chrome node --test client/js/browser/deferred-scene3d.test.cjs
const test = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');

const enabled = process.env.GOSX_BROWSER_TEST === '1';
test('deferred Scene3D respects CSP, navigation, retry and disposal', {skip: !enabled}, async () => {
  const {chromium} = require('playwright');
  const js = path.resolve(__dirname, '..');
  const navigation = path.resolve(js, '../runtime/host/navigation-runtime.min.js');
  const base = fs.readFileSync(path.join(js, 'bootstrap-feature-scene3d.js'));
  const manifest = {version: '0.1.0', engines: [{id: 'scene', component: 'GoSXScene3D', kind: 'surface', mountId: 'mount', deferUntilOpen: true, props: {width: 240, height: 140, scene: {objects: []}}}], runtime: {path: ''}};
  let holdHydrate = false, releaseHydrate;
  function html(name) {
    const nonce = name === 'a' ? 'nonceA' : 'nonceB';
    const doc = {version: 1, page: {path: '/' + name}, enhancement: {bootstrap: true, navigation: true}, assets: {bootstrapMode: 'full', manifest: true, bootstrapFeatureEnginesPath: '/bootstrap-feature-engines.js', bootstrapFeatureScene3dPath: '/bootstrap-feature-scene3d.js'}};
    return `<!doctype html><html><head><meta name="gosx-head-start" content=""><script id="gosx-manifest" type="application/json">${JSON.stringify(manifest)}</script><script id="gosx-document" type="application/json">${JSON.stringify(doc)}</script><script defer nonce="${nonce}" data-gosx-script="bootstrap" src="/bootstrap-runtime.js"></script><script defer nonce="${nonce}" data-gosx-navigation="true" src="/navigation.js"></script><template id="gosx-deferred-scene3d">${holdHydrate ? `<script nonce="${nonce}" data-gosx-script="feature-scene3d-hydrate" src="/hydrate.js"></script>` : ""}<script nonce="${nonce}" data-gosx-script="feature-scene3d" data-gosx-scene3d-webgl-url="/${name}-webgl.js" src="/bootstrap-feature-scene3d.js"></script></template><meta name="gosx-head-end" content=""></head><body><details><summary>Open</summary><div id="mount">Fallback</div></details></body></html>`;
  }
  let sceneRequests = 0;
  const server = http.createServer((req, res) => {
    if (req.url === '/hydrate.js') { res.setHeader('content-type', 'text/javascript'); releaseHydrate = () => res.end(''); return; }
    if (req.url === '/navigation.js') { res.setHeader('content-type', 'text/javascript'); return res.end(fs.readFileSync(navigation)); }
    if (req.url === '/bootstrap-feature-scene3d.js') {
      sceneRequests++;
      if (sceneRequests === 1) { res.statusCode = 503; return res.end('unavailable'); }
      res.setHeader('content-type', 'text/javascript'); return res.end(base);
    }
    if (req.url.endsWith('.js')) {
      const file = path.join(js, path.basename(req.url));
      if (fs.existsSync(file)) { res.setHeader('content-type', 'text/javascript'); return res.end(fs.readFileSync(file)); }
      res.statusCode = 404; return res.end();
    }
    const name = req.url === '/b' ? 'b' : 'a';
    res.setHeader('Content-Security-Policy', `script-src 'nonce-${name === 'a' ? 'nonceA' : 'nonceB'}'`);
    res.setHeader('content-type', 'text/html'); return res.end(html(name));
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch({headless: true, executablePath: process.env.CHROME_BIN || undefined, args: ['--no-sandbox', '--use-gl=swiftshader']});
  try {
    const page = await browser.newPage();
    await page.goto(`http://127.0.0.1:${server.address().port}/a`);
    await page.waitForFunction(() => window.__gosx?.ready);
    await page.evaluate(() => window.__gosx_page_nav.navigate('/b'));
    await page.waitForURL('**/b');
    await page.waitForFunction(() => window.__gosx?.ready);
    assert.equal(sceneRequests, 0, 'closed deferred scene makes no base request');
    await page.locator('summary').click();
    await page.waitForFunction(() => window.__gosx?.listIssues?.().some(issue => issue.ref === 'scene3d'));
    assert.equal(await page.locator('#mount').textContent(), 'Fallback');
    await page.locator('summary').click();
    await page.locator('summary').click();
    await page.waitForFunction(() => window.__gosx?.engines?.has('scene'));
    assert.equal(sceneRequests, 2, 'a failed request is retried once on reopen');
    const script = page.locator('script[data-gosx-script="feature-scene3d"]');
    assert.deepEqual(await script.evaluate(node => ({nonce: node.nonce, webgl: node.dataset.gosxScene3dWebglUrl})), {nonce: 'nonceA', webgl: '/b-webgl.js'});
    await page.evaluate(() => window.__gosx.host.lifecycle.disposePage());
    assert.equal(await page.evaluate(() => window.__gosx.engines.has('scene')), false);
    // Start loading A, navigate before its hydrate completes, then open B.
    // The shared promise must apply B's metadata after A's script arrives.
    holdHydrate = true;
    const crossing = await browser.newPage();
    await crossing.goto(`http://127.0.0.1:${server.address().port}/a`);
    await crossing.waitForFunction(() => window.__gosx?.ready);
    await crossing.locator('summary').click();
    await crossing.waitForFunction(() => !!document.querySelector('script[data-gosx-script="feature-scene3d-hydrate"]'));
    await crossing.evaluate(() => window.__gosx_page_nav.navigate('/b'));
    await crossing.waitForURL('**/b');
    await crossing.waitForFunction(() => window.__gosx?.ready);
    await crossing.locator('summary').click();
    assert.ok(releaseHydrate);
    releaseHydrate();
    await crossing.waitForFunction(() => window.__gosx?.engines?.has('scene'));
    assert.equal(await crossing.locator('script[data-gosx-script="feature-scene3d"]').evaluate(node => node.dataset.gosxScene3dWebglUrl), '/b-webgl.js');
    await crossing.close();
  } finally {
    await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
});
