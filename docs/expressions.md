# GoSX expression language

The `.gsx` expression language is the subset of Go-flavoured syntax
that may appear inside `{ ... }` interpolations and attribute values.
It compiles to the unified VM's opcode set (`island/program/program.go`)
and is evaluated by `client/vm/vm.go`.

This document specifies the **supported subset as of Phase 4** and the
**explicit non-goals**. Anything not on the supported list is rejected
at parse time. Authors who hit the wall should
[file a follow-up](#when-you-hit-the-wall) rather than work around it.

---

## HTML boolean attributes

HTML presence attributes use native HTML semantics in server output and island
patches. A boolean expression such as `hidden={collapsed}` or
`required={needsValue}` emits the attribute when true and removes it when
false; the browser's reflected property is updated with it. This applies to
standard presence attributes such as `hidden`, `required`, `disabled`,
`selected`, and `checked`, with ASCII-case-insensitive names.

Enumerated and application attributes are not booleans. Values such as
`aria-pressed={false}` and `spellcheck={false}` remain the literal string
`"false"`. The special HTML state `hidden="until-found"` is also preserved as
a string. Use `gosx.BoolAttr(name)` when a custom attribute intentionally needs
presence semantics independent of the standard HTML name list.

---

## Supported array methods

Each form lowers to an existing opcode — no new opcodes were added in
Phase 4 (per ADR 0002: stay on v1).

| Form                                | Opcode      | Notes                                          |
|-------------------------------------|-------------|------------------------------------------------|
| `xs.filter(fn)`                     | `OpFilter`  | `fn` is a single-param closure or expression   |
| `xs.map(fn)`                        | `OpMap`     | Body may be any single expression              |
| `xs.find(fn)`                       | `OpFind`    | Returns the zero value when nothing matches    |
| `xs.slice(start, end)`              | `OpSlice`   | Both bounds required (positive ints)           |
| `xs.append(x)`                      | `OpAppend`  | Returns a new array; no mutation               |
| `xs.len()` or `xs.length`           | `OpLen`     | Equivalent forms                               |
| `xs.contains(x)`                    | `OpContains`| Returns bool                                   |

### Predicate / transformer arguments

The `xs.map(fn)`, `xs.filter(fn)`, and `xs.find(fn)` methods accept
either of two equivalent forms:

```gsx
// Bare-expression form (legacy; still supported).
items.map(_item * 2)

// Single-param closure form (Phase 4 addition).
items.map(func(i){ return i * 2 })
items.map(func(i){ i * 2 })       // 'return' is optional for expression bodies
```

Both lower to the same opcode shape. The closure form binds its
parameter name to the magic `_item` prop for the duration of the body,
so authors may pick whichever reads better.

The closure body also has access to `_index` (the current iteration
position):

```gsx
items.map(func(i){ _index + 1 })  // 1, 2, 3, ...
```

---

## Supported string methods

| Form                            | Opcode         |
|---------------------------------|----------------|
| `s.toLower()`                   | `OpToLower`    |
| `s.toUpper()`                   | `OpToUpper`    |
| `s.trim()`                      | `OpTrim`       |
| `s.split(sep)`                  | `OpSplit`      |
| `s.contains(sub)`               | `OpContains`   |
| `s.startsWith(prefix)`          | `OpStartsWith` |
| `s.endsWith(suffix)`            | `OpEndsWith`   |
| `s.replace(old, new)`           | `OpReplace`    |
| `s.substring(start, end)`       | `OpSubstring`  |
| `xs.join(sep)`                  | `OpJoin`       |

Method names are matched case-insensitively, so `s.ToUpper()`,
`s.toUpper()`, and `s.TOUPPER()` all map to `OpToUpper`.

---

## Closures

Phase 4 introduces a tight subset of closure syntax for predicates and
transformers passed to `.map`, `.filter`, and `.find`.

### Allowed

- **Single parameter** only: `func(x){ ... }`.
- **Single expression body**: `func(x){ x.name }` or `func(x){ return x.name }`.
- The `return` keyword is optional for a one-expression body. Both
  forms produce identical IR.
- The parameter name is local to the closure body. Outer props,
  signals, and the magic `_item` / `_index` props remain visible.

### Not allowed (rejected at parse time with a clear error)

- **Multi-param closures**: `func(a, b){ ... }` —
  there is no second iteration variable in the runtime; if you need
  one, pre-shape the data Go-side or file a follow-up.
- **Zero-param closures**: `func(){ ... }` —
  ambiguous and not needed by any supported method.
- **Multi-statement bodies**: `func(i){ x := 1; return x }` —
  the VM has no statement evaluator inside expression context.
- **Closures outside `.map` / `.filter` / `.find` arguments** —
  rejected to keep the grammar unambiguous with Go's anonymous
  function syntax.
- **Captures by reference** — the closure body only reads props /
  signals / the magic iteration prop. There is no mutable shared
  state captured from the enclosing scope at parse time.

### Method-chain depth cap

Chains deeper than **four** `.foo(...)` levels are rejected with a
helpful error message that points the author to refactor the
expression or file a follow-up:

```gsx
// OK — four levels.
items.filter(func(i){ return i.a }).map(func(i){ return i.b }).filter(func(i){ return i.c }).map(func(i){ return i.d })

// Rejected — exceeds the 4-level cap.
items.filter(...).map(...).filter(...).map(...).filter(...)
```

This is a sanity cap, not a hard architectural limit. If a real-world
case needs deeper chains, file a follow-up plan that documents the
case and the desired new cap.

---

## Computed island state

`signal.Derive` and `signal.Computed` declarations compile into reactive,
read-only VM values. Computeds may read mutable signals, shared `$` signals,
props, and computed values declared earlier in the component:

```gsx
count := signal.New(1)
doubled := signal.Derive(func() int { return count.Get() * 2 })
label := signal.Computed(func() int { return doubled.Get() + 1 })
increment := func() { count.Set(count.Get() + 1) }

return <button onClick={increment}>{label.Get()}</button>
```

The VM uses the signal package's dependency tracking and batch semantics, so a
handler with several writes still reconciles once from the final derived
value. Computeds work in rendered expressions and handler bodies. Installing a
shared signal rebinds the derived graph to that shared instance; program reload
preserves mutable state but rebuilds derived definitions from the new
bytecode. Reload also binds all new shared inputs before its single reconcile,
so an existing store value never produces an intermediate stale patch.
Initial hydration follows the same bind-all/one-reconcile rule, patching any
preloaded browser-store value against the server-rendered initializer.
Disposal stops every retained derived subscription.

Definitions are initialized in source order, matching Go's lexical rules.
Normal `.gsx` compilation rejects a reference to a later declaration. A
malformed hand-built program that contains a forward/self reference remains
bounded and observes the typed zero value rather than recursing.

---

## Browser effects in island handlers

Island handlers have one reserved capability receiver, `browser`. Calls lower
to the existing `OpHostCall` opcode, so adding the authoring surface does not
change the island program wire format. The receiver exists only while parsing
handler bodies; it is not available in render expressions, computed values, or
server components.

```gsx
import "m31labs.dev/gosx/browser"

openPalette := func() { browser.Open("#command-palette") }
closePalette := func() { browser.Close("#command-palette") }
nextResult := func() { browser.FocusMove("[data-command-result]", 1) }
activateResult := func() { browser.Activate("[data-command-result]") }
copyCode := func() { browser.ClipboardWrite(code.Get()) }
refresh := func() { browser.Refresh() }
```

The allowlisted methods are:

| Method | Result | Behavior |
|---|---:|---|
| `Open(selector string)` | `bool` | After the handler, calls `showModal` when available; otherwise opens an accessible hidden/open fallback. |
| `Close(selector string)` | `bool` | After the handler, calls `close` when available; otherwise closes `<details>` without hiding its summary, or restores the hidden state for other fallbacks. |
| `Focus(selector string)` | `bool` | Focuses one matching element after the handler. |
| `FocusMove(selector string, direction int)` | `bool` | After the handler, moves from `document.activeElement` through visible matches, wrapping at either end. A positive direction moves forward; a negative direction moves backward. |
| `Activate(selector string)` | `bool` | Defers a click on the first visible match to a microtask, avoiding reentrant island dispatch. |
| `ClipboardWrite(text string)` | `bool` | Uses `navigator.clipboard.writeText`; the focus-capable temporary-textarea fallback runs after the handler when the API is missing, or from the API's rejection microtask. Returns false when neither mechanism can accept the request. |
| `Navigate(url string[, replace bool[, preserveScroll bool]])` | `bool` | Soft-navigates same-origin HTTP(S), hard-navigates safe cross-origin HTTP(S), and rejects other schemes. A rejected managed-navigation promise falls back once to the validated hard target. |
| `Refresh()` | `bool` | Uses managed same-URL `navigation.revalidate` when present, with a legacy forced navigate/cache-clear fallback. It never calls the state-only `navigation.refresh`; a rejected managed promise reloads the current URL. |
| `Submit(selector string)` | `bool` | Defers `requestSubmit` to a microtask so signal-derived form inputs are patched first. |
| `PreventDefault()` | `bool` | Calls `preventDefault` on the event currently dispatching the handler. |
| `StopPropagation()` | `bool` | Stops the current DOM event and any remaining global-island fanout. |
| `ScrollIntoView(selector string[, behavior string])` | `bool` | After reconciliation, scrolls the current matching element; a behavior such as `"smooth"` is optional. |

Every effect accepts an optional **leading boolean guard**. This is the
statement-free equivalent of a small conditional in the constrained handler
language:

```gsx
browser.Open(canOpen.Get(), "#command-palette")
browser.PreventDefault(metaKey || ctrlKey)
browser.Navigate(saved.Get(), "/agenda", false, true)
```

A false guard returns false without touching the browser. Empty selectors and
URLs are also safe no-ops. Unknown methods and invalid arities are compile
errors; invalid runtime value types become VM diagnostics rather than panics.

### Root scoping and lifecycle

Selector methods query only inside the hydrated island root (the root itself is
also eligible). A nested `[data-gosx-island]` starts a new ownership boundary,
so an outer host cannot focus, activate, submit, or otherwise select a nested
island's elements. Each hydrated island receives its own host receiver from the
bridge factory. Reload
rebinds it, and disposal removes the binding and invokes receiver cleanup before
the island is released. Deferred submission and activation store only the
island id and selector, then resolve the current root in the microtask; they
never retain a detached DOM node.

Open, close, focus movement, activation, submission, and the synchronous
clipboard fallback are microtask-isolated from the dispatch that requested
them. Native focus/blur events therefore cannot re-enter the island VM while
its outer handler still owns the event scope. `PreventDefault` and
`StopPropagation` remain synchronous so they still control the native event.

An island runs at most one handler at a time. If a custom host receiver tries
to dispatch back into the same island synchronously, the nested dispatch is a
no-op with a `reentrant_dispatch` diagnostic (and the bridge returns an
explicit error). Nested dispatch to a different island is allowed; the bridge
tracks both active islands so shared-signal callbacks cannot reconcile either
one until its own handler returns.

### Handler event fields

The delegated runtime exposes these identifiers directly inside handlers. A
field omitted from the compact browser payload evaluates as the zero value.
The runtime therefore omits false, zero, and empty typed defaults. Pointer and
mouse coordinates are copied only for their event families; `timeStamp` is
retained for `keydown`/`keyup`, where timed keyboard chords need the browser
clock.

| Group | Fields |
|---|---|
| Core/control | `type`, `value`, `checked`, `selectedIndex`, `editable` |
| Keyboard | `key`, `code`, `ctrlKey`, `metaKey`, `altKey`, `shiftKey`, `repeat`, `timeStamp` |
| Targets | `targetID`, `currentTargetID` |
| Pointer | `pointerID`, `pointerType`, `isPrimary`, `clientX`, `clientY`, `button`, `buttons`, `pressure` |
| Window | `width`, `height` |
| Element/transfer data | `data`, `eventData` |

`data` is an object containing the current handler element's application
`data-*` values (framework-owned `data-gosx-*` wiring is excluded).
`eventData` is the `data-gosx-event-value` string. On `dragstart` the runtime
writes it to `text/plain`; on `drop` the transferred `text/plain` value becomes
`eventData`. External drop text is accepted only through 64 KiB of UTF-8 data
before JSON/WASM forwarding; a larger transfer is ignored. Authored
`data-gosx-event-value` content is unaffected by that external-input boundary.
Handled `dragover` and `drop` events are automatically prevented.

Delegated root events include click/input/change/submit, keyboard/focus, drag,
and pointer events. The island manifest's existing `events` list is used to
attach only the distinct event types the island declares; manifests that omit
or set the list to null retain the attach-all behavior for backward
compatibility. Modern eventless islands serialize an explicit `events: []` and
attach no root listeners. Delegated traversal and global-marker discovery stop
at nested island roots. Three
convention-based attributes provide explicit global ownership without a
manifest or wire-format addition:

```gsx
<main
  onDocumentKeyDown={handleShortcut}
  onDocumentKeyUp={releaseShortcut}
  onWindowResize={measureViewport}
>
  ...
</main>
```

Document/window events fan out to every island that declares the convention.
Calling `browser.StopPropagation()` is the explicit way for one handler to stop
the remaining island fanout. Listener records retain their actual event target,
so page/island disposal removes document and window listeners from the same
objects on which they were installed.

---

## Non-goals

These are intentionally out of scope for the expression language.
Implementing any of them requires its own plan and (typically) an ADR.

- **Multi-param lambdas** — `func(a, b){ ... }`.
- **Map / dict comprehensions** — `{k: v for k, v in m}`.
- **Generic functions** — `func[T any](x T){ ... }`.
- **Operator overloading** — defining `+` for custom types.
- **Method definitions** — `func (r Receiver) Name() { ... }`.
- **Control-flow keywords** beyond `return` — no `if`, `for`,
  `switch`, `defer`, `go`, `select`, channels.
- **Method chains** deeper than four levels.

If the grammar accepts a form, the runtime handles it. If the runtime
needs a form the grammar rejects, the grammar change is the work —
not a runtime hack.

---

## When you hit the wall

If a real `.gsx` use case demands something outside the supported
subset:

1. **Stop**. Don't silently expand the parser.
2. **File a Phase 4.x follow-up plan** under
   `~/.hyphae/spaces/m31labs-gosx/plans/` describing:
   - The use case (concrete `.gsx` snippet you wanted to write).
   - Whether the runtime would need a new opcode (if yes, the
     follow-up also triggers an ADR per ADR 0002).
   - Whether the grammar change is local or risks breaking existing
     valid `.gsx` files.
3. **Workaround until then**: pre-shape the data in the Go handler
   file companion to your `.gsx`. The "Go-side pre-shaping" pattern
   was the only option pre–Phase 4; it remains the safe fallback.

The expression language is deliberately small. Keeping it small keeps
the VM small, keeps the WASM bundle small, and keeps the contract
between `.gsx` authors and runtime engineers narrow enough to fit in
this document.
