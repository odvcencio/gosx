const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const http = require('node:http');
const path = require('node:path');

// Optional standalone browser contract using an existing Playwright install.
const modulePath = process.env.GOSX_PLAYWRIGHT_MODULE;

test('managed navigation failures preserve long-page drafts and allow retry', {skip: !modulePath}, async () => {
  const {chromium} = require(modulePath);
  const runtime = fs.readFileSync(path.join(__dirname, '../runtime/host/navigation-runtime.min.js'), 'utf8');
  const server = http.createServer((req, res) => {
    if (req.url === '/runtime.js') {
      res.writeHead(200, {'content-type': 'text/javascript'});
      return res.end(runtime);
    }
    if (req.url === '/grid.css') {
      res.writeHead(200, {'content-type': 'text/css'});
      return res.end('#grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:4px} #grid > *{display:block;min-height:48px;border:1px solid black}');
    }
    if (req.url === '/next') {
      res.writeHead(200, {'content-type': 'text/html'});
      return res.end('<!doctype html><title>Destination</title><main id="destination">Done</main>');
    }
    res.writeHead(200, {
      'content-type': 'text/html',
      'content-security-policy': "default-src 'self'; script-src 'self'; style-src 'self'",
    });
    res.end('<!doctype html><title>Source</title><link rel="stylesheet" href="/grid.css">' +
      '<main id="source"><button id="show" data-gosx-disclosure-target="#dialog">Open dialog</button>' +
      '<section id="dialog" role="dialog" aria-modal="true" data-gosx-disclosure hidden>' +
      '<input id="draft" value="original"><div id="grid">' +
      '<div id="before">Before</div><a id="open" href="/next" data-gosx-link data-gosx-prefetch="none">Open</a>' +
      '<div id="after">After</div></div></section>' + '<p>long page content</p>'.repeat(300) +
      '</main><script src="/runtime.js"></script>');
  });
  let browser;
  try {
    await new Promise((resolve, reject) => server.listen(0, '127.0.0.1', resolve).on('error', reject));
    const base = 'http://127.0.0.1:' + server.address().port;
    browser = await chromium.launch({headless: true, executablePath: process.env.GOSX_BROWSER_EXECUTABLE || undefined});
    for (const mode of ['popover', 'native-alert']) {
      for (const width of [390, 1280]) {
        const page = await browser.newPage({viewport: {width, height: 600}});
        if (mode === 'native-alert') {
          await page.addInitScript(() => {
            Object.defineProperty(HTMLElement.prototype, 'showPopover', {configurable: true, value: undefined});
            Object.defineProperty(HTMLDialogElement.prototype, 'show', {configurable: true, value: undefined});
          });
        }
        const dialogs = [];
        page.on('dialog', async dialog => {
          dialogs.push({type: dialog.type(), message: dialog.message()});
          await dialog.accept();
        });
        let requested, release;
        const seen = new Promise(resolve => { requested = resolve; });
        const gate = new Promise(resolve => { release = resolve; });
        let requests = 0;
        await page.route('**/next', async route => {
          requests++;
          if (requests === 1) {
            requested();
            await gate;
            await route.abort('failed');
          } else if (requests === 2) {
            await route.fulfill({status: 503, body: 'unavailable'});
          } else {
            await route.continue();
          }
        });
        await page.goto(base + '/source');
        await page.locator('#show').click();
        assert.equal(await page.locator('#dialog').isVisible(), true);
        await page.evaluate(() => {
          window.__sourceDocument = document;
          window.__draftNode = document.querySelector('#draft');
          window.__focusEvents = [];
          window.__draftNode.addEventListener('blur', () => window.__focusEvents.push('blur'));
          window.__draftNode.addEventListener('focus', () => window.__focusEvents.push('focus'));
        });
        const rects = () => page.locator('#grid > *').evaluateAll(nodes => nodes.map(node => {
          const rect = node.getBoundingClientRect();
          return [rect.x, rect.y, rect.width, rect.height].map(value => Math.round(value * 100) / 100);
        }));
        const before = await rects();
        await page.locator('#open').click({noWaitAfter: true});
        await seen;
        await page.locator('#draft').fill('unsaved during request');
        await page.locator('#draft').focus();
        await page.evaluate(() => { window.scrollTo(0, 0); window.__focusEvents = []; });
        release();
        if (mode === 'popover') {
          await page.getByRole('alert').waitFor();
        } else {
          await page.waitForFunction(() => document.documentElement.getAttribute('data-gosx-navigation-state') === 'idle');
        }
        assert.equal(page.url(), base + '/source');
        assert.equal(await page.locator('#draft').inputValue(), 'unsaved during request');
        assert.deepEqual(await page.evaluate(() => ({
          same: document === window.__sourceDocument && document.querySelector('#draft') === window.__draftNode,
          focused: document.activeElement === window.__draftNode,
          scroll: scrollY,
          events: window.__focusEvents,
        })), {same: true, focused: true, scroll: 0, events: []});
        assert.deepEqual(await rects(), before);
        if (mode === 'popover') {
          assert.equal(await page.getByRole('alert').isVisible(), true);
          assert.equal(await page.getByRole('alert').getAttribute('style'), null);
        } else {
          assert.equal(dialogs.length, 1);
          assert.equal(dialogs[0].type, 'alert');
          assert.match(dialogs[0].message, /try the link again/i);
        }
        await page.locator('#open').click();
        await page.waitForFunction(() => document.documentElement.getAttribute('data-gosx-navigation-state') === 'idle');
        assert.equal(requests, 2);
        assert.deepEqual(await rects(), before);
        if (mode === 'popover') {
          assert.equal(await page.getByRole('alert').count(), 1);
          await page.locator('#draft').focus();
          await page.evaluate(() => { window.__focusEvents = []; });
          await page.keyboard.press('Escape');
          assert.equal(await page.getByRole('alert').count(), 0);
          assert.equal(await page.locator('#dialog').isVisible(), true);
          assert.equal(await page.locator('#draft').inputValue(), 'unsaved during request');
          assert.deepEqual(await page.evaluate(() => ({
            focused: document.activeElement === window.__draftNode,
            events: window.__focusEvents,
          })), {focused: true, events: []});
          await page.keyboard.press('Escape');
          assert.equal(await page.locator('#dialog').isHidden(), true);
          await page.locator('#show').click();
          assert.equal(await page.locator('#draft').inputValue(), 'unsaved during request');
        } else {
          assert.equal(dialogs.length, 2);
        }
        await page.locator('#open').click();
        await page.locator('#destination').waitFor();
        assert.equal(page.url(), base + '/next');
        assert.equal(requests, 3);
        await page.close();
      }
    }
  } finally {
    if (browser) await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
});
