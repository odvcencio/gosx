import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const coreSource = fs.readFileSync(path.join(repoRoot, "client/js/bootstrap-src/10-runtime-scene-core.ts"), "utf8");
const mathSource = fs.readFileSync(path.join(repoRoot, "client/js/bootstrap-src/11-scene-math.ts"), "utf8");
const overlaySource = fs.readFileSync(path.join(repoRoot, "client/runtime/scene3d/overlay-dom.ts"), "utf8");

function functionSource(source, name) {
  const start = source.indexOf(`function ${name}(`);
  assert.notEqual(start, -1, `missing function ${name}`);
  const bodyStart = source.indexOf("{", start);
  let depth = 0;
  for (let index = bodyStart; index < source.length; index += 1) {
    if (source[index] === "{") depth += 1;
    if (source[index] === "}") {
      depth -= 1;
      // Sources are TypeScript; the test harness runs plain JavaScript, so drop
      // the ": any" parameter annotations.
      if (depth === 0) return source.slice(start, index + 1).replace(/\b(\w+): any\b/g, "$1");
    }
  }
  throw new Error(`unterminated function ${name}`);
}

function createHarness() {
  const context = vm.createContext({
    Math,
    Float32Array,
    Float64Array,
    Uint8Array,
    sceneNumber(value, fallback) {
      const number = Number(value);
      return Number.isFinite(number) ? number : fallback;
    },
    sceneClamp(value, min, max) { return Math.max(min, Math.min(max, value)); },
    sceneRenderCamera(camera) {
      return Object.assign({
        kind: "perspective", x: 0, y: 0, z: 5,
        rotationX: 0, rotationY: 0, rotationZ: 0,
        fov: 90, near: 0.1, far: 100,
      }, camera || {});
    },
    sceneOrthographicBounds() { return { left: -1, right: 1, top: 1, bottom: -1 }; },
    setAttrValue(element, name, value) { element.attributes[name] = String(value); },
    setStyleValue(style, name, value) { style[name] = String(value); },
    normalizeSceneHTMLPointerEvents(value) { return value || "none"; },
    clamp01(value) { return Math.max(0, Math.min(1, value)); },
    sceneHTMLTextureSurfaceIsLive() { return false; },
    normalizeSceneHTMLMode(value, fallback) { return String(value || fallback || "dom").toLowerCase(); },
  });
  vm.runInContext(mathSource, context, { filename: "11-scene-math.ts" });
  vm.runInContext(functionSource(coreSource, "sceneHTMLPerspectiveCorners"), context, { filename: "10-runtime-scene-core.ts" });
  vm.runInContext(functionSource(overlaySource, "sceneHTMLPerspectiveTransform"), context, { filename: "overlay-dom.ts" });
  vm.runInContext(functionSource(overlaySource, "renderSceneHTMLElement"), context, { filename: "overlay-dom.ts" });
  return context;
}

function parseMatrix3d(value) {
  const match = /^matrix3d\(([^)]+)\)$/.exec(value);
  assert.ok(match, `expected matrix3d(), got ${value}`);
  return match[1].split(",").map(Number);
}

function mapCSSPoint(matrix, x, y) {
  const w = matrix[3] * x + matrix[7] * y + matrix[15];
  return {
    x: (matrix[0] * x + matrix[4] * y + matrix[12]) / w,
    y: (matrix[1] * x + matrix[5] * y + matrix[13]) / w,
  };
}

function element(width, height) {
  return {
    offsetWidth: width,
    offsetHeight: height,
    attributes: {},
    style: {},
    innerHTML: "",
  };
}

const camera = { kind: "perspective", x: 0, y: 0, z: 5, fov: 90, near: 0.1, far: 100 };

test("perspective matrix3d maps all CSS box corners to projected rotated plane corners", () => {
  const context = createHarness();
  const entry = {
    x: 0.2, y: -0.1, z: 0,
    surfaceWidth: 2.4, surfaceHeight: 1.3,
    rotationX: 0.27, rotationY: -0.42, rotationZ: 0.18,
  };
  const corners = context.sceneHTMLPerspectiveCorners(entry, camera, 640, 480, 0);
  assert.equal(corners.length, 4);
  const target = element(240, 130);
  const matrix = parseMatrix3d(context.sceneHTMLPerspectiveTransform(target, corners));
  const source = [[0, 0], [240, 0], [0, 130], [240, 130]];
  for (let index = 0; index < source.length; index += 1) {
    const actual = mapCSSPoint(matrix, source[index][0], source[index][1]);
    assert.ok(Math.abs(actual.x - corners[index].x) < 1e-6, `corner ${index} x: ${actual.x} != ${corners[index].x}`);
    assert.ok(Math.abs(actual.y - corners[index].y) < 1e-6, `corner ${index} y: ${actual.y} != ${corners[index].y}`);
  }
  assert.equal(target.__gosxHTMLPerspectiveSize.width, 240);
  assert.equal(target.__gosxHTMLPerspectiveSize.height, 130);
});

test("plane facing the camera gets an axis-aligned scale and translate matrix", () => {
  const context = createHarness();
  const corners = context.sceneHTMLPerspectiveCorners({ surfaceWidth: 2, surfaceHeight: 1 }, camera, 100, 80, 0);
  const matrix = parseMatrix3d(context.sceneHTMLPerspectiveTransform(element(200, 100), corners));
  assert.ok(Math.abs(matrix[0] - 0.08) < 1e-12);
  assert.ok(Math.abs(matrix[5] - 0.08) < 1e-12);
  for (const index of [1, 4, 3, 7]) assert.ok(Math.abs(matrix[index]) < 1e-12, `matrix[${index}]=${matrix[index]}`);
  assert.equal(matrix[12], 42);
  assert.equal(matrix[13], 36);
});

test("a perspective plane behind the camera is hidden and aria-hidden", () => {
  const context = createHarness();
  const corners = context.sceneHTMLPerspectiveCorners({ z: 6, surfaceWidth: 2, surfaceHeight: 1 }, camera, 100, 80, 0);
  const target = element(200, 100);
  context.renderSceneHTMLElement(target, {
    id: "behind", mode: "dom", perspective: true, perspectiveCorners: corners,
    html: "<button>Behind</button>", pointerEvents: "auto",
  }, { anchor: { x: 0, y: 0 } }, false, false);
  assert.equal(target.style.visibility, "hidden");
  assert.equal(target.attributes["aria-hidden"], "true");
  assert.equal(target.attributes["data-gosx-scene-html-visibility"], "hidden");
  assert.equal(target.style["--gosx-scene-html-pointer-events"], "auto");
});

test("perspective element dimensions are measured once and reused", () => {
  const context = createHarness();
  let reads = 0;
  const target = element(90, 42);
  Object.defineProperty(target, "offsetWidth", { get() { reads += 1; return 90; } });
  const corners = context.sceneHTMLPerspectiveCorners({ surfaceWidth: 1, surfaceHeight: 1 }, camera, 100, 80, 0);
  context.sceneHTMLPerspectiveTransform(target, corners);
  context.sceneHTMLPerspectiveTransform(target, corners);
  assert.equal(reads, 1);
});
