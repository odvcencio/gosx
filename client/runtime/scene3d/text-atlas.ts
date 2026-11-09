// text-atlas.ts — label textures drawn with the canvas 2D API (Props.TextAtlases).
//
// A text atlas is a grid of short labels (jersey numbers, nameplates) that
// many materials share; each material picks its cell with a UV region. This
// module ships in the opt-in bootstrap-feature-scene3d-text-atlas.js chunk,
// which a page advertises only for scenes that declare text atlases (the
// monolith carries it inline). The mount registers the atlases before each
// frame through optional-chunks.ts. Drawing happens once per distinct texture
// URL ("gosx-text-atlas:<id>#<hash>"): the canvas is encoded as a PNG data URL,
// and both renderers' texture descriptors resolve the atlas URL to it through
// sceneTextAtlasURL, so standard maps and Selena texture params load it like
// any image. The API's canvas() keeps the canvas for a direct upload path (a
// WriteTexture-style API) to use instead.
(function() {
const sceneTextAtlasImages = new Map<string, string>();
const sceneTextAtlasCanvases = new Map<string, any>();
const SCENE_TEXT_ATLAS_SCHEME = "gosx-text-atlas:";
const num = function(value: any, fallback: number): number { const n = Number(value); return value == null || value === "" || !Number.isFinite(n) ? fallback : n; };

function sceneTextAtlasDraw(spec: any): any {
  if (typeof document === "undefined" || typeof document.createElement !== "function") return null;
  const labels = Array.isArray(spec.labels) ? spec.labels : [];
  const cw = Math.max(1, spec.cellWidth | 0), ch = Math.max(1, spec.cellHeight | 0);
  const cols = Math.max(1, spec.columns | 0), rows = Math.max(1, Math.ceil(labels.length / cols));
  const canvas: any = document.createElement("canvas");
  canvas.width = cols * cw;
  canvas.height = rows * ch;
  const ctx = typeof canvas.getContext === "function" ? canvas.getContext("2d") : null;
  if (!ctx) return null;
  const pad = Math.min(0.45, Math.max(0, num(spec.padding, 0.08)));
  const outline = num(spec.outlineWidth, 0);
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  ctx.lineJoin = "round";
  for (let i = 0; i < labels.length; i++) {
    const text = labels[i] == null ? "" : String(labels[i]);
    if (!text) continue; // a blank cell stays transparent
    const x = (i % cols) * cw, y = Math.floor(i / cols) * ch;
    // The fill stops short of the cell edge so texture filtering and mipmaps
    // do not bleed it into a neighboring (possibly blank) cell.
    const gutter = Math.min(4, Math.floor(Math.min(cw, ch) / 12));
    if (spec.background) { ctx.fillStyle = spec.background; ctx.fillRect(x + gutter, y + gutter, cw - 2 * gutter, ch - 2 * gutter); }
    // Start at the cell height and shrink until the text fits the width.
    let size = ch * (1 - 2 * pad);
    ctx.font = spec.weight + " " + size + "px " + spec.font;
    const width = ctx.measureText(text).width, room = cw * (1 - 2 * pad) - outline * 2;
    if (width > room && width > 0) {
      size = Math.max(1, size * room / width);
      ctx.font = spec.weight + " " + size + "px " + spec.font;
    }
    if (outline > 0 && spec.outlineColor) {
      ctx.lineWidth = outline * 2;
      ctx.strokeStyle = spec.outlineColor;
      ctx.strokeText(text, x + cw / 2, y + ch / 2);
    }
    ctx.fillStyle = spec.color || "#ffffff";
    ctx.fillText(text, x + cw / 2, y + ch / 2);
  }
  return canvas;
}

// sceneTextAtlasRegister draws the listed atlases that are not drawn yet.
function sceneTextAtlasRegister(list: any): void {
  if (!Array.isArray(list)) return;
  for (const spec of list) {
    const key = spec && typeof spec.texture === "string" ? spec.texture : "";
    if (!key || sceneTextAtlasImages.has(key)) continue;
    const canvas = sceneTextAtlasDraw(spec);
    let url = "";
    try { url = canvas && typeof canvas.toDataURL === "function" ? canvas.toDataURL("image/png") : ""; } catch (_error) { url = ""; }
    sceneTextAtlasImages.set(key, url);
    if (canvas) sceneTextAtlasCanvases.set(key, canvas);
  }
}

// sceneTextAtlasResolve returns the drawn image of an atlas URL, or "" when the
// atlas is not drawn (or the browser could not draw it).
function sceneTextAtlasResolve(url: string): string {
  return url.indexOf(SCENE_TEXT_ATLAS_SCHEME) === 0 ? sceneTextAtlasImages.get(url) || "" : url;
}

(window as any).__gosx_scene3d_text_atlas_api = {
  register: sceneTextAtlasRegister,
  url: sceneTextAtlasResolve,
  canvas: function(url: string): any { return sceneTextAtlasCanvases.get(url) || null; },
};
})();
