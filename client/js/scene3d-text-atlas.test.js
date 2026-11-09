"use strict";
// text-atlas.ts draws Props.TextAtlases with the canvas 2D API and resolves
// "gosx-text-atlas:" texture URLs to the drawn image for both renderers. It
// ships in an opt-in chunk; optional-chunks.ts holds the base-side shims.
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const ts = require("../runtime/node_modules/typescript");

function transpile(file) {
  const source = fs.readFileSync(path.join(__dirname, "../runtime/scene3d", file), "utf8");
  return ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
}
const code = transpile("text-atlas.ts");
const shimCode = transpile("optional-chunks.ts");

function load() {
  const canvases = [];
  const document = { createElement(tag) {
    assert.equal(tag, "canvas");
    const ops = [];
    const ctx = {
      ops, font: "",
      clearRect(...a) { ops.push(["clearRect", ...a]); },
      fillRect(...a) { ops.push(["fillRect", ...a]); },
      // Each character is as wide as 0.6 of the font size.
      measureText(text) { const size = Number(/(\d+(\.\d+)?)px/.exec(this.font)[1]); return { width: text.length * size * 0.6 }; },
      fillText(text, x, y) { ops.push(["fillText", text, x, y, this.font, this.fillStyle]); },
      strokeText(text, x, y) { ops.push(["strokeText", text, x, y, this.lineWidth, this.strokeStyle]); },
    };
    const canvas = { ctx, getContext: kind => (kind === "2d" ? ctx : null), toDataURL: type => "data:" + type + ";base64,ATLAS" + canvases.length };
    canvases.push(canvas);
    return canvas;
  } };
  const window = {};
  const context = vm.createContext({ window, document });
  vm.runInContext(code, context);
  return { api: window.__gosx_scene3d_text_atlas_api, window, canvases };
}

const spec = {
  id: "numbers", texture: "gosx-text-atlas:numbers#00ff", labels: ["7", "", "1234567890"],
  cellWidth: 100, cellHeight: 50, columns: 2, font: "sans-serif", weight: "700", color: "#ffffff",
  outlineColor: "#000000", outlineWidth: 2, padding: 0.1,
};

test("atlases draw once, skip blank cells and shrink long labels to fit", () => {
  const h = load();
  h.api.register([spec]);
  h.api.register([spec]);
  assert.equal(h.canvases.length, 1, "one draw per texture URL");
  const canvas = h.canvases[0];
  assert.equal(canvas.width, 200);
  assert.equal(canvas.height, 100, "three labels in two columns make two rows");
  const fills = canvas.ctx.ops.filter(op => op[0] === "fillText");
  assert.deepEqual(fills.map(op => op[1]), ["7", "1234567890"], "the blank label is not drawn");
  // Label 0 sits at the center of cell (0,0); label 2 at the center of cell (0,1).
  assert.deepEqual(fills.map(op => [op[2], op[3]]), [[50, 25], [50, 75]]);
  // "7" keeps the padded cell height as its size; the long label shrinks to the padded width minus the outline.
  assert.match(fills[0][4], /^700 40px sans-serif$/);
  const longSize = Number(/(\d+(\.\d+)?)px/.exec(fills[1][4])[1]);
  assert.ok(Math.abs(longSize * 0.6 * 10 - (100 * 0.8 - 4)) < 1e-6, "long label width " + longSize * 6);
  assert.equal(canvas.ctx.ops.filter(op => op[0] === "strokeText").length, 2, "outlined labels get a stroke");
});

test("a background fills each labeled cell inside a transparent margin", () => {
  const h = load();
  h.api.register([Object.assign({}, spec, { texture: "gosx-text-atlas:plates#01", background: "#f2d14b" })]);
  const rects = h.canvases[0].ctx.ops.filter(op => op[0] === "fillRect");
  // Cells are 100x50, so the margin is floor(50 / 12) = 4 px; the blank
  // label 1 gets no fill, so filtering cannot bleed a fill into it.
  assert.deepEqual(rects.map(op => op.slice(1)), [[4, 4, 92, 42], [4, 54, 92, 42]]);
});

test("atlas URLs resolve to the drawn image and other URLs pass through", () => {
  const h = load();
  assert.equal(h.api.url(spec.texture), "", "an atlas not drawn yet resolves to no image, so nothing fetches the scheme URL");
  h.api.register([spec]);
  assert.equal(h.api.url(spec.texture), "data:image/png;base64,ATLAS1");
  assert.equal(h.api.url("/textures/wood.png"), "/textures/wood.png");
  assert.equal(h.api.canvas(spec.texture), h.canvases[0], "the canvas stays available for a direct upload");
  // A new label set is a new URL and a new drawing.
  h.api.register([Object.assign({}, spec, { texture: "gosx-text-atlas:numbers#0100", labels: ["8"] })]);
  assert.equal(h.canvases.length, 2);
  h.api.register(null);
});

// The base chunk keeps only optional-chunks.ts: it loads the text atlas chunk
// when a later prop update brings atlases, resolves atlas URLs through the
// chunk once it is there, and delegates warm-up frames to the warm-up chunk.
test("base shims load the opt-in chunks and delegate to them", async () => {
  const loads = [];
  const events = [];
  const window = { __gosx_scene3d_api: {} };
  const helpers = { sceneObjectMaterialProfile() {}, sceneWorldObjectRenderPass() {}, scenePBRObjectRenderPass() {} };
  const context = vm.createContext(Object.assign({ window, CustomEvent: class { constructor(type) { this.type = type; } },
    ensureSceneGatedFeatureLoaded(kind, key, fallback) { loads.push([kind, key, fallback]); return Promise.resolve({}); } }, helpers));
  vm.runInContext(shimCode + "\n;globalThis.__s = { sceneTextAtlasURL, sceneRegisterTextAtlases, sceneShaderWarmupFrame, sceneShaderWarmupSettle };", context);
  const s = context.__s;
  const mount = { dispatchEvent(event) { events.push(event.type); } };
  assert.equal(s.sceneTextAtlasURL(spec.texture), "", "no chunk yet: no image");
  assert.equal(s.sceneTextAtlasURL("/a.png"), "/a.png");
  s.sceneRegisterTextAtlases({}, mount);
  assert.equal(loads.length, 0, "a scene without atlases loads nothing");
  s.sceneRegisterTextAtlases({ textAtlases: [spec] }, mount);
  assert.deepEqual(loads, [["text-atlas", "gosxScene3dTextAtlasUrl", ""]]);
  await Promise.resolve(); await Promise.resolve();
  assert.deepEqual(events, ["gosx:scene3d:program-ready"], "the mount redraws when the chunk lands");
  const registered = [];
  window.__gosx_scene3d_text_atlas_api = { register: list => registered.push(list), url: () => "data:x" };
  s.sceneRegisterTextAtlases({ textAtlases: [spec] }, mount);
  assert.deepEqual(registered, [[spec]]);
  assert.equal(s.sceneTextAtlasURL(spec.texture), "data:x");
  assert.equal(window.__gosx_scene3d_api.sceneTextAtlasURL, s.sceneTextAtlasURL, "renderer chunks import the resolver from the scene API");
  assert.equal(s.sceneShaderWarmupFrame({}, { shaderWarmup: {} }, {}), null, "no warm-up chunk: no warm-up bundle");
  assert.equal(s.sceneShaderWarmupSettle({ warmup: {} }, "pending"), "pending");
  window.__gosx_scene3d_warmup_api = { frame: (_m, _p, _r, profile) => (profile === helpers.sceneObjectMaterialProfile ? "bundle" : "no profile"), settle: (_meta, outcome) => outcome + "!" };
  assert.equal(s.sceneShaderWarmupFrame({}, {}, {}), null, "a scene without Props.shaderWarmup pays one property read");
  assert.equal(s.sceneShaderWarmupFrame({}, { shaderWarmup: {} }, {}), "bundle");
  assert.equal(s.sceneShaderWarmupSettle({ warmup: {} }, "submitted"), "submitted!");
  assert.equal(s.sceneShaderWarmupSettle({}, "submitted"), "submitted");
});
