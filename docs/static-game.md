# Ship a static game

`gosx export` turns a GoSX app into a folder (or one zip) of plain files. Any
static host can serve it, from any path, with no Go server. This is the format
a game jam platform, itch.io-style host, or CDN bucket expects.

## Build

```sh
gosx export --zip          # dist/static/ and dist/static.zip
gosx export --zip=game.zip # archive at a path you choose
```

`gosx export` runs the same production pipeline as `gosx build --prod`: the
TinyGo runtime, content-hashed asset names, and `wasm-opt` when it is installed.
It keeps `dist/static`, so the two commands write the same static folder.
Export needs TinyGo on `PATH`. The build runs `wasm-opt` from `PATH`, or the
copy that ships inside TinyGo. If neither exists it prints an install hint
(`apt install binaryen` or `brew install binaryen`) and ships a larger runtime.

The zip holds `index.html` at its root, plus every page, script, WASM file, and
public asset. It leaves out the `.gz` and `.br` copies, because a host can
compress on its own. The folder keeps them for hosts that serve precompressed
files.

## What "any path" means

Every URL the page uses to load code or data is relative to the page:

- `href`, `src`, `srcset`, `poster`, and `data-gosx-*-url` attributes,
- the paths in the `#gosx-manifest` and `#gosx-document` JSON (runtime WASM,
  island programs, script loader),
- strings in scene and engine props that name a file in `public/`
  (for example a `.glb` model).

A page at `/index.html` writes `gosx/assets/...`; a page at `/about/index.html`
writes `../gosx/assets/...`. The same folder therefore works at
`https://host/`, `https://host/games/x/`, or `file`-style mounts, with no build
flag. If you do know the mount, `server.SetBasePath` still works: the exporter
writes the files under that prefix and the URLs stay relative.

Your stylesheets get the same treatment: root-absolute `url(/img/a.png)` and
`@import "/x.css"` that name a file in the bundle become paths relative to the
stylesheet. Not rewritten: URLs in your own scripts. Use relative URLs there.

## No server calls

A static export:

- turns browser telemetry off (`<meta name="gosx-telemetry" content="off">`),
  so the page does not POST to `/_gosx/client-events`. An app can switch it
  back on with `window.__gosx_telemetry_config = {enabled: true, endpoint: "https://..."}`.
- fails if a page still names a `/_gosx/` endpoint,
- drops the unused full WASM runtime from pages that do not need WASM.

Pages cannot be exported when they declare actions, take route parameters, or
set cookies. `gosx export` stops and lists each one. Set `{"prerender":false}` in
`route.config.json` to leave a page out on purpose. A page with a `Load` function
runs it once at build time and prints a warning.

## Hosting in an iframe

Tested: Chromium, the game in a cross-origin iframe with
`sandbox="allow-scripts allow-same-origin"`, at the host root and at
`/games/x/`. Hydration, click handlers, and the Scene3D canvas all work, with no
404s and no request to `/_gosx/`.

| iframe `sandbox` | Extra host headers | Result |
|---|---|---|
| `allow-scripts allow-same-origin` | none | Works. |
| `allow-scripts` only (opaque origin) | `Access-Control-Allow-Origin: *` on the game's files | Works. |
| `allow-scripts` only (opaque origin) | none | Fails: scripts and the WASM runtime are blocked by CORS. |

With no `allow-same-origin`, the page's origin is `null`, so every file it
loads is a cross-origin request. The scripts carry `crossorigin="anonymous"`
and `integrity`, and the runtime fetches WASM and island programs with
`fetch`. All of these need a CORS response. Dropping `crossorigin` and
`integrity` would fix the scripts only: `fetch` from a `null` origin always
needs `Access-Control-Allow-Origin`, so the game would still fail. GoSX keeps
the integrity checks and documents the requirement instead. If the platform
sandboxes without `allow-same-origin` and cannot add the header, the game
cannot run there; ask the platform for `allow-same-origin`.

Your host must not send `X-Frame-Options: DENY`/`SAMEORIGIN` or a
`frame-ancestors` CSP that excludes the platform for the game's URL.
The page's own scripts need `script-src` to allow the host origin and
`'wasm-unsafe-eval'` (the runtime compiles WASM). Hashed scripts use
`integrity`, so a platform that rewrites JS breaks them.

## Size

Measured on a one-island, one-Scene3D-box app (`gosx init`, reduced), built with
TinyGo. Bytes; gzip is `gzip -9`, brotli is `brotli -q 11`.

| Build | Files | Raw | gzip -9 | brotli -q 11 |
|---|---|---|---|---|
| `gosx build --prod` static output before this change | 21 | 4,930,437 | 1,753,332 | 1,372,057 |
| `gosx export`, no `wasm-opt` | 20 | 2,713,277 | 875,352 | 724,476 |
| `gosx export`, TinyGo's bundled `wasm-opt` (v116) | 20 | 2,675,456 | 866,567 | 716,861 |

The drop is the full runtime `gosx-runtime.wasm` (2.2 MB raw) that the old
output staged for the 404 and about pages, which never fetch it. The largest
files that remain are the Scene3D chunk (681 KB), the core runtime WASM
(695 KB), and the WebGPU renderer (495 KB). The build uses `wasm-opt` from `PATH`, or the copy
that TinyGo bundles, and saves about 8 KB (brotli) on the core runtime WASM.

Check your own bundle: `gosx export --zip`, then `unzip -l dist/static.zip`.
Get the platform's size limit before you add models and audio, because
`public/` assets add to the total byte for byte.
