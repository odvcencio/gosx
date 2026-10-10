# Workbench controls

Require the workbench feature on a page that uses drag handles or layout controls:

```go
ctx.Runtime().RequireFeature("workbench")
```

`ctx.Runtime().Commands(...)` also requires it. The selective loader fetches
`bootstrap-feature-workbench.js`; a page with drag handles additionally loads the
hashed `bootstrap-controller-input.js` URL from the document contract. Pages
without controllers still publish that URL. The core bundles do not grow.

## Drag handles

A handle writes a numeric shared signal on every pointer move and supported key:

```html
<div role="slider" tabindex="0" aria-label="Volume"
     aria-valuemin="0" aria-valuemax="1" aria-valuenow="0.8"
     data-gosx-drag="$mix.vol" data-gosx-drag-axis="y"
     data-gosx-drag-min="0" data-gosx-drag-max="1"
     data-gosx-drag-step="0.01" data-gosx-drag-reset="0.8"
     style="touch-action: none"></div>
```

Use a labeled, focusable `role="separator"` for pane boundaries. Set
`aria-orientation`, `aria-valuemin` and `aria-valuemax` to describe the separator.
`aria-valuenow` and `aria-valuetext` follow the signal. Handles inside island roots
are unsupported; keep them in server-rendered markup.

| Attribute after `data-gosx-drag-` | Default and meaning |
|---|---|
| `axis` | `y`; `x` uses horizontal motion, `y` uses upward motion, `xy` uses horizontal minus vertical motion |
| `min`, `max` | Unbounded; clamp writes to the configured bounds |
| `step` | Range / 100 when both bounds exist, otherwise 1 |
| `scale` | Range / element size along the axis when bounded, otherwise 1; `xy` uses the larger dimension |
| `fine` | 0.1; Alt multiplies the pointer or arrow increment by this factor |
| `reset` | Absent; when supplied, double-click, Backspace or Delete restores this value |
| `end` | Absent; when supplied, write the end-event detail to this signal |

Pointer values snap to `step`, measured from `min` when finite, otherwise zero.
The handle captures the primary pointer. Release commits; Escape, pointer cancel,
lost capture, blur and page disposal restore the value from pointer-down.

| Key | Change |
|---|---|
| Right / Left | Add / subtract one step for `x` or `xy` |
| Up / Down | Add / subtract one step for `y` or `xy` |
| Shift + arrow | Ten steps |
| Alt + arrow | One fine step without snapping |
| PageUp / PageDown | Add / subtract ten steps |
| Home / End | Minimum / maximum when finite |
| Backspace / Delete | Configured reset value |

Every supported key commits. A bubbling `gosx:drag:end` event carries
`{ signal, value, commit }`. Pointer cancellation emits `commit: false`; release,
reset and keyboard changes emit `commit: true`. The optional end signal receives
the same object. Storage subscribed to the value signal sees intermediate writes
and the restored value on cancellation; use the end signal for commit-only work.

## CSS variable bindings

```html
<div data-gosx-bind-style="--gsx-split-a:$layout.sidebar:px,--peak:@data-peak"
     data-peak="0.2"></div>
```

Pairs are comma-separated `--property:source[:unit]`. A `$` source can name an
independent signal (`$layout.sidebar`) or read an object path (`sidebar` in the
`$layout` signal). Both kinds of updates reach the property. An `@data-*` source
reads an attribute and follows attribute changes, so live region bindings can
feed CSS variables without JavaScript in the application. Null values remove the
property. Newly inserted elements are bound after region swaps. Page disposal
removes subscriptions and observers.

## Tabs and collapsible panels

The tabs recipe renders ordinary links in a labeled `nav`. The server selects the
requested `?tab=` value, sets `aria-current="page"` on that link and renders only
the selected panel visible. These links work before the chunk loads.

The workbench adds `tablist`, `tab` and `tabpanel` roles, `aria-controls`,
`aria-labelledby`, `aria-selected` and roving tabindex. Left / Right wrap through
horizontal tabs; Up / Down wrap through vertical tabs when the `nav` has
`aria-orientation="vertical"`. Home / End select the first / last tab. Enter,
Space and click select in place. `data-gosx-tabs-signal` records selection and
restores it from a shared signal. Modified clicks retain ordinary link behavior.

`<details data-gosx-collapsible="$layout.browserOpen">` mirrors native `open`
changes to the signal and boolean signal updates back to `open`. Its `summary`
keeps native keyboard behavior.

## Layout recipes and persistence

Install `splitpane`, `dock`, `tabs` or `collapsible` with `gosx ui add <name>` and
load each installed stylesheet alongside `tokens.css`.

`SplitPane` takes `ID`, `Orientation`, `Signal` and `Initial` strings. A vertical
split puts the sized pane first, then the separator, then the flexible pane. A
horizontal split puts the flexible pane first, then the separator, then the
sized bottom pane. `SplitHandleX` uses a vertical separator; `SplitHandleY` uses a
horizontal one. Both take `Signal`, `Label`, `Min`, `Max` and `Value` strings,
with an 8 px keyboard step. The strict server compiler does not accept an
attribute ternary, so the two handle components provide explicit axes.

`Dock` takes `ID`, `LeftSignal`, `RightSignal`, `BottomSignal`, `Left`, `Right` and
`Bottom` strings. Give its children the `gsx-dock__top`, `gsx-dock__left`,
`gsx-dock__center`, `gsx-dock__right` and `gsx-dock__bottom` classes. Put
`SplitHandleX` last in the left region and first in the right region. For pixel
sizing, add `data-gosx-drag-scale="1"` to the copied handles. Use
`data-gosx-drag-scale="-1"` on a right-edge handle so motion to the left grows the
right pane; adapt the copied component for that override. Put `SplitHandleY`
first in the bottom region. The children own their content and overflow policy.

`Tabs` takes `ID`, `Signal` and `Label`. Put `Tab` links in its default children
slot and `TabPanel` components in `slot="Panels"`; panels render after the nav in
the same container. Each `Tab` takes `ID`, `Href`, `Panel` strings and `Selected`
bool. `TabPanel` takes `ID` and `Hidden`. `Collapsible` takes `Signal`, `Summary`
and `Open`.

Persist layout through the existing controller storage contract:

```go
ctx.Runtime().Controller(controller.Config{
    Root: "#app",
    Storage: &controller.Storage{
        Area: "local", Namespace: "studio:layout",
        Load: []controller.StorageSlot{{Key: "sidebar", Signal: "$layout.sidebar"}},
        Save: []controller.StorageSlot{{Key: "sidebar", Signal: "$layout.sidebar"}},
    },
})
```

Use `$layout.sidebar` for both the handle and its CSS variable binding. Storage
loads before interactions and saves each signal update. The runtime integration
test loads 320, sends ArrowRight on an 8 px separator and observes 328 written to
`studio:layout:sidebar`, with one input-chunk fetch shared by both features.
