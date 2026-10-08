# Island events

An island handler attribute (`onClick={save}`) binds a DOM event to a handler
in the island. The compiler accepts the attributes in the table below. Any
other `onX` attribute on an island element is a compile error that names the
attribute and lists the supported ones, because the runtime would never attach
a listener for it.

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
| `offsetX`, `offsetY` | float | coordinates relative to the element that owns the handler, not to `event.target` |
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
optional leading boolean guard. When the browser ends the capture, the island
receives `onLostPointerCapture`.

```gsx
//gosx:island
component Fader(props: FaderProps) {
    value := signal.New(props.Initial)
    grab := func() { browser.CapturePointer(pointerID) }
    drag := func() { value.Set(offsetY) }
    drop := func() { browser.ReleasePointer(pointerID) }
    nudge := func() { value.Set(value.Get() + 1) }

    return <div tabIndex="0" onPointerDown={grab} onPointerMove={drag}
        onPointerUp={drop} onLostPointerCapture={drop} onKeyDown={nudge}>
        {value.Get()}
    </div>
}
```

Inside a handler, payload fields are bare names (`pointerID`, `offsetY`). A
signal, prop or handler with the same name shadows the field. This sketch is
not compiled by the test suite; the compile path is covered by
`ir/exprparse_browser_test.go`.

## Wheel and scrolling

The runtime registers `wheel` listeners as non-passive, so a handler can call
`browser.PreventDefault()` to stop the page from scrolling. Prevent the default
only for a handler that uses the wheel, because a non-passive listener can
slow scrolling.
