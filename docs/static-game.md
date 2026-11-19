# Ship a static game

`gosx export` turns a GoSX app into a folder (or one zip) of plain files. Any
static host can serve it, from any path, with no Go server. This is the format
a game jam platform, itch.io-style host, or CDN bucket expects.

## Build

```sh
gosx export --zip          # dist/static/ and dist/static.zip
gosx export --zip=game.zip # archive at a path you choose
gosx export --out=site     # also write the bundle to ./site
gosx export --precompressed=false   # no .gz or .br copies in the folder
gosx export --single-file  # also write dist/static.html, one file (see "One file")
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
files, unless you pass `--precompressed=false`.

| Option | What it does |
|---|---|
| `--zip[=path]` | Writes the archive. Default path `dist/static.zip`. |
| `--out=<dir>` | Also writes the static bundle to `<dir>`. A relative path is relative to the current directory. The folder is created. It must be missing, empty, or a previous export (it has an `index.html`); a previous export's files are replaced, not merged. A folder that is not empty and has no `index.html`, one that holds a `go.mod`, the project folder or a parent of it, and `dist/static` are refused, before the build starts. The last line reads `gosx export: static bundle in <dir>`. |
| `--precompressed=false` | Leaves the `.gz` and `.br` copies out of the exported folder: `<dir>` with `--out`, else `dist/static`. Use it for a host that rejects them. Only a `.gz` or `.br` file that sits beside its original is dropped, so an app's own `data.gz` stays. Default `true`. |
| `--single-file[=path]` | Also writes the bundle as one HTML file. Default path `dist/static.html`. A relative path is relative to the current directory. |

Tested: the flag parsing, the `--out` folder rule, the copy that skips the
copies, and one real export of the starter app with all three options. Not
tested: `--out` on Windows, and `--out` onto a symbolic link.

## One file

`--single-file` packs the whole bundle into one `.html` file. Double-click it
and the game runs from disk (`file://`) with no server and no network. The same
file also works when a host serves it from any path.

How it works. The file holds a small loader page and, inside it, every file of
the bundle in one gzip block (base64). The loader's `<title>`, `lang`,
viewport and `theme-color` come from your `index.html`, so the loading screen
looks like the app. On open, the loader unpacks the block with
`DecompressionStream`, makes one blob URL per file, rewrites the paths in
`index.html` (attributes and the inline JSON) to those URLs, and writes the page
with `document.open/write/close`. A stylesheet gets its `url(...)` and `@import`
paths rewritten before its blob is made. A `fetch` of a path that names a
bundled file is answered from memory; every other request goes to the real
`fetch`. glTF and GLB paths stay as written in the markup and are served the
same way through `fetch`, because the scene loader picks its parser from the
path and resolves a model's buffers and textures against it. A worker or a
worklet that starts from a relative path to a bundled script gets the blob (or
`data:`) URL. The output is deterministic: sorted paths, a fixed MIME table, and
a gzip header with no timestamp. The same bytes in give the same file out (with
one Go version; the compressor may differ between Go versions).

The file holds every file except `.gz`, `.br`, `.map` and `.html` pages other
than `index.html`. A single file holds one page: if the bundle has pages besides
`index.html` and `404.html`, `gosx export` prints a warning that names them. The
loader shows a status line while it unpacks. If the browser lacks
`DecompressionStream` or WebAssembly, or the unpack fails, it shows a plain
message instead. It makes no external request.

What does not work (each is pinned by the browser test):

- `import()` from a bundled script, a `<script type="module">` that imports
  another file by relative path, and `importScripts()` inside a worker. A blob
  URL is no base for a relative path, so these fail. Bundle the module graph into
  one script before you export (for example with esbuild).
- `XMLHttpRequest`, and `fetch` inside a worker, are not wrapped, so they go to the real network (not tested).
- A path built at run time (`"models/" + name`) works through `fetch`, but not as
  an `<img src>` set from script: only paths written in `index.html` are
  rewritten.
- The unpack needs the whole file in memory: the loader holds the base64 text,
  the bytes and the blobs at once.

Tested in Chrome for Testing 1248 (headless, Linux), on a small fixture
(a stylesheet with `@import` and `url()`, an image, a classic script, an inline
JSON manifest, a bundled JSON file, a WebAssembly module loaded with
`instantiateStreaming(fetch(...))`, a glTF with a separate buffer, a worker, and
an audio worklet): opened with `file://` while every `http://` and `https://`
request was blocked, and served over HTTP from `/deep/sub/` (the server saw no
request for a bundled file). Not tested: Firefox and Safari, Windows `file://`
paths, and any game other than the fixture and one real export below. The
fixture packs to 10,466 bytes. A real game with 65 files (3D models, textures,
WebAssembly, runtime scripts), 20.7 MB of files without the `.gz` and
`.br` copies, packs to 11,599,640 bytes (11.1 MiB).

## A WASM audio kernel in a static game

A game that plays a WASM audio kernel (`game/audio/kernel`, for example
Cicada's music engine) has no server to mount `worklet.Handler()` on. Set
`kernel.Config.InlineProcessor`: the processor then loads from the copy
compiled into the game, as a `data:` URL, in the folder and in the single file.
Put the kernel module and its data in `public/` and fetch them with
`audio.HTTPLoader` and a relative path; in the single file those requests are
answered from memory.

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

Object keys in those JSON blocks are lookup names and stay as written. The
texture-variant table (`textureVariants`) is left untouched too: its keys are
the source paths the app was built with, and the glTF loader resolves its URIs
against the model, not the page. Compressed-texture variants therefore match
only when the bundle is served from the mount it was built for (the root by
default). At another path the game falls back to the original texture. Build
with `SetBasePath` for a fixed subpath when you ship variants.

Your stylesheets get the same treatment: root-absolute `url(/img/a.png)` and
`@import "/x.css"` that name a file in the bundle become paths relative to the
stylesheet. Not rewritten: URLs in your own scripts. Use relative URLs there.

## No server calls

A static export:

- turns browser telemetry off in `gosx export` output (`<meta name="gosx-telemetry" content="off">`),
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
