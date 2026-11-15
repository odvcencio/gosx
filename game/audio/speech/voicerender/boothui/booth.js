(() => {
  "use strict";
  const token = new URLSearchParams(location.search).get("token") || "";
  const $ = (id) => document.getElementById(id);
  const HINTS = {
    start: "Start of a sentence: open with energy, as if more follows.",
    mid: "Middle of a sentence: keep it level, as if the sentence goes on.",
    end: "End of a sentence: let the pitch fall and finish the line.",
  };
  let s_hasRef = true;
  let lines = [], cur = 0, voice = "", ctx = null, node = null, stream = null;
  let recording = false, chunks = [], lastTake = null, previewURL = null, busy = false;

  const api = (path, opt = {}) => fetch(path, { ...opt, headers: { ...(opt.headers || {}), "X-Booth-Token": token } });
  const say = (m) => { $("status").textContent = m; };

  async function load() {
    const q = new URLSearchParams(location.search).get("voice") || "";
    const r = await api("/api/state?voice=" + encodeURIComponent(q));
    if (!r.ok) { say("Could not load the script: " + r.status); return; }
    const s = await r.json();
    voice = s.voice; lines = s.lines; s_hasRef = !!s.reference;
    $("who").textContent = "(" + voice + ")";
    cur = Math.max(0, lines.findIndex((l) => !l.have));
    render();
  }

  function render() {
    const l = lines[cur];
    const have = lines.filter((x) => x.have).length;
    $("progress").textContent = have + " of " + lines.length + " recorded";
    if (!l) { $("line").textContent = "Nothing to record."; return; }
    $("variant").textContent = l.variant;
    $("key").textContent = l.key + (l.have ? " (recorded)" : "");
    $("line").textContent = l.text;
    $("hint").textContent = HINTS[l.variant] || "";
    $("record").textContent = recording ? "Stop (space)" : "Record (space)";
    $("record").classList.toggle("rec", recording);
    $("retake").disabled = !l.have && !lastTake;
    $("play").disabled = !lastTake;
    $("ref").disabled = !s_hasRef;
    const list = $("list");
    list.textContent = "";
    lines.forEach((x, i) => {
      const li = document.createElement("li");
      li.textContent = x.key + " / " + x.variant;
      if (x.have) li.className = "done";
      if (i === cur) li.className += " cur";
      li.onclick = () => go(i);
      list.appendChild(li);
    });
    const c = list.querySelector(".cur");
    if (c) c.scrollIntoView({ block: "nearest" });
  }

  function go(i) {
    if (recording) return;
    cur = Math.min(Math.max(i, 0), lines.length - 1);
    lastTake = null;
    render();
  }

  async function ensureAudio() {
    if (ctx) return;
    stream = await navigator.mediaDevices.getUserMedia({
      audio: { echoCancellation: false, noiseSuppression: false, autoGainControl: false, channelCount: 1 },
    });
    ctx = new AudioContext();
    await ctx.audioWorklet.addModule("recorder-worklet.js");
    const src = ctx.createMediaStreamSource(stream);
    node = new AudioWorkletNode(ctx, "booth-recorder", { numberOfInputs: 1, numberOfOutputs: 0 });
    node.port.onmessage = (e) => {
      meter(e.data.peak);
      if (recording && e.data.samples) chunks.push(e.data.samples);
    };
    src.connect(node);
  }

  function meter(peak) {
    const db = peak > 0 ? 20 * Math.log10(peak) : -90;
    const pct = Math.max(0, Math.min(100, (db + 60) / 60 * 100));
    const bar = $("bar");
    bar.style.width = pct + "%";
    bar.className = db > -1 ? "clip" : db > -9 ? "hot" : "";
  }

  async function toggle() {
    if (busy) return;
    if (!recording) {
      try { await ensureAudio(); await ctx.resume(); } catch (e) { say("No microphone: " + e.message); return; }
      chunks = []; recording = true; node.port.postMessage({ record: true });
      say("Recording...");
      render();
      return;
    }
    recording = false; node.port.postMessage({ record: false });
    busy = true;
    const wav = toWAV(chunks, ctx.sampleRate);
    chunks = [];
    if (!wav) { busy = false; say("Nothing was recorded."); render(); return; }
    const l = lines[cur];
    const r = await api("/api/take?index=" + l.index, { method: "POST", body: wav });
    busy = false;
    if (!r.ok) { say("Save failed: " + (await r.text())); render(); return; }
    l.have = true; lastTake = wav;
    say("Saved. Press n for the next line, or r to retake.");
    render();
  }

  function toWAV(parts, rate) {
    let n = 0; for (const p of parts) n += p.length;
    if (n < rate * 0.05) return null;
    const buf = new ArrayBuffer(44 + n * 2), v = new DataView(buf);
    const str = (o, s) => { for (let i = 0; i < s.length; i++) v.setUint8(o + i, s.charCodeAt(i)); };
    str(0, "RIFF"); v.setUint32(4, 36 + n * 2, true); str(8, "WAVEfmt ");
    v.setUint32(16, 16, true); v.setUint16(20, 1, true); v.setUint16(22, 1, true);
    v.setUint32(24, rate, true); v.setUint32(28, rate * 2, true); v.setUint16(32, 2, true); v.setUint16(34, 16, true);
    str(36, "data"); v.setUint32(40, n * 2, true);
    let o = 44;
    for (const p of parts) for (let i = 0; i < p.length; i++, o += 2) {
      const x = Math.max(-1, Math.min(1, p[i]));
      v.setInt16(o, x < 0 ? x * 32768 : x * 32767, true);
    }
    return buf;
  }

  async function playBlob(bytes) {
    const c = ctx || new AudioContext();
    const b = await c.decodeAudioData(bytes.slice(0));
    const s = c.createBufferSource(); s.buffer = b; s.connect(c.destination); s.start();
  }

  $("record").onclick = toggle;
  $("retake").onclick = () => { if (!recording) { lastTake = null; toggle(); } };
  $("play").onclick = () => { if (lastTake) playBlob(lastTake); };
  $("ref").onclick = async () => {
    const r = await api("/api/ref?index=" + lines[cur].index);
    if (!r.ok) { s_hasRef = false; say("No reference voice is configured."); render(); return; }
    playBlob(await r.arrayBuffer());
  };
  $("skip").onclick = () => go(cur + 1);
  $("next").onclick = () => go(cur + 1);
  $("prev").onclick = () => go(cur - 1);
  addEventListener("keydown", (e) => {
    if (e.target.tagName === "BUTTON" && e.code === "Space") e.preventDefault();
    const k = e.key.toLowerCase();
    if (e.code === "Space") { e.preventDefault(); toggle(); }
    else if (k === "r") $("retake").click();
    else if (k === "p") $("play").click();
    else if (k === "f") $("ref").click();
    else if (k === "s") $("skip").click();
    else if (k === "n") $("next").click();
    else if (k === "b") $("prev").click();
  });
  load();
})();
