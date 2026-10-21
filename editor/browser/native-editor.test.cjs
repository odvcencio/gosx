const assert = require('node:assert/strict');
const {readFile} = require('node:fs/promises');
const path = require('node:path');
const {after, before, test} = require('node:test');

// Optional standalone browser contract suite. Point GOSX_PLAYWRIGHT_MODULE at
// an existing Playwright installation; this module needs no npm dependencies.
const modulePath = process.env.GOSX_PLAYWRIGHT_MODULE;
let browser, script, intelligenceScript, editorStylesheet;
const readAsset = async name => {
  try {
    return await readFile(path.join(__dirname, '../assets', name + '.js'), 'utf8');
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
    return readFile(path.join(__dirname, '../assets', name + '.ts'), 'utf8');
  }
};
before(async () => {
  if (!modulePath) return;
  const {chromium} = require(modulePath);
  browser = await chromium.launch({
    executablePath: process.env.GOSX_BROWSER_EXECUTABLE || undefined,
    headless: true,
  });
  script = await readAsset('native-editor');
  intelligenceScript = await readAsset('code-intelligence');
  editorStylesheet = await readFile(path.join(__dirname, '../assets/editor.css'), 'utf8');
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
    .editor-source-shell { position: relative; width: 600px; height: 300px; }
    textarea, .editor-highlight-layer { position: absolute; inset: 0; width: 600px; height: 300px; margin: 0; border: 0; box-sizing: border-box; padding: 8px; font: 16px/24px monospace; white-space: pre-wrap; overflow-wrap: break-word; overflow: auto; }
    textarea { z-index: 1; }
    .editor-line-numbers { height: 300px; overflow: hidden; box-sizing: border-box; padding: 8px 0; }
    .editor-line-numbers span { display: block; height: 24px; }
  </style><main class="editor-page-native"><form data-editor-native="true"
    data-editor-surface="${options.surface || 'code'}" data-code-tab-width="${options.width || 2}"
    ${options.tabs ? '' : 'data-code-insert-spaces'} data-code-highlight-source="external">
    <div class="editor-source-shell"><div id="editor-line-numbers" class="editor-line-numbers"></div>
    <pre class="editor-highlight-layer"><code id="editor-highlight-content"><span class="syntax-initial">${escape(source)}</span></code></pre>
    <textarea id="editor-content" name="content">${escape(source)}</textarea></div>
    <div><span id="editor-save-status"></span></div>
    <button type="button" data-code-command="goto">Go to line</button>
    <div data-code-goto-panel hidden><input data-code-goto="query" aria-label="Line and column">
      <button type="button" data-code-goto="go">Go</button><button type="button" data-code-goto="close">Close go to line</button>
      <span data-code-goto="status" aria-live="polite"></span>
    </div>
    <div class="editor-code-find" hidden><input data-code-find="query"><input data-code-find="replacement">
      <span class="editor-code-find-status"></span>
      <button type="button" data-code-find-action="replace">Replace</button>
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
async function caretVisible(textarea, clipSelector) {
  return textarea.evaluate((node, clipSelector) => {
    const walker = document.createTreeWalker(document.querySelector('#editor-highlight-content'), NodeFilter.SHOW_TEXT);
    let offset = node.selectionStart, text;
    while ((text = walker.nextNode())) {
      if (offset >= text.length) { offset -= text.length; continue; }
      const range = document.createRange();
      range.setStart(text, offset); range.setEnd(text, offset + 1);
      const rect = range.getBoundingClientRect(), bounds = node.getBoundingClientRect();
      const clip = clipSelector ? document.querySelector(clipSelector).getBoundingClientRect() : bounds;
      const view = {top: Math.max(bounds.top, clip.top, 0), bottom: Math.min(bounds.bottom, clip.bottom, innerHeight),
        left: Math.max(bounds.left, clip.left, 0), right: Math.min(bounds.right, clip.right, innerWidth)};
      return rect.top >= view.top && rect.bottom <= view.bottom && rect.left >= view.left && rect.right <= view.right;
    }
    return false;
  }, clipSelector);
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

browserTest('Find keeps typing focus, navigates both directions and wraps; Replace edits the selected match with local Undo', async t => {
  const {page, textarea} = await mount(t, 'alpha beta alpha');
  await page.keyboard.press('Control+f');
  const query = page.locator('[data-code-find="query"]');
  for (const character of 'alpha') {
    await page.keyboard.type(character);
    assert.equal(await query.evaluate(node => document.activeElement === node), true, 'typing stole search focus');
  }
  assert.equal(await query.inputValue(), 'alpha');
  assert.equal(await textarea.inputValue(), 'alpha beta alpha', 'search typing edited the source');
  assert.equal(await page.locator('.editor-code-find-status').textContent(), '1 / 2');
  await page.keyboard.press('Enter');
  assert.deepEqual(await textarea.evaluate(node => [node.selectionStart, node.selectionEnd]), [11, 16]);
  assert.equal(await query.evaluate(node => document.activeElement === node), true);
  await page.keyboard.press('Enter');
  assert.deepEqual(await textarea.evaluate(node => [node.selectionStart, node.selectionEnd]), [0, 5]);
  await page.keyboard.press('Shift+Enter');
  assert.deepEqual(await textarea.evaluate(node => [node.selectionStart, node.selectionEnd]), [11, 16]);
  await textarea.focus();
  await page.keyboard.press('F3');
  assert.deepEqual(await textarea.evaluate(node => [node.selectionStart, node.selectionEnd]), [0, 5]);
  await page.keyboard.press('Shift+F3');
  assert.deepEqual(await textarea.evaluate(node => [node.selectionStart, node.selectionEnd]), [11, 16]);
  await select(textarea, 8);
  await page.keyboard.press('Shift+F3');
  assert.deepEqual(await textarea.evaluate(node => [node.selectionStart, node.selectionEnd]), [0, 5]);
  const replacement = page.locator('[data-code-find="replacement"]');
  await replacement.fill('omega');
  await page.keyboard.press('Enter');
  assert.equal(await textarea.inputValue(), 'omega beta alpha', 'Replace skipped the selected occurrence');
  assert.equal(await replacement.evaluate(node => document.activeElement === node), true);
  await page.keyboard.press('Enter');
  assert.equal(await textarea.inputValue(), 'omega beta omega');
  await textarea.focus();
  await page.keyboard.press('Control+z');
  assert.equal(await textarea.inputValue(), 'omega beta alpha', 'Undo did not restore exactly one replacement');
  for (const field of [query, replacement]) {
    const prevented = await field.evaluate(node => {
      const event = new KeyboardEvent('keydown', {key: 'Enter', isComposing: true, bubbles: true, cancelable: true});
      node.dispatchEvent(event);
      return event.defaultPrevented;
    });
    assert.equal(prevented, false, 'search command intercepted IME confirmation');
  }
  assert.equal(await textarea.inputValue(), 'omega beta alpha');
});

browserTest('Find restores field focus and caret when native textarea selection moves focus as WebKit does', async t => {
  const {page, textarea} = await mount(t, 'alpha beta alpha');
  await textarea.evaluate(node => {
    // WKWebView focuses the textarea from selection APIs. Also reset the
    // blurred control's caret so restoring focus alone cannot pass this test.
    for (const method of ['setSelectionRange', 'setRangeText']) {
      const original = node[method];
      node[method] = function (...args) {
        const active = document.activeElement;
        const result = original.apply(this, args);
        this.focus();
        if (active !== this && active?.matches('[data-code-find]')) active.setSelectionRange(0, 0);
        return result;
      };
    }
  });
  const query = page.locator('[data-code-find="query"]');
  const replacement = page.locator('[data-code-find="replacement"]');
  const fieldState = field => field.evaluate(node => ({focused: document.activeElement === node, start: node.selectionStart, end: node.selectionEnd, direction: node.selectionDirection}));
  await page.keyboard.press('Control+f');
  for (const [index, character] of Array.from('alpha').entries()) {
    await page.keyboard.type(character);
    assert.deepEqual(await fieldState(query), {focused: true, start: index + 1, end: index + 1, direction: 'forward'});
  }
  assert.equal(await query.inputValue(), 'alpha');
  assert.equal(await textarea.inputValue(), 'alpha beta alpha');
  await query.evaluate(node => node.setSelectionRange(1, 4, 'backward'));
  await page.keyboard.press('Enter');
  assert.deepEqual(await fieldState(query), {focused: true, start: 1, end: 4, direction: 'backward'});
  assert.deepEqual(await textarea.evaluate(node => [node.selectionStart, node.selectionEnd]), [11, 16]);
  await textarea.focus(); await select(textarea, 0, 5);
  await replacement.focus();
  for (const [index, character] of Array.from('omega').entries()) {
    await page.keyboard.type(character);
    assert.deepEqual(await fieldState(replacement), {focused: true, start: index + 1, end: index + 1, direction: 'forward'});
  }
  await replacement.evaluate(node => node.setSelectionRange(1, 4, 'backward'));
  for (const expected of ['omega beta alpha', 'omega beta omega']) {
    await page.keyboard.press('Enter');
    assert.equal(await textarea.inputValue(), expected);
    assert.deepEqual(await fieldState(replacement), {focused: true, start: 1, end: 4, direction: 'backward'});
  }
  await textarea.focus(); await page.keyboard.press('F3');
  assert.equal(await textarea.evaluate(node => document.activeElement === node), true, 'explicit editor navigation must keep source focus');
});

browserTest('Go to line uses scalar columns, reveals distant and wrapped targets, validates input and restores Escape checkpoint', async t => {
  const lines = ['// 🎵 café', 'a🎵bc', ...Array.from({length: 160}, (_, i) => `line ${i}`), 'x'.repeat(2000)];
  const source = lines.join('\n');
  for (const platform of ['Linux x86_64', 'Win32', 'MacIntel']) {
    const {page, textarea} = await mount(t, source, {platform});
    await page.keyboard.press('Control+g');
    const query = page.locator('[data-code-goto="query"]');
    const panel = page.locator('[data-code-goto-panel]');
    assert.equal(await query.inputValue(), '1:1');
    assert.equal(await query.evaluate(node => document.activeElement === node), true, platform);
    await query.fill('2:4');
    await page.keyboard.press('Enter');
    const unicodeOffset = lines[0].length + 1 + 4;
    assert.deepEqual(await textarea.evaluate(node => [node.selectionStart, node.selectionEnd]), [unicodeOffset, unicodeOffset]);
    assert.equal(await panel.isHidden(), true);
    assert.equal(await textarea.evaluate(node => document.activeElement === node), true);
    await page.keyboard.press('Control+g');
    await query.fill('2:999');
    await page.keyboard.press('Enter');
    assert.equal((await snapshot(textarea)).start, lines[0].length + 1 + lines[1].length, 'columns beyond EOL must clamp');
    await page.keyboard.press('Control+g');
    for (const invalid of ['0', '999', '2:0', '-1', '1.5', '2:1.5', 'wat']) {
      await query.fill(invalid); await page.keyboard.press('Enter');
      assert.equal(await query.getAttribute('aria-invalid'), 'true', invalid);
      assert.notEqual(await page.locator('[data-code-goto="status"]').textContent(), '');
      assert.equal(await panel.isVisible(), true);
      assert.equal(await textarea.inputValue(), source);
    }
    await query.fill('150'); await page.keyboard.press('Enter');
    const distant = await textarea.evaluate(node => node.scrollTop);
    assert.ok(distant > 1000, 'distant line stayed outside the editor viewport');
    assert.equal(await caretVisible(textarea), true, 'distant line caret is outside the visible text area');
    await page.keyboard.press('Control+g');
    await query.fill(`${lines.length}:1800`); await page.keyboard.press('Enter');
    assert.ok(await textarea.evaluate(node => node.scrollTop) > distant + 400, 'wrapped long-line column was not revealed');
    assert.equal(await caretVisible(textarea), true, 'wrapped target caret is outside the visible text area');
    const synchronized = await textarea.evaluate(node => ({
      textarea: node.scrollTop, layer: document.querySelector('.editor-highlight-layer').scrollTop,
      gutter: document.querySelector('#editor-line-numbers').scrollTop,
    }));
    assert.equal(synchronized.layer, synchronized.textarea);
    assert.equal(synchronized.gutter, synchronized.textarea);
    await textarea.evaluate(async node => {
      node.style.whiteSpace = 'pre';
      document.querySelector('.editor-highlight-layer').style.whiteSpace = 'pre';
      window.dispatchEvent(new Event('resize'));
      await new Promise(requestAnimationFrame);
    });
    await page.keyboard.press('Control+g');
    await query.fill(`${lines.length}:1800`); await page.keyboard.press('Enter');
    assert.ok(await textarea.evaluate(node => node.scrollLeft) > 1000, 'unwrapped long-line target did not scroll horizontally');
    assert.equal(await caretVisible(textarea), true, 'horizontal target caret is outside the visible text area');
    await select(textarea, 4, 12, 'backward');
    const original = await textarea.evaluate(node => {
      node.scrollTop = 240; node.scrollLeft = 300;
      node.dispatchEvent(new Event('scroll'));
      return {start: node.selectionStart, end: node.selectionEnd, direction: node.selectionDirection, top: node.scrollTop, left: node.scrollLeft};
    });
    await page.keyboard.press('Control+g');
    await query.fill('150:4');
    const imePrevented = await query.evaluate(node => {
      const event = new KeyboardEvent('keydown', {key: 'Enter', isComposing: true, bubbles: true, cancelable: true});
      node.dispatchEvent(event); return event.defaultPrevented;
    });
    assert.equal(imePrevented, false, 'Go to line intercepted IME confirmation');
    assert.equal(await panel.isVisible(), true);
    await page.keyboard.press('Escape');
    assert.deepEqual(await textarea.evaluate(node => ({start: node.selectionStart, end: node.selectionEnd, direction: node.selectionDirection, top: node.scrollTop, left: node.scrollLeft})), original);
    assert.equal(await panel.isHidden(), true);
    assert.equal(await textarea.inputValue(), source);
    if (platform === 'MacIntel') {
      const prevented = await textarea.evaluate(node => {
        const event = new KeyboardEvent('keydown', {key: 'g', metaKey: true, bubbles: true, cancelable: true});
        node.dispatchEvent(event);
        return event.defaultPrevented;
      });
      assert.equal(prevented, false, 'Mac Command-G must remain native');
    }
  }
});

browserTest('Go to line reveals targets inside ancestor clips without scrolling the application', async t => {
  const source = ['wrap '.repeat(30), ...Array.from({length: 13}, (_, i) => `line ${i + 2} café 🎵`)].join('\n');
  for (const overflow of ['hidden', 'auto', 'scroll']) {
    const {page, textarea} = await mount(t, source);
    const original = await textarea.evaluate((node, overflow) => {
      const shell = node.parentElement;
      const clip = document.createElement('div');
      clip.id = 'clipped-editor';
      clip.style.cssText = `width:320px;height:180px;overflow:${overflow}`;
      shell.replaceWith(clip); clip.appendChild(shell);
      return {top: clip.scrollTop, left: clip.scrollLeft, x: window.scrollX, y: window.scrollY};
    }, overflow);
    await textarea.focus();
    await page.keyboard.press('Control+g');
    const query = page.locator('[data-code-goto="query"]');
    await query.fill('8:3'); await page.keyboard.press('Enter');
    assert.equal(await caretVisible(textarea, '#clipped-editor'), true, overflow + ': target was selected below its visible dock');
    assert.ok(await textarea.evaluate(node => node.scrollTop) > 0, 'the hidden portion of the textarea counted as visible');
    assert.equal(await textarea.inputValue(), source);
    await textarea.evaluate(async node => {
      node.style.whiteSpace = 'pre';
      document.querySelector('.editor-highlight-layer').style.whiteSpace = 'pre';
      window.dispatchEvent(new Event('resize'));
      await new Promise(requestAnimationFrame);
    });
    await page.keyboard.press('Control+g');
    await query.fill('1:55'); await page.keyboard.press('Enter');
    assert.equal(await caretVisible(textarea, '#clipped-editor'), true, overflow + ': target was selected beyond the horizontal clip');
    assert.ok(await textarea.evaluate(node => node.scrollLeft) > 0);
    await page.evaluate(() => {
      const clip = document.querySelector('#clipped-editor');
      clip.style.position = 'fixed';
      clip.style.top = `${innerHeight - 100}px`;
    });
    await page.keyboard.press('Control+g');
    await query.fill('5:3'); await page.keyboard.press('Enter');
    assert.equal(await caretVisible(textarea, '#clipped-editor'), true, overflow + ': target stayed beyond the browser viewport');
    assert.deepEqual(await page.evaluate(() => {
      const clip = document.querySelector('#clipped-editor');
      return {top: clip.scrollTop, left: clip.scrollLeft, x: window.scrollX, y: window.scrollY};
    }), original, 'navigation scrolled an application ancestor');
    assert.deepEqual(await textarea.evaluate(node => [
      node.scrollTop, document.querySelector('.editor-highlight-layer').scrollTop, document.querySelector('#editor-line-numbers').scrollTop,
    ]), await textarea.evaluate(node => [node.scrollTop, node.scrollTop, node.scrollTop]));
  }
});

browserTest('EOF keeps the final glyph and gutter aligned with actual editor padding, with or without a trailing newline', async t => {
  const lines = ['// cicada-code-editor-test: disposable fixture', '// café 🎵 日本語', 'cicada 2',
    'title "Code editor platform checks"', 'track bass acid { cutoff=700Hz }', 'pattern pulse { 1 . 5 . }',
    'arrange {', '  place one bass pulse { at = @1.1.1 length = 4bars }', '  place two bass pulse { at = @5.1.1 length = 4bars }', '}'];
  for (const trailing of ['', '\n']) {
    const source = lines.join('\n') + trailing;
    const {page, textarea} = await mount(t, source);
    await textarea.evaluate(node => {
      // Replace the minimal fixture's gutter approximation with the shipped
      // stylesheet, in a short, narrow editor like an embedded source dock.
      for (const rule of document.querySelector('style').sheet.cssRules) {
        if (rule.selectorText === '.editor-line-numbers') {
          rule.style.removeProperty('height'); rule.style.removeProperty('padding');
        }
      }
      node.classList.add('editor-source');
      node.parentElement.style.width = '300px'; node.parentElement.style.height = '163px';
    });
    await page.addStyleTag({content: editorStylesheet});
    await page.evaluate(async () => {
      document.querySelector('form').dispatchEvent(new CustomEvent('gosx:highlight-spans', {detail: []}));
      window.dispatchEvent(new Event('resize'));
      await new Promise(requestAnimationFrame);
    });
    await textarea.focus(); await page.keyboard.press('Control+g');
    await page.locator('[data-code-goto="query"]').fill(`${lines.length + (trailing ? 1 : 0)}:9999`);
    await page.keyboard.press('Enter');
    assert.deepEqual(await textarea.evaluate(node => [node.selectionStart, node.selectionEnd]), [source.length, source.length]);
    const geometry = await textarea.evaluate(node => {
      const target = node.value.lastIndexOf('}');
      const walker = document.createTreeWalker(document.querySelector('#editor-highlight-content'), NodeFilter.SHOW_TEXT);
      let offset = target, text;
      while ((text = walker.nextNode())) {
        if (offset >= text.length) { offset -= text.length; continue; }
        const range = document.createRange(); range.setStart(text, offset); range.setEnd(text, offset + 1);
        const glyph = range.getBoundingClientRect(), view = node.getBoundingClientRect();
        const gutter = document.querySelector('#editor-line-numbers'), layer = document.querySelector('.editor-highlight-layer');
        return {glyph: range.toString(), visible: glyph.top >= view.top && glyph.bottom <= view.bottom,
          source: node.scrollTop, highlight: layer.scrollTop, gutter: gutter.scrollTop,
          sourceMax: node.scrollHeight - node.clientHeight, gutterMax: gutter.scrollHeight - gutter.clientHeight};
      }
    });
    t.diagnostic(JSON.stringify({trailingNewline: Boolean(trailing), ...geometry}));
    assert.equal(geometry.glyph, '}'); assert.equal(geometry.visible, true);
    assert.ok(Math.abs(geometry.source - geometry.highlight) <= 1, JSON.stringify(geometry));
    assert.ok(Math.abs(geometry.source - geometry.gutter) <= 1, JSON.stringify(geometry));
    assert.ok(geometry.gutterMax >= geometry.source - 1, JSON.stringify(geometry));
    assert.equal(await textarea.inputValue(), source);
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
