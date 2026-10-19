const assert = require('node:assert/strict');
const vm = require('node:vm');
const input = JSON.parse(require('node:fs').readFileSync(0, 'utf8'));
function page() {
  const posted = [];
  let receive;
  const window = {chrome: {webview: {
    postMessage(raw) { posted.push(JSON.parse(raw)); },
    addEventListener(_, handler) { receive = handler; }
  }}};
  const context = vm.createContext({window});
  return {window, posted, run(script) { vm.runInContext(script, context); },
    event(raw) { receive({data: raw}); }};
}
const preview = 'gosx.desktop.service.diagnostics.preview';
const fullscreen = 'gosx.desktop.window.setFullscreen';
const fresh = page();
fresh.run(input.initial);
const api = fresh.window.gosxDesktop;
assert.equal(api.hasMethod(fullscreen), input.native);
assert.equal(api.services.available(), false);
assert.equal(typeof api.service('privacySettings').deleteMyData, 'function');
assert.equal(fresh.posted.length, 0, 'capability queries must not send discovery RPCs');

// A service bound before first navigation is visible within the same frame.
const bound = page();
bound.run(input.withAppPreload);
assert.equal(bound.window.afterBridge, true);
assert.equal(bound.window.gosxDesktop.hasMethod(preview), true);
assert.equal(bound.window.gosxDesktop.hasMethod('gosx.desktop.service.diagnostics.export'), false);

// Bind/unbind on an existing page updates metadata without waiting for RPCs.
fresh.event(input.boundEvent);
assert.equal(api.hasMethod(preview), true);
assert.equal(api.services.available(), true);
fresh.event(input.unboundEvent);
assert.equal(api.hasMethod(preview), false);
assert.equal(api.services.available(), false);
fresh.event(input.boundEvent);
assert.equal(api.hasMethod(preview), false, 'older events must not restore removed methods');

// Windows may accumulate document-start scripts; any order must retain the
// newest snapshot, including a service removed before the next navigation.
for (const scripts of [[input.initial, input.bound, input.unbound], [input.unbound, input.bound, input.initial]]) {
  const replay = page();
  for (const script of scripts) replay.run(script);
  assert.equal(replay.window.gosxDesktop.hasMethod(preview), false);
  assert.equal(replay.window.gosxDesktop.hasMethod(fullscreen), input.native);
}
api.call('legacy.echo');
api.service('notBound').ping();
assert.deepEqual(fresh.posted.map(r => r.method), ['legacy.echo', 'gosx.desktop.service.notBound.ping']);
