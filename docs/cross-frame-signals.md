# Cross-frame shared signals

> Status: shipped 2026-05-26. See ADR 0009 (gosx hyphae space:
> `decisions/0009-iframe-transport-postmessage-relay.md`) for the
> architectural rationale and the falsification that motivated this work.

GoSX shared signals (`$preview.*`, `$selection.*`, etc.) normally fan out
inside a single browser frame: the Bridge's signal store is per-Bridge,
and `js.Global()` resolves to the current frame's window. The
cross-frame relay opt-in lets one frame's writes to a configured prefix
reach peer frames' Bridges securely via `postMessage`.

The canonical use case is the GoSX-Studio editor's preview iframe:
editor-side islands write `$preview.block.<key>.visible`, and the
storefront iframe (which mounts a `.gsx` subscriber) receives the write
and applies the DOM mutation locally.

---

## When to use

Reach for the relay when:

- Two frames need to exchange shared-signal state and can name the peer origin
  explicitly. Same-origin and deliberately trusted cross-origin peers both use
  the same allow-list contract.
- The signal namespace is well-scoped (e.g. `$preview.*`) — relaying
  every signal would defeat frame-local semantics elsewhere.
- The peer frame can mount the gosx WASM runtime (it needs a Bridge to
  receive into).

Do NOT reach for the relay when:

- The two pages are cross-origin and you don't want to commit a stable
  origin contract.
- The state needs ordering / acknowledgement guarantees (postMessage
  delivery is fire-and-forget; revisit ADR 0009's triggers).
- The components live in the same document. A `signal.Signal` over the local
  store is faster and simpler.

---

## Security model

Origin validation is mandatory:

- `Bridge.EnableCrossFrameRelay(prefix, allowedOrigin)` records the peer
  origin. Inbound messages are dropped when `event.origin !=
  allowedOrigin`. The literal `"*"` wildcard is allowed for dev work
  only — the JS-side emits a `console.warn` on first use so production
  deployments can audit.
- Outbound `postMessage` uses the configured `allowedOrigin` as the
  `targetOrigin` parameter, so the browser refuses to deliver if the
  peer's actual origin doesn't match.
- The signal-name prefix is non-optional. Without a registered prefix,
  no signals relay; with a prefix, only names that match it relay.
  Other namespaces (`$selection.*`, `$workbench.*`) stay frame-local.

Production deployments should call:

```go
b.EnableCrossFrameRelay("$preview.", "https://studio.example.com")
```

with the editor's expected origin. Never deploy the `"*"` wildcard.

### URL bootstrap compatibility

URL-driven preview mode fails closed. `?gosx-preview=1` no longer implies a
wildcard peer: the URL must also carry a canonical, absolute HTTP(S) origin in
`gosx-preview-origin`. Paths, non-web schemes, malformed escapes, an empty
value, and `*` all leave the relay disabled. The runtime emits a
`[gosx/relay] preview relay disabled` console warning for an active preview URL
that fails this check, so a stale editor integration is visible instead of
silently losing preview updates.

This is a breaking security hardening for editor integrations that previously
sent only `?gosx-preview=1`. Update those iframe URLs to include the editor
origin, for example:

```text
?gosx-preview=1&gosx-preview-origin=https%3A%2F%2Feditor.example.com
```

The programmatic Bridge API still accepts an explicitly requested `"*"` for
local development and warns when it is used. URL auto-bootstrap never does.

---

## Runtime cost

- Each relayed signal write costs one `postMessage` per registered
  peer. Editors with one iframe peer pay one extra message per write.
- The relay never echoes inbound writes back to the peer (depth
  counter in `Bridge.DispatchInboundSignal` suppresses the outbound
  observer). Loops are not a concern.
- Transport latency and artifact bytes are measured properties, not API
  constants. Use the current build manifest and Ouroboros receipt for the
  release you deploy instead of a historical size estimate.

---

## Sample — editor side

```go
// In the editor page's init, after constructing the Bridge:
b := bridge.New()
// ... register runtime, hydrate islands ...
b.EnableCrossFrameRelay("$preview.", "https://storefront.example.com")
```

Or, more commonly, the WASM-side auto-detects the relay from the URL
when the page is loaded inside a preview iframe. The editor passes
`?gosx-preview=1&gosx-preview-origin=https%3A%2F%2Feditor.example.com`
in the iframe's `src` attribute; the storefront WASM reads the query
string at startup and calls `EnableCrossFrameRelay` automatically.

## Sample — storefront side

```go
// In the storefront's layout init (e.g. app/layout.server.go):
import "m31labs.dev/gosx/island"

func init() {
    island.EnablePreviewBootstrap()
    // ... register route module ...
}
```

`EnablePreviewBootstrap` is a process-level idempotent flag. When set,
the storefront Renderer emits a minimal WASM bootstrap (islands runtime
+ relay.js) on every page, so the iframe has a Bridge to receive into.
The relay activation is gated by `?gosx-preview=1` plus an explicit valid
`gosx-preview-origin` — public storefront traffic never activates it.

## Sample — editor builds the iframe URL

```go
previewURL := augmentPreviewURLForCrossFrameRelay("/", request)
// → "/?gosx-preview=1&gosx-preview-origin=https%3A%2F%2Feditor.example.com"
```

Pass `previewURL` into the iframe's `src` attribute.

---

## API summary

### Bridge

```go
// Opt this Bridge into shared-signal relay with peer Bridges.
func (b *Bridge) EnableCrossFrameRelay(prefix, allowedOrigin string)

// Inspect registered relay configurations.
func (b *Bridge) CrossFrameRelays() []CrossFrameRelayConfig

// Register the outbound relay callback (the wasm-side adapter wires this to
// the browser relay facade so postMessage fires for matching writes).
func (b *Bridge) SetCrossFrameRelaySendCallback(fn func(name, valueJSON string))

// Route an inbound peer message into the local store.
func (b *Bridge) DispatchInboundSignal(name, valueJSON, originatingOrigin string) error
```

### Island

```go
// Opt every Renderer thereafter into emitting the preview-mode bootstrap.
func island.EnablePreviewBootstrap()

// Test-only: clear the flag.
func island.ResetPreviewBootstrap()

// Inspect the current state.
func island.PreviewBootstrapEnabled() bool
```

### JS — `window.__gosx.relay`

```js
// Push relay configurations (called by the wasm-side at startup).
window.__gosx.relay.configure([
  {prefix: "$preview.", allowedOrigin: "https://editor.example.com"},
])

// Register an explicit peer target.
window.__gosx.relay.registerPeer(targetWindow, originString)

// Outbound send (called by the wasm-side bridge callback).
window.__gosx.relay.send(name, valueJSON)

// Flush messages that arrived before the wasm-side inbound bridge was ready.
window.__gosx.relay.flushInboundBuffer()
```

The host lifecycle has a separate internal adapter at
`window.__gosx.host.relay.flushInbound()`. It delegates to the same buffer
flush but is named for host-module use. Application and integration code should
use `window.__gosx.relay.flushInboundBuffer()`.

---

## Wire shape

```
EDITOR FRAME                                     IFRAME (STOREFRONT)
─────────────────────────────────────────────────────────────────
[editor island]
  signal.Set "$preview.block.hero.visible" true
       ↓
[Bridge.store.Set]
       ↓
[notifySharedSignal → relaySharedSignal]
       ↓ (prefix matches, not inbound depth)
[relaySendFn → window.__gosx.relay.send]
       ↓
[postMessage {type:"gosx:shared-signal", name, valueJSON, origin}]
                  ─────────────postMessage─────────────→
                                                            ↓
                                                  [window.addEventListener("message")]
                                                            ↓ (type + origin + prefix valid)
                                                  [WASM inbound bridge callback]
                                                            ↓
                                                  [Bridge.DispatchInboundSignal]
                                                            ↓ (relayInboundDepth++)
                                                  [SetSharedSignalJSON → store.Set]
                                                            ↓
                                                  [local fan-out to subscribers]
```

---

## See also

- `decisions/0009-iframe-transport-postmessage-relay.md` (gosx hyphae)
- `decisions/0008-iframe-preview-stays-via-shared-signal-portal.md` (the
  ADR with the falsified original claim)
- `plans/2026-05-26-iframe-cross-frame-signal-transport.md`
- `client/bridge/cross_frame.go`
- `client/wasm/cross_frame.go`
- `client/wasm/cross_frame_parse.go`
- `client/js/relay.js`
- `island/preview_bootstrap.go`
