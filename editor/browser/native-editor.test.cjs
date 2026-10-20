const assert = require('node:assert/strict');
const {readFile} = require('node:fs/promises');
const path = require('node:path');
const {after, before, test} = require('node:test');

// Optional standalone browser contract suite. Point GOSX_PLAYWRIGHT_MODULE at
// an existing Playwright installation; this module needs no npm dependencies.
const modulePath = process.env.GOSX_PLAYWRIGHT_MODULE;
let browser, script, intelligenceScript;
before(async () => {
  if (!modulePath) return;
  const {chromium} = require(modulePath);
  browser = await chromium.launch({
    executablePath: process.env.GOSX_BROWSER_EXECUTABLE || undefined,
    headless: true,
  });
  script = await readFile(path.join(__dirname, '../assets/native-editor.js'), 'utf8');
  intelligenceScript = await readFile(path.join(__dirname, '../assets/code-intelligence.js'), 'utf8');
});
after(async () => { await browser?.close(); });

const escape = value => value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');
async function mount(t, source, options = {}) {
  const context = await browser.newContext({viewport: {width: 900, height: 700}});
  t.after(() => context.close());
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  t.after(() => assert.deepEqual(errors, []));
  await page.setContent(`<!doctype html><style>
    textarea { width: 600px; height: 300px; font: 16px/24px monospace; }
  </style><main class="editor-page-native"><form data-editor-native="true"
    data-editor-surface="${options.surface || 'code'}" data-code-tab-width="${options.width || 2}"
    ${options.tabs ? '' : 'data-code-insert-spaces'} data-code-highlight-source="external">
    <textarea id="editor-content" name="content">${escape(source)}</textarea>
    <pre id="editor-highlight-content"><span class="syntax-initial">${escape(source)}</span></pre>
    <div><span id="editor-save-status"></span></div>
    <div class="editor-code-find" hidden><input data-code-find="query"><input data-code-find="replacement">
      <span class="editor-code-find-status"></span>
      <button type="button" data-code-find-action="close">Close</button>
    </div>
  </form></main>`);
  await page.evaluate(platform => Object.defineProperty(navigator, 'platform', {configurable: true, value: platform}), options.platform || 'Linux x86_64');
  await page.addScriptTag({content: script});
  await page.waitForFunction(() => document.querySelector('form').dataset.undoDepth === '0');
  const textarea = page.locator('#editor-content');
  await textarea.focus();
  return {page, textarea};
}

async function select(textarea, start, end = start, direction = 'forward') {
  await textarea.evaluate((node, value) => node.setSelectionRange(...value), [start, end, direction]);
}
async function snapshot(textarea) {
  return textarea.evaluate(node => ({value: node.value, start: node.selectionStart, end: node.selectionEnd, direction: node.selectionDirection}));
}
const browserTest = (name, run) => test(name, {skip: !modulePath && 'Set GOSX_PLAYWRIGHT_MODULE to run browser contracts'}, run);

browserTest('Code preserves source whitespace and updates stale external highlights before analysis', async t => {
  const source = '// café 🎵\n  \n';
  const {page, textarea} = await mount(t, source);
  assert.equal(await textarea.inputValue(), source);
  assert.equal(await page.locator('.syntax-initial').count(), 1, 'mount discarded matching semantic spans');
  await select(textarea, source.length);
  await page.keyboard.type('note');
  const value = await textarea.inputValue();
  assert.equal(value, source + 'note');
  await page.waitForFunction(() => document.querySelector('#editor-highlight-content').textContent === document.querySelector('#editor-content').value + '\n');
  assert.equal(await page.locator('.syntax-initial').count(), 0, 'stale semantic spans remained over new text');
  await page.locator('form').evaluate(form => form.dispatchEvent(new CustomEvent('gosx:highlight-spans', {detail: [{startUTF16: 0, endUTF16: 2, capture: 'comment'}]})));
  await page.keyboard.press('ArrowLeft');
  assert.equal(await page.locator('.syntax-comment').textContent(), '//', 'matching fresh semantic spans were lost');
});

browserTest('Code Tab follows spaces or tabs settings and ShiftTab is its inverse', async t => {
  const {page, textarea} = await mount(t, 'note');
  await select(textarea, 0);
  await page.keyboard.press('Tab');
  assert.equal(await textarea.inputValue(), '  note');
  await page.keyboard.press('Shift+Tab');
  assert.equal(await textarea.inputValue(), 'note');
  await page.keyboard.press('Control+z');
  assert.equal(await textarea.inputValue(), '  note');
  const tabs = await mount(t, 'note', {tabs: true, width: 8});
  await select(tabs.textarea, 0);
  await tabs.page.keyboard.press('Tab');
  assert.equal(await tabs.textarea.inputValue(), '\tnote');
});

browserTest('multiline indentation excludes a trailing column-zero line and retains backward selection', async t => {
  const {page, textarea} = await mount(t, 'one\ntwo\nthree');
  await select(textarea, 0, 8, 'backward');
  await page.keyboard.press('Tab');
  assert.deepEqual(await snapshot(textarea), {value: '  one\n  two\nthree', start: 2, end: 12, direction: 'backward'});
  await page.keyboard.press('Shift+Tab');
  assert.deepEqual(await snapshot(textarea), {value: 'one\ntwo\nthree', start: 0, end: 8, direction: 'backward'});
  await select(textarea, 1, 5);
  await page.keyboard.press('Control+/');
  assert.equal(await textarea.inputValue(), '// one\n// two\nthree', 'comment command edited partial lines');
});

browserTest('Code Enter preserves indentation, expands matching braces, and undoes once', async t => {
  const {page, textarea} = await mount(t, '  pattern {}');
  await select(textarea, 11);
  await page.keyboard.press('Enter');
  assert.deepEqual(await snapshot(textarea), {value: '  pattern {\n    \n  }', start: 16, end: 16, direction: 'forward'});
  await page.keyboard.press('Control+z');
  assert.equal(await textarea.inputValue(), '  pattern {}');
  await page.keyboard.press('Control+Shift+z');
  assert.equal(await textarea.inputValue(), '  pattern {\n    \n  }');
  await page.keyboard.type('note');
  await page.keyboard.press('Enter');
  assert.equal(await textarea.inputValue(), '  pattern {\n    note\n    \n  }');
});

browserTest('composition bypasses keyboard and multicursor interception and remains one undo step', async t => {
  const {page, textarea} = await mount(t, 'alpha\nbeta');
  await select(textarea, 1);
  await page.keyboard.press('Control+Alt+ArrowDown');
  assert.equal(await page.locator('form').getAttribute('data-multi-cursor-count'), '2');
  const guarded = await textarea.evaluate(node => {
    node.dispatchEvent(new CompositionEvent('compositionstart', {bubbles: true}));
    const key = new KeyboardEvent('keydown', {key: 'Tab', isComposing: true, keyCode: 229, bubbles: true, cancelable: true});
    node.dispatchEvent(key);
    const input = new InputEvent('beforeinput', {inputType: 'insertCompositionText', data: 'あ', isComposing: true, bubbles: true, cancelable: true});
    node.dispatchEvent(input);
    const before = node.value;
    node.setRangeText('あ', node.selectionStart, node.selectionEnd, 'end');
    node.dispatchEvent(new InputEvent('input', {inputType: 'insertCompositionText', isComposing: true, bubbles: true}));
    node.dispatchEvent(new CompositionEvent('compositionend', {data: 'あ', bubbles: true}));
    return {keyPrevented: key.defaultPrevented, inputPrevented: input.defaultPrevented, before};
  });
  assert.deepEqual(guarded, {keyPrevented: false, inputPrevented: false, before: 'alpha\nbeta'});
  assert.equal(await textarea.inputValue(), 'aあlpha\nbeta');
  assert.equal(await page.locator('form').getAttribute('data-multi-cursor-count'), '1');
  await page.keyboard.press('Control+z');
  assert.equal(await textarea.inputValue(), 'alpha\nbeta');
});

browserTest('Replace uses platform conventions and Mac Control-H/Command-H stay native', async t => {
  for (const platform of ['Linux x86_64', 'Win32', 'MacIntel']) {
    const {page, textarea} = await mount(t, 'note', {platform});
    await page.keyboard.press(platform === 'MacIntel' ? 'Meta+Alt+f' : 'Control+h');
    assert.equal(await page.locator('[data-code-find="replacement"]').evaluate(node => document.activeElement === node), true, platform);
    if (platform !== 'MacIntel') continue;
    await page.locator('[data-code-find-action="close"]').click();
    await textarea.focus();
    const prevented = await textarea.evaluate(node => [false, true].map(metaKey => {
      const event = new KeyboardEvent('keydown', {key: 'h', metaKey, ctrlKey: !metaKey, bubbles: true, cancelable: true});
      node.dispatchEvent(event);
      return event.defaultPrevented;
    }));
    assert.deepEqual(prevented, [false, false]);
  }
});

browserTest('Markdown retains whitespace normalization and literal-tab behavior', async t => {
  const {page, textarea} = await mount(t, 'alpha\n  \n', {surface: 'markdown'});
  assert.equal(await textarea.inputValue(), 'alpha\n\n');
  await select(textarea, 0);
  await page.keyboard.press('Tab');
  assert.equal(await textarea.inputValue(), '\talpha\n\n');
});

browserTest('managed form replacement and script reexecution mount exactly one set of native handlers', async t => {
  const {page} = await mount(t, 'first');
  for (const source of ['second', 'third', 'first']) {
    await page.evaluate(source => {
      const old = document.querySelector('form');
      window.__detachedEditor = old;
      const replacement = old.cloneNode(true);
      replacement.querySelector('textarea').value = source;
      replacement.querySelector('#editor-highlight-content').textContent = source;
      old.replaceWith(replacement);
    }, source);
    await page.addScriptTag({content: script});
    const textarea = page.locator('#editor-content');
    await textarea.focus();
    await select(textarea, 0);
    await page.keyboard.press('Tab');
    assert.equal(await textarea.inputValue(), '  ' + source);
    await page.keyboard.press('Control+z');
    assert.equal(await textarea.inputValue(), source, 'duplicate handlers consumed more than one undo step');
    const oldPrevented = await page.evaluate(() => {
      const key = new KeyboardEvent('keydown', {key: 'Tab', bubbles: true, cancelable: true});
      window.__detachedEditor.querySelector('textarea').dispatchEvent(key);
      return key.defaultPrevented;
    });
    assert.equal(oldPrevented, false, 'detached editor retained its keyboard listeners');
    await page.keyboard.press('Control+h');
    assert.equal(await page.locator('[data-code-find="replacement"]').evaluate(node => document.activeElement === node), true);
  }
});

browserTest('analysis ignores stale initial/update responses, aborts detached forms, and mounts replacement forms', async t => {
  const {page, textarea} = await mount(t, 'alpha');
  await page.evaluate(() => {
    window.__analysisRequests = [];
    window.fetch = (url, options) => new Promise(resolve => {
      window.__analysisRequests.push({
        content: JSON.parse(options.body).content, signal: options.signal,
        resolve: analysis => resolve(new Response(JSON.stringify(analysis), {status: 200})),
      });
    });
    document.querySelector('form').dataset.codeIntelligenceServer = '/analyze';
  });
  await page.addScriptTag({content: intelligenceScript});
  await page.waitForFunction(() => window.__analysisRequests.length === 1);
  await select(textarea, 5);
  await page.keyboard.type(' beta');
  await page.evaluate(() => window.__analysisRequests[0].resolve({highlights: [{startByte: 0, endByte: 5, capture: 'stale-initial'}]}));
  await page.waitForFunction(() => window.__analysisRequests.length === 2);
  assert.equal(await page.locator('.syntax-stale-initial').count(), 0);
  assert.equal(await page.evaluate(() => window.__analysisRequests[1].content), 'alpha beta');
  await page.evaluate(() => window.__analysisRequests[1].resolve({highlights: [{startByte: 0, endByte: 5, capture: 'fresh'}]}));
  await page.locator('.syntax-fresh').waitFor();

  await page.keyboard.type(' gamma');
  await page.waitForFunction(() => window.__analysisRequests.length === 3);
  await page.keyboard.type(' delta');
  await page.waitForFunction(() => window.__analysisRequests.length === 4);
  await page.evaluate(() => window.__analysisRequests[2].resolve({highlights: [{startByte: 0, endByte: 5, capture: 'stale-update'}]}));
  assert.equal(await page.locator('.syntax-stale-update').count(), 0);

  await page.evaluate(() => {
    const old = document.querySelector('form');
    window.__detachedAnalysis = old;
    window.__detachedHTML = old.querySelector('#editor-highlight-content').innerHTML;
    const replacement = old.cloneNode(true);
    replacement.querySelector('textarea').value = 'new file';
    old.replaceWith(replacement);
  });
  await page.waitForFunction(() => window.__analysisRequests.length === 5);
  assert.equal(await page.evaluate(() => window.__analysisRequests[3].signal.aborted), true);
  await page.evaluate(() => window.__analysisRequests[3].resolve({highlights: [{startByte: 0, endByte: 5, capture: 'detached'}]}));
  assert.equal(await page.evaluate(() => window.__detachedAnalysis.querySelector('#editor-highlight-content').innerHTML === window.__detachedHTML), true);
  await page.evaluate(() => window.__analysisRequests[4].resolve({highlights: [{startByte: 0, endByte: 3, capture: 'new-file'}]}));
  assert.equal(await page.locator('.syntax-new-file').textContent(), 'new');
  await page.addScriptTag({content: intelligenceScript});
  const replacement = page.locator('#editor-content');
  await replacement.focus();
  await select(replacement, 8);
  await page.keyboard.type('!');
  await page.waitForFunction(() => window.__analysisRequests.length === 6);
  await page.evaluate(() => window.__analysisRequests[5].resolve({highlights: [{startByte: 0, endByte: 3, capture: 'new-file'}]}));
  assert.equal(await page.evaluate(() => window.__analysisRequests.length), 6, 'script reexecution duplicated provider requests');
});

browserTest('a provider outage retries on editing and successful analysis clears stale diagnostics', async t => {
  const {page, textarea} = await mount(t, 'alpha');
  await page.evaluate(() => {
    const diagnostics = document.createElement('div');
    diagnostics.id = 'editor-diagnostics';
    document.querySelector('form').appendChild(diagnostics);
    window.__providerCalls = 0;
    window.fetch = async () => {
      const call = ++window.__providerCalls;
      return new Response(JSON.stringify(call <= 2 ? {} : {
        highlights: [{startByte: 0, endByte: 5, capture: 'recovered'}], hasErrors: call === 3,
      }), {status: call <= 2 ? 503 : 200});
    };
    document.querySelector('form').dataset.codeIntelligenceServer = '/analyze';
  });
  await page.addScriptTag({content: intelligenceScript});
  await page.waitForFunction(() => document.querySelector('#editor-diagnostics').textContent.startsWith('Code intelligence unavailable:'));
  await select(textarea, 5);
  await page.keyboard.type('!');
  await page.locator('.syntax-recovered').waitFor();
  assert.equal(await page.locator('#editor-diagnostics').textContent(), 'Syntax tree contains an error node.');
  await page.keyboard.type('!');
  await page.waitForFunction(() => document.querySelector('#editor-diagnostics').textContent === '');
  assert.equal(await page.evaluate(() => window.__providerCalls), 4);
});

browserTest('retained-form morph restores mounted presentation and analysis without replacing history or handlers', async t => {
  const {page, textarea} = await mount(t, 'alpha');
  await page.evaluate(() => {
    window.__retainedEditor = {
      page: document.querySelector('.editor-page-native'),
      form: document.querySelector('form'), textarea: document.querySelector('textarea'),
    };
    window.__providerCalls = 0;
    window.__analysisEpoch = 0;
    document.querySelector('form').addEventListener('gosx:code-analysis', () => window.__analysisEpoch++);
    window.fetch = async () => {
      window.__providerCalls++;
      return new Response(JSON.stringify({highlights: [{startByte: 0, endByte: 2, capture: 'current'}]}), {status: 200});
    };
    document.querySelector('form').dataset.codeIntelligenceServer = '/analyze';
  });
  await page.addScriptTag({content: intelligenceScript});
  await page.locator('.syntax-current').waitFor();
  await select(textarea, 5);
  await page.keyboard.type('!');

  for (const source of ['beta', 'gamma']) {
    const depth = await page.locator('form').getAttribute('data-undo-depth');
    await page.evaluate(source => {
      const {page, form, textarea} = window.__retainedEditor;
      // A managed save/Undo projection writes the server attributes and
      // canonical textarea content while keeping all three DOM identities.
      page.className = 'editor-page-native editor-surface-code';
      for (const key of ['undoDepth', 'redoDepth', 'multiCursorCount', 'codeIntelligenceLane', 'highlightSpanCount']) delete form.dataset[key];
      textarea.defaultValue = source;
      textarea.value = source;
    }, source);
    await page.waitForFunction(() => {
      const refs = window.__retainedEditor;
      return refs.page.classList.contains('editor-highlight-ready') &&
        refs.form.dataset.codeIntelligenceLane === 'server' && refs.form.dataset.highlightSpanCount === '1' &&
        refs.form.querySelector('#editor-highlight-content').textContent === refs.textarea.value + '\n';
    });
    assert.equal(await page.locator('form').getAttribute('data-undo-depth'), depth, 'morph replaced local history');
    assert.equal(await page.evaluate(() => {
      const refs = window.__retainedEditor;
      return refs.page === document.querySelector('.editor-page-native') && refs.form === document.querySelector('form') && refs.textarea === document.querySelector('textarea');
    }), true);
    const epoch = await page.evaluate(() => window.__analysisEpoch);
    await select(textarea, 0);
    await page.keyboard.press('Tab');
    assert.equal(await textarea.inputValue(), '  ' + source);
    await page.keyboard.press('Control+z');
    assert.equal(await textarea.inputValue(), source, 'morph duplicated keyboard handlers');
    await page.waitForFunction(epoch => window.__analysisEpoch > epoch, epoch);
  }

  // Attribute repair converges: it must not keep repainting or refetching
  // the same analysis after the managed transaction has settled.
  await page.waitForFunction(() => document.querySelector('.syntax-current')?.textContent === 'ga');
  const idle = await page.evaluate(async () => {
    let mutations = 0;
    const observer = new MutationObserver(records => { mutations += records.length; });
    observer.observe(document.querySelector('form'), {attributes: true, childList: true, subtree: true});
    const settle = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    await settle();
    const before = {mutations, calls: window.__providerCalls};
    await settle();
    observer.disconnect();
    return {before, after: {mutations, calls: window.__providerCalls}};
  });
  assert.deepEqual(idle.after, idle.before, 'retained-form repair did not become idle');
});
