const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const generatedSource = fs.readFileSync(path.join(__dirname, "../generated/runtime-abi.ts"), "utf8");
const mailboxSource = fs.readFileSync(path.join(__dirname, "mailbox.ts"), "utf8");

function load(constructed) {
  const RealDecoder = TextDecoder;
  const window = {};
  const context = {
    window: window,
    gosxRuntime: {},
    Uint8Array, ArrayBuffer, DataView, Error, TypeError, String,
    TextDecoder: function() { constructed.count++; return new RealDecoder(); },
  };
  vm.createContext(context);
  vm.runInContext(generatedSource, context);
  vm.runInContext(mailboxSource, context);
  return { mailbox: context.gosxRuntime.mailbox, contract: window.__gosx_runtime_contract };
}

const enc = new TextEncoder();
function str(parts, s) {
  const b = typeof s === "string" ? enc.encode(s) : s;
  const len = new Uint8Array(4);
  new DataView(len.buffer).setUint32(0, b.length, true);
  parts.push(len, b);
}
function u32(parts, n) {
  const b = new Uint8Array(4);
  new DataView(b.buffer).setUint32(0, n, true);
  parts.push(b);
}
function build(contract, island, patches) {
  const parts = [];
  str(parts, island);
  u32(parts, patches.length);
  for (const p of patches) {
    parts.push(Uint8Array.of(p.kind));
    str(parts, p.path); str(parts, p.tag); str(parts, p.text); str(parts, p.attrName);
    u32(parts, p.children.length);
    for (const c of p.children) {
      const b = new Uint8Array(4);
      new DataView(b.buffer).setInt32(0, c, true);
      parts.push(b);
    }
  }
  const total = parts.reduce((n, b) => n + b.length, 0);
  const out = new Uint8Array(24 + total);
  const v = new DataView(out.buffer);
  v.setUint32(0, contract.mailboxMagic, true);
  v.setUint16(4, contract.mailboxVersion, true);
  v.setUint16(6, contract.opcodes.patches, true);
  v.setUint32(12, total, true);
  v.setUint32(20, contract.mailboxFlagResponse, true);
  let pos = 24;
  for (const b of parts) { out.set(b, pos); pos += b.length; }
  return out;
}

const texts = [
  "", "a", "div", "0.1.2.3", "class", "x".repeat(23), "x".repeat(24), "x".repeat(300),
  "café", "日本語", "emoji 🚢", "﻿bom", "mix ascii é tail",
];

test("mailbox strings decode like a fresh TextDecoder", () => {
  const constructed = { count: 0 };
  const { mailbox, contract } = load(constructed);
  const patches = texts.map((t, i) => ({ kind: i % 7, path: t, tag: t, text: t, attrName: t, children: [i, -i] }));
  const bad = Uint8Array.of(0x61, 0xff, 0xc3, 0x28, 0x62);
  patches.push({ kind: 1, path: bad, tag: "p", text: bad, attrName: "", children: [] });
  const decoded = mailbox.decodePatchMailbox(build(contract, "islé", patches));
  assert.equal(decoded.islandID, "islé");
  const ref = (b) => new TextDecoder().decode(b);
  decoded.patches.forEach((p, i) => {
    const want = i < texts.length ? ref(enc.encode(texts[i])) : ref(bad);
    assert.equal(p.path, want);
    assert.equal(p.text, want);
    assert.equal(p.attrName, i < texts.length ? want : "");
  });
});

test("mailbox constructs at most one TextDecoder per runtime", () => {
  const constructed = { count: 0 };
  const { mailbox, contract } = load(constructed);
  const patches = [];
  for (let i = 0; i < 200; i++) patches.push({ kind: 1, path: "0." + i, tag: "span", text: "value é " + i, attrName: "", children: [] });
  const buf = build(contract, "island-with-a-long-identifier-1", patches);
  mailbox.decodePatchMailbox(buf);
  mailbox.decodePatchMailbox(buf);
  assert.equal(constructed.count, 1);
});
