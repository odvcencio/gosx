# GoSX Modules

This repository contains independently versioned Go modules:

- `m31labs.dev/gosx`: the core framework.
- `m31labs.dev/gosx/editor`: the optional browser editor shell and assets.

Markdown++ rendering is the canonical content-source layer for the core
`content` package through `github.com/odvcencio/mdpp`. The framework owns the
collection-loading contract: mdpp handles parsing/rendering, typed frontmatter,
diagnostics, and renderer options, while applications can still import mdpp
directly for lower-level renderer control.

The core module also exposes `m31labs.dev/gosx/prose`, a renderer-agnostic
contract for shared prose rhythm and conservative top-level Markdown++ block
boundaries. The optional editor consumes it, but docs, blogs, and CMS surfaces
can use it without importing the editor shell; `prose.AssetHandler()` serves
the shared `prose.css` and standalone generic `prose-runtime.js` assets
directly. The editor composes that runtime with its Markdown++ diagram
signature adapter rather than carrying a second keyed reconciliation engine.

Optional browser surfaces can register with the shared bootstrap through
`data-gosx-runtime-surface`. The bootstrap owns discovery, navigation
remounting, scoped DOM/query/fetch/listen/abort access, stream-template
consumption, and disposal; ecosystem packages such as `gosx/editor` provide
the surface factory without making core import the package. The markup
vocabulary is exposed as `gosx.RuntimeSurface`/`RuntimeSurfaceAttrs`,
`gosx.Action`/`ActionAttrs`, and `gosx.Region`/`RegionAttrs`, so products can
describe progressive-enhancement contracts without copying framework
attribute strings.

Core `gosx.ProgressiveEnhancementAttrs` and `gosx.ManagedFormAttrs` provide the
same contract for navigation, motion, and managed forms. They keep the
enhancement layer and native fallback explicit in server HTML; optional
packages should consume these helpers instead of hand-building
`data-gosx-enhance`, `data-gosx-form`, and fallback attributes.

The bootstrap also exposes one request boundary at `window.__gosx.request`
and `window.__gosx.transport`. It adds the page CSRF token to mutating requests
when the caller has not supplied one, preserves caller headers, exposes a
shared JSON response helper, and lets runtime surfaces inherit their scoped
abort signal. Actions, regions, enhanced navigation, and optional surfaces
use this bridge when available and fall back to ordinary `fetch` when the
bootstrap is absent.

Surface contexts additionally expose `requestLatest(key, input, init)` and
`cancelRequest(key)` for latest-wins work such as previews, diagnostics, and
search suggestions. A latest request is aborted when a newer request with the
same key starts and is also cancelled when the surface unmounts. `json` and
`requestJSON` keep response decoding on the same surface contract.

Surface contexts also receive a scoped `scheduler` with keyed `schedule` /
`cancelSchedule` methods and keyed animation-frame `frame` /
`cancelFrame` methods. Scheduled work is coalesced within that surface and
cancelled on unmount, so debounced previews, autosaves, and layout work do not
outlive the DOM they target. The same service is available globally at
`window.__gosx.scheduler`, with `scope(parentSignal)` for other runtime
surfaces.

The context also publishes scoped `dom` and `transport` services plus live
`navigation`, `diagnostics`, `telemetry`, `prose`, `actions`, `regions`, and
`stream` services when those core APIs are present. `navigate(url, options)`
routes through managed navigation when available and otherwise preserves native
location navigation, so an optional surface does not need to reach into private
globals for redirects.
`reconcile(target, updater, options)` provides the lifecycle-aware DOM update
transaction through the surface boundary for streamed or keyed product views.
`reportFailure(operation, error, fields)` applies the shared warning-level
diagnostic and telemetry policy while ignoring stale or unmounted aborts.

The same failure policy is available globally as
`window.__gosx.reportFailure(operation, error, fields)` and through
`window.__gosx.diagnostics.reportFailure`. Core declarative actions and regions
use it for request failures, while optional surfaces delegate to it with their
scope and component metadata. `fields.telemetry` carries the bounded event
payload; diagnostic issue fields remain separate so DOM nodes and error objects
are never sent as telemetry data.

The same policy is available to any runtime through
`window.__gosx.transport.scope(parentSignal)`, which returns a scoped requester
with `request`, `requestLatest`, `cancelRequest`, `requestJSON`, and `dispose`.
The transport also exposes a global `requestLatest` for non-scoped work. This
keeps cancellation and lifecycle composition in GoSX while allowing products to
choose their own request keys and response semantics.

The matching DOM bridge is `window.__gosx.dom.scope(root, parentSignal)`. It
returns `query`, `queryAll`, `contains`, `listen`/`on`, `dispatch`, `own` /
`portal`, `signal`, and `dispose` methods. Surface contexts delegate to this
scope when the full DOM runtime is present, so event listeners and DOM access
are cleaned up with the same parent lifecycle as requests. `portal(node)` owns
a node mounted outside the surface root—typically under `document.body`—and
removes it when the returned release function or the scope disposal runs.
Optional adapters should bind listeners through `surface.listen` (or
`surface.dom.listen`) and register external nodes through `surface.dom.portal`
when they are mounted; this keeps async-render listeners and overlays inside
the same disposal boundary.

Declarative regions are also bootstrap-managed: they bind idempotently on
initial render and every enhanced navigation, abort superseded requests,
dispose nested runtime surfaces before swaps, and emit
`gosx:region:before|after|error` lifecycle events. Deferred server streams
carry a declarative `data-gosx-stream-target` marker in addition to their
backward-compatible inline replacement script, allowing navigation-cloned
streams to resolve through the core bootstrap. The stream consumer delegates
to `window.__gosx.dom.replaceFragment` when available, so a streamed marker is
disposed before its replacement is inserted, every inserted element is
remounted, and the template is removed after one successful consumption. The
inline script uses the same boundary when bootstrap has already loaded and
falls back to native `replaceWith` when it has not.

The same primitives are available as `window.__gosx.actions` (`run`, `parse`,
`applyResult`, `dispatch`) and `window.__gosx.regions` (`mount`, `dispose`,
`refresh`, `applySceneCommands`, `bindings`). The older
`__gosx_declarative_regions` name remains an alias for compatibility. These
APIs expose transport and lifecycle seams; product-specific response meaning
and editor behavior remain outside the core runtime.

The managed navigation script publishes its API at `window.__gosx.navigation`
(`navigate`, `submitAction`, `getState`, and `refresh`) while retaining
`window.__gosx_page_nav` as a compatibility alias. Programmatic navigation can
therefore stay in the shared GoSX namespace instead of requiring optional
surfaces to know a private global. Stylesheet readiness checks use the shared
core scheduler when available, and navigation/form failures use the shared
`reportFailure` diagnostics/telemetry policy before preserving native fallback.

Fragment-producing actions, regions, and deferred streams share the core DOM
lifecycle at `window.__gosx.dom` (`mount`, `dispose`, `replace`, and
`replaceFragment`). HTML replacement performs one ordered dispose → swap →
stream/scene/surface/region enhancement pass, including scoped managed-motion
and text-layout records, and preserving a region root's own binding while
replacing its contents. Fragment replacement applies the same transaction to
the marker element itself and mounts each top-level inserted element. The
`gosx:runtime:content:before|after|error` events make that transition visible
to extensions without requiring them to patch action or region internals.

Streaming products can use `window.__gosx.dom.reconcile(root, update)` when
their own keyed reconciler mutates an enhanced subtree. GoSX disposes the old
surface/region state, invokes the product-owned updater, and remounts the new
content in one transaction; the region root is preserved by default. This is
the lifecycle seam used by the editor preview, so Markdown++ diagram rendering
stays editor-owned without requiring the editor to duplicate GoSX cleanup.

Keyed HTML responses use the core stream API at
`window.__gosx.stream.reconcileHTML`, `reconcileBlocks`, and
`createBlockStream`; the same generic contract is also available at
`window.__gosx.prose` and `window.GosxProse`. It accepts a signature hook and
key-attribute option so an optional surface can preserve its own enhanced
subtrees without copying the reconciliation algorithm. Changed or removed
nodes pass through the core dispose/mount lifecycle, while unchanged keyed
nodes retain their DOM state; when a product calls the reconciler inside
`dom.reconcile`, the outer transaction owns the single lifecycle pass. The
editor's compatibility asset supplies only the Markdown++ diagram-aware
signature and keeps a standalone fallback for pages that deliberately omit the
core bootstrap.

Core lifecycle transitions also flow through the existing bounded telemetry
channel under the `runtime-surface`, `runtime-dom`, `action`, `region`, `stream`, and
`navigation` categories. Applications can observe the corresponding DOM
events for local UI while the server-side client-events handler receives only
bounded operational fields. The public `window.__gosx.telemetry` API exposes
`emit`, `flush`, `session`, and `enabled`; the legacy telemetry globals remain
available for compatibility and the disabled configuration remains a no-op.

Runtime failures use the core diagnostics API: `window.__gosx.diagnostics`
supports `report`, `list`, `clear`, `clearFor`, and `subscribe`, while the
legacy `reportIssue`/`listIssues` functions remain available. Clearing a
surface or navigation root removes its issue annotations and emits a
`gosx:diagnostics` change event; editor source diagnostics remain an
editor-owned Markdown++ concern.
