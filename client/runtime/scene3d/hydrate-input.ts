// Strict Scene3D initial-hydrate envelope boundary. Loaded only when a shared
// runtime has an initial program; bootstrap.js carries this API inline.
// @ts-check

(function() {
  "use strict";

  function scene3DHydrateRecord(value) {
    if (!value || typeof value !== "object" || Array.isArray(value) ||
        typeof ArrayBuffer !== "undefined" && ArrayBuffer.isView && ArrayBuffer.isView(value)) return false;
    const proto = Object.getPrototypeOf(value);
    if (proto === null) return true;
    const descriptor = Object.getOwnPropertyDescriptor(proto, "constructor");
    const constructor = descriptor && descriptor.value;
    const prototype = typeof constructor === "function" &&
      Object.getOwnPropertyDescriptor(constructor, "prototype");
    return Object.getPrototypeOf(proto) === null && prototype && prototype.value === proto &&
      Function.prototype.toString.call(constructor) === Function.prototype.toString.call(Object);
  }

  function scene3DHydrateShape(value, keys) {
    const fields = [];
    return scene3DHydrateRecord(value) && Reflect.ownKeys(value).length === keys.length && keys.every(function(key) {
      const descriptor = Object.getOwnPropertyDescriptor(value, key);
      return descriptor && descriptor.enumerable && !("get" in descriptor) && (fields.push(descriptor.value), true);
    }) ? fields : null;
  }

  function scene3DHydrateFail() {
    throw new Error("invalid Scene3D hydrate envelope");
  }

  function scene3DHydrateArray(value) {
    const items = Object.getOwnPropertyDescriptors(value);
    const length = items.length.value;
    if (Reflect.ownKeys(items).length - 1 !== length) return null;
    return Array.from({ length }, function(_, index) {
      const item = items[index];
      return item && item.enumerable && "value" in item && item.value;
    });
  }

  function decodeScene3DInitialHydrateEnvelope(value, targetID) {
    const envelope = scene3DHydrateShape(value, ["version", "surfaceKind", "outputKind", "targetId", "mode", "commands"]);
    if (!envelope || envelope[0] !== 1 || envelope[1] !== "scene3d" ||
        envelope[2] !== "scene3d.commands" || envelope[3] !== targetID ||
        envelope[4] !== "initial" || !Array.isArray(envelope[5])) scene3DHydrateFail();
    const commands = scene3DHydrateArray(envelope[5]);
    if (!commands) scene3DHydrateFail();
    for (let index = 0; index < commands.length; index += 1) {
      let command = scene3DHydrateShape(commands[index], ["kind", "objectId"]);
      if (!command) command = scene3DHydrateShape(commands[index], ["kind", "objectId", "data"]);
      const kind = command && command[0];
      if (!command || (kind === 1) !== (command.length === 2) ||
          !Number.isInteger(kind) || kind < 0 || kind > 6 ||
          !Number.isInteger(command[1]) || command[1] < 0) scene3DHydrateFail();
      const data = command[2];
      commands[index] = { kind, objectId: command[1], data };
      if (kind === 1) continue;
      if (kind === 6) {
        if (!scene3DHydrateRecord(data) && !(Array.isArray(data) && data.every(scene3DHydrateRecord))) {
          scene3DHydrateFail();
        }
        continue;
      }
      if (!scene3DHydrateRecord(data)) scene3DHydrateFail();
      if (kind === 0) {
        const create = scene3DHydrateShape(data, ["kind", "geometry", "material", "props", "children", "static"]);
        if (!create || typeof create[0] !== "string" || !create[0] || typeof create[1] !== "string" ||
            typeof create[2] !== "string" || create[3] !== null && !scene3DHydrateRecord(create[3]) ||
            create[4] !== null && (!Array.isArray(create[4]) || !create[4].every(function(child) { return Number.isInteger(child) && child >= 0; })) ||
            typeof create[5] !== "boolean") scene3DHydrateFail();
      }
    }
    return commands;
  }

  async function hydrateScene3DInitialProgram(ctx) {
    if (!ctx.runtime || !ctx.runtime.available()) scene3DHydrateFail();
    const output = await ctx.runtime.hydrateFromProgramRef();
    if (ctx.isCurrent && !ctx.isCurrent()) return null;
    const commands = decodeScene3DInitialHydrateEnvelope(output, ctx.id);
    ctx._ssr = false;
    return commands;
  }

  // Reuse the governed runtime namespace; bootstrap/runtime publish it first.
  window.__gosx_runtime_api.scene3DHydrateInitialProgram = hydrateScene3DInitialProgram;
})();
