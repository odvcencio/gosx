# Immutable asset caching with sessions

Framework handlers serving content-addressed, session-independent assets keep
`Cache-Control: public, max-age=31536000, immutable` even when global auth
middleware reads the session on every request. A session read does not change
the asset bytes, so these responses do not acquire `Vary: Cookie`. Compression
still adds `Vary: Accept-Encoding`.

The framework classifies the response after resolving a production artifact or
a versioned public file. Neither an asset-looking URL nor an application-set
public cache header grants this classification to a page or API handler.

## Classified handlers and routes

For a request with a session cookie and global middleware that reads it, every
asset route below previously returned `private, no-store`. Eligible responses
now return `public, max-age=31536000, immutable`.

| Handler | Routes | Bytes served |
| --- | --- | --- |
| Production runtime assets | `/gosx/assets/runtime/<content-hashed-file>` | Runtime JavaScript and WASM, including capability-linked variants |
| Emitted assets | `/gosx/assets/islands/<content-hashed-file>`, `/gosx/assets/css/<content-hashed-file>` | Binary island programs and CSS |
| Emitted images and other assets | `/gosx/assets/images/<content-hashed-file>`, `/gosx/assets/<bucket>/<content-hashed-file>` | Hashed images, scene posters, fonts and media |
| Manifest-backed compatibility assets | The `/gosx/` routes listed below, with or without `?v=<version>` | The production artifact selected by the build manifest |
| Versioned public files | `/<public-file>?v=<version>` | A public asset whose version is supplied by the application |

The manifest-backed compatibility routes are:

- `/gosx/runtime.wasm`, `/gosx/runtime-islands.wasm`, `/gosx/runtime-core.wasm`,
  `/gosx/runtime-engine.wasm`, `/gosx/runtime-collab.wasm`.
- `/gosx/wasm_exec.js`, `/gosx/standard-go-wasm_exec.js`.
- `/gosx/bootstrap.js`, `/gosx/bootstrap-lite.js`, `/gosx/bootstrap-runtime.js`.
- `/gosx/bootstrap-feature-islands.js`, `/gosx/bootstrap-feature-engines.js`,
  `/gosx/bootstrap-feature-hubs.js`, `/gosx/bootstrap-feature-controllers.js`,
  `/gosx/bootstrap-feature-textlayout.js`.
- `/gosx/bootstrap-feature-scene3d.js`,
  `/gosx/bootstrap-feature-scene3d-command.js`,
  `/gosx/bootstrap-feature-scene3d-hydrate.js`,
  `/gosx/bootstrap-feature-scene3d-webgpu.js`,
  `/gosx/bootstrap-feature-scene3d-webgl.js`,
  `/gosx/bootstrap-feature-scene3d-gltf.js`,
  `/gosx/bootstrap-feature-scene3d-animation.js`,
  `/gosx/bootstrap-feature-scene3d-compute.js`,
  `/gosx/bootstrap-feature-scene3d-decompress.js`,
  `/gosx/bootstrap-feature-scene3d-walk.js`,
  `/gosx/bootstrap-feature-scene3d-zoom.js`,
  `/gosx/bootstrap-feature-scene3d-vessel.js`,
  `/gosx/bootstrap-feature-scene3d-ocean-query.js`,
  `/gosx/bootstrap-feature-scene3d-instance-stream.js`.
- `/gosx/patch.js`, `/gosx/hls.min.js`, `/gosx/stripe-bridge.js`, `/gosx/relay.js`.
- `/gosx/islands/<component>.<extension>` and `/gosx/css/<component>.css`.

An unversioned compatibility route can still select development source before
the production manifest. Only the branches that resolve production artifacts
receive this classification. Public files without a version keep revalidation;
applications must update the version when they change a versioned public file.

## Private response boundaries

The session writer checks the classification and final headers before committing
the response. A classified response falls back to `private, no-store` if it:

- Writes session state or sets any cookie, including an application cookie.
- Varies by anything other than `Accept-Encoding`, including `Cookie`,
  `Authorization`, custom session headers, or `*`.
- Has an explicit `private`, `no-store`, or `no-cache` policy.
- Serves HTML, JSON/data, an unknown media type, a redirect, or an error.

Eligible media types are JavaScript, CSS, WASM, binary assets, images, fonts,
audio, video and models. Successful GET/HEAD, 206 partial responses, and 304
conditional responses retain immutable caching. A 304 uses the resolved
artifact's media type when the HTTP server removes representation headers.

Session-dependent pages and API data remain private for visitors with session
state, even if the handler requests public or static caching. Anonymous pages
retain their existing policy and cookie variance. The runtime image optimizer
at `/_gosx/image` and the JSON table at `/_gosx/emoji-codes.json` do not receive
the exemption: their routes do not identify content-addressed assets. Scene
poster header helpers and page cache helpers also do not grant it.
