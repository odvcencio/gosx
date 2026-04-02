# Changelog

## v0.3.2

- **fix(isr):** stale ISR pages now snapshot the existing artifact before background regeneration starts, so the first stale response cannot race ahead and serve the freshly regenerated HTML.
- **fix(test):** hardened hub websocket bootstrap tests to wait for the `__welcome` event without assuming it always arrives before every join broadcast once multiple clients connect.

## v0.3.1

- **fix(eval):** `nil == ""` now returns `true` in template expressions, so unbound variables like `flash.contact` compare equal to empty string instead of rendering conditional branches incorrectly.
- **release:** rolls the `v0.2.4` template-eval fix forward onto the `v0.3.x` line without backing out the native text layout, managed video, or managed motion work shipped in `v0.3.0`.

## v0.3.0

- **feat(text-layout):** `TextBlock` now supports explicit native server-measured layout with `mode="native"` / `TextBlockModeNative`, keeping pages off the bootstrap layer when final wrapped HTML should come from the server.
- **feat(video):** `server.Video`, `ctx.Video`, and `<Video />` now emit a real server `<video>` baseline with authored `<source>` and `<track>` children, and the managed runtime upgrades that baseline in place for HLS, sync, and shared media signals.
- **feat(motion):** added `server.Motion`, `ctx.Motion`, and the `.gsx` `<Motion />` builtin for bootstrap-managed DOM motion presets with `load`/`view` triggers, timing controls, and reduced-motion awareness.
- **docs:** refreshed the README and gosx-docs coverage for engines, video, motion, and native-vs-bootstrap text layout.
- **fix(ci):** restored `make fmt-check` on `main` by formatting `server/runtime_assets.go`.

## v0.2.4

- **fix(eval):** `nil == ""` now returns `true` in template expressions — unbound variables like `flash.contact` compare equal to empty string instead of being a distinct nil that renders `<If>` bodies incorrectly

## v0.2.3

- **fix(css):** Global selectors (`body`, `html`, `:root`, `*`, `*::before`, `*::after`, `::selection`, `::placeholder`) in sidecar CSS are no longer scoped — they pass through to the document directly
- **fix(server):** `/gosx/bootstrap.js` serves a minimal stub script when `gosx build` has not been run, instead of returning 404. The stub initializes the gosx runtime namespace and auto-mounts registered engines from the page manifest.

## v0.2.2

- **feat(route):** Added underscore param convention as Go-compatible alternative to bracket syntax
  - `_slug` directory → `{slug}` route parameter (single underscore = dynamic param)
  - `__path` directory → `{path...}` route parameter (double underscore = catch-all)
  - Valid Go package names, works with `go mod tidy`, `go build ./...`, and module imports
  - Legacy `[slug]` bracket syntax still supported for backward compatibility

## v0.2.1

- **fix(ir):** HTML entities (`&rarr;`, `&mdash;`, etc.) in `.gsx` text are now decoded to UTF-8 characters instead of being double-escaped
- **fix(ir):** `<script>` and `<style>` element content is now treated as raw text, preventing HTML escaping of `&&`, `<`, etc. inside inline scripts
- **fix(build):** `gosx build` no longer generates invalid Go import paths for `[slug]` dynamic route directories
- **fix(route):** Pattern conflicts between `__actions` and wildcard siblings now produce a clear diagnostic error instead of a raw panic

## v0.2.0

### Authentication

- added session-backed auth manager with `auth.New()`, `Require` middleware, and `Current()` user context
- added GitHub OAuth provider (`auth.GitHubProvider`) with automatic email fetching
- added Google OAuth provider (`auth.GoogleProvider`)
- added custom OAuth providers with `OAuthUserResolverFunc`
- added magic link passwordless auth with configurable resolver
- added WebAuthn/passkey auth with register and login flows
- added role-based access control via `RequireRole` middleware

### Engine System

- added Surface engine primitive for dedicated canvas/WebGL/WebGPU workloads
- added Scene3D engine with server-side render bundle generation
- added pixel-surface engine runtime with managed GPU framebuffers
- added 3D perspective rendering with world-space coordinates, frustum culling, and depth sorting
- added material system with presets (flat, glow, ghost, glass, matte) and blend modes
- added fluent `Builder` API for program construction
- added box, sphere, pyramid, and plane 3D primitives
- added scene label support with collision avoidance, occlusion, and priority layout
- added shared engine runtime with program-driven scenes
- added input providers with batched signal delivery (pointer, keyboard)

### Islands and Reactivity

- added shared signals across islands (prefixed with `$`)
- added `Each`/`For` rendering with resolved tree reconciliation
- added keyed reconciliation with auto-key stabilization
- added event field access in island handlers
- added string concatenation in VM expressions
- added raw props object access in island expressions

### File-Based Routing

- added directory-scoped modules (`DirModule`) with inherited middleware
- added file-route metadata sidecars (`page.server.go`)
- added sidecar CSS ownership and layer tracking
- added `route.config.json` for directory-scoped cache and header config
- added spread attribute support in file renderer
- added auto-resolved Go components from `Funcs`, `Values`, and dotted paths
- added not-found error handling for data loaders

### Server and Runtime

- added client-side page navigation with head/body swaps and scroll restoration
- added managed form submission via fetch with navigation runtime
- added ISR serving for prerendered bundles
- added static export (`gosx export`) with prerender pipeline
- added production build pipeline with hashed runtime assets and edge bundle artifacts
- added CSS layer architecture with ownership metadata and global styles
- added JSON request observer for structured logging
- added path traversal protection and runtime manifest caching
- added CSRF token injection in form fetch requests

### Text Layout

- added browser-backed text measurement and layout engine
- added grapheme-aware text layout with word segmentation
- added `TextBlock` primitive for framework-level text layout
- added server-side sizing hints, tab stops, soft hyphens, and CJK punctuation
- added `maxLines` and overflow support for text truncation
- added i18n and vertical writing mode support
- added text layout caching with font loading invalidation

### CRDT

- added document core, sync messages, and hub transport for real-time collaboration

### CLI

- added `gosx fmt` with `--check` mode
- added `gosx check` integration test for `.gsx` files
- added `gosx lsp` language server
- added `gosx init --template docs` scaffold

### Docs and Examples

- added gosx-docs reference app with CMS demo, Scene3D demo, auth flows, streaming, and ISR
- rewrote README with clearer structure and user focus
- renamed JSX terminology to native GSX throughout

### Internal

- modularized `bootstrap.js` with split source files and build step
- extracted helper functions across client, route, and VM packages for readability
- optimized WebGL state with cached blend/depth and static geometry buffers
- added capability-aware rendering and resilient WebGL fallback
- added `prefers-reduced-motion` and scroll handling
- added environment, document, and presentation observation APIs

## v0.1.0

- formalized the initial GoSX release line with a repo-level `gosx.Version`
- completed the zero-manual-wiring island path for compiled `.gsx` islands
- added build-manifest loading for hashed runtime and island assets
- hardened actions, hubs, server timeouts, HTML escaping, and build failure handling
- added repeatable repo tooling with `make test`, `make test-race`, `make test-wasm`, `make build-runtime`, and `make ci`
- added CI coverage for format checks, race tests, js/wasm runtime tests, CLI build, and WASM runtime build
- added js/wasm runtime tests that compile `.gsx` islands, hydrate them through `__gosx_hydrate`, dispatch via `__gosx_action`, and assert the client patch stream
