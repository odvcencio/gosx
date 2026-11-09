// text-atlas.ts — label textures drawn with the canvas 2D API (Props.TextAtlases).
//
// A text atlas is a grid of short labels (jersey numbers, nameplates) that
// many materials share; each material picks its cell with a UV region. The
// mount registers the atlases from props before each frame. Drawing happens
// once per distinct texture URL ("gosx-text-atlas:<id>#<hash>"): the canvas is
// encoded as a PNG data URL, and both renderers' texture descriptors resolve
// the atlas URL to it through sceneTextAtlasURL, so standard maps and Selena
// texture params load it like any image. sceneTextAtlasCanvas keeps the canvas
// for a direct upload path (a WriteTexture-style API) to use instead.
const sceneTextAtlasImages = new Map<string, string>();
const sceneTextAtlasCanvases = new Map<string, any>();
const SCENE_TEXT_ATLAS_SCHEME = "gosx-text-atlas:";

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
  const pad = Math.min(0.45, Math.max(0, sceneNumber(spec.padding, 0.08)));
  const outline = sceneNumber(spec.outlineWidth, 0);
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  ctx.lineJoin = "round";
  for (let i = 0; i < labels.length; i++) {
    const text = labels[i] == null ? "" : String(labels[i]);
    if (!text) continue; // a blank cell stays transparent
    const x = (i % cols) * cw, y = Math.floor(i / cols) * ch;
    if (spec.background) { ctx.fillStyle = spec.background; ctx.fillRect(x, y, cw, ch); }
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

// sceneRegisterTextAtlases draws the atlases in props that are not drawn yet.
// The mount calls it before every frame; a scene without atlases pays one
// property read.
function sceneRegisterTextAtlases(props: any): void {
  const list = props && props.textAtlases;
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

// sceneTextAtlasURL resolves an atlas texture URL to its drawn image. Other
// URLs, and atlases not drawn yet, pass through unchanged.
function sceneTextAtlasURL(url: any): any {
  if (typeof url !== "string" || url.charCodeAt(0) !== 103 || !url.startsWith(SCENE_TEXT_ATLAS_SCHEME)) return url;
  return sceneTextAtlasImages.get(url) || url;
}

function sceneTextAtlasCanvas(url: string): any {
  return sceneTextAtlasCanvases.get(url) || null;
}

if (typeof window !== "undefined") {
  const sceneTextAtlasApi = window.__gosx_scene3d_api || (window.__gosx_scene3d_api = {});
  sceneTextAtlasApi.sceneTextAtlasURL = sceneTextAtlasURL;
  sceneTextAtlasApi.sceneTextAtlasCanvas = sceneTextAtlasCanvas;
}
