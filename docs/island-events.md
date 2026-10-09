# Island events

An island handler attribute (`onClick={save}`) binds a DOM event to a handler
in the island. The runtime handles the attributes in the table below, and the lowercase-tail
spellings it has always resolved (`onKeydown`, `onPointerdown`, `onDragstart`).
Any other `onX` attribute on an island element (`onMouseDown`, `onScroll`,
`onKey`) never fires. `gosx check` reports it as a warning with the source
position and, when one is close, a "did you mean" suggestion. The warning does
not fail the build.

The legacy `data-on-<event>` spelling accepts the same event types in
lowercase (`data-on-wheel`, `data-on-dblclick`).

## Events

| Attribute | DOM event | Payload fields | Keyboard path |
|---|---|---|---|
| `onClick` | `click` | pointer position, `button`, `buttons` | Enter or Space on a button |
| `onInput`, `onChange` | `input`, `change` | `value`, `checked`, `selectedIndex` | native |
| `onSubmit` | `submit` | | Enter in a form field |
| `onKeyDown`, `onKeyUp` | `keydown`, `keyup` | `key`, `code`, modifiers, `repeat`, `timeStamp` | native |
| `onFocus`, `onBlur` | `focus`, `blur` | | Tab |
| `onDragStart`, `onDragEnd`, `onDragOver`, `onDragLeave`, `onDrop` | `drag*`, `drop` | pointer position, `eventData` | pair with `onKeyDown` move commands |
| `onPointerDown`, `onPointerMove`, `onPointerUp`, `onPointerCancel` | `pointer*` | `pointerID`, `pointerType`, `isPrimary`, pointer position, `offsetX`, `offsetY`, `pressure`, `width`, `height` | pair with `onKeyDown` (arrow keys) |
| `onWheel` | `wheel` | pointer position, `offsetX`, `offsetY`, `deltaX`, `deltaY`, `deltaMode` | pair with `onKeyDown` (`+`, `-`) |
| `onDblClick` | `dblclick` | pointer position, `offsetX`, `offsetY`, `button` | pair with `onKeyDown` (Backspace or Delete resets) |
| `onContextMenu` | `contextmenu` | pointer position, `offsetX`, `offsetY`, `button` | the browser also fires it from the ContextMenu key and Shift+F10 |
| `onLostPointerCapture` | `lostpointercapture` | `pointerID`, pointer position, `offsetX`, `offsetY` | none needed |
| `onDocumentKeyDown`, `onDocumentKeyUp` | `keydown`, `keyup` on `document` | as `onKeyDown` | native |
| `onWindowResize` | `resize` on `window` | `width`, `height` | none needed |

Every gesture needs a keyboard path. A handler that only reacts to the wheel,
a double-click or a drag leaves keyboard users without the feature, so add an
`onKeyDown` handler beside it.

## Payload fields

| Field | VM type | Meaning |
|---|---|---|
| `clientX`, `clientY` | float | viewport coordinates |
| `offsetX`, `offsetY` | float | `clientX` and `clientY` minus the top-left of the handler element's `getBoundingClientRect()`. The origin is the border box edge, so the border counts. CSS transforms are not undone: under a rotation or scale the values are in viewport pixels, not in the element's own space. |
| `elementWidth`, `elementHeight` | float | the handler element's `getBoundingClientRect()` width and height, so a handler can compute `offsetX / elementWidth`. Pointer events already use `width` and `height` for the contact size, so these have their own names. |
| `deltaX`, `deltaY` | float | wheel deltas |
| `deltaMode` | int | 0 pixels, 1 lines, 2 pages |
| `pointerID`, `button`, `buttons`, `selectedIndex` | int | |

The runtime omits zero-valued numeric fields, so a missing field reads as zero.

## Pointer capture

`browser.CapturePointer(pointerID)` routes later pointer events for that
pointer to the element that owns the handler. `browser.ReleasePointer(pointerID)`
undoes it. Call `CapturePointer` from `onPointerDown`: browsers honour the
request only while that event dispatches. Outside a dispatch, or when the
browser refuses, both calls return false. Each call takes one argument and an
optional leading boolean guard. Capture ends as the Pointer Events
specification defines: after `pointerup` or `pointercancel`, on `ReleasePointer`,
or when the element leaves the document. The island then receives
`onLostPointerCapture`. Disposing an island does not release a capture early.

```gsx
package fader

import "m31labs.dev/gosx/signal"

type FaderProps struct {
	Initial float64
}

//gosx:island
func Fader(props FaderProps) Node {
	value := signal.New(props.Initial)
	grab := func() { browser.CapturePointer(pointerID) }
	drag := func() { value.Set(offsetY / elementHeight) }
	drop := func() { browser.ReleasePointer(pointerID) }
	nudge := func() { value.Set(value.Get() + 0.01) }

	return <div tabIndex="0" onPointerDown={grab} onPointerMove={drag}
		onPointerUp={drop} onLostPointerCapture={drop} onKeyDown={nudge}>
		{value.Get()}
	</div>
}
```

Inside a handler, payload fields are bare names (`pointerID`, `offsetY`). A
signal, prop or handler with the same name shadows the field. Strict
`component` syntax does not accept bare event fields, so use the `func` form
for handlers that read them. `ir/island_docs_test.go` compiles this example.

Inside a handler, payload fields are bare names (`pointerID`, `offsetY`). A
signal, prop or handler with the same name shadows the field. This sketch is
not compiled by the test suite; the compile path is covered by
`ir/exprparse_browser_test.go`.

## Wheel and scrolling

The runtime registers `wheel` listeners as non-passive, so a handler can call
`browser.PreventDefault()` to stop the page from scrolling. Prevent the default
only for a handler that uses the wheel, because a non-passive listener can
slow scrolling.
