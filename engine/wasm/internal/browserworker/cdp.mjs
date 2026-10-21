// Minimal raw Chrome DevTools Protocol client. Playwright emulates focus and
// visibility, so a tab never turns hidden under it; raw CDP lets a second tab
// take the foreground so the first tab is really hidden.
export async function connect(port) {
  const ver = await (await fetch(`http://127.0.0.1:${port}/json/version`)).json();
  const ws = new WebSocket(ver.webSocketDebuggerUrl);
  await new Promise((ok, bad) => { ws.onopen = ok; ws.onerror = bad; });
  let id = 0; const pending = new Map(); const listeners = [];
  ws.onmessage = ev => { const m = JSON.parse(ev.data); if (m.id && pending.has(m.id)) { const {ok, bad} = pending.get(m.id); pending.delete(m.id); m.error ? bad(new Error(JSON.stringify(m.error))) : ok(m.result); } else listeners.forEach(l => l(m)); };
  const send = (method, params = {}, sessionId) => new Promise((ok, bad) => { const i = ++id; pending.set(i, {ok, bad}); ws.send(JSON.stringify({id: i, method, params, sessionId})); });
  return { send, on: l => listeners.push(l), close: () => ws.close() };
}
export async function attach(c, targetId) { const { sessionId } = await c.send("Target.attachToTarget", { targetId, flatten: true }); return sessionId; }
export async function evalIn(c, sessionId, expr) { const r = await c.send("Runtime.evaluate", { expression: expr, awaitPromise: true, returnByValue: true }, sessionId); if (r.exceptionDetails) throw new Error(JSON.stringify(r.exceptionDetails).slice(0, 300)); return r.result.value; }
