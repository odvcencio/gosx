# GoSX Editor

`m31labs.dev/gosx/editor` is an optional Markdown++ editor module with
its own `go.mod`. It ships the server-rendered editor shell, toolbar model,
text operations, and the native browser assets used for live preview, autosave,
outline, gallery, and metadata stats.

## Agent Skill

Agents working on GoSX editor integration should use the [using-gosx-ecosystem](https://github.com/odvcencio/m31labs-skills/blob/main/skills/using-gosx-ecosystem/SKILL.md) skill.

Mount the assets only in apps that use the editor:

```go
app.Mount("/editor/", http.StripPrefix("/editor/", editor.AssetHandler()))
```

The native surface opts into the GoSX runtime-surface contract. When the app
uses the standard GoSX bootstrap, it owns editor discovery, mount/dispose
lifecycle, navigation remounting, abort signals, and the browser fetch bridge.
The editor still renders a complete HTML form and remains usable without the
bootstrap as a standalone progressive enhancement.

The generic contract lives in core GoSX (`gosx.RuntimeSurfaceAttrs` and the
bootstrap runtime-surface API); the editor registers through the public
`runtimeSurfaceAPI.register` seam, with a legacy compatibility fallback, and
owns Markdown++ behavior, commands, panels, and editor-specific UX.
Its managed form markup uses the core `gosx.ManagedFormAttrs` progressive-
enhancement contract, so native submission and the bootstrap enhancement layer
remain explicit without duplicating framework attribute vocabulary in the
editor module.
Its preview, autosave, diagnostics, and upload bridge receives the core
surface request function, so requests inherit the surface abort signal and
shared CSRF behavior without making the editor duplicate framework transport
machinery. The core transport provides each surface with an isolated latest
request scope, so stale preview/metadata/diagnostics/upload work is cancelled
without colliding with another mounted surface. Preview, metadata, diagnostics,
autosave, upload, and gallery work also use the surface's latest-request and JSON
helpers; the editor may still provide explicit headers or request options for
endpoint-specific behavior.

The surface's query, event-listener, dispatch, and disposal methods likewise
come from the core scoped DOM bridge when the standard bootstrap is present.
Editor code can stay focused on its controls and document behavior while
navigation or fragment replacement tears down those listeners automatically.
Its scoped scheduler also owns keyed debounce timers and animation-frame work,
so preview, metadata, diagnostics, autosave, and editor rendering are cancelled
with the surface lifecycle; a small timer fallback keeps standalone HTML
enhancement usable without the bootstrap.
The Markdown++ diagram adapter keeps Mermaid and zoom behavior in the editor,
but binds generated preview and zoom listeners through `surface.listen`, so
async diagram rendering cannot leave listeners behind after a remount.
Its zoom overlay is registered through `surface.dom.portal` when the core DOM
bridge is present, so a surface remount removes the body-mounted dialog and
restores the document scroll state without moving Mermaid or zoom semantics
into GoSX.
The surface also exposes the managed navigation boundary, diagnostics,
telemetry, prose, action, region, and stream services directly; editor
redirects use that navigation boundary and fall back to native location
navigation without bootstrap.
Preview reconciliation uses the surface's lifecycle bridge, so the editor does
not reach into private bootstrap registries for DOM replacement.
The bootstrap's action and region APIs are likewise available through the
shared GoSX namespace; the editor uses them only for generic transport and
fragment lifecycle, keeping Markdown++ response interpretation in the editor.
Runtime telemetry and diagnostics are also core-owned; editor integrations can
report operational request failures through `surface.reportFailure` without
moving Markdown++ source-diagnostic semantics out of the editor. Aborted stale
or unmounted requests are ignored, while preview, metadata, autosave, upload,
and gallery failures retain their HTML/UI fallback states.

Render the component from request-scoped options:

```go
ed := editor.New("post-editor", editor.Options{
	Content:     post.Content,
	Title:       post.Title,
	Slug:        post.Slug,
	FormAction:  ctx.ActionPath("update"),
	AutoSaveURL: ctx.ActionPath("autosave"),
	PreviewURL:  ctx.ActionPath("preview"),
	DiagnosticsURL: ctx.ActionPath("diagnostics"),
	UploadURL:   ctx.ActionPath("upload"),
	ImagesURL:   ctx.ActionPath("images"),
	CSRFToken:   token,
})
return ed.Render()
```

For reusable document editing, keep the surface separated from publishing
metadata:

```go
ed := editor.New("document-editor", editor.Options{
	Surface: editor.SurfaceDocument,
	Editor: editor.EditorOptions{
		Content: "# Notes",
		Label:   "Document editor",
	},
	Runtime: editor.RuntimeOptions{
		PreviewURL: ctx.ActionPath("preview"),
		CSRFToken:  token,
	},
})
return ed.Render()
```

`MetadataOptions` is the publishing/CMS profile. The flattened fields remain
supported for older integrations; profile-based options are the preferred API
for new products.

`Keymap` is honored by the native surface as well as exposed to extensions.
`Mod` resolves to Ctrl on Windows/Linux and Cmd on macOS; mapped formatting
commands use the same `gosx:editor:command` lifecycle event as toolbar actions.

The editor preview uses the shared GoSX prose contract. Consumers can tune the
rhythm without replacing the renderer:

```go
ed := editor.New("post-editor", editor.Options{
	Prose: editor.ProseStyle{
		Size:    "clamp(1rem, 2vw, 1.2rem)",
		Leading: "1.7",
		Flow:    "1.1rem",
	},
})
```

`prose.AssetHandler()` serves the standalone shared stylesheet from the core
module. The editor asset handler also serves it at
`editor.DefaultProseStylesheetURL` for convenience. Link that stylesheet
anywhere a GoSX app renders Markdown++ or other block-oriented prose, then use
the `gosx-prose` class.

For keyed streaming surfaces, the core bootstrap exposes
`window.__gosx.stream.reconcileHTML`, `reconcileBlocks`, and `createBlockStream`.
`editor.DefaultProseScriptURL` serves the core standalone prose runtime plus a
small editor-owned compatibility adapter that exposes the `window.GosxProse`
names and adds only the Markdown++ diagram-aware signature. The generic
fallback therefore remains usable without bootstrap while living in the core
prose package.
Preview updates use `window.__gosx.dom.reconcile` when the core bootstrap is
present, which brackets the adapter's keyed update with surface disposal and
remounting. Without bootstrap, the editor keeps its direct diagram-render
fallback and server-rendered HTML remains the baseline.
The Mermaid/diagram enhancer registers as an editor-owned `mdpp-diagrams`
runtime surface, so GoSX owns its mount/dispose and navigation/fragment
remount lifecycle while the editor retains diagram rendering and zoom UX.

Browser extensions can contribute assets and toolbar commands. Their scripts
listen for `gosx:editor:mount`, `gosx:editor:command`, and
`gosx:editor:unmount` events; command events can call `event.preventDefault()`
and use the supplied `detail.insert` or `detail.replace` helpers.

```js
document.addEventListener("gosx:editor:command", (event) => {
  if (event.detail.extension !== "citations") return;
  event.preventDefault();
  event.detail.insert("[^citation]");
});
```

The preview endpoint remains application-owned. It should accept `content`,
return JSON with an `html` string, and may include `redirect` when a slug or
document identity changes. Streaming-capable endpoints may instead return
`blocks`, an array of `{key, html, complete}` objects; stable keyed blocks are
reconciled in place and the active block can keep changing. Core GoSX disposes
changed block subtrees and mounts their replacements while preserving stable
keyed nodes; the editor's outer surface transaction prevents duplicate mounts.
Each block should render to one top-level HTML element; `complete` identifies
which block is still receiving tokens.

When the editor includes `PanelDiagnostics`, `DiagnosticsURL` enables a
debounced source check while the user edits. The endpoint receives the same
form payload as preview and may return either a diagnostics array or
`{"diagnostics": [...]}`. Each item follows the LSP shape:
`{severity, message, range: {start: {line, character}, end: {line, character}}}`
with zero-based positions. Severity `1`/`error` and `2`/`warning` receive
matching editor treatment; other values are informational. Clicking an item
returns focus to its source range.

Markdown++ rendering is intentionally not a dependency of this module. Apps
should import `github.com/odvcencio/mdpp` directly. Upgrading the renderer
should not require a GoSX framework or editor release, and upgrading the editor
should not require a framework release.
