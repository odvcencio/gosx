// GoSX binary mailbox codec.
// @ts-check

/**
 * @typedef {object} GoSXRuntimeMailboxHeader
 * @property {number} magic
 * @property {number} version
 * @property {number} opcode
 * @property {number} requestID
 * @property {number} payloadSize
 * @property {number} status
 * @property {number} flags
 *
 * @typedef {object} GoSXDecodedPatchMailbox
 * @property {GoSXRuntimeMailboxHeader} header
 * @property {string} islandID
 * @property {Array<object>} patches
 */

(function() {
  "use strict";
  if (typeof window === "undefined") return;

  const contract = window.__gosx_runtime_contract;
  if (!contract) throw new Error("generated runtime ABI contract is missing");
  const MAGIC = contract.mailboxMagic;
  const VERSION = contract.mailboxVersion;
  const HEADER_BYTES = contract.mailboxHeaderBytes;
  const FLAG_RESPONSE = contract.mailboxFlagResponse;
  const STATUS_OK = contract.mailboxStatusOK;
  const OPCODE_PATCHES = contract.opcodes.patches;
  const MAX_PAYLOAD = contract.mailboxMaxPayload;

  function asBytes(value) {
    if (value instanceof Uint8Array) return value;
    if (value instanceof ArrayBuffer) return new Uint8Array(value);
    if (ArrayBuffer.isView(value)) return new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
    throw new TypeError("runtime mailbox requires a Uint8Array or ArrayBuffer");
  }

  function decodeUTF8(bytes) {
    if (typeof TextDecoder === "function") return new TextDecoder().decode(bytes);
    let out = "";
    for (let i = 0; i < bytes.length; i++) out += String.fromCharCode(bytes[i]);
    return out;
  }

  function readString(view, cursor) {
    const length = view.getUint32(cursor.pos, true);
    cursor.pos += 4;
    if (length > view.byteLength - cursor.pos) throw new Error("runtime mailbox string is truncated");
    const bytes = new Uint8Array(view.buffer, view.byteOffset + cursor.pos, length);
    cursor.pos += length;
    return decodeUTF8(bytes);
  }

  function decodeHeader(input) {
    const bytes = asBytes(input);
    if (bytes.byteLength < HEADER_BYTES) throw new Error("runtime mailbox is shorter than its header");
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    const header = {
      magic: view.getUint32(0, true),
      version: view.getUint16(4, true),
      opcode: view.getUint16(6, true),
      requestID: view.getUint32(8, true),
      payloadSize: view.getUint32(12, true),
      status: view.getInt32(16, true),
      flags: view.getUint32(20, true),
    };
    if (header.magic !== MAGIC) throw new Error("runtime mailbox magic is invalid");
    if (header.version !== VERSION) throw new Error("runtime mailbox version is unsupported");
    if (header.payloadSize > MAX_PAYLOAD) throw new Error("runtime mailbox payload is too large");
    if (HEADER_BYTES + header.payloadSize !== bytes.byteLength) throw new Error("runtime mailbox length does not match its header");
    return { header: header, bytes: bytes, view: view };
  }

  function decodePatchMailbox(input) {
    const decoded = decodeHeader(input);
    const header = decoded.header;
    if (header.opcode !== OPCODE_PATCHES || (header.flags & FLAG_RESPONSE) === 0) {
      throw new Error("runtime mailbox is not a patch response");
    }
    const cursor = { pos: HEADER_BYTES };
    const islandID = readString(decoded.view, cursor);
    const count = decoded.view.getUint32(cursor.pos, true);
    cursor.pos += 4;
    if (count > decoded.view.byteLength) throw new Error("runtime mailbox patch count is invalid");
    const patches = [];
    for (let i = 0; i < count; i++) {
      if (cursor.pos >= decoded.view.byteLength) throw new Error("runtime mailbox patch is truncated");
      const kind = decoded.view.getUint8(cursor.pos++);
      const path = readString(decoded.view, cursor);
      const tag = readString(decoded.view, cursor);
      const text = readString(decoded.view, cursor);
      const attrName = readString(decoded.view, cursor);
      if (cursor.pos + 4 > decoded.view.byteLength) throw new Error("runtime mailbox child count is truncated");
      const childCount = decoded.view.getUint32(cursor.pos, true);
      cursor.pos += 4;
      if (childCount > (decoded.view.byteLength - cursor.pos) / 4) throw new Error("runtime mailbox child list is truncated");
      const children = [];
      for (let childIndex = 0; childIndex < childCount; childIndex++) {
        children.push(decoded.view.getInt32(cursor.pos, true));
        cursor.pos += 4;
      }
      patches.push({ kind: kind, path: path, tag: tag, text: text, attrName: attrName, children: children });
    }
    if (cursor.pos !== decoded.view.byteLength) throw new Error("runtime mailbox patch payload has trailing bytes");
    return { header: header, islandID: islandID, patches: patches };
  }

  function decodeMailbox(input) {
    const decoded = decodeHeader(input);
    return {
      header: decoded.header,
      payload: decoded.bytes.slice(HEADER_BYTES),
    };
  }

  gosxRuntime.mailbox = {
    magic: MAGIC,
    version: VERSION,
    headerBytes: HEADER_BYTES,
    statusOK: STATUS_OK,
    opcodePatches: OPCODE_PATCHES,
    decode: decodeMailbox,
    decodePatchMailbox: decodePatchMailbox,
  };
})();
