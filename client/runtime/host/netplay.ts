// @ts-check
// GoSX netplay browser binding: owns RTCPeerConnection and one unreliable data
// channel per connection, and exposes a polled datagram API to Go WASM
// (netplay/client). Applications write no JavaScript. The signaling messages
// match package netplay/signal; Go carries them over the hub WebSocket.
(function() {
  "use strict";
  const MAX_PACKET = 1200;      // netplay.MaxPacketSize
  const QUEUE_SLOTS = 128;      // received datagrams held per connection
  const MAX_BUFFERED = 16 * 1024; // above this the channel drops sends
  const MAX_CONNECTIONS = 8;
  const MAX_SDP = 16 * 1024;
  const MAX_CANDIDATES = 64;

  const conns = new Map();
  let nextID = 1;

  function supported() { return typeof window.RTCPeerConnection === "function"; }

  // A connection reports each of "open", "failed" and "closed" at most once,
  // and nothing after it has failed or closed.
  function notify(c, next) {
    if (c.state === next || c.state === "closed" || c.state === "failed") return;
    c.state = next;
    try { if (c.cfg.onState) c.cfg.onState(next); } catch (_) { /* a callback error must not break the connection */ }
  }

  function emit(c, event, data) {
    try { c.cfg.onSignal(event, JSON.stringify(data)); } catch (_) { /* Go side gone */ }
  }

  function release(c) {
    try { if (c.dc) { c.dc.onopen = c.dc.onmessage = c.dc.onclose = c.dc.onerror = null; c.dc.close(); } } catch (_) {}
    try { c.pc.onicecandidate = c.pc.onconnectionstatechange = null; c.pc.close(); } catch (_) {}
  }

  function fail(c) {
    if (c.state === "closed" || c.state === "failed") return;
    notify(c, "failed");
    release(c);
  }

  function pushIncoming(c, data) {
    const bytes = data instanceof ArrayBuffer ? new Uint8Array(data) : ArrayBuffer.isView(data) ? new Uint8Array(data.buffer, data.byteOffset, data.byteLength) : null;
    if (!bytes || bytes.length > MAX_PACKET || bytes.length === 0 || c.count === QUEUE_SLOTS) { c.receiveDropped++; return; }
    const slot = (c.head + c.count) % QUEUE_SLOTS;
    c.queue.set(bytes, slot * MAX_PACKET);
    c.lens[slot] = bytes.length;
    c.count++;
    c.received++;
  }

  function open(cfg) {
    if (!cfg || typeof cfg.onSignal !== "function") return 0;
    const failEarly = () => { try { if (cfg.onState) cfg.onState("failed"); } catch (_) {} return 0; };
    if (!supported() || conns.size >= MAX_CONNECTIONS) return failEarly();
    let pc, dc;
    try {
      pc = new window.RTCPeerConnection({ iceServers: Array.isArray(cfg.iceServers) ? cfg.iceServers : [] });
      dc = pc.createDataChannel("netplay", { ordered: false, maxRetransmits: 0, negotiated: true, id: 0 });
    } catch (_) {
      try { if (pc) pc.close(); } catch (_) {}
      return failEarly();
    }
    dc.binaryType = "arraybuffer";
    const id = nextID++;
    const c = {
      id, cfg, pc, dc, state: "connecting", session: String(cfg.session || ""),
      queue: new Uint8Array(QUEUE_SLOTS * MAX_PACKET), lens: new Int32Array(QUEUE_SLOTS), head: 0, count: 0,
      received: 0, receiveDropped: 0, sent: 0, sendDropped: 0,
      offered: false, held: [], remoteSet: false, remoteReady: false, early: [], remoteCandidates: 0,
    };
    conns.set(id, c);
    dc.onopen = () => notify(c, "open");
    dc.onclose = () => { if (c.state === "connecting") fail(c); else notify(c, "closed"); };
    dc.onmessage = ev => pushIncoming(c, ev.data);
    pc.onconnectionstatechange = () => {
      if (pc.connectionState === "failed") fail(c);
      else if (pc.connectionState === "closed") notify(c, c.state === "connecting" ? "failed" : "closed");
    };
    pc.onicecandidate = ev => {
      const cand = ev.candidate
        ? { session: c.session, candidate: ev.candidate.candidate, sdpMid: ev.candidate.sdpMid || "", sdpMLineIndex: ev.candidate.sdpMLineIndex || 0, usernameFragment: ev.candidate.usernameFragment || "" }
        : { session: c.session, candidate: "", sdpMLineIndex: 0 };
      if (!c.offered) c.held.push(cand);
      else emit(c, "netplay.candidate", cand);
    };
    (async () => {
      try {
        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);
        const sdp = (pc.localDescription && pc.localDescription.sdp) || offer.sdp;
        if (typeof sdp !== "string" || sdp.length > MAX_SDP) throw new Error("offer size");
        if (c.state !== "connecting") return;
        emit(c, "netplay.offer", { session: c.session, sdp });
        c.offered = true;
        for (const cand of c.held) emit(c, "netplay.candidate", cand);
        c.held = [];
      } catch (_) { fail(c); }
    })();
    return id;
  }

  function applyRemote(c, data) {
    c.pc.addIceCandidate({ candidate: data.candidate, sdpMid: data.sdpMid, sdpMLineIndex: data.sdpMLineIndex }).catch(() => {});
  }

  function signal(id, event, json) {
    const c = conns.get(id);
    if (!c || c.state === "closed" || c.state === "failed") return;
    let data;
    try { data = JSON.parse(json); } catch (_) { return; }
    if (!data || typeof data !== "object" || data.session !== c.session) return;
    if (event === "netplay.answer") {
      if (c.remoteSet || typeof data.sdp !== "string" || data.sdp.length > MAX_SDP || data.sdp.indexOf("v=0") !== 0) return;
      c.remoteSet = true;
      c.pc.setRemoteDescription({ type: "answer", sdp: data.sdp }).then(() => {
        c.remoteReady = true;
        for (const cand of c.early) applyRemote(c, cand);
        c.early = [];
      }).catch(() => fail(c));
    } else if (event === "netplay.candidate") {
      if (typeof data.candidate !== "string" || data.candidate === "" || data.candidate.length > 1024) return;
      if (++c.remoteCandidates > MAX_CANDIDATES) return;
      if (!c.remoteReady) { c.early.push(data); return; }
      applyRemote(c, data);
    } else if (event === "netplay.failure") {
      fail(c);
    }
  }

  function send(id, bytes, n) {
    const c = conns.get(id);
    if (!c || c.state !== "open" || !bytes) return false;
    if (!(n > 0) || n > MAX_PACKET || n > bytes.length) { c.sendDropped++; return false; }
    if (c.dc.bufferedAmount > MAX_BUFFERED) { c.sendDropped++; return false; }
    try {
      c.dc.send(bytes.subarray(0, n));
      c.sent++;
      return true;
    } catch (_) { c.sendDropped++; return false; }
  }

  function recv(id, dst) {
    const c = conns.get(id);
    if (!c || c.count === 0 || !dst) return -1;
    const n = Math.min(c.lens[c.head], dst.length);
    dst.set(c.queue.subarray(c.head * MAX_PACKET, c.head * MAX_PACKET + n));
    c.head = (c.head + 1) % QUEUE_SLOTS;
    c.count--;
    return n;
  }

  function state(id) { const c = conns.get(id); return c ? c.state : "closed"; }

  function stats(id) {
    const c = conns.get(id);
    if (!c) return null;
    return { received: c.received, receiveDropped: c.receiveDropped, sent: c.sent, sendDropped: c.sendDropped, queued: c.count };
  }

  function close(id) {
    const c = conns.get(id);
    if (!c) return;
    if (c.state !== "failed") c.state = "closed";
    release(c);
    conns.delete(id);
  }

  const api = { version: 1, supported, open, signal, send, recv, state, stats, close };
  // The typed host namespace, like __gosx.workbench. No new ambient global.
  const host = window.__gosx;
  if (!host) return;
  host.netplay = api;

  const register = window.__gosx_register_bootstrap_feature;
  if (typeof register === "function") {
    register("netplay", function() {
      return {
        disposePage() { for (const id of Array.from(conns.keys())) close(id); },
      };
    });
  }
})();
